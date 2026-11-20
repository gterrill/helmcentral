package main

import (
	"fmt"
	"log"
	"math"
	"sync"
	"time"
)

// The track point recorder (ADR 0176 decisions 2 to 4). Observe is fed one
// sample per poll tick and decides whether the sample becomes a row in the
// track point log. It knows nothing about SignalK: passages_live.go builds the
// samples, so the rules here are testable on their own.

const (
	trackKeepMeters    = 25.0
	trackKeepMinGap    = 5 * time.Second
	trackRestInterval  = 10 * time.Minute
	trackSegmentGap    = 10 * time.Minute
	trackMaxJumpKts    = 40.0
	trackReadingMaxAge = 10 * time.Second
	trackMetersPerKnot = 1852.0 / 3600.0
)

// trackSample is one poll tick. FixOK is false when the position is not to be
// trusted (gnss_critical_alert, or the fix itself is older than the readings
// limit). The readings are already nil where too old; Gust is the recorder's
// own.
type trackSample struct {
	At       time.Time
	Lat, Lon float64
	FixOK    bool

	SOGKts, COGDeg, TWSKts, SoCPct, FuelLph, WaterL *float64
	// RollDeg, PitchDeg and HeaveM are the boat's attitude on this tick, signed.
	// The recorder keeps the largest magnitude between kept points.
	RollDeg, PitchDeg, HeaveM *float64
	// The context readings are kept as read on the tick that becomes a point,
	// like SOG.
	trackContext
	GenState    *string
	AnchorSetAt *time.Time
}

type trackRecorder struct {
	store *passageStore

	mu        sync.Mutex
	last      *recordedTrackPoint // last kept point
	lastFixAt time.Time           // last accepted fix, kept or not
	segment   int
	maxTWS    *float64 // highest true wind speed seen since the last kept point

	// Largest magnitudes of roll, pitch and heave since the last kept point.
	peakRoll, peakPitch, peakHeave *float64

	// motion is fed by the 10 Hz motion stream (signalk_motion_stream.go). Each
	// kept point takes its window. Nil when no stream runs, as in most tests.
	motion *motionAccumulator
	// warnedNoAttitude and warnedNoHeave hold an episode of checkMotionStream.
	warnedNoAttitude, warnedNoHeave bool

	// rejected is the run of fixes thrown out as jumps that agree with one
	// another. If it holds for trackJumpRecovery the kept reference was the
	// bad one, and the run becomes the new reference.
	rejected *rejectedRun
}

type rejectedRun struct {
	since, lastAt time.Time
	lastLat       float64
	lastLon       float64
}

const trackJumpRecovery = 2 * time.Minute

// newTrackRecorder opens the recorder over store. motion is the accumulator the
// motion stream feeds; nil when no stream runs, as in most tests.
func newTrackRecorder(store *passageStore, motion *motionAccumulator) (*trackRecorder, error) {
	r := &trackRecorder{store: store, motion: motion}
	last, err := store.LastTrackPoint()
	if err != nil {
		return nil, err
	}
	if last != nil {
		r.last = last
		r.lastFixAt = last.At
		r.segment = last.Segment
	}
	return r, nil
}

// Observe applies the fix filter and the keep rule and, when the sample is
// kept, appends it to the log. A rejected fix is not an error.
func (r *trackRecorder) Observe(s trackSample) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if s.TWSKts != nil && (r.maxTWS == nil || *s.TWSKts > *r.maxTWS) {
		v := *s.TWSKts
		r.maxTWS = &v
	}

	r.peakRoll = peakOf(r.peakRoll, s.RollDeg)
	r.peakPitch = peakOf(r.peakPitch, s.PitchDeg)
	r.peakHeave = peakOf(r.peakHeave, s.HeaveM)

	if !s.FixOK || !hasUsableVesselPosition(s.Lat, s.Lon) {
		return false, nil
	}

	var meters, dt float64
	if r.last != nil {
		dt = s.At.Sub(r.last.At).Seconds()
		if dt <= 0 {
			return false, nil
		}
		meters = haversineMeters(r.last.Lat, r.last.Lon, s.Lat, s.Lon)
		if meters/dt > trackMaxJumpKts*trackMetersPerKnot {
			if !r.noteRejectedJump(s) {
				return false, nil
			}
			// The fixes since the last kept point agree with each other and
			// not with it: the kept point was the bad one.
			log.Printf("passages: track jump lockout: %v at %.5f,%.5f abandoned for %.5f,%.5f after %v of consistent fixes elsewhere; new segment",
				r.last.At.Format(time.RFC3339), r.last.Lat, r.last.Lon, s.Lat, s.Lon, trackJumpRecovery)
			r.rejected = nil
			r.segment++
			r.lastFixAt = s.At
			return r.keep(s)
		}
		r.rejected = nil
	}

	if !r.lastFixAt.IsZero() && s.At.Sub(r.lastFixAt) > trackSegmentGap {
		r.segment++
	}
	r.lastFixAt = s.At

	if !r.shouldKeep(s, meters, dt) {
		return false, nil
	}

	return r.keep(s)
}

// noteRejectedJump extends or restarts the run of mutually consistent
// rejected fixes and reports whether it has lasted long enough to trust.
func (r *trackRecorder) noteRejectedJump(s trackSample) bool {
	if r.rejected != nil {
		dt := s.At.Sub(r.rejected.lastAt).Seconds()
		if dt > 0 && haversineMeters(r.rejected.lastLat, r.rejected.lastLon, s.Lat, s.Lon)/dt <= trackMaxJumpKts*trackMetersPerKnot {
			r.rejected.lastAt, r.rejected.lastLat, r.rejected.lastLon = s.At, s.Lat, s.Lon
			return s.At.Sub(r.rejected.since) >= trackJumpRecovery
		}
	}
	r.rejected = &rejectedRun{since: s.At, lastAt: s.At, lastLat: s.Lat, lastLon: s.Lon}
	return false
}

func (r *trackRecorder) keep(s trackSample) (bool, error) {
	p := recordedTrackPoint{
		At: s.At, Segment: r.segment, Lat: s.Lat, Lon: s.Lon,
		SOGKts: s.SOGKts, COGDeg: s.COGDeg, TWSKts: s.TWSKts, GustKts: r.maxTWS,
		SoCPct: s.SoCPct, FuelLph: s.FuelLph, WaterL: s.WaterL,
		GenState: s.GenState, AnchorSetAt: s.AnchorSetAt,
		RollPeakDeg: r.peakRoll, PitchPeakDeg: r.peakPitch, HeavePeakM: r.peakHeave,
		trackContext: s.trackContext,
	}
	// The window is taken before the write, and the motion filter keeps its
	// history either way. A failed write loses this window's figures where the
	// peaks above survive to the next point; the failure is logged by the
	// caller, and the figures are statistics of the interval, not state.
	if r.motion != nil {
		figures, counts := r.motion.TakeWindow()
		p.motionFigures = figures
		r.checkMotionStream(s, counts)
	}
	if err := r.store.AppendTrackPoint(p); err != nil {
		return false, fmt.Errorf("record track point: %w", err)
	}
	r.last = &p
	r.maxTWS = nil
	r.peakRoll, r.peakPitch, r.peakHeave = nil, nil, nil
	return true, nil
}

// motionHealthMinSilence is how long a signal must have gone without an
// accepted sample, on Helmcentral's clock, before that counts as a fault: a
// stream that has only just started has not had time to deliver. It is measured
// across windows, because under way a window is only seconds long.
const motionHealthMinSilence = time.Minute

// checkMotionStream is the motion stream's health signal. A signal with no
// accepted sample for motionHealthMinSilence (counted from startup when it has
// had none) while the main snapshot has a fresh reading of it means the stream
// is delivering nothing that SignalK has: a dead subscription, a refused login.
// It says so once per episode, and again after samples have returned and
// stopped.
func (r *trackRecorder) checkMotionStream(s trackSample, counts motionWindowCounts) {
	check := func(name string, got int, silence time.Duration, snapshotHas bool, warned *bool) {
		switch {
		case got > 0:
			*warned = false
		case snapshotHas && silence >= motionHealthMinSilence && !*warned:
			*warned = true
			log.Printf("passages: motion stream delivering no %s while SignalK has it", name)
		}
	}
	check("attitude", counts.AttitudeSamples, counts.AttitudeSilence, s.RollDeg != nil, &r.warnedNoAttitude)
	check("heave", counts.HeaveSamples, counts.HeaveSilence, s.HeaveM != nil, &r.warnedNoHeave)
}

// peakOf returns the larger of the running peak and |reading|. A missing
// reading leaves the peak as it was.
func peakOf(peak, reading *float64) *float64 {
	if reading == nil {
		return peak
	}
	v := math.Abs(*reading)
	if peak != nil && *peak >= v {
		return peak
	}
	return &v
}

func (r *trackRecorder) shouldKeep(s trackSample, meters, dt float64) bool {
	if r.last == nil {
		return true
	}
	if meters >= trackKeepMeters && dt >= trackKeepMinGap.Seconds() {
		return true
	}
	if dt >= trackRestInterval.Seconds() {
		return true
	}
	// A generator start or stop is always a point, so running hours can be
	// summed from the intervals between them.
	if s.GenState != nil && (r.last.GenState == nil || *r.last.GenState != *s.GenState) {
		return true
	}
	// Coming to rest is always a point, so arrival is accurate to the tick.
	if s.SOGKts != nil && *s.SOGKts < passageRestSOGKts && r.last.SOGKts != nil && *r.last.SOGKts >= passageRestSOGKts {
		return true
	}
	return false
}
