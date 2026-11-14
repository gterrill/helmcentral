package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"slices"
	"strconv"
	"time"
)

// Plan progress (ADR 0288): which legs the boat has done or passed by, read
// from the recorded passages. Nothing in the plan is rewritten.
//
// A leg is Done when a recorded passage ends within planArrivedNm of the leg's
// destination any time after the plan's first day and after the passage that
// reached the previous stop, so leaving early still counts. A passage that ends
// at a later stop than a leg's destination marks that stop, and the legs into
// and out of it that no passage ended at, Skipped: the boat went by.

const (
	planProgressDone    = "done"
	planProgressSkipped = "skipped"

	// planArrivedNm is how close a passage's end, or the boat now, must be to a
	// stop to count as being there.
	planArrivedNm = 1.5
)

// planPassageEnd is where and when a recorded passage ended.
type planPassageEnd struct {
	At       time.Time
	Lat, Lon float64
}

// withinArrivedNm says whether two places are close enough to count as the same
// stop.
func withinArrivedNm(aLat, aLon, bLat, bLon float64) bool {
	return haversineMeters(aLat, aLon, bLat, bLon)/metersPerNauticalMile <= planArrivedNm
}

// planProgressOf is each leg's progress: "" still to do, planProgressDone or
// planProgressSkipped.
func planProgressOf(p plan, ends []planPassageEnd) ([]string, error) {
	zones, bad, err := planZones(p)
	if err != nil {
		return nil, fmt.Errorf("stop %d (%s): %w", bad+1, p.Stops[bad].Name, err)
	}
	legs := max(len(p.Stops)-1, 0)
	// reached[j] says a passage ended at stop j. Passages are taken in time order,
	// each counting for the first stop after the last one reached that it ends
	// within planArrivedNm of, and only from the plan's first day on.
	reached := make([]bool, len(p.Stops))
	last := 0
	if len(p.Stops) > 0 {
		first, err := dayStart(p.Stops[0].Arrive, zones[0].Loc)
		if err != nil {
			return nil, fmt.Errorf("first stop arrival %q: %w", p.Stops[0].Arrive, err)
		}
		ordered := slices.Clone(ends)
		slices.SortStableFunc(ordered, func(a, b planPassageEnd) int { return a.At.Compare(b.At) })
		for _, e := range ordered {
			if e.At.Before(first) {
				continue
			}
			for j := last + 1; j < len(p.Stops); j++ {
				if withinArrivedNm(e.Lat, e.Lon, p.Stops[j].Lat, p.Stops[j].Lon) {
					reached[j] = true
					last = j
					break
				}
			}
		}
	}
	out := make([]string, legs)
	for i := range out {
		switch {
		case reached[i+1]:
			out[i] = planProgressDone
		case i+1 < last:
			out[i] = planProgressSkipped
		}
	}
	return out, nil
}

// readProgress asks for the plan's per-leg progress, one entry for every leg.
// A read that fails is said once on the assessment and marks nothing, so the
// legs are judged as still to come rather than guessed done.
func (d planAssessDeps) readProgress(ctx context.Context, p plan, out *planAssessment) []string {
	res := make([]string, max(len(p.Stops)-1, 0))
	if len(res) == 0 {
		return res
	}
	got, err := d.progress(ctx, p)
	if err != nil {
		log.Printf("plans: progress not read: %v", err)
		out.ProgressError = err.Error()
		return res
	}
	copy(res, got)
	return res
}

// planLegKey is how a leg is named across edits: its two stops and which trip
// between that pair it is (the first, the second on the way back). The date is
// not part of it, so a slipped leg keeps its baseline. Each part is escaped so
// a name holding the separator cannot make two legs share a key.
func planLegKey(p plan, i int) string {
	from, to := p.Stops[i].Name, p.Stops[i+1].Name
	n := 0
	for j := 0; j < i; j++ {
		if p.Stops[j].Name == from && p.Stops[j+1].Name == to {
			n++
		}
	}
	return url.QueryEscape(from) + "|" + url.QueryEscape(to) + "|" + strconv.Itoa(n)
}

// planStillInPortAfter is how long past a leg's departure the boat may stay at
// the stop before the table asks about it.
const planStillInPortAfter = time.Hour

// markStillInPort flags the legs leaving today whose departure is an hour gone
// with the boat still at the stop. The reference is the operator's time, else the
// leg's first suggestion of the day (kept in the database, and still the
// reference once the day's window has closed), else, with the window closed and
// none kept, now. With no position there is no prompt: an unknown
// fix is not "in port".
func (d planAssessDeps) markStillInPort(p plan, work []*planLegWork, progress []string, now time.Time, out *planAssessment) {
	var late []int
	for i, w := range work {
		if progress[i] != "" {
			continue
		}
		zone, err := stayZoneAt(p.Stops[i].Lat, p.Stops[i].Lon)
		if err != nil || p.Stops[i].Depart != dayAt(zone, now) {
			continue
		}
		if d.pastDepartureHour(p, i, w, zone, now) {
			late = append(late, i)
		}
	}
	if len(late) == 0 {
		return
	}
	lat, lon, err := d.position()
	if err != nil {
		return
	}
	for _, i := range late {
		s := p.Stops[i]
		out.Legs[i].StillInPort = withinArrivedNm(lat, lon, s.Lat, s.Lon)
	}
}

func (d planAssessDeps) pastDepartureHour(p plan, i int, w *planLegWork, zone placeZone, now time.Time) bool {
	s := p.Stops[i]
	if s.DepartTime != "" {
		h, m, ok := passageClock(s.DepartTime)
		day, err := dayStart(s.Depart, zone.Loc)
		if !ok || err != nil {
			return false
		}
		y, mo, dd := day.Date()
		return !now.Before(time.Date(y, mo, dd, h, m, 0, 0, zone.Loc).Add(planStillInPortAfter))
	}
	res := w.res
	if res.DepartSuggested {
		t, err := time.Parse(time.RFC3339, res.Depart)
		if err != nil {
			return false
		}
		first := t
		if d.suggested != nil {
			if first, err = d.suggested(p.ID, res.Key, s.Depart, t); err != nil {
				// No reference, no prompt: the box could not say when the boat was
				// expected to leave.
				log.Printf("plans: first suggestion for %s not kept: %v", res.Key, err)
				return false
			}
		}
		return !now.Before(first.Add(planStillInPortAfter))
	}
	if res.NoteKind != planKindWindowPassed {
		return false
	}
	// The day's window has closed, so nothing is suggested now. The boat was still
	// expected to leave an hour after the first suggestion the day had, when one
	// was kept; the closed window alone counts only when none was.
	if d.keptSuggestion != nil {
		first, ok, err := d.keptSuggestion(p.ID, res.Key, s.Depart, zone.Loc)
		if err != nil {
			log.Printf("plans: first suggestion for %s not read: %v", res.Key, err)
			return false
		}
		if ok {
			return !now.Before(first.Add(planStillInPortAfter))
		}
	}
	return true
}

// visiblePassagesSince is the visible recorded passages that ended since the
// given time. A store that is missing or cannot be read is an error: without the
// record every leg would read as still to do, and no speed could be had.
func visiblePassagesSince(ctx context.Context, store *passageStore, since time.Time) ([]passageRecord, error) {
	if store == nil {
		return nil, fmt.Errorf("the passage record is not available")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("the passage record was not read: %w", err)
	}
	records, err := store.ListPassages()
	if err != nil {
		return nil, fmt.Errorf("the passage record could not be read: %w", err)
	}
	out := records[:0]
	for _, r := range records {
		if !r.End.Before(since) {
			out = append(out, r)
		}
	}
	return out, nil
}

// readPassageEnds is the last fix of every visible recorded passage that ended
// since the given time. A passage with no fix inside it cannot be placed; it is
// left out and logged.
func readPassageEnds(ctx context.Context, store *passageStore, since time.Time) ([]planPassageEnd, error) {
	records, err := visiblePassagesSince(ctx, store, since)
	if err != nil {
		return nil, err
	}
	var out []planPassageEnd
	for _, r := range records {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("the passage record was not read: %w", err)
		}
		end, err := store.LastTrackPointBetween(r.Start, r.End)
		if err != nil {
			return nil, fmt.Errorf("the passage record could not be read: %w", err)
		}
		if end == nil {
			log.Printf("plans: passage %s has no fix, so it cannot mark a leg done", r.ID)
			continue
		}
		out = append(out, planPassageEnd{At: r.End, Lat: end.Lat, Lon: end.Lon})
	}
	return out, nil
}
