//go:build !std
// +build !std

// baddatum is a tide-provider WASM fixture whose height_datum export names a
// datum the host does not know. The host must refuse to load it rather than
// guess what its heights are measured from (ADR 0273). See
// backend/wasm_tide_provider_test.go for the regeneration command.
package main

import (
	"github.com/extism/go-pdk"
)

//go:wasmexport id
func id() int32 {
	pdk.OutputString("bad-datum-fixture")
	return 0
}

//go:wasmexport name
func name() int32 {
	pdk.OutputString("Bad Datum Fixture Provider")
	return 0
}

//go:wasmexport height_datum
func heightDatum() int32 {
	pdk.OutputString("lowest_astronomical_tide_maybe")
	return 0
}

//go:wasmexport search_stations
func searchStations() int32 {
	pdk.OutputString("[]")
	return 0
}

//go:wasmexport fetch_tide_chart
func fetchTideChart() int32 {
	pdk.SetErrorString("not reachable")
	return -1
}

func main() {}
