//go:build tinygo
// +build tinygo

// openmeteo is Helmcentral's Open-Meteo weather-provider WASM plugin, backed
// by Open-Meteo's free, keyless Forecast API covering weather worldwide. It
// is the DEFAULT weather provider for fresh Helmcentral installs, precisely
// because it needs no API key and no operator-supplied secrets - just lat/lon,
// and the host's network sandbox handles all authorization via Open-Meteo's
// unrestricted public tier.
//
// Open-Meteo endpoint used (shape confirmed by querying Open-Meteo's own live
// API directly on 2026-07-19):
//
//	GET https://api.open-meteo.com/v1/forecast
//	  ?latitude=<lat>&longitude=<lon>
//	  &current=temperature_2m,weather_code,wind_speed_10m,wind_gusts_10m,wind_direction_10m,is_day,precipitation_probability
//	  &hourly=temperature_2m,weather_code,wind_speed_10m,wind_gusts_10m,wind_direction_10m,precipitation_probability,precipitation,uv_index,is_day
//	  &daily=weather_code,temperature_2m_max,temperature_2m_min,wind_speed_10m_max,wind_gusts_10m_max,wind_direction_10m_dominant,precipitation_probability_max,sunrise,sunset
//	  &wind_speed_unit=ms&timezone=auto&forecast_days=<days>
//	-> {"latitude":..,"longitude":..,"current":{...},"hourly":{...},"daily":{...},"utc_offset_seconds":...,"timezone":"..."}
//
// IMPORTANT: Open-Meteo returns naive local times (not RFC3339), coupled with
// an explicit utc_offset_seconds field. This plugin converts every timestamp
// to RFC3339 UTC in open-meteo.go's parseOpenMeteoLocalTime function before
// emitting it.
//
// This file (main.go) holds only the thin //go:wasmexport wrapper layer;
// all the actual parsing/filtering logic lives in open-meteo.go, which has no
// dependency on "github.com/extism/go-pdk" specifically so it (and
// main_test.go) can be built and tested with the plain host Go toolchain
// (`go test ./...`, no TinyGo/wasm target needed) - go-pdk's WASM-import
// stubs (internal/memory) don't have function bodies and only compile
// under a wasm target, so importing it here would make the whole package
// host-untestable if open-meteo.go's logic weren't split out. The `//go:build
// tinygo` constraint above keeps this file out of a plain `go test` build
// entirely (TinyGo defines the "tinygo" build tag automatically; plain `go
// test` doesn't set it), while still including it whenever this plugin is
// actually compiled via `tinygo build` - see README.md.
package main

import (
	"encoding/json"
	"fmt"

	"github.com/extism/go-pdk"
)

//go:wasmexport id
func id() int32 {
	pdk.OutputString("open-meteo")
	return 0
}

//go:wasmexport name
func name() int32 {
	pdk.OutputString("Open-Meteo")
	return 0
}

//go:wasmexport ttl_seconds
func ttlSeconds() int32 {
	pdk.OutputString("900") // 15 minutes
	return 0
}

//go:wasmexport description
func description() int32 {
	pdk.OutputString("Free, keyless weather forecasts from Open-Meteo.")
	return 0
}

//go:wasmexport fetch_forecast
func fetchForecast() int32 {
	var input wasmFetchForecastInput
	if err := pdk.InputJSON(&input); err != nil {
		pdk.SetError(err)
		return -1
	}

	// Clamp days to Open-Meteo's supported range: 1-16 (default 7 if <= 0)
	days := input.Days
	if days <= 0 {
		days = 7
	} else if days > 16 {
		days = 16
	}

	// Build URL
	url := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f"+
			"&current=temperature_2m,weather_code,wind_speed_10m,wind_gusts_10m,wind_direction_10m,is_day,precipitation_probability"+
			"&hourly=temperature_2m,weather_code,wind_speed_10m,wind_gusts_10m,wind_direction_10m,precipitation_probability,precipitation,uv_index,is_day"+
			"&daily=weather_code,temperature_2m_max,temperature_2m_min,wind_speed_10m_max,wind_gusts_10m_max,wind_direction_10m_dominant,precipitation_probability_max,sunrise,sunset"+
			"&wind_speed_unit=ms&timezone=auto&forecast_days=%d",
		input.Lat, input.Lon, days,
	)

	// Fetch the forecast
	req := pdk.NewHTTPRequest(pdk.MethodGet, url)
	res := req.Send()
	if res.Status() < 200 || res.Status() >= 300 {
		pdk.SetErrorString(fmt.Sprintf("Open-Meteo forecast request failed: status %d", res.Status()))
		return -1
	}

	// Parse the raw Open-Meteo response
	var rawResponse openMeteoResponse
	if err := json.Unmarshal(res.Body(), &rawResponse); err != nil {
		pdk.SetError(err)
		return -1
	}

	// Convert to output contract
	out, err := parseOpenMeteoForecast(&rawResponse)
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
