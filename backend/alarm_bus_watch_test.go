package main

import (
	"testing"
	"time"
)

const busTestNotificationPath = "notifications.electrical.batteries.0.voltage.high"

func busTestNotification(state, message string) map[string]any {
	return map[string]any{"state": state, "message": message}
}

// Real-world symptom this whole file exists to fix: a Victron "High cell
// voltage" alarm raised and cleared overnight with no push, no log row, and
// once Victron cleared it, no trace it had ever happened.

// ADR 0038 §3: undebounced alarms are why people switch marine alarm systems
// off. A notification must hold for the dwell before Helmcentral raises it.
func TestBusNotificationWatcherDwellsBeforeRaising(t *testing.T) {
	snapshot := snapshotWithNotification(busTestNotificationPath, busTestNotification(alarmStateAlarm, "High cell voltage"))
	watcher := newBusNotificationWatcher(snapshot)
	watcher.dwell = 5 * time.Second

	if events := watcher.check(alarmNow); len(events) != 0 {
		t.Fatalf("must not raise on first sight, got %+v", events)
	}
	if events := watcher.check(alarmNow.Add(2 * time.Second)); len(events) != 0 {
		t.Fatalf("must not raise before the dwell elapses, got %+v", events)
	}

	events := watcher.check(alarmNow.Add(6 * time.Second))
	if len(events) != 1 || events[0].Kind != alarmEventRaised {
		t.Fatalf("expected a single raise once the dwell elapses, got %+v", events)
	}
	if events[0].Source != alarmSourceSignalK {
		t.Fatalf("source: got %q, want %q", events[0].Source, alarmSourceSignalK)
	}
	if events[0].Rule.ID != "notifications:electrical.batteries.0.voltage.high" {
		t.Fatalf("rule id: got %q", events[0].Rule.ID)
	}
}

// Matches the rule engine's own dwell semantics (alarm_engine.go): recovery
// before the dwell elapsed restarts the timer rather than accumulating it.
func TestBusNotificationWatcherRecoveryBeforeDwellEmitsNothingAndRestartsTheTimer(t *testing.T) {
	live := snapshotWithNotification(busTestNotificationPath, busTestNotification(alarmStateAlarm, "High cell voltage"))
	watcher := newBusNotificationWatcher(live)
	watcher.dwell = 5 * time.Second

	if events := watcher.check(alarmNow); len(events) != 0 {
		t.Fatalf("must not raise on first sight, got %+v", events)
	}

	// It clears before the dwell elapses.
	clear := newSignalKSnapshot()
	clear.setSelfContext("vessels.self")
	watcher.snapshot = clear
	if events := watcher.check(alarmNow.Add(2 * time.Second)); len(events) != 0 {
		t.Fatalf("recovery before the dwell elapsed must emit nothing, got %+v", events)
	}

	// It comes back; this sighting becomes the new pendingSince.
	watcher.snapshot = live
	if events := watcher.check(alarmNow.Add(2 * time.Second)); len(events) != 0 {
		t.Fatalf("must not raise on the sighting itself, got %+v", events)
	}
	// Short of a full dwell from the new sighting, it must still not raise -
	// if the timer had not restarted this would already be past the original
	// 5s mark.
	if events := watcher.check(alarmNow.Add(2*time.Second + 3*time.Second)); len(events) != 0 {
		t.Fatalf("the dwell must have restarted from the new sighting, got %+v", events)
	}

	events := watcher.check(alarmNow.Add(2*time.Second + 5*time.Second))
	if len(events) != 1 || events[0].Kind != alarmEventRaised {
		t.Fatalf("expected a raise once the restarted dwell elapses, got %+v", events)
	}
}

// Edge-triggered like the rule engine and the watchdog: a persistently live
// notification produces exactly one raise, not one per tick.
func TestBusNotificationWatcherRaisesOnlyOnceWhileNotificationStaysLive(t *testing.T) {
	snapshot := snapshotWithNotification(busTestNotificationPath, busTestNotification(alarmStateAlarm, "High cell voltage"))
	watcher := newBusNotificationWatcher(snapshot)
	watcher.dwell = 5 * time.Second

	raises := 0
	for i := 0; i < 50; i++ {
		for _, event := range watcher.check(alarmNow.Add(time.Duration(i) * time.Second)) {
			if event.Kind == alarmEventRaised {
				raises++
			}
		}
	}
	if raises != 1 {
		t.Fatalf("expected exactly 1 raise across a sustained notification, got %d", raises)
	}
}

// Once Victron cleared its alarm overnight it vanished from the live list with
// no trace; a raised notification disappearing from the tree must clear.
func TestBusNotificationWatcherClearsWhenNotificationDisappears(t *testing.T) {
	live := snapshotWithNotification(busTestNotificationPath, busTestNotification(alarmStateAlarm, "High cell voltage"))
	watcher := newBusNotificationWatcher(live)
	watcher.dwell = 5 * time.Second

	watcher.check(alarmNow)
	if events := watcher.check(alarmNow.Add(6 * time.Second)); len(events) != 1 || events[0].Kind != alarmEventRaised {
		t.Fatalf("setup: expected a raise, got %+v", events)
	}

	gone := newSignalKSnapshot()
	gone.setSelfContext("vessels.self")
	watcher.snapshot = gone

	events := watcher.check(alarmNow.Add(7 * time.Second))
	if len(events) != 1 || events[0].Kind != alarmEventCleared {
		t.Fatalf("expected a clear once the notification disappears, got %+v", events)
	}
	if events[0].Rule.ID != "notifications:electrical.batteries.0.voltage.high" {
		t.Fatalf("cleared rule id: got %q", events[0].Rule.ID)
	}
}

// signalk-server keeps a cleared key and normalises its state to "normal"
// rather than deleting it; that must clear exactly like disappearing.
func TestBusNotificationWatcherClearsWhenStateGoesNormal(t *testing.T) {
	snapshot := snapshotWithNotification(busTestNotificationPath, busTestNotification(alarmStateAlarm, "High cell voltage"))
	watcher := newBusNotificationWatcher(snapshot)
	watcher.dwell = 5 * time.Second

	watcher.check(alarmNow)
	if events := watcher.check(alarmNow.Add(6 * time.Second)); len(events) != 1 || events[0].Kind != alarmEventRaised {
		t.Fatalf("setup: expected a raise, got %+v", events)
	}

	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{Values: []signalKValue{{Path: busTestNotificationPath, Value: busTestNotification(alarmStateNormal, "OK")}}}},
	}, alarmNow.Add(7*time.Second))

	events := watcher.check(alarmNow.Add(7 * time.Second))
	if len(events) != 1 || events[0].Kind != alarmEventCleared {
		t.Fatalf("expected a clear once the notification returns to normal, got %+v", events)
	}
}

// Severity worsening on an already-raised notification must be dispatched even
// though it does not open a new log row.
func TestBusNotificationWatcherEscalatesOnWorseningSeverity(t *testing.T) {
	snapshot := snapshotWithNotification(busTestNotificationPath, busTestNotification(alarmStateWarn, "Elevated cell voltage"))
	watcher := newBusNotificationWatcher(snapshot)
	watcher.dwell = 5 * time.Second

	watcher.check(alarmNow)
	if events := watcher.check(alarmNow.Add(6 * time.Second)); len(events) != 1 || events[0].Kind != alarmEventRaised {
		t.Fatalf("setup: expected a raise, got %+v", events)
	}

	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{Values: []signalKValue{{Path: busTestNotificationPath, Value: busTestNotification(alarmStateAlarm, "High cell voltage")}}}},
	}, alarmNow.Add(7*time.Second))

	events := watcher.check(alarmNow.Add(7 * time.Second))
	if len(events) != 1 || events[0].Kind != alarmEventEscalated {
		t.Fatalf("expected an escalation when severity worsens, got %+v", events)
	}
	if events[0].Status.State != alarmStateAlarm {
		t.Fatalf("escalated state: got %q, want %q", events[0].Status.State, alarmStateAlarm)
	}
}

// Severity easing without clearing (alarm -> warn, still live) is not an
// escalation and must not re-raise or emit anything.
func TestBusNotificationWatcherEmitsNothingWhenSeverityEasesWithoutClearing(t *testing.T) {
	snapshot := snapshotWithNotification(busTestNotificationPath, busTestNotification(alarmStateAlarm, "High cell voltage"))
	watcher := newBusNotificationWatcher(snapshot)
	watcher.dwell = 5 * time.Second

	watcher.check(alarmNow)
	watcher.check(alarmNow.Add(6 * time.Second))

	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{Values: []signalKValue{{Path: busTestNotificationPath, Value: busTestNotification(alarmStateWarn, "Elevated cell voltage")}}}},
	}, alarmNow.Add(7*time.Second))

	if events := watcher.check(alarmNow.Add(7 * time.Second)); len(events) != 0 {
		t.Fatalf("easing severity without clearing must emit nothing, got %+v", events)
	}
}

// ── echo suppression ─────────────────────────────────────────────────────────
//
// Commit 7d8ffb3 made Helmcentral publish its own alarms onto notifications.*
// (signalk_publish.go). Without a guard: rule fires -> published -> read back
// here -> re-raised as a "bus" alarm -> duplicate log row and duplicate push
// for every one of Helmcentral's own alarms.

func TestBusNotificationWatcherIgnoresAPathALocalRulePublishes(t *testing.T) {
	withTempAlarmRules(t)

	rule := validRule()
	rule.Path = "electrical.batteries.house.voltage"
	if _, err := createAlarmRule(rule); err != nil {
		t.Fatalf("createAlarmRule: %v", err)
	}

	// Read back exactly as signalKNotifyTransport.Send wrote it.
	snapshot := snapshotWithNotification("notifications."+rule.Path, busTestNotification(alarmStateAlarm, "House bank low"))
	watcher := newBusNotificationWatcher(snapshot)
	watcher.dwell = time.Second

	// Two ticks spanning the dwell. One would prove nothing: check never
	// raises on a notification's first sighting, because that sighting is what
	// starts the dwell, so a single call passes whether or not the guard is
	// there at all.
	watcher.check(alarmNow)
	if events := watcher.check(alarmNow.Add(time.Second)); len(events) != 0 {
		t.Fatalf("must not re-raise a path Helmcentral itself publishes, got %+v", events)
	}
}

func TestBusNotificationWatcherIgnoresTheWatchdogsOwnPath(t *testing.T) {
	snapshot := snapshotWithNotification(watchdogPath, busTestNotification(alarmStateAlarm, "SignalK stream is not connected"))
	watcher := newBusNotificationWatcher(snapshot)
	watcher.dwell = time.Second

	watcher.check(alarmNow)
	if events := watcher.check(alarmNow.Add(time.Second)); len(events) != 0 {
		t.Fatalf("must not re-raise the watchdog's own published path, got %+v", events)
	}
}

// A genuinely third-party notification at a different path is unaffected by
// the ownership guard.
func TestBusNotificationWatcherStillRaisesAnUnrelatedPath(t *testing.T) {
	withTempAlarmRules(t)

	rule := validRule()
	rule.Path = "electrical.batteries.house.voltage"
	if _, err := createAlarmRule(rule); err != nil {
		t.Fatalf("createAlarmRule: %v", err)
	}

	snapshot := snapshotWithNotification(busTestNotificationPath, busTestNotification(alarmStateAlarm, "High cell voltage"))
	watcher := newBusNotificationWatcher(snapshot)
	watcher.dwell = time.Second

	watcher.check(alarmNow)
	events := watcher.check(alarmNow.Add(time.Second))
	if len(events) != 1 || events[0].Kind != alarmEventRaised {
		t.Fatalf("a path no local rule owns must still raise, got %+v", events)
	}
}
