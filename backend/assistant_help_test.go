package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

// testHelpFS is a small fstest.MapFS standing in for the staged
// backend/help tree, exercising loadHelp/helpSection/helpIndexLine
// against exactly the shapes assistant_help.go must handle: a page with
// an H1 title, a page with no H1 at all (falls back to its ID), a page with
// two "## " sections (the last one running to EOF), and the tracked
// .gitkeep placeholder (skipped, not treated as a page).
func testHelpFS() fstest.MapFS {
	return fstest.MapFS{
		"help/.gitkeep": &fstest.MapFile{Data: []byte{}},
		"help/features/forecast.md": &fstest.MapFile{Data: []byte(
			"# Forecast\n\nIntro text.\n\n" +
				"## Steepness, not height\n\nBody one.\n\n" +
				"## Upper air, days ahead\n\nBody two.\nSecond line.\n",
		)},
		"help/features/alarms.md": &fstest.MapFile{Data: []byte("# Alarms\n\nAlarm body.\n")},
		"help/how-to/no-title.md": &fstest.MapFile{Data: []byte("Just a body, no H1 heading at all.\n")},
	}
}

func TestLoadHelp_SortsByIDAndSkipsNonMarkdown(t *testing.T) {
	pages, err := loadHelp(testHelpFS(), "help")
	if err != nil {
		t.Fatalf("loadHelp: %v", err)
	}
	if len(pages) != 3 {
		t.Fatalf("expected 3 pages (.gitkeep excluded), got %d: %+v", len(pages), pages)
	}
	var ids []string
	for _, p := range pages {
		ids = append(ids, p.ID)
	}
	want := []string{"features/alarms", "features/forecast", "how-to/no-title"}
	for i, id := range want {
		if ids[i] != id {
			t.Fatalf("expected sorted ids %v, got %v", want, ids)
		}
	}
}

// TestLoadHelp_RootLevelPageIDHasNoDirectory pins loadHelp's handling of
// a page staged directly at the help root - docs/index.md, staged as
// backend/help/index.md by the Makefile's help-stage target - rather
// than under one of the three directories: its ID must be the bare filename
// with the extension stripped ("index"), not "/index" or "index/index",
// since helpIndexLine and the in-app Help sheet's /api/help endpoint
// both use ID as the page's address.
func TestLoadHelp_RootLevelPageIDHasNoDirectory(t *testing.T) {
	fsys := fstest.MapFS{
		"help/index.md":           &fstest.MapFile{Data: []byte("# Helmcentral documentation\n\nContents.\n")},
		"help/features/alarms.md": &fstest.MapFile{Data: []byte("# Alarms\n\nAlarm body.\n")},
	}
	pages, err := loadHelp(fsys, "help")
	if err != nil {
		t.Fatalf("loadHelp: %v", err)
	}

	byID := map[string]helpPage{}
	for _, p := range pages {
		byID[p.ID] = p
	}
	index, ok := byID["index"]
	if !ok {
		var ids []string
		for id := range byID {
			ids = append(ids, id)
		}
		t.Fatalf(`expected a root-level help/index.md to load as page id "index", got ids %v`, ids)
	}
	if index.Title != "Helmcentral documentation" {
		t.Fatalf("expected the root page's H1 as its title, got %q", index.Title)
	}
}

func TestLoadHelp_TitleFromH1OrIDFallback(t *testing.T) {
	pages, err := loadHelp(testHelpFS(), "help")
	if err != nil {
		t.Fatalf("loadHelp: %v", err)
	}
	byID := map[string]helpPage{}
	for _, p := range pages {
		byID[p.ID] = p
	}
	if byID["features/forecast"].Title != "Forecast" {
		t.Fatalf("expected the H1 title, got %q", byID["features/forecast"].Title)
	}
	if byID["how-to/no-title"].Title != "how-to/no-title" {
		t.Fatalf("expected the ID fallback title for a page with no H1, got %q", byID["how-to/no-title"].Title)
	}
}

func TestHelpSection_CaseInsensitiveLastSectionToEOFAndMissing(t *testing.T) {
	body := "# Forecast\n\nIntro.\n\n" +
		"## Steepness, not height\n\nBody one.\n\n" +
		"## Upper air, days ahead\n\nBody two.\nSecond line.\n"

	got, ok := helpSection(body, "upper air, days ahead")
	if !ok {
		t.Fatalf("expected a case-insensitive match")
	}
	if want := "Body two.\nSecond line."; got != want {
		t.Fatalf("expected the last section to run to EOF, got %q want %q", got, want)
	}

	got2, ok2 := helpSection(body, "Steepness, not height")
	if !ok2 || got2 != "Body one." {
		t.Fatalf("expected the middle section bounded by the next heading, got %q ok=%v", got2, ok2)
	}

	if _, ok3 := helpSection(body, "does not exist"); ok3 {
		t.Fatalf("expected no match for a heading that isn't on the page")
	}
}

func TestHelpIndexLine(t *testing.T) {
	pages := []helpPage{
		{ID: "features/alarms", Title: "Alarms"},
		{ID: "features/forecast", Title: "Forecast"},
	}
	want := "Help pages: features/alarms (Alarms), features/forecast (Forecast)"
	if got := helpIndexLine(pages); got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if got := helpIndexLine(nil); got != "" {
		t.Fatalf("expected an empty index line for no pages, got %q", got)
	}
}

func TestExecuteReadHelp_SuccessWholePage(t *testing.T) {
	pages, err := loadHelp(testHelpFS(), "help")
	if err != nil {
		t.Fatalf("loadHelp: %v", err)
	}
	deps := assistantToolDeps{help: func() []helpPage { return pages }}

	raw, err := deps.executeReadHelp(context.Background(), json.RawMessage(`{"page":"features/forecast"}`))
	if err != nil {
		t.Fatalf("executeReadHelp: %v", err)
	}
	var result assistantReadHelpResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Page != "features/forecast" || result.Title != "Forecast" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if !strings.Contains(result.Content, "Upper air, days ahead") {
		t.Fatalf("expected the whole page body when no section is requested, got %q", result.Content)
	}
}

func TestExecuteReadHelp_SuccessWithSection(t *testing.T) {
	pages, err := loadHelp(testHelpFS(), "help")
	if err != nil {
		t.Fatalf("loadHelp: %v", err)
	}
	deps := assistantToolDeps{help: func() []helpPage { return pages }}

	raw, err := deps.executeReadHelp(context.Background(), json.RawMessage(`{"page":"features/forecast","section":"upper air, days ahead"}`))
	if err != nil {
		t.Fatalf("executeReadHelp: %v", err)
	}
	var result assistantReadHelpResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Section != "upper air, days ahead" {
		t.Fatalf("expected the section to echo back, got %q", result.Section)
	}
	if result.Content != "Body two.\nSecond line." {
		t.Fatalf("expected only the requested section's body, got %q", result.Content)
	}
}

func TestExecuteReadHelp_UnknownPageListsValidIDs(t *testing.T) {
	pages, err := loadHelp(testHelpFS(), "help")
	if err != nil {
		t.Fatalf("loadHelp: %v", err)
	}
	deps := assistantToolDeps{help: func() []helpPage { return pages }}

	if _, err := deps.executeReadHelp(context.Background(), json.RawMessage(`{"page":"nope"}`)); err == nil {
		t.Fatalf("expected an error for an unknown page")
	} else if !strings.Contains(err.Error(), "features/forecast") {
		t.Fatalf("expected the error to list valid page ids, got %v", err)
	}
}

func TestExecuteReadHelp_UnknownSectionListsHeadings(t *testing.T) {
	pages, err := loadHelp(testHelpFS(), "help")
	if err != nil {
		t.Fatalf("loadHelp: %v", err)
	}
	deps := assistantToolDeps{help: func() []helpPage { return pages }}

	if _, err := deps.executeReadHelp(context.Background(), json.RawMessage(`{"page":"features/forecast","section":"nope"}`)); err == nil {
		t.Fatalf("expected an error for an unknown section")
	} else if !strings.Contains(err.Error(), "Upper air, days ahead") {
		t.Fatalf("expected the error to list the page's valid headings, got %v", err)
	}
}

func TestExecuteReadHelp_EmptyHelpIsError(t *testing.T) {
	deps := assistantToolDeps{help: func() []helpPage { return nil }}

	if _, err := deps.executeReadHelp(context.Background(), json.RawMessage(`{"page":"features/forecast"}`)); err == nil {
		t.Fatalf("expected an error when help is empty")
	} else if !strings.Contains(err.Error(), "make help-stage") {
		t.Fatalf("expected the error to name the fix, got %v", err)
	}
}

// TestLoadHelp_RealDocsTreeHasForecastPageWithUpperAirSection loads the
// repo's actual docs/ tree (not the fstest fixture above) to catch drift
// between this loader and the real help content it will actually serve -
// skipped when docs/features isn't present in this checkout (e.g. a build
// running from an extracted source archive without the docs tree).
func TestLoadHelp_RealDocsTreeHasForecastPageWithUpperAirSection(t *testing.T) {
	if _, err := os.Stat("../docs/features"); err != nil {
		t.Skip("../docs/features not present in this checkout")
	}

	pages, err := loadHelp(os.DirFS(".."), "docs")
	if err != nil {
		t.Fatalf("loadHelp: %v", err)
	}

	var forecast *helpPage
	for i := range pages {
		if pages[i].ID == "features/forecast" {
			forecast = &pages[i]
			break
		}
	}
	if forecast == nil {
		t.Fatalf("expected docs/features/forecast.md to load as features/forecast")
	}
	if _, ok := helpSection(forecast.Body, "Upper air, days ahead"); !ok {
		t.Fatalf(`expected features/forecast to have an "Upper air, days ahead" section`)
	}
}
