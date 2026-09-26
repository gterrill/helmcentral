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
// vessel-state payload carries the vessel's local zone derived from
// longitude (ADR 0035), not the browser's. The wall display's Ubuntu box is
// stuck on UTC, so the frontend clock needs this field to render the
// vessel's actual local time rather than the browser's.
func TestBuildVesselStatePayload_TimezoneReflectsVesselLocalZone(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	server := trustedSignalKPayloadServer(t, -21.1113, 148.9) // UTC+10
	defer server.Close()
	host, port := hostPort(t, server.URL)
	t.Setenv("SETTINGS_FILE", writeTestSettings(t, host, port))

	payload := buildVesselStatePayload()

	if got := payload["timezone"]; got != "Etc/GMT-10" {
		t.Fatalf("timezone: got %v, want %q", got, "Etc/GMT-10")
	}
}

// TestBuildVesselStatePayload_TimezoneFallsBackToUTCForSentinelPosition is
// the no-masking-fallback half: when the position resolves to the -1,-1
// sentinel, the payload must report the honest UTC fallback rather than
// inventing a zone from a bogus longitude.
func TestBuildVesselStatePayload_TimezoneFallsBackToUTCForSentinelPosition(t *testing.T) {
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
}
