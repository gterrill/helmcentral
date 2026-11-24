package main

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

// Review pass 2: the leg under way, the fix placement, recorded fixes only, the
// window decided before the forecast, and failed runs.

// uwOffRoute is a point off the middle of the plan's first leg by nm nautical miles, at
// right angles to the route, to the east side when east is true.
func uwOffRoute(p plan, nm float64, east bool) (lat, lon float64) {
	a, b := p.Stops[0], p.Stops[1]
	cos := math.Cos((a.Lat + b.Lat) / 2 * math.Pi / 180)
	dx, dy := (b.Lon-a.Lon)*cos*60, (b.Lat-a.Lat)*60
	l := math.Hypot(dx, dy)
	px, py := -dy/l, dx/l // a quarter turn to the left of the route
	if (px > 0) != east {
		px, py = -px, -py
	}
	return (a.Lat+b.Lat)/2 + py*nm/60, (a.Lon+b.Lon)/2 + px*nm/60/cos
}

func uwFix(p plan, start, at time.Time, lat, lon float64) *planUnderway {
	s := p.Stops[0]
	return &planUnderway{Lat: s.Lat, Lon: s.Lon, Start: start, FixLat: lat, FixLon: lon, FixAt: at}
}

// 1: once the passage has begun the row shows when it began, the ETA from the fix, and
// no suggestion, ranking or verdict.
func TestAssessPlan_TheLegUnderWayShowsThePassageStartAndTheEtaFromTheFixNotASuggestion(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	a, b := p.Stops[0], p.Stops[1]
	f := &paFake{underway: &planUnderway{
		Lat: a.Lat, Lon: a.Lon, Start: paAt(14, 3, 0),
		FixLat: (a.Lat + b.Lat) / 2, FixLon: (a.Lon + b.Lon) / 2, FixAt: paAt(14, 4, 20),
	}}
	l := paAssess(t, p, legs, f).Legs[0]
	if l.Depart == "" || !paParse(t, l.Depart).Equal(paAt(14, 3, 0)) || l.DepartSuggested {
		t.Fatalf("depart %q suggested %v: the row shows when the passage began, as the boat's own", l.Depart, l.DepartSuggested)
	}
	if got := paParse(t, l.Arrive); got.Sub(paAt(14, 6, 50)).Abs() > 2*time.Minute {
		t.Fatalf("arrive %s, want about 6.50 AM from the fix", l.Arrive)
	}
	if l.Verdict != "" || l.DepartBasis != nil || l.Comfort != nil {
		t.Fatalf("leg = %+v: a leg under way is given no new verdict, ranking or basis", l)
	}
	if l.Hours == nil {
		t.Fatalf("leg = %+v: no hours", l)
	}
	end := paParse(t, l.Depart).Add(time.Duration(*l.Hours * float64(time.Hour)))
	if end.Sub(paParse(t, l.Arrive)).Abs() > 2*time.Second {
		t.Fatalf("depart %s + %.4f h is %s, not the arrival %s", l.Depart, *l.Hours, end, l.Arrive)
	}
}

// 1: the ETA of a leg under way is never taken from a suggested departure.
func TestAssessPlan_TheLegUnderWayIsNotRankedOrGivenASuggestedDeparture(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	a, b := p.Stops[0], p.Stops[1]
	stored := newTestDocumentStore(t)
	f := &paFake{store: stored, underway: &planUnderway{
		Lat: a.Lat, Lon: a.Lon, Start: paAt(14, 3, 0),
		FixLat: (a.Lat + b.Lat) / 2, FixLon: (a.Lon + b.Lon) / 2, FixAt: paAt(14, 4, 20),
	}}
	paAssess(t, p, legs, f)
	if _, ok, err := stored.KeptSuggestion(p.ID, planLegKey(p, 0), p.Stops[0].Depart, paLoc); err != nil || ok {
		t.Fatalf("a suggestion was kept for the leg under way (%v, %v)", ok, err)
	}
}

// 1: an ETA that has passed says the leg's ETA has passed, with no hours made up from now.
func TestAssessPlan_ALegUnderWayWhoseEtaHasPassedSaysSoWithNoHoursFromNow(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	end := p.Stops[1]
	f := &paFake{underway: uwFix(p, paAt(14, 0, 0), paAt(14, 4, 20), end.Lat, end.Lon)}
	l := paAssess(t, p, legs, f).Legs[0]
	if !strings.Contains(l.WindChanges.NotChecked, planETAPassed) {
		t.Fatalf("not checked = %q, want %q", l.WindChanges.NotChecked, planETAPassed)
	}
	if l.Depart == "" || !paParse(t, l.Depart).Equal(paAt(14, 0, 0)) || l.Hours == nil {
		t.Fatalf("leg = %+v", l)
	}
	if got := paParse(t, l.Depart).Add(time.Duration(*l.Hours * float64(time.Hour))); got.After(paAt(14, 4, 30)) {
		t.Fatalf("depart + hours = %s: hours are the passage's, not counted on from now", got)
	}
	if l.EtaBasis != nil {
		t.Fatalf("an ETA that has passed has a basis: %q", *l.EtaBasis)
	}
}

// 2: more than a mile off the route there is no ETA, and the distance is said.
func TestAssessPlan_ABoatWellOffTheRouteHasNoEtaAndSaysHowFar(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	lat, lon := uwOffRoute(p, 2, true)
	f := &paFake{underway: uwFix(p, paAt(14, 3, 0), paAt(14, 4, 20), lat, lon)}
	l := paAssess(t, p, legs, f).Legs[0]
	want := "No ETA: the boat is 2.0 nm off the planned route"
	if !strings.Contains(l.WindChanges.NotChecked, want) {
		t.Fatalf("not checked = %q, want %q", l.WindChanges.NotChecked, want)
	}
	if l.Arrive != "" || l.Hours != nil {
		t.Fatalf("arrive %q hours %v: no ETA from a boat that far off", l.Arrive, l.Hours)
	}
}

// 2: a boat a little off the route is placed at the foot of the perpendicular, not
// pulled along toward where the schedule says it should be.
func TestAssessPlan_ABoatBesideTheRouteIsPlacedAtTheFootNotPulledTowardTheSchedule(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	lat, lon := uwOffRoute(p, 0.3, true)
	// Half way is 150 minutes in still water; the boat is 20 minutes behind that.
	f := &paFake{underway: uwFix(p, paAt(14, 4, 20).Add(-170*time.Minute), paAt(14, 4, 20), lat, lon)}
	l := paAssess(t, p, legs, f).Legs[0]
	if l.Arrive == "" {
		t.Fatalf("leg = %+v", l)
	}
	if got := paParse(t, l.Arrive); got.Sub(paAt(14, 6, 50)).Abs() > 20*time.Second {
		t.Fatalf("arrive %s, want 6.50 AM: 15 nm left from the foot of the perpendicular at 6 kn", l.Arrive)
	}
}

// 2: the placement is a point on a step, with the share of the step left and how far
// off the route the fix is.
func TestLegPlace_IsTheFootOfThePerpendicularAndHowFarOffTheRouteTheFixIs(t *testing.T) {
	m := &legPaceModel{CruiseKn: 6, Slow: map[paceCell]float64{}}
	leg := planLeg{From: 0, To: 1, DistanceNm: 6, Route: [][2]float64{{-20.0, 149.0}, {-20.1, 149.0}}}
	p, err := m.profile(leg)
	if err != nil {
		t.Fatal(err)
	}
	lon := 149.0 + 0.3/60/math.Cos(20.05*math.Pi/180)
	at := p.place(-20.05, lon, 30*time.Minute)
	var behind float64
	for _, s := range p.steps[:at.step] {
		behind += s.nm
	}
	behind += (1 - at.left) * p.steps[at.step].nm
	if math.Abs(behind-3) > 0.01 || math.Abs(at.offNm-0.3) > 0.01 {
		t.Fatalf("placed %.3f nm along and %.3f nm off, want 3 and 0.3", behind, at.offNm)
	}
}

// Review 3: a fix still at the route's start, after the passage began, has the
// timeline start at the passage start, so the hours run from there to the ETA.
func TestLegRunFrom_AFixAtTheRoutesStartTimesFromThePassageStart(t *testing.T) {
	m := &legPaceModel{CruiseKn: 6, Slow: map[paceCell]float64{}}
	leg := planLeg{From: 0, To: 1, DistanceNm: 6, Route: [][2]float64{{-20.0, 149.0}, {-20.1, 149.0}}}
	p, err := m.profile(leg)
	if err != nil {
		t.Fatal(err)
	}
	start, at := paAt(14, 8, 0), paAt(14, 8, 40)
	r, err := p.runFrom(start, at, 0, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !r.tl.depart().Equal(start) {
		t.Fatalf("timeline departs %s, want the passage start %s", r.tl.depart(), start)
	}
	if got := r.tl.depart().Add(time.Duration(r.tl.hours() * float64(time.Hour))); got.Sub(r.tl.arrive()).Abs() > time.Second {
		t.Fatalf("depart + hours is %s, not the arrival %s", got, r.tl.arrive())
	}
	if want := at.Add(time.Hour); r.tl.arrive().Sub(want).Abs() > time.Second {
		t.Fatalf("arrive %s, want %s: 6 nm at 6 kn from the fix", r.tl.arrive(), want)
	}
}

// Review 3: a run that fails for the leg under way says so in the operator's words,
// not the error's.
func TestAssessPlan_ALegUnderWayWhoseRunFailsSaysItsEtaCouldNotBeWorkedOut(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	a, b := p.Stops[0], p.Stops[1]
	// The fix is on the leg but stamped at the instant the passage began.
	f := &paFake{underway: uwFix(p, paAt(14, 4, 20), paAt(14, 4, 20), (a.Lat+b.Lat)/2, (a.Lon+b.Lon)/2)}
	l := paAssess(t, p, legs, f).Legs[0]
	want := "No ETA: the ETA from the latest position could not be worked out"
	if l.WindChanges.NotChecked != want {
		t.Fatalf("not checked = %q, want %q", l.WindChanges.NotChecked, want)
	}
	if strings.Contains(l.Note, "not after") || l.Arrive != "" {
		t.Fatalf("leg = %+v: no raw error, no arrival", l)
	}
}

// Review 3: the leg under way shows the current over its remaining open-water steps,
// or why there is none.
func TestAssessPlan_TheLegUnderWayShowsItsCurrentFromTheRunFromTheFix(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	p.Stops[0].DepartTime = "04:00"
	a, b := p.Stops[0], p.Stops[1]
	f := &paFake{
		currents: func(*legPaceProfile) (legCurrentField, error) { return crUniform(0.5), nil },
		underway: uwFix(p, paAt(14, 4, 0), paAt(14, 4, 20), (a.Lat+b.Lat)/2, (a.Lon+b.Lon)/2),
	}
	l := paAssess(t, p, legs, f).Legs[0]
	if l.CurrentKn == nil || *l.CurrentKn != 0.5 {
		t.Fatalf("current %v note %q, want 0.5 from the run from the fix", l.CurrentKn, l.CurrentNote)
	}
}

func TestAssessPlan_TheLegUnderWayWhoseCurrentReadFailsIsBlankWithTheReason(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	a, b := p.Stops[0], p.Stops[1]
	f := &paFake{
		currentErr: errors.New("the Open-Meteo current request failed"),
		underway:   uwFix(p, paAt(14, 4, 0), paAt(14, 4, 20), (a.Lat+b.Lat)/2, (a.Lon+b.Lon)/2),
	}
	l := paAssess(t, p, legs, f).Legs[0]
	if l.CurrentKn != nil || !strings.Contains(l.CurrentNote, "request failed") {
		t.Fatalf("current %v note %q", l.CurrentKn, l.CurrentNote)
	}
	if l.Arrive == "" {
		t.Fatalf("a failed current read does not take the ETA: %+v", l)
	}
}

// Review 3: a verdicts-only assessment leaves the wind changes of a no-window leg as
// they are everywhere in it, not the unassessed sentence the full one gives.
func TestAssessPlan_VerdictsOnlyKeepsTheWindChangesOfALegWithNoWindow(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	newFake := func() *paFake {
		return &paFake{limits: crDaylightLimits(), run: crRun(func(time.Time) float64 { return 15 })}
	}
	full := paAssess(t, p, legs, newFake()).Legs[0]
	if full.NoteKind != planKindNoWindow || full.WindChanges.NotChecked == "" {
		t.Fatalf("full = %+v, want no window with its wind changes not read", full)
	}
	only := assessPlan(context.Background(), p, legs, paDeps(newFake()), planAssessOptions{VerdictsOnly: true}).Legs[0]
	if only.NoteKind != planKindNoWindow {
		t.Fatalf("verdicts only = %+v, want no window", only)
	}
	if only.WindChanges.NotChecked != "" || len(only.WindChanges.Changes) != 0 {
		t.Fatalf("wind changes = %+v, want none and no sentence", only.WindChanges)
	}
}

// 3: only a recorded point is the latest fix; an imported one never is.
func TestReadUnderway_AnImportedPointIsNeverTheLatestFix(t *testing.T) {
	store := newTestPassageStore(t)
	b := newTrackBuilder().run(10*time.Minute, time.Minute, 0).run(40*time.Minute, time.Minute, 6)
	for _, pt := range b.pts {
		if err := store.AppendTrackPoint(pt); err != nil {
			t.Fatal(err)
		}
	}
	last := b.last()
	if err := store.AppendTrackPoint(recordedTrackPoint{At: last.At.Add(5 * time.Minute), Lat: -25, Lon: 153, Source: trackSourceImported}); err != nil {
		t.Fatal(err)
	}
	got, err := readUnderway(context.Background(), store, last.At.Add(time.Minute))
	if err != nil || got == nil {
		t.Fatalf("underway = %+v (%v)", got, err)
	}
	if !got.FixAt.Equal(last.At) || got.FixLat != last.Lat || got.FixLon != last.Lon {
		t.Fatalf("fix at %s (%v, %v), want the recorder's last at %s (%v, %v)", got.FixAt, got.FixLat, got.FixLon, last.At, last.Lat, last.Lon)
	}
}

// 3: a recorded point stamped after now is not the latest fix: the fresh fix before
// it is used.
func TestReadUnderway_AFutureStampedPointIsNotTheLatestFix(t *testing.T) {
	store := newTestPassageStore(t)
	b := newTrackBuilder().run(10*time.Minute, time.Minute, 0).run(40*time.Minute, time.Minute, 6)
	for _, pt := range b.pts {
		if err := store.AppendTrackPoint(pt); err != nil {
			t.Fatal(err)
		}
	}
	last := b.last()
	future := recordedTrackPoint{At: last.At.Add(2 * time.Hour), Lat: -25, Lon: 153, Source: last.Source}
	if err := store.AppendTrackPoint(future); err != nil {
		t.Fatal(err)
	}
	got, err := readUnderway(context.Background(), store, last.At.Add(time.Minute))
	if err != nil || got == nil {
		t.Fatalf("underway = %+v (%v)", got, err)
	}
	if !got.FixAt.Equal(last.At) || got.FixLat != last.Lat {
		t.Fatalf("fix at %s (%v), want the fix a minute old at %s (%v)", got.FixAt, got.FixLat, last.At, last.Lat)
	}
}

// uwFetches is a forecast that says where it was read.
type uwFetches struct {
	mu   sync.Mutex
	at   [][2]float64
	fail error
}

func (u *uwFetches) source() forecastSource {
	inner := pwUniform(10, 190, 12, nil, 0, paAt(14, 0, 0), paAt(24, 23, 0))
	return forecastSource{Provider: "wx", Fetch: func(ctx context.Context, lat, lon float64) (passageForecast, error) {
		u.mu.Lock()
		u.at = append(u.at, [2]float64{lat, lon})
		u.mu.Unlock()
		if u.fail != nil {
			return passageForecast{}, u.fail
		}
		return inner.Fetch(ctx, lat, lon)
	}}
}

// near says the forecast was read within a mile of a stop.
func (u *uwFetches) near(s planStop) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, at := range u.at {
		if haversineMeters(at[0], at[1], s.Lat, s.Lon) < metersPerNauticalMile {
			return true
		}
	}
	return false
}

// 4: a leg whose window has passed never reads a forecast.
func TestAssessPlan_ALegWhoseWindowHasPassedReadsNoForecast(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	fx := &uwFetches{}
	f := &paFake{
		limits: crDaylightLimits(), fc: fx.source(),
		run: crRun(func(dep time.Time) float64 {
			if dep.Before(paAt(14, 13, 0)) {
				return 5
			}
			return 6
		}),
	}
	d := paDeps(f)
	d.now = func() time.Time { return paAt(14, 13, 10) }
	out := assessPlan(context.Background(), p, legs, d, planAssessOptions{})
	if l := out.Legs[0]; l.NoteKind != planKindWindowPassed {
		t.Fatalf("leg 0 = %+v, want the window passed", l)
	}
	if fx.near(p.Stops[0]) {
		t.Fatalf("the forecast was read at the first stop, which only leg 0 starts from: %v", fx.at)
	}
	if !fx.near(p.Stops[2]) {
		t.Fatalf("leg 1 was not read: %v", fx.at)
	}
}

// 4: a leg with no window never reads a forecast either.
func TestAssessPlan_ALegWithNoWindowReadsNoForecast(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	fx := &uwFetches{}
	f := &paFake{limits: crDaylightLimits(), fc: fx.source(), run: crRun(func(time.Time) float64 { return 15 })}
	out := paAssess(t, p, legs, f)
	if l := out.Legs[0]; l.NoteKind != planKindNoWindow {
		t.Fatalf("leg = %+v", l)
	}
	if len(fx.at) != 0 {
		t.Fatalf("%d forecast reads for a leg that has no window", len(fx.at))
	}
}

// 4: a forecast that fails leaves the leg that needed none as it was.
func TestAssessPlan_AForecastThatFailsLeavesALegWithNoWindowAsItWas(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	fx := &uwFetches{fail: errors.New("the forecast service is down")}
	f := &paFake{
		limits: crDaylightLimits(), fc: fx.source(),
		run: crRun(func(dep time.Time) float64 {
			if dep.Before(paAt(14, 13, 0)) {
				return 5
			}
			return 6
		}),
	}
	d := paDeps(f)
	d.now = func() time.Time { return paAt(14, 13, 10) }
	out := assessPlan(context.Background(), p, legs, d, planAssessOptions{})
	if l := out.Legs[0]; l.NoteKind != planKindWindowPassed || l.Note != planNoteWindowPassed {
		t.Fatalf("leg 0 = %+v: it needed no forecast, so its note stands", l)
	}
	if l := out.Legs[1]; !strings.HasPrefix(l.Note, "Forecast failed") || l.NoteKind != planKindProblem {
		t.Fatalf("leg 1 = %+v: it needed the forecast, which failed", l)
	}
}

// 5: a run that fails is not in the window, whether it is past or to come.
func TestDepartureCandidates_AFailedRunIsNeverInTheWindow(t *testing.T) {
	now := paAt(14, 6, 5)
	ok := func(h, m int) departureRun {
		tl, _ := linearTimeline(paAt(14, h, m), 3)
		return departureRun{dep: paAt(14, h, m), run: legRun{tl: tl}}
	}
	bad := func(h, m int) departureRun {
		return departureRun{dep: paAt(14, h, m), err: errors.New("no timeline")}
	}
	fits, kind, _ := departureCandidates([]departureRun{bad(5, 45), ok(6, 15), bad(6, 30)}, now, time.Time{})
	if kind != planKindNone || len(fits) != 1 || !fits[0].dep.Equal(paAt(14, 6, 15)) {
		t.Fatalf("fits %+v kind %q: only the departure that ran is a candidate", fits, kind)
	}
	// A failed past run does not make the day have had a window.
	_, kind, _ = departureCandidates([]departureRun{bad(5, 45), ok(7, 15)}, paAt(14, 7, 0), paAt(14, 9, 0))
	if kind != planKindNoWindow {
		t.Fatalf("kind %q: a past run that failed is not a departure that fit", kind)
	}
	// Every departure to come failing says the timeline could not be worked out.
	_, kind, note := departureCandidates([]departureRun{ok(5, 45), bad(6, 15), bad(6, 30)}, now, time.Time{})
	if kind != planKindNoDeparture || note != planNoteNoTimeline {
		t.Fatalf("kind %q note %q, want the timeline could not be worked out", kind, note)
	}
}

// 5: a leg whose every departure fails says so in the operator's words, not the error's.
func TestAssessPlan_ALegWhoseRunsAllFailSaysItsTimelineCouldNotBeWorkedOut(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	f := &paFake{run: func(*legPaceProfile, time.Time, legCurrentField) (legRun, error) {
		return legRun{}, errors.New("the pace gave no timeline for 6.15 AM")
	}}
	l := paAssess(t, p, legs, f).Legs[0]
	want := "No departure suggested: the leg's timeline could not be worked out"
	if l.Note != want || l.NoteKind != planKindNoDeparture || l.Depart != "" || l.Verdict != "" {
		t.Fatalf("leg = %+v, want note %q", l, want)
	}
}
