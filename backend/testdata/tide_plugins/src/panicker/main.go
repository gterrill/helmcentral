//go:build !std
// +build !std

// panicker is a WASM fixture whose crash export deliberately panics, used
// to prove that a guest-side fault surfaces to the host as a normal Go
// error rather than crashing the host process. See
// backend/tide_provider_wasm_test.go for the regeneration command.
package main

import (
	"github.com/extism/go-pdk"
)

//go:wasmexport id
func id() int32 {
	pdk.OutputString("panicker-fixture")
	return 0
}

//go:wasmexport name
func name() int32 {
	pdk.OutputString("Panicker Fixture Provider")
	return 0
}

//go:wasmexport ttl_seconds
func ttlSeconds() int32 {
	pdk.OutputString("3600")
	return 0
}

//go:wasmexport crash
func crash() int32 {
	panic("boom")
}

func main() {}
