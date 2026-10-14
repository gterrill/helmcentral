package main

import (
	"fmt"
	"log"
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
	GenState                                        *string
	AnchorSetAt                                     *time.Time
}

type trackRecorder struct {
	store *passageStore

	mu        sync.Mutex
	last      *recordedTrackPoint // last kept point
	lastFixAt time.Time           // last accepted fix, kept or not
	segment   int
	maxTWS    *float64 // highest true wind speed seen since the last kept point

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

func newTrackRecorder(store *passageStore) (*trackRecorder, error) {
	r := &trackRecorder{store: store}
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
	}
	if err := r.store.AppendTrackPoint(p); err != nil {
		return false, fmt.Errorf("record track point: %w", err)
	}
	r.last = &p
	r.maxTWS = nil
	return true, nil
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
