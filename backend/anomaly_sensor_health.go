package main

import (
	"regexp"
	"sort"
	"time"
)

// anomaly_sensor_health.go implements the "sensor health" anomaly detector:
// a physical-limits range check, a frozen/stuck-sensor check correlated
// against engine rpm, and a silent-$source check, plus inputValidity, the
// single lookup every other detector (battery, engine differentials) uses to
// decide whether it trusts a reading before publishing anything from it. See
// the anomaly-detection plan for full cycle context.
//
// This file's three checks are deliberately pure functions taking a
// precomputed window/summary, not raw time-series accumulators: the caller
// (the anomaly detector goroutine, anomaly_detector.go) owns the actual
// 15-minute and 5-minute rolling windows, because computeDerivedPathsFromTree
// is recomputed concurrently by several readers and cannot itself hold that
// state (the same reasoning anomaly_battery.go's fullBankChargingLevel
// already applies to dV/dt).

// --- Range: a physical-limits sanity table, not an alarm level -------------

// rangeVerdict is classifyRange's result.
type rangeVerdict int

const (
	// rangeNotApplicable means path's quantity has no entry in
	// physicalLimits -- classifyRange makes no claim about it either way,
	// rather than guessing a range for a quantity it was never taught.
	rangeNotApplicable rangeVerdict = iota
	rangeValid
	rangeOutOfRange
)

// physicalLimit is a SI-unit sanity range: values outside it are not a
// plausible physical reading for that quantity on any vessel, not merely an
// alarm-worthy one. A dead or miswired sensor commonly reports 0, a negative
// number, or a wildly implausible value (400 V on a 12/24/48 V house bank);
// a genuinely alarming-but-real reading (a 60 psi drop in oil pressure)
// stays well inside these bounds and is caught by the alarm rules, not here.
type physicalLimit struct{ min, max float64 }

// physicalLimits keys on the path suffix after its "propulsion.<id>." or
// "electrical.batteries.<id>." instance segment -- see engineOrBatterySuffix.
// "transmission.oilPressure" and "oilPressure" are separate entries because a
// gear/transmission oil circuit runs at a much higher working pressure than
// an engine's own oil gallery (Pikorua's own transmission.oilPressure
// readings top 2.3 MPa -- see anomaly_sensor_health_test.go's fixture
// assertion); a shared limit would either miss a dead engine oil sender
// reading 0 or flag a perfectly healthy transmission.
var physicalLimits = map[string]physicalLimit{
	"revolutions":                 {0, 105},     // Hz: 0-6300 rpm
	"temperature":                 {250, 410},   // K: -23 to 137 degC, coolant
	"oilPressure":                 {0, 1200000}, // Pa: engine oil gallery, 0-1200 kPa
	"boostPressure":               {0, 400000},  // Pa: 0-400 kPa gauge
	"engineLoad":                  {0, 1.05},    // ratio, 0-100% with a little sensor headroom
	"transmission.oilPressure":    {0, 3500000}, // Pa: gear oil runs much higher, 0-3500 kPa
	"transmission.oilTemperature": {250, 410},   // K
	"voltage":                     {0, 70},      // V: covers 12/24/48V systems with margin
	"current":                     {-800, 800},  // A: bank shunt, either direction
	"capacity.stateOfCharge":      {0, 1.05},    // ratio
}

var propulsionQuantityRe = regexp.MustCompile(`^propulsion\.[^.]+\.(.+)$`)
var batteryQuantityRe = regexp.MustCompile(`^electrical\.batteries\.[^.]+\.(.+)$`)

// engineOrBatterySuffix strips a path's "propulsion.<id>." or
// "electrical.batteries.<id>." instance segment and reports the remaining
// suffix, only when that suffix is one physicalLimits actually knows.
func engineOrBatterySuffix(path string) (string, bool) {
	if m := propulsionQuantityRe.FindStringSubmatch(path); m != nil {
		if _, ok := physicalLimits[m[1]]; ok {
			return m[1], true
		}
		return "", false
	}
	if m := batteryQuantityRe.FindStringSubmatch(path); m != nil {
		if _, ok := physicalLimits[m[1]]; ok {
			return m[1], true
		}
		return "", false
	}
	return "", false
}

// classifyRange reports whether value is a physically plausible reading for
// path's quantity. rangeNotApplicable for any path outside the engine/battery
// quantities physicalLimits covers -- this is a sanity check on a known
// domain, not a general-purpose SignalK validator.
func classifyRange(path string, value float64) rangeVerdict {
	suffix, ok := engineOrBatterySuffix(path)
	if !ok {
		return rangeNotApplicable
	}
	limit := physicalLimits[suffix]
	if value < limit.min || value > limit.max {
		return rangeOutOfRange
	}
	return rangeValid
}

// --- Frozen: a stuck sensor, correlated against engine rpm ------------------

// frozenWindowSpan is how long a path must be observed before frozenVerdict
// will consider it, matching the plan's "over 15 min".
const frozenWindowSpan = 15 * time.Minute

// frozenMinUpdates is the plan's "at least 60 updates" over frozenWindowSpan.
const frozenMinUpdates = 60

// frozenMinEngineHz is "every configured engine stayed at or above 600 rpm",
// converted to SignalK's own Hz (1 Hz = 60 rpm).
const frozenMinEngineHz = 600.0 / 60.0

// frozenMinRPMSwingHz is "rpm varied by at least 300 rpm" over the window.
const frozenMinRPMSwingHz = 300.0 / 60.0

// frozenWindow is the trailing-window summary frozenVerdict judges, computed
// by the detector goroutine's own per-tick bookkeeping (this file does no
// windowing itself -- see the file doc comment).
type frozenWindow struct {
	// UpdateCount is how many distinct updates the path received across the
	// window.
	UpdateCount int
	// ValueChanged is true if the path's raw value changed at all across
	// the window.
	ValueChanged bool
	// StillUpdating is false once the path itself has gone quiet (its most
	// recent update is no longer recent) -- distinct from ValueChanged,
	// which only tells a live-but-stuck sensor from a live, changing one.
	StillUpdating bool
	// MinEngineHz and MaxEngineHz are the lowest and highest rpm (in Hz)
	// seen across every configured, included engine over the window.
	MinEngineHz, MaxEngineHz float64
}

// frozenVerdict reports whether path looks like a frozen/stuck sensor: it
// kept updating, its value never moved, and every configured engine was
// worked hard enough over the same window (steady at or above 600 rpm, with
// at least a 300 rpm swing) that a live sensor should have moved too.
// excluded lets the operator drop a path (Pikorua's dead exhaust senders,
// via the alarm card's "ignore this sensor" action) from consideration
// entirely.
func frozenVerdict(path string, w frozenWindow, excluded map[string]bool) bool {
	if excluded[path] {
		return false
	}
	if !w.StillUpdating {
		return false
	}
	if w.UpdateCount < frozenMinUpdates {
		return false
	}
	if w.ValueChanged {
		return false
	}
	if w.MinEngineHz < frozenMinEngineHz {
		return false
	}
	if w.MaxEngineHz-w.MinEngineHz < frozenMinRPMSwingHz {
		return false
	}
	return true
}

// --- Silent source: a $source that stopped publishing -----------------------

// silentSourceWatchAfter and silentSourceMinUpdates are the plan's "a source
// counts as watched after 5 min and 30 updates" -- both must hold before an
// absence of updates from it means anything (a source that has never
// established a cadence yet has nothing to go silent from).
const silentSourceWatchAfter = 5 * time.Minute
const silentSourceMinUpdates = 30

// silentSourceQuietFor is "quiet for more than 120 s" -- the FLOOR of the
// per-source threshold silentSources actually applies, not a flat number
// for every source. See silentSourceCadenceMultiple.
const silentSourceQuietFor = 120 * time.Second

// silentSourceCadenceMultiple scales the flat 120s floor up for a source
// whose own observed reporting cadence is naturally slower than that --
// e.g. a switch or alarm state that only publishes on change, not a
// periodic sensor. Without this, such a source trips "silent" on its own
// ordinary behaviour the moment 120s passes between two genuinely healthy
// updates (code review finding 8). 10x is generous enough that one cycle
// running a bit long does not itself look like the source going dark,
// while a source that has gone truly quiet -- more than ten of its own
// typical gaps -- still fires well before the operator would otherwise
// notice on their own.
const silentSourceCadenceMultiple = 10

// silentSourceStreamMaxAge is "while the stream is under 10 s old": a dead
// SignalK connection is a different, already-alarmed failure (every source
// goes quiet at once), not this one source going quiet on its own.
const silentSourceStreamMaxAge = 10 * time.Second

// silentSourceBurstSpread is the least First-to-Last spread a source's
// observed history needs before its Count is trusted to represent a genuine
// publishing cadence, rather than a single burst of retained/cached values
// SignalK replays within milliseconds of a connection being established
// (confirmed against the real capture
// backend/testdata/anomaly/signalk-deltas-2026-09-25.ndjson, whose
// venus.com.victronenergy.vebus.276 source lands more than 20 update blocks
// inside 30ms at connect time). Without this, a burst-then-quiet source
// reads First and Last as both the connection instant, so
// (Last-First)/(Count-1) comes out near zero no matter how large Count is,
// flooring the scaled threshold below back down to the flat
// silentSourceQuietFor and reporting the source silent the moment
// silentSourceWatchAfter elapses -- 5 minutes after every restart,
// regardless of whether the source is actually healthy (code review finding
// 7). 2s is comfortably above any realistic burst's own span and
// comfortably below any cadence this codebase treats as meaningful.
const silentSourceBurstSpread = 2 * time.Second

// sourceHealth is one $source's publishing history as of "now" -- the same
// fields sourceSeenEntry tracks, decoupled so silentSources can be
// unit-tested without a live snapshot.
type sourceHealth struct {
	Source string
	First  time.Time
	Last   time.Time
	Count  int
}

// silentSources reports which of sources have gone silent: watched (per
// silentSourceWatchAfter/-MinUpdates/-BurstSpread) and quiet for over this
// source's own threshold, evaluated as of now. That threshold is silentSourceQuietFor's
// 120s floor, scaled up to silentSourceCadenceMultiple times the source's
// own observed average gap (First-to-Last spread divided by update count)
// when that is slower -- a source watched long enough to judge at all has
// at least silentSourceMinUpdates updates, so this average is never
// computed from too few samples to mean anything. streamAge is the overall
// snapshot's own staleness (now minus its last received message, any
// source); when streamAge itself exceeds silentSourceStreamMaxAge, nothing
// fires -- the whole feed is down, not this one source. excluded drops
// engine-bound sources the frozen check already accounts for, or any source
// the operator has ignored.
//
// The result is sorted by source id, so it is deterministic for a golden-text
// caller the way anomaly_battery.go's evidence formatter already is.
func silentSources(sources []sourceHealth, now time.Time, streamAge time.Duration, excluded map[string]bool) []string {
	if streamAge > silentSourceStreamMaxAge {
		return nil
	}

	var out []string
	for _, s := range sources {
		if excluded[s.Source] {
			continue
		}
		watched := s.Count >= silentSourceMinUpdates &&
			now.Sub(s.First) >= silentSourceWatchAfter &&
			s.Last.Sub(s.First) >= silentSourceBurstSpread
		if !watched {
			continue
		}

		threshold := silentSourceQuietFor
		if avgGap := s.Last.Sub(s.First) / time.Duration(s.Count-1); avgGap*silentSourceCadenceMultiple > threshold {
			threshold = avgGap * silentSourceCadenceMultiple
		}

		if now.Sub(s.Last) > threshold {
			out = append(out, s.Source)
		}
	}

	sort.Strings(out)
	return out
}

// --- inputValidity: the single lookup every detector trusts -----------------

// inputValidityMaxAge is how old a path's own last update can be before
// inputValidity treats it as stale rather than merely a little behind --
// generous next to the ~1s tick most engine/battery N2K data actually
// arrives on, so a momentary gap does not itself blank out every detector
// reading it.
const inputValidityMaxAge = 30 * time.Second

// inputValidity reports whether path currently holds a trustworthy reading:
// present, not stale, and physically in range (classifyRange). This is the
// snapshot-level half of the plan's "absent, stale, out-of-range, frozen or
// silent-source" list; the frozen and silent-source halves are windowed
// state the detector goroutine computes once per tick (frozenVerdict,
// silentSources) and folds into the same per-tick validity map this feeds --
// see anomaly_detector.go. A detector with an invalid input publishes
// nothing, exactly like fullBankChargingLevel's own valid func.
func inputValidity(snapshot *signalKSnapshot, path string, now time.Time) bool {
	self := snapshot.selfContext()
	if self == "" {
		return false
	}

	node := snapshot.nodeAt(path)
	if node == nil {
		return false
	}

	value, ok := numericNodeValue(node)
	if !ok {
		return false
	}

	if classifyRange(path, value) == rangeOutOfRange {
		return false
	}

	seen := snapshot.lastSeen(self, path)
	if seen.IsZero() || now.Sub(seen) > inputValidityMaxAge {
		return false
	}

	return true
}

// numericNodeValue extracts a float64 from a snapshot node's "value" key,
// the same shape applyDelta builds every leaf into.
func numericNodeValue(node map[string]any) (float64, bool) {
	raw, ok := node["value"]
	if !ok {
		return 0, false
	}
	v, ok := raw.(float64)
	return v, ok
}
