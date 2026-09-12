package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

// testManualFS is a small fstest.MapFS standing in for the staged
// backend/manual tree, exercising loadManual/manualSection/manualIndexLine
// against exactly the shapes assistant_manual.go must handle: a page with
// an H1 title, a page with no H1 at all (falls back to its ID), a page with
// two "## " sections (the last one running to EOF), and the tracked
// .gitkeep placeholder (skipped, not treated as a page).
func testManualFS() fstest.MapFS {
	return fstest.MapFS{
		"manual/.gitkeep": &fstest.MapFile{Data: []byte{}},
		"manual/features/forecast.md": &fstest.MapFile{Data: []byte(
			"# Forecast\n\nIntro text.\n\n" +
				"## Steepness, not height\n\nBody one.\n\n" +
				"## Upper air, days ahead\n\nBody two.\nSecond line.\n",
		)},
		"manual/features/alarms.md": &fstest.MapFile{Data: []byte("# Alarms\n\nAlarm body.\n")},
		"manual/how-to/no-title.md": &fstest.MapFile{Data: []byte("Just a body, no H1 heading at all.\n")},
	}
}

func TestLoadManual_SortsByIDAndSkipsNonMarkdown(t *testing.T) {
	pages, err := loadManual(testManualFS(), "manual")
	if err != nil {
		t.Fatalf("loadManual: %v", err)
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

// TestLoadManual_RootLevelPageIDHasNoDirectory pins loadManual's handling of
// a page staged directly at the manual root - docs/index.md, staged as
// backend/manual/index.md by the Makefile's manual-stage target - rather
// than under one of the three directories: its ID must be the bare filename
// with the extension stripped ("index"), not "/index" or "index/index",
// since manualIndexLine and the in-app Manual sheet's /api/manual endpoint
// both use ID as the page's address.
func TestLoadManual_RootLevelPageIDHasNoDirectory(t *testing.T) {
	fsys := fstest.MapFS{
		"manual/index.md":           &fstest.MapFile{Data: []byte("# Helmcentral documentation\n\nContents.\n")},
		"manual/features/alarms.md": &fstest.MapFile{Data: []byte("# Alarms\n\nAlarm body.\n")},
	}
	pages, err := loadManual(fsys, "manual")
	if err != nil {
		t.Fatalf("loadManual: %v", err)
	}

	byID := map[string]manualPage{}
	for _, p := range pages {
		byID[p.ID] = p
	}
	index, ok := byID["index"]
	if !ok {
		var ids []string
		for id := range byID {
			ids = append(ids, id)
		}
		t.Fatalf(`expected a root-level manual/index.md to load as page id "index", got ids %v`, ids)
	}
	if index.Title != "Helmcentral documentation" {
		t.Fatalf("expected the root page's H1 as its title, got %q", index.Title)
	}
}

func TestLoadManual_TitleFromH1OrIDFallback(t *testing.T) {
	pages, err := loadManual(testManualFS(), "manual")
	if err != nil {
		t.Fatalf("loadManual: %v", err)
	}
	byID := map[string]manualPage{}
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

func TestManualSection_CaseInsensitiveLastSectionToEOFAndMissing(t *testing.T) {
	body := "# Forecast\n\nIntro.\n\n" +
		"## Steepness, not height\n\nBody one.\n\n" +
		"## Upper air, days ahead\n\nBody two.\nSecond line.\n"

	got, ok := manualSection(body, "upper air, days ahead")
	if !ok {
		t.Fatalf("expected a case-insensitive match")
	}
	if want := "Body two.\nSecond line."; got != want {
		t.Fatalf("expected the last section to run to EOF, got %q want %q", got, want)
	}

	got2, ok2 := manualSection(body, "Steepness, not height")
	if !ok2 || got2 != "Body one." {
		t.Fatalf("expected the middle section bounded by the next heading, got %q ok=%v", got2, ok2)
	}

	if _, ok3 := manualSection(body, "does not exist"); ok3 {
		t.Fatalf("expected no match for a heading that isn't on the page")
	}
}

func TestManualIndexLine(t *testing.T) {
	pages := []manualPage{
		{ID: "features/alarms", Title: "Alarms"},
		{ID: "features/forecast", Title: "Forecast"},
	}
	want := "Manual pages: features/alarms (Alarms), features/forecast (Forecast)"
	if got := manualIndexLine(pages); got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if got := manualIndexLine(nil); got != "" {
		t.Fatalf("expected an empty index line for no pages, got %q", got)
	}
}

func TestExecuteReadManual_SuccessWholePage(t *testing.T) {
	pages, err := loadManual(testManualFS(), "manual")
	if err != nil {
		t.Fatalf("loadManual: %v", err)
	}
	deps := assistantToolDeps{manual: func() []manualPage { return pages }}

	raw, err := deps.executeReadManual(json.RawMessage(`{"page":"features/forecast"}`))
	if err != nil {
		t.Fatalf("executeReadManual: %v", err)
	}
	var result assistantReadManualResult
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

func TestExecuteReadManual_SuccessWithSection(t *testing.T) {
	pages, err := loadManual(testManualFS(), "manual")
	if err != nil {
		t.Fatalf("loadManual: %v", err)
	}
	deps := assistantToolDeps{manual: func() []manualPage { return pages }}

	raw, err := deps.executeReadManual(json.RawMessage(`{"page":"features/forecast","section":"upper air, days ahead"}`))
	if err != nil {
		t.Fatalf("executeReadManual: %v", err)
	}
	var result assistantReadManualResult
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

func TestExecuteReadManual_UnknownPageListsValidIDs(t *testing.T) {
	pages, err := loadManual(testManualFS(), "manual")
	if err != nil {
		t.Fatalf("loadManual: %v", err)
	}
	deps := assistantToolDeps{manual: func() []manualPage { return pages }}

	if _, err := deps.executeReadManual(json.RawMessage(`{"page":"nope"}`)); err == nil {
		t.Fatalf("expected an error for an unknown page")
	} else if !strings.Contains(err.Error(), "features/forecast") {
		t.Fatalf("expected the error to list valid page ids, got %v", err)
	}
}

func TestExecuteReadManual_UnknownSectionListsHeadings(t *testing.T) {
	pages, err := loadManual(testManualFS(), "manual")
	if err != nil {
		t.Fatalf("loadManual: %v", err)
	}
	deps := assistantToolDeps{manual: func() []manualPage { return pages }}

	if _, err := deps.executeReadManual(json.RawMessage(`{"page":"features/forecast","section":"nope"}`)); err == nil {
		t.Fatalf("expected an error for an unknown section")
	} else if !strings.Contains(err.Error(), "Upper air, days ahead") {
		t.Fatalf("expected the error to list the page's valid headings, got %v", err)
	}
}

func TestExecuteReadManual_EmptyManualIsError(t *testing.T) {
	deps := assistantToolDeps{manual: func() []manualPage { return nil }}

	if _, err := deps.executeReadManual(json.RawMessage(`{"page":"features/forecast"}`)); err == nil {
		t.Fatalf("expected an error when the manual is empty")
	} else if !strings.Contains(err.Error(), "make manual-stage") {
		t.Fatalf("expected the error to name the fix, got %v", err)
	}
}

// TestLoadManual_RealDocsTreeHasForecastPageWithUpperAirSection loads the
// repo's actual docs/ tree (not the fstest fixture above) to catch drift
// between this loader and the real manual content it will actually serve -
// skipped when docs/features isn't present in this checkout (e.g. a build
// running from an extracted source archive without the docs tree).
func TestLoadManual_RealDocsTreeHasForecastPageWithUpperAirSection(t *testing.T) {
	if _, err := os.Stat("../docs/features"); err != nil {
		t.Skip("../docs/features not present in this checkout")
	}

	pages, err := loadManual(os.DirFS(".."), "docs")
	if err != nil {
		t.Fatalf("loadManual: %v", err)
	}

	var forecast *manualPage
	for i := range pages {
		if pages[i].ID == "features/forecast" {
			forecast = &pages[i]
			break
		}
	}
	if forecast == nil {
		t.Fatalf("expected docs/features/forecast.md to load as features/forecast")
	}
	if _, ok := manualSection(forecast.Body, "Upper air, days ahead"); !ok {
		t.Fatalf(`expected features/forecast to have an "Upper air, days ahead" section`)
	}
}
