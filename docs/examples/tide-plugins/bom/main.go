//go:build tinygo
// +build tinygo

// bom is Helmcentral's Bureau of Meteorology (Australia) tide-provider WASM
// plugin - the real, only implementation of the "bom" provider now that the
// formerly-native backend/tide_provider_bom.go has been deleted (see
// docs/adr/0017-wasm-plugin-tide-providers.md's "Update: BOM ported to WASM"
// section for why that port was made and what tradeoffs were accepted). It
// is a 1:1 port, not a new implementation: same embedded station list, same
// AAC station IDs, same HTML-scraping approach - existing settings.yaml
// configs (tide_provider: bom, tide_station_id: <AAC>) keep working
// unchanged.
//
// BOM has no public JSON tide API for this data - it's scraped from an
// undocumented HTML endpoint:
//
//	GET https://www.bom.gov.au/australia/tides/scripts/getTidesTable.php?aac=<AAC>&date=<DD-MM-YYYY>&days=8&offset=0&type=tide
//
// <AAC> is the station's BOM area code (this plugin's station_id); <date>
// is "yesterday" (so the window includes a little of today's already-passed
// tide state, matching the native fetchBomTidesTable's behavior exactly).
// The response is an HTML page parsed with two regexes
// (bomTideTimeRegex/bomTideHeightRegex in bom.go, ported verbatim from the
// native Go version) - not JSON. BOM's server also rejects requests without
// a browser-like User-Agent header, hence bomUserAgent in bom.go.
//
// This file (main.go) holds only the thin //go:wasmexport wrapper layer;
// all the actual parsing/filtering logic lives in bom.go, which has no
// dependency on "github.com/extism/go-pdk" specifically so it (and
// main_test.go) can be built and tested with the plain host Go toolchain
// (`go test ./...`, no TinyGo/wasm target needed) - go-pdk's WASM-import
// stubs (internal/memory) don't have function bodies and only compile
// under a wasm target, so importing it here would make the whole package
// host-untestable if bom.go's logic weren't split out. The `//go:build
// tinygo` constraint above keeps this file out of a plain `go test` build
// entirely (TinyGo defines the "tinygo" build tag automatically; plain `go
// test` doesn't set it), while still including it whenever this plugin is
// actually compiled via `tinygo build` - see README.md. Verified empirically
// both ways before this file was written.
//
// Like every WASM tide plugin, this returns only raw station and extremes
// data; it does NOT cache results or interpolate the current tide
// height/direction - the host (backend/wasm_tide_provider.go) already does
// both generically for every plugin, so duplicating either here would risk
// silently drifting from the host's behavior.
package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/extism/go-pdk"
)

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
	pdk.OutputString("43200") // 12h, matching the native bomTideCacheTTL
	return 0
}

//go:wasmexport search_stations
func searchStations() int32 {
	var input searchStationsInput
	if err := pdk.InputJSON(&input); err != nil {
		pdk.SetError(err)
		return -1
	}

	stations, err := parseBomStations(bomTideSitesJSON)
	if err != nil {
		pdk.SetError(err)
		return -1
	}

	out := searchBomStations(stations, input.Query, input.Limit)
	if err := pdk.OutputJSON(out); err != nil {
		pdk.SetError(err)
		return -1
	}
	return 0
}

//go:wasmexport fetch_tide_chart
func fetchTideChart() int32 {
	stationID := strings.TrimSpace(string(pdk.Input()))
	if stationID == "" {
		pdk.SetErrorString("missing station id")
		return -1
	}

	stations, err := parseBomStations(bomTideSitesJSON)
	if err != nil {
		pdk.SetError(err)
		return -1
	}

	station, ok := findBomStation(stations, stationID)
	if !ok {
		pdk.SetErrorString(fmt.Sprintf("unknown BOM tide station: %s", stationID))
		return -1
	}

	now := time.Now().UTC()
	startDate := now.AddDate(0, 0, -1).Format("02-01-2006")
	url := fmt.Sprintf(bomTidesTableURLFmt, station.StationID, startDate)

	req := pdk.NewHTTPRequest(pdk.MethodGet, url)
	req.SetHeader("User-Agent", bomUserAgent)
	res := req.Send()
	if res.Status() < 200 || res.Status() >= 300 {
		pdk.SetErrorString(fmt.Sprintf("BOM tide table request failed for %s: status %d", stationID, res.Status()))
		return -1
	}

	extremes, err := parseBomTidesTable(string(res.Body()))
	if err != nil {
		pdk.SetError(err)
		return -1
	}

	if err := pdk.OutputJSON(tideChartOut{Station: station, Extremes: extremes}); err != nil {
		pdk.SetError(err)
		return -1
	}
	return 0
}

func main() {}
