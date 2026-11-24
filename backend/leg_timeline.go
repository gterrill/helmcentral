package main

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// legKnot says how far along the route a leg is at an instant: frac is the
// share of the route's distance covered, 0 at the departure and 1 at the arrival.
type legKnot struct {
	t    time.Time
	frac float64
}

// legTimeline is a leg's progress from one departure: where along the route the
// boat is at any time, and when it is at any place. Progress runs piecewise
// linearly between its knots, so a leg that holds one speed has two knots, and one
// that slows in a cell, or meets a stream that changes by the hour, has more.
//
// This is the one place that turns time into a position along a leg and back.
// Nothing else adds a duration to a departure or divides a span to find a
// fraction of the way.
type legTimeline struct {
	knots []legKnot
	// steadyHours is the hours a leg of one steady pace was built from, and
	// steady says it was. Rounding to the minute works from the hours, as the
	// ranking always has, because the nanosecond duration can sit just under a
	// half minute. Any other change to the knots drops both.
	steadyHours float64
	steady      bool
}

// legPace works out a leg's timeline for a departure. Each departure has a
// timeline of its own, because the speed over the ground can depend on when the
// boat leaves. It is the only way a leg's pace is given: a leg has no hours of its
// own beside it.
type legPace func(depart time.Time) (legTimeline, error)

// newLegTimeline checks knots that run from the departure (frac 0) to the arrival
// (frac 1), each later in time and further along the route than the one before.
// The one exception is a leg of no time under way, whose two knots share a time.
// The timeline takes the slice over, so the caller hands it one it will not use again.
func newLegTimeline(knots []legKnot) (legTimeline, error) {
	if len(knots) < 2 {
		return legTimeline{}, fmt.Errorf("a leg's timeline needs a departure and an arrival, and has %d knots", len(knots))
	}
	if knots[0].frac != 0 || knots[len(knots)-1].frac != 1 {
		return legTimeline{}, fmt.Errorf("a leg's timeline must run from 0 to 1 of the route, not %v to %v", knots[0].frac, knots[len(knots)-1].frac)
	}
	for i := 1; i < len(knots); i++ {
		a, b := knots[i-1], knots[i]
		if b.t.Before(a.t) || (b.t.Equal(a.t) && len(knots) > 2) {
			return legTimeline{}, fmt.Errorf("a leg's timeline must move forward in time, and knot %d is not after knot %d", i, i-1)
		}
		if !(b.frac > a.frac) { // false for NaN too
			return legTimeline{}, fmt.Errorf("a leg's timeline must move forward along the route, and knot %d is not past knot %d", i, i-1)
		}
	}
	return legTimeline{knots: knots}, nil
}

// linearTimeline is a leg at one steady pace: it arrives hours after it departs.
// Zero hours is a leg of no length, which arrives when it departs. Hours that are
// not a number of zero or more are an error, never a default.
func linearTimeline(depart time.Time, hours float64) (legTimeline, error) {
	if !(hours >= 0) || math.IsInf(hours, 0) {
		return legTimeline{}, fmt.Errorf("a leg's time under way must be a number of hours, zero or more, not %v", hours)
	}
	tl, err := newLegTimeline([]legKnot{{t: depart, frac: 0}, {t: depart.Add(hoursDuration(hours)), frac: 1}})
	tl.steadyHours, tl.steady = hours, true
	return tl, err
}

// linearPace is the pace of a leg that holds one speed whenever it leaves.
func linearPace(hours float64) legPace {
	return func(depart time.Time) (legTimeline, error) { return linearTimeline(depart, hours) }
}

// known is false for the zero timeline, a leg that has none (no departure, or a
// ranking that carried none).
func (tl legTimeline) known() bool { return len(tl.knots) > 0 }

func (tl legTimeline) depart() time.Time { return tl.knots[0].t }
func (tl legTimeline) arrive() time.Time { return tl.knots[len(tl.knots)-1].t }

// duration is the time under way.
func (tl legTimeline) duration() time.Duration { return tl.arrive().Sub(tl.depart()) }

// hours is the time under way in hours.
func (tl legTimeline) hours() float64 { return tl.duration().Hours() }

// fracAt is the share of the route covered at t: 0 before the departure and 1
// after the arrival. A leg of no time under way is at the start of its route at
// every time.
func (tl legTimeline) fracAt(t time.Time) float64 {
	n := len(tl.knots)
	if tl.duration() == 0 || !t.After(tl.knots[0].t) {
		return 0
	}
	if !t.Before(tl.knots[n-1].t) {
		return 1
	}
	// The first knot after t ends the segment t falls in.
	i := sort.Search(n, func(i int) bool { return tl.knots[i].t.After(t) })
	a, b := tl.knots[i-1], tl.knots[i]
	return a.frac + (b.frac-a.frac)*(float64(t.Sub(a.t))/float64(b.t.Sub(a.t)))
}

// timeAt is when the boat has covered frac of the route: the departure for 0 or
// less, the arrival for 1 or more.
func (tl legTimeline) timeAt(frac float64) time.Time {
	n := len(tl.knots)
	if !(frac > 0) {
		return tl.knots[0].t
	}
	if !(frac < 1) {
		return tl.knots[n-1].t
	}
	i := sort.Search(n, func(i int) bool { return tl.knots[i].frac > frac })
	a, b := tl.knots[i-1], tl.knots[i]
	u := (frac - a.frac) / (b.frac - a.frac)
	return a.t.Add(time.Duration(float64(b.t.Sub(a.t)) * u))
}

// timeAtStep is when the boat has covered k/n of the route, for the samples that
// lie k steps along n. It multiplies by k before it divides by n, so on a steady
// pace the time is exact to the nanosecond where timeAt's fraction can be one off.
// A run of no steps (n of 0) is at its departure.
func (tl legTimeline) timeAtStep(k, n int) time.Time {
	last := len(tl.knots) - 1
	if n <= 0 || k <= 0 {
		return tl.knots[0].t
	}
	if k >= n {
		return tl.knots[last].t
	}
	nf, kf := float64(n), float64(k)
	i := sort.Search(len(tl.knots), func(i int) bool { return tl.knots[i].frac*nf > kf })
	a, b := tl.knots[i-1], tl.knots[i]
	return a.t.Add(time.Duration(float64(b.t.Sub(a.t)) * (kf - a.frac*nf) / ((b.frac - a.frac) * nf)))
}

// mapTimes is the same progress with every knot's time changed by f. The result is
// checked like any timeline. It rejects a knot that falls before the one ahead of it,
// and more than two knots at one time. Only a timeline of two knots may collapse to
// no length, a leg of no time under way.
func (tl legTimeline) mapTimes(f func(time.Time) time.Time) (legTimeline, error) {
	knots := make([]legKnot, len(tl.knots))
	for i, k := range tl.knots {
		knots[i] = legKnot{t: f(k.t), frac: k.frac}
	}
	return newLegTimeline(knots)
}

// roundedTo is the same progress with every knot's time since the departure
// rounded to a multiple of step, which keeps a run's arithmetic exact. A leg of
// one steady pace shorter than half a step rounds to a leg of no length.
func (tl legTimeline) roundedTo(step time.Duration) (legTimeline, error) {
	d := tl.depart()
	if tl.steady && step == time.Minute {
		whole := time.Duration(math.Round(tl.steadyHours*60)) * time.Minute
		return newLegTimeline([]legKnot{{t: d, frac: 0}, {t: d.Add(whole), frac: 1}})
	}
	return tl.mapTimes(func(t time.Time) time.Time { return d.Add(t.Sub(d).Round(step)) })
}

// truncatedTo is the same progress with every knot's time cut to a multiple of
// step, as a row's times are shown.
func (tl legTimeline) truncatedTo(step time.Duration) (legTimeline, error) {
	return tl.mapTimes(func(t time.Time) time.Time { return t.Truncate(step) })
}
