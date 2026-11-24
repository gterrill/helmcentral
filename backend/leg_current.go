package main

import (
	"context"
	"fmt"
	"math"
	"time"
)

// A leg's run is the leg walked from one departure with the modelled current beside
// it. In this release the current is read and reported but not yet added to the
// speed, so every run has the pace's own timeline; the figures are what the boat
// would meet along its track at the times it is there.

// legCurrentField is the modelled current a leg's steps will meet.
type legCurrentField interface {
	// alongAt is the current along the track, in knots, a boat entering step k at
	// t meets: positive helps it. It reports false when the model has none there,
	// which is a gap in the data and never slack.
	alongAt(k int, t time.Time) (kn float64, ok bool)
	// none is why the model gave no current anywhere on the leg, "" when it gave
	// some.
	none() string
}

// legRun is a leg run from one departure: its timeline and the current it meets.
type legRun struct {
	tl legTimeline
	// alongKn, modelledNm and unmodelledNm are the walk's (legWalk).
	alongKn                  float64
	modelledNm, unmodelledNm float64
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

// run is the leg from a departure at depart. The field may be nil, for a run made
// only for its timeline. A leg with no slow water keeps the exact minutes of its
// steady pace.
func (p *legPaceProfile) run(depart time.Time, f legCurrentField) (legRun, error) {
	if p.steady && f == nil {
		tl, err := p.pace(depart)
		return legRun{tl: tl}, err
	}
	w := p.integrate(depart, 0, 1, f)
	tl, err := p.timelineOf(depart, w)
	if err != nil {
		return legRun{}, err
	}
	return legRun{tl: tl, alongKn: w.alongKn, modelledNm: w.modelledNm, unmodelledNm: w.unmodelledNm}, nil
}

// timelineOf is the timeline of a walk from the start of the leg.
func (p *legPaceProfile) timelineOf(depart time.Time, w legWalk) (legTimeline, error) {
	if p.steady {
		return p.pace(depart)
	}
	return newLegTimeline(w.knots)
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
	w := p.integrate(at, from, left, f)
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
	return legRun{tl: tl, alongKn: w.alongKn, modelledNm: w.modelledNm, unmodelledNm: w.unmodelledNm}, nil
}

// legCellField is the modelled current on a leg, by step: the series of the model's
// cell each open-water step is in, read along the step's course.
type legCellField struct {
	p *legPaceProfile
	// series is the current at each of the profile's cells, nil for a cell with none.
	series []tidalCurrents
	why    string
}

func (f legCellField) alongAt(k int, t time.Time) (float64, bool) {
	i := f.p.cellIdx[k]
	if i < 0 {
		return 0, false
	}
	e, n, ok := f.series[i].at(t)
	if !ok {
		return 0, false
	}
	return alongKnots(e, n, f.p.steps[k].course), true
}

func (f legCellField) none() string { return f.why }

// readLegCurrents reads the modelled current for a leg in one request: the first
// open-water step's position in each 1/12 degree cell, in route order, for the
// longest the model forecasts. What is asked depends on the leg alone, so the page,
// a slip and Mate's plan read share one cached answer however the window falls.
// A leg over the cells one request carries, and a request that fails, are errors.
func (d assistantToolDeps) readLegCurrents(ctx context.Context, p *legPaceProfile) (legCurrentField, error) {
	field := legCellField{p: p}
	if len(p.cells) == 0 {
		field.why = "the whole leg is in slow water, where the model has no current"
		return field, nil
	}
	cur, err := d.fetchCellCurrents(ctx, p.cells, p.query, assistantTidalMaxDays)
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
