//go:build !std
// +build !std

// httpfetch is a WASM fixture used to test the host's Extism HTTP
// containment: AllowedHosts enforcement and Manifest.Timeout enforcement.
// Its probe_url export takes a target URL as raw string input (rather than
// a hardcoded URL) so the same compiled binary can be pointed at an
// httptest.Server's dynamically-assigned address at test time. See
// backend/tide_provider_wasm_test.go for the regeneration command.
package main

import (
	"github.com/extism/go-pdk"
)

//go:wasmexport id
func id() int32 {
	pdk.OutputString("httpfetch-fixture")
	return 0
}

//go:wasmexport name
func name() int32 {
	pdk.OutputString("HTTP Fetch Fixture Provider")
	return 0
}

//go:wasmexport ttl_seconds
func ttlSeconds() int32 {
	pdk.OutputString("3600")
	return 0
}

//go:wasmexport probe_url
func probeURL() int32 {
	url := string(pdk.Input())
	if url == "" {
		pdk.SetErrorString("missing url")
		return -1
	}

	req := pdk.NewHTTPRequest(pdk.MethodGet, url)
	res := req.Send()

	if res.Status() < 200 || res.Status() >= 300 {
		pdk.SetErrorString("upstream returned non-2xx status")
		return -1
	}

	pdk.OutputMemory(res.Memory())
	return 0
}

func main() {}
