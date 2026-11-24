package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The Thinking setting: Mate's chat and its unattended reviews send
// OpenRouter's reasoning effort on every round; Model default sends nothing.

func TestAssistantRunner_ReasoningEffortGoesOnEveryRound(t *testing.T) {
	round0 := chatResponse(t, http.StatusOK, openRouterChatResponse{
		Model: "m",
		Choices: []openRouterChoice{{Message: openRouterMessage{Role: "assistant", ToolCalls: []openRouterToolCall{
			{ID: "call_1", Type: "function", Function: openRouterToolCallFunction{Name: "find_places", Arguments: openRouterArguments(`{"query":"x"}`)}},
		}}}},
		Usage: usage(1, 1, 0),
	})
	round1 := finalResponse(t, "done", "m", usage(1, 1, 0))
	doer := &queuedChatDoer{responses: []*http.Response{round0, round1}, errs: []error{nil, nil}}
	tools := &fakeToolExecutor{results: map[string]string{"find_places": `{}`}}
	emit, _ := recordingEmitter()
	runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: tools, emit: emit, options: assistantRunnerOptions{ReasoningEffort: "high"}}
	if _, err := runner.run(context.Background(), "system prompt", "", nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(doer.rawBodies) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(doer.rawBodies))
	}
	for i, body := range doer.rawBodies {
		if !strings.Contains(string(body), `"reasoning":{"effort":"high"}`) {
			t.Errorf("round %d lacks the reasoning effort: %s", i, body)
		}
	}
}

func TestAssistantRunner_ModelDefaultSendsNoReasoningKey(t *testing.T) {
	doer := &queuedChatDoer{responses: []*http.Response{finalResponse(t, "done", "m", usage(1, 1, 0))}, errs: []error{nil}}
	emit, _ := recordingEmitter()
	runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: &fakeToolExecutor{}, emit: emit}
	if _, err := runner.run(context.Background(), "system prompt", "", nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(string(doer.rawBodies[0]), "reasoning") {
		t.Fatalf("Model default must send no reasoning key: %s", doer.rawBodies[0])
	}
}

func writeReasoningSettingsFixture(t *testing.T, effort string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.yaml")
	body := "assistant:\n    enabled: true\n    model: \"test/model\"\n    notes: \"\"\n    reasoning_effort: \"" + effort + "\"\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write settings fixture: %v", err)
	}
	return path
}

func TestWatchReview_RunnerOptionsCarryTheThinkingSetting(t *testing.T) {
	resetGNSSForTest(t)
	path := writeReasoningSettingsFixture(t, "medium")
	var got assistantRunnerOptions
	prev := watchReviewRunnerFor
	watchReviewRunnerFor = func(_, _ string, tools assistantToolExecutor, options assistantRunnerOptions) assistantRunnerFace {
		got = options
		return &fakeReviewRunner{tools: tools, script: func(context.Context, assistantToolExecutor) (assistantReply, error) {
			return assistantReply{}, context.Canceled
		}}
	}
	t.Cleanup(func() { watchReviewRunnerFor = prev })
	cfg := watchConfig{SettingsPath: path, Readiness: assistantReadiness{Enabled: true, Configured: true, Model: "test/model"}, APIKey: "k"}
	w, report, findings := reviewInputs()
	runWatchReview(context.Background(), cfg, w, report, findings, watchTestTime(29, 6, 0), time.UTC)
	if got.ReasoningEffort != "medium" {
		t.Fatalf("the review runner got effort %q, want medium", got.ReasoningEffort)
	}
}

func TestAlarmReview_RunnerOptionsCarryTheThinkingSetting(t *testing.T) {
	h := newAlarmReviewHarness(t, true)
	path := writeReasoningSettingsFixture(t, "medium")
	h.r.config = func() (watchConfig, error) {
		return watchConfig{SettingsPath: path, Readiness: assistantReadiness{Enabled: true, Configured: true, Model: "test/model"}, APIKey: "k"}, nil
	}
	var got assistantRunnerOptions
	prev := watchReviewRunnerFor
	watchReviewRunnerFor = func(_, _ string, tools assistantToolExecutor, options assistantRunnerOptions) assistantRunnerFace {
		got = options
		return &fakeReviewRunner{tools: tools, script: func(context.Context, assistantToolExecutor) (assistantReply, error) {
			return assistantReply{}, context.Canceled
		}}
	}
	t.Cleanup(func() { watchReviewRunnerFor = prev })
	h.r.onRaise(h.raise(), h.now)
	h.r.wait()
	if got.ReasoningEffort != "medium" {
		t.Fatalf("the alarm review runner got effort %q, want medium", got.ReasoningEffort)
	}
}

func TestAssistantRunner_ForcedFinalRoundStillSendsTheReasoningEffort(t *testing.T) {
	responses := make([]*http.Response, 0, stalledToolRounds+1)
	errs := make([]error, 0, stalledToolRounds+1)
	for i := 0; i < stalledToolRounds; i++ {
		responses = append(responses, toolCallResponse(t, fmt.Sprintf("call_%d", i), "get_tides", `{"lat":1,"lon":2}`, openRouterUsage{}))
		errs = append(errs, nil)
	}
	responses = append(responses, finalResponse(t, "forced final answer", "m", openRouterUsage{}))
	errs = append(errs, nil)
	doer := &queuedChatDoer{responses: responses, errs: errs}
	tools := &fakeToolExecutor{results: map[string]string{"get_tides": `{"height":1.2}`}}
	emit, _ := recordingEmitter()
	runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: tools, emit: emit, options: assistantRunnerOptions{ReasoningEffort: "high"}}
	history := []openRouterMessage{{Role: "user", Content: openRouterContent("Why did the depth stop?")}}
	if _, err := runner.run(context.Background(), "system", "", history); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(doer.rawBodies) != stalledToolRounds+1 {
		t.Fatalf("expected %d requests, got %d", stalledToolRounds+1, len(doer.rawBodies))
	}
	forced := doer.requests[stalledToolRounds]
	if len(forced.Tools) != 0 {
		t.Fatalf("the last round was not the forced final one: %d tools", len(forced.Tools))
	}
	if !strings.Contains(string(doer.rawBodies[stalledToolRounds]), `"reasoning":{"effort":"high"}`) {
		t.Errorf("the forced final round lacks the reasoning effort: %s", doer.rawBodies[stalledToolRounds])
	}
}

func TestAssistantRunner_RefusesAnUnknownThinkingSettingBeforeSending(t *testing.T) {
	doer := &queuedChatDoer{responses: []*http.Response{finalResponse(t, "done", "m", usage(1, 1, 0))}, errs: []error{nil}}
	emit, _ := recordingEmitter()
	runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: &fakeToolExecutor{}, emit: emit, options: assistantRunnerOptions{ReasoningEffort: "HIGH"}}
	_, err := runner.run(context.Background(), "system prompt", "", nil)
	if err == nil {
		t.Fatal("a bad stored Thinking value must fail the run")
	}
	for _, want := range []string{`"HIGH"`, "Model default, low, medium or high", "settings.yaml", "Settings"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	if len(doer.rawBodies) != 0 {
		t.Fatalf("nothing may be sent with a bad Thinking value, got %d requests", len(doer.rawBodies))
	}
}

// The summary note and the end-of-watch handoff read their routing through
// assistantTurnParamsFromSettings. A bad stored Thinking value must not stop
// them there; only a Mate run that uses it fails. The summary note builds its
// own request with no reasoning key.
func TestChatTurnParamsDoNotRefuseAnUnknownThinkingSetting(t *testing.T) {
	path := writeReasoningSettingsFixture(t, "HIGH")
	params, err := assistantTurnParamsFromSettings(path, assistantReadiness{Model: "test/model"}, "k")
	if err != nil {
		t.Fatalf("settings read must not fail on a bad Thinking value: %v", err)
	}
	if params.options.ReasoningEffort != "HIGH" {
		t.Fatalf("the stored value is passed on as written, got %q", params.options.ReasoningEffort)
	}
}

func TestChatTurnParamsCarryTheThinkingSetting(t *testing.T) {
	path := writeReasoningSettingsFixture(t, "low")
	params, err := assistantTurnParamsFromSettings(path, assistantReadiness{Model: "test/model"}, "k")
	if err != nil {
		t.Fatalf("params: %v", err)
	}
	if params.options.ReasoningEffort != "low" {
		t.Fatalf("chat options carry %q, want low", params.options.ReasoningEffort)
	}
}

// A hand-edited non-string value (YAML reads `1` or `true` as a number or
// bool) must reach the run's check, not read as Model default.
func TestSettingsPayloadKeepsANonStringReasoningEffortInvalid(t *testing.T) {
	for _, raw := range []any{1, true, 2.5} {
		got := buildSettingsPayload(map[string]any{"assistant": map[string]any{"reasoning_effort": raw}}).Assistant.ReasoningEffort
		if validReasoningEffort(got) {
			t.Errorf("reasoning_effort %v (%T) read as valid %q; it must stay invalid so the run refuses it", raw, raw, got)
		}
	}
}

func TestSettingsPayloadRoundTripsReasoningEffort(t *testing.T) {
	if got := buildSettingsPayload(map[string]any{}).Assistant.ReasoningEffort; got != "" {
		t.Fatalf("absent setting must read as Model default, got %q", got)
	}
	settingsPath := writeTestSettings(t, "203.0.113.1", 3000)
	code, body := postSettings(t, settingsPath, func(p *settingsPayload) { p.Assistant.ReasoningEffort = "high" })
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body %v)", code, body)
	}
	saved, err := readSettings(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if got := buildSettingsPayload(saved).Assistant.ReasoningEffort; got != "high" {
		t.Fatalf("round-trip gave %q, want high", got)
	}
	code, _ = postSettings(t, settingsPath, func(p *settingsPayload) { p.Assistant.ReasoningEffort = "" })
	if code != http.StatusOK {
		t.Fatalf("clearing to Model default: got %d", code)
	}
	cleared, err := readSettings(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	assistantBlock, _ := cleared["assistant"].(map[string]any)
	if _, present := assistantBlock["reasoning_effort"]; present {
		t.Fatalf("Model default must leave reasoning_effort out of settings.yaml, got %v", assistantBlock)
	}
	if got := buildSettingsPayload(cleared).Assistant.ReasoningEffort; got != "" {
		t.Fatalf("settings report %q after clearing, want blank", got)
	}
}

func TestSettingsRejectsAnUnknownReasoningEffort(t *testing.T) {
	settingsPath := writeTestSettings(t, "203.0.113.1", 3000)
	for _, bad := range []string{"extreme", "HIGH", "minimal"} {
		code, body := postSettings(t, settingsPath, func(p *settingsPayload) { p.Assistant.ReasoningEffort = bad })
		if code != http.StatusBadRequest {
			t.Errorf("%q: expected 400, got %d (body %v)", bad, code, body)
			continue
		}
		if msg, _ := body["error"].(string); !strings.Contains(msg, "Thinking must be Model default, Low, Medium or High") {
			t.Errorf("%q: error %q lacks the operator wording", bad, msg)
		}
	}
}
