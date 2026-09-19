package main

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// This file embeds Helmcentral's own in-app help - docs/features,
// docs/how-to and docs/reference, staged into backend/help by the
// Makefile's help-stage target, the Dockerfile and .goreleaser.yaml - and
// exposes it to the onboard assistant, Mate, as a read_help tool
// (assistant_tools.go): the mate-voice-assistant plan's "App-wide voice"
// section, so a question about the app itself ("explain the upper
// atmosphere graph") gets answered from the docs' own words rather than a
// model's guess. Staging mirrors how backend/dist stages the built frontend
// for static.go's //go:embed - all:dist there, all:help here, both with a
// tracked .gitkeep placeholder so a fresh clone still `go build`s with
// nothing staged.

//go:embed all:help
var helpFS embed.FS

// globalHelp is the help loaded once at startup (main()), read by
// assistantProductionToolDeps' help func. Empty on a host `go build` that
// never ran help-stage - a legitimate developer build, not an error - in
// which case read_help reports the gap to the model itself rather than
// failing startup.
var globalHelp []helpPage

// helpPage is one help page.
type helpPage struct {
	// ID is the page's path under help/ with the .md extension removed
	// (e.g. "features/forecast") - what read_help's page argument names
	// and what helpIndexLine lists in the system prompt.
	ID    string
	Title string
	Body  string
}

// loadHelp walks root in fsys and returns every *.md page under it,
// sorted by ID. fsys/root are parameters rather than reading helpFS
// directly so tests can drive this against an fstest.MapFS, or a real
// docs/ tree via os.DirFS, with no embedding involved. Any file that is not
// a .md page (the tracked .gitkeep placeholder, most often) is skipped, not
// an error - loadHelp's job is to find pages, not to validate the tree.
func loadHelp(fsys fs.FS, root string) ([]helpPage, error) {
	var pages []helpPage
	err := fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk help tree at %q: %w", p, err)
		}
		if d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}

		data, rerr := fs.ReadFile(fsys, p)
		if rerr != nil {
			return fmt.Errorf("read help page %q: %w", p, rerr)
		}

		id := strings.TrimSuffix(strings.TrimPrefix(p, root+"/"), ".md")
		body := string(data)
		pages = append(pages, helpPage{ID: id, Title: helpPageTitle(body, id), Body: body})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(pages, func(i, j int) bool { return pages[i].ID < pages[j].ID })
	return pages, nil
}

// helpPageTitle returns body's first "# " heading, trimmed, or id when
// the page carries no H1 at all - a page missing its title still needs a
// name the model (and helpIndexLine) can show.
func helpPageTitle(body, id string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		}
	}
	return id
}

// helpSection returns the text of body's "## " heading whose title
// matches heading case-insensitively (both trimmed), from just after that
// heading line up to the next "## " heading or the end of the page. ok is
// false when no such heading exists on this page.
func helpSection(body, heading string) (string, bool) {
	wanted := strings.ToLower(strings.TrimSpace(heading))
	lines := strings.Split(body, "\n")

	start := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "## ") {
			continue
		}
		title := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")))
		if title == wanted {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return "", false
	}

	end := len(lines)
	for i := start; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
			end = i
			break
		}
	}
	return strings.TrimSpace(strings.Join(lines[start:end], "\n")), true
}

// helpPageHeadings lists a page's "## " headings in document order, for
// read_help's unknown-section error.
func helpPageHeadings(body string) []string {
	var headings []string
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			headings = append(headings, strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")))
		}
	}
	return headings
}

// helpIndexLine renders pages as the system prompt's help index, e.g.
// "Help pages: features/alarms (Alarms), features/forecast (Forecast)" -
// so the model knows what read_help can actually return before calling
// it. "" when pages is empty, so buildAssistantSystemPrompt can omit the
// line entirely on a build with no help staged.
func helpIndexLine(pages []helpPage) string {
	if len(pages) == 0 {
		return ""
	}
	parts := make([]string, 0, len(pages))
	for _, p := range pages {
		parts = append(parts, fmt.Sprintf("%s (%s)", p.ID, p.Title))
	}
	return "Help pages: " + strings.Join(parts, ", ")
}
