package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// autoRaiseWatcherFor builds a watcher with every dependency stubbed, mirroring
// testAnchorWatcher in alarm_anchor_test.go. Individual tests override the
// fields they need to vary from tick to tick.
func autoRaiseWatcherFor(enabled bool, anchor *anchorWatchData, sample func() (autoRaiseSample, bool), raise func(time.Time) (bool, error)) *anchorAutoRaiseWatcher {
	return &anchorAutoRaiseWatcher{
		enabled: func() bool { return enabled },
		anchor:  func() *anchorWatchData { return anchor },
		sample:  sample,
		raise:   raise,
	}
}

// qualifyingSample is a sample that satisfies every auto-raise condition:
// fresh RPM on engine 0, a position 60m from a 20m-radius anchor (outside
// radius+buffer), and 5kt SOG.
func qualifyingSample() (autoRaiseSample, bool) {
	return autoRaiseSample{
		Lat: metersNorth(-21.1113, 60), Lon: 149.2276,
		PositionOK: true, GNSSCritical: false,
		SOGKts: 5.0, Engine0RPM: 1200, Engine1RPM: -1,
	}, true
}

func tickEvery5s(t *testing.T, watcher *anchorAutoRaiseWatcher, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		watcher.check(alarmNow.Add(time.Duration(i*5) * time.Second))
	}
}

// Test 1: the core happy path. Every condition holding continuously for the
// full 15s sustain window fires exactly once, not before.
func TestAnchorAutoRaiseFiresAfterSustainedConditions(t *testing.T) {
	anchor := anchorAt(-21.1113, 149.2276, 20)
	raises := 0
	watcher := autoRaiseWatcherFor(true, anchor, qualifyingSample, func(time.Time) (bool, error) { raises++; return true, nil })

	watcher.check(alarmNow)
	watcher.check(alarmNow.Add(5 * time.Second))
	watcher.check(alarmNow.Add(10 * time.Second))
	if raises != 0 {
		t.Fatalf("must not raise before the 15s sustain window elapses, got %d raises", raises)
	}

	watcher.check(alarmNow.Add(15 * time.Second))
	if raises != 1 {
		t.Fatalf("expected exactly 1 raise once sustained for 15s, got %d", raises)
	}
}

// Test 2: backing down to set the hook stops the boat at low SOG well before
// the rode snubs. Even sustained well past 15s, this must never fire — SOG
// is what separates this from actually getting under way, not a longer
// fixed grace period.
func TestAnchorAutoRaiseNeverFiresWhileBackingDown(t *testing.T) {
	anchor := anchorAt(-21.1113, 149.2276, 20)
	watcher := autoRaiseWatcherFor(true, anchor, func() (autoRaiseSample, bool) {
		return autoRaiseSample{
			Lat: metersNorth(-21.1113, 60), Lon: 149.2276,
			PositionOK: true, SOGKts: 1.5, Engine0RPM: 1200, Engine1RPM: -1,
		}, true
	}, func(time.Time) (bool, error) { t.Fatal("backing down (1.5kt SOG) must never auto-raise"); return false, nil })

	tickEvery5s(t, watcher, 10)
}

// Test 3: SOG unavailable (-1) means NOT eligible, never "assume moving."
func TestAnchorAutoRaiseNeverFiresWithSOGUnavailable(t *testing.T) {
	anchor := anchorAt(-21.1113, 149.2276, 20)
	watcher := autoRaiseWatcherFor(true, anchor, func() (autoRaiseSample, bool) {
		return autoRaiseSample{
			Lat: metersNorth(-21.1113, 60), Lon: 149.2276,
			PositionOK: true, SOGKts: -1, Engine0RPM: 1200, Engine1RPM: -1,
		}, true
	}, func(time.Time) (bool, error) { t.Fatal("must not raise when SOG is unavailable"); return false, nil })

	tickEvery5s(t, watcher, 10)
}

// Test 4: readEngineRPM's own sentinel (-1) covers both "no engine path"
// and "the reading is older than defaultRPMMaxAge" - either way, no fresh
// RPM evidence means no auto-raise.
func TestAnchorAutoRaiseNeverFiresWithStaleOrAbsentRPM(t *testing.T) {
	anchor := anchorAt(-21.1113, 149.2276, 20)
	watcher := autoRaiseWatcherFor(true, anchor, func() (autoRaiseSample, bool) {
		return autoRaiseSample{
			Lat: metersNorth(-21.1113, 60), Lon: 149.2276,
			PositionOK: true, SOGKts: 5.0, Engine0RPM: -1, Engine1RPM: -1,
		}, true
	}, func(time.Time) (bool, error) { t.Fatal("must not raise with no fresh RPM evidence"); return false, nil })

	tickEvery5s(t, watcher, 10)
}

// Test 5: a critical GNSS fix must gate this the same way it gates the drag
// watcher (alarm_anchor.go) - a corrupt/jammed fix is not evidence of
// departure.
func TestAnchorAutoRaiseNeverFiresOnCriticalGNSS(t *testing.T) {
	anchor := anchorAt(-21.1113, 149.2276, 20)
	watcher := autoRaiseWatcherFor(true, anchor, func() (autoRaiseSample, bool) {
		return autoRaiseSample{
			Lat: metersNorth(-21.1113, 60), Lon: 149.2276,
			PositionOK: true, GNSSCritical: true, SOGKts: 5.0, Engine0RPM: 1200, Engine1RPM: -1,
		}, true
	}, func(time.Time) (bool, error) { t.Fatal("must not raise on a critical GNSS fix"); return false, nil })

	tickEvery5s(t, watcher, 10)
}

// Test 6: the -1,-1 sentinel pair (and the 0,0 pair) must gate this exactly
// like hasUsableVesselPosition gates every other geolocated feature -
// checked as its own condition, not folded into a bare coordinate range.
func TestAnchorAutoRaiseNeverFiresOnSentinelPosition(t *testing.T) {
	anchor := anchorAt(-21.1113, 149.2276, 20)
	watcher := autoRaiseWatcherFor(true, anchor, func() (autoRaiseSample, bool) {
		return autoRaiseSample{
			Lat: -1, Lon: -1, PositionOK: false, SOGKts: 5.0, Engine0RPM: 1200, Engine1RPM: -1,
		}, true
	}, func(time.Time) (bool, error) { t.Fatal("must not raise on the -1,-1 sentinel position"); return false, nil })

	tickEvery5s(t, watcher, 10)
}

// Test 7: a fetch failure (SignalK unreachable) must never be treated as
// eligible - fail-fast, per AGENTS.md.
func TestAnchorAutoRaiseNeverFiresWhenSampleFetchFails(t *testing.T) {
	anchor := anchorAt(-21.1113, 149.2276, 20)
	watcher := autoRaiseWatcherFor(true, anchor, func() (autoRaiseSample, bool) {
		return autoRaiseSample{}, false
	}, func(time.Time) (bool, error) { t.Fatal("must not raise when the vessel-state read fails"); return false, nil })

	tickEvery5s(t, watcher, 10)
}

// Test 8: any single condition dropping partway through the window resets
// the timer - a fresh 15s of holding is required after the drop, not just
// the remainder.
func TestAnchorAutoRaiseResetsTimerWhenAConditionDrops(t *testing.T) {
	anchor := anchorAt(-21.1113, 149.2276, 20)
	sog := 5.0
	raises := 0
	watcher := autoRaiseWatcherFor(true, anchor, func() (autoRaiseSample, bool) {
		return autoRaiseSample{
			Lat: metersNorth(-21.1113, 60), Lon: 149.2276,
			PositionOK: true, SOGKts: sog, Engine0RPM: 1200, Engine1RPM: -1,
		}, true
	}, func(time.Time) (bool, error) { raises++; return true, nil })

	watcher.check(alarmNow)
	watcher.check(alarmNow.Add(10 * time.Second)) // 10s in - not yet fired

	sog = 1.0 // drops below the SOG floor mid-window
	watcher.check(alarmNow.Add(12 * time.Second))

	sog = 5.0 // recovers - the window must restart from here, not resume
	watcher.check(alarmNow.Add(14 * time.Second))
	watcher.check(alarmNow.Add(20 * time.Second)) // 6s since recovery
	if raises != 0 {
		t.Fatalf("expected no raise before a fresh 15s elapses after the reset, got %d", raises)
	}

	watcher.check(alarmNow.Add(29 * time.Second)) // 15s since recovery at t=14
	if raises != 1 {
		t.Fatalf("expected exactly 1 raise once re-sustained for 15s after the reset, got %d", raises)
	}
}

// Test 9: the feature setting gates this before anything else - disabled
// must never fire, however long conditions hold.
func TestAnchorAutoRaiseNeverFiresWhenDisabled(t *testing.T) {
	anchor := anchorAt(-21.1113, 149.2276, 20)
	watcher := autoRaiseWatcherFor(false, anchor, qualifyingSample, func(time.Time) (bool, error) {
		t.Fatal("a disabled setting must never auto-raise")
		return false, nil
	})

	tickEvery5s(t, watcher, 10)
}

// Test 10: fires only once across a sustained departure, never in a loop -
// pins the raiseAttempted latch directly (the anchor here deliberately
// keeps returning a live watch after "raising" it, isolating the latch from
// the anchor-becomes-nil side effect a real raise would also produce).
func TestAnchorAutoRaiseFiresOnlyOnce(t *testing.T) {
	anchor := anchorAt(-21.1113, 149.2276, 20)
	raises := 0
	watcher := autoRaiseWatcherFor(true, anchor, qualifyingSample, func(time.Time) (bool, error) { raises++; return true, nil })

	tickEvery5s(t, watcher, 20)
	if raises != 1 {
		t.Fatalf("expected exactly 1 raise across a sustained departure, got %d", raises)
	}
}

// Test 11: no active anchor watch means nothing to evaluate, and must never
// panic or call raise.
func TestAnchorAutoRaiseQuietWhenNoWatchIsSet(t *testing.T) {
	watcher := autoRaiseWatcherFor(true, nil, qualifyingSample, func(time.Time) (bool, error) {
		t.Fatal("no anchor watch means nothing to auto-raise")
		return false, nil
	})

	tickEvery5s(t, watcher, 5)
}

// Test 12: a raise failure (e.g. the SignalK publish fails) is logged
// explicitly and not retried in a tight loop while conditions stay true -
// it only gets another chance once conditions drop and re-arm. The failure
// also leaves the watch active and outside radius+buffer, which is exactly
// the condition the existing anchor-drag alarm (alarm_anchor.go) already
// watches for, so the operator is not left with a silent failure - the drag
// alarm covers it independently.
func TestAnchorAutoRaiseFailureIsLoggedAndNotRetriedInATightLoop(t *testing.T) {
	var buf bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(previous)

	anchor := anchorAt(-21.1113, 149.2276, 20)
	attempts := 0
	watcher := autoRaiseWatcherFor(true, anchor, qualifyingSample, func(time.Time) (bool, error) {
		attempts++
		return false, fmt.Errorf("simulated signalk publish failure")
	})

	tickEvery5s(t, watcher, 20)

	if attempts != 1 {
		t.Fatalf("a failed raise must not be retried in a tight loop while conditions stay true, got %d attempts", attempts)
	}
	if !strings.Contains(buf.String(), "anchor auto-raise") || !strings.Contains(buf.String(), "simulated signalk publish failure") {
		t.Fatalf("expected the raise failure to be logged explicitly, got: %s", buf.String())
	}
}

// Test 13: a successful auto-raise records last_auto_raise so polling
// clients can learn about it (getAnchorWatch surfaces it - see anchor_test.go).
func TestAnchorAutoRaiseRecordsLastAutoRaiseOnSuccess(t *testing.T) {
	autoRaiseMu.Lock()
	lastAutoRaise = nil
	autoRaiseMu.Unlock()
	t.Cleanup(func() {
		autoRaiseMu.Lock()
		lastAutoRaise = nil
		autoRaiseMu.Unlock()
	})

	anchor := anchorAt(-21.1113, 149.2276, 20)
	watcher := autoRaiseWatcherFor(true, anchor, qualifyingSample, func(time.Time) (bool, error) { return true, nil })

	tickEvery5s(t, watcher, 4) // reaches t=15s, the sustain threshold

	record := getLastAutoRaise()
	if record == nil {
		t.Fatal("expected a last_auto_raise record after a successful auto-raise")
	}
	if record.Reason != autoRaiseReasonEnginesRunning {
		t.Fatalf("expected reason %q, got %q", autoRaiseReasonEnginesRunning, record.Reason)
	}
	want := alarmNow.Add(15 * time.Second)
	if !record.At.Equal(want) {
		t.Fatalf("expected last_auto_raise.At %v, got %v", want, record.At)
	}
}

// Test 14: the race the raise step must close. check() reads the anchor
// (and its session identity, SetAt) once per tick, but in production a real
// network round trip happens inside w.sample() before raise() ever runs -
// a real window for a concurrent operator action (Raise, then Drop a new
// anchorage) to replace anchorWatchState entirely before this watcher's
// raise() call executes under anchorLifecycleMu. A raise must never apply
// to a session that isn't the one it evaluated: raise must re-confirm,
// under the lock, that the current watch is still the same SetAt it started
// timing - or skip, without recording last_auto_raise, and reset.
func TestAnchorAutoRaiseSkipsRaiseWhenAnchorSessionChangedBeforeRaiseRuns(t *testing.T) {
	stub := anchorPublishEnv(t)

	autoRaiseMu.Lock()
	lastAutoRaise = nil
	autoRaiseMu.Unlock()
	t.Cleanup(func() {
		autoRaiseMu.Lock()
		lastAutoRaise = nil
		autoRaiseMu.Unlock()
	})

	firstSetAt := alarmNow
	replacementSetAt := alarmNow.Add(999 * time.Second)
	anchorWatchMu.Lock()
	anchorWatchState = &anchorWatchData{Lat: -21.1113, Lon: 149.2276, RadiusMeters: 20, SetAt: firstSetAt}
	anchorWatchMu.Unlock()

	watcher := newAnchorAutoRaiseWatcher()
	watcher.enabled = func() bool { return true }

	sampleCalls := 0
	watcher.sample = func() (autoRaiseSample, bool) {
		sampleCalls++
		if sampleCalls == 4 {
			// The 4th tick (t=15s) is where the sustain window elapses and
			// this watcher is about to raise. Simulate a concurrent
			// operator action landing in the real gap this represents (in
			// production, the network round trip inside this very
			// sample() call): a brand-new anchorage, unrelated to the
			// session being timed.
			anchorWatchMu.Lock()
			anchorWatchState = &anchorWatchData{Lat: -21.5000, Lon: 149.6000, RadiusMeters: 20, SetAt: replacementSetAt}
			anchorWatchMu.Unlock()
		}
		return qualifyingSample()
	}

	watcher.check(alarmNow)
	watcher.check(alarmNow.Add(5 * time.Second))
	watcher.check(alarmNow.Add(10 * time.Second))
	watcher.check(alarmNow.Add(15 * time.Second))

	if record := getLastAutoRaise(); record != nil {
		t.Fatalf("must not record an auto-raise for a session that changed before raise ran, got %+v", record)
	}
	if len(stub.captured()) != 0 {
		t.Fatalf("must not have published anything for a session it never evaluated, got %d frames", len(stub.captured()))
	}

	anchorWatchMu.RLock()
	current := anchorWatchState
	anchorWatchMu.RUnlock()
	if current == nil || !current.SetAt.Equal(replacementSetAt) {
		t.Fatalf("the new (unevaluated) anchor session must be untouched, got %+v", current)
	}

	if !watcher.conditionsSince.IsZero() || watcher.raiseAttempted {
		t.Fatalf("expected the watcher to reset after a session mismatch, got conditionsSince=%v raiseAttempted=%v",
			watcher.conditionsSince, watcher.raiseAttempted)
	}
}

// TestNewAnchorAutoRaiseWatcher_EnabledReadsSettingsFailFast pins that a
// settings.yaml readSettings genuinely can't parse is treated as disabled,
// never as "assume on" - AGENTS.md's fallback policy applies to this
// setting the same as any other upstream read. A missing file is NOT this
// case: readSettings treats that as a legitimate default state (every other
// settings consumer in this codebase relies on the same behaviour), which is
// why buildSettingsPayload's own default-true already covers it.
func TestNewAnchorAutoRaiseWatcher_EnabledReadsSettingsFailFast(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.yaml")
	if err := os.WriteFile(path, []byte("anchor: [unterminated"), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv("SETTINGS_FILE", path)

	watcher := newAnchorAutoRaiseWatcher()
	if watcher.enabled() {
		t.Fatal("a settings file readSettings can't parse must never be treated as enabled")
	}
}

// TestNewAnchorAutoRaiseWatcher_EnabledDefaultsTrueWhenSettingsFileIsMissing
// is the counterpart: no settings.yaml at all (a fresh install) is not a
// read failure in this codebase's own convention (readSettings returns an
// empty map, not an error), so it must default true like every other
// installation with no anchor block.
func TestNewAnchorAutoRaiseWatcher_EnabledDefaultsTrueWhenSettingsFileIsMissing(t *testing.T) {
	t.Setenv("SETTINGS_FILE", filepath.Join(t.TempDir(), "does-not-exist.yaml"))

	watcher := newAnchorAutoRaiseWatcher()
	if !watcher.enabled() {
		t.Fatal("expected a missing settings file to default the feature to enabled")
	}
}
