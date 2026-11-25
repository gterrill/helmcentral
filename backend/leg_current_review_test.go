package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// crCount is a current field that counts the lookups made of it and keeps the
// earliest time it was asked for.
type crCount struct {
	mu   sync.Mutex
	n    int
	minT time.Time
}

func (c *crCount) alongAt(_ *legPaceProfile, _ int, t time.Time) (float64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	if c.minT.IsZero() || t.Before(c.minT) {
		c.minT = t
	}
	return 0, true
}
func (c *crCount) none() string { return "" }

func crCounting(c *crCount) func(*legPaceProfile) (legCurrentField, error) {
	return func(*legPaceProfile) (legCurrentField, error) { return c, nil }
}

// Review 1: a forecast that fails when no leg is left to judge is not the plan's problem.
func TestAssessPlan_AForecastFailureIsNotThePlansProblemWhenNoLegNeededIt(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	f := &paFake{
		limits: crDaylightLimits(),
		run: crRun(func(dep time.Time) float64 {
			if dep.Before(paAt(14, 13, 0)) {
				return 5
			}
			return 6
		}),
		fc: forecastSource{Provider: "wx", Fetch: func(context.Context, float64, float64) (passageForecast, error) {
			return passageForecast{}, errors.New("the forecast service is down")
		}},
	}
	d := paDeps(f)
	d.now = func() time.Time { return paAt(14, 13, 10) }
	out := assessPlan(context.Background(), p, legs, d, planAssessOptions{})
	l := out.Legs[0]
	if out.Problem != "" || l.Note != planNoteWindowPassed || l.NoteKind != planKindWindowPassed {
		t.Fatalf("problem %q, leg %+v: the window passed, and no leg needed the forecast", out.Problem, l)
	}
}

// Review 2: the leg under way shows its arrival from the fix.
func TestAssessPlan_TheLegUnderWayShowsItsArrivalFromTheFix(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	a, b := p.Stops[0], p.Stops[1]
	f := &paFake{underway: &planUnderway{
		Lat: a.Lat, Lon: a.Lon, Start: paAt(14, 3, 0),
		FixLat: (a.Lat + b.Lat) / 2, FixLon: (a.Lon + b.Lon) / 2, FixAt: paAt(14, 4, 20),
	}}
	l := paAssess(t, p, legs, f).Legs[0]
	// Half way at 4.20 AM with 15 nm to go at 6 kn: 6.50 AM.
	if l.Arrive == "" || l.EtaBasis == nil {
		t.Fatalf("leg = %+v, want an arrival and its basis", l)
	}
	if got := paParse(t, l.Arrive); got.Sub(paAt(14, 6, 50)).Abs() > 2*time.Minute {
		t.Fatalf("arrive %s, want about 6.50 AM from the fix, not the suggested departure's", l.Arrive)
	}
}

// Review 3: a fix more than legFixMaxAge old gives no ETA.
func TestAssessPlan_AStaleFixGivesTheLegUnderWayNoEta(t *testing.T) {
	for _, c := range []struct {
		fixAt time.Time
		want  string
	}{{paAt(14, 4, 10), "No ETA: the latest position is 20 min old"}, {paAt(14, 3, 25), "No ETA: the latest position is 1 h 5 min old"}} {
		p, legs := paPlan(t, "2026-11-14", 0, 0)
		a, b := p.Stops[0], p.Stops[1]
		f := &paFake{underway: &planUnderway{
			Lat: a.Lat, Lon: a.Lon, Start: paAt(14, 3, 0),
			FixLat: (a.Lat + b.Lat) / 2, FixLon: (a.Lon + b.Lon) / 2, FixAt: c.fixAt,
		}}
		l := paAssess(t, p, legs, f).Legs[0]
		if !strings.Contains(l.WindChanges.NotChecked, c.want) {
			t.Fatalf("not checked = %q, want %q", l.WindChanges.NotChecked, c.want)
		}
		if l.Arrive != "" && paParse(t, l.Arrive).Equal(paAt(14, 6, 40)) {
			t.Fatalf("the arrival %s was taken from the stale fix", l.Arrive)
		}
	}
}

// Review 4: a route that passes near itself places the fix by the time as well.
func TestLegPlace_ARouteThatPassesNearItselfPlacesByTheTimeSinceTheStart(t *testing.T) {
	m := &legPaceModel{CruiseKn: 6, Slow: map[paceCell]float64{}}
	leg := planLeg{From: 0, To: 1, DistanceNm: 12, Route: [][2]float64{{-20.9, 149.0}, {-20.8, 149.0}, {-20.8, 149.0003}, {-20.9, 149.0003}}}
	p, err := m.profile(leg)
	if err != nil {
		t.Fatal(err)
	}
	// 0.0002 degrees from the way out and 0.0001 from the way back, and the other way.
	if k := p.place(-20.85, 149.0002, 30*time.Minute).step; k >= len(p.steps)/2 {
		t.Fatalf("step %d of %d: half an hour in, the boat is on the way out", k, len(p.steps))
	}
	if k := p.place(-20.85, 149.0001, 90*time.Minute).step; k < len(p.steps)/2 {
		t.Fatalf("step %d of %d: an hour and a half in, the boat is on the way back", k, len(p.steps))
	}
	// Well off either, the nearest step still wins whatever the time.
	if k := p.place(-20.8, 149.0, 90*time.Minute).step; k >= len(p.steps)/2+len(p.steps)/4 || k < len(p.steps)/4 {
		t.Fatalf("step %d of %d: the top of the route", k, len(p.steps))
	}
}

// Review 5: a leg with no open water is ranked without a current.
func TestAssessPlan_ALegAllInSlowWaterIsRankedWithoutCurrent(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	a, b := p.Stops[0], p.Stops[1]
	slow := map[paceCell]float64{}
	for i := 0; i <= 4000; i++ {
		u := float64(i) / 4000
		slow[cellOf(a.Lat+(b.Lat-a.Lat)*u, a.Lon+(b.Lon-a.Lon)*u)] = 6
	}
	l := paAssess(t, p, legs, &paFake{pace: &legPaceModel{CruiseKn: 6, Slow: slow}}).Legs[0]
	if l.Note != "" || !l.DepartSuggested || l.Depart == "" || l.Verdict == "" {
		t.Fatalf("leg = %+v: a leg in slow water is still suggested a departure and judged", l)
	}
	if l.CurrentKn != nil || l.CurrentNote != "" {
		t.Fatalf("current %v, note %q: there is no open water to say anything of", l.CurrentKn, l.CurrentNote)
	}
	if !paParse(t, l.Depart).Equal(paAt(14, 5, 15)) {
		t.Fatalf("depart %s: departures that fit stay in grid order", l.Depart)
	}
}

// Review 6: the leg under way reads the current and hands it to its run.
func TestAssessPlan_TheLegUnderWayRunsWithTheCurrent(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	p.Stops[0].DepartTime = "03:00"
	a, b := p.Stops[0], p.Stops[1]
	cnt := &crCount{}
	f := &paFake{currents: crCounting(cnt), underway: &planUnderway{
		Lat: a.Lat, Lon: a.Lon, Start: paAt(14, 3, 0),
		FixLat: (a.Lat + b.Lat) / 2, FixLon: (a.Lon + b.Lon) / 2, FixAt: paAt(14, 4, 20),
	}}
	paAssess(t, p, legs, f)
	if cnt.n == 0 || cnt.minT.Before(paAt(14, 4, 20)) {
		t.Fatalf("%d lookups from %s: the run from the fix reads the current from the fix on", cnt.n, cnt.minT)
	}
}

// Review 7: a departure before now is run with the field like the rest, so the window
// state comes from the same arrivals as the suggestion.
func TestAssessPlan_PastDeparturesAreRunWithTheCurrent(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 0)
	cnt := &crCount{}
	f := &paFake{currents: crCounting(cnt)}
	d := paDeps(f)
	d.now = func() time.Time { return paAt(14, 13, 10) }
	out := assessPlan(context.Background(), p, legs, d, planAssessOptions{})
	if out.Legs[0].Depart == "" {
		t.Fatalf("leg = %+v", out.Legs[0])
	}
	if cnt.n == 0 || cnt.minT.After(paAt(14, 6, 0)) {
		t.Fatalf("%d lookups from %s: the grid's past departures (first light on) are run with the current too", cnt.n, cnt.minT)
	}
}
