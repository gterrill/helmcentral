package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
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

// noopOnContent is what every test below passes when it has nothing of its
// own to do with streamed fragments - the same role a discard writer plays
// elsewhere.
func noopOnContent(string) {}

// collectingOnContent returns an onContent func that appends every fragment
// it is called with, in call order, to the returned slice - for tests that
// need to assert on the fragments themselves (order, count, or that a
// held-back marker never reached it).
func collectingOnContent() (func(string), *[]string) {
	var got []string
	return func(fragment string) { got = append(got, fragment) }, &got
}

// ctxErrDoer simulates a transport that observes the request's context has
// already been cancelled, the way a real *http.Client would.
type ctxErrDoer struct{}

func (ctxErrDoer) Do(req *http.Request) (*http.Response, error) {
	return nil, req.Context().Err()
}

// ctxAwareBody is an io.ReadCloser whose Read blocks until ctx is done and
// then returns ctx.Err() - a stand-in for a real *http.Client body read
// that observes the request's context being cancelled mid-stream (a real
// transport ties body reads to the request's context the same way).
// started is closed the first time Read is entered, so a test can wait for
// the read to have actually begun before cancelling ctx.
type ctxAwareBody struct {
	ctx     context.Context
	started chan struct{}
	once    sync.Once
}

func (b *ctxAwareBody) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.started) })
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *ctxAwareBody) Close() error { return nil }

// ctxAwareBodyDoer returns a 200 response whose body blocks on ctx (via
// ctxAwareBody) instead of a fake queued response - used to prove
// openRouterChatCompletion's stream read surfaces a mid-body context
// cancellation as an error satisfying errors.Is(err, context.Canceled).
type ctxAwareBodyDoer struct {
	body *ctxAwareBody
}

func (d *ctxAwareBodyDoer) Do(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: d.body, Header: make(http.Header)}, nil
}

func TestOpenRouterChatCompletion_SendsExpectedRequestShape(t *testing.T) {
	body := `data: {"id":"gen-1","model":"anthropic/claude-sonnet-4.5","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"cost":0.0184}}` + "\n\n" + `data: [DONE]` + "\n\n"
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, body)}}

	req := openRouterChatRequest{
		Model:    "anthropic/claude-sonnet-4.5",
		Messages: []openRouterMessage{{Role: "user", Content: "hello"}},
		Tools: []openRouterTool{
			{Type: "function", Function: openRouterFunctionDef{Name: "find_places"}},
		},
		Usage: &openRouterUsageOption{Include: true},
	}

	onContent, fragments := collectingOnContent()
	resp, err := openRouterChatCompletion(context.Background(), doer, "sk-test", req, onContent)
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

	reqBody := string(doer.bodies[0])
	if !strings.Contains(reqBody, `"usage":{"include":true}`) {
		t.Fatalf("expected request body to carry usage.include=true, got %s", reqBody)
	}
	if !strings.Contains(reqBody, `"tools"`) {
		t.Fatalf("expected request body to carry tools when set, got %s", reqBody)
	}
	if !strings.Contains(reqBody, `"stream":true`) {
		t.Fatalf("expected the request to always set stream:true, got %s", reqBody)
	}

	if resp.Choices[0].Message.Content != "hi" {
		t.Fatalf("unexpected content: %q", resp.Choices[0].Message.Content)
	}
	if resp.Usage.Cost != 0.0184 {
		t.Fatalf("expected usage.cost to decode, got %v", resp.Usage.Cost)
	}
	if len(*fragments) != 1 || (*fragments)[0] != "hi" {
		t.Fatalf("expected onContent called once with the single fragment, got %+v", *fragments)
	}
}

func TestOpenRouterChatCompletion_OmitsToolsAndUsageWhenUnset(t *testing.T) {
	body := `data: {"id":"gen-1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}` + "\n\n" + `data: [DONE]` + "\n\n"
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, body)}}

	req := openRouterChatRequest{Model: "m", Messages: []openRouterMessage{{Role: "user", Content: "hi"}}}
	if _, err := openRouterChatCompletion(context.Background(), doer, "sk", req, noopOnContent); err != nil {
		t.Fatalf("openRouterChatCompletion: %v", err)
	}

	reqBody := string(doer.bodies[0])
	if strings.Contains(reqBody, `"tools"`) {
		t.Fatalf("expected no tools key when unset, got %s", reqBody)
	}
	if strings.Contains(reqBody, `"usage"`) {
		t.Fatalf("expected no usage key when unset, got %s", reqBody)
	}
}

func TestOpenRouterChatCompletion_DecodesToolCallsWithStringArguments(t *testing.T) {
	body := `data: {"id":"gen-1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_tides","arguments":"{\"lat\":-20.5}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n" + `data: [DONE]` + "\n\n"
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, body)}}

	resp, err := openRouterChatCompletion(context.Background(), doer, "sk", openRouterChatRequest{Model: "m"}, noopOnContent)
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
	body := `data: {"id":"gen-1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":[{"type":"text","text":"Tongue Bay "},{"type":"text","text":"looks good."}]},"finish_reason":"stop"}]}` + "\n\n" + `data: [DONE]` + "\n\n"
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, body)}}

	resp, err := openRouterChatCompletion(context.Background(), doer, "sk", openRouterChatRequest{Model: "m"}, noopOnContent)
	if err != nil {
		t.Fatalf("openRouterChatCompletion: %v", err)
	}
	if resp.Choices[0].Message.Content != "Tongue Bay looks good." {
		t.Fatalf("expected joined text parts, got %q", resp.Choices[0].Message.Content)
	}
}

func TestOpenRouterChatCompletion_DecodesObjectArgumentsToCompactString(t *testing.T) {
	body := `data: {"id":"gen-1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_tides","arguments":{"lat":-20.5,"lon":149.1}}}]},"finish_reason":"tool_calls"}]}` + "\n\n" + `data: [DONE]` + "\n\n"
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, body)}}

	resp, err := openRouterChatCompletion(context.Background(), doer, "sk", openRouterChatRequest{Model: "m"}, noopOnContent)
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

	_, err := openRouterChatCompletion(context.Background(), doer, "sk", openRouterChatRequest{Model: "m"}, noopOnContent)
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

// TestOpenRouterChatCompletion_MidStreamErrorChunkIsAnError proves a chunk
// carrying its own "error" object - one of the shapes OpenRouter uses to
// report a failure without ever sending a non-2xx status, and without
// closing the stream first (openrouter_client.go's doc comment) - ends the
// call with an error, even after a legitimate content fragment has already
// streamed through onContent. No [DONE] ever arrives in this fixture; the
// error return happens before the reader would have noticed that.
func TestOpenRouterChatCompletion_MidStreamErrorChunkIsAnError(t *testing.T) {
	body := `data: {"id":"gen-1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"Working on it"},"finish_reason":null}]}` + "\n\n" +
		`data: {"error":{"message":"model not found"}}` + "\n\n"
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, body)}}

	onContent, fragments := collectingOnContent()
	_, err := openRouterChatCompletion(context.Background(), doer, "sk", openRouterChatRequest{Model: "m"}, onContent)
	if err == nil {
		t.Fatalf("expected an error when a stream chunk carries its own error object")
	}
	if !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("expected error to contain the upstream message, got %q", err.Error())
	}
	if len(*fragments) != 1 || (*fragments)[0] != "Working on it" {
		t.Fatalf("expected the fragment that streamed before the error to have reached onContent, got %+v", *fragments)
	}
}

// TestOpenRouterChatCompletion_EOFBeforeDoneIsAnError proves a connection
// that ends (EOF) without ever sending "data: [DONE]" or a finish_reason -
// a cut connection, not a completed answer - is reported as an error
// rather than treated as a (truncated) success.
func TestOpenRouterChatCompletion_EOFBeforeDoneIsAnError(t *testing.T) {
	body := `data: {"id":"gen-1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"Tongue Bay"},"finish_reason":null}]}` + "\n\n"
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, body)}}

	_, err := openRouterChatCompletion(context.Background(), doer, "sk", openRouterChatRequest{Model: "m"}, noopOnContent)
	if err == nil {
		t.Fatalf("expected an error when the stream ends before [DONE] or a finish_reason")
	}
	if !strings.Contains(err.Error(), "ended early") {
		t.Fatalf("expected the error to say the stream ended early, got %q", err.Error())
	}
}

func TestOpenRouterChatCompletion_ZeroChoicesIsAnError(t *testing.T) {
	body := `data: [DONE]` + "\n\n"
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, body)}}

	_, err := openRouterChatCompletion(context.Background(), doer, "sk", openRouterChatRequest{Model: "m"}, noopOnContent)
	if err == nil {
		t.Fatalf("expected an error for zero choices")
	}
	if !strings.Contains(err.Error(), "no choices") {
		t.Fatalf("expected the error to say no choices, got %q", err.Error())
	}
}

func TestOpenRouterChatCompletion_PropagatesContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := openRouterChatCompletion(ctx, ctxErrDoer{}, "sk", openRouterChatRequest{Model: "m"}, noopOnContent)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled to propagate, got %v", err)
	}
}

// TestOpenRouterChatCompletion_ContextCancelledMidBodyIsAnError proves a
// context that is cancelled while the stream's body is being read (as
// opposed to before the request is even sent, the case above) surfaces
// through the ordinary read-error path with errors.Is(err, context.
// Canceled) still true - openrouter_client.go's doc comment describes why
// a real *http.Client's body reads behave this way; ctxAwareBody stands in
// for that behaviour without a real network round trip.
func TestOpenRouterChatCompletion_ContextCancelledMidBodyIsAnError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	body := &ctxAwareBody{ctx: ctx, started: make(chan struct{})}
	doer := &ctxAwareBodyDoer{body: body}

	go func() {
		<-body.started
		cancel()
	}()

	_, err := openRouterChatCompletion(ctx, doer, "sk", openRouterChatRequest{Model: "m"}, noopOnContent)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled to propagate from a mid-body cancellation, got %v", err)
	}
}

// TestOpenRouterChatCompletion_LongArgumentsLineDecodes proves the stream
// is read with a bufio.Reader over whole lines, not bufio.Scanner (whose
// default token buffer caps a single line at 64KB) - one SSE "data: ..."
// line is one whole chunk here, and a tool call's arguments alone can
// exceed that comfortably in a real conversation (a long list of
// candidates, a big forecast echoed back, and so on).
func TestOpenRouterChatCompletion_LongArgumentsLineDecodes(t *testing.T) {
	long := strings.Repeat("a", 200_000)
	argsJSON, err := json.Marshal(map[string]string{"query": long})
	if err != nil {
		t.Fatalf("marshal long arguments: %v", err)
	}
	argsEncoded, err := json.Marshal(string(argsJSON))
	if err != nil {
		t.Fatalf("marshal arguments as a JSON string: %v", err)
	}

	chunk := fmt.Sprintf(
		`{"id":"gen-1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"find_places","arguments":%s}}]},"finish_reason":"tool_calls"}]}`,
		argsEncoded,
	)
	body := "data: " + chunk + "\n\n" + "data: [DONE]\n\n"
	if len(body) < 64*1024 {
		t.Fatalf("test setup bug: body (%d bytes) must exceed bufio.Scanner's 64KB token limit to prove anything", len(body))
	}
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, body)}}

	resp, err := openRouterChatCompletion(context.Background(), doer, "sk", openRouterChatRequest{Model: "m"}, noopOnContent)
	if err != nil {
		t.Fatalf("openRouterChatCompletion: %v", err)
	}
	got := resp.Choices[0].Message.ToolCalls[0].Function.Arguments
	if string(got) != string(argsJSON) {
		t.Fatalf("expected the 200KB arguments to decode intact (got %d bytes, want %d)", len(got), len(argsJSON))
	}
}

// TestOpenRouterChatCompletion_OnContentCalledInOrder proves onContent is
// invoked once per content fragment, in the order OpenRouter sent them, and
// that the fragments concatenate to the final assembled content.
func TestOpenRouterChatCompletion_OnContentCalledInOrder(t *testing.T) {
	body := strings.Join([]string{
		`data: {"id":"gen-1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"Ton"},"finish_reason":null}]}`,
		`data: {"id":"gen-1","model":"m","choices":[{"index":0,"delta":{"content":"gue Bay "},"finish_reason":null}]}`,
		`data: {"id":"gen-1","model":"m","choices":[{"index":0,"delta":{"content":"looks good."},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
	}, "\n\n") + "\n\n"
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, body)}}

	onContent, fragments := collectingOnContent()
	resp, err := openRouterChatCompletion(context.Background(), doer, "sk", openRouterChatRequest{Model: "m"}, onContent)
	if err != nil {
		t.Fatalf("openRouterChatCompletion: %v", err)
	}

	want := []string{"Ton", "gue Bay ", "looks good."}
	if len(*fragments) != len(want) {
		t.Fatalf("expected %d fragments, got %d: %+v", len(want), len(*fragments), *fragments)
	}
	for i, f := range want {
		if (*fragments)[i] != f {
			t.Fatalf("fragment %d: got %q, want %q", i, (*fragments)[i], f)
		}
	}
	if resp.Choices[0].Message.Content != "Tongue Bay looks good." {
		t.Fatalf("expected the fragments to concatenate to the final content, got %q", resp.Choices[0].Message.Content)
	}
}

// TestOpenRouterChatCompletion_CommentLinesAreIgnored proves ':' comment
// lines (OpenRouter sends ": OPENROUTER PROCESSING" as a keepalive while it
// is still routing the request) are skipped rather than treated as - or
// breaking the decode of - a real event.
func TestOpenRouterChatCompletion_CommentLinesAreIgnored(t *testing.T) {
	body := ": OPENROUTER PROCESSING\n\n" +
		": OPENROUTER PROCESSING\n\n" +
		`data: {"id":"gen-1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"Paris."},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"cost":0.001}}` + "\n\n" +
		"data: [DONE]\n\n"
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, body)}}

	onContent, fragments := collectingOnContent()
	resp, err := openRouterChatCompletion(context.Background(), doer, "sk", openRouterChatRequest{Model: "m"}, onContent)
	if err != nil {
		t.Fatalf("openRouterChatCompletion: %v", err)
	}
	if resp.Choices[0].Message.Content != "Paris." {
		t.Fatalf("unexpected content: %q", resp.Choices[0].Message.Content)
	}
	if len(*fragments) != 1 || (*fragments)[0] != "Paris." {
		t.Fatalf("expected the comment lines to produce no fragments of their own, got %+v", *fragments)
	}
}

// TestOpenRouterChatCompletion_TextFixtureDecodesLiveCapturedShape decodes
// backend/testdata/openrouter_stream_text.txt, a real streamed OpenRouter
// response body captured live against the configured model
// (openrouter_stream_fixtures_live_test.go,
// [[feedback_verify_fixtures_against_live_data]]) rather than an assumed
// shape - it carries fields (reasoning, reasoning_details, provider,
// service_tier, native_finish_reason...) this decoder deliberately ignores,
// and repeats content:"" and finish_reason:"stop" across more than one
// chunk, both of which an assumed fixture would be unlikely to include.
func TestOpenRouterChatCompletion_TextFixtureDecodesLiveCapturedShape(t *testing.T) {
	body, err := os.ReadFile("testdata/openrouter_stream_text.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, string(body))}}

	onContent, fragments := collectingOnContent()
	resp, err := openRouterChatCompletion(context.Background(), doer, "sk", openRouterChatRequest{Model: "m"}, onContent)
	if err != nil {
		t.Fatalf("openRouterChatCompletion: %v", err)
	}

	const wantContent = "The capital of France is Paris."
	if resp.Choices[0].Message.Content != wantContent {
		t.Fatalf("unexpected content: %q", resp.Choices[0].Message.Content)
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Fatalf("expected finish_reason stop, got %q", resp.Choices[0].FinishReason)
	}
	if resp.Model != "deepseek/deepseek-v4-flash-0731" {
		t.Fatalf("unexpected model: %q", resp.Model)
	}
	if resp.Usage.PromptTokens != 49 || resp.Usage.CompletionTokens != 53 || resp.Usage.TotalTokens != 102 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}
	if resp.Usage.Cost != 0.00001815 {
		t.Fatalf("unexpected cost: %v", resp.Usage.Cost)
	}
	if len(resp.Choices[0].Message.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %+v", resp.Choices[0].Message.ToolCalls)
	}

	joined := strings.Join(*fragments, "")
	if joined != wantContent {
		t.Fatalf("expected onContent's fragments to concatenate to the final content, got %q", joined)
	}
}

// TestOpenRouterChatCompletion_ToolCallFixtureDecodesLiveCapturedShape
// decodes backend/testdata/openrouter_stream_toolcall.txt, a real streamed
// tool-call round captured live - see the text fixture test's doc comment.
// This fixture in particular proves the accumulation logic against real
// upstream quirks a hand-written fixture would be unlikely to guess: the
// id/type/function.name fields arrive on the tool call's first fragment
// only, content is explicitly null (not omitted) on every delta here, and
// finish_reason:"tool_calls" is repeated on the final two chunks the same
// way the text fixture repeats "stop".
func TestOpenRouterChatCompletion_ToolCallFixtureDecodesLiveCapturedShape(t *testing.T) {
	body, err := os.ReadFile("testdata/openrouter_stream_toolcall.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, string(body))}}

	resp, err := openRouterChatCompletion(context.Background(), doer, "sk", openRouterChatRequest{Model: "m"}, noopOnContent)
	if err != nil {
		t.Fatalf("openRouterChatCompletion: %v", err)
	}

	if resp.Choices[0].Message.Content != "" {
		t.Fatalf("expected empty content for a tool-call-only reply, got %q", resp.Choices[0].Message.Content)
	}
	if resp.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("expected finish_reason tool_calls, got %q", resp.Choices[0].FinishReason)
	}
	if resp.Model != "openai/gpt-5.6-luna" {
		t.Fatalf("unexpected model: %q", resp.Model)
	}

	calls := resp.Choices[0].Message.ToolCalls
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 tool call, got %d: %+v", len(calls), calls)
	}
	call := calls[0]
	if call.ID != "call_DNXqXxJuMQTRpJXNZT8xHqC7" {
		t.Fatalf("unexpected tool call id: %q", call.ID)
	}
	if call.Type != "function" {
		t.Fatalf("unexpected tool call type: %q", call.Type)
	}
	if call.Function.Name != "get_current_time" {
		t.Fatalf("unexpected tool call name: %q", call.Function.Name)
	}
	if call.Function.Arguments != `{"timezone":"UTC"}` {
		t.Fatalf("expected the fragmented arguments to concatenate to valid JSON, got %q", call.Function.Arguments)
	}

	if resp.Usage.PromptTokens != 99 || resp.Usage.CompletionTokens != 19 || resp.Usage.TotalTokens != 118 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}
	if resp.Usage.Cost != 0.0000426 {
		t.Fatalf("unexpected cost: %v", resp.Usage.Cost)
	}
}

func TestOpenRouterHTTPClient_SlowBodyWithinDeadlineSucceeds(t *testing.T) {
	origHeaderTimeout := openRouterResponseHeaderTimeout
	origCompletionTimeout := openRouterCompletionTimeout
	openRouterResponseHeaderTimeout = 50 * time.Millisecond
	openRouterCompletionTimeout = 2 * time.Second
	t.Cleanup(func() {
		openRouterResponseHeaderTimeout = origHeaderTimeout
		openRouterCompletionTimeout = origCompletionTimeout
	})

	body := `{"id":"gen-1","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"slow but complete"}}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Longer than the header timeout above, well inside the completion
		// deadline below - proving a slow BODY, once headers have already
		// arrived, is not what ResponseHeaderTimeout bounds.
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	client := newOpenRouterHTTPClient()

	ctx, cancel := context.WithTimeout(context.Background(), openRouterCompletionTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("expected the slow-body response to succeed once headers arrive fast, got: %v", err)
	}
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(got) != body {
		t.Fatalf("unexpected body: %s", got)
	}
}

// ── openRouterChatCompletionOnce (B4: non-streaming, for OCR annotations) ──

func TestOpenRouterChatCompletionOnce_SendsStreamFalseAndDecodesBody(t *testing.T) {
	body := `{"id":"gen-1","model":"google/gemini-2.5-flash","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"hi"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"cost":0.001}}`
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, body)}}

	req := openRouterChatRequest{Model: "google/gemini-2.5-flash", Messages: []openRouterMessage{{Role: "user", Content: "hello"}}}
	resp, err := openRouterChatCompletionOnce(context.Background(), doer, "sk-test", req)
	if err != nil {
		t.Fatalf("openRouterChatCompletionOnce: %v", err)
	}

	if len(doer.requests) != 1 {
		t.Fatalf("expected exactly one request, got %d", len(doer.requests))
	}
	got := doer.requests[0]
	if auth := got.Header.Get("Authorization"); auth != "Bearer sk-test" {
		t.Fatalf("unexpected Authorization header: %q", auth)
	}
	reqBody := string(doer.bodies[0])
	if !strings.Contains(reqBody, `"stream":false`) {
		t.Fatalf("expected the request to always set stream:false, got %s", reqBody)
	}

	if resp.Choices[0].Message.Content != "hi" {
		t.Fatalf("unexpected content: %q", resp.Choices[0].Message.Content)
	}
	if resp.Usage.Cost != 0.001 {
		t.Fatalf("expected usage.cost to decode, got %v", resp.Usage.Cost)
	}
}

func TestOpenRouterChatCompletionOnce_401ReturnsUpstreamMessageAndStatus(t *testing.T) {
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(401, `{"error":{"message":"bad key"}}`)}}

	_, err := openRouterChatCompletionOnce(context.Background(), doer, "sk", openRouterChatRequest{Model: "m"})
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

// TestOpenRouterChatCompletionOnce_EmbeddedErrorObjectIsAnError proves a 200
// response whose body still carries a top-level "error" object (one of the
// failure shapes openRouterChatCompletion's own doc comment describes) is
// reported as an error rather than a reply with empty choices.
func TestOpenRouterChatCompletionOnce_EmbeddedErrorObjectIsAnError(t *testing.T) {
	body := `{"id":"gen-1","model":"m","choices":[],"error":{"message":"model not found"}}`
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, body)}}

	_, err := openRouterChatCompletionOnce(context.Background(), doer, "sk", openRouterChatRequest{Model: "m"})
	if err == nil {
		t.Fatalf("expected an error when the body carries its own error object")
	}
	if !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("expected error to contain the upstream message, got %q", err.Error())
	}
}

func TestOpenRouterChatCompletionOnce_ZeroChoicesIsAnError(t *testing.T) {
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, `{"id":"gen-1","model":"m","choices":[]}`)}}

	_, err := openRouterChatCompletionOnce(context.Background(), doer, "sk", openRouterChatRequest{Model: "m"})
	if err == nil {
		t.Fatalf("expected an error when the response carries no choices")
	}
}

// ── real OpenRouter captures (backend/testdata/openrouter_document_*.json) ─

func TestOpenRouterChatCompletionOnce_DecodesScannedPDFFixtureAnnotations(t *testing.T) {
	body, err := os.ReadFile("testdata/openrouter_document_pdf.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, string(body))}}

	resp, err := openRouterChatCompletionOnce(context.Background(), doer, "sk", openRouterChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("openRouterChatCompletionOnce: %v", err)
	}

	ann := resp.Choices[0].Message.Annotations
	if len(ann) != 1 {
		t.Fatalf("expected 1 annotation, got %d", len(ann))
	}
	if ann[0].Type != "file" {
		t.Fatalf("unexpected annotation type: %q", ann[0].Type)
	}
	if ann[0].File.Name != "scanned.pdf" {
		t.Fatalf("unexpected annotation file name: %q", ann[0].File.Name)
	}
	if ann[0].File.Hash == "" {
		t.Fatalf("expected a non-empty file hash")
	}
	parts := ann[0].File.Content
	if len(parts) != 3 {
		t.Fatalf("expected 3 content parts (open sentinel, page text, close sentinel), got %d", len(parts))
	}
	if parts[0].Text != `<file name="scanned.pdf">` {
		t.Fatalf("unexpected opening sentinel: %q", parts[0].Text)
	}
	if !strings.Contains(parts[1].Text, "WHITSUNDAY MARINE SUPPLIES") {
		t.Fatalf("expected the OCR'd page text, got %q", parts[1].Text)
	}
	if parts[2].Text != "</file>" {
		t.Fatalf("unexpected closing sentinel: %q", parts[2].Text)
	}
	if resp.Usage.Cost != 0.0022807 {
		t.Fatalf("unexpected cost: %v", resp.Usage.Cost)
	}
	if !strings.Contains(string(resp.Choices[0].Message.Content), "```json") {
		t.Fatalf("expected the fenced JSON suggestion reply, got %q", resp.Choices[0].Message.Content)
	}
}

func TestOpenRouterChatCompletionOnce_DecodesTwoPageScannedPDFFixtureOnePartPerPage(t *testing.T) {
	body, err := os.ReadFile("testdata/openrouter_document_pdf_2p.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, string(body))}}

	resp, err := openRouterChatCompletionOnce(context.Background(), doer, "sk", openRouterChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("openRouterChatCompletionOnce: %v", err)
	}

	parts := resp.Choices[0].Message.Annotations[0].File.Content
	if len(parts) != 4 {
		t.Fatalf("expected 4 content parts (open sentinel, 2 pages, close sentinel), got %d", len(parts))
	}
	if !strings.Contains(parts[1].Text, "impeller") {
		t.Fatalf("expected page 1 to mention impeller, got %q", parts[1].Text)
	}
	if !strings.Contains(parts[2].Text, "pump seal") {
		t.Fatalf("expected page 2 to mention pump seal, got %q", parts[2].Text)
	}
	if resp.Usage.Cost != 0.0046432 {
		t.Fatalf("unexpected cost: %v", resp.Usage.Cost)
	}
}

func TestOpenRouterChatCompletionOnce_DecodesImageFixtureNoAnnotations(t *testing.T) {
	body, err := os.ReadFile("testdata/openrouter_document_image.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, string(body))}}

	resp, err := openRouterChatCompletionOnce(context.Background(), doer, "sk", openRouterChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("openRouterChatCompletionOnce: %v", err)
	}
	if len(resp.Choices[0].Message.Annotations) != 0 {
		t.Fatalf("expected no annotations for an image reply, got %+v", resp.Choices[0].Message.Annotations)
	}
	if !strings.Contains(string(resp.Choices[0].Message.Content), `"text"`) {
		t.Fatalf("expected the reply's JSON to carry a text field, got %q", resp.Choices[0].Message.Content)
	}
	if resp.Usage.Cost != 0.0011695 {
		t.Fatalf("unexpected cost: %v", resp.Usage.Cost)
	}
}

// ── content blocks: file / image_url, text-only-for-text-blocks ───────────

func TestOpenRouterContentBlock_TextBlockMarshalUnchanged(t *testing.T) {
	data, err := json.Marshal(openRouterContentBlock{Type: "text", Text: "hello", CacheControl: &openRouterCacheControl{Type: "ephemeral"}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `{"type":"text","text":"hello","cache_control":{"type":"ephemeral"}}`
	if string(data) != want {
		t.Fatalf("expected byte-identical text block marshalling, got %s, want %s", data, want)
	}
}

func TestOpenRouterContentBlock_FileBlockOmitsTextKey(t *testing.T) {
	data, err := json.Marshal(openRouterContentBlock{
		Type: "file",
		File: &openRouterFileBlock{Filename: "x.pdf", FileData: "data:application/pdf;base64,AA=="},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := decoded["text"]; ok {
		t.Fatalf("expected no text key on a file block, got %s", data)
	}
	file, ok := decoded["file"].(map[string]any)
	if !ok {
		t.Fatalf("expected a file object, got %s", data)
	}
	if file["filename"] != "x.pdf" || file["file_data"] != "data:application/pdf;base64,AA==" {
		t.Fatalf("unexpected file block: %+v", file)
	}
}

func TestOpenRouterContentBlock_ImageURLBlockOmitsTextKey(t *testing.T) {
	data, err := json.Marshal(openRouterContentBlock{
		Type:     "image_url",
		ImageURL: &openRouterImageURLBlock{URL: "data:image/png;base64,AA=="},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := decoded["text"]; ok {
		t.Fatalf("expected no text key on an image_url block, got %s", data)
	}
	imageURL, ok := decoded["image_url"].(map[string]any)
	if !ok {
		t.Fatalf("expected an image_url object, got %s", data)
	}
	if imageURL["url"] != "data:image/png;base64,AA==" {
		t.Fatalf("unexpected image_url block: %+v", imageURL)
	}
}

func TestOpenRouterUserMessageFromBlocks_EncodesAsContentArray(t *testing.T) {
	msg := openRouterUserMessage(
		openRouterContentBlock{Type: "text", Text: "Describe this file."},
		openRouterContentBlock{Type: "file", File: &openRouterFileBlock{Filename: "x.pdf", FileData: "data:application/pdf;base64,AA=="}},
	)
	if msg.Role != "user" {
		t.Fatalf("expected role user, got %q", msg.Role)
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `{"role":"user","content":[{"type":"text","text":"Describe this file."},{"type":"file","file":{"filename":"x.pdf","file_data":"data:application/pdf;base64,AA=="}}]}`
	if string(data) != want {
		t.Fatalf("unexpected user message JSON:\ngot:  %s\nwant: %s", data, want)
	}
}

// ── plugin pdf.engine ───────────────────────────────────────────────────

func TestOpenRouterPlugin_PDFEngineMarshalsWhenSet(t *testing.T) {
	data, err := json.Marshal(openRouterPlugin{ID: "file-parser", PDF: &openRouterPluginPDF{Engine: "mistral-ocr"}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `{"id":"file-parser","pdf":{"engine":"mistral-ocr"}}`
	if string(data) != want {
		t.Fatalf("unexpected plugin JSON: got %s, want %s", data, want)
	}
}

func TestOpenRouterPlugin_PDFOmittedWhenNil(t *testing.T) {
	data, err := json.Marshal(openRouterPlugin{ID: "web"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "pdf") {
		t.Fatalf("expected no pdf key when unset, got %s", data)
	}
}

func TestOpenRouterHTTPClient_NoHeadersWithinHeaderTimeoutFailsClearly(t *testing.T) {
	origHeaderTimeout := openRouterResponseHeaderTimeout
	openRouterResponseHeaderTimeout = 50 * time.Millisecond
	t.Cleanup(func() { openRouterResponseHeaderTimeout = origHeaderTimeout })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Never responds within the header timeout below - a stand-in for a
		// dead upstream.
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newOpenRouterHTTPClient()

	// Generous relative to the header timeout, so a failure here is
	// unambiguously the header timeout firing, not this outer deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	_, err = client.Do(req)
	if err == nil {
		t.Fatal("expected a dead upstream to fail once the header timeout elapses")
	}
	if !strings.Contains(err.Error(), "timeout awaiting response headers") {
		t.Fatalf("expected a clear header-timeout error, got: %v", err)
	}
}
