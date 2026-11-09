//go:build !std
// +build !std

// positionbased is a tide-provider WASM fixture that declares itself
// position-based with heights from mean sea level, through the optional
// position_based and height_datum exports (ADR 0273). fetch_tide_chart echoes
// the "<lat>,<lon>" it was given back as the station id, so a test can see
// what the host passed. See backend/wasm_tide_provider_test.go for the
// regeneration command.
package main

import (
	"github.com/extism/go-pdk"
)

type stationOut struct {
	StationID string  `json:"station_id"`
	Name      string  `json:"name"`
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
}

type extremeOut struct {
	Time    string  `json:"time"`
	HeightM float64 `json:"height_m"`
	High    bool    `json:"high"`
}

type tideChartOut struct {
	Station  stationOut   `json:"station"`
	Extremes []extremeOut `json:"extremes"`
}

//go:wasmexport id
func id() int32 {
	pdk.OutputString("position-fixture")
	return 0
}

//go:wasmexport name
func name() int32 {
	pdk.OutputString("Position Fixture Provider")
	return 0
}

//go:wasmexport position_based
func positionBased() int32 {
	pdk.OutputString("true")
	return 0
}

//go:wasmexport height_datum
func heightDatum() int32 {
	pdk.OutputString("mean_sea_level")
	return 0
}

//go:wasmexport search_stations
func searchStations() int32 {
	if err := pdk.OutputJSON([]stationOut{}); err != nil {
		pdk.SetError(err)
		return -1
	}
	return 0
}

//go:wasmexport fetch_tide_chart
func fetchTideChart() int32 {
	stationID := string(pdk.Input())
	if stationID == "" {
		pdk.SetErrorString("missing station id")
		return -1
	}
	out := tideChartOut{
		Station: stationOut{StationID: stationID, Name: "Echo"},
		Extremes: []extremeOut{
			{Time: "2026-06-16T00:00:00Z", HeightM: -0.6, High: false},
			{Time: "2026-06-16T06:00:00Z", HeightM: -0.1, High: true},
		},
	}
	if err := pdk.OutputJSON(out); err != nil {
		pdk.SetError(err)
		return -1
	}
	return 0
}

func main() {}
