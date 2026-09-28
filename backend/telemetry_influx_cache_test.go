package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// resetTelemetryInfluxSlot leaves globalTelemetryInfluxSlot exactly as a
// fresh process would have it (zero-value: nothing fetched yet), so one
// test's stored result never leaks into another test that reads the slot
// (e.g. TestApplyInfluxSolarOverride_ReplacesAllFourFieldsWholesale in
// main_test.go, which relies on it starting empty).
func resetTelemetryInfluxSlot(t *testing.T) {
	t.Helper()
	globalTelemetryInfluxSlot.set(telemetryInfluxResult{})
	t.Cleanup(func() { globalTelemetryInfluxSlot.set(telemetryInfluxResult{}) })
}

// TestTelemetryInfluxFetcherRefresh_DoesNotCallInfluxQueryFuncsDirectly
// proves refresh() only ever goes through its injected func fields, never a
// hard-coded call to the real queryInfluxMaxWindGustKtsFor/queryInfluxSolar*
// functions -- those would try to dial Influx. Fake funcs that record they
// were called, returning made-up data, stand in for a live connection.
func TestTelemetryInfluxFetcherRefresh_UsesInjectedQueryFuncs(t *testing.T) {
	resetTelemetryInfluxSlot(t)

	var gustCalled, todayCalled, yesterdayCalled, peakCalled, trendCalled bool
	fetcher := &telemetryInfluxFetcher{
		slot: globalTelemetryInfluxSlot,
		queryGust: func(windows []string) map[string]float64 {
			gustCalled = true
			return map[string]float64{"10m": 12.3, "30m": 14.1, "1h": 15.0, "24h": 20.2}
		},
		querySolarToday:     func(time.Time, *time.Location) float64 { todayCalled = true; return 4.5 },
		querySolarYesterday: func(time.Time, *time.Location) float64 { yesterdayCalled = true; return 6.1 },
		querySolarPeak:      func(time.Time, *time.Location) float64 { peakCalled = true; return 820 },
		querySolarTrend: func(time.Time) []solarTrendPoint {
			trendCalled = true
			return []solarTrendPoint{{Time: time.Now(), TotalW: 500}}
		},
		vesselLocalLocation: func() (*time.Location, bool) { return time.UTC, true },
	}

	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	fetcher.refresh(now)

	if !gustCalled || !todayCalled || !yesterdayCalled || !peakCalled || !trendCalled {
		t.Fatalf("expected refresh to call every injected query func, got gust=%v today=%v yesterday=%v peak=%v trend=%v",
			gustCalled, todayCalled, yesterdayCalled, peakCalled, trendCalled)
	}

	result := globalTelemetryInfluxSlot.get()
	if result.gustKts["24h"] != 20.2 || result.solarTodayKWh != 4.5 || result.solarPeakTodayW != 820 {
		t.Fatalf("expected the slot to store the injected funcs' results, got %+v", result)
	}
	if !result.fetchedAt.Equal(now) {
		t.Fatalf("expected fetchedAt %v, got %v", now, result.fetchedAt)
	}
}

// TestTelemetryInfluxFetcherRefresh_PassesResolvedLocationToSolarQueries
// pins refresh()'s wiring: whatever *time.Location its injected
// vesselLocalLocation func resolves must reach every one of the three
// day-boundary solar queries, not a hard-coded UTC.
func TestTelemetryInfluxFetcherRefresh_PassesResolvedLocationToSolarQueries(t *testing.T) {
	resetTelemetryInfluxSlot(t)

	wantLoc := vesselLocalLocation(153.0)

	var gotTodayLoc, gotYesterdayLoc, gotPeakLoc *time.Location
	fetcher := &telemetryInfluxFetcher{
		slot:      globalTelemetryInfluxSlot,
		queryGust: func([]string) map[string]float64 { return map[string]float64{} },
		querySolarToday: func(now time.Time, loc *time.Location) float64 {
			gotTodayLoc = loc
			return 0
		},
		querySolarYesterday: func(now time.Time, loc *time.Location) float64 {
			gotYesterdayLoc = loc
			return 0
		},
		querySolarPeak: func(now time.Time, loc *time.Location) float64 {
			gotPeakLoc = loc
			return 0
		},
		querySolarTrend:     func(time.Time) []solarTrendPoint { return nil },
		vesselLocalLocation: func() (*time.Location, bool) { return wantLoc, true },
	}

	fetcher.refresh(time.Now().UTC())

	if gotTodayLoc != wantLoc || gotYesterdayLoc != wantLoc || gotPeakLoc != wantLoc {
		t.Fatalf("expected refresh to pass the resolved vessel-local location to every solar query, got today=%v yesterday=%v peak=%v want=%v",
			gotTodayLoc, gotYesterdayLoc, gotPeakLoc, wantLoc)
	}
}

// TestTelemetryInfluxFetcherRefresh_NoPositionReportsSentinelsAndSkipsSolarQueries
// covers the "timezone not known at startup" follow-up: before the first
// solar sample with a position has arrived (or if SignalK never reports
// panel power at all), refresh() must not silently assume UTC. It must
// report today_kwh/yesterday_kwh/peak_today_w as the -1 sentinel for that
// tick, and must not call the three day-boundary solar queries at all - a
// stub that returns a recognisable non-sentinel value proves this, since
// calling it with loc=UTC would have "worked" (returned a plausible-looking
// number) and hidden the bug.
func TestTelemetryInfluxFetcherRefresh_NoPositionReportsSentinelsAndSkipsSolarQueries(t *testing.T) {
	resetTelemetryInfluxSlot(t)

	var todayCalled, yesterdayCalled, peakCalled bool
	fetcher := &telemetryInfluxFetcher{
		slot:      globalTelemetryInfluxSlot,
		queryGust: func([]string) map[string]float64 { return map[string]float64{"10m": 5.0} },
		querySolarToday: func(time.Time, *time.Location) float64 {
			todayCalled = true
			return 99
		},
		querySolarYesterday: func(time.Time, *time.Location) float64 {
			yesterdayCalled = true
			return 99
		},
		querySolarPeak: func(time.Time, *time.Location) float64 {
			peakCalled = true
			return 99
		},
		querySolarTrend:     func(time.Time) []solarTrendPoint { return []solarTrendPoint{{TotalW: 1}} },
		vesselLocalLocation: func() (*time.Location, bool) { return nil, false },
	}

	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	fetcher.refresh(now)

	if todayCalled || yesterdayCalled || peakCalled {
		t.Fatalf("expected refresh to skip the day-boundary solar queries entirely with no known vessel position, got today=%v yesterday=%v peak=%v",
			todayCalled, yesterdayCalled, peakCalled)
	}

	result := globalTelemetryInfluxSlot.get()
	if result.solarTodayKWh != -1 || result.solarYesterdayKWh != -1 || result.solarPeakTodayW != -1 {
		t.Fatalf("expected the solar day-bounded fields to report sentinel -1 with no known position, got %+v", result)
	}
	// Gust and the rolling 24h trend have nothing to do with vessel position
	// and must still run.
	if len(result.gustKts) == 0 {
		t.Fatalf("expected the gust ladder to still be queried with no known vessel position, got %+v", result)
	}
	if len(result.solarTrend24h) == 0 {
		t.Fatalf("expected the rolling 24h trend to still be queried with no known vessel position, got %+v", result.solarTrend24h)
	}
	if !result.fetchedAt.Equal(now) {
		t.Fatalf("expected fetchedAt %v, got %v", now, result.fetchedAt)
	}
}

// TestTelemetryInfluxFetcherRefresh_PositionFlapReusesLastKnownLocation
// covers a review finding: the -1,-1 sentinel position (see
// hasUsableVesselPosition, weather_providers.go) flaps in and out at anchor,
// and the very first tick after every restart also has no fix yet. Neither
// case means the boat's local timezone actually changed, and the solar
// figures themselves are unaffected - only the day-boundary math needs a
// zone. Once a real fix has been resolved once, a later tick with no fix
// must keep using that last-known zone (and must still call the
// day-boundary queries with it) rather than reporting -1 the way
// "never seen a position at all" correctly still does.
func TestTelemetryInfluxFetcherRefresh_PositionFlapReusesLastKnownLocation(t *testing.T) {
	resetTelemetryInfluxSlot(t)

	knownLoc := vesselLocalLocation(153.0)

	var gotTodayLoc, gotYesterdayLoc, gotPeakLoc *time.Location
	positionOK := true // tick 1 has a fix; tick 2 (below) flips this to false
	fetcher := &telemetryInfluxFetcher{
		slot:      globalTelemetryInfluxSlot,
		queryGust: func([]string) map[string]float64 { return map[string]float64{"10m": 5.0} },
		querySolarToday: func(now time.Time, loc *time.Location) float64 {
			gotTodayLoc = loc
			return 4.5
		},
		querySolarYesterday: func(now time.Time, loc *time.Location) float64 {
			gotYesterdayLoc = loc
			return 6.1
		},
		querySolarPeak: func(now time.Time, loc *time.Location) float64 {
			gotPeakLoc = loc
			return 820
		},
		querySolarTrend: func(time.Time) []solarTrendPoint { return []solarTrendPoint{{TotalW: 1}} },
		vesselLocalLocation: func() (*time.Location, bool) {
			if positionOK {
				return knownLoc, true
			}
			return nil, false
		},
	}

	tick1 := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	fetcher.refresh(tick1)

	result := globalTelemetryInfluxSlot.get()
	if result.solarTodayKWh != 4.5 || result.solarYesterdayKWh != 6.1 || result.solarPeakTodayW != 820 {
		t.Fatalf("expected tick 1's real fix to produce real solar figures, got %+v", result)
	}

	// Tick 2: the position sentinel flaps in (GNSS untrusted at anchor).
	positionOK = false
	gotTodayLoc, gotYesterdayLoc, gotPeakLoc = nil, nil, nil
	tick2 := tick1.Add(telemetryInfluxRefreshInterval)
	fetcher.refresh(tick2)

	if gotTodayLoc != knownLoc || gotYesterdayLoc != knownLoc || gotPeakLoc != knownLoc {
		t.Fatalf("expected tick 2 to reuse the last-known location for every day-boundary query, got today=%v yesterday=%v peak=%v want=%v",
			gotTodayLoc, gotYesterdayLoc, gotPeakLoc, knownLoc)
	}

	result = globalTelemetryInfluxSlot.get()
	if result.solarTodayKWh == -1 || result.solarYesterdayKWh == -1 || result.solarPeakTodayW == -1 {
		t.Fatalf("expected tick 2 to keep reporting real solar figures using the last-known zone despite the position flap, got %+v", result)
	}
	if !result.fetchedAt.Equal(tick2) {
		t.Fatalf("expected fetchedAt %v, got %v", tick2, result.fetchedAt)
	}
}

// TestCurrentVesselLocalLocation_UsesCachedSnapshotPosition pins
// currentVesselLocalLocation (the real, non-injected implementation
// newTelemetryInfluxFetcher wires in) to reading globalSignalKSnapshot's own
// cached navigation.position - the same delta-stream snapshot
// readOwnEncounterFacts (collision_ais.go) already reads position from via
// nodeAt - rather than performing a fresh SignalK HTTP fetch of its own.
func TestCurrentVesselLocalLocation_UsesCachedSnapshotPosition(t *testing.T) {
	snapshot := snapshotWithSelfValues(map[string]any{
		"navigation.position": map[string]any{"latitude": -36.8, "longitude": 153.0},
	})
	origSnapshot := globalSignalKSnapshot
	globalSignalKSnapshot = snapshot
	t.Cleanup(func() { globalSignalKSnapshot = origSnapshot })

	loc, ok := currentVesselLocalLocation()
	if !ok {
		t.Fatalf("expected a usable location from a seeded position")
	}
	// vesselLocalLocation builds a fresh time.FixedZone per call
	// (weather_tide.go), so two locations for the same offset are never the
	// same pointer - compare the offset itself, the idiom
	// TestVesselLocalLocation_UsesLongitudeOffset (weather_tide_test.go)
	// already uses.
	_, gotOffset := time.Now().In(loc).Zone()
	if gotOffset != 10*3600 {
		t.Fatalf("expected UTC+10 offset (longitude 153.0), got %d seconds", gotOffset)
	}
}

// TestCurrentVesselLocalLocation_NoPositionReportsNotOK covers a fresh
// process (or a fresh restart) before the delta stream has ever carried a
// navigation.position update for self.
func TestCurrentVesselLocalLocation_NoPositionReportsNotOK(t *testing.T) {
	origSnapshot := globalSignalKSnapshot
	globalSignalKSnapshot = newSignalKSnapshot()
	t.Cleanup(func() { globalSignalKSnapshot = origSnapshot })

	if _, ok := currentVesselLocalLocation(); ok {
		t.Fatalf("expected no usable location when navigation.position has never been seen")
	}
}

// TestCurrentVesselLocalLocation_SentinelPositionReportsNotOK covers
// hasUsableVesselPosition's own rejected sentinel pair (weather_providers.go):
// -1,-1 is a syntactically valid lat/lon but never a real fix, and must not
// be handed to vesselLocalLocation as if it were.
func TestCurrentVesselLocalLocation_SentinelPositionReportsNotOK(t *testing.T) {
	snapshot := snapshotWithSelfValues(map[string]any{
		"navigation.position": map[string]any{"latitude": -1.0, "longitude": -1.0},
	})
	origSnapshot := globalSignalKSnapshot
	globalSignalKSnapshot = snapshot
	t.Cleanup(func() { globalSignalKSnapshot = origSnapshot })

	if _, ok := currentVesselLocalLocation(); ok {
		t.Fatalf("expected the -1,-1 unset-position sentinel to be rejected, not treated as a real fix")
	}
}

func TestCachedMaxGustKtsFor_FreshResultPassesThrough(t *testing.T) {
	resetTelemetryInfluxSlot(t)

	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	globalTelemetryInfluxSlot.set(telemetryInfluxResult{
		gustKts:   map[string]float64{"10m": 8.0, "30m": 9.5, "1h": 11.0, "24h": 15.0},
		fetchedAt: now,
	})

	// Just inside the staleness window (2 tick intervals old).
	got := cachedMaxGustKtsFor(gustWindowLadder, now.Add(2*telemetryInfluxRefreshInterval))

	want := map[string]float64{"10m": 8.0, "30m": 9.5, "1h": 11.0, "24h": 15.0}
	for window, wantValue := range want {
		if got[window] != wantValue {
			t.Fatalf("window %q: got %v, want %v (full result %+v)", window, got[window], wantValue, got)
		}
	}
}

func TestCachedMaxGustKtsFor_StaleResultReportsMinusOne(t *testing.T) {
	resetTelemetryInfluxSlot(t)

	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	globalTelemetryInfluxSlot.set(telemetryInfluxResult{
		gustKts:   map[string]float64{"10m": 8.0, "30m": 9.5, "1h": 11.0, "24h": 15.0},
		fetchedAt: now,
	})

	// Just past the staleness window (> 3 tick intervals old).
	got := cachedMaxGustKtsFor(gustWindowLadder, now.Add(telemetryInfluxStaleAfter+time.Second))

	for _, window := range gustWindowLadder {
		if got[window] != -1 {
			t.Fatalf("window %q: expected stale sentinel -1, got %v (full result %+v)", window, got[window], got)
		}
	}
}

// A slot that has never been populated (process just started, ticker hasn't
// run yet) must report unavailable, not a zero-valued 0.0 that could be
// mistaken for a real dead-calm reading.
func TestCachedMaxGustKtsFor_NeverFetchedReportsMinusOne(t *testing.T) {
	resetTelemetryInfluxSlot(t)

	got := cachedMaxGustKtsFor(gustWindowLadder, time.Now())

	for _, window := range gustWindowLadder {
		if got[window] != -1 {
			t.Fatalf("window %q: expected -1 before the first refresh, got %v", window, got[window])
		}
	}
}

func TestCachedInfluxSolarOverride_FreshResultPassesThrough(t *testing.T) {
	resetTelemetryInfluxSlot(t)

	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	trend := []solarTrendPoint{{Time: now.Add(-time.Hour), TotalW: 640}}
	globalTelemetryInfluxSlot.set(telemetryInfluxResult{
		solarTodayKWh:     3.2,
		solarYesterdayKWh: 5.9,
		solarPeakTodayW:   910,
		solarTrend24h:     trend,
		fetchedAt:         now,
	})

	state := cachedInfluxSolarOverride(solarStateData{}, now.Add(telemetryInfluxRefreshInterval))

	if state.TodayKWh != 3.2 || state.YesterdayKWh != 5.9 || state.PeakTodayW != 910 {
		t.Fatalf("expected fresh cached values to pass through, got %+v", state)
	}
	if len(state.Trend24hTotal) != 1 || state.Trend24hTotal[0].TotalW != 640 {
		t.Fatalf("expected the cached trend to pass through, got %+v", state.Trend24hTotal)
	}
}

func TestCachedInfluxSolarOverride_StaleResultReportsSentinels(t *testing.T) {
	resetTelemetryInfluxSlot(t)

	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	globalTelemetryInfluxSlot.set(telemetryInfluxResult{
		solarTodayKWh:     3.2,
		solarYesterdayKWh: 5.9,
		solarPeakTodayW:   910,
		solarTrend24h:     []solarTrendPoint{{Time: now, TotalW: 640}},
		fetchedAt:         now,
	})

	// Pre-populate with values that must be overwritten, proving this is a
	// wholesale override on staleness, not a merge that leaves old good
	// numbers in place.
	input := solarStateData{TodayKWh: 99, YesterdayKWh: 99, PeakTodayW: 99, Trend24hTotal: []solarTrendPoint{{TotalW: 99}}}
	state := cachedInfluxSolarOverride(input, now.Add(telemetryInfluxStaleAfter+time.Second))

	if state.TodayKWh != -1 || state.YesterdayKWh != -1 || state.PeakTodayW != -1 {
		t.Fatalf("expected stale sentinels -1/-1/-1, got %+v", state)
	}
	if state.Trend24hTotal != nil {
		t.Fatalf("expected stale trend to be nil, got %+v", state.Trend24hTotal)
	}
}

// startTelemetryInfluxTicker must refresh once before its first tick (30s
// away), not leave callers waiting behind a zero-value slot until then. No
// Influx is configured in this test process, so the real query functions
// the ticker wires in (newTelemetryInfluxFetcher's defaults) return their
// sentinels immediately via newInfluxClient's ok=false rather than touching
// the network -- this exercises the actual production entrypoint, not a
// stand-in.
func TestStartTelemetryInfluxTicker_RefreshesImmediatelyAtStartup(t *testing.T) {
	resetTelemetryInfluxSlot(t)
	t.Setenv("SETTINGS_FILE", filepath.Join(t.TempDir(), "missing-settings.yaml"))

	before := globalTelemetryInfluxSlot.get()
	if !before.fetchedAt.IsZero() {
		t.Fatalf("expected a zero-value slot before the ticker starts, got %+v", before)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		startTelemetryInfluxTicker(ctx)
		close(done)
	}()
	// A bare "defer cancel()" only signals the goroutine to stop; it does not
	// wait for it to actually exit, so the goroutine could still be mid-tick
	// when the next test starts. That mattered little when refresh() only
	// touched solarStats' own low-contention lock, but its production
	// vesselLocalLocation default (currentVesselLocalLocation) now also
	// RLocks globalSignalKSnapshot on every tick - a lock nearly every other
	// test in this package also takes - so an un-synchronized leak here risks
	// perturbing whichever test runs next. Waiting for done keeps this test's
	// background goroutine fully wound down before it returns.
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Errorf("startTelemetryInfluxTicker did not exit within 2s of cancellation")
		}
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !globalTelemetryInfluxSlot.get().fetchedAt.IsZero() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected startTelemetryInfluxTicker to refresh once immediately at startup; slot was still zero-valued after 2s")
}
