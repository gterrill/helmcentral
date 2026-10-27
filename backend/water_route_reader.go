package main

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// waterRouteMargins are the corridor margins tried in turn, in nautical
// miles. A narrow corridor is fast and nearly always enough; a wider one is
// tried only when the narrow one found no path. Each needs its own coverage
// squares, so a route near the edge of the held coastline reports the missing
// square instead of giving a path that might be too long.
var waterRouteMargins = []float64{3, 8, 20, 50}

// squaresInBox returns the coverage squares a box touches.
func squaresInBox(south, west, north, east float64) ([]coverageSquare, error) {
	if west < -180 || east > 180 {
		return nil, fmt.Errorf("%w: the route area crosses the 180° meridian", errCoastlineUnsupportedArea)
	}
	if math.Abs(south) > coastlineMaxLat || math.Abs(north) > coastlineMaxLat {
		return nil, fmt.Errorf("%w: the route area reaches a pole", errCoastlineUnsupportedArea)
	}
	var out []coverageSquare
	for la := int(math.Floor(south)); la <= int(math.Floor(north)); la++ {
		for lo := int(math.Floor(west)); lo <= int(math.Floor(east)); lo++ {
			out = append(out, coverageSquare{Lat: la, Lon: lo})
		}
	}
	return out, nil
}

// shorelineInBox is the shoreline of every square a box touches. Every
// square must be held (an inland square that was never downloaded holds no
// coastline by definition). A square that is not is named in the error: a
// route over a coastline with a hole in it could cross land.
func (r *offlineCoastlineReader) shorelineInBox(south, west, north, east float64) ([]shorelineLine, error) {
	if r == nil || r.Store == nil {
		return nil, fmt.Errorf("Offline coastline is not available on this install")
	}
	enabled, err := r.Enabled()
	if err != nil {
		return nil, fmt.Errorf("could not read the Offline coastline setting: %w", err)
	}
	if !enabled {
		return nil, fmt.Errorf("Offline coastline is off — turn it on in Settings → Offline coastline")
	}
	needed, err := squaresInBox(south, west, north, east)
	if err != nil {
		return nil, err
	}
	recs, err := r.Store.Records()
	if err != nil {
		return nil, err
	}
	byKey := make(map[coverageSquare]coastlineSquareRecord, len(recs))
	for _, rec := range recs {
		byKey[rec.Square] = rec
	}
	var used []coverageSquare
	var missing []string
	for _, sq := range needed {
		rec, held := byKey[sq]
		switch {
		case held && rec.Status == coastlineStatusReady:
			used = append(used, sq)
		case squareInland(sq):
			// no sea within 20 nm, so no coastline to hold
		case held && rec.Status == coastlineStatusFailed:
			missing = append(missing, fmt.Sprintf("%s (download failed: %s)", sq.Label(), rec.LastError))
		default:
			missing = append(missing, fmt.Sprintf("%s (not downloaded)", sq.Label()))
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("the route needs Offline coastline squares that are not held: %s. Helmcentral holds the squares within %.0f nm of the boat and any it downloaded earlier",
			strings.Join(missing, ", "), coastlineCoverageRadiusNm)
	}
	return r.Store.Lines(used)
}

// RouteOverWater is the shortest path over water between two positions,
// from the coastline held on board. It never returns the straight line in
// place of a route: when the coastline it needs is missing, an end point is
// on land, or no water connects the two, it says so.
func (r *offlineCoastlineReader) RouteOverWater(fromLat, fromLon, toLat, toLon float64) (waterRoute, error) {
	for _, e := range []struct {
		name     string
		lat, lon float64
	}{{"start", fromLat, fromLon}, {"destination", toLat, toLon}} {
		sq, err := squareContaining(e.lat, e.lon)
		if err != nil {
			return waterRoute{}, err
		}
		if squareInland(sq) {
			return waterRoute{}, fmt.Errorf("the %s (%.4f, %.4f) is inland, well clear of the sea", e.name, e.lat, e.lon)
		}
	}
	from, to := shorelinePoint{Lat: fromLat, Lon: fromLon}, shorelinePoint{Lat: toLat, Lon: toLon}
	if straight := haversineMeters(fromLat, fromLon, toLat, toLon) / metersPerNauticalMile; straight > waterRouteMaxStraightNm {
		return waterRoute{}, fmt.Errorf("a %.0f nm run is more than the %.0f nm that can be routed over water in one go; plan it in legs", straight, waterRouteMaxStraightNm)
	}

	try := func(margin float64) (waterRoute, error) {
		dLat := margin / 60
		dLon := dLat / math.Cos(math.Max(math.Abs(fromLat), math.Abs(toLat))*math.Pi/180)
		lines, err := r.shorelineInBox(
			math.Min(fromLat, toLat)-dLat, math.Min(fromLon, toLon)-dLon,
			math.Max(fromLat, toLat)+dLat, math.Max(fromLon, toLon)+dLon)
		if err != nil {
			return waterRoute{}, err
		}
		return routeOverWater(from, to, lines, margin)
	}

	var lastErr error
	for _, m := range waterRouteMargins {
		route, err := try(m)
		if errors.Is(err, errNoWaterRoute) {
			lastErr = err
			continue
		}
		if err != nil {
			return waterRoute{}, err
		}
		if route.ExactWithinNm > m {
			// A shorter path could lie outside this corridor: search the
			// corridor the path found allows, which settles it.
			return try(route.ExactWithinNm + 0.1)
		}
		return route, nil
	}
	return waterRoute{}, lastErr
}
