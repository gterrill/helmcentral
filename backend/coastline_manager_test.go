package main

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeCoastlineSource struct {
	mu        sync.Mutex
	downloads []coverageSquare
	checks    []coverageSquare
	fail      map[coverageSquare]error
	edits     map[coverageSquare]int
	checkErr  error
	lines     []shorelineLine
}

func (f *fakeCoastlineSource) SupportsShoreline() bool { return true }

func (f *fakeCoastlineSource) ShorelineSquare(sq coverageSquare) ([]shorelineLine, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.downloads = append(f.downloads, sq)
	if err := f.fail[sq]; err != nil {
		return nil, err
	}
	return f.lines, nil
}

func (f *fakeCoastlineSource) ShorelineChanges(sq coverageSquare, since time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks = append(f.checks, sq)
	if f.checkErr != nil {
		return 0, f.checkErr
	}
	return f.edits[sq], nil
}

type coastlineHarness struct {
	m       *coastlineManager
	store   *coastlineStore
	src     *fakeCoastlineSource
	now     time.Time
	enabled bool
	fix     bool
	lat     float64
	lon     float64
	srcErr  error
}

func newCoastlineHarness(t *testing.T) *coastlineHarness {
	t.Helper()
	h := &coastlineHarness{
		store:   newTestCoastlineStore(t),
		src:     &fakeCoastlineSource{fail: map[coverageSquare]error{}, edits: map[coverageSquare]int{}},
		now:     time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC),
		enabled: true, fix: true, lat: -20.1, lon: 148.9,
	}
	h.m = newCoastlineManager(coastlineManagerConfig{
		Store:    h.store,
		Enabled:  func() (bool, error) { return h.enabled, nil },
		Position: func() (float64, float64, bool) { return h.lat, h.lon, h.fix },
		Source: func() (coastlineSource, error) {
			if h.srcErr != nil {
				return nil, h.srcErr
			}
			return h.src, nil
		},
		Now: func() time.Time { return h.now },
	})
	return h
}

func TestCoastlineManager_OffDoesNothing(t *testing.T) {
	h := newCoastlineHarness(t)
	h.enabled = false
	if h.m.step() {
		t.Error("nothing to do when off")
	}
	if len(h.src.downloads)+len(h.src.checks) != 0 {
		t.Errorf("off must not call the plugin: %v %v", h.src.downloads, h.src.checks)
	}
	st, err := h.m.Status()
	if err != nil || st.Enabled || st.State != "off" {
		t.Errorf("status %+v %v", st, err)
	}
}

func TestCoastlineManager_DownloadsNearestFirstAndOnlyMissing(t *testing.T) {
	h := newCoastlineHarness(t)
	wanted, err := squaresWithin(h.lat, h.lon, coastlineCoverageRadiusNm)
	if err != nil {
		t.Fatal(err)
	}
	held := wanted[1]
	if err := h.store.SaveSquare(held, nil, h.now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if !h.m.step() {
			t.Fatalf("step %d did nothing", i)
		}
	}
	want := []coverageSquare{wanted[0], wanted[2], wanted[3], wanted[4], wanted[5]}
	for i, sq := range want {
		if h.src.downloads[i] != sq {
			t.Fatalf("download %d = %v, want %v (nearest first, skipping the held %v)", i, h.src.downloads[i], sq, held)
		}
	}
	rec, ok, _ := h.store.Record(wanted[0])
	if !ok || rec.Status != coastlineStatusReady {
		t.Errorf("downloaded square not stored: %+v", rec)
	}

	// Run to completion: every wanted square held, then nothing more to do.
	for h.m.step() {
	}
	for _, sq := range wanted {
		if rec, ok, _ := h.store.Record(sq); !ok || rec.Status != coastlineStatusReady {
			t.Errorf("%v not ready: %+v", sq, rec)
		}
	}
	n := len(h.src.downloads)
	if h.m.step() || len(h.src.downloads) != n {
		t.Error("a complete coverage downloads nothing")
	}
	st, _ := h.m.Status()
	if st.State != "idle" || st.Held != len(wanted) || st.Wanted != len(wanted) {
		t.Errorf("status %+v", st)
	}
}

func TestCoastlineManager_MovingOnlyFetchesSquaresNewlyInRange(t *testing.T) {
	h := newCoastlineHarness(t)
	for h.m.step() {
	}
	before := len(h.src.downloads)
	h.lat = -19.1 // one degree north
	for h.m.step() {
	}
	got := h.src.downloads[before:]
	if len(got) == 0 || len(got) > 20 {
		t.Fatalf("moved one degree: %d new squares", len(got))
	}
	seen := map[coverageSquare]bool{}
	for _, sq := range h.src.downloads[:before] {
		seen[sq] = true
	}
	for _, sq := range got {
		if seen[sq] {
			t.Errorf("%v downloaded twice", sq)
		}
	}
	// Squares behind the boat are kept.
	st, _ := h.m.Status()
	if st.Held != before+len(got) || st.Held <= st.Wanted {
		t.Errorf("held %d wanted %d: squares out of range must be kept", st.Held, st.Wanted)
	}
}

func TestCoastlineManager_NoFixPausesTopUpButStatusSaysSo(t *testing.T) {
	h := newCoastlineHarness(t)
	h.fix = false
	if h.m.step() {
		t.Error("no fix, no downloads")
	}
	if len(h.src.downloads) != 0 {
		t.Errorf("downloaded without a position: %v", h.src.downloads)
	}
	st, _ := h.m.Status()
	if st.State != "waiting_for_fix" || st.HasFix || st.Position != nil {
		t.Errorf("status %+v", st)
	}
	h.fix = true
	if !h.m.step() {
		t.Error("a fix resumes the top-up")
	}
}

func TestCoastlineManager_FailedSquareIsRecordedBackedOffAndRetried(t *testing.T) {
	h := newCoastlineHarness(t)
	wanted, _ := squaresWithin(h.lat, h.lon, coastlineCoverageRadiusNm)
	h.src.fail[wanted[0]] = errors.New("overpass rate limited")
	h.m.step()
	rec, ok, _ := h.store.Record(wanted[0])
	if !ok || rec.Status != coastlineStatusFailed || rec.LastError != "overpass rate limited" || rec.Attempts != 1 {
		t.Fatalf("failure not recorded: %+v", rec)
	}
	first := rec.NextAttemptAt.Sub(h.now)
	if first <= 0 {
		t.Fatalf("no backoff: %v", first)
	}

	// The next step moves on instead of hammering the failed square.
	h.m.step()
	if h.src.downloads[1] != wanted[1] {
		t.Errorf("second download %v, want the next square %v", h.src.downloads[1], wanted[1])
	}
	for _, sq := range h.src.downloads[1:] {
		if sq == wanted[0] {
			t.Error("retried before the backoff elapsed")
		}
	}
	st, _ := h.m.Status()
	if st.Failed != 1 {
		t.Errorf("failed count %d", st.Failed)
	}
	var shown *coastlineStatusSquare
	for i := range st.Squares {
		if st.Squares[i].Lat == wanted[0].Lat && st.Squares[i].Lon == wanted[0].Lon {
			shown = &st.Squares[i]
		}
	}
	if shown == nil || shown.State != "failed" || shown.Error != "overpass rate limited" {
		t.Errorf("the page must show the failed square and why: %+v", shown)
	}

	// After the backoff it is tried again, and a second failure waits longer.
	for h.m.step() {
	}
	h.now = h.now.Add(first + time.Second)
	h.m.step()
	last := h.src.downloads[len(h.src.downloads)-1]
	if last != wanted[0] {
		t.Fatalf("retry: got %v", last)
	}
	rec, _, _ = h.store.Record(wanted[0])
	if rec.Attempts != 2 || rec.NextAttemptAt.Sub(h.now) <= first {
		t.Errorf("backoff must grow: %+v (first %v)", rec, first)
	}

	// And it recovers.
	delete(h.src.fail, wanted[0])
	h.now = rec.NextAttemptAt.Add(time.Second)
	h.m.step()
	rec, _, _ = h.store.Record(wanted[0])
	if rec.Status != coastlineStatusReady || rec.LastError != "" {
		t.Errorf("not recovered: %+v", rec)
	}
}

func TestCoastlineManager_WeeklyCheckStoresEditsAndNeverAppliesThem(t *testing.T) {
	h := newCoastlineHarness(t)
	sq := coverageSquare{-21, 148}
	_ = h.store.SaveSquare(sq, sampleCoastLines(), h.now)
	h.fix = false // checks need no position
	h.src.edits[sq] = 12

	h.now = h.now.Add(6 * 24 * time.Hour)
	if h.m.step() || len(h.src.checks) != 0 {
		t.Fatal("a square checked six days ago is not due")
	}
	h.now = h.now.Add(25 * time.Hour)
	if !h.m.step() || len(h.src.checks) != 1 {
		t.Fatalf("a week on, the check runs: %v", h.src.checks)
	}
	rec, _, _ := h.store.Record(sq)
	if rec.EditsSince != 12 || !rec.CheckedAt.Equal(h.now) {
		t.Errorf("edits not stored: %+v", rec)
	}
	if len(h.src.downloads) != 0 {
		t.Error("edits are applied only when the operator asks")
	}
	h.m.step()
	if len(h.src.downloads) != 0 || len(h.src.checks) != 1 {
		t.Error("the same square is not checked again the same day")
	}

	st, _ := h.m.Status()
	if st.EditsSince != 12 || st.SquaresWithEdits != 1 {
		t.Errorf("status %+v", st)
	}

	// Update re-downloads the squares with edits, and only those.
	other := coverageSquare{-21, 149}
	_ = h.store.SaveSquare(other, nil, h.now)
	h.fix = true
	n, err := h.m.Update()
	if err != nil || n != 1 {
		t.Fatalf("update queued %d: %v", n, err)
	}
	st, _ = h.m.Status()
	if st.State != "updating" || st.Updating != 1 {
		t.Errorf("status while updating %+v", st)
	}
	h.m.step()
	if len(h.src.downloads) != 1 || h.src.downloads[0] != sq {
		t.Fatalf("downloads %v", h.src.downloads)
	}
	rec, _, _ = h.store.Record(sq)
	if rec.EditsSince != 0 || !rec.DownloadedAt.Equal(h.now) {
		t.Errorf("not refreshed: %+v", rec)
	}
}

func TestCoastlineManager_UpdateWithNothingToUpdateOrWhenOffIsAnError(t *testing.T) {
	h := newCoastlineHarness(t)
	if n, err := h.m.Update(); err == nil || n != 0 {
		t.Errorf("no edits: %d %v", n, err)
	}
	_ = h.store.SaveSquare(coverageSquare{-21, 148}, nil, h.now)
	_ = h.store.RecordEdits(coverageSquare{-21, 148}, 2, h.now)
	h.enabled = false
	if _, err := h.m.Update(); err == nil || !strings.Contains(err.Error(), "off") {
		t.Errorf("off: %v", err)
	}
}

func TestCoastlineManager_FailedUpdateKeepsOldSquareAndEdits(t *testing.T) {
	h := newCoastlineHarness(t)
	sq := coverageSquare{-21, 148}
	_ = h.store.SaveSquare(sq, sampleCoastLines(), h.now)
	_ = h.store.RecordEdits(sq, 5, h.now)
	h.src.fail[sq] = errors.New("timed out")
	if _, err := h.m.Update(); err != nil {
		t.Fatal(err)
	}
	h.m.step()
	rec, _, _ := h.store.Record(sq)
	if rec.Status != coastlineStatusReady || rec.EditsSince != 5 || rec.LastError != "timed out" {
		t.Errorf("%+v", rec)
	}
	if lines, err := h.store.Lines([]coverageSquare{sq}); err != nil || len(lines) != 2 {
		t.Errorf("old geometry lost: %v %v", lines, err)
	}
}

func TestCoastlineManager_WeeklyCheckFailureBacksOff(t *testing.T) {
	h := newCoastlineHarness(t)
	sq := coverageSquare{-21, 148}
	_ = h.store.SaveSquare(sq, nil, h.now)
	h.now = h.now.Add(8 * 24 * time.Hour)
	h.src.checkErr = fmt.Errorf("overpass rate limited")
	h.fix = false
	if !h.m.step() {
		t.Fatal("check should have run")
	}
	if h.m.step() || len(h.src.checks) != 1 {
		t.Error("a failed check waits for its backoff")
	}
	rec, _, _ := h.store.Record(sq)
	if rec.LastError == "" || rec.Status != coastlineStatusReady {
		t.Errorf("%+v", rec)
	}
}

func TestCoastlineManager_SurfacesProblemsInsteadOfWorkingAround(t *testing.T) {
	h := newCoastlineHarness(t)
	h.lat, h.lon = -17, 179.6
	if h.m.step() {
		t.Error("an unsupported area downloads nothing")
	}
	st, _ := h.m.Status()
	if st.State != "error" || !strings.Contains(st.Error, "180") {
		t.Errorf("antimeridian: %+v", st)
	}

	h = newCoastlineHarness(t)
	h.srcErr = errors.New("osm-overpass plugin is not installed")
	if h.m.step() {
		t.Error("no plugin, no downloads")
	}
	st, _ = h.m.Status()
	if st.State != "error" || !strings.Contains(st.Error, "plugin") {
		t.Errorf("plugin: %+v", st)
	}
}

func TestCoastlineManager_ClearEmptiesTheStoreAndTheQueue(t *testing.T) {
	h := newCoastlineHarness(t)
	sq := coverageSquare{-21, 148}
	_ = h.store.SaveSquare(sq, nil, h.now)
	_ = h.store.RecordEdits(sq, 1, h.now)
	if _, err := h.m.Update(); err != nil {
		t.Fatal(err)
	}
	if err := h.m.Clear(); err != nil {
		t.Fatal(err)
	}
	recs, _ := h.store.Records()
	if len(recs) != 0 {
		t.Errorf("records left: %v", recs)
	}
	h.enabled = false
	if st, _ := h.m.Status(); st.Updating != 0 {
		t.Errorf("queue left: %+v", st)
	}
}

func TestCoastlineRetryDelayGrowsAndCaps(t *testing.T) {
	prev := time.Duration(0)
	for a := 1; a <= 4; a++ {
		d := coastlineRetryDelay(a)
		if d <= prev {
			t.Errorf("attempt %d: %v not above %v", a, d, prev)
		}
		prev = d
	}
	if d := coastlineRetryDelay(40); d != coastlineRetryMax {
		t.Errorf("cap: %v", d)
	}
}

func TestSettingsPayloadRoundTripsOfflineCoastline(t *testing.T) {
	if buildSettingsPayload(map[string]any{}).OfflineCoastline.Enabled {
		t.Fatal("Offline coastline is off until the operator turns it on")
	}
	settingsPath := writeTestSettings(t, "203.0.113.1", 3000)
	code, body := postSettings(t, settingsPath, func(p *settingsPayload) { p.OfflineCoastline.Enabled = true })
	if code != 200 {
		t.Fatalf("expected 200, got %d (%v)", code, body)
	}
	saved, err := readSettings(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !buildSettingsPayload(saved).OfflineCoastline.Enabled {
		t.Fatal("offline_coastline.enabled did not round-trip")
	}
	on, err := offlineCoastlineEnabledFromSettingsFile(settingsPath)()
	if err != nil || !on {
		t.Fatalf("reader: %v %v", on, err)
	}
	code, _ = postSettings(t, settingsPath, func(p *settingsPayload) { p.OfflineCoastline.Enabled = false })
	if code != 200 {
		t.Fatal("save off")
	}
	if on, _ := offlineCoastlineEnabledFromSettingsFile(settingsPath)(); on {
		t.Fatal("turning it off must stick")
	}
}
