package main

import (
	"sync/atomic"
	"testing"
	"time"
)

// shrinkTelemetryStreamTickForTest speeds up the hub's driving loop for a
// test, restoring the production value afterward. The real 1s resolution
// (telemetryStreamTick) is what buildAndBroadcast's TryLock guard, dueAt's
// scheduling and this file's slower tests all still exercise unshrunk; this
// is only for tests that need many ticks to happen inside a short test
// timeout (e.g. filling a 32-slot subscriber buffer) without either waiting
// on real wall-clock seconds or hand-rolling a substitute for the actual
// driving loop.
func shrinkTelemetryStreamTickForTest(t *testing.T, tick time.Duration) {
	t.Helper()
	original := telemetryStreamTick
	telemetryStreamTick = tick
	t.Cleanup(func() { telemetryStreamTick = original })
}

// waitForFrame reads from sub.frames until it sees event, or fails the test
// after timeout. Any other event received while waiting is ignored (the
// heartbeat-less hubs built in this file only ever carry the one event
// under test, but this keeps the helper honest if that changes).
func waitForFrame(t *testing.T, sub *telemetryHubSubscriber, event string, timeout time.Duration) telemetryHubFrame {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case frame := <-sub.frames:
			if frame.event == event {
				return frame
			}
		case <-sub.done:
			t.Fatalf("subscriber was dropped while waiting for event %q", event)
		case <-deadline:
			t.Fatalf("timed out waiting for event %q", event)
		}
	}
}

// Two subscribers must share exactly one build per interval, not one each —
// the entire point of the hub replacing per-connection emitters. The build
// stamps its own call count into the payload so this holds regardless of any
// timing slop between the two subscribers receiving it and the test checking
// a separately-read counter afterward.
func TestTelemetryHub_TwoSubscribersShareOneBuildPerInterval(t *testing.T) {
	hub := newTelemetryHub()
	var calls int32
	hub.events = []*streamEmitter{
		{event: "x", interval: 1 * time.Hour, build: func() map[string]any {
			n := atomic.AddInt32(&calls, 1)
			return map[string]any{"n": n}
		}},
	}

	s1 := hub.Subscribe()
	defer hub.Unsubscribe(s1)
	s2 := hub.Subscribe()
	defer hub.Unsubscribe(s2)

	f1 := waitForFrame(t, s1, "x", 2*time.Second)
	f2 := waitForFrame(t, s2, "x", 2*time.Second)

	want := `{"n":1}`
	if f1.payload != want || f2.payload != want {
		t.Fatalf("expected both subscribers to see the single first build (n=1), got s1=%q s2=%q", f1.payload, f2.payload)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected exactly 1 build for 2 subscribers, got %d", got)
	}
}

// A subscriber arriving after the hub is already running must get the
// current cached frame immediately, not wait for the next tick. The event's
// interval (1h) rules out a second live build landing within the test.
func TestTelemetryHub_LateSubscriberGetsCachedFrameImmediately(t *testing.T) {
	hub := newTelemetryHub()
	hub.events = []*streamEmitter{
		{event: "x", interval: 1 * time.Hour, build: func() map[string]any { return map[string]any{"v": 1} }},
	}

	s1 := hub.Subscribe()
	defer hub.Unsubscribe(s1)
	waitForFrame(t, s1, "x", 2*time.Second)

	s2 := hub.Subscribe()
	defer hub.Unsubscribe(s2)
	frame := waitForFrame(t, s2, "x", 2*time.Second)

	if frame.payload != `{"v":1}` {
		t.Fatalf("expected the late subscriber to get the cached frame immediately, got %q", frame.payload)
	}
}

// Once the last subscriber leaves, the hub must stop building entirely — an
// idle backend does no work. removeSubscriber blocks until the driving
// goroutine has actually exited (see telemetryHub.stopped), so this can
// check the call count deterministically rather than guessing at a sleep
// long enough to "probably" have caught a stray tick.
func TestTelemetryHub_StopsBuildingOnceLastSubscriberLeaves(t *testing.T) {
	shrinkTelemetryStreamTickForTest(t, 5*time.Millisecond)

	hub := newTelemetryHub()
	var calls int32
	hub.events = []*streamEmitter{
		{event: "x", interval: 5 * time.Millisecond, build: func() map[string]any {
			atomic.AddInt32(&calls, 1)
			return map[string]any{}
		}},
	}

	s1 := hub.Subscribe()
	waitForFrame(t, s1, "x", 2*time.Second)
	hub.Unsubscribe(s1)

	afterStop := atomic.LoadInt32(&calls)
	// Several would-be 5ms intervals; if the hub were still ticking, this
	// would easily pick up more builds.
	time.Sleep(100 * time.Millisecond)
	if got := atomic.LoadInt32(&calls); got != afterStop {
		t.Fatalf("expected no builds after the last subscriber left (was %d), got %d more", afterStop, got-afterStop)
	}
}

// A subscriber that never drains its channel must be dropped once its
// buffer fills, and must never stall delivery to any other subscriber in
// the meantime.
func TestTelemetryHub_StuckSubscriberIsDroppedWithoutStallingOthers(t *testing.T) {
	shrinkTelemetryStreamTickForTest(t, 5*time.Millisecond)

	hub := newTelemetryHub()
	hub.events = []*streamEmitter{
		{event: "x", interval: 5 * time.Millisecond, build: func() map[string]any {
			return map[string]any{"t": time.Now().UnixNano()}
		}},
	}

	stuck := hub.Subscribe() // deliberately never read from stuck.frames
	fine := hub.Subscribe()
	defer hub.Unsubscribe(fine)

	deadline := time.After(5 * time.Second)
	stuckDropped := false
	fineFrames := 0
	for !stuckDropped || fineFrames < 5 {
		select {
		case <-stuck.done:
			stuckDropped = true
		case <-fine.frames:
			fineFrames++
		case <-deadline:
			t.Fatalf("timed out: stuckDropped=%v fineFrames=%d (a stuck subscriber must not stall the others)", stuckDropped, fineFrames)
		}
	}
}

// A sole subscriber that never reads its channel gets its buffer filled and
// dropped by the hub itself (dropSubscriber, called from inside a build
// goroutine tracked by run()'s WaitGroup). Since this was the last
// subscriber, removeSubscriber decides to stop the hub. If the drop path
// waited for that stop (as Unsubscribe correctly does), it would deadlock:
// run() can't finish wg.Wait() until this very build goroutine returns, and
// this goroutine can't return until run() finishes. The goroutine would
// also leave the event's buildMu locked forever (its deferred Unlock never
// runs), so the event could never build again even after a fresh
// subscriber arrives. This is the kiosk-is-the-only-client-and-stalls case.
func TestTelemetryHub_DropOfLastSubscriberDoesNotDeadlockAndEventBuildsAgainAfter(t *testing.T) {
	shrinkTelemetryStreamTickForTest(t, 5*time.Millisecond)

	hub := newTelemetryHub()
	hub.events = []*streamEmitter{
		{event: "x", interval: 5 * time.Millisecond, build: func() map[string]any {
			return map[string]any{"t": time.Now().UnixNano()}
		}},
	}

	stuck := hub.Subscribe() // sole subscriber, never drained

	select {
	case <-stuck.done:
		// Dropped as expected; the call inside the hub that did this must
		// already have returned by now for this select to even be reachable
		// promptly rather than hanging — but assert the timeout explicitly
		// below too, since a deadlocked goroutine leaves stuck.done open
		// forever rather than failing fast.
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for the sole stuck subscriber to be dropped (hub likely deadlocked)")
	}

	// The hub must have fully stopped and released the event's buildMu, so
	// a fresh subscriber sees it build again rather than sitting silent
	// forever.
	fresh := hub.Subscribe()
	defer hub.Unsubscribe(fresh)

	select {
	case <-fresh.frames:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for a fresh subscriber to receive a build of the event whose drop triggered the last-subscriber stop (buildMu likely still locked)")
	}
}

// A client that unsubscribes and immediately resubscribes (a kiosk page
// reload does this within milliseconds) must see every event rebuilt and
// broadcast on the new run's first pass, even though every event's own
// interval says it isn't due yet. nextDue values surviving a stop/start
// cycle would otherwise leave the resubscribed client waiting out each
// event's full interval before hearing anything from it.
func TestTelemetryHub_ResubscribeImmediatelyRebuildsDespiteFutureNextDue(t *testing.T) {
	hub := newTelemetryHub()
	hub.events = []*streamEmitter{
		{event: "x", interval: 1 * time.Hour, build: func() map[string]any { return map[string]any{"v": 1} }},
	}

	s1 := hub.Subscribe()
	waitForFrame(t, s1, "x", 2*time.Second)
	hub.Unsubscribe(s1) // waits for the run to fully stop; nextDue for "x" is now ~1h in the future

	s2 := hub.Subscribe()
	defer hub.Unsubscribe(s2)
	waitForFrame(t, s2, "x", 2*time.Second)
}

// Companion to the above: even once an event is due again, the change gate
// must not suppress it just because the rebuilt payload happens to equal
// whatever was cached from the previous run — a fresh run has no
// subscribers carrying that old frame yet, so its first pass must broadcast
// regardless of whether the value actually changed. Today this is masked by
// every real payload carrying a changing datetime field, but a resubscribed
// client must not depend on that to hear about tanks-state, solar-state and
// the like.
func TestTelemetryHub_ResubscribeImmediatelyRebroadcastsUnchangedPayload(t *testing.T) {
	hub := newTelemetryHub()
	hub.events = []*streamEmitter{
		{event: "x", interval: 1 * time.Millisecond, build: func() map[string]any { return map[string]any{"v": 1} }},
	}

	s1 := hub.Subscribe()
	waitForFrame(t, s1, "x", 2*time.Second)
	hub.Unsubscribe(s1)

	s2 := hub.Subscribe()
	defer hub.Unsubscribe(s2)
	waitForFrame(t, s2, "x", 2*time.Second)
}

// TestStreamEmitterDueAt_DoesNotDriftUnderJitter guards the drift bug the
// audit measured on the boat: with the original nextDue = now.Add(interval)
// formula, a build that landed even slightly late (relative to the previous
// tick) permanently shifted nextDue forward by that overshoot, and the next
// tick — running on the ticker's own unshifted schedule — could then arrive
// with *less* overshoot than the one baked into nextDue, land before it, and
// get skipped. Repeating, this is what turned "every 1s" into "every ~2s"
// (gaps of 0.85, 2.00, 2.01, 2.00, 1.00s).
//
// This sequence is a verified, worked example of exactly that: four ticks at
// 0ms, 1003ms, 2001ms and 3002ms (interval 1s) with the old formula skip the
// third tick (2001ms arrives before the 2003ms the previous fire's overshoot
// baked into nextDue), while the fixed dueAt — which advances nextDue from
// its own previous value rather than from now — fires on every one of them.
func TestStreamEmitterDueAt_DoesNotDriftUnderJitter(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ticks := []time.Time{
		start,
		start.Add(1003 * time.Millisecond),
		start.Add(2001 * time.Millisecond),
		start.Add(3002 * time.Millisecond),
	}

	e := &streamEmitter{event: "x", interval: 1 * time.Second}

	for i, now := range ticks {
		if !e.dueAt(now) {
			t.Fatalf("tick %d (t=%v): expected dueAt to fire, but it did not (nextDue=%v) — this is the drift bug", i, now.Sub(start), e.nextDue.Sub(start))
		}
	}
}
