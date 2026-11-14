package main

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

/*
The watch report (ADR 0227).

buildWatchReport is a pure function of what was read: the boat's status (the
same builders get_vessel_status uses), the alarms raised since the last run,
the previous report and the planned stay's forecast. Each section returns
figures in the operator's units and the findings its rules reach, or says
plainly why it cannot report. gatherWatchReportInput (watches_gather.go)
does the reading.

Findings are decided here, by rules the operator can read and the thresholds
they set. Mate's review (watches_review.go) may add to them and never
removes one.
*/

// watchThresholds turn a figure into a finding. Every value has a default,
// none of them tied to a particular boat.
type watchThresholds struct {
	// AlarmsRaised is how many alarms raised since the last run make a finding.
	AlarmsRaised int `json:"alarms_raised"`
	// InstrumentIssues is how many frozen, impossible or silent sensors make one.
	InstrumentIssues int `json:"instrument_issues"`
	// TankChangePoints is the change in a tank's level, in percentage points
	// since the last run, that makes one.
	TankChangePoints float64 `json:"tank_change_points"`
	// PressureFallHPa is the fall in barometric pressure over three hours, in
	// hectopascals, that makes one.
	PressureFallHPa float64 `json:"pressure_fall_hpa"`
}

func defaultWatchThresholds() watchThresholds {
	return watchThresholds{AlarmsRaised: 1, InstrumentIssues: 1, TankChangePoints: 10, PressureFallHPa: 3}
}

func (t watchThresholds) validate() error {
	if t.AlarmsRaised < 1 || t.AlarmsRaised > 100 {
		return fieldErr("alarms_raised", "must be a whole number from 1 to 100")
	}
	if t.InstrumentIssues < 1 || t.InstrumentIssues > 100 {
		return fieldErr("instrument_issues", "must be a whole number from 1 to 100")
	}
	if !(t.TankChangePoints >= 1 && t.TankChangePoints <= 100) {
		return fieldErr("tank_change_points", "must be from 1 to 100 percentage points")
	}
	if !(t.PressureFallHPa >= 0.5 && t.PressureFallHPa <= 30) {
		return fieldErr("pressure_fall_hpa", "must be from 0.5 to 30 hPa")
	}
	return nil
}

type watchReportSection struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Unavailable string   `json:"unavailable,omitempty"`
	Lines       []string `json:"lines,omitempty"`
	// Figures are values the next report compares with (tank levels).
	Figures map[string]any `json:"figures,omitempty"`
	// Rows, Summary and NotChecked are the section as the page lays it out
	// (ADR 0255). Lines stay as they were, for the email, Mate and any reader of
	// an older report; reports made before these fields have none of them.
	Rows    []watchReportRow `json:"rows,omitempty"`
	Summary string           `json:"summary,omitempty"`
	// NotChecked are the checks or rules that could not run, one sentence each
	// in the form "Label: why". A missing reading that no rule depends on is a
	// row, not a sentence here.
	NotChecked []string `json:"not_checked,omitempty"`
}

// watchReportRow is one figure of a section, split into the parts the page
// sets in columns.
type watchReportRow struct {
	Label  string `json:"label,omitempty"`
	Value  string `json:"value,omitempty"`
	When   string `json:"when,omitempty"`
	Detail string `json:"detail,omitempty"`
	// Change is the move since the last report, "−11", "+4" or "no change".
	Change string `json:"change,omitempty"`
	// Flagged says a rule reached a finding about this row.
	Flagged bool `json:"flagged,omitempty"`
	// NotChecked says the check behind this row could not run, so Value is not
	// a reading.
	NotChecked bool `json:"not_checked,omitempty"`
}

type watchReport struct {
	Kind        string `json:"kind"`
	GeneratedAt string `json:"generated_at"`
	BoatTime    string `json:"boat_time"`
	Since       string `json:"since"`
	// SinceAt is Since as a UTC instant, for a receiver that reads times.
	SinceAt   string               `json:"since_at"`
	SinceNote string               `json:"since_note,omitempty"`
	Sections  []watchReportSection `json:"sections"`
	// Unusual is the learned normal's suggestion (ADR 0276). It is a field of
	// the report, not a section and not a finding, so it is never counted as an
	// issue. Reports made before it have none.
	Unusual *watchUnusual `json:"unusual,omitempty"`
}

// watchStayForecast is what the planned stay's forecast says against the
// operator's no-go wind, read by the gatherer.
type watchStayForecast struct {
	// Name is empty when no stay is planned.
	Name string
	// Problem says why the forecast could not be read.
	Problem    string
	MaxWindKn  float64
	MaxWindDay string
	// NoGoKn is the lowest no-go wind the operator has set, 0 when none is.
	NoGoKn float64
}

// watchWindChanges are the wind changes in the forecast at the boat's own
// position (ADR 0287).
type watchWindChanges struct {
	Problem string
	Changes []windChange
	// LimitsProblem says why the changes were not judged against the
	// operator's limits; the changes are still listed, none prominent.
	LimitsProblem string
	// CheckedTo names the last hour read ("Thu 19 3 AM") when the hourly
	// forecast stops before the forecast does, and CheckedReason says why
	// ("the forecast skips an hour here"); both empty when the whole forecast
	// was read.
	CheckedTo     string
	CheckedReason string
}

type watchReportInput struct {
	Now      time.Time
	Loc      *time.Location
	Since    time.Time
	FirstRun bool

	// Status is the boat's status, nil with StatusProblem when it could not
	// be built.
	Status        *vesselStatusResult
	StatusProblem string
	// Raised are the alarms raised in [Since, Now]; RaisedProblem says why
	// they could not be read.
	Raised        []alarmRaisedGroup
	RaisedProblem string
	// Previous is the last run's report and PreviousAt its time.
	Previous    *watchReport
	PreviousAt  time.Time
	Stay        watchStayForecast
	WindChanges watchWindChanges
	// Unusual is the window scored against the learned normal, nil when the
	// watch does not check instruments. Its figures use Status.Units.
	Unusual *unusualResult
}

var watchSectionTitles = map[string]string{
	watchSectionPower: "Power", watchSectionAnchor: "Anchor", watchSectionAlarms: "Alarms",
	watchSectionTanks: "Tanks", watchSectionWeather: "Weather", watchSectionInstruments: "Instruments",
}

func buildWatchReport(w savedWatch, in watchReportInput) (watchReport, []watchFinding) {
	loc := in.Loc
	if loc == nil {
		loc = time.UTC
	}
	report := watchReport{
		Kind:        watchReportKindSections,
		GeneratedAt: in.Now.UTC().Format(time.RFC3339),
		BoatTime:    watchClock(in.Now, loc),
		Since:       watchClock(in.Since, loc),
		SinceAt:     in.Since.UTC().Format(time.RFC3339),
	}
	if in.FirstRun {
		report.SinceNote = "This is the first report, so it covers the last 24 hours."
	}
	want := map[string]bool{}
	for _, s := range w.Sections {
		want[s] = true
	}
	var findings []watchFinding
	for _, name := range watchSectionOrder {
		if !want[name] {
			continue
		}
		var s watchReportSection
		var f []watchFinding
		switch name {
		case watchSectionPower:
			s, f = watchPowerSection(w, in)
		case watchSectionAnchor:
			s, f = watchAnchorSection(w, in)
		case watchSectionAlarms:
			s, f = watchAlarmsSection(w, in, loc)
		case watchSectionTanks:
			s, f = watchTanksSection(w, in)
		case watchSectionWeather:
			s, f = watchWeatherSection(w, in)
		case watchSectionInstruments:
			s, f = watchInstrumentsSection(w, in)
		}
		s.Name, s.Title = name, watchSectionTitles[name]
		report.Sections = append(report.Sections, s)
		findings = append(findings, f...)
	}
	if findings == nil {
		findings = []watchFinding{}
	}
	if in.Unusual != nil {
		report.Unusual = watchUnusualFor(in, loc)
	}
	return report, findings
}

// watchUnusualFor words the learned normal's stretches in the units the boat's
// status gives. With stretches to word and no known units it says so and shows
// no figure, rather than guess a system.
func watchUnusualFor(in watchReportInput, loc *time.Location) *watchUnusual {
	system := ""
	if in.Status != nil {
		system = in.Status.Units
	}
	if system == "" && len(in.Unusual.Stretches) > 0 {
		why := in.StatusProblem
		if why == "" {
			why = "the boat's units are not known"
		}
		return &watchUnusual{Note: "Unusual readings could not be worded: " + endedSentence(why), JudgedMinutes: in.Unusual.JudgedMinutes}
	}
	return buildWatchUnusual(*in.Unusual, system, loc, in.Now)
}

// watchClock is a time on the boat's clock, with its zone.
func watchClock(t time.Time, loc *time.Location) string {
	return boatDayClockWithZone(t, loc)
}

func ruleFinding(section, rule, text string) watchFinding {
	return watchFinding{Section: section, Rule: rule, Text: text, By: watchByRules}
}

// watchValueText is a reading in the operator's units, "13.1 V".
func watchValueText(v statusValue) (string, bool) {
	if v.Missing != "" {
		return "", false
	}
	n, unit := v.Value, v.Unit
	if v.Display != nil {
		n, unit = v.Display, v.DisplayUnit
	}
	if n == nil {
		return "", false
	}
	text := strconv.FormatFloat(*n, 'f', -1, 64)
	if unit != "" {
		text += " " + unit
	}
	return text, true
}

// watchReading is a reading three ways: the line "Label: 13.1 V" or "Label:
// not available (reason)", the row value "13.1 V" or "Not available (reason)",
// and whether it has a value.
func watchReading(label string, v statusValue) (line, value string, ok bool) {
	if text, ok := watchValueText(v); ok {
		return label + ": " + text, text, true
	}
	reason := watchMissingReason(v)
	return label + ": not available (" + reason + ")", "Not available (" + reason + ")", false
}

// watchLabelled is "Label: 13.1 V", or "Label: not available (reason)".
func watchLabelled(label string, v statusValue) string {
	line, _, _ := watchReading(label, v)
	return line
}

// watchMissingReason is why a reading has no value, "no reading" when the
// status does not say.
func watchMissingReason(v statusValue) string {
	if v.Missing != "" {
		return v.Missing
	}
	return "no reading"
}

// watchRowValue is a reading for a row, "13.1 V", or "Not available (reason)".
func watchRowValue(v statusValue) string {
	_, value, _ := watchReading("", v)
	return value
}

// notChecked records a check that was not run: it adds "label: why" to the
// section's not-checked list and returns the row that says so.
func (s *watchReportSection) notChecked(label, why string) watchReportRow {
	s.NotChecked = append(s.NotChecked, label+": "+why)
	return watchReportRow{Label: label, Value: "Not checked", NotChecked: true}
}

// watchStatusProblem is why the boat's status is missing, with a default when
// the gatherer gave none.
func watchStatusProblem(problem string) string {
	if problem == "" {
		return "the boat's readings could not be read"
	}
	return problem
}

func watchNoStatus(problem string) watchReportSection {
	return watchReportSection{Unavailable: watchStatusProblem(problem)}
}

// ── power ───────────────────────────────────────────────────────────────

func watchPowerSection(_ savedWatch, in watchReportInput) (watchReportSection, []watchFinding) {
	if in.Status == nil {
		return watchNoStatus(in.StatusProblem), nil
	}
	p := in.Status.Power
	var s watchReportSection
	var parts []string
	for _, x := range []struct {
		label string
		v     statusValue
	}{{"charge", p.HouseBank.SocPercent}, {"voltage", p.HouseBank.Voltage}, {"current", p.HouseBank.Current}} {
		if text, ok := watchValueText(x.v); ok {
			parts = append(parts, text)
		}
	}
	// With no figure at all, the bank is as unavailable as its charge reading.
	bankLine, bank, _ := watchReading("House bank", p.HouseBank.SocPercent)
	if len(parts) > 0 {
		bank = strings.Join(parts, ", ")
		bankLine = "House bank: " + bank
	}
	s.Lines = append(s.Lines, bankLine, watchLabelled("Solar", p.SolarW), watchLabelled("DC load", p.DCLoadW))
	s.Rows = append(s.Rows,
		watchReportRow{Label: "House bank", Value: bank},
		watchReportRow{Label: "Solar", Value: watchRowValue(p.SolarW)},
		watchReportRow{Label: "DC load", Value: watchRowValue(p.DCLoadW)})
	for _, why := range in.Status.ObservationsUnchecked {
		s.Lines = append(s.Lines, "Not checked, "+why)
		if label, reason, found := strings.Cut(why, ": "); found {
			s.Rows = append(s.Rows, s.notChecked(upperFirstLetter(label), reason))
		} else {
			s.Rows = append(s.Rows, watchReportRow{Label: "Other check", Value: "Not checked", NotChecked: true})
			s.NotChecked = append(s.NotChecked, upperFirstLetter(why))
		}
	}
	var findings []watchFinding
	for _, o := range in.Status.Observations {
		if o.Kind == "overnight_house_draw" {
			f := ruleFinding(watchSectionPower, "overnight_draw", o.Summary)
			f.chart = overnightChartSpec(o)
			findings = append(findings, f)
			// The review is shown the nights behind the finding: which were
			// compared, which were left out and why, and where the boat was.
			if s.Figures == nil {
				s.Figures = map[string]any{}
			}
			s.Figures["overnight_draw_nights"] = map[string]any{
				"window":           o.Window,
				"charging_checked": o.ChargingChecked,
				"last_night":       map[string]any{"date": o.LastNightDate, "place": o.LastNightPlace},
				"earlier_nights":   o.Nights,
			}
		}
	}
	return s, findings
}

// ── anchor ──────────────────────────────────────────────────────────────

func watchLength(m float64, units string) string {
	if units == "imperial" {
		return fmt.Sprintf("%.0f ft", math.Round(m*3.28084))
	}
	return fmt.Sprintf("%.0f m", math.Round(m))
}

func watchAnchorSection(_ savedWatch, in watchReportInput) (watchReportSection, []watchFinding) {
	if in.Status == nil {
		return watchNoStatus(in.StatusProblem), nil
	}
	a := in.Status.Boat.Anchor
	units := in.Status.Units
	switch {
	case a.Missing != "":
		return watchReportSection{Unavailable: a.Missing}, nil
	case !a.Down:
		return watchReportSection{Lines: []string{"No anchor watch set"}}, nil
	case a.DistanceFromAnchorM == nil:
		why := a.DistanceMissing
		if why == "" {
			why = "no boat position"
		}
		return watchReportSection{Lines: []string{
			"Anchor watch set, radius " + watchLength(a.RadiusM, units),
			"Distance from the anchor is not known (" + why + ")",
		}, NotChecked: []string{"Distance from the anchor: " + why}}, nil
	}
	d := *a.DistanceFromAnchorM
	if d > a.RadiusM {
		text := fmt.Sprintf("The boat is %s from the anchor, outside the %s anchor watch radius.", watchLength(d, units), watchLength(a.RadiusM, units))
		return watchReportSection{Lines: []string{text}}, []watchFinding{ruleFinding(watchSectionAnchor, "outside_anchor_radius", text)}
	}
	return watchReportSection{Lines: []string{fmt.Sprintf("%s from the anchor, inside the %s radius", watchLength(d, units), watchLength(a.RadiusM, units))}}, nil
}

// ── alarms ──────────────────────────────────────────────────────────────

func watchAlarmsSection(w savedWatch, in watchReportInput, loc *time.Location) (watchReportSection, []watchFinding) {
	if in.RaisedProblem != "" {
		return watchReportSection{Unavailable: in.RaisedProblem}, nil
	}
	var s watchReportSection
	// activeNow is the state of the active list as the summary and the short
	// finding say it: "" when it could not be read.
	activeNow, activeSummary := "", ""
	if in.Status == nil {
		s.Lines = append(s.Lines, "Active alarms: not available ("+in.StatusProblem+")")
		s.NotChecked = append(s.NotChecked, "Active alarms: "+watchStatusProblem(in.StatusProblem))
	} else if n := len(in.Status.Alarms.Active); n == 0 {
		s.Lines = append(s.Lines, "No alarms active now")
		activeNow, activeSummary = "No alarms active now.", ", none active now"
	} else {
		titles := make([]string, n)
		for i, a := range in.Status.Alarms.Active {
			titles[i] = a.Title
		}
		s.Lines = append(s.Lines, fmt.Sprintf("%d active now: %s", in.Status.Alarms.ActiveCount, strings.Join(titles, ", ")))
		activeNow = fmt.Sprintf("%d active now: %s.", in.Status.Alarms.ActiveCount, strings.Join(capList(titles, watchAlarmTitleCap), ", "))
		// The page shows the rows and this summary, not the lines, so an active
		// alarm that was not raised in the window is named here.
		activeSummary = ", active now: " + strings.Join(capList(titles, watchAlarmTitleCap), ", ")
	}
	window := "since " + watchDayTime(in.Since, in.Now, loc)
	if in.FirstRun {
		window = "in the last 24 hours"
	}
	groups := append([]alarmRaisedGroup(nil), in.Raised...)
	total := 0
	for _, g := range groups {
		total += g.Count
	}
	if total == 0 {
		s.Lines = append(s.Lines, "None raised "+window)
		s.Summary = "None raised" + activeSummary
		return s, nil
	}
	s.Summary = fmt.Sprintf("%d raised%s", total, activeSummary)
	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].Count != groups[j].Count {
			return groups[i].Count > groups[j].Count
		}
		return groups[i].Last.After(groups[j].Last)
	})
	active := map[string]bool{}
	if in.Status != nil {
		for _, a := range in.Status.Alarms.Active {
			active[a.Title] = true
		}
	}
	var titles []map[string]any
	var parts []string
	shown := 0
	for i, g := range groups {
		if i >= watchAlarmTitleCap {
			break
		}
		entry := map[string]any{
			"title": g.Title, "count": g.Count, "active_now": active[g.Title],
			"first": watchClock(g.First, loc), "last": watchClock(g.Last, loc),
		}
		if g.Sources > 0 {
			entry["sources"] = g.Sources
			if g.SourceKind != "" {
				entry["source_kind"] = g.SourceKind.Label()
			}
			if len(g.TopSources) > 0 {
				entry["top_sources"] = g.TopSources
			}
		}
		titles = append(titles, entry)
		parts = append(parts, watchAlarmGroupText(g, active[g.Title], in.Now, loc))
		s.Rows = append(s.Rows, watchAlarmRow(g, active[g.Title], in.Now, loc))
		shown += g.Count
	}
	others := total - shown
	if others > 0 {
		parts = append(parts, fmt.Sprintf("and %d more %s", others, watchPlural(others, "alarm", "alarms")))
		s.Rows = append(s.Rows, watchReportRow{Label: "Other alarms", Value: strconv.Itoa(others)})
	}
	s.Figures = map[string]any{"raised": total, "others_count": others, "titles": titles}
	// The line keeps every title's sentence, for the email and Mate. The finding
	// is short: the page shows the detail as rows.
	s.Lines = append(s.Lines, fmt.Sprintf("%d %s raised %s: %s", total, watchPlural(total, "alarm", "alarms"), window, strings.Join(parts, "; ")))
	if total >= w.Thresholds.AlarmsRaised {
		return s, []watchFinding{ruleFinding(watchSectionAlarms, "alarms_raised", watchAlarmFindingText(total, window, groups[0], activeNow))}
	}
	return s, nil
}

// watchAlarmFindingText is the alarms_raised finding: the window, the top
// title with its count and sources, and the active state. The per-title detail
// is in the section's rows and lines.
func watchAlarmFindingText(total int, window string, top alarmRaisedGroup, activeNow string) string {
	text := fmt.Sprintf("%d %s raised %s.", total, watchPlural(total, "alarm", "alarms"), window)
	from := watchAlarmSourcesPhrase(top)
	if top.Count == 1 {
		if from != "" {
			from = " " + from
		}
		text += fmt.Sprintf(" %s, once%s.", top.Title, from)
	} else {
		if from != "" {
			from = ", " + from
		}
		text += fmt.Sprintf(" %s %d times%s.", top.Title, top.Count, from)
	}
	if activeNow != "" {
		text += " " + activeNow
	}
	return text
}

// watchAlarmRow is one alarm title as the page lays it out.
func watchAlarmRow(g alarmRaisedGroup, activeNow bool, now time.Time, loc *time.Location) watchReportRow {
	row := watchReportRow{Label: g.Title, Value: strconv.Itoa(g.Count), When: watchDayTime(g.First, now, loc)}
	if last := watchDayTime(g.Last, now, loc); g.Count > 1 && last != row.When {
		sameDay := g.First.In(loc).Format("2006-01-02") == g.Last.In(loc).Format("2006-01-02")
		if sameDay {
			last = boatClock(g.Last, loc)
		}
		row.When += " to " + last
	}
	var detail []string
	if d := watchAlarmSourcesDetail(g); d != "" {
		detail = append(detail, d)
	}
	if activeNow {
		detail = append(detail, "active now")
	}
	row.Detail = strings.Join(detail, "; ")
	return row
}

// watchAlarmSourcesDetail is what raised an alarm for a row: "27 vessels; most
// Delphi 13, Chantelleiv 12", or "Encore" when one named vessel did.
func watchAlarmSourcesDetail(g alarmRaisedGroup) string {
	text := strings.TrimPrefix(watchAlarmSourcesPhrase(g), "from ")
	if text != "" && len(g.TopSources) > 0 && g.Sources > 1 {
		text += "; most " + watchTopSourcesText(g)
	}
	return text
}

// watchTopSourcesText is the busiest sources with their counts, "Delphi 13,
// Chantelleiv 12".
func watchTopSourcesText(g alarmRaisedGroup) string {
	top := make([]string, len(g.TopSources))
	for i, s := range g.TopSources {
		top[i] = fmt.Sprintf("%s %d", s.Name, s.Count)
	}
	return strings.Join(top, ", ")
}

// watchAlarmTitleCap is how many alarm titles a report names before it
// counts the rest.
const watchAlarmTitleCap = 5

// watchDayTime is a time on the boat's clock that names its day when it is not
// today: "3.42 PM", "yesterday 3.42 PM", "Tue 3.42 PM" within the last week,
// and "Wed 21 Oct 3.42 PM" beyond it.
func watchDayTime(t, now time.Time, loc *time.Location) string {
	t, now = t.In(loc), now.In(loc)
	day := func(x time.Time) time.Time { return time.Date(x.Year(), x.Month(), x.Day(), 12, 0, 0, 0, loc) }
	switch days := int(day(now).Sub(day(t)).Hours()/24 + 0.5); {
	case days <= 0:
		return boatClock(t, loc)
	case days == 1:
		return "yesterday " + boatClock(t, loc)
	case days < 7:
		return boatWeekdayClock(t, loc)
	default:
		return boatDayClock(t, loc)
	}
}

// watchAlarmGroupText is one title's line. It says "active now" only when the
// alarm is in the active list: the log's cleared times are not trusted to say
// an alarm is over.
func watchAlarmGroupText(g alarmRaisedGroup, activeNow bool, now time.Time, loc *time.Location) string {
	from := watchAlarmSourcesText(g)
	if from != "" {
		from = " " + from
	}
	var text string
	if g.Count == 1 {
		text = fmt.Sprintf("%s, once%s (%s)", g.Title, from, watchDayTime(g.First, now, loc))
	} else {
		times := fmt.Sprintf("first %s, last %s", watchDayTime(g.First, now, loc), watchDayTime(g.Last, now, loc))
		if len(g.TopSources) > 0 && g.Sources > 1 {
			times += "; most: " + watchTopSourcesText(g)
		}
		text = fmt.Sprintf("%s, %d times%s (%s)", g.Title, g.Count, from, times)
	}
	if activeNow {
		text += ", active now"
	}
	return text
}

// watchAlarmSourcesText says what raised a repeated alarm: "from 31 vessels,
// AIS closest approach", or "from Encore, AIS closest approach" when one named
// vessel raised it. A lone source adds nothing to an alarm that is not a
// vessel's, so it is left out. It reads the same fields the report's figures
// carry, so the line and Mate's figures agree.
func watchAlarmSourcesText(g alarmRaisedGroup) string {
	text := watchAlarmSourcesPhrase(g)
	if text == "" {
		return ""
	}
	if label := g.SourceKind.Label(); label != "" {
		text += ", " + label
	}
	return text
}

// watchAlarmSourcesPhrase is "from 27 vessels" or "from Encore", with no
// description of the kind of source.
func watchAlarmSourcesPhrase(g alarmRaisedGroup) string {
	vessels := g.SourceKind == alarmSourceAISClosestApproach
	if g.Sources < 1 || (g.Sources < 2 && !vessels) {
		return ""
	}
	switch {
	case g.Sources == 1 && len(g.TopSources) == 1:
		return "from " + g.TopSources[0].Name
	case vessels:
		return fmt.Sprintf("from %d %s", g.Sources, watchPlural(g.Sources, "vessel", "vessels"))
	default:
		return fmt.Sprintf("from %d sources", g.Sources)
	}
}

func watchPlural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func capList(items []string, max int) []string {
	if len(items) <= max {
		return items
	}
	out := append([]string(nil), items[:max]...)
	return append(out, fmt.Sprintf("and %d more", len(items)-max))
}

// ── tanks ───────────────────────────────────────────────────────────────

type watchTankFigures struct {
	Levels map[string]float64 `json:"levels"`
}

func watchPreviousTankLevels(prev *watchReport) (map[string]float64, bool) {
	if prev == nil {
		return nil, false
	}
	for _, s := range prev.Sections {
		if s.Name != watchSectionTanks || s.Figures == nil {
			continue
		}
		raw, err := json.Marshal(s.Figures)
		if err != nil {
			return nil, false
		}
		var f watchTankFigures
		if json.Unmarshal(raw, &f) != nil || len(f.Levels) == 0 {
			return nil, false
		}
		return f.Levels, true
	}
	return nil, false
}

func watchTanksSection(w savedWatch, in watchReportInput) (watchReportSection, []watchFinding) {
	if in.Status == nil {
		return watchNoStatus(in.StatusProblem), nil
	}
	if in.Status.Tanks.Missing != "" {
		return watchReportSection{Unavailable: in.Status.Tanks.Missing}, nil
	}
	loc := in.Loc
	if loc == nil {
		loc = time.UTC
	}
	prev, havePrev := watchPreviousTankLevels(in.Previous)
	levels := map[string]float64{}
	var s watchReportSection
	var findings []watchFinding
	type tankRow struct {
		rank int
		row  watchReportRow
	}
	var rows []tankRow
	for _, tank := range in.Status.Tanks.Tanks {
		s.Lines = append(s.Lines, watchLabelled(tank.Name, tank.Level))
		row := tankRow{rank: watchTankKindRank(tank.Kind), row: watchReportRow{Label: tank.Name}}
		if tank.Level.Value == nil {
			row.row = s.notChecked(tank.Name+" level", watchMissingReason(tank.Level))
			row.row.Label, row.row.Value = tank.Name, "Not available"
			rows = append(rows, row)
			continue
		}
		now := *tank.Level.Value
		levels[tank.Name] = now
		row.row.Value = fmt.Sprintf("%.0f %%", now)
		was, ok := prev[tank.Name]
		if havePrev && ok {
			row.row.Change = watchTankChange(now - was)
			row.row.Flagged = math.Abs(now-was) >= w.Thresholds.TankChangePoints
		}
		rows = append(rows, row)
		if !havePrev || !ok {
			continue
		}
		delta := now - was
		if math.Abs(delta) < w.Thresholds.TankChangePoints {
			continue
		}
		dir := "up"
		if delta < 0 {
			dir = "down"
		}
		f := ruleFinding(watchSectionTanks, "tank_change", fmt.Sprintf(
			"%s is %s %.0f points since %s (%.0f %% to %.0f %%).", tank.Name, dir, math.Abs(delta), boatClock(in.PreviousAt, loc), was, now))
		f.chart = tankChartSpec(tank, in)
		findings = append(findings, f)
	}
	if !havePrev {
		const noEarlier = "No earlier report to compare with, so changes are measured from the next one."
		s.Lines = append(s.Lines, noEarlier)
		// The page shows the rows and the summary, not the lines.
		s.Summary = noEarlier
	}
	// The page reads water, fuel, then waste, then anything else; the lines keep
	// the order the boat's equipment gives.
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].rank < rows[j].rank })
	for _, r := range rows {
		s.Rows = append(s.Rows, r.row)
	}
	s.Figures = map[string]any{"levels": levels}
	return s, findings
}

// watchTankKindRank orders tank kinds for the rows: fresh water, fuel, waste,
// then any other.
func watchTankKindRank(kind string) int {
	switch kind {
	case tankKindWater:
		return 0
	case tankKindFuel:
		return 1
	case tankKindWaste:
		return 2
	}
	return 3
}

// watchTankChange is a tank's move since the last report in whole points, with
// a real minus sign, or "no change" when it rounds to nothing.
func watchTankChange(delta float64) string {
	r := math.Round(delta)
	switch {
	case r == 0:
		return "no change"
	case r < 0:
		return fmt.Sprintf("−%.0f", -r)
	}
	return fmt.Sprintf("+%.0f", r)
}

// ── weather ─────────────────────────────────────────────────────────────

func watchWeatherSection(w savedWatch, in watchReportInput) (watchReportSection, []watchFinding) {
	if in.Status == nil {
		return watchNoStatus(in.StatusProblem), nil
	}
	sur := in.Status.Surroundings
	var s watchReportSection
	var findings []watchFinding
	pressureLine, pressureValue, _ := watchReading("Pressure", sur.Pressure)
	changeLine, changeValue, changeOK := watchReading("Pressure change in 3 hours", sur.PressureChange3h)
	windLine, windValue, windOK := watchReading("True wind", sur.TrueWindSpeed)
	if gust, ok := watchValueText(sur.TrueWindGust); windOK && ok {
		windLine += ", gust " + gust
		windValue += ", gust " + gust
	}
	s.Lines = append(s.Lines, pressureLine, changeLine, windLine)
	// The fall rule reads only the 3-hour change, so a missing pressure is not a
	// check left unrun: its row says "Not available (reason)" and that is all.
	pressure := watchReportRow{Label: "Pressure", Value: pressureValue}
	change := watchReportRow{Label: "Pressure change in 3 hours", Value: changeValue}
	if !changeOK {
		change = s.notChecked("Pressure change in 3 hours", watchMissingReason(sur.PressureChange3h))
	}
	fell := sur.PressureChange3h.Value != nil && *sur.PressureChange3h.Value <= -w.Thresholds.PressureFallHPa*100
	change.Flagged = fell
	s.Rows = append(s.Rows, pressure, change, watchReportRow{Label: "True wind", Value: windValue})
	if fell {
		change := sur.PressureChange3h
		unit := change.DisplayUnit
		fall := 0.0
		if change.Display != nil {
			fall = math.Abs(*change.Display)
		}
		text := fmt.Sprintf("Pressure is down %s %s in 3 hours", strconv.FormatFloat(fall, 'f', -1, 64), unit)
		if now, ok := watchValueText(sur.Pressure); ok {
			text += ", now " + now
		}
		f := ruleFinding(watchSectionWeather, "pressure_fall", text+".")
		f.chart = pressureChartSpec(sur, w.Thresholds.PressureFallHPa, in.Now)
		findings = append(findings, f)
	}
	stay := in.Stay
	switch {
	case stay.Name == "":
	case stay.Problem != "":
		s.Lines = append(s.Lines, "Forecast for "+stay.Name+" was not read: "+stay.Problem)
		s.Rows = append(s.Rows, s.notChecked("Forecast for "+stay.Name, stay.Problem))
	case stay.NoGoKn <= 0:
		s.Lines = append(s.Lines, fmt.Sprintf("Forecast for %s: wind up to %.0f kn on %s. There are no passage limits set, so it is not judged.", stay.Name, stay.MaxWindKn, stay.MaxWindDay))
		// Not a check that failed: the operator has set no limit to judge against.
		s.Rows = append(s.Rows, watchReportRow{Label: "Forecast for " + stay.Name,
			Value:  fmt.Sprintf("up to %.0f kn on %s", stay.MaxWindKn, stay.MaxWindDay),
			Detail: "There are no passage limits set, so it is not judged."})
	case stay.MaxWindKn >= stay.NoGoKn:
		text := fmt.Sprintf("Forecast wind at %s reaches %.0f kn on %s, over your lowest no-go wind of %.0f kn.", stay.Name, stay.MaxWindKn, stay.MaxWindDay, stay.NoGoKn)
		s.Lines = append(s.Lines, text)
		s.Rows = append(s.Rows, watchReportRow{Label: "Forecast for " + stay.Name, Flagged: true,
			Value: fmt.Sprintf("up to %.0f kn on %s, over your lowest no-go wind of %.0f kn", stay.MaxWindKn, stay.MaxWindDay, stay.NoGoKn)})
		findings = append(findings, ruleFinding(watchSectionWeather, "forecast_no_go", text))
	default:
		s.Lines = append(s.Lines, fmt.Sprintf("Forecast for %s: wind up to %.0f kn on %s, under your lowest no-go wind of %.0f kn.", stay.Name, stay.MaxWindKn, stay.MaxWindDay, stay.NoGoKn))
		s.Rows = append(s.Rows, watchReportRow{Label: "Forecast for " + stay.Name,
			Value: fmt.Sprintf("up to %.0f kn on %s, under your lowest no-go wind of %.0f kn", stay.MaxWindKn, stay.MaxWindDay, stay.NoGoKn)})
	}
	// A forecast is not a fault on board, so a change never raises a finding. It
	// is a line (email, Mate) and a row (the page draws only rows).
	wc := in.WindChanges
	switch {
	case wc.Problem != "":
		s.Lines = append(s.Lines, "Forecast wind changes were not read: "+wc.Problem)
		s.Rows = append(s.Rows, s.notChecked("Forecast wind changes", wc.Problem))
	case len(wc.Changes) > 0:
		text := watchWindChangeText(wc)
		s.Lines = append(s.Lines, "Forecast wind change: "+text+".")
		s.Rows = append(s.Rows, watchReportRow{Label: "Wind change", Value: text})
	}
	if wc.Problem == "" && wc.LimitsProblem != "" && len(wc.Changes) > 0 {
		s.Lines = append(s.Lines, "Wind changes not judged against your limits: "+wc.LimitsProblem)
		s.Rows = append(s.Rows, s.notChecked("Wind changes not judged against your limits", wc.LimitsProblem))
	}
	if wc.Problem == "" && wc.CheckedTo != "" {
		checked := wc.CheckedTo + ": " + wc.CheckedReason
		s.Lines = append(s.Lines, "Wind changes checked to "+checked+".")
		s.Rows = append(s.Rows, watchReportRow{Label: "Wind changes checked to", Value: checked})
	}
	return s, findings
}

// watchWindChangeText says the first change and how many follow, without a
// full stop: the page row's value and the body of the report line.
func watchWindChangeText(w watchWindChanges) string {
	c := w.Changes[0]
	speed := fmt.Sprintf("%d kn", c.SpeedMaxKn)
	if c.SpeedMinKn != c.SpeedMaxKn {
		speed = fmt.Sprintf("%d to %d kn", c.SpeedMinKn, c.SpeedMaxKn)
	}
	line := fmt.Sprintf("%s to %s %s around %s, %s behind it", c.From, c.To, c.DayLabel, c.Label, speed)
	if c.LowerConfidence {
		line += " (lower confidence)"
	}
	if c.Prominent {
		line += ", at or over your lowest marginal wind"
	}
	if more := len(w.Changes) - 1; more > 0 {
		line += fmt.Sprintf(", and %d more after", more)
	}
	return line
}

// ── instruments ─────────────────────────────────────────────────────────

func watchInstrumentsSection(w savedWatch, in watchReportInput) (watchReportSection, []watchFinding) {
	if in.Status == nil {
		return watchNoStatus(in.StatusProblem), nil
	}
	a := in.Status.Anomalies
	if a.Missing != "" {
		return watchReportSection{Unavailable: a.Missing}, nil
	}
	var s watchReportSection
	var parts []string
	add := func(count int, label string, names []string) {
		if count == 0 {
			return
		}
		line := fmt.Sprintf("%d %s", count, label)
		if len(names) > 0 {
			line += ": " + strings.Join(capList(names, 6), "; ")
		}
		parts = append(parts, line)
	}
	add(a.Frozen, "frozen", a.FrozenSensors)
	add(a.OutOfRange, "out of range", a.OutOfRangeSensors)
	add(a.SilentSources, "silent", a.SilentSourceNames)
	if len(parts) == 0 {
		s.Lines = append(s.Lines, "The sensor checks found no frozen, out of range or silent sensors")
		return s, nil
	}
	s.Lines = append(s.Lines, parts...)
	if a.Frozen+a.OutOfRange+a.SilentSources >= w.Thresholds.InstrumentIssues {
		return s, []watchFinding{ruleFinding(watchSectionInstruments, "instrument_issues", "Sensor checks: "+strings.Join(parts, ". ")+".")}
	}
	return s, nil
}

// watchBoatClock is a time as the Watches page shows it: on the boat's clock
// when its zone is known, else the UTC time said to be UTC. A nil loc, or UTC
// itself, means the zone is not known (a known zone is a fixed offset such as
// UTC+0, never plain UTC).
func watchBoatClock(t time.Time, loc *time.Location) string {
	if loc == nil || loc == time.UTC || loc.String() == "UTC" {
		return boatDayClock(t, time.UTC) + " UTC (boat time not known)"
	}
	return watchClock(t, loc)
}

// watchBoatZone is the boat clock's offset label, "" when it is not known.
func watchBoatZone(t time.Time, loc *time.Location) string {
	if loc == nil || loc == time.UTC || loc.String() == "UTC" {
		return ""
	}
	return zoneAbbreviation(t, loc)
}

// ── chart specs (ADR 0228) ──────────────────────────────────────────────
// What each rule's finding wants drawn. They say where to read the series and
// in which window; resolveWatchChart (watches_chart.go) reads them.

// watchPressureChartWindow is how far back a pressure chart looks.
const watchPressureChartWindow = 12 * time.Hour

func pressureChartSpec(sur vesselStatusSurroundings, fallHPa float64, now time.Time) *watchChartSpec {
	spec := &watchChartSpec{Title: "Pressure, last 12 hours", From: now.Add(-watchPressureChartWindow), To: now}
	p := sur.Pressure
	_, conv, _, known := operatorUnit("Pa")
	if p.Value == nil || p.Unit != "Pa" || p.DisplayUnit == "" || !known {
		spec.Problem = "the pressure is not given in the operator's unit"
		return spec
	}
	spec.Series = []watchChartSeriesSpec{{Label: "Pressure", Unit: p.DisplayUnit, Barometer: true, SIUnit: "Pa"}}
	if c := sur.PressureChange3h; c.Value != nil && c.Unit == "Pa" {
		spec.Threshold = &watchChartThreshold{Label: "3 h fall limit", Value: chartRound(conv(*p.Value - *c.Value - fallHPa*100))}
	}
	return spec
}

// The dashed lines on a tank's chart, in a watch report and on an alarm.
const (
	tankWarnLevelLabel  = "Warning level"
	tankAlarmLevelLabel = "Alarm level"
)

// tankPercentSeries is a tank's level as a percent of full, read the way the
// Tanks tile reads it.
func tankPercentSeries(name, path string) watchChartSeriesSpec {
	return watchChartSeriesSpec{Label: name, Unit: "%", Path: path, Convert: "percent"}
}

func tankChartSpec(tank vesselStatusTank, in watchReportInput) *watchChartSpec {
	from := in.PreviousAt
	if latest := in.Now.Add(-time.Hour); from.After(latest) {
		from = latest
	}
	spec := &watchChartSpec{Title: tank.Name + " level", From: from, To: in.Now}
	if tank.levelPath == "" {
		spec.Problem = "the tank's SignalK path is not known"
		return spec
	}
	spec.Series = []watchChartSeriesSpec{tankPercentSeries(tank.Name, tank.levelPath)}
	if tank.WarnPercent > 0 && tank.AlarmsNotRunning == "" && !tank.AlarmsOff {
		spec.Threshold = &watchChartThreshold{Label: tankWarnLevelLabel, Value: tank.WarnPercent}
	}
	return spec
}

func overnightChartSpec(o vesselObservation) *watchChartSpec {
	spec := &watchChartSpec{Title: "House bank overnight", From: o.nightStart, To: o.nightEnd}
	if o.bankPath == "" || o.nightStart.IsZero() || !o.nightEnd.After(o.nightStart) {
		spec.Problem = "the night's window or the house bank's path is not known"
		return spec
	}
	spec.Series = []watchChartSeriesSpec{
		{Label: "Charge", Unit: "%", Path: o.bankPath + ".capacity.stateOfCharge", Convert: "percent"},
		{Label: "Current", Unit: "A", Path: o.bankPath + ".current"},
	}
	return spec
}
