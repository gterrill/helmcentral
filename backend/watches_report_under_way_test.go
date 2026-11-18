package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The alarms_raised finding leaves out alarms raised under way and no longer
// active (ADR 0255). The section's lines and rows still list every alarm.

// underWayGroup is a title with n raised and left of them left out, the rest
// counted, the shape RaisedSummary gives.
func underWayGroup(title string, n, leftOut int, first, last, lastCounted time.Time) alarmRaisedGroup {
	g := alarmRaisedGroup{Title: title, Count: n, First: first, Last: last, LeftOut: leftOut}
	if leftOut > 0 {
		g.Counted = &alarmRaisedGroup{Title: title, Count: n - leftOut, First: first, Last: lastCounted}
	}
	return g
}

func TestWatchReport_FindingSaysHowManyUnderWayAlarmsItLeftOut(t *testing.T) {
	in := baseInput()
	at := in.Since.Add(40 * time.Minute)
	in.Raised = []alarmRaisedGroup{underWayGroup("House Bank Low", 6, 5, at, at.Add(50*time.Minute), at)}
	_, findings := buildWatchReport(baseWatch(), in)
	got := findingsFor(findings, "alarms")
	want := "1 alarm raised since 3.00 AM, not counting 5 raised under way. House Bank Low, once. No alarms active now."
	if len(got) != 1 || got[0].Text != want {
		t.Fatalf("finding %+v, want %q", got, want)
	}
}

func TestWatchReport_NoFindingWhenEveryAlarmWasRaisedUnderWay(t *testing.T) {
	in := baseInput()
	at := in.Since.Add(time.Hour)
	in.Raised = []alarmRaisedGroup{underWayGroup("Collision Risk", 245, 245, at, at, time.Time{})}
	report, findings := buildWatchReport(baseWatch(), in)
	if got := findingsFor(findings, "alarms"); len(got) != 0 {
		t.Fatalf("245 alarms raised under way make no finding: %+v", got)
	}
	// The section still lists them all.
	s := sectionOf(t, report, "alarms")
	if !strings.Contains(strings.Join(s.Lines, "|"), "245 alarms raised since 3.00 AM: Collision Risk, 245 times") || !strings.HasPrefix(s.Summary, "245 raised") {
		t.Errorf("lines %q summary %q", s.Lines, s.Summary)
	}
	if len(s.Rows) != 1 || s.Rows[0].Value != "245" {
		t.Errorf("rows %+v", s.Rows)
	}
}

func TestWatchReport_TopTitleAndSourcesComeFromTheCountedAlarms(t *testing.T) {
	in := baseInput()
	at := in.Since.Add(30 * time.Minute)
	later := at.Add(time.Hour)
	risk := underWayGroup("Collision Risk", 100, 98, at, later, later)
	risk.Sources, risk.SourceKind = 40, alarmSourceAISClosestApproach
	risk.Counted.Sources, risk.Counted.SourceKind = 2, alarmSourceAISClosestApproach
	risk.Counted.TopSources = []alarmSourceCount{{"Bravo", 1}, {"Charlie", 1}}
	bank := alarmRaisedGroup{Title: "House Bank Low", Count: 3, First: at, Last: at}
	in.Raised = []alarmRaisedGroup{risk, bank}
	_, findings := buildWatchReport(baseWatch(), in)
	got := findingsFor(findings, "alarms")
	want := "5 alarms raised since 3.00 AM, not counting 98 raised under way. House Bank Low 3 times. No alarms active now."
	if len(got) != 1 || got[0].Text != want {
		t.Fatalf("the most raised of the counted alarms is the top title: %+v", got)
	}

	// With the bank alarm gone, the collision alarms are the top title and
	// name two vessels, not forty.
	in.Raised = []alarmRaisedGroup{risk}
	_, findings = buildWatchReport(baseWatch(), in)
	got = findingsFor(findings, "alarms")
	want = "2 alarms raised since 3.00 AM, not counting 98 raised under way. Collision Risk 2 times, from 2 vessels. No alarms active now."
	if len(got) != 1 || got[0].Text != want {
		t.Fatalf("finding %+v, want %q", got, want)
	}
}

func TestWatchReport_CountedTiesGoToTheLatestCountedRaise(t *testing.T) {
	in := baseInput()
	at := in.Since.Add(time.Hour)
	// Equal counted numbers; the one with the later counted raise wins, even
	// though the other was raised later under way.
	a := underWayGroup("Alpha Alarm", 10, 8, at, at.Add(3*time.Hour), at.Add(10*time.Minute))
	b := underWayGroup("Bravo Alarm", 2, 0, at, at.Add(20*time.Minute), at.Add(20*time.Minute))
	in.Raised = []alarmRaisedGroup{a, b}
	_, findings := buildWatchReport(baseWatch(), in)
	got := findingsFor(findings, "alarms")
	if len(got) != 1 || !strings.Contains(got[0].Text, "Bravo Alarm 2 times") {
		t.Fatalf("%+v", got)
	}
}

func TestWatchReport_TheThresholdIsJudgedOnTheCountedAlarms(t *testing.T) {
	in := baseInput()
	at := in.Since.Add(time.Hour)
	in.Raised = []alarmRaisedGroup{underWayGroup("Collision Risk", 50, 48, at, at, at)}
	w := baseWatch()
	w.Thresholds.AlarmsRaised = 3
	if _, f := buildWatchReport(w, in); len(findingsFor(f, "alarms")) != 0 {
		t.Error("two counted alarms are under a threshold of three, however many were raised")
	}
	in.Raised = []alarmRaisedGroup{underWayGroup("Collision Risk", 50, 47, at, at, at)}
	if _, f := buildWatchReport(w, in); len(findingsFor(f, "alarms")) != 1 {
		t.Error("three counted alarms reach a threshold of three")
	}
}

func TestWatchReport_FiguresSayHowManyWereLeftOut(t *testing.T) {
	in := baseInput()
	at := in.Since.Add(time.Hour)
	in.Raised = []alarmRaisedGroup{underWayGroup("Collision Risk", 12, 9, at, at, at), {Title: "Tide Alarm", Count: 1, First: at, Last: at}}
	report, _ := buildWatchReport(baseWatch(), in)
	raw, _ := json.Marshal(sectionOf(t, report, "alarms").Figures)
	for _, want := range []string{`"raised":13`, `"under_way_left_out":9`, `"left_out":9`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("figures lack %s: %s", want, raw)
		}
	}
}

func TestWatchReport_WithoutTheActiveListNothingIsLeftOutAndTheSectionSaysSo(t *testing.T) {
	in := baseInput()
	at := in.Since.Add(time.Hour)
	in.Raised = []alarmRaisedGroup{{Title: "Collision Risk", Count: 4, UnderWay: 3, First: at, Last: at}}
	in.ActiveProblem = "the list of active alarms could not be read"
	report, findings := buildWatchReport(baseWatch(), in)
	s := sectionOf(t, report, "alarms")
	if len(s.NotChecked) != 1 || !strings.Contains(s.NotChecked[0], "the list of active alarms could not be read") || !strings.Contains(s.NotChecked[0], "under way") || strings.Contains(s.NotChecked[0], "wired") {
		t.Errorf("not checked %q", s.NotChecked)
	}
	if got := findingsFor(findings, "alarms"); len(got) != 1 || !strings.HasPrefix(got[0].Text, "4 alarms raised since 3.00 AM.") {
		t.Errorf("the alarms still count: %+v", got)
	}
}

// With no alarm raised under way the missing list changes nothing, so the
// report does not say it went unchecked.
func TestWatchReport_NoNotCheckedLineWhenNothingWasRaisedUnderWay(t *testing.T) {
	in := baseInput()
	at := in.Since.Add(time.Hour)
	in.Raised = []alarmRaisedGroup{{Title: "Collision Risk", Count: 4, First: at, Last: at}}
	in.ActiveProblem = "the list of active alarms could not be read"
	report, _ := buildWatchReport(baseWatch(), in)
	if s := sectionOf(t, report, "alarms"); len(s.NotChecked) != 0 {
		t.Errorf("not checked %q", s.NotChecked)
	}
}

func TestWatchReport_RowSaysHowManyUnderWayAlarmsAreNotCounted(t *testing.T) {
	in := baseInput()
	at := in.Since.Add(time.Hour)
	in.Raised = []alarmRaisedGroup{underWayGroup("House Bank Low", 6, 5, at, at, at)}
	report, _ := buildWatchReport(baseWatch(), in)
	s := sectionOf(t, report, "alarms")
	if len(s.Rows) != 1 || !strings.Contains(s.Rows[0].Detail, "5 under way not counted") {
		t.Errorf("rows %+v", s.Rows)
	}
}

// The row marker and the figure follow the group's rule-id answer, not the
// titles in the status list, which stops at ten.
func TestWatchReport_ActiveNowFollowsTheRuleIDAnswerNotTheStatusTitles(t *testing.T) {
	in := baseInput()
	at := in.Since.Add(time.Hour)
	g := alarmRaisedGroup{Title: "Collision Risk", Count: 2, First: at, Last: at, ActiveNow: true}
	quiet := alarmRaisedGroup{Title: "Tide Alarm", Count: 1, First: at, Last: at}
	in.Raised = []alarmRaisedGroup{g, quiet}
	report, _ := buildWatchReport(baseWatch(), in)
	s := sectionOf(t, report, "alarms")
	if len(s.Rows) != 2 || !strings.Contains(s.Rows[0].Detail, "active now") || strings.Contains(s.Rows[1].Detail, "active now") {
		t.Errorf("rows %+v", s.Rows)
	}
	raw, _ := json.Marshal(s.Figures)
	if !strings.Contains(string(raw), `"active_now":true`) {
		t.Errorf("figures %s", raw)
	}
}

func TestWatchReport_CountedTiesGoToTheTitle(t *testing.T) {
	in := baseInput()
	at := in.Since.Add(time.Hour)
	in.Raised = []alarmRaisedGroup{
		{Title: "Bravo Alarm", Count: 2, First: at, Last: at},
		{Title: "Alpha Alarm", Count: 2, First: at, Last: at},
	}
	_, findings := buildWatchReport(baseWatch(), in)
	got := findingsFor(findings, "alarms")
	if len(got) != 1 || !strings.Contains(got[0].Text, "Alpha Alarm 2 times") {
		t.Fatalf("%+v", got)
	}
}
