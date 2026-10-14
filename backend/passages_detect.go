package main

import (
	"time"
)

// The passage detector (ADR 0173 decision 5). It is a pure function over
// time-ordered track points so the same rules can later run over a GPX import.
// Speeds come from each point's SOG reading; a point with no SOG says nothing
// about whether the boat was moving, so it neither starts a passage nor holds
// one open.

const (
	passageStartSOGKts   = 3.0
	passageStartHold     = 2 * time.Minute
	passageRestSOGKts    = 1.0
	passageEndQuiet      = 60 * time.Minute
	passageMinDuration   = 15 * time.Minute
	passageMinDistanceNM = 0.5
	passageMetersPerMile = 1852.0
)

// loggedPassage is one detected run. Start is backdated to the last at-rest
// point before departure; Departed is the first moment at passageStartSOGKts,
// which is what the minimum-duration test measures from, so the backdating
// cannot rescue a run that was too short.
type loggedPassage struct {
	Start      time.Time
	Departed   time.Time
	End        time.Time
	DistanceNM float64
}

type passageDetection struct {
	Closed []loggedPassage
	// Open is a passage still under way at the end of the points, or nil.
	Open *loggedPassage
	// Resume is the time to restart from next run: the start of the open
	// passage or of a departure still being confirmed, otherwise the last
	// point seen. Points before it can never change a result.
	Resume time.Time
}

func pointMoving(p recordedTrackPoint) bool {
	return p.SOGKts != nil && *p.SOGKts >= passageRestSOGKts
}

func pointAtRest(p recordedTrackPoint) bool {
	return p.SOGKts != nil && *p.SOGKts < passageRestSOGKts
}

// trackDistanceNM sums the legs between consecutive points of the same
// segment whose time lies in [from, to]. A segment break is a gap in the
// record, not a leg the boat sailed.
func trackDistanceNM(pts []recordedTrackPoint, from, to time.Time) float64 {
	var meters float64
	var prev *recordedTrackPoint
	for i := range pts {
		p := &pts[i]
		if p.At.Before(from) || p.At.After(to) {
			continue
		}
		if prev != nil && prev.Segment == p.Segment {
			meters += haversineMeters(prev.Lat, prev.Lon, p.Lat, p.Lon)
		}
		prev = p
	}
	return meters / passageMetersPerMile
}

func detectLoggedPassages(pts []recordedTrackPoint) passageDetection {
	var res passageDetection
	if len(pts) == 0 {
		return res
	}

	running := false
	runStart := -1 // first point of a run at >= start speed, while idle
	var startIdx, departIdx, lastMoving int

	backdate := func(rs int) int {
		for j := rs - 1; j >= 0 && pts[j].Segment == pts[rs].Segment; j-- {
			if pointAtRest(pts[j]) {
				return j
			}
		}
		return rs
	}

	// restStart is when the boat first stopped after its last moving point.
	// Across a segment break nothing says when it stopped, so the last moving
	// point stands.
	restStart := func(i int) time.Time {
		j := lastMoving + 1
		if j < len(pts) && j <= i && pts[j].Segment == pts[lastMoving].Segment && (j < i || pointAtRest(pts[i])) {
			return pts[j].At
		}
		return pts[lastMoving].At
	}

	finish := func(end time.Time) {
		lp := loggedPassage{Start: pts[startIdx].At, Departed: pts[departIdx].At, End: end}
		if end.Before(lp.Start) {
			end = lp.Start
			lp.End = end
		}
		lp.DistanceNM = trackDistanceNM(pts, lp.Start, lp.End)
		if lp.End.Sub(lp.Departed) < passageMinDuration || lp.DistanceNM < passageMinDistanceNM {
			return
		}
		res.Closed = append(res.Closed, lp)
	}

	for i := range pts {
		p := pts[i]

		if running {
			var end time.Time
			ended := false

			if quiet := p.At.Sub(pts[lastMoving].At); quiet >= passageEndQuiet {
				end, ended = restStart(i), true
			}
			if p.AnchorSetAt != nil && p.AnchorSetAt.After(pts[startIdx].At) && !p.AnchorSetAt.After(p.At) {
				d := *p.AnchorSetAt
				if rs := restStart(i); rs.Before(d) {
					d = rs
				}
				if !ended || d.Before(end) {
					end, ended = d, true
				}
			}

			if !ended {
				if pointMoving(p) {
					lastMoving = i
				}
				continue
			}
			finish(end)
			running = false
			runStart = -1
		}

		if p.SOGKts != nil && *p.SOGKts >= passageStartSOGKts {
			if runStart < 0 || pts[runStart].Segment != p.Segment {
				runStart = i
			}
			if p.At.Sub(pts[runStart].At) >= passageStartHold {
				running = true
				departIdx = runStart
				startIdx = backdate(runStart)
				lastMoving = i
			}
		} else {
			runStart = -1
		}
	}

	switch {
	case running:
		open := loggedPassage{Start: pts[startIdx].At, Departed: pts[departIdx].At, End: pts[len(pts)-1].At}
		open.DistanceNM = trackDistanceNM(pts, open.Start, open.End)
		res.Open = &open
		res.Resume = open.Start
	case runStart >= 0:
		res.Resume = pts[backdate(runStart)].At
	default:
		res.Resume = pts[len(pts)-1].At
	}
	return res
}
