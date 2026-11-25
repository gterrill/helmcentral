package main

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

var ltDepart = time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)

// spanTimeline is a leg that runs from depart to arrive at a steady pace.
func spanTimeline(depart, arrive time.Time) (legTimeline, error) {
	return newLegTimeline([]legKnot{{t: depart, frac: 0}, {t: arrive, frac: 1}})
}

func ltClose(t *testing.T, what string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func TestLinearTimeline_HoldsTwoKnotsFromDepartToArrival(t *testing.T) {
	tl, err := linearTimeline(ltDepart, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(tl.knots) != 2 {
		t.Fatalf("knots = %d, want 2", len(tl.knots))
	}
	if !tl.depart().Equal(ltDepart) || !tl.arrive().Equal(ltDepart.Add(5*time.Hour)) {
		t.Errorf("depart %v arrive %v", tl.depart(), tl.arrive())
	}
	if tl.duration() != 5*time.Hour {
		t.Errorf("duration = %v", tl.duration())
	}
	ltClose(t, "hours", tl.hours(), 5)
	ltClose(t, "frac a fifth in", tl.fracAt(ltDepart.Add(time.Hour)), 0.2)
	ltClose(t, "frac half way", tl.fracAt(ltDepart.Add(150*time.Minute)), 0.5)
	if got := tl.timeAt(0.4); !got.Equal(ltDepart.Add(2 * time.Hour)) {
		t.Errorf("timeAt(0.4) = %v", got)
	}
}

func TestLegTimeline_ClampsBeforeDepartAndAfterArrival(t *testing.T) {
	tl, _ := linearTimeline(ltDepart, 4)
	if got := tl.fracAt(ltDepart.Add(-3 * time.Hour)); got != 0 {
		t.Errorf("before depart = %v, want 0", got)
	}
	if got := tl.fracAt(ltDepart.Add(9 * time.Hour)); got != 1 {
		t.Errorf("after arrival = %v, want 1", got)
	}
	if got := tl.timeAt(-0.5); !got.Equal(tl.depart()) {
		t.Errorf("timeAt(-0.5) = %v, want the departure", got)
	}
	if got := tl.timeAt(1.5); !got.Equal(tl.arrive()) {
		t.Errorf("timeAt(1.5) = %v, want the arrival", got)
	}
}

func TestLegTimeline_ThreeKnotsRoundTrip(t *testing.T) {
	// Half the route in the first hour (a fair stream), the rest over four.
	tl, err := newLegTimeline([]legKnot{
		{t: ltDepart, frac: 0},
		{t: ltDepart.Add(time.Hour), frac: 0.5},
		{t: ltDepart.Add(5 * time.Hour), frac: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	ltClose(t, "frac at 30 min", tl.fracAt(ltDepart.Add(30*time.Minute)), 0.25)
	ltClose(t, "frac at 1 h", tl.fracAt(ltDepart.Add(time.Hour)), 0.5)
	ltClose(t, "frac at 3 h", tl.fracAt(ltDepart.Add(3*time.Hour)), 0.75)
	for _, f := range []float64{0, 0.1, 0.25, 0.5, 0.6, 0.75, 0.99, 1} {
		got := tl.fracAt(tl.timeAt(f))
		if math.Abs(got-f) > 1e-9 {
			t.Errorf("fracAt(timeAt(%v)) = %v", f, got)
		}
	}
	for _, m := range []int{0, 7, 60, 90, 200, 300} {
		at := ltDepart.Add(time.Duration(m) * time.Minute)
		if got := tl.timeAt(tl.fracAt(at)); got.Sub(at).Abs() > time.Microsecond {
			t.Errorf("timeAt(fracAt(+%d min)) = %v, want %v", m, got, at)
		}
	}
	if tl.duration() != 5*time.Hour {
		t.Errorf("duration = %v", tl.duration())
	}
}

func TestLinearTimeline_RejectsHoursThatAreNotANumberOfZeroOrMore(t *testing.T) {
	for _, h := range []float64{-2, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := linearTimeline(ltDepart, h); err == nil {
			t.Errorf("hours %v accepted", h)
		}
	}
}

// A leg of no length arrives when it departs, and is nowhere along its route at
// any time, as a leg of no hours always was.
func TestLinearTimeline_ZeroHoursArrivesWhenItDeparts(t *testing.T) {
	tl, err := linearTimeline(ltDepart, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !tl.arrive().Equal(tl.depart()) || tl.duration() != 0 {
		t.Fatalf("depart %v arrive %v", tl.depart(), tl.arrive())
	}
	for _, d := range []time.Duration{-time.Hour, 0, time.Hour} {
		if got := tl.fracAt(ltDepart.Add(d)); got != 0 {
			t.Errorf("fracAt(%v) = %v, want 0", d, got)
		}
	}
	for _, f := range []float64{-1, 0, 0.5, 1, 2, math.NaN()} {
		if got := tl.timeAt(f); !got.Equal(ltDepart) {
			t.Errorf("timeAt(%v) = %v, want the departure", f, got)
		}
	}
}

func TestLegTimeline_RoundingCannotCollapseKnotsIntoOneTime(t *testing.T) {
	tl, err := newLegTimeline([]legKnot{
		{t: ltDepart, frac: 0},
		{t: ltDepart.Add(10 * time.Second), frac: 0.5},
		{t: ltDepart.Add(5 * time.Minute), frac: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tl.roundedTo(time.Minute); err == nil {
		t.Error("two knots rounded to one time and were accepted")
	}
	// A leg of one steady pace shorter than half a step has two knots, and rounds
	// to a leg of no length.
	short, _ := linearTimeline(ltDepart, 20.0/3600)
	got, err := short.roundedTo(time.Minute)
	if err != nil || got.duration() != 0 {
		t.Fatalf("rounded sub-30 s leg = %v, %v", got, err)
	}
}

func TestNewLegTimeline_RejectsKnotsThatDoNotRunDepartToArrival(t *testing.T) {
	at := func(m int) time.Time { return ltDepart.Add(time.Duration(m) * time.Minute) }
	cases := map[string][]legKnot{
		"none":                       nil,
		"one":                        {{t: at(0), frac: 0}},
		"starts part way":            {{t: at(0), frac: 0.1}, {t: at(60), frac: 1}},
		"ends short":                 {{t: at(0), frac: 0}, {t: at(60), frac: 0.9}},
		"time stands still part way": {{t: at(0), frac: 0}, {t: at(0), frac: 0.5}, {t: at(60), frac: 1}},
		"time runs back":             {{t: at(60), frac: 0}, {t: at(0), frac: 1}},
		"goes backwards":             {{t: at(0), frac: 0}, {t: at(30), frac: 0.6}, {t: at(60), frac: 0.4}, {t: at(90), frac: 1}},
		"not a number":               {{t: at(0), frac: 0}, {t: at(30), frac: math.NaN()}, {t: at(60), frac: 1}},
	}
	for name, knots := range cases {
		if _, err := newLegTimeline(knots); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestSpanTimeline_RunsBetweenTwoInstantsAndRefusesAnArrivalBeforeTheDeparture(t *testing.T) {
	tl, err := spanTimeline(ltDepart, ltDepart.Add(90*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	ltClose(t, "frac", tl.fracAt(ltDepart.Add(45*time.Minute)), 0.5)
	if _, err := spanTimeline(ltDepart, ltDepart.Add(-time.Minute)); err == nil {
		t.Error("an arrival before the departure was accepted")
	}
}

func TestRoundedTimeline_KeepsWholeMinutes(t *testing.T) {
	tl, _ := linearTimeline(ltDepart, 6.2)
	got, err := tl.roundedTo(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if got.duration() != 372*time.Minute {
		t.Errorf("duration = %v, want 372m", got.duration())
	}
}

// Main ran a ranked departure for math.Round(hours*60) minutes, and showed arrivals
// at hoursDuration(hours). Both hold for every hours value to the millihour.
func TestLinearTimeline_RoundsToMainsWholeMinutesAndArrivesAtHoursDuration(t *testing.T) {
	bad := 0
	for i := 1; i <= 48000; i++ {
		h := float64(i) / 1000
		tl, err := linearTimeline(ltDepart, h)
		if err != nil {
			t.Fatal(err)
		}
		if want := ltDepart.Add(hoursDuration(h)); !tl.arrive().Equal(want) {
			t.Fatalf("hours %v arrive %v, want %v", h, tl.arrive(), want)
		}
		got, err := tl.roundedTo(time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if want := time.Duration(math.Round(h*60)) * time.Minute; got.duration() != want {
			bad++
			t.Errorf("hours %v rounds to %v, want %v", h, got.duration(), want)
		}
	}
	if bad > 0 {
		t.Logf("%d mismatches", bad)
	}
}

func TestLinearPace_GivesTheSameHoursForAnyDeparture(t *testing.T) {
	pace := linearPace(3)
	for _, d := range []time.Time{ltDepart, ltDepart.Add(7 * time.Hour)} {
		tl, err := pace(d)
		if err != nil || tl.duration() != 3*time.Hour || !tl.depart().Equal(d) {
			t.Fatalf("pace(%v) = %v, %v", d, tl, err)
		}
	}
	if _, err := linearPace(-1)(ltDepart); err == nil {
		t.Error("negative hours gave a timeline")
	}
}

func TestPassageLegPlanTimeline_RefusesALegWithNoPace(t *testing.T) {
	if _, err := (passageLegPlan{}).timeline(ltDepart); err == nil {
		t.Error("a plan with no pace gave a timeline")
	}
}

func BenchmarkLegTimeline(b *testing.B) {
	knots := make([]legKnot, 400)
	for i := range knots {
		knots[i] = legKnot{t: ltDepart.Add(time.Duration(i) * 3 * time.Minute), frac: float64(i) / 399}
	}
	tl, err := newLegTimeline(knots)
	if err != nil {
		b.Fatal(err)
	}
	span := tl.duration()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tl.fracAt(ltDepart.Add(time.Duration(i%1000) * span / 1000))
	}
}

func TestLegTimeline_TimeAtStepIsExactOnAnEvenStep(t *testing.T) {
	tl, err := linearTimeline(ltDepart, 3)
	if err != nil {
		t.Fatal(err)
	}
	for k := 0; k <= 12; k++ {
		if got, want := tl.timeAtStep(k, 12), ltDepart.Add(time.Duration(k)*15*time.Minute); !got.Equal(want) {
			t.Errorf("timeAtStep(%d, 12) = %v, want %v", k, got, want)
		}
	}
	if got := tl.timeAtStep(0, 0); !got.Equal(ltDepart) {
		t.Errorf("timeAtStep(0, 0) = %v, want the departure", got)
	}
}

// On a steady pace every sample time is the one main computed: the duration times
// k, then over n, to the nanosecond.
func TestLegTimeline_TimeAtStepMatchesMainToTheNanosecond(t *testing.T) {
	for mins := 1; mins <= 600; mins += 7 {
		d := time.Duration(mins) * time.Minute
		tl, err := spanTimeline(ltDepart, ltDepart.Add(d))
		if err != nil {
			t.Fatal(err)
		}
		for n := 1; n <= 40; n++ {
			for k := 0; k <= n; k++ {
				want := ltDepart.Add(time.Duration(float64(d) * float64(k) / float64(n)))
				if got := tl.timeAtStep(k, n); !got.Equal(want) {
					t.Fatalf("%v, step %d of %d: %v, want %v", d, k, n, got.Sub(ltDepart), want.Sub(ltDepart))
				}
			}
		}
	}
}

func TestLegTimeline_TimeAtStepFollowsAKnotInTheMiddle(t *testing.T) {
	tl, err := newLegTimeline([]legKnot{{t: ltDepart, frac: 0}, {t: ltDepart.Add(2 * time.Hour), frac: 0.5}, {t: ltDepart.Add(3 * time.Hour), frac: 1}})
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[int]time.Duration{0: 0, 2: time.Hour, 4: 2 * time.Hour, 6: 150 * time.Minute, 8: 3 * time.Hour} {
		if got := tl.timeAtStep(k, 8); !got.Equal(ltDepart.Add(want)) {
			t.Errorf("timeAtStep(%d, 8) = %v, want +%v", k, got, want)
		}
	}
}

// A window whose latest is more than a step before its earliest has no
// departures, as on main, and is not a panic.
func TestRankTidalDepartures_InvertedWindowHasNothingToRank(t *testing.T) {
	q := tidalRankQuery{
		Lat: -27, Lon: 153, CourseDeg: 90, Hours: 2, Pace: linearPace(2),
		Earliest: ltDepart, Latest: ltDepart.Add(-time.Hour), Now: ltDepart,
	}
	_, err := assistantToolDeps{now: func() time.Time { return ltDepart }}.rankTidalDepartures(context.Background(), q)
	var nothing errTidalNothingToRank
	if !errors.As(err, &nothing) {
		t.Fatalf("err = %v, want errTidalNothingToRank", err)
	}
}
