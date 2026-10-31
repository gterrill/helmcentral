package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Fourth review of the run budget (ADR 0240).

func TestAssistantRunReview4_TheReviewDeadlineStartsAfterTheInputIsBuilt(t *testing.T) {
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

func TestAssistantRunReview4_AToolThatIgnoresItsContextCannotHoldThePastTheToolPhase(t *testing.T) {
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

func TestAssistantRunReview4_ASubmitThatFailedGetsOneMoreTurn(t *testing.T) {
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

func TestAssistantRunReview4_ASubmitThatFailsTwiceFailsTheRunWithItsError(t *testing.T) {
	doer := stalledThenSubmits(t, 2)
	emit, _ := recordingEmitter()
	runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: &failingSubmitTools{fail: 2}, emit: emit, options: assistantRunnerOptions{Unattended: true, SubmitTool: "submit_test"}}
	_, err := runner.run(context.Background(), "system", "", nil)
	if err == nil || !strings.Contains(err.Error(), "review is empty") {
		t.Fatalf("err = %v, want the submit error", err)
	}
}

func TestAssistantRunReview4_APerReplyTimeoutIsNotReportedAsTheReviewsDeadline(t *testing.T) {
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

func TestAssistantRunReview4_ACorrectionDoesNotUseUpALookupRound(t *testing.T) {
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

func TestAssistantRunReview4_AFailedRunRecordsTheModelThatAnsweredItsRounds(t *testing.T) {
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
