package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// ── fakes ───────────────────────────────────────────────────────────────

// queuedChatCall is one captured request/response pair a queuedChatDoer
// replays: the decoded request body (for assertions on what the runner sent)
// paired with the canned response (or error) to hand back.
type queuedChatDoer struct {
	responses []*http.Response
	errs      []error

	requests []openRouterChatRequest
}

func (q *queuedChatDoer) Do(req *http.Request) (*http.Response, error) {
	data, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
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
// SignalK, Overpass or weather/tide provider needed).
type fakeToolExecutor struct {
	results map[string]string
	errs    map[string]error
	calls   []fakeToolCall
}

func (f *fakeToolExecutor) execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	f.calls = append(f.calls, fakeToolCall{name: name, args: string(args)})
	if err, ok := f.errs[name]; ok {
		return "", err
	}
	if result, ok := f.results[name]; ok {
		return result, nil
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
	reply, err := runner.run(context.Background(), "system prompt", nil)
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

	if len(tools.calls) != 2 {
		t.Fatalf("expected both tools executed, got %d calls: %+v", len(tools.calls), tools.calls)
	}
	if tools.calls[0].name != "find_places" || tools.calls[1].name != "get_wind_forecast" {
		t.Fatalf("unexpected tool call order/names: %+v", tools.calls)
	}

	if len(doer.requests) != 2 {
		t.Fatalf("expected 2 completion requests, got %d", len(doer.requests))
	}
	second := doer.requests[1]
	// The assistant's tool_calls message from round 0 must be echoed back
	// verbatim as history, followed by one tool-role message per call with
	// the matching tool_call_id.
	var assistantMsg *openRouterMessage
	toolByID := map[string]openRouterMessage{}
	for i := range second.Messages {
		m := second.Messages[i]
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			assistantMsg = &second.Messages[i]
		}
		if m.Role == "tool" {
			toolByID[m.ToolCallID] = m
		}
	}
	if assistantMsg == nil || len(assistantMsg.ToolCalls) != 2 {
		t.Fatalf("expected the second request to echo the assistant's tool_calls message, got %+v", second.Messages)
	}
	if tm, ok := toolByID["call_1"]; !ok || string(tm.Content) != `{"results":[{"name":"Tongue Bay"}]}` {
		t.Fatalf("expected tool message for call_1 with matching content, got %+v ok=%v", tm, ok)
	}
	if tm, ok := toolByID["call_2"]; !ok || string(tm.Content) != `{"days":[]}` {
		t.Fatalf("expected tool message for call_2 with matching content, got %+v ok=%v", tm, ok)
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
	if _, err := runner.run(context.Background(), "system", nil); err != nil {
		t.Fatalf("run: %v", err)
	}

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
	reply, err := runner.run(context.Background(), "system", nil)
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
	reply, err := runner.run(context.Background(), "system", nil)
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
	_, err := runner.run(context.Background(), "system", nil)
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
	_, err := runner.run(context.Background(), "system", nil)
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
	_, err := runner.run(context.Background(), "system", nil)
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
