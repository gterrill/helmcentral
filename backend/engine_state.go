package main

import (
	"slices"
	"strings"
	"time"
)

// engine_state.go reads an engine source going quiet as the engine being
// switched off, for the Engine Cluster tile. Switching the key off powers the
// engine computer down, so its readings stop (the last rpm is idle, never 0).
// The decision is the silent-source check's own (engineKeyOff,
// anomaly_sensor_health.go), reused rather than restated: the tile and the
// alarm must never disagree about what a quiet engine means.

const (
	// engineStateRunning: an engine source for this engine is reporting.
	engineStateRunning = "running"
	// engineStateOff: every source is quiet and its connection is alive (or
	// carries nothing else), which reads as the key being turned off.
	engineStateOff = "off"
	// engineStateLost: every source is quiet and so is its connection, which
	// is a gateway failure, not a switch-off.
	engineStateLost = "lost"
)

// engineStateInfo is one engine's state. LastUpdate is the newest update from
// any of its sources, set only for off and lost: a running engine's last
// update is "now", and carrying it would change the gauge-values payload on
// every tick.
type engineStateInfo struct {
	State      string
	LastUpdate time.Time
}

// engineStateJSON is engineStateInfo as the gauge-values payload carries it.
type engineStateJSON struct {
	State      string `json:"state"`
	LastUpdate string `json:"last_update,omitempty"`
}

// engineStates reports the state of every engine some source is known to
// publish, keyed by propulsion id. An engine no source is known to publish
// has no entry, and with the stream itself down (streamAge over
// silentSourceStreamMaxAge) nothing is claimed at all: a dead feed is not an
// engine switching off.
//
// Unlike quietSources it does not require a source to be "watched" first: a
// backend restart replays a dead source's retained values as one update, and
// that engine is plainly off, not unknown.
func engineStates(sources []sourceHealth, now time.Time, streamAge time.Duration) map[string]engineStateInfo {
	if streamAge > silentSourceStreamMaxAge {
		return nil
	}

	// Sources in key order, so a tie on Last below resolves the same way on
	// every tick whatever order the caller built the slice in.
	ordered := slices.Clone(sources)
	slices.SortFunc(ordered, func(a, b sourceHealth) int { return strings.Compare(a.Source, b.Source) })

	type engineSource struct {
		health sourceHealth
		last   time.Time
	}
	byEngine := map[string][]engineSource{}
	for _, s := range ordered {
		for id, last := range s.EngineLast {
			byEngine[id] = append(byEngine[id], engineSource{s, last})
		}
	}

	out := make(map[string]engineStateInfo, len(byEngine))
	for id, group := range byEngine {
		// The engine's own newest reading, from whichever source carried it.
		// A fixed two minutes, not the source's own cadence: a source's
		// whole-history average gap includes key-off periods and replayed
		// timestamps and would delay OFF by minutes.
		newest := group[0]
		for _, g := range group[1:] {
			if g.last.After(newest.last) {
				newest = g
			}
		}
		switch {
		case now.Sub(newest.last) <= silentSourceQuietFor:
			out[id] = engineStateInfo{State: engineStateRunning}
		case engineKeyOff(newest.health, sources, now):
			out[id] = engineStateInfo{State: engineStateOff, LastUpdate: newest.last}
		default:
			out[id] = engineStateInfo{State: engineStateLost, LastUpdate: newest.last}
		}
	}
	return out
}

// engineStateFunc yields the engine states of one build, computed on first use
// and shared by every consumer in it.
type engineStateFunc func() map[string]engineStateInfo

// engineStatesFor is engineStates over the live snapshot.
func engineStatesFor(snapshot *signalKSnapshot, now time.Time) map[string]engineStateInfo {
	health := sourceHealthFor(snapshot, snapshot.selfContext())
	_, lastMessage := snapshot.status()
	return engineStates(health, now, now.Sub(lastMessage))
}

// memoEngineStates defers engineStatesFor until a consumer asks, then reuses
// the answer. Not safe for concurrent use: one build, one goroutine.
func memoEngineStates(snapshot *signalKSnapshot, now time.Time) engineStateFunc {
	var states map[string]engineStateInfo
	done := false
	return func() map[string]engineStateInfo {
		if !done {
			states, done = engineStatesFor(snapshot, now), true
		}
		return states
	}
}

// engineStatesPayload is engineStates over the live snapshot, shaped for the
// gauge-values event. Nil when there is nothing to say.
func engineStatesPayload(snapshot *signalKSnapshot, now time.Time) map[string]engineStateJSON {
	return engineStatesJSON(engineStatesFor(snapshot, now))
}

// engineStatesJSON shapes engine states for the gauge-values event.
func engineStatesJSON(states map[string]engineStateInfo) map[string]engineStateJSON {
	if len(states) == 0 {
		return nil
	}
	out := make(map[string]engineStateJSON, len(states))
	for id, info := range states {
		j := engineStateJSON{State: info.State}
		if !info.LastUpdate.IsZero() {
			j.LastUpdate = info.LastUpdate.UTC().Format(time.RFC3339)
		}
		out[id] = j
	}
	return out
}
