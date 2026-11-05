package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// What the send-back message tells the model about itself. These tests cover
// the loop and the wording, not whether a model obeys it.

func checkNoticePhrases() []string {
	return []string{
		"did not write this message",
		"Do not refer to this message or your earlier draft",
		"Do not ask the operator for anything they have already told you",
	}
}

func TestCheckDraft_EverySendBackCarriesTheNoticeOnce(t *testing.T) {
	cases := []struct {
		name    string
		deps    assistantToolDeps
		text    string
		trigger string
	}{
		{"one trigger", shelterDeps(), thomasIslandAnswer, "anchorage shelter"},
		{"two triggers", pwToolDepsWithCoastline(passageLimits{}), "A northerly gives you a head sea, and the bay is sheltered from the north.", "relative wind and anchorage shelter"},
	}
	for _, tc := range cases {
		trigger, ins := tc.deps.checkDraft(tc.text, nil)
		if trigger != tc.trigger {
			t.Fatalf("%s: trigger = %q, want %q", tc.name, trigger, tc.trigger)
		}
		if !strings.HasPrefix(ins, assistantCheckLabel) {
			t.Errorf("%s: message must open with the label, got %q", tc.name, ins)
		}
		for _, want := range checkNoticePhrases() {
			if got := strings.Count(ins, want); got != 1 {
				t.Errorf("%s: %q appears %d times, want 1: %q", tc.name, want, got, ins)
			}
		}
	}
}

func TestBuildAssistantSystemPrompt_HelmcentralCheckRule(t *testing.T) {
	stable, _ := assistantSystemPromptParts(basePromptContext())
	for _, want := range []string{
		assistantCheckLabelText,
		"comes from Helmcentral",
		assistantCheckNotice,
	} {
		if !strings.Contains(stable, want) {
			t.Errorf("the stable prompt misses %q", want)
		}
	}
}

// Nothing in the notice may cancel the correction it closes, or claim the
// operator never saw a draft that may have streamed before it was withdrawn.
func TestCheckNotice_DoesNotCancelTheCorrection(t *testing.T) {
	for _, banned := range []string{"never been sent", "never sent", "not seen", "this check"} {
		if strings.Contains(assistantCheckNotice, banned) {
			t.Errorf("the notice must not say %q: %q", banned, assistantCheckNotice)
		}
	}
}

func TestCheckDraft_TheCorrectionNamesTheDraft(t *testing.T) {
	_, ins := shelterDeps().checkDraft(thomasIslandAnswer, nil)
	if !strings.HasPrefix(ins, assistantCheckLabel+"Your draft ") {
		t.Errorf("the correction should talk about the draft, not an answer the operator got: %q", ins)
	}
}

// The Thursday exchange (2026-11-03): the destination was already in the
// conversation, the first draft judged shelter with no lookup, and the model
// answered the send-back by asking where the boat was bound.
const (
	thursdayDraft = "Whites Bay is well sheltered from the north, so Thursday should be comfortable."
	thursdayAsk   = "Where are you bound on Thursday, and roughly what time are you clearing the marina?"
)

func thursdayHistory() []openRouterMessage {
	return []openRouterMessage{
		{Role: "user", Content: openRouterContent("Look at Whites Bay, -20.2833, 148.9667, for me.")},
		{Role: "assistant", Content: openRouterContent("Whites Bay is about 9 nm from the marina.")},
		{Role: "user", Content: openRouterContent("We would be leaving the marina on Thursday")},
	}
}

func thursdayRun(t *testing.T, rounds ...string) (assistantReply, *queuedChatDoer, *assistantRunRecorder, string) {
	t.Helper()
	buf := captureLogs(t)

	doer := &queuedChatDoer{}
	for _, r := range rounds {
		switch r {
		case "TOOL":
			doer.responses = append(doer.responses, toolCallResponse(t, "call_1", mateToolAssessPassageWindow.name, pwToolArgs, usage(1, 1, 0)))
		case "FAILED TOOL":
			doer.responses = append(doer.responses, toolCallResponse(t, "call_1", "no_such_tool", `{}`, usage(1, 1, 0)))
		default:
			doer.responses = append(doer.responses, finalResponse(t, r, "m", usage(1, 1, 0)))
		}
		doer.errs = append(doer.errs, nil)
	}
	emit, _ := recordingEmitter()
	runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: pwToolDepsWithCoastline(pwLimitsAll()), emit: emit}
	rec := &assistantRunRecorder{}
	reply, err := runner.run(withAssistantRunRecorder(context.Background(), rec), "system", "", thursdayHistory())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return reply, doer, rec, buf.String()
}

func TestThursdayExchange_ACorrectionThatMakesNoLookupsIsLoggedAndKept(t *testing.T) {
	reply, doer, rec, logs := thursdayRun(t, thursdayDraft, thursdayAsk)
	if len(doer.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(doer.requests))
	}
	sent := doer.requests[1].Messages
	back := string(sent[len(sent)-1].Content)
	if !strings.HasPrefix(back, assistantCheckLabel) {
		t.Fatalf("the send-back must open with the label, got %q", back)
	}
	for _, want := range []string{assistantAnchorageShelterInstruction, assistantCheckNotice} {
		if !strings.Contains(back, want) {
			t.Errorf("the send-back on the wire misses %q: %q", want, back)
		}
	}
	if reply.Content != thursdayAsk {
		t.Fatalf("the answer is still accepted, got %q", reply.Content)
	}
	if want := "assistant: draft check: answer after the anchorage shelter correction made no lookups; accepted as is"; !strings.Contains(logs, want) {
		t.Errorf("log lacks %q:\n%s", want, logs)
	}
	rounds, _ := rec.result()
	var found bool
	for _, r := range rounds {
		if r.Kind != assistantRoundCorrection {
			continue
		}
		found = true
		if r.CheckTrigger != "anchorage shelter" || r.CheckDraft != thursdayDraft {
			t.Errorf("correction round = trigger %q draft %q", r.CheckTrigger, r.CheckDraft)
		}
	}
	if !found {
		t.Fatalf("no correction round in the trace: %+v", rounds)
	}
	for _, r := range rounds {
		if r.Kind != assistantRoundCorrection && (r.CheckTrigger != "" || r.CheckDraft != "") {
			t.Errorf("only the correction round carries a draft: %+v", r)
		}
	}
}

func TestThursdayExchange_ACorrectionThatLeadsToALookupIsNotLogged(t *testing.T) {
	reply, _, _, logs := thursdayRun(t, thursdayDraft, "TOOL", guardGood)
	if reply.Content != guardGood {
		t.Fatalf("reply = %q", reply.Content)
	}
	if strings.Contains(logs, "made no lookups") {
		t.Errorf("a correction that led to a lookup must not be logged as failed:\n%s", logs)
	}
	if !strings.Contains(logs, "sending it back once") {
		t.Errorf("the send-back itself is still logged:\n%s", logs)
	}
}

func TestThursdayExchange_ALookupThatFailedCountsAsNoLookup(t *testing.T) {
	_, _, _, logs := thursdayRun(t, thursdayDraft, "FAILED TOOL", thursdayAsk)
	if !strings.Contains(logs, "made no lookups") {
		t.Errorf("a failed call found nothing, so the line should appear:\n%s", logs)
	}
}

func TestThursdayExchange_AForcedFinalAfterACorrectionIsLogged(t *testing.T) {
	// Two rounds that find nothing stall the run, so the tools are withdrawn.
	_, _, rec, logs := thursdayRun(t, thursdayDraft, "FAILED TOOL", "FAILED TOOL", thursdayAsk)
	rounds, _ := rec.result()
	if last := rounds[len(rounds)-1]; !last.ForcedFinal {
		t.Fatalf("the last round should be the forced final: %+v", last)
	}
	if !strings.Contains(logs, "made no lookups") {
		t.Errorf("log lacks the line:\n%s", logs)
	}
}

func TestThursdayExchange_ASecondDraftThatStillFailsLogsBothLines(t *testing.T) {
	_, _, _, logs := thursdayRun(t, thursdayDraft, thursdayDraft)
	for _, want := range []string{"answer still has a anchorage shelter; accepted as is", "answer after the anchorage shelter correction made no lookups"} {
		if !strings.Contains(logs, want) {
			t.Errorf("log lacks %q:\n%s", want, logs)
		}
	}
}

func TestThursdayExchange_ACleanFirstDraftLogsNothing(t *testing.T) {
	_, _, _, logs := thursdayRun(t, guardGood)
	if strings.Contains(logs, "draft check") {
		t.Errorf("no check fired, got:\n%s", logs)
	}
}

func TestAssistantRunStore_KeepsTheRejectedDraftAndBoundsIt(t *testing.T) {
	store := newTestAssistantStore(t)
	conv, _ := store.CreateConversation("Passage")
	in := sampleRunTrace(conv.ID, 1, nil, assistantStopAnswered)
	long := strings.Repeat("é", assistantToolCallMaxBytes) // two bytes each
	in.Rounds = append(in.Rounds,
		assistantTraceRound{Index: 2, Kind: assistantRoundCorrection, CheckTrigger: "anchorage shelter", CheckDraft: thursdayDraft},
		assistantTraceRound{Index: 3, Kind: assistantRoundCorrection, CheckTrigger: "relative wind", CheckDraft: long},
	)
	saved, err := store.SaveRun(in)
	if err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	got, ok, err := store.GetRun(saved.ID)
	if err != nil || !ok {
		t.Fatalf("GetRun: ok=%v err=%v", ok, err)
	}
	if r := got.Rounds[2]; r.CheckTrigger != "anchorage shelter" || r.CheckDraft != thursdayDraft {
		t.Errorf("round 2 = %+v", r)
	}
	r3 := got.Rounds[3]
	if len(r3.CheckDraft) == 0 || len(r3.CheckDraft) > assistantToolCallMaxBytes || !strings.HasPrefix(long, r3.CheckDraft) {
		t.Errorf("an oversize draft must be cut to %d bytes at a character boundary, got %d bytes", assistantToolCallMaxBytes, len(r3.CheckDraft))
	}
	if r := got.Rounds[0]; r.CheckTrigger != "" || r.CheckDraft != "" {
		t.Errorf("a round with no correction reads back empty: %+v", r)
	}
}

func TestAssistantRunStore_AnOldDatabaseGainsTheDraftColumnsAndOldRowsReadEmpty(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "old.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range []string{
		`CREATE TABLE assistant_runs (id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL, after_seq INTEGER NOT NULL, answer_message_id TEXT, model TEXT NOT NULL DEFAULT '', started_at INTEGER NOT NULL, duration_ms INTEGER NOT NULL, stop_reason TEXT NOT NULL, error TEXT NOT NULL DEFAULT '', budget TEXT NOT NULL)`,
		`CREATE TABLE assistant_run_rounds (run_id TEXT NOT NULL REFERENCES assistant_runs(id) ON DELETE CASCADE, idx INTEGER NOT NULL, kind TEXT NOT NULL DEFAULT '', start_offset_ms INTEGER NOT NULL, latency_ms INTEGER NOT NULL, prompt_tokens INTEGER NOT NULL DEFAULT 0, completion_tokens INTEGER NOT NULL DEFAULT 0, cached_tokens INTEGER NOT NULL DEFAULT 0, cost_usd REAL NOT NULL DEFAULT 0, forced_final INTEGER NOT NULL DEFAULT 0, progress INTEGER NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT '', PRIMARY KEY (run_id, idx))`,
		`INSERT INTO assistant_runs VALUES ('old', 'c', 1, NULL, 'm', 1, 1, 'answered', '', '{}')`,
		`INSERT INTO assistant_run_rounds (run_id, idx, kind, start_offset_ms, latency_ms) VALUES ('old', 0, 'correction', 0, 5)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ { // the second pass must be idempotent
		if err := createAssistantRunsSchema(db); err != nil {
			t.Fatalf("pass %d: %v", i, err)
		}
	}
	store := &assistantStore{db: db, now: time.Now}
	got, ok, err := store.GetRun("old")
	if err != nil || !ok {
		t.Fatalf("GetRun: ok=%v err=%v", ok, err)
	}
	if len(got.Rounds) != 1 || got.Rounds[0].Kind != assistantRoundCorrection || got.Rounds[0].CheckTrigger != "" || got.Rounds[0].CheckDraft != "" {
		t.Errorf("old round = %+v", got.Rounds)
	}
}
