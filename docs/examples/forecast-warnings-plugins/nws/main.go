//go:build tinygo
// +build tinygo

// nws is Helmcentral's US National Weather Service forecast-warnings WASM
// plugin - a reference (non-default) implementation of the pluggable
// Forecast Warnings provider contract (backend/forecast_warnings_providers.go),
// giving real coverage outside Australia (unlike ../bom, the default).
//
// Unlike ../bom, this plugin talks to a completely open, keyless,
// HTTP-reachable API (api.weather.gov) via Extism's built-in HTTP support
// (pdk.NewHTTPRequest) - no custom "ftp_fetch" host function needed. NWS
// asks (not requires) an identifying User-Agent header as good API
// citizenship; see doHTTPFetch below.
//
// This file (main.go) holds only the thin //go:wasmexport wrapper layer and
// the real HTTP-fetch implementation; all the actual JSON-mapping/
// categorization/filtering/orchestration logic lives in nws.go, which does
// not depend on "github.com/extism/go-pdk" so it (and main_test.go) can be
// built and tested with the plain host Go toolchain (`go test ./...`, no
// TinyGo/wasm target needed) - go-pdk's WASM-import stubs don't have
// function bodies, which the Go compiler only permits under a wasm target -
// so importing it here would make the whole package host-untestable if that
// logic weren't split out (confirmed empirically: go-pdk's own bodyless
// wasmimport functions fail to compile under a plain host `go build`/`go
// test` with "missing function body", regardless of build tags, since that
// restriction is enforced by GOARCH, not by any tag). The `//go:build
// tinygo` constraint above keeps this file out of a plain `go test` build
// entirely - TinyGo defines the "tinygo" build tag automatically; plain `go
// test` doesn't - while still including it whenever this plugin is actually
// compiled via `tinygo build`, exactly mirroring ../bom/main.go's identical
// split (confirmed empirically correct by that plugin's own author; the
// alternative "!std" tag used by some permanent wasm-only test fixtures is
// NOT reliably false under a plain host build, so it would not have been a
// safe choice here where `go test ./...` at this package root must actually
// pass).
package main

import (
	"github.com/extism/go-pdk"
)

// nwsUserAgent identifies this plugin to api.weather.gov per NWS's usage
// policy (https://www.weather.gov/documentation/services-web-api - "a
// User Agent is required to identify your application"). No fixed format is
// enforced by the API; this is a reasonable app-identifying string, not a
// hard requirement.
const nwsUserAgent = "(helmcentral, contact-not-provided)"

// doHTTPFetch is this plugin's httpFetcher implementation (see nws.go):
// issue a GET with the NWS-recommended User-Agent header via
// pdk.NewHTTPRequest/SetHeader/Send, and return the status code and body.
// err is reserved for genuine transport-level failures - go-pdk's HTTP
// support doesn't expose those separately from a status code today, so this
// always returns a nil error and lets the caller check status, mirroring
// how docs/examples/tide-plugins/noaa/main.go checks res.Status() itself
// rather than treating every non-2xx as a Go error.
func doHTTPFetch(url string) (int, []byte, error) {
	req := pdk.NewHTTPRequest(pdk.MethodGet, url)
	req.SetHeader("User-Agent", nwsUserAgent)
	res := req.Send()
	return int(res.Status()), res.Body(), nil
}

//go:wasmexport id
func id() int32 {
	pdk.OutputString("nws")
	return 0
}

//go:wasmexport name
func name() int32 {
	pdk.OutputString("US National Weather Service")
	return 0
}

//go:wasmexport ttl_seconds
func ttlSeconds() int32 {
	pdk.OutputString("1800") // 30m - NWS alerts can change faster than BOM's twice-daily bulletin cycle
	return 0
}

//go:wasmexport description
func description() int32 {
	pdk.OutputString("US National Weather Service marine/coastal warnings.")
	return 0
}

//go:wasmexport fetch_warnings
func fetchWarnings() int32 {
	var input fetchWarningsInput
	if err := pdk.InputJSON(&input); err != nil {
		pdk.SetError(err)
		return -1
	}

	out, err := buildFetchWarningsOutput(input.Lat, input.Lon, doHTTPFetch)
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
