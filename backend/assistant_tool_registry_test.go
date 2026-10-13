package main

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestMateTools_NamesUniqueAndComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, tool := range mateTools {
		if tool.name == "" {
			t.Fatalf("a registry record has no name: %+v", tool.function)
		}
		if seen[tool.name] {
			t.Errorf("tool %q is registered twice", tool.name)
		}
		seen[tool.name] = true
		if tool.execute == nil {
			t.Errorf("%s has no execute", tool.name)
		}
		if tool.describe == nil {
			t.Errorf("%s has no describe", tool.name)
		}
		if tool.function.Description == "" || len(tool.function.Parameters) == 0 {
			t.Errorf("%s has an empty definition", tool.name)
		}
		if tool.function.Name != "" {
			t.Errorf("%s writes its name into the definition; the registry stamps it", tool.name)
		}
	}
	if last := mateTools[len(mateTools)-1].name; last != "search_web" {
		t.Errorf("search_web should be last, got %q", last)
	}
}

func TestMateTools_DefinitionNameIsRecordName(t *testing.T) {
	for _, on := range []bool{false, true} {
		deps := assistantToolDeps{}
		if on {
			deps = webSearchStubDeps()
		}
		tools := assistantToolDefinitions(deps)
		for _, tool := range tools {
			if tool.Type != "function" {
				t.Errorf("%s: type %q", tool.Function.Name, tool.Type)
			}
			if lookupMateTool(tool.Function.Name) == nil {
				t.Errorf("definition %q has no registry record", tool.Function.Name)
			}
		}
		want := len(mateTools)
		if !on {
			want--
		}
		if len(tools) != want {
			t.Errorf("web search %v: %d definitions, want %d", on, len(tools), want)
		}
	}
}

func TestMateTools_UnavailableToolIsRefusedByExecute(t *testing.T) {
	deps := assistantToolDeps{} // webSearch nil: search_web unavailable
	_, err := deps.execute(context.Background(), "search_web", json.RawMessage(`{"query":"x"}`))
	if err == nil || !strings.Contains(err.Error(), "switched off") {
		t.Fatalf("expected a switched-off refusal, got %v", err)
	}
	if mateToolAvailable(deps, "search_web") {
		t.Fatal("search_web should be unavailable without a web search function")
	}
}

func TestMateTools_CollectWiredForProposalsAndForms(t *testing.T) {
	var got []string
	for _, tool := range mateTools {
		if tool.collect != nil {
			got = append(got, tool.name)
		}
	}
	sort.Strings(got)
	if strings.Join(got, ",") != "fill_form,propose_changes" {
		t.Fatalf("collect is wired on %v", got)
	}
}

// toolLikeToken matches the verb_noun shape of a tool name. A prompt or a
// piece of tool-naming prose that mentions a tool that is not registered (a
// rename that missed a sentence) fails here.
var toolLikeToken = regexp.MustCompile(`\b(?:find|get|search|read|list|estimate|plan|check|describe|propose|start|inspect|fill)_[a-z]+(?:_[a-z]+)*\b`)

// notToolNames are tokens that look like a tool name and are not one.
var notToolNames = map[string]bool{
	"plan_document_id":            true, // a field on the inventory record
	"read_only":                   true, // ordinary prose in pdf_form_fill.go
	"propose_maintenance_changes": true, // ADR 0146's old name, named in a history comment
}

func unregisteredToolTokens(text string) []string {
	var bad []string
	for _, tok := range toolLikeToken.FindAllString(text, -1) {
		if notToolNames[tok] || lookupMateTool(tok) != nil {
			continue
		}
		bad = append(bad, tok)
	}
	return bad
}

func TestMateTools_PromptNamesOnlyRegisteredTools(t *testing.T) {
	for _, web := range []bool{false, true} {
		pc := basePromptContext()
		pc.WebSearch = web
		stable, live := assistantSystemPromptParts(pc)
		if bad := unregisteredToolTokens(stable + "\n" + live); len(bad) > 0 {
			t.Errorf("web search %v: prompt names unregistered tools %v", web, bad)
		}
	}
}

func TestMateTools_ToolNamingProseNamesOnlyRegisteredTools(t *testing.T) {
	for _, file := range []string{
		"changeset.go", "record_type_maintenance.go", "record_type_inventory.go",
		"record_type_vessel.go", "record_types.go", "pdf_form_fill.go", "assistant_watch.go",
		"assistant_proposals.go", "assistant_propose_changes.go", "maintenance_commands.go",
	} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if bad := unregisteredToolTokens(string(src)); len(bad) > 0 {
			t.Errorf("%s names unregistered tools %v", file, bad)
		}
	}
}

func TestMateTools_TokenScanCatchesARenamedTool(t *testing.T) {
	if bad := unregisteredToolTokens("call find_place_names first"); len(bad) != 1 {
		t.Fatalf("scan missed an unregistered name: %v", bad)
	}
}
