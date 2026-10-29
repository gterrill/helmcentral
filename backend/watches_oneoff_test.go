package main

import (
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// A timed watch is a saved one-off watch (ADR 0227): it survives a restart and
// its report is kept as a run.

type oneOffFixture struct {
	h     *watchTestHarness
	docs  *documentStore
	store *watchOneOffStore
}

func newOneOffFixture(t *testing.T) *oneOffFixture {
	t.Helper()
	env := newProposalEnv(t)
	h := newWatchTestHarness(t)
	h.readings["a.one"] = constantReading(12.5)
	store := newWatchOneOffStore(func() *documentStore { return env.docs })
	h.reg.deps.persist = store
	return &oneOffFixture{h: h, docs: env.docs, store: store}
}

func (f *oneOffFixture) watches(t *testing.T) []savedWatch {
	t.Helper()
	var list []savedWatch
	if err := f.docs.Read(func(q sqlQueryer) error {
		var err error
		list, err = listSavedWatchesQ(q)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return list
}

func (f *oneOffFixture) runs(t *testing.T, id string) []watchRun {
	t.Helper()
	var runs []watchRun
	if err := f.docs.Read(func(q sqlQueryer) error {
		var err error
		runs, err = watchRunsQ(q, id, 10, 0)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return runs
}

const oneOffArgs = `{"paths":[{"path":"a.one","label":"House voltage"}],"minutes":5,"reason":"look for dips"}`

func TestOneOffWatch_StartSavesIt(t *testing.T) {
	f := newOneOffFixture(t)
	info, err := f.h.reg.start(watchRequest("conv-1", oneOffArgs))
	if err != nil {
		t.Fatal(err)
	}
	list := f.watches(t)
	if len(list) != 1 {
		t.Fatalf("watches %+v", list)
	}
	w := list[0]
	if w.ID != info.ID || w.Kind != watchKindOneOff || w.Name != "House voltage" || w.DurationMinutes != 5 ||
		w.ConversationID != "conv-1" || !w.Enabled || w.EndsAt == nil || !w.EndsAt.Equal(info.EndsAt) || len(w.Sections) != 0 {
		t.Fatalf("saved watch %+v", w)
	}
	if entry := watchScheduleLabel(w); entry != "One-off · 5 minutes" {
		t.Errorf("label %q", entry)
	}
}

func TestOneOffWatch_ARefusedSaveStopsItStarting(t *testing.T) {
	f := newOneOffFixture(t)
	if _, err := f.docs.db.Exec(`DROP TABLE watches`); err != nil {
		t.Fatal(err)
	}
	_, err := f.h.reg.start(watchRequest("conv-1", oneOffArgs))
	if err == nil || !strings.Contains(err.Error(), "could not be saved") {
		t.Fatalf("a watch that cannot be saved does not run: %v", err)
	}
	if _, ok := f.h.reg.get("conv-1"); ok {
		t.Fatal("nothing is registered")
	}
}

func TestOneOffWatch_ItsReportIsKeptAsARun(t *testing.T) {
	f := newOneOffFixture(t)
	info, err := f.h.reg.start(watchRequest("conv-1", `{"paths":[{"path":"a.one","label":"House voltage"}],"minutes":1,"reason":"look for dips"}`))
	if err != nil {
		t.Fatal(err)
	}
	f.h.tickThrough(f.h.ticker(0), 60)
	f.h.waitFinished()

	w := f.watches(t)[0]
	if w.Enabled || w.LastRunAt == nil || f.sampleCount(t, info.ID) != 0 {
		t.Fatalf("an ended watch is switched off with its checkpoint cleared: %+v", w)
	}
	runs := f.runs(t, info.ID)
	if len(runs) != 1 || runs[0].Outcome != watchOutcomeRan || runs[0].ReportKind != watchReportKindSamples ||
		!strings.Contains(string(runs[0].Report), "House voltage") || runs[0].Review != nil {
		t.Fatalf("runs %+v", runs)
	}
}

func TestOneOffWatch_StoppingItLeavesASkippedRun(t *testing.T) {
	f := newOneOffFixture(t)
	info, _ := f.h.reg.start(watchRequest("conv-1", oneOffArgs))
	if stopped, err := f.h.reg.stop("conv-1"); !stopped || err != nil {
		t.Fatalf("stop: %v %v", stopped, err)
	}
	w := f.watches(t)[0]
	runs := f.runs(t, info.ID)
	if w.Enabled || len(runs) != 1 || runs[0].Outcome != watchOutcomeSkipped || !strings.Contains(runs[0].SkipReason, "Stopped") {
		t.Fatalf("watch %+v runs %+v", w, runs)
	}
}

func TestOneOffWatch_ResumesAfterARestartFromItsCheckpoint(t *testing.T) {
	f := newOneOffFixture(t)
	info, _ := f.h.reg.start(watchRequest("conv-1", `{"paths":[{"path":"a.one","label":"House voltage"}],"minutes":5,"reason":"look for dips"}`))
	f.h.tickThrough(f.h.ticker(0), assistantWatchCheckpointEvery+1) // two minutes sampled, then saved
	waitUntil(t, "the samples to be checkpointed", func() bool { return f.sampleCount(t, info.ID) == assistantWatchCheckpointEvery })

	// Helmcentral restarts 3 minutes in; a fresh registry resumes the watch.
	h2 := newWatchTestHarness(t)
	h2.readings["a.one"] = constantReading(12.5)
	h2.t0 = f.h.t0.Add(3 * time.Minute)
	h2.reg.deps.persist = f.store
	n, ended, err := resumeOneOffWatches(h2.reg, f.docs, h2.t0)
	if err != nil || n != 1 || ended != 0 {
		t.Fatalf("resumed %d ended %d err %v", n, ended, err)
	}
	got, ok := h2.reg.get("conv-1")
	if !ok || got.ID != info.ID || got.Status != assistantWatchStatusWatching || !got.EndsAt.Equal(info.EndsAt) {
		t.Fatalf("resumed watch %+v ok %v", got, ok)
	}
	// It samples to its original end, then reports.
	for i := 1; i <= 2*60; i++ {
		h2.ticker(0).ch <- h2.t0.Add(time.Duration(i) * time.Second)
	}
	fin := h2.waitFinished()
	s := fin.report.Series[0]
	if s.Ticks < 290 || s.GapSeconds < 55 {
		t.Fatalf("the report covers the whole watch and shows the minute Helmcentral was down as a gap: ticks %d gap %d", s.Ticks, s.GapSeconds)
	}
	if len(f.runs(t, info.ID)) != 1 {
		t.Fatal("the finished watch is stored once")
	}
}

func TestOneOffWatch_EndedWhileDownIsRecordedAndSaysSo(t *testing.T) {
	f := newOneOffFixture(t)
	info, _ := f.h.reg.start(watchRequest("conv-1", `{"paths":[{"path":"a.one","label":"House voltage"}],"minutes":3,"reason":"look for dips"}`))
	f.h.tickThrough(f.h.ticker(0), assistantWatchCheckpointEvery+1)

	h2 := newWatchTestHarness(t)
	h2.t0 = f.h.t0.Add(30 * time.Minute)
	h2.reg.deps.persist = f.store
	n, ended, err := resumeOneOffWatches(h2.reg, f.docs, h2.t0)
	if err != nil || n != 0 || ended != 1 {
		t.Fatalf("resumed %d ended %d err %v", n, ended, err)
	}
	fin := h2.waitFinished()
	if !strings.Contains(fin.report.Note, "Helmcentral was not running") {
		t.Fatalf("the report says so: %q", fin.report.Note)
	}
	runs := f.runs(t, info.ID)
	if len(runs) != 1 || runs[0].Outcome != watchOutcomeRan || !strings.Contains(string(runs[0].Report), "was not running") {
		t.Fatalf("runs %+v", runs)
	}
	if f.watches(t)[0].Enabled {
		t.Error("it is over")
	}
	// Run again: nothing is left to resume.
	if n, ended, _ := resumeOneOffWatches(newWatchTestHarness(t).reg, f.docs, h2.t0); n+ended != 0 {
		t.Errorf("an ended watch is not resumed twice: %d %d", n, ended)
	}
}

func TestOneOffWatch_StoredTextNeverExceedsTheLimits(t *testing.T) {
	f := newOneOffFixture(t)
	now := f.h.t0
	info := assistantWatchInfo{
		ID: "w-long", ConversationID: "conv-1", Subject: strings.Repeat("ü", 90), Minutes: 5, StartedAt: now, EndsAt: now.Add(5 * time.Minute),
		Reason: strings.Repeat("é", 600), Today: now, Location: time.UTC,
	}
	if err := f.store.Start(info); err != nil {
		t.Fatal(err)
	}
	w := f.watches(t)[0]
	if n := utf8.RuneCountInString(w.LookOutFor); n != maxWatchLookOutForChars || strings.Contains(w.LookOutFor, "...") {
		t.Errorf("look_out_for is cut to %d characters with no ellipsis, got %d", maxWatchLookOutForChars, n)
	}
	if n := utf8.RuneCountInString(w.Name); n != maxWatchNameChars {
		t.Errorf("name has %d characters", n)
	}
}

// ── review fixes ────────────────────────────────────────────────────────

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (f *oneOffFixture) samples(t *testing.T, id string, paths int) [][]assistantWatchSample {
	t.Helper()
	var series [][]assistantWatchSample
	if err := f.docs.Read(func(q sqlQueryer) error {
		var err error
		series, err = loadWatchSamplesQ(q, id, paths)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return series
}

func (f *oneOffFixture) sampleCount(t *testing.T, id string) int {
	t.Helper()
	return len(f.samples(t, id, 1)[0])
}

func TestOneOffWatch_AnEndThatCannotBeSavedIsStillNeverReportedTwice(t *testing.T) {
	f := newOneOffFixture(t)
	info, _ := f.h.reg.start(watchRequest("conv-1", `{"paths":[{"path":"a.one","label":"House voltage"}],"minutes":1,"reason":"look for dips"}`))
	// The run cannot be saved, so the end cannot be, but the row can be marked.
	if _, err := f.docs.db.Exec(`DROP TABLE watch_runs`); err != nil {
		t.Fatal(err)
	}
	f.h.tickThrough(f.h.ticker(0), 60)
	f.h.waitFinished() // the report still goes to the chat
	w := f.watches(t)[0]
	if w.Enabled || w.ReportedAt == nil {
		t.Fatalf("the row is marked ended and reported so a restart does not report it again: %+v", w)
	}
	h2 := newWatchTestHarness(t)
	h2.reg.deps.persist = f.store
	if n, ended, err := resumeOneOffWatches(h2.reg, f.docs, f.h.t0.Add(time.Hour)); err != nil || n+ended != 0 {
		t.Fatalf("resumed %d ended %d err %v", n, ended, err)
	}
	_ = info
}

func TestOneOffWatch_IfEvenMarkingFailsThisProcessRemembersItReported(t *testing.T) {
	f := newOneOffFixture(t)
	info, _ := f.h.reg.start(watchRequest("conv-1", `{"paths":[{"path":"a.one","label":"House voltage"}],"minutes":1,"reason":"look for dips"}`))
	for _, stmt := range []string{
		`DROP TABLE watch_runs`,
		`CREATE TRIGGER refuse_update BEFORE UPDATE ON watches BEGIN SELECT RAISE(FAIL, 'disk full'); END`,
	} {
		if _, err := f.docs.db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	f.h.tickThrough(f.h.ticker(0), 60)
	f.h.waitFinished()
	if !f.store.Reported(info.ID) {
		t.Fatal("the in-memory flag says it was reported")
	}
	h2 := newWatchTestHarness(t)
	h2.reg.deps.persist = f.store
	if n, ended, _ := resumeOneOffWatches(h2.reg, f.docs, f.h.t0.Add(time.Hour)); n+ended != 0 {
		t.Fatalf("not reported again by this process: resumed %d ended %d", n, ended)
	}
}

func TestOneOffWatch_IsMarkedReportedBeforeTheFollowUpIsPosted(t *testing.T) {
	f := newOneOffFixture(t)
	f.h.reg.deps.finish = func(info assistantWatchInfo, report assistantWatchReport, release func()) {
		w := f.watches(t)[0]
		if w.ReportedAt == nil || w.Enabled {
			t.Errorf("when the follow-up is being posted the row already says it was reported: %+v", w)
		}
		f.h.finished <- assistantWatchFinished{info: info, report: report}
	}
	f.h.reg.start(watchRequest("conv-1", `{"paths":[{"path":"a.one","label":"House voltage"}],"minutes":1,"reason":"look for dips"}`))
	f.h.tickThrough(f.h.ticker(0), 60)
	f.h.waitFinished()

	// A restart after the report reached the chat finds nothing to report,
	// even if the row were somehow still on.
	if _, err := f.docs.db.Exec(`UPDATE watches SET enabled = 1`); err != nil {
		t.Fatal(err)
	}
	h2 := newWatchTestHarness(t)
	h2.reg.deps.persist = f.store
	if n, ended, err := resumeOneOffWatches(h2.reg, f.docs, f.h.t0.Add(time.Hour)); err != nil || n+ended != 0 {
		t.Fatalf("resumed %d ended %d err %v", n, ended, err)
	}
}

func TestOneOffWatch_APanicStopsItInTheStore(t *testing.T) {
	f := newOneOffFixture(t)
	info, _ := f.h.reg.start(watchRequest("conv-1", oneOffArgs))
	f.h.readings["a.one"] = func(time.Time) assistantWatchReading { panic("boom") } // after the start check
	f.h.ticker(0).ch <- f.h.t0.Add(time.Second)
	waitUntil(t, "the row to be switched off", func() bool { return !f.watches(t)[0].Enabled })
	runs := f.runs(t, info.ID)
	if len(runs) != 1 || runs[0].Outcome != watchOutcomeSkipped {
		t.Fatalf("runs %+v", runs)
	}
	if _, ok := f.h.reg.get("conv-1"); ok {
		t.Error("it holds no slot")
	}
}

func TestOneOffWatch_DeletingARunningOneStopsItAndPostsNothing(t *testing.T) {
	f := newOneOffFixture(t)
	prev := globalAssistantWatches
	globalAssistantWatches = f.h.reg
	t.Cleanup(func() { globalAssistantWatches = prev })
	info, err := f.h.reg.start(watchRequest("conv-1", oneOffArgs))
	if err != nil {
		t.Fatal(err)
	}
	f.h.tickThrough(f.h.ticker(0), 10)

	c, rec := watchRequestTo(http.MethodDelete, "/api/watches/"+info.ID, "", "id", info.ID)
	if err := deleteWatchHandler(c); err != nil || rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %v %d %s", err, rec.Code, rec.Body)
	}
	if _, ok := f.h.reg.get("conv-1"); ok {
		t.Fatal("the registry no longer holds it")
	}
	select {
	case <-f.h.ticker(0).stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("sampling stopped")
	}
	if len(f.watches(t)) != 0 {
		t.Fatal("the watch is gone")
	}
	// Past its end, nothing is reported to the chat.
	select {
	case fin := <-f.h.finished:
		t.Fatalf("a deleted watch posted its report: %+v", fin.info)
	case <-time.After(100 * time.Millisecond):
	}
	if n := len(f.runs(t, info.ID)); n != 0 {
		t.Errorf("its runs went with it: %d", n)
	}
}

// blockingPersister holds every checkpoint write until released.
type blockingPersister struct {
	*watchOneOffStore
	release chan struct{}
	entered chan struct{}
}

func (b *blockingPersister) Checkpoint(info assistantWatchInfo, batch [][]assistantWatchSample) {
	select {
	case b.entered <- struct{}{}:
	default:
	}
	<-b.release
	b.watchOneOffStore.Checkpoint(info, batch)
}

func TestOneOffWatch_ASlowCheckpointWriteNeverStallsSampling(t *testing.T) {
	f := newOneOffFixture(t)
	bp := &blockingPersister{watchOneOffStore: f.store, release: make(chan struct{}), entered: make(chan struct{}, 1)}
	f.h.reg.deps.persist = bp
	info, _ := f.h.reg.start(watchRequest("conv-1", `{"paths":[{"path":"a.one","label":"House voltage"}],"minutes":10,"reason":"look for dips"}`))

	sampled := make(chan struct{})
	go func() {
		f.h.tickThrough(f.h.ticker(0), 3*assistantWatchCheckpointEvery) // three saves are due
		close(sampled)
	}()
	select {
	case <-sampled:
	case <-time.After(3 * time.Second):
		t.Fatal("sampling stalled behind a blocked write")
	}
	<-bp.entered
	close(bp.release)
	want := 3 * assistantWatchCheckpointEvery
	waitUntil(t, "every sample to be saved", func() bool { return f.sampleCount(t, info.ID) == want })
	series := f.samples(t, info.ID, 1)[0]
	for i := 1; i < len(series); i++ {
		if !series[i].T.After(series[i-1].T) {
			t.Fatalf("samples out of order at %d", i)
		}
	}
}

func TestOneOffWatch_ACheckpointAddsOnlyTheNewSamples(t *testing.T) {
	f := newOneOffFixture(t)
	info, _ := f.h.reg.start(watchRequest("conv-1", `{"paths":[{"path":"a.one","label":"House voltage"}],"minutes":10,"reason":"look for dips"}`))
	f.h.tickThrough(f.h.ticker(0), 2*assistantWatchCheckpointEvery+1)
	waitUntil(t, "two saves", func() bool { return f.sampleCount(t, info.ID) == 2*assistantWatchCheckpointEvery })
	var chunks, bytes int
	if err := f.docs.Read(func(q sqlQueryer) error {
		return q.QueryRow(`SELECT COUNT(*), COALESCE(SUM(length(samples)), 0) FROM watch_samples WHERE watch_id = ?`, info.ID).Scan(&chunks, &bytes)
	}); err != nil {
		t.Fatal(err)
	}
	if chunks != 2 {
		t.Errorf("each save appends one chunk of new samples, got %d chunks", chunks)
	}
}
