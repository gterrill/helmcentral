package main

import (
	"fmt"
	"log"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

/*
anomaly_stumble.go is the engine-stumble detector: an engine held at a steady
throttle whose rpm repeatedly dips for a second or two and recovers, which is
what an engine being starved of fuel looks like long before it quits.

It needs no vessel setup. Engines are discovered from
propulsion.<id>.revolutions, the way the sensor-health checks discover what the
boat publishes, and every threshold is relative to that engine's own steady
rpm, so there is no learned baseline to wait for either. The detector counts
dips in a trailing ten minutes and publishes the count as
helmcentral.anomaly.propulsion.<id>.stumbleCount; a seeded alarm rule watches
it like every other anomaly path.

State lives in engineStumbleTracker, owned by the anomaly detector goroutine
(anomalyTrackers.stumble), never in the derived-path computation.
*/

const (
	// stumbleWindowSamples is how many trailing rpm samples define "steady".
	stumbleWindowSamples = 10
	// stumbleMinSamples is how many of them are needed before a median means anything.
	stumbleMinSamples = 6
	// stumbleMaxSampleGap is the longest gap between two samples that still
	// counts as one continuous run; a longer one resets the window.
	stumbleMaxSampleGap = 3 * time.Second
	// stumbleMinSteadyRPM: below this (idle, shutdown) the engine is never judged.
	stumbleMinSteadyRPM = 800.0
	// The engine is steady when the window's spread (max-min) is within
	// the larger of the absolute and relative allowances.
	stumbleSteadySpreadRPM      = 15.0
	stumbleSteadySpreadFraction = 0.01
	// A dip is a sample this far under the window's median: the larger of
	// the absolute and relative depths.
	stumbleDipRPM      = 40.0
	stumbleDipFraction = 0.03
	// A dip counts only if rpm returns to this fraction of the median ...
	stumbleRecoverFraction = 0.985
	// ... within this long. No recovery means the operator moved the throttle.
	stumbleRecoverWithin = 6 * time.Second
	// A dip is discarded as sea state when another engine dipped within this
	// long of it, either side.
	stumbleSeaWindow = 3 * time.Second
	// stumbleSettleAfter is how long after a dip starts it is held back before
	// it can be counted: another engine's dip starting up to stumbleSeaWindow
	// later is only recognised once it has recovered, up to
	// stumbleRecoverWithin after that.
	stumbleSettleAfter = stumbleSeaWindow + stumbleRecoverWithin
	// stumbleCountWindow is the trailing period the published count covers.
	stumbleCountWindow = 10 * time.Minute

	// Alarm rule: fires at a count of 3, lets go when the trailing window is empty.
	stumbleAlarmCount      = 3
	stumbleAlarmThreshold  = 2.5 // count > 2.5, i.e. 3 or more
	stumbleAlarmHysteresis = 2.0 // clears at 0.5, i.e. when the count is back to 0
)

type stumbleSample struct {
	at  time.Time
	rpm float64
}

// stumbleEvent is one dip that fell and recovered. At is when it started.
type stumbleEvent struct {
	At        time.Time
	DepthRPM  float64
	SteadyRPM float64
}

type stumbleDip struct {
	start  time.Time
	median float64
	lowest float64
}

type stumbleEngine struct {
	window []stumbleSample
	dip    *stumbleDip
	last   time.Time
	events []stumbleEvent
}

// engineStumbleTracker holds every discovered engine's trailing samples and
// detected dips. Not safe for concurrent use: the anomaly goroutine owns it.
type engineStumbleTracker struct {
	engines map[string]*stumbleEngine
}

// engineStumbleSummary is one engine's published figures.
type engineStumbleSummary struct {
	Count int
	// DeepestRPM is how far below its steady rpm the worst counted dip fell,
	// and SteadyRPM the steady rpm it fell from.
	DeepestRPM, SteadyRPM float64
}

func newEngineStumbleTracker() *engineStumbleTracker {
	return &engineStumbleTracker{engines: map[string]*stumbleEngine{}}
}

func (e *stumbleEngine) reset() {
	e.window = e.window[:0]
	e.dip = nil
}

// observe feeds one rpm reading (SignalK revolutions, Hz) taken at at. Callers
// pass the reading's own timestamp, not the wall clock, and each distinct
// reading once. Out-of-order or repeated timestamps are ignored.
func (t *engineStumbleTracker) observe(id string, at time.Time, hz float64) {
	e := t.engines[id]
	if e == nil {
		e = &stumbleEngine{}
		t.engines[id] = e
	}
	if !e.last.IsZero() {
		if !at.After(e.last) {
			return
		}
		if at.Sub(e.last) > stumbleMaxSampleGap {
			e.reset()
		}
	}
	e.last = at
	rpm := hz * 60

	if e.dip != nil {
		d := e.dip
		if rpm < d.lowest {
			d.lowest = rpm
		}
		if at.Sub(d.start) > stumbleRecoverWithin {
			// Never came back in time: the operator changed the throttle.
			// Start over from here.
			e.reset()
			e.window = append(e.window, stumbleSample{at, rpm})
			return
		}
		if rpm >= d.median*stumbleRecoverFraction {
			e.events = append(e.events, stumbleEvent{At: d.start, DepthRPM: d.median - d.lowest, SteadyRPM: d.median})
			e.reset()
			e.window = append(e.window, stumbleSample{at, rpm})
		}
		return
	}

	if len(e.window) >= stumbleMinSamples {
		rpms := make([]float64, len(e.window))
		lo, hi := math.Inf(1), math.Inf(-1)
		for i, s := range e.window {
			rpms[i] = s.rpm
			lo, hi = math.Min(lo, s.rpm), math.Max(hi, s.rpm)
		}
		med := median(rpms)
		steady := med >= stumbleMinSteadyRPM && hi-lo <= math.Max(stumbleSteadySpreadRPM, stumbleSteadySpreadFraction*med)
		if steady && rpm <= med-math.Max(stumbleDipRPM, stumbleDipFraction*med) {
			e.dip = &stumbleDip{start: at, median: med, lowest: rpm}
			return
		}
	}

	e.window = append(e.window, stumbleSample{at, rpm})
	if len(e.window) > stumbleWindowSamples {
		e.window = e.window[len(e.window)-stumbleWindowSamples:]
	}
}

// ids lists the engines the tracker has seen, sorted.
func (t *engineStumbleTracker) ids() []string {
	out := make([]string, 0, len(t.engines))
	for id := range t.engines {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// summary reports, for every engine seen, the dips counted in the trailing
// stumbleCountWindow as of now. A dip is held back until stumbleSettleAfter
// has passed so that every other engine's dips around it are known, then
// discarded if another engine dipped within stumbleSeaWindow of it: both
// props unloading on the same wave is the sea, not the engine. A boat with one
// engine has nothing to compare against and so no suppression.
func (t *engineStumbleTracker) summary(now time.Time) map[string]engineStumbleSummary {
	out := make(map[string]engineStumbleSummary, len(t.engines))
	for id, e := range t.engines {
		// Dips older than the count window can never count again.
		keep := e.events[:0]
		for _, ev := range e.events {
			if now.Sub(ev.At) <= stumbleCountWindow+stumbleSettleAfter {
				keep = append(keep, ev)
			}
		}
		e.events = keep

		var s engineStumbleSummary
		for _, ev := range e.events {
			age := now.Sub(ev.At)
			if age < stumbleSettleAfter || age > stumbleCountWindow {
				continue
			}
			if t.seaStumble(id, ev.At) {
				continue
			}
			s.Count++
			if ev.DepthRPM > s.DeepestRPM {
				s.DeepestRPM, s.SteadyRPM = ev.DepthRPM, ev.SteadyRPM
			}
		}
		out[id] = s
	}
	return out
}

func (t *engineStumbleTracker) seaStumble(id string, at time.Time) bool {
	for other, e := range t.engines {
		if other == id {
			continue
		}
		for _, ev := range e.events {
			if d := ev.At.Sub(at); d >= -stumbleSeaWindow && d <= stumbleSeaWindow {
				return true
			}
		}
	}
	return false
}

// --- Paths, evidence, wording -------------------------------------------------

var propulsionRevolutionsRe = regexp.MustCompile(`^propulsion\.([^.]+)\.revolutions$`)

// anomalyEngineStumbleCountPath is helmcentral.anomaly.propulsion.<instance>.stumbleCount,
// keyed on the SignalK instance id, never the operator's display name.
func anomalyEngineStumbleCountPath(instance string) string {
	return derivedPathPrefix + "anomaly.propulsion." + instance + ".stumbleCount"
}

func formatStumbleEvidence(s engineStumbleSummary) string {
	noun := "dips"
	if s.Count == 1 {
		noun = "dip"
	}
	return fmt.Sprintf("%d RPM %s in 10 min · up to %d rpm below %d rpm",
		s.Count, noun, int(math.Round(s.DeepestRPM)), int(math.Round(s.SteadyRPM)))
}

// stumbleRuleTitle is the alarm title: the event, in the operator's name for
// the engine. With no name set it falls back to the instance id made
// readable ("port" -> "Port"), and a bare number to "Engine 0".
func stumbleRuleTitle(name, instance string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = strings.TrimSpace(instance)
		if name != "" && strings.IndexFunc(name, func(r rune) bool { return !unicode.IsDigit(r) }) < 0 {
			return "Engine " + name + " RPM Dips"
		}
		if name != "" {
			r := []rune(name)
			r[0] = unicode.ToUpper(r[0])
			name = string(r)
		}
	}
	if name == "" {
		return "Engine RPM Dips"
	}
	if strings.HasSuffix(strings.ToLower(name), "engine") {
		return name + " RPM Dips"
	}
	return name + " Engine RPM Dips"
}

// --- Tick wiring ----------------------------------------------------------------

// computeAnomalyEngineStumble feeds every newly-reported rpm reading of every
// discovered engine to the tracker and publishes each engine's trailing
// count and evidence. The count is a fact about the last ten minutes, so it
// keeps publishing (and decays to 0) after an engine stops reporting rather
// than going absent, which would hold a raised alarm up indefinitely.
// engines supplies display names only; ignored is the operator's ignore list.
func computeAnomalyEngineStumble(snapshot *signalKSnapshot, engines []vesselEngineSetting, ignored map[string]bool, trackers *anomalyTrackers, now time.Time, reading *anomalyReading) {
	self := snapshot.selfContext()
	for _, path := range discoveredEngineBatteryPaths(snapshot) {
		m := propulsionRevolutionsRe.FindStringSubmatch(path)
		if m == nil || ignored[path] {
			continue
		}
		id := m[1]
		if _, known := trackers.stumbleNames[id]; !known {
			trackers.stumbleNames[id] = ""
		}
		seen := snapshot.lastSeen(self, path)
		if seen.IsZero() || seen.Equal(trackers.stumbleSeen[id]) || !inputValidity(snapshot, path, now) {
			continue
		}
		hz, ok := numericFromPath(snapshot, path)
		if !ok {
			continue
		}
		trackers.stumbleSeen[id] = seen
		trackers.stumble.observe(id, seen, hz)
	}
	for _, e := range engines {
		if _, known := trackers.stumbleNames[e.Instance]; known {
			trackers.stumbleNames[e.Instance] = strings.TrimSpace(e.Name)
		}
	}

	for id, s := range trackers.stumble.summary(now) {
		path := anomalyEngineStumbleCountPath(id)
		reading.Values[path] = float64(s.Count)
		if s.Count > 0 {
			reading.Evidence[path] = formatStumbleEvidence(s)
		}
	}
}

// --- Seeding --------------------------------------------------------------------

// anomalyEngineStumbleSeedMarker is keyed on the SignalK instance id for the
// same reason anomalyEngineSeedMarker is: a rename must not look like a new
// engine.
func anomalyEngineStumbleSeedMarker(instance string) string {
	return "anomaly-v1:stumble:" + strings.ToLower(strings.TrimSpace(instance))
}

func anomalyEngineStumbleRuleID(instance string) string {
	return "helmcentral:anomaly-engine-stumble-" + strings.ToLower(strings.TrimSpace(instance))
}

// anomalyEngineStumbleSeedRules is the one rule for an engine. It ships
// enabled, unlike the twin residuals: the measure is relative to the
// engine's own steady rpm, so there is no baseline to learn first.
func anomalyEngineStumbleSeedRules(instance, name string) []alarmRule {
	return []alarmRule{{
		ID: anomalyEngineStumbleRuleID(instance), Label: stumbleRuleTitle(name, instance), Enabled: true,
		Path: anomalyEngineStumbleCountPath(instance), Op: alarmOpAbove,
		Value: stumbleAlarmThreshold, Hysteresis: stumbleAlarmHysteresis,
		DwellSeconds: 0, State: alarmStateWarn,
	}}
}

func seedEngineStumbleRule(instance, name string) error {
	return seedAnomalySet(anomalyEngineStumbleSeedMarker(instance), anomalyEngineStumbleSeedRules(instance, name))
}

// stumbleSeedRetryEvery spaces out retries of a failed seed so a persistent
// disk error logs once a minute, not once a second.
const stumbleSeedRetryEvery = time.Minute

// seedDiscoveredStumbleRules gives every engine the detector has discovered
// its rule, once. An engine that first reports later gets its rule then.
// Called from the detector goroutine after each tick.
func seedDiscoveredStumbleRules(trackers *anomalyTrackers, now time.Time) {
	for id, name := range trackers.stumbleNames {
		if trackers.stumbleSeeded[id] {
			continue
		}
		if last, ok := trackers.stumbleSeedTry[id]; ok && now.Sub(last) < stumbleSeedRetryEvery {
			continue
		}
		trackers.stumbleSeedTry[id] = now
		if err := seedEngineStumbleRule(id, name); err != nil {
			log.Printf("anomaly detector: could not seed the engine stumble alarm rule for %q: %v", id, err)
			continue
		}
		trackers.stumbleSeeded[id] = true
	}
}
