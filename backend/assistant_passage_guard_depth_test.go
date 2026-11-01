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
		if got, _ := shelterDeps().checkDraft(tc.text, tc.calls, results); got != tc.want {
			t.Errorf("%s: trigger = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestDepthGuard_OffWhenTheToolCannotRun(t *testing.T) {
	text := "Butterfly Bay is about 6 m deep on sand."
	d := shelterDeps()
	d.offlineCoastline = nil
	if got, _ := d.checkDraft(text, nil, nil); got != "" {
		t.Errorf("no Offline coastline: trigger = %q, want none", got)
	}
	d = shelterDeps()
	d.offlineCoastline = func() *offlineCoastlineReader {
		return &offlineCoastlineReader{Enabled: func() (bool, error) { return false, nil }}
	}
	if got, _ := d.checkDraft(text, nil, nil); got != "" {
		t.Errorf("coastline switched off: trigger = %q, want none", got)
	}
}

func TestDepthGuard_InstructionSaysWhereADepthMayComeFrom(t *testing.T) {
	_, ins := shelterDeps().checkDraft("Butterfly Bay is about 6 m deep on sand.", nil, nil)
	for _, want := range []string{"depth_m", "anchorage_details", "check the chart", assistantCheckLabel} {
		if !strings.Contains(ins, want) {
			t.Errorf("instruction lacks %q: %q", want, ins)
		}
	}
}

func TestDepthGuard_SentTogetherWithTheShelterCorrection(t *testing.T) {
	text := "The bay is 6 m deep and sheltered from the north."
	trig, ins := shelterDeps().checkDraft(text, nil, nil)
	if !strings.Contains(trig, "anchorage shelter") || !strings.Contains(trig, "anchorage depth") {
		t.Fatalf("trigger = %q", trig)
	}
	if !strings.Contains(ins, "shoreline check") || !strings.Contains(ins, "water depth") {
		t.Errorf("instruction should carry both: %q", ins)
	}
}
