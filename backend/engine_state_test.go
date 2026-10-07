package main

import (
	"testing"
	"time"
)

func engineStateNow() time.Time { return time.Date(2026, 10, 6, 7, 0, 0, 0, time.UTC) }

func TestEngineStatesRunningWhenSourceIsReporting(t *testing.T) {
	now := engineStateNow()
	sources := []sourceHealth{
		{Source: "n2k.engine.port", First: now.Add(-20 * time.Minute), Last: now.Add(-1 * time.Second), Count: 1200, EngineBound: true, EngineLast: map[string]time.Time{"port": now.Add(-1 * time.Second)}},
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
		{Source: "n2k.engine.port", First: now.Add(-3 * time.Hour), Last: last, Count: 500, EngineBound: true, EngineLast: map[string]time.Time{"port": last}},
		{Source: "n2k.engine.stbd", First: now.Add(-3 * time.Hour), Last: now.Add(-1 * time.Second), Count: 5000, EngineBound: true, EngineLast: map[string]time.Time{"starboard": now.Add(-1 * time.Second)}},
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
		{Source: "n2k.engine.port", First: now.Add(-20 * time.Minute), Last: last, Count: 200, EngineBound: true, EngineLast: map[string]time.Time{"port": last}},
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
		{Source: "eng.port", First: now.Add(-20 * time.Minute), Last: now.Add(-5 * time.Minute), Count: 200, EngineBound: true, EngineLast: map[string]time.Time{"port": now.Add(-5 * time.Minute)}},
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
		{Source: "n2k.engine.port", First: now.Add(-3 * time.Hour), Last: now.Add(-2 * time.Hour), Count: 500, EngineBound: true, EngineLast: map[string]time.Time{"port": now.Add(-2 * time.Hour)}},
		{Source: "n2k.engine.port2", First: now.Add(-3 * time.Hour), Last: now.Add(-1 * time.Second), Count: 5000, EngineBound: true, EngineLast: map[string]time.Time{"port": now.Add(-1 * time.Second)}},
	}
	if got := engineStates(sources, now, 2*time.Second); got["port"].State != engineStateRunning {
		t.Fatalf("port: got %+v, want running", got["port"])
	}
}

// A feed that is down altogether is not an engine switching off: no claim.
func TestEngineStatesNoClaimWhenStreamIsDown(t *testing.T) {
	now := engineStateNow()
	sources := []sourceHealth{
		{Source: "n2k.engine.port", First: now.Add(-20 * time.Minute), Last: now.Add(-5 * time.Minute), Count: 200, EngineBound: true, EngineLast: map[string]time.Time{"port": now.Add(-5 * time.Minute)}},
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
		{Source: "n2k.engine.port", First: last, Last: last, Count: 1, EngineBound: true, EngineLast: map[string]time.Time{"port": last}},
		{Source: "n2k.gps", First: now.Add(-time.Minute), Last: now.Add(-1 * time.Second), Count: 40},
	}
	if got := engineStates(sources, now, 2*time.Second); got["port"].State != engineStateOff {
		t.Fatalf("port: got %+v, want off", got["port"])
	}
}

func TestApplyDeltaRecordsEngineLastPerSource(t *testing.T) {
	snapshot := newSignalKSnapshot()
	now := engineStateNow()
	old := now.Add(-2 * time.Hour)
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{
			{SourceRef: "n2k.engine.0", Timestamp: old.Format(time.RFC3339), Values: []signalKValue{
				{Path: "propulsion.port.revolutions", Value: 20.0},
				{Path: "propulsion.port.fuel.rate", Value: 1.0},
			}},
			{SourceRef: "n2k.engine.0", Timestamp: now.Format(time.RFC3339), Values: []signalKValue{
				{Path: "propulsion.starboard.revolutions", Value: 20.0}}},
			{SourceRef: "n2k.gps", Values: []signalKValue{{Path: "navigation.speedOverGround", Value: 1.0}}},
		},
	}, now)
	sources := snapshot.sourcesFor("vessels.self")
	last := sources["n2k.engine.0"].EngineLast
	if len(last) != 2 || !last["port"].Equal(old) || !last["starboard"].Equal(now) {
		t.Fatalf("n2k.engine.0: got %v, want port %v and starboard %v", last, old, now)
	}
	if got := sources["n2k.gps"].EngineLast; len(got) != 0 {
		t.Fatalf("n2k.gps: got %v, want none", got)
	}
}

// One gateway publishes both engines. The stopped one must read off while the
// other keeps the source itself fresh.
func TestEngineStatesSharedSourceTellsEnginesApart(t *testing.T) {
	now := engineStateNow()
	stopped := now.Add(-10 * time.Minute)
	sources := []sourceHealth{
		{Source: "n2k.gateway", First: now.Add(-3 * time.Hour), Last: now.Add(-1 * time.Second), Count: 9000, EngineBound: true,
			EngineLast: map[string]time.Time{"port": stopped, "starboard": now.Add(-1 * time.Second)}},
		{Source: "n2k.gps", First: now.Add(-3 * time.Hour), Last: now.Add(-1 * time.Second), Count: 9000},
	}
	got := engineStates(sources, now, 2*time.Second)
	if got["port"].State != engineStateOff || !got["port"].LastUpdate.Equal(stopped) {
		t.Fatalf("port: got %+v, want off since %v", got["port"], stopped)
	}
	if got["starboard"].State != engineStateRunning {
		t.Fatalf("starboard: got %+v, want running", got["starboard"])
	}
}

// After a restart replays the source, First is the oldest retained timestamp
// and the whole-history average gap is huge. Off still arrives two minutes
// after the last reading, as documented.
func TestEngineStatesOffAfterTwoMinutesDespiteReplayedHistory(t *testing.T) {
	now := engineStateNow()
	last := now.Add(-150 * time.Second)
	sources := []sourceHealth{
		{Source: "n2k.engine.port", First: now.Add(-20 * time.Hour), Last: last, Count: 1800, EngineBound: true,
			EngineLast: map[string]time.Time{"port": last}},
		{Source: "n2k.gps", First: now.Add(-20 * time.Hour), Last: now.Add(-1 * time.Second), Count: 90000},
	}
	if got := engineStates(sources, now, 2*time.Second); got["port"].State != engineStateOff {
		t.Fatalf("port: got %+v, want off at 150 s", got["port"])
	}
	sources[0].Last = now.Add(-90 * time.Second)
	sources[0].EngineLast = map[string]time.Time{"port": sources[0].Last}
	if got := engineStates(sources, now, 2*time.Second); got["port"].State != engineStateRunning {
		t.Fatalf("port: got %+v, want running at 90 s", got["port"])
	}
}

// Two sources last updated the same instant, one off and one lost by the
// connection check: the answer must not depend on slice or map order.
func TestEngineStatesTieOnLastIsDeterministic(t *testing.T) {
	now := engineStateNow()
	last := now.Add(-10 * time.Minute)
	a := sourceHealth{Source: "a.eng", First: last.Add(-time.Hour), Last: last, Count: 100, EngineBound: true,
		EngineLast: map[string]time.Time{"port": last}}
	aSibling := sourceHealth{Source: "a.depth", First: last.Add(-time.Hour), Last: last, Count: 100}
	b := sourceHealth{Source: "b.eng", First: last.Add(-time.Hour), Last: last, Count: 100, EngineBound: true,
		EngineLast: map[string]time.Time{"port": last}}
	orders := [][]sourceHealth{{a, aSibling, b}, {b, aSibling, a}, {aSibling, b, a}}
	for i, order := range orders {
		if got := engineStates(order, now, 2*time.Second); got["port"].State != engineStateLost {
			t.Fatalf("order %d: got %+v, want lost (lowest source key wins the tie)", i, got["port"])
		}
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

// offEngineSnapshot has a port engine silent for two hours, a starboard engine
// reporting now, and a GPS on the same bus reporting now.
func offEngineSnapshot(t *testing.T, now time.Time) *signalKSnapshot {
	t.Helper()
	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{
			{SourceRef: "n2k.engine.0", Timestamp: now.Add(-2 * time.Hour).Format(time.RFC3339),
				Values: []signalKValue{{Path: "propulsion.port.revolutions", Value: 12.0}}},
			{SourceRef: "n2k.engine.1", Timestamp: now.Format(time.RFC3339),
				Values: []signalKValue{{Path: "propulsion.starboard.revolutions", Value: 30.0}}},
			{SourceRef: "n2k.gps", Timestamp: now.Format(time.RFC3339),
				Values: []signalKValue{{Path: "navigation.speedOverGround", Value: 0.0}}},
		},
	}, now)
	snapshot.setSelfContext("vessels.self")
	return snapshot
}

// Every gauge, group and lamp bound to a switched-off engine's paths agrees
// with the cluster: no value and no stale age, not a frozen reading marked
// STALE. A running engine's paths are untouched.
func TestGaugeValuesPayloadBlanksSwitchedOffEnginePathsWithoutAge(t *testing.T) {
	now := time.Now().UTC()
	withGlobalSnapshot(t, offEngineSnapshot(t, now))
	setPagesWithGaugePaths(t, "propulsion.port.revolutions", "propulsion.starboard.revolutions")

	payload := buildGaugeValuesPayload()
	values := payload["values"].(map[string]any)
	ages := payload["ages"].(map[string]float64)
	if values["propulsion.port.revolutions"] != nil || ages["propulsion.port.revolutions"] != -1 {
		t.Fatalf("off engine: got value %v age %v, want nil and -1", values["propulsion.port.revolutions"], ages["propulsion.port.revolutions"])
	}
	if values["propulsion.starboard.revolutions"] == nil {
		t.Fatalf("running engine lost its value: %v", values)
	}
}

// An alarm rule bound to a switched-off engine's path neither fires on the
// frozen value nor raises a stale alarm for it; a running engine's does.
func TestAlarmReaderTreatsSwitchedOffEnginePathAsNeitherStaleNorPresent(t *testing.T) {
	now := time.Now().UTC()
	snapshot := offEngineSnapshot(t, now)
	read := derivedAwareAlarmReader(snapshot)

	off := read("propulsion.port.revolutions")
	if off.Present {
		t.Fatalf("off engine sample must read absent, got %+v", off)
	}
	staleRule := alarmRule{Op: alarmOpStale, StaleAfterSeconds: 30}
	if alarmSampleStale(staleRule, off, now) {
		t.Fatal("a stale-data rule must not fire for an engine that is switched off")
	}
	if alarmSampleStale(staleRule, read("propulsion.starboard.revolutions"), now.Add(time.Hour)) != true {
		t.Fatal("a running engine's path still goes stale as before")
	}
	if !read("propulsion.starboard.revolutions").Present {
		t.Fatal("running engine must stay present")
	}
}
