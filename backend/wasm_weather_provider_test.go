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

// TestMapWasmFetchForecastOutput_NextHourAbsentStaysNil covers a provider
// with no nowcast coverage at all (e.g. WeatherKit outside its
// forecastNextHour region, or a plugin that predates next_hour entirely,
// like weathervalid.wasm above): the JSON key is simply missing, and the
// mapped bundle must carry a nil/empty NextHour, never a fabricated dry
// window - see the fallback policy in AGENTS.md and the "no next_hour" case
// in weather_providers.go's top doc comment.
func TestMapWasmFetchForecastOutput_NextHourAbsentStaysNil(t *testing.T) {
	out := wasmFetchForecastOutput{
		Current: wasmWeatherCurrentOutput{Time: "2026-06-14T00:00:00Z"},
		Days:    []wasmWeatherDayOutput{{Start: "2026-06-14T00:00:00Z"}},
	}

	bundle, err := mapWasmFetchForecastOutput(out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bundle.NextHour) != 0 {
		t.Fatalf("expected an absent next_hour to map to an empty slice, got %+v", bundle.NextHour)
	}
}

// TestMapWasmFetchForecastOutput_ParsesNextHourPoints pins the happy path:
// each point's time is parsed, and chance/mm-per-h pass through unchanged
// and in the order the plugin sent them - the host does not sort, since the
// contract requires the plugin to already emit them in time order.
func TestMapWasmFetchForecastOutput_ParsesNextHourPoints(t *testing.T) {
	out := wasmFetchForecastOutput{
		Current: wasmWeatherCurrentOutput{Time: "2026-06-14T00:00:00Z"},
		Days:    []wasmWeatherDayOutput{{Start: "2026-06-14T00:00:00Z"}},
		NextHour: []wasmWeatherNextHourOutput{
			{Time: "2026-06-14T00:00:00Z", PrecipitationChancePct: 10, PrecipitationMMPerH: 0},
			{Time: "2026-06-14T00:01:00Z", PrecipitationChancePct: 40, PrecipitationMMPerH: 1.2},
		},
		NextHourSource: "nowcast",
	}

	bundle, err := mapWasmFetchForecastOutput(out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bundle.NextHour) != 2 {
		t.Fatalf("expected 2 next_hour points, got %d", len(bundle.NextHour))
	}
	if bundle.NextHourSource != "nowcast" {
		t.Fatalf("expected next_hour_source to pass through, got %q", bundle.NextHourSource)
	}
	if bundle.NextHour[0].Time.IsZero() || bundle.NextHour[1].Time.IsZero() {
		t.Fatalf("expected both next_hour times to be parsed, got %+v", bundle.NextHour)
	}
	if !bundle.NextHour[1].Time.After(bundle.NextHour[0].Time) {
		t.Fatalf("expected next_hour points to stay in the order the plugin sent them")
	}
	if bundle.NextHour[0].PrecipitationChancePct != 10 || bundle.NextHour[1].PrecipitationChancePct != 40 {
		t.Fatalf("expected chance_pct to pass through unchanged, got %+v", bundle.NextHour)
	}
	if bundle.NextHour[1].PrecipitationMMPerH != 1.2 {
		t.Fatalf("expected mm_per_h to pass through unchanged, got %v", bundle.NextHour[1].PrecipitationMMPerH)
	}
}

// TestMapWasmFetchForecastOutput_NextHourNegativeChancePassesThrough covers
// the "provider has intensity but no probability at this resolution" case
// (e.g. Open-Meteo's minutely_15, which has no minutely precipitation_probability
// variable): a negative chance_pct is the same "not supplied" convention
// current/days/hourly already use, and it must reach the bundle unchanged,
// not get clamped to 0 (which would read as "definitely won't rain").
func TestMapWasmFetchForecastOutput_NextHourNegativeChancePassesThrough(t *testing.T) {
	out := wasmFetchForecastOutput{
		Current: wasmWeatherCurrentOutput{Time: "2026-06-14T00:00:00Z"},
		Days:    []wasmWeatherDayOutput{{Start: "2026-06-14T00:00:00Z"}},
		NextHour: []wasmWeatherNextHourOutput{
			{Time: "2026-06-14T00:00:00Z", PrecipitationChancePct: -1, PrecipitationMMPerH: 0.6},
		},
		NextHourSource: "hourly",
	}

	bundle, err := mapWasmFetchForecastOutput(out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bundle.NextHour[0].PrecipitationChancePct != -1 {
		t.Fatalf("expected the not-supplied sentinel to pass through unchanged, got %v", bundle.NextHour[0].PrecipitationChancePct)
	}
	if bundle.NextHour[0].PrecipitationMMPerH != 0.6 {
		t.Fatalf("expected mm_per_h to still be mapped when chance is unsupplied, got %v", bundle.NextHour[0].PrecipitationMMPerH)
	}
}

// TestMapWasmFetchForecastOutput_NextHourRequiresTimePerPoint is the
// fail-fast case: a plugin bug that emits a next_hour point with no time is
// a hard error, same as every other required-time field in this contract -
// never silently dropped or zeroed (AGENTS.md fallback policy).
func TestMapWasmFetchForecastOutput_NextHourRequiresTimePerPoint(t *testing.T) {
	out := wasmFetchForecastOutput{
		Current:  wasmWeatherCurrentOutput{Time: "2026-06-14T00:00:00Z"},
		Days:     []wasmWeatherDayOutput{{Start: "2026-06-14T00:00:00Z"}},
		NextHour: []wasmWeatherNextHourOutput{{PrecipitationChancePct: 10}},
	}

	if _, err := mapWasmFetchForecastOutput(out); err == nil {
		t.Fatalf("expected an error for a next_hour point missing time, got nil")
	}
}

// TestMapWasmFetchForecastOutput_NextHourRequiresSource is the fail-fast
// case for next_hour_source itself: next_hour is present but the plugin
// sent no next_hour_source at all (a plugin built before this field
// existed, or a bug that dropped it) - this must be a hard error, not a
// silent "assume nowcast" default, since a provider whose nowcast is
// actually interpolated hourly data masquerading as real short-range data
// is exactly the failure this field exists to prevent (AGENTS.md fallback
// policy).
func TestMapWasmFetchForecastOutput_NextHourRequiresSource(t *testing.T) {
	out := wasmFetchForecastOutput{
		Current:  wasmWeatherCurrentOutput{Time: "2026-06-14T00:00:00Z"},
		Days:     []wasmWeatherDayOutput{{Start: "2026-06-14T00:00:00Z"}},
		NextHour: []wasmWeatherNextHourOutput{{Time: "2026-06-14T00:00:00Z", PrecipitationChancePct: 10}},
		// NextHourSource deliberately left empty.
	}

	if _, err := mapWasmFetchForecastOutput(out); err == nil {
		t.Fatalf("expected an error for next_hour present with no next_hour_source, got nil")
	}
}

// TestMapWasmFetchForecastOutput_NextHourRejectsUnknownSource covers a typo
// or a plugin sending some other value ("interpolated", "unknown", ...) -
// only the two contract-defined values are accepted.
func TestMapWasmFetchForecastOutput_NextHourRejectsUnknownSource(t *testing.T) {
	out := wasmFetchForecastOutput{
		Current:        wasmWeatherCurrentOutput{Time: "2026-06-14T00:00:00Z"},
		Days:           []wasmWeatherDayOutput{{Start: "2026-06-14T00:00:00Z"}},
		NextHour:       []wasmWeatherNextHourOutput{{Time: "2026-06-14T00:00:00Z", PrecipitationChancePct: 10}},
		NextHourSource: "interpolated",
	}

	if _, err := mapWasmFetchForecastOutput(out); err == nil {
		t.Fatalf("expected an error for an unrecognized next_hour_source, got nil")
	}
}

// TestMapWasmFetchForecastOutput_NextHourSourceIgnoredWhenNextHourEmpty
// covers the inverse: no next_hour points at all means next_hour_source is
// simply not looked at, whatever value (or none) the plugin sent.
func TestMapWasmFetchForecastOutput_NextHourSourceIgnoredWhenNextHourEmpty(t *testing.T) {
	out := wasmFetchForecastOutput{
		Current: wasmWeatherCurrentOutput{Time: "2026-06-14T00:00:00Z"},
		Days:    []wasmWeatherDayOutput{{Start: "2026-06-14T00:00:00Z"}},
		// NextHour absent, NextHourSource absent too.
	}

	bundle, err := mapWasmFetchForecastOutput(out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bundle.NextHourSource != "" {
		t.Fatalf("expected NextHourSource to stay empty when next_hour is empty, got %q", bundle.NextHourSource)
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
	// than serving a within-TTL hit. FetchForecast always caches under
	// weatherFetchMaxDays regardless of the days argument (2, above) - see
	// weatherFetchMaxDays's doc comment.
	cacheKey := weatherWasmCacheKey(30.0, 40.0, weatherFetchMaxDays, "Etc/GMT-3")
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

// TestSliceWeatherForecastBundle_TrimsDaysAndMatchingHourly is the direct
// unit test for the day-slicing fix: a bundle fetched for the full
// weatherFetchMaxDays window, sliced to fewer days, keeps only that many
// Days entries and only the Hourly entries that actually fall within them -
// using nothing but the typed Start/Time fields the host already parses,
// no plugin-specific format knowledge.
func TestSliceWeatherForecastBundle_TrimsDaysAndMatchingHourly(t *testing.T) {
	day0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	day1 := day0.AddDate(0, 0, 1)
	day2 := day0.AddDate(0, 0, 2)

	bundle := weatherForecastBundle{
		Days: []weatherDayPoint{
			{Start: day0, Condition: "day0"},
			{Start: day1, Condition: "day1"},
			{Start: day2, Condition: "day2"},
		},
		Hourly: []weatherHourPoint{
			{Time: day0.Add(3 * time.Hour)},
			{Time: day0.Add(20 * time.Hour)},
			{Time: day1.Add(3 * time.Hour)},
			{Time: day1.Add(20 * time.Hour)},
			{Time: day2.Add(3 * time.Hour)},
		},
	}

	sliced := sliceWeatherForecastBundle(bundle, 2)

	if len(sliced.Days) != 2 || sliced.Days[0].Condition != "day0" || sliced.Days[1].Condition != "day1" {
		t.Fatalf("expected exactly [day0, day1], got %+v", sliced.Days)
	}
	if len(sliced.Hourly) != 4 {
		t.Fatalf("expected 4 hourly entries (2 per day for day0/day1), got %d: %+v", len(sliced.Hourly), sliced.Hourly)
	}
	for _, hp := range sliced.Hourly {
		if !hp.Time.Before(day2) {
			t.Errorf("expected every remaining hourly entry to fall before day2's start, got %v", hp.Time)
		}
	}
}

// TestSliceWeatherForecastBundle_NoOpWhenDaysCoversOrExceedsTheWholeBundle
// covers both no-op cases: a non-positive days, and a days at or past the
// bundle's actual length (a plugin that returned fewer than
// weatherFetchMaxDays days must never be padded out).
func TestSliceWeatherForecastBundle_NoOpWhenDaysCoversOrExceedsTheWholeBundle(t *testing.T) {
	bundle := weatherForecastBundle{
		Days:   []weatherDayPoint{{Condition: "only-day"}},
		Hourly: []weatherHourPoint{{Time: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)}},
	}

	for _, days := range []int{0, -1, 1, 5} {
		sliced := sliceWeatherForecastBundle(bundle, days)
		if len(sliced.Days) != 1 || len(sliced.Hourly) != 1 {
			t.Errorf("days=%d: expected the bundle unchanged, got Days=%+v Hourly=%+v", days, sliced.Days, sliced.Hourly)
		}
	}
}

// TestWasmWeatherProvider_FetchForecast_SharesOneCacheEntryAcrossDays is the
// end-to-end proof of the fix, through a real compiled plugin: a
// weatherToday-shaped call (days=1) and a weatherForecast-shaped call
// (days=2, the max this fixture actually returns) at the same position
// share ONE cache entry - the second call is served from cache even though
// its own days argument differs from the first's - rather than each days
// value paying its own upstream fetch.
func TestWasmWeatherProvider_FetchForecast_SharesOneCacheEntryAcrossDays(t *testing.T) {
	provider := mustNewWasmWeatherProvider(t, weatherValidFixtureWasm)

	today, err := provider.FetchForecast(-27.4, 153.0, 1, "Etc/GMT-10")
	if err != nil {
		t.Fatalf("first FetchForecast (days=1) returned error: %v", err)
	}
	if today.Cached {
		t.Fatalf("expected the first fetch to be a live (non-cached) fetch")
	}
	if len(today.Days) != 1 {
		t.Fatalf("expected days=1 to return exactly 1 day, got %d", len(today.Days))
	}

	forecast, err := provider.FetchForecast(-27.4, 153.0, 2, "Etc/GMT-10")
	if err != nil {
		t.Fatalf("second FetchForecast (days=2) returned error: %v", err)
	}
	if !forecast.Cached {
		t.Fatalf("expected the days=2 call at the same position to hit the same max-days cache entry the days=1 call already populated")
	}
	if len(forecast.Days) != 2 {
		t.Fatalf("expected days=2 to return 2 days, got %d", len(forecast.Days))
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
