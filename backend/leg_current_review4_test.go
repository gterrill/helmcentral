package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Review pass 3 of the current in the step speed: correctness fixes.

// ── 1: each current error is worded for what it is ──────────────────────────

func TestFetchCellCurrents_ALegOverTheCapIsATypedErrorWithItsCells(t *testing.T) {
	deps := tidalDepsWithProvider(steadyCurrent(180))
	deps.now = func() time.Time { return paNow }
	p, err := (&legPaceModel{CruiseKn: 8, Slow: map[paceCell]float64{}}).profile(lpLeg(-20, -30))
	if err != nil {
		t.Fatal(err)
	}
	_, err = deps.readLegCurrents(context.Background(), p)
	var long currentTooLongError
	if !errors.As(err, &long) || long.cells != len(p.cells) || long.cells <= maxCurrentsPoints {
		t.Fatalf("err = %v (%T), want a currentTooLongError for %d cells", err, err, len(p.cells))
	}
}

func TestFetchCellCurrents_AProviderThatCannotBeResolvedIsNoCurrentProvider(t *testing.T) {
	deps := tidalDepsWithProvider(steadyCurrent(180))
	deps.waves = func() (waveProvider, string, error) { return nil, "", errors.New("settings unreadable") }
	p := csProfile(t, 8, 16)
	_, err := deps.readLegCurrents(context.Background(), p)
	if !errors.Is(err, errCurrentNoProvider) {
		t.Fatalf("err = %v, want errCurrentNoProvider", err)
	}
	prov := steadyCurrent(180)
	prov.fail = errors.New("boom")
	_, err = tidalDepsWithProvider(prov).readLegCurrents(context.Background(), p)
	if err == nil || errors.Is(err, errCurrentNoProvider) {
		t.Fatalf("err = %v: a failed request is not a missing provider", err)
	}
	var long currentTooLongError
	if errors.As(err, &long) {
		t.Fatalf("err = %v: a failed request is not a long leg", err)
	}
}

func TestEtaRules_EachCurrentErrorIsWordedForWhatItIs(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		why  string
	}{
		{"request failed", errors.New("the Open-Meteo current request failed"), "the modelled current could not be read"},
		{"leg too long", fmt.Errorf("read: %w", currentTooLongError{cells: 120}), "the leg is too long for one request to the current model (120 cells, up to 100)"},
		{"no provider", fmt.Errorf("read: %w", errCurrentNoProvider), "no modelled current is set up"},
	} {
		op := csOperatorLeg(t, &paFake{currentErr: c.err})
		if want := "No ETA: " + c.why; op.Note != want || op.NoteKind != planKindOther {
			t.Errorf("%s: operator leg note %q (%s), want %q", c.name, op.Note, op.NoteKind, want)
		}
		p, legs := paPlan(t, "2026-11-14", 0, 0)
		sug := paAssess(t, p, legs, &paFake{currentErr: c.err}).Legs[0]
		if want := "No departure suggested: " + c.why; sug.Note != want || sug.NoteKind != planKindNoDeparture {
			t.Errorf("%s: suggested leg note %q (%s), want %q", c.name, sug.Note, sug.NoteKind, want)
		}
		// The leg under way says it as the operator's leg does.
		p, legs = paPlan(t, "2026-11-14", 0, 0)
		a, b := p.Stops[0], p.Stops[1]
		uw := paAssess(t, p, legs, &paFake{currentErr: c.err,
			underway: uwFix(p, paAt(14, 4, 0), paAt(14, 4, 20), (a.Lat+b.Lat)/2, (a.Lon+b.Lon)/2)}).Legs[0]
		if want := "No ETA: " + c.why; uw.WindChanges.NotChecked != want {
			t.Errorf("%s: under way not checked %q, want %q", c.name, uw.WindChanges.NotChecked, want)
		}
	}
}

// ── 2: a provider's own timeout is shared across slips ──────────────────────

func TestSharedLegCurrents_AProviderTimeoutWithLiveContextsIsSharedForTheAssessment(t *testing.T) {
	p := csProfile(t, 8, 16)
	var calls atomic.Int32
	shared := sharedLegCurrents(func(context.Context, *legPaceProfile) (legCurrentField, error) {
		calls.Add(1)
		return nil, fmt.Errorf("the Open-Meteo current request failed: %w", context.DeadlineExceeded)
	})
	for range 3 {
		if _, err := shared(context.Background(), p); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, the provider's timeout is the answer", err)
		}
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("%d reads, want 1 across the slips", n)
	}
}

// ── 3: under way, a failure that is not the current says main's words ───────

func TestAssessPlan_ALegUnderWayWhoseRunFailsNotOnTheCurrentSaysMainsWords(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	a, b := p.Stops[0], p.Stops[1]
	f := &paFake{underway: uwFix(p, paAt(14, 4, 20), paAt(14, 4, 20), (a.Lat+b.Lat)/2, (a.Lon+b.Lon)/2)}
	l := paAssess(t, p, legs, f).Legs[0]
	want := "No ETA: the ETA from the latest position could not be worked out"
	if l.WindChanges.NotChecked != want {
		t.Fatalf("not checked = %q, want %q", l.WindChanges.NotChecked, want)
	}
}

// ── 4: the stall is strictly under the threshold ────────────────────────────

func TestLegRun_ExactlyHalfAKnotRunsAndUnderItStalls(t *testing.T) {
	p := csProfile(t, 8, 16)
	// 16 nm at 0.5 kn is a run of 32 hours: an overnight run, but a run.
	if r, err := p.run(lpNow, crUniform(-7.5)); err != nil || r.fate == fateStalled || r.tl.hours() < 31.9 {
		t.Fatalf("0.5 kn over the ground: fate %v err %v hours %.1f, want a run", r.fate, err, r.tl.hours())
	}
	if r, err := p.run(lpNow, crUniform(-7.51)); !errors.Is(err, errCurrentAgainst) || r.fate != fateStalled {
		t.Fatalf("0.49 kn over the ground: fate %v err %v, want stalled", r.fate, err)
	}
}

// ── 5: a leg whose still-water pace failed reads no current ─────────────────

func TestEtaRules_ALegWhoseStillWaterPaceFailedReadsNoCurrentAndKeepsItsNote(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	p.Stops[0].DepartTime = "08:00"
	m := &legPaceModel{CruiseKn: 6, Slow: map[paceCell]float64{}}
	for _, leg := range legs {
		prof, err := m.profile(leg)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range prof.steps {
			m.Slow[cellOf(s.lat, s.lon)] = math.NaN() // not a speed: the still-water timeline cannot be made
		}
	}
	f := &paFake{pace: m, realRun: true, currents: func(*legPaceProfile) (legCurrentField, error) { return crUniform(2), nil }}
	l := paAssess(t, p, legs, f).Legs[0]
	if !strings.HasPrefix(l.Note, "No ETA: ") || !strings.Contains(l.Note, "must move forward") || l.NoteKind != planKindOther {
		t.Fatalf("leg = %+v: the still-water failure is the note", l)
	}
	if f.reads != 0 {
		t.Fatalf("%d current reads, want none for a leg with no pace", f.reads)
	}
	if l.Arrive != "" || l.Hours != nil || l.SpeedKn != nil || l.CurrentKn != nil {
		t.Fatalf("leg = %+v: nothing overwrites the No ETA", l)
	}
}

// ── 6: a suggested leg reads the current before its window is decided ───────

func TestEtaRules_ASuggestedLegWithNoWindowSaysAFailedReadFirst(t *testing.T) {
	noDawn := func(d *planAssessDeps) {
		d.dawn = func(time.Time, float64, float64) (time.Time, bool) { return time.Time{}, false }
	}
	late := func(d *planAssessDeps) { // first light after the day's last departure
		d.dawn = func(date time.Time, _, _ float64) (time.Time, bool) { return date.Add(23 * time.Hour), true }
	}
	for name, tweak := range map[string]func(*planAssessDeps){"sun unknown": noDawn, "bound before first light": late} {
		p, legs := paPlan(t, "2026-11-14", 0, 0)
		f := &paFake{currentErr: errors.New("the request failed")}
		d := paDeps(f)
		tweak(&d)
		l := assessPlan(context.Background(), p, legs, d, planAssessOptions{}).Legs[0]
		if want := "No departure suggested: the modelled current could not be read"; l.Note != want || l.NoteKind != planKindNoDeparture {
			t.Errorf("%s: leg = %+v, want %q", name, l, want)
		}
		// With the current read, the window's own note stands.
		p, legs = paPlan(t, "2026-11-14", 0, 0)
		f = &paFake{}
		d = paDeps(f)
		tweak(&d)
		l = assessPlan(context.Background(), p, legs, d, planAssessOptions{}).Legs[0]
		if l.Note != planNoteNoWindow || l.NoteKind != planKindNoWindow {
			t.Errorf("%s: leg = %+v, want %q once the current is read", name, l, planNoteNoWindow)
		}
	}
}
