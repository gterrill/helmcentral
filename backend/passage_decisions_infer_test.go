package main

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

// Geometry: everything lies on the 149 E meridian, so 1 NM is one arc-minute
// of latitude. The planned first leg runs from -20.00 to -19.50 (30 NM).
const (
	pdiStartLat = -20.0
	pdiEndLat   = -19.5
	pdiLon      = 149.0
	pdiNM       = 1.0 / 60.0
)

var (
	pdiBrisbane = mustLoc("Australia/Brisbane")
	// pdiDepart is 06:00 on 23 Oct in Brisbane.
	pdiDepart = time.Date(2026, 10, 23, 6, 0, 0, 0, pdiBrisbane)
	pdiCard   = pdiDepart.Add(-20 * time.Hour)
	pdiNow    = time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
)

func mustLoc(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

func pdiRoutes() [][]passageRoutePoint {
	return [][]passageRoutePoint{{{Lat: pdiStartLat, Lon: pdiLon}, {Lat: pdiEndLat, Lon: pdiLon}}}
}

// pdiDecision is a blank decision whose card offered departures on dates, each
// at 06:00 local.
func pdiDecision(id int64, created time.Time, dates ...string) decisionInput {
	snap := decisionSnapshot{Loc: pdiBrisbane, Routes: pdiRoutes()}
	for _, d := range dates {
		day, _ := time.ParseInLocation("2006-01-02", d, pdiBrisbane)
		snap.Rows = append(snap.Rows, decisionRowSnap{Date: d, Depart: day.Add(6 * time.Hour)})
	}
	return decisionInput{ID: id, CardCreated: created, Snap: snap}
}

func pdiPassage(id string, start time.Time) decisionPassage {
	return decisionPassage{ID: id, Start: start, End: start.Add(8 * time.Hour)}
}

// pdiTrack is a single-segment track from startLat to endLat in 1 NM steps.
func pdiTrack(p decisionPassage, startLat, endLat float64) []recordedTrackPoint {
	var out []recordedTrackPoint
	i := 0
	for lat := startLat; lat <= endLat+1e-9; lat += pdiNM {
		out = append(out, recordedTrackPoint{At: p.Start.Add(time.Duration(i) * time.Minute), Lat: lat, Lon: pdiLon})
		i++
	}
	if last := out[len(out)-1]; math.Abs(last.Lat-endLat) > 1e-9 {
		out = append(out, recordedTrackPoint{At: p.Start.Add(time.Duration(i) * time.Minute), Lat: endLat, Lon: pdiLon})
	}
	return out
}

func pdiRun(decs []decisionInput, ps []decisionPassage, tracks map[string][]recordedTrackPoint) decisionOutcome {
	return pdiRunAt(decs, ps, tracks, pdiNow, time.Time{}, false)
}

func pdiRunAt(decs []decisionInput, ps []decisionPassage, tracks map[string][]recordedTrackPoint, now, resume time.Time, hasResume bool) decisionOutcome {
	out, err := inferPassageDecisions(decisionRun{
		Decisions: decs, Passages: ps, Now: now, DetectorResume: resume, HasResume: hasResume,
		Track: func(p decisionPassage) ([]recordedTrackPoint, error) { return tracks[p.ID], nil },
	})
	if err != nil {
		panic(err)
	}
	return out
}

func pdiUpdate(o decisionOutcome, id int64) *decisionUpdate {
	for i := range o.Updates {
		if o.Updates[i].ID == id {
			return &o.Updates[i]
		}
	}
	return nil
}

func wantWent(t *testing.T, o decisionOutcome, id int64, date, passage string) {
	t.Helper()
	u := pdiUpdate(o, id)
	if u == nil || u.State != "went" || u.RowDate != date || u.PassageID != passage || u.Link {
		t.Fatalf("decision %d: want inferred went %s on %s, got %+v (all %+v)", id, date, passage, u, o.Updates)
	}
}

func wantNoChange(t *testing.T, o decisionOutcome, id int64) {
	t.Helper()
	if u := pdiUpdate(o, id); u != nil {
		t.Fatalf("decision %d: want no change, got %+v", id, u)
	}
}

func wantBlank(t *testing.T, o decisionOutcome, id int64) {
	t.Helper()
	u := pdiUpdate(o, id)
	if u == nil || u.State != "" || u.RowDate != "" || u.PassageID != "" || u.Link {
		t.Fatalf("decision %d: want reset to blank, got %+v", id, u)
	}
}

func oneTrip(startOffset time.Duration, startLat, endLat float64) ([]decisionPassage, map[string][]recordedTrackPoint) {
	p := pdiPassage("p1", pdiDepart.Add(startOffset))
	return []decisionPassage{p}, map[string][]recordedTrackPoint{"p1": pdiTrack(p, startLat, endLat)}
}

func TestInfer_APassageOnThePlannedRouteIsWent(t *testing.T) {
	ps, tr := oneTrip(30*time.Minute, pdiStartLat, pdiEndLat)
	o := pdiRun([]decisionInput{pdiDecision(1, pdiCard, "2026-10-22", "2026-10-23")}, ps, tr)
	wantWent(t, o, 1, "2026-10-23", "p1")
}

func TestInfer_DepartureWindowIsFourHoursEitherWay(t *testing.T) {
	for _, c := range []struct {
		off  time.Duration
		want bool
	}{
		{4 * time.Hour, true}, {-4 * time.Hour, true},
		{4*time.Hour + time.Minute, false}, {-4*time.Hour - time.Minute, false},
	} {
		ps, tr := oneTrip(c.off, pdiStartLat, pdiEndLat)
		o := pdiRun([]decisionInput{pdiDecision(1, pdiCard.Add(-24*time.Hour), "2026-10-23")}, ps, tr)
		if got := pdiUpdate(o, 1) != nil && pdiUpdate(o, 1).State == "went"; got != c.want {
			t.Errorf("offset %v: went=%v, want %v", c.off, got, c.want)
		}
	}
}

func TestInfer_StartRadiusIsTwoNauticalMiles(t *testing.T) {
	for _, c := range []struct {
		nm   float64
		want bool
	}{{1.9, true}, {2.1, false}} {
		ps, tr := oneTrip(time.Hour, pdiStartLat-c.nm*pdiNM, pdiEndLat)
		o := pdiRun([]decisionInput{pdiDecision(1, pdiCard, "2026-10-23")}, ps, tr)
		if got := pdiUpdate(o, 1) != nil && pdiUpdate(o, 1).State == "went"; got != c.want {
			t.Errorf("start %.1f NM off: went=%v, want %v", c.nm, got, c.want)
		}
	}
}

func TestInfer_EndRadiusOnAVertex(t *testing.T) {
	for _, c := range []struct {
		nm   float64
		want bool
	}{{1.9, true}, {2.1, false}} {
		ps, tr := oneTrip(time.Hour, pdiStartLat, pdiEndLat-c.nm*pdiNM)
		o := pdiRun([]decisionInput{pdiDecision(1, pdiCard, "2026-10-23")}, ps, tr)
		if got := pdiUpdate(o, 1) != nil && pdiUpdate(o, 1).State == "went"; got != c.want {
			t.Errorf("end short by %.1f NM: went=%v, want %v", c.nm, got, c.want)
		}
	}
}

func TestInfer_EndRadiusMidSegmentOnAThinnedTrack(t *testing.T) {
	p := pdiPassage("p1", pdiDepart.Add(time.Hour))
	// Two points 60 NM apart: the planned end is 30 NM from either vertex and
	// on the segment between them.
	thin := []recordedTrackPoint{
		{At: p.Start, Lat: pdiStartLat, Lon: pdiLon},
		{At: p.End, Lat: pdiStartLat + 1.0, Lon: pdiLon},
	}
	o := pdiRun([]decisionInput{pdiDecision(1, pdiCard, "2026-10-23")}, []decisionPassage{p}, map[string][]recordedTrackPoint{"p1": thin})
	wantWent(t, o, 1, "2026-10-23", "p1")
}

func TestInfer_NoJoiningAcrossASegmentBreak(t *testing.T) {
	p := pdiPassage("p1", pdiDepart.Add(time.Hour))
	tr := []recordedTrackPoint{
		{At: p.Start, Segment: 0, Lat: pdiStartLat, Lon: pdiLon},
		{At: p.Start.Add(time.Hour), Segment: 0, Lat: -19.7, Lon: pdiLon},
		{At: p.Start.Add(2 * time.Hour), Segment: 1, Lat: -19.3, Lon: pdiLon},
		{At: p.Start.Add(3 * time.Hour), Segment: 1, Lat: -19.0, Lon: pdiLon},
	}
	o := pdiRun([]decisionInput{pdiDecision(1, pdiCard, "2026-10-23")}, []decisionPassage{p}, map[string][]recordedTrackPoint{"p1": tr})
	wantNoChange(t, o, 1) // the straight line across the break passes the end, the track does not
}

// The operator planned to leave at 23:00 on the 22nd and sailed at 00:30 on
// the 23rd. The row is the one that was planned; no calendar date is compared.
func TestInfer_APassageAfterMidnightMatchesThePlannedRow(t *testing.T) {
	planned := time.Date(2026, 10, 22, 23, 0, 0, 0, pdiBrisbane)
	start := time.Date(2026, 10, 23, 0, 30, 0, 0, pdiBrisbane)
	p := pdiPassage("p1", start)
	tr := map[string][]recordedTrackPoint{"p1": pdiTrack(p, pdiStartLat, pdiEndLat)}
	d := pdiDecision(1, planned.Add(-10*time.Hour), "2026-10-22", "2026-10-23")
	d.Snap.Rows[0].Depart = planned
	d.Snap.Rows[1].Depart = planned.Add(31 * time.Hour) // far from the start
	wantWent(t, pdiRun([]decisionInput{d}, []decisionPassage{p}, tr), 1, "2026-10-22", "p1")
}

// The window is a duration. Sydney's clocks went forward at 02:00 on 4 Oct
// 2026 (16:00Z on the 3rd), so 17:00Z on the 3rd is four elapsed hours after a 23:00 AEST departure on the
// 3rd and five on the wall clock.
func TestInfer_TheFourHourWindowIsElapsedTimeAcrossADaylightSavingChange(t *testing.T) {
	syd := mustLoc("Australia/Sydney")
	planned := time.Date(2026, 10, 3, 23, 0, 0, 0, syd)
	for _, c := range []struct {
		start time.Time
		want  bool
	}{
		{time.Date(2026, 10, 3, 17, 0, 0, 0, time.UTC), true},
		{time.Date(2026, 10, 3, 17, 1, 0, 0, time.UTC), false},
		{time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC), true}, // 19:00 AEST, four hours early
		{time.Date(2026, 10, 3, 8, 59, 0, 0, time.UTC), false},
	} {
		p := pdiPassage("p1", c.start)
		tr := map[string][]recordedTrackPoint{"p1": pdiTrack(p, pdiStartLat, pdiEndLat)}
		d := pdiDecision(1, planned.Add(-48*time.Hour), "2026-10-03")
		d.Snap.Loc = syd
		d.Snap.Rows[0].Depart = planned
		o := pdiRun([]decisionInput{d}, []decisionPassage{p}, tr)
		if got := pdiUpdate(o, 1) != nil && pdiUpdate(o, 1).State == "went"; got != c.want {
			t.Errorf("start %v: went=%v, want %v", c.start, got, c.want)
		}
	}
}

// Left but did not arrive: the card is blank, never stayed, however the
// passage came to stop short.
func TestInfer_LeftButDidNotArriveStaysBlank(t *testing.T) {
	resume := time.Date(2026, 10, 25, 0, 0, 0, 0, pdiBrisbane)
	stayed := func() decisionInput {
		d := pdiDecision(1, pdiCard, "2026-10-23")
		d.State, d.Source = "stayed", "inferred" // an earlier pass thought so
		return d
	}
	// Turned back: the boat left on time and stopped 18 NM short of the end.
	ps, tr := oneTrip(time.Hour, pdiStartLat, pdiEndLat-18*pdiNM)
	wantBlank(t, pdiRunAt([]decisionInput{stayed()}, ps, tr, pdiNow, resume, true), 1)

	// Split: the first half left on time and ends mid-way, the second half
	// starts far from the planned start.
	first := pdiPassage("a", pdiDepart.Add(time.Hour))
	first.End = first.Start.Add(2 * time.Hour)
	second := pdiPassage("b", first.End)
	tracks := map[string][]recordedTrackPoint{
		"a": pdiTrack(first, pdiStartLat, -19.75),
		"b": pdiTrack(second, -19.75, pdiEndLat),
	}
	wantBlank(t, pdiRunAt([]decisionInput{stayed()}, []decisionPassage{first, second}, tracks, pdiNow, resume, true), 1)

	// A passage that never started near the route is no sign of leaving: the
	// card can still be stayed.
	far := pdiPassage("c", pdiDepart.Add(time.Hour))
	farTrack := map[string][]recordedTrackPoint{"c": pdiTrack(far, -25, -24.5)}
	d := pdiDecision(1, pdiCard, "2026-10-23")
	if u := pdiUpdate(pdiRunAt([]decisionInput{d}, []decisionPassage{far}, farTrack, pdiNow, resume, true), 1); u == nil || u.State != "stayed" {
		t.Fatalf("a passage elsewhere does not stop stayed: %+v", u)
	}
	// And a passage that started but whose card is later than its start is not
	// evidence either.
	late := pdiDecision(1, ps[0].Start.Add(time.Minute), "2026-10-23")
	if u := pdiUpdate(pdiRunAt([]decisionInput{late}, ps, tr, pdiNow, resume, true), 1); u == nil || u.State != "stayed" {
		t.Fatalf("a passage that began before the card was made is not evidence: %+v", u)
	}
}

func TestParseDecisionSnapshot_UnloadableZoneIsAnError(t *testing.T) {
	routes, _ := json.Marshal(pdiRoutes())
	payload := func(tz string) string {
		return fmt.Sprintf(`{"timezone":%q,"legs":[{"name":"x"}],"rows":[{"depart_date":"2026-10-23","verdict":"go","cells":[{"leg":0,"date":"2026-10-23","depart":"2026-10-23T06:00:00+10:00","arrive":"2026-10-23T16:00:00+10:00","state":"go","not_checked":[]}]}]}`, tz)
	}
	snap, err := parseDecisionSnapshot(payload("Australia/Brisbane"), string(routes))
	if err != nil || snap.Loc.String() != "Australia/Brisbane" || len(snap.Rows) != 1 || !snap.Rows[0].Depart.Equal(pdiDepart) {
		t.Fatalf("snap=%+v err=%v", snap, err)
	}
	if _, err := parseDecisionSnapshot(payload("Nowhere/Land"), string(routes)); err == nil || !strings.Contains(err.Error(), "Nowhere/Land") {
		t.Fatalf("an unloadable zone must be an error naming it, got %v", err)
	}
	if _, err := parseDecisionSnapshot(payload(""), string(routes)); err == nil {
		t.Fatal("an empty zone must not fall back to UTC")
	}
	if _, err := parseDecisionSnapshot(`{"timezone":"Australia/Brisbane","legs":[],"rows":[{"depart_date":"2026-10-23","cells":[]}]}`, string(routes)); err == nil {
		t.Fatal("a row with no first-leg cell is an error")
	}
	if _, err := parseDecisionSnapshot(payload("Australia/Brisbane"), `[]`); err == nil {
		t.Fatal("no routes is an error")
	}
}

func TestInfer_ACardMadeAfterDepartureIsNotEligible(t *testing.T) {
	ps, tr := oneTrip(time.Hour, pdiStartLat, pdiEndLat)
	late := pdiDecision(1, ps[0].Start.Add(time.Minute), "2026-10-23")
	wantNoChange(t, pdiRun([]decisionInput{late}, ps, tr), 1)
	atStart := pdiDecision(2, ps[0].Start, "2026-10-23")
	wantNoChange(t, pdiRun([]decisionInput{atStart}, ps, tr), 2) // strictly before
}

func TestInfer_LatestCardBeforeDepartureWinsAndTheEarlierEndsBlank(t *testing.T) {
	ps, tr := oneTrip(time.Hour, pdiStartLat, pdiEndLat)
	older := pdiDecision(1, pdiCard.Add(-2*time.Hour), "2026-10-23")
	newer := pdiDecision(2, pdiCard, "2026-10-23")
	older.State, older.RowDate, older.Source, older.PassageID = "went", "2026-10-23", "inferred", "p1"
	o := pdiRun([]decisionInput{older, newer}, ps, tr)
	wantWent(t, o, 2, "2026-10-23", "p1")
	wantBlank(t, o, 1)

	// Same creation time: the higher id wins.
	a, b := pdiDecision(5, pdiCard, "2026-10-23"), pdiDecision(6, pdiCard, "2026-10-23")
	o = pdiRun([]decisionInput{a, b}, ps, tr)
	wantWent(t, o, 6, "2026-10-23", "p1")
	wantNoChange(t, o, 5) // already blank
}

func TestInfer_TwoPassagesOneDayTakesTheStartClosestToThePlan(t *testing.T) {
	p1 := pdiPassage("p1", pdiDepart.Add(3*time.Hour))
	p2 := pdiPassage("p2", pdiDepart.Add(30*time.Minute))
	tr := map[string][]recordedTrackPoint{
		"p1": pdiTrack(p1, pdiStartLat, pdiEndLat), "p2": pdiTrack(p2, pdiStartLat, pdiEndLat),
	}
	d := pdiDecision(1, pdiCard, "2026-10-23")
	wantWent(t, pdiRun([]decisionInput{d}, []decisionPassage{p1, p2}, tr), 1, "2026-10-23", "p2")
	// A tie goes to the earlier start: 30 min after and 30 min before.
	early := pdiPassage("pe", pdiDepart.Add(-30*time.Minute))
	late := pdiPassage("pl", pdiDepart.Add(30*time.Minute))
	tr["pe"], tr["pl"] = pdiTrack(early, pdiStartLat, pdiEndLat), pdiTrack(late, pdiStartLat, pdiEndLat)
	wantWent(t, pdiRun([]decisionInput{d}, []decisionPassage{late, early}, tr), 1, "2026-10-23", "pe")
}

func TestInfer_StayedNeedsThePastDateAndTheDetectorPastIt(t *testing.T) {
	d := pdiDecision(1, pdiCard, "2026-10-22", "2026-10-23")
	endOfLast := time.Date(2026, 10, 24, 0, 0, 0, 0, pdiBrisbane)
	stayed := func(o decisionOutcome) bool {
		u := pdiUpdate(o, 1)
		return u != nil && u.State == "stayed" && u.RowDate == "" && u.PassageID == ""
	}

	// Today is the last date: not yet.
	if o := pdiRunAt([]decisionInput{d}, nil, nil, time.Date(2026, 10, 23, 20, 0, 0, 0, pdiBrisbane), endOfLast, true); pdiUpdate(o, 1) != nil {
		t.Errorf("on the last date nothing is inferred: %+v", o.Updates)
	}
	// A new day, detector resume exactly at the end of the date: stayed.
	tomorrow := time.Date(2026, 10, 24, 0, 5, 0, 0, pdiBrisbane)
	if o := pdiRunAt([]decisionInput{d}, nil, nil, tomorrow, endOfLast, true); !stayed(o) {
		t.Errorf("want stayed, got %+v", o.Updates)
	}
	// Detector one second short of the end of the date: a late passage could still be coming.
	if o := pdiRunAt([]decisionInput{d}, nil, nil, tomorrow, endOfLast.Add(-time.Second), true); pdiUpdate(o, 1) != nil {
		t.Errorf("detector behind: %+v", o.Updates)
	}
	// No detector state at all.
	if o := pdiRunAt([]decisionInput{d}, nil, nil, tomorrow, time.Time{}, false); pdiUpdate(o, 1) != nil {
		t.Errorf("no detector state: %+v", o.Updates)
	}
}

func TestInfer_ASupersededCardStaysBlank(t *testing.T) {
	old := pdiDecision(1, pdiCard.Add(-time.Hour), "2026-10-22")
	newer := pdiDecision(2, pdiCard, "2026-10-22", "2026-10-23")
	resume := time.Date(2026, 10, 25, 0, 0, 0, 0, pdiBrisbane)
	o := pdiRunAt([]decisionInput{old, newer}, nil, nil, pdiNow, resume, true)
	wantNoChange(t, o, 1) // superseded: blank stays blank
	if u := pdiUpdate(o, 2); u == nil || u.State != "stayed" {
		t.Fatalf("the latest card is not superseded: %+v", o.Updates)
	}
	// A later card for a different first-leg start does not supersede.
	other := pdiDecision(3, pdiCard, "2026-10-22")
	other.Snap.Routes = [][]passageRoutePoint{{{Lat: -25, Lon: 150}, {Lat: -24, Lon: 150}}}
	o = pdiRunAt([]decisionInput{old, other}, nil, nil, pdiNow, resume, true)
	if u := pdiUpdate(o, 1); u == nil || u.State != "stayed" {
		t.Errorf("a different route does not supersede: %+v", o.Updates)
	}
	// A later card with no shared date does not supersede.
	apart := pdiDecision(4, pdiCard, "2026-10-21")
	o = pdiRunAt([]decisionInput{old, apart}, nil, nil, pdiNow, resume, true)
	if u := pdiUpdate(o, 1); u == nil || u.State != "stayed" {
		t.Errorf("no shared date does not supersede: %+v", o.Updates)
	}
}

func TestInfer_ALatePassageFlipsInferredStayedToWent(t *testing.T) {
	d := pdiDecision(1, pdiCard, "2026-10-23")
	d.State, d.Source = "stayed", "inferred"
	ps, tr := oneTrip(time.Hour, pdiStartLat, pdiEndLat)
	wantWent(t, pdiRun([]decisionInput{d}, ps, tr), 1, "2026-10-23", "p1")
}

func TestInfer_InferredWentThatNoLongerMatchesResets(t *testing.T) {
	d := pdiDecision(1, pdiCard, "2026-10-23")
	d.State, d.RowDate, d.Source, d.PassageID = "went", "2026-10-23", "inferred", "gone"
	// The passage was hidden or deleted: it is not in the visible list. The
	// detector has not reached the end of the date, so the card is blank, not stayed.
	o := pdiRunAt([]decisionInput{d}, nil, nil, time.Date(2026, 10, 23, 20, 0, 0, 0, pdiBrisbane), time.Time{}, false)
	wantBlank(t, o, 1)
	// Unhiding puts the passage back in the list and the went returns.
	ps, tr := oneTrip(time.Hour, pdiStartLat, pdiEndLat)
	wantWent(t, pdiRun([]decisionInput{d}, ps, tr), 1, "2026-10-23", "p1")
}

func TestInfer_OperatorStateIsNeverOverwrittenAndABlankIsNeverRefilled(t *testing.T) {
	ps, tr := oneTrip(time.Hour, pdiStartLat, pdiEndLat)
	resume := time.Date(2026, 10, 25, 0, 0, 0, 0, pdiBrisbane)
	for _, state := range []string{"", "stayed", "just_looking"} {
		d := pdiDecision(1, pdiCard, "2026-10-23")
		d.State, d.Source = state, "operator"
		o := pdiRunAt([]decisionInput{d}, ps, tr, pdiNow, resume, true)
		wantNoChange(t, o, 1)
	}
	// An operator state with a stale link loses only the link.
	d := pdiDecision(1, pdiCard, "2026-10-23")
	d.State, d.Source, d.PassageID = "stayed", "operator", "old"
	o := pdiRun([]decisionInput{d}, ps, tr)
	if u := pdiUpdate(o, 1); u == nil || !u.Link || u.PassageID != "" || u.State != "stayed" {
		t.Fatalf("want a link-only clear, got %+v", u)
	}
}

func TestInfer_OperatorWentLinksToTheBestMatchOrNone(t *testing.T) {
	ps, tr := oneTrip(time.Hour, pdiStartLat, pdiEndLat)
	d := pdiDecision(1, pdiCard, "2026-10-22", "2026-10-23")
	d.State, d.RowDate, d.Source = "went", "2026-10-23", "operator"
	o := pdiRun([]decisionInput{d}, ps, tr)
	if u := pdiUpdate(o, 1); u == nil || !u.Link || u.PassageID != "p1" || u.State != "went" || u.RowDate != "2026-10-23" {
		t.Fatalf("got %+v", u)
	}
	// The operator's row is the one that counts: a passage on the other date
	// is no link.
	d.RowDate = "2026-10-22"
	wantNoChange(t, pdiRun([]decisionInput{d}, ps, tr), 1)
	// An already correct link makes no update.
	d.RowDate, d.PassageID = "2026-10-23", "p1"
	wantNoChange(t, pdiRun([]decisionInput{d}, ps, tr), 1)
}

func TestInfer_AnOperatorWentOnAPassageBlocksInferenceForItElsewhere(t *testing.T) {
	ps, tr := oneTrip(time.Hour, pdiStartLat, pdiEndLat)
	op := pdiDecision(1, pdiCard.Add(-time.Hour), "2026-10-23")
	op.State, op.RowDate, op.Source, op.PassageID = "went", "2026-10-23", "operator", "p1"
	other := pdiDecision(2, pdiCard, "2026-10-23") // newer, would win on its own
	o := pdiRun([]decisionInput{op, other}, ps, tr)
	wantNoChange(t, o, 2)
	wantNoChange(t, o, 1)
}

func TestInfer_DecisionsWithNoRowsOrRoutesAreSkippedNotGuessed(t *testing.T) {
	ps, tr := oneTrip(time.Hour, pdiStartLat, pdiEndLat)
	d := pdiDecision(1, pdiCard)
	wantNoChange(t, pdiRun([]decisionInput{d}, ps, tr), 1)
}

func TestPointToSegmentMeters(t *testing.T) {
	// Perpendicular foot inside the segment: 3 NM east of the meridian at 20 S.
	lon3NM := pdiLon + 3.0*pdiNM/0.9396926 // cos(20 deg)
	got := pointToSegmentMeters(-19.5, lon3NM, -20.0, pdiLon, -19.0, pdiLon)
	if got < 3*1852*0.99 || got > 3*1852*1.01 {
		t.Errorf("perpendicular = %.0f m", got)
	}
	// Beyond the end it is the distance to the end point.
	got = pointToSegmentMeters(-18.9, pdiLon, -20.0, pdiLon, -19.0, pdiLon)
	if got < 6*1852*0.99 || got > 6*1852*1.01 {
		t.Errorf("past the end = %.0f m", got)
	}
	// A zero-length segment is a point.
	if got := pointToSegmentMeters(-19.9, pdiLon, -20.0, pdiLon, -20.0, pdiLon); got < 5.9*1852 || got > 6.1*1852 {
		t.Errorf("degenerate = %.0f m", got)
	}
}

func TestInfer_APassageOfUnknownSourceNearThePlanMeansNotStayed(t *testing.T) {
	resume := time.Date(2026, 10, 25, 0, 0, 0, 0, pdiBrisbane)
	run := func(unknownStart time.Time) *decisionUpdate {
		d := pdiDecision(1, pdiCard, "2026-10-23")
		out, err := inferPassageDecisions(decisionRun{
			Decisions: []decisionInput{d}, Unknown: []decisionPassage{pdiPassage("u", unknownStart)},
			Track: func(decisionPassage) ([]recordedTrackPoint, error) { return nil, nil },
			Now:   pdiNow, DetectorResume: resume, HasResume: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		return pdiUpdate(out, 1)
	}
	// We cannot see its track, so we cannot say the boat stayed.
	if u := run(pdiDepart.Add(time.Hour)); u != nil {
		t.Fatalf("an unreadable passage on the plan keeps the card blank: %+v", u)
	}
	// Far from the plan, or made before the card: no evidence either way.
	if u := run(pdiDepart.Add(5 * time.Hour)); u == nil || u.State != "stayed" {
		t.Fatalf("outside the window it is no evidence: %+v", u)
	}
	if u := run(pdiCard.Add(-time.Minute)); u == nil || u.State != "stayed" {
		t.Fatalf("started before the card was made: %+v", u)
	}
}

func TestInfer_ACardThatLosesItsFirstPassageTriesItsOthers(t *testing.T) {
	p1 := pdiPassage("p1", pdiDepart.Add(30*time.Minute))
	p2 := pdiPassage("p2", pdiDepart.Add(2*time.Hour))
	tr := map[string][]recordedTrackPoint{"p1": pdiTrack(p1, pdiStartLat, pdiEndLat), "p2": pdiTrack(p2, pdiStartLat, pdiEndLat)}
	ps := []decisionPassage{p1, p2}

	older := pdiDecision(1, pdiCard.Add(-time.Hour), "2026-10-23")
	later := pdiDecision(2, pdiCard, "2026-10-23")
	o := pdiRun([]decisionInput{older, later}, ps, tr)
	wantWent(t, o, 2, "2026-10-23", "p1")
	wantWent(t, o, 1, "2026-10-23", "p2")

	// An operator went on p1 blocks it, and the inferred card moves to p2.
	op := pdiDecision(3, pdiCard.Add(-2*time.Hour), "2026-10-23")
	op.State, op.RowDate, op.Source = "went", "2026-10-23", "operator"
	o = pdiRun([]decisionInput{op, later}, ps, tr)
	wantWent(t, o, 2, "2026-10-23", "p2")

	// With nothing left it stays blank, not stayed: the boat did go out.
	o = pdiRunAt([]decisionInput{op, later, older}, ps, tr, pdiNow, time.Date(2026, 10, 25, 0, 0, 0, 0, pdiBrisbane), true)
	wantNoChange(t, o, 1)
}
