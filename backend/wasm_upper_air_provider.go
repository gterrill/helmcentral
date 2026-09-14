// Package main: WASM upper-air provider adapter.
//
// NOTE on this file's name: like wasm_tide_provider.go and
// wasm_wave_provider.go, it is deliberately NOT named anything ending in
// "_wasm.go" - see wasm_tide_provider.go's header comment for why (implicit
// GOARCH=wasm build constraint).
//
// The thin upper-air layer on the shared WASM host machinery in
// wasm_plugin.go, mirroring wasmWaveProvider exactly. See
// upper_air_providers.go's top doc comment for the guest contract.
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

type wasmUpperAirProvider struct {
	*wasmPluginBase
	cache *wasmPluginCache[upperAirBundle]
}

func pluginsUpperAirDir() string {
	return cacheFilePath("PLUGINS_UPPER_AIR_DIR", "plugins/upper-air")
}

func newWasmUpperAirProvider(path string) (*wasmUpperAirProvider, error) {
	manifest, err := manifestForWasmPlugin(path)
	if err != nil {
		return nil, err
	}
	return newWasmUpperAirProviderWithManifest(manifest)
}

func newWasmUpperAirProviderWithManifest(manifest extism.Manifest) (*wasmUpperAirProvider, error) {
	base, err := newWasmPluginBase(manifest, "plugins/upper-air")
	if err != nil {
		return nil, err
	}
	return newWasmUpperAirProviderFromBase(base), nil
}

func newWasmUpperAirProviderFromBase(base *wasmPluginBase) *wasmUpperAirProvider {
	cacheFile := cacheFilePath(
		"UPPER_AIR_WASM_CACHE_FILE_"+strings.ToUpper(strings.ReplaceAll(base.ID(), "-", "_")),
		fmt.Sprintf("cache/upper_air_wasm_%s_cache.json", base.ID()),
	)
	cache := newWasmPluginCache[upperAirBundle](cacheFile)
	cache.loadFromDisk()

	return &wasmUpperAirProvider{wasmPluginBase: base, cache: cache}
}

type wasmFetchUpperAirInput struct {
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	Days int     `json:"days"`
}

// wasmUpperAirHourOutput mirrors the guest's fetch_upper_air output contract.
//
// Every value is a bare float64 rather than a pointer: a zero geopotential
// height is not a reading of anything, so zero is an unambiguous marker for
// "absent" here in a way it would not be for, say, a temperature.
type wasmUpperAirHourOutput struct {
	Time                    string  `json:"time"`
	GeopotentialHeight500M  float64 `json:"geopotential_height_500_m"`
	GeopotentialHeight1000M float64 `json:"geopotential_height_1000_m"`
	WindSpeed500MS          float64 `json:"wind_speed_500_ms"`
	Temperature500C         float64 `json:"temperature_500_c"`
}

type wasmFetchUpperAirOutput struct {
	Hourly []wasmUpperAirHourOutput `json:"hourly"`
}

func mapWasmFetchUpperAirOutput(out wasmFetchUpperAirOutput) (upperAirBundle, error) {
	var bundle upperAirBundle

	bundle.Hourly = make([]upperAirHourPoint, 0, len(out.Hourly))
	for i, h := range out.Hourly {
		hourTime, err := parseRequiredTime(fmt.Sprintf("hourly[%d].time", i), h.Time)
		if err != nil {
			return upperAirBundle{}, err
		}
		bundle.Hourly = append(bundle.Hourly, upperAirHourPoint{
			Time:                    hourTime,
			GeopotentialHeight500M:  h.GeopotentialHeight500M,
			GeopotentialHeight1000M: h.GeopotentialHeight1000M,
			WindSpeed500MS:          h.WindSpeed500MS,
			Temperature500C:         h.Temperature500C,
		})
	}

	return bundle, nil
}

// upperAirWasmCacheKey follows waveWasmCacheKey: rounded position plus days,
// with no timezone dimension since fetch_upper_air has no daily rollup.
func upperAirWasmCacheKey(lat, lon float64, days int) string {
	return weatherWasmCacheKey(lat, lon, days, "")
}

func (p *wasmUpperAirProvider) FetchUpperAir(lat, lon float64, days int) (bundle upperAirBundle, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("plugin %q panicked in fetch_upper_air: %v", p.id, r)
			bundle = upperAirBundle{}
		}
	}()

	key := upperAirWasmCacheKey(lat, lon, days)

	if cached, ok := p.cache.get(key, p.ttlDuration()); ok {
		cached.Cached = true
		return cached, nil
	}

	fetched, ferr := p.cache.singleflightFetch(key, func() (upperAirBundle, error) {
		return p.fetchFromPlugin(lat, lon, days)
	})
	if ferr != nil {
		if stale, ok := p.cache.getStale(key); ok {
			stale.Cached = true
			return stale, nil
		}
		return upperAirBundle{}, ferr
	}

	fetched.CachedAt = time.Now().UTC()
	fetched.Cached = false
	p.cache.set(key, fetched)

	return fetched, nil
}

func (p *wasmUpperAirProvider) fetchFromPlugin(lat, lon float64, days int) (upperAirBundle, error) {
	input, err := json.Marshal(wasmFetchUpperAirInput{Lat: lat, Lon: lon, Days: days})
	if err != nil {
		return upperAirBundle{}, fmt.Errorf("plugin %q: failed to marshal fetch_upper_air input: %w", p.id, err)
	}

	out, err := p.call("fetch_upper_air", input)
	if err != nil {
		return upperAirBundle{}, err
	}

	var parsed wasmFetchUpperAirOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return upperAirBundle{}, fmt.Errorf("plugin %q: unparseable fetch_upper_air JSON: %w", p.id, err)
	}

	return mapWasmFetchUpperAirOutput(parsed)
}

func loadWasmUpperAirProviders(dir string) {
	loadWasmPluginsFromDir(dir, "plugins/upper-air",
		func(id string) bool {
			_, ok := getUpperAirProvider(id)
			return ok
		},
		func(base *wasmPluginBase, path string) error {
			provider := newWasmUpperAirProviderFromBase(base)
			registerUpperAirProvider(provider)
			log.Printf("plugins/upper-air: registered plugin %q (%s) from %s", provider.ID(), provider.Name(), filepath.Base(path))
			return nil
		},
	)
}
