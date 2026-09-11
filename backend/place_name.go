package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// ── Overpass query ladder ────────────────────────────────────────────────

// placeNameRadiiMeters is the widening ladder, tightest ring first.
// Overpass's around: filter measures distance to an element's linework, not
// containment, so a point well inside a wide bay can miss a tight ring -
// widening is mandatory, not optional (confirmed live: the 400m ring at
// Lindeman Island is empty even though the point sits on the island). The
// ladder stops at the first non-empty ring: ranking a set of one is sound,
// ranking a set of thirteen (the 5000m ring at Lindeman) is guesswork. See
// docs/adr/0056.
var placeNameRadiiMeters = []int{400, 1500, 5000}

const defaultOverpassAPIURL = "https://overpass-api.de/api/interpreter"
const overpassTimeout = 20 * time.Second

// overpassAPIURL is the Overpass endpoint used by both place-name
// resolution (this file) and the assistant's find_places tool
// (assistant_tools.go), via the shared postOverpassQuery below. It is
// resolved once at startup by resolveOverpassAPIURL from OVERPASS_API_URL
// (main.go's main), and initialised to the default here so tests that never
// run main() still get it.
var overpassAPIURL = defaultOverpassAPIURL

// resolveOverpassAPIURL reads the optional OVERPASS_API_URL environment
// variable, defaulting to defaultOverpassAPIURL when raw is blank or
// whitespace-only - an unset OVERPASS_API_URL is the normal case, not a
// mistake. A non-blank value must parse as an absolute https URL with a
// host; this mirrors the POI plugin's resolveOverpassURL
// (docs/examples/poi-plugins/osm-overpass/osm-overpass.go) validation
// exactly, including never falling back to the default silently on a
// malformed value - a set-but-broken OVERPASS_API_URL almost certainly did
// not mean "use overpass-api.de", so it surfaces as an error instead, per
// AGENTS.md's fail-fast / no-masking-fallback policy.
func resolveOverpassAPIURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return defaultOverpassAPIURL, nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", fmt.Errorf("OVERPASS_API_URL: must be an absolute https URL, got %q", raw)
	}
	return trimmed, nil
}

// overpassFetcher is the minimal interface place name resolution needs from
// an HTTP client, mirroring tileFetcher (tile_cache.go:60) so tests can
// inject a fake upstream. *http.Client already satisfies this interface.
type overpassFetcher interface {
	Do(req *http.Request) (*http.Response, error)
}

// overpassHTTPClient is the production overpassFetcher used by the server
// poll tick (tracks.go) and the anchor-watch resolver (anchor.go). Tests
// swap it for a fake and restore it afterward.
var overpassHTTPClient overpassFetcher = &http.Client{Timeout: overpassTimeout}

// featureRank orders candidate kinds within a winning ring: anchorage first
// (a human already decided this is where you anchor), then bay, then
// island/islet/rock by descending size-implication. A tagged element whose
// kind isn't one of these five is discarded before ranking, as is any
// element with no name tag.
var featureRank = map[string]int{
	"anchorage": 0,
	"bay":       1,
	"island":    2,
	"islet":     3,
	"rock":      4,
}

// overpassLatLon is the {lat,lon} shape Overpass emits for a way/relation's
// "out center" point.
type overpassLatLon struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// overpassElement is one member of an Overpass "out tags center" response.
// Node elements carry lat/lon directly; way/relation elements carry it
// under center instead (see elementLatLon). "out tags center" is used
// deliberately, not "out geom": geometry for a mainland coastline relation
// is enormous, and around: has already done the geometric filtering
// server-side, so only the tags and a representative point are needed.
type overpassElement struct {
	Type   string            `json:"type"`
	ID     int64             `json:"id"`
	Lat    *float64          `json:"lat,omitempty"`
	Lon    *float64          `json:"lon,omitempty"`
	Center *overpassLatLon   `json:"center,omitempty"`
	Tags   map[string]string `json:"tags"`
}

type overpassResponse struct {
	Elements []overpassElement `json:"elements"`
}

// elementLatLon resolves a queried element's representative point.
func elementLatLon(el overpassElement) (lat, lon float64, ok bool) {
	if el.Lat != nil && el.Lon != nil {
		return *el.Lat, *el.Lon, true
	}
	if el.Center != nil {
		return el.Center.Lat, el.Center.Lon, true
	}
	return 0, 0, false
}

// featureKind classifies a tagged element into one of featureRank's keys, or
// "" if it matches none of the query's three clauses.
func featureKind(tags map[string]string) string {
	if tags["seamark:type"] == "anchorage" {
		return "anchorage"
	}
	if tags["natural"] == "bay" {
		return "bay"
	}
	switch tags["place"] {
	case "island", "islet", "rock":
		return tags["place"]
	}
	return ""
}

// buildOverpassQuery builds the Overpass QL for one ring at radiusMeters
// around (lat, lon). See docs/adr/0056 for why this exact tag set.
func buildOverpassQuery(radiusMeters int, lat, lon float64) string {
	around := fmt.Sprintf("%d,%.6f,%.6f", radiusMeters, lat, lon)
	return fmt.Sprintf(
		`[out:json][timeout:25];(nwr["seamark:type"="anchorage"](around:%s);nwr["natural"="bay"](around:%s);nwr["place"~"^(island|islet|rock)$"](around:%s););out tags center 20;`,
		around, around, around,
	)
}

// looksLikeOverpassRateLimit reports whether a non-JSON 200 response is the
// documented Overpass rate-limit signature: an HTML body (rather than the
// JSON a normal query returns), typically containing
// Dispatcher_Client::request_read_and_idx::rate_limited. Confirmed against
// the live API while capturing this feature's test fixtures - checking
// resp.StatusCode alone is not sufficient, since Overpass signals rate
// limiting with HTTP 200.
func looksLikeOverpassRateLimit(contentType string, body []byte) bool {
	if strings.Contains(strings.ToLower(contentType), "text/html") {
		return true
	}
	return bytes.Contains(body, []byte("rate_limited"))
}

// postOverpassQuery performs one POST to the Overpass API for an
// already-built query string. A transport error, non-200 status, or
// unparseable body are all reported as an error - the rate-limited case
// gets its own explicit log line so it's never confused with a legitimate
// empty result set or a generic parse failure.
//
// Split out of fetchOverpassRing (ADR 0056) so the assistant's find_places
// tool (ADR 0093, assistant_tools.go) can post its own name-search query
// through the same request-building, header and rate-limit handling without
// duplicating it - fetchOverpassRing's ring-widening ladder is specific to
// place-name resolution and has no bearing on a name search.
func postOverpassQuery(fetcher overpassFetcher, query string) ([]overpassElement, error) {
	form := url.Values{"data": {query}}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, overpassAPIURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build overpass request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "helmcentral/1.0 (+https://github.com/gterrill/helmcentral; place-name lookup)")

	resp, err := fetcher.Do(req)
	if err != nil {
		return nil, fmt.Errorf("overpass request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read overpass response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("overpass returned status %d", resp.StatusCode)
	}

	var parsed overpassResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		if looksLikeOverpassRateLimit(resp.Header.Get("Content-Type"), body) {
			// The radius that made fetchOverpassRing's version of this log
			// line meaningful doesn't exist here - a name search has no
			// ring - so the query's length stands in as the short label
			// distinguishing one rate-limited call from another in the log.
			log.Printf("place name: overpass rate-limited (HTTP 200 with a non-JSON body), query length %d bytes", len(query))
			return nil, fmt.Errorf("overpass rate limited")
		}
		return nil, fmt.Errorf("parse overpass response: %w", err)
	}

	return parsed.Elements, nil
}

// fetchOverpassRing performs one POST to the Overpass API for a single
// widening ring, delegating the request/response handling to
// postOverpassQuery (ADR 0056).
func fetchOverpassRing(fetcher overpassFetcher, radiusMeters int, lat, lon float64) ([]overpassElement, error) {
	return postOverpassQuery(fetcher, buildOverpassQuery(radiusMeters, lat, lon))
}

// bestNamedFeature ranks the named, tag-matched elements in a ring and
// returns the winner: lowest featureRank first, ties broken by great-circle
// distance to the query point via the existing haversineMeters
// (signalk.go:2113). Elements with no name tag are discarded before
// ranking, so an unnamed feature can never win.
func bestNamedFeature(elements []overpassElement, lat, lon float64) (string, bool) {
	type candidate struct {
		name string
		rank int
		dist float64
	}

	var candidates []candidate
	for _, el := range elements {
		name := strings.TrimSpace(el.Tags["name"])
		if name == "" {
			continue
		}
		kind := featureKind(el.Tags)
		rank, known := featureRank[kind]
		if !known {
			continue
		}
		elLat, elLon, ok := elementLatLon(el)
		if !ok {
			continue
		}
		candidates = append(candidates, candidate{
			name: name,
			rank: rank,
			dist: haversineMeters(lat, lon, elLat, elLon),
		})
	}
	if len(candidates) == 0 {
		return "", false
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].rank != candidates[j].rank {
			return candidates[i].rank < candidates[j].rank
		}
		return candidates[i].dist < candidates[j].dist
	})
	return candidates[0].name, true
}

// resolvePlaceName walks placeNameRadiiMeters, tightest first, and returns
// the winning named feature from the first ring with a named match. It
// never falls back to a different upstream source on failure - a transport
// error, bad status, or unparseable body all surface as an error so the
// caller can log and retry on the next tick, per AGENTS.md's fail-fast / no
// masking-fallback policy. A ladder that reaches the end with no named
// match anywhere returns ("", nil): that's a legitimate negative, not a
// failure.
func resolvePlaceName(fetcher overpassFetcher, lat, lon float64) (string, error) {
	for _, radius := range placeNameRadiiMeters {
		elements, err := fetchOverpassRing(fetcher, radius, lat, lon)
		if err != nil {
			return "", err
		}
		if name, ok := bestNamedFeature(elements, lat, lon); ok {
			return name, nil
		}
	}
	return "", nil
}

// ── Cache ────────────────────────────────────────────────────────────────

const (
	placeNameCacheTTL         = 24 * time.Hour
	placeNameCacheMaxEntries  = 512
	placeNameCacheCellDegrees = 0.005 // ~550m, matches the tightest resolution ring
)

type placeNameCacheEntry struct {
	name     string
	cachedAt time.Time
}

// placeNameCacheStore caches only successful resolutions (see put's
// doc comment) at a ~550m cell size, bounded at placeNameCacheMaxEntries by
// evicting the oldest entry on insert - the finer cell size (vs. the old
// GeoNames cache's 0.5-degree/56km cells) means many more distinct cells
// accumulate while cruising.
type placeNameCacheStore struct {
	mu    sync.Mutex
	data  map[string]placeNameCacheEntry
	order []string // insertion order, oldest first
}

var placeNameCache = &placeNameCacheStore{data: make(map[string]placeNameCacheEntry)}

// placeNameCacheKey builds the cache key for (lat, lon) at the ~550m cell
// size. This replaces the old 0.5-degree (~56km) rounding in geonames.go
// that was the direct cause of the reported bug: Goldsmith Island and
// Lindeman Island, 28.9km apart, rounded to the same cell and so shared one
// cache entry.
func placeNameCacheKey(lat, lon float64) string {
	cellLat := math.Round(lat/placeNameCacheCellDegrees) * placeNameCacheCellDegrees
	cellLon := math.Round(lon/placeNameCacheCellDegrees) * placeNameCacheCellDegrees
	return fmt.Sprintf("%.3f,%.3f", cellLat, cellLon)
}

func (s *placeNameCacheStore) get(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.data[key]
	if !ok || time.Since(entry.cachedAt) >= placeNameCacheTTL {
		return "", false
	}
	return entry.name, true
}

// put caches a successful resolution only. An Overpass hiccup must never
// pin a blank name for the TTL - that's exactly the masking fallback
// AGENTS.md rules out, so callers only reach put with a non-empty, actually
// resolved name; a failure retries on the next tick instead.
func (s *placeNameCacheStore) put(key, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.data[key]; !exists {
		s.order = append(s.order, key)
		if len(s.order) > placeNameCacheMaxEntries {
			oldest := s.order[0]
			s.order = s.order[1:]
			delete(s.data, oldest)
		}
	}
	s.data[key] = placeNameCacheEntry{name: name, cachedAt: time.Now()}
}

// resolveAndCachePlaceName is the cache-first entry point: a cache hit
// returns immediately with no upstream call; a miss resolves live and, only
// on success, caches the result. A resolution failure logs explicitly and
// returns "" without touching the cache.
func resolveAndCachePlaceName(fetcher overpassFetcher, lat, lon float64) string {
	key := placeNameCacheKey(lat, lon)
	if name, ok := placeNameCache.get(key); ok {
		return name
	}

	name, err := resolvePlaceName(fetcher, lat, lon)
	if err != nil {
		log.Printf("place name: resolve failed for %.4f,%.4f: %v", lat, lon, err)
		return ""
	}
	if name == "" {
		return ""
	}

	placeNameCache.put(key, name)
	return name
}

// ── Server-tick integration (ADR 0001: the server owns sampling) ──────────

// currentPlaceNameStore holds the most recently resolved live place name,
// updated once per server poll tick (tracks.go's sampleTracks) and read by
// the /api/place-name handler, which is now a pure cache read with no HTTP
// call of its own.
var currentPlaceNameStore = struct {
	mu      sync.RWMutex
	name    string
	cellKey string
}{}

func setCurrentPlaceName(name string) {
	currentPlaceNameStore.mu.Lock()
	currentPlaceNameStore.name = name
	currentPlaceNameStore.mu.Unlock()
}

func getCurrentPlaceName() string {
	currentPlaceNameStore.mu.RLock()
	defer currentPlaceNameStore.mu.RUnlock()
	return currentPlaceNameStore.name
}

// enterPlaceNameCell records which ~550m cell the vessel is in and what is
// known about that cell right now. Moving into a cell we have not resolved
// yet publishes "" rather than leaving the previous cell's name on display.
// Reporting a name for somewhere the vessel no longer is is the entire
// class of bug this file exists to fix, and AGENTS.md rules out masking a
// missing value with a stale one.
func enterPlaceNameCell(cellKey, name string) {
	currentPlaceNameStore.mu.Lock()
	currentPlaceNameStore.cellKey = cellKey
	currentPlaceNameStore.name = name
	currentPlaceNameStore.mu.Unlock()
}

// publishPlaceNameForCell stores name only while cellKey is still the cell
// the vessel occupies, reporting whether it did. A background resolve that
// lands after the vessel has already moved on must not overwrite the
// current cell's name with the previous cell's answer.
func publishPlaceNameForCell(cellKey, name string) bool {
	currentPlaceNameStore.mu.Lock()
	defer currentPlaceNameStore.mu.Unlock()
	if currentPlaceNameStore.cellKey != cellKey {
		return false
	}
	currentPlaceNameStore.name = name
	return true
}

// ── Background resolution: the poll tick must never block ─────────────────

// placeNameResolve is a single-flight guard for background resolution.
// startTrackPoller (tracks.go) drives sampleTracks sequentially on one
// goroutine, and Go drops ticks while that receiver is busy, so anything
// blocking in the tick stalls wind/depth/solar history, track points, the
// motoring trail and nearby-vessel contact recording behind it.
// resolvePlaceName walks up to three rings at overpassTimeout each, so a
// synchronous resolve could hold the whole poller for a minute whenever the
// tight rings come back empty (the measured Lindeman case). Resolution
// therefore runs in the background, and this guard stops a 5s tick stacking
// goroutines against a resolution that slow. A plain mutex and flag: there
// is no golang.org/x/sync dependency in go.mod and this does not warrant
// adding one.
var placeNameResolve = struct {
	mu     sync.Mutex
	active bool
}{}

// startPlaceNameResolve runs resolve on its own goroutine unless one is
// already in flight, reporting whether it started one. The in-flight flag
// is cleared with defer so a panicking or failing resolve cannot wedge it.
func startPlaceNameResolve(resolve func()) bool {
	placeNameResolve.mu.Lock()
	if placeNameResolve.active {
		placeNameResolve.mu.Unlock()
		return false
	}
	placeNameResolve.active = true
	placeNameResolve.mu.Unlock()

	go func() {
		defer func() {
			placeNameResolve.mu.Lock()
			placeNameResolve.active = false
			placeNameResolve.mu.Unlock()
		}()
		resolve()
	}()
	return true
}

// resolveAndPinAnchorWatchPlaceName resolves the place name at (lat, lon)
// and, if resolution succeeds, persists it onto the anchor watch identified
// by original - but only if that anchor watch is still the active one. An
// intervening delete or re-set (a new anchorage, a different position)
// must not have its state overwritten by a stale lookup for the previous
// position. Used both by setAnchorWatch's fire-and-forget goroutine and by
// the regular tick's retry-until-resolved path (updateTickPlaceName), so a
// failed initial resolution still gets filled in once Overpass is
// reachable again. A failure is logged inside resolveAndCachePlaceName;
// the field is simply left empty for the next tick to retry.
func resolveAndPinAnchorWatchPlaceName(original *anchorWatchData, lat, lon float64) string {
	name := resolveAndCachePlaceName(overpassHTTPClient, lat, lon)
	if name == "" {
		return ""
	}

	anchorWatchMu.Lock()
	current := anchorWatchState
	if current == nil || current != original {
		anchorWatchMu.Unlock()
		return name
	}
	updated := *current
	updated.PlaceName = name
	anchorWatchState = &updated
	anchorWatchMu.Unlock()

	if err := saveAnchorWatch(&updated); err != nil {
		log.Printf("place name: failed to persist anchor watch place name: %v", err)
	}
	return name
}

// updateTickPlaceName is called once per server poll tick (tracks.go's
// sampleTracks) with the current vessel position. It never blocks: it
// answers from what is already known and starts any needed resolution in
// the background (see placeNameResolve), because the poller is a single
// sequential goroutine and Overpass can take up to three rings x
// overpassTimeout to answer.
//
// While an anchor watch is active and already has a pinned name, live
// resolution is skipped entirely and the pinned name is served as-is, so it
// stops drifting as the boat swings or approaches (matching the anchor
// tile's own bearing/range freeze). If the watch is active but its name
// hasn't resolved yet (setAnchorWatch's resolve hasn't completed, or it
// failed), the tick retries in the background until it succeeds. With no
// anchor watch active, this serves the current cell's cached name, and
// returns "" for a cell not yet resolved rather than the previous cell's
// answer.
func updateTickPlaceName(lat, lon float64) string {
	anchorWatchMu.RLock()
	aw := anchorWatchState
	anchorWatchMu.RUnlock()

	if aw != nil {
		if aw.PlaceName != "" {
			return aw.PlaceName
		}
		startPlaceNameResolve(func() {
			resolveAndPinAnchorWatchPlaceName(aw, aw.Lat, aw.Lon)
		})
		return ""
	}

	key := placeNameCacheKey(lat, lon)
	if name, ok := placeNameCache.get(key); ok {
		enterPlaceNameCell(key, name)
		return name
	}

	enterPlaceNameCell(key, "")
	startPlaceNameResolve(func() {
		if name := resolveAndCachePlaceName(overpassHTTPClient, lat, lon); name != "" {
			publishPlaceNameForCell(key, name)
		}
	})
	return ""
}

// ── HTTP handler ────────────────────────────────────────────────────────

// placeName is the GET /api/place-name handler. It is a pure cache read:
// no HTTP call happens here. Resolution runs on the server's own 5s poll
// tick (tracks.go's sampleTracks -> updateTickPlaceName) so the cache is
// always warm regardless of whether or how often the frontend polls this
// endpoint - see docs/adr/0056. While an anchor watch is active, the
// pinned anchorWatchState.PlaceName is served instead of the tick's
// roaming value.
func placeName(c echo.Context) error {
	anchorWatchMu.RLock()
	aw := anchorWatchState
	anchorWatchMu.RUnlock()

	if aw != nil {
		return c.JSON(http.StatusOK, map[string]string{"name": aw.PlaceName})
	}
	return c.JSON(http.StatusOK, map[string]string{"name": getCurrentPlaceName()})
}
