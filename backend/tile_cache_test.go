package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
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

	// Repeat identical request: should be served entirely from the cache
	// entry written under the ORIGINAL requested key, with no further
	// fetcher invocations.
	c2, rec2 := newTileProxyRequest("10", "500", "600")
	if err := handler(c2); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !bytes.Equal(rec2.Body.Bytes(), parentBytes) {
		t.Fatalf("expected cached degraded bytes %q on repeat, got %q", parentBytes, rec2.Body.Bytes())
	}
	if fetcher.callCount() != 2 {
		t.Fatalf("expected fetcher call count to stay at 2 on repeat request, got %d", fetcher.callCount())
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

	// Repeat: the blank result is now cached under the original key too, so
	// no further fetcher calls should occur.
	c2, rec2 := newTileProxyRequest("10", "500", "600")
	if err := handler(c2); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !bytes.Equal(rec2.Body.Bytes(), transparentPNG1x1) {
		t.Fatalf("expected cached transparent blank PNG on repeat, got %d bytes", len(rec2.Body.Bytes()))
	}
	if fetcher.callCount() != 5 {
		t.Fatalf("expected fetcher call count to stay at 5 on repeat request, got %d", fetcher.callCount())
	}
}

func newPrefetchRequest(t *testing.T, body any) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	e := echo.New()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/world-imagery/prefetch", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

func TestPrefetchWorldImageryHandler_RejectsOverCapBBoxWithComputedCount(t *testing.T) {
	cache := newTestTileCache(t)
	fetcher := &fakeTileFetcher{responder: func(req *http.Request) (*http.Response, error) {
		t.Fatal("fetcher should not be invoked for a rejected over-cap prefetch request")
		return nil, nil
	}}

	handler := prefetchWorldImageryHandler(cache, fetcher)
	c, rec := newPrefetchRequest(t, map[string]any{
		"west": -180.0, "south": -85.0, "east": 180.0, "north": 85.0,
		"minZoom": 0, "maxZoom": 8,
	})
	if err := handler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an over-cap bbox, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		TotalTiles int `json:"totalTiles"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	// 87892 Esri imagery tiles (z0-8 over nearly the whole world) plus an
	// identical 87892 Carto basemap tiles - the requested maxZoom (8) is
	// already under basemapPrefetchMaxZoom (14), so the basemap range is
	// the exact same [minZoom,maxZoom] as imagery, over the exact same
	// bbox, which is why the two halves are equal rather than the basemap
	// half being capped smaller.
	const wantTotal = 87892 * 2
	if resp.TotalTiles != wantTotal {
		t.Fatalf("expected computed combined totalTiles %d, got %d", wantTotal, resp.TotalTiles)
	}
}

func TestPrefetchWorldImageryJob_RunsToCompletionWithCorrectCounts(t *testing.T) {
	cache := newTestTileCache(t)
	fetcher := &fakeTileFetcher{responder: func(req *http.Request) (*http.Response, error) {
		return fakeUpstreamResponse(http.StatusOK, "image/jpeg", []byte("tile-bytes")), nil
	}}

	prefetchHandler := prefetchWorldImageryHandler(cache, fetcher)
	c, rec := newPrefetchRequest(t, map[string]any{
		"west": 150.0, "south": -25.1, "east": 150.2, "north": -24.9,
		"minZoom": 10, "maxZoom": 10,
	})
	if err := prefetchHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		JobID      string `json:"jobId"`
		TotalTiles int    `json:"totalTiles"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	// 2 Esri imagery tiles + 2 Carto basemap tiles (z10 is under the z14
	// basemap cap, so this tiny bbox's basemap range is identical to its
	// imagery range) = 4 total. The combined total, not just the imagery
	// half, is what the frontend's progress pill reads.
	if resp.TotalTiles != 4 {
		t.Fatalf("expected combined totalTiles 4 for this tiny bbox, got %d", resp.TotalTiles)
	}
	if resp.JobID == "" {
		t.Fatal("expected a non-empty jobId")
	}

	statusHandler := prefetchStatusHandler()
	deadline := time.Now().Add(5 * time.Second)
	var total, done int
	var complete bool
	for time.Now().Before(deadline) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/api/world-imagery/prefetch/"+resp.JobID, nil)
		srec := httptest.NewRecorder()
		sc := e.NewContext(req, srec)
		sc.SetParamNames("jobId")
		sc.SetParamValues(resp.JobID)
		if err := statusHandler(sc); err != nil {
			t.Fatalf("status handler returned error: %v", err)
		}
		var statusResp struct {
			Total    int  `json:"total"`
			Done     int  `json:"done"`
			Complete bool `json:"complete"`
		}
		if err := json.Unmarshal(srec.Body.Bytes(), &statusResp); err != nil {
			t.Fatalf("unmarshal status response: %v", err)
		}
		total, done, complete = statusResp.Total, statusResp.Done, statusResp.Complete
		if complete {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !complete {
		t.Fatal("expected prefetch job to complete within the deadline")
	}
	if total != 4 {
		t.Fatalf("expected combined total 4, got %d", total)
	}
	if done != 4 {
		t.Fatalf("expected combined done 4, got %d", done)
	}
}

func TestNewWorldImageryHTTPClient_HasSixSecondTimeout(t *testing.T) {
	client := newWorldImageryHTTPClient()
	wantTimeout := 6 * time.Second
	if got := client.Timeout; got != wantTimeout {
		t.Fatalf("expected timeout %v, got %v", wantTimeout, got)
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

// ── combined imagery + basemap prefetch ─────────────────────────────────────

func TestPrefetchTileCoords_CapsBasemapAtZ14ButNotImagery(t *testing.T) {
	west, south, east, north := 150.0, -25.1, 150.2, -24.9

	tiles := prefetchTileCoords(west, south, east, north, 10, 20)

	imageryZooms, basemapZooms := map[int]bool{}, map[int]bool{}
	for _, tl := range tiles {
		switch tl.kind {
		case tileKindImagery:
			imageryZooms[tl.z] = true
		case tileKindBasemap:
			basemapZooms[tl.z] = true
		default:
			t.Fatalf("unexpected tileKind %d", tl.kind)
		}
	}

	for z := 10; z <= 20; z++ {
		if !imageryZooms[z] {
			t.Errorf("expected an imagery tile at z=%d, found none", z)
		}
	}
	for z := 10; z <= 14; z++ {
		if !basemapZooms[z] {
			t.Errorf("expected a basemap tile at z=%d, found none", z)
		}
	}
	for z := 15; z <= 20; z++ {
		if basemapZooms[z] {
			t.Errorf("expected NO basemap tile at z=%d (past Carto's z14 maxzoom), found one", z)
		}
	}

	if wantCount := countPrefetchTiles(west, south, east, north, 10, 20); len(tiles) != wantCount {
		t.Fatalf("prefetchTileCoords returned %d tiles, countPrefetchTiles said %d", len(tiles), wantCount)
	}
}

func TestPrefetchTileCoords_NoBasemapTilesWhenMinZoomPastCartoMaxZoom(t *testing.T) {
	// Requesting only z16-18 (entirely past Carto's z14 basemap maxzoom)
	// must enqueue imagery tiles but ZERO basemap tiles - not a
	// clamped-down z14 tile mislabeled as z16/17/18, which would be the
	// same coordinate-mismatch hazard the resolver-level tests above
	// guard against, just introduced one layer up.
	west, south, east, north := 150.0, -25.1, 150.2, -24.9
	tiles := prefetchTileCoords(west, south, east, north, 16, 18)

	imageryCount, basemapCount := 0, 0
	for _, tl := range tiles {
		if tl.kind == tileKindBasemap {
			basemapCount++
		} else {
			imageryCount++
		}
	}
	if imageryCount == 0 {
		t.Fatal("expected imagery tiles for z16-18")
	}
	if basemapCount != 0 {
		t.Fatalf("expected zero basemap tiles when minZoom(16) is past cartoBasemapMaxZoom(14), got %d", basemapCount)
	}
}

// TestRunPrefetchJob_DispatchesEachKindToItsOwnResolverAndSource proves the
// worker pool routes tileKindImagery through resolveWorldImageryTile (Esri)
// and tileKindBasemap through resolveCartoVectorTile (Carto) - never mixed
// up, which is the one dispatch bug that would reproduce the MVT
// coordinate-mismatch hazard inside the prefetch pool.
func TestRunPrefetchJob_DispatchesEachKindToItsOwnResolverAndSource(t *testing.T) {
	cache := newTestTileCache(t)

	imageryBytes := []byte("esri-imagery-bytes")
	basemapBytes := []byte("carto-vector-bytes")
	fetcher := &fakeTileFetcher{responder: func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Host, "arcgisonline.com"):
			return fakeUpstreamResponse(http.StatusOK, "image/jpeg", imageryBytes), nil
		case strings.Contains(req.URL.Host, "cartocdn.com"):
			return fakeUpstreamResponse(http.StatusOK, "application/x-protobuf", basemapBytes), nil
		}
		t.Fatalf("unexpected upstream host: %s", req.URL.Host)
		return nil, nil
	}}

	tiles := []tileCoord{
		{kind: tileKindImagery, z: 8, x: 10, y: 20},
		{kind: tileKindBasemap, z: 8, x: 10, y: 20},
	}
	job := &prefetchJob{ID: "test-job", Total: len(tiles)}
	runPrefetchJob(job, cache, fetcher, tiles)

	if _, _, complete := job.snapshot(); !complete {
		t.Fatal("expected job to complete")
	}

	imageryData, _, ok, err := cache.get(worldImagerySource, 8, 10, 20)
	if err != nil || !ok {
		t.Fatalf("expected imagery tile cached under worldImagerySource: ok=%v err=%v", ok, err)
	}
	if !bytes.Equal(imageryData, imageryBytes) {
		t.Fatalf("expected imagery bytes %q, got %q", imageryBytes, imageryData)
	}

	basemapData, _, ok, err := cache.get(cartoBasemapSource, 8, 10, 20)
	if err != nil || !ok {
		t.Fatalf("expected basemap tile cached under cartoBasemapSource: ok=%v err=%v", ok, err)
	}
	if !bytes.Equal(basemapData, basemapBytes) {
		t.Fatalf("expected basemap bytes %q, got %q", basemapBytes, basemapData)
	}
}
