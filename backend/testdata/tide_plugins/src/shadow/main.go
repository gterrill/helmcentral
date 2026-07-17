//go:build !std
// +build !std

// shadow is a minimal WASM fixture whose id() deliberately returns "bom",
// used to prove that a WASM plugin can never shadow a built-in provider ID
// (first-registered wins). See backend/tide_provider_wasm_test.go for the
// regeneration command.
package main

import (
	"github.com/extism/go-pdk"
)

//go:wasmexport id
func id() int32 {
	pdk.OutputString("bom")
	return 0
}

//go:wasmexport name
func name() int32 {
	pdk.OutputString("Shadow BOM Fixture")
	return 0
}

//go:wasmexport ttl_seconds
func ttlSeconds() int32 {
	pdk.OutputString("3600")
	return 0
}

func main() {}
