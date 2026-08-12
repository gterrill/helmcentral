package main

import (
	"testing"
)

func snapshotWithNotification(path string, value any) *signalKSnapshot {
	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{Values: []signalKValue{{Path: path, Value: value}}}},
	}, alarmNow)
	snapshot.setSelfContext("vessels.self")
	return snapshot
}

// The payoff of using SignalK's own vocabulary: an alarm raised by any other
// producer on the bus appears with no per-source integration at all.
func TestSignalKNotificationsSurfacesAnAlarmFromAnotherProducer(t *testing.T) {
	snapshot := snapshotWithNotification("notifications.electrical.batteries.house.voltage", map[string]any{
		"state":   "alarm",
		"message": "House bank critically low",
		"method":  []any{"visual", "sound"},
	})

	statuses := signalKNotifications(snapshot)
	if len(statuses) != 1 {
		t.Fatalf("expected 1 notification, got %d (%+v)", len(statuses), statuses)
	}
	if statuses[0].State != alarmStateAlarm {
		t.Fatalf("state: got %q, want %q", statuses[0].State, alarmStateAlarm)
	}
	if statuses[0].Message != "House bank critically low" {
		t.Fatalf("message: got %q", statuses[0].Message)
	}
	if statuses[0].Path != "notifications.electrical.batteries.house.voltage" {
		t.Fatalf("path: got %q", statuses[0].Path)
	}
}

func TestSignalKNotificationsIgnoresNormalState(t *testing.T) {
	snapshot := snapshotWithNotification("notifications.environment.depth.belowTransducer", map[string]any{
		"state":   "normal",
		"message": "Depth OK",
	})

	if statuses := signalKNotifications(snapshot); len(statuses) != 0 {
		t.Fatalf("normal is the cleared state and must not surface, got %+v", statuses)
	}
}

// SignalK clears a notification by writing null to the path.
func TestSignalKNotificationsIgnoresClearedNullValue(t *testing.T) {
	snapshot := snapshotWithNotification("notifications.mob", nil)

	if statuses := signalKNotifications(snapshot); len(statuses) != 0 {
		t.Fatalf("a null notification is cleared and must not surface, got %+v", statuses)
	}
}

func TestSignalKNotificationsIgnoresUnknownState(t *testing.T) {
	snapshot := snapshotWithNotification("notifications.something.odd", map[string]any{
		"state":   "spicy",
		"message": "not a real severity",
	})

	if statuses := signalKNotifications(snapshot); len(statuses) != 0 {
		t.Fatalf("an unrecognised state must not raise anything, got %+v", statuses)
	}
}

func TestSignalKNotificationsCollectsSeveralAndOrdersThem(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{Values: []signalKValue{
			{Path: "notifications.mob", Value: map[string]any{"state": "emergency", "message": "MOB"}},
			{Path: "notifications.environment.depth", Value: map[string]any{"state": "warn", "message": "Shallow"}},
		}}},
	}, alarmNow)
	snapshot.setSelfContext("vessels.self")

	statuses := signalKNotifications(snapshot)
	if len(statuses) != 2 {
		t.Fatalf("expected 2 notifications, got %d (%+v)", len(statuses), statuses)
	}
	if statuses[0].Path >= statuses[1].Path {
		t.Fatalf("expected path ordering for stable output, got %q then %q", statuses[0].Path, statuses[1].Path)
	}
}

// Inbound notification ids must not be able to collide with a locally
// configured rule id, or acknowledging one would silence the other.
func TestSignalKNotificationsNamespaceTheirRuleIDs(t *testing.T) {
	snapshot := snapshotWithNotification("notifications.mob", map[string]any{"state": "emergency"})

	statuses := signalKNotifications(snapshot)
	if len(statuses) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(statuses))
	}
	if statuses[0].RuleID != "notifications:mob" {
		t.Fatalf("rule id: got %q, want %q", statuses[0].RuleID, "notifications:mob")
	}
}

func TestSignalKNotificationsEmptyWhenNoneRaised(t *testing.T) {
	snapshot := snapshotWithSelfDelta("environment.depth.belowTransducer", 3.0, alarmNow)

	if statuses := signalKNotifications(snapshot); len(statuses) != 0 {
		t.Fatalf("expected no notifications, got %+v", statuses)
	}
}
