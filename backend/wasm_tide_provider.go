// Package main: WASM tide provider adapter.
//
// NOTE on this file's name: it is deliberately "wasm_tide_provider.go", not
// "tide_provider_wasm.go" (which would otherwise match this repo's
// tide_provider_bom.go / tide_provider_stormglass.go naming convention).
// A source file whose name ends in "_wasm.go" (or "_wasm_test.go" - the Go
// toolchain strips a trailing "_test" before checking) is treated by the Go
// toolchain as implicitly constrained to GOARCH=wasm, per the filename-based
// build-constraint rules in `go help buildconstraint`. Such a file is
// silently excluded from every non-wasm build (confirmed empirically: it
// showed up as go.list's IgnoredGoFiles and produced "undefined:" errors at
// the call sites instead of any error in the file itself). Keep "wasm" out
// of the trailing position of this file's name.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	extism "github.com/extism/go-sdk"
	"github.com/tetratelabs/wazero"
)

// wasmModuleConfig gives every guest instance real wall-clock time via WASI.
// Without WithSysWalltime(), wazero's default WASI clock is a fixed fake time
// (observed: 2022-01-01T00:00:00Z) rather than the host's actual clock - fine
// for deterministic testing, but silently broken for any plugin (native BOM's
// WASM port, NOAA's reference plugin, ...) that computes a date range via
// time.Now(), since every such request ends up asking the upstream tide API
// for a fixed historical/invalid date instead of "today".
func wasmModuleConfig() wazero.ModuleConfig {
	return wazero.NewModuleConfig().WithSysWalltime()
}

const (
	defaultWasmPluginTimeoutMS  = 15000 // ms, overridable via WASM_PLUGIN_TIMEOUT_MS
	defaultWasmPluginTTLSeconds = 3600  // used when a plugin has no ttl_seconds() export
)

// wasmTideProvider is a tideProvider backed by a sandboxed WASM guest module,
// loaded via extism/go-sdk (wazero underneath, pure Go, no cgo). One
// extism.CompiledPlugin is compiled once per discovered .wasm file; a fresh
// extism.Plugin ("Instance") is created per call, since a Plugin is not safe
// for concurrent use but CompiledPlugin.Instance is Extism's documented
// pattern for concurrent-safe access.
type wasmTideProvider struct {
	id         string
	name       string
	ttlSeconds int64
	compiled   *extism.CompiledPlugin
	cache      *wasmTideCache
}

// wasmTideCache mirrors bomTideCache/bomTideCacheDisk's shape from
// tide_provider_bom.go: in-memory map + mutex + JSON-on-disk persistence +
// stale-on-fetch-error fallback. One cache (and one disk file) per WASM
// provider, keyed by station ID, since a single provider may serve many
// stations.
type wasmTideCache struct {
	mu   sync.RWMutex
	data map[string]tideChartResult
	path string
}

type wasmTideCacheDisk struct {
	Station        tideStation        `json:"station"`
	Extremes       []tideExtremePoint `json:"extremes"`
	CurrentHeightM float64            `json:"current_height_m"`
	Direction      string             `json:"direction"`
	CachedAt       time.Time          `json:"cached_at"`
}

func newWasmTideCache(path string) *wasmTideCache {
	return &wasmTideCache{data: make(map[string]tideChartResult), path: path}
}

func (c *wasmTideCache) get(stationID string, ttl time.Duration) (tideChartResult, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result, ok := c.data[stationID]
	if !ok || time.Since(result.CachedAt) >= ttl {
		return tideChartResult{}, false
	}
	return result, true
}

func (c *wasmTideCache) getStale(stationID string) (tideChartResult, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result, ok := c.data[stationID]
	return result, ok
}

func (c *wasmTideCache) set(stationID string, result tideChartResult) {
	c.mu.Lock()
	c.data[stationID] = result
	c.mu.Unlock()
	c.persistToDisk()
}

func (c *wasmTideCache) loadFromDisk() {
	raw, err := os.ReadFile(c.path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("plugins/tides: failed to read cache file %s: %v", c.path, err)
		}
		return
	}

	payload := map[string]wasmTideCacheDisk{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		log.Printf("plugins/tides: failed to parse cache file %s: %v", c.path, err)
		return
	}

	loaded := make(map[string]tideChartResult, len(payload))
	for key, item := range payload {
		loaded[key] = tideChartResult{
			Station:        item.Station,
			Extremes:       item.Extremes,
			CurrentHeightM: item.CurrentHeightM,
			Direction:      item.Direction,
			CachedAt:       item.CachedAt,
		}
	}

	c.mu.Lock()
	c.data = loaded
	c.mu.Unlock()
}

func (c *wasmTideCache) persistToDisk() {
	c.mu.RLock()
	payload := make(map[string]wasmTideCacheDisk, len(c.data))
	for key, item := range c.data {
		payload[key] = wasmTideCacheDisk{
			Station:        item.Station,
			Extremes:       item.Extremes,
			CurrentHeightM: item.CurrentHeightM,
			Direction:      item.Direction,
			CachedAt:       item.CachedAt,
		}
	}
	c.mu.RUnlock()

	if err := writeJSONFileAtomic(c.path, payload); err != nil {
		log.Printf("plugins/tides: failed to persist cache to %s: %v", c.path, err)
	}
}

// wasmPluginTimeoutMS resolves the manifest timeout (ms) from
// WASM_PLUGIN_TIMEOUT_MS, falling back to defaultWasmPluginTimeoutMS on an
// unset or invalid value (logged loudly rather than failing plugin discovery
// outright over a malformed env var).
func wasmPluginTimeoutMS() uint64 {
	raw := getEnv("WASM_PLUGIN_TIMEOUT_MS", strconv.Itoa(defaultWasmPluginTimeoutMS))
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		log.Printf("plugins/tides: invalid WASM_PLUGIN_TIMEOUT_MS %q, falling back to %dms", raw, defaultWasmPluginTimeoutMS)
		return defaultWasmPluginTimeoutMS
	}
	return uint64(v)
}

// pluginsTidesDir follows this codebase's existing cacheFilePath(envKey,
// fallback) idiom (see weather_tide.go) for env-override-with-fallback
// directory resolution.
func pluginsTidesDir() string {
	return cacheFilePath("PLUGINS_TIDES_DIR", "plugins/tides")
}

// allowedHostsForWasmPlugin reads the companion <name>.allowed_hosts.json
// file next to a .wasm plugin (JSON array of strings). A missing file is the
// safe default (no network access for that plugin), not an error. A file
// that exists but is malformed IS an error - a plugin author's broken config
// should fail loudly, not be silently treated as "no hosts allowed".
func allowedHostsForWasmPlugin(wasmPath string) ([]string, error) {
	companion := strings.TrimSuffix(wasmPath, ".wasm") + ".allowed_hosts.json"
	raw, err := os.ReadFile(companion)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read allowed hosts file %s: %w", companion, err)
	}

	var hosts []string
	if err := json.Unmarshal(raw, &hosts); err != nil {
		return nil, fmt.Errorf("failed to parse allowed hosts file %s: %w", companion, err)
	}
	return hosts, nil
}

// newWasmTideProvider compiles the .wasm file at path and validates its
// contract at discovery time (id()/name() must resolve and be callable).
// It deliberately does NOT call search_stations/fetch_tide_chart here -
// those need live network access, which has no place at container boot.
func newWasmTideProvider(path string) (*wasmTideProvider, error) {
	allowedHosts, err := allowedHostsForWasmPlugin(path)
	if err != nil {
		return nil, err
	}

	manifest := extism.Manifest{
		Wasm:         []extism.Wasm{extism.WasmFile{Path: path}},
		AllowedHosts: allowedHosts,
		Timeout:      wasmPluginTimeoutMS(),
	}

	return newWasmTideProviderWithManifest(manifest)
}

// wasmManifestLabel returns a human-readable label for a manifest's first
// wasm source, for use in error/log messages, falling back to a generic
// label for manifest.Wasm sources that aren't a plain WasmFile.
func wasmManifestLabel(manifest extism.Manifest) string {
	if len(manifest.Wasm) > 0 {
		if f, ok := manifest.Wasm[0].(extism.WasmFile); ok && f.Path != "" {
			return f.Path
		}
	}
	return "wasm plugin"
}

// newWasmTideProviderWithManifest is split out from newWasmTideProvider so
// tests can construct a provider against a fully custom manifest (e.g. a
// short Timeout, or explicit AllowedHosts) without needing a real companion
// allowed-hosts file on disk or the WASM_PLUGIN_TIMEOUT_MS env var.
func newWasmTideProviderWithManifest(manifest extism.Manifest) (provider *wasmTideProvider, err error) {
	path := wasmManifestLabel(manifest)

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("plugin %s: panic during construction: %v", path, r)
		}
	}()

	ctx := context.Background()
	compiled, cerr := extism.NewCompiledPlugin(ctx, manifest, extism.PluginConfig{EnableWasi: true}, []extism.HostFunction{})
	if cerr != nil {
		return nil, fmt.Errorf("failed to compile plugin %s: %w", path, cerr)
	}

	instance, ierr := compiled.Instance(ctx, extism.PluginInstanceConfig{ModuleConfig: wasmModuleConfig()})
	if ierr != nil {
		compiled.Close(ctx)
		return nil, fmt.Errorf("failed to instantiate plugin %s: %w", path, ierr)
	}
	defer instance.Close(ctx)

	if !instance.FunctionExists("id") {
		compiled.Close(ctx)
		return nil, fmt.Errorf("plugin %s does not export id()", path)
	}
	_, idOut, ierr := instance.Call("id", nil)
	if ierr != nil {
		compiled.Close(ctx)
		return nil, fmt.Errorf("plugin %s: id() call failed: %w", path, ierr)
	}
	id := strings.TrimSpace(string(idOut))
	if id == "" {
		compiled.Close(ctx)
		return nil, fmt.Errorf("plugin %s: id() returned empty string", path)
	}

	if !instance.FunctionExists("name") {
		compiled.Close(ctx)
		return nil, fmt.Errorf("plugin %s does not export name()", path)
	}
	_, nameOut, ierr := instance.Call("name", nil)
	if ierr != nil {
		compiled.Close(ctx)
		return nil, fmt.Errorf("plugin %s: name() call failed: %w", path, ierr)
	}
	name := strings.TrimSpace(string(nameOut))
	if name == "" {
		name = id
	}

	ttlSeconds := int64(defaultWasmPluginTTLSeconds)
	if instance.FunctionExists("ttl_seconds") {
		_, ttlOut, terr := instance.Call("ttl_seconds", nil)
		if terr != nil {
			log.Printf("plugin %s: ttl_seconds() call failed, using default %ds: %v", path, defaultWasmPluginTTLSeconds, terr)
		} else if v, perr := strconv.ParseInt(strings.TrimSpace(string(ttlOut)), 10, 64); perr == nil && v > 0 {
			ttlSeconds = v
		} else {
			log.Printf("plugin %s: ttl_seconds() returned unparseable value %q, using default %ds", path, string(ttlOut), defaultWasmPluginTTLSeconds)
		}
	}

	cacheFile := cacheFilePath(
		"TIDE_WASM_CACHE_FILE_"+strings.ToUpper(strings.ReplaceAll(id, "-", "_")),
		fmt.Sprintf("cache/tide_wasm_%s_cache.json", id),
	)
	cache := newWasmTideCache(cacheFile)
	cache.loadFromDisk()

	return &wasmTideProvider{
		id:         id,
		name:       name,
		ttlSeconds: ttlSeconds,
		compiled:   compiled,
		cache:      cache,
	}, nil
}

func (p *wasmTideProvider) ID() string        { return p.id }
func (p *wasmTideProvider) Name() string      { return p.name }
func (p *wasmTideProvider) TTLSeconds() int64 { return p.ttlSeconds }

func (p *wasmTideProvider) ttlDuration() time.Duration {
	return time.Duration(p.ttlSeconds) * time.Second
}

// call creates a fresh extism.Plugin ("Instance") for this call, invokes the
// named export with the given input, and closes the instance afterwards.
// A fresh instance per call is Extism's documented pattern for
// concurrent-safe access, since a Plugin/Instance is not itself safe for
// concurrent use. Shared by SearchStations and fetchFromPlugin below, and
// exercised directly by tests that need to call fixture-specific exports
// (e.g. the httpfetch/panicker fixtures' probe_url/crash).
func (p *wasmTideProvider) call(name string, input []byte) (out []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("plugin %q panicked calling %s: %v", p.id, name, r)
		}
	}()

	ctx := context.Background()
	instance, ierr := p.compiled.Instance(ctx, extism.PluginInstanceConfig{ModuleConfig: wasmModuleConfig()})
	if ierr != nil {
		return nil, fmt.Errorf("plugin %q: failed to create instance: %w", p.id, ierr)
	}
	defer instance.Close(ctx)

	_, out, err = instance.Call(name, input)
	if err != nil {
		return nil, fmt.Errorf("plugin %q: %s call failed: %w", p.id, name, err)
	}
	return out, nil
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

// loadWasmTideProviders scans dir once at startup for .wasm plugins. A
// missing dir is the normal default state (zero plugins) and is silent. Any
// other read error is logged. A file that fails to load as a valid plugin
// is logged and skipped - discovery continues for the remaining files,
// mirroring sat_charts.go's listSatChartsHandler "skip corrupt, keep going"
// idiom. Because native providers register first in main.go, a WASM plugin
// can never shadow "bom" or "stormglass" - first-registered wins.
func loadWasmTideProviders(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("plugins/tides: failed to read %s: %v", dir, err)
		}
		return
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".wasm") {
			continue
		}

		provider, err := newWasmTideProvider(filepath.Join(dir, e.Name()))
		if err != nil {
			log.Printf("plugins/tides: skipping invalid plugin %q: %v", e.Name(), err)
			continue
		}

		if _, exists := getTideProvider(provider.ID()); exists {
			log.Printf("plugins/tides: skipping %q: id %q already registered", e.Name(), provider.ID())
			continue
		}

		registerTideProvider(provider)
		log.Printf("plugins/tides: registered plugin %q (%s) from %s", provider.ID(), provider.Name(), e.Name())
	}
}
