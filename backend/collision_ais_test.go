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

// snapshotWithTargets seeds every target's position as having just arrived.
func snapshotWithTargets(targets map[string]any) *signalKSnapshot {
	return snapshotWithTargetsAged(targets, nil)
}

// snapshotWithTargetsAged takes absolute per-context position times. A context
// absent from the map is stamped fresh (as of alarmNow, the same instant the
// notification below is written); one mapped to the zero time never carried a
// position delta at all. Absolute rather than durations (contrast
// seedVesselTreesAged), because TestCollisionNotificationsKeepATargetStillTransmitting
// needs a position NEWER than the notification, and a single "how long ago"
// duration cannot express that.
//
// pathSeen is written directly rather than through a second applyDelta with a
// synthetic lat/lon, mirroring seedVesselTreesAged (signalk_payload_test.go):
// the collision path only ever reads pathSeen for position
// (aisTargetPositionFresh), never the position value itself.
func snapshotWithTargetsAged(targets map[string]any, positionSeen map[string]time.Time) *signalKSnapshot {
	snapshot := newSignalKSnapshot()
	for context, value := range targets {
		snapshot.applyDelta(signalKDelta{
			Context: context,
			Updates: []signalKUpdate{{Values: []signalKValue{{
				Path:  "notifications.navigation.closestApproach",
				Value: value,
			}}}},
		}, alarmNow)

		seenAt, aged := positionSeen[context]
		if !aged {
			seenAt = alarmNow
		}
		if seenAt.IsZero() {
			// Nothing to write: applyDelta above only ever touched the
			// notification path, so pathSeen already has no entry for
			// navigation.position on this context, which is exactly what
			// "never carried a position delta" means to lastSeen.
			continue
		}
		snapshot.pathSeen[context+"|navigation.position"] = seenAt
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

	statuses := signalKCollisionNotifications(snapshot, alarmNow)
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

	if statuses := signalKCollisionNotifications(snapshot, alarmNow); len(statuses) != 0 {
		t.Fatalf("the self-context GPS fault must not surface as a collision alarm, got %+v", statuses)
	}
}

func TestCollisionNotificationsIgnoreWatchingTargets(t *testing.T) {
	snapshot := snapshotWithTargets(map[string]any{
		"vessels.urn:mrn:imo:mmsi:503016440": collisionNotification("normal", "Watching"),
	})

	if statuses := signalKCollisionNotifications(snapshot, alarmNow); len(statuses) != 0 {
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

	statuses := signalKCollisionNotifications(snapshot, alarmNow)
	if len(statuses) != 2 {
		t.Fatalf("expected 2 collision notifications, got %d (%+v)", len(statuses), statuses)
	}
	if statuses[0].RuleID == statuses[1].RuleID {
		t.Fatalf("both targets claimed rule id %q", statuses[0].RuleID)
	}
}

// Only the collision branch on a target's tree is alarm-worthy.
//
// This target's position has to be stamped fresh even though snapshotWithTargets
// is no use here (it writes the closestApproach node this test must NOT have,
// or the branch check it exists to exercise never runs). Left unstamped, the
// staleness gate this ADR adds would drop the target for having no recorded
// position at all -- a true fact, but the wrong reason: the test would pass
// whether or not the branch filter still worked, which is exactly the "passing
// for the wrong reason" trap the plan for that gate calls out by name.
func TestCollisionNotificationsIgnoreOtherTargetNotifications(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.urn:mrn:imo:mmsi:503016440",
		Updates: []signalKUpdate{{Values: []signalKValue{{
			Path:  "notifications.navigation.anchor",
			Value: collisionNotification("alarm", "Someone else's anchor drag"),
		}}}},
	}, alarmNow)
	snapshot.pathSeen["vessels.urn:mrn:imo:mmsi:503016440|navigation.position"] = alarmNow
	snapshot.setSelfContext("vessels.self")

	if statuses := signalKCollisionNotifications(snapshot, alarmNow); len(statuses) != 0 {
		t.Fatalf("only the collision branch becomes an alarm, got %+v", statuses)
	}
}

// ── target position staleness ────────────────────────────────────────────────
//
// The plugin writes notifications.navigation.closestApproach on a state
// change, not on a timer (ADR 0057), and the snapshot never evicts a context
// (signalk_snapshot.go has no delete(s.contexts, ...) anywhere). So a target
// that stops transmitting leaves its last warn/alarm node frozen in the tree
// forever, and without a gate here it stays live in the bus watcher's set for
// as long as the process runs.

// Headline reproduction: a target with a frozen alarm and a position that
// stopped arriving collisionTargetMaxAge-and-then-some ago must not still
// count. Before the fix nothing aged a collision notification against
// anything, so signalKCollisionNotifications returned 1 here regardless.
func TestCollisionNotificationsDropATargetThatHasLeftAISRange(t *testing.T) {
	context := "vessels.urn:mrn:imo:mmsi:512213970"
	snapshot := snapshotWithTargetsAged(
		map[string]any{context: collisionNotification("alarm", "WINGS 12 - CPA ALARM")},
		map[string]time.Time{context: alarmNow.Add(-(collisionTargetMaxAge + time.Minute))},
	)

	if statuses := signalKCollisionNotifications(snapshot, alarmNow); len(statuses) != 0 {
		t.Fatalf("a target with no position update in over %s must not hold its alarm up, got %+v", collisionTargetMaxAge, statuses)
	}
}

// The notification itself is 30 minutes stale by the time this evaluates --
// the plugin wrote it once and has not touched it since, exactly as designed
// for a target that is still closing steadily. Position, not the notification,
// is what has to stay fresh. This is what breaks first if someone later "cleans
// up" the gate by ageing the notification's own write time instead of position:
// it would drop a target that is still very much in range.
func TestCollisionNotificationsKeepATargetStillTransmitting(t *testing.T) {
	context := "vessels.urn:mrn:imo:mmsi:512213970"
	snapshot := snapshotWithTargetsAged(
		map[string]any{context: collisionNotification("alarm", "WINGS 12 - CPA ALARM")},
		map[string]time.Time{context: alarmNow.Add(30 * time.Minute)},
	)

	statuses := signalKCollisionNotifications(snapshot, alarmNow.Add(30*time.Minute+time.Second))
	if len(statuses) != 1 {
		t.Fatalf("expected 1 collision notification, got %d (%+v)", len(statuses), statuses)
	}
	if statuses[0].State != alarmStateAlarm {
		t.Fatalf("state: got %q, want %q", statuses[0].State, alarmStateAlarm)
	}
}

// Pins the boundary the same way TestFetchSignalKNearbyVessels_KeepsVesselAtCutoffBoundary
// does for the tile: a target aged to exactly the cutoff has not yet exceeded
// it, so the comparison must be strictly greater-than, not greater-or-equal.
func TestCollisionNotificationsKeepATargetAtTheStalenessBoundary(t *testing.T) {
	context := "vessels.urn:mrn:imo:mmsi:512213970"
	snapshot := snapshotWithTargetsAged(
		map[string]any{context: collisionNotification("alarm", "WINGS 12 - CPA ALARM")},
		map[string]time.Time{context: alarmNow.Add(-collisionTargetMaxAge)},
	)

	if statuses := signalKCollisionNotifications(snapshot, alarmNow); len(statuses) != 1 {
		t.Fatalf("a target aged exactly to the cutoff has not yet exceeded it, got %d (%+v)", len(statuses), statuses)
	}
}

// Locks in the post-restart consequence as deliberate rather than an accident:
// a target can carry a frozen alarm node with no navigation.position delta
// received yet this run. There is nothing to age it against, and a CPA figure
// with no corroborating position is unverifiable, so it must not alarm until
// the first position delta arrives -- matching the tile's own
// TestFetchSignalKNearbyVessels_DropsVesselWithNoPositionDelta.
func TestCollisionNotificationsDropATargetWithNoPositionDeltaAtAll(t *testing.T) {
	context := "vessels.urn:mrn:imo:mmsi:512213970"
	snapshot := snapshotWithTargetsAged(
		map[string]any{context: collisionNotification("alarm", "WINGS 12 - CPA ALARM")},
		map[string]time.Time{context: time.Time{}},
	)

	if statuses := signalKCollisionNotifications(snapshot, alarmNow); len(statuses) != 0 {
		t.Fatalf("a target with no position delta at all is unverifiable and must not alarm, got %+v", statuses)
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

	ruleID := signalKCollisionNotifications(snapshot, alarmNow)[0].RuleID
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

// The real reproduction: nothing changes on the bus, only the clock advances.
// That is exactly what "the target left AIS range" looks like from inside this
// process -- the plugin will not touch this notification again, ever, because
// it only writes on a state change and there is no state change left to make.
// Before the fix this never clears: signalKCollisionNotifications keeps
// returning the frozen node forever, so it never leaves the watcher's live set
// and the clear branch at the bottom of check() is never reached.
func TestBusNotificationWatcherClearsACollisionAlarmWhenTheTargetLeavesRange(t *testing.T) {
	snapshot := snapshotWithTargets(map[string]any{
		"vessels.urn:mrn:imo:mmsi:512213970": collisionNotification("alarm", "WINGS 12 - CPA ALARM"),
	})
	watcher := newBusNotificationWatcher(snapshot)
	watcher.dwell = 5 * time.Second

	if events := watcher.check(alarmNow); len(events) != 0 {
		t.Fatalf("must not raise on first sight, got %+v", events)
	}
	if events := watcher.check(alarmNow.Add(6 * time.Second)); len(events) != 1 || events[0].Kind != alarmEventRaised {
		t.Fatalf("setup: expected a raise once the dwell elapses, got %+v", events)
	}

	// The snapshot is untouched from here on -- only the clock moves past the
	// staleness cutoff.
	events := watcher.check(alarmNow.Add(collisionTargetMaxAge + time.Minute))
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 event once the target ages out, got %d (%+v)", len(events), events)
	}
	if events[0].Kind != alarmEventCleared {
		t.Fatalf("kind: got %q, want %q", events[0].Kind, alarmEventCleared)
	}
	if events[0].Rule.ID != "notifications:navigation.closestApproach@urn:mrn:imo:mmsi:512213970" {
		t.Fatalf("rule id: got %q", events[0].Rule.ID)
	}
	if events[0].Status.State != alarmStateNormal {
		t.Fatalf("state: got %q, want %q", events[0].Status.State, alarmStateNormal)
	}
}

// Guards against a gate that drops the whole slice rather than one entry: two
// targets in alarm, one goes stale and one keeps transmitting, and only the
// stale one may clear.
func TestBusNotificationWatcherClearsOnlyTheTargetThatLeftRange(t *testing.T) {
	leaving := "vessels.urn:mrn:imo:mmsi:512213970"
	staying := "vessels.urn:mrn:imo:mmsi:503016440"
	snapshot := snapshotWithTargets(map[string]any{
		leaving: collisionNotification("alarm", "WINGS 12 - CPA ALARM"),
		staying: collisionNotification("alarm", "TASHTEGO - CPA ALARM"),
	})
	watcher := newBusNotificationWatcher(snapshot)
	watcher.dwell = 5 * time.Second

	watcher.check(alarmNow)
	if events := watcher.check(alarmNow.Add(6 * time.Second)); len(events) != 2 {
		t.Fatalf("setup: expected both targets to raise, got %d (%+v)", len(events), events)
	}

	checkAt := alarmNow.Add(collisionTargetMaxAge + time.Minute)
	// The staying target keeps transmitting: its position is refreshed right up
	// to the moment being checked. The leaving target's position is left alone,
	// still stamped from setup, so only it crosses the staleness cutoff.
	snapshot.pathSeen[staying+"|navigation.position"] = checkAt

	events := watcher.check(checkAt)
	if len(events) != 1 || events[0].Kind != alarmEventCleared {
		t.Fatalf("expected exactly one clear for the target that left range, got %+v", events)
	}
	if events[0].Rule.ID != "notifications:navigation.closestApproach@urn:mrn:imo:mmsi:512213970" {
		t.Fatalf("cleared rule id: got %q", events[0].Rule.ID)
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
