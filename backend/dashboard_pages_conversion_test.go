package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// One combined fixture carrying every case at once, as the boat's own file
// does, rather than the one-case-per-test battery beside it. A one-shot
// conversion breaks on the interactions between cases - a hero on a flagged
// page, a stale duration on an unflagged one, a flagged page with no
// duration at all - and a per-case test cannot see an interaction. This is
// also the test that pins idempotency, which only means anything against a
// file that exercised every branch on the first pass.
func TestLoadDashboardPages_ConvertsARealisticFileAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dashboard-pages.json")
	t.Setenv("DASHBOARD_PAGES_FILE", path)

	fixture := `{
	  "pages": [
	    {"id":"p1","name":"Anchored","position":0,"hero":"depth-tide",
	     "widgets":[{"id":"depth-tide","x":0,"y":0,"w":6,"h":4}],
	     "created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"},
	    {"id":"p2","name":"Engines","position":1,"kiosk_seconds":45,
	     "widgets":[{"id":"engine-cluster","x":0,"y":0,"w":12,"h":7}],
	     "created_at":"2026-01-02T00:00:00Z","updated_at":"2026-01-02T00:00:00Z"},
	    {"id":"p3","name":"Wall: Conditions","position":2,"kiosk":true,"kiosk_seconds":30,
	     "hero":"wall-clock",
	     "widgets":[{"id":"wall-clock","x":0,"y":0,"w":4,"h":4}],
	     "created_at":"2026-01-03T00:00:00Z","updated_at":"2026-01-03T00:00:00Z"},
	    {"id":"p4","name":"Wall: Anchor","position":3,"kiosk":true,"kiosk_seconds":60,
	     "kiosk_when":"anchored",
	     "widgets":[{"id":"anchor-watch","x":0,"y":0,"w":6,"h":6}],
	     "created_at":"2026-01-04T00:00:00Z","updated_at":"2026-01-04T00:00:00Z"},
	    {"id":"p5","name":"Wall: Broken","position":4,"kiosk":true,
	     "widgets":[{"id":"wall-sea-state","x":0,"y":0,"w":4,"h":4}],
	     "created_at":"2026-01-05T00:00:00Z","updated_at":"2026-01-05T00:00:00Z"}
	  ],
	  "ribbon": {"lamps":[{"id":"l1","label":"Bilge","path":"electrical.switches.bilge"}]}
	}`
	if err := os.WriteFile(path, []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}

	loadDashboardPages()

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Pages []struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			Hero         string `json:"hero"`
			DisplayID    string `json:"display_id"`
			DwellSeconds int    `json:"dwell_seconds"`
			ShowWhen     string `json:"show_when"`
		} `json:"pages"`
		Ribbon   json.RawMessage `json:"ribbon"`
		Displays []struct {
			Name   string `json:"name"`
			Slug   string `json:"slug"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
			Rotate int    `json:"rotate"`
		} `json:"displays"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("rewritten file does not parse: %v\n%s", err, out)
	}

	if len(got.Displays) != 1 {
		t.Fatalf("want 1 display, got %d", len(got.Displays))
	}
	d := got.Displays[0]
	if d.Name != "Flybridge" || d.Slug != "flybridge" || d.Width != 1920 || d.Height != 360 || d.Rotate != 180 {
		t.Errorf("flybridge display wrong: %+v", d)
	}
	id := ""
	for _, pg := range got.Pages {
		switch pg.ID {
		case "p1":
			if pg.DisplayID != "" || pg.Hero != "depth-tide" {
				t.Errorf("p1 (plain, has hero) should be untouched: %+v", pg)
			}
		case "p2":
			if pg.DisplayID != "" {
				t.Errorf("p2 (unflagged) must not be assigned: %+v", pg)
			}
			if pg.DwellSeconds != 45 {
				t.Errorf("p2 stale dwell must carry forward, got %d", pg.DwellSeconds)
			}
		case "p3":
			id = pg.DisplayID
			if pg.DisplayID == "" || pg.DwellSeconds != 30 {
				t.Errorf("p3 should be assigned with dwell 30: %+v", pg)
			}
			if pg.Hero != "" {
				t.Errorf("p3 hero must be cleared on assignment, got %q", pg.Hero)
			}
		case "p4":
			if pg.DisplayID == "" || pg.DwellSeconds != 60 || pg.ShowWhen != "anchored" {
				t.Errorf("p4 should be assigned with dwell 60 / anchored: %+v", pg)
			}
		case "p5":
			if pg.DisplayID != "" {
				t.Errorf("p5 (flagged, no dwell) must be left unassigned: %+v", pg)
			}
		}
	}
	if id == "" {
		t.Fatal("no display id assigned")
	}
	if len(got.Ribbon) == 0 || !strings.Contains(string(got.Ribbon), "Bilge") {
		t.Errorf("ribbon lost: %s", got.Ribbon)
	}
	if strings.Contains(string(out), `"kiosk`) {
		t.Errorf("rewritten file still carries a kiosk key:\n%s", out)
	}

	// Second load must change nothing.
	before := string(out)
	loadDashboardPages()
	after, _ := os.ReadFile(path)
	if string(after) != before {
		t.Errorf("second load rewrote the file:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}
