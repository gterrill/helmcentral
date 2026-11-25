package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Review pass 1 of the current in the step speed.

// ── mixed fates ──────────────────────────────────────────────────────────────

func rvFateRuns(now time.Time, fates ...runFate) []departureRun {
	soonest := ceilToStep(now, planDepartureStep)
	runs := make([]departureRun, len(fates))
	for i, f := range fates {
		runs[i] = departureRun{dep: soonest.Add(time.Duration(i) * planDepartureStep), run: legRun{fate: f}}
	}
	return runs
}

func TestDepartureCandidates_MixedFatesNameTheDominantCause(t *testing.T) {
	var fates []runFate
	for range 10 {
		fates = append(fates, fateOvernight)
	}
	for range 30 {
		fates = append(fates, fateStalled)
	}
	_, kind, note := departureCandidates(rvFateRuns(lpNow, fates...), lpNow, time.Time{})
	if kind != planKindNoDeparture || !strings.Contains(note, "current against the boat") {
		t.Fatalf("30 stalled and 10 overnight: %v %q, want the stalled wording", kind, note)
	}
	// With none stalled, overnight is the cause.
	fates = append(slices.Repeat([]runFate{fateOvernight}, 10), slices.Repeat([]runFate{fateOK}, 5)...)
	runs := rvFateRuns(lpNow, fates...)
	for i := 10; i < len(runs); i++ {
		runs[i].err = errors.New("no timeline")
	}
	if _, kind, note := departureCandidates(runs, lpNow, time.Time{}); kind != planKindOvernight || note != planNoteOvernight {
		t.Fatalf("overnight and unrun: %v %q", kind, note)
	}
	// Nothing but runs that could not be made is the generic note.
	runs = rvFateRuns(lpNow, fateOK, fateOK)
	runs[0].err, runs[1].err = errors.New("a"), errors.New("b")
	if _, kind, note := departureCandidates(runs, lpNow, time.Time{}); kind != planKindNoDeparture || note != planNoteNoTimeline {
		t.Fatalf("unrun: %v %q", kind, note)
	}
}

func TestReadLeg_LogsTheTallyOfFatesWhenEveryDepartureFails(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(log.Writer())
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	f := &paFake{run: func(_ *legPaceProfile, dep time.Time, _ legCurrentField) (legRun, error) {
		if dep.Before(paAt(14, 14, 0)) { // the larger share of the grid
			return legRun{fate: fateStalled}, nil
		}
		return legRun{fate: fateOvernight}, nil
	}}
	l := paAssess(t, p, legs, f).Legs[0]
	if !strings.Contains(l.Note, "current against the boat") {
		t.Fatalf("note %q: the stalled departures are the cause to name", l.Note)
	}
	out := buf.String()
	if !regexp.MustCompile(`[1-9]\d* stalled`).MatchString(out) || !regexp.MustCompile(`[1-9]\d* overnight`).MatchString(out) {
		t.Fatalf("log has no tally of both fates:\n%s", out)
	}
}

// A leg that cannot be judged has its ETA with the current, or none: see
// leg_current_review3_test.go.

// ── hours beside an overnight or stalled note ───────────────────────────────

func TestAssessPlan_ASuggestedLegWithNoDepartureShowsNoHours(t *testing.T) {
	for name, cur := range map[string]func(*legPaceProfile) (legCurrentField, error){
		"overnight": csCrawl,
		"stalled":   func(*legPaceProfile) (legCurrentField, error) { return crUniform(-5.8), nil },
	} {
		p, legs := paPlan(t, "2026-11-14", 0, 0)
		l := paAssess(t, p, legs, &paFake{realRun: true, currents: cur}).Legs[0]
		if l.Note == "" || l.Depart != "" {
			t.Fatalf("%s: leg = %+v", name, l)
		}
		if l.Hours != nil || l.SpeedKn != nil || l.Arrive != "" || l.EtaBasis != nil {
			t.Errorf("%s: leg = %+v: the note says there is no ETA, so no hours, speed or arrival", name, l)
		}
	}
}

// ── a step with no length is never stalled ──────────────────────────────────

func TestRunFrom_ABoatAtTheEndOfTheLastStepIsNotStalledByTheCurrentThere(t *testing.T) {
	p := csProfile(t, 8, 16)
	last := len(p.steps) - 1
	r, err := p.runFrom(lpNow.Add(-time.Hour), lpNow, last, 0, crUniform(-8))
	if err != nil || r.fate != fateOK {
		t.Fatalf("run %+v err %v: nothing is left to cross, so no current can stall it", r.fate, err)
	}
	if !r.tl.arrive().Equal(lpNow) {
		t.Fatalf("arrives %v, want the time of the fix %v", r.tl.arrive(), lpNow)
	}
}

// ── the shared read of a leg's current ──────────────────────────────────────

func TestSharedLegCurrents_AFailureThatIsNotTheContextsIsSharedForTheAssessment(t *testing.T) {
	p := csProfile(t, 8, 16)
	var calls atomic.Int32
	shared := sharedLegCurrents(func(context.Context, *legPaceProfile) (legCurrentField, error) {
		calls.Add(1)
		return nil, errors.New("the request failed")
	})
	for range 3 {
		if _, err := shared(context.Background(), p); err == nil {
			t.Fatal("the failure is the answer")
		}
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("%d requests, want 1", n)
	}
}

func TestSharedLegCurrents_ACancelledReadIsNeverTheOthersAnswer(t *testing.T) {
	p := csProfile(t, 8, 16)
	started := make(chan struct{})
	var calls atomic.Int32
	shared := sharedLegCurrents(func(ctx context.Context, _ *legPaceProfile) (legCurrentField, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return crUniform(1), nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, err := shared(ctx, p); first <- err }()
	<-started
	type answer struct {
		f   legCurrentField
		err error
	}
	second := make(chan answer, 1)
	go func() { f, err := shared(context.Background(), p); second <- answer{f, err} }()
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("the cancelled slip left with %v", err)
	}
	if a := <-second; a.err != nil || a.f == nil {
		t.Fatalf("the other slip got %v, %v: it reads for itself", a.f, a.err)
	}
	if f, err := shared(context.Background(), p); err != nil || f == nil {
		t.Fatalf("a later slip got %v, %v", f, err)
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("%d requests, want the cancelled one and one that answered", n)
	}
}

// ── the field reads the caller's profile ────────────────────────────────────

func TestLegCellField_EachProfileReadsItsOwnStepCourses(t *testing.T) {
	out := southbound(t, 30)
	back := *out
	back.steps = slices.Clone(out.steps)
	for i := range back.steps {
		c := back.steps[i].course
		back.steps[i].course = trackCourse{sin: -c.sin, cos: -c.cos}
	}
	if back.currentKey() != out.currentKey() {
		t.Fatal("setup: the two profiles must share one request")
	}
	// One field, read for the first profile, 0.5 m/s to the south.
	field, err := tidalDepsWithProvider(steadyCurrent(180)).readLegCurrents(context.Background(), out)
	if err != nil {
		t.Fatal(err)
	}
	dep := time.Date(2026, 9, 12, 2, 0, 0, 0, time.UTC)
	a, err := out.run(dep, field)
	if err != nil {
		t.Fatal(err)
	}
	b, err := back.run(dep, field)
	if err != nil {
		t.Fatal(err)
	}
	if a.figure() < 0.5 || b.figure() > -0.5 {
		t.Fatalf("southbound %.2f kn, northbound %.2f kn: each reads the current along its own courses", a.figure(), b.figure())
	}
}
