package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The deterministic watch report (ADR 0227): figures, and findings reached by
// rules the operator can read.

func reportBoatTime() (time.Time, *time.Location) {
	loc := time.FixedZone("AEST", 10*3600)
	return time.Date(2026, 10, 29, 6, 0, 0, 0, loc), loc
}

func val(v float64, unit string) statusValue {
	return statusValue{Value: &v, Unit: unit, Display: &v, DisplayUnit: unit}
}

func calmStatus() *vesselStatusResult {
	return &vesselStatusResult{
		Units: "metric",
		Power: vesselStatusPower{
			HouseBank: vesselStatusHouseBank{SocPercent: val(87, "%"), Voltage: val(13.1, "V"), Current: val(-4.2, "A")},
			SolarW:    val(120, "W"), DCLoadW: val(55, "W"),
		},
		Tanks: vesselStatusTanks{Tanks: []vesselStatusTank{
			{Name: "Fresh Water", Kind: "water", Level: val(62, "%")},
			{Name: "Diesel", Kind: "fuel", Level: val(80, "%")},
		}},
		Surroundings: vesselStatusSurroundings{
			Pressure: val(1013, "hPa"), PressureChange3h: statusValue{Value: fp(-100), Unit: "Pa", Display: fp(-1), DisplayUnit: "hPa"},
			TrueWindSpeed: val(12, "kn"), TrueWindGust: val(18, "kn"),
		},
		Boat:      vesselStatusBoat{Anchor: vesselStatusAnchor{Down: true, RadiusM: 45, DistanceFromAnchorM: fp(32)}},
		Alarms:    vesselStatusAlarms{Active: []vesselStatusAlarm{}, Ignored: []string{}},
		Anomalies: vesselStatusAnomalies{},
	}
}

func allSections() []string { return append([]string(nil), watchSectionOrder...) }

func sectionOf(t *testing.T, r watchReport, name string) watchReportSection {
	t.Helper()
	for _, s := range r.Sections {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("report has no %s section: %+v", name, r.Sections)
	return watchReportSection{}
}

func findingsFor(f []watchFinding, section string) []watchFinding {
	var out []watchFinding
	for _, x := range f {
		if x.Section == section {
			out = append(out, x)
		}
	}
	return out
}

func baseWatch() savedWatch {
	return savedWatch{Name: "Morning", Kind: watchKindScheduled, Sections: allSections(), Thresholds: defaultWatchThresholds()}
}

func baseInput() watchReportInput {
	now, loc := reportBoatTime()
	return watchReportInput{Now: now, Loc: loc, Since: now.Add(-3 * time.Hour), Status: calmStatus()}
}

func TestWatchReport_CalmBoatHasFiguresAndNoFindings(t *testing.T) {
	report, findings := buildWatchReport(baseWatch(), baseInput())
	if len(findings) != 0 {
		t.Fatalf("a calm boat has no findings: %+v", findings)
	}
	if len(report.Sections) != 6 {
		t.Fatalf("sections %d", len(report.Sections))
	}
	power := sectionOf(t, report, "power")
	if !strings.Contains(strings.Join(power.Lines, "|"), "87 %") || !strings.Contains(strings.Join(power.Lines, "|"), "13.1 V") {
		t.Errorf("power lines %v", power.Lines)
	}
	if got := strings.Join(sectionOf(t, report, "anchor").Lines, "|"); !strings.Contains(got, "32 m") {
		t.Errorf("anchor lines %v", got)
	}
}

func TestWatchReport_OnlyTheChosenSectionsAppearInOrder(t *testing.T) {
	w := baseWatch()
	w.Sections = []string{watchSectionWeather, watchSectionPower}
	report, _ := buildWatchReport(w, baseInput())
	if len(report.Sections) != 2 || report.Sections[0].Name != "power" || report.Sections[1].Name != "weather" {
		t.Fatalf("sections %+v", report.Sections)
	}
}

func TestWatchReport_SinceTheLastRun(t *testing.T) {
	in := baseInput()
	report, _ := buildWatchReport(baseWatch(), in)
	if !strings.Contains(report.Since, "03:00") || report.SinceNote != "" {
		t.Errorf("since %q note %q", report.Since, report.SinceNote)
	}
	in.FirstRun = true
	report, _ = buildWatchReport(baseWatch(), in)
	if !strings.Contains(report.SinceNote, "first report") || !strings.Contains(report.SinceNote, "24 hours") {
		t.Errorf("a first run says so: %q", report.SinceNote)
	}
}

func TestWatchReport_AlarmsRaisedSinceTheLastRun(t *testing.T) {
	in := baseInput()
	at := in.Since.Add(40 * time.Minute)
	in.Raised = []alarmRaisedGroup{{Title: "Anchor Dragging", Count: 1, First: at, Last: at}, {Title: "House Bank Low", Count: 1, First: at.Add(20 * time.Minute), Last: at.Add(20 * time.Minute)}}
	_, findings := buildWatchReport(baseWatch(), in)
	got := findingsFor(findings, "alarms")
	if len(got) != 1 || got[0].Rule != "alarms_raised" || got[0].By != watchByRules {
		t.Fatalf("findings %+v", got)
	}
	if !strings.Contains(got[0].Text, "2 alarms raised since 03:00") || !strings.Contains(got[0].Text, "Anchor Dragging, once (03:40)") {
		t.Errorf("text %q", got[0].Text)
	}
	// A higher threshold is respected.
	w := baseWatch()
	w.Thresholds.AlarmsRaised = 3
	if _, f := buildWatchReport(w, in); len(findingsFor(f, "alarms")) != 0 {
		t.Error("two raised is under a threshold of three")
	}
}

func TestWatchReport_AlarmLogUnreadableMakesTheSectionUnavailable(t *testing.T) {
	in := baseInput()
	in.RaisedProblem = "the alarm log is not open"
	report, findings := buildWatchReport(baseWatch(), in)
	if s := sectionOf(t, report, "alarms"); s.Unavailable != "the alarm log is not open" {
		t.Errorf("alarms %+v", s)
	}
	if len(findings) != 0 {
		t.Errorf("no finding is invented: %+v", findings)
	}
}

func TestWatchReport_InstrumentIssues(t *testing.T) {
	in := baseInput()
	in.Status.Anomalies = vesselStatusAnomalies{Frozen: 1, SilentSources: 1, FrozenSensors: []string{"Depth has read 4.2 m for 3 hours"}, SilentSourceNames: []string{"Furuno GPS"}}
	_, findings := buildWatchReport(baseWatch(), in)
	got := findingsFor(findings, "instruments")
	if len(got) != 1 || !strings.Contains(got[0].Text, "Furuno GPS") || !strings.Contains(got[0].Text, "Depth has read") {
		t.Fatalf("findings %+v", got)
	}
	in.Status.Anomalies = vesselStatusAnomalies{Missing: "the sensor checks have not run yet"}
	report, f := buildWatchReport(baseWatch(), in)
	if s := sectionOf(t, report, "instruments"); s.Unavailable != "the sensor checks have not run yet" || len(f) != 0 {
		t.Errorf("section %+v findings %+v", s, f)
	}
}

func TestWatchReport_PowerOvernightDraw(t *testing.T) {
	in := baseInput()
	in.Status.Observations = []vesselObservation{{Kind: "overnight_house_draw", Summary: "On the last completed night the house bank drained at 1.20 % of charge an hour."}}
	_, findings := buildWatchReport(baseWatch(), in)
	got := findingsFor(findings, "power")
	if len(got) != 1 || got[0].Rule != "overnight_draw" || !strings.Contains(got[0].Text, "drained at 1.20") {
		t.Fatalf("findings %+v", got)
	}
}

func TestWatchReport_Anchor(t *testing.T) {
	in := baseInput()
	in.Status.Boat.Anchor = vesselStatusAnchor{Down: true, RadiusM: 45, DistanceFromAnchorM: fp(52)}
	_, findings := buildWatchReport(baseWatch(), in)
	got := findingsFor(findings, "anchor")
	if len(got) != 1 || got[0].Rule != "outside_anchor_radius" || !strings.Contains(got[0].Text, "52 m") || !strings.Contains(got[0].Text, "45 m") {
		t.Fatalf("findings %+v", got)
	}

	in.Status.Boat.Anchor = vesselStatusAnchor{}
	report, f := buildWatchReport(baseWatch(), in)
	if s := sectionOf(t, report, "anchor"); strings.Join(s.Lines, "|") != "No anchor watch set" || len(f) != 0 {
		t.Errorf("no anchor watch is not a finding: %+v %+v", s, f)
	}

	in.Status.Boat.Anchor = vesselStatusAnchor{Down: true, RadiusM: 45, DistanceMissing: "no boat position, so the distance from the anchor is unknown"}
	report, f = buildWatchReport(baseWatch(), in)
	if len(f) != 0 || !strings.Contains(strings.Join(sectionOf(t, report, "anchor").Lines, "|"), "not known") {
		t.Errorf("an unknown distance is said, not guessed: %+v", report)
	}

	in.Status = calmStatus()
	in.Status.Units = "imperial"
	in.Status.Boat.Anchor = vesselStatusAnchor{Down: true, RadiusM: 45, DistanceFromAnchorM: fp(52)}
	_, f = buildWatchReport(baseWatch(), in)
	if got := findingsFor(f, "anchor"); len(got) != 1 || !strings.Contains(got[0].Text, "171 ft") {
		t.Errorf("imperial figures: %+v", got)
	}
}

func TestWatchReport_TankChangeAgainstThePreviousReport(t *testing.T) {
	prevReport, _ := buildWatchReport(baseWatch(), baseInput())
	raw, _ := json.Marshal(prevReport)
	var prev watchReport
	if err := json.Unmarshal(raw, &prev); err != nil {
		t.Fatal(err)
	}

	in := baseInput()
	in.Previous, in.PreviousAt = &prev, in.Since
	in.Status.Tanks.Tanks[1].Level = val(66, "%") // diesel 80 -> 66
	in.Status.Tanks.Tanks[0].Level = val(55, "%") // water 62 -> 55, under the threshold
	_, findings := buildWatchReport(baseWatch(), in)
	got := findingsFor(findings, "tanks")
	if len(got) != 1 || got[0].Rule != "tank_change" || !strings.Contains(got[0].Text, "Diesel") || !strings.Contains(got[0].Text, "14 points") {
		t.Fatalf("findings %+v", got)
	}

	// A tank that filled is a change too.
	in.Status.Tanks.Tanks[1].Level = val(95, "%")
	_, findings = buildWatchReport(baseWatch(), in)
	if got := findingsFor(findings, "tanks"); len(got) != 1 || !strings.Contains(got[0].Text, "up 15 points") {
		t.Fatalf("findings %+v", got)
	}

	// The first run has nothing to compare with and says so.
	first := baseInput()
	first.FirstRun = true
	report, f := buildWatchReport(baseWatch(), first)
	if len(findingsFor(f, "tanks")) != 0 || !strings.Contains(strings.Join(sectionOf(t, report, "tanks").Lines, "|"), "No earlier report") {
		t.Errorf("first run: %+v", sectionOf(t, report, "tanks"))
	}
}

func TestWatchReport_TankLevelUnreadableIsNotAChange(t *testing.T) {
	prevReport, _ := buildWatchReport(baseWatch(), baseInput())
	in := baseInput()
	in.Previous, in.PreviousAt = &prevReport, in.Since
	in.Status.Tanks.Tanks[1].Level = missingValue("last updated 5 minutes ago")
	report, f := buildWatchReport(baseWatch(), in)
	if len(findingsFor(f, "tanks")) != 0 {
		t.Fatalf("no finding from a reading that is not there: %+v", f)
	}
	if got := strings.Join(sectionOf(t, report, "tanks").Lines, "|"); !strings.Contains(got, "Diesel: not available (last updated 5 minutes ago)") {
		t.Errorf("lines %q", got)
	}
}

func TestWatchReport_WeatherPressureFallAndForecast(t *testing.T) {
	in := baseInput()
	in.Status.Surroundings.PressureChange3h = statusValue{Value: fp(-340), Unit: "Pa", Display: fp(-3.4), DisplayUnit: "hPa"}
	_, findings := buildWatchReport(baseWatch(), in)
	got := findingsFor(findings, "weather")
	if len(got) != 1 || got[0].Rule != "pressure_fall" || !strings.Contains(got[0].Text, "3.4 hPa") {
		t.Fatalf("findings %+v", got)
	}

	in = baseInput()
	in.Stay = watchStayForecast{Name: "Coral Coast Marina", MaxWindKn: 26, MaxWindDay: "Tue 3 Nov", NoGoKn: 22}
	_, findings = buildWatchReport(baseWatch(), in)
	got = findingsFor(findings, "weather")
	if len(got) != 1 || got[0].Rule != "forecast_no_go" || !strings.Contains(got[0].Text, "Coral Coast Marina") || !strings.Contains(got[0].Text, "26 kn") || !strings.Contains(got[0].Text, "22 kn") {
		t.Fatalf("findings %+v", got)
	}

	in.Stay = watchStayForecast{Name: "Coral Coast Marina", MaxWindKn: 18, MaxWindDay: "Tue 3 Nov", NoGoKn: 22}
	report, f := buildWatchReport(baseWatch(), in)
	if len(findingsFor(f, "weather")) != 0 || !strings.Contains(strings.Join(sectionOf(t, report, "weather").Lines, "|"), "Coral Coast Marina") {
		t.Errorf("under the limit: %+v", report)
	}

	in.Stay = watchStayForecast{Name: "Coral Coast Marina", MaxWindKn: 26, MaxWindDay: "Tue 3 Nov"}
	report, f = buildWatchReport(baseWatch(), in)
	if len(findingsFor(f, "weather")) != 0 || !strings.Contains(strings.Join(sectionOf(t, report, "weather").Lines, "|"), "no passage limits") {
		t.Errorf("without a limit nothing is judged: %+v", report)
	}
}

func TestWatchReport_PressureChangeUnavailableIsSaid(t *testing.T) {
	in := baseInput()
	in.Status.Surroundings.PressureChange3h = missingValue("pressure history still filling (40 min of 3 h)")
	report, f := buildWatchReport(baseWatch(), in)
	if len(findingsFor(f, "weather")) != 0 || !strings.Contains(strings.Join(sectionOf(t, report, "weather").Lines, "|"), "still filling") {
		t.Errorf("%+v", sectionOf(t, report, "weather"))
	}
}

func TestWatchReport_NoLiveStatusLeavesEverySectionUnavailable(t *testing.T) {
	in := baseInput()
	in.Status, in.StatusProblem = nil, "no live instrument data"
	report, findings := buildWatchReport(baseWatch(), in)
	for _, s := range report.Sections {
		if s.Name == "alarms" {
			continue // the alarm log is separate from the instruments
		}
		if !strings.Contains(s.Unavailable, "no live instrument data") {
			t.Errorf("%s: %+v", s.Name, s)
		}
	}
	if len(findings) != 0 {
		t.Errorf("findings %+v", findings)
	}
}

func TestWatchThresholdsValidate(t *testing.T) {
	d := defaultWatchThresholds()
	if d.AlarmsRaised != 1 || d.InstrumentIssues != 1 || d.TankChangePoints != 10 || d.PressureFallHPa != 3 {
		t.Fatalf("defaults %+v", d)
	}
	for name, mod := range map[string]func(*watchThresholds){
		"alarms zero":        func(x *watchThresholds) { x.AlarmsRaised = 0 },
		"alarms huge":        func(x *watchThresholds) { x.AlarmsRaised = 1000 },
		"instruments zero":   func(x *watchThresholds) { x.InstrumentIssues = 0 },
		"tank change zero":   func(x *watchThresholds) { x.TankChangePoints = 0 },
		"tank change 101":    func(x *watchThresholds) { x.TankChangePoints = 101 },
		"pressure fall zero": func(x *watchThresholds) { x.PressureFallHPa = 0 },
		"pressure fall 50":   func(x *watchThresholds) { x.PressureFallHPa = 50 },
	} {
		x := defaultWatchThresholds()
		mod(&x)
		if err := x.validate(); err == nil {
			t.Errorf("%s should be refused", name)
		}
	}
	if err := d.validate(); err != nil {
		t.Error(err)
	}
}

func TestWatchReport_AlarmsAreGroupedByTitleAndMarkedActiveNow(t *testing.T) {
	in := baseInput()
	in.Since = in.Now.Add(-24 * time.Hour) // Wed 28 Oct 06:00
	in.FirstRun = true
	day := func(d, h, m int) time.Time { return time.Date(2026, 10, d, h, m, 0, 0, in.Now.Location()) }
	in.Raised = []alarmRaisedGroup{
		{Title: "Anchor Dragging", Count: 1, First: day(28, 7, 0), Last: day(28, 7, 0)},
		{Title: "House Bank Low", Count: 2, First: day(28, 20, 0), Last: day(28, 21, 0)},
		{Title: "Collision Profile Not Changed", Count: 46, First: day(28, 15, 42), Last: day(29, 2, 23)},
	}
	in.Status.Alarms.Active = []vesselStatusAlarm{{Title: "Collision Profile Not Changed"}}
	in.Status.Alarms.ActiveCount = 1
	report, findings := buildWatchReport(baseWatch(), in)
	s := sectionOf(t, report, "alarms")
	text := strings.Join(s.Lines, "|")
	for _, want := range []string{
		"49 alarms raised in the last 24 hours",
		"Collision Profile Not Changed, 46 times (first yesterday 15:42, last 02:23), active now",
		"House Bank Low, 2 times (first yesterday 20:00, last yesterday 21:00)",
		"Anchor Dragging, once (yesterday 07:00)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("lacks %q in %q", want, text)
		}
	}
	if strings.Contains(text, "House Bank Low, 2 times (first yesterday 20:00, last yesterday 21:00), active") || strings.Contains(text, "cleared") || strings.Contains(text, "still open") {
		t.Errorf("only an alarm in the active list is marked, and nothing is said about clearing: %q", text)
	}
	if strings.Index(text, "Collision") > strings.Index(text, "House Bank") || strings.Index(text, "House Bank") > strings.Index(text, "Anchor") {
		t.Errorf("ordered by count then latest: %q", text)
	}
	if strings.Contains(text, "raised since") {
		t.Errorf("a first run says the last 24 hours: %q", text)
	}
	raw, _ := json.Marshal(s.Figures)
	for _, want := range []string{`"raised":49`, `"others_count":0`, `"active_now":true`, `"active_now":false`, `"Collision Profile Not Changed"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("figures lack %s: %s", want, raw)
		}
	}
	if strings.Contains(string(raw), "cleared") || strings.Contains(string(raw), `"open"`) {
		t.Errorf("figures say nothing about clearing: %s", raw)
	}
	if got := findingsFor(findings, "alarms"); len(got) != 1 || !strings.Contains(got[0].Text, "Collision Profile Not Changed, 46 times") {
		t.Errorf("finding %+v", got)
	}
}

func TestWatchReport_AlarmTimesCarryTheirDayAndTheListIsCapped(t *testing.T) {
	in := baseInput()
	in.Since = in.Now.Add(-8 * 24 * time.Hour)
	for i := 0; i < 7; i++ {
		at := in.Now.Add(-time.Duration(i+1) * 24 * time.Hour)
		in.Raised = append(in.Raised, alarmRaisedGroup{Title: "Alarm " + string(rune('A'+i)), Count: 1, First: at, Last: at})
	}
	in.Raised[6].Count, in.Raised[5].Count = 4, 3
	report, _ := buildWatchReport(baseWatch(), in)
	s := sectionOf(t, report, "alarms")
	text := strings.Join(s.Lines, "|")
	for _, want := range []string{
		"raised since Wed 21 Oct 06:00",
		"Alarm A, once (yesterday 06:00)", "Alarm B, once (Tue 06:00)", "Alarm F, 3 times (first Fri 06:00", "Alarm G, 4 times (first Thu 22 Oct 06:00",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("lacks %q in %q", want, text)
		}
	}
	if strings.Contains(text, "Alarm E") || !strings.Contains(text, "and 2 more alarms") {
		t.Errorf("cap of five titles then a count of the rest: %q", text)
	}
	raw, _ := json.Marshal(s.Figures)
	if !strings.Contains(string(raw), `"others_count":2`) || strings.Contains(string(raw), "Alarm E") || !strings.Contains(string(raw), `"raised":12`) {
		t.Errorf("figures cap at five titles and count the rest: %s", raw)
	}
}

func TestWatchReport_RepeatedCollisionRiskSaysHowManyVessels(t *testing.T) {
	in := baseInput()
	in.FirstRun = true
	in.Since = in.Now.Add(-24 * time.Hour)
	first := time.Date(2026, 10, 28, 6, 45, 0, 0, in.Now.Location())
	last := time.Date(2026, 10, 29, 5, 10, 0, 0, in.Now.Location())
	in.Raised = []alarmRaisedGroup{
		{Title: "Collision Risk", Count: 111, First: first, Last: last, Sources: 31, SourceKind: alarmSourceAISClosestApproach,
			TopSources: []alarmSourceCount{{"Bella Breeza", 4}, {"Encore", 4}, {"Whitsunday Bullet", 4}}},
		{Title: "Tide Alarm", Count: 6, First: first, Last: last, Sources: 3},
		{Title: "House Bank Low", Count: 1, First: first, Last: first, Sources: 1},
	}
	before := watchFindingKey(watchFinding{Section: "alarms", Rule: "alarms_raised", By: watchByRules})
	report, findings := buildWatchReport(baseWatch(), in)
	text := strings.Join(sectionOf(t, report, "alarms").Lines, "|")
	for _, want := range []string{
		"Collision Risk, 111 times from 31 vessels, AIS closest approach (first yesterday 06:45, last 05:10; most: Bella Breeza 4, Encore 4, Whitsunday Bullet 4)",
		"Tide Alarm, 6 times from 3 sources (first",
		"House Bank Low, once (",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("lacks %q in %q", want, text)
		}
	}
	if strings.Contains(text, "mmsi") || strings.Contains(text, "<") {
		t.Errorf("no identifiers: %q", text)
	}
	raw, _ := json.Marshal(sectionOf(t, report, "alarms").Figures)
	for _, want := range []string{`"sources":31`, `"source_kind":"AIS closest approach"`, `"top_sources":[{"name":"Bella Breeza","count":4}`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("figures lack %s: %s", want, raw)
		}
	}
	got := findingsFor(findings, "alarms")
	if len(got) != 1 || !strings.Contains(got[0].Text, "from 31 vessels") || watchFindingKey(got[0]) != before {
		t.Errorf("finding %+v key %q want %q", got, watchFindingKey(got[0]), before)
	}
}

func TestWatchReport_AnUnnamedVesselCountsButIsNeverListed(t *testing.T) {
	in := baseInput()
	in.Raised = []alarmRaisedGroup{{Title: "Collision Risk", Count: 7, First: in.Now, Last: in.Now, Sources: 2, SourceKind: alarmSourceAISClosestApproach,
		TopSources: []alarmSourceCount{{"Encore", 2}}}}
	report, _ := buildWatchReport(baseWatch(), in)
	text := strings.Join(sectionOf(t, report, "alarms").Lines, "|")
	if !strings.Contains(text, "from 2 vessels") || !strings.Contains(text, "most: Encore 2)") || strings.Contains(text, "319123600") {
		t.Errorf("%q", text)
	}
}

func TestWatchReport_ASingleCollisionRiskNamesItsVesselAndTheFiguresAgree(t *testing.T) {
	in := baseInput()
	at := time.Date(2026, 10, 29, 5, 10, 0, 0, in.Now.Location())
	in.Raised = []alarmRaisedGroup{{Title: "Collision Risk", Count: 1, First: at, Last: at, Sources: 1, SourceKind: alarmSourceAISClosestApproach,
		TopSources: []alarmSourceCount{{"Encore", 1}}}}
	report, _ := buildWatchReport(baseWatch(), in)
	s := sectionOf(t, report, "alarms")
	text := strings.Join(s.Lines, "|")
	if !strings.Contains(text, "Collision Risk, once from Encore, AIS closest approach (05:10)") {
		t.Errorf("%q", text)
	}
	raw, _ := json.Marshal(s.Figures)
	if !strings.Contains(string(raw), `"top_sources":[{"name":"Encore","count":1}]`) || !strings.Contains(string(raw), `"sources":1`) {
		t.Errorf("figures %s", raw)
	}
	in.Raised[0].Count, in.Raised[0].TopSources[0].Count = 3, 3
	in.Raised[0].Last = at.Add(time.Hour)
	report, _ = buildWatchReport(baseWatch(), in)
	if got := strings.Join(sectionOf(t, report, "alarms").Lines, "|"); !strings.Contains(got, "Collision Risk, 3 times from Encore, AIS closest approach (first") || strings.Contains(got, "most:") {
		t.Errorf("%q", got)
	}
}

func TestWatchReport_AnUnnamedSingleVesselIsOneVessel(t *testing.T) {
	in := baseInput()
	in.Raised = []alarmRaisedGroup{{Title: "Collision Risk", Count: 1, First: in.Now, Last: in.Now, Sources: 1, SourceKind: alarmSourceAISClosestApproach}}
	report, _ := buildWatchReport(baseWatch(), in)
	if text := strings.Join(sectionOf(t, report, "alarms").Lines, "|"); !strings.Contains(text, "Collision Risk, once from 1 vessel, AIS closest approach (") {
		t.Errorf("%q", text)
	}
}
