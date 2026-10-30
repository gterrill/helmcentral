package main

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// A rule that is the same on every call lives in the tool's description, which
// Mate's prompt cache holds. Only text that depends on the result is a
// "reading" field.
func TestMCPMoreTools_OnlyFigureDependentTextIsAReading(t *testing.T) {
	has := func(v any) bool {
		_, ok := reflect.TypeOf(v).FieldByName("Reading")
		return ok
	}
	for name, v := range map[string]any{
		"forecast":       assistantWindForecastResult{},
		"routed passage": assistantRoutedPassage{},
		"estimate":       assistantEstimatePassageResult{},
		"nearby":         assistantGetNearbyVesselsResult{},
		"planned stay":   assistantPlannedStayResult{},
		"window":         passageModelResult{},
	} {
		if has(v) {
			t.Errorf("%s: a constant rule belongs in the description, not the result", name)
		}
	}
	for name, v := range map[string]any{
		"tides":   assistantGetTidesResult{},
		"shelter": assistantAnchorageShelterResult{},
		"tidal":   assistantTidalDepartureResult{},
	} {
		if !has(v) {
			t.Errorf("%s: its text depends on the result and should be a reading", name)
		}
	}
}

// Each rule is said once in the description.
func TestMCPMoreTools_EstimatePassageSaysEachRuleOnce(t *testing.T) {
	desc := strings.ToLower(lookupMateTool("estimate_passage").function.Description)
	if n := strings.Count(desc, "head seas"); n != 1 {
		t.Errorf("head seas said %d times", n)
	}
	if n := strings.Count(desc, "never a straight-line distance"); n != 1 {
		t.Errorf("the straight-line rule said %d times", n)
	}
}

// The window has no card over MCP: no sink, no card, no instruction about one,
// and the grid stays in the result. Mate keeps its card and its instruction.
func TestAssessPassageWindow_NoCardSinkMeansNoCardInstruction(t *testing.T) {
	d := pwToolDeps(pwLimitsAll())
	d.cards = nil
	out, err := d.execute(context.Background(), "assess_passage_window", json.RawMessage(pwToolArgs))
	if err != nil {
		t.Fatalf("without a card sink the tool should still answer: %v", err)
	}
	var res struct {
		Instruction string            `json:"instruction"`
		Rows        []json.RawMessage `json:"rows"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) == 0 || !strings.Contains(out, `"verdict"`) {
		t.Errorf("the grid figures must stay in the result: %s", out)
	}
	if strings.Contains(strings.ToLower(out), "card") {
		t.Errorf("no card exists, so the result must not mention one: %s", out)
	}
}

func TestAssessPassageWindow_MateKeepsItsCardAndInstruction(t *testing.T) {
	d := pwToolDeps(pwLimitsAll())
	out, err := d.execute(context.Background(), "assess_passage_window", json.RawMessage(pwToolArgs))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "A card showing this grid is attached") {
		t.Errorf("Mate's result lost the card instruction: %s", out)
	}
	if len(d.takeCards(out)) != 1 {
		t.Errorf("Mate's card was not built")
	}
}

func TestAssessPassageWindow_DescriptionIsTrueOnBothPaths(t *testing.T) {
	desc := lookupMateTool("assess_passage_window").function.Description
	if strings.Contains(desc, "A card is attached to your reply") {
		t.Errorf("over MCP no card is attached, so the description must not say so: %s", desc)
	}
	if !strings.Contains(desc, "When Helmcentral shows a card") {
		t.Errorf("the description should make the card conditional: %s", desc)
	}
}

// The straight line's bearing is never a course, as its distance is never a
// passage distance.
func TestFindPlaces_BearingIsNeverACourse(t *testing.T) {
	desc := lookupMateTool("find_places").function.Description
	if !strings.Contains(desc, "straight_line_bearing_deg") || !strings.Contains(desc, "never a course") {
		t.Errorf("find_places should say the bearing is never a course: %s", desc)
	}
}

func TestPlannedStayNote_DoesNotRepeatTheDescription(t *testing.T) {
	env := newProposalEnv(t)
	env.deps.now = func() time.Time { return time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC) }
	if _, err := env.docs.SetPlannedStay(plannedStay{Name: "Coral Coast Marina", Lat: -27.2, Lon: 153.1, Arrive: "2026-10-27", Depart: "2026-10-31"}); err != nil {
		t.Fatal(err)
	}
	raw, err := runRecordTool(t, env.deps, "get_planned_stay", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "local to the place") || strings.Contains(raw, "take its lat and lon") {
		t.Errorf("the description carries these; the note must not repeat them: %s", raw)
	}
}

// The tide station's bearing is true, measured from the place asked about.
func TestTides_StationBearingIsTrueFromThePlaceAsked(t *testing.T) {
	cases := []struct {
		lat2, lon2 float64
		want       int
	}{
		{0, 1, 90},   // due east
		{1, 0, 0},    // due north
		{0, -1, 270}, // due west
		{-1, 0, 180}, // due south
	}
	for _, c := range cases {
		if got := int(bearingDeg(0, 0, c.lat2, c.lon2) + 0.5); got%360 != c.want {
			t.Errorf("bearing to %v,%v = %d, want %d", c.lat2, c.lon2, got, c.want)
		}
	}
	got := readingTides(assistantStationInfo{Name: "X", DistanceNm: 3.2, BearingDeg: 5}, "UTC")
	if !strings.Contains(got, "3.2 nm from the place asked about, bearing 005°T") {
		t.Errorf("reading: %s", got)
	}
}
