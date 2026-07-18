// bom.go holds the parsing logic for the BOM (Bureau of Meteorology,
// Australia) tide-provider plugin, kept in a separate file from main.go
// deliberately: this file has no dependency on "github.com/extism/go-pdk",
// so it (and main_test.go, which exercises it) can be built and tested with
// the plain host Go toolchain (`go test ./...`, no TinyGo/wasm target
// needed) - see main.go's doc comment for why that split matters. main.go's
// //go:wasmexport functions call straight into these same functions; nothing
// here is reimplemented or duplicated there.
package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed data/bom_tide_sites.json
var bomTideSitesJSON []byte

const (
	// bomUserAgent matches the formerly-native backend/tide_provider_bom.go's
	// bomUserAgent constant verbatim - BOM's tide-table endpoint rejects
	// requests without a browser-like User-Agent.
	bomUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

	// bomTidesTableURLFmt mirrors fetchBomTidesTable's native URL exactly:
	// %s = AAC station code, %s = start date formatted "02-01-2006".
	bomTidesTableURLFmt = "https://www.bom.gov.au/australia/tides/scripts/getTidesTable.php?aac=%s&date=%s&days=8&offset=0&type=tide"

	// defaultSearchLimit matches the NOAA reference plugin's and the native
	// bomTideProvider.SearchStations' default when limit<=0.
	defaultSearchLimit = 20
)

// --- Helmcentral plugin contract shapes (backend/wasm_tide_provider.go) ---
// Field names/JSON tags match docs/examples/tide-plugins/noaa/main.go's
// identical types exactly - both plugins speak the same host contract.

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

// --- BOM's embedded station GeoJSON shape ---
// Identical field-for-field to the formerly-native backend/tide_provider_bom.go's
// bomGeoJSON: PORT_NAME, AAC, STATE_NAME, TIME_ZONE, LAT, LON, TYPE.

type bomGeoJSON struct {
	Features []struct {
		Properties struct {
			PortName  string  `json:"PORT_NAME"`
			AAC       string  `json:"AAC"`
			StateName string  `json:"STATE_NAME"`
			TimeZone  string  `json:"TIME_ZONE"`
			Lat       float64 `json:"LAT"`
			Lon       float64 `json:"LON"`
			Type      string  `json:"TYPE"`
		} `json:"properties"`
	} `json:"features"`
}

// parseBomStations parses the embedded BOM site GeoJSON, keeping only
// features with Type == "TIDE" and a non-empty AAC code - identical
// filtering to the formerly-native newBomTideProvider(). Unlike that native
// constructor (which logged and continued with zero stations on a parse
// error), this returns the error to the caller: a WASM instance is created
// fresh per call here, so there's no "started up successfully but empty"
// state to preserve - fail fast and let the caller surface the error
// (per this repo's no-masking-fallbacks policy).
func parseBomStations(raw []byte) ([]stationOut, error) {
	var parsed bomGeoJSON
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse embedded BOM tide site list: %w", err)
	}

	stations := make([]stationOut, 0, len(parsed.Features))
	for _, feature := range parsed.Features {
		props := feature.Properties
		if props.Type != "TIDE" || props.AAC == "" {
			continue
		}
		stations = append(stations, stationOut{
			StationID: props.AAC,
			Name:      props.PortName,
			State:     props.StateName,
			Lat:       props.Lat,
			Lon:       props.Lon,
			Timezone:  props.TimeZone,
		})
	}
	return stations, nil
}

// bomTideTimeRegex and bomTideHeightRegex are ported verbatim from the
// formerly-native backend/tide_provider_bom.go.
var (
	bomTideTimeRegex   = regexp.MustCompile(`data-time-utc="([^"]+)"[^>]*class="localtime (high|low)-tide"`)
	bomTideHeightRegex = regexp.MustCompile(`class="height (?:high|low)-tide">([\d.]+)\s*m`)
)

// parseBomTidesTable parses BOM's scraped tide-table HTML into a
// time-sorted list of extremes, matching the formerly-native
// parseBomTidesTable's behavior exactly (same two regexes, same pairing of
// the Nth time match with the Nth height match, same sort). It returns
// extremeOut (Time as an RFC3339 string) rather than the native
// tideExtremePoint (Time as time.Time) because that's this plugin's JSON
// contract shape with the host - see stationOut/extremeOut/tideChartOut
// above.
func parseBomTidesTable(html string) ([]extremeOut, error) {
	timeMatches := bomTideTimeRegex.FindAllStringSubmatch(html, -1)
	heightMatches := bomTideHeightRegex.FindAllStringSubmatch(html, -1)

	if len(timeMatches) == 0 || len(timeMatches) != len(heightMatches) {
		return nil, fmt.Errorf("unexpected BOM tide table format (%d times, %d heights)", len(timeMatches), len(heightMatches))
	}

	type parsedExtreme struct {
		t       time.Time
		heightM float64
		high    bool
	}

	extremes := make([]parsedExtreme, 0, len(timeMatches))
	for i, timeMatch := range timeMatches {
		t, err := time.Parse(time.RFC3339, timeMatch[1])
		if err != nil {
			continue
		}
		height, err := strconv.ParseFloat(heightMatches[i][1], 64)
		if err != nil {
			continue
		}
		extremes = append(extremes, parsedExtreme{t: t, heightM: height, high: timeMatch[2] == "high"})
	}

	sort.Slice(extremes, func(i, j int) bool { return extremes[i].t.Before(extremes[j].t) })

	out := make([]extremeOut, 0, len(extremes))
	for _, e := range extremes {
		out = append(out, extremeOut{Time: e.t.Format(time.RFC3339), HeightM: e.heightM, High: e.high})
	}
	return out, nil
}

// searchBomStations applies the same case-insensitive substring filter as
// the formerly-native bomTideProvider.SearchStations: empty query returns up
// to limit stations from the full catalog, unfiltered - a documented
// contract convention (see the tideProvider interface's doc comment in
// backend/tide_providers.go) that the host's generic nearestStation lookup
// depends on for every provider, native or WASM.
func searchBomStations(stations []stationOut, query string, limit int) []stationOut {
	query = strings.ToLower(strings.TrimSpace(query))
	if limit <= 0 {
		limit = defaultSearchLimit
	}

	results := make([]stationOut, 0, limit)
	for _, s := range stations {
		if query != "" &&
			!strings.Contains(strings.ToLower(s.Name), query) &&
			!strings.Contains(strings.ToLower(s.StationID), query) &&
			!strings.Contains(strings.ToLower(s.State), query) {
			continue
		}
		results = append(results, s)
		if len(results) >= limit {
			break
		}
	}
	return results
}

// findBomStation looks up a station by its AAC station ID.
func findBomStation(stations []stationOut, stationID string) (stationOut, bool) {
	for _, s := range stations {
		if s.StationID == stationID {
			return s, true
		}
	}
	return stationOut{}, false
}
