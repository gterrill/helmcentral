package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func (s *stubWaveProvider) ID() string          { return s.id }
func (s *stubWaveProvider) Name() string        { return s.name }
func (s *stubWaveProvider) Description() string { return "Stub wave provider for tests" }
func (s *stubWaveProvider) TTLSeconds() int64   { return s.ttl }
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

// --- steepness (ADR 0070) ---

// TestWaveSteepness_BookWorkedExample reproduces the example Surviving the
// Storm works through on page 252: a 40-foot (12.2m) sea with a 14-to-16
// second period is a slope of "1 in 25", which the text calls "not normally
// considered steep enough to break". Both ends of that period range must
// therefore land outside the breaking band.
func TestWaveSteepness_BookWorkedExample(t *testing.T) {
	for _, tc := range []struct {
		periodS   float64
		wantRatio float64
	}{
		{14, 0.0399},
		{16, 0.0305},
	} {
		got, ok := waveSteepness(12.2, tc.periodS)
		if !ok {
			t.Fatalf("period %.0fs: expected a defined steepness", tc.periodS)
		}
		if math.Abs(got-tc.wantRatio) > 0.0005 {
			t.Fatalf("period %.0fs: steepness = %.4f, want ~%.4f", tc.periodS, got, tc.wantRatio)
		}
		if band := waveSteepnessBand(got); band == waveSteepnessBreaking {
			t.Fatalf("period %.0fs: the book calls 1-in-25 non-breaking, got band %q", tc.periodS, band)
		}
	}
}

func TestWaveSteepnessBand_Thresholds(t *testing.T) {
	for _, tc := range []struct {
		name     string
		heightM  float64
		periodS  float64
		wantBand string
	}{
		{"long swell is rolling", 2, 12, waveSteepnessRolling},
		{"moderate sea is building", 3, 6, waveSteepnessBuilding},
		{"short steep sea", 3, 5, waveSteepnessSteep},
		{"past one in ten breaks", 4, 5, waveSteepnessBreaking},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ratio, ok := waveSteepness(tc.heightM, tc.periodS)
			if !ok {
				t.Fatalf("expected a defined steepness for %.1fm at %.1fs", tc.heightM, tc.periodS)
			}
			if band := waveSteepnessBand(ratio); band != tc.wantBand {
				t.Fatalf("%.1fm at %.1fs: ratio %.4f banded %q, want %q", tc.heightM, tc.periodS, ratio, band, tc.wantBand)
			}
		})
	}
}

// The breaking threshold is the book's real-world 1-in-10 (page 231), not the
// 1-in-7 tank-test figure the same page reports and then discounts.
func TestWaveSteepnessBand_BreaksAtOneInTen(t *testing.T) {
	if band := waveSteepnessBand(0.0999); band == waveSteepnessBreaking {
		t.Fatalf("just under 1-in-10 should not be breaking, got %q", band)
	}
	if band := waveSteepnessBand(0.1001); band != waveSteepnessBreaking {
		t.Fatalf("past 1-in-10 should be breaking, got %q", band)
	}
}

// Absence, never a masked zero: a zero steepness would render as a
// perfectly flat sea, which is a measurement rather than the lack of one.
func TestWaveSteepness_AbsentWhenUndefined(t *testing.T) {
	for _, tc := range []struct {
		name    string
		heightM float64
		periodS float64
	}{
		{"no period", 2.0, 0},
		{"negative period", 2.0, -1},
		{"negative height marker", -1, 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, ok := waveSteepness(tc.heightM, tc.periodS); ok {
				t.Fatalf("expected absence, got %.4f", got)
			}
		})
	}
}

func TestBuildWaveSummary_NamesPeakBandOnlyWhenItMatters(t *testing.T) {
	// A long rolling swell says nothing extra: the summary would be cluttered
	// on every benign day, which is most days.
	rolling := []waveDayHourlyData{
		{WaveHeightM: 1.2, WavePeriodS: 12, WaveDirectionDeg: 90},
	}
	if summary := buildWaveSummary(rolling); strings.Contains(summary, "Steepness") {
		t.Fatalf("a rolling day should not mention steepness, got %q", summary)
	}

	// A day that reaches the breaking band does, because that is the number
	// Surviving the Storm argues actually matters.
	breaking := []waveDayHourlyData{
		{WaveHeightM: 1.2, WavePeriodS: 12, WaveDirectionDeg: 90},
		{WaveHeightM: 4.0, WavePeriodS: 5, WaveDirectionDeg: 90},
	}
	summary := buildWaveSummary(breaking)
	if !strings.Contains(summary, "Steepness peaks at 1:9") {
		t.Fatalf("expected the peak steepness ratio in the summary, got %q", summary)
	}
	if !strings.Contains(summary, waveSteepnessBreaking) {
		t.Fatalf("expected the band named in the summary, got %q", summary)
	}
}

func TestMapWaveHourlyResponse_CarriesSteepness(t *testing.T) {
	got := mapWaveHourlyResponse([]waveDayHourlyData{
		{WaveHeightM: 4.0, WavePeriodS: 5},
		{WaveHeightM: 2.0, WavePeriodS: 0},
	})
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}

	if got[0].SteepnessRatio == nil {
		t.Fatalf("expected a steepness ratio for 4.0m at 5s")
	}
	if math.Abs(*got[0].SteepnessRatio-0.1025) > 0.0005 {
		t.Fatalf("steepness = %.4f, want ~0.1025", *got[0].SteepnessRatio)
	}
	if got[0].SteepnessBand != waveSteepnessBreaking {
		t.Fatalf("band = %q, want %q", got[0].SteepnessBand, waveSteepnessBreaking)
	}

	// No period means no steepness, and the band stays empty rather than
	// defaulting to the calmest one - an absent reading is not a calm sea.
	if got[1].SteepnessRatio != nil {
		t.Fatalf("expected absence with no period, got %.4f", *got[1].SteepnessRatio)
	}
	if got[1].SteepnessBand != "" {
		t.Fatalf("expected an empty band with no period, got %q", got[1].SteepnessBand)
	}
}

// --- leading indicators (ADR 0070) ---

// hourlyRamp builds a day whose height and period follow the given series,
// one entry per hour from midnight.
func hourlyRamp(heights, periods []float64) []waveDayHourlyData {
	out := make([]waveDayHourlyData, 0, len(heights))
	for i := range heights {
		out = append(out, waveDayHourlyData{
			Label:       fmt.Sprintf("%dh", i),
			HourOfDay:   i,
			WaveHeightM: heights[i],
			WavePeriodS: periods[i],
		})
	}
	return out
}

// Scott Prosise screened a year of buoy data for a 10-foot (3m) rise inside
// three hours and found ten instances on the whole US West Coast (page 246).
// That is the threshold, so 3m in three hours trips and 2.9m does not.
func TestWaveIndicators_WaveFrontRise(t *testing.T) {
	tripped := waveDayIndicators(hourlyRamp(
		[]float64{1.0, 1.5, 2.5, 4.0, 4.1},
		[]float64{8, 8, 8, 8, 8},
	))
	if !tripped.WaveFront {
		t.Fatalf("a 3.0m rise across three hours should trip the wave-front flag")
	}

	quiet := waveDayIndicators(hourlyRamp(
		[]float64{1.0, 1.5, 2.5, 3.9, 3.9},
		[]float64{8, 8, 8, 8, 8},
	))
	if quiet.WaveFront {
		t.Fatalf("a 2.9m rise across three hours should not trip the wave-front flag")
	}
}

// Lee Chesneau of the Marine Prediction Center, page 250: "A wave height and
// period increase of 50 percent in an hour is a certain danger signal." Both
// have to rise; either one alone is ordinary weather.
func TestWaveIndicators_RapidBuildNeedsBoth(t *testing.T) {
	both := waveDayIndicators(hourlyRamp(
		[]float64{2.0, 3.0},
		[]float64{8, 12},
	))
	if !both.RapidBuild {
		t.Fatalf("height and period both up 50%% in an hour should trip the rapid-build flag")
	}

	heightOnly := waveDayIndicators(hourlyRamp(
		[]float64{2.0, 3.0},
		[]float64{8, 8},
	))
	if heightOnly.RapidBuild {
		t.Fatalf("height alone should not trip the rapid-build flag")
	}
}

// Prosise again, page 250: the period lengthening sharply just before onset,
// "a typical 8-second period that suddenly lengthens to 11 seconds".
func TestWaveIndicators_PeriodStep(t *testing.T) {
	stepped := waveDayIndicators(hourlyRamp(
		[]float64{2.0, 2.0},
		[]float64{8, 11},
	))
	if !stepped.PeriodStep {
		t.Fatalf("8s to 11s in an hour should trip the period-step flag")
	}

	gradual := waveDayIndicators(hourlyRamp(
		[]float64{2.0, 2.0, 2.0},
		[]float64{8, 9, 10},
	))
	if gradual.PeriodStep {
		t.Fatalf("a gradual 1s-per-hour lengthening should not trip the period-step flag")
	}
}

// Absence must not read as calm: a day with one hour, or with the negative
// markers this file uses for missing data, reports nothing rather than false
// reassurance.
func TestWaveIndicators_AbsentDataTripsNothing(t *testing.T) {
	for _, tc := range []struct {
		name   string
		hourly []waveDayHourlyData
	}{
		{"empty", nil},
		{"single hour", hourlyRamp([]float64{2.0}, []float64{8})},
		{"missing markers", hourlyRamp([]float64{-1, -1, -1, -1}, []float64{-1, -1, -1, -1})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := waveDayIndicators(tc.hourly)
			if got.WaveFront || got.RapidBuild || got.PeriodStep {
				t.Fatalf("expected no indicators, got %+v", got)
			}
		})
	}
}

// The indicators have to reach the wire, not just exist as a helper.
func TestWaveForecast_CarriesDayIndicators(t *testing.T) {
	withCleanWaveProviderRegistry(t)

	loc := vesselLocalLocation(153.0)
	nowLocal := time.Now().In(loc)
	midnight := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), 0, 0, 0, 0, loc)

	// A 1.0m to 4.2m build across three hours: a wave front by Prosise's
	// criterion, with the period stepping 8s to 11s on the way.
	bundle := waveForecastBundle{
		Hourly: []waveHourPoint{
			{Time: midnight.Add(time.Hour), WaveHeightM: 1.0, WavePeriodS: 8, WaveDirectionDeg: 90},
			{Time: midnight.Add(2 * time.Hour), WaveHeightM: 1.6, WavePeriodS: 8, WaveDirectionDeg: 90},
			{Time: midnight.Add(3 * time.Hour), WaveHeightM: 2.6, WavePeriodS: 9, WaveDirectionDeg: 90},
			{Time: midnight.Add(4 * time.Hour), WaveHeightM: 4.2, WavePeriodS: 12, WaveDirectionDeg: 90},
		},
		CachedAt: time.Now().UTC(),
	}
	registerWaveProvider(&stubWaveProvider{id: "open-meteo-marine", name: "Open-Meteo Marine", ttl: 900, bundle: bundle})

	server := trustedSignalKPayloadServer(t, -27.4, 153.0)
	defer server.Close()
	t.Setenv("SETTINGS_FILE", writeWaveSettings(t, "open-meteo-marine", server.URL))

	e := echo.New()
	rec := httptest.NewRecorder()
	if err := waveForecast(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/wave-forecast", nil), rec)); err != nil {
		t.Fatalf("waveForecast returned error: %v", err)
	}

	var payload waveForecastResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(payload.Days) == 0 {
		t.Fatalf("expected at least one day")
	}
	got := payload.Days[0].Indicators
	if !got.WaveFront {
		t.Fatalf("expected the wave-front indicator on the wire, got %+v", got)
	}
	if !got.PeriodStep {
		t.Fatalf("expected the period-step indicator on the wire, got %+v", got)
	}
}

// --- cross seas (ADR 0070) ---

// Verified against a live Open-Meteo Marine response at the vessel's own
// position: when a component has no waves in it, the provider reports its
// height, period AND direction as 0 rather than null. At 2026-09-03T00:00
// the wind wave was 0.6m from 79 degrees and the swell was flat, reported as
// "0 degrees". Reading that as a 79-degree separation would flag a cross sea
// on an ordinary single-system day, which is most days.
func TestCrossSea_IgnoresAComponentThatIsNotThere(t *testing.T) {
	noSwell := waveDayHourlyData{
		WaveHeightM: 0.6, WavePeriodS: 7.4,
		WindWaveHeightM: 0.6, WindWaveDirectionDeg: 79, WindWavePeriodS: 7.4,
		SwellWaveHeightM: 0, SwellWaveDirectionDeg: 0, SwellWavePeriodS: 0,
	}
	if _, ok := crossSeaSeparationDeg(noSwell); ok {
		t.Fatalf("a flat swell component must not produce a separation")
	}

	noWindWave := waveDayHourlyData{
		WaveHeightM: 0.52, WavePeriodS: 7.45,
		WindWaveHeightM: 0, WindWaveDirectionDeg: 0, WindWavePeriodS: 0,
		SwellWaveHeightM: 0.52, SwellWaveDirectionDeg: 73, SwellWavePeriodS: 7.45,
	}
	if _, ok := crossSeaSeparationDeg(noWindWave); ok {
		t.Fatalf("a flat wind-wave component must not produce a separation")
	}
}

// The genuine two-system hour from that same response: wind wave 0.26m from
// 109 degrees at 2.35s over a 0.44m swell from 65 degrees at 7.7s. That is a
// real 44-degree separation, and below the threshold, so it is reported as a
// number but does not trip the flag.
func TestCrossSea_RealTwoSystemHour(t *testing.T) {
	hour := waveDayHourlyData{
		WaveHeightM: 0.5, WavePeriodS: 7.7,
		WindWaveHeightM: 0.26, WindWaveDirectionDeg: 109, WindWavePeriodS: 2.35,
		SwellWaveHeightM: 0.44, SwellWaveDirectionDeg: 65, SwellWavePeriodS: 7.7,
	}
	separation, ok := crossSeaSeparationDeg(hour)
	if !ok {
		t.Fatalf("expected a separation for a genuine two-system sea")
	}
	if math.Abs(separation-44) > 0.5 {
		t.Fatalf("separation = %.1f degrees, want 44", separation)
	}
	if waveDayIndicators([]waveDayHourlyData{hour}).CrossSea {
		t.Fatalf("44 degrees is within one system's own spread and must not flag")
	}
}

// Page 233 puts a single system's spread at "plus or minus 20 to 30 degrees"
// off the wind axis, so past 60 the simpler explanation is two systems.
func TestCrossSea_FlagsPastSixtyDegrees(t *testing.T) {
	crossing := waveDayHourlyData{
		WaveHeightM: 2.0, WavePeriodS: 9,
		WindWaveHeightM: 1.2, WindWaveDirectionDeg: 200, WindWavePeriodS: 5,
		SwellWaveHeightM: 1.6, SwellWaveDirectionDeg: 110, SwellWavePeriodS: 12,
	}
	separation, ok := crossSeaSeparationDeg(crossing)
	if !ok || math.Abs(separation-90) > 0.5 {
		t.Fatalf("separation = %.1f (ok=%v), want 90", separation, ok)
	}
	if !waveDayIndicators([]waveDayHourlyData{crossing}).CrossSea {
		t.Fatalf("90 degrees apart is a cross sea")
	}
}

// Direction is circular, so a swell on north and a wind wave just west of it
// are close together, not 350 degrees apart.
func TestCrossSea_WrapsAtNorth(t *testing.T) {
	hour := waveDayHourlyData{
		WindWaveHeightM: 1.0, WindWaveDirectionDeg: 355, WindWavePeriodS: 5,
		SwellWaveHeightM: 1.0, SwellWaveDirectionDeg: 5, SwellWavePeriodS: 10,
	}
	separation, ok := crossSeaSeparationDeg(hour)
	if !ok || math.Abs(separation-10) > 0.5 {
		t.Fatalf("separation = %.1f (ok=%v), want 10", separation, ok)
	}
}

// Found against ten days of live forecast: the provider will report a 0.02m
// swell alongside a 1.44m wind sea, 75 degrees apart. That is arithmetic, not
// a crossing sea, and flagging it is how an indicator teaches the operator to
// stop reading it. Page 233's reason a secondary system matters is that it
// can put the boat beam-on to the primary one, which a component this small
// cannot do.
func TestCrossSea_IgnoresANegligibleSecondarySystem(t *testing.T) {
	noise := waveDayHourlyData{
		WaveHeightM: 1.44, WavePeriodS: 5,
		WindWaveHeightM: 1.44, WindWaveDirectionDeg: 140, WindWavePeriodS: 5,
		SwellWaveHeightM: 0.02, SwellWaveDirectionDeg: 65, SwellWavePeriodS: 12,
	}
	if waveDayIndicators([]waveDayHourlyData{noise}).CrossSea {
		t.Fatalf("a 0.02m component against 1.44m is not a crossing sea")
	}

	// The same geometry with a secondary system big enough to matter is.
	real := noise
	real.SwellWaveHeightM = 0.6
	if !waveDayIndicators([]waveDayHourlyData{real}).CrossSea {
		t.Fatalf("0.6m against 1.44m, 75 degrees apart, is a crossing sea")
	}
}
