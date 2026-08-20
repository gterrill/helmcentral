package main

import (
	"errors"
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

// ── acknowledgement round-trips through the bus (ADR 0038) ──────────────────
//
// A bus-sourced notification is recomputed from the SignalK tree on every
// poll, so it has no local state an acknowledgement could live in. SignalK has
// no "acknowledged" state either — silencing is expressed by dropping "sound"
// from the notification's method array, so that is both what an ack writes and
// how one is read back.

func TestSignalKNotificationsReportsASilencedNotificationAsAcknowledged(t *testing.T) {
	snapshot := snapshotWithNotification("notifications.arrivalCircleEntered", map[string]any{
		"state":   "alarm",
		"message": "WP arrival circle entered!",
		"method":  []any{"visual"},
	})

	statuses := signalKNotifications(snapshot)
	if len(statuses) != 1 {
		t.Fatalf("expected 1 notification, got %d (%+v)", len(statuses), statuses)
	}
	if statuses[0].Phase != alarmPhaseAcknowledged {
		t.Fatalf("a notification with sound dropped is silenced: phase got %q, want %q", statuses[0].Phase, alarmPhaseAcknowledged)
	}
	if statuses[0].State != alarmStateAlarm {
		t.Fatalf("silencing must not change the severity: state got %q", statuses[0].State)
	}
}

func TestSignalKNotificationsReportsASoundingNotificationAsActive(t *testing.T) {
	snapshot := snapshotWithNotification("notifications.arrivalCircleEntered", map[string]any{
		"state":   "alarm",
		"message": "WP arrival circle entered!",
		"method":  []any{"visual", "sound"},
	})

	statuses := signalKNotifications(snapshot)
	if len(statuses) != 1 {
		t.Fatalf("expected 1 notification, got %d (%+v)", len(statuses), statuses)
	}
	if statuses[0].Phase != alarmPhaseActive {
		t.Fatalf("phase: got %q, want %q", statuses[0].Phase, alarmPhaseActive)
	}
}

// An absent method array is a producer that never said how to alert, not a
// producer asking for silence. Reading it as acknowledged would silently
// swallow every alarm from such a producer.
func TestSignalKNotificationsTreatsAnAbsentMethodAsActive(t *testing.T) {
	snapshot := snapshotWithNotification("notifications.arrivalCircleEntered", map[string]any{
		"state":   "alarm",
		"message": "WP arrival circle entered!",
	})

	statuses := signalKNotifications(snapshot)
	if len(statuses) != 1 {
		t.Fatalf("expected 1 notification, got %d (%+v)", len(statuses), statuses)
	}
	if statuses[0].Phase != alarmPhaseActive {
		t.Fatalf("phase: got %q, want %q", statuses[0].Phase, alarmPhaseActive)
	}
}

func TestNotificationRuleIDPathRecognisesBusSourcedIDs(t *testing.T) {
	path, ok := notificationRuleIDPath("notifications:navigation.arrivalCircleEntered")
	if !ok {
		t.Fatalf("expected a namespaced notification id to be recognised as bus-sourced")
	}
	if path != "navigation.arrivalCircleEntered" {
		t.Fatalf("path: got %q", path)
	}

	if _, ok := notificationRuleIDPath("a3f1-locally-configured-rule"); ok {
		t.Fatalf("a local rule id must never be treated as bus-sourced")
	}
}

// The regression behind the bug report: acknowledging a notification raised by
// another producer writes it back to the bus with sound dropped, preserving
// everything else the producer published.
func TestAcknowledgeSignalKNotificationWritesBackWithoutSound(t *testing.T) {
	snapshot := snapshotWithNotification("notifications.arrivalCircleEntered", map[string]any{
		"state":   "alarm",
		"message": "WP arrival circle entered!",
		"method":  []any{"visual", "sound"},
		"id":      "producer-private-field",
	})

	var gotPath string
	var gotValue any
	put := func(path string, value any) error {
		gotPath, gotValue = path, value
		return nil
	}

	status, err := acknowledgeSignalKNotification(snapshot, "arrivalCircleEntered", alarmNow, put)
	if err != nil {
		t.Fatalf("acknowledgeSignalKNotification: %v", err)
	}

	if gotPath != "notifications.arrivalCircleEntered" {
		t.Fatalf("write path: got %q", gotPath)
	}
	value, ok := gotValue.(map[string]any)
	if !ok {
		t.Fatalf("expected a notification object, got %T", gotValue)
	}
	if got := methodStrings(t, value["method"]); len(got) != 1 || got[0] != "visual" {
		t.Fatalf("method: got %v, want [visual]", got)
	}
	if value["state"] != "alarm" {
		t.Fatalf("acknowledging must not change the severity: state got %v", value["state"])
	}
	if value["message"] != "WP arrival circle entered!" {
		t.Fatalf("message must survive the round trip: got %v", value["message"])
	}
	if value["id"] != "producer-private-field" {
		t.Fatalf("a PUT replaces the whole object, so producer fields must be preserved: got %v", value["id"])
	}

	if status.Phase != alarmPhaseAcknowledged {
		t.Fatalf("returned phase: got %q, want %q", status.Phase, alarmPhaseAcknowledged)
	}
	if !status.AckedAt.Equal(alarmNow) {
		t.Fatalf("returned acked_at: got %v, want %v", status.AckedAt, alarmNow)
	}
}

func TestAcknowledgeSignalKNotificationDefaultsToVisualWhenMethodIsAbsent(t *testing.T) {
	snapshot := snapshotWithNotification("notifications.arrivalCircleEntered", map[string]any{
		"state":   "alarm",
		"message": "WP arrival circle entered!",
	})

	var gotValue any
	put := func(_ string, value any) error {
		gotValue = value
		return nil
	}

	if _, err := acknowledgeSignalKNotification(snapshot, "arrivalCircleEntered", alarmNow, put); err != nil {
		t.Fatalf("acknowledgeSignalKNotification: %v", err)
	}

	value := gotValue.(map[string]any)
	if got := methodStrings(t, value["method"]); len(got) != 1 || got[0] != "visual" {
		t.Fatalf("method: got %v, want [visual]", got)
	}
}

// The one spec rule that outranks the operator: an emergency cannot be
// silenced, so nothing may be written to the bus at all.
func TestAcknowledgeSignalKNotificationRefusesAnEmergency(t *testing.T) {
	snapshot := snapshotWithNotification("notifications.mob", map[string]any{
		"state":   "emergency",
		"message": "MOB",
		"method":  []any{"visual", "sound"},
	})

	put := func(string, any) error {
		t.Fatalf("an emergency must never be written back silenced")
		return nil
	}

	_, err := acknowledgeSignalKNotification(snapshot, "mob", alarmNow, put)
	if !errors.Is(err, errNotificationEmergency) {
		t.Fatalf("expected errNotificationEmergency, got %v", err)
	}
}

func TestAcknowledgeSignalKNotificationRefusesAPathThatIsNotLive(t *testing.T) {
	snapshot := snapshotWithNotification("notifications.arrivalCircleEntered", map[string]any{
		"state":   "normal",
		"message": "cleared",
	})

	put := func(string, any) error {
		t.Fatalf("nothing is live at that path, so nothing may be written")
		return nil
	}

	if _, err := acknowledgeSignalKNotification(snapshot, "arrivalCircleEntered", alarmNow, put); !errors.Is(err, errNotificationNotLive) {
		t.Fatalf("expected errNotificationNotLive, got %v", err)
	}
	if _, err := acknowledgeSignalKNotification(snapshot, "nothing.here", alarmNow, put); !errors.Is(err, errNotificationNotLive) {
		t.Fatalf("expected errNotificationNotLive for an unknown path, got %v", err)
	}
}

// A failed write must surface, not be swallowed into a success the operator
// would read as "silenced" while the buzzer keeps sounding.
func TestAcknowledgeSignalKNotificationSurfacesAWriteFailure(t *testing.T) {
	snapshot := snapshotWithNotification("notifications.arrivalCircleEntered", map[string]any{
		"state":  "alarm",
		"method": []any{"visual", "sound"},
	})

	put := func(string, any) error { return errors.New("signalk returned status 405: no PUT handler") }

	_, err := acknowledgeSignalKNotification(snapshot, "arrivalCircleEntered", alarmNow, put)
	if err == nil {
		t.Fatalf("expected the upstream write failure to surface")
	}
	if errors.Is(err, errNotificationNotLive) || errors.Is(err, errNotificationEmergency) {
		t.Fatalf("an upstream write failure must not be reported as a refusal: %v", err)
	}
}

func methodStrings(t *testing.T, raw any) []string {
	t.Helper()
	switch methods := raw.(type) {
	case []string:
		return methods
	case []any:
		out := make([]string, 0, len(methods))
		for _, m := range methods {
			out = append(out, m.(string))
		}
		return out
	default:
		t.Fatalf("method: expected an array, got %T (%v)", raw, raw)
		return nil
	}
}
