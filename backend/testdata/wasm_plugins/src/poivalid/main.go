//go:build !std
// +build !std

// poivalid is a minimal but complete POI-provider WASM fixture,
// implementing the full plugin contract (id/name/ttl_seconds/fetch_poi)
// with small, fixed test data: three features, one carrying detail and
// source_url (the Wikipedia-enrichment happy path), plus a non-empty
// truncated and unsupported list so the host's pass-through of both is
// exercised. See backend/wasm_poi_provider_test.go for the regeneration
// command. Position/radius/categories/limit from the input are read but
// ignored - the fixture is deliberately request-independent, since
// distance/bearing/radius-filtering are host-side concerns exercised
// against a stub provider in backend/poi_providers_test.go instead.
package main

import (
	"github.com/extism/go-pdk"
)

type fetchPOIInput struct {
	Lat        float64  `json:"lat"`
	Lon        float64  `json:"lon"`
	RadiusM    int      `json:"radius_m"`
	Categories []string `json:"categories"`
	Limit      int      `json:"limit"`
}

type featureOut struct {
	ID        string  `json:"id"`
	Category  string  `json:"category"`
	Name      string  `json:"name"`
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
	Detail    string  `json:"detail"`
	SourceURL string  `json:"source_url"`
}

type fetchPOIOutput struct {
	Features    []featureOut `json:"features"`
	Truncated   []string     `json:"truncated"`
	Unsupported []string     `json:"unsupported"`
}

//go:wasmexport id
func id() int32 {
	pdk.OutputString("poi-valid-fixture")
	return 0
}

//go:wasmexport name
func name() int32 {
	pdk.OutputString("POI Valid Fixture Provider")
	return 0
}

//go:wasmexport ttl_seconds
func ttlSeconds() int32 {
	pdk.OutputString("21600")
	return 0
}

//go:wasmexport fetch_poi
func fetchPOI() int32 {
	var input fetchPOIInput
	if err := pdk.InputJSON(&input); err != nil {
		pdk.SetError(err)
		return -1
	}

	out := fetchPOIOutput{
		Features: []featureOut{
			{ID: "node/1", Category: "anchorage", Name: "Sample Anchorage", Lat: input.Lat, Lon: input.Lon},
			{
				ID: "node/2", Category: "historic", Name: "Lindeman Island",
				Lat: input.Lat + 0.01, Lon: input.Lon + 0.01,
				Detail:    "Lindeman Island is a continental island in the Whitsunday Islands.",
				SourceURL: "https://en.wikipedia.org/wiki/Lindeman_Island",
			},
			{ID: "way/3", Category: "viewpoint", Name: "Sample Lookout", Lat: input.Lat - 0.01, Lon: input.Lon - 0.01},
		},
		Truncated:   []string{"mooring"},
		Unsupported: []string{"dive"},
	}

	if err := pdk.OutputJSON(out); err != nil {
		pdk.SetError(err)
		return -1
	}
	return 0
}

func main() {}
