package main

import (
	"context"
	"encoding/json"
	"fmt"
)

var mateToolAnchorageShelter = mateTool{
	name: "anchorage_shelter",
	function: openRouterFunctionDef{
		Description: "Estimate how much wind and sea shelter an anchorage has, from charted OpenStreetMap " +
			"shoreline (coastline and reef lines). Returns the compass arcs the position is sheltered, " +
			"partly sheltered and exposed from, and a table of the fetch (distance to the nearest " +
			"obstruction, up to 15 nm) for every 10 degrees with what blocks it. Wind directions are where " +
			"the wind comes FROM. This is an estimate from shoreline geometry only: it does not account for " +
			"wind bending round headlands, gusts funnelling down valleys, or swell wrapping round a point " +
			"or over a reef, so always compare its sheltered arcs with the forecast wind direction for the " +
			"stay (get_wind_forecast) and say it is an estimate. If the position is inside a reef, shelter " +
			"depends on the tide. Resolve a place name with find_places first and pass its lat/lon.",
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

	provider, providerID, err := d.placeNames()
	if err != nil {
		return "", fmt.Errorf("anchorage_shelter: %w", err)
	}
	lines, err := shorelineNearCached(provider, providerID, *args.Lat, *args.Lon)
	if err != nil {
		return "", fmt.Errorf("anchorage_shelter: %w", err)
	}

	result := assistantAnchorageShelterResult{
		Lat: *args.Lat, Lon: *args.Lon, Source: providerID,
		shelterEstimate: estimateShelter(*args.Lat, *args.Lon, lines),
	}
	return capToolResultJSON(&result, nil)
}
