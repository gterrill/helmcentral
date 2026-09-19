package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// A trimmed stand-in for Carto's real style document, carrying exactly the
// structure rewriteCartoStyle cares about plus a sample of the layer ids
// the frontend pins by name. Captured shape (not content) from the live
// positron-gl-style document: one vector source keyed "carto", a glyphs
// template, a sprite base URL, and a layers array whose first entry is
// "background".
const fakeCartoStyleJSON = `{
	"version": 8,
	"name": "Positron",
	"sources": {"carto": {"type": "vector", "url": "https://tiles.basemaps.cartocdn.com/vector/carto.streets/v1/tiles.json"}},
	"glyphs": "https://tiles.basemaps.cartocdn.com/fonts/{fontstack}/{range}.pbf",
	"sprite": "https://tiles.basemaps.cartocdn.com/gl/positron-gl-style/sprite",
	"layers": [
		{"id": "background", "type": "background"},
		{"id": "landcover", "type": "fill", "source": "carto", "source-layer": "globallandcover"},
		{"id": "water", "type": "fill", "source": "carto", "source-layer": "water"},
		{"id": "watername_lake", "type": "symbol", "source": "carto", "source-layer": "water_name"},
		{"id": "place_town", "type": "symbol", "source": "carto", "source-layer": "place"},
		{"id": "place_city_dot_z7", "type": "symbol", "source": "carto", "source-layer": "place"}
	]
}`

const fakeCartoTileJSON = `{
	"tilejson": "2.2.0",
	"tiles": ["https://tiles-a.basemaps.cartocdn.com/vectortiles/carto.streets/v1/{z}/{x}/{y}.mvt"],
	"minzoom": 0,
	"maxzoom": 14,
	"attribution": "&copy; CARTO, &copy; OpenStreetMap contributors",
	"vector_layers": [{"id": "place"}, {"id": "water_name"}]
}`

// newBasemapTestContext builds an echo context for one basemap handler
// call, mirroring the param-name/param-value setup the other handler tests
// in this package use.
func newBasemapTestContext(target string, paramNames, paramValues []string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if len(paramNames) > 0 {
		c.SetParamNames(paramNames...)
		c.SetParamValues(paramValues...)
	}
	return c, rec
}

// respondingFetcher returns a fakeTileFetcher that serves body/contentType
// for any request whose URL contains match, and 404s everything else - so a
// test fails loudly if a handler reaches for an upstream URL it shouldn't.
func respondingFetcher(match, contentType, body string) *fakeTileFetcher {
	return &fakeTileFetcher{
		responder: func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.String(), match) {
				return fakeUpstreamResponse(http.StatusOK, contentType, []byte(body)), nil
			}
			return fakeUpstreamResponse(http.StatusNotFound, "text/plain", []byte("not found")), nil
		},
	}
}

func failingFetcher() *fakeTileFetcher {
	return &fakeTileFetcher{
		responder: func(*http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("simulated uplink outage")
		},
	}
}

// The whole reason the style handler decodes and re-encodes JSON rather
// than piping upstream bytes through: the three URL fields must move to
// this proxy, and everything the frontend pins by name must survive. A
// regression in either direction silently breaks the maps - a stale URL
// breaks offline, a renamed source/layer breaks ADR 0066's label layers and
// the hybrid-satellite restyling in route-planner-map.tsx.
func TestBasemapStyleHandler_RewritesURLsAndPreservesSourceAndLayerIDs(t *testing.T) {
	cache := newTestTileCache(t)
	fetcher := respondingFetcher("positron-gl-style/style.json", "application/json", fakeCartoStyleJSON)

	c, rec := newBasemapTestContext("/api/basemap/style/positron", []string{"name"}, []string{"positron"})
	if err := basemapStyleHandler(cache, fetcher)(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body %s)", rec.Code, rec.Body.String())
	}

	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	sources, ok := doc["sources"].(map[string]any)
	if !ok {
		t.Fatal("rewritten style lost its sources object")
	}
	carto, ok := sources["carto"].(map[string]any)
	if !ok {
		t.Fatalf(`rewritten style lost the "carto" source id that map-place-labels.tsx pins; sources = %v`, sources)
	}
	// Served absolute; the cached copy stays relative. See
	// TestBasemapStyleHandler_ServesAbsoluteSpriteURLBuiltFromRequest.
	if got := carto["url"]; got != "http://example.com/api/basemap/tilejson" {
		t.Errorf("sources.carto.url = %v, want the absolute same-origin URL", got)
	}
	if got := doc["glyphs"]; got != "http://example.com/api/basemap/fonts/{fontstack}/{range}.pbf" {
		t.Errorf("glyphs = %v, want the absolute same-origin template", got)
	}
	// Served absolute (MapLibre rejects a relative sprite URL); the cached
	// copy stays relative. See TestBasemapStyleHandler_ServesAbsoluteSpriteURLBuiltFromRequest.
	if got := doc["sprite"]; got != "http://example.com/api/basemap/sprite/positron" {
		t.Errorf("sprite = %v, want an absolute same-origin URL", got)
	}

	layers, ok := doc["layers"].([]any)
	if !ok {
		t.Fatal("rewritten style lost its layers array")
	}
	ids := make(map[string]bool, len(layers))
	for _, l := range layers {
		if m, ok := l.(map[string]any); ok {
			if id, ok := m["id"].(string); ok {
				ids[id] = true
			}
		}
	}
	// route-planner-map.tsx pins these by name (HYBRID_HIDDEN_LAYER_IDS,
	// HYBRID_LABEL_LAYER_IDS, BASE_STYLE_FIRST_LAYER_ID).
	for _, want := range []string{"background", "landcover", "water", "watername_lake", "place_town", "place_city_dot_z7"} {
		if !ids[want] {
			t.Errorf("rewritten style dropped layer id %q", want)
		}
	}
	if first, ok := layers[0].(map[string]any); !ok || first["id"] != "background" {
		t.Errorf(`first layer must stay "background" (BASE_STYLE_FIRST_LAYER_ID), got %v`, layers[0])
	}
}

// The cached copy has to be the rewritten document, not the upstream one:
// an offline cold start serves straight from cache, and an upstream-URL
// style cached there would send the browser back to the CDN it cannot
// reach.
func TestBasemapStyleHandler_CachesRewrittenBytesNotUpstreamBytes(t *testing.T) {
	cache := newTestTileCache(t)
	fetcher := respondingFetcher("positron-gl-style/style.json", "application/json", fakeCartoStyleJSON)

	c, _ := newBasemapTestContext("/api/basemap/style/positron", []string{"name"}, []string{"positron"})
	if err := basemapStyleHandler(cache, fetcher)(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	data, _, ok, err := cache.getBasemapAsset("/api/basemap/style/positron")
	if err != nil {
		t.Fatalf("reading cached asset: %v", err)
	}
	if !ok {
		t.Fatal("style was not cached at all")
	}
	if strings.Contains(string(data), "basemaps.cartocdn.com") {
		t.Errorf("cached style still points at the CDN, so an offline start would fail:\n%s", data)
	}
	if !strings.Contains(string(data), "/api/basemap/tilejson") {
		t.Errorf("cached style is missing the rewritten tilejson URL:\n%s", data)
	}

	// Second call must be served from that cache without touching upstream.
	before := fetcher.callCount()
	c2, rec2 := newBasemapTestContext("/api/basemap/style/positron", []string{"name"}, []string{"positron"})
	if err := basemapStyleHandler(cache, fetcher)(c2); err != nil {
		t.Fatalf("handler returned error on repeat: %v", err)
	}
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 on repeat, got %d", rec2.Code)
	}
	if got := fetcher.callCount(); got != before {
		t.Errorf("cache hit still called upstream: %d calls before, %d after", before, got)
	}
}

func TestBasemapStyleHandler_UpstreamFailureWithEmptyCacheIs502(t *testing.T) {
	cache := newTestTileCache(t)

	c, rec := newBasemapTestContext("/api/basemap/style/positron", []string{"name"}, []string{"positron"})
	if err := basemapStyleHandler(cache, failingFetcher())(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d (body %s)", rec.Code, rec.Body.String())
	}
	// Fail-fast: the failure must be reported, never papered over with an
	// empty-but-valid style that would leave the map silently blank.
	if !strings.Contains(rec.Body.String(), "basemap style unavailable") {
		t.Errorf("502 body should name the failure, got %s", rec.Body.String())
	}
	if _, _, ok, err := cache.getBasemapAsset("/api/basemap/style/positron"); err != nil {
		t.Fatalf("reading cached asset: %v", err)
	} else if ok {
		t.Error("a failed fetch must not leave anything in the cache")
	}
}

// :name is interpolated into nothing - it only ever selects from a fixed
// map. Anything else must be rejected before an upstream request is built.
func TestBasemapStyleHandler_RejectsNamesOutsideAllowlist(t *testing.T) {
	for _, name := range []string{"bogus", "", "../../etc/passwd", "positron-gl-style", "http://evil.test/x"} {
		t.Run(name, func(t *testing.T) {
			cache := newTestTileCache(t)
			fetcher := respondingFetcher("nothing-matches-this", "application/json", "{}")

			c, rec := newBasemapTestContext("/api/basemap/style/x", []string{"name"}, []string{name})
			if err := basemapStyleHandler(cache, fetcher)(c); err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400 for %q, got %d", name, rec.Code)
			}
			if fetcher.callCount() != 0 {
				t.Errorf("SSRF guard leaked: %q reached upstream", name)
			}
		})
	}
}

func TestBasemapTileJSONHandler_RewritesTilesArrayAndKeepsZoomRange(t *testing.T) {
	cache := newTestTileCache(t)
	fetcher := respondingFetcher("carto.streets/v1/tiles.json", "application/json", fakeCartoTileJSON)

	c, rec := newBasemapTestContext("/api/basemap/tilejson", nil, nil)
	if err := basemapTileJSONHandler(cache, fetcher)(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body %s)", rec.Code, rec.Body.String())
	}

	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	// Absolute, not relative: MapLibre fetches vector tiles from a Web
	// Worker, which has no document base URL, so a relative template fails
	// with "Failed to construct 'Request'" and not one tile ever loads.
	tiles, ok := doc["tiles"].([]any)
	if !ok || len(tiles) != 1 || tiles[0] != "http://example.com/api/basemap/tiles/{z}/{x}/{y}" {
		t.Errorf("tiles = %v, want the single absolute same-origin template", doc["tiles"])
	}
	// maxzoom is what makes MapLibre overzoom client-side past 14 rather
	// than asking this proxy for tiles Carto does not have.
	if got := doc["maxzoom"]; got != float64(14) {
		t.Errorf("maxzoom = %v, want 14", got)
	}
	if got := doc["minzoom"]; got != float64(0) {
		t.Errorf("minzoom = %v, want 0", got)
	}
	if got, _ := doc["attribution"].(string); !strings.Contains(got, "CARTO") {
		t.Errorf("attribution must survive the rewrite, got %q", got)
	}
}

func TestBasemapTileJSONHandler_UpstreamFailureWithEmptyCacheIs502(t *testing.T) {
	cache := newTestTileCache(t)

	c, rec := newBasemapTestContext("/api/basemap/tilejson", nil, nil)
	if err := basemapTileJSONHandler(cache, failingFetcher())(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d (body %s)", rec.Code, rec.Body.String())
	}
}

// MapLibre substitutes {fontstack} itself, producing a single path segment
// carrying commas and spaces. Echo hands it over percent-decoded, so the
// handler has to re-encode for the upstream call - and must never let the
// value smuggle a "/" into the upstream path.
func TestBasemapFontsHandler_ReEncodesFontstackForUpstream(t *testing.T) {
	cache := newTestTileCache(t)
	const stack = "Montserrat Medium,Open Sans Bold,Noto Sans Regular"

	var gotURL string
	fetcher := &fakeTileFetcher{
		responder: func(req *http.Request) (*http.Response, error) {
			gotURL = req.URL.String()
			return fakeUpstreamResponse(http.StatusOK, "application/x-protobuf", []byte("glyph-bytes")), nil
		},
	}

	c, rec := newBasemapTestContext(
		"/api/basemap/fonts/x/0-255.pbf",
		[]string{"fontstack", "range"},
		[]string{stack, "0-255.pbf"},
	)
	if err := basemapFontsHandler(cache, fetcher)(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body %s)", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "glyph-bytes" {
		t.Errorf("glyph body = %q, want the upstream bytes verbatim", rec.Body.String())
	}

	if !strings.HasPrefix(gotURL, cartoGlyphsUpstreamHost+"/") {
		t.Fatalf("upstream URL escaped the glyphs host: %s", gotURL)
	}
	if !strings.HasSuffix(gotURL, "/0-255.pbf") {
		t.Errorf("upstream URL lost the range suffix: %s", gotURL)
	}
	// Spaces and commas must be percent-encoded, and the stack must remain
	// exactly one path segment.
	segment := strings.TrimSuffix(strings.TrimPrefix(gotURL, cartoGlyphsUpstreamHost+"/"), "/0-255.pbf")
	if strings.Contains(segment, " ") || strings.Contains(segment, "/") {
		t.Errorf("fontstack segment not safely encoded: %q", segment)
	}
	if !strings.Contains(segment, "%20") || !strings.Contains(segment, "%2C") {
		t.Errorf("fontstack segment missing percent-encoding for space/comma: %q", segment)
	}

	// Cached under the decoded path, and served from there next time.
	before := fetcher.callCount()
	c2, rec2 := newBasemapTestContext(
		"/api/basemap/fonts/x/0-255.pbf",
		[]string{"fontstack", "range"},
		[]string{stack, "0-255.pbf"},
	)
	if err := basemapFontsHandler(cache, fetcher)(c2); err != nil {
		t.Fatalf("handler returned error on repeat: %v", err)
	}
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 on repeat, got %d", rec2.Code)
	}
	if got := fetcher.callCount(); got != before {
		t.Errorf("glyph cache hit still called upstream: %d before, %d after", before, got)
	}
}

func TestBasemapFontsHandler_RejectsRangeWithoutPbfSuffix(t *testing.T) {
	cache := newTestTileCache(t)
	fetcher := respondingFetcher("nothing", "application/x-protobuf", "x")

	c, rec := newBasemapTestContext(
		"/api/basemap/fonts/x/0-255",
		[]string{"fontstack", "range"},
		[]string{"Montserrat Medium", "0-255"},
	)
	if err := basemapFontsHandler(cache, fetcher)(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
	if fetcher.callCount() != 0 {
		t.Error("malformed range reached upstream")
	}
}

func TestBasemapSpriteHandler_ServesAllowlistedNamesAndRejectsOthers(t *testing.T) {
	allowed := []string{
		"positron.json", "positron.png", "positron@2x.json", "positron@2x.png",
		"dark-matter.json", "dark-matter.png", "dark-matter@2x.json", "dark-matter@2x.png",
	}
	for _, name := range allowed {
		t.Run("allow/"+name, func(t *testing.T) {
			cache := newTestTileCache(t)
			fetcher := respondingFetcher("sprite", "image/png", "sprite-bytes")

			c, rec := newBasemapTestContext("/api/basemap/sprite/"+name, []string{"name"}, []string{name})
			if err := basemapSpriteHandler(cache, fetcher)(c); err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if rec.Code != http.StatusOK {
				t.Errorf("expected 200 for %q, got %d", name, rec.Code)
			}
		})
	}

	for _, name := range []string{"positron", "positron.svg", "", "../style.json", "evil@2x.png"} {
		t.Run("reject/"+name, func(t *testing.T) {
			cache := newTestTileCache(t)
			fetcher := respondingFetcher("sprite", "image/png", "sprite-bytes")

			c, rec := newBasemapTestContext("/api/basemap/sprite/x", []string{"name"}, []string{name})
			if err := basemapSpriteHandler(cache, fetcher)(c); err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400 for %q, got %d", name, rec.Code)
			}
			if fetcher.callCount() != 0 {
				t.Errorf("SSRF guard leaked: %q reached upstream", name)
			}
		})
	}
}

func TestBasemapVectorTileHandler_ServesUpstreamBytesThenCacheHitWithoutFetching(t *testing.T) {
	cache := newTestTileCache(t)
	fetcher := respondingFetcher("vectortiles/carto.streets", "application/x-protobuf", "mvt-bytes")

	c, rec := newBasemapTestContext("/api/basemap/tiles/12/3742/2283",
		[]string{"z", "x", "y"}, []string{"12", "3742", "2283"})
	if err := basemapVectorTileHandler(cache, fetcher)(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "mvt-bytes" {
		t.Errorf("tile body = %q, want upstream bytes verbatim", rec.Body.String())
	}

	before := fetcher.callCount()
	c2, rec2 := newBasemapTestContext("/api/basemap/tiles/12/3742/2283",
		[]string{"z", "x", "y"}, []string{"12", "3742", "2283"})
	if err := basemapVectorTileHandler(cache, fetcher)(c2); err != nil {
		t.Fatalf("handler returned error on repeat: %v", err)
	}
	if rec2.Code != http.StatusOK || rec2.Body.String() != "mvt-bytes" {
		t.Fatalf("repeat request: code=%d body=%q", rec2.Code, rec2.Body.String())
	}
	if got := fetcher.callCount(); got != before {
		t.Errorf("tile cache hit still called upstream: %d before, %d after", before, got)
	}
}

// A missing vector tile is "no data here", which MapLibre already handles
// by drawing nothing. It is not an outage worth a 502, and - critically -
// it must never be answered with some other tile's bytes.
func TestBasemapVectorTileHandler_UpstreamFailureWithEmptyCacheIs404(t *testing.T) {
	cache := newTestTileCache(t)

	c, rec := newBasemapTestContext("/api/basemap/tiles/12/3742/2283",
		[]string{"z", "x", "y"}, []string{"12", "3742", "2283"})
	if err := basemapVectorTileHandler(cache, failingFetcher())(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (body %s)", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Errorf("404 should carry no body, got %q", rec.Body.String())
	}
}

// The handler-level half of the MVT coordinate-mismatch guard: even with a
// perfectly good parent tile sitting in the cache, a failed deep request
// must 404 rather than hand back the parent's bytes. Vector tile
// coordinates are tile-local, so the parent's geometry would land in
// visibly the wrong place.
func TestBasemapVectorTileHandler_NeverServesAParentTilesBytesForAMissingChild(t *testing.T) {
	cache := newTestTileCache(t)
	const parentBytes = "parent-mvt-bytes"
	if err := cache.put(cartoBasemapSource, 11, 1871, 1141, []byte(parentBytes), "application/x-protobuf"); err != nil {
		t.Fatalf("seeding parent tile: %v", err)
	}

	c, rec := newBasemapTestContext("/api/basemap/tiles/12/3742/2283",
		[]string{"z", "x", "y"}, []string{"12", "3742", "2283"})
	if err := basemapVectorTileHandler(cache, failingFetcher())(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), parentBytes) {
		t.Fatal("served the parent tile's bytes for a missing child - every feature would be misplaced")
	}
	if _, _, ok, err := cache.get(cartoBasemapSource, 12, 3742, 2283); err != nil {
		t.Fatalf("reading cache: %v", err)
	} else if ok {
		t.Fatal("cached something under the missing child's key after a failed fetch")
	}
}

func TestBasemapVectorTileHandler_RejectsZoomPastCartoMaxZoom(t *testing.T) {
	cache := newTestTileCache(t)
	fetcher := respondingFetcher("vectortiles/carto.streets", "application/x-protobuf", "mvt-bytes")

	c, rec := newBasemapTestContext("/api/basemap/tiles/15/7484/4566",
		[]string{"z", "x", "y"}, []string{"15", "7484", "4566"})
	if err := basemapVectorTileHandler(cache, fetcher)(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for z=15 (Carto maxzoom is %d), got %d", cartoBasemapMaxZoom, rec.Code)
	}
	if fetcher.callCount() != 0 {
		t.Error("z past maxzoom still reached upstream")
	}
}

func TestBasemapVectorTileHandler_RejectsNonNumericCoords(t *testing.T) {
	cache := newTestTileCache(t)
	fetcher := respondingFetcher("vectortiles/carto.streets", "application/x-protobuf", "mvt-bytes")

	c, rec := newBasemapTestContext("/api/basemap/tiles/a/b/c",
		[]string{"z", "x", "y"}, []string{"a", "b", "c"})
	if err := basemapVectorTileHandler(cache, fetcher)(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
	if fetcher.callCount() != 0 {
		t.Error("malformed coords reached upstream")
	}
}

// E-7: strconv.Atoi("-1") parses fine, so a negative x/y used to sail past
// the zoom check straight into resolveCartoVectorTile ->
// fetchCartoVectorTileUpstream, where
// cartoVectorTileHosts[(x+y)%len(cartoVectorTileHosts)] (tile_cache.go)
// indexes a 4-element array with a negative remainder (Go's % keeps the
// sign of the dividend) and panics with "index out of range". In the real
// server middleware.Recover() turns that into a 500 per request rather than
// crashing the process, but it is still an unvalidated-input panic reachable
// by anyone -- GET /api/basemap/tiles/0/-1/-1.
func TestBasemapVectorTileHandler_RejectsNegativeCoords(t *testing.T) {
	cache := newTestTileCache(t)
	fetcher := respondingFetcher("vectortiles/carto.streets", "application/x-protobuf", "mvt-bytes")

	c, rec := newBasemapTestContext("/api/basemap/tiles/0/-1/-1",
		[]string{"z", "x", "y"}, []string{"0", "-1", "-1"})
	if err := basemapVectorTileHandler(cache, fetcher)(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for negative x/y, got %d", rec.Code)
	}
	if fetcher.callCount() != 0 {
		t.Error("negative coords reached upstream")
	}
}

// x and y are only meaningful in [0, 2^z-1] for their own z. Rejecting an
// out-of-range coordinate rather than clamping it matches the reasoning
// cartoBasemapMaxZoom's doc comment gives for z itself: a request for tile
// (4,0) at z=2 (valid index range 0-3) has no "nearest valid tile" that
// isn't actually some other, wrong, place.
func TestBasemapVectorTileHandler_RejectsCoordsOutOfRangeForZoom(t *testing.T) {
	cache := newTestTileCache(t)
	fetcher := respondingFetcher("vectortiles/carto.streets", "application/x-protobuf", "mvt-bytes")

	// z=2 has a valid x/y range of 0-3; 4 is one past it.
	c, rec := newBasemapTestContext("/api/basemap/tiles/2/4/0",
		[]string{"z", "x", "y"}, []string{"2", "4", "0"})
	if err := basemapVectorTileHandler(cache, fetcher)(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for x past the valid range at z=2, got %d", rec.Code)
	}
	if fetcher.callCount() != 0 {
		t.Error("out-of-range coords reached upstream")
	}
}

func TestRewriteCartoStyle_FailsLoudlyOnUnexpectedUpstreamShape(t *testing.T) {
	cases := map[string]string{
		"no sources":      `{"version":8,"glyphs":"g","sprite":"s","layers":[]}`,
		"no carto source": `{"version":8,"sources":{"other":{"url":"u"}},"glyphs":"g","sprite":"s"}`,
		"no glyphs":       `{"version":8,"sources":{"carto":{"url":"u"}},"sprite":"s"}`,
		"no sprite":       `{"version":8,"sources":{"carto":{"url":"u"}},"glyphs":"g"}`,
		"not json":        `not json at all`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := rewriteCartoStyle([]byte(raw), "positron"); err == nil {
				t.Error("expected an error rather than a best-effort partial rewrite")
			}
		})
	}
}

func TestRewriteCartoTileJSON_FailsLoudlyWithoutTilesArray(t *testing.T) {
	if _, err := rewriteCartoTileJSON([]byte(`{"tilejson":"2.2.0","maxzoom":14}`)); err == nil {
		t.Error("expected an error when the upstream tilejson has no tiles array")
	}
}

// MapLibre rejects a relative "sprite" URL outright ("Invalid sprite URL
// ..., must be absolute") and aborts the whole style load, so no source is
// ever registered and not a single tile is requested. The map goes blank
// with only the route line and coastline fallback left drawing. Everything
// else in the style may be relative; sprite specifically may not.
//
// The absolute URL has to be built per-request from the incoming scheme and
// host, because only the request knows what origin the browser used to get
// here. In production the backend serves the embedded frontend, so that is
// the same origin the app itself was loaded from.
func TestBasemapStyleHandler_ServesAbsoluteSpriteURLBuiltFromRequest(t *testing.T) {
	cache := newTestTileCache(t)
	fetcher := respondingFetcher("positron-gl-style/style.json", "application/json", fakeCartoStyleJSON)

	c, rec := newBasemapTestContext("/api/basemap/style/positron", []string{"name"}, []string{"positron"})
	if err := basemapStyleHandler(cache, fetcher)(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	// httptest.NewRequest defaults Host to example.com. Every same-origin
	// URL in the served document must be absolute, not just sprite: the
	// vector tiles are fetched from a Web Worker, which has no document
	// base URL to resolve a relative path against.
	if got := doc["sprite"]; got != "http://example.com/api/basemap/sprite/positron" {
		t.Errorf("sprite = %v, want an absolute same-origin URL", got)
	}
	if got := doc["glyphs"]; got != "http://example.com/api/basemap/fonts/{fontstack}/{range}.pbf" {
		t.Errorf("glyphs = %v, want an absolute same-origin URL", got)
	}
	sources, _ := doc["sources"].(map[string]any)
	carto, _ := sources["carto"].(map[string]any)
	if got := carto["url"]; got != "http://example.com/api/basemap/tilejson" {
		t.Errorf("sources.carto.url = %v, want an absolute same-origin URL", got)
	}

	// The CACHED copy must stay relative, so one cache entry stays correct
	// whichever hostname the dashboard is reached on (localhost, the boat's
	// LAN IP, Tailscale).
	data, _, ok, err := cache.getBasemapAsset("/api/basemap/style/positron")
	if err != nil || !ok {
		t.Fatalf("style not cached: ok=%v err=%v", ok, err)
	}
	var cached map[string]any
	if err := json.Unmarshal(data, &cached); err != nil {
		t.Fatalf("cached style is not valid JSON: %v", err)
	}
	if got := cached["sprite"]; got != "/api/basemap/sprite/positron" {
		t.Errorf("cached sprite = %v, want the host-independent relative path", got)
	}
	if got := cached["glyphs"]; got != "/api/basemap/fonts/{fontstack}/{range}.pbf" {
		t.Errorf("cached glyphs = %v, want the host-independent relative path", got)
	}
}

func TestBasemapStyleHandler_HonoursForwardedProtoAndHost(t *testing.T) {
	cache := newTestTileCache(t)
	fetcher := respondingFetcher("positron-gl-style/style.json", "application/json", fakeCartoStyleJSON)

	c, rec := newBasemapTestContext("/api/basemap/style/positron", []string{"name"}, []string{"positron"})
	c.Request().Header.Set("X-Forwarded-Proto", "https")
	c.Request().Header.Set("X-Forwarded-Host", "boat.example.net")
	if err := basemapStyleHandler(cache, fetcher)(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if got := doc["sprite"]; got != "https://boat.example.net/api/basemap/sprite/positron" {
		t.Errorf("sprite = %v, want the forwarded origin", got)
	}
	if got := doc["glyphs"]; got != "https://boat.example.net/api/basemap/fonts/{fontstack}/{range}.pbf" {
		t.Errorf("glyphs = %v, want the forwarded origin", got)
	}
}

// The regression that blanked the whole basemap: a relative tile template
// in the TileJSON cannot be resolved inside MapLibre's Web Worker, so every
// tile request dies at construction and the map draws nothing but the route
// line. The cached copy still stays relative so it survives a hostname
// change.
func TestBasemapTileJSONHandler_ServesAbsoluteTileTemplateButCachesRelative(t *testing.T) {
	cache := newTestTileCache(t)
	fetcher := respondingFetcher("carto.streets/v1/tiles.json", "application/json", fakeCartoTileJSON)

	c, rec := newBasemapTestContext("/api/basemap/tilejson", nil, nil)
	if err := basemapTileJSONHandler(cache, fetcher)(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	tiles, _ := doc["tiles"].([]any)
	if len(tiles) != 1 {
		t.Fatalf("tiles = %v", doc["tiles"])
	}
	got, _ := tiles[0].(string)
	if !strings.HasPrefix(got, "http://example.com/") {
		t.Errorf("tile template %q is not absolute; the Web Worker cannot resolve it", got)
	}

	data, _, ok, err := cache.getBasemapAsset("/api/basemap/tilejson")
	if err != nil || !ok {
		t.Fatalf("tilejson not cached: ok=%v err=%v", ok, err)
	}
	var cached map[string]any
	if err := json.Unmarshal(data, &cached); err != nil {
		t.Fatalf("cached tilejson is not valid JSON: %v", err)
	}
	cachedTiles, _ := cached["tiles"].([]any)
	if len(cachedTiles) != 1 || cachedTiles[0] != "/api/basemap/tiles/{z}/{x}/{y}" {
		t.Errorf("cached tiles = %v, want the host-independent relative template", cached["tiles"])
	}
}
