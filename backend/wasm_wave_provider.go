// Package main: WASM wave provider adapter.
//
// NOTE on this file's name: like wasm_tide_provider.go and
// wasm_weather_provider.go, it is deliberately NOT named anything ending in
// "_wasm.go" - see wasm_tide_provider.go's header comment for the full
// explanation of why (implicit GOARCH=wasm build constraint).
//
// This is the thin wave-specific layer on top of the shared WASM host
// machinery in wasm_plugin.go: wasmWaveProvider embeds *wasmPluginBase
// (ID/Name/TTLSeconds/ttlDuration/call all come from there) and adds a
// wasmPluginCache[waveForecastBundle] plus the waveProvider interface
// method. See wave_providers.go's top doc comment for the guest contract
// this adapter maps JSON against.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	extism "github.com/extism/go-sdk"
)

// wasmWaveProvider is a waveProvider backed by a sandboxed WASM guest
// module, loaded via extism/go-sdk - same pattern as wasmWeatherProvider.
type wasmWaveProvider struct {
	*wasmPluginBase
	cache *wasmPluginCache[waveForecastBundle]
}

// pluginsWavesDir follows the codebase's cacheFilePath(envKey, fallback)
// env-override-with-fallback idiom, mirroring pluginsWeatherDir.
func pluginsWavesDir() string {
	return cacheFilePath("PLUGINS_WAVES_DIR", "plugins/waves")
}

// newWasmWaveProvider compiles the .wasm file at path and validates its
// contract at discovery time (id()/name() must resolve and be callable). It
// deliberately does NOT call fetch_waves here - that needs live network
// access, which has no place at container boot.
func newWasmWaveProvider(path string) (*wasmWaveProvider, error) {
	manifest, err := manifestForWasmPlugin(path)
	if err != nil {
		return nil, err
	}
	return newWasmWaveProviderWithManifest(manifest)
}

// newWasmWaveProviderWithManifest is split out from newWasmWaveProvider so
// tests can construct a provider against a fully custom manifest, mirroring
// newWasmWeatherProviderWithManifest.
func newWasmWaveProviderWithManifest(manifest extism.Manifest) (*wasmWaveProvider, error) {
	base, err := newWasmPluginBase(manifest, "plugins/waves")
	if err != nil {
		return nil, err
	}
	return newWasmWaveProviderFromBase(base), nil
}

// newWasmWaveProviderFromBase wraps an already-validated base with a
// wave-specific disk-backed cache (loaded from disk immediately), mirroring
// newWasmWeatherProviderFromBase.
func newWasmWaveProviderFromBase(base *wasmPluginBase) *wasmWaveProvider {
	cacheFile := cacheFilePath(
		"WAVE_WASM_CACHE_FILE_"+strings.ToUpper(strings.ReplaceAll(base.ID(), "-", "_")),
		fmt.Sprintf("cache/wave_wasm_%s_cache.json", base.ID()),
	)
	cache := newWasmPluginCache[waveForecastBundle](cacheFile)
	cache.loadFromDisk()

	return &wasmWaveProvider{wasmPluginBase: base, cache: cache}
}

// wasmFetchWavesInput mirrors the guest's fetch_waves input contract.
type wasmFetchWavesInput struct {
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	Days int     `json:"days"`
}

// wasmWaveHourOutput/wasmFetchWavesOutput mirror the guest's fetch_waves
// output contract exactly (snake_case field names) - see
// wave_providers.go's top doc comment for the full JSON shape.
//
// SeaSurfaceTemperatureC is a *float64 so a JSON payload that omits the key
// entirely (the contract's "genuinely no data here" case) naturally
// unmarshals to nil - no special detection needed.
type wasmWaveHourOutput struct {
	Time             string  `json:"time"`
	WaveHeightM      float64 `json:"wave_height_m"`
	WavePeriodS      float64 `json:"wave_period_s"`
	WaveDirectionDeg float64 `json:"wave_direction_deg"`
	WindWaveHeightM  float64 `json:"wind_wave_height_m"`
	SwellWaveHeightM float64 `json:"swell_wave_height_m"`
}

type wasmFetchWavesOutput struct {
	Hourly                 []wasmWaveHourOutput `json:"hourly"`
	SeaSurfaceTemperatureC *float64             `json:"sea_surface_temperature_c"`
}

func mapWasmFetchWavesOutput(out wasmFetchWavesOutput) (waveForecastBundle, error) {
	var bundle waveForecastBundle

	bundle.Hourly = make([]waveHourPoint, 0, len(out.Hourly))
	for i, h := range out.Hourly {
		hourTime, err := parseRequiredTime(fmt.Sprintf("hourly[%d].time", i), h.Time)
		if err != nil {
			return waveForecastBundle{}, err
		}
		bundle.Hourly = append(bundle.Hourly, waveHourPoint{
			Time:             hourTime,
			WaveHeightM:      h.WaveHeightM,
			WavePeriodS:      h.WavePeriodS,
			WaveDirectionDeg: h.WaveDirectionDeg,
			WindWaveHeightM:  h.WindWaveHeightM,
			SwellWaveHeightM: h.SwellWaveHeightM,
		})
	}

	bundle.SeaTempC = out.SeaSurfaceTemperatureC

	return bundle, nil
}

// waveWasmCacheKey rounds lat/lon to 1 decimal place (same rounding the
// weather adapter's weatherWasmCacheKey uses) and folds in days, so a
// waveForecast(days=10) call at the same rounded position doesn't clobber a
// different days value's cache entry within the TTL window - same reasoning
// as weatherWasmCacheKey.
func waveWasmCacheKey(lat, lon float64, days int) string {
	return weatherWasmCacheKey(lat, lon, days)
}

// FetchWaves calls the guest's fetch_waves, unmarshals+maps the raw JSON
// into a typed waveForecastBundle, and applies the same TTL cache +
// stale-on-error fallback pattern as wasmWeatherProvider.FetchForecast.
func (p *wasmWaveProvider) FetchWaves(lat, lon float64, days int) (bundle waveForecastBundle, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("plugin %q panicked in fetch_waves: %v", p.id, r)
			bundle = waveForecastBundle{}
		}
	}()

	key := waveWasmCacheKey(lat, lon, days)

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
		return waveForecastBundle{}, ferr
	}

	fetched.CachedAt = time.Now().UTC()
	fetched.Cached = false
	p.cache.set(key, fetched)

	return fetched, nil
}

func (p *wasmWaveProvider) fetchFromPlugin(lat, lon float64, days int) (waveForecastBundle, error) {
	input, err := json.Marshal(wasmFetchWavesInput{Lat: lat, Lon: lon, Days: days})
	if err != nil {
		return waveForecastBundle{}, fmt.Errorf("plugin %q: failed to marshal fetch_waves input: %w", p.id, err)
	}

	out, err := p.call("fetch_waves", input)
	if err != nil {
		return waveForecastBundle{}, err
	}

	var parsed wasmFetchWavesOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return waveForecastBundle{}, fmt.Errorf("plugin %q: unparseable fetch_waves JSON: %w", p.id, err)
	}

	return mapWasmFetchWavesOutput(parsed)
}

// loadWasmWaveProviders scans dir once at startup for .wasm plugins, via the
// shared loadWasmPluginsFromDir. Mirrors loadWasmWeatherProviders - a file
// that fails to load as a valid plugin is logged and skipped, discovery
// continues for the rest.
func loadWasmWaveProviders(dir string) {
	loadWasmPluginsFromDir(dir, "plugins/waves",
		func(id string) bool {
			_, ok := getWaveProvider(id)
			return ok
		},
		func(base *wasmPluginBase, path string) error {
			provider := newWasmWaveProviderFromBase(base)
			registerWaveProvider(provider)
			log.Printf("plugins/waves: registered plugin %q (%s) from %s", provider.ID(), provider.Name(), filepath.Base(path))
			return nil
		},
	)
}
