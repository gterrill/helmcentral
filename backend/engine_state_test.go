package main

import (
	"testing"
	"time"
)

func engineStateNow() time.Time { return time.Date(2026, 10, 6, 7, 0, 0, 0, time.UTC) }

func TestEngineStatesRunningWhenSourceIsReporting(t *testing.T) {
	now := engineStateNow()
	sources := []sourceHealth{
		{Source: "n2k.engine.port", First: now.Add(-20 * time.Minute), Last: now.Add(-1 * time.Second), Count: 1200, EngineBound: true, EngineIDs: []string{"port"}},
	}
	got := engineStates(sources, now, 2*time.Second)
	if got["port"].State != engineStateRunning {
		t.Fatalf("port: got %+v, want running", got["port"])
	}
	if !got["port"].LastUpdate.IsZero() {
		t.Fatalf("a running engine carries no last-update time, got %v", got["port"].LastUpdate)
	}
}

// Switching the key off powers the engine computer down while the rest of the
// bus carries on.
func TestEngineStatesOffWhenSiblingSourceOnConnectionIsLive(t *testing.T) {
	now := engineStateNow()
	last := now.Add(-2 * time.Hour)
	sources := []sourceHealth{
		{Source: "n2k.engine.port", First: now.Add(-3 * time.Hour), Last: last, Count: 500, EngineBound: true, EngineIDs: []string{"port"}},
		{Source: "n2k.engine.stbd", First: now.Add(-3 * time.Hour), Last: now.Add(-1 * time.Second), Count: 5000, EngineBound: true, EngineIDs: []string{"starboard"}},
		{Source: "n2k.gps", First: now.Add(-3 * time.Hour), Last: now.Add(-1 * time.Second), Count: 9000},
	}
	got := engineStates(sources, now, 2*time.Second)
	if got["port"].State != engineStateOff || !got["port"].LastUpdate.Equal(last) {
		t.Fatalf("port: got %+v, want off since %v", got["port"], last)
	}
	if got["starboard"].State != engineStateRunning {
		t.Fatalf("starboard: got %+v, want running", got["starboard"])
	}
}

func TestEngineStatesLostWhenConnectionIsAlsoQuiet(t *testing.T) {
	now := engineStateNow()
	last := now.Add(-5 * time.Minute)
	sources := []sourceHealth{
		{Source: "n2k.engine.port", First: now.Add(-20 * time.Minute), Last: last, Count: 200, EngineBound: true, EngineIDs: []string{"port"}},
		{Source: "n2k.depth", First: now.Add(-20 * time.Minute), Last: last, Count: 200},
	}
	got := engineStates(sources, now, 2*time.Second)
	if got["port"].State != engineStateLost || !got["port"].LastUpdate.Equal(last) {
		t.Fatalf("port: got %+v, want lost since %v", got["port"], last)
	}
}

func TestEngineStatesDedicatedEngineConnectionCountsAsOff(t *testing.T) {
	now := engineStateNow()
	sources := []sourceHealth{
		{Source: "eng.port", First: now.Add(-20 * time.Minute), Last: now.Add(-5 * time.Minute), Count: 200, EngineBound: true, EngineIDs: []string{"port"}},
	}
	if got := engineStates(sources, now, 2*time.Second); got["port"].State != engineStateOff {
		t.Fatalf("port: got %+v, want off", got["port"])
	}
}

// One source may publish several engines (a gateway relaying both); the
// engine is running while any source for it still reports.
func TestEngineStatesRunningWhileAnySourceForTheEngineReports(t *testing.T) {
	now := engineStateNow()
	sources := []sourceHealth{
		{Source: "n2k.engine.port", First: now.Add(-3 * time.Hour), Last: now.Add(-2 * time.Hour), Count: 500, EngineBound: true, EngineIDs: []string{"port"}},
		{Source: "n2k.engine.port2", First: now.Add(-3 * time.Hour), Last: now.Add(-1 * time.Second), Count: 5000, EngineBound: true, EngineIDs: []string{"port"}},
	}
	if got := engineStates(sources, now, 2*time.Second); got["port"].State != engineStateRunning {
		t.Fatalf("port: got %+v, want running", got["port"])
	}
}

// A feed that is down altogether is not an engine switching off: no claim.
func TestEngineStatesNoClaimWhenStreamIsDown(t *testing.T) {
	now := engineStateNow()
	sources := []sourceHealth{
		{Source: "n2k.engine.port", First: now.Add(-20 * time.Minute), Last: now.Add(-5 * time.Minute), Count: 200, EngineBound: true, EngineIDs: []string{"port"}},
	}
	if got := engineStates(sources, now, time.Minute); len(got) != 0 {
		t.Fatalf("expected no state with the stream down, got %+v", got)
	}
}

// A source that never published a propulsion path yields no engine state.
func TestEngineStatesIgnoresSourcesWithoutEngineIDs(t *testing.T) {
	now := engineStateNow()
	sources := []sourceHealth{
		{Source: "n2k.gps", First: now.Add(-20 * time.Minute), Last: now.Add(-5 * time.Minute), Count: 200},
	}
	if got := engineStates(sources, now, 2*time.Second); len(got) != 0 {
		t.Fatalf("expected no engines, got %+v", got)
	}
}

// After a restart the replay can leave a dead source with a single update: it
// is not "watched" for the silent-source alarm, but its engine is plainly off.
func TestEngineStatesOffFromSingleReplayedUpdate(t *testing.T) {
	now := engineStateNow()
	last := now.Add(-3 * time.Hour)
	sources := []sourceHealth{
		{Source: "n2k.engine.port", First: last, Last: last, Count: 1, EngineBound: true, EngineIDs: []string{"port"}},
		{Source: "n2k.gps", First: now.Add(-time.Minute), Last: now.Add(-1 * time.Second), Count: 40},
	}
	if got := engineStates(sources, now, 2*time.Second); got["port"].State != engineStateOff {
		t.Fatalf("port: got %+v, want off", got["port"])
	}
}

func TestApplyDeltaRecordsEngineIDsPerSource(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{
			{SourceRef: "n2k.engine.0", Values: []signalKValue{
				{Path: "propulsion.port.revolutions", Value: 20.0},
				{Path: "propulsion.port.fuel.rate", Value: 1.0},
			}},
			{SourceRef: "n2k.engine.1", Values: []signalKValue{{Path: "propulsion.starboard.revolutions", Value: 20.0}}},
			{SourceRef: "n2k.gps", Values: []signalKValue{{Path: "navigation.speedOverGround", Value: 1.0}}},
		},
	}, engineStateNow())
	sources := snapshot.sourcesFor("vessels.self")
	if ids := sources["n2k.engine.0"].EngineIDs; len(ids) != 1 || ids[0] != "port" {
		t.Fatalf("n2k.engine.0: got %v, want [port]", ids)
	}
	if ids := sources["n2k.engine.1"].EngineIDs; len(ids) != 1 || ids[0] != "starboard" {
		t.Fatalf("n2k.engine.1: got %v, want [starboard]", ids)
	}
	if ids := sources["n2k.gps"].EngineIDs; len(ids) != 0 {
		t.Fatalf("n2k.gps: got %v, want none", ids)
	}
}

func TestGaugeValuesPayloadCarriesEngineStates(t *testing.T) {
	now := time.Now().UTC()
	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{
			{SourceRef: "n2k.engine.0", Timestamp: now.Add(-2 * time.Hour).Format(time.RFC3339),
				Values: []signalKValue{{Path: "propulsion.port.revolutions", Value: 12.0}}},
			{SourceRef: "n2k.gps", Timestamp: now.Format(time.RFC3339),
				Values: []signalKValue{{Path: "navigation.speedOverGround", Value: 0.0}}},
		},
	}, now)
	snapshot.setSelfContext("vessels.self")

	engines := engineStatesPayload(snapshot, now)
	port, ok := engines["port"]
	if !ok || port.State != "off" || port.LastUpdate == "" {
		t.Fatalf("port: got %+v (present %v), want off with a last_update", port, ok)
	}
}
