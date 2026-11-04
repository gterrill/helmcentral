package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The structured sections of a watch report (ADR 0255): rows, a summary and
// the checks that could not run, beside the lines older readers still use.

func rowOf(t *testing.T, s watchReportSection, label string) watchReportRow {
	t.Helper()
	for _, r := range s.Rows {
		if r.Label == label {
			return r
		}
	}
	t.Fatalf("section %s has no %q row: %+v", s.Name, label, s.Rows)
	return watchReportRow{}
}

func hasRow(s watchReportSection, label string) bool {
	for _, r := range s.Rows {
		if r.Label == label {
			return true
		}
	}
	return false
}

func TestWatchRows_PowerRowsAndTheChecksThatDidNotRun(t *testing.T) {
	in := baseInput()
	in.Status.Power.HouseBank = vesselStatusHouseBank{SocPercent: val(100, "%"), Voltage: val(28.27, "V"), Current: val(3.5, "A")}
	in.Status.ObservationsUnchecked = []string{
		"overnight house bank draw: the last completed night has no usable record (a gap in the data)",
		"the mystery check could not run",
	}
	report, _ := buildWatchReport(baseWatch(), in)
	s := sectionOf(t, report, "power")
	if r := rowOf(t, s, "House bank"); r.Value != "100 %, 28.27 V, 3.5 A" || r.NotChecked {
		t.Errorf("house bank %+v", r)
	}
	if r := rowOf(t, s, "Solar"); r.Value != "120 W" {
		t.Errorf("solar %+v", r)
	}
	if r := rowOf(t, s, "DC load"); r.Value != "55 W" {
		t.Errorf("dc load %+v", r)
	}
	if r := rowOf(t, s, "Overnight house bank draw"); r.Value != "Not checked" || !r.NotChecked {
		t.Errorf("draw %+v", r)
	}
	if r := rowOf(t, s, "Other check"); r.Value != "Not checked" || !r.NotChecked {
		t.Errorf("other %+v", r)
	}
	want := []string{
		"Overnight house bank draw: the last completed night has no usable record (a gap in the data)",
		"The mystery check could not run",
	}
	if strings.Join(s.NotChecked, "|") != strings.Join(want, "|") {
		t.Errorf("not checked %q", s.NotChecked)
	}
	// The lines are what they were.
	if got := strings.Join(s.Lines, "|"); !strings.Contains(got, "House bank: 100 %, 28.27 V, 3.5 A") || !strings.Contains(got, "Not checked, overnight house bank draw: the last") {
		t.Errorf("lines %q", got)
	}
}

func TestWatchRows_ABoatWithNoSolarIsNotNaggedEveryMorning(t *testing.T) {
	in := baseInput()
	in.Status.Power.SolarW = missingValue("no solar reading")
	report, _ := buildWatchReport(baseWatch(), in)
	s := sectionOf(t, report, "power")
	r := rowOf(t, s, "Solar")
	if r.Value != "Not available (no solar reading)" || r.NotChecked || len(s.NotChecked) != 0 {
		t.Errorf("solar %+v not checked %q", r, s.NotChecked)
	}
}

func prevReportFor(t *testing.T, mutate func(*watchReportInput)) *watchReport {
	t.Helper()
	in := baseInput()
	if mutate != nil {
		mutate(&in)
	}
	report, _ := buildWatchReport(baseWatch(), in)
	raw, _ := json.Marshal(report)
	var prev watchReport
	if err := json.Unmarshal(raw, &prev); err != nil {
		t.Fatal(err)
	}
	return &prev
}

func TestWatchRows_TankRowsCarryTheLevelTheChangeAndTheFlag(t *testing.T) {
	in := baseInput()
	in.Status.Tanks.Tanks = []vesselStatusTank{
		{Name: "Black", Kind: "waste", Level: val(30, "%")},
		{Name: "Diesel", Kind: "fuel", Level: val(48.6, "%")},
		{Name: "Spare", Kind: "", Level: val(10, "%")},
		{Name: "Fresh Water", Kind: "water", Level: val(70, "%")},
		{Name: "Bow", Kind: "fuel", Level: missingValue("last updated 5 minutes ago")},
	}
	in.Previous = prevReportFor(t, func(p *watchReportInput) {
		p.Status.Tanks.Tanks = []vesselStatusTank{
			{Name: "Black", Kind: "waste", Level: val(30.2, "%")},
			{Name: "Diesel", Kind: "fuel", Level: val(60, "%")},
			{Name: "Spare", Kind: "", Level: val(6, "%")},
			{Name: "Fresh Water", Kind: "water", Level: val(66, "%")},
			{Name: "Bow", Kind: "fuel", Level: val(50, "%")},
		}
	})
	in.PreviousAt = in.Since
	report, findings := buildWatchReport(baseWatch(), in)
	s := sectionOf(t, report, "tanks")
	var order []string
	for _, r := range s.Rows {
		order = append(order, r.Label)
	}
	if got := strings.Join(order, ","); got != "Fresh Water,Diesel,Bow,Black,Spare" {
		t.Errorf("rows are water, fuel, waste, then others, stable within a kind: %s", got)
	}
	if r := rowOf(t, s, "Diesel"); r.Value != "49 %" || r.Change != "−11" || !r.Flagged {
		t.Errorf("diesel %+v", r)
	}
	if r := rowOf(t, s, "Fresh Water"); r.Value != "70 %" || r.Change != "+4" || r.Flagged {
		t.Errorf("water %+v", r)
	}
	if r := rowOf(t, s, "Black"); r.Value != "30 %" || r.Change != "no change" || r.Flagged {
		t.Errorf("black %+v", r)
	}
	if r := rowOf(t, s, "Bow"); r.Value != "Not available" || !r.NotChecked || r.Change != "" {
		t.Errorf("bow %+v", r)
	}
	if len(s.NotChecked) != 1 || s.NotChecked[0] != "Bow level: last updated 5 minutes ago" {
		t.Errorf("not checked %q", s.NotChecked)
	}
	// The row's flag agrees with the finding, and the lines keep the status order.
	if got := findingsFor(findings, "tanks"); len(got) != 1 {
		t.Errorf("findings %+v", got)
	}
	if !strings.HasPrefix(s.Lines[0], "Black: 30") {
		t.Errorf("lines keep the status order: %q", s.Lines)
	}
}

func TestWatchRows_TankChangeIsEmptyWithoutAPreviousReport(t *testing.T) {
	in := baseInput()
	in.FirstRun = true
	report, _ := buildWatchReport(baseWatch(), in)
	for _, r := range sectionOf(t, report, "tanks").Rows {
		if r.Change != "" || r.Flagged {
			t.Errorf("no earlier report, so no change: %+v", r)
		}
	}
}

// The page shows a section's rows and summary, not its lines, so the sentence
// that says why there is no change column has to be in the summary.
func TestWatchRows_TanksWithNoEarlierReportSayWhyInTheSummary(t *testing.T) {
	const why = "No earlier report to compare with, so changes are measured from the next one."
	in := baseInput()
	in.FirstRun = true
	report, _ := buildWatchReport(baseWatch(), in)
	s := sectionOf(t, report, "tanks")
	if s.Summary != why {
		t.Errorf("summary %q", s.Summary)
	}
	if got := s.Lines[len(s.Lines)-1]; got != why {
		t.Errorf("the line stays for the email and Mate: %q", got)
	}

	in.Previous = prevReportFor(t, nil)
	in.PreviousAt = in.Since
	report, _ = buildWatchReport(baseWatch(), in)
	if got := sectionOf(t, report, "tanks").Summary; got != "" {
		t.Errorf("with an earlier report there is nothing to explain: %q", got)
	}
}

func TestWatchRows_AnOldStoredReportStillLoads(t *testing.T) {
	old := `{"kind":"sections","generated_at":"x","boat_time":"x","since":"x","since_at":"x","sections":[{"name":"power","title":"Power","lines":["House bank: 87 %"]}]}`
	var r watchReport
	if err := json.Unmarshal([]byte(old), &r); err != nil {
		t.Fatal(err)
	}
	s := r.Sections[0]
	if len(s.Rows) != 0 || s.Summary != "" || len(s.NotChecked) != 0 || len(s.Lines) != 1 {
		t.Errorf("%+v", s)
	}
	raw, _ := json.Marshal(s)
	if strings.Contains(string(raw), "rows") || strings.Contains(string(raw), "not_checked") || strings.Contains(string(raw), "summary") {
		t.Errorf("new fields are omitted when empty: %s", raw)
	}
}

func alarmReportInput() watchReportInput {
	in := baseInput()
	in.Now = time.Date(2026, 11, 4, 6, 0, 0, 0, in.Loc)
	in.Since = time.Date(2026, 11, 3, 6, 0, 0, 0, in.Loc)
	return in
}

func TestWatchRows_AlarmSummaryAndRows(t *testing.T) {
	in := alarmReportInput()
	d := func(day, h, m int) time.Time { return time.Date(2026, 11, day, h, m, 0, 0, in.Loc) }
	in.Raised = []alarmRaisedGroup{
		{Title: "Collision Risk", Count: 211, First: d(3, 9, 47), Last: d(3, 16, 39), Sources: 27, SourceKind: alarmSourceAISClosestApproach,
			TopSources: []alarmSourceCount{{"Delphi", 13}, {"Chantelleiv", 12}, {"Rummage", 12}}},
		{Title: "Silent sensor source", Count: 1, First: d(3, 22, 0), Last: d(3, 22, 0)},
		{Title: "Tide Alarm", Count: 5, First: d(4, 1, 0), Last: d(4, 1, 0)},
	}
	in.Status.Alarms.Active = []vesselStatusAlarm{{Title: "Tide Alarm"}}
	in.Status.Alarms.ActiveCount = 1
	report, _ := buildWatchReport(baseWatch(), in)
	s := sectionOf(t, report, "alarms")
	if s.Summary != "217 raised, active now: Tide Alarm" {
		t.Errorf("summary %q", s.Summary)
	}
	if r := rowOf(t, s, "Collision Risk"); r.Value != "211" || r.When != "yesterday 9.47 AM to 4.39 PM" || r.Detail != "27 vessels; most Delphi 13, Chantelleiv 12, Rummage 12" {
		t.Errorf("collision %+v", r)
	}
	if r := rowOf(t, s, "Silent sensor source"); r.Value != "1" || r.When != "yesterday 10.00 PM" || r.Detail != "" {
		t.Errorf("silent %+v", r)
	}
	if r := rowOf(t, s, "Tide Alarm"); r.Value != "5" || r.When != "1.00 AM" || r.Detail != "active now" {
		t.Errorf("tide %+v", r)
	}
	if len(s.NotChecked) != 0 {
		t.Errorf("not checked %q", s.NotChecked)
	}

	in.Status.Alarms.Active, in.Status.Alarms.ActiveCount = []vesselStatusAlarm{}, 0
	report, _ = buildWatchReport(baseWatch(), in)
	if got := sectionOf(t, report, "alarms").Summary; got != "217 raised, none active now" {
		t.Errorf("summary %q", got)
	}
}

// The page shows the rows and the summary, not the lines, so an alarm that is
// active now but was not raised in the window is named in the summary.
func TestWatchRows_AnActiveAlarmNotRaisedInTheWindowIsNamedInTheSummary(t *testing.T) {
	in := alarmReportInput()
	in.Raised = []alarmRaisedGroup{{Title: "Tide Alarm", Count: 1, First: in.Since, Last: in.Since}}
	in.Status.Alarms.Active = []vesselStatusAlarm{{Title: "Bilge pump"}}
	in.Status.Alarms.ActiveCount = 1
	report, _ := buildWatchReport(baseWatch(), in)
	if got := sectionOf(t, report, "alarms").Summary; got != "1 raised, active now: Bilge pump" {
		t.Errorf("summary %q", got)
	}

	in.Raised = nil
	in.Status.Alarms.Active = []vesselStatusAlarm{{Title: "A"}, {Title: "B"}}
	in.Status.Alarms.ActiveCount = 2
	report, _ = buildWatchReport(baseWatch(), in)
	if got := sectionOf(t, report, "alarms").Summary; got != "None raised, active now: A, B" {
		t.Errorf("summary %q", got)
	}

	in.Status.Alarms.Active = nil
	for i := 0; i < watchAlarmTitleCap+2; i++ {
		in.Status.Alarms.Active = append(in.Status.Alarms.Active, vesselStatusAlarm{Title: "Alarm " + string(rune('A'+i))})
	}
	in.Status.Alarms.ActiveCount = len(in.Status.Alarms.Active)
	report, _ = buildWatchReport(baseWatch(), in)
	want := "None raised, active now: " + strings.Join(capList([]string{"Alarm A", "Alarm B", "Alarm C", "Alarm D", "Alarm E", "Alarm F", "Alarm G"}, watchAlarmTitleCap), ", ")
	if got := sectionOf(t, report, "alarms").Summary; got != want || !strings.HasSuffix(got, ", and 2 more") {
		t.Errorf("summary %q want %q", got, want)
	}
}

func TestWatchAlarmFinding_IsShortAndKeepsItsRuleKey(t *testing.T) {
	in := alarmReportInput()
	d := func(day, h, m int) time.Time { return time.Date(2026, 11, day, h, m, 0, 0, in.Loc) }
	in.Raised = []alarmRaisedGroup{
		{Title: "Collision Risk", Count: 211, First: d(3, 9, 47), Last: d(3, 16, 39), Sources: 27, SourceKind: alarmSourceAISClosestApproach,
			TopSources: []alarmSourceCount{{"Delphi", 13}}},
		{Title: "Tide Alarm", Count: 6, First: d(3, 10, 0), Last: d(3, 11, 0)},
	}
	_, findings := buildWatchReport(baseWatch(), in)
	got := findingsFor(findings, "alarms")
	want := "217 alarms raised since yesterday 6.00 AM. Collision Risk 211 times, from 27 vessels. No alarms active now."
	if len(got) != 1 || got[0].Text != want || got[0].Rule != "alarms_raised" {
		t.Fatalf("finding %+v", got)
	}

	// Active alarms are named; a list that could not be read says nothing.
	in.Status.Alarms.Active = []vesselStatusAlarm{{Title: "A"}, {Title: "B"}}
	in.Status.Alarms.ActiveCount = 2
	_, findings = buildWatchReport(baseWatch(), in)
	if got := findingsFor(findings, "alarms")[0].Text; !strings.HasSuffix(got, " 2 active now: A, B.") {
		t.Errorf("finding %q", got)
	}
	in.Status, in.StatusProblem = nil, "no live instrument data"
	_, findings = buildWatchReport(baseWatch(), in)
	if got := findingsFor(findings, "alarms")[0].Text; strings.Contains(got, "active") || !strings.HasSuffix(got, "from 27 vessels.") {
		t.Errorf("finding %q", got)
	}
}

func TestWatchAlarmFinding_OneAlarm(t *testing.T) {
	in := baseInput()
	at := time.Date(2026, 10, 29, 5, 51, 0, 0, in.Loc)
	in.Raised = []alarmRaisedGroup{{Title: "Silent sensor source", Count: 1, First: at, Last: at}}
	_, findings := buildWatchReport(baseWatch(), in)
	got := findingsFor(findings, "alarms")
	want := "1 alarm raised since 3.00 AM. Silent sensor source, once. No alarms active now."
	if len(got) != 1 || got[0].Text != want {
		t.Fatalf("finding %+v", got)
	}
}

func TestWatchRows_AlarmsBeyondTheCapAreCountedAsOthers(t *testing.T) {
	in := alarmReportInput()
	for i := 0; i < 7; i++ {
		in.Raised = append(in.Raised, alarmRaisedGroup{Title: "Alarm " + string(rune('A'+i)), Count: 1, First: in.Since, Last: in.Since})
	}
	report, _ := buildWatchReport(baseWatch(), in)
	s := sectionOf(t, report, "alarms")
	if len(s.Rows) != 6 {
		t.Fatalf("five titles and the rest: %+v", s.Rows)
	}
	if r := rowOf(t, s, "Other alarms"); r.Value != "2" {
		t.Errorf("others %+v", r)
	}
}

func TestWatchRows_NoAlarmsRaisedHasASummaryAndNoRows(t *testing.T) {
	in := alarmReportInput()
	report, _ := buildWatchReport(baseWatch(), in)
	s := sectionOf(t, report, "alarms")
	if s.Summary != "None raised, none active now" || len(s.Rows) != 0 {
		t.Errorf("%+v", s)
	}
}

func TestWatchRows_ActiveAlarmsUnavailableIsAnUncheckedItem(t *testing.T) {
	in := alarmReportInput()
	in.Status, in.StatusProblem = nil, "no live instrument data"
	in.Raised = []alarmRaisedGroup{{Title: "Tide Alarm", Count: 2, First: in.Since, Last: in.Since.Add(time.Hour)}}
	report, _ := buildWatchReport(baseWatch(), in)
	s := sectionOf(t, report, "alarms")
	if len(s.NotChecked) != 1 || s.NotChecked[0] != "Active alarms: no live instrument data" {
		t.Errorf("not checked %q", s.NotChecked)
	}
	if strings.Contains(s.Summary, "active now") {
		t.Errorf("an unknown is not none: %q", s.Summary)
	}
}

func TestWatchRows_WeatherRows(t *testing.T) {
	in := baseInput()
	in.Stay = watchStayForecast{Name: "Mackay Marina", MaxWindKn: 14, MaxWindDay: "Wed 4 Nov", NoGoKn: 15}
	report, _ := buildWatchReport(baseWatch(), in)
	s := sectionOf(t, report, "weather")
	if r := rowOf(t, s, "Pressure"); r.Value != "1013 hPa" {
		t.Errorf("pressure %+v", r)
	}
	if r := rowOf(t, s, "Pressure change in 3 hours"); r.Value != "-1 hPa" || r.NotChecked || r.Flagged {
		t.Errorf("change %+v", r)
	}
	if r := rowOf(t, s, "True wind"); r.Value != "12 kn, gust 18 kn" {
		t.Errorf("wind %+v", r)
	}
	if r := rowOf(t, s, "Forecast for Mackay Marina"); r.Value != "up to 14 kn on Wed 4 Nov, under your lowest no-go wind of 15 kn" || r.Flagged {
		t.Errorf("forecast %+v", r)
	}
	if len(s.NotChecked) != 0 {
		t.Errorf("not checked %q", s.NotChecked)
	}

	in.Stay.MaxWindKn = 26
	report, _ = buildWatchReport(baseWatch(), in)
	if r := rowOf(t, sectionOf(t, report, "weather"), "Forecast for Mackay Marina"); r.Value != "up to 26 kn on Wed 4 Nov, over your lowest no-go wind of 15 kn" || !r.Flagged {
		t.Errorf("forecast %+v", r)
	}

	in.Stay.NoGoKn = 0
	report, _ = buildWatchReport(baseWatch(), in)
	r := rowOf(t, sectionOf(t, report, "weather"), "Forecast for Mackay Marina")
	if r.Value != "up to 26 kn on Wed 4 Nov" || !strings.Contains(r.Detail, "no passage limits") || r.Flagged || r.NotChecked {
		t.Errorf("without limits nothing is judged: %+v", r)
	}

	in.Stay = watchStayForecast{Name: "Mackay Marina", Problem: "the forecast does not reach the stay's dates yet"}
	report, _ = buildWatchReport(baseWatch(), in)
	s = sectionOf(t, report, "weather")
	if r := rowOf(t, s, "Forecast for Mackay Marina"); r.Value != "Not checked" || !r.NotChecked {
		t.Errorf("forecast %+v", r)
	}
	if len(s.NotChecked) != 1 || s.NotChecked[0] != "Forecast for Mackay Marina: the forecast does not reach the stay's dates yet" {
		t.Errorf("not checked %q", s.NotChecked)
	}
}

func TestWatchRows_PressureFallFlagsItsRow(t *testing.T) {
	in := baseInput()
	in.Status.Surroundings.PressureChange3h = statusValue{Value: fp(-340), Unit: "Pa", Display: fp(-3.4), DisplayUnit: "hPa"}
	report, _ := buildWatchReport(baseWatch(), in)
	if r := rowOf(t, sectionOf(t, report, "weather"), "Pressure change in 3 hours"); !r.Flagged || r.Value != "-3.4 hPa" {
		t.Errorf("%+v", r)
	}
}

func TestWatchRows_MissingPressureChangeIsNotChecked(t *testing.T) {
	in := baseInput()
	in.Status.Surroundings.PressureChange3h = missingValue("pressure history still filling (40 min of 3 h)")
	report, _ := buildWatchReport(baseWatch(), in)
	s := sectionOf(t, report, "weather")
	if r := rowOf(t, s, "Pressure change in 3 hours"); r.Value != "Not checked" || !r.NotChecked {
		t.Errorf("%+v", r)
	}
	if len(s.NotChecked) != 1 || s.NotChecked[0] != "Pressure change in 3 hours: pressure history still filling (40 min of 3 h)" {
		t.Errorf("not checked %q", s.NotChecked)
	}
}

func TestWatchRows_MissingPressureAndWindReadingsSayWhy(t *testing.T) {
	in := baseInput()
	in.Status.Surroundings.Pressure = missingValue("no barometer reading")
	in.Status.Surroundings.TrueWindSpeed = missingValue("no wind reading")
	report, _ := buildWatchReport(baseWatch(), in)
	s := sectionOf(t, report, "weather")
	if r := rowOf(t, s, "Pressure"); r.Value != "Not available (no barometer reading)" || r.NotChecked {
		t.Errorf("pressure %+v", r)
	}
	if r := rowOf(t, s, "True wind"); r.Value != "Not available (no wind reading)" || r.NotChecked {
		t.Errorf("wind %+v", r)
	}
	// The fall rule reads only the 3-hour change, which is there.
	if len(s.NotChecked) != 0 {
		t.Errorf("only the 3-hour change feeds a rule: %q", s.NotChecked)
	}

	// A boat with no barometer has no change either: one entry, from the change.
	in.Status.Surroundings.PressureChange3h = missingValue("no barometer reading")
	report, _ = buildWatchReport(baseWatch(), in)
	s = sectionOf(t, report, "weather")
	if r := rowOf(t, s, "Pressure"); r.Value != "Not available (no barometer reading)" || r.NotChecked {
		t.Errorf("pressure %+v", r)
	}
	if got := strings.Join(s.NotChecked, "|"); got != "Pressure change in 3 hours: no barometer reading" {
		t.Errorf("one entry for a boat with no barometer: %q", got)
	}
}

func TestWatchRows_AnUnknownAnchorDistanceIsNotChecked(t *testing.T) {
	in := baseInput()
	in.Status.Boat.Anchor = vesselStatusAnchor{Down: true, RadiusM: 45, DistanceMissing: "no boat position, so the distance from the anchor is unknown"}
	report, _ := buildWatchReport(baseWatch(), in)
	s := sectionOf(t, report, "anchor")
	if len(s.NotChecked) != 1 || s.NotChecked[0] != "Distance from the anchor: no boat position, so the distance from the anchor is unknown" {
		t.Errorf("not checked %q", s.NotChecked)
	}
	if len(s.Lines) != 2 {
		t.Errorf("anchor stays as lines: %q", s.Lines)
	}
	calm, _ := buildWatchReport(baseWatch(), baseInput())
	if got := sectionOf(t, calm, "anchor"); len(got.NotChecked) != 0 {
		t.Errorf("a known distance checks out: %+v", got)
	}
}
