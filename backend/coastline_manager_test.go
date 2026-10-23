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
	enMu    sync.Mutex // guards enabled while a step runs in another goroutine
}

func (h *coastlineHarness) setEnabled(v bool) {
	h.enMu.Lock()
	h.enabled = v
	h.enMu.Unlock()
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
		Enabled:  func() (bool, error) { h.enMu.Lock(); defer h.enMu.Unlock(); return h.enabled, nil },
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
	h.src.fail[wanted[0]] = errors.New("overpass query failed: runtime error: out of memory")
	h.m.step()
	rec, ok, _ := h.store.Record(wanted[0])
	if !ok || rec.Status != coastlineStatusFailed || rec.LastError != "overpass query failed: runtime error: out of memory" || rec.Attempts != 1 {
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
	if shown == nil || shown.State != "failed" || shown.Error != "overpass query failed: runtime error: out of memory" {
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
	h.src.checkErr = fmt.Errorf("overpass query failed: runtime error: out of memory")
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

// blockingCoastlineSource holds each plugin call until released, so a test
// can act while the call is in flight.
type blockingCoastlineSource struct {
	*fakeCoastlineSource
	started chan struct{}
	release chan struct{}
}

func newBlockingSource(f *fakeCoastlineSource) *blockingCoastlineSource {
	return &blockingCoastlineSource{fakeCoastlineSource: f, started: make(chan struct{}, 4), release: make(chan struct{})}
}

func (b *blockingCoastlineSource) ShorelineSquare(sq coverageSquare) ([]shorelineLine, error) {
	b.started <- struct{}{}
	<-b.release
	return b.fakeCoastlineSource.ShorelineSquare(sq)
}

func (b *blockingCoastlineSource) ShorelineChanges(sq coverageSquare, since time.Time) (int, error) {
	b.started <- struct{}{}
	<-b.release
	return b.fakeCoastlineSource.ShorelineChanges(sq, since)
}

// stepWhileBlocked runs one step against a blocked plugin call, runs during
// while the call is in flight, then releases the call and waits for the step.
func stepWhileBlocked(t *testing.T, h *coastlineHarness, during func()) {
	t.Helper()
	b := newBlockingSource(h.src)
	h.m.cfg.Source = func() (coastlineSource, error) { return b, nil }
	done := make(chan struct{})
	go func() { h.m.step(); close(done) }()
	select {
	case <-b.started:
	case <-time.After(5 * time.Second):
		t.Fatal("plugin call never started")
	}
	during()
	close(b.release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("step did not finish")
	}
}

func TestCoastlineManager_ClearDiscardsDownloadInFlight(t *testing.T) {
	h := newCoastlineHarness(t)
	stepWhileBlocked(t, h, func() {
		if err := h.m.Clear(); err != nil {
			t.Error(err)
		}
	})
	recs, err := h.store.Records()
	if err != nil || len(recs) != 0 {
		t.Errorf("a download that began before Clear must not be saved: %v %v", recs, err)
	}
}

func TestCoastlineManager_ClearDiscardsFailureInFlight(t *testing.T) {
	h := newCoastlineHarness(t)
	wanted, _ := squaresWithin(h.lat, h.lon, coastlineCoverageRadiusNm)
	h.src.fail[wanted[0]] = errors.New("overpass down")
	stepWhileBlocked(t, h, func() {
		if err := h.m.Clear(); err != nil {
			t.Error(err)
		}
	})
	recs, _ := h.store.Records()
	if len(recs) != 0 {
		t.Errorf("a failure that began before Clear must not leave a row: %v", recs)
	}
}

func TestCoastlineManager_ClearDiscardsEditCheckInFlight(t *testing.T) {
	h := newCoastlineHarness(t)
	h.fix = false
	sq := coverageSquare{Lat: -21, Lon: 148}
	if err := h.store.SaveSquare(sq, nil, h.now.Add(-30*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	h.src.edits[sq] = 3
	stepWhileBlocked(t, h, func() {
		if err := h.m.Clear(); err != nil {
			t.Error(err)
		}
	})
	recs, _ := h.store.Records()
	if len(recs) != 0 {
		t.Errorf("edit check racing Clear must not bring the square back: %v", recs)
	}
}

func TestCoastlineManager_ClearDiscardsQueuedUpdateDownload(t *testing.T) {
	h := newCoastlineHarness(t)
	h.fix = false
	sq := coverageSquare{Lat: -21, Lon: 148}
	if err := h.store.SaveSquare(sq, nil, h.now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := h.store.RecordEdits(sq, 2, h.now); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.Update(); err != nil {
		t.Fatal(err)
	}
	stepWhileBlocked(t, h, func() {
		if err := h.m.Clear(); err != nil {
			t.Error(err)
		}
	})
	recs, _ := h.store.Records()
	if len(recs) != 0 {
		t.Errorf("an update download racing Clear must be discarded: %v", recs)
	}
}

func TestCoastlineManager_SwitchOffDiscardsDownloadInFlight(t *testing.T) {
	h := newCoastlineHarness(t)
	stepWhileBlocked(t, h, func() { h.setEnabled(false) })
	recs, _ := h.store.Records()
	if len(recs) != 0 {
		t.Errorf("a download finishing after the switch went off must not be saved: %v", recs)
	}
}

const rateLimitedErr = "overpass rate limited (HTTP 429 with a non-JSON body, retry after 120s)"

func TestClassifyCoastlineError(t *testing.T) {
	for _, msg := range []string{
		"overpass rate limited",
		"overpass rate limited (HTTP 429 with a non-JSON body)",
		"overpass returned status 429",
		"overpass returned status 503, retry after 20s",
		"overpass returned status 504",
		"Post \"https://x\": dial tcp 1.2.3.4:443: connect: connection refused",
		"dial tcp: lookup overpass-api.de: no such host",
		"read tcp: i/o timeout",
		"the server is probably too busy",
	} {
		if classifyCoastlineError(errors.New(msg)) == nil {
			t.Errorf("%q is the whole service refusing, not one square", msg)
		}
	}
	for _, msg := range []string{
		"overpass query failed: runtime error: out of memory",
		"failed to parse overpass response: invalid character",
		"shoreline_square: way/1 has a node with no position",
		"overpass returned status 400",
	} {
		if classifyCoastlineError(errors.New(msg)) != nil {
			t.Errorf("%q is about the square", msg)
		}
	}
	if p := classifyCoastlineError(errors.New(rateLimitedErr)); p.RetryAfter != 120*time.Second {
		t.Errorf("retry after %v", p.RetryAfter)
	}
}

func TestCoastlineManager_RateLimitPausesTheWholeJobWithoutMarkingTheSquare(t *testing.T) {
	h := newCoastlineHarness(t)
	wanted, _ := squaresWithin(h.lat, h.lon, coastlineCoverageRadiusNm)
	h.src.fail[wanted[0]] = errors.New(rateLimitedErr)
	start := h.now

	h.m.step()
	if len(h.src.downloads) != 1 {
		t.Fatalf("downloads %v", h.src.downloads)
	}
	if _, ok, _ := h.store.Record(wanted[0]); ok {
		t.Error("a rate limit says nothing about the square; it must not be recorded as failed")
	}

	// Ten seconds later, the next square is NOT tried: the server just said no.
	h.now = start.Add(coastlineDownloadPause)
	if h.m.step() || len(h.src.downloads) != 1 {
		t.Errorf("worker kept hitting a rate-limiting server: %v", h.src.downloads)
	}
	st, _ := h.m.Status()
	if st.State != "paused" || !strings.Contains(st.PauseReason, "rate-limiting") || st.RetryAt == nil || !st.RetryAt.Equal(start.Add(coastlineRetryBase)) {
		t.Errorf("the page must say the server is rate-limiting and when it will retry: %+v", st)
	}
	if st.Failed != 0 {
		t.Errorf("no square failed: %d", st.Failed)
	}

	// After the wait the same square is tried again; a second refusal waits
	// twice as long.
	h.now = start.Add(coastlineRetryBase + time.Second)
	h.m.step()
	if len(h.src.downloads) != 2 || h.src.downloads[1] != wanted[0] {
		t.Fatalf("after the pause: %v", h.src.downloads)
	}
	st, _ = h.m.Status()
	if want := h.now.Add(2 * coastlineRetryBase); !st.RetryAt.Equal(want) {
		t.Errorf("second pause until %v, want %v", st.RetryAt, want)
	}

	// Recovery clears the pause.
	delete(h.src.fail, wanted[0])
	h.now = st.RetryAt.Add(time.Second)
	h.m.step()
	st, _ = h.m.Status()
	if st.State == "paused" || st.RetryAt != nil {
		t.Errorf("still paused after success: %+v", st)
	}
}

func TestCoastlineManager_RetryAfterLongerThanTheBackoffIsHonoured(t *testing.T) {
	h := newCoastlineHarness(t)
	wanted, _ := squaresWithin(h.lat, h.lon, coastlineCoverageRadiusNm)
	h.src.fail[wanted[0]] = errors.New("overpass rate limited (HTTP 429 with a non-JSON body, retry after 1800s)")
	h.m.step()
	st, _ := h.m.Status()
	if st.RetryAt == nil || !st.RetryAt.Equal(h.now.Add(30*time.Minute)) {
		t.Errorf("retry at %v, want 30 minutes", st.RetryAt)
	}
}

func TestCoastlineManager_RefusedUpdateStaysQueued(t *testing.T) {
	h := newCoastlineHarness(t)
	sq := coverageSquare{-21, 148}
	_ = h.store.SaveSquare(sq, sampleCoastLines(), h.now)
	_ = h.store.RecordEdits(sq, 5, h.now)
	h.src.fail[sq] = errors.New("overpass returned status 503")
	if _, err := h.m.Update(); err != nil {
		t.Fatal(err)
	}
	h.m.step()
	st, _ := h.m.Status()
	if st.Updating != 1 || st.State != "paused" {
		t.Errorf("the operator's update must wait out the pause: %+v", st)
	}
	delete(h.src.fail, sq)
	h.now = h.now.Add(coastlineRetryBase + time.Second)
	h.m.step()
	rec, _, _ := h.store.Record(sq)
	if rec.EditsSince != 0 {
		t.Errorf("update not applied after the pause: %+v", rec)
	}
}

func TestCoastlineManager_RefusedWeeklyCheckPausesInsteadOfRecordingFailure(t *testing.T) {
	h := newCoastlineHarness(t)
	sq := coverageSquare{-21, 148}
	_ = h.store.SaveSquare(sq, nil, h.now)
	h.now = h.now.Add(8 * 24 * time.Hour)
	h.src.checkErr = errors.New("overpass rate limited")
	h.fix = false
	h.m.step()
	rec, _, _ := h.store.Record(sq)
	if rec.LastError != "" {
		t.Errorf("a refused check is not the square's fault: %+v", rec)
	}
	st, _ := h.m.Status()
	if st.State != "paused" {
		t.Errorf("%+v", st)
	}
}

func TestCoastlineManager_StatusServesCachedStateInsteadOfFetchingEveryPoll(t *testing.T) {
	h := newCoastlineHarness(t)
	var posCalls, enCalls int
	cfg := h.m.cfg
	pos, en := cfg.Position, cfg.Enabled
	h.m.cfg.Position = func() (float64, float64, bool) { posCalls++; return pos() }
	h.m.cfg.Enabled = func() (bool, error) { enCalls++; return en() }

	h.m.step()
	p0, e0 := posCalls, enCalls
	for i := 0; i < 5; i++ {
		st, err := h.m.Status()
		if err != nil || !st.Enabled || !st.HasFix {
			t.Fatalf("%+v %v", st, err)
		}
	}
	if posCalls != p0 || enCalls != e0 {
		t.Errorf("Status fetched position %d times and the setting %d times", posCalls-p0, enCalls-e0)
	}

	// A settings save shows at once.
	h.setEnabled(false)
	h.m.SettingsChanged()
	if st, _ := h.m.Status(); st.Enabled || st.State != "off" {
		t.Errorf("after the save: %+v", st)
	}
}

func TestCoastlineManager_StatusCountsOnlyWantedFailuresAndKeepsFailedChecksHeld(t *testing.T) {
	h := newCoastlineHarness(t)
	wanted, _ := squaresWithin(h.lat, h.lon, coastlineCoverageRadiusNm)
	// A wanted square whose download failed and will be retried.
	_ = h.store.RecordFailure(wanted[0], "runtime error", h.now, h.now.Add(time.Hour))
	// A failed square far outside the ring: never retried, not counted.
	far := coverageSquare{40, 10}
	_ = h.store.RecordFailure(far, "runtime error", h.now, h.now.Add(time.Hour))
	// A held square whose weekly check failed.
	held := wanted[1]
	_ = h.store.SaveSquare(held, nil, h.now)
	_ = h.store.RecordFailure(held, "check failed", h.now, h.now.Add(time.Hour))

	h.m.refresh()
	st, _ := h.m.Status()
	if st.Failed != 1 {
		t.Errorf("failed %d, want only the wanted square awaiting retry", st.Failed)
	}
	if st.CheckFailed != 1 {
		t.Errorf("check_failed %d", st.CheckFailed)
	}
	for _, q := range st.Squares {
		if q.Lat == held.Lat && q.Lon == held.Lon {
			if q.State != "ready" || q.Error != "" || q.CheckError != "check failed" {
				t.Errorf("held square with a failed check: %+v", q)
			}
		}
	}
}
