package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// fetch_currents answers one series per requested point, in order. An hour
// carries both figures or neither. A missing key is absent (nil), a reported 0
// is a real slack figure and is kept.
func TestMapWasmFetchCurrentsOutput_FiguresAreOptional(t *testing.T) {
	raw := `{"points":[{"hourly":[
		{"time":"2026-07-19T05:00:00Z","current_velocity_ms":0.3,"current_set_deg":153},
		{"time":"2026-07-19T06:00:00Z","current_velocity_ms":0,"current_set_deg":0},
		{"time":"2026-07-19T07:00:00Z"}]},
		{"hourly":[{"time":"2026-07-19T05:00:00Z"}]}]}`
	var parsed wasmFetchCurrentsOutput
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatal(err)
	}
	bundle, err := mapWasmFetchCurrentsOutput(parsed, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Points) != 2 {
		t.Fatalf("one series per point, got %d", len(bundle.Points))
	}
	a, b, c := bundle.Points[0].Hourly[0], bundle.Points[0].Hourly[1], bundle.Points[0].Hourly[2]
	if a.VelocityMS == nil || *a.VelocityMS != 0.3 || a.SetDeg == nil || *a.SetDeg != 153 {
		t.Errorf("current figures lost: %+v", a)
	}
	if b.VelocityMS == nil || *b.VelocityMS != 0 || b.SetDeg == nil || *b.SetDeg != 0 {
		t.Errorf("a reported 0 must be kept: %+v", b)
	}
	if c.VelocityMS != nil || c.SetDeg != nil {
		t.Errorf("no keys must be absent: %+v", c)
	}
}

func TestMapWasmFetchCurrentsOutput_ImpossibleFiguresAreErrors(t *testing.T) {
	cases := map[string]string{
		"negative velocity":    `"current_velocity_ms":-0.1,"current_set_deg":90`,
		"velocity above 10":    `"current_velocity_ms":11,"current_set_deg":90`,
		"set below 0":          `"current_velocity_ms":0.2,"current_set_deg":-5`,
		"set above 360":        `"current_velocity_ms":0.2,"current_set_deg":361`,
		"velocity without set": `"current_velocity_ms":0.2`,
		"set without velocity": `"current_set_deg":90`,
	}
	for name, fields := range cases {
		t.Run(name, func(t *testing.T) {
			var parsed wasmFetchCurrentsOutput
			body := `{"points":[{"hourly":[{"time":"2026-07-19T05:00:00Z"},{"time":"2026-07-19T06:00:00Z",` + fields + `}]}]}`
			if err := json.Unmarshal([]byte(body), &parsed); err != nil {
				t.Fatal(err)
			}
			_, err := mapWasmFetchCurrentsOutput(parsed, 1)
			if err == nil || !strings.Contains(err.Error(), "points[0].hourly[1]") {
				t.Fatalf("want an error naming points[0].hourly[1], got %v", err)
			}
		})
	}
}

func TestMapWasmFetchCurrentsOutput_WrongPointCountIsAnError(t *testing.T) {
	var parsed wasmFetchCurrentsOutput
	if err := json.Unmarshal([]byte(`{"points":[{"hourly":[]}]}`), &parsed); err != nil {
		t.Fatal(err)
	}
	if _, err := mapWasmFetchCurrentsOutput(parsed, 2); err == nil {
		t.Fatal("a plugin that answers for fewer points than asked is broken")
	}
}

// A wave plugin built before currents existed has no fetch_currents. It still
// loads and serves waves; asking it for currents says what to install.
func TestWasmWaveProvider_PluginWithoutFetchCurrentsSaysSo(t *testing.T) {
	p := mustNewWasmWaveProvider(t, waveValidFixtureWasm)
	if p.supportsCurrents {
		t.Fatal("the fixture exports no fetch_currents")
	}
	var cp currentsProvider = p
	_, err := cp.FetchCurrents(context.Background(), []currentsPoint{{Lat: -27.4, Lon: 153.0}}, 2)
	if !errors.Is(err, errNoCurrentsExport) || !strings.Contains(err.Error(), "install the updated open-meteo-marine plugin") || !strings.Contains(err.Error(), "wave-valid-fixture") {
		t.Fatalf("want the install hint naming the plugin, got %v", err)
	}
}

func TestWasmWaveProvider_FetchCurrentsRefusesMoreThanTheCapAndEmpty(t *testing.T) {
	p := mustNewWasmWaveProvider(t, waveValidFixtureWasm)
	p.supportsCurrents = true
	over := make([]currentsPoint, maxCurrentsPoints+1)
	if _, err := p.FetchCurrents(context.Background(), over, 2); err == nil || !strings.Contains(err.Error(), "100") {
		t.Errorf("over the cap must be an error naming it, got %v", err)
	}
	if _, err := p.FetchCurrents(context.Background(), nil, 2); err == nil {
		t.Errorf("no points must be an error")
	}
}

func TestWasmWaveProvider_FetchCurrentsReturnsTheContextError(t *testing.T) {
	p := mustNewWasmWaveProvider(t, waveValidFixtureWasm)
	p.supportsCurrents = true
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.FetchCurrents(ctx, []currentsPoint{{Lat: -27.4, Lon: 153.0}}, 2); err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

// ---- fetch_currents memory cache ----

type currentsCacheRig struct {
	p     *wasmWaveProvider
	calls int
	fail  error
	now   time.Time
}

func newCurrentsCacheRig(t *testing.T) *currentsCacheRig {
	t.Helper()
	r := &currentsCacheRig{now: time.Date(2026, 9, 12, 3, 0, 0, 0, time.UTC)}
	r.p = mustNewWasmWaveProvider(t, waveValidFixtureWasm)
	r.p.supportsCurrents = true
	r.p.currentsNow = func() time.Time { return r.now }
	r.p.currentsCall = func(points []currentsPoint, days int) (currentsBundle, error) {
		r.calls++
		if r.fail != nil {
			return currentsBundle{}, r.fail
		}
		return currentsBundle{Points: make([]currentPointSeries, len(points))}, nil
	}
	return r
}

func TestWasmWaveProvider_FetchCurrentsRepeatWithinThirtyMinutesIsNotRequestedAgain(t *testing.T) {
	r := newCurrentsCacheRig(t)
	pts := []currentsPoint{{Lat: -27.4, Lon: 153.0}, {Lat: -27.5, Lon: 153.1}}
	for i := 0; i < 2; i++ {
		if _, err := r.p.FetchCurrents(context.Background(), pts, 7); err != nil {
			t.Fatal(err)
		}
	}
	if r.calls != 1 {
		t.Errorf("a repeat question must not call the plugin again, got %d calls", r.calls)
	}
	r.now = r.now.Add(29 * time.Minute)
	r.p.FetchCurrents(context.Background(), pts, 7)
	if r.calls != 1 {
		t.Errorf("29 minutes on is still fresh, got %d calls", r.calls)
	}
	// A different question is a different entry.
	r.p.FetchCurrents(context.Background(), pts, 16)
	r.p.FetchCurrents(context.Background(), pts[:1], 7)
	if r.calls != 3 {
		t.Errorf("different days or points are separate entries, got %d calls", r.calls)
	}
}

func TestWasmWaveProvider_FetchCurrentsNeverServesAnExpiredEntryNotEvenOnFailure(t *testing.T) {
	r := newCurrentsCacheRig(t)
	pts := []currentsPoint{{Lat: -27.4, Lon: 153.0}}
	r.p.FetchCurrents(context.Background(), pts, 7)
	r.now = r.now.Add(31 * time.Minute)
	r.fail = errors.New("open-meteo down")
	if _, err := r.p.FetchCurrents(context.Background(), pts, 7); err == nil {
		t.Fatal("an expired entry must not stand in for a failed request")
	}
	// A failure is not remembered either.
	r.fail = nil
	if _, err := r.p.FetchCurrents(context.Background(), pts, 7); err != nil {
		t.Fatal(err)
	}
	if r.calls != 3 {
		t.Errorf("want 3 plugin calls, got %d", r.calls)
	}
}

func TestWasmWaveProvider_FetchCurrentsKeepsOnlyTheLastSixteenQuestions(t *testing.T) {
	r := newCurrentsCacheRig(t)
	for i := 0; i < 17; i++ {
		r.p.FetchCurrents(context.Background(), []currentsPoint{{Lat: -27.0 - float64(i)/10, Lon: 153.0}}, 7)
	}
	r.p.FetchCurrents(context.Background(), []currentsPoint{{Lat: -27.0, Lon: 153.0}}, 7)
	if r.calls != 18 {
		t.Errorf("the oldest of 17 questions must have been dropped, got %d calls", r.calls)
	}
	r.p.FetchCurrents(context.Background(), []currentsPoint{{Lat: -28.6, Lon: 153.0}}, 7)
	if r.calls != 18 {
		t.Errorf("a recent question stays, got %d calls", r.calls)
	}
}

func TestWasmWaveProvider_FetchCurrentsCacheStartsANewDayAtUTCMidnight(t *testing.T) {
	r := newCurrentsCacheRig(t)
	r.now = time.Date(2026, 9, 12, 23, 50, 0, 0, time.UTC)
	pts := []currentsPoint{{Lat: -27.4, Lon: 153.0}}
	r.p.FetchCurrents(context.Background(), pts, 7)
	r.now = r.now.Add(15 * time.Minute)
	r.p.FetchCurrents(context.Background(), pts, 7)
	if r.calls != 2 {
		t.Errorf("the series starts at UTC midnight, so a bundle from the day before is not reused, got %d calls", r.calls)
	}
}

func TestWasmWaveProvider_FetchCurrentsKeyKeepsTheRequestedListWithItsDuplicates(t *testing.T) {
	r := newCurrentsCacheRig(t)
	a, b := currentsPoint{Lat: -27.4, Lon: 153.0}, currentsPoint{Lat: -27.5, Lon: 153.1}
	first, _ := r.p.FetchCurrents(context.Background(), []currentsPoint{a, b}, 7)
	second, err := r.p.FetchCurrents(context.Background(), []currentsPoint{a, b, a}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Points) != 2 || len(second.Points) != 3 || r.calls != 2 {
		t.Errorf("a bundle answers the requested list point for point: got %d and %d points from %d calls", len(first.Points), len(second.Points), r.calls)
	}
}

func TestWasmWaveProvider_FetchCurrentsMergesIdenticalRequestsInFlight(t *testing.T) {
	r := newCurrentsCacheRig(t)
	var mu sync.Mutex
	calls := 0
	r.p.currentsCall = func(points []currentsPoint, days int) (currentsBundle, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		time.Sleep(200 * time.Millisecond)
		return currentsBundle{Points: make([]currentPointSeries, len(points))}, nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.p.FetchCurrents(context.Background(), []currentsPoint{{Lat: -27.4, Lon: 153.0}}, 7)
		}()
	}
	wg.Wait()
	if calls != 1 {
		t.Errorf("identical requests in flight must share one plugin call, got %d", calls)
	}
}
