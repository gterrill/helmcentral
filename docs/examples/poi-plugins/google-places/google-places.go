// google-places.go holds the request-building, response-parsing and
// category-mapping logic for the Google Places (New) POI-provider plugin,
// kept in a separate file from main.go deliberately: this file has no
// dependency on "github.com/extism/go-pdk", so it (and main_test.go, which
// exercises it) can be built and tested with the plain host Go toolchain
// (`go test ./...`, no TinyGo/wasm target needed) - see main.go's doc
// comment and docs/examples/wave-plugins/open-meteo-marine/main.go for the
// same split and the reasoning behind it. main.go's //go:wasmexport
// functions call straight into these same functions.
package main

import (
	"encoding/json"
	"fmt"
)

const searchNearbyURL = "https://places.googleapis.com/v1/places:searchNearby"

// googleMaxRadiusMeters and googleMaxResultCount are Places API (New)'s
// documented hard limits for Nearby Search - locationRestriction.circle's
// radius tops out at 50,000m and maxResultCount at 20.
const (
	googleMaxRadiusMeters = 50000
	googleMaxResultCount  = 20
)

// poiCategoryToGoogleTypes maps this codebase's POI category ids
// (docs/reference/poi-categories.md) onto Google Places API (New) Table A
// type identifiers (includedTypes). A category absent from this map, or
// present with an empty slice, has no Google equivalent at all and is
// reported in the fetch_poi response's "unsupported" list rather than
// silently dropped or mapped to something misleading.
//
// Verified against Google's Table A (developers.google.com/maps/
// documentation/places/web-service/place-types) at the time this plugin was
// written: there is no dedicated boat-ramp or scuba-diving/dive-shop type,
// so ramp and dive are unsupported. anchorage, bay, island, fuel and
// mooring have no Places equivalent either - Places models businesses and
// named attractions, not physical landform/navigational features or fuel
// specifically for boats (mapping fuel to the generic "gas_station" type
// would return car filling stations, which is worse than reporting the
// category unsupported).
var poiCategoryToGoogleTypes = map[string][]string{
	"marina":    {"marina"},
	"historic":  {"historical_landmark", "tourist_attraction"},
	"viewpoint": {"scenic_spot"},
	"trail":     {"hiking_area", "park"},
}

// poiCategoryOrder is the fixed precedence used when a single Google place
// carries types that map back to more than one requested category (e.g. a
// place tagged both "historical_landmark" and "tourist_attraction" is
// merely "historic" once, not double-counted; a place that happens to
// straddle two DIFFERENT requested categories picks the first one here).
var poiCategoryOrder = []string{
	"anchorage", "bay", "island", "marina", "fuel", "ramp",
	"mooring", "historic", "viewpoint", "dive", "trail",
}

// requestedInFixedOrder returns every entry of categories exactly once,
// ordered by poiCategoryOrder's fixed precedence first, with any category
// id NOT in that table (a genuine caller mistake, or a future
// poiCategoryIDs addition this plugin hasn't caught up with yet) appended
// afterward in its original order - so every requested category is visited
// by mapCategoriesToIncludedTypes/classifyGooglePlace exactly once,
// unknown ids included, rather than silently skipped by an iteration that
// only walks the known table.
func requestedInFixedOrder(categories []string) []string {
	requested := make(map[string]bool, len(categories))
	for _, c := range categories {
		requested[c] = true
	}

	ordered := make([]string, 0, len(categories))
	seen := make(map[string]bool, len(categories))
	for _, id := range poiCategoryOrder {
		if requested[id] {
			ordered = append(ordered, id)
			seen[id] = true
		}
	}
	for _, id := range categories {
		if !seen[id] {
			ordered = append(ordered, id)
			seen[id] = true
		}
	}
	return ordered
}

// mapCategoriesToIncludedTypes returns the deduplicated, order-stable union
// of Google types for every requested category that has a mapping, plus
// the list of requested categories that have none (including any category
// id this plugin doesn't recognise at all).
func mapCategoriesToIncludedTypes(categories []string) (includedTypes []string, unsupported []string) {
	seen := make(map[string]bool)

	for _, id := range requestedInFixedOrder(categories) {
		types, ok := poiCategoryToGoogleTypes[id]
		if !ok || len(types) == 0 {
			unsupported = append(unsupported, id)
			continue
		}
		for _, t := range types {
			if !seen[t] {
				seen[t] = true
				includedTypes = append(includedTypes, t)
			}
		}
	}
	return includedTypes, unsupported
}

// classifyGooglePlace returns the first requested category (fixed
// precedence order) whose Google type list intersects place's own types.
func classifyGooglePlace(categories []string, placeTypes []string) (string, bool) {
	placeTypeSet := make(map[string]bool, len(placeTypes))
	for _, t := range placeTypes {
		placeTypeSet[t] = true
	}

	for _, id := range requestedInFixedOrder(categories) {
		for _, t := range poiCategoryToGoogleTypes[id] {
			if placeTypeSet[t] {
				return id, true
			}
		}
	}
	return "", false
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// searchNearbyRequest mirrors the Places API (New) Nearby Search request
// body shape exactly (https://developers.google.com/maps/documentation/
// places/web-service/nearby-search).
type searchNearbyRequest struct {
	IncludedTypes       []string `json:"includedTypes"`
	MaxResultCount      int      `json:"maxResultCount"`
	LocationRestriction struct {
		Circle struct {
			Center struct {
				Latitude  float64 `json:"latitude"`
				Longitude float64 `json:"longitude"`
			} `json:"center"`
			Radius float64 `json:"radius"`
		} `json:"circle"`
	} `json:"locationRestriction"`
}

// buildSearchNearbyRequestBody marshals the Nearby Search (New) request
// body for the given position/radius/included types, clamping radius and
// result count to Google's documented limits (defensive - the host already
// validates radius_nm to at most 25nm/46.3km, under the 50km cap, and limit
// to at most 100, over Google's 20 cap).
func buildSearchNearbyRequestBody(lat, lon float64, radiusM int, includedTypes []string, maxResultCount int) ([]byte, error) {
	var req searchNearbyRequest
	req.IncludedTypes = includedTypes
	req.MaxResultCount = clampInt(maxResultCount, 1, googleMaxResultCount)
	req.LocationRestriction.Circle.Center.Latitude = lat
	req.LocationRestriction.Circle.Center.Longitude = lon
	req.LocationRestriction.Circle.Radius = float64(clampInt(radiusM, 1, googleMaxRadiusMeters))

	return json.Marshal(req)
}

// googlePlace mirrors the subset of the Places API (New) Place resource
// named by this plugin's FieldMask
// (places.id,places.displayName,places.location,places.types,
// places.editorialSummary,places.googleMapsUri) - see main.go. No photo
// fields are requested at all: Google's photo URLs embed the API key in a
// URL the browser would fetch directly, which this plugin's contract
// (no photo URLs) rules out.
type googlePlace struct {
	ID          string `json:"id"`
	DisplayName struct {
		Text string `json:"text"`
	} `json:"displayName"`
	Location struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	} `json:"location"`
	Types            []string `json:"types"`
	EditorialSummary struct {
		Text string `json:"text"`
	} `json:"editorialSummary"`
	GoogleMapsURI string `json:"googleMapsUri"`
}

type searchNearbyResponse struct {
	Places []googlePlace `json:"places"`
}

// parseSearchNearbyResponse parses the Places API (New) response and
// classifies each place back into one of the requested categories,
// dropping any place that (unexpectedly, since includedTypes already
// constrained the search) doesn't map to any of them.
func parseSearchNearbyResponse(body []byte, categories []string) ([]poiFeatureOut, error) {
	var parsed searchNearbyResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse places:searchNearby response: %w", err)
	}

	features := make([]poiFeatureOut, 0, len(parsed.Places))
	for _, p := range parsed.Places {
		category, ok := classifyGooglePlace(categories, p.Types)
		if !ok {
			continue
		}
		features = append(features, poiFeatureOut{
			ID:        p.ID,
			Category:  category,
			Name:      p.DisplayName.Text,
			Lat:       p.Location.Latitude,
			Lon:       p.Location.Longitude,
			Detail:    p.EditorialSummary.Text,
			SourceURL: p.GoogleMapsURI,
		})
	}
	return features, nil
}

// poiFeatureOut mirrors backend/wasm_poi_provider.go's
// wasmPOIFeatureOutput.
type poiFeatureOut struct {
	ID        string
	Category  string
	Name      string
	Lat       float64
	Lon       float64
	Detail    string
	SourceURL string
}
