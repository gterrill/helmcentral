package main

import (
	"testing"
	"time"
)

// collisionNotification mirrors what signalk-ais-target-prioritizer actually
// writes, captured from the live server (ADR 0057).
func collisionNotification(state, message string) map[string]any {
	return map[string]any{
		"state":   state,
		"method":  []any{"visual", "sound"},
		"message": message,
		"id":      "83c4bc56-f75e-4138-88f6-83a8bfeae043",
		"status": map[string]any{
			"silenced":       false,
			"acknowledged":   false,
			"canSilence":     true,
			"canAcknowledge": true,
			"canClear":       false,
		},
	}
}

func snapshotWithTargets(targets map[string]any) *signalKSnapshot {
	snapshot := newSignalKSnapshot()
	for context, value := range targets {
		snapshot.applyDelta(signalKDelta{
			Context: context,
			Updates: []signalKUpdate{{Values: []signalKValue{{
				Path:  "notifications.navigation.closestApproach",
				Value: value,
			}}}},
		}, alarmNow)
	}
	snapshot.setSelfContext("vessels.self")
	return snapshot
}

// The gap ADR 0057 exists to close: signalKNotifications reads the self tree,
// and every one of these alarms is raised on the target's own context.
func TestCollisionNotificationsSurfaceFromATargetContext(t *testing.T) {
	snapshot := snapshotWithTargets(map[string]any{
		"vessels.urn:mrn:imo:mmsi:503016440": collisionNotification("warn", "TASHTEGO - CPA WARNING"),
	})

	if selfOnly := signalKNotifications(snapshot); len(selfOnly) != 0 {
		t.Fatalf("the self walk must not see target notifications, got %+v", selfOnly)
	}

	statuses := signalKCollisionNotifications(snapshot)
	if len(statuses) != 1 {
		t.Fatalf("expected 1 collision notification, got %d (%+v)", len(statuses), statuses)
	}
	if statuses[0].State != alarmStateWarn {
		t.Fatalf("state: got %q, want %q", statuses[0].State, alarmStateWarn)
	}
	if statuses[0].Message != "TASHTEGO - CPA WARNING" {
		t.Fatalf("message: got %q", statuses[0].Message)
	}
}

// The plugin reuses this same path under self to report losing our own GPS fix.
// That is a sensor fault, not a collision, and must not surface as one.
func TestCollisionNotificationsSkipTheSelfContext(t *testing.T) {
	snapshot := snapshotWithTargets(map[string]any{
		"vessels.self": collisionNotification("alarm", "No GPS position received for more than 32 seconds"),
	})

	if statuses := signalKCollisionNotifications(snapshot); len(statuses) != 0 {
		t.Fatalf("the self-context GPS fault must not surface as a collision alarm, got %+v", statuses)
	}
}

func TestCollisionNotificationsIgnoreWatchingTargets(t *testing.T) {
	snapshot := snapshotWithTargets(map[string]any{
		"vessels.urn:mrn:imo:mmsi:503016440": collisionNotification("normal", "Watching"),
	})

	if statuses := signalKCollisionNotifications(snapshot); len(statuses) != 0 {
		t.Fatalf("normal is the cleared state and must not surface, got %+v", statuses)
	}
}

// Two boats can be in alarm at once. Sharing a rule id would make them
// overwrite each other in the watcher's live set, so one alarm would vanish.
func TestCollisionNotificationsGiveEachTargetItsOwnRuleID(t *testing.T) {
	snapshot := snapshotWithTargets(map[string]any{
		"vessels.urn:mrn:imo:mmsi:503016440": collisionNotification("warn", "TASHTEGO - CPA WARNING"),
		"vessels.urn:mrn:imo:mmsi:512213970": collisionNotification("alarm", "WINGS 12 - CPA ALARM"),
	})

	statuses := signalKCollisionNotifications(snapshot)
	if len(statuses) != 2 {
		t.Fatalf("expected 2 collision notifications, got %d (%+v)", len(statuses), statuses)
	}
	if statuses[0].RuleID == statuses[1].RuleID {
		t.Fatalf("both targets claimed rule id %q", statuses[0].RuleID)
	}
}

// Only the collision branch on a target's tree is alarm-worthy.
func TestCollisionNotificationsIgnoreOtherTargetNotifications(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.urn:mrn:imo:mmsi:503016440",
		Updates: []signalKUpdate{{Values: []signalKValue{{
			Path:  "notifications.navigation.anchor",
			Value: collisionNotification("alarm", "Someone else's anchor drag"),
		}}}},
	}, alarmNow)
	snapshot.setSelfContext("vessels.self")

	if statuses := signalKCollisionNotifications(snapshot); len(statuses) != 0 {
		t.Fatalf("only the collision branch becomes an alarm, got %+v", statuses)
	}
}

// The plugin publishes bearing in degrees despite the SignalK convention (and
// its own README) saying radians. Measured 4.20 to 358.48 across 21 live
// targets. Consumed raw it is out by a factor of 57.
func TestCollisionBearingConvertsDegreesToRadians(t *testing.T) {
	got, ok := collisionBearingRadians(180)
	if !ok {
		t.Fatal("180 degrees is a valid bearing")
	}
	if got < 3.14158 || got > 3.14161 {
		t.Fatalf("bearing: got %v rad, want pi", got)
	}
}

// A plugin release that quietly switched to radians would otherwise sail
// through as a plausible-looking bearing near north. Per the fallback policy
// this rejects rather than guessing which unit was meant.
func TestCollisionBearingRejectsValuesOutsideTheDegreeRange(t *testing.T) {
	for _, raw := range []float64{-1, 360.5, 720} {
		if _, ok := collisionBearingRadians(raw); ok {
			t.Fatalf("%v is not a bearing in degrees and must be rejected", raw)
		}
	}
}

// The plugin's notifications carry an id and canAcknowledge, so the UI offers
// the buttons. Acknowledging has to resolve the value on the target's context;
// looking it up on the self tree finds nothing and refuses a live alarm.
func TestActOnACollisionNotificationResolvesItOnTheTargetContext(t *testing.T) {
	snapshot := snapshotWithTargets(map[string]any{
		"vessels.urn:mrn:imo:mmsi:503016440": collisionNotification("warn", "TASHTEGO - CPA WARNING"),
	})

	ruleID := signalKCollisionNotifications(snapshot)[0].RuleID
	path, ok := notificationRuleIDPath(ruleID)
	if !ok {
		t.Fatalf("rule id %q did not unwrap to a notification path", ruleID)
	}

	var gotID, gotAction string
	post := func(id, action string) error {
		gotID, gotAction = id, action
		return nil
	}

	status, err := actOnSignalKNotification(snapshot, path, notificationActionAcknowledge, alarmNow, post)
	if err != nil {
		t.Fatalf("actOnSignalKNotification: %v", err)
	}
	if gotID != "83c4bc56-f75e-4138-88f6-83a8bfeae043" {
		t.Fatalf("the action is keyed by the notification's own id, got %q", gotID)
	}
	if gotAction != notificationActionAcknowledge {
		t.Fatalf("action: got %q", gotAction)
	}
	if status.Phase != alarmPhaseAcknowledged {
		t.Fatalf("phase: got %q, want %q", status.Phase, alarmPhaseAcknowledged)
	}
	// The round trip has to survive, or the next poll rebuilds a status the
	// frontend cannot match against the one it just acted on.
	if status.RuleID != ruleID {
		t.Fatalf("rule id: got %q, want %q", status.RuleID, ruleID)
	}
}

// A collision alarm that never reaches the watcher is visible only in the live
// list: no push, no log row, and no trace once the target clears.
func TestBusNotificationWatcherRaisesACollisionAlarm(t *testing.T) {
	snapshot := snapshotWithTargets(map[string]any{
		"vessels.urn:mrn:imo:mmsi:512213970": collisionNotification("alarm", "WINGS 12 - CPA ALARM"),
	})
	watcher := newBusNotificationWatcher(snapshot)
	watcher.dwell = 5 * time.Second

	if events := watcher.check(alarmNow); len(events) != 0 {
		t.Fatalf("must not raise on first sight, got %+v", events)
	}

	events := watcher.check(alarmNow.Add(6 * time.Second))
	if len(events) != 1 || events[0].Kind != alarmEventRaised {
		t.Fatalf("expected a single raise once the dwell elapses, got %+v", events)
	}
	if events[0].Rule.ID != "notifications:navigation.closestApproach@urn:mrn:imo:mmsi:512213970" {
		t.Fatalf("rule id: got %q", events[0].Rule.ID)
	}
}

// Two targets in alarm are two alarms. Sharing a tracked key would raise one
// and silently swallow the other.
func TestBusNotificationWatcherRaisesBothTargetsSeparately(t *testing.T) {
	snapshot := snapshotWithTargets(map[string]any{
		"vessels.urn:mrn:imo:mmsi:512213970": collisionNotification("alarm", "WINGS 12 - CPA ALARM"),
		"vessels.urn:mrn:imo:mmsi:503016440": collisionNotification("alarm", "TASHTEGO - CPA ALARM"),
	})
	watcher := newBusNotificationWatcher(snapshot)
	watcher.dwell = 5 * time.Second
	watcher.check(alarmNow)

	events := watcher.check(alarmNow.Add(6 * time.Second))
	if len(events) != 2 {
		t.Fatalf("expected both targets to raise, got %d (%+v)", len(events), events)
	}
}

func targetTree(closestApproach map[string]any) map[string]any {
	return map[string]any{
		"navigation": map[string]any{
			"closestApproach": map[string]any{"value": closestApproach},
		},
	}
}

func TestCollisionFiguresReadTheClosingCase(t *testing.T) {
	figures := collisionFiguresFor(targetTree(map[string]any{
		"distance":            452.7,
		"timeTo":              611.0,
		"range":               1003.5,
		"bearing":             242.42945772101007,
		"collisionRiskRating": 400108.36954535864,
		"collisionAlarmType":  "cpa",
		"collisionAlarmState": "warning",
	}))

	if figures.CpaM == nil || *figures.CpaM != 452.7 {
		t.Fatalf("cpa: got %v", figures.CpaM)
	}
	if figures.TcpaSeconds == nil || *figures.TcpaSeconds != 611.0 {
		t.Fatalf("tcpa: got %v", figures.TcpaSeconds)
	}
	if figures.BearingRad == nil {
		t.Fatal("bearing is published for a closing target")
	}
	if *figures.BearingRad < 4.2311 || *figures.BearingRad > 4.2313 {
		t.Fatalf("bearing: got %v rad, want ~4.23119", *figures.BearingRad)
	}
	if figures.AlarmType != "cpa" || figures.AlarmState != "warning" {
		t.Fatalf("alarm: got %q/%q", figures.AlarmType, figures.AlarmState)
	}
}

// A target that is opening carries range and bearing but no CPA at all. Those
// have to stay absent rather than reporting a closest approach of zero.
func TestCollisionFiguresOmitCpaForAnOpeningTarget(t *testing.T) {
	figures := collisionFiguresFor(targetTree(map[string]any{
		"range":   210.2,
		"bearing": 33.40387571438379,
	}))

	if figures.CpaM != nil || figures.TcpaSeconds != nil {
		t.Fatalf("an opening target has no CPA, got %v/%v", figures.CpaM, figures.TcpaSeconds)
	}
	if figures.BearingRad == nil {
		t.Fatal("bearing is still published for an opening target")
	}
}

// TCPA is legitimately negative just after a target passes its closest point,
// which is exactly when the operator most wants to see it.
func TestCollisionFiguresKeepNegativeTcpa(t *testing.T) {
	figures := collisionFiguresFor(targetTree(map[string]any{"distance": 90.0, "timeTo": -12.0}))

	if figures.TcpaSeconds == nil || *figures.TcpaSeconds != -12.0 {
		t.Fatalf("tcpa: got %v, want -12", figures.TcpaSeconds)
	}
}

// If a plugin release switches to radians the guard drops the reading rather
// than publishing a confident bearing near north.
func TestCollisionFiguresDropAnOutOfRangeBearing(t *testing.T) {
	figures := collisionFiguresFor(targetTree(map[string]any{"bearing": 421.0}))

	if figures.BearingRad != nil {
		t.Fatalf("an out-of-range bearing must not be published, got %v", *figures.BearingRad)
	}
}

func TestCollisionFiguresAreEmptyWithoutThePlugin(t *testing.T) {
	figures := collisionFiguresFor(map[string]any{"navigation": map[string]any{}})

	if figures.CpaM != nil || figures.TcpaSeconds != nil || figures.BearingRad != nil {
		t.Fatalf("no plugin means no figures, got %+v", figures)
	}
}
