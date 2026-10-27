package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
)

// ── routed passage distance (ADR 0216) ────────────────────────────────────
//
// A passage distance is the length of a route over water, never the straight
// line between two places: islands and headlands are in the way. route_distance
// answers that on its own, for a leg between two places, and estimate_passage
// does the same before it works out time and fuel. Both go through routePassage.

var mateToolRouteDistance = mateTool{
	name: "route_distance",
	function: openRouterFunctionDef{
		Description: "How far it is by water between two positions, routed round islands and headlands over the " +
			"offline coastline held on board. Use it for the distance and first-leg bearing of any leg you " +
			"mention, planned or not, instead of working one out from two positions: a straight line often " +
			"crosses an island. Give to_lat and to_lon (from find_places); the run starts at the vessel's " +
			"position unless you give from_lat and from_lon. Returns distance_nm, straight_line_nm (for " +
			"comparison only, never a passage distance), bearing_deg of the first leg, and the waypoints. If it " +
			"returns an error (a coastline square is not downloaded, an end point is on land, no water route), " +
			"tell the operator you cannot give a distance and why, in the tool's words; never substitute a " +
			"straight line. For time and fuel use estimate_passage, which routes the same way.",
		Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"to_lat": {"type": "number", "description": "Destination latitude in decimal degrees (south negative)."},
						"to_lon": {"type": "number", "description": "Destination longitude in decimal degrees (west negative)."},
						"from_lat": {"type": "number", "description": "Start latitude. Omit both from_lat and from_lon to start at the vessel's position."},
						"from_lon": {"type": "number", "description": "Start longitude. Give both or neither."}
					},
					"required": ["to_lat", "to_lon"]
				}`),
	},
	execute: assistantToolDeps.executeRouteDistance,
	describe: func(args json.RawMessage) string {
		var a assistantRouteDistanceArgs
		if err := json.Unmarshal(args, &a); err != nil || a.ToLat == nil || a.ToLon == nil {
			return "Routing over water…"
		}
		return fmt.Sprintf("Routing to %.3f, %.3f over water…", *a.ToLat, *a.ToLon)
	},
}

type assistantRouteDistanceArgs struct {
	ToLat   *float64 `json:"to_lat"`
	ToLon   *float64 `json:"to_lon"`
	FromLat *float64 `json:"from_lat"`
	FromLon *float64 `json:"from_lon"`
}

// assistantRoutedPassage is a route over water as Mate sees it.
type assistantRoutedPassage struct {
	From assistantLatLon `json:"from"`
	To   assistantLatLon `json:"to"`
	// DistanceNm is the routed distance over water. StraightLineNm is the
	// straight line, for comparison only: it is never a passage distance.
	DistanceNm     float64 `json:"distance_nm"`
	StraightLineNm float64 `json:"straight_line_nm"`
	// BearingDeg is the true bearing of the first leg of the route: the
	// course for wind angle and tidal stream work, not the bearing straight
	// to the destination.
	BearingDeg int `json:"bearing_deg"`
	// Route is the waypoints from start to destination: a bend at each
	// headland the run rounds. Two points means clear water all the way.
	Route []assistantLatLon `json:"route"`
	// RouteNote is set when an end point was on the charted shore and was
	// measured from the water in front of it.
	RouteNote string `json:"route_note,omitempty"`

	rawDistanceNm float64
}

// routePassage routes a run over water from the given start (the vessel's
// position when nil) to the destination. Every failure says what is wrong
// and that there is no passage distance: the caller must not fall back to a
// straight line.
func (d assistantToolDeps) routePassage(tool string, fromLat, fromLon *float64, toLat, toLon float64) (assistantRoutedPassage, error) {
	if !hasUsableVesselPosition(toLat, toLon) {
		return assistantRoutedPassage{}, fmt.Errorf("%s: %g, %g is not a usable destination", tool, toLat, toLon)
	}
	if (fromLat == nil) != (fromLon == nil) {
		return assistantRoutedPassage{}, fmt.Errorf("%s: give both from_lat and from_lon, or neither to start at the vessel's position", tool)
	}
	var startLat, startLon float64
	if fromLat != nil {
		startLat, startLon = *fromLat, *fromLon
		if !hasUsableVesselPosition(startLat, startLon) {
			return assistantRoutedPassage{}, fmt.Errorf("%s: %g, %g is not a usable start", tool, startLat, startLon)
		}
	} else {
		if d.vesselState == nil {
			return assistantRoutedPassage{}, fmt.Errorf("%s: the vessel's position is not available; give from_lat and from_lon", tool)
		}
		state, serr := d.vesselState()
		lat, lon, ok := usableOwnShipFix(state, serr)
		if !ok {
			return assistantRoutedPassage{}, fmt.Errorf("%s: the vessel has no usable position fix, so the run cannot start from it; give from_lat and from_lon or ask the operator where it starts", tool)
		}
		startLat, startLon = lat, lon
	}
	if d.offlineCoastline == nil {
		return assistantRoutedPassage{}, fmt.Errorf("%s: no passage distance: Offline coastline is not available on this install, so the run cannot be routed over water", tool)
	}
	route, err := d.offlineCoastline().RouteOverWater(startLat, startLon, toLat, toLon)
	if err != nil {
		return assistantRoutedPassage{}, fmt.Errorf("%s: no passage distance: %w", tool, err)
	}

	note := ""
	switch {
	case route.StartMovedNm > 0 && route.EndMovedNm > 0:
		note = fmt.Sprintf("Both ends are charted on the shore; the run is measured from the water in front of them (%.2f nm and %.2f nm off).", route.StartMovedNm, route.EndMovedNm)
	case route.StartMovedNm > 0:
		note = fmt.Sprintf("The start is charted on the shore; the run is measured from the water in front of it (%.2f nm off).", route.StartMovedNm)
	case route.EndMovedNm > 0:
		note = fmt.Sprintf("The destination is charted on the shore; the run is measured to the water in front of it (%.2f nm off).", route.EndMovedNm)
	}
	waypoints := make([]assistantLatLon, len(route.Points))
	for i, p := range route.Points {
		waypoints[i] = assistantLatLon{Lat: roundTo5(p.Lat), Lon: roundTo5(p.Lon)}
	}
	return assistantRoutedPassage{
		From:           assistantLatLon{Lat: startLat, Lon: startLon},
		To:             assistantLatLon{Lat: toLat, Lon: toLon},
		DistanceNm:     roundTo1(route.DistanceNm),
		StraightLineNm: roundTo1(route.StraightLineNm),
		BearingDeg:     int(math.Round(route.InitialBearingDeg)) % 360,
		Route:          waypoints,
		RouteNote:      note,
		rawDistanceNm:  route.DistanceNm,
	}, nil
}

func (d assistantToolDeps) executeRouteDistance(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var args assistantRouteDistanceArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("parse route_distance arguments: %w", err)
	}
	if args.ToLat == nil || args.ToLon == nil {
		return "", fmt.Errorf("route_distance: to_lat and to_lon are required")
	}
	routed, err := d.routePassage("route_distance", args.FromLat, args.FromLon, *args.ToLat, *args.ToLon)
	if err != nil {
		return "", err
	}
	return capToolResultJSON(&routed, nil)
}
