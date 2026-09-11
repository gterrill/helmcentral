package main

import (
	"context"
	"encoding/json"
	"fmt"
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
	doer   openRouterDoer
	apiKey string
	model  string
	tools  assistantToolExecutor
	emit   assistantEmitter
}

// run asks the model for a reply, answers any tool calls it makes, and
// repeats until the model returns plain text or assistantMaxToolRounds is
// reached, at which point tools are withdrawn (tool_choice "none") to force
// a final answer. A model that still calls a tool on that forced round is a
// bug in the model's behaviour Helmcentral cannot paper over, so that
// surfaces as an error rather than a fabricated reply (AGENTS.md's fallback
// policy). Every completion is independently timed out
// (openRouterCompletionTimeout); the whole call is bounded by
// assistantRunTimeout via ctx.
func (r *assistantRunner) run(ctx context.Context, system string, history []openRouterMessage) (assistantReply, error) {
	ctx, cancel := context.WithTimeout(ctx, assistantRunTimeout)
	defer cancel()

	messages := make([]openRouterMessage, 0, len(history)+1)
	messages = append(messages, openRouterMessage{Role: "system", Content: openRouterContent(system)})
	messages = append(messages, history...)

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
			reply.Content = string(choice.Content)
			return reply, nil
		}

		if forcedFinal {
			return assistantReply{}, fmt.Errorf("the assistant did not produce an answer within %d tool rounds", assistantMaxToolRounds)
		}

		messages = append(messages, choice)

		for _, call := range choice.ToolCalls {
			if call.ID == "" {
				return assistantReply{}, fmt.Errorf("assistant requested tool %q with no tool_call id", call.Function.Name)
			}

			args := json.RawMessage(call.Function.Arguments)
			r.emit("status", assistantStatus(describeAssistantToolCall(call.Function.Name, args)))

			result, terr := r.tools.execute(ctx, call.Function.Name, args)
			if terr != nil {
				oneLine := firstErrorLine(terr)
				errBody, merr := json.Marshal(map[string]string{"error": oneLine})
				if merr != nil {
					return assistantReply{}, fmt.Errorf("marshal tool error for %q: %w", call.Function.Name, merr)
				}
				result = string(errBody)
				r.emit("status", assistantStatus(fmt.Sprintf("%s failed: %s", call.Function.Name, oneLine)))
			}

			messages = append(messages, openRouterMessage{
				Role:       "tool",
				Content:    openRouterContent(result),
				ToolCallID: call.ID,
			})
		}

		reply.ToolRounds++
	}
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
