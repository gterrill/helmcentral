package main

import (
	"testing"
	"time"
)

func snapshotWithSelfDelta(path string, value any, at time.Time) *signalKSnapshot {
	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{Values: []signalKValue{{Path: path, Value: value}}}},
	}, at)
	snapshot.setSelfContext("vessels.self")
	return snapshot
}

// The whole point of the delta stream (ADR 0037): a rule can name any path the
// server publishes, not just the ones a fetcher was hand-wired to.
func TestSnapshotAlarmReaderReadsAnArbitraryPath(t *testing.T) {
	snapshot := snapshotWithSelfDelta("electrical.batteries.house.voltage", 12.4, alarmNow)

	sample := snapshotAlarmReader(snapshot)("electrical.batteries.house.voltage")
	if !sample.Present {
		t.Fatalf("expected the path to be present")
	}
	if sample.Value != 12.4 {
		t.Fatalf("value: got %v, want 12.4", sample.Value)
	}
	if !sample.LastSeen.Equal(alarmNow) {
		t.Fatalf("last seen: got %v, want %v", sample.LastSeen, alarmNow)
	}
}

func TestSnapshotAlarmReaderReportsUnknownPathAsAbsent(t *testing.T) {
	snapshot := snapshotWithSelfDelta("electrical.batteries.house.voltage", 12.4, alarmNow)

	sample := snapshotAlarmReader(snapshot)("tanks.fuel.0.currentLevel")
	if sample.Present {
		t.Fatalf("an unconfigured path must read as absent, not zero")
	}
}

// -1 is lookupNumber's miss sentinel and also a real reading (a negative
// current, say). The reader must not confuse the two.
func TestSnapshotAlarmReaderTreatsNegativeOneAsARealValue(t *testing.T) {
	snapshot := snapshotWithSelfDelta("electrical.batteries.house.current", -1.0, alarmNow)

	sample := snapshotAlarmReader(snapshot)("electrical.batteries.house.current")
	if !sample.Present {
		t.Fatalf("-1 is a legitimate reading and must report as present")
	}
	if sample.Value != -1 {
		t.Fatalf("value: got %v, want -1", sample.Value)
	}
}

func TestSnapshotAlarmReaderReportsAbsentWhenSelfContextUnknown(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.urn:mrn:signalk:uuid:abc",
		Updates: []signalKUpdate{{Values: []signalKValue{{Path: "environment.depth.belowTransducer", Value: 3.0}}}},
	}, alarmNow)
	// No hello frame yet, so nothing is known to be self.

	sample := snapshotAlarmReader(snapshot)("environment.depth.belowTransducer")
	if sample.Present {
		t.Fatalf("without a self context there is nothing to read")
	}
}

// End-to-end: a delta lands, a rule reads it, and the alarm fires.
func TestSnapshotAlarmReaderDrivesTheEngine(t *testing.T) {
	snapshot := snapshotWithSelfDelta("electrical.batteries.house.voltage", 11.0, alarmNow)

	rule := validRule()
	rule.ID = "low-house-bank"
	rule.DwellSeconds = 0

	engine := newAlarmEngine()
	events := engine.evaluate([]alarmRule{rule}, snapshotAlarmReader(snapshot), alarmNow)

	if len(events) != 1 || events[0].Kind != alarmEventRaised {
		t.Fatalf("expected the rule to fire from streamed data, got %+v", events)
	}
	if engine.worstActiveState() != alarmStateAlarm {
		t.Fatalf("worst active state: got %q, want %q", engine.worstActiveState(), alarmStateAlarm)
	}
}
