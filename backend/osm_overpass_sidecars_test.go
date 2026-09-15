// osm_overpass_sidecars_test.go proves the shipped osm-overpass POI plugin
// sidecars (docs/examples/poi-plugins/osm-overpass/) declare the Overpass
// mirror as the plugin's OWN operator-editable config field ("plugins
// declare their own settings" - see docs/adr/0100), not a global app
// setting, and that the plugin's allowlist still carries every host it
// needs.
//
// pluginConfigFieldsForWasmPlugin and allowedHostsForWasmPlugin only ever
// read the companion *.config_fields.json / *.allowed_hosts.json files next
// to a wasmPath - see their doc comments in wasm_plugin.go - so the
// osm-overpass.wasm file itself need not exist on disk for these tests;
// only the sidecars matter.
package main

import (
	"testing"
)

// osmOverpassWasmPath is the real shipped wasmPath for the bundled
// osm-overpass POI plugin, resolved relative to this package's directory
// (backend/), which is `go test`'s working directory. The .wasm file need
// not exist on disk (see package doc comment above); only its sidecars
// (osm-overpass.config_fields.json, osm-overpass.allowed_hosts.json) are
// read.
const osmOverpassWasmPath = "../docs/examples/poi-plugins/osm-overpass/osm-overpass.wasm"

// withNilPluginOverridesStore forces globalPluginOverridesStore to nil for
// the duration of the test, restoring whatever it was afterwards.
// allowedHostsForWasmPlugin consults that store before falling back to the
// on-disk companion files (wasm_plugin.go), and this test is exercising the
// on-disk sidecar files, not a saved override.
func withNilPluginOverridesStore(t *testing.T) {
	t.Helper()
	prev := globalPluginOverridesStore
	globalPluginOverridesStore = nil
	t.Cleanup(func() { globalPluginOverridesStore = prev })
}

// TestOsmOverpassSidecars_ConfigFieldsDeclaresOverpassURL proves the shipped
// config_fields.json (docs/examples/poi-plugins/osm-overpass/
// osm-overpass.config_fields.json) declares "overpass_url" as an
// operator-editable field of type "url" - the sidecar the Settings provider
// modal reads to render the field, and wasmPluginBase.call's
// applyConfigValues overlay reads to know which config.json keys an
// operator-saved value is allowed to replace.
func TestOsmOverpassSidecars_ConfigFieldsDeclaresOverpassURL(t *testing.T) {
	withNilPluginOverridesStore(t)

	fields, err := pluginConfigFieldsForWasmPlugin(osmOverpassWasmPath)
	if err != nil {
		t.Fatalf("pluginConfigFieldsForWasmPlugin: %v", err)
	}
	if len(fields) != 2 {
		t.Fatalf("expected exactly 2 declared config fields (overpass_url, detail_limit), got %+v", fields)
	}
	f := fieldByKey(t, fields, "overpass_url")
	if f.Type != "url" {
		t.Errorf("expected type url, got %q", f.Type)
	}
	if f.Label == "" {
		t.Errorf("expected a non-empty label")
	}
}

// TestOsmOverpassSidecars_ConfigFieldsDeclaresDetailLimit proves the shipped
// config_fields.json also declares "detail_limit" - how many nearest
// results get an extra Wikipedia summary fetched (osm-overpass.go's
// resolveDetailLimit) - as an operator-editable field. Its type is "text",
// not "url": the host only format-validates "url"-typed fields at save time
// (postPluginConfigHandler), so a bad detail_limit value is caught instead
// at read time by resolveDetailLimit itself (falls back to the default of
// 5), never at save time.
func TestOsmOverpassSidecars_ConfigFieldsDeclaresDetailLimit(t *testing.T) {
	withNilPluginOverridesStore(t)

	fields, err := pluginConfigFieldsForWasmPlugin(osmOverpassWasmPath)
	if err != nil {
		t.Fatalf("pluginConfigFieldsForWasmPlugin: %v", err)
	}
	f := fieldByKey(t, fields, "detail_limit")
	if f.Type != "text" {
		t.Errorf("expected type text, got %q", f.Type)
	}
	if f.Label == "" {
		t.Errorf("expected a non-empty label")
	}
}

// fieldByKey finds the declared config field with the given key, failing
// the test outright if it isn't present - every test in this file expects
// its field to exist, never merely tolerates its absence.
func fieldByKey(t *testing.T, fields []pluginConfigFieldSpec, key string) pluginConfigFieldSpec {
	t.Helper()
	for _, f := range fields {
		if f.Key == key {
			return f
		}
	}
	t.Fatalf("expected a declared config field with key %q, got %+v", key, fields)
	return pluginConfigFieldSpec{}
}

// TestOsmOverpassSidecars_NoConfigJSONShipped confirms the plugin no longer
// ships an osm-overpass.config.json sidecar at all - it had exactly one key
// (overpass_url), and that key moved entirely to config_fields.json /
// plugin_config_values (ADR 0100 rewrite), leaving nothing left for
// config.json to carry.
func TestOsmOverpassSidecars_NoConfigJSONShipped(t *testing.T) {
	withNilPluginOverridesStore(t)

	config, err := configForWasmPlugin(osmOverpassWasmPath)
	if err != nil {
		t.Fatalf("configForWasmPlugin: %v", err)
	}
	if len(config) != 0 {
		t.Errorf("expected no load-time config (config.json removed), got %+v", config)
	}
}

// TestOsmOverpassSidecars_AllowlistKeepsExistingHosts proves the shipped
// allowlist still carries every host this plugin needs: overpass-api.de
// (the built-in default endpoint), overpass.openstreetmap.fr (the
// documented mirror choice), and en.wikipedia.org (detail enrichment).
func TestOsmOverpassSidecars_AllowlistKeepsExistingHosts(t *testing.T) {
	withNilPluginOverridesStore(t)

	hosts, err := allowedHostsForWasmPlugin(osmOverpassWasmPath)
	if err != nil {
		t.Fatalf("allowedHostsForWasmPlugin: %v", err)
	}
	for _, want := range []string{"overpass-api.de", "overpass.openstreetmap.fr", "en.wikipedia.org"} {
		if !containsString(hosts, want) {
			t.Errorf("expected allowed hosts %+v to still contain %q", hosts, want)
		}
	}
}
