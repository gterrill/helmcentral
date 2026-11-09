//go:build tinygo
// +build tinygo

// open-meteo is Helmcentral's global tide-provider WASM plugin, backed by the
// Open-Meteo Marine API's modelled sea level. It is the default tide provider
// (docs/adr/0273-tides-come-from-a-global-model-by-default.md): it needs no key, covers the
// world's coasts, and no station has to be chosen.
//
// It differs from the bom and noaa plugins in two ways, both declared to the
// host through optional exports:
//
//   - position_based: there is no station catalog. The host passes the boat's
//     position (or a planned place's) as "<lat>,<lon>" in place of a station
//     id, and search_stations returns an empty list.
//   - height_datum: the heights are metres above mean sea level, not chart
//     datum, and include non-tidal sea-level effects (surge, pressure). The
//     host will not use them as depth above chart datum.
//
// Endpoint (shape captured from the live API, see testdata/):
//
//	GET https://marine-api.open-meteo.com/v1/marine?latitude=<lat>&longitude=<lon>
//	    &hourly=sea_level_height_msl&timezone=GMT&cell_selection=sea&past_days=1&forecast_days=7
//
// The API returns an hourly series, not highs and lows; open-meteo.go derives
// the extremes. All parsing lives there so it tests on the plain host Go
// toolchain; this file holds only the //go:wasmexport wrappers.
package main

import (
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
	pdk.OutputString("Open-Meteo (global sea level)")
	return 0
}

//go:wasmexport ttl_seconds
func ttlSeconds() int32 {
	pdk.OutputString("10800") // 3h
	return 0
}

//go:wasmexport description
func description() int32 {
	pdk.OutputString("Modelled tide anywhere at sea, from the boat's position. Heights are from mean sea level, not chart datum.")
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
	lat, lon, err := parsePosition(stationID)
	if err != nil {
		pdk.SetError(err)
		return -1
	}

	res := pdk.NewHTTPRequest(pdk.MethodGet, requestURL(lat, lon)).Send()
	body := res.Body()
	if res.Status() < 200 || res.Status() >= 300 {
		// A 400 carries {"error":true,"reason":"..."}; say why when it does.
		if _, perr := parseMarine(body); perr != nil {
			pdk.SetErrorString(fmt.Sprintf("open-meteo marine request failed: status %d: %v", res.Status(), perr))
		} else {
			pdk.SetErrorString(fmt.Sprintf("open-meteo marine request failed: status %d", res.Status()))
		}
		return -1
	}

	chart, err := buildChart(stationID, body)
	if err != nil {
		pdk.SetError(err)
		return -1
	}
	if err := pdk.OutputJSON(chart); err != nil {
		pdk.SetError(err)
		return -1
	}
	return 0
}

func main() {}
