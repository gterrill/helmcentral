package main

import (
	"encoding/json"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
)

// assistantRoundCapture is the one place a round's streamed output is observed
// and sorted into lanes (ADR 0305). Its hooks fill it as the stream arrives, so
// a round that fails mid-stream still has what came before; finish copies it
// into the round's trace entry, and the live stream is its emit.
//
// A round's text starts waiting. It becomes chatter when the model starts a
// tool call (text alongside tool calls is thinking aloud, ADR 0093), or the
// answer when it is promoted: at once on the forced final round; at a paragraph
// break or assistantAnswerHoldBytes with no tool call begun; or at the round's
// end when no tools were asked for and the draft was accepted. A promoted
// opening that then ends in tools is taken back (withdraw), which is rare and
// shown as such. Every lane sends only what assistantSafeStreamLen has cleared,
// so a text tool-call marker never reaches any of them.
//
// The runner says what the round turned out to be with resolve, and the lookups
// its tools run go through lookup and lookupDone, so everything on the stream
// takes the same path and keeps its order.
type assistantRoundCapture struct {
	emit        assistantEmitter
	round       int
	forcedFinal bool
	runStart    time.Time
	// now is the clock the capture's times and coalescing read; a test sets it.
	now func() time.Time
	// scanMarker looks for a text tool-call marker; a test counts what it is given.
	scanMarker func(string) (string, bool)

	// The reasoning head is kept up to the cap; reasoningSize is its full
	// size. Whether it is cut for storage is the store's business. reasoningLive
	// is how much of it the stream has been given, which stops at the same cap.
	reasoningHead strings.Builder
	reasoningSize int
	reasoningLive int
	private       bool

	// opened says the completion got a 2xx response, so there was a stream to
	// observe. Without it the round never reached the model's output and its
	// reasoning kind is not recorded rather than "none". Only the Opened hook
	// sets it.
	opened bool

	// content is the round's text, whole. textStarted is read from another
	// goroutine (the tool phase's end) and so is atomic.
	content      strings.Builder
	contentStart *int64
	textStarted  atomic.Bool

	// The lanes are exclusive: the text goes to one of them, and sent is how many
	// bytes of content that lane (or the lane it was taken back into) has been
	// given. scanned is how many bytes have been searched for a marker.
	lane        assistantRoundLane
	sent        int
	scanned     int
	markerFound bool
	// answerHeld says a tool call began after the answer was promoted, so no
	// more of it is sent as the answer.
	answerHeld bool

	// pending is the one kind of text (reasoning or chatter) waiting to be sent
	// in a single event, and since when.
	pendingKind  string
	pendingText  strings.Builder
	pendingSince time.Time
}

type assistantRoundLane int

const (
	// laneWaiting: the text so far is held, not yet the answer or chatter.
	laneWaiting assistantRoundLane = iota
	laneChatter
	laneAnswer
)

// assistantRoundOutcomeKind is what a round turned out to be.
type assistantRoundOutcomeKind int

const (
	// roundAnswered: the text is the answer, with whatever the check found in it
	// that was let through.
	roundAnswered assistantRoundOutcomeKind = iota
	// roundSentBack: the draft failed the check and the model is asked again.
	roundSentBack
	// roundAskedForTools: the round went on to ask for lookups.
	roundAskedForTools
	// roundCutOff: the tool phase ran out of time during the round.
	roundCutOff
	// roundFailed: the round's completion failed, or what it wrote cannot be
	// used (a tool call written as text, nothing at all).
	roundFailed
)

// assistantRoundOutcome is what the runner tells the capture at a round's end.
// Triggers are what the draft check found in the round's text.
type assistantRoundOutcome struct {
	kind     assistantRoundOutcomeKind
	triggers []string
}

// assistantAnswerHoldBytes is how much opening text a round holds before it is
// taken for the answer when no paragraph break or tool call has settled it.
const assistantAnswerHoldBytes = 400

// assistantCoalesceWindow is how long reasoning or chatter waits to be sent in
// one event. There is no timer: the next fragment after the window, the next
// event of another kind, or the round's end sends it.
const assistantCoalesceWindow = 100 * time.Millisecond

const (
	pendingReasoning = "reasoning"
	pendingChatter   = "chatter"
)

// newAssistantRoundCapture starts round's capture. emit may be nil, which
// sends nothing and still records the trace.
func newAssistantRoundCapture(emit assistantEmitter, runStart time.Time, round int, forcedFinal bool) *assistantRoundCapture {
	if emit == nil {
		emit = func(mateEvent) {}
	}
	return &assistantRoundCapture{emit: emit, runStart: runStart, round: round, forcedFinal: forcedFinal, now: func() time.Time { return assistantRunClock() }, scanMarker: assistantTextToolCallMarker}
}

// assistantReasoningBlockBreak goes between two reasoning entries, so they do
// not run together as one sentence.
const assistantReasoningBlockBreak = "\n\n"

func (c *assistantRoundCapture) atMs() int64 { return c.now().Sub(c.runStart).Milliseconds() }

// send sends ev after whatever reasoning or chatter is waiting, so the stream
// keeps the order things happened in.
func (c *assistantRoundCapture) send(ev mateEvent) {
	c.flushPending()
	c.emit(ev)
}

// queue adds text to the event of kind that is waiting, and sends it when the
// window it began in has passed.
func (c *assistantRoundCapture) queue(kind, text string) {
	if c.pendingKind != kind {
		c.flushPending()
		c.pendingKind = kind
	}
	if c.pendingText.Len() == 0 {
		c.pendingSince = c.now()
	}
	c.pendingText.WriteString(text)
	if c.now().Sub(c.pendingSince) >= assistantCoalesceWindow {
		c.flushPending()
	}
}

func (c *assistantRoundCapture) flushPending() {
	if c.pendingText.Len() == 0 {
		return
	}
	text, kind := c.pendingText.String(), c.pendingKind
	c.pendingText.Reset()
	c.pendingKind = ""
	if kind == pendingChatter {
		c.emit(mateChatterEvent{Round: c.round, Text: text})
		return
	}
	c.emit(mateReasoningEvent{Round: c.round, Text: text})
}

// begin opens the round on the stream and returns the at_ms it was opened at,
// which is also the round's start offset in the trace.
func (c *assistantRoundCapture) begin() int64 {
	at := c.atMs()
	c.emit(mateRoundEvent{Round: c.round, AtMs: at, Final: c.forcedFinal})
	return at
}

// queueReasoning sends reasoning text to the stream, up to the cap a round's
// reasoning is stored at, cut on a rune boundary. What the model wrote past it
// still counts in the round's size, so round_end can say the view was cut.
func (c *assistantRoundCapture) queueReasoning(text string) {
	room := assistantReasoningMaxBytes - c.reasoningLive
	if len(text) > room {
		text = cutUTF8(text, max(room, 0))
		c.reasoningLive = assistantReasoningMaxBytes
	} else {
		c.reasoningLive += len(text)
	}
	if text != "" {
		c.queue(pendingReasoning, text)
	}
}

func (c *assistantRoundCapture) hooks() openRouterStreamHooks {
	return openRouterStreamHooks{
		Opened: func() { c.opened = true },
		Content: func(fragment string) {
			c.textStarted.Store(true)
			// The run thought until the answer's first real text; a leading
			// newline is not it.
			if c.contentStart == nil && strings.TrimSpace(fragment) != "" {
				ms := c.atMs()
				c.contentStart = &ms
			}
			c.content.WriteString(fragment)
			c.advance()
		},
		Reasoning: func(text string, newBlock bool) {
			// The size is the model's own text; the break is not part of it.
			sent := text
			if newBlock && c.reasoningSize > 0 {
				if c.reasoningHead.Len() < assistantReasoningMaxBytes {
					c.reasoningHead.WriteString(assistantReasoningBlockBreak)
				}
				sent = assistantReasoningBlockBreak + text
			}
			c.reasoningSize += len(text)
			if c.reasoningHead.Len() < assistantReasoningMaxBytes {
				c.reasoningHead.WriteString(text)
			}
			c.queueReasoning(sent)
		},
		Private: func() { c.private = true },
		ToolCall: func(int) {
			// Text before a tool call is chatter, whatever was held. A promoted
			// answer stops streaming: the round will ask for tools and withdraw
			// it, so the rest is held for the chatter.
			switch c.lane {
			case laneWaiting:
				c.lane = laneChatter
				c.advance()
			case laneAnswer:
				c.answerHeld = true
			}
		},
	}
}

// advance gives the lane the text that has become safe to show.
func (c *assistantRoundCapture) advance() {
	if c.markerFound {
		return
	}
	text := c.content.String()
	// A marker split across fragments straddles the end of what was already
	// searched, so the search starts that far back, and no further.
	from := max(0, c.scanned-(assistantTextToolCallMarkerMaxLen-1))
	if _, found := c.scanMarker(text[from:]); found {
		c.markerFound = true
		return
	}
	c.scanned = len(text)
	safe := assistantSafeStreamLen(text)
	if c.lane == laneWaiting && c.shouldPromote(text, safe) {
		c.promote()
	}
	c.sendLane(text, safe)
}

// shouldPromote says the held opening is the answer, once some of it can be
// shown: there must be non-blank text inside what assistantSafeStreamLen has
// cleared, so an answer is never opened with nothing to put in it. The forced
// final round's text is the answer from then on; any other round's is once it
// has a paragraph break or is long.
func (c *assistantRoundCapture) shouldPromote(text string, safe int) bool {
	opening := strings.TrimLeftFunc(text[:safe], unicode.IsSpace)
	if opening == "" {
		return false
	}
	if c.forcedFinal {
		return true
	}
	return len(opening) >= assistantAnswerHoldBytes || strings.Contains(opening, "\n\n")
}

// promote takes the round's text for the answer. Its at_ms is when the text
// began, the same time the trace calls the end of the run's thinking. Only text
// with something in it is promoted, and the first such fragment sets
// contentStart, so it is always set here.
func (c *assistantRoundCapture) promote() {
	c.lane = laneAnswer
	c.send(mateAnswerStartEvent{Round: c.round, AtMs: *c.contentStart})
}

// sendLane gives the current lane the text up to safe that it has not been
// given: the answer as a delta, chatter queued to be coalesced. A waiting round
// gives nothing.
func (c *assistantRoundCapture) sendLane(text string, safe int) {
	if safe <= c.sent || c.lane == laneWaiting || (c.lane == laneAnswer && c.answerHeld) {
		return
	}
	piece := text[c.sent:safe]
	if c.lane == laneAnswer {
		c.send(mateDeltaEvent{Text: piece})
	} else {
		// Blank text ahead of the first words is not a step of its own; it goes
		// with them.
		if c.sent == 0 && strings.TrimSpace(piece) == "" {
			return
		}
		c.queue(pendingChatter, piece)
	}
	c.sent = safe
}

// resolve says what the round turned out to be. It puts the matching event on
// the stream and sets what the trace entry records of it, so the two cannot
// disagree: the text is chatter unless the round was the answer or a draft sent
// back, and then it is not.
func (c *assistantRoundCapture) resolve(o assistantRoundOutcome, tr *assistantTraceRound) {
	switch o.kind {
	case roundAnswered:
		tr.Chatter = c.endAnswer()
		if len(o.triggers) > 0 {
			tr.CheckTriggers = o.triggers
		}
	case roundSentBack:
		// The browser takes whatever of the draft was shown out of the answer, so
		// the event carries all of it.
		draft := c.content.String()
		c.send(mateDraftSentBackEvent{Round: c.round, AtMs: c.atMs(), Triggers: append([]string{}, o.triggers...), Draft: draft})
		// The draft is held whole in CheckDraft; only text that already went
		// out as chatter is recorded as chatter, so the trace matches the stream.
		tr.Chatter = ""
		if c.lane == laneChatter {
			if shown := draft[:c.sent]; strings.TrimSpace(shown) != "" {
				tr.Chatter = shown
			}
		}
		tr.CheckDraft, tr.CheckTriggers = draft, o.triggers
	case roundAskedForTools:
		c.withdraw(mateWithdrawnLookups, tr)
	case roundCutOff:
		c.withdraw(mateWithdrawnCutOff, tr)
	case roundFailed:
		c.withdraw(mateWithdrawnFailed, tr)
	}
}

// endAnswer ends a round whose text is the answer: the draft was accepted. What
// the stream was still holding goes out now that the whole text is known clean.
// A round that had been sent as chatter and turns out to be the answer opens the
// answer afresh and sends all of its text from the first byte; the chatter
// already sent stays in the timeline as chatter. It returns that chatter, for
// the trace to record the same.
func (c *assistantRoundCapture) endAnswer() (chatter string) {
	text := c.content.String()
	if c.lane != laneAnswer {
		if strings.TrimSpace(text) == "" {
			return ""
		}
		if c.lane == laneChatter {
			if shown := text[:c.sent]; strings.TrimSpace(shown) != "" {
				chatter = shown
			}
			c.sent = 0
		}
		c.promote()
	}
	if tail := text[c.sent:]; tail != "" {
		c.send(mateDeltaEvent{Text: tail})
		c.sent = len(text)
	}
	return chatter
}

// withdraw ends a round whose text was not the answer after all: the round went
// on to ask for tools, was cut off, or failed. Text already shown as the answer
// is taken back, and the part the stream still held goes out as chatter, up to
// any text tool-call marker; text not yet shown is chatter all along. The trace
// records the chatter that went out, so it and the stream agree.
func (c *assistantRoundCapture) withdraw(reason string, tr *assistantTraceRound) {
	if c.lane == laneAnswer {
		c.send(mateAnswerWithdrawnEvent{Round: c.round, AtMs: c.atMs(), Reason: reason})
	}
	c.lane = laneChatter
	text := c.content.String()
	limit := len(text)
	// A round that asked for tools wrote a whole response. One that was cut off
	// or failed may end inside a text tool-call marker, so the tail the
	// hold-back would still be holding (the start of a marker) is not sent.
	// Clean text ending short of the hold-back's reach is sent whole.
	if reason != mateWithdrawnLookups {
		limit = min(limit, len(text)-partialMarkerTail(text))
	}
	if c.markerFound {
		for _, marker := range assistantTextToolCallMarkers {
			if i := strings.Index(text, marker); i >= 0 {
				limit = min(limit, i)
			}
		}
	}
	c.sendLane(text, limit)
	c.flushPending()
	tr.Chatter = ""
	if shown := text[:c.sent]; strings.TrimSpace(shown) != "" {
		tr.Chatter = shown
	}
}

// partialMarkerTail is how many bytes at the end of text are the start of a text
// tool-call marker, none when the text does not end inside one.
func partialMarkerTail(text string) int {
	tail := 0
	for _, marker := range assistantTextToolCallMarkers {
		for k := min(len(marker)-1, len(text)); k > tail; k-- {
			if strings.HasSuffix(text, marker[:k]) {
				tail = k
				break
			}
		}
	}
	return tail
}

// finish sets what the round streamed on its trace entry. The text is its
// chatter; resolve clears it when the round turns out to be the answer or a
// draft sent back.
func (c *assistantRoundCapture) finish(tr *assistantTraceRound) {
	tr.Reasoning, tr.ReasoningBytes = c.reasoningHead.String(), c.reasoningSize
	tr.ReasoningKind = ""
	if c.opened {
		tr.ReasoningKind = assistantReasoningKind(c.reasoningSize, c.private)
	}
	tr.Chatter = ""
	if text := c.content.String(); strings.TrimSpace(text) != "" {
		tr.Chatter = text
	}
	tr.ContentStartMs = c.contentStart
}

// end closes the round on the stream, after anything still waiting.
func (c *assistantRoundCapture) end(tr *assistantTraceRound) {
	c.send(mateRoundEndEvent{
		Round:            c.round,
		AtMs:             c.atMs(),
		Reasoning:        tr.ReasoningKind,
		ReasoningBytes:   tr.ReasoningBytes,
		PromptTokens:     tr.PromptTokens,
		CompletionTokens: tr.CompletionTokens,
		CachedTokens:     tr.CachedTokens,
		CostUSD:          tr.CostUSD,
	})
}

// lookup announces a call of the round's, or says why it is not run (skipped).
// The runner's tool phase serialises calls to it and to lookupDone.
func (c *assistantRoundCapture) lookup(seq int, call openRouterToolCall, args json.RawMessage, skipped string) {
	c.send(mateLookupEvent{Round: c.round, Seq: seq, AtMs: c.atMs(), Text: describeAssistantToolCall(call.Function.Name, args), Skipped: skipped})
}

// lookupDone says the call at seq is over, with errText when it failed.
func (c *assistantRoundCapture) lookupDone(seq int, errText string) {
	c.send(mateLookupDoneEvent{Round: c.round, Seq: seq, AtMs: c.atMs(), Error: errText})
}

// lookupCut says the call at seq was abandoned at the tool phase's time limit.
// It did not fail, so it carries no error.
func (c *assistantRoundCapture) lookupCut(seq int) {
	c.send(mateLookupDoneEvent{Round: c.round, Seq: seq, AtMs: c.atMs(), Cut: true})
}
