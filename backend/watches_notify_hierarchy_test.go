package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// How a report with questions and extending findings is told (ADR 0255): the
// title counts what to look at, an extending finding sits under its parent, and
// the email lists the questions after the review.

func mateExtending(section, text string, parent int) watchFinding {
	return watchFinding{Section: section, Rule: "mate_review", Text: text, By: watchByMate, Key: "k_" + section, Extends: &parent}
}

func TestWatchIssueCount_CountsFindingsThatDoNotExtendAnother(t *testing.T) {
	a := notifyRuleFinding("power", "overnight_draw", "A.")
	b := notifyRuleFinding("tanks", "tank_change", "B.")
	own := watchFinding{Section: "weather", Rule: "mate_review", Text: "C.", By: watchByMate, Key: "own_issue"}
	for name, tc := range map[string]struct {
		in   []watchFinding
		want int
	}{
		"none":                   {nil, 0},
		"rules only":             {[]watchFinding{a, b}, 2},
		"an extension is no new": {[]watchFinding{a, b, mateExtending("tanks", "D.", 1)}, 2},
		"Mate's own issue":       {[]watchFinding{a, own}, 2},
		"only extensions":        {[]watchFinding{mateExtending("tanks", "D.", 0)}, 0},
	} {
		if got := watchIssueCount(tc.in); got != tc.want {
			t.Errorf("%s: %d, want %d", name, got, tc.want)
		}
	}
}

func TestWatchReportMessage_TheTitleCountsWhatToLookAt(t *testing.T) {
	w := savedWatch{ID: "w1", Name: "Overnight check"}
	run := func(f ...watchFinding) watchRun {
		return watchRun{ID: "r1", WatchID: "w1", Outcome: watchOutcomeRan, Findings: f}
	}
	a := notifyRuleFinding("power", "overnight_draw", "A.")
	b := notifyRuleFinding("tanks", "tank_change", "B.")
	for _, tc := range []struct {
		want string
		run  watchRun
	}{
		{"Overnight check: nothing to report", run()},
		{"Overnight check: 1 to look at", run(a)},
		{"Overnight check: 2 to look at", run(a, b)},
		// Mate's addition to a rule finding is not another thing to look at.
		{"Overnight check: 1 to look at", run(a, mateExtending("power", "Detail.", 0))},
		{"Overnight check: 2 to look at", run(a, b, mateExtending("tanks", "Detail.", 1))},
	} {
		if got := buildWatchReportMessage(w, tc.run, "Pikorua", "").Title; got != tc.want {
			t.Errorf("title %q, want %q", got, tc.want)
		}
	}
}

func TestWatchReportMessage_AnExtendingFindingFollowsItsParent(t *testing.T) {
	a := notifyRuleFinding("power", "overnight_draw", "Draw A.")
	b := notifyRuleFinding("tanks", "tank_change", "Tank B.")
	c := notifyRuleFinding("anchor", "outside_anchor_radius", "Anchor C.")
	x := mateExtending("power", "Extra on A.", 0)
	y := mateExtending("tanks", "Extra on B.", 1)
	review := "All quiet."
	run := watchRun{ID: "r1", WatchID: "w1", Outcome: watchOutcomeRan, Findings: []watchFinding{a, b, c, x, y}, Review: &review}
	m := buildWatchReportMessage(savedWatch{ID: "w1", Name: "Night"}, run, "Pikorua", "")

	// The stored findings are not reordered: a chart names its finding by index.
	if m.Findings[3].Text != "Extra on A." || m.Findings[3].Extends == nil {
		t.Fatalf("findings %+v", m.Findings)
	}
	// The short message names the first three in reading order, then counts the rest.
	want := "Draw A.\nExtra on A.\nTank B.\n2 more findings.\nAll quiet."
	if m.Summary != want {
		t.Errorf("summary %q, want %q", m.Summary, want)
	}

	order := func(text string, parts ...string) {
		t.Helper()
		last := -1
		for _, p := range parts {
			i := strings.Index(text, p)
			if i < 0 || i < last {
				t.Errorf("%q is missing or out of order in:\n%s", p, text)
				return
			}
			last = i
		}
	}
	m.Charts = []watchChart{{FindingIndex: 0, Problem: "no history"}}
	text := watchEmailText(m)
	order(text, "- Draw A.", "Chart left out: no history", "- Extra on A.", "- Tank B.", "- Extra on B.", "- Anchor C.")
	page := watchEmailHTML(m)
	order(page, "Draw A.", "Chart left out: no history", "Extra on A.", "Tank B.", "Extra on B.", "Anchor C.")
}

func TestWatchReportMessage_TheEmailListsTheQuestionsAfterTheReview(t *testing.T) {
	review := "Diesel fell faster than the hours explain."
	run := watchRun{ID: "r1", WatchID: "w1", Outcome: watchOutcomeRan, Review: &review,
		Questions: []string{"Were the heaters on?", "Is the fridge <new>?"},
		Report:    []byte(`{"kind":"sections","sections":[{"name":"power","title":"Power","lines":["House bank 13.1 V"]}]}`),
		Findings:  []watchFinding{notifyRuleFinding("power", "overnight_draw", "Drew a lot.")}}
	m := buildWatchReportMessage(savedWatch{ID: "w1", Name: "Night"}, run, "Pikorua", "")

	text := watchEmailText(m)
	i, q, s := strings.Index(text, "Mate's review"), strings.Index(text, "- Were the heaters on?"), strings.Index(text, "Power\n")
	if i < 0 || q < i || s < q || !strings.Contains(text, "- Is the fridge <new>?") {
		t.Errorf("the questions follow the review and come before the sections:\n%s", text)
	}
	page := watchEmailHTML(m)
	i, q, s = strings.Index(page, "Mate's review"), strings.Index(page, "Were the heaters on?"), strings.Index(page, "<h3>Power</h3>")
	if i < 0 || q < i || s < q || !strings.Contains(page, "Is the fridge &lt;new&gt;?") {
		t.Errorf("the HTML lists them too, escaped:\n%s", page)
	}
	if none := watchEmailText(buildWatchReportMessage(savedWatch{ID: "w1", Name: "Night"}, watchRun{Review: &review}, "", "")); strings.Contains(none, "Questions") {
		t.Errorf("no questions, no heading:\n%s", none)
	}
}

func TestWatchReportMessage_TheWebhookPayloadGainsQuestionsAndExtends(t *testing.T) {
	review := "Diesel fell."
	run := watchRun{ID: "r1", WatchID: "w1", Outcome: watchOutcomeRan, Review: &review, Questions: []string{"Were the heaters on?"},
		Findings: []watchFinding{notifyRuleFinding("tanks", "tank_change", "Diesel down."), mateExtending("tanks", "With the heaters off.", 0)}}
	raw, err := json.Marshal(buildWatchReportMessage(savedWatch{ID: "w1", Name: "Night"}, run, "Pikorua", ""))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if qs, _ := got["questions"].([]any); len(qs) != 1 || qs[0] != "Were the heaters on?" {
		t.Errorf("questions %v", got["questions"])
	}
	findings := got["findings"].([]any)
	if _, has := findings[0].(map[string]any)["extends"]; has {
		t.Errorf("a rule finding has no extends: %v", findings[0])
	}
	if findings[1].(map[string]any)["extends"] != float64(0) {
		t.Errorf("the extending finding names its parent: %v", findings[1])
	}
	for _, k := range []string{"kind", "title", "summary", "watch", "run", "findings", "charts", "report", "review", "path"} {
		if _, has := got[k]; !has {
			t.Errorf("the payload lost %q", k)
		}
	}
	// A run with none sends an empty list, so a receiver can read it without a check.
	raw, _ = json.Marshal(buildWatchReportMessage(savedWatch{ID: "w1", Name: "Night"}, watchRun{}, "", ""))
	_ = json.Unmarshal(raw, &got)
	if qs, ok := got["questions"].([]any); !ok || len(qs) != 0 {
		t.Errorf("questions %v", got["questions"])
	}
}
