package main

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The eleven tools that used to need Mate's prompt (ADR 0236).
var mcpMoreToolPolicy = map[string]mcpToolStatus{
	"read_help":             mcpDefaultOn,
	"find_places":           mcpDefaultOn,
	"anchorage_shelter":     mcpDefaultOn,
	"get_wind_forecast":     mcpDefaultOn,
	"get_tides":             mcpDefaultOn,
	"route_distance":        mcpDefaultOn,
	"get_nearby_vessels":    mcpDefaultOn,
	"get_planned_stay":      mcpDefaultOn,
	"estimate_passage":      mcpOfferedOff,
	"plan_tidal_departure":  mcpOfferedOff,
	"assess_passage_window": mcpOfferedOff,
}

func TestMCPMoreTools_Policy(t *testing.T) {
	for name, want := range mcpMoreToolPolicy {
		p, ok := mcpPolicyFor(name)
		if !ok || p.Status != want {
			t.Errorf("%s: status %q, want %q", name, p.Status, want)
		}
		if p.Plain == "" {
			t.Errorf("%s: Settings needs a plain name", name)
		}
		if strings.Contains(strings.ToLower(p.Reason), "not moved into the result yet") {
			t.Errorf("%s: stale reason %q", name, p.Reason)
		}
	}
}

func TestMCPMoreTools_DefaultOnlyForTheSafeEight(t *testing.T) {
	on := map[string]bool{}
	for _, n := range mcpDefaultTools() {
		on[n] = true
	}
	for name, want := range mcpMoreToolPolicy {
		if on[name] != (want == mcpDefaultOn) {
			t.Errorf("%s: in default list = %v", name, on[name])
		}
	}
}

// Nothing offered may call OpenRouter: the offered tools stay off the key.
func TestMCPMoreTools_SameWordsAsMate(t *testing.T) {
	for name := range mcpMoreToolPolicy {
		def := mcpToolDefByName(name)
		if def == nil {
			t.Errorf("%s is not offered", name)
			continue
		}
		p, _ := mcpPolicyFor(name)
		if p.Description != "" || p.Extra != "" {
			t.Errorf("%s: the MCP surface must read the registry's words, not a fork", name)
		}
		if def.Description != lookupMateTool(name).function.Description {
			t.Errorf("%s: MCP description differs from Mate's", name)
		}
	}
}

// Each rule an outside reader needs now travels with the tool.
func TestMCPMoreTools_DescriptionsCarryTheirRules(t *testing.T) {
	want := map[string][]string{
		"read_help":             {"no link", "valid page ids"},
		"find_places":           {"say which candidate", "never a passage distance", "straight_line_bearing_deg", "never a course"},
		"anchorage_shelter":     {"not a survey", "lee shore"},
		"get_wind_forecast":     {"not observed", "ten days", "local to the place"},
		"get_tides":             {"predicted", "datum"},
		"route_distance":        {"never substitute a straight line", "never a passage distance"},
		"get_nearby_vessels":    {"AIS", "closest point of approach", "MMSI", "lower bound"},
		"get_planned_stay":      {"local to the place", "ten days"},
		"estimate_passage":      {"never a straight-line distance", "head seas"},
		"plan_tidal_departure":  {"flood_set_source", "atlas", "never present a guessed direction", "ask for the course and passage hours"},
		"assess_passage_window": {"no-go", "not a go-ahead", "no_data"},
	}
	for name, phrases := range want {
		desc := strings.ToLower(lookupMateTool(name).function.Description)
		for _, ph := range phrases {
			if !strings.Contains(desc, strings.ToLower(ph)) {
				t.Errorf("%s description lacks %q:\n%s", name, ph, desc)
			}
		}
	}
}

func TestMCPMoreTools_ReadingTexts(t *testing.T) {
	// Only text that depends on the result stays a reading; the rest is in
	// the description, which Mate's prompt cache holds.
	cases := map[string][]string{
		"tides":   {readingTides(assistantStationInfo{Name: "Bris Bar", DistanceNm: 3.2, BearingDeg: 45}, "Australia/Brisbane"), "Bris Bar", "3.2 nm from the place asked about", "bearing 045°T", "Australia/Brisbane"},
		"shelter": {readingShelter("2026-10-01"), "2026-10-01"},
		"tidal":   {readingTidalDeparture, "wind against tide", "say so and say when", "fair_fraction"},
	}
	for name, parts := range cases {
		text := strings.ToLower(parts[0])
		for _, want := range parts[1:] {
			if !strings.Contains(text, strings.ToLower(want)) {
				t.Errorf("%s reading lacks %q: %s", name, want, parts[0])
			}
		}
	}
}

// A new token sees the new default-on tools over MCP, and an existing token
// keeps the list it was given.
func TestMCPMoreTools_RoundTrip_NewTokenSeesNewDefaults(t *testing.T) {
	env := newMCPTestEnv(t)
	_, old, _ := env.store.CreateToken("old", []string{"get_vessel_status"})
	_, fresh, _ := env.store.CreateToken("new", mcpDefaultTools())

	got := strings.Join(mcpClientToolNames(t, env.connect(t, fresh)), ",")
	for name, status := range mcpMoreToolPolicy {
		has := strings.Contains(","+got+",", ","+name+",")
		if has != (status == mcpDefaultOn) {
			t.Errorf("new token: %s listed = %v (%s)", name, has, status)
		}
	}
	if oldTools := strings.Join(mcpClientToolNames(t, env.connect(t, old)), ","); oldTools != "get_vessel_status" {
		t.Errorf("existing token changed: %s", oldTools)
	}
}

func TestMCPMoreTools_RoundTrip_ListedToolCarriesDescription(t *testing.T) {
	env := newMCPTestEnv(t)
	_, secret, _ := env.store.CreateToken("t", []string{"get_tides", "assess_passage_window"})
	cs := env.connect(t, secret)
	res, err := cs.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, tool := range res.Tools {
		seen[tool.Name] = tool.Description
	}
	if !strings.Contains(strings.ToLower(seen["get_tides"]), "datum") || !strings.Contains(strings.ToLower(seen["assess_passage_window"]), "not a go-ahead") {
		t.Errorf("descriptions lost the rules: %v", seen)
	}
}
