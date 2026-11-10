package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// Final review pass.

// --- 1. a pinned sender that has gone quiet ------------------------------------------

const oilSilent = "oil pressure: the sender this was learned from (can0.1) has gone quiet; relearning at the next training run"

func TestScore_APinnedSenderThatHasGoneQuietIsSaidAndAsksForTraining(t *testing.T) {
	// Another sender carries the reading now.
	feed := steadyScoreFeed(110)
	retag(feed, "propulsion.port.oilPressure", "other.sender", scoreRunStart.Add(-time.Hour))
	c := newScoreCase(trainedPort(t), feed)
	res := c.score(24 * time.Hour)
	got := notJudgedOfKind(res, "sender_silent")
	if len(got) != 1 || got[0].Reason != oilSilent || got[0].Minutes == 0 || res.JudgedMinutes != 0 {
		t.Fatalf("judged %d, not judged = %+v", res.JudgedMinutes, res.NotJudged)
	}
	if len(notJudgedOfKind(res, "missing_reading")) != 0 {
		t.Fatalf("a quiet sender is not also a missing reading: %+v", res.NotJudged)
	}
	if c.wakes == 0 {
		t.Fatal("a model that no longer matches the boat must ask for training")
	}
}

func TestScore_APinnedSenderWithNoDataAtAllIsSaidToo(t *testing.T) {
	feed := steadyScoreFeed(111)
	delete(feed, "propulsion.port.oilPressure")
	c := newScoreCase(trainedPort(t), feed)
	res := c.score(24 * time.Hour)
	got := notJudgedOfKind(res, "sender_silent")
	if len(got) != 1 || got[0].Reason != oilSilent || c.wakes == 0 {
		t.Fatalf("not judged = %+v, wakes %d", res.NotJudged, c.wakes)
	}
}

func TestScore_ARequiredReadingFromAnotherSenderIsSaidAsTheEnginesReading(t *testing.T) {
	feed := steadyScoreFeed(112)
	retag(feed, "propulsion.port.revolutions", "other.sender", scoreRunStart.Add(-time.Hour))
	c := newScoreCase(trainedPort(t), feed)
	res := c.score(24 * time.Hour)
	got := notJudgedOfKind(res, "sender_silent")
	if len(got) != 1 || got[0].Reason != "rpm: the sender this was learned from (can0.1) has gone quiet; relearning at the next training run" || c.wakes == 0 {
		t.Fatalf("not judged = %+v, wakes %d", res.NotJudged, c.wakes)
	}
}

// An engine that did not run in the window says nothing about its senders.
func TestScore_AnEngineThatDidNotRunSaysNothingOfItsSenders(t *testing.T) {
	c := newScoreCase(trainedPort(t), map[string][]normalMinuteRow{})
	res := c.score(24 * time.Hour)
	if len(notJudgedOfKind(res, "sender_silent")) != 0 || c.wakes != 0 {
		t.Fatalf("not judged = %+v, wakes %d", res.NotJudged, c.wakes)
	}
}

func TestBuildWatchUnusual_ASilentSenderIsShown(t *testing.T) {
	res := unusualResult{JudgedMinutes: 300, NotJudged: []unusualNotJudged{
		{Kind: "sender_silent", Subject: "port", SubjectName: "Port engine", Reason: oilSilent, Minutes: 12},
	}}
	u := buildWatchUnusual(res, "metric", time.UTC, vesselStatusTestNow)
	if u == nil || !strings.Contains(u.Note, "Port engine oil pressure: the sender this was learned from (can0.1) has gone quiet; relearning at the next training run.") {
		t.Fatalf("block = %+v", u)
	}
}

// --- 2. what an alarm takes out ---------------------------------------------------------

func TestTrain_AnAlarmOnAReadingTheModelDoesNotUseTakesNothingOut(t *testing.T) {
	cleared := daysAgo(5)
	h := newTrainHarness(synthFeed("port", 120, goodRuns()), "port")
	h.alarms = []alarmLogEntry{{ID: "state", Label: "Port Engine State", Path: "propulsion.port.state", RaisedAt: daysAgo(10), ClearedAt: &cleared}}
	m := h.train(t, normalSubsystemModel{})
	if len(m.Excluded) != 0 || m.Subjects["port"].GatedMinutes != 140 {
		t.Fatalf("excluded %+v, minutes %d", m.Excluded, m.Subjects["port"].GatedMinutes)
	}
	if !regimeNamed(t, m, "port/rpm1800").Judged {
		t.Fatal("the band is learned")
	}
}

func TestTrain_AnAlarmOnAReadingTheModelUsesStillTakesItOut(t *testing.T) {
	cleared := daysAgo(5)
	h := newTrainHarness(synthFeed("port", 121, goodRuns()), "port")
	h.alarms = []alarmLogEntry{{ID: "oil", Label: "Port Engine Oil Pressure Low", Path: "propulsion.port.oilPressure", RaisedAt: daysAgo(10), ClearedAt: &cleared}}
	m := h.train(t, normalSubsystemModel{})
	if len(m.Excluded) != 1 || m.Subjects["port"].GatedMinutes != 0 {
		t.Fatalf("excluded %+v, minutes %d", m.Excluded, m.Subjects["port"].GatedMinutes)
	}
}

// --- 3. a short gap is the same run ----------------------------------------------------------

func TestEngineRuns_AGapUnderFiveMinutesIsTheSameRun(t *testing.T) {
	minutes := steadyEngine(30, 1900)
	tail := steadyEngine(30, 1900)
	for i := range tail {
		tail[i].At = engineGateBase.Add(time.Duration(33+i) * time.Minute) // minutes 30 to 32 missing
	}
	minutes = append(minutes, tail...)
	sub := normalEnginesSubsystem()
	runs := sub.Run(minutes)
	if runs[0] != runs[len(runs)-1] {
		t.Fatalf("a 3 minute gap with the engine above idle either side is one run: %v", runs)
	}
	running := normalEngineRunning(minutes)
	if running[30] < 30*time.Minute {
		t.Fatalf("the running clock was reset by the gap: %v", running[30])
	}
}

func TestEngineRuns_AGapOfFiveMinutesOrMoreIsANewRun(t *testing.T) {
	minutes := steadyEngine(30, 1900)
	tail := steadyEngine(30, 1900)
	for i := range tail {
		tail[i].At = engineGateBase.Add(time.Duration(40+i) * time.Minute)
	}
	minutes = append(minutes, tail...)
	runs := normalEnginesSubsystem().Run(minutes)
	if runs[0] == runs[len(runs)-1] {
		t.Fatalf("ten minutes of no data is a new run: %v", runs)
	}
}

// The twin detector keeps its own tolerance of one minute.
func TestRunningDurationsAt_StillResetsOnAnyGap(t *testing.T) {
	at := []time.Time{engineGateBase, engineGateBase.Add(time.Minute), engineGateBase.Add(3 * time.Minute)}
	rpm := map[time.Time]float64{at[0]: 30, at[1]: 30, at[2]: 30}
	if got := runningDurationsAt(at, rpm, twinIdleRPMHz); got[2] != 0 {
		t.Fatalf("durations = %v", got)
	}
}

// --- 4. the report's budget ----------------------------------------------------------------------

func gatherWithUnusual(t *testing.T, ctx context.Context, fn func(context.Context, time.Time, time.Time, string) unusualResult) watchReportInput {
	t.Helper()
	env := newProposalEnv(t)
	d := statusTestDeps(t)
	d.alarmLog = func() *alarmLogStore { return nil }
	d.unusualReadings = fn
	w := savedWatch{ID: "w1", Name: "Morning", Kind: watchKindScheduled, Sections: []string{"instruments"}, Thresholds: defaultWatchThresholds()}
	in, err := gatherWatchReportInput(ctx, d, env.docs, w, vesselStatusTestNow, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	return in
}

// The budget comes from the run's own context, so a shutdown ends it too.
func TestGatherWatchReportInput_TheBudgetIsDerivedFromTheRunsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var seen error
	gatherWithUnusual(t, ctx, func(c context.Context, _, _ time.Time, _ string) unusualResult {
		seen = c.Err()
		return unusualResult{}
	})
	if !errors.Is(seen, context.Canceled) {
		t.Fatalf("the scoring context was not derived from the run's: %v", seen)
	}
}

// A result that came back whole is kept, even when the deadline passed as it did.
func TestGatherWatchReportInput_AResultThatCameBackIsKeptEvenIfTheDeadlinePassed(t *testing.T) {
	old := watchUnusualBudget
	watchUnusualBudget = 20 * time.Millisecond
	defer func() { watchUnusualBudget = old }()
	in := gatherWithUnusual(t, context.Background(), func(c context.Context, _, _ time.Time, _ string) unusualResult {
		time.Sleep(60 * time.Millisecond)
		return unusualResult{JudgedMinutes: 77}
	})
	if in.Unusual == nil || in.Unusual.ProblemKind != "" || in.Unusual.JudgedMinutes != 77 {
		t.Fatalf("Unusual = %+v", in.Unusual)
	}
}

// --- 5. floats only ---------------------------------------------------------------------------------

func TestBuildNormalMinutesFlux_KeepsFloatsOnly(t *testing.T) {
	flux, err := buildNormalMinutesFlux("b", "value", []string{"p"}, minutesStart, minutesStart.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	filter := `types.isType(v: r._value, type: "float")`
	if !strings.HasPrefix(flux, `import "types"`) || !strings.Contains(flux, filter) || strings.Index(flux, filter) > strings.Index(flux, "aggregateWindow") {
		t.Fatalf("flux = %s", flux)
	}
}

// --- 6. wording from the shared helpers --------------------------------------------------------------

func TestNotJudgedWording_ManyReadings(t *testing.T) {
	feed := steadyScoreFeed(113)
	c := newScoreCase(trainedPort(t), feed)
	c.ignored = map[string]bool{"propulsion.port.oilPressure": true, "propulsion.port.boostPressure": true, "propulsion.port.engineLoad": true}
	off := notJudgedOfKind(c.score(24*time.Hour), "switched_off")
	want := "oil pressure, boost pressure and engine load are switched off; relearning at the next training run"
	if len(off) == 0 || off[0].Reason != want {
		t.Fatalf("not judged = %+v", off)
	}
}

// --- 7 and 8. one run, one dedicated connection ----------------------------------------------------------

func TestQueryNormalMinutes_OpensFromTheSettingsPathItIsGiven(t *testing.T) {
	var got string
	old := openNormalInflux
	defer func() { openNormalInflux = old }()
	openNormalInflux = func(path string) (normalInflux, error) { got = path; return normalInflux{}, errors.New("stop") }
	_, _ = queryNormalMinutes(context.Background(), "/boat/settings.yaml", normalSource{Kind: normalSourceMainBucket}, []string{"p"}, trainNow.Add(-time.Hour), trainNow)
	if got != "/boat/settings.yaml" {
		t.Fatalf("opened from %q", got)
	}
}

func TestQueryNormalMinutes_ClosesTheRunsConnectionWhenItEnds(t *testing.T) {
	closed := 0
	oldOpen, oldRun := openNormalInflux, runNormalMinutesQuery
	defer func() { openNormalInflux, runNormalMinutesQuery = oldOpen, oldRun }()
	openNormalInflux = func(string) (normalInflux, error) { return normalInflux{close: func() { closed++ }}, nil }
	fail := false
	runNormalMinutesQuery = func(context.Context, normalInflux, normalSource, []string, time.Time, time.Time) ([]normalMinuteRow, error) {
		if fail {
			return nil, errors.New("boom")
		}
		return nil, nil
	}
	src := normalSource{Kind: normalSourceMainBucket}
	if _, err := queryNormalMinutes(context.Background(), "s", src, []string{"p"}, trainNow.Add(-3*24*time.Hour), trainNow); err != nil || closed != 1 {
		t.Fatalf("err %v, closed %d", err, closed)
	}
	fail = true
	if _, err := queryNormalMinutes(context.Background(), "s", src, []string{"p"}, trainNow.Add(-3*24*time.Hour), trainNow); err == nil || closed != 2 {
		t.Fatalf("err %v, closed %d", err, closed)
	}
}

// A run reads the source once, so the settings cannot change between the two.
func TestTrainNormal_ResolvesTheSourceOncePerRun(t *testing.T) {
	h := newTrainHarness(synthFeed("port", 130, goodRuns()), "port")
	deps := h.deps()
	reads := 0
	inner := deps.Source
	deps.Source = func() (normalSource, error) { reads++; return inner() }
	if err := trainNormal(context.Background(), t.TempDir()+"/n.json", deps); err != nil {
		t.Fatal(err)
	}
	if reads != 1 {
		t.Fatalf("the source was read %d times", reads)
	}
}
