//go:build !std
// +build !std

// describederror is a minimal WASM fixture proving that a description()
// export which fails when CALLED (as opposed to being absent entirely) is
// handled by newWasmPluginBase (backend/wasm_plugin.go) the same
// non-fatal way: the failure is logged and the plugin still loads with an
// empty description, mirroring the existing "panicker" fixture's proof for
// a guest-side fault on any other export
// (backend/testdata/tide_plugins/src/panicker/main.go). See
// backend/wasm_plugin_test.go for the regeneration command.
package main

import (
	"github.com/extism/go-pdk"
)

//go:wasmexport id
func id() int32 {
	pdk.OutputString("described-error-fixture")
	return 0
}

//go:wasmexport name
func name() int32 {
	pdk.OutputString("Described Error Fixture Provider")
	return 0
}

//go:wasmexport ttl_seconds
func ttlSeconds() int32 {
	pdk.OutputString("3600")
	return 0
}

//go:wasmexport description
func description() int32 {
	panic("description boom")
}

func main() {}
