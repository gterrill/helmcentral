//go:build tinygo
// +build tinygo

// google-places is a Helmcentral POI-provider WASM plugin backed by the
// Places API (New)'s Nearby Search endpoint. Unlike osm-overpass, it needs
// an operator-supplied API key (GOOGLE_PLACES_API_KEY) and only maps a
// subset of this codebase's eleven POI categories - see
// poiCategoryToGoogleTypes in google-places.go and this plugin's README for
// which, and why.
//
// This file (main.go) holds only the thin //go:wasmexport wrapper layer;
// all the actual request-building/parsing/mapping logic lives in
// google-places.go, which has no dependency on "github.com/extism/go-pdk"
// specifically so it (and main_test.go) can be built and tested with the
// plain host Go toolchain (`go test ./...`, no TinyGo/wasm target needed) -
// see docs/examples/wave-plugins/open-meteo-marine/main.go for the same
// split and the reasoning behind it. Always build the whole package
// directory (`.`), not just main.go by name - see this plugin's README.
package main

import (
	"fmt"
	"strings"

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
	pdk.OutputString("google-places")
	return 0
}

//go:wasmexport name
func name() int32 {
	pdk.OutputString("Google Places")
	return 0
}

//go:wasmexport description
func description() int32 {
	pdk.OutputString("Points of interest from Google's Places API (New). Needs an API key.")
	return 0
}

//go:wasmexport ttl_seconds
func ttlSeconds() int32 {
	// 6 hours - matches osm-overpass; Places API billing is per request, so
	// a long TTL plus the host's position-rounded cache cell both matter
	// here more than for a free provider.
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

	includedTypes, unsupported := mapCategoriesToIncludedTypes(input.Categories)

	output := wasmFetchPOIOutput{
		Features:    []wasmPOIFeatureOutput{},
		Truncated:   []string{},
		Unsupported: unsupported,
	}

	// Nothing requested has a Google equivalent at all - skip the (billed)
	// API call entirely rather than spend a request to learn what
	// mapCategoriesToIncludedTypes already knows.
	if len(includedTypes) == 0 {
		if err := pdk.OutputJSON(output); err != nil {
			pdk.SetError(err)
			return -1
		}
		return 0
	}

	apiKey, present := pdk.GetConfig("api_key")
	if !present || strings.TrimSpace(apiKey) == "" {
		pdk.SetErrorString("Google Places API key is not configured; set GOOGLE_PLACES_API_KEY")
		return -1
	}

	limit := input.Limit
	if limit <= 0 {
		limit = googleMaxResultCount
	}
	body, err := buildSearchNearbyRequestBody(input.Lat, input.Lon, input.RadiusM, includedTypes, limit)
	if err != nil {
		pdk.SetError(err)
		return -1
	}

	req := pdk.NewHTTPRequest(pdk.MethodPost, searchNearbyURL)
	req.SetHeader("Content-Type", "application/json")
	req.SetHeader("X-Goog-Api-Key", apiKey)
	req.SetHeader("X-Goog-FieldMask", "places.id,places.displayName,places.location,places.types,places.editorialSummary,places.googleMapsUri")
	req.SetBody(body)
	resp := req.Send()

	if resp.Status() < 200 || resp.Status() >= 300 {
		pdk.SetErrorString(fmt.Sprintf("places:searchNearby returned status %d: %s", resp.Status(), string(resp.Body())))
		return -1
	}

	features, err := parseSearchNearbyResponse(resp.Body(), input.Categories)
	if err != nil {
		pdk.SetError(err)
		return -1
	}

	output.Features = make([]wasmPOIFeatureOutput, 0, len(features))
	for _, f := range features {
		output.Features = append(output.Features, wasmPOIFeatureOutput{
			ID: f.ID, Category: f.Category, Name: f.Name, Lat: f.Lat, Lon: f.Lon,
			Detail: f.Detail, SourceURL: f.SourceURL,
		})
	}
	// Google's Nearby Search caps results at maxResultCount with no
	// "there were more" signal, exactly like Overpass's per-category cap -
	// see osm-overpass.go's truncatedCategories doc comment for the same
	// reasoning. Places has no per-category caps to attribute this to
	// individually, so every supported requested category is flagged when
	// the flat result count hits the cap.
	if len(features) >= clampInt(limit, 1, googleMaxResultCount) {
		isUnsupported := make(map[string]bool, len(unsupported))
		for _, id := range unsupported {
			isUnsupported[id] = true
		}
		for _, id := range input.Categories {
			if !isUnsupported[id] {
				output.Truncated = append(output.Truncated, id)
			}
		}
	}

	if err := pdk.OutputJSON(output); err != nil {
		pdk.SetError(err)
		return -1
	}
	return 0
}

func main() {}
