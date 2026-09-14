package main

import (
	"math"
	"sort"
	"strings"
	"time"
)

// collisionNotificationBranch is the only path on another vessel's tree that
// becomes an alarm. Nothing else a target carries is ours to raise a klaxon
// over (ADR 0057).
var collisionNotificationBranch = []string{"navigation", "closestApproach"}

// notificationVesselSeparator marks which AIS target a notification belongs to
// inside a rule id.
//
// Every collision notification shares one path, so the vessel is the only thing
// telling two of them apart. A urn-shaped SignalK vessel id contains no "@",
// and neither does any notification path, so the split is unambiguous in both
// directions.
const notificationVesselSeparator = "@"

// splitNotificationVessel separates a notification branch from the AIS target
// it was raised on. A self-tree notification carries no vessel and comes back
// with an empty id.
func splitNotificationVessel(path string) (branch, vesselID string) {
	branch, vesselID, found := strings.Cut(path, notificationVesselSeparator)
	if !found {
		return path, ""
	}
	return branch, vesselID
}

// signalKCollisionNotifications surfaces the AIS collision warnings raised by
// signalk-ais-target-prioritizer.
//
// signalKNotifications reads the self tree, which is where every other producer
// on the bus raises its alarms. The target prioritizer is the exception: it
// raises one notification per AIS target, on that target's own context, so the
// self walk finds none of them. Hence a sibling rather than a widened walk —
// these are per-target, unbounded in number, and carry a vessel identity the
// self walk has no notion of.
//
// The self context is skipped rather than merged. The plugin reuses this same
// path under self to report losing our own GPS fix, which is a sensor fault
// wearing a collision alarm's clothes; that condition is already visible
// through vessel-state staleness, and routing it here would put a klaxon on the
// wrong problem and teach the crew to distrust the CPA alarm (ADR 0057).
//
// Not every vessel context ever seen is fair game, either. The snapshot never
// evicts a context and the plugin only rewrites this notification on a state
// change, not on a timer, so a target that stops transmitting leaves its last
// warn/alarm node frozen in the tree forever. Without a freshness gate that
// frozen node would outlive the target by however long this process keeps
// running, which is the bug ADR 0057 §6 exists to close.
func signalKCollisionNotifications(snapshot *signalKSnapshot, now time.Time) []alarmStatus {
	// vesselNotificationBranches copies only each vessel's notifications
	// subtree, not everything AIS publishes about it the way vesselsTree()
	// would -- this runs on every activeAlarms() call (backend-perf-audit.md
	// Tier 1 #2), against however many AIS contexts this box has heard from.
	vessels := snapshot.vesselNotificationBranches()
	if len(vessels) == 0 {
		return nil
	}

	self := strings.TrimPrefix(snapshot.selfContext(), vesselContextPrefix)

	var out []alarmStatus
	for vesselID, tree := range vessels {
		// "self" is checked alongside the resolved context because some
		// servers report self unprefixed (see setSelfContext).
		if vesselID == self || vesselID == "self" {
			continue
		}

		// Gate on the target's position, never on this notification's own
		// age: the plugin writes it once on a state change and then leaves it
		// alone for as long as the target keeps closing, so ageing the
		// notification against itself would clear an alarm that is still
		// entirely valid. Position keeps arriving on a timer for as long as
		// the target is in AIS range, which is exactly the fact this gate
		// needs (ADR 0057 §6).
		if _, fresh := aisTargetPositionFresh(snapshot, vesselID, collisionTargetMaxAge, now); !fresh {
			continue
		}

		value, ok := collisionNotificationValue(tree)
		if !ok {
			continue
		}
		status, ok := notificationStatus(value, collisionNotificationBranch)
		if !ok {
			continue
		}

		// Two boats can be in alarm at once. Without the vessel in the rule id
		// they collide in the watcher's live set and one alarm disappears.
		status.RuleID += notificationVesselSeparator + vesselID
		out = append(out, status)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].RuleID < out[j].RuleID })
	return out
}

// collisionNotificationValue walks one target's tree to its collision
// notification value.
func collisionNotificationValue(tree map[string]any) (map[string]any, bool) {
	node, ok := tree[notificationsRoot].(map[string]any)
	if !ok {
		return nil, false
	}
	for _, segment := range collisionNotificationBranch {
		child, ok := node[segment].(map[string]any)
		if !ok {
			return nil, false
		}
		node = child
	}
	value, ok := node["value"].(map[string]any)
	return value, ok
}

// collisionBearingRadians converts the prioritizer's bearing to the radians
// every other bearing in the model is expressed in.
//
// The plugin publishes degrees. Its README claims rad True and the SignalK
// convention is radians, but the schema-supplied meta on this path documents
// only distance and timeTo, so nothing in the model declares a unit for bearing
// and nothing catches the mismatch. Measured across 21 live targets the values
// spanned 4.20 to 358.48, which settles it.
//
// Out-of-range is rejected rather than reinterpreted. If a plugin release
// quietly switches to radians, every reading becomes a plausible-looking
// bearing near north, and silently picking whichever unit looks reasonable is
// exactly the masking fallback the project forbids. Rejecting surfaces the
// disagreement instead.
func collisionBearingRadians(degrees float64) (float64, bool) {
	if math.IsNaN(degrees) || degrees < 0 || degrees > 360 {
		return 0, false
	}
	return degrees * math.Pi / 180, true
}

// collisionNumber reads one property off a target's closestApproach value.
//
// It reports presence rather than using lookupNumber's -1 sentinel: TCPA is
// legitimately negative once a target is opening, so -1 cannot mean both
// "missing" and "one second past CPA".
func collisionNumber(vesselMap map[string]any, property string) (float64, bool) {
	node := lookupAnyMap(vesselMap, "navigation", "closestApproach", "value")
	if node == nil {
		return 0, false
	}
	switch v := node[property].(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	}
	return 0, false
}

// collisionFigures are the AIS collision numbers for one target, all optional:
// a target that is opening rather than closing carries a bearing but no CPA,
// and with the plugin uninstalled a target carries none of them.
type collisionFigures struct {
	CpaM        *float64
	TcpaSeconds *float64
	BearingRad  *float64
	AlarmType   string
	AlarmState  string
}

// collisionFiguresFor reads the prioritizer's published figures off one
// target's tree, normalising bearing on the way in so the rest of the host only
// ever sees radians.
//
// collisionRiskRating is deliberately not read. It is a sort key running to six
// significant figures, not a number an operator can check against the plotter,
// and what displays these targets is a separate decision (ADR 0057).
func collisionFiguresFor(vesselMap map[string]any) collisionFigures {
	var figures collisionFigures

	if cpa, ok := collisionNumber(vesselMap, "distance"); ok {
		figures.CpaM = &cpa
	}
	if tcpa, ok := collisionNumber(vesselMap, "timeTo"); ok {
		figures.TcpaSeconds = &tcpa
	}
	if degrees, ok := collisionNumber(vesselMap, "bearing"); ok {
		if radians, ok := collisionBearingRadians(degrees); ok {
			figures.BearingRad = &radians
		}
	}

	figures.AlarmType = strings.TrimSpace(lookupString(vesselMap, "navigation", "closestApproach", "value", "collisionAlarmType"))
	figures.AlarmState = strings.TrimSpace(lookupString(vesselMap, "navigation", "closestApproach", "value", "collisionAlarmState"))
	return figures
}
