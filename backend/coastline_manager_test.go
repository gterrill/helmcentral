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
	// downloadAsOf is the data date a download reports. Empty means a valid
	// default; noDownloadAsOf makes the download report none.
	downloadAsOf   string
	noDownloadAsOf bool
	// asOf is the data date an edit check reports. Empty means a date far
	// ahead of any test clock, so the data counts as current; noAsOf makes
	// the check report none at all, as an old plugin build does.
	asOf   string
	noAsOf bool
	// sinces records the since each edit check was asked about.
	sinces []time.Time
}

func (f *fakeCoastlineSource) ID() string              { return "osm-overpass" }
func (f *fakeCoastlineSource) SupportsShoreline() bool { return true }

func (f *fakeCoastlineSource) ShorelineSquare(sq coverageSquare) ([]shorelineLine, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.downloads = append(f.downloads, sq)
	if err := f.fail[sq]; err != nil {
		return nil, "", err
	}
	asOf := f.downloadAsOf
	if asOf == "" {
		asOf = "2026-10-02T00:00:00Z"
	}
	if f.noDownloadAsOf {
		asOf = ""
	}
	return f.lines, asOf, nil
}

func (f *fakeCoastlineSource) ShorelineChanges(sq coverageSquare, since time.Time) (int, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks = append(f.checks, sq)
	f.sinces = append(f.sinces, since)
	if f.checkErr != nil {
		return 0, "", f.checkErr
	}
	asOf := f.asOf
	if asOf == "" {
		asOf = "2099-01-01T00:00:00Z"
	}
	if f.noAsOf {
		asOf = ""
	}
	return f.edits[sq], asOf, nil
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
		Now:  func() time.Time { return h.now },
		Zone: func() *time.Location { return time.FixedZone("AEST", 10*3600) },
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
	wanted, _, err := coverageWanted(h.lat, h.lon)
	if err != nil {
		t.Fatal(err)
	}
	held := wanted[1]
	if err := h.store.SaveSquareAsOf(held, nil, h.now.Add(-time.Hour), coastlineTestDataAsOf); err != nil {
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
	_ = h.store.SaveSquareAsOf(sq, sampleCoastLines(), h.now, coastlineTestDataAsOf)
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
	_ = h.store.SaveSquareAsOf(other, nil, h.now, coastlineTestDataAsOf)
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
	_ = h.store.SaveSquareAsOf(coverageSquare{-21, 148}, nil, h.now, coastlineTestDataAsOf)
	_ = h.store.RecordEdits(coverageSquare{-21, 148}, 2, h.now)
	h.enabled = false
	if _, err := h.m.Update(); err == nil || !strings.Contains(err.Error(), "off") {
		t.Errorf("off: %v", err)
	}
}

func TestCoastlineManager_FailedUpdateKeepsOldSquareAndEdits(t *testing.T) {
	h := newCoastlineHarness(t)
	sq := coverageSquare{-21, 148}
	_ = h.store.SaveSquareAsOf(sq, sampleCoastLines(), h.now, coastlineTestDataAsOf)
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
	_ = h.store.SaveSquareAsOf(sq, nil, h.now, coastlineTestDataAsOf)
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
	_ = h.store.SaveSquareAsOf(sq, nil, h.now, coastlineTestDataAsOf)
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

func (b *blockingCoastlineSource) ShorelineSquare(sq coverageSquare) ([]shorelineLine, string, error) {
	b.started <- struct{}{}
	<-b.release
	return b.fakeCoastlineSource.ShorelineSquare(sq)
}

func (b *blockingCoastlineSource) ShorelineChanges(sq coverageSquare, since time.Time) (int, string, error) {
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
	if err := h.store.SaveSquareAsOf(sq, nil, h.now.Add(-30*24*time.Hour), coastlineTestDataAsOf); err != nil {
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
	if err := h.store.SaveSquareAsOf(sq, nil, h.now.Add(-time.Hour), coastlineTestDataAsOf); err != nil {
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
		"overpass returned status 403",
		"overpass returned status 401",
		"overpass returned status 451",
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
	_ = h.store.SaveSquareAsOf(sq, sampleCoastLines(), h.now, coastlineTestDataAsOf)
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
	_ = h.store.SaveSquareAsOf(sq, nil, h.now, coastlineTestDataAsOf)
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
	_ = h.store.SaveSquareAsOf(held, nil, h.now, coastlineTestDataAsOf)
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

func TestCoastlineManager_ClearDuringARefusedUpdateDoesNotRequeueTheSquare(t *testing.T) {
	for name, msg := range map[string]string{
		"rate limit": rateLimitedErr,
		"timeout":    "read tcp: i/o timeout",
	} {
		t.Run(name, func(t *testing.T) {
			h := newCoastlineHarness(t)
			sq := coverageSquare{-21, 148}
			_ = h.store.SaveSquareAsOf(sq, sampleCoastLines(), h.now, coastlineTestDataAsOf)
			_ = h.store.RecordEdits(sq, 5, h.now)
			if _, err := h.m.Update(); err != nil {
				t.Fatal(err)
			}
			h.src.fail[sq] = errors.New(msg)
			stepWhileBlocked(t, h, func() {
				if err := h.m.Clear(); err != nil {
					t.Error(err)
				}
			})
			if _, queued := h.m.nextQueued(); queued {
				t.Error("a square the operator cleared was queued again after the refusal")
			}
			if recs, _ := h.store.Records(); len(recs) != 0 {
				t.Errorf("a cleared square was saved: %v", recs)
			}
			if st, _ := h.m.Status(); st.State == "paused" {
				t.Errorf("a refusal from before the Clear must not pause the new job: %+v", st)
			}
		})
	}
}

func TestCoastlineManager_ServerRefusalPausesTheWholeJob(t *testing.T) {
	for _, msg := range []string{
		"overpass returned status 403",
		"overpass returned status 403: only available to white-listed usages",
		"overpass returned status 401",
		"overpass returned status 451",
	} {
		t.Run(msg, func(t *testing.T) {
			h := newCoastlineHarness(t)
			wanted, _ := squaresWithin(h.lat, h.lon, coastlineCoverageRadiusNm)
			h.src.fail[wanted[0]] = errors.New(msg)
			start := h.now
			h.m.step()
			st, _ := h.m.Status()
			if st.State != "paused" || st.Failed != 0 || st.RetryAt == nil {
				t.Fatalf("a refused boat pauses the job and fails no square: %+v", st)
			}
			if recs, _ := h.store.Records(); len(recs) != 0 {
				t.Errorf("no square may be recorded: %v", recs)
			}
			h.now = start.Add(coastlineDownloadPause)
			h.m.step()
			if len(h.src.downloads) != 1 {
				t.Errorf("worker kept asking a server that refused the boat: %v", h.src.downloads)
			}
		})
	}
}

func TestCoastlineManager_SwitchOffDuringARefusedUpdateDoesNotRequeueOrPause(t *testing.T) {
	h := newCoastlineHarness(t)
	sq := coverageSquare{-21, 148}
	_ = h.store.SaveSquareAsOf(sq, sampleCoastLines(), h.now, coastlineTestDataAsOf)
	_ = h.store.RecordEdits(sq, 5, h.now)
	if _, err := h.m.Update(); err != nil {
		t.Fatal(err)
	}
	h.src.fail[sq] = errors.New(rateLimitedErr)
	stepWhileBlocked(t, h, func() { h.setEnabled(false) })
	h.m.mu.Lock()
	queued, pausedUntil := len(h.m.queue), h.m.pause.until
	h.m.mu.Unlock()
	if queued != 0 || !pausedUntil.IsZero() {
		t.Errorf("a refusal after the switch went off must neither requeue nor pause: queue %d, paused until %v", queued, pausedUntil)
	}
}

func TestCoastlineManager_RequeueHappensInsideThePauseDecision(t *testing.T) {
	h := newCoastlineHarness(t)
	sq := coverageSquare{-21, 148}
	p := classifyCoastlineError(errors.New(rateLimitedErr))
	h.m.gateMu.Lock()
	gen := h.m.gen
	h.m.gateMu.Unlock()
	if !h.m.serviceFailed(gen, "test", p, h.now, &sq) {
		t.Fatal("a current refusal should be accepted")
	}
	if got, ok := h.m.nextQueued(); !ok || got != sq {
		t.Fatalf("the square must be queued before serviceFailed returns, got %v %v", got, ok)
	}
	_ = h.m.Clear()
	if h.m.serviceFailed(gen, "test", p, h.now, &sq) {
		t.Fatal("a stale refusal must be dropped")
	}
	if _, ok := h.m.nextQueued(); ok {
		t.Fatal("a stale refusal must not queue the square")
	}
}

func TestClassifyCoastlineErrorTurnedAwaySummaryPointsAtTheFix(t *testing.T) {
	for _, msg := range []string{"overpass returned status 403", "overpass returned status 401", "overpass returned status 451"} {
		p := classifyCoastlineError(errors.New(msg))
		if p == nil || !strings.Contains(p.Summary, "turned the boat away") || !strings.Contains(p.Summary, "another Overpass server") {
			t.Errorf("%s: summary %+v", msg, p)
		}
	}
	if p := classifyCoastlineError(errors.New("connection refused")); strings.Contains(p.Summary, "turned") {
		t.Errorf("a network refusal keeps the network wording: %q", p.Summary)
	}
}

func TestCoastlineManager_NeverDownloadsInlandSquaresAndCountsThem(t *testing.T) {
	h := newCoastlineHarness(t)
	wanted, skipped, err := coverageWanted(h.lat, h.lon)
	if err != nil || skipped == 0 {
		t.Fatalf("wanted %d skipped %d err %v: the harness position must reach inland squares", len(wanted), skipped, err)
	}
	for h.m.step() {
	}
	for _, sq := range h.src.downloads {
		if squareInland(sq) {
			t.Errorf("downloaded inland square %v", sq)
		}
	}
	if len(h.src.downloads) != len(wanted) {
		t.Errorf("downloaded %d squares, want %d", len(h.src.downloads), len(wanted))
	}
	st, _ := h.m.Status()
	if st.Wanted != len(wanted) || st.WantedHeld != len(wanted) || st.InlandSkipped != skipped || st.State != "idle" {
		t.Errorf("status wanted %d held %d skipped %d state %s", st.Wanted, st.WantedHeld, st.InlandSkipped, st.State)
	}
}

func TestCoastlineManager_AnInlandSquareAlreadyHeldStaysHeld(t *testing.T) {
	h := newCoastlineHarness(t)
	inland := coverageSquare{Lat: -24, Lon: 133}
	if err := h.store.SaveSquareAsOf(inland, nil, h.now.Add(-time.Hour), coastlineTestDataAsOf); err != nil {
		t.Fatal(err)
	}
	for h.m.step() {
	}
	st, _ := h.m.Status()
	if st.Held == 0 {
		t.Fatal("nothing held")
	}
	if rec, ok, _ := h.store.Record(inland); !ok || rec.Status != coastlineStatusReady {
		t.Errorf("held inland square must stay: %+v", rec)
	}
}

func TestCoastlineManager_DropsFailedInlandRecordsButKeepsHeldOnes(t *testing.T) {
	h := newCoastlineHarness(t)
	failedInland := coverageSquare{Lat: -24, Lon: 133}
	heldInland := coverageSquare{Lat: -24, Lon: 134}
	if !squareInland(failedInland) || !squareInland(heldInland) {
		t.Fatal("fixture squares must be inland")
	}
	_ = h.store.RecordFailure(failedInland, "runtime error", h.now, h.now.Add(time.Hour))
	_ = h.store.SaveSquareAsOf(heldInland, nil, h.now.Add(-time.Hour), coastlineTestDataAsOf)
	h.m.step()
	if _, ok, _ := h.store.Record(failedInland); ok {
		t.Error("a failed inland square holds nothing and must be dropped")
	}
	if rec, ok, _ := h.store.Record(heldInland); !ok || rec.Status != coastlineStatusReady {
		t.Errorf("held inland square must stay: %+v", rec)
	}
}

func TestCoastlineManager_NoWeeklyCheckForInlandSquares(t *testing.T) {
	h := newCoastlineHarness(t)
	inland := coverageSquare{Lat: -24, Lon: 133}
	_ = h.store.SaveSquareAsOf(inland, nil, h.now.Add(-30*24*time.Hour), coastlineTestDataAsOf)
	for h.m.step() {
	}
	for _, sq := range h.src.checks {
		if squareInland(sq) {
			t.Errorf("weekly check asked Overpass about inland square %v", sq)
		}
	}
}

// heldDueForCheck holds a square whose data is dataAsOf, downloaded 8 days
// ago so its edit check is due, and returns it.
func heldDueForCheck(t *testing.T, h *coastlineHarness, sq coverageSquare, dataAsOf time.Time) {
	t.Helper()
	if err := h.store.SaveSquareAsOf(sq, nil, h.now, dataAsOf); err != nil {
		t.Fatal(err)
	}
}

func TestCoastlineManager_StaleServerPausesOnlyTheEditCheck(t *testing.T) {
	h := newCoastlineHarness(t)
	sq := coverageSquare{-21, 148}
	dataDate := time.Date(2026, 10, 1, 4, 0, 0, 0, time.UTC)
	heldDueForCheck(t, h, sq, dataDate)
	downloaded := h.now
	h.now = h.now.Add(8 * 24 * time.Hour)
	// The server's own data is 10 days old: its updates have stopped.
	h.src.asOf = h.now.Add(-10 * 24 * time.Hour).Format(time.RFC3339)
	h.fix = false
	h.m.step()
	rec, _, _ := h.store.Record(sq)
	if rec.LastError != "" || !rec.CheckedAt.Equal(downloaded) || rec.EditsSince != 0 {
		t.Errorf("a stale server is not the square's fault and must not count as a check: %+v", rec)
	}
	if len(h.src.sinces) != 1 || !h.src.sinces[0].Equal(dataDate) {
		t.Errorf("the check must ask about edits since the square's data date, got %v", h.src.sinces)
	}
	st, _ := h.m.Status()
	if st.State == "paused" {
		t.Errorf("a stale edit check is not a pause of the whole job: %+v", st)
	}
	// 04:00 UTC on the 30th is 14:00 AEST on the 30th; the boat's zone
	// decides the date shown.
	if !strings.Contains(st.CheckPauseReason, "osm-overpass") || !strings.Contains(st.CheckPauseReason, "daily updates") || st.CheckRetryAt == nil {
		t.Errorf("want the edit check paused naming the plugin: %+v", st)
	}

	// A boat entering a new square still gets it downloaded.
	h.fix = true
	before := len(h.src.downloads)
	for i := 0; i < 3 && len(h.src.downloads) == before; i++ {
		h.m.step()
	}
	if len(h.src.downloads) == before {
		t.Fatal("downloads of new squares must carry on while the edit check is paused")
	}
	if len(h.src.checks) != 1 {
		t.Errorf("the paused edit check must not run again before its retry time: %d checks", len(h.src.checks))
	}

	// Once the server is current, the check resumes after the retry time.
	h.now = h.now.Add(7 * time.Hour)
	h.src.asOf = h.now.Add(-time.Hour).Format(time.RFC3339)
	for i := 0; i < 60 && len(h.src.checks) < 2; i++ {
		h.m.step()
	}
	if len(h.src.checks) < 2 {
		t.Fatal("the edit check must resume after the pause")
	}
	st, _ = h.m.Status()
	if st.CheckPauseReason != "" {
		t.Errorf("a good check clears the pause: %+v", st)
	}
}

func TestCoastlineManager_StaleMessageUsesTheBoatsTimeZone(t *testing.T) {
	h := newCoastlineHarness(t)
	heldDueForCheck(t, h, coverageSquare{-21, 148}, h.now.Add(-time.Hour))
	h.now = h.now.Add(8 * 24 * time.Hour)
	// 20:00 UTC on 25 Sep is 06:00 on 26 Sep in AEST.
	h.src.asOf = "2026-09-25T20:00:00Z"
	h.fix = false
	h.m.step()
	st, _ := h.m.Status()
	if !strings.Contains(st.CheckPauseReason, "26 Sep 2026") {
		t.Errorf("want the date in the boat's zone: %q", st.CheckPauseReason)
	}
}

func TestCoastlineManager_SquareNewerThanTheLastUpdateDoesNotBlockAnOlderOne(t *testing.T) {
	h := newCoastlineHarness(t)
	older, newer := coverageSquare{-21, 148}, coverageSquare{-21, 149}
	serverDate := time.Date(2026, 10, 10, 4, 0, 0, 0, time.UTC)
	h.now = time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	// "newer" was downloaded after the last update: it holds the server's own
	// date. "older" holds a date a week before. newer is the least recently
	// checked, so it comes first.
	_ = h.store.SaveSquareAsOf(newer, nil, h.now.Add(-9*24*time.Hour), serverDate)
	_ = h.store.SaveSquareAsOf(older, nil, h.now.Add(-8*24*time.Hour), serverDate.Add(-7*24*time.Hour))
	h.src.asOf = serverDate.Format(time.RFC3339)
	h.src.edits[older] = 2
	h.fix = false
	h.m.step()
	recNewer, _, _ := h.store.Record(newer)
	if recNewer.LastError != "" || !recNewer.CheckedAt.Equal(h.now.Add(-9*24*time.Hour)) {
		t.Errorf("nothing to count yet is not a check: %+v", recNewer)
	}
	st, _ := h.m.Status()
	if st.CheckPauseReason != "" || st.State == "paused" {
		t.Errorf("a square the server has not passed is not a server problem: %+v", st)
	}
	h.m.step()
	recOlder, _, _ := h.store.Record(older)
	if recOlder.EditsSince != 2 {
		t.Errorf("the older square must be checked right after: %+v", recOlder)
	}
}

// A square held from before data dates were kept, as after an upgrade.
func heldWithoutDataDate(t *testing.T, h *coastlineHarness, sq coverageSquare) {
	t.Helper()
	if err := h.store.SaveSquareAsOf(sq, sampleCoastLines(), h.now, coastlineTestDataAsOf); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.db.Exec(`UPDATE coastline_squares SET data_as_of = 0 WHERE lat = ? AND lon = ?`, sq.Lat, sq.Lon); err != nil {
		t.Fatal(err)
	}
}

func TestCoastlineManager_EditCheckSkipsASquareWithoutDataDate(t *testing.T) {
	h := newCoastlineHarness(t)
	sq := coverageSquare{-21, 148}
	heldWithoutDataDate(t, h, sq)
	// Downloads fail, so only a check could have reached the plugin with a since.
	h.src.fail[sq] = errors.New("no route to host")
	h.now = h.now.Add(8 * 24 * time.Hour)
	h.fix = false
	h.m.step()
	if len(h.src.checks) != 0 {
		t.Errorf("the edit check must never run on a square without a data date: %v %v", h.src.checks, h.src.sinces)
	}
}

func TestCoastlineManager_UpgradeOfflineKeepsTheSquareUsable(t *testing.T) {
	h := newCoastlineHarness(t)
	sq := coverageSquare{-21, 148}
	heldWithoutDataDate(t, h, sq)
	h.src.fail[sq] = errors.New("dial tcp: no route to host")
	h.fix = false
	h.m.step()
	rec, ok, _ := h.store.Record(sq)
	if !ok || rec.Status != coastlineStatusReady || !rec.DataAsOf.IsZero() {
		t.Fatalf("the square must stay held and unrefreshed: %+v", rec)
	}
	lines, err := h.store.Lines([]coverageSquare{sq})
	if err != nil || len(lines) == 0 {
		t.Fatalf("routing and shelter must still read the square: %v %v", lines, err)
	}
	st, _ := h.m.Status()
	if st.AwaitingRefresh != 1 || st.Held != 1 {
		t.Errorf("status must show the square held and awaiting refresh: %+v", st)
	}
	var found bool
	for _, q := range st.Squares {
		if q.Lat == sq.Lat && q.Lon == sq.Lon {
			found = q.State == coastlineStatusReady && q.AwaitingRefresh
			if q.CheckError != "" || !strings.Contains(q.RefreshError, "no route") {
				t.Errorf("a failed refresh is a refresh error, not an edit-check error: %+v", q)
			}
		}
	}
	if !found {
		t.Errorf("the square must be listed as ready and awaiting refresh: %+v", st.Squares)
	}
	if st.CheckFailed != 0 || st.RefreshFailed != 1 {
		t.Errorf("a failed refresh must be counted as one: check_failed %d, refresh_failed %d", st.CheckFailed, st.RefreshFailed)
	}
}

func TestCoastlineManager_RefreshSkipsInlandSquaresAndTakesInRangeFirst(t *testing.T) {
	h := newCoastlineHarness(t)
	h.fix = true
	inRange := coverageSquare{-21, 148}
	farOff := coverageSquare{-33, 151}
	var inland coverageSquare
	found := false
	for lat := -30; lat < -20 && !found; lat++ {
		for lon := 130; lon < 145; lon++ {
			if sq := (coverageSquare{lat, lon}); squareInland(sq) {
				inland, found = sq, true
				break
			}
		}
	}
	if !found {
		t.Skip("no inland square in the search window")
	}
	heldWithoutDataDate(t, h, inland)
	heldWithoutDataDate(t, h, farOff)
	heldWithoutDataDate(t, h, inRange)
	// Everything wanted is held already, so only refreshes remain.
	wanted, _, _ := coverageWanted(h.lat, h.lon)
	for _, sq := range wanted {
		if sq != inRange {
			_ = h.store.SaveSquareAsOf(sq, nil, h.now, coastlineTestDataAsOf)
		}
	}
	h.m.step()
	if len(h.src.downloads) != 1 || h.src.downloads[0] != inRange {
		t.Fatalf("the in-range square must refresh first: %v", h.src.downloads)
	}
	h.m.step()
	h.m.step()
	for _, d := range h.src.downloads {
		if d == inland {
			t.Errorf("an all-land square is not refreshed: %v", h.src.downloads)
		}
	}
}

func TestCoastlineManager_SquareWithoutDataDateRefreshesWhenDownloadsWork(t *testing.T) {
	h := newCoastlineHarness(t)
	sq := coverageSquare{-21, 148}
	heldWithoutDataDate(t, h, sq)
	h.fix = false
	h.m.step()
	rec, _, _ := h.store.Record(sq)
	if rec.DataAsOf.IsZero() || len(h.src.downloads) != 1 {
		t.Fatalf("the square must be downloaded again and take the server's data date: %+v downloads %v", rec, h.src.downloads)
	}
	if st, _ := h.m.Status(); st.AwaitingRefresh != 0 {
		t.Errorf("nothing awaits refresh now: %+v", st)
	}
}

func TestCoastlineManager_CheckPauseReasonOutlivesItsRetryTime(t *testing.T) {
	h := newCoastlineHarness(t)
	heldDueForCheck(t, h, coverageSquare{-21, 148}, coastlineTestDataAsOf)
	h.now = h.now.Add(8 * 24 * time.Hour)
	h.src.noAsOf = true
	h.fix = false
	h.m.step()
	st, _ := h.m.Status()
	if st.CheckPauseReason == "" || st.CheckRetryAt == nil {
		t.Fatalf("setup: %+v", st)
	}
	h.now = st.CheckRetryAt.Add(time.Minute)
	st, _ = h.m.Status()
	if st.CheckPauseReason == "" || !st.CheckRetryAt.Before(h.now) {
		t.Errorf("the reason must stay until a check succeeds: %+v", st)
	}
	h.src.noAsOf = false
	h.m.step()
	st, _ = h.m.Status()
	if st.CheckPauseReason != "" {
		t.Errorf("a good check clears it: %+v", st)
	}
}

func TestCoastlineManager_DeferredCheckClearsAnEarlierFailure(t *testing.T) {
	h := newCoastlineHarness(t)
	sq := coverageSquare{-21, 148}
	serverDate := h.now.Add(8*24*time.Hour - time.Hour) // current when the check runs
	heldDueForCheck(t, h, sq, serverDate)
	_ = h.store.RecordFailure(sq, "earlier", h.now, h.now)
	h.now = h.now.Add(8 * 24 * time.Hour)
	h.src.asOf = serverDate.Format(time.RFC3339) // nothing newer yet
	h.fix = false
	h.m.step()
	rec, _, _ := h.store.Record(sq)
	if rec.LastError != "" || rec.Attempts != 0 {
		t.Errorf("the check reached a working server: %+v", rec)
	}
}

func TestCoastlineManager_DownloadRecordsTheServersDataDate(t *testing.T) {
	h := newCoastlineHarness(t)
	h.src.downloadAsOf = "2026-10-30T04:20:00Z"
	h.fix = true
	h.m.step()
	recs, _ := h.store.Records()
	if len(recs) == 0 || !recs[0].DataAsOf.Equal(time.Date(2026, 10, 30, 4, 20, 0, 0, time.UTC)) {
		t.Errorf("the square must keep the server's data date: %+v", recs)
	}
}

func TestCoastlineManager_DownloadWithoutDataDateFailsExplicitly(t *testing.T) {
	h := newCoastlineHarness(t)
	h.src.noDownloadAsOf = true
	h.fix = true
	h.m.step()
	recs, _ := h.store.Records()
	if len(recs) != 0 {
		t.Errorf("nothing may be stored without a data date: %+v", recs)
	}
	st, _ := h.m.Status()
	if st.State != "paused" || !strings.Contains(st.PauseReason, "Neither the osm-overpass plugin nor the Overpass server") {
		t.Errorf("want one explicit problem, not a failure per square: %+v", st)
	}
}

func TestCoastlineManager_EditCheckWithoutDataDateIsOneNeutralProblem(t *testing.T) {
	h := newCoastlineHarness(t)
	for _, sq := range []coverageSquare{{-21, 148}, {-21, 149}} {
		heldDueForCheck(t, h, sq, coastlineTestDataAsOf)
	}
	h.now = h.now.Add(8 * 24 * time.Hour)
	h.src.noAsOf = true
	h.fix = false
	h.m.step()
	recs, _ := h.store.Records()
	for _, r := range recs {
		if r.LastError != "" {
			t.Errorf("no failure may be recorded on a square: %+v", r)
		}
	}
	st, _ := h.m.Status()
	if !strings.Contains(st.CheckPauseReason, "Neither the osm-overpass plugin nor the Overpass server gave the date") || !strings.Contains(st.CheckPauseReason, "still be importing") {
		t.Errorf("want the neutral wording naming the plugin: %+v", st)
	}
	if len(h.src.checks) != 1 {
		t.Errorf("the second square must not be tried: %d checks", len(h.src.checks))
	}
}

func TestCoastlineManager_ServerWithoutMetadataPausesOnlyTheEditCheck(t *testing.T) {
	h := newCoastlineHarness(t)
	sq := coverageSquare{-21, 148}
	heldDueForCheck(t, h, sq, coastlineTestDataAsOf)
	h.now = h.now.Add(8 * 24 * time.Hour)
	h.src.checkErr = &coastlineEditCheckCodeError{Code: "no_metadata", Detail: "Tried to use meta file but no meta files available"}
	h.fix = false
	h.m.step()
	rec, _, _ := h.store.Record(sq)
	if rec.LastError != "" {
		t.Errorf("no failure on the square: %+v", rec)
	}
	st, _ := h.m.Status()
	if st.State == "paused" || !strings.Contains(st.CheckPauseReason, "OVERPASS_META=yes") || !strings.Contains(st.CheckPauseReason, "Re-import") {
		t.Errorf("want the edit check paused with the re-import fix: %+v", st)
	}
	if !strings.Contains(st.CheckPauseDetail, "Tried to use meta file") {
		t.Errorf("the server's remark must stay in the detail: %q", st.CheckPauseDetail)
	}
}

func TestParseWasmShorelineChanges_NoMetadataCode(t *testing.T) {
	// The exact output of the osm-overpass plugin for a server without
	// metadata, copied from the plugin test's log
	// (TestRunShorelineChanges_NoMetadataIsAnErrorCodeNotACount).
	out := []byte(`{"error_code":"no_metadata","detail":"overpass server has no metadata files: runtime error: Tried to use meta file but no meta files available","data_as_of":""}`)
	_, _, err := parseWasmShorelineChanges("osm-overpass", out)
	if p := editCheckProblem(err); p == nil || !strings.Contains(p.Summary, "OVERPASS_META=yes") {
		t.Fatalf("the code must classify as the no-metadata problem: %v", err)
	}
	n, asOf, err := parseWasmShorelineChanges("osm-overpass", []byte(`{"changed":0,"data_as_of":"2026-10-27T04:00:00Z"}`))
	if err != nil || n != 0 || asOf != "2026-10-27T04:00:00Z" {
		t.Errorf("zero edits is a real answer: %d %q %v", n, asOf, err)
	}
	if _, _, err := parseWasmShorelineChanges("osm-overpass", []byte(`{"data_as_of":"2026-10-27T04:00:00Z"}`)); err == nil {
		t.Error("no count and no code is an error")
	}
}

func TestParseCoastlineDataAsOf(t *testing.T) {
	if got, err := parseCoastlineDataAsOf("2026-10-27T04:00:00Z"); err != nil || !got.Equal(time.Date(2026, 10, 27, 4, 0, 0, 0, time.UTC)) {
		t.Errorf("good date: %v %v", got, err)
	}
	for _, bad := range []string{"", "soon"} {
		if _, err := parseCoastlineDataAsOf(bad); err == nil {
			t.Errorf("%q must be an error", bad)
		}
	}
}
