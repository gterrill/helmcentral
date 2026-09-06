package main

import (
	"testing"
	"time"
)

// withinTolerance reports whether got is within tolerance of want.
func withinTolerance(t *testing.T, label string, got, want time.Time, tolerance time.Duration) {
	t.Helper()
	diff := got.Sub(want)
	if diff < 0 {
		diff = -diff
	}
	if diff > tolerance {
		t.Errorf("%s: got %s, want %s (+-%s), diff %s", label, got.UTC().Format(time.RFC3339), want.UTC().Format(time.RFC3339), tolerance, diff)
	}
}

// The vessel's position and provider-reported sunrise/sunset times captured
// live on 2026-09-07 (backend/testdata/sun_times_provider.json). Australia/Brisbane
// runs AEST (UTC+10) year-round with no daylight saving, so a fixed zone is
// exact here and avoids depending on the IANA tzdata being installed
// wherever `go test` runs.
var (
	vesselLat = -20.109054166666667
	vesselLon = 148.8943935
	aest      = time.FixedZone("AEST", 10*60*60)
)

func TestSunTimes_MatchesProviderSep7(t *testing.T) {
	sunrise, sunset, ok := sunTimes(time.Date(2026, 9, 7, 0, 0, 0, 0, aest), vesselLat, vesselLon)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	withinTolerance(t, "sunrise", sunrise, time.Date(2026, 9, 6, 20, 8, 0, 0, time.UTC), 3*time.Minute)
	withinTolerance(t, "sunset", sunset, time.Date(2026, 9, 7, 7, 57, 0, 0, time.UTC), 3*time.Minute)
}

func TestSunTimes_MatchesProviderSep8(t *testing.T) {
	sunrise, sunset, ok := sunTimes(time.Date(2026, 9, 8, 0, 0, 0, 0, aest), vesselLat, vesselLon)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	withinTolerance(t, "sunrise", sunrise, time.Date(2026, 9, 7, 20, 7, 0, 0, time.UTC), 3*time.Minute)
	withinTolerance(t, "sunset", sunset, time.Date(2026, 9, 8, 7, 57, 0, 0, time.UTC), 3*time.Minute)
}

func TestSunTimes_MatchesProviderSep9(t *testing.T) {
	sunrise, sunset, ok := sunTimes(time.Date(2026, 9, 9, 0, 0, 0, 0, aest), vesselLat, vesselLon)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	withinTolerance(t, "sunrise", sunrise, time.Date(2026, 9, 8, 20, 6, 0, 0, time.UTC), 3*time.Minute)
	withinTolerance(t, "sunset", sunset, time.Date(2026, 9, 9, 7, 57, 0, 0, time.UTC), 3*time.Minute)
}

func TestSunTimes_MatchesProviderSep10(t *testing.T) {
	sunrise, sunset, ok := sunTimes(time.Date(2026, 9, 10, 0, 0, 0, 0, aest), vesselLat, vesselLon)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	withinTolerance(t, "sunrise", sunrise, time.Date(2026, 9, 9, 20, 5, 0, 0, time.UTC), 3*time.Minute)
	withinTolerance(t, "sunset", sunset, time.Date(2026, 9, 10, 7, 57, 0, 0, time.UTC), 3*time.Minute)
}

// TestSunTimes_NorthernHemispherePublishedTable checks the sign conventions
// (an east-positive vessel longitude alone doesn't prove the west-negative
// side works) against a live lookup from the US Naval Observatory's
// Astronomical Applications API for New York City on 2026-06-21:
// https://aa.usno.navy.mil/api/rstt/oneday?date=2026-06-21&coords=40.7128,-74.0060&tz=-4
// which returned sunrise 05:25 and sunset 20:31 EDT (UTC-4), fetched
// 2026-09-07. EDT is a fixed UTC-4 offset with daylight saving already in
// effect in June, so no IANA tzdata lookup is needed to reproduce it.
func TestSunTimes_NorthernHemispherePublishedTable(t *testing.T) {
	edt := time.FixedZone("EDT", -4*60*60)
	sunrise, sunset, ok := sunTimes(time.Date(2026, 6, 21, 0, 0, 0, 0, edt), 40.7128, -74.0060)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	withinTolerance(t, "sunrise", sunrise, time.Date(2026, 6, 21, 9, 25, 0, 0, time.UTC), 3*time.Minute)
	withinTolerance(t, "sunset", sunset, time.Date(2026, 6, 22, 0, 31, 0, 0, time.UTC), 3*time.Minute)
}

// TestSunTimes_PolarDayReturnsNotOK covers the summer-solstice midnight sun
// at Svalbard (78N), where the sun never sets - the hour-angle cosine falls
// below -1 and there is no sunrise/sunset to report.
func TestSunTimes_PolarDayReturnsNotOK(t *testing.T) {
	_, _, ok := sunTimes(time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC), 78.0, 15.6)
	if ok {
		t.Fatalf("expected ok=false for polar day")
	}
}
