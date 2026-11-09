package main

import (
	"fmt"
	"math"
	"sort"
	"time"

	"gonum.org/v1/gonum/stat/distuv"
)

// Local tide calibration at anchor (ADR 0274). While an anchor watch is set the
// server records the sounder depth and the boat's position once a minute, and
// fits
//
//	depth = seabed[cell] + k × tide(t + offset)
//
// to them, where tide is the station's prediction, a cell is a 2.5 m square of
// seabed in a local east/north frame from the anchor, k is how much of the
// station's range the water here has, and offset is how far ahead the station's
// curve is read: a positive offset means the local tide runs earlier than the
// station's, a negative one that it runs later. The seabed figure of each cell and k are fitted
// by Huber-weighted least squares; k comes from the readings taken over the same
// cell at different heights of tide, so a swinging boat separates tide from
// seabed. The model, its backtest and the reasons for every choice here are in
// docs/research/local-tide-calibration.md and the ADR.
//
// Nothing here falls back. A fit that cannot be made, a tide curve that does not
// reach a time, or a k with no detectable tide in it comes back as an error with
// a code the caller shows.
//
// The constants below are the choices the operator or the design made. Every
// other threshold is derived from the data or from a rate the server already
// has: the sounder's resolution, the poll interval, how stale a depth may be.

const (
	// tideCalCellM is the side of a seabed cell.
	tideCalCellM = 2.5
	// tideCalRecordInterval is how often a reading is stored.
	tideCalRecordInterval = time.Minute
	// The time offset is searched across plus and minus tideCalOffsetSearchSpan,
	// coarsely first and then at the recording interval around the best coarse
	// offset.
	tideCalOffsetSearchSpan = 30 * time.Minute
	tideCalOffsetCoarseStep = 5 * tideCalRecordInterval
	// Huber's tuning constant for 95% efficiency on clean data.
	tideCalHuberC         = 1.345
	tideCalIRLSIterations = 40
	// tideCalCautionM is the operator-agreed 0.2 m. The verdict adds it to the
	// shallowest sounded cell because the boat may never lie on that exact spot
	// at low water, and the page mentions a shallowest or deepest spot that
	// differs from the boat's own cell by this much (backtest, ADR 0274).
	tideCalCautionM = 0.2
)

// tideCalRecencyLimit is how old the latest stored minute may be for the boat's
// own cell to count: the sounder's staleness limit, plus the minute it takes to
// store a reading.
const tideCalRecencyLimit = vesselStatusFreshAge + tideCalRecordInterval

// Why a calibration is not ready. Each is a word the page and Mate turn into
// sentences.
const (
	tideCalTooFewReadings  = "too_few_readings"
	tideCalTooScattered    = "readings_too_scattered"
	tideCalImplausibleFit  = "implausible_fit"
	tideCalNoRecentReading = "no_recent_reading"
	tideCalNoTideCurve     = "no_tide_curve"
	tideCalNoClearanceSet  = "no_clearance_set"
	tideCalWorking         = "working_it_out"
	// tideCalNoUpcomingHigh is a high-water prediction's own reason, never the
	// calibration's.
	tideCalNoUpcomingHigh = "no_upcoming_high"
	tideCalUnavailable    = "unavailable"
)

// tideCalError is a calibration that is not ready, with the code for why.
type tideCalError struct {
	Code   string
	Detail string
	// HoursUsed is how long the readings that reached the fit span, when the fit
	// failed after the readings were filtered.
	HoursUsed float64
}

func (e *tideCalError) Error() string {
	if e.Detail == "" {
		return "tide calibration: " + e.Code
	}
	return "tide calibration: " + e.Code + ": " + e.Detail
}

func tideCalErr(code, format string, args ...any) *tideCalError {
	return &tideCalError{Code: code, Detail: fmt.Sprintf(format, args...)}
}

// tideCalReading is one stored minute: the robust depth for the minute at the
// GPS antenna's position.
type tideCalReading struct {
	At       time.Time
	Lat, Lon float64
	DepthM   float64
}

// tideCurve is the station's predicted height between its highs and lows,
// cosine-interpolated as interpolateTideNow does, and unknown outside them.
type tideCurve []tideExtremePoint

func newTideCurve(extremes []tideExtremePoint) tideCurve {
	c := make(tideCurve, len(extremes))
	copy(c, extremes)
	sort.Slice(c, func(i, j int) bool { return c[i].Time.Before(c[j].Time) })
	return c
}

// heightAt is the predicted height at t, false when t is outside the extremes.
func (c tideCurve) heightAt(t time.Time) (float64, bool) {
	if len(c) < 2 || t.Before(c[0].Time) || t.After(c[len(c)-1].Time) {
		return 0, false
	}
	i := sort.Search(len(c), func(i int) bool { return !c[i].Time.Before(t) })
	if i == 0 {
		return c[0].HeightM, true
	}
	return cosineTideHeight(&c[i-1], &c[i], t), true
}

// tideCalNoiseWindow is how far either side of a reading the running median that
// stands for the tide reaches, when the stay's noise level is measured.
const tideCalNoiseWindow = 10 * time.Minute

// noiseFloor is the stay's overall noise level: the robust scale (1.4826 × MAD)
// of the readings' deviations from a straight line fitted to the minutes around
// each (tideCalNoiseWindow either side), which is the slow tide taken out. A
// sounder that reports in steps of 0.1 m shows up as a scatter of about a
// twentieth of a metre. It is the floor of the solver's scale, so a run of
// readings that agree exactly does not make every other reading an outlier. A
// series with no scatter gives the smallest positive floor, so the solver still
// divides cleanly. rs must be in time order.
func noiseFloor(rs []tideCalReading) float64 {
	var dev []float64
	lo, hi := 0, 0
	for i, r := range rs {
		for r.At.Sub(rs[lo].At) > tideCalNoiseWindow {
			lo++
		}
		for hi+1 < len(rs) && rs[hi+1].At.Sub(r.At) <= tideCalNoiseWindow {
			hi++
		}
		if hi-lo+1 < 5 {
			continue
		}
		// Least-squares line through the window, in minutes from this reading.
		var n, sx, sy, sxx, sxy float64
		for _, w := range rs[lo : hi+1] {
			x := w.At.Sub(r.At).Minutes()
			n++
			sx += x
			sy += w.DepthM
			sxx += x * x
			sxy += x * w.DepthM
		}
		den := n*sxx - sx*sx
		fitted := sy / n
		if den > 1e-9 {
			slope := (n*sxy - sx*sy) / den
			fitted = (sy - slope*sx) / n // the line's value at x = 0
		}
		dev = append(dev, rs[i].DepthM-fitted)
	}
	if len(dev) == 0 {
		return 1e-6
	}
	return math.Max(1.4826*medianAbsoluteDeviation(dev, median(dev)), 1e-6)
}

type tideCalCell struct{ E, N int }

// tideCalCellAt is the cell of a position in the frame whose origin is the
// anchor.
func tideCalCellAt(anchorLat, anchorLon, lat, lon float64) tideCalCell {
	east, north := localENMeters(anchorLat, anchorLon, lat, lon)
	return tideCalCell{E: int(math.Floor(east / tideCalCellM)), N: int(math.Floor(north / tideCalCellM))}
}

// tideCalCellStat is what the fit knows of one cell.
type tideCalCellStat struct {
	SeabedM float64 // depth at tide height zero
	HBar    float64 // weighted mean tide height of the readings in the cell
	NEff    float64 // effective number of readings, after the Huber weights
}

// tideCalMember is the fit at one time offset.
type tideCalMember struct {
	OffsetMin int
	K         float64
	KHalf     float64 // half width of the 95% interval on K
	VarK      float64
	TQ        float64 // the two-sided 95% Student's t multiplier for the degrees of freedom
	Sigma     float64
	Inflation float64 // (1+rho)/(1-rho): how much correlated minutes inflate the variance
	Cells     map[tideCalCell]*tideCalCellStat
}

// tideCalFit is a finished fit. Members[0] is the offset that fits best. The
// others are the offsets the readings cannot yet tell from it, at the 95% level;
// a prediction's interval takes in all of them, so an offset the readings have
// not settled widens the interval instead of being quietly picked.
type tideCalFit struct {
	*tideCalMember
	Members  []*tideCalMember
	LastCell tideCalCell
	FirstAt  time.Time
	LastAt   time.Time
	Curve    tideCurve
}

// Hours is how long the readings the fit used span: the hours of readings the
// page and Mate report.
func (f *tideCalFit) Hours() float64 { return f.LastAt.Sub(f.FirstAt).Hours() }

type tideCalRow struct {
	cell int
	d    float64
	at   time.Time
}

type tideCalSolution struct {
	k        float64
	seabed   []float64
	hbar     []float64
	den      float64
	resid    []float64
	weights  []float64
	heights  []float64 // the curve's height at each row at this offset
	scale    float64
	offsetMn int
	loss     float64 // the Huber loss at the fit's threshold, set when solved
}

// selectMedian is the median of xs by selection, using buf (the same length) as the
// workspace so xs is left alone and nothing is allocated or sorted.
func selectMedian(buf, xs []float64) float64 {
	n := len(xs)
	if n == 0 {
		return 0
	}
	copy(buf, xs)
	k := n / 2
	hiv := selectKth(buf, k)
	if n%2 == 1 {
		return hiv
	}
	// An even count averages the two middle values; after selection everything
	// left of k is no larger, so the lower middle is the largest of them.
	lo := math.Inf(-1)
	for _, v := range buf[:k] {
		if v > lo {
			lo = v
		}
	}
	return (lo + hiv) / 2
}

// selectKth partially orders a so that a[k] is the value a full sort would put
// there, with no larger value to its left and no smaller to its right (quickselect,
// median of three), and returns it.
func selectKth(a []float64, k int) float64 {
	lo, hi := 0, len(a)-1
	for lo < hi {
		mid := lo + (hi-lo)/2
		if a[mid] < a[lo] {
			a[mid], a[lo] = a[lo], a[mid]
		}
		if a[hi] < a[lo] {
			a[hi], a[lo] = a[lo], a[hi]
		}
		if a[hi] < a[mid] {
			a[hi], a[mid] = a[mid], a[hi]
		}
		pivot := a[mid]
		i, j := lo, hi
		for i <= j {
			for a[i] < pivot {
				i++
			}
			for a[j] > pivot {
				j--
			}
			if i <= j {
				a[i], a[j] = a[j], a[i]
				i++
				j--
			}
		}
		switch {
		case k <= j:
			hi = j
		case k >= i:
			lo = i
		default:
			return a[k]
		}
	}
	return a[k]
}

// tideCalScratch holds the buffers the solver reuses between offsets.
type tideCalScratch struct{ h, abs, sel, sw, swh, swd []float64 }

func newTideCalScratch(rows, cells int) *tideCalScratch {
	return &tideCalScratch{h: make([]float64, rows), abs: make([]float64, rows), sel: make([]float64, rows), sw: make([]float64, cells), swh: make([]float64, cells), swd: make([]float64, cells)}
}

// solveAt fits k and the seabed figures at one time offset by iteratively
// reweighted least squares with Huber weights. The weights start from w0, the
// previous offset's, when given, since neighbouring offsets weigh the readings
// alike. The scale never falls below floorM, the stay's noise level.
//
// heights0 are the curve's heights at this offset for the first rows, when an
// earlier fit already read them off the same curve; only the rest are looked up.
func solveAt(rows []tideCalRow, nCells int, curve tideCurve, offsetMin int, floorM float64, sc *tideCalScratch, w0, heights0 []float64) (*tideCalSolution, bool) {
	h := sc.h
	known := copy(h, heights0)
	for i := known; i < len(rows); i++ {
		r := rows[i]
		v, ok := curve.heightAt(r.at.Add(time.Duration(offsetMin) * time.Minute))
		if !ok {
			return nil, false
		}
		h[i] = v
	}
	heights := append([]float64(nil), h...)
	w := make([]float64, len(rows))
	if w0 != nil {
		copy(w, w0)
	} else {
		for i := range w {
			w[i] = 1
		}
	}
	sol := &tideCalSolution{seabed: make([]float64, nCells), hbar: make([]float64, nCells), resid: make([]float64, len(rows)), offsetMn: offsetMin, heights: heights}
	sw, swh, swd, abs := sc.sw, sc.swh, sc.swd, sc.abs
	prevK := math.NaN()
	for iter := 0; iter < tideCalIRLSIterations; iter++ {
		for c := range sw {
			sw[c], swh[c], swd[c] = 0, 0, 0
		}
		for i, r := range rows {
			sw[r.cell] += w[i]
			swh[r.cell] += w[i] * h[i]
			swd[r.cell] += w[i] * r.d
		}
		var num, den float64
		for i, r := range rows {
			hb, db := swh[r.cell]/sw[r.cell], swd[r.cell]/sw[r.cell]
			num += w[i] * (h[i] - hb) * (r.d - db)
			den += w[i] * (h[i] - hb) * (h[i] - hb)
		}
		if den < 1e-9 {
			return nil, false
		}
		k := num / den
		for c := range sw {
			if sw[c] > 0 {
				sol.hbar[c] = swh[c] / sw[c]
				sol.seabed[c] = swd[c]/sw[c] - k*sol.hbar[c]
			}
		}
		for i, r := range rows {
			sol.resid[i] = r.d - sol.seabed[r.cell] - k*h[i]
			abs[i] = math.Abs(sol.resid[i])
		}
		scale := math.Max(1.4826*selectMedian(sc.sel, abs), floorM)
		for i := range w {
			if a := abs[i]; a <= tideCalHuberC*scale {
				w[i] = 1
			} else {
				w[i] = tideCalHuberC * scale / a
			}
		}
		sol.k, sol.den, sol.scale = k, den, scale
		if !math.IsNaN(prevK) && math.Abs(k-prevK) < 1e-7 {
			break
		}
		prevK = k
	}
	sol.weights = w
	return sol, true
}

// huberLoss is the fit's cost at a given threshold, for comparing offsets.
func (s *tideCalSolution) huberLoss(delta float64) float64 {
	var loss float64
	for _, r := range s.resid {
		a := math.Abs(r)
		if a <= delta {
			loss += 0.5 * a * a
		} else {
			loss += delta * (a - 0.5*delta)
		}
	}
	return loss
}

// fitTideCalibration fits the readings against the station's tide curve. The
// anchor is the origin of the cell frame. Every fit searches the whole offset
// span, so the offsets that stay plausible are decided by the readings alone.
// Its errors are *tideCalError.
func fitTideCalibration(readings []tideCalReading, anchorLat, anchorLon float64, curve tideCurve) (*tideCalFit, *tideCalError) {
	fit, err, _ := fitTideCalibrationWarm(readings, anchorLat, anchorLon, curve, "", nil)
	return fit, err
}

// tideCalWarm is what a fit leaves for the next one on the same stay: the Huber
// weights it ended with at each offset it solved, and the readings they belong
// to. A minute later the readings are the same ones with another at the end, so
// those weights are almost the answer and the solver needs a round or two, not
// forty. The fit it starts is the same optimum a cold fit reaches: the weights
// only choose where the iteration begins.
type tideCalWarm struct {
	key             string // the chart (and so the curve) the weights were found on
	lat, lon        float64
	firstAt, lastAt time.Time
	n               int
	weights         map[int][]float64 // by offset, minutes
	heights         map[int][]float64 // the curve's height at each row, by offset
}

// startHeights are the heights the previous fit read off the curve at this
// offset, for the readings it had.
func (w *tideCalWarm) startHeights(offsetMin int) []float64 {
	if w == nil {
		return nil
	}
	return w.heights[offsetMin]
}

// startWeights are the previous fit's weights at this offset, extended with 1
// for the minutes added since, or nil if there are none for it.
func (w *tideCalWarm) startWeights(offsetMin, rows int) []float64 {
	if w == nil {
		return nil
	}
	prev, ok := w.weights[offsetMin]
	if !ok {
		return nil
	}
	out := make([]float64, rows)
	n := copy(out, prev)
	for i := n; i < rows; i++ {
		out[i] = 1
	}
	return out
}

// fitTideCalibrationWarm is fitTideCalibration started from the previous fit's
// weights when those belong to these readings: same key (the chart), same
// anchor, and the same readings with minutes added at the end. Otherwise it fits
// cold. It returns what the next fit can start from.
func fitTideCalibrationWarm(readings []tideCalReading, anchorLat, anchorLon float64, curve tideCurve, key string, warm *tideCalWarm) (*tideCalFit, *tideCalError, *tideCalWarm) {
	if len(readings) == 0 {
		return nil, tideCalErr(tideCalTooFewReadings, "no readings"), nil
	}
	sorted := append([]tideCalReading(nil), readings...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].At.Before(sorted[j].At) })
	floor := noiseFloor(sorted)

	// Every offset is fitted to the same readings, so one the curve does not
	// reach at either end of the search drops the reading for all of them.
	var use []tideCalReading
	for _, r := range sorted {
		if _, ok := curve.heightAt(r.At.Add(-tideCalOffsetSearchSpan)); !ok {
			continue
		}
		if _, ok := curve.heightAt(r.At.Add(tideCalOffsetSearchSpan)); !ok {
			continue
		}
		use = append(use, r)
	}
	if len(use) == 0 {
		return nil, tideCalErr(tideCalNoTideCurve, "the station's tide curve does not reach the readings"), nil
	}

	cellIndex := map[tideCalCell]int{}
	var cells []tideCalCell
	rows := make([]tideCalRow, 0, len(use))
	for _, r := range use {
		c := tideCalCellAt(anchorLat, anchorLon, r.Lat, r.Lon)
		idx, ok := cellIndex[c]
		if !ok {
			idx = len(cells)
			cellIndex[c] = idx
			cells = append(cells, c)
		}
		rows = append(rows, tideCalRow{cell: idx, d: r.DepthM, at: r.At})
	}
	usedHours := use[len(use)-1].At.Sub(use[0].At).Hours()
	failed := func(code, format string, args ...any) (*tideCalFit, *tideCalError, *tideCalWarm) {
		e := tideCalErr(code, format, args...)
		e.HoursUsed = usedHours
		return nil, e, nil
	}
	if len(rows)-len(cells)-1 < 1 {
		return failed(tideCalTooFewReadings, "%d readings over %d cells", len(rows), len(cells))
	}

	// The previous fit's weights stand only for the same chart, the same anchor
	// and the same readings with minutes added at the end.
	if warm != nil && !(warm.key == key && warm.lat == anchorLat && warm.lon == anchorLon &&
		warm.n > 0 && warm.n <= len(rows) && rows[0].at.Equal(warm.firstAt) && rows[warm.n-1].at.Equal(warm.lastAt)) {
		warm = nil
	}
	next := &tideCalWarm{key: key, lat: anchorLat, lon: anchorLon, firstAt: rows[0].at, lastAt: rows[len(rows)-1].at, n: len(rows), weights: map[int][]float64{}, heights: map[int][]float64{}}

	sc := newTideCalScratch(len(rows), len(cells))
	base, ok := solveAt(rows, len(cells), curve, 0, floor, sc, warm.startWeights(0, len(rows)), warm.startHeights(0))
	if !ok {
		return failed(tideCalTooFewReadings, "no cell has been crossed at two heights of tide")
	}
	delta := tideCalHuberC * base.scale
	base.loss = base.huberLoss(delta)
	next.weights[0], next.heights[0] = base.weights, base.heights

	// Coarse offsets across the span, then every recording interval around the
	// best of them.
	sols := []*tideCalSolution{base}
	solved := map[int]bool{0: true}
	span := int(tideCalOffsetSearchSpan / time.Minute)
	coarse := int(tideCalOffsetCoarseStep / time.Minute)
	step := int(tideCalRecordInterval / time.Minute)
	best := base
	try := func(off int, from *tideCalSolution) {
		if solved[off] || off < -span || off > span {
			return
		}
		solved[off] = true
		start := warm.startWeights(off, len(rows))
		if start == nil {
			start = from.weights
		}
		if s, ok := solveAt(rows, len(cells), curve, off, floor, sc, start, warm.startHeights(off)); ok {
			s.loss = s.huberLoss(delta)
			next.weights[off], next.heights[off] = s.weights, s.heights
			sols = append(sols, s)
			if s.loss < best.loss {
				best = s
			}
		}
	}
	for off := -span; off <= span; off += coarse {
		try(off, base)
	}
	bestCoarse := best
	for off := bestCoarse.offsetMn - coarse + step; off < bestCoarse.offsetMn+coarse; off += step {
		try(off, bestCoarse)
	}

	bestMember := finishMember(best, rows, cells, cellIndex)
	// An offset is still possible when it fits no worse than the best by the
	// likelihood-ratio limit: 2(L - Lbest) / (sigma^2 inflation) <= 3.84, the 95%
	// point of chi-squared with one degree of freedom.
	plausible := []*tideCalMember{bestMember}
	for _, s := range sols {
		if s != best && 2*(s.loss-best.loss) <= 3.84*bestMember.Sigma*bestMember.Sigma*bestMember.Inflation {
			plausible = append(plausible, finishMember(s, rows, cells, cellIndex))
		}
	}

	fit := &tideCalFit{tideCalMember: bestMember, Members: plausible, Curve: curve, FirstAt: use[0].At}
	lastUse := use[len(use)-1]
	fit.LastCell = tideCalCellAt(anchorLat, anchorLon, lastUse.Lat, lastUse.Lon)
	fit.LastAt = lastUse.At
	return fit, nil, next
}

// finishMember turns a solution into a member: the cell figures, and the
// variances of k and of each cell's seabed.
//
// Minutes are not independent: the boat lingers and the sounder drifts, so the
// residuals of neighbouring minutes agree. Their lag-1 autocorrelation rho gives
// an effective sample size n(1-rho)/(1+rho), and the variances are inflated by
// its inverse, (1+rho)/(1-rho). Residuals more than twice the recording interval
// apart are not neighbours.
func finishMember(s *tideCalSolution, rows []tideCalRow, cells []tideCalCell, cellIndex map[tideCalCell]int) *tideCalMember {
	var num, den float64
	for i := 0; i+1 < len(rows); i++ {
		if rows[i+1].at.Sub(rows[i].at) > 2*tideCalRecordInterval {
			continue
		}
		num += s.resid[i] * s.resid[i+1]
	}
	for _, r := range s.resid {
		den += r * r
	}
	rho := 0.0
	if den > 0 {
		rho = math.Min(math.Max(num/den, 0), 0.95)
	}
	inflation := (1 + rho) / (1 - rho)
	dof0 := float64(len(rows) - len(cells) - 1)
	var wr2 float64
	for i, r := range s.resid {
		wr2 += s.weights[i] * r * r
	}
	sigma2 := wr2 / dof0

	dof := math.Max(dof0/inflation, 1)
	tq := distuv.StudentsT{Mu: 0, Sigma: 1, Nu: dof}.Quantile(0.975)
	varK := sigma2 * inflation / s.den
	m := &tideCalMember{
		OffsetMin: s.offsetMn, K: s.k, VarK: varK, KHalf: tq * math.Sqrt(varK), TQ: tq,
		Sigma: math.Sqrt(sigma2), Inflation: inflation,
		Cells: make(map[tideCalCell]*tideCalCellStat, len(cells)),
	}
	sw, sw2 := make([]float64, len(cells)), make([]float64, len(cells))
	for i, r := range rows {
		sw[r.cell] += s.weights[i]
		sw2[r.cell] += s.weights[i] * s.weights[i]
	}
	for c, idx := range cellIndex {
		m.Cells[c] = &tideCalCellStat{SeabedM: s.seabed[idx], HBar: s.hbar[idx], NEff: sw[idx] * sw[idx] / sw2[idx]}
	}
	return m
}

// tideCalPrediction is a depth with the half-width of its 95% interval, and the
// local time of the extreme it is the depth at.
type tideCalPrediction struct {
	ValueM     float64
	HalfWidthM float64
	At         time.Time
}

func (p tideCalPrediction) BottomM() float64 { return p.ValueM - p.HalfWidthM }

// predict is the depth over a cell at the station's extreme at t. The model is
// depth = seabed + k × tide(t + offset), so the local tide reaches the station's
// extreme at t - offset, and there the depth is seabed + k × the extreme's
// height. The value is the best offset's, and so is the local time. The
// half-width is that offset's 95% interval, widened to take in the value and
// interval of every offset the readings cannot rule out.
func (f *tideCalFit) predict(cell tideCalCell, t time.Time) (tideCalPrediction, *tideCalError) {
	h, ok := f.Curve.heightAt(t)
	if !ok {
		return tideCalPrediction{}, tideCalErr(tideCalNoTideCurve, "the station's tide curve does not reach %s", t.UTC().Format(time.RFC3339))
	}
	var best tideCalPrediction
	for i, m := range f.Members {
		c := m.Cells[cell]
		dh := h - c.HBar
		variance := m.Sigma*m.Sigma*m.Inflation/c.NEff + dh*dh*m.VarK
		p := tideCalPrediction{ValueM: c.SeabedM + m.K*h, HalfWidthM: m.TQ * math.Sqrt(variance)}
		if i == 0 {
			p.At = t.Add(-time.Duration(m.OffsetMin) * time.Minute)
			best = p
			continue
		}
		best.HalfWidthM = math.Max(best.HalfWidthM, math.Abs(p.ValueM-best.ValueM)+p.HalfWidthM)
	}
	return best, nil
}

// extremeCell finds the shallowest (or deepest) cell sounded and its figure at
// t. Cells are ranked by the side of their interval the operator can be sure of:
// the shallowest has the lowest upper bound, the deepest the highest lower bound.
// A cell crossed once by GPS jitter has a wide interval, so it does not win on a
// lucky value.
func (f *tideCalFit) extremeCell(shallowest bool, t time.Time) (tideCalPrediction, *tideCalError) {
	// Map order is random; ties break on position so the answer is repeatable.
	keys := make([]tideCalCell, 0, len(f.Cells))
	for c := range f.Cells {
		keys = append(keys, c)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].E != keys[j].E {
			return keys[i].E < keys[j].E
		}
		return keys[i].N < keys[j].N
	})
	var best tideCalPrediction
	for i, c := range keys {
		p, err := f.predict(c, t)
		if err != nil {
			return tideCalPrediction{}, err
		}
		if i == 0 || (shallowest && p.ValueM+p.HalfWidthM < best.ValueM+best.HalfWidthM) || (!shallowest && p.BottomM() > best.BottomM()) {
			best = p
		}
	}
	return best, nil
}

// nextLocal finds the station's next low and next high whose local time, the
// station's less the offset, is still ahead of now. It walks the extremes the
// stay has stored, so a local low that falls after a station low that has already
// passed is not skipped, and a local time that has gone by is never reported. A
// zero time means no such extreme in the stored span.
func (f *tideCalFit) nextLocal(now time.Time) (low, high time.Time) {
	shift := time.Duration(f.OffsetMin) * time.Minute
	for _, e := range f.Curve {
		if !e.Time.Add(-shift).After(now) {
			continue
		}
		if e.High && high.IsZero() {
			high = e.Time
		} else if !e.High && low.IsZero() {
			low = e.Time
		}
	}
	return low, high
}

// tideCalEstimate is a ready calibration's prediction for the next low and high.
// The high is separate: when it can't be predicted, HighUnavailable says why and
// the low stands.
type tideCalEstimate struct {
	Fit             *tideCalFit
	LowHere         tideCalPrediction // the boat's own cell at the next low
	LowShallowest   tideCalPrediction
	HighHere        *tideCalPrediction // nil when HighUnavailable is set
	HighDeepest     *tideCalPrediction
	HighUnavailable *tideCalError
	ShallowestApart bool // the shallowest spot differs from LowHere enough to say so
	DeepestApart    bool
}

// estimate predicts the next low and high from a finished fit. It is ready, and
// returns an estimate, when the 95% interval on the next-low prediction for the
// boat's own cell is narrower than marginM, the clearance the operator wants.
// Otherwise it returns the reason it is not. lowAt and highAt are the station's
// extremes; highAt may be the zero time when there is no upcoming high.
func (f *tideCalFit) estimate(lowAt, highAt time.Time, marginM float64, now time.Time) (*tideCalEstimate, *tideCalError) {
	// Readiness is the interval against the operator's clearance. With none set
	// there is nothing to judge it by, and nothing stands in for it.
	if marginM <= 0 {
		return nil, tideCalErr(tideCalNoClearanceSet, "")
	}
	// No detectable tide in the readings: k's 95% interval takes in zero.
	if f.K <= f.KHalf {
		return nil, tideCalErr(tideCalImplausibleFit, "k = %.2f, 95%% interval ±%.2f", f.K, f.KHalf)
	}
	if _, ok := f.Cells[f.LastCell]; !ok || now.Sub(f.LastAt) > tideCalRecencyLimit {
		return nil, tideCalErr(tideCalNoRecentReading, "")
	}
	est := &tideCalEstimate{Fit: f}
	var err *tideCalError
	if est.LowHere, err = f.predict(f.LastCell, lowAt); err != nil {
		return nil, err
	}
	if est.LowShallowest, err = f.extremeCell(true, lowAt); err != nil {
		return nil, err
	}
	est.ShallowestApart = est.LowHere.ValueM-est.LowShallowest.ValueM >= tideCalCautionM

	// The low is judged first. A high that can't be predicted leaves the high
	// empty with its own reason and does not hold the low back.
	switch {
	case est.LowHere.HalfWidthM < marginM:
	case 2*f.Sigma > marginM:
		// One reading's own scatter is wider than the margin, so more readings
		// narrow the interval slowly: the sounder is the problem.
		return nil, tideCalErr(tideCalTooScattered, "")
	default:
		return nil, tideCalErr(tideCalTooFewReadings, "")
	}
	est.HighUnavailable = est.predictHigh(highAt)
	return est, nil
}

// predictHigh fills the high-water figures, or says why it can't.
func (est *tideCalEstimate) predictHigh(highAt time.Time) *tideCalError {
	if highAt.IsZero() {
		return tideCalErr(tideCalNoUpcomingHigh, "")
	}
	f := est.Fit
	here, err := f.predict(f.LastCell, highAt)
	if err != nil {
		return err
	}
	deepest, err := f.extremeCell(false, highAt)
	if err != nil {
		return err
	}
	est.HighHere, est.HighDeepest = &here, &deepest
	est.DeepestApart = deepest.ValueM-here.ValueM >= tideCalCautionM
	return nil
}

// estimateTideCalibration fits the readings and estimates, for callers that
// hold the readings themselves. The error is a *tideCalError.
func estimateTideCalibration(readings []tideCalReading, anchorLat, anchorLon float64, curve tideCurve, lowAt, highAt time.Time, marginM float64, now time.Time) (*tideCalEstimate, error) {
	fit, err := fitTideCalibration(readings, anchorLat, anchorLon, curve)
	if err != nil {
		return nil, err
	}
	est, err := fit.estimate(lowAt, highAt, marginM, now)
	if err != nil {
		return nil, err
	}
	return est, nil
}
