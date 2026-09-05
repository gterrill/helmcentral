package main

import (
	"bytes"
	"errors"
	"log"
	"testing"
	"time"
)

// ghostPressureRateValue is the unacknowledged shape a live ghost can carry.
// The acknowledged variant, captured off the boat, is exercised separately
// below (TestBusEchoReconcilerClearsAnAcknowledgedGhost) because acknowledged
// state must not change whether something counts as a ghost.
func ghostPressureRateValue() map[string]any {
	return map[string]any{
		"state":   "warn",
		"message": "Barometer falling: below -0.027777777777777776 (-0.0283)",
		"method":  []any{"visual", "sound"},
	}
}

// reconcileOwnershipPredicate exercises the real ownership rule with no rules
// configured, so these tests stay honest about production logic: it owns
// exactly the derived-path namespace, which is all a ghost fixture needs.
func reconcileOwnershipPredicate(path string) bool {
	return helmcentralOwnsPath(path, nil)
}

func newTestBusEchoReconciler(snapshot *signalKSnapshot, publish func(string, any) error, enabled bool) *busEchoReconciler {
	return &busEchoReconciler{
		snapshot:         snapshot,
		publish:          publish,
		transportEnabled: func() bool { return enabled },
		lastAttempt:      map[string]time.Time{},
		warnedDisabled:   map[string]bool{},
	}
}

func TestBusEchoReconcilerClearsAGhostTheEngineDoesNotHoldLive(t *testing.T) {
	snapshot := snapshotWithNotification("notifications.helmcentral.environment.pressureRate", ghostPressureRateValue())

	calls := 0
	var gotPath string
	var gotValue any
	publish := func(path string, value any) error {
		calls++
		gotPath, gotValue = path, value
		return nil
	}

	r := newTestBusEchoReconciler(snapshot, publish, true)
	r.check(alarmNow, reconcileOwnershipPredicate, map[string]bool{})

	if calls != 1 {
		t.Fatalf("expected exactly one publish call, got %d", calls)
	}
	if gotPath != "notifications.helmcentral.environment.pressureRate" {
		t.Fatalf("path: got %q", gotPath)
	}
	if gotValue != nil {
		t.Fatalf("clearing a notification writes nil, got %v", gotValue)
	}
}

func TestBusEchoReconcilerLeavesAPathTheEngineHoldsLiveAlone(t *testing.T) {
	snapshot := snapshotWithNotification("notifications.helmcentral.environment.pressureRate", ghostPressureRateValue())

	calls := 0
	publish := func(string, any) error { calls++; return nil }

	r := newTestBusEchoReconciler(snapshot, publish, true)
	r.check(alarmNow, reconcileOwnershipPredicate, map[string]bool{"helmcentral.environment.pressureRate": true})

	if calls != 0 {
		t.Fatalf("the engine holds this alarm live, so it is not a ghost: expected no publish, got %d", calls)
	}
}

func TestBusEchoReconcilerLeavesAForeignNotificationAlone(t *testing.T) {
	snapshot := snapshotWithNotification("notifications.radar.fur6424A.guardZone.1", map[string]any{
		"state":   "alert",
		"message": "Radar fur6424A guard zone 1: target 100000294 acquired",
		"method":  []any{},
	})

	calls := 0
	publish := func(string, any) error { calls++; return nil }

	r := newTestBusEchoReconciler(snapshot, publish, true)
	r.check(alarmNow, reconcileOwnershipPredicate, map[string]bool{})

	if calls != 0 {
		t.Fatalf("a notification Helmcentral does not own must never be touched, got %d publish calls", calls)
	}
}

// The dev laptop must never write to the boat's bus -- that is the entire
// point of the transport switch -- so a disabled transport reports the ghost
// and stops, and does not repeat the warning on every tick.
func TestBusEchoReconcilerWarnsOnceAndDoesNothingWhileTheTransportIsDisabled(t *testing.T) {
	snapshot := snapshotWithNotification("notifications.helmcentral.environment.pressureRate", ghostPressureRateValue())

	calls := 0
	publish := func(string, any) error { calls++; return nil }

	origOutput := log.Writer()
	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(origOutput) })

	r := newTestBusEchoReconciler(snapshot, publish, false)
	r.check(alarmNow, reconcileOwnershipPredicate, map[string]bool{})
	r.check(alarmNow.Add(time.Second), reconcileOwnershipPredicate, map[string]bool{})

	if calls != 0 {
		t.Fatalf("the transport is disabled: expected no publish calls, got %d", calls)
	}
	if got := bytes.Count(logBuf.Bytes(), []byte("cannot be cleared from here")); got != 1 {
		t.Fatalf("expected exactly one warning across two ticks, got %d in: %s", got, logBuf.String())
	}
}

func TestBusEchoReconcilerRetriesAFailedClearOnlyAfterTheCooldown(t *testing.T) {
	snapshot := snapshotWithNotification("notifications.helmcentral.environment.pressureRate", ghostPressureRateValue())

	calls := 0
	publish := func(string, any) error {
		calls++
		return errors.New("signalk returned status 502")
	}

	r := newTestBusEchoReconciler(snapshot, publish, true)

	r.check(alarmNow, reconcileOwnershipPredicate, map[string]bool{})
	if calls != 1 {
		t.Fatalf("expected the first attempt to publish, got %d calls", calls)
	}

	r.check(alarmNow.Add(1*time.Minute), reconcileOwnershipPredicate, map[string]bool{})
	if calls != 1 {
		t.Fatalf("a tick inside the cooldown must not retry a failed clear, got %d calls", calls)
	}

	r.check(alarmNow.Add(6*time.Minute), reconcileOwnershipPredicate, map[string]bool{})
	if calls != 2 {
		t.Fatalf("a tick past the cooldown must retry, got %d calls", calls)
	}
}

// The exact live shape captured off the boat: acknowledged true, method
// empty. Acknowledgement changes how a notification displays, not whether it
// is a ghost -- this engine still is not holding it live, so it still needs
// clearing.
func TestBusEchoReconcilerClearsAnAcknowledgedGhost(t *testing.T) {
	snapshot := snapshotWithNotification("notifications.helmcentral.environment.pressureRate", map[string]any{
		"state":   "warn",
		"message": "Barometer falling: below -0.027777777777777776 (-0.0283)",
		"method":  []any{},
		"id":      "89f3b708-602d-4882-8044-182e6e24a134",
		"status": map[string]any{
			"silenced": false, "acknowledged": true,
			"canSilence": true, "canAcknowledge": true, "canClear": false,
		},
	})

	calls := 0
	publish := func(string, any) error { calls++; return nil }

	r := newTestBusEchoReconciler(snapshot, publish, true)
	r.check(alarmNow, reconcileOwnershipPredicate, map[string]bool{})

	if calls != 1 {
		t.Fatalf("an acknowledged ghost is still a ghost and must still be cleared, got %d calls", calls)
	}
}
