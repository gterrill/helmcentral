package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// lightningFixture is a schedule on a temp file with inline refreshes, a
// movable clock and a stub provider.
type lightningFixture struct {
	t    *testing.T
	s    *lightningScheduler
	p    *stubLightningProvider
	now  time.Time
	path string
	lat  float64
	lon  float64
}

func newLightningFixture(t *testing.T) *lightningFixture {
	t.Helper()
	f := &lightningFixture{
		t:    t,
		now:  time.Date(2026, 11, 12, 11, 44, 0, 0, time.UTC),
		path: filepath.Join(t.TempDir(), "lightning.json"),
		lat:  -27.4,
		lon:  153.1,
		p:    &stubLightningProvider{id: "stub", run: time.Date(2026, 11, 12, 6, 0, 0, 0, time.UTC), probability: 0.3},
	}
	f.s = newLightningScheduler(f.path)
	f.s.spawn = func(fn func()) { fn() }
	orig := lightningNow
	lightningNow = func() time.Time { return f.now }
	t.Cleanup(func() { lightningNow = orig })
	return f
}

func (f *lightningFixture) view() lightningView { return f.s.view(f.p, f.lat, f.lon) }

// callsSince returns the provider calls made since the last reset.
func (f *lightningFixture) take() []stubLightningCall {
	calls := f.p.calls
	f.p.calls = nil
	return calls
}

func (f *lightningFixture) advance(d time.Duration) { f.now = f.now.Add(d) }

func lastEnd(v lightningView) time.Time {
	if len(v.Windows) == 0 {
		return time.Time{}
	}
	return v.Windows[len(v.Windows)-1].End
}

func TestLightningSchedule_FirstRequestFetchesNearThenFarInDaySpans(t *testing.T) {
	f := newLightningFixture(t)
	start := f.now

	v := f.view()
	calls := f.take()

	// 48 hours near is 2 spans; the rest of 348 hours (14.5 days) is 300 hours
	// = 12.5 spans, so 13.
	if len(calls) != 2+13 {
		t.Fatalf("got %d plugin calls, want 15", len(calls))
	}
	if !calls[0].from.Equal(start) || !calls[0].to.Equal(start.Add(24*time.Hour)) {
		t.Errorf("first span = %s..%s", calls[0].from, calls[0].to)
	}
	for i := 1; i < len(calls); i++ {
		if !calls[i].from.Equal(calls[i-1].to) {
			t.Fatalf("span %d starts at %s, previous ended %s: spans must be contiguous", i, calls[i].from, calls[i-1].to)
		}
		if calls[i].to.Sub(calls[i].from) > 24*time.Hour {
			t.Fatalf("span %d is longer than 24 hours", i)
		}
	}
	if !calls[1].to.Equal(start.Add(48 * time.Hour)) {
		t.Errorf("near ends at %s, want now+48h", calls[1].to)
	}
	runEnd := f.p.run.Add(348 * time.Hour)
	if !calls[len(calls)-1].to.Equal(runEnd) {
		t.Errorf("far ends at %s, want the run's last window, run+348h = %s", calls[len(calls)-1].to, runEnd)
	}
	// The last span is not empty: it holds the windows up to the run's last.
	if !calls[len(calls)-1].from.Before(runEnd) {
		t.Errorf("last span starts at %s, at or after the run's end", calls[len(calls)-1].from)
	}
	// Run discovery happens once: the first call finds the run, the rest reuse it.
	if !calls[0].runIn.IsZero() {
		t.Errorf("first span was given run %s, want discovery", calls[0].runIn)
	}
	for i := 1; i < len(calls); i++ {
		if !calls[i].runIn.Equal(f.p.run) {
			t.Fatalf("span %d was given run %s, want the one the first span returned (%s)", i, calls[i].runIn, f.p.run)
		}
	}
	if v.Refreshing || v.RefreshError != "" || !v.NearAt.Equal(start) {
		t.Errorf("view = refreshing %v, error %q, near_at %s", v.Refreshing, v.RefreshError, v.NearAt)
	}
	// Windows end every 3 hours; the first still to come ends at 12:00.
	want := 0
	for end := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC); !end.After(f.p.run.Add(348 * time.Hour)); end = end.Add(3 * time.Hour) {
		want++
	}
	if len(v.Windows) != want {
		t.Fatalf("got %d windows, want %d", len(v.Windows), want)
	}
	if !v.Windows[0].End.Equal(time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("first window ends %s, want the one in progress, ending 12:00", v.Windows[0].End)
	}
	if !lastEnd(v).After(start.Add(14 * 24 * time.Hour)) {
		t.Errorf("trace stops at %s, far windows are missing", lastEnd(v))
	}
}

func TestLightningSchedule_NothingToFetchWithinTheTTL(t *testing.T) {
	f := newLightningFixture(t)
	f.view()
	f.take()

	f.advance(5 * time.Hour)
	v := f.view()
	if calls := f.take(); len(calls) != 0 {
		t.Fatalf("%d plugin calls inside the TTL", len(calls))
	}
	if len(v.Windows) == 0 {
		t.Fatal("held windows should still be served")
	}
}

func TestLightningSchedule_NearFollowsTheRunFarWaitsForTheDay(t *testing.T) {
	f := newLightningFixture(t)
	f.view()
	f.take()

	// Past the 6 hour TTL: the near windows only.
	f.advance(6*time.Hour + time.Minute)
	f.view()
	calls := f.take()
	if len(calls) != 2 {
		t.Fatalf("got %d calls after the TTL, want the 2 near spans", len(calls))
	}
	if !calls[1].to.Equal(f.now.Add(48 * time.Hour)) {
		t.Errorf("near ends at %s", calls[1].to)
	}

	// Past a day since the far windows were fetched: near and far together.
	f.advance(18 * time.Hour)
	f.p.run = f.now.Add(-6 * time.Hour) // a day on there is a newer run
	f.view()
	if calls := f.take(); len(calls) != 15 {
		t.Fatalf("got %d calls on the daily refresh, want 15", len(calls))
	}
}

func TestLightningSchedule_SwingingAtAnchorFetchesNothing(t *testing.T) {
	f := newLightningFixture(t)
	f.view()
	f.take()

	f.advance(7 * time.Hour)
	f.view()
	f.take()
	for _, step := range [][2]float64{{0.0003, 0.0004}, {-0.0005, 0.0002}, {0.0001, -0.0006}} {
		f.lat, f.lon = -27.4+step[0], 153.1+step[1]
		f.view()
	}
	if calls := f.take(); len(calls) != 0 {
		t.Fatalf("swinging inside one cell made %d plugin calls", len(calls))
	}
}

func TestLightningSchedule_NewCellGetsNearNowAndFarAtTheNextDailyRefresh(t *testing.T) {
	f := newLightningFixture(t)
	f.view()
	f.take()

	f.advance(time.Hour)
	f.lon += 0.5 // well over a cell (0.28125 degrees)
	v := f.view()
	calls := f.take()
	if len(calls) != 2 {
		t.Fatalf("a new cell made %d calls, want its 2 near spans only", len(calls))
	}
	_, centreLon := testCellCentre(f.lat, f.lon)
	for _, c := range calls {
		if c.lon != centreLon {
			t.Errorf("a call asked about longitude %v, not the new cell's centre %v", c.lon, centreLon)
		}
	}
	// The old cell's far windows are for another place and must not be shown.
	if end := lastEnd(v); end.After(f.now.Add(48 * time.Hour)) {
		t.Errorf("new cell shows a window ending %s, beyond the near span: the old cell's far windows leaked", end)
	}

	// At the next daily refresh the far span is fetched for the cell the boat is in.
	f.advance(24 * time.Hour)
	f.p.run = f.now.Add(-6 * time.Hour)
	v = f.view()
	if calls := f.take(); len(calls) != 15 {
		t.Fatalf("got %d calls at the daily refresh, want 15", len(calls))
	}
	if !lastEnd(v).After(f.now.Add(14 * 24 * time.Hour)) {
		t.Errorf("far windows still missing after the daily refresh, trace ends %s", lastEnd(v))
	}
}

func TestLightningSchedule_FailedRefreshKeepsFutureWindowsAndDropsPastOnes(t *testing.T) {
	f := newLightningFixture(t)
	f.view()
	f.take()

	f.advance(7 * time.Hour) // 18:44; the TTL has passed
	f.p.err = fmt.Errorf("bucket unreachable")
	v := f.view()

	if v.RefreshError == "" {
		t.Fatal("a failed refresh must be reported")
	}
	if len(v.Windows) == 0 {
		t.Fatal("held windows should be served stale")
	}
	for _, w := range v.Windows {
		if !w.End.After(f.now) {
			t.Fatalf("a window ending %s has passed and is still served", w.End)
		}
		if !w.Run.Equal(f.p.run) {
			t.Fatalf("held window lost the run it came from: %s", w.Run)
		}
	}
	if !v.Windows[0].End.Equal(time.Date(2026, 11, 12, 21, 0, 0, 0, time.UTC)) {
		t.Errorf("first held window ends %s, want 21:00", v.Windows[0].End)
	}
	if !v.NearAt.Equal(time.Date(2026, 11, 12, 11, 44, 0, 0, time.UTC)) {
		t.Errorf("near_at moved to %s though nothing refreshed", v.NearAt)
	}
}

func TestLightningSchedule_FailedRefreshBacksOffThenRetries(t *testing.T) {
	f := newLightningFixture(t)
	f.view()
	f.take()

	// A day on, near and far are both due. Each range gives up after
	// lightningMaxSpanFails spans in a row; the near range has only two spans.
	f.advance(25 * time.Hour)
	f.p.err = fmt.Errorf("bucket unreachable")
	f.view()
	failedCalls := f.take()
	if want := 2 + lightningMaxSpanFails; len(failedCalls) != want {
		t.Fatalf("a dead source cost %d calls, want it to stop after %d", len(failedCalls), want)
	}

	f.advance(time.Minute)
	f.view()
	if calls := f.take(); len(calls) != 0 {
		t.Fatalf("retried after a minute (%d calls)", len(calls))
	}

	f.advance(lightningRetryAfterFailure)
	f.p.err = nil
	f.p.run = f.now.Add(-6 * time.Hour)
	v := f.view()
	if calls := f.take(); len(calls) != 15 {
		t.Fatalf("retry made %d calls, want the 15 spans of a full refresh", len(calls))
	}
	if v.RefreshError != "" {
		t.Errorf("error %q still reported after a good refresh", v.RefreshError)
	}
}

func TestLightningSchedule_OneBadSpanKeepsItsOldWindowsAndTheRestRefresh(t *testing.T) {
	f := newLightningFixture(t)
	f.view()
	f.take()

	f.advance(7 * time.Hour) // 18:44
	oldRun := f.p.run
	f.p.run = time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	f.p.probability = 0.6
	// Make the second near span fail by having the plugin break its contract there.
	calls := 0
	failing := &spanFailer{stubLightningProvider: f.p, failOn: 2, calls: &calls}
	v := f.s.view(failing, f.lat, f.lon)

	if v.RefreshError == "" {
		t.Fatal("the bad span must be reported")
	}
	byEnd := map[time.Time]lightningHeldWindow{}
	for _, w := range v.Windows {
		byEnd[w.End] = w
	}
	if w := byEnd[time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)]; w.Probability != 0.6 || !w.Run.Equal(f.p.run) {
		t.Errorf("a good near span was not refreshed: %+v", w)
	}
	badSpanFrom := f.now.Add(24 * time.Hour)
	var stale lightningHeldWindow
	for _, w := range v.Windows {
		if w.End.After(badSpanFrom) && !w.End.After(badSpanFrom.Add(24*time.Hour)) {
			stale = w
			break
		}
	}
	if stale.Probability != 0.3 || !stale.Run.Equal(oldRun) {
		t.Errorf("the failed span should keep its old windows from the old run, got %+v", stale)
	}
}

func TestValidateLightningSpan_RejectsProbabilityOutsideZeroToOne(t *testing.T) {
	from := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	for _, p := range []float64{1.2, -0.1, math.NaN()} {
		span := lightningSpan{Run: from, Windows: []lightningWindow{{Start: from, End: from.Add(3 * time.Hour), Probability: p}}}
		err := validateLightningSpan(span, from, to)
		if err == nil || !strings.Contains(err.Error(), "outside 0..1") {
			t.Errorf("probability %v: err = %v, want one mentioning outside 0..1", p, err)
		}
	}
}

// spanFailer returns a window outside the span asked for on one call.
type spanFailer struct {
	*stubLightningProvider
	failOn int
	calls  *int
}

func (s *spanFailer) FetchLightning(lat, lon float64, from, to, run time.Time) (lightningSpan, error) {
	*s.calls++
	span, err := s.stubLightningProvider.FetchLightning(lat, lon, from, to, run)
	if *s.calls == s.failOn {
		span.Windows = append(span.Windows, lightningWindow{Start: to, End: to.Add(3 * time.Hour), Probability: 0.1})
	}
	return span, err
}

func TestLightningSchedule_MissingWindowStaysAGap(t *testing.T) {
	f := newLightningFixture(t)
	gap := time.Date(2026, 11, 12, 18, 0, 0, 0, time.UTC)
	f.p.skip = map[time.Time]bool{gap: true}

	v := f.view()
	var ends []time.Time
	for _, w := range v.Windows[:4] {
		ends = append(ends, w.End)
	}
	want := []time.Time{
		time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 12, 21, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC),
	}
	for i := range want {
		if !ends[i].Equal(want[i]) {
			t.Fatalf("window ends = %v, want %v (the 18:00 gap must not be filled)", ends, want)
		}
	}

	// Once the run has the window, a refresh puts it in; and a refresh that
	// lacks a window it once had does not keep the old one in its place.
	f.advance(7 * time.Hour)
	f.p.skip = nil
	f.p.run = time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	f.view()
	f.take()
	f.advance(7 * time.Hour) // 01:44 on the 13th
	f.p.skip = map[time.Time]bool{time.Date(2026, 11, 13, 6, 0, 0, 0, time.UTC): true}
	v = f.view()
	for _, w := range v.Windows {
		if w.End.Equal(time.Date(2026, 11, 13, 6, 0, 0, 0, time.UTC)) {
			t.Fatal("a window the new run lacks was kept from the old run")
		}
	}
}

func TestLightningSchedule_HeldWindowsSurviveARestart(t *testing.T) {
	f := newLightningFixture(t)
	f.view()
	f.take()

	raw, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatalf("state was not saved: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("empty state file")
	}

	restarted := newLightningScheduler(f.path)
	restarted.spawn = func(fn func()) { fn() }
	f.advance(time.Hour)
	v := restarted.view(f.p, f.lat, f.lon)
	if calls := f.take(); len(calls) != 0 {
		t.Fatalf("a restart inside the TTL refetched (%d calls)", len(calls))
	}
	if len(v.Windows) == 0 || v.NearAt.IsZero() {
		t.Fatalf("restart lost the held windows: %d windows, near_at %s", len(v.Windows), v.NearAt)
	}
}

func TestLightningSchedule_UnreadableStateFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lightning.json")
	if err := os.WriteFile(path, []byte(`{"far_at": "not a time", "cells": 7`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newLightningScheduler(path)
	if s.state.Cell != nil || !s.state.FarAt.IsZero() {
		t.Fatalf("state = %+v, want empty", s.state)
	}

	// A file written by a build with fields this one does not know still loads.
	if err := os.WriteFile(path, []byte(`{"far_at":"2026-11-12T00:00:00Z","something_new":1,"cell":{"row":480,"col":640,"near_at":"2026-11-12T00:00:00Z","extra":true,"windows":[]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s = newLightningScheduler(path)
	if s.state.Cell == nil || s.state.Cell.Row != 480 || s.state.FarAt.IsZero() {
		t.Fatalf("state = %+v, want the known fields loaded", s.state)
	}

	// A file in the old several-cell shape holds no cell, so it starts empty.
	if err := os.WriteFile(path, []byte(`{"far_at":"2026-11-12T00:00:00Z","cells":{"480,640":{"near_at":"2026-11-12T00:00:00Z","windows":[]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s = newLightningScheduler(path)
	if s.state.Cell != nil || !s.state.FarAt.IsZero() {
		t.Fatalf("state = %+v, want no cell and no far clock from the old shape", s.state)
	}

	// Its recent far_at must not suppress the far range: the first view buys it.
	f := newLightningFixture(t)
	f.s = s
	f.s.spawn = func(fn func()) { fn() }
	f.view()
	farStart := f.now.Add(lightningNearHorizon)
	farCalled := false
	for _, c := range f.take() {
		if !c.from.Before(farStart) {
			farCalled = true
		}
	}
	if !farCalled {
		t.Fatal("the first view after an old-shape file did not schedule the far range")
	}
}

// Finding 3: a far-range error stands until a far refresh clears it.
func TestLightningSchedule_FarErrorSurvivesNearOnlyRefresh(t *testing.T) {
	f := newLightningFixture(t)
	f.view()
	f.take()

	f.advance(25 * time.Hour) // near and far due; the first far span breaks its contract
	calls := 0
	v := f.s.view(&spanFailer{stubLightningProvider: f.p, failOn: 3, calls: &calls}, f.lat, f.lon)
	if v.RefreshError == "" {
		t.Fatal("a failed far span must be reported")
	}
	f.take()
	if !f.s.state.FarAt.Equal(f.now) {
		t.Fatalf("far_at = %s, want it advanced to %s after a partial far refresh", f.s.state.FarAt, f.now)
	}

	f.advance(6*time.Hour + time.Minute) // near only
	v = f.view()
	if got := len(f.take()); got != 2 {
		t.Fatalf("got %d calls, want the 2 near spans", got)
	}
	if v.RefreshError == "" {
		t.Fatal("a near-only refresh hid the standing far error")
	}

	f.advance(19 * time.Hour) // far due again
	v = f.view()
	if v.RefreshError != "" {
		t.Fatalf("error %q still reported after a good far refresh", v.RefreshError)
	}
}

// Finding 6: held windows are state and live under data/, the one directory
// that survives a container recreate.
func TestLightningStateFilePathDefaultsToDataDir(t *testing.T) {
	t.Setenv("HELMCENTRAL_STATE_DIR", "")
	t.Setenv("LIGHTNING_STATE_FILE_STUB", "")

	if got := lightningStateFilePath("stub"); got != "data/lightning_stub_state.json" {
		t.Fatalf("expected the lightning state default under data/, got %q", got)
	}
}

// Finding 7: at anchor on a cell edge the position wanders across it.
func TestLightningSchedule_PositionNearACellEdgeKeepsTheLastCell(t *testing.T) {
	f := newLightningFixture(t)
	f.lon = 153.1
	f.view()
	f.take()

	// 153.0 is a column edge; these are 0.01 degrees either side of it.
	for _, lon := range []float64{152.99, 153.01, 152.99, 153.01} {
		f.lon = lon
		f.advance(time.Minute)
		f.view()
	}
	if calls := f.take(); len(calls) != 0 {
		t.Fatalf("wandering across a cell edge made %d plugin calls", len(calls))
	}
	_, wantCol := lightningCellIndex(f.lat, 153.1)
	if cell := f.s.state.Cell; cell == nil || cell.Col != wantCol {
		t.Fatalf("held cell = %+v, want column %d kept", cell, wantCol)
	}

	// Well past the margin is a move.
	f.lon = 152.9
	f.advance(time.Minute)
	f.view()
	_, movedCol := lightningCellIndex(f.lat, f.lon)
	if cell := f.s.state.Cell; cell == nil || cell.Col != movedCol {
		t.Fatalf("held cell = %+v after a real move, want column %d", cell, movedCol)
	}
}

// Finding 8: the run's last window is run+348h, not now+348h.
func TestLightningSchedule_FarWalkEndsAtTheRunsLastWindow(t *testing.T) {
	f := newLightningFixture(t)
	f.p.run = f.now.Add(-12 * time.Hour)
	runEnd := f.p.run.Add(348 * time.Hour)

	f.view()
	calls := f.take()

	// 2 near spans, then 288 hours of far = exactly 12 spans.
	if len(calls) != 2+12 {
		t.Fatalf("got %d plugin calls, want 14 (no call for an empty last span)", len(calls))
	}
	for _, c := range calls {
		if !c.from.Before(runEnd) {
			t.Errorf("a span starting %s was asked for, at or after the run's end %s", c.from, runEnd)
		}
	}
	if last := calls[len(calls)-1]; !last.to.Equal(runEnd) {
		t.Errorf("far walk ends at %s, want run+348h = %s", last.to, runEnd)
	}
}

// Errors belong to the cell they were found for.
func TestLightningSchedule_MovingCellClearsTheOldCellsErrors(t *testing.T) {
	f := newLightningFixture(t)
	f.p.err = fmt.Errorf("bucket unreachable")
	if v := f.view(); v.RefreshError == "" {
		t.Fatal("a failed refresh must be reported")
	}

	f.advance(lightningRetryAfterFailure + time.Minute)
	f.p.err = nil
	f.lon += 0.5
	if v := f.view(); v.RefreshError != "" {
		t.Fatalf("new cell still reports %q from the old one", v.RefreshError)
	}
}

// The GET handler never waits: a request that starts a refresh with nothing
// held answers refreshing, not an error.
func TestLightningSchedule_ViewWhileFirstRefreshRuns(t *testing.T) {
	f := newLightningFixture(t)
	f.s.spawn = func(func()) {} // the refresh never ends
	v := f.view()
	if !v.Refreshing || len(v.Windows) != 0 || v.RefreshError != "" {
		t.Fatalf("view = %+v, want refreshing with nothing held and no error", v)
	}
}

func TestLightningSchedule_PlaceholderCoordinatesAreNotTheSchedulesToJudge(t *testing.T) {
	// The handler gates the position (hasUsableVesselPosition); the schedule
	// only needs every in-range position to land in a real cell.
	for _, p := range [][2]float64{{90, 180}, {-90, -180}, {0, 360}} {
		row, col := lightningCellIndex(p[0], p[1])
		if row < 0 || row >= lightningCellRows || col < 0 || col >= lightningCellCols {
			t.Errorf("(%v, %v) -> (%d, %d) is off the grid", p[0], p[1], row, col)
		}
	}
}

// testCellCentre is the centre of the grid cell holding a position.
func testCellCentre(lat, lon float64) (float64, float64) {
	row, col := lightningCellIndex(lat, lon)
	return -90 + (float64(row)+0.5)*lightningCellLatStep, -180 + (float64(col)+0.5)*lightningCellLonStep
}

// (a) The kept cell is refreshed at its centre. A position that has drifted
// across the cell edge, inside the margin, must not make the plugin read the
// neighbouring cell.
func TestLightningSchedule_RefreshUsesTheCellCentreEvenWhenThePositionDrifted(t *testing.T) {
	f := newLightningFixture(t)
	f.lon = 153.1
	centreLat, centreLon := testCellCentre(f.lat, f.lon)
	f.view()
	for _, c := range f.take() {
		if c.lat != centreLat || c.lon != centreLon {
			t.Fatalf("first refresh asked about %v,%v, want the cell centre %v,%v", c.lat, c.lon, centreLat, centreLon)
		}
	}

	// 152.99 is in the next column west but inside the margin of the held cell.
	f.advance(7 * time.Hour)
	f.lon = 152.99
	f.view()
	calls := f.take()
	if len(calls) == 0 {
		t.Fatal("expected a near refresh after the TTL")
	}
	for _, c := range calls {
		if c.lat != centreLat || c.lon != centreLon {
			t.Fatalf("refresh of the kept cell asked about %v,%v, want the cell centre %v,%v", c.lat, c.lon, centreLat, centreLon)
		}
	}
}

// movingProvider runs a hook on its first call, so a test can move the boat
// while a refresh is running.
type movingProvider struct {
	*stubLightningProvider
	onFirst func()
	done    bool
}

func (m *movingProvider) FetchLightning(lat, lon float64, from, to, run time.Time) (lightningSpan, error) {
	if !m.done {
		m.done = true
		m.onFirst()
	}
	return m.stubLightningProvider.FetchLightning(lat, lon, from, to, run)
}

// (b) Moving to a new cell drops the old cell's windows, and a refresh still
// running for the old cell neither merges into the new cell nor clears the new
// cell's refresh.
func TestLightningSchedule_MovingCellDropsOldWindowsAndAnOldRefreshCannotTouchTheNew(t *testing.T) {
	f := newLightningFixture(t)
	f.p.probability = 0.9
	var queued []func()
	moved := &movingProvider{stubLightningProvider: f.p}
	moved.onFirst = func() {
		// The boat leaves while the first cell's refresh is on its first span.
		f.lon += 0.5
		f.s.spawn = func(fn func()) { queued = append(queued, fn) }
		f.p.probability = 0.9
		f.s.view(f.p, f.lat, f.lon)
	}
	originalLon := f.lon
	f.s.view(moved, f.lat, f.lon)
	f.take()

	// The old refresh has finished; the new cell's has not run.
	v := f.s.view(f.p, f.lat, f.lon)
	if len(queued) != 1 {
		t.Fatalf("%d refreshes queued, want the new cell's one", len(queued))
	}
	if !v.Refreshing {
		t.Fatal("the old refresh finishing cleared the new cell's refreshing flag")
	}
	if len(v.Windows) != 0 {
		t.Fatalf("the new cell shows %d windows from the old cell's refresh", len(v.Windows))
	}

	f.p.probability = 0.5
	queued[0]()
	v = f.s.view(f.p, f.lat, f.lon)
	if len(v.Windows) == 0 {
		t.Fatal("the new cell's own refresh left no windows")
	}
	for _, w := range v.Windows {
		if w.Probability != 0.5 {
			t.Fatalf("window ending %s has probability %v, from the old cell's refresh", w.End, w.Probability)
		}
	}

	// Going back is a new cell again: its windows were dropped, so it refetches.
	f.s.spawn = func(fn func()) { fn() }
	f.take()
	f.advance(time.Minute)
	f.lon = originalLon
	f.view()
	if len(f.take()) == 0 {
		t.Fatal("returning to the first cell fetched nothing; its windows should have been dropped")
	}
}

// (c) One retry wait for the whole scheduler: a new cell inside ten minutes of
// a failure does not start a refresh.
func TestLightningSchedule_OneRetryWaitCoversANewCell(t *testing.T) {
	f := newLightningFixture(t)
	f.p.err = fmt.Errorf("bucket unreachable")
	f.view()
	f.take()

	f.advance(time.Minute)
	f.lon += 0.5
	f.view()
	if calls := f.take(); len(calls) != 0 {
		t.Fatalf("a new cell inside the retry wait made %d plugin calls", len(calls))
	}

	f.advance(lightningRetryAfterFailure)
	f.p.err = nil
	f.view()
	if len(f.take()) == 0 {
		t.Fatal("no refresh after the retry wait")
	}
}

// A new cell inside the shared retry wait still reports why the last refresh
// failed; it must not look like a plugin that has nothing to say.
func TestLightningSchedule_ErrorStillReportedForANewCellInsideTheRetryWait(t *testing.T) {
	f := newLightningFixture(t)
	f.p.err = fmt.Errorf("bucket unreachable")
	if v := f.view(); v.RefreshError == "" {
		t.Fatal("the failing refresh reported no error")
	}
	f.take()

	f.advance(time.Minute)
	f.lon += 0.5
	v := f.view()
	if v.RefreshError == "" {
		t.Fatalf("view = %+v, want the last refresh error for the new cell", v)
	}
	if len(f.take()) != 0 {
		t.Fatal("a refresh started inside the retry wait")
	}

	// The next successful near refresh clears it.
	f.advance(lightningRetryAfterFailure)
	f.p.err = nil
	f.view()
	f.take()
	if v := f.view(); v.RefreshError != "" {
		t.Fatalf("RefreshError = %q after a successful refresh, want none", v.RefreshError)
	}
}

// Finding 2: a refresh for a cell the boat has left makes no further plugin
// calls, whether its span failed or succeeded.
func TestLightningSchedule_AbandonedRefreshStopsCallingThePlugin(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{{"failing", fmt.Errorf("bucket unreachable")}, {"succeeding", nil}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newLightningFixture(t)
			f.p.err = tc.err
			oldLat, oldLon := testCellCentre(f.lat, f.lon)
			moved := &movingProvider{stubLightningProvider: f.p}
			moved.onFirst = func() {
				f.lon += 0.5
				f.s.spawn = func(func()) {} // the new cell's refresh does not run
				f.s.view(f.p, f.lat, f.lon)
			}
			f.s.view(moved, f.lat, f.lon)
			n := 0
			for _, c := range f.take() {
				if c.lat == oldLat && c.lon == oldLon {
					n++
				}
			}
			if n != 1 {
				t.Fatalf("%d plugin calls for the cell the boat left, want exactly 1", n)
			}
		})
	}
}
