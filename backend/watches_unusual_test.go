package main

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

var unusualNow = time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)

func unusualResultWith(stretches ...unusualStretch) unusualResult {
	return unusualResult{Since: unusualNow.Add(-24 * time.Hour), Until: unusualNow, JudgedMinutes: 300, Stretches: stretches,
		Model: &unusualModelInfo{TrainedAt: unusualNow.Add(-6 * time.Hour)}}
}

func stretchAt(hour int, minutes int) unusualStretch {
	return oilStretch(time.Date(2026, 11, 10, hour, 10, 0, 0, time.UTC), minutes)
}

func TestBuildWatchUnusual_ShowsAtMostTwoLinesAndSaysHowManyWereLeftOut(t *testing.T) {
	res := unusualResultWith(stretchAt(2, 30), stretchAt(3, 20), stretchAt(4, 10))
	u := buildWatchUnusual(res, "metric", time.UTC, unusualNow)
	if u == nil || len(u.Lines) != 2 || u.JudgedMinutes != 300 {
		t.Fatalf("block = %+v", u)
	}
	if u.Lines[0] != "Port engine oil pressure ran low from 2.10 AM to 2.40 AM: 280 kPa against a usual 340 kPa at 1,800 to 2,000 rpm." {
		t.Fatalf("line = %q", u.Lines[0])
	}
	if u.Note != "1 more unusual stretch is not shown." {
		t.Fatalf("note = %q", u.Note)
	}
}

func TestBuildWatchUnusual_Cases(t *testing.T) {
	cases := []struct {
		name    string
		res     unusualResult
		hidden  bool
		note    string
		lineLen int
	}{
		{"something stood out", unusualResultWith(stretchAt(2, 30)), false, "", 1},
		{"judged, nothing stood out", unusualResultWith(), true, "", 0},
		{"the engines did not run", unusualResult{NotJudged: []unusualNotJudged{{Kind: "not_steady", Minutes: 4}}}, true, "", 0},
		{"no influx", unusualResult{ProblemKind: unusualNoInflux, Problem: "InfluxDB is not set up"}, false, "No learned normal: InfluxDB is not set up.", 0},
		{"no model yet", unusualResult{ProblemKind: unusualNeverTrained, Problem: normalNoModelYet}, false, normalNoModelYet, 0},
		{"training error", unusualResult{ProblemKind: unusualNoModel, Problem: `the one-minute bucket "b" could not be read: nope`}, false, `No learned normal: the one-minute bucket "b" could not be read: nope.`, 0},
		{"file unreadable", unusualResult{ProblemKind: unusualUnreadable, Problem: "parsing x: bad"}, false, "The learned normal could not be read: parsing x: bad.", 0},
		{"scoring read failed", unusualResult{ProblemKind: unusualError, Problem: "InfluxDB could not be read: timeout"}, false, "Unusual readings could not be checked: InfluxDB could not be read: timeout.", 0},
		{"only thin bands ran", unusualResult{NotJudged: []unusualNotJudged{
			{Kind: "not_steady", Minutes: 10},
			{Kind: "thin_band", Label: "1,400 to 1,600 rpm", Short: "1,400 to 1,600 rpm has 22 minutes of history, needs 60", Minutes: 60},
		}}, false, "Not judged: 1,400 to 1,600 rpm has 22 minutes of history, needs 60.", 0},
		{"only a missing reading", unusualResult{NotJudged: []unusualNotJudged{{Kind: "missing_reading", Reason: "no oil pressure reading", Minutes: 12}}}, false, "Not judged: no oil pressure reading for 12 minutes.", 0},
		{"thin bands but other minutes judged", func() unusualResult {
			r := unusualResultWith()
			r.NotJudged = []unusualNotJudged{{Kind: "thin_band", Label: "x", Short: "x has 1 minute of history, needs 60", Minutes: 3}}
			return r
		}(), true, "", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := buildWatchUnusual(c.res, "metric", time.UTC, unusualNow)
			if c.hidden {
				if u != nil {
					t.Fatalf("want hidden, got %+v", u)
				}
				return
			}
			if u == nil {
				t.Fatal("want a block")
			}
			if u.Note != c.note || len(u.Lines) != c.lineLen {
				t.Fatalf("block = %+v, want note %q and %d lines", u, c.note, c.lineLen)
			}
		})
	}
}

// A good model whose latest training run failed still scores, and the block
// says the model is not being kept up to date.
func TestBuildWatchUnusual_AFailedLatestTrainingRunIsSaidBesideWhatWasFound(t *testing.T) {
	res := unusualResultWith(stretchAt(2, 30))
	res.TrainingProblem = "InfluxDB could not be read: timeout"
	u := buildWatchUnusual(res, "metric", time.UTC, unusualNow)
	if u == nil || len(u.Lines) != 1 || u.Note != "The learned normal was not updated: InfluxDB could not be read: timeout." {
		t.Fatalf("block = %+v", u)
	}
	quiet := unusualResultWith()
	quiet.TrainingProblem = "InfluxDB could not be read: timeout"
	if u := buildWatchUnusual(quiet, "metric", time.UTC, unusualNow); u == nil || len(u.Lines) != 0 || !strings.Contains(u.Note, "was not updated") {
		t.Fatalf("a quiet window with a failing trainer still says so: %+v", u)
	}
}

// The block is a report field, not a section and not a finding, so the rules'
// findings and the issue count are what they were without it.
func TestBuildWatchReport_UnusualLinesAreNeverFindingsAndNeverCounted(t *testing.T) {
	w := savedWatch{ID: "w1", Name: "Night", Sections: allSections(), Thresholds: defaultWatchThresholds()}
	in := watchReportInput{Now: unusualNow, Loc: time.UTC, Since: unusualNow.Add(-time.Hour), Status: calmStatus(), Raised: []alarmRaisedGroup{{Title: "Anchor Dragging", Count: 2}}}
	without, findingsWithout := buildWatchReport(w, in)
	res := unusualResultWith(stretchAt(2, 30), stretchAt(3, 20))
	in.Unusual = &res
	with, findingsWith := buildWatchReport(w, in)

	if without.Unusual != nil {
		t.Fatalf("no result, no block: %+v", without.Unusual)
	}
	if with.Unusual == nil || len(with.Unusual.Lines) != 2 {
		t.Fatalf("block = %+v", with.Unusual)
	}
	if !reflect.DeepEqual(findingsWith, findingsWithout) || watchIssueCount(findingsWith) != watchIssueCount(findingsWithout) {
		t.Fatalf("findings changed:\n with    %+v\n without %+v", findingsWith, findingsWithout)
	}
	if len(with.Sections) != len(without.Sections) {
		t.Fatalf("the block is not a section")
	}
}

func TestWatchReport_AnOldReportWithoutTheFieldStillLoads(t *testing.T) {
	var r watchReport
	if err := json.Unmarshal([]byte(`{"kind":"sections","sections":[{"name":"power","title":"Power"}]}`), &r); err != nil || r.Unusual != nil {
		t.Fatalf("err %v unusual %+v", err, r.Unusual)
	}
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), "unusual") {
		t.Fatalf("an absent block is not written: %s", raw)
	}
}

func TestWatchReportForModel_KeepsTheUnusualBlock(t *testing.T) {
	stored := json.RawMessage(`{"kind":"sections","sections":[{"name":"power","title":"Power","rows":[{"label":"x"}],"summary":"s"}],"unusual":{"lines":["Port engine oil pressure ran low."],"judged_minutes":300}}`)
	out, err := watchReportForModel(stored)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	_ = json.Unmarshal(out, &got)
	if !strings.Contains(string(got["unusual"]), "ran low") {
		t.Fatalf("the model must see the lines: %s", out)
	}
	if strings.Contains(string(got["sections"]), "rows") {
		t.Fatalf("rows are still trimmed: %s", out)
	}
}

// The rule for the unusual lines rides with the report that has them, so the
// standing prompt stays short.
func TestWatchReviewMessage_CarriesTheUnusualRuleOnlyWhenTheReportHasUnusualLines(t *testing.T) {
	w := savedWatch{ID: "w1", Name: "Night"}
	with := watchReport{Kind: watchReportKindSections, Unusual: &watchUnusual{Lines: []string{"Port engine oil pressure ran low."}}}
	msg, err := watchReviewMessage(w, with, nil, watchPreviousRun{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"unusual" lines`, "learned normal", "not a rule", "worth a look", "never a finding or a cause"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the message does not say %q", want)
		}
	}
	for _, none := range []watchReport{
		{Kind: watchReportKindSections},
		{Kind: watchReportKindSections, Unusual: &watchUnusual{Note: "No learned normal yet."}},
	} {
		msg, err := watchReviewMessage(w, none, nil, watchPreviousRun{})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(msg, "learned normal, not a rule") {
			t.Errorf("no lines, no rule: %s", msg)
		}
	}
	prompt := buildWatchReviewPrompt(assistantPromptContext{}, "metric", unusualNow, time.UTC, false)
	if strings.Contains(prompt, "unusual") {
		t.Error("the standing prompt does not carry the rule")
	}
}

// The figures use the units of the boat's status. With stretches to word and no
// known units the block says so and shows no figure.
func TestBuildWatchReport_UnusualFiguresUseTheStatusUnitsAndNeverGuessOne(t *testing.T) {
	w := savedWatch{ID: "w1", Name: "Night", Sections: allSections(), Thresholds: defaultWatchThresholds()}
	res := unusualResultWith(stretchAt(2, 30))
	status := calmStatus()
	status.Units = "imperial"
	rep, _ := buildWatchReport(w, watchReportInput{Now: unusualNow, Loc: time.UTC, Since: unusualNow.Add(-time.Hour), Status: status, Unusual: &res})
	if rep.Unusual == nil || len(rep.Unusual.Lines) != 1 || !strings.Contains(rep.Unusual.Lines[0], "40.6 psi") {
		t.Fatalf("block = %+v", rep.Unusual)
	}

	rep, _ = buildWatchReport(w, watchReportInput{Now: unusualNow, Loc: time.UTC, Since: unusualNow.Add(-time.Hour), StatusProblem: "the boat's readings are not wired (telemetry)", Unusual: &res})
	if rep.Unusual == nil || len(rep.Unusual.Lines) != 0 || rep.Unusual.Note != "Unusual readings could not be worded: the boat's readings are not wired (telemetry)." {
		t.Fatalf("block = %+v", rep.Unusual)
	}
	noUnits := calmStatus()
	noUnits.Units = ""
	rep, _ = buildWatchReport(w, watchReportInput{Now: unusualNow, Loc: time.UTC, Since: unusualNow.Add(-time.Hour), Status: noUnits, Unusual: &res})
	if rep.Unusual == nil || len(rep.Unusual.Lines) != 0 || !strings.Contains(rep.Unusual.Note, "units are not known") {
		t.Fatalf("block = %+v", rep.Unusual)
	}

	// A problem needs no units to be said.
	problem := unusualResult{ProblemKind: unusualNoInflux, Problem: "InfluxDB is not set up"}
	rep, _ = buildWatchReport(w, watchReportInput{Now: unusualNow, Loc: time.UTC, Since: unusualNow.Add(-time.Hour), Unusual: &problem})
	if rep.Unusual == nil || rep.Unusual.Note != "No learned normal: InfluxDB is not set up." {
		t.Fatalf("block = %+v", rep.Unusual)
	}
}

func TestBuildWatchUnusual_AStretchThatCannotBeWordedInTheUnitsIsSaidNotShownInSI(t *testing.T) {
	old := describeDisplayFor
	describeDisplayFor = func(string, string, string) (string, func(float64) float64, error) {
		return "", nil, errors.New("no display unit for Pa")
	}
	defer func() { describeDisplayFor = old }()
	u := buildWatchUnusual(unusualResultWith(stretchAt(2, 30)), "metric", time.UTC, unusualNow)
	if u == nil || len(u.Lines) != 0 || !strings.HasPrefix(u.Note, "Unusual readings could not be worded: ") || strings.Contains(u.Note, "280") {
		t.Fatalf("block = %+v", u)
	}
}

func unusualRunWith(u *watchUnusual) watchRun {
	rep := watchReport{Kind: watchReportKindSections, Sections: []watchReportSection{{Name: "power", Title: "Power", Lines: []string{"House bank 13.1 V"}}}, Unusual: u}
	raw, _ := json.Marshal(rep)
	review := "All quiet."
	return watchRun{ID: "r1", WatchID: "w1", Outcome: watchOutcomeRan, Report: raw, Review: &review,
		Findings: []watchFinding{notifyRuleFinding("power", "overnight_draw", "Drew a lot.")}}
}

func TestWatchReportMessage_UnusualFollowsTheFindingsInTheEmailAndLeavesTheTitleAlone(t *testing.T) {
	u := &watchUnusual{Lines: []string{"Port engine oil pressure ran low from 2.10 AM to 2.40 AM."}, Note: "1 more unusual stretch is not shown."}
	withBlock := buildWatchReportMessage(savedWatch{ID: "w1", Name: "Night"}, unusualRunWith(u), "Pikorua", "")
	without := buildWatchReportMessage(savedWatch{ID: "w1", Name: "Night"}, unusualRunWith(nil), "Pikorua", "")
	if withBlock.Title != without.Title || withBlock.Summary != without.Summary {
		t.Fatalf("the push title and summary must not change: %q/%q vs %q/%q", withBlock.Title, withBlock.Summary, without.Title, without.Summary)
	}

	text := watchEmailText(withBlock)
	f, un, rev, sec := strings.Index(text, "- Drew a lot."), strings.Index(text, "Unusual\n"), strings.Index(text, "Mate's review"), strings.Index(text, "Power\n")
	if f < 0 || un < f || rev < un || sec < rev {
		t.Fatalf("Unusual comes after the findings and before the review:\n%s", text)
	}
	if !strings.Contains(text, "- Port engine oil pressure ran low") || !strings.Contains(text, "1 more unusual stretch is not shown.") {
		t.Fatalf("lines missing:\n%s", text)
	}
	if strings.Contains(watchEmailText(without), "Unusual") {
		t.Fatalf("no block, no heading:\n%s", watchEmailText(without))
	}
	page := watchEmailHTML(withBlock)
	if !strings.Contains(page, "Unusual") || !strings.Contains(page, "Port engine oil pressure ran low") {
		t.Fatalf("the HTML email carries it too:\n%s", page)
	}
	if strings.Contains(watchEmailHTML(without), ">Unusual<") {
		t.Fatal("no block, no heading in the HTML")
	}

	raw, _ := json.Marshal(withBlock)
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	rep, _ := got["report"].(map[string]any)
	if _, has := rep["unusual"]; !has {
		t.Fatalf("the webhook carries the block inside report: %s", raw)
	}
}

func TestGatherWatchReportInput_ReadsUnusualReadingsOnlyForAWatchThatChecksInstruments(t *testing.T) {
	env := newProposalEnv(t)
	d := statusTestDeps(t)
	d.alarmLog = func() *alarmLogStore { return nil }
	var asked []time.Duration
	var engine string
	d.unusualReadings = func(_ context.Context, since, until time.Time, eng string) unusualResult {
		asked = append(asked, until.Sub(since))
		engine = eng
		return unusualResultWith(stretchAt(2, 30))
	}
	now := vesselStatusTestNow
	w := savedWatch{ID: "w1", Name: "Morning", Kind: watchKindScheduled, Sections: []string{"instruments"}, Thresholds: defaultWatchThresholds()}
	in, err := gatherWatchReportInput(context.Background(), d, env.docs, w, now, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if in.Unusual == nil || len(asked) != 1 || asked[0] != 24*time.Hour || engine != "" {
		t.Fatalf("unusual %+v asked %v", in.Unusual, asked)
	}

	w.Sections = []string{"weather"}
	asked = nil
	in, err = gatherWatchReportInput(context.Background(), d, env.docs, w, now, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if in.Unusual != nil || len(asked) != 0 {
		t.Fatalf("a watch without instruments does not pay for the queries: %+v %v", in.Unusual, asked)
	}
}

func TestProductionToolDeps_WireTheUnusualReadingsReader(t *testing.T) {
	if assistantProductionToolDeps("../settings.yaml").unusualReadings == nil {
		t.Fatal("production deps must wire unusualReadings")
	}
}
