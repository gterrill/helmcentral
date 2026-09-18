package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
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

// openRouterEmbeddingsURL is OpenRouter's embeddings endpoint (E1b: document
// semantic search). It takes the same Authorization/HTTP-Referer/X-Title
// headers as chat completions - see doOpenRouterRequestBody, which both
// endpoints now share.
const openRouterEmbeddingsURL = "https://openrouter.ai/api/v1/embeddings"

// maxOpenRouterEmbeddingsBatch bounds how many strings openRouterEmbeddings
// will send in one request. OpenRouter's own limit is far higher, but how
// many chunks to batch into one call is the caller's decision (the document
// indexer's own batching policy), not something this function should
// silently split on the caller's behalf - a silent split would hide what
// could otherwise be a caller bug, such as an entire document's chunks
// landing in one slice by accident.
const maxOpenRouterEmbeddingsBatch = 64

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

// openRouterFileBlock is the "file" half of a file content block (B4's
// document enrichment): a base64 data URL of the whole file, sent to the
// file-parser plugin (openRouterPluginPDF) for OCR.
type openRouterFileBlock struct {
	Filename string `json:"filename"`
	FileData string `json:"file_data"`
}

// openRouterImageURLBlock is the "image_url" half of an image content
// block (B4's document enrichment): a base64 data URL, the same shape
// OpenAI-compatible vision models expect.
type openRouterImageURLBlock struct {
	URL string `json:"url"`
}

// openRouterContentBlock is one block of a chat message's content when it
// is sent as an array rather than a plain string. Two callers build these:
// assistant_run.go's assistantSystemMessage, for the system message sent to
// an Anthropic model (a cache_control breakpoint on the block carrying the
// stable prefix tells Anthropic's own cache, via OpenRouter, that
// everything up to and including that block is eligible to be reused on the
// next turn), and B4's document enrichment (documents_enrich.go), which
// pairs a plain text prompt block with a file or image_url block carrying
// the document itself. Only File or ImageURL is ever set, never both, and
// never alongside a genuinely non-text Type with Text also set - MarshalJSON
// below only ever writes the "text" key for Type=="text", so a file/image
// block's Text field (always "" in practice) never appears on the wire.
type openRouterContentBlock struct {
	Type         string
	Text         string
	CacheControl *openRouterCacheControl
	File         *openRouterFileBlock
	ImageURL     *openRouterImageURLBlock
}

// MarshalJSON encodes exactly what the plain struct tags used to before
// File/ImageURL existed for every text block still in use today (Text
// always written, cache_control omitted only when nil) - see
// TestOpenRouterContentBlock_TextBlockMarshalUnchanged - while adding "file"
// and "image_url" keys for B4's new block kinds, and omitting "text"
// entirely for them (openRouterContentBlock's own doc comment explains why
// a plain `Text string \`json:"text"\“ field, with no omitempty, would
// otherwise put a stray `"text":""` on every file/image block).
func (b openRouterContentBlock) MarshalJSON() ([]byte, error) {
	type wire struct {
		Type         string                   `json:"type"`
		Text         string                   `json:"text,omitempty"`
		CacheControl *openRouterCacheControl  `json:"cache_control,omitempty"`
		File         *openRouterFileBlock     `json:"file,omitempty"`
		ImageURL     *openRouterImageURLBlock `json:"image_url,omitempty"`
	}
	w := wire{Type: b.Type, CacheControl: b.CacheControl, File: b.File, ImageURL: b.ImageURL}
	if b.Type == "text" {
		w.Text = b.Text
	}
	return json.Marshal(w)
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

	// Annotations carries the file-parser plugin's OCR result (B4's
	// document enrichment): one entry per file sent, each holding the
	// extracted text as a sequence of content parts bracketed by
	// `<file name="...">`/`</file>` sentinels, one part per page
	// (backend/testdata/openrouter_document_pdf_2p.json). Decode-only - it
	// is never set on a message this codebase sends, so MarshalJSON below
	// (which builds its own "wire" struct field-by-field) never echoes it
	// back into a request.
	Annotations []openRouterAnnotation `json:"annotations,omitempty"`

	contentBlocks []openRouterContentBlock
}

// openRouterAnnotationFileContentPart is one part of an
// openRouterAnnotationFile's Content array - either a `<file ...>`/
// `</file>` sentinel or one page's worth of OCR'd text.
type openRouterAnnotationFileContentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// openRouterAnnotationFile is the "file" half of one annotation: which file
// this is (by name and content hash - re-uploading the identical bytes
// would produce the identical hash) and its OCR'd text, split into parts.
type openRouterAnnotationFile struct {
	Hash    string                                `json:"hash"`
	Name    string                                `json:"name"`
	Content []openRouterAnnotationFileContentPart `json:"content"`
}

// openRouterAnnotation is one entry of openRouterMessage.Annotations - the
// file-parser plugin's only documented annotation type is "file".
type openRouterAnnotation struct {
	Type string                   `json:"type"`
	File openRouterAnnotationFile `json:"file"`
}

// openRouterUserMessage builds a user-role message whose content is the
// given blocks (B4's document enrichment: a text prompt block paired with a
// file or image_url block). Always non-empty content blocks, so
// openRouterMessage's own MarshalJSON encodes Content as an array rather
// than falling back to the empty plain-string case.
func openRouterUserMessage(blocks ...openRouterContentBlock) openRouterMessage {
	return openRouterMessage{Role: "user", contentBlocks: blocks}
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

// openRouterPluginPDF configures the file-parser plugin's PDF handling
// (B4's document enrichment). Engine "mistral-ocr" is the only engine this
// codebase requests - see documents_enrich.go.
type openRouterPluginPDF struct {
	Engine string `json:"engine,omitempty"`
}

type openRouterPlugin struct {
	ID             string               `json:"id"`
	AllowedModels  []string             `json:"allowed_models,omitempty"`
	ExcludedModels []string             `json:"excluded_models,omitempty"`
	CostTier       string               `json:"cost_tier,omitempty"`
	PDF            *openRouterPluginPDF `json:"pdf,omitempty"`
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
	// Stream is always set explicitly by whichever function actually sends
	// the request, never by a caller building this struct: true by
	// openRouterChatCompletion (its own doc comment), false by
	// openRouterChatCompletionOnce. No omitempty, so the false case is
	// still visible on the wire as "stream":false rather than silently
	// omitted (OpenRouter defaults to non-streaming either way, but B4's
	// non-streaming call is deliberate, not an accident of a zero value).
	Stream bool `json:"stream"`
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

// openRouterStreamToolCallDelta is one fragment of one tool call inside a
// streamed chunk's delta.tool_calls array. Index is the only field every
// fragment for a given tool call carries; OpenRouter sends id/type/
// function.name once, on the first fragment for that index, and every
// fragment (including that first one) carries a piece of
// function.arguments to be concatenated in order - see
// openRouterChatCompletion's accumulation loop. Arguments is typed as
// openRouterArguments, not a plain string, purely for the tolerant-decoding
// safety net described on that type: real captures (backend/testdata/
// openrouter_stream_toolcall.txt) show every fragment's own JSON encoding
// is a plain string, but should a model behind OpenRouter ever emit one
// fragment as a bare JSON object instead, this decodes it the same way a
// non-streamed response already would rather than failing to parse.
type openRouterStreamToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string              `json:"name"`
		Arguments openRouterArguments `json:"arguments"`
	} `json:"function"`
}

// openRouterStreamChoice is one choice inside a streamed chunk. Unlike
// openRouterChoice (the non-streaming shape), Message is replaced by Delta:
// a fragment to be merged into the choice being assembled, not a complete
// message. FinishReason is empty on every fragment but the last one or two
// for a choice (OpenRouter's own captures show it repeated, once with no
// usage and again on the chunk that carries usage).
type openRouterStreamChoice struct {
	Index int `json:"index"`
	Delta struct {
		Role      string                          `json:"role"`
		Content   openRouterContent               `json:"content"`
		ToolCalls []openRouterStreamToolCallDelta `json:"tool_calls"`
	} `json:"delta"`
	FinishReason string `json:"finish_reason"`
}

// openRouterStreamChunk is one `data: {...}` line of a streamed response -
// see openRouterChatCompletion's doc comment for the accumulation this
// feeds. Usage is a pointer (unlike openRouterChatResponse.Usage's plain
// value) because its presence, not its zero value, is what marks the chunk
// that carries the reply's final cost accounting; every other chunk omits
// the field entirely.
type openRouterStreamChunk struct {
	ID      string                   `json:"id"`
	Model   string                   `json:"model"`
	Choices []openRouterStreamChoice `json:"choices"`
	Usage   *openRouterUsage         `json:"usage"`
	Error   *openRouterAPIError      `json:"error"`
}

// openRouterStreamToolCallAccum accumulates one tool call's fields across
// however many delta fragments carried pieces of it - see
// openRouterChatCompletion.
type openRouterStreamToolCallAccum struct {
	id, callType, name string
	arguments          strings.Builder
}

// doOpenRouterRequestBody builds a POST to url carrying body with the
// standard OpenRouter headers (Authorization bearer, Content-Type,
// HTTP-Referer, X-Title) and executes it through doer - the
// header/request-building half shared by every OpenRouter caller in this
// file: doOpenRouterRequest below (chat completions, both the streaming and
// non-streaming callers) and openRouterEmbeddings. The caller owns closing
// resp.Body.
func doOpenRouterRequestBody(ctx context.Context, doer openRouterDoer, apiKey, url string, body []byte) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build openrouter request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("HTTP-Referer", "https://github.com/gterrill/helmcentral")
	httpReq.Header.Set("X-Title", "Helmcentral")

	resp, err := doer.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openrouter request failed: %w", err)
	}
	return resp, nil
}

// doOpenRouterRequest marshals req and posts it to openRouterChatCompletionsURL
// via doOpenRouterRequestBody - the request-building half shared by
// openRouterChatCompletion and openRouterChatCompletionOnce, so the two
// differ only in how they read the response (SSE stream vs. one JSON body).
// The caller owns closing resp.Body.
func doOpenRouterRequest(ctx context.Context, doer openRouterDoer, apiKey string, req openRouterChatRequest) (*http.Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal openrouter request: %w", err)
	}
	return doOpenRouterRequestBody(ctx, doer, apiKey, openRouterChatCompletionsURL, body)
}

// openRouterNonSuccessError reads resp's body (a non-2xx status is never a
// stream, even from openRouterChatCompletion's own SSE call) and reports it
// as a single-line error: the upstream's own error.message when the body
// parses as openRouterChatResponse and carries one, otherwise the raw body
// text - shared by openRouterChatCompletion and openRouterChatCompletionOnce
// so a caller sees the identical error shape from either. Closing resp.Body
// stays the caller's job (both callers already defer it before checking the
// status).
func openRouterNonSuccessError(resp *http.Response) error {
	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return fmt.Errorf("read openrouter response: %w", readErr)
	}
	msg := strings.TrimSpace(string(respBody))
	var parsed openRouterChatResponse
	if err := json.Unmarshal(respBody, &parsed); err == nil && parsed.Error != nil && parsed.Error.Message != "" {
		msg = parsed.Error.Message
	}
	return fmt.Errorf("openrouter status %d: %s", resp.StatusCode, firstErrorLine(errors.New(msg)))
}

// openRouterChatCompletionOnce posts one chat-completion request with
// stream forced false and decodes a single JSON response body, instead of
// openRouterChatCompletion's SSE accumulation. B4's document enrichment
// needs this: OpenRouter only returns the file-parser plugin's OCR text
// (message.annotations) on a non-streaming response, never over SSE (see
// this file's package doc / ADR 0106's "OCR must not stream" constraint).
// It shares request-building (doOpenRouterRequest) and non-2xx/embedded-error
// handling (openRouterNonSuccessError) with the streaming call, so a 401, a
// malformed key, or an upstream error.message surfaces identically either
// way.
func openRouterChatCompletionOnce(ctx context.Context, doer openRouterDoer, apiKey string, req openRouterChatRequest) (openRouterChatResponse, error) {
	req.Stream = false

	resp, err := doOpenRouterRequest(ctx, doer, apiKey, req)
	if err != nil {
		return openRouterChatResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return openRouterChatResponse{}, openRouterNonSuccessError(resp)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return openRouterChatResponse{}, fmt.Errorf("read openrouter response: %w", err)
	}

	var parsed openRouterChatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return openRouterChatResponse{}, fmt.Errorf("parse openrouter response: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return openRouterChatResponse{}, fmt.Errorf("openrouter error: %s", firstErrorLine(errors.New(parsed.Error.Message)))
	}
	if len(parsed.Choices) == 0 {
		return openRouterChatResponse{}, fmt.Errorf("openrouter returned no choices (status %d)", resp.StatusCode)
	}

	return parsed, nil
}

// openRouterChatCompletion posts one chat-completion request and returns
// the fully assembled response, exactly the same openRouterChatResponse
// shape callers read today (ADR 0093 §3's tolerant openRouterContent/
// openRouterArguments decoding and the runner's cost-summing both stay
// unchanged) - built here from OpenRouter's server-sent-events stream
// instead of one JSON body, since req.Stream is always forced true
// (Helmcentral's "token-by-token streaming... deliberately out of scope"
// note in ADR 0093 §5 is what this function retires).
//
// onContent is called once per non-empty content fragment, in the order
// OpenRouter sent them, so a caller (assistant_run.go's run) can forward
// partial text to the operator as it arrives; it is never called for a
// tool-call-only fragment or an empty content string. Pass a no-op func if
// a caller has nothing to do with partial text.
//
// The body is read with a bufio.Reader over whole lines
// (reader.ReadString('\n')), never bufio.Scanner: Scanner's default token
// buffer caps a single line at 64KB, and one line here is one whole SSE
// event - a tool call's arguments alone can exceed that (see
// TestOpenRouterChatCompletion_LongArgumentsLineDecodes).
//
// Four failure shapes surface as a single-line error, mirroring how
// place_name.go and wasm_plugin.go trim upstream errors for a status line
// rather than a stack trace: a non-2xx status (body read as plain JSON, not
// a stream - OpenRouter never streams an error response), a chunk carrying
// its own top-level error object (some OpenRouter failure modes report
// this way mid-stream instead of a non-2xx status or even instead of
// closing the connection), the stream ending (EOF) with neither a `data:
// [DONE]` line nor any finish_reason ever seen (a cut connection, not a
// completed answer), and a completed stream that never carried a single
// choice (nothing to reply with, as today's non-streaming zero-choices
// check). ctx cancellation surfaces through the ordinary read-error path:
// a *http.Client ties body reads to the request's context, so a read
// failing because ctx was cancelled comes back wrapping ctx.Err(), and
// errors.Is(err, context.Canceled) still holds for a caller checking it.
func openRouterChatCompletion(ctx context.Context, doer openRouterDoer, apiKey string, req openRouterChatRequest, onContent func(string)) (openRouterChatResponse, error) {
	req.Stream = true

	resp, err := doOpenRouterRequest(ctx, doer, apiKey, req)
	if err != nil {
		return openRouterChatResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return openRouterChatResponse{}, openRouterNonSuccessError(resp)
	}

	var (
		id, model, role string
		content         strings.Builder
		finishReason    string
		usage           openRouterUsage
		sawChoice       bool
		sawDone         bool
		toolOrder       []int
		toolCalls       = map[int]*openRouterStreamToolCallAccum{}
	)

	reader := bufio.NewReader(resp.Body)
readLoop:
	for {
		line, readErr := reader.ReadString('\n')
		trimmed := strings.TrimRight(line, "\r\n")

		switch {
		case trimmed == "":
			// The blank line separating SSE events - nothing to do.
		case strings.HasPrefix(trimmed, ":"):
			// A comment line, e.g. OpenRouter's ": OPENROUTER PROCESSING"
			// keepalive - not an event.
		case strings.HasPrefix(trimmed, "data:"):
			payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
			if payload == "[DONE]" {
				sawDone = true
				break readLoop
			}

			var chunk openRouterStreamChunk
			if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
				return openRouterChatResponse{}, fmt.Errorf("parse openrouter stream chunk: %w", err)
			}
			if chunk.Error != nil {
				return openRouterChatResponse{}, fmt.Errorf("openrouter error: %s", firstErrorLine(errors.New(chunk.Error.Message)))
			}
			if chunk.ID != "" {
				id = chunk.ID
			}
			if chunk.Model != "" {
				model = chunk.Model
			}
			if chunk.Usage != nil {
				usage = *chunk.Usage
			}
			if len(chunk.Choices) > 0 {
				sawChoice = true
				choice := chunk.Choices[0]
				if choice.Delta.Role != "" {
					role = choice.Delta.Role
				}
				if fragment := string(choice.Delta.Content); fragment != "" {
					content.WriteString(fragment)
					onContent(fragment)
				}
				for _, tc := range choice.Delta.ToolCalls {
					acc, ok := toolCalls[tc.Index]
					if !ok {
						acc = &openRouterStreamToolCallAccum{}
						toolCalls[tc.Index] = acc
						toolOrder = append(toolOrder, tc.Index)
					}
					if tc.ID != "" {
						acc.id = tc.ID
					}
					if tc.Type != "" {
						acc.callType = tc.Type
					}
					if tc.Function.Name != "" {
						acc.name = tc.Function.Name
					}
					acc.arguments.WriteString(string(tc.Function.Arguments))
				}
				if choice.FinishReason != "" {
					finishReason = choice.FinishReason
				}
			}
		}

		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break readLoop
			}
			return openRouterChatResponse{}, fmt.Errorf("read openrouter stream: %w", readErr)
		}
	}

	if !sawDone && finishReason == "" {
		return openRouterChatResponse{}, errors.New("openrouter stream ended early")
	}
	if !sawChoice {
		return openRouterChatResponse{}, fmt.Errorf("openrouter returned no choices (status %d)", resp.StatusCode)
	}

	if role == "" {
		role = "assistant"
	}
	msg := openRouterMessage{Role: role, Content: openRouterContent(content.String())}
	if len(toolOrder) > 0 {
		sort.Ints(toolOrder)
		calls := make([]openRouterToolCall, 0, len(toolOrder))
		for _, idx := range toolOrder {
			acc := toolCalls[idx]
			calls = append(calls, openRouterToolCall{
				ID:   acc.id,
				Type: acc.callType,
				Function: openRouterToolCallFunction{
					Name:      acc.name,
					Arguments: openRouterArguments(acc.arguments.String()),
				},
			})
		}
		msg.ToolCalls = calls
	}

	return openRouterChatResponse{
		ID:    id,
		Model: model,
		Choices: []openRouterChoice{{
			Index:        0,
			Message:      msg,
			FinishReason: finishReason,
		}},
		Usage: usage,
	}, nil
}

// ── embeddings (E1b: document semantic search) ────────────────────────────

// openRouterEmbeddingsRequest is the request body for POST
// /api/v1/embeddings. Dimensions is honoured by OpenAI's text-embedding-3
// family via Matryoshka representation learning (a shorter prefix of the
// native vector is still a usable embedding) but is omitted from the wire
// entirely when zero, the same "let the model use its native size" meaning
// omitting Model's own value would have.
type openRouterEmbeddingsRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions,omitempty"`
}

// openRouterEmbeddingData is one vector inside an embeddings response.
// Index is the position of the corresponding Input string in the request -
// see openRouterEmbeddings's own doc comment for why the response is placed
// by this field rather than trusted in array order.
type openRouterEmbeddingData struct {
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

// openRouterEmbeddingsResponse is the response body for POST
// /api/v1/embeddings (backend/testdata/openrouter_embeddings.json is a real
// capture, not an assumed shape). Usage.Cost is present here without the
// usage.include=true option chat completions needs (openRouterUsageOption) -
// verified live against OpenRouter on 2026-09-18.
type openRouterEmbeddingsResponse struct {
	ID       string                    `json:"id"`
	Object   string                    `json:"object"`
	Model    string                    `json:"model"`
	Provider string                    `json:"provider"`
	Data     []openRouterEmbeddingData `json:"data"`
	Usage    openRouterUsage           `json:"usage"`
	Error    *openRouterAPIError       `json:"error,omitempty"`
}

// openRouterEmbeddings posts one embeddings request and returns its vectors
// placed at the position each one's own Index field names, not the order
// they arrived on the wire: OpenRouter documents index as the way to match
// a vector back to its input, and array order is not a promised part of the
// contract. It shares request-building (doOpenRouterRequestBody) and
// non-2xx/embedded-error handling (openRouterNonSuccessError) with the chat-
// completions calls above, so a 401, a bad key or an upstream error.message
// surfaces identically.
//
// A caller that gets a nil error back can trust the result holds exactly
// len(req.Input) vectors, one per input position, all the same length.
// Per AGENTS.md's fallback policy, every one of the following is a fail-fast
// error rather than a best-effort result: an empty or over-sized Input (the
// request is never sent - maxOpenRouterEmbeddingsBatch's own doc comment
// explains why batching is the caller's job, not this function's), an
// embedded error object in an otherwise-200 body, a vector count that does
// not match len(req.Input), a vector whose Index is out of range or repeated,
// an empty embedding, embeddings of differing lengths within one response,
// and a length that differs from a non-zero req.Dimensions.
func openRouterEmbeddings(ctx context.Context, doer openRouterDoer, apiKey string, req openRouterEmbeddingsRequest) (openRouterEmbeddingsResponse, error) {
	if len(req.Input) == 0 {
		return openRouterEmbeddingsResponse{}, errors.New("openrouter embeddings: empty input")
	}
	if len(req.Input) > maxOpenRouterEmbeddingsBatch {
		return openRouterEmbeddingsResponse{}, fmt.Errorf("openrouter embeddings: %d inputs exceeds the %d-input batch limit", len(req.Input), maxOpenRouterEmbeddingsBatch)
	}

	body, err := json.Marshal(req)
	if err != nil {
		return openRouterEmbeddingsResponse{}, fmt.Errorf("marshal openrouter embeddings request: %w", err)
	}

	resp, err := doOpenRouterRequestBody(ctx, doer, apiKey, openRouterEmbeddingsURL, body)
	if err != nil {
		return openRouterEmbeddingsResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return openRouterEmbeddingsResponse{}, openRouterNonSuccessError(resp)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return openRouterEmbeddingsResponse{}, fmt.Errorf("read openrouter embeddings response: %w", err)
	}

	var parsed openRouterEmbeddingsResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return openRouterEmbeddingsResponse{}, fmt.Errorf("parse openrouter embeddings response: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return openRouterEmbeddingsResponse{}, fmt.Errorf("openrouter error: %s", firstErrorLine(errors.New(parsed.Error.Message)))
	}
	if len(parsed.Data) != len(req.Input) {
		return openRouterEmbeddingsResponse{}, fmt.Errorf("openrouter embeddings: expected %d vectors, got %d", len(req.Input), len(parsed.Data))
	}

	ordered := make([]openRouterEmbeddingData, len(parsed.Data))
	seen := make([]bool, len(parsed.Data))
	dims := 0
	for _, d := range parsed.Data {
		if d.Index < 0 || d.Index >= len(ordered) {
			return openRouterEmbeddingsResponse{}, fmt.Errorf("openrouter embeddings: vector index %d out of range for %d inputs", d.Index, len(req.Input))
		}
		if seen[d.Index] {
			return openRouterEmbeddingsResponse{}, fmt.Errorf("openrouter embeddings: duplicate vector index %d", d.Index)
		}
		seen[d.Index] = true
		if len(d.Embedding) == 0 {
			return openRouterEmbeddingsResponse{}, fmt.Errorf("openrouter embeddings: empty embedding at index %d", d.Index)
		}
		if dims == 0 {
			dims = len(d.Embedding)
		} else if len(d.Embedding) != dims {
			return openRouterEmbeddingsResponse{}, fmt.Errorf("openrouter embeddings: inconsistent embedding length at index %d (%d, expected %d)", d.Index, len(d.Embedding), dims)
		}
		ordered[d.Index] = d
	}
	if req.Dimensions > 0 && dims != req.Dimensions {
		return openRouterEmbeddingsResponse{}, fmt.Errorf("openrouter embeddings: requested %d dimensions, got %d", req.Dimensions, dims)
	}

	parsed.Data = ordered
	return parsed, nil
}
