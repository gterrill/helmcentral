package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const weatherValidFixtureWasm = "testdata/wasm_plugins/weathervalid.wasm"

// mustNewWasmWeatherProvider constructs a provider against weatherValidFixtureWasm
// with its disk-backed cache file redirected into a fresh t.TempDir() (via the
// WEATHER_WASM_CACHE_FILE_<ID> env override newWasmWeatherProviderFromBase
// honors) - without this, every test would share and persist to the repo's
// real cache/weather_wasm_weather-valid-fixture_cache.json, so a fresh-fetch
// assertion in one test run could spuriously see a prior run's still-within-TTL
// cache entry on disk.
func mustNewWasmWeatherProvider(t *testing.T, path string) *wasmWeatherProvider {
	t.Helper()
	cacheFile := filepath.Join(t.TempDir(), "weather-wasm-cache.json")
	t.Setenv("WEATHER_WASM_CACHE_FILE_WEATHER_VALID_FIXTURE", cacheFile)

	p, err := newWasmWeatherProvider(path)
	if err != nil {
		t.Fatalf("newWasmWeatherProvider(%q) failed: %v", path, err)
	}
	return p
}

func TestWasmWeatherProvider_FetchForecast_MapsFixtureFieldsCorrectly(t *testing.T) {
	provider := mustNewWasmWeatherProvider(t, weatherValidFixtureWasm)

	bundle, err := provider.FetchForecast(-27.4, 153.0, 2)
	if err != nil {
		t.Fatalf("FetchForecast returned error: %v", err)
	}

	if bundle.Current.Condition != "clear" {
		t.Errorf("expected current condition 'clear', got %q", bundle.Current.Condition)
	}
	if bundle.Current.TemperatureC != 20.0 {
		t.Errorf("expected current temperature_c 20.0, got %v", bundle.Current.TemperatureC)
	}
	if bundle.Current.Time.IsZero() {
		t.Errorf("expected current.time to be parsed, got zero time")
	}

	if len(bundle.Days) != 2 {
		t.Fatalf("expected 2 days, got %d", len(bundle.Days))
	}
	if bundle.Days[0].Condition != "clear" || bundle.Days[1].Condition != "rain" {
		t.Errorf("unexpected day conditions: %+v", bundle.Days)
	}
	if bundle.Days[0].Sunset.IsZero() {
		t.Errorf("expected day 0 sunset to be parsed, got zero time")
	}

	if len(bundle.Hourly) != 4 {
		t.Fatalf("expected 4 hourly entries, got %d", len(bundle.Hourly))
	}
	sawNightHour := false
	for _, h := range bundle.Hourly {
		if !h.IsDaylight {
			sawNightHour = true
		}
	}
	if !sawNightHour {
		t.Errorf("expected at least one hourly entry with is_daylight:false")
	}

	if bundle.Cached {
		t.Errorf("expected a fresh (non-cached) fetch on first call")
	}
	if bundle.CachedAt.IsZero() {
		t.Errorf("expected CachedAt to be set")
	}
}

func TestWasmWeatherProvider_FetchForecast_CachesWithinTTL(t *testing.T) {
	provider := mustNewWasmWeatherProvider(t, weatherValidFixtureWasm)

	first, err := provider.FetchForecast(10.0, 20.0, 2)
	if err != nil {
		t.Fatalf("first FetchForecast returned error: %v", err)
	}
	if first.Cached {
		t.Fatalf("expected the first fetch to be a live (non-cached) fetch")
	}

	second, err := provider.FetchForecast(10.0, 20.0, 2)
	if err != nil {
		t.Fatalf("second FetchForecast returned error: %v", err)
	}
	if !second.Cached {
		t.Fatalf("expected the second fetch within TTL to be served from cache")
	}
	if second.Current.TemperatureC != first.Current.TemperatureC {
		t.Fatalf("expected the cached bundle's data to match the original fetch")
	}
}

func TestWasmWeatherProvider_FetchForecast_StaleOnErrorFallback(t *testing.T) {
	provider := mustNewWasmWeatherProvider(t, weatherValidFixtureWasm)

	fresh, err := provider.FetchForecast(30.0, 40.0, 2)
	if err != nil {
		t.Fatalf("initial FetchForecast returned error: %v", err)
	}

	// Backdate the just-populated cache entry past its TTL directly (same
	// package, unexported field access - mirrors
	// TestWasmPluginCache_PastTTLMissesButGetStaleHits in wasm_plugin_test.go)
	// so the next FetchForecast call is forced to attempt a live call rather
	// than serving a within-TTL hit.
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

	stale, err := provider.FetchForecast(30.0, 40.0, 2)
	if err != nil {
		t.Fatalf("expected a stale-cache fallback instead of an error, got: %v", err)
	}
	if !stale.Cached {
		t.Fatalf("expected the stale fallback bundle to be marked Cached=true")
	}
	if stale.Current.TemperatureC != fresh.Current.TemperatureC {
		t.Fatalf("expected the stale fallback to carry the original fetched data")
	}
}

func TestLoadWasmWeatherProviders_RegistersValidPlugin(t *testing.T) {
	withCleanWeatherProviderRegistry(t)

	dir := t.TempDir()
	goodBytes, err := os.ReadFile(weatherValidFixtureWasm)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "weathervalid.wasm"), goodBytes, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.wasm"), []byte("not a real wasm module"), 0o644); err != nil {
		t.Fatalf("write corrupt fixture: %v", err)
	}

	loadWasmWeatherProviders(dir)

	provider, ok := getWeatherProvider("weather-valid-fixture")
	if !ok {
		t.Fatalf("expected the weathervalid plugin to be registered")
	}
	if provider.Name() != "Weather Valid Fixture Provider" {
		t.Errorf("unexpected provider name: %q", provider.Name())
	}
	if len(weatherProviderOrder) != 1 {
		t.Errorf("expected exactly 1 registered provider, got %d: %v", len(weatherProviderOrder), weatherProviderOrder)
	}
}

func TestPluginsWeatherDir_DefaultsAndEnvOverride(t *testing.T) {
	if got := pluginsWeatherDir(); got != "plugins/weather" {
		t.Fatalf("expected default 'plugins/weather', got %q", got)
	}

	t.Setenv("PLUGINS_WEATHER_DIR", "/tmp/custom-weather-plugins")
	if got := pluginsWeatherDir(); got != "/tmp/custom-weather-plugins" {
		t.Fatalf("expected env override to take effect, got %q", got)
	}
}
