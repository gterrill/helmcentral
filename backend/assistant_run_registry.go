package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// This file backs ADR 0105's "the answer outlives the page" behaviour: an
// assistant reply keeps running server-side after the operator navigates
// away or the request that started it disconnects, and a page (the same
// tab, a reload, or a different device on the same session) can rejoin it
// later. postAssistantMessageHandler (assistant_handlers.go) starts a run
// here instead of driving assistant_run.go's runner directly off the
// request's own context; getAssistantRunHandler and
// postAssistantRunCancelHandler are what let a page reattach to, or stop,
// whatever is still going.

// assistantRunEvent is one entry in a run's append-only event log: the
// event name ("round", "delta", "lookup", "message", "error" and the rest of
// the set in assistant_run.go; never "clock", which is not stored) and its
// already-marshalled JSON payload, so every subscriber replays the exact
// bytes the run produced rather than re-encoding a shared value once per
// reader.
type assistantRunEvent struct {
	Event   string
	Payload json.RawMessage
}

// assistantRun is one in-flight (or just-finished) assistant reply. It is
// shared between the goroutine driving assistantRunner.run and every HTTP
// handler streaming its progress: the POST that started it, and any number
// of GET .../run callers attaching or reattaching later.
//
// append never blocks on a subscriber: it only ever appends to log and then
// closes changed, which wakes every current waiter (see snapshot and
// streamAssistantRun below) without knowing how many waiters exist or
// whether any of them are still reading at all. A subscriber that stops
// reading - a browser tab whose fetch stalled, an iPad that locked its
// screen - can therefore never slow the run down; it just stops seeing new
// events until, if ever, it reads again.
type assistantRun struct {
	mu   sync.Mutex
	log  []assistantRunEvent
	done bool
	// changed is closed and replaced on every append/finish, the signal
	// every waiting subscriber selects on. A fresh channel each time means
	// "wait for the next change" is always just "receive from the current
	// value of changed", with no separate broadcast bookkeeping.
	changed   chan struct{}
	cancelFn  context.CancelFunc
	cancelled bool
	// released is closed when the registry drops this run, the moment the
	// conversation can take its next question. postAssistantRunCancelHandler
	// waits on it so Stop never hands the composer back while a new question
	// would still get 409.
	released chan struct{}
	// started is the instant just before the runner's first request to the
	// model: the zero of every at_ms on its stream, and the start its trace uses.
	// It is zero until the runner takes it (setStarted), and guarded by mu.
	// streamAssistantRun tells each subscriber how far in the server is
	// (mateClockEvent) from it.
	started time.Time
}

// assistantRunClock is the time the run's zero is taken from, and the time its
// rounds' at_ms and a subscription's clock event read. A var so a test can fix
// it.
var assistantRunClock = time.Now

// newAssistantRun builds a run already wired to cancelFn - the
// context.CancelFunc for the context the runner will actually execute
// against (see assistantRunRegistry.start).
func newAssistantRun(cancelFn context.CancelFunc) *assistantRun {
	return &assistantRun{cancelFn: cancelFn, changed: make(chan struct{}), released: make(chan struct{})}
}

// clock is the event that opens a subscription: how far into the run the
// server is now. It is built fresh for each subscriber and never stored in the
// log, so a reload replays the same events under a new clock. Until the runner
// has started it reads 0; streamAssistantRun does not send it until the run has
// started, the log holds an event, or the run is done, so the browser's zero is
// not set early on a live run.
func (r *assistantRun) clock() mateClockEvent {
	ev, _ := r.clockIfStarted()
	return ev
}

// clockIfStarted is the clock, and whether the runner has taken the start.
func (r *assistantRun) clockIfStarted() (mateClockEvent, bool) {
	r.mu.Lock()
	started := r.started
	r.mu.Unlock()
	if started.IsZero() {
		return mateClockEvent{}, false
	}
	return mateClockEvent{AtMs: assistantRunClock().Sub(started).Milliseconds()}, true
}

// setStarted records the instant the run's clock counts from and wakes the
// subscribers waiting for it.
func (r *assistantRun) setStarted(at time.Time) {
	r.mu.Lock()
	r.started = at
	ch := r.changed
	r.changed = make(chan struct{})
	r.mu.Unlock()
	close(ch)
}

// append adds one event to the log and wakes every current subscriber.
// Marshalling happens once, here, rather than once per subscriber.
func (r *assistantRun) append(ev mateEvent) {
	event := ev.mateEventName()
	data, err := json.Marshal(ev)
	if err != nil {
		log.Printf("assistant: marshal %s event for run: %v", event, err)
		return
	}
	r.mu.Lock()
	r.log = append(r.log, assistantRunEvent{Event: event, Payload: data})
	ch := r.changed
	r.changed = make(chan struct{})
	r.mu.Unlock()
	close(ch)
}

// finish marks the run done. Callers append the run's final event
// ("message" or "error") before calling this, never after - a subscriber
// that observes done=true assumes the log already holds everything the run
// will ever produce, and stops waiting for more (streamAssistantRun).
func (r *assistantRun) finish() {
	r.mu.Lock()
	r.done = true
	ch := r.changed
	r.changed = make(chan struct{})
	r.mu.Unlock()
	close(ch)
}

// cancel stops the run: it flags the run as operator-cancelled, so the
// goroutine driving it (postAssistantMessageHandler) can report a plain
// "stopped" instead of whatever raw error falls out of the context
// cancellation (a wrapped context.Canceled from deep inside the OpenRouter
// client, most likely), and it cancels the context assistantRunner.run is
// actually executing against. Safe to call more than once -
// postAssistantRunCancelHandler's idempotence rests on that, and
// context.CancelFunc itself already tolerates repeat calls.
func (r *assistantRun) cancel() {
	r.mu.Lock()
	r.cancelled = true
	fn := r.cancelFn
	r.mu.Unlock()
	fn()
}

// wasCancelled reports whether cancel() was ever called on this run.
func (r *assistantRun) wasCancelled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cancelled
}

// snapshot returns a copy of the log from index i onward, whether the run is
// done, and the channel to wait on for the next change. Always a fresh copy
// - streamAssistantRun must never hold r.mu while it writes to a (possibly
// slow) response.
func (r *assistantRun) snapshot(i int) (events []assistantRunEvent, done bool, changed chan struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i < len(r.log) {
		events = append(events, r.log[i:]...)
	}
	return events, r.done, r.changed
}

// assistantRunRegistry tracks the one in-flight run per conversation,
// replacing the old assistantRunsInFlight sync.Map guard: start still
// answers the same "is one already running" question
// postAssistantMessageHandler needs for its 409, but now also hands back
// the context and *assistantRun the goroutine and every subscriber share.
type assistantRunRegistry struct {
	mu   sync.Mutex
	runs map[string]*assistantRun
}

// globalAssistantRuns is the process-wide run registry, mirroring
// globalAssistantStore's package-level-var pattern so tests can inspect or
// reset it directly where needed.
var globalAssistantRuns = &assistantRunRegistry{runs: make(map[string]*assistantRun)}

// start registers a new run for conversationID, unless one is already
// running - in which case it returns ok=false and the caller
// (postAssistantMessageHandler) answers 409, exactly as beginAssistantRun
// did. The returned context is derived from context.Background(), not any
// HTTP request's - see ADR 0105 "the answer outlives the page": the run
// this context drives must survive every subscriber disconnecting, and end
// only via its own cancel() or the run ceiling (assistantRunCeiling).
func (reg *assistantRunRegistry) start(conversationID string) (ctx context.Context, run *assistantRun, ok bool) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if _, exists := reg.runs[conversationID]; exists {
		return nil, nil, false
	}
	runCtx, cancel := context.WithCancel(context.Background())
	run = newAssistantRun(cancel)
	reg.runs[conversationID] = run
	return runCtx, run, true
}

// get returns the run currently in flight for conversationID, if any -
// getAssistantRunHandler's 204-vs-stream decision and
// postAssistantRunCancelHandler's cancel target.
func (reg *assistantRunRegistry) get(conversationID string) (*assistantRun, bool) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	run, ok := reg.runs[conversationID]
	return run, ok
}

// remove deletes conversationID's entry, but only if it still points at
// run: a run that has already been superseded (which start's 409 guard
// should make impossible today, but this keeps remove safe regardless of
// that) must never delete a newer run's entry out from under it.
func (reg *assistantRunRegistry) remove(conversationID string, run *assistantRun) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.runs[conversationID] == run {
		delete(reg.runs, conversationID)
		close(run.released)
	}
}

// assistantRunStreamKeepalive matches logsStreamHandler's own interval: long
// enough to never trip a proxy's idle-connection buffering during an
// ordinary tool round, short enough that a client watching the connection
// doesn't mistake a long round for a dead one.
const assistantRunStreamKeepalive = 15 * time.Second

// assistantRunSSEWriter is the minimal seam streamAssistantRun needs from an
// echo.Response: just enough to write and flush a frame. Small enough that
// a test can substitute its own writer where a real HTTP response isn't the
// point (e.g. proving a stalled subscriber's Write blocking forever never
// slows the run itself).
type assistantRunSSEWriter interface {
	io.Writer
	Flush()
}

// writeAssistantRunSSEHeaders sets the response headers streamAssistantRun's
// framing depends on and writes the 200 status line that must precede it.
// postAssistantMessageHandler and getAssistantRunHandler both call this
// right before streamAssistantRun - they only differ in how they got the
// *assistantRun to stream (starting one vs. attaching to one already
// running).
func writeAssistantRunSSEHeaders(c echo.Context) {
	header := c.Response().Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
	c.Response().WriteHeader(http.StatusOK)
}

// streamAssistantRun writes run's event log as SSE frames to w, starting at
// index fromIndex, then continues live until the run finishes or ctx is
// done. ctx here is always the *subscriber's* context (the HTTP request
// this call is serving), never the run's own - a subscriber leaving can
// therefore never reach back and cancel the run it was only watching.
//
// Nothing here ever touches assistantRun's internals except through
// snapshot/append's own locking, and no lock is held across the blocking
// write to w - a slow or wedged subscriber blocks only this call, on this
// goroutine, never the goroutine actually driving the run (which only ever
// calls append/finish, and neither touches w at all).
func streamAssistantRun(ctx context.Context, run *assistantRun, fromIndex int, w assistantRunSSEWriter) {
	i := fromIndex
	keepalive := time.NewTicker(assistantRunStreamKeepalive)
	defer keepalive.Stop()

	// Every subscription opens with the clock, so the browser can place each
	// event's at_ms on its own time. It is not in the log: a replay for a later
	// subscriber gets the same events and a clock of its own. The browser takes
	// its zero when the clock arrives, so the clock waits for the runner's start
	// and goes out ahead of the first event (the round event is the first to
	// carry an at_ms, and is only appended after the start). A run that ended
	// without starting has no at_ms to place and sends the clock at 0.
	clockSent := false

	for {
		events, done, changed := run.snapshot(i)
		if !clockSent {
			clock, started := run.clockIfStarted()
			// The runner appends nothing before it has taken the start, so an
			// event already in the log means the start is as set as it will be.
			if started || done || len(events) > 0 {
				clockData, err := json.Marshal(clock)
				if err != nil {
					log.Printf("assistant: marshal clock event: %v", err)
					return
				}
				if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", clock.mateEventName(), clockData); err != nil {
					return
				}
				w.Flush()
				clockSent = true
			}
		}
		for _, e := range events {
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Event, e.Payload); err != nil {
				return
			}
			w.Flush()
			i++
		}
		if done {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-changed:
		case <-keepalive.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			w.Flush()
		}
	}
}
