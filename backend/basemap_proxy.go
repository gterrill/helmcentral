package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

// cartoBasemapMaxZoom is the deepest zoom level Carto's vector tile service
// actually serves, confirmed directly against the upstream tiles.json
// response's own "maxzoom" field (14). MapLibre overzooms client-side past
// a source's declared maxzoom - it keeps re-showing the deepest tile it
// already fetched, scaled up, rather than requesting one that doesn't
// exist - so a well-behaved client never asks this proxy for anything
// deeper. basemapVectorTileHandler enforces the cap anyway: rejecting a
// z>14 request outright is the only safe option, because "clamp" here
// would mean handing back a shallower tile's bytes under the deeper
// request key, which is exactly the MVT tile-local-coordinate mismatch
// resolveCartoVectorTile's doc comment (tile_cache.go) explains - moved up
// one layer, from "wrong cached bytes" to "wrong served bytes," but the
// same hazard either way.
const cartoBasemapMaxZoom = 14

// Cache-Control policy for the two shapes of basemap response. The server-
// side cache (basemap_assets / tiles table) never expires either way - see
// tileCache's doc comment - these headers only govern how long a browser
// keeps its own copy before asking again:
//   - tiles/glyphs/sprite are immutable in practice (a given z/x/y tile, or
//     a given font-range PBF, or a given theme's sprite sheet, is the same
//     bytes forever from Carto's point of view - confirmed against
//     upstream, which itself sends a 180-day max-age on all three), so
//     browsers get a long one.
//   - style.json and tiles.json are the one part of this proxy that could
//     plausibly change server-side (an app deploy that alters what gets
//     rewritten, or Carto shipping a new style upstream that this cache
//     hasn't picked up yet), and they're a handful of KB - cheap enough to
//     let the browser re-check often.
const basemapImmutableCacheControl = "public, max-age=31536000, immutable"
const basemapDocumentCacheControl = "public, max-age=300"

// cartoStyleUpstreamURLs is a closed allowlist from this proxy's own
// :name path param to the one upstream URL it's allowed to mean. This is
// an SSRF guard, not just tidiness: :name comes straight from the request,
// and if it were interpolated into an upstream URL template (e.g.
// fmt.Sprintf("https://basemaps.cartocdn.com/gl/%s-gl-style/style.json",
// name)) this endpoint would let any caller make the server fetch-and-
// cache an arbitrary path under that host. Looking name up in this fixed
// map instead means anything not already in it never reaches
// http.NewRequest at all - see basemapStyleHandler.
var cartoStyleUpstreamURLs = map[string]string{
	"positron":    "https://basemaps.cartocdn.com/gl/positron-gl-style/style.json",
	"dark-matter": "https://basemaps.cartocdn.com/gl/dark-matter-gl-style/style.json",
}

// cartoTileJSONUpstreamURL is Carto's one vector TileJSON document - the
// same "carto.streets/v1" source both the positron and dark-matter styles
// reference, which is why there is one tilejson endpoint, not two.
const cartoTileJSONUpstreamURL = "https://tiles.basemaps.cartocdn.com/vector/carto.streets/v1/tiles.json"

// cartoGlyphsUpstreamHost is the fixed, hardcoded host every glyph fetch
// goes to. Unlike the style/sprite allowlists above, the :fontstack and
// :range path params here are not restricted to a closed set of legal
// values (there is no fixed list of "every font stack a Carto style might
// ever reference"), but that's still not an SSRF hole: the host is never
// attacker-influenced, and url.PathEscape below guarantees neither param
// can inject a "/" to escape the /fonts/{fontstack}/{range}.pbf path shape
// onto some other upstream path.
const cartoGlyphsUpstreamHost = "https://tiles.basemaps.cartocdn.com/fonts"

// cartoSpriteUpstreamURLs is the same allowlist-map SSRF guard as
// cartoStyleUpstreamURLs above, covering the 8 legal (theme, resolution,
// extension) combinations a Carto style's "sprite" field can expand to:
// {positron, dark-matter} x {"", "@2x"} x {.json, .png}.
var cartoSpriteUpstreamURLs = map[string]string{
	"positron.json":       "https://basemaps.cartocdn.com/gl/positron-gl-style/sprite.json",
	"positron.png":        "https://basemaps.cartocdn.com/gl/positron-gl-style/sprite.png",
	"positron@2x.json":    "https://basemaps.cartocdn.com/gl/positron-gl-style/sprite@2x.json",
	"positron@2x.png":     "https://basemaps.cartocdn.com/gl/positron-gl-style/sprite@2x.png",
	"dark-matter.json":    "https://basemaps.cartocdn.com/gl/dark-matter-gl-style/sprite.json",
	"dark-matter.png":     "https://basemaps.cartocdn.com/gl/dark-matter-gl-style/sprite.png",
	"dark-matter@2x.json": "https://basemaps.cartocdn.com/gl/dark-matter-gl-style/sprite@2x.json",
	"dark-matter@2x.png":  "https://basemaps.cartocdn.com/gl/dark-matter-gl-style/sprite@2x.png",
}

// fetchBasemapUpstream performs one GET against upstreamURL and returns the
// response body verbatim plus its Content-Type, falling back to
// fallbackContentType when upstream sends none. Every basemap asset fetch
// (style/tilejson before rewriting, glyphs, sprite) goes through this one
// function - same non-200/transport-error/body-read-error handling as
// fetchWorldImageryUpstream and fetchCartoVectorTileUpstream in
// tile_cache.go, just without their tile-shaped URL construction.
func fetchBasemapUpstream(fetcher tileFetcher, upstreamURL, fallbackContentType string) ([]byte, string, error) {
	req, err := http.NewRequest(http.MethodGet, upstreamURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("build upstream request: %w", err)
	}
	req.Header.Set("User-Agent", "helmcentral/1.0")

	resp, err := fetcher.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("upstream request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read upstream body: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = fallbackContentType
	}
	return body, contentType, nil
}

// resolveBasemapDocument implements cache-through semantics for the four
// Carto asset classes that live in basemap_assets rather than the
// (z,x,y)-keyed tiles table (style.json, tiles.json, glyph PBFs, sprite
// files): cache hit -> serve; cache miss -> run fetch, cache its result,
// serve. fetch is a closure so each asset class supplies its own upstream-
// fetch-and-transform (style/tilejson need a JSON rewrite pass on top of
// fetchBasemapUpstream; glyphs/sprite serve fetchBasemapUpstream's result
// unmodified) while sharing one cache-hit/miss/store path. Same fail-fast
// shape as resolveCartoVectorTile in tile_cache.go: a miss that then fails
// (upstream unreachable, or - for style/tilejson - a rewrite failure
// because the upstream document didn't have the shape expected) comes back
// as an error, which every caller here turns into a 502, never a
// fabricated empty document.
func resolveBasemapDocument(cache *tileCache, cachePath string, fetch func() (data []byte, contentType string, err error)) ([]byte, string, error) {
	if data, contentType, ok, err := cache.getBasemapAsset(cachePath); err != nil {
		return nil, "", err
	} else if ok {
		return data, contentType, nil
	}

	data, contentType, err := fetch()
	if err != nil {
		return nil, "", err
	}

	if err := cache.putBasemapAsset(cachePath, data, contentType); err != nil {
		return nil, "", err
	}
	return data, contentType, nil
}

// rewriteCartoStyle rewrites exactly three URL fields in an upstream Carto
// style document to point at this same-origin proxy, and returns the
// re-marshalled document otherwise unchanged. This is the entire reason
// the style handler decodes and re-encodes JSON instead of piping upstream
// bytes straight through: everything else in the document is load-bearing
// frontend state that must survive intact -
//   - the vector source id "carto": frontend/src/components/
//     map-place-labels.tsx pins BASE_VECTOR_SOURCE_ID = 'carto' and
//     attaches every place-name label layer to a source with that exact id
//     (ADR 0066).
//   - every layer id: route-planner-map.tsx's HYBRID_HIDDEN_LAYER_IDS and
//     HYBRID_LABEL_LAYER_IDS reference Carto's own layer ids by name for
//     the hybrid-satellite restyling, and BASE_STYLE_FIRST_LAYER_ID =
//     'background' pins the first layer as a beforeId anchor (ADR 0016).
//
// A missing/wrong-shaped field at any of the three rewrite points is
// treated as an upstream-shape failure (an error, not a best-effort partial
// rewrite) - serving a style with a stale/broken glyphs or sprite URL would
// silently break label/icon rendering with no obvious cause, so this fails
// loudly instead per the repo's fallback policy.
func rewriteCartoStyle(raw []byte, styleName string) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("decode upstream style: %w", err)
	}

	sources, ok := doc["sources"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("upstream style has no sources object")
	}
	carto, ok := sources["carto"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("upstream style has no sources.carto object")
	}
	if _, ok := carto["url"].(string); !ok {
		return nil, fmt.Errorf("upstream style sources.carto has no url string")
	}
	carto["url"] = "/api/basemap/tilejson"

	if _, ok := doc["glyphs"].(string); !ok {
		return nil, fmt.Errorf("upstream style has no glyphs string")
	}
	doc["glyphs"] = "/api/basemap/fonts/{fontstack}/{range}.pbf"

	if _, ok := doc["sprite"].(string); !ok {
		return nil, fmt.Errorf("upstream style has no sprite string")
	}
	doc["sprite"] = "/api/basemap/sprite/" + styleName

	rewritten, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encode rewritten style: %w", err)
	}
	return rewritten, nil
}

// rewriteCartoTileJSON rewrites tiles.json's "tiles" array to this proxy's
// own same-origin template, leaving every other field - minzoom, maxzoom,
// attribution, bounds, vector_layers, etc. - untouched by only ever
// touching the one key. Same fail-fast shape as rewriteCartoStyle: a
// missing/wrong-shaped "tiles" field is an upstream-format error, not
// something to paper over with a best guess.
func rewriteCartoTileJSON(raw []byte) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("decode upstream tilejson: %w", err)
	}

	if _, ok := doc["tiles"].([]any); !ok {
		return nil, fmt.Errorf("upstream tilejson has no tiles array")
	}
	doc["tiles"] = []string{"/api/basemap/tiles/{z}/{x}/{y}"}

	rewritten, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encode rewritten tilejson: %w", err)
	}
	return rewritten, nil
}

// requestOrigin reconstructs the scheme://host the browser actually used to
// reach this server. Echo's c.Scheme() already honours X-Forwarded-Proto;
// X-Forwarded-Host gets the same treatment here so the value stays correct
// behind a reverse proxy. In the normal deployment there is no proxy at all
// - the backend serves the embedded frontend itself (see static.go), so the
// request Host is by construction the same origin the app was loaded from.
func requestOrigin(c echo.Context) string {
	host := c.Request().Header.Get("X-Forwarded-Host")
	if host == "" {
		host = c.Request().Host
	}
	return c.Scheme() + "://" + host
}

// absolutiseSameOrigin turns a rooted path ("/api/basemap/...") into an
// absolute URL under origin, and leaves anything else alone.
func absolutiseSameOrigin(value, origin string) string {
	if strings.HasPrefix(value, "/") {
		return origin + value
	}
	return value
}

// absolutiseStyleURLs rewrites every same-origin URL in a cached style
// document (sprite, glyphs, and each source's url) to an absolute URL under
// origin.
//
// Done at serve time rather than baked into the cache because the two
// requirements pull in opposite directions. MapLibre needs these absolute:
//
//   - sprite is validated up front and a relative value is rejected outright
//     ("Invalid sprite URL ..., must be absolute"), aborting the entire style
//     load so no source is ever registered.
//   - glyphs and the vector tile template are fetched from a Web Worker,
//     which has no document to resolve a relative path against - a relative
//     value fails at request construction ("Failed to construct 'Request'")
//     and every tile silently fails to load, leaving a blank basemap.
//
// The cache, meanwhile, needs them relative: an absolute URL bakes in one
// hostname, and this dashboard is reached on several (localhost in dev, the
// boat's LAN IP, Tailscale). Storing the relative form keeps a single cache
// entry correct on all of them and confines the host-dependent part to the
// one place that actually knows the answer, which is the request.
func absolutiseStyleURLs(raw []byte, origin string) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("decode cached style: %w", err)
	}

	sprite, ok := doc["sprite"].(string)
	if !ok {
		return nil, fmt.Errorf("cached style has no sprite string")
	}
	doc["sprite"] = absolutiseSameOrigin(sprite, origin)

	glyphs, ok := doc["glyphs"].(string)
	if !ok {
		return nil, fmt.Errorf("cached style has no glyphs string")
	}
	doc["glyphs"] = absolutiseSameOrigin(glyphs, origin)

	if sources, ok := doc["sources"].(map[string]any); ok {
		for _, src := range sources {
			srcMap, ok := src.(map[string]any)
			if !ok {
				continue
			}
			if u, ok := srcMap["url"].(string); ok {
				srcMap["url"] = absolutiseSameOrigin(u, origin)
			}
		}
	}

	out, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encode style: %w", err)
	}
	return out, nil
}

// absolutiseTileJSONTiles does the same for the TileJSON's tiles array. This
// is the one that actually matters most: the tile template is consumed
// entirely inside MapLibre's Web Worker, so a relative value there means not
// a single vector tile ever loads.
func absolutiseTileJSONTiles(raw []byte, origin string) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("decode cached tilejson: %w", err)
	}

	tiles, ok := doc["tiles"].([]any)
	if !ok {
		return nil, fmt.Errorf("cached tilejson has no tiles array")
	}
	absolute := make([]string, 0, len(tiles))
	for _, t := range tiles {
		u, ok := t.(string)
		if !ok {
			return nil, fmt.Errorf("cached tilejson has a non-string tiles entry")
		}
		absolute = append(absolute, absolutiseSameOrigin(u, origin))
	}
	doc["tiles"] = absolute

	out, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encode tilejson: %w", err)
	}
	return out, nil
}

// basemapStyleHandler is the GET /api/basemap/style/:name handler factory.
// :name must be "positron" or "dark-matter" (cartoStyleUpstreamURLs' SSRF-
// guard allowlist above) - anything else is a 400, not a proxied fetch.
func basemapStyleHandler(cache *tileCache, fetcher tileFetcher) echo.HandlerFunc {
	return func(c echo.Context) error {
		name := c.Param("name")
		upstreamURL, ok := cartoStyleUpstreamURLs[name]
		if !ok {
			return c.NoContent(http.StatusBadRequest)
		}

		cachePath := "/api/basemap/style/" + name
		data, contentType, err := resolveBasemapDocument(cache, cachePath, func() ([]byte, string, error) {
			raw, _, fetchErr := fetchBasemapUpstream(fetcher, upstreamURL, "application/json")
			if fetchErr != nil {
				return nil, "", fetchErr
			}
			rewritten, rewriteErr := rewriteCartoStyle(raw, name)
			if rewriteErr != nil {
				return nil, "", rewriteErr
			}
			return rewritten, "application/json; charset=utf-8", nil
		})
		if err != nil {
			// Fail-fast: nothing cached and upstream/rewrite failed is a
			// real outage, surfaced as 502 with the real reason - never a
			// fabricated empty style, which would leave both maps
			// silently blank with no way to tell why.
			return c.JSON(http.StatusBadGateway, map[string]string{"error": "basemap style unavailable: " + err.Error()})
		}

		absolute, err := absolutiseStyleURLs(data, requestOrigin(c))
		if err != nil {
			return c.JSON(http.StatusBadGateway, map[string]string{"error": "basemap style unusable: " + err.Error()})
		}

		c.Response().Header().Set("Cache-Control", basemapDocumentCacheControl)
		return c.Blob(http.StatusOK, contentType, absolute)
	}
}

// basemapTileJSONHandler is the GET /api/basemap/tilejson handler factory.
// One handler, one cache entry: both styles' rewritten sources.carto.url
// point at this same path, matching the one upstream TileJSON document
// both share.
func basemapTileJSONHandler(cache *tileCache, fetcher tileFetcher) echo.HandlerFunc {
	return func(c echo.Context) error {
		const cachePath = "/api/basemap/tilejson"
		data, contentType, err := resolveBasemapDocument(cache, cachePath, func() ([]byte, string, error) {
			raw, _, fetchErr := fetchBasemapUpstream(fetcher, cartoTileJSONUpstreamURL, "application/json")
			if fetchErr != nil {
				return nil, "", fetchErr
			}
			rewritten, rewriteErr := rewriteCartoTileJSON(raw)
			if rewriteErr != nil {
				return nil, "", rewriteErr
			}
			return rewritten, "application/json; charset=utf-8", nil
		})
		if err != nil {
			return c.JSON(http.StatusBadGateway, map[string]string{"error": "basemap tilejson unavailable: " + err.Error()})
		}

		absolute, err := absolutiseTileJSONTiles(data, requestOrigin(c))
		if err != nil {
			return c.JSON(http.StatusBadGateway, map[string]string{"error": "basemap tilejson unusable: " + err.Error()})
		}

		c.Response().Header().Set("Cache-Control", basemapDocumentCacheControl)
		return c.Blob(http.StatusOK, contentType, absolute)
	}
}

// basemapVectorTileHandler is the GET /api/basemap/tiles/:z/:x/:y handler
// factory, backed by resolveCartoVectorTile (tile_cache.go) - deliberately
// NOT resolveWorldImageryTile; see that function's doc comment for why
// mixing them up would corrupt every vector tile it degrades.
func basemapVectorTileHandler(cache *tileCache, fetcher tileFetcher) echo.HandlerFunc {
	return func(c echo.Context) error {
		z := strings.TrimSpace(c.Param("z"))
		x := strings.TrimSpace(c.Param("x"))
		y := strings.TrimSpace(c.Param("y"))
		if z == "" || x == "" || y == "" {
			return c.NoContent(http.StatusBadRequest)
		}

		zInt, zErr := strconv.Atoi(z)
		xInt, xErr := strconv.Atoi(x)
		yInt, yErr := strconv.Atoi(y)
		if zErr != nil || xErr != nil || yErr != nil {
			return c.NoContent(http.StatusBadRequest)
		}

		// Reject, don't clamp: see cartoBasemapMaxZoom's doc comment above
		// for why serving a shallower tile's bytes under this deeper key
		// would be exactly the coordinate-mismatch hazard this feature
		// exists to avoid, just moved from the cache into the handler. A
		// compliant MapLibre client never sends z>14 in the first place
		// (it overzooms client-side instead), so this only ever fires
		// against a misbehaving client or a manual request.
		if zInt < 0 || zInt > cartoBasemapMaxZoom {
			return c.NoContent(http.StatusBadRequest)
		}

		// x and y are only meaningful in [0, 2^z-1] for this z - the same
		// reject-don't-clamp reasoning as the zoom check above, one level
		// down. strconv.Atoi happily parses a negative coordinate, and
		// Go's % keeps the sign of its dividend, so an unvalidated
		// negative x or y used to reach
		// cartoVectorTileHosts[(x+y)%len(cartoVectorTileHosts)]
		// (tile_cache.go's fetchCartoVectorTileUpstream) as a negative
		// slice index and panic - caught by middleware.Recover() as a
		// 500, but still an unvalidated-input panic reachable by anyone.
		// An x/y past the top of the range is equally meaningless: there
		// is no "nearest valid tile" for it that isn't actually some
		// other, wrong, place.
		maxIndex := 1 << uint(zInt)
		if xInt < 0 || yInt < 0 || xInt >= maxIndex || yInt >= maxIndex {
			return c.NoContent(http.StatusBadRequest)
		}

		data, contentType, err := resolveCartoVectorTile(cache, fetcher, zInt, xInt, yInt)
		if err != nil {
			// Nothing cached and upstream failed/unreachable: 404, not
			// 502. MapLibre already treats a missing vector tile as "no
			// data here" and simply doesn't draw it, which is the correct
			// behavior for genuinely absent data - unlike the style/
			// tilejson documents above, where the same situation is a
			// real outage worth surfacing loudly.
			return c.NoContent(http.StatusNotFound)
		}

		c.Response().Header().Set("Cache-Control", basemapImmutableCacheControl)
		return c.Blob(http.StatusOK, contentType, data)
	}
}

// basemapFontsHandler is the GET /api/basemap/fonts/:fontstack/:range
// handler factory. MapLibre substitutes the style's "glyphs" template
// itself, producing requests like
// /api/basemap/fonts/Montserrat%20Medium%2COpen%20Sans%20Bold/0-255.pbf -
// :fontstack is a single URL-encoded path segment (commas and spaces come
// through already percent-decoded by the time Echo hands it to c.Param,
// same as any other path param), and :range arrives as "0-255.pbf" because
// the style's URL template has the literal ".pbf" glued onto {range} with
// no separating slash.
func basemapFontsHandler(cache *tileCache, fetcher tileFetcher) echo.HandlerFunc {
	return func(c echo.Context) error {
		fontstack := c.Param("fontstack")
		rangeParam := c.Param("range")
		rangeStr, hasPbfSuffix := strings.CutSuffix(rangeParam, ".pbf")
		if fontstack == "" || !hasPbfSuffix || rangeStr == "" {
			return c.NoContent(http.StatusBadRequest)
		}

		cachePath := "/api/basemap/fonts/" + fontstack + "/" + rangeStr
		data, contentType, err := resolveBasemapDocument(cache, cachePath, func() ([]byte, string, error) {
			// Re-encode for the upstream call: fontstack/rangeStr arrived
			// already percent-decoded (see the doc comment above), and
			// Carto's own server expects the same percent-encoded segment
			// a browser would have sent it directly. url.PathEscape also
			// guarantees neither value can smuggle a "/" into the request
			// path - the SSRF-relevant property this endpoint relies on
			// instead of a closed allowlist (see cartoGlyphsUpstreamHost's
			// doc comment).
			upstreamURL := fmt.Sprintf("%s/%s/%s.pbf",
				cartoGlyphsUpstreamHost, url.PathEscape(fontstack), url.PathEscape(rangeStr))
			return fetchBasemapUpstream(fetcher, upstreamURL, "application/x-protobuf")
		})
		if err != nil {
			return c.JSON(http.StatusBadGateway, map[string]string{"error": "basemap glyphs unavailable: " + err.Error()})
		}

		c.Response().Header().Set("Cache-Control", basemapImmutableCacheControl)
		return c.Blob(http.StatusOK, contentType, data)
	}
}

// basemapSpriteHandler is the GET /api/basemap/sprite/:name handler
// factory. :name carries its own suffix (e.g. "positron.json",
// "positron@2x.png", "dark-matter.png") and must match one of the 8 legal
// combinations in cartoSpriteUpstreamURLs' SSRF-guard allowlist above -
// anything else is a 400, not a proxied fetch.
func basemapSpriteHandler(cache *tileCache, fetcher tileFetcher) echo.HandlerFunc {
	return func(c echo.Context) error {
		name := c.Param("name")
		upstreamURL, ok := cartoSpriteUpstreamURLs[name]
		if !ok {
			return c.NoContent(http.StatusBadRequest)
		}

		fallbackContentType := "image/png"
		if strings.HasSuffix(name, ".json") {
			fallbackContentType = "application/json"
		}

		cachePath := "/api/basemap/sprite/" + name
		data, contentType, err := resolveBasemapDocument(cache, cachePath, func() ([]byte, string, error) {
			return fetchBasemapUpstream(fetcher, upstreamURL, fallbackContentType)
		})
		if err != nil {
			return c.JSON(http.StatusBadGateway, map[string]string{"error": "basemap sprite unavailable: " + err.Error()})
		}

		c.Response().Header().Set("Cache-Control", basemapImmutableCacheControl)
		return c.Blob(http.StatusOK, contentType, data)
	}
}
