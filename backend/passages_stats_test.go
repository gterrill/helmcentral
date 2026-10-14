package main

import (
	"testing"
	"time"
)

// A one-hour run at 6 kn, points every minute, with full readings.
func instrumentedRun() []recordedTrackPoint {
	b := newTrackBuilder()
	b.run(time.Minute, time.Minute, 0) // rest point at departure
	b.run(time.Hour, time.Minute, 6)
	for i := range b.pts {
		p := &b.pts[i]
		p.TWSKts = pfloat(10 + float64(i%3)) // 10, 11, 12
		p.GustKts = pfloat(14 + float64(i%5))
		p.FuelLph = pfloat(36)
		p.SoCPct = pfloat(90 - float64(i)/10)
	}
	b.pts[0].SOGKts = pfloat(0)
	b.pts[0].FuelLph = pfloat(0)
	return b.pts
}

func TestStatsFullReadings(t *testing.T) {
	pts := instrumentedRun()
	s := computePassageStats(pts)

	if !approx(s.DistanceNM, 6, 0.1) {
		t.Errorf("distance %.3f", s.DistanceNM)
	}
	if !approx(s.UnderwaySeconds, 3600, 70) {
		t.Errorf("underway %.0f s", s.UnderwaySeconds)
	}
	if s.MeanSOGKts == nil || !approx(*s.MeanSOGKts, 6, 0.05) {
		t.Errorf("mean sog %v", s.MeanSOGKts)
	}
	if s.MaxSOGKts == nil || *s.MaxSOGKts != 6 {
		t.Errorf("max sog %v", s.MaxSOGKts)
	}
	// 36 L/h for one hour; the first minute is a ramp from 0 (trapezoid)
	if s.FuelBurnedL == nil || !approx(*s.FuelBurnedL, 36, 1) {
		t.Errorf("fuel %v", s.FuelBurnedL)
	}
	if s.TWSMeanKts == nil || !approx(*s.TWSMeanKts, 11, 0.05) {
		t.Errorf("tws mean %v", s.TWSMeanKts)
	}
	if s.MaxGustKts == nil || *s.MaxGustKts != 18 {
		t.Errorf("max gust %v", s.MaxGustKts)
	}
	if s.SoCStartPct == nil || *s.SoCStartPct != 90 || s.SoCEndPct == nil || !approx(*s.SoCEndPct, 84, 0.2) || s.SoCMinPct == nil || *s.SoCMinPct != *s.SoCEndPct {
		t.Errorf("soc start/end/min %v %v %v", s.SoCStartPct, s.SoCEndPct, s.SoCMinPct)
	}
}

func TestStatsMissingReadingsAreAbsentNotZero(t *testing.T) {
	pts := instrumentedRun()
	for i := range pts {
		pts[i].TWSKts, pts[i].GustKts, pts[i].FuelLph, pts[i].SoCPct, pts[i].SOGKts = nil, nil, nil, nil, nil
	}
	s := computePassageStats(pts)
	if s.MeanSOGKts != nil || s.MaxSOGKts != nil || s.FuelBurnedL != nil || s.TWSMeanKts != nil ||
		s.MaxGustKts != nil || s.SoCStartPct != nil || s.SoCEndPct != nil || s.SoCMinPct != nil {
		t.Errorf("every reading missing must give absent stats, got %+v", s)
	}
	// distance and time come from position, which is always there
	if !approx(s.DistanceNM, 6, 0.1) {
		t.Errorf("distance %.3f", s.DistanceNM)
	}
}

func TestStatsFuelAbsentWhenAnyLegHasNoRate(t *testing.T) {
	pts := instrumentedRun()
	pts[30].FuelLph = nil
	if s := computePassageStats(pts); s.FuelBurnedL != nil {
		t.Errorf("fuel %v: a burn with an unmeasured stretch must not be reported", *s.FuelBurnedL)
	}
}

func TestStatsSegmentBreakIsNotIntegratedOrCounted(t *testing.T) {
	pts := instrumentedRun()
	for i := 30; i < len(pts); i++ {
		pts[i].Segment = 1
		pts[i].At = pts[i].At.Add(3 * time.Hour)
		pts[i].Lat += 2
	}
	s := computePassageStats(pts)
	if !approx(s.DistanceNM, 6, 0.3) {
		t.Errorf("distance %.2f: the break must not be bridged", s.DistanceNM)
	}
	if s.UnderwaySeconds > 3700 {
		t.Errorf("underway %.0f s counted the gap", s.UnderwaySeconds)
	}
}

func TestStatsEmptyAndSinglePoint(t *testing.T) {
	if s := computePassageStats(nil); s.DistanceNM != 0 || s.FuelBurnedL != nil {
		t.Errorf("%+v", s)
	}
	if s := computePassageStats(instrumentedRun()[:1]); s.DistanceNM != 0 || s.FuelBurnedL != nil {
		t.Errorf("%+v", s)
	}
}

func TestSimplifyKeepsEndsAndCornersAndSplitsSegments(t *testing.T) {
	// an L-shaped track with many collinear points on each leg
	var pts []recordedTrackPoint
	for i := 0; i <= 50; i++ {
		pts = append(pts, recordedTrackPoint{At: passageEpoch.Add(time.Duration(i) * time.Minute), Lat: -20 + float64(i)*0.001, Lon: 149})
	}
	for i := 1; i <= 50; i++ {
		pts = append(pts, recordedTrackPoint{At: passageEpoch.Add(time.Duration(50+i) * time.Minute), Lat: -20 + 0.05, Lon: 149 + float64(i)*0.001})
	}
	out := simplifyTrack(pts, 10)
	if len(out) != 1 || len(out[0]) != 3 {
		t.Fatalf("want one segment of 3 points (start, corner, end), got %v", out)
	}
	if out[0][0] != [2]float64{-20, 149} || out[0][2] != [2]float64{-20 + 0.05, 149.05} {
		t.Errorf("ends moved: %v", out[0])
	}

	pts[60].Segment = 1
	for i := 61; i < len(pts); i++ {
		pts[i].Segment = 1
	}
	if out := simplifyTrack(pts, 10); len(out) != 2 {
		t.Errorf("want 2 segments, got %d", len(out))
	}
}

func TestSimplifyToleranceIsMetricAtHighLatitude(t *testing.T) {
	// 0.0001 deg of longitude at 60N is 5.6 m; a 10 m tolerance drops the bump
	pts := []recordedTrackPoint{
		{At: passageEpoch, Lat: 60, Lon: 10},
		{At: passageEpoch.Add(time.Minute), Lat: 60.005, Lon: 10.0001},
		{At: passageEpoch.Add(2 * time.Minute), Lat: 60.01, Lon: 10},
	}
	if out := simplifyTrack(pts, 10); len(out[0]) != 2 {
		t.Errorf("a 5.6 m wobble should vanish at 10 m tolerance, got %v", out)
	}
	if out := simplifyTrack(pts, 3); len(out[0]) != 3 {
		t.Errorf("a 5.6 m wobble should survive a 3 m tolerance, got %v", out)
	}
}
