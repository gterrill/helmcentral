package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// ensureTideProvidersRegistered registers the real tide providers if they
// aren't already — registerTideProvider is normally only called from main(),
// which never runs under `go test`, so the registry is empty by default.
func ensureTideProvidersRegistered(t *testing.T) {
	t.Helper()
	if _, ok := getTideProvider("bom"); !ok {
		registerTideProvider(newBomTideProvider())
	}
	if _, ok := getTideProvider("stormglass"); !ok {
		registerTideProvider(newStormGlassTideProvider())
	}
}

func writeTideTodaySettings(t *testing.T, provider, stationID string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.yaml")
	content := "ui:\n  tide_provider: " + provider + "\n  tide_station_id: " + stationID + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write test settings file: %v", err)
	}
	return path
}

func TestTideToday_ReturnsBadGatewayWhenProviderFetchFails(t *testing.T) {
	ensureTideProvidersRegistered(t)
	settingsPath := writeTideTodaySettings(t, "bom", "DOES_NOT_EXIST_12345")
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/tide-today", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := tideToday(c); err != nil {
		t.Fatalf("tideToday returned error: %v", err)
	}

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status %d when no real tide data is available, got %d (body: %s)", http.StatusBadGateway, rec.Code, rec.Body.String())
	}

	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	if payload["error"] == "" {
		t.Fatalf("expected a non-empty error message, got %+v", payload)
	}
}

func TestTideToday_ReturnsOKWithProviderData(t *testing.T) {
	ensureTideProvidersRegistered(t)
	provider, ok := getTideProvider("bom")
	if !ok {
		t.Fatalf("expected bom provider to be registered")
	}
	bom, ok := provider.(*bomTideProvider)
	if !ok || len(bom.stations) == 0 {
		t.Fatalf("expected bom provider to have embedded stations")
	}
	station := bom.stations[0]

	now := time.Now().UTC()
	fakeResult := tideChartResult{
		Station: station,
		Extremes: []tideExtremePoint{
			{Time: now.Add(-2 * time.Hour), HeightM: 1.0, High: false},
			{Time: now.Add(4 * time.Hour), HeightM: 2.0, High: true},
		},
		CurrentHeightM: 1.5,
		Direction:      "Rising",
		CachedAt:       now,
	}

	bomTideCacheStore.mu.Lock()
	original, hadOriginal := bomTideCacheStore.data[station.StationID]
	bomTideCacheStore.data[station.StationID] = fakeResult
	bomTideCacheStore.mu.Unlock()
	t.Cleanup(func() {
		bomTideCacheStore.mu.Lock()
		if hadOriginal {
			bomTideCacheStore.data[station.StationID] = original
		} else {
			delete(bomTideCacheStore.data, station.StationID)
		}
		bomTideCacheStore.mu.Unlock()
	})

	settingsPath := writeTideTodaySettings(t, "bom", station.StationID)
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/tide-today", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := tideToday(c); err != nil {
		t.Fatalf("tideToday returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var payload tideTodayResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if payload.Provider != "bom" {
		t.Fatalf("expected provider bom, got %q", payload.Provider)
	}
	if payload.StationName != station.Name {
		t.Fatalf("expected station name %q, got %q", station.Name, payload.StationName)
	}
}

func TestTideToday_ReturnsBadGatewayForUnknownProvider(t *testing.T) {
	ensureTideProvidersRegistered(t)
	settingsPath := writeTideTodaySettings(t, "not-a-real-provider", "ANY")
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/tide-today", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := tideToday(c); err != nil {
		t.Fatalf("tideToday returned error: %v", err)
	}

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status %d for an unknown configured provider, got %d (body: %s)", http.StatusBadGateway, rec.Code, rec.Body.String())
	}
}

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

func TestBuildWeatherHourlyEntries_IncludesWindDirection(t *testing.T) {
	loc := time.FixedZone("AEST", 10*60*60)
	now := time.Date(2026, 6, 14, 23, 0, 0, 0, time.UTC) // 09:00 AEST on Jun 15
	result := map[string]any{
		"forecastHourly": map[string]any{
			"hours": []any{
				map[string]any{"forecastStart": "2026-06-14T23:00:00Z", "conditionCode": "Clear", "temperature": 20.0, "windSpeed": 18.52, "windGust": 27.78, "windDirection": 90.0},
			},
		},
	}

	entries := buildWeatherHourlyEntries(result, now, loc, "2026-06-15", time.Time{})

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if got := entries[0].WindSpeedKts; got < 9.99 || got > 10.01 {
		t.Fatalf("expected ~10kt wind speed, got %f", got)
	}
	if entries[0].WindDirection != "E" {
		t.Fatalf("expected E direction, got %q", entries[0].WindDirection)
	}
	if entries[0].WindDirectionDeg != 90.0 {
		t.Fatalf("expected 90 degree direction, got %f", entries[0].WindDirectionDeg)
	}
}

func TestBuildWeatherHourlyEntries_SunsetEntryHasNoWindData(t *testing.T) {
	loc := time.FixedZone("AEST", 10*60*60)
	now := time.Date(2026, 6, 14, 22, 0, 0, 0, time.UTC) // 08:00 AEST on Jun 15
	sunset := time.Date(2026, 6, 15, 8, 30, 0, 0, loc)
	result := map[string]any{
		"forecastHourly": map[string]any{
			"hours": []any{
				map[string]any{"forecastStart": "2026-06-14T22:00:00Z", "conditionCode": "Clear", "temperature": 20.0, "windSpeed": 18.52, "windDirection": 90.0},
				map[string]any{"forecastStart": "2026-06-14T23:00:00Z", "conditionCode": "Clear", "temperature": 19.0, "windSpeed": 18.52, "windDirection": 90.0},
			},
		},
	}

	entries := buildWeatherHourlyEntries(result, now, loc, "2026-06-15", sunset)

	sunsetIdx := -1
	for i, entry := range entries {
		if entry.Kind == "sunset" {
			sunsetIdx = i
			break
		}
	}
	if sunsetIdx == -1 {
		t.Fatalf("expected a sunset entry, got %+v", entries)
	}
	if entries[sunsetIdx].WindSpeedKts != -1 || entries[sunsetIdx].WindGustKts != -1 {
		t.Fatalf("expected sunset entry to have -1 wind sentinels, got %+v", entries[sunsetIdx])
	}
	if entries[sunsetIdx].WindDirection != "—" || entries[sunsetIdx].WindDirectionDeg != -1 {
		t.Fatalf("expected sunset entry to have unavailable wind direction, got %+v", entries[sunsetIdx])
	}
}

func TestMapWeatherHourlyResponse_IncludesWindFields(t *testing.T) {
	entries := []weatherHourlyEntryData{
		{Label: "Now", Condition: "Clear", TemperatureF: 68, WindSpeedKts: 10, WindGustKts: 15, WindDirection: "E", WindDirectionDeg: 90, Kind: "forecast"},
	}

	response := mapWeatherHourlyResponse(entries)

	if len(response) != 1 {
		t.Fatalf("expected 1 response entry, got %d", len(response))
	}
	if response[0].WindSpeedKts != 10 || response[0].WindGustKts != 15 {
		t.Fatalf("expected wind speed/gust to carry through, got %+v", response[0])
	}
	if response[0].WindDirection != "E" || response[0].WindDirectionDeg != 90 {
		t.Fatalf("expected wind direction to carry through, got %+v", response[0])
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
				map[string]any{"forecastStart": "2026-06-14T13:00:00Z", "windSpeed": 18.52, "windGust": 27.78, "windDirection": 90.0}, // 23:00 AEST (Jun 14)
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

func TestBuildDailyPrecipitationSeries_BucketsHoursByLocalDate(t *testing.T) {
	loc := time.FixedZone("AEST", 10*60*60)
	result := map[string]any{
		"forecastHourly": map[string]any{
			"hours": []any{
				map[string]any{"forecastStart": "2026-06-14T13:00:00Z", "precipitationChance": 0.65, "precipitationIntensity": 1.2}, // 23:00 AEST (Jun 14)
				map[string]any{"forecastStart": "2026-06-14T14:00:00Z", "precipitationChance": 0.2},                                 // 00:00 AEST (Jun 15), no intensity
				map[string]any{"forecastStart": "not-a-timestamp"},                                                                  // ignored
			},
		},
	}

	series := buildDailyPrecipitationSeries(result, loc)

	day14 := series["2026-06-14"]
	if len(day14) != 1 {
		t.Fatalf("expected 1 entry for Jun 14, got %d", len(day14))
	}
	if day14[0].Label != "11PM" {
		t.Fatalf("expected label 11PM, got %s", day14[0].Label)
	}
	if got := day14[0].PrecipitationChancePct; got < 64.99 || got > 65.01 {
		t.Fatalf("expected ~65%% chance, got %f", got)
	}
	if day14[0].PrecipitationIntensityMm != 1.2 {
		t.Fatalf("expected 1.2mm/hr intensity, got %f", day14[0].PrecipitationIntensityMm)
	}

	day15 := series["2026-06-15"]
	if len(day15) != 1 {
		t.Fatalf("expected 1 entry for Jun 15, got %d", len(day15))
	}
	if day15[0].Label != "12AM" {
		t.Fatalf("expected label 12AM, got %s", day15[0].Label)
	}
	if got := day15[0].PrecipitationChancePct; got < 19.99 || got > 20.01 {
		t.Fatalf("expected ~20%% chance, got %f", got)
	}
	if day15[0].PrecipitationIntensityMm != 0 {
		t.Fatalf("expected 0mm/hr intensity for missing field, got %f", day15[0].PrecipitationIntensityMm)
	}
}

func TestBuildDailyUVSeries_BucketsHoursByLocalDate(t *testing.T) {
	loc := time.FixedZone("AEST", 10*60*60)
	result := map[string]any{
		"forecastHourly": map[string]any{
			"hours": []any{
				map[string]any{"forecastStart": "2026-06-14T13:00:00Z", "uvIndex": 5.0}, // 23:00 AEST (Jun 14)
				map[string]any{"forecastStart": "2026-06-14T14:00:00Z"},                 // 00:00 AEST (Jun 15), no uvIndex
				map[string]any{"forecastStart": "not-a-timestamp"},                      // ignored
			},
		},
	}

	series := buildDailyUVSeries(result, loc)

	day14 := series["2026-06-14"]
	if len(day14) != 1 {
		t.Fatalf("expected 1 entry for Jun 14, got %d", len(day14))
	}
	if day14[0].Label != "11PM" {
		t.Fatalf("expected label 11PM, got %s", day14[0].Label)
	}
	if day14[0].UVIndex != 5.0 {
		t.Fatalf("expected UV index 5, got %f", day14[0].UVIndex)
	}

	day15 := series["2026-06-15"]
	if len(day15) != 1 {
		t.Fatalf("expected 1 entry for Jun 15, got %d", len(day15))
	}
	if day15[0].Label != "12AM" {
		t.Fatalf("expected label 12AM, got %s", day15[0].Label)
	}
	if day15[0].UVIndex != 0 {
		t.Fatalf("expected UV index 0 for missing field, got %f", day15[0].UVIndex)
	}
}

func TestBuildWindSummary_FormatsRangeDirectionAndGust(t *testing.T) {
	hourly := []weatherHourlyWindData{
		{WindSpeedKts: 19.0, WindGustKts: 24.0, WindDirection: "S"},
		{WindSpeedKts: 22.0, WindGustKts: 29.4, WindDirection: "SSE"},
		{WindSpeedKts: 20.0, WindGustKts: 25.0, WindDirection: "SE"},
	}

	summary := buildWindSummary(hourly)
	expected := "Winds 19 to 22 kts from the S-SE, gusting to 29 kts."
	if summary != expected {
		t.Fatalf("expected %q, got %q", expected, summary)
	}
}

func TestBuildWindSummary_OmitsGustWhenNotAboveSustained(t *testing.T) {
	hourly := []weatherHourlyWindData{
		{WindSpeedKts: 12.0, WindGustKts: 12.0, WindDirection: "NE"},
		{WindSpeedKts: 12.0, WindGustKts: 12.0, WindDirection: "NE"},
	}

	summary := buildWindSummary(hourly)
	expected := "Winds around 12 kts from the NE."
	if summary != expected {
		t.Fatalf("expected %q, got %q", expected, summary)
	}
}

func TestBuildWindSummary_EmptyWhenNoHourlyData(t *testing.T) {
	if summary := buildWindSummary(nil); summary != "" {
		t.Fatalf("expected empty summary, got %q", summary)
	}

	hourly := []weatherHourlyWindData{{WindSpeedKts: -1, WindGustKts: -1}}
	if summary := buildWindSummary(hourly); summary != "" {
		t.Fatalf("expected empty summary for all-missing data, got %q", summary)
	}
}

func TestWindDirectionRange_SingleAndRangeAndMissing(t *testing.T) {
	single := windDirectionRange([]weatherHourlyWindData{{WindDirection: "NE"}, {WindDirection: "NE"}})
	if single != "NE" {
		t.Fatalf("expected NE, got %q", single)
	}

	rangeResult := windDirectionRange([]weatherHourlyWindData{{WindDirection: "S"}, {WindDirection: "SSE"}, {WindDirection: "SE"}})
	if rangeResult != "S-SE" {
		t.Fatalf("expected S-SE, got %q", rangeResult)
	}

	if empty := windDirectionRange([]weatherHourlyWindData{{WindDirection: "—"}}); empty != "" {
		t.Fatalf("expected empty, got %q", empty)
	}
}

func TestBuildWaveSummary_FormatsRangeDirectionAndPeriod(t *testing.T) {
	hourly := []weatherHourlyWaveData{
		{WaveHeightM: 1.1, WavePeriodS: 8.9, WaveDirectionDeg: 63},
		{WaveHeightM: 1.28, WavePeriodS: 8.9, WaveDirectionDeg: 63},
		{WaveHeightM: 1.2, WavePeriodS: 9.1, WaveDirectionDeg: 67},
	}

	summary := buildWaveSummary(hourly)
	expected := "Significant wave height 1.1 to 1.3 m from the ENE, with a period around 9 sec."
	if summary != expected {
		t.Fatalf("expected %q, got %q", expected, summary)
	}
}

func TestBuildWaveSummary_FormatsSteadyHeightWithPeriod(t *testing.T) {
	hourly := []weatherHourlyWaveData{
		{WaveHeightM: 1.1, WavePeriodS: 8.0, WaveDirectionDeg: 90},
		{WaveHeightM: 1.1, WavePeriodS: 10.0, WaveDirectionDeg: 90},
	}

	summary := buildWaveSummary(hourly)
	expected := "Significant wave height around 1.1 m from the E, with a period around 9 sec."
	if summary != expected {
		t.Fatalf("expected %q, got %q", expected, summary)
	}
}

func TestBuildWaveSummary_EmptyWhenNoHourlyData(t *testing.T) {
	if summary := buildWaveSummary(nil); summary != "" {
		t.Fatalf("expected empty summary, got %q", summary)
	}

	hourly := []weatherHourlyWaveData{{WaveHeightM: -1, WavePeriodS: -1, WaveDirectionDeg: -1}}
	if summary := buildWaveSummary(hourly); summary != "" {
		t.Fatalf("expected empty summary for all-missing data, got %q", summary)
	}
}

func TestBuildPrecipitationSummary_LittleToNoRain(t *testing.T) {
	hourly := []weatherHourlyPrecipitationData{
		{Label: "12AM", PrecipitationChancePct: 5, PrecipitationIntensityMm: 0},
		{Label: "1AM", PrecipitationChancePct: 10, PrecipitationIntensityMm: 0},
		{Label: "2AM", PrecipitationChancePct: 20, PrecipitationIntensityMm: 0},
	}

	summary := buildPrecipitationSummary(hourly)
	expected := "Little to no rain is expected."
	if summary != expected {
		t.Fatalf("expected %q, got %q", expected, summary)
	}
}

func TestBuildPrecipitationSummary_SlightChanceAfterHour(t *testing.T) {
	hourly := []weatherHourlyPrecipitationData{
		{Label: "12AM", PrecipitationChancePct: 10, PrecipitationIntensityMm: 0},
		{Label: "1AM", PrecipitationChancePct: 15, PrecipitationIntensityMm: 0},
		{Label: "5PM", PrecipitationChancePct: 45, PrecipitationIntensityMm: 1.0},
	}

	summary := buildPrecipitationSummary(hourly)
	expected := "Slight chance of rain after 5PM."
	if summary != expected {
		t.Fatalf("expected %q, got %q", expected, summary)
	}
}

func TestBuildPrecipitationSummary_RainThroughoutTheDay(t *testing.T) {
	lightHourly := []weatherHourlyPrecipitationData{
		{Label: "12AM", PrecipitationChancePct: 70, PrecipitationIntensityMm: 1.0},
		{Label: "1AM", PrecipitationChancePct: 65, PrecipitationIntensityMm: 0.5},
	}

	lightSummary := buildPrecipitationSummary(lightHourly)
	expectedLight := "Showers expected throughout the day."
	if lightSummary != expectedLight {
		t.Fatalf("expected %q, got %q", expectedLight, lightSummary)
	}

	moderateHourly := []weatherHourlyPrecipitationData{
		{Label: "12AM", PrecipitationChancePct: 80, PrecipitationIntensityMm: 4.0},
	}

	moderateSummary := buildPrecipitationSummary(moderateHourly)
	expectedModerate := "Rain expected throughout the day."
	if moderateSummary != expectedModerate {
		t.Fatalf("expected %q, got %q", expectedModerate, moderateSummary)
	}
}

func TestBuildPrecipitationSummary_HeavyRainAfterHour(t *testing.T) {
	hourly := []weatherHourlyPrecipitationData{
		{Label: "12AM", PrecipitationChancePct: 10, PrecipitationIntensityMm: 0},
		{Label: "12PM", PrecipitationChancePct: 20, PrecipitationIntensityMm: 0.5},
		{Label: "1PM", PrecipitationChancePct: 80, PrecipitationIntensityMm: 8.0},
	}

	summary := buildPrecipitationSummary(hourly)
	expected := "Heavy rain expected after 1PM."
	if summary != expected {
		t.Fatalf("expected %q, got %q", expected, summary)
	}
}

func TestBuildPrecipitationSummary_EmptyWhenNoHourlyData(t *testing.T) {
	if summary := buildPrecipitationSummary(nil); summary != "" {
		t.Fatalf("expected empty summary, got %q", summary)
	}
}

func TestWaveDirectionRange_SingleAndRangeAndMissing(t *testing.T) {
	single := waveDirectionRange([]weatherHourlyWaveData{{WaveDirectionDeg: 90}, {WaveDirectionDeg: 90}})
	if single != "E" {
		t.Fatalf("expected E, got %q", single)
	}

	rangeResult := waveDirectionRange([]weatherHourlyWaveData{{WaveDirectionDeg: 63}, {WaveDirectionDeg: 67}, {WaveDirectionDeg: 90}})
	if rangeResult != "ENE-E" {
		t.Fatalf("expected ENE-E, got %q", rangeResult)
	}

	if empty := waveDirectionRange([]weatherHourlyWaveData{{WaveDirectionDeg: -1}}); empty != "" {
		t.Fatalf("expected empty, got %q", empty)
	}
}

func TestParseOpenMeteoMarineResponse_BucketsHoursByLocalDate(t *testing.T) {
	result := map[string]any{
		"hourly": map[string]any{
			"time":              []any{"2026-06-14T23:00", "2026-06-15T00:00"},
			"wave_height":       []any{1.2, 1.5},
			"wave_period":       []any{6.5, 7.0},
			"wave_direction":    []any{135.0, 140.0},
			"wind_wave_height":  []any{0.4, 0.5},
			"swell_wave_height": []any{0.9, 1.1},
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
	if day14[0].WindWaveHeightM != 0.4 {
		t.Fatalf("expected wind wave height 0.4, got %f", day14[0].WindWaveHeightM)
	}
	if day14[0].SwellWaveHeightM != 0.9 {
		t.Fatalf("expected swell wave height 0.9, got %f", day14[0].SwellWaveHeightM)
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

func TestParseOpenMeteoSeaTemperatureResponse_ReturnsCurrentValue(t *testing.T) {
	result := map[string]any{
		"current": map[string]any{
			"time":                    "2026-06-19T04:45",
			"sea_surface_temperature": 59.2,
		},
	}

	temp, err := parseOpenMeteoSeaTemperatureResponse(result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if temp != 59.2 {
		t.Fatalf("expected sea temperature 59.2, got %f", temp)
	}
}

func TestParseOpenMeteoSeaTemperatureResponse_RequiresCurrentField(t *testing.T) {
	if _, err := parseOpenMeteoSeaTemperatureResponse(map[string]any{}); err == nil {
		t.Fatalf("expected error for missing current field")
	}
}

func TestParseOpenMeteoSeaTemperatureResponse_RequiresSeaSurfaceTemperature(t *testing.T) {
	result := map[string]any{
		"current": map[string]any{
			"time": "2026-06-19T04:45",
			// sea_surface_temperature can come back null/missing when the
			// underlying model has no data for a given location (e.g. inland
			// coordinates), matching the wave-model "undefined units" case.
		},
	}

	if _, err := parseOpenMeteoSeaTemperatureResponse(result); err == nil {
		t.Fatalf("expected error for missing sea_surface_temperature field")
	}
}
