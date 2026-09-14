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
		querySolarToday:     func(time.Time) float64 { todayCalled = true; return 4.5 },
		querySolarYesterday: func(time.Time) float64 { yesterdayCalled = true; return 6.1 },
		querySolarPeak:      func(time.Time) float64 { peakCalled = true; return 820 },
		querySolarTrend: func(time.Time) []solarTrendPoint {
			trendCalled = true
			return []solarTrendPoint{{Time: time.Now(), TotalW: 500}}
		},
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
	defer cancel()
	go startTelemetryInfluxTicker(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !globalTelemetryInfluxSlot.get().fetchedAt.IsZero() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected startTelemetryInfluxTicker to refresh once immediately at startup; slot was still zero-valued after 2s")
}
