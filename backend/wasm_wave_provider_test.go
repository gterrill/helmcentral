package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	if bundle.Hourly[0].WaveHeightM == nil || *bundle.Hourly[0].WaveHeightM != 1.2 {
		t.Errorf("expected wave_height_m 1.2, got %v", bundle.Hourly[0].WaveHeightM)
	}
	if bundle.Hourly[0].WavePeriodS == nil || *bundle.Hourly[0].WavePeriodS != 8.5 {
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
	// Trailing comma: waveWasmCacheKey delegates to weatherWasmCacheKey with
	// an empty timezone, since fetch_waves has no daily rollup to place.
	cacheKey := "30.0,40.0,2,"
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

// A plugin omits wave_height_m, wave_period_s or wave_direction_deg when its
// model has no value. The host reads the missing key as absent, which is not
// the same as 0 m, 0 s or due north.
func TestMapWasmFetchWavesOutput_OmittedCombinedFiguresMapToAbsent(t *testing.T) {
	raw := `{"hourly":[
		{"time":"2026-07-19T05:00:00Z","wave_height_m":1.2},
		{"time":"2026-07-19T06:00:00Z","wave_height_m":0,"wave_period_s":0,"wave_direction_deg":0},
		{"time":"2026-07-19T07:00:00Z"}]}`
	var parsed wasmFetchWavesOutput
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatal(err)
	}
	bundle, err := mapWasmFetchWavesOutput(parsed)
	if err != nil {
		t.Fatal(err)
	}
	a, b, c := bundle.Hourly[0], bundle.Hourly[1], bundle.Hourly[2]
	if a.WaveHeightM == nil || *a.WaveHeightM != 1.2 || a.WavePeriodS != nil || a.WaveDirectionDeg != nil {
		t.Errorf("height only: %+v", a)
	}
	if b.WaveHeightM != nil || b.WavePeriodS != nil || b.WaveDirectionDeg != nil {
		t.Errorf("a zero period is no reading, so the whole hour is absent: %+v", b)
	}
	if c.WaveHeightM != nil || c.WavePeriodS != nil || c.WaveDirectionDeg != nil {
		t.Errorf("no keys must be absent: %+v", c)
	}
}

// A plugin that reports an impossible figure is broken, and the host says so
// rather than reading the figure as absent.
func TestMapWasmFetchWavesOutput_ImpossibleFiguresAreErrors(t *testing.T) {
	cases := map[string]string{
		"negative height":     `{"time":"2026-07-19T05:00:00Z","wave_height_m":1},{"time":"2026-07-19T06:00:00Z","wave_height_m":-0.5}`,
		"negative period":     `{"time":"2026-07-19T05:00:00Z","wave_height_m":1},{"time":"2026-07-19T06:00:00Z","wave_period_s":-3}`,
		"direction below 0":   `{"time":"2026-07-19T05:00:00Z","wave_height_m":1},{"time":"2026-07-19T06:00:00Z","wave_direction_deg":-10}`,
		"direction above 360": `{"time":"2026-07-19T05:00:00Z","wave_height_m":1},{"time":"2026-07-19T06:00:00Z","wave_direction_deg":361}`,
	}
	for name, hours := range cases {
		t.Run(name, func(t *testing.T) {
			var parsed wasmFetchWavesOutput
			if err := json.Unmarshal([]byte(`{"hourly":[`+hours+`]}`), &parsed); err != nil {
				t.Fatal(err)
			}
			_, err := mapWasmFetchWavesOutput(parsed)
			if err == nil || !strings.Contains(err.Error(), "hourly[1]") {
				t.Fatalf("want an error naming hourly[1], got %v", err)
			}
		})
	}
	var ok wasmFetchWavesOutput
	if err := json.Unmarshal([]byte(`{"hourly":[{"time":"2026-07-19T05:00:00Z","wave_height_m":0,"wave_period_s":0,"wave_direction_deg":360}]}`), &ok); err != nil {
		t.Fatal(err)
	}
	if _, err := mapWasmFetchWavesOutput(ok); err != nil {
		t.Fatalf("0 m, 0 s and 360 degrees are in range: %v", err)
	}
}

// A cache file written before absent figures existed holds 0 for a null, and
// would read as a real 0 m sea, including through the stale fallback. It is
// not served.
func TestWasmWaveProvider_OldFormatCacheFileIsNotServed(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("HELMCENTRAL_STATE_DIR", stateDir)
	t.Setenv("WAVE_WASM_CACHE_FILE_WAVE_VALID_FIXTURE", "")

	oldPath := filepath.Join(stateDir, "cache", "wave_wasm_wave-valid-fixture_cache.json")
	zero := 0.0
	old := newWasmPluginCache[waveForecastBundle](oldPath)
	old.set("30.0,40.0,2,", waveForecastBundle{
		Hourly:   []waveHourPoint{{Time: time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC), WaveHeightM: &zero, WavePeriodS: &zero, WaveDirectionDeg: &zero}},
		CachedAt: time.Now().UTC(),
	})
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("test setup: old cache file not written: %v", err)
	}

	p, err := newWasmWaveProvider(waveValidFixtureWasm)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.cache.getStale("30.0,40.0,2,"); ok {
		t.Fatal("an old-format wave cache entry must not be served, not even as stale")
	}
}

// A provider that cannot place a point in the model (a land-masked cell)
// reports every figure as 0 rather than null. A wave with no period is not a
// wave, so the host reads the hour as absent instead of a flat calm from due
// north.
func TestMapWasmFetchWavesOutput_ZeroPeriodHourIsAbsent(t *testing.T) {
	raw := `{"hourly":[
		{"time":"2026-07-19T05:00:00Z","wave_height_m":0,"wave_period_s":0,"wave_direction_deg":0,
		 "wind_wave_height_m":0,"wind_wave_period_s":0,"wind_wave_direction_deg":0,
		 "swell_wave_height_m":0,"swell_wave_period_s":0,"swell_wave_direction_deg":0},
		{"time":"2026-07-19T06:00:00Z","wave_height_m":0.6,"wave_period_s":7.4,"wave_direction_deg":79,
		 "wind_wave_height_m":0.6,"wind_wave_period_s":7.4,"wind_wave_direction_deg":79,
		 "swell_wave_height_m":0.3,"swell_wave_period_s":0,"swell_wave_direction_deg":65}]}`
	var parsed wasmFetchWavesOutput
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatal(err)
	}
	bundle, err := mapWasmFetchWavesOutput(parsed)
	if err != nil {
		t.Fatal(err)
	}
	land, ok := bundle.Hourly[0], bundle.Hourly[1]
	if land.WaveHeightM != nil || land.WavePeriodS != nil || land.WaveDirectionDeg != nil {
		t.Errorf("zero-period hour must be absent: %+v", land)
	}
	if !waveSeriesHasNoValues(bundle.Hourly[:1]) {
		t.Errorf("an all-zero hour is a series with no values")
	}
	if ok.WaveHeightM == nil || *ok.WaveHeightM != 0.6 {
		t.Errorf("a real hour is kept: %+v", ok)
	}
	// The swell component has no period, so it is no train: not 0.3 m from 65.
	if ok.SwellWaveHeightM != 0 || ok.SwellWaveDirectionDeg != 0 || ok.SwellWavePeriodS != 0 {
		t.Errorf("a component with no period is absent: %+v", ok)
	}
	if ok.WindWaveHeightM != 0.6 || ok.WindWavePeriodS != 7.4 {
		t.Errorf("a component with a period is kept: %+v", ok)
	}
}

func TestMapWasmFetchWavesOutput_CarriesTheModelAndFallbackFlag(t *testing.T) {
	var parsed wasmFetchWavesOutput
	raw := `{"hourly":[{"time":"2026-07-19T05:00:00Z","wave_height_m":0.2,"wave_period_s":3}],"model":"best_match","fallback_used":true}`
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatal(err)
	}
	bundle, err := mapWasmFetchWavesOutput(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.FallbackModel != "best_match" {
		t.Errorf("fallback model %q", bundle.FallbackModel)
	}
}

// Saving a plugin's settings must make the next call fetch afresh, or a
// switch turned on would not apply until the old answer expired.
func TestPostPluginConfigHandler_ConfigChangeYieldsAFreshFetch(t *testing.T) {
	withCleanWaveProviderRegistry(t)
	withTestPluginOverridesStore(t)

	dir := t.TempDir()
	wasmPath := filepath.Join(dir, "plugin.wasm")
	raw, err := os.ReadFile(waveValidFixtureWasm)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wasmPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.config_fields.json"), []byte(`[{"key":"flag","label":"Flag","type":"boolean"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAVE_WASM_CACHE_FILE_WAVE_VALID_FIXTURE", filepath.Join(dir, "cache.json"))
	provider, err := newWasmWaveProvider(wasmPath)
	if err != nil {
		t.Fatal(err)
	}
	registerWaveProvider(provider)
	if _, err := provider.FetchWaves(30, 40, 2); err != nil {
		t.Fatal(err)
	}
	again, err := provider.FetchWaves(30, 40, 2)
	if err != nil || !again.Cached {
		t.Fatalf("expected a cache hit before the save: %v cached=%v", err, again.Cached)
	}

	c, rec := newPluginTestEchoContext(http.MethodPost, "/api/plugins/wave/"+provider.ID()+"/config", `{"values":{"flag":"true"}}`, "wave", provider.ID())
	if err := postPluginConfigHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("save: %v %d %s", err, rec.Code, rec.Body.String())
	}
	fresh, err := provider.FetchWaves(30, 40, 2)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Cached {
		t.Fatal("a config change must yield a fresh fetch, not the cached answer")
	}
}
