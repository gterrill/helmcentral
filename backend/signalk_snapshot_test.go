package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testNow is the fixed receive time most tests apply deltas at; only the
// staleness tests care about its actual value.
var testNow = time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)

func depthDelta(context string, value float64) signalKDelta {
	return signalKDelta{
		Context: context,
		Updates: []signalKUpdate{{
			Timestamp: "2026-08-12T10:00:00.000Z",
			SourceRef: "n2k.1",
			Values:    []signalKValue{{Path: "environment.depth.belowTransducer", Value: value}},
		}},
	}
}

// TestApplyDeltaNestsDottedPathUnderValueKey verifies that a dotted path
// is correctly nested and stored under a "value" key, making it readable
// via the existing lookupNumber function that expects the REST tree shape.
func TestApplyDeltaNestsDottedPathUnderValueKey(t *testing.T) {
	snapshot := newSignalKSnapshot()
	delta := signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{
			{
				Timestamp: "2026-08-12T10:00:00.000Z",
				SourceRef: "n2k.1",
				Values: []signalKValue{
					{
						Path:  "environment.depth.belowTransducer",
						Value: 1.7,
					},
				},
			},
		},
	}

	snapshot.applyDelta(delta, testNow)
	tree := snapshot.treeFor("vessels.self")

	if tree == nil {
		t.Fatalf("treeFor returned nil for vessels.self")
	}

	// lookupNumber walks: environment -> depth -> belowTransducer -> value
	got := lookupNumber(tree, "environment", "depth", "belowTransducer", "value")
	want := 1.7
	if got != want {
		t.Fatalf("lookupNumber(tree, \"environment\", \"depth\", \"belowTransducer\", \"value\"): got %v, want %v", got, want)
	}
}

// TestApplyDeltaObjectValueRoundTrips verifies that object values are stored
// under "value" and their fields are accessible as nested keys.
func TestApplyDeltaObjectValueRoundTrips(t *testing.T) {
	snapshot := newSignalKSnapshot()
	delta := signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{
			{
				Timestamp: "2026-08-12T10:00:00.000Z",
				SourceRef: "n2k.1",
				Values: []signalKValue{
					{
						Path: "navigation.position",
						Value: map[string]any{
							"latitude":  -36.8,
							"longitude": 174.7,
						},
					},
				},
			},
		},
	}

	snapshot.applyDelta(delta, testNow)
	tree := snapshot.treeFor("vessels.self")

	if tree == nil {
		t.Fatalf("treeFor returned nil for vessels.self")
	}

	// lookupNumber walks: navigation -> position -> value -> latitude
	latGot := lookupNumber(tree, "navigation", "position", "value", "latitude")
	latWant := -36.8
	if latGot != latWant {
		t.Fatalf("latitude: got %v, want %v", latGot, latWant)
	}

	lonGot := lookupNumber(tree, "navigation", "position", "value", "longitude")
	lonWant := 174.7
	if lonGot != lonWant {
		t.Fatalf("longitude: got %v, want %v", lonGot, lonWant)
	}
}

// TestApplyDeltaStringValueReadableViaLookupString verifies that string
// values are correctly stored and accessible via lookupString.
func TestApplyDeltaStringValueReadableViaLookupString(t *testing.T) {
	snapshot := newSignalKSnapshot()
	delta := signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{
			{
				Timestamp: "2026-08-12T10:00:00.000Z",
				SourceRef: "n2k.1",
				Values: []signalKValue{
					{
						Path:  "navigation.state",
						Value: "moored",
					},
				},
			},
		},
	}

	snapshot.applyDelta(delta, testNow)
	tree := snapshot.treeFor("vessels.self")

	if tree == nil {
		t.Fatalf("treeFor returned nil for vessels.self")
	}

	got := lookupString(tree, "navigation", "state", "value")
	want := "moored"
	if got != want {
		t.Fatalf("lookupString(tree, \"navigation\", \"state\", \"value\"): got %q, want %q", got, want)
	}
}

// TestApplyDeltaEmptyPathMergesTopLevel verifies that an empty path merges
// the object's keys at the top level without a "value" wrapper.
func TestApplyDeltaEmptyPathMergesTopLevel(t *testing.T) {
	snapshot := newSignalKSnapshot()
	delta := signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{
			{
				Timestamp: "2026-08-12T10:00:00.000Z",
				SourceRef: "n2k.1",
				Values: []signalKValue{
					{
						Path: "",
						Value: map[string]any{
							"name": "Pikorua",
						},
					},
				},
			},
		},
	}

	snapshot.applyDelta(delta, testNow)
	tree := snapshot.treeFor("vessels.self")

	if tree == nil {
		t.Fatalf("treeFor returned nil for vessels.self")
	}

	// Empty path means keys are merged at top level, so name should be directly accessible
	got := lookupString(tree, "name")
	want := "Pikorua"
	if got != want {
		t.Fatalf("lookupString(tree, \"name\"): got %q, want %q", got, want)
	}
}

// TestApplyDeltaMergingPreservesMultipleSubtrees verifies that applying
// deltas to different subtrees preserves both after both are applied.
func TestApplyDeltaMergingPreservesMultipleSubtrees(t *testing.T) {
	snapshot := newSignalKSnapshot()

	delta1 := signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{
			{
				Timestamp: "2026-08-12T10:00:00.000Z",
				SourceRef: "n2k.1",
				Values: []signalKValue{
					{
						Path:  "environment.depth.belowTransducer",
						Value: 2.5,
					},
				},
			},
		},
	}

	delta2 := signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{
			{
				Timestamp: "2026-08-12T10:00:01.000Z",
				SourceRef: "n2k.1",
				Values: []signalKValue{
					{
						Path:  "navigation.state",
						Value: "anchored",
					},
				},
			},
		},
	}

	snapshot.applyDelta(delta1, testNow)
	snapshot.applyDelta(delta2, testNow)
	tree := snapshot.treeFor("vessels.self")

	if tree == nil {
		t.Fatalf("treeFor returned nil for vessels.self")
	}

	// Both subtrees should be present
	depth := lookupNumber(tree, "environment", "depth", "belowTransducer", "value")
	if depth != 2.5 {
		t.Fatalf("environment.depth: got %v, want 2.5", depth)
	}

	state := lookupString(tree, "navigation", "state", "value")
	if state != "anchored" {
		t.Fatalf("navigation.state: got %q, want %q", state, "anchored")
	}
}

// TestApplyDeltaMergingPreservesSiblingLeaves verifies that updating one leaf
// does not wipe out a sibling leaf set by an earlier delta.
func TestApplyDeltaMergingPreservesSiblingLeaves(t *testing.T) {
	snapshot := newSignalKSnapshot()

	delta1 := signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{
			{
				Timestamp: "2026-08-12T10:00:00.000Z",
				SourceRef: "n2k.1",
				Values: []signalKValue{
					{
						Path:  "environment.depth.belowTransducer",
						Value: 2.5,
					},
				},
			},
		},
	}

	delta2 := signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{
			{
				Timestamp: "2026-08-12T10:00:01.000Z",
				SourceRef: "n2k.1",
				Values: []signalKValue{
					{
						Path:  "environment.depth.belowTransducer",
						Value: 3.0,
					},
				},
			},
		},
	}

	snapshot.applyDelta(delta1, testNow)
	snapshot.applyDelta(delta2, testNow)
	tree := snapshot.treeFor("vessels.self")

	if tree == nil {
		t.Fatalf("treeFor returned nil for vessels.self")
	}

	// The leaf should be updated to 3.0
	depth := lookupNumber(tree, "environment", "depth", "belowTransducer", "value")
	if depth != 3.0 {
		t.Fatalf("environment.depth after second update: got %v, want 3.0", depth)
	}
}

// TestTreeForReturnsDeepCopyNotLiveMap verifies that treeFor returns a deep
// copy, not the live map. Mutating the returned map must not affect what a
// subsequent treeFor call returns.
func TestTreeForReturnsDeepCopyNotLiveMap(t *testing.T) {
	snapshot := newSignalKSnapshot()
	delta := signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{
			{
				Timestamp: "2026-08-12T10:00:00.000Z",
				SourceRef: "n2k.1",
				Values: []signalKValue{
					{
						Path:  "environment.depth.belowTransducer",
						Value: 2.5,
					},
				},
			},
		},
	}

	snapshot.applyDelta(delta, testNow)
	tree1 := snapshot.treeFor("vessels.self")

	if tree1 == nil {
		t.Fatalf("treeFor returned nil for vessels.self")
	}

	// Mutate the returned tree
	if envMap, ok := tree1["environment"].(map[string]any); ok {
		if depthMap, ok := envMap["depth"].(map[string]any); ok {
			if btMap, ok := depthMap["belowTransducer"].(map[string]any); ok {
				btMap["value"] = 999.0
			}
		}
	}

	// Fetch again - it should still have the original value
	tree2 := snapshot.treeFor("vessels.self")
	if tree2 == nil {
		t.Fatalf("treeFor returned nil for vessels.self")
	}

	depth := lookupNumber(tree2, "environment", "depth", "belowTransducer", "value")
	if depth != 2.5 {
		t.Fatalf("depth after mutating tree1: got %v, want 2.5 (not 999.0)", depth)
	}
}

// TestMultipleContextsStayIsolated verifies that deltas applied to one context
// do not appear in another context's tree.
func TestMultipleContextsStayIsolated(t *testing.T) {
	snapshot := newSignalKSnapshot()

	delta1 := signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{
			{
				Timestamp: "2026-08-12T10:00:00.000Z",
				SourceRef: "n2k.1",
				Values: []signalKValue{
					{
						Path:  "environment.depth.belowTransducer",
						Value: 2.5,
					},
				},
			},
		},
	}

	delta2 := signalKDelta{
		Context: "vessels.urn:mrn:signalk:uuid:other",
		Updates: []signalKUpdate{
			{
				Timestamp: "2026-08-12T10:00:01.000Z",
				SourceRef: "n2k.1",
				Values: []signalKValue{
					{
						Path:  "environment.depth.belowTransducer",
						Value: 5.0,
					},
				},
			},
		},
	}

	snapshot.applyDelta(delta1, testNow)
	snapshot.applyDelta(delta2, testNow)

	tree1 := snapshot.treeFor("vessels.self")
	tree2 := snapshot.treeFor("vessels.urn:mrn:signalk:uuid:other")

	if tree1 == nil || tree2 == nil {
		t.Fatalf("one or both trees are nil")
	}

	depth1 := lookupNumber(tree1, "environment", "depth", "belowTransducer", "value")
	if depth1 != 2.5 {
		t.Fatalf("vessels.self depth: got %v, want 2.5", depth1)
	}

	depth2 := lookupNumber(tree2, "environment", "depth", "belowTransducer", "value")
	if depth2 != 5.0 {
		t.Fatalf("vessels.urn:mrn:signalk:uuid:other depth: got %v, want 5.0", depth2)
	}
}

// TestStaleReturnsTrueForNeverSeenPath verifies that stale returns true for
// a path that was never applied.
func TestStaleReturnsTrueForNeverSeenPath(t *testing.T) {
	snapshot := newSignalKSnapshot()
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	maxAge := 1 * time.Minute

	result := snapshot.stale("vessels.self", "environment.depth.belowTransducer", maxAge, now)
	if !result {
		t.Fatalf("stale for never-seen path: got false, want true")
	}
}

// TestStaleFalseForRecentPath verifies that stale returns false for a path
// applied recently, checked with maxAge that has not elapsed.
func TestStaleFalseForRecentPath(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(depthDelta("vessels.self", 2.5), testNow)

	result := snapshot.stale("vessels.self", "environment.depth.belowTransducer", time.Minute, testNow.Add(30*time.Second))
	if result {
		t.Fatalf("stale 30s after apply with a 1m maxAge: got true, want false")
	}
}

// TestStaleTrueForOldPath verifies that stale returns true when now.Sub(T) > maxAge.
// This is what keeps a sensor that stopped reporting visibly dead rather than
// frozen at its last good reading — deltas persist until superseded.
func TestStaleTrueForOldPath(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(depthDelta("vessels.self", 2.5), testNow)

	result := snapshot.stale("vessels.self", "environment.depth.belowTransducer", time.Minute, testNow.Add(5*time.Minute))
	if !result {
		t.Fatalf("stale 5m after apply with a 1m maxAge: got false, want true")
	}
}

// TestStaleIsPerPathNotPerContext guards against a whole context being written
// off as stale because one of its paths went quiet.
func TestStaleIsPerPathNotPerContext(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(depthDelta("vessels.self", 2.5), testNow)
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{
			Values: []signalKValue{{Path: "navigation.speedOverGround", Value: 3.1}},
		}},
	}, testNow.Add(5*time.Minute))

	later := testNow.Add(5 * time.Minute)
	if !snapshot.stale("vessels.self", "environment.depth.belowTransducer", time.Minute, later) {
		t.Fatalf("depth should be stale 5m after its last delta")
	}
	if snapshot.stale("vessels.self", "navigation.speedOverGround", time.Minute, later) {
		t.Fatalf("speedOverGround should be fresh, it was just updated")
	}
}

// TestTreeForOnUnknownContextReturnsNil verifies that treeFor on a context
// that has never received any delta returns nil.
func TestTreeForOnUnknownContextReturnsNil(t *testing.T) {
	snapshot := newSignalKSnapshot()

	tree := snapshot.treeFor("vessels.unknown")
	if tree != nil {
		t.Fatalf("treeFor on unknown context: got %v, want nil", tree)
	}
}

// TestConcurrentApplyDeltaAndTreeFor verifies thread-safety of concurrent
// applyDelta and treeFor operations under -race.
func TestConcurrentApplyDeltaAndTreeFor(t *testing.T) {
	snapshot := newSignalKSnapshot()
	numGoroutines := 10
	numOpsPerGoroutine := 50
	var wg sync.WaitGroup

	// Writers: apply deltas
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < numOpsPerGoroutine; j++ {
				delta := signalKDelta{
					Context: "vessels.self",
					Updates: []signalKUpdate{
						{
							Timestamp: "2026-08-12T10:00:00.000Z",
							SourceRef: "n2k.1",
							Values: []signalKValue{
								{
									Path:  "environment.depth.belowTransducer",
									Value: float64(idx*numOpsPerGoroutine + j),
								},
							},
						},
					},
				}
				snapshot.applyDelta(delta, testNow)
			}
		}(i)
	}

	// Readers: fetch trees
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOpsPerGoroutine; j++ {
				tree := snapshot.treeFor("vessels.self")
				// treeFor returns map[string]any or nil; if non-nil it's valid
				_ = tree
			}
		}()
	}

	wg.Wait()
	// If we get here without -race detecting a data race, we pass.
}

// TestApplyDeltaSetsTimestampAndSourceWhenProvided verifies that timestamp
// and $source are stored as sibling keys when the update provides them.
func TestApplyDeltaSetsTimestampAndSourceWhenProvided(t *testing.T) {
	snapshot := newSignalKSnapshot()
	delta := signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{
			{
				Timestamp: "2026-08-12T10:00:00.000Z",
				SourceRef: "n2k.1",
				Values: []signalKValue{
					{
						Path:  "environment.depth.belowTransducer",
						Value: 2.5,
					},
				},
			},
		},
	}

	snapshot.applyDelta(delta, testNow)
	tree := snapshot.treeFor("vessels.self")

	if tree == nil {
		t.Fatalf("treeFor returned nil for vessels.self")
	}

	// Check that timestamp is stored
	timestamp := lookupString(tree, "environment", "depth", "belowTransducer", "timestamp")
	if timestamp != "2026-08-12T10:00:00.000Z" {
		t.Fatalf("timestamp: got %q, want \"2026-08-12T10:00:00.000Z\"", timestamp)
	}

	// Check that $source is stored
	source := lookupString(tree, "environment", "depth", "belowTransducer", "$source")
	if source != "n2k.1" {
		t.Fatalf("$source: got %q, want \"n2k.1\"", source)
	}
}

// TestApplyDeltaOmitsTimestampAndSourceWhenNotProvided verifies that timestamp
// and $source are not set when the update does not provide them (empty string).
func TestApplyDeltaOmitsTimestampAndSourceWhenNotProvided(t *testing.T) {
	snapshot := newSignalKSnapshot()
	delta := signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{
			{
				// Timestamp: "", SourceRef: "" - omitted or empty
				Values: []signalKValue{
					{
						Path:  "environment.depth.belowTransducer",
						Value: 2.5,
					},
				},
			},
		},
	}

	snapshot.applyDelta(delta, testNow)
	tree := snapshot.treeFor("vessels.self")

	if tree == nil {
		t.Fatalf("treeFor returned nil for vessels.self")
	}

	// Value should still be there
	value := lookupNumber(tree, "environment", "depth", "belowTransducer", "value")
	if value != 2.5 {
		t.Fatalf("value: got %v, want 2.5", value)
	}

	// But timestamp should not be set (lookupString returns "" for missing)
	timestamp := lookupString(tree, "environment", "depth", "belowTransducer", "timestamp")
	if timestamp != "" {
		t.Fatalf("timestamp when not provided: got %q, want \"\"", timestamp)
	}

	// And $source should not be set
	source := lookupString(tree, "environment", "depth", "belowTransducer", "$source")
	if source != "" {
		t.Fatalf("$source when not provided: got %q, want \"\"", source)
	}
}

// TestConnectedStatusTracking verifies that setConnected and status work correctly.
func TestConnectedStatusTracking(t *testing.T) {
	snapshot := newSignalKSnapshot()

	snapshot.setConnected(true)
	connected, _ := snapshot.status()
	if !connected {
		t.Fatalf("connected after setConnected(true): got false, want true")
	}

	snapshot.setConnected(false)
	connected, _ = snapshot.status()
	if connected {
		t.Fatalf("connected after setConnected(false): got true, want false")
	}
}

// TestApplyDeltaTracksSourceSeen verifies applyDelta records a $source's
// first-seen, last-seen and update count, and that a second update from the
// same source advances Last and Count while leaving First alone -- the
// history the sensor-health "silent source" check needs to tell a source
// that has gone quiet from one that never established a cadence.
func TestApplyDeltaTracksSourceSeen(t *testing.T) {
	snapshot := newSignalKSnapshot()
	first := time.Date(2026, 9, 25, 6, 0, 0, 0, time.UTC)
	second := first.Add(10 * time.Second)

	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{
			SourceRef: "venus.battery.512",
			Values:    []signalKValue{{Path: "electrical.batteries.512.voltage", Value: 27.2}},
		}},
	}, first)

	sources := snapshot.sourcesFor("vessels.self")
	entry, ok := sources["venus.battery.512"]
	if !ok {
		t.Fatalf("expected venus.battery.512 to be tracked after one delta")
	}
	if !entry.First.Equal(first) || !entry.Last.Equal(first) || entry.Count != 1 {
		t.Fatalf("after one delta: got %+v, want First=Last=%v Count=1", entry, first)
	}

	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{
			SourceRef: "venus.battery.512",
			Values:    []signalKValue{{Path: "electrical.batteries.512.voltage", Value: 27.3}},
		}},
	}, second)

	entry = snapshot.sourcesFor("vessels.self")["venus.battery.512"]
	if !entry.First.Equal(first) {
		t.Fatalf("First should not move on a later update: got %v, want %v", entry.First, first)
	}
	if !entry.Last.Equal(second) {
		t.Fatalf("Last: got %v, want %v", entry.Last, second)
	}
	if entry.Count != 2 {
		t.Fatalf("Count: got %d, want 2", entry.Count)
	}
}

// TestApplyDeltaSourceSeenIsPerContext verifies sourcesFor only returns
// entries for the requested context, the same isolation pathSeen already
// gives per-path.
func TestApplyDeltaSourceSeenIsPerContext(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{SourceRef: "n2k.1", Values: []signalKValue{{Path: "navigation.speedOverGround", Value: 5.0}}}},
	}, testNow)
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.urn:mrn:imo:mmsi:987654321",
		Updates: []signalKUpdate{{SourceRef: "ais", Values: []signalKValue{{Path: "navigation.speedOverGround", Value: 3.0}}}},
	}, testNow)

	self := snapshot.sourcesFor("vessels.self")
	if _, ok := self["n2k.1"]; !ok {
		t.Fatalf("expected n2k.1 under vessels.self")
	}
	if _, ok := self["ais"]; ok {
		t.Fatalf("expected the other vessel's source not to leak into vessels.self")
	}
}

// TestApplyDeltaSourceSeenIgnoresEmptySourceRef verifies a delta carrying no
// $source (SourceRef == "") is not tracked, matching signalKUpdate's own
// omitempty convention for a field a sender may simply not have set.
func TestApplyDeltaSourceSeenIgnoresEmptySourceRef(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{Values: []signalKValue{{Path: "navigation.speedOverGround", Value: 5.0}}}},
	}, testNow)

	if sources := snapshot.sourcesFor("vessels.self"); len(sources) != 0 {
		t.Fatalf("expected no tracked sources for a delta with no $source, got %v", sources)
	}
}

// TestKnownContextsReturnsEmptySliceWhenNoneApplied verifies that knownContexts
// returns an empty slice when no deltas have been applied.
func TestKnownContextsReturnsEmptySliceWhenNoneApplied(t *testing.T) {
	snapshot := newSignalKSnapshot()
	contexts := snapshot.knownContexts()

	if len(contexts) != 0 {
		t.Fatalf("knownContexts when none applied: got %v, want empty slice", contexts)
	}
}

// TestKnownContextsReturnsSortedContexts verifies that knownContexts returns
// contexts in sorted order.
func TestKnownContextsReturnsSortedContexts(t *testing.T) {
	snapshot := newSignalKSnapshot()

	delta1 := signalKDelta{
		Context: "vessels.zebra",
		Updates: []signalKUpdate{
			{
				Values: []signalKValue{
					{Path: "navigation.state", Value: "moored"},
				},
			},
		},
	}

	delta2 := signalKDelta{
		Context: "vessels.alpha",
		Updates: []signalKUpdate{
			{
				Values: []signalKValue{
					{Path: "navigation.state", Value: "underway"},
				},
			},
		},
	}

	snapshot.applyDelta(delta1, testNow)
	snapshot.applyDelta(delta2, testNow)

	contexts := snapshot.knownContexts()

	if len(contexts) != 2 {
		t.Fatalf("knownContexts: got %d contexts, want 2", len(contexts))
	}

	if contexts[0] != "vessels.alpha" || contexts[1] != "vessels.zebra" {
		t.Fatalf("knownContexts: got %v, want [vessels.alpha vessels.zebra] (sorted)", contexts)
	}
}

// TestUnmarshalSignalKDeltaJSON verifies that a raw SignalK delta message
// unmarshals directly into signalKDelta without custom logic.
func TestUnmarshalSignalKDeltaJSON(t *testing.T) {
	jsonData := `{
		"context": "vessels.self",
		"updates": [
			{
				"source": {"label": "n2k", "type": "NMEA2000"},
				"$source": "n2k.1",
				"timestamp": "2026-08-12T10:00:00.000Z",
				"values": [
					{"path": "environment.depth.belowTransducer", "value": 1.7},
					{"path": "navigation.position", "value": {"latitude": -36.8, "longitude": 174.7}},
					{"path": "navigation.state", "value": "moored"},
					{"path": "", "value": {"name": "Pikorua"}}
				]
			}
		]
	}`

	var delta signalKDelta
	err := json.Unmarshal([]byte(jsonData), &delta)
	if err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if delta.Context != "vessels.self" {
		t.Fatalf("context: got %q, want \"vessels.self\"", delta.Context)
	}

	if len(delta.Updates) != 1 {
		t.Fatalf("updates length: got %d, want 1", len(delta.Updates))
	}

	update := delta.Updates[0]
	if update.Timestamp != "2026-08-12T10:00:00.000Z" {
		t.Fatalf("timestamp: got %q, want \"2026-08-12T10:00:00.000Z\"", update.Timestamp)
	}

	if update.SourceRef != "n2k.1" {
		t.Fatalf("$source: got %q, want \"n2k.1\"", update.SourceRef)
	}

	if len(update.Values) != 4 {
		t.Fatalf("values length: got %d, want 4", len(update.Values))
	}

	// Spot-check the first value
	if update.Values[0].Path != "environment.depth.belowTransducer" {
		t.Fatalf("first value path: got %q", update.Values[0].Path)
	}
	if update.Values[0].Value != 1.7 {
		t.Fatalf("first value: got %v, want 1.7", update.Values[0].Value)
	}
}

// SignalK deltas carry the vessel's real context ("vessels.urn:mrn:..."), never
// the literal "vessels.self", so the snapshot has to be told which one is self.
func TestSelfTreeReturnsTreeForRegisteredSelfContext(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(depthDelta("vessels.urn:mrn:signalk:uuid:abc", 3.5), testNow)
	snapshot.setSelfContext("vessels.urn:mrn:signalk:uuid:abc")

	tree := snapshot.selfTree()
	if tree == nil {
		t.Fatalf("selfTree returned nil for a registered self context")
	}
	if got := lookupNumber(tree, "environment", "depth", "belowTransducer", "value"); got != 3.5 {
		t.Fatalf("depth from selfTree: got %v, want 3.5", got)
	}
}

func TestSelfTreeReturnsNilWhenSelfContextUnknown(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(depthDelta("vessels.urn:mrn:signalk:uuid:abc", 3.5), testNow)

	if snapshot.selfTree() != nil {
		t.Fatalf("selfTree should be nil until the hello frame names the self context")
	}
}

// Some servers report self without the "vessels." prefix; both must resolve.
func TestSetSelfContextNormalizesMissingVesselsPrefix(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(depthDelta("vessels.urn:mrn:signalk:uuid:abc", 3.5), testNow)
	snapshot.setSelfContext("urn:mrn:signalk:uuid:abc")

	if snapshot.selfContext() != "vessels.urn:mrn:signalk:uuid:abc" {
		t.Fatalf("selfContext: got %q, want the vessels-prefixed form", snapshot.selfContext())
	}
	if snapshot.selfTree() == nil {
		t.Fatalf("selfTree should resolve after normalizing an unprefixed self context")
	}
}

// vesselsTree stands in for GET /signalk/v1/api/vessels, which is keyed by bare
// vessel id rather than by delta context.
func TestVesselsTreeKeysByVesselIDStrippingContextPrefix(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(depthDelta("vessels.urn:mrn:signalk:uuid:abc", 3.5), testNow)
	snapshot.applyDelta(depthDelta("vessels.urn:mrn:signalk:uuid:def", 7.5), testNow)

	vessels := snapshot.vesselsTree()
	if len(vessels) != 2 {
		t.Fatalf("vesselsTree size: got %d, want 2", len(vessels))
	}

	abc, ok := vessels["urn:mrn:signalk:uuid:abc"].(map[string]any)
	if !ok {
		t.Fatalf("vesselsTree missing bare-id key for abc, got keys %v", vessels)
	}
	if got := lookupNumber(abc, "environment", "depth", "belowTransducer", "value"); got != 3.5 {
		t.Fatalf("abc depth: got %v, want 3.5", got)
	}
}

func TestVesselsTreeExcludesNonVesselContexts(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(depthDelta("vessels.urn:mrn:signalk:uuid:abc", 3.5), testNow)
	snapshot.applyDelta(depthDelta("atons.urn:mrn:signalk:uuid:buoy", 1.0), testNow)

	vessels := snapshot.vesselsTree()
	if len(vessels) != 1 {
		t.Fatalf("vesselsTree should only contain vessel contexts: got %d entries, want 1", len(vessels))
	}
	if _, present := vessels["urn:mrn:signalk:uuid:buoy"]; present {
		t.Fatalf("vesselsTree must not include an aton context")
	}
}

// ── reconcileNotifications ──────────────────────────────────────────────────
//
// Nothing ever re-reads the SignalK server once a notification is in the
// snapshot (ADR 0086): a notification the server has forgotten -- a restart
// that wiped its in-memory NotificationManager, a clear the instant+minPeriod
// stream dropped, or its 60-120s clean() sweep -- stays live in Helmcentral
// forever. reconcileNotifications is the periodic REST correction for that.

// loadNotificationFixture reads a captured `GET .../notifications` body --
// the raw notifications subtree, not wrapped in another "notifications" key.
func loadNotificationFixture(t *testing.T, name string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "signalk_notifications", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	var tree map[string]any
	if err := json.Unmarshal(raw, &tree); err != nil {
		t.Fatalf("decoding fixture %s: %v", name, err)
	}
	return tree
}

// The exact bug the boat hit: a live notification we hold goes stale, and the
// server's own copy -- fetched here from the real self.json capture -- now
// says normal.
func TestReconcileNotificationsReplacesALiveLeafTheServerNowSaysIsNormal(t *testing.T) {
	snapshot := newSignalKSnapshot()
	ctx := "vessels.self"
	snapshot.setSelfContext(ctx)

	seenAt := time.Date(2026, 9, 8, 4, 0, 0, 0, time.UTC)
	snapshot.applyDelta(signalKDelta{
		Context: ctx,
		Updates: []signalKUpdate{{Values: []signalKValue{{
			Path: "notifications.arrivalCircleEntered",
			Value: map[string]any{
				"state":   "warn",
				"message": "WP arrival circle entered!",
				"id":      "9925e7ce-363e-4e76-8889-ec20197d72d8",
				"status": map[string]any{
					"silenced": false, "acknowledged": false,
					"canSilence": true, "canAcknowledge": true,
				},
			},
		}}}},
	}, seenAt)

	readStartedAt := seenAt.Add(1 * time.Hour)
	server := loadNotificationFixture(t, "self.json") // arrivalCircleEntered is "normal" there

	changed := snapshot.reconcileNotifications(ctx, server, readStartedAt)
	if !slices.Contains(changed, "arrivalCircleEntered") {
		t.Fatalf("expected arrivalCircleEntered among the corrected paths, got %v", changed)
	}

	for _, status := range signalKNotifications(snapshot, ownsNothing) {
		if status.Label == "arrivalCircleEntered" {
			t.Fatalf("the server's normal copy must not surface as live: %+v", status)
		}
	}

	value := lookupAnyMap(snapshot.treeFor(ctx), "notifications", "arrivalCircleEntered", "value")
	if state, _ := value["state"].(string); state != "normal" {
		t.Fatalf("state: got %q, want normal (the server's copy)", state)
	}
}

// A leaf we hold that the server no longer has at all must be dropped, along
// with the now-empty branches leading to it and its pathSeen entry -- this is
// what a signalk-server restart does to every notification (ADR 0086 §1).
func TestReconcileNotificationsDropsALeafTheServerNoLongerHas(t *testing.T) {
	snapshot := newSignalKSnapshot()
	ctx := "vessels.self"
	snapshot.setSelfContext(ctx)

	seenAt := time.Date(2026, 9, 8, 4, 0, 0, 0, time.UTC)
	snapshot.applyDelta(signalKDelta{
		Context: ctx,
		Updates: []signalKUpdate{{Values: []signalKValue{{
			Path:  "notifications.electrical.batteries.house.voltage",
			Value: map[string]any{"state": "alarm", "message": "House bank critically low"},
		}}}},
	}, seenAt)

	readStartedAt := seenAt.Add(1 * time.Minute)
	changed := snapshot.reconcileNotifications(ctx, map[string]any{}, readStartedAt)

	if !slices.Contains(changed, "electrical.batteries.house.voltage") {
		t.Fatalf("expected the dropped leaf reported, got %v", changed)
	}

	tree := snapshot.treeFor(ctx)
	if _, present := tree["notifications"]; present {
		t.Fatalf("the notifications subtree must be gone once its only leaf is dropped, got %v", tree["notifications"])
	}
	if _, present := snapshot.pathSeen[ctx+"|notifications.electrical.batteries.house.voltage"]; present {
		t.Fatalf("pathSeen must be cleared for a dropped leaf")
	}
}

// A delta that arrived after the read began is newer than anything the read
// can say, so it must survive even when the server disagrees.
func TestReconcileNotificationsKeepsALeafSeenAtOrAfterReadStarted(t *testing.T) {
	snapshot := newSignalKSnapshot()
	ctx := "vessels.self"
	snapshot.setSelfContext(ctx)

	readStartedAt := time.Date(2026, 9, 8, 5, 0, 0, 0, time.UTC)
	seenAt := readStartedAt.Add(1 * time.Second) // arrived after the read started
	snapshot.applyDelta(signalKDelta{
		Context: ctx,
		Updates: []signalKUpdate{{Values: []signalKValue{{
			Path:  "notifications.mob",
			Value: map[string]any{"state": "emergency", "message": "Man overboard"},
		}}}},
	}, seenAt)

	server := map[string]any{"mob": map[string]any{"value": map[string]any{"state": "normal", "message": "cleared"}}}
	changed := snapshot.reconcileNotifications(ctx, server, readStartedAt)

	if slices.Contains(changed, "mob") {
		t.Fatalf("a leaf newer than the read must not be reported as corrected, got %v", changed)
	}
	value := lookupAnyMap(snapshot.treeFor(ctx), "notifications", "mob", "value")
	if state, _ := value["state"].(string); state != "emergency" {
		t.Fatalf("expected our own newer leaf kept untouched, got state %q", state)
	}
}

// A leaf live on the server that we never held at all -- the other half of
// what a restart does: signalk-server's fresh notification is invisible to
// Helmcentral until the path changes again, unless the sync adds it.
func TestReconcileNotificationsAddsALiveLeafWeDidNotHold(t *testing.T) {
	snapshot := newSignalKSnapshot()
	ctx := "vessels.self"
	// The context must already be held -- the syncer only ever asks about
	// contexts it has seen a hello or a delta for -- but this vessel has never
	// carried a notification before, so its notifications subtree starts empty.
	snapshot.contexts[ctx] = map[string]any{}
	snapshot.setSelfContext(ctx)

	readStartedAt := time.Date(2026, 9, 8, 5, 0, 0, 0, time.UTC)
	server := map[string]any{
		"navigation": map[string]any{"arrivalCircleEntered": map[string]any{
			"value": map[string]any{"state": "alarm", "message": "WP arrival circle entered!"},
		}},
	}

	changed := snapshot.reconcileNotifications(ctx, server, readStartedAt)
	if !slices.Contains(changed, "navigation.arrivalCircleEntered") {
		t.Fatalf("expected the newly-discovered live leaf reported, got %v", changed)
	}

	statuses := signalKNotifications(snapshot, ownsNothing)
	if len(statuses) != 1 || statuses[0].Label != "navigation.arrivalCircleEntered" {
		t.Fatalf("expected the leaf added and listed, got %+v", statuses)
	}
}

// server == nil is the 404 case: the context has no notifications at all any
// more (or never did). Every notification for that context is removed, but
// nothing else about it, and no other context, is touched.
func TestReconcileNotificationsWithNilServerRemovesEveryNotificationForThatContext(t *testing.T) {
	snapshot := newSignalKSnapshot()
	ctx := "vessels.self"
	snapshot.contexts[ctx] = map[string]any{
		"notifications": loadNotificationFixture(t, "self.json"),
		"navigation":    map[string]any{"position": map[string]any{"value": map[string]any{"latitude": 1.0}}},
	}
	snapshot.pathSeen[ctx+"|notifications.arrivalCircleEntered"] = time.Date(2026, 9, 8, 4, 0, 0, 0, time.UTC)
	snapshot.setSelfContext(ctx)

	otherCtx := "vessels.urn:mrn:imo:mmsi:503135940"
	snapshot.contexts[otherCtx] = map[string]any{
		"notifications": map[string]any{"navigation": map[string]any{"closestApproach": map[string]any{
			"value": map[string]any{"state": "warn", "message": "Closing"},
		}}},
	}

	readStartedAt := time.Date(2026, 9, 8, 5, 20, 0, 0, time.UTC)
	changed := snapshot.reconcileNotifications(ctx, nil, readStartedAt)
	if len(changed) == 0 {
		t.Fatalf("expected the removed leaves to be reported")
	}

	tree := snapshot.treeFor(ctx)
	if _, present := tree["notifications"]; present {
		t.Fatalf("expected notifications gone entirely for a 404 context, got %v", tree["notifications"])
	}
	if got := lookupNumber(tree, "navigation", "position", "value", "latitude"); got != 1.0 {
		t.Fatalf("an unrelated subtree must survive the reconcile untouched, got %v", got)
	}
	if _, present := snapshot.pathSeen[ctx+"|notifications.arrivalCircleEntered"]; present {
		t.Fatalf("pathSeen for a removed leaf must be cleared")
	}

	other := snapshot.treeFor(otherCtx)
	if lookupAnyMap(other, "notifications", "navigation", "closestApproach", "value") == nil {
		t.Fatalf("a different context must not be touched by reconciling this one")
	}
}

// The syncer only ever asks about contexts it already knows about; an unknown
// one must not spring into existence just because it was asked about.
func TestReconcileNotificationsOnUnknownContextIsANoOp(t *testing.T) {
	snapshot := newSignalKSnapshot()

	changed := snapshot.reconcileNotifications("vessels.nobody-home", map[string]any{}, time.Now())
	if changed != nil {
		t.Fatalf("expected nil for an unknown context, got %v", changed)
	}
	if _, ok := snapshot.contexts["vessels.nobody-home"]; ok {
		t.Fatalf("reconcileNotifications must never create a context")
	}
}

// A replacement that leaves liveness unchanged (still live, different
// message) is applied -- the server is still the record -- but is not one of
// the corrections logged, since nothing about what the operator sees changed.
func TestReconcileNotificationsAppliesALivenessPreservingReplacementSilently(t *testing.T) {
	snapshot := newSignalKSnapshot()
	ctx := "vessels.self"
	snapshot.setSelfContext(ctx)

	seenAt := time.Date(2026, 9, 8, 4, 0, 0, 0, time.UTC)
	snapshot.applyDelta(signalKDelta{
		Context: ctx,
		Updates: []signalKUpdate{{Values: []signalKValue{{
			Path:  "notifications.electrical.batteries.house.voltage",
			Value: map[string]any{"state": "warn", "message": "Elevated cell voltage"},
		}}}},
	}, seenAt)

	readStartedAt := seenAt.Add(1 * time.Minute)
	server := map[string]any{"electrical": map[string]any{"batteries": map[string]any{"house": map[string]any{"voltage": map[string]any{
		"value": map[string]any{"state": "warn", "message": "High cell voltage"},
	}}}}}

	changed := snapshot.reconcileNotifications(ctx, server, readStartedAt)
	if slices.Contains(changed, "electrical.batteries.house.voltage") {
		t.Fatalf("a same-liveness replacement must not be reported as a correction, got %v", changed)
	}

	value := lookupAnyMap(snapshot.treeFor(ctx), "notifications", "electrical", "batteries", "house", "voltage", "value")
	if message, _ := value["message"].(string); message != "High cell voltage" {
		t.Fatalf("expected the server's copy applied even though it was not logged, got message %q", message)
	}
}

// ── radar target deltas (backend-perf-audit.md Tier 1 #3) ─────────────────────

// radarTargetDelta builds a delta carrying one mayara ARPA target node, the
// shape captured in testdata/mayara/target-delta.json.
func radarTargetDelta(radarID string, targetID int) signalKDelta {
	return signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{
			Timestamp: "2026-08-28T02:01:13.963584Z",
			SourceRef: "mayara",
			Values: []signalKValue{{
				Path:  "radars." + radarID + ".targets." + strconv.Itoa(targetID),
				Value: map[string]any{"id": targetID, "status": "tracking"},
			}},
		}},
	}
}

// Reproduces the growth backend-perf-audit.md Tier 1 #3 measured: mayara
// target ids only climb and are replayed as nulls on every resubscribe, so
// storing them cost every whole-tree copy in this file a little more every
// week. radar_source.go's REST poller owns radar targets entirely; nothing
// reads one off the snapshot, so applyDelta must never let one reach the
// tree or pathSeen at all.
func TestApplyDeltaDropsRadarTargetNodesFromTheSnapshot(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.setSelfContext("vessels.self")

	for i := 0; i < 50; i++ {
		snapshot.applyDelta(radarTargetDelta("fur6424A", 100000000+i), testNow)
	}
	// A null delta -- how mayara reports a lost or removed target -- must be
	// dropped the same way, not create a value:nil leaf.
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{Values: []signalKValue{{Path: "radars.fur6424A.targets.100000003", Value: nil}}}},
	}, testNow)
	// A control delta on the same radar is a different path shape
	// ("radars.<id>.controls.*") and must still be stored -- only targets are
	// dropped.
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{Values: []signalKValue{{Path: "radars.fur6424A.controls.userName", Value: "Radar 1"}}}},
	}, testNow)

	tree := snapshot.treeFor("vessels.self")
	radar := lookupAnyMap(tree, "radars", "fur6424A")
	if radar == nil {
		t.Fatalf("expected the radar branch to exist from the controls delta")
	}
	if _, ok := radar["targets"]; ok {
		t.Fatalf("radar target nodes must never reach the snapshot tree, got %+v", radar["targets"])
	}
	if _, ok := radar["controls"]; !ok {
		t.Fatalf("a radar control delta on the same radar must still be stored")
	}

	for key := range snapshot.pathSeen {
		if strings.HasPrefix(key, "vessels.self|radars.") && strings.Contains(key, ".targets.") {
			t.Fatalf("pathSeen must not accumulate radar target keys, got %q", key)
		}
	}
}

// ── delta ingestion depth guard (K-1, backend security audit) ─────────────

// TestApplyDeltaRejectsExcessivePathNesting reproduces K-1: an unbounded
// segment count in a delta's path let applyDelta build an arbitrarily deep
// nested tree, which every recursive walker over the snapshot
// (deepCopyMap/deepCopyValue here, freshestTimestampAge and
// flattenNotificationLeaves elsewhere) would later blow Go's goroutine
// stack walking -- a FATAL runtime error recover() cannot catch. The fix
// rejects the value outright at ingestion, before the deep tree is ever
// built, so this test only needs to prove the value never reaches the tree
// -- actually reproducing the stack overflow itself would crash the test
// binary, which is exactly the bug this guards against.
func TestApplyDeltaRejectsExcessivePathNesting(t *testing.T) {
	snapshot := newSignalKSnapshot()

	segments := make([]string, 5000)
	for i := range segments {
		segments[i] = "a"
	}
	hostile := strings.Join(segments, ".")
	if len(segments) <= signalKDeltaMaxPathSegments {
		t.Fatalf("test setup broken: hostile path has %d segments, not more than the cap %d", len(segments), signalKDeltaMaxPathSegments)
	}

	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{
			{
				Timestamp: "2026-08-12T10:00:00.000Z",
				Values: []signalKValue{
					{Path: hostile, Value: 1.0},
					// A legitimate sibling value in the same delta must still be
					// stored: rejection is per-value, not per-delta.
					{Path: "environment.depth.belowTransducer", Value: 2.0},
				},
			},
		},
	}, testNow)

	tree := snapshot.treeFor("vessels.self")
	if depth := lookupNumber(tree, "environment", "depth", "belowTransducer", "value"); depth != 2.0 {
		t.Fatalf("legitimate sibling value in the same delta: got %v, want 2.0", depth)
	}
	if _, ok := tree["a"]; ok {
		t.Fatalf("rejected path must not partially build the tree, found top-level key %q", "a")
	}
	if _, present := snapshot.pathSeen["vessels.self|"+hostile]; present {
		t.Fatalf("rejected path must not be recorded in pathSeen either")
	}
}

// ── empty-path merge safety (K-4, backend security audit) ─────────────────

// TestApplyDeltaEmptyPathDoesNotOverwriteExistingBranch reproduces K-4: an
// empty-path delta whose value object happens to share a key with an
// existing branch (built up by ordinary dotted-path deltas) must not
// replace that whole branch with a bare scalar. The empty-path merge exists
// to carry unwrapped top-level scalars like "name" (see the comment on
// applyDelta's empty-path handling), never to destroy a subtree.
func TestApplyDeltaEmptyPathDoesNotOverwriteExistingBranch(t *testing.T) {
	snapshot := newSignalKSnapshot()

	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{
			Timestamp: "2026-08-12T10:00:00.000Z",
			Values:    []signalKValue{{Path: "navigation.state", Value: "moored"}},
		}},
	}, testNow)

	// {"path":"","value":{"navigation":0}} -- the exact repro from the
	// finding -- landing in the same delta as a genuine top-level scalar.
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{
			Timestamp: "2026-08-12T10:00:01.000Z",
			Values:    []signalKValue{{Path: "", Value: map[string]any{"navigation": 0.0, "name": "Pikorua"}}},
		}},
	}, testNow.Add(time.Second))

	tree := snapshot.treeFor("vessels.self")
	if got := lookupString(tree, "navigation", "state", "value"); got != "moored" {
		t.Fatalf("navigation branch was destroyed by an empty-path merge: lookupString(...) = %q, want %q", got, "moored")
	}
	// A genuine top-level scalar in the same delta must still merge normally.
	if got := lookupString(tree, "name"); got != "Pikorua" {
		t.Fatalf("expected name to merge normally alongside the refused navigation key, got %q", got)
	}
}

// ── excess path eviction (K-2, backend security audit) ─────────────────────

// TestEvictExcessPathsCapsDistinctPathsIncludingSelf reproduces K-2: nothing
// ever bounded how many distinct paths one context's tree could hold, and
// evictStaleVesselContexts deliberately never touches self (a vessel's own
// data must not disappear just because it is stationary) -- so a device
// publishing an ever-growing set of distinct paths under self (no context
// key at all files a sender under self, signalKDelta.Context's own doc
// comment) grew unbounded, with nothing anywhere to catch it.
func TestEvictExcessPathsCapsDistinctPathsIncludingSelf(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.setSelfContext("vessels.self")

	for i := 0; i < signalKContextMaxDistinctPaths+1; i++ {
		path := "environment.sensor.s" + strconv.Itoa(i)
		snapshot.applyDelta(signalKDelta{
			Context: "vessels.self",
			Updates: []signalKUpdate{{Values: []signalKValue{{Path: path, Value: float64(i)}}}},
		}, testNow.Add(time.Duration(i)*time.Millisecond))
	}

	evicted := snapshot.evictExcessPaths()
	if got := evicted["vessels.self"]; got != 1 {
		t.Fatalf("expected 1 path evicted over the cap, got %d (%v)", got, evicted)
	}

	count := 0
	for key := range snapshot.pathSeen {
		if strings.HasPrefix(key, "vessels.self|") {
			count++
		}
	}
	if count != signalKContextMaxDistinctPaths {
		t.Fatalf("pathSeen entries for vessels.self after eviction: got %d, want %d", count, signalKContextMaxDistinctPaths)
	}
}

// TestEvictExcessPathsEvictsLeastRecentlySeenFirst proves eviction order:
// when a context is over the cap, the path that has gone longest without an
// update goes first -- the least likely thing anyone is actually looking at
// right now -- and the path just touched survives.
func TestEvictExcessPathsEvictsLeastRecentlySeenFirst(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.setSelfContext("vessels.self")

	for i := 0; i < signalKContextMaxDistinctPaths+1; i++ {
		path := "environment.sensor.s" + strconv.Itoa(i)
		snapshot.applyDelta(signalKDelta{
			Context: "vessels.self",
			Updates: []signalKUpdate{{Values: []signalKValue{{Path: path, Value: float64(i)}}}},
		}, testNow.Add(time.Duration(i)*time.Millisecond))
	}

	snapshot.evictExcessPaths()

	if _, present := snapshot.pathSeen["vessels.self|environment.sensor.s0"]; present {
		t.Fatalf("expected the least-recently-seen path (s0) evicted first, but it survived")
	}
	newest := "environment.sensor.s" + strconv.Itoa(signalKContextMaxDistinctPaths)
	if _, present := snapshot.pathSeen["vessels.self|"+newest]; !present {
		t.Fatalf("expected the most recently seen path (%s) to survive eviction", newest)
	}

	tree := snapshot.treeFor("vessels.self")
	sensor := lookupAnyMap(tree, "environment", "sensor")
	if _, ok := sensor["s0"]; ok {
		t.Fatalf("evicted path's tree node must be gone too, not just its pathSeen entry")
	}
}

// TestEvictExcessPathsNoOpUnderCap proves the common case -- an ordinary
// boat's tree, nowhere near the cap -- is left completely untouched.
func TestEvictExcessPathsNoOpUnderCap(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.setSelfContext("vessels.self")
	snapshot.applyDelta(depthDelta("vessels.self", 5.0), testNow)

	evicted := snapshot.evictExcessPaths()
	if len(evicted) != 0 {
		t.Fatalf("expected no eviction well under the cap, got %v", evicted)
	}
	if depth := lookupNumber(snapshot.treeFor("vessels.self"), "environment", "depth", "belowTransducer", "value"); depth != 5.0 {
		t.Fatalf("untouched path should be unaffected: got %v, want 5.0", depth)
	}
}

// TestEvictExcessPathsAppliesToNonVesselContextsToo guards against the fix
// accidentally scoping itself to only self or only "vessels."-prefixed
// contexts -- the cap is per context, whatever the context is.
func TestEvictExcessPathsAppliesToNonVesselContextsToo(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.setSelfContext("vessels.self")

	for i := 0; i < signalKContextMaxDistinctPaths+1; i++ {
		path := "notifications.n" + strconv.Itoa(i)
		snapshot.applyDelta(signalKDelta{
			Context: "atons",
			Updates: []signalKUpdate{{Values: []signalKValue{{Path: path, Value: "x"}}}},
		}, testNow.Add(time.Duration(i)*time.Millisecond))
	}

	evicted := snapshot.evictExcessPaths()
	if got := evicted["atons"]; got != 1 {
		t.Fatalf("expected 1 path evicted for the atons context, got %d (%v)", got, evicted)
	}
}

// ── vessel context eviction (backend-perf-audit.md Tier 1 #3) ─────────────────

func TestEvictStaleVesselContextsDropsAContextQuietPastTheThreshold(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.setSelfContext("vessels.self")

	stale := "vessels.urn:mrn:imo:mmsi:503016440"
	snapshot.applyDelta(depthDelta(stale, 5.0), testNow)

	now := testNow.Add(vesselContextStaleAfter + time.Minute)
	evicted := snapshot.evictStaleVesselContexts(now)

	if len(evicted) != 1 || evicted[0] != stale {
		t.Fatalf("expected %q evicted, got %v", stale, evicted)
	}
	if snapshot.treeFor(stale) != nil {
		t.Fatalf("evicted context's tree must be gone")
	}
	if _, present := snapshot.pathSeen[stale+"|environment.depth.belowTransducer"]; present {
		t.Fatalf("evicted context's pathSeen entries must be gone")
	}
}

func TestEvictStaleVesselContextsKeepsAFreshContext(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.setSelfContext("vessels.self")

	fresh := "vessels.urn:mrn:imo:mmsi:503016441"
	snapshot.applyDelta(depthDelta(fresh, 5.0), testNow)

	now := testNow.Add(30 * time.Minute)
	evicted := snapshot.evictStaleVesselContexts(now)

	if len(evicted) != 0 {
		t.Fatalf("expected nothing evicted within the staleness window, got %v", evicted)
	}
	if snapshot.treeFor(fresh) == nil {
		t.Fatalf("a fresh context must survive the sweep")
	}
}

// self must never be evicted, however old its own data looks -- it is this
// vessel, not a contact that can go out of range.
func TestEvictStaleVesselContextsNeverEvictsSelf(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.setSelfContext("vessels.self")
	snapshot.applyDelta(depthDelta("vessels.self", 5.0), testNow)

	now := testNow.Add(24 * time.Hour)
	evicted := snapshot.evictStaleVesselContexts(now)

	if len(evicted) != 0 {
		t.Fatalf("expected self never evicted, got %v", evicted)
	}
	if snapshot.treeFor("vessels.self") == nil {
		t.Fatalf("self's tree must survive the sweep")
	}
}

// TestEvictExcessSourcesCapsDistinctSourcesIncludingSelf mirrors
// TestEvictExcessPathsCapsDistinctPathsIncludingSelf for sourceSeen: a
// context (self included, since self is never a candidate for whole-context
// eviction) that has accumulated more distinct $source strings than
// signalKContextMaxDistinctSources must be brought back under the cap, the
// same flood protection K-2 already gives pathSeen.
func TestEvictExcessSourcesCapsDistinctSourcesIncludingSelf(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.setSelfContext("vessels.self")

	for i := 0; i < signalKContextMaxDistinctSources+1; i++ {
		source := "n2k." + strconv.Itoa(i)
		snapshot.applyDelta(signalKDelta{
			Context: "vessels.self",
			Updates: []signalKUpdate{{SourceRef: source, Values: []signalKValue{{Path: "environment.depth.belowTransducer", Value: 5.0}}}},
		}, testNow.Add(time.Duration(i)*time.Millisecond))
	}

	evicted := snapshot.evictExcessSources()
	if got := evicted["vessels.self"]; got != 1 {
		t.Fatalf("expected 1 source evicted over the cap, got %d (%v)", got, evicted)
	}

	count := 0
	for key := range snapshot.sourceSeen {
		if strings.HasPrefix(key, "vessels.self|") {
			count++
		}
	}
	if count != signalKContextMaxDistinctSources {
		t.Fatalf("sourceSeen entries for vessels.self after eviction: got %d, want %d", count, signalKContextMaxDistinctSources)
	}
	if _, present := snapshot.sourceSeen["vessels.self|n2k.0"]; present {
		t.Fatalf("expected the least-recently-seen source (n2k.0) evicted first, but it survived")
	}
	newest := "n2k." + strconv.Itoa(signalKContextMaxDistinctSources)
	if _, present := snapshot.sourceSeen["vessels.self|"+newest]; !present {
		t.Fatalf("expected the most recently seen source (%s) to survive eviction", newest)
	}
}

// sourceSeen is keyed "<context>|<$source>", exactly parallel to pathSeen's
// own "<context>|<path>" -- an evicted context must lose its sourceSeen
// entries the same way it loses its pathSeen entries, or a contact heard
// once, then gone for good, leaks one sourceSeen entry per $source it ever
// used for as long as the process runs.
func TestEvictStaleVesselContextsDropsSourceSeenEntriesToo(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.setSelfContext("vessels.self")

	stale := "vessels.urn:mrn:imo:mmsi:503016440"
	snapshot.applyDelta(depthDelta(stale, 5.0), testNow)

	if _, present := snapshot.sourceSeen[stale+"|n2k.1"]; !present {
		t.Fatalf("test setup: expected sourceSeen to hold an entry for the stale context before eviction")
	}

	now := testNow.Add(vesselContextStaleAfter + time.Minute)
	evicted := snapshot.evictStaleVesselContexts(now)

	if len(evicted) != 1 || evicted[0] != stale {
		t.Fatalf("expected %q evicted, got %v", stale, evicted)
	}
	if _, present := snapshot.sourceSeen[stale+"|n2k.1"]; present {
		t.Fatalf("evicted context's sourceSeen entries must be gone")
	}
}

// A vessel that reappears after eviction is stored again as an ordinary new
// context -- eviction must leave nothing behind that would make a later
// applyDelta for the same context behave differently from a first sighting.
func TestEvictedVesselContextIsStoredAgainNormallyOnReappearance(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.setSelfContext("vessels.self")

	ctx := "vessels.urn:mrn:imo:mmsi:503016442"
	snapshot.applyDelta(depthDelta(ctx, 5.0), testNow)

	afterSweep := testNow.Add(vesselContextStaleAfter + time.Minute)
	if evicted := snapshot.evictStaleVesselContexts(afterSweep); len(evicted) != 1 {
		t.Fatalf("expected the quiet context evicted first, got %v", evicted)
	}

	reappeared := afterSweep.Add(time.Minute)
	snapshot.applyDelta(depthDelta(ctx, 7.5), reappeared)

	tree := snapshot.treeFor(ctx)
	if tree == nil {
		t.Fatalf("expected the reappeared context to be stored")
	}
	if depth := lookupNumber(tree, "environment", "depth", "belowTransducer", "value"); depth != 7.5 {
		t.Fatalf("depth after reappearance: got %v, want 7.5", depth)
	}
	if seen := snapshot.pathSeen[ctx+"|environment.depth.belowTransducer"]; !seen.Equal(reappeared) {
		t.Fatalf("expected pathSeen refreshed to the reappearance time, got %v", seen)
	}

	// A second sweep right away must not re-evict a context that just came
	// back to life.
	if evicted := snapshot.evictStaleVesselContexts(reappeared); len(evicted) != 0 {
		t.Fatalf("expected the reappeared context to survive an immediate sweep, got %v", evicted)
	}
}

// ── whole-tree copy accounting (backend-perf-audit.md Tier 1 #2) ──────────────

// nodeAt must still hand back an independent copy: mutating what it returns
// must never be visible through a later nodeAt call, the same "handlers read
// this concurrently with the stream writing" guarantee treeFor documents.
func TestNodeAtReturnsACopyNotTheLiveNode(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.setSelfContext("vessels.self")
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{Values: []signalKValue{{Path: "environment.depth.belowTransducer", Value: 2.5}}}},
	}, testNow)

	node := snapshot.nodeAt("environment.depth.belowTransducer")
	if node == nil {
		t.Fatalf("expected a node")
	}
	node["value"] = 999.0

	again := snapshot.nodeAt("environment.depth.belowTransducer")
	if again["value"] != 2.5 {
		t.Fatalf("mutating a returned node must not affect the snapshot, got %v", again["value"])
	}
}

// nodeAt must not touch snapshotWholeTreeCopies: copying one node, not the
// whole tree, is the entire point of this accessor (backend-perf-audit.md
// Tier 1 #2 measured selfTree()+walk at 1.2ms/8k allocs per call against a
// 3,000-leaf tree, paid once per enabled alarm rule every tick before this).
func TestNodeAtDoesNotCountAsAWholeTreeCopy(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.setSelfContext("vessels.self")
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{Values: []signalKValue{{Path: "environment.depth.belowTransducer", Value: 2.5}}}},
	}, testNow)

	before := atomic.LoadInt64(&snapshotWholeTreeCopies)
	for i := 0; i < 15; i++ {
		snapshot.nodeAt("environment.depth.belowTransducer")
	}
	if after := atomic.LoadInt64(&snapshotWholeTreeCopies); after != before {
		t.Fatalf("nodeAt must never deep-copy the whole tree, count went %d -> %d", before, after)
	}
}

// vesselNotificationBranches must copy only the notifications branch per
// vessel, never the whole vessel tree vesselsTree() would (collision_ais.go's
// only caller reads nothing else off another vessel's tree).
func TestVesselNotificationBranchesDoesNotCountAsAWholeTreeCopy(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.setSelfContext("vessels.self")
	target := "vessels.urn:mrn:imo:mmsi:503016440"
	snapshot.applyDelta(signalKDelta{
		Context: target,
		Updates: []signalKUpdate{{Values: []signalKValue{{
			Path:  "notifications.navigation.closestApproach",
			Value: collisionNotification("warn", "TASHTEGO - CPA WARNING"),
		}}}},
	}, testNow)

	before := atomic.LoadInt64(&snapshotWholeTreeCopies)
	branches := snapshot.vesselNotificationBranches()
	if after := atomic.LoadInt64(&snapshotWholeTreeCopies); after != before {
		t.Fatalf("vesselNotificationBranches must never deep-copy a whole vessel tree, count went %d -> %d", before, after)
	}
	if _, ok := branches[strings.TrimPrefix(target, vesselContextPrefix)]; !ok {
		t.Fatalf("expected the target's notifications branch, got %v", branches)
	}
}
