package main

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/labstack/echo/v4"
)

// newCompressedTestEcho builds an Echo instance with exactly the compression
// middleware main.go registers in production, nothing else. Individual
// tests then register only the routes they need against it, the same
// minimal-dependency style static_routing_test.go and
// vessel_state_stream_test.go already use.
func newCompressedTestEcho() *echo.Echo {
	e := echo.New()
	registerCompressionMiddleware(e)
	return e
}

// TestCompression_LargeJSONRouteIsGzipped exercises the real, dependency-free
// gshhg-coastline handler - a ~1.5MB embedded JSON blob and exactly one of
// the oversized payloads the performance audit named - and checks a client
// that offers gzip gets back a smaller, correctly round-tripping body.
func TestCompression_LargeJSONRouteIsGzipped(t *testing.T) {
	e := newCompressedTestEcho()
	e.GET("/api/gshhg-coastline", gshhgCoastlineHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/gshhg-coastline", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if rec.Body.Len() >= len(gshhgCoastlineJSON) {
		t.Fatalf("compressed body (%d bytes) is not smaller than the source (%d bytes)", rec.Body.Len(), len(gshhgCoastlineJSON))
	}

	gz, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer gz.Close()
	decoded, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("reading gzip body: %v", err)
	}
	if string(decoded) != string(gshhgCoastlineJSON) {
		t.Fatalf("decompressed body does not match the source coastline JSON")
	}
}

// TestCompression_JSAssetIsGzipped covers the other half of the audit
// finding: the embedded SPA's hashed JS bundles, served through
// registerStaticHandlerFS's http.FileServer wrapping, must also come back
// compressed for a client that asks for it.
func TestCompression_JSAssetIsGzipped(t *testing.T) {
	e := newCompressedTestEcho()
	jsBody := "console.log(\"" + strings.Repeat("x", 4096) + "\");"
	dist := fstest.MapFS{
		"index.html":          {Data: []byte(`<!doctype html><div id="root"></div>`)},
		"assets/index-abc.js": {Data: []byte(jsBody)},
	}
	registerStaticHandlerFS(e, dist)

	req := httptest.NewRequest(http.MethodGet, "/assets/index-abc.js", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}

	gz, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer gz.Close()
	decoded, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("reading gzip body: %v", err)
	}
	if string(decoded) != jsBody {
		t.Fatalf("decompressed asset body does not match the source JS")
	}
}

// TestCompression_SmallResponseStaysUncompressed guards the MinLength
// threshold: a tiny JSON body should not pay the gzip framing overhead or
// carry a Content-Encoding header nobody benefits from.
func TestCompression_SmallResponseStaysUncompressed(t *testing.T) {
	e := newCompressedTestEcho()
	e.GET("/api/health", healthCheck)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty for a response under MinLength", got)
	}
}

// TestCompression_SSEStreamIsGzippedAndStreamsBeforeHandlerReturns is the
// regression guard for Tier 1 #4 taking /api/stream off the gzip skip list:
// a client that offers gzip gets Content-Encoding: gzip, and — the actual
// risk the audit called out — a single small SSE frame can still be read
// and decoded while telemetryStream is still running. telemetryStream loops
// until the request context is cancelled and never returns on its own, so
// decoding a frame at all is only possible if gzip forwarded it immediately
// rather than withholding it behind MinLength until enough frames piled up
// to cross that threshold (see compression.go's noCompressRoutePatterns
// comment for what reading the vendored middleware found).
func TestCompression_SSEStreamIsGzippedAndStreamsBeforeHandlerReturns(t *testing.T) {
	withGlobalSnapshot(t, newSignalKSnapshot())
	// An empty snapshot drives GNSS validation critical, and that state
	// latches in module-level globals until several good samples clear it.
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	e := newCompressedTestEcho()
	e.GET("/api/stream", telemetryStream)
	server := httptest.NewServer(e)
	t.Cleanup(server.Close)

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/stream", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/stream: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}

	if err := waitForDecodedSSEDataLine(resp.Body); err != nil {
		t.Fatalf("reading/decoding a gzip SSE frame: %v", err)
	}
}

// TestCompression_LogsStreamIsGzippedAndStreamsBeforeHandlerReturns is the
// same regression guard as the test above, for /api/logs/stream — the other
// route Tier 1 #4 took off the skip list. logsStreamHandler flushes its
// backlog replay in one batch before entering its event loop
// (log_handlers.go), so the pre-seeded log entry below is what this test
// waits to see decoded.
func TestCompression_LogsStreamIsGzippedAndStreamsBeforeHandlerReturns(t *testing.T) {
	orig := globalLogBuffer
	t.Cleanup(func() { globalLogBuffer = orig })
	buf := newLogBuffer(10)
	buf.write("compression test log")
	globalLogBuffer = buf

	e := newCompressedTestEcho()
	e.GET("/api/logs/stream", logsStreamHandler)
	server := httptest.NewServer(e)
	t.Cleanup(server.Close)

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/logs/stream", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/logs/stream: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}

	if err := waitForDecodedSSEDataLine(resp.Body); err != nil {
		t.Fatalf("reading/decoding a gzip SSE frame: %v", err)
	}
}

// waitForDecodedSSEDataLine reads a gzip-encoded SSE body until it can
// decode one "data: " line, or times out. A timeout, rather than the gzip
// reader simply erroring, is what an actual stall looks like: the
// goroutine below blocks inside gzip.NewReader/scanner.Scan waiting on
// bytes that gzip's own buffering never released, so the select is the
// part of this helper that is actually load-bearing.
func waitForDecodedSSEDataLine(body io.Reader) error {
	result := make(chan error, 1)
	go func() {
		gz, err := gzip.NewReader(body)
		if err != nil {
			result <- fmt.Errorf("gzip.NewReader: %w", err)
			return
		}
		defer gz.Close()
		scanner := bufio.NewScanner(gz)
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), "data: ") {
				result <- nil
				return
			}
		}
		if err := scanner.Err(); err != nil {
			result <- fmt.Errorf("scanning decoded body: %w", err)
			return
		}
		result <- fmt.Errorf("stream ended with no data line decoded")
	}()

	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		return fmt.Errorf("no decodable SSE frame within 5s; gzip's MinLength buffering may be stalling the stream")
	}
}

// TestCompression_ETag304StaysCorrectUnderGzip is the regression guard for
// the weak-vs-strong ETag interaction: respondJSONWithETag (weather_tide.go)
// already emits a weak validator (W/"..."), which is what HTTP requires once
// a response can be served in more than one content-coding, so a client
// replaying that same ETag must still get a 304 with no body once gzip sits
// in front of it.
func TestCompression_ETag304StaysCorrectUnderGzip(t *testing.T) {
	e := newCompressedTestEcho()
	payload := map[string]string{"padding": strings.Repeat("a", 2048)}
	e.GET("/test/etag", func(c echo.Context) error {
		etag, err := weakETagForJSON(payload)
		if err != nil {
			return err
		}
		return respondJSONWithETag(c, http.StatusOK, etag, payload)
	})

	req := httptest.NewRequest(http.MethodGet, "/test/etag", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want 200", rec.Code)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" || !strings.HasPrefix(etag, `W/"`) {
		t.Fatalf("ETag = %q, want a weak validator", etag)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("first response Content-Encoding = %q, want gzip", got)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/test/etag", nil)
	req2.Header.Set("Accept-Encoding", "gzip")
	req2.Header.Set("If-None-Match", etag)
	rec2 := httptest.NewRecorder()
	e.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusNotModified {
		t.Fatalf("second request status = %d, want 304", rec2.Code)
	}
	if rec2.Body.Len() != 0 {
		t.Fatalf("304 response body = %d bytes, want 0", rec2.Body.Len())
	}
	if got := rec2.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("304 Content-Encoding = %q, want empty", got)
	}
}

// TestCompression_SkipperExcludesStreamingAndBinaryRoutes is a direct,
// white-box check of compressionSkipper's route table for the paths that
// are impractical to exercise end-to-end in a fast unit test (the radar
// websocket upgrade, and the tile/font proxy endpoints that need a live
// upstream or a fake tileCache to actually answer). Echo sets c.Path() to
// the exact registered pattern once a route has matched (e.g.
// "/api/world-imagery/:z/:x/:y", never the resolved "/api/world-imagery/9/1/2"),
// so matching against the pattern here is exactly what compressionSkipper
// sees in production.
func TestCompression_SkipperExcludesStreamingAndBinaryRoutes(t *testing.T) {
	e := echo.New()
	cases := []struct {
		path string
		want bool
	}{
		// Off the skip list since Tier 1 #4: both flush after every write,
		// so gzip's MinLength buffering never withholds a frame (see
		// noCompressRoutePatterns' comment in compression.go).
		{"/api/stream", false},
		{"/api/logs/stream", false},
		{"/api/assistant/conversations/:id/messages", true},
		{"/api/radar/spokes", true},
		{"/api/world-imagery/:z/:x/:y", true},
		{"/api/sat-charts/:id/:z/:x/:y", true},
		{"/api/basemap/style/:name", true},
		{"/api/basemap/tilejson", true},
		{"/api/basemap/tiles/:z/:x/:y", true},
		{"/api/basemap/fonts/:fontstack/:range", true},
		{"/api/basemap/sprite/:name", true},
		{"/api/weather-forecast", false},
		{"/api/wave-forecast", false},
		{"/api/gshhg-coastline", false},
		{"/api/vessel-state", false},
		{"/api/assistant/conversations/:id", false},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/irrelevant", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath(tc.path)
		if got := compressionSkipper(c); got != tc.want {
			t.Errorf("compressionSkipper(path=%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestCompression_SkipListMatchesRegisteredRoutes guards against drift: if
// a route in noCompressRoutePatterns is ever renamed in buildAPIRoutes, this
// fails loudly instead of the skip list silently going stale and either
// gzip-wrapping a websocket upgrade or leaving a real JSON route
// uncompressed.
func TestCompression_SkipListMatchesRegisteredRoutes(t *testing.T) {
	sessions := newTestSessionStore(t)
	routes := buildAPIRoutes(sessions, newWorldImageryHTTPClient())
	registered := make(map[string]bool, len(routes))
	for _, r := range routes {
		registered[r.Path] = true
	}
	for pattern := range noCompressRoutePatterns {
		if !registered[pattern] {
			t.Errorf("noCompressRoutePatterns contains %q, which is not a currently registered API route", pattern)
		}
	}
}

// TestCompression_SkipperExcludesStaticBinaryExtensions covers the other
// half of the Skipper: static.go serves every embedded file through one
// wildcard route ("/*"), so images and fonts can only be told apart from JS
// and CSS by the actual request path, not the route pattern.
func TestCompression_SkipperExcludesStaticBinaryExtensions(t *testing.T) {
	e := echo.New()
	cases := []struct {
		path string
		want bool
	}{
		{"/assets/geist-sans-latin-400-normal-abc.woff2", true},
		{"/assets/geist-sans-latin-400-normal-abc.woff", true},
		{"/icons/icon-192.png", true},
		{"/icons/icon.svg", false},
		{"/assets/index-abc.js", false},
		{"/assets/map-vendor-abc.css", false},
		{"/", false},
		{"/anchor", false},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/*")
		if got := compressionSkipper(c); got != tc.want {
			t.Errorf("compressionSkipper(request path=%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestCompression_SkipperExcludesRangeRequests is the defensive edge case:
// a byte Range refers to offsets in the original, uncompressed
// representation. Serving a ranged response gzip-encoded would make the
// range meaningless, so any Range request is left alone regardless of path.
func TestCompression_SkipperExcludesRangeRequests(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/assets/index-abc.js", nil)
	req.Header.Set("Range", "bytes=0-100")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/*")
	if !compressionSkipper(c) {
		t.Error("compressionSkipper should skip a request that carries a Range header")
	}
}
