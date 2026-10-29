package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func chartRunEnv(t *testing.T) (*proposalEnv, savedWatch) {
	t.Helper()
	env := newProposalEnv(t)
	c, rec := watchRequestTo(http.MethodPost, "/api/watches", `{"name":"At anchor","schedule":"0 */2 * * *","sections":["power"]}`)
	if err := createWatchHandler(c); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("create: %v %d %s", err, rec.Code, rec.Body)
	}
	var w savedWatch
	_ = json.Unmarshal(rec.Body.Bytes(), &w)
	chart := testWatchChart()
	err := env.docs.RunTx("run", func(tx *sql.Tx, _ time.Time) error {
		return insertWatchRunTx(tx, watchRun{ID: "r1", WatchID: w.ID, StartedAt: env.clock, FinishedAt: env.clock, Outcome: watchOutcomeRan, CostKnown: true,
			Findings: []watchFinding{{Section: "weather", Rule: "pressure_fall", Text: "t", By: watchByRules}, {Section: "tanks", Rule: "tank_change", Text: "u", By: watchByRules, ChartProblem: "No history to chart: nothing was recorded"}},
			Charts:   []watchChart{chart}}, true)
	})
	if err != nil {
		t.Fatal(err)
	}
	return env, w
}

func TestWatchRun_ChartsAndProblemsAreStoredWithTheRun(t *testing.T) {
	env, w := chartRunEnv(t)
	var run watchRun
	if err := env.docs.Read(func(q sqlQueryer) error {
		var err error
		run, err = watchRunByIDQ(q, w.ID, "r1")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(run.Charts) != 1 || run.Charts[0].Title != "Pressure, last 12 hours" || len(run.Charts[0].Series[0].Points) != 49 || run.Charts[0].Threshold == nil {
		t.Fatalf("charts %+v", run.Charts)
	}
	if run.Findings[1].ChartProblem == "" {
		t.Errorf("the chart problem is kept on its finding")
	}
	raw, _ := json.Marshal(run)
	var back map[string]any
	_ = json.Unmarshal(raw, &back)
	if _, ok := back["charts"].([]any); !ok {
		t.Errorf("a run's JSON has charts: %s", raw)
	}
}

func TestWatchRun_DeliveryNotesAreStoredWithTheRun(t *testing.T) {
	env, w := chartRunEnv(t)
	if err := env.docs.RunTx("note", func(tx *sql.Tx, _ time.Time) error {
		return addWatchRunNotifyNoteTx(tx, w.ID, "r1", "ntfy: sent without the chart (the server refused attachments)")
	}); err != nil {
		t.Fatal(err)
	}
	var run watchRun
	_ = env.docs.Read(func(q sqlQueryer) error {
		var err error
		run, err = watchRunByIDQ(q, w.ID, "r1")
		return err
	})
	if len(run.NotifyNotes) != 1 || run.NotifyNotes[0] != "ntfy: sent without the chart (the server refused attachments)" {
		t.Fatalf("notes %v", run.NotifyNotes)
	}
}
