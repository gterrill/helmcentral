package main

import (
	"math"
	"path/filepath"
	"testing"
	"time"
)

var passageEpoch = time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)

func pfloat(v float64) *float64 { return &v }
func pstr(v string) *string     { return &v }

func newTestPassageStore(t *testing.T) *passageStore {
	t.Helper()
	s, err := newPassageStore(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("newPassageStore: %v", err)
	}
	t.Cleanup(func() { _ = s.close() })
	return s
}

// trackBuilder lays points along a meridian so distances are exact: 1 NM is
// one arc-minute of latitude.
type trackBuilder struct {
	pts []recordedTrackPoint
	lat float64
	t   time.Time
	seg int
}

func newTrackBuilder() *trackBuilder {
	return &trackBuilder{lat: -20.0, t: passageEpoch}
}

// run adds points every step for dur at sog knots, advancing north.
func (b *trackBuilder) run(dur, step time.Duration, sog float64) *trackBuilder {
	for el := time.Duration(0); el < dur; el += step {
		b.t = b.t.Add(step)
		b.lat += sog * step.Hours() / 60.0
		b.pts = append(b.pts, recordedTrackPoint{At: b.t, Segment: b.seg, Lat: b.lat, Lon: 149.0, SOGKts: pfloat(sog)})
	}
	return b
}

func (b *trackBuilder) gap(d time.Duration) *trackBuilder {
	b.t = b.t.Add(d)
	b.seg++
	return b
}

func (b *trackBuilder) last() *recordedTrackPoint { return &b.pts[len(b.pts)-1] }

func approx(a, b, tol float64) bool { return math.Abs(a-b) <= tol }
