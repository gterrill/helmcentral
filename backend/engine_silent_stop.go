package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// engineSilentStopSettingLabel is the Settings label the log line names, so an
// operator reading the log can find the switch that caused the write.
const engineSilentStopSettingLabel = "Mark engines stopped when RPM goes quiet"

const (
	// engineSilentCheckEvery is how often the stopper judges the engines. The
	// judgement itself has a two-minute grain (silentSourceQuietFor), so asking
	// every second of the alarm loop would only repeat the same answer.
	engineSilentCheckEvery = 15 * time.Second
	// engineSilentRetryFirst and engineSilentRetryMax bound the wait after a
	// failed write: a minute, doubling to ten.
	engineSilentRetryFirst = time.Minute
	engineSilentRetryMax   = 10 * time.Minute
	// engineSilentEnginesTTL is the longest the mapped-engine list is kept.
	engineSilentEnginesTTL = 5 * time.Minute
)

// heldStringIs and stringRejectedMessage read back a string leaf of the
// server's model.
func heldStringIs(raw json.RawMessage, want string) bool {
	var got string
	return json.Unmarshal(raw, &got) == nil && got == want
}

// stringRejectedMessage quotes the raw JSON the server holds, so a non-string
// value or a missing leaf is not reported as an empty string.
func stringRejectedMessage(path, want string, raw json.RawMessage) string {
	held := strings.TrimSpace(string(raw))
	if held == "" {
		held = "no value"
	}
	return fmt.Sprintf("SignalK did not accept %s = %q: the delta was sent but the server's model holds %s", path, want, held)
}

// engineSilentStopper marks an engine stopped on the SignalK bus when
// Helmcentral judges it off (engineStates) while SignalK still says started
// (ADR 0292).
//
// Some engine gateways stop sending anything when the engine is switched off,
// so the last revolutions SignalK sees is idle RPM, never zero. The derived-data
// plugin only writes propulsion.<instance>.state = "stopped" when a revolutions
// update arrives at zero, so the state stays "started" and the autostate plugin
// reports motoring on any movement.
//
// This is an approved fallback (AGENTS.md, Exception Rule): it is behind an
// operator setting, off by default, and logs every time it acts. It writes the
// state, a conclusion, and never revolutions, which would be a measurement
// nobody took.
type engineSilentStopper struct {
	// enabled reads the operator's setting; an error is logged once and the
	// stopper does nothing until it reads.
	enabled func() (bool, error)
	// engines are the mapped engines. Only these instances are ever touched.
	engines func() ([]vesselEngineSetting, error)
	// read returns the live view of the self tree.
	read func() telemetryReader
	// states is the engine off/lost/running judgement, keyed by propulsion id
	// (which is the mapped engine's Instance). Nil while Helmcentral's own
	// stream is stalled.
	states func(now time.Time) map[string]engineStateInfo
	// publish writes "stopped" to one propulsion state path and confirms it landed.
	publish func(path string) error

	// running keeps the evaluator tick from starting a second check while the
	// previous one is still publishing.
	running atomic.Bool

	mu sync.Mutex
	// perEngine is keyed by instance. Touched only by check.
	perEngine map[string]*engineSilentState
	// lastError is the error text last logged per kind, so a persistent
	// failure is logged once and not every tick.
	lastError [errKinds]string
	// lastCheck is when tick last started a check, for the cadence.
	lastCheck time.Time

	// generation is the equipment-change signal the mapped-engine cache
	// follows.
	generation func() uint64
	// The cached mapped engines, when and under which generation they were
	// read. Touched only by check.
	cachedEngines []vesselEngineSetting
	cachedAt      time.Time
	cachedGen     uint64
}

// engineSilentState is what the stopper remembers about one engine.
type engineSilentState struct {
	// retry is the backoff of a failed publish.
	retry saveBackoff
	// missingLogged is true once the "no state path" line has been logged,
	// until the path next appears.
	missingLogged bool
}

func newEngineSilentStopper(settingsPath string) *engineSilentStopper {
	return &engineSilentStopper{
		enabled: markEnginesStoppedFromSettingsFile(settingsPath),
		engines: configuredEngineInstances(settingsPath),
		read:    func() telemetryReader { return globalSignalKSnapshot.liveSelfTelemetry(time.Now().UTC()) },
		states: func(now time.Time) map[string]engineStateInfo {
			return engineStatesFor(globalSignalKSnapshot, now)
		},
		publish:    publishEngineStopped,
		perEngine:  map[string]*engineSilentState{},
		generation: sourceItemsGeneration,
	}
}

// sourceItemsGeneration is the counter equipment edits bump
// (invalidateSourceItems).
func sourceItemsGeneration() uint64 {
	sourceItemsCache.mu.Lock()
	defer sourceItemsCache.mu.Unlock()
	return sourceItemsCache.generation
}

// markEnginesStoppedFromSettingsFile reads the operator's switch from
// settings.yaml on each call, so turning it on or off takes effect without a
// restart. It uses the shared parse and a direct map read: the alarm evaluator
// asks every second.
func markEnginesStoppedFromSettingsFile(settingsPath string) func() (bool, error) {
	return func() (bool, error) {
		settings, err := readSettingsShared(settingsPath)
		if err != nil {
			return false, err
		}
		engines, _ := settings["engines"].(map[string]any)
		on, _ := engines["mark_stopped_when_silent"].(bool)
		return on, nil
	}
}

// tick runs check on its own goroutine, unless the last one is still going. A
// publish waits on the SignalK server for seconds; the alarm evaluator must not.
// With the setting off it costs one cached map read and starts nothing.
func (s *engineSilentStopper) tick(now time.Time) {
	on, err := s.enabled()
	if err != nil {
		s.noteError(errSettings, err, "could not read the setting \""+engineSilentStopSettingLabel+"\"")
		return
	}
	s.noteError(errSettings, nil, "")
	if !s.running.CompareAndSwap(false, true) {
		return
	}
	if !on {
		// Turning the setting back on starts clean: no stale backoff or
		// logged-once flag from the last time it was on. running is held so a
		// check cannot be touching them meanwhile.
		clear(s.perEngine)
		s.noteError(errEngines, nil, "")
		s.mu.Lock()
		s.lastCheck = time.Time{}
		s.mu.Unlock()
		s.running.Store(false)
		return
	}
	s.mu.Lock()
	due := s.lastCheck.IsZero() || now.Sub(s.lastCheck) >= engineSilentCheckEvery
	if due {
		s.lastCheck = now
	}
	s.mu.Unlock()
	if !due {
		s.running.Store(false)
		return
	}
	go func() {
		defer s.running.Store(false)
		s.check(now)
	}()
}

// The kinds of error the stopper logs once each. Each keeps its own last text,
// so one kind recovering never re-arms another's log-once guard.
const (
	errSettings = iota
	errEngines
	errKinds
)

func (s *engineSilentStopper) noteError(kind int, err error, what string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		s.lastError[kind] = ""
		return
	}
	text := what + ": " + err.Error()
	if text != s.lastError[kind] {
		s.lastError[kind] = text
		log.Printf("engine state: %s", text)
	}
}

// check looks at the engine states once, with the setting already known to be
// on. It resolves the mapped engines and reads the bus only when some engine is
// judged off.
func (s *engineSilentStopper) check(now time.Time) {
	// A nil map means Helmcentral's own stream is stalled, so nothing is known:
	// write nothing until messages arrive again.
	states := s.states(now)
	if states == nil {
		return
	}

	// An engine no longer off and witnessed starts its next silence with a
	// fresh backoff, not the delay an earlier outage had grown.
	for instance, st := range s.perEngine {
		if !offAndWitnessed(states[instance]) {
			st.retry = saveBackoff{}
		}
	}

	anyOff := false
	for _, info := range states {
		if offAndWitnessed(info) {
			anyOff = true
			break
		}
	}
	if !anyOff {
		return
	}

	engines, err := s.mappedEngines(now)
	if err != nil {
		s.noteError(errEngines, err, "could not read the mapped engines")
		return
	}
	s.noteError(errEngines, nil, "")

	t := s.read()
	for _, engine := range engines {
		if info := states[engine.Instance]; engine.Instance != "" && offAndWitnessed(info) {
			s.checkEngine(t, engine, info, now)
		}
	}
}

// mappedEngines is the list of mapped engines, read at most every
// engineSilentEnginesTTL or when equipment is edited. The generation counter
// is the cheap change signal equipment saves already bump; an engine left off
// all winter would otherwise cost a store read every 15 s.
func (s *engineSilentStopper) mappedEngines(now time.Time) ([]vesselEngineSetting, error) {
	gen := s.generation()
	if !s.cachedAt.IsZero() && gen == s.cachedGen && now.Sub(s.cachedAt) < engineSilentEnginesTTL {
		return s.cachedEngines, nil
	}
	engines, err := s.engines()
	if err != nil {
		return nil, err
	}
	s.cachedEngines, s.cachedAt, s.cachedGen = engines, now, gen
	return engines, nil
}

// offAndWitnessed is the only judgement the stopper acts on. Off without a
// witness (no other device on the engine's connection) is also what a failed
// gateway looks like, and that must never be written over as a stop.
func offAndWitnessed(info engineStateInfo) bool {
	return info.State == engineStateOff && info.Witnessed
}

func (s *engineSilentStopper) checkEngine(t telemetryReader, engine vesselEngineSetting, info engineStateInfo, now time.Time) {
	prefix := "propulsion." + engine.Instance
	name := strings.TrimSpace(engine.Name)
	if name == "" {
		name = "engine " + engine.Instance
	}

	st := s.perEngine[engine.Instance]
	if st == nil {
		st = &engineSilentState{}
		s.perEngine[engine.Instance] = st
	}

	if _, present := t.Value(prefix + ".state"); !present {
		// Nothing to correct and nothing to write over: say so once, so an
		// engine that never gets marked is not a silent mystery.
		if !st.missingLogged {
			st.missingLogged = true
			log.Printf("engine state: %s has no %s.state on SignalK, so it can't be marked stopped (setting %q)", name, prefix, engineSilentStopSettingLabel)
		}
		return
	}
	st.missingLogged = false
	if state, _ := t.String(prefix + ".state"); state != "started" {
		// Stopped: nothing to correct, and the next "started" is a new case.
		st.retry = saveBackoff{}
		return
	}
	if !st.retry.due(now) {
		return
	}

	if err := s.publish(prefix + ".state"); err != nil {
		st.retry.fail(now, err, engineSilentRetryFirst, engineSilentRetryMax)
		log.Printf("engine state: could not mark %s stopped, trying again in %s: %v", name, st.retry.delay, err)
		return
	}
	st.retry = saveBackoff{}
	log.Printf("engine state: %s has been off for %ds (no RPM while its connection still sends other data) but SignalK says started; marked stopped (setting %q)", name, int(now.Sub(info.LastUpdate).Seconds()), engineSilentStopSettingLabel)
}

// publishEngineStopped writes propulsion.<instance>.state = "stopped" and reads
// it back from the server's model.
func publishEngineStopped(path string) error {
	const want = "stopped"
	return publishSignalKValue(path, want, func(ctx context.Context, base, token string) error {
		var held json.RawMessage
		return confirmSignalKPublish(ctx, path,
			func() (bool, error) {
				raw, _, err := signalKSelfLeaf(ctx, base, token, path)
				if err != nil {
					return false, err
				}
				held = raw
				return heldStringIs(raw, want), nil
			},
			func() string { return stringRejectedMessage(path, want, held) })
	})
}
