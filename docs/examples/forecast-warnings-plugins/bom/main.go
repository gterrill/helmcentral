//go:build tinygo
// +build tinygo

// bom is Helmcentral's Bureau of Meteorology (Australia) forecast-warnings
// WASM plugin - the default ("bom") implementation of the pluggable
// Forecast Warnings provider contract (backend/forecast_warnings_providers.go).
// It is a port, not a new implementation: same state/zone resolution, same
// BOM FTP product IDs, same text-parsing rules as the formerly-hardcoded
// backend/bom_marine_warnings.go + backend/bom_marine_zones.go (still
// present until a later phase deletes them once this plugin replaces them).
//
// Unlike every other WASM plugin in this repo, this one does NOT use
// Extism's built-in HTTP support (pdk.NewHTTPRequest) for its network
// access. BOM's warnings text is only reliably available over anonymous
// FTP (ftp.bom.gov.au) - BOM's own HTTP surface actively bot-blocks
// scraping of the same product text ("does not support web scraping"),
// live-tested during planning. A WASM guest cannot open a raw FTP socket
// itself, so this plugin instead calls Helmcentral's custom "ftp_fetch"
// host function (backend/wasm_ftp_fetch.go), generically available to every
// plugin type via newWasmPluginBase. See docs/adr/0019-ftp-host-function-and-forecast-warnings-provider.md
// for the full rationale and security model, and README.md for this
// plugin's own notes on why it's unusual in this respect.
//
// This file (main.go) holds only the thin //go:wasmexport wrapper layer and
// the hand-written ftp_fetch host-function import; all the actual
// parsing/zone-resolution/orchestration logic lives in bom.go and
// bom_zones.go, neither of which depends on "github.com/extism/go-pdk" so
// they (and main_test.go) can be built and tested with the plain host Go
// toolchain (`go test ./...`, no TinyGo/wasm target needed) - go-pdk's
// WASM-import stubs don't have function bodies, which the Go compiler only
// permits under a wasm target - so importing it here would make the whole
// package host-untestable if that logic weren't split out (confirmed
// empirically: go-pdk's own bodyless wasmimport functions fail to compile
// under a plain host `go build`/`go test` with "missing function body",
// regardless of build tags, since that restriction is enforced by GOARCH,
// not by any tag). The `//go:build tinygo` constraint above keeps this file
// out of a plain `go test` build entirely - TinyGo defines the "tinygo"
// build tag automatically; plain `go test` doesn't - while still including
// it whenever this plugin is actually compiled via `tinygo build`, exactly
// mirroring docs/examples/tide-plugins/bom/main.go's identical split and
// verified the same way. See README.md, and
// backend/testdata/wasm_plugins/src/ftpfetch/main.go for the proven pattern
// this file's ftp_fetch calling convention is copied from (that fixture
// itself is a permanent wasm-only artifact with no host-testable split, so
// it uses "!std" instead - a tag that, unlike "tinygo", is not reliably
// false under a plain host build, so it would NOT have been a safe choice
// here where `go test ./...` at this package root must actually pass).
package main

import (
	"encoding/json"
	"errors"

	"github.com/extism/go-pdk"
)

// ftpFetch is the hand-written low-level import for the host's "ftp_fetch"
// custom host function - see backend/testdata/wasm_plugins/src/ftpfetch/main.go
// for the identical declaration and the full explanation of the
// //go:wasmimport contract (there is no generic "call a custom host
// function" helper in go-pdk; this is the officially-documented pattern).
//
//go:wasmimport extism:host/user ftp_fetch
func ftpFetch(uint64) uint64

// ftpFetchRequest/ftpFetchResponse mirror the host's ftp_fetch JSON contract
// exactly (backend/wasm_ftp_fetch.go's ftpFetchRequest/ftpFetchResponse).
type ftpFetchRequest struct {
	Host string `json:"host"`
	Path string `json:"path"`
}

type ftpFetchResponse struct {
	Body  string `json:"body"`
	Error string `json:"error"`
}

// fetchOverFTP is this plugin's ftpFetcher implementation (see bom.go):
// marshal {host,path} into guest memory, call the host's ftp_fetch import,
// and unmarshal its {body,error} response - the exact calling convention
// proven by backend/testdata/wasm_plugins/src/ftpfetch/main.go's
// fetchFtp export.
func fetchOverFTP(host, path string) (string, error) {
	reqMem, err := pdk.AllocateJSON(ftpFetchRequest{Host: host, Path: path})
	if err != nil {
		return "", err
	}
	defer reqMem.Free()

	respOffset := ftpFetch(reqMem.Offset())
	respMem := pdk.FindMemory(respOffset)

	var resp ftpFetchResponse
	if err := json.Unmarshal(respMem.ReadBytes(), &resp); err != nil {
		return "", err
	}
	if resp.Error != "" {
		return "", errors.New(resp.Error)
	}
	return resp.Body, nil
}

//go:wasmexport id
func id() int32 {
	pdk.OutputString("bom")
	return 0
}

//go:wasmexport name
func name() int32 {
	pdk.OutputString("Bureau of Meteorology (Australia)")
	return 0
}

//go:wasmexport ttl_seconds
func ttlSeconds() int32 {
	pdk.OutputString("5400") // 90m, matching the native bomMarineWarningsCacheTTL
	return 0
}

//go:wasmexport fetch_warnings
func fetchWarnings() int32 {
	var input fetchWarningsInput
	if err := pdk.InputJSON(&input); err != nil {
		pdk.SetError(err)
		return -1
	}

	out, err := buildFetchWarningsOutput(input.Lat, input.Lon, fetchOverFTP)
	if err != nil {
		pdk.SetError(err)
		return -1
	}

	if err := pdk.OutputJSON(out); err != nil {
		pdk.SetError(err)
		return -1
	}
	return 0
}

func main() {}
