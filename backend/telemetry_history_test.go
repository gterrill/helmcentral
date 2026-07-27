package main

import (
	"sort"
	"testing"
	"time"
)

func TestTelemetryRingBuffer_RecordAndSince(t *testing.T) {
	buf := newTelemetryRingBuffer(3)
	now := time.Now().UTC()

	buf.record(1.0, now.Add(-3*time.Minute))
	buf.record(2.0, now.Add(-2*time.Minute))
	buf.record(3.0, now.Add(-1*time.Minute))

	points := buf.since(now.Add(-5 * time.Minute))
	if len(points) != 3 {
		t.Fatalf("expected 3 points, got %d", len(points))
	}
	if points[0].Value != 1.0 || points[2].Value != 3.0 {
		t.Fatalf("expected chronological order, got %+v", points)
	}
}

func TestTelemetryRingBuffer_SinceFiltersOldPoints(t *testing.T) {
	buf := newTelemetryRingBuffer(5)
	now := time.Now().UTC()

	buf.record(1.0, now.Add(-10*time.Minute))
	buf.record(2.0, now.Add(-1*time.Minute))

	points := buf.since(now.Add(-5 * time.Minute))
	if len(points) != 1 {
		t.Fatalf("expected 1 point after cutoff, got %d", len(points))
	}
	if points[0].Value != 2.0 {
		t.Fatalf("expected the recent point, got %+v", points[0])
	}
}

func TestTelemetryRingBuffer_WrapsAroundCapacity(t *testing.T) {
	buf := newTelemetryRingBuffer(3)
	now := time.Now().UTC()

	buf.record(1.0, now.Add(-4*time.Minute))
	buf.record(2.0, now.Add(-3*time.Minute))
	buf.record(3.0, now.Add(-2*time.Minute))
	buf.record(4.0, now.Add(-1*time.Minute)) // overwrites the oldest sample (1.0)

	points := buf.since(time.Time{})
	if len(points) != 3 {
		t.Fatalf("expected buffer capped at capacity 3, got %d", len(points))
	}
	if points[0].Value != 2.0 {
		t.Fatalf("expected oldest surviving point to be 2.0, got %v", points[0].Value)
	}
	if points[2].Value != 4.0 {
		t.Fatalf("expected newest point to be 4.0, got %v", points[2].Value)
	}
}

func TestInMemoryMaxWindGustKts_ReturnsMaxInWindow(t *testing.T) {
	windGustHistory = newTelemetryRingBuffer(windGustHistoryCapacity)
	now := time.Now().UTC()
	windGustHistory.record(12.0, now.Add(-5*time.Minute))
	windGustHistory.record(18.5, now.Add(-2*time.Minute))
	windGustHistory.record(9.0, now.Add(-1*time.Minute))

	got := inMemoryMaxWindGustKts("10m")
	if got != 18.5 {
		t.Fatalf("expected max gust 18.5, got %v", got)
	}
}

func TestInMemoryMaxWindGustKts_NoSamplesReturnsSentinel(t *testing.T) {
	windGustHistory = newTelemetryRingBuffer(windGustHistoryCapacity)

	got := inMemoryMaxWindGustKts("10m")
	if got != -1 {
		t.Fatalf("expected sentinel -1 for no samples, got %v", got)
	}
}

// TestInMemoryMaxWindGustKts_DoesNotReapplyMetersPerSecondConversion guards
// against a unit-conversion regression: state.WindSpeedApparentKts (what
// sampleTracks records) is already in knots, unlike raw Influx data which is
// in m/s and requires queryInfluxMaxWindGustKtsForWindow's *metersPerSecondToKnots
// step. Recording a realistic knots value and reading it back must return
// the same value, not one multiplied by metersPerSecondToKnots (~1.94x)
// again.
func TestInMemoryMaxWindGustKts_DoesNotReapplyMetersPerSecondConversion(t *testing.T) {
	windGustHistory = newTelemetryRingBuffer(windGustHistoryCapacity)
	now := time.Now().UTC()
	windGustHistory.record(20.0, now)

	got := inMemoryMaxWindGustKts("10m")
	if got != 20.0 {
		t.Fatalf("expected 20.0 kts unchanged, got %v (metersPerSecondToKnots re-applied?)", got)
	}
}

// TestInMemoryMaxWindGustKtsFor_ReturnsPerWindowMap covers the thin wrapper
// over inMemoryMaxWindGustKts that computes every window in a slice at once
// (used by vesselState to fill gustWindowLadder). With only a sample inside
// the shortest window recorded, the short window should report it while
// longer windows containing the same sample should too (a longer window
// always contains everything a shorter one does), and any window with no
// matching samples at all should fall back to the -1 sentinel.
func TestInMemoryMaxWindGustKtsFor_ReturnsPerWindowMap(t *testing.T) {
	windGustHistory = newTelemetryRingBuffer(windGustHistoryCapacity)
	now := time.Now().UTC()
	windGustHistory.record(15.5, now.Add(-5*time.Minute))

	got := inMemoryMaxWindGustKtsFor([]string{"10m", "30m", "1h", "24h"})

	if len(got) != 4 {
		t.Fatalf("expected 4 entries, got %d: %+v", len(got), got)
	}
	for _, w := range []string{"10m", "30m", "1h", "24h"} {
		if got[w] != 15.5 {
			t.Fatalf("expected window %q to report 15.5 (sample is within all windows), got %v", w, got[w])
		}
	}
}

func TestInMemoryMaxWindGustKtsFor_NoSamplesReturnsSentinelPerWindow(t *testing.T) {
	windGustHistory = newTelemetryRingBuffer(windGustHistoryCapacity)

	got := inMemoryMaxWindGustKtsFor([]string{"10m", "30m", "1h", "24h"})

	for _, w := range []string{"10m", "30m", "1h", "24h"} {
		if got[w] != -1 {
			t.Fatalf("expected sentinel -1 for window %q with no samples, got %v", w, got[w])
		}
	}
}

// TestInMemoryMaxWindGustKtsFor_MatchesPerWindowCalls_RandomValues proves the
// single-pass ladder computation agrees with calling the single-window
// inMemoryMaxWindGustKts once per window, for a random-valued input
// sequence.
func TestInMemoryMaxWindGustKtsFor_MatchesPerWindowCalls_RandomValues(t *testing.T) {
	seed := func() {
		windGustHistory = newTelemetryRingBuffer(windGustHistoryCapacity)
		now := time.Now().UTC()
		samples := []struct {
			value float64
			age   time.Duration
		}{
			{12.3, 9 * time.Minute},
			{5.6, 25 * time.Minute},
			{30.1, 50 * time.Minute},
			{9.9, 8 * time.Hour},
			{18.4, 3 * time.Minute},
			{2.2, 40 * time.Minute},
			{25.0, 20 * time.Hour},
			{14.7, 55 * time.Minute},
		}
		// Insert oldest-first: record() must be called with non-decreasing
		// timestamps, mirroring how the real poller records samples as time
		// actually advances. inMemoryMaxWindGustKtsFor walks insertion order
		// backward and relies on that equaling chronological order (exactly
		// as since()'s own wraparound logic does).
		sort.Slice(samples, func(i, j int) bool { return samples[i].age > samples[j].age })
		for _, s := range samples {
			windGustHistory.record(s.value, now.Add(-s.age))
		}
	}

	windows := []string{"10m", "30m", "1h", "24h"}

	seed()
	got := inMemoryMaxWindGustKtsFor(windows)

	seed()
	want := make(map[string]float64, len(windows))
	for _, w := range windows {
		want[w] = inMemoryMaxWindGustKts(w)
	}

	for _, w := range windows {
		if got[w] != want[w] {
			t.Fatalf("window %q: batched=%v per-window=%v (random-valued sequence)", w, got[w], want[w])
		}
	}
}

// TestInMemoryMaxWindGustKtsFor_MatchesPerWindowCalls_MonotonicValues covers
// the same equivalence for a strictly-decreasing sequence (oldest highest,
// newest lowest), which exercises the running-max logic differently than
// random data (the max for every window is set by the oldest sample within
// it, not some interior sample).
func TestInMemoryMaxWindGustKtsFor_MatchesPerWindowCalls_MonotonicValues(t *testing.T) {
	seed := func() {
		windGustHistory = newTelemetryRingBuffer(windGustHistoryCapacity)
		now := time.Now().UTC()
		// Oldest (largest age) has the highest value, decreasing toward now.
		samples := []struct {
			value float64
			age   time.Duration
		}{
			{40.0, 23 * time.Hour},
			{35.0, 50 * time.Minute},
			{30.0, 40 * time.Minute},
			{25.0, 25 * time.Minute},
			{20.0, 9 * time.Minute},
			{15.0, 4 * time.Minute},
			{10.0, 1 * time.Minute},
		}
		for _, s := range samples {
			windGustHistory.record(s.value, now.Add(-s.age))
		}
	}

	windows := []string{"10m", "30m", "1h", "24h"}

	seed()
	got := inMemoryMaxWindGustKtsFor(windows)

	seed()
	want := make(map[string]float64, len(windows))
	for _, w := range windows {
		want[w] = inMemoryMaxWindGustKts(w)
	}

	for _, w := range windows {
		if got[w] != want[w] {
			t.Fatalf("window %q: batched=%v per-window=%v (monotonic-decreasing sequence)", w, got[w], want[w])
		}
	}
}

// TestInMemoryMaxWindGustKtsFor_UnpopulatedWindowStaysSentinel proves that,
// within a single call, a window shorter than every recorded sample reports
// the -1 sentinel while longer windows containing that same sample still
// report the correct value — i.e. sentinel handling is truly per-window, not
// contaminated by neighboring windows in the same pass.
func TestInMemoryMaxWindGustKtsFor_UnpopulatedWindowStaysSentinel(t *testing.T) {
	windGustHistory = newTelemetryRingBuffer(windGustHistoryCapacity)
	now := time.Now().UTC()
	windGustHistory.record(15.5, now.Add(-5*time.Minute))

	got := inMemoryMaxWindGustKtsFor([]string{"1ms", "10m", "1h"})

	if got["1ms"] != -1 {
		t.Fatalf("expected window %q (shorter than the only sample) to be sentinel -1, got %v", "1ms", got["1ms"])
	}
	if got["10m"] != 15.5 {
		t.Fatalf("expected window %q to report 15.5, got %v", "10m", got["10m"])
	}
	if got["1h"] != 15.5 {
		t.Fatalf("expected window %q to report 15.5, got %v", "1h", got["1h"])
	}
}

// TestInMemoryMaxWindGustKtsFor_EmptyBufferReturnsSentinelForEveryWindow
// covers the empty-buffer case (count == 0, the backward walk never runs at
// all) for the full gust ladder.
func TestInMemoryMaxWindGustKtsFor_EmptyBufferReturnsSentinelForEveryWindow(t *testing.T) {
	windGustHistory = newTelemetryRingBuffer(windGustHistoryCapacity)

	got := inMemoryMaxWindGustKtsFor([]string{"10m", "30m", "1h", "24h"})

	for _, w := range []string{"10m", "30m", "1h", "24h"} {
		if got[w] != -1 {
			t.Fatalf("expected sentinel -1 for window %q on empty buffer, got %v", w, got[w])
		}
	}
}

// TestInMemoryMaxWindGustKtsFor_PartiallyFilledBufferComputesCorrectMaxima
// exercises the b.full == false branch (count = b.index) with a handful of
// records well under capacity.
func TestInMemoryMaxWindGustKtsFor_PartiallyFilledBufferComputesCorrectMaxima(t *testing.T) {
	windGustHistory = newTelemetryRingBuffer(windGustHistoryCapacity)
	now := time.Now().UTC()
	// Inserted oldest-first (non-decreasing timestamps across record()
	// calls), matching real poller usage and the insertion-order-equals-
	// chronological-order assumption inMemoryMaxWindGustKtsFor's backward
	// walk depends on.
	windGustHistory.record(31.0, now.Add(-45*time.Minute))
	windGustHistory.record(18.0, now.Add(-20*time.Minute))
	windGustHistory.record(10.0, now.Add(-8*time.Minute))
	windGustHistory.record(22.0, now.Add(-6*time.Minute))

	got := inMemoryMaxWindGustKtsFor([]string{"10m", "30m", "1h"})

	if got["10m"] != 22.0 {
		t.Fatalf("expected 10m max 22.0, got %v", got["10m"])
	}
	if got["30m"] != 22.0 {
		t.Fatalf("expected 30m max 22.0, got %v", got["30m"])
	}
	if got["1h"] != 31.0 {
		t.Fatalf("expected 1h max 31.0, got %v", got["1h"])
	}
}

// TestInMemoryMaxWindGustKtsFor_WrappedBufferOnlyReflectsSurvivingPoints
// exercises the b.full == true branch and the modular index arithmetic by
// temporarily swapping in a small ring buffer, recording more points than
// its capacity (forcing wrap-around and overwriting the oldest points), and
// verifying the overwritten points do not influence the result.
func TestInMemoryMaxWindGustKtsFor_WrappedBufferOnlyReflectsSurvivingPoints(t *testing.T) {
	original := windGustHistory
	defer func() { windGustHistory = original }()

	windGustHistory = newTelemetryRingBuffer(5)
	now := time.Now().UTC()
	// First two points (100.0, 90.0) will be overwritten by the 3 that
	// follow, since capacity is 5 and we record 5 total... so record 7 to
	// force wrap: capacity 5, record 7 -> oldest 2 (100.0, 90.0) overwritten.
	windGustHistory.record(100.0, now.Add(-50*time.Minute)) // overwritten
	windGustHistory.record(90.0, now.Add(-45*time.Minute))  // overwritten
	windGustHistory.record(12.0, now.Add(-40*time.Minute))
	windGustHistory.record(8.0, now.Add(-30*time.Minute))
	windGustHistory.record(20.0, now.Add(-20*time.Minute))
	windGustHistory.record(15.0, now.Add(-10*time.Minute))
	windGustHistory.record(5.0, now.Add(-1*time.Minute))

	got := inMemoryMaxWindGustKtsFor([]string{"1h"})

	// If the overwritten 100.0/90.0 samples leaked into the result, "1h"
	// would report 100.0 instead of the correct surviving max, 20.0.
	if got["1h"] != 20.0 {
		t.Fatalf("expected 1h max 20.0 reflecting only surviving points, got %v (overwritten points may have leaked in)", got["1h"])
	}
}

// TestInMemoryMaxWindGustKtsFor_SortsOutOfOrderLadderInternally proves the
// function sorts windows by duration internally rather than trusting input
// order for the boundary-crossing walk. Construction: one sample 5 minutes
// ago (20.0) and one 40 minutes ago (30.0). Requesting ["1h", "10m"]
// (longest first) must still yield 10m == 20.0 (only the 5-min-old sample
// qualifies) and 1h == 30.0 (both qualify). If the implementation walked the
// ring using the *input* order of windows instead of sorting by duration
// first, it would cross the "1h" cutoff before the "10m" cutoff and
// incorrectly snapshot the running max (30.0) for "10m" too.
func TestInMemoryMaxWindGustKtsFor_SortsOutOfOrderLadderInternally(t *testing.T) {
	windGustHistory = newTelemetryRingBuffer(windGustHistoryCapacity)
	now := time.Now().UTC()
	windGustHistory.record(30.0, now.Add(-40*time.Minute))
	windGustHistory.record(20.0, now.Add(-5*time.Minute))

	got := inMemoryMaxWindGustKtsFor([]string{"1h", "10m"})

	if got["10m"] != 20.0 {
		t.Fatalf("expected 10m == 20.0 (only the 5-min-ago sample is within 10 minutes), got %v", got["10m"])
	}
	if got["1h"] != 30.0 {
		t.Fatalf("expected 1h == 30.0 (both samples are within 1 hour), got %v", got["1h"])
	}
}

func BenchmarkInMemoryMaxWindGustKtsFor(b *testing.B) {
	windGustHistory = newTelemetryRingBuffer(windGustHistoryCapacity)
	now := time.Now().UTC()
	for i := 0; i < windGustHistoryCapacity; i++ {
		windGustHistory.record(float64(i%40), now.Add(-time.Duration(windGustHistoryCapacity-i)*5*time.Second))
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inMemoryMaxWindGustKtsFor(gustWindowLadder)
	}
}

func TestInMemoryDepthTrend_BucketsIntoFiveMinuteMeans(t *testing.T) {
	depthHistory = newTelemetryRingBuffer(telemetryHistoryCapacity)
	base := time.Now().UTC().Add(-20 * time.Minute).Truncate(5 * time.Minute)

	depthHistory.record(4.0, base)
	depthHistory.record(4.2, base.Add(1*time.Minute))
	depthHistory.record(5.0, base.Add(5*time.Minute))
	depthHistory.record(5.2, base.Add(6*time.Minute))

	points := inMemoryDepthTrend("1h")
	if len(points) != 2 {
		t.Fatalf("expected 2 buckets, got %d: %+v", len(points), points)
	}
	if points[0].DepthM != 4.1 {
		t.Fatalf("expected first bucket mean 4.1, got %v", points[0].DepthM)
	}
	if points[1].DepthM != 5.1 {
		t.Fatalf("expected second bucket mean 5.1, got %v", points[1].DepthM)
	}
}

func TestInMemoryDepthTrend_NoSamplesReturnsNil(t *testing.T) {
	depthHistory = newTelemetryRingBuffer(telemetryHistoryCapacity)

	points := inMemoryDepthTrend("3h")
	if points != nil {
		t.Fatalf("expected nil for no samples, got %+v", points)
	}
}

func TestInMemoryDepthTrend_ComposesWithFindLastTideTurningPoint(t *testing.T) {
	depthHistory = newTelemetryRingBuffer(telemetryHistoryCapacity)
	base := time.Now().UTC().Add(-90 * time.Minute)

	depths := []float64{4.8, 5.0, 5.3, 5.2, 5.0, 4.9, 4.85}
	for i, d := range depths {
		depthHistory.record(d, base.Add(time.Duration(i*5)*time.Minute))
	}

	points := inMemoryDepthTrend("3h")
	turn, ok := findLastTideTurningPoint(points)
	if !ok {
		t.Fatalf("expected a turning point to be found from in-memory depth trend")
	}
	if !turn.IsHigh {
		t.Fatalf("expected the turning point to be a high tide")
	}
}
