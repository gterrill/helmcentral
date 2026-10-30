package main

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

// Every Mate tool needs a decision. A tool added to the registry without one
// fails here, so no new tool reaches an outside assistant by accident.
func TestMCPPolicy_EveryMateToolHasADecision(t *testing.T) {
	for _, mt := range mateTools {
		p, ok := mcpPolicyFor(mt.name)
		if !ok {
			t.Errorf("%s has no outside-assistant decision", mt.name)
			continue
		}
		if p.Reason == "" {
			t.Errorf("%s: a decision needs its reason", mt.name)
		}
	}
	for _, p := range mcpToolPolicies {
		if p.Name != mcpBoatContextTool && lookupMateTool(p.Name) == nil {
			t.Errorf("policy for %q names no tool", p.Name)
		}
	}
}

func TestMCPPolicy_NeverOfferedList(t *testing.T) {
	for _, name := range []string{
		"search_conversations", "read_conversation", // chat history
		"propose_changes", "fill_form", "inspect_form", "start_watch", // writes and proposals
		"search_web", "search_documents", "run_watch_now", // spend the OpenRouter key
		"list_records", "get_record", "describe_record_type", // can reach the owner's contact details
	} {
		if mcpOffered(name) {
			t.Errorf("%s must never be offered", name)
		}
		p, _ := mcpPolicyFor(name)
		if p.Status != mcpNever {
			t.Errorf("%s: status %q", name, p.Status)
		}
	}
}

func TestMCPPolicy_CuratedDefaultSet(t *testing.T) {
	want := []string{
		"anchorage_shelter", "find_equipment", "find_places", "get_active_alarms", "get_alarm_context", "get_boat_context",
		"get_cruise_summary", "get_last_recorded", "get_maintenance_log", "get_nearby_vessels", "get_path_history",
		"get_planned_stay", "get_tides", "get_vessel_particulars", "get_vessel_status", "get_wind_forecast",
		"list_maintenance", "list_passages", "read_help", "route_distance",
	}
	got := mcpDefaultTools()
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("default set:\n got %v\nwant %v", got, want)
	}
}

func TestMCPTools_ListIsDeterministicAndOfferedOnly(t *testing.T) {
	defs := mcpToolDefs()
	var names []string
	for _, d := range defs {
		names = append(names, d.Name)
		if !mcpOffered(d.Name) {
			t.Errorf("%s is listed but not offered", d.Name)
		}
		if d.Description == "" {
			t.Errorf("%s has no description", d.Name)
		}
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("tool order is not by name: %v", names)
	}
}

func TestMCPTools_ForTokenFiltersToTheTokensList(t *testing.T) {
	got := mcpToolsForList([]string{"get_vessel_status", "search_web", "nonsense", "check_signalk_paths"})
	var names []string
	for _, d := range got {
		names = append(names, d.Name)
	}
	if strings.Join(names, ",") != "check_signalk_paths,get_vessel_status" {
		t.Fatalf("filtered: %v", names)
	}
}

func TestMCPTools_ParticularsLoseContactDetails(t *testing.T) {
	store := newTestDocumentStore(t)
	year := 1998
	loa := 14.2
	if _, err := store.SetVesselParticulars(vesselParticulars{
		Builder: "Lagoon", Model: "420", Year: &year, LOAM: &loa, HomeMarina: "Marina One",
		OwnerName: "Pat Skipper", OwnerPhone: "+61 400 111 222", OwnerEmail: "pat@example.com",
		Insurer: "Salty Mutual", PolicyNumber: "POL-99-123", StormDelegate: "Sam Neighbour",
	}); err != nil {
		t.Fatal(err)
	}
	d := assistantToolDeps{
		documents:   func() *documentStore { return store },
		vesselState: func() (vesselStateData, error) { return vesselStateData{Name: "Pikorua"}, nil },
	}
	def := mcpToolDefByName("get_vessel_particulars")
	if def == nil {
		t.Fatal("get_vessel_particulars is not offered")
	}
	out, err := def.Run(context.Background(), d, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"Pat Skipper", "+61 400", "pat@example.com", "Salty Mutual", "POL-99", "Sam Neighbour", "owner_", "Owner", "insurer", "policy", "delegate"} {
		if strings.Contains(out, leak) {
			t.Errorf("result leaks %q: %s", leak, out)
		}
	}
	for _, keep := range []string{"Lagoon", "Marina One", "Pikorua"} {
		if !strings.Contains(out, keep) {
			t.Errorf("result lost %q: %s", keep, out)
		}
	}
	if strings.Contains(strings.ToLower(def.Description), "owner name") {
		t.Errorf("description still advertises owner details: %s", def.Description)
	}
}

func TestMCPTools_ResultsStandAlone(t *testing.T) {
	// Wording that Mate's prompt used to carry now rides with the tool.
	want := map[string]string{
		"get_vessel_status":  "missing",
		"get_active_alarms":  "investigate",
		"get_alarm_context":  "live value",
		"list_passages":      "logbook",
		"get_cruise_summary": "logbook",
		"get_path_history":   "never guess a path",
	}
	for name, phrase := range want {
		def := mcpToolDefByName(name)
		if def == nil {
			t.Errorf("%s not offered", name)
			continue
		}
		if !strings.Contains(strings.ToLower(def.Description), strings.ToLower(phrase)) {
			t.Errorf("%s description lacks %q:\n%s", name, phrase, def.Description)
		}
	}
}
