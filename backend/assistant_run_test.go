package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── fakes ───────────────────────────────────────────────────────────────

// queuedChatDoer is one captured request/response pair a queuedChatDoer
// replays: the decoded request body (for assertions on what the runner sent)
// paired with the canned response (or error) to hand back. rawBodies keeps
// the undecoded bytes too, for assertions that need to see the wire shape
// openRouterMessage.MarshalJSON actually produced (e.g. an Anthropic
// system message's content-blocks array) rather than what decoding it back
// through openRouterChatRequest's tolerant-but-lossy Content type would
// show.
type queuedChatDoer struct {
	responses []*http.Response
	errs      []error

	requests  []openRouterChatRequest
	rawBodies [][]byte
}

func (q *queuedChatDoer) Do(req *http.Request) (*http.Response, error) {
	data, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	q.rawBodies = append(q.rawBodies, data)

	var body openRouterChatRequest
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, fmt.Errorf("decode request body: %w", err)
	}
	q.requests = append(q.requests, body)

	i := len(q.requests) - 1
	if i >= len(q.responses) {
		return nil, fmt.Errorf("queuedChatDoer: no response queued for call %d", i)
	}
	return q.responses[i], q.errs[i]
}

// chatResponse builds an *http.Response carrying resp as its JSON body, the
// shape openRouterChatCompletion (openrouter_client.go) decodes.
func chatResponse(t *testing.T, status int, resp openRouterChatResponse) *http.Response {
	t.Helper()
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal chat response: %v", err)
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewReader(data)),
		Header:     make(http.Header),
	}
}

// toolCallResponse builds a response whose only choice asks for one tool
// call by id/name/args, with no text content - the shape a model emits
// while it is still gathering information.
func toolCallResponse(t *testing.T, id, name, args string, usage openRouterUsage) *http.Response {
	t.Helper()
	return chatResponse(t, http.StatusOK, openRouterChatResponse{
		Model: "anthropic/claude-sonnet-4.5",
		Choices: []openRouterChoice{{
			Message: openRouterMessage{
				Role: "assistant",
				ToolCalls: []openRouterToolCall{{
					ID:   id,
					Type: "function",
					Function: openRouterToolCallFunction{
						Name:      name,
						Arguments: openRouterArguments(args),
					},
				}},
			},
		}},
		Usage: usage,
	})
}

// finalResponse builds a response whose only choice is a plain-text answer
// with no tool calls - the shape that ends the loop.
func finalResponse(t *testing.T, content, model string, usage openRouterUsage) *http.Response {
	t.Helper()
	return chatResponse(t, http.StatusOK, openRouterChatResponse{
		Model: model,
		Choices: []openRouterChoice{{
			Message: openRouterMessage{Role: "assistant", Content: openRouterContent(content)},
		}},
		Usage: usage,
	})
}

// fakeToolCall records one call a fakeToolExecutor answered, for assertions
// on what the runner actually dispatched.
type fakeToolCall struct {
	name string
	args string
}

// fakeToolExecutor is a map-backed assistantToolExecutor test double so the
// runner's loop can be exercised without a real assistantToolDeps (no live
// SignalK, Overpass or weather/tide provider needed). Its execute is called
// from more than one goroutine at once now that one round's tool calls run
// concurrently (assistant_run.go's runToolRound), so calls/mu guards the
// only shared, mutable state here - results and errs are populated once
// before a test starts and never written again, so concurrent reads of
// those two maps need no lock.
type fakeToolExecutor struct {
	results map[string]string
	errs    map[string]error

	mu    sync.Mutex
	calls []fakeToolCall
}

func (f *fakeToolExecutor) execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, fakeToolCall{name: name, args: string(args)})
	f.mu.Unlock()

	if err, ok := f.errs[name]; ok {
		return "", err
	}
	if result, ok := f.results[name]; ok {
		return result, nil
	}
	return "{}", nil
}

// slowToolExecutor's execute sleeps for delay before returning a canned
// result, or returns ctx's error immediately if ctx is cancelled first.
// Used to prove tool calls in one round actually run concurrently
// (TestAssistantRunner_ToolCallsInOneRoundRunConcurrently) and that
// cancelling the run's context stops them promptly rather than waiting the
// sleep out (TestAssistantRunner_CancellingContextStopsInFlightTools).
type slowToolExecutor struct {
	delay time.Duration

	mu    sync.Mutex
	calls int
}

func (s *slowToolExecutor) execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	select {
	case <-time.After(s.delay):
	case <-ctx.Done():
		return "", ctx.Err()
	}
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return "{}", nil
}

// orderedDelayToolExecutor answers each named tool after that tool's own
// configured delay, so a test can make an earlier call in a round finish
// after a later one - proving the round's results still come back in call
// order rather than completion order.
type orderedDelayToolExecutor struct {
	delays  map[string]time.Duration
	results map[string]string
}

func (o *orderedDelayToolExecutor) execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if d, ok := o.delays[name]; ok {
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if r, ok := o.results[name]; ok {
		return r, nil
	}
	return "{}", nil
}

// recordedEvent is one call a recordingEmitter captured, with "text"
// extracted from the {"text": ...} payload shape every status event carries
// (assistant_run.go's assistantStatus).
type recordedEvent struct {
	event string
	text  string
}

func recordingEmitter() (assistantEmitter, *[]recordedEvent) {
	events := make([]recordedEvent, 0)
	emit := func(event string, payload any) {
		text := ""
		if m, ok := payload.(map[string]string); ok {
			text = m["text"]
		}
		events = append(events, recordedEvent{event: event, text: text})
	}
	return emit, &events
}

func usage(promptTokens, completionTokens int, cost float64) openRouterUsage {
	return openRouterUsage{PromptTokens: promptTokens, CompletionTokens: completionTokens, Cost: cost}
}

// ── tests ───────────────────────────────────────────────────────────────

func TestAssistantRunner_TwoToolCallsInOneTurnBothExecutedAndTokensSummed(t *testing.T) {
	round0 := chatResponse(t, http.StatusOK, openRouterChatResponse{
		Model: "anthropic/claude-sonnet-4.5",
		Choices: []openRouterChoice{{
			Message: openRouterMessage{
				Role: "assistant",
				ToolCalls: []openRouterToolCall{
					{ID: "call_1", Type: "function", Function: openRouterToolCallFunction{Name: "find_places", Arguments: openRouterArguments(`{"query":"Tongue Bay"}`)}},
					{ID: "call_2", Type: "function", Function: openRouterToolCallFunction{Name: "get_wind_forecast", Arguments: openRouterArguments(`{"lat":-20.1,"lon":149.1}`)}},
				},
			},
		}},
		Usage: usage(100, 20, 0.01),
	})
	round1 := finalResponse(t, "Tongue Bay looks better on the rising tide.", "anthropic/claude-sonnet-4.5", usage(150, 40, 0.02))

	doer := &queuedChatDoer{responses: []*http.Response{round0, round1}, errs: []error{nil, nil}}
	tools := &fakeToolExecutor{results: map[string]string{
		"find_places":       `{"results":[{"name":"Tongue Bay"}]}`,
		"get_wind_forecast": `{"days":[]}`,
	}}
	emit, _ := recordingEmitter()

	runner := &assistantRunner{doer: doer, apiKey: "test-key", model: "anthropic/claude-sonnet-4.5", tools: tools, emit: emit}
	reply, err := runner.run(context.Background(), "system prompt", "", nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if reply.Content != "Tongue Bay looks better on the rising tide." {
		t.Fatalf("unexpected content: %q", reply.Content)
	}
	if reply.Model != "anthropic/claude-sonnet-4.5" {
		t.Fatalf("unexpected model: %q", reply.Model)
	}
	if reply.PromptTokens != 250 || reply.CompletionTokens != 60 {
		t.Fatalf("expected tokens summed across both completions, got prompt=%d completion=%d", reply.PromptTokens, reply.CompletionTokens)
	}
	if got, want := reply.CostUSD, 0.03; got < want-1e-9 || got > want+1e-9 {
		t.Fatalf("expected cost summed to %v, got %v", want, got)
	}
	if reply.ToolRounds != 1 {
		t.Fatalf("expected ToolRounds=1, got %d", reply.ToolRounds)
	}

	// The two calls in this round now run concurrently (assistant_run.go's
	// runToolRound), so which one's execute() lands in tools.calls first is
	// not guaranteed - only that both ran, exactly once each.
	if len(tools.calls) != 2 {
		t.Fatalf("expected both tools executed, got %d calls: %+v", len(tools.calls), tools.calls)
	}
	gotNames := map[string]int{}
	for _, c := range tools.calls {
		gotNames[c.name]++
	}
	if gotNames["find_places"] != 1 || gotNames["get_wind_forecast"] != 1 {
		t.Fatalf("expected exactly one call each to find_places and get_wind_forecast, got %+v", tools.calls)
	}

	if len(doer.requests) != 2 {
		t.Fatalf("expected 2 completion requests, got %d", len(doer.requests))
	}
	second := doer.requests[1]
	// The assistant's tool_calls message from round 0 must be echoed back
	// verbatim as history, followed by one tool-role message per call with
	// the matching tool_call_id, in call order (see the order-specific test
	// below for a case where the calls finish out of order).
	var assistantMsg *openRouterMessage
	var toolMsgs []openRouterMessage
	for i := range second.Messages {
		m := second.Messages[i]
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			assistantMsg = &second.Messages[i]
		}
		if m.Role == "tool" {
			toolMsgs = append(toolMsgs, m)
		}
	}
	if assistantMsg == nil || len(assistantMsg.ToolCalls) != 2 {
		t.Fatalf("expected the second request to echo the assistant's tool_calls message, got %+v", second.Messages)
	}
	if len(toolMsgs) != 2 || toolMsgs[0].ToolCallID != "call_1" || toolMsgs[1].ToolCallID != "call_2" {
		t.Fatalf("expected tool messages call_1 then call_2 in order, got %+v", toolMsgs)
	}
	if string(toolMsgs[0].Content) != `{"results":[{"name":"Tongue Bay"}]}` {
		t.Fatalf("expected tool message for call_1 with matching content, got %+v", toolMsgs[0])
	}
	if string(toolMsgs[1].Content) != `{"days":[]}` {
		t.Fatalf("expected tool message for call_2 with matching content, got %+v", toolMsgs[1])
	}
}

// TestAssistantRunner_LogsToolCallBeforeAndAfter captures package log output
// (log.SetOutput to a buffer, restored on cleanup) around a run with one
// tool call, so a failed or slow lookup can be diagnosed from the server log
// after the fact rather than only from what the operator saw live in the
// SSE stream (see assistant_run.go's compactAssistantToolArgs and
// assistantFindPlacesLogSuffix).
func TestAssistantRunner_LogsToolCallBeforeAndAfter(t *testing.T) {
	var logBuf bytes.Buffer
	origOutput := log.Writer()
	origFlags := log.Flags()
	log.SetOutput(&logBuf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(origOutput)
		log.SetFlags(origFlags)
	})

	round0 := chatResponse(t, http.StatusOK, openRouterChatResponse{
		Choices: []openRouterChoice{{
			Message: openRouterMessage{
				Role: "assistant",
				ToolCalls: []openRouterToolCall{
					{ID: "call_1", Type: "function", Function: openRouterToolCallFunction{Name: "find_places", Arguments: openRouterArguments(`{"query":"Tongue Bay"}`)}},
				},
			},
		}},
	})
	round1 := finalResponse(t, "done", "m", openRouterUsage{})

	doer := &queuedChatDoer{responses: []*http.Response{round0, round1}, errs: []error{nil, nil}}
	tools := &fakeToolExecutor{results: map[string]string{
		"find_places": `{"search":"exact","results":[{"name":"Tongue Bay"}]}`,
	}}
	emit, _ := recordingEmitter()

	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: tools, emit: emit}
	if _, err := runner.run(context.Background(), "system", "", nil); err != nil {
		t.Fatalf("run: %v", err)
	}

	logged := logBuf.String()
	if !strings.Contains(logged, "assistant: tool find_places") {
		t.Fatalf("expected a log line for the tool call, got:\n%s", logged)
	}
	if !strings.Contains(logged, "-> ") {
		t.Fatalf("expected a log line reporting the result size/duration, got:\n%s", logged)
	}
}

func TestAssistantRunner_OpenRouterAutoAddsAutoRouterPlugin(t *testing.T) {
	round0 := finalResponse(t, "ok", "openrouter/auto", usage(10, 5, 0.001))
	doer := &queuedChatDoer{responses: []*http.Response{round0}, errs: []error{nil}}
	tools := &fakeToolExecutor{results: map[string]string{}}
	emit, _ := recordingEmitter()

	runner := &assistantRunner{
		doer:   doer,
		apiKey: "test-key",
		model:  "openrouter/auto",
		autoRouter: assistantAutoRouterOptions{
			AllowedModels:  []string{"anthropic/*"},
			ExcludedModels: []string{"openai/gpt-4o-mini"},
			CostTier:       "high",
		},
		tools: tools,
		emit:  emit,
	}

	if _, err := runner.run(context.Background(), "system prompt", "", nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(doer.requests) != 1 {
		t.Fatalf("expected one request, got %d", len(doer.requests))
	}
	if len(doer.requests[0].Plugins) != 1 {
		t.Fatalf("expected one plugin, got %+v", doer.requests[0].Plugins)
	}
	plugin := doer.requests[0].Plugins[0]
	if plugin.ID != "auto-router" {
		t.Fatalf("expected auto-router plugin id, got %q", plugin.ID)
	}
	if len(plugin.AllowedModels) != 1 || plugin.AllowedModels[0] != "anthropic/*" {
		t.Fatalf("unexpected allowed_models: %+v", plugin.AllowedModels)
	}
	if len(plugin.ExcludedModels) != 1 || plugin.ExcludedModels[0] != "openai/gpt-4o-mini" {
		t.Fatalf("unexpected excluded_models: %+v", plugin.ExcludedModels)
	}
	if plugin.CostTier != "high" {
		t.Fatalf("expected cost_tier=high, got %q", plugin.CostTier)
	}
}

func TestAssistantRunner_NonAutoModelOmitsAutoRouterPlugin(t *testing.T) {
	round0 := finalResponse(t, "ok", "anthropic/claude-sonnet-4.5", usage(10, 5, 0.001))
	doer := &queuedChatDoer{responses: []*http.Response{round0}, errs: []error{nil}}
	tools := &fakeToolExecutor{results: map[string]string{}}
	emit, _ := recordingEmitter()

	runner := &assistantRunner{
		doer:   doer,
		apiKey: "test-key",
		model:  "anthropic/claude-sonnet-4.5",
		autoRouter: assistantAutoRouterOptions{
			AllowedModels: []string{"anthropic/*"},
			CostTier:      "high",
		},
		tools: tools,
		emit:  emit,
	}

	if _, err := runner.run(context.Background(), "system prompt", "", nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(doer.requests) != 1 {
		t.Fatalf("expected one request, got %d", len(doer.requests))
	}
	if len(doer.requests[0].Plugins) != 0 {
		t.Fatalf("expected no plugins for non-auto model, got %+v", doer.requests[0].Plugins)
	}
}

func TestAssistantRunner_EmitsStatusEventsInOrder(t *testing.T) {
	round0 := chatResponse(t, http.StatusOK, openRouterChatResponse{
		Choices: []openRouterChoice{{
			Message: openRouterMessage{
				Role: "assistant",
				ToolCalls: []openRouterToolCall{
					{ID: "call_1", Type: "function", Function: openRouterToolCallFunction{Name: "find_places", Arguments: openRouterArguments(`{"query":"Tongue Bay"}`)}},
					{ID: "call_2", Type: "function", Function: openRouterToolCallFunction{Name: "get_wind_forecast", Arguments: openRouterArguments(`{"lat":1,"lon":2}`)}},
				},
			},
		}},
	})
	round1 := finalResponse(t, "done", "m", openRouterUsage{})

	doer := &queuedChatDoer{responses: []*http.Response{round0, round1}, errs: []error{nil, nil}}
	tools := &fakeToolExecutor{}
	emit, events := recordingEmitter()

	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: tools, emit: emit}
	if _, err := runner.run(context.Background(), "system", "", nil); err != nil {
		t.Fatalf("run: %v", err)
	}

	// The two calls run concurrently, but their "about to call" status
	// events are still emitted in call order from the outer sequential
	// loop (assistant_run.go's runToolRound), not from inside the
	// goroutines that do the actual work - so this stays deterministic.
	wantTexts := []string{
		"Thinking…",
		"Looking up Tongue Bay…",
		"Fetching wind forecast for 1.0000,2.0000…",
		"Working out the answer…",
	}
	if len(*events) != len(wantTexts) {
		t.Fatalf("expected %d events, got %d: %+v", len(wantTexts), len(*events), *events)
	}
	for i, want := range wantTexts {
		got := (*events)[i]
		if got.event != "status" || got.text != want {
			t.Fatalf("event %d: got {%q %q}, want status %q", i, got.event, got.text, want)
		}
	}
}

func TestAssistantRunner_ToolExecutorErrorBecomesErrorJSONAndContinues(t *testing.T) {
	round0 := chatResponse(t, http.StatusOK, openRouterChatResponse{
		Choices: []openRouterChoice{{
			Message: openRouterMessage{
				Role: "assistant",
				ToolCalls: []openRouterToolCall{
					{ID: "call_1", Type: "function", Function: openRouterToolCallFunction{Name: "get_tides", Arguments: openRouterArguments(`{"lat":1,"lon":2}`)}},
				},
			},
		}},
	})
	round1 := finalResponse(t, "tides unavailable, here is what I know", "m", openRouterUsage{})

	doer := &queuedChatDoer{responses: []*http.Response{round0, round1}, errs: []error{nil, nil}}
	tools := &fakeToolExecutor{errs: map[string]error{
		"get_tides": errors.New("tide station offline\nfull stack trace follows"),
	}}
	emit, events := recordingEmitter()

	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: tools, emit: emit}
	reply, err := runner.run(context.Background(), "system", "", nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if reply.Content != "tides unavailable, here is what I know" {
		t.Fatalf("unexpected content: %q", reply.Content)
	}

	second := doer.requests[1]
	var toolMsg *openRouterMessage
	for i := range second.Messages {
		if second.Messages[i].Role == "tool" {
			toolMsg = &second.Messages[i]
		}
	}
	if toolMsg == nil {
		t.Fatalf("expected a tool-role message in the second request, got %+v", second.Messages)
	}
	if string(toolMsg.Content) != `{"error":"tide station offline"}` {
		t.Fatalf("expected the tool error trimmed to one line, got %q", toolMsg.Content)
	}

	foundFailureStatus := false
	for _, e := range *events {
		if e.event == "status" && e.text == "get_tides failed: tide station offline" {
			foundFailureStatus = true
		}
	}
	if !foundFailureStatus {
		t.Fatalf("expected a status event reporting the tool failure, got %+v", *events)
	}
}

func TestAssistantRunner_ForcedFinalRoundSendsNoToolsAndToolChoiceNone(t *testing.T) {
	responses := make([]*http.Response, 0, assistantMaxToolRounds+1)
	errs := make([]error, 0, assistantMaxToolRounds+1)
	for i := 0; i < assistantMaxToolRounds; i++ {
		responses = append(responses, toolCallResponse(t, fmt.Sprintf("call_%d", i), "get_tides", `{"lat":1,"lon":2}`, openRouterUsage{}))
		errs = append(errs, nil)
	}
	responses = append(responses, finalResponse(t, "forced final answer", "m", openRouterUsage{}))
	errs = append(errs, nil)

	doer := &queuedChatDoer{responses: responses, errs: errs}
	tools := &fakeToolExecutor{}
	emit, _ := recordingEmitter()

	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: tools, emit: emit}
	reply, err := runner.run(context.Background(), "system", "", nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if reply.Content != "forced final answer" {
		t.Fatalf("unexpected content: %q", reply.Content)
	}
	if len(doer.requests) != assistantMaxToolRounds+1 {
		t.Fatalf("expected %d requests, got %d", assistantMaxToolRounds+1, len(doer.requests))
	}

	forced := doer.requests[assistantMaxToolRounds]
	if len(forced.Tools) != 0 {
		t.Fatalf("expected the forced final round to send no tools, got %d", len(forced.Tools))
	}
	if forced.ToolChoice != "none" {
		t.Fatalf("expected the forced final round's tool_choice to be %q, got %q", "none", forced.ToolChoice)
	}

	// Every earlier round must still have offered tools normally.
	for i := 0; i < assistantMaxToolRounds; i++ {
		if len(doer.requests[i].Tools) == 0 {
			t.Fatalf("expected round %d to offer tools", i)
		}
		if doer.requests[i].ToolChoice == "none" {
			t.Fatalf("round %d should not have forced tool_choice none", i)
		}
	}
}

func TestAssistantRunner_ForcedFinalRoundStillReturningToolCallsErrors(t *testing.T) {
	responses := make([]*http.Response, 0, assistantMaxToolRounds+1)
	errs := make([]error, 0, assistantMaxToolRounds+1)
	for i := 0; i < assistantMaxToolRounds; i++ {
		responses = append(responses, toolCallResponse(t, fmt.Sprintf("call_%d", i), "get_tides", `{"lat":1,"lon":2}`, openRouterUsage{}))
		errs = append(errs, nil)
	}
	// Even on the forced round, the model insists on calling a tool.
	responses = append(responses, toolCallResponse(t, "call_final", "get_tides", `{"lat":1,"lon":2}`, openRouterUsage{}))
	errs = append(errs, nil)

	doer := &queuedChatDoer{responses: responses, errs: errs}
	tools := &fakeToolExecutor{}
	emit, _ := recordingEmitter()

	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: tools, emit: emit}
	_, err := runner.run(context.Background(), "system", "", nil)
	if err == nil {
		t.Fatal("expected an error when the model keeps calling tools past the forced final round")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("%d tool rounds", assistantMaxToolRounds)) {
		t.Fatalf("expected the error to name the round cap, got %q", err)
	}
}

func TestAssistantRunner_UpstreamErrorReturnsWithoutSucceedingAndEmitsNoMessage(t *testing.T) {
	doer := &queuedChatDoer{
		responses: []*http.Response{chatResponse(t, http.StatusUnauthorized, openRouterChatResponse{
			Error: &openRouterAPIError{Message: "invalid api key"},
		})},
		errs: []error{nil},
	}
	tools := &fakeToolExecutor{}
	emit, events := recordingEmitter()

	runner := &assistantRunner{doer: doer, apiKey: "bad-key", model: "m", tools: tools, emit: emit}
	_, err := runner.run(context.Background(), "system", "", nil)
	if err == nil {
		t.Fatal("expected an upstream error to propagate")
	}
	for _, e := range *events {
		if e.event == "message" {
			t.Fatalf("expected no message event on an upstream error, got %+v", *events)
		}
	}
}

func TestAssistantRunner_ToolCallWithoutIDErrors(t *testing.T) {
	round0 := chatResponse(t, http.StatusOK, openRouterChatResponse{
		Choices: []openRouterChoice{{
			Message: openRouterMessage{
				Role: "assistant",
				ToolCalls: []openRouterToolCall{
					{ID: "", Type: "function", Function: openRouterToolCallFunction{Name: "find_places", Arguments: openRouterArguments(`{"query":"Bay"}`)}},
				},
			},
		}},
	})
	doer := &queuedChatDoer{responses: []*http.Response{round0}, errs: []error{nil}}
	tools := &fakeToolExecutor{}
	emit, _ := recordingEmitter()

	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: tools, emit: emit}
	_, err := runner.run(context.Background(), "system", "", nil)
	if err == nil {
		t.Fatal("expected an error for a tool call with no id")
	}
	if len(tools.calls) != 0 {
		t.Fatalf("expected no tool to be executed when the call has no id, got %+v", tools.calls)
	}
}

func TestAssistantHistoryMessages_ConvertsUserAndAssistantRows(t *testing.T) {
	msgs := []assistantMessage{
		{Role: "user", Content: "Tongue Bay or Blue Pearl Bay first?"},
		{Role: "assistant", Content: "Tongue Bay first, on the rising tide."},
	}
	got := assistantHistoryMessages(msgs)
	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got))
	}
	if got[0].Role != "user" || string(got[0].Content) != msgs[0].Content {
		t.Fatalf("unexpected first message: %+v", got[0])
	}
	if got[1].Role != "assistant" || string(got[1].Content) != msgs[1].Content {
		t.Fatalf("unexpected second message: %+v", got[1])
	}
}

// ── concurrent tool rounds (backend perf audit, Tier 3 item 2) ─────────

func TestAssistantRunner_ToolCallsInOneRoundRunConcurrently(t *testing.T) {
	round0 := chatResponse(t, http.StatusOK, openRouterChatResponse{
		Choices: []openRouterChoice{{
			Message: openRouterMessage{
				Role: "assistant",
				ToolCalls: []openRouterToolCall{
					{ID: "call_1", Type: "function", Function: openRouterToolCallFunction{Name: "get_tides", Arguments: openRouterArguments(`{"lat":1,"lon":2}`)}},
					{ID: "call_2", Type: "function", Function: openRouterToolCallFunction{Name: "get_wind_forecast", Arguments: openRouterArguments(`{"lat":1,"lon":2}`)}},
				},
			},
		}},
	})
	round1 := finalResponse(t, "done", "m", openRouterUsage{})

	doer := &queuedChatDoer{responses: []*http.Response{round0, round1}, errs: []error{nil, nil}}
	tools := &slowToolExecutor{delay: 150 * time.Millisecond}
	emit, _ := recordingEmitter()

	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: tools, emit: emit}

	start := time.Now()
	if _, err := runner.run(context.Background(), "system", "", nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	elapsed := time.Since(start)

	// Two calls at 150ms each run serially would take ~300ms; concurrently,
	// about 150ms. 250ms leaves generous headroom for CI jitter while still
	// failing if they ran one after another.
	if elapsed > 250*time.Millisecond {
		t.Fatalf("expected the round's two tool calls to run concurrently, took %s", elapsed)
	}
	if tools.calls != 2 {
		t.Fatalf("expected both tool calls to complete, got %d", tools.calls)
	}
}

func TestAssistantRunner_ToolRoundResultsStayInCallOrderEvenWhenLaterCallFinishesFirst(t *testing.T) {
	round0 := chatResponse(t, http.StatusOK, openRouterChatResponse{
		Choices: []openRouterChoice{{
			Message: openRouterMessage{
				Role: "assistant",
				ToolCalls: []openRouterToolCall{
					{ID: "call_1", Type: "function", Function: openRouterToolCallFunction{Name: "find_places", Arguments: openRouterArguments(`{"query":"Tongue Bay"}`)}},
					{ID: "call_2", Type: "function", Function: openRouterToolCallFunction{Name: "get_tides", Arguments: openRouterArguments(`{"lat":1,"lon":2}`)}},
				},
			},
		}},
	})
	round1 := finalResponse(t, "done", "m", openRouterUsage{})

	doer := &queuedChatDoer{responses: []*http.Response{round0, round1}, errs: []error{nil, nil}}
	// call_1 (find_places) is deliberately the slower of the two, so its
	// result lands after call_2's - the assembled history must still list
	// call_1's tool message before call_2's.
	tools := &orderedDelayToolExecutor{
		delays:  map[string]time.Duration{"find_places": 80 * time.Millisecond, "get_tides": 5 * time.Millisecond},
		results: map[string]string{"find_places": `{"results":[]}`, "get_tides": `{"now":{}}`},
	}
	emit, _ := recordingEmitter()

	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: tools, emit: emit}
	if _, err := runner.run(context.Background(), "system", "", nil); err != nil {
		t.Fatalf("run: %v", err)
	}

	second := doer.requests[1]
	var toolMsgs []openRouterMessage
	for _, m := range second.Messages {
		if m.Role == "tool" {
			toolMsgs = append(toolMsgs, m)
		}
	}
	if len(toolMsgs) != 2 {
		t.Fatalf("expected 2 tool messages, got %d", len(toolMsgs))
	}
	if toolMsgs[0].ToolCallID != "call_1" || toolMsgs[1].ToolCallID != "call_2" {
		t.Fatalf("expected tool messages in call order despite call_1 finishing later, got %+v", toolMsgs)
	}
}

func TestAssistantRunner_CancellingContextStopsInFlightTools(t *testing.T) {
	round0 := chatResponse(t, http.StatusOK, openRouterChatResponse{
		Choices: []openRouterChoice{{
			Message: openRouterMessage{
				Role: "assistant",
				ToolCalls: []openRouterToolCall{
					{ID: "call_1", Type: "function", Function: openRouterToolCallFunction{Name: "get_tides", Arguments: openRouterArguments(`{"lat":1,"lon":2}`)}},
					{ID: "call_2", Type: "function", Function: openRouterToolCallFunction{Name: "get_wind_forecast", Arguments: openRouterArguments(`{"lat":1,"lon":2}`)}},
				},
			},
		}},
	})
	doer := &queuedChatDoer{responses: []*http.Response{round0}, errs: []error{nil}}
	tools := &slowToolExecutor{delay: 2 * time.Second}
	emit, _ := recordingEmitter()

	ctx, cancel := context.WithCancel(context.Background())
	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: tools, emit: emit}

	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := runner.run(ctx, "system", "", nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error when the run's context is cancelled mid-round")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled to propagate, got %v", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("expected cancelling ctx to stop the in-flight tools promptly (not wait out the 2s delay), took %s", elapsed)
	}
}

// ── system message caching breakpoint (backend perf audit, Tier 3 item 3) ──

func TestAssistantSystemMessage_AnthropicModelCarriesCacheControlBreakpoint(t *testing.T) {
	msg := assistantSystemMessage("anthropic/claude-sonnet-4.5", "STABLE PREFIX", "LIVE SUFFIX")
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded struct {
		Role    string `json:"role"`
		Content []struct {
			Type         string `json:"type"`
			Text         string `json:"text"`
			CacheControl *struct {
				Type string `json:"type"`
			} `json:"cache_control"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("expected the anthropic system message to encode content as an array of blocks, got %s: %v", data, err)
	}
	if len(decoded.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %+v", decoded.Content)
	}
	if decoded.Content[0].Text != "STABLE PREFIX" || decoded.Content[0].CacheControl == nil || decoded.Content[0].CacheControl.Type != "ephemeral" {
		t.Fatalf("expected the first block to carry the stable prefix and the ephemeral cache_control breakpoint, got %+v", decoded.Content[0])
	}
	if decoded.Content[1].Text != "LIVE SUFFIX" || decoded.Content[1].CacheControl != nil {
		t.Fatalf("expected the second block to carry the live suffix with no cache_control, got %+v", decoded.Content[1])
	}
}

func TestAssistantSystemMessage_NonAnthropicModelKeepsPlainStringContent(t *testing.T) {
	msg := assistantSystemMessage("openai/gpt-4o", "STABLE PREFIX", "LIVE SUFFIX")
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"content":"STABLE PREFIXLIVE SUFFIX"`) {
		t.Fatalf("expected a plain string content field for a non-anthropic model, got %s", data)
	}
	if strings.Contains(string(data), "cache_control") {
		t.Fatalf("expected no cache_control for a non-anthropic model, got %s", data)
	}
}

func TestAssistantRunner_AnthropicModelRequestCarriesCacheControlBreakpoint(t *testing.T) {
	round0 := finalResponse(t, "ok", "anthropic/claude-sonnet-4.5", openRouterUsage{})
	doer := &queuedChatDoer{responses: []*http.Response{round0}, errs: []error{nil}}
	tools := &fakeToolExecutor{}
	emit, _ := recordingEmitter()

	runner := &assistantRunner{doer: doer, apiKey: "key", model: "anthropic/claude-sonnet-4.5", tools: tools, emit: emit}
	if _, err := runner.run(context.Background(), "STABLE", "LIVE", nil); err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(doer.rawBodies) != 1 {
		t.Fatalf("expected 1 request, got %d", len(doer.rawBodies))
	}
	if !strings.Contains(string(doer.rawBodies[0]), `"cache_control":{"type":"ephemeral"}`) {
		t.Fatalf("expected the anthropic request to carry the cache_control breakpoint, got %s", doer.rawBodies[0])
	}
}

func TestAssistantRunner_NonAnthropicModelRequestHasNoCacheControl(t *testing.T) {
	round0 := finalResponse(t, "ok", "m", openRouterUsage{})
	doer := &queuedChatDoer{responses: []*http.Response{round0}, errs: []error{nil}}
	tools := &fakeToolExecutor{}
	emit, _ := recordingEmitter()

	runner := &assistantRunner{doer: doer, apiKey: "key", model: "m", tools: tools, emit: emit}
	if _, err := runner.run(context.Background(), "STABLE", "LIVE", nil); err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(doer.rawBodies) != 1 {
		t.Fatalf("expected 1 request, got %d", len(doer.rawBodies))
	}
	if strings.Contains(string(doer.rawBodies[0]), "cache_control") {
		t.Fatalf("expected no cache_control for a non-anthropic model, got %s", doer.rawBodies[0])
	}
}
