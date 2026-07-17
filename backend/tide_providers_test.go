package main

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
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

// classifyTidalPhaseFixtureExtremes gives 3 consecutive-pairs: a narrow-range
// (neap) pair on 2026-07-01, a mid pair spanning 07-01/07-04 that isn't used
// by any assertion, and a wide-range (spring) pair on 2026-07-04.
func classifyTidalPhaseFixtureExtremes() []tideExtremePoint {
	return []tideExtremePoint{
		{Time: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), HeightM: 1.0, High: false},
		{Time: time.Date(2026, 7, 1, 6, 0, 0, 0, time.UTC), HeightM: 1.2, High: true}, // range 0.2 (neap), mid 07-01T03:00
		{Time: time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC), HeightM: 1.0, High: false},
		{Time: time.Date(2026, 7, 4, 6, 0, 0, 0, time.UTC), HeightM: 3.0, High: true}, // range 2.0 (springs), mid 07-04T03:00
	}
}

func TestClassifyTidalPhase_LabelsNearestSpringsOrNeapsWithDayOffset(t *testing.T) {
	extremes := classifyTidalPhaseFixtureExtremes()

	tests := []struct {
		name string
		now  time.Time
		want string
	}{
		{
			name: "same day as the springs pair, closest to it",
			now:  time.Date(2026, 7, 4, 2, 0, 0, 0, time.UTC),
			want: "springs",
		},
		{
			name: "same day as the neaps pair, closest to it",
			now:  time.Date(2026, 7, 1, 2, 0, 0, 0, time.UTC),
			want: "neaps",
		},
		{
			name: "springs pair 2 days in the future",
			now:  time.Date(2026, 7, 2, 20, 0, 0, 0, time.UTC),
			want: "springs+2",
		},
		{
			name: "neaps pair 1 day in the past",
			now:  time.Date(2026, 7, 2, 4, 0, 0, 0, time.UTC),
			want: "neaps-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyTidalPhase(extremes, tt.now)
			if got != tt.want {
				t.Errorf("classifyTidalPhase() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClassifyTidalPhase_ReturnsEmptyWithInsufficientData(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		extremes []tideExtremePoint
	}{
		{name: "no extremes", extremes: nil},
		{
			name: "fewer than 4 extremes",
			extremes: []tideExtremePoint{
				{Time: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), HeightM: 1.0, High: false},
				{Time: time.Date(2026, 7, 1, 6, 0, 0, 0, time.UTC), HeightM: 2.0, High: true},
				{Time: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC), HeightM: 1.0, High: false},
			},
		},
		{
			name: "no range variation between pairs",
			extremes: []tideExtremePoint{
				{Time: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), HeightM: 1.0, High: false},
				{Time: time.Date(2026, 7, 1, 6, 0, 0, 0, time.UTC), HeightM: 2.0, High: true},
				{Time: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC), HeightM: 1.0, High: false},
				{Time: time.Date(2026, 7, 2, 6, 0, 0, 0, time.UTC), HeightM: 2.0, High: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyTidalPhase(tt.extremes, now)
			if got != "" {
				t.Errorf("classifyTidalPhase() = %q, want empty string", got)
			}
		})
	}
}

func TestHasDoubleTide_DetectsTwoHighsWithinLocalCalendarDay(t *testing.T) {
	// The two highs sit on different UTC calendar days (2026-07-01 and
	// 2026-07-02) but the same Australia/Sydney (UTC+10, no July DST) local
	// calendar day (2026-07-02) - proving local-day math is used, not
	// UTC-day math that would (wrongly) split them across two days.
	extremes := []tideExtremePoint{
		{Time: time.Date(2026, 7, 1, 23, 0, 0, 0, time.UTC), HeightM: 1.8, High: true},  // Sydney 07-02T09:00
		{Time: time.Date(2026, 7, 2, 1, 0, 0, 0, time.UTC), HeightM: 1.9, High: true},   // Sydney 07-02T11:00
		{Time: time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC), HeightM: 0.3, High: false}, // Sydney 07-02T20:00 - single low
		{Time: time.Date(2026, 7, 3, 5, 0, 0, 0, time.UTC), HeightM: 1.7, High: true},   // Sydney 07-03 - different day
	}
	station := tideStation{StationID: "TEST", Timezone: "Australia/Sydney"}
	now := time.Date(2026, 7, 2, 0, 30, 0, 0, time.UTC) // Sydney 07-02T10:30

	doubleHigh, doubleLow := hasDoubleTide(extremes, station, now)
	if !doubleHigh {
		t.Errorf("expected doubleHigh=true (2 highs on the Sydney local day), got false")
	}
	if doubleLow {
		t.Errorf("expected doubleLow=false (1 low on the Sydney local day), got true")
	}
}

func TestHasDoubleTide_FallsBackToUTCWhenStationTimezoneUnknown(t *testing.T) {
	extremes := []tideExtremePoint{
		{Time: time.Date(2026, 7, 2, 1, 0, 0, 0, time.UTC), HeightM: 1.8, High: true},
		{Time: time.Date(2026, 7, 2, 13, 0, 0, 0, time.UTC), HeightM: 1.9, High: true},
		{Time: time.Date(2026, 7, 2, 7, 0, 0, 0, time.UTC), HeightM: 0.3, High: false},
	}
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		timezone string
	}{
		{name: "empty timezone", timezone: ""},
		{name: "invalid timezone", timezone: "Not/AZone"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			station := tideStation{StationID: "TEST", Timezone: tt.timezone}
			doubleHigh, doubleLow := hasDoubleTide(extremes, station, now)
			if !doubleHigh {
				t.Errorf("expected doubleHigh=true using UTC-day fallback, got false")
			}
			if doubleLow {
				t.Errorf("expected doubleLow=false using UTC-day fallback, got true")
			}
		})
	}
}

type stubTideProvider struct {
	id       string
	result   tideChartResult
	fetchErr error
}

func (s *stubTideProvider) ID() string        { return s.id }
func (s *stubTideProvider) Name() string      { return "Stub " + s.id }
func (s *stubTideProvider) TTLSeconds() int64 { return 3600 }
func (s *stubTideProvider) SearchStations(query string, limit int) []tideStation { return nil }
func (s *stubTideProvider) FetchTideChart(stationID string) (tideChartResult, error) {
	return s.result, s.fetchErr
}

func TestTideChartHandler_IncludesTidalPhaseAndDoubleTideFields(t *testing.T) {
	origRegistry := tideProviderRegistry
	origOrder := tideProviderOrder
	tideProviderRegistry = map[string]tideProvider{}
	tideProviderOrder = nil
	t.Cleanup(func() {
		tideProviderRegistry = origRegistry
		tideProviderOrder = origOrder
	})

	// The handler stamps "now" as the real wall-clock time, so these
	// extremes are built relative to it (not fixed 2026 dates) - two highs
	// both within today's UTC calendar day (station has no timezone so the
	// handler falls back to UTC) to exercise double_high_today, plus a
	// springs/neaps-eligible 4+ point extreme set so tidal_phase resolves to
	// a non-empty value.
	now := time.Now().UTC()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	extremes := []tideExtremePoint{
		{Time: todayStart.Add(-23 * time.Hour), HeightM: 1.0, High: false}, // yesterday
		{Time: todayStart.Add(1 * time.Hour), HeightM: 1.2, High: false},   // today, sole low
		{Time: todayStart.Add(7 * time.Hour), HeightM: 3.0, High: true},    // today, high #1
		{Time: todayStart.Add(19 * time.Hour), HeightM: 3.2, High: true},   // today, high #2
	}
	station := tideStation{StationID: "STUB1", Name: "Stub Station"}
	provider := &stubTideProvider{
		id: "stub",
		result: tideChartResult{
			Station:  station,
			Extremes: extremes,
		},
	}
	registerTideProvider(provider)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/tide-chart?provider=stub&station_id=STUB1", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := tideChartHandler(c); err != nil {
		t.Fatalf("tideChartHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var payload tideChartResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if payload.TidalPhase == "" {
		t.Errorf("expected a non-empty tidal_phase, got empty string")
	}
	if !payload.DoubleHighToday {
		t.Errorf("expected double_high_today=true, got false")
	}
	if payload.DoubleLowToday {
		t.Errorf("expected double_low_today=false, got true")
	}
}
