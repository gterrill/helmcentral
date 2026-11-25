package main

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

// The modelled along-track current is in each open-water step's speed: base knots
// plus the current at the moment the boat enters the step.

// csTurning is a current that helps by 2 kn until turn and hinders by 2 kn after it.
func csTurning(turn time.Time) crField {
	return crField{along: func(_ int, t time.Time) (float64, bool) {
		if t.Before(turn) {
			return 2, true
		}
		return -2, true
	}}
}

func csProfile(t *testing.T, kn, nm float64) *legPaceProfile {
	t.Helper()
	p, _ := crLeg(t, &legPaceModel{CruiseKn: kn, Slow: map[paceCell]float64{}}, nm)
	return p
}

func TestLegRun_TheModelledCurrentIsAddedToTheOpenWaterSpeed(t *testing.T) {
	p := csProfile(t, 8, 16)
	for _, c := range []struct {
		cur, hours float64
	}{{2, 1.6}, {-2, 16.0 / 6}, {0, 2}} {
		r, err := p.run(lpNow, crUniform(c.cur))
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(r.tl.hours()-c.hours) > 0.005 {
			t.Errorf("current %v: %.4f h, want %.4f", c.cur, r.tl.hours(), c.hours)
		}
	}
	// No field is still water, and a field with no current anywhere is cruise alone.
	still, _ := p.run(lpNow, nil)
	none, _ := p.run(lpNow, crField{along: func(int, time.Time) (float64, bool) { return 0, false }})
	if math.Abs(still.tl.hours()-2) > 1e-9 || math.Abs(none.tl.hours()-2) > 1e-9 {
		t.Errorf("still %.4f h, unmodelled %.4f h, want 2", still.tl.hours(), none.tl.hours())
	}
}

func TestLegRun_SlowWaterRunsAtItsOwnSpeedWhateverTheCurrent(t *testing.T) {
	m := lpModel(lpBoats[0])
	p, _ := crLeg(t, m, 3)
	want := p.slowHours()
	if want <= 0 {
		t.Fatal("setup: no slow water")
	}
	slowOf := func(r legRun) float64 {
		// The slow steps' time is the run's less the open water's at the helped speed.
		var h float64
		for _, s := range p.steps {
			if !s.slow {
				h += s.nm / (s.kn + 2)
			}
		}
		return r.tl.hours() - h
	}
	r, err := p.run(lpNow, crUniform(2))
	if err != nil {
		t.Fatal(err)
	}
	if got := slowOf(r); math.Abs(got-want) > 0.002 {
		t.Fatalf("slow water took %.4f h with the current, %.4f without: it must not change", got, want)
	}
}

func TestLegRun_TwoDeparturesAQuarterHourApartMeetATurningCurrentDifferently(t *testing.T) {
	p := csProfile(t, 8, 24)
	f := csTurning(lpNow.Add(time.Hour))
	a, err := p.run(lpNow, f)
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.run(lpNow.Add(15*time.Minute), f)
	if err != nil {
		t.Fatal(err)
	}
	// From the first: 10 nm in the hour at 10 kn, then 14 nm at 6 kn is 3.33 h. From the
	// second: 7.5 nm in 45 minutes, then 16.5 nm at 6 kn is 3.5 h.
	if math.Abs(a.tl.hours()-10.0/3) > 0.03 || math.Abs(b.tl.hours()-3.5) > 0.03 {
		t.Fatalf("hours %.3f and %.3f, want 3.33 and 3.50", a.tl.hours(), b.tl.hours())
	}
	if !b.tl.arrive().After(a.tl.arrive().Add(15 * time.Minute)) {
		t.Fatalf("the later departure arrives %v after the first: it met the foul current for longer", b.tl.arrive().Sub(a.tl.arrive()))
	}
}

// A steady leg the model has no current for is the leg's own steady pace (linearPace,
// whose arrival is distance over cruise to the nanosecond, not a sum of steps), as it
// was before the current was in the speed; one it has a current for is summed step by
// step.
func TestLegRun_ASteadyLegWithNoModelledCurrentIsItsPacesOwnTimeline(t *testing.T) {
	p := csProfile(t, 7, 10)
	if !p.steady {
		t.Fatal("setup: the leg has slow water")
	}
	want, err := p.pace(lpNow)
	if err != nil {
		t.Fatal(err)
	}
	none, err := p.run(lpNow, crField{along: func(int, time.Time) (float64, bool) { return 0, false }})
	if err != nil || !none.tl.steady || len(none.tl.knots) != 2 || !none.tl.arrive().Equal(want.arrive()) {
		t.Fatalf("run %+v (err %v), want the pace's %+v", none.tl, err, want)
	}
	with, err := p.run(lpNow, crUniform(0.7))
	if err != nil || with.tl.steady || len(with.tl.knots) != 2 {
		t.Fatalf("a modelled current is summed over the steps: %+v (err %v)", with.tl, err)
	}
}

func TestLegRun_AFieldWithCurrentIsIntegratedEvenOnALegWithNoSlowWater(t *testing.T) {
	p := csProfile(t, 8, 16)
	if !p.steady {
		t.Fatal("setup: the leg has slow water")
	}
	r, err := p.run(lpNow, crUniform(2))
	if err != nil {
		t.Fatal(err)
	}
	if r.tl.hours() > 1.7 {
		t.Fatalf("%.3f h: a steady leg must not skip the current", r.tl.hours())
	}
}

// The latest departure that still arrives by sunset moves with the current.
func TestDepartureCandidates_AFairCurrentLetsTheLatestDepartureBeLaterAndAFoulOneEarlier(t *testing.T) {
	p := csProfile(t, 6, 30)
	arriveBy := lpNow.Add(6*time.Hour + 35*time.Minute)
	latest := func(f legCurrentField) time.Time {
		var runs []departureRun
		for d := time.Duration(0); d <= 12*time.Hour; d += planDepartureStep {
			r := departureRun{dep: lpNow.Add(d)}
			r.run, r.err = p.run(r.dep, f)
			runs = append(runs, r)
		}
		fits, _, _ := departureCandidates(runs, lpNow, arriveBy)
		if len(fits) == 0 {
			return time.Time{}
		}
		return fits[len(fits)-1].dep
	}
	still, fair, foul := latest(nil), latest(crUniform(1)), latest(crUniform(-1))
	if !still.Equal(lpNow.Add(90*time.Minute)) || !fair.Equal(lpNow.Add(135*time.Minute)) || !foul.Equal(lpNow.Add(30*time.Minute)) {
		t.Fatalf("latest departures %v %v %v, want +90m, +135m (fair), +30m (foul)", still.Sub(lpNow), fair.Sub(lpNow), foul.Sub(lpNow))
	}
}

// ── against the boat as strong as its cruising speed ────────────────────────

func TestLegRun_ACurrentAgainstTheBoatNearItsCruisingSpeedFailsTheRun(t *testing.T) {
	p := csProfile(t, 8, 16)
	for _, c := range []struct {
		cur  float64
		fail bool
	}{{-7.4, false}, {-7.6, true}, {-8, true}, {-9, true}} {
		_, err := p.run(lpNow, crUniform(c.cur))
		if (err != nil) != c.fail {
			t.Errorf("current %v: err = %v, want failure %v", c.cur, err, c.fail)
		}
		if err != nil && !strings.Contains(err.Error(), "leaves it under 0.5 kn") {
			t.Errorf("current %v: err = %q", c.cur, err)
		}
	}
	// One step is enough, and the leg under way fails the same way.
	one := crField{along: func(k int, _ time.Time) (float64, bool) {
		if k == len(p.steps)/2 {
			return -8, true
		}
		return 0, true
	}}
	if _, err := p.run(lpNow, one); err == nil {
		t.Error("a single step against the boat at its cruising speed must fail the run")
	}
	if _, err := p.runFrom(lpNow.Add(-time.Hour), lpNow, 0, 1, one); err == nil {
		t.Error("the leg under way must fail the same way")
	}
	// Slow steps and unmodelled steps are never stalled by it.
	allSlow := crField{along: func(int, time.Time) (float64, bool) { return -9, false }}
	if _, err := p.run(lpNow, allSlow); err != nil {
		t.Errorf("an unmodelled current fails nothing: %v", err)
	}
}

func csOperatorLeg(t *testing.T, f *paFake) planLegAssessment {
	t.Helper()
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	p.Stops[0].DepartTime = "08:00"
	return paAssess(t, p, legs, f).Legs[0]
}

func TestAssessPlan_TheOperatorsDepartureWithTheCurrentAgainstItHasNoEta(t *testing.T) {
	f := &paFake{realRun: true, currents: func(*legPaceProfile) (legCurrentField, error) { return crUniform(-5.8), nil }}
	l := csOperatorLeg(t, f)
	if !strings.HasPrefix(l.Note, "No ETA: ") || !strings.Contains(l.Note, "current against the boat") || l.NoteKind != planKindOther {
		t.Fatalf("leg = %+v", l)
	}
	if l.Arrive != "" || l.Hours != nil || l.SpeedKn != nil || l.EtaBasis != nil || l.Verdict != "" || l.Depart == "" {
		t.Fatalf("no ETA, no verdict, and the operator's own departure stays: %+v", l)
	}
	if !strings.Contains(l.WindChanges.NotChecked, "No ETA") {
		t.Fatalf("wind changes not checked = %q", l.WindChanges.NotChecked)
	}
}

func TestAssessPlan_ASuggestedDepartureTheCurrentStallsIsLeftOut(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	// The current helps by 1.5 kn until 9, is as strong as the boat from 9 to 10 and
	// helps by 0.5 after. Every run at sea in that hour fails, though the first one's
	// mean is the fairest: the first that clears it leaves at 10.
	f := &paFake{realRun: true, currents: func(*legPaceProfile) (legCurrentField, error) {
		return crField{along: func(_ int, t time.Time) (float64, bool) {
			switch {
			case t.Before(paAt(14, 9, 0)):
				return 1.5, true
			case t.Before(paAt(14, 10, 0)):
				return -5.8, true
			}
			return 0.5, true
		}}, nil
	}}
	l := paAssess(t, p, legs, f).Legs[0]
	if !l.DepartSuggested || !paParse(t, l.Depart).Equal(paAt(14, 10, 0)) {
		t.Fatalf("leg = %+v: the runs through the stalled hour are left out, not suggested", l)
	}
}

func TestAssessPlan_WhenTheCurrentStallsEverySuggestionTheLegSaysSo(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	f := &paFake{realRun: true, currents: func(*legPaceProfile) (legCurrentField, error) { return crUniform(-5.8), nil }}
	l := paAssess(t, p, legs, f).Legs[0]
	if l.NoteKind != planKindNoDeparture || !strings.HasPrefix(l.Note, "No departure suggested: ") || !strings.Contains(l.Note, "current against the boat") {
		t.Fatalf("leg = %+v", l)
	}
	if l.Depart != "" || l.Arrive != "" {
		t.Fatalf("a leg with no departure shows no time: %+v", l)
	}
}

func csUnderwayFake(p plan, f *paFake) *paFake {
	a, b := p.Stops[0], p.Stops[1]
	f.underway = &planUnderway{
		Lat: a.Lat, Lon: a.Lon, Start: paAt(14, 3, 0),
		FixLat: (a.Lat + b.Lat) / 2, FixLon: (a.Lon + b.Lon) / 2, FixAt: paAt(14, 4, 20),
	}
	return f
}

func TestAssessPlan_TheLegUnderWayWithTheCurrentAgainstItHasNoEta(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	p.Stops[0].DepartTime = "03:00"
	f := csUnderwayFake(p, &paFake{realRun: true, currents: func(*legPaceProfile) (legCurrentField, error) { return crUniform(-5.8), nil }})
	l := paAssess(t, p, legs, f).Legs[0]
	if l.Arrive != "" || l.Hours != nil || !strings.HasPrefix(l.WindChanges.NotChecked, "No ETA: ") || !strings.Contains(l.WindChanges.NotChecked, "current against the boat") {
		t.Fatalf("leg = %+v", l)
	}
}

// ── a failed request ─────────────────────────────────────────────────────────

const csCouldNotBeRead = "No ETA: the modelled current could not be read"

func TestAssessPlan_AFailedCurrentRequestLeavesTheOperatorsDepartureWithNoEta(t *testing.T) {
	l := csOperatorLeg(t, &paFake{currentErr: errors.New("the Open-Meteo current request failed")})
	if l.Note != csCouldNotBeRead || l.NoteKind != planKindOther {
		t.Fatalf("leg = %+v", l)
	}
	if l.Arrive != "" || l.Hours != nil || l.Verdict != "" || l.CurrentKn != nil {
		t.Fatalf("no ETA and no verdict: %+v", l)
	}
	if strings.Contains(l.Note, "request failed") {
		t.Fatalf("the detail is logged, not shown: %q", l.Note)
	}
}

func TestAssessPlan_AFailedCurrentRequestLeavesTheLegUnderWayWithNoEta(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	p.Stops[0].DepartTime = "03:00"
	f := csUnderwayFake(p, &paFake{currentErr: errors.New("the Open-Meteo current request failed")})
	l := paAssess(t, p, legs, f).Legs[0]
	if l.Arrive != "" || l.Hours != nil || l.WindChanges.NotChecked != csCouldNotBeRead {
		t.Fatalf("leg = %+v", l)
	}
}

// ── overnight, run by run ────────────────────────────────────────────────────

// csCrawl is a current that leaves the boat 1.2 kn at a 6 kn cruise: 25 hours for 30 nm.
func csCrawl(*legPaceProfile) (legCurrentField, error) { return crUniform(-4.8), nil }

func TestAssessPlan_ARunTheCurrentPushesPastADayIsNotSuggested(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	l := paAssess(t, p, legs, &paFake{realRun: true, currents: csCrawl}).Legs[0]
	if l.NoteKind != planKindOvernight || l.Note != planNoteOvernight || l.Depart != "" {
		t.Fatalf("leg = %+v: every departure takes over a day, so none is suggested", l)
	}
}

func TestAssessPlan_TheOperatorsDepartureTheCurrentPushesPastADayGetsTheOvernightNote(t *testing.T) {
	l := csOperatorLeg(t, &paFake{realRun: true, currents: csCrawl})
	if l.NoteKind != planKindOvernight || l.Note != planNoteOvernight || l.Verdict != "" {
		t.Fatalf("leg = %+v", l)
	}
	if got := paParse(t, l.Arrive); got.Sub(paAt(15, 9, 0)).Abs() > 5*time.Minute {
		t.Fatalf("arrive %s: the arrival still shows, 25 hours on", l.Arrive)
	}
}

func TestAssessPlan_TheStillWaterOvernightPreCheckIsUnchanged(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	// 30 nm at 1 kn cruise is 30 hours before any current.
	l := paAssess(t, p, legs, &paFake{speed: 1, realRun: true, currents: func(*legPaceProfile) (legCurrentField, error) { return crUniform(5), nil }}).Legs[0]
	if l.NoteKind != planKindOvernight || l.Note != planNoteOvernight {
		t.Fatalf("leg = %+v: still water decides the pre-check", l)
	}
}

// ── departures that have passed are run with the field ──────────────────────

func TestAssessPlan_EachSuggestionIsJudgedOnItsOwnArrivalWithTheCurrent(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	// A gale from 10 AM, dead ahead. In still water the 5.15 AM departure arrives at
	// 10.15 and is in it; with a 2 kn current it arrives at 9.00 and is not.
	fc := paWind(func(h time.Time) float64 {
		if h.Before(paAt(14, 10, 0)) {
			return 10
		}
		return 30
	})
	f := &paFake{fc: fc, realRun: true, currents: func(*legPaceProfile) (legCurrentField, error) { return crUniform(2), nil }}
	l := paAssess(t, p, legs, f).Legs[0]
	if l.Verdict != "go" || !paParse(t, l.Depart).Equal(paAt(14, 5, 15)) || paParse(t, l.Arrive).Sub(paAt(14, 9, 0)).Abs() > 2*time.Minute {
		t.Fatalf("leg = %+v", l)
	}
	if l.CurrentKn == nil || *l.CurrentKn != 2 || l.EtaBasis == nil || !strings.Contains(*l.EtaBasis, "current with you 2.0 kn") {
		t.Fatalf("current %v basis %v", l.CurrentKn, l.EtaBasis)
	}
}

// ── slips read the current ───────────────────────────────────────────────────

func TestSlipPlan_ReadsOneCurrentRequestForEachLegAcrossAllTheSlips(t *testing.T) {
	p, legs := slipPlan2(t, "2026-11-16T12:00")
	f := &paFake{fc: slipWind()}
	got := slipsOf(context.Background(), p, legs, paDeps(f))
	if len(got.Slips) != 2 {
		t.Fatalf("slips = %+v", got)
	}
	if f.reads != 2 {
		t.Fatalf("%d current requests for 2 legs over 2 slips, want one for each leg", f.reads)
	}
	if f.warningsOpen != 0 {
		t.Fatalf("%d warnings opens: a slip needs only verdicts", f.warningsOpen)
	}
}

// ── the basis words ──────────────────────────────────────────────────────────

func TestEtaBasisText_NamesTheCurrentAndWhatTheModelDidNotCover(t *testing.T) {
	min := time.Minute
	for _, c := range []struct {
		name string
		kn   float64
		slow time.Duration
		run  legRun
		want string
	}{
		{"against, uncovered, slow", 10.1, 5 * min, legRun{alongKn: -0.4, modelledNm: 8, unmodelledNm: 2}, "Cruise 10.1 kn, current against you 0.4 kn, current not modelled for 2 nm, 5 min in slow water."},
		{"with you", 8, 0, legRun{alongKn: 0.34, modelledNm: 10}, "Cruise 8.0 kn, current with you 0.3 kn."},
		{"rounds to nothing", 8, 0, legRun{alongKn: 0.04, modelledNm: 10}, "Cruise 8.0 kn, no current along the course."},
		{"rounds to nothing against", 8, 0, legRun{alongKn: -0.04, modelledNm: 10}, "Cruise 8.0 kn, no current along the course."},
		{"under half a mile uncovered", 8, 0, legRun{alongKn: -1, modelledNm: 10, unmodelledNm: 0.4}, "Cruise 8.0 kn, current against you 1.0 kn."},
		{"half a mile uncovered", 8, 0, legRun{alongKn: 1, modelledNm: 10, unmodelledNm: 0.5}, "Cruise 8.0 kn, current with you 1.0 kn, current not modelled for 1 nm."},
		{"whole miles", 8, 0, legRun{alongKn: 1, modelledNm: 10, unmodelledNm: 2.6}, "Cruise 8.0 kn, current with you 1.0 kn, current not modelled for 3 nm."},
		{"none modelled", 8, 0, legRun{unmodelledNm: 3}, "Cruise 8.0 kn, current not modelled for 3 nm."},
		{"none modelled and a little", 8, 40 * min, legRun{unmodelledNm: 0.3}, "Cruise 8.0 kn, 40 min in slow water."},
		{"no open water", 8, 40 * min, legRun{}, "Cruise 8.0 kn, 40 min in slow water."},
	} {
		if got := etaBasisText(c.kn, c.slow, c.run.currentWords()); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
