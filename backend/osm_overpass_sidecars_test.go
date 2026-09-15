// osm_overpass_sidecars_test.go proves the shipped osm-overpass POI plugin
// sidecars (docs/examples/poi-plugins/osm-overpass/) actually wire
// OVERPASS_API_URL through to the plugin's "overpass_url" config key, and
// that the mirror they point at by default is allowlisted.
//
// configForWasmPlugin and allowedHostsForWasmPlugin only ever read the
// companion *.config.json / *.allowed_hosts.json files next to a wasmPath -
// see their doc comments in wasm_plugin.go - so the osm-overpass.wasm file
// itself need not exist on disk for these tests; only the sidecars matter.
package main

import (
	"net/url"
	"os"
	"testing"
)

// osmOverpassWasmPath is the real shipped wasmPath for the bundled
// osm-overpass POI plugin, resolved relative to this package's directory
// (backend/), which is `go test`'s working directory. The .wasm file need
// not exist on disk (see package doc comment above); only its sidecars
// (osm-overpass.config.json, osm-overpass.allowed_hosts.json) are read.
const osmOverpassWasmPath = "../docs/examples/poi-plugins/osm-overpass/osm-overpass.wasm"

// withNilPluginOverridesStore forces globalPluginOverridesStore to nil for
// the duration of the test, restoring whatever it was afterwards. Both
// configForWasmPlugin and allowedHostsForWasmPlugin consult that store
// before falling back to the on-disk companion files (wasm_plugin.go), and
// this test is exercising the on-disk sidecar files, not a saved override.
func withNilPluginOverridesStore(t *testing.T) {
	t.Helper()
	prev := globalPluginOverridesStore
	globalPluginOverridesStore = nil
	t.Cleanup(func() { globalPluginOverridesStore = prev })
}

// TestOsmOverpassSidecars_OverpassAPIURLSetFlowsIntoConfigAndAllowlist
// proves the two changes this ships work together: with OVERPASS_API_URL
// set to the boat's mirror, configForWasmPlugin resolves the shipped
// osm-overpass.config.json's "${OVERPASS_API_URL}" reference to that exact
// URL, and the URL's hostname is present in the shipped
// osm-overpass.allowed_hosts.json - so the plugin can actually reach it
// rather than being pointed at a host the sandbox then refuses.
func TestOsmOverpassSidecars_OverpassAPIURLSetFlowsIntoConfigAndAllowlist(t *testing.T) {
	withNilPluginOverridesStore(t)
	const mirror = "https://overpass.openstreetmap.fr/api/interpreter"
	t.Setenv("OVERPASS_API_URL", mirror)

	config, err := configForWasmPlugin(osmOverpassWasmPath)
	if err != nil {
		t.Fatalf("configForWasmPlugin: %v", err)
	}
	if got := config["overpass_url"]; got != mirror {
		t.Fatalf("expected overpass_url=%q, got %+v", mirror, config)
	}

	hosts, err := allowedHostsForWasmPlugin(osmOverpassWasmPath)
	if err != nil {
		t.Fatalf("allowedHostsForWasmPlugin: %v", err)
	}
	mirrorHost := mustHost(t, mirror)
	if !containsString(hosts, mirrorHost) {
		t.Errorf("expected allowed hosts %+v to contain mirror host %q", hosts, mirrorHost)
	}
}

// TestOsmOverpassSidecars_OverpassAPIURLUnsetDropsConfigKey proves the
// normal, unconfigured case: with OVERPASS_API_URL unset, the shipped
// config.json's "${OVERPASS_API_URL}" reference resolves to nothing and the
// whole "overpass_url" key is dropped from the returned config (per
// configForWasmPlugin's documented "unset env var drops the key" contract),
// leaving the plugin to fall back to its own built-in default
// (overpass-api.de) rather than being handed an empty string.
func TestOsmOverpassSidecars_OverpassAPIURLUnsetDropsConfigKey(t *testing.T) {
	withNilPluginOverridesStore(t)
	t.Setenv("OVERPASS_API_URL", "placeholder")
	os.Unsetenv("OVERPASS_API_URL")

	config, err := configForWasmPlugin(osmOverpassWasmPath)
	if err != nil {
		t.Fatalf("configForWasmPlugin: %v", err)
	}
	if _, ok := config["overpass_url"]; ok {
		t.Errorf("expected no overpass_url key with OVERPASS_API_URL unset, got %+v", config)
	}
}

// TestOsmOverpassSidecars_AllowlistKeepsExistingHosts proves the allowlist
// edit that adds the mirror host is additive: the two hosts the plugin
// already needed - overpass-api.de (the built-in default endpoint) and
// en.wikipedia.org (detail enrichment) - are still present.
func TestOsmOverpassSidecars_AllowlistKeepsExistingHosts(t *testing.T) {
	withNilPluginOverridesStore(t)

	hosts, err := allowedHostsForWasmPlugin(osmOverpassWasmPath)
	if err != nil {
		t.Fatalf("allowedHostsForWasmPlugin: %v", err)
	}
	for _, want := range []string{"overpass-api.de", "en.wikipedia.org"} {
		if !containsString(hosts, want) {
			t.Errorf("expected allowed hosts %+v to still contain %q", hosts, want)
		}
	}
}

func mustHost(t *testing.T, rawURL string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", rawURL, err)
	}
	return parsed.Hostname()
}
