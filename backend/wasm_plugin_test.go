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
//	  tinygo build -o /src/backend/testdata/wasm_plugins/configecho.wasm -target wasip1 -buildmode c-shared ./configecho &&
//	  tinygo build -o /src/backend/testdata/wasm_plugins/describedvalid.wasm -target wasip1 -buildmode c-shared ./describedvalid &&
//	  tinygo build -o /src/backend/testdata/wasm_plugins/describederror.wasm -target wasip1 -buildmode c-shared ./describederror &&
//	  tinygo build -o /src/backend/testdata/wasm_plugins/poivalid.wasm -target wasip1 -buildmode c-shared ./poivalid
//	"
//
// describedvalid and describederror back the description()-export tests in
// this file (present-and-successful, and present-but-fails-when-called,
// respectively). The absent-export case is deliberately tested against an
// EXISTING unmodified fixture (e.g. configecho) rather than a new one, since
// "no description() export at all" is the normal/default state every
// pre-existing fixture already represents.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	extism "github.com/extism/go-sdk"
)

const configEchoFixtureWasm = "testdata/wasm_plugins/configecho.wasm"
const describedValidFixtureWasm = "testdata/wasm_plugins/describedvalid.wasm"
const describedErrorFixtureWasm = "testdata/wasm_plugins/describederror.wasm"

// osmOverpassRealPluginWasm is the actual, built osm-overpass POI plugin
// (docs/examples/poi-plugins/osm-overpass), not a committed testdata
// fixture - plugins/* is gitignored and only exists once the plugins-builder
// has run, so TestNewWasmPluginBase_SupportsPlaceNamesTrueWhenBothExportsPresent
// below skips rather than fails when it is absent (a fresh checkout, or CI).
const osmOverpassRealPluginWasm = "../plugins/poi/osm-overpass.wasm"

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

// TestWasmPluginCache_LoadFromDisk_UnparsableFileLogsAndStartsEmpty confirms
// loadFromDisk logs the path and the parse error for a file that exists but
// isn't valid JSON at all (as opposed to the old-flat-format case above,
// which is a different failure mode with its own test) - and, having
// logged, leaves the cache empty rather than partially populated. Starting
// empty after logging is the documented fine outcome; silently starting
// empty with nothing logged is the bug this guards against.
func TestWasmPluginCache_LoadFromDisk_UnparsableFileLogsAndStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt_cache.json")
	if err := os.WriteFile(path, []byte(`{"STATION1": {"value":`), 0o644); err != nil {
		t.Fatalf("write corrupt cache file: %v", err)
	}

	var logBuf bytes.Buffer
	origOutput := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(origOutput)

	cache := newWasmPluginCache[wasmPluginCacheTestValue](path)
	cache.loadFromDisk()

	if _, ok := cache.get("STATION1", time.Hour); ok {
		t.Fatalf("expected the unparsable file to leave the cache empty")
	}
	logged := logBuf.String()
	if !strings.Contains(logged, path) {
		t.Errorf("expected the log line to name the cache file path %q, got: %q", path, logged)
	}
}

// TestWasmPluginCache_Set_PrunesEntriesOlderThanMaxAge is the direct test
// for the "no eviction" fix: an entry old enough that it can no longer serve
// even as a stale-on-error fallback must be dropped the next time set() is
// called, not kept forever.
func TestWasmPluginCache_Set_PrunesEntriesOlderThanMaxAge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	cache := newWasmPluginCache[wasmPluginCacheTestValue](path)

	cache.set("old", wasmPluginCacheTestValue{Foo: "old"})
	cache.mu.Lock()
	entry := cache.data["old"]
	entry.CachedAt = time.Now().UTC().Add(-wasmPluginCacheMaxAge - time.Hour)
	cache.data["old"] = entry
	cache.mu.Unlock()

	// Any subsequent set() call prunes on its way in.
	cache.set("new", wasmPluginCacheTestValue{Foo: "new"})

	if _, ok := cache.getStale("old"); ok {
		t.Fatalf("expected the entry older than wasmPluginCacheMaxAge to be pruned")
	}
	if _, ok := cache.getStale("new"); !ok {
		t.Fatalf("expected the freshly set entry to survive pruning")
	}
}

// TestWasmPluginCache_Set_EvictsOldestPastMaxEntries is the direct test for
// the "file grows all season" fix: once past wasmPluginCacheMaxEntries, the
// single oldest entry (by CachedAt) is evicted on the next set(), not
// whatever happens to iterate first.
func TestWasmPluginCache_Set_EvictsOldestPastMaxEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	cache := newWasmPluginCache[wasmPluginCacheTestValue](path)

	// Fill to exactly the cap, each with a distinct, increasing CachedAt so
	// "oldest" is unambiguous.
	base := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < wasmPluginCacheMaxEntries; i++ {
		key := fmt.Sprintf("k%d", i)
		cache.set(key, wasmPluginCacheTestValue{Foo: key})
		cache.mu.Lock()
		entry := cache.data[key]
		entry.CachedAt = base.Add(time.Duration(i) * time.Second)
		cache.data[key] = entry
		cache.mu.Unlock()
	}

	if _, ok := cache.getStale("k0"); !ok {
		t.Fatalf("expected k0 (the oldest so far) to still be present before going over the cap")
	}

	// One more entry pushes the cache over the cap - k0 (the oldest) must
	// be the one evicted, everything else must survive.
	cache.set("newest", wasmPluginCacheTestValue{Foo: "newest"})

	if _, ok := cache.getStale("k0"); ok {
		t.Fatalf("expected the oldest entry (k0) to be evicted once over wasmPluginCacheMaxEntries")
	}
	for i := 1; i < wasmPluginCacheMaxEntries; i++ {
		key := fmt.Sprintf("k%d", i)
		if _, ok := cache.getStale(key); !ok {
			t.Fatalf("expected %s to survive eviction (only the single oldest entry should be dropped)", key)
		}
	}
	if _, ok := cache.getStale("newest"); !ok {
		t.Fatalf("expected the newly set entry to be present")
	}

	cache.mu.RLock()
	size := len(cache.data)
	cache.mu.RUnlock()
	if size != wasmPluginCacheMaxEntries {
		t.Fatalf("expected cache size to stay at the cap (%d), got %d", wasmPluginCacheMaxEntries, size)
	}
}

// TestWasmPluginCache_SingleflightFetch_ConcurrentMissesCauseOneUpstreamCall
// is the direct test for the in-flight request merging fix: N goroutines
// racing a miss for the same key must collapse into exactly one call to
// fetch, all of them getting fetch's single result back.
func TestWasmPluginCache_SingleflightFetch_ConcurrentMissesCauseOneUpstreamCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	cache := newWasmPluginCache[wasmPluginCacheTestValue](path)

	var calls atomic.Int32
	release := make(chan struct{})
	fetch := func() (wasmPluginCacheTestValue, error) {
		calls.Add(1)
		<-release // hold every caller "in flight" simultaneously
		return wasmPluginCacheTestValue{Foo: "fetched"}, nil
	}

	const callers = 20
	var wg sync.WaitGroup
	results := make([]wasmPluginCacheTestValue, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			v, err := cache.singleflightFetch("shared-key", fetch)
			if err != nil {
				t.Errorf("singleflightFetch: %v", err)
				return
			}
			results[n] = v
		}(i)
	}

	// Give every goroutine a chance to reach the fetch call before
	// releasing it, so the merge is actually being exercised under real
	// concurrency rather than resolving one at a time.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 upstream call for %d concurrent misses on the same key, got %d", callers, got)
	}
	for i, v := range results {
		if v.Foo != "fetched" {
			t.Errorf("caller %d: expected the shared fetched value, got %+v", i, v)
		}
	}
}

// TestWasmPluginCache_SingleflightFetch_ErrorPropagatesToAllWaiters proves
// a failed fetch is reported to every merged caller, not silently turned
// into a zero value for the followers.
func TestWasmPluginCache_SingleflightFetch_ErrorPropagatesToAllWaiters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	cache := newWasmPluginCache[wasmPluginCacheTestValue](path)

	wantErr := errors.New("simulated upstream failure")
	_, err := cache.singleflightFetch("k", func() (wasmPluginCacheTestValue, error) {
		return wasmPluginCacheTestValue{}, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected the fetch error to propagate, got %v", err)
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

// withTestPluginOverridesStore points globalPluginOverridesStore at a fresh
// t.TempDir()-backed store for the duration of the test, restoring the
// prior value (typically nil, in this package's tests) afterwards.
func withTestPluginOverridesStore(t *testing.T) *pluginOverridesStore {
	t.Helper()
	store := newTestPluginOverridesStore(t)
	prev := globalPluginOverridesStore
	globalPluginOverridesStore = store
	t.Cleanup(func() { globalPluginOverridesStore = prev })
	return store
}

// TestAllowedHostsForWasmPlugin_OverridePresentReturnsOverrideInsteadOfFile
// proves that a saved DB override takes priority over the companion
// <name>.allowed_hosts.json file - the file still exists on disk (an
// operator wouldn't necessarily delete it after saving an override via the
// Settings UI) but must be ignored once an override row exists.
func TestAllowedHostsForWasmPlugin_OverridePresentReturnsOverrideInsteadOfFile(t *testing.T) {
	store := withTestPluginOverridesStore(t)

	dir := t.TempDir()
	wasmPath := filepath.Join(dir, "plugin.wasm")
	if err := os.WriteFile(filepath.Join(dir, "plugin.allowed_hosts.json"), []byte(`["file.example.com"]`), 0o644); err != nil {
		t.Fatalf("write allowed hosts file: %v", err)
	}
	if err := store.Set(wasmPath, []string{"override.example.com"}, nil); err != nil {
		t.Fatalf("store.Set: %v", err)
	}

	hosts, err := allowedHostsForWasmPlugin(wasmPath)
	if err != nil {
		t.Fatalf("allowedHostsForWasmPlugin: %v", err)
	}
	if len(hosts) != 1 || hosts[0] != "override.example.com" {
		t.Errorf("expected override hosts [override.example.com], got %+v", hosts)
	}
}

// TestAllowedHostsForWasmPlugin_NoOverrideFallsBackToFileBasedBehavior is a
// regression check: with globalPluginOverridesStore set but no override row
// for this path, behavior must be unchanged from the pre-override,
// file-only implementation.
func TestAllowedHostsForWasmPlugin_NoOverrideFallsBackToFileBasedBehavior(t *testing.T) {
	withTestPluginOverridesStore(t)

	dir := t.TempDir()
	wasmPath := filepath.Join(dir, "plugin.wasm")
	if err := os.WriteFile(filepath.Join(dir, "plugin.allowed_hosts.json"), []byte(`["file.example.com"]`), 0o644); err != nil {
		t.Fatalf("write allowed hosts file: %v", err)
	}

	hosts, err := allowedHostsForWasmPlugin(wasmPath)
	if err != nil {
		t.Fatalf("allowedHostsForWasmPlugin: %v", err)
	}
	if len(hosts) != 1 || hosts[0] != "file.example.com" {
		t.Errorf("expected file-based hosts [file.example.com], got %+v", hosts)
	}
}

// TestAllowedSecretsForWasmPlugin_OverridePresentReturnsOverrideInsteadOfFile
// mirrors the allowed-hosts override test above for allowed_secrets.
func TestAllowedSecretsForWasmPlugin_OverridePresentReturnsOverrideInsteadOfFile(t *testing.T) {
	store := withTestPluginOverridesStore(t)

	dir := t.TempDir()
	wasmPath := filepath.Join(dir, "plugin.wasm")
	if err := os.WriteFile(filepath.Join(dir, "plugin.allowed_secrets.json"), []byte(`["FILE_SECRET"]`), 0o644); err != nil {
		t.Fatalf("write allowed secrets file: %v", err)
	}
	if err := store.Set(wasmPath, nil, []string{"OVERRIDE_SECRET"}); err != nil {
		t.Fatalf("store.Set: %v", err)
	}

	secrets, err := allowedSecretsForWasmPlugin(wasmPath)
	if err != nil {
		t.Fatalf("allowedSecretsForWasmPlugin: %v", err)
	}
	if len(secrets) != 1 || secrets[0] != "OVERRIDE_SECRET" {
		t.Errorf("expected override secrets [OVERRIDE_SECRET], got %+v", secrets)
	}
}

// TestAllowedSecretsForWasmPlugin_NoOverrideFallsBackToFileBasedBehavior is
// the allowed-secrets regression check mirroring the allowed-hosts one
// above.
func TestAllowedSecretsForWasmPlugin_NoOverrideFallsBackToFileBasedBehavior(t *testing.T) {
	withTestPluginOverridesStore(t)

	dir := t.TempDir()
	wasmPath := filepath.Join(dir, "plugin.wasm")
	if err := os.WriteFile(filepath.Join(dir, "plugin.allowed_secrets.json"), []byte(`["FILE_SECRET"]`), 0o644); err != nil {
		t.Fatalf("write allowed secrets file: %v", err)
	}

	secrets, err := allowedSecretsForWasmPlugin(wasmPath)
	if err != nil {
		t.Fatalf("allowedSecretsForWasmPlugin: %v", err)
	}
	if len(secrets) != 1 || secrets[0] != "FILE_SECRET" {
		t.Errorf("expected file-based secrets [FILE_SECRET], got %+v", secrets)
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

// TestNewWasmPluginBase_DescriptionPresentIsCaptured proves that an
// optional description() export, when present and successful, is captured
// on the resulting wasmPluginBase - mirroring ttl_seconds()'s existing
// present-and-successful behavior.
// ── SupportsPlaceNames: the optional place_name_at + search_places pair
// (ADR 0101, POI plugins declaring place-name support) ──────────────────────

// TestNewWasmPluginBase_SupportsPlaceNamesTrueWhenBothExportsPresent proves
// detection against the real osm-overpass plugin build, not a synthetic
// fixture - skipped when that build is absent (plugins/* is gitignored; see
// osmOverpassRealPluginWasm's doc comment) rather than failing a fresh
// checkout or CI.
func TestNewWasmPluginBase_SupportsPlaceNamesTrueWhenBothExportsPresent(t *testing.T) {
	if _, err := os.Stat(osmOverpassRealPluginWasm); err != nil {
		t.Skipf("osm-overpass.wasm not built (run the plugins-builder first): %v", err)
	}
	manifest := extism.Manifest{Wasm: []extism.Wasm{extism.WasmFile{Path: osmOverpassRealPluginWasm}}}

	base, err := newWasmPluginBase(manifest, "plugins/test")
	if err != nil {
		t.Fatalf("newWasmPluginBase failed: %v", err)
	}
	if !base.SupportsPlaceNames() {
		t.Fatalf("expected the osm-overpass plugin (exports both place_name_at and search_places) to support place names")
	}
}

// TestNewWasmPluginBase_SupportsPlaceNamesFalseWhenExportsAbsent uses the
// existing configecho fixture (no place_name_at/search_places at all) - the
// normal/default state every other plugin kind (tide, weather, wave, POI
// without place-name support) already represents.
func TestNewWasmPluginBase_SupportsPlaceNamesFalseWhenExportsAbsent(t *testing.T) {
	manifest := extism.Manifest{Wasm: []extism.Wasm{extism.WasmFile{Path: configEchoFixtureWasm}}}

	base, err := newWasmPluginBase(manifest, "plugins/test")
	if err != nil {
		t.Fatalf("newWasmPluginBase failed: %v", err)
	}
	if base.SupportsPlaceNames() {
		t.Fatalf("expected a plugin exporting neither place_name_at nor search_places to not support place names")
	}
}

func TestNewWasmPluginBase_DescriptionPresentIsCaptured(t *testing.T) {
	manifest := extism.Manifest{Wasm: []extism.Wasm{extism.WasmFile{Path: describedValidFixtureWasm}}}

	base, err := newWasmPluginBase(manifest, "plugins/test")
	if err != nil {
		t.Fatalf("newWasmPluginBase failed: %v", err)
	}

	if got, want := base.Description(), "A fixture plugin that exports a description."; got != want {
		t.Errorf("Description() = %q, want %q", got, want)
	}
}

// TestNewWasmPluginBase_DescriptionAbsentDefaultsToEmptyString proves that a
// plugin with no description() export at all (the normal/default case for
// older or minimal plugins) loads successfully with an empty description
// and no error - using an EXISTING unmodified fixture (configecho) so that
// fixture keeps exercising this "optional export absent" path exactly like
// it already does for other optional behavior.
func TestNewWasmPluginBase_DescriptionAbsentDefaultsToEmptyString(t *testing.T) {
	manifest := extism.Manifest{Wasm: []extism.Wasm{extism.WasmFile{Path: configEchoFixtureWasm}}}

	base, err := newWasmPluginBase(manifest, "plugins/test")
	if err != nil {
		t.Fatalf("newWasmPluginBase failed: %v", err)
	}

	if got := base.Description(); got != "" {
		t.Errorf("Description() = %q, want empty string for a plugin with no description() export", got)
	}
}

// TestNewWasmPluginBase_DescriptionCallErrorDefaultsToEmptyStringNoError
// proves that a description() export which fails WHEN CALLED (as opposed to
// being absent) is handled the same non-fatal way: newWasmPluginBase must
// still succeed, with an empty Description() - description() is purely
// cosmetic metadata and must never block plugin loading.
func TestNewWasmPluginBase_DescriptionCallErrorDefaultsToEmptyStringNoError(t *testing.T) {
	manifest := extism.Manifest{Wasm: []extism.Wasm{extism.WasmFile{Path: describedErrorFixtureWasm}}}

	base, err := newWasmPluginBase(manifest, "plugins/test")
	if err != nil {
		t.Fatalf("newWasmPluginBase failed: %v (a failing description() call must not fail plugin construction)", err)
	}

	if got := base.Description(); got != "" {
		t.Errorf("Description() = %q, want empty string when description() fails when called", got)
	}
}

// withTestGlobalSecretsStore points globalSecretsStore at a fresh
// t.TempDir()-backed store for the duration of the test, restoring the
// prior value afterwards. Mirrors withTestSecretsStore in
// secrets_settings_handlers_test.go but lives here too since this file's
// tests need the same swap for the wasm_plugin.go allowlist gate.
func withTestGlobalSecretsStore(t *testing.T) *secretsStore {
	t.Helper()
	dir := t.TempDir()
	store, err := newSecretsStore(filepath.Join(dir, "secrets.sqlite"), filepath.Join(dir, "secrets.key"))
	if err != nil {
		t.Fatalf("newSecretsStore: %v", err)
	}
	prev := globalSecretsStore
	globalSecretsStore = store
	t.Cleanup(func() { globalSecretsStore = prev })
	return store
}

// TestConfigForWasmPlugin_KnownSecretDroppedWithNoAllowedSecretsFile confirms
// a known-secret name (e.g. WEATHERKIT_KEY_ID) referenced in a plugin's
// config.json is dropped when there is no companion allowed_secrets.json -
// the safe default is "no secrets allowed" for a plugin that hasn't
// declared any, mirroring allowed_hosts.json's missing-file default.
func TestConfigForWasmPlugin_KnownSecretDroppedWithNoAllowedSecretsFile(t *testing.T) {
	withTestGlobalSecretsStore(t)
	if err := globalSecretsStore.Set("WEATHERKIT_KEY_ID", "the-real-key-id"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	dir := t.TempDir()
	wasmPath := filepath.Join(dir, "plugin.wasm")
	companion := filepath.Join(dir, "plugin.config.json")
	if err := os.WriteFile(companion, []byte(`{"key_id":"${WEATHERKIT_KEY_ID}"}`), 0o644); err != nil {
		t.Fatalf("write companion file: %v", err)
	}

	config, err := configForWasmPlugin(wasmPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := config["key_id"]; ok {
		t.Errorf("expected key_id to be dropped with no allowed_secrets.json, got %+v", config)
	}
}

// TestConfigForWasmPlugin_KnownSecretDroppedAndLoggedWhenNotInAllowedSecretsFile
// confirms a known-secret name is dropped (and a denial is logged) when
// allowed_secrets.json exists but does not list it.
func TestConfigForWasmPlugin_KnownSecretDroppedAndLoggedWhenNotInAllowedSecretsFile(t *testing.T) {
	withTestGlobalSecretsStore(t)
	if err := globalSecretsStore.Set("WEATHERKIT_KEY_ID", "the-real-key-id"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	dir := t.TempDir()
	wasmPath := filepath.Join(dir, "plugin.wasm")
	if err := os.WriteFile(filepath.Join(dir, "plugin.config.json"), []byte(`{"key_id":"${WEATHERKIT_KEY_ID}"}`), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	// allowed_secrets.json exists but lists a different key.
	if err := os.WriteFile(filepath.Join(dir, "plugin.allowed_secrets.json"), []byte(`["WEATHERKIT_TEAM_ID"]`), 0o644); err != nil {
		t.Fatalf("write allowed secrets file: %v", err)
	}

	var logBuf bytes.Buffer
	origOutput := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(origOutput)

	config, err := configForWasmPlugin(wasmPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := config["key_id"]; ok {
		t.Errorf("expected key_id to be dropped when not listed in allowed_secrets.json, got %+v", config)
	}
	if logBuf.Len() == 0 {
		t.Errorf("expected a denial to be logged when a secret is referenced but not in allowed_secrets.json")
	}
}

// TestConfigForWasmPlugin_KnownSecretResolvesFromStoreWhenAllowed confirms a
// known-secret name resolves correctly from globalSecretsStore when the
// plugin's allowed_secrets.json DOES list it and the store has the value.
func TestConfigForWasmPlugin_KnownSecretResolvesFromStoreWhenAllowed(t *testing.T) {
	withTestGlobalSecretsStore(t)
	if err := globalSecretsStore.Set("WEATHERKIT_KEY_ID", "the-real-key-id"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	dir := t.TempDir()
	wasmPath := filepath.Join(dir, "plugin.wasm")
	if err := os.WriteFile(filepath.Join(dir, "plugin.config.json"), []byte(`{"key_id":"${WEATHERKIT_KEY_ID}"}`), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.allowed_secrets.json"), []byte(`["WEATHERKIT_KEY_ID"]`), 0o644); err != nil {
		t.Fatalf("write allowed secrets file: %v", err)
	}

	config, err := configForWasmPlugin(wasmPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config["key_id"] != "the-real-key-id" {
		t.Errorf("expected key_id=the-real-key-id, got %+v", config)
	}
}

// TestConfigForWasmPlugin_HELMCENTRALMasterKeyIsNeverExpanded is the S-2
// security-audit finding: HELMCENTRAL_MASTER_KEY is NOT in knownSecretKeys
// (see secrets_store.go - deliberately so, it is not a storable secret, it
// is the key that decrypts every stored secret), so before this fix it fell
// straight through configForWasmPlugin's mapping closure to the raw
// os.LookupEnv branch every ordinary, non-secret env var reference uses. An
// operator who sets HELMCENTRAL_MASTER_KEY in the container environment
// (ADR 0023 §2's own documented option, for a secrets manager injecting it
// at container start) would hand the base64 master key to any plugin whose
// config.json referenced "${HELMCENTRAL_MASTER_KEY}" - no allowed_secrets.json
// entry required, no denial logged. This must be denied the same way a
// known secret without allowlist coverage is denied.
func TestConfigForWasmPlugin_HELMCENTRALMasterKeyIsNeverExpanded(t *testing.T) {
	t.Setenv("HELMCENTRAL_MASTER_KEY", "cGxhY2Vob2xkZXItbWFzdGVyLWtleS0zMi1ieXRlcyE=")

	dir := t.TempDir()
	wasmPath := filepath.Join(dir, "plugin.wasm")
	companion := filepath.Join(dir, "plugin.config.json")
	if err := os.WriteFile(companion, []byte(`{"x":"${HELMCENTRAL_MASTER_KEY}"}`), 0o644); err != nil {
		t.Fatalf("write companion file: %v", err)
	}

	config, err := configForWasmPlugin(wasmPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, ok := config["x"]; ok {
		t.Errorf("expected HELMCENTRAL_MASTER_KEY to never reach a plugin's config, got %q", v)
	}
}

// TestConfigForWasmPlugin_HELMCENTRALMasterKeyDenialIsLogged mirrors
// TestConfigForWasmPlugin_KnownSecretDroppedAndLoggedWhenNotInAllowedSecretsFile's
// proof that a denial is visible in the server log, not just silently
// absent from the result - an operator debugging "why is this config key
// missing" needs the same signal here as for any other denied secret.
func TestConfigForWasmPlugin_HELMCENTRALMasterKeyDenialIsLogged(t *testing.T) {
	t.Setenv("HELMCENTRAL_MASTER_KEY", "cGxhY2Vob2xkZXItbWFzdGVyLWtleS0zMi1ieXRlcyE=")

	dir := t.TempDir()
	wasmPath := filepath.Join(dir, "plugin.wasm")
	companion := filepath.Join(dir, "plugin.config.json")
	if err := os.WriteFile(companion, []byte(`{"x":"${HELMCENTRAL_MASTER_KEY}"}`), 0o644); err != nil {
		t.Fatalf("write companion file: %v", err)
	}

	var logBuf bytes.Buffer
	origOutput := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(origOutput)

	if _, err := configForWasmPlugin(wasmPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if logBuf.Len() == 0 {
		t.Errorf("expected a denial to be logged for a referenced HELMCENTRAL_MASTER_KEY")
	}
}

// TestAllowedSecretsForWasmPlugin_NoCompanionFileReturnsNilNoError mirrors
// allowedHostsForWasmPlugin's missing-file contract: no file is the safe
// default (no secrets allowed), not an error.
func TestAllowedSecretsForWasmPlugin_NoCompanionFileReturnsNilNoError(t *testing.T) {
	dir := t.TempDir()
	wasmPath := filepath.Join(dir, "plugin.wasm")

	keys, err := allowedSecretsForWasmPlugin(wasmPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("expected no allowed secrets, got %+v", keys)
	}
}

// TestAllowedSecretsForWasmPlugin_MalformedJSONIsError mirrors
// allowedHostsForWasmPlugin's malformed-file contract: a file that exists
// but isn't valid JSON is a hard error, not silently treated as empty.
func TestAllowedSecretsForWasmPlugin_MalformedJSONIsError(t *testing.T) {
	dir := t.TempDir()
	wasmPath := filepath.Join(dir, "plugin.wasm")
	if err := os.WriteFile(filepath.Join(dir, "plugin.allowed_secrets.json"), []byte(`{not valid json`), 0o644); err != nil {
		t.Fatalf("write allowed secrets file: %v", err)
	}

	if _, err := allowedSecretsForWasmPlugin(wasmPath); err == nil {
		t.Fatalf("expected an error for malformed allowed_secrets.json, got nil")
	}
}

// ── plugin-declared config fields (config_fields.json sidecar) ────────────
//
// A plugin's <name>.config_fields.json sidecar declares which config.json
// keys are operator-editable from the Settings provider modal ("plugins
// declare their own settings" - see docs/adr/0100). Unlike config.json's
// env/secret substitution (resolved once at plugin load), a declared
// field's operator-saved value (plugin_overrides_store.go's
// plugin_config_values table) is overlaid on every call
// (wasmPluginBase.call) via applyConfigValues, so a Settings save takes
// effect on the very next call with no restart. These tests cover sidecar
// parsing/validation at load time and the full compiled-plugin round trip
// proving a stored value actually reaches the guest and changes live
// between calls.

func TestPluginConfigFieldsForWasmPlugin_NoCompanionFileReturnsNilNoError(t *testing.T) {
	dir := t.TempDir()
	wasmPath := filepath.Join(dir, "plugin.wasm")

	fields, err := pluginConfigFieldsForWasmPlugin(wasmPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fields) != 0 {
		t.Errorf("expected no config fields, got %+v", fields)
	}
}

func TestPluginConfigFieldsForWasmPlugin_ValidSidecarParsed(t *testing.T) {
	dir := t.TempDir()
	wasmPath := filepath.Join(dir, "plugin.wasm")
	companion := filepath.Join(dir, "plugin.config_fields.json")
	body := `[{"key":"overpass_url","label":"Overpass server","type":"url","placeholder":"https://overpass-api.de/api/interpreter","help":"Blank uses the public overpass-api.de."}]`
	if err := os.WriteFile(companion, []byte(body), 0o644); err != nil {
		t.Fatalf("write companion file: %v", err)
	}

	fields, err := pluginConfigFieldsForWasmPlugin(wasmPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fields) != 1 {
		t.Fatalf("expected 1 field, got %+v", fields)
	}
	f := fields[0]
	if f.Key != "overpass_url" || f.Label != "Overpass server" || f.Type != "url" {
		t.Errorf("unexpected field: %+v", f)
	}
	if f.Placeholder != "https://overpass-api.de/api/interpreter" || f.Help != "Blank uses the public overpass-api.de." {
		t.Errorf("unexpected placeholder/help: %+v", f)
	}
}

func TestPluginConfigFieldsForWasmPlugin_MalformedJSONIsError(t *testing.T) {
	dir := t.TempDir()
	wasmPath := filepath.Join(dir, "plugin.wasm")
	if err := os.WriteFile(filepath.Join(dir, "plugin.config_fields.json"), []byte(`{not valid json`), 0o644); err != nil {
		t.Fatalf("write companion file: %v", err)
	}

	if _, err := pluginConfigFieldsForWasmPlugin(wasmPath); err == nil {
		t.Fatalf("expected an error for malformed config_fields.json")
	}
}

func TestPluginConfigFieldsForWasmPlugin_DuplicateKeyIsError(t *testing.T) {
	dir := t.TempDir()
	wasmPath := filepath.Join(dir, "plugin.wasm")
	body := `[{"key":"overpass_url","label":"A","type":"url"},{"key":"overpass_url","label":"B","type":"text"}]`
	if err := os.WriteFile(filepath.Join(dir, "plugin.config_fields.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write companion file: %v", err)
	}

	_, err := pluginConfigFieldsForWasmPlugin(wasmPath)
	if err == nil {
		t.Fatalf("expected an error for a duplicate key")
	}
	if !strings.Contains(err.Error(), "overpass_url") {
		t.Errorf("expected error to name the duplicate key, got: %v", err)
	}
}

func TestPluginConfigFieldsForWasmPlugin_EmptyKeyIsError(t *testing.T) {
	dir := t.TempDir()
	wasmPath := filepath.Join(dir, "plugin.wasm")
	body := `[{"key":"","label":"A","type":"text"}]`
	if err := os.WriteFile(filepath.Join(dir, "plugin.config_fields.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write companion file: %v", err)
	}

	if _, err := pluginConfigFieldsForWasmPlugin(wasmPath); err == nil {
		t.Fatalf("expected an error for an empty key")
	}
}

func TestPluginConfigFieldsForWasmPlugin_UnknownTypeIsError(t *testing.T) {
	dir := t.TempDir()
	wasmPath := filepath.Join(dir, "plugin.wasm")
	body := `[{"key":"overpass_url","label":"A","type":"number"}]`
	if err := os.WriteFile(filepath.Join(dir, "plugin.config_fields.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write companion file: %v", err)
	}

	_, err := pluginConfigFieldsForWasmPlugin(wasmPath)
	if err == nil {
		t.Fatalf("expected an error for an unknown type")
	}
	if !strings.Contains(err.Error(), "number") {
		t.Errorf("expected error to name the unknown type, got: %v", err)
	}
}

// A malformed config_fields.json must fail plugin load outright, the same
// "fail loudly on an author mistake" treatment every other malformed
// sidecar file gets in this package (allowed_hosts.json, config.json,
// allowed_secrets.json).
func TestNewWasmPluginBase_MalformedConfigFieldsFailsLoad(t *testing.T) {
	dir := t.TempDir()
	wasmBytes, err := os.ReadFile(configEchoFixtureWasm)
	if err != nil {
		t.Fatalf("read configecho fixture: %v", err)
	}
	wasmPath := filepath.Join(dir, "configecho.wasm")
	if err := os.WriteFile(wasmPath, wasmBytes, 0o644); err != nil {
		t.Fatalf("write configecho copy: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "configecho.config_fields.json"), []byte(`{not valid json`), 0o644); err != nil {
		t.Fatalf("write config_fields.json: %v", err)
	}

	manifest, err := manifestForWasmPlugin(wasmPath)
	if err != nil {
		t.Fatalf("manifestForWasmPlugin: %v", err)
	}
	if _, err := newWasmPluginBase(manifest, "plugins/test"); err == nil {
		t.Fatalf("expected newWasmPluginBase to fail loading a malformed config_fields.json")
	}
}

// withTestPluginConfigValuesStore points globalPluginOverridesStore at a
// fresh temp-dir-backed store for the duration of the test, restoring the
// previous value (typically nil) afterward, and returns it so the test can
// seed plugin_config_values rows directly.
func withTestPluginConfigValuesStore(t *testing.T) *pluginOverridesStore {
	t.Helper()
	store := newTestPluginOverridesStore(t)
	prev := globalPluginOverridesStore
	globalPluginOverridesStore = store
	t.Cleanup(func() { globalPluginOverridesStore = prev })
	return store
}

// newConfigEchoBaseWithDeclaredField copies the prebuilt configecho.wasm
// fixture into a fresh temp dir alongside a config_fields.json declaring
// "some_key" as an operator-editable field, and (when non-empty) a
// config.json giving it a load-time default, then constructs a real
// *wasmPluginBase through the normal manifestForWasmPlugin +
// newWasmPluginBase path (not a hand-built extism.Manifest), so
// newWasmPluginBase's own sidecar-validation call is exercised too.
func newConfigEchoBaseWithDeclaredField(t *testing.T, configJSONDefault string) *wasmPluginBase {
	t.Helper()
	dir := t.TempDir()
	wasmBytes, err := os.ReadFile(configEchoFixtureWasm)
	if err != nil {
		t.Fatalf("read configecho fixture: %v", err)
	}
	wasmPath := filepath.Join(dir, "configecho.wasm")
	if err := os.WriteFile(wasmPath, wasmBytes, 0o644); err != nil {
		t.Fatalf("write configecho copy: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "configecho.config_fields.json"), []byte(`[{"key":"some_key","label":"Some Key","type":"text"}]`), 0o644); err != nil {
		t.Fatalf("write config_fields.json: %v", err)
	}
	if configJSONDefault != "" {
		if err := os.WriteFile(filepath.Join(dir, "configecho.config.json"), []byte(configJSONDefault), 0o644); err != nil {
			t.Fatalf("write config.json: %v", err)
		}
	}

	manifest, err := manifestForWasmPlugin(wasmPath)
	if err != nil {
		t.Fatalf("manifestForWasmPlugin: %v", err)
	}
	base, err := newWasmPluginBase(manifest, "plugins/test")
	if err != nil {
		t.Fatalf("newWasmPluginBase: %v", err)
	}
	return base
}

func dumpConfigViaCall(t *testing.T, base *wasmPluginBase) map[string]string {
	t.Helper()
	out, err := base.call("dump_config", nil)
	if err != nil {
		t.Fatalf("dump_config call failed: %v", err)
	}
	var result map[string]string
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("unmarshal dump_config output: %v (raw: %s)", err, out)
	}
	return result
}

// TestWasmPluginBase_StoredConfigValueReachesGuest proves the end-to-end
// path: an operator-saved plugin_config_values row for a declared field
// reaches the guest on the very next call.
func TestWasmPluginBase_StoredConfigValueReachesGuest(t *testing.T) {
	store := withTestPluginConfigValuesStore(t)
	base := newConfigEchoBaseWithDeclaredField(t, "")
	if err := store.SetConfigValues(base.Path(), map[string]string{"some_key": "stored-value"}); err != nil {
		t.Fatalf("SetConfigValues: %v", err)
	}

	result := dumpConfigViaCall(t, base)
	if got := result["some_key"]; got != "stored-value" {
		t.Errorf("expected some_key to carry the stored value, got %+v", result)
	}
}

// TestWasmPluginBase_StoredConfigValueChangesBetweenCallsWithNoReload proves
// a Settings save takes effect on the very next call to an ALREADY-LOADED
// plugin - the entire point of resolving at call time instead of at load
// time.
func TestWasmPluginBase_StoredConfigValueChangesBetweenCallsWithNoReload(t *testing.T) {
	store := withTestPluginConfigValuesStore(t)
	base := newConfigEchoBaseWithDeclaredField(t, "")
	if err := store.SetConfigValues(base.Path(), map[string]string{"some_key": "first-value"}); err != nil {
		t.Fatalf("SetConfigValues: %v", err)
	}

	first := dumpConfigViaCall(t, base)
	if got := first["some_key"]; got != "first-value" {
		t.Fatalf("expected first call to carry the first value, got %+v", first)
	}

	if err := store.SetConfigValues(base.Path(), map[string]string{"some_key": "second-value"}); err != nil {
		t.Fatalf("SetConfigValues (update): %v", err)
	}

	updated := dumpConfigViaCall(t, base)
	if got := updated["some_key"]; got != "second-value" {
		t.Errorf("expected the SAME base's next call to pick up the new value with no reload, got %+v", updated)
	}
}

// TestWasmPluginBase_ConfigJSONValueUsedWhenNothingStored proves the normal,
// unconfigured case: with no plugin_config_values row at all, the guest
// still sees whatever config.json's own default provided.
func TestWasmPluginBase_ConfigJSONValueUsedWhenNothingStored(t *testing.T) {
	withTestPluginConfigValuesStore(t)
	base := newConfigEchoBaseWithDeclaredField(t, `{"some_key":"config-json-default"}`)

	result := dumpConfigViaCall(t, base)
	if got := result["some_key"]; got != "config-json-default" {
		t.Errorf("expected some_key to carry config.json's default, got %+v", result)
	}
}

// TestWasmPluginBase_StoredConfigValueDoesNotMutateSharedManifestConfig
// guards the specific bug applyConfigValues' doc comment warns about:
// overlaying a stored value for one call must not leak into the compiled
// plugin's shared manifest.Config, which every future instance's Config
// starts from (go-sdk's Instance() aliases it directly). Proven by driving
// two separate instances from the same *wasmPluginBase.compiled and
// checking a fresh, untouched instance never picks up the resolved value as
// a static baseline.
func TestWasmPluginBase_StoredConfigValueDoesNotMutateSharedManifestConfig(t *testing.T) {
	store := withTestPluginConfigValuesStore(t)
	base := newConfigEchoBaseWithDeclaredField(t, "")
	if err := store.SetConfigValues(base.Path(), map[string]string{"some_key": "stored-value"}); err != nil {
		t.Fatalf("SetConfigValues: %v", err)
	}

	_ = dumpConfigViaCall(t, base)

	// A fresh instance off the SAME compiled plugin, with no per-call
	// resolution applied (bypassing base.call), must not see the previous
	// call's resolved value baked into instance.Config - if applyConfigValues
	// had mutated the shared manifest map instead of a clone, this second,
	// untouched instance would.
	ctx := context.Background()
	instance, err := base.compiled.Instance(ctx, extism.PluginInstanceConfig{ModuleConfig: wasmModuleConfig()})
	if err != nil {
		t.Fatalf("Instance: %v", err)
	}
	defer instance.Close(ctx)

	if v, ok := instance.Config["some_key"]; ok {
		t.Errorf("expected the compiled plugin's own manifest Config to carry no baked-in some_key, got %q", v)
	}
}
