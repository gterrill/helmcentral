package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func ensureTideProvidersRegistered(t *testing.T) {
	t.Helper()
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
	withCleanTideProviderRegistry(t)
	ensureTideProvidersRegistered(t)
	registerTideProvider(&stubTideProvider{id: "bom", fetchErr: errors.New("simulated fetch failure")})

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
	withCleanTideProviderRegistry(t)
	ensureTideProvidersRegistered(t)

	station := tideStation{StationID: "TEST_STATION", Name: "Test Harbour"}
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
	registerTideProvider(&stubTideProvider{id: "bom", result: fakeResult})

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
	withCleanTideProviderRegistry(t)
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

// TestTideToday_ReturnsBadGatewayWhenProviderEmpty guards the fix for the
// bug where an empty ui.tide_provider silently defaulted to "stormglass" -
// which then 502'd anyway on any install without STORMGLASS_API_KEY set,
// with no indication of what to actually configure. Empty is now treated
// the same as "not configured", with an actionable error.
func TestTideToday_ReturnsBadGatewayWhenProviderEmpty(t *testing.T) {
	withCleanTideProviderRegistry(t)
	ensureTideProvidersRegistered(t)

	path := filepath.Join(t.TempDir(), "settings.yaml")
	if err := os.WriteFile(path, []byte("ui:\n  tide_station_id: ANY\n"), 0o644); err != nil {
		t.Fatalf("failed to write test settings file: %v", err)
	}
	t.Setenv("SETTINGS_FILE", path)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/tide-today", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := tideToday(c); err != nil {
		t.Fatalf("tideToday returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status %d when no tide provider is configured, got %d (body: %s)", http.StatusBadGateway, rec.Code, rec.Body.String())
	}

	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	if payload["error"] == "" {
		t.Fatalf("expected a non-empty, actionable error message, got %+v", payload)
	}
}

// TestTideToday_ReturnsBadGatewayWhenStationEmptyForNonStormglassProvider
// guards the fix for a live bug: ui.tide_station_id defaulted to Storm
// Glass's "vessel-position" pseudo-station regardless of which provider was
// actually configured, so a BOM/NOAA user who hadn't picked a real station
// got a confusing "unknown BOM tide station: vessel-position" error instead
// of a clear "you haven't picked a station yet" message.
func TestTideToday_ReturnsBadGatewayWhenStationEmptyForNonStormglassProvider(t *testing.T) {
	withCleanTideProviderRegistry(t)
	ensureTideProvidersRegistered(t)
	registerTideProvider(&stubTideProvider{id: "bom"})

	settingsPath := writeTideTodaySettings(t, "bom", "")
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/tide-today", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := tideToday(c); err != nil {
		t.Fatalf("tideToday returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status %d when no tide station is configured for a non-stormglass provider, got %d (body: %s)", http.StatusBadGateway, rec.Code, rec.Body.String())
	}

	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	if payload["error"] == "" {
		t.Fatalf("expected a non-empty, actionable error message, got %+v", payload)
	}
}

// TestTideToday_StormglassDefaultsEmptyStationToVesselPosition is a
// regression guard: Storm Glass is the one provider where an empty station
// ID is meaningful (it has exactly one pseudo-station, the vessel's live
// position) - that auto-fill behavior must survive the fix above, which
// only removes the auto-fill for every OTHER provider.
func TestTideToday_StormglassDefaultsEmptyStationToVesselPosition(t *testing.T) {
	withCleanTideProviderRegistry(t)
	ensureTideProvidersRegistered(t)

	settingsPath := writeTideTodaySettings(t, "stormglass", "")
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/tide-today", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := tideToday(c); err != nil {
		t.Fatalf("tideToday returned error: %v", err)
	}
	// Storm Glass has no credentials configured in this test environment,
	// so the request still fails - but it must fail at the *fetch* step
	// (proving the empty station was auto-filled and provider resolution
	// succeeded), not at the provider/station-configuration step.
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	if strings.Contains(payload["error"], "no tide provider configured") || strings.Contains(payload["error"], "no tide station configured") {
		t.Fatalf("expected the stormglass vessel-position station auto-fill to still apply, got configuration error: %q", payload["error"])
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

func TestMapWeatherHourlyEntryResponse_IncludesWindFields(t *testing.T) {
	entries := []weatherHourlyEntryData{
		{Label: "Now", Condition: "Clear", TemperatureF: 68, WindSpeedKts: 10, WindGustKts: 15, WindDirection: "E", WindDirectionDeg: 90, Kind: "forecast"},
	}

	response := mapWeatherHourlyEntryResponse(entries)

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
