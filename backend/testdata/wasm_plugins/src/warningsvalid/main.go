//go:build !std
// +build !std

// warningsvalid is a minimal but complete forecast-warnings-provider WASM
// fixture, implementing the full plugin contract
// (id/name/ttl_seconds/fetch_warnings) with small, fixed test data: 2
// bulletins, one with multiple sections and a populated issued_at, and one
// with a single section and an empty issued_at (covering the contract's
// optional-field path). See backend/wasm_forecast_warnings_provider_test.go
// for the regeneration command.
package main

import (
	"github.com/extism/go-pdk"
)

type fetchWarningsInput struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type sectionOut struct {
	Day         string `json:"day"`
	WarningType string `json:"warning_type"`
}

type bulletinOut struct {
	ID         string       `json:"id"`
	Title      string       `json:"title"`
	Category   string       `json:"category"`
	IssuedAt   string       `json:"issued_at"`
	DetailsURL string       `json:"details_url"`
	Sections   []sectionOut `json:"sections"`
}

type fetchWarningsOutput struct {
	Region    string        `json:"region"`
	Bulletins []bulletinOut `json:"bulletins"`
}

//go:wasmexport id
func id() int32 {
	pdk.OutputString("warnings-valid-fixture")
	return 0
}

//go:wasmexport name
func name() int32 {
	pdk.OutputString("Forecast Warnings Valid Fixture Provider")
	return 0
}

//go:wasmexport ttl_seconds
func ttlSeconds() int32 {
	pdk.OutputString("900")
	return 0
}

//go:wasmexport fetch_warnings
func fetchWarnings() int32 {
	var input fetchWarningsInput
	if err := pdk.InputJSON(&input); err != nil {
		pdk.SetError(err)
		return -1
	}

	out := fetchWarningsOutput{
		Region: "QLD — Capricornia Coast",
		Bulletins: []bulletinOut{
			{
				ID:         "IDQ20085",
				Title:      "Strong Wind Warning for Small Craft Operators",
				Category:   "wind",
				IssuedAt:   "2026-07-19T11:51:00Z",
				DetailsURL: "https://www.bom.gov.au/fwo/IDQ20085.txt",
				Sections: []sectionOut{
					{Day: "Sunday", WarningType: "Strong Wind Warning"},
					{Day: "Monday", WarningType: "Strong Wind Warning"},
				},
			},
			{
				ID:         "IDQ20083",
				Title:      "Marine Wind Warning",
				Category:   "wind",
				IssuedAt:   "",
				DetailsURL: "https://www.bom.gov.au/fwo/IDQ20083.txt",
				Sections: []sectionOut{
					{Day: "Sunday", WarningType: "Gale Warning"},
				},
			},
		},
	}

	if err := pdk.OutputJSON(out); err != nil {
		pdk.SetError(err)
		return -1
	}
	return 0
}

func main() {}
