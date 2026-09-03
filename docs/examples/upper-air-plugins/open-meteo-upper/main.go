//go:build tinygo
// +build tinygo

// open-meteo-upper is Helmcentral's free, keyless upper-air (500mb) provider
// WASM plugin.
//
// Upper air is its own provider category rather than part of the weather
// provider because the two data sources do not overlap: Apple WeatherKit
// makes an excellent surface forecast and carries no pressure levels at all,
// so a boat should be able to run one source for weather and this for the
// upper pattern.
//
// Surviving the Storm (Dashew, 1999) calls the 500mb chart the most valuable
// forecasting tool aboard, because the upper trough is the venting mechanism
// that lets a surface low develop: without one, "the surface low will be
// anemic, or won't develop at all" (p62).
//
// Like every Helmcentral plugin this returns only raw SI-unit,
// RFC3339-timestamped values. It does not bucket by day, work out percentiles
// or decide what counts as a trough - the host does all of that generically
// (backend/upper_air.go), so duplicating it here would risk drifting from the
// host's behaviour.
//
// This file holds only the //go:wasmexport wrapper layer and is gated
// `//go:build tinygo` so the plain host toolchain can still build and test
// open-meteo-upper.go. See the wave plugin's main.go for the full explanation.
package main

import (
	"fmt"

	"github.com/extism/go-pdk"
)

type wasmFetchUpperAirInput struct {
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	Days int     `json:"days"`
}

//go:wasmexport id
func id() int32 {
	pdk.OutputString("open-meteo-upper")
	return 0
}

//go:wasmexport name
func name() int32 {
	pdk.OutputString("Open-Meteo Upper Air")
	return 0
}

//go:wasmexport ttl_seconds
func ttlSeconds() int32 {
	// 6 hours: the global models this comes from run four times a day, so
	// asking more often returns the same numbers.
	pdk.OutputString("21600")
	return 0
}

//go:wasmexport description
func description() int32 {
	pdk.OutputString("Free, keyless 500mb upper-air forecasts from Open-Meteo.")
	return 0
}

//go:wasmexport fetch_upper_air
func fetchUpperAir() int32 {
	var input wasmFetchUpperAirInput
	if err := pdk.InputJSON(&input); err != nil {
		pdk.SetError(err)
		return -1
	}

	req := pdk.NewHTTPRequest(pdk.MethodGet, upperAirRequestURL(input.Lat, input.Lon, input.Days))
	res := req.Send()
	if res.Status() < 200 || res.Status() >= 300 {
		pdk.SetErrorString(fmt.Sprintf("open-meteo upper-air API returned %d", res.Status()))
		return -1
	}

	hourly, err := parseOpenMeteoUpperAir(res.Body())
	if err != nil {
		pdk.SetErrorString(fmt.Sprintf("failed to parse upper-air response: %v", err))
		return -1
	}

	if err := pdk.OutputJSON(fetchUpperAirOutput{Hourly: hourly}); err != nil {
		pdk.SetError(err)
		return -1
	}

	return 0
}

func main() {}
