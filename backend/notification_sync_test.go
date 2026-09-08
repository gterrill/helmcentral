package main

import (
	"bytes"
	"errors"
	"log"
	"slices"
	"strings"
	"testing"
	"time"
)

// ── interval gating ──────────────────────────────────────────────────────

func TestNotificationSyncerRunsOnFirstTickThenWaitsForInterval(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.contexts["vessels.self"] = map[string]any{}
	snapshot.setSelfContext("vessels.self")

	syncer := newNotificationSyncer(snapshot)
	syncer.interval = 30 * time.Second

	calls := 0
	syncer.fetch = func(vesselID string) (map[string]any, bool, error) {
		calls++
		return map[string]any{}, true, nil
	}

	now := time.Date(2026, 9, 8, 5, 0, 0, 0, time.UTC)
	syncer.check(now)
	if calls != 1 {
		t.Fatalf("expected the first tick to run, got %d calls", calls)
	}

	syncer.check(now.Add(10 * time.Second))
	if calls != 1 {
		t.Fatalf("expected no run before the interval elapses, got %d calls", calls)
	}

	syncer.invalidate()
	syncer.check(now.Add(11 * time.Second))
	if calls != 2 {
		t.Fatalf("invalidate must force the next tick to run regardless of interval, got %d calls", calls)
	}

	syncer.check(now.Add(12 * time.Second))
	if calls != 2 {
		t.Fatalf("expected no run before the interval elapses again, got %d calls", calls)
	}

	syncer.check(now.Add(41 * time.Second))
	if calls != 3 {
		t.Fatalf("expected a run once the interval elapses, got %d calls", calls)
	}
}

// ── which contexts get fetched ───────────────────────────────────────────

func TestNotificationSyncerFetchesSelfAndOnlyLiveOtherContexts(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.contexts["vessels.self"] = map[string]any{}
	snapshot.setSelfContext("vessels.self")

	// A live one: must be fetched.
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.live-target",
		Updates: []signalKUpdate{{Values: []signalKValue{{
			Path:  "notifications.navigation.closestApproach",
			Value: map[string]any{"state": "warn", "message": "Closing"},
		}}}},
	}, alarmNow)

	// A cleared one: must not be fetched.
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.cleared-target",
		Updates: []signalKUpdate{{Values: []signalKValue{{
			Path:  "notifications.navigation.closestApproach",
			Value: map[string]any{"state": "normal", "message": "Watching"},
		}}}},
	}, alarmNow)

	// No notifications at all: must not be fetched.
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.quiet-target",
		Updates: []signalKUpdate{{Values: []signalKValue{{
			Path:  "navigation.position",
			Value: map[string]any{"latitude": 1.0, "longitude": 2.0},
		}}}},
	}, alarmNow)

	syncer := newNotificationSyncer(snapshot)
	var fetched []string
	syncer.fetch = func(vesselID string) (map[string]any, bool, error) {
		fetched = append(fetched, vesselID)
		return map[string]any{}, true, nil
	}

	syncer.check(alarmNow)

	if !slices.Contains(fetched, "self") {
		t.Fatalf("expected self fetched, got %v", fetched)
	}
	if !slices.Contains(fetched, "live-target") {
		t.Fatalf("expected the live target fetched, got %v", fetched)
	}
	if slices.Contains(fetched, "cleared-target") {
		t.Fatalf("a normal notification must not trigger a fetch, got %v", fetched)
	}
	if slices.Contains(fetched, "quiet-target") {
		t.Fatalf("a context with no notifications must not trigger a fetch, got %v", fetched)
	}
	if len(fetched) != 2 {
		t.Fatalf("expected exactly self plus the live target fetched, got %v", fetched)
	}
}

// ── a 404 clears the bus alarm it backs ──────────────────────────────────

func TestNotificationSyncerA404RemovesTheContextsNotificationsAndItsBusAlarm(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.contexts["vessels.self"] = map[string]any{}
	snapshot.setSelfContext("vessels.self")

	snapshot.applyDelta(signalKDelta{
		Context: "vessels.jarricki",
		Updates: []signalKUpdate{{Values: []signalKValue{
			{Path: "navigation.position", Value: map[string]any{"latitude": 1.0, "longitude": 2.0}},
			{Path: "notifications.navigation.closestApproach", Value: map[string]any{"state": "warn", "message": "Closing"}},
		}}},
	}, alarmNow)

	if statuses := signalKCollisionNotifications(snapshot, alarmNow); len(statuses) != 1 {
		t.Fatalf("setup: expected the collision alarm live, got %+v", statuses)
	}

	syncer := newNotificationSyncer(snapshot)
	syncer.fetch = func(vesselID string) (map[string]any, bool, error) {
		if vesselID == "jarricki" {
			return nil, false, nil // 404: the server has forgotten this vessel's notifications
		}
		return map[string]any{}, true, nil
	}

	// A read that starts at the exact instant the leaf was last seen ties in
	// the leaf's favour (reconcileNotifications: "seen at or after
	// readStartedAt" keeps ours) -- correct for a delta racing a read, but not
	// what this test means to exercise, so the read starts a moment later.
	syncer.check(alarmNow.Add(time.Second))

	if statuses := signalKCollisionNotifications(snapshot, alarmNow); len(statuses) != 0 {
		t.Fatalf("expected the collision alarm gone after a 404, got %+v", statuses)
	}
}

// ── a fetch error stops the run cold ──────────────────────────────────────

func TestNotificationSyncerAFetchErrorStopsTheRunWithoutTouchingTheSnapshot(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.contexts["vessels.self"] = map[string]any{}
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{Values: []signalKValue{{
			Path:  "notifications.arrivalCircleEntered",
			Value: map[string]any{"state": "alarm", "message": "WP arrival circle entered!"},
		}}}},
	}, alarmNow)
	snapshot.setSelfContext("vessels.self")

	// A second, live-holding vessel context that must never be reached: self
	// sorts first, so a failure fetching self must stop the run before this
	// context's fetch is attempted at all.
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.jarricki",
		Updates: []signalKUpdate{{Values: []signalKValue{{
			Path:  "notifications.navigation.closestApproach",
			Value: map[string]any{"state": "warn", "message": "Closing"},
		}}}},
	}, alarmNow)

	syncer := newNotificationSyncer(snapshot)
	calls := 0
	syncer.fetch = func(vesselID string) (map[string]any, bool, error) {
		calls++
		return nil, false, errors.New("dial tcp: connection refused")
	}

	var logBuf bytes.Buffer
	originalOutput := log.Writer()
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(originalOutput) })

	syncer.check(alarmNow)
	if calls != 1 {
		t.Fatalf("expected the run to stop after the first (failing) context, got %d calls", calls)
	}
	if !syncer.failing {
		t.Fatalf("expected failing to be set after an error")
	}

	statuses := signalKNotifications(snapshot, ownsNothing)
	if len(statuses) != 1 || statuses[0].Label != "arrivalCircleEntered" {
		t.Fatalf("a fetch error must leave the snapshot untouched, got %+v", statuses)
	}

	// A second failing tick must not log a second time.
	syncer.invalidate()
	syncer.check(alarmNow.Add(time.Second))

	if got := strings.Count(logBuf.String(), "notification sync:"); got != 1 {
		t.Fatalf("expected exactly one failure log line across a sustained outage, got %d: %s", got, logBuf.String())
	}
}

// ── end to end: the corrected tree clears an alarm the watcher already raised, same tick ──

func TestNotificationSyncEndToEndClearsAnOpenBusAlarmOnTheSameTick(t *testing.T) {
	withTempAlarmRules(t)

	snapshot := newSignalKSnapshot()
	snapshot.contexts["vessels.self"] = map[string]any{}
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{Values: []signalKValue{{
			Path:  busTestNotificationPath, // "notifications.electrical.batteries.0.voltage.high"
			Value: busTestNotification(alarmStateAlarm, "High cell voltage"),
		}}}},
	}, alarmNow)
	snapshot.setSelfContext("vessels.self")
	withGlobalSnapshot(t, snapshot)

	originalWatcher := globalBusNotificationWatcher
	globalBusNotificationWatcher = newBusNotificationWatcher(snapshot)
	// busNotificationWatcher.check treats a zero dwell as unconfigured and
	// resets it to the 5s default, so this needs some positive value -- 1s,
	// the same short dwell alarm_bus_watch_test.go's own tests use -- and two
	// ticks spanning it, not one.
	globalBusNotificationWatcher.dwell = time.Second
	t.Cleanup(func() { globalBusNotificationWatcher = originalWatcher })

	originalEngine := globalAlarmEngine
	globalAlarmEngine = newAlarmEngine()
	t.Cleanup(func() { globalAlarmEngine = originalEngine })

	store := newTestAlarmLog(t)
	originalStore := globalAlarmLogStore
	globalAlarmLogStore = store
	t.Cleanup(func() { globalAlarmLogStore = originalStore })

	originalSyncer := globalNotificationSyncer
	globalNotificationSyncer = newNotificationSyncer(snapshot)
	t.Cleanup(func() { globalNotificationSyncer = originalSyncer })

	// withGlobalSnapshot already pointed the *previous* global syncer at a
	// no-network echo stub; replacing the syncer above lost it, and the sync
	// runs on every tick regardless of what else a test cares about, so the
	// first tick needs its own safe stub before it can call evaluateAlarmsOnce
	// at all -- echoing the snapshot's current self subtree back is a no-op
	// reconcile, same as withGlobalSnapshot's.
	globalNotificationSyncer.fetch = func(vesselID string) (map[string]any, bool, error) {
		notifications, ok := snapshot.treeFor("vessels.self")[notificationsRoot].(map[string]any)
		if !ok {
			return nil, false, nil
		}
		return notifications, true, nil
	}

	// First tick starts the dwell; the second, past it, raises and logs the
	// row -- the same two-tick shape every other watcher test in this package
	// uses (alarm_bus_watch_test.go).
	evaluateAlarmsOnce(alarmNow)
	evaluateAlarmsOnce(alarmNow.Add(2 * time.Second))

	ruleID := "notifications:electrical.batteries.0.voltage.high"
	if _, ok, err := store.OpenOccurrence(ruleID); err != nil || !ok {
		t.Fatalf("setup: expected an open log occurrence, ok=%v err=%v", ok, err)
	}
	found := false
	for _, alarm := range activeAlarms() {
		if alarm.RuleID == ruleID {
			found = true
		}
	}
	if !found {
		t.Fatalf("setup: expected the bus alarm active")
	}

	// The server now says it's normal.
	globalNotificationSyncer.fetch = func(vesselID string) (map[string]any, bool, error) {
		return map[string]any{"electrical": map[string]any{"batteries": map[string]any{"0": map[string]any{"voltage": map[string]any{"high": map[string]any{
			"value": map[string]any{"state": "normal", "message": "OK"},
		}}}}}}, true, nil
	}
	globalNotificationSyncer.invalidate()

	// Same-tick pipeline: sync corrects the snapshot, then the watcher (which
	// runs right after in evaluateAlarmsOnce) sees the correction and clears.
	evaluateAlarmsOnce(alarmNow.Add(3 * time.Second))

	entries, err := store.Recent(10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	closed := false
	for _, entry := range entries {
		if entry.RuleID == ruleID && entry.ClearedAt != nil {
			closed = true
		}
	}
	if !closed {
		t.Fatalf("expected the log row closed by the sync-triggered clear, got %+v", entries)
	}

	for _, alarm := range activeAlarms() {
		if alarm.RuleID == ruleID {
			t.Fatalf("expected the alarm gone from activeAlarms, still present: %+v", alarm)
		}
	}
}
