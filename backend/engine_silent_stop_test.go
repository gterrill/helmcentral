package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"os"
	"strings"
	"testing"
	"time"
)

var silentStopNow = time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)

// silentStopRig is a stopper wired to a mutable fake bus, a settable engine
// state map and a recording publisher.
type silentStopRig struct {
	stopper    *engineSilentStopper
	samples    map[string]telemetrySample
	enabled    bool
	engines    []vesselEngineSetting
	states     map[string]engineStateInfo
	published  []string
	publishErr error
	logs       *bytes.Buffer

	statesCalls  int
	enginesCalls int
	// generation stands in for the equipment-change signal.
	generation uint64
}

func newSilentStopRig(t *testing.T) *silentStopRig {
	t.Helper()
	rig := &silentStopRig{
		samples: map[string]telemetrySample{},
		enabled: true,
		engines: []vesselEngineSetting{{Instance: "0", Name: "port"}},
		states:  map[string]engineStateInfo{},
		logs:    &bytes.Buffer{},
	}
	log.SetOutput(rig.logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	rig.stopper = newEngineSilentStopper("unused-settings.yaml")
	rig.stopper.enabled = func() (bool, error) { return rig.enabled, nil }
	rig.stopper.engines = func() ([]vesselEngineSetting, error) {
		rig.enginesCalls++
		return rig.engines, nil
	}
	rig.stopper.generation = func() uint64 { return rig.generation }
	rig.stopper.read = func() telemetryReader { return fakeTelemetry{Samples: rig.samples} }
	rig.stopper.states = func(time.Time) map[string]engineStateInfo {
		rig.statesCalls++
		return rig.states
	}
	rig.stopper.publish = rig.record
	return rig
}

func (r *silentStopRig) record(path string) error {
	if r.publishErr != nil {
		return r.publishErr
	}
	r.published = append(r.published, path)
	return nil
}

// signalK puts one engine's propulsion state on the bus; "" leaves it off.
func (r *silentStopRig) signalK(instance, state string) {
	path := "propulsion." + instance + ".state"
	delete(r.samples, path)
	if state != "" {
		r.samples[path] = telemetrySample{Present: true, Value: state}
	}
}

// quietFor is an engine judged off, last heard d before silentStopNow.
func quietFor(d time.Duration) engineStateInfo {
	return engineStateInfo{State: engineStateOff, LastUpdate: silentStopNow.Add(-d), Witnessed: true}
}

// waitIdle waits for the goroutine tick starts to finish its check.
func (r *silentStopRig) waitIdle(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for r.stopper.running.Load() {
		if time.Now().After(deadline) {
			t.Fatal("the check did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSilentStopNeverWritesForAnUnwitnessedOff(t *testing.T) {
	rig := newSilentStopRig(t)
	rig.signalK("0", "started")
	// Off, but no other device on the engine's connection vouches for the link:
	// a failed gateway looks the same.
	rig.states["0"] = engineStateInfo{State: engineStateOff, LastUpdate: silentStopNow.Add(-5 * time.Minute)}
	rig.stopper.check(silentStopNow)
	rig.wantPublished(t)
	rig.wantNoLogs(t)
	if rig.enginesCalls != 0 {
		t.Fatalf("resolved the mapped engines with nothing witnessed: %d", rig.enginesCalls)
	}
}

func TestSilentStopLogsAPersistentEnginesErrorOnceAcrossTicks(t *testing.T) {
	rig := newSilentStopRig(t)
	rig.signalK("0", "started")
	rig.states["0"] = quietFor(5 * time.Minute)
	rig.stopper.engines = func() ([]vesselEngineSetting, error) { return nil, errors.New("settings garbled") }
	for i := 0; i < 4; i++ {
		rig.stopper.tick(silentStopNow.Add(time.Duration(i) * engineSilentCheckEvery))
		rig.waitIdle(t)
	}
	if n := strings.Count(rig.logs.String(), "settings garbled"); n != 1 {
		t.Fatalf("want the engines error logged once, got %d: %s", n, rig.logs.String())
	}
}

func TestSilentStopStartsCleanWhenTurnedOffAndOnAgain(t *testing.T) {
	rig := newSilentStopRig(t)
	rig.signalK("0", "started")
	rig.states["0"] = quietFor(5 * time.Minute)
	rig.publishErr = errors.New("signalk refused")
	rig.stopper.check(silentStopNow)

	// Still inside the backoff of that failure: nothing is tried.
	rig.publishErr = nil
	rig.stopper.check(silentStopNow.Add(20 * time.Second))
	rig.wantPublished(t)

	// Off and on again forgets the backoff, so the next check tries at once.
	rig.enabled = false
	rig.stopper.tick(silentStopNow.Add(30 * time.Second))
	rig.enabled = true
	rig.stopper.check(silentStopNow.Add(40 * time.Second))
	rig.wantPublished(t, "propulsion.0.state")
}

func TestSilentStopReadsTheMappedEnginesOnceUntilEquipmentChanges(t *testing.T) {
	rig := newSilentStopRig(t)
	rig.signalK("0", "started")
	rig.states["0"] = quietFor(5 * time.Minute)
	for i := 0; i < 5; i++ {
		rig.stopper.check(silentStopNow.Add(time.Duration(i) * engineSilentCheckEvery))
	}
	if rig.enginesCalls != 1 {
		t.Fatalf("five checks inside the refresh interval read the engines %d times, want 1", rig.enginesCalls)
	}

	rig.generation++
	rig.stopper.check(silentStopNow.Add(time.Minute))
	if rig.enginesCalls != 2 {
		t.Fatalf("after the equipment changed: %d reads, want 2", rig.enginesCalls)
	}

	rig.stopper.check(silentStopNow.Add(time.Minute + engineSilentEnginesTTL))
	if rig.enginesCalls != 3 {
		t.Fatalf("after the refresh interval: %d reads, want 3", rig.enginesCalls)
	}
}

func TestSilentStopChecksNoMoreOftenThanTheCadence(t *testing.T) {
	rig := newSilentStopRig(t)
	for i := 0; i < 20; i++ {
		rig.stopper.tick(silentStopNow.Add(time.Duration(i) * time.Second))
		rig.waitIdle(t)
	}
	if rig.statesCalls != 2 {
		t.Fatalf("20 s of one-second ticks: want 2 checks (at 0 s and 15 s), got %d", rig.statesCalls)
	}
}

func TestPublishedValueRejectionNamesWhatTheServerHolds(t *testing.T) {
	cases := []struct {
		raw     string
		matches bool
		holds   string
	}{
		{`"stopped"`, true, `"stopped"`},
		{`"started"`, false, `"started"`},
		{`7`, false, `7`},
		{`{"a":1}`, false, `{"a":1}`},
		{``, false, `no value`},
	}
	for _, c := range cases {
		if got := heldStringIs(json.RawMessage(c.raw), "stopped"); got != c.matches {
			t.Errorf("heldStringIs(%q) = %v, want %v", c.raw, got, c.matches)
		}
		msg := stringRejectedMessage("propulsion.0.state", "stopped", json.RawMessage(c.raw))
		if !strings.Contains(msg, "holds "+c.holds) {
			t.Errorf("message %q lacks %q", msg, "holds "+c.holds)
		}
	}
}

func TestSaveBackoffTakesItsOwnFirstDelayAndCap(t *testing.T) {
	var b saveBackoff
	now := silentStopNow
	b.fail(now, errors.New("x"), 10*time.Second, 25*time.Second)
	if b.delay != 10*time.Second {
		t.Fatalf("first delay %v, want 10s", b.delay)
	}
	b.fail(now, errors.New("x"), 10*time.Second, 25*time.Second)
	b.fail(now, errors.New("x"), 10*time.Second, 25*time.Second)
	if b.delay != 25*time.Second {
		t.Fatalf("capped delay %v, want 25s", b.delay)
	}
}

func (r *silentStopRig) wantPublished(t *testing.T, want ...string) {
	t.Helper()
	if got, wantJoined := strings.Join(r.published, ","), strings.Join(want, ","); got != wantJoined {
		t.Fatalf("published %q, want %q", got, wantJoined)
	}
}

func (r *silentStopRig) wantNoLogs(t *testing.T) {
	t.Helper()
	if r.logs.Len() != 0 {
		t.Fatalf("unexpected log output: %s", r.logs.String())
	}
}

func TestSilentStopMarksAnOffEngineStopped(t *testing.T) {
	rig := newSilentStopRig(t)
	rig.signalK("0", "started")
	rig.states["0"] = quietFor(150 * time.Second)
	rig.stopper.check(silentStopNow)
	rig.wantPublished(t, "propulsion.0.state")
	want := `engine state: port has been off for 150s (no RPM while its connection still sends other data) but SignalK says started; marked stopped (setting "Mark engines stopped when RPM goes quiet")`
	if !strings.Contains(rig.logs.String(), want) {
		t.Fatalf("log %q lacks %q", rig.logs.String(), want)
	}
}

// The write is level-triggered: SignalK still reading "started" on the next
// check (a restart re-emitted it) is written over again.
func TestSilentStopWritesAgainWhenSignalKStillSaysStarted(t *testing.T) {
	rig := newSilentStopRig(t)
	rig.signalK("0", "started")
	rig.states["0"] = quietFor(150 * time.Second)
	rig.stopper.check(silentStopNow)
	rig.wantPublished(t, "propulsion.0.state")

	rig.stopper.check(silentStopNow.Add(engineSilentCheckEvery))
	rig.wantPublished(t, "propulsion.0.state", "propulsion.0.state")
}

func TestSilentStopWritesNothingOnceSignalKReadsStopped(t *testing.T) {
	rig := newSilentStopRig(t)
	rig.signalK("0", "started")
	rig.states["0"] = quietFor(150 * time.Second)
	rig.stopper.check(silentStopNow)
	rig.wantPublished(t, "propulsion.0.state")

	rig.signalK("0", "stopped")
	rig.logs.Reset()
	rig.stopper.check(silentStopNow.Add(engineSilentCheckEvery))
	rig.stopper.check(silentStopNow.Add(time.Hour))
	rig.wantPublished(t, "propulsion.0.state")
	rig.wantNoLogs(t)
}

func TestSilentStopLogsAMissingStatePathOncePerEngineUntilItAppears(t *testing.T) {
	rig := newSilentStopRig(t)
	rig.states["0"] = quietFor(5 * time.Minute)
	want := `engine state: port has no propulsion.0.state on SignalK, so it can't be marked stopped (setting "Mark engines stopped when RPM goes quiet")`
	for i := 0; i < 4; i++ {
		rig.stopper.check(silentStopNow.Add(time.Duration(i) * engineSilentCheckEvery))
	}
	rig.wantPublished(t)
	if n := strings.Count(rig.logs.String(), want); n != 1 {
		t.Fatalf("want the missing-path line once, got %d: %s", n, rig.logs.String())
	}

	// The path appears (stopped, so nothing to write) and goes again: a new
	// problem, logged again.
	rig.signalK("0", "stopped")
	rig.stopper.check(silentStopNow.Add(time.Minute))
	rig.signalK("0", "")
	rig.stopper.check(silentStopNow.Add(2 * time.Minute))
	if n := strings.Count(rig.logs.String(), want); n != 2 {
		t.Fatalf("want the line again after the path came and went, got %d: %s", n, rig.logs.String())
	}
}

func TestSilentStopNeverWritesForALostEngine(t *testing.T) {
	rig := newSilentStopRig(t)
	rig.signalK("0", "started")
	rig.states["0"] = engineStateInfo{State: engineStateLost, LastUpdate: silentStopNow.Add(-time.Hour)}
	rig.stopper.check(silentStopNow)
	rig.wantPublished(t)
	rig.wantNoLogs(t)
	if rig.enginesCalls != 0 {
		t.Fatalf("resolved the mapped engines with nothing off: %d", rig.enginesCalls)
	}
}

func TestSilentStopNeverWritesForARunningOrUnknownEngine(t *testing.T) {
	rig := newSilentStopRig(t)
	rig.signalK("0", "started")
	rig.states["0"] = engineStateInfo{State: engineStateRunning}
	rig.stopper.check(silentStopNow)
	rig.states = map[string]engineStateInfo{} // no source known for the engine
	rig.stopper.check(silentStopNow.Add(time.Second))
	rig.wantPublished(t)
	rig.wantNoLogs(t)
	if rig.enginesCalls != 0 {
		t.Fatalf("resolved the mapped engines with nothing off: %d", rig.enginesCalls)
	}
}

func TestSilentStopWritesNothingWhileTheStreamIsStalled(t *testing.T) {
	rig := newSilentStopRig(t)
	rig.signalK("0", "started")
	rig.states = nil // engineStates answers nil when the stream is stalled
	rig.stopper.check(silentStopNow)
	rig.wantPublished(t)
	rig.wantNoLogs(t)
	if len(rig.stopper.perEngine) != 0 {
		t.Fatalf("set state while the stream was stalled: %v", rig.stopper.perEngine)
	}
}

func TestSilentStopLeavesAnEngineSignalKCallsStoppedAlone(t *testing.T) {
	rig := newSilentStopRig(t)
	rig.signalK("0", "stopped")
	rig.states["0"] = quietFor(5 * time.Minute)
	rig.stopper.check(silentStopNow)
	rig.wantPublished(t)
}

func TestSilentStopIgnoresUnmappedInstances(t *testing.T) {
	rig := newSilentStopRig(t)
	rig.signalK("1", "started")
	rig.states["1"] = quietFor(5 * time.Minute)
	rig.stopper.check(silentStopNow)
	rig.wantPublished(t)
}

func TestSilentStopHandlesEachMappedEngineOnItsOwn(t *testing.T) {
	rig := newSilentStopRig(t)
	rig.engines = []vesselEngineSetting{{Instance: "0", Name: "port"}, {Instance: "1", Name: "starboard"}}
	rig.signalK("0", "started")
	rig.signalK("1", "started")
	rig.states["0"] = quietFor(5 * time.Minute)
	rig.states["1"] = engineStateInfo{State: engineStateRunning}
	rig.stopper.check(silentStopNow)
	rig.wantPublished(t, "propulsion.0.state")
}

func TestSilentStopRetriesAFailedPublishOnlyAfterBackoff(t *testing.T) {
	rig := newSilentStopRig(t)
	rig.signalK("0", "started")
	rig.states["0"] = quietFor(3 * time.Minute)
	rig.publishErr = errors.New("signalk refused")

	rig.stopper.check(silentStopNow)
	if !strings.Contains(rig.logs.String(), "could not mark port stopped") || !strings.Contains(rig.logs.String(), "signalk refused") {
		t.Fatalf("failure not logged: %s", rig.logs.String())
	}

	attempts := 0
	rig.stopper.publish = func(string) error {
		attempts++
		return rig.publishErr
	}
	rig.logs.Reset()
	rig.stopper.check(silentStopNow.Add(30 * time.Second))
	if attempts != 0 {
		t.Fatalf("retried inside the backoff: attempts=%d", attempts)
	}
	rig.wantNoLogs(t)

	rig.stopper.check(silentStopNow.Add(61 * time.Second))
	if attempts != 1 {
		t.Fatalf("want one retry after a minute, got %d", attempts)
	}

	// The wait doubles: two minutes after the second failure.
	rig.stopper.check(silentStopNow.Add(61*time.Second + 90*time.Second))
	if attempts != 1 {
		t.Fatalf("retried inside the doubled backoff: %d", attempts)
	}
	rig.stopper.check(silentStopNow.Add(61*time.Second + 121*time.Second))
	if attempts != 2 {
		t.Fatalf("want a third try after the doubled wait, got %d attempts", attempts)
	}
}

// growBackoff fails three publishes so the retry delay has doubled to four
// minutes, and returns the time of the last failure and the attempt counter.
func growBackoff(t *testing.T, rig *silentStopRig) (time.Time, *int) {
	t.Helper()
	attempts := 0
	rig.stopper.publish = func(string) error {
		attempts++
		return errors.New("signalk refused")
	}
	rig.signalK("0", "started")
	rig.states["0"] = quietFor(3 * time.Minute)
	at := silentStopNow
	rig.stopper.check(at)
	at = at.Add(61 * time.Second)
	rig.stopper.check(at)
	at = at.Add(121 * time.Second)
	rig.stopper.check(at)
	if attempts != 3 {
		t.Fatalf("setup: want 3 failed attempts, got %d", attempts)
	}
	return at, &attempts
}

func TestSilentStopRestartsTheBackoffAfterTheEngineRuns(t *testing.T) {
	rig := newSilentStopRig(t)
	at, attempts := growBackoff(t, rig)

	rig.states["0"] = engineStateInfo{State: engineStateRunning}
	at = at.Add(engineSilentCheckEvery)
	rig.stopper.check(at)

	rig.states["0"] = quietFor(3 * time.Minute)
	at = at.Add(time.Minute)
	rig.stopper.check(at)
	if *attempts != 4 {
		t.Fatalf("a new silence should try at once, attempts=%d", *attempts)
	}
	// The failure just now starts from the first delay (a minute), not the
	// four minutes the earlier outage had grown to.
	rig.stopper.check(at.Add(61 * time.Second))
	if *attempts != 5 {
		t.Fatalf("want a retry after the first delay, attempts=%d", *attempts)
	}
}

func TestSilentStopRestartsTheBackoffAfterSignalKReadsStopped(t *testing.T) {
	rig := newSilentStopRig(t)
	at, attempts := growBackoff(t, rig)

	rig.signalK("0", "stopped")
	at = at.Add(engineSilentCheckEvery)
	rig.stopper.check(at)

	rig.signalK("0", "started")
	at = at.Add(engineSilentCheckEvery)
	rig.stopper.check(at)
	if *attempts != 4 {
		t.Fatalf("a new started reading should try at once, attempts=%d", *attempts)
	}
	rig.stopper.check(at.Add(61 * time.Second))
	if *attempts != 5 {
		t.Fatalf("want a retry after the first delay, attempts=%d", *attempts)
	}
}

func TestSilentStopSucceedsOnRetryAndThenStaysQuiet(t *testing.T) {
	rig := newSilentStopRig(t)
	rig.signalK("0", "started")
	rig.states["0"] = quietFor(3 * time.Minute)
	rig.publishErr = errors.New("down")
	rig.stopper.check(silentStopNow)
	rig.publishErr = nil
	rig.stopper.check(silentStopNow.Add(2 * time.Minute))
	rig.signalK("0", "stopped")
	rig.stopper.check(silentStopNow.Add(10 * time.Minute))
	rig.wantPublished(t, "propulsion.0.state")

	// The success reset the backoff: a later "started" is written at the next
	// check, not after the doubled wait.
	rig.signalK("0", "started")
	rig.stopper.check(silentStopNow.Add(10*time.Minute + engineSilentCheckEvery))
	rig.wantPublished(t, "propulsion.0.state", "propulsion.0.state")
}

func TestSilentStopTickDoesNothingWhileTheSettingIsOff(t *testing.T) {
	rig := newSilentStopRig(t)
	rig.enabled = false
	rig.signalK("0", "started")
	rig.states["0"] = quietFor(5 * time.Minute)
	rig.stopper.tick(silentStopNow)
	if rig.stopper.running.Load() {
		t.Fatal("started a check with the setting off")
	}
	if rig.statesCalls != 0 || rig.enginesCalls != 0 {
		t.Fatalf("looked at the engines with the setting off: states=%d engines=%d", rig.statesCalls, rig.enginesCalls)
	}
	rig.wantPublished(t)
	rig.wantNoLogs(t)
}

func TestSilentStopReportsAnUnreadableSettingOnceAndDoesNothing(t *testing.T) {
	rig := newSilentStopRig(t)
	rig.signalK("0", "started")
	rig.states["0"] = quietFor(5 * time.Minute)
	rig.stopper.enabled = func() (bool, error) { return false, errors.New("settings unreadable") }
	rig.stopper.tick(silentStopNow)
	rig.stopper.tick(silentStopNow.Add(time.Second))
	rig.wantPublished(t)
	if rig.statesCalls != 0 {
		t.Fatalf("looked at the engines with an unreadable setting: %d", rig.statesCalls)
	}
	if n := strings.Count(rig.logs.String(), "settings unreadable"); n != 1 {
		t.Fatalf("want the error logged once, got %d: %s", n, rig.logs.String())
	}
}
