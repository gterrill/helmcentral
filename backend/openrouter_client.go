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
	"time"
)

// openRouterChatCompletionsURL is OpenRouter's OpenAI-compatible
// chat-completions endpoint (ADR 0093). Helmcentral talks to it directly
// with a hand-rolled client rather than github.com/sashabaranov/go-openai
// (see ADR 0065 §5's follow-up): OpenRouter's usage.include cost field, its
// HTTP-Referer/X-Title attribution headers, and tolerant decoding of the
// tool-call shape quirks below have no clean home in a generic OpenAI
// client, and one POST behind one Do(req) seam is all this needs.
const openRouterChatCompletionsURL = "https://openrouter.ai/api/v1/chat/completions"

// openRouterCompletionTimeout bounds a single chat-completion round trip.
// The assistant's agentic loop (assistant_run.go) makes several of these per
// reply, each independently timed out so one slow round never wedges the
// whole conversation.
const openRouterCompletionTimeout = 60 * time.Second

// openRouterDoer is the minimal interface the client needs from an HTTP
// client, mirroring overpassFetcher (place_name.go) so tests can inject a
// fake upstream. *http.Client already satisfies this interface.
type openRouterDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// openRouterHTTPClient is the production openRouterDoer. Tests swap it, or
// pass a fake directly to openRouterChatCompletion, and restore it
// afterward.
var openRouterHTTPClient openRouterDoer = &http.Client{Timeout: openRouterCompletionTimeout}

// openRouterFunctionDef describes one callable tool in the OpenAI
// function-calling shape OpenRouter proxies unchanged.
type openRouterFunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// openRouterTool wraps a function definition in the {"type":"function",...}
// envelope the chat-completions API expects.
type openRouterTool struct {
	Type     string                `json:"type"`
	Function openRouterFunctionDef `json:"function"`
}

// openRouterContent is chat message content that tolerates the two wire
// shapes a model behind OpenRouter may emit: a plain string, or an array of
// content parts such as [{"type":"text","text":"..."}]. Both decode to a
// plain Go string (text parts joined in order, non-text parts such as image
// references dropped); a named string type re-encodes as a JSON string with
// no custom MarshalJSON needed, so a message read from a response and
// replayed into the next request's history round-trips as a plain string.
type openRouterContent string

func (c *openRouterContent) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		*c = ""
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return fmt.Errorf("decode string content: %w", err)
		}
		*c = openRouterContent(s)
		return nil
	}

	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(trimmed, &parts); err != nil {
		return fmt.Errorf("decode content parts: %w", err)
	}
	var joined strings.Builder
	for _, part := range parts {
		if part.Type != "text" {
			continue
		}
		joined.WriteString(part.Text)
	}
	*c = openRouterContent(joined.String())
	return nil
}

// openRouterArguments is a tool call's arguments, tolerant of the two shapes
// models behind OpenRouter emit: a JSON-encoded string (the OpenAI norm) or,
// for some models, a bare JSON object. Both decode to the compact JSON
// string a caller can json.Unmarshal directly; a named string type
// re-encodes as a JSON string automatically.
type openRouterArguments string

func (a *openRouterArguments) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		*a = ""
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return fmt.Errorf("decode string arguments: %w", err)
		}
		*a = openRouterArguments(s)
		return nil
	}

	var obj any
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return fmt.Errorf("decode object arguments: %w", err)
	}
	compact, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("re-marshal object arguments: %w", err)
	}
	*a = openRouterArguments(compact)
	return nil
}

// openRouterToolCallFunction is the function half of a tool call: which
// tool the model wants to run, and its arguments (see openRouterArguments).
type openRouterToolCallFunction struct {
	Name      string              `json:"name"`
	Arguments openRouterArguments `json:"arguments"`
}

// openRouterToolCall is one tool invocation the model asked for. ID matches
// the tool_call_id on the openRouterMessage carrying the tool's result.
type openRouterToolCall struct {
	ID       string                     `json:"id"`
	Type     string                     `json:"type"`
	Function openRouterToolCallFunction `json:"function"`
}

// openRouterMessage is one turn of the conversation: system, user,
// assistant or tool. ToolCalls is set on an assistant message that invoked
// tools; ToolCallID identifies which call a tool-role message answers.
type openRouterMessage struct {
	Role       string               `json:"role"`
	Content    openRouterContent    `json:"content,omitempty"`
	ToolCalls  []openRouterToolCall `json:"tool_calls,omitempty"`
	ToolCallID string               `json:"tool_call_id,omitempty"`
}

// openRouterUsageOption requests OpenRouter's per-reply cost accounting.
// Without usage.include=true, a response carries no cost field at all.
type openRouterUsageOption struct {
	Include bool `json:"include"`
}

type openRouterPlugin struct {
	ID             string   `json:"id"`
	AllowedModels  []string `json:"allowed_models,omitempty"`
	ExcludedModels []string `json:"excluded_models,omitempty"`
	CostTier       string   `json:"cost_tier,omitempty"`
}

// openRouterChatRequest is the request body for POST
// /api/v1/chat/completions. ToolChoice is a bare string ("none" to force a
// final answer with no further tool calls, see assistant_run.go); omitted
// it defaults to OpenRouter's own "auto" behaviour.
type openRouterChatRequest struct {
	Model      string                 `json:"model"`
	Messages   []openRouterMessage    `json:"messages"`
	Tools      []openRouterTool       `json:"tools,omitempty"`
	Plugins    []openRouterPlugin     `json:"plugins,omitempty"`
	ToolChoice string                 `json:"tool_choice,omitempty"`
	Usage      *openRouterUsageOption `json:"usage,omitempty"`
}

type openRouterChoice struct {
	Index        int               `json:"index"`
	Message      openRouterMessage `json:"message"`
	FinishReason string            `json:"finish_reason"`
}

// openRouterUsage is per-reply token and dollar accounting, present only
// when the request carried usage.include=true.
type openRouterUsage struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	Cost             float64 `json:"cost"`
}

// openRouterAPIError is OpenRouter's error envelope, returned either as the
// body of a non-2xx response or, for some failure modes, embedded in an
// otherwise-200 response. Code varies in type across providers (numeric or
// string), hence any.
type openRouterAPIError struct {
	Code    any    `json:"code"`
	Message string `json:"message"`
}

type openRouterChatResponse struct {
	ID      string              `json:"id"`
	Model   string              `json:"model"`
	Choices []openRouterChoice  `json:"choices"`
	Usage   openRouterUsage     `json:"usage"`
	Error   *openRouterAPIError `json:"error,omitempty"`
}

// openRouterChatCompletion posts one chat-completion request and returns the
// decoded response. Three failure shapes all surface as a single-line error
// carrying the upstream message and (where meaningful) the HTTP status,
// mirroring how place_name.go and wasm_plugin.go trim upstream errors for a
// status line rather than a stack trace: a non-2xx status, a 2xx response
// that still carries a top-level error object (some OpenRouter failure
// modes report this way instead of a non-2xx status), and a 2xx response
// with zero choices (nothing to reply with).
func openRouterChatCompletion(ctx context.Context, doer openRouterDoer, apiKey string, req openRouterChatRequest) (openRouterChatResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return openRouterChatResponse{}, fmt.Errorf("marshal openrouter request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, openRouterChatCompletionsURL, bytes.NewReader(body))
	if err != nil {
		return openRouterChatResponse{}, fmt.Errorf("build openrouter request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("HTTP-Referer", "https://github.com/gterrill/helmcentral")
	httpReq.Header.Set("X-Title", "Helmcentral")

	resp, err := doer.Do(httpReq)
	if err != nil {
		return openRouterChatResponse{}, fmt.Errorf("openrouter request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return openRouterChatResponse{}, fmt.Errorf("read openrouter response: %w", err)
	}

	var parsed openRouterChatResponse
	parseErr := json.Unmarshal(respBody, &parsed)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(respBody))
		if parseErr == nil && parsed.Error != nil && parsed.Error.Message != "" {
			msg = parsed.Error.Message
		}
		return openRouterChatResponse{}, fmt.Errorf("openrouter status %d: %s", resp.StatusCode, firstErrorLine(errors.New(msg)))
	}

	if parseErr != nil {
		return openRouterChatResponse{}, fmt.Errorf("parse openrouter response: %w", parseErr)
	}
	if parsed.Error != nil {
		return openRouterChatResponse{}, fmt.Errorf("openrouter error: %s", firstErrorLine(errors.New(parsed.Error.Message)))
	}
	if len(parsed.Choices) == 0 {
		return openRouterChatResponse{}, fmt.Errorf("openrouter returned no choices (status %d)", resp.StatusCode)
	}

	return parsed, nil
}
