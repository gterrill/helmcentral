package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

// TestVesselStateHandler_MaxGustTrueKtsCoversFullLadderAndClampsMonotonically
// (ADR 0130) is TestVesselStateHandler_MaxGustKtsCoversFullLadderAndClampsMonotonically's
// true-wind counterpart: max_gust_true_kts is its own keyed object, walking
// the same gustWindowLadder with the same non-decreasing clamp, sourced from
// trueWindGustHistory rather than windGustHistory.
func TestVesselStateHandler_MaxGustTrueKtsCoversFullLadderAndClampsMonotonically(t *testing.T) {
	original := trueWindGustHistory
	t.Cleanup(func() { trueWindGustHistory = original })
	trueWindGustHistory = newTelemetryRingBuffer(windGustHistoryCapacity)

	now := time.Now().UTC()
	trueWindGustHistory.record(14.1, now.Add(-2*time.Minute))

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

	maxGustTrueKts, ok := payload["max_gust_true_kts"].(map[string]any)
	if !ok {
		t.Fatalf("expected max_gust_true_kts to serialize as an object, got %T: %+v", payload["max_gust_true_kts"], payload["max_gust_true_kts"])
	}

	ladder := []string{"10m", "30m", "1h", "24h"}
	var previous float64
	for i, window := range ladder {
		raw, exists := maxGustTrueKts[window]
		if !exists {
			t.Fatalf("expected max_gust_true_kts to contain window %q, got %+v", window, maxGustTrueKts)
		}
		value, ok := raw.(float64)
		if !ok {
			t.Fatalf("expected max_gust_true_kts[%q] to be a number, got %T", window, raw)
		}
		if value < 0 {
			t.Fatalf("expected max_gust_true_kts[%q] to be clamped to >= 0, got %v", window, value)
		}
		if i > 0 && value < previous {
			t.Fatalf("expected max_gust_true_kts to be non-decreasing walking the ladder in order; window %q (%v) is less than the previous window's %v", window, value, previous)
		}
		previous = value
	}
}

// vesselStatePayloadFor seeds the self tree/a stub SignalK server with body
// and returns the parsed /api/vessel-state JSON, factoring out the
// httptest-server/settings-file wiring the true-wind gust-clamp tests below
// share with TestVesselStateHandler_MaxGustTrueKtsCoversFullLadderAndClampsMonotonically
// above (which predates this helper and still wires it out longhand).
func vesselStatePayloadFor(t *testing.T, body []byte) map[string]any {
	t.Helper()

	seedSelfTree(t, string(body))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	t.Setenv("SETTINGS_FILE", settingsFileForServer(t, srv.URL))

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
	return payload
}

// TestVesselStateHandler_MaxGustTrueKtsStaysUnknownWhenNoSamplesRecorded
// (code-review fix, 2026-09-25) proves a window with no true-wind gust
// samples reports the -1 "unknown" sentinel, not 0. The apparent ladder's
// clamp deliberately turns "no data" into "0 calm" (see the comment above
// TestVesselStateHandler_MaxGustKtsCoversFullLadderAndClampsMonotonically),
// but that is wrong for true wind: a boat with no true-wind source, or one
// that has just restarted with an empty ring buffer, must never be shown as
// dead calm.
func TestVesselStateHandler_MaxGustTrueKtsStaysUnknownWhenNoSamplesRecorded(t *testing.T) {
	original := trueWindGustHistory
	t.Cleanup(func() { trueWindGustHistory = original })
	trueWindGustHistory = newTelemetryRingBuffer(windGustHistoryCapacity)

	body := []byte(`{"name": "Test Vessel", "navigation": {"state": {"value": "sailing"}}}`)
	payload := vesselStatePayloadFor(t, body)

	maxGustTrueKts, ok := payload["max_gust_true_kts"].(map[string]any)
	if !ok {
		t.Fatalf("expected max_gust_true_kts to serialize as an object, got %T: %+v", payload["max_gust_true_kts"], payload["max_gust_true_kts"])
	}

	for _, window := range []string{"10m", "30m", "1h", "24h"} {
		value, ok := maxGustTrueKts[window].(float64)
		if !ok {
			t.Fatalf("expected max_gust_true_kts[%q] to be a number, got %T", window, maxGustTrueKts[window])
		}
		if value != -1 {
			t.Fatalf("expected max_gust_true_kts[%q] to stay at the -1 unknown sentinel with no samples recorded, got %v (a boat with no true-wind source must not read as dead calm)", window, value)
		}
	}
}

// TestVesselStateHandler_MaxGustTrueKtsClampsOnlyAfterAShorterWindowHasData
// proves the monotonic (longer >= shorter) clamp only applies once a
// shorter window has actually produced a real (>=0) value: a sample old
// enough to fall outside the 10m/30m windows but still inside 1h/24h must
// leave the shorter windows at -1 (unknown), not clamp them up to 0 or down
// to the longer windows' real reading.
func TestVesselStateHandler_MaxGustTrueKtsClampsOnlyAfterAShorterWindowHasData(t *testing.T) {
	original := trueWindGustHistory
	t.Cleanup(func() { trueWindGustHistory = original })
	trueWindGustHistory = newTelemetryRingBuffer(windGustHistoryCapacity)

	now := time.Now().UTC()
	// 45 minutes old: inside the 1h/24h windows, outside 10m/30m.
	trueWindGustHistory.record(9.4, now.Add(-45*time.Minute))

	body := []byte(`{"name": "Test Vessel", "navigation": {"state": {"value": "sailing"}}}`)
	payload := vesselStatePayloadFor(t, body)

	maxGustTrueKts, ok := payload["max_gust_true_kts"].(map[string]any)
	if !ok {
		t.Fatalf("expected max_gust_true_kts to serialize as an object, got %T: %+v", payload["max_gust_true_kts"], payload["max_gust_true_kts"])
	}

	tenMin, ok := maxGustTrueKts["10m"].(float64)
	if !ok || tenMin != -1 {
		t.Fatalf("expected max_gust_true_kts[10m] to stay unknown (-1) with no sample inside 10m, got %v (ok=%v)", maxGustTrueKts["10m"], ok)
	}
	thirtyMin, ok := maxGustTrueKts["30m"].(float64)
	if !ok || thirtyMin != -1 {
		t.Fatalf("expected max_gust_true_kts[30m] to stay unknown (-1) with no sample inside 30m, got %v (ok=%v)", maxGustTrueKts["30m"], ok)
	}
	oneHour, ok := maxGustTrueKts["1h"].(float64)
	if !ok || oneHour < 0 {
		t.Fatalf("expected max_gust_true_kts[1h] to report the real recorded value, got %v (ok=%v)", maxGustTrueKts["1h"], ok)
	}
	if !approxEqual(oneHour, 9.4, 0.01) {
		t.Fatalf("expected max_gust_true_kts[1h] to be 9.4, got %v", oneHour)
	}
	twentyFourHour, ok := maxGustTrueKts["24h"].(float64)
	if !ok || twentyFourHour < oneHour {
		t.Fatalf("expected max_gust_true_kts[24h] (%v) to be >= 1h's value (%v)", maxGustTrueKts["24h"], oneHour)
	}
}

// TestBuildVesselStatePayload_NoSeparateMaxTrueWindKts1hField is the direct
// regression test for a code-review finding (2026-09-25): the payload used
// to carry a max_true_wind_kts_1h field - the Current Conditions tile's
// "obs" marker (ADR 0129) - computed from trueWindSpeedHistory (raw m/s,
// gated by the heavy-weather trend's own staleness check), a SEPARATE
// buffer from the one max_gust_true_kts["1h"] (the Wind tile's True mode,
// ADR 0130) already reads, trueWindGustHistory (already-knots, gated on
// state.WindSpeedTrueKts >= 0). Two buffers behind two different freshness
// gates holding "the same" quantity meant the two tiles could disagree.
// This test seeds the two buffers with DIFFERENT values specifically so a
// reintroduced max_true_wind_kts_1h field would show a value that disagrees
// with max_gust_true_kts["1h"] - the exact bug this fix removes. See
// docs/adr/0130's amendment.
func TestBuildVesselStatePayload_NoSeparateMaxTrueWindKts1hField(t *testing.T) {
	originalSpeed, originalGust := trueWindSpeedHistory, trueWindGustHistory
	t.Cleanup(func() { trueWindSpeedHistory, trueWindGustHistory = originalSpeed, originalGust })
	trueWindSpeedHistory = newTelemetryRingBuffer(windGustHistoryCapacity)
	trueWindGustHistory = newTelemetryRingBuffer(windGustHistoryCapacity)

	now := time.Now().UTC()
	// Deliberately different quantities: 20.0 m/s (~38.9 kts) into the
	// buffer the removed field used to read, 9.4 kts into the one
	// max_gust_true_kts actually uses.
	trueWindSpeedHistory.record(20.0, now.Add(-30*time.Minute))
	trueWindGustHistory.record(9.4, now.Add(-30*time.Minute))

	body := []byte(`{"name": "Test Vessel", "navigation": {"state": {"value": "sailing"}}}`)
	payload := vesselStatePayloadFor(t, body)

	if _, present := payload["max_true_wind_kts_1h"]; present {
		t.Fatalf("expected max_true_wind_kts_1h to be gone from the payload entirely, got %v", payload["max_true_wind_kts_1h"])
	}

	maxGustTrueKts, ok := payload["max_gust_true_kts"].(map[string]any)
	if !ok {
		t.Fatalf("expected max_gust_true_kts to serialize as an object, got %T", payload["max_gust_true_kts"])
	}
	oneHour, ok := maxGustTrueKts["1h"].(float64)
	if !ok {
		t.Fatalf("expected max_gust_true_kts[1h] to be a number, got %T", maxGustTrueKts["1h"])
	}
	if !approxEqual(oneHour, 9.4, 0.01) {
		t.Fatalf("expected max_gust_true_kts[1h]=9.4 (from trueWindGustHistory) - got %v, which would mean something is still reading trueWindSpeedHistory (~38.9)", oneHour)
	}
}

// TestVesselStateHandler_LengthOverallMPresentSerializesAsNumber proves the
// /api/vessel-state body carries the SignalK-published LOA (ADR 0047's
// "Swing radius refuses to compute when LOA is unset" now resolves this from
// SignalK too, not just settings.yaml).
func TestVesselStateHandler_LengthOverallMPresentSerializesAsNumber(t *testing.T) {
	body := []byte(`{"name": "Test Vessel", "design": {"length": {"value": {"overall": 17.9}}}}`)

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

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse vessel-state response: %v", err)
	}

	got, ok := payload["length_overall_m"].(float64)
	if !ok || got != 17.9 {
		t.Fatalf("expected length_overall_m 17.9, got %v (%T)", payload["length_overall_m"], payload["length_overall_m"])
	}
}

// TestVesselStateHandler_LengthOverallMAbsentSerializesAsNullNotSentinel is
// the no-masking-fallback half: an unpublished LOA must reach the frontend
// as JSON null, never as the internal lookupNumber -1 sentinel leaking out
// and being mistaken for a real (negative) length.
func TestVesselStateHandler_LengthOverallMAbsentSerializesAsNullNotSentinel(t *testing.T) {
	body := []byte(`{"name": "Test Vessel"}`)

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

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse vessel-state response: %v", err)
	}

	v, exists := payload["length_overall_m"]
	if !exists {
		t.Fatalf("expected length_overall_m key to be present in the response")
	}
	if v != nil {
		t.Fatalf("expected length_overall_m to be null when unpublished, got %v (%T)", v, v)
	}
}

// TestVesselStateHandler_DraftMPresentSerializesAsNumber proves the
// /api/vessel-state body carries the SignalK-published draft (ADR 0135's
// low-water clearance warning needs it), the same way length_overall_m
// already does.
func TestVesselStateHandler_DraftMPresentSerializesAsNumber(t *testing.T) {
	body := []byte(`{"name": "Test Vessel", "design": {"draft": {"value": {"maximum": 1.2}}}}`)

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

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse vessel-state response: %v", err)
	}

	got, ok := payload["draft_m"].(float64)
	if !ok || got != 1.2 {
		t.Fatalf("expected draft_m 1.2, got %v (%T)", payload["draft_m"], payload["draft_m"])
	}
}

// TestVesselStateHandler_DraftMAbsentSerializesAsNullNotSentinel is the
// no-masking-fallback half: an unpublished draft must reach the frontend as
// JSON null, never as the internal lookupNumber -1 sentinel leaking out and
// being mistaken for a real (negative) draft.
func TestVesselStateHandler_DraftMAbsentSerializesAsNullNotSentinel(t *testing.T) {
	body := []byte(`{"name": "Test Vessel"}`)

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

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse vessel-state response: %v", err)
	}

	v, exists := payload["draft_m"]
	if !exists {
		t.Fatalf("expected draft_m key to be present in the response")
	}
	if v != nil {
		t.Fatalf("expected draft_m to be null when unpublished, got %v (%T)", v, v)
	}
}

// TestBuildVesselStatePayload_TimezoneReflectsVesselLocalZone proves the
// vessel-state payload carries the vessel's local zone found from its
// position (ADR 0230), not the browser's. The wall display's Ubuntu box is
// stuck on UTC, so the frontend clock needs this field to render the
// vessel's actual local time rather than the browser's.
func TestBuildVesselStatePayload_TimezoneReflectsVesselLocalZone(t *testing.T) {
	useFreshBoatZone(t)
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	server := trustedSignalKPayloadServer(t, -21.1113, 148.9) // UTC+10
	defer server.Close()
	host, port := hostPort(t, server.URL)
	t.Setenv("SETTINGS_FILE", writeTestSettings(t, host, port))

	payload := buildVesselStatePayload()

	if got := payload["timezone"]; got != "Australia/Brisbane" {
		t.Fatalf("timezone: got %v, want %q", got, "Australia/Brisbane")
	}
	if got := payload["timezone_source"]; got != "fix" {
		t.Fatalf("timezone_source: got %v, want fix", got)
	}
}

// boat_date is the boat's calendar day on the event's own datetime, so a
// browser that cannot format the zone still learns when the day turns.
func TestBuildVesselStatePayload_BoatDateIsTheDayInTheBoatsZone(t *testing.T) {
	useFreshBoatZone(t)
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	server := trustedSignalKPayloadServer(t, -21.1113, 148.9) // UTC+10
	defer server.Close()
	host, port := hostPort(t, server.URL)
	t.Setenv("SETTINGS_FILE", writeTestSettings(t, host, port))

	payload := buildVesselStatePayload()

	datetime, err := time.Parse(time.RFC3339, payload["datetime"].(string))
	if err != nil {
		t.Fatal(err)
	}
	want := datetime.In(time.FixedZone("AEST", 10*3600)).Format("2006-01-02")
	if got := payload["boat_date"]; got != want {
		t.Fatalf("boat_date: got %v, want %q", got, want)
	}
}

// Before any fix the zone is a UTC stand-in, which is no boat day.
func TestBuildVesselStatePayload_BoatDateIsNullBeforeAnyFix(t *testing.T) {
	useFreshBoatZone(t)
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	server := trustedSignalKPayloadServer(t, -1, -1)
	defer server.Close()
	host, port := hostPort(t, server.URL)
	t.Setenv("SETTINGS_FILE", writeTestSettings(t, host, port))

	payload := buildVesselStatePayload()

	if got, present := payload["boat_date"]; !present || got != nil {
		t.Fatalf("boat_date: got %v (present %v), want null", got, present)
	}
}

// TestBuildVesselStatePayload_TimezoneFallsBackToUTCForSentinelPosition is
// the no-masking-fallback half: when the position resolves to the -1,-1
// sentinel, the payload must report the honest UTC fallback rather than
// inventing a zone from a bogus longitude.
func TestBuildVesselStatePayload_TimezoneIsUTCAndSaysSoBeforeAnyFix(t *testing.T) {
	useFreshBoatZone(t)
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	server := trustedSignalKPayloadServer(t, -1, -1)
	defer server.Close()
	host, port := hostPort(t, server.URL)
	t.Setenv("SETTINGS_FILE", writeTestSettings(t, host, port))

	payload := buildVesselStatePayload()

	if got := payload["timezone"]; got != "UTC" {
		t.Fatalf("timezone: got %v, want %q", got, "UTC")
	}
	if got := payload["timezone_source"]; got != "none" {
		t.Fatalf("timezone_source: got %v, want none", got)
	}
}

// With a fix remembered, losing it keeps the boat's zone and says so.
func TestBuildVesselStatePayload_TimezoneStaysOnTheLastFixWhenTheFixIsLost(t *testing.T) {
	useFreshBoatZone(t)
	boatTimeAt(-21.1113, 148.9, true)
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	server := trustedSignalKPayloadServer(t, -1, -1)
	defer server.Close()
	host, port := hostPort(t, server.URL)
	t.Setenv("SETTINGS_FILE", writeTestSettings(t, host, port))

	payload := buildVesselStatePayload()
	if payload["timezone"] != "Australia/Brisbane" || payload["timezone_source"] != "last_fix" {
		t.Fatalf("timezone %v source %v", payload["timezone"], payload["timezone_source"])
	}
	if payload["boat_date"] == nil {
		t.Fatal("boat_date: want the day in the last fix's zone, got null")
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
	withSeededSecretsStore(t, map[string]string{"INFLUXDB_TOKEN": "sometoken"})

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

// ── auth.mode startup validation (docs/adr/0040) ─────────────────────────────
//
// checkAuthModeAtStartup is the pure, error-returning function main() wraps
// with log.Fatalf — following this codebase's established pattern for every
// other fail-fast startup check (secrets store, tile cache, ...), none of
// which are tested by actually exec'ing a subprocess. Testing the
// error-returning function directly is what "exits rather than booting"
// means here: main() has exactly one line translating a non-nil error from
// this function into a fatal exit, and that translation is not itself
// meaningfully testable in-process.

func writeAuthStartupSettingsFixture(t *testing.T, signalkURL, mode string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	content := "signalk:\n  address: " + signalkURL + "\n  port: 3000\n"
	if mode != "" {
		content += "auth:\n  mode: " + mode + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("could not write temp settings: %v", err)
	}
	return path
}

func TestCheckAuthModeAtStartup_SignalKModeAgainstSecurityOffStubFailsFast(t *testing.T) {
	srv, rs := newRecordingServer(t)
	defer srv.Close()
	rs.on(http.MethodPost, "/signalk/v1/auth/login", http.StatusNotFound, "Cannot POST /signalk/v1/auth/login")

	settingsPath := writeAuthStartupSettingsFixture(t, srv.URL, "signalk")

	_, err := checkAuthModeAtStartup(settingsPath)
	if err == nil {
		t.Fatal("expected checkAuthModeAtStartup to fail fast when auth.mode is signalk but SignalK security is off")
	}
	if !strings.Contains(err.Error(), "security is disabled") {
		t.Fatalf("expected the error to name the unsatisfiable combination, got: %v", err)
	}
}

func TestCheckAuthModeAtStartup_SignalKModeAgainstSecurityOnStubSucceeds(t *testing.T) {
	srv, rs := newRecordingServer(t)
	defer srv.Close()
	rs.on(http.MethodPost, "/signalk/v1/auth/login", http.StatusUnauthorized, `{"message":"invalid username or password"}`)

	settingsPath := writeAuthStartupSettingsFixture(t, srv.URL, "signalk")

	mode, err := checkAuthModeAtStartup(settingsPath)
	if err != nil {
		t.Fatalf("expected startup to succeed when SignalK security is on, got: %v", err)
	}
	if mode != authModeSignalK {
		t.Fatalf("expected mode %q, got %q", authModeSignalK, mode)
	}
}

// TestCheckAuthModeAtStartup_ModeNoneNeverProbesSignalK proves mode:none
// takes no dependency on SignalK being reachable at all — a boat configured
// with no SignalK address yet, or one that's powered off, must still boot.
func TestCheckAuthModeAtStartup_ModeNoneNeverProbesSignalK(t *testing.T) {
	settingsPath := writeAuthStartupSettingsFixture(t, "http://127.0.0.1:1", "none")

	mode, err := checkAuthModeAtStartup(settingsPath)
	if err != nil {
		t.Fatalf("expected mode:none to boot without ever reaching SignalK, got: %v", err)
	}
	if mode != authModeNone {
		t.Fatalf("expected mode %q, got %q", authModeNone, mode)
	}
}

func TestCheckAuthModeAtStartup_UnrecognisedModeFailsFast(t *testing.T) {
	settingsPath := writeAuthStartupSettingsFixture(t, "http://localhost:3000", "yolo")

	_, err := checkAuthModeAtStartup(settingsPath)
	if err == nil {
		t.Fatal("expected an unrecognised auth.mode value to fail fast rather than guess")
	}
	if !strings.Contains(err.Error(), "yolo") {
		t.Fatalf("expected the error to name the bad value, got: %v", err)
	}
}

// ── SignalK security probe ───────────────────────────────────────────────────

func TestProbeSignalKSecurityEnabled_404MeansDisabled(t *testing.T) {
	srv, rs := newRecordingServer(t)
	defer srv.Close()
	rs.on(http.MethodPost, "/signalk/v1/auth/login", http.StatusNotFound, "not found")

	enabled, err := probeSignalKSecurityEnabled(srv.URL)
	if err != nil {
		t.Fatalf("probeSignalKSecurityEnabled: %v", err)
	}
	if enabled {
		t.Fatal("expected a 404 from the login route to mean security is disabled")
	}
}

func TestProbeSignalKSecurityEnabled_NonNotFoundMeansEnabled(t *testing.T) {
	srv, rs := newRecordingServer(t)
	defer srv.Close()
	rs.on(http.MethodPost, "/signalk/v1/auth/login", http.StatusUnauthorized, `{"message":"bad creds"}`)

	enabled, err := probeSignalKSecurityEnabled(srv.URL)
	if err != nil {
		t.Fatalf("probeSignalKSecurityEnabled: %v", err)
	}
	if !enabled {
		t.Fatal("expected a non-404 response from the login route to mean security is enabled")
	}
}

func TestProbeSignalKSecurityEnabled_UnreachableSurfacesErrorNotSwallowed(t *testing.T) {
	unreachable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := unreachable.URL
	unreachable.Close()

	_, err := probeSignalKSecurityEnabled(url)
	if err == nil {
		t.Fatal("expected an unreachable SignalK to surface an explicit error rather than a guessed answer")
	}
	var unreachableErr *signalKUnreachableError
	if !errors.As(err, &unreachableErr) {
		t.Fatalf("expected a closed port to be a retryable signalKUnreachableError, got: %v", err)
	}
}

// ── SignalK startup wait ─────────────────────────────────────────────────────
//
// After a power cut SignalK is often still starting when Helmcentral comes up.
// An unreachable SignalK is waited on; any answer is judged at once.

func TestCheckAuthModeAtStartup_WaitsForUnreachableSignalKThenSucceeds(t *testing.T) {
	settingsPath := writeAuthStartupSettingsFixture(t, "http://signalk.test", "signalk")
	calls := 0
	probe := func(string) (bool, error) {
		calls++
		if calls < 3 {
			return false, &signalKUnreachableError{err: errors.New("connection refused")}
		}
		return true, nil
	}

	mode, err := checkAuthModeAtStartupWith(settingsPath, probe, time.Millisecond)
	if err != nil {
		t.Fatalf("expected startup to succeed once SignalK answered, got: %v", err)
	}
	if mode != authModeSignalK {
		t.Fatalf("expected mode %q, got %q", authModeSignalK, mode)
	}
	if calls != 3 {
		t.Fatalf("expected 3 probes (2 unreachable, 1 answer), got %d", calls)
	}
}

func TestCheckAuthModeAtStartup_SecurityOffAfterWaitingStillFailsFast(t *testing.T) {
	settingsPath := writeAuthStartupSettingsFixture(t, "http://signalk.test", "signalk")
	calls := 0
	probe := func(string) (bool, error) {
		calls++
		if calls == 1 {
			return false, &signalKUnreachableError{err: errors.New("connection refused")}
		}
		return false, nil
	}

	_, err := checkAuthModeAtStartupWith(settingsPath, probe, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "security is disabled") {
		t.Fatalf("expected a security-disabled error, got: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected no retry after SignalK answered, got %d probes", calls)
	}
}

func TestCheckAuthModeAtStartup_SecurityOffFirstProbeDoesNotRetry(t *testing.T) {
	settingsPath := writeAuthStartupSettingsFixture(t, "http://signalk.test", "signalk")
	calls := 0
	probe := func(string) (bool, error) {
		calls++
		return false, nil
	}

	if _, err := checkAuthModeAtStartupWith(settingsPath, probe, time.Millisecond); err == nil {
		t.Fatal("expected security off to refuse to start")
	}
	if calls != 1 {
		t.Fatalf("expected exactly 1 probe, got %d", calls)
	}
}

func TestCheckAuthModeAtStartup_NonTransportErrorDoesNotRetry(t *testing.T) {
	settingsPath := writeAuthStartupSettingsFixture(t, "http://signalk.test", "signalk")
	calls := 0
	probe := func(string) (bool, error) {
		calls++
		return false, errors.New("unusable answer")
	}

	_, err := checkAuthModeAtStartupWith(settingsPath, probe, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "unusable answer") {
		t.Fatalf("expected the probe error to surface, got: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected exactly 1 probe, got %d", calls)
	}
}

// Any failure where SignalK gives no HTTP answer at all is waited on, whatever
// the cause, and the underlying error stays in the text for the log.
func TestProbeSignalKSecurityEnabled_AnyNoAnswerIsUnreachable(t *testing.T) {
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer tlsSrv.Close()

	cases := map[string]string{
		"unsupported scheme": "ftp://127.0.0.1:1",
		"unparseable URL":    "http://127.0.0.1:bad",
		"untrusted TLS cert": tlsSrv.URL,
	}
	for name, address := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := probeSignalKSecurityEnabled(address)
			if err == nil {
				t.Fatal("expected an error")
			}
			var unreachable *signalKUnreachableError
			if !errors.As(err, &unreachable) {
				t.Fatalf("a no-answer failure must be waited on, got: %v", err)
			}
			if !strings.Contains(err.Error(), address) {
				t.Fatalf("expected the error to name %q, got: %v", address, err)
			}
		})
	}
}

// The settings are read again on every try, so a fix made while Helmcentral
// waits takes effect without a restart.
func TestCheckAuthModeAtStartup_RereadsSignalKAddressWhileWaiting(t *testing.T) {
	settingsPath := writeAuthStartupSettingsFixture(t, "http://wrong.test", "signalk")
	var seen []string
	probe := func(u string) (bool, error) {
		seen = append(seen, u)
		if len(seen) == 1 {
			content := "signalk:\n  address: http://corrected.example.test\n  port: 3000\nauth:\n  mode: signalk\n"
			if err := os.WriteFile(settingsPath, []byte(content), 0o644); err != nil {
				t.Fatalf("rewrite settings: %v", err)
			}
			return false, &signalKUnreachableError{err: errors.New("connection refused")}
		}
		return true, nil
	}

	if _, err := checkAuthModeAtStartupWith(settingsPath, probe, time.Millisecond); err != nil {
		t.Fatalf("expected startup to succeed, got: %v", err)
	}
	want := []string{"http://wrong.test", "http://corrected.example.test"}
	if len(seen) != 2 || seen[0] != want[0] || seen[1] != want[1] {
		t.Fatalf("expected probes at %v, got %v", want, seen)
	}
}

func TestCheckAuthModeAtStartup_StopsWaitingWhenModeChangesAwayFromSignalK(t *testing.T) {
	settingsPath := writeAuthStartupSettingsFixture(t, "http://signalk.test", "signalk")
	calls := 0
	probe := func(string) (bool, error) {
		calls++
		content := "signalk:\n  address: http://signalk.test\n  port: 3000\nauth:\n  mode: none\n"
		if err := os.WriteFile(settingsPath, []byte(content), 0o644); err != nil {
			t.Fatalf("rewrite settings: %v", err)
		}
		return false, &signalKUnreachableError{err: errors.New("connection refused")}
	}

	mode, err := checkAuthModeAtStartupWith(settingsPath, probe, time.Millisecond)
	if err != nil {
		t.Fatalf("expected startup to carry on in mode none, got: %v", err)
	}
	if mode != authModeNone {
		t.Fatalf("expected mode %q, got %q", authModeNone, mode)
	}
	if calls != 1 {
		t.Fatalf("expected no probe after the mode changed, got %d", calls)
	}
}

// A settings file that cannot be read has no auth.mode to know, so startup does
// what it always did: treat the mode as none and carry on. Only signalk mode
// has a security check to refuse on.
func TestCheckAuthModeAtStartup_UnreadableSettingsIsModeNoneAsBefore(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(settingsPath, []byte("auth: [unclosed\n"), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	calls := 0
	probe := func(string) (bool, error) {
		calls++
		return true, nil
	}

	mode, err := checkAuthModeAtStartupWith(settingsPath, probe, time.Millisecond)
	if err != nil {
		t.Fatalf("expected the unreadable file to behave as on main (mode none), got: %v", err)
	}
	if mode != authModeNone {
		t.Fatalf("expected mode %q, got %q", authModeNone, mode)
	}
	if calls != 0 {
		t.Fatalf("expected no probe, got %d", calls)
	}
	if got := resolveAuthMode(settingsPath); got != authModeNone {
		t.Fatalf("resolveAuthMode on an unreadable file = %q, want %q", got, authModeNone)
	}
}

// Once startup has seen mode signalk, a settings file that stops being usable
// while it waits (unreadable, empty or missing) is one more failed try: startup
// keeps waiting, probes no guessed address, logs the settings problem, and
// carries on in signalk mode once the file is good again. It neither refuses
// nor falls back to mode none.
func TestCheckAuthModeAtStartup_SettingsUnusableWhileWaitingKeepsWaiting(t *testing.T) {
	cases := []struct {
		name    string
		damage  func(t *testing.T, path string)
		logWord string
	}{
		{"unreadable", func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("auth: [unclosed\n"), 0o644); err != nil {
				t.Fatalf("damage settings: %v", err)
			}
		}, "settings.yaml"},
		{"empty", func(t *testing.T, path string) {
			if err := os.WriteFile(path, nil, 0o644); err != nil {
				t.Fatalf("damage settings: %v", err)
			}
		}, "settings.yaml"},
		{"missing", func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatalf("damage settings: %v", err)
			}
		}, "settings.yaml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			settingsPath := writeAuthStartupSettingsFixture(t, "http://signalk.test", "signalk")
			good, err := os.ReadFile(settingsPath)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			logs := captureLog(t)
			probes := 0
			var seen []string
			probe := func(u string) (bool, error) {
				probes++
				seen = append(seen, u)
				if probes == 1 {
					tc.damage(t, settingsPath)
					return false, &signalKUnreachableError{err: errors.New("connection refused")}
				}
				return true, nil
			}
			sleeps := 0
			clock := &fakeStartupClock{t: time.Unix(0, 0)}
			w := startupWait{
				// Each sleep moves the fake clock a full summary interval, so
				// every failed try writes a log line.
				retry: time.Second, summary: time.Minute, now: clock.now,
				sleep: func(time.Duration) {
					clock.sleep(time.Minute)
					sleeps++
					if sleeps == 3 { // two tries read the damaged file, then it is restored
						if err := os.WriteFile(settingsPath, good, 0o644); err != nil {
							t.Fatalf("restore settings: %v", err)
						}
					}
				},
			}

			mode, err := checkAuthModeAtStartupWaiting(settingsPath, probe, w)
			if err != nil {
				t.Fatalf("expected the wait to survive a damaged settings file, got: %v", err)
			}
			if mode != authModeSignalK {
				t.Fatalf("expected mode %q, got %q", authModeSignalK, mode)
			}
			if probes != 2 {
				t.Fatalf("expected no probe while the file was unusable, got %d probes: %v", probes, seen)
			}
			if sleeps < 3 {
				t.Fatalf("expected the wait to sleep through the damaged reads, got %d sleeps", sleeps)
			}
			if !strings.Contains(logs.String(), tc.logWord) || !strings.Contains(logs.String(), "settings") {
				t.Fatalf("expected the log to name the settings problem, got:\n%s", logs.String())
			}
		})
	}
}

// A file that reads fine, has content and names another mode still ends the
// wait in that mode.
func TestCheckAuthModeAtStartup_ContentfulOtherModeEndsWaitAfterSettingsProblem(t *testing.T) {
	settingsPath := writeAuthStartupSettingsFixture(t, "http://signalk.test", "signalk")
	probe := func(string) (bool, error) {
		if err := os.WriteFile(settingsPath, nil, 0o644); err != nil {
			t.Fatalf("empty settings: %v", err)
		}
		return false, &signalKUnreachableError{err: errors.New("connection refused")}
	}
	sleeps := 0
	w := startupWait{
		retry: time.Second, summary: time.Minute, now: time.Now,
		sleep: func(time.Duration) {
			sleeps++
			if sleeps == 2 {
				if err := os.WriteFile(settingsPath, []byte("auth:\n  mode: none\n"), 0o644); err != nil {
					t.Fatalf("write settings: %v", err)
				}
			}
		},
	}
	mode, err := checkAuthModeAtStartupWaiting(settingsPath, probe, w)
	if err != nil || mode != authModeNone {
		t.Fatalf("expected mode none, no error; got %q, %v", mode, err)
	}
}

// ── Wait logging ─────────────────────────────────────────────────────────────

// fakeStartupClock lets a test run many "5 s" tries without waiting: sleeping
// just moves the clock on.
type fakeStartupClock struct{ t time.Time }

func (c *fakeStartupClock) now() time.Time        { return c.t }
func (c *fakeStartupClock) sleep(d time.Duration) { c.t = c.t.Add(d) }

// N failed tries log the first one in full, then one summary per interval,
// then one line when SignalK answers — never one line per try.
func TestCheckAuthModeAtStartup_WaitLogIsFirstTryThenOneSummaryPerInterval(t *testing.T) {
	settingsPath := writeAuthStartupSettingsFixture(t, "http://signalk.test", "signalk")
	logs := captureLog(t)
	clock := &fakeStartupClock{t: time.Unix(0, 0)}
	calls := 0
	probe := func(string) (bool, error) {
		calls++
		if calls <= 25 {
			return false, &signalKUnreachableError{err: fmt.Errorf("refused %d", calls)}
		}
		return true, nil
	}

	_, err := checkAuthModeAtStartupWaiting(settingsPath, probe, startupWait{
		retry: 5 * time.Second, summary: time.Minute, now: clock.now, sleep: clock.sleep,
	})
	if err != nil {
		t.Fatalf("expected startup to succeed, got: %v", err)
	}
	// 25 failed tries at 5 s spacing: tries 1..25 at t=0..120 s. First line at
	// try 1, summaries at t=60 s and t=120 s, then the answered line.
	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 log lines (first, 2 summaries, answered), got %d:\n%s", len(lines), logs.String())
	}
	if !strings.Contains(lines[0], "http://signalk.test") || !strings.Contains(lines[0], "refused 1") ||
		!strings.Contains(lines[0], "login can't start until SignalK answers") {
		t.Fatalf("first line should name the address, the real error and the consequence: %s", lines[0])
	}
	if !strings.Contains(lines[1], "refused 13") || !strings.Contains(lines[1], "13 tries") {
		t.Fatalf("summary should carry the try count and latest error: %s", lines[1])
	}
	if !strings.Contains(lines[2], "refused 25") || !strings.Contains(lines[2], "25 tries") {
		t.Fatalf("summary should carry the try count and latest error: %s", lines[2])
	}
	if !strings.Contains(lines[3], "answering") {
		t.Fatalf("expected a final answered line, got: %s", lines[3])
	}
}

// An absent signalk section is not a read failure: the documented default
// address applies.
func TestCheckAuthModeAtStartup_AbsentSignalKSettingUsesDocumentedDefault(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(settingsPath, []byte("auth:\n  mode: signalk\n"), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	var seen string
	probe := func(u string) (bool, error) {
		seen = u
		return true, nil
	}

	if _, err := checkAuthModeAtStartupWith(settingsPath, probe, time.Millisecond); err != nil {
		t.Fatalf("expected startup to succeed, got: %v", err)
	}
	if want := buildSignalKURL(defaultSignalKAddress, defaultSignalKPort); seen != want {
		t.Fatalf("expected the default address %q, got %q", want, seen)
	}
}
