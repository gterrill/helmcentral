package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The sets below are who may call what, pinned by name. A review runs
// unattended and the outside assistant reaches beyond the boat, so a change to
// either list is a decision for the operator and is made here, on purpose.
// The MCP default-on list is pinnedMCPDefaultOn, in mcp_tools_test.go.
var (
	pinnedReviewReadTools = []string{
		"check_signalk_paths", "get_active_alarms", "get_alarm_context",
		"get_cruise_summary", "get_last_recorded", "get_path_history",
		"get_planned_stay", "get_tides", "get_vessel_particulars",
		"get_vessel_status", "get_wind_forecast", "list_alarm_rules",
		"list_passages",
	}
	pinnedWatchReviewReadTools = append(append([]string{}, pinnedReviewReadTools...), "search_web")
	pinnedMCPOfferedOff        = []string{
		"assess_passage_window", "check_signalk_paths", "estimate_passage",
		"list_alarm_rules", "plan_tidal_departure",
	}
)

// namesWhere lists, from every registry tool plus the MCP-only boat context,
// those the test accepts.
func namesWhere(ok func(name string) bool) []string {
	names := []string{mcpBoatContextTool}
	for _, mt := range mateTools {
		names = append(names, mt.name)
	}
	out := []string{}
	for _, n := range names {
		if ok(n) {
			out = append(out, n)
		}
	}
	return sortedCopy(out)
}

func TestMateToolAudience_SetsAreUnchanged(t *testing.T) {
	cases := []struct {
		name string
		want []string
		ok   func(string) bool
	}{
		{"alarm review", pinnedReviewReadTools, alarmReviewReadTool},
		{"watch review", pinnedWatchReviewReadTools, watchReviewReadTool},
		{"MCP offered", append(append([]string{}, pinnedMCPDefaultOn...), pinnedMCPOfferedOff...), mcpOffered},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, want := namesWhere(c.ok), sortedCopy(c.want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s set changed:\n got  %v\n want %v", c.name, got, want)
			}
		})
	}
}

// A tool added to the registry must say what it does and decide each audience.
// Left alone, it reaches no one.
func TestMateToolAudience_EveryToolHasDecidedEveryAudience(t *testing.T) {
	for _, mt := range mateTools {
		if mt.effect == effectUndecided {
			t.Errorf("%s: say whether the tool only reads (effect)", mt.name)
		}
		for audience, access := range map[string]mateAccess{
			"alarmReview": mt.audiences.alarmReview,
			"watchReview": mt.audiences.watchReview,
			"mcp":         mt.audiences.mcp,
		} {
			if access == accessUndecided {
				t.Errorf("%s: decide the %s audience (granted or denied)", mt.name, audience)
			}
		}
	}
}

// Reviews run unattended and the outside assistant is off the boat, so only a
// tool that reads may be granted to either.
func TestMateToolAudience_EveryGrantedToolOnlyReads(t *testing.T) {
	for _, mt := range mateTools {
		granted := mt.audiences.alarmReview == accessGranted || mt.audiences.watchReview == accessGranted || mt.audiences.mcp == accessGranted
		if !granted {
			continue
		}
		if mt.effect != effectReads {
			t.Errorf("%s is granted to a review or the outside assistant but is not a read", mt.name)
		}
		if mt.collect != nil {
			t.Errorf("%s is granted to a review or the outside assistant but adds proposals or form drafts to the reply", mt.name)
		}
	}
}

// A review has a live card sink, so a tool that draws a card must not be
// granted to one. Granting it to an outside assistant is safe only because
// that audience's sink is nil, which TestMateToolAudience_MCPHasNoCardSink pins.
func TestMateToolAudience_NoCardToolIsGrantedToAReview(t *testing.T) {
	for _, mt := range mateTools {
		if !mt.drawsCard {
			continue
		}
		if mt.audiences.alarmReview == accessGranted || mt.audiences.watchReview == accessGranted {
			t.Errorf("%s draws a card but is granted to a review", mt.name)
		}
		granted := mt
		granted.audiences = mateAudiences{alarmReview: accessGranted, watchReview: accessGranted, mcp: accessGranted}
		if granted.reaches(audienceAlarmReview) || granted.reaches(audienceWatchReview) {
			t.Errorf("%s draws a card and reaches a review once granted", mt.name)
		}
	}
}

// drawsCard is a claim about a handler. This reads the source and cannot match
// a flag to a single handler, only to a file: a file that puts a card must hold
// a record that says drawsCard, and a record that says it must sit in a file
// that puts one. A tool that draws a card from a helper in another file, or two
// tools in one file where only one draws, would pass. It catches a new card
// tool that forgot the flag, which is the drift that matters.
func TestMateToolAudience_DrawsCardMatchesTheSource(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	flagged := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		puts := strings.Contains(string(src), "cards.put(")
		says := strings.Contains(string(src), "drawsCard: true")
		if puts != says {
			t.Errorf("%s: puts a card %v but a record says drawsCard %v", f, puts, says)
		}
		if says {
			flagged[f] = true
		}
	}
	count := 0
	for _, mt := range mateTools {
		if mt.drawsCard {
			count++
		}
	}
	if count == 0 || len(flagged) == 0 {
		t.Error("no card tool found; the scan is broken")
	}
}

// An outside assistant is shown no card: the MCP server clears the sink for
// every tool it runs, which is why a card tool may be offered to it.
func TestMateToolAudience_MCPHasNoCardSink(t *testing.T) {
	env := newMCPTestEnv(t)
	env.deps.cards = newAssistantCardSink()
	svc := newMCPService(env.store, func() assistantToolDeps { return env.deps })
	for _, def := range mcpToolDefs() {
		if svc.toolDeps(def.Name).cards != nil {
			t.Errorf("%s: an outside assistant's call has a card sink", def.Name)
		}
	}
	if svc.toolDeps(mcpBoatContextTool).cards != nil {
		t.Error("the boat context has a card sink")
	}
}

// get_boat_context is not a Mate tool: no handler in the registry, no record,
// so it has no audiences to decide. It is built from the same readers as
// get_vessel_status and shown to outside assistants, so what is real to assert
// is that it has no registry record to act or draw through, and that running it
// puts nothing in a card sink.
func TestMCPBoatContext_IsReadOnly(t *testing.T) {
	if lookupMateTool(mcpBoatContextTool) != nil {
		t.Fatal("get_boat_context became a Mate tool; give it a record with its audiences")
	}
	def := mcpToolDefByName(mcpBoatContextTool)
	if def == nil {
		t.Fatal("get_boat_context is not offered")
	}
	d := contextTestDeps(t)
	d.cards = newAssistantCardSink()
	out, err := def.Run(context.Background(), d, json.RawMessage(`{}`))
	if err != nil || out == "" {
		t.Fatalf("run: %q %v", out, err)
	}
	if len(d.cards.byResult) != 0 {
		t.Errorf("get_boat_context drew a card: %v", d.cards.byResult)
	}
}

func TestMateToolAudience_FailsClosed(t *testing.T) {
	for _, a := range []mateAudience{audienceAlarmReview, audienceWatchReview, audienceMCP} {
		if (mateTool{}).reaches(a) {
			t.Errorf("a tool that decided nothing reached audience %d", a)
		}
		acting := mateTool{effect: effectActs, audiences: mateAudiences{alarmReview: accessGranted, watchReview: accessGranted, mcp: accessGranted}}
		if acting.reaches(a) {
			t.Errorf("a tool that acts reached audience %d", a)
		}
		undecided := mateTool{effect: effectReads}
		if undecided.reaches(a) {
			t.Errorf("a read with no audience decision reached audience %d", a)
		}
		if reachesAudience("no_such_tool", a) {
			t.Errorf("an unknown tool reached audience %d", a)
		}
	}
}

// The MCP policy keeps each tool's wording and default; the registry decides
// whether it is offered at all. The two may not disagree.
func TestMateToolAudience_MCPPolicyAgreesWithRegistry(t *testing.T) {
	for _, mt := range mateTools {
		p, ok := mcpPolicyFor(mt.name)
		if !ok {
			continue // TestMCPPolicy_EveryMateToolHasADecision reports it
		}
		if granted := mt.reaches(audienceMCP); granted != (p.Status != mcpNever) {
			t.Errorf("%s: the registry grants the outside assistant %v but the policy status is %q", mt.name, granted, p.Status)
		}
	}
}

// A policy that names no registry tool stops the server building, so a renamed
// tool cannot drop silently out of every token.
func TestMCPToolDefs_PolicyOutOfStepWithRegistryPanics(t *testing.T) {
	panics := func(policies []mcpToolPolicy) (msg string) {
		defer func() {
			if r := recover(); r != nil {
				msg, _ = r.(string)
			}
		}()
		buildMCPToolDefsFor(policies)
		return ""
	}
	renamed := append(append([]mcpToolPolicy{}, mcpToolPolicies...), mcpToolPolicy{Name: "get_vessel_status_renamed", Status: mcpDefaultOn})
	if msg := panics(renamed); !strings.Contains(msg, "which is not a Mate tool") {
		t.Errorf("a policy naming no tool: %q", msg)
	}
	var missing []mcpToolPolicy
	for _, p := range mcpToolPolicies {
		if p.Name != "get_tides" {
			missing = append(missing, p)
		}
	}
	if msg := panics(missing); !strings.Contains(msg, "has no policy entry") {
		t.Errorf("a granted tool with no policy: %q", msg)
	}
	if msg := panics(mcpToolPolicies); msg != "" {
		t.Errorf("the real policy panicked: %q", msg)
	}
}

func TestReviewAudience_RefusalNamesTheReviewNotTheTool(t *testing.T) {
	_, _, err := reviewAudienceExecute(context.Background(), assistantToolDeps{}, audienceAlarmReview, "an alarm review", "marina_details", json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "marina_details is not available in an alarm review") {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(err.Error(), "cannot change anything") {
		t.Errorf("the refusal calls a read tool a writer: %v", err)
	}
}

func TestMateToolAudience_SearchWebIsForWatchReviewsOnly(t *testing.T) {
	if !watchReviewReadTool("search_web") || alarmReviewReadTool("search_web") || mcpOffered("search_web") {
		t.Error("search_web belongs to watch reviews alone")
	}
}
