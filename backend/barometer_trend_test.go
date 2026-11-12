package main

import (
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func withBarometerHistory(t *testing.T) {
	t.Helper()
	orig := barometerHistory
	barometerHistory = newTelemetryRingBuffer(4096)
	t.Cleanup(func() { barometerHistory = orig })
}

func TestBuildBarometerTrend_BucketsIntoFiveMinuteMeansInPascals(t *testing.T) {
	withBarometerHistory(t)
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	base := now.Add(-35 * time.Minute).Truncate(5 * time.Minute)

	barometerHistory.record(101000, base)
	barometerHistory.record(101100, base.Add(1*time.Minute))
	barometerHistory.record(101300, base.Add(5*time.Minute))
	barometerHistory.record(101500, base.Add(30*time.Minute))

	got := buildBarometerTrend(now)
	if len(got.Points) != 3 {
		t.Fatalf("want 3 buckets, got %d: %+v", len(got.Points), got.Points)
	}
	if got.Points[0].PressurePa != 101050 || got.Points[1].PressurePa != 101300 || got.Points[2].PressurePa != 101500 {
		t.Fatalf("unexpected bucket means: %+v", got.Points)
	}
	if got.CurrentPa == nil || *got.CurrentPa != 101500 {
		t.Fatalf("want current 101500, got %v", got.CurrentPa)
	}
	// 30 minutes of span reaches trendMinimumSpan: last minus first raw sample.
	if got.ChangePa == nil || math.Abs(*got.ChangePa-500) > 1e-9 {
		t.Fatalf("want change 500 Pa, got %v", got.ChangePa)
	}
	if got.ChangeSpanS == nil || *got.ChangeSpanS != 1800 {
		t.Fatalf("want a 1800 s span, got %v", got.ChangeSpanS)
	}
}

func TestBuildBarometerTrend_IgnoresSamplesOlderThanThreeHours(t *testing.T) {
	withBarometerHistory(t)
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	barometerHistory.record(99000, now.Add(-4*time.Hour))
	barometerHistory.record(101000, now.Add(-2*time.Minute))

	got := buildBarometerTrend(now)
	if len(got.Points) != 1 || got.Points[0].PressurePa != 101000 {
		t.Fatalf("want only the recent bucket, got %+v", got.Points)
	}
	if got.ChangePa != nil || got.ChangeSpanS != nil {
		t.Fatalf("one sample cannot give a change, got %v %v", got.ChangePa, got.ChangeSpanS)
	}
}

func TestBuildBarometerTrend_NoSamplesIsExplicitlyEmpty(t *testing.T) {
	withBarometerHistory(t)
	got := buildBarometerTrend(time.Now().UTC())
	if got.Points == nil || len(got.Points) != 0 {
		t.Fatalf("want an empty, non-nil list, got %#v", got.Points)
	}
	if got.CurrentPa != nil || got.ChangePa != nil || got.ChangeSpanS != nil {
		t.Fatalf("want null current, change and span, got %v %v %v", got.CurrentPa, got.ChangePa, got.ChangeSpanS)
	}
}

func TestBuildBarometerTrend_StaleBarometerIsEmpty(t *testing.T) {
	withBarometerHistory(t)
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	barometerHistory.record(101000, now.Add(-90*time.Minute))
	barometerHistory.record(101200, now.Add(-barometerMaxAge-time.Minute))

	got := buildBarometerTrend(now)
	if len(got.Points) != 0 || got.CurrentPa != nil || got.ChangePa != nil {
		t.Fatalf("a dead barometer must not show its last reading, got %+v", got)
	}
}

func TestBarometerTrendRoute(t *testing.T) {
	withBarometerHistory(t)
	var found bool
	for _, r := range vesselStateRoutes() {
		if r.Path == "/api/barometer-trend" && r.Method == http.MethodGet && r.Tier == tierRead {
			found = true
		}
	}
	if !found {
		t.Fatal("GET /api/barometer-trend (tierRead) is not registered")
	}

	e := echo.New()
	rec := httptest.NewRecorder()
	c := e.NewContext(httptest.NewRequest(http.MethodGet, "/api/barometer-trend", nil), rec)
	if err := barometerTrend(c); err != nil {
		t.Fatal(err)
	}
	want := `{"points":[],"current_pa":null,"change_pa":null,"change_span_s":null}`
	if got := rec.Body.String(); got != want+"\n" {
		t.Fatalf("body = %q, want %q", got, want)
	}
}
