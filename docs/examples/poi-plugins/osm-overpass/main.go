//go:build tinygo
// +build tinygo

// osm-overpass is Helmcentral's free, keyless POI-provider WASM plugin,
// backed by OpenStreetMap data via the public Overpass API. It is the
// default POI provider for fresh Helmcentral installs because it needs no
// API key.
//
// One Overpass POST per fetch_poi call covers every requested category (see
// osm-overpass.go's buildOverpassPOIQuery), each in its own named set with
// its own cap, per docs/reference/poi-categories.md. Overpass's rate-limit
// signature - an HTTP 200 carrying an HTML body instead of JSON - is
// detected explicitly and returned as an error, never silently read as an
// empty result: see looksLikeOverpassRateLimit in osm-overpass.go.
//
// The Overpass endpoint itself defaults to the public overpass-api.de but
// can be pointed at a mirror via the optional "overpass_url" config key -
// see resolveOverpassURL in osm-overpass.go and this plugin's README. A
// malformed override fails the call outright rather than silently using the
// default.
//
// After classification, the nearest detail_limit (config, default 5)
// features carrying an OSM "wikipedia" tag are enriched with the first
// sentence of the English Wikipedia REST page summary. A feature that has
// no wikipedia tag, or whose summary fetch fails, simply carries no detail
// - never a placeholder string.
//
// Two further exports are OPTIONAL and independent of fetch_poi:
// place_name_at (one ring's worth of backend/place_name.go's anchorage/bay/
// island ranking) and search_places (backend/assistant_tools.go's
// find_places two-rung name-search ladder). Both are plain ports of that
// backend logic onto this plugin's own Overpass endpoint - see
// osm-overpass.go's "place_name_at" and "search_places" sections for the
// query building/ranking/orchestration logic (runPlaceNameAt,
// runSearchPlaces), which is exercised directly by `go test` via the
// injectable overpassQueryFunc; this file supplies doOverpassQuery, the one
// place both wasmexports below actually call the pdk HTTP client.
//
// This file (main.go) holds only the thin //go:wasmexport wrapper layer;
// all the actual query-building/parsing logic lives in osm-overpass.go,
// which has no dependency on "github.com/extism/go-pdk" specifically so it
// (and main_test.go) can be built and tested with the plain host Go
// toolchain (`go test ./...`, no TinyGo/wasm target needed) - see
// docs/examples/wave-plugins/open-meteo-marine/main.go for the same split
// and the reasoning behind it. Always build the whole package directory
// (`.`), not just main.go by name - see this plugin's README.
package main

import (
	"fmt"
	"net/url"

	"github.com/extism/go-pdk"
)

// wasmFetchPOIInput mirrors the host's contract shape
// (backend/wasm_poi_provider.go).
type wasmFetchPOIInput struct {
	Lat        float64  `json:"lat"`
	Lon        float64  `json:"lon"`
	RadiusM    int      `json:"radius_m"`
	Categories []string `json:"categories"`
	Limit      int      `json:"limit"`
}

type wasmPOIFeatureOutput struct {
	ID        string  `json:"id"`
	Category  string  `json:"category"`
	Name      string  `json:"name"`
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
	Detail    string  `json:"detail"`
	SourceURL string  `json:"source_url"`
}

type wasmFetchPOIOutput struct {
	Features    []wasmPOIFeatureOutput `json:"features"`
	Truncated   []string               `json:"truncated"`
	Unsupported []string               `json:"unsupported"`
}

//go:wasmexport id
func id() int32 {
	pdk.OutputString("osm-overpass")
	return 0
}

//go:wasmexport name
func name() int32 {
	pdk.OutputString("OpenStreetMap (Overpass)")
	return 0
}

//go:wasmexport description
func description() int32 {
	pdk.OutputString("Free, keyless points of interest from OpenStreetMap via the public Overpass API.")
	return 0
}

//go:wasmexport ttl_seconds
func ttlSeconds() int32 {
	// 6 hours - see docs/adr/0091 for why (Overpass etiquette, local
	// features change slowly).
	pdk.OutputString("21600")
	return 0
}

//go:wasmexport fetch_poi
func fetchPOI() int32 {
	var input wasmFetchPOIInput
	if err := pdk.InputJSON(&input); err != nil {
		pdk.SetError(err)
		return -1
	}

	query := buildOverpassPOIQuery(input.Lat, input.Lon, input.RadiusM, input.Categories)

	overpassURL, err := resolveOverpassURL(pdk.GetConfig("overpass_url"))
	if err != nil {
		pdk.SetError(err)
		return -1
	}

	req := pdk.NewHTTPRequest(pdk.MethodPost, overpassURL)
	req.SetHeader("Content-Type", "application/x-www-form-urlencoded")
	req.SetHeader("User-Agent", "helmcentral-osm-overpass-plugin/1.0")
	req.SetBody([]byte("data=" + url.QueryEscape(query)))
	resp := req.Send()

	body := resp.Body()
	contentType := headerCaseInsensitive(resp.Headers(), "Content-Type")

	if resp.Status() != 200 {
		if looksLikeOverpassRateLimit(contentType, body) {
			pdk.SetErrorString("overpass rate limited (HTTP " + fmt.Sprint(resp.Status()) + " with a non-JSON body)")
			return -1
		}
		pdk.SetErrorString(fmt.Sprintf("overpass returned status %d", resp.Status()))
		return -1
	}

	elements, err := parseOverpassBody(contentType, body)
	if err != nil {
		pdk.SetError(err)
		return -1
	}

	features, truncated := classifyAndCountFeatures(input.Categories, elements)

	detailLimit := resolveDetailLimit(pdk.GetConfig("detail_limit"))
	for _, idx := range nearestWikipediaFeatures(features, input.Lat, input.Lon, detailLimit) {
		title := wikipediaTitle(features[idx].tags["wikipedia"])
		if title == "" {
			continue
		}
		enrichWithWikipediaSummary(&features[idx], title)
	}

	output := wasmFetchPOIOutput{
		Features:    make([]wasmPOIFeatureOutput, 0, len(features)),
		Truncated:   truncated,
		Unsupported: []string{}, // osm-overpass maps every category in poiCategoryIDs
	}
	for _, f := range features {
		output.Features = append(output.Features, wasmPOIFeatureOutput{
			ID: f.ID, Category: f.Category, Name: f.Name, Lat: f.Lat, Lon: f.Lon,
			Detail: f.Detail, SourceURL: f.SourceURL,
		})
	}

	if err := pdk.OutputJSON(output); err != nil {
		pdk.SetError(err)
		return -1
	}
	return 0
}

// enrichWithWikipediaSummary fetches title's English Wikipedia REST page
// summary and, on success, sets f.Detail/f.SourceURL. Any failure (network
// error, non-2xx, unparseable body, no extract) leaves both fields empty -
// never a placeholder - per the plugin contract.
func enrichWithWikipediaSummary(f *poiFeatureOut, title string) {
	req := pdk.NewHTTPRequest(pdk.MethodGet, wikipediaSummaryURL(title))
	req.SetHeader("User-Agent", "helmcentral-osm-overpass-plugin/1.0")
	resp := req.Send()
	if resp.Status() < 200 || resp.Status() >= 300 {
		return
	}

	detail, sourceURL, err := parseWikipediaSummary(resp.Body())
	if err != nil {
		return
	}
	f.Detail = detail
	f.SourceURL = sourceURL
}

// doOverpassQuery posts one already-built Overpass QL query to the
// configured Overpass endpoint (resolveOverpassURL) and returns its parsed
// elements, or an error naming the failure - a transport error, a non-200
// status, a detected rate limit, a runtime error remark, or a JSON parse
// failure - never an empty success. Shared by the place_name_at and
// search_places wasmexports below so their osm-overpass.go orchestration
// (runPlaceNameAt, runSearchPlaces) stays plain-Go-testable via a fake
// overpassQueryFunc while this is the one place that actually performs the
// HTTP call for both. fetch_poi above keeps its own inline version since it
// also needs the response's Content-Type for its own rate-limit check at a
// second call site (Wikipedia enrichment uses a different endpoint entirely).
func doOverpassQuery(query string) ([]overpassElement, error) {
	overpassURL, err := resolveOverpassURL(pdk.GetConfig("overpass_url"))
	if err != nil {
		return nil, err
	}

	req := pdk.NewHTTPRequest(pdk.MethodPost, overpassURL)
	req.SetHeader("Content-Type", "application/x-www-form-urlencoded")
	req.SetHeader("User-Agent", "helmcentral-osm-overpass-plugin/1.0")
	req.SetBody([]byte("data=" + url.QueryEscape(query)))
	resp := req.Send()

	body := resp.Body()
	contentType := headerCaseInsensitive(resp.Headers(), "Content-Type")

	if resp.Status() != 200 {
		if looksLikeOverpassRateLimit(contentType, body) {
			return nil, fmt.Errorf("overpass rate limited (HTTP %d with a non-JSON body)", resp.Status())
		}
		return nil, fmt.Errorf("overpass returned status %d", resp.Status())
	}

	return parseOverpassBody(contentType, body)
}

//go:wasmexport place_name_at
func placeNameAt() int32 {
	var input placeNameAtInput
	if err := pdk.InputJSON(&input); err != nil {
		pdk.SetError(err)
		return -1
	}

	output, err := runPlaceNameAt(doOverpassQuery, input.Lat, input.Lon, input.RadiusM)
	if err != nil {
		pdk.SetError(err)
		return -1
	}

	if err := pdk.OutputJSON(output); err != nil {
		pdk.SetError(err)
		return -1
	}
	return 0
}

//go:wasmexport search_places
func searchPlaces() int32 {
	var input searchPlacesInput
	if err := pdk.InputJSON(&input); err != nil {
		pdk.SetError(err)
		return -1
	}

	output, err := runSearchPlaces(doOverpassQuery, input)
	if err != nil {
		pdk.SetError(err)
		return -1
	}

	if err := pdk.OutputJSON(output); err != nil {
		pdk.SetError(err)
		return -1
	}
	return 0
}

func main() {}
