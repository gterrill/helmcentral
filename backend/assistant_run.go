package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// This file drives the onboard assistant's agentic tool loop against
// OpenRouter (ADR 0093): repeatedly ask the model for the next step, answer
// any tool calls it makes, and stop once it returns plain text. assistant_
// handlers.go owns the SSE framing and persistence around one call to run;
// this file has no knowledge of HTTP or the conversation store.

// assistantMaxToolRounds bounds how many rounds of tool calls the loop will
// answer before withdrawing tools and forcing a final answer. Comparing two
// candidate anchorages (find_places, then wind and tides for each) is four
// calls; eight rounds leaves headroom for a model that checks a third
// option or retries, while still bounding a runaway loop's latency and
// OpenRouter spend.
const assistantMaxToolRounds = 8

// assistantMaxToolFailures bounds how many times one named tool may fail
// before the loop stops dispatching it for the rest of this run. Three
// strikes is not a magic number chosen for its own sake: a tool that has
// failed this many times in a row is not transiently flaky, it is down
// (Overpass unreachable, a plugin misconfigured, a provider timing out
// every call), and a model with assistantMaxToolRounds rounds to spend
// will burn the whole budget retrying it rather than answering with what
// it already has. That is exactly what happened in the incident that added
// this const: every find_places call failed because Overpass was down, the
// model retried it in 7 of 8 rounds, and the forced final round then had
// no budget left to actually answer - it returned raw tool-call markup
// instead (see assistantTextToolCallMarker below).
//
// This is deliberately NOT a masking fallback under AGENTS.md's fallback
// policy: once a tool is withheld (assistantWithheldToolResult), the
// synthesised tool result tells the model plainly that the tool is gone
// and to say so to the operator, so the upstream failure still reaches the
// answer instead of being silently retried into dead time.
const assistantMaxToolFailures = 3

// assistantMaxConcurrentToolCalls caps how many of one round's tool calls
// run at once. The system prompt asks the model to fetch both a wind
// forecast and tides for every candidate anchorage under discussion, so one
// round can carry several slow plugin fetches; running them concurrently -
// capped so a round with far more calls than the prompt actually asks for
// still can't open an unbounded number of outbound requests at once - gets
// the round back in roughly the time of its slowest call rather than their
// sum.
const assistantMaxConcurrentToolCalls = 4

// assistantMaxToolCallsPerRound bounds how many tool calls one round will
// actually dispatch (M-2 finding). assistantMaxToolRounds and
// assistantMaxToolFailures both bound how long a bad conversation can run -
// how many rounds, and how many times one named tool can be retried - but
// neither bounds how WIDE a single round can be: every one of a round's
// calls that doesn't fail outright is dispatched in full, whatever the
// count. An injected document that tells the model "a complete briefing
// needs the forecast at every one of these 200 waypoints" produces exactly
// that - 200 calls, all of which succeed, each capped at
// assistantMaxToolResultChars but still appending up to several megabytes
// of tool messages to a history that is resent in full on every remaining
// round, all billed to the operator's OpenRouter account.
//
// Comparing five candidate anchorages - find_places once for each, plus
// wind and tides for each, per the system prompt's own "for every candidate
// anchorage under discussion, fetch both" instruction - is about 15 calls
// in the busiest realistic round; 20 leaves headroom for that while still
// refusing an order-of-magnitude runaway. A call beyond the cap is refused
// explicitly (assistantExcessToolCallResult), the same pattern
// assistantWithheldToolResult already uses for a call withheld after
// repeated failure, so the model is told why it got fewer results rather
// than silently served a truncated round.
const assistantMaxToolCallsPerRound = 20

// assistantRunTimeout bounds one whole reply end to end, across every tool
// round. openRouterCompletionTimeout (openrouter_client.go) bounds each
// individual completion inside it; this is the outer ceiling so a model
// that keeps calling tools slowly can never wedge a conversation open past
// a few minutes.
const assistantRunTimeout = 3 * time.Minute

// assistantReply is what one call to assistantRunner.run produces: the
// final answer text, plus the accounting assistant_handlers.go persists
// alongside it. Tokens and cost are summed across every completion the loop
// made, not just the last one, since a multi-round answer costs more than
// its final message alone.
type assistantReply struct {
	Content          string
	Model            string
	PromptTokens     int
	CompletionTokens int
	CostUSD          float64
	ToolRounds       int
}

// assistantToolFailures counts, per tool name, how many times a tool call
// has failed across one whole call to run - not per round. It is created
// once in run and threaded into every round's runToolRound so a tool that
// fails once every round for assistantMaxToolFailures rounds is treated
// exactly like one that fails assistantMaxToolFailures times in a single
// round: both mean "this tool is down for the rest of this answer" (see
// assistantMaxToolFailures). It carries its own mutex rather than reusing
// runToolRound's local mu - that one only serialises r.emit and firstErr
// within a single round's goroutines and is recreated fresh on every call,
// so it cannot hold state that has to survive from one round to the next.
type assistantToolFailures struct {
	mu     sync.Mutex
	counts map[string]int
}

// record increments name's failure count and returns the new total. Called
// once per failed tool execution; a withheld call (see exhausted) never
// reaches this, so declining to dispatch a call never inflates the count
// past what has actually failed.
func (f *assistantToolFailures) record(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counts[name]++
	return f.counts[name]
}

// exhausted reports whether name has already failed assistantMaxToolFailures
// times this run, i.e. whether runToolRound should withhold it rather than
// dispatch it again.
func (f *assistantToolFailures) exhausted(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[name] >= assistantMaxToolFailures
}

// assistantWithheldToolResult builds the tool-role message content
// runToolRound sends back for a call it declines to dispatch because name
// has already failed assistantMaxToolFailures times this run. It is built
// with json.Marshal into the same {"error": "..."} shape a real failure
// produces a little further down in runToolRound, so the model sees one
// consistent envelope either way - but the message text is deliberately
// different from a normal failure: it tells the model outright that the
// tool is finished for this answer and what to do about it, rather than
// leaving it to guess whether retrying might work this time. That is what
// makes this fail-fast rather than a masking fallback (AGENTS.md's
// fallback policy) - the model is told plainly, so the failure reaches the
// operator in the answer instead of being quietly retried away.
func assistantWithheldToolResult(name string) (string, error) {
	msg := fmt.Sprintf(
		"%s has failed %d times and is unavailable for the rest of this answer. Do not call it again. Answer using what you already have and tell the operator %s was unavailable.",
		name, assistantMaxToolFailures, name,
	)
	body, err := json.Marshal(map[string]string{"error": msg})
	if err != nil {
		return "", fmt.Errorf("marshal withheld tool result for %q: %w", name, err)
	}
	return string(body), nil
}

// assistantExcessToolCallResult builds the tool-role message content
// runToolRound sends back for a call beyond assistantMaxToolCallsPerRound
// (M-2 finding) - the same {"error": "..."} envelope
// assistantWithheldToolResult already uses for a call withheld after
// repeated failure, so the model sees one consistent shape either way, but
// worded for a different reason: this call didn't fail and isn't broken,
// the round just asked for more than the per-round limit allows. Telling
// the model plainly, rather than just dropping the call and letting it
// infer a shorter results list on its own, is what keeps this fail-fast
// rather than a masking fallback (AGENTS.md's fallback policy).
func assistantExcessToolCallResult(name string) (string, error) {
	msg := fmt.Sprintf(
		"This round asked for more than %d tool calls; %s was not run because the per-round limit was already reached. Continue with the results already returned this round, and narrow or split the remaining work across follow-up turns rather than requesting it all at once.",
		assistantMaxToolCallsPerRound, name,
	)
	body, err := json.Marshal(map[string]string{"error": msg})
	if err != nil {
		return "", fmt.Errorf("marshal excess tool-call result for %q: %w", name, err)
	}
	return string(body), nil
}

// assistantEmitter pushes one named progress event to the SSE stream a
// caller is writing (assistant_handlers.go). This file emits "status"
// events with a {"text": "..."} payload (see assistantStatus), "delta"
// events carrying a fragment of the round's answer text as it streams in
// (assistantDelta), and "retract" events with an empty payload
// (assistantRetractPayload) telling the browser to discard whatever delta
// text it has shown for the round just finished; "message" and "error" are
// the handler's own, sent once run has returned.
type assistantEmitter func(event string, payload any)

// assistantStatus builds the payload every status event carries.
func assistantStatus(text string) any {
	return map[string]string{"text": text}
}

// assistantDelta builds the payload every delta event carries: one
// fragment of the round's answer text, to be appended to whatever the
// operator's browser has shown for this round so far. Typed the same as
// assistantStatus's payload (a plain map[string]string) so a test's
// recordingEmitter can read either kind of event's text the same way.
func assistantDelta(text string) any {
	return map[string]string{"text": text}
}

// assistantRetractPayload builds the payload every retract event carries:
// an intentionally empty object. A retract event exists purely to tell the
// browser "throw away what I showed you for this round" - the round's text
// doesn't need to travel again to say that, and typing it as
// map[string]string{} (rather than, say, struct{}{}) keeps it in the same
// family as assistantStatus/assistantDelta's payloads so a test helper that
// only knows how to read a map[string]string off an event payload still
// works uniformly across all three event kinds.
func assistantRetractPayload() any {
	return map[string]string{}
}

// assistantToolExecutor is the minimal seam the loop needs from its tool
// dependencies. assistantToolDeps (assistant_tools.go) satisfies it in
// production; tests substitute a map-backed fake so the loop can be
// exercised with no live SignalK, Overpass, or weather/tide provider.
type assistantToolExecutor interface {
	execute(ctx context.Context, name string, args json.RawMessage) (string, error)
}

// assistantRunner drives one assistant reply's agentic tool loop.
type assistantRunner struct {
	doer       openRouterDoer
	apiKey     string
	model      string
	autoRouter assistantAutoRouterOptions
	tools      assistantToolExecutor
	emit       assistantEmitter
}

// assistantTextToolCallMarkers lists the substrings that mark a message's
// content as one specific model family's own text tool-call dialect rather
// than a real answer - see assistantTextToolCallMarker for how these are
// used. One entry per known text tool-call dialect, with a comment naming
// the model family each belongs to.
var assistantTextToolCallMarkers = []string{
	"<｜DSML｜",              // DeepSeek - the dialect behind the incident assistantMaxToolFailures responds to; the full-width pipe characters (U+FF5C) are part of the literal, not a typo
	"<｜tool▁calls▁begin｜>", // DeepSeek's older tool-call dialect
	"<function_calls>",     // Anthropic-style XML
	"<invoke name=\"",      // Anthropic-style XML
	"<tool_call>",          // Qwen / Hermes / ChatML
	"<|python_tag|>",       // Llama
}

// assistantTextToolCallMarker scans content for any marker in
// assistantTextToolCallMarkers and returns the first one found. It exists
// because a model can fall back to emitting a tool call in its own text
// markup instead of the structured tool_calls field OpenRouter normally
// decodes - most commonly once tools are withdrawn with tool_choice "none"
// on the forced final round, though nothing here assumes that; a model can
// do this on any round. When that happens,
// resp.Choices[0].Message.ToolCalls is empty (there is nothing to run) and
// Content holds raw, model-internal syntax instead of prose - see run's use
// of this function for what happens next.
//
// This is a plain substring scan, so on its own it false-positives on a
// legitimate answer that happens to quote one of these markers verbatim.
// That used to be a vanishingly unlikely trade (the comment here once said
// so outright), but ADR 0106 made it a real one: read_document and
// search_documents now put arbitrary uploaded document text in front of
// the model, and an operator asking what a document about LLM tooling says
// can get a marker like "<tool_call>" quoted straight back at them (M-3
// finding). This function stays a pure substring probe regardless - run's
// onContent needs to react to a marker's mere presence, live, before
// enough of the round has arrived to judge anything more (the streaming
// hold-back window, assistantSafeStreamLen, has to err toward caution) -
// but the decision that actually rejects a reply as a broken tool call
// no longer trusts this function alone; see assistantTextToolCallMarkerIsGenuine.
func assistantTextToolCallMarker(content string) (string, bool) {
	for _, marker := range assistantTextToolCallMarkers {
		if strings.Contains(content, marker) {
			return marker, true
		}
	}
	return "", false
}

// assistantTextToolCallMarkerIsGenuine reports whether content's marker
// (assistantTextToolCallMarker) looks like a model that has actually
// fallen back to emitting a tool call in its own text dialect, rather than
// a model legitimately quoting that dialect's syntax back to the operator
// (M-3 finding). run uses this, not assistantTextToolCallMarker alone, to
// decide whether to reject a reply outright.
//
// The signal is markdown code-span/fence detection
// (assistantMarkerIsQuoted): every real fallback this package has ever
// captured (the DeepSeek DSML and Qwen ChatML fixtures in
// TestAssistantRunner_TextToolCallMarkupInFinalResponseErrorsInsteadOfReturningReply,
// ...OnForcedFinalRoundStillErrors, and the split-chunk fixture in
// TestAssistantRunner_MarkerSplitAcrossChunksNeverLeaksIntoDeltas) emits the
// marker raw, with no backticks anywhere near it - a model that has decided
// to "call a tool" this way is not presenting the syntax, it is attempting
// to use it. A model quoting the same syntax back to an operator (answering
// "what does this PDF say about tool calling") overwhelmingly wraps it in a
// code span or fence instead, the same way every model is trained to
// present literal syntax in ordinary prose.
//
// This is a heuristic, not a proof, and it is not the only signal that
// could work (position in the output and whether the surrounding text
// actually parses as a call are others) - it was chosen because it is cheap,
// requires no per-dialect parsing, and directly targets the one failure mode
// this codebase has actually seen: quoting inside an explanatory answer. A
// residual false positive is still possible (an unquoted, unfenced quote at
// the very start of an answer) - see the error message run builds when this
// returns true, which says so rather than only blaming the model.
func assistantTextToolCallMarkerIsGenuine(content string) (string, bool) {
	marker, found := assistantTextToolCallMarker(content)
	if !found {
		return "", false
	}
	if assistantMarkerIsQuoted(content, strings.Index(content, marker)) {
		return "", false
	}
	return marker, true
}

// assistantMarkerIsQuoted reports whether the byte offset idx in content
// sits inside a markdown fenced code block (triple backticks, may span
// lines) or an inline code span (single backticks, same line only) - see
// assistantTextToolCallMarkerIsGenuine. idx < 0 (no marker found) is never
// "quoted".
func assistantMarkerIsQuoted(content string, idx int) bool {
	if idx < 0 {
		return false
	}

	// Triple-backtick fence: count fence markers strictly before idx: an
	// odd count means idx falls between an opening and (eventually) a
	// closing fence.
	fenced := false
	pos := 0
	for {
		next := strings.Index(content[pos:], "```")
		if next == -1 || pos+next >= idx {
			break
		}
		fenced = !fenced
		pos += next + 3
	}
	if fenced {
		return true
	}

	// Inline code span: a backtick earlier on the same line, and another
	// later on the same line - a code span never crosses a newline in
	// markdown, so the search is bounded to idx's own line.
	lineStart := strings.LastIndexByte(content[:idx], '\n') + 1
	lineEnd := len(content)
	if rel := strings.IndexByte(content[idx:], '\n'); rel >= 0 {
		lineEnd = idx + rel
	}
	before := strings.LastIndexByte(content[lineStart:idx], '`')
	after := strings.IndexByte(content[idx:lineEnd], '`')
	return before >= 0 && after >= 0
}

// assistantTextToolCallMarkerMaxLen is the byte length of the longest
// marker in assistantTextToolCallMarkers. run's onContent uses it to size
// the streaming hold-back window (assistantSafeStreamLen below): as long as
// a round's whole accumulated-so-far text has been scanned for a marker and
// none was found, withholding its trailing assistantTextToolCallMarkerMaxLen-1
// bytes at every point guarantees no marker can ever be revealed one
// streamed fragment at a time, however it is split across chunk boundaries
// - see assistantSafeStreamLen's doc comment for the proof.
var assistantTextToolCallMarkerMaxLen = func() int {
	max := 0
	for _, marker := range assistantTextToolCallMarkers {
		if len(marker) > max {
			max = len(marker)
		}
	}
	return max
}()

// assistantSafeStreamLen returns how many leading bytes of text are
// provably free of any assistantTextToolCallMarkers marker, given that text
// in its entirety has already been scanned (by the caller) and found
// marker-free.
//
// The proof: let L = len(text) and let maxLen =
// assistantTextToolCallMarkerMaxLen. This returns safeLen = L - (maxLen-1),
// clamped to [0, L] and then backed off to the nearest rune boundary so a
// caller never slices a multi-byte UTF-8 rune in half. For any position p <
// safeLen (pre-clamping), p+maxLen <= L, so a marker of any length up to
// maxLen starting at p would have to be entirely contained within text -
// and since text has already been scanned in full and found clean, no such
// marker exists. Therefore text[:safeLen] cannot contain the start of any
// marker, complete or not, and is safe to emit; only the trailing
// maxLen-1 bytes might be an in-progress marker still waiting on more
// input, so those stay held back until either more text arrives (pushing
// safeLen forward) or the round ends (run's flush-the-tail step, once the
// final content is known clean).
func assistantSafeStreamLen(text string) int {
	safeLen := len(text) - (assistantTextToolCallMarkerMaxLen - 1)
	if safeLen < 0 {
		safeLen = 0
	}
	if safeLen > len(text) {
		safeLen = len(text)
	}
	for safeLen > 0 && safeLen < len(text) && !utf8.RuneStart(text[safeLen]) {
		safeLen--
	}
	return safeLen
}

func autoRouterPluginForModel(model string, opts assistantAutoRouterOptions) *openRouterPlugin {
	trimmed := strings.TrimSpace(strings.ToLower(model))
	pluginID := ""
	switch trimmed {
	case "openrouter/auto":
		pluginID = "auto-router"
	case "openrouter/auto-beta":
		pluginID = "auto-beta-router"
	default:
		return nil
	}
	if len(opts.AllowedModels) == 0 && len(opts.ExcludedModels) == 0 && strings.TrimSpace(opts.CostTier) == "" {
		return nil
	}
	return &openRouterPlugin{
		ID:             pluginID,
		AllowedModels:  opts.AllowedModels,
		ExcludedModels: opts.ExcludedModels,
		CostTier:       strings.TrimSpace(opts.CostTier),
	}
}

// assistantModelIsAnthropic reports whether model is one of OpenRouter's
// Anthropic-hosted ids ("anthropic/..."), the only family assistantSystemMessage
// builds a cache_control breakpoint for - see its own doc comment for why.
func assistantModelIsAnthropic(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "anthropic/")
}

// assistantAnthropicCacheControl is the ephemeral prompt-caching breakpoint
// marker assistantSystemMessage puts on the stable prefix's content block
// for an Anthropic model. A shared pointer value (every call gets the same
// one) since it never varies - always {"type":"ephemeral"}.
var assistantAnthropicCacheControl = &openRouterCacheControl{Type: "ephemeral"}

// assistantSystemMessage builds the run's system message from the stable
// prefix and live suffix assistant_prompt.go's assistantSystemPromptParts
// produces. For every model except an Anthropic one, this is exactly what
// it always was: Content is the two parts joined back into one plain
// string, byte-for-byte what buildAssistantSystemPrompt returns.
//
// For an Anthropic model, Content becomes two content blocks instead, with
// the cache_control breakpoint on the first (openRouterContentBlock,
// openrouter_client.go): OpenRouter's provider-side cache for Anthropic
// models only ever matches from the very start of the prompt, so this is
// what actually lets a later turn in the same conversation reuse the
// stable part rather than reprocessing (and repaying for) the whole prompt
// every time. Other providers behind OpenRouter are not known to
// understand this content-array shape, which is why it is scoped to
// Anthropic ids only rather than sent unconditionally.
func assistantSystemMessage(model, systemStable, systemLive string) openRouterMessage {
	if !assistantModelIsAnthropic(model) {
		return openRouterMessage{Role: "system", Content: openRouterContent(systemStable + systemLive)}
	}
	msg := openRouterMessage{Role: "system"}
	msg.contentBlocks = []openRouterContentBlock{
		{Type: "text", Text: systemStable, CacheControl: assistantAnthropicCacheControl},
		{Type: "text", Text: systemLive},
	}
	return msg
}

// run asks the model for a reply, answers any tool calls it makes, and
// repeats until the model returns plain text or assistantMaxToolRounds is
// reached, at which point tools are withdrawn (tool_choice "none") to force
// a final answer. A model that still calls a tool on that forced round is a
// bug in the model's behaviour Helmcentral cannot paper over, so that
// surfaces as an error rather than a fabricated reply (AGENTS.md's fallback
// policy). The same is true of a model that swaps the structured tool_calls
// field for its own text tool-call markup instead of a real answer (see
// assistantTextToolCallMarker) - accepting that text as the reply would
// show the operator raw model-internal syntax instead of an error, so it is
// checked and rejected on every round, not only the forced one, since a
// model doing this mid-run is just as broken. A tool that keeps failing is
// handled separately and earlier: assistantMaxToolFailures withholds it
// from further dispatch (see runToolRound) well before the round budget
// above is exhausted. Every completion is independently timed out
// (openRouterCompletionTimeout); the whole call is bounded by
// assistantRunTimeout via ctx.
//
// Each round's text streams live to the operator as "delta" events, built
// from the onContent callback openRouterChatCompletion invokes once per
// content fragment as OpenRouter sends it (this is the follow-up ADR 0093
// §5 recorded as out of scope for v1). onContent is only ever called from
// here, synchronously, inside this same call to openRouterChatCompletion -
// never from a goroutine - so it can call r.emit directly with no locking
// of its own; runToolRound's own goroutines, which do need r.emit's calls
// serialised through their local mu, only start after this round's
// completion call has already returned.
//
// A round's text is never forwarded to the browser blindly, for two
// reasons layered on top of each other. First (ADR 0093 §2's "text
// alongside tool calls is thinking aloud, not the answer"): a round that
// ends with tool calls was never going to keep its streamed text as the
// final reply, so if any of it reached the operator it has to be visibly
// taken back - see the retract event below. Second, and the reason
// forwarding is held back rather than sent immediately and retracted
// later (ADR 0103's text-tool-call-markup rejection): a model can emit its
// tool call as raw text markup instead of a real answer, and that markup
// must never be shown even for the instant before a retract could arrive.
// assistantSafeStreamLen's hold-back window means a marker can never be
// forwarded one streamed fragment at a time regardless of where OpenRouter
// happens to split it across chunks - see that function's own doc comment
// for the proof - and once assistantTextToolCallMarker finds a marker in
// the round's accumulated text, no further delta is emitted for the rest
// of the round at all.
func (r *assistantRunner) run(ctx context.Context, systemStable, systemLive string, history []openRouterMessage) (assistantReply, error) {
	ctx, cancel := context.WithTimeout(ctx, assistantRunTimeout)
	defer cancel()

	messages := make([]openRouterMessage, 0, len(history)+1)
	messages = append(messages, assistantSystemMessage(r.model, systemStable, systemLive))
	messages = append(messages, history...)

	// failures tracks each tool's failure count for this whole run (every
	// round), not just one round - see assistantToolFailures.
	failures := &assistantToolFailures{counts: make(map[string]int)}

	var reply assistantReply

	for round := 0; ; round++ {
		if round == 0 {
			r.emit("status", assistantStatus("Thinking…"))
		} else {
			r.emit("status", assistantStatus("Working out the answer…"))
		}

		req := openRouterChatRequest{
			Model:    r.model,
			Messages: messages,
			Tools:    assistantToolDefinitions(),
			Usage:    &openRouterUsageOption{Include: true},
		}
		if plugin := autoRouterPluginForModel(r.model, r.autoRouter); plugin != nil {
			req.Plugins = []openRouterPlugin{*plugin}
		}
		forcedFinal := round == assistantMaxToolRounds
		if forcedFinal {
			req.Tools = nil
			req.ToolChoice = "none"
		}

		// roundText mirrors, fragment by fragment, the content
		// openRouterChatCompletion is assembling for this round - kept here
		// too (rather than only reading the final resp.Choices[0].Message.
		// Content once the round ends) because the marker scan and the
		// hold-back window both need to run live, as each fragment arrives,
		// not only once the whole round is in hand. emittedLen is how many
		// of roundText's bytes have already gone out as delta events;
		// markerFound latches once assistantTextToolCallMarker finds a
		// marker in roundText, after which onContent stops doing anything
		// at all for the rest of the round.
		var (
			roundText    strings.Builder
			emittedLen   int
			markerFound  bool
			deltaEmitted bool
		)
		onContent := func(fragment string) {
			if markerFound {
				return
			}
			roundText.WriteString(fragment)
			text := roundText.String()
			if _, found := assistantTextToolCallMarker(text); found {
				markerFound = true
				return
			}
			safeLen := assistantSafeStreamLen(text)
			if safeLen <= emittedLen {
				return
			}
			r.emit("delta", assistantDelta(text[emittedLen:safeLen]))
			emittedLen = safeLen
			deltaEmitted = true
		}

		completionCtx, cancelCompletion := context.WithTimeout(ctx, openRouterCompletionTimeout)
		resp, err := openRouterChatCompletion(completionCtx, r.doer, r.apiKey, req, onContent)
		cancelCompletion()
		if err != nil {
			return assistantReply{}, err
		}

		reply.PromptTokens += resp.Usage.PromptTokens
		reply.CompletionTokens += resp.Usage.CompletionTokens
		reply.CostUSD += resp.Usage.Cost
		reply.Model = resp.Model

		// Text returned alongside tool calls is thinking aloud, not the
		// answer - only a choice with zero tool calls is treated as final.
		choice := resp.Choices[0].Message
		if len(choice.ToolCalls) == 0 {
			content := string(choice.Content)
			if marker, found := assistantTextToolCallMarkerIsGenuine(content); found {
				return assistantReply{}, fmt.Errorf("model %q returned a tool call as plain text (%s) instead of an answer; it is not reliably usable with tool calling here - choose a different model in Settings. If this answer was actually quoting that syntax verbatim (for example, describing a document that discusses it) rather than attempting a real call, asking the model to quote it inside a code block will avoid this", r.model, marker)
			}
			// The round ended clean: flush whatever the hold-back window
			// was still withholding, so the operator sees the reply in
			// full rather than missing its last few bytes.
			if tail := content[emittedLen:]; tail != "" {
				r.emit("delta", assistantDelta(tail))
			}
			reply.Content = content
			return reply, nil
		}

		if forcedFinal {
			return assistantReply{}, fmt.Errorf("the assistant did not produce an answer within %d tool rounds", assistantMaxToolRounds)
		}

		messages = append(messages, choice)

		// This round's text (if any reached the operator at all) was never
		// going to be the final answer - it ended in tool calls, so
		// whatever was shown has to be visibly withdrawn before the tool
		// statuses below start arriving (ADR 0093 §2).
		if deltaEmitted {
			r.emit("retract", assistantRetractPayload())
		}

		toolMessages, terr := r.runToolRound(ctx, choice.ToolCalls, failures)
		if terr != nil {
			return assistantReply{}, terr
		}
		messages = append(messages, toolMessages...)

		reply.ToolRounds++
	}
}

// runToolRound executes one round's tool calls concurrently, capped at
// assistantMaxConcurrentToolCalls, and returns their tool-role messages in
// the same order as calls - the message history must list them in that
// order regardless of which finished first, since each tool-role message's
// tool_call_id has to line up with the assistant message that requested it.
//
// failures tracks each tool's failure count across the whole run (see
// assistantToolFailures), so a call to a tool that has already failed
// assistantMaxToolFailures times is withheld rather than dispatched: its
// tool-role result is synthesised directly by assistantWithheldToolResult,
// with no call to r.tools.execute and no semaphore slot spent on it. Calls
// past assistantMaxToolCallsPerRound (M-2 finding) get the same treatment
// via assistantExcessToolCallResult, checked first - a call beyond the cap
// is refused regardless of whether its own tool has ever failed.
//
// The "about to call" status event for each dispatched call is emitted
// here in the outer, sequential loop, before that call's goroutine is even
// started, so the SSE stream still announces tool calls in the order the
// model asked for them - only the actual work (and a failure's status
// event) happens concurrently. r.emit itself is not safe for concurrent use
// (it writes SSE frames straight to the HTTP response), so every call to it
// from inside a goroutine below is serialised through mu.
//
// Every tool assistant_tools.go defines (find_places, get_wind_forecast,
// get_tides, estimate_passage, read_help) only reads: none of them writes
// to the conversation store, settings, or any other shared state, so
// running a round's calls in parallel needs no locking beyond r.emit's own
// and failures' own (see assistantToolFailures's doc comment for why that
// one is not just guarded by this round's local mu). If a future tool ever
// needs to mutate shared state, it must either take its own lock or be
// called out here as one that has to run serially.
func (r *assistantRunner) runToolRound(ctx context.Context, calls []openRouterToolCall, failures *assistantToolFailures) ([]openRouterMessage, error) {
	for _, call := range calls {
		if call.ID == "" {
			return nil, fmt.Errorf("assistant requested tool %q with no tool_call id", call.Function.Name)
		}
	}

	results := make([]openRouterMessage, len(calls))
	sem := make(chan struct{}, assistantMaxConcurrentToolCalls)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for i, call := range calls {
		args := json.RawMessage(call.Function.Arguments)

		if i >= assistantMaxToolCallsPerRound {
			mu.Lock()
			stop := firstErr != nil
			mu.Unlock()
			if stop {
				break
			}

			body, werr := assistantExcessToolCallResult(call.Function.Name)
			if werr != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = werr
				}
				mu.Unlock()
				break
			}
			log.Printf("assistant: tool %s REFUSED, round already at the %d-call limit", call.Function.Name, assistantMaxToolCallsPerRound)
			mu.Lock()
			r.emit("status", assistantStatus(fmt.Sprintf("%s was not run: this round asked for more than %d tool calls", call.Function.Name, assistantMaxToolCallsPerRound)))
			mu.Unlock()
			results[i] = openRouterMessage{Role: "tool", Content: openRouterContent(body), ToolCallID: call.ID}
			continue
		}

		if failures.exhausted(call.Function.Name) {
			mu.Lock()
			stop := firstErr != nil
			mu.Unlock()
			if stop {
				break
			}

			body, werr := assistantWithheldToolResult(call.Function.Name)
			if werr != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = werr
				}
				mu.Unlock()
				break
			}
			log.Printf("assistant: tool %s WITHHELD, already failed %d times", call.Function.Name, assistantMaxToolFailures)
			mu.Lock()
			r.emit("status", assistantStatus(fmt.Sprintf("%s is unavailable after %d failures, skipping", call.Function.Name, assistantMaxToolFailures)))
			mu.Unlock()
			results[i] = openRouterMessage{Role: "tool", Content: openRouterContent(body), ToolCallID: call.ID}
			continue
		}

		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			mu.Lock()
			if firstErr == nil {
				firstErr = ctx.Err()
			}
			mu.Unlock()
		}
		mu.Lock()
		stop := firstErr != nil
		mu.Unlock()
		if stop {
			break
		}

		mu.Lock()
		r.emit("status", assistantStatus(describeAssistantToolCall(call.Function.Name, args)))
		mu.Unlock()
		log.Printf("assistant: tool %s %s", call.Function.Name, compactAssistantToolArgs(args))

		wg.Add(1)
		go func(i int, call openRouterToolCall, args json.RawMessage) {
			defer wg.Done()
			defer func() { <-sem }()

			toolStart := time.Now()
			result, terr := r.tools.execute(ctx, call.Function.Name, args)
			elapsed := time.Since(toolStart).Round(time.Millisecond)
			if terr != nil {
				failures.record(call.Function.Name)
				oneLine := firstErrorLine(terr)
				log.Printf("assistant: tool %s failed after %s: %v", call.Function.Name, elapsed, terr)
				errBody, merr := json.Marshal(map[string]string{"error": oneLine})
				if merr != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("marshal tool error for %q: %w", call.Function.Name, merr)
					}
					mu.Unlock()
					return
				}
				result = string(errBody)
				mu.Lock()
				r.emit("status", assistantStatus(fmt.Sprintf("%s failed: %s", call.Function.Name, oneLine)))
				mu.Unlock()
			} else {
				log.Printf("assistant: tool %s -> %d chars in %s%s", call.Function.Name, len(result), elapsed, assistantFindPlacesLogSuffix(call.Function.Name, result))
			}

			results[i] = openRouterMessage{Role: "tool", Content: openRouterContent(result), ToolCallID: call.ID}
		}(i, call, args)
	}

	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// compactAssistantToolArgs trims a tool call's raw arguments to 200 runes
// for the "about to call" log line - long enough to show a query, coordinates
// and any day count, short enough that a pathological argument (or a model
// that pastes something huge into a string field) never floods the log.
func compactAssistantToolArgs(args json.RawMessage) string {
	runes := []rune(strings.TrimSpace(string(args)))
	if len(runes) > 200 {
		runes = runes[:200]
	}
	return string(runes)
}

// assistantFindPlacesLogSuffix decodes just the "search" and "results"
// fields of a successful find_places result for the "tool ... -> N chars"
// log line, so a failed or surprising lookup (the wrong rung, or zero
// results) can be diagnosed from the log after the fact without persisting
// the full tool transcript (ADR 0093 section 6 deliberately does not).
// Empty for any other tool, or if result does not decode as JSON.
func assistantFindPlacesLogSuffix(name, result string) string {
	if name != "find_places" {
		return ""
	}
	var decoded struct {
		Search  string            `json:"search"`
		Results []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal([]byte(result), &decoded); err != nil {
		return ""
	}
	return fmt.Sprintf(" (search=%s, results=%d)", decoded.Search, len(decoded.Results))
}

// assistantAttachmentExcerptRunes bounds how much of an attached document's
// markdown assistantAttachmentBlock quotes into the most recent user
// message's preamble (ADR 0106). 4000 characters is enough to answer most
// questions about a manual page or a receipt outright; read_document is
// there for the rest, named explicitly in the preamble so the model knows
// to reach for it rather than guess from a partial excerpt.
const assistantAttachmentExcerptRunes = 4000

// assistantHistoryMessages converts a conversation's persisted rows
// (assistant_store.go, which keeps only user/assistant roles - a tool round
// trip is live status only, never stored) into the wire shape a new
// completion request replays as history.
//
// getDocument resolves one attachment's live document (ADR 0106) - always
// assistantDocumentLookup (assistant_handlers.go) in production, reading
// globalDocumentStore fresh on every call so a summary or a status change
// that only lands after an earlier turn still shows up when that turn is
// replayed as history on a later one. A message with no attachments never
// calls it at all. errDocumentNotFound (the document was deleted since it
// was attached) is not an error here - see the "[attachment deleted: ...]"
// placeholder below - but every other error propagates: a database read
// that fails outright is a genuine failure (AGENTS.md's fallback policy),
// not something to paper over as "deleted".
func assistantHistoryMessages(msgs []assistantMessage, getDocument func(id string) (document, error)) ([]openRouterMessage, error) {
	lastUserIdx := -1
	for i, m := range msgs {
		if m.Role == "user" {
			lastUserIdx = i
		}
	}

	out := make([]openRouterMessage, 0, len(msgs))
	for i, m := range msgs {
		content := m.Content
		if len(m.Attachments) > 0 {
			var preamble strings.Builder
			for _, att := range m.Attachments {
				doc, err := getDocument(att.DocumentID)
				if errors.Is(err, errDocumentNotFound) {
					fmt.Fprintf(&preamble, "[attachment deleted: %s]\n", att.Filename)
					continue
				}
				if err != nil {
					return nil, fmt.Errorf("read attached document %s: %w", att.DocumentID, err)
				}
				preamble.WriteString(assistantAttachmentBlock(doc, i == lastUserIdx))
			}
			if content != "" {
				preamble.WriteString("\n")
				preamble.WriteString(content)
			}
			content = preamble.String()
		}
		out = append(out, openRouterMessage{Role: m.Role, Content: openRouterContent(content)})
	}
	return out, nil
}

// assistantDocumentBlockOpen and assistantDocumentBlockClose delimit one
// attached document's whole rendered block - header, summary and excerpt
// alike - inside the user turn that attached it (M-1 finding). Without a
// boundary, a forged trailing line inside a document's own text - one that
// happens to end in wording that mimics this very codebase's "Use
// read_document with this id for the rest." line - reads to the model as
// the start of the operator's real question, and everything the document
// says after it is then read as if the operator said it. The tag embeds
// doc.ID purely for readability when more than one document is attached to
// the same message; it is not what makes the boundary trustworthy (see
// assistantNeutralizeDocumentBlockMarker for that).
func assistantDocumentBlockOpen(id string) string {
	return fmt.Sprintf("<<<ATTACHED DOCUMENT id=%s>>>", id)
}

func assistantDocumentBlockClose(id string) string {
	return fmt.Sprintf("<<<END ATTACHED DOCUMENT id=%s>>>", id)
}

// assistantDocumentBlockMarkerPrefix is the one substring every real
// boundary tag (assistantDocumentBlockOpen/Close, for any id) starts with.
// assistantNeutralizeDocumentBlockMarker scrubs this exact substring out of
// every piece of document-derived text before it goes anywhere near the
// block, so nothing a document's own bytes contain can ever byte-match a
// real boundary tag - not this document's, and not some other attached
// document's whose id an attacker might separately know.
const assistantDocumentBlockMarkerPrefix = "<<<"

// assistantNeutralizeDocumentBlockMarker replaces every literal occurrence
// of assistantDocumentBlockMarkerPrefix in s with a character sequence that
// reads the same to a human, and close enough to a model, but can never
// byte-match assistantDocumentBlockOpen/Close (M-1 finding: "make the
// delimiter robust against a document that contains the delimiter text
// itself"). Applied to every document-derived string
// assistantAttachmentBlock writes - filename, summary, excerpt and error,
// not just the excerpt - because doc.Summary is itself model-generated from
// the document's own text and is shown on every later turn that references
// the document (showExcerpt only gates the excerpt), so injected content
// surviving summarisation would otherwise get the lighter treatment forever
// rather than the one turn the excerpt gets.
//
// A real document containing a literal run of three or more "<" (a git
// merge-conflict marker's "<<<<<<<", say) gets cosmetically mangled by
// this - an acceptable trade for a sequence with no place in a place name,
// receipt or manual page, and one a false positive here costs nothing
// beyond appearance, unlike a false negative.
func assistantNeutralizeDocumentBlockMarker(s string) string {
	return strings.ReplaceAll(s, assistantDocumentBlockMarkerPrefix, "‹‹‹")
}

// assistantAttachmentBlock renders one attached document (ADR 0106) as the
// text preamble assistantHistoryMessages puts ahead of the message that
// attached it - v1 sends no image part at all; the enrich stage's vision
// transcription (B4, documents_enrich.go) already turned a scanned page or
// a photo into markdown the model can read as plain text here. showExcerpt
// is true only when this is the most recent user message in the
// conversation (assistantHistoryMessages' lastUserIdx): earlier references
// to the same document get the header, and a summary or error if there is
// one, but never the full excerpt - otherwise a long-lived conversation
// would re-quote the same 4000 characters into every subsequent turn's
// context for no benefit, since the model can already call read_document
// for the rest.
//
// The whole block sits between assistantDocumentBlockOpen/Close (M-1
// finding) so the model can tell, unambiguously, where the document's own
// text starts and stops rather than reading it as more of the operator's
// message - assistantSystemPromptParts tells the model plainly what these
// tags mean and that content inside them is data, never instructions.
func assistantAttachmentBlock(doc document, showExcerpt bool) string {
	var b strings.Builder

	b.WriteString(assistantDocumentBlockOpen(doc.ID))
	b.WriteString("\n")

	fmt.Fprintf(&b, "[Attached document id=%s %q %s", doc.ID, assistantNeutralizeDocumentBlockMarker(doc.Filename), doc.MIME)
	if doc.PageCount > 0 {
		fmt.Fprintf(&b, ", %d pages", doc.PageCount)
	}
	b.WriteString(", status: ")
	if doc.Status == "pending" {
		b.WriteString("pending (still being read; no summary yet)")
	} else {
		b.WriteString(doc.Status)
	}
	b.WriteString("]\n")

	if doc.Summary != "" {
		fmt.Fprintf(&b, "Summary: %s\n", assistantNeutralizeDocumentBlockMarker(doc.Summary))
	}
	if doc.Status == "failed" {
		if doc.Error != "" {
			fmt.Fprintf(&b, "Error: %s\n", assistantNeutralizeDocumentBlockMarker(doc.Error))
		}
		b.WriteString(assistantDocumentBlockClose(doc.ID))
		b.WriteString("\n")
		return b.String()
	}

	if showExcerpt {
		if excerpt := strings.TrimSpace(doc.Markdown); excerpt != "" {
			runes := []rune(excerpt)
			if len(runes) > assistantAttachmentExcerptRunes {
				runes = runes[:assistantAttachmentExcerptRunes]
			}
			fmt.Fprintf(&b, "Excerpt: %s\nUse read_document with this id for the rest.\n", assistantNeutralizeDocumentBlockMarker(string(runes)))
		}
	}

	b.WriteString(assistantDocumentBlockClose(doc.ID))
	b.WriteString("\n")

	return b.String()
}
