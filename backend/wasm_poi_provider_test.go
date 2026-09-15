package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const poiValidFixtureWasm = "testdata/wasm_plugins/poivalid.wasm"

// mustNewWasmPOIProvider constructs a provider against poiValidFixtureWasm
// with its disk-backed cache file redirected into a fresh t.TempDir() (via
// the POI_WASM_CACHE_FILE_<ID> env override newWasmPOIProviderFromBase
// honors) - mirrors mustNewWasmWaveProvider's reasoning.
func mustNewWasmPOIProvider(t *testing.T, path string) *wasmPOIProvider {
	t.Helper()
	cacheFile := filepath.Join(t.TempDir(), "poi-wasm-cache.json")
	t.Setenv("POI_WASM_CACHE_FILE_POI_VALID_FIXTURE", cacheFile)

	p, err := newWasmPOIProvider(path)
	if err != nil {
		t.Fatalf("newWasmPOIProvider(%q) failed: %v", path, err)
	}
	return p
}

func TestWasmPOIProvider_FetchPOI_MapsFixtureFieldsCorrectly(t *testing.T) {
	provider := mustNewWasmPOIProvider(t, poiValidFixtureWasm)

	result, err := provider.FetchPOI(-20.4467, 149.0353, 9260, poiCategoryIDs, 50)
	if err != nil {
		t.Fatalf("FetchPOI returned error: %v", err)
	}

	if len(result.Features) != 3 {
		t.Fatalf("expected 3 features, got %d: %+v", len(result.Features), result.Features)
	}

	first := result.Features[0]
	if first.ID != "node/1" || first.Category != "anchorage" || first.Name != "Sample Anchorage" {
		t.Errorf("unexpected first feature: %+v", first)
	}
	if first.Lat != -20.4467 || first.Lon != 149.0353 {
		t.Errorf("expected fixture position to pass through unchanged, got lat=%v lon=%v", first.Lat, first.Lon)
	}

	enriched := result.Features[1]
	if enriched.Detail == "" || enriched.SourceURL == "" {
		t.Errorf("expected the second fixture feature to carry detail/source_url, got %+v", enriched)
	}

	if len(result.Truncated) != 1 || result.Truncated[0] != "mooring" {
		t.Fatalf("expected truncated=[mooring], got %v", result.Truncated)
	}
	if len(result.Unsupported) != 1 || result.Unsupported[0] != "dive" {
		t.Fatalf("expected unsupported=[dive], got %v", result.Unsupported)
	}

	if result.Cached {
		t.Errorf("expected a fresh (non-cached) fetch on first call")
	}
	if result.CachedAt.IsZero() {
		t.Errorf("expected CachedAt to be set")
	}
}

func TestWasmPOIProvider_FetchPOI_CachesWithinTTL(t *testing.T) {
	provider := mustNewWasmPOIProvider(t, poiValidFixtureWasm)

	first, err := provider.FetchPOI(10.0, 20.0, 9260, []string{"anchorage"}, 50)
	if err != nil {
		t.Fatalf("first FetchPOI returned error: %v", err)
	}
	if first.Cached {
		t.Fatalf("expected the first fetch to be a live (non-cached) fetch")
	}

	second, err := provider.FetchPOI(10.0, 20.0, 9260, []string{"anchorage"}, 50)
	if err != nil {
		t.Fatalf("second FetchPOI returned error: %v", err)
	}
	if !second.Cached {
		t.Fatalf("expected the second fetch within TTL to be served from cache")
	}
	if len(second.Features) != len(first.Features) {
		t.Fatalf("expected the cached result's data to match the original fetch")
	}
}

// Limit is part of the provider contract input, so requests that differ only
// by limit must not share one cache entry.
func TestWasmPOIProvider_FetchPOI_DifferentLimitDoesNotHitSameCacheEntry(t *testing.T) {
	provider := mustNewWasmPOIProvider(t, poiValidFixtureWasm)

	first, err := provider.FetchPOI(10.0, 20.0, 9260, []string{"anchorage"}, 10)
	if err != nil {
		t.Fatalf("first FetchPOI returned error: %v", err)
	}
	if first.Cached {
		t.Fatalf("expected first fetch to be live")
	}

	second, err := provider.FetchPOI(10.0, 20.0, 9260, []string{"anchorage"}, 20)
	if err != nil {
		t.Fatalf("second FetchPOI returned error: %v", err)
	}
	if second.Cached {
		t.Fatalf("expected different limit values to use different cache entries")
	}
}

// A position 0.01 degrees away (well under the 0.02 degree cache cell) must
// still hit the same cache entry - the whole point of the cell is that small
// vessel movement doesn't re-trigger a live Overpass/Places call.
func TestWasmPOIProvider_FetchPOI_CacheCellCollapsesNearbyPositions(t *testing.T) {
	provider := mustNewWasmPOIProvider(t, poiValidFixtureWasm)

	if _, err := provider.FetchPOI(10.000, 20.000, 9260, []string{"anchorage"}, 50); err != nil {
		t.Fatalf("first FetchPOI returned error: %v", err)
	}

	second, err := provider.FetchPOI(10.005, 20.005, 9260, []string{"anchorage"}, 50)
	if err != nil {
		t.Fatalf("second FetchPOI returned error: %v", err)
	}
	if !second.Cached {
		t.Fatalf("expected a position 0.005 degrees away (inside the 0.02 degree cell) to hit the same cache entry")
	}
}

// Categories in a different order must hit the same cache entry - the cache
// key sorts categories before folding them in.
func TestPOIWasmCacheKey_SortsCategoriesBeforeFolding(t *testing.T) {
	a := poiWasmCacheKey(10.0, 20.0, 9260, []string{"bay", "anchorage"}, 50)
	b := poiWasmCacheKey(10.0, 20.0, 9260, []string{"anchorage", "bay"}, 50)
	if a != b {
		t.Fatalf("expected category order to not affect the cache key, got %q vs %q", a, b)
	}
}

func TestPOIWasmCacheKey_RoundsToTheDocumentedCell(t *testing.T) {
	a := poiWasmCacheKey(10.001, 20.001, 9260, []string{"anchorage"}, 50)
	b := poiWasmCacheKey(10.0, 20.0, 9260, []string{"anchorage"}, 50)
	if a != b {
		t.Fatalf("expected positions within one 0.02 degree cell to share a cache key, got %q vs %q", a, b)
	}

	c := poiWasmCacheKey(10.05, 20.0, 9260, []string{"anchorage"}, 50)
	if a == c {
		t.Fatalf("expected a position a full cell away to get a distinct cache key")
	}
}

func TestPOIWasmCacheKey_DiffersForDifferentLimits(t *testing.T) {
	a := poiWasmCacheKey(10.0, 20.0, 9260, []string{"anchorage"}, 10)
	b := poiWasmCacheKey(10.0, 20.0, 9260, []string{"anchorage"}, 20)
	if a == b {
		t.Fatalf("expected different limits to produce different cache keys")
	}
}

func TestWasmPOIProvider_FetchPOI_StaleOnErrorFallback(t *testing.T) {
	provider := mustNewWasmPOIProvider(t, poiValidFixtureWasm)

	fresh, err := provider.FetchPOI(30.0, 40.0, 9260, []string{"anchorage"}, 50)
	if err != nil {
		t.Fatalf("initial FetchPOI returned error: %v", err)
	}

	cacheKey := poiWasmCacheKey(30.0, 40.0, 9260, []string{"anchorage"}, 50)
	provider.cache.mu.Lock()
	entry, ok := provider.cache.data[cacheKey]
	if !ok {
		provider.cache.mu.Unlock()
		t.Fatalf("expected a cache entry for key %q after the initial fetch", cacheKey)
	}
	entry.CachedAt = time.Now().Add(-2 * provider.ttlDuration())
	provider.cache.data[cacheKey] = entry
	provider.cache.mu.Unlock()

	// Close the underlying compiled plugin so the next live call fails
	// deterministically, simulating an upstream outage without needing a
	// second, failure-simulating fixture.
	provider.compiled.Close(context.Background())

	stale, err := provider.FetchPOI(30.0, 40.0, 9260, []string{"anchorage"}, 50)
	if err != nil {
		t.Fatalf("expected a stale-cache fallback instead of an error, got: %v", err)
	}
	if !stale.Cached {
		t.Fatalf("expected the stale fallback result to be marked Cached=true")
	}
	if len(stale.Features) != len(fresh.Features) {
		t.Fatalf("expected the stale fallback to carry the original fetched data")
	}
}

// ── PlaceNameAt / SearchPlaces: the only network-free assertion possible
// without the real osm-overpass build is "a plugin that doesn't export
// either one errors clearly" - live JSON-mapping checks against the real
// plugin (network required) live in wasm_poi_provider_live_test.go instead,
// gated by testing.Short() like every other live-network test in this
// package (assistant_tools_live_test.go, wasm_ftp_fetch_test.go).

func TestWasmPOIProvider_PlaceNameAtAndSearchPlaces_UnsupportedPluginErrors(t *testing.T) {
	provider := mustNewWasmPOIProvider(t, poiValidFixtureWasm)

	if _, err := provider.PlaceNameAt(-20.4467, 149.0353, 400); err == nil {
		t.Fatalf("expected PlaceNameAt to error on a plugin that does not export place_name_at/search_places")
	}
	if _, err := provider.SearchPlaces(placeSearchInput{Query: "Hill Inlet", Lat: -20.4467, Lon: 149.0353, MaxResults: 5}); err == nil {
		t.Fatalf("expected SearchPlaces to error on a plugin that does not export place_name_at/search_places")
	}
}

func TestLoadWasmPOIProviders_RegistersValidPlugin(t *testing.T) {
	withCleanPOIProviderRegistry(t)

	dir := t.TempDir()
	goodBytes, err := os.ReadFile(poiValidFixtureWasm)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "poivalid.wasm"), goodBytes, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.wasm"), []byte("not a real wasm module"), 0o644); err != nil {
		t.Fatalf("write corrupt fixture: %v", err)
	}

	loadWasmPOIProviders(dir)

	provider, ok := getPOIProvider("poi-valid-fixture")
	if !ok {
		t.Fatalf("expected the poivalid plugin to be registered")
	}
	if provider.Name() != "POI Valid Fixture Provider" {
		t.Errorf("unexpected provider name: %q", provider.Name())
	}
	if len(poiProviderOrder) != 1 {
		t.Errorf("expected exactly 1 registered provider, got %d: %v", len(poiProviderOrder), poiProviderOrder)
	}
}

func TestPluginsPOIDir_DefaultsAndEnvOverride(t *testing.T) {
	if got := pluginsPOIDir(); got != "plugins/poi" {
		t.Fatalf("expected default 'plugins/poi', got %q", got)
	}

	t.Setenv("PLUGINS_POI_DIR", "/tmp/custom-poi-plugins")
	if got := pluginsPOIDir(); got != "/tmp/custom-poi-plugins" {
		t.Fatalf("expected env override to take effect, got %q", got)
	}
}
