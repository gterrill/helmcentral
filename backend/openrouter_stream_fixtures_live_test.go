package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"os"
	"slices"
	"testing"
)

// This file captures real OpenRouter streamed chat-completion response
// bodies to backend/testdata/, for openrouter_client_test.go's fixture test
// to decode against - a real upstream shape rather than an assumed one
// ([[feedback_verify_fixtures_against_live_data]]).
//
// It is opt-in on two separate switches, deliberately more guarded than
// this package's other live tests (assistant_tools_live_test.go,
// wasm_poi_provider_live_test.go): skipped under -short like every other
// live-network test here, AND skipped unless
// HELMCENTRAL_CAPTURE_OPENROUTER_FIXTURES=1 is set, because unlike a read
// against a free OSM mirror, every run of this test spends the operator's
// real OpenRouter balance. `go test -short ./...` (this repo's normal test
// command, per house rules) never runs it.
//
// The API key is resolved exactly the way postAssistantMessageHandler does
// (checkAssistantReadiness, assistant_handlers.go): read from the same
// encrypted secrets store and the same settings.yaml the running server
// uses, never a separate env var or a hand-decrypted copy. The key is never
// logged, printed, or written anywhere by this file - only the response
// bodies OpenRouter itself sends back are captured, and OpenRouter's own
// streamed chunks do not echo the request's Authorization header back.
func TestCaptureOpenRouterStreamFixtures_Live(t *testing.T) {
	apiKey, model := liveOpenRouterSetup(t)

	textReq := openRouterChatRequest{
		Model: model,
		Messages: []openRouterMessage{
			{Role: "system", Content: "Reply with exactly one short sentence (12 words or fewer). Call no tools."},
			{Role: "user", Content: "What is the capital of France?"},
		},
		Usage: &openRouterUsageOption{Include: true},
	}
	captureOpenRouterRawStream(t, apiKey, textReq, "testdata/openrouter_stream_text.txt")

	toolReq := openRouterChatRequest{
		Model: model,
		Messages: []openRouterMessage{
			{Role: "system", Content: "You have exactly one tool available. You must call it to answer; do not answer directly."},
			{Role: "user", Content: "Use the tool to tell me the current time in UTC."},
		},
		Tools: []openRouterTool{
			{Type: "function", Function: openRouterFunctionDef{
				Name:        "get_current_time",
				Description: "Returns the current time for a given IANA timezone.",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"timezone":{"type":"string","description":"IANA timezone name, e.g. UTC or Australia/Brisbane"}},"required":["timezone"]}`),
			}},
		},
		ToolChoice: "required",
		Usage:      &openRouterUsageOption{Include: true},
	}
	captureOpenRouterRawStream(t, apiKey, toolReq, "testdata/openrouter_stream_toolcall.txt")
}

// TestCaptureOpenRouterReasoningRoundTrip_Live checks, against the real
// API and the operator's configured Mate model, that the reasoning_details
// openRouterChatCompletion rebuilds from a tool-call round's stream are
// accepted when sent back on the next round. Round 1's raw body is saved for
// openrouter_client_test.go to decode against; round 2 only has to be
// accepted, so it is not saved. Same two opt-in switches as TestCaptureOpenRouterStreamFixtures_Live; it spends real
// balance.
func TestCaptureOpenRouterReasoningRoundTrip_Live(t *testing.T) {
	apiKey, model := liveOpenRouterSetup(t)
	t.Logf("model: %s", model)

	tools := []openRouterTool{{Type: "function", Function: openRouterFunctionDef{
		Name:        "get_tide_height",
		Description: "Returns the tide height in metres at a named place now.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"place":{"type":"string"}},"required":["place"]}`),
	}}}
	messages := []openRouterMessage{
		{Role: "system", Content: "Think it through, then use the tool. Answer in one short sentence."},
		{Role: "user", Content: "Is there more than 2 m of tide at Rangitoto now?"},
	}

	round1 := &teeOpenRouterDoer{t: t, dest: "testdata/openrouter_stream_reasoning_toolcall.txt"}
	resp1, err := openRouterChatCompletion(context.Background(), round1, apiKey, openRouterChatRequest{
		Model: model, Messages: messages, Tools: tools, Usage: &openRouterUsageOption{Include: true},
	}, noStreamHooks)
	if err != nil {
		t.Fatalf("round 1: %v", err)
	}
	choice := resp1.Choices[0].Message
	if len(choice.ToolCalls) == 0 {
		t.Fatalf("round 1 made no tool call; content %q", string(choice.Content))
	}
	t.Logf("round 1: model %s, %d reasoning_details entries", resp1.Model, len(choice.ReasoningDetails))
	for i, d := range choice.ReasoningDetails {
		t.Logf("  detail %d: type=%v format=%v keys=%v", i, d["type"], d["format"], slices.Sorted(maps.Keys(d)))
	}

	messages = append(messages, choice, openRouterMessage{Role: "tool", ToolCallID: choice.ToolCalls[0].ID, Content: openRouterContent(`{"height_m":2.4}`)})
	resp2, err := openRouterChatCompletion(context.Background(), openRouterHTTPClient, apiKey, openRouterChatRequest{
		Model: model, Messages: messages, Tools: tools, Usage: &openRouterUsageOption{Include: true},
	}, noStreamHooks)
	if err != nil {
		t.Fatalf("round 2 (reasoning sent back) rejected: %v", err)
	}
	t.Logf("round 2 accepted: %q", string(resp2.Choices[0].Message.Content))
}

// teeOpenRouterDoer sends the request to the real OpenRouter and saves the
// raw response body to dest while handing the same bytes to the caller's
// parser, so the fixture is what OpenRouter sent, not a re-serialisation.
type teeOpenRouterDoer struct {
	t    *testing.T
	dest string
}

func (d *teeOpenRouterDoer) Do(req *http.Request) (*http.Response, error) {
	resp, err := openRouterHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	// An error body must not replace a good committed fixture.
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if err := os.WriteFile(d.dest, raw, 0o644); err != nil {
			d.t.Fatalf("write fixture %s: %v", d.dest, err)
		}
		d.t.Logf("captured %d bytes to %s", len(raw), d.dest)
	}
	resp.Body = io.NopCloser(bytes.NewReader(raw))
	return resp, nil
}

// liveOpenRouterSetup skips unless both opt-in switches are on, then
// resolves the operator's OpenRouter key and Mate model.
func liveOpenRouterSetup(t *testing.T) (apiKey, model string) {
	t.Helper()
	if testing.Short() {
		t.Skip("live OpenRouter call, costs real money; skipped under -short")
	}
	if os.Getenv("HELMCENTRAL_CAPTURE_OPENROUTER_FIXTURES") != "1" {
		t.Skip("set HELMCENTRAL_CAPTURE_OPENROUTER_FIXTURES=1 to run this against the real OpenRouter API")
	}

	// Point globalSecretsStore at the real, on-disk encrypted store (the
	// same default paths secretsDBPath/secretsKeyPath resolve to for a live
	// server) rather than a fresh t.TempDir() fixture - this test needs
	// whatever key the operator actually configured, not a fake one.
	store, err := newSecretsStore(secretsDBPath(), secretsKeyPath())
	if err != nil {
		t.Fatalf("open the real secrets store at %s: %v (no local OpenRouter key available to capture fixtures with)", secretsDBPath(), err)
	}
	t.Cleanup(func() { store.db.Close() })
	prevStore := globalSecretsStore
	globalSecretsStore = store
	t.Cleanup(func() { globalSecretsStore = prevStore })

	readiness, apiKey, err := checkAssistantReadiness(assistantSettingsPath())
	if err != nil {
		t.Fatalf("checkAssistantReadiness: %v", err)
	}
	if readiness.Problem != "" {
		t.Fatalf("assistant not ready to capture fixtures with (%s) - no local OpenRouter key/model configured; hand-write the fixtures instead", readiness.Problem)
	}
	return apiKey, readiness.Model
}

// captureOpenRouterRawStream posts req (forced to Stream: true, mirroring
// what the production client now always sends) directly against OpenRouter
// - deliberately bypassing openRouterChatCompletion's own SSE parsing - and
// writes the raw, unparsed response body to destPath. Saving the bytes
// exactly as OpenRouter sent them, rather than a re-serialization of
// whatever this codebase's own parser made of them, is the point: a bug in
// the parser must not also corrupt the fixture it is tested against.
func captureOpenRouterRawStream(t *testing.T, apiKey string, req openRouterChatRequest, destPath string) {
	t.Helper()
	req.Stream = true

	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	httpReq, err := http.NewRequestWithContext(context.Background(), http.MethodPost, openRouterChatCompletionsURL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("HTTP-Referer", "https://github.com/gterrill/helmcentral")
	httpReq.Header.Set("X-Title", "Helmcentral")

	resp, err := openRouterHTTPClient.Do(httpReq)
	if err != nil {
		t.Fatalf("openrouter request failed: %v", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read openrouter response: %v", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("openrouter status %d: %s", resp.StatusCode, raw)
	}

	if err := os.WriteFile(destPath, raw, 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", destPath, err)
	}
	t.Logf("captured %d bytes to %s", len(raw), destPath)
}
