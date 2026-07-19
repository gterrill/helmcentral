package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const waveValidFixtureWasm = "testdata/wasm_plugins/wavevalid.wasm"

// mustNewWasmWaveProvider constructs a provider against waveValidFixtureWasm
// with its disk-backed cache file redirected into a fresh t.TempDir() (via
// the WAVE_WASM_CACHE_FILE_<ID> env override newWasmWaveProviderFromBase
// honors) - mirrors mustNewWasmWeatherProvider's reasoning: without this,
// every test would share and persist to the repo's real
// cache/wave_wasm_wave-valid-fixture_cache.json.
func mustNewWasmWaveProvider(t *testing.T, path string) *wasmWaveProvider {
	t.Helper()
	cacheFile := filepath.Join(t.TempDir(), "wave-wasm-cache.json")
	t.Setenv("WAVE_WASM_CACHE_FILE_WAVE_VALID_FIXTURE", cacheFile)

	p, err := newWasmWaveProvider(path)
	if err != nil {
		t.Fatalf("newWasmWaveProvider(%q) failed: %v", path, err)
	}
	return p
}

func TestWasmWaveProvider_FetchWaves_MapsFixtureFieldsCorrectly(t *testing.T) {
	provider := mustNewWasmWaveProvider(t, waveValidFixtureWasm)

	bundle, err := provider.FetchWaves(-27.4, 153.0, 2)
	if err != nil {
		t.Fatalf("FetchWaves returned error: %v", err)
	}

	if len(bundle.Hourly) != 4 {
		t.Fatalf("expected 4 hourly entries, got %d", len(bundle.Hourly))
	}
	if bundle.Hourly[0].Time.IsZero() {
		t.Errorf("expected hourly[0].time to be parsed, got zero time")
	}
	if bundle.Hourly[0].WaveHeightM != 1.2 {
		t.Errorf("expected wave_height_m 1.2, got %v", bundle.Hourly[0].WaveHeightM)
	}
	if bundle.Hourly[0].WavePeriodS != 8.5 {
		t.Errorf("expected wave_period_s 8.5, got %v", bundle.Hourly[0].WavePeriodS)
	}

	if bundle.SeaTempC == nil {
		t.Fatalf("expected a non-nil sea_surface_temperature_c from the fixture")
	}
	if *bundle.SeaTempC != 21.5 {
		t.Errorf("expected sea_surface_temperature_c 21.5, got %v", *bundle.SeaTempC)
	}

	if bundle.Cached {
		t.Errorf("expected a fresh (non-cached) fetch on first call")
	}
	if bundle.CachedAt.IsZero() {
		t.Errorf("expected CachedAt to be set")
	}
}

func TestWasmWaveProvider_FetchWaves_CachesWithinTTL(t *testing.T) {
	provider := mustNewWasmWaveProvider(t, waveValidFixtureWasm)

	first, err := provider.FetchWaves(10.0, 20.0, 2)
	if err != nil {
		t.Fatalf("first FetchWaves returned error: %v", err)
	}
	if first.Cached {
		t.Fatalf("expected the first fetch to be a live (non-cached) fetch")
	}

	second, err := provider.FetchWaves(10.0, 20.0, 2)
	if err != nil {
		t.Fatalf("second FetchWaves returned error: %v", err)
	}
	if !second.Cached {
		t.Fatalf("expected the second fetch within TTL to be served from cache")
	}
	if len(second.Hourly) != len(first.Hourly) {
		t.Fatalf("expected the cached bundle's data to match the original fetch")
	}
}

func TestWasmWaveProvider_FetchWaves_StaleOnErrorFallback(t *testing.T) {
	provider := mustNewWasmWaveProvider(t, waveValidFixtureWasm)

	fresh, err := provider.FetchWaves(30.0, 40.0, 2)
	if err != nil {
		t.Fatalf("initial FetchWaves returned error: %v", err)
	}

	// Backdate the just-populated cache entry past its TTL directly (same
	// package, unexported field access - mirrors
	// TestWasmWeatherProvider_FetchForecast_StaleOnErrorFallback) so the next
	// FetchWaves call is forced to attempt a live call rather than serving a
	// within-TTL hit.
	cacheKey := "30.0,40.0,2"
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
	// deterministically (instance creation errors out), simulating an
	// upstream outage without needing a second, failure-simulating fixture.
	provider.compiled.Close(context.Background())

	stale, err := provider.FetchWaves(30.0, 40.0, 2)
	if err != nil {
		t.Fatalf("expected a stale-cache fallback instead of an error, got: %v", err)
	}
	if !stale.Cached {
		t.Fatalf("expected the stale fallback bundle to be marked Cached=true")
	}
	if len(stale.Hourly) != len(fresh.Hourly) {
		t.Fatalf("expected the stale fallback to carry the original fetched data")
	}
}

func TestLoadWasmWaveProviders_RegistersValidPlugin(t *testing.T) {
	withCleanWaveProviderRegistry(t)

	dir := t.TempDir()
	goodBytes, err := os.ReadFile(waveValidFixtureWasm)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "wavevalid.wasm"), goodBytes, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.wasm"), []byte("not a real wasm module"), 0o644); err != nil {
		t.Fatalf("write corrupt fixture: %v", err)
	}

	loadWasmWaveProviders(dir)

	provider, ok := getWaveProvider("wave-valid-fixture")
	if !ok {
		t.Fatalf("expected the wavevalid plugin to be registered")
	}
	if provider.Name() != "Wave Valid Fixture Provider" {
		t.Errorf("unexpected provider name: %q", provider.Name())
	}
	if len(waveProviderOrder) != 1 {
		t.Errorf("expected exactly 1 registered provider, got %d: %v", len(waveProviderOrder), waveProviderOrder)
	}
}

func TestPluginsWavesDir_DefaultsAndEnvOverride(t *testing.T) {
	if got := pluginsWavesDir(); got != "plugins/waves" {
		t.Fatalf("expected default 'plugins/waves', got %q", got)
	}

	t.Setenv("PLUGINS_WAVES_DIR", "/tmp/custom-wave-plugins")
	if got := pluginsWavesDir(); got != "/tmp/custom-wave-plugins" {
		t.Fatalf("expected env override to take effect, got %q", got)
	}
}

// TestMapWasmFetchWavesOutput_SeaTempAbsentMapsToNilPointer directly unit
// tests the "sea temp entirely absent" JSON-unmarshal-to-nil-pointer
// behavior described in the wave plugin contract, without requiring a
// second real WASM fixture: a *float64 field with the right JSON tag
// naturally stays nil when the key is absent from the JSON payload.
func TestMapWasmFetchWavesOutput_SeaTempAbsentMapsToNilPointer(t *testing.T) {
	raw := `{"hourly":[{"time":"2026-07-19T05:00:00+10:00","wave_height_m":1.2,"wave_period_s":8.5,"wave_direction_deg":140,"wind_wave_height_m":0.4,"swell_wave_height_m":1.0}]}`

	var parsed wasmFetchWavesOutput
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("failed to unmarshal fixture JSON: %v", err)
	}
	if parsed.SeaSurfaceTemperatureC != nil {
		t.Fatalf("expected SeaSurfaceTemperatureC to be nil when the JSON key is absent, got %v", *parsed.SeaSurfaceTemperatureC)
	}

	bundle, err := mapWasmFetchWavesOutput(parsed)
	if err != nil {
		t.Fatalf("mapWasmFetchWavesOutput returned error: %v", err)
	}
	if bundle.SeaTempC != nil {
		t.Fatalf("expected mapped bundle.SeaTempC to be nil, got %v", *bundle.SeaTempC)
	}
	if len(bundle.Hourly) != 1 {
		t.Fatalf("expected 1 hourly entry, got %d", len(bundle.Hourly))
	}
}

func TestMapWasmFetchWavesOutput_SeaTempPresentMapsToPointer(t *testing.T) {
	raw := `{"hourly":[{"time":"2026-07-19T05:00:00+10:00","wave_height_m":1.2,"wave_period_s":8.5,"wave_direction_deg":140,"wind_wave_height_m":0.4,"swell_wave_height_m":1.0}],"sea_surface_temperature_c":22.1}`

	var parsed wasmFetchWavesOutput
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("failed to unmarshal fixture JSON: %v", err)
	}
	if parsed.SeaSurfaceTemperatureC == nil {
		t.Fatalf("expected SeaSurfaceTemperatureC to be non-nil when the JSON key is present")
	}

	bundle, err := mapWasmFetchWavesOutput(parsed)
	if err != nil {
		t.Fatalf("mapWasmFetchWavesOutput returned error: %v", err)
	}
	if bundle.SeaTempC == nil || *bundle.SeaTempC != 22.1 {
		t.Fatalf("expected mapped bundle.SeaTempC to be 22.1, got %v", bundle.SeaTempC)
	}
}
