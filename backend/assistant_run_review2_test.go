package main

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Second review of the run budget (ADR 0240).

func TestAssistantRunReview2_LegacyToolCallsMoveToRunsBeforeTheTableGoes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "assistant.sqlite")
	store, err := newAssistantStore(path)
	if err != nil {
		t.Fatal(err)
	}
	conv, _ := store.CreateConversation("x")
	store.now = func() time.Time { return time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC) }
	store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "user", Content: "q"})
	answer, _ := store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "assistant", Content: "a", Model: "m1", ToolRounds: 2})
	covered, _ := store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "assistant", Content: "b", ToolRounds: 1})
	saveAnswerRun(t, store, covered, time.Now(), assistantToolCall{Name: "kept", Args: "{}", Result: "{}"})
	for _, stmt := range []string{
		`CREATE TABLE message_tool_calls (message_id TEXT NOT NULL, round INTEGER NOT NULL, seq INTEGER NOT NULL, name TEXT NOT NULL, args TEXT NOT NULL DEFAULT '', result TEXT NOT NULL DEFAULT '', elapsed_ms INTEGER NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT '', truncated INTEGER NOT NULL DEFAULT 0, original_bytes INTEGER, PRIMARY KEY (message_id, round, seq))`,
		`INSERT INTO message_tool_calls (message_id, round, seq, name, args, result, elapsed_ms) VALUES ('` + answer.ID + `', 0, 0, 'get_tides', '{"lat":1}', '{"high":"14:10"}', 40)`,
		`INSERT INTO message_tool_calls (message_id, round, seq, name, args, result, elapsed_ms, error, truncated, original_bytes) VALUES ('` + answer.ID + `', 1, 0, 'find_places', '{}', '{}', 3, 'boom', 1, 5000)`,
		`INSERT INTO message_tool_calls (message_id, round, seq, name) VALUES ('` + covered.ID + `', 0, 0, 'old')`,
	} {
		if _, err := store.db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	store.Close()

	store, err = newAssistantStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var tables int
	store.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'message_tool_calls'`).Scan(&tables)
	if tables != 0 {
		t.Fatal("the legacy table should be gone")
	}
	calls := toolCallsForTest(t, store, answer.ID)
	if len(calls) != 2 || calls[0].Name != "get_tides" || calls[0].Result != `{"high":"14:10"}` || calls[1].Round != 1 || calls[1].Error != "boom" || calls[1].ResultBytes != 4998 {
		t.Fatalf("calls = %+v", calls)
	}
	if got := toolCallsForTest(t, store, covered.ID); len(got) != 1 || got[0].Name != "kept" {
		t.Fatalf("an answer that already has a run keeps it, not the legacy rows: %+v", got)
	}
	runs, _ := store.ListRuns(conv.ID)
	var migrated *assistantRunTrace
	for i := range runs {
		if runs[i].AnswerMessageID != nil && *runs[i].AnswerMessageID == answer.ID {
			migrated = &runs[i]
		}
	}
	if migrated == nil || migrated.StopReason != assistantStopAnswered || migrated.Model != "m1" || !migrated.StartedAt.Equal(answer.CreatedAt) {
		t.Fatalf("migrated run = %+v", migrated)
	}
	if run, ok, err := store.GetRun(migrated.ID); err != nil || !ok || len(run.Rounds) != 2 {
		t.Fatalf("the run should read back with its rounds: %v %v %+v", ok, err, run.Rounds)
	}
}

func TestAssistantRunReview2_OnlyAToolPhaseDeadlineCountsAsToolPhaseOver(t *testing.T) {
	shortBudget(t, 40*time.Millisecond, 5*time.Second)
	calls := 0
	doer := budgetDoerFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls > 1 {
			return finalResponse(t, "wrapped up", "m", openRouterUsage{}), nil
		}
		<-req.Context().Done()
		return nil, errors.New("openrouter returned 402: payment required")
	})
	emit, _ := recordingEmitter()
	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: &fakeToolExecutor{}, emit: emit}
	_, err := runner.run(context.Background(), "system", "", nil)
	if err == nil || !strings.Contains(err.Error(), "payment required") {
		t.Fatalf("a provider refusal after the deadline must fail the run with its own error, got %v", err)
	}
}

// deletingRunner runs the real runner and then removes the conversation, so
// storing the answer fails.
type deletingRunner struct {
	inner assistantRunnerFace
	store *assistantStore
	conv  string
}

func (d *deletingRunner) run(ctx context.Context, a, b string, h []openRouterMessage) (assistantReply, error) {
	reply, err := d.inner.run(ctx, a, b, h)
	_ = d.store.DeleteConversation(d.conv)
	return reply, err
}

func TestAssistantRunReview2_AnAnswerThatCouldNotBeStoredIsNotARunThatAnswered(t *testing.T) {
	store, conv := readyAssistant(t)
	swapAssistantRunner(t, func(apiKey, model string, tools assistantToolDeps, options assistantRunnerOptions, emit assistantEmitter) assistantRunnerFace {
		doer := queued(finalResponse(t, "Looks fine.", "openai/gpt-4o", usage(10, 5, 0.001)))
		return &deletingRunner{inner: &assistantRunner{doer: doer, apiKey: apiKey, model: model, tools: &fakeToolExecutor{}, emit: emit}, store: store, conv: conv.ID}
	})
	postQuestion(t, conv.ID)
	runs, err := store.ListRuns(conv.ID)
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs = %+v err = %v", runs, err)
	}
	if runs[0].AnswerMessageID != nil || runs[0].StopReason != assistantStopError || runs[0].Error == "" {
		t.Fatalf("run = %+v, want an error with the storage failure", runs[0])
	}
}

type panickingAssistantRunner struct{}

func (panickingAssistantRunner) run(context.Context, string, string, []openRouterMessage) (assistantReply, error) {
	panic("boom")
}

func TestAssistantRunReview2_ATurnStoresAtMostOneRun(t *testing.T) {
	store, conv := readyAssistant(t)
	swapAssistantRunner(t, func(apiKey, model string, tools assistantToolDeps, options assistantRunnerOptions, emit assistantEmitter) assistantRunnerFace {
		return panickingAssistantRunner{}
	})
	postQuestion(t, conv.ID)
	if runs, _ := store.ListRuns(conv.ID); len(runs) != 1 || runs[0].StopReason != assistantStopError {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestAssistantRunReview2_PruningAndRatingAgreeOnTheAnswersAge(t *testing.T) {
	store := newTestAssistantStore(t)
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	conv, _ := store.CreateConversation("x")
	// The run began ten minutes before its answer was stored.
	store.now = func() time.Time { return now.Add(-assistantRunRetention + time.Minute) }
	inside, _ := store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "assistant", Content: "a", ToolRounds: 1})
	saveAnswerRun(t, store, inside, now.Add(-assistantRunRetention-9*time.Minute), assistantToolCall{Name: "t"})
	store.now = func() time.Time { return now.Add(-assistantRunRetention - time.Minute) }
	outside, _ := store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "assistant", Content: "b", ToolRounds: 1})
	saveAnswerRun(t, store, outside, now.Add(-assistantRunRetention-11*time.Minute), assistantToolCall{Name: "t"})

	store.now = func() time.Time { return now }
	n, err := store.PruneRuns()
	if err != nil || n != 1 {
		t.Fatalf("pruned %d, %v: only the run behind the answer older than a week goes", n, err)
	}
	if got := toolCallsForTest(t, store, inside.ID); len(got) != 1 {
		t.Fatal("an answer under a week old keeps its run")
	}
	if _, err := store.SetMessageFeedback(inside.ID, "up", ""); err != nil {
		t.Fatalf("the answer under a week old can be rated: %v", err)
	}
	if _, err := store.SetMessageFeedback(outside.ID, "up", ""); !errors.Is(err, errAssistantFeedbackTooOld) {
		t.Fatalf("the answer past a week with its run gone cannot: %v", err)
	}
}

func TestAssistantRunReview2_TheFollowUpWaitFollowsTheBudgetAtUseTime(t *testing.T) {
	shortBudget(t, 10*time.Millisecond, 10*time.Millisecond)
	orig := assistantWatchFollowUpGrace
	assistantWatchFollowUpGrace = 0
	t.Cleanup(func() { assistantWatchFollowUpGrace = orig })
	if got := assistantWatchFollowUpWait(); got != 3*30*time.Millisecond {
		t.Fatalf("wait = %s", got)
	}
}

func TestAssistantRunReview2_AnUnattendedRunStopsAtItsOwnCeiling(t *testing.T) {
	var responses []*http.Response
	for i := 0; i < assistantUnattendedMaxToolRounds; i++ {
		responses = append(responses, newCallRound(t, i))
	}
	responses = append(responses, finalResponse(t, "reviewed", "m", openRouterUsage{}))
	doer := queued(responses...)
	emit, _ := recordingEmitter()
	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: &fakeToolExecutor{}, emit: emit, options: assistantRunnerOptions{Unattended: true}}
	reply, err := runner.run(context.Background(), "system", "", nil)
	if err != nil || reply.Content != "reviewed" {
		t.Fatalf("reply %q err %v", reply.Content, err)
	}
	if len(doer.requests) != assistantUnattendedMaxToolRounds+1 || len(doer.requests[assistantUnattendedMaxToolRounds].Tools) != 0 {
		t.Fatalf("%d requests: an unattended run must withdraw its tools after %d rounds", len(doer.requests), assistantUnattendedMaxToolRounds)
	}
}

func TestAssistantRunReview2_ReviewRunnersAreUnattendedAndTheirBudgetIsInTheTrace(t *testing.T) {
	runner, ok := watchReviewRunnerFor("k", "m", &fakeToolExecutor{}, assistantRunnerOptions{}).(*assistantRunner)
	if !ok || !runner.options.Unattended {
		t.Fatal("a review runner must be unattended")
	}
	b := runner.options.budget().trace()
	if b.MaxRounds != assistantUnattendedMaxToolRounds || b.ToolPhaseMs != assistantUnattendedToolPhaseTimeout.Milliseconds() || b.MaxStalledRounds != assistantMaxStalledRounds {
		t.Fatalf("trace budget = %+v", b)
	}
	if a := (assistantRunnerOptions{}).budget().trace(); a != currentAssistantTraceBudget() {
		t.Fatalf("attended budget = %+v", a)
	}
}

// toolCallsForTest reads an answer's calls the way the feedback listing does:
// through the run that answered it.
func toolCallsForTest(t *testing.T, store *assistantStore, messageID string) []assistantToolCall {
	t.Helper()
	rows, err := store.db.Query(`SELECT `+assistantCallColumns+` FROM assistant_run_calls
		WHERE run_id IN (SELECT id FROM assistant_runs WHERE answer_message_id = ?) ORDER BY round, seq`, messageID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := []assistantToolCall{}
	for rows.Next() {
		c, err := scanAssistantCall(rows)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}
