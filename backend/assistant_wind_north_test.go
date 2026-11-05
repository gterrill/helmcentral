package main

import (
	"encoding/json"
	"testing"
	"time"
)

// A forecast direction of 0 is due north when there is wind. Open-Meteo sends a
// null direction only with a null speed, which decodes to 0 for both, so a
// zero direction with no wind is the only unknown.

func northShelter() *shelterResult {
	return &shelterResult{shelterEstimate: shelterEstimate{Bearings: []shelterBearing{
		{BearingDeg: 0, Grade: shelterSheltered},
		{BearingDeg: 180, Grade: shelterExposed},
	}}}
}

func TestBuildWindHour_DueNorthWindKeepsItsDirection(t *testing.T) {
	course := 90.0
	hp := weatherHourPoint{Time: time.Date(2026, 9, 12, 2, 0, 0, 0, time.UTC), WindSpeedMS: 6.1, WindDirectionDeg: 0}
	row := buildWindHour(hp, time.UTC, nil, &course, northShelter())

	if row.Dir == nil || *row.Dir != "N" {
		t.Errorf("dir = %v, want N", row.Dir)
	}
	if row.DirDeg == nil || *row.DirDeg != 0 {
		t.Errorf("dir_deg = %v, want 0", row.DirDeg)
	}
	if row.RelWindDeg == nil || *row.RelWindDeg != 90 || row.RelWind == nil || *row.RelWind == "" {
		t.Errorf("relative wind = %v %v, want 90 and a label", row.RelWindDeg, row.RelWind)
	}
	if row.Shelter != shelterSheltered {
		t.Errorf("shelter = %q, want %q", row.Shelter, shelterSheltered)
	}
}

func TestBuildWindHour_ZeroDirectionWithNoWindStaysUnknown(t *testing.T) {
	course := 90.0
	hp := weatherHourPoint{Time: time.Date(2026, 9, 12, 2, 0, 0, 0, time.UTC)}
	row := buildWindHour(hp, time.UTC, nil, &course, northShelter())

	if row.Dir != nil || row.DirDeg != nil {
		t.Errorf("dir = %v, dir_deg = %v, want both unknown", row.Dir, row.DirDeg)
	}
	if row.RelWindDeg != nil || row.RelWind != nil {
		t.Errorf("relative wind = %v %v, want none", row.RelWindDeg, row.RelWind)
	}
	if row.Shelter != "" {
		t.Errorf("shelter = %q, want none", row.Shelter)
	}
}

func TestBuildWindHour_Direction360WithWindIsNorth(t *testing.T) {
	course := 90.0
	hp := weatherHourPoint{Time: time.Date(2026, 9, 12, 2, 0, 0, 0, time.UTC), WindSpeedMS: 5, WindDirectionDeg: 360}
	row := buildWindHour(hp, time.UTC, nil, &course, northShelter())

	if row.Dir == nil || *row.Dir != "N" {
		t.Errorf("dir = %v, want N", row.Dir)
	}
	if row.RelWindDeg == nil || *row.RelWindDeg != 90 {
		t.Errorf("relative wind = %v, want 90", row.RelWindDeg)
	}
	if row.Shelter != shelterSheltered {
		t.Errorf("shelter = %q, want %q", row.Shelter, shelterSheltered)
	}
}

// The draft check counts a forecast as having given the angle only when a row
// carries a relative label, so a due-north hour has to carry one.
func TestForecastDeliveredAngle_DueNorthHourCounts(t *testing.T) {
	course := 90.0
	hp := weatherHourPoint{Time: time.Date(2026, 9, 12, 2, 0, 0, 0, time.UTC), WindSpeedMS: 6.1, WindDirectionDeg: 0}
	raw, err := json.Marshal(map[string]any{"hourly": []assistantWindHour{buildWindHour(hp, time.UTC, nil, &course, nil)}})
	if err != nil {
		t.Fatal(err)
	}
	if !forecastDeliveredAngle(string(raw)) {
		t.Errorf("a due-north forecast hour should count as giving the angle: %s", raw)
	}
}
