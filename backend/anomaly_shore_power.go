package main

import (
	"log"
	"slices"
	"strings"
	"time"
)

// anomaly_shore_power.go tracks the vessel's AC input source so the
// silent-source check can tell a shore charger that lost its power when shore
// AC was disconnected from one that failed. Any source publishing an
// electrical.*.acSource (name) or electrical.*.acSourceNumber (Venus number)
// counts; no source id is hardcoded.

// venusACSourceBattery is Venus's ActiveIn/Source number for running from the
// battery (inverting): 0 not available, 1 grid, 2 generator, 3 shore.
const venusACSourceBattery = 240

// signalKACSourceMaxTracked bounds acSourceSeen so a hostile stream inventing
// sources or paths cannot grow it without limit.
const signalKACSourceMaxTracked = 32

// acSourceKey is one source's one acSource path. Only the self vessel is
// tracked, so no context is part of the key.
type acSourceKey struct{ source, path string }

// acSourceEntry is one source's reading of one acSource path.
type acSourceEntry struct {
	OnBattery bool
	// Changed is when the reading last changed (the declared timestamp of
	// the update that changed it), not when it was last repeated. Zero for a
	// first sighting: a restart or reconnect cannot know when an earlier
	// switch happened.
	Changed time.Time
}

// acSourceState is the vessel's AC input as silentSources reads it.
type acSourceState struct {
	// BatteryAt is when the AC source last changed to battery; zero unless it
	// reads battery now.
	BatteryAt time.Time
	// Sources are the sources publishing an acSource path. They are the
	// evidence for this rule and are never excused by it. When one of them is
	// itself quiet, nothing on its connection is excused either: a GX crash
	// just after the unplugging must still report every service behind it.
	Sources []string
}

// isACSourcePath reports whether path is an electrical.*.acSource or
// acSourceNumber leaf.
func isACSourcePath(path string) bool {
	return strings.HasPrefix(path, "electrical.") &&
		(strings.HasSuffix(path, ".acSource") || strings.HasSuffix(path, ".acSourceNumber"))
}

// acSourceReading classifies an acSource value. known is false for anything
// that is neither battery nor a recognised off-battery source (null, "NA",
// 0 not available, unrecognised): such a value says nothing and is ignored.
func acSourceReading(v any) (onBattery, known bool) {
	switch x := v.(type) {
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "battery":
			return true, true
		case "grid", "shore", "generator":
			return false, true
		}
	case float64:
		switch x {
		case venusACSourceBattery:
			return true, true
		case 1, 2, 3:
			return false, true
		}
	}
	return false, false
}

// noteACSourceLocked records one acSource reading. Callers hold s.mu.
func (s *signalKSnapshot) noteACSourceLocked(context, source, path string, value any, at time.Time) {
	if s.selfCtx == "" || context != s.selfCtx {
		return
	}
	onBattery, known := acSourceReading(value)
	if !known {
		return
	}
	key := acSourceKey{source, path}
	prev, seen := s.acSourceSeen[key]
	if !seen {
		if len(s.acSourceSeen) >= signalKACSourceMaxTracked {
			log.Printf("signalk snapshot: not tracking AC source %s from %s: %d readings already tracked", path, source, signalKACSourceMaxTracked)
			return
		}
		s.acSourceSeen[key] = acSourceEntry{OnBattery: onBattery}
		return
	}
	// An older reading arriving late says nothing about now.
	if at.Before(prev.Changed) || prev.OnBattery == onBattery {
		return
	}
	s.acSourceSeen[key] = acSourceEntry{OnBattery: onBattery, Changed: at}
}

// acSourceLocked reports the self vessel's AC input state. The vessel reads
// battery when the most recent change among its readings was to battery; a
// tie goes to the reading that is not battery. Callers hold s.mu.
func (s *signalKSnapshot) acSourceLocked() acSourceState {
	var state acSourceState
	var latest acSourceEntry
	for key, e := range s.acSourceSeen {
		if !slices.Contains(state.Sources, key.source) {
			state.Sources = append(state.Sources, key.source)
		}
		if e.Changed.After(latest.Changed) || (e.Changed.Equal(latest.Changed) && !e.OnBattery) {
			latest = e
		}
	}
	if latest.OnBattery {
		state.BatteryAt = latest.Changed
	}
	return state
}

// acSourceFor is acSourceLocked under the read lock.
func (s *signalKSnapshot) acSourceFor() acSourceState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.acSourceLocked()
}

// publishes reports whether source is one of those publishing an AC source.
func (a acSourceState) publishes(source string) bool {
	return slices.Contains(a.Sources, source)
}

// withinKeyOffWindow reports whether last falls within silentSourceKeyOffWindow
// of off, the moment something it depends on went down.
func withinKeyOffWindow(last, off time.Time) bool {
	d := last.Sub(off)
	return d >= -silentSourceKeyOffWindow && d <= silentSourceKeyOffWindow
}
