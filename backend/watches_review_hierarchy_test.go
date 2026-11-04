package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// What a review may hold now that the page ranks its parts (ADR 0255): a short
// review, its questions apart, findings that say which rule finding they add
// to, and one chart per reading.

func TestWatchReviewSentenceCount(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		want     int
	}{
		{"empty", "", 0},
		{"blank", "  \n ", 0},
		{"one", "Quiet night.", 1},
		{"no stop", "Quiet night", 1},
		{"two", "Diesel fell. Water is fine.", 2},
		{"a time is not an end", "The bank was at 100 % at 6.00 AM. Fine.", 2},
		{"a decimal is not an end", "The pump drew 3.5 A. Then it stopped at 1.30 PM. Fine.", 3},
		{"no. is not an end", "Check tank no. 2 before noon. Fine.", 2},
		{"approx. is not an end", "It is approx. 5 nm away. Fine.", 2},
		{"Mr. is not an end", "Mr. Smith left.", 1},
		{"a wrapped sentence", "Diesel fell\nfast. Water fine.", 2},
		{"two paragraphs", "One.\n\nTwo.", 2},
		{"a list is one each", "- a\n- b\n- c", 3},
		{"bold", "**Bold.** Next.", 2},
		{"ellipsis", "Well... fine.", 2},
		{"exclamation", "Look! Now.", 2},
		{"five", "A. B. C. D. E.", 5},
	} {
		if got := watchReviewSentenceCount(tc.in); got != tc.want {
			t.Errorf("%s: %q counts %d, want %d", tc.name, tc.in, got, tc.want)
		}
	}
}

func submitJSON(t *testing.T, v map[string]any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestWatchReviewSubmit_TheReviewHoldsNoQuestionAndAtMostFourSentences(t *testing.T) {
	tools := &watchReviewTools{}
	_, err := tools.submit(submitJSON(t, map[string]any{"review": "Diesel fell. Were the heaters on?", "added_findings": []any{}}))
	if err == nil || !strings.Contains(err.Error(), "questions") {
		t.Fatalf("a question in the review is sent to the questions field: %v", err)
	}
	if tools.submission() != nil {
		t.Fatal("a refused review records nothing")
	}

	five := "One. Two. Three. Four. Five."
	_, err = tools.submit(submitJSON(t, map[string]any{"review": five, "added_findings": []any{}}))
	if err == nil || !strings.Contains(err.Error(), "four") {
		t.Fatalf("five sentences are refused: %v", err)
	}
	if tools.submission() != nil {
		t.Fatal("a refused review records nothing")
	}

	four := "Quiet night at 6.00 AM. Diesel is down 11 points. Check tank no. 2 today. Nothing else."
	if _, err := tools.submit(submitJSON(t, map[string]any{"review": four, "added_findings": []any{}})); err != nil {
		t.Fatalf("four sentences with a time and an abbreviation are taken: %v", err)
	}
}

func TestWatchReviewSubmit_AFindingWithAQuestionMarkIsSentToTheQuestionsField(t *testing.T) {
	tools := &watchReviewTools{}
	item := map[string]any{"section": "tanks", "key": "diesel_low", "text": "Is the diesel falling?"}
	_, err := tools.submit(submitJSON(t, map[string]any{"review": "Fine.", "added_findings": []any{item}}))
	if err == nil || !strings.Contains(err.Error(), "questions belong in questions") || strings.Contains(err.Error(), "belong in the review") {
		t.Fatalf("the refusal must point at the questions field, not the review: %v", err)
	}
	if tools.submission() != nil {
		t.Fatal("a refused review records nothing")
	}
}

func TestWatchReviewSubmit_QuestionsAreCheckedAndKept(t *testing.T) {
	long := strings.Repeat("x", 200) + "?"
	for name, qs := range map[string][]string{
		"four":            {"a?", "b?", "c?", "d?"},
		"no question end": {"Is the pump on"},
		"too long":        {long},
		"blank":           {"  "},
	} {
		tools := &watchReviewTools{}
		_, err := tools.submit(submitJSON(t, map[string]any{"review": "Fine.", "added_findings": []any{}, "questions": qs}))
		if err == nil || !strings.Contains(err.Error(), "questions") || tools.submission() != nil {
			t.Errorf("%s should be refused naming questions, got %v", name, err)
		}
	}
	tools := &watchReviewTools{}
	ok := []string{" Were the heaters on? ", "Is the fridge new?", strings.Repeat("y", 199) + "?"}
	if _, err := tools.submit(submitJSON(t, map[string]any{"review": "Fine.", "added_findings": []any{}, "questions": ok})); err != nil {
		t.Fatal(err)
	}
	if got := tools.submission().Questions; len(got) != 3 || got[0] != "Were the heaters on?" {
		t.Errorf("questions %q", got)
	}
	// Leaving it out is none.
	tools = &watchReviewTools{}
	if _, err := tools.submit(submitArgs("Fine.")); err != nil || len(tools.submission().Questions) != 0 {
		t.Errorf("no questions: %v", err)
	}
}

func extendsItem(extends any) map[string]any {
	item := map[string]any{"section": "tanks", "key": "diesel_low", "text": "Diesel fell with the heaters off."}
	if extends != nil {
		item["extends"] = extends
	}
	return item
}

func TestWatchReviewSubmit_ExtendsPointsAtARuleFinding(t *testing.T) {
	rules := []watchFinding{{Section: "tanks", Rule: "tank_change", By: watchByRules}, {Section: "power", Rule: "overnight_draw", By: watchByRules}}
	submit := func(tools *watchReviewTools, extends any) error {
		_, err := tools.submit(submitJSON(t, map[string]any{"review": "Fine.", "added_findings": []any{extendsItem(extends)}}))
		return err
	}
	for name, extends := range map[string]any{"negative": -1, "past the end": 2, "fraction": 1.5, "text": "0"} {
		tools := &watchReviewTools{findings: rules}
		if err := submit(tools, extends); err == nil || !strings.Contains(err.Error(), "extends") || tools.submission() != nil {
			t.Errorf("%s should be refused naming extends, got %v", name, err)
		}
	}
	tools := &watchReviewTools{}
	if err := submit(tools, 0); err == nil || !strings.Contains(err.Error(), "extends") {
		t.Errorf("with no rule findings there is nothing to extend: %v", err)
	}
	tools = &watchReviewTools{findings: rules}
	if err := submit(tools, 1); err != nil {
		t.Fatal(err)
	}
	if got := tools.submission().Added[0].Extends; got == nil || *got != 1 {
		t.Errorf("extends %v", got)
	}
	tools = &watchReviewTools{findings: rules}
	if err := submit(tools, nil); err != nil || tools.submission().Added[0].Extends != nil {
		t.Errorf("extends is optional: %v", err)
	}
}

func TestWatchReviewSubmit_AReadingARuleFindingChartsIsNotChartedAgain(t *testing.T) {
	rules := []watchFinding{
		{Section: "tanks", Rule: "tank_change", By: watchByRules, chart: &watchChartSpec{Series: []watchChartSeriesSpec{{Label: "Diesel", Path: "tanks.fuel.0.currentLevel"}}}},
		{Section: "weather", Rule: "pressure_fall", By: watchByRules, chart: &watchChartSpec{Series: []watchChartSeriesSpec{{Label: "Pressure", Barometer: true}}}},
		{Section: "anchor", Rule: "outside_anchor_radius", By: watchByRules},
	}
	chart := func(path string) json.RawMessage {
		return submitWithChart(`{"path":"` + path + `","hours":6,"label":"Diesel"}`)
	}
	tools := &watchReviewTools{findings: rules}
	_, err := tools.submit(chart("tanks.fuel.0.currentLevel"))
	if err == nil || !strings.Contains(err.Error(), "already charted") || tools.submission() != nil {
		t.Fatalf("a repeated chart is refused: %v", err)
	}
	if _, err := tools.submit(chart("electrical.pump.current")); err != nil {
		t.Fatalf("another reading is charted: %v", err)
	}
}

func TestWatchReview_QuestionsAndExtendsComeOutOfTheReview(t *testing.T) {
	withReviewRunner(t, func(ctx context.Context, tools assistantToolExecutor) (assistantReply, error) {
		item := extendsItem(0)
		_, _, err := tools.execute(ctx, watchReviewToolName, submitJSON(t, map[string]any{
			"review": "Diesel fell faster than the hours explain.", "added_findings": []any{item}, "questions": []string{"Were the heaters on?"}}))
		return assistantReply{Content: "done", Model: "m"}, err
	})
	w, report, findings := reviewInputs() // one rule finding: the diesel fall
	out := runWatchReview(context.Background(), reviewConfig(t), w, report, findings, watchTestTime(29, 6, 0), time.UTC)
	if out.Problem != "" || len(out.Questions) != 1 || out.Questions[0] != "Were the heaters on?" {
		t.Fatalf("%+v", out)
	}
	if len(out.Added) != 1 || out.Added[0].Extends == nil || *out.Added[0].Extends != 0 {
		t.Fatalf("added %+v", out.Added)
	}
}

func TestWatchReview_AnExtendsPastTheRuleFindingsIsRefusedInTheRealToolSet(t *testing.T) {
	var refused error
	withReviewRunner(t, func(ctx context.Context, tools assistantToolExecutor) (assistantReply, error) {
		_, _, refused = tools.execute(ctx, watchReviewToolName, submitJSON(t, map[string]any{
			"review": "Diesel fell.", "added_findings": []any{extendsItem(9)}}))
		return assistantReply{Content: "done", Model: "m"}, nil
	})
	w, report, findings := reviewInputs()
	out := runWatchReview(context.Background(), reviewConfig(t), w, report, findings, watchTestTime(29, 6, 0), time.UTC)
	if refused == nil || !strings.Contains(refused.Error(), "extends") {
		t.Fatalf("the executor knows the rule findings: %v", refused)
	}
	if out.Review != "" || out.Problem == "" {
		t.Errorf("a review never validly submitted is a problem, not a quiet acceptance: %+v", out)
	}
}

func TestWatchReview_ARefusedReviewThatIsNotResubmittedEndsAsAProblem(t *testing.T) {
	withReviewRunner(t, func(ctx context.Context, tools assistantToolExecutor) (assistantReply, error) {
		if _, _, err := tools.execute(ctx, watchReviewToolName, submitJSON(t, map[string]any{"review": "All fine. Is it?", "added_findings": []any{}})); err == nil {
			return assistantReply{}, errors.New("the question in the review should have been refused")
		}
		assistantCostMeterFrom(ctx).add(0.02)
		return assistantReply{Content: "done", Model: "m"}, nil
	})
	w, report, findings := reviewInputs()
	out := runWatchReview(context.Background(), reviewConfig(t), w, report, findings, watchTestTime(29, 6, 0), time.UTC)
	if out.Review != "" || out.Problem != "Mate finished without submitting a review" || !out.Reached || out.CostUSD != 0.02 {
		t.Fatalf("%+v", out)
	}
}

func TestWatchReview_LastTimesQuestionsGoInSoTheyAreNotAskedAgain(t *testing.T) {
	fake := withReviewRunner(t, func(ctx context.Context, tools assistantToolExecutor) (assistantReply, error) {
		return assistantReply{}, errors.New("stop here")
	})
	w, report, findings := reviewInputs()
	ctx := withWatchPreviousRun(context.Background(), watchPreviousRun{Review: "Diesel fell.", Questions: []string{"Were the heaters on?"}})
	runWatchReview(ctx, reviewConfig(t), w, report, findings, watchTestTime(29, 6, 0), time.UTC)
	message := ""
	for _, m := range fake.history {
		message += string(m.Content)
	}
	for _, want := range []string{`"previous_review_you_wrote":"Diesel fell."`, `"previous_questions_you_asked":["Were the heaters on?"]`} {
		if !strings.Contains(message, want) {
			t.Errorf("the review input lacks %s:\n%s", want, message)
		}
	}
	for _, want := range []string{"previous_questions_you_asked", "Do not repeat a question"} {
		if !strings.Contains(fake.system, want) {
			t.Errorf("the prompt lacks %q", want)
		}
	}
}

func TestWatchReviewToolSchema(t *testing.T) {
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(watchReviewToolDef.Parameters, &schema); err != nil {
		t.Fatal(err)
	}
	var qs struct {
		Type     string `json:"type"`
		MaxItems int    `json:"maxItems"`
		Items    struct {
			Type      string `json:"type"`
			MaxLength int    `json:"maxLength"`
		} `json:"items"`
	}
	if err := json.Unmarshal(schema.Properties["questions"], &qs); err != nil {
		t.Fatal(err)
	}
	if qs.Type != "array" || qs.MaxItems != 3 || qs.Items.Type != "string" || qs.Items.MaxLength != 200 {
		t.Errorf("questions %+v", qs)
	}
	var added struct {
		Items struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"items"`
	}
	if err := json.Unmarshal(schema.Properties["added_findings"], &added); err != nil {
		t.Fatal(err)
	}
	if _, ok := added.Items.Properties["extends"]; !ok {
		t.Error("added_findings carries extends")
	}
}

func TestWatchRun_RuleFindingsComeFirstSoExtendsKeepsPointingAtTheRightOne(t *testing.T) {
	f := newWatchHostFixture(t)
	f.add("Hourly", "0 * * * *")
	one, two := 1, 0
	f.findings = []watchFinding{notifyRuleFinding("power", "overnight_draw", "Drew a lot."), notifyRuleFinding("anchor", "outside_radius", "Outside.")}
	f.review.Added = []watchFinding{
		{Section: "anchor", Rule: "mate_review", Text: "The wind backed.", By: watchByMate, Key: "wind_backed", Extends: &one},
		{Section: "power", Rule: "mate_review", Text: "The fridge is new.", By: watchByMate, Key: "new_fridge", Extends: &two},
	}
	f.review.Questions = []string{"Is the fridge new?"}
	f.host.tick(context.Background())
	runs := f.runsOf("id-Hourly")
	if len(runs) != 1 {
		t.Fatalf("runs %d", len(runs))
	}
	got := runs[0].Findings
	if len(got) != 4 || got[0].By != watchByRules || got[1].By != watchByRules || got[2].By != watchByMate || got[3].By != watchByMate {
		t.Fatalf("rule findings first: %+v", got)
	}
	for _, i := range []int{2, 3} {
		parent := got[*got[i].Extends]
		if parent.By != watchByRules || parent.Section != got[i].Section {
			t.Errorf("finding %d extends %d, which is %+v", i, *got[i].Extends, parent)
		}
	}
	if len(runs[0].Questions) != 1 || runs[0].Questions[0] != "Is the fridge new?" {
		t.Errorf("the questions are kept on the run: %q", runs[0].Questions)
	}
}

func TestWatchRun_TheNextReviewIsToldLastTimesQuestions(t *testing.T) {
	f := newWatchHostFixture(t)
	f.add("Hourly", "0 * * * *")
	var seen watchPreviousRun
	f.host.review = func(ctx context.Context, _ watchConfig, w savedWatch, _ watchReport, _ []watchFinding, _ time.Time, _ *time.Location) watchReviewOutcome {
		seen = watchPreviousRunFrom(ctx)
		return watchReviewOutcome{Review: "Fine.", Questions: []string{"Is the fridge new?"}, Reached: true, CostKnown: true}
	}
	f.host.tick(context.Background())
	f.nextHour()
	if len(seen.Questions) != 1 || seen.Questions[0] != "Is the fridge new?" {
		t.Errorf("previous run %+v", seen)
	}
}

// The page's rows and summary repeat the lines, so the model is not sent them,
// and the stored report keeps them.
func TestWatchReviewMessage_LeavesOutTheRowsThatRepeatTheLines(t *testing.T) {
	report := watchReport{Sections: []watchReportSection{{
		Name:       watchSectionPower,
		Lines:      []string{"House bank: 80 %"},
		Rows:       []watchReportRow{{Label: "House bank", Value: "80 %"}},
		Summary:    "ok",
		NotChecked: []string{"Overnight house bank draw: no house bank role set"},
	}}}
	msg, err := watchReviewMessage(savedWatch{Name: "Night"}, report, nil, watchPreviousRun{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(msg, `"rows"`) || strings.Contains(msg, `"summary"`) {
		t.Errorf("the model was sent the page's rows or summary: %s", msg)
	}
	for _, want := range []string{`"lines"`, `"not_checked"`, "House bank: 80 %"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the message lost %s: %s", want, msg)
		}
	}
	if len(report.Sections[0].Rows) != 1 || report.Sections[0].Summary != "ok" {
		t.Errorf("the stored report was changed: %+v", report.Sections[0])
	}
}
