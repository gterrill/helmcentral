package main

import (
	"testing"
	"time"
)

// First light is civil dawn: the sun 6 degrees below the horizon. At the
// vessel's latitude in September that is about 25 minutes before sunrise.
func TestCivilDawn_IsBeforeSunriseByTheTwilight(t *testing.T) {
	day := time.Date(2026, 9, 7, 0, 0, 0, 0, aest)
	rise, _, ok := sunTimes(day, vesselLat, vesselLon)
	if !ok {
		t.Fatal("sunTimes not ok")
	}
	dawn, ok := civilDawn(day, vesselLat, vesselLon)
	if !ok {
		t.Fatal("civilDawn not ok")
	}
	gap := rise.Sub(dawn)
	if gap < 20*time.Minute || gap > 30*time.Minute {
		t.Fatalf("dawn %s is %s before sunrise %s, want about 25 minutes", dawn, gap, rise)
	}
}

func TestSunTimes_StillTheSameAfterTheDawnSplit(t *testing.T) {
	sunrise, sunset, ok := sunTimes(time.Date(2026, 9, 7, 0, 0, 0, 0, aest), vesselLat, vesselLon)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	withinTolerance(t, "sunrise", sunrise, time.Date(2026, 9, 6, 20, 8, 0, 0, time.UTC), 3*time.Minute)
	withinTolerance(t, "sunset", sunset, time.Date(2026, 9, 7, 7, 57, 0, 0, time.UTC), 3*time.Minute)
}

func TestCivilDawn_PolarNightIsNotOk(t *testing.T) {
	if _, ok := civilDawn(time.Date(2026, 12, 21, 0, 0, 0, 0, time.UTC), 89, 0); ok {
		t.Fatal("no civil dawn at 89N at midwinter: the sun never gets within 6 degrees")
	}
}
