package main

import (
	"context"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// Angles in the log. SignalK stores every angle in radians, and an angle is
// not a magnitude: 350 and 10 degrees are 20 degrees apart, and their linear
// mean points the wrong way round the compass. Everything that averages,
// draws or reports an angle goes through this file (ADR 0221).

// angleKind says how an angle is read and drawn.
type angleKind string

const (
	angleNone angleKind = ""
	// angleBearing is a compass bearing, 0-360 degrees: wind direction,
	// heading, course over ground.
	angleBearing angleKind = "bearing"
	// angleRelative is an angle off the bow or a tilt, -180 to 180 degrees:
	// apparent wind angle, rudder angle, pitch and roll.
	angleRelative angleKind = "relative"
)

// circularMinResultant is the shortest mean vector that still points
// somewhere. The mean of unit vectors has length 1 when every sample agrees
// and 0 when they cancel out. Under 0.1 the samples spread so evenly round the
// compass that the mean direction is noise, so there is none to report.
const circularMinResultant = 0.1

func floatPtr(v float64) *float64 { return &v }

// angleKindOf reads a path's kind from the unit the boat declares. Only radians
// are angles here: a path with no declared unit is never guessed to be one, and
// a rate (rad/s) is not an angle.
func angleKindOf(path, siUnit string) angleKind {
	if siUnit != "rad" {
		return angleNone
	}
	if isBearingLeaf(pathLeaf(path)) {
		return angleBearing
	}
	return angleRelative
}

func pathLeaf(path string) string {
	if i := strings.LastIndexByte(path, '.'); i >= 0 {
		return strings.ToLower(path[i+1:])
	}
	return strings.ToLower(path)
}

func isBearingLeaf(leaf string) bool {
	for _, w := range []string{"direction", "heading", "courseoverground", "bearing"} {
		if strings.Contains(leaf, w) {
			return true
		}
	}
	return false
}

// angleUnitLabel is the unit a reader sees: °T for a true bearing, °M for a
// magnetic one, and plain ° for anything that names neither (a relative angle
// is not a bearing, so it never carries T or M).
func angleUnitLabel(path string) string {
	leaf := pathLeaf(path)
	if isBearingLeaf(leaf) {
		switch {
		case strings.Contains(leaf, "true"):
			return "°T"
		case strings.Contains(leaf, "magnetic"):
			return "°M"
		}
	}
	return "°"
}

// angleDegrees converts radians to the display range: [0, 360) for a bearing,
// (-180, 180] for a relative angle.
func angleDegrees(kind angleKind, radians float64) float64 {
	deg := math.Mod(radians*180/math.Pi, 360)
	if kind == angleRelative {
		if deg > 180 {
			deg -= 360
		} else if deg <= -180 {
			deg += 360
		}
		return deg
	}
	if deg < 0 {
		deg += 360
	}
	return deg
}

// circularMean is the direction of a mean vector given as its mean sine and
// cosine: radians in [0, 2π), the vector's length, and whether it points
// anywhere (circularMinResultant).
func circularMean(meanSin, meanCos float64) (radians, resultant float64, ok bool) {
	resultant = math.Hypot(meanSin, meanCos)
	if resultant < circularMinResultant {
		return 0, resultant, false
	}
	radians = math.Atan2(meanSin, meanCos)
	if radians < 0 {
		radians += 2 * math.Pi
	}
	return radians, resultant, true
}

// circularSpreadDegrees is the circular standard deviation, sqrt(-2 ln R), in
// degrees: 0 when every sample agrees and growing as the vector shortens.
func circularSpreadDegrees(resultant float64) float64 {
	if resultant >= 1 {
		return 0
	}
	if resultant <= 0 {
		return 180
	}
	return math.Sqrt(-2*math.Log(resultant)) * 180 / math.Pi
}

// compassPoints are the sixteen names of the compass, north first.
var compassPoints = [16]string{"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE", "S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"}

func compassPoint(deg float64) string {
	i := int(math.Floor(math.Mod(deg, 360)/22.5+0.5)) % 16
	if i < 0 {
		i += 16
	}
	return compassPoints[i]
}

// weightPathFor is the series that weights a direction's average. Wind
// direction is averaged by wind speed, so the afternoon's 20 knots outweighs
// the dawn's 2. Only the true wind direction has one; a heading has none.
func weightPathFor(path string) string {
	switch path {
	case windDirectionTruePath, "environment.wind.directionMagnetic":
		return windSpeedTruePath
	}
	return ""
}

// circularSample is one short window of an angle: its mean sine and cosine, and
// the weight it carries in the bucket it belongs to.
type circularSample struct {
	T        time.Time
	Sin, Cos float64
	Weight   float64
}

// circularBucket is the vector mean of the samples in one bucket. Defined is
// false when they cancel out or carry no weight (a dead calm): there is no
// mean direction, and none is reported.
type circularBucket struct {
	T         time.Time
	Mean      float64 // radians in [0, 2π); meaningful only when Defined
	Resultant float64
	Weight    float64 // the total weight of the samples
	Defined   bool
}

// combineCircular groups samples into width-sized buckets and takes each
// bucket's weighted vector mean: the mean of sin and the mean of cos, each
// weighted, recombined with atan2.
func combineCircular(samples []circularSample, width time.Duration) []circularBucket {
	type acc struct{ sumW, sumS, sumC float64 }
	by := map[int64]*acc{}
	at := map[int64]time.Time{}
	var keys []int64
	for _, s := range samples {
		bt := s.T.Truncate(width)
		k := bt.Unix()
		a, ok := by[k]
		if !ok {
			a = &acc{}
			by[k] = a
			at[k] = bt
			keys = append(keys, k)
		}
		a.sumW += s.Weight
		a.sumS += s.Weight * s.Sin
		a.sumC += s.Weight * s.Cos
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	out := make([]circularBucket, 0, len(keys))
	for _, k := range keys {
		a := by[k]
		b := circularBucket{T: at[k], Weight: a.sumW}
		if a.sumW > 0 {
			b.Mean, b.Resultant, b.Defined = circularMean(a.sumS/a.sumW, a.sumC/a.sumW)
		}
		out = append(out, b)
	}
	return out
}

// circularSubWidth is the window an angle is first averaged over inside a
// bucket, so a weight can tell a gale from a breeze within the same hour. It
// is the widest allowlisted width that fits in a sixth of the bucket and
// divides it, and never finer than a minute.
func circularSubWidth(bucket time.Duration) (string, time.Duration) {
	label, width := telemetrySeriesBucketWidths[0].label, telemetrySeriesBucketWidths[0].width
	for _, w := range telemetrySeriesBucketWidths {
		if w.width*6 <= bucket && bucket%w.width == 0 {
			label, width = w.label, w.width
		}
	}
	return label, width
}

// queryCircularHistoryBuckets is queryPathHistoryBuckets for an angle. It
// reads the mean sine and mean cosine of path per short window, optionally the
// mean of a weight path (wind speed) over the same windows, and returns one
// vector-mean bucket per widthFor bucket. The Mean of each merged bucket is in
// radians, and nil when the bucket has no direction. Min and Max stay nil: an
// angle has no linear extremes.
//
// Weighting is on when weightPath is given and has data in the range. With
// none logged the mean is unweighted and Weighted is false, which callers say
// aloud. That is a different method with the same meaning, not a stand-in for
// a failed read: a failed read is an error.
func queryCircularHistoryBuckets(ctx context.Context, io pathHistoryIO, path, source, weightPath, weightSource string, start, stop time.Time, fitToData bool, widthFor func(time.Duration) (string, time.Duration)) (pathHistoryBuckets, error) {
	out, err := planPathHistoryRange(ctx, io, path, source, start, stop, fitToData, widthFor)
	if err != nil {
		return out, err
	}
	subLabel, _ := circularSubWidth(out.Width)

	type job struct {
		path, source, fn string
	}
	jobs := []job{{path, source, "sin_mean"}, {path, source, "cos_mean"}}
	if weightPath != "" {
		jobs = append(jobs, job{weightPath, weightSource, "mean"})
	}
	pts := make([][]telemetryPoint, len(jobs))
	errs := make([]error, len(jobs))
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pts[i], errs[i] = io.stat(ctx, j.path, j.source, out.QueryStart, out.QueryStop, subLabel, j.fn)
		}()
	}
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			return out, e
		}
	}

	cos := map[int64]float64{}
	for _, p := range pts[1] {
		cos[p.Timestamp.Unix()] = p.Value
	}
	weights := map[int64]float64{}
	out.Weighted = weightPath != "" && len(pts[2]) > 0
	if out.Weighted {
		for _, p := range pts[2] {
			weights[p.Timestamp.Unix()] = p.Value
		}
	}
	var samples []circularSample
	for _, p := range pts[0] {
		k := p.Timestamp.Unix()
		c, ok := cos[k]
		if !ok {
			continue
		}
		s := circularSample{T: p.Timestamp, Sin: p.Value, Cos: c, Weight: 1}
		if out.Weighted {
			w, ok := weights[k]
			if !ok {
				// A window with a direction and no speed has no weight to give.
				continue
			}
			s.Weight = math.Max(w, 0)
		}
		samples = append(samples, s)
	}

	for _, b := range combineCircular(samples, out.Width) {
		pt := assistantPathHistoryPoint{T: b.T, Weight: b.Weight}
		if b.Defined {
			m, r := b.Mean, b.Resultant
			pt.Mean, pt.Resultant = &m, &r
		} else {
			out.Undefined++
		}
		out.Merged = append(out.Merged, pt)
	}
	if !fitToData {
		out.Found = len(out.Merged) > 0
	}
	out.Gaps, out.GapCount = computePathHistoryGaps(out.QueryStart, stop, out.Width, out.Merged)
	return out, nil
}

// overallCircularMean is the mean direction of a range: the buckets' vectors
// combined, each weighted by its own weight. ok is false when they cancel out
// or there are none.
func overallCircularMean(points []assistantPathHistoryPoint) (radians, resultant float64, ok bool) {
	var sumW, sumS, sumC float64
	for _, p := range points {
		if p.Mean == nil || p.Resultant == nil {
			continue
		}
		w := p.Weight
		sumW += w
		sumS += w * *p.Resultant * math.Sin(*p.Mean)
		sumC += w * *p.Resultant * math.Cos(*p.Mean)
	}
	if sumW <= 0 {
		return 0, 0, false
	}
	return circularMean(sumS/sumW, sumC/sumW)
}
