package main

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// The motion accumulator turns the attitude sensor's 10 Hz samples into the
// figures a track point keeps about snap and pounding (ADR 0295). It does no
// I/O: the motion stream feeds it, the track recorder takes the window.
//
// Each signal is first laid on an even 10 Hz grid from the sensor's own
// timestamps, because arrival gaps jitter from 13 to 274 ms and a filter that
// assumed even arrival would read that jitter as motion. A Savitzky-Golay
// filter then gives the derivatives: a cubic over 11 samples (1.0 s) for the
// first and second, a quartic over 15 samples (1.5 s) for the third. The
// derivative at each grid point is centred, so it lags the newest sample by
// half the window.

const (
	motionGridStep = 100 * time.Millisecond
	// motionMaxGap is the longest silence the grid bridges. Past it the signal
	// is not continuous, nothing is interpolated across it, and the filter
	// starts again. The worst spacing measured between update timestamps of
	// the 10 Hz sensor was 274 ms, so 450 ms clears that with room; a stretch
	// slower than about 2 Hz breaks continuity instead of being interpolated
	// into 10 Hz.
	motionMaxGap = 450 * time.Millisecond
	// motionBackStep is how far the sensor's clock may step back before the
	// channel restarts instead of dropping samples.
	motionBackStep = 2 * time.Second
	// motionMaxWindow caps what one window may cover. A recorder that keeps no
	// point (a rejected position fix for days) must not grow it without limit,
	// and a point's figures then describe at most the last window.
	motionMaxWindow = 15 * time.Minute
	// motionMinRateHz is the slowest attitude stream the derivatives trust. A
	// jerk taken from 4 Hz samples is a number about the interpolation, not the
	// boat.
	motionMinRateHz = 8.0

	motionSmoothWindow, motionSmoothOrder = 11, 3
	motionJerkWindow, motionJerkOrder     = 15, 4
)

var (
	sgRate  = sgCoefficients(motionSmoothWindow, motionSmoothOrder, 1)
	sgAccel = sgCoefficients(motionSmoothWindow, motionSmoothOrder, 2)
	sgJerk  = sgCoefficients(motionJerkWindow, motionJerkOrder, 3)
)

// sgCoefficients returns the Savitzky-Golay weights for the deriv-th derivative
// at the centre of a window of odd length, fitted by least squares with a
// polynomial of the given order. The window runs oldest sample first, in
// sample units: divide the weighted sum by step^deriv for a derivative per
// second.
func sgCoefficients(window, order, deriv int) []float64 {
	if window%2 == 0 || order >= window || deriv > order {
		panic(fmt.Sprintf("sgCoefficients(%d, %d, %d): impossible filter", window, order, deriv))
	}
	half := (window - 1) / 2
	n := order + 1

	// Normal equations G = A^T A, with A[i][j] = x_i^j and x from -half to half.
	g := make([][]float64, n)
	for r := range g {
		g[r] = make([]float64, 2*n)
		for c := 0; c < n; c++ {
			for i := 0; i < window; i++ {
				g[r][c] += math.Pow(float64(i-half), float64(r+c))
			}
		}
		g[r][n+r] = 1
	}
	// Gauss-Jordan with partial pivoting leaves G^-1 in the right half.
	for col := 0; col < n; col++ {
		pivot := col
		for r := col + 1; r < n; r++ {
			if math.Abs(g[r][col]) > math.Abs(g[pivot][col]) {
				pivot = r
			}
		}
		if g[pivot][col] == 0 {
			panic("sgCoefficients: singular normal matrix")
		}
		g[col], g[pivot] = g[pivot], g[col]
		p := g[col][col]
		for c := range g[col] {
			g[col][c] /= p
		}
		for r := 0; r < n; r++ {
			if r == col {
				continue
			}
			f := g[r][col]
			for c := range g[r] {
				g[r][c] -= f * g[col][c]
			}
		}
	}

	factorial := 1.0
	for k := 2; k <= deriv; k++ {
		factorial *= float64(k)
	}
	w := make([]float64, window)
	for i := range w {
		var s float64
		for j := 0; j < n; j++ {
			s += g[deriv][n+j] * math.Pow(float64(i-half), float64(j))
		}
		w[i] = factorial * s
	}
	return w
}

// motionStep is one grid sample of a channel with whatever derivatives the
// filter has enough history for.
type motionStep struct {
	value                      float64
	rate, accel, jerk          float64
	hasRate, hasAccel, hasJerk bool
}

// motionChannel resamples one signal onto the grid and filters it.
type motionChannel struct {
	// maxGap is the longest silence the channel bridges, set when the accumulator
	// is built.
	maxGap   time.Duration
	have     bool
	prevT    time.Time
	prevV    float64
	nextGrid time.Time
	hist     []float64 // grid values of the current unbroken run, oldest first
	// cells holds, for each value in hist, how many raw samples landed in the
	// grid step ending at it. A derivative trusts only a span the sensor really
	// sampled quickly: a stretch bridged by interpolation has few.
	cells   []int
	pending int // raw samples since the last grid point
}

// sampleOutcome says what a channel did with a sample.
type sampleOutcome int

const (
	// sampleUsed: the sample joined the signal, or began a new run of it.
	sampleUsed sampleOutcome = iota
	// sampleDropped: the timestamp did not move forward, and the signal has no
	// second value at one instant.
	sampleDropped
	// sampleRestarted: the sensor's clock stepped back by more than
	// motionBackStep. The filter started again from this sample, because
	// dropping every sample until the old time passed would freeze the signal.
	sampleRestarted
)

// restart empties the filter and makes the next sample begin a new run.
func (c *motionChannel) restart() {
	c.have = false
	c.hist = c.hist[:0]
	c.cells = c.cells[:0]
	c.pending = 0
}

// spanSampledFast says whether the newest n grid steps hold enough raw samples
// to stand for an 8 Hz stream (at least 0.8 per step), over the whole span and
// over each half of it, so a slow stretch at either end cannot hide in a full
// one.
func (c *motionChannel) spanSampledFast(n int) bool {
	cells := c.cells[len(c.cells)-n:]
	sum := func(s []int) (t int) {
		for _, v := range s {
			t += v
		}
		return t
	}
	half := (n + 1) / 2
	return sum(cells) >= int(math.Ceil(0.8*float64(n))) &&
		sum(cells[:half]) >= int(0.8*float64(half)) &&
		sum(cells[n-half:]) >= int(0.8*float64(half))
}

// add takes one source sample and calls emit for each grid point it completes.
// continuedBy is the time since the previous sample when the sample continues
// an unbroken run, and zero when it begins one: the rate is measured over
// those continuous intervals only.
func (c *motionChannel) add(t time.Time, v float64, emit func(motionStep)) (outcome sampleOutcome, continuedBy time.Duration) {
	outcome = sampleUsed
	if c.have && !t.After(c.prevT) {
		if c.prevT.Sub(t) <= motionBackStep {
			return sampleDropped, 0
		}
		outcome = sampleRestarted
		c.restart()
	}
	if !c.have || t.Sub(c.prevT) > c.maxGap {
		c.restart()
		c.have, c.prevT, c.prevV = true, t, v
		c.push(v, 1, emit)
		c.nextGrid = t.Add(motionGridStep)
		return outcome, 0
	}
	continuedBy = t.Sub(c.prevT)
	span := continuedBy.Seconds()
	for !c.nextGrid.After(t) {
		frac := c.nextGrid.Sub(c.prevT).Seconds() / span
		c.push(c.prevV+frac*(v-c.prevV), c.pending, emit)
		c.pending = 0
		c.nextGrid = c.nextGrid.Add(motionGridStep)
	}
	// This sample lies after the last grid point, so it belongs to the next step.
	c.pending++
	c.prevT, c.prevV = t, v
	return outcome, continuedBy
}

func (c *motionChannel) push(v float64, raw int, emit func(motionStep)) {
	if len(c.hist) == motionJerkWindow {
		copy(c.hist, c.hist[1:])
		c.hist = c.hist[:motionJerkWindow-1]
		copy(c.cells, c.cells[1:])
		c.cells = c.cells[:motionJerkWindow-1]
	}
	c.hist = append(c.hist, v)
	c.cells = append(c.cells, raw)

	step := motionStep{value: v}
	h := motionGridStep.Seconds()
	if n := len(c.hist); n >= motionSmoothWindow && c.spanSampledFast(motionSmoothWindow) {
		recent := c.hist[n-motionSmoothWindow:]
		step.rate, step.hasRate = weighted(sgRate, recent)/h, true
		step.accel, step.hasAccel = weighted(sgAccel, recent)/(h*h), true
	}
	if len(c.hist) == motionJerkWindow && c.spanSampledFast(motionJerkWindow) {
		step.jerk, step.hasJerk = weighted(sgJerk, c.hist)/(h*h*h), true
	}
	emit(step)
}

func weighted(w, y []float64) float64 {
	var s float64
	for i := range w {
		s += w[i] * y[i]
	}
	return s
}

// welford keeps a running mean and spread without storing the samples.
type welford struct {
	n        int
	mean, m2 float64
}

func (w *welford) add(x float64) {
	w.n++
	d := x - w.mean
	w.mean += d / float64(w.n)
	w.m2 += d * (x - w.mean)
}

// spread is the standard deviation about the window mean, which is the RMS of
// the signal with its offset (a heel, a mounting angle) taken out.
func (w *welford) spread() float64 { return math.Sqrt(w.m2 / float64(w.n)) }

// peakTrack keeps the largest magnitude, the sum of squares and, when asked,
// every magnitude for a percentile.
type peakTrack struct {
	n     int
	peak  float64
	sumSq float64
	all   []float64
	keep  bool
}

func (p *peakTrack) add(x float64) {
	a := math.Abs(x)
	p.n++
	if a > p.peak {
		p.peak = a
	}
	p.sumSq += a * a
	if p.keep {
		p.all = append(p.all, a)
	}
}

func (p *peakTrack) peakPtr() *float64 {
	if p.n == 0 {
		return nil
	}
	v := p.peak
	return &v
}

func (p *peakTrack) rmsPtr() *float64 {
	if p.n == 0 {
		return nil
	}
	v := math.Sqrt(p.sumSq / float64(p.n))
	return &v
}

// p95Ptr is the nearest-rank 95th percentile of the magnitudes.
func (p *peakTrack) p95Ptr() *float64 {
	if p.n == 0 {
		return nil
	}
	// The window was handed off by Take, so nothing else holds this slice and
	// it is sorted where it lies.
	s := p.all
	sort.Float64s(s)
	v := s[int(math.Ceil(0.95*float64(len(s))))-1]
	return &v
}

// motionFigures is what one window of motion is worth to a track point. A nil
// figure is one that could not be worked out, never a zero.
type motionFigures struct {
	RollRMSDeg        *float64 `json:"roll_rms_deg"`
	RollRatePeakDps   *float64 `json:"roll_rate_peak_dps"`
	RollRateRMSDps    *float64 `json:"roll_rate_rms_dps"`
	RollAccelPeakDps2 *float64 `json:"roll_accel_peak_dps2"`
	RollJerkP95Dps3   *float64 `json:"roll_jerk_p95_dps3"`

	PitchRMSDeg        *float64 `json:"pitch_rms_deg"`
	PitchAccelPeakDps2 *float64 `json:"pitch_accel_peak_dps2"`
	PitchJerkP95Dps3   *float64 `json:"pitch_jerk_p95_dps3"`

	HeaveSDM         *float64 `json:"heave_sd_m"`
	VertAccelP95Ms2  *float64 `json:"vert_accel_p95_ms2"`
	VertAccelPeakMs2 *float64 `json:"vert_accel_peak_ms2"`

	// MotionRateHz and HeaveRateHz are the rates the device actually delivered
	// in the window, measured over unbroken runs only.
	MotionRateHz *float64 `json:"motion_rate_hz"`
	HeaveRateHz  *float64 `json:"heave_rate_hz"`
}

// motionWindow is everything one window has gathered. Take hands it off whole
// and starts a fresh one, so the figures (a sort among them) are worked out
// outside the lock.
type motionWindow struct {
	start time.Time // Helmcentral's time when the window opened

	att, heave signalStats

	rollLevel, pitchLevel, heaveLevel welford
	rollRate, rollAccel, rollJerk     peakTrack
	pitchAccel, pitchJerk             peakTrack
	vertAccel                         peakTrack
}

func newMotionWindow(start time.Time) motionWindow {
	return motionWindow{
		start:     start,
		rollJerk:  peakTrack{keep: true},
		pitchJerk: peakTrack{keep: true},
		vertAccel: peakTrack{keep: true},
	}
}

// signalStats counts what one signal delivered in a window. Samples count what
// arrived. Intervals and span count only the time between consecutive samples
// of an unbroken run, so a dropout is not averaged into the rate.
type signalStats struct {
	samples, intervals int
	span               time.Duration
}

// record counts one accepted sample. continuedBy is zero when the sample began
// a run.
func (s *signalStats) record(continuedBy time.Duration) {
	s.samples++
	if continuedBy > 0 {
		s.intervals++
		s.span += continuedBy
	}
}

// rate is the continuous intervals over the time they cover; ok is false with
// no continuous interval.
func (s signalStats) rate() (hz float64, ok bool) {
	if s.intervals == 0 {
		return 0, false
	}
	return float64(s.intervals) / s.span.Seconds(), true
}

// motionWindowCounts is what a window received, for the health check: a window
// with no attitude samples while SignalK has an attitude means the stream
// delivers nothing.
type motionWindowCounts struct {
	AttitudeSamples, HeaveSamples int
	// AttitudeSilence and HeaveSilence are how long, on Helmcentral's clock, it
	// is since the signal's last accepted sample, or since the accumulator
	// started when it has had none. They span windows, because under way a
	// window is only seconds long.
	AttitudeSilence, HeaveSilence time.Duration
}

// motionAccumulator is safe for one writer (the motion stream) and the
// recorder taking windows.
type motionAccumulator struct {
	mu  sync.Mutex
	now func() time.Time

	roll, pitch, heave motionChannel
	win                motionWindow

	// started is when the accumulator began, and lastAttitude and lastHeave when
	// each signal last had a sample accepted (zero for none yet), all on
	// Helmcentral's clock.
	started, lastAttitude, lastHeave time.Time
}

func newMotionAccumulator() *motionAccumulator {
	return newMotionAccumulatorClock(func() time.Time { return time.Now().UTC() })
}

// newMotionAccumulatorClock takes the clock the window is bounded by, so tests
// control it.
func newMotionAccumulatorClock(now func() time.Time) *motionAccumulator {
	return newMotionAccumulatorGap(now, motionMaxGap)
}

// newMotionAccumulatorGap also takes the longest silence the channels bridge. A
// replay of a stream kept at about 1 Hz sets it wider than the live 0.45 s, so
// that stream counts as one continuous run whose measured rate is then found
// to be under the 8 Hz floor.
func newMotionAccumulatorGap(now func() time.Time, maxGap time.Duration) *motionAccumulator {
	start := now()
	return &motionAccumulator{
		now: now, win: newMotionWindow(start), started: start,
		roll: motionChannel{maxGap: maxGap}, pitch: motionChannel{maxGap: maxGap}, heave: motionChannel{maxGap: maxGap},
	}
}

// silence is how long since a signal's last accepted sample, counted from the
// start when it has had none.
func (m *motionAccumulator) silence(last, now time.Time) time.Duration {
	if last.IsZero() {
		last = m.started
	}
	return now.Sub(last)
}

// admit starts a new window when Helmcentral's clock has run more than
// motionMaxWindow past the window's start. The figures it held are discarded,
// not stored late: they describe a stretch the point would not be about. The
// window never looks at a sensor timestamp.
func (m *motionAccumulator) admit() {
	if now := m.now(); now.Sub(m.win.start) > motionMaxWindow {
		m.win = newMotionWindow(now)
	}
}

// count records an accepted sample in the window; a dropped one is not counted.
func (m *motionAccumulator) count(outcome sampleOutcome, continuedBy time.Duration, stats *signalStats) {
	if outcome == sampleDropped {
		return
	}
	stats.record(continuedBy)
}

// AddAttitude takes one roll and pitch pair in degrees. The sensor's own time
// t only spaces the sample on the filter's grid.
func (m *motionAccumulator) AddAttitude(t time.Time, rollDeg, pitchDeg float64) sampleOutcome {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.admit()

	w := &m.win
	outcome, cont := m.roll.add(t, rollDeg, func(s motionStep) {
		w.rollLevel.add(s.value)
		if s.hasRate {
			w.rollRate.add(s.rate)
		}
		if s.hasAccel {
			w.rollAccel.add(s.accel)
		}
		if s.hasJerk {
			w.rollJerk.add(s.jerk)
		}
	})
	// Roll and pitch share one timestamp, so they accept or drop together.
	m.pitch.add(t, pitchDeg, func(s motionStep) {
		w.pitchLevel.add(s.value)
		if s.hasAccel {
			w.pitchAccel.add(s.accel)
		}
		if s.hasJerk {
			w.pitchJerk.add(s.jerk)
		}
	})
	m.count(outcome, cont, &w.att)
	if outcome != sampleDropped {
		m.lastAttitude = m.now()
	}
	return outcome
}

// AddHeave takes one heave sample in metres. The sensor's own time t only
// spaces the sample on the filter's grid.
func (m *motionAccumulator) AddHeave(t time.Time, heaveM float64) sampleOutcome {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.admit()

	w := &m.win
	outcome, cont := m.heave.add(t, heaveM, func(s motionStep) {
		w.heaveLevel.add(s.value)
		if s.hasAccel {
			w.vertAccel.add(s.accel)
		}
	})
	m.count(outcome, cont, &w.heave)
	if outcome != sampleDropped {
		m.lastHeave = m.now()
	}
	return outcome
}

// RestartAttitude and RestartHeave make the next sample of that signal begin a
// new run, as when its source changes. The window's figures are kept.
func (m *motionAccumulator) RestartAttitude() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.roll.restart()
	m.pitch.restart()
}

func (m *motionAccumulator) RestartHeave() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.heave.restart()
}

// Take returns the figures for the window since the last Take and starts a new
// one. The filters keep their history, so derivatives run on across the
// boundary.
//
// Each rate is the continuous intervals over the time they cover, so a
// dropout is not averaged into it. Attitude and heave each have their own rate
// and their own gate. With no continuous interval of a signal there is no rate
// and so no figure from it. Under motionMinRateHz its rate and RMS (heave
// spread for heave) are given and every derivative of it is nil.
//
// The counts say what the window received.
func (m *motionAccumulator) TakeWindow() (motionFigures, motionWindowCounts) {
	m.mu.Lock()
	w := m.win
	now := m.now()
	m.win = newMotionWindow(now)
	counts := motionWindowCounts{
		AttitudeSamples: w.att.samples, HeaveSamples: w.heave.samples,
		AttitudeSilence: m.silence(m.lastAttitude, now), HeaveSilence: m.silence(m.lastHeave, now),
	}
	m.mu.Unlock()

	var f motionFigures

	if rate, ok := w.att.rate(); ok {
		f.MotionRateHz = &rate
		if w.rollLevel.n > 0 {
			v := w.rollLevel.spread()
			f.RollRMSDeg = &v
		}
		if w.pitchLevel.n > 0 {
			v := w.pitchLevel.spread()
			f.PitchRMSDeg = &v
		}
		if rate >= motionMinRateHz {
			f.RollRatePeakDps, f.RollRateRMSDps = w.rollRate.peakPtr(), w.rollRate.rmsPtr()
			f.RollAccelPeakDps2 = w.rollAccel.peakPtr()
			f.RollJerkP95Dps3 = w.rollJerk.p95Ptr()
			f.PitchAccelPeakDps2 = w.pitchAccel.peakPtr()
			f.PitchJerkP95Dps3 = w.pitchJerk.p95Ptr()
		}
	}
	if rate, ok := w.heave.rate(); ok {
		f.HeaveRateHz = &rate
		if w.heaveLevel.n > 0 {
			v := w.heaveLevel.spread()
			f.HeaveSDM = &v
		}
		if rate >= motionMinRateHz {
			f.VertAccelP95Ms2, f.VertAccelPeakMs2 = w.vertAccel.p95Ptr(), w.vertAccel.peakPtr()
		}
	}
	return f, counts
}
