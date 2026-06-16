package main

import (
	"math"
	"os"
	"testing"
	"time"
)

func TestParseBomTidesTable(t *testing.T) {
	html, err := os.ReadFile("testdata/bom_tides_table_sample.html")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	extremes, err := parseBomTidesTable(string(html))
	if err != nil {
		t.Fatalf("parseBomTidesTable returned error: %v", err)
	}

	if len(extremes) != 31 {
		t.Fatalf("expected 31 extremes, got %d", len(extremes))
	}

	first := extremes[0]
	wantTime, err := time.Parse(time.RFC3339, "2026-06-15T17:12:00Z")
	if err != nil {
		t.Fatalf("failed to parse expected time: %v", err)
	}
	if !first.Time.Equal(wantTime) {
		t.Errorf("expected first extreme time %v, got %v", wantTime, first.Time)
	}
	if first.High {
		t.Errorf("expected first extreme to be a low tide")
	}
	if first.HeightM != 0.22 {
		t.Errorf("expected first extreme height 0.22, got %v", first.HeightM)
	}

	for i := 1; i < len(extremes); i++ {
		if extremes[i].Time.Before(extremes[i-1].Time) {
			t.Errorf("extremes not sorted by time at index %d", i)
		}
	}
}

func TestInterpolateTideNowBetweenExtremes(t *testing.T) {
	base := time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC)
	extremes := []tideExtremePoint{
		{Time: base, HeightM: 0.2, High: false},
		{Time: base.Add(6 * time.Hour), HeightM: 1.8, High: true},
	}

	height, direction := interpolateTideNow(extremes, base.Add(3*time.Hour))
	if direction != "Rising" {
		t.Errorf("expected Rising direction, got %s", direction)
	}

	want := (0.2 + 1.8) / 2
	if math.Abs(height-want) > 0.01 {
		t.Errorf("expected height ~%.2f at the midpoint, got %.2f", want, height)
	}
}

func TestInterpolateTideNowBeforeFirstExtreme(t *testing.T) {
	base := time.Date(2026, 6, 16, 6, 0, 0, 0, time.UTC)
	extremes := []tideExtremePoint{
		{Time: base, HeightM: 1.8, High: true},
		{Time: base.Add(6 * time.Hour), HeightM: 0.2, High: false},
	}

	height, direction := interpolateTideNow(extremes, base.Add(-time.Hour))
	if direction != "Rising" {
		t.Errorf("expected Rising direction before first (high) extreme, got %s", direction)
	}
	if height != 1.8 {
		t.Errorf("expected height to match the upcoming extreme (1.8), got %v", height)
	}
}
