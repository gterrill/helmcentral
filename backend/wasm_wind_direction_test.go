package main

import (
	"encoding/json"
	"testing"
	"time"
)

// wind_direction_deg is nullable on the plugin wire: null or absent is an
// unknown direction, 0 is due north. The host carries unknown as -1.

func TestWasmWindDirection_AbsentUnmarshalsToNilAndZeroToNorth(t *testing.T) {
	var absent wasmWeatherHourOutput
	if err := json.Unmarshal([]byte(`{"time":"2026-06-14T00:00:00Z","wind_direction_deg":null}`), &absent); err != nil {
		t.Fatal(err)
	}
	if absent.WindDirectionDeg != nil {
		t.Errorf("null direction = %v, want nil", *absent.WindDirectionDeg)
	}
	var north wasmWeatherHourOutput
	if err := json.Unmarshal([]byte(`{"time":"2026-06-14T00:00:00Z","wind_direction_deg":0}`), &north); err != nil {
		t.Fatal(err)
	}
	if north.WindDirectionDeg == nil || *north.WindDirectionDeg != 0 {
		t.Errorf("zero direction = %v, want a pointer to 0", north.WindDirectionDeg)
	}
}

func TestMapWasmFetchForecastOutput_WindDirectionNullIsUnknownZeroIsNorth(t *testing.T) {
	zero := 0.0
	out := wasmFetchForecastOutput{
		Current: wasmWeatherCurrentOutput{Time: "2026-06-14T00:00:00Z", WindSpeedMS: 5, WindDirectionDeg: &zero},
		Days: []wasmWeatherDayOutput{
			{Start: "2026-06-14T00:00:00Z", WindSpeedMS: 5, WindDirectionDeg: &zero},
			{Start: "2026-06-15T00:00:00Z", WindSpeedMS: 5},
		},
		Hourly: []wasmWeatherHourOutput{
			{Time: "2026-06-14T00:00:00Z", WindSpeedMS: 5, WindDirectionDeg: &zero},
			{Time: "2026-06-14T01:00:00Z", WindSpeedMS: 5},
		},
	}
	b, err := mapWasmFetchForecastOutput(out)
	if err != nil {
		t.Fatal(err)
	}
	if b.Current.WindDirectionDeg != 0 || b.Days[0].WindDirectionDeg != 0 || b.Hourly[0].WindDirectionDeg != 0 {
		t.Errorf("direction 0 should stay 0, got current=%v day=%v hour=%v",
			b.Current.WindDirectionDeg, b.Days[0].WindDirectionDeg, b.Hourly[0].WindDirectionDeg)
	}
	if b.Days[1].WindDirectionDeg != -1 || b.Hourly[1].WindDirectionDeg != -1 {
		t.Errorf("null direction should map to -1, got day=%v hour=%v", b.Days[1].WindDirectionDeg, b.Hourly[1].WindDirectionDeg)
	}
}

func TestSentinelDirection_NorthIsNotUnknown(t *testing.T) {
	if name, deg := sentinelDirection(0); name != "N" || deg != 0 {
		t.Errorf("sentinelDirection(0) = %q, %v, want N, 0", name, deg)
	}
	if name, deg := sentinelDirection(-1); name != "—" || deg != -1 {
		t.Errorf("sentinelDirection(-1) = %q, %v, want —, -1", name, deg)
	}
	if name, deg := sentinelDirection(270); name != "W" || deg != 270 {
		t.Errorf("sentinelDirection(270) = %q, %v, want W, 270", name, deg)
	}
}

func TestBuildWindHour_UnknownDirectionWithWindKeepsSpeedOnly(t *testing.T) {
	course := 90.0
	hp := weatherHourPoint{Time: time.Date(2026, 9, 12, 2, 0, 0, 0, time.UTC), WindSpeedMS: 6.1, WindDirectionDeg: -1}
	row := buildWindHour(hp, time.UTC, nil, &course, northShelter())
	if row.WindKts == nil {
		t.Error("wind speed should survive an unknown direction")
	}
	if row.Dir != nil || row.DirDeg != nil || row.RelWind != nil || row.Shelter != "" {
		t.Errorf("an unknown direction must not produce dir/relative/shelter: %+v", row)
	}
}

func TestPassageHoursFromBundles_UnknownDirectionHourIsAbsentNorthIsKept(t *testing.T) {
	t0 := time.Date(2026, 9, 12, 2, 0, 0, 0, time.UTC)
	wb := weatherForecastBundle{Hourly: []weatherHourPoint{
		{Time: t0, WindSpeedMS: 6, WindDirectionDeg: 0},
		{Time: t0.Add(time.Hour), WindSpeedMS: 6, WindDirectionDeg: -1},
		{Time: t0.Add(2 * time.Hour), WindDirectionDeg: -1},
	}}
	got := passageHoursFromBundles(wb, nil)
	if _, ok := got[t0.Unix()]; !ok {
		t.Error("a due-north hour should be kept")
	}
	if len(got) != 1 {
		t.Errorf("hours with an unknown direction should be absent, got %d hours", len(got))
	}
}
