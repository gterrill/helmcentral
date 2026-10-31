package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Cases found reviewing the run budget (ADR 0240).

func TestAssistantRunReview_APanicRecordsErrorNotAnswered(t *testing.T) {
	doer := budgetDoerFunc(func(req *http.Request) (*http.Response, error) { panic("kaboom") })
	emit, _ := recordingEmitter()
	rec := &assistantRunRecorder{}
	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: &fakeToolExecutor{}, emit: emit}

	_, err := runner.run(withAssistantRunRecorder(context.Background(), rec), "system", "", nil)
	if err == nil || !strings.Contains(err.Error(), "crashed: kaboom") {
		t.Fatalf("err = %v, want the crash as an error", err)
	}
	if _, stop := rec.result(); stop != assistantStopError {
		t.Fatalf("stop reason = %q, want %q", stop, assistantStopError)
	}
}

func TestAssistantRunReview_CallsRefusedForTheWidthCapAreNotSeen(t *testing.T) {
	wide := func(from, to int) *http.Response {
		var calls []openRouterToolCall
		for i := from; i < to; i++ {
			calls = append(calls, openRouterToolCall{ID: fmt.Sprintf("c%d", i), Type: "function", Function: openRouterToolCallFunction{
				Name: "get_tides", Arguments: openRouterArguments(fmt.Sprintf(`{"lat":%d,"lon":2}`, i))}})
		}
		return chatResponse(t, http.StatusOK, openRouterChatResponse{Choices: []openRouterChoice{{Message: openRouterMessage{Role: "assistant", ToolCalls: calls}}}})
	}
	total := assistantMaxToolCallsPerRound + 4
	doer := queued(wide(0, total), wide(assistantMaxToolCallsPerRound, total), finalResponse(t, "done", "m", openRouterUsage{}))
	emit, _ := recordingEmitter()
	rec := &assistantRunRecorder{}
	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: &fakeToolExecutor{}, emit: emit}
	if _, err := runner.run(withAssistantRunRecorder(context.Background(), rec), "system", "", nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	rounds, _ := rec.result()
	if len(rounds) < 2 || !rounds[1].Progress {
		t.Fatalf("the refused calls were never run, so asking again is new: %+v", rounds)
	}
}

func TestAssistantRunReview_TheRunCeilingHoldsWhenAStreamingRoundRunsOn(t *testing.T) {
	shortBudget(t, 60*time.Millisecond, 120*time.Millisecond)
	if got, want := assistantRunCeiling(), 60*time.Millisecond+2*120*time.Millisecond; got != want {
		t.Fatalf("ceiling = %s, want tool phase plus a reserve for the streaming round and one for the final: %s", got, want)
	}
	first := &streamingAnswerDoer{first: "Looks fine, ", rest: "never sent", delay: time.Minute}
	calls := 0
	doer := budgetDoerFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return first.Do(req)
		}
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	emit, _ := recordingEmitter()
	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: &fakeToolExecutor{}, emit: emit}
	start := time.Now()
	_, _ = runner.run(context.Background(), "system", "", nil)
	if took := time.Since(start); took > assistantRunCeiling()+300*time.Millisecond {
		t.Fatalf("took %s, past the ceiling %s", took, assistantRunCeiling())
	}
}

func TestAssistantRunReview_ACompletionTimeoutIsAnErrorNotATimeCeiling(t *testing.T) {
	orig := openRouterCompletionTimeout
	openRouterCompletionTimeout = 50 * time.Millisecond
	t.Cleanup(func() { openRouterCompletionTimeout = orig })
	doer := budgetDoerFunc(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	emit, _ := recordingEmitter()
	rec := &assistantRunRecorder{}
	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: &fakeToolExecutor{}, emit: emit}
	_, err := runner.run(withAssistantRunRecorder(context.Background(), rec), "system", "", nil)
	if err == nil || !strings.Contains(err.Error(), "a reply from the model took too long") {
		t.Fatalf("err = %v", err)
	}
	if _, stop := rec.result(); stop != assistantStopError {
		t.Fatalf("stop reason = %q, want %q: the tool phase and final reserve had not run out", stop, assistantStopError)
	}
}

func TestAssistantRunReview_CallsThatFinishedWhenThePhaseEndsReachTheFinalAnswer(t *testing.T) {
	shortBudget(t, 150*time.Millisecond, 5*time.Second)
	doer := queued(
		chatResponse(t, http.StatusOK, openRouterChatResponse{Choices: []openRouterChoice{{Message: openRouterMessage{Role: "assistant", ToolCalls: []openRouterToolCall{
			{ID: "a", Type: "function", Function: openRouterToolCallFunction{Name: "get_tides", Arguments: openRouterArguments(`{"lat":1}`)}},
			{ID: "b", Type: "function", Function: openRouterToolCallFunction{Name: "get_wind_forecast", Arguments: openRouterArguments(`{"lat":1}`)}},
		}}}}}),
		finalResponse(t, "tides only", "m", openRouterUsage{}),
	)
	emit, _ := recordingEmitter()
	tools := &orderedDelayToolExecutor{delays: map[string]time.Duration{"get_wind_forecast": time.Minute}, results: map[string]string{"get_tides": `{"high":"14:10"}`}}
	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: tools, emit: emit}
	if _, err := runner.run(context.Background(), "system", "", nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	system, _ := json.Marshal(doer.requests[1].Messages[0])
	if !strings.Contains(string(system), `get_tides({\"lat\":1}) -> {\"high\":\"14:10\"}`) && !strings.Contains(string(system), "14:10") {
		t.Fatalf("the finished tide lookup should be in the final answer's log: %s", system)
	}
	if strings.Contains(string(system), "get_wind_forecast(") {
		t.Fatalf("the abandoned wind lookup should not be: %s", system)
	}
}

func TestAssistantRunReview_ARatingNeedsNoRunRowUnlessItWasPruned(t *testing.T) {
	store := newTestAssistantStore(t)
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now.Add(-time.Hour) }
	conv, _ := store.CreateConversation("x")
	recent, _ := store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "assistant", Content: "a", ToolRounds: 2})
	store.now = func() time.Time { return now.Add(-assistantRunRetention - time.Hour) }
	old, _ := store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "assistant", Content: "b", ToolRounds: 2})
	store.now = func() time.Time { return now }

	if _, err := store.SetMessageFeedback(recent.ID, "up", ""); err != nil {
		t.Fatalf("a recent answer whose run is not on record yet can be rated: %v", err)
	}
	if _, err := store.SetMessageFeedback(old.ID, "up", ""); err != nil {
		t.Fatalf("an old answer whose run was never stored (not pruned) can be rated: %v", err)
	}
}

func TestAssistantRunReview_TheOrphanedToolCallTableIsDroppedOnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "assistant.sqlite")
	store, err := newAssistantStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`CREATE TABLE message_tool_calls (message_id TEXT)`); err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = newAssistantStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var n int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'message_tool_calls'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("message_tool_calls still exists: n=%d err=%v", n, err)
	}
}
