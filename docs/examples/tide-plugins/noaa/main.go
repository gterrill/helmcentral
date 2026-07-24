//go:build !std
// +build !std

// noaa is a reference tide-provider WASM plugin for Helmcentral, backed by
// NOAA's public CO-OPS (Center for Operational Oceanographic Products and
// Services) Tides & Currents API. It exists to show what wiring up a real
// government tide API into the plugin contract looks like, as a teaching
// example. It is a different plugin from ../bom (the real, only "bom"
// provider, ported from the formerly-native backend/tide_provider_bom.go)
// - see docs/adr/0017-wasm-plugin-tide-providers.md's "Update: BOM ported
// to WASM" section for why that port was made.
//
// NOAA endpoints used (shapes confirmed by querying NOAA's own live API
// directly on 2026-07-17, cross-checked against
// https://api.tidesandcurrents.noaa.gov/api/prod/ and
// https://api.tidesandcurrents.noaa.gov/mdapi/prod/):
//
//   - Station search (all tide-prediction stations, ~3500 of them):
//     GET https://api.tidesandcurrents.noaa.gov/mdapi/prod/webapi/stations.json?type=tidepredictions
//     -> {"stations":[{"id":"8557863","name":"...","state":"...","lat":...,"lng":...,"timezone":"..."}, ...]}
//
//   - Single station metadata:
//     GET https://api.tidesandcurrents.noaa.gov/mdapi/prod/webapi/stations/<id>.json
//     -> {"stations":[{...same shape as above...}]}
//
//   - Tide predictions (high/low only):
//     GET https://api.tidesandcurrents.noaa.gov/api/prod/datagetter
//         ?product=predictions&datum=MLLW&interval=hilo&units=metric
//         &time_zone=gmt&format=json&application=helmcentral
//         &station=<id>&begin_date=<YYYYMMDD>&end_date=<YYYYMMDD>
//     -> {"predictions":[{"t":"2026-07-17 02:23","v":"1.552","type":"H"}, ...]}
//     "t" is "YYYY-MM-DD HH:MM" in the requested time_zone (gmt here, so no
//     offset math needed); "type" is "H" or "L".
//
// This is a teaching example, not a production-hardened client:
//   - No pagination of the mdapi station list - it's fetched whole (~3500
//     entries) and filtered guest-side by substring match on name. Fine for
//     a one-shot search call; wasteful if called very frequently.
//   - No retry/backoff on transient NOAA errors - a single failed request
//     just fails the call; the host's disk cache (wasm_tide_provider.go)
//     covers short outages by serving a stale result.
//   - NOAA's "timezone" field is a human-readable US zone name (e.g.
//     "Eastern"), not an IANA name like "America/New_York" - passed through
//     as-is rather than mapped. Verify against live NOAA docs before
//     relying on it for anything beyond display.
package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/extism/go-pdk"
)

const (
	stationsListURL    = "https://api.tidesandcurrents.noaa.gov/mdapi/prod/webapi/stations.json?type=tidepredictions"
	stationURLFmt      = "https://api.tidesandcurrents.noaa.gov/mdapi/prod/webapi/stations/%s.json"
	predictionsURLFmt  = "https://api.tidesandcurrents.noaa.gov/api/prod/datagetter?product=predictions&datum=MLLW&interval=hilo&units=metric&time_zone=gmt&format=json&application=helmcentral&station=%s&begin_date=%s&end_date=%s"
	defaultSearchLimit = 20
)

// --- NOAA mdapi response shapes ---

type mdapiStation struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	State    string  `json:"state"`
	Lat      float64 `json:"lat"`
	Lng      float64 `json:"lng"`
	Timezone string  `json:"timezone"`
}

type mdapiStationsResponse struct {
	Stations []mdapiStation `json:"stations"`
}

// --- NOAA datagetter response shapes ---

type noaaPrediction struct {
	Time  string `json:"t"`
	Value string `json:"v"`
	Type  string `json:"type"`
}

type noaaPredictionsResponse struct {
	Predictions []noaaPrediction `json:"predictions"`
}

// --- Helmcentral plugin contract shapes (backend/wasm_tide_provider.go) ---

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

type searchStationsInput struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

func mdapiToStationOut(s mdapiStation) stationOut {
	return stationOut{
		StationID: s.ID,
		Name:      s.Name,
		State:     s.State,
		Lat:       s.Lat,
		Lon:       s.Lng,
		Timezone:  s.Timezone,
	}
}

//go:wasmexport id
func id() int32 {
	pdk.OutputString("noaa")
	return 0
}

//go:wasmexport name
func name() int32 {
	pdk.OutputString("NOAA CO-OPS")
	return 0
}

//go:wasmexport ttl_seconds
func ttlSeconds() int32 {
	pdk.OutputString("3600")
	return 0
}

//go:wasmexport description
func description() int32 {
	pdk.OutputString("NOAA CO-OPS tide predictions for US stations.")
	return 0
}

//go:wasmexport search_stations
func searchStations() int32 {
	var input searchStationsInput
	if err := pdk.InputJSON(&input); err != nil {
		pdk.SetError(err)
		return -1
	}
	limit := input.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}

	req := pdk.NewHTTPRequest(pdk.MethodGet, stationsListURL)
	res := req.Send()
	if res.Status() < 200 || res.Status() >= 300 {
		pdk.SetErrorString(fmt.Sprintf("noaa mdapi station list request failed: status %d", res.Status()))
		return -1
	}

	var parsed mdapiStationsResponse
	if err := json.Unmarshal(res.Body(), &parsed); err != nil {
		pdk.SetError(err)
		return -1
	}

	query := strings.ToLower(strings.TrimSpace(input.Query))
	out := make([]stationOut, 0, limit)
	for _, s := range parsed.Stations {
		if query != "" && !strings.Contains(strings.ToLower(s.Name), query) {
			continue
		}
		out = append(out, mdapiToStationOut(s))
		if len(out) >= limit {
			break
		}
	}

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

	station, err := fetchStation(stationID)
	if err != nil {
		pdk.SetErrorString(err.Error())
		return -1
	}

	extremes, err := fetchExtremes(stationID)
	if err != nil {
		pdk.SetErrorString(err.Error())
		return -1
	}

	if err := pdk.OutputJSON(tideChartOut{Station: station, Extremes: extremes}); err != nil {
		pdk.SetError(err)
		return -1
	}
	return 0
}

func fetchStation(stationID string) (stationOut, error) {
	url := fmt.Sprintf(stationURLFmt, stationID)
	req := pdk.NewHTTPRequest(pdk.MethodGet, url)
	res := req.Send()
	if res.Status() < 200 || res.Status() >= 300 {
		return stationOut{}, fmt.Errorf("noaa mdapi station lookup failed for %s: status %d", stationID, res.Status())
	}

	var parsed mdapiStationsResponse
	if err := json.Unmarshal(res.Body(), &parsed); err != nil {
		return stationOut{}, err
	}
	if len(parsed.Stations) == 0 {
		return stationOut{}, fmt.Errorf("noaa mdapi returned no station for %s", stationID)
	}
	return mdapiToStationOut(parsed.Stations[0]), nil
}

// fetchExtremes requests a week-ish window of high/low predictions (one day
// back, six days forward) around "now" - enough for the tide chart to show
// today's and tomorrow's extremes with margin, matching the rough magnitude
// of window the native BOM/StormGlass providers fetch.
func fetchExtremes(stationID string) ([]extremeOut, error) {
	now := time.Now().UTC()
	begin := now.AddDate(0, 0, -1).Format("20060102")
	end := now.AddDate(0, 0, 6).Format("20060102")
	url := fmt.Sprintf(predictionsURLFmt, stationID, begin, end)

	req := pdk.NewHTTPRequest(pdk.MethodGet, url)
	res := req.Send()
	if res.Status() < 200 || res.Status() >= 300 {
		return nil, fmt.Errorf("noaa datagetter predictions request failed for %s: status %d", stationID, res.Status())
	}

	var parsed noaaPredictionsResponse
	if err := json.Unmarshal(res.Body(), &parsed); err != nil {
		return nil, err
	}

	out := make([]extremeOut, 0, len(parsed.Predictions))
	for _, p := range parsed.Predictions {
		height, err := strconv.ParseFloat(p.Value, 64)
		if err != nil {
			continue // TODO: a production client would log/report malformed rows instead of silently skipping them
		}
		t, err := time.Parse("2006-01-02 15:04", p.Time)
		if err != nil {
			continue
		}
		out = append(out, extremeOut{
			Time:    t.Format(time.RFC3339),
			HeightM: height,
			High:    p.Type == "H",
		})
	}
	return out, nil
}

func main() {}
