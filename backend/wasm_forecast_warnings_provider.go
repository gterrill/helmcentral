// Package main: WASM forecast-warnings provider adapter.
//
// NOTE on this file's name: like wasm_tide_provider.go and
// wasm_wave_provider.go, it is deliberately NOT named anything ending in
// "_wasm.go" - see wasm_tide_provider.go's header comment for the full
// explanation of why (implicit GOARCH=wasm build constraint).
//
// This is the thin forecast-warnings-specific layer on top of the shared
// WASM host machinery in wasm_plugin.go: wasmForecastWarningsProvider
// embeds *wasmPluginBase (ID/Name/TTLSeconds/ttlDuration/call all come from
// there) and adds a wasmPluginCache[forecastWarningsBundle] plus the
// forecastWarningsProvider interface method. See
// forecast_warnings_providers.go's top doc comment for the guest contract
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

// wasmForecastWarningsProvider is a forecastWarningsProvider backed by a
// sandboxed WASM guest module, loaded via extism/go-sdk - same pattern as
// wasmWaveProvider.
type wasmForecastWarningsProvider struct {
	*wasmPluginBase
	cache *wasmPluginCache[forecastWarningsBundle]
}

// pluginsForecastWarningsDir follows the codebase's
// cacheFilePath(envKey, fallback) env-override-with-fallback idiom,
// mirroring pluginsWavesDir.
func pluginsForecastWarningsDir() string {
	return cacheFilePath("PLUGINS_FORECAST_WARNINGS_DIR", "plugins/forecast-warnings")
}

// newWasmForecastWarningsProvider compiles the .wasm file at path and
// validates its contract at discovery time (id()/name() must resolve and be
// callable). It deliberately does NOT call fetch_warnings here - that needs
// live network access, which has no place at container boot.
func newWasmForecastWarningsProvider(path string) (*wasmForecastWarningsProvider, error) {
	manifest, err := manifestForWasmPlugin(path)
	if err != nil {
		return nil, err
	}
	return newWasmForecastWarningsProviderWithManifest(manifest)
}

// newWasmForecastWarningsProviderWithManifest is split out from
// newWasmForecastWarningsProvider so tests can construct a provider against
// a fully custom manifest, mirroring newWasmWaveProviderWithManifest.
func newWasmForecastWarningsProviderWithManifest(manifest extism.Manifest) (*wasmForecastWarningsProvider, error) {
	base, err := newWasmPluginBase(manifest, "plugins/forecast-warnings")
	if err != nil {
		return nil, err
	}
	return newWasmForecastWarningsProviderFromBase(base), nil
}

// newWasmForecastWarningsProviderFromBase wraps an already-validated base
// with a forecast-warnings-specific disk-backed cache (loaded from disk
// immediately), mirroring newWasmWaveProviderFromBase.
func newWasmForecastWarningsProviderFromBase(base *wasmPluginBase) *wasmForecastWarningsProvider {
	cacheFile := cacheFilePath(
		"FORECAST_WARNINGS_WASM_CACHE_FILE_"+strings.ToUpper(strings.ReplaceAll(base.ID(), "-", "_")),
		fmt.Sprintf("cache/forecast_warnings_wasm_%s_cache.json", base.ID()),
	)
	cache := newWasmPluginCache[forecastWarningsBundle](cacheFile)
	cache.loadFromDisk()

	return &wasmForecastWarningsProvider{wasmPluginBase: base, cache: cache}
}

// wasmFetchWarningsInput mirrors the guest's fetch_warnings input contract.
type wasmFetchWarningsInput struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// wasmForecastWarningSectionOutput/wasmForecastWarningBulletinOutput/
// wasmFetchWarningsOutput mirror the guest's fetch_warnings output contract
// exactly (snake_case field names) - see forecast_warnings_providers.go's
// top doc comment for the full JSON shape.
//
// IssuedAt is a string, not a time.Time, since the contract allows it to be
// empty/omitted when the plugin genuinely doesn't know it - parsed leniently
// via parseOptionalTime (empty -> zero time.Time, non-empty-but-unparseable
// -> hard error), the same helper wasm_weather_provider.go's
// mapWasmFetchForecastOutput uses for sunrise/sunset.
type wasmForecastWarningSectionOutput struct {
	Day         string `json:"day"`
	WarningType string `json:"warning_type"`
}

type wasmForecastWarningBulletinOutput struct {
	ID         string                             `json:"id"`
	Title      string                             `json:"title"`
	Category   string                             `json:"category"`
	IssuedAt   string                             `json:"issued_at"`
	DetailsURL string                             `json:"details_url"`
	Sections   []wasmForecastWarningSectionOutput `json:"sections"`
}

type wasmFetchWarningsOutput struct {
	Region    string                              `json:"region"`
	Bulletins []wasmForecastWarningBulletinOutput `json:"bulletins"`
}

func mapWasmFetchWarningsOutput(out wasmFetchWarningsOutput) (forecastWarningsBundle, error) {
	var bundle forecastWarningsBundle
	bundle.Region = out.Region

	bundle.Bulletins = make([]forecastWarningBulletin, 0, len(out.Bulletins))
	for i, b := range out.Bulletins {
		issuedAt, err := parseOptionalTime(fmt.Sprintf("bulletins[%d].issued_at", i), b.IssuedAt)
		if err != nil {
			return forecastWarningsBundle{}, err
		}

		sections := make([]forecastWarningSection, 0, len(b.Sections))
		for _, s := range b.Sections {
			sections = append(sections, forecastWarningSection{Day: s.Day, WarningType: s.WarningType})
		}

		bundle.Bulletins = append(bundle.Bulletins, forecastWarningBulletin{
			ID:         b.ID,
			Title:      b.Title,
			Category:   b.Category,
			IssuedAt:   issuedAt,
			DetailsURL: b.DetailsURL,
			Sections:   sections,
		})
	}

	return bundle, nil
}

// forecastWarningsWasmCacheKey rounds lat/lon to 1 decimal place (same
// rounding the wave/weather adapters use), matching the wave-provider
// cache-key convention. Unlike wave/weather, fetch_warnings takes no
// separate "days" parameter, so there is nothing else to fold in.
func forecastWarningsWasmCacheKey(lat, lon float64) string {
	roundedLat := math.Round(lat*10) / 10
	roundedLon := math.Round(lon*10) / 10
	return fmt.Sprintf("%.1f,%.1f", roundedLat, roundedLon)
}

// FetchWarnings calls the guest's fetch_warnings, unmarshals+maps the raw
// JSON into a typed forecastWarningsBundle, and applies the same TTL cache +
// stale-on-error fallback pattern as wasmWaveProvider.FetchWaves.
func (p *wasmForecastWarningsProvider) FetchWarnings(lat, lon float64) (bundle forecastWarningsBundle, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("plugin %q panicked in fetch_warnings: %v", p.id, r)
			bundle = forecastWarningsBundle{}
		}
	}()

	key := forecastWarningsWasmCacheKey(lat, lon)

	if cached, ok := p.cache.get(key, p.ttlDuration()); ok {
		cached.Cached = true
		return cached, nil
	}

	fetched, ferr := p.fetchFromPlugin(lat, lon)
	if ferr != nil {
		if stale, ok := p.cache.getStale(key); ok {
			stale.Cached = true
			return stale, nil
		}
		return forecastWarningsBundle{}, ferr
	}

	fetched.CachedAt = time.Now().UTC()
	fetched.Cached = false
	p.cache.set(key, fetched)

	return fetched, nil
}

func (p *wasmForecastWarningsProvider) fetchFromPlugin(lat, lon float64) (forecastWarningsBundle, error) {
	input, err := json.Marshal(wasmFetchWarningsInput{Lat: lat, Lon: lon})
	if err != nil {
		return forecastWarningsBundle{}, fmt.Errorf("plugin %q: failed to marshal fetch_warnings input: %w", p.id, err)
	}

	out, err := p.call("fetch_warnings", input)
	if err != nil {
		return forecastWarningsBundle{}, err
	}

	var parsed wasmFetchWarningsOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return forecastWarningsBundle{}, fmt.Errorf("plugin %q: unparseable fetch_warnings JSON: %w", p.id, err)
	}

	return mapWasmFetchWarningsOutput(parsed)
}

// loadWasmForecastWarningsProviders scans dir once at startup for .wasm
// plugins, via the shared loadWasmPluginsFromDir. Mirrors
// loadWasmWaveProviders - a file that fails to load as a valid plugin is
// logged and skipped, discovery continues for the rest.
func loadWasmForecastWarningsProviders(dir string) {
	loadWasmPluginsFromDir(dir, "plugins/forecast-warnings",
		func(id string) bool {
			_, ok := getForecastWarningsProvider(id)
			return ok
		},
		func(base *wasmPluginBase, path string) error {
			provider := newWasmForecastWarningsProviderFromBase(base)
			registerForecastWarningsProvider(provider)
			log.Printf("plugins/forecast-warnings: registered plugin %q (%s) from %s", provider.ID(), provider.Name(), filepath.Base(path))
			return nil
		},
	)
}
