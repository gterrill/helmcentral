package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// One routine run (ADR 0227): the same hand-off the end of a watch uses
// (assistantWatchFollowUp), except that the scheduler waits for the turn to
// finish so it can record what came of it, and only one run is ever in flight.

// mateRoutineHistoryRuns is how many earlier runs of a routine its next run
// can read. The conversation keeps every run for the operator; the model sees
// only the last few, so an hourly routine does not pay for a day of history
// each hour.
const mateRoutineHistoryRuns = 3

// mateRoutineInstruction is the message a run appends: what to check, and
// where the boat's clock stands.
func mateRoutineInstruction(r mateRoutine, now time.Time, loc *time.Location) string {
	local := now.In(loc)
	clock := local.Format("Mon 2 Jan 15:04") + " (" + local.Format("MST") + ")"
	if loc == time.UTC {
		clock += ", UTC: the boat's position is not known"
	}
	// The name is alone on the first line: the chat shows that line as the
	// run's label.
	var b strings.Builder
	fmt.Fprintf(&b, "Routine run: %s\n", r.Name)
	fmt.Fprintf(&b, "Schedule: %s\n", mateRoutineSchedule(r.Cadence, r.DailyTime))
	fmt.Fprintf(&b, "Time now: %s\n", clock)
	fmt.Fprintf(&b, "Focus: %s\n", r.Focus)
	b.WriteString("[Automatic routine run from Helmcentral, not a message from the skipper. Check the boat with your tools, " +
		"starting with get_vessel_status and then what the focus needs, and reply as the routine rules say.]")
	return b.String()
}

// mateRoutineHistory keeps the messages from the instruction of the
// mateRoutineHistoryRuns-th earlier run onward. The last row is the current
// run's own instruction, which does not count as an earlier run.
func mateRoutineHistory(messages []assistantMessage) []assistantMessage {
	seen := 0
	counting := false
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "routine" {
			continue
		}
		if !counting {
			counting = true // the current instruction
			continue
		}
		seen++
		if seen == mateRoutineHistoryRuns {
			return messages[i:]
		}
	}
	return messages
}

// runMateRoutineTurn runs one routine with the readiness and key the tick read
// once. The outcome says whether the model was asked and what that cost, even
// when the run fails, so the spend ledger sees it.
func runMateRoutineTurn(ctx context.Context, cfg mateRoutineConfig, r mateRoutine, now time.Time, loc *time.Location) (mateRoutineOutcome, error) {
	if globalAssistantStore == nil {
		return mateRoutineOutcome{}, errMateRoutinesNoStore
	}
	if cfg.Readiness.Problem != "" {
		return mateRoutineOutcome{}, fmt.Errorf("Mate is not ready (%s)", strings.TrimRight(cfg.Readiness.Problem, "."))
	}
	params, err := assistantTurnParamsFromSettings(cfg.SettingsPath, cfg.Readiness, cfg.APIKey)
	if err != nil {
		return mateRoutineOutcome{}, err
	}
	// The run reads the date on the boat, as an operator's question reads
	// theirs.
	params.today = time.Date(now.In(loc).Year(), now.In(loc).Month(), now.In(loc).Day(), 0, 0, 0, 0, time.UTC)
	params.routine = &r

	if _, ok, err := globalAssistantStore.GetConversation(r.ConversationID); err != nil {
		return mateRoutineOutcome{}, err
	} else if !ok {
		return mateRoutineOutcome{}, errors.New("the routine's conversation has been deleted")
	}

	// Take the conversation before writing to it: a busy conversation then
	// leaves no half-run behind.
	runCtx, run, ok := acquireAssistantRunForWatch(r.ConversationID, mateRoutineBusyWait)
	if !ok {
		return mateRoutineOutcome{Skipped: true, Note: "a question was being answered in this conversation"}, nil
	}
	row, err := globalAssistantStore.AppendMessage(assistantMessage{ConversationID: r.ConversationID, Role: "routine", Content: mateRoutineInstruction(r, now, loc)})
	if err != nil {
		globalAssistantRuns.remove(r.ConversationID, run)
		return mateRoutineOutcome{}, err
	}
	messages, err := globalAssistantStore.ListMessages(r.ConversationID)
	if err != nil {
		globalAssistantRuns.remove(r.ConversationID, run)
		return mateRoutineOutcome{}, err
	}
	meter := &assistantCostMeter{}
	if err := beginAssistantTurn(withAssistantCostMeter(runCtx, meter), run, r.ConversationID, mateRoutineHistory(messages), params); err != nil {
		globalAssistantRuns.remove(r.ConversationID, run)
		return mateRoutineOutcome{}, err
	}
	// From here the model has been asked.
	out := mateRoutineOutcome{Reached: true}
	// A run that did not finish leaves its meter for the scheduler to price:
	// a generation that was cut off is looked up when the run is recorded.
	finish := func() {
		out.Meter = meter
		out.CostUSD, out.CostKnown = meter.snapshot()
	}

	select {
	case <-run.released:
	case <-ctx.Done():
		run.cancel()
		<-run.released
		finish()
		return out, ctx.Err()
	}

	after, err := globalAssistantStore.ListMessages(r.ConversationID)
	if err != nil {
		finish()
		return out, err
	}
	seenRow := false
	for _, m := range after {
		if m.ID == row.ID {
			seenRow = true
			continue
		}
		if seenRow && m.Role == "assistant" {
			// A finished reply carries the charge for the whole run.
			out.CostUSD, out.CostKnown = m.CostUSD, true
			headline := mateRoutineFirstLine(m.Content)
			if headline == "" {
				return out, errors.New("Mate's reply was empty")
			}
			out.Note, out.Finding = headline, mateRoutineIsFinding(headline)
			return out, nil
		}
	}
	finish()
	return out, errors.New("Mate did not finish the run; see the server log")
}
