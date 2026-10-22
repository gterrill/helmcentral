package main

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// ── shoreline cache (ADR 0201) ───────────────────────────────────────────
//
// Charted shoreline barely changes, and fetching it is a single slow
// Overpass round trip on the boat's link, so the raw lines are kept in
// memory by rounded position. The shelter is always computed from the true
// anchor position over the cached lines, so rounding costs no accuracy as
// long as the fetch reaches every ray from anywhere in the cell
// (shorelineFetchRadiusM). An error is never cached.

const (
	shorelineCacheTTL         = 7 * 24 * time.Hour
	shorelineCacheMaxEntries  = 16
	shorelineCacheCellDegrees = 0.02

	// shorelineCellMarginNm covers the distance from a cell's centre to its
	// furthest corner (about 0.9 nm at the equator) with room to spare.
	shorelineCellMarginNm = 1.5
)

// shorelineFetchRadiusM is the radius asked of the provider: the full ray
// length plus the cache cell margin.
var shorelineFetchRadiusM = int(math.Ceil((shelterMaxFetchNm + shorelineCellMarginNm) * metersPerNauticalMile))

type shorelineCacheEntry struct {
	lines    []shorelineLine
	cachedAt time.Time
}

type shorelineCacheStore struct {
	mu    sync.Mutex
	data  map[string]shorelineCacheEntry
	order []string
	now   func() time.Time
}

func newShorelineCacheStore(now func() time.Time) *shorelineCacheStore {
	return &shorelineCacheStore{data: make(map[string]shorelineCacheEntry), now: now}
}

var shorelineCache = newShorelineCacheStore(time.Now)

// reset empties the store and restores the real clock; tests call it so one
// test's answer cannot satisfy the next.
func (s *shorelineCacheStore) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = make(map[string]shorelineCacheEntry)
	s.order = nil
	s.now = time.Now
}

func (s *shorelineCacheStore) get(key string) ([]shorelineLine, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.data[key]
	if !ok || s.now().Sub(e.cachedAt) >= shorelineCacheTTL {
		return nil, false
	}
	return e.lines, true
}

func (s *shorelineCacheStore) put(key string, lines []shorelineLine) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.data[key]; !exists {
		s.order = append(s.order, key)
		if len(s.order) > shorelineCacheMaxEntries {
			oldest := s.order[0]
			s.order = s.order[1:]
			delete(s.data, oldest)
		}
	}
	s.data[key] = shorelineCacheEntry{lines: lines, cachedAt: s.now()}
}

// shorelineProvider is a place-names provider whose plugin also exports
// shoreline_near. Like placeNameCapable it is a two-step check: the Go
// method always exists on a WASM provider, so SupportsShoreline says whether
// the plugin behind it does.
type shorelineProvider interface {
	SupportsShoreline() bool
	ShorelineNear(lat, lon float64, radiusM int) ([]shorelineLine, error)
}

func shorelineCellCentre(lat, lon float64) (float64, float64) {
	return math.Round(lat/shorelineCacheCellDegrees) * shorelineCacheCellDegrees,
		math.Round(lon/shorelineCacheCellDegrees) * shorelineCacheCellDegrees
}

// shorelineNearCached returns the shoreline for the cache cell containing
// (lat, lon), fetching it from the provider on a miss.
func shorelineNearCached(provider placeNameProvider, providerID string, lat, lon float64) ([]shorelineLine, error) {
	sp, ok := provider.(shorelineProvider)
	if !ok || !sp.SupportsShoreline() {
		return nil, fmt.Errorf("place-names plugin %q has no shoreline_near export; install the updated osm-overpass plugin", providerID)
	}
	cLat, cLon := shorelineCellCentre(lat, lon)
	key := fmt.Sprintf("%s|%.2f,%.2f", providerID, cLat, cLon)
	if lines, ok := shorelineCache.get(key); ok {
		return lines, nil
	}
	lines, err := sp.ShorelineNear(cLat, cLon, shorelineFetchRadiusM)
	if err != nil {
		return nil, err
	}
	shorelineCache.put(key, lines)
	return lines, nil
}
