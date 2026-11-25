package main

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

// A leg's run carries the modelled current beside its timeline. In this release the
// current is only read, never added to the speed, so every run is the pace's own.

// crField is a current field made of a function, for the runs the tests build.
type crField struct {
	along func(k int, t time.Time) (float64, bool)
	why   string
}

func (f crField) alongAt(_ *legPaceProfile, k int, t time.Time) (float64, bool) {
	return f.along(k, t)
}
func (f crField) none() string { return f.why }

// runsOf runs the leg from each departure with the field, as readLeg does.
func runsOf(p *legPaceProfile, f legCurrentField, deps ...time.Time) []departureRun {
	runs := make([]departureRun, len(deps))
	for i, dep := range deps {
		runs[i].dep = dep
		runs[i].run, runs[i].err = p.run(dep, f)
	}
	return runs
}

// crUniform is the same current at every step and every time.
func crUniform(kn float64) crField {
	return crField{along: func(int, time.Time) (float64, bool) { return kn, true }}
}

// paClockKey is the time of day of a departure on the stop's clock, to the second:
// the stubs list the departures of one day, and a later leg of the plan leaves at the
// same times of its own day.
func paClockKey(t time.Time) int {
	t = t.In(paLoc)
	return (t.Hour()*60+t.Minute())*60 + t.Second()
}

// paStubField answers the current by the departure that reaches a step at a time:
// the departures the stubs list have a mean, any other has none. offset is how long
// the boat in still water takes to reach each step.
type paStubField struct {
	offset []time.Duration
	means  map[int]float64
	why    string
}

func (s paStubField) alongAt(_ *legPaceProfile, k int, t time.Time) (float64, bool) {
	kn, ok := s.means[paClockKey(t.Add(-s.offset[k]))]
	return kn, ok
}
func (s paStubField) none() string { return s.why }

// paStillWater is how long the boat takes to reach each step of a leg at the steps'
// own speeds.
func paStillWater(p *legPaceProfile) []time.Duration {
	out := make([]time.Duration, len(p.steps))
	var hours float64
	for k, s := range p.steps {
		out[k] = hoursDuration(hours)
		hours += s.nm / s.kn
	}
	return out
}

// readCurrents is the fake's answer to a leg's current read: the departures the
// stubs list, each with its mean, a suggested leg's by the departures stub and an
// operator's by the current stub.
func (f *paFake) readCurrents(p *legPaceProfile) (legCurrentField, error) {
	f.mu.Lock()
	f.reads++
	f.mu.Unlock()
	if f.currentErr != nil {
		return nil, f.currentErr
	}
	if f.currents != nil {
		return f.currents(p)
	}
	field := paStubField{offset: paStillWater(p), means: map[int]float64{}}
	for key, r := range f.current {
		if r.Have {
			field.means[key] = r.MeanKn
		} else if field.why == "" {
			field.why = r.Why
		}
	}
	cands := []tidalDeparture{paDeparture(6, 15, 0.4)}
	if f.departures != nil {
		var err error
		if cands, err = f.departures(); err != nil {
			return nil, err
		}
	}
	for _, c := range cands {
		if c.Current.Have {
			field.means[paClockKey(c.Depart)] = c.Current.MeanKn
		} else if field.why == "" {
			field.why = c.Current.Why
		}
	}
	return field, nil
}

// paCurrentAt is the reading the model gives for a departure at a time of day.
func paCurrentAt(hour, min int, r planCurrentReading) map[int]planCurrentReading {
	return map[int]planCurrentReading{paClockKey(paAt(14, hour, min)): r}
}

// crRun gives each departure the hours the table says, with the figures of the real run.
func crRun(hours func(dep time.Time) float64) func(p *legPaceProfile, dep time.Time, f legCurrentField) (legRun, error) {
	return func(p *legPaceProfile, dep time.Time, f legCurrentField) (legRun, error) {
		r, err := p.run(dep, f)
		if err != nil {
			return r, err
		}
		tl, err := linearTimeline(dep, hours(dep))
		r.tl = tl
		return r, err
	}
}

func crDaylightLimits() passageLimits {
	l := pwLimitsAll()
	l.DaylightArrival = true
	return l
}

// 5: a later departure that arrives earlier is in the window on its own arrival.
func TestLegCurrent_ADepartureIsInTheWindowByItsOwnArrival(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	// Sunset is 5.32 PM. A departure takes 5 hours, so by 12.30 PM it arrives in
	// daylight and from 12.45 PM it does not; the 1.00 PM departure alone takes 2.
	f := &paFake{
		limits: crDaylightLimits(),
		run: crRun(func(dep time.Time) float64 {
			if dep.Equal(paAt(14, 13, 0)) {
				return 2
			}
			return 5
		}),
		departures: func() ([]tidalDeparture, error) {
			return []tidalDeparture{paDeparture(13, 0, 0.9), paDeparture(12, 45, 0.95), paDeparture(12, 30, 0.5)}, nil
		},
	}
	l := paAssess(t, p, legs, f).Legs[0]
	if !l.DepartSuggested || !paParse(t, l.Depart).Equal(paAt(14, 13, 0)) || !paParse(t, l.Arrive).Equal(paAt(14, 15, 0)) {
		t.Fatalf("leg = %+v: 1.00 PM arrives at 3.00 PM and is the fairest in the window, 12.45 PM is fairer but arrives after sunset", l)
	}
}

// 6: every departure from now on arrives after sunset, though earlier ones fit.
func TestLegCurrent_WhenNoDepartureFromNowFitsTheWindowHasPassed(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	f := &paFake{
		limits: crDaylightLimits(),
		run: crRun(func(dep time.Time) float64 {
			switch {
			case dep.Before(paAt(14, 13, 0)):
				return 5
			case dep.Minute()%15 == 0:
				return 6
			}
			return 2
		}),
	}
	d := paDeps(f)
	d.now = func() time.Time { return paAt(14, 13, 10) }
	l := assessPlan(context.Background(), p, legs, d, planAssessOptions{}).Legs[0]
	if l.Note != planNoteWindowPassed || l.NoteKind != planKindWindowPassed || l.Depart != "" || l.Verdict != "" {
		t.Fatalf("leg = %+v", l)
	}
	if f.reads != 1 {
		t.Fatalf("%d current reads: the leg's current is read once, first, and the runs say the window has passed", f.reads)
	}
}

// 6: and when none fits at any hour of the day there never was a window.
func TestLegCurrent_WhenNoDepartureEverFitsThereIsNoWindow(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	f := &paFake{limits: crDaylightLimits(), run: crRun(func(time.Time) float64 { return 15 })}
	l := paAssess(t, p, legs, f).Legs[0]
	if l.Note != planNoteNoWindow || l.NoteKind != planKindNoWindow || l.Depart != "" {
		t.Fatalf("leg = %+v", l)
	}
	if l.WindChanges.NotChecked == "" {
		t.Fatalf("a leg with no window says its wind changes were not read: %+v", l.WindChanges)
	}
}

// 7: a current request that fails leaves the operator's departure with no ETA, since
// the current is in the speed (leg_current_speed_test.go has the full cases).
func TestLegCurrent_AFailedRequestLeavesTheOperatorsDepartureWithNoEtaAndNoCurrent(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	p.Stops[0].DepartTime = "08:00"
	f := &paFake{currentErr: errors.New("the Open-Meteo current request failed")}
	l := paAssess(t, p, legs, f).Legs[0]
	if l.CurrentKn != nil || l.Note != "No ETA: the modelled current could not be read" {
		t.Fatalf("current %v, note %q", l.CurrentKn, l.Note)
	}
	if l.Verdict != "" || l.Arrive != "" || l.Hours != nil {
		t.Fatalf("no ETA means no verdict: %+v", l)
	}
}

// 7: a leg that is suggested a departure has nothing to rank by when the request fails.
func TestLegCurrent_AFailedRequestLeavesASuggestedLegWithNoDeparture(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	f := &paFake{currentErr: errors.New("the Open-Meteo current request failed")}
	l := paAssess(t, p, legs, f).Legs[0]
	if l.NoteKind != planKindNoDeparture || l.Note != "No departure suggested: the modelled current could not be read" || l.Depart != "" {
		t.Fatalf("leg = %+v", l)
	}
}

// crLeg is a leg of nm nautical miles north of the berth cell, at the model given.
func crLeg(t *testing.T, m *legPaceModel, nm float64) (*legPaceProfile, planLeg) {
	t.Helper()
	leg := lpLeg(lpBerthLat, lpBerthLat+lpDeg(nm))
	p, err := m.profile(leg)
	if err != nil {
		t.Fatal(err)
	}
	return p, leg
}

// 8: the harbour ends are slow water, which the model has no current for, and
// they are not counted as missing.
func TestLegRun_SlowWaterIsNeitherModelledNorUnmodelled(t *testing.T) {
	m := lpModel(lpBoats[0])
	endLat := lpBerthLat + lpDeg(3)
	m.Slow[cellOf(endLat, 149.0)] = 2
	p, _ := crLeg(t, m, 3)
	slowNm := 0.0
	for _, s := range p.steps {
		if s.slow {
			slowNm += s.nm
		}
	}
	if slowNm < 0.3 {
		t.Fatalf("setup: only %.2f nm of slow water", slowNm)
	}
	// The model has a current at every open-water step and none at the slow ones.
	field := crField{along: func(k int, _ time.Time) (float64, bool) { return 1.5, !p.steps[k].slow }}
	r, err := p.run(lpNow, field)
	if err != nil {
		t.Fatal(err)
	}
	if r.unmodelledNm != 0 || math.Abs(r.modelledNm-(3-slowNm)) > 1e-6 {
		t.Fatalf("modelled %.3f unmodelled %.3f, want %.3f and 0: the slow water is in neither", r.modelledNm, r.unmodelledNm, 3-slowNm)
	}
	if math.Abs(r.alongKn-1.5) > 1e-9 {
		t.Fatalf("mean = %v", r.alongKn)
	}
	// With the model blank at the far end of the open water, those miles are unmodelled.
	half := len(p.steps) / 2
	field = crField{along: func(k int, _ time.Time) (float64, bool) { return 1.5, k < half && !p.steps[k].slow }}
	r, err = p.run(lpNow, field)
	if err != nil {
		t.Fatal(err)
	}
	if r.unmodelledNm < 1 || r.modelledNm < 1 {
		t.Fatalf("modelled %.3f unmodelled %.3f", r.modelledNm, r.unmodelledNm)
	}
	if got := r.modelledNm + r.unmodelledNm + slowNm; math.Abs(got-3) > 1e-6 {
		t.Fatalf("the three parts add to %.4f nm, want the leg's 3", got)
	}
}

// 8: the mean is weighted by distance, not by step count.
func TestLegRun_TheMeanIsWeightedByTheDistanceOfEachStep(t *testing.T) {
	p, _ := crLeg(t, &legPaceModel{CruiseKn: 8, Slow: map[paceCell]float64{}}, 3)
	// The first fifth of the route has 2 kn, the rest 1 kn: a mean by distance of 1.2.
	field := crField{along: func(k int, _ time.Time) (float64, bool) {
		if k < len(p.steps)/5 {
			return 2, true
		}
		return 1, true
	}}
	r, err := p.run(lpNow, field)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(r.alongKn-1.2) > 0.01 {
		t.Fatalf("mean = %v", r.alongKn)
	}
}

// 9: a step past the end of a series, or in a gap of more than an hour, has no current.
func TestLegRun_AStepPastTheSeriesOrInAGapOfOverAnHourIsNotModelled(t *testing.T) {
	p, _ := crLeg(t, &legPaceModel{CruiseKn: 8, Slow: map[paceCell]float64{}}, 32) // 4 hours
	northMS := 1 / metersPerSecondToKnots                                          // 1 kn north
	at := func(h float64) tidalCurrentPoint {
		return tidalCurrentPoint{t: lpNow.Add(hoursDuration(h)), n: northMS}
	}
	// Hours 0 to 1 are covered; 1 to 3 is a gap of two hours; the series ends at 3.
	series := tidalCurrents{at(0), at(1), at(3)}
	field := crField{along: func(_ int, t time.Time) (float64, bool) {
		e, n, ok := series.at(t)
		_ = e
		return n * metersPerSecondToKnots, ok
	}}
	r, err := p.run(lpNow, field)
	if err != nil {
		t.Fatal(err)
	}
	// The current is in the speed: the first hour is covered at 9 kn, 9 nm of it.
	if math.Abs(r.modelledNm-9) > 0.2 || math.Abs(r.unmodelledNm-23) > 0.2 {
		t.Fatalf("modelled %.2f, unmodelled %.2f: want the first hour (9 nm) and no more", r.modelledNm, r.unmodelledNm)
	}
	if math.Abs(r.alongKn-1) > 1e-6 {
		t.Fatalf("mean = %v over the covered part", r.alongKn)
	}
}

// With no current modelled anywhere the run is the pace's own timeline; with one it
// is not (leg_current_speed_test.go).
func TestLegRun_IsThePacesTimelineWhenTheModelHasNoCurrent(t *testing.T) {
	p, _ := crLeg(t, lpModel(lpBoats[0]), 3)
	want, err := p.pace(lpNow)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []legCurrentField{crUniform(0), crField{along: func(int, time.Time) (float64, bool) { return 0, false }, why: "none"}} {
		r, err := p.run(lpNow, f)
		if err != nil || len(r.tl.knots) != len(want.knots) || !r.tl.arrive().Equal(want.arrive()) {
			t.Fatalf("run %+v err %v, want the pace's %v", r.tl, err, want.arrive())
		}
	}
	none := crField{along: func(int, time.Time) (float64, bool) { return 0, false }}
	w, err := p.integrate(lpNow, 0, 1, none)
	if err != nil || w.modelledNm != 0 || p.slowHours() <= 0 {
		t.Fatalf("a field with no current models nothing, and the slow water is timed: %+v, %v", w, err)
	}
	if still, err := p.integrate(lpNow, 0, 1, nil); err != nil || len(still.knots) != len(w.knots) {
		t.Fatalf("no field is the still-water walk: %+v against %+v", still, w)
	}
}

// 11: the leg under way is run from the latest fix, projected on the route.
func TestAssessPlan_TheLegUnderWayIsRunFromTheLatestFix(t *testing.T) {
	for _, off := range []struct {
		name     string
		eastDegs float64
		tol      float64
	}{{"on the route", 0, 0.05}, {"a little beside it", 0.3 / 60, 0.05}} {
		t.Run(off.name, func(t *testing.T) {
			p, legs := paPlan(t, "2026-11-14", 0, 0)
			p.Stops[0].DepartTime = "03:00" // before now (4.30 AM): the leg is under way
			a, b := p.Stops[0], p.Stops[1]
			f := &paFake{underway: &planUnderway{
				Lat: a.Lat, Lon: a.Lon, Start: paAt(14, 3, 0),
				FixLat: (a.Lat + b.Lat) / 2, FixLon: (a.Lon+b.Lon)/2 + off.eastDegs, FixAt: paAt(14, 4, 20),
			}}
			l := paAssess(t, p, legs, f).Legs[0]
			// Half way (15 of the 30 nm) at 4.20 AM with 15 nm to go at 6 kn is 6.50 AM,
			// 3.83 hours after the passage began at 3.00 AM. The leg's own 5 hours shifted
			// to end at the ETA would say 5. A boat 9 nm off the route is placed at the
			// step nearest it, which is a little along from the middle.
			if l.Hours == nil || math.Abs(*l.Hours-23.0/6) > off.tol {
				t.Fatalf("hours = %v, want 3.83: the ETA is 6.50 AM from the fix", l.Hours)
			}
		})
	}
}

// 12: the current is read once for a leg, with the same points and days however
// the window falls.
func TestReadLegCurrents_OneRequestKeyedByTheLegAlone(t *testing.T) {
	prov := steadyCurrent(180)
	deps := tidalDepsWithProvider(prov)
	p, leg := crLeg(t, &legPaceModel{CruiseKn: 8, Slow: map[paceCell]float64{}}, 20)
	deps.now = func() time.Time { return paNow }
	if _, err := deps.readLegCurrents(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	deps.now = func() time.Time { return paNow.Add(5 * time.Hour) }
	if _, err := deps.readLegCurrents(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if len(prov.calls) != 2 || prov.lastDays != assistantTidalMaxDays {
		t.Fatalf("%d calls, last for %d days, want 2 calls for %d", len(prov.calls), prov.lastDays, assistantTidalMaxDays)
	}
	if len(prov.calls[0]) != len(prov.calls[1]) {
		t.Fatalf("the points moved with the clock: %v vs %v", prov.calls[0], prov.calls[1])
	}
	for i := range prov.calls[0] {
		if prov.calls[0][i] != prov.calls[1][i] {
			t.Fatalf("point %d moved with the clock", i)
		}
	}
	// One point for each 1/12 degree cell, at the first step in it, in route order.
	var want []currentsPoint
	seen := map[tidalGridPoint]bool{}
	for _, s := range p.steps {
		if c := roundToTidalGrid(s.lat, s.lon); !seen[c] {
			seen[c] = true
			want = append(want, currentsPoint{Lat: s.lat, Lon: s.lon})
		}
	}
	if len(want) < 3 || len(prov.calls[0]) != len(want) {
		t.Fatalf("%d points asked for, want %d (%d nm of route)", len(prov.calls[0]), len(want), int(leg.DistanceNm))
	}
	for i := range want {
		if prov.calls[0][i] != want[i] {
			t.Fatalf("point %d = %+v, want %+v", i, prov.calls[0][i], want[i])
		}
	}
}

// 12: a leg needing more cells than one request carries is an error.
func TestReadLegCurrents_ALegOverTheRequestCapIsAnError(t *testing.T) {
	deps := tidalDepsWithProvider(steadyCurrent(180))
	deps.now = func() time.Time { return paNow }
	p, err := (&legPaceModel{CruiseKn: 8, Slow: map[paceCell]float64{}}).profile(lpLeg(-20, -30))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deps.readLegCurrents(context.Background(), p); err == nil || !strings.Contains(err.Error(), "100") {
		t.Fatalf("err = %v", err)
	}
}

// 13: a candidate is judged on its own timeline, the one the row will show.
func TestAssessPlan_ACandidateIsJudgedOnItsOwnTimeline(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	// A gale ahead from 4 PM. The 1.00 PM departure takes 2 hours on its timeline and
	// is over by 3 PM; the leg's own 5 hours would have it in the gale at 6 PM.
	fc := paWind(func(h time.Time) float64 {
		if h.Before(paAt(14, 16, 0)) {
			return 10
		}
		return 30
	})
	f := &paFake{
		fc: fc,
		run: crRun(func(dep time.Time) float64 {
			if dep.Equal(paAt(14, 13, 0)) {
				return 2
			}
			return 5
		}),
		departures: func() ([]tidalDeparture, error) {
			return []tidalDeparture{paDeparture(13, 0, 0.9)}, nil
		},
	}
	for _, l := range paAssess(t, p, legs, f).Legs[:1] {
		if l.Verdict != "go" || !paParse(t, l.Arrive).Equal(paAt(14, 15, 0)) {
			t.Fatalf("leg = %+v: the verdict is for the 2 hour run", l)
		}
	}
}

// 14: a suggested leg with no chosen departure shows no hours, no speed and no basis.
func TestAssessPlan_ASuggestedLegWithNoDepartureShowsNoHoursAndNoBasis(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	f := &paFake{currents: func(*legPaceProfile) (legCurrentField, error) {
		return crField{along: func(int, time.Time) (float64, bool) { return 0, false }, why: "no current near shore"}, nil
	}}
	l := paAssess(t, p, legs, f).Legs[0]
	if l.NoteKind != planKindNoDeparture || !strings.Contains(l.Note, "no current near shore") {
		t.Fatalf("leg = %+v", l)
	}
	if l.Depart != "" || l.Arrive != "" || l.EtaBasis != nil {
		t.Fatalf("a leg with no departure shows no time and no basis: %+v", l)
	}
	if l.Hours != nil || l.SpeedKn != nil {
		t.Fatalf("hours %v speed %v: no still-water reference reaches the wire", l.Hours, l.SpeedKn)
	}
}

// 15: the figure on the row is the run's, rounded to a tenth, and a mean that
// rounds to 0.0 is a figure, not a missing one.
func TestAssessPlan_TheCurrentFigureIsTheRunsMeanRoundedToATenth(t *testing.T) {
	for _, tc := range []struct {
		mean, want float64
	}{{0.26, 0.3}, {-0.84, -0.8}, {0.04, 0}, {-0.04, 0}} {
		p, legs := paPlan(t, "2026-11-14", 0, 0)
		p.Stops[0].DepartTime = "08:00"
		f := &paFake{currents: func(*legPaceProfile) (legCurrentField, error) { return crUniform(tc.mean), nil }}
		l := paAssess(t, p, legs, f).Legs[0]
		if l.CurrentKn == nil || *l.CurrentKn != tc.want {
			t.Fatalf("mean %v: current %v, want %v", tc.mean, l.CurrentKn, tc.want)
		}
	}
}

// 16: Mate's ranking takes one duration for every departure, as it always had.
func TestRankTidalDepartures_ErrorsWhenTheRunDurationsDiffer(t *testing.T) {
	q := tidalRankQuery{
		Lat: -27, Lon: 153, CourseDeg: 90, Hours: 2,
		Pace: func(dep time.Time) (legTimeline, error) {
			if dep.After(ltDepart) {
				return linearTimeline(dep, 3)
			}
			return linearTimeline(dep, 2)
		},
		Earliest: ltDepart, Latest: ltDepart.Add(time.Hour), Now: ltDepart,
	}
	_, err := assistantToolDeps{now: func() time.Time { return ltDepart }}.rankTidalDepartures(context.Background(), q)
	if err == nil || !strings.Contains(err.Error(), "same time") {
		t.Fatalf("err = %v", err)
	}
}

// BenchmarkLegRunCurrent is one leg's search: 48 departures (a quarter hour apart
// across 12 hours) each run over about 400 steps, with the modelled current of 16
// days of hourly series in every cell of the leg, as the page does it.
func BenchmarkLegRunCurrent(b *testing.B) {
	m := &legPaceModel{CruiseKn: 8, Slow: map[paceCell]float64{cellOf(lpBerthLat, 149.0): 1.5}}
	p, err := m.profile(lpLeg(lpBerthLat, lpBerthLat+lpDeg(60)))
	if err != nil {
		b.Fatal(err)
	}
	var series tidalCurrents
	for h := range 16 * 24 {
		series = append(series, tidalCurrentPoint{t: lpNow.Add(time.Duration(h-24) * time.Hour), e: 0.2 * math.Sin(float64(h)/2), n: 0.6 * math.Cos(float64(h)/2)})
	}
	field := legCellField{series: make([]tidalCurrents, len(p.cells))}
	for i := range field.series {
		field.series[i] = series
	}
	var deps []time.Time
	for d := range 48 {
		deps = append(deps, lpNow.Add(time.Duration(d)*planDepartureStep))
	}
	b.Logf("%d steps, %d departures", len(p.steps), len(deps))
	b.ResetTimer()
	for range b.N {
		runs := make([]departureRun, len(deps))
		for i, dep := range deps {
			runs[i].dep = dep
			runs[i].run, runs[i].err = p.run(dep, field)
		}
		if got, err := orderDepartures(runs, field); err != nil || len(got) != len(deps) {
			b.Fatalf("%d ranked, %v", len(got), err)
		}
	}
}
