package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
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
	defer store.db.Close()
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

	textReq := openRouterChatRequest{
		Model: readiness.Model,
		Messages: []openRouterMessage{
			{Role: "system", Content: "Reply with exactly one short sentence (12 words or fewer). Call no tools."},
			{Role: "user", Content: "What is the capital of France?"},
		},
		Usage: &openRouterUsageOption{Include: true},
	}
	captureOpenRouterRawStream(t, apiKey, textReq, "testdata/openrouter_stream_text.txt")

	toolReq := openRouterChatRequest{
		Model: readiness.Model,
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
