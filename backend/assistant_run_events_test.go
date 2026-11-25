package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// What a run puts on Mate's stream: rounds, lanes (chatter or answer), lookups,
// and the events that take text back (ADR 0305). The wire bytes of each shape
// are pinned in assistant_mate_events_test.go.

// eventTape records every event a runner emits, typed.
type eventTape struct {
	mu     sync.Mutex
	events []mateEvent
}

func newEventTape() (*eventTape, assistantEmitter) {
	tape := &eventTape{}
	return tape, func(ev mateEvent) {
		tape.mu.Lock()
		defer tape.mu.Unlock()
		tape.events = append(tape.events, ev)
	}
}

func (t *eventTape) all() []mateEvent {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]mateEvent(nil), t.events...)
}

func (t *eventTape) names() []string {
	var out []string
	for _, ev := range t.all() {
		out = append(out, ev.mateEventName())
	}
	return out
}

// eventsNamed returns every event with the given wire name.
func (t *eventTape) named(name string) []mateEvent {
	var out []mateEvent
	for _, ev := range t.all() {
		if ev.mateEventName() == name {
			out = append(out, ev)
		}
	}
	return out
}

// indexOf is the position of the first event with the given name at or after
// from, or -1.
func (t *eventTape) indexOf(name string, from int) int {
	for i, ev := range t.all() {
		if i >= from && ev.mateEventName() == name {
			return i
		}
	}
	return -1
}

// text is what an answer, chatter or reasoning lane carried, joined.
func (t *eventTape) text(name string) string {
	var b strings.Builder
	for _, ev := range t.all() {
		switch e := ev.(type) {
		case mateDeltaEvent:
			if name == "delta" {
				b.WriteString(e.Text)
			}
		case mateChatterEvent:
			if name == "chatter" {
				b.WriteString(e.Text)
			}
		case mateReasoningEvent:
			if name == "reasoning" {
				b.WriteString(e.Text)
			}
		}
	}
	return b.String()
}

func runWithTape(t *testing.T, doer openRouterDoer, tools assistantToolExecutor, opts assistantRunnerOptions) (assistantReply, error, *eventTape) {
	t.Helper()
	tape, emit := newEventTape()
	runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: tools, emit: emit, options: opts}
	reply, err := runner.run(context.Background(), "system", "", nil)
	return reply, err, tape
}

// streamContent is a response that streams the text in pieces of at most size
// bytes, then ends. toolDelta adds a tool call after the text.
func streamContent(text string, size int, toolDelta bool) *http.Response {
	var parts []string
	for len(text) > 0 {
		n := min(size, len(text))
		for n < len(text) && n > 0 && text[n]&0xC0 == 0x80 {
			n++
		}
		parts = append(parts, activityChunk(activityContent(text[:n])))
		text = text[n:]
	}
	finish := "stop"
	if toolDelta {
		parts = append(parts, activityChunk(activityToolDelta))
		finish = "tool_calls"
	}
	parts = append(parts, activityFinish(finish))
	return activityResponse(parts...)
}

// A forced final round streams at once: answer_start comes before its first
// delta, with no chatter and nothing withdrawn.
func TestEvents_AForcedFinalRoundStreamsAtOnceWithNoChatter(t *testing.T) {
	var responses []*http.Response
	for i := 0; i < stalledToolRounds; i++ {
		responses = append(responses, toolCallResponse(t, fmt.Sprintf("call_%d", i), "get_tides", `{"lat":1,"lon":2}`, openRouterUsage{}))
	}
	responses = append(responses, streamContent("Short answer.", 4, false))
	reply, err, tape := runWithTape(t, queued(responses...), &fakeToolExecutor{}, assistantRunnerOptions{})
	if err != nil || reply.Content != "Short answer." {
		t.Fatalf("reply %q err %v", reply.Content, err)
	}
	rounds := tape.named("round")
	last := rounds[len(rounds)-1].(mateRoundEvent)
	if !last.Final || rounds[0].(mateRoundEvent).Final {
		t.Fatalf("rounds = %+v: only the last is the forced final", rounds)
	}
	if len(tape.named("chatter")) != 0 || len(tape.named("answer_withdrawn")) != 0 {
		t.Fatalf("events = %v: a forced final has no chatter and nothing to withdraw", tape.names())
	}
	start, delta := tape.indexOf("answer_start", 0), tape.indexOf("delta", 0)
	if start < 0 || delta < 0 || start > delta {
		t.Fatalf("answer_start at %d, first delta at %d: the answer opens before its text", start, delta)
	}
	if got := tape.text("delta"); got != "Short answer." {
		t.Fatalf("deltas = %q", got)
	}
}

// Text before a tool-call delta is chatter: one event, no delta, nothing taken
// back.
func TestEvents_TextThenAToolCallIsChatterNotAnAnswer(t *testing.T) {
	doer := queued(
		streamContent("Let me check the tide.", 5, true),
		streamContent("High at six.", 5, false),
	)
	reply, err, tape := runWithTape(t, doer, &fakeToolExecutor{}, assistantRunnerOptions{})
	if err != nil || reply.Content != "High at six." {
		t.Fatalf("reply %q err %v", reply.Content, err)
	}
	chatter := tape.named("chatter")
	if len(chatter) != 1 || chatter[0].(mateChatterEvent).Text != "Let me check the tide." || chatter[0].(mateChatterEvent).Round != 0 {
		t.Fatalf("chatter = %+v, want one event with the whole text", chatter)
	}
	if got := tape.text("delta"); got != "High at six." {
		t.Fatalf("deltas = %q: the chatter must not reach the answer", got)
	}
	if len(tape.named("answer_withdrawn")) != 0 || len(tape.named("answer_start")) != 1 {
		t.Fatalf("events = %v", tape.names())
	}
	// The order a round keeps: open, chatter, lookup, done, close.
	want := []string{"round", "chatter", "lookup", "lookup_done", "round_end", "round", "answer_start"}
	got := tape.names()
	for i, name := range want {
		if i >= len(got) || got[i] != name {
			t.Fatalf("events = %v, want it to begin %v", got, want)
		}
	}
}

// A long opening is promoted to the answer at once; when the round then asks for
// tools the promoted text is taken back, and the held tail goes to the chatter.
func TestEvents_PromotedTextThatEndsInToolsIsWithdrawn(t *testing.T) {
	long := strings.Repeat("The wind looks fine for the run across. ", 12)
	doer := queued(streamContent(long, 100, true), streamContent("High at six.", 5, false))
	_, err, tape := runWithTape(t, doer, &fakeToolExecutor{}, assistantRunnerOptions{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	withdrawn := tape.named("answer_withdrawn")
	if len(withdrawn) != 1 || withdrawn[0].(mateAnswerWithdrawnEvent).Reason != "lookups" {
		t.Fatalf("answer_withdrawn = %+v, want one with reason lookups", withdrawn)
	}
	start, w, lookup := tape.indexOf("answer_start", 0), tape.indexOf("answer_withdrawn", 0), tape.indexOf("lookup", 0)
	if !(start >= 0 && start < w && w < lookup) {
		t.Fatalf("events = %v: answer_start, then withdrawn, before the lookup", tape.names())
	}
	// Nothing of the round is lost: what was shown plus the tail is the text.
	round0 := ""
	for _, ev := range tape.all()[:tape.indexOf("round_end", 0)] {
		switch e := ev.(type) {
		case mateDeltaEvent:
			round0 += e.Text
		case mateChatterEvent:
			round0 += e.Text
		}
	}
	if round0 != long {
		t.Fatalf("the round's text over delta and chatter = %q, want %q", round0, long)
	}
	if got := tape.text("chatter"); got == long {
		t.Fatal("the whole text went to chatter; what was already shown as the answer is moved by the browser, not resent")
	}
}

// A paragraph break promotes too, before 400 bytes.
func TestEvents_AParagraphBreakPromotesTheOpening(t *testing.T) {
	text := "First the tide at the bar, which turns at six.\n\nThen the wind, which stays light until the afternoon."
	doer := queued(streamContent(text, 10, false))
	_, err, tape := runWithTape(t, doer, &fakeToolExecutor{}, assistantRunnerOptions{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := tape.text("delta"); got != text {
		t.Fatalf("deltas = %q", got)
	}
	start := tape.indexOf("answer_start", 0)
	firstDelta := tape.indexOf("delta", 0)
	// The break is inside the held-back window until more text follows, so the
	// answer opened before the end of the stream.
	if start < 0 || firstDelta < start || len(tape.named("delta")) < 2 {
		t.Fatalf("events = %v: the opening should have streamed in pieces", tape.names())
	}
}

// A short answer with no break is held, then promoted whole at the round's end.
func TestEvents_AShortAnswerIsPromotedAtTheEndOfTheRound(t *testing.T) {
	doer := queued(streamContent("High at six.", 3, false))
	_, err, tape := runWithTape(t, doer, &fakeToolExecutor{}, assistantRunnerOptions{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := tape.text("delta"); got != "High at six." || len(tape.named("answer_start")) != 1 || len(tape.named("chatter")) != 0 {
		t.Fatalf("events = %v, deltas %q", tape.names(), got)
	}
}

// A tool phase that ends while a round that began text asks for tools: the text
// is taken back (promoted) or becomes chatter (not).
func TestEvents_ACutOffRoundWithdrawsPromotedTextOrFilesItAsChatter(t *testing.T) {
	for _, tc := range []struct {
		name     string
		first    string
		promoted bool
	}{
		{"promoted", strings.Repeat("The wind looks fine for the run across. ", 12), true},
		{"not promoted", "Let me look.", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shortBudget(t, 80*time.Millisecond, 5*time.Second)
			calls := 0
			doer := budgetDoerFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if calls > 1 {
					return streamContent("Best answer.", 4, false), nil
				}
				pr, pw := io.Pipe()
				go func() {
					_, _ = io.WriteString(pw, activityChunk(activityContent(tc.first)))
					select {
					case <-time.After(250 * time.Millisecond):
					case <-req.Context().Done():
						pw.CloseWithError(req.Context().Err())
						return
					}
					_, _ = io.WriteString(pw, activityChunk(activityToolDelta)+activityFinish("tool_calls"))
					pw.Close()
				}()
				return &http.Response{StatusCode: 200, Body: pr, Header: make(http.Header)}, nil
			})
			reply, err, tape := runWithTape(t, doer, &fakeToolExecutor{}, assistantRunnerOptions{})
			if err != nil || reply.Content != "Best answer." {
				t.Fatalf("reply %q err %v", reply.Content, err)
			}
			withdrawn := tape.named("answer_withdrawn")
			if tc.promoted {
				if len(withdrawn) != 1 || withdrawn[0].(mateAnswerWithdrawnEvent).Reason != "cut_off" {
					t.Fatalf("withdrawn = %+v, want one cut_off (events %v)", withdrawn, tape.names())
				}
				return
			}
			if len(withdrawn) != 0 || tape.text("chatter") != tc.first {
				t.Fatalf("events = %v, chatter %q: unpromoted text is chatter, not a withdrawal", tape.names(), tape.text("chatter"))
			}
		})
	}
}

// draftCheckTools fails any draft that contains BAD, once the check is asked.
type draftCheckTools struct {
	*fakeToolExecutor
	triggers []string
}

func (d *draftCheckTools) checkDraftTriggers(draft string, _ mateRunRecord) ([]string, string) {
	if strings.Contains(draft, "BAD") {
		return d.triggers, "look again"
	}
	return nil, ""
}

// A draft sent back arrives whole, held tail included, with its triggers, and the
// next answer opens afresh.
func TestEvents_ASentBackDraftArrivesWholeAndTheNextAnswerOpensAfresh(t *testing.T) {
	for _, tc := range []struct {
		name  string
		draft string
	}{
		{"held (short)", "BAD draft that fails the check."},
		{"promoted (long)", "BAD " + strings.Repeat("The wind looks fine for the run across. ", 12)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tools := &draftCheckTools{fakeToolExecutor: &fakeToolExecutor{}, triggers: []string{"shelter at the anchorage", "depth at the anchorage"}}
			doer := queued(streamContent(tc.draft, 100, false), streamContent("Good answer.", 4, false))
			reply, err, tape := runWithTape(t, doer, tools, assistantRunnerOptions{})
			if err != nil || reply.Content != "Good answer." {
				t.Fatalf("reply %q err %v", reply.Content, err)
			}
			sent := tape.named("draft_sent_back")
			if len(sent) != 1 {
				t.Fatalf("events = %v, want one draft_sent_back", tape.names())
			}
			ev := sent[0].(mateDraftSentBackEvent)
			if ev.Draft != tc.draft || ev.Round != 0 || strings.Join(ev.Triggers, "|") != "shelter at the anchorage|depth at the anchorage" {
				t.Fatalf("draft_sent_back = %+v", ev)
			}
			at := tape.indexOf("draft_sent_back", 0)
			shown := ""
			for _, e := range tape.all()[:at] {
				if d, ok := e.(mateDeltaEvent); ok {
					shown += d.Text
				}
			}
			if !strings.HasPrefix(tc.draft, shown) {
				t.Fatalf("what streamed before it, %q, is not a prefix of the draft", shown)
			}
			if tc.name == "held (short)" && shown != "" {
				t.Fatalf("a short draft was held, yet %q streamed", shown)
			}
			if tape.indexOf("answer_start", at) < 0 {
				t.Fatalf("events = %v: no second answer_start after the draft went back", tape.names())
			}
			if len(tape.named("chatter")) != 0 {
				t.Fatalf("a draft sent back is not chatter: %v", tape.names())
			}
		})
	}
}

// A second draft that still fails is accepted, and only the first was sent back.
func TestEvents_ASecondFailingDraftIsAcceptedAfterOneSendBack(t *testing.T) {
	tools := &draftCheckTools{fakeToolExecutor: &fakeToolExecutor{}, triggers: []string{"depth at the anchorage"}}
	doer := queued(streamContent("BAD first.", 4, false), streamContent("BAD second.", 4, false))
	reply, err, tape := runWithTape(t, doer, tools, assistantRunnerOptions{})
	if err != nil || reply.Content != "BAD second." {
		t.Fatalf("reply %q err %v", reply.Content, err)
	}
	if n := len(tape.named("draft_sent_back")); n != 1 {
		t.Fatalf("draft_sent_back events = %d, want exactly one: %v", n, tape.names())
	}
	if got := tape.text("delta"); got != "BAD second." {
		t.Fatalf("deltas = %q: only the accepted draft streams", got)
	}
}

// No marker of a text tool call, or the start of one, reaches any lane.
func TestEvents_TextToolCallMarkupNeverReachesAnyLane(t *testing.T) {
	const marker = "<tool_call>"
	rest := `_call>{"name":"get_tides"}</tool_call> trailing`
	for _, tc := range []struct {
		name  string
		first string
		tool  bool
		// toolFirst puts the tool call between the two halves of the marker.
		toolFirst bool
	}{
		{"chatter", "Checking now. <tool", true, false},
		{"tool call begins mid-marker", "Checking now. <tool", true, true},
		{"held opening", "Checking now. <tool", false, false},
		{"promoted opening", strings.Repeat("The wind looks fine for the run across. ", 12) + "<tool", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parts := []string{activityChunk(activityContent(tc.first)), activityChunk(activityContent(rest))}
			finish := "stop"
			if tc.tool {
				finish = "tool_calls"
				if tc.toolFirst {
					parts = []string{parts[0], activityChunk(activityToolDelta), parts[1]}
				} else {
					parts = append(parts, activityChunk(activityToolDelta))
				}
			}
			parts = append(parts, activityFinish(finish))
			doer := queued(activityResponse(parts...), streamContent("Done.", 5, false))
			_, err, tape := runWithTape(t, doer, &fakeToolExecutor{}, assistantRunnerOptions{})
			if tc.tool && err != nil {
				t.Fatalf("run: %v", err)
			}
			if !tc.tool && err == nil {
				t.Fatal("the marker in an answer must fail the run")
			}
			var all strings.Builder
			for _, ev := range tape.all() {
				b, _ := json.Marshal(ev)
				all.Write(b)
			}
			for n := 1; n <= len(marker); n++ {
				if strings.Contains(all.String(), marker[:n]) {
					t.Fatalf("a lane carried %q: %s", marker[:n], all.String())
				}
			}
		})
	}
}

// Many reasoning fragments inside 100 ms are one event, flushed before the next
// event of another kind and at the round's end.
func TestEvents_ReasoningAndChatterAreCoalescedWithoutTimers(t *testing.T) {
	now := time.Unix(1000, 0)
	var tape []mateEvent
	c := newAssistantRoundCapture(func(ev mateEvent) { tape = append(tape, ev) }, now, 3, false)
	c.now = func() time.Time { return now }
	h := c.hooks()

	for _, f := range []string{"a", "b", "c", "d"} {
		h.Reasoning(f, false)
		now = now.Add(20 * time.Millisecond)
	}
	if len(tape) != 0 {
		t.Fatalf("events = %v: fragments inside the window must wait", tape)
	}
	// The next fragment after the window flushes, itself included.
	now = now.Add(100 * time.Millisecond)
	h.Reasoning("e", false)
	if len(tape) != 1 || tape[0].(mateReasoningEvent).Text != "abcde" || tape[0].(mateReasoningEvent).Round != 3 {
		t.Fatalf("events = %+v, want one reasoning event abcde", tape)
	}
	h.Reasoning("f", false)
	h.Reasoning("g", true)
	if len(tape) != 1 {
		t.Fatalf("events = %v", tape)
	}
	// Another kind of event flushes what waits, first.
	c.forcedFinal = true
	h.Content("Hello there, this is the answer.")
	if len(tape) != 4 || tape[1].(mateReasoningEvent).Text != "f\n\ng" || tape[2].mateEventName() != "answer_start" {
		t.Fatalf("events = %+v, want the reasoning flushed before answer_start", tape)
	}
	// And the round's end flushes too, before round_end.
	h.Reasoning("late", false)
	var tr assistantTraceRound
	c.finish(&tr)
	c.end(&tr)
	n := len(tape)
	if tape[n-2].(mateReasoningEvent).Text != "late" || tape[n-1].mateEventName() != "round_end" {
		t.Fatalf("events = %+v, want reasoning then round_end", tape)
	}
}

// A round's live reasoning stops at the cap, cut on a rune boundary, and
// round_end says how much the model really wrote.
func TestEvents_LiveReasoningStopsAtTheCapAndRoundEndSaysTheFullSize(t *testing.T) {
	now := time.Unix(1000, 0)
	var tape []mateEvent
	c := newAssistantRoundCapture(func(ev mateEvent) { tape = append(tape, ev) }, now, 0, false)
	c.now = func() time.Time { return now }
	h := c.hooks()

	// Three-byte runes, so the cap falls inside one.
	fragment := strings.Repeat("€", 333)
	const fragments = 40
	for i := 0; i < fragments; i++ {
		h.Reasoning(fragment, false)
		now = now.Add(200 * time.Millisecond)
	}
	var tr assistantTraceRound
	c.finish(&tr)
	c.end(&tr)

	var live strings.Builder
	for _, ev := range tape {
		if r, ok := ev.(mateReasoningEvent); ok {
			live.WriteString(r.Text)
		}
	}
	if live.Len() > assistantReasoningMaxBytes || live.Len() < assistantReasoningMaxBytes-3 || !utf8.ValidString(live.String()) {
		t.Fatalf("live reasoning is %d bytes (valid UTF-8 %v), want the cap of %d cut on a rune", live.Len(), utf8.ValidString(live.String()), assistantReasoningMaxBytes)
	}
	end := tape[len(tape)-1].(mateRoundEndEvent)
	if want := fragments * len(fragment); end.ReasoningBytes != want {
		t.Fatalf("round_end.reasoning_bytes = %d, want the full %d", end.ReasoningBytes, want)
	}
}

// A marker split across two fragments is caught wherever the split falls, and
// each fragment is scanned only with the tail of the text before it.
func TestEvents_AMarkerSplitAcrossFragmentsIsCaughtAtEverySplit(t *testing.T) {
	for _, marker := range assistantTextToolCallMarkers {
		for i := 1; i < len(marker); i++ {
			if !utf8.RuneStart(marker[i]) {
				continue
			}
			c := newAssistantRoundCapture(nil, time.Now(), 0, false)
			h := c.hooks()
			h.Content("Fine so far. " + marker[:i])
			h.Content(marker[i:])
			if !c.markerFound {
				t.Fatalf("marker %q split at %d was not caught", marker, i)
			}
		}
	}
}

func TestEvents_EachFragmentIsScannedForMarkersOnlyWithTheTextJustBeforeIt(t *testing.T) {
	c := newAssistantRoundCapture(nil, time.Now(), 0, false)
	scanned := 0
	c.scanMarker = func(text string) (string, bool) {
		scanned += len(text)
		return assistantTextToolCallMarker(text)
	}
	h := c.hooks()
	const fragments, size = 2000, 10
	for i := 0; i < fragments; i++ {
		h.Content("abcdefghij")
	}
	if limit := fragments * (size + assistantTextToolCallMarkerMaxLen); scanned > limit {
		t.Fatalf("scanned %d bytes for %d bytes of text, want at most %d", scanned, fragments*size, limit)
	}
}

// A round whose only reasoning is encrypted shows no reasoning, and says private.
func TestEvents_EncryptedOnlyReasoningIsPrivateWithNoReasoningEvents(t *testing.T) {
	body, err := os.ReadFile("testdata/openrouter_stream_reasoning_toolcall.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	doer := queued(openRouterFakeResponse(200, string(body)), finalResponse(t, "done", "m", openRouterUsage{}))
	_, runErr, tape := runWithTape(t, doer, &fakeToolExecutor{results: map[string]string{"get_tide_height": `{}`}}, assistantRunnerOptions{})
	if runErr != nil {
		t.Fatalf("run: %v", runErr)
	}
	if n := len(tape.named("reasoning")); n != 0 {
		t.Fatalf("%d reasoning events from an encrypted-only round", n)
	}
	ends := tape.named("round_end")
	if ends[0].(mateRoundEndEvent).Reasoning != "private" || ends[1].(mateRoundEndEvent).Reasoning != "none" {
		t.Fatalf("round_end = %+v", ends)
	}
}

// Visible reasoning streams as reasoning events.
func TestEvents_VisibleReasoningStreamsBeforeTheAnswer(t *testing.T) {
	doer := queued(activityResponse(
		activityChunk(activityText("need ")),
		activityChunk(activityText("the tide")),
		activityChunk(activityContent("High at six.")),
		activityFinish("stop"),
	))
	_, err, tape := runWithTape(t, doer, &fakeToolExecutor{}, assistantRunnerOptions{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := tape.text("reasoning"); got != "need the tide" {
		t.Fatalf("reasoning = %q", got)
	}
	if r, a := tape.indexOf("reasoning", 0), tape.indexOf("answer_start", 0); r < 0 || r > a {
		t.Fatalf("events = %v", tape.names())
	}
	if end := tape.named("round_end")[0].(mateRoundEndEvent); end.Reasoning != "text" || end.PromptTokens != 1 || end.CompletionTokens != 1 {
		t.Fatalf("round_end = %+v", end)
	}
}

// A lookup's text is the operator's wording, and every lookup that ran is
// closed.
func TestEvents_LookupsCarryTheOperatorsWordsAndAreClosed(t *testing.T) {
	doer := queued(toolCallResponse(t, "call_1", "get_tides", `{"lat":1,"lon":2}`, openRouterUsage{}), streamContent("Done.", 5, false))
	tools := &fakeToolExecutor{errs: map[string]error{"get_tides": errors.New("station offline")}}
	_, err, tape := runWithTape(t, doer, tools, assistantRunnerOptions{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	lookups, dones := tape.named("lookup"), tape.named("lookup_done")
	if len(lookups) != 1 || len(dones) != 1 {
		t.Fatalf("events = %v", tape.names())
	}
	l, d := lookups[0].(mateLookupEvent), dones[0].(mateLookupDoneEvent)
	if want := describeAssistantToolCall("get_tides", json.RawMessage(`{"lat":1,"lon":2}`)); l.Text != want || strings.Contains(l.Text, "get_tides") {
		t.Fatalf("lookup text %q, want %q and never the tool's name", l.Text, want)
	}
	if l.Skipped != "" || l.Round != 0 || l.Seq != 0 || d.Round != 0 || d.Seq != 0 || d.Outcome != "failed" || d.Reason != "station offline" || l.Subject == "" {
		t.Fatalf("lookup %+v done %+v", l, d)
	}
}

// A call that is not run says why, and gets no lookup_done.
func TestEvents_ACallThatIsNotRunIsSkippedWithAReason(t *testing.T) {
	t.Run("round full", func(t *testing.T) {
		var calls []openRouterToolCall
		for i := 0; i < assistantMaxToolCallsPerRound+1; i++ {
			calls = append(calls, openRouterToolCall{ID: fmt.Sprintf("c%d", i), Type: "function", Function: openRouterToolCallFunction{Name: "get_tides", Arguments: openRouterArguments(fmt.Sprintf(`{"lat":%d,"lon":2}`, i))}})
		}
		first := chatResponse(t, http.StatusOK, openRouterChatResponse{Choices: []openRouterChoice{{Message: openRouterMessage{Role: "assistant", ToolCalls: calls}}}})
		_, err, tape := runWithTape(t, queued(first, finalResponse(t, "ok", "m", openRouterUsage{})), &fakeToolExecutor{}, assistantRunnerOptions{})
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		lookups := tape.named("lookup")
		last := lookups[len(lookups)-1].(mateLookupEvent)
		if len(lookups) != assistantMaxToolCallsPerRound+1 || last.Skipped != "round_full" || last.Seq != assistantMaxToolCallsPerRound || strings.Contains(last.Text, "get_tides") {
			t.Fatalf("last lookup = %+v of %d", last, len(lookups))
		}
		if n := len(tape.named("lookup_done")); n != assistantMaxToolCallsPerRound {
			t.Fatalf("lookup_done events = %d, want one per call that ran", n)
		}
	})
	t.Run("withheld", func(t *testing.T) {
		var responses []*http.Response
		for i := 0; i <= assistantMaxToolFailures; i++ {
			// A new wind call each round keeps the run from stalling before the
			// tide tool's failures are used up.
			responses = append(responses, chatResponse(t, http.StatusOK, openRouterChatResponse{
				Choices: []openRouterChoice{{Message: openRouterMessage{Role: "assistant", ToolCalls: []openRouterToolCall{
					{ID: fmt.Sprintf("call_%d", i), Type: "function", Function: openRouterToolCallFunction{Name: "get_tides", Arguments: openRouterArguments(fmt.Sprintf(`{"lat":%d,"lon":2}`, i))}},
					{ID: fmt.Sprintf("wind_%d", i), Type: "function", Function: openRouterToolCallFunction{Name: "get_wind_forecast", Arguments: openRouterArguments(fmt.Sprintf(`{"lat":%d,"lon":2}`, i))}},
				}}}},
			}))
		}
		responses = append(responses, finalResponse(t, "ok", "m", openRouterUsage{}))
		tools := &fakeToolExecutor{errs: map[string]error{"get_tides": errors.New("down")}}
		_, err, tape := runWithTape(t, queued(responses...), tools, assistantRunnerOptions{})
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		var withheld []mateLookupEvent
		for _, ev := range tape.named("lookup") {
			if l := ev.(mateLookupEvent); l.Skipped == "withheld" {
				withheld = append(withheld, l)
			}
		}
		if len(withheld) != 1 || withheld[0].Seq != 0 || withheld[0].Round != assistantMaxToolFailures || strings.Contains(withheld[0].Text, "get_tides") {
			t.Fatalf("withheld lookups = %+v, want the 4th tide call, in the operator's words", withheld)
		}
	})
}

// An unattended run puts nothing on any stream.
func TestEvents_AnUnattendedRunEmitsNothing(t *testing.T) {
	doer := queued(
		activityResponse(
			activityChunk(activityText("quietly")),
			activityChunk(activityContent("Checking.")),
			activityChunk(activityToolDelta),
			activityFinish("tool_calls"),
		),
		streamContent("Fridge fine.", 4, false),
	)
	reply, err, tape := runWithTape(t, doer, &fakeToolExecutor{}, assistantRunnerOptions{Unattended: true})
	if err != nil || reply.Content != "Fridge fine." {
		t.Fatalf("reply %q err %v", reply.Content, err)
	}
	if names := tape.names(); len(names) != 0 {
		t.Fatalf("an unattended run emitted %v", names)
	}
}

// A reload mid-run replays the log and a fresh clock; two subscribers see the
// same bytes, and the clock is never in the log.
func TestEvents_AReloadReplaysTheLogUnderAFreshClock(t *testing.T) {
	prev := assistantRunClock
	t.Cleanup(func() { assistantRunClock = prev })
	start := time.Unix(5000, 0)
	assistantRunClock = func() time.Time { return start.Add(1500 * time.Millisecond) }

	run := newAssistantRun(func() {})
	run.started = start
	run.append(mateRoundEvent{Round: 0, AtMs: 3})
	run.append(mateChatterEvent{Round: 0, Text: "Looking."})
	run.append(mateLookupEvent{Round: 0, Seq: 0, AtMs: 40, Text: "Checking the tide"})
	run.finish()

	read := func() string {
		var out mateFlushBuffer
		streamAssistantRun(context.Background(), run, 0, &out)
		return out.String()
	}
	first, second := read(), read()
	if first != second {
		t.Fatalf("two subscribers saw different streams:\n%s\n%s", first, second)
	}
	if !strings.HasPrefix(first, "event: clock\ndata: {\"at_ms\":1500}\n\n") {
		t.Fatalf("stream = %q, want it to open with the clock", first)
	}
	if n := strings.Count(first, "event: clock"); n != 1 {
		t.Fatalf("clock frames = %d", n)
	}
	if evs, _, _ := run.snapshot(0); len(evs) != 3 {
		t.Fatalf("the log holds %d events, want 3 and no clock", len(evs))
	}
	// A later subscriber gets the same events and a later clock.
	assistantRunClock = func() time.Time { return start.Add(9 * time.Second) }
	later := read()
	if !strings.HasPrefix(later, "event: clock\ndata: {\"at_ms\":9000}\n\n") || strings.TrimPrefix(later, "event: clock\ndata: {\"at_ms\":9000}\n\n") != strings.TrimPrefix(first, "event: clock\ndata: {\"at_ms\":1500}\n\n") {
		t.Fatalf("a reload must replay the same events under a new clock: %q", later)
	}
}

// The run's one zero is taken just before the runner's first request to the
// model: the time between the request arriving and the runner starting is in
// neither the live round's at_ms, the trace's offsets nor the stream's clock.
// The clock frame reads 0 until the runner has started.
func TestEvents_TheZeroIsTakenWhenTheRunnerStartsNotWhenTheRunWasRegistered(t *testing.T) {
	prev := assistantRunClock
	t.Cleanup(func() { assistantRunClock = prev })
	registered := time.Unix(7000, 0)
	assistantRunClock = func() time.Time { return registered }
	run := newAssistantRun(func() {})

	// The runner starts five seconds after the run was registered.
	runnerStart := registered.Add(5 * time.Second)
	assistantRunClock = func() time.Time { return runnerStart }
	if got := run.clock().AtMs; got != 0 {
		t.Fatalf("clock before the runner started = %d ms, want 0", got)
	}

	rec := &assistantRunRecorder{onStart: run.setStarted}
	runner := &assistantRunner{doer: queued(streamContent("High at six.", 4, false)), apiKey: "k", model: "m", tools: &fakeToolExecutor{}, emit: run.append}
	if _, err := runner.run(withAssistantRunRecorder(context.Background(), rec), "system", "", nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	evs, _, _ := run.snapshot(0)
	var live mateRoundEvent
	for _, ev := range evs {
		if ev.Event == "round" {
			if err := json.Unmarshal(ev.Payload, &live); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	rounds, _ := rec.result()
	if live.AtMs != 0 || rounds[0].StartOffsetMs != 0 {
		t.Fatalf("live round at_ms = %d, stored start_offset_ms = %d: the gap before the runner started must not count", live.AtMs, rounds[0].StartOffsetMs)
	}
	if got := rec.startOr(time.Time{}); !got.Equal(runnerStart) {
		t.Fatalf("the trace's start = %v, want the runner's start %v", got, runnerStart)
	}
	assistantRunClock = func() time.Time { return runnerStart.Add(1500 * time.Millisecond) }
	if got := run.clock().AtMs; got != 1500 {
		t.Fatalf("clock = %d ms, want 1500 from the runner's start", got)
	}
}

// A forced final round whose text so far is only the start of a text tool call
// has nothing showable, so it is not promoted: no answer_start goes out, and
// none is taken back when the marker completes.
func TestEvents_AForcedFinalIsNotPromotedBeforeAnythingCanBeShown(t *testing.T) {
	var responses []*http.Response
	for i := 0; i < stalledToolRounds; i++ {
		responses = append(responses, toolCallResponse(t, fmt.Sprintf("call_%d", i), "get_tides", `{"lat":1,"lon":2}`, openRouterUsage{}))
	}
	responses = append(responses, activityResponse(
		activityChunk(activityContent("<tool")),
		activityChunk(activityContent(`_call>{"name":"get_tides"}</tool_call>`)),
		activityFinish("stop"),
	))
	_, err, tape := runWithTape(t, queued(responses...), &fakeToolExecutor{}, assistantRunnerOptions{})
	if err == nil {
		t.Fatal("the marker in the final answer must fail the run")
	}
	if len(tape.named("answer_start")) != 0 || len(tape.named("answer_withdrawn")) != 0 {
		t.Fatalf("events = %v: nothing showable, so nothing promoted or withdrawn", tape.names())
	}
}

// A round whose opening was sent as chatter (a tool call began) and which then
// resolves as the answer opens the answer afresh: the whole text goes out as the
// answer, from its first byte.
func TestEvents_AChatterRoundThatBecomesTheAnswerResendsFromTheStart(t *testing.T) {
	var tape []mateEvent
	c := newAssistantRoundCapture(func(ev mateEvent) { tape = append(tape, ev) }, time.Unix(1000, 0), 0, false)
	h := c.hooks()
	text := strings.Repeat("Let me check the tide. ", 6)
	h.Content(text)
	h.ToolCall(0)
	c.resolve(assistantRoundOutcome{kind: roundAnswered}, &assistantTraceRound{})
	var chatter, delta string
	var names []string
	for _, ev := range tape {
		names = append(names, ev.mateEventName())
		switch e := ev.(type) {
		case mateChatterEvent:
			chatter += e.Text
		case mateDeltaEvent:
			delta += e.Text
		}
	}
	if chatter == "" || !strings.HasPrefix(text, chatter) || delta != text {
		t.Fatalf("chatter %q, delta %q (events %v): chatter already sent stays, and the answer carries the whole text", chatter, delta, names)
	}
	if start, d := indexOfName(names, "answer_start"), indexOfName(names, "delta"); start < 0 || d < start {
		t.Fatalf("events = %v: answer_start comes before the delta", names)
	}
}

func captureTape() (*[]mateEvent, *assistantRoundCapture) {
	var tape []mateEvent
	c := newAssistantRoundCapture(func(ev mateEvent) { tape = append(tape, ev) }, time.Unix(1000, 0), 0, false)
	return &tape, c
}

// Unicode whitespace is blank everywhere in the capture: a forced final that
// opens with it does not promote, and does not panic, until real text arrives.
func TestEvents_AUnicodeWhitespaceOpeningIsNotPromoted(t *testing.T) {
	var tape []mateEvent
	c := newAssistantRoundCapture(func(ev mateEvent) { tape = append(tape, ev) }, time.Unix(1000, 0), 0, true)
	h := c.hooks()
	h.Content(strings.Repeat(" 　", 150))
	for _, ev := range tape {
		if ev.mateEventName() == "answer_start" {
			t.Fatal("answer_start before any real text")
		}
	}
	h.Content(strings.Repeat("Real text follows here. ", 20))
	started := false
	for _, ev := range tape {
		if ev.mateEventName() == "answer_start" {
			started = true
		}
	}
	if !started {
		t.Fatal("answer_start expected once real text arrived")
	}
}

// After a tool-call delta the promoted answer stops streaming: the round will
// ask for tools, so later text is held and ends as chatter.
func TestEvents_APromotedAnswerStopsAtAToolCallDelta(t *testing.T) {
	tape, c := captureTape()
	h := c.hooks()
	opening := strings.Repeat("The wind looks fine for the run. ", 14)
	h.Content(opening)
	h.ToolCall(0)
	at := len(*tape)
	later := " And then I will check the tide."
	h.Content(later)
	for _, ev := range (*tape)[at:] {
		if ev.mateEventName() == "delta" {
			t.Fatalf("a delta after the tool call: %+v", ev)
		}
	}
	c.resolve(assistantRoundOutcome{kind: roundAskedForTools}, &assistantTraceRound{})
	var chatter string
	var names []string
	for _, ev := range *tape {
		names = append(names, ev.mateEventName())
		if e, ok := ev.(mateChatterEvent); ok {
			chatter += e.Text
		}
	}
	if indexOfName(names, "answer_withdrawn") < 0 || !strings.Contains(chatter, strings.TrimSpace(later)) {
		t.Fatalf("events %v, chatter %q: want the answer withdrawn and the later text as chatter", names, chatter)
	}
}

// A draft sent back after part of the round went out as chatter records that
// chatter; the draft stays whole in CheckDraft.
func TestEvents_ASentBackRoundRecordsTheChatterItStreamed(t *testing.T) {
	_, c := captureTape()
	h := c.hooks()
	first := strings.Repeat("Let me look at the tide. ", 6)
	h.Content(first)
	h.ToolCall(0)
	h.Content(strings.Repeat("More thoughts here. ", 6))
	var tr assistantTraceRound
	c.finish(&tr)
	c.resolve(assistantRoundOutcome{kind: roundSentBack, triggers: []string{"x"}}, &tr)
	if tr.Chatter == "" || !strings.HasPrefix(tr.Chatter, first) || !strings.HasPrefix(tr.CheckDraft, tr.Chatter) || len(tr.CheckDraft) <= len(tr.Chatter) {
		t.Fatalf("chatter %q, draft %q: want the streamed part as chatter and the whole draft held", tr.Chatter, tr.CheckDraft)
	}
}

func indexOfName(names []string, name string) int {
	for i, n := range names {
		if n == name {
			return i
		}
	}
	return -1
}

// filingText is long enough that the forced final round has shown some of it
// before the submit call arrives.
const filingText = "Filing the review now with what I have gathered about the tides so far. "

const submitToolDelta = `{"tool_calls":[{"index":0,"id":"s0","type":"function","function":{"name":"submit_test","arguments":"{}"}}]}`

// A forced final round that files its result with the submit tool has not given
// its text as an answer: the stream and the trace settle it the same way as any
// round that asked for a tool, whether the filing worked or not.
func TestEvents_ASubmitRoundSettlesItsTextLikeAnyRoundThatAskedForATool(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fail  int
		wantR int
	}{
		{"filed", 0, 1},
		{"failed twice", 2, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var responses []*http.Response
			for i := 0; i < stalledToolRounds; i++ {
				responses = append(responses, toolCallResponse(t, fmt.Sprintf("call_%d", i), "get_tides", `{"lat":1,"lon":2}`, openRouterUsage{}))
			}
			for i := 0; i < tc.wantR; i++ {
				responses = append(responses, activityResponse(
					activityChunk(activityContent(filingText)),
					activityChunk(submitToolDelta),
					activityFinish("tool_calls"),
				))
			}
			tape, emit := newEventTape()
			rec := &assistantRunRecorder{}
			runner := &assistantRunner{doer: queued(responses...), apiKey: "k", model: "m", tools: &failingSubmitTools{fail: tc.fail}, emit: emit, options: assistantRunnerOptions{SubmitTool: "submit_test"}}
			_, _ = runner.run(withAssistantRunRecorder(context.Background(), rec), "system", "", nil)
			withdrawn := tape.named("answer_withdrawn")
			if len(withdrawn) != tc.wantR || withdrawn[0].(mateAnswerWithdrawnEvent).Reason != "lookups" {
				t.Fatalf("answer_withdrawn = %+v, want %d with reason lookups (events %v)", withdrawn, tc.wantR, tape.names())
			}
			// Each round's answer part and chatter part are pieces of its one text,
			// so between them they hold every byte of it, once.
			if got := tape.text("delta") + tape.text("chatter"); len(got) != len(filingText)*tc.wantR {
				t.Fatalf("shown %d bytes %q, want %d", len(got), got, len(filingText)*tc.wantR)
			}
			rounds, _ := rec.result()
			if last := rounds[len(rounds)-1]; last.Chatter != filingText {
				t.Fatalf("stored chatter %q, want the text recorded as chatter", last.Chatter)
			}
		})
	}
}

// stuckTools runs a call that ignores its context until released, as a tool
// that does not honour the tool phase's end would.
type stuckTools struct {
	*fakeToolExecutor
	release chan struct{}
}

func (s *stuckTools) execute(ctx context.Context, name string, args json.RawMessage) (string, assistantToolOutcome, error) {
	<-s.release
	return "{}", assistantToolOutcome{}, nil
}

// A lookup that was announced and then abandoned at the tool phase's end is
// closed as cut off, not as a failure, before its round ends.
func TestEvents_AnAbandonedLookupIsClosedAsCutNotAsAnError(t *testing.T) {
	shortBudget(t, 80*time.Millisecond, 5*time.Second)
	prev := assistantToolAbandonGrace
	assistantToolAbandonGrace = 20 * time.Millisecond
	t.Cleanup(func() { assistantToolAbandonGrace = prev })
	tools := &stuckTools{fakeToolExecutor: &fakeToolExecutor{}, release: make(chan struct{})}
	t.Cleanup(func() { close(tools.release) })

	doer := queued(newCallRound(t, 0), streamContent("Best answer.", 4, false))
	_, err, tape := runWithTape(t, doer, tools, assistantRunnerOptions{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	lookup, done, end := tape.indexOf("lookup", 0), tape.indexOf("lookup_done", 0), tape.indexOf("round_end", 0)
	if lookup < 0 || done < lookup || end < done {
		t.Fatalf("events = %v: want lookup, lookup_done, round_end", tape.names())
	}
	d := tape.all()[done].(mateLookupDoneEvent)
	if d.Seq != 0 || d.Outcome != "cut" || d.Reason != "" {
		t.Fatalf("lookup_done = %+v, want cut with no reason", d)
	}
}

// ctxTools runs a call that waits for the tool phase to end and then returns the
// context's error, as a tool that honours ctx does.
type ctxTools struct {
	*fakeToolExecutor
}

func (s *ctxTools) execute(ctx context.Context, name string, args json.RawMessage) (string, assistantToolOutcome, error) {
	<-ctx.Done()
	return "", assistantToolOutcome{}, ctx.Err()
}

// A tool that honours the time limit and returns its context error is cut off,
// not failed.
func TestEvents_AToolThatReturnsTheTimeLimitsErrorIsCutNotFailed(t *testing.T) {
	shortBudget(t, 80*time.Millisecond, 5*time.Second)
	tools := &ctxTools{fakeToolExecutor: &fakeToolExecutor{}}
	doer := queued(newCallRound(t, 0), streamContent("Best answer.", 4, false))
	_, err, tape := runWithTape(t, doer, tools, assistantRunnerOptions{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	done := tape.indexOf("lookup_done", 0)
	if done < 0 {
		t.Fatalf("events = %v: want a lookup_done", tape.names())
	}
	d := tape.all()[done].(mateLookupDoneEvent)
	if d.Outcome != "cut" || d.Reason != "" {
		t.Fatalf("lookup_done = %+v, want cut with no reason", d)
	}
}

// A tool that finishes just as the phase is abandoned is either done and in the
// trace, or cut and not: the stream and the trace never disagree.
func TestEvents_ALookupSaidDoneIsInTheTrace(t *testing.T) {
	shortBudget(t, 80*time.Millisecond, 5*time.Second)
	prev := assistantToolAbandonGrace
	assistantToolAbandonGrace = 20 * time.Millisecond
	t.Cleanup(func() { assistantToolAbandonGrace = prev })
	tools := &lateTools{fakeToolExecutor: &fakeToolExecutor{}}
	tape := &eventTape{}
	emit := func(ev mateEvent) {
		if d, ok := ev.(mateLookupDoneEvent); ok && d.Outcome != "cut" {
			// The emit runs under the runner's lock: hold it past the grace so the
			// abandonment is waiting for it.
			time.Sleep(100 * time.Millisecond)
		}
		tape.mu.Lock()
		tape.events = append(tape.events, ev)
		tape.mu.Unlock()
	}
	rec := &assistantRunRecorder{}
	runner := &assistantRunner{doer: queued(newCallRound(t, 0), streamContent("Best answer.", 4, false)), apiKey: "k", model: "m", tools: tools, emit: emit}
	if _, err := runner.run(withAssistantRunRecorder(context.Background(), rec), "system", "", nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	rounds, _ := rec.result()
	inTrace := len(rounds) > 0 && len(rounds[0].Calls) == 1
	var done, cut bool
	for _, ev := range tape.named("lookup_done") {
		if ev.(mateLookupDoneEvent).Outcome == "cut" {
			cut = true
		} else {
			done = true
		}
	}
	if done && !inTrace {
		t.Fatalf("stream says the lookup is done but the trace dropped it (events %v)", tape.names())
	}
	if !done && !cut {
		t.Fatalf("lookup never closed (events %v)", tape.names())
	}
}

// lateTools returns a result just after the tool phase ends, ignoring ctx.
type lateTools struct {
	*fakeToolExecutor
}

func (s *lateTools) execute(ctx context.Context, name string, args json.RawMessage) (string, assistantToolOutcome, error) {
	<-ctx.Done()
	return "{}", assistantToolOutcome{}, nil
}

// pipedStream is a response whose body the test feeds and then ends with err
// (nil ends it cleanly).
func pipedStream(parts []string, err error) *http.Response {
	pr, pw := io.Pipe()
	go func() {
		for _, p := range parts {
			_, _ = io.WriteString(pw, p)
		}
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		pw.Close()
	}()
	return &http.Response{StatusCode: 200, Body: pr, Header: make(http.Header)}
}

// firstRoundTrace is the trace's entry for round 0.
func firstRoundTrace(t *testing.T, rec *assistantRunRecorder) assistantTraceRound {
	t.Helper()
	rounds, _ := rec.result()
	if len(rounds) == 0 {
		t.Fatal("no rounds in the trace")
	}
	return rounds[0]
}

func runRecorded(t *testing.T, doer openRouterDoer) (error, *eventTape, *assistantRunRecorder) {
	t.Helper()
	tape, emit := newEventTape()
	rec := &assistantRunRecorder{}
	runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: &fakeToolExecutor{}, emit: emit}
	_, err := runner.run(withAssistantRunRecorder(context.Background(), rec), "system", "", nil)
	return err, tape, rec
}

// A completion that fails mid-stream streams the text it held as chatter, and
// the trace records the same.
func TestEvents_AFailedRoundStreamsAndRecordsItsHeldTextTheSame(t *testing.T) {
	held := "Let me think about the tide here"
	err, tape, rec := runRecorded(t, budgetDoerFunc(func(*http.Request) (*http.Response, error) {
		return pipedStream([]string{activityChunk(activityContent(held))}, errors.New("connection reset")), nil
	}))
	if err == nil {
		t.Fatal("expected the run to fail")
	}
	tr := firstRoundTrace(t, rec)
	if got := tape.text("chatter"); got != held || tr.Chatter != held {
		t.Fatalf("live chatter %q, stored chatter %q, want both %q", got, tr.Chatter, held)
	}
	if end, chatter := tape.indexOf("round_end", 0), tape.indexOf("chatter", 0); chatter < 0 || end < chatter {
		t.Fatalf("events = %v: chatter before round_end", tape.names())
	}
}

// A raw text tool call ends the run, and what came before it is chatter that
// streamed and was recorded the same, with no marker in either.
func TestEvents_ATextToolCallRoundKeepsTheTextBeforeTheMarkerAsChatter(t *testing.T) {
	parts := []string{
		activityChunk(activityContent("Checking now. ")),
		activityChunk(activityContent(`<tool_call>{"name":"get_tides"}</tool_call>`)),
		activityFinish("stop"),
	}
	err, tape, rec := runRecorded(t, budgetDoerFunc(func(*http.Request) (*http.Response, error) {
		return pipedStream(parts, nil), nil
	}))
	if err == nil {
		t.Fatal("the marker in an answer must fail the run")
	}
	tr := firstRoundTrace(t, rec)
	if got := tape.text("chatter"); got != "Checking now. " || tr.Chatter != got {
		t.Fatalf("live chatter %q, stored %q, want both %q", got, tr.Chatter, "Checking now. ")
	}
}

// Promoted text that the round then fails is taken back like any other.
func TestEvents_APromotedOpeningThatEndsInATextToolCallIsWithdrawn(t *testing.T) {
	long := strings.Repeat("The wind looks fine for the run across. ", 12)
	parts := []string{
		activityChunk(activityContent(long)),
		activityChunk(activityContent(`<tool_call>{"name":"get_tides"}</tool_call>`)),
		activityFinish("stop"),
	}
	err, tape, rec := runRecorded(t, budgetDoerFunc(func(*http.Request) (*http.Response, error) {
		return pipedStream(parts, nil), nil
	}))
	if err == nil {
		t.Fatal("the marker in an answer must fail the run")
	}
	withdrawn := tape.named("answer_withdrawn")
	if len(withdrawn) != 1 || withdrawn[0].(mateAnswerWithdrawnEvent).Reason != "failed" {
		t.Fatalf("answer_withdrawn = %+v, want one with reason failed (events %v)", withdrawn, tape.names())
	}
	tr := firstRoundTrace(t, rec)
	if got := tape.text("delta") + tape.text("chatter"); got != long || tr.Chatter != long {
		t.Fatalf("shown %q, stored %q, want both the opening %q", got, tr.Chatter, long)
	}
}

// Past the cap the reasoning kept for the trace stops growing, block breaks
// included.
func TestEvents_ReasoningBlockBreaksDoNotGrowTheHeadPastTheCap(t *testing.T) {
	c := newAssistantRoundCapture(nil, time.Now(), 0, false)
	h := c.hooks()
	h.Reasoning(strings.Repeat("a", assistantReasoningMaxBytes), false)
	for i := 0; i < 50; i++ {
		h.Reasoning("b", true)
	}
	if n := c.reasoningHead.Len(); n > assistantReasoningMaxBytes {
		t.Fatalf("head is %d bytes, cap %d", n, assistantReasoningMaxBytes)
	}
}
