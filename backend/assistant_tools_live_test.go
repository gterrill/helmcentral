package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestFindPlacesLive_* run find_places (assistant_tools.go's
// executeFindPlaces) end to end against the real, built osm-overpass plugin
// and a live Overpass mirror - ADR 0101 deleted the backend's own Overpass
// client, so find_places now only ever calls whatever place-names provider
// is configured (d.placeNames), exactly like production.
//
// Both tests reuse mustNewOsmOverpassPOIProviderLive
// (wasm_poi_provider_live_test.go) rather than standing up a second copy of
// the same plugin/mirror/cache-file wiring, so they inherit its skip rules
// too: skipped under -short, and skipped outright when
// plugins/poi/osm-overpass.wasm hasn't been built (plugins/* is
// gitignored). There is deliberately no env-var mirror override here (the
// old OVERPASS_LIVE_TEST_MIRROR knob) - liveOverpassMirror is the one mirror
// this project's boat network actually reaches, and every other live test
// in this package already hardcodes it the same way.
func liveFindPlacesDeps(t *testing.T) assistantToolDeps {
	t.Helper()
	provider := mustNewOsmOverpassPOIProviderLive(t)
	return assistantToolDeps{
		vesselState: func() (vesselStateData, error) { return vesselStateData{Latitude: -1, Longitude: -1}, nil },
		placeNames:  func() (placeNameProvider, string, error) { return provider, provider.ID(), nil },
		routes:      func() []routeData { return nil },
	}
}

type liveFindPlacesResult struct {
	Search  string `json:"search"`
	Results []struct {
		Name string  `json:"name"`
		Kind string  `json:"kind"`
		Lat  float64 `json:"lat"`
		Lon  float64 `json:"lon"`
	} `json:"results"`
}

// TestFindPlacesLive_HillInlet: an exact name match near Whitehaven Beach,
// Whitsunday Island.
func TestFindPlacesLive_HillInlet(t *testing.T) {
	deps := liveFindPlacesDeps(t)

	raw, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Hill Inlet","near_lat":-20.2833,"near_lon":148.9667}`))
	if err != nil {
		t.Fatalf("find_places: %v", err)
	}
	var result liveFindPlacesResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("parse result: %v\n%s", err, raw)
	}
	found := false
	for _, r := range result.Results {
		if strings.EqualFold(r.Name, "Hill Inlet") {
			found = true
		}
	}
	if !found {
		t.Errorf("Hill Inlet not in results:\n%s", raw)
	}
	t.Logf("find_places(Hill Inlet): %s", raw)
}

// TestFindPlacesLive_WhitehavenPartialMatch: a lower-case, partial query
// that only the broader (non-exact) search rung can answer.
func TestFindPlacesLive_WhitehavenPartialMatch(t *testing.T) {
	deps := liveFindPlacesDeps(t)

	raw, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"whitehaven","near_lat":-20.2833,"near_lon":148.9667}`))
	if err != nil {
		t.Fatalf("find_places: %v", err)
	}
	var result liveFindPlacesResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("parse result: %v\n%s", err, raw)
	}
	found := false
	for _, r := range result.Results {
		if strings.Contains(strings.ToLower(r.Name), "whitehaven") {
			found = true
		}
	}
	if !found {
		t.Errorf("no whitehaven-named result found:\n%s", raw)
	}
	t.Logf("find_places(whitehaven): search=%q %s", result.Search, raw)
}
