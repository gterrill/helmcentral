package main

import (
	"fmt"
	"sync"
	"time"
)

// The track point recorder (ADR 0173 decisions 2 to 4). Observe is fed one
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
}

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
			return false, nil
		}
	}

	if !r.lastFixAt.IsZero() && s.At.Sub(r.lastFixAt) > trackSegmentGap {
		r.segment++
	}
	r.lastFixAt = s.At

	if !r.shouldKeep(s, meters, dt) {
		return false, nil
	}

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
	return false
}
