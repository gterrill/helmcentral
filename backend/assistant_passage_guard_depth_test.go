package main

import (
	"strings"
	"testing"
)

// The anchorage-depth check. Mate has no depth for an anchorage of its own: a
// figure in a reply needs the operator's word or a saved place behind it.

func depthCall(name, args string) []openRouterToolCall {
	var c openRouterToolCall
	c.Function.Name = name
	c.Function.Arguments = openRouterArguments(args)
	return []openRouterToolCall{c}
}

const (
	knownDepthResult   = `{"depth":{"known":true,"depth_m":4,"source":"saved_place"}}`
	unknownDepthResult = `{"depth":{"known":false,"depth_m":null,"source":"none"}}`
)

func TestDepthGuardTrigger(t *testing.T) {
	cases := []struct {
		name    string
		text    string
		calls   []openRouterToolCall
		results []string
		want    string
	}{
		// Hits: a figure, a depth word and an anchorage word in one sentence.
		{"deep on sand", "Butterfly Bay is about 6 m deep on sand, with good holding.", nil, nil, "anchorage depth"},
		{"depth is metres", "The depth in the anchorage is 4 metres at low tide.", nil, nil, "anchorage depth"},
		{"fathoms of water", "You will find 3 fathoms of water in the cove.", nil, nil, "anchorage depth"},
		{"shoals", "The anchorage shoals to 2.5 m near the reef.", nil, nil, "anchorage depth"},
		{"feet", "There is 12 ft of water where you would anchor.", nil, nil, "anchorage depth"},
		{"under the keel", "At the mooring you will have 2 m under the keel at low water.", nil, nil, "anchorage depth"},
		{"anchored in metres", "You can anchor in 5 m over sand in the bay.", nil, nil, "anchorage depth"},
		{"anchored in feet", "We anchored in 12 feet last time.", nil, nil, "anchorage depth"},
		{"drop the anchor in fathoms", "Drop the anchor in about 3 fathoms off the beach.", nil, nil, "anchorage depth"},
		{"anchoring in decimal", "Anchoring in 4.5 metres is fine here.", nil, nil, "anchorage depth"},

		// Misses: no figure, no depth word, or no anchorage.
		{"mooring size", "The moorings here take vessels up to 12 m, class B.", nil, nil, ""},
		{"tide range", "The tide range at the anchorage is 2.4 m.", nil, nil, ""},
		{"distance in nm", "Good holding in the bay, 1.4 nm from the pass.", nil, nil, ""},
		{"depth word without a figure", "The anchorage is deep enough for you, but check the chart.", nil, nil, ""},
		{"wind in m/s", "Expect 10 m/s in the bay overnight.", nil, nil, ""},
		{"depth with no anchorage", "The channel is 6 m deep at the marina entrance.", nil, nil, ""},
		{"mooring takes boats to 25 m", "The mooring takes boats to 25 m.", nil, nil, ""},
		{"anchor in the bay, figure elsewhere", "Anchor in the bay, 5 m from the reef.", nil, nil, ""},
		{"figure in the next sentence", "You can anchor in the bay. It takes boats to 25 m.", nil, nil, ""},
		{"no depth at all", "There is room to swing and good holding on sand.", nil, nil, ""},

		// Sourced: a known depth in the tool's result, or a figure passed on.
		{"known depth in the result", "Butterfly Bay is 4 m deep.", depthCall("anchorage_details", `{"lat":-20,"lon":149}`), []string{knownDepthResult}, ""},
		{"depth_m passed to anchorage_details", "The anchorage is 4 m deep, as you said.", depthCall("anchorage_details", `{"lat":-20,"lon":149,"depth_m":4}`), []string{unknownDepthResult}, ""},
		{"depth_m offered in save_place", "I have proposed saving Butterfly Bay with a depth of 4 m.", depthCall("save_place", `{"name":"Butterfly Bay","lat":-20,"lon":149,"depth_m":4}`), []string{`{"proposal":{}}`}, ""},

		// Not sourced: the tool found no depth, or errored.
		{"unknown depth in the result", "Butterfly Bay is 4 m deep.", depthCall("anchorage_details", `{"lat":-20,"lon":149}`), []string{unknownDepthResult}, "anchorage depth"},
		{"errored call", "Butterfly Bay is 4 m deep.", depthCall("anchorage_details", `{"lat":-20,"lon":149,"depth_m":4}`), []string{""}, "anchorage depth"},
		{"other tool", "Butterfly Bay is 4 m deep.", depthCall("find_places", `{"query":"Butterfly Bay"}`), []string{`{"results":[]}`}, "anchorage depth"},
	}
	for _, tc := range cases {
		results := tc.results
		if results == nil {
			results = okResults(tc.calls)
		}
		if got, _ := shelterDeps().checkDraft(tc.text, runOf(tc.calls, results)); got != tc.want {
			t.Errorf("%s: trigger = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// An unsourced depth is wrong whether or not anchorage_details can run, so the
// guard stays armed with the coastline off. Its correction then must not send
// Mate to a tool that would only refuse.
func TestDepthGuard_StaysArmedWhenTheToolCannotRun(t *testing.T) {
	text := "Butterfly Bay is about 6 m deep on sand."
	off := func() *offlineCoastlineReader {
		return &offlineCoastlineReader{Enabled: func() (bool, error) { return false, nil }}
	}
	for name, reader := range map[string]func() *offlineCoastlineReader{"no coastline on this install": nil, "coastline switched off": off} {
		d := shelterDeps()
		d.offlineCoastline = reader
		got, ins := d.checkDraft(text, nil)
		if got != "anchorage depth" {
			t.Errorf("%s: trigger = %q, want anchorage depth", name, got)
		}
		if !strings.Contains(ins, "from a tool result this turn") {
			t.Errorf("%s: instruction should allow a depth read from another tool: %q", name, ins)
		}
		if strings.Contains(ins, "call anchorage_details") || !strings.Contains(ins, "Otherwise remove the depth") || !strings.Contains(ins, ruleDepth) {
			t.Errorf("%s: instruction = %q", name, ins)
		}
		// The shelter check stays disarmed: it can only be answered by the tool.
		if got, _ := d.checkDraft(thomasIslandAnswer, nil); got != "" {
			t.Errorf("%s: shelter trigger = %q, want none", name, got)
		}
		// A figure passed on in a call that ran still sources it.
		if got, _ := d.checkDraft(text, runOf(depthCall("save_place", `{"name":"Butterfly Bay","lat":-20,"lon":149,"depth_m":6}`), []string{`{"proposal":{}}`})); got != "" {
			t.Errorf("%s: sourced depth trigger = %q, want none", name, got)
		}
	}
}

// A depth_m of null is the model leaving the figure out, not the operator's depth.
func TestDepthGuard_ANullDepthArgumentIsNotADepth(t *testing.T) {
	calls := depthCall("anchorage_details", `{"lat":-20,"lon":149,"depth_m":null}`)
	got, _ := shelterDeps().checkDraft("Butterfly Bay is about 6 m deep on sand.", runOf(calls, []string{unknownDepthResult}))
	if got != "anchorage depth" {
		t.Errorf("trigger = %q, want anchorage depth", got)
	}
}

// The operator's figure can ride in a nested argument: propose_changes puts it
// in operations[].fields. Only a number sources a depth, at any depth.
func TestDepthGuard_FindsANumericDepthAnywhereInTheArguments(t *testing.T) {
	text := "Butterfly Bay is about 6 m deep on sand."
	cases := []struct {
		name, tool, args string
		want             string
	}{
		{"propose_changes fields", "propose_changes", `{"operations":[{"type":"location","action":"update","id":"l1","fields":{"depth_m":6}}]}`, ""},
		{"save_place top level", "save_place", `{"name":"Butterfly Bay","lat":-20,"lon":149,"depth_m":6}`, ""},
		{"nested null", "propose_changes", `{"operations":[{"type":"location","action":"update","id":"l1","fields":{"depth_m":null}}]}`, "anchorage depth"},
		{"nested string", "propose_changes", `{"operations":[{"type":"location","action":"update","id":"l1","fields":{"depth_m":"six"}}]}`, "anchorage depth"},
		{"nested other field", "propose_changes", `{"operations":[{"type":"location","action":"update","id":"l1","fields":{"name":"x"}}]}`, "anchorage depth"},
	}
	for _, tc := range cases {
		calls := depthCall(tc.tool, tc.args)
		if got, _ := shelterDeps().checkDraft(text, runOf(calls, []string{`{"proposal":{}}`})); got != tc.want {
			t.Errorf("%s: trigger = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestDepthGuard_InstructionSaysWhereADepthMayComeFrom(t *testing.T) {
	_, ins := shelterDeps().checkDraft("Butterfly Bay is about 6 m deep on sand.", nil)
	for _, want := range []string{"depth_m", "anchorage_details", "no charted depth", assistantCheckLabel} {
		if !strings.Contains(ins, want) {
			t.Errorf("instruction lacks %q: %q", want, ins)
		}
	}
}

func TestDepthGuard_SentTogetherWithTheShelterCorrection(t *testing.T) {
	text := "The bay is 6 m deep and sheltered from the north."
	trig, ins := shelterDeps().checkDraft(text, nil)
	if !strings.Contains(trig, "anchorage shelter") || !strings.Contains(trig, "anchorage depth") {
		t.Fatalf("trigger = %q", trig)
	}
	if !strings.Contains(ins, "shoreline check") || !strings.Contains(ins, "water depth") {
		t.Errorf("instruction should carry both: %q", ins)
	}
}

// Helmcentral holds no chart, so no correction may send the operator to one.
func TestDepthGuard_NeverTellsTheOperatorToCheckTheChart(t *testing.T) {
	for name, ins := range map[string]string{
		"with the tool":    assistantAnchorageDepthInstruction,
		"without the tool": assistantAnchorageDepthNoToolInstruction,
		"ruleDepth":        ruleDepth,
	} {
		if strings.Contains(ins, "check the chart") {
			t.Errorf("%s: sends the operator to a chart: %q", name, ins)
		}
		for _, want := range []string{"sounder", "unknown", "no charted depth"} {
			if !strings.Contains(ins, want) {
				t.Errorf("%s: lacks %q: %q", name, want, ins)
			}
		}
	}
}

// 2026-11-08: Mate read the sounder through show_tile and was sent back for a
// depth "with no source". This is the draft from the run trace; the sentence that
// tripped the check is the "swing is the thing to check" bullet. The boat's own
// sounder is a source.
const sounderDepthDraft = "You're reading **4.6 m** right now, and the tide is **falling — 3.68 m and going down**, with the next low of only **0.64 m at 2.45 PM**.\n\nSo the shallowing you're seeing isn't the boat dragging over a shelf — it's the tide going out. Look back at the trend over the last three hours: you were sitting in a steady 7.2–7.4 m for a couple of hours, then it drops sharply from about 11.55 PM last night, dipping to 4.6 m now. That steady background level is roughly your draught plus the tide height above datum; as the water drains away toward this afternoon's low, the depth under the keel shrinks with it.\n\nWorth watching rather than alarming about:\n\n- **The swing is the thing to check.** At this afternoon's low the tide will be down to about 0.64 m, so depth could drop well under what you have now. If the boat swings into a shallower patch as it turns, you'll feel it long before it matters, but keep an eye on where the anchor lies relative to the dropping water.\n- The depth reading is live and fresh (1 second old), so the figure is real — not a sensor dropout.\n\nThe Anchor Watch will be your friend through the low this afternoon; if it's armed, it'll call out if the boat drifts off the swung arc as the water falls. Want me to run through what its current limits are set to?"

// The tile as it read at the time: the sounder at 4.61 m, the tide falling to a
// low of 0.64 m, and three hours of trend.
const sounderTileResult = `{"tile":"depth-tide","snapshot":{"depth_m":4.61,"depth_age_s":1.2,` +
	`"tide":{"current_tide_height_ft":12.07,"high_tide_height_ft":14.4,"low_tide_height_ft":2.1},` +
	`"depth_trend":{"points":[{"time":"2026-11-07T12:00:00Z","depth_m":7.4},{"time":"2026-11-08T02:00:00Z","depth_m":4.61}]}}}`

func tileCall() []openRouterToolCall { return depthCall("show_tile", `{"tile":"depth-tide"}`) }

// The boat's own sounder, read this turn with no call about another place, is
// the depth at the boat's position.
func TestDepthGuard_TheBoatsOwnSounderIsASourceWhenNoOtherPlaceWasAskedAbout(t *testing.T) {
	if got, _ := shelterDeps().checkDraft(sounderDepthDraft, runOf(tileCall(), []string{sounderTileResult})); got != "" {
		t.Errorf("trace draft after the tile read: trigger = %q, want none", got)
	}
	// With no sounder read the same draft is still sent back.
	if got, _ := shelterDeps().checkDraft(sounderDepthDraft, nil); got != "anchorage depth" {
		t.Errorf("trace draft with no source: trigger = %q, want anchorage depth", got)
	}
	// A tile with no depth, another tile, or an errored call is no sounder read.
	for name, result := range map[string]string{
		"no depth":   `{"tile":"depth-tide","snapshot":{"depth_m":null}}`,
		"errored":    ``,
		"other tile": `{"tile":"wind","snapshot":{"depth_m":4.61}}`,
	} {
		if got, _ := shelterDeps().checkDraft(sounderDepthDraft, runOf(tileCall(), []string{result})); got != "anchorage depth" {
			t.Errorf("%s: trigger = %q, want anchorage depth", name, got)
		}
	}
}

// A call about somewhere else means the draft's depths may be for that place,
// where the boat's own sounder says nothing.
func TestDepthGuard_AnyCallAboutAnotherPlaceTakesTheSounderAway(t *testing.T) {
	cases := []struct {
		name, tool, args string
	}{
		{"find_places", "find_places", `{"query":"Butterfly Bay"}`},
		{"marina_details", "marina_details", `{"lat":-20,"lon":149}`},
		{"anchorage_details", "anchorage_details", `{"lat":-20,"lon":149}`},
		{"save_place", "save_place", `{"name":"Butterfly Bay","lat":-20,"lon":149}`},
		{"plan_tidal_departure", "plan_tidal_departure", `{}`},
		{"route_distance", "route_distance", `{}`},
		{"assess_passage_window", "assess_passage_window", `{}`},
		{"estimate_passage", "estimate_passage", `{}`},
		{"read_document", "read_document", `{"document_id":"d1"}`},
		{"search_documents", "search_documents", `{"query":"anchorages"}`},
		{"search_web", "search_web", `{"query":"Butterfly Bay depth"}`},
		{"get_plans", "get_plans", `{}`},
		{"get_tides at coordinates", "get_tides", `{"lat":-20,"lon":149}`},
		{"get_wind_forecast at coordinates", "get_wind_forecast", `{"lat":-20,"lon":149}`},
	}
	for _, tc := range cases {
		calls := append(tileCall(), depthCall(tc.tool, tc.args)...)
		results := []string{sounderTileResult, `{"summary":"ok"}`}
		if tc.name == "anchorage_details" {
			results[1] = unknownDepthResult
		}
		if got, _ := shelterDeps().checkDraft(sounderDepthDraft, runOf(calls, results)); got != "anchorage depth" {
			t.Errorf("%s: trigger = %q, want anchorage depth", tc.name, got)
		}
	}
	// The same tools without coordinates read the boat's own position.
	for _, tool := range []string{"get_tides", "get_wind_forecast"} {
		calls := append(tileCall(), depthCall(tool, `{}`)...)
		if got, _ := shelterDeps().checkDraft(sounderDepthDraft, runOf(calls, []string{sounderTileResult, `{"summary":"ok"}`})); got != "" {
			t.Errorf("%s with no coordinates: trigger = %q, want none", tool, got)
		}
	}
	// A place call that failed told Mate nothing about a place.
	calls := append(tileCall(), depthCall("find_places", `{"query":"x"}`)...)
	if got, _ := shelterDeps().checkDraft(sounderDepthDraft, runOf(calls, []string{sounderTileResult, ""})); got != "" {
		t.Errorf("errored find_places: trigger = %q, want none", got)
	}
}

func TestDepthGuard_AStatusReadCountsAsASounderRead(t *testing.T) {
	calls := depthCall("get_vessel_status", `{}`)
	cases := []struct {
		name, text, result, want string
	}{
		// An accepted gap: the check cannot tell that a bay with the boat's own
		// depth is not this one. A depth word and a figure with no place call
		// in the run is read as about the boat's position.
		{"another bay's depth after a status read", "Bay X has 4 m of water.", `{"surroundings":{"depth":{"value":12,"unit":"m"}}}`, ""},
		{"surroundings depth", "At anchor you have 12 m of water right now.", `{"surroundings":{"depth":{"value":12,"unit":"m"}}}`, ""},
		{"low_water only, no surroundings depth", "At anchor you have 1.3 m of water at the low.", `{"surroundings":{"depth":{"missing":"stale"}},"boat":{"anchor":{"low_water":{"status":"too_shallow","depth_now":{"value":1.9,"unit":"m"}}}}}`, ""},
		{"no depth anywhere", "At anchor you have 12 m of water right now.", `{"surroundings":{"depth":{"missing":"no depth reading"}},"boat":{"anchor":{"low_water":{"status":"unknown","reason":"no_depth"}}}}`, "anchorage depth"},
	}
	for _, tc := range cases {
		if got, _ := shelterDeps().checkDraft(tc.text, runOf(calls, []string{tc.result})); got != tc.want {
			t.Errorf("%s: trigger = %q, want %q", tc.name, got, tc.want)
		}
	}
}
