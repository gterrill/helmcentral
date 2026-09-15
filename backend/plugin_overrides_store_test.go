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

// ── plugin_config_values (plugin-declared settings) ─────────────────────────

func TestPluginOverridesStore_GetConfigValuesOnMissingRowsReturnsEmptyNoError(t *testing.T) {
	store := newTestPluginOverridesStore(t)

	values, err := store.GetConfigValues("plugins/poi/osm-overpass.wasm")
	if err != nil {
		t.Fatalf("GetConfigValues: %v", err)
	}
	if len(values) != 0 {
		t.Fatalf("expected no stored config values, got %+v", values)
	}
}

func TestPluginOverridesStore_SetConfigValuesThenGetRoundTrips(t *testing.T) {
	store := newTestPluginOverridesStore(t)
	path := "plugins/poi/osm-overpass.wasm"

	if err := store.SetConfigValues(path, map[string]string{"overpass_url": "https://overpass.openstreetmap.fr/api/interpreter"}); err != nil {
		t.Fatalf("SetConfigValues: %v", err)
	}

	values, err := store.GetConfigValues(path)
	if err != nil {
		t.Fatalf("GetConfigValues: %v", err)
	}
	if values["overpass_url"] != "https://overpass.openstreetmap.fr/api/interpreter" {
		t.Errorf("values = %+v, want overpass_url set to the mirror", values)
	}
}

// A blank value deletes the row rather than storing an empty string, so the
// field reverts to config.json's own default (or the plugin's built-in
// default) - the same "blank means unset" contract every other
// settings-shaped value in this app follows.
func TestPluginOverridesStore_SetConfigValuesBlankValueDeletesRow(t *testing.T) {
	store := newTestPluginOverridesStore(t)
	path := "plugins/poi/osm-overpass.wasm"

	if err := store.SetConfigValues(path, map[string]string{"overpass_url": "https://overpass.openstreetmap.fr/api/interpreter"}); err != nil {
		t.Fatalf("first SetConfigValues: %v", err)
	}
	if err := store.SetConfigValues(path, map[string]string{"overpass_url": ""}); err != nil {
		t.Fatalf("second SetConfigValues (blank): %v", err)
	}

	values, err := store.GetConfigValues(path)
	if err != nil {
		t.Fatalf("GetConfigValues: %v", err)
	}
	if _, ok := values["overpass_url"]; ok {
		t.Errorf("expected overpass_url row to be deleted by a blank value, got %+v", values)
	}
}

func TestPluginOverridesStore_SetConfigValuesOverwritesOnlyGivenKeys(t *testing.T) {
	store := newTestPluginOverridesStore(t)
	path := "plugins/poi/osm-overpass.wasm"

	if err := store.SetConfigValues(path, map[string]string{"overpass_url": "https://overpass.kumi.systems/api/interpreter", "other_key": "keep-me"}); err != nil {
		t.Fatalf("first SetConfigValues: %v", err)
	}
	if err := store.SetConfigValues(path, map[string]string{"overpass_url": "https://overpass.openstreetmap.fr/api/interpreter"}); err != nil {
		t.Fatalf("second SetConfigValues: %v", err)
	}

	values, err := store.GetConfigValues(path)
	if err != nil {
		t.Fatalf("GetConfigValues: %v", err)
	}
	if values["overpass_url"] != "https://overpass.openstreetmap.fr/api/interpreter" {
		t.Errorf("overpass_url = %q, want the updated mirror", values["overpass_url"])
	}
	if values["other_key"] != "keep-me" {
		t.Errorf("other_key = %q, want it left untouched by a save that didn't mention it", values["other_key"])
	}
}

func TestPluginOverridesStore_ConfigValuesKeyedByFullWasmPathNotID(t *testing.T) {
	store := newTestPluginOverridesStore(t)

	if err := store.SetConfigValues("plugins/poi/osm-overpass.wasm", map[string]string{"overpass_url": "https://overpass.kumi.systems/api/interpreter"}); err != nil {
		t.Fatalf("SetConfigValues poi/osm-overpass: %v", err)
	}
	if err := store.SetConfigValues("plugins/other/osm-overpass.wasm", map[string]string{"overpass_url": "https://overpass.openstreetmap.fr/api/interpreter"}); err != nil {
		t.Fatalf("SetConfigValues other/osm-overpass: %v", err)
	}

	poiValues, err := store.GetConfigValues("plugins/poi/osm-overpass.wasm")
	if err != nil {
		t.Fatalf("GetConfigValues poi: %v", err)
	}
	if poiValues["overpass_url"] != "https://overpass.kumi.systems/api/interpreter" {
		t.Errorf("poi overpass_url = %q, want the poi mirror", poiValues["overpass_url"])
	}
}

func TestPluginOverridesStore_DeleteConfigValuesClearsAllKeys(t *testing.T) {
	store := newTestPluginOverridesStore(t)
	path := "plugins/poi/osm-overpass.wasm"

	if err := store.SetConfigValues(path, map[string]string{"overpass_url": "https://overpass.kumi.systems/api/interpreter", "other_key": "value"}); err != nil {
		t.Fatalf("SetConfigValues: %v", err)
	}
	if err := store.DeleteConfigValues(path); err != nil {
		t.Fatalf("DeleteConfigValues: %v", err)
	}

	values, err := store.GetConfigValues(path)
	if err != nil {
		t.Fatalf("GetConfigValues: %v", err)
	}
	if len(values) != 0 {
		t.Errorf("expected no config values after DeleteConfigValues, got %+v", values)
	}
}

func TestPluginOverridesStore_DeleteConfigValuesOnMissingRowsIsNotAnError(t *testing.T) {
	store := newTestPluginOverridesStore(t)

	if err := store.DeleteConfigValues("plugins/poi/never-set.wasm"); err != nil {
		t.Fatalf("DeleteConfigValues on a never-set path should be a no-op, got error: %v", err)
	}
}
