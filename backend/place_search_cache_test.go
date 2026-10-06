package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func findPlacesRaw(t *testing.T, deps assistantToolDeps, args string) (string, error) {
	t.Helper()
	return deps.execute(context.Background(), "find_places", json.RawMessage(args))
}

func cacheTestResult() placeSearchResult {
	return placeSearchResult{Search: "exact", RadiusNm: 50, Results: []placeSearchMatch{
		{Name: "Breakwater Marina", Kind: "marina", Lat: -19.25, Lon: 146.83},
	}}
}

func noRoutes() []routeData { return nil }

func TestFindPlacesCache_IdenticalCallsHitProviderOnce(t *testing.T) {
	provider := &fakeSearchPlacesProvider{id: "p", result: cacheTestResult()}
	deps := findPlacesDeps(-19.2, 146.8, provider, noRoutes)
	a, err := findPlacesRaw(t, deps, `{"query":"Breakwater Marina"}`)
	if err != nil {
		t.Fatal(err)
	}
	b, err := findPlacesRaw(t, deps, `{"query":"Breakwater Marina"}`)
	if err != nil {
		t.Fatal(err)
	}
	if provider.callCount() != 1 {
		t.Fatalf("provider called %d times, want 1", provider.callCount())
	}
	if a != b {
		t.Errorf("results differ:\n%s\n%s", a, b)
	}
}

func TestFindPlacesCache_QueryWhitespaceSharesEntryCaseDoesNot(t *testing.T) {
	provider := &fakeSearchPlacesProvider{id: "p", result: cacheTestResult()}
	deps := findPlacesDeps(-19.2, 146.8, provider, noRoutes)
	for _, q := range []string{"Breakwater Marina", "  Breakwater   Marina "} {
		if _, err := findPlacesRaw(t, deps, fmt.Sprintf(`{"query":%q}`, q)); err != nil {
			t.Fatal(err)
		}
	}
	if provider.callCount() != 1 {
		t.Fatalf("whitespace variants: provider called %d times, want 1", provider.callCount())
	}
	// The plugin's exact search is case-sensitive, so "mckenzie bay" must
	// not answer for "McKenzie Bay".
	for _, q := range []string{"mckenzie bay", "McKenzie Bay"} {
		if _, err := findPlacesRaw(t, deps, fmt.Sprintf(`{"query":%q}`, q)); err != nil {
			t.Fatal(err)
		}
	}
	if provider.callCount() != 3 {
		t.Fatalf("case variants: provider called %d times, want 3", provider.callCount())
	}
}

func TestPlaceSearchCache_ClearKeepsClockResetRestoresIt(t *testing.T) {
	fixed := time.Unix(1000, 0)
	s := newPlaceSearchCacheStore(func() time.Time { return fixed })
	s.put("k", cacheTestResult())
	s.clear()
	if _, ok := s.get("k"); ok || len(s.order) != 0 {
		t.Fatal("clear should empty the store")
	}
	if !s.now().Equal(fixed) {
		t.Error("clear must not touch the clock")
	}
	s.reset()
	if s.now().Equal(fixed) {
		t.Error("reset should restore the real clock")
	}
}

func TestFindPlacesCache_DistinctInputsAreSeparateEntries(t *testing.T) {
	p1 := &fakeSearchPlacesProvider{id: "p1", result: cacheTestResult()}
	p2 := &fakeSearchPlacesProvider{id: "p2", result: cacheTestResult()}
	d1 := findPlacesDeps(-19.2, 146.8, p1, noRoutes)
	// findPlacesDeps resets the cache, so build every deps before any call.
	d2 := findPlacesDeps(-19.2, 146.8, p2, noRoutes)
	dFar := findPlacesDeps(-19.5, 146.8, p1, noRoutes)
	withWaypoint := func() []routeData {
		return []routeData{{Name: "R", Waypoints: []routeWaypoint{{Name: "Breakwater Marina", Lat: -19.2, Lon: 146.8}}}}
	}
	dBroad := findPlacesDeps(-19.2, 146.8, p1, withWaypoint)

	calls := []struct {
		deps assistantToolDeps
		args string
	}{
		{d1, `{"query":"Breakwater Marina"}`},
		{d1, `{"query":"Other Marina"}`},
		{d1, `{"query":"Breakwater Marina","max_results":3}`},
		{dFar, `{"query":"Breakwater Marina"}`},
		{dBroad, `{"query":"Breakwater Marina"}`},
		{d2, `{"query":"Breakwater Marina"}`},
	}
	for _, c := range calls {
		if _, err := findPlacesRaw(t, c.deps, c.args); err != nil {
			t.Fatal(err)
		}
	}
	if got := p1.callCount(); got != 5 {
		t.Errorf("p1 calls = %d, want 5", got)
	}
	if got := p2.callCount(); got != 1 {
		t.Errorf("p2 calls = %d, want 1", got)
	}
}

func TestFindPlacesCache_SameCellDifferentCentreStillCachedButDistanceFresh(t *testing.T) {
	provider := &fakeSearchPlacesProvider{id: "p", result: cacheTestResult()}
	dNear := findPlacesDeps(-19.200, 146.800, provider, noRoutes)
	dNudge := findPlacesDeps(-19.204, 146.800, provider, noRoutes)
	a, _ := findPlacesRaw(t, dNear, `{"query":"Breakwater Marina"}`)
	b, _ := findPlacesRaw(t, dNudge, `{"query":"Breakwater Marina"}`)
	if provider.callCount() != 1 {
		t.Fatalf("provider called %d times, want 1", provider.callCount())
	}
	if a == "" || b == "" {
		t.Fatal("empty result")
	}
}

func TestFindPlacesCache_ErrorNotCachedAndReturned(t *testing.T) {
	provider := &fakeSearchPlacesProvider{id: "p", err: fmt.Errorf("network unreachable")}
	deps := findPlacesDeps(-19.2, 146.8, provider, noRoutes)
	for i := 0; i < 2; i++ {
		if _, err := findPlacesRaw(t, deps, `{"query":"Breakwater Marina"}`); err == nil {
			t.Fatal("expected error")
		}
	}
	if provider.callCount() != 2 {
		t.Fatalf("provider called %d times, want 2", provider.callCount())
	}
}

func TestFindPlacesCache_ExpiredEntryNotServedOnProviderError(t *testing.T) {
	provider := &fakeSearchPlacesProvider{id: "p", result: cacheTestResult()}
	deps := findPlacesDeps(-19.2, 146.8, provider, noRoutes)
	clock := time.Now()
	placeSearchCache.now = func() time.Time { return clock }
	if _, err := findPlacesRaw(t, deps, `{"query":"Breakwater Marina"}`); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(placeSearchCacheTTL + time.Minute)
	provider.err = fmt.Errorf("down")
	if _, err := findPlacesRaw(t, deps, `{"query":"Breakwater Marina"}`); err == nil {
		t.Fatal("expired entry must not mask a provider error")
	}
}

func TestPlaceSearchCache_Expiry(t *testing.T) {
	clock := time.Now()
	s := newPlaceSearchCacheStore(func() time.Time { return clock })
	s.put("full", cacheTestResult())
	s.put("empty", placeSearchResult{Search: "none"})

	clock = clock.Add(placeSearchEmptyCacheTTL - time.Second)
	if _, ok := s.get("empty"); !ok {
		t.Error("empty entry expired early")
	}
	clock = clock.Add(2 * time.Second)
	if _, ok := s.get("empty"); ok {
		t.Error("empty entry should expire after 6h")
	}
	if _, ok := s.get("full"); !ok {
		t.Error("non-empty entry should outlive 6h")
	}
	clock = clock.Add(placeSearchCacheTTL)
	if _, ok := s.get("full"); ok {
		t.Error("non-empty entry should expire after 24h")
	}
}

func TestPlaceSearchCache_EvictsOldestAtBound(t *testing.T) {
	s := newPlaceSearchCacheStore(time.Now)
	for i := 0; i <= placeSearchCacheMaxEntries; i++ {
		s.put(fmt.Sprintf("k%d", i), cacheTestResult())
	}
	if _, ok := s.get("k0"); ok {
		t.Error("oldest entry should have been evicted")
	}
	if _, ok := s.get("k1"); !ok {
		t.Error("second-oldest entry should remain")
	}
	if len(s.data) != placeSearchCacheMaxEntries {
		t.Errorf("size = %d, want %d", len(s.data), placeSearchCacheMaxEntries)
	}
}

// A provider reports a degraded search (mirror trouble, partial coverage) as
// an empty result with a Note; remembering it would repeat the failure for
// six hours.
func TestFindPlacesCache_EmptyResultWithNoteIsNotCached(t *testing.T) {
	provider := &fakeSearchPlacesProvider{id: "p", result: placeSearchResult{Search: "none", RadiusNm: 50, Note: "the place-name service did not answer"}}
	deps := findPlacesDeps(-19.2, 146.8, provider, noRoutes)
	for i := 0; i < 2; i++ {
		if _, err := findPlacesRaw(t, deps, `{"query":"Nowhere Cove"}`); err != nil {
			t.Fatal(err)
		}
	}
	if provider.callCount() != 2 {
		t.Fatalf("provider called %d times, want 2: a noted empty answer must not be cached", provider.callCount())
	}
}

func TestFindPlacesCache_GenuinelyEmptyResultIsCached(t *testing.T) {
	provider := &fakeSearchPlacesProvider{id: "p", result: placeSearchResult{Search: "none", RadiusNm: 50}}
	deps := findPlacesDeps(-19.2, 146.8, provider, noRoutes)
	for i := 0; i < 2; i++ {
		if _, err := findPlacesRaw(t, deps, `{"query":"Nowhere Cove"}`); err != nil {
			t.Fatal(err)
		}
	}
	if provider.callCount() != 1 {
		t.Fatalf("provider called %d times, want 1", provider.callCount())
	}
}
