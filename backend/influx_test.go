package main

import (
	"testing"
	"time"
)

func depthPoint(minutesAgo int, depthM float64) depthTrendPoint {
	return depthTrendPoint{
		Time:   time.Now().Add(-time.Duration(minutesAgo) * time.Minute),
		DepthM: depthM,
	}
}

func TestFindLastTideTurningPointFallingAfterHighTide(t *testing.T) {
	points := []depthTrendPoint{
		depthPoint(60, 4.8),
		depthPoint(50, 5.0),
		depthPoint(40, 5.3), // high tide
		depthPoint(30, 5.2),
		depthPoint(20, 5.0),
		depthPoint(10, 4.9),
		depthPoint(0, 4.85),
	}

	turn, ok := findLastTideTurningPoint(points)
	if !ok {
		t.Fatalf("expected a turning point to be found")
	}
	if !turn.IsHigh {
		t.Fatalf("expected the turning point to be a high tide")
	}
	if turn.DepthM != 5.3 {
		t.Fatalf("expected turning point depth 5.3, got %v", turn.DepthM)
	}
	if !turn.Time.Equal(points[2].Time) {
		t.Fatalf("expected turning point time to match the peak sample")
	}
}

func TestFindLastTideTurningPointRisingAfterLowTide(t *testing.T) {
	points := []depthTrendPoint{
		depthPoint(60, 5.2),
		depthPoint(50, 4.9),
		depthPoint(40, 4.6), // low tide
		depthPoint(30, 4.7),
		depthPoint(20, 4.9),
		depthPoint(10, 5.1),
		depthPoint(0, 5.2),
	}

	turn, ok := findLastTideTurningPoint(points)
	if !ok {
		t.Fatalf("expected a turning point to be found")
	}
	if turn.IsHigh {
		t.Fatalf("expected the turning point to be a low tide")
	}
	if turn.DepthM != 4.6 {
		t.Fatalf("expected turning point depth 4.6, got %v", turn.DepthM)
	}
}

func TestFindLastTideTurningPointReturnsMostRecentReversal(t *testing.T) {
	points := []depthTrendPoint{
		depthPoint(120, 4.0),
		depthPoint(100, 5.0), // high tide
		depthPoint(80, 4.0),  // low tide
		depthPoint(60, 5.0),  // most recent high tide
		depthPoint(40, 4.6),
		depthPoint(20, 4.4),
		depthPoint(0, 4.2),
	}

	turn, ok := findLastTideTurningPoint(points)
	if !ok {
		t.Fatalf("expected a turning point to be found")
	}
	if !turn.IsHigh {
		t.Fatalf("expected the most recent turning point to be a high tide")
	}
	if !turn.Time.Equal(points[3].Time) {
		t.Fatalf("expected turning point to be the most recent reversal, not an earlier one")
	}
}

func TestFindLastTideTurningPointIgnoresNoise(t *testing.T) {
	points := []depthTrendPoint{
		depthPoint(40, 4.50),
		depthPoint(30, 4.52), // sub-threshold wobble
		depthPoint(20, 4.49), // sub-threshold wobble
		depthPoint(10, 4.55),
		depthPoint(0, 4.60),
	}

	if _, ok := findLastTideTurningPoint(points); ok {
		t.Fatalf("expected no turning point for noise-only fluctuations")
	}
}

// TestFindLastTideTurningPointNoFalsePositiveFromAnchorNoise checks that
// sonar noise from a boat swinging at anchor (~0.2m variations) is NOT
// mistaken for a real tide turning point.  With the original 0.05m threshold
// the first ±0.12m swing would trigger a false IsHigh=true detection.
func TestFindLastTideTurningPointNoFalsePositiveFromAnchorNoise(t *testing.T) {
	points := []depthTrendPoint{
		depthPoint(150, 2.10),
		depthPoint(140, 2.22), // +0.12 from start
		depthPoint(130, 2.05), // −0.17 from peak — false high-tide at 0.05m threshold
		depthPoint(120, 2.18),
		depthPoint(110, 2.30),
		depthPoint(100, 2.08),
		depthPoint(90, 2.25),
		depthPoint(80, 2.15),
		depthPoint(70, 2.20),
		depthPoint(60, 2.10),
		depthPoint(50, 2.18),
		depthPoint(40, 2.05),
		depthPoint(30, 2.22),
		depthPoint(20, 2.12),
		depthPoint(10, 2.18),
		depthPoint(0, 2.20),
	}
	if _, ok := findLastTideTurningPoint(points); ok {
		t.Fatalf("expected no turning point for anchor-swing noise; false positive detected")
	}
}

func TestFindLastTideTurningPointNoReversal(t *testing.T) {
	points := []depthTrendPoint{
		depthPoint(40, 4.0),
		depthPoint(30, 4.3),
		depthPoint(20, 4.6),
		depthPoint(10, 4.9),
		depthPoint(0, 5.2),
	}

	if _, ok := findLastTideTurningPoint(points); ok {
		t.Fatalf("expected no turning point for a monotonic series")
	}
}

func TestFindLastTideTurningPointTooFewPoints(t *testing.T) {
	points := []depthTrendPoint{
		depthPoint(10, 4.0),
		depthPoint(0, 4.5),
	}

	if _, ok := findLastTideTurningPoint(points); ok {
		t.Fatalf("expected no turning point with fewer than 3 points")
	}
}
