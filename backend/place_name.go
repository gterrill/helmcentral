package main

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// ── Place-name resolution ladder (ADR 0056, ADR 0101) ───────────────────────
//
// placeNameRadiiMeters is the widening ladder, tightest ring first. A
// provider's own place_name_at typically measures distance to a feature's
// linework, not containment, so a point well inside a wide bay can miss a
// tight ring - widening is mandatory, not optional (confirmed live: the
// 400m ring at Lindeman Island is empty even though the point sits on the
// island). The ladder stops at the first ring with a name: ranking a set of
// one is sound, ranking a set of thirteen (the 5000m ring at Lindeman) is
// guesswork better left to the provider's own place_name_at, which does
// exactly that ranking itself (see docs/adr/0056, docs/adr/0101).
var placeNameRadiiMeters = []int{400, 1500, 5000}

// resolvePlaceName resolves the currently configured place-names provider
// (resolve) once, then walks placeNameRadiiMeters, tightest first, calling
// its PlaceNameAt for each ring and returning the first ring's named
// answer. It never falls back to a different provider on failure - a
// provider-resolution error or a PlaceNameAt error both surface as an error
// so the caller can log and retry on the next tick (AGENTS.md's fail-fast /
// no-masking-fallback policy). A ladder that reaches the end with no named
// match anywhere returns ("", nil): that's a legitimate negative, not a
// failure.
func resolvePlaceName(resolve placeNameProviderResolver, lat, lon float64) (string, error) {
	provider, id, err := resolve()
	if err != nil {
		return "", fmt.Errorf("place-names provider: %w", err)
	}

	for _, radius := range placeNameRadiiMeters {
		result, err := provider.PlaceNameAt(lat, lon, radius)
		if err != nil {
			return "", fmt.Errorf("place-names provider %q: %w", id, err)
		}
		if result.Name != "" {
			return result.Name, nil
		}
	}
	return "", nil
}

// ── Cache ────────────────────────────────────────────────────────────────

const (
	placeNameCacheTTL         = 24 * time.Hour
	placeNameCacheMaxEntries  = 512
	placeNameCacheCellDegrees = 0.005 // ~550m, matches the tightest resolution ring

	// placeNameEmptyCacheTTL is the TTL for a cell where the ladder ran to
	// the end and genuinely found no named feature - a real result (per
	// AGENTS.md's fail-fast policy, "no named feature here" is a legitimate
	// answer, not a masking fallback), but a much less durable one than a
	// resolved name: open water a boat is passing through today may well be
	// in range of a newly-tagged feature, or simply a different cell,
	// tomorrow. Shorter than placeNameCacheTTL so an empty cell is retried
	// well within one cruising day rather than once every 24h.
	placeNameEmptyCacheTTL = 6 * time.Hour
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

// get applies placeNameEmptyCacheTTL to a cached empty result and the
// longer placeNameCacheTTL to a cached name - an empty cell is a real
// answer (see placeNameEmptyCacheTTL's doc comment) but a less durable one.
func (s *placeNameCacheStore) get(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.data[key]
	if !ok {
		return "", false
	}
	ttl := placeNameCacheTTL
	if entry.name == "" {
		ttl = placeNameEmptyCacheTTL
	}
	if time.Since(entry.cachedAt) >= ttl {
		return "", false
	}
	return entry.name, true
}

// put caches a genuinely completed resolution, named or empty - see
// placeNameEmptyCacheTTL's doc comment for why an empty result is cached at
// all (it's a real "no named feature here" answer, not a masking
// fallback). A provider hiccup must still never pin a blank name: callers
// only reach put after resolvePlaceName returns with no error at all; a
// failure never calls put and retries (subject to backoff) on the next
// tick instead.
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

// placeNameBackoffInitial/placeNameBackoffMax bound the backoff ladder a
// resolution failure engages: 1 minute, doubling each further failure, up
// to 15 minutes. Offshore, this is the difference between a dead
// place-names provider being retried every 5s poll tick forever and being
// retried on a schedule that actually gives the link a chance to recover.
const (
	placeNameBackoffInitial = 1 * time.Minute
	placeNameBackoffMax     = 15 * time.Minute
)

// placeNameBackoffState tracks the place-names provider failure backoff
// window. active gates resolveAndCachePlaceName from even attempting a live
// call while backed off, so at most one real attempt (and therefore at most
// one recordFailure/recordSuccess call) happens per window - which is what
// keeps the "log once per state change" requirement true without any
// separate rate-limiting on the logging itself.
type placeNameBackoffState struct {
	mu        sync.Mutex
	until     time.Time
	nextDelay time.Duration
}

var placeNameBackoff = &placeNameBackoffState{}

func (b *placeNameBackoffState) active() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return time.Now().Before(b.until)
}

// recordFailure opens (or extends) the backoff window: the first failure
// after a clear window starts the ladder at placeNameBackoffInitial; a
// failure while already backed off doubles the delay, capped at
// placeNameBackoffMax.
func (b *placeNameBackoffState) recordFailure() {
	b.mu.Lock()
	if b.nextDelay <= 0 {
		b.nextDelay = placeNameBackoffInitial
	} else {
		b.nextDelay *= 2
		if b.nextDelay > placeNameBackoffMax {
			b.nextDelay = placeNameBackoffMax
		}
	}
	delay := b.nextDelay
	b.until = time.Now().Add(delay)
	b.mu.Unlock()
	log.Printf("place name: lookup failed, backing off for %s", delay)
}

// recordSuccess clears the backoff window and resets the ladder, logging
// only if backoff was actually engaged - the common case (no prior
// failure) must stay silent.
func (b *placeNameBackoffState) recordSuccess() {
	b.mu.Lock()
	wasEngaged := b.nextDelay > 0
	b.until = time.Time{}
	b.nextDelay = 0
	b.mu.Unlock()
	if wasEngaged {
		log.Printf("place name: lookups recovered, backoff cleared")
	}
}

// resolveAndCachePlaceName is the cache-first entry point: a cache hit
// (named or a still-fresh empty result, see placeNameEmptyCacheTTL) returns
// immediately with no upstream call. A miss resolves live UNLESS a prior
// failure's backoff window is still open, in which case it returns ""
// without touching the provider at all. A resolution failure (provider
// resolution itself, or the provider's own PlaceNameAt) logs explicitly,
// engages/extends the backoff window, and returns "" without caching
// anything; a genuine completion - named or empty - clears backoff and
// caches the result either way.
func resolveAndCachePlaceName(resolve placeNameProviderResolver, lat, lon float64) string {
	key := placeNameCacheKey(lat, lon)
	if name, ok := placeNameCache.get(key); ok {
		return name
	}

	if placeNameBackoff.active() {
		return ""
	}

	name, err := resolvePlaceName(resolve, lat, lon)
	if err != nil {
		log.Printf("place name: resolve failed for %.4f,%.4f: %v", lat, lon, err)
		placeNameBackoff.recordFailure()
		return ""
	}
	placeNameBackoff.recordSuccess()

	placeNameCache.put(key, name)
	return name
}

// ── Provider resolution for the tick/anchor paths ──────────────────────────

// placeNameProviderResolve is the production placeNameProviderResolver:
// resolvePlaceNameProvider against the settings file, read fresh on every
// call so a Settings change takes effect on the very next lookup with no
// restart. A package var (like the old overpassHTTPClient) so tests can
// swap in a fake resolver and restore it afterward.
var placeNameProviderResolve placeNameProviderResolver = func() (placeNameProvider, string, error) {
	return resolvePlaceNameProvider(getEnv("SETTINGS_FILE", "../settings.yaml"))
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
// resolvePlaceName walks up to three rings, so a synchronous resolve could
// hold the whole poller for a while whenever the tight rings come back
// empty (the measured Lindeman case). Resolution therefore runs in the
// background, and this guard stops a 5s tick stacking goroutines against a
// resolution that slow. A plain mutex and flag: there is no
// golang.org/x/sync dependency in go.mod and this does not warrant adding
// one.
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
// failed initial resolution still gets filled in once the provider is
// reachable again. A failure is logged inside resolveAndCachePlaceName;
// the field is simply left empty for the next tick to retry.
func resolveAndPinAnchorWatchPlaceName(original *anchorWatchData, lat, lon float64) string {
	name := resolveAndCachePlaceName(placeNameProviderResolve, lat, lon)
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
// sequential goroutine and the provider's ladder can take a while to
// answer.
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
		if name := resolveAndCachePlaceName(placeNameProviderResolve, lat, lon); name != "" {
			publishPlaceNameForCell(key, name)
		}
	})
	return ""
}

// ── HTTP handler ────────────────────────────────────────────────────────

// placeName is the GET /api/place-name handler. It is a pure cache read:
// no upstream call happens here. Resolution runs on the server's own 5s
// poll tick (tracks.go's sampleTracks -> updateTickPlaceName) so the cache
// is always warm regardless of whether or how often the frontend polls this
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
