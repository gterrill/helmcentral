package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func postQuestion(t *testing.T, convID string) {
	t.Helper()
	c, _ := newAssistantEchoContext(http.MethodPost, "/api/assistant/conversations/"+convID+"/messages", `{"today":"2026-09-30","content":"check the passage"}`, convID)
	if err := postAssistantMessageHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
}

func readyAssistant(t *testing.T) (*assistantStore, assistantConversation) {
	t.Helper()
	secrets := withTestSecretsStore(t)
	if err := secrets.Set("OPENROUTER_API_KEY", "sk-test"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	store := withTestAssistantStore(t)
	t.Setenv("SETTINGS_FILE", writeAssistantSettingsFixture(t, true, "openai/gpt-4o"))
	conv, err := store.CreateConversation("")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	return store, conv
}

func TestAssistantRunTrace_AnAnsweredRunIsStoredWithItsRoundsAndAnswer(t *testing.T) {
	store, conv := readyAssistant(t)
	swapAssistantRunner(t, func(apiKey, model string, tools assistantToolDeps, autoRouter assistantRunnerOptions, emit assistantEmitter) assistantRunnerFace {
		doer := queued(newCallRound(t, 0), finalResponse(t, "Looks fine.", "openai/gpt-4o", usage(10, 5, 0.001)))
		return &assistantRunner{doer: doer, apiKey: apiKey, model: model, tools: &fakeToolExecutor{}, emit: emit}
	})

	postQuestion(t, conv.ID)

	messages, _ := store.ListMessages(conv.ID)
	if len(messages) != 2 || messages[1].Role != "assistant" {
		t.Fatalf("messages = %+v", messages)
	}
	runs, err := store.ListRuns(conv.ID)
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs = %+v, err = %v", runs, err)
	}
	run := runs[0]
	if run.AnswerMessageID == nil || *run.AnswerMessageID != messages[1].ID {
		t.Fatalf("answer id = %v, want %s", run.AnswerMessageID, messages[1].ID)
	}
	if run.StopReason != assistantStopAnswered || run.RoundCount != 1 || run.AfterSeq != messages[0].Seq || run.Model != "openai/gpt-4o" || run.Error != "" {
		t.Fatalf("run = %+v", run)
	}
	calls := toolCallsForTest(t, store, messages[1].ID)
	if len(calls) != 1 || calls[0].Name != "get_tides" || calls[0].Result != "{}" {
		t.Fatalf("the answer's calls should be found through its run: %+v", calls)
	}

	// The conversation names an answered run by id and answer only.
	c, rec := newAssistantEchoContext(http.MethodGet, "/api/assistant/conversations/"+conv.ID, "", conv.ID)
	if err := getAssistantConversationHandler(c); err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	var body struct {
		Runs []map[string]any `json:"runs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Runs) != 1 {
		t.Fatalf("runs = %v, err = %v", body.Runs, err)
	}
	if got := body.Runs[0]; len(got) != 2 || got["id"] != run.ID || got["answer_message_id"] != messages[1].ID {
		t.Fatalf("an answered run should be listed as {id, answer_message_id}, got %v", got)
	}
}

func TestAssistantRunTrace_AFailedRunIsStoredAndTheConversationReturnsIt(t *testing.T) {
	store, conv := readyAssistant(t)
	swapAssistantRunner(t, func(apiKey, model string, tools assistantToolDeps, autoRouter assistantRunnerOptions, emit assistantEmitter) assistantRunnerFace {
		doer := budgetDoerFunc(func(req *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("read openrouter stream: %w", context.DeadlineExceeded)
		})
		return &assistantRunner{doer: doer, apiKey: apiKey, model: model, tools: &fakeToolExecutor{}, emit: emit}
	})

	postQuestion(t, conv.ID)

	messages, _ := store.ListMessages(conv.ID)
	if len(messages) != 1 || messages[0].Role != "user" {
		t.Fatalf("a failed run must leave only the question in the thread, got %+v", messages)
	}
	c, rec := newAssistantEchoContext(http.MethodGet, "/api/assistant/conversations/"+conv.ID, "", conv.ID)
	if err := getAssistantConversationHandler(c); err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	var body struct {
		Runs []assistantRunTrace `json:"runs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Runs) != 1 {
		t.Fatalf("runs = %+v", body.Runs)
	}
	run := body.Runs[0]
	if run.AnswerMessageID != nil || run.StopReason != assistantStopError || run.AfterSeq != messages[0].Seq || run.Error == "" {
		t.Fatalf("run = %+v", run)
	}
	if run.RoundCount != 0 {
		t.Fatalf("a run that never looked anything up has no lookup rounds: %+v", run)
	}
}

func TestAssistantRunTrace_ACancelledRunIsStored(t *testing.T) {
	store, conv := readyAssistant(t)
	started := make(chan struct{})
	swapAssistantRunner(t, func(apiKey, model string, tools assistantToolDeps, autoRouter assistantRunnerOptions, emit assistantEmitter) assistantRunnerFace {
		return &cancelAwareAssistantRunner{emit: emit, started: started}
	})
	c, _ := newAssistantEchoContext(http.MethodPost, "/api/assistant/conversations/"+conv.ID+"/messages", `{"today":"2026-09-30","content":"hi"}`, conv.ID)
	done := make(chan struct{})
	go func() {
		_ = postAssistantMessageHandler(c)
		close(done)
	}()
	<-started
	cc, _ := newAssistantEchoContext(http.MethodPost, "/api/assistant/conversations/"+conv.ID+"/run/cancel", "", conv.ID)
	if err := postAssistantRunCancelHandler(cc); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	// The cancel answers only once the run is released, and the trace is saved
	// before that: a read right after it must already see the run.
	if runs, _ := store.ListRuns(conv.ID); len(runs) != 1 {
		t.Fatalf("the trace must be stored before the cancel answers, got %+v", runs)
	}
	<-done

	runs, _ := store.ListRuns(conv.ID)
	if len(runs) != 1 || runs[0].StopReason != assistantStopCancelled || runs[0].AnswerMessageID != nil {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestAssistantRunTrace_GetRunReturnsTheFullTraceAndUnknownIsNotFound(t *testing.T) {
	store, conv := readyAssistant(t)
	saved, err := store.SaveRun(sampleRunTrace(conv.ID, 1, nil, assistantStopStalled))
	if err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	c, rec := newAssistantEchoContext(http.MethodGet, "/api/assistant/runs/"+saved.ID, "", saved.ID)
	if err := getAssistantRunTraceHandler(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	var got assistantRunTrace
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != saved.ID || len(got.Rounds) != 2 || got.Rounds[0].Calls[0].Cache != "miss" || got.Budget.MaxRounds != assistantMaxToolRounds {
		t.Fatalf("trace = %+v", got)
	}

	c, rec = newAssistantEchoContext(http.MethodGet, "/api/assistant/runs/nope", "", "nope")
	if err := getAssistantRunTraceHandler(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestAssistantRunTrace_VerboseRunLogSettingReachesTheRunner(t *testing.T) {
	for _, tc := range []struct {
		yaml string
		want bool
	}{
		{"    verbose_run_log: true\n", true},
		{"    verbose_run_log: false\n", false},
		{"", false},
	} {
		_, conv := readyAssistant(t)
		path := filepath.Join(t.TempDir(), "settings.yaml")
		body := "assistant:\n    enabled: true\n    model: \"openai/gpt-4o\"\n    notes: \"\"\n" + tc.yaml
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("SETTINGS_FILE", path)
		got, asked := false, false
		swapAssistantRunner(t, func(apiKey, model string, tools assistantToolDeps, options assistantRunnerOptions, emit assistantEmitter) assistantRunnerFace {
			got, asked = options.VerboseRunLog, true
			return &fakeAssistantRunner{emit: emit, reply: assistantReply{Content: "ok", Model: "m"}}
		})
		postQuestion(t, conv.ID)
		if !asked || got != tc.want {
			t.Fatalf("settings %q: verbose = %v, want %v", tc.yaml, got, tc.want)
		}
	}
}

func TestAssistantRunTrace_StopReasonForTheRunnerErrors(t *testing.T) {
	for _, tc := range []struct {
		forced    string
		err       error
		cancelled bool
		timeUp    bool
		want      string
	}{
		{"", nil, false, false, assistantStopAnswered},
		{assistantStopStalled, nil, false, false, assistantStopStalled},
		{"", errors.New("boom"), false, false, assistantStopError},
		{assistantStopRoundCeiling, errors.New("empty"), false, false, assistantStopRoundCeiling},
		{"", fmt.Errorf("stream: %w", context.DeadlineExceeded), false, true, assistantStopTimeCeiling},
		{"", fmt.Errorf("stream: %w", context.DeadlineExceeded), false, false, assistantStopError},
		{"", context.Canceled, true, false, assistantStopCancelled},
	} {
		if got := assistantStopReasonFor(tc.forced, tc.err, tc.cancelled, tc.timeUp); got != tc.want {
			t.Errorf("assistantStopReasonFor(%q, %v, %v, %v) = %q, want %q", tc.forced, tc.err, tc.cancelled, tc.timeUp, got, tc.want)
		}
	}
}
