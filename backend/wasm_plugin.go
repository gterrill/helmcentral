// Package main: shared WASM plugin host layer.
//
// This file holds the generic machinery every WASM plugin type (tide,
// weather, wave, ...) reuses: compiling+validating the universal
// id()/name()/ttl_seconds() contract, a fresh-Instance-per-call helper, the
// allowed-hosts and config/secrets companion-file channels, a unified
// manifest builder, a generic disk-backed TTL cache, and a generic plugin
// directory scanner. Type-specific adapters (wasm_tide_provider.go, and
// later weather/wave adapters) embed wasmPluginBase and wrap
// wasmPluginCache[T] with their own result type.
//
// Every WASM plugin also gets Extism's built-in HTTP host function (via
// manifest.AllowedHosts, enforced internally by go-sdk) plus a custom
// "ftp_fetch" host function (wasm_ftp_fetch.go), gated by that same
// AllowedHosts list, so a guest that needs FTP (e.g. the planned Forecast
// Warnings plugin's BOM default, which can only reliably fetch bulletins
// over anonymous FTP) can get it without any type-specific wiring.
//
// NOTE on this file's name: like wasm_tide_provider.go, it is deliberately
// NOT named anything ending in "_wasm.go" (or "_wasm_test.go" for the test
// file) - such a name is treated by the Go toolchain as an implicit
// GOARCH=wasm build constraint and silently excluded from ordinary builds.
// See wasm_tide_provider.go's header comment for the full explanation.
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
// for deterministic testing, but silently broken for any plugin that
// computes a date range via time.Now(), since every such request ends up
// asking the upstream API for a fixed historical/invalid date instead of
// "today".
func wasmModuleConfig() wazero.ModuleConfig {
	return wazero.NewModuleConfig().WithSysWalltime()
}

const (
	defaultWasmPluginTimeoutMS  = 15000 // ms, overridable via WASM_PLUGIN_TIMEOUT_MS
	defaultWasmPluginTTLSeconds = 3600  // used when a plugin has no ttl_seconds() export
)

// wasmPluginBase holds the identity + compiled module shared by every WASM
// plugin type (tide, weather, wave, ...). One extism.CompiledPlugin is
// compiled once per discovered .wasm file; a fresh extism.Plugin
// ("Instance") is created per call via (*wasmPluginBase).call, since a
// Plugin is not safe for concurrent use but CompiledPlugin.Instance is
// Extism's documented pattern for concurrent-safe access.
type wasmPluginBase struct {
	id         string
	name       string
	ttlSeconds int64
	compiled   *extism.CompiledPlugin
}

func (b *wasmPluginBase) ID() string        { return b.id }
func (b *wasmPluginBase) Name() string      { return b.name }
func (b *wasmPluginBase) TTLSeconds() int64 { return b.ttlSeconds }

func (b *wasmPluginBase) ttlDuration() time.Duration {
	return time.Duration(b.ttlSeconds) * time.Second
}

// call creates a fresh extism.Plugin ("Instance") for this call, invokes the
// named export with the given input, and closes the instance afterwards.
// A fresh instance per call is Extism's documented pattern for
// concurrent-safe access, since a Plugin/Instance is not itself safe for
// concurrent use. Shared by every plugin type's exported-function callers.
func (b *wasmPluginBase) call(name string, input []byte) (out []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("plugin %q panicked calling %s: %v", b.id, name, r)
		}
	}()

	ctx := context.Background()
	instance, ierr := b.compiled.Instance(ctx, extism.PluginInstanceConfig{ModuleConfig: wasmModuleConfig()})
	if ierr != nil {
		return nil, fmt.Errorf("plugin %q: failed to create instance: %w", b.id, ierr)
	}
	defer instance.Close(ctx)

	_, out, err = instance.Call(name, input)
	if err != nil {
		return nil, fmt.Errorf("plugin %q: %s call failed: %w", b.id, name, err)
	}
	return out, nil
}

// wasmPluginTimeoutMS resolves the manifest timeout (ms) from
// WASM_PLUGIN_TIMEOUT_MS, falling back to defaultWasmPluginTimeoutMS on an
// unset or invalid value (logged loudly rather than failing plugin discovery
// outright over a malformed env var).
func wasmPluginTimeoutMS() uint64 {
	raw := getEnv("WASM_PLUGIN_TIMEOUT_MS", strconv.Itoa(defaultWasmPluginTimeoutMS))
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		log.Printf("wasm plugins: invalid WASM_PLUGIN_TIMEOUT_MS %q, falling back to %dms", raw, defaultWasmPluginTimeoutMS)
		return defaultWasmPluginTimeoutMS
	}
	return uint64(v)
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

// allowedSecretsForWasmPlugin reads the companion <name>.allowed_secrets.json
// file next to a .wasm plugin (JSON array of secret key names, e.g.
// "WEATHERKIT_KEY_ID"). A missing file is the safe default (no secrets
// visible to that plugin), not an error - mirroring
// allowedHostsForWasmPlugin's missing-file contract. A file that exists but
// is malformed JSON IS an error, same "fail loudly" reasoning. Only names in
// knownSecretKeys are ever gated through this allowlist at all (see
// configForWasmPlugin's mapping closure); this file has no effect on
// ordinary, non-secret env var references in a plugin's config.json.
func allowedSecretsForWasmPlugin(wasmPath string) ([]string, error) {
	companion := strings.TrimSuffix(wasmPath, ".wasm") + ".allowed_secrets.json"
	raw, err := os.ReadFile(companion)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read allowed secrets file %s: %w", companion, err)
	}

	var keys []string
	if err := json.Unmarshal(raw, &keys); err != nil {
		return nil, fmt.Errorf("failed to parse allowed secrets file %s: %w", companion, err)
	}
	return keys, nil
}

// configForWasmPlugin reads the companion <name>.config.json file next to a
// .wasm plugin: a flat JSON object of string values. Each value is expanded
// against the process environment via os.Expand (so a plugin author writes
// "${WEATHERKIT_KEY_ID}" and the operator sets that env var on the backend
// container). If ANY env var referenced inside a value is unset, that whole
// key is dropped from the returned map - never substituted with an empty
// string - so a plugin's own "is this config key present?" check behaves
// correctly for operators who haven't set the optional keys a given plugin
// doesn't need. A missing companion file is the normal default (nil map, no
// error) - most plugins (e.g. Open-Meteo) need no config at all. A file that
// exists but is malformed JSON IS an error - same "fail loudly" reasoning as
// allowedHostsForWasmPlugin.
//
// Secrets gate: a referenced name that is one of knownSecretKeys (see
// secrets_store.go) is resolved from globalSecretsStore instead of the raw
// process environment, and ONLY if the plugin's companion
// <name>.allowed_secrets.json explicitly lists it - this is the actual
// security boundary that keeps secrets like WEATHERKIT_PRIVATE_KEY from
// being globally visible to every plugin via os.Setenv (LoadIntoEnv
// deliberately never sets WEATHERKIT_* into the process env at all).
// Non-secret names are entirely unaffected and keep today's raw
// os.LookupEnv behavior.
func configForWasmPlugin(wasmPath string) (map[string]string, error) {
	companion := strings.TrimSuffix(wasmPath, ".wasm") + ".config.json"
	raw, err := os.ReadFile(companion)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read config file %s: %w", companion, err)
	}

	var fields map[string]string
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", companion, err)
	}

	allowedSecrets, err := allowedSecretsForWasmPlugin(wasmPath)
	if err != nil {
		return nil, err
	}
	allowedSecretsSet := make(map[string]bool, len(allowedSecrets))
	for _, k := range allowedSecrets {
		allowedSecretsSet[k] = true
	}

	sawUnset := false
	mapping := func(name string) string {
		if isKnownSecretKey(name) {
			if !allowedSecretsSet[name] {
				log.Printf("wasm plugin %q: secret %q not in allowed_secrets.json, denied", strings.TrimSuffix(filepath.Base(wasmPath), ".wasm"), name)
				sawUnset = true
				return ""
			}
			if globalSecretsStore == nil {
				sawUnset = true
				return ""
			}
			v, ok, gerr := globalSecretsStore.Get(name)
			if gerr != nil || !ok {
				sawUnset = true
				return ""
			}
			return v
		}
		v, ok := os.LookupEnv(name)
		if !ok {
			sawUnset = true
			return ""
		}
		return v
	}

	result := make(map[string]string, len(fields))
	for key, v := range fields {
		sawUnset = false
		expanded := os.Expand(v, mapping)
		if sawUnset {
			continue
		}
		result[key] = expanded
	}

	return result, nil
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

// manifestForWasmPlugin builds the extism.Manifest for path: allowed hosts
// (allowedHostsForWasmPlugin), config (configForWasmPlugin), and the shared
// WASM_PLUGIN_TIMEOUT_MS timeout. Used by all plugin types (tide, weather,
// wave, ...).
func manifestForWasmPlugin(path string) (extism.Manifest, error) {
	allowedHosts, err := allowedHostsForWasmPlugin(path)
	if err != nil {
		return extism.Manifest{}, err
	}

	config, err := configForWasmPlugin(path)
	if err != nil {
		return extism.Manifest{}, err
	}

	return extism.Manifest{
		Wasm:         []extism.Wasm{extism.WasmFile{Path: path}},
		AllowedHosts: allowedHosts,
		Config:       config,
		Timeout:      wasmPluginTimeoutMS(),
	}, nil
}

// newWasmPluginBase compiles manifest and validates the universal plugin
// contract (id()/name() required and callable and non-empty; ttl_seconds()
// optional, defaulting to defaultWasmPluginTTLSeconds on absence or error).
// logPrefix is used in log.Printf lines only (e.g. "plugins/tides",
// "plugins/weather"). The compiled plugin is given a generic "ftp_fetch"
// custom host function (wasm_ftp_fetch.go), gated by manifest.AllowedHosts -
// the same allowlist Extism's own built-in HTTP host function already
// enforces - so every plugin type gets FTP access for free without any
// type-specific wiring; a plugin that never imports ftp_fetch is completely
// unaffected by its presence.
func newWasmPluginBase(manifest extism.Manifest, logPrefix string) (base *wasmPluginBase, err error) {
	path := wasmManifestLabel(manifest)

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("plugin %s: panic during construction: %v", path, r)
		}
	}()

	ctx := context.Background()
	compiled, cerr := extism.NewCompiledPlugin(ctx, manifest, extism.PluginConfig{EnableWasi: true}, []extism.HostFunction{newFTPFetchHostFunction(manifest.AllowedHosts)})
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
			log.Printf("%s: plugin %s: ttl_seconds() call failed, using default %ds: %v", logPrefix, path, defaultWasmPluginTTLSeconds, terr)
		} else if v, perr := strconv.ParseInt(strings.TrimSpace(string(ttlOut)), 10, 64); perr == nil && v > 0 {
			ttlSeconds = v
		} else {
			log.Printf("%s: plugin %s: ttl_seconds() returned unparseable value %q, using default %ds", logPrefix, path, string(ttlOut), defaultWasmPluginTTLSeconds)
		}
	}

	return &wasmPluginBase{
		id:         id,
		name:       name,
		ttlSeconds: ttlSeconds,
		compiled:   compiled,
	}, nil
}

// wasmCacheEntry wraps a cached value T with the bookkeeping timestamp used
// for TTL/staleness decisions. Note this CachedAt is the cache's own "when
// was this entry stored" bookkeeping - independent of any CachedAt-shaped
// field a given T might carry for its own (e.g. API response) purposes.
type wasmCacheEntry[T any] struct {
	Value    T         `json:"value"`
	CachedAt time.Time `json:"cached_at"`
}

// wasmPluginCache is a generic in-memory + disk-persisted TTL cache, keyed
// by an arbitrary string key (e.g. a tide station ID). One cache (and one
// disk file) per plugin instance, mirroring bomTideCache/bomTideCacheDisk's
// shape from tide_provider_bom.go: in-memory map + mutex + JSON-on-disk
// persistence + stale-on-fetch-error fallback via getStale.
type wasmPluginCache[T any] struct {
	mu   sync.RWMutex
	data map[string]wasmCacheEntry[T]
	path string
}

func newWasmPluginCache[T any](path string) *wasmPluginCache[T] {
	return &wasmPluginCache[T]{data: make(map[string]wasmCacheEntry[T]), path: path}
}

func (c *wasmPluginCache[T]) get(key string, ttl time.Duration) (T, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.data[key]
	if !ok || time.Since(entry.CachedAt) >= ttl {
		var zero T
		return zero, false
	}
	return entry.Value, true
}

func (c *wasmPluginCache[T]) getStale(key string) (T, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.data[key]
	return entry.Value, ok
}

func (c *wasmPluginCache[T]) set(key string, value T) {
	c.mu.Lock()
	c.data[key] = wasmCacheEntry[T]{Value: value, CachedAt: time.Now().UTC()}
	c.mu.Unlock()
	c.persistToDisk()
}

// loadFromDisk populates the cache from c.path, tolerating a missing file
// (the normal default - same end state as a freshly constructed cache).
//
// IMPORTANT: the pre-refactor tide cache file format was FLAT per entry
// ({"station":...,"extremes":...,"current_height_m":...,"direction":...,
// "cached_at":...}), not this type's {"value":{...},"cached_at":...}
// envelope. Naively unmarshaling an old-format file into
// map[string]wasmCacheEntry[T] would NOT error (Go's lenient JSON decoding
// would silently produce entries with a zero-value Value but a real
// CachedAt, since "cached_at" matches at both levels) - that would make a
// stale-cache fallback return bogus zero-valued data instead of erroring,
// exactly the kind of silent-masking behavior this codebase's fail-fast
// policy forbids. So: before trusting the parsed data, detect the old flat
// shape (no top-level "value" key on any entry) and discard the WHOLE file
// with a logged warning, leaving the cache empty - the same end state as
// "file doesn't exist" - rather than partially loading zero-valued entries.
func (c *wasmPluginCache[T]) loadFromDisk() {
	raw, err := os.ReadFile(c.path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("wasm plugins: failed to read cache file %s: %v", c.path, err)
		}
		return
	}

	var rawEntries map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rawEntries); err != nil {
		log.Printf("wasm plugins: failed to parse cache file %s: %v", c.path, err)
		return
	}

	for key, entryRaw := range rawEntries {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(entryRaw, &fields); err != nil {
			log.Printf("wasm plugins: cache file %s is in an unrecognized format (entry %q failed to parse), discarding: %v", c.path, key, err)
			return
		}
		if _, ok := fields["value"]; !ok {
			log.Printf("wasm plugins: cache file %s is in an old format, discarding", c.path)
			return
		}
	}

	payload := map[string]wasmCacheEntry[T]{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		log.Printf("wasm plugins: failed to parse cache file %s: %v", c.path, err)
		return
	}

	c.mu.Lock()
	c.data = payload
	c.mu.Unlock()
}

func (c *wasmPluginCache[T]) persistToDisk() {
	c.mu.RLock()
	payload := make(map[string]wasmCacheEntry[T], len(c.data))
	for key, entry := range c.data {
		payload[key] = entry
	}
	c.mu.RUnlock()

	if err := writeJSONFileAtomic(c.path, payload); err != nil {
		log.Printf("wasm plugins: failed to persist cache to %s: %v", c.path, err)
	}
}

// loadWasmPluginsFromDir scans dir once for *.wasm files and, for each,
// builds a manifest (manifestForWasmPlugin), compiles+validates it
// (newWasmPluginBase(manifest, logPrefix)), and if the resulting base.ID()
// is not already known (per exists), calls register with the base and the
// plugin's file path so the caller can build its concrete provider type
// (wrapping base with a type-specific cache, etc.) and do its own
// registration. Any error at any step is logged with logPrefix and that file
// is skipped; discovery continues for the rest.
//
// A missing dir is the normal default state (zero plugins) and is silent.
// Any other read error is logged. Discovery order (and therefore
// first-registered-wins semantics via exists) follows os.ReadDir's
// lexicographic order.
func loadWasmPluginsFromDir(dir, logPrefix string, exists func(id string) bool, register func(base *wasmPluginBase, path string) error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("%s: failed to read %s: %v", logPrefix, dir, err)
		}
		return
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".wasm") {
			continue
		}

		path := filepath.Join(dir, e.Name())

		manifest, merr := manifestForWasmPlugin(path)
		if merr != nil {
			log.Printf("%s: skipping invalid plugin %q: %v", logPrefix, e.Name(), merr)
			continue
		}

		base, berr := newWasmPluginBase(manifest, logPrefix)
		if berr != nil {
			log.Printf("%s: skipping invalid plugin %q: %v", logPrefix, e.Name(), berr)
			continue
		}

		if exists(base.ID()) {
			log.Printf("%s: skipping %q: id %q already registered", logPrefix, e.Name(), base.ID())
			continue
		}

		if rerr := register(base, path); rerr != nil {
			log.Printf("%s: skipping %q: %v", logPrefix, e.Name(), rerr)
			continue
		}
	}
}
