package main

import (
	"context"
	"encoding/json"
	"fmt"
)

var mateToolAnchorageShelter = mateTool{
	name: "anchorage_shelter",
	function: openRouterFunctionDef{
		Description: "Estimate how much wind and sea shelter an anchorage has, from the charted OpenStreetMap " +
			"shoreline (coastline and reef lines) held on board as the Offline coastline. Returns the compass arcs the position is sheltered, " +
			"partly sheltered and exposed from, and a table of the fetch (distance to the nearest " +
			"obstruction, up to 15 nm) for every 10 degrees with what blocks it. Wind directions are where " +
			"the wind comes FROM. This is an estimate from shoreline geometry only: it does not account for " +
			"wind bending round headlands, gusts funnelling down valleys, or swell wrapping round a point " +
			"or over a reef, so always compare its sheltered arcs with the forecast wind direction for the " +
			"stay (get_wind_forecast). " + ruleShelter + " If the position is inside a reef, shelter " +
			"depends on the tide. Resolve a place name with find_places first and pass its lat/lon. It works only " +
			"where the Offline coastline has been downloaded; if it returns an error, tell the operator what " +
			"it says (turned off, outside coverage, still downloading) rather than guessing the shelter.",
		Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"lat": {
							"type": "number",
							"description": "Latitude of the anchorage in decimal degrees (south negative)."
						},
						"lon": {
							"type": "number",
							"description": "Longitude of the anchorage in decimal degrees (west negative)."
						}
					},
					"required": ["lat", "lon"]
				}`),
	},
	execute: assistantToolDeps.executeAnchorageShelter,
	describe: func(args json.RawMessage) string {
		return "Checking anchorage shelter…"
	},
}

type assistantAnchorageShelterArgs struct {
	Lat *float64 `json:"lat"`
	Lon *float64 `json:"lon"`
}

type assistantAnchorageShelterResult struct {
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
	Source string  `json:"source"`
	// CoastlineDownloaded is the date of the oldest coverage square the
	// estimate used, so Mate can say how old the shoreline is.
	CoastlineDownloaded string `json:"coastline_downloaded"`
	// Reading says how old the shoreline is.
	Reading string `json:"reading"`
	shelterEstimate
}

func (d assistantToolDeps) executeAnchorageShelter(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var args assistantAnchorageShelterArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("parse anchorage_shelter arguments: %w", err)
	}
	if args.Lat == nil || args.Lon == nil {
		return "", fmt.Errorf("anchorage_shelter: lat and lon are required")
	}
	if !hasUsableVesselPosition(*args.Lat, *args.Lon) {
		return "", fmt.Errorf("anchorage_shelter: %g, %g is not a usable position", *args.Lat, *args.Lon)
	}

	if d.offlineCoastline == nil {
		return "", fmt.Errorf("anchorage_shelter: Offline coastline is not available on this install")
	}
	set, err := d.offlineCoastline().ShorelineAround(*args.Lat, *args.Lon)
	if err != nil {
		return "", fmt.Errorf("anchorage_shelter: %w", err)
	}

	result := assistantAnchorageShelterResult{
		Lat: *args.Lat, Lon: *args.Lon, Source: "Offline coastline (OpenStreetMap)",
		CoastlineDownloaded: set.Oldest.UTC().Format("2006-01-02"),
		shelterEstimate:     estimateShelter(*args.Lat, *args.Lon, set.Lines),
	}
	result.Reading = readingShelter(result.CoastlineDownloaded)
	return capToolResultJSON(&result, nil)
}
