package main

import (
	"os"
	"path/filepath"
	"testing"
)

// This file's tests hit a real Overpass mirror through the real, built
// osm-overpass.wasm plugin (docs/examples/poi-plugins/osm-overpass) -
// skipped under -short like every other live-network test in this package
// (assistant_tools_live_test.go, wasm_ftp_fetch_test.go), and also skipped
// outright when the plugin build itself is absent (plugins/* is gitignored;
// see wasm_plugin_test.go's osmOverpassRealPluginWasm doc comment), so a
// fresh checkout or CI never depends on either a third party or a local
// docker build step having already run.

// liveOverpassMirror is the mirror this project's boat network can actually
// reach (project notes: overpass-api.de, the plugin's own default, refuses
// the boat network entirely) - configured the same way an operator would,
// through the plugin's own stored "overpass_url" config value, never a
// package-level var.
const liveOverpassMirror = "https://overpass.openstreetmap.fr/api/interpreter"

func mustNewOsmOverpassPOIProviderLive(t *testing.T) *wasmPOIProvider {
	t.Helper()
	if testing.Short() {
		t.Skip("live Overpass query; skipped under -short")
	}
	if _, err := os.Stat(osmOverpassRealPluginWasm); err != nil {
		t.Skipf("osm-overpass.wasm not built (run the plugins-builder first): %v", err)
	}
	cacheFile := filepath.Join(t.TempDir(), "osm-overpass-poi-cache.json")
	t.Setenv("POI_WASM_CACHE_FILE_OSM_OVERPASS", cacheFile)

	p, err := newWasmPOIProvider(osmOverpassRealPluginWasm)
	if err != nil {
		t.Fatalf("newWasmPOIProvider(%q) failed: %v", osmOverpassRealPluginWasm, err)
	}

	store := withTestPluginOverridesStore(t)
	if err := store.SetConfigValues(p.Path(), map[string]string{"overpass_url": liveOverpassMirror}); err != nil {
		t.Fatalf("SetConfigValues: %v", err)
	}

	return p
}

// TestWasmPOIProviderLive_PlaceNameAt_MapsFieldsCorrectly proves the host's
// JSON decoding of place_name_at against the real plugin, not just a fake -
// Lindeman Island, per place_name.go's own long-standing test coordinates.
func TestWasmPOIProviderLive_PlaceNameAt_MapsFieldsCorrectly(t *testing.T) {
	provider := mustNewOsmOverpassPOIProviderLive(t)

	result, err := provider.PlaceNameAt(lindemanLat, lindemanLon, 5000)
	if err != nil {
		t.Fatalf("PlaceNameAt: %v", err)
	}
	if result.Name == "" {
		t.Fatalf("expected a named feature within 5000m of Lindeman Island, got empty")
	}
	t.Logf("PlaceNameAt(%v, %v, 5000) = %+v", lindemanLat, lindemanLon, result)
}

// TestWasmPOIProviderLive_SearchPlaces_ExactMatchNearWhitehaven proves the
// host's JSON decoding of search_places against the real plugin.
func TestWasmPOIProviderLive_SearchPlaces_ExactMatchNearWhitehaven(t *testing.T) {
	provider := mustNewOsmOverpassPOIProviderLive(t)

	// Whitsundays anchorage, near Whitehaven Beach.
	result, err := provider.SearchPlaces(placeSearchInput{
		Query: "Whitehaven Beach", Lat: -20.2833, Lon: 148.9667, MaxResults: 5, Broad: true,
	})
	if err != nil {
		t.Fatalf("SearchPlaces: %v", err)
	}
	if result.Search != "exact" {
		t.Fatalf("expected search=exact for Whitehaven Beach, got %q (results: %+v)", result.Search, result.Results)
	}
	found := false
	for _, r := range result.Results {
		if r.Name == "Whitehaven Beach" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected Whitehaven Beach among results, got %+v", result.Results)
	}
}
