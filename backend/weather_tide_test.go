package main

import (
	"testing"
	"time"
)

func TestWeatherKitForecastLocation_UsesTopLevelTimezone(t *testing.T) {
	result := map[string]any{"timezone": "Pacific/Auckland"}
	loc := weatherKitForecastLocation(result, map[string]any{})

	if loc.String() != "Pacific/Auckland" {
		t.Fatalf("expected Pacific/Auckland location, got %s", loc.String())
	}
}

func TestWeatherKitForecastLocation_FallsBackToMetadataTimezone(t *testing.T) {
	forecastDaily := map[string]any{
		"metadata": map[string]any{"timezone": "America/Los_Angeles"},
	}
	loc := weatherKitForecastLocation(map[string]any{}, forecastDaily)

	if loc.String() != "America/Los_Angeles" {
		t.Fatalf("expected America/Los_Angeles location, got %s", loc.String())
	}
}

func TestWeatherKitForecastLocation_FallsBackToUTC(t *testing.T) {
	loc := weatherKitForecastLocation(map[string]any{"timezone": "Invalid/Zone"}, map[string]any{})

	if loc != time.UTC {
		t.Fatalf("expected UTC fallback location, got %s", loc.String())
	}
}

func TestForecastStartUsesLocalTimezoneForDateLabels(t *testing.T) {
	loc := time.FixedZone("AEST", 10*60*60)
	forecastStart := "2026-06-14T00:30:00Z"
	parsed, err := time.Parse(time.RFC3339, forecastStart)
	if err != nil {
		t.Fatalf("expected valid RFC3339 timestamp: %v", err)
	}

	localized := parsed.In(loc)
	if got := localized.Format("Jan 2"); got != "Jun 14" {
		t.Fatalf("expected date label Jun 14, got %s", got)
	}
	if got := localized.Weekday().String(); got != "Sunday" {
		t.Fatalf("expected weekday Sunday, got %s", got)
	}
}

func TestFetchWeatherKitForecastData_RequiresReferenceDatetime(t *testing.T) {
	_, err := fetchWeatherKitForecastData(-27.0, 153.0, 6, time.Time{}, time.Local)
	if err == nil {
		t.Fatalf("expected error for missing SignalK reference datetime")
	}
}

func TestBuildWeatherForecastResponse_UsesCacheMetadata(t *testing.T) {
	updatedAt := time.Date(2026, time.June, 14, 12, 30, 0, 0, time.UTC)
	state := []weatherForecastDayData{
		{
			Date:             "Jun 14",
			DayName:          "Sunday",
			Condition:        "Clear",
			HighTempF:        75,
			LowTempF:         62,
			WindSpeedKts:     10,
			WindGustKts:      15,
			WindDirection:    "NE",
			PrecipitationPct: 5,
		},
	}

	response := buildWeatherForecastResponse(state, true, updatedAt)

	if !response.Cached {
		t.Fatalf("expected cached=true")
	}
	if response.UpdatedAt != updatedAt.Format(time.RFC3339) {
		t.Fatalf("expected updated_at %s, got %s", updatedAt.Format(time.RFC3339), response.UpdatedAt)
	}
	if response.TTLSeconds != int64(weatherForecastCacheTTL/time.Second) {
		t.Fatalf("expected ttl_seconds %d, got %d", int64(weatherForecastCacheTTL/time.Second), response.TTLSeconds)
	}
	if len(response.Days) != 1 {
		t.Fatalf("expected 1 forecast day, got %d", len(response.Days))
	}
}

func TestVesselLocalLocation_UsesLongitudeOffset(t *testing.T) {
	loc := vesselLocalLocation(152.38)
	_, offsetSeconds := time.Now().In(loc).Zone()
	if offsetSeconds != 10*3600 {
		t.Fatalf("expected UTC+10 offset, got %d seconds", offsetSeconds)
	}
}

func TestVesselLocalLocation_ClampsInvalidLongitudeToUTC(t *testing.T) {
	loc := vesselLocalLocation(181)
	if loc != time.UTC {
		t.Fatalf("expected UTC for invalid longitude, got %s", loc.String())
	}
}
