//go:build !std
// +build !std

// ftpfetch is a real (non-spike) WASM fixture proving the generic ftp_fetch
// host function wired into wasm_plugin.go's newWasmPluginBase - not just
// wasm_ftp_spike_test.go's disposable ad-hoc manifest - actually works
// end-to-end through a plugin that satisfies the universal id()/name()/
// ttl_seconds() contract every real plugin type must implement.
//
// Unlike ftpfetchspike (which hardcodes a single fixed BOM request so it can
// stay a minimal, permanent feasibility contract test), this fixture takes
// {"host": "...", "path": "..."} as its fetch_ftp input, so the SAME
// compiled .wasm can be reused by the host-side test suite to exercise both
// the allowed-host success path and the disallowed-host path (varying only
// the manifest's AllowedHosts and/or the input host between test cases,
// without recompiling).
//
// See backend/wasm_ftp_fetch_test.go for the host-side tests and
// backend/wasm_ftp_spike_test.go's header comment for the shared fixture
// regeneration command/pattern this fixture also follows.
package main

import (
	"encoding/json"

	"github.com/extism/go-pdk"
)

// ftpFetch is the hand-written low-level import for the host's "ftp_fetch"
// custom host function - see ftpfetchspike/main.go's identical declaration
// for the full explanation of the //go:wasmimport contract.
//
//go:wasmimport extism:host/user ftp_fetch
func ftpFetch(uint64) uint64

type ftpFetchInput struct {
	Host string `json:"host"`
	Path string `json:"path"`
}

type ftpFetchResponse struct {
	Body  string `json:"body"`
	Error string `json:"error"`
}

//go:wasmexport id
func id() int32 {
	pdk.OutputString("ftpfetch-fixture")
	return 0
}

//go:wasmexport name
func name() int32 {
	pdk.OutputString("FTP Fetch Fixture")
	return 0
}

//go:wasmexport ttl_seconds
func ttlSeconds() int32 {
	pdk.OutputString("3600")
	return 0
}

// fetchFtp reads {"host","path"} from the guest's input, calls the host's
// ftp_fetch import, and outputs the host's {"body","error"} response
// verbatim as this call's own output JSON - so the host-side test can
// inspect either field directly, without needing a second guest export for
// the error case. A disallowed host doesn't reach this far: the host
// function panics before returning control to the guest at all, which
// surfaces to the host-side caller as an instance.Call error, not as guest
// output.
//
//go:wasmexport fetch_ftp
func fetchFtp() int32 {
	var input ftpFetchInput
	if err := pdk.InputJSON(&input); err != nil {
		pdk.SetError(err)
		return -1
	}

	reqMem, err := pdk.AllocateJSON(input)
	if err != nil {
		pdk.SetError(err)
		return -1
	}
	defer reqMem.Free()

	respOffset := ftpFetch(reqMem.Offset())
	respMem := pdk.FindMemory(respOffset)

	var resp ftpFetchResponse
	if err := json.Unmarshal(respMem.ReadBytes(), &resp); err != nil {
		pdk.SetError(err)
		return -1
	}

	if err := pdk.OutputJSON(resp); err != nil {
		pdk.SetError(err)
		return -1
	}
	return 0
}

func main() {}
