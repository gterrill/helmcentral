// Package main: WASM weather provider adapter.
//
// NOTE on this file's name: like wasm_tide_provider.go, it is deliberately
// NOT named anything ending in "_wasm.go" - see that file's header comment
// for the full explanation of why (implicit GOARCH=wasm build constraint).
//
// This is the thin weather-specific layer on top of the shared WASM host
// machinery in wasm_plugin.go: wasmWeatherProvider embeds *wasmPluginBase
// (ID/Name/TTLSeconds/ttlDuration/call all come from there) and adds a
// wasmPluginCache[weatherForecastBundle] plus the weatherProvider interface
// method. See weather_providers.go's top doc comment for the guest contract
// this adapter maps JSON against.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"path/filepath"
	"strings"
	"time"

	extism "github.com/extism/go-sdk"
)

// wasmWeatherProvider is a weatherProvider backed by a sandboxed WASM guest
// module, loaded via extism/go-sdk - same pattern as wasmTideProvider.
type wasmWeatherProvider struct {
	*wasmPluginBase
	cache *wasmPluginCache[weatherForecastBundle]
}

// pluginsWeatherDir follows the codebase's cacheFilePath(envKey, fallback)
// env-override-with-fallback idiom, mirroring pluginsTidesDir.
func pluginsWeatherDir() string {
	return cacheFilePath("PLUGINS_WEATHER_DIR", "plugins/weather")
}

// newWasmWeatherProvider compiles the .wasm file at path and validates its
// contract at discovery time (id()/name() must resolve and be callable). It
// deliberately does NOT call fetch_forecast here - that needs live network
// access, which has no place at container boot.
func newWasmWeatherProvider(path string) (*wasmWeatherProvider, error) {
	manifest, err := manifestForWasmPlugin(path)
	if err != nil {
		return nil, err
	}
	return newWasmWeatherProviderWithManifest(manifest)
}

// newWasmWeatherProviderWithManifest is split out from newWasmWeatherProvider
// so tests can construct a provider against a fully custom manifest, mirroring
// newWasmTideProviderWithManifest.
func newWasmWeatherProviderWithManifest(manifest extism.Manifest) (*wasmWeatherProvider, error) {
	base, err := newWasmPluginBase(manifest, "plugins/weather")
	if err != nil {
		return nil, err
	}
	return newWasmWeatherProviderFromBase(base), nil
}

// newWasmWeatherProviderFromBase wraps an already-validated base with a
// weather-specific disk-backed cache (loaded from disk immediately),
// mirroring newWasmTideProviderFromBase.
func newWasmWeatherProviderFromBase(base *wasmPluginBase) *wasmWeatherProvider {
	cacheFile := cacheFilePath(
		"WEATHER_WASM_CACHE_FILE_"+strings.ToUpper(strings.ReplaceAll(base.ID(), "-", "_")),
		fmt.Sprintf("cache/weather_wasm_%s_cache.json", base.ID()),
	)
	cache := newWasmPluginCache[weatherForecastBundle](cacheFile)
	cache.loadFromDisk()

	return &wasmWeatherProvider{wasmPluginBase: base, cache: cache}
}

// wasmFetchForecastInput mirrors the guest's fetch_forecast input contract.
type wasmFetchForecastInput struct {
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	Days int     `json:"days"`
}

// wasmWeatherCurrentOutput/wasmWeatherDayOutput/wasmWeatherHourOutput/
// wasmFetchForecastOutput mirror the guest's fetch_forecast output contract
// exactly (snake_case field names) - see weather_providers.go's top doc
// comment for the full JSON shape.
type wasmWeatherCurrentOutput struct {
	Time                   string  `json:"time"`
	TemperatureC           float64 `json:"temperature_c"`
	Condition              string  `json:"condition"`
	WindSpeedMS            float64 `json:"wind_speed_ms"`
	WindGustMS             float64 `json:"wind_gust_ms"`
	WindDirectionDeg       float64 `json:"wind_direction_deg"`
	PrecipitationChancePct float64 `json:"precipitation_chance_pct"`
}

type wasmWeatherDayOutput struct {
	Start                  string  `json:"start"`
	Condition              string  `json:"condition"`
	TempMaxC               float64 `json:"temp_max_c"`
	TempMinC               float64 `json:"temp_min_c"`
	WindSpeedMS            float64 `json:"wind_speed_ms"`
	WindGustMS             float64 `json:"wind_gust_ms"`
	WindDirectionDeg       float64 `json:"wind_direction_deg"`
	PrecipitationChancePct float64 `json:"precipitation_chance_pct"`
	Sunrise                string  `json:"sunrise"`
	Sunset                 string  `json:"sunset"`
}

type wasmWeatherHourOutput struct {
	Time                   string  `json:"time"`
	TemperatureC           float64 `json:"temperature_c"`
	Condition              string  `json:"condition"`
	WindSpeedMS            float64 `json:"wind_speed_ms"`
	WindGustMS             float64 `json:"wind_gust_ms"`
	WindDirectionDeg       float64 `json:"wind_direction_deg"`
	PrecipitationChancePct float64 `json:"precipitation_chance_pct"`
	PrecipitationMM        float64 `json:"precipitation_mm"`
	UVIndex                float64 `json:"uv_index"`
	IsDaylight             bool    `json:"is_daylight"`
}

type wasmFetchForecastOutput struct {
	Current wasmWeatherCurrentOutput `json:"current"`
	Days    []wasmWeatherDayOutput   `json:"days"`
	Hourly  []wasmWeatherHourOutput  `json:"hourly"`
}

// parseRequiredTime parses an RFC3339 timestamp that the guest contract
// says must always be present (current.time, days[].start, hourly[].time).
// An empty or unparseable value is a hard error - never silently zeroed -
// since that would mask a real plugin bug (fail-fast policy).
func parseRequiredTime(field, value string) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, fmt.Errorf("missing required %s", field)
	}
	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return time.Time{}, fmt.Errorf("unparseable %s %q: %w", field, value, err)
	}
	return parsed, nil
}

// parseOptionalTime parses an RFC3339 timestamp that the guest contract
// allows to be omitted (days[].sunrise/sunset) when genuinely unavailable
// (e.g. polar day/night). An empty value returns the zero time.Time with no
// error; a non-empty-but-unparseable value IS an error, same fail-fast
// reasoning as parseRequiredTime.
func parseOptionalTime(field, value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("unparseable %s %q: %w", field, value, err)
	}
	return parsed, nil
}

func mapWasmFetchForecastOutput(out wasmFetchForecastOutput) (weatherForecastBundle, error) {
	var bundle weatherForecastBundle

	currentTime, err := parseRequiredTime("current.time", out.Current.Time)
	if err != nil {
		return weatherForecastBundle{}, err
	}
	bundle.Current = weatherCurrentPoint{
		Time:                   currentTime,
		TemperatureC:           out.Current.TemperatureC,
		Condition:              out.Current.Condition,
		WindSpeedMS:            out.Current.WindSpeedMS,
		WindGustMS:             out.Current.WindGustMS,
		WindDirectionDeg:       out.Current.WindDirectionDeg,
		PrecipitationChancePct: out.Current.PrecipitationChancePct,
	}

	bundle.Days = make([]weatherDayPoint, 0, len(out.Days))
	for i, d := range out.Days {
		start, err := parseRequiredTime(fmt.Sprintf("days[%d].start", i), d.Start)
		if err != nil {
			return weatherForecastBundle{}, err
		}
		sunrise, err := parseOptionalTime(fmt.Sprintf("days[%d].sunrise", i), d.Sunrise)
		if err != nil {
			return weatherForecastBundle{}, err
		}
		sunset, err := parseOptionalTime(fmt.Sprintf("days[%d].sunset", i), d.Sunset)
		if err != nil {
			return weatherForecastBundle{}, err
		}
		bundle.Days = append(bundle.Days, weatherDayPoint{
			Start:                  start,
			Condition:              d.Condition,
			TempMaxC:               d.TempMaxC,
			TempMinC:               d.TempMinC,
			WindSpeedMS:            d.WindSpeedMS,
			WindGustMS:             d.WindGustMS,
			WindDirectionDeg:       d.WindDirectionDeg,
			PrecipitationChancePct: d.PrecipitationChancePct,
			Sunrise:                sunrise,
			Sunset:                 sunset,
		})
	}

	bundle.Hourly = make([]weatherHourPoint, 0, len(out.Hourly))
	for i, h := range out.Hourly {
		hourTime, err := parseRequiredTime(fmt.Sprintf("hourly[%d].time", i), h.Time)
		if err != nil {
			return weatherForecastBundle{}, err
		}
		bundle.Hourly = append(bundle.Hourly, weatherHourPoint{
			Time:                   hourTime,
			TemperatureC:           h.TemperatureC,
			Condition:              h.Condition,
			WindSpeedMS:            h.WindSpeedMS,
			WindGustMS:             h.WindGustMS,
			WindDirectionDeg:       h.WindDirectionDeg,
			PrecipitationChancePct: h.PrecipitationChancePct,
			PrecipitationMM:        math.Max(0, h.PrecipitationMM),
			UVIndex:                math.Max(0, h.UVIndex),
			IsDaylight:             h.IsDaylight,
		})
	}

	return bundle, nil
}

// weatherWasmCacheKey rounds lat/lon to 1 decimal place (same rounding the
// pre-Phase-3 weatherToday cache used) and folds in days, so a
// weatherToday(days=1) call and a weatherForecast(days=10) call at the same
// position never share - and silently truncate - each other's cached bundle.
func weatherWasmCacheKey(lat, lon float64, days int) string {
	roundedLat := math.Round(lat*10) / 10
	roundedLon := math.Round(lon*10) / 10
	return fmt.Sprintf("%.1f,%.1f,%d", roundedLat, roundedLon, days)
}

// FetchForecast calls the guest's fetch_forecast, unmarshals+maps the raw
// JSON into a typed weatherForecastBundle, and applies the same TTL cache +
// stale-on-error fallback pattern as wasmTideProvider.FetchTideChart.
func (p *wasmWeatherProvider) FetchForecast(lat, lon float64, days int) (bundle weatherForecastBundle, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("plugin %q panicked in fetch_forecast: %v", p.id, r)
			bundle = weatherForecastBundle{}
		}
	}()

	key := weatherWasmCacheKey(lat, lon, days)

	if cached, ok := p.cache.get(key, p.ttlDuration()); ok {
		cached.Cached = true
		return cached, nil
	}

	fetched, ferr := p.fetchFromPlugin(lat, lon, days)
	if ferr != nil {
		if stale, ok := p.cache.getStale(key); ok {
			stale.Cached = true
			return stale, nil
		}
		return weatherForecastBundle{}, ferr
	}

	fetched.CachedAt = time.Now().UTC()
	fetched.Cached = false
	p.cache.set(key, fetched)

	return fetched, nil
}

func (p *wasmWeatherProvider) fetchFromPlugin(lat, lon float64, days int) (weatherForecastBundle, error) {
	input, err := json.Marshal(wasmFetchForecastInput{Lat: lat, Lon: lon, Days: days})
	if err != nil {
		return weatherForecastBundle{}, fmt.Errorf("plugin %q: failed to marshal fetch_forecast input: %w", p.id, err)
	}

	out, err := p.call("fetch_forecast", input)
	if err != nil {
		return weatherForecastBundle{}, err
	}

	var parsed wasmFetchForecastOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return weatherForecastBundle{}, fmt.Errorf("plugin %q: unparseable fetch_forecast JSON: %w", p.id, err)
	}

	return mapWasmFetchForecastOutput(parsed)
}

// loadWasmWeatherProviders scans dir once at startup for .wasm plugins, via
// the shared loadWasmPluginsFromDir. Mirrors loadWasmTideProviders - a file
// that fails to load as a valid plugin is logged and skipped, discovery
// continues for the rest.
func loadWasmWeatherProviders(dir string) {
	loadWasmPluginsFromDir(dir, "plugins/weather",
		func(id string) bool {
			_, ok := getWeatherProvider(id)
			return ok
		},
		func(base *wasmPluginBase, path string) error {
			provider := newWasmWeatherProviderFromBase(base)
			registerWeatherProvider(provider)
			log.Printf("plugins/weather: registered plugin %q (%s) from %s", provider.ID(), provider.Name(), filepath.Base(path))
			return nil
		},
	)
}
