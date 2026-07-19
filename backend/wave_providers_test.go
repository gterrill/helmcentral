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

// withCleanWaveProviderRegistry saves the global wave provider registry,
// resets it to empty for the duration of the test, and restores the
// original afterwards - mirrors withCleanWeatherProviderRegistry.
func withCleanWaveProviderRegistry(t *testing.T) {
	t.Helper()
	origRegistry := waveProviderRegistry
	origOrder := waveProviderOrder
	waveProviderRegistry = map[string]waveProvider{}
	waveProviderOrder = nil
	t.Cleanup(func() {
		waveProviderRegistry = origRegistry
		waveProviderOrder = origOrder
	})
}

type stubWaveProvider struct {
	id     string
	name   string
	ttl    int64
	bundle waveForecastBundle
	err    error
}

func (s *stubWaveProvider) ID() string        { return s.id }
func (s *stubWaveProvider) Name() string      { return s.name }
func (s *stubWaveProvider) TTLSeconds() int64 { return s.ttl }
func (s *stubWaveProvider) FetchWaves(lat, lon float64, days int) (waveForecastBundle, error) {
	if s.err != nil {
		return waveForecastBundle{}, s.err
	}
	return s.bundle, nil
}

func writeWaveSettings(t *testing.T, provider, signalkURL string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.yaml")
	content := fmt.Sprintf("signalk:\n  address: %s\n  port: 0\nui:\n  wave_provider: %s\n", signalkURL, provider)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write test settings file: %v", err)
	}
	return path
}

// trustedSignalKPayloadServer is defined in signalk_test.go and reused here.

func TestRegisterWaveProvider_PreservesRegistrationOrder(t *testing.T) {
	withCleanWaveProviderRegistry(t)

	registerWaveProvider(&stubWaveProvider{id: "b", name: "B"})
	registerWaveProvider(&stubWaveProvider{id: "a", name: "A"})

	if len(waveProviderOrder) != 2 || waveProviderOrder[0] != "b" || waveProviderOrder[1] != "a" {
		t.Fatalf("expected order [b a], got %v", waveProviderOrder)
	}

	if _, ok := getWaveProvider("a"); !ok {
		t.Fatalf("expected provider a to be registered")
	}
	if _, ok := getWaveProvider("missing"); ok {
		t.Fatalf("expected provider 'missing' to not be registered")
	}
}

func TestWaveProvidersHandler_ReturnsRegisteredProvidersInOrder(t *testing.T) {
	withCleanWaveProviderRegistry(t)
	registerWaveProvider(&stubWaveProvider{id: "open-meteo-marine", name: "Open-Meteo Marine"})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/wave-providers", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := waveProvidersHandler(c); err != nil {
		t.Fatalf("waveProvidersHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var payload []waveProviderInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(payload) != 1 || payload[0].ID != "open-meteo-marine" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestWaveForecast_ReturnsBadGatewayForUnknownProvider(t *testing.T) {
	withCleanWaveProviderRegistry(t)
	settingsPath := writeWaveSettings(t, "not-a-real-provider", "http://127.0.0.1:1")
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/wave-forecast", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := waveForecast(c); err != nil {
		t.Fatalf("waveForecast returned error: %v", err)
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

// TestWaveForecast_502sWithZeroRegisteredProviders proves that a
// freshly-booted install with no plugins/waves/*.wasm installed yet (the
// WASM-only-registry, no-native-built-in state main.go leaves things in)
// fails loudly instead of ever returning fake data.
func TestWaveForecast_502sWithZeroRegisteredProviders(t *testing.T) {
	withCleanWaveProviderRegistry(t)
	settingsPath := writeWaveSettings(t, "open-meteo-marine", "http://127.0.0.1:1")
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/wave-forecast", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := waveForecast(c); err != nil {
		t.Fatalf("waveForecast returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 with no registered wave providers, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestWaveForecast_ReturnsBadGatewayWhenProviderFetchFails(t *testing.T) {
	withCleanWaveProviderRegistry(t)
	registerWaveProvider(&stubWaveProvider{id: "open-meteo-marine", name: "Open-Meteo Marine", ttl: 900, err: fmt.Errorf("simulated upstream failure")})

	server := trustedSignalKPayloadServer(t, -27.4, 153.0)
	defer server.Close()

	settingsPath := writeWaveSettings(t, "open-meteo-marine", server.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/wave-forecast", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := waveForecast(c); err != nil {
		t.Fatalf("waveForecast returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on fetch error (no fake defaults), got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestWaveForecast_ReturnsBadGatewayWhenHourlyEmpty(t *testing.T) {
	withCleanWaveProviderRegistry(t)
	registerWaveProvider(&stubWaveProvider{id: "open-meteo-marine", name: "Open-Meteo Marine", ttl: 900, bundle: waveForecastBundle{}})

	server := trustedSignalKPayloadServer(t, -27.4, 153.0)
	defer server.Close()

	settingsPath := writeWaveSettings(t, "open-meteo-marine", server.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/wave-forecast", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := waveForecast(c); err != nil {
		t.Fatalf("waveForecast returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 for an empty hourly forecast, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestWaveForecast_ReturnsOKWithDayBucketingAndSeaTempNull(t *testing.T) {
	withCleanWaveProviderRegistry(t)

	loc := vesselLocalLocation(153.0)
	nowLocal := time.Now().In(loc)
	todayLocalMidnight := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), 0, 0, 0, 0, loc)
	day2Start := todayLocalMidnight.AddDate(0, 0, 1)

	bundle := waveForecastBundle{
		Hourly: []waveHourPoint{
			{Time: todayLocalMidnight.Add(time.Hour), WaveHeightM: 1.1, WavePeriodS: 8.0, WaveDirectionDeg: 90, WindWaveHeightM: 0.3, SwellWaveHeightM: 0.9},
			{Time: todayLocalMidnight.Add(2 * time.Hour), WaveHeightM: 1.3, WavePeriodS: 8.5, WaveDirectionDeg: 100, WindWaveHeightM: 0.4, SwellWaveHeightM: 1.0},
			{Time: day2Start.Add(time.Hour), WaveHeightM: 1.5, WavePeriodS: 9.0, WaveDirectionDeg: 110, WindWaveHeightM: 0.5, SwellWaveHeightM: 1.1},
		},
		SeaTempC: nil,
		Cached:   false,
		CachedAt: time.Now().UTC(),
	}
	registerWaveProvider(&stubWaveProvider{id: "open-meteo-marine", name: "Open-Meteo Marine", ttl: 900, bundle: bundle})

	server := trustedSignalKPayloadServer(t, -27.4, 153.0)
	defer server.Close()

	settingsPath := writeWaveSettings(t, "open-meteo-marine", server.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/wave-forecast", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := waveForecast(c); err != nil {
		t.Fatalf("waveForecast returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if !bodyContains(body, `"sea_temperature_f":null`) {
		t.Fatalf("expected sea_temperature_f to be JSON null when SeaTempC is nil, got: %s", body)
	}

	var payload waveForecastResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if payload.Provider != "open-meteo-marine" {
		t.Fatalf("expected provider open-meteo-marine, got %q", payload.Provider)
	}
	if payload.SeaTemperatureF != nil {
		t.Fatalf("expected nil SeaTemperatureF, got %v", *payload.SeaTemperatureF)
	}
	if len(payload.Days) != 2 {
		t.Fatalf("expected 2 days, got %d: %+v", len(payload.Days), payload.Days)
	}
	if len(payload.Days[0].HourlyWave) != 2 {
		t.Fatalf("expected 2 hourly entries on day 0, got %d", len(payload.Days[0].HourlyWave))
	}
	if payload.Days[0].WaveSummary == "" {
		t.Fatalf("expected a non-empty wave summary on day 0")
	}
	if payload.Days[0].DayKey == "" || payload.Days[0].Date == "" || payload.Days[0].DayName == "" {
		t.Fatalf("expected non-empty day_key/date/day_name, got %+v", payload.Days[0])
	}
}

func TestWaveForecast_ReturnsRealSeaTemperatureFWhenPresent(t *testing.T) {
	withCleanWaveProviderRegistry(t)

	loc := vesselLocalLocation(153.0)
	nowLocal := time.Now().In(loc)
	todayLocalMidnight := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), 0, 0, 0, 0, loc)

	seaTempC := 21.0
	bundle := waveForecastBundle{
		Hourly: []waveHourPoint{
			{Time: todayLocalMidnight.Add(time.Hour), WaveHeightM: 1.1, WavePeriodS: 8.0, WaveDirectionDeg: 90},
		},
		SeaTempC: &seaTempC,
		Cached:   false,
		CachedAt: time.Now().UTC(),
	}
	registerWaveProvider(&stubWaveProvider{id: "open-meteo-marine", name: "Open-Meteo Marine", ttl: 900, bundle: bundle})

	server := trustedSignalKPayloadServer(t, -27.4, 153.0)
	defer server.Close()

	settingsPath := writeWaveSettings(t, "open-meteo-marine", server.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/wave-forecast", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := waveForecast(c); err != nil {
		t.Fatalf("waveForecast returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var payload waveForecastResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if payload.SeaTemperatureF == nil {
		t.Fatalf("expected a non-nil SeaTemperatureF")
	}
	wantF := seaTempC*9/5 + 32
	if diff := *payload.SeaTemperatureF - wantF; diff > 0.01 || diff < -0.01 {
		t.Fatalf("expected SeaTemperatureF ~%.2f, got %v", wantF, *payload.SeaTemperatureF)
	}
}

// --- day bucketing ---

func TestBuildWaveHourlySeriesByDay_BucketsHoursByLocalDate(t *testing.T) {
	loc := time.FixedZone("AEST", 10*60*60)
	hourly := []waveHourPoint{
		{Time: time.Date(2026, 6, 14, 13, 0, 0, 0, time.UTC), WaveHeightM: 1.2, WavePeriodS: 8.5, WaveDirectionDeg: 90, WindWaveHeightM: 0.4, SwellWaveHeightM: 0.9},  // 23:00 AEST Jun 14
		{Time: time.Date(2026, 6, 14, 14, 0, 0, 0, time.UTC), WaveHeightM: 1.4, WavePeriodS: 8.9, WaveDirectionDeg: 100, WindWaveHeightM: 0.5, SwellWaveHeightM: 1.0}, // 00:00 AEST Jun 15
	}

	byDay := buildWaveHourlySeriesByDay(hourly, loc)

	day14 := byDay["2026-06-14"]
	if len(day14) != 1 {
		t.Fatalf("expected 1 entry for Jun 14, got %d", len(day14))
	}
	if day14[0].Label != "11PM" || day14[0].HourOfDay != 23 {
		t.Fatalf("unexpected day14 bucket: %+v", day14[0])
	}
	if day14[0].WaveHeightM != 1.2 || day14[0].WavePeriodS != 8.5 || day14[0].WaveDirectionDeg != 90 {
		t.Fatalf("unexpected day14 wave fields: %+v", day14[0])
	}

	day15 := byDay["2026-06-15"]
	if len(day15) != 1 || day15[0].HourOfDay != 0 {
		t.Fatalf("expected 1 entry at hour 0 for Jun 15, got %+v", day15)
	}
}

// --- buildWaveSummary / waveDirectionRange (ported from weather_tide_test.go) ---

func TestBuildWaveSummary_FormatsRangeDirectionAndPeriod(t *testing.T) {
	hourly := []waveDayHourlyData{
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
	hourly := []waveDayHourlyData{
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

	hourly := []waveDayHourlyData{{WaveHeightM: -1, WavePeriodS: -1, WaveDirectionDeg: -1}}
	if summary := buildWaveSummary(hourly); summary != "" {
		t.Fatalf("expected empty summary for all-missing data, got %q", summary)
	}
}

func TestWaveDirectionRange_SingleAndRangeAndMissing(t *testing.T) {
	single := waveDirectionRange([]waveDayHourlyData{{WaveDirectionDeg: 90}, {WaveDirectionDeg: 90}})
	if single != "E" {
		t.Fatalf("expected E, got %q", single)
	}

	rangeResult := waveDirectionRange([]waveDayHourlyData{{WaveDirectionDeg: 63}, {WaveDirectionDeg: 67}, {WaveDirectionDeg: 90}})
	if rangeResult != "ENE-E" {
		t.Fatalf("expected ENE-E, got %q", rangeResult)
	}

	if empty := waveDirectionRange([]waveDayHourlyData{{WaveDirectionDeg: -1}}); empty != "" {
		t.Fatalf("expected empty, got %q", empty)
	}
}
