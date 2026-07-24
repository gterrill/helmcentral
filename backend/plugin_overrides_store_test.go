package main

import (
	"path/filepath"
	"reflect"
	"testing"
)

func newTestPluginOverridesStore(t *testing.T) *pluginOverridesStore {
	t.Helper()
	dir := t.TempDir()
	store, err := newPluginOverridesStore(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatalf("newPluginOverridesStore: %v", err)
	}
	t.Cleanup(func() { _ = store.db.Close() })
	return store
}

func TestPluginOverridesStore_GetOnMissingRowReturnsNotOkNoError(t *testing.T) {
	store := newTestPluginOverridesStore(t)

	hosts, secrets, ok, err := store.Get("plugins/tides/bom.wasm")
	if err != nil {
		t.Fatalf("Get returned error for missing row: %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false for a path with no stored override, got hosts=%v secrets=%v", hosts, secrets)
	}
}

func TestPluginOverridesStore_SetThenGetRoundTrips(t *testing.T) {
	store := newTestPluginOverridesStore(t)

	wantHosts := []string{"api.example.com", "cdn.example.com"}
	wantSecrets := []string{"WEATHERKIT_KEY_ID"}

	if err := store.Set("plugins/weather/weatherkit.wasm", wantHosts, wantSecrets); err != nil {
		t.Fatalf("Set: %v", err)
	}

	gotHosts, gotSecrets, ok, err := store.Get("plugins/weather/weatherkit.wasm")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true after Set")
	}
	if !reflect.DeepEqual(gotHosts, wantHosts) {
		t.Errorf("hosts = %v, want %v", gotHosts, wantHosts)
	}
	if !reflect.DeepEqual(gotSecrets, wantSecrets) {
		t.Errorf("secrets = %v, want %v", gotSecrets, wantSecrets)
	}
}

func TestPluginOverridesStore_SetOverwritesBothArraysTogether(t *testing.T) {
	store := newTestPluginOverridesStore(t)
	path := "plugins/tides/bom.wasm"

	if err := store.Set(path, []string{"old.example.com"}, []string{"OLD_SECRET"}); err != nil {
		t.Fatalf("first Set: %v", err)
	}
	if err := store.Set(path, []string{"new.example.com"}, []string{"NEW_SECRET"}); err != nil {
		t.Fatalf("second Set: %v", err)
	}

	hosts, secrets, ok, err := store.Get(path)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if !reflect.DeepEqual(hosts, []string{"new.example.com"}) {
		t.Errorf("hosts = %v, want [new.example.com] (overwritten, not merged)", hosts)
	}
	if !reflect.DeepEqual(secrets, []string{"NEW_SECRET"}) {
		t.Errorf("secrets = %v, want [NEW_SECRET] (overwritten, not merged)", secrets)
	}
}

func TestPluginOverridesStore_DeleteThenGetReturnsNotOk(t *testing.T) {
	store := newTestPluginOverridesStore(t)
	path := "plugins/tides/bom.wasm"

	if err := store.Set(path, []string{"api.example.com"}, nil); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := store.Delete(path); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, _, ok, err := store.Get(path)
	if err != nil {
		t.Fatalf("Get after Delete: %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false after Delete")
	}
}

func TestPluginOverridesStore_DeleteOnMissingRowIsNotAnError(t *testing.T) {
	store := newTestPluginOverridesStore(t)

	if err := store.Delete("plugins/tides/never-set.wasm"); err != nil {
		t.Fatalf("Delete on a never-set path should be a no-op, got error: %v", err)
	}
}

// TestPluginOverridesStore_KeyedByFullWasmPathNotID proves the store is
// keyed by the full wasm file path, not (type, id) - two plugins with the
// same self-reported id in different domains (e.g. the BOM tide plugin and
// the BOM forecast-warnings plugin both report id "bom") must not collide.
func TestPluginOverridesStore_KeyedByFullWasmPathNotID(t *testing.T) {
	store := newTestPluginOverridesStore(t)

	if err := store.Set("plugins/tides/bom.wasm", []string{"www.bom.gov.au"}, nil); err != nil {
		t.Fatalf("Set tides/bom: %v", err)
	}
	if err := store.Set("plugins/forecast-warnings/bom.wasm", []string{"ftp.bom.gov.au"}, nil); err != nil {
		t.Fatalf("Set forecast-warnings/bom: %v", err)
	}

	tideHosts, _, ok, err := store.Get("plugins/tides/bom.wasm")
	if err != nil || !ok {
		t.Fatalf("Get tides/bom: ok=%v err=%v", ok, err)
	}
	if !reflect.DeepEqual(tideHosts, []string{"www.bom.gov.au"}) {
		t.Errorf("tides/bom hosts = %v, want [www.bom.gov.au]", tideHosts)
	}

	warningsHosts, _, ok, err := store.Get("plugins/forecast-warnings/bom.wasm")
	if err != nil || !ok {
		t.Fatalf("Get forecast-warnings/bom: ok=%v err=%v", ok, err)
	}
	if !reflect.DeepEqual(warningsHosts, []string{"ftp.bom.gov.au"}) {
		t.Errorf("forecast-warnings/bom hosts = %v, want [ftp.bom.gov.au]", warningsHosts)
	}
}
