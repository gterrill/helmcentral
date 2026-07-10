package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
	const wantTotal = 87892
	if resp.TotalTiles != wantTotal {
		t.Fatalf("expected computed totalTiles %d, got %d", wantTotal, resp.TotalTiles)
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
	if resp.TotalTiles != 2 {
		t.Fatalf("expected totalTiles 2 for this tiny bbox, got %d", resp.TotalTiles)
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
	if total != 2 {
		t.Fatalf("expected total 2, got %d", total)
	}
	if done != 2 {
		t.Fatalf("expected done 2, got %d", done)
	}
}

func TestNewWorldImageryHTTPClient_HasSixSecondTimeout(t *testing.T) {
	client := newWorldImageryHTTPClient()
	wantTimeout := 6 * time.Second
	if got := client.Timeout; got != wantTimeout {
		t.Fatalf("expected timeout %v, got %v", wantTimeout, got)
	}
}
