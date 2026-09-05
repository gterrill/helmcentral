package main

import (
	"encoding/json"
	"errors"
	"testing"
)

// snapshotFromSelfTreeJSON builds a local snapshot from a REST-shaped JSON
// body, the way seedSelfTree does for the global one (signalk_payload_test.go)
// -- used here because applyDelta's flat path/value shape has nowhere to carry
// "meta", and a unit lookup needs a real meta.units node sitting alongside a
// notification in the same tree.
func snapshotFromSelfTreeJSON(t *testing.T, body string) *signalKSnapshot {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("seeding self tree: %v", err)
	}

	snapshot := newSignalKSnapshot()
	snapshot.contexts["vessels.self"] = payload
	snapshot.setSelfContext("vessels.self")
	return snapshot
}

func snapshotWithNotification(path string, value any) *signalKSnapshot {
	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{Values: []signalKValue{{Path: path, Value: value}}}},
	}, alarmNow)
	snapshot.setSelfContext("vessels.self")
	return snapshot
}

// ownsNothing is the ownership predicate for tests that are not exercising
// the ownership guard itself: every notification passes through untouched.
func ownsNothing(string) bool { return false }

// The payoff of using SignalK's own vocabulary: an alarm raised by any other
// producer on the bus appears with no per-source integration at all.
func TestSignalKNotificationsSurfacesAnAlarmFromAnotherProducer(t *testing.T) {
	snapshot := snapshotWithNotification("notifications.electrical.batteries.house.voltage", map[string]any{
		"state":   "alarm",
		"message": "House bank critically low",
		"method":  []any{"visual", "sound"},
	})

	statuses := signalKNotifications(snapshot, ownsNothing)
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

	if statuses := signalKNotifications(snapshot, ownsNothing); len(statuses) != 0 {
		t.Fatalf("normal is the cleared state and must not surface, got %+v", statuses)
	}
}

// SignalK clears a notification by writing null to the path.
func TestSignalKNotificationsIgnoresClearedNullValue(t *testing.T) {
	snapshot := snapshotWithNotification("notifications.mob", nil)

	if statuses := signalKNotifications(snapshot, ownsNothing); len(statuses) != 0 {
		t.Fatalf("a null notification is cleared and must not surface, got %+v", statuses)
	}
}

func TestSignalKNotificationsIgnoresUnknownState(t *testing.T) {
	snapshot := snapshotWithNotification("notifications.something.odd", map[string]any{
		"state":   "spicy",
		"message": "not a real severity",
	})

	if statuses := signalKNotifications(snapshot, ownsNothing); len(statuses) != 0 {
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

	statuses := signalKNotifications(snapshot, ownsNothing)
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

	statuses := signalKNotifications(snapshot, ownsNothing)
	if len(statuses) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(statuses))
	}
	if statuses[0].RuleID != "notifications:mob" {
		t.Fatalf("rule id: got %q, want %q", statuses[0].RuleID, "notifications:mob")
	}
}

func TestSignalKNotificationsEmptyWhenNoneRaised(t *testing.T) {
	snapshot := snapshotWithSelfDelta("environment.depth.belowTransducer", 3.0, alarmNow)

	if statuses := signalKNotifications(snapshot, ownsNothing); len(statuses) != 0 {
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

	statuses := signalKNotifications(snapshot, ownsNothing)
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

	got := signalKNotifications(snapshot, ownsNothing)[0]
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

	got := signalKNotifications(snapshot, ownsNothing)[0]
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

	got := signalKNotifications(snapshot, ownsNothing)[0]
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
			got := signalKNotifications(snapshotWithNotification("notifications.bilge", value), ownsNothing)[0]

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

	got := signalKNotifications(snapshotWithNotification("notifications.arrivalCircleEntered", value), ownsNothing)[0]

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

// ── ownership: Helmcentral's own echoes are never shown as bus alarms ──────
//
// Helmcentral publishes its own rule alarms onto notifications.<path>
// (signalk_publish.go). Reading that tree back without knowing which paths
// are its own turns every one of those echoes into a phantom "bus" alarm --
// including one left behind by a different Helmcentral instance on the same
// boat, which this engine holds no rule for at all. The fixture values below
// are the real shapes captured off the boat, not assumed ones.
func TestSignalKNotificationsSkipsPathsHelmcentralOwns(t *testing.T) {
	withTempAlarmRules(t)
	if _, err := createAlarmRule(validRule()); err != nil { // enabled, path electrical.batteries.house.voltage
		t.Fatalf("createAlarmRule: %v", err)
	}

	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{Values: []signalKValue{
			// The ghost: another Helmcentral instance's echo, with no rule for
			// it on this engine at all -- ownership here comes only from the
			// derived-path namespace.
			{Path: "notifications.helmcentral.environment.pressureRate", Value: map[string]any{
				"state":   "warn",
				"message": "Barometer falling: below -0.027777777777777776 (-0.0283)",
				"method":  []any{},
				"id":      "89f3b708-602d-4882-8044-182e6e24a134",
				"status": map[string]any{
					"silenced": false, "acknowledged": true,
					"canSilence": true, "canAcknowledge": true, "canClear": false,
				},
			}},
			// Owned via the enabled rule created above.
			{Path: "notifications.electrical.batteries.house.voltage", Value: map[string]any{
				"state":   "alarm",
				"message": "House bank critically low",
				"method":  []any{"visual", "sound"},
			}},
			// Foreign: no rule, no derived-path prefix.
			{Path: "notifications.radar.fur6424A.guardZone.1", Value: map[string]any{
				"state":   "alert",
				"message": "Radar fur6424A guard zone 1: target 100000294 acquired",
				"method":  []any{},
				"id":      "7b454514-4b75-4658-89db-2b98a4ef17e8",
				"status": map[string]any{
					"silenced": false, "acknowledged": true,
					"canSilence": true, "canAcknowledge": true, "canClear": false,
				},
			}},
		}}},
	}, alarmNow)
	snapshot.setSelfContext("vessels.self")

	statuses := signalKNotifications(snapshot, helmcentralOwnershipPredicate())
	if len(statuses) != 1 {
		t.Fatalf("expected exactly the foreign radar notification, got %d: %+v", len(statuses), statuses)
	}
	if statuses[0].Path != "notifications.radar.fur6424A.guardZone.1" {
		t.Fatalf("path: got %q, want the radar guard zone", statuses[0].Path)
	}
}

// ── unit: bus notifications report the unit of the path they are ABOUT ─────
//
// A bus notification's own node under notifications.* carries no unit --
// that is metadata on the data path itself. The bare path a notification
// reports against is its Label, so the unit lookup has to be keyed off that,
// not off Path (which is notifications.<label>).

func TestSignalKNotificationsSetsUnitFromTheDataPathMeta(t *testing.T) {
	snapshot := snapshotFromSelfTreeJSON(t, `{
		"notifications": {"electrical": {"batteries": {"house": {"voltage": {"value": {
			"state": "alarm", "message": "High voltage", "method": ["visual", "sound"]
		}}}}}},
		"electrical": {"batteries": {"house": {"voltage": {"value": 14.9, "meta": {"units": "V"}}}}}
	}`)

	statuses := signalKNotifications(snapshot, ownsNothing)
	if len(statuses) != 1 {
		t.Fatalf("expected 1 notification, got %d (%+v)", len(statuses), statuses)
	}
	if statuses[0].Unit != "V" {
		t.Fatalf("unit: got %q, want %q", statuses[0].Unit, "V")
	}
}

// The radar guard zone case named in the spec: a notification path with
// nothing behind it in the data tree, so there is no meta to read at all.
func TestSignalKNotificationsOmitsUnitWhenDataPathHasNoMeta(t *testing.T) {
	snapshot := snapshotFromSelfTreeJSON(t, `{
		"notifications": {"radar": {"fur6424A": {"guardZone": {"1": {"value": {
			"state": "alert", "message": "Radar fur6424A guard zone 1: target acquired", "method": []
		}}}}}}
	}`)

	statuses := signalKNotifications(snapshot, ownsNothing)
	if len(statuses) != 1 {
		t.Fatalf("expected 1 notification, got %d (%+v)", len(statuses), statuses)
	}
	if statuses[0].Unit != "" {
		t.Fatalf("expected no unit for a path with no data-tree meta, got %q", statuses[0].Unit)
	}

	encoded, err := json.Marshal(statuses[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := payload["unit"]; present {
		t.Fatalf("unknown unit must be absent from the JSON, not an empty string: %s", encoded)
	}
}

// A bus notification is never a rule alarm, so none of the rule-only fields
// belong on it -- op, threshold, hysteresis and clear_value all describe a
// rule this engine holds, and a bus notification is read straight off
// someone else's tree with no rule behind it at all.
func TestSignalKNotificationsOmitRuleOnlyJSONFields(t *testing.T) {
	snapshot := snapshotWithNotification("notifications.electrical.batteries.house.voltage", map[string]any{
		"state": "alarm", "message": "House bank critically low", "method": []any{"visual", "sound"},
	})

	statuses := signalKNotifications(snapshot, ownsNothing)
	if len(statuses) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(statuses))
	}

	encoded, err := json.Marshal(statuses[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"op", "threshold", "hysteresis", "clear_value"} {
		if _, present := payload[key]; present {
			t.Fatalf("%s must be absent from a bus notification, got %v in %s", key, payload[key], encoded)
		}
	}
}

// A disabled rule's path is not owned: disabling a rule does not retract
// anything already on the bus, and some other producer may legitimately be
// raising a notification at the same path this engine no longer watches.
func TestSignalKNotificationsDoesNotOwnAPathOfADisabledRule(t *testing.T) {
	withTempAlarmRules(t)

	rule := validRule()
	rule.Enabled = false
	if _, err := createAlarmRule(rule); err != nil {
		t.Fatalf("createAlarmRule: %v", err)
	}

	snapshot := snapshotWithNotification("notifications.electrical.batteries.house.voltage", map[string]any{
		"state":   "alarm",
		"message": "House bank critically low",
		"method":  []any{"visual", "sound"},
	})

	statuses := signalKNotifications(snapshot, helmcentralOwnershipPredicate())
	if len(statuses) != 1 {
		t.Fatalf("a disabled rule's path is not owned, so the bus notification must still show, got %d: %+v", len(statuses), statuses)
	}
}
