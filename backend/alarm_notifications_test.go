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

// ── the SignalK Notifications API is the acknowledgement mechanism ──────────
//
// SignalK 2.x manages alerts through /signalk/v2/api/notifications, keyed by
// the notification's own id, and draws a distinction Helmcentral's engine does
// not: silence removes "sound", acknowledge removes both "sound" and "visual".
// Each notification carries its own status and capability flags, so what an
// alarm supports is the server's answer, not a guess.

const liveNotificationID = "7f060fc2-15ef-49ef-9049-35e7ec282d1b"

func notificationWithStatus(state string, status map[string]any) map[string]any {
	return map[string]any{
		"state":   state,
		"message": "WP arrival circle entered!",
		"method":  []any{"sound", "visual"},
		"id":      liveNotificationID,
		"status":  status,
	}
}

func liveNotificationStatus() map[string]any {
	return map[string]any{
		"silenced": false, "acknowledged": false,
		"canSilence": true, "canAcknowledge": true, "canClear": false,
	}
}

func TestSignalKNotificationsSurfacesTheAPIStatusAndCapabilities(t *testing.T) {
	snapshot := snapshotWithNotification("notifications.arrivalCircleEntered",
		notificationWithStatus("alarm", liveNotificationStatus()))

	statuses := signalKNotifications(snapshot)
	if len(statuses) != 1 {
		t.Fatalf("expected 1 notification, got %d (%+v)", len(statuses), statuses)
	}
	got := statuses[0]
	if got.Phase != alarmPhaseActive || got.Silenced {
		t.Fatalf("an untouched alarm is active and unsilenced, got phase %q silenced %v", got.Phase, got.Silenced)
	}
	if !got.CanAcknowledge || !got.CanSilence {
		t.Fatalf("the server says both actions are available: can_ack %v can_silence %v", got.CanAcknowledge, got.CanSilence)
	}
}

func TestSignalKNotificationsReadsAcknowledgedFromTheAPIStatus(t *testing.T) {
	status := liveNotificationStatus()
	status["acknowledged"] = true
	snapshot := snapshotWithNotification("notifications.arrivalCircleEntered",
		notificationWithStatus("alarm", status))

	got := signalKNotifications(snapshot)[0]
	if got.Phase != alarmPhaseAcknowledged {
		t.Fatalf("phase: got %q, want %q", got.Phase, alarmPhaseAcknowledged)
	}
	if got.CanAcknowledge {
		t.Fatalf("an already-acknowledged alarm must not offer the action again")
	}
}

// Silencing is not acknowledging: the alarm stops sounding but is still
// demanding attention, so it stays active rather than dropping out of the
// banner the way an acknowledged one does.
func TestSignalKNotificationsKeepsASilencedAlarmActive(t *testing.T) {
	status := liveNotificationStatus()
	status["silenced"] = true
	snapshot := snapshotWithNotification("notifications.arrivalCircleEntered",
		notificationWithStatus("alarm", status))

	got := signalKNotifications(snapshot)[0]
	if !got.Silenced {
		t.Fatalf("expected silenced")
	}
	if got.Phase != alarmPhaseActive {
		t.Fatalf("silenced is not acknowledged: phase got %q, want %q", got.Phase, alarmPhaseActive)
	}
	if got.CanSilence {
		t.Fatalf("an already-silenced alarm must not offer the action again")
	}
	if !got.CanAcknowledge {
		t.Fatalf("a silenced alarm can still be acknowledged")
	}
}

// ADR 0038's rule outranks the server's flags: an emergency offers neither
// action however permissive canSilence/canAcknowledge are.
func TestSignalKNotificationsRefusesBothActionsForAnEmergency(t *testing.T) {
	snapshot := snapshotWithNotification("notifications.mob",
		notificationWithStatus("emergency", liveNotificationStatus()))

	got := signalKNotifications(snapshot)[0]
	if got.CanAcknowledge || got.CanSilence {
		t.Fatalf("an emergency cannot be silenced or acknowledged, got can_ack %v can_silence %v", got.CanAcknowledge, got.CanSilence)
	}
}

// A producer that writes the tree directly has no id, so there is no API call
// to make — no buttons are offered. Its method array still says whether it is
// sounding, and the API defines its actions in exactly those terms.
func TestSignalKNotificationsFallsBackToTheMethodArrayWithoutAStatusObject(t *testing.T) {
	cases := []struct {
		name         string
		method       []any
		wantPhase    alarmPhase
		wantSilenced bool
	}{
		{"sounding", []any{"sound", "visual"}, alarmPhaseActive, false},
		{"sound dropped is silenced", []any{"visual"}, alarmPhaseActive, true},
		{"both dropped is acknowledged", []any{}, alarmPhaseAcknowledged, true},
		{"absent method is not silence", nil, alarmPhaseActive, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value := map[string]any{"state": "alarm", "message": "raised directly"}
			if tc.method != nil {
				value["method"] = tc.method
			}
			got := signalKNotifications(snapshotWithNotification("notifications.bilge", value))[0]

			if got.Phase != tc.wantPhase {
				t.Fatalf("phase: got %q, want %q", got.Phase, tc.wantPhase)
			}
			if got.Silenced != tc.wantSilenced {
				t.Fatalf("silenced: got %v, want %v", got.Silenced, tc.wantSilenced)
			}
			if got.CanAcknowledge || got.CanSilence {
				t.Fatalf("no id means no API action to invoke, so no buttons")
			}
		})
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

// The regression behind the bug report, at the layer that actually works: the
// action is a POST to the notification's own id, not a write to its path.
func TestActOnSignalKNotificationPostsTheActionForTheNotificationID(t *testing.T) {
	for _, action := range []string{notificationActionAcknowledge, notificationActionSilence} {
		t.Run(action, func(t *testing.T) {
			snapshot := snapshotWithNotification("notifications.arrivalCircleEntered",
				notificationWithStatus("alarm", liveNotificationStatus()))

			var gotID, gotAction string
			post := func(id, action string) error {
				gotID, gotAction = id, action
				return nil
			}

			status, err := actOnSignalKNotification(snapshot, "arrivalCircleEntered", action, alarmNow, post)
			if err != nil {
				t.Fatalf("actOnSignalKNotification: %v", err)
			}
			if gotID != liveNotificationID {
				t.Fatalf("the action is keyed by the notification's own id, got %q", gotID)
			}
			if gotAction != action {
				t.Fatalf("action: got %q, want %q", gotAction, action)
			}
			if !status.Silenced {
				t.Fatalf("both actions stop the sound")
			}

			wantPhase := alarmPhaseActive
			if action == notificationActionAcknowledge {
				wantPhase = alarmPhaseAcknowledged
			}
			if status.Phase != wantPhase {
				t.Fatalf("phase after %s: got %q, want %q", action, status.Phase, wantPhase)
			}
		})
	}
}

func TestActOnSignalKNotificationRefusesAnEmergency(t *testing.T) {
	snapshot := snapshotWithNotification("notifications.mob",
		notificationWithStatus("emergency", liveNotificationStatus()))

	post := func(string, string) error {
		t.Fatalf("an emergency must never reach the server")
		return nil
	}

	if _, err := actOnSignalKNotification(snapshot, "mob", notificationActionAcknowledge, alarmNow, post); !errors.Is(err, errNotificationEmergency) {
		t.Fatalf("expected errNotificationEmergency, got %v", err)
	}
}

// The server's own answer is respected: an alarm it says cannot be silenced is
// not offered to it anyway on the chance it changes its mind.
func TestActOnSignalKNotificationRespectsTheServersCapabilityFlags(t *testing.T) {
	status := liveNotificationStatus()
	status["canSilence"] = false
	snapshot := snapshotWithNotification("notifications.arrivalCircleEntered",
		notificationWithStatus("alarm", status))

	post := func(string, string) error {
		t.Fatalf("the server said this alarm cannot be silenced")
		return nil
	}

	_, err := actOnSignalKNotification(snapshot, "arrivalCircleEntered", notificationActionSilence, alarmNow, post)
	if !errors.Is(err, errNotificationNotAllowed) {
		t.Fatalf("expected errNotificationNotAllowed, got %v", err)
	}
}

func TestActOnSignalKNotificationRefusesAPathThatIsNotLive(t *testing.T) {
	snapshot := snapshotWithNotification("notifications.arrivalCircleEntered", map[string]any{
		"state": "normal", "message": "cleared",
	})

	post := func(string, string) error {
		t.Fatalf("nothing is live at that path")
		return nil
	}

	if _, err := actOnSignalKNotification(snapshot, "arrivalCircleEntered", notificationActionAcknowledge, alarmNow, post); !errors.Is(err, errNotificationNotLive) {
		t.Fatalf("expected errNotificationNotLive, got %v", err)
	}
	if _, err := actOnSignalKNotification(snapshot, "nothing.here", notificationActionAcknowledge, alarmNow, post); !errors.Is(err, errNotificationNotLive) {
		t.Fatalf("expected errNotificationNotLive for an unknown path, got %v", err)
	}
}

// A failed call must surface, not be swallowed into a success the operator
// reads as "silenced" while the klaxon keeps sounding.
func TestActOnSignalKNotificationSurfacesAServerFailure(t *testing.T) {
	snapshot := snapshotWithNotification("notifications.arrivalCircleEntered",
		notificationWithStatus("alarm", liveNotificationStatus()))

	post := func(string, string) error { return errors.New("signalk returned status 404: Resource not found.") }

	_, err := actOnSignalKNotification(snapshot, "arrivalCircleEntered", notificationActionAcknowledge, alarmNow, post)
	if err == nil {
		t.Fatalf("expected the server failure to surface")
	}
	if errors.Is(err, errNotificationNotLive) || errors.Is(err, errNotificationEmergency) || errors.Is(err, errNotificationNotAllowed) {
		t.Fatalf("a server failure must not be reported as a refusal: %v", err)
	}
}

// Acknowledging is strictly stronger than silencing — it removes the visual
// alert too — so an acknowledged alarm must not still offer a Silence button.
// SignalK's own canSilence stays true after an acknowledge, and its silenced
// flag stays false even though the method array is empty, so this cannot be
// taken from the server's flags alone.
func TestSignalKNotificationsOffersNoSilenceOnceAcknowledged(t *testing.T) {
	status := liveNotificationStatus()
	status["acknowledged"] = true
	value := notificationWithStatus("alarm", status)
	value["method"] = []any{}

	got := signalKNotifications(snapshotWithNotification("notifications.arrivalCircleEntered", value))[0]

	if got.Phase != alarmPhaseAcknowledged {
		t.Fatalf("phase: got %q, want %q", got.Phase, alarmPhaseAcknowledged)
	}
	if got.CanSilence {
		t.Fatalf("an acknowledged alarm has already stopped sounding; offering Silence is a button that does nothing")
	}
	if !got.Silenced {
		t.Fatalf("an acknowledged alarm is silenced by definition, whatever the server's silenced flag says")
	}
}
