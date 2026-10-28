package main

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

// A direction in the log is radians, and its history is a vector mean in
// degrees: 350 and 10 degrees average to north, not to 180, and a bucket whose
// samples cancel out has no mean at all (ADR 0221).
func TestExecuteGetPathHistory_AnAngleIsAVectorMeanInDegreesWithASpreadAndNoMinMax(t *testing.T) {
	now := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	t0 := now.Add(-2 * time.Hour)
	// Bucket one: 350 and 10 degrees. Bucket two: due east and due west, which
	// cancel.
	stat := func(_ context.Context, path, _ string, _, _ time.Time, _, fn string) ([]telemetryPoint, error) {
		if path != "environment.wind.directionTrue" {
			return nil, nil
		}
		var vals []float64
		switch fn {
		case "sin_mean":
			vals = []float64{(math.Sin(rad(350)) + math.Sin(rad(10))) / 2, 0}
		case "cos_mean":
			vals = []float64{(math.Cos(rad(350)) + math.Cos(rad(10))) / 2, 0}
		default:
			return nil, nil
		}
		return []telemetryPoint{{Timestamp: t0, Value: vals[0]}, {Timestamp: t0.Add(time.Hour), Value: vals[1]}}, nil
	}
	deps := assistantToolDeps{
		now:                        func() time.Time { return now },
		vesselState:                func() (vesselStateData, error) { return vesselStateData{}, errNoVesselState },
		influxPathHistoryStat:      stat,
		influxPathHistoryFirstLast: stubInfluxPathHistoryFirstLast(t0, t0.Add(time.Hour), true),
		siUnit:                     func(path string) string { return map[string]string{"environment.wind.directionTrue": "rad"}[path] },
	}
	raw, err := deps.execute(context.Background(), "get_path_history", json.RawMessage(`{"path":"environment.wind.directionTrue","hours_back":3}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var result assistantGetPathHistoryResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatal(err)
	}
	if result.Unit != "°T" {
		t.Errorf("unit = %q, want °T", result.Unit)
	}
	if len(result.Buckets) != 2 {
		t.Fatalf("buckets = %+v", result.Buckets)
	}
	b0 := result.Buckets[0]
	if b0.Mean == nil || (*b0.Mean > 1 && *b0.Mean < 359) {
		t.Errorf("bucket 0 mean = %v, want north, not a linear 180", b0.Mean)
	}
	if b0.Spread == nil || *b0.Spread < 5 || *b0.Spread > 20 {
		t.Errorf("bucket 0 spread = %v degrees, want about 10", b0.Spread)
	}
	if b0.Min != nil || b0.Max != nil || result.OverallMin != nil || result.OverallMax != nil {
		t.Error("an angle has no min or max")
	}
	if b1 := result.Buckets[1]; b1.Mean != nil {
		t.Errorf("bucket 1 cancels out and must have no mean, got %v", *b1.Mean)
	}
	if result.UndefinedBuckets != 1 || !strings.Contains(result.Note, "vector") || !strings.Contains(result.Note, "no mean direction") {
		t.Errorf("note %q, undefined %d: the result must say the average is a vector mean and that a bucket had no direction", result.Note, result.UndefinedBuckets)
	}
	if strings.Contains(raw, `"min"`) || strings.Contains(raw, `"max"`) {
		t.Errorf("no min or max key for an angle: %s", raw)
	}
}

// A path that is not an angle keeps its min, mean and max, whatever siUnit says.
func TestExecuteGetPathHistory_AScalarKeepsMinMeanMax(t *testing.T) {
	now := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	t0 := now.Add(-2 * time.Hour)
	series := map[string][]telemetryPoint{
		"min":  {{Timestamp: t0, Value: 1}},
		"mean": {{Timestamp: t0, Value: 2}},
		"max":  {{Timestamp: t0, Value: 3}},
	}
	deps := assistantToolDeps{
		now:                        func() time.Time { return now },
		vesselState:                func() (vesselStateData, error) { return vesselStateData{}, errNoVesselState },
		influxPathHistoryStat:      stubInfluxPathHistoryStat(series, nil),
		influxPathHistoryFirstLast: stubInfluxPathHistoryFirstLast(t0, t0, true),
		siUnit:                     func(string) string { return "m/s" },
	}
	raw, err := deps.execute(context.Background(), "get_path_history", json.RawMessage(`{"path":"environment.wind.speedTrue","hours_back":3}`))
	if err != nil {
		t.Fatal(err)
	}
	var result assistantGetPathHistoryResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatal(err)
	}
	if b := result.Buckets[0]; b.Min == nil || b.Mean == nil || b.Max == nil || result.Unit != "" {
		t.Errorf("bucket %+v unit %q", b, result.Unit)
	}
}
