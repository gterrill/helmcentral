package main

import "time"

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

	byEngine := map[string][]sourceHealth{}
	for _, s := range sources {
		for _, id := range s.EngineIDs {
			byEngine[id] = append(byEngine[id], s)
		}
	}

	out := make(map[string]engineStateInfo, len(byEngine))
	for id, group := range byEngine {
		newest := group[0]
		running := false
		for _, s := range group {
			if now.Sub(s.Last) <= sourceQuietThreshold(s) {
				running = true
			}
			if s.Last.After(newest.Last) {
				newest = s
			}
		}
		switch {
		case running:
			out[id] = engineStateInfo{State: engineStateRunning}
		case engineKeyOff(newest, sources, now):
			out[id] = engineStateInfo{State: engineStateOff, LastUpdate: newest.Last}
		default:
			out[id] = engineStateInfo{State: engineStateLost, LastUpdate: newest.Last}
		}
	}
	return out
}

// engineStatesPayload is engineStates over the live snapshot, shaped for the
// gauge-values event. Nil when there is nothing to say.
func engineStatesPayload(snapshot *signalKSnapshot, now time.Time) map[string]engineStateJSON {
	health := sourceHealthFor(snapshot, snapshot.selfContext())
	_, lastMessage := snapshot.status()
	states := engineStates(health, now, now.Sub(lastMessage))
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
