//go:build !std
// +build !std

// valid is a minimal but complete tide-provider WASM fixture, implementing
// the full plugin contract (id/name/ttl_seconds/search_stations/
// fetch_tide_chart) with small, fixed test data. See
// backend/tide_provider_wasm_test.go for the regeneration command.
package main

import (
	"github.com/extism/go-pdk"
)

type searchStationsInput struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type stationOut struct {
	StationID string  `json:"station_id"`
	Name      string  `json:"name"`
	State     string  `json:"state,omitempty"`
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
	Timezone  string  `json:"timezone,omitempty"`
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

var fixtureStations = []stationOut{
	{StationID: "VALID1", Name: "Valid Fixture Station", State: "TS", Lat: -33.8, Lon: 151.2, Timezone: "Australia/Sydney"},
}

//go:wasmexport id
func id() int32 {
	pdk.OutputString("valid-fixture")
	return 0
}

//go:wasmexport name
func name() int32 {
	pdk.OutputString("Valid Fixture Provider")
	return 0
}

//go:wasmexport ttl_seconds
func ttlSeconds() int32 {
	pdk.OutputString("3600")
	return 0
}

//go:wasmexport search_stations
func searchStations() int32 {
	var input searchStationsInput
	if err := pdk.InputJSON(&input); err != nil {
		pdk.SetError(err)
		return -1
	}

	if err := pdk.OutputJSON(fixtureStations); err != nil {
		pdk.SetError(err)
		return -1
	}
	return 0
}

// fetchTideChart returns fixed extremes matching the exact fixture shape
// used by TestInterpolateTideNowBetweenExtremes in tide_providers_test.go,
// so the host-side interpolation test can assert against independently
// computed expected values.
//
//go:wasmexport fetch_tide_chart
func fetchTideChart() int32 {
	stationID := string(pdk.Input())
	if stationID == "" {
		pdk.SetErrorString("missing station id")
		return -1
	}

	out := tideChartOut{
		Station: fixtureStations[0],
		Extremes: []extremeOut{
			{Time: "2026-06-16T00:00:00Z", HeightM: 0.2, High: false},
			{Time: "2026-06-16T06:00:00Z", HeightM: 1.8, High: true},
		},
	}

	if err := pdk.OutputJSON(out); err != nil {
		pdk.SetError(err)
		return -1
	}
	return 0
}

func main() {}
