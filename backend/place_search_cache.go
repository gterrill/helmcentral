package main

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

// ── find_places result cache ─────────────────────────────────────────────
//
// find_places hands the configured place-names provider's SearchPlaces a
// query that, on the boat, runs Overpass over a slow link. The operator
// asks about the same marina or anchorage across several chats in a
// morning, and each ask paid the full round trip again. This caches the
// provider's whole answer in memory, modelled on placeNameCacheStore.

const (
	placeSearchCacheTTL         = 24 * time.Hour
	placeSearchCacheMaxEntries  = 256
	placeSearchCacheCellDegrees = 0.02 // same cell as poiWasmCacheCellDegrees

	// placeSearchEmptyCacheTTL is shorter for the same reason as
	// placeNameEmptyCacheTTL: "nothing found" is a real answer, but a
	// less durable one than a hit (a feature may be tagged, or the
	// provider's mirror may have been incomplete), so it is retried within
	// the cruising day.
	placeSearchEmptyCacheTTL = 6 * time.Hour
)

type placeSearchCacheEntry struct {
	result   placeSearchResult
	cachedAt time.Time
}

// placeSearchCacheStore caches only successful provider answers. An error
// is never stored, and an expired entry is never served when the provider
// then fails: the error is returned as-is (AGENTS.md fail-fast policy).
// now is injectable so TTL expiry is testable without sleeping.
type placeSearchCacheStore struct {
	mu    sync.Mutex
	data  map[string]placeSearchCacheEntry
	order []string // insertion order, oldest first
	now   func() time.Time
}

func newPlaceSearchCacheStore(now func() time.Time) *placeSearchCacheStore {
	return &placeSearchCacheStore{data: make(map[string]placeSearchCacheEntry), now: now}
}

var placeSearchCache = newPlaceSearchCacheStore(time.Now)

// clear drops every cached answer. Saving a plugin's settings calls it: an
// answer fetched under the old config (a wrong mirror URL returning
// "nothing found", say) must not outlive the fix.
func (s *placeSearchCacheStore) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = make(map[string]placeSearchCacheEntry)
	s.order = nil
}

// reset clears the store and restores the real clock. Tests call it so a
// cached answer from one test cannot satisfy the next.
func (s *placeSearchCacheStore) reset() {
	s.clear()
	s.mu.Lock()
	s.now = time.Now
	s.mu.Unlock()
}

// placeSearchCacheKey folds in everything that changes the provider's
// answer: which provider, the normalised query, the centre rounded to a
// 0.02 degree cell, the result cap and the Broad flag. The query is trimmed
// and its internal whitespace collapsed, but its case is kept: the
// osm-overpass plugin's exact search is case-sensitive and derives its
// spellings from the query as typed, so "mckenzie bay" cannot find
// "McKenzie Bay" and its empty answer must not be served to that query.
// Distance and bearing
// are computed from the true centre by the caller after the lookup, so
// rounding here costs no accuracy.
func placeSearchCacheKey(providerID string, in placeSearchInput) string {
	lat := math.Round(in.Lat/placeSearchCacheCellDegrees) * placeSearchCacheCellDegrees
	lon := math.Round(in.Lon/placeSearchCacheCellDegrees) * placeSearchCacheCellDegrees
	query := strings.Join(strings.Fields(in.Query), " ")
	return fmt.Sprintf("%s|%q|%.2f,%.2f|%d|%t", providerID, query, lat, lon, in.MaxResults, in.Broad)
}

// get returns a fresh entry, applying the shorter TTL to an empty result.
func (s *placeSearchCacheStore) get(key string) (placeSearchResult, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.data[key]
	if !ok {
		return placeSearchResult{}, false
	}
	ttl := placeSearchCacheTTL
	if len(entry.result.Results) == 0 {
		ttl = placeSearchEmptyCacheTTL
	}
	if s.now().Sub(entry.cachedAt) >= ttl {
		return placeSearchResult{}, false
	}
	return entry.result, true
}

// put stores a completed provider answer, evicting the oldest entry once
// the store holds more than placeSearchCacheMaxEntries.
func (s *placeSearchCacheStore) put(key string, result placeSearchResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.data[key]; !exists {
		s.order = append(s.order, key)
		if len(s.order) > placeSearchCacheMaxEntries {
			oldest := s.order[0]
			s.order = s.order[1:]
			delete(s.data, oldest)
		}
	}
	s.data[key] = placeSearchCacheEntry{result: result, cachedAt: s.now()}
}

// searchPlacesCached answers from the cache when it can and otherwise
// calls the provider, caching only a successful, non-degraded answer.
func searchPlacesCached(provider placeNameProvider, providerID string, in placeSearchInput) (placeSearchResult, error) {
	key := placeSearchCacheKey(providerID, in)
	if cached, ok := placeSearchCache.get(key); ok {
		return cached, nil
	}
	result, err := provider.SearchPlaces(in)
	if err != nil {
		return placeSearchResult{}, err
	}
	// An empty result carrying a Note is how a provider reports a degraded
	// search; remembering it would repeat the failure for hours. A
	// genuinely empty result has no Note and is cached.
	//
	// This is an inference, not a contract: the search_places output the
	// plugins return (wasmSearchPlacesOutput) has no explicit "degraded"
	// field, and the plugins live outside this repository, so the host cannot
	// add one that they set. A provider that attaches a Note to a genuinely
	// empty answer is judged by this rule alone. Replace it with an explicit
	// flag if the plugin ABI gains one.
	if len(result.Results) == 0 && result.Note != "" {
		return result, nil
	}
	placeSearchCache.put(key, result)
	return result, nil
}
