package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

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

func TestAssistantRunReview3_AStalledUnattendedReviewCanStillFileItsReview(t *testing.T) {
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

func TestAssistantRunReview3_TheAlarmReviewDeadlineLeavesRoomForTheFinalReserve(t *testing.T) {
	b := assistantRunnerOptions{Unattended: true}.budget()
	if got := newAlarmReviewer("x").deadline; got < b.toolPhase+b.finalReserve {
		t.Fatalf("deadline %s ends before the tool phase %s and its final reserve %s", got, b.toolPhase, b.finalReserve)
	}
}

func TestAssistantRunReview3_ATimeCutIsATimeCeilingWhateverElseIsTrue(t *testing.T) {
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

func TestAssistantRunReview3_SearchSpendIsInTheRoundsSoTheyAddUp(t *testing.T) {
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

func TestAssistantRunReview3_RoundsSayWhatTheyWere(t *testing.T) {
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
