package main

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"
)

// Mate's questions and the findings' extends are stored with the run, and a
// run saved before them still loads (ADR 0255).

func TestWatchRun_QuestionsAndExtendsAreStoredAndReadBackEverywhere(t *testing.T) {
	env, w := chartRunEnv(t)
	zero := 0
	later := env.clock.Add(time.Hour)
	err := env.docs.RunTx("run 2", func(tx *sql.Tx, _ time.Time) error {
		return insertWatchRunTx(tx, watchRun{ID: "r2", WatchID: w.ID, StartedAt: later, FinishedAt: later, Outcome: watchOutcomeRan, CostKnown: true,
			Findings:  []watchFinding{{Section: "tanks", Rule: "tank_change", Text: "t", By: watchByRules}, {Section: "tanks", Rule: "mate_review", Text: "u", By: watchByMate, Key: "k", Extends: &zero}},
			Questions: []string{"Were the heaters on?", "Is the fridge new?"}}, true)
	})
	if err != nil {
		t.Fatal(err)
	}
	check := func(where string, r watchRun) {
		t.Helper()
		if len(r.Questions) != 2 || r.Questions[1] != "Is the fridge new?" {
			t.Errorf("%s: questions %q", where, r.Questions)
		}
		if len(r.Findings) != 2 || r.Findings[1].Extends == nil || *r.Findings[1].Extends != 0 || r.Findings[0].Extends != nil {
			t.Errorf("%s: findings %+v", where, r.Findings)
		}
	}
	if err := env.docs.Read(func(q sqlQueryer) error {
		byID, err := watchRunByIDQ(q, w.ID, "r2")
		if err != nil {
			return err
		}
		check("by id", byID)
		any, err := watchRunAnyByIDQ(q, "r2")
		if err != nil {
			return err
		}
		check("any by id", any)
		page, err := watchRunsQ(q, w.ID, 10, nil)
		if err != nil {
			return err
		}
		check("page", page[0])
		last, ok, err := lastRanRunQ(q, w.ID)
		if err != nil || !ok {
			return err
		}
		check("last ran", last)
		before, ok, err := lastRanRunBeforeQ(q, w.ID, later.Add(time.Minute), "zzz")
		if err != nil || !ok {
			return err
		}
		check("last ran before", before)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(watchRun{Questions: []string{"a?"}, Findings: []watchFinding{{Extends: &zero}}})
	var back map[string]any
	_ = json.Unmarshal(raw, &back)
	if qs, ok := back["questions"].([]any); !ok || len(qs) != 1 {
		t.Errorf("the run's JSON has questions: %s", raw)
	}
	if f := back["findings"].([]any)[0].(map[string]any); f["extends"] != float64(0) {
		t.Errorf("the finding's JSON has extends, even when it is 0: %s", raw)
	}
	plain, _ := json.Marshal(watchFinding{Section: "tanks"})
	if string(plain) != `{"section":"tanks","rule":"","text":"","by":""}` {
		t.Errorf("a finding with no extends has none: %s", plain)
	}
}

func TestWatchRun_ARunSavedBeforeQuestionsStillLoads(t *testing.T) {
	env, w := chartRunEnv(t)
	// r1 was saved with no questions: the column is NULL, as on an old database.
	if err := env.docs.RunTx("null", func(tx *sql.Tx, _ time.Time) error {
		_, err := tx.Exec(`UPDATE watch_runs SET questions = NULL`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var run watchRun
	if err := env.docs.Read(func(q sqlQueryer) error {
		var err error
		run, err = watchRunByIDQ(q, w.ID, "r1")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(run.Questions) != 0 {
		t.Errorf("questions %q", run.Questions)
	}
	raw, _ := json.Marshal(run)
	var back map[string]any
	_ = json.Unmarshal(raw, &back)
	if _, has := back["questions"]; has {
		t.Errorf("a run with none has no questions field: %s", raw)
	}
}

func TestWatchRun_TheQuestionsColumnIsAddedToAnOlderDatabase(t *testing.T) {
	env, w := chartRunEnv(t)
	if _, err := env.docs.db.Exec(`ALTER TABLE watch_runs DROP COLUMN questions`); err != nil {
		t.Fatal(err)
	}
	if err := createWatchesSchema(env.docs.db); err != nil {
		t.Fatalf("the schema is brought up to date: %v", err)
	}
	if err := env.docs.Read(func(q sqlQueryer) error {
		r, err := watchRunByIDQ(q, w.ID, "r1")
		if err == nil && len(r.Questions) != 0 {
			t.Errorf("questions %q", r.Questions)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
