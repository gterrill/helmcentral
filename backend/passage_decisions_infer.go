package main

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"
)

// Inference of what the operator did with a weather-window card (ADR 0211).
// This file is pure: the shell in passage_decisions_reconcile.go loads the
// decisions, passages and tracks and writes the answer back.

const (
	passageDecisionDepartWindow      = 4 * time.Hour
	passageDecisionStartRadiusNM     = 2.0
	passageDecisionEndRadiusNM       = 2.0
	passageDecisionSupersedeRadiusNM = 2.0
	metersPerNM                      = 1852.0
)

// decisionRowSnap is one row of the card: the date offered and when the first leg (index 0)
// would leave.
type decisionRowSnap struct {
	Date   string
	Depart time.Time
}

// decisionSnapshot is what the card said when it was made, parsed.
type decisionSnapshot struct {
	Loc    *time.Location
	Rows   []decisionRowSnap
	Routes [][]passageRoutePoint
}

// parseDecisionSnapshot reads a stored payload and routes. A zone that does
// not load is an error, never UTC: a wrong day would record the wrong row.
func parseDecisionSnapshot(payloadJSON, routesJSON string) (decisionSnapshot, error) {
	var p passageWindowPayload
	if err := json.Unmarshal([]byte(payloadJSON), &p); err != nil {
		return decisionSnapshot{}, fmt.Errorf("payload: %w", err)
	}
	if p.Timezone == "" {
		return decisionSnapshot{}, fmt.Errorf("payload has no timezone")
	}
	loc, err := time.LoadLocation(p.Timezone)
	if err != nil {
		return decisionSnapshot{}, fmt.Errorf("timezone %q: %w", p.Timezone, err)
	}
	var routes [][]passageRoutePoint
	if err := json.Unmarshal([]byte(routesJSON), &routes); err != nil {
		return decisionSnapshot{}, fmt.Errorf("routes: %w", err)
	}
	if len(routes) == 0 || len(routes[0]) < 2 {
		return decisionSnapshot{}, fmt.Errorf("routes: no route for the first leg")
	}
	snap := decisionSnapshot{Loc: loc, Routes: routes}
	for _, r := range p.Rows {
		var depart time.Time
		found := false
		for _, c := range r.Cells {
			if c.Leg == 0 {
				t, err := time.Parse(time.RFC3339, c.Depart)
				if err != nil {
					return decisionSnapshot{}, fmt.Errorf("row %s first leg depart %q: %w", r.DepartDate, c.Depart, err)
				}
				depart, found = t, true
				break
			}
		}
		if !found {
			return decisionSnapshot{}, fmt.Errorf("row %s has no first-leg cell", r.DepartDate)
		}
		snap.Rows = append(snap.Rows, decisionRowSnap{Date: r.DepartDate, Depart: depart})
	}
	return snap, nil
}

// decisionInput is one stored decision with its parsed snapshot. RowDate and
// PassageID are empty for NULL.
type decisionInput struct {
	ID          int64
	CardCreated time.Time
	State       string
	RowDate     string
	Source      string
	PassageID   string
	Snap        decisionSnapshot
}

type decisionPassage struct {
	ID         string
	Start, End time.Time
}

type decisionRun struct {
	Decisions []decisionInput
	// Passages are the visible ones; a hidden passage is simply absent.
	Passages []decisionPassage
	// Unknown are visible passages whose track source is unknown. They cannot
	// be matched, but one that started on a row's plan after the card was made
	// means the card can never be called stayed.
	Unknown []decisionPassage
	// Track loads a passage's track. It is called only for passages that pass
	// the cheap date and time checks, at most once each.
	Track          func(decisionPassage) ([]recordedTrackPoint, error)
	DetectorResume time.Time
	HasResume      bool
	Now            time.Time
}

// decisionUpdate is a change to write. A Link update changes only an operator
// decision's passage link, guarded by the state and row date seen here. Any
// other update replaces an inferred or blank decision.
type decisionUpdate struct {
	ID        int64
	Link      bool
	State     string
	RowDate   string
	PassageID string
}

type decisionOutcome struct {
	Updates []decisionUpdate
}

type decisionMatch struct {
	row      int
	passage  int
	distance time.Duration
}

// inferPassageDecisions works out, from scratch, what every decision should
// say. Operator choices are never changed; an operator went gets its link.
// Everything else is rewritten to whatever the passages show now.
func inferPassageDecisions(run decisionRun) (decisionOutcome, error) {
	type trackEntry struct {
		pts []recordedTrackPoint
		err error
	}
	tracks := map[string]trackEntry{}
	trackOf := func(p decisionPassage) ([]recordedTrackPoint, error) {
		if e, ok := tracks[p.ID]; ok {
			return e.pts, e.err
		}
		pts, err := run.Track(p)
		tracks[p.ID] = trackEntry{pts, err}
		return pts, err
	}

	// matchKind says how far a passage got along row's plan. Starting needs the
	// departure within 4 hours of the row's planned first-leg departure (elapsed
	// time, so a daylight-saving change counts as the hours it really was),
	// the card made before the start, and the first track point within 2 NM
	// of the route's start. Arriving also needs the track to pass within 2 NM
	// of the first leg's end. No calendar date is compared: a boat that planned 23:00
	// and left at 00:30 left on that row.
	const (
		noMatch = iota
		started
		arrived
	)
	matchKind := func(d decisionInput, row int, p decisionPassage) (int, error) {
		diff := p.Start.Sub(d.Snap.Rows[row].Depart)
		if diff < 0 {
			diff = -diff
		}
		if diff > passageDecisionDepartWindow || !d.CardCreated.Before(p.Start) {
			return noMatch, nil
		}
		pts, err := trackOf(p)
		if err != nil || len(pts) == 0 {
			return noMatch, err
		}
		route := d.Snap.Routes[0]
		first, end := route[0], route[len(route)-1]
		if haversineMeters(first.Lat, first.Lon, pts[0].Lat, pts[0].Lon) > passageDecisionStartRadiusNM*metersPerNM {
			return noMatch, nil
		}
		if minDistanceToTrack(end.Lat, end.Lon, pts) <= passageDecisionEndRadiusNM*metersPerNM {
			return arrived, nil
		}
		return started, nil
	}

	// candidates are the passages that arrived on some row's plan, best first:
	// the start closest to that row's planned departure, the earlier start on a
	// tie. Each passage is listed once, with its closest row. left reports a
	// passage that started on a row's plan but never reached the first leg's
	// end, so the boat did go out.
	candidates := func(d decisionInput, onlyRow string) (list []decisionMatch, left bool, err error) {
		for pi, p := range run.Passages {
			var m *decisionMatch
			for ri, r := range d.Snap.Rows {
				if onlyRow != "" && r.Date != onlyRow {
					continue
				}
				kind, err := matchKind(d, ri, p)
				if err != nil {
					return nil, false, err
				}
				if kind == started {
					left = true
				}
				if kind != arrived {
					continue
				}
				dist := p.Start.Sub(r.Depart)
				if dist < 0 {
					dist = -dist
				}
				if m == nil || dist < m.distance {
					m = &decisionMatch{row: ri, passage: pi, distance: dist}
				}
			}
			if m != nil {
				list = append(list, *m)
			}
		}
		sort.SliceStable(list, func(i, j int) bool {
			if list[i].distance != list[j].distance {
				return list[i].distance < list[j].distance
			}
			return run.Passages[list[i].passage].Start.Before(run.Passages[list[j].passage].Start)
		})
		return list, left, nil
	}

	var out decisionOutcome
	blocked := map[string]bool{}

	// Operator decisions first: their links block inference for the same passage.
	for _, d := range run.Decisions {
		if d.Source != "operator" {
			continue
		}
		link := ""
		if d.State == "went" && len(d.Snap.Rows) > 0 {
			list, _, err := candidates(d, d.RowDate)
			if err != nil {
				return out, err
			}
			if len(list) > 0 {
				link = run.Passages[list[0].passage].ID
				blocked[link] = true
			}
		}
		if link != d.PassageID {
			out.Updates = append(out.Updates, decisionUpdate{ID: d.ID, Link: true, State: d.State, RowDate: d.RowDate, PassageID: link})
		}
	}

	// Every other decision claims a passage, the latest card first, each
	// taking its best passage that is neither blocked by an operator went nor
	// already claimed by a later card.
	type claim struct {
		m       decisionMatch
		passage string
	}
	var order []int
	for i, d := range run.Decisions {
		if d.Source != "operator" && len(d.Snap.Rows) > 0 {
			order = append(order, i)
		}
	}
	sort.SliceStable(order, func(a, b int) bool { return cardAfter(run.Decisions[order[a]], run.Decisions[order[b]]) })
	claimed := map[string]bool{}
	claims := map[int]*claim{}
	arrivedAnywhere := map[int]bool{}
	leftNotArrived := map[int]bool{}
	for _, i := range order {
		list, left, err := candidates(run.Decisions[i], "")
		if err != nil {
			return out, err
		}
		leftNotArrived[i] = left
		arrivedAnywhere[i] = len(list) > 0
		for _, m := range list {
			pid := run.Passages[m.passage].ID
			if blocked[pid] || claimed[pid] {
				continue
			}
			claimed[pid] = true
			claims[i] = &claim{m, pid}
			break
		}
	}

	for _, i := range order {
		d := run.Decisions[i]
		state, rowDate, passageID := "", "", ""
		switch {
		case claims[i] != nil:
			c := claims[i]
			state, rowDate, passageID = "went", d.Snap.Rows[c.m.row].Date, c.passage
		case arrivedAnywhere[i]:
			// Every passage that arrived on its plan is held by someone else:
			// the boat did sail, so "stayed" would be wrong.
		case !leftNotArrived[i] && !unknownNearPlan(d, run.Unknown) && staysAtDock(d, run):
			state = "stayed"
		}
		if state == d.State && rowDate == d.RowDate && passageID == d.PassageID && (state == "" || d.Source == "inferred") {
			continue
		}
		out.Updates = append(out.Updates, decisionUpdate{ID: d.ID, State: state, RowDate: rowDate, PassageID: passageID})
	}
	return out, nil
}

// unknownNearPlan says a passage with an unreadable track started within the
// departure window of one of d's rows, after the card was made. The boat may
// well have sailed, so the card cannot be called stayed.
func unknownNearPlan(d decisionInput, unknown []decisionPassage) bool {
	for _, p := range unknown {
		if !d.CardCreated.Before(p.Start) {
			continue
		}
		for _, r := range d.Snap.Rows {
			diff := p.Start.Sub(r.Depart)
			if diff < 0 {
				diff = -diff
			}
			if diff <= passageDecisionDepartWindow {
				return true
			}
		}
	}
	return false
}

// cardAfter says whether a was made after b, the higher id breaking a tie.
func cardAfter(a, b decisionInput) bool {
	if !a.CardCreated.Equal(b.CardCreated) {
		return a.CardCreated.After(b.CardCreated)
	}
	return a.ID > b.ID
}

// staysAtDock is the three-part test for an inferred stayed: the card's last
// date is behind us, the detector has read past the end of that date so a late
// passage would already be stored, and no later card replaced this one.
func staysAtDock(d decisionInput, run decisionRun) bool {
	last := d.Snap.Rows[0].Date
	for _, r := range d.Snap.Rows {
		if r.Date > last {
			last = r.Date
		}
	}
	if run.Now.In(d.Snap.Loc).Format("2006-01-02") <= last {
		return false
	}
	day, err := time.ParseInLocation("2006-01-02", last, d.Snap.Loc)
	if err != nil || !run.HasResume {
		return false
	}
	endOfDay := time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, d.Snap.Loc)
	if run.DetectorResume.Before(endOfDay) {
		return false
	}
	for _, e := range run.Decisions {
		if e.ID != d.ID && cardAfter(e, d) && supersedes(e, d) {
			return false
		}
	}
	return true
}

// supersedes says later card e repeats d: it offers one of d's dates on a first leg
// that starts and ends within 2 NM of d's.
func supersedes(e, d decisionInput) bool {
	if len(e.Snap.Routes) == 0 || len(d.Snap.Routes) == 0 || len(e.Snap.Routes[0]) < 2 || len(d.Snap.Routes[0]) < 2 {
		return false
	}
	shared := false
	for _, er := range e.Snap.Rows {
		for _, dr := range d.Snap.Rows {
			if er.Date == dr.Date {
				shared = true
			}
		}
	}
	if !shared {
		return false
	}
	ea, eb := e.Snap.Routes[0][0], e.Snap.Routes[0][len(e.Snap.Routes[0])-1]
	da, db := d.Snap.Routes[0][0], d.Snap.Routes[0][len(d.Snap.Routes[0])-1]
	const lim = passageDecisionSupersedeRadiusNM * metersPerNM
	return haversineMeters(ea.Lat, ea.Lon, da.Lat, da.Lon) <= lim && haversineMeters(eb.Lat, eb.Lon, db.Lat, db.Lon) <= lim
}

// minDistanceToTrack is the least distance from a point to the track, taken
// along each segment's lines and never across a segment break.
func minDistanceToTrack(lat, lon float64, pts []recordedTrackPoint) float64 {
	best := math.Inf(1)
	for i, p := range pts {
		if d := haversineMeters(lat, lon, p.Lat, p.Lon); d < best {
			best = d
		}
		if i > 0 && pts[i-1].Segment == p.Segment {
			if d := pointToSegmentMeters(lat, lon, pts[i-1].Lat, pts[i-1].Lon, p.Lat, p.Lon); d < best {
				best = d
			}
		}
	}
	return best
}

// pointToSegmentMeters is the distance from (lat, lon) to the segment a-b on a
// local equirectangular plane centred on the point, which is exact enough for
// the few nautical miles this is used at.
func pointToSegmentMeters(lat, lon, aLat, aLon, bLat, bLon float64) float64 {
	const earth = 6371000.0
	k := math.Cos(lat * math.Pi / 180)
	xy := func(la, lo float64) (float64, float64) {
		return (lo - lon) * math.Pi / 180 * k * earth, (la - lat) * math.Pi / 180 * earth
	}
	ax, ay := xy(aLat, aLon)
	bx, by := xy(bLat, bLon)
	dx, dy := bx-ax, by-ay
	t := 0.0
	if l2 := dx*dx + dy*dy; l2 > 0 {
		t = math.Max(0, math.Min(1, -(ax*dx+ay*dy)/l2))
	}
	return math.Hypot(ax+t*dx, ay+t*dy)
}
