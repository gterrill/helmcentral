package main

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// The record of how one Mate run went: its rounds, their timings and tool
// calls, how the run ended and what it was allowed to spend (ADR 0240). A
// run that answers keeps its answer in the conversation; one that fails keeps
// nothing there, so this record is the only trace of what happened. The
// runner fills in a recorder as it works, and the handler stores it when the
// run ends, whatever way that is.

// How a run ended. Answered means the model wrote its answer without being
// stopped. Stalled, round ceiling and time ceiling mean the budget forced the
// final answer; the run still answered when it has an answer message, and
// failed when the forced answer did not arrive.
const (
	assistantStopAnswered     = "answered"
	assistantStopStalled      = "stalled"
	assistantStopRoundCeiling = "round_ceiling"
	assistantStopTimeCeiling  = "time_ceiling"
	assistantStopCancelled    = "cancelled"
	assistantStopError        = "error"
)

// assistantTraceArgsMaxBytes bounds the arguments the run details show for a
// call; the stored call keeps them up to assistantToolCallMaxBytes.
const assistantTraceArgsMaxBytes = 2048

// assistantTraceBudget is the budget a run was given, stored with the run so
// the details still make sense after a limit changes.
type assistantTraceBudget struct {
	MaxRounds        int   `json:"max_rounds"`
	MaxStalledRounds int   `json:"max_stalled_rounds"`
	ToolPhaseMs      int64 `json:"tool_phase_ms"`
	FinalReserveMs   int64 `json:"final_reserve_ms"`
}

func currentAssistantTraceBudget() assistantTraceBudget {
	return assistantRunnerOptions{}.budget().trace()
}

// What a round was. Lookups asked for tools. Correction was a draft sent back
// once. Answer wrote the answer unprompted; Final wrote it after the tools were
// withdrawn. Cut was ended by the tool phase running out of time. Failed ended
// in an error.
const (
	assistantRoundLookups    = "lookups"
	assistantRoundCorrection = "correction"
	assistantRoundAnswer     = "answer"
	assistantRoundFinal      = "final"
	assistantRoundCut        = "cut"
	assistantRoundFailed     = "failed"
)

// assistantTraceRound is one completion and the tool calls it asked for.
type assistantTraceRound struct {
	Index int `json:"index"`
	// Kind says what the round was: assistantRound* below.
	Kind             string  `json:"kind"`
	StartOffsetMs    int64   `json:"start_offset_ms"`
	LatencyMs        int64   `json:"latency_ms"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CachedTokens     int     `json:"cached_tokens"`
	CostUSD          float64 `json:"cost_usd"`
	// ForcedFinal marks the round that withdrew the tools to get an answer.
	ForcedFinal bool `json:"forced_final"`
	// Progress says the round made at least one new call that succeeded.
	Progress bool `json:"progress"`
	// Error is why the round's completion or tools failed, if they did.
	Error string              `json:"error"`
	Calls []assistantToolCall `json:"calls"`
}

// assistantRunTrace is one stored run.
type assistantRunTrace struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
	// AfterSeq is the sequence number of the last message in the conversation
	// when the run began (the question it answered), so a failed run can be
	// shown in its place in the thread.
	AfterSeq int `json:"after_seq"`
	// AnswerMessageID is the answer the run produced, nil when it failed.
	AnswerMessageID *string `json:"answer_message_id"`
	Model           string  `json:"model"`
	// StartedAt is when the run began.
	StartedAt  time.Time `json:"started_at"`
	DurationMs int64     `json:"duration_ms"`
	StopReason string    `json:"stop_reason"`
	Error      string    `json:"error"`
	// RoundCount is the number of rounds, filled in on summaries that omit
	// Rounds.
	RoundCount int                   `json:"round_count"`
	Budget     assistantTraceBudget  `json:"budget"`
	Rounds     []assistantTraceRound `json:"rounds,omitempty"`
}

// assistantRunRecorder collects a run's rounds and stop reason while it works.
// Every method is safe on a nil recorder, so a run with nowhere to keep a
// trace (a watch review) records nothing.
type assistantRunRecorder struct {
	mu     sync.Mutex
	rounds []assistantTraceRound
	stop   string
	// model is the one that answered the latest completion.
	model string
}

type assistantRunRecorderKey struct{}

func withAssistantRunRecorder(ctx context.Context, rec *assistantRunRecorder) context.Context {
	return context.WithValue(ctx, assistantRunRecorderKey{}, rec)
}

func assistantRunRecorderFrom(ctx context.Context) *assistantRunRecorder {
	rec, _ := ctx.Value(assistantRunRecorderKey{}).(*assistantRunRecorder)
	return rec
}

func (r *assistantRunRecorder) addRound(round assistantTraceRound) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if round.Calls == nil {
		round.Calls = []assistantToolCall{}
	}
	r.rounds = append(r.rounds, round)
}

func (r *assistantRunRecorder) setModel(model string) {
	if r == nil || model == "" {
		return
	}
	r.mu.Lock()
	r.model = model
	r.mu.Unlock()
}

// lastModel is the model that answered the latest completion, or "" if none did.
func (r *assistantRunRecorder) lastModel() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.model
}

func (r *assistantRunRecorder) setStop(reason string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.stop = reason
	r.mu.Unlock()
}

// result returns the rounds and stop reason recorded so far.
func (r *assistantRunRecorder) result() ([]assistantTraceRound, string) {
	if r == nil {
		return nil, ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]assistantTraceRound(nil), r.rounds...), r.stop
}

// assistantToolOutcome is what a tool can say about how it ran that its result
// text does not carry. Cache is "hit" or "miss" for a tool that has a cache
// (find_places and the place search cache), and empty for every other tool.
type assistantToolOutcome struct {
	Cache string
}

// assistantCallKey names a tool call for the progress rule: the tool and its
// arguments with the JSON canonicalised, so reordered keys or different
// spacing do not make a repeat look new. Arguments that are not JSON compare
// as written.
func assistantCallKey(name, args string) string {
	var decoded any
	if err := json.Unmarshal([]byte(args), &decoded); err != nil {
		return name + "\x00" + args
	}
	canonical, err := json.Marshal(decoded)
	if err != nil {
		return name + "\x00" + args
	}
	return name + "\x00" + string(canonical)
}

// assistantStopReasonFor works out how a run ended from what the runner
// recorded and what came back. forced is the reason the budget gave for
// withdrawing tools, empty when it never did. timeUp says the budget's own
// clock (the tool phase or the final reserve) had run out, which is what makes
// a deadline error a time ceiling; one reply's own timeout is an error.
func assistantStopReasonFor(forced string, err error, cancelled, timeUp bool) string {
	switch {
	case cancelled:
		return assistantStopCancelled
	case err == nil && forced == "":
		return assistantStopAnswered
	case err == nil:
		return forced
	case errors.Is(err, context.DeadlineExceeded) && timeUp:
		return assistantStopTimeCeiling
	case forced != "":
		return forced
	}
	return assistantStopError
}

// An unattended run (a watch or alarm review: nobody is waiting on it) keeps
// tighter limits than a conversation: fewer rounds and a shorter tool phase,
// with the same stall rule and final reserve (ADR 0240).
const assistantUnattendedMaxToolRounds = 8

// assistantUnattendedToolPhaseTimeout is the tool phase of an unattended run. A
// var so a test can shorten it.
var assistantUnattendedToolPhaseTimeout = 3 * time.Minute

// assistantRunBudget is the limits one run works to.
type assistantRunBudget struct {
	maxRounds    int
	maxStalled   int
	toolPhase    time.Duration
	finalReserve time.Duration
}

// budget is the budget a runner built with these options works to.
func (o assistantRunnerOptions) budget() assistantRunBudget {
	if o.Unattended {
		return assistantRunBudget{assistantUnattendedMaxToolRounds, assistantMaxStalledRounds, assistantUnattendedToolPhaseTimeout, assistantFinalReserve}
	}
	return assistantRunBudget{assistantMaxToolRounds, assistantMaxStalledRounds, assistantToolPhaseTimeout, assistantFinalReserve}
}

// ceiling is the longest a run to this budget can last: the tool phase, a
// reserve for a round already writing when it ends, and the final answer's.
func (b assistantRunBudget) ceiling() time.Duration { return b.toolPhase + 2*b.finalReserve }

// trace is the budget as a run's trace records it.
func (b assistantRunBudget) trace() assistantTraceBudget {
	return assistantTraceBudget{
		MaxRounds:        b.maxRounds,
		MaxStalledRounds: b.maxStalled,
		ToolPhaseMs:      b.toolPhase.Milliseconds(),
		FinalReserveMs:   b.finalReserve.Milliseconds(),
	}
}
