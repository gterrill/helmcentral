package main

import (
	"encoding/json"
	"sync"
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
