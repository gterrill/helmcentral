package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// cartoBasemapMaxZoom is the deepest zoom level Carto's vector tile service
// serves, taken from the upstream style's own source "maxzoom" (15; a test
// pins the two together against a captured copy of the real style).
// MapLibre overzooms client-side past a source's declared maxzoom - it keeps
// re-showing the deepest tile it already fetched, scaled up, rather than
// requesting one that doesn't exist - so a well-behaved client never asks
// this proxy for anything deeper. basemapVectorTileHandler enforces the cap
// anyway: rejecting a deeper request outright is the only safe option,
// because "clamp" here would mean handing back a shallower tile's bytes
// under the deeper request key, which is exactly the MVT tile-local-
// coordinate mismatch resolveCartoVectorTile's doc comment (tile_cache.go)
// explains - moved up one layer, from "wrong cached bytes" to "wrong served
// bytes," but the same hazard either way.
const cartoBasemapMaxZoom = 15

// Cache-Control policy for the two shapes of basemap response. The server-
// side cache (basemap_assets / tiles table) never expires either way - see
// tileCache's doc comment - these headers only govern how long a browser
// keeps its own copy before asking again:
//   - tiles/glyphs/sprite are immutable in practice (a given z/x/y tile, or
//     a given font-range PBF, or a given theme's sprite sheet, is the same
//     bytes forever from Carto's point of view; upstream itself sends a
//     long max-age on all three), so browsers get a long one.
//   - style.json is the one part of this proxy that could plausibly change
//     server-side (an app deploy that alters what gets rewritten), and it
//     is a handful of KB - cheap enough to let the browser re-check often.
const basemapImmutableCacheControl = "public, max-age=31536000, immutable"
const basemapStyleCacheControl = "private, max-age=300"

// basemapAssetPrefix namespaces every basemap_assets row this schema
// generation writes, and cartoBasemapSource (tile_cache.go) namespaces the
// tiles. The Carto Protomaps styles (ADR 0201) are a different tile schema
// from the OpenMapTiles-shaped v1 ones, so rows cached before the switch
// must never be served for it; newTileCache deletes anything outside this
// prefix.
const basemapAssetPrefix = "/api/basemap/v2/"

// cartoStyleUpstreamURLs is a closed allowlist from this proxy's own
// :name path param to the one upstream URL it's allowed to mean. This is
// an SSRF guard, not just tidiness: :name comes straight from the request,
// and if it were interpolated into an upstream URL template this endpoint
// would let any caller make the server fetch-and-cache an arbitrary path
// under that host. Looking name up in this fixed map instead means anything
// not already in it never reaches http.NewRequest at all - see
// basemapStyleHandler.
var cartoStyleUpstreamURLs = map[string]string{
	"positron":    "https://basemaps.cartocdn.com/gl/positron-gl-style/style.json",
	"dark-matter": "https://basemaps.cartocdn.com/gl/dark-matter-gl-style/style.json",
}

// cartoAllowedUpstreamHosts is the closed set of hosts this proxy will ever
// fetch from. The tile, glyph and sprite URL templates come out of the
// upstream style document, which is data we do not control, so each one is
// checked against this set (https only, no credentials, no port) by
// parseCartoUpstream before anything is cached or fetched. A style that
// points anywhere else is rejected as an upstream-shape error, so a
// poisoned or hijacked style cannot steer the server at another host.
var cartoAllowedUpstreamHosts = map[string]bool{
	"basemaps.cartocdn.com":       true,
	"tiles.basemaps.cartocdn.com": true,
}

// cartoUpstream is what this proxy needs to know from one upstream style:
// where the vector tiles, glyphs and sprite live right now. Carto puts a
// release stamp in those paths (".../v/20261001-03/...") and moves it when
// it republishes, so the paths are read from the style rather than
// hardcoded. The raw upstream style is what basemap_assets holds; this is
// parsed from it on demand.
type cartoUpstream struct {
	sourceID string
	tiles    string // contains {z} {x} {y}
	glyphs   string // contains {fontstack} {range}
	sprite   string // base URL, no extension
}

// checkCartoUpstreamURL reports whether raw is an https URL on an
// allowlisted Carto host with no credentials and no explicit port.
func checkCartoUpstreamURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("unparseable upstream URL %q: %w", raw, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("upstream URL %q is not https", raw)
	}
	if u.User != nil || u.Port() != "" || !cartoAllowedUpstreamHosts[u.Hostname()] {
		return fmt.Errorf("upstream URL %q is not on an allowed Carto host", raw)
	}
	return nil
}

// parseCartoUpstream decodes an upstream style document and extracts its
// single vector source's tile template, plus the glyphs and sprite URLs,
// validating every one against the host allowlist. It returns the decoded
// document too so rewriteCartoStyle does not parse twice. Anything unexpected
// is an error (fail-fast, repo fallback policy): a style we half understand
// would draw a blank or subtly wrong chart with no obvious cause.
func parseCartoUpstream(raw []byte) (*cartoUpstream, map[string]any, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, nil, fmt.Errorf("decode upstream style: %w", err)
	}

	sources, ok := doc["sources"].(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("upstream style has no sources object")
	}
	var up cartoUpstream
	vectorCount := 0
	for id, s := range sources {
		src, ok := s.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := src["type"].(string); t != "vector" {
			continue
		}
		vectorCount++
		up.sourceID = id
	}
	if vectorCount != 1 {
		return nil, nil, fmt.Errorf("upstream style has %d vector sources, want exactly 1", vectorCount)
	}
	src := sources[up.sourceID].(map[string]any)

	tiles, ok := src["tiles"].([]any)
	if !ok || len(tiles) != 1 {
		return nil, nil, fmt.Errorf("upstream style source %q needs a tiles array with exactly one template", up.sourceID)
	}
	if up.tiles, ok = tiles[0].(string); !ok {
		return nil, nil, fmt.Errorf("upstream style source %q has a non-string tiles entry", up.sourceID)
	}
	for _, ph := range []string{"{z}", "{x}", "{y}"} {
		if !strings.Contains(up.tiles, ph) {
			return nil, nil, fmt.Errorf("upstream tile template %q lacks %s", up.tiles, ph)
		}
	}

	if up.glyphs, ok = doc["glyphs"].(string); !ok {
		return nil, nil, fmt.Errorf("upstream style has no glyphs string")
	}
	for _, ph := range []string{"{fontstack}", "{range}"} {
		if !strings.Contains(up.glyphs, ph) {
			return nil, nil, fmt.Errorf("upstream glyphs template %q lacks %s", up.glyphs, ph)
		}
	}
	if up.sprite, ok = doc["sprite"].(string); !ok {
		return nil, nil, fmt.Errorf("upstream style has no sprite string")
	}

	for _, u := range []string{up.tiles, up.glyphs, up.sprite} {
		if err := checkCartoUpstreamURL(u); err != nil {
			return nil, nil, err
		}
	}
	return &up, doc, nil
}

// cartoUpstreamAssetPath is where the raw upstream style for a theme is
// cached. Everything the proxy fetches later (tiles, glyphs, sprites) is
// derived from it, so the cached copy is the single record of which release
// of Carto's data this install is on.
func cartoUpstreamAssetPath(name string) string {
	return basemapAssetPrefix + "upstream/" + name
}

// cartoUpstreamRaw returns the raw upstream style for a theme from the
// basemap_assets cache, fetching and validating it on a miss. name must be
// a key of cartoStyleUpstreamURLs; callers have already checked that.
func cartoUpstreamRaw(cache *tileCache, fetcher tileFetcher, name string) ([]byte, error) {
	upstreamURL, ok := cartoStyleUpstreamURLs[name]
	if !ok {
		return nil, fmt.Errorf("unknown basemap style %q", name)
	}
	raw, _, err := resolveBasemapDocument(cache, cartoUpstreamAssetPath(name), func() ([]byte, string, error) {
		body, _, fetchErr := fetchBasemapUpstream(fetcher, upstreamURL, "application/json")
		if fetchErr != nil {
			return nil, "", fetchErr
		}
		// Validate before caching: a document we cannot use must not be
		// stored, or every later request would fail on the stored copy.
		if _, _, parseErr := parseCartoUpstream(body); parseErr != nil {
			return nil, "", parseErr
		}
		return body, "application/json; charset=utf-8", nil
	})
	return raw, err
}

// loadCartoUpstream returns a theme's parsed templates. They are held in
// memory (safe for concurrent use) so a tile miss does not re-read and
// re-parse the cached style JSON; the first use per theme loads them from
// basemap_assets, fetching the style if the cache has none yet.
func loadCartoUpstream(cache *tileCache, fetcher tileFetcher, name string) (*cartoUpstream, error) {
	cache.upstreamTemplatesMu.RLock()
	up := cache.upstreamTemplates[name]
	cache.upstreamTemplatesMu.RUnlock()
	if up != nil {
		return up, nil
	}
	raw, err := cartoUpstreamRaw(cache, fetcher, name)
	if err != nil {
		return nil, err
	}
	up, _, err = parseCartoUpstream(raw)
	if err != nil {
		return nil, err
	}
	return cache.rememberCartoTemplates(name, up), nil
}

// rememberCartoTemplates stores lazily loaded templates unless some other
// goroutine already stored some - a slow loader that read the cached style
// before a refresh replaced it must not put the old templates back. It
// returns the templates now held.
func (tc *tileCache) rememberCartoTemplates(name string, up *cartoUpstream) *cartoUpstream {
	tc.upstreamTemplatesMu.Lock()
	defer tc.upstreamTemplatesMu.Unlock()
	if existing := tc.upstreamTemplates[name]; existing != nil {
		return existing
	}
	if tc.upstreamTemplates == nil {
		tc.upstreamTemplates = map[string]*cartoUpstream{}
	}
	tc.upstreamTemplates[name] = up
	return up
}

// setCartoTemplates replaces the held templates (a refresh found new ones).
func (tc *tileCache) setCartoTemplates(name string, up *cartoUpstream) {
	tc.upstreamTemplatesMu.Lock()
	if tc.upstreamTemplates == nil {
		tc.upstreamTemplates = map[string]*cartoUpstream{}
	}
	tc.upstreamTemplates[name] = up
	tc.upstreamTemplatesMu.Unlock()
}

// basemapStyleError marks a failure to obtain or refresh the upstream style
// the tile, glyph and sprite templates come from. It is an outage of the
// proxy's own prerequisite, never "this tile does not exist".
type basemapStyleError struct{ err error }

func (e *basemapStyleError) Error() string { return e.err.Error() }
func (e *basemapStyleError) Unwrap() error { return e.err }

// upstreamStatusError is a clean non-200 answer from upstream, as opposed to
// a transport failure. fetchWithStampRecovery keys off 404 and 410.
type upstreamStatusError struct{ status int }

func (e *upstreamStatusError) Error() string {
	return fmt.Sprintf("upstream returned status %d", e.status)
}

// Refetch pacing per theme. After a refetch that completed (templates
// changed or not) the next one waits cartoUpstreamRefreshInterval: a real
// rotation is fixed by the first refetch, and the wait only stops a run of
// genuinely missing tiles (which also 404) from hammering the style URL.
// After a refetch that failed, a short backoff applies instead, so one bad
// moment (uplink blip, upstream 5xx) cannot block recovery for minutes.
const (
	cartoUpstreamRefreshInterval = 5 * time.Minute
	cartoUpstreamRetryBackoff    = 30 * time.Second
)

// cartoRefreshState is when a theme may next be refetched, and the error the
// last refetch ended with (nil if it completed). Inside the window callers
// get that error back, so a failed refresh keeps reading as a failure.
type cartoRefreshState struct {
	next time.Time
	err  error
}

func sameCartoUpstream(a, b *cartoUpstream) bool {
	return a.sourceID == b.sourceID && a.tiles == b.tiles && a.glyphs == b.glyphs && a.sprite == b.sprite
}

// refreshCartoUpstream refetches a theme's upstream style, validates it
// exactly as the first fetch was, and, when its templates differ from the
// known ones, replaces the cached copy and the in-memory templates, and drops
// the cached glyph and sprite files that belong to the old stamp (cached
// vector tiles stay: same v2 schema, and they are the offline chart). It
// returns the templates now in force. Concurrent callers share one refetch
// (singleflight); pacing is described at cartoUpstreamRefreshInterval. A
// failed or rejected refetch is logged once and returned, and leaves the
// cache untouched.
func refreshCartoUpstream(cache *tileCache, fetcher tileFetcher, theme string) (*cartoUpstream, error) {
	v, err, _ := cache.upstreamRefreshGroup.Do(theme, func() (any, error) {
		current, err := loadCartoUpstream(cache, fetcher, theme)
		if err != nil {
			return nil, err
		}

		now := cache.clock()
		cache.upstreamRefreshMu.Lock()
		if st, ok := cache.upstreamRefreshNext[theme]; ok && now.Before(st.next) {
			cache.upstreamRefreshMu.Unlock()
			return current, st.err
		}
		cache.upstreamRefreshMu.Unlock()

		fresh, err := fetchAndStoreCartoUpstream(cache, fetcher, theme, current)

		st := cartoRefreshState{next: now.Add(cartoUpstreamRefreshInterval)}
		if err != nil {
			err = fmt.Errorf("style refresh failed: %w", err)
			log.Printf("basemap: refreshing the Carto %s style failed: %v", theme, err)
			st = cartoRefreshState{next: now.Add(cartoUpstreamRetryBackoff), err: err}
		}
		cache.upstreamRefreshMu.Lock()
		if cache.upstreamRefreshNext == nil {
			cache.upstreamRefreshNext = map[string]cartoRefreshState{}
		}
		cache.upstreamRefreshNext[theme] = st
		cache.upstreamRefreshMu.Unlock()
		if err != nil {
			return nil, err
		}
		return fresh, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*cartoUpstream), nil
}

func fetchAndStoreCartoUpstream(cache *tileCache, fetcher tileFetcher, theme string, current *cartoUpstream) (*cartoUpstream, error) {
	body, _, err := fetchBasemapUpstream(fetcher, cartoStyleUpstreamURLs[theme], "application/json")
	if err != nil {
		return nil, err
	}
	fresh, _, err := parseCartoUpstream(body)
	if err != nil {
		return nil, err
	}
	if sameCartoUpstream(current, fresh) {
		return current, nil
	}
	if err := cache.putBasemapAsset(cartoUpstreamAssetPath(theme), body, "application/json; charset=utf-8"); err != nil {
		return nil, err
	}
	cache.setCartoTemplates(theme, fresh)
	if current.sprite != fresh.sprite {
		if err := cache.deleteBasemapAssetsWithPrefix(basemapAssetPrefix + "sprite/" + theme); err != nil {
			return nil, err
		}
	}
	// Fonts are fetched from the light style's glyph template only.
	if theme == "positron" && current.glyphs != fresh.glyphs {
		if err := cache.deleteBasemapAssetsWithPrefix(basemapAssetPrefix + "fonts/"); err != nil {
			return nil, err
		}
	}
	log.Printf("basemap: Carto %s style moved from %s to %s", theme, current.tiles, fresh.tiles)
	return fresh, nil
}

// fetchWithStampRecovery runs attempt against a theme's upstream templates.
// Carto puts a release stamp in those URLs and retires old ones, so a 404 or
// 410 may mean the stamp moved rather than that the asset is missing. On
// either, the style is refetched (refreshCartoUpstream: allowlist checked,
// single-flight, paced) and, if its templates changed, attempt runs once more
// with the new ones. If the templates are unchanged the original error (the
// genuine 404/410) is returned. If the refetch failed, the returned error
// says so and carries the reason: the 404 is then unproven, so it is not
// reported as one. A missing style is likewise a basemapStyleError. Nothing
// stands in for the asset.
func fetchWithStampRecovery(cache *tileCache, fetcher tileFetcher, theme string, attempt func(up *cartoUpstream) ([]byte, string, error)) ([]byte, string, error) {
	up, err := loadCartoUpstream(cache, fetcher, theme)
	if err != nil {
		return nil, "", &basemapStyleError{err}
	}
	data, contentType, firstErr := attempt(up)
	if firstErr == nil {
		return data, contentType, nil
	}
	var status *upstreamStatusError
	if !errors.As(firstErr, &status) || (status.status != http.StatusNotFound && status.status != http.StatusGone) {
		return nil, "", firstErr
	}
	fresh, err := refreshCartoUpstream(cache, fetcher, theme)
	if err != nil {
		return nil, "", &basemapStyleError{fmt.Errorf("%v; %w", firstErr, err)}
	}
	if sameCartoUpstream(up, fresh) {
		return nil, "", firstErr
	}
	return attempt(fresh)
}

// cartoGlyphsUpstreamURL expands the upstream glyph template for one font
// stack and range. The template's host was validated in parseCartoUpstream;
// url.PathEscape guarantees neither param can inject a "/" and escape the
// {fontstack}/{range}.pbf path shape onto some other upstream path.
func cartoGlyphsUpstreamURL(up *cartoUpstream, fontstack, rangeStr string) string {
	r := strings.NewReplacer("{fontstack}", url.PathEscape(fontstack), "{range}", url.PathEscape(rangeStr))
	return r.Replace(up.glyphs)
}

// cartoSpriteSuffixes are the four files MapLibre asks for from a sprite
// base URL: it appends ".json" / ".png" (and "@2x" before the extension on
// high-DPI screens) itself. Together with the two theme names this is the
// closed 8-name set basemapSpriteHandler accepts, the same SSRF guard the
// old per-file URL map was.
var cartoSpriteSuffixes = []string{".json", ".png", "@2x.json", "@2x.png"}

// splitCartoSpriteName splits "dark-matter@2x.png" into ("dark-matter",
// "@2x.png"), accepting only allowlisted themes and suffixes.
func splitCartoSpriteName(name string) (theme, suffix string, ok bool) {
	for _, s := range cartoSpriteSuffixes {
		if t, found := strings.CutSuffix(name, s); found {
			if _, known := cartoStyleUpstreamURLs[t]; known {
				return t, s, true
			}
		}
	}
	return "", "", false
}

// fetchBasemapUpstream performs one GET against upstreamURL and returns the
// response body verbatim plus its Content-Type, falling back to
// fallbackContentType when upstream sends none. Every basemap asset fetch
// (upstream style, glyphs, sprite) goes through this one
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
		return nil, "", &upstreamStatusError{status: resp.StatusCode}
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

// resolveBasemapDocument implements cache-through semantics for the Carto
// asset classes that live in basemap_assets rather than the (z,x,y)-keyed
// tiles table (the upstream style, glyph PBFs, sprite files): cache hit ->
// serve; cache miss -> run fetch, cache its result, serve. fetch is a
// closure so each asset class supplies its own upstream fetch (the style
// adds a validation pass on top of fetchBasemapUpstream; glyphs/sprite serve
// its result unmodified) while sharing one cache-hit/miss/store path. Same
// fail-fast shape as resolveCartoVectorTile in tile_cache.go: a miss that
// then fails (upstream unreachable, or - for the style - a document that
// didn't have the shape expected) comes back
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

// buildCartoStyle turns a cached upstream Carto style into the document this
// server hands the browser: the vector source's tiles array, the glyphs URL
// and the sprite URL are pointed at this same-origin proxy, and everything
// else is returned unchanged. That is the entire reason the style handler
// decodes and re-encodes JSON instead of piping upstream bytes through:
// everything else in the document is load-bearing frontend state that must
// survive intact -
//   - the vector source id: frontend/src/components/map-place-labels.tsx
//     pins BASE_VECTOR_SOURCE_ID to the id the upstream style uses and
//     attaches every place-name label layer to a source with that exact id
//     (ADR 0066). The id is left as upstream names it.
//   - every layer id: BASE_STYLE_FIRST_LAYER_ID = 'background' pins the
//     first layer as a beforeId anchor (ADR 0016).
//
// The URLs are made absolute under origin at serve time, not stored that
// way: MapLibre rejects a relative sprite ("must be absolute") and aborts
// the whole style load, and glyphs and the tile template are fetched from a
// Web Worker that has no document to resolve a relative path against. The
// dashboard is reached on several hostnames (localhost, the boat's LAN IP,
// Tailscale), so only the request knows the right origin. The upstream
// style that is cached is host-independent.
//
// A missing or wrong-shaped field is an error (see parseCartoUpstream), never
// a best-effort partial rewrite: serving a style with a stale or broken URL
// would silently break the chart with no obvious cause.
func buildCartoStyle(rawUpstream []byte, styleName, origin string) ([]byte, error) {
	up, doc, err := parseCartoUpstream(rawUpstream)
	if err != nil {
		return nil, err
	}

	src := doc["sources"].(map[string]any)[up.sourceID].(map[string]any)
	delete(src, "url")
	src["tiles"] = []string{origin + "/api/basemap/tiles/{z}/{x}/{y}"}
	doc["glyphs"] = origin + "/api/basemap/fonts/{fontstack}/{range}.pbf"
	doc["sprite"] = origin + "/api/basemap/sprite/" + styleName

	rewritten, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encode rewritten style: %w", err)
	}
	return rewritten, nil
}

// forwardedHostPattern is a plain DNS name, IPv4 address or bracketed IPv6
// address, with an optional numeric port. Anything else in X-Forwarded-Host
// (paths, credentials, quotes, whitespace) is ignored, because the value ends
// up inside URLs in the served style.
var forwardedHostPattern = regexp.MustCompile(`^(?:[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?|\[[0-9A-Fa-f:.]{2,45}\])(?::[0-9]{1,5})?$`)

// firstForwardedValue returns the first comma-separated element of a
// forwarding header, trimmed.
func firstForwardedValue(v string) string {
	first, _, _ := strings.Cut(v, ",")
	return strings.TrimSpace(first)
}

// requestOrigin reconstructs the scheme://host the browser actually used to
// reach this server, honouring X-Forwarded-Proto and X-Forwarded-Host from a
// reverse proxy. Both are client-supplied unless a proxy overwrites them, so
// each is reduced to its first value and checked: the scheme must be http or
// https, the host a plain host[:port]; otherwise the header is ignored and the
// connection's own scheme and Host are used. In the normal deployment there is
// no proxy at all - the backend serves the embedded frontend itself (see
// static.go), so the request Host is by construction the same origin the app
// was loaded from.
func requestOrigin(c echo.Context) string {
	req := c.Request()
	scheme := "http"
	if req.TLS != nil {
		scheme = "https"
	}
	if proto := strings.ToLower(firstForwardedValue(req.Header.Get("X-Forwarded-Proto"))); proto == "http" || proto == "https" {
		scheme = proto
	}
	host := req.Host
	if fwd := firstForwardedValue(req.Header.Get("X-Forwarded-Host")); fwd != "" && forwardedHostPattern.MatchString(fwd) {
		host = fwd
	}
	return scheme + "://" + host
}

// basemapStyleHandler is the GET /api/basemap/style/:name handler factory.
// :name must be "positron" or "dark-matter" (cartoStyleUpstreamURLs' SSRF-
// guard allowlist above) - anything else is a 400, not a proxied fetch.
func basemapStyleHandler(cache *tileCache, fetcher tileFetcher) echo.HandlerFunc {
	return func(c echo.Context) error {
		name := c.Param("name")
		if _, ok := cartoStyleUpstreamURLs[name]; !ok {
			return c.NoContent(http.StatusBadRequest)
		}

		raw, err := cartoUpstreamRaw(cache, fetcher, name)
		if err != nil {
			// Fail-fast: nothing cached and upstream failed is a real
			// outage, surfaced as 502 with the real reason - never a
			// fabricated empty style, which would leave both maps
			// silently blank with no way to tell why.
			return c.JSON(http.StatusBadGateway, map[string]string{"error": "basemap style unavailable: " + err.Error()})
		}

		style, err := buildCartoStyle(raw, name, requestOrigin(c))
		if err != nil {
			return c.JSON(http.StatusBadGateway, map[string]string{"error": "basemap style unusable: " + err.Error()})
		}

		// The served style embeds the request's origin, so no shared cache
		// may keep it for another viewer.
		c.Response().Header().Set("Cache-Control", basemapStyleCacheControl)
		return c.Blob(http.StatusOK, "application/json; charset=utf-8", style)
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
		// compliant MapLibre client never sends z>15 in the first place
		// (it overzooms client-side instead), so this only ever fires
		// against a misbehaving client or a manual request.
		if zInt < 0 || zInt > cartoBasemapMaxZoom {
			return c.NoContent(http.StatusBadRequest)
		}

		// x and y are only meaningful in [0, 2^z-1] for this z - the same
		// reject-don't-clamp reasoning as the zoom check above, one level
		// down. strconv.Atoi happily parses a negative coordinate, which
		// would otherwise be interpolated into the upstream tile URL as a
		// tile that does not exist. An x/y past the top of the range is
		// equally meaningless: there is no "nearest valid tile" for it
		// that isn't actually some other, wrong, place.
		maxIndex := 1 << uint(zInt)
		if xInt < 0 || yInt < 0 || xInt >= maxIndex || yInt >= maxIndex {
			return c.NoContent(http.StatusBadRequest)
		}

		data, contentType, err := resolveCartoVectorTile(cache, fetcher, zInt, xInt, yInt)
		if err != nil {
			return basemapTileError(c, err)
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

		cachePath := basemapAssetPrefix + "fonts/" + fontstack + "/" + rangeStr
		data, contentType, err := resolveBasemapDocument(cache, cachePath, func() ([]byte, string, error) {
			// The glyph template comes from the cached upstream style (its
			// host already checked against the allowlist). Both themes
			// share one glyph set, so the light style's is used.
			// fontstack/rangeStr arrived already percent-decoded (see the
			// doc comment above); cartoGlyphsUpstreamURL re-encodes them as
			// single path segments, so neither can smuggle a "/" into the
			// upstream path - the SSRF-relevant property this endpoint
			// relies on instead of a closed allowlist.
			return fetchWithStampRecovery(cache, fetcher, "positron", func(up *cartoUpstream) ([]byte, string, error) {
				return fetchBasemapUpstream(fetcher, cartoGlyphsUpstreamURL(up, fontstack, rangeStr), "application/x-protobuf")
			})
		})
		if err != nil {
			return c.JSON(http.StatusBadGateway, map[string]string{"error": "basemap glyphs unavailable: " + err.Error()})
		}

		c.Response().Header().Set("Cache-Control", basemapImmutableCacheControl)
		return c.Blob(http.StatusOK, contentType, data)
	}
}

// basemapSpriteHandler is the GET /api/basemap/sprite/:name handler
// factory. The style's sprite field is a base URL with no extension, so
// MapLibre requests <base>.json, <base>.png and, on high-DPI screens,
// <base>@2x.json and <base>@2x.png. :name carries that suffix (e.g.
// "positron.json", "dark-matter@2x.png") and must be one of the 8 legal
// theme x suffix combinations (splitCartoSpriteName) - anything else is a
// 400, not a proxied fetch. The upstream base comes from that theme's
// cached upstream style.
func basemapSpriteHandler(cache *tileCache, fetcher tileFetcher) echo.HandlerFunc {
	return func(c echo.Context) error {
		name := c.Param("name")
		theme, suffix, ok := splitCartoSpriteName(name)
		if !ok {
			return c.NoContent(http.StatusBadRequest)
		}

		fallbackContentType := "image/png"
		if strings.HasSuffix(name, ".json") {
			fallbackContentType = "application/json"
		}

		cachePath := basemapAssetPrefix + "sprite/" + name
		data, contentType, err := resolveBasemapDocument(cache, cachePath, func() ([]byte, string, error) {
			return fetchWithStampRecovery(cache, fetcher, theme, func(up *cartoUpstream) ([]byte, string, error) {
				return fetchBasemapUpstream(fetcher, up.sprite+suffix, fallbackContentType)
			})
		})
		if err != nil {
			return c.JSON(http.StatusBadGateway, map[string]string{"error": "basemap sprite unavailable: " + err.Error()})
		}

		c.Response().Header().Set("Cache-Control", basemapImmutableCacheControl)
		return c.Blob(http.StatusOK, contentType, data)
	}
}

// basemapTileError maps a failed vector tile resolve to a response. MapLibre
// treats a 404 tile as "no data here" and draws nothing, silently, so 404 is
// kept for the cases where that is true:
//   - upstream answered 404 or 410 for this tile, after the release-stamp
//     recovery in fetchWithStampRecovery had its chance;
//   - upstream could not be reached at all (offline with this tile not
//     cached, or the circuit breaker open). Nothing is wrong with the
//     proxy; the tile is simply not available.
//
// Everything else is a fault that must not look like empty sea: a local
// cache error is a 500; a missing, unusable or unrefreshable upstream style,
// or any other upstream status, is a 502 with the reason.
func basemapTileError(c echo.Context, err error) error {
	var local *localCacheError
	if errors.As(err, &local) {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "basemap cache failed: " + err.Error()})
	}
	var styleErr *basemapStyleError
	if errors.As(err, &styleErr) {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "basemap style unavailable: " + err.Error()})
	}
	var status *upstreamStatusError
	if errors.As(err, &status) {
		if status.status == http.StatusNotFound || status.status == http.StatusGone {
			return c.NoContent(http.StatusNotFound)
		}
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "basemap tile unavailable: " + err.Error()})
	}
	var transport *tileTransportError
	if errors.As(err, &transport) || errors.Is(err, errTileCircuitBreakerOpen) {
		return c.NoContent(http.StatusNotFound)
	}
	return c.JSON(http.StatusBadGateway, map[string]string{"error": "basemap tile unavailable: " + err.Error()})
}
