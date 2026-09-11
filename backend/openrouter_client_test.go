package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// fakeOpenRouterDoer is an injectable openRouterDoer for tests, mirroring
// fakeOverpassFetcher (place_name_test.go): it records every request it
// sees (headers, URL and raw body) and returns queued responses/errors in
// call order.
type fakeOpenRouterDoer struct {
	mu        sync.Mutex
	requests  []*http.Request
	bodies    [][]byte
	responses []*http.Response
	errs      []error
	calls     int
}

func (f *fakeOpenRouterDoer) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
	}
	f.requests = append(f.requests, req)
	f.bodies = append(f.bodies, body)

	idx := f.calls
	f.calls++

	if idx < len(f.errs) && f.errs[idx] != nil {
		return nil, f.errs[idx]
	}
	if idx < len(f.responses) {
		return f.responses[idx], nil
	}
	return nil, fmt.Errorf("fakeOpenRouterDoer: no queued response for call %d", idx)
}

func openRouterFakeResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

// ctxErrDoer simulates a transport that observes the request's context has
// already been cancelled, the way a real *http.Client would.
type ctxErrDoer struct{}

func (ctxErrDoer) Do(req *http.Request) (*http.Response, error) {
	return nil, req.Context().Err()
}

func TestOpenRouterChatCompletion_SendsExpectedRequestShape(t *testing.T) {
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, `{
		"id": "gen-1", "model": "anthropic/claude-sonnet-4.5",
		"choices": [{"index": 0, "message": {"role": "assistant", "content": "hi"}, "finish_reason": "stop"}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 2, "total_tokens": 12, "cost": 0.0184}
	}`)}}

	req := openRouterChatRequest{
		Model:    "anthropic/claude-sonnet-4.5",
		Messages: []openRouterMessage{{Role: "user", Content: "hello"}},
		Tools: []openRouterTool{
			{Type: "function", Function: openRouterFunctionDef{Name: "find_places"}},
		},
		Usage: &openRouterUsageOption{Include: true},
	}

	resp, err := openRouterChatCompletion(context.Background(), doer, "sk-test", req)
	if err != nil {
		t.Fatalf("openRouterChatCompletion: %v", err)
	}

	if len(doer.requests) != 1 {
		t.Fatalf("expected exactly one request, got %d", len(doer.requests))
	}
	got := doer.requests[0]

	if got.URL.String() != "https://openrouter.ai/api/v1/chat/completions" {
		t.Fatalf("unexpected URL: %s", got.URL.String())
	}
	if auth := got.Header.Get("Authorization"); auth != "Bearer sk-test" {
		t.Fatalf("unexpected Authorization header: %q", auth)
	}
	if referer := got.Header.Get("HTTP-Referer"); referer != "https://github.com/gterrill/helmcentral" {
		t.Fatalf("unexpected HTTP-Referer header: %q", referer)
	}
	if title := got.Header.Get("X-Title"); title != "Helmcentral" {
		t.Fatalf("unexpected X-Title header: %q", title)
	}
	if ct := got.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("unexpected Content-Type header: %q", ct)
	}

	body := string(doer.bodies[0])
	if !strings.Contains(body, `"usage":{"include":true}`) {
		t.Fatalf("expected request body to carry usage.include=true, got %s", body)
	}
	if !strings.Contains(body, `"tools"`) {
		t.Fatalf("expected request body to carry tools when set, got %s", body)
	}

	if resp.Choices[0].Message.Content != "hi" {
		t.Fatalf("unexpected content: %q", resp.Choices[0].Message.Content)
	}
	if resp.Usage.Cost != 0.0184 {
		t.Fatalf("expected usage.cost to decode, got %v", resp.Usage.Cost)
	}
}

func TestOpenRouterChatCompletion_OmitsToolsAndUsageWhenUnset(t *testing.T) {
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, `{
		"id": "gen-1", "model": "m",
		"choices": [{"index": 0, "message": {"role": "assistant", "content": "hi"}, "finish_reason": "stop"}]
	}`)}}

	req := openRouterChatRequest{Model: "m", Messages: []openRouterMessage{{Role: "user", Content: "hi"}}}
	if _, err := openRouterChatCompletion(context.Background(), doer, "sk", req); err != nil {
		t.Fatalf("openRouterChatCompletion: %v", err)
	}

	body := string(doer.bodies[0])
	if strings.Contains(body, `"tools"`) {
		t.Fatalf("expected no tools key when unset, got %s", body)
	}
	if strings.Contains(body, `"usage"`) {
		t.Fatalf("expected no usage key when unset, got %s", body)
	}
}

func TestOpenRouterChatCompletion_DecodesToolCallsWithStringArguments(t *testing.T) {
	body := `{
		"id": "gen-1", "model": "m",
		"choices": [{"index": 0, "finish_reason": "tool_calls", "message": {
			"role": "assistant",
			"tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "get_tides", "arguments": "{\"lat\":-20.5}"}}]
		}}]
	}`
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, body)}}

	resp, err := openRouterChatCompletion(context.Background(), doer, "sk", openRouterChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("openRouterChatCompletion: %v", err)
	}

	calls := resp.Choices[0].Message.ToolCalls
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].ID != "call_1" || calls[0].Type != "function" || calls[0].Function.Name != "get_tides" {
		t.Fatalf("unexpected tool call: %+v", calls[0])
	}
	if calls[0].Function.Arguments != `{"lat":-20.5}` {
		t.Fatalf("expected string arguments to decode verbatim, got %q", calls[0].Function.Arguments)
	}
}

func TestOpenRouterChatCompletion_DecodesArrayContentToJoinedText(t *testing.T) {
	body := `{
		"id": "gen-1", "model": "m",
		"choices": [{"index": 0, "finish_reason": "stop", "message": {
			"role": "assistant",
			"content": [{"type": "text", "text": "Tongue Bay "}, {"type": "text", "text": "looks good."}]
		}}]
	}`
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, body)}}

	resp, err := openRouterChatCompletion(context.Background(), doer, "sk", openRouterChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("openRouterChatCompletion: %v", err)
	}
	if resp.Choices[0].Message.Content != "Tongue Bay looks good." {
		t.Fatalf("expected joined text parts, got %q", resp.Choices[0].Message.Content)
	}
}

func TestOpenRouterChatCompletion_DecodesObjectArgumentsToCompactString(t *testing.T) {
	body := `{
		"id": "gen-1", "model": "m",
		"choices": [{"index": 0, "finish_reason": "tool_calls", "message": {
			"role": "assistant",
			"tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "get_tides", "arguments": {"lat": -20.5, "lon": 149.1}}}]
		}}]
	}`
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, body)}}

	resp, err := openRouterChatCompletion(context.Background(), doer, "sk", openRouterChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("openRouterChatCompletion: %v", err)
	}

	args := resp.Choices[0].Message.ToolCalls[0].Function.Arguments
	var decoded map[string]any
	if err := json.Unmarshal([]byte(args), &decoded); err != nil {
		t.Fatalf("expected arguments to decode as JSON, got %q: %v", args, err)
	}
	if decoded["lat"] != -20.5 || decoded["lon"] != 149.1 {
		t.Fatalf("unexpected decoded arguments: %+v", decoded)
	}
}

func TestOpenRouterChatCompletion_401ReturnsUpstreamMessageAndStatus(t *testing.T) {
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(401, `{"error":{"message":"bad key"}}`)}}

	_, err := openRouterChatCompletion(context.Background(), doer, "sk", openRouterChatRequest{Model: "m"})
	if err == nil {
		t.Fatalf("expected an error for a 401 response")
	}
	if !strings.Contains(err.Error(), "bad key") {
		t.Fatalf("expected error to contain the upstream message, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected error to contain the status code, got %q", err.Error())
	}
	if strings.Contains(err.Error(), "\n") {
		t.Fatalf("expected a one-line error, got %q", err.Error())
	}
}

func TestOpenRouterChatCompletion_TopLevelErrorOn200IsAnError(t *testing.T) {
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, `{"error":{"message":"model not found"},"choices":[]}`)}}

	_, err := openRouterChatCompletion(context.Background(), doer, "sk", openRouterChatRequest{Model: "m"})
	if err == nil {
		t.Fatalf("expected an error when the response carries a top-level error field")
	}
	if !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("expected error to contain the upstream message, got %q", err.Error())
	}
}

func TestOpenRouterChatCompletion_ZeroChoicesIsAnError(t *testing.T) {
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, `{"id":"x","model":"m","choices":[]}`)}}

	_, err := openRouterChatCompletion(context.Background(), doer, "sk", openRouterChatRequest{Model: "m"})
	if err == nil {
		t.Fatalf("expected an error for zero choices")
	}
}

func TestOpenRouterChatCompletion_PropagatesContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := openRouterChatCompletion(ctx, ctxErrDoer{}, "sk", openRouterChatRequest{Model: "m"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled to propagate, got %v", err)
	}
}
