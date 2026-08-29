package main

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// radarTargetMaxAge is how long a target survives without a refresh.
//
// Since the ADR 0062 amendment that moved radar ingestion from mayara's own
// WebSocket to polling the mayara SignalK plugin's REST endpoint
// (radar_source.go), this no longer guards a broadcast cadence. Each poll
// carries the tracker's full current state, and replace already does
// full-state eviction on every call: a target absent from the response is
// gone immediately, with no ageing involved.
//
// What remains is a wall-clock backstop for the poll itself failing
// silently — no error, no fresh data, targets and their alarms frozen at
// whatever they last were. That is the 9cc5926 / ADR 0057 §6 lesson: a
// target that stops transmitting must not keep a CPA alarm ringing forever.
// 30s is ten missed 2s polls (radarPollInterval, radar_source.go) — long
// enough that a couple of dropped polls change nothing, short enough that a
// poll that is actually stuck is caught quickly rather than left stale for
// minutes.
const radarTargetMaxAge = 30 * time.Second

// radarTargetStore holds the current set of tracked ARPA targets, keyed
// "<radarID>:<targetID>" (radarTargetKey, radar_targets.go). Thread-safe:
// radarPoller writes from its own goroutine while the SSE emitter and REST
// handler read from theirs.
type radarTargetStore struct {
	mu          sync.RWMutex
	targets     map[string]radarTarget
	connected   bool
	lastMessage time.Time
}

func newRadarTargetStore() *radarTargetStore {
	return &radarTargetStore{targets: make(map[string]radarTarget)}
}

// replace swaps the entire target set for one radar id. A poll response is
// the tracker's full current state, so a target absent from it is gone: it
// is dropped here rather than surviving until it ages out, since the ageing
// path (radarTargetMaxAge) exists for a failing poll, not for a target
// mayara has already stopped reporting.
//
// Entries carrying status == "lost" are dropped on the way in rather than
// stored: mayara's REST payload still reports that transitional status
// before a target disappears from the list entirely, and showing it as
// still tracked would be a stale contact wearing a live one's marker.
func (s *radarTargetStore) replace(radarID string, targets []radarTarget, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	prefix := radarID + ":"
	for key := range s.targets {
		if strings.HasPrefix(key, prefix) {
			delete(s.targets, key)
		}
	}

	for _, target := range targets {
		if target.Status == "lost" {
			continue
		}
		s.targets[radarTargetKey(radarID, target.TargetID)] = target
	}

	s.lastMessage = now
}

// list returns every target younger than radarTargetMaxAge, nearest first
// (mirroring the AIS nearby-vessels range ordering), with AgeSeconds
// recomputed against now rather than trusted from creation time. This is the
// lazy half of the wall-clock guard: filtering here means neither this nor
// sweep needs its own goroutine, since both callers already run on a timer.
func (s *radarTargetStore) list(now time.Time) []radarTarget {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]radarTarget, 0, len(s.targets))
	for _, target := range s.targets {
		age := now.Sub(target.Seen)
		if age > radarTargetMaxAge {
			continue
		}
		target.AgeSeconds = int(age.Seconds())
		result = append(result, target)
	}

	sort.Slice(result, func(i, j int) bool { return result[i].RangeM < result[j].RangeM })
	return result
}

// sweep deletes every target older than radarTargetMaxAge and reports how
// many it reclaimed. This is the wall-clock backstop and the one that
// actually implements the 9cc5926 / ADR 0057 §6 lesson: it is the only
// signal that catches the poll hanging or silently failing without ever
// surfacing an error, which would otherwise leave every entry frozen at its
// last value with its alarm still ringing. list already hides stale targets
// from readers; sweep is what frees the memory.
func (s *radarTargetStore) sweep(now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	evicted := 0
	for key, target := range s.targets {
		if now.Sub(target.Seen) > radarTargetMaxAge {
			delete(s.targets, key)
			evicted++
		}
	}
	return evicted
}

// setConnected records the poller's last-poll outcome. It deliberately does
// NOT touch s.targets: a single failed poll must not blank the map (ADR
// 0062). list's age filter clears stale targets on its own within
// radarTargetMaxAge; buildRadarTargetsPayload (radar_source.go) is what
// flips source to mayara-unreachable.
func (s *radarTargetStore) setConnected(connected bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connected = connected
}

// status reports the outcome of the last poll and when it landed, success or
// failure either way.
func (s *radarTargetStore) status() (connected bool, lastMessage time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.connected, s.lastMessage
}
