package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func TestHealthCheckIncludesBuildMetadata(t *testing.T) {
	originalVersion := buildVersion
	originalRevision := buildRevision
	t.Cleanup(func() {
		buildVersion = originalVersion
		buildRevision = originalRevision
	})

	buildVersion = "v9.9.9"
	buildRevision = "deadbeef"

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := healthCheck(c); err != nil {
		t.Fatalf("healthCheck returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse health response: %v", err)
	}

	if payload["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", payload["status"])
	}
	if payload["version"] != "v9.9.9" {
		t.Fatalf("expected version v9.9.9, got %q", payload["version"])
	}
	if payload["revision"] != "deadbeef" {
		t.Fatalf("expected revision deadbeef, got %q", payload["revision"])
	}
}

func TestFormatWeatherConditionAt_UsesMostlySunnyDuringDaytime(t *testing.T) {
	loc := time.FixedZone("AEST", 10*3600)
	observedAt := time.Date(2026, time.June, 14, 12, 0, 0, 0, loc)

	condition := formatWeatherConditionAt("MostlyClear", observedAt, loc, false)
	if condition != "Mostly Sunny" {
		t.Fatalf("expected Mostly Sunny, got %q", condition)
	}
}

func TestFormatWeatherConditionAt_UsesMostlyClearAtNight(t *testing.T) {
	loc := time.FixedZone("AEST", 10*3600)
	observedAt := time.Date(2026, time.June, 14, 23, 0, 0, 0, loc)

	condition := formatWeatherConditionAt("MostlyClear", observedAt, loc, false)
	if condition != "Mostly Clear" {
		t.Fatalf("expected Mostly Clear, got %q", condition)
	}
}

func TestFormatWeatherConditionAt_PrefersDaytimeForForecastCards(t *testing.T) {
	condition := formatWeatherConditionAt("MostlyClear", time.Time{}, nil, true)
	if condition != "Mostly Sunny" {
		t.Fatalf("expected Mostly Sunny, got %q", condition)
	}
}

func TestApplyInMemorySolarDefaults_FillsOnlyMissingFields(t *testing.T) {
	solarStats = &solarDayStats{yesterdayKWh: -1, peakTodayW: -1}
	solarPowerHistory = newTelemetryRingBuffer(solarTrendHistoryCapacity)
	t.Cleanup(func() {
		solarStats = &solarDayStats{yesterdayKWh: -1, peakTodayW: -1}
		solarPowerHistory = newTelemetryRingBuffer(solarTrendHistoryCapacity)
	})

	base := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	solarStats.record(500, base)
	solarStats.record(800, base.Add(5*time.Second))
	solarPowerHistory.record(800, base.Add(5*time.Second))

	state := solarStateData{
		Datetime:     base,
		CurrentW:     900,
		TodayKWh:     -1,
		YesterdayKWh: 4.7,
		PeakTodayW:   -1,
		Controllers:  []solarControllerData{},
		Trend24hTotal: []solarTrendPoint{
			{Time: base.Add(-time.Hour), TotalW: 760},
		},
	}

	next := applyInMemorySolarDefaults(state)

	if next.TodayKWh < 0 {
		t.Fatalf("expected today_kwh filled from in-memory accumulator, got %v", next.TodayKWh)
	}
	if next.YesterdayKWh != 4.7 {
		t.Fatalf("expected yesterday_kwh to remain the pre-populated 4.7, got %v", next.YesterdayKWh)
	}
	if next.PeakTodayW != 800 {
		t.Fatalf("expected peak_today_w filled from in-memory accumulator, got %v", next.PeakTodayW)
	}
	if len(next.Trend24hTotal) != 1 || next.Trend24hTotal[0].TotalW != 760 {
		t.Fatalf("expected existing trend to be preserved, got %+v", next.Trend24hTotal)
	}
}

func TestApplyInfluxSolarOverride_ReplacesAllFourFieldsWholesale(t *testing.T) {
	t.Setenv("SETTINGS_FILE", filepath.Join(t.TempDir(), "missing-settings.yaml"))

	baseTime := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)

	// All four fields already populated (e.g. from SignalK/in-memory) to
	// prove this is a real override, not a merge: with Influx unreachable
	// (no settings configured, so newInfluxClient's ok is false),
	// applyInfluxSolarOverride still replaces every field with Influx's own
	// sentinel rather than leaving the pre-existing good values in place.
	state := solarStateData{
		Datetime:     baseTime,
		CurrentW:     900,
		TodayKWh:     5.1,
		YesterdayKWh: 4.7,
		PeakTodayW:   1240,
		Controllers:  []solarControllerData{},
		Trend24hTotal: []solarTrendPoint{
			{Time: baseTime.Add(-time.Hour), TotalW: 760},
		},
	}

	next := applyInfluxSolarOverride(state)

	if next.TodayKWh != -1 {
		t.Fatalf("expected today_kwh overridden to influx sentinel -1, got %v", next.TodayKWh)
	}
	if next.YesterdayKWh != -1 {
		t.Fatalf("expected yesterday_kwh overridden to influx sentinel -1, got %v", next.YesterdayKWh)
	}
	if next.PeakTodayW != -1 {
		t.Fatalf("expected peak_today_w overridden to influx sentinel -1, got %v", next.PeakTodayW)
	}
	if next.Trend24hTotal != nil {
		t.Fatalf("expected trend_24h_total overridden to nil, got %+v", next.Trend24hTotal)
	}
}

func TestSolarStateHandler_BackendFallbackContract(t *testing.T) {
	t.Setenv("SETTINGS_FILE", filepath.Join(t.TempDir(), "missing-settings.yaml"))
	solarStats = &solarDayStats{yesterdayKWh: -1, peakTodayW: -1}
	solarPowerHistory = newTelemetryRingBuffer(solarTrendHistoryCapacity)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/solar-state", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := solarState(c); err != nil {
		t.Fatalf("solarState returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse solar-state response: %v", err)
	}

	if payload["source"] != "backend-fallback" {
		t.Fatalf("expected source backend-fallback, got %v", payload["source"])
	}

	if _, ok := payload["trend_24h_total"].([]any); !ok {
		t.Fatalf("expected trend_24h_total to serialize as array, got %T", payload["trend_24h_total"])
	}

	if _, ok := payload["controllers"].([]any); !ok {
		t.Fatalf("expected controllers to serialize as array, got %T", payload["controllers"])
	}
}

func TestSolarStateHandler_UsesSignalKPayload(t *testing.T) {
	solarStats = &solarDayStats{yesterdayKWh: -1, peakTodayW: -1}
	solarPowerHistory = newTelemetryRingBuffer(solarTrendHistoryCapacity)

	body := []byte(`{
		"timestamp": "2026-07-22T00:00:00Z",
		"electrical": {
			"venus": {
				"totalPanelPower": {"value": 980.2}
			},
			"solar": {
				"0": {
					"panelPower": {"value": 320.5},
					"yieldToday": {"value": 1.2},
					"yieldYesterday": {"value": 1.1},
					"chargingMode": {"value": "bulk"},
					"error": {"value": "none"}
				}
			}
		}
	}`)

	seedSelfTree(t, string(body))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("failed to parse server url: %v", err)
	}
	host, portRaw, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("failed to split host/port: %v", err)
	}
	port, err := strconv.Atoi(portRaw)
	if err != nil {
		t.Fatalf("failed to parse port: %v", err)
	}

	settingsPath := filepath.Join(t.TempDir(), "settings.yaml")
	settings := fmt.Sprintf("signalk:\n  address: %q\n  port: %d\n", host, port)
	if err := os.WriteFile(settingsPath, []byte(settings), 0o600); err != nil {
		t.Fatalf("failed to write settings file: %v", err)
	}

	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/solar-state", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := solarState(c); err != nil {
		t.Fatalf("solarState returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse solar-state response: %v", err)
	}

	if payload["source"] != "signalk" {
		t.Fatalf("expected source signalk, got %v", payload["source"])
	}

	if current, ok := payload["current_w"].(float64); !ok || current < 980 || current > 981 {
		t.Fatalf("expected current_w around 980.2, got %v", payload["current_w"])
	}

	controllers, ok := payload["controllers"].([]any)
	if !ok || len(controllers) != 1 {
		t.Fatalf("expected one controller entry, got %T len=%d", payload["controllers"], len(controllers))
	}
}

// TestVesselStateHandler_MaxGustKtsCoversFullLadderAndClampsMonotonically
// asserts the vessel-state API's max_gust_kts field is a single keyed object
// covering the full gustWindowLadder ("10m","30m","1h","24h") - replacing
// the old max_gust_10m_kts/max_gust_1h_kts fields outright - and that,
// mirroring the pre-existing 10m/1h clamp, every window's value is >= 0 and
// non-decreasing walking the ladder in order (a longer window's max gust can
// never be less than a shorter window's). Only a 10m-window sample is
// seeded, so this also proves the longer windows still surface a real
// clamped value rather than falling through to a raw sentinel.
func TestVesselStateHandler_MaxGustKtsCoversFullLadderAndClampsMonotonically(t *testing.T) {
	windGustHistory = newTelemetryRingBuffer(windGustHistoryCapacity)
	t.Cleanup(func() {
		windGustHistory = newTelemetryRingBuffer(windGustHistoryCapacity)
	})

	now := time.Now().UTC()
	windGustHistory.record(22.3, now.Add(-2*time.Minute))

	body := []byte(`{"name": "Test Vessel", "navigation": {"state": {"value": "sailing"}}}`)

	seedSelfTree(t, string(body))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("failed to parse server url: %v", err)
	}
	host, portRaw, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("failed to split host/port: %v", err)
	}
	port, err := strconv.Atoi(portRaw)
	if err != nil {
		t.Fatalf("failed to parse port: %v", err)
	}

	settingsPath := filepath.Join(t.TempDir(), "settings.yaml")
	settings := fmt.Sprintf("signalk:\n  address: %q\n  port: %d\n", host, port)
	if err := os.WriteFile(settingsPath, []byte(settings), 0o600); err != nil {
		t.Fatalf("failed to write settings file: %v", err)
	}
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/vessel-state", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := vesselState(c); err != nil {
		t.Fatalf("vesselState returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse vessel-state response: %v", err)
	}

	if _, exists := payload["max_gust_10m_kts"]; exists {
		t.Fatalf("expected max_gust_10m_kts to be removed, but it was present")
	}
	if _, exists := payload["max_gust_1h_kts"]; exists {
		t.Fatalf("expected max_gust_1h_kts to be removed, but it was present")
	}

	maxGustKts, ok := payload["max_gust_kts"].(map[string]any)
	if !ok {
		t.Fatalf("expected max_gust_kts to serialize as an object, got %T: %+v", payload["max_gust_kts"], payload["max_gust_kts"])
	}

	ladder := []string{"10m", "30m", "1h", "24h"}
	var previous float64
	for i, window := range ladder {
		raw, exists := maxGustKts[window]
		if !exists {
			t.Fatalf("expected max_gust_kts to contain window %q, got %+v", window, maxGustKts)
		}
		value, ok := raw.(float64)
		if !ok {
			t.Fatalf("expected max_gust_kts[%q] to be a number, got %T", window, raw)
		}
		if value < 0 {
			t.Fatalf("expected max_gust_kts[%q] to be clamped to >= 0, got %v", window, value)
		}
		if i > 0 && value < previous {
			t.Fatalf("expected max_gust_kts to be non-decreasing walking the ladder in order; window %q (%v) is less than the previous window's %v", window, value, previous)
		}
		previous = value
	}
}

// TestComputeMaxGustKtsFor_SkipsInMemoryWhenInfluxConfigured proves the
// in-memory branch is not consulted when Influx is configured — not just
// that its result is discarded, but that inMemoryMaxWindGustKts's
// windGustHistory.mu.RLock() is never attempted. We prove this by holding
// windGustHistory.mu locked for writing before calling computeMaxGustKtsFor:
// if the in-memory path were still invoked, it would block forever trying to
// RLock, and computeMaxGustKtsFor would never return.
func TestComputeMaxGustKtsFor_SkipsInMemoryWhenInfluxConfigured(t *testing.T) {
	path := writeInfluxSettingsFixture(t, "influxdb:\n  enabled: true\n  url: http://127.0.0.1:1\n  org: myorg\n  bucket: mybucket\n")
	t.Setenv("SETTINGS_FILE", path)
	t.Setenv("INFLUXDB_TOKEN", "sometoken")

	windGustHistory = newTelemetryRingBuffer(windGustHistoryCapacity)
	windGustHistory.mu.Lock()
	defer windGustHistory.mu.Unlock()

	done := make(chan map[string]float64, 1)
	go func() {
		done <- computeMaxGustKtsFor(gustWindowLadder)
	}()

	select {
	case <-done:
		// Returned without ever needing windGustHistory's lock — in-memory
		// path was correctly skipped.
	case <-time.After(3 * time.Second):
		t.Fatal("computeMaxGustKtsFor blocked, implying it tried to RLock windGustHistory (the in-memory path) even though Influx is configured")
	}
}
