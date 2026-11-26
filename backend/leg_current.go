package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// A leg's run is the leg walked from one departure with the modelled current in its
// speed: each open-water step runs at the boat's speed there plus the current along
// the track at the moment the boat enters it. The figures beside the timeline are what
// the boat meets along its track at the times it is there.

// legCurrentStallKn is the least speed over the ground the boat is taken to hold.
// Below it a current against the boat has all but stopped it, the time a step takes
// runs away from any figure that means something, and the run is not an ETA: it
// fails. Half a knot is a boat that covers a step of 0.15 nm in 18 minutes.
const legCurrentStallKn = 0.5

// errCurrentAgainst is a run the current against the boat stops, said as the
// operator reads it: the words follow the threshold.
var errCurrentAgainst = fmt.Errorf("the current against the boat leaves it under %.1f kn part of the way", legCurrentStallKn)

// legCurrentField is the modelled current a leg's steps will meet.
type legCurrentField interface {
	// alongAt is the current along the track of p's step k, in knots, a boat entering
	// it at t meets: positive helps it. It reports false when the model has none
	// there, which is a gap in the data and never slack. The profile is the caller's:
	// a field shared by legs that ask for one request reads each one's own courses.
	alongAt(p *legPaceProfile, k int, t time.Time) (kn float64, ok bool)
	// none is why the model gave no current anywhere on the leg, "" when it gave
	// some.
	none() string
}

// runFate is what became of a run. fated is the one place that decides it, except for
// fateUnreadable, which a leg whose current could not be read gives its run before it
// is walked, and fateNotRun, which departureRun.outcome reads from a run that failed
// for any other reason.
type runFate int

const (
	fateOK runFate = iota
	// fateStalled is a run the current against the boat stops (errCurrentAgainst).
	fateStalled
	// fateOvernight is a run that takes more than planAssessMaxLegHours: it has its
	// timeline, and the leg is not judged.
	fateOvernight
	// fateUnreadable is a leg whose current request failed: the current is in the
	// speed, so there is no run.
	fateUnreadable
	// fateNotRun is a run that could not be made, its timeline not worked out.
	fateNotRun
)

// legRun is a leg run from one departure: its timeline and the current it meets.
type legRun struct {
	tl   legTimeline
	fate runFate
	// alongKn, modelledNm, unmodelledNm and slowHours are the walk's (legWalk).
	alongKn                  float64
	modelledNm, unmodelledNm float64
	slowHours                float64
}

// fated settles a run's fate: a run the current stalls (err is errCurrentAgainst) is
// stalled, and a run that takes more than maxHours (0 for no limit) is overnight.
func fated(r legRun, err error, maxHours float64) (legRun, error) {
	switch {
	case errors.Is(err, errCurrentAgainst):
		r.fate = fateStalled
	case err == nil && maxHours > 0 && r.tl.hours() > maxHours:
		r.fate = fateOvernight
	}
	return r, err
}

// openNm is the open water the run covers.
func (r legRun) openNm() float64 { return r.modelledNm + r.unmodelledNm }

// covered is the share of the open water the model has a current for, 0 for a run
// with no open water.
func (r legRun) covered() float64 {
	if r.openNm() <= 0 {
		return 0
	}
	return r.modelledNm / r.openNm()
}

// figure is the current the row shows, a tenth of a knot. The figure and the words
// that go with it are one number. Adding 0 turns a rounded -0.04 into 0, not -0.
func (r legRun) figure() float64 { return roundTo1(r.alongKn) + 0 }

// legBasisMinUnmodelledNm is the least open water the model has no current for that
// the basis names.
const legBasisMinUnmodelledNm = 0.5

// currentWords is what the run says of the current in a leg's ETA basis: the one
// figure the row shows, with or against the boat or none along the course, and the
// open water the model had none for, to whole miles. A clause with nothing to say is
// left out: a run with no open water has no current clause, a stretch of under half
// a mile is not named, and "" is a run with neither.
func (r legRun) currentWords() string {
	var parts []string
	if r.modelledNm > 0 {
		switch fig := r.figure(); {
		case fig > 0:
			parts = append(parts, fmt.Sprintf("current with you %.1f kn", fig))
		case fig < 0:
			parts = append(parts, fmt.Sprintf("current against you %.1f kn", -fig))
		default:
			parts = append(parts, "no current along the course")
		}
	}
	if r.unmodelledNm >= legBasisMinUnmodelledNm {
		parts = append(parts, fmt.Sprintf("current not modelled for %d nm", int(math.Round(r.unmodelledNm))))
	}
	return strings.Join(parts, ", ")
}

// noCurrentAtTheHours is why a run has no current when the model has a series for
// the leg and none that covers the time the boat is there.
const noCurrentAtTheHours = "the Open-Meteo model has no current at the hours of the run"

// noCurrentWhy is why the model gave no current for a run.
func noCurrentWhy(f legCurrentField) string {
	if why := f.none(); why != "" {
		return why
	}
	return noCurrentAtTheHours
}

// run is the leg from a departure at depart, with its fate. The field may be nil, for
// a run made only for its timeline.
func (p *legPaceProfile) run(depart time.Time, f legCurrentField) (legRun, error) {
	r, err := p.walkRun(depart, f)
	return fated(r, err, planAssessMaxLegHours)
}

// walkRun is run before its fate.
func (p *legPaceProfile) walkRun(depart time.Time, f legCurrentField) (legRun, error) {
	if p.steady && f == nil {
		tl, err := p.pace(depart) // a steady leg has no slow water
		return legRun{tl: tl}, err
	}
	w, err := p.integrate(depart, 0, 1, f)
	if err != nil {
		return legRun{}, err
	}
	var tl legTimeline
	if p.keepsPaceMinutes(w) {
		tl, err = p.pace(depart)
	} else {
		tl, err = newLegTimeline(w.knots)
	}
	if err != nil {
		return legRun{}, err
	}
	return w.run(tl), nil
}

// run is the walk with the timeline it was made into.
func (w legWalk) run(tl legTimeline) legRun {
	return legRun{tl: tl, alongKn: w.alongKn, modelledNm: w.modelledNm, unmodelledNm: w.unmodelledNm, slowHours: w.slowHours}
}

// keepsPaceMinutes says a walk had no current in its speed anywhere on a leg with no
// slow water: it is then the leg's steady pace, and takes that pace's exact minutes
// (linearPace) rather than the sum of the steps, so a field with no current for the
// leg changes no arrival from version 4.0's.
func (p *legPaceProfile) keepsPaceMinutes(w legWalk) bool {
	return p.steady && w.modelledNm == 0
}

// legFixMaxAge is how old the latest fix may be for the leg under way to be timed from
// it: an older one says where the boat was, not where it is.
const legFixMaxAge = 15 * time.Minute

// legPlaceNearNm is how much farther than the nearest step another may be and still
// be a candidate for a fix, when the route passes near itself.
const legPlaceNearNm = 0.1

// legFixOffRouteNm is how far from the route the latest fix may be for the leg under
// way to be timed from it: a boat farther off than this is not on the leg's route.
const legFixOffRouteNm = 1.0

// fixPlace is where a fix falls on the leg: the step it is on, the share of that step
// still ahead of it, and how far it is from the route.
type fixPlace struct {
	step        int
	left, offNm float64
}

// place says where a position falls on the leg: the foot of the perpendicular from it
// on the route's segments (the steps), the nearest one. A route that passes near
// itself (out and back) has segments within legPlaceNearNm of the nearest; among those
// it takes the one the boat would reach in still water closest to since, the time from
// the start of the passage to the fix. Only a segment the perpendicular lands on is
// one of them, where there is one, so the segments either side of the foot on a
// straight route do not pull the boat along toward the schedule.
func (p *legPaceProfile) place(lat, lon float64, since time.Duration) fixPlace {
	type foot struct {
		t, offNm float64
		on       bool
	}
	const edge = 1e-9
	feet := make([]foot, len(p.steps))
	nearest := math.Inf(1)
	for k, s := range p.steps {
		t, raw, m := segmentFoot(lat, lon, s.fromLat, s.fromLon, s.toLat, s.toLon)
		feet[k] = foot{t: t, offNm: m / metersPerNauticalMile, on: raw >= -edge && raw <= 1+edge}
		nearest = min(nearest, feet[k].offNm)
	}
	near := func(k int) bool { return feet[k].offNm <= nearest+legPlaceNearNm }
	anyOn := false
	for k := range p.steps {
		anyOn = anyOn || (near(k) && feet[k].on)
	}
	best, bestGap := 0, time.Duration(math.MaxInt64)
	var hours float64
	for k, s := range p.steps {
		reach := hoursDuration(hours + feet[k].t*s.nm/s.kn)
		hours += s.nm / s.kn
		if !near(k) || (anyOn && !feet[k].on) {
			continue
		}
		if gap := (reach - since).Abs(); gap < bestGap {
			best, bestGap = k, gap
		}
	}
	return fixPlace{step: best, left: 1 - feet[best].t, offNm: nearest}
}

// runFrom is the leg under way, from a fix at step from, taken at at, the passage
// having begun at start, with the share left of that step still to go. The timeline
// runs from the start of the passage to the fix, at the share of the leg behind it,
// and on from there at the leg's pace.
func (p *legPaceProfile) runFrom(start, at time.Time, from int, left float64, f legCurrentField) (legRun, error) {
	// A leg under way has no day's limit: its timeline runs from the passage's start.
	r, err := p.walkRunFrom(start, at, from, left, f)
	return fated(r, err, 0)
}

// walkRunFrom is runFrom before its fate.
func (p *legPaceProfile) walkRunFrom(start, at time.Time, from int, left float64, f legCurrentField) (legRun, error) {
	w, err := p.integrate(at, from, left, f)
	if err != nil {
		return legRun{}, err
	}
	knots := w.knots
	if knots[0].frac >= 1-legPaceKnotEps {
		// The fix is at the end of the leg: the boat is there at the time of the fix.
		if !start.Before(at) {
			return legRun{}, fmt.Errorf("the latest fix (%s) is not after the passage began (%s)", at.Format(time.RFC3339), start.Format(time.RFC3339))
		}
		knots = []legKnot{{t: start, frac: 0}, {t: at, frac: 1}}
	} else if behind := knots[0].frac; behind > legPaceKnotEps {
		if !start.Before(at) {
			return legRun{}, fmt.Errorf("the latest fix (%s) is not after the passage began (%s)", at.Format(time.RFC3339), start.Format(time.RFC3339))
		}
		knots = append([]legKnot{{t: start, frac: 0}}, knots...)
	} else {
		// The fix is at the route's start: the boat began there at the start of the
		// passage, so the row's hours run from then.
		knots[0].frac = 0
		if start.Before(at) {
			knots[0].t = start
		}
	}
	tl, err := newLegTimeline(knots)
	if err != nil {
		return legRun{}, err
	}
	return w.run(tl), nil
}

// legCellField is the modelled current on a leg, by step: the series of the model's
// cell each open-water step is in, read along the step's course. It holds the series
// alone, in the order of the cells one request asks for, so every profile with that
// request reads it along its own steps.
type legCellField struct {
	// series is the current at each cell of the request, nil for a cell with none.
	series []tidalCurrents
	why    string
}

func (f legCellField) alongAt(p *legPaceProfile, k int, t time.Time) (float64, bool) {
	i := p.cellIdx[k]
	if i < 0 {
		return 0, false
	}
	e, n, ok := f.series[i].at(t)
	if !ok {
		return 0, false
	}
	return alongKnots(e, n, p.steps[k].course), true
}

func (f legCellField) none() string { return f.why }

// readLegCurrents reads the modelled current for a leg in one request: the first
// open-water step's position in each 1/12 degree cell, in route order, for the
// longest the model forecasts. What is asked depends on the leg alone, so the page,
// a slip and Mate's plan read share one cached answer however the window falls.
// A leg over the cells one request carries, and a request that fails, are errors.
func (d assistantToolDeps) readLegCurrents(ctx context.Context, p *legPaceProfile) (legCurrentField, error) {
	field := legCellField{}
	if len(p.cells) == 0 {
		field.why = "the whole leg is in slow water, where the model has no current"
		return field, nil
	}
	cur, err := d.fetchCellCurrents(ctx, p.cells, p.query, false)
	if err != nil {
		return nil, err
	}
	field.series = make([]tidalCurrents, len(p.cells))
	some := false
	for i, c := range p.cells {
		if series := cur.series[c]; len(series) > 0 {
			field.series[i] = series
			some = true
		}
	}
	if !some {
		samples := make([]tidalSample, len(p.cells))
		for i, c := range p.cells {
			samples[i].cell = c
		}
		field.why = noModelDataReason(cur, samples)
	}
	return field, nil
}
