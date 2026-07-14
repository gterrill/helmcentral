package main

import "testing"

// TestQldZoneForPosition_MackayCapricorniaBoundary guards the Mackay
// Coast / Capricornia Coast boundary against drifting from BOM's real
// zone definitions ("Mackay Coastal Waters Forecast: Bowen to St
// Lawrence"; "Capricornia Coastal Waters Forecast: St Lawrence to
// Burnett Heads" - both bom.gov.au). St Lawrence sits at -22.3476,
// so a vessel at -21.5953 (well north of St Lawrence, near Mackay) must
// resolve to Mackay Coast, not Capricornia Coast.
func TestQldZoneForPosition_MackayCapricorniaBoundary(t *testing.T) {
	// 21°35'43.1"S 149°47'47.3"E - a real vessel fix south of Mackay,
	// north of St Lawrence.
	lat, lon := -21.5953, 149.7965

	zone, ok := qldZoneForPosition(lat, lon)
	if !ok {
		t.Fatalf("qldZoneForPosition(%v, %v) = not ok, want Mackay Coast", lat, lon)
	}
	if zone != "Mackay Coast" {
		t.Errorf("qldZoneForPosition(%v, %v) = %q, want %q", lat, lon, zone, "Mackay Coast")
	}
}

func TestQldZoneForPosition_JustSouthOfStLawrenceIsCapricornia(t *testing.T) {
	lat, lon := -22.4, 150.0

	zone, ok := qldZoneForPosition(lat, lon)
	if !ok {
		t.Fatalf("qldZoneForPosition(%v, %v) = not ok, want Capricornia Coast", lat, lon)
	}
	if zone != "Capricornia Coast" {
		t.Errorf("qldZoneForPosition(%v, %v) = %q, want %q", lat, lon, zone, "Capricornia Coast")
	}
}
