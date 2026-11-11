package main

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Scoring the house bank and the engines together (ADR 0276).

// houseModel trains the house bank from houseSpells.
func houseModel(t *testing.T) normalSubsystemModel {
	t.Helper()
	m := houseHarness(houseFeed(31, houseSpells())).trainHouse(t, normalSubsystemModel{})
	if !m.Regimes["house/none/soc60"].Judged {
		t.Fatalf("setup: %+v", m.Regimes)
	}
	return m
}

// houseScoreCase scores the house bank alone against a model that holds it.
func houseScoreCase(t *testing.T, feed map[string][]normalMinuteRow) *scoreCase {
	t.Helper()
	v := houseVessel()
	c := newScoreCase(normalModelFile{Subsystems: map[string]normalSubsystemModel{normalSubsystemElectrical: houseModel(t)}}, feed)
	c.vessel = &v
	return c
}

func (c *scoreCase) scoreHouse(since time.Duration) unusualResult {
	return scoreOnly(context.Background(), normalElectricalSubsystem(), c.deps(), trainNow.Add(-since), trainNow, "", c.answers)
}

// A bank that ran steady for an hour and then sagged for an hour.
func sagFeed(seed int64) map[string][]normalMinuteRow {
	return houseFeed(seed, []houseSpell{
		{Start: scoreRunStart, Minutes: 60, SOC: 0.7},
		{Start: scoreRunStart.Add(60 * time.Minute), Minutes: 60, SOC: 0.7, Sag: 0.5},
	})
}

func TestScoreHouse_AVoltageSagWithNothingChargingIsAStretchNamingTheBanksVoltage(t *testing.T) {
	res := houseScoreCase(t, sagFeed(41)).scoreHouse(24 * time.Hour)
	if res.Problem != "" {
		t.Fatalf("problem = %q", res.Problem)
	}
	if len(res.Stretches) != 1 {
		t.Fatalf("stretches = %+v, not judged = %+v", res.Stretches, res.NotJudged)
	}
	s := res.Stretches[0]
	if s.Subsystem != "electrical" || s.Subject != "house" || s.SubjectName != "House bank" || s.Band != "with nothing charging, 60 to 80 % charge" {
		t.Fatalf("stretch = %+v", s)
	}
	if s.Top.Key != "voltage" || s.Top.Name != "House bank voltage" || s.Top.Direction != "low" || s.Top.SIUnit != "V" {
		t.Fatalf("top = %+v", s.Top)
	}
	if s.Top.Usual < 26.0 || s.Top.Usual > 26.3 || s.Top.StretchMean > s.Top.Usual-0.4 {
		t.Fatalf("usual %v, stretch mean %v: the sag was half a volt", s.Top.Usual, s.Top.StretchMean)
	}
	if s.ID != "electrical:house@"+s.Start.Format(time.RFC3339) {
		t.Fatalf("id = %q", s.ID)
	}
	v, err := describeUnusualStretch(s, "metric", time.UTC, trainNow)
	if err != nil {
		t.Fatal(err)
	}
	usual := strconv.FormatFloat(roundTo(s.Top.Usual, 2), 'f', -1, 64)
	if !strings.HasPrefix(v.Summary, "House bank voltage ran low from ") || !strings.HasSuffix(v.Summary, " V against a usual "+usual+" V with nothing charging, 60 to 80 % charge.") {
		t.Fatalf("summary = %q", v.Summary)
	}
}

// A charging source that is off reads nothing. Nothing is not a sender that has
// gone quiet, and does not ask for a new training run.
func TestScoreHouse_AnAlternatorThatIsOffIsNeverASilentSender(t *testing.T) {
	spells := houseSpells()
	for i := 0; i < 2; i++ {
		s := gatedHouse(daysAgo(13-float64(i)), 70, 0.7)
		s.Alt = true
		spells = append(spells, s)
	}
	model := houseHarness(houseFeed(32, spells)).trainHouse(t, normalSubsystemModel{})
	if !slices.Contains(model.Regimes["house/engines/soc60"].Features, "alternator") {
		t.Fatalf("setup: %+v", model.Regimes["house/engines/soc60"])
	}
	v := houseVessel()
	c := newScoreCase(normalModelFile{Subsystems: map[string]normalSubsystemModel{normalSubsystemElectrical: model}}, sagFeed(42))
	c.vessel = &v
	res := c.scoreHouse(24 * time.Hour)
	for _, n := range res.NotJudged {
		if n.Kind == "sender_silent" {
			t.Fatalf("an alternator that is off is not silent: %+v", n)
		}
	}
	if c.wakes != 0 || len(res.Stretches) != 1 {
		t.Fatalf("wakes %d, stretches %+v", c.wakes, res.Stretches)
	}
}

func bothVessel() *vesselView {
	v := houseVessel()
	v.Engines = []vesselEngineSetting{{Instance: "port", Name: "Port"}}
	return &v
}

// With an engine turning the bank is judged in the engines bands, so the bank is
// trained there too, and its feed leaves the revolutions to the engine's own.
func bothModel(t *testing.T) normalModelFile {
	t.Helper()
	m := trainedPort(t)
	m.Subsystems[normalSubsystemElectrical] = houseHarness(houseFeed(31, altSpells(13))).trainHouse(t, normalSubsystemModel{})
	return m
}

func sagEnginesFeed(seed int64) map[string][]normalMinuteRow {
	return houseFeed(seed, []houseSpell{
		{Start: scoreRunStart, Minutes: 60, SOC: 0.7, Alt: true, NoRPM: true},
		{Start: scoreRunStart.Add(60 * time.Minute), Minutes: 60, SOC: 0.7, Alt: true, NoRPM: true, Sag: 0.5},
	})
}

func (c *scoreCase) scoreAll(subject unusualSubject) unusualResult {
	return scoreUnusualAll(context.Background(), c.deps(), trainNow.Add(-24*time.Hour), trainNow, subject, c.answers)
}

func TestScoreAll_ScoresBothSubsystemsAndRanksTheirStretchesTogether(t *testing.T) {
	feed := mergeFeeds(steadyScoreFeed(44), sagEnginesFeed(44))
	bend(feed, "propulsion.port.oilPressure", 40, 70, halve)
	c := newScoreCase(bothModel(t), feed)
	c.vessel = bothVessel()
	res := c.scoreAll(unusualSubject{})
	if res.ProblemKind != "" {
		t.Fatalf("problem = %q %q", res.ProblemKind, res.Problem)
	}
	if len(res.Stretches) != 2 {
		t.Fatalf("stretches = %+v", res.Stretches)
	}
	subsystems := []string{res.Stretches[0].Subsystem, res.Stretches[1].Subsystem}
	slices.Sort(subsystems)
	if !slices.Equal(subsystems, []string{"electrical", "engines"}) {
		t.Fatalf("subsystems = %v", subsystems)
	}
	if res.Stretches[0].PeakRatio < res.Stretches[1].PeakRatio {
		t.Fatalf("ranked together, worst first: %v then %v", res.Stretches[0].PeakRatio, res.Stretches[1].PeakRatio)
	}
	if res.JudgedMinutes != 110+111 {
		t.Fatalf("judged %d minutes: the engine's 110 and the bank's 111", res.JudgedMinutes)
	}
	if res.Model == nil || res.Model.Bands["Port engine"] != 2 || res.Model.Bands["House bank"] != 3 {
		t.Fatalf("model = %+v", res.Model)
	}
}

// A subsystem with no model yet is one note. The other is still judged.
func TestScoreAll_AHouseBankWithNoModelIsANoteAndDoesNotHideTheEngines(t *testing.T) {
	feed := mergeFeeds(steadyScoreFeed(45), sagEnginesFeed(45))
	bend(feed, "propulsion.port.oilPressure", 40, 70, halve)
	c := newScoreCase(trainedPort(t), feed)
	c.vessel = bothVessel()
	res := c.scoreAll(unusualSubject{})
	if res.ProblemKind != "" || len(res.Stretches) != 1 || res.Stretches[0].Subsystem != "engines" {
		t.Fatalf("problem %q, stretches %+v", res.ProblemKind, res.Stretches)
	}
	var note *unusualNotJudged
	for i := range res.NotJudged {
		if res.NotJudged[i].Kind == "subject_problem" {
			note = &res.NotJudged[i]
		}
	}
	if note == nil || note.SubjectName != "House bank" || note.Reason != "no learned normal yet" {
		t.Fatalf("not judged = %+v", res.NotJudged)
	}
}

// A boat with no house bank is judged on its engines alone, with nothing said
// about a bank.
func TestScoreAll_ABoatWithNoHouseBankIsScoredOnItsEnginesAlone(t *testing.T) {
	c := newScoreCase(trainedPort(t), steadyScoreFeed(46))
	res := c.scoreAll(unusualSubject{})
	if res.ProblemKind != "" || res.JudgedMinutes != 110 {
		t.Fatalf("problem %q judged %d", res.ProblemKind, res.JudgedMinutes)
	}
	for _, n := range res.NotJudged {
		if strings.Contains(n.SubjectName, "House") {
			t.Fatalf("a boat without a bank is told nothing about one: %+v", n)
		}
	}
}

func TestScoreAll_NamingASubjectScoresOnlyItsSubsystem(t *testing.T) {
	feed := mergeFeeds(steadyScoreFeed(47), sagEnginesFeed(47))
	bend(feed, "propulsion.port.oilPressure", 40, 70, halve)
	c := newScoreCase(bothModel(t), feed)
	c.vessel = bothVessel()
	res := c.scoreAll(unusualSubject{Subsystem: "electrical", ID: "house"})
	if len(res.Stretches) != 1 || res.Stretches[0].Subsystem != "electrical" || res.JudgedMinutes != 111 {
		t.Fatalf("stretches %+v judged %d", res.Stretches, res.JudgedMinutes)
	}
	res = c.scoreAll(unusualSubject{Subsystem: "engines", ID: "port"})
	if len(res.Stretches) != 1 || res.Stretches[0].Subsystem != "engines" || res.JudgedMinutes != 110 {
		t.Fatalf("stretches %+v judged %d", res.Stretches, res.JudgedMinutes)
	}
	if res = c.scoreAll(unusualSubject{Subsystem: "hull", ID: "x"}); res.ProblemKind != unusualError {
		t.Fatalf("an unknown subsystem is an error: %+v", res)
	}
}

// When every subsystem fails, the run fails with the first one's reason.
func TestScoreAll_WhenNothingCanBeScoredTheProblemIsStatedOnce(t *testing.T) {
	c := newScoreCase(normalModelFile{Subsystems: map[string]normalSubsystemModel{}}, nil)
	c.vessel = bothVessel()
	res := c.scoreAll(unusualSubject{})
	if res.ProblemKind != unusualNeverTrained || res.Problem != normalNoModelYet {
		t.Fatalf("result = %+v", res)
	}
}

func houseStretch(hour, minutes int) unusualStretch {
	start := time.Date(2026, 11, 10, hour, 10, 0, 0, time.UTC)
	return unusualStretch{
		ID: "electrical:house@" + start.Format(time.RFC3339), Subsystem: "electrical", Subject: "house", SubjectName: "House bank",
		Start: start, End: start.Add(time.Duration(minutes) * time.Minute), Minutes: minutes, Band: "with nothing charging, 60 to 80 % charge",
		Top: unusualReading{Key: "voltage", Path: "electrical.batteries.1.voltage", Name: "House bank voltage", SIUnit: "V", Direction: "low", StretchMean: 25.6, Usual: 26.1},
	}
}

func TestDescribeStretch_TheHouseBankIsWordedFromItsOwnFeatureNameAndBand(t *testing.T) {
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	v := mustDescribe(t, houseStretch(2, 30), "metric", time.UTC, now)
	want := "House bank voltage ran low from 2.10 AM to 2.40 AM: 25.6 V against a usual 26.1 V with nothing charging, 60 to 80 % charge."
	if v.Summary != want || v.Subject != "House bank" || v.Reading != "House bank voltage" || v.Unit != "V" {
		t.Fatalf("view = %+v", v)
	}
	bare := houseStretch(2, 30)
	bare.Top = unusualReading{}
	v = mustDescribe(t, bare, "metric", time.UTC, now)
	if want := "House bank's readings were outside their usual pattern from 2.10 AM to 2.40 AM with nothing charging, 60 to 80 % charge."; v.Summary != want {
		t.Fatalf("summary = %q", v.Summary)
	}
}

func TestDescribeStretch_ASubsystemThisBuildDoesNotLearnIsAnError(t *testing.T) {
	st := houseStretch(2, 30)
	st.Subsystem = "hull"
	if _, err := describeUnusualStretch(st, "metric", time.UTC, trainNow); err == nil || !strings.Contains(err.Error(), "hull") {
		t.Fatalf("err = %v", err)
	}
}

// Both subsystems share the report's two lines, worst first, and the engines
// are worded as they always were.
func TestBuildWatchUnusual_TheBankAndTheEnginesShareTwoLines(t *testing.T) {
	eng := stretchAt(3, 20)
	eng.Subsystem, eng.ID = normalSubsystemEngines, stretchIDFor(normalSubsystemEngines, "port", eng.Start)
	res := unusualResultWith(houseStretch(2, 30), eng, stretchAt(4, 10))
	res.Stretches[2].Subsystem = normalSubsystemEngines
	u := buildWatchUnusual(res, "metric", time.UTC, unusualNow)
	if u == nil || len(u.Lines) != 2 || len(u.Stretches) != 2 {
		t.Fatalf("block = %+v", u)
	}
	if u.Lines[0] != "House bank voltage ran low from 2.10 AM to 2.40 AM: 25.6 V against a usual 26.1 V with nothing charging, 60 to 80 % charge." {
		t.Fatalf("line = %q", u.Lines[0])
	}
	if s := u.Stretches[0]; s.Subsystem != "electrical" || s.Subject != "house" || s.SubjectName != "House bank" {
		t.Fatalf("stretch = %+v", s)
	}
	if u.Note != "1 more unusual stretch is not shown." {
		t.Fatalf("note = %q", u.Note)
	}
}

func TestBuildWatchUnusual_ABankWithNoModelIsANoteBesideTheEnginesLines(t *testing.T) {
	st := stretchAt(2, 30)
	st.Subsystem = normalSubsystemEngines
	res := unusualResultWith(st)
	res.NotJudged = []unusualNotJudged{{Kind: "subject_problem", SubjectName: "House bank", Reason: "no learned normal yet"}}
	u := buildWatchUnusual(res, "metric", time.UTC, unusualNow)
	if u == nil || len(u.Lines) != 1 || u.Note != "House bank: no learned normal yet." {
		t.Fatalf("block = %+v", u)
	}
}

// A stretch of the house bank is answered like an engine's, and the answer is
// kept under the bank.
func TestRecordNormalAnswer_AnswersAHouseBankStretch(t *testing.T) {
	deps, woke := answerDeps(t)
	st := watchUnusualStretch{
		StretchID: stretchIDFor(normalSubsystemElectrical, "house", answerFrom), Subsystem: normalSubsystemElectrical, Subject: "house", SubjectName: "House bank",
		From: answerFrom, To: answerFrom.Add(20 * time.Minute),
	}
	deps.Run = func(string, string) (watchRun, error) {
		return unusualRunWith(&watchUnusual{Lines: []string{"line"}, Stretches: []watchUnusualStretch{st}}), nil
	}
	deps.Vessel = func() (vesselView, error) { return houseVessel(), nil }
	saved, err := recordNormalAnswer(deps, answerRequest())
	if err != nil {
		t.Fatal(err)
	}
	if saved.Subsystem != "electrical" || saved.Subject != "house" || saved.Answer != normalAnswerReal || *woke != 1 {
		t.Fatalf("saved = %+v, woke %d", saved, *woke)
	}
}

func TestRecordNormalAnswer_AHouseBankThatIsNoLongerChosenSaysSo(t *testing.T) {
	deps, _ := answerDeps(t)
	st := watchUnusualStretch{StretchID: "electrical:house@x", Subsystem: normalSubsystemElectrical, Subject: "house", From: answerFrom, To: answerFrom.Add(20 * time.Minute)}
	deps.Run = func(string, string) (watchRun, error) {
		return unusualRunWith(&watchUnusual{Lines: []string{"line"}, Stretches: []watchUnusualStretch{st}}), nil
	}
	_, err := recordNormalAnswer(deps, answerRequest())
	var bad *normalAnswerError
	if !errors.As(err, &bad) || bad.Error() != "The house bank is no longer set up. It may have been changed or removed." {
		t.Fatalf("err = %v", err)
	}
}

// The stored block names the subject in subject_name.
func TestUnusualStretchesOf_SavesTheSubjectName(t *testing.T) {
	u := buildWatchUnusual(unusualResultWith(houseStretch(2, 30)), "metric", time.UTC, unusualNow)
	raw, _ := json.Marshal(u)
	if !strings.Contains(string(raw), `"subject_name":"House bank"`) || strings.Contains(string(raw), `"engine"`) {
		t.Fatalf("stored block = %s", raw)
	}
}
