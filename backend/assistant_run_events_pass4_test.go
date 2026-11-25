package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// Stop, the trace of an abandoned call, the held tail of a failed round, the
// order of content and a tool call in one chunk, the 400-byte hold and the
// clock before the runner starts (ADR 0305).

// startedTools runs a call that says when it has begun and then waits: for the
// context to end (honourCtx) or to be released.
type startedTools struct {
	*fakeToolExecutor
	started   chan struct{}
	release   chan struct{}
	honourCtx bool
}

func newStartedTools(honourCtx bool) *startedTools {
	return &startedTools{fakeToolExecutor: &fakeToolExecutor{}, started: make(chan struct{}), release: make(chan struct{}), honourCtx: honourCtx}
}

func (s *startedTools) execute(ctx context.Context, name string, args json.RawMessage) (string, assistantToolOutcome, error) {
	close(s.started)
	if s.honourCtx {
		<-ctx.Done()
		return "", assistantToolOutcome{}, ctx.Err()
	}
	<-s.release
	return "{}", assistantToolOutcome{}, nil
}

// An operator's Stop is not the tool phase's time limit: the running lookup is
// closed with a plain "stopped", not marked cut, so the view never shows it
// running forever and never blames the time limit.
func TestEvents_StopClosesARunningLookupAsStoppedNotCut(t *testing.T) {
	for _, honour := range []bool{true, false} {
		name := "a tool that ignores the context"
		if honour {
			name = "a tool that honours the context"
		}
		t.Run(name, func(t *testing.T) {
			prev := assistantToolAbandonGrace
			assistantToolAbandonGrace = 20 * time.Millisecond
			t.Cleanup(func() { assistantToolAbandonGrace = prev })
			tools := newStartedTools(honour)
			t.Cleanup(func() { close(tools.release) })
			ctx, cancel := context.WithCancel(context.Background())
			go func() {
				<-tools.started
				cancel()
			}()
			tape, emit := newEventTape()
			runner := &assistantRunner{doer: queued(newCallRound(t, 0), streamContent("Best answer.", 4, false)), apiKey: "k", model: "m", tools: tools, emit: emit}
			if _, err := runner.run(ctx, "system", "", nil); err == nil {
				t.Fatal("a stopped run must end with an error")
			}
			dones := tape.named("lookup_done")
			if len(dones) != 1 {
				t.Fatalf("events = %v: want exactly one lookup_done", tape.names())
			}
			d := dones[0].(mateLookupDoneEvent)
			if d.Outcome != "stopped" || d.Reason != "" {
				t.Fatalf("lookup_done = %+v, want stopped with no reason", d)
			}
		})
	}
}

// A call abandoned at the end of the tool phase is cut on the stream and is in
// the trace as a call that ran: the stream's lookups, the trace's calls and the
// run's lookup count agree.
func TestEvents_AnAbandonedCallIsInTheTraceAndCounted(t *testing.T) {
	shortBudget(t, 80*time.Millisecond, 5*time.Second)
	prev := assistantToolAbandonGrace
	assistantToolAbandonGrace = 20 * time.Millisecond
	t.Cleanup(func() { assistantToolAbandonGrace = prev })
	tools := &stuckTools{fakeToolExecutor: &fakeToolExecutor{}, release: make(chan struct{})}
	t.Cleanup(func() { close(tools.release) })

	tape, emit := newEventTape()
	rec := &assistantRunRecorder{}
	runner := &assistantRunner{doer: queued(newCallRound(t, 0), streamContent("Best answer.", 4, false)), apiKey: "k", model: "m", tools: tools, emit: emit}
	if _, err := runner.run(withAssistantRunRecorder(context.Background(), rec), "system", "", nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	rounds, _ := rec.result()
	streamed := len(tape.named("lookup"))
	var calls []assistantToolCall
	for _, r := range rounds {
		calls = append(calls, r.Calls...)
	}
	if streamed != 1 || len(calls) != streamed || assistantRunLookupCount(rounds) != streamed {
		t.Fatalf("stream lookups %d, trace calls %d, lookup count %d: want all 1", streamed, len(calls), assistantRunLookupCount(rounds))
	}
	if calls[0].Name == "" || calls[0].Error != assistantToolCutError {
		t.Fatalf("call = %+v, want it named and marked cut", calls[0])
	}
}

// A round that failed or was cut off mid-stream sends only what the hold-back
// has cleared, so the start of a text tool call never reaches the chatter.
func TestEvents_AFailedOrCutRoundDoesNotSendAPartialMarker(t *testing.T) {
	for _, kind := range []assistantRoundOutcomeKind{roundCutOff, roundFailed} {
		tape, c := captureTape()
		h := c.hooks()
		h.Content("Let me check the tide for you now, then <tool_ca")
		tr := &assistantTraceRound{}
		c.resolve(assistantRoundOutcome{kind: kind}, tr)
		var chatter string
		for _, ev := range *tape {
			if e, ok := ev.(mateChatterEvent); ok {
				chatter += e.Text
			}
		}
		if strings.Contains(chatter, "<") || strings.Contains(tr.Chatter, "<") || chatter != tr.Chatter {
			t.Fatalf("outcome %d: chatter %q, stored %q: neither may hold the partial marker, and they must match", kind, chatter, tr.Chatter)
		}
	}
}

// A chunk that carries both text and the first tool-call delta settles the text
// as chatter: no promotion, no withdrawal.
func TestEvents_ContentAndAToolCallInOneChunkIsChatter(t *testing.T) {
	text := strings.Repeat("Let me check the tide for the whole afternoon first. ", 2) + "\n\nOne moment please while I look it up"
	b, _ := json.Marshal(text)
	delta := `{"content":` + string(b) + `,"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_tides","arguments":"{}"}}]}`
	doer := &fakeOpenRouterDoer{responses: []*http.Response{activityResponse(activityChunk(delta), activityFinish("tool_calls"))}}
	tape, c := captureTape()
	if _, err := openRouterChatCompletion(context.Background(), doer, "sk", openRouterChatRequest{Model: "m"}, c.hooks()); err != nil {
		t.Fatal(err)
	}
	c.resolve(assistantRoundOutcome{kind: roundAskedForTools}, &assistantTraceRound{})
	var names []string
	var chatter string
	for _, ev := range *tape {
		names = append(names, ev.mateEventName())
		if e, ok := ev.(mateChatterEvent); ok {
			chatter += e.Text
		}
	}
	if indexOfName(names, "answer_start") >= 0 || indexOfName(names, "answer_withdrawn") >= 0 || chatter != text {
		t.Fatalf("events %v, chatter %q: want chatter only, with the whole text", names, chatter)
	}
}

// The 400-byte hold is measured on the opening without its leading blanks, as
// the paragraph check is.
func TestEvents_LeadingBlanksDoNotCountTowardTheHold(t *testing.T) {
	tape, c := captureTape()
	h := c.hooks()
	h.Content(strings.Repeat("\n", 380) + strings.Repeat("a b c d e f g h i j k l m n o p q r s t u v w x y z ", 2))
	h.ToolCall(0)
	c.resolve(assistantRoundOutcome{kind: roundAskedForTools}, &assistantTraceRound{})
	for _, ev := range *tape {
		if n := ev.mateEventName(); n == "answer_start" || n == "answer_withdrawn" {
			t.Fatalf("got %s: a short opening behind blank lines is chatter", n)
		}
	}
}

// syncFlushBuffer is a stream's writer that a test can read while it is written.
type syncFlushBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncFlushBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}
func (*syncFlushBuffer) Flush() {}
func (b *syncFlushBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// A subscriber that attaches before the runner has started gets its clock when
// the start is set, ahead of the first event, so the browser's zero is the
// server's.
func TestEvents_AClockForASubscriberWhoAttachedEarlyComesWithTheStart(t *testing.T) {
	prev := assistantRunClock
	t.Cleanup(func() { assistantRunClock = prev })
	registered := time.Unix(7000, 0)
	assistantRunClock = func() time.Time { return registered }
	run := newAssistantRun(func() {})
	out := &syncFlushBuffer{}
	finished := make(chan struct{})
	go func() {
		streamAssistantRun(context.Background(), run, 0, out)
		close(finished)
	}()

	time.Sleep(50 * time.Millisecond)
	if got := out.String(); strings.Contains(got, "event: clock") {
		t.Fatalf("clock sent before the runner started: %q", got)
	}

	runnerStart := registered.Add(5 * time.Second)
	assistantRunClock = func() time.Time { return runnerStart }
	run.setStarted(runnerStart)
	run.append(mateRoundEvent{Round: 0, AtMs: 0})
	run.finish()
	<-finished
	got := out.String()
	clock, round := strings.Index(got, "event: clock\ndata: {\"at_ms\":0}\n\n"), strings.Index(got, "event: round")
	if clock != 0 || round < clock {
		t.Fatalf("stream = %q, want the clock at the runner's zero (at_ms 0) first, then the round", got)
	}
}
