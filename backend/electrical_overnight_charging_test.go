package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// These tests are built on what the boat showed on 24 to 27 Oct 2026: the
// house bank was charging at +130 to +150 A in the evening, the inverter's
// AC-in power read about 8000 W until 19:30 boat time and then 0, and the
// generator's stateNumber read 0 the whole time. Idle reads exactly 0 W.

func ruleWindowFor(t *testing.T, w nightWindow) nightWindow {
	t.Helper()
	rw, ok := ruleNightWindow(w, mustLocationAt(t, vesselLat, vesselLon))
	if !ok {
		t.Fatalf("no 10 PM window inside %+v", w)
	}
	return rw
}

// nightSeries is a night's charge: flat until 10 PM, then falling at rate
// percent an hour to dawn, so the rate over the 10 PM window is exact.
func nightSeries(t *testing.T, w nightWindow, rate float64) []telemetryPoint {
	t.Helper()
	rw := ruleWindowFor(t, w)
	before := syntheticSlopeSeries(nightWindow{Start: w.Start, End: rw.Start.Add(-overnightSampleInterval)}, overnightSampleInterval, 65, 65)
	after := syntheticSlopeSeries(rw, overnightSampleInterval, 65, 65+rate*rw.End.Sub(rw.Start).Hours())
	return concatPoints(before, after)
}

// inverterPoints reads watts at every sample time from from to to.
func inverterPoints(from, to time.Time, watts float64) []telemetryPoint {
	var out []telemetryPoint
	for at := from; !at.After(to); at = at.Add(overnightSampleInterval) {
		out = append(out, telemetryPoint{Timestamp: at, Value: watts})
	}
	return out
}

func idleInverter(w nightWindow) []telemetryPoint { return inverterPoints(w.Start, w.End, 0) }

func computeForTest(t *testing.T, now time.Time, paths overnightPaths, query func(string, time.Time, time.Time, string) ([]telemetryPoint, error)) overnightResult {
	t.Helper()
	return computeOvernight(now, boatAtForTest(t, vesselLat, vesselLon), vesselLat, vesselLon, paths, true, true, query)
}

// 25 Oct: the generator fed the Multi until 19:30 and its state number never
// left 0. The tile scan has to drop the night; the rule, which starts at
// 10 PM, still rates it.
func TestComputeOvernight_InverterACInExcludesTheNightFromTheTileButNotTheRule(t *testing.T) {
	now, windows := allLookbackWindows(t)
	loc := mustLocationAt(t, vesselLat, vesselLon)
	d := windows[0].Start.In(loc)
	evening := time.Date(d.Year(), d.Month(), d.Day(), 19, 30, 0, 0, loc)
	inverter := concatPoints(
		inverterPoints(windows[0].Start, evening, 8000),
		inverterPoints(evening.Add(overnightSampleInterval), windows[0].End, 0),
		idleInverter(windows[1]), idleInverter(windows[2]), idleInverter(windows[3]), idleInverter(windows[4]),
	)
	soc := concatPoints(
		nightSeries(t, windows[0], -2.52),
		nightSeries(t, windows[1], -1.6), nightSeries(t, windows[2], -1.6),
		nightSeries(t, windows[3], -1.6), nightSeries(t, windows[4], -1.6),
	)

	r := computeForTest(t, now, testOvernightPaths, lookbackQuery(t, soc, nil, inverter))

	if !r.ChargingChecked {
		t.Fatal("charging was checked: the inverter series has data")
	}
	if r.NightsExcludedCharging != 1 || r.NightsUsed != 4 {
		t.Fatalf("tile scan: excluded charging %d, used %d; want 1 and 4", r.NightsExcludedCharging, r.NightsUsed)
	}
	rw := ruleWindowFor(t, windows[0])
	last := r.lastNight()
	if last == nil || !near(last.RatePercentPerHour, -2.52, 0.001) {
		t.Fatalf("the rule rates last night from 10 PM: got %v", last)
	}
	if !last.Start.Equal(rw.Start) || !last.End.Equal(windows[0].End) {
		t.Errorf("rule window: %v to %v, want %v to %v", last.Start, last.End, rw.Start, windows[0].End)
	}
	if len(r.priorRates()) != 4 {
		t.Errorf("prior nights: %v", r.priorRates())
	}
	if got, want := r.RuleNights[0].date(r.Loc), d.Format("2006-01-02"); got != want || r.RuleNights[0].LeftOut != "" {
		t.Errorf("last night: %+v, want date %s and not left out", r.RuleNights[0], want)
	}
	if !r.RuleNights[0].Start.Equal(rw.Start) {
		t.Errorf("rule night window: %+v", r.RuleNights[0])
	}
}

func TestComputeOvernight_ChargingAfterTenPMLeavesTheNightOutOfTheRule(t *testing.T) {
	now, windows := allLookbackWindows(t)
	rw1 := ruleWindowFor(t, windows[1])
	inverter := concatPoints(
		idleInverter(windows[0]),
		idleInverter(windows[1]),
		[]telemetryPoint{{Timestamp: rw1.Start.Add(time.Hour), Value: 8000}},
		idleInverter(windows[2]), idleInverter(windows[3]), idleInverter(windows[4]),
	)
	soc := concatPoints(
		nightSeries(t, windows[0], -2.52), nightSeries(t, windows[1], -1.0), nightSeries(t, windows[2], -1.6),
		nightSeries(t, windows[3], -1.6), nightSeries(t, windows[4], -1.6),
	)

	r := computeForTest(t, now, testOvernightPaths, lookbackQuery(t, soc, nil, inverter))

	if len(r.priorRates()) != 3 {
		t.Fatalf("prior nights: %v, want the three clean ones", r.priorRates())
	}
	if r.RuleNights[1].LeftOut != leftOutCharging || r.RuleNights[1].RatePercentPerHour != nil {
		t.Errorf("night 2: %+v, want left out for charging", r.RuleNights[1])
	}
}

func TestComputeOvernight_RuleNightsNameWhyEachWasLeftOut(t *testing.T) {
	now, windows := allLookbackWindows(t)
	rw1 := ruleWindowFor(t, windows[1])
	shore := []telemetryPoint{{Timestamp: rw1.Start.Add(time.Hour), Value: 12}}
	soc := concatPoints(
		nightSeries(t, windows[0], -2.0),
		nightSeries(t, windows[1], -1.0),
		nightSeries(t, windows[2], -1.0)[:3],                              // a gap in the data
		syntheticSlopeSeries(windows[3], overnightSampleInterval, 40, 50), // the charge rose
		nightSeries(t, windows[4], -1.0),
	)
	r := computeForTest(t, now, testOvernightPaths, lookbackQuery(t, soc, shore, idleInverter(windows[0])))

	want := map[int]string{1: leftOutShore, 2: leftOutGap, 3: leftOutRise}
	for i, reason := range want {
		if r.RuleNights[i].LeftOut != reason {
			t.Errorf("night %d: left out %q, want %q", i, r.RuleNights[i].LeftOut, reason)
		}
	}
}

func TestComputeOvernight_AnInverterBelowTheThresholdIsNotCharging(t *testing.T) {
	now, windows := allLookbackWindows(t)
	hum := concatPoints(
		inverterPoints(windows[0].Start, windows[0].End, inverterChargingPresentWatts-60),
		inverterPoints(windows[1].Start, windows[1].End, inverterChargingPresentWatts-60),
		inverterPoints(windows[2].Start, windows[2].End, inverterChargingPresentWatts-60),
	)
	soc := concatPoints(nightSeries(t, windows[0], -1.0), nightSeries(t, windows[1], -1.0), nightSeries(t, windows[2], -1.0))

	r := computeForTest(t, now, testOvernightPaths, lookbackQuery(t, soc, nil, hum))

	if r.NightsExcludedCharging != 0 || r.NightsUsed != 3 {
		t.Fatalf("40 W is sensor noise, not charging: excluded %d used %d", r.NightsExcludedCharging, r.NightsUsed)
	}
}

func TestComputeOvernight_NoInverterDataMeansChargingWasNotChecked(t *testing.T) {
	now, windows := allLookbackWindows(t)
	soc := concatPoints(nightSeries(t, windows[0], -1.0), nightSeries(t, windows[1], -1.0))

	// An inverter is chosen but the history holds nothing for it.
	r := computeForTest(t, now, testOvernightPaths, lookbackQuery(t, soc, nil, nil))
	if r.ChargingChecked {
		t.Error("an empty inverter series is not a check")
	}
	// No inverter is chosen at all.
	noInverter := overnightPaths{Soc: defaultSocMeasurement}
	r = computeForTest(t, now, noInverter, lookbackQuery(t, soc, nil, nil))
	if r.ChargingChecked || r.NightsExcludedCharging != 0 {
		t.Errorf("no inverter role: %+v", r)
	}
}

// Charging is judged night by night: a night the inverter logged nothing for
// is not a clean night, whatever the other nights show.
func TestComputeOvernight_ANightWithoutInverterSamplesIsLeftOutAsUnchecked(t *testing.T) {
	now, windows := allLookbackWindows(t)
	soc := concatPoints(nightSeries(t, windows[0], -1.0), nightSeries(t, windows[1], -1.0), nightSeries(t, windows[2], -1.0))
	// The inverter logged night 0 only.
	r := computeForTest(t, now, testOvernightPaths, lookbackQuery(t, soc, nil, idleInverter(windows[0])))

	if !r.ChargingChecked {
		t.Fatal("the inverter has data, so charging was checked for the nights it covers")
	}
	// Nights 1 and 2 have a clean slope but no inverter samples; every older
	// night has neither SoC nor inverter data, so it is a gap, not unchecked.
	if r.NightsUsed != 1 || r.NightsExcludedChargingUnchecked != 2 {
		t.Errorf("tile scan: used %d, unchecked %d; want 1 and 2", r.NightsUsed, r.NightsExcludedChargingUnchecked)
	}
	if r.RuleNights[0].LeftOut != "" || r.RuleNights[1].LeftOut != leftOutChargingUnchecked || r.RuleNights[2].LeftOut != leftOutChargingUnchecked {
		t.Errorf("rule nights: %+v", r.RuleNights[:3])
	}
}

func TestRuleNightWindow_RunsFromTenPMToTheNightsDawnEnd(t *testing.T) {
	loc := mustLocationAt(t, vesselLat, vesselLon)
	w := nightWindow{Start: time.Date(2026, 10, 27, 18, 10, 0, 0, loc), End: time.Date(2026, 10, 28, 5, 5, 0, 0, loc)}
	rw, ok := ruleNightWindow(w, loc)
	if !ok || !rw.Start.Equal(time.Date(2026, 10, 27, 22, 0, 0, 0, loc)) || !rw.End.Equal(w.End) {
		t.Fatalf("got %+v %v", rw, ok)
	}
	// A night that starts after 10 PM keeps its own start.
	late := nightWindow{Start: time.Date(2026, 12, 21, 22, 30, 0, 0, loc), End: time.Date(2026, 12, 22, 4, 0, 0, 0, loc)}
	if rw, ok := ruleNightWindow(late, loc); !ok || !rw.Start.Equal(late.Start) {
		t.Errorf("late sunset: %+v %v", rw, ok)
	}
	// A window that opens after midnight (a high-latitude summer sunset) belongs
	// to the evening before: it keeps its own start rather than meeting 10 PM
	// of the day it opened on, which would drop it.
	small := nightWindow{Start: time.Date(2026, 6, 22, 0, 10, 0, 0, loc), End: time.Date(2026, 6, 22, 2, 40, 0, 0, loc)}
	if rw, ok := ruleNightWindow(small, loc); !ok || !rw.Start.Equal(small.Start) || !rw.End.Equal(small.End) {
		t.Errorf("a 00:10 start: %+v %v", rw, ok)
	}
	// A night that ends before 10 PM has no rule window.
	if _, ok := ruleNightWindow(nightWindow{Start: time.Date(2026, 6, 1, 17, 0, 0, 0, loc), End: time.Date(2026, 6, 1, 21, 0, 0, 0, loc)}, loc); ok {
		t.Error("a window that ends before 10 PM has nothing to measure")
	}
}

func TestHeaviestStretch(t *testing.T) {
	loc := mustLocationAt(t, vesselLat, vesselLon)
	w := nightWindow{Start: time.Date(2026, 10, 27, 22, 0, 0, 0, loc), End: time.Date(2026, 10, 28, 5, 0, 0, 0, loc)}
	series := func(fallByHour func(h int) float64) []telemetryPoint {
		var pts []telemetryPoint
		v := 80.0
		for at, h := w.Start, 0; !at.After(w.End); at, h = at.Add(time.Hour), h+1 {
			pts = append(pts, telemetryPoint{Timestamp: at, Value: v})
			v -= fallByHour(h)
		}
		return pts
	}

	// 5 points an hour for the first two hours, 1 an hour after: 10 of 15.
	front := series(func(h int) float64 {
		if h < 2 {
			return 5
		}
		return 1
	})
	got, ok := heaviestStretch(front, w)
	if !ok || !got.Start.Equal(w.Start) || !got.End.Equal(w.Start.Add(2*time.Hour)) {
		t.Fatalf("front-loaded night: %+v %v", got, ok)
	}

	// An even drain has no heaviest stretch worth naming.
	if _, ok := heaviestStretch(series(func(int) float64 { return 2 }), w); ok {
		t.Error("an even drain must not name a stretch")
	}
	// Two hours holding exactly half is not "most".
	half := series(func(h int) float64 {
		if h < 2 {
			return 2.5 // 5 of the 10 points the night falls
		}
		return 1
	})
	if _, ok := heaviestStretch(half, w); ok {
		t.Error("exactly half is not more than half")
	}
	// A night that did not fall has nothing to name.
	if _, ok := heaviestStretch(series(func(int) float64 { return -1 }), w); ok {
		t.Error("a rising night has no heaviest stretch")
	}
}

func TestHeaviestStretch_ALastStretchUnderAnHourJoinsTheOneBefore(t *testing.T) {
	loc := mustLocationAt(t, vesselLat, vesselLon)
	w := nightWindow{Start: time.Date(2026, 10, 27, 22, 0, 0, 0, loc), End: time.Date(2026, 10, 28, 4, 30, 0, 0, loc)}
	var pts []telemetryPoint
	v := 80.0
	for at := w.Start; !at.After(w.End); at = at.Add(30 * time.Minute) {
		pts = append(pts, telemetryPoint{Timestamp: at, Value: v})
		if at.After(w.End.Add(-3 * time.Hour)) {
			v -= 3 // the last three hours fall hardest
		} else {
			v -= 0.1
		}
	}
	got, ok := heaviestStretch(pts, w)
	if !ok || !got.End.Equal(w.End) {
		t.Fatalf("the stretch should run to dawn: %+v %v", got, ok)
	}
}

func drawFigures(t *testing.T) overnightDrawFigures {
	t.Helper()
	loc := mustLocationAt(t, vesselLat, vesselLon)
	w := nightWindow{Start: time.Date(2026, 10, 27, 22, 0, 0, 0, loc), End: time.Date(2026, 10, 28, 5, 20, 0, 0, loc)}
	heavy := nightWindow{Start: w.Start, End: w.Start.Add(2 * time.Hour)}
	return overnightDrawFigures{
		Last: 2.52, Typical: 1.61, Ratio: 1.57, Compared: 6, Window: w, Loc: loc,
		Heaviest: &heavy, LeftOutCharging: 2, ChargingChecked: true,
	}
}

func TestOvernightDrawSummary_ShowsTheWorking(t *testing.T) {
	want := "Between 10.00 PM and dawn the house bank drained at 2.52 % of charge an hour, 1.6 times the usual 1.61 %, compared over 6 nights. " +
		"Most of it fell between 10.00 PM and 12.00 AM. 2 nights with charging in those hours were left out."
	if got := drawFigures(t).summary(); got != want {
		t.Fatalf("summary:\n got %s\nwant %s", got, want)
	}
}

func TestOvernightDrawSummary_Variants(t *testing.T) {
	f := drawFigures(t)
	f.Heaviest = nil
	f.LeftOutCharging = 0
	got := f.summary()
	if strings.Contains(got, "Most of it") || strings.Contains(got, "left out") {
		t.Errorf("an even drain with nothing left out has neither sentence: %s", got)
	}

	f.LeftOutCharging = 1
	if got := f.summary(); !strings.Contains(got, "1 night with charging in those hours was left out.") {
		t.Errorf("singular: %s", got)
	}

	// A stretch that runs to dawn says dawn.
	f = drawFigures(t)
	f.Heaviest = &nightWindow{Start: f.Window.End.Add(-2 * time.Hour), End: f.Window.End}
	if got := f.summary(); !strings.Contains(got, "Most of it fell between 3.20 AM and dawn.") {
		t.Errorf("dawn end: %s", got)
	}

	// Charging that could not be checked is said plainly, and no cause is offered.
	f = drawFigures(t)
	f.ChargingChecked = false
	f.LeftOutCharging = 0
	got = f.summary()
	if !strings.Contains(got, "Charging was not checked") {
		t.Errorf("not checked: %s", got)
	}
	for _, banned := range []string{"because", "probably", "likely", "caused"} {
		if strings.Contains(strings.ToLower(got), banned) {
			t.Errorf("summary offers a cause (%q): %s", banned, got)
		}
	}
}

func TestOvernightDrawSummary_ShoreNightsAreCountedApartFromCharging(t *testing.T) {
	cases := []struct {
		name            string
		charging, shore int
		checked         bool
		want            string // the sentence that must appear
	}{
		{"both", 2, 3, true, " 2 nights with charging and 3 on shore power in those hours were left out."},
		{"one each", 1, 1, true, " 1 night with charging and 1 on shore power in those hours were left out."},
		{"shore only", 0, 3, true, " 3 nights on shore power in those hours were left out."},
		{"one shore night", 0, 1, true, " 1 night on shore power in those hours was left out."},
		{"charging only", 2, 0, true, " 2 nights with charging in those hours were left out."},
		{"shore beside not checked", 0, 3, false, "Charging was not checked, so a night with charging may be in the comparison. 3 nights on shore power in those hours were left out."},
	}
	for _, c := range cases {
		f := drawFigures(t)
		f.LeftOutCharging, f.LeftOutShore, f.ChargingChecked = c.charging, c.shore, c.checked
		got := f.summary()
		if !strings.Contains(got, c.want) {
			t.Errorf("%s:\n got %s\nwant it to contain %q", c.name, got, c.want)
		}
		if c.shore > 0 && strings.Contains(got, "with charging and") && c.charging == 0 {
			t.Errorf("%s: shore nights are not charging nights: %s", c.name, got)
		}
	}
}

func TestOvernightDrawObservation_CountsShoreNightsApartFromChargingNights(t *testing.T) {
	r := liveNightsResult(t)
	r.RuleNights[3].LeftOut = leftOutShore // 24 Oct was on shore power; 25 Oct stays charging
	o := overnightDrawObservation(r)
	if o == nil || !strings.Contains(o.Summary, "1 night with charging and 1 on shore power in those hours were left out.") {
		t.Fatalf("summary: %+v", o)
	}
}

func ruleNightFor(t *testing.T, loc *time.Location, day int, rate float64, leftOut string) overnightRuleNight {
	t.Helper()
	start := time.Date(2026, 10, day, 22, 0, 0, 0, loc)
	n := overnightRuleNight{Start: start, End: start.Add(7 * time.Hour), Latest: day == 27, LeftOut: leftOut}
	if leftOut == "" {
		n.RatePercentPerHour = &rate
	}
	return n
}

func liveNightsResult(t *testing.T) overnightResult {
	t.Helper()
	loc := mustLocationAt(t, vesselLat, vesselLon)
	last := -2.52
	nights := []overnightRuleNight{
		ruleNightFor(t, loc, 27, last, ""),
		ruleNightFor(t, loc, 26, -1.7, ""),
		ruleNightFor(t, loc, 25, 0, leftOutCharging),
		ruleNightFor(t, loc, 24, 0, leftOutCharging),
		ruleNightFor(t, loc, 23, -1.5, ""),
		ruleNightFor(t, loc, 22, -1.61, ""),
		ruleNightFor(t, loc, 21, 0, leftOutGap),
		ruleNightFor(t, loc, 20, -1.61, ""),
		ruleNightFor(t, loc, 19, -1.62, ""),
		ruleNightFor(t, loc, 18, -1.6, ""),
	}
	heavy := nightWindow{Start: nights[0].Start, End: nights[0].Start.Add(2 * time.Hour)}
	return overnightResult{
		Basis: "history", SocPath: "electrical.batteries.512.capacity.stateOfCharge", Loc: loc, ChargingChecked: true,
		RuleNights: nights, LastNightHeaviest: &heavy,
	}
}

func TestOvernightDrawObservation_CarriesTheNightsComparedAndLeftOut(t *testing.T) {
	r := liveNightsResult(t)
	o := overnightDrawObservation(r)
	if o == nil {
		t.Fatal("expected an observation")
	}
	if !strings.HasPrefix(o.Summary, "Between 10.00 PM and dawn the house bank drained at 2.52 %") ||
		!strings.Contains(o.Summary, "2 nights with charging in those hours were left out.") {
		t.Errorf("summary: %s", o.Summary)
	}
	if o.Window != "10.00 PM to dawn, boat time" || !o.ChargingChecked || o.LastNightDate != "2026-10-27" {
		t.Errorf("window %q checked %v date %q", o.Window, o.ChargingChecked, o.LastNightDate)
	}
	if !o.nightStart.Equal(r.lastNight().Start) || !o.nightEnd.Equal(r.lastNight().End) {
		t.Errorf("the chart covers the rule window: %v to %v", o.nightStart, o.nightEnd)
	}
	if len(o.Nights) != 9 {
		t.Fatalf("nights: %d, want the 9 before last night", len(o.Nights))
	}
	byDate := map[string]observationNight{}
	for _, n := range o.Nights {
		byDate[n.Date] = n
	}
	if n := byDate["2026-10-25"]; n.LeftOut != "charging" || n.DrainPercentPerHour != nil {
		t.Errorf("25 Oct: %+v", n)
	}
	if n := byDate["2026-10-21"]; n.LeftOut != "a gap in the data" {
		t.Errorf("21 Oct: %+v", n)
	}
	if n := byDate["2026-10-26"]; n.LeftOut != "" || n.DrainPercentPerHour == nil || *n.DrainPercentPerHour != 1.7 {
		t.Errorf("26 Oct: %+v", n)
	}
	for _, n := range o.Nights {
		if n.Place != placeNotKnown {
			t.Errorf("no logbook is wired, so the place is %q: %+v", placeNotKnown, n)
		}
	}
	raw, _ := json.Marshal(o)
	for _, key := range []string{`"nights"`, `"left_out_because"`, `"charging_checked":true`, `"last_night_place"`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("observation JSON lacks %s: %s", key, raw)
		}
	}
}

func TestBoatPlaceForNight(t *testing.T) {
	start := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	end := start.Add(10 * time.Hour)
	at := func(h int) time.Time { return start.Add(time.Duration(h) * time.Hour) }
	arrived := passageRecord{Start: at(-30), End: at(-26), FromName: "Airlie", ToName: "Cid Harbour"}

	cases := []struct {
		name   string
		ps     []passageRecord
		hidden [][2]time.Time
		want   string
	}{
		{"no logbook", nil, nil, placeNotKnown},
		{"arrived the day before", []passageRecord{arrived}, nil, "Cid Harbour"},
		{"left again after dawn", []passageRecord{arrived, {Start: at(14), End: at(18), ToName: "Hamilton"}}, nil, "Cid Harbour"},
		{"under way in the night", []passageRecord{arrived, {Start: at(4), End: at(9), FromName: "Cid Harbour", ToName: "Hamilton"}}, nil, "under way on a passage"},
		{"arrival has no name", []passageRecord{{Start: at(-30), End: at(-26)}}, nil, placeNotKnown},
		{"a hidden move since", []passageRecord{arrived}, [][2]time.Time{{at(-20), at(-19)}}, placeNotKnown},
	}
	for _, c := range cases {
		if got := boatPlaceForNight(c.ps, c.hidden, nil, start, end); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}

	// A passage still under way is not in the logbook yet; it reads as under
	// way for any night it began before the end of.
	open := func(h int) *logbookOpen { return &logbookOpen{start: at(h)} }
	if got := boatPlaceForNight([]passageRecord{arrived}, nil, open(3), start, end); got != "under way on a passage" {
		t.Errorf("open run began mid-night: got %q", got)
	}
	if got := boatPlaceForNight([]passageRecord{arrived}, nil, open(-3), start, end); got != "under way on a passage" {
		t.Errorf("open run began before the night: got %q", got)
	}
	if got := boatPlaceForNight([]passageRecord{arrived}, nil, open(12), start, end); got != "Cid Harbour" {
		t.Errorf("open run began after the night: got %q", got)
	}
}

func TestVesselObservations_ANightOverlappingAnOpenPassageIsUnderWay(t *testing.T) {
	r := liveNightsResult(t)
	s := newTestPassageStore(t)
	for _, p := range newTrackBuilder().run(90*time.Minute, time.Minute, 6).pts {
		if err := s.AppendTrackPoint(p); err != nil {
			t.Fatal(err)
		}
	}
	d := statusTestDeps(t)
	d.overnight = func(time.Time) (overnightResult, string) { return r, "" }
	d.passageLog = func() *passageStore { return s }

	obs, unchecked := d.vesselObservationsFor(vesselStatusTestNow)
	if len(obs) != 1 || len(unchecked) != 0 {
		t.Fatalf("obs=%+v unchecked=%v", obs, unchecked)
	}
	if obs[0].LastNightPlace != "under way on a passage" {
		t.Errorf("last night place: %q", obs[0].LastNightPlace)
	}
}

func TestVesselObservations_AnUnreadableLogbookIsSurfacedAndTheFindingKept(t *testing.T) {
	r := liveNightsResult(t)
	s := newTestPassageStore(t)
	if err := s.close(); err != nil {
		t.Fatal(err)
	}
	d := statusTestDeps(t)
	d.overnight = func(time.Time) (overnightResult, string) { return r, "" }
	d.passageLog = func() *passageStore { return s }

	obs, unchecked := d.vesselObservationsFor(vesselStatusTestNow)
	if len(obs) != 1 || obs[0].LastNightPlace != placeNotKnown {
		t.Fatalf("the finding stays, with the place not known: %+v", obs)
	}
	if len(unchecked) != 1 || !strings.Contains(unchecked[0], "logbook could not be read") {
		t.Errorf("unchecked: %v", unchecked)
	}

	// An absent logbook is not an error: the places are just not known.
	d.passageLog = nil
	obs, unchecked = d.vesselObservationsFor(vesselStatusTestNow)
	if len(obs) != 1 || len(unchecked) != 0 {
		t.Errorf("no logbook: obs=%d unchecked=%v", len(obs), unchecked)
	}
	d.passageLog = func() *passageStore { return nil }
	if _, unchecked = d.vesselObservationsFor(vesselStatusTestNow); len(unchecked) != 0 {
		t.Errorf("a nil store: unchecked=%v", unchecked)
	}
}

func TestVesselObservations_NightsNameTheLogbookPlace(t *testing.T) {
	r := liveNightsResult(t)
	s := newTestPassageStore(t)
	// The boat arrived at Cid Harbour on 20 Oct and stayed.
	arrive := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	if _, err := s.InsertPassage(passageRecord{Start: arrive.Add(-4 * time.Hour), End: arrive, FromName: "Airlie", ToName: "Cid Harbour"}); err != nil {
		t.Fatal(err)
	}
	d := statusTestDeps(t)
	d.overnight = func(time.Time) (overnightResult, string) { return r, "" }
	d.passageLog = func() *passageStore { return s }

	obs, _ := d.vesselObservationsFor(vesselStatusTestNow)
	if len(obs) != 1 {
		t.Fatalf("observations: %+v", obs)
	}
	if obs[0].LastNightPlace != "Cid Harbour" {
		t.Errorf("last night place: %q", obs[0].LastNightPlace)
	}
	for _, n := range obs[0].Nights {
		want := "Cid Harbour"
		if n.Date == "2026-10-18" || n.Date == "2026-10-19" {
			want = placeNotKnown // before the arrival the logbook has nothing
		}
		if n.Place != want {
			t.Errorf("%s: place %q, want %q", n.Date, n.Place, want)
		}
	}
}

func TestVesselObservations_AnUncheckedChargingNightListSaysSo(t *testing.T) {
	r := liveNightsResult(t)
	r.ChargingChecked = false
	o := overnightDrawObservation(r)
	if o == nil || o.ChargingChecked || !strings.Contains(o.Summary, "Charging was not checked") {
		t.Fatalf("observation: %+v", o)
	}
}

func TestWatchPowerSection_GivesTheReviewTheNights(t *testing.T) {
	r := liveNightsResult(t)
	o := overnightDrawObservation(r)
	in := watchReportInput{Status: &vesselStatusResult{Observations: []vesselObservation{*o}}}
	section, findings := watchPowerSection(savedWatch{}, in)
	if len(findings) != 1 || findings[0].Rule != "overnight_draw" {
		t.Fatalf("findings: %+v", findings)
	}
	raw, _ := json.Marshal(section.Figures)
	if !strings.Contains(string(raw), "overnight_draw_nights") || !strings.Contains(string(raw), "left_out_because") {
		t.Errorf("the review needs the nights: %s", raw)
	}
}

func TestPromptsTellMateToCheckAnExplanationAgainstTheNights(t *testing.T) {
	review := buildWatchReviewPrompt(assistantPromptContext{}, "metric", watchTestTime(29, 6, 0), time.UTC, false)
	chat := buildAssistantSystemPrompt(assistantPromptContext{})
	for name, prompt := range map[string]string{"review": review, "chat": chat} {
		for _, want := range []string{
			"overnight draw",
			"against the nights compared",
			"does not explain the figures",
			"unless the left-out list or the data shows it",
		} {
			if !strings.Contains(prompt, want) {
				t.Errorf("%s prompt lacks %q", name, want)
			}
		}
	}
}

// A boat that runs the generator most evenings has almost no clean whole
// nights, so the tile's scan finds no history. The rule measures from 10 PM and
// has to stand on its own nights.
func TestVesselObservations_TheRuleDoesNotNeedTheTilesHistoryBasis(t *testing.T) {
	now, windows := allLookbackWindows(t)
	loc := mustLocationAt(t, vesselLat, vesselLon)
	var soc, inverter []telemetryPoint
	for i := 0; i < 8; i++ {
		rate := -1.0
		if i == 0 {
			rate = -2.5
		}
		soc = concatPoints(soc, nightSeries(t, windows[i], rate))
		d := windows[i].Start.In(loc)
		evening := time.Date(d.Year(), d.Month(), d.Day(), 19, 30, 0, 0, loc)
		inverter = concatPoints(inverter,
			inverterPoints(windows[i].Start, evening, 8000),
			inverterPoints(evening.Add(overnightSampleInterval), windows[i].End, 0))
	}
	r := computeForTest(t, now, testOvernightPaths, lookbackQuery(t, soc, nil, inverter))
	if r.Basis == "history" {
		t.Fatalf("fixture: the whole-night scan should find no history, got %+v", r)
	}
	if len(r.priorRates()) < observationMinPriorNights {
		t.Fatalf("fixture: the rule should hold clean nights: %+v", r.RuleNights)
	}

	d := statusTestDeps(t)
	d.overnight = func(time.Time) (overnightResult, string) { return r, "" }
	obs, unchecked := d.vesselObservationsFor(vesselStatusTestNow)
	if len(obs) != 1 || len(unchecked) != 0 {
		t.Fatalf("the rule should fire on its own nights: obs=%+v unchecked=%v", obs, unchecked)
	}
}

func TestVesselObservations_NoRuleNightsAtAllReportsTheReason(t *testing.T) {
	reason := "influxdb not configured"
	d := statusTestDeps(t)
	d.overnight = func(time.Time) (overnightResult, string) {
		return overnightResult{Basis: "none", Reason: &reason}, ""
	}
	obs, unchecked := d.vesselObservationsFor(vesselStatusTestNow)
	if len(obs) != 0 || len(unchecked) != 1 || !strings.Contains(unchecked[0], reason) {
		t.Fatalf("obs=%v unchecked=%v", obs, unchecked)
	}
}
