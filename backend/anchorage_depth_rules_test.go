package main

import (
	"strings"
	"testing"
)

// The depth rule is written once, as ruleDepth, and the tool and the guard
// carry it; the system prompt only points at the tool.
func TestDepthRuleHasOneHome(t *testing.T) {
	if !strings.Contains(mateToolAnchorageDetails.function.Description, ruleDepth) {
		t.Error("the anchorage_details description must carry ruleDepth")
	}
	if !strings.Contains(assistantAnchorageDepthInstruction, ruleDepth) {
		t.Error("the guard's correction must carry ruleDepth")
	}
	if !strings.Contains(mateToolSavePlace.function.Description, ruleDepthArg) {
		t.Error("the save_place description must carry ruleDepthArg")
	}
	prompt, _ := assistantSystemPromptParts(basePromptContext())
	if strings.Contains(prompt, ruleDepthArg) || strings.Contains(prompt, "depth.known") {
		t.Error("the system prompt must not repeat the depth rule")
	}
	if !strings.Contains(prompt, "where a depth may come from") {
		t.Error("the system prompt should point at the tool for where a depth may come from")
	}
}

func TestReadingAnchorageDetails_StatesOnlyThePerCallDepthFact(t *testing.T) {
	known := anchorageDepthSection{Known: true, Label: "4.0 m at datum, from you"}
	if got := readingAnchorageDetails(false, "", "", "", known); got != "The depth used is 4.0 m at datum, from you." {
		t.Errorf("reading = %q", got)
	}
	if got := readingAnchorageDetails(false, "", "", "", anchorageDepthSection{}); !strings.Contains(got, "depth is unknown") || strings.Contains(got, "chart") {
		t.Errorf("reading = %q", got)
	}
}

func TestSavedPlacesNear_BoxKeepsTheCircleAndDropsTheRest(t *testing.T) {
	env := newProposalEnv(t)
	// 0.1 nm is 185 m, 0.00166 degrees of latitude. At 60 N a degree of
	// longitude is half as long, so the same circle spans 0.0033 degrees.
	createDepthPlace(t, env, "Inside north", 60.0015, 10.0, depthOf(5), "charted", "2026-10-01")
	createDepthPlace(t, env, "Inside east", 60.0, 10.003, depthOf(5), "charted", "2026-10-01")
	createDepthPlace(t, env, "Far north", 60.01, 10.0, depthOf(5), "charted", "2026-10-01")
	createDepthPlace(t, env, "Far east", 60.0, 10.02, depthOf(5), "charted", "2026-10-01")
	got, err := readSavedPlacesNear(env.docs, 60.0, 10.0, anchorageSavedPlaceRadiusM)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range got {
		names = append(names, p.Name)
	}
	if strings.Join(names, ",") != "Inside east,Inside north" {
		t.Errorf("places in the box = %v", names)
	}

	// Next to the antimeridian the longitude bound is dropped, not wrapped.
	createDepthPlace(t, env, "Dateline", 0.0, 179.9995, depthOf(5), "charted", "2026-10-01")
	got, err = readSavedPlacesNear(env.docs, 0.0, 179.9999, anchorageSavedPlaceRadiusM)
	if err != nil || len(got) != 1 || got[0].Name != "Dateline" {
		t.Errorf("dateline: %v %+v", err, got)
	}
}
