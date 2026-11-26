package main

import (
	"strconv"
	"strings"
	"testing"
)

// The comfort check (ADR 0234, amended 2026-11-26): a sentence that names both
// stops of one plan leg and gives it a band get_plans did not return for that
// leg is sent back. Nothing else about comfort is checked.

// abcPlanJSON is a get_plans result with three legs: A to B Lively then
// Comfortable, B to C Comfortable throughout, C to D with no comfort.
const abcPlanJSON = `{"plan":{"legs":[
 {"from":"Airlie Beach","to":"Hamilton Island","verdict":"go","comfort":{"text":"Lively until 10.00 AM, then Comfortable.","stretches":[
  {"band":"Lively","from_label":"8.00 AM","until_label":"10.00 AM"},
  {"band":"Comfortable","from_label":"10.00 AM","until_label":"1.30 PM"}]}},
 {"from":"Hamilton Island","to":"Whitehaven","verdict":"go","comfort":{"text":"Comfortable throughout.","stretches":[
  {"band":"Comfortable","from_label":"1.30 PM","until_label":"5.00 PM"}]}},
 {"from":"Whitehaven","to":"Cid Harbour","verdict":"go"}
]}}`

// plansRun is a run whose one call is get_plans returning result; "" is a call
// that failed.
func plansRun(result string) mateRunRecord {
	var c openRouterToolCall
	c.Function.Name = mateToolGetPlans.name
	c.Function.Arguments = `{}`
	return runOf([]openRouterToolCall{c}, []string{result})
}

// plansRuns is a run of several get_plans calls, oldest first.
func plansRuns(results ...string) mateRunRecord {
	calls := make([]openRouterToolCall, len(results))
	for i := range calls {
		calls[i].Function.Name = mateToolGetPlans.name
		calls[i].Function.Arguments = `{}`
	}
	return runOf(calls, results)
}

// planOf is a get_plans result for plan id with one leg from to, returning band.
func planOf(id int, name, from, to, band string) string {
	return `{"plan":{"id":` + strconv.Itoa(id) + `,"name":"` + name + `","legs":[{"from":"` + from + `","to":"` + to +
		`","comfort":{"text":"` + band + `.","stretches":[{"band":"` + band + `"}]}}]}}`
}

func TestComfortGuardTrigger(t *testing.T) {
	abc := plansRun(abcPlanJSON)
	const mismatch = "comfort band mismatch"
	opua := plansRun(`{"plan":{"legs":[{"from":"Ōpua","to":"Russell","comfort":{"text":"Comfortable throughout.","stretches":[{"band":"Comfortable"}]}}]}}`)
	refuge := plansRun(`{"plan":{"legs":[{"from":"Pittwater (Refuge Bay)","to":"Broken Bay","comfort":{"text":"Comfortable throughout.","stretches":[{"band":"Comfortable"}]}}]}}`)
	shrunk := plansRun(`{"plan":{"legs":[{"from":"Hayman Island","to":"Nara Inlet","comfort":{"text":"Rough at times; roughest from 7.00 AM for 1 hour","stretches":[]}}]}}`)
	doneLeg := plansRun(`{"plan":{"legs":[{"from":"Airlie Beach","to":"Hamilton Island","progress":"done","comfort":{"text":"Lively.","stretches":[{"band":"Lively"}]}}]}}`)
	skippedLeg := plansRun(`{"plan":{"legs":[{"from":"Airlie Beach","to":"Hamilton Island","progress":"skipped","comfort":{"text":"Lively.","stretches":[{"band":"Lively"}]}}]}}`)
	bandNames := plansRun(planOf(1, "Cruise", "Rough Point", "Comfortable Cove", "Lively"))
	wharf := plansRun(`{"plan":{"legs":[
	 {"from":"Ōpua","to":"Russell","comfort":{"text":"Comfortable.","stretches":[{"band":"Comfortable"}]}},
	 {"from":"Russell Wharf","to":"Paihia","comfort":{"text":"Comfortable.","stretches":[{"band":"Comfortable"}]}}]}}`)
	lively := plansRun(planOf(1, "Cruise", "Airlie Beach", "Hamilton Island", "Lively"))
	outAndBack := plansRun(`{"plan":{"id":1,"name":"Cruise","legs":[
	 {"from":"Airlie Beach","to":"Hamilton Island","comfort":{"text":"Lively.","stretches":[{"band":"Lively"}]}},
	 {"from":"Hamilton Island","to":"Airlie Beach","comfort":{"text":"Lively.","stretches":[{"band":"Lively"}]}}]}}`)
	cases := []struct {
		name string
		text string
		run  mateRunRecord
		want string
	}{
		// The middle leg: a band it did not return is sent back, and its
		// neighbours' bands do not excuse it.
		{"middle leg wrong band", "Hamilton Island to Whitehaven is Rough until 2 PM.", abc, mismatch},
		{"middle leg right band", "Hamilton Island to Whitehaven is Comfortable until 5 PM.", abc, ""},
		{"first leg right bands", "Airlie Beach to Hamilton Island is Lively until 10 AM, then Comfortable.", abc, ""},
		{"first leg wrong band", "Airlie Beach to Hamilton Island is Rough.", abc, mismatch},
		{"case-insensitive stops", "airlie beach to HAMILTON ISLAND is Rough.", abc, mismatch},
		{"markdown stripped", "Airlie Beach to Hamilton Island is **Rough**.", abc, mismatch},
		{"leg with no comfort is not checked", "Whitehaven to Cid Harbour is Lively.", abc, ""},

		// Only the capitalised form is a band word.
		{"lower case is no band", "Airlie Beach to Hamilton Island is rough.", abc, ""},

		// Negated.
		{"not", "Hamilton Island to Whitehaven is not Rough.", abc, ""},
		{"n't", "Hamilton Island to Whitehaven won't be Lively.", abc, ""},
		{"never", "Hamilton Island to Whitehaven is never Rough.", abc, ""},
		{"less", "Hamilton Island to Whitehaven is less Lively than the first leg.", abc, ""},
		{"far from", "Hamilton Island to Whitehaven is far from Rough.", abc, ""},
		{"not sent back, not Comfortable until", "The leg to Cid Harbour won't be Comfortable until 1 PM.", abc, ""},

		// A one-stop remark or a sentence naming two legs is not checked.
		{"one stop", "Hamilton Island is Rough in a southerly.", abc, ""},
		{"anchorage remark", "Whitehaven is comfortable in a southerly.", abc, ""},
		{"stops of two legs", "Airlie Beach to Whitehaven is Rough.", abc, ""},
		{"stops of no leg", "Airlie Beach to Cid Harbour is Rough.", abc, ""},
		{"no stop at all", "The leg will be Rough.", abc, ""},
		{"no get_plans", "Airlie Beach to Hamilton Island is Rough.", nil, ""},
		{"failed get_plans", "Airlie Beach to Hamilton Island is Rough.", plansRun(""), ""},
		{"a list of plans", "Airlie Beach to Hamilton Island is Rough.", plansRun(`{"plans":[]}`), ""},

		// Stop names with non-ASCII letters and punctuation, whole-name only.
		{"Ōpua wrong band", "Ōpua to Russell is Rough.", opua, mismatch},
		{"Ōpua lower case", "ōpua to russell is Lively.", opua, mismatch},
		{"Ōpua right band", "Ōpua to Russell is Comfortable.", opua, ""},
		{"punctuated stop", "Pittwater (Refuge Bay) to Broken Bay is Rough.", refuge, mismatch},
		{"punctuated stop, right band", "Pittwater (Refuge Bay) to Broken Bay is Comfortable.", refuge, ""},
		{"name inside a longer word", "Hamilton Islands to Whitehavens is Rough.", abc, ""},

		// Shrunk result: the bands are not fully known, so the leg is not checked.
		{"shrunk text, band in the text", "Hayman Island to Nara Inlet is Rough at times.", shrunk, ""},
		{"shrunk text, band not in the text", "Hayman Island to Nara Inlet is Lively at times.", shrunk, ""},

		// Done or skipped legs carry a logged rating, not a forecast.
		{"done leg", "Airlie Beach to Hamilton Island is Rough.", doneLeg, ""},
		{"skipped leg", "Airlie Beach to Hamilton Island is Rough.", skippedLeg, ""},

		// Stop names are blanked out before band words are read, longest first.
		{"stop named like a band", "Rough Point to Comfortable Cove is Lively.", bandNames, ""},
		{"stop named like a band, wrong band", "Rough Point to Comfortable Cove is Rough.", bandNames, mismatch},
		{"longer stop name hides the shorter", "Ōpua to Russell Wharf is Rough.", wharf, ""},

		// Only the clauses of the leg's statement are charged to it.
		{"next clause is another leg", "Airlie Beach to Hamilton Island is Lively until 10 AM; the next leg is Comfortable.", lively, ""},
		{"later clause not charged", "Airlie Beach to Hamilton Island is Lively until 10 AM; then Rough.", lively, ""},
		{"band after the semicolon before the stops", "It is Rough today; Airlie Beach to Hamilton Island is Lively.", lively, ""},
		{"wrong band in own clause", "Airlie Beach to Hamilton Island is Rough until 10 AM; the next leg is Lively.", lively, mismatch},

		// A negation does not reach past punctuation.
		{"negation stops at colon", "Hamilton Island to Whitehaven isn't bad: Rough after 2 PM.", abc, mismatch},
		{"negation stops at comma", "Hamilton Island to Whitehaven is not bad, Rough after 2 PM.", abc, mismatch},
		{"negation stops at dash", "Hamilton Island to Whitehaven won't be nice — Rough after 2 PM.", abc, mismatch},

		// Leg identity: a sentence matching several legs is ambiguous.
		{"two plans share a leg", "Airlie Beach to Hamilton Island is Rough.",
			plansRuns(planOf(1, "Cruise", "Airlie Beach", "Hamilton Island", "Lively"), planOf(2, "Return", "Airlie Beach", "Hamilton Island", "Comfortable")), ""},
		{"out and back in one plan", "Airlie Beach to Hamilton Island is Rough.", outAndBack, ""},
		{"re-read of a plan replaces it", "Airlie Beach to Hamilton Island is Rough.",
			plansRuns(planOf(1, "Cruise", "Airlie Beach", "Hamilton Island", "Lively"), planOf(1, "Cruise", "Airlie Beach", "Hamilton Island", "Rough")), ""},
		{"re-read drops the leg", "Airlie Beach to Hamilton Island is Rough.",
			plansRuns(planOf(1, "Cruise", "Airlie Beach", "Hamilton Island", "Lively"), planOf(1, "Cruise", "Whitehaven", "Cid Harbour", "Lively")), ""},
		{"different plans kept apart", "Whitehaven to Cid Harbour is Rough.",
			plansRuns(planOf(1, "Cruise", "Airlie Beach", "Hamilton Island", "Lively"), planOf(2, "Return", "Whitehaven", "Cid Harbour", "Lively")), mismatch},

		// The latest get_plans result for a leg wins; results are not merged.
		{"latest wins, band now returned", "Airlie Beach to Hamilton Island is Rough.",
			plansRuns(abcPlanJSON, strings.Replace(abcPlanJSON, `"band":"Lively"`, `"band":"Rough"`, 1)), ""},
		{"latest wins, band no longer returned", "Airlie Beach to Hamilton Island is Lively.",
			plansRuns(abcPlanJSON, strings.Replace(abcPlanJSON, `"band":"Lively"`, `"band":"Rough"`, 1)), mismatch},
		{"latest wins, older result is not merged", "Hamilton Island to Whitehaven is Rough.",
			plansRuns(strings.Replace(abcPlanJSON, `"band":"Comfortable","from_label":"1.30 PM"`, `"band":"Rough","from_label":"1.30 PM"`, 1), abcPlanJSON), mismatch},
	}
	for _, tc := range cases {
		got, _ := joinedDraftTrigger(pwToolDeps(pwLimitsAll()), tc.text, tc.run)
		if got != tc.want {
			t.Errorf("%s: trigger = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestComfortGuardInstruction(t *testing.T) {
	d := pwToolDeps(pwLimitsAll())
	_, ins := joinedDraftTrigger(d, "Hamilton Island to Whitehaven is Rough.", plansRun(abcPlanJSON))
	if !strings.Contains(ins, assistantComfortMismatchInstruction) || !strings.Contains(ins, assistantCheckNotice) || !strings.HasPrefix(ins, assistantCheckLabel) {
		t.Errorf("mismatch instruction = %q", ins)
	}
}

// The saved cases hold the answers the check exists to catch and the one it
// must leave alone.
func TestComfortGuard_SavedCases(t *testing.T) {
	cases := []struct {
		name string
		run  mateRunRecord
		want string
	}{
		{"comfort-band-not-returned-for-leg", plansRun(abcPlanJSON), "comfort band mismatch"},
		{"comfort-anchorage-and-negation-not-sent-back", plansRun(abcPlanJSON), ""},
	}
	for _, tc := range cases {
		c := loadMateCase(t, tc.name)
		if got, _ := joinedDraftTrigger(pwToolDeps(pwLimitsAll()), c.BadAnswer, tc.run); got != tc.want {
			t.Errorf("%s: trigger = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The comfort check does not change the relative-wind check: a plan's comfort is
// no source of the sea's angle.
func TestComfortGuard_RelativeWindCheckUnchanged(t *testing.T) {
	d := pwToolDeps(passageLimits{})
	noVerdicts := `{"plan":{"legs":[{"from":"A","to":"B","comfort":{"text":"Lively, astern.","stretches":[{"band":"Lively","reason":"swell 0.6 m 6 s astern"}]}}]}}`
	if got, _ := joinedDraftTrigger(d, "A northerly swell astern.", plansRun(noVerdicts)); got != "relative wind" {
		t.Errorf("trigger = %q, want relative wind", got)
	}
}
