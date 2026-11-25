package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// The saved call and the live lookup_done say the same thing about a call,
// because both come from assistantCallOutcome and assistantCallReason.
func TestAssistantCallOutcome_SavedMatchesLive(t *testing.T) {
	cases := []struct {
		name       string
		err        string
		wantOut    string
		wantReason string
	}{
		{"done", "", "done", ""},
		{"failed", "get_tides: station offline", "failed", "station offline"},
		{"failed without prefix", "station offline", "failed", "station offline"},
		{"cut", assistantToolCutError, "cut", ""},
		{"stopped", assistantLookupStoppedText, "stopped", ""},
		{"withheld", assistantToolWithheldError, "withheld", ""},
		{"round full", assistantRoundFullError, "round_full", ""},
		// Calls saved before the outcome was recorded hold the tool's own context error.
		{"old cut by the time limit", "get_tides: context deadline exceeded", "cut", ""},
		{"old stopped by the operator", "get_tides: context canceled", "stopped", ""},
	}

	store := newTestAssistantStore(t)
	conv, _ := store.CreateConversation("Passage")
	trace := sampleRunTrace(conv.ID, 1, nil, assistantStopTimeCeiling)
	trace.Rounds[0].Calls = nil
	for i, tc := range cases {
		trace.Rounds[0].Calls = append(trace.Rounds[0].Calls, assistantToolCall{Seq: i, Name: "get_tides", Args: `{"lat":1,"lon":2}`, Error: tc.err})
	}
	saved, err := store.SaveRun(trace)
	if err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	got, ok, err := store.GetRun(saved.ID)
	if err != nil || !ok {
		t.Fatalf("GetRun: ok=%v err=%v", ok, err)
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			call := got.Rounds[0].Calls[i]
			if call.Outcome != tc.wantOut || call.Reason != tc.wantReason {
				t.Fatalf("saved call = outcome %q reason %q, want %q %q", call.Outcome, call.Reason, tc.wantOut, tc.wantReason)
			}
			// A call that was never run gets no lookup_done; see the test below.
			if tc.wantOut == "withheld" || tc.wantOut == "round_full" {
				return
			}
			tape, c := captureTape()
			c.lookupDone(0, "get_tides", tc.err)
			d := (*tape)[0].(mateLookupDoneEvent)
			if d.Outcome != call.Outcome || d.Reason != call.Reason {
				t.Fatalf("live lookup_done = %+v, saved call = %q %q: they must agree", d, call.Outcome, call.Reason)
			}
		})
	}
}

// lookup_done carries only done, failed, cut or stopped. A call that was never
// run (withheld, round full) must not put its own outcome on the stream.
func TestLookupDone_NeverCarriesANotRunOutcome(t *testing.T) {
	for _, errText := range []string{assistantToolWithheldError, assistantRoundFullError} {
		tape, c := captureTape()
		c.lookupDone(0, "get_tides", errText)
		d := (*tape)[0].(mateLookupDoneEvent)
		if d.Outcome != "failed" {
			t.Errorf("lookup_done for %q carries outcome %q, want failed", errText, d.Outcome)
		}
	}
}

// A tool that honours the context returns the context's error. The saved call
// must still read back as cut or stopped, as the live lookup_done said, and not
// as failed with "context deadline exceeded".
func TestAssistantCallOutcome_CtxHonouringToolSavesAsLiveSaid(t *testing.T) {
	for _, tc := range []string{"cut", "stopped"} {
		t.Run(tc, func(t *testing.T) {
			tools := newStartedTools(true)
			t.Cleanup(func() { close(tools.release) })
			var ctx context.Context
			var cancel context.CancelFunc
			if tc == "cut" {
				shortBudget(t, 80*time.Millisecond, 5*time.Second)
				ctx, cancel = context.WithCancel(context.Background())
			} else {
				ctx, cancel = context.WithCancel(context.Background())
				go func() {
					<-tools.started
					cancel()
				}()
			}
			defer cancel()
			tape, emit := newEventTape()
			rec := &assistantRunRecorder{}
			runner := &assistantRunner{doer: queued(newCallRound(t, 0), streamContent("Best answer.", 4, false)), apiKey: "k", model: "m", tools: tools, emit: emit}
			_, _ = runner.run(withAssistantRunRecorder(ctx, rec), "system", "", nil)
			dones := tape.named("lookup_done")
			if len(dones) != 1 {
				t.Fatalf("events = %v: want one lookup_done", tape.names())
			}
			live := dones[0].(mateLookupDoneEvent)
			rounds, _ := rec.result()
			var calls []assistantToolCall
			for _, r := range rounds {
				calls = append(calls, r.Calls...)
			}
			if len(calls) != 1 {
				t.Fatalf("calls = %d, want 1", len(calls))
			}
			if got := assistantCallOutcome(calls[0].Error); got != live.Outcome || got != tc {
				t.Fatalf("saved error %q reads back as %q, live said %q, want %q", calls[0].Error, got, live.Outcome, tc)
			}
		})
	}
}

// waitsForCtxTools runs a call that waits for the context to end and returns its error.
type waitsForCtxTools struct{ *fakeToolExecutor }

func (w waitsForCtxTools) execute(ctx context.Context, name string, args json.RawMessage) (string, assistantToolOutcome, error) {
	<-ctx.Done()
	return "", assistantToolOutcome{}, ctx.Err()
}

// A call that was cut off by the time limit, or stopped by the operator, did not
// fail: it does not count against the tool's failure budget.
func TestRunToolRound_CutAndStoppedCallsAreNotCountedAsFailures(t *testing.T) {
	for _, tc := range []string{"cut", "stopped"} {
		t.Run(tc, func(t *testing.T) {
			runner := &assistantRunner{tools: waitsForCtxTools{&fakeToolExecutor{}}}
			failures := &assistantToolFailures{counts: make(map[string]int)}
			for i := 0; i < assistantMaxToolFailures; i++ {
				var ctx context.Context
				var cancel context.CancelFunc
				if tc == "cut" {
					ctx, cancel = context.WithTimeout(context.Background(), 30*time.Millisecond)
				} else {
					ctx, cancel = context.WithCancel(context.Background())
					time.AfterFunc(30*time.Millisecond, cancel)
				}
				_, capture := captureTape()
				_, calls, _ := runner.runToolRound(ctx, []openRouterToolCall{
					{ID: "c", Type: "function", Function: openRouterToolCallFunction{Name: "get_tides", Arguments: openRouterArguments(`{"lat":1,"lon":2}`)}},
				}, failures, capture)
				cancel()
				if len(calls) != 1 || assistantCallOutcome(calls[0].Error) != tc {
					t.Fatalf("round %d calls = %+v, want one %s call", i, calls, tc)
				}
			}
			if failures.exhausted("get_tides") {
				t.Fatalf("a tool %s %d times was withheld as if it had failed", tc, assistantMaxToolFailures)
			}
		})
	}
}

// The subject of a search puts the query in typographic quotes and does not
// escape what is inside them: an operator reads it, not a parser.
func TestQuotedSubject_QuotesWithoutEscaping(t *testing.T) {
	subject := quotedSubject("documents", func(args json.RawMessage) string { return string(args) })
	cases := map[string]string{
		`impeller`:        "documents for “impeller”",
		`the "blue" pump`: "documents for “the \"blue\" pump”",
		`  `:              "documents",
	}
	for q, want := range cases {
		if got := subject(json.RawMessage(q)); got != want {
			t.Errorf("subject(%q) = %q, want %q", q, got, want)
		}
	}
}

// Every registered tool names what it looked at, for the burst line.
func TestToolSubjects_EveryRegisteredToolHasOne(t *testing.T) {
	argSets := []string{
		`{}`,
		`{"lat":1.5,"lon":2.5,"name":"Mackay","query":"tide","to_lat":1,"to_lon":2,"page":"tides","document_id":"d1","filter":"nav","path":"navigation.speed","tile":"depth"}`,
	}
	for _, tool := range mateTools {
		for _, args := range argSets {
			if got := describeAssistantToolSubject(tool.name, json.RawMessage(args)); got == "" {
				t.Errorf("%s with %s has no subject", tool.name, args)
			}
		}
	}
}
