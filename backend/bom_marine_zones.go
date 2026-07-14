package main

// stateForPosition maps a lat/lon position to an Australian state/territory
// code using a simple bounding-box table.
//
// State borders are not rectangular, so these boxes deliberately overlap
// slightly. This is a coastal approximation, not exact — that's acceptable
// for a marine app since vessels are near the coast, not sitting on
// ambiguous inland borders. Order matters: check more specific/eastern
// states first and return on the first match.
func stateForPosition(lat, lon float64) (state string, ok bool) {
	type bbox struct {
		state          string
		minLat, maxLat float64
		minLon, maxLon float64
	}

	boxes := []bbox{
		{"QLD", -29, -10, 138, 154},
		{"NSW", -37, -28, 141, 154},
		{"VIC", -39, -34, 141, 150},
		{"TAS", -44, -39, 143, 149},
		{"SA", -38, -26, 129, 141},
		{"WA", -35, -14, 112, 129},
		{"NT", -26, -11, 129, 138},
	}

	for _, b := range boxes {
		if lat >= b.minLat && lat <= b.maxLat && lon >= b.minLon && lon <= b.maxLon {
			return b.state, true
		}
	}

	return "", false
}

// zoneForPosition maps a lat/lon position within a given state to a BOM
// marine forecast zone name using an ordered latitude-band table.
func zoneForPosition(state string, lat, lon float64) (zone string, ok bool) {
	switch state {
	case "QLD":
		return qldZoneForPosition(lat, lon)
	case "NSW":
		return nswZoneForPosition(lat, lon)
	case "VIC":
		return vicZoneForPosition(lat, lon)
	case "TAS":
		return tasZoneForPosition(lat, lon)
	case "WA":
		return waZoneForPosition(lat, lon)
	default:
		// SA/NT: no warning product exists yet, so no zone table.
		return "", false
	}
}

// qldZoneForPosition uses verified real BOM marine zone names for
// Queensland's east coast, north to south, plus a Gulf of Carpentaria
// check on the west side. Boundaries are the actual landmarks BOM's own
// coastal waters forecasts name as each zone's extent (e.g. "Mackay
// Coastal Waters Forecast: Bowen to St Lawrence", "Capricornia Coastal
// Waters Forecast: St Lawrence to Burnett Heads" - bom.gov.au), not
// round-number approximations - a vessel sitting between the old
// approximate boundary and the real one would get warnings for the
// wrong zone. Sharp Point (Peninsula Coast's northern extent) is the one
// landmark left unverified; -10 (the state bounding box edge) stands in
// for it since no vessel position here has depended on that edge yet.
func qldZoneForPosition(lat, lon float64) (string, bool) {
	// Gulf of Carpentaria (west side of Cape York Peninsula).
	if lon < 142 && lat >= -17 && lat <= -11 {
		return "Gulf Waters", true
	}

	type band struct {
		zone           string
		minLat, maxLat float64
	}

	bands := []band{
		{"Peninsula Coast", -14.17, -10},          // Sharp Point (unverified) to Cape Melville
		{"Cooktown Coast", -16.09, -14.17},        // Cape Melville to Cape Tribulation
		{"Cairns Coast", -18.27, -16.09},          // Cape Tribulation to Cardwell
		{"Townsville Coast", -20.01, -18.27},      // Cardwell to Bowen
		{"Mackay Coast", -22.35, -20.01},          // Bowen to St Lawrence
		{"Capricornia Coast", -24.75, -22.35},     // St Lawrence to Burnett Heads/Sandy Cape
		{"K'gari Coast", -25.93, -24.75},          // Burnett Heads/Sandy Cape to Double Island Point
		{"Sunshine Coast Waters", -27.03, -25.93}, // Double Island Point to Cape Moreton
		{"Gold Coast Waters", -28.17, -27.03},     // Cape Moreton to Point Danger
	}

	for _, b := range bands {
		if lat > b.minLat && lat <= b.maxLat {
			return b.zone, true
		}
	}

	return "", false
}

// nswZoneForPosition uses verified real BOM marine zone names for the NSW
// coast, north to south. "Sydney Enclosed Waters" is intentionally omitted —
// open-water positions near Sydney match "Sydney Coast".
func nswZoneForPosition(lat, lon float64) (string, bool) {
	type band struct {
		zone           string
		minLat, maxLat float64
	}

	bands := []band{
		{"Byron Coast", -29.5, -28.0},
		{"Coffs Coast", -31.0, -29.5},
		{"Macquarie Coast", -32.0, -31.0},
		{"Hunter Coast", -33.0, -32.0},
		{"Sydney Coast", -34.2, -33.0},
		{"Illawarra Coast", -35.2, -34.2},
		{"Batemans Coast", -36.0, -35.2},
		{"Eden Coast", -37.5, -36.0},
	}

	for _, b := range bands {
		if lat > b.minLat && lat <= b.maxLat {
			return b.zone, true
		}
	}

	return "", false
}

// APPROXIMATE — not verified against BOM's official marine zone maps, only
// "East Gippsland Coast" (VIC) was confirmed live during research. Verify
// before relying on these boundaries.
func vicZoneForPosition(lat, lon float64) (string, bool) {
	type band struct {
		zone           string
		minLat, maxLat float64
	}

	bands := []band{
		{"East Gippsland Coast", -39, -37.5},
	}

	for _, b := range bands {
		if lat > b.minLat && lat <= b.maxLat {
			return b.zone, true
		}
	}

	return "", false
}

// APPROXIMATE — not verified against BOM's official marine zone maps.
// No zone names have been confirmed live for TAS yet. Verify before relying
// on these boundaries.
func tasZoneForPosition(lat, lon float64) (string, bool) {
	return "", false
}

// APPROXIMATE — not verified against BOM's official marine zone maps.
// No zone names have been confirmed live for WA yet. Verify before relying
// on these boundaries.
func waZoneForPosition(lat, lon float64) (string, bool) {
	return "", false
}
