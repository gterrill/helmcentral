package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

// The stretches get_plans sends are worded for Mate: their clock labels read as the leg's
// Depart and Arrive do, and their reason carries the beyond-the-record words.

// wireHour is a judged hour of the given band from hour h of the 14th on loc, one hour long.
func wireHour(loc *time.Location, h int, band string) judgedHour {
	start := time.Date(2026, 11, 14, h, 0, 0, 0, loc)
	return judgedHour{start: start, end: start.Add(time.Hour), band: band}
}

func wireInput(loc, toLoc *time.Location, learned bool) legComfortInput {
	return legComfortInput{comfortSetup: &comfortSetup{bands: comfortBands{Learned: learned}}, loc: loc, toLoc: toLoc}
}

func wireZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("no zone data for %s: %v", name, err)
	}
	return loc
}

// 2: Brisbane to Sydney under daylight time. The last stretch ends at the arrival, and
// the arrival is told on the clock of the stop it is at.
func TestStretchesWire_TheLastUntilLabelReadsAsTheLegsArriveDoes(t *testing.T) {
	bris, syd := wireZone(t, "Australia/Brisbane"), wireZone(t, "Australia/Sydney")
	hours := []judgedHour{wireHour(bris, 8, bandComfortable), wireHour(bris, 9, bandComfortable), wireHour(bris, 10, bandComfortable)}
	arrive := hours[2].end
	got := wireInput(bris, syd, true).stretchesWire(stretchesOf(hours))
	if len(got) != 1 {
		t.Fatalf("stretches = %+v", got)
	}
	wantArrive, err := planInstantLabel(fmtLeg(arrive, syd))
	if err != nil {
		t.Fatal(err)
	}
	// The zones differ, so the label is the leg's Arrive (which names no zone) with the zone.
	if got[0].UntilLabel != wantArrive+" AEDT" || got[0].UntilLabel != "Sat 14 Nov 12.00 PM AEDT" {
		t.Errorf("until_label = %q, the leg's Arrive reads %q", got[0].UntilLabel, wantArrive)
	}
	// The departure is told on the departure stop's clock.
	wantDepart, _ := planInstantLabel(fmtLeg(hours[0].start, bris))
	if got[0].FromLabel != "Sat 14 Nov 8.00 AM AEST" || got[0].FromLabel != wantDepart+" AEST" {
		t.Errorf("from_label = %q, the leg's Depart reads %q", got[0].FromLabel, wantDepart)
	}
}

// 3: the first label always carries its day; a later one only when it falls on another
// day than the label before it.
func TestStretchesWire_TheFirstFromLabelCarriesItsDay(t *testing.T) {
	for _, underway := range []bool{false, true} {
		in := wireInput(paLoc, nil, true)
		in.underway = underway
		// Under way, the span starts at now, half past the hour.
		first := rvHour(30, 90, bandComfortable, "")
		got := in.stretchesWire(stretchesOf([]judgedHour{first, rvHour(90, 150, bandLively, "")}))
		if len(got) != 2 {
			t.Fatalf("underway %v: stretches = %+v", underway, got)
		}
		if got[0].FromLabel != "Sat 14 Nov 8.30 AM" {
			t.Errorf("underway %v: first from_label = %q", underway, got[0].FromLabel)
		}
		if got[0].UntilLabel != "9.30 AM" || got[1].FromLabel != "9.30 AM" || got[1].UntilLabel != "10.30 AM" {
			t.Errorf("underway %v: same-day labels carry no day: %+v", underway, got)
		}
	}
}

func TestStretchesWire_ALabelOnAnotherDayThanTheOneBeforeItCarriesItsDay(t *testing.T) {
	in := wireInput(paLoc, nil, true)
	hours := []judgedHour{wireHour(paLoc, 22, bandComfortable), wireHour(paLoc, 23, bandLively), wireHour(paLoc, 24, bandLively), wireHour(paLoc, 25, bandComfortable), wireHour(paLoc, 26, bandComfortable)}
	got := in.stretchesWire(stretchesOf(hours))
	if len(got) != 3 {
		t.Fatalf("stretches = %+v", got)
	}
	// 10 PM Comfortable, 11 PM to 1 AM Lively, 1 AM to 3 AM Comfortable.
	if got[0].FromLabel != "Sat 14 Nov 10.00 PM" || got[0].UntilLabel != "11.00 PM" {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].FromLabel != "11.00 PM" || got[1].UntilLabel != "Sun 15 Nov 1.00 AM" {
		t.Errorf("second = %+v", got[1])
	}
	if got[2].FromLabel != "1.00 AM" || got[2].UntilLabel != "3.00 AM" {
		t.Errorf("third = %+v", got[2])
	}
}

// 4: the reason carries the beyond-the-record words; there is no separate flag.
func TestStretchesWire_TheReasonCarriesTheBeyondTheRecordWords(t *testing.T) {
	in := wireInput(paLoc, nil, true)
	rougherComfortable := rvHour(0, 60, bandComfortable, "")
	rougherComfortable.aboveRated = true
	steeperLively := rvHour(60, 120, bandLively, "")
	steeperLively.beyond = true
	roughLively := rvHour(120, 180, bandLively, "")
	roughLively.aboveRated = true
	roughLively.lead = comfortLead{Word: "pitch"}
	got := in.stretchesWire(stretchesOf([]judgedHour{rougherComfortable, steeperLively, roughLively}))
	if len(got) != 3 {
		t.Fatalf("stretches = %+v", got)
	}
	if got[0].Reason != "" {
		t.Errorf("a Comfortable stretch with a rougher hour says nothing: %+v", got[0])
	}
	if !strings.Contains(got[1].Reason, comfortBeyondSteep) || !strings.Contains(got[2].Reason, comfortBeyondRough) {
		t.Errorf("a Lively stretch beyond the record says so: %+v", got[1:])
	}
}

// Every kind of hour, judged, out of time or a gap, ends up in a stretch that names the
// place, since where is read from the route and not from the hour.
func TestJudgedHours_EveryKindOfHourIsInAStretchWithItsPlace(t *testing.T) {
	hours := lcHours(lcHour(0.2, 0), lcHour(0.2, 0), lcHour(0.2, 0))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calm := lcHour(0.2, 0)
	for name, in := range map[string]lcIn{
		"judged":      {hours: hours},
		"out of time": {hours: hours, ctx: ctx},
		"gap":         {hours: []*passageHour{&calm, nil, &calm}},
	} {
		c := lcRun(t, in)
		if len(c.Stretches) == 0 {
			t.Fatalf("%s: no stretches", name)
		}
		for i, s := range c.Stretches {
			if s.Where != "A → B" {
				t.Errorf("%s: stretch %d where = %q", name, i, s.Where)
			}
		}
	}
}

// A sliver folded into the stretch beside it puts its place in the stretch's where,
// because the stretch's span includes it.
func TestStretchesWire_WhereIncludesTheSegmentsOfFoldedSlivers(t *testing.T) {
	// 65 minutes, a degree of latitude for each minute: B is an hour out, 5 minutes from the end.
	plan, tl := wireMeridianPlan(t, paAt(14, 8, 0), 65.0/60, []float64{0, 60, 65})
	in := wireInput(paLoc, nil, true)
	in.plan, in.tl, in.from = plan, tl, paAt(14, 8, 0)
	last := rvHour(60, 65, bandComfortable, "")
	got := in.stretchesWire(stretchesOf([]judgedHour{rvHour(0, 60, bandComfortable, ""), last}))
	if len(got) != 1 || got[0].Where != "A → B then B → C" {
		t.Fatalf("stretches = %+v", got)
	}
	// And one folded ahead of the first hour.
	plan, tl = wireMeridianPlan(t, paAt(14, 8, 0), 65.0/60, []float64{0, 5, 65})
	in.plan, in.tl = plan, tl
	got = in.stretchesWire(stretchesOf([]judgedHour{rvHour(0, 5, bandComfortable, ""), rvHour(5, 65, bandComfortable, "")}))
	if len(got) != 1 || got[0].Where != "A → B then B → C" {
		t.Fatalf("stretches = %+v", got)
	}
}

// wireMeridianPlan is a leg north along one meridian through the named points, a degree of
// latitude for each stated degree, so a point's share of the route is its latitude over the
// last point's. The leg takes hours from depart.
func wireMeridianPlan(t *testing.T, depart time.Time, hours float64, lats []float64) (passageLegPlan, legTimeline) {
	t.Helper()
	var route []passageRoutePoint
	for i, lat := range lats {
		route = append(route, passageRoutePoint{Lat: lat, Lon: 150, Name: string(rune('A' + i))})
	}
	plan := planPassageLeg(0, passageLegArgs{Name: "A → Z", Route: route, PassageHours: &hours})
	tl, err := plan.timeline(depart)
	if err != nil {
		t.Fatal(err)
	}
	return plan, tl
}

// A leg under way names the places from now on, not the one the boat passed before the
// top of the hour it is clipped to.
func TestStretchesWire_WhereOfALegUnderWayStartsAtTheCurrentSegment(t *testing.T) {
	// A 4 hour leg whose first segment is the first 20 minutes.
	plan, tl := wireMeridianPlan(t, paAt(14, 8, 0), 4, []float64{0, 1, 12})
	in := wireInput(paLoc, nil, true)
	in.plan, in.tl, in.underway, in.from = plan, tl, true, paAt(14, 8, 40)
	hours := []judgedHour{in.hourFrom(paAt(14, 8, 0)), in.hourFrom(paAt(14, 9, 0))}
	got := in.stretchesWire(stretchesOf(hours))
	if len(got) != 1 || got[0].Where != "B → C" {
		t.Fatalf("stretches = %+v, want where %q", got, "B → C")
	}
}

// A segment crossed wholly between two hourly stamps is still named, in route order.
func TestStretchesWire_WhereNamesASegmentCrossedBetweenTwoStamps(t *testing.T) {
	// 12 hours, a degree an hour: B → C is 6.1 to 6.4 hours out, 2.06 PM to 2.24 PM.
	plan, tl := wireMeridianPlan(t, paAt(14, 8, 0), 12, []float64{0, 6.1, 6.4, 12})
	in := wireInput(paLoc, nil, true)
	in.plan, in.tl, in.from = plan, tl, paAt(14, 8, 0)
	var hours []judgedHour
	for h := 8; h < 20; h++ {
		hours = append(hours, in.hourFrom(paAt(14, h, 0)))
		hours[len(hours)-1].band = bandComfortable
	}
	got := in.stretchesWire(stretchesOf(hours))
	if len(got) != 1 || got[0].Where != "A → B then B → C then C → D" {
		t.Fatalf("stretches = %+v", got)
	}
}

// A stretch that ends where a segment ends does not name the next segment.
func TestStretchesWire_WhereStopsAtTheSegmentBoundaryAStretchEndsOn(t *testing.T) {
	plan, tl := wireMeridianPlan(t, paAt(14, 8, 0), 4, []float64{0, 2, 4})
	in := wireInput(paLoc, nil, true)
	in.plan, in.tl, in.from = plan, tl, paAt(14, 8, 0)
	hours := []judgedHour{in.hourFrom(paAt(14, 8, 0)), in.hourFrom(paAt(14, 9, 0)), in.hourFrom(paAt(14, 10, 0)), in.hourFrom(paAt(14, 11, 0))}
	hours[0].band, hours[1].band, hours[2].band, hours[3].band = bandComfortable, bandComfortable, bandLively, bandLively
	got := in.stretchesWire(stretchesOf(hours))
	if len(got) != 2 || got[0].Where != "A → B" || got[1].Where != "B → C" {
		t.Fatalf("stretches = %+v", got)
	}
}

// A leg that crosses a zone line tells every label with its day and zone, so the labels
// read in order: the departure's zone up to the last, which is told as the arrival is.
func TestStretchesWire_ACrossZoneLegLabelsEveryTimeWithDayAndZone(t *testing.T) {
	syd, bris := wireZone(t, "Australia/Sydney"), wireZone(t, "Australia/Brisbane")
	start := time.Date(2026, 11, 27, 0, 30, 0, 0, syd)
	a := judgedHour{start: start, end: start.Add(30 * time.Minute), band: bandComfortable}
	b := judgedHour{start: a.end, end: a.end.Add(15 * time.Minute), band: bandLively, lead: comfortLead{Word: "roll"}, figures: "swell 1 m 6 s"}
	got := wireInput(syd, bris, true).stretchesWire(stretchesOf([]judgedHour{a, b}))
	if len(got) != 2 {
		t.Fatalf("stretches = %+v", got)
	}
	want := [][2]string{
		{"Fri 27 Nov 12.30 AM AEDT", "Fri 27 Nov 1.00 AM AEDT"},
		{"Fri 27 Nov 1.00 AM AEDT", "Fri 27 Nov 12.15 AM AEST"},
	}
	for i, w := range want {
		if got[i].FromLabel != w[0] || got[i].UntilLabel != w[1] {
			t.Errorf("stretch %d labels = %q until %q, want %q until %q", i, got[i].FromLabel, got[i].UntilLabel, w[0], w[1])
		}
	}
}

// 5: before the bands are learned the stretch's reason carries the sea beside the
// not-learned words, and the chip's text stays as it was.
func TestStretchesWire_AnUnlearnedStretchCarriesItsSea(t *testing.T) {
	in := wireInput(paLoc, nil, false)
	h := rvHour(0, 60, "", "")
	h.lead = comfortLead{Word: "roll"}
	h.figures = "swell 1 m 6 s"
	h.beyond = true
	hours := []judgedHour{h}
	st := stretchesOf(hours)
	got := in.stretchesWire(st)
	want := comfortNotLearnedReason + ": roll, swell 1 m 6 s, " + comfortBeyondSteep
	if len(got) != 1 || got[0].Reason != want || got[0].Band != "" {
		t.Errorf("stretches = %+v, want reason %q", got, want)
	}
	if text := in.text(hours, st); text != "Comfort not learned yet. Roughest sea: roll, swell 1 m 6 s, "+comfortBeyondSteep+"." {
		t.Errorf("the chip text changed: %q", text)
	}
	flat := rvHour(60, 120, "", "")
	if got := in.stretchesWire(stretchesOf([]judgedHour{flat})); got[0].Reason != comfortNotLearnedReason+": flat" {
		t.Errorf("flat sea reason = %q", got[0].Reason)
	}
	// A gap keeps its own reason.
	gap := rvHour(0, 60, "", "position charted ashore")
	if got := in.stretchesWire(stretchesOf([]judgedHour{gap})); got[0].Reason != "position charted ashore" {
		t.Errorf("gap reason = %q", got[0].Reason)
	}
}
