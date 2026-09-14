package main

import (
	"strings"
)

// snapshotAlarmReader reads arbitrary dotted SignalK paths out of the delta
// stream snapshot (ADR 0037).
//
// This is the point of the stream work: rules can name any path the server
// publishes, not only the handful the fetchers were hand-wired to.
func snapshotAlarmReader(snapshot *signalKSnapshot) alarmReader {
	return alarmReaderFromTree(snapshot, snapshot.selfContext(), snapshot.selfTree())
}

// alarmReaderFromTree is snapshotAlarmReader over an already-fetched self
// tree and context, so a caller that has already paid for one selfTree()
// copy for some other reason (computeDerivedPaths needs the tree itself to
// find fuel paths; buildGaugeValuesPayload needs it for the same reason)
// does not pay for a second whole-tree copy just to build a reader over the
// same data (backend-perf-audit.md Tier 1 #2).
func alarmReaderFromTree(snapshot *signalKSnapshot, context string, tree map[string]any) alarmReader {
	return func(path string) alarmSample {
		if tree == nil {
			return alarmSample{}
		}

		segments := strings.Split(strings.TrimSpace(path), ".")
		if len(segments) == 0 || segments[0] == "" {
			return alarmSample{}
		}

		// SignalK wraps leaves in a "value" key; the snapshot preserves that
		// shape, so a dotted path becomes the segments plus "value".
		keys := append(append([]string{}, segments...), "value")

		value := lookupNumber(tree, keys...)
		if value == -1 {
			// -1 is lookupNumber's miss sentinel and also a legitimate reading,
			// so confirm presence before treating a miss as absence.
			if _, ok := lookupAnyValue(tree, keys...); !ok {
				return alarmSample{}
			}
		}

		return alarmSample{
			Value:    value,
			Present:  true,
			LastSeen: snapshot.lastSeen(context, path),
		}
	}
}

// lookupAnyValue walks the same key chain as lookupNumber but reports presence
// rather than coercing, so a genuine -1 is distinguishable from a missing path.
func lookupAnyValue(payload map[string]any, keys ...string) (any, bool) {
	var current any = payload
	for _, key := range keys {
		asMap, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := asMap[key]
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}
