package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Cases found reviewing the run budget (ADR 0240).

func TestAssistantRun_APanicRecordsErrorNotAnswered(t *testing.T) {
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

func TestAssistantRun_CallsRefusedForTheWidthCapAreNotSeen(t *testing.T) {
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

func TestAssistantRun_TheRunCeilingHoldsWhenAStreamingRoundRunsOn(t *testing.T) {
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

func TestAssistantRun_ACompletionTimeoutIsAnErrorNotATimeCeiling(t *testing.T) {
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

func TestAssistantRun_CallsThatFinishedWhenThePhaseEndsReachTheFinalAnswer(t *testing.T) {
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

func TestAssistantRun_ARatingNeedsNoRunRowUnlessItWasPruned(t *testing.T) {
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

func TestAssistantRun_TheOrphanedToolCallTableIsDroppedOnOpen(t *testing.T) {
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

// Second review of the run budget (ADR 0240).

func TestAssistantRun_LegacyToolCallsMoveToRunsBeforeTheTableGoes(t *testing.T) {
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

func TestAssistantRun_OnlyAToolPhaseDeadlineCountsAsToolPhaseOver(t *testing.T) {
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

func TestAssistantRun_AnAnswerThatCouldNotBeStoredIsNotARunThatAnswered(t *testing.T) {
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

func TestAssistantRun_ATurnStoresAtMostOneRun(t *testing.T) {
	store, conv := readyAssistant(t)
	swapAssistantRunner(t, func(apiKey, model string, tools assistantToolDeps, options assistantRunnerOptions, emit assistantEmitter) assistantRunnerFace {
		return panickingAssistantRunner{}
	})
	postQuestion(t, conv.ID)
	if runs, _ := store.ListRuns(conv.ID); len(runs) != 1 || runs[0].StopReason != assistantStopError {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestAssistantRun_PruningAndRatingAgreeOnTheAnswersAge(t *testing.T) {
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

func TestAssistantRun_TheFollowUpWaitFollowsTheBudgetAtUseTime(t *testing.T) {
	shortBudget(t, 10*time.Millisecond, 10*time.Millisecond)
	orig := assistantWatchFollowUpGrace
	assistantWatchFollowUpGrace = 0
	t.Cleanup(func() { assistantWatchFollowUpGrace = orig })
	if got := assistantWatchFollowUpWait(); got != 3*30*time.Millisecond {
		t.Fatalf("wait = %s", got)
	}
}

func TestAssistantRun_AnUnattendedRunStopsAtItsOwnCeiling(t *testing.T) {
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

func TestAssistantRun_ReviewRunnersAreUnattendedAndTheirBudgetIsInTheTrace(t *testing.T) {
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

// Third review of the run budget (ADR 0240).

// submitTools is a tool executor that offers one submit tool.
type submitTools struct {
	fakeToolExecutor
	submitted int
}

func (s *submitTools) definitions() []openRouterTool {
	return append(s.fakeToolExecutor.definitions(), openRouterTool{Type: "function", Function: openRouterFunctionDef{Name: "submit_test", Description: "file the review", Parameters: json.RawMessage(`{"type":"object"}`)}})
}

func (s *submitTools) execute(ctx context.Context, name string, args json.RawMessage) (string, assistantToolOutcome, error) {
	if name == "submit_test" {
		s.submitted++
		return `{"ok":true}`, assistantToolOutcome{}, nil
	}
	return s.fakeToolExecutor.execute(ctx, name, args)
}

func TestAssistantRun_AStalledUnattendedReviewCanStillFileItsReview(t *testing.T) {
	var responses []*http.Response
	for i := 0; i < stalledToolRounds; i++ {
		responses = append(responses, toolCallResponse(t, "c", "get_tides", `{"lat":1,"lon":2}`, openRouterUsage{}))
	}
	responses = append(responses, toolCallResponse(t, "s", "submit_test", `{}`, openRouterUsage{}))
	doer := queued(responses...)
	emit, _ := recordingEmitter()
	tools := &submitTools{}
	runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: tools, emit: emit,
		options: assistantRunnerOptions{Unattended: true, SubmitTool: "submit_test"}}
	if _, err := runner.run(context.Background(), "system", "", nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if tools.submitted != 1 {
		t.Fatalf("the review was filed %d times, want once", tools.submitted)
	}
	last := doer.requests[len(doer.requests)-1]
	if len(last.Tools) != 1 || last.Tools[0].Function.Name != "submit_test" {
		t.Fatalf("the forced final should offer the submit tool and nothing else: %+v", last.Tools)
	}
}

func TestAssistantRun_TheAlarmReviewDeadlineLeavesRoomForTheFinalReserve(t *testing.T) {
	b := assistantRunnerOptions{Unattended: true}.budget()
	if got := newAlarmReviewer("x").deadline; got < b.toolPhase+b.finalReserve {
		t.Fatalf("deadline %s ends before the tool phase %s and its final reserve %s", got, b.toolPhase, b.finalReserve)
	}
}

func TestAssistantRun_ATimeCutIsATimeCeilingWhateverElseIsTrue(t *testing.T) {
	orig := assistantUnattendedToolPhaseTimeout
	assistantUnattendedToolPhaseTimeout = 250 * time.Millisecond
	t.Cleanup(func() { assistantUnattendedToolPhaseTimeout = orig })
	var responses []*http.Response
	for i := 0; i < assistantUnattendedMaxToolRounds-1; i++ {
		responses = append(responses, newCallRound(t, i))
	}
	// The last round the ceiling allows hangs until the tool phase ends.
	responses = append(responses, toolCallResponse(t, "w", "get_wind_forecast", `{"lat":1}`, openRouterUsage{}), finalResponse(t, "done", "m", openRouterUsage{}))
	doer := queued(responses...)
	emit, _ := recordingEmitter()
	rec := &assistantRunRecorder{}
	tools := &orderedDelayToolExecutor{delays: map[string]time.Duration{"get_wind_forecast": time.Minute}}
	runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: tools, emit: emit, options: assistantRunnerOptions{Unattended: true}}
	if _, err := runner.run(withAssistantRunRecorder(context.Background(), rec), "system", "", nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, stop := rec.result(); stop != assistantStopTimeCeiling {
		t.Fatalf("stop reason = %q, want %q", stop, assistantStopTimeCeiling)
	}
}

func TestAssistantRun_SearchSpendIsInTheRoundsSoTheyAddUp(t *testing.T) {
	round0 := chatResponse(t, http.StatusOK, openRouterChatResponse{
		Model: "m",
		Choices: []openRouterChoice{{Message: openRouterMessage{Role: "assistant", ToolCalls: []openRouterToolCall{
			{ID: "a", Type: "function", Function: openRouterToolCallFunction{Name: "get_tides", Arguments: openRouterArguments(`{"lat":1}`)}},
		}}}},
		Usage: usage(10, 5, 0.01),
	})
	final := finalResponse(t, "done", "m", usage(20, 5, 0.02))
	tools := &searchSpendingTools{usage: openRouterUsage{PromptTokens: 100, CompletionTokens: 7, Cost: 0.05}}
	emit, _ := recordingEmitter()
	rec := &assistantRunRecorder{}
	runner := &assistantRunner{doer: queued(round0, final), apiKey: "k", model: "m", tools: tools, emit: emit}
	reply, err := runner.run(withAssistantRunRecorder(context.Background(), rec), "system", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	rounds, _ := rec.result()
	var cost float64
	var prompt int
	for _, r := range rounds {
		cost += r.CostUSD
		prompt += r.PromptTokens
	}
	if d := cost - reply.CostUSD; d > 1e-9 || d < -1e-9 || prompt != reply.PromptTokens {
		t.Fatalf("rounds add up to $%.4f and %d prompt tokens, the answer says $%.4f and %d", cost, prompt, reply.CostUSD, reply.PromptTokens)
	}
}

func TestAssistantRun_RoundsSayWhatTheyWere(t *testing.T) {
	doer := queued(newCallRound(t, 0), finalResponse(t, "done", "m", openRouterUsage{}))
	emit, _ := recordingEmitter()
	rec := &assistantRunRecorder{}
	runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: &fakeToolExecutor{}, emit: emit}
	if _, err := runner.run(withAssistantRunRecorder(context.Background(), rec), "system", "", nil); err != nil {
		t.Fatal(err)
	}
	rounds, _ := rec.result()
	if len(rounds) != 2 || rounds[0].Kind != assistantRoundLookups || rounds[1].Kind != assistantRoundAnswer {
		t.Fatalf("kinds = %+v", rounds)
	}

	var responses []*http.Response
	for i := 0; i < stalledToolRounds; i++ {
		responses = append(responses, toolCallResponse(t, "c", "get_tides", `{"lat":1,"lon":2}`, openRouterUsage{}))
	}
	responses = append(responses, finalResponse(t, "done", "m", openRouterUsage{}))
	rec = &assistantRunRecorder{}
	runner = &assistantRunner{doer: queued(responses...), apiKey: "k", model: "m", tools: &fakeToolExecutor{}, emit: emit}
	if _, err := runner.run(withAssistantRunRecorder(context.Background(), rec), "system", "", nil); err != nil {
		t.Fatal(err)
	}
	rounds, _ = rec.result()
	if rounds[len(rounds)-1].Kind != assistantRoundFinal || rounds[0].Kind != assistantRoundLookups {
		t.Fatalf("kinds = %+v", rounds)
	}
}

// Fourth review of the run budget (ADR 0240).

func TestAssistantRun_TheReviewDeadlineStartsAfterTheInputIsBuilt(t *testing.T) {
	var left time.Duration
	withReviewRunner(t, func(ctx context.Context, tools assistantToolExecutor) (assistantReply, error) {
		if dl, ok := ctx.Deadline(); ok {
			left = time.Until(dl)
		}
		return assistantReply{}, nil
	})
	runUnattendedReview(context.Background(), unattendedTurn{
		Cfg: reviewConfig(t), Now: time.Now(), Deadline: 600 * time.Millisecond, ConversationID: "c", LogPrefix: "test", Name: "n",
		Build: func(ctx context.Context, deps assistantToolDeps, units string, pc assistantPromptContext) (unattendedInput, string) {
			time.Sleep(300 * time.Millisecond)
			return unattendedInput{System: "s", Message: "m", Tools: &fakeToolExecutor{}, Submitted: func() bool { return true }}, ""
		},
	})
	if left < 500*time.Millisecond {
		t.Fatalf("the run had %s left: building its input used some of its ceiling", left)
	}
}

// ignoringTools is a tool whose work does not stop when it is told to.
type ignoringTools struct{ fakeToolExecutor }

func (*ignoringTools) execute(ctx context.Context, name string, args json.RawMessage) (string, assistantToolOutcome, error) {
	if name == "get_wind_forecast" {
		time.Sleep(2 * time.Second)
	}
	return "{}", assistantToolOutcome{}, nil
}

func TestAssistantRun_AToolThatIgnoresItsContextCannotHoldThePastTheToolPhase(t *testing.T) {
	shortBudget(t, 100*time.Millisecond, 5*time.Second)
	doer := queued(
		toolCallResponse(t, "w", "get_wind_forecast", `{"lat":1}`, openRouterUsage{}),
		finalResponse(t, "wrapped up", "m", openRouterUsage{}),
	)
	emit, _ := recordingEmitter()
	runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: &ignoringTools{}, emit: emit}
	start := time.Now()
	reply, err := runner.run(context.Background(), "system", "", nil)
	if err != nil || reply.Content != "wrapped up" {
		t.Fatalf("reply %q err %v", reply.Content, err)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("took %s: the run waited for a tool that ignored the end of the tool phase", took)
	}
}

// failingSubmitTools offers a submit tool that fails its first `fail` calls.
type failingSubmitTools struct {
	submitTools
	fail, calls int
}

func (f *failingSubmitTools) execute(ctx context.Context, name string, args json.RawMessage) (string, assistantToolOutcome, error) {
	if name == "submit_test" {
		f.calls++
		if f.calls <= f.fail {
			return "", assistantToolOutcome{}, errors.New("review is empty; say what you see")
		}
	}
	return f.submitTools.execute(ctx, name, args)
}

func stalledThenSubmits(t *testing.T, submits int) *queuedChatDoer {
	var responses []*http.Response
	for i := 0; i < stalledToolRounds; i++ {
		responses = append(responses, toolCallResponse(t, "c", "get_tides", `{"lat":1,"lon":2}`, openRouterUsage{}))
	}
	for i := 0; i < submits; i++ {
		responses = append(responses, toolCallResponse(t, fmt.Sprintf("s%d", i), "submit_test", `{}`, openRouterUsage{}))
	}
	return queued(responses...)
}

func TestAssistantRun_ASubmitThatFailedGetsOneMoreTurn(t *testing.T) {
	doer := stalledThenSubmits(t, 2)
	emit, _ := recordingEmitter()
	tools := &failingSubmitTools{fail: 1}
	runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: tools, emit: emit, options: assistantRunnerOptions{Unattended: true, SubmitTool: "submit_test"}}
	if _, err := runner.run(context.Background(), "system", "", nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if tools.submitted != 1 || tools.calls != 2 {
		t.Fatalf("filed %d times in %d tries, want filed once on the second try", tools.submitted, tools.calls)
	}
	last := doer.requests[len(doer.requests)-1]
	if len(last.Tools) != 1 || last.Tools[0].Function.Name != "submit_test" || !strings.Contains(fmt.Sprint(last.Messages), "review is empty") {
		t.Fatalf("the retry should offer only the submit tool and carry its error: %+v", last)
	}
}

func TestAssistantRun_ASubmitThatFailsTwiceFailsTheRunWithItsError(t *testing.T) {
	doer := stalledThenSubmits(t, 2)
	emit, _ := recordingEmitter()
	runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: &failingSubmitTools{fail: 2}, emit: emit, options: assistantRunnerOptions{Unattended: true, SubmitTool: "submit_test"}}
	_, err := runner.run(context.Background(), "system", "", nil)
	if err == nil || !strings.Contains(err.Error(), "review is empty") {
		t.Fatalf("err = %v, want the submit error", err)
	}
}

func TestAssistantRun_APerReplyTimeoutIsNotReportedAsTheReviewsDeadline(t *testing.T) {
	withReviewRunner(t, func(ctx context.Context, tools assistantToolExecutor) (assistantReply, error) {
		return assistantReply{}, fmt.Errorf("a reply from the model took too long: %w", context.DeadlineExceeded)
	})
	out := runUnattendedReview(context.Background(), unattendedTurn{
		Cfg: reviewConfig(t), Now: time.Now(), Deadline: time.Hour, ConversationID: "c", LogPrefix: "test", Name: "n",
		Build: func(ctx context.Context, deps assistantToolDeps, units string, pc assistantPromptContext) (unattendedInput, string) {
			return unattendedInput{System: "s", Message: "m", Tools: &fakeToolExecutor{}, Submitted: func() bool { return false }}, ""
		},
	})
	if !strings.Contains(out.Problem, "a reply from the model took too long") || strings.Contains(out.Problem, "ran past its deadline") {
		t.Fatalf("problem = %q", out.Problem)
	}
}

// draftingTools sends the first draft back once.
type draftingTools struct{ fakeToolExecutor }

func (*draftingTools) checkDraft(draft string, calls []openRouterToolCall) (string, string) {
	if draft == "first draft" {
		return "wind claim", "Check the wind first."
	}
	return "", ""
}

func TestAssistantRun_ACorrectionDoesNotUseUpALookupRound(t *testing.T) {
	var responses []*http.Response
	for i := 0; i < 6; i++ {
		responses = append(responses, newCallRound(t, i))
	}
	responses = append(responses, finalResponse(t, "first draft", "m", openRouterUsage{}), newCallRound(t, 6), newCallRound(t, 7), finalResponse(t, "done", "m", openRouterUsage{}))
	doer := queued(responses...)
	emit, _ := recordingEmitter()
	runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: &draftingTools{}, emit: emit, options: assistantRunnerOptions{Unattended: true}}
	if _, err := runner.run(context.Background(), "system", "", nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	// 6 lookups, the correction, 2 more lookups (8 in all), then the forced final.
	if len(doer.requests) != 10 {
		t.Fatalf("%d requests, want 10", len(doer.requests))
	}
	for i := 0; i < 9; i++ {
		if len(doer.requests[i].Tools) == 0 {
			t.Fatalf("request %d withdrew the tools although fewer than %d lookup rounds had run", i, assistantUnattendedMaxToolRounds)
		}
	}
	if len(doer.requests[9].Tools) != 0 {
		t.Fatal("the request after the eighth lookup round should withdraw the tools")
	}
}

func TestAssistantRun_AFailedRunRecordsTheModelThatAnsweredItsRounds(t *testing.T) {
	store, conv := readyAssistant(t)
	swapAssistantRunner(t, func(apiKey, model string, tools assistantToolDeps, options assistantRunnerOptions, emit assistantEmitter) assistantRunnerFace {
		doer := &queuedChatDoer{responses: []*http.Response{newCallRound(t, 0), nil}, errs: []error{nil, errors.New("upstream is down")}}
		return &assistantRunner{doer: doer, apiKey: apiKey, model: model, tools: &fakeToolExecutor{}, emit: emit}
	})
	postQuestion(t, conv.ID)
	runs, _ := store.ListRuns(conv.ID)
	if len(runs) != 1 || runs[0].Model != "anthropic/claude-sonnet-4.5" {
		t.Fatalf("runs = %+v: the model that answered the first round was not the configured default", runs)
	}
}
