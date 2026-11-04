package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// A chat opened from a run's Answer button reads the run through
// get_watch_runs. The questions Mate asked in that review must be in what it
// reads, or it cannot tell what the skipper is answering.
func TestGetWatchRunsGivesMateTheRunsQuestions(t *testing.T) {
	env := newProposalEnv(t)
	env.applyOK(t, watchCreateArgs)
	w := firstWatch(t, env.docs)
	review := "Fridge draw is up."
	if err := env.docs.RunTx("run", func(tx *sql.Tx, _ time.Time) error {
		return insertWatchRunTx(tx, watchRun{ID: "run-q", WatchID: w.ID, StartedAt: env.clock, FinishedAt: env.clock, Outcome: watchOutcomeRan,
			Report: json.RawMessage(`{"kind":"sections","sections":[]}`), Review: &review, CostKnown: true,
			Questions: []string{"Did you run the watermaker?"}}, true)
	}); err != nil {
		t.Fatal(err)
	}
	out, err := env.deps.executeGetWatchRuns(context.Background(), json.RawMessage(`{"run_id":"run-q"}`))
	if err != nil || !strings.Contains(out, `"questions":["Did you run the watermaker?"]`) {
		t.Fatalf("Mate does not see the run's questions: %s %v", out, err)
	}
}

const storedReportWithRows = `{"kind":"sections","since":"x","sections":[` +
	`{"name":"power","title":"Power","lines":["House bank: 80 %"],"rows":[{"label":"House bank","value":"80 %"}],"summary":"all well",` +
	`"not_checked":["Overnight draw: no house bank"],"figures":{"raised":3}},` +
	`{"name":"anchor","title":"Anchor","unavailable":"no position"}]}`

// The page's rows and summary repeat the lines. Mate is not sent them from
// either place it reads a report, and the stored report keeps them.
func TestWatchReportForModel_LeavesOutRowsAndSummaryAndKeepsTheRest(t *testing.T) {
	in := json.RawMessage(storedReportWithRows)
	out, err := watchReportForModel(in)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if strings.Contains(got, `"rows"`) || strings.Contains(got, `"summary"`) {
		t.Errorf("rows or summary left in: %s", got)
	}
	for _, want := range []string{`"lines":["House bank: 80 %"]`, `"not_checked":["Overnight draw: no house bank"]`, `"figures":{"raised":3}`, `"unavailable":"no position"`, `"since":"x"`} {
		if !strings.Contains(got, want) {
			t.Errorf("lost %s: %s", want, got)
		}
	}
	if string(in) != storedReportWithRows {
		t.Errorf("the input was changed: %s", in)
	}
	if empty, err := watchReportForModel(nil); err != nil || len(empty) != 0 {
		t.Errorf("no report is no report: %s %v", empty, err)
	}
	if _, err := watchReportForModel(json.RawMessage(`nope`)); err == nil {
		t.Error("a report that is not JSON is an error, not passed on")
	}
}

func TestGetWatchRunsLeavesOutTheRowsAndSummaryButKeepsQuestionsAndLines(t *testing.T) {
	env := newProposalEnv(t)
	env.applyOK(t, watchCreateArgs)
	w := firstWatch(t, env.docs)
	review := "Fridge draw is up."
	if err := env.docs.RunTx("run", func(tx *sql.Tx, _ time.Time) error {
		return insertWatchRunTx(tx, watchRun{ID: "run-r", WatchID: w.ID, StartedAt: env.clock, FinishedAt: env.clock, Outcome: watchOutcomeRan,
			Report: json.RawMessage(storedReportWithRows), Review: &review, CostKnown: true,
			Questions: []string{"Did you run the watermaker?"}}, true)
	}); err != nil {
		t.Fatal(err)
	}
	out, err := env.deps.executeGetWatchRuns(context.Background(), json.RawMessage(`{"run_id":"run-r"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, `"rows"`) || strings.Contains(out, `"summary"`) {
		t.Errorf("Mate was sent the page's rows or summary: %s", out)
	}
	for _, want := range []string{`House bank: 80 %`, `"not_checked"`, `"figures"`, `"unavailable"`, `"questions":["Did you run the watermaker?"]`} {
		if !strings.Contains(out, want) {
			t.Errorf("lost %s: %s", want, out)
		}
	}
	// Reading it for Mate does not change what is stored.
	var stored watchRun
	if err := env.docs.Read(func(q sqlQueryer) error {
		var err error
		stored, err = watchRunAnyByIDQ(q, "run-r")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stored.Report), `"rows"`) {
		t.Errorf("the stored report lost its rows: %s", stored.Report)
	}
}
