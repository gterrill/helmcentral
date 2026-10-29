package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// Mate's review of a watch report (ADR 0227).

type fakeReviewRunner struct {
	tools   assistantToolExecutor
	script  func(ctx context.Context, tools assistantToolExecutor) (assistantReply, error)
	system  string
	history []openRouterMessage
}

func (f *fakeReviewRunner) run(ctx context.Context, systemStable, systemLive string, history []openRouterMessage) (assistantReply, error) {
	f.system, f.history = systemStable+systemLive, history
	return f.script(ctx, f.tools)
}

// withReviewRunner replaces the model exchange and returns what was built.
func withReviewRunner(t *testing.T, script func(ctx context.Context, tools assistantToolExecutor) (assistantReply, error)) *fakeReviewRunner {
	t.Helper()
	fake := &fakeReviewRunner{script: script}
	prev := watchReviewRunnerFor
	watchReviewRunnerFor = func(_, _ string, tools assistantToolExecutor, _ assistantAutoRouterOptions) assistantRunnerFace {
		fake.tools = tools
		return fake
	}
	t.Cleanup(func() { watchReviewRunnerFor = prev })
	return fake
}

func reviewConfig(t *testing.T) watchConfig {
	t.Helper()
	// The review reads the boat's state for its prompt, which feeds the
	// process-wide GNSS validator; reset it so no "no position" reading leaks
	// into later tests.
	resetGNSSForTest(t)
	path := writeAssistantSettingsFixture(t, true, "test/model")
	return watchConfig{SettingsPath: path, Readiness: assistantReadiness{Enabled: true, Configured: true, Model: "test/model"}, APIKey: "k"}
}

func reviewInputs() (savedWatch, watchReport, []watchFinding) {
	w := savedWatch{ID: "w1", Name: "Overnight check", LookOutFor: "the fridge and the anchor", Sections: allSections(), Thresholds: defaultWatchThresholds()}
	report, findings := buildWatchReport(w, baseInput())
	return w, report, append(findings, watchFinding{Section: "tanks", Rule: "tank_change", Text: "Diesel is down 14 points since 03:00.", By: watchByRules})
}

func submitArgs(review string, added ...string) json.RawMessage {
	items := []map[string]string{}
	for _, text := range added {
		items = append(items, map[string]string{"section": "tanks", "text": text})
	}
	raw, _ := json.Marshal(map[string]any{"review": review, "added_findings": items})
	return raw
}

func TestWatchReview_MateNotReadyIsAProblemAndNeverReachesTheModel(t *testing.T) {
	built := false
	prev := watchReviewRunnerFor
	watchReviewRunnerFor = func(_, _ string, _ assistantToolExecutor, _ assistantAutoRouterOptions) assistantRunnerFace {
		built = true
		return nil
	}
	t.Cleanup(func() { watchReviewRunnerFor = prev })
	w, report, findings := reviewInputs()
	out := runWatchReview(context.Background(), watchConfig{NotReady: "No model is chosen in Settings, Mate."}, w, report, findings, watchTestTime(29, 6, 0), time.UTC)
	if built || out.Reached || out.Review != "" || out.Problem != "Mate is not ready (No model is chosen in Settings, Mate)" || !out.CostKnown || out.CostUSD != 0 {
		t.Fatalf("built %v outcome %+v", built, out)
	}
}

func TestWatchReview_ParsesTheSubmittedReviewAndAddedFindings(t *testing.T) {
	fake := withReviewRunner(t, func(ctx context.Context, tools assistantToolExecutor) (assistantReply, error) {
		if _, err := tools.execute(ctx, watchReviewToolName, submitArgs("Diesel fell faster than the engine hours explain. Were the generator and heater on overnight?", "Diesel fell 14 points in three hours.")); err != nil {
			return assistantReply{}, err
		}
		assistantCostMeterFrom(ctx).add(0.031)
		return assistantReply{Content: "done", Model: "test/model-2"}, nil
	})
	w, report, findings := reviewInputs()
	out := runWatchReview(context.Background(), reviewConfig(t), w, report, findings, watchTestTime(29, 6, 0), time.FixedZone("AEST", 10*3600))
	if out.Problem != "" || !out.Reached || !strings.HasPrefix(out.Review, "Diesel fell faster") || out.Model != "test/model-2" {
		t.Fatalf("outcome %+v", out)
	}
	if len(out.Added) != 1 || out.Added[0].By != watchByMate || out.Added[0].Rule != "mate_review" || out.Added[0].Section != "tanks" {
		t.Fatalf("added %+v", out.Added)
	}
	if out.CostUSD != 0.031 || !out.CostKnown {
		t.Fatalf("cost %v known %v", out.CostUSD, out.CostKnown)
	}
	// The report, the rules' findings and what to look out for are all it is given.
	if len(fake.history) != 1 || fake.history[0].Role != "user" {
		t.Fatalf("history %+v", fake.history)
	}
	msg := string(fake.history[0].Content)
	for _, want := range []string{"the fridge and the anchor", "Overnight check", "Diesel is down 14 points since 03:00.", `"sections"`} {
		if !strings.Contains(msg, want) {
			t.Errorf("the message lacks %q:\n%s", want, msg)
		}
	}
}

func TestWatchReview_OffersOnlyReadToolsAndTheSubmitTool(t *testing.T) {
	var offered []string
	withReviewRunner(t, func(ctx context.Context, tools assistantToolExecutor) (assistantReply, error) {
		for _, def := range tools.definitions() {
			offered = append(offered, def.Function.Name)
		}
		for _, name := range []string{"propose_changes", "start_watch", "fill_form", "inspect_form", "run_watch_now", "list_watches", "show_telemetry_chart"} {
			if _, err := tools.execute(ctx, name, json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "not available in a watch review") {
				t.Errorf("%s must be refused in a review, got %v", name, err)
			}
		}
		if _, err := tools.execute(ctx, "search_web", json.RawMessage(`{"query":"tides"}`)); err == nil {
			t.Error("web search is refused while the operator has it off")
		}
		return assistantReply{}, errors.New("stop here")
	})
	w, report, findings := reviewInputs()
	runWatchReview(context.Background(), reviewConfig(t), w, report, findings, watchTestTime(29, 6, 0), time.UTC)

	has := func(name string) bool {
		for _, n := range offered {
			if n == name {
				return true
			}
		}
		return false
	}
	for _, name := range []string{"get_vessel_status", "get_active_alarms", "get_alarm_context", "get_path_history", "list_passages", watchReviewToolName} {
		if !has(name) {
			t.Errorf("%s should be offered, got %v", name, offered)
		}
	}
	for _, name := range []string{"propose_changes", "start_watch", "fill_form", "inspect_form", "run_watch_now", "list_watches", "search_web", "show_telemetry_chart", "show_wind_rose"} {
		if has(name) {
			t.Errorf("%s must not be offered, got %v", name, offered)
		}
	}
}

func TestWatchReview_AnAnswerWithoutTheSubmitCallIsAProblem(t *testing.T) {
	withReviewRunner(t, func(ctx context.Context, tools assistantToolExecutor) (assistantReply, error) {
		assistantCostMeterFrom(ctx).add(0.01)
		return assistantReply{Content: "All fine.", Model: "m"}, nil
	})
	w, report, findings := reviewInputs()
	out := runWatchReview(context.Background(), reviewConfig(t), w, report, findings, watchTestTime(29, 6, 0), time.UTC)
	if out.Review != "" || out.Problem != "Mate finished without submitting a review" || !out.Reached || out.CostUSD != 0.01 {
		t.Fatalf("prose is not parsed for a review: %+v", out)
	}
}

func TestWatchReview_AFailedRunKeepsWhatItCostAndPricesCutOffGenerations(t *testing.T) {
	prevLookup, prevDelays := generationCostLookup, generationCostLookupDelays
	generationCostLookupDelays = []time.Duration{time.Millisecond}
	t.Cleanup(func() { generationCostLookup, generationCostLookupDelays = prevLookup, prevDelays })
	generationCostLookup = func(_ context.Context, key, id string) (float64, error) {
		if key != "k" || id != "gen-9" {
			t.Errorf("lookup %s %s", key, id)
		}
		return 0.04, nil
	}
	withReviewRunner(t, func(ctx context.Context, _ assistantToolExecutor) (assistantReply, error) {
		m := assistantCostMeterFrom(ctx)
		m.add(0.01)
		m.addPending("gen-9")
		return assistantReply{}, errors.New("openrouter stream ended early")
	})
	w, report, findings := reviewInputs()
	out := runWatchReview(context.Background(), reviewConfig(t), w, report, findings, watchTestTime(29, 6, 0), time.UTC)
	if !strings.HasPrefix(out.Problem, "Mate's review failed: ") || !out.Reached || out.Review != "" {
		t.Fatalf("%+v", out)
	}
	if out.CostUSD < 0.0499 || out.CostUSD > 0.0501 || !out.CostKnown {
		t.Fatalf("cost %v known %v", out.CostUSD, out.CostKnown)
	}

	withReviewRunner(t, func(ctx context.Context, _ assistantToolExecutor) (assistantReply, error) {
		assistantCostMeterFrom(ctx).markUnknown()
		return assistantReply{}, errors.New("timeout")
	})
	out = runWatchReview(context.Background(), reviewConfig(t), w, report, findings, watchTestTime(29, 6, 0), time.UTC)
	if out.CostKnown || out.Problem == "" {
		t.Fatalf("an unreadable cost is shown as not known: %+v", out)
	}
}

func TestWatchReview_SubmitChecksWhatItIsGiven(t *testing.T) {
	tools := &watchReviewTools{}
	long := strings.Repeat("x", maxWatchReviewChars+1)
	for name, args := range map[string]json.RawMessage{
		"not json":              json.RawMessage(`nope`),
		"empty review":          submitArgs("  "),
		"review too long":       submitArgs(long),
		"six findings":          submitArgs("ok", "a", "b", "c", "d", "e", "f"),
		"question mark":         submitArgs("ok", "Is the bilge pump stuck?"),
		"exclamation":           submitArgs("ok", "Diesel is falling fast!"),
		"empty finding":         submitArgs("ok", " "),
		"unknown section":       json.RawMessage(`{"review":"ok","added_findings":[{"section":"engine","text":"x"}]}`),
		"finding text too long": submitArgs("ok", long[:maxWatchReviewFindingText+1]),
	} {
		if _, err := tools.submit(args); err == nil {
			t.Errorf("%s should be refused", name)
		}
		if tools.submission() != nil {
			t.Fatalf("%s must record nothing", name)
		}
	}
	if _, err := tools.submit(submitArgs("Quiet night. Nothing to ask.")); err != nil || tools.submission() == nil {
		t.Fatalf("a plain review is taken: %v", err)
	}
}

func TestWatchReviewPrompt(t *testing.T) {
	now := time.Date(2026, 10, 28, 20, 0, 0, 0, time.UTC)
	loc := time.FixedZone("AEST", 10*3600)
	pc := assistantPromptContext{VesselName: "Sea Biscuit", BoatModel: "Leopard 40", HullType: "power_cat", LOAM: 12.2, PlaceName: "Coral Cove", Notes: "Never anchor north of the reef."}
	prompt := buildWatchReviewPrompt(pc, "imperial", now, loc, false)
	for _, want := range []string{
		"Sea Biscuit", "Leopard 40", "power catamaran", "12.2 m", "Coral Cove", "Never anchor north of the reef.",
		"Boat time: Thu 29 Oct 2026 06:00 AEST", "units are imperial",
		"never remove", "can never remove", "Where you are unsure, say so and ask", "Never guess a cause", "do not raise it again",
		"not an all clear", "02:00", "submit_watch_review", "The tools only read",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt lacks %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "Web search") {
		t.Error("web search is mentioned only when the operator turned it on")
	}
	if !strings.Contains(buildWatchReviewPrompt(pc, "metric", now, loc, true), "Web search is available") {
		t.Error("web search is mentioned when on")
	}
	if strings.Contains(strings.ToLower(prompt), "routine") {
		t.Error("the word routine appears nowhere")
	}
	if len(prompt) > 4000 {
		t.Errorf("a trimmed prompt, not the whole one: %d characters", len(prompt))
	}
}

func TestWatchReview_ACrashInTheReviewIsAProblemNotACrashOfTheScheduler(t *testing.T) {
	withReviewRunner(t, func(context.Context, assistantToolExecutor) (assistantReply, error) {
		panic("boom")
	})
	w, report, findings := reviewInputs()
	out := runWatchReview(context.Background(), reviewConfig(t), w, report, findings, watchTestTime(29, 6, 0), time.UTC)
	if !strings.Contains(out.Problem, "the review crashed: boom") || out.CostKnown {
		t.Fatalf("%+v", out)
	}
}
