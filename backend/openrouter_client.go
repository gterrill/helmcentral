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

// openRouterResponseHeaderTimeout bounds how long the transport will wait
// for a dead upstream to send response headers at all. It does not bound
// reading a slow model's body once headers arrive - that is
// openRouterCompletionTimeout's job below, via the per-round context
// assistant_run.go derives from it. A var, not a const, so a test can
// shrink it and build a fresh client with newOpenRouterHTTPClient rather
// than waiting out a real 60s.
var openRouterResponseHeaderTimeout = 60 * time.Second

// openRouterCompletionTimeout bounds one whole chat-completion round trip,
// body included. The assistant's agentic loop (assistant_run.go) derives
// each round's context deadline from this. It used to also be the
// *http.Client's own Timeout field, which capped the entire request
// (headers and body together) at 60s - long enough that a genuinely slow
// but healthy model's full answer could fail after the operator had
// already waited the whole 60s for it. 180s replaces that: the transport's
// ResponseHeaderTimeout above still fails a dead upstream fast, and this is
// now the only thing bounding a live one. A var so a test can shrink it.
var openRouterCompletionTimeout = 180 * time.Second

// openRouterDoer is the minimal interface the client needs from an HTTP
// client, mirroring tileFetcher (tile_cache.go) so tests can inject a
// fake upstream. *http.Client already satisfies this interface.
type openRouterDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// newOpenRouterHTTPClient builds the production openRouterDoer: an
// *http.Client with no overall Timeout field (that used to cap the whole
// request, body included - see openRouterCompletionTimeout above) and a
// cloned default transport whose ResponseHeaderTimeout still fails a dead
// upstream quickly. Cloning http.DefaultTransport, rather than starting
// from a bare &http.Transport{}, keeps its other defaults (proxy from the
// environment, connection pooling, TLS handshake timeout) instead of
// silently losing them.
func newOpenRouterHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = openRouterResponseHeaderTimeout
	return &http.Client{Transport: transport}
}

// openRouterHTTPClient is the production openRouterDoer. Tests swap it, or
// pass a fake directly to openRouterChatCompletion, and restore it
// afterward.
var openRouterHTTPClient openRouterDoer = newOpenRouterHTTPClient()

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

// openRouterCacheControl is the {"type":"ephemeral"} marker OpenRouter
// documents for a provider-side prompt-caching breakpoint. Helmcentral only
// ever sets it on a system message's first content block, and only for an
// Anthropic model (assistant_run.go's assistantSystemMessage, ADR 0093's
// prompt-caching follow-up) - see openRouterContentBlock.
type openRouterCacheControl struct {
	Type string `json:"type"`
}

// openRouterContentBlock is one block of a chat message's content when it
// is sent as an array rather than a plain string. Helmcentral only ever
// builds these for the system message it sends an Anthropic model: a
// cache_control breakpoint on the block carrying the stable prefix tells
// Anthropic's own cache (via OpenRouter) that everything up to and
// including that block is eligible to be reused on the next turn.
type openRouterContentBlock struct {
	Type         string                  `json:"type"`
	Text         string                  `json:"text"`
	CacheControl *openRouterCacheControl `json:"cache_control,omitempty"`
}

// openRouterMessage is one turn of the conversation: system, user,
// assistant or tool. ToolCalls is set on an assistant message that invoked
// tools; ToolCallID identifies which call a tool-role message answers.
//
// Content is a plain string for every message this codebase builds except
// one: the system message sent to an Anthropic model, which needs an array
// of content blocks instead so a cache_control breakpoint can mark the end
// of the reusable prefix (assistant_run.go's assistantSystemMessage).
// contentBlocks carries that array when set; MarshalJSON below prefers it
// over Content whenever it is non-empty. It is never populated by decoding
// an incoming response - openRouterContent's own UnmarshalJSON already
// tolerates a response that comes back as an array of text parts (some
// models emit that shape) and folds it into the plain Content string, which
// is all any caller here has ever needed to read back.
type openRouterMessage struct {
	Role       string               `json:"role"`
	Content    openRouterContent    `json:"content,omitempty"`
	ToolCalls  []openRouterToolCall `json:"tool_calls,omitempty"`
	ToolCallID string               `json:"tool_call_id,omitempty"`

	contentBlocks []openRouterContentBlock
}

// MarshalJSON encodes exactly what the plain struct tags above used to -
// including omitting the "content" key entirely on a tool_calls-only
// assistant message - unless contentBlocks is set, in which case an array
// of content blocks is encoded instead of Content's plain string. This is
// the only place in the codebase that builds a content-blocks message (see
// openRouterMessage's own doc comment), so every message on every other
// model still encodes exactly as it always did.
func (m openRouterMessage) MarshalJSON() ([]byte, error) {
	type wire struct {
		Role       string               `json:"role"`
		Content    json.RawMessage      `json:"content,omitempty"`
		ToolCalls  []openRouterToolCall `json:"tool_calls,omitempty"`
		ToolCallID string               `json:"tool_call_id,omitempty"`
	}
	w := wire{Role: m.Role, ToolCalls: m.ToolCalls, ToolCallID: m.ToolCallID}

	switch {
	case len(m.contentBlocks) > 0:
		data, err := json.Marshal(m.contentBlocks)
		if err != nil {
			return nil, fmt.Errorf("marshal openrouter content blocks: %w", err)
		}
		w.Content = data
	case m.Content != "":
		data, err := json.Marshal(m.Content)
		if err != nil {
			return nil, fmt.Errorf("marshal openrouter content: %w", err)
		}
		w.Content = data
	}

	return json.Marshal(w)
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
