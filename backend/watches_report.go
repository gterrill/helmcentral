package main

import (
	"encoding/json"
	"fmt"
	"math"
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
}

type watchReport struct {
	Kind        string               `json:"kind"`
	GeneratedAt string               `json:"generated_at"`
	BoatTime    string               `json:"boat_time"`
	Since       string               `json:"since"`
	SinceNote   string               `json:"since_note,omitempty"`
	Sections    []watchReportSection `json:"sections"`
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
	Raised        []alarmLogEntry
	RaisedProblem string
	// Previous is the last run's report and PreviousAt its time.
	Previous   *watchReport
	PreviousAt time.Time
	Stay       watchStayForecast
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
	return report, findings
}

// watchClock is a time on the boat's clock, with its zone.
func watchClock(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("Mon 2 Jan 15:04 MST")
}

func watchHM(t time.Time, loc *time.Location) string { return t.In(loc).Format("15:04") }

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

// watchLabelled is "Label: 13.1 V", or "Label: not available (reason)".
func watchLabelled(label string, v statusValue) string {
	if text, ok := watchValueText(v); ok {
		return label + ": " + text
	}
	reason := v.Missing
	if reason == "" {
		reason = "no reading"
	}
	return label + ": not available (" + reason + ")"
}

func watchNoStatus(problem string) watchReportSection {
	if problem == "" {
		problem = "the boat's readings could not be read"
	}
	return watchReportSection{Unavailable: problem}
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
	if len(parts) > 0 {
		s.Lines = append(s.Lines, "House bank: "+strings.Join(parts, ", "))
	} else {
		reason := p.HouseBank.SocPercent.Missing
		s.Lines = append(s.Lines, "House bank: not available ("+reason+")")
	}
	s.Lines = append(s.Lines, watchLabelled("Solar", p.SolarW), watchLabelled("DC load", p.DCLoadW))
	for _, why := range in.Status.ObservationsUnchecked {
		s.Lines = append(s.Lines, "Not checked, "+why)
	}
	var findings []watchFinding
	for _, o := range in.Status.Observations {
		if o.Kind == "overnight_house_draw" {
			findings = append(findings, ruleFinding(watchSectionPower, "overnight_draw", o.Summary))
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
		}}, nil
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
	if in.Status == nil {
		s.Lines = append(s.Lines, "Active alarms: not available ("+in.StatusProblem+")")
	} else if n := len(in.Status.Alarms.Active); n == 0 {
		s.Lines = append(s.Lines, "No alarms active now")
	} else {
		titles := make([]string, n)
		for i, a := range in.Status.Alarms.Active {
			titles[i] = a.Title
		}
		s.Lines = append(s.Lines, fmt.Sprintf("%d active now: %s", in.Status.Alarms.ActiveCount, strings.Join(titles, ", ")))
	}
	var raised []string
	for _, e := range in.Raised {
		if e.RaisedAt.Before(in.Since) || e.RaisedAt.After(in.Now) {
			continue
		}
		raised = append(raised, fmt.Sprintf("%s (%s)", e.Label, watchHM(e.RaisedAt, loc)))
	}
	if len(raised) == 0 {
		s.Lines = append(s.Lines, "None raised since "+watchHM(in.Since, loc))
		return s, nil
	}
	text := fmt.Sprintf("%d %s raised since %s: %s", len(raised), watchPlural(len(raised), "alarm", "alarms"), watchHM(in.Since, loc), strings.Join(capList(raised, 8), ", "))
	s.Lines = append(s.Lines, text)
	if len(raised) >= w.Thresholds.AlarmsRaised {
		return s, []watchFinding{ruleFinding(watchSectionAlarms, "alarms_raised", text)}
	}
	return s, nil
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
	for _, tank := range in.Status.Tanks.Tanks {
		s.Lines = append(s.Lines, watchLabelled(tank.Name, tank.Level))
		if tank.Level.Value == nil {
			continue
		}
		now := *tank.Level.Value
		levels[tank.Name] = now
		was, ok := prev[tank.Name]
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
		findings = append(findings, ruleFinding(watchSectionTanks, "tank_change", fmt.Sprintf(
			"%s is %s %.0f points since %s (%.0f %% to %.0f %%).", tank.Name, dir, math.Abs(delta), watchHM(in.PreviousAt, loc), was, now)))
	}
	if !havePrev {
		s.Lines = append(s.Lines, "No earlier report to compare with, so changes are measured from the next one.")
	}
	s.Figures = map[string]any{"levels": levels}
	return s, findings
}

// ── weather ─────────────────────────────────────────────────────────────

func watchWeatherSection(w savedWatch, in watchReportInput) (watchReportSection, []watchFinding) {
	if in.Status == nil {
		return watchNoStatus(in.StatusProblem), nil
	}
	sur := in.Status.Surroundings
	var s watchReportSection
	var findings []watchFinding
	s.Lines = append(s.Lines, watchLabelled("Pressure", sur.Pressure), watchLabelled("Pressure change in 3 hours", sur.PressureChange3h))
	if text, ok := watchValueText(sur.TrueWindSpeed); ok {
		wind := "True wind: " + text
		if gust, ok := watchValueText(sur.TrueWindGust); ok {
			wind += ", gust " + gust
		}
		s.Lines = append(s.Lines, wind)
	} else {
		s.Lines = append(s.Lines, watchLabelled("True wind", sur.TrueWindSpeed))
	}
	if sur.PressureChange3h.Value != nil && *sur.PressureChange3h.Value <= -w.Thresholds.PressureFallHPa*100 {
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
		findings = append(findings, ruleFinding(watchSectionWeather, "pressure_fall", text+"."))
	}
	stay := in.Stay
	switch {
	case stay.Name == "":
	case stay.Problem != "":
		s.Lines = append(s.Lines, "Forecast for "+stay.Name+" was not read: "+stay.Problem)
	case stay.NoGoKn <= 0:
		s.Lines = append(s.Lines, fmt.Sprintf("Forecast for %s: wind up to %.0f kn on %s. There are no passage limits set, so it is not judged.", stay.Name, stay.MaxWindKn, stay.MaxWindDay))
	case stay.MaxWindKn >= stay.NoGoKn:
		text := fmt.Sprintf("Forecast wind at %s reaches %.0f kn on %s, over your lowest no-go wind of %.0f kn.", stay.Name, stay.MaxWindKn, stay.MaxWindDay, stay.NoGoKn)
		s.Lines = append(s.Lines, text)
		findings = append(findings, ruleFinding(watchSectionWeather, "forecast_no_go", text))
	default:
		s.Lines = append(s.Lines, fmt.Sprintf("Forecast for %s: wind up to %.0f kn on %s, under your lowest no-go wind of %.0f kn.", stay.Name, stay.MaxWindKn, stay.MaxWindDay, stay.NoGoKn))
	}
	return s, findings
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
		return t.UTC().Format("Mon 2 Jan 15:04") + " UTC (boat time not known)"
	}
	return watchClock(t, loc)
}

// watchBoatZone is the boat clock's offset label, "" when it is not known.
func watchBoatZone(t time.Time, loc *time.Location) string {
	if loc == nil || loc == time.UTC || loc.String() == "UTC" {
		return ""
	}
	name, _ := t.In(loc).Zone()
	return name
}
