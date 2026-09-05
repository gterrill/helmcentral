package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// withCleanWeatherProviderRegistry saves the global weather provider
// registry, resets it to empty for the duration of the test, and restores
// the original afterwards - mirrors withCleanTideProviderRegistry.
func withCleanWeatherProviderRegistry(t *testing.T) {
	t.Helper()
	origRegistry := weatherProviderRegistry
	origOrder := weatherProviderOrder
	weatherProviderRegistry = map[string]weatherProvider{}
	weatherProviderOrder = nil
	t.Cleanup(func() {
		weatherProviderRegistry = origRegistry
		weatherProviderOrder = origOrder
	})
}

type stubWeatherProvider struct {
	id     string
	name   string
	ttl    int64
	bundle weatherForecastBundle
	err    error

	// gotTimezone records the zone the handler asked for, so tests can assert
	// the vessel's local zone (not UTC) reaches the plugin.
	gotTimezone string
}

func (s *stubWeatherProvider) ID() string          { return s.id }
func (s *stubWeatherProvider) Name() string        { return s.name }
func (s *stubWeatherProvider) Description() string { return "Stub weather provider for tests" }
func (s *stubWeatherProvider) TTLSeconds() int64   { return s.ttl }
func (s *stubWeatherProvider) FetchForecast(lat, lon float64, days int, timezone string) (weatherForecastBundle, error) {
	s.gotTimezone = timezone
	if s.err != nil {
		return weatherForecastBundle{}, s.err
	}
	return s.bundle, nil
}

func writeWeatherSettings(t *testing.T, provider, signalkURL string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.yaml")
	content := fmt.Sprintf("signalk:\n  address: %s\n  port: 0\nui:\n  weather_provider: %s\n", signalkURL, provider)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write test settings file: %v", err)
	}
	return path
}

// trustedSignalKPayloadServer is defined in signalk_test.go and reused here.

func TestRegisterWeatherProvider_PreservesRegistrationOrder(t *testing.T) {
	withCleanWeatherProviderRegistry(t)

	registerWeatherProvider(&stubWeatherProvider{id: "b", name: "B"})
	registerWeatherProvider(&stubWeatherProvider{id: "a", name: "A"})

	if len(weatherProviderOrder) != 2 || weatherProviderOrder[0] != "b" || weatherProviderOrder[1] != "a" {
		t.Fatalf("expected order [b a], got %v", weatherProviderOrder)
	}

	if _, ok := getWeatherProvider("a"); !ok {
		t.Fatalf("expected provider a to be registered")
	}
	if _, ok := getWeatherProvider("missing"); ok {
		t.Fatalf("expected provider 'missing' to not be registered")
	}
}

func TestWeatherProvidersHandler_ReturnsRegisteredProvidersInOrder(t *testing.T) {
	withCleanWeatherProviderRegistry(t)
	registerWeatherProvider(&stubWeatherProvider{id: "open-meteo", name: "Open-Meteo"})
	registerWeatherProvider(&stubWeatherProvider{id: "weatherkit", name: "Apple WeatherKit"})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/weather-providers", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := weatherProvidersHandler(c); err != nil {
		t.Fatalf("weatherProvidersHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var payload []weatherProviderInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(payload) != 2 || payload[0].ID != "open-meteo" || payload[1].ID != "weatherkit" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestWeatherToday_ReturnsBadGatewayForUnknownProvider(t *testing.T) {
	withCleanWeatherProviderRegistry(t)
	settingsPath := writeWeatherSettings(t, "not-a-real-provider", "http://127.0.0.1:1")
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/weather-today", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := weatherToday(c); err != nil {
		t.Fatalf("weatherToday returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	if payload["error"] == "" {
		t.Fatalf("expected a non-empty error message")
	}
}

func TestWeatherToday_ReturnsBadGatewayWhenProviderFetchFails(t *testing.T) {
	withCleanWeatherProviderRegistry(t)
	registerWeatherProvider(&stubWeatherProvider{id: "open-meteo", name: "Open-Meteo", ttl: 900, err: fmt.Errorf("simulated upstream failure")})

	server := trustedSignalKPayloadServer(t, -27.4, 153.0)
	defer server.Close()

	settingsPath := writeWeatherSettings(t, "open-meteo", server.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/weather-today", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := weatherToday(c); err != nil {
		t.Fatalf("weatherToday returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on fetch error (no fake defaults), got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestWeatherToday_ReturnsOKWithMappedFields(t *testing.T) {
	withCleanWeatherProviderRegistry(t)
	now := time.Date(2026, 7, 19, 4, 0, 0, 0, time.UTC)
	bundle := weatherForecastBundle{
		Current: weatherCurrentPoint{
			Time:                   now,
			TemperatureC:           21.4,
			Condition:              "partlyCloudy",
			WindSpeedMS:            6.4,
			WindGustMS:             9.3,
			WindDirectionDeg:       45,
			PrecipitationChancePct: 15,
		},
		Cached:   true,
		CachedAt: now,
	}
	registerWeatherProvider(&stubWeatherProvider{id: "open-meteo", name: "Open-Meteo", ttl: 900, bundle: bundle})

	server := trustedSignalKPayloadServer(t, -27.4, 153.0)
	defer server.Close()

	settingsPath := writeWeatherSettings(t, "open-meteo", server.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/weather-today", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := weatherToday(c); err != nil {
		t.Fatalf("weatherToday returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if _, present := payload["high_temp_f"]; present {
		t.Fatalf("expected high_temp_f to be dropped from weather-today response, got %+v", payload)
	}
	if _, present := payload["sea_temperature_f"]; present {
		t.Fatalf("expected sea_temperature_f to be dropped from weather-today response, got %+v", payload)
	}
	if payload["provider"] != "open-meteo" {
		t.Fatalf("expected provider open-meteo, got %+v", payload["provider"])
	}
	if payload["condition"] != "Partly Cloudy" {
		t.Fatalf("expected condition 'Partly Cloudy', got %+v", payload["condition"])
	}

	gotTempF, _ := payload["temperature_f"].(float64)
	wantTempF := 21.4*9/5 + 32
	if diffF := gotTempF - wantTempF; diffF > 0.01 || diffF < -0.01 {
		t.Fatalf("expected temperature_f ~%.2f, got %v", wantTempF, gotTempF)
	}

	gotWindKts, _ := payload["wind_speed_kts"].(float64)
	wantWindKts := 6.4 * metersPerSecondToKnots
	if diffK := gotWindKts - wantWindKts; diffK > 0.01 || diffK < -0.01 {
		t.Fatalf("expected wind_speed_kts ~%.2f, got %v", wantWindKts, gotWindKts)
	}

	if payload["cached"] != true {
		t.Fatalf("expected cached=true, got %+v", payload["cached"])
	}
	if ttl, _ := payload["ttl_seconds"].(float64); int64(ttl) != 900 {
		t.Fatalf("expected ttl_seconds 900, got %v", payload["ttl_seconds"])
	}
}

func TestWeatherForecast_ReturnsBadGatewayForUnknownProvider(t *testing.T) {
	withCleanWeatherProviderRegistry(t)
	settingsPath := writeWeatherSettings(t, "not-a-real-provider", "http://127.0.0.1:1")
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/weather-forecast", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := weatherForecast(c); err != nil {
		t.Fatalf("weatherForecast returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestWeatherForecast_ReturnsBadGatewayWhenProviderFetchFails(t *testing.T) {
	withCleanWeatherProviderRegistry(t)
	registerWeatherProvider(&stubWeatherProvider{id: "open-meteo", name: "Open-Meteo", ttl: 900, err: fmt.Errorf("simulated upstream failure")})

	server := trustedSignalKPayloadServer(t, -27.4, 153.0)
	defer server.Close()

	settingsPath := writeWeatherSettings(t, "open-meteo", server.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/weather-forecast", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := weatherForecast(c); err != nil {
		t.Fatalf("weatherForecast returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on fetch error (no fake defaults), got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestWeatherForecast_ReturnsBadGatewayWhenNoDaysReturned(t *testing.T) {
	withCleanWeatherProviderRegistry(t)
	registerWeatherProvider(&stubWeatherProvider{id: "open-meteo", name: "Open-Meteo", ttl: 900, bundle: weatherForecastBundle{}})

	server := trustedSignalKPayloadServer(t, -27.4, 153.0)
	defer server.Close()

	settingsPath := writeWeatherSettings(t, "open-meteo", server.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/weather-forecast", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := weatherForecast(c); err != nil {
		t.Fatalf("weatherForecast returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 for an empty forecast, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestWeatherForecast_ReturnsOKWithDayKeyAndNoWaveFields(t *testing.T) {
	withCleanWeatherProviderRegistry(t)

	// trustedSignalKPayloadServer stamps navigation.datetime with the real
	// time.Now(), so the bundle's Days/Hourly must be anchored to the actual
	// "now" (via vesselLocalLocation for this test's longitude) rather than a
	// fixed historical date, or the handler's local-day bucketing would
	// filter every day out as "before today".
	loc := vesselLocalLocation(153.0)
	nowLocal := time.Now().In(loc)
	todayLocalMidnight := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), 0, 0, 0, 0, loc)
	day1Start := todayLocalMidnight
	day2Start := todayLocalMidnight.AddDate(0, 0, 1)

	bundle := weatherForecastBundle{
		Days: []weatherDayPoint{
			{Start: day1Start, Condition: "clear", TempMaxC: 24, TempMinC: 15, WindSpeedMS: 5, WindGustMS: 8, WindDirectionDeg: 90, PrecipitationChancePct: 10},
			{Start: day2Start, Condition: "rain", TempMaxC: 22, TempMinC: 14, WindSpeedMS: 6, WindGustMS: 12, WindDirectionDeg: 180, PrecipitationChancePct: 70},
		},
		Hourly: []weatherHourPoint{
			{Time: todayLocalMidnight.Add(time.Hour), TemperatureC: 20, Condition: "clear", WindSpeedMS: 5, WindDirectionDeg: 90, IsDaylight: true},
			{Time: day2Start.Add(time.Hour), TemperatureC: 18, Condition: "cloudy", WindSpeedMS: 6, WindDirectionDeg: 180, IsDaylight: false},
		},
		Cached:   false,
		CachedAt: time.Now().UTC(),
	}
	registerWeatherProvider(&stubWeatherProvider{id: "open-meteo", name: "Open-Meteo", ttl: 900, bundle: bundle})

	server := trustedSignalKPayloadServer(t, -27.4, 153.0)
	defer server.Close()

	settingsPath := writeWeatherSettings(t, "open-meteo", server.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/weather-forecast", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := weatherForecast(c); err != nil {
		t.Fatalf("weatherForecast returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if bodyContains(body, "wave_summary") || bodyContains(body, "hourly_wave") {
		t.Fatalf("expected no wave fields in weather-forecast response, got: %s", body)
	}

	var payload weatherForecastResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if payload.Provider != "open-meteo" {
		t.Fatalf("expected provider open-meteo, got %q", payload.Provider)
	}
	if len(payload.Days) == 0 {
		t.Fatalf("expected at least 1 day")
	}
	if payload.Days[0].DayKey == "" {
		t.Fatalf("expected non-empty day_key on days[0]")
	}
	_ = loc
}

func bodyContains(body, substr string) bool {
	return len(body) > 0 && (func() bool {
		for i := 0; i+len(substr) <= len(body); i++ {
			if body[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})()
}

// --- moonPhase ---

func TestMoonPhase_NewMoonEpochIsNew(t *testing.T) {
	// 2000-01-06T18:14:00Z is a well documented real astronomical new moon,
	// used as the synodic-cycle reference epoch.
	epoch := time.Date(2000, 1, 6, 18, 14, 0, 0, time.UTC)
	if got := moonPhase(epoch); got != "new" {
		t.Fatalf("expected 'new' at the reference new moon, got %q", got)
	}
}

func TestMoonPhase_KnownFullMoonDate(t *testing.T) {
	// 2020-12-30T03:28:00Z was a real, independently documented full moon
	// (the "Cold Moon" / final full moon of 2020), verified via web search
	// against multiple lunar-calendar sources, not derived from this
	// package's own epoch/synodic-month math.
	fullMoon := time.Date(2020, 12, 30, 3, 28, 0, 0, time.UTC)
	if got := moonPhase(fullMoon); got != "full" {
		t.Fatalf("expected 'full' at the known 2020-12-30 full moon, got %q", got)
	}
}

func TestMoonPhase_ReturnsOnlyKnownBuckets(t *testing.T) {
	valid := map[string]bool{
		"new": true, "waxingCrescent": true, "firstQuarter": true, "waxingGibbous": true,
		"full": true, "waningGibbous": true, "lastQuarter": true, "waningCrescent": true,
	}
	epoch := time.Date(2000, 1, 6, 18, 14, 0, 0, time.UTC)
	for i := 0; i < 40; i++ {
		got := moonPhase(epoch.AddDate(0, 0, i))
		if !valid[got] {
			t.Fatalf("moonPhase returned an unrecognized bucket %q for offset %d days", got, i)
		}
	}
}

// --- day bucketing ---

func TestBuildHourlySeriesByDay_BucketsHoursByLocalDate(t *testing.T) {
	loc := time.FixedZone("AEST", 10*60*60)
	hourly := []weatherHourPoint{
		{Time: time.Date(2026, 6, 14, 13, 0, 0, 0, time.UTC), WindSpeedMS: 9.52, WindGustMS: 14.29, WindDirectionDeg: 90, TemperatureC: 18, Condition: "cloudy", PrecipitationChancePct: 65, PrecipitationMM: 1.2, UVIndex: 5, IsDaylight: false}, // 23:00 AEST Jun 14
		{Time: time.Date(2026, 6, 14, 14, 0, 0, 0, time.UTC), WindSpeedMS: 4.76, WindDirectionDeg: 180, TemperatureC: 16, Condition: "clear", PrecipitationChancePct: 20, IsDaylight: true},                                                       // 00:00 AEST Jun 15
	}

	windByDay, precipByDay, uvByDay, cloudByDay := buildHourlySeriesByDay(hourly, loc)

	day14Wind := windByDay["2026-06-14"]
	if len(day14Wind) != 1 {
		t.Fatalf("expected 1 wind entry for Jun 14, got %d", len(day14Wind))
	}
	if day14Wind[0].Label != "11PM" || day14Wind[0].HourOfDay != 23 {
		t.Fatalf("unexpected day14 wind bucket: %+v", day14Wind[0])
	}
	if got := day14Wind[0].WindSpeedKts; got < 18.49 || got > 18.51 {
		t.Fatalf("expected ~18.5kt wind speed (9.52 m/s), got %f", got)
	}
	if day14Wind[0].WindDirection != "E" {
		t.Fatalf("expected E direction, got %s", day14Wind[0].WindDirection)
	}

	day15Wind := windByDay["2026-06-15"]
	if len(day15Wind) != 1 || day15Wind[0].HourOfDay != 0 {
		t.Fatalf("expected 1 wind entry at hour 0 for Jun 15, got %+v", day15Wind)
	}
	// Missing WindGustMS falls back to wind speed.
	if day15Wind[0].WindGustKts != day15Wind[0].WindSpeedKts {
		t.Fatalf("expected gust to fall back to wind speed, got speed=%f gust=%f", day15Wind[0].WindSpeedKts, day15Wind[0].WindGustKts)
	}

	day14Precip := precipByDay["2026-06-14"]
	if len(day14Precip) != 1 || day14Precip[0].PrecipitationChancePct != 65 || day14Precip[0].PrecipitationIntensityMm != 1.2 {
		t.Fatalf("unexpected precip bucket: %+v", day14Precip)
	}

	day14UV := uvByDay["2026-06-14"]
	if len(day14UV) != 1 || day14UV[0].UVIndex != 5 {
		t.Fatalf("unexpected uv bucket: %+v", day14UV)
	}

	day14Cloud := cloudByDay["2026-06-14"]
	if len(day14Cloud) != 1 || day14Cloud[0].Condition != "Cloudy" || day14Cloud[0].IsDaylight {
		t.Fatalf("unexpected cloud bucket: %+v", day14Cloud)
	}
	day15Cloud := cloudByDay["2026-06-15"]
	if len(day15Cloud) != 1 || !day15Cloud[0].IsDaylight {
		t.Fatalf("expected IsDaylight true for the 12AM entry, got %+v", day15Cloud)
	}
}

// --- hourly strip (today) ---

func TestBuildWeatherHourlyStrip_SunsetSpliceHasNoWindData(t *testing.T) {
	loc := time.FixedZone("AEST", 10*60*60)
	now := time.Date(2026, 6, 14, 22, 0, 0, 0, time.UTC) // 08:00 AEST on Jun 15... actually Jun 15 08:00
	sunset := time.Date(2026, 6, 15, 8, 30, 0, 0, loc)
	hourly := []weatherHourPoint{
		{Time: time.Date(2026, 6, 14, 22, 0, 0, 0, time.UTC), Condition: "clear", TemperatureC: 20, WindSpeedMS: 9.26, WindDirectionDeg: 90},
		{Time: time.Date(2026, 6, 14, 23, 0, 0, 0, time.UTC), Condition: "clear", TemperatureC: 19, WindSpeedMS: 9.26, WindDirectionDeg: 90},
	}

	entries := buildWeatherHourlyStrip(hourly, now, loc, "2026-06-15", sunset)

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
}

func TestBuildWeatherHourlyStrip_LabelsCurrentHourNow(t *testing.T) {
	loc := time.FixedZone("AEST", 10*60*60)
	now := time.Date(2026, 6, 14, 23, 0, 0, 0, time.UTC)
	hourly := []weatherHourPoint{
		{Time: now, Condition: "clear", TemperatureC: 20, WindSpeedMS: 9.26, WindGustMS: 13.89, WindDirectionDeg: 90},
	}

	entries := buildWeatherHourlyStrip(hourly, now, loc, "2026-06-15", time.Time{})
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Label != "Now" {
		t.Fatalf("expected label 'Now', got %q", entries[0].Label)
	}
	if entries[0].WindDirection != "E" {
		t.Fatalf("expected E direction, got %q", entries[0].WindDirection)
	}
}

// --- day assembly ---

func TestBuildDayData_MapsUnitsAndSummaries(t *testing.T) {
	loc := time.FixedZone("AEST", 10*60*60)
	referenceDatetime := time.Date(2026, 6, 14, 23, 0, 0, 0, time.UTC)
	dayPoint := weatherDayPoint{
		Start:                  time.Date(2026, 6, 14, 14, 0, 0, 0, time.UTC), // local Jun 15 00:00
		Condition:              "rain",
		TempMaxC:               24.0,
		TempMinC:               17.5,
		WindSpeedMS:            5.1,
		WindGustMS:             11.0,
		WindDirectionDeg:       120,
		PrecipitationChancePct: 60,
		Sunrise:                time.Date(2026, 6, 14, 20, 38, 0, 0, time.UTC),
		Sunset:                 time.Date(2026, 6, 15, 7, 9, 0, 0, time.UTC),
	}

	day := buildDayData(dayPoint, referenceDatetime, loc, nil, nil, nil, nil)

	wantHighF := 24.0*9/5 + 32
	if diff := day.HighTempF - wantHighF; diff > 0.01 || diff < -0.01 {
		t.Fatalf("expected HighTempF ~%.2f, got %v", wantHighF, day.HighTempF)
	}
	wantWindKts := 5.1 * metersPerSecondToKnots
	if diff := day.WindSpeedKts - wantWindKts; diff > 0.01 || diff < -0.01 {
		t.Fatalf("expected WindSpeedKts ~%.2f, got %v", wantWindKts, day.WindSpeedKts)
	}
	if day.Condition != "Rain" {
		t.Fatalf("expected condition Rain, got %q", day.Condition)
	}
	if day.SunriseTime == "" || day.SunsetTime == "" {
		t.Fatalf("expected non-empty sunrise/sunset times, got sunrise=%q sunset=%q", day.SunriseTime, day.SunsetTime)
	}
	if day.MoonPhase == "" {
		t.Fatalf("expected a non-empty moon phase")
	}
}

// Precipitation is the one field where the codebase's usual "exactly zero
// means no data" sentinel convention must NOT apply: 0% chance of rain is a
// legitimate, extremely common reading. Absence therefore has to arrive as an
// explicit negative from the plugin, and a real 0 must survive untouched.
// Conflating the two is what showed a confident "0% precip" on a drizzling
// morning.
func TestSentinelPrecipitationPct_DistinguishesRealZeroFromAbsent(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   float64
		want float64
	}{
		{"a real zero stays zero", 0, 0},
		{"a real reading passes through", 43, 43},
		{"100 percent passes through", 100, 100},
		{"negative means absent", -1, -1},
		{"any negative normalizes to the sentinel", -12.5, -1},
		{"out-of-range high clamps", 140, 100},
	} {
		if got := sentinelPrecipitationPct(tc.in); got != tc.want {
			t.Errorf("%s: sentinelPrecipitationPct(%v) = %v, want %v", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestBuildDayData_PropagatesAbsentPrecipitationAsSentinel(t *testing.T) {
	loc := time.FixedZone("AEST", 10*60*60)
	referenceDatetime := time.Date(2026, 8, 9, 20, 0, 0, 0, time.UTC)
	dayPoint := weatherDayPoint{
		Start:                  time.Date(2026, 8, 8, 14, 0, 0, 0, time.UTC),
		Condition:              "drizzle",
		TempMaxC:               21.0,
		TempMinC:               18.0,
		PrecipitationChancePct: -1, // plugin says "upstream did not report this"
	}

	day := buildDayData(dayPoint, referenceDatetime, loc, nil, nil, nil, nil)

	if day.PrecipitationPct != -1 {
		t.Fatalf("expected an absent precipitation chance to stay -1, got %v", day.PrecipitationPct)
	}

	dayPoint.PrecipitationChancePct = 0
	if day := buildDayData(dayPoint, referenceDatetime, loc, nil, nil, nil, nil); day.PrecipitationPct != 0 {
		t.Fatalf("expected a genuine 0%% to stay 0, got %v", day.PrecipitationPct)
	}
}

func TestMapWeatherHourlyPrecipitationResponse_PropagatesAbsentAsSentinel(t *testing.T) {
	entries := []weatherHourlyPrecipitationData{
		{Label: "6AM", HourOfDay: 6, PrecipitationChancePct: -1, PrecipitationIntensityMm: 0},
		{Label: "7AM", HourOfDay: 7, PrecipitationChancePct: 0, PrecipitationIntensityMm: 0},
		{Label: "8AM", HourOfDay: 8, PrecipitationChancePct: 65, PrecipitationIntensityMm: 1.2},
	}

	got := mapWeatherHourlyPrecipitationResponse(entries)

	if got[0].PrecipitationChancePct != -1 {
		t.Errorf("expected absent hourly chance to stay -1, got %v", got[0].PrecipitationChancePct)
	}
	if got[1].PrecipitationChancePct != 0 {
		t.Errorf("expected a genuine 0%% hourly chance to stay 0, got %v", got[1].PrecipitationChancePct)
	}
	if got[2].PrecipitationChancePct != 65 {
		t.Errorf("expected a real hourly chance to pass through, got %v", got[2].PrecipitationChancePct)
	}
}

// resolveGNSSPosition (gnss_validation.go) returns the -1,-1 sentinel when
// GNSS is untrusted and no prior trusted fix exists. A plain range check
// accepts that, because -1,-1 is a syntactically valid coordinate - it is
// just in the Gulf of Guinea rather than under the boat. The repo's own
// weather cache picked up a real "-1.0,-1.0,1" entry this way, meaning a
// forecast for the wrong hemisphere was fetched, cached and displayed.
func TestHasUsableVesselPosition_RejectsSentinelAndOutOfRange(t *testing.T) {
	for _, tc := range []struct {
		name string
		lat  float64
		lon  float64
		want bool
	}{
		{"a real fix is usable", -21.111, 149.228, true},
		{"the untrusted -1,-1 sentinel is not", -1, -1, false},
		{"latitude out of range", 91, 149.228, false},
		{"longitude out of range", -21.111, 181, false},
		{"null island is not a plausible vessel fix", 0, 0, false},
		{"a genuine position near -1 longitude still works", 51.5, -1.0, true},
		{"a genuine position near -1 latitude still works", -1.0, 36.8, true},
	} {
		if got := hasUsableVesselPosition(tc.lat, tc.lon); got != tc.want {
			t.Errorf("%s: hasUsableVesselPosition(%v, %v) = %v, want %v", tc.name, tc.lat, tc.lon, got, tc.want)
		}
	}
}

// End-to-end guard: a vessel reporting the untrusted -1,-1 sentinel must get
// a 502, not a confidently-rendered forecast for the Gulf of Guinea.
func TestWeatherForecast_RejectsSentinelPosition(t *testing.T) {
	withCleanWeatherProviderRegistry(t)
	stub := &stubWeatherProvider{id: "open-meteo", name: "Open-Meteo", ttl: 900}
	registerWeatherProvider(stub)

	server := trustedSignalKPayloadServer(t, -1, -1)
	defer server.Close()

	settingsPath := writeWeatherSettings(t, "open-meteo", server.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/weather-forecast", nil)
	rec := httptest.NewRecorder()

	if err := weatherForecast(e.NewContext(req, rec)); err != nil {
		t.Fatalf("weatherForecast returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 for the -1,-1 sentinel position, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	if stub.gotTimezone != "" {
		t.Errorf("expected the provider never to be called for an unusable position, but it was")
	}
}

// --- humidity/visibility sentinels ---
//
// Humidity and visibility mirror precipitation's absence problem, one level
// worse: precipitation's wire field is a bare float64 that the plugin itself
// already writes -1 into for "no data" (see sentinelPrecipitationPct above).
// Humidity/visibility's wire field is a POINTER
// (wasmWeatherHourOutput.HumidityPct / VisibilityM), so there is a second,
// distinct absence a bare float64 could never represent: a plugin built
// before this feature shipped simply omits the JSON key, which decodes to
// nil. Both nil and a plugin-emitted negative must collapse to -1; a genuine
// 0.0 nm visibility - real fog thick enough to hide the bow - must not.
func TestSentinelHumidityPct_DistinguishesRealZeroFromAbsent(t *testing.T) {
	zero := 0.0
	real := 62.5
	over := 140.0
	negative := -5.0
	for _, tc := range []struct {
		name string
		in   *float64
		want float64
	}{
		{"nil (field absent from the wire entirely) means absent", nil, -1},
		{"a real zero stays zero", &zero, 0},
		{"a real reading passes through", &real, 62.5},
		{"a negative from the plugin means absent", &negative, -1},
		{"out-of-range high clamps to 100", &over, 100},
	} {
		if got := sentinelHumidityPct(tc.in); got != tc.want {
			t.Errorf("%s: sentinelHumidityPct(%v) = %v, want %v", tc.name, tc.in, got, tc.want)
		}
	}
}

// This is the whole point of the pointer-based convention sentinelVisibilityNm
// documents: a real 0.0 metre reading (dense fog) MUST survive as 0.0 nm, not
// collapse into the same -1 sentinel as "field absent". A bare float64 field
// could never make this distinction, because JSON-absent and JSON-zero both
// decode to 0.0 - the same failure mode that once rendered a confident "0%
// precip" during real rainfall (see docs/adr/0035-weather-local-day-boundaries.md).
// This is the most important test in this change.
func TestSentinelVisibilityNm_RealZeroSurvivesButAbsenceDoesNot(t *testing.T) {
	zero := 0.0
	oneNm := 1852.0
	negative := -1.0
	for _, tc := range []struct {
		name string
		in   *float64
		want float64
	}{
		{"nil (field absent from the wire entirely) means absent", nil, -1},
		{"a genuine 0.0m reading (real fog) survives as 0.0nm, not absent", &zero, 0},
		{"1852m converts to exactly 1nm", &oneNm, 1},
		{"a negative from the plugin means absent", &negative, -1},
	} {
		if got := sentinelVisibilityNm(tc.in); got != tc.want {
			t.Errorf("%s: sentinelVisibilityNm(%v) = %v, want %v", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestBuildHourlySeriesByDay_CarriesHumidityAndVisibilityIntoCloudSeries(t *testing.T) {
	loc := time.FixedZone("AEST", 10*60*60)
	// 03:00/04:00 UTC = 13:00/14:00 AEST - both land on the same local date,
	// unlike the day-boundary-crossing times TestBuildHourlySeriesByDay_BucketsHoursByLocalDate uses.
	hourly := []weatherHourPoint{
		{Time: time.Date(2026, 6, 14, 3, 0, 0, 0, time.UTC), TemperatureC: 18, Condition: "cloudy", HumidityPct: 55, VisibilityNm: 8},
		{Time: time.Date(2026, 6, 14, 4, 0, 0, 0, time.UTC), TemperatureC: 17, Condition: "cloudy", HumidityPct: -1, VisibilityNm: -1},
	}

	_, _, _, cloudByDay := buildHourlySeriesByDay(hourly, loc)

	day := cloudByDay["2026-06-14"]
	if len(day) != 2 {
		t.Fatalf("expected 2 cloud entries, got %d", len(day))
	}
	if day[0].HumidityPct != 55 || day[0].VisibilityNm != 8 {
		t.Fatalf("expected the real hour's humidity/visibility to pass through, got %+v", day[0])
	}
	if day[1].HumidityPct != -1 || day[1].VisibilityNm != -1 {
		t.Fatalf("expected the absent hour's sentinel to pass through, got %+v", day[1])
	}
}

// buildWindSummary's exact shape - `< 0` skip, `found` flag, absent when
// nothing qualifies - applied to humidity: an absent hour must not drag the
// mean down toward a fabricated low reading.
func TestReduceHumidityPct_MeansValidSamplesSkippingAbsent(t *testing.T) {
	hourly := []weatherHourlyCloudData{
		{HumidityPct: 40},
		{HumidityPct: -1}, // absent hour must not drag the mean down
		{HumidityPct: 60},
	}
	if got := reduceHumidityPct(hourly); got != 50 {
		t.Fatalf("expected mean of valid samples (40,60) = 50, got %v", got)
	}
}

func TestReduceHumidityPct_AllAbsentStaysSentinel(t *testing.T) {
	hourly := []weatherHourlyCloudData{{HumidityPct: -1}, {HumidityPct: -1}}
	if got := reduceHumidityPct(hourly); got != -1 {
		t.Fatalf("expected -1 when every hour is absent, not a fabricated 0, got %v", got)
	}
}

// Visibility is hazard-shaped in the low direction: the worst (minimum)
// reading of the day is what a skipper needs, not a mean that would smooth a
// 0.5nm fog bank into a comfortable-looking 8nm.
func TestReduceVisibilityNm_TakesMinimumOfValidSamplesSkippingAbsent(t *testing.T) {
	hourly := []weatherHourlyCloudData{
		{VisibilityNm: 10},
		{VisibilityNm: -1}, // absent hour must not win as the "worst" reading
		{VisibilityNm: 3},
		{VisibilityNm: 6},
	}
	if got := reduceVisibilityNm(hourly); got != 3 {
		t.Fatalf("expected the minimum of valid samples (3), got %v", got)
	}
}

func TestReduceVisibilityNm_AllAbsentStaysSentinel(t *testing.T) {
	hourly := []weatherHourlyCloudData{{VisibilityNm: -1}, {VisibilityNm: -1}}
	if got := reduceVisibilityNm(hourly); got != -1 {
		t.Fatalf("expected -1 when every hour is absent, not a fabricated 0, got %v", got)
	}
}

// The single most important test in this change: a genuine 0.0nm visibility
// reading - fog thick enough that the bow is out of sight - must survive the
// day-level MIN reduction as a real 0.0, not be mistaken for "no data" and
// reported as the -1 sentinel.
func TestReduceVisibilityNm_GenuineZeroSurvivesAsTheWorstReading(t *testing.T) {
	hourly := []weatherHourlyCloudData{
		{VisibilityNm: 8},
		{VisibilityNm: 0}, // real fog - must win as the day's minimum
		{VisibilityNm: 5},
	}
	if got := reduceVisibilityNm(hourly); got != 0 {
		t.Fatalf("expected a genuine 0.0nm reading to survive as the day's minimum, got %v", got)
	}
}

func TestBuildDayData_SetsHumidityAndVisibilityFromCloudSeries(t *testing.T) {
	loc := time.FixedZone("AEST", 10*60*60)
	referenceDatetime := time.Date(2026, 6, 14, 23, 0, 0, 0, time.UTC)
	dayPoint := weatherDayPoint{
		Start:     time.Date(2026, 6, 14, 14, 0, 0, 0, time.UTC),
		Condition: "cloudy",
	}
	cloudSeries := []weatherHourlyCloudData{
		{HumidityPct: 40, VisibilityNm: 10},
		{HumidityPct: 60, VisibilityNm: 4},
	}

	day := buildDayData(dayPoint, referenceDatetime, loc, nil, nil, nil, cloudSeries)

	if day.HumidityPct != 50 {
		t.Fatalf("expected day humidity to be the mean (50), got %v", day.HumidityPct)
	}
	if day.VisibilityNm != 4 {
		t.Fatalf("expected day visibility to be the minimum (4), got %v", day.VisibilityNm)
	}
}

// ALL hours absent must report -1 for the day, not 0 - the same "0% precip
// during actual rainfall" failure mode ADR 0035 documents, but graver here:
// a fabricated 0.0nm visibility on a clear day would read as "you cannot see
// the bow" when nobody ever measured visibility at all.
func TestBuildDayData_AllHoursAbsentHumidityAndVisibilityStaySentinelNotZero(t *testing.T) {
	loc := time.FixedZone("AEST", 10*60*60)
	referenceDatetime := time.Date(2026, 6, 14, 23, 0, 0, 0, time.UTC)
	dayPoint := weatherDayPoint{Start: time.Date(2026, 6, 14, 14, 0, 0, 0, time.UTC), Condition: "cloudy"}
	cloudSeries := []weatherHourlyCloudData{
		{HumidityPct: -1, VisibilityNm: -1},
		{HumidityPct: -1, VisibilityNm: -1},
	}

	day := buildDayData(dayPoint, referenceDatetime, loc, nil, nil, nil, cloudSeries)

	if day.HumidityPct != -1 {
		t.Fatalf("expected an all-absent day to report humidity -1, not a fabricated 0, got %v", day.HumidityPct)
	}
	if day.VisibilityNm != -1 {
		t.Fatalf("expected an all-absent day to report visibility -1, not a fabricated 0, got %v", day.VisibilityNm)
	}
}

func TestMapWeatherForecastDayResponse_IncludesHumidityAndVisibility(t *testing.T) {
	day := weatherForecastDayData{HumidityPct: 62, VisibilityNm: 7.5}
	got := mapWeatherForecastDayResponse(day, "2026-06-14")
	if got.HumidityPct != 62 || got.VisibilityNm != 7.5 {
		t.Fatalf("expected humidity/visibility to pass through to the response, got %+v", got)
	}
}

func TestMapWeatherHourlyCloudResponse_IncludesHumidityAndVisibility(t *testing.T) {
	entries := []weatherHourlyCloudData{{Label: "6AM", HourOfDay: 6, HumidityPct: 71, VisibilityNm: -1}}
	got := mapWeatherHourlyCloudResponse(entries)
	if got[0].HumidityPct != 71 {
		t.Fatalf("expected humidity to pass through, got %v", got[0].HumidityPct)
	}
	if got[0].VisibilityNm != -1 {
		t.Fatalf("expected absent visibility sentinel to pass through, got %v", got[0].VisibilityNm)
	}
}

// The vessel's local zone - not UTC - must reach the plugin, so the provider
// rolls its daily summaries up on the same boundaries the host buckets on.
func TestWeatherForecast_PassesVesselLocalTimezoneToProvider(t *testing.T) {
	withCleanWeatherProviderRegistry(t)
	now := time.Date(2026, 8, 9, 20, 0, 0, 0, time.UTC)
	stub := &stubWeatherProvider{
		id: "open-meteo", name: "Open-Meteo", ttl: 900,
		bundle: weatherForecastBundle{
			Current: weatherCurrentPoint{Time: now, TemperatureC: 19, Condition: "drizzle"},
			Days: []weatherDayPoint{
				{Start: time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC), Condition: "drizzle", TempMaxC: 22, TempMinC: 18, PrecipitationChancePct: 91},
			},
			CachedAt: now,
		},
	}
	registerWeatherProvider(stub)

	server := trustedSignalKPayloadServer(t, -21.1113, 149.2277) // Mackay, UTC+10
	defer server.Close()

	settingsPath := writeWeatherSettings(t, "open-meteo", server.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/weather-forecast", nil)
	rec := httptest.NewRecorder()

	if err := weatherForecast(e.NewContext(req, rec)); err != nil {
		t.Fatalf("weatherForecast returned error: %v", err)
	}
	if stub.gotTimezone != "Etc/GMT-10" {
		t.Fatalf("expected the vessel's local zone Etc/GMT-10 to reach the provider, got %q", stub.gotTimezone)
	}
}
