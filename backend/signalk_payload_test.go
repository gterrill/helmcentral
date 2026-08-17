package main

import (
	"encoding/json"
	"testing"
	"time"
)

// withGlobalSnapshot swaps the package-level snapshot for the duration of a test.
func withGlobalSnapshot(t *testing.T, snapshot *signalKSnapshot) {
	t.Helper()
	original := globalSignalKSnapshot
	globalSignalKSnapshot = snapshot
	t.Cleanup(func() { globalSignalKSnapshot = original })
}

// seedSelfTree installs a SignalK REST-shaped JSON body as the self vessel's
// tree. The stream reassembles deltas into exactly this shape (ADR 0037), so
// REST fixtures remain valid test data without going near a real delta.
func seedSelfTree(t *testing.T, body string) {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("seeding self tree: %v", err)
	}

	snapshot := newSignalKSnapshot()
	snapshot.contexts["vessels.self"] = payload
	snapshot.setSelfContext("vessels.self")
	withGlobalSnapshot(t, snapshot)
}

// seedVesselTrees installs a GET /signalk/v1/api/vessels-shaped body, keyed by
// bare vessel id, as the set of known vessel contexts. Every vessel's
// navigation.position is stamped as seen right now, so fixtures written before
// the staleness filter existed (backend/signalk.go's fetchSignalKNearbyVessels)
// still report as fresh without having to know about pathSeen at all.
func seedVesselTrees(t *testing.T, body string) {
	t.Helper()
	seedVesselTreesAged(t, body, nil, time.Now().UTC())
}

// seedVesselTreesAged is seedVesselTrees with control over receive time: each
// vessel id present in ages has its navigation.position path backdated by that
// duration from now; any id absent from ages is stamped fresh, same as
// seedVesselTrees. This is what drives the staleness-filter tests, which need
// pathSeen to disagree with "just seeded" for specific vessels.
func seedVesselTreesAged(t *testing.T, body string, ages map[string]time.Duration, now time.Time) {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("seeding vessel trees: %v", err)
	}

	snapshot := newSignalKSnapshot()
	for id, tree := range payload {
		asMap, ok := tree.(map[string]any)
		if !ok {
			continue
		}
		snapshot.contexts[vesselContextPrefix+id] = asMap

		seenAt := now
		if age, ok := ages[id]; ok {
			seenAt = now.Add(-age)
		}
		snapshot.pathSeen[vesselContextPrefix+id+"|navigation.position"] = seenAt
	}
	withGlobalSnapshot(t, snapshot)
}

func selfSnapshotWithDepth(depth float64) *signalKSnapshot {
	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(depthDelta("vessels.urn:mrn:signalk:uuid:abc", depth), testNow)
	snapshot.setSelfContext("vessels.urn:mrn:signalk:uuid:abc")
	return snapshot
}

func TestSignalKSelfPayloadReadsFromSnapshot(t *testing.T) {
	withGlobalSnapshot(t, selfSnapshotWithDepth(2.5))

	payload, err := signalKSelfPayload()
	if err != nil {
		t.Fatalf("signalKSelfPayload: unexpected error %v", err)
	}
	if got := lookupNumber(payload, "environment", "depth", "belowTransducer", "value"); got != 2.5 {
		t.Fatalf("depth: got %v, want 2.5", got)
	}
}

// The fallback policy is the point: an empty stream is an outage and must be
// reported as one. There is no REST path left to quietly borrow.
func TestSignalKSelfPayloadErrorsWhenStreamHasNoData(t *testing.T) {
	withGlobalSnapshot(t, newSignalKSnapshot())

	if _, err := signalKSelfPayload(); err == nil {
		t.Fatalf("expected an error when the stream has no data, got nil")
	}
}

func TestSignalKVesselsPayloadIsKeyedByBareVesselID(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(depthDelta("vessels.urn:mrn:signalk:uuid:abc", 3.5), testNow)
	withGlobalSnapshot(t, snapshot)

	payload, err := signalKVesselsPayload()
	if err != nil {
		t.Fatalf("signalKVesselsPayload: unexpected error %v", err)
	}

	vessel, ok := payload["urn:mrn:signalk:uuid:abc"].(map[string]any)
	if !ok {
		t.Fatalf("vessels payload should be keyed by bare vessel id, got %v", payload)
	}
	if got := lookupNumber(vessel, "environment", "depth", "belowTransducer", "value"); got != 3.5 {
		t.Fatalf("vessel depth: got %v, want 3.5", got)
	}
}

func TestFetchSignalKVesselStateReadsFromStreamSnapshot(t *testing.T) {
	withGlobalSnapshot(t, selfSnapshotWithDepth(6.25))
	// GNSS validation latches critical across calls in module-level state.
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	state, err := fetchSignalKVesselState()
	if err != nil {
		t.Fatalf("fetchSignalKVesselState: unexpected error %v", err)
	}
	if state.Depth != 6.25 {
		t.Fatalf("depth: got %v, want 6.25", state.Depth)
	}
}

// A stream outage must reach criticalVesselState so position freezes at the
// last trusted fix rather than reading as a jump and tripping the anchor alarm.
func TestFetchSignalKVesselStateReportsCriticalWhenStreamHasNoData(t *testing.T) {
	withGlobalSnapshot(t, newSignalKSnapshot())
	// This test deliberately drives the validator critical, and that state
	// latches until several good samples clear it — reset it so later tests
	// do not inherit blanked coordinates.
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	state, err := fetchSignalKVesselState()
	if err == nil {
		t.Fatalf("expected an error when the stream has no data, got nil")
	}
	if !state.GNSSCriticalAlert {
		t.Fatalf("a stream outage must raise the GNSS critical alert")
	}
	if state.GNSSValidationState != "critical" {
		t.Fatalf("GNSS validation state: got %q, want %q", state.GNSSValidationState, "critical")
	}
}
