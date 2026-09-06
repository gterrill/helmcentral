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

// allLookbackWindows computes the real windows computeOvernight will ask
// recentNightWindows for at its lookback constant, so the fake queries
// below can be built against exactly the same windows computeOvernight
// itself will compute - recentNightWindows is pure, so the two agree
// exactly given identical (now, lat, lon).
func allLookbackWindows(t *testing.T) (now time.Time, windows []nightWindow) {
	t.Helper()
	now = time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	windows = recentNightWindows(now, vesselLat, vesselLon, overnightLookbackNights)
	if len(windows) != overnightLookbackNights {
		t.Fatalf("recentNightWindows: got %d windows, want %d", len(windows), overnightLookbackNights)
	}
	return now, windows
}

// concatPoints flattens per-night point slices (some deliberately nil, for
// a night left without data) into the one series a real Influx query now
// returns - the whole lookback range in a single call, sliced per night in
// Go rather than queried per night.
func concatPoints(nightly ...[]telemetryPoint) []telemetryPoint {
	var out []telemetryPoint
	for _, points := range nightly {
		out = append(out, points...)
	}
	return out
}

// lookbackQuery builds the fake query computeOvernight now calls exactly
// three times per compute - once for socPath, once for the shore path and
// once for the generator path - each covering the single wide
// [oldest-window-start, now] range the real implementation asks for. It
// dispatches on which path was requested and filters that path's series by
// [start, stop], the same filtering queryInfluxPathRange itself does.
func lookbackQuery(t *testing.T, socSeries, shoreSeries, generatorSeries []telemetryPoint) func(path string, start, stop time.Time, every string) ([]telemetryPoint, error) {
	t.Helper()
	filterRange := func(points []telemetryPoint, start, stop time.Time) []telemetryPoint {
		var out []telemetryPoint
		for _, p := range points {
			if !p.Timestamp.Before(start) && !p.Timestamp.After(stop) {
				out = append(out, p)
			}
		}
		return out
	}
	return func(path string, start, stop time.Time, every string) ([]telemetryPoint, error) {
		switch path {
		case defaultSocMeasurement:
			return filterRange(socSeries, start, stop), nil
		case defaultShoreMeasurement:
			return filterRange(shoreSeries, start, stop), nil
		case defaultGeneratorMeasurement:
			return filterRange(generatorSeries, start, stop), nil
		default:
			t.Fatalf("query called with unexpected path %q", path)
			return nil, nil
		}
	}
}

func TestComputeOvernight_ThreeCleanNightsMedian(t *testing.T) {
	now, windows := allLookbackWindows(t)
	socSeries := concatPoints(
		syntheticSlopeSeries(windows[0], overnightSampleInterval, 65, 65+(-1.0)*windows[0].End.Sub(windows[0].Start).Hours()),
		syntheticSlopeSeries(windows[1], overnightSampleInterval, 65, 65+(-0.8)*windows[1].End.Sub(windows[1].Start).Hours()),
		syntheticSlopeSeries(windows[2], overnightSampleInterval, 65, 65+(-1.2)*windows[2].End.Sub(windows[2].Start).Hours()),
		// windows[3:] left without data: "insufficient samples" each.
	)
	query := lookbackQuery(t, socSeries, nil, nil)

	result := computeOvernight(now, vesselLat, vesselLon, defaultSocMeasurement, true, true, query)

	if result.Basis != "history" {
		t.Fatalf("basis: got %q, want history (reason=%v)", result.Basis, result.Reason)
	}
	if result.NightsUsed != 3 {
		t.Fatalf("nights_used: got %d, want 3", result.NightsUsed)
	}
	if result.NightsConsidered != overnightLookbackNights {
		t.Fatalf("nights_considered: got %d, want %d (only 3 usable, so the full lookback is exhausted)", result.NightsConsidered, overnightLookbackNights)
	}
	if result.NightsExcludedGap != overnightLookbackNights-3 {
		t.Fatalf("nights_excluded_gap: got %d, want %d", result.NightsExcludedGap, overnightLookbackNights-3)
	}
	if result.LookbackNights != overnightLookbackNights {
		t.Fatalf("lookback_nights: got %d, want %d", result.LookbackNights, overnightLookbackNights)
	}
	if result.NightRatePercentPerHour == nil {
		t.Fatalf("expected a night rate on the history basis")
	}
	if got := *result.NightRatePercentPerHour; got < -1.001 || got > -0.999 {
		t.Fatalf("night rate: got %v, want -1.0 (median of -1.2, -1.0, -0.8)", got)
	}
	if !result.OldestNightStart.Equal(windows[overnightLookbackNights-1].Start) {
		t.Fatalf("oldest_night_start: got %v, want %v (the last window scanned)", result.OldestNightStart, windows[overnightLookbackNights-1].Start)
	}
}

func TestComputeOvernight_NetRiseNightExcludedButOthersStillCount(t *testing.T) {
	now, windows := allLookbackWindows(t)
	socSeries := concatPoints(
		syntheticSlopeSeries(windows[0], overnightSampleInterval, 65, 65+(-1.0)*windows[0].End.Sub(windows[0].Start).Hours()),
		syntheticSlopeSeries(windows[1], overnightSampleInterval, 65, 65+(-0.8)*windows[1].End.Sub(windows[1].Start).Hours()),
		syntheticSlopeSeries(windows[2], overnightSampleInterval, 65, 65+(-1.2)*windows[2].End.Sub(windows[2].Start).Hours()),
		// Neither charger circuit shows anything for this night, but SoC
		// climbed instead of draining regardless - the net-rise backstop is
		// what catches a source the shore/generator paths didn't see.
		syntheticSlopeSeries(windows[3], overnightSampleInterval, 40, 40+2.0*windows[3].End.Sub(windows[3].Start).Hours()),
	)
	query := lookbackQuery(t, socSeries, nil, nil)

	result := computeOvernight(now, vesselLat, vesselLon, defaultSocMeasurement, true, true, query)

	if result.Basis != "history" {
		t.Fatalf("basis: got %q, want history (reason=%v)", result.Basis, result.Reason)
	}
	if result.NightsUsed != 3 {
		t.Fatalf("nights_used: got %d, want 3 (the rise night must not count)", result.NightsUsed)
	}
	if result.NightsExcludedRise != 1 {
		t.Fatalf("nights_excluded_rise: got %d, want 1", result.NightsExcludedRise)
	}
}

func TestComputeOvernight_GapNightExcluded(t *testing.T) {
	now, windows := allLookbackWindows(t)
	fullNight0 := syntheticSlopeSeries(windows[0], overnightSampleInterval, 65, 65+(-1.0)*windows[0].End.Sub(windows[0].Start).Hours())
	fullNight1 := syntheticSlopeSeries(windows[1], overnightSampleInterval, 65, 65+(-0.8)*windows[1].End.Sub(windows[1].Start).Hours())
	// A night with most of its samples missing - the SignalK stream or the
	// InfluxDB write path dropped out partway through.
	gapNight2 := syntheticSlopeSeries(windows[2], overnightSampleInterval, 65, 55)
	gapNight2 = gapNight2[:3]

	socSeries := concatPoints(fullNight0, fullNight1, gapNight2)
	query := lookbackQuery(t, socSeries, nil, nil)

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
	now, windows := allLookbackWindows(t)
	socSeries := syntheticSlopeSeries(windows[0], overnightSampleInterval, 65, 65+(-1.0)*windows[0].End.Sub(windows[0].Start).Hours())
	query := lookbackQuery(t, socSeries, nil, nil)

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
	now, windows := allLookbackWindows(t)
	socSeries := syntheticSlopeSeries(windows[0], overnightSampleInterval, 65, 65+(-1.0)*windows[0].End.Sub(windows[0].Start).Hours())
	query := lookbackQuery(t, socSeries, nil, nil)

	result := computeOvernight(now, vesselLat, vesselLon, defaultSocMeasurement, false, true, query)

	if result.Basis != "none" {
		t.Fatalf("basis: got %q, want none", result.Basis)
	}
	want := "need 2 usable nights, have 1"
	if result.Reason == nil || *result.Reason != want {
		t.Fatalf("reason: got %v, want %q", result.Reason, want)
	}
}

// ── computeOvernight: shore power and generator exclusions ──────────────

// TestComputeOvernight_ShorePowerNightsExcluded is the marina-week case the
// exclusion rules exist for: a charger holding the state of charge flat
// scores as a near-zero slope, which used to pass the net-rise check and
// pull the median toward zero. windows[0..2] look like exactly that; the
// shore series has current present throughout each of those three windows,
// so they must be excluded before the slope is even judged. windows[3..9]
// are seven clean discharging nights - enough to hit the target and stop
// scanning before windows[10] and beyond are ever looked at.
func TestComputeOvernight_ShorePowerNightsExcluded(t *testing.T) {
	now, windows := allLookbackWindows(t)

	var socSeries, shoreSeries []telemetryPoint
	for i := 0; i < 3; i++ {
		socSeries = append(socSeries, syntheticSlopeSeries(windows[i], overnightSampleInterval, 65, 65)...)     // flat: charger in float
		shoreSeries = append(shoreSeries, syntheticSlopeSeries(windows[i], overnightSampleInterval, 12, 12)...) // 12A the whole night
	}
	for i := 3; i < 10; i++ {
		socSeries = append(socSeries, syntheticSlopeSeries(windows[i], overnightSampleInterval, 65, 65+(-1.0)*windows[i].End.Sub(windows[i].Start).Hours())...)
	}

	query := lookbackQuery(t, socSeries, shoreSeries, nil)
	result := computeOvernight(now, vesselLat, vesselLon, defaultSocMeasurement, true, true, query)

	if result.Basis != "history" {
		t.Fatalf("basis: got %q, want history (reason=%v)", result.Basis, result.Reason)
	}
	if result.NightsExcludedShore != 3 {
		t.Fatalf("nights_excluded_shore: got %d, want 3", result.NightsExcludedShore)
	}
	if result.NightsUsed != overnightTargetNights {
		t.Fatalf("nights_used: got %d, want %d", result.NightsUsed, overnightTargetNights)
	}
	if result.NightsConsidered != 10 {
		t.Fatalf("nights_considered: got %d, want 10 (3 shore-excluded plus 7 usable)", result.NightsConsidered)
	}
	if result.NightRatePercentPerHour == nil {
		t.Fatalf("expected a night rate on the history basis")
	}
	if got := *result.NightRatePercentPerHour; got < -1.001 || got > -0.999 {
		t.Fatalf("night rate: got %v, want -1.0 - the flat shore-power nights must not pull the median toward zero", got)
	}
}

// TestComputeOvernight_GeneratorNightExcluded checks a single 15-minute
// sample of generator stateNumber > 0 is enough on its own to exclude a
// night, even though the SoC series for that night is flat in exactly the
// shape a charger-fed night would be (the shore series is empty, so only
// the generator check is doing anything here).
func TestComputeOvernight_GeneratorNightExcluded(t *testing.T) {
	now, windows := allLookbackWindows(t)
	genPoint := []telemetryPoint{{Timestamp: windows[0].Start.Add(time.Hour), Value: 1}}
	socSeries := concatPoints(
		syntheticSlopeSeries(windows[0], overnightSampleInterval, 65, 65),
		syntheticSlopeSeries(windows[1], overnightSampleInterval, 65, 65+(-1.0)*windows[1].End.Sub(windows[1].Start).Hours()),
		syntheticSlopeSeries(windows[2], overnightSampleInterval, 65, 65+(-0.8)*windows[2].End.Sub(windows[2].Start).Hours()),
	)
	query := lookbackQuery(t, socSeries, nil, genPoint)

	result := computeOvernight(now, vesselLat, vesselLon, defaultSocMeasurement, true, true, query)

	if result.Basis != "history" {
		t.Fatalf("basis: got %q, want history (reason=%v)", result.Basis, result.Reason)
	}
	if result.NightsExcludedGenerator != 1 {
		t.Fatalf("nights_excluded_generator: got %d, want 1", result.NightsExcludedGenerator)
	}
	if result.NightsUsed != 2 {
		t.Fatalf("nights_used: got %d, want 2", result.NightsUsed)
	}
}

// ── computeOvernight: lookback scanning bounds ──────────────────────────

func TestComputeOvernight_StopsAtTargetNights(t *testing.T) {
	now, windows := allLookbackWindows(t)
	// Twelve clean discharging nights are available, but the scan only
	// needs overnightTargetNights (7) of them and must stop there.
	var socSeries []telemetryPoint
	for i := 0; i < 12; i++ {
		socSeries = append(socSeries, syntheticSlopeSeries(windows[i], overnightSampleInterval, 65, 65+(-1.0)*windows[i].End.Sub(windows[i].Start).Hours())...)
	}
	query := lookbackQuery(t, socSeries, nil, nil)

	result := computeOvernight(now, vesselLat, vesselLon, defaultSocMeasurement, true, true, query)

	if result.NightsUsed != overnightTargetNights {
		t.Fatalf("nights_used: got %d, want %d", result.NightsUsed, overnightTargetNights)
	}
	if result.NightsConsidered != overnightTargetNights {
		t.Fatalf("nights_considered: got %d, want %d (scanning should stop once the target is met)", result.NightsConsidered, overnightTargetNights)
	}
	if !result.OldestNightStart.Equal(windows[overnightTargetNights-1].Start) {
		t.Fatalf("oldest_night_start: got %v, want %v", result.OldestNightStart, windows[overnightTargetNights-1].Start)
	}
}

func TestComputeOvernight_LookbackExhaustion(t *testing.T) {
	now, windows := allLookbackWindows(t)
	// Only 4 clean nights exist anywhere in the 30-night lookback; the scan
	// must exhaust the whole lookback looking for the other 3 before giving
	// up and reporting on what it actually found.
	var socSeries []telemetryPoint
	for i := 0; i < 4; i++ {
		socSeries = append(socSeries, syntheticSlopeSeries(windows[i], overnightSampleInterval, 65, 65+(-1.0)*windows[i].End.Sub(windows[i].Start).Hours())...)
	}
	query := lookbackQuery(t, socSeries, nil, nil)

	result := computeOvernight(now, vesselLat, vesselLon, defaultSocMeasurement, true, true, query)

	if result.Basis != "history" {
		t.Fatalf("basis: got %q, want history (reason=%v) - 4 usable nights is still >= the minimum of 2", result.Basis, result.Reason)
	}
	if result.NightsUsed != 4 {
		t.Fatalf("nights_used: got %d, want 4", result.NightsUsed)
	}
	if result.NightsConsidered != overnightLookbackNights {
		t.Fatalf("nights_considered: got %d, want %d (the full lookback, since only 4 usable nights were ever found)", result.NightsConsidered, overnightLookbackNights)
	}
	if result.NightsExcludedGap != overnightLookbackNights-4 {
		t.Fatalf("nights_excluded_gap: got %d, want %d", result.NightsExcludedGap, overnightLookbackNights-4)
	}
	if !result.OldestNightStart.Equal(windows[overnightLookbackNights-1].Start) {
		t.Fatalf("oldest_night_start: got %v, want %v (the very last window scanned)", result.OldestNightStart, windows[overnightLookbackNights-1].Start)
	}
}

// TestComputeOvernight_ShoreQueryFailureFailsFastEvenWithGoodSoc is
// deliverable 2's fail-fast rule: an error on any of the three paths must
// surface exactly as a SoC query error does today, never be treated as "no
// shore power seen" and quietly fall through to the SoC-only slope checks.
func TestComputeOvernight_ShoreQueryFailureFailsFastEvenWithGoodSoc(t *testing.T) {
	now, windows := allLookbackWindows(t)
	socSeries := concatPoints(
		syntheticSlopeSeries(windows[0], overnightSampleInterval, 65, 65+(-1.0)*windows[0].End.Sub(windows[0].Start).Hours()),
		syntheticSlopeSeries(windows[1], overnightSampleInterval, 65, 65+(-0.8)*windows[1].End.Sub(windows[1].Start).Hours()),
	)
	queryErr := errors.New("shore measurement not found")
	query := func(path string, start, stop time.Time, every string) ([]telemetryPoint, error) {
		switch path {
		case defaultShoreMeasurement:
			return nil, queryErr
		case defaultSocMeasurement:
			return socSeries, nil
		default:
			return nil, nil
		}
	}

	result := computeOvernight(now, vesselLat, vesselLon, defaultSocMeasurement, true, true, query)

	if result.Basis != "linear" {
		t.Fatalf("basis: got %q, want linear - a failed shore query must not be treated as \"no shore power\"", result.Basis)
	}
	want := "influxdb query failed: shore measurement not found"
	if result.Reason == nil || *result.Reason != want {
		t.Fatalf("reason: got %v, want %q", result.Reason, want)
	}
	if result.NightRatePercentPerHour != nil {
		t.Fatalf("expected no night rate when a query path failed")
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
		if path != defaultSocMeasurement {
			// The fixture only captured SoC history; no shore or generator
			// activity was recorded for this week, so both paths come back
			// empty and neither exclusion fires.
			return nil, nil
		}
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

func TestPositionForSun_RejectsSentinelCriticalAndRange(t *testing.T) {
	cases := []struct {
		name  string
		state vesselStateData
		want  bool
	}{
		{"valid fix", vesselStateData{Latitude: -20.109, Longitude: 148.894}, true},
		{"the -1/-1 no-position sentinel", vesselStateData{Latitude: -1, Longitude: -1}, false},
		{"gnss gate holding the position back", vesselStateData{Latitude: -20.109, Longitude: 148.894, GNSSCriticalAlert: true}, false},
		{"latitude out of range", vesselStateData{Latitude: 91, Longitude: 0}, false},
		{"a genuine -1 latitude with a real longitude", vesselStateData{Latitude: -1, Longitude: 148.894}, true},
	}
	for _, tc := range cases {
		_, _, ok := positionForSun(tc.state)
		if ok != tc.want {
			t.Errorf("%s: ok=%v, want %v", tc.name, ok, tc.want)
		}
	}
}

func TestOvernightCache_KeyedOnWholeDegreePosition(t *testing.T) {
	c := &overnightCache{}
	now := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	calls := 0
	compute := func() overnightResult { calls++; return overnightResult{NightsUsed: calls} }

	c.get(now, -20.109, 148.894, compute)
	c.get(now.Add(time.Minute), -20.2, 148.7, compute) // same whole degrees: served from cache
	if calls != 1 {
		t.Fatalf("expected one compute for two calls at the same whole-degree position, got %d", calls)
	}
	c.get(now.Add(2*time.Minute), -21.5, 148.7, compute) // a degree south: recompute
	if calls != 2 {
		t.Fatalf("expected a recompute after moving a whole degree, got %d computes", calls)
	}
	if _, ok := c.fresh(now.Add(3 * time.Minute)); !ok {
		t.Fatalf("a result computed a minute ago must be fresh regardless of position")
	}
	if _, ok := c.fresh(now.Add(overnightCacheTTL + 3*time.Minute)); ok {
		t.Fatalf("an expired result must not be served as fresh")
	}
}
