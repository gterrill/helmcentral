package main

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"
)

// A passage window cell's wind summary (ADR 0265). The limiting line names one
// hour, so a leg that is astern for five hours and ahead for the first
// half-hour out of a creek reads "Gusts 16 kn ahead", and a model reading only
// that called the whole leg dead to windward. The summary gives the model the
// wind's direction and speed over the leg and how long the leg runs in each
// sector. It goes to the model only; the card keeps its limiting line.

// windSectorOrder is the order sectors are listed in when hours tie: the
// sectors of ADR 0191, then the hours too calm to have a direction.
var windSectorOrder = []string{"ahead", "abeam", "astern", "calm"}

// windMissing is the sector of an entry for an hour with no forecast.
const windMissing = "missing"

// minShownHours is the least an entry or a gap may hold to print as hours. Less
// reads as an end of the leg, so a few minutes' change in departure does not
// change the form.
const minShownHours = 0.05

// windEdges are the ends of the leg a stamp was judged at when too little of its
// hour lay inside the leg.
type windEdges struct{ dep, arr bool }

func (e windEdges) String() string {
	switch {
	case e.dep && e.arr:
		return "at departure and arrival"
	case e.arr:
		return "at arrival"
	}
	return "at departure"
}

func (e *windEdges) mark(atStart bool) {
	if atStart {
		e.dep = true
	} else {
		e.arr = true
	}
}

// windSpan is where a sector's hours lie in the leg, in hours from departure:
// the start of the first window counted and the end of the last.
type windSpan struct{ first, last float64 }

// windEntry is one forecast stamp the cell judged. lo and hi bound the part of
// its hour inside the leg, in hours from departure; the part is empty when hi
// does not exceed lo.
type windEntry struct {
	sector, seg string // ahead, abeam, astern, "calm", or windMissing
	lo, hi      float64
	atStart     bool // for an entry too small to count: nearer departure than arrival
	kn, dirDeg  float64
}

func (e windEntry) weight() float64 { return math.Max(0, e.hi-e.lo) }

// passageWindTally keeps one entry per stamp the cell loop judged, in order.
// The summary is worked out from them when asked for, so a rule about runs,
// ends or gaps reads the whole leg and not a running count.
type passageWindTally struct {
	entries  []windEntry
	legHours float64
	legSegs  int
}

func newPassageWindTally(legHours float64, legSegs int) *passageWindTally {
	return &passageWindTally{legHours: legHours, legSegs: legSegs}
}

// legWindow is the start and end of the part of the hour stamped t that lies
// inside the leg.
func legWindow(t, depart, arrive time.Time) (lo, hi time.Time) {
	lo, hi = t.Add(-30*time.Minute), t.Add(30*time.Minute)
	if lo.Before(depart) {
		lo = depart
	}
	if hi.After(arrive) {
		hi = arrive
	}
	return lo, hi
}

// entry builds the entry for the hour stamped t.
func (w *passageWindTally) entry(t, depart, arrive time.Time, sector, seg string) windEntry {
	lo, hi := legWindow(t, depart, arrive)
	e := windEntry{sector: sector, seg: seg, lo: lo.Sub(depart).Hours(), hi: hi.Sub(depart).Hours()}
	if e.weight() <= 0 {
		e.atStart = t.Before(depart)
	} else {
		e.atStart = (e.lo+e.hi)/2 < w.legHours/2
	}
	return e
}

// add records the hour stamped t as the cell judged it. Every stamp feeds the
// speed range, and the direction range unless the wind is under 1 kn. Those
// hours are "calm", not ahead, abeam or astern, since their direction means
// nothing; the cell's verdict still comes from the sector it judged.
func (w *passageWindTally) add(t time.Time, depart, arrive time.Time, dirDeg, windKn float64, sector, segLabel string) {
	if windKn < 1 {
		sector = "calm"
	}
	e := w.entry(t, depart, arrive, sector, segLabel)
	e.kn, e.dirDeg = windKn, dirDeg
	w.entries = append(w.entries, e)
}

// gap records the hour stamped t as having no forecast.
func (w *passageWindTally) gap(t time.Time, depart, arrive time.Time) {
	w.entries = append(w.entries, w.entry(t, depart, arrive, windMissing, ""))
}

// compassRange names the smallest arc of directions holding every one: the
// circle is cut at its widest empty gap. An arc of a half circle or more is
// "variable", since two points on it say nothing about the wind between. No
// direction at all (every hour calm) is "calm".
func compassRange(dirs []float64) string {
	if len(dirs) == 0 {
		return "calm"
	}
	var norm []float64
	for _, d := range dirs {
		norm = append(norm, normalizeDegrees(d))
	}
	sort.Float64s(norm)
	norm = slices.Compact(norm)
	if len(norm) == 1 {
		return degreesToDirection(norm[0])
	}
	start, widest := 0, -1.0
	for i := range norm {
		next := norm[(i+1)%len(norm)]
		gap := math.Mod(next-norm[i]+360, 360)
		if gap > widest {
			widest, start = gap, (i+1)%len(norm)
		}
	}
	if 360-widest >= 180 {
		return "variable"
	}
	from, to := degreesToDirection(norm[start]), degreesToDirection(norm[(start+len(norm)-1)%len(norm)])
	if from == to {
		return from
	}
	return from + "–" + to
}

// windFold is what the entries add up to.
type windFold struct {
	hours       map[string]float64
	span        map[string]windSpan
	stretches   map[string]int
	markers     map[string]windEdges
	segTotal    map[string]float64
	segHours    map[string]map[string]float64
	calmInside  map[string]bool
	missing     float64
	missingEnds windEdges
}

// fold reads the entries in order. An entry with under minShownHours of the leg
// inside it is an end marker: it never counts as hours or breaks a run. Calm
// entries don't break another sector's run. A gap (a row of missing entries)
// breaks every run, and is hours of "no forecast" only at minShownHours or more;
// a smaller one is an end marker.
func (w *passageWindTally) fold() windFold {
	f := windFold{
		hours: map[string]float64{}, span: map[string]windSpan{}, stretches: map[string]int{},
		markers: map[string]windEdges{}, segTotal: map[string]float64{},
		segHours: map[string]map[string]float64{}, calmInside: map[string]bool{},
	}
	lastSolid, lastCalm := "", false
	inGap, gapWeight, gapAtStart := false, 0.0, false
	closeGap := func() {
		if !inGap {
			return
		}
		inGap = false
		if gapWeight >= minShownHours {
			f.missing += gapWeight
			lastSolid, lastCalm = "", false
			return
		}
		f.missingEnds.mark(gapAtStart)
	}
	var calms []windEntry
	for _, e := range w.entries {
		if e.sector == windMissing {
			if !inGap {
				inGap, gapWeight, gapAtStart = true, 0, e.atStart
			}
			gapWeight += e.weight()
			continue
		}
		closeGap()
		weight := e.weight()
		if weight < minShownHours {
			m := f.markers[e.sector]
			m.mark(e.atStart)
			f.markers[e.sector] = m
			continue
		}
		f.hours[e.sector] += weight
		f.segTotal[e.seg] += weight
		if f.segHours[e.sector] == nil {
			f.segHours[e.sector] = map[string]float64{}
		}
		f.segHours[e.sector][e.seg] += weight
		sp, seen := f.span[e.sector]
		if !seen {
			sp.first = e.lo
		}
		sp.last = e.hi
		f.span[e.sector] = sp
		if e.sector == "calm" {
			calms = append(calms, e)
			if !lastCalm {
				f.stretches["calm"]++
			}
			lastCalm = true
			continue
		}
		lastCalm = false
		if lastSolid != e.sector {
			f.stretches[e.sector]++
		}
		lastSolid = e.sector
	}
	closeGap()
	for sec, sp := range f.span {
		for _, c := range calms {
			if sec != "calm" && c.lo >= sp.first && c.hi <= sp.last {
				f.calmInside[sec] = true
			}
		}
	}
	return f
}

// summary is empty when no hour of the leg had a forecast.
func (w *passageWindTally) summary(legName string) string {
	var dirs []float64
	minKn, maxKn, judged := math.MaxFloat64, 0.0, 0
	for _, e := range w.entries {
		if e.sector == windMissing {
			continue
		}
		judged++
		if e.kn >= 1 {
			dirs = append(dirs, e.dirDeg)
		}
		minKn, maxKn = math.Min(minKn, e.kn), math.Max(maxKn, e.kn)
	}
	if judged == 0 {
		return ""
	}
	f := w.fold()
	speed := fmt.Sprintf("%d", int(minKn))
	if maxKn != minKn {
		speed = fmt.Sprintf("%d–%d", int(minKn), int(maxKn))
	}
	// A sector with no hours of its own but a stamp at an end of the leg is
	// named by that end.
	var order, ends []string
	for _, sec := range windSectorOrder {
		switch {
		case f.hours[sec] > 0:
			order = append(order, sec)
		case f.markers[sec] != (windEdges{}):
			ends = append(ends, sec)
		}
	}
	sort.SliceStable(order, func(i, j int) bool { return f.hours[order[i]] > f.hours[order[j]] })
	parts := make([]string, 0, len(order)+len(ends)+1)
	for _, sec := range order {
		part := fmt.Sprintf("%s %.1f h", sec, f.hours[sec])
		// A sector under half the leg is a stretch of it, so say where.
		if f.hours[sec]*2 < w.legHours {
			part += " (" + w.stretch(f, sec, legName) + ")"
		}
		parts = append(parts, part)
	}
	for _, sec := range ends {
		parts = append(parts, sec+" "+f.markers[sec].String())
	}
	switch {
	case f.missing > 0:
		parts = append(parts, fmt.Sprintf("no forecast %.1f h", f.missing))
	case f.missingEnds != (windEdges{}):
		parts = append(parts, "no forecast "+f.missingEnds.String())
	}
	dir := compassRange(dirs)
	if dir != "calm" && dir != "variable" {
		dir = "from " + dir
	}
	return fmt.Sprintf("Wind %s %s kn: %s", dir, speed, strings.Join(parts, ", "))
}

// stretch says where a sector's hours fall. Hours in separate runs are said to
// be, since one range would be false. A run that fills one named segment of a
// leg with several is named by it. Otherwise it is placed by position in the
// leg, and a run with a calm hour inside it says it spans that hour.
func (w *passageWindTally) stretch(f windFold, sec, legName string) string {
	if f.stretches[sec] > 1 {
		return fmt.Sprintf("in %d stretches", f.stretches[sec])
	}
	if w.legSegs > 1 && len(f.segHours[sec]) == 1 {
		for label, h := range f.segHours[sec] {
			if label != legName && math.Abs(h-f.segTotal[label]) < 1e-9 {
				return label
			}
		}
	}
	sp := f.span[sec]
	within := ""
	if f.calmInside[sec] {
		within = "within "
	}
	switch {
	case sp.first < 1e-9 && within != "":
		return fmt.Sprintf("within the first %.1f h", sp.last)
	case sp.first < 1e-9:
		return fmt.Sprintf("first %.1f h", f.hours[sec])
	case w.legHours-sp.last < 1e-9 && within != "":
		return fmt.Sprintf("within the last %.1f h", w.legHours-sp.first)
	case w.legHours-sp.last < 1e-9:
		return fmt.Sprintf("last %.1f h", f.hours[sec])
	}
	return fmt.Sprintf("%s%.1f–%.1f h in", within, sp.first, sp.last)
}
