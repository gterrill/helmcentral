// Package main: WASM tide provider adapter.
//
// NOTE on this file's name: it is deliberately "wasm_tide_provider.go", not
// "tide_provider_wasm.go" (which would otherwise match this repo's
// tide_providers.go naming convention).
// A source file whose name ends in "_wasm.go" (or "_wasm_test.go" - the Go
// toolchain strips a trailing "_test" before checking) is treated by the Go
// toolchain as implicitly constrained to GOARCH=wasm, per the filename-based
// build-constraint rules in `go help buildconstraint`. Such a file is
// silently excluded from every non-wasm build (confirmed empirically: it
// showed up as go.list's IgnoredGoFiles and produced "undefined:" errors at
// the call sites instead of any error in the file itself). Keep "wasm" out
// of the trailing position of this file's name.
//
// The generic WASM plugin host machinery (contract validation, per-call
// instance creation, allowed-hosts/config companion files, the disk-backed
// TTL cache, and directory scanning) lives in wasm_plugin.go and is shared
// with the weather/wave WASM adapters. This file is the thin tide-specific
// layer on top: wasmTideProvider embeds *wasmPluginBase (so ID/Name/
// TTLSeconds/ttlDuration/call all come from there) and adds a
// wasmPluginCache[tideChartResult] plus the tideProvider interface methods.
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

// wasmTideProvider is a tideProvider backed by a sandboxed WASM guest
// module, loaded via extism/go-sdk (wazero underneath, pure Go, no cgo).
type wasmTideProvider struct {
	*wasmPluginBase
	cache *wasmPluginCache[tideChartResult]
}

// pluginsTidesDir follows this codebase's existing cacheFilePath(envKey,
// fallback) idiom (see weather_tide.go) for env-override-with-fallback
// directory resolution.
func pluginsTidesDir() string {
	return cacheFilePath("PLUGINS_TIDES_DIR", "plugins/tides")
}

// newWasmTideProvider compiles the .wasm file at path and validates its
// contract at discovery time (id()/name() must resolve and be callable).
// It deliberately does NOT call search_stations/fetch_tide_chart here -
// those need live network access, which has no place at container boot.
func newWasmTideProvider(path string) (*wasmTideProvider, error) {
	manifest, err := manifestForWasmPlugin(path)
	if err != nil {
		return nil, err
	}
	return newWasmTideProviderWithManifest(manifest)
}

// newWasmTideProviderWithManifest is split out from newWasmTideProvider so
// tests can construct a provider against a fully custom manifest (e.g. a
// short Timeout, or explicit AllowedHosts) without needing a real companion
// allowed-hosts file on disk or the WASM_PLUGIN_TIMEOUT_MS env var.
func newWasmTideProviderWithManifest(manifest extism.Manifest) (*wasmTideProvider, error) {
	base, err := newWasmPluginBase(manifest, "plugins/tides")
	if err != nil {
		return nil, err
	}
	return newWasmTideProviderFromBase(base), nil
}

// newWasmTideProviderFromBase wraps an already-validated base with a
// tide-specific disk-backed cache (loaded from disk immediately, mirroring
// the pre-refactor constructor's behavior). Shared by
// newWasmTideProviderWithManifest and loadWasmTideProviders's register
// callback below so the cache-file-path convention lives in exactly one
// place.
func newWasmTideProviderFromBase(base *wasmPluginBase) *wasmTideProvider {
	cacheFile := cacheFilePath(
		"TIDE_WASM_CACHE_FILE_"+strings.ToUpper(strings.ReplaceAll(base.ID(), "-", "_")),
		fmt.Sprintf("cache/tide_wasm_%s_cache.json", base.ID()),
	)
	cache := newWasmPluginCache[tideChartResult](cacheFile)
	cache.loadFromDisk()

	return &wasmTideProvider{wasmPluginBase: base, cache: cache}
}

type wasmSearchStationsInput struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

// SearchStations has no error return on the tideProvider interface
// (pre-existing constraint). On any failure it logs loudly and returns an
// empty slice - it does NOT follow FetchTideChart's error-returning pattern
// because it structurally cannot.
func (p *wasmTideProvider) SearchStations(query string, limit int) (stations []tideStation) {
	stations = []tideStation{}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("plugins/tides: plugin %q panicked in search_stations: %v", p.id, r)
			stations = []tideStation{}
		}
	}()

	input, err := json.Marshal(wasmSearchStationsInput{Query: query, Limit: limit})
	if err != nil {
		log.Printf("plugins/tides: plugin %q failed to marshal search_stations input: %v", p.id, err)
		return stations
	}

	out, err := p.call("search_stations", input)
	if err != nil {
		log.Printf("plugins/tides: plugin %q search_stations call failed: %v", p.id, err)
		return stations
	}

	var result []tideStation
	if err := json.Unmarshal(out, &result); err != nil {
		log.Printf("plugins/tides: plugin %q returned unparseable search_stations JSON: %v", p.id, err)
		return stations
	}

	return result
}

type wasmFetchTideChartOutput struct {
	Station  tideStation        `json:"station"`
	Extremes []tideExtremePoint `json:"extremes"`
}

// FetchTideChart calls the guest's fetch_tide_chart, unmarshals the raw
// station+extremes JSON, then fills CurrentHeightM/Direction via the shared
// interpolateTideNow - the same math every other provider uses, never
// reimplemented per-plugin. Unlike SearchStations, this DOES return an
// error (per the interface), so failures are surfaced rather than swallowed
// into an empty result - except when a stale cache entry lets us degrade
// gracefully instead, the same behavior the formerly-native BOM tide
// provider had before it was ported to WASM (docs/examples/tide-plugins/bom).
func (p *wasmTideProvider) FetchTideChart(stationID string) (result tideChartResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("plugin %q panicked in fetch_tide_chart: %v", p.id, r)
			result = tideChartResult{}
		}
	}()

	if cached, ok := p.cache.get(stationID, p.ttlDuration()); ok {
		cached.Cached = true
		cached.CurrentHeightM, cached.Direction = interpolateTideNow(cached.Extremes, time.Now().UTC())
		return cached, nil
	}

	fetched, ferr := p.fetchFromPlugin(stationID)
	if ferr != nil {
		if stale, ok := p.cache.getStale(stationID); ok {
			stale.Cached = true
			stale.CurrentHeightM, stale.Direction = interpolateTideNow(stale.Extremes, time.Now().UTC())
			return stale, nil
		}
		return tideChartResult{}, ferr
	}

	fetched.CachedAt = time.Now().UTC()
	fetched.Cached = false
	p.cache.set(stationID, fetched)

	return fetched, nil
}

func (p *wasmTideProvider) fetchFromPlugin(stationID string) (tideChartResult, error) {
	out, err := p.call("fetch_tide_chart", []byte(stationID))
	if err != nil {
		return tideChartResult{}, err
	}

	var parsed wasmFetchTideChartOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return tideChartResult{}, fmt.Errorf("plugin %q: unparseable fetch_tide_chart JSON: %w", p.id, err)
	}

	now := time.Now().UTC()
	heightM, direction := interpolateTideNow(parsed.Extremes, now)

	return tideChartResult{
		Station:        parsed.Station,
		Extremes:       parsed.Extremes,
		CurrentHeightM: heightM,
		Direction:      direction,
	}, nil
}

// loadWasmTideProviders scans dir once at startup for .wasm plugins, via the
// shared loadWasmPluginsFromDir. A file that fails to load as a valid plugin
// is logged and skipped - discovery continues for the remaining files,
// mirroring sat_charts.go's listSatChartsHandler "skip corrupt, keep going"
// idiom. Tides are WASM-plugin-only - there is no native provider to shadow,
// so first-registered-wins here only matters between plugins themselves.
func loadWasmTideProviders(dir string) {
	loadWasmPluginsFromDir(dir, "plugins/tides",
		func(id string) bool {
			_, ok := getTideProvider(id)
			return ok
		},
		func(base *wasmPluginBase, path string) error {
			provider := newWasmTideProviderFromBase(base)
			registerTideProvider(provider)
			log.Printf("plugins/tides: registered plugin %q (%s) from %s", provider.ID(), provider.Name(), filepath.Base(path))
			return nil
		},
	)
}
