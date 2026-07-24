//go:build !std
// +build !std

// describedvalid is a minimal WASM fixture proving that an optional
// description() export, when present and successful, is captured by
// newWasmPluginBase (backend/wasm_plugin.go) exactly like ttl_seconds()
// already is. It otherwise mirrors the existing "valid" fixture's shape
// (backend/testdata/tide_plugins/src/valid/main.go) but only implements the
// universal id/name/ttl_seconds/description contract - no
// provider-type-specific exports are needed for this test. See
// backend/wasm_plugin_test.go for the regeneration command.
package main

import (
	"github.com/extism/go-pdk"
)

//go:wasmexport id
func id() int32 {
	pdk.OutputString("described-valid-fixture")
	return 0
}

//go:wasmexport name
func name() int32 {
	pdk.OutputString("Described Valid Fixture Provider")
	return 0
}

//go:wasmexport ttl_seconds
func ttlSeconds() int32 {
	pdk.OutputString("3600")
	return 0
}

//go:wasmexport description
func description() int32 {
	pdk.OutputString("A fixture plugin that exports a description.")
	return 0
}

func main() {}
