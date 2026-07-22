package main

import (
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
	windGustHistory = newTelemetryRingBuffer(telemetryHistoryCapacity)
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
	windGustHistory = newTelemetryRingBuffer(telemetryHistoryCapacity)

	got := inMemoryMaxWindGustKts("10m")
	if got != -1 {
		t.Fatalf("expected sentinel -1 for no samples, got %v", got)
	}
}

// TestInMemoryMaxWindGustKts_DoesNotReapplyMetersPerSecondConversion guards
// against a unit-conversion regression: state.WindSpeedApparentKts (what
// sampleTracks records) is already in knots, unlike raw Influx data which is
// in m/s and requires queryInfluxMaxWindGustKts's *metersPerSecondToKnots
// step. Recording a realistic knots value and reading it back must return
// the same value, not one multiplied by metersPerSecondToKnots (~1.94x)
// again.
func TestInMemoryMaxWindGustKts_DoesNotReapplyMetersPerSecondConversion(t *testing.T) {
	windGustHistory = newTelemetryRingBuffer(telemetryHistoryCapacity)
	now := time.Now().UTC()
	windGustHistory.record(20.0, now)

	got := inMemoryMaxWindGustKts("10m")
	if got != 20.0 {
		t.Fatalf("expected 20.0 kts unchanged, got %v (metersPerSecondToKnots re-applied?)", got)
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
