package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
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

// assistantEmitter pushes one named progress event to the SSE stream a
// caller is writing (assistant_handlers.go). This file only ever emits
// "status" events with an {"text": "..."} payload (see assistantStatus);
// "message" and "error" are the handler's own, sent once run has returned.
type assistantEmitter func(event string, payload any)

// assistantStatus builds the payload every status event carries.
func assistantStatus(text string) any {
	return map[string]string{"text": text}
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
// This is a plain substring scan, so it can false-positive on a legitimate
// answer that happens to quote one of these markers verbatim - for
// example, an answer that shows the operator an example of Anthropic's
// tool-call XML. That trade is deliberate: none of Helmcentral's tools,
// system prompt, or manual content ever produce this markup in a genuine
// answer, so a false positive here is vanishingly unlikely, and a
// silently-broken answer served to the operator as if it were real prose -
// the failure mode this exists to catch - is worse than an occasional
// loud, explicit error asking the operator to pick a different model.
func assistantTextToolCallMarker(content string) (string, bool) {
	for _, marker := range assistantTextToolCallMarkers {
		if strings.Contains(content, marker) {
			return marker, true
		}
	}
	return "", false
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

		completionCtx, cancelCompletion := context.WithTimeout(ctx, openRouterCompletionTimeout)
		resp, err := openRouterChatCompletion(completionCtx, r.doer, r.apiKey, req)
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
			if marker, found := assistantTextToolCallMarker(content); found {
				return assistantReply{}, fmt.Errorf("model %q returned a tool call as plain text (%s) instead of an answer; it is not reliably usable with tool calling here - choose a different model in Settings", r.model, marker)
			}
			reply.Content = content
			return reply, nil
		}

		if forcedFinal {
			return assistantReply{}, fmt.Errorf("the assistant did not produce an answer within %d tool rounds", assistantMaxToolRounds)
		}

		messages = append(messages, choice)

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
// with no call to r.tools.execute and no semaphore slot spent on it.
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
// get_tides, estimate_passage, read_manual) only reads: none of them writes
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

// assistantHistoryMessages converts a conversation's persisted rows
// (assistant_store.go, which keeps only user/assistant roles - a tool round
// trip is live status only, never stored) into the wire shape a new
// completion request replays as history.
func assistantHistoryMessages(msgs []assistantMessage) []openRouterMessage {
	out := make([]openRouterMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, openRouterMessage{Role: m.Role, Content: openRouterContent(m.Content)})
	}
	return out
}
