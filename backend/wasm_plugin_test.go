// Fixture regeneration:
//
// The compiled .wasm fixtures under backend/testdata/wasm_plugins/ are built
// from the TinyGo sources under backend/testdata/wasm_plugins/src/<name>/
// using the tinygo/tinygo:0.41.1 Docker image, sharing one go.mod the same
// way backend/testdata/tide_plugins/src does. To regenerate all fixtures
// under this directory, run from the repo root:
//
//	docker run --rm -v $(pwd):/src -w /src tinygo/tinygo:0.41.1 sh -c "
//	  cd backend/testdata/wasm_plugins/src &&
//	  go mod tidy &&
//	  tinygo build -o /src/backend/testdata/wasm_plugins/es256sign.wasm -target wasip1 -buildmode c-shared ./es256sign &&
//	  tinygo build -o /src/backend/testdata/wasm_plugins/configecho.wasm -target wasip1 -buildmode c-shared ./configecho
//	"
package main

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	extism "github.com/extism/go-sdk"
)

const configEchoFixtureWasm = "testdata/wasm_plugins/configecho.wasm"

// wasmPluginCacheTestValue is a small local struct used to prove
// wasmPluginCache[T] is generic, rather than reusing tideChartResult for
// every test.
type wasmPluginCacheTestValue struct {
	Foo string
	Bar int
}

func TestWasmPluginCache_SetThenGetWithinTTLHits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	cache := newWasmPluginCache[wasmPluginCacheTestValue](path)

	want := wasmPluginCacheTestValue{Foo: "a", Bar: 1}
	cache.set("k1", want)

	got, ok := cache.get("k1", time.Hour)
	if !ok {
		t.Fatalf("expected a cache hit for a freshly set key within TTL")
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestWasmPluginCache_PastTTLMissesButGetStaleHits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	cache := newWasmPluginCache[wasmPluginCacheTestValue](path)

	want := wasmPluginCacheTestValue{Foo: "a", Bar: 1}
	cache.set("k1", want)

	// Backdate the entry directly (same package, unexported field access) to
	// simulate TTL expiry without sleeping in a test.
	cache.mu.Lock()
	entry := cache.data["k1"]
	entry.CachedAt = time.Now().Add(-2 * time.Hour)
	cache.data["k1"] = entry
	cache.mu.Unlock()

	if _, ok := cache.get("k1", time.Hour); ok {
		t.Fatalf("expected a cache miss for an entry past its TTL")
	}

	stale, ok := cache.getStale("k1")
	if !ok {
		t.Fatalf("expected getStale to still return the expired entry")
	}
	if stale != want {
		t.Errorf("got %+v, want %+v", stale, want)
	}
}

func TestWasmPluginCache_PersistToDiskThenLoadFromDiskRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	cache := newWasmPluginCache[wasmPluginCacheTestValue](path)

	cache.set("k1", wasmPluginCacheTestValue{Foo: "a", Bar: 1})
	cache.set("k2", wasmPluginCacheTestValue{Foo: "b", Bar: 2})

	reloaded := newWasmPluginCache[wasmPluginCacheTestValue](path)
	reloaded.loadFromDisk()

	got1, ok := reloaded.get("k1", time.Hour)
	if !ok || got1 != (wasmPluginCacheTestValue{Foo: "a", Bar: 1}) {
		t.Errorf("k1 mismatch after reload: ok=%v got=%+v", ok, got1)
	}
	got2, ok := reloaded.get("k2", time.Hour)
	if !ok || got2 != (wasmPluginCacheTestValue{Foo: "b", Bar: 2}) {
		t.Errorf("k2 mismatch after reload: ok=%v got=%+v", ok, got2)
	}
}

// TestWasmPluginCache_LoadFromDisk_DiscardsOldFlatFormat writes a cache file
// in the OLD pre-refactor flat shape (wasmTideCacheDisk: {"station":...,
// "extremes":...,"current_height_m":...,"direction":...,"cached_at":...})
// directly at the top level of each entry, with no "value" envelope key.
// loadFromDisk must detect this incompatible shape and discard the whole
// file (leaving the cache empty) rather than silently loading zero-valued
// entries - see the long comment on loadFromDisk in wasm_plugin.go for why
// that would otherwise be a silent-masking bug.
func TestWasmPluginCache_LoadFromDisk_DiscardsOldFlatFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old_format_cache.json")
	oldFormat := `{
		"STATION1": {
			"station": {"station_id":"STATION1","name":"Old Station","lat":-33.8,"lon":151.2},
			"extremes": [{"time":"2026-01-01T00:00:00Z","height_m":1.5,"high":true}],
			"current_height_m": 1.5,
			"direction": "Rising",
			"cached_at": "2026-01-01T00:00:00Z"
		}
	}`
	if err := os.WriteFile(path, []byte(oldFormat), 0o644); err != nil {
		t.Fatalf("write old-format cache file: %v", err)
	}

	var logBuf bytes.Buffer
	origOutput := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(origOutput)

	cache := newWasmPluginCache[tideChartResult](path)
	cache.loadFromDisk()

	if got, ok := cache.get("STATION1", time.Hour); ok {
		t.Fatalf("expected the old-format file to be discarded entirely, got a populated entry: %+v", got)
	}
	if _, ok := cache.getStale("STATION1"); ok {
		t.Fatalf("expected the old-format file to be discarded entirely (getStale also empty)")
	}
	if logBuf.Len() == 0 {
		t.Errorf("expected a warning to be logged about the old-format cache file")
	}
}

func TestConfigForWasmPlugin_NoCompanionFileReturnsNoErrorAndEmptyMap(t *testing.T) {
	dir := t.TempDir()
	wasmPath := filepath.Join(dir, "plugin.wasm")

	config, err := configForWasmPlugin(wasmPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(config) != 0 {
		t.Errorf("expected no config entries, got %+v", config)
	}
}

func TestConfigForWasmPlugin_ExpandsSetEnvVar(t *testing.T) {
	t.Setenv("WASM_PLUGIN_TEST_KEY", "secret-value")

	dir := t.TempDir()
	wasmPath := filepath.Join(dir, "plugin.wasm")
	companion := filepath.Join(dir, "plugin.config.json")
	if err := os.WriteFile(companion, []byte(`{"api_key":"${WASM_PLUGIN_TEST_KEY}"}`), 0o644); err != nil {
		t.Fatalf("write companion file: %v", err)
	}

	config, err := configForWasmPlugin(wasmPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config["api_key"] != "secret-value" {
		t.Errorf("expected api_key=secret-value, got %+v", config)
	}
}

func TestConfigForWasmPlugin_DropsKeyReferencingUnsetEnvVarButKeepsOthers(t *testing.T) {
	dir := t.TempDir()
	wasmPath := filepath.Join(dir, "plugin.wasm")
	companion := filepath.Join(dir, "plugin.config.json")
	body := `{"missing_key":"${WASM_PLUGIN_TEST_DEFINITELY_UNSET_KEY}","present_key":"literal-value"}`
	if err := os.WriteFile(companion, []byte(body), 0o644); err != nil {
		t.Fatalf("write companion file: %v", err)
	}

	config, err := configForWasmPlugin(wasmPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := config["missing_key"]; ok {
		t.Errorf("expected missing_key to be absent from the result, got %+v", config)
	}
	if config["present_key"] != "literal-value" {
		t.Errorf("expected present_key=literal-value unaffected by the dropped key, got %+v", config)
	}
}

func TestConfigForWasmPlugin_MalformedJSONIsError(t *testing.T) {
	dir := t.TempDir()
	wasmPath := filepath.Join(dir, "plugin.wasm")
	companion := filepath.Join(dir, "plugin.config.json")
	if err := os.WriteFile(companion, []byte(`{not valid json`), 0o644); err != nil {
		t.Fatalf("write companion file: %v", err)
	}

	if _, err := configForWasmPlugin(wasmPath); err == nil {
		t.Fatalf("expected an error for malformed config JSON, got nil")
	}
}

func TestManifestForWasmPlugin_MergesAllowedHostsConfigAndTimeout(t *testing.T) {
	t.Setenv("WASM_PLUGIN_TEST_MANIFEST_KEY", "manifest-value")
	t.Setenv("WASM_PLUGIN_TIMEOUT_MS", "12345")

	dir := t.TempDir()
	wasmPath := filepath.Join(dir, "plugin.wasm")
	if err := os.WriteFile(wasmPath, []byte("not a real wasm module"), 0o644); err != nil {
		t.Fatalf("write dummy wasm file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.allowed_hosts.json"), []byte(`["example.com"]`), 0o644); err != nil {
		t.Fatalf("write allowed hosts file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.config.json"), []byte(`{"key":"${WASM_PLUGIN_TEST_MANIFEST_KEY}"}`), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	manifest, err := manifestForWasmPlugin(wasmPath)
	if err != nil {
		t.Fatalf("manifestForWasmPlugin failed: %v", err)
	}

	if len(manifest.Wasm) != 1 {
		t.Fatalf("expected exactly 1 wasm source, got %d", len(manifest.Wasm))
	}
	if f, ok := manifest.Wasm[0].(extism.WasmFile); !ok || f.Path != wasmPath {
		t.Errorf("unexpected wasm source: %+v", manifest.Wasm[0])
	}
	if len(manifest.AllowedHosts) != 1 || manifest.AllowedHosts[0] != "example.com" {
		t.Errorf("unexpected AllowedHosts: %+v", manifest.AllowedHosts)
	}
	if manifest.Config["key"] != "manifest-value" {
		t.Errorf("unexpected Config: %+v", manifest.Config)
	}
	if manifest.Timeout != 12345 {
		t.Errorf("unexpected Timeout: %d", manifest.Timeout)
	}
}

// TestLoadWasmPluginsFromDir_SkipsInvalidKeepsGoing reuses the existing
// tide "valid" fixture purely as a generic contract-conforming .wasm file
// (id/name/ttl_seconds) - loadWasmPluginsFromDir itself has no tide-specific
// knowledge, it only validates the universal plugin contract.
func TestLoadWasmPluginsFromDir_SkipsInvalidKeepsGoing(t *testing.T) {
	dir := t.TempDir()

	goodBytes, err := os.ReadFile(validFixtureWasm)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "good.wasm"), goodBytes, 0o644); err != nil {
		t.Fatalf("write good fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.wasm"), []byte("not a real wasm module"), 0o644); err != nil {
		t.Fatalf("write corrupt fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore me, not a .wasm file"), 0o644); err != nil {
		t.Fatalf("write non-wasm file: %v", err)
	}

	var registeredIDs []string
	loadWasmPluginsFromDir(dir, "plugins/test",
		func(id string) bool { return false },
		func(base *wasmPluginBase, path string) error {
			registeredIDs = append(registeredIDs, base.ID())
			return nil
		},
	)

	if len(registeredIDs) != 1 {
		t.Fatalf("expected exactly 1 successful registration, got %d: %v", len(registeredIDs), registeredIDs)
	}
	if registeredIDs[0] != "valid-fixture" {
		t.Errorf("unexpected registered id: %q", registeredIDs[0])
	}
}

func TestLoadWasmPluginsFromDir_ExistsCallbackBlocksRegistration(t *testing.T) {
	dir := t.TempDir()

	goodBytes, err := os.ReadFile(validFixtureWasm)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "good.wasm"), goodBytes, 0o644); err != nil {
		t.Fatalf("write good fixture: %v", err)
	}

	registerCalled := false
	loadWasmPluginsFromDir(dir, "plugins/test",
		func(id string) bool { return true }, // simulate "already registered" for every id
		func(base *wasmPluginBase, path string) error {
			registerCalled = true
			return nil
		},
	)

	if registerCalled {
		t.Fatalf("expected register to never be called when exists always reports true")
	}
}

// TestConfigEchoPlugin_ConfigReachesGuest proves the config channel reaches
// the guest end-to-end through a real compiled plugin (configecho.wasm),
// not just the host-side manifest-building unit tests above.
func TestConfigEchoPlugin_ConfigReachesGuest(t *testing.T) {
	manifest := extism.Manifest{
		Wasm:   []extism.Wasm{extism.WasmFile{Path: configEchoFixtureWasm}},
		Config: map[string]string{"some_key": "some_value"},
	}

	base, err := newWasmPluginBase(manifest, "plugins/test")
	if err != nil {
		t.Fatalf("newWasmPluginBase failed: %v", err)
	}

	out, err := base.call("dump_config", nil)
	if err != nil {
		t.Fatalf("dump_config call failed: %v", err)
	}

	var result map[string]string
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("unmarshal dump_config output failed: %v (raw: %s)", err, out)
	}
	if result["some_key"] != "some_value" {
		t.Errorf("expected some_key=some_value, got %+v", result)
	}
}
