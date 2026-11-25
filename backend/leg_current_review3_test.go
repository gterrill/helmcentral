package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// Review pass 2 of the current in the step speed: one set of ETA failure rules.

func r3Stalled(*legPaceProfile) (legCurrentField, error) { return crUniform(-5.8), nil }

// ── rule 1: any leg with a departure reads the current ──────────────────────

func TestEtaRules_ALegThatCannotBeJudgedStillHasAnEtaWithTheCurrent(t *testing.T) {
	// 30 nm at 6 kn is 5 h; with 2 kn behind it, 3.75 h.
	f := &paFake{realRun: true, limits: passageLimits{DaylightArrival: true},
		currents: func(*legPaceProfile) (legCurrentField, error) { return crUniform(2), nil }}
	l := csOperatorLeg(t, f)
	if l.NoteKind != planKindProblem || !strings.Contains(l.Note, "Passage limits aren't set") {
		t.Fatalf("leg = %+v: its own note stays", l)
	}
	if got := paParse(t, l.Arrive); got.Sub(paAt(14, 11, 45)).Abs() > 2*time.Minute {
		t.Fatalf("arrive %s, want 11.45 AM: the current is in the ETA", l.Arrive)
	}
	if l.Hours == nil || l.SpeedKn == nil || l.CurrentKn == nil || *l.CurrentKn != 2 {
		t.Fatalf("leg = %+v: hours, speed and the current figure show", l)
	}
	if l.EtaBasis == nil || !strings.Contains(*l.EtaBasis, "current with you 2.0 kn") {
		t.Fatalf("basis = %v", l.EtaBasis)
	}
}

func TestEtaRules_AStillWaterOvernightLegKeepsItsNoteAndHasAnEtaWithTheCurrent(t *testing.T) {
	// 30 nm at a 1 kn cruise is 30 h in still water; 5 kn behind it, 5 h.
	f := &paFake{speed: 1, realRun: true, currents: func(*legPaceProfile) (legCurrentField, error) { return crUniform(5), nil }}
	l := csOperatorLeg(t, f)
	if l.NoteKind != planKindOvernight || l.Note != planNoteOvernight {
		t.Fatalf("leg = %+v", l)
	}
	if got := paParse(t, l.Arrive); got.Sub(paAt(14, 13, 0)).Abs() > 2*time.Minute || l.Hours == nil {
		t.Fatalf("leg = %+v: arrive 1 PM with the current in it", l)
	}
}

func TestEtaRules_AFailedReadIsNoEtaWhateverElseTheLegSays(t *testing.T) {
	for name, f := range map[string]*paFake{
		"limits not set":     {currentErr: errors.New("the request failed"), limits: passageLimits{DaylightArrival: true}},
		"still-water night":  {currentErr: errors.New("the request failed"), speed: 1},
		"stalled and unset":  {realRun: true, currents: r3Stalled, limits: passageLimits{DaylightArrival: true}},
		"stalled and normal": {realRun: true, currents: r3Stalled},
	} {
		l := csOperatorLeg(t, f)
		if !strings.HasPrefix(l.Note, "No ETA: ") || l.NoteKind != planKindOther {
			t.Errorf("%s: leg = %+v: the failure replaces the leg's own note", name, l)
		}
		if f.currentErr != nil && l.Note != planNoEtaNoCurrent {
			t.Errorf("%s: note %q", name, l.Note)
		}
		if l.Arrive != "" || l.Hours != nil || l.SpeedKn != nil || l.EtaBasis != nil {
			t.Errorf("%s: leg = %+v: no ETA, no hours", name, l)
		}
	}
}

// ── rule 2: a failed read stops a suggestion before anything is run ─────────

func TestEtaRules_ASuggestedLegWhoseReadFailsRunsNothing(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	runs := 0
	f := &paFake{currentErr: errors.New("the request failed"), run: func(*legPaceProfile, time.Time, legCurrentField) (legRun, error) {
		runs++
		return legRun{}, nil
	}}
	l := paAssess(t, p, legs, f).Legs[0]
	if want := "No departure suggested: the modelled current could not be read"; l.Note != want || l.NoteKind != planKindNoDeparture {
		t.Fatalf("leg = %+v, want %q", l, want)
	}
	if runs != 0 {
		t.Fatalf("%d runs made on a still-water grid", runs)
	}
}

// ── rule 3: no departure or no ETA, no hours ────────────────────────────────

func TestEtaRules_ARowWithNoDepartureOrNoEtaShowsNoHoursSpeedOrBasis(t *testing.T) {
	short := pwUniform(10, 190, 12, nil, 0, paAt(14, 0, 0), paAt(14, 9, 0))
	for _, c := range []struct {
		name  string
		f     *paFake
		tweak func(d *planAssessDeps)
		judge bool
	}{
		{name: "no window", f: &paFake{}, tweak: func(d *planAssessDeps) {
			d.dawn = func(time.Time, float64, float64) (time.Time, bool) { return time.Time{}, false }
		}},
		{name: "window passed", f: &paFake{}, tweak: func(d *planAssessDeps) { d.now = func() time.Time { return paAt(14, 20, 0) } }},
		{name: "overnight", f: &paFake{realRun: true, currents: csCrawl}},
		{name: "stalled", f: &paFake{realRun: true, currents: r3Stalled}},
		{name: "failed ranking", f: &paFake{realRun: true, currents: func(*legPaceProfile) (legCurrentField, error) {
			return crField{along: func(int, time.Time) (float64, bool) { return 0, false }, why: "none"}, nil
		}}},
		{name: "beyond the forecast", f: &paFake{fc: short}},
		{name: "failed read", f: &paFake{currentErr: errors.New("the request failed")}},
		{name: "failed judge", f: &paFake{}, judge: true}, // last: it holds departureCell until the test ends
	} {
		if c.judge {
			saved := departureCell
			departureCell = func(_ context.Context, as *passageAssessor, w *planLegWork, d tidalDeparture) (passageWindowCell, error) {
				return passageWindowCell{}, errors.New("no forecast cell")
			}
			t.Cleanup(func() { departureCell = saved })
		}
		p, legs := paPlan(t, "2026-11-14", 0, 0)
		d := paDeps(c.f)
		if c.tweak != nil {
			c.tweak(&d)
		}
		l := assessPlan(context.Background(), p, legs, d, planAssessOptions{}).Legs[0]
		if l.Note == "" || l.Depart != "" {
			t.Errorf("%s: leg = %+v: a note and no departure", c.name, l)
		}
		if l.Hours != nil || l.SpeedKn != nil || l.EtaBasis != nil || l.Arrive != "" {
			t.Errorf("%s: leg = %+v: no still-water reference hours on the wire", c.name, l)
		}
	}
}

// ── rule 4: the most common cause ───────────────────────────────────────────

func TestEtaRules_TheNoteNamesTheMostCommonCause(t *testing.T) {
	mix := func(stalled, overnight, other int) []runFate {
		var f []runFate
		f = append(f, slices.Repeat([]runFate{fateStalled}, stalled)...)
		f = append(f, slices.Repeat([]runFate{fateOvernight}, overnight)...)
		return append(f, slices.Repeat([]runFate{fateOK}, other)...)
	}
	for _, c := range []struct {
		name                     string
		stalled, overnight, rest int
		kind                     planNoteKind
		note                     string
	}{
		{"39 overnight, 1 stalled", 1, 39, 0, planKindOvernight, planNoteOvernight},
		{"30 stalled, 10 overnight", 30, 10, 0, planKindNoDeparture, "No departure suggested: " + errCurrentAgainst.Error()},
		{"tie stalled and overnight", 5, 5, 0, planKindNoDeparture, "No departure suggested: " + errCurrentAgainst.Error()},
		{"tie overnight and generic", 0, 5, 5, planKindOvernight, planNoteOvernight},
		{"3 unrun beat 1 stalled", 1, 0, 3, planKindNoDeparture, planNoteNoTimeline},
		{"three way tie", 4, 4, 4, planKindNoDeparture, "No departure suggested: " + errCurrentAgainst.Error()},
	} {
		runs := rvFateRuns(lpNow, mix(c.stalled, c.overnight, c.rest)...)
		for i := c.stalled + c.overnight; i < len(runs); i++ {
			runs[i].err = errors.New("no timeline")
		}
		if _, kind, note := departureCandidates(runs, lpNow, time.Time{}); kind != c.kind || note != c.note {
			t.Errorf("%s: %v %q, want %v %q", c.name, kind, note, c.kind, c.note)
		}
	}
}

// ── rule 5: the stall wording follows the threshold ─────────────────────────

func TestEtaRules_TheStallWordingNamesTheThreshold(t *testing.T) {
	want := fmt.Sprintf("the current against the boat leaves it under %.1f kn part of the way", legCurrentStallKn)
	if errCurrentAgainst.Error() != want {
		t.Fatalf("%q, want %q", errCurrentAgainst, want)
	}
	if strings.Contains(errCurrentAgainst.Error(), "cruising speed") {
		t.Fatal("it is not the cruising speed that matters")
	}
}

// ── rule 7: the key is the query alone ──────────────────────────────────────

func TestEtaRules_TheCurrentKeyIsWhatTheRequestAsksAndNothingElse(t *testing.T) {
	p := southbound(t, 30)
	other := *p
	other.cellIdx = slices.Repeat([]int{0}, len(p.cellIdx))
	if p.currentKey() != other.currentKey() {
		t.Fatal("the answer holds only the series: two profiles that ask the same share it, whatever their steps")
	}
	moved := *p
	moved.query = slices.Clone(p.query)
	moved.query[0].Lat += 0.1
	if p.currentKey() == moved.currentKey() {
		t.Fatal("a request for other points is another request")
	}
}

// ── rule 6: the leg under way counts only the slow water ahead of the fix ───

func TestEtaRules_TheLegUnderWayBasisCountsOnlyTheSlowWaterAheadOfTheFix(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	p.Stops[0].DepartTime = "03:00"
	// The berth is slow water: the first steps of the route.
	model := &legPaceModel{CruiseKn: 6, Slow: map[paceCell]float64{}}
	open, err := model.profile(legs[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range open.steps[:3] {
		model.Slow[cellOf(s.lat, s.lon)] = 2
	}
	still := func(*legPaceProfile) (legCurrentField, error) { return crUniform(0), nil }
	// From the start, the berth's slow water is in the hours.
	op := csOperatorLeg(t, &paFake{pace: model, realRun: true, currents: still})
	if op.EtaBasis == nil || !strings.Contains(*op.EtaBasis, "in slow water") {
		t.Fatalf("setup: the operator's leg from the berth has slow water: %v", op.EtaBasis)
	}
	f := csUnderwayFake(p, &paFake{pace: model, realRun: true, currents: still})
	l := paAssess(t, p, legs, f).Legs[0]
	if l.EtaBasis == nil || strings.Contains(*l.EtaBasis, "slow water") {
		t.Fatalf("basis = %v: the fix is past the berth, none of its slow water is ahead", l.EtaBasis)
	}
}
