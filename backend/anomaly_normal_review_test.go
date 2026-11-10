package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 1. A reading the gate needs, on the ignored list, stops the engine with a
// problem that names it. It does not gate on a sender the operator ignored
// because it may be dead.
func TestTrain_AnIgnoredGateReadingStopsTheEngineAndSaysWhichOne(t *testing.T) {
	h := newTrainHarness(synthFeed("port", 80, goodRuns()), "port")
	h.ignored = map[string]bool{"propulsion.port.revolutions": true}
	m := h.train(t, normalSubsystemModel{})
	if got := m.Subjects["port"].Problem; got != "rpm is switched off, so this engine cannot be judged" {
		t.Fatalf("problem = %q", got)
	}
	if len(m.Regimes) != 0 {
		t.Fatalf("regimes = %+v", m.Regimes)
	}
}

// 2. A band with enough minutes and runs whose fit failed says it could not be
// fitted. It never says it needs more runs.
func TestScore_ABandThatCouldNotBeFittedIsNotSaidToNeedMoreHistory(t *testing.T) {
	model := trainedPort(t)
	eng := model.Subsystems[normalSubsystemEngines]
	eng.Regimes["port/rpm1800"] = normalRegime{Subject: "port", Label: "1,800 to 2,000 rpm", Minutes: 200, Runs: 3, Judged: false,
		Reason: "could not be fitted: reading 2 did not change, so it has no spread to judge by"}
	model.Subsystems[normalSubsystemEngines] = eng
	res := newScoreCase(model, steadyScoreFeed(81)).score(24 * time.Hour)
	for _, n := range res.NotJudged {
		if n.Kind != "thin_band" {
			continue
		}
		if !strings.Contains(n.Short, "could not be fitted") || strings.Contains(n.Short, "needs") || strings.Contains(n.Reason, "needs") {
			t.Fatalf("not judged = %+v", n)
		}
		return
	}
	t.Fatalf("no band reported: %+v", res.NotJudged)
}

// 3. A shutdown is not a failed attempt: the record is left as it was.
func TestTrainNormal_AStoppedRunLeavesTheLastAttemptAsItWas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anomaly-normal.json")
	h := newTrainHarness(synthFeed("port", 82, goodRuns()), "port")
	if err := trainNormal(context.Background(), path, h.deps()); err != nil {
		t.Fatal(err)
	}
	before, _ := loadNormalModel(path)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.now = trainNow.Add(6 * time.Hour)
	if err := trainNormal(ctx, path, h.deps()); err == nil {
		t.Fatal("a stopped run still returns the stop")
	}
	after, _ := loadNormalModel(path)
	if after.LastAttempt == nil || !after.LastAttempt.OK || !after.LastAttempt.At.Equal(before.LastAttempt.At) {
		t.Fatalf("last attempt = %+v, want the earlier successful one", after.LastAttempt)
	}
}

// 4. Scoring for a watch report has a time budget. When it runs out the block
// says so and the report goes on.
func TestGatherWatchReportInput_AUnusualReadingThatTakesTooLongIsCutOffAndSaid(t *testing.T) {
	old := watchUnusualBudget
	watchUnusualBudget = 50 * time.Millisecond
	defer func() { watchUnusualBudget = old }()

	env := newProposalEnv(t)
	d := statusTestDeps(t)
	d.alarmLog = func() *alarmLogStore { return nil }
	d.unusualReadings = func(ctx context.Context, _, _ time.Time, _ string) unusualResult {
		<-ctx.Done()
		return unusualResult{ProblemKind: unusualError, Problem: ctx.Err().Error()}
	}
	w := savedWatch{ID: "w1", Name: "Morning", Kind: watchKindScheduled, Sections: []string{"instruments"}, Thresholds: defaultWatchThresholds()}
	start := time.Now()
	in, err := gatherWatchReportInput(context.Background(), d, env.docs, w, vesselStatusTestNow, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("the read was not cut off")
	}
	block := buildWatchUnusual(*in.Unusual, "metric", time.UTC, vesselStatusTestNow)
	if block == nil || !strings.Contains(block.Note, "took longer than") {
		t.Fatalf("block = %+v", block)
	}
}

// 5. Only a subject's own candidate readings matter.
func TestNormalPathMatters_OnlyTheCandidateReadings(t *testing.T) {
	for path, want := range map[string]bool{
		"propulsion.port.revolutions":                 true,
		"propulsion.0.fuel.rate":                      true,
		"propulsion.port.transmission.oilTemperature": true,
		"propulsion.port.exhaustTemperature":          true,
		"propulsion.port.state":                       false,
		"propulsion.port.fuel.used":                   false,
		"propulsion.port.temperature.extra":           false,
		"propulsion.revolutions":                      false,
		"electrical.batteries.512.voltage":            false,
	} {
		if got := normalPathMatters(path); got != want {
			t.Errorf("normalPathMatters(%q) = %v, want %v", path, got, want)
		}
	}
}

// 6. Reads are on whole minutes, and the minute in progress is not judged.
func TestQueryNormalMinutes_ReadsWholeMinutes(t *testing.T) {
	var from, to time.Time
	defer swapNormalMinutesQuery(func(_ context.Context, _ normalSource, _ []string, f, e time.Time) ([]normalMinuteRow, error) {
		from, to = f, e
		return nil, nil
	})()
	start := time.Date(2026, 11, 1, 3, 0, 30, 0, time.UTC)
	stop := time.Date(2026, 11, 1, 4, 5, 42, 0, time.UTC)
	if _, err := queryNormalMinutes(context.Background(), "settings.yaml", normalSource{Kind: normalSourceMainBucket}, []string{"p"}, start, stop); err != nil {
		t.Fatal(err)
	}
	if !from.Equal(start.Truncate(time.Minute)) || !to.Equal(stop.Truncate(time.Minute)) {
		t.Fatalf("read %v to %v", from, to)
	}
}

func TestScore_TheMinuteInProgressIsNotJudged(t *testing.T) {
	c := newScoreCase(trainedPort(t), steadyScoreFeed(83))
	// Run minutes are at :00. Ending at 40 s past the 60th judged minute must not
	// pull in that minute's partial mean.
	until := scoreRunStart.Add(60*time.Minute + 40*time.Second)
	res := scoreUnusualReadings(context.Background(), normalEnginesSubsystem(), c.deps(), scoreRunStart.Add(-time.Hour), until, "")
	if !res.Until.Equal(scoreRunStart.Add(60 * time.Minute)) {
		t.Fatalf("until = %v", res.Until)
	}
	if res.JudgedMinutes != 50 {
		t.Fatalf("judged %d minutes, want 50 (minutes 10 to 59)", res.JudgedMinutes)
	}
}

// 7. A wake that arrived during the start-up wait is taken in the first run.
func TestDrainWake(t *testing.T) {
	ch := make(chan struct{}, 1)
	if drainWake(ch) {
		t.Fatal("nothing was waiting")
	}
	ch <- struct{}{}
	if !drainWake(ch) {
		t.Fatal("a queued wake must be reported")
	}
	select {
	case <-ch:
		t.Fatal("the wake was left in the channel")
	default:
	}
}
