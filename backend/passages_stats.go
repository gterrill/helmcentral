package main

import (
	"math"
	"time"
)

// Per-passage figures (plan step 4), computed from the track points. A figure
// whose readings are missing is nil, never a guess.

type passageStats struct {
	DistanceNM      float64  `json:"distance_nm"`
	UnderwaySeconds float64  `json:"underway_seconds"`
	MeanSOGKts      *float64 `json:"mean_sog_kts"`
	MaxSOGKts       *float64 `json:"max_sog_kts"`
	FuelBurnedL     *float64 `json:"fuel_burned_l"`
	TWSMeanKts      *float64 `json:"tws_mean_kts"`
	MaxGustKts      *float64 `json:"max_gust_kts"`
	SoCStartPct     *float64 `json:"soc_start_pct"`
	SoCEndPct       *float64 `json:"soc_end_pct"`
	SoCMinPct       *float64 `json:"soc_min_pct"`

	// The time of each maximum, in Unix seconds, so the page can move the
	// replay to it. It is the time of the stored point that carries the figure;
	// a gust or motion peak is the largest since the point before it, so the
	// moment itself lies in the interval just before.
	MaxSOGAt  *int64 `json:"max_sog_at"`
	MaxGustAt *int64 `json:"max_gust_at"`

	// Motion maxima (degrees, metres), from the peaks the recorder keeps. Nil
	// when no point has a reading, which is every passage recorded before
	// motion was kept.
	MaxRollDeg  *float64 `json:"max_roll_deg"`
	MaxRollAt   *int64   `json:"max_roll_at"`
	MaxPitchDeg *float64 `json:"max_pitch_deg"`
	MaxPitchAt  *int64   `json:"max_pitch_at"`
	MaxHeaveM   *float64 `json:"max_heave_m"`
	MaxHeaveAt  *int64   `json:"max_heave_at"`
}

// raiseMax keeps the larger reading and the time it was taken. The first
// point to reach a value keeps it: a later tie does not move the time.
func raiseMax(best **float64, at **int64, reading *float64, t time.Time) {
	if reading == nil || (*best != nil && **best >= *reading) {
		return
	}
	v, ts := *reading, t.Unix()
	*best, *at = &v, &ts
}

// A leg joins two consecutive points of one segment no further apart than the
// segment gap. Anything else is a hole in the record.
//
// Imported points are the exception to the gap limit: a simplified GPX track
// is sparse by design, and a long leg between two of its fixes is the boat
// travelling (or sitting) there, not a hole.
func legBetween(a, b recordedTrackPoint) bool {
	if a.Segment != b.Segment || !b.At.After(a.At) {
		return false
	}
	if a.Source == trackSourceImported && b.Source == trackSourceImported {
		return true
	}
	return b.At.Sub(a.At) <= trackSegmentGap
}

func computePassageStats(pts []recordedTrackPoint) passageStats {
	var s passageStats
	if len(pts) == 0 {
		return s
	}

	var (
		sogWeighted, sogSeconds float64
		fuelL                   float64
		fuelComplete            = len(pts) > 1
		twsSum                  float64
		twsN                    int
	)

	for i, p := range pts {
		raiseMax(&s.MaxSOGKts, &s.MaxSOGAt, p.SOGKts, p.At)
		raiseMax(&s.MaxGustKts, &s.MaxGustAt, p.GustKts, p.At)
		// A point's motion peak is the interval before it, which for the first
		// point lies outside the passage.
		if i > 0 {
			raiseMax(&s.MaxRollDeg, &s.MaxRollAt, p.RollPeakDeg, p.At)
			raiseMax(&s.MaxPitchDeg, &s.MaxPitchAt, p.PitchPeakDeg, p.At)
			raiseMax(&s.MaxHeaveM, &s.MaxHeaveAt, p.HeavePeakM, p.At)
		}
		if p.TWSKts != nil {
			twsSum += *p.TWSKts
			twsN++
		}
		if p.SoCPct != nil {
			if s.SoCStartPct == nil {
				v := *p.SoCPct
				s.SoCStartPct = &v
			}
			v := *p.SoCPct
			s.SoCEndPct = &v
			if s.SoCMinPct == nil || v < *s.SoCMinPct {
				m := v
				s.SoCMinPct = &m
			}
		}

		if i == 0 {
			continue
		}
		prev := pts[i-1]
		if !legBetween(prev, p) {
			// A hole in the record. Fuel is only known across it if the
			// engines were measured off on both sides.
			if prev.FuelLph == nil || p.FuelLph == nil || *prev.FuelLph != 0 || *p.FuelLph != 0 {
				fuelComplete = false
			}
			continue
		}
		dt := p.At.Sub(prev.At).Seconds()
		meters := haversineMeters(prev.Lat, prev.Lon, p.Lat, p.Lon)
		s.DistanceNM += meters / passageMetersPerMile

		// Underway is judged from position, which every point has: a leg
		// counts when it was covered at one knot or better.
		if meters/dt >= passageRestSOGKts*trackMetersPerKnot {
			s.UnderwaySeconds += dt
			if p.SOGKts != nil {
				sogWeighted += *p.SOGKts * dt
				sogSeconds += dt
			}
		}

		if prev.FuelLph != nil && p.FuelLph != nil {
			fuelL += (*prev.FuelLph + *p.FuelLph) / 2 * dt / 3600
		} else {
			fuelComplete = false
		}
	}

	if sogSeconds > 0 {
		v := sogWeighted / sogSeconds
		s.MeanSOGKts = &v
	}
	if twsN > 0 {
		v := twsSum / float64(twsN)
		s.TWSMeanKts = &v
	}
	if fuelComplete {
		s.FuelBurnedL = &fuelL
	}
	return s
}

// simplifyTrack thins a track for display with Douglas-Peucker, working in a
// metric projection around the track's mean latitude so the tolerance means
// metres east-west as well as north-south. Segments are simplified apart and
// returned apart, as [lat, lon] pairs. The log itself is never thinned.
func simplifyTrack(pts []recordedTrackPoint, toleranceMeters float64) [][][2]float64 {
	lines, _ := simplifyTrackTimed(pts, toleranceMeters)
	return lines
}

// simplifyTrackTimed is simplifyTrack plus the time of every kept point, as
// Unix seconds in the same shape, so a replay can place the boat at a time.
func simplifyTrackTimed(pts []recordedTrackPoint, toleranceMeters float64) ([][][2]float64, [][]int64) {
	if len(pts) == 0 {
		return nil, nil
	}
	var latSum float64
	for _, p := range pts {
		latSum += p.Lat
	}
	cosLat := math.Cos(latSum / float64(len(pts)) * math.Pi / 180)
	const metersPerDegree = 111195.0

	var out [][][2]float64
	var times [][]int64
	for start := 0; start < len(pts); {
		end := start
		for end < len(pts) && pts[end].Segment == pts[start].Segment {
			end++
		}
		seg := pts[start:end]
		xy := make([][2]float64, len(seg))
		for i, p := range seg {
			xy[i] = [2]float64{p.Lon * metersPerDegree * cosLat, p.Lat * metersPerDegree}
		}
		keep := make([]bool, len(seg))
		keep[0], keep[len(seg)-1] = true, true
		douglasPeucker(xy, 0, len(seg)-1, toleranceMeters, keep)
		var line [][2]float64
		var at []int64
		for i, k := range keep {
			if k {
				line = append(line, [2]float64{seg[i].Lat, seg[i].Lon})
				at = append(at, seg[i].At.Unix())
			}
		}
		out = append(out, line)
		times = append(times, at)
		start = end
	}
	return out, times
}

func douglasPeucker(xy [][2]float64, lo, hi int, tol float64, keep []bool) {
	// iterative to keep a multi-day track off the call stack
	type span struct{ lo, hi int }
	stack := []span{{lo, hi}}
	for len(stack) > 0 {
		s := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if s.hi-s.lo < 2 {
			continue
		}
		far, farD := -1, tol
		for i := s.lo + 1; i < s.hi; i++ {
			if d := perpendicularMeters(xy[i], xy[s.lo], xy[s.hi]); d > farD {
				far, farD = i, d
			}
		}
		if far >= 0 {
			keep[far] = true
			stack = append(stack, span{s.lo, far}, span{far, s.hi})
		}
	}
}

func perpendicularMeters(p, a, b [2]float64) float64 {
	dx, dy := b[0]-a[0], b[1]-a[1]
	if dx == 0 && dy == 0 {
		return math.Hypot(p[0]-a[0], p[1]-a[1])
	}
	t := ((p[0]-a[0])*dx + (p[1]-a[1])*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(p[0]-(a[0]+t*dx), p[1]-(a[1]+t*dy))
}

// passageReplayInterval is the spacing of the replay track.
const passageReplayInterval = 30 * time.Second

// replayTrack thins a track in time rather than in distance, for replay: about
// one point per interval within each segment, plus the first and last point of
// the segment and both points either side of every crossing of the at-rest
// speed line, so a stop shows as a stop. Returns [lat, lon] lines and their
// Unix-second times, in the shape simplifyTrackTimed uses.
func replayTrack(pts []recordedTrackPoint, interval time.Duration) ([][][2]float64, [][]int64) {
	if len(pts) == 0 {
		return nil, nil
	}
	var lines [][][2]float64
	var times [][]int64
	for start := 0; start < len(pts); {
		end := start
		for end < len(pts) && pts[end].Segment == pts[start].Segment {
			end++
		}
		seg := pts[start:end]
		keep := make([]bool, len(seg))
		keep[0], keep[len(seg)-1] = true, true
		lastKept := seg[0].At
		for i := 1; i < len(seg); i++ {
			if seg[i].At.Sub(lastKept) >= interval {
				keep[i] = true
			}
			if keep[i] {
				lastKept = seg[i].At
			}
			if a, b := seg[i-1].SOGKts, seg[i].SOGKts; a != nil && b != nil && (*a < passageRestSOGKts) != (*b < passageRestSOGKts) {
				keep[i-1], keep[i] = true, true
				lastKept = seg[i].At
			}
		}
		var line [][2]float64
		var at []int64
		for i, k := range keep {
			if k {
				line = append(line, [2]float64{seg[i].Lat, seg[i].Lon})
				at = append(at, seg[i].At.Unix())
			}
		}
		lines = append(lines, line)
		times = append(times, at)
		start = end
	}
	return lines, times
}
