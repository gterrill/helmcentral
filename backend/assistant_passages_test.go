package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// sogSeries builds 5-minute SOG points (in m/s) from knots values, starting
// at start. A NaN-free helper: a negative knots value skips that bucket, the
// way createEmpty:false leaves a gap.
func sogSeries(start time.Time, knots ...float64) []telemetryPoint {
	var pts []telemetryPoint
	for i, k := range knots {
		if k < 0 {
			continue
		}
		pts = append(pts, telemetryPoint{
			Timestamp: start.Add(time.Duration(i+1) * assistantPassagesBucket),
			Value:     k / metersPerSecondToKnots,
		})
	}
	return pts
}

func repeat(v float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func cat(parts ...[]float64) []float64 {
	var out []float64
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func TestDetectPassages(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		knots []float64
		want  int // passages found
	}{
		{"empty", nil, 0},
		{"all at anchor", repeat(0.2, 40), 0},
		{"one two-hour run", cat(repeat(0, 6), repeat(6, 24), repeat(0, 6)), 1},
		{"below threshold is not underway", repeat(2.9, 24), 0},
		{"at threshold counts", repeat(3.0, 6), 1},
		{"short run dropped", repeat(6, 2), 0},
		{"brief slow-down merges", cat(repeat(6, 12), repeat(1, 3), repeat(6, 12)), 1},
		{"long stop splits", cat(repeat(6, 12), repeat(0, 12), repeat(6, 12)), 2},
		{"missing data gap shorter than merge gap merges", cat(repeat(6, 12), repeat(-1, 3), repeat(6, 12)), 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := detectPassages(sogSeries(t0, tc.knots...), assistantPassagesBucket)
			if len(got) != tc.want {
				t.Fatalf("want %d passages, got %d: %+v", tc.want, len(got), got)
			}
		})
	}
}

func TestDetectPassages_FiguresAndOrder(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	knots := cat(repeat(0, 2), repeat(6, 12), repeat(0, 12), repeat(8, 12))
	got := detectPassages(sogSeries(t0, knots...), assistantPassagesBucket)
	if len(got) != 2 {
		t.Fatalf("want 2 passages, got %d", len(got))
	}
	if !got[0].Start.Before(got[1].Start) {
		t.Fatalf("detectPassages must return oldest first, got %v then %v", got[0].Start, got[1].Start)
	}
	p := got[0]
	if want := t0.Add(2 * assistantPassagesBucket); !p.Start.Equal(want) {
		t.Errorf("start: want %v got %v", want, p.Start)
	}
	if want := t0.Add(14 * assistantPassagesBucket); !p.End.Equal(want) {
		t.Errorf("end: want %v got %v", want, p.End)
	}
	if d := p.End.Sub(p.Start); d != time.Hour {
		t.Errorf("duration: want 1h got %v", d)
	}
	if p.MaxSOGKts < 5.99 || p.MaxSOGKts > 6.01 || p.MeanSOGKts < 5.99 || p.MeanSOGKts > 6.01 {
		t.Errorf("sog figures: %+v", p)
	}
	if p.DistanceNm < 5.9 || p.DistanceNm > 6.1 {
		t.Errorf("distance: want ~6 nm, got %v", p.DistanceNm)
	}
}

func TestExecuteListPassages(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	t0 := now.Add(-10 * 24 * time.Hour)
	knots := cat(repeat(6, 12), repeat(0, 12), repeat(6, 12), repeat(0, 12), repeat(6, 12))
	series := sogSeries(t0, knots...)

	var gotPath, gotEvery string
	var gotStart time.Time
	deps := assistantToolDeps{
		now: func() time.Time { return now },
		influxRange: func(path string, start, stop time.Time, every string) ([]telemetryPoint, error) {
			gotPath, gotEvery, gotStart = path, every, start
			return series, nil
		},
	}

	out, err := deps.execute(context.Background(), "list_passages", json.RawMessage(`{"limit":2}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotPath != "navigation.speedOverGround" || gotEvery != "5m" {
		t.Errorf("query: path %q every %q", gotPath, gotEvery)
	}
	if !gotStart.Equal(now.AddDate(0, 0, -assistantPassagesDefaultDaysBack)) {
		t.Errorf("default days_back not applied: %v", gotStart)
	}
	var res struct {
		Passages []struct {
			Start         string  `json:"start"`
			End           string  `json:"end"`
			DurationHours float64 `json:"duration_hours"`
			DistanceNm    float64 `json:"distance_nm"`
		} `json:"passages"`
		Found int `json:"found"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if res.Found != 3 || len(res.Passages) != 2 {
		t.Fatalf("want found 3 with 2 returned, got found %d returned %d", res.Found, len(res.Passages))
	}
	if res.Passages[0].Start < res.Passages[1].Start {
		t.Errorf("most recent must come first: %+v", res.Passages)
	}
	if _, err := time.Parse(time.RFC3339, res.Passages[0].Start); err != nil {
		t.Errorf("start not RFC3339: %v", err)
	}
}

// The range query does not group by source, so two receivers publishing SOG
// come back as two time-ordered blocks one after the other. They describe the
// same passage and must read as one, with its distance counted once.
func TestExecuteListPassages_TwoSOGSourcesAreOnePassage(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	t0 := now.Add(-2 * 24 * time.Hour)
	knots := cat(repeat(0, 6), repeat(6, 12), repeat(0, 6))
	series := append(sogSeries(t0, knots...), sogSeries(t0, knots...)...)

	deps := assistantToolDeps{
		now: func() time.Time { return now },
		influxRange: func(string, time.Time, time.Time, string) ([]telemetryPoint, error) {
			return series, nil
		},
	}
	out, err := deps.execute(context.Background(), "list_passages", nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var res struct {
		Passages []struct {
			DurationHours float64 `json:"duration_hours"`
			DistanceNm    float64 `json:"distance_nm"`
		} `json:"passages"`
		Found int `json:"found"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if res.Found != 1 {
		t.Fatalf("want 1 passage, got %d: %s", res.Found, out)
	}
	if p := res.Passages[0]; p.DurationHours != 1 || p.DistanceNm < 5.9 || p.DistanceNm > 6.1 {
		t.Errorf("want 1 h and ~6 nm, got %+v", p)
	}
}

func TestExecuteListPassages_CapsDaysBack(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	var gotStart time.Time
	deps := assistantToolDeps{
		now: func() time.Time { return now },
		influxRange: func(path string, start, stop time.Time, every string) ([]telemetryPoint, error) {
			gotStart = start
			return nil, nil
		},
	}
	if _, err := deps.execute(context.Background(), "list_passages", json.RawMessage(`{"days_back":500}`)); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !gotStart.Equal(now.AddDate(0, 0, -assistantPassagesMaxDaysBack)) {
		t.Errorf("days_back not capped: %v", gotStart)
	}
}

func TestExecuteListPassages_NoSOGDataSaysSo(t *testing.T) {
	deps := assistantToolDeps{
		now: time.Now,
		influxRange: func(string, time.Time, time.Time, string) ([]telemetryPoint, error) {
			return nil, nil
		},
	}
	out, err := deps.execute(context.Background(), "list_passages", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "no_speed_data") {
		t.Errorf("expected an explicit no-data result, got %s", out)
	}
}

func TestExecuteListPassages_NoPassagesButDataIsNotNoData(t *testing.T) {
	deps := assistantToolDeps{
		now: func() time.Time { return time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC) },
		influxRange: func(string, time.Time, time.Time, string) ([]telemetryPoint, error) {
			return sogSeries(time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC), repeat(0, 10)...), nil
		},
	}
	out, err := deps.execute(context.Background(), "list_passages", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(out, "no_speed_data") || !strings.Contains(out, `"found":0`) {
		t.Errorf("unexpected result %s", out)
	}
}

func TestExecuteListPassages_InfluxErrorFailsFast(t *testing.T) {
	deps := assistantToolDeps{
		now: time.Now,
		influxRange: func(string, time.Time, time.Time, string) ([]telemetryPoint, error) {
			return nil, errors.New("influxdb is not configured")
		},
	}
	_, err := deps.execute(context.Background(), "list_passages", json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "influxdb is not configured") {
		t.Fatalf("expected the influx error, got %v", err)
	}
}
