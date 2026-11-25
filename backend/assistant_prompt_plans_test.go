package main

import (
	"strings"
	"testing"
)

// An outside assistant reads the same leg wording: the MCP tool takes the registry's
// description, so the rule reaches it.
func TestMCPGetPlans_DescriptionCarriesTheLegWording(t *testing.T) {
	def := mcpToolDefByName("get_plans")
	if def == nil {
		t.Fatal("get_plans is not an MCP tool")
	}
	if !strings.Contains(def.Description, rulePlanLegWording) {
		t.Errorf("the MCP get_plans description misses the leg wording rule: %q", def.Description)
	}
}

// ADR 0289: Mate reads the plans with get_plans, quotes the plan's own verdict,
// and proposes a plan only when asked, from places it looked up.
func TestBuildAssistantSystemPrompt_PlansRule(t *testing.T) {
	prompt := buildAssistantSystemPrompt(basePromptContext())
	for _, want := range []string{
		// Reading.
		"get_plans",
		"quote the plan's own verdict",
		"assess_passage_window only for a date or time that is not in a plan",
		// Comfort and the ETA come from the plan, never from the model's own idea of the sea.
		rulePlanLegWording,
		// A passage outside any plan still gets its sea from rel_wave.
		"For a passage that is not a plan leg, say whether it is a head, beam or following sea from rel_wave",
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
	// The tool's own description says the same, so an outside assistant reads it too.
	if !strings.Contains(mateToolGetPlans.function.Description, rulePlanLegWording) {
		t.Errorf("the get_plans description misses the leg wording rule")
	}
	// The rule itself still says the three things that keep Mate off its own idea of the sea.
	for _, want := range []string{
		"quote the leg's comfort text or its stretches with their times and place",
		"never describe how the sea will feel yourself",
		"quote eta_basis with an ETA",
	} {
		if !strings.Contains(rulePlanLegWording, want) {
			t.Errorf("the leg wording rule misses %q", want)
		}
	}
	for _, gone := range []string{"You cannot create or change a plan", "get_planned_stay", "planned stay", "A power catamaran runs comfortably"} {
		if strings.Contains(prompt, gone) {
			t.Errorf("the prompt still says %q", gone)
		}
	}
}
