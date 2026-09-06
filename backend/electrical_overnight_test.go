package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// ── nightSlopePercentPerHour ────────────────────────────────────────────────

// syntheticSlopeSeries builds points at the given cadence from w.Start to
// w.End, walking linearly from startValue to endValue, so first/last are
// exactly startValue/endValue and the slope nightSlopePercentPerHour derives
// is exactly (endValue-startValue) scaled and divided by the window's hours.
func syntheticSlopeSeries(w nightWindow, cadence time.Duration, startValue, endValue float64) []telemetryPoint {
	steps := int(w.End.Sub(w.Start) / cadence)
	points := make([]telemetryPoint, 0, steps+1)
	for i := 0; i <= steps; i++ {
		frac := float64(i) / float64(steps)
		points = append(points, telemetryPoint{
			Timestamp: w.Start.Add(time.Duration(i) * cadence),
			Value:     startValue + frac*(endValue-startValue),
		})
	}
	return points
}

// TestNightSlopePercentPerHour_RatioScaledToPercent is the plan's own worked
// example: a night from 0.65 to 0.56 (the 0..1 ratio InfluxDB stores SoC as)
// over 12 hours is -0.75 %/h.
func TestNightSlopePercentPerHour_RatioScaledToPercent(t *testing.T) {
	w := nightWindow{Start: time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC), End: time.Date(2026, 1, 2, 8, 0, 0, 0, time.UTC)}
	points := syntheticSlopeSeries(w, time.Hour, 0.65, 0.56)

	slope, usable, reason := nightSlopePercentPerHour(points, w, time.Hour)
	if !usable {
		t.Fatalf("expected usable, got reason %q", reason)
	}
	if diff := slope - (-0.75); diff < -0.001 || diff > 0.001 {
		t.Fatalf("slope: got %v, want -0.75", slope)
	}
}

func TestNightSlopePercentPerHour_InsufficientSamplesExcluded(t *testing.T) {
	w := nightWindow{Start: time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC), End: time.Date(2026, 1, 2, 8, 0, 0, 0, time.UTC)}
	full := syntheticSlopeSeries(w, time.Hour, 65, 56)
	// Only every third sample survives: 5 of the 13 hourly points, well
	// under the 80% (of 12 expected) threshold.
	sparse := []telemetryPoint{full[0], full[3], full[6], full[9], full[12]}

	_, usable, reason := nightSlopePercentPerHour(sparse, w, time.Hour)
	if usable {
		t.Fatalf("expected not usable with only %d of 13 samples", len(sparse))
	}
	if reason != "insufficient samples" {
		t.Fatalf("reason: got %q, want %q", reason, "insufficient samples")
	}
}

func TestNightSlopePercentPerHour_NetRiseExcluded(t *testing.T) {
	w := nightWindow{Start: time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC), End: time.Date(2026, 1, 2, 8, 0, 0, 0, time.UTC)}
	// A generator or shore charger ran overnight: SoC climbed from 50% to
	// 60% instead of draining.
	points := syntheticSlopeSeries(w, time.Hour, 50, 60)

	slope, usable, reason := nightSlopePercentPerHour(points, w, time.Hour)
	if usable {
		t.Fatalf("expected not usable for a net rise, got slope %v", slope)
	}
	if reason != "net rise" {
		t.Fatalf("reason: got %q, want %q", reason, "net rise")
	}
	if slope <= 0 {
		t.Fatalf("expected the (unusable) slope to still read positive, got %v", slope)
	}
}

// ── medianNightRate ──────────────────────────────────────────────────────

func TestMedianNightRate_RequiresAtLeastTwo(t *testing.T) {
	if _, ok := medianNightRate([]float64{-1.0}); ok {
		t.Fatalf("expected ok=false for a single slope")
	}
	if _, ok := medianNightRate(nil); ok {
		t.Fatalf("expected ok=false for no slopes")
	}
}

func TestMedianNightRate_EvenCountAverages(t *testing.T) {
	median, ok := medianNightRate([]float64{-2.0, -1.0})
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if median != -1.5 {
		t.Fatalf("median: got %v, want -1.5", median)
	}
}

func TestMedianNightRate_OddCountIsMiddleRegardlessOfInputOrder(t *testing.T) {
	median, ok := medianNightRate([]float64{-1.0, -3.0, -2.0})
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if median != -2.0 {
		t.Fatalf("median: got %v, want -2.0", median)
	}
}

// ── computeOvernight: sun-time-only paths ───────────────────────────────

const failIfCalledReason = "test query should not have been called"

func failIfCalledQuery(t *testing.T) func(path string, start, stop time.Time, every string) ([]telemetryPoint, error) {
	return func(path string, start, stop time.Time, every string) ([]telemetryPoint, error) {
		t.Fatalf(failIfCalledReason)
		return nil, nil
	}
}

func TestComputeOvernight_InfluxNotConfiguredLinearWhenFlagOn(t *testing.T) {
	now := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	result := computeOvernight(now, vesselLat, vesselLon, defaultSocMeasurement, true, false, failIfCalledQuery(t))

	if result.Basis != "linear" {
		t.Fatalf("basis: got %q, want linear", result.Basis)
	}
	if result.Reason == nil || *result.Reason != "influxdb not configured" {
		t.Fatalf("reason: got %v, want \"influxdb not configured\"", result.Reason)
	}
	if result.Sunset.IsZero() || result.Sunrise.IsZero() {
		t.Fatalf("expected sunset/sunrise to still be populated: sunset=%v sunrise=%v", result.Sunset, result.Sunrise)
	}
	if result.NightRatePercentPerHour != nil {
		t.Fatalf("expected no night rate on the linear basis, got %v", *result.NightRatePercentPerHour)
	}
}

func TestComputeOvernight_InfluxNotConfiguredNoneWhenFlagOff(t *testing.T) {
	now := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	result := computeOvernight(now, vesselLat, vesselLon, defaultSocMeasurement, false, false, failIfCalledQuery(t))

	if result.Basis != "none" {
		t.Fatalf("basis: got %q, want none", result.Basis)
	}
	if result.Reason == nil || *result.Reason != "influxdb not configured" {
		t.Fatalf("reason: got %v, want \"influxdb not configured\"", result.Reason)
	}
}

func TestComputeOvernight_QueryFailureSurfacesReason(t *testing.T) {
	now := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	queryErr := errors.New("connection refused")
	query := func(path string, start, stop time.Time, every string) ([]telemetryPoint, error) {
		return nil, queryErr
	}

	result := computeOvernight(now, vesselLat, vesselLon, defaultSocMeasurement, true, true, query)
	if result.Basis != "linear" {
		t.Fatalf("basis: got %q, want linear", result.Basis)
	}
	want := "influxdb query failed: connection refused"
	if result.Reason == nil || *result.Reason != want {
		t.Fatalf("reason: got %v, want %q", result.Reason, want)
	}
}

// ── computeOvernight: history branch with synthetic nights ─────────────

// testNightWindows computes the real windows computeOvernight will ask for,
// so the fake query below can recognise exactly which night each call is
// for - computeOvernight and this helper both call recentNightWindows with
// identical (now, lat, lon), a pure function, so the two agree exactly.
func testNightWindows(t *testing.T) (now time.Time, windows []nightWindow) {
	t.Helper()
	now = time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	windows = recentNightWindows(now, vesselLat, vesselLon, overnightNightsConsidered)
	if len(windows) != overnightNightsConsidered {
		t.Fatalf("recentNightWindows: got %d windows, want %d", len(windows), overnightNightsConsidered)
	}
	return now, windows
}

// windowQuery dispatches on which of `windows` (start, stop) exactly
// matches, returning that index's entry in byIndex (nil/absent means no
// data for that night, e.g. before the vessel's history began).
func windowQuery(t *testing.T, windows []nightWindow, byIndex map[int][]telemetryPoint) func(path string, start, stop time.Time, every string) ([]telemetryPoint, error) {
	return func(path string, start, stop time.Time, every string) ([]telemetryPoint, error) {
		for i, w := range windows {
			if start.Equal(w.Start) && stop.Equal(w.End) {
				return byIndex[i], nil
			}
		}
		t.Fatalf("query called with a range that doesn't match any computed night window: start=%s stop=%s", start, stop)
		return nil, nil
	}
}

func TestComputeOvernight_ThreeCleanNightsMedian(t *testing.T) {
	now, windows := testNightWindows(t)
	query := windowQuery(t, windows, map[int][]telemetryPoint{
		0: syntheticSlopeSeries(windows[0], overnightSampleInterval, 65, 65+(-1.0)*windows[0].End.Sub(windows[0].Start).Hours()),
		1: syntheticSlopeSeries(windows[1], overnightSampleInterval, 65, 65+(-0.8)*windows[1].End.Sub(windows[1].Start).Hours()),
		2: syntheticSlopeSeries(windows[2], overnightSampleInterval, 65, 65+(-1.2)*windows[2].End.Sub(windows[2].Start).Hours()),
		// windows[3..6] left absent: nil slice, "insufficient samples".
	})

	result := computeOvernight(now, vesselLat, vesselLon, defaultSocMeasurement, true, true, query)

	if result.Basis != "history" {
		t.Fatalf("basis: got %q, want history (reason=%v)", result.Basis, result.Reason)
	}
	if result.NightsUsed != 3 {
		t.Fatalf("nights_used: got %d, want 3", result.NightsUsed)
	}
	if result.NightsConsidered != 7 {
		t.Fatalf("nights_considered: got %d, want 7", result.NightsConsidered)
	}
	if result.NightRatePercentPerHour == nil {
		t.Fatalf("expected a night rate on the history basis")
	}
	if got := *result.NightRatePercentPerHour; got < -1.001 || got > -0.999 {
		t.Fatalf("night rate: got %v, want -1.0 (median of -1.2, -1.0, -0.8)", got)
	}
}

func TestComputeOvernight_NetRiseNightExcludedButOthersStillCount(t *testing.T) {
	now, windows := testNightWindows(t)
	query := windowQuery(t, windows, map[int][]telemetryPoint{
		0: syntheticSlopeSeries(windows[0], overnightSampleInterval, 65, 65+(-1.0)*windows[0].End.Sub(windows[0].Start).Hours()),
		1: syntheticSlopeSeries(windows[1], overnightSampleInterval, 65, 65+(-0.8)*windows[1].End.Sub(windows[1].Start).Hours()),
		2: syntheticSlopeSeries(windows[2], overnightSampleInterval, 65, 65+(-1.2)*windows[2].End.Sub(windows[2].Start).Hours()),
		// A generator ran through the night: SoC climbed instead of
		// draining, fully sampled so only the slope sign excludes it.
		3: syntheticSlopeSeries(windows[3], overnightSampleInterval, 40, 40+2.0*windows[3].End.Sub(windows[3].Start).Hours()),
	})

	result := computeOvernight(now, vesselLat, vesselLon, defaultSocMeasurement, true, true, query)

	if result.Basis != "history" {
		t.Fatalf("basis: got %q, want history (reason=%v)", result.Basis, result.Reason)
	}
	if result.NightsUsed != 3 {
		t.Fatalf("nights_used: got %d, want 3 (the rise night must not count)", result.NightsUsed)
	}
}

func TestComputeOvernight_GapNightExcluded(t *testing.T) {
	now, windows := testNightWindows(t)
	fullNight0 := syntheticSlopeSeries(windows[0], overnightSampleInterval, 65, 65+(-1.0)*windows[0].End.Sub(windows[0].Start).Hours())
	fullNight1 := syntheticSlopeSeries(windows[1], overnightSampleInterval, 65, 65+(-0.8)*windows[1].End.Sub(windows[1].Start).Hours())
	// A night with most of its samples missing - the SignalK stream or the
	// InfluxDB write path dropped out partway through.
	gapNight2 := syntheticSlopeSeries(windows[2], overnightSampleInterval, 65, 55)
	gapNight2 = gapNight2[:3]

	query := windowQuery(t, windows, map[int][]telemetryPoint{
		0: fullNight0,
		1: fullNight1,
		2: gapNight2,
	})

	result := computeOvernight(now, vesselLat, vesselLon, defaultSocMeasurement, true, true, query)

	if result.Basis != "history" {
		t.Fatalf("basis: got %q, want history (reason=%v)", result.Basis, result.Reason)
	}
	if result.NightsUsed != 2 {
		t.Fatalf("nights_used: got %d, want 2 (the gap night must not count)", result.NightsUsed)
	}
	if got := *result.NightRatePercentPerHour; got < -0.901 || got > -0.899 {
		t.Fatalf("night rate: got %v, want -0.9 (average of -1.0 and -0.8)", got)
	}
}

func TestComputeOvernight_OneUsableNightLinearWhenFlagOn(t *testing.T) {
	now, windows := testNightWindows(t)
	query := windowQuery(t, windows, map[int][]telemetryPoint{
		0: syntheticSlopeSeries(windows[0], overnightSampleInterval, 65, 65+(-1.0)*windows[0].End.Sub(windows[0].Start).Hours()),
	})

	result := computeOvernight(now, vesselLat, vesselLon, defaultSocMeasurement, true, true, query)

	if result.Basis != "linear" {
		t.Fatalf("basis: got %q, want linear", result.Basis)
	}
	want := "need 2 usable nights, have 1"
	if result.Reason == nil || *result.Reason != want {
		t.Fatalf("reason: got %v, want %q", result.Reason, want)
	}
	if result.NightRatePercentPerHour != nil {
		t.Fatalf("expected no night rate outside the history basis")
	}
}

func TestComputeOvernight_OneUsableNightNoneWhenFlagOff(t *testing.T) {
	now, windows := testNightWindows(t)
	query := windowQuery(t, windows, map[int][]telemetryPoint{
		0: syntheticSlopeSeries(windows[0], overnightSampleInterval, 65, 65+(-1.0)*windows[0].End.Sub(windows[0].Start).Hours()),
	})

	result := computeOvernight(now, vesselLat, vesselLon, defaultSocMeasurement, false, true, query)

	if result.Basis != "none" {
		t.Fatalf("basis: got %q, want none", result.Basis)
	}
	want := "need 2 usable nights, have 1"
	if result.Reason == nil || *result.Reason != want {
		t.Fatalf("reason: got %v, want %q", result.Reason, want)
	}
}

// ── computeOvernight: real 7-day fixture ────────────────────────────────

// socHistoryFixture mirrors the /api/telemetry/history response shape
// (backend/testdata/soc_history_7d.json), captured live from the dev
// backend per the plan: electrical.batteries.0.capacity.stateOfCharge, 0..1
// ratio, hourly means, 2026-08-30T22:00Z to 2026-09-06T21:58Z.
type socHistoryFixture struct {
	Path   string `json:"path"`
	Points []struct {
		Time  string  `json:"time"`
		Value float64 `json:"value"`
	} `json:"points"`
}

func loadSocHistoryFixture(t *testing.T) []telemetryPoint {
	t.Helper()
	raw, err := os.ReadFile("testdata/soc_history_7d.json")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	var fixture socHistoryFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("parsing fixture: %v", err)
	}
	points := make([]telemetryPoint, 0, len(fixture.Points))
	for _, p := range fixture.Points {
		ts, err := time.Parse(time.RFC3339, p.Time)
		if err != nil {
			t.Fatalf("parsing fixture timestamp %q: %v", p.Time, err)
		}
		points = append(points, telemetryPoint{Timestamp: ts, Value: p.Value})
	}
	return points
}

// TestComputeOvernight_RealFixtureReachesHistory exercises computeOvernight
// end to end against the captured live series instead of an assumed shape,
// with hourly aggregation (every=1h) to match how the fixture was captured -
// a live InfluxDB would be asked for 15-minute means, but this fixture is
// what /api/telemetry/history?window=7d actually returned.
func TestComputeOvernight_RealFixtureReachesHistory(t *testing.T) {
	origEvery, origInterval := overnightQueryEvery, overnightSampleInterval
	overnightQueryEvery, overnightSampleInterval = "1h", time.Hour
	t.Cleanup(func() { overnightQueryEvery, overnightSampleInterval = origEvery, origInterval })

	fixture := loadSocHistoryFixture(t)
	now := fixture[len(fixture)-1].Timestamp

	query := func(path string, start, stop time.Time, every string) ([]telemetryPoint, error) {
		var out []telemetryPoint
		for _, p := range fixture {
			if !p.Timestamp.Before(start) && !p.Timestamp.After(stop) {
				out = append(out, p)
			}
		}
		return out, nil
	}

	result := computeOvernight(now, vesselLat, vesselLon, defaultSocMeasurement, true, true, query)

	if result.Basis != "history" {
		t.Fatalf("basis: got %q (reason=%v), want history - the vessel has been at anchor with a week of history", result.Basis, result.Reason)
	}
	if result.NightsUsed < 2 {
		t.Fatalf("nights_used: got %d, want at least 2", result.NightsUsed)
	}
	if result.NightRatePercentPerHour == nil {
		t.Fatalf("expected a night rate on the history basis")
	}
	if got := *result.NightRatePercentPerHour; got >= 0 {
		t.Fatalf("night rate: got %v, want negative (a discharging bank)", got)
	}
}

// ── HTTP handler ─────────────────────────────────────────────────────────

func TestElectricalOvernightHandler_PositionUnavailableIs503(t *testing.T) {
	// The GNSS validator is a package-level singleton with its own
	// hysteresis state (gnss_validation.go) so a "position lost" reading
	// here doesn't leak into later tests as a false "position just
	// jumped" flag - every other test touching fetchSignalKVesselState
	// resets it the same way (see signalk_test.go, tracks_test.go, etc.).
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	origSnapshot := globalSignalKSnapshot
	globalSignalKSnapshot = newSignalKSnapshot() // no self context registered at all
	t.Cleanup(func() { globalSignalKSnapshot = origSnapshot })

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/electrical/overnight", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := electricalOvernightHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response body: %v", err)
	}
	want := "vessel position unavailable; dawn projection needs it for sunrise"
	if body["error"] != want {
		t.Fatalf("error: got %q, want %q", body["error"], want)
	}
}
