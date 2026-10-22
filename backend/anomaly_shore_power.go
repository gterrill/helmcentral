package main

import (
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

// acSourceEntry is one source's reading of one acSource path.
type acSourceEntry struct {
	OnBattery bool
	// Changed is when the reading last changed (the declared timestamp of
	// the update that changed it), not when it was last repeated. Zero for a
	// first sighting: a restart cannot know when an earlier switch happened.
	Changed time.Time
}

// acSourceState is the vessel's AC input as silentSources reads it.
type acSourceState struct {
	// BatteryAt is when the AC source last changed to battery; zero unless it
	// reads battery now.
	BatteryAt time.Time
	// Publishers is every source that publishes an acSource path. Such a
	// source is the evidence for this rule, so it is never excused by it.
	Publishers map[string]bool
}

// isACSourcePath reports whether path is an electrical.*.acSource or
// acSourceNumber leaf.
func isACSourcePath(path string) bool {
	return strings.HasPrefix(path, "electrical.") &&
		(strings.HasSuffix(path, ".acSource") || strings.HasSuffix(path, ".acSourceNumber"))
}

// acSourceIsBattery reads an acSource value: the string "battery" or the Venus
// number 240.
func acSourceIsBattery(v any) bool {
	switch x := v.(type) {
	case string:
		return strings.EqualFold(strings.TrimSpace(x), "battery")
	case float64:
		return x == venusACSourceBattery
	case int:
		return x == venusACSourceBattery
	}
	return false
}

// noteACSourceLocked records one acSource reading. Callers hold s.mu.
func (s *signalKSnapshot) noteACSourceLocked(context, source, path string, value any, at time.Time) {
	if s.selfCtx == "" || context != s.selfCtx {
		return
	}
	key := context + "|" + source + "|" + path
	onBattery := acSourceIsBattery(value)
	prev, known := s.acSourceSeen[key]
	if !known {
		if len(s.acSourceSeen) >= signalKACSourceMaxTracked {
			return
		}
		s.acSourceSeen[key] = acSourceEntry{OnBattery: onBattery}
		return
	}
	if prev.OnBattery == onBattery {
		return
	}
	s.acSourceSeen[key] = acSourceEntry{OnBattery: onBattery, Changed: at}
}

// acSourceFor reports the AC input state for context. The vessel reads
// battery when the most recent change among its acSource readings was to
// battery; a tie goes to the reading that is not battery.
func (s *signalKSnapshot) acSourceFor(context string) acSourceState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state := acSourceState{Publishers: map[string]bool{}}
	prefix := context + "|"
	var latest acSourceEntry
	for key, e := range s.acSourceSeen {
		rest, ok := strings.CutPrefix(key, prefix)
		if !ok {
			continue
		}
		source, _, _ := strings.Cut(rest, "|")
		state.Publishers[source] = true
		if e.Changed.After(latest.Changed) || (e.Changed.Equal(latest.Changed) && !e.OnBattery) {
			latest = e
		}
	}
	if latest.OnBattery {
		state.BatteryAt = latest.Changed
	}
	return state
}

// excusesQuietAt reports whether a source whose last update was at last went
// quiet with the shore power: the vessel now reads battery and the source's
// last update falls within silentSourceKeyOffWindow of the switch.
func (a acSourceState) excusesQuietAt(source string, last time.Time) bool {
	if a.BatteryAt.IsZero() || a.Publishers[source] {
		return false
	}
	d := last.Sub(a.BatteryAt)
	return d >= -silentSourceKeyOffWindow && d <= silentSourceKeyOffWindow
}
