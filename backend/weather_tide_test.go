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

	response := buildWeatherForecastResponse(weatherForecastDataBundle{Days: state}, true, updatedAt)

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

func TestSummarizeHourlyForecast_IncludesTypicalWindAndGust(t *testing.T) {
	entries := []weatherHourlyEntryData{
		{Label: "Now", Condition: "Mostly Sunny", WindSpeedKts: 10, WindGustKts: 18, Kind: "forecast"},
		{Label: "11AM", Condition: "Mostly Sunny", WindSpeedKts: 12, WindGustKts: 20, Kind: "forecast"},
		{Label: "12PM", Condition: "Mostly Sunny", WindSpeedKts: 11, WindGustKts: 19, Kind: "forecast"},
		{Label: "5:09PM", Condition: "Sunset", Kind: "sunset"},
	}

	summary := summarizeHourlyForecast(entries)
	expected := "Mostly Sunny conditions will continue all day. Winds around 11 kts with gusts up to 20 kts."
	if summary != expected {
		t.Fatalf("expected %q, got %q", expected, summary)
	}
}

func TestBuildDailyWindSeries_BucketsHoursByLocalDate(t *testing.T) {
	loc := time.FixedZone("AEST", 10*60*60)
	result := map[string]any{
		"forecastHourly": map[string]any{
			"hours": []any{
				map[string]any{"forecastStart": "2026-06-14T13:00:00Z", "windSpeed": 18.52, "windGust": 27.78, "windDirection": 90.0},  // 23:00 AEST (Jun 14)
				map[string]any{"forecastStart": "2026-06-14T14:00:00Z", "windSpeed": 9.26, "windDirection": 180.0},                    // 00:00 AEST (Jun 15), no gust
				map[string]any{"forecastStart": "not-a-timestamp"},                                                                    // ignored
			},
		},
	}

	series := buildDailyWindSeries(result, loc)

	day14 := series["2026-06-14"]
	if len(day14) != 1 {
		t.Fatalf("expected 1 entry for Jun 14, got %d", len(day14))
	}
	if day14[0].Label != "11PM" {
		t.Fatalf("expected label 11PM, got %s", day14[0].Label)
	}
	if got := day14[0].WindSpeedKts; got < 9.99 || got > 10.01 {
		t.Fatalf("expected ~10kt wind speed, got %f", got)
	}
	if got := day14[0].WindGustKts; got < 14.99 || got > 15.01 {
		t.Fatalf("expected ~15kt gust, got %f", got)
	}
	if day14[0].WindDirection != "E" {
		t.Fatalf("expected E direction, got %s", day14[0].WindDirection)
	}
	if day14[0].WindDirectionDeg != 90.0 {
		t.Fatalf("expected 90 degree direction, got %f", day14[0].WindDirectionDeg)
	}

	day15 := series["2026-06-15"]
	if len(day15) != 1 {
		t.Fatalf("expected 1 entry for Jun 15, got %d", len(day15))
	}
	if day15[0].Label != "12AM" {
		t.Fatalf("expected label 12AM, got %s", day15[0].Label)
	}
	// Missing windGust falls back to windSpeed.
	if day15[0].WindGustKts != day15[0].WindSpeedKts {
		t.Fatalf("expected gust to fall back to wind speed, got speed=%f gust=%f", day15[0].WindSpeedKts, day15[0].WindGustKts)
	}
	if day15[0].WindDirectionDeg != 180.0 {
		t.Fatalf("expected 180 degree direction, got %f", day15[0].WindDirectionDeg)
	}
}

func TestBuildWindSummary_FormatsRangeAndGust(t *testing.T) {
	hourly := []weatherHourlyWindData{
		{WindSpeedKts: 19.0, WindGustKts: 24.0},
		{WindSpeedKts: 22.0, WindGustKts: 29.4},
		{WindSpeedKts: 20.0, WindGustKts: 25.0},
	}

	summary := buildWindSummary("Tuesday", hourly)
	expected := "On Tuesday, winds will be 19 to 22 kts, with gusts up to 29 kts."
	if summary != expected {
		t.Fatalf("expected %q, got %q", expected, summary)
	}
}

func TestBuildWindSummary_OmitsGustWhenNotAboveSustained(t *testing.T) {
	hourly := []weatherHourlyWindData{
		{WindSpeedKts: 12.0, WindGustKts: 12.0},
		{WindSpeedKts: 12.0, WindGustKts: 12.0},
	}

	summary := buildWindSummary("Wednesday", hourly)
	expected := "On Wednesday, winds will be around 12 kts."
	if summary != expected {
		t.Fatalf("expected %q, got %q", expected, summary)
	}
}

func TestBuildWindSummary_EmptyWhenNoHourlyData(t *testing.T) {
	if summary := buildWindSummary("Thursday", nil); summary != "" {
		t.Fatalf("expected empty summary, got %q", summary)
	}

	hourly := []weatherHourlyWindData{{WindSpeedKts: -1, WindGustKts: -1}}
	if summary := buildWindSummary("Thursday", hourly); summary != "" {
		t.Fatalf("expected empty summary for all-missing data, got %q", summary)
	}
}

func TestParseOpenMeteoMarineResponse_BucketsHoursByLocalDate(t *testing.T) {
	result := map[string]any{
		"hourly": map[string]any{
			"time":           []any{"2026-06-14T23:00", "2026-06-15T00:00"},
			"wave_height":    []any{1.2, 1.5},
			"wave_period":    []any{6.5, 7.0},
			"wave_direction": []any{135.0, 140.0},
		},
	}

	series, err := parseOpenMeteoMarineResponse(result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	day14 := series["2026-06-14"]
	if len(day14) != 1 {
		t.Fatalf("expected 1 entry for Jun 14, got %d", len(day14))
	}
	if day14[0].Label != "11PM" {
		t.Fatalf("expected label 11PM, got %s", day14[0].Label)
	}
	if day14[0].WaveHeightM != 1.2 {
		t.Fatalf("expected wave height 1.2, got %f", day14[0].WaveHeightM)
	}
	if day14[0].WavePeriodS != 6.5 {
		t.Fatalf("expected wave period 6.5, got %f", day14[0].WavePeriodS)
	}
	if day14[0].WaveDirectionDeg != 135.0 {
		t.Fatalf("expected wave direction 135, got %f", day14[0].WaveDirectionDeg)
	}

	day15 := series["2026-06-15"]
	if len(day15) != 1 {
		t.Fatalf("expected 1 entry for Jun 15, got %d", len(day15))
	}
}

func TestParseOpenMeteoMarineResponse_RequiresHourlyField(t *testing.T) {
	if _, err := parseOpenMeteoMarineResponse(map[string]any{}); err == nil {
		t.Fatalf("expected error for missing hourly field")
	}
}
