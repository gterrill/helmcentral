//go:build !std
// +build !std

// ftpfetchspike is a disposable feasibility spike, NOT a real plugin. It
// exists only to answer one question empirically: can a TinyGo 0.41.1
// wasip1 guest, via a hand-written //go:wasmimport declaration, call a
// custom (non-built-in) Extism host function and get real bytes back -
// specifically, bytes fetched by the HOST over anonymous FTP, since a WASM
// guest cannot open raw sockets itself and Extism's own built-in
// pdk.NewHTTPRequest only speaks HTTP?
//
// This is the hard technical gate for the planned "Forecast Warnings"
// plugin type's BOM default implementation, which can only reliably fetch
// its bulletins over BOM's anonymous FTP mirror (their HTTP surface
// actively bot-blocks scraping). See backend/wasm_ftp_spike_test.go for the
// host-side round trip that proves (or disproves) this against REAL,
// currently-live BOM data - not a mock.
//
// The host function contract (mirrored exactly by the host-side test):
//
//	request:  {"host": "ftp.bom.gov.au:21", "path": "/anon/gen/fwo/IDQ20085.txt"}
//	response: {"body": "<file contents>", "error": ""}
package main

import (
	"encoding/json"

	"github.com/extism/go-pdk"
)

// ftpFetch is the hand-written low-level import for the host's "ftp_fetch"
// custom host function, registered by the host-side test under the
// default Extism host-function namespace "extism:host/user" (the go-sdk
// package's NewHostFunctionWithStack default, unless a caller explicitly
// calls SetNamespace) - matched here by the //go:wasmimport module name.
// The single uint64 param/return pair is a WASM memory offset into shared
// Extism-managed guest memory, per go-pdk's documented custom-host-function
// pattern (README.md, "Host Functions" section): the guest allocates its
// JSON request via pdk.AllocateJSON, passes the offset across, and the
// host hands back a new offset holding its JSON response.
//
//go:wasmimport extism:host/user ftp_fetch
func ftpFetch(uint64) uint64

type ftpFetchRequest struct {
	Host string `json:"host"`
	Path string `json:"path"`
}

type ftpFetchResponse struct {
	Body  string `json:"body"`
	Error string `json:"error"`
}

// fetch calls the ftp_fetch host function with a fixed, real, currently-live
// BOM product request (the QLD Marine Wind Warning Summary bulletin,
// publicly served over BOM's own sanctioned anonymous FTP mirror) and
// returns the response body as the guest's output. A non-empty error in the
// host's response is surfaced via pdk.SetErrorString rather than treated as
// success - this spike deliberately does not test the disallowed-host panic
// path (that's a later phase's job), only the ordinary success/fetch-failure
// paths.
//
//go:wasmexport fetch
func fetch() int32 {
	req := ftpFetchRequest{
		Host: "ftp.bom.gov.au:21",
		Path: "/anon/gen/fwo/IDQ20085.txt",
	}

	reqMem, err := pdk.AllocateJSON(req)
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

	if resp.Error != "" {
		pdk.SetErrorString(resp.Error)
		return -1
	}

	pdk.OutputString(resp.Body)
	return 0
}

func main() {}
