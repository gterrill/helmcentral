package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"strings"
	"time"
)

/*
The unattended Mate turn that watch reviews (ADR 0227) and alarm reviews
(ADR 0237) share: readiness, the turn's tool dependencies, units and prompt
context, the cost meter, a run that survives a panic, and the cost lookup.
A caller supplies what differs, which is the prompt, the message, the tools
and what the submission means.
*/

// unattendedInput is what a caller builds once the turn's dependencies exist.
type unattendedInput struct {
	System  string
	Message string
	Tools   assistantToolExecutor
	// Submitted says whether the model ended the turn with its submit tool.
	Submitted func() bool
}

type unattendedTurn struct {
	Cfg watchConfig
	Now time.Time
	// Loc is the zone the turn's "today" is read in; nil is UTC.
	Loc *time.Location
	// WebSearch lets the turn search the web when the operator's setting is on.
	WebSearch bool
	// Deadline ends the turn; zero means the caller's context decides.
	Deadline time.Duration
	// ConversationID names the turn for the cost meter.
	ConversationID string
	// LogPrefix and Name start the log lines, such as "watches" and a watch's name.
	LogPrefix, Name string
	// Build makes the turn's input. A non-empty problem ends the turn there.
	Build func(ctx context.Context, deps assistantToolDeps, units string, pc assistantPromptContext) (in unattendedInput, problem string)
}

// unattendedOutcome is how a turn ended. Problem says why there is no
// submission. Reached says the model was asked, so the cost means something
// even when the turn failed.
type unattendedOutcome struct {
	Problem   string
	Reached   bool
	CostUSD   float64
	CostKnown bool
	Model     string
}

// runUnattendedReview runs one turn. It never returns an error: whatever went
// wrong is the outcome's Problem.
func runUnattendedReview(ctx context.Context, t unattendedTurn) unattendedOutcome {
	cfg := t.Cfg
	if cfg.NotReady != "" {
		return unattendedOutcome{Problem: "Mate is not ready (" + strings.TrimRight(cfg.NotReady, ".") + ")", CostKnown: true}
	}
	params, err := assistantTurnParamsFromSettings(cfg.SettingsPath, cfg.Readiness, cfg.APIKey)
	if err != nil {
		return unattendedOutcome{Problem: "Mate's settings could not be read: " + firstErrorLine(err), CostKnown: true}
	}
	if t.Deadline > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, t.Deadline)
		defer cancel()
	}
	loc := t.Loc
	if loc == nil {
		loc = time.UTC
	}
	local := t.Now.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	deps := assistantTurnToolDeps(cfg.APIKey, cfg.SettingsPath, t.WebSearch && params.webSearch, today)
	units := "metric"
	if u, err := deps.displayUnits(); err == nil {
		units = u
	}
	in, problem := t.Build(ctx, deps, units, collectAssistantPromptContext(cfg.SettingsPath, t.Now))
	if problem != "" {
		return unattendedOutcome{Problem: problem, CostKnown: true}
	}

	meter := &assistantCostMeter{}
	runCtx := withAssistantConversationID(withAssistantCostMeter(ctx, meter), t.ConversationID)
	runner := watchReviewRunnerFor(cfg.APIKey, cfg.Readiness.Model, in.Tools, params.autoRouter)
	reply, runErr := func() (reply assistantReply, err error) {
		// The caller's goroutine must outlive a bug in one review.
		defer func() {
			if r := recover(); r != nil {
				log.Printf("%s: the review of %q panicked: %v\n%s", t.LogPrefix, t.Name, r, debug.Stack())
				meter.markUnknown()
				err = fmt.Errorf("the review crashed: %v", r)
			}
		}()
		return runner.run(runCtx, in.System, "", []openRouterMessage{{Role: "user", Content: openRouterContent(in.Message)}})
	}()

	out := unattendedOutcome{Reached: true, Model: reply.Model}
	if out.Model == "" {
		out.Model = cfg.Readiness.Model
	}
	// Whatever the run spent counts, finished or not. A generation that was cut
	// off is priced from OpenRouter; a cost that cannot be read is shown as not
	// known. The lookup has its own time, so a review that ran out of its own
	// does not lose the cost of what it spent.
	costCtx, cancelCost := context.WithTimeout(context.WithoutCancel(ctx), watchCostLookupTimeout)
	defer cancelCost()
	out.CostUSD, out.CostKnown = meter.resolve(costCtx, func(c context.Context, id string) (float64, error) {
		return generationCostLookup(c, cfg.APIKey, id)
	})
	switch {
	case runErr != nil && t.Deadline > 0 && (errors.Is(runErr, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded)):
		log.Printf("%s: Mate's review of %q ran past its deadline: %v", t.LogPrefix, t.Name, runErr)
		out.Problem = fmt.Sprintf("Mate's review ran past its deadline of %s", t.Deadline)
	case runErr != nil:
		log.Printf("%s: Mate's review of %q failed: %v", t.LogPrefix, t.Name, runErr)
		out.Problem = "Mate's review failed: " + firstErrorLine(runErr)
	case !in.Submitted():
		log.Printf("%s: Mate answered the review of %q without submitting it", t.LogPrefix, t.Name)
		out.Problem = "Mate finished without submitting a review"
	}
	return out
}
