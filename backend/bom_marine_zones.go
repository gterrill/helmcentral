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
// check on the west side.
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
		{"Peninsula Coast", -12.5, -10},
		{"Cooktown Coast", -15.5, -12.5},
		{"Cairns Coast", -17.5, -15.5},
		{"Townsville Coast", -19.5, -17.5},
		{"Mackay Coast", -21.5, -19.5},
		{"Capricornia Coast", -24.5, -21.5},
		{"K'gari Coast", -26.0, -24.5},
		{"Sunshine Coast Waters", -27.0, -26.0},
		{"Gold Coast Waters", -28.0, -27.0},
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
