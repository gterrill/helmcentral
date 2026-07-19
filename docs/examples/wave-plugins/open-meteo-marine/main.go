//go:build tinygo
// +build tinygo

// open-meteo-marine is Helmcentral's free, keyless Open-Meteo Marine
// wave-provider WASM plugin. This is the default wave provider for fresh
// Helmcentral installs because it needs no API key.
//
// The Marine API is queried with two separate HTTP calls:
//   1. Wave data (REQUIRED): Pinned to NOAA's GFS-Wave (WaveWatch III) model
//      so swell height, period and direction line up with other WaveWatch
//      III-based swell forecasts for a given coastline; Open-Meteo's default
//      "best_match" model blend runs noticeably lower/different.
//   2. Sea surface temperature (OPTIONAL): Deliberately does not pin a
//      models= param (unlike the wave-data fetch) - the wave-specific NOAA
//      GFS-Wave model does not carry sea surface temperature at all (returns
//      null/"undefined" units); only Open-Meteo's default model blend does.
//
// If the wave-data fetch fails (non-2xx status or unparseable JSON), that is
// a hard error: pdk.SetErrorString(...) + return -1. If the sea-temperature
// fetch fails or the value is genuinely absent/null (confirmed live: inland
// coordinates return 200 with sea_surface_temperature:null), the whole
// fetch_waves call still succeeds - sea_surface_temperature_c is simply
// omitted from the output JSON entirely (do not emit a placeholder 0).
//
// This file (main.go) holds only the thin //go:wasmexport wrapper layer;
// all the actual parsing/HTTP logic lives in open-meteo-marine.go, which has
// no dependency on "github.com/extism/go-pdk" specifically so it (and
// main_test.go) can be built and tested with the plain host Go toolchain
// (`go test ./...`, no TinyGo/wasm target needed) - go-pdk's WASM-import
// stubs (internal/memory) don't have function bodies and only compile under
// a wasm target, so importing it here would make the whole package
// host-untestable if open-meteo-marine.go's logic weren't split out. The
// `//go:build tinygo` constraint above keeps this file out of a plain `go
// test` build entirely (TinyGo defines the "tinygo" build tag automatically;
// plain `go test` doesn't set it), while still including it whenever this
// plugin is actually compiled via `tinygo build` - see README.md. Verified
// empirically both ways before this file was written.
//
// Like every WASM wave plugin, this returns only raw SI-unit, RFC3339-timestamped
// wave and sea-temperature data; it does NOT bucket by day or interpolate
// current conditions - the host (backend/wasm_wave_provider.go) already does
// both generically for every plugin, so duplicating either here would risk
// silently drifting from the host's behavior.
package main

import (
	"encoding/json"
	"fmt"

	"github.com/extism/go-pdk"
)

// wasmFetchWavesInput mirrors the host's contract shape
// (backend/wasm_wave_provider.go).
type wasmFetchWavesInput struct {
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	Days int     `json:"days"`
}

//go:wasmexport id
func id() int32 {
	pdk.OutputString("open-meteo-marine")
	return 0
}

//go:wasmexport name
func name() int32 {
	pdk.OutputString("Open-Meteo Marine")
	return 0
}

//go:wasmexport ttl_seconds
func ttlSeconds() int32 {
	// 1 hour - marine models update far less often than point weather
	pdk.OutputString("3600")
	return 0
}

//go:wasmexport fetch_waves
func fetchWaves() int32 {
	var input wasmFetchWavesInput
	if err := pdk.InputJSON(&input); err != nil {
		pdk.SetError(err)
		return -1
	}

	days := clampForecastDays(input.Days)

	// Fetch wave data (REQUIRED)
	waveURL := fmt.Sprintf(marineWaveURLFmt, input.Lat, input.Lon, days)
	waveReq := pdk.NewHTTPRequest(pdk.MethodGet, waveURL)
	waveRes := waveReq.Send()
	if waveRes.Status() < 200 || waveRes.Status() >= 300 {
		pdk.SetErrorString(fmt.Sprintf("open-meteo marine wave API returned %d", waveRes.Status()))
		return -1
	}

	waveBytes := waveRes.Body()

	// Extract UTC offset from wave response (needed for local-time conversion)
	var waveRawResult map[string]any
	if err := json.Unmarshal(waveBytes, &waveRawResult); err != nil {
		pdk.SetErrorString(fmt.Sprintf("failed to parse wave response: %v", err))
		return -1
	}

	utcOffsetSeconds := 0
	if offset, ok := waveRawResult["utc_offset_seconds"].(float64); ok {
		utcOffsetSeconds = int(offset)
	}

	// Parse wave data
	hourlyData, err := parseOpenMeteoWaveResponse(waveBytes, utcOffsetSeconds)
	if err != nil {
		pdk.SetErrorString(fmt.Sprintf("failed to parse wave response: %v", err))
		return -1
	}

	// Fetch sea temperature (OPTIONAL)
	var seaTemp *float64
	seaTempURL := fmt.Sprintf(marineSeaTempURLFmt, input.Lat, input.Lon)
	seaTempReq := pdk.NewHTTPRequest(pdk.MethodGet, seaTempURL)
	seaTempRes := seaTempReq.Send()
	if seaTempRes.Status() >= 200 && seaTempRes.Status() < 300 {
		seaTempBytes := seaTempRes.Body()
		// Ignore errors - sea temperature is optional
		seaTemp, _ = parseOpenMeteoSeaTemperatureResponse(seaTempBytes)
	}

	// Build output
	output := fetchWavesOutput{
		Hourly:                 hourlyData,
		SeaSurfaceTemperatureC: seaTemp,
	}

	if err := pdk.OutputJSON(output); err != nil {
		pdk.SetError(err)
		return -1
	}

	return 0
}

func main() {}
