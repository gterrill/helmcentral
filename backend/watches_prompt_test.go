package main

import (
	"strings"
	"testing"
)

// ADR 0227: a turn seeded from a watch run names it, so Mate reads the run with
// get_watch_runs before anything else.
func TestAssistantScreenSentence_NamesTheWatchRunBeingAskedAbout(t *testing.T) {
	got := assistantScreenSentence(assistantScreenContext{Panel: "watches", WatchRunID: "run-7", WatchName: "Morning report"})
	for _, want := range []string{`"Morning report"`, "run_id run-7", "get_watch_runs"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in %q", want, got)
		}
	}
	if got := assistantScreenSentence(assistantScreenContext{Panel: "watches"}); strings.Contains(got, "get_watch_runs") {
		t.Fatalf("a watches page with no run names no run: %q", got)
	}
}

func TestWatchPromptRule(t *testing.T) {
	stable, live := assistantSystemPromptParts(basePromptContext())
	for _, want := range []string{
		"list_watches", "type watch", "five-field cron", "Watches page", "Mate reviews every report",
		"notify", "only when it finds something", "never SignalK", "does not replace an alarm", "taps Apply", "get_watch_runs", "run_watch_now",
	} {
		if !strings.Contains(stable, want) {
			t.Errorf("expected the stable prefix to contain %q", want)
		}
	}
	if strings.Contains(stable, "nothing is sent to their phone") {
		t.Error("a watch can now notify; the rule must not say it cannot")
	}
	if strings.Contains(live, "list_watches") {
		t.Error("the watch rule belongs in the stable prefix only")
	}
	for _, word := range []string{"routine", "Routine"} {
		if strings.Contains(stable, word) || strings.Contains(live, word) {
			t.Errorf("%q appears nowhere an operator or the model reads", word)
		}
	}
}
