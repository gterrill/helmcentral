//go:build !std
// +build !std

// configecho is a minimal WASM fixture proving the plugin config/secrets
// channel (manifestForWasmPlugin's Config, configForWasmPlugin's companion
// <name>.config.json file) actually reaches the guest end-to-end, not just
// the host-side manifest-building unit tests. It implements the universal
// id()/name()/ttl_seconds() contract trivially, plus a dump_config export
// that echoes back a couple of known config keys via pdk.GetConfig, as JSON,
// so the host test can assert the round trip.
//
// This fixture is also the first consumer of the go.mod shared across all
// backend/testdata/wasm_plugins/src/<name>/ fixtures (the same layout
// backend/testdata/tide_plugins/src uses) - see
// backend/wasm_plugin_test.go's regeneration comment for the build command.
package main

import (
	"github.com/extism/go-pdk"
)

//go:wasmexport id
func id() int32 {
	pdk.OutputString("configecho-fixture")
	return 0
}

//go:wasmexport name
func name() int32 {
	pdk.OutputString("Config Echo Fixture")
	return 0
}

//go:wasmexport ttl_seconds
func ttlSeconds() int32 {
	pdk.OutputString("3600")
	return 0
}

// dumpConfig echoes back a small, fixed set of known config keys as a JSON
// object, using pdk.GetConfig for each. A key that isn't present in the
// manifest's config is simply omitted from the output (mirrors
// pdk.GetConfig's own (string, bool) "present?" contract).
//
//go:wasmexport dump_config
func dumpConfig() int32 {
	result := map[string]string{}
	for _, key := range []string{"some_key", "another_key"} {
		if v, ok := pdk.GetConfig(key); ok {
			result[key] = v
		}
	}

	if err := pdk.OutputJSON(result); err != nil {
		pdk.SetError(err)
		return -1
	}
	return 0
}

func main() {}
