//go:build tinygo
// +build tinygo

// weatherkit is Helmcentral's reference Apple WeatherKit weather-provider
// WASM plugin - the only weather plugin needing per-operator secrets (an
// Apple WeatherKit key) and the only one doing in-guest cryptographic
// signing (ES256 JWT, required by WeatherKit's API). See README.md for how
// to obtain WeatherKit credentials and install this plugin.
//
// Unlike the free, keyless ../open-meteo reference plugin, WeatherKit
// requires a paid Apple Developer Program membership. If this plugin's
// config keys aren't set, it still loads successfully (id/name/ttl_seconds
// need no config) but fetch_forecast fails with a clear "missing config
// key(s)" error the moment it's actually selected and called - never fake
// weather data.
//
// This file (main.go) holds only the thin //go:wasmexport wrapper layer;
// all the actual JWT-building/URL-building/JSON-mapping logic lives in
// weatherkit.go, which has no dependency on "github.com/extism/go-pdk"
// specifically so it (and main_test.go) can be built and tested with the
// plain host Go toolchain (`go test ./...`, no TinyGo/wasm target needed) -
// see docs/examples/tide-plugins/bom/main.go's doc comment for the same
// split rationale, and weatherkit.go's top doc comment for the full field
// mapping / porting notes.
package main

import (
	"fmt"
	"time"

	"github.com/extism/go-pdk"
)

//go:wasmexport id
func id() int32 {
	pdk.OutputString("weatherkit")
	return 0
}

//go:wasmexport name
func name() int32 {
	pdk.OutputString("Apple WeatherKit")
	return 0
}

//go:wasmexport ttl_seconds
func ttlSeconds() int32 {
	pdk.OutputString("900") // 15m
	return 0
}

//go:wasmexport fetch_forecast
func fetchForecast() int32 {
	var input fetchForecastInput
	if err := pdk.InputJSON(&input); err != nil {
		pdk.SetError(err)
		return -1
	}

	cfg, missing := resolveWeatherKitConfig(pdk.GetConfig)
	if len(missing) > 0 {
		pdk.SetErrorString(missingConfigKeyErrorMessage(missing))
		return -1
	}

	token, err := buildWeatherKitJWT(cfg.KeyID, cfg.TeamID, cfg.ServiceID, cfg.PrivateKeyPEM, time.Now().UTC())
	if err != nil {
		pdk.SetErrorString(err.Error())
		return -1
	}

	url := weatherKitRequestURL(input.Lat, input.Lon)
	req := pdk.NewHTTPRequest(pdk.MethodGet, url)
	req.SetHeader("Authorization", "Bearer "+token)
	res := req.Send()
	if res.Status() < 200 || res.Status() >= 300 {
		pdk.SetErrorString(fmt.Sprintf("weatherkit: WeatherKit API request failed: status %d", res.Status()))
		return -1
	}

	out, err := parseWeatherKitResponse(res.Body(), input.Days)
	if err != nil {
		pdk.SetErrorString(err.Error())
		return -1
	}

	if err := pdk.OutputJSON(out); err != nil {
		pdk.SetError(err)
		return -1
	}
	return 0
}

func main() {}
