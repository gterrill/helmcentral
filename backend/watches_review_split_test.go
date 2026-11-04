package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The run report page reads a review in two parts, and an old run's boat time
// in the form new runs are written in. The server does both, on read.

func TestSplitWatchReview(t *testing.T) {
	cases := []struct{ name, in, opening, rest string }{
		{"one sentence", "Quiet night.", "Quiet night.", ""},
		{"two sentences on a line", "Quiet night. Bank fell 4%.", "Quiet night.", "Bank fell 4%."},
		{"rest keeps markdown", "**Quiet night.** Bank fell.\n\n- one\n- two", "Quiet night.", "Bank fell.\n\n- one\n- two"},
		{"bold opening, two more sentences", "**Quiet night.** Bank fell 4%. Fridge steady.", "Quiet night.", "Bank fell 4%. Fridge steady."},
		{"bold span holds two sentences", "**Bank fell. Fridge is up.** Check it tonight.", "Bank fell. Fridge is up.", "Check it tonight."},
		{"list has no opening", "1. Bank fell 4%\n2. Fridge up", "", "1. Bank fell 4%\n2. Fridge up"},
		{"dash list has no opening", "- Bank fell 4%\n- Fridge up", "", "- Bank fell 4%\n- Fridge up"},
		{"no. is not a sentence end", "Tank no. 2 fell overnight. Worth a look.", "Tank no. 2 fell overnight.", "Worth a look."},
		{"St. is not a sentence end", "Anchored off St. Helena overnight. Calm.", "Anchored off St. Helena overnight.", "Calm."},
		{"approx. is not a sentence end", "approx. 13 V at dawn. Fine.", "approx. 13 V at dawn.", "Fine."},
		{"emphasis opening", "*Quiet night.* Bank fell.", "Quiet night.", "Bank fell."},
		{"code and link", "The `bilge` count is [up](http://x). Check it.", "The bilge count is up.", "Check it."},
		{"minus sign stays", "-2% on the house bank since 10.00 PM.", "-2% on the house bank since 10.00 PM.", ""},
		{"hash number stays", "#1 alternator is hot.", "#1 alternator is hot.", ""},
		{"wrapped sentence", "The bank fell faster than\nusual overnight. Check it.", "The bank fell faster than usual overnight.", "Check it."},
		{"time with full stop", "Bank at 6.00 AM was full. Fine.", "Bank at 6.00 AM was full.", "Fine."},
		{"paragraph stops at a list", "Quiet night\n- one\n- two", "Quiet night", "- one\n- two"},
		{"snake case stays", "port_alternator is hot. Check.", "port_alternator is hot.", "Check."},
		{"star between digits stays", "3*4 is 12. Fine.", "3*4 is 12.", "Fine."},
		{"time is not a sentence end", "Report for 6.00 AM looks fine. Nothing odd.", "Report for 6.00 AM looks fine.", "Nothing odd."},
		{"question mark", "Is the bilge dry? Yes, it is.", "Is the bilge dry?", "Yes, it is."},
		{"heading marker dropped", "## All quiet\n\nNothing to report. Really.", "All quiet", "Nothing to report. Really."},
		{"opening is not capped", strings.Repeat("word ", 80) + "end. More.", strings.Repeat("word ", 80) + "end.", "More."},
		{"empty", "  \n ", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o, r := splitWatchReview(tc.in)
			if o != tc.opening || r != tc.rest {
				t.Fatalf("split(%q) = %q, %q; want %q, %q", tc.in, o, r, tc.opening, tc.rest)
			}
		})
	}
}

func TestWatchFirstSentenceStillCapsForNotifications(t *testing.T) {
	long := strings.Repeat("word ", 80) + "end. More."
	got := watchFirstSentence(long)
	if n := len([]rune(got)); n != watchNotifySentenceChars || !strings.HasSuffix(got, "…") {
		t.Fatalf("capped sentence is %d runes: %q", n, got)
	}
	if got := watchFirstSentence("**Quiet night.** Bank fell."); got != "Quiet night." {
		t.Fatalf("got %q", got)
	}
	// A review that opens with a list still gives a push its line.
	if got := watchFirstSentence("1. Bank fell 4%. Check.\n2. Fridge up"); got != "Bank fell 4%." {
		t.Fatalf("list review gave %q", got)
	}
	if got := watchFirstSentence("- **Bank fell 4%**\n- Fridge up"); got != "Bank fell 4%" {
		t.Fatalf("list review gave %q", got)
	}
}

func TestNormaliseWatchBoatTime(t *testing.T) {
	cases := map[string]string{
		"Tue 3 Nov 06:00 AEST":                          "Tue 3 Nov 6.00 AM AEST",
		"Tue 3 Nov 14:30 AEST":                          "Tue 3 Nov 2.30 PM AEST",
		"Tue 3 Nov 00:05 UTC+12":                        "Tue 3 Nov 12.05 AM UTC+12",
		"Tue 3 Nov 12:00 AEST":                          "Tue 3 Nov 12.00 PM AEST",
		"Tue 3 Nov 06:00 UTC (boat time not known)":     "Tue 3 Nov 6.00 AM UTC (boat time not known)",
		"Wed 4 Nov 6.00 AM AEST":                        "Wed 4 Nov 6.00 AM AEST",
		"Thu 29 Oct 10.00 AM UTC (boat time not known)": "Thu 29 Oct 10.00 AM UTC (boat time not known)",
		"":    "",
		"now": "now",
	}
	for in, want := range cases {
		if got := normaliseWatchBoatTime(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
	// What the boat clock writes with no location is already the new form.
	at := time.Date(2026, 11, 4, 6, 0, 0, 0, time.UTC)
	noLoc := watchBoatClock(at, nil)
	if noLoc != "Wed 4 Nov 6.00 AM UTC (boat time not known)" || normaliseWatchBoatTime(noLoc) != noLoc {
		t.Fatalf("no-location clock %q", noLoc)
	}
}

func TestWatchREST_RunsCarryTheSplitReviewAndNewBoatTime(t *testing.T) {
	env := newProposalEnv(t)
	c, rec := watchRequestTo(http.MethodPost, "/api/watches", `{"name":"Night","schedule":"0 6 * * *","sections":["power"]}`)
	if err := createWatchHandler(c); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("create: %v %d %s", err, rec.Code, rec.Body)
	}
	var created savedWatch
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	review := "Quiet night. Bank fell 4%.\n\n- one"
	if err := env.docs.RunTx("run", func(tx *sql.Tx, _ time.Time) error {
		return insertWatchRunTx(tx, watchRun{ID: "r1", WatchID: created.ID, StartedAt: env.clock, FinishedAt: env.clock, Outcome: watchOutcomeRan,
			Report: json.RawMessage(`{"kind":"sections"}`), Review: &review, BoatTime: "Tue 3 Nov 14:30 AEST"}, true)
	}); err != nil {
		t.Fatal(err)
	}

	check := func(label string, run watchRun, raw string) {
		t.Helper()
		if run.ReviewOpening != "Quiet night." || run.ReviewRest != "Bank fell 4%.\n\n- one" {
			t.Errorf("%s: opening %q rest %q", label, run.ReviewOpening, run.ReviewRest)
		}
		if run.BoatTime != "Tue 3 Nov 2.30 PM AEST" {
			t.Errorf("%s: boat time %q", label, run.BoatTime)
		}
		if run.Review == nil || *run.Review != review || !strings.Contains(raw, `"review":"Quiet night.`) {
			t.Errorf("%s: the review stays whole in the JSON: %s", label, raw)
		}
	}

	c, rec = watchRequestTo(http.MethodGet, "/api/watches/"+created.ID+"?limit=5", "", "id", created.ID)
	if err := getWatchHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("get: %v %d", err, rec.Code)
	}
	var detail struct {
		Runs []watchRun `json:"runs"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &detail)
	if len(detail.Runs) != 1 {
		t.Fatalf("runs %+v", detail.Runs)
	}
	check("list", detail.Runs[0], rec.Body.String())

	c, rec = watchRequestTo(http.MethodGet, "/api/watches/"+created.ID+"/runs/r1", "", "id", created.ID, "runId", "r1")
	if err := getWatchRunHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("linked: %v %d", err, rec.Code)
	}
	var one watchRun
	_ = json.Unmarshal(rec.Body.Bytes(), &one)
	check("linked", one, rec.Body.String())

	// Mate's tools read no split, but every reader gets the one time label. A run
	// with no review carries neither field.
	if err := env.docs.Read(func(q sqlQueryer) error {
		stored, err := watchRunByIDQ(q, created.ID, "r1")
		if err != nil {
			return err
		}
		if stored.ReviewOpening != "" || stored.ReviewRest != "" || stored.BoatTime != "Tue 3 Nov 2.30 PM AEST" {
			t.Errorf("run read from the store: %+v", stored)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestWatchRunForPage_ListReviewGivesTheWholeRest(t *testing.T) {
	review := "- Bank fell 4%\n- Fridge up"
	got := watchRun{Review: &review}.forPage()
	if got.ReviewOpening != "" || got.ReviewRest != review {
		t.Fatalf("opening %q rest %q", got.ReviewOpening, got.ReviewRest)
	}
}

func TestWatchRunJSON_NoReviewOmitsTheSplitFields(t *testing.T) {
	raw, _ := json.Marshal(watchRun{ID: "r"})
	if strings.Contains(string(raw), "review_opening") || strings.Contains(string(raw), "review_rest") {
		t.Fatalf("empty fields appear: %s", raw)
	}
}
