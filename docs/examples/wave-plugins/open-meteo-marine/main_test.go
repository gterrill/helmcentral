package main

import (
	"testing"
	"time"
)

func TestParseOpenMeteoWaveResponse_FlatChronologicalHourly(t *testing.T) {
	// Fixture adapted from original TestParseOpenMeteoMarineResponse_BucketsHoursByLocalDate,
	// but now expecting a flat list (not bucketed by day) with RFC3339 times.
	// The fixture has utc_offset_seconds=36000 (UTC+10:00, Australian Eastern Standard Time).
	waveRaw := []byte(`{
		"latitude": -34.0,
		"longitude": 151.25,
		"utc_offset_seconds": 36000,
		"timezone": "Australia/Sydney",
		"hourly": {
			"time": ["2026-06-14T23:00", "2026-06-15T00:00"],
			"wave_height": [1.2, 1.5],
			"wave_period": [6.5, 7.0],
			"wave_direction": [135.0, 140.0],
			"wind_wave_height": [0.4, 0.5],
			"swell_wave_height": [0.9, 1.1]
		}
	}`)

	hourly, err := parseOpenMeteoWaveResponse(waveRaw, 36000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(hourly) != 2 {
		t.Fatalf("expected 2 hourly entries, got %d", len(hourly))
	}

	// First entry: 2026-06-14T23:00 local (UTC+10) = 2026-06-14T13:00 UTC
	first := hourly[0]
	if first.Time != "2026-06-14T13:00:00Z" {
		t.Errorf("expected first time 2026-06-14T13:00:00Z, got %s", first.Time)
	}
	if first.WaveHeightM != 1.2 {
		t.Errorf("expected wave height 1.2, got %f", first.WaveHeightM)
	}
	if first.WavePeriodS != 6.5 {
		t.Errorf("expected wave period 6.5, got %f", first.WavePeriodS)
	}
	if first.WaveDirectionDeg != 135.0 {
		t.Errorf("expected wave direction 135, got %f", first.WaveDirectionDeg)
	}
	if first.WindWaveHeightM != 0.4 {
		t.Errorf("expected wind wave height 0.4, got %f", first.WindWaveHeightM)
	}
	if first.SwellWaveHeightM != 0.9 {
		t.Errorf("expected swell wave height 0.9, got %f", first.SwellWaveHeightM)
	}

	// Second entry: 2026-06-15T00:00 local (UTC+10) = 2026-06-14T14:00 UTC
	second := hourly[1]
	if second.Time != "2026-06-14T14:00:00Z" {
		t.Errorf("expected second time 2026-06-14T14:00:00Z, got %s", second.Time)
	}
	if second.WaveHeightM != 1.5 {
		t.Errorf("expected wave height 1.5, got %f", second.WaveHeightM)
	}

	// Verify times are in chronological order (they should be)
	t1, _ := time.Parse(time.RFC3339, first.Time)
	t2, _ := time.Parse(time.RFC3339, second.Time)
	if t2.Before(t1) {
		t.Errorf("hourly entries not sorted by time")
	}
}

func TestParseOpenMeteoWaveResponse_RequiresHourlyField(t *testing.T) {
	raw := []byte(`{"utc_offset_seconds": 0}`)
	if _, err := parseOpenMeteoWaveResponse(raw, 0); err == nil {
		t.Fatalf("expected error for missing hourly field")
	}
}

func TestParseOpenMeteoWaveResponse_RequiresTimeArray(t *testing.T) {
	raw := []byte(`{"hourly": {"wave_height": [1.0]}}`)
	if _, err := parseOpenMeteoWaveResponse(raw, 0); err == nil {
		t.Fatalf("expected error for missing hourly.time")
	}
}

func TestParseOpenMeteoSeaTemperatureResponse_ReturnsCurrentValue(t *testing.T) {
	// Fixture adapted from original TestParseOpenMeteoSeaTemperatureResponse_ReturnsCurrentValue.
	raw := []byte(`{
		"current": {
			"time": "2026-06-19T04:45",
			"sea_surface_temperature": 19.3
		}
	}`)

	temp, err := parseOpenMeteoSeaTemperatureResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if temp == nil {
		t.Fatalf("expected non-nil temp")
	}
	if *temp != 19.3 {
		t.Fatalf("expected temperature 19.3, got %f", *temp)
	}
}

func TestParseOpenMeteoSeaTemperatureResponse_MissingCurrentField(t *testing.T) {
	// When current field is missing entirely, treat as "no data" (nil, not error).
	raw := []byte(`{}`)
	temp, err := parseOpenMeteoSeaTemperatureResponse(raw)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if temp != nil {
		t.Fatalf("expected nil temp for missing current field, got %v", temp)
	}
}

func TestParseOpenMeteoSeaTemperatureResponse_NullSeaSurfaceTemperature(t *testing.T) {
	// Fixture from original test: sea_surface_temperature can be null when
	// the underlying model has no data for a location (e.g. inland).
	raw := []byte(`{
		"current": {
			"time": "2026-07-19T08:30",
			"sea_surface_temperature": null
		}
	}`)

	temp, err := parseOpenMeteoSeaTemperatureResponse(raw)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if temp != nil {
		t.Fatalf("expected nil temp for null sea_surface_temperature, got %v", temp)
	}
}

func TestParseOpenMeteoSeaTemperatureResponse_MissingSeaSurfaceTemperatureKey(t *testing.T) {
	// current exists but sea_surface_temperature key is absent - treat as "no data".
	raw := []byte(`{
		"current": {
			"time": "2026-07-19T08:30"
		}
	}`)

	temp, err := parseOpenMeteoSeaTemperatureResponse(raw)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if temp != nil {
		t.Fatalf("expected nil temp for missing sea_surface_temperature key, got %v", temp)
	}
}

func TestParseOpenMeteoLocalTime_PositiveOffset(t *testing.T) {
	// UTC+10:00 (e.g. Australian Eastern Standard Time)
	rfc, err := parseOpenMeteoLocalTime("2026-06-14T23:00", 36000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 2026-06-14T23:00 local + UTC+10 = 2026-06-14T13:00 UTC
	expected := "2026-06-14T13:00:00Z"
	if rfc.UTC().Format(time.RFC3339) != expected {
		t.Errorf("expected %s, got %s", expected, rfc.UTC().Format(time.RFC3339))
	}
}

func TestParseOpenMeteoLocalTime_NegativeOffset(t *testing.T) {
	// UTC-5:00 (e.g. US Eastern Standard Time)
	rfc, err := parseOpenMeteoLocalTime("2026-06-14T13:00", -18000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 2026-06-14T13:00 local - UTC-5 (i.e., UTC+5 when subtracting the negative offset) = 2026-06-14T18:00 UTC
	expected := "2026-06-14T18:00:00Z"
	if rfc.UTC().Format(time.RFC3339) != expected {
		t.Errorf("expected %s, got %s", expected, rfc.UTC().Format(time.RFC3339))
	}
}

func TestClampForecastDays_DefaultWhenZeroOrNegative(t *testing.T) {
	if got := clampForecastDays(0); got != 10 {
		t.Errorf("expected 10 for days=0, got %d", got)
	}
	if got := clampForecastDays(-1); got != 10 {
		t.Errorf("expected 10 for days=-1, got %d", got)
	}
}

func TestClampForecastDays_CapAtMaximum(t *testing.T) {
	if got := clampForecastDays(20); got != 16 {
		t.Errorf("expected 16 for days=20, got %d", got)
	}
	if got := clampForecastDays(16); got != 16 {
		t.Errorf("expected 16 for days=16, got %d", got)
	}
}

func TestClampForecastDays_PassThrough(t *testing.T) {
	if got := clampForecastDays(10); got != 10 {
		t.Errorf("expected 10 for days=10, got %d", got)
	}
	if got := clampForecastDays(1); got != 1 {
		t.Errorf("expected 1 for days=1, got %d", got)
	}
}
