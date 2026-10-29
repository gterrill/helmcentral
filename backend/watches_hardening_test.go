package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Review fixes for watches (ADR 0227): partial changes, switching back on,
// and the other guarantees below.

func patchWatch(t *testing.T, id, body string) (int, string) {
	t.Helper()
	c, rec := watchRequestTo(http.MethodPatch, "/api/watches/"+id, body, "id", id)
	if err := patchWatchHandler(c); err != nil {
		t.Fatal(err)
	}
	return rec.Code, rec.Body.String()
}

func TestWatchREST_APartialThresholdChangeKeepsTheOthers(t *testing.T) {
	env := newProposalEnv(t)
	env.applyOK(t, watchCreateArgs)
	w := firstWatch(t, env.docs)

	code, body := patchWatch(t, w.ID, `{"thresholds":{"tank_change_points":20}}`)
	if code != http.StatusOK {
		t.Fatalf("a partial change is accepted: %d %s", code, body)
	}
	var got savedWatch
	_ = json.Unmarshal([]byte(body), &got)
	want := defaultWatchThresholds()
	want.TankChangePoints = 20
	if got.Thresholds != want {
		t.Fatalf("fields not sent keep their stored values: %+v", got.Thresholds)
	}
	if stored := firstWatch(t, env.docs); stored.Thresholds != want {
		t.Fatalf("stored %+v", stored.Thresholds)
	}

	code, body = patchWatch(t, w.ID, `{"thresholds":{"alarms_raised":0}}`)
	if code != http.StatusBadRequest || !strings.Contains(body, "alarms_raised") {
		t.Fatalf("zero is refused, not defaulted: %d %s", code, body)
	}
	if stored := firstWatch(t, env.docs); stored.Thresholds != want {
		t.Fatalf("a refused change leaves the watch alone: %+v", stored.Thresholds)
	}
}

func TestWatch_SwitchingBackOnRestartsTheScheduleClock(t *testing.T) {
	env := newProposalEnv(t)
	env.applyOK(t, watchCreateArgs)
	w := firstWatch(t, env.docs)
	created := w.ScheduleChangedAt

	env.advance()
	env.applyOK(t, `{"operations":[{"type":"watch","action":"update","id":"`+w.ID+`","fields":{"enabled":false}}]}`)
	if got := firstWatch(t, env.docs); !got.ScheduleChangedAt.Equal(created) {
		t.Fatalf("switching off leaves the clock: %+v", got)
	}
	env.advance()
	env.applyOK(t, `{"operations":[{"type":"watch","action":"update","id":"`+w.ID+`","fields":{"enabled":true}}]}`)
	got := firstWatch(t, env.docs)
	if !got.ScheduleChangedAt.Equal(env.clock) {
		t.Fatalf("switched on through Mate, the watch does not run for an instant missed while it was off: changed at %v, now %v", got.ScheduleChangedAt, env.clock)
	}

	// The same through the Watches page.
	env.advance()
	if code, body := patchWatch(t, w.ID, `{"enabled":false}`); code != http.StatusOK {
		t.Fatal(code, body)
	}
	env.advance()
	if code, body := patchWatch(t, w.ID, `{"enabled":true}`); code != http.StatusOK {
		t.Fatal(code, body)
	}
	if got := firstWatch(t, env.docs); !got.ScheduleChangedAt.Equal(env.clock) {
		t.Fatalf("switched on from the page: changed at %v, now %v", got.ScheduleChangedAt, env.clock)
	}
	// Saving an enabled watch again is not a switch on.
	before := env.clock
	env.advance()
	if code, body := patchWatch(t, w.ID, `{"name":"Renamed"}`); code != http.StatusOK {
		t.Fatal(code, body)
	}
	if got := firstWatch(t, env.docs); !got.ScheduleChangedAt.Equal(before) {
		t.Fatalf("a rename leaves the clock: %v", got.ScheduleChangedAt)
	}
}

func TestWatchTick_ATickQueuedBehindRunNowDoesNotRunAgain(t *testing.T) {
	f := newWatchHostFixture(t)
	w := f.add("Hourly", "0 * * * *")
	release := make(chan struct{})
	inReview := make(chan struct{})
	var reviews int32
	f.host.review = func(context.Context, watchConfig, savedWatch, watchReport, []watchFinding, time.Time, *time.Location) watchReviewOutcome {
		if atomic.AddInt32(&reviews, 1) == 1 {
			close(inReview)
			<-release
		}
		return f.review
	}
	configRead := make(chan struct{}, 4)
	f.host.config = func() (watchConfig, error) { configRead <- struct{}{}; return watchConfig{}, nil }

	runNowDone := make(chan error, 1)
	go func() { _, err := f.host.RunNow(context.Background(), w); runNowDone <- err }()
	<-inReview
	<-configRead // Run now's own read

	tickDone := make(chan struct{})
	go func() { f.host.tick(context.Background()); close(tickDone) }()
	<-configRead // the tick found the watch due and is now queued behind Run now
	time.Sleep(50 * time.Millisecond)
	close(release)
	if err := <-runNowDone; err != nil {
		t.Fatal(err)
	}
	<-tickDone
	if n := len(f.runsOf(w.ID)); n != 1 {
		t.Fatalf("the watch ran %d times; the queued tick should see it was just run", n)
	}
}

func TestWatchRunNow_LeavingThePageDoesNotCancelTheRun(t *testing.T) {
	f := newWatchHostFixture(t)
	w := f.add("Morning", "0 6 * * *")
	var reportErr, reviewErr error
	var hasDeadline bool
	f.host.report = func(ctx context.Context, _ savedWatch, _ time.Time, _ *time.Location) (watchReport, []watchFinding, error) {
		reportErr = ctx.Err()
		return watchReport{Kind: watchReportKindSections, BoatTime: "now"}, nil, nil
	}
	f.host.review = func(ctx context.Context, _ watchConfig, _ savedWatch, _ watchReport, _ []watchFinding, _ time.Time, _ *time.Location) watchReviewOutcome {
		reviewErr = ctx.Err()
		_, hasDeadline = ctx.Deadline()
		return f.review
	}
	gone, cancel := context.WithCancel(context.Background())
	cancel() // the page was closed, or the chat turn stopped
	run, err := f.host.RunNow(gone, w)
	if err != nil || run == nil {
		t.Fatalf("run: %v %v", run, err)
	}
	if reportErr != nil || reviewErr != nil {
		t.Fatalf("a run started by the operator is not cancelled with their request: report %v review %v", reportErr, reviewErr)
	}
	if !hasDeadline {
		t.Error("the review has its own time limit")
	}
	if n := len(f.runsOf(w.ID)); n != 1 {
		t.Errorf("the run is saved: %d", n)
	}
}
