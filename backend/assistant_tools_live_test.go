package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestFindPlacesLive_BonaBay runs find_places against a real Overpass server.
// It is skipped under -short (like the live BOM FTP tests) and unless
// OVERPASS_LIVE_TEST_MIRROR names the mirror to use, so CI never depends on
// a third party. This is a TEST-ONLY knob: it exists purely to point this
// one test at a mirror without touching the operator's real osm-overpass
// plugin config, and it does so by registering a throwaway "osm-overpass"
// POI provider and a temp plugin overrides store, feeding the mirror
// through the same stored config value production code reads
// (currentOverpassAPIURL, place_name.go) - never by poking a package var
// directly. Run it by hand after changing the query shape:
//
//	OVERPASS_LIVE_TEST_MIRROR=https://overpass.openstreetmap.fr/api/interpreter go test -run TestFindPlacesLive -v .
//
// The two-rung ladder in executeFindPlaces exists because of what this
// query costs on a public mirror (ADR 0093 §8); this test is the check that
// the exact-name rung still answers within the client timeout.
func TestFindPlacesLive_BonaBay(t *testing.T) {
	if testing.Short() {
		t.Skip("live Overpass query; skipped under -short")
	}
	mirror := os.Getenv("OVERPASS_LIVE_TEST_MIRROR")
	if mirror == "" {
		t.Skip("set OVERPASS_LIVE_TEST_MIRROR to run the live Overpass query")
	}
	if _, err := resolveOverpassPluginURL(mirror); err != nil {
		t.Fatalf("OVERPASS_LIVE_TEST_MIRROR: %v", err)
	}

	wasmPath, store := withOsmOverpassPOIProvider(t)
	if err := store.SetConfigValues(wasmPath, map[string]string{"overpass_url": mirror}); err != nil {
		t.Fatalf("SetConfigValues: %v", err)
	}

	deps := assistantToolDeps{
		vesselState: func() (vesselStateData, error) { return vesselStateData{Latitude: -1, Longitude: -1}, nil },
		overpass:    overpassHTTPClient,
		routes:      func() []routeData { return nil },
	}
	// Gloucester Island; Bona Bay is on its western side.
	raw, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Bona Bay","near_lat":-20.03,"near_lon":148.45}`))
	if err != nil {
		t.Fatalf("find_places: %v", err)
	}
	var result struct {
		Search  string `json:"search"`
		Results []struct {
			Name string  `json:"name"`
			Kind string  `json:"kind"`
			Lat  float64 `json:"lat"`
			Lon  float64 `json:"lon"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("parse result: %v\n%s", err, raw)
	}
	if result.Search != "exact" {
		t.Errorf("search = %q, want exact\n%s", result.Search, raw)
	}
	found := false
	for _, r := range result.Results {
		if strings.EqualFold(r.Name, "Bona Bay") && r.Kind == "bay" && r.Lat < -19.9 && r.Lat > -20.2 {
			found = true
		}
	}
	if !found {
		t.Errorf("Bona Bay (bay) not in results:\n%s", raw)
	}
	t.Logf("find_places: %s", raw)
}
