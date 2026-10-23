package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// cartoFixture reads a copy of Carto's real upstream style, captured from
// https://basemaps.cartocdn.com/gl/{positron,dark-matter}-gl-style/style.json
// on 2026-10-23 (the "Protomaps" generation: one source "protomaps" with an
// inline tiles template, versioned glyphs and sprite URLs, 95 layers).
func cartoFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/carto/" + name + "-style.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(b)
}

const (
	fixtureTilesTemplate = "https://tiles.basemaps.cartocdn.com/v/20261001-03/vectortiles/carto.streets/v2/{z}/{x}/{y}.mvt"
	fixtureGlyphs        = "https://tiles.basemaps.cartocdn.com/v/20261001-03/fonts/{fontstack}/{range}.pbf"
	fixtureSpritePos     = "https://tiles.basemaps.cartocdn.com/v/20261001-03/gl/positron-gl-style/sprite"
	fixtureSpriteDark    = "https://tiles.basemaps.cartocdn.com/v/20261001-03/gl/dark-matter-gl-style/sprite"
)

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

// seedCartoUpstream stores the real upstream styles in the cache, as a prior
// style request would have, so tests of the tile/glyph/sprite paths start
// from "this install already knows which Carto release it is on" and their
// fetcher call counts only count the asset under test.
func seedCartoUpstream(t *testing.T, cache *tileCache) {
	t.Helper()
	for _, name := range []string{"positron", "dark-matter"} {
		if err := cache.putBasemapAsset(cartoUpstreamAssetPath(name), []byte(cartoFixture(t, name)), "application/json"); err != nil {
			t.Fatalf("seed upstream style %s: %v", name, err)
		}
	}
}

// upstreamURLRecorder returns a fetcher that records every URL it is asked
// for and answers each with body.
func upstreamURLRecorder(contentType, body string, urls *[]string) *fakeTileFetcher {
	return &fakeTileFetcher{responder: func(req *http.Request) (*http.Response, error) {
		*urls = append(*urls, req.URL.String())
		return fakeUpstreamResponse(http.StatusOK, contentType, []byte(body)), nil
	}}
}

func decodeStyle(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	return doc
}

// The whole reason the style handler decodes and re-encodes JSON rather
// than piping upstream bytes through: the tile, glyph and sprite URLs must
// move to this proxy, and everything the frontend pins by name must survive.
func TestBasemapStyleHandler_RewritesURLsAndPreservesSourceAndLayerIDs(t *testing.T) {
	for _, theme := range []string{"positron", "dark-matter"} {
		t.Run(theme, func(t *testing.T) {
			cache := newTestTileCache(t)
			fetcher := respondingFetcher(theme+"-gl-style/style.json", "application/json", cartoFixture(t, theme))

			c, rec := newBasemapTestContext("/api/basemap/style/"+theme, []string{"name"}, []string{theme})
			if err := basemapStyleHandler(cache, fetcher)(c); err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d (body %s)", rec.Code, rec.Body.String())
			}
			doc := decodeStyle(t, rec.Body.Bytes())

			sources, _ := doc["sources"].(map[string]any)
			if len(sources) != 1 {
				t.Fatalf("sources = %v, want exactly the one vector source", sources)
			}
			src, ok := sources["protomaps"].(map[string]any)
			if !ok {
				t.Fatalf(`style lost the "protomaps" source id that map-place-labels.tsx pins; sources = %v`, sources)
			}
			tiles, _ := src["tiles"].([]any)
			if len(tiles) != 1 || tiles[0] != "http://example.com/api/basemap/v2/tiles/{z}/{x}/{y}" {
				t.Errorf("tiles = %v, want the single absolute same-origin template", src["tiles"])
			}
			if _, has := src["url"]; has {
				t.Errorf("source still carries a url: %v", src["url"])
			}
			// maxzoom is what makes MapLibre overzoom client-side rather than
			// asking this proxy for tiles Carto does not have.
			if src["maxzoom"] != float64(cartoBasemapMaxZoom) {
				t.Errorf("source maxzoom = %v, proxy cap is %d", src["maxzoom"], cartoBasemapMaxZoom)
			}
			if a, _ := src["attribution"].(string); !strings.Contains(a, "CARTO") {
				t.Errorf("attribution must survive the rewrite, got %q", a)
			}
			if got := doc["glyphs"]; got != "http://example.com/api/basemap/v2/fonts/{fontstack}/{range}.pbf" {
				t.Errorf("glyphs = %v", got)
			}
			if got := doc["sprite"]; got != "http://example.com/api/basemap/v2/sprite/"+theme {
				t.Errorf("sprite = %v", got)
			}
			if strings.Contains(rec.Body.String(), "cartocdn.com/v/") {
				t.Error("served style still points at the versioned upstream CDN paths")
			}

			layers, _ := doc["layers"].([]any)
			if len(layers) == 0 {
				t.Fatal("rewritten style lost its layers array")
			}
			// BASE_STYLE_FIRST_LAYER_ID.
			if first, ok := layers[0].(map[string]any); !ok || first["id"] != "background" {
				t.Errorf(`first layer must stay "background", got %v`, layers[0])
			}
		})
	}
}

// What is cached is the validated upstream style, host-independent, so an
// offline cold start (and a different hostname: localhost, LAN IP,
// Tailscale) is served from it without touching the CDN. Only the request
// decides the origin written into the served copy.
func TestBasemapStyleHandler_CachesUpstreamStyleAndServesRepeatsOffline(t *testing.T) {
	cache := newTestTileCache(t)
	fetcher := respondingFetcher("positron-gl-style/style.json", "application/json", cartoFixture(t, "positron"))

	c, _ := newBasemapTestContext("/api/basemap/style/positron", []string{"name"}, []string{"positron"})
	if err := basemapStyleHandler(cache, fetcher)(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if _, _, ok, err := cache.getBasemapAsset(cartoUpstreamAssetPath("positron")); err != nil || !ok {
		t.Fatalf("upstream style not cached: ok=%v err=%v", ok, err)
	}

	before := fetcher.callCount()
	c2, rec2 := newBasemapTestContext("/api/basemap/style/positron", []string{"name"}, []string{"positron"})
	c2.Request().Header.Set("X-Forwarded-Proto", "https")
	c2.Request().Header.Set("X-Forwarded-Host", "boat.example.net")
	if err := basemapStyleHandler(cache, fetcher)(c2); err != nil {
		t.Fatalf("handler returned error on repeat: %v", err)
	}
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 on repeat, got %d", rec2.Code)
	}
	if got := fetcher.callCount(); got != before {
		t.Errorf("cache hit still called upstream: %d before, %d after", before, got)
	}
	doc := decodeStyle(t, rec2.Body.Bytes())
	if doc["sprite"] != "https://boat.example.net/api/basemap/v2/sprite/positron" {
		t.Errorf("sprite = %v, want the forwarded origin", doc["sprite"])
	}
	if doc["glyphs"] != "https://boat.example.net/api/basemap/v2/fonts/{fontstack}/{range}.pbf" {
		t.Errorf("glyphs = %v, want the forwarded origin", doc["glyphs"])
	}
	tiles := doc["sources"].(map[string]any)["protomaps"].(map[string]any)["tiles"].([]any)
	if tiles[0] != "https://boat.example.net/api/basemap/v2/tiles/{z}/{x}/{y}" {
		t.Errorf("tiles = %v, want the forwarded origin", tiles)
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
	if _, _, ok, err := cache.getBasemapAsset(cartoUpstreamAssetPath("positron")); err != nil {
		t.Fatalf("reading cached asset: %v", err)
	} else if ok {
		t.Error("a failed fetch must not leave anything in the cache")
	}
}

// A style in a shape the proxy does not understand is an upstream error and
// is never cached, so the next request tries again rather than serving a
// stored unusable document forever.
func TestBasemapStyleHandler_UnusableUpstreamStyleIs502AndNotCached(t *testing.T) {
	cache := newTestTileCache(t)
	// The retired v1 shape: a source with a TileJSON url and no tiles.
	old := `{"version":8,"sources":{"carto":{"type":"vector","url":"https://tiles.basemaps.cartocdn.com/vector/carto.streets/v1/tiles.json"}},"glyphs":"https://tiles.basemaps.cartocdn.com/fonts/{fontstack}/{range}.pbf","sprite":"https://basemaps.cartocdn.com/gl/positron-gl-style/sprite","layers":[]}`
	fetcher := respondingFetcher("positron-gl-style/style.json", "application/json", old)

	c, rec := newBasemapTestContext("/api/basemap/style/positron", []string{"name"}, []string{"positron"})
	if err := basemapStyleHandler(cache, fetcher)(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
	if _, _, ok, _ := cache.getBasemapAsset(cartoUpstreamAssetPath("positron")); ok {
		t.Error("an unusable upstream style was cached")
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

func TestParseCartoUpstream_ReadsRealStyles(t *testing.T) {
	for theme, wantSprite := range map[string]string{"positron": fixtureSpritePos, "dark-matter": fixtureSpriteDark} {
		up, _, err := parseCartoUpstream([]byte(cartoFixture(t, theme)))
		if err != nil {
			t.Fatalf("%s: %v", theme, err)
		}
		if up.sourceID != "protomaps" || up.tiles != fixtureTilesTemplate || up.glyphs != fixtureGlyphs || up.sprite != wantSprite {
			t.Errorf("%s: parsed %+v", theme, up)
		}
	}
}

// Everything the proxy fetches comes from URLs in a document it does not
// control, so each one is checked against the fixed host list before it is
// cached or fetched. Also the exactly-one-vector-source rule and the
// fail-fast shape checks.
func TestParseCartoUpstream_FailsLoudlyOnUnexpectedShape(t *testing.T) {
	const okTiles = `["https://tiles.basemaps.cartocdn.com/v/1/t/{z}/{x}/{y}.mvt"]`
	const okGlyphs = `"https://tiles.basemaps.cartocdn.com/v/1/fonts/{fontstack}/{range}.pbf"`
	const okSprite = `"https://tiles.basemaps.cartocdn.com/v/1/sprite"`
	mk := func(sources, glyphs, sprite string) string {
		return `{"version":8,"sources":` + sources + `,"glyphs":` + glyphs + `,"sprite":` + sprite + `}`
	}
	vec := func(tiles string) string { return `{"p":{"type":"vector","tiles":` + tiles + `}}` }
	cases := map[string]string{
		"not json":              `not json at all`,
		"no sources":            `{"version":8,"glyphs":` + okGlyphs + `,"sprite":` + okSprite + `}`,
		"no vector source":      mk(`{"r":{"type":"raster","tiles":`+okTiles+`}}`, okGlyphs, okSprite),
		"two vector sources":    mk(`{"a":{"type":"vector","tiles":`+okTiles+`},"b":{"type":"vector","tiles":`+okTiles+`}}`, okGlyphs, okSprite),
		"tilejson url only":     mk(`{"p":{"type":"vector","url":"https://tiles.basemaps.cartocdn.com/x.json"}}`, okGlyphs, okSprite),
		"two tile templates":    mk(vec(`["https://tiles.basemaps.cartocdn.com/{z}/{x}/{y}","https://tiles.basemaps.cartocdn.com/{z}/{x}/{y}"]`), okGlyphs, okSprite),
		"tile template no xyz":  mk(vec(`["https://tiles.basemaps.cartocdn.com/fixed.mvt"]`), okGlyphs, okSprite),
		"no glyphs":             `{"version":8,"sources":` + vec(okTiles) + `,"sprite":` + okSprite + `}`,
		"glyphs no placeholder": mk(vec(okTiles), `"https://tiles.basemaps.cartocdn.com/fonts.pbf"`, okSprite),
		"no sprite":             `{"version":8,"sources":` + vec(okTiles) + `,"glyphs":` + okGlyphs + `}`,
		"tiles on foreign host": mk(vec(`["https://evil.example/{z}/{x}/{y}.mvt"]`), okGlyphs, okSprite),
		"tiles over http":       mk(vec(`["http://tiles.basemaps.cartocdn.com/{z}/{x}/{y}.mvt"]`), okGlyphs, okSprite),
		"glyphs foreign host":   mk(vec(okTiles), `"https://evil.example/{fontstack}/{range}.pbf"`, okSprite),
		"sprite foreign host":   mk(vec(okTiles), okGlyphs, `"https://evil.example/sprite"`),
		"lookalike host":        mk(vec(`["https://tiles.basemaps.cartocdn.com.evil.example/{z}/{x}/{y}"]`), okGlyphs, okSprite),
		"userinfo trick":        mk(vec(`["https://tiles.basemaps.cartocdn.com@evil.example/{z}/{x}/{y}"]`), okGlyphs, okSprite),
		"explicit port":         mk(vec(`["https://tiles.basemaps.cartocdn.com:8443/{z}/{x}/{y}"]`), okGlyphs, okSprite),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := parseCartoUpstream([]byte(raw)); err == nil {
				t.Error("expected an error rather than a best-effort partial parse")
			}
		})
	}
	// Control: the shape the cases above are variations of is accepted.
	if _, _, err := parseCartoUpstream([]byte(mk(vec(okTiles), okGlyphs, okSprite))); err != nil {
		t.Fatalf("control document rejected: %v", err)
	}
}

// A poisoned style must not get as far as the cache or the fetcher.
func TestBasemapStyleHandler_ForeignHostInStyleNeverCachedOrFetched(t *testing.T) {
	cache := newTestTileCache(t)
	poisoned := strings.ReplaceAll(cartoFixture(t, "positron"), "tiles.basemaps.cartocdn.com", "evil.example")
	var urls []string
	fetcher := &fakeTileFetcher{responder: func(req *http.Request) (*http.Response, error) {
		urls = append(urls, req.URL.String())
		return fakeUpstreamResponse(http.StatusOK, "application/json", []byte(poisoned)), nil
	}}

	c, rec := newBasemapTestContext("/api/basemap/style/positron", []string{"name"}, []string{"positron"})
	if err := basemapStyleHandler(cache, fetcher)(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
	for _, u := range urls {
		if strings.Contains(u, "evil.example") {
			t.Errorf("fetched a foreign host: %s", u)
		}
	}
	if _, _, ok, _ := cache.getBasemapAsset(cartoUpstreamAssetPath("positron")); ok {
		t.Error("poisoned style was cached")
	}
}

// Tiles, glyphs and sprite are fetched from the URLs the current upstream
// style names, not from constants that would rot when Carto moves its release
// stamp.
func TestBasemapAssets_FetchedFromTheTemplatesInTheUpstreamStyle(t *testing.T) {
	cache := newTestTileCache(t)
	seedCartoUpstream(t, cache)
	var urls []string
	fetcher := upstreamURLRecorder("application/octet-stream", "bytes", &urls)

	cT, _ := newBasemapTestContext("/", []string{"z", "x", "y"}, []string{"12", "3742", "2283"})
	if err := basemapVectorTileHandler(cache, fetcher)(cT); err != nil {
		t.Fatal(err)
	}
	cF, _ := newBasemapTestContext("/", []string{"fontstack", "range"}, []string{"Montserrat Medium,Open Sans Bold", "0-255.pbf"})
	if err := basemapFontsHandler(cache, fetcher)(cF); err != nil {
		t.Fatal(err)
	}
	cS, _ := newBasemapTestContext("/", []string{"name"}, []string{"dark-matter@2x.png"})
	if err := basemapSpriteHandler(cache, fetcher)(cS); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"https://tiles.basemaps.cartocdn.com/v/20261001-03/vectortiles/carto.streets/v2/12/3742/2283.mvt",
		"https://tiles.basemaps.cartocdn.com/v/20261001-03/fonts/Montserrat%20Medium%2COpen%20Sans%20Bold/0-255.pbf",
		"https://tiles.basemaps.cartocdn.com/v/20261001-03/gl/dark-matter-gl-style/sprite@2x.png",
	}
	if strings.Join(urls, "\n") != strings.Join(want, "\n") {
		t.Errorf("upstream URLs =\n%s\nwant\n%s", strings.Join(urls, "\n"), strings.Join(want, "\n"))
	}
}

// On a fresh install the first tile request has no cached style to read the
// template from, so it fetches the style once, caches it, then fetches the
// tile.
func TestBasemapVectorTileHandler_FreshInstallFetchesStyleThenTile(t *testing.T) {
	cache := newTestTileCache(t)
	var urls []string
	fetcher := &fakeTileFetcher{responder: func(req *http.Request) (*http.Response, error) {
		urls = append(urls, req.URL.String())
		if strings.HasSuffix(req.URL.Path, "style.json") {
			return fakeUpstreamResponse(http.StatusOK, "application/json", []byte(cartoFixture(t, "positron"))), nil
		}
		return fakeUpstreamResponse(http.StatusOK, "application/x-protobuf", []byte("mvt-bytes")), nil
	}}

	c, rec := newBasemapTestContext("/", []string{"z", "x", "y"}, []string{"3", "4", "2"})
	if err := basemapVectorTileHandler(cache, fetcher)(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || rec.Body.String() != "mvt-bytes" {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
	if len(urls) != 2 || !strings.HasSuffix(urls[0], "positron-gl-style/style.json") || !strings.HasSuffix(urls[1], "/v2/3/4/2.mvt") {
		t.Errorf("upstream calls = %v", urls)
	}
}

// MapLibre substitutes {fontstack} itself, producing a single path segment
// carrying commas and spaces. Echo hands it over percent-decoded, so the
// handler has to re-encode for the upstream call - and must never let the
// value smuggle a "/" into the upstream path.
func TestBasemapFontsHandler_ReEncodesFontstackForUpstream(t *testing.T) {
	cache := newTestTileCache(t)
	seedCartoUpstream(t, cache)
	const stack = "Montserrat Medium,Open Sans Bold,Noto Sans Regular"

	var gotURL string
	fetcher := &fakeTileFetcher{
		responder: func(req *http.Request) (*http.Response, error) {
			gotURL = req.URL.String()
			return fakeUpstreamResponse(http.StatusOK, "application/x-protobuf", []byte("glyph-bytes")), nil
		},
	}

	c, rec := newBasemapTestContext(
		"/api/basemap/v2/fonts/x/0-255.pbf",
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

	const prefix = "https://tiles.basemaps.cartocdn.com/v/20261001-03/fonts/"
	if !strings.HasPrefix(gotURL, prefix) {
		t.Fatalf("upstream URL escaped the glyphs path: %s", gotURL)
	}
	if !strings.HasSuffix(gotURL, "/0-255.pbf") {
		t.Errorf("upstream URL lost the range suffix: %s", gotURL)
	}
	segment := strings.TrimSuffix(strings.TrimPrefix(gotURL, prefix), "/0-255.pbf")
	if strings.Contains(segment, " ") || strings.Contains(segment, "/") {
		t.Errorf("fontstack segment not safely encoded: %q", segment)
	}
	if !strings.Contains(segment, "%20") || !strings.Contains(segment, "%2C") {
		t.Errorf("fontstack segment missing percent-encoding for space/comma: %q", segment)
	}

	before := fetcher.callCount()
	c2, rec2 := newBasemapTestContext(
		"/api/basemap/v2/fonts/x/0-255.pbf",
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

func TestBasemapFontsHandler_FontstackCannotEscapeTheGlyphPath(t *testing.T) {
	cache := newTestTileCache(t)
	seedCartoUpstream(t, cache)
	var urls []string
	fetcher := upstreamURLRecorder("application/x-protobuf", "x", &urls)

	c, _ := newBasemapTestContext("/", []string{"fontstack", "range"}, []string{"../../gl/positron-gl-style/style.json", "0-255.pbf"})
	if err := basemapFontsHandler(cache, fetcher)(c); err != nil {
		t.Fatal(err)
	}
	if len(urls) != 1 || !strings.HasPrefix(urls[0], "https://tiles.basemaps.cartocdn.com/v/20261001-03/fonts/..%2F..%2F") {
		t.Errorf("fontstack was not confined to one path segment: %v", urls)
	}
}

func TestBasemapFontsHandler_RejectsRangeWithoutPbfSuffix(t *testing.T) {
	cache := newTestTileCache(t)
	fetcher := respondingFetcher("nothing", "application/x-protobuf", "x")

	c, rec := newBasemapTestContext(
		"/api/basemap/v2/fonts/x/0-255",
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

// MapLibre appends ".json"/".png" (and "@2x" before the extension) to the
// style's sprite base URL, so the proxy serves exactly those 8 names.
func TestBasemapSpriteHandler_ServesAllowlistedNamesAndRejectsOthers(t *testing.T) {
	allowed := []string{
		"positron.json", "positron.png", "positron@2x.json", "positron@2x.png",
		"dark-matter.json", "dark-matter.png", "dark-matter@2x.json", "dark-matter@2x.png",
	}
	for _, name := range allowed {
		t.Run("allow/"+name, func(t *testing.T) {
			cache := newTestTileCache(t)
			seedCartoUpstream(t, cache)
			var urls []string
			fetcher := upstreamURLRecorder("image/png", "sprite-bytes", &urls)

			c, rec := newBasemapTestContext("/api/basemap/v2/sprite/"+name, []string{"name"}, []string{name})
			if err := basemapSpriteHandler(cache, fetcher)(c); err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if rec.Code != http.StatusOK {
				t.Errorf("expected 200 for %q, got %d", name, rec.Code)
			}
			theme, suffix, _ := splitCartoSpriteName(name)
			want := "https://tiles.basemaps.cartocdn.com/v/20261001-03/gl/" + theme + "-gl-style/sprite" + suffix
			if len(urls) != 1 || urls[0] != want {
				t.Errorf("upstream URLs = %v, want [%s]", urls, want)
			}
		})
	}

	for _, name := range []string{"positron", "positron.svg", "", "../style.json", "evil@2x.png", "positron.json/../x", "positron@3x.png"} {
		t.Run("reject/"+name, func(t *testing.T) {
			cache := newTestTileCache(t)
			fetcher := respondingFetcher("sprite", "image/png", "sprite-bytes")

			c, rec := newBasemapTestContext("/api/basemap/v2/sprite/x", []string{"name"}, []string{name})
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
	seedCartoUpstream(t, cache)
	fetcher := respondingFetcher("vectortiles/carto.streets", "application/x-protobuf", "mvt-bytes")

	c, rec := newBasemapTestContext("/api/basemap/v2/tiles/12/3742/2283",
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
	c2, rec2 := newBasemapTestContext("/api/basemap/v2/tiles/12/3742/2283",
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
	seedCartoUpstream(t, cache)

	c, rec := newBasemapTestContext("/api/basemap/v2/tiles/12/3742/2283",
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
	seedCartoUpstream(t, cache)
	const parentBytes = "parent-mvt-bytes"
	if err := cache.put(cartoBasemapSource, 11, 1871, 1141, []byte(parentBytes), "application/x-protobuf"); err != nil {
		t.Fatalf("seeding parent tile: %v", err)
	}

	c, rec := newBasemapTestContext("/api/basemap/v2/tiles/12/3742/2283",
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

	z := cartoBasemapMaxZoom + 1
	c, rec := newBasemapTestContext(fmt.Sprintf("/api/basemap/v2/tiles/%d/7484/4566", z),
		[]string{"z", "x", "y"}, []string{fmt.Sprint(z), "7484", "4566"})
	if err := basemapVectorTileHandler(cache, fetcher)(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for z=%d (Carto maxzoom is %d), got %d", z, cartoBasemapMaxZoom, rec.Code)
	}
	if fetcher.callCount() != 0 {
		t.Error("z past maxzoom still reached upstream")
	}
}

func TestBasemapVectorTileHandler_RejectsNonNumericCoords(t *testing.T) {
	cache := newTestTileCache(t)
	fetcher := respondingFetcher("vectortiles/carto.streets", "application/x-protobuf", "mvt-bytes")

	c, rec := newBasemapTestContext("/api/basemap/v2/tiles/a/b/c",
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

// strconv.Atoi("-1") parses fine, so a negative x/y must be rejected before
// it is interpolated into an upstream tile URL.
func TestBasemapVectorTileHandler_RejectsNegativeCoords(t *testing.T) {
	cache := newTestTileCache(t)
	fetcher := respondingFetcher("vectortiles/carto.streets", "application/x-protobuf", "mvt-bytes")

	c, rec := newBasemapTestContext("/api/basemap/v2/tiles/0/-1/-1",
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
// cartoBasemapMaxZoom's doc comment gives for z itself.
func TestBasemapVectorTileHandler_RejectsCoordsOutOfRangeForZoom(t *testing.T) {
	cache := newTestTileCache(t)
	fetcher := respondingFetcher("vectortiles/carto.streets", "application/x-protobuf", "mvt-bytes")

	c, rec := newBasemapTestContext("/api/basemap/v2/tiles/2/4/0",
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

// The cache-migration contract (ADR 0201): rows written for the
// OpenMapTiles-shaped styles are purged when the cache opens and are never
// reachable through the v2 keys, so a boat that upgrades refetches.
func TestNewTileCache_PurgesPreProtomapsBasemapRows(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/tiles.sqlite"
	cache, err := newTileCache(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.put(legacyCartoBasemapSource, 5, 1, 1, []byte("old-tile"), "application/x-protobuf"); err != nil {
		t.Fatal(err)
	}
	if err := cache.put(worldImagerySource, 5, 1, 1, []byte("imagery"), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/api/basemap/style/positron", "/api/basemap/tilejson", "/api/basemap/fonts/a/0-255", "/api/basemap/sprite/positron.json"} {
		if err := cache.putBasemapAsset(p, []byte("old"), "application/json"); err != nil {
			t.Fatal(err)
		}
	}
	if err := cache.putBasemapAsset(basemapAssetPrefix+"fonts/a/0-255", []byte("new"), "application/x-protobuf"); err != nil {
		t.Fatal(err)
	}
	_ = cache.close()

	cache, err = newTileCache(path)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.close()

	if _, _, ok, _ := cache.get(legacyCartoBasemapSource, 5, 1, 1); ok {
		t.Error("legacy vector tile survived the purge")
	}
	if _, _, ok, _ := cache.get(cartoBasemapSource, 5, 1, 1); ok {
		t.Error("legacy tile reachable under the v2 source")
	}
	if _, _, ok, _ := cache.get(worldImagerySource, 5, 1, 1); !ok {
		t.Error("the purge removed Esri imagery")
	}
	for _, p := range []string{"/api/basemap/style/positron", "/api/basemap/tilejson", "/api/basemap/fonts/a/0-255", "/api/basemap/sprite/positron.json"} {
		if _, _, ok, _ := cache.getBasemapAsset(p); ok {
			t.Errorf("legacy asset %s survived the purge", p)
		}
	}
	if _, _, ok, _ := cache.getBasemapAsset(basemapAssetPrefix + "fonts/a/0-255"); !ok {
		t.Error("the purge removed a current-generation asset")
	}
}

// Every path the proxy hands out is host-independent in the cache and
// absolute only in the served copy; the retired TileJSON route is gone.
func TestMapRoutes_NoTileJSONEndpoint(t *testing.T) {
	for _, r := range mapRoutes(nil) {
		if strings.Contains(r.Path, "tilejson") {
			t.Errorf("route %s still registered", r.Path)
		}
	}
}

// The browser caches tiles, sprite and glyphs as immutable, so the old
// unversioned paths must not exist: a v0.52.0 browser would otherwise keep
// pairing its cached OpenMapTiles data with the Protomaps style.
func TestBasemapRoutes_OnlyVersionedAssetPathsAreRegistered(t *testing.T) {
	registered := map[string]apiRoute{}
	for _, r := range mapRoutes(nil) {
		registered[r.Path] = r
	}
	for _, want := range []string{
		"/api/basemap/style/:name",
		"/api/basemap/v2/tiles/:z/:x/:y",
		"/api/basemap/v2/fonts/:fontstack/:range",
		"/api/basemap/v2/sprite/:name",
	} {
		r, ok := registered[want]
		if !ok {
			t.Errorf("route %s not registered", want)
		} else if r.Tier != tierRead {
			t.Errorf("route %s tier = %v, want tierRead", want, r.Tier)
		}
	}
	for _, old := range []string{
		"/api/basemap/tiles/:z/:x/:y",
		"/api/basemap/fonts/:fontstack/:range",
		"/api/basemap/sprite/:name",
	} {
		if _, ok := registered[old]; ok {
			t.Errorf("unversioned route %s must be removed", old)
		}
	}
}
