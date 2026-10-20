package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// fakeTileFetcher is an injectable tileFetcher for tests, backed by a
// caller-supplied responder function so tests can simulate upstream
// success/failure without any real network call. It also counts calls so
// tests can assert exactly how many times (if any) the upstream was hit.
type fakeTileFetcher struct {
	mu        sync.Mutex
	calls     int
	responder func(req *http.Request) (*http.Response, error)
}

func (f *fakeTileFetcher) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return f.responder(req)
}

func (f *fakeTileFetcher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func fakeUpstreamResponse(status int, contentType string, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
}

func newTestTileCache(t *testing.T) *tileCache {
	t.Helper()
	dir := t.TempDir()
	tc, err := newTileCache(filepath.Join(dir, "tiles.sqlite"))
	if err != nil {
		t.Fatalf("newTileCache: %v", err)
	}
	t.Cleanup(func() { _ = tc.close() })
	// globalTileCircuitBreaker is process-wide state keyed on real
	// wall-clock time (up to tileCircuitBreakerCooldown), same leak concern
	// as place_name.go's backoff: a transport failure induced by any
	// test's fake fetcher (this package has several, e.g. basemap_proxy_
	// test.go's failingFetcher) would otherwise silently suppress upstream
	// attempts in every OTHER test that shares this same window - and
	// every test that resolves a tile constructs its cache through this
	// one helper, so resetting it here covers the whole suite.
	resetTileCircuitBreaker(t)
	return tc
}

func newTileProxyRequest(z, x, y string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/world-imagery/"+z+"/"+x+"/"+y, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("z", "x", "y")
	c.SetParamValues(z, x, y)
	return c, rec
}

func TestProxyWorldImageryTileHandler_CacheHitServesWithoutInvokingFetcher(t *testing.T) {
	cache := newTestTileCache(t)
	wantBytes := []byte("cached-tile-bytes")
	if err := cache.put(worldImagerySource, 10, 100, 200, wantBytes, "image/jpeg"); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	fetcher := &fakeTileFetcher{responder: func(req *http.Request) (*http.Response, error) {
		t.Fatal("fetcher should not be invoked on a cache hit")
		return nil, nil
	}}

	handler := proxyWorldImageryTileHandler(cache, fetcher)
	c, rec := newTileProxyRequest("10", "100", "200")
	if err := handler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !bytes.Equal(rec.Body.Bytes(), wantBytes) {
		t.Fatalf("expected cached bytes %q, got %q", wantBytes, rec.Body.Bytes())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Fatalf("expected content-type image/jpeg, got %q", ct)
	}
	if fetcher.callCount() != 0 {
		t.Fatalf("expected fetcher not to be called, got %d calls", fetcher.callCount())
	}
}

func TestProxyWorldImageryTileHandler_CacheMissFetchesOnceThenCachesForRepeat(t *testing.T) {
	cache := newTestTileCache(t)
	upstreamBytes := []byte("fresh-tile-bytes")
	fetcher := &fakeTileFetcher{responder: func(req *http.Request) (*http.Response, error) {
		return fakeUpstreamResponse(http.StatusOK, "image/jpeg", upstreamBytes), nil
	}}

	handler := proxyWorldImageryTileHandler(cache, fetcher)

	c1, rec1 := newTileProxyRequest("11", "500", "600")
	if err := handler(c1); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !bytes.Equal(rec1.Body.Bytes(), upstreamBytes) {
		t.Fatalf("expected upstream bytes %q, got %q", upstreamBytes, rec1.Body.Bytes())
	}
	if fetcher.callCount() != 1 {
		t.Fatalf("expected 1 fetcher call after first request, got %d", fetcher.callCount())
	}

	c2, rec2 := newTileProxyRequest("11", "500", "600")
	if err := handler(c2); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !bytes.Equal(rec2.Body.Bytes(), upstreamBytes) {
		t.Fatalf("expected cached upstream bytes %q on repeat, got %q", upstreamBytes, rec2.Body.Bytes())
	}
	if fetcher.callCount() != 1 {
		t.Fatalf("expected fetcher call count to stay at 1 on repeat request, got %d", fetcher.callCount())
	}
}

func TestProxyWorldImageryTileHandler_DegradesToParentTileOnUpstream404(t *testing.T) {
	cache := newTestTileCache(t)
	parentBytes := []byte("z9-parent-tile-bytes")

	fetcher := &fakeTileFetcher{}
	fetcher.responder = func(req *http.Request) (*http.Response, error) {
		// Requested tile is z=10; its Esri URL path is .../tile/10/<y>/<x>.
		// The parent (z=9) URL is .../tile/9/<y>/<x>.
		if bytes.Contains([]byte(req.URL.Path), []byte("/tile/10/")) {
			return fakeUpstreamResponse(http.StatusNotFound, "text/plain", nil), nil
		}
		if bytes.Contains([]byte(req.URL.Path), []byte("/tile/9/")) {
			return fakeUpstreamResponse(http.StatusOK, "image/jpeg", parentBytes), nil
		}
		t.Fatalf("unexpected upstream request: %s", req.URL.Path)
		return nil, nil
	}

	handler := proxyWorldImageryTileHandler(cache, fetcher)

	c1, rec1 := newTileProxyRequest("10", "500", "600")
	if err := handler(c1); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec1.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec1.Code)
	}
	if !bytes.Equal(rec1.Body.Bytes(), parentBytes) {
		t.Fatalf("expected degraded parent bytes %q, got %q", parentBytes, rec1.Body.Bytes())
	}
	if fetcher.callCount() != 2 {
		t.Fatalf("expected 2 fetcher calls (failed z10, successful z9), got %d", fetcher.callCount())
	}
	if cc := rec1.Header().Get("Cache-Control"); cc != degradedTileCacheControl {
		t.Fatalf("expected a degraded result to get the short Cache-Control %q, got %q", degradedTileCacheControl, cc)
	}

	// Repeat identical request: the degraded z9 bytes are NOT cached under
	// the z10 key (that's the fix - see resolveWorldImageryTile), so this
	// makes one more attempt at z10 (fails again, same as before) but then
	// hits the z9 tile already cached under ITS OWN key from the first
	// request - one more fetcher call, not a full re-walk.
	c2, rec2 := newTileProxyRequest("10", "500", "600")
	if err := handler(c2); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !bytes.Equal(rec2.Body.Bytes(), parentBytes) {
		t.Fatalf("expected degraded parent bytes %q on repeat, got %q", parentBytes, rec2.Body.Bytes())
	}
	if fetcher.callCount() != 3 {
		t.Fatalf("expected exactly 1 additional fetcher call on repeat (retry z10, then a cache hit on z9's own key), got %d total calls", fetcher.callCount())
	}
	if cc := rec2.Header().Get("Cache-Control"); cc != degradedTileCacheControl {
		t.Fatalf("expected the repeat's degraded result to also get the short Cache-Control %q, got %q", degradedTileCacheControl, cc)
	}
}

func TestProxyWorldImageryTileHandler_FallsBackToBlankAfterExhaustingDegradeLevels(t *testing.T) {
	cache := newTestTileCache(t)
	fetcher := &fakeTileFetcher{responder: func(req *http.Request) (*http.Response, error) {
		return fakeUpstreamResponse(http.StatusNotFound, "text/plain", nil), nil
	}}

	handler := proxyWorldImageryTileHandler(cache, fetcher)

	// z=10 degrading down through z=9,8,7,6 (4 coarser levels) = 5 total attempts.
	c1, rec1 := newTileProxyRequest("10", "500", "600")
	if err := handler(c1); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec1.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec1.Code)
	}
	if !bytes.Equal(rec1.Body.Bytes(), transparentPNG1x1) {
		t.Fatalf("expected transparent blank PNG fallback, got %d bytes", len(rec1.Body.Bytes()))
	}
	if fetcher.callCount() != 5 {
		t.Fatalf("expected 5 fetcher calls (original + 4 degrade levels), got %d", fetcher.callCount())
	}
	if cc := rec1.Header().Get("Cache-Control"); cc != degradedTileCacheControl {
		t.Fatalf("expected the blank fallback to get the short Cache-Control %q, got %q", degradedTileCacheControl, cc)
	}

	// Repeat: the blank result is deliberately NOT cached under the
	// original key (that's the fix - a permanently blank tile after one
	// offline pan is exactly the bug), and nothing else got cached either
	// (every level 404'd, none succeeded), so this walks the full ladder
	// again - 5 more fetcher calls, not zero.
	c2, rec2 := newTileProxyRequest("10", "500", "600")
	if err := handler(c2); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !bytes.Equal(rec2.Body.Bytes(), transparentPNG1x1) {
		t.Fatalf("expected transparent blank PNG on repeat, got %d bytes", len(rec2.Body.Bytes()))
	}
	if fetcher.callCount() != 10 {
		t.Fatalf("expected a full second ladder walk on repeat (5 more calls, 10 total) since nothing was cached under the requested key, got %d total calls", fetcher.callCount())
	}
}

func TestNewWorldImageryHTTPClient_HasSixSecondTimeout(t *testing.T) {
	client := newWorldImageryHTTPClient()
	wantTimeout := 6 * time.Second
	if got := client.Timeout; got != wantTimeout {
		t.Fatalf("expected timeout %v, got %v", wantTimeout, got)
	}
}

// ── circuit breaker: shared by the imagery and vector-tile fetchers ────────

// resetTileCircuitBreaker clears the package-level globalTileCircuitBreaker
// for the duration of the test, restoring the original afterward - the
// same leak concern as place_name.go's backoff: this state is keyed on
// real wall-clock time (up to tileCircuitBreakerCooldown), so a test that
// trips it would otherwise silently suppress every other test's upstream
// calls for the rest of that window.
func resetTileCircuitBreaker(t *testing.T) {
	t.Helper()
	globalTileCircuitBreaker.mu.Lock()
	origFail, origUntil := globalTileCircuitBreaker.consecutiveFail, globalTileCircuitBreaker.openUntil
	globalTileCircuitBreaker.consecutiveFail = 0
	globalTileCircuitBreaker.openUntil = time.Time{}
	globalTileCircuitBreaker.mu.Unlock()
	t.Cleanup(func() {
		globalTileCircuitBreaker.mu.Lock()
		globalTileCircuitBreaker.consecutiveFail, globalTileCircuitBreaker.openUntil = origFail, origUntil
		globalTileCircuitBreaker.mu.Unlock()
	})
}

// transportFailFetcher always fails at the transport level (fetcher.Do
// itself returns an error), the same shape a dead uplink actually produces
// - as opposed to fakeUpstreamResponse(http.StatusNotFound, ...), which is
// a clean HTTP response and must never trip the breaker.
func transportFailFetcher() *fakeTileFetcher {
	return &fakeTileFetcher{responder: func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("simulated transport failure (connection refused)")
	}}
}

func TestResolveWorldImageryTile_BreakerOpensAfterConsecutiveTransportFailuresAndSkipsUpstream(t *testing.T) {
	resetTileCircuitBreaker(t)
	cache := newTestTileCache(t)
	fetcher := transportFailFetcher()

	// Each resolve at a distinct (z,x,y) walks the original zoom plus
	// maxDegradeLevels coarser ones, all of which fail the same way -
	// tileCircuitBreakerThreshold consecutive transport failures trips the
	// breaker well before that first resolve even returns.
	_, _, _, err := resolveWorldImageryTile(cache, fetcher, worldImagerySource, 10, 500, 600)
	if err != nil {
		t.Fatalf("expected the blank fallback (no error) even with the breaker tripping mid-walk, got %v", err)
	}
	callsAfterFirst := fetcher.callCount()
	if callsAfterFirst < tileCircuitBreakerThreshold {
		t.Fatalf("expected at least %d real attempts before the breaker could trip, got %d", tileCircuitBreakerThreshold, callsAfterFirst)
	}

	// A second resolve for a DIFFERENT tile (so the cache can't be why)
	// must skip upstream entirely while the breaker is open: no new
	// fetcher calls at all.
	_, _, _, err = resolveWorldImageryTile(cache, fetcher, worldImagerySource, 11, 700, 800)
	if err != nil {
		t.Fatalf("expected the blank fallback (no error) while the breaker is open, got %v", err)
	}
	if got := fetcher.callCount(); got != callsAfterFirst {
		t.Fatalf("expected no additional upstream calls while the breaker is open, got %d (was %d)", got, callsAfterFirst)
	}
}

func TestResolveCartoVectorTile_BreakerOpensAndSkipsUpstream(t *testing.T) {
	resetTileCircuitBreaker(t)
	cache := newTestTileCache(t)
	fetcher := transportFailFetcher()

	for i := 0; i < tileCircuitBreakerThreshold; i++ {
		if _, _, err := resolveCartoVectorTile(cache, fetcher, 10, i, i); err == nil {
			t.Fatalf("expected an error on a transport failure with nothing cached")
		}
	}
	tripped := fetcher.callCount()
	if tripped < tileCircuitBreakerThreshold {
		t.Fatalf("expected at least %d real attempts, got %d", tileCircuitBreakerThreshold, tripped)
	}

	if _, _, err := resolveCartoVectorTile(cache, fetcher, 12, 999, 999); err == nil {
		t.Fatalf("expected an error while the breaker is open (cache-only, nothing cached)")
	}
	if got := fetcher.callCount(); got != tripped {
		t.Fatalf("expected no additional upstream call while the breaker is open, got %d (was %d)", got, tripped)
	}
}

// TestResolveWorldImageryTile_StatusErrorsNeverTripTheBreaker is the direct
// regression guard for "not HTTP 404": a clean 404 response is a real
// answer (no coverage at this tile), not evidence the link is down, and
// must never count toward the breaker even after far more than
// tileCircuitBreakerThreshold of them.
func TestResolveWorldImageryTile_StatusErrorsNeverTripTheBreaker(t *testing.T) {
	resetTileCircuitBreaker(t)
	cache := newTestTileCache(t)
	fetcher := &fakeTileFetcher{responder: func(req *http.Request) (*http.Response, error) {
		return fakeUpstreamResponse(http.StatusNotFound, "text/plain", nil), nil
	}}

	// One resolve alone makes 5 real (non-transport) failed attempts
	// (original + 4 degrade levels), already more than
	// tileCircuitBreakerThreshold - if 404s counted, the breaker would be
	// open by the end of this single call.
	if _, _, _, err := resolveWorldImageryTile(cache, fetcher, worldImagerySource, 10, 500, 600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !globalTileCircuitBreaker.allow() {
		t.Fatalf("expected the breaker to remain closed after nothing but clean 404 responses")
	}
}

func TestTileCircuitBreaker_HalfOpensAfterCooldownAndClosesOnSuccess(t *testing.T) {
	b := &tileCircuitBreaker{}

	for i := 0; i < tileCircuitBreakerThreshold; i++ {
		b.recordTransportFailure()
	}
	if b.allow() {
		t.Fatalf("expected the breaker to be open immediately after tripping")
	}

	// Force the cooldown to have already elapsed rather than sleeping in
	// the test.
	b.mu.Lock()
	b.openUntil = time.Now().Add(-time.Millisecond)
	b.mu.Unlock()

	if !b.allow() {
		t.Fatalf("expected the breaker to half-open (allow a trial attempt) once the cooldown has elapsed")
	}

	b.recordSuccess()
	if !b.allow() {
		t.Fatalf("expected the breaker to stay closed after a successful half-open probe")
	}
	b.mu.Lock()
	failCount := b.consecutiveFail
	b.mu.Unlock()
	if failCount != 0 {
		t.Fatalf("expected recordSuccess to reset the failure count, got %d", failCount)
	}
}

// ── per-key in-flight merging ────────────────────────────────────────────

func TestResolveWorldImageryTile_ConcurrentMissesForSameTileCauseOneUpstreamCall(t *testing.T) {
	resetTileCircuitBreaker(t)
	cache := newTestTileCache(t)
	wantBytes := []byte("upstream-tile-bytes")

	release := make(chan struct{})
	var calls int32
	fetcher := &fakeTileFetcher{responder: func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		<-release
		return fakeUpstreamResponse(http.StatusOK, "image/jpeg", wantBytes), nil
	}}

	const callers = 10
	var wg sync.WaitGroup
	errs := make([]error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, _, _, err := resolveWorldImageryTile(cache, fetcher, worldImagerySource, 10, 500, 600)
			errs[n] = err
		}(i)
	}

	time.Sleep(50 * time.Millisecond) // let every goroutine reach the fetcher
	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected exactly 1 upstream call for %d concurrent misses on the same tile, got %d", callers, got)
	}
	for i, err := range errs {
		if err != nil {
			t.Errorf("caller %d: unexpected error: %v", i, err)
		}
	}
}

func TestResolveCartoVectorTile_ConcurrentMissesForSameTileCauseOneUpstreamCall(t *testing.T) {
	resetTileCircuitBreaker(t)
	cache := newTestTileCache(t)
	wantBytes := []byte("upstream-mvt-bytes")

	release := make(chan struct{})
	var calls int32
	fetcher := &fakeTileFetcher{responder: func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		<-release
		return fakeUpstreamResponse(http.StatusOK, "application/x-protobuf", wantBytes), nil
	}}

	const callers = 10
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := resolveCartoVectorTile(cache, fetcher, 12, 3000, 2000); err != nil {
				t.Errorf("resolveCartoVectorTile: %v", err)
			}
		}()
	}

	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected exactly 1 upstream call for %d concurrent misses on the same tile, got %d", callers, got)
	}
}

// ── SQLite pragmas: WAL, synchronous NORMAL, busy_timeout ──────────────────

func TestNewTileCache_OpensInWALModeWithMultipleConnectionsAllowed(t *testing.T) {
	cache := newTestTileCache(t)

	var journalMode string
	if err := cache.db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if strings.ToLower(journalMode) != "wal" {
		t.Fatalf("expected journal_mode=wal, got %q", journalMode)
	}

	var synchronous int
	if err := cache.db.QueryRow(`PRAGMA synchronous`).Scan(&synchronous); err != nil {
		t.Fatalf("PRAGMA synchronous: %v", err)
	}
	// SQLite reports synchronous as an integer: 0=OFF, 1=NORMAL, 2=FULL.
	if synchronous != 1 {
		t.Fatalf("expected synchronous=NORMAL (1), got %d", synchronous)
	}

	var busyTimeoutMS int
	if err := cache.db.QueryRow(`PRAGMA busy_timeout`).Scan(&busyTimeoutMS); err != nil {
		t.Fatalf("PRAGMA busy_timeout: %v", err)
	}
	if busyTimeoutMS != 5000 {
		t.Fatalf("expected busy_timeout=5000ms, got %d", busyTimeoutMS)
	}

	if got := cache.db.Stats().MaxOpenConnections; got != tileCacheMaxOpenConns {
		t.Fatalf("expected MaxOpenConnections=%d, got %d", tileCacheMaxOpenConns, got)
	}
}

// TestNewTileCache_ConcurrentReadDuringWriteDoesNotError is a light
// end-to-end check that WAL mode actually delivers what it's for here: a
// reader isn't blocked out by an in-progress writer, unlike the old
// rollback-journal/single-connection setup this replaces.
func TestNewTileCache_ConcurrentReadDuringWriteDoesNotError(t *testing.T) {
	cache := newTestTileCache(t)

	var wg sync.WaitGroup
	errCh := make(chan error, 20)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if err := cache.put(worldImagerySource, 10, n, n, []byte("tile"), "image/jpeg"); err != nil {
				errCh <- fmt.Errorf("put: %w", err)
			}
		}(i)
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if _, _, _, err := cache.get(worldImagerySource, 10, n, n); err != nil {
				errCh <- fmt.Errorf("get: %w", err)
			}
		}(i)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent read/write error: %v", err)
	}
}

// ── basemap_assets cache (style/tilejson/glyph/sprite documents) ───────────

func TestBasemapAssetCache_PutGetRoundTrip(t *testing.T) {
	cache := newTestTileCache(t)

	if _, _, ok, err := cache.getBasemapAsset("/api/basemap/style/positron"); err != nil {
		t.Fatalf("getBasemapAsset on empty cache: %v", err)
	} else if ok {
		t.Fatal("expected a miss on an empty cache")
	}

	want := []byte(`{"version":8}`)
	if err := cache.putBasemapAsset("/api/basemap/style/positron", want, "application/json"); err != nil {
		t.Fatalf("putBasemapAsset: %v", err)
	}

	data, contentType, ok, err := cache.getBasemapAsset("/api/basemap/style/positron")
	if err != nil {
		t.Fatalf("getBasemapAsset: %v", err)
	}
	if !ok {
		t.Fatal("expected a hit after put")
	}
	if !bytes.Equal(data, want) {
		t.Fatalf("expected %q, got %q", want, data)
	}
	if contentType != "application/json" {
		t.Fatalf("expected content-type application/json, got %q", contentType)
	}

	// A different path is a distinct entry - basemap_assets is keyed on
	// the full request path, not just a bare asset name.
	if _, _, ok, err := cache.getBasemapAsset("/api/basemap/style/dark-matter"); err != nil {
		t.Fatalf("getBasemapAsset: %v", err)
	} else if ok {
		t.Fatal("expected a miss for a different path")
	}

	// Re-putting the same path updates in place (ON CONFLICT DO UPDATE),
	// same as tileCache.put for the (z,x,y) tiles table.
	updated := []byte(`{"version":8,"name":"updated"}`)
	if err := cache.putBasemapAsset("/api/basemap/style/positron", updated, "application/json; charset=utf-8"); err != nil {
		t.Fatalf("putBasemapAsset (update): %v", err)
	}
	data, contentType, ok, err = cache.getBasemapAsset("/api/basemap/style/positron")
	if err != nil || !ok {
		t.Fatalf("getBasemapAsset after update: ok=%v err=%v", ok, err)
	}
	if !bytes.Equal(data, updated) {
		t.Fatalf("expected updated bytes %q, got %q", updated, data)
	}
	if contentType != "application/json; charset=utf-8" {
		t.Fatalf("expected updated content-type, got %q", contentType)
	}
}

// TestTileCacheClear_AlsoClearsBasemapAssets is the regression guard for
// the documented decision on tileCache.clear: DELETE /api/world-imagery/cache
// wipes both the tiles table and basemap_assets, deliberately, not by
// accident.
func TestTileCacheClear_AlsoClearsBasemapAssets(t *testing.T) {
	cache := newTestTileCache(t)
	if err := cache.put(worldImagerySource, 5, 1, 1, []byte("tile"), "image/jpeg"); err != nil {
		t.Fatalf("seed tiles: %v", err)
	}
	if err := cache.putBasemapAsset("/api/basemap/tilejson", []byte("{}"), "application/json"); err != nil {
		t.Fatalf("seed basemap_assets: %v", err)
	}

	if err := cache.clear(); err != nil {
		t.Fatalf("clear: %v", err)
	}

	if _, _, ok, err := cache.get(worldImagerySource, 5, 1, 1); err != nil {
		t.Fatalf("get after clear: %v", err)
	} else if ok {
		t.Fatal("expected tiles table cleared")
	}
	if _, _, ok, err := cache.getBasemapAsset("/api/basemap/tilejson"); err != nil {
		t.Fatalf("getBasemapAsset after clear: %v", err)
	} else if ok {
		t.Fatal("expected basemap_assets table cleared too, per tileCache.clear's documented decision")
	}
}

// ── resolveCartoVectorTile: cache-through, no graceful degradation ─────────

func TestResolveCartoVectorTile_CacheHitServesWithoutInvokingFetcher(t *testing.T) {
	cache := newTestTileCache(t)
	want := []byte("cached-mvt-bytes")
	if err := cache.put(cartoBasemapSource, 12, 3000, 2000, want, "application/x-protobuf"); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	fetcher := &fakeTileFetcher{responder: func(req *http.Request) (*http.Response, error) {
		t.Fatal("fetcher should not be invoked on a cache hit")
		return nil, nil
	}}

	data, contentType, err := resolveCartoVectorTile(cache, fetcher, 12, 3000, 2000)
	if err != nil {
		t.Fatalf("resolveCartoVectorTile: %v", err)
	}
	if !bytes.Equal(data, want) {
		t.Fatalf("expected cached bytes %q, got %q", want, data)
	}
	if contentType != "application/x-protobuf" {
		t.Fatalf("expected content-type application/x-protobuf, got %q", contentType)
	}
	if fetcher.callCount() != 0 {
		t.Fatalf("expected fetcher not to be called, got %d calls", fetcher.callCount())
	}
}

func TestResolveCartoVectorTile_CacheMissFetchesOnceThenCachesForRepeat(t *testing.T) {
	cache := newTestTileCache(t)
	upstreamBytes := []byte("fresh-mvt-bytes")
	fetcher := &fakeTileFetcher{responder: func(req *http.Request) (*http.Response, error) {
		return fakeUpstreamResponse(http.StatusOK, "application/x-protobuf", upstreamBytes), nil
	}}

	data, _, err := resolveCartoVectorTile(cache, fetcher, 11, 1500, 1000)
	if err != nil {
		t.Fatalf("resolveCartoVectorTile: %v", err)
	}
	if !bytes.Equal(data, upstreamBytes) {
		t.Fatalf("expected upstream bytes %q, got %q", upstreamBytes, data)
	}
	if fetcher.callCount() != 1 {
		t.Fatalf("expected 1 fetcher call, got %d", fetcher.callCount())
	}

	data2, _, err := resolveCartoVectorTile(cache, fetcher, 11, 1500, 1000)
	if err != nil {
		t.Fatalf("resolveCartoVectorTile (repeat): %v", err)
	}
	if !bytes.Equal(data2, upstreamBytes) {
		t.Fatalf("expected cached upstream bytes %q on repeat, got %q", upstreamBytes, data2)
	}
	if fetcher.callCount() != 1 {
		t.Fatalf("expected fetcher call count to stay at 1 on repeat, got %d", fetcher.callCount())
	}
}

// TestResolveCartoVectorTile_UpstreamFailureNeverDegradesToACoarserZoom is
// the regression guard for the correctness constraint this whole separate
// resolver exists for. Unlike resolveWorldImageryTile, a vector tile
// resolve must NEVER walk to a coarser zoom and serve/cache its bytes
// under the originally-requested key: MVT feature coordinates are
// tile-local, so a coarser tile's bytes served under a deeper key would
// put every coastline, island, and label at the wrong place and scale,
// confidently and silently wrong - worse than a blank tile.
func TestResolveCartoVectorTile_UpstreamFailureNeverDegradesToACoarserZoom(t *testing.T) {
	cache := newTestTileCache(t)

	// Seed a REAL, distinct tile at the parent (coarser) coordinate a
	// degrading resolver (like resolveWorldImageryTile) would have walked
	// to next. If resolveCartoVectorTile ever consulted this, the
	// assertions below would catch its bytes leaking into the child's key.
	parentBytes := []byte("z9-parent-bytes-must-never-appear-under-the-z10-key")
	if err := cache.put(cartoBasemapSource, 9, 250, 300, parentBytes, "application/x-protobuf"); err != nil {
		t.Fatalf("seed parent tile: %v", err)
	}

	fetcher := &fakeTileFetcher{responder: func(req *http.Request) (*http.Response, error) {
		return fakeUpstreamResponse(http.StatusNotFound, "text/plain", nil), nil
	}}

	// z=10, x=500, y=600 -> parent is (z=9, x=250, y=300), exactly what
	// was seeded above.
	if _, _, err := resolveCartoVectorTile(cache, fetcher, 10, 500, 600); err == nil {
		t.Fatal("expected an error on upstream failure with nothing cached at the requested key")
	}
	// Exactly one upstream call, at the originally-requested tile - no
	// parent-tile walk, no second/third/fourth-level attempt the way
	// resolveWorldImageryTile's maxDegradeLevels loop would make.
	if fetcher.callCount() != 1 {
		t.Fatalf("expected exactly 1 fetcher call (no degrade walk), got %d", fetcher.callCount())
	}

	// Nothing was cached under the requested key - proves the failure
	// wasn't quietly cached as a "success" (e.g. the parent's bytes, or a
	// synthesized empty tile).
	if _, _, ok, err := cache.get(cartoBasemapSource, 10, 500, 600); err != nil {
		t.Fatalf("cache.get after failed resolve: %v", err)
	} else if ok {
		t.Fatal("expected nothing cached under the requested key after an upstream failure")
	}

	// A repeat request must hit the upstream again and fail again - never
	// silently serve the parent tile's bytes just because they happen to
	// be sitting in the cache under a different key.
	if _, _, err := resolveCartoVectorTile(cache, fetcher, 10, 500, 600); err == nil {
		t.Fatal("expected the repeat request to fail again, not silently serve the parent tile's bytes")
	}
	if fetcher.callCount() != 2 {
		t.Fatalf("expected a second fetcher call on retry (nothing was cached), got %d", fetcher.callCount())
	}

	// The parent tile itself is untouched throughout.
	parentData, _, ok, err := cache.get(cartoBasemapSource, 9, 250, 300)
	if err != nil || !ok {
		t.Fatalf("parent tile should still be cached untouched: ok=%v err=%v", ok, err)
	}
	if !bytes.Equal(parentData, parentBytes) {
		t.Fatalf("parent tile bytes changed unexpectedly: got %q", parentData)
	}
}
