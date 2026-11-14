package main

import (
	"strings"
	"testing"
)

// ADR 0289: Mate reads the plans with get_plans, quotes the plan's own verdict,
// and proposes a plan only when asked, from places it looked up.
func TestBuildAssistantSystemPrompt_PlansRule(t *testing.T) {
	prompt := buildAssistantSystemPrompt(basePromptContext())
	for _, want := range []string{
		// Reading.
		"get_plans",
		"quote the plan's own verdict",
		"assess_passage_window only for a date or time that is not in a plan",
		// Proposing, only when asked.
		"propose_changes on the plan record",
		"only when the operator asks you to change or create a plan, never unprompted",
		"a No-Go leg is answered, not re-planned",
		// Places.
		"Look up every stop with find_places",
		"ask the operator which before you draft",
		"never guess a position",
		`"saved place", "place names" or "your position"`,
		`"your position" for a position the operator gave`,
		`never "saved place" unless find_places returned it from saved places`,
		// Every stop gives its nights; the dates follow from them.
		"give every stop its nights, 0 for a stop passed through the same day",
		"give an arrive only on the first stop",
		"later stop's arrive or a stop's depart that disagrees with the nights is refused",
		// A plan alone; an update looks up only what is new.
		"propose only the plan",
		"do not offer to save its stops as places unless the operator asks",
		"look up only the stops that are new or moved",
		"keep the other stops' positions as they are",
		// Reading a day-by-day plan.
		"A lay day row is a night at that stop",
		"Take the year from today's date",
		"Give a via only when it matches a place",
		"say so in your reply",
		"bar crossing",
		"\"To <last stop>\"",
		"say the plan started in the past",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the plans rule misses %q", want)
		}
	}
	for _, gone := range []string{"You cannot create or change a plan", "get_planned_stay", "planned stay"} {
		if strings.Contains(prompt, gone) {
			t.Errorf("the prompt still says %q", gone)
		}
	}
}
