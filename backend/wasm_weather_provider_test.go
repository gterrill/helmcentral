package main

import (
	"context"
	"encoding/json"
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

	bundle, err := provider.FetchForecast(-27.4, 153.0, 2, "Etc/GMT-10")
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

	// weathervalid.wasm predates humidity/visibility and never emits either
	// field - the real "field entirely absent from the wire" case, exercised
	// end-to-end through an actual compiled plugin rather than a literal Go
	// struct. It must map to the -1 sentinel, never a fabricated 0.
	for _, h := range bundle.Hourly {
		if h.HumidityPct != -1 {
			t.Errorf("expected the fixture plugin (which never emits humidity_pct) to map to -1, got %v", h.HumidityPct)
		}
		if h.VisibilityNm != -1 {
			t.Errorf("expected the fixture plugin (which never emits visibility_m) to map to -1, got %v", h.VisibilityNm)
		}
	}
}

// wasmWeatherHourOutput.HumidityPct/VisibilityM are *float64, deliberately -
// see sentinelHumidityPct/sentinelVisibilityNm in weather_providers.go. A
// bare float64 would decode a JSON-absent field to 0.0, indistinguishable
// from a real 0% humidity or 0.0nm visibility reading. This test pins the
// unmarshal behavior the whole convention depends on.
func TestWasmWeatherHourOutput_MissingHumidityAndVisibilityUnmarshalToNilNotZero(t *testing.T) {
	raw := []byte(`{"time":"2026-06-14T00:00:00Z","temperature_c":18}`)
	var out wasmWeatherHourOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if out.HumidityPct != nil {
		t.Fatalf("expected HumidityPct to unmarshal to nil when the JSON key is absent, got %v", *out.HumidityPct)
	}
	if out.VisibilityM != nil {
		t.Fatalf("expected VisibilityM to unmarshal to nil when the JSON key is absent, got %v", *out.VisibilityM)
	}
}

func TestMapWasmFetchForecastOutput_MissingHumidityAndVisibilityBecomeSentinel(t *testing.T) {
	out := wasmFetchForecastOutput{
		Current: wasmWeatherCurrentOutput{Time: "2026-06-14T00:00:00Z"},
		Days:    []wasmWeatherDayOutput{{Start: "2026-06-14T00:00:00Z"}},
		Hourly:  []wasmWeatherHourOutput{{Time: "2026-06-14T00:00:00Z"}}, // no humidity_pct/visibility_m at all
	}

	bundle, err := mapWasmFetchForecastOutput(out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bundle.Hourly[0].HumidityPct != -1 {
		t.Fatalf("expected an entirely-missing humidity field to map to the -1 sentinel, got %v", bundle.Hourly[0].HumidityPct)
	}
	if bundle.Hourly[0].VisibilityNm != -1 {
		t.Fatalf("expected an entirely-missing visibility field to map to the -1 sentinel, got %v", bundle.Hourly[0].VisibilityNm)
	}
}

// The most important test in this change, repeated at the mapping boundary:
// a genuine 0.0m visibility reading (real fog) must survive mapWasmFetchForecastOutput
// as 0.0nm, not collapse into the same -1 sentinel as an absent field.
func TestMapWasmFetchForecastOutput_GenuineZeroVisibilitySurvives(t *testing.T) {
	zero := 0.0
	out := wasmFetchForecastOutput{
		Current: wasmWeatherCurrentOutput{Time: "2026-06-14T00:00:00Z"},
		Days:    []wasmWeatherDayOutput{{Start: "2026-06-14T00:00:00Z"}},
		Hourly:  []wasmWeatherHourOutput{{Time: "2026-06-14T00:00:00Z", VisibilityM: &zero}},
	}

	bundle, err := mapWasmFetchForecastOutput(out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bundle.Hourly[0].VisibilityNm != 0 {
		t.Fatalf("expected a genuine 0.0m reading to survive as 0.0nm, not collapse to the -1 sentinel, got %v", bundle.Hourly[0].VisibilityNm)
	}
}

// The timezone drives the provider's daily rollup boundaries, so two
// requests for the same position in different zones are genuinely different
// bundles and must not share a cache slot - otherwise a vessel crossing a
// zone boundary keeps serving day summaries rolled up on the old boundary.
func TestWeatherWasmCacheKey_DistinguishesTimezone(t *testing.T) {
	brisbane := weatherWasmCacheKey(-21.1, 149.2, 10, "Etc/GMT-10")
	utc := weatherWasmCacheKey(-21.1, 149.2, 10, "UTC")

	if brisbane == utc {
		t.Fatalf("expected different cache keys for different timezones, both were %q", brisbane)
	}
	if same := weatherWasmCacheKey(-21.1, 149.2, 10, "Etc/GMT-10"); same != brisbane {
		t.Fatalf("expected a stable key for identical inputs, got %q then %q", brisbane, same)
	}
}

func TestWasmWeatherProvider_FetchForecast_CachesWithinTTL(t *testing.T) {
	provider := mustNewWasmWeatherProvider(t, weatherValidFixtureWasm)

	first, err := provider.FetchForecast(10.0, 20.0, 2, "Etc/GMT-1")
	if err != nil {
		t.Fatalf("first FetchForecast returned error: %v", err)
	}
	if first.Cached {
		t.Fatalf("expected the first fetch to be a live (non-cached) fetch")
	}

	second, err := provider.FetchForecast(10.0, 20.0, 2, "Etc/GMT-1")
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

	fresh, err := provider.FetchForecast(30.0, 40.0, 2, "Etc/GMT-3")
	if err != nil {
		t.Fatalf("initial FetchForecast returned error: %v", err)
	}

	// Backdate the just-populated cache entry past its TTL directly (same
	// package, unexported field access - mirrors
	// TestWasmPluginCache_PastTTLMissesButGetStaleHits in wasm_plugin_test.go)
	// so the next FetchForecast call is forced to attempt a live call rather
	// than serving a within-TTL hit.
	cacheKey := "30.0,40.0,2,Etc/GMT-3"
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

	stale, err := provider.FetchForecast(30.0, 40.0, 2, "Etc/GMT-3")
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
