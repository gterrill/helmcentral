package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── helpers ─────────────────────────────────────────────────────────────

var pwLoc = vesselLocalLocation(147)

func pwF(v float64) *float64 { return &v }

func pwDay(day, hour int) time.Time {
	return time.Date(2026, time.October, day, hour, 0, 0, 0, pwLoc)
}

var pwNow = pwDay(19, 9)

func kn(v float64) float64 { return v / metersPerSecondToKnots }

// pwUniform is the same weather at every point for every hour in [from, to].
func pwUniform(windKn, fromDeg, gustKn float64, waveM *float64, waveFrom float64, from, to time.Time) forecastSource {
	hours := map[int64]passageHour{}
	for t := from; !t.After(to); t = t.Add(time.Hour) {
		h := passageHour{WindMS: kn(windKn), GustMS: kn(gustKn), WindDirDeg: fromDeg}
		if waveM != nil {
			h.HasWave, h.WaveM, h.WaveDirDeg, h.HasWaveDir = true, *waveM, waveFrom, true
		}
		hours[t.Unix()] = h
	}
	return forecastSource{Provider: "test-wx", WaveProvider: "test-waves", Fetch: func(_ context.Context, lat, lon float64) (passageForecast, error) {
		return passageForecast{Hours: hours}, nil
	}}
}

func pwWeek(windKn, fromDeg float64) forecastSource {
	return pwUniform(windKn, fromDeg, windKn, nil, 0, pwDay(19, 0), pwDay(25, 23))
}

func pwLimitsAll() passageLimits {
	return passageLimits{
		Ahead:  passageSectorLimits{WindKnNoGo: pwF(20)},
		Abeam:  passageSectorLimits{WindKnNoGo: pwF(30)},
		Astern: passageSectorLimits{WindKnNoGo: pwF(35)},
	}
}

func pwNoSun(date time.Time, lat, lon float64) (time.Time, time.Time, bool) {
	return time.Time{}, time.Time{}, false
}

func pwNoTides(_ context.Context, lat, lon float64) ([]tideExtremePoint, error) {
	return nil, fmt.Errorf("no tide station")
}

// pwNorthLeg is 60 NM due north, Alpha to Bravo.
func pwNorthLeg() passageLegArgs {
	return passageLegArgs{Route: []passageRoutePoint{{Lat: -19, Lon: 147, Name: "Alpha"}, {Lat: -18, Lon: 147, Name: "Bravo"}}, SpeedKn: pwF(6)}
}

func pwArgs(dates []string, depart string, legs ...passageLegArgs) passageWindowArgs {
	a := passageWindowArgs{Legs: legs, DepartureDates: dates, DepartTime: depart}
	if len(legs) > 1 {
		a.StaysNights = make([]int, len(legs)-1)
	}
	return a
}

func pwAssess(t *testing.T, a passageWindowArgs, l passageLimits, fc forecastSource) passageWindowPayload {
	t.Helper()
	p, err := assessItinerary(context.Background(), a, l, pwNow, fc, pwNoTides, pwNoSun)
	if err != nil {
		t.Fatalf("assessItinerary: %v", err)
	}
	return p
}

func pwCell0(t *testing.T, p passageWindowPayload) passageWindowCell {
	t.Helper()
	if len(p.Rows) == 0 || len(p.Rows[0].Cells) == 0 {
		t.Fatalf("no cells in %+v", p)
	}
	return p.Rows[0].Cells[0]
}

func pwHas(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// ── sectors ─────────────────────────────────────────────────────────────

func TestAssessItinerary_GoCellShowsItsWorstFigure(t *testing.T) {
	a := pwArgs([]string{"2026-10-20"}, "06:00", pwNorthLeg())
	c := pwCell0(t, pwAssess(t, a, pwLimitsAll(), pwWeek(5, 180)))
	if c.State != "go" || c.Limiting == nil {
		t.Fatalf("a light-air go cell must still say what it was judged on, got %+v", c)
	}
	if want := "Wind S 5 kn astern · Alpha → Bravo"; c.Limiting.Text != want {
		t.Fatalf("text = %q, want %q", c.Limiting.Text, want)
	}
}

func TestAssessItinerary_SameWindPassesAsternAndFailsAhead(t *testing.T) {
	a := pwArgs([]string{"2026-10-20"}, "06:00", pwNorthLeg())

	ahead := pwCell0(t, pwAssess(t, a, pwLimitsAll(), pwWeek(25, 0)))
	if ahead.State != "no_go" || ahead.Limiting == nil || ahead.Limiting.Factor != "wind" {
		t.Fatalf("25 kn on the nose must be no_go on wind, got %+v", ahead)
	}
	if ahead.Limiting.Sector == nil || *ahead.Limiting.Sector != "ahead" {
		t.Fatalf("sector = %v, want ahead", ahead.Limiting.Sector)
	}
	if want := "Wind N 25 kn ahead · Alpha → Bravo"; ahead.Limiting.Text != want {
		t.Fatalf("text = %q, want %q", ahead.Limiting.Text, want)
	}

	astern := pwCell0(t, pwAssess(t, a, pwLimitsAll(), pwWeek(24, 180)))
	if astern.State != "go" {
		t.Fatalf("24 kn from astern must be go, got %+v", astern)
	}
}

func TestAssessItinerary_ViaAtAHeadlandChangesSectorMidLeg(t *testing.T) {
	// North 60 NM, then east. Wind from the east: abeam on the first segment,
	// dead ahead on the second.
	leg := passageLegArgs{
		Route:   []passageRoutePoint{{Lat: -19, Lon: 147, Name: "Alpha"}, {Lat: -18, Lon: 147, Name: "Cape Via"}, {Lat: -18, Lon: 148, Name: "Bravo"}},
		SpeedKn: pwF(6),
	}
	l := pwLimitsAll()
	l.Abeam.WindKnNoGo = pwF(40)
	c := pwCell0(t, pwAssess(t, pwArgs([]string{"2026-10-20"}, "06:00", leg), l, pwWeek(25, 90)))
	if c.State != "no_go" || c.Limiting == nil {
		t.Fatalf("the eastbound segment is on the nose, got %+v", c)
	}
	if c.Limiting.Sector == nil || *c.Limiting.Sector != "ahead" {
		t.Fatalf("sector = %v, want ahead", c.Limiting.Sector)
	}
	if want := "Wind E 25 kn ahead · Cape Via → Bravo"; c.Limiting.Text != want {
		t.Fatalf("text = %q, want %q", c.Limiting.Text, want)
	}
}

func TestAssessItinerary_UnnamedPointsFallBackToTheLegName(t *testing.T) {
	leg := passageLegArgs{Route: []passageRoutePoint{{Lat: -19, Lon: 147, Name: "Alpha"}, {Lat: -18, Lon: 147}}, SpeedKn: pwF(6)}
	p := pwAssess(t, pwArgs([]string{"2026-10-20"}, "06:00", leg), pwLimitsAll(), pwWeek(25, 0))
	if p.Legs[0].Name != "Leg 1" {
		t.Fatalf("leg name = %q, want Leg 1", p.Legs[0].Name)
	}
	c := pwCell0(t, p)
	if c.Limiting == nil || !strings.HasSuffix(c.Limiting.Text, "· "+p.Legs[0].Name) {
		t.Fatalf("an unnamed segment point falls back to the leg name, got %+v", c.Limiting)
	}
}

// ── scheduling ──────────────────────────────────────────────────────────

func pwTwoLegs(second string) passageWindowArgs {
	l0 := passageLegArgs{Route: []passageRoutePoint{{Lat: -19, Lon: 147, Name: "Alpha"}, {Lat: -18, Lon: 147, Name: "Bravo"}}, PassageHours: pwF(10)}
	l1 := passageLegArgs{Route: []passageRoutePoint{{Lat: -18, Lon: 147, Name: "Bravo"}, {Lat: -17, Lon: 147, Name: "Charlie"}}, PassageHours: pwF(10), DepartTime: second}
	return pwArgs([]string{"2026-10-22"}, "06:00", l0, l1)
}

func TestAssessItinerary_ThursdayPlusTwoNightsIsSaturday(t *testing.T) {
	a := pwTwoLegs("")
	a.StaysNights = []int{2}
	p := pwAssess(t, a, pwLimitsAll(), pwWeek(5, 0))
	cells := p.Rows[0].Cells
	if cells[0].Date != "2026-10-22" || cells[0].Depart != "2026-10-22T06:00:00+10:00" || cells[0].Arrive != "2026-10-22T16:00:00+10:00" {
		t.Fatalf("leg 0 = %+v", cells[0])
	}
	if cells[1].Date != "2026-10-24" || cells[1].Depart != "2026-10-24T06:00:00+10:00" {
		t.Fatalf("leg 1 should leave Saturday 06:00, got %+v", cells[1])
	}
	if p.Legs[0].StayNightsAfter == nil || *p.Legs[0].StayNightsAfter != 2 || p.Legs[1].StayNightsAfter != nil {
		t.Fatalf("stays = %+v", p.Legs)
	}
	if p.Timezone != vesselLocalTimezoneName(147) || p.ForecastFetchedAt != "2026-10-19T09:00:00+10:00" || p.Provider != "test-wx" || p.WaveProvider != "test-waves" {
		t.Fatalf("header = %+v", p)
	}
}

func TestAssessItinerary_ZeroNightsLeavesSameDateOnlyIfLaterThanArrival(t *testing.T) {
	for _, tc := range []struct{ depart, wantDate string }{
		{"18:00", "2026-10-22"},
		{"16:00", "2026-10-23"}, // exactly at arrival is not later
		{"06:00", "2026-10-23"},
	} {
		a := pwTwoLegs(tc.depart)
		p := pwAssess(t, a, pwLimitsAll(), pwWeek(5, 0))
		if got := p.Rows[0].Cells[1].Date; got != tc.wantDate {
			t.Errorf("leg 1 depart_time %s: date = %s, want %s", tc.depart, got, tc.wantDate)
		}
	}
}

// ── daylight ────────────────────────────────────────────────────────────

func pwSun(date time.Time, lat, lon float64) (time.Time, time.Time, bool) {
	y, m, d := date.Date()
	return time.Date(y, m, d, 5, 40, 0, 0, date.Location()), time.Date(y, m, d, 17, 32, 0, 0, date.Location()), true
}

func TestAssessItinerary_ArrivalAfterSunsetIsNoGoWhenRequired(t *testing.T) {
	l := pwLimitsAll()
	l.DaylightArrival = true
	a := pwArgs([]string{"2026-10-20"}, "12:00", pwNorthLeg())
	p, err := assessItinerary(context.Background(), a, l, pwNow, pwWeek(5, 180), pwNoTides, pwSun)
	if err != nil {
		t.Fatal(err)
	}
	c := pwCell0(t, p)
	if c.State != "no_go" || c.Limiting == nil || c.Limiting.Factor != "daylight" {
		t.Fatalf("got %+v", c)
	}
	if want := "Arrives 22:00, after sunset 17:32 · Alpha → Bravo"; c.Limiting.Text != want {
		t.Fatalf("text = %q, want %q", c.Limiting.Text, want)
	}
	if c.Limiting.Sector != nil {
		t.Fatalf("daylight has no sector, got %v", *c.Limiting.Sector)
	}

	l.DaylightArrival = false
	p, _ = assessItinerary(context.Background(), a, l, pwNow, pwWeek(5, 180), pwNoTides, pwSun)
	if got := pwCell0(t, p).State; got != "go" {
		t.Fatalf("daylight off: state = %s, want go", got)
	}
}

func TestAssessItinerary_SunUnknownIsNotCheckedNotPassed(t *testing.T) {
	l := pwLimitsAll()
	l.DaylightArrival = true
	c := pwCell0(t, pwAssess(t, pwArgs([]string{"2026-10-20"}, "12:00", pwNorthLeg()), l, pwWeek(5, 180)))
	if !pwHas(c.NotChecked, "daylight") {
		t.Fatalf("not_checked = %v, want daylight", c.NotChecked)
	}
}

// ── wind against tide ───────────────────────────────────────────────────

// pwTides: HW 03:00, LW 09:15, HW 15:30, LW 21:45 on 20 October.
func pwTides(_ context.Context, lat, lon float64) ([]tideExtremePoint, error) {
	at := func(h, m int) time.Time { return time.Date(2026, 10, 20, h, m, 0, 0, pwLoc) }
	return []tideExtremePoint{
		{Time: at(3, 0), High: true}, {Time: at(9, 15)}, {Time: at(15, 30), High: true}, {Time: at(21, 45)},
		{Time: at(21, 45).Add(6*time.Hour + 15*time.Minute), High: true},
	}, nil
}

func pwTideLimits() passageLimits {
	l := pwLimitsAll()
	// Ahead and astern no-go 30 less the 15 kn reduction: 15 kn no-go and
	// 10.5 kn marginal while the wind runs against the stream.
	l.Ahead.WindKnNoGo = pwF(30)
	l.Astern.WindKnNoGo = pwF(30)
	l.WindAgainstTideOffsetKn = pwF(15)
	return l
}

func pwTideLeg(hours float64) passageLegArgs {
	l := pwNorthLeg()
	l.SpeedKn, l.PassageHours, l.FloodSetDeg = nil, pwF(hours), pwF(0)
	return l
}

func TestAssessItinerary_WindAgainstTheEbb(t *testing.T) {
	// 04:00-09:00 is ebb, which sets south. Wind from the south is against it.
	a := pwArgs([]string{"2026-10-20"}, "04:00", pwTideLeg(5))
	p, err := assessItinerary(context.Background(), a, pwTideLimits(), pwNow, pwWeek(18, 180), pwTides, pwNoSun)
	if err != nil {
		t.Fatal(err)
	}
	c := pwCell0(t, p)
	if c.State != "no_go" || c.Limiting == nil || c.Limiting.Factor != "wind_against_tide" {
		t.Fatalf("got %+v", c)
	}
	if want := "Wind 18 kn against the ebb · Alpha → Bravo"; c.Limiting.Text != want {
		t.Fatalf("text = %q, want %q", c.Limiting.Text, want)
	}
	if pwHas(c.NotChecked, "wind_against_tide") {
		t.Fatalf("it was checked: %v", c.NotChecked)
	}

	// Wind from the north runs with the ebb: no tide limit.
	p, _ = assessItinerary(context.Background(), a, pwTideLimits(), pwNow, pwWeek(18, 0), pwTides, pwNoSun)
	if c := pwCell0(t, p); c.State != "go" {
		t.Fatalf("wind with the stream must not trip the tide limit, got %+v", c)
	}
}

func TestAssessItinerary_WindAgainstTheFlood(t *testing.T) {
	a := pwArgs([]string{"2026-10-20"}, "10:00", pwTideLeg(5))
	p, _ := assessItinerary(context.Background(), a, pwTideLimits(), pwNow, pwWeek(12, 0), pwTides, pwNoSun)
	c := pwCell0(t, p)
	if c.State != "marginal" || c.Limiting == nil || c.Limiting.Text != "Wind 12 kn against the flood (marginal from 10.5) · Alpha → Bravo" {
		t.Fatalf("got %+v", c)
	}
}

func TestAssessItinerary_TideUnavailableIsNotCheckedNotPassed(t *testing.T) {
	a := pwArgs([]string{"2026-10-20"}, "04:00", pwTideLeg(5))
	p, err := assessItinerary(context.Background(), a, pwTideLimits(), pwNow, pwWeek(18, 180), pwNoTides, pwNoSun)
	if err != nil {
		t.Fatal(err)
	}
	if c := pwCell0(t, p); !pwHas(c.NotChecked, "wind_against_tide") {
		t.Fatalf("not_checked = %v", c.NotChecked)
	}
}

func TestAssessItinerary_NoFloodSetMeansTideNotChecked(t *testing.T) {
	leg := pwTideLeg(5)
	leg.FloodSetDeg = nil
	p, _ := assessItinerary(context.Background(), pwArgs([]string{"2026-10-20"}, "04:00", leg), pwTideLimits(), pwNow, pwWeek(18, 180), pwTides, pwNoSun)
	c := pwCell0(t, p)
	if c.State != "go" || !pwHas(c.NotChecked, "wind_against_tide") {
		t.Fatalf("got %+v", c)
	}
}

// ── missing data and blank limits ───────────────────────────────────────

func TestAssessItinerary_BeyondTheForecastIsNoData(t *testing.T) {
	fc := pwUniform(5, 0, 5, nil, 0, pwDay(19, 0), pwDay(22, 23))
	a := pwArgs([]string{"2026-10-25", "2026-10-22"}, "06:00", pwNorthLeg())
	p := pwAssess(t, a, pwLimitsAll(), fc)
	far := p.Rows[0]
	if far.Verdict != "no_data" || far.Cells[0].State != "no_data" || far.Cells[0].Limiting != nil || far.Cells[0].Note != "No forecast this far ahead" {
		t.Fatalf("got %+v", far)
	}
	// Leaving 22nd 06:00 for 10 h is inside the forecast.
	if p.Rows[1].Verdict != "go" {
		t.Fatalf("row 1 = %+v", p.Rows[1])
	}
	// An overnight leg that runs past the last hour is no_data too.
	late := pwArgs([]string{"2026-10-22"}, "20:00", pwNorthLeg())
	if c := pwCell0(t, pwAssess(t, late, pwLimitsAll(), fc)); c.State != "no_data" {
		t.Fatalf("a leg that outruns the forecast must be no_data, got %+v", c)
	}
}

func TestAssessItinerary_BlankLimitsAreNotChecked(t *testing.T) {
	c := pwCell0(t, pwAssess(t, pwArgs([]string{"2026-10-20"}, "06:00", pwNorthLeg()), pwLimitsAll(),
		pwUniform(5, 180, 20, pwF(3), 0, pwDay(19, 0), pwDay(25, 23))))
	if c.State != "go" {
		t.Fatalf("blank wave limits must not fail the cell, got %+v", c)
	}
	for _, want := range []string{"wave", "wind_against_tide"} {
		if !pwHas(c.NotChecked, want) {
			t.Errorf("not_checked = %v, missing %s", c.NotChecked, want)
		}
	}
	for _, bad := range []string{"wind_marginal", "gust"} {
		if pwHas(c.NotChecked, bad) {
			t.Errorf("%s is always worked out now, but not_checked = %v", bad, c.NotChecked)
		}
	}
}

// Marginal is 70% of no-go: astern no-go 35 is marginal from 24.5, and the
// card says so.
func TestAssessItinerary_MarginalIsComputedFromNoGoAndNamed(t *testing.T) {
	cases := []struct {
		name  string
		wind  float64
		state string
		text  string
	}{
		{"just under marginal", 24, "go", ""},
		{"marginal", 25, "marginal", "Wind S 25 kn astern (marginal from 24.5) · Alpha → Bravo"},
		{"no-go", 35, "no_go", "Wind S 35 kn astern · Alpha → Bravo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := pwCell0(t, pwAssess(t, pwArgs([]string{"2026-10-20"}, "06:00", pwNorthLeg()), pwLimitsAll(), pwWeek(tc.wind, 180)))
			if c.State != tc.state {
				t.Fatalf("state = %s, want %s (%+v)", c.State, tc.state, c.Limiting)
			}
			if tc.text != "" && (c.Limiting == nil || c.Limiting.Text != tc.text) {
				t.Fatalf("limiting = %+v, want %q", c.Limiting, tc.text)
			}
		})
	}
}

func TestAssessItinerary_WaveHeightAgainstItsSectorLimit(t *testing.T) {
	l := pwLimitsAll()
	l.Ahead.WaveMNoGo = pwF(2)
	// Wave from the north on a northbound leg is ahead.
	fc := pwUniform(5, 180, 5, pwF(2.14), 0, pwDay(19, 0), pwDay(25, 23))
	c := pwCell0(t, pwAssess(t, pwArgs([]string{"2026-10-20"}, "06:00", pwNorthLeg()), l, fc))
	if c.State != "no_go" || c.Limiting == nil || c.Limiting.Factor != "wave" || c.Limiting.Text != "Waves 2.1 m ahead · Alpha → Bravo" {
		t.Fatalf("got %+v / %+v", c, c.Limiting)
	}
}

// Gusts are judged against the sector's no-go wind plus 10 kn, marginal at
// 70% of that: abeam no-go 30 gives gust no-go 40 and gust marginal 28.
func TestAssessItinerary_GustsAreJudgedFromTheSectorNoGoWind(t *testing.T) {
	cases := []struct {
		name  string
		gust  float64
		state string
		text  string
	}{
		{"under the gust marginal", 27, "go", ""},
		{"gust marginal", 34, "marginal", "Gusts 34 kn abeam (marginal from 28) · Alpha → Bravo"},
		{"gust no-go", 41, "no_go", "Gusts 41 kn abeam · Alpha → Bravo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fc := pwUniform(20, 90, tc.gust, nil, 0, pwDay(19, 0), pwDay(25, 23))
			c := pwCell0(t, pwAssess(t, pwArgs([]string{"2026-10-20"}, "06:00", pwNorthLeg()), pwLimitsAll(), fc))
			if c.State != tc.state {
				t.Fatalf("state = %s, want %s (%+v)", c.State, tc.state, c.Limiting)
			}
			if tc.text != "" && (c.Limiting == nil || c.Limiting.Text != tc.text) {
				t.Fatalf("limiting = %+v, want %q", c.Limiting, tc.text)
			}
		})
	}
}

func TestAssessItinerary_MissingWaveDataIsNotCalm(t *testing.T) {
	l := pwLimitsAll()
	l.Ahead.WaveMNoGo = pwF(2)
	l.Astern.WaveMNoGo = pwF(2)
	c := pwCell0(t, pwAssess(t, pwArgs([]string{"2026-10-20"}, "06:00", pwNorthLeg()), l, pwWeek(5, 180)))
	if !pwHas(c.NotChecked, "wave") {
		t.Fatalf("no wave forecast must read as not checked, got %v", c.NotChecked)
	}
}

// ── limits are required ─────────────────────────────────────────────────

func TestAssessItinerary_NoLimitsIsAnError(t *testing.T) {
	_, err := assessItinerary(context.Background(), pwArgs([]string{"2026-10-20"}, "06:00", pwNorthLeg()), passageLimits{}, pwNow, pwWeek(5, 0), pwNoTides, pwNoSun)
	if err == nil || !strings.Contains(err.Error(), "Passage limits aren't set") || !strings.Contains(err.Error(), "Settings → Mate → Passage limits") {
		t.Fatalf("err = %v", err)
	}
}

func TestAssessItinerary_SectorWithoutWindNoGoIsAnError(t *testing.T) {
	l := pwLimitsAll()
	l.Abeam.WindKnNoGo = nil
	leg := pwNorthLeg()
	leg.Name = "Alpha to Bravo"
	_, err := assessItinerary(context.Background(), pwArgs([]string{"2026-10-20"}, "06:00", leg), l, pwNow, pwWeek(25, 90), pwNoTides, pwNoSun)
	if err == nil || !strings.Contains(err.Error(), "abeam") || !strings.Contains(err.Error(), "Alpha to Bravo") {
		t.Fatalf("err = %v", err)
	}
}

// ── verdict ─────────────────────────────────────────────────────────────

func TestPassageRowVerdict(t *testing.T) {
	for _, tc := range []struct {
		states []string
		want   string
	}{
		{[]string{"go", "go"}, "go"},
		{[]string{"go", "marginal"}, "marginal"},
		{[]string{"marginal", "no_data"}, "no_data"},
		{[]string{"no_data", "no_go", "go"}, "no_go"},
		{[]string{"no_go", "no_data"}, "no_go"},
	} {
		if got := passageRowVerdict(tc.states); got != tc.want {
			t.Errorf("%v = %s, want %s", tc.states, got, tc.want)
		}
	}
}

// ── arguments ───────────────────────────────────────────────────────────

func TestParsePassageWindowArgs_NamesTheBadArgument(t *testing.T) {
	good := `{"legs":[{"route":[{"lat":-19,"lon":147},{"lat":-18,"lon":147}],"passage_hours":10}],"departure_dates":["2026-10-20"],"depart_time":"06:00"}`
	if _, err := parsePassageWindowArgs(json.RawMessage(good)); err != nil {
		t.Fatalf("good args: %v", err)
	}
	for name, tc := range map[string]struct{ args, want string }{
		"both hours and speed": {`{"legs":[{"route":[{"lat":-19,"lon":147},{"lat":-18,"lon":147}],"passage_hours":10,"speed_kn":6}],"departure_dates":["2026-10-20"],"depart_time":"06:00"}`, "legs[0]"},
		"neither":              {`{"legs":[{"route":[{"lat":-19,"lon":147},{"lat":-18,"lon":147}]}],"departure_dates":["2026-10-20"],"depart_time":"06:00"}`, "passage_hours"},
		"one point":            {`{"legs":[{"route":[{"lat":-19,"lon":147}],"passage_hours":10}],"departure_dates":["2026-10-20"],"depart_time":"06:00"}`, "route"},
		"hours too long":       {`{"legs":[{"route":[{"lat":-19,"lon":147},{"lat":-18,"lon":147}],"passage_hours":40}],"departure_dates":["2026-10-20"],"depart_time":"06:00"}`, "passage_hours"},
		"speed too low":        {`{"legs":[{"route":[{"lat":-19,"lon":147},{"lat":-18,"lon":147}],"speed_kn":0.5}],"departure_dates":["2026-10-20"],"depart_time":"06:00"}`, "speed_kn"},
		"bad lat":              {`{"legs":[{"route":[{"lat":-99,"lon":147},{"lat":-18,"lon":147}],"passage_hours":10}],"departure_dates":["2026-10-20"],"depart_time":"06:00"}`, "lat"},
		"bad flood":            {`{"legs":[{"route":[{"lat":-19,"lon":147},{"lat":-18,"lon":147}],"passage_hours":10,"flood_set_deg":400}],"departure_dates":["2026-10-20"],"depart_time":"06:00"}`, "flood_set_deg"},
		"no legs":              {`{"legs":[],"departure_dates":["2026-10-20"],"depart_time":"06:00"}`, "legs"},
		"stays length":         {`{"legs":[{"route":[{"lat":-19,"lon":147},{"lat":-18,"lon":147}],"passage_hours":10}],"stays_nights":[1],"departure_dates":["2026-10-20"],"depart_time":"06:00"}`, "stays_nights"},
		"stay too long":        {`{"legs":[{"route":[{"lat":-19,"lon":147},{"lat":-18,"lon":147}],"passage_hours":10},{"route":[{"lat":-18,"lon":147},{"lat":-17,"lon":147}],"passage_hours":10}],"stays_nights":[15],"departure_dates":["2026-10-20"],"depart_time":"06:00"}`, "stays_nights"},
		"no dates":             {`{"legs":[{"route":[{"lat":-19,"lon":147},{"lat":-18,"lon":147}],"passage_hours":10}],"departure_dates":[],"depart_time":"06:00"}`, "departure_dates"},
		"bad date":             {`{"legs":[{"route":[{"lat":-19,"lon":147},{"lat":-18,"lon":147}],"passage_hours":10}],"departure_dates":["20/10/2026"],"depart_time":"06:00"}`, "departure_dates"},
		"missing depart":       {`{"legs":[{"route":[{"lat":-19,"lon":147},{"lat":-18,"lon":147}],"passage_hours":10}],"departure_dates":["2026-10-20"]}`, "depart_time"},
		"bad depart":           {`{"legs":[{"route":[{"lat":-19,"lon":147},{"lat":-18,"lon":147}],"passage_hours":10}],"departure_dates":["2026-10-20"],"depart_time":"6am"}`, "depart_time"},
		"bad leg depart":       {`{"legs":[{"route":[{"lat":-19,"lon":147},{"lat":-18,"lon":147}],"passage_hours":10,"depart_time":"25:00"}],"departure_dates":["2026-10-20"],"depart_time":"06:00"}`, "depart_time"},
	} {
		_, err := parsePassageWindowArgs(json.RawMessage(tc.args))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to name %q", name, err, tc.want)
		}
	}
}

// ── the tool ────────────────────────────────────────────────────────────

func pwToolDeps(limits passageLimits) assistantToolDeps {
	var hours []weatherHourPoint
	for t := pwDay(19, 0); !t.After(pwDay(40, 23)); t = t.Add(time.Hour) {
		hours = append(hours, weatherHourPoint{Time: t, WindSpeedMS: kn(25), WindGustMS: kn(30), WindDirectionDeg: 0})
	}
	wx := &pwWeather{bundle: weatherForecastBundle{Hourly: hours}}
	wv := &stubWaveProvider{id: "wv", bundle: waveForecastBundle{}}
	return assistantToolDeps{
		now:           func() time.Time { return pwNow },
		weather:       func() (weatherProvider, string, error) { return wx, "wx", nil },
		waves:         func() (waveProvider, string, error) { return wv, "wv", nil },
		tides:         func() (tideProvider, string, error) { return nil, "", fmt.Errorf("no tide provider") },
		passageLimits: func() (passageLimits, error) { return limits, nil },
		cards:         newAssistantCardSink(),
	}
}

const pwToolArgs = `{"legs":[{"route":[{"lat":-19,"lon":147,"name":"Alpha"},{"lat":-18,"lon":147,"name":"Bravo"}],"speed_kn":6}],"departure_dates":["2026-10-20","2026-10-21"],"depart_time":"06:00"}`

// pwRun drives the real run loop with real tool deps through one tool round
// calling assess_passage_window with args, then a final answer.
func pwRun(t *testing.T, d assistantToolDeps, args string) (assistantReply, error) {
	t.Helper()
	round := chatResponse(t, http.StatusOK, openRouterChatResponse{
		Model: "m",
		Choices: []openRouterChoice{{Message: openRouterMessage{
			Role: "assistant",
			ToolCalls: []openRouterToolCall{{ID: "call_1", Type: "function", Function: openRouterToolCallFunction{
				Name: "assess_passage_window", Arguments: openRouterArguments(args),
			}}},
		}}},
		Usage: usage(10, 5, 0),
	})
	doer := &queuedChatDoer{responses: []*http.Response{round, finalResponse(t, "Thursday looks best.", "m", usage(10, 5, 0))}, errs: []error{nil, nil}}
	emit, _ := recordingEmitter()
	runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: d, emit: emit}
	return runner.run(context.Background(), "system", "", nil)
}

func TestAssessPassageWindowTool_CollectsExactlyOneCardAndTellsTheModelNotToRepeatIt(t *testing.T) {
	d := pwToolDeps(pwLimitsAll())
	result, err := d.execute(context.Background(), "assess_passage_window", json.RawMessage(pwToolArgs))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(result, "A card showing this grid is attached; do not repeat it as a table") {
		t.Fatalf("the model is not told to leave the grid to the card: %s", result)
	}
	if !strings.Contains(result, "Wind N 25 kn ahead · Alpha → Bravo") {
		t.Fatalf("the model needs the limiting text: %s", result)
	}
	cards := d.takeCards(result)
	if len(cards) != 1 || cards[0].Kind != "passage_window" {
		t.Fatalf("cards = %+v", cards)
	}
	var payload passageWindowPayload
	if err := json.Unmarshal(cards[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Rows) != 2 || payload.Rows[0].Verdict != "no_go" {
		t.Fatalf("payload = %+v", payload)
	}
	if strings.Contains(string(cards[0].Payload), "do not repeat") {
		t.Fatalf("the instruction belongs to the model, not the card: %s", cards[0].Payload)
	}
}

func TestAssessPassageWindowTool_ARefusedCallThroughTheRunIsMatesReplyNotAFailedTurn(t *testing.T) {
	d := pwToolDeps(passageLimits{}) // limits not set: the tool errors, the run turns it into {"error":...}
	reply, err := pwRun(t, d, pwToolArgs)
	if err != nil {
		t.Fatalf("a tool error must reach the model, not fail the turn: %v", err)
	}
	if reply.Content != "Thursday looks best." || len(reply.Cards) != 0 {
		t.Fatalf("reply = %+v", reply)
	}
}

// pwBigArgs is the largest itinerary: 7 dates by 6 legs.
func pwBigArgs() string {
	var legs []string
	for i := 0; i < 6; i++ {
		legs = append(legs, fmt.Sprintf(`{"name":"Leg number %d to a long anchorage name","route":[{"lat":%.2f,"lon":147,"name":"Port %d Anchorage Bay"},{"lat":%.2f,"lon":147,"name":"Port %d Anchorage Bay"}],"passage_hours":2}`, i, -19+float64(i)*0.3, i, -19+float64(i)*0.3+0.3, i+1))
	}
	return `{"legs":[` + strings.Join(legs, ",") + `],"stays_nights":[0,0,0,0,0],"departure_dates":["2026-10-20","2026-10-21","2026-10-22","2026-10-23","2026-10-24","2026-10-25","2026-10-26"],"depart_time":"05:00"}`
}

func TestAssessPassageWindowTool_TheCardIsNotCappedAndTheModelStillNamesEveryVerdict(t *testing.T) {
	d := pwToolDeps(pwLimitsAll())
	reply, err := pwRun(t, d, pwBigArgs())
	if err != nil {
		t.Fatal(err)
	}
	if len(reply.Cards) != 1 {
		t.Fatalf("cards = %d", len(reply.Cards))
	}
	var payload passageWindowPayload
	if err := json.Unmarshal(reply.Cards[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Rows) != 7 || len(payload.Legs) != 6 {
		t.Fatalf("rows %d legs %d", len(payload.Rows), len(payload.Legs))
	}
	for _, r := range payload.Rows {
		if len(r.Cells) != 6 || r.Cells[0].Limiting == nil {
			t.Fatalf("row %s lost detail: %+v", r.DepartDate, r)
		}
	}
	if len(reply.Cards[0].Payload) <= assistantMaxToolResultChars {
		t.Fatalf("the fixture is meant to exceed the chat cap, card is %d bytes", len(reply.Cards[0].Payload))
	}

	result, err := d.execute(context.Background(), "assess_passage_window", json.RawMessage(pwBigArgs()))
	if err != nil {
		t.Fatal(err)
	}
	if len(result) > assistantMaxToolResultChars {
		t.Fatalf("model result is %d chars", len(result))
	}
	for _, r := range payload.Rows {
		if !strings.Contains(result, `"depart_date":"`+r.DepartDate+`","verdict":"`+r.Verdict+`"`) {
			t.Fatalf("the model must see %s %s: %s", r.DepartDate, r.Verdict, result)
		}
	}
	if strings.Contains(result, "truncated") {
		t.Fatalf("the model result was cut to the fallback: %s", result)
	}
}

func TestPassageModelResult_ShrinksDetailBeforeVerdicts(t *testing.T) {
	var p passageWindowPayload
	for r := 0; r < 7; r++ {
		row := passageWindowRow{DepartDate: fmt.Sprintf("2026-10-%02d", 20+r), Verdict: "no_go"}
		for l := 0; l < 6; l++ {
			row.Cells = append(row.Cells, passageWindowCell{Leg: l, State: "no_go", Depart: "2026-10-20T05:00:00+10:00", Arrive: "2026-10-20T07:00:00+10:00",
				Limiting: &passageWindowLimiting{Text: strings.Repeat("long limiting text ", 20)}})
		}
		p.Rows = append(p.Rows, row)
	}
	res := newPassageModelResult(p)
	out, err := capToolResultJSON(res, res.shrink)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > assistantMaxToolResultChars || strings.Contains(out, "truncated") {
		t.Fatalf("len %d: %.200s", len(out), out)
	}
	for _, r := range p.Rows {
		if !strings.Contains(out, `"depart_date":"`+r.DepartDate+`","verdict":"no_go"`) {
			t.Fatalf("lost %s: %.300s", r.DepartDate, out)
		}
	}
}

func TestAssessPassageWindowTool_RefusesWithoutLimits(t *testing.T) {
	d := pwToolDeps(passageLimits{})
	_, err := d.execute(context.Background(), "assess_passage_window", json.RawMessage(pwToolArgs))
	if err == nil || !strings.Contains(err.Error(), "Passage limits aren't set") {
		t.Fatalf("err = %v", err)
	}
}

func TestAssessPassageWindowTool_WaveProviderDownIsNotCheckedNotCalm(t *testing.T) {
	d := pwToolDeps(pwLimitsAll())
	d.waves = func() (waveProvider, string, error) { return nil, "", fmt.Errorf("no wave provider") }
	result, err := d.execute(context.Background(), "assess_passage_window", json.RawMessage(pwToolArgs))
	if err != nil {
		t.Fatal(err)
	}
	cards := d.takeCards(result)
	var payload passageWindowPayload
	_ = json.Unmarshal(cards[0].Payload, &payload)
	if !pwHas(payload.Rows[0].Cells[0].NotChecked, "wave") || payload.WaveProvider != "" {
		t.Fatalf("payload = %+v", payload)
	}
}

// ── captured forecasts ──────────────────────────────────────────────────

// pwCapture is a real Open-Meteo forecast and marine response for one point,
// saved under testdata/passage_window. Captured 2026-10-19 from
// api.open-meteo.com/v1/forecast (wind_speed_10m, wind_gusts_10m,
// wind_direction_10m, m/s, 7 days) and marine-api.open-meteo.com/v1/marine
// (wave_height, wave_direction, wave_period, models=ncep_gfswave025), the two
// requests the reference weather and wave plugins make, for Cape Upstart
// (-19.72, 147.75), Bowen (-20.01, 148.25) and Townsville (-19.25, 146.82).
type pwCapture struct {
	Hourly map[string]json.RawMessage `json:"hourly"`
}

func pwLoadSeries(t *testing.T, file string) (times []time.Time, series map[string][]float64) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "passage_window", file))
	if err != nil {
		t.Fatal(err)
	}
	var c pwCapture
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	var ts []string
	if err := json.Unmarshal(c.Hourly["time"], &ts); err != nil {
		t.Fatal(err)
	}
	for _, s := range ts {
		tm, err := time.ParseInLocation("2006-01-02T15:04", s, pwLoc)
		if err != nil {
			t.Fatal(err)
		}
		times = append(times, tm)
	}
	series = map[string][]float64{}
	for k, v := range c.Hourly {
		if k == "time" {
			continue
		}
		var f []float64
		if err := json.Unmarshal(v, &f); err != nil {
			t.Fatalf("%s %s: %v", file, k, err)
		}
		series[k] = f
	}
	return times, series
}

type pwPoint struct {
	name     string
	lat, lon float64
	hours    map[int64]passageHour
}

// pwCaptured serves the nearest captured point, built through the same
// bundle adapter production uses.
func pwCaptured(t *testing.T) forecastSource {
	t.Helper()
	var pts []pwPoint
	for _, p := range []pwPoint{{name: "upstart", lat: -19.72, lon: 147.75}, {name: "bowen", lat: -20.01, lon: 148.25}, {name: "townsville", lat: -19.25, lon: 146.82}} {
		ts, wind := pwLoadSeries(t, p.name+"_wind.json")
		_, wave := pwLoadSeries(t, p.name+"_wave.json")
		var wb weatherForecastBundle
		var vb waveForecastBundle
		for i, tm := range ts {
			wb.Hourly = append(wb.Hourly, weatherHourPoint{Time: tm, WindSpeedMS: wind["wind_speed_10m"][i], WindGustMS: wind["wind_gusts_10m"][i], WindDirectionDeg: wind["wind_direction_10m"][i]})
			vb.Hourly = append(vb.Hourly, waveHourPoint{Time: tm, WaveHeightM: wave["wave_height"][i], WaveDirectionDeg: wave["wave_direction"][i], WavePeriodS: wave["wave_period"][i]})
		}
		p.hours = passageHoursFromBundles(wb, &vb)
		pts = append(pts, p)
	}
	return forecastSource{Provider: "open-meteo", WaveProvider: "open-meteo-marine", Fetch: func(_ context.Context, lat, lon float64) (passageForecast, error) {
		best, bd := 0, 1e18
		for i, p := range pts {
			if d := haversineMeters(lat, lon, p.lat, p.lon); d < bd {
				best, bd = i, d
			}
		}
		return passageForecast{Hours: pts[best].hours}, nil
	}}
}

func TestAssessItinerary_CapturedForecastTownsvilleUpstartBowen(t *testing.T) {
	fc := pwCaptured(t)
	l := passageLimits{
		Ahead:  passageSectorLimits{WindKnNoGo: pwF(14), WaveMNoGo: pwF(2)},
		Abeam:  passageSectorLimits{WindKnNoGo: pwF(28), WaveMNoGo: pwF(2.5)},
		Astern: passageSectorLimits{WindKnNoGo: pwF(33), WaveMNoGo: pwF(3)},
	}
	a := passageWindowArgs{
		Legs: []passageLegArgs{
			{Route: []passageRoutePoint{{Lat: -19.25, Lon: 146.82, Name: "Townsville"}, {Lat: -19.30, Lon: 147.35, Name: "Cape Bowling Green"}, {Lat: -19.72, Lon: 147.75, Name: "Cape Upstart"}}, SpeedKn: pwF(6)},
			{Route: []passageRoutePoint{{Lat: -19.72, Lon: 147.75, Name: "Cape Upstart"}, {Lat: -20.01, Lon: 148.25, Name: "Bowen"}}, SpeedKn: pwF(6)},
		},
		StaysNights:    []int{1},
		DepartureDates: []string{"2026-10-20", "2026-10-21", "2026-10-24", "2026-10-25"},
		DepartTime:     "06:00",
	}
	p, err := assessItinerary(context.Background(), a, l, pwNow, fc, pwNoTides, pwNoSun)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Rows) != 4 {
		t.Fatalf("rows = %d", len(p.Rows))
	}
	// By hand from the captured hours: 20 October has gusts of 32 kn on the
	// south-easterly leg, past the ahead sector's gust no-go (14 + 10 = 24 kn),
	// while its wind peaks at 12-13 kn, which is only marginal against the 14 kn
	// no-go. 21 October's gusts reach 22 kn, past the 16.8 kn gust marginal.
	if got := p.Rows[0]; got.Verdict != "no_go" || got.Cells[0].State != "no_go" || got.Cells[0].Limiting == nil ||
		got.Cells[0].Limiting.Factor != "gust" || got.Cells[0].Limiting.Text != "Gusts 32 kn ahead · Cape Bowling Green → Cape Upstart" {
		t.Fatalf("20 October = %+v / %+v", got, got.Cells[0].Limiting)
	}
	if got := p.Rows[1]; got.Verdict != "marginal" || got.Cells[0].Limiting == nil ||
		got.Cells[0].Limiting.Text != "Gusts 22 kn ahead (marginal from 16.8) · Townsville → Cape Bowling Green" {
		t.Fatalf("21 October = %+v / %+v", got, got.Cells[0].Limiting)
	}
	// Leaving the 25th, the second leg is on the 26th: beyond the captured week.
	if got := p.Rows[3]; got.Verdict != "no_data" || got.Cells[1].State != "no_data" {
		t.Fatalf("last row = %+v", got)
	}
	for _, r := range p.Rows[:3] {
		for _, c := range r.Cells {
			if c.State == "no_data" {
				t.Fatalf("row %s should be inside the forecast: %+v", r.DepartDate, c)
			}
			if pwHas(c.NotChecked, "wave") {
				t.Fatalf("captured data has waves: %+v", c)
			}
		}
	}
}

// ── review fixes ────────────────────────────────────────────────────────

func TestAssessItinerary_ForecastFetchedAtIsTheOldestSourceTime(t *testing.T) {
	fc := pwWeek(5, 180)
	inner := fc.Fetch
	fc.Fetch = func(ctx context.Context, lat, lon float64) (passageForecast, error) {
		f, err := inner(ctx, lat, lon)
		f.FetchedAt = pwNow.Add(-5 * time.Hour)
		if lat > -18.5 {
			f.FetchedAt = pwNow.Add(-1 * time.Hour)
		}
		return f, err
	}
	p := pwAssess(t, pwArgs([]string{"2026-10-20"}, "06:00", pwNorthLeg()), pwLimitsAll(), fc)
	if want := "2026-10-19T04:00:00+10:00"; p.ForecastFetchedAt != want {
		t.Fatalf("forecast_fetched_at = %s, want %s", p.ForecastFetchedAt, want)
	}
}

func TestAssessPassageWindowTool_CachedForecastReportsItsCacheAge(t *testing.T) {
	d := pwToolDeps(pwLimitsAll())
	wx := d.mustWeatherStub(t)
	wx.bundle.Cached, wx.bundle.CachedAt = true, pwNow.Add(-5*time.Hour)
	result, err := d.execute(context.Background(), "assess_passage_window", json.RawMessage(pwToolArgs))
	if err != nil {
		t.Fatal(err)
	}
	var payload passageWindowPayload
	_ = json.Unmarshal(d.takeCards(result)[0].Payload, &payload)
	if want := "2026-10-19T04:00:00+10:00"; payload.ForecastFetchedAt != want {
		t.Fatalf("forecast_fetched_at = %s, want %s", payload.ForecastFetchedAt, want)
	}
}

// pwWeather is a weather provider that is safe to call from several
// goroutines, as the tool does.
type pwWeather struct{ bundle weatherForecastBundle }

func (p *pwWeather) ID() string          { return "wx" }
func (p *pwWeather) Name() string        { return "wx" }
func (p *pwWeather) Description() string { return "pw" }
func (p *pwWeather) TTLSeconds() int64   { return 0 }
func (p *pwWeather) FetchForecast(lat, lon float64, days int, timezone string) (weatherForecastBundle, error) {
	return p.bundle, nil
}

func (d assistantToolDeps) mustWeatherStub(t *testing.T) *pwWeather {
	t.Helper()
	p, _, err := d.weather()
	if err != nil {
		t.Fatal(err)
	}
	return p.(*pwWeather)
}

func TestAssessItinerary_TideIsFetchedOncePerDistinctStartPoint(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}
	tides := func(_ context.Context, lat, lon float64) ([]tideExtremePoint, error) {
		mu.Lock()
		calls[fmt.Sprintf("%.2f,%.2f", lat, lon)]++
		mu.Unlock()
		var ext []tideExtremePoint
		for t0, high := pwDay(18, 0), true; t0.Before(pwDay(30, 0)); t0, high = t0.Add(6*time.Hour+12*time.Minute), !high {
			ext = append(ext, tideExtremePoint{Time: t0, High: high})
		}
		return ext, nil
	}
	l0, l1 := pwTideLeg(4), pwTideLeg(4)
	l1.Route = []passageRoutePoint{{Lat: -18, Lon: 147, Name: "Bravo"}, {Lat: -17, Lon: 147, Name: "Charlie"}}
	a := pwArgs([]string{"2026-10-20", "2026-10-21", "2026-10-22"}, "06:00", l0, l1)
	if _, err := assessItinerary(context.Background(), a, pwTideLimits(), pwNow, pwWeek(5, 0), tides, pwNoSun); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls["-19.00,147.00"] != 1 || calls["-18.00,147.00"] != 1 {
		t.Fatalf("tide fetches = %v, want one per distinct start point", calls)
	}
}

func TestAssessItinerary_CancelledContextStopsFurtherProviderCalls(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls int32
	var mu sync.Mutex
	fc := pwWeek(5, 0)
	fc.Fetch = func(_ context.Context, lat, lon float64) (passageForecast, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		cancel()
		return passageForecast{}, errors.New("fetch interrupted")
	}
	var route []passageRoutePoint
	for i := 0; i < 10; i++ {
		route = append(route, passageRoutePoint{Lat: -19 + float64(i)*0.2, Lon: 147, Name: fmt.Sprintf("P%d", i)})
	}
	leg := passageLegArgs{Route: route, SpeedKn: pwF(6)}
	_, err := assessItinerary(ctx, pwArgs([]string{"2026-10-20"}, "06:00", leg), pwLimitsAll(), pwNow, fc, pwNoTides, pwNoSun)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the context error", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls >= 10 || calls > 4 {
		t.Fatalf("%d provider calls after cancellation", calls)
	}
}

func TestAssessPassageWindowTool_ProviderGapsAreLoggedOncePerCauseAndStayNotChecked(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	d := pwToolDeps(pwLimitsAll())
	d.waves = func() (waveProvider, string, error) {
		return &stubWaveProvider{id: "wv", err: errors.New("marine api down")}, "wv", nil
	}
	args := `{"legs":[{"route":[{"lat":-19,"lon":147,"name":"Alpha"},{"lat":-18.5,"lon":147},{"lat":-18,"lon":147,"name":"Bravo"}],"speed_kn":6,"flood_set_deg":0}],"departure_dates":["2026-10-20"],"depart_time":"06:00"}`
	l := pwLimitsAll()
	l.WindAgainstTideOffsetKn = pwF(5)
	d.passageLimits = func() (passageLimits, error) { return l, nil }
	d.tides = func() (tideProvider, string, error) { return nil, "tide-x", errors.New("tide provider offline") }
	result, err := d.execute(context.Background(), "assess_passage_window", json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	var payload passageWindowPayload
	_ = json.Unmarshal(d.takeCards(result)[0].Payload, &payload)
	nc := payload.Rows[0].Cells[0].NotChecked
	if !pwHas(nc, "wave") || !pwHas(nc, "wind_against_tide") {
		t.Fatalf("not_checked = %v", nc)
	}
	out := buf.String()
	if n := strings.Count(out, "marine api down"); n != 1 {
		t.Fatalf("wave failure logged %d times, want once:\n%s", n, out)
	}
	if n := strings.Count(out, "tide provider offline"); n != 1 {
		t.Fatalf("tide failure logged %d times, want once:\n%s", n, out)
	}
	if !strings.Contains(out, "wv") || !strings.Contains(out, "tide-x") {
		t.Fatalf("the log must name the providers:\n%s", out)
	}
}

func TestAssessItinerary_ArrivalBeforeSunriseIsNoGoWhenRequired(t *testing.T) {
	l := pwLimitsAll()
	l.DaylightArrival = true
	sun := func(date time.Time, lat, lon float64) (time.Time, time.Time, bool) {
		y, m, d := date.Date()
		return time.Date(y, m, d, 5, 45, 0, 0, date.Location()), time.Date(y, m, d, 17, 32, 0, 0, date.Location()), true
	}
	leg := pwNorthLeg()
	leg.SpeedKn, leg.PassageHours = nil, pwF(2)
	a := pwArgs([]string{"2026-10-20"}, "02:10", leg)
	p, err := assessItinerary(context.Background(), a, l, pwNow, pwWeek(5, 180), pwNoTides, sun)
	if err != nil {
		t.Fatal(err)
	}
	c := pwCell0(t, p)
	if c.State != "no_go" || c.Limiting == nil || c.Limiting.Factor != "daylight" {
		t.Fatalf("got %+v", c)
	}
	if want := "Arrives 04:10, before sunrise 05:45 · Alpha → Bravo"; c.Limiting.Text != want {
		t.Fatalf("text = %q, want %q", c.Limiting.Text, want)
	}
}

func TestAssessItinerary_TheForecastHourAfterArrivalIsJudged(t *testing.T) {
	// Depart 06:00, arrive 14:30. Only the 15:00 forecast is a no-go.
	hours := map[int64]passageHour{}
	for t0 := pwDay(19, 0); !t0.After(pwDay(25, 23)); t0 = t0.Add(time.Hour) {
		w := kn(5)
		if t0.Equal(pwDay(20, 15)) {
			w = kn(25)
		}
		hours[t0.Unix()] = passageHour{WindMS: w, GustMS: w, WindDirDeg: 0}
	}
	fc := forecastSource{Provider: "p", Fetch: func(_ context.Context, lat, lon float64) (passageForecast, error) {
		return passageForecast{Hours: hours}, nil
	}}
	leg := pwNorthLeg()
	leg.SpeedKn, leg.PassageHours = nil, pwF(8.5)
	c := pwCell0(t, pwAssess(t, pwArgs([]string{"2026-10-20"}, "06:00", leg), pwLimitsAll(), fc))
	if c.State != "no_go" {
		t.Fatalf("the 15:00 hour covers a 14:30 arrival, got %+v", c)
	}
}

// The cell judges the figure it prints: a gust that reads "25 kn" against a
// marginal of 24.5 is marginal, whatever the raw forecast said.
func TestAssessItinerary_JudgesTheFigureAsDisplayed(t *testing.T) {
	// Abeam no-go 25 gives gust no-go 35 and gust marginal 24.5.
	gustLimits := func() passageLimits {
		l := pwLimitsAll()
		l.Abeam.WindKnNoGo = pwF(25)
		return l
	}
	waveLimits := func() passageLimits {
		l := pwLimitsAll()
		l.Ahead.WaveMNoGo = pwF(1.8)
		return l
	}
	cases := []struct {
		name  string
		l     passageLimits
		fc    forecastSource
		state string
		text  string
	}{
		{"gust 24.6 reads 25 and is marginal", gustLimits(), pwUniform(10, 90, 24.6, nil, 0, pwDay(19, 0), pwDay(25, 23)), "marginal", "Gusts 25 kn abeam (marginal from 24.5) · Alpha → Bravo"},
		{"gust 24.4 reads 24 and is go", gustLimits(), pwUniform(10, 90, 24.4, nil, 0, pwDay(19, 0), pwDay(25, 23)), "go", "Gusts 24 kn abeam · Alpha → Bravo"},
		{"wave 1.75 reads 1.8 and is no-go", waveLimits(), pwUniform(5, 180, 5, pwF(1.75), 0, pwDay(19, 0), pwDay(25, 23)), "no_go", "Waves 1.8 m ahead · Alpha → Bravo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := pwCell0(t, pwAssess(t, pwArgs([]string{"2026-10-20"}, "06:00", pwNorthLeg()), tc.l, tc.fc))
			if c.State != tc.state || c.Limiting == nil || c.Limiting.Text != tc.text {
				t.Fatalf("got %s / %+v, want %s / %q", c.State, c.Limiting, tc.state, tc.text)
			}
		})
	}
}

func TestPassageWindowArgs_DuplicateDepartureDatesAreRejected(t *testing.T) {
	a := pwArgs([]string{"2026-10-21", "2026-10-22", "2026-10-21"}, "07:00", pwNorthLeg())
	_, err := assessItinerary(context.Background(), a, pwLimitsAll(), pwNow, pwWeek(10, 0), pwNoTides, pwNoSun)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("got %v", err)
	}
}

func TestPassageLeg_RoutePointsAcrossTheDateLineInterpolateTheShortWay(t *testing.T) {
	leg := passageLegArgs{Route: []passageRoutePoint{{Lat: -17, Lon: 179}, {Lat: -17, Lon: -179}}, SpeedKn: pwF(6)}
	p := planPassageLeg(0, leg)
	_, lon, _ := p.at(0.5)
	if math.Abs(math.Abs(lon)-180) > 0.01 {
		t.Errorf("midpoint lon = %v, want +-180", lon)
	}
	for _, s := range p.samples {
		if math.Abs(s.Lon) < 170 {
			t.Errorf("sample at lon %v is on the wrong side of the world", s.Lon)
		}
	}
}

func TestAssessItinerary_TideDataThatStopsShortIsLoggedAsTheReason(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	// pwTides ends at 04:00 on the 21st; a 22:00 departure runs past it.
	a := pwArgs([]string{"2026-10-20"}, "22:00", pwTideLeg(8))
	p, err := assessItinerary(context.Background(), a, pwTideLimits(), pwNow, pwWeek(18, 180), pwTides, pwNoSun)
	if err != nil {
		t.Fatal(err)
	}
	if c := pwCell0(t, p); !pwHas(c.NotChecked, "wind_against_tide") {
		t.Fatalf("not_checked = %v", c.NotChecked)
	}
	if !strings.Contains(buf.String(), "tide extremes end") {
		t.Errorf("the reason was not logged: %q", buf.String())
	}
}
