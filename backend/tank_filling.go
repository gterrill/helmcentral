package main

import (
	"math"
	"sort"
	"sync"
	"time"
)

// Tank filling detection (ADR 0282). The Tanks tile says FILLING while a
// fresh water or fuel tank is being filled, and how long until it is full.
//
// The tank sender is quantised: it reports in steps of two or three percent
// and holds each value a minute or two even while the tank fills. A short
// window slope would read that staircase as noise, so detection counts
// rising steps instead.
//
// The detector keeps no state between calls beyond a short buffer of level
// changes per tank. status recomputes the answer from that buffer every time,
// so nothing latches on and nothing has to be reset.

const (
	// A hose or nozzle: two rises this close, level at its high. Seven minutes
	// lets steps up to three and a half minutes apart keep two rises in view
	// right up to the next step.
	tankFillFastWindow = 7 * time.Minute
	// A slow watermaker: three rises this close, level at its high.
	tankFillSlowWindow = 30 * time.Minute
	// The slow rule wants the last rise this recent, and no more than twice the
	// typical gap between rises ago, whichever is shorter. A hose paused for a
	// few minutes is therefore not held long by it.
	tankFillSlowLastRise = 15 * time.Minute
	tankFillSlowGapShare = 2.0
	// Net gain never counts for less than this, however fine the sender's
	// resolution, so noise on a 0.1% sender cannot pass for a fill.
	tankFillMinGainPoints = 2.0
	// Rises closer than this are one real step reported as two values.
	tankFillMergeGap = 30 * time.Second
	// The sender's own resolution, as a floor under the step size learned
	// from the tank, so a flickering sender cannot teach a step of nothing.
	tankFillMinStepPercent = 0.5
	// A rise must be this fraction of the tank's median observed step.
	tankFillStepShare = 0.4
	// The net gain a fill must show, in learned steps.
	tankFillFastGainSteps = 2.0
	tankFillSlowGainSteps = 3.0
	// Pairs of rising points closer than this say nothing about the rate.
	tankFillMinBaseline = 60 * time.Second
	tankFillMinPoints   = 3
	// Under this many seconds from full the estimate is not worth showing.
	tankFillMinSecondsToFull = 60.0
	// Steady rate: the upper quartile of pairwise slopes within this factor
	// of the lower quartile.
	tankFillSteadyRatio = 3.0
	// The rate is fitted through the rises of the current climb, going back at
	// most this far.
	tankFillClimbSpan = 30 * time.Minute
	// A gap between rises longer than this is a pause; the rate restarts after it.
	tankFillClimbPause = 15 * time.Minute
	// The buffer holds the slow window plus margin.
	tankFillKeep       = tankFillSlowWindow + 5*time.Minute
	tankFillStepSample = 20
	// Stopped means under one knot over the ground.
	tankFillStoppedBelowKts = 1.0
	// A tank reading older than this has not been seen: a dead sender must
	// not keep a fill alive.
	tankFillMaxReadingAge = 60 * time.Second
)

type tankFillSample struct {
	at    time.Time
	level float64 // percent, after the change
}

type tankFillState struct {
	samples  []tankFillSample // every change in level, oldest first
	deltas   []float64        // sizes of the latest nonzero changes, for the learned step
	lastSeen time.Time        // the latest reading of this tank
	sogKts   float64          // as of the latest reading; negative when unknown
}

type tankFillTracker struct {
	mu    sync.Mutex
	tanks map[string]*tankFillState
}

func newTankFillTracker() *tankFillTracker {
	return &tankFillTracker{tanks: map[string]*tankFillState{}}
}

// tankFill is the live tracker, fed by sampleTracks on the poll tick.
var tankFill = newTankFillTracker()

// observe records one reading of a tank. levelPercent is 0..100. sogKts is
// the boat's speed over ground, negative when it is unknown or stale.
//
// Only water and fuel tanks are tracked. When SOG is unknown the tank is
// never reported as filling: a missing speed is not evidence the boat is
// stopped, and the tile must not claim a fill it cannot vouch for.
func (tr *tankFillTracker) observe(id, kind string, levelPercent, sogKts float64, now time.Time) {
	if kind != "water" && kind != "fuel" {
		return
	}
	tr.mu.Lock()
	defer tr.mu.Unlock()
	st := tr.tanks[id]
	if st == nil {
		st = &tankFillState{}
		tr.tanks[id] = st
	}
	st.lastSeen = now
	st.sogKts = sogKts
	if n := len(st.samples); n == 0 || st.samples[n-1].level != levelPercent {
		if n > 0 {
			st.deltas = append(st.deltas, math.Abs(levelPercent-st.samples[n-1].level))
			if len(st.deltas) > tankFillStepSample {
				st.deltas = st.deltas[len(st.deltas)-tankFillStepSample:]
			}
		}
		st.samples = append(st.samples, tankFillSample{at: now, level: levelPercent})
		st.trim(now)
	}
}

// trim drops samples older than tankFillKeep, keeping the newest one outside
// the window: it is the level the window opened at.
func (st *tankFillState) trim(now time.Time) {
	drop := 0
	for drop+1 < len(st.samples) && now.Sub(st.samples[drop+1].at) > tankFillKeep {
		drop++
	}
	if drop > 0 {
		st.samples = st.samples[:copy(st.samples, st.samples[drop:])]
	}
}

// status is what the Tanks payload carries for one tank.
func (tr *tankFillTracker) status(id string, now time.Time) (bool, *float64) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	st := tr.tanks[id]
	if st == nil || len(st.samples) == 0 {
		return false, nil
	}
	if st.sogKts < 0 || st.sogKts >= tankFillStoppedBelowKts || now.Sub(st.lastSeen) > tankFillMaxReadingAge {
		return false, nil
	}
	typical := median(st.deltas)
	floor := math.Max(tankFillMinStepPercent, tankFillStepShare*typical)
	step := math.Max(tankFillMinStepPercent, typical)

	if w := st.window(now, tankFillFastWindow).fromLowest(); !fastFill(w, floor, step) {
		if w := st.window(now, tankFillSlowWindow).fromLowest(); !slowFill(w, now, floor, step) {
			return false, nil
		}
	}
	// Filling is decided; the rate comes from the whole current climb.
	remaining := secondsToFull(sinceLastPause(st.window(now, tankFillClimbSpan).climb(floor).rises(floor)), now)
	if remaining == nil {
		return true, nil
	}
	// Whole minutes, so the payload does not change every poll.
	rounded := math.Round(*remaining/60) * 60
	return true, &rounded
}

// tankFillWindow is the level history from one window's opening to now: the
// level the window opened at, then every change inside it.
type tankFillWindow []tankFillSample

// window returns the samples that matter to a span ending at now.
func (st *tankFillState) window(now time.Time, span time.Duration) tankFillWindow {
	start := now.Add(-span)
	first := 0
	for i, s := range st.samples {
		if !s.at.After(start) {
			first = i // the newest sample at or before the window opening is its opening level
		}
	}
	return tankFillWindow(st.samples[first:])
}

type tankFillRise struct {
	at    time.Time
	level float64 // percent, after the rise
}

// rises lists the climbs of at least floor above the level of the last rise
// (the window's opening level before the first), so a level that dips and
// comes back is not a second rise. Rises less than tankFillMergeGap after the
// first rise of their group are one step reported as several values.
func (w tankFillWindow) rises(floor float64) []tankFillRise {
	if len(w) == 0 {
		return nil
	}
	var out []tankFillRise
	var groupStart time.Time
	ref := w[0].level
	for _, s := range w[1:] {
		if s.level-ref < floor {
			continue
		}
		ref = s.level
		if n := len(out); n > 0 && s.at.Sub(groupStart) < tankFillMergeGap {
			out[n-1] = tankFillRise{at: s.at, level: s.level}
			continue
		}
		groupStart = s.at
		out = append(out, tankFillRise{at: s.at, level: s.level})
	}
	return out
}

// climb is the part of the window since its lowest level or since the last
// step down of floor or more, whichever came later.
func (w tankFillWindow) climb(floor float64) tankFillWindow {
	start := 0
	for i, s := range w {
		if s.level <= w[start].level {
			start = i
		}
		if i > 0 && w[i-1].level-s.level >= floor {
			start = i
		}
	}
	return w[start:]
}

// fromLowest is the part of the window since its lowest level (the latest, if
// the level touched it more than once). A fill is a climb from there; a dip
// and a recovery above it adds no rise, because rises count only above the
// level of the last rise.
func (w tankFillWindow) fromLowest() tankFillWindow {
	start := 0
	for i, s := range w {
		if s.level <= w[start].level {
			start = i
		}
	}
	return w[start:]
}

// sinceLastPause drops the rises before the latest gap longer than
// tankFillClimbPause: a fill that stopped and resumed has two rates, and the
// resumed one is the one to extrapolate.
func sinceLastPause(rises []tankFillRise) []tankFillRise {
	for i := len(rises) - 1; i > 0; i-- {
		if rises[i].at.Sub(rises[i-1].at) > tankFillClimbPause {
			return rises[i:]
		}
	}
	return rises
}

// netGain is the current level less the level the window opens at. The caller
// passes the window from its lowest level, so a drawdown inside the window
// does not hide a fill that follows it.
func (w tankFillWindow) netGain() float64 {
	return w[len(w)-1].level - w[0].level
}

// atHigh reports whether the level is at the window's maximum, with no fall
// of floor or more after the latest time it was there. A step down therefore
// ends a fill at once.
func (w tankFillWindow) atHigh(floor float64) bool {
	high, at := w[0].level, 0
	for i, s := range w {
		if s.level >= high {
			high, at = s.level, i
		}
	}
	for _, s := range w[at+1:] {
		if s.level <= high-floor {
			return false
		}
	}
	return true
}

// fastFill is the hose or nozzle rule: two rises in the climb and a gain of
// two steps (at least two points), with the level at its high.
func fastFill(w tankFillWindow, floor, step float64) bool {
	return len(w) > 1 && len(w.rises(floor)) >= 2 &&
		w.netGain() >= math.Max(tankFillFastGainSteps*step, tankFillMinGainPoints) && w.atHigh(floor)
}

// slowFill is the watermaker rule: three rises in the climb, the last of them
// recent, a gain of three steps (at least two points), with the level at its
// high. "Recent" is within 15 minutes and within twice the typical gap between
// rises, and the gap before the last rise is held to the same bound, so a rise
// after a long pause does not revive a stopped fill.
func slowFill(w tankFillWindow, now time.Time, floor, step float64) bool {
	if len(w) < 2 {
		return false
	}
	rises := w.rises(floor)
	if len(rises) < 3 {
		return false
	}
	gaps := make([]float64, 0, len(rises)-1)
	for i := 1; i < len(rises); i++ {
		gaps = append(gaps, rises[i].at.Sub(rises[i-1].at).Seconds())
	}
	bound := min(tankFillSlowLastRise, time.Duration(tankFillSlowGapShare*median(gaps)*float64(time.Second)))
	last := rises[len(rises)-1]
	return now.Sub(last.at) <= bound && last.at.Sub(rises[len(rises)-2].at) <= bound &&
		w.netGain() >= math.Max(tankFillSlowGainSteps*step, tankFillMinGainPoints) && w.atHigh(floor)
}

// secondsToFull is the time from now to 100% along the Theil-Sen line through
// the rise points, or nil when the rate is unsteady or the tank is under a
// minute from full. The line is fixed in time, so the answer counts down
// between steps.
func secondsToFull(points []tankFillRise, now time.Time) *float64 {
	if len(points) < tankFillMinPoints {
		return nil
	}
	var slopes []float64
	for i := 0; i < len(points); i++ {
		for j := i + 1; j < len(points); j++ {
			dt := points[j].at.Sub(points[i].at)
			if dt < tankFillMinBaseline {
				continue
			}
			slopes = append(slopes, (points[j].level-points[i].level)/dt.Seconds())
		}
	}
	if len(slopes) < tankFillMinPoints {
		return nil
	}
	sort.Float64s(slopes)
	low, high := slopes[len(slopes)/4], slopes[(3*len(slopes))/4]
	slope := median(slopes)
	if slope <= 0 || low <= 0 || high > tankFillSteadyRatio*low {
		return nil
	}
	// Seconds are measured from now, so the intercept is the level now.
	intercepts := make([]float64, len(points))
	for i, p := range points {
		intercepts[i] = p.level - slope*p.at.Sub(now).Seconds()
	}
	secs := (100 - median(intercepts)) / slope
	if secs < tankFillMinSecondsToFull {
		return nil
	}
	return &secs
}

// annotateTankFilling puts each listed tank's filling state on the Tanks
// payload.
func annotateTankFilling(tanks []tankLevelData, now time.Time) {
	for i := range tanks {
		tanks[i].Filling, tanks[i].SecondsToFull = tankFill.status(tanks[i].ID, now)
	}
}

// sampleTankFilling feeds the live tracker from the poll tick (ADR 0282).
// Speed over ground is -1 when the vessel state could not be read, carries
// none, or the speed reading is stale; the detector then reports no fill
// rather than guess the boat is stopped. A tank whose level reading is stale
// is not observed at all: a dead sender must not look like a live one. Nothing
// here loads label overrides, since detection needs only each tank's id, kind
// and level.
func sampleTankFilling(state vesselStateData, stateErr error, now time.Time) {
	t := globalSignalKSnapshot.selfTelemetry()
	if !t.HasSelf() {
		return
	}
	sogKts := -1.0
	if stateErr == nil {
		if sog, ok := t.Number("navigation.speedOverGround"); ok && sog.AgeKnown && sog.Age <= tankFillMaxReadingAge {
			sogKts = state.SpeedOverGroundKts
		}
	}
	for _, category := range t.Children("tanks") {
		kind := tankFillKind(category)
		if kind == "" {
			continue
		}
		for _, entryID := range t.Children("tanks." + category) {
			reading, ok := t.Number("tanks." + category + "." + entryID + ".currentLevel")
			if !ok || !reading.AgeKnown || reading.Age > tankFillMaxReadingAge {
				continue
			}
			level, ok := tankLevelPercent(reading.Value)
			if !ok {
				continue
			}
			tankFill.observe(category+"."+entryID, kind, level, sogKts, now)
		}
	}
}
