package main

import (
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// compressionMinLength is the response-body size (bytes, uncompressed)
// below which gzip is skipped. Under this, the gzip container's own framing
// overhead can make the "compressed" response larger than the original, and
// the CPU spent on both ends buys nothing - this is Echo's own MinLength
// knob (middleware/compress.go), just given a non-zero default here.
const compressionMinLength = 1024

// noCompressRoutePatterns are registered Echo route patterns - what
// echo.Context.Path() reports once a route has matched, e.g.
// "/api/world-imagery/:z/:x/:y", never a resolved "/api/world-imagery/9/1/2"
// - that must never be gzip-wrapped. Matching the pattern rather than the
// resolved request path means a param value can never accidentally dodge,
// or accidentally trigger, the skip.
//
// TestCompression_SkipListMatchesRegisteredRoutes (compression_test.go)
// cross-checks every entry here against buildAPIRoutes (main.go), so a
// route rename shows up as a test failure instead of a silently stale skip
// list.
var noCompressRoutePatterns = map[string]bool{
	// /api/stream and /api/logs/stream used to sit here too, on the theory
	// that gzip's own MinLength buffering (1KB, below) might withhold a
	// small SSE frame until enough of them piled up to cross that
	// threshold, stalling a stream whose whole point is low latency.
	// Reading the vendored gzip middleware (echo/middleware/compress.go)
	// settled that: its gzipResponseWriter.Flush() unconditionally forces
	// minLengthExceeded, writes whatever is buffered so far, and flushes
	// the underlying connection, regardless of MinLength — so a handler
	// that already calls response.Flush() after every write, as both of
	// these do, never sits behind that threshold. Only a handler that
	// wrote without ever flushing would need MinLength bypassed for it;
	// neither does.
	// TestCompression_SSEStreamIsGzippedAndStreamsBeforeHandlerReturns and
	// TestCompression_LogsStreamIsGzippedAndStreamsBeforeHandlerReturns
	// (compression_test.go) exercise both through the real middleware
	// stack: a client offering gzip gets Content-Encoding: gzip, and a
	// single small event decodes before the handler ever returns.
	//
	// assistant_handlers.go: postAssistantMessageHandler streams its reply
	// as SSE too, over the same connection that accepted the POST, and
	// stays skipped here for the same reason as the two routes above, now
	// more true than when this note was first written: since the backend
	// token-streaming follow-up ADR 0093 §5 named (assistant_run.go's run)
	// this route flushes a "delta" frame per streamed fragment of the
	// model's answer, genuinely small and genuinely frequent, on top of its
	// "status"/"retract"/"message"/"error" frames - not the whole-message-
	// at-once shape this comment used to describe. The gzipResponseWriter.
	// Flush() behaviour above should cover it identically, but that has not
	// been exercised through the real middleware stack the way
	// TestCompression_SSEStreamIsGzippedAndStreamsBeforeHandlerReturns does
	// for the other two, so it stays off this list rather than assumed
	// safe.
	"/api/assistant/conversations/:id/messages": true,

	// GET .../run (ADR 0105) replays a run's event log and then streams live
	// events the same way the POST above does, over its own connection -
	// same reasoning, same skip.
	"/api/assistant/conversations/:id/run": true,

	// WebSocket upgrade (grep for "websocket.Accept" turned up exactly this
	// one inbound upgrade; signalk_publish.go and signalk_stream.go dial
	// *outbound* websockets to SignalK and are not HTTP routes at all).
	// There is no HTTP response body to compress once the connection has
	// been upgraded, and wrapping the writer risks breaking whatever
	// hijack/upgrade path coder/websocket relies on.
	"/api/radar/spokes": true, // radar_spoke_relay.go: radarSpokeRelayHandler

	// Already-compressed raster tile proxy endpoints (tile_proxy.go and the
	// tile half of sat_charts.go): PNG/JPEG/WebP map tiles gain nothing from
	// a second compression pass, and it would spend CPU on every proxied
	// fetch for no smaller a response.
	//
	// basemap_proxy.go's style/tilejson/vector-tile/glyph routes used to
	// sit here too, on the same "already compressed" assumption - but they
	// aren't (backend perf audit Tier 3). fetchBasemapUpstream
	// (basemap_proxy.go) fetches through newWorldImageryHTTPClient's plain
	// *http.Client with a nil Transport, which defaults to http.
	// DefaultTransport: since fetchBasemapUpstream never sets its own
	// Accept-Encoding, that transport requests gzip and transparently
	// decompresses the response itself, stripping Content-Encoding before
	// this code ever sees it - confirmed against the real upstream, not
	// just read from the Go docs. fetchBasemapUpstream also never reads or
	// forwards a Content-Encoding header at all, so even the JSON/PBF
	// bytes this cached and served were genuinely plain, uncompressed, and
	// silently skipped by this exact skip list. Only basemap_proxy.go's
	// sprite route stayed off this list on purpose: it serves both
	// sprite.json (now compressed, like every other JSON/PBF route here)
	// and sprite.png (still skipped, but by the noCompressExtensions
	// ".png" check below, on the resolved request path - one route
	// pattern can't otherwise tell the two apart).
	"/api/world-imagery/:z/:x/:y":  true, // tile_proxy.go
	"/api/sat-charts/:id/:z/:x/:y": true, // sat_charts.go

	// documents_handlers.go: documentContentHandler serves a document's raw
	// bytes through http.ServeContent, which needs to answer Range requests
	// against the exact byte offsets of the underlying file (a PDF viewer's
	// partial fetch, a scrubbed audio/video position). Gzip-wrapping those
	// bytes would make Range's offsets meaningless, the same reasoning
	// compressionSkipper already applies to any request carrying a Range
	// header - this covers the initial, rangeless request for the same
	// route too, before a client has any ETag to make a ranged follow-up
	// against. Many of the served MIME types (PDF, JPEG, PNG, WebP) are
	// already-compressed anyway.
	"/api/documents/:id/content": true,

	// gshhg.go: gshhgCoastlineHandler gzips its own response once at
	// startup (gshhgCoastlineGzipped) and negotiates Content-Encoding
	// itself against the request's Accept-Encoding, exactly like the
	// gzip middleware this route is skipping would have. Recompressing an
	// already-gzipped body here would be wasted CPU on every request for a
	// payload that never changes (backend perf audit Tier 3: measured
	// +90-110ms on the boat before this fix).
	"/api/gshhg-coastline": true,
}

// noCompressExtensions covers static files served from the embedded SPA
// build (static.go): images and web fonts, which are already-compressed
// binary formats. Matched against the actual request path rather than a
// route pattern, because static.go registers one wildcard route ("/*") for
// every file in dist - the pattern alone can't tell a font apart from a JS
// bundle.
//
// woff2 is the audit's own example; plain .woff is included alongside it
// for the same reason (its internal per-glyph tables are already
// compressed) even though the current frontend build emits both formats
// side by side for older browsers.
var noCompressExtensions = []string{
	".png", ".jpg", ".jpeg", ".webp", ".gif", ".avif", ".ico",
	".woff", ".woff2",
}

// compressionSkipper decides which requests the gzip middleware leaves
// alone: SSE streams, the websocket upgrade, the tile/image/font proxy
// endpoints, already-compressed static file extensions, and any request
// that already carries a byte Range. A Range header is the defensive case:
// a range refers to offsets into the original representation, and
// compressing the response out from under it would make those offsets
// meaningless.
func compressionSkipper(c echo.Context) bool {
	if c.Request().Header.Get("Range") != "" {
		return true
	}
	if noCompressRoutePatterns[c.Path()] {
		return true
	}
	path := c.Request().URL.Path
	for _, ext := range noCompressExtensions {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}

// registerCompressionMiddleware wires gzip response compression ahead of
// every route registered on e - both the /api routes (registerAPIRoutes)
// and the embedded SPA (registerStaticHandler) - so it must be called
// before either. Order relative to the other e.Use() calls in main() does
// not otherwise matter: Echo nests global middleware by registration order
// regardless of when routes are added.
func registerCompressionMiddleware(e *echo.Echo) {
	e.Use(middleware.GzipWithConfig(middleware.GzipConfig{
		Skipper:   compressionSkipper,
		MinLength: compressionMinLength,
	}))
}
