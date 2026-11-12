package main

import (
	"testing"
)

// The bearing sector (ADR 0282): the nearest named land feature within +/-25
// degrees of the way the camera pointed, out to 10 nautical miles.

func TestInSectorWrapsAroundNorth(t *testing.T) {
	cases := []struct {
		camera, feature float64
		want            bool
	}{
		{0, 350, true},   // 10 to port of north
		{0, 10, true},    // 10 to starboard of north
		{359, 20, true},  // 21 apart across 0/360
		{10, 350, true},  // 20 apart across 0/360
		{355, 25, false}, // 30 apart across 0/360
		{180, 180, true},
		{180, 0, false},
	}
	for _, c := range cases {
		if got := inSector(c.camera, c.feature); got != c.want {
			t.Errorf("inSector(%v, %v) = %v, want %v", c.camera, c.feature, got, c.want)
		}
	}
}

func TestInSectorEdgesAreInclusive(t *testing.T) {
	if !inSector(0, 25) || !inSector(0, 335) {
		t.Error("exactly 25 degrees either side is inside")
	}
	if inSector(0, 25.01) || inSector(0, 334.99) {
		t.Error("just over 25 degrees is outside")
	}
	if !inSector(100, 125) || !inSector(100, 75) || inSector(100, 125.5) {
		t.Error("the edges away from north behave the same")
	}
}

func TestCameraBearingTrueUsesTheReference(t *testing.T) {
	ten := 10.0
	west := -12.0
	cases := []struct {
		name string
		deg  float64
		ref  string
		vari *float64
		want float64
	}{
		{"true stays", 100, "T", &ten, 100},
		{"no reference is read as true", 100, "", &ten, 100},
		{"magnetic plus east variation", 100, "M", &ten, 110},
		{"magnetic wraps past 360", 355, "M", &ten, 5},
		{"west variation subtracts and wraps below 0", 5, "M", &west, 353},
		{"magnetic with no variation known is read as true", 100, "M", nil, 100},
	}
	for _, c := range cases {
		if got := cameraBearingTrue(c.deg, c.ref, c.vari); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestNearbyFeaturesStopAtTenMilesAndSortByDistance(t *testing.T) {
	// One degree of latitude in the metres haversineMeters uses, to place
	// features just inside and just outside 10 nm due north of the boat.
	perDeg := haversineMeters(0, 0, 1, 0)
	at := func(nm float64) float64 { return nm * 1852 / perDeg }
	raw := []poiFeatureRaw{
		{Name: "Far Rock", Category: "island", Lat: at(10.02), Lon: 0},
		{Name: "Edge Island", Category: "island", Lat: at(9.98), Lon: 0},
		{Name: "Near Bay", Category: "bay", Lat: at(1), Lon: 0},
		{Name: "", Category: "bay", Lat: at(0.5), Lon: 0}, // unnamed: never offered
	}
	got := nearbyFeatures(raw, 0, 0)
	if len(got) != 2 || got[0].Name != "Near Bay" || got[1].Name != "Edge Island" {
		t.Fatalf("got %+v", got)
	}
	if got[0].DistanceNm < 0.99 || got[0].DistanceNm > 1.01 || got[0].BearingDeg > 0.5 && got[0].BearingDeg < 359.5 {
		t.Errorf("distance or bearing off: %+v", got[0])
	}
}

func TestNearestInSectorSkipsNearerFeaturesOutsideIt(t *testing.T) {
	features := []inboxFeature{
		{Name: "Behind", DistanceNm: 0.5, BearingDeg: 200},
		{Name: "Left of it", DistanceNm: 1, BearingDeg: 60},
		{Name: "On the line", DistanceNm: 4, BearingDeg: 95},
		{Name: "Beyond", DistanceNm: 8, BearingDeg: 100},
	}
	got, ok := nearestInSector(features, 90)
	if !ok || got.Name != "On the line" {
		t.Fatalf("got %+v %v", got, ok)
	}
	if _, ok := nearestInSector(features, 300); ok {
		t.Error("nothing lies that way")
	}
	if _, ok := nearestInSector(nil, 90); ok {
		t.Error("no features, no answer")
	}
}

func TestMarkSectorFlagsEveryCandidateInTheSector(t *testing.T) {
	features := []inboxFeature{{Name: "A", BearingDeg: 88}, {Name: "B", BearingDeg: 200}, {Name: "C", BearingDeg: 114}}
	got := markSector(features, 90)
	if !got[0].InSector || got[1].InSector || !got[2].InSector {
		t.Fatalf("%+v", got)
	}
}

func TestFeatureDescriptionGivesDistanceAndSideOfTheBow(t *testing.T) {
	heading := 90.0
	cases := []struct {
		bearing float64
		want    string
	}{
		{50, "Cape Capricorn, 3.1 nm, 40° to port"},
		{140, "Cape Capricorn, 3.1 nm, 50° to starboard"},
		{90, "Cape Capricorn, 3.1 nm, dead ahead"},
		{270, "Cape Capricorn, 3.1 nm, dead astern"},
	}
	for _, c := range cases {
		f := inboxFeature{Name: "Cape Capricorn", DistanceNm: 3.1, BearingDeg: c.bearing}
		if got := f.describe(&heading); got != c.want {
			t.Errorf("bearing %v: %q, want %q", c.bearing, got, c.want)
		}
	}
	// With no heading the true bearing is all there is to say.
	f := inboxFeature{Name: "Cape Capricorn", DistanceNm: 3.1, BearingDeg: 50}
	if got := f.describe(nil); got != "Cape Capricorn, 3.1 nm, bearing 050° true" {
		t.Errorf("no heading: %q", got)
	}
}
