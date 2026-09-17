package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sync"
	"time"
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
// event name ("status", "delta", "retract", "message", "error") and its
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
}

// newAssistantRun builds a run already wired to cancelFn - the
// context.CancelFunc for the context the runner will actually execute
// against (see assistantRunRegistry.start).
func newAssistantRun(cancelFn context.CancelFunc) *assistantRun {
	return &assistantRun{cancelFn: cancelFn, changed: make(chan struct{}), released: make(chan struct{})}
}

// append adds one event to the log and wakes every current subscriber.
// Marshalling happens once, here, rather than once per subscriber - the
// same shape assistantEmitter's callers already expect (assistant_run.go's
// assistantStatus/assistantDelta/assistantRetractPayload all return a plain
// value meant to be marshalled, not JSON already).
func (r *assistantRun) append(event string, payload any) {
	data, err := json.Marshal(payload)
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
// only via its own cancel() or assistantRunTimeout.
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

	for {
		events, done, changed := run.snapshot(i)
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
