package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// The morning report says when the forecast wind changes (ADR 0287).

// boatFix is the position the status already holds, so the report never reads
// the vessel state a second time.
func boatFix() *vesselStatusResult {
	return &vesselStatusResult{Position: vesselStatusFix{Lat: -26.68, Lon: 153.12}}
}

func TestWatchWindChangesFor_FindsTheNextChangeAtTheBoat(t *testing.T) {
	loc := queenslandLoc(t)
	now := time.Date(2026, 11, 14, 6, 0, 0, 0, loc)
	hours := windChangeHourly(now.Truncate(time.Hour), 48, 10)
	got := watchWindChangesFor(resolveWatchForecast(stayForecastDeps(hours, nil, passageLimits{Abeam: passageSectorLimits{WindKnNoGo: fp(20)}})), boatFix(), now)
	if got.Problem != "" || len(got.Changes) != 1 {
		t.Fatalf("%+v", got)
	}
	c := got.Changes[0]
	if c.From != "N" || c.To != "SE" || c.Label != "2 AM" || !c.Prominent || c.DayLabel != "tomorrow" {
		t.Fatalf("%+v", c)
	}
}

func TestWatchWindChangesFor_SaysWhyItCouldNotBeRead(t *testing.T) {
	now := time.Date(2026, 11, 14, 6, 0, 0, 0, queenslandLoc(t))
	if got := watchWindChangesFor(resolveWatchForecast(stayForecastDeps(nil, errors.New("weather plugin offline"), passageLimits{})), boatFix(), now); !strings.Contains(got.Problem, "weather plugin offline") {
		t.Fatalf("%+v", got)
	}
	if got := watchWindChangesFor(resolveWatchForecast(assistantToolDeps{}), boatFix(), now); got.Problem == "" {
		t.Fatalf("unwired is said: %+v", got)
	}
	noFix := &vesselStatusResult{Position: vesselStatusFix{Missing: "no fresh boat position"}}
	if got := watchWindChangesFor(resolveWatchForecast(stayForecastDeps(nil, nil, passageLimits{})), noFix, now); !strings.Contains(got.Problem, "no fresh boat position") {
		t.Fatalf("a boat with no fix says so: %+v", got)
	}
}

func TestWatchWeatherSection_WindChangeLine(t *testing.T) {
	in := baseInput()
	in.WindChanges = watchWindChanges{Changes: []windChange{{From: "N", To: "SE", Label: "2 PM", DayLabel: "Tue 17", SpeedMinKn: 15, SpeedMaxKn: 20, Prominent: true}}}
	report, findings := buildWatchReport(baseWatch(), in)
	if len(findings) != 0 {
		t.Fatalf("a forecast is not a fault on board: %+v", findings)
	}
	s := sectionOf(t, report, "weather")
	want := "Forecast wind change: N to SE Tue 17 around 2 PM, 15 to 20 kn behind it, at or over your lowest marginal wind."
	if !strings.Contains(strings.Join(s.Lines, "\n"), want) {
		t.Errorf("lines %q", s.Lines)
	}

	in.WindChanges = watchWindChanges{Changes: []windChange{{From: "N", To: "SE", Label: "2 PM", DayLabel: "today", SpeedMinKn: 15, SpeedMaxKn: 15, LowerConfidence: true, Prominent: true}, {From: "SE", To: "W", Label: "1 AM"}}}
	report, _ = buildWatchReport(baseWatch(), in)
	lines := strings.Join(sectionOf(t, report, "weather").Lines, "\n")
	if !strings.Contains(lines, "N to SE today around 2 PM, 15 kn behind it (lower confidence), at or over your lowest marginal wind, and 1 more after.") {
		t.Errorf("lines %q", lines)
	}

	in.WindChanges = watchWindChanges{}
	report, _ = buildWatchReport(baseWatch(), in)
	if strings.Contains(strings.Join(sectionOf(t, report, "weather").Lines, "\n"), "wind change") {
		t.Error("no change, no line")
	}
	for _, r := range sectionOf(t, report, "weather").Rows {
		if r.Label == "Wind change" {
			t.Errorf("no change, no row: %+v", r)
		}
	}

	in.WindChanges = watchWindChanges{Problem: "the forecast is not wired"}
	report, _ = buildWatchReport(baseWatch(), in)
	if !strings.Contains(strings.Join(sectionOf(t, report, "weather").Lines, "\n"), "Forecast wind changes were not read: the forecast is not wired") {
		t.Error("a failure to read is said")
	}
}

// The page draws only rows when a section has any, so the change must be a row.
func TestWatchWeatherSection_WindChangeIsARow(t *testing.T) {
	in := baseInput()
	in.WindChanges = watchWindChanges{Changes: []windChange{{From: "N", To: "SE", Label: "2 PM", DayLabel: "Tue 17", SpeedMinKn: 15, SpeedMaxKn: 20, Prominent: true}}}
	report, _ := buildWatchReport(baseWatch(), in)
	var got *watchReportRow
	rows := sectionOf(t, report, "weather").Rows
	for i := range rows {
		if rows[i].Label == "Wind change" {
			got = &rows[i]
		}
	}
	if got == nil || got.Value != "N to SE Tue 17 around 2 PM, 15 to 20 kn behind it, at or over your lowest marginal wind" {
		t.Fatalf("rows %+v", rows)
	}

	in.WindChanges = watchWindChanges{Problem: "the forecast is not wired"}
	report, _ = buildWatchReport(baseWatch(), in)
	s := sectionOf(t, report, "weather")
	var nc *watchReportRow
	for i := range s.Rows {
		if s.Rows[i].NotChecked && strings.Contains(s.Rows[i].Label, "wind change") {
			nc = &s.Rows[i]
		}
	}
	if nc == nil || !strings.Contains(strings.Join(s.NotChecked, "\n"), "the forecast is not wired") {
		t.Fatalf("a failure to read is a not-checked row: rows %+v notchecked %q", s.Rows, s.NotChecked)
	}
}

// Unreadable limits keep the changes, as the drawer does, and say why none is prominent.
func TestWatchWindChangesFor_UnreadableLimitsKeepTheChanges(t *testing.T) {
	loc := queenslandLoc(t)
	now := time.Date(2026, 11, 14, 6, 0, 0, 0, loc)
	hours := windChangeHourly(now.Truncate(time.Hour), 48, 10)
	d := stayForecastDeps(hours, nil, passageLimits{})
	d.passageLimits = func() (passageLimits, error) {
		return passageLimits{}, errors.New("assistant.passage_limits is unreadable")
	}
	got := watchWindChangesFor(resolveWatchForecast(d), boatFix(), now)
	if got.Problem != "" || len(got.Changes) != 1 || got.Changes[0].Prominent {
		t.Fatalf("changes are listed, none prominent: %+v", got)
	}
	if !strings.Contains(got.LimitsProblem, "assistant.passage_limits is unreadable") {
		t.Fatalf("the limits problem is carried: %+v", got)
	}

	in := baseInput()
	in.WindChanges = got
	report, _ := buildWatchReport(baseWatch(), in)
	s := sectionOf(t, report, "weather")
	all := strings.Join(s.Lines, "\n") + "\n" + strings.Join(s.NotChecked, "\n")
	if !strings.Contains(all, "Wind changes not judged against your limits: assistant.passage_limits is unreadable") {
		t.Errorf("the report says why: lines %q notchecked %q", s.Lines, s.NotChecked)
	}
}

// With no change to judge there is nothing the limits could have judged, so the
// note about them would only worry the reader.
func TestWatchWeatherSection_NoLimitsNoteWithoutAChange(t *testing.T) {
	in := baseInput()
	in.WindChanges = watchWindChanges{LimitsProblem: "assistant.passage_limits is unreadable"}
	report, _ := buildWatchReport(baseWatch(), in)
	s := sectionOf(t, report, "weather")
	all := strings.Join(s.Lines, "\n") + "\n" + strings.Join(s.NotChecked, "\n")
	if strings.Contains(all, "not judged against your limits") {
		t.Errorf("no change, no note: lines %q notchecked %q", s.Lines, s.NotChecked)
	}
	for _, r := range s.Rows {
		if strings.Contains(r.Label, "not judged") {
			t.Errorf("no change, no row: %+v", r)
		}
	}
}

// The forecast endpoint takes the boat's last trusted fix, however old and
// whatever the GNSS alert says, so the watch's wind changes come from the same
// place as the drawer's. A fix is refused only when it is not a position.
func TestVesselStatusPosition_IsTheLastTrustedFixAsTheForecastEndpointTakesIt(t *testing.T) {
	d := statusTestDeps(t)
	d.vesselState = func() (vesselStateData, error) {
		return vesselStateData{Depth: 3, DepthLastUpdateAge: 2, WindSpeedTrueKts: -1, Latitude: -26.68, Longitude: 153.12, PositionLastUpdateAge: 3600, GNSSCriticalAlert: true}, nil
	}
	position := func() vesselStatusFix {
		res, err := d.buildVesselStatus(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return res.Position
	}
	if got := position(); got.Missing != "" || got.Lat != -26.68 || got.Lon != 153.12 {
		t.Fatalf("a stale fix under a GNSS alert is still the last trusted fix: %+v", got)
	}
	d.vesselState = func() (vesselStateData, error) {
		return vesselStateData{Depth: 3, DepthLastUpdateAge: 2, WindSpeedTrueKts: -1, Latitude: -1, Longitude: -1, PositionLastUpdateAge: -1}, nil
	}
	if got := position(); got.Missing == "" {
		t.Fatalf("the -1,-1 placeholder is no position: %+v", got)
	}
}

func TestWatchWeatherSection_SaysHowFarTheWindChangesWereChecked(t *testing.T) {
	in := baseInput()
	in.WindChanges = watchWindChanges{CheckedTo: "Thu 19 3 AM", CheckedReason: "the forecast skips an hour here"}
	report, _ := buildWatchReport(baseWatch(), in)
	s := sectionOf(t, report, "weather")
	if !strings.Contains(strings.Join(s.Lines, "\n"), "Wind changes checked to Thu 19 3 AM: the forecast skips an hour here.") {
		t.Errorf("lines %q", s.Lines)
	}
	found := false
	for _, r := range s.Rows {
		found = found || (r.Label == "Wind changes checked to" && r.Value == "Thu 19 3 AM: the forecast skips an hour here")
	}
	if !found {
		t.Errorf("rows %+v", s.Rows)
	}
	in.WindChanges = watchWindChanges{}
	report, _ = buildWatchReport(baseWatch(), in)
	if strings.Contains(strings.Join(sectionOf(t, report, "weather").Lines, "\n"), "checked to") {
		t.Error("the whole forecast was read: nothing to say")
	}
}
