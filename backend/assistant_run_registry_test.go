package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// blockingSSEWriter is an assistantRunSSEWriter whose Write blocks forever
// until unblock is closed - a stalled subscriber that never reads a real
// HTTP connection at all. Used to prove append/finish on the run they are
// "watching" never wait on them (see
// TestAssistantRun_StalledSubscriberDoesNotBlockAppends).
type blockingSSEWriter struct {
	unblock <-chan struct{}
}

func (w *blockingSSEWriter) Write(p []byte) (int, error) {
	<-w.unblock
	return len(p), nil
}

func (w *blockingSSEWriter) Flush() {}

// TestAssistantRun_StalledSubscriberDoesNotBlockAppends is the "a slow or
// disconnected subscriber must never block or slow the run" guarantee this
// registry exists to provide. append/finish only ever touch the run's own
// log and its changed channel - never a subscriber's writer - so a
// goroutine stuck forever inside streamAssistantRun's write to a wedged
// writer must never be able to hold up the goroutine actually driving the
// run.
func TestAssistantRun_StalledSubscriberDoesNotBlockAppends(t *testing.T) {
	run := newAssistantRun(func() {})

	unblock := make(chan struct{}) // never closed - the subscriber is stalled for good
	stalled := &blockingSSEWriter{unblock: unblock}
	subscriberCtx, cancelSubscriber := context.WithCancel(context.Background())
	t.Cleanup(cancelSubscriber) // let the leaked goroutine's ctx.Done() unstick it... but Write is what's actually stuck.

	// Seed one event so the stalled subscriber's first snapshot read has
	// something to write and gets stuck on that Write call, exactly like a
	// client whose socket never drains.
	run.append("status", assistantStatus("Thinking…"))
	go streamAssistantRun(subscriberCtx, run, 0, stalled)

	// Give the stalled goroutine a moment to actually reach the blocking
	// Write call before the real assertion below - this is not what proves
	// the guarantee (the timeout on the run's own work is), just makes the
	// scenario realistic.
	time.Sleep(20 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 5000; i++ {
			run.append("delta", assistantDelta("x"))
		}
		run.append("message", map[string]string{"content": "done"})
		run.finish()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run.append/finish blocked - a stalled subscriber must never slow the run down")
	}
}

// TestAssistantRun_ReplaysLogThenLiveEvents is a focused unit test of
// streamAssistantRun's own contract (the handler-level tests exercise the
// same thing end to end over real SSE frames): events already in the log
// before a subscriber attaches are replayed in order, further events
// appended afterward are streamed live, and the stream ends once finish()
// is called - after, not before, the final event is visible.
func TestAssistantRun_ReplaysLogThenLiveEvents(t *testing.T) {
	run := newAssistantRun(func() {})
	run.append("status", assistantStatus("Thinking…"))
	run.append("delta", assistantDelta("Tongue "))

	var sb strings.Builder
	w := &syncBufferSSEWriter{sb: &sb}

	streamed := make(chan struct{})
	go func() {
		streamAssistantRun(context.Background(), run, 0, w)
		close(streamed)
	}()

	waitForSubstring(t, w, "event: delta\ndata: {\"text\":\"Tongue \"}", time.Second)

	run.append("delta", assistantDelta("Bay"))
	waitForSubstring(t, w, "event: delta\ndata: {\"text\":\"Bay\"}", time.Second)

	run.append("message", map[string]string{"content": "Tongue Bay"})
	run.finish()

	select {
	case <-streamed:
	case <-time.After(time.Second):
		t.Fatal("streamAssistantRun did not return after finish()")
	}

	body := w.String()
	statusIdx := strings.Index(body, "event: status")
	firstDeltaIdx := strings.Index(body, "\"text\":\"Tongue ")
	secondDeltaIdx := strings.Index(body, "\"text\":\"Bay\"")
	messageIdx := strings.Index(body, "event: message")
	if statusIdx < 0 || firstDeltaIdx < 0 || secondDeltaIdx < 0 || messageIdx < 0 {
		t.Fatalf("expected all four events in order, got:\n%s", body)
	}
	if !(statusIdx < firstDeltaIdx && firstDeltaIdx < secondDeltaIdx && secondDeltaIdx < messageIdx) {
		t.Fatalf("expected replay-then-live order status < delta1 < delta2 < message, got:\n%s", body)
	}
}

// TestAssistantRun_CancelIsIdempotentAndMarksCancelled proves cancel() can
// be called any number of times safely (postAssistantRunCancelHandler's
// 204-either-way contract rests on this) and that wasCancelled() reports
// true from the first call onward.
func TestAssistantRun_CancelIsIdempotentAndMarksCancelled(t *testing.T) {
	cancelCalls := 0
	run := newAssistantRun(func() { cancelCalls++ })

	if run.wasCancelled() {
		t.Fatal("expected wasCancelled() to be false before cancel()")
	}
	run.cancel()
	run.cancel()
	run.cancel()

	if !run.wasCancelled() {
		t.Fatal("expected wasCancelled() to be true after cancel()")
	}
	if cancelCalls != 3 {
		t.Fatalf("expected the underlying cancel func to be called every time, got %d calls", cancelCalls)
	}
}

// TestAssistantRunRegistry_StartGetRemove exercises the registry's own
// bookkeeping directly: a second start() for the same conversation fails
// while the first is still registered, get() finds it, and remove() only
// deletes the entry if it still points at the same run (never a newer run
// that has since replaced it).
func TestAssistantRunRegistry_StartGetRemove(t *testing.T) {
	reg := &assistantRunRegistry{runs: make(map[string]*assistantRun)}

	_, run1, ok := reg.start("c1")
	if !ok || run1 == nil {
		t.Fatalf("expected the first start() to succeed, got ok=%v", ok)
	}
	if _, _, ok := reg.start("c1"); ok {
		t.Fatal("expected a second start() for the same conversation to fail while the first is registered")
	}

	got, ok := reg.get("c1")
	if !ok || got != run1 {
		t.Fatal("expected get() to return the same run start() created")
	}

	// A stale run (already superseded, which start's own guard should make
	// impossible today, but remove must stay defensive regardless) must
	// never delete a newer run's entry.
	stale := newAssistantRun(func() {})
	reg.remove("c1", stale)
	if _, ok := reg.get("c1"); !ok {
		t.Fatal("remove() with a stale run pointer deleted the current run's entry")
	}

	reg.remove("c1", run1)
	if _, ok := reg.get("c1"); ok {
		t.Fatal("expected remove() with the current run pointer to delete the entry")
	}

	if _, _, ok := reg.start("c1"); !ok {
		t.Fatal("expected start() to succeed again once the entry was removed")
	}
}

// syncBufferSSEWriter is a thread-safe assistantRunSSEWriter over a
// strings.Builder, for tests that read the accumulated body from a
// different goroutine than the one calling streamAssistantRun (mirroring a
// real HTTP client's own body reader running independently of the server's
// write goroutine).
type syncBufferSSEWriter struct {
	mu sync.Mutex
	sb *strings.Builder
}

func (w *syncBufferSSEWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sb.Write(p)
}

func (w *syncBufferSSEWriter) Flush() {}

func (w *syncBufferSSEWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sb.String()
}

// waitForSubstring polls w.String() for substr, failing the test if it
// hasn't appeared within timeout. Appending to an assistantRun's log wakes
// a live streamAssistantRun subscriber via a closed channel, not a
// timer, so in practice this resolves within microseconds; the poll only
// exists to avoid a fixed sleep racing that handoff.
func waitForSubstring(t *testing.T, w *syncBufferSSEWriter, substr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(w.String(), substr) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in:\n%s", substr, w.String())
}
