package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// updateMateFixtures rewrites the golden fixtures from what Go emits:
// testdata/mate_events/*.sse (go test -run TestMateEventFixtures -update, ADR
// 0189) and testdata/mate_cards/*.json (go test -run MateCardGoldens -update,
// ADR 0261). Go owns the bytes; the frontend tests decode the same files.
var updateMateFixtures = flag.Bool("update", false, "rewrite the Mate event (testdata/mate_events) and card (testdata/mate_cards) golden fixtures")

type mateFlushBuffer struct{ bytes.Buffer }

func (*mateFlushBuffer) Flush() {}

// mateFixtureMessage is a real stored reply (a form draft attached) trimmed
// for size, read from message_source.json.
func mateFixtureMessage(t *testing.T) mateMessageEvent {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "mate_events", "message_source.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ev mateMessageEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatal(err)
	}
	if len(ev.Message.FormDrafts) == 0 {
		t.Fatal("message_source.json must keep a form draft so the optional fields stay covered")
	}
	return ev
}

// fixtureClockMs is the clock every golden stream is written under.
const fixtureClockMs = 4200

// assertMateFixture streams run as one subscriber does and compares the bytes
// with testdata/mate_events/<name>.sse, rewriting it under -update. The clock
// frame every subscription opens with is part of the stream; it has a fixture
// of its own (clock.sse), so the others hold the run's events alone, as the
// decoder test reads them.
func assertMateFixture(t *testing.T, name string, run *assistantRun, withClock bool) {
	t.Helper()
	prev := assistantRunClock
	t.Cleanup(func() { assistantRunClock = prev })
	start := time.Unix(1_000, 0)
	assistantRunClock = func() time.Time { return start.Add(fixtureClockMs * time.Millisecond) }
	run.started = start

	var out mateFlushBuffer
	streamAssistantRun(context.Background(), run, 0, &out)
	got := out.Bytes()
	clockFrame := []byte(fmt.Sprintf("event: clock\ndata: {\"at_ms\":%d}\n\n", fixtureClockMs))
	if !bytes.HasPrefix(got, clockFrame) {
		t.Fatalf("a stream must open with the clock, got %q", got)
	}
	if !withClock {
		got = bytes.TrimPrefix(got, clockFrame)
	}

	path := filepath.Join("testdata", "mate_events", name+".sse")
	if *updateMateFixtures {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("wire bytes changed for %s\n got: %q\nwant: %q", name, got, want)
	}
}

func TestMateEventFixtures(t *testing.T) {
	thought := int64(5300)
	cases := []struct {
		name string
		ev   mateEvent
	}{
		{"round", mateRoundEvent{Round: 1, AtMs: 1840, Final: false}},
		{"reasoning", mateReasoningEvent{Round: 1, Text: "Neaps at Mackay, so the bar is shallow at six."}},
		{"chatter", mateChatterEvent{Round: 1, Text: "Let me check the tide."}},
		{"lookup", mateLookupEvent{Round: 1, Seq: 0, AtMs: 2100, Text: "Checking the tide at Mackay", Subject: "the tide at Mackay"}},
		{"lookup_skipped", mateLookupEvent{Round: 1, Seq: 21, AtMs: 2101, Text: "Checking the tide at Hay Point", Subject: "the tide at Hay Point", Skipped: mateSkippedRoundFull}},
		{"lookup_done", mateLookupDoneEvent{Round: 1, Seq: 0, AtMs: 3050, Outcome: assistantOutcomeDone}},
		{"lookup_done_failed", mateLookupDoneEvent{Round: 1, Seq: 1, AtMs: 3060, Outcome: assistantOutcomeFailed, Reason: "tide station offline"}},
		{"lookup_done_cut", mateLookupDoneEvent{Round: 1, Seq: 2, AtMs: 3070, Outcome: assistantOutcomeCut}},
		{"answer_start", mateAnswerStartEvent{Round: 2, AtMs: 5300}},
		{"delta", mateDeltaEvent{Text: "Tongue Bay first, "}},
		{"answer_withdrawn", mateAnswerWithdrawnEvent{Round: 1, AtMs: 2090, Reason: mateWithdrawnLookups}},
		{"answer_withdrawn_failed", mateAnswerWithdrawnEvent{Round: 1, AtMs: 2091, Reason: mateWithdrawnFailed}},
		{"draft_sent_back", mateDraftSentBackEvent{Round: 2, AtMs: 6400, Triggers: []string{"shelter at the anchorage", "depth at the anchorage"}, Draft: "Tongue Bay is well sheltered and has 3 m at low water."}},
		{"round_end", mateRoundEndEvent{Round: 1, AtMs: 3070, Reasoning: "text", ReasoningBytes: 2150, PromptTokens: 1200, CompletionTokens: 340, CachedTokens: 900, CostUSD: 0.0042}},
		{"message", mateFixtureMessage(t)},
		{"message_run", func() mateEvent {
			ev := mateFixtureMessage(t)
			ev.Run = &mateRunRef{ID: "5d0f6c1a-77a2-4a3e-bb1f-0c2f4f6d9e11", ThoughtMs: &thought, Lookups: 4}
			return ev
		}()},
		{"message_run_no_thought", func() mateEvent {
			ev := mateFixtureMessage(t)
			ev.Run = &mateRunRef{ID: "5d0f6c1a-77a2-4a3e-bb1f-0c2f4f6d9e11", Lookups: 0}
			return ev
		}()},
		{"error", mateErrorEvent{Error: "stopped"}},
		{"error_run", mateErrorEvent{Error: "stopped", RunID: "5d0f6c1a-77a2-4a3e-bb1f-0c2f4f6d9e11"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, cancel := context.WithCancel(context.Background())
			run := newAssistantRun(cancel)
			run.append(tc.ev)
			run.finish()
			assertMateFixture(t, tc.name, run, false)
		})
	}

	// The clock is written by the stream itself and is never in the log.
	t.Run("clock", func(t *testing.T) {
		_, cancel := context.WithCancel(context.Background())
		run := newAssistantRun(cancel)
		run.finish()
		assertMateFixture(t, "clock", run, true)
	})
}

// Empty lists and absent run references keep a stable shape: a draft with no
// triggers is [] and not null, and a message with no stored run has no "run".
func TestMateEventShapes(t *testing.T) {
	c := newAssistantRoundCapture(nil, time.Now(), 0, false)
	var sent mateEvent
	c.emit = func(ev mateEvent) { sent = ev }
	c.hooks().Content("draft")
	c.resolve(assistantRoundOutcome{kind: roundSentBack}, &assistantTraceRound{})
	got, err := json.Marshal(sent)
	if err != nil || !strings.Contains(string(got), `"triggers":[]`) {
		t.Fatalf("draft_sent_back marshals %s (%v)", got, err)
	}
	got, _ = json.Marshal(mateFixtureMessage(t))
	if strings.Contains(string(got), `"run"`) {
		t.Fatalf("a message with no stored run carries a run: %s", got)
	}
	got, _ = json.Marshal(mateErrorEvent{Error: "x"})
	if string(got) != `{"error":"x"}` {
		t.Fatalf("an error with no stored run marshals %s", got)
	}
}
