package main

import (
	"math"
	"math/rand"
	"sync"
	"testing"
	"time"
)

var motionT0 = time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)

// Take is TakeWindow without the counts, for tests that read figures only.
func (m *motionAccumulator) Take() motionFigures {
	f, _ := m.TakeWindow()
	return f
}

func atSec(s float64) time.Time {
	return motionT0.Add(time.Duration(s * float64(time.Second)))
}

// feedSine drives an accumulator with a roll sine (amplitude ampDeg, period
// periodS, plus offsetDeg) and a pitch sine at a tenth of the roll, sampled at
// hz from start to end seconds. jitter shifts each SOURCE timestamp by up to
// that many seconds, the way an irregular sensor would; the value is the sine
// at the jittered time, so the signal itself stays true.
func feedSine(m *motionAccumulator, start, end, hz, ampDeg, periodS, offsetDeg, jitter float64, rng *rand.Rand) {
	w := 2 * math.Pi / periodS
	for ts := start; ts < end; ts += 1 / hz {
		t := ts
		if jitter > 0 {
			t += (rng.Float64()*2 - 1) * jitter
		}
		m.AddAttitude(atSec(t), offsetDeg+ampDeg*math.Sin(w*t), 0.1*ampDeg*math.Sin(w*t))
	}
}

func nearFig(t *testing.T, name string, got *float64, want, relTol float64) {
	t.Helper()
	if got == nil {
		t.Errorf("%s is null, want %.4g", name, want)
		return
	}
	if math.Abs(*got-want) > relTol*math.Abs(want) {
		t.Errorf("%s = %.4g, want %.4g within %.0f%%", name, *got, want, relTol*100)
	}
}

// sgGain is how much of a sinusoid's true deriv-th derivative a filter passes
// at angular frequency w: its response to cos(w t), against (w)^deriv. A
// Savitzky-Golay derivative is a low-pass, so it under-reads and never over-reads.
func sgGain(weights []float64, deriv int, w float64) float64 {
	half := (len(weights) - 1) / 2
	h := motionGridStep.Seconds()
	var s float64
	for i, c := range weights {
		phase := w * float64(i-half) * h
		if deriv%2 == 0 {
			s += c * math.Cos(phase) // even derivatives of cos are cos
		} else {
			s += c * math.Sin(phase) // odd ones are sin
		}
	}
	return math.Abs(s) / (math.Pow(h, float64(deriv)) * math.Pow(w, float64(deriv)))
}

// At the 3 s period of a lively roll the filters keep nearly all of the signal;
// at 2 s they have already lost a fifth of the jerk. That bias is recorded in
// the ADR, so a change to a window length has to change it too.
func TestMotionFilterGainsAtARollPeriod(t *testing.T) {
	w3, w2 := 2*math.Pi/3, 2*math.Pi/2
	if g := sgGain(sgRate, 1, w3); g < 0.97 || g > 1 {
		t.Errorf("rate gain at 3 s = %.3f", g)
	}
	if g := sgGain(sgAccel, 2, w3); g < 0.88 || g > 0.95 {
		t.Errorf("accel gain at 3 s = %.3f", g)
	}
	if g := sgGain(sgJerk, 3, w3); g < 0.82 || g > 0.92 {
		t.Errorf("jerk gain at 3 s = %.3f", g)
	}
	if g := sgGain(sgJerk, 3, w2); g > sgGain(sgJerk, 3, w3) {
		t.Errorf("a shorter period must lose more: %.3f", g)
	}
}

func TestSavitzkyGolayCoefficientsMatchTheClassicFivePointFilter(t *testing.T) {
	// Cubic over 5 points, first derivative: the textbook weights.
	got := sgCoefficients(5, 3, 1)
	want := []float64{1.0 / 12, -8.0 / 12, 0, 8.0 / 12, -1.0 / 12}
	// Oldest sample first: f' = (f[-2] - 8 f[-1] + 8 f[1] - f[2]) / 12.
	for i := range want {
		if !approx(got[i], want[i], 1e-9) {
			t.Fatalf("weights %v, want %v", got, want)
		}
	}
}

func TestSavitzkyGolayReproducesPolynomialDerivativesExactly(t *testing.T) {
	// y = 2 + 3x + 4x^2 + x^3 + 0.5x^4 sampled at integer x. At x=0 the
	// derivatives are 3, 8, 6 and 0 (third derivative of x^3 is 6, of x^4 is 0 at 0).
	y := func(x float64) float64 { return 2 + 3*x + 4*x*x + x*x*x + 0.5*x*x*x*x }
	apply := func(w []float64) float64 {
		m := (len(w) - 1) / 2
		var s float64
		for i, c := range w {
			s += c * y(float64(i-m))
		}
		return s
	}
	// The cubic filter cannot see the x^4 term in its 2nd derivative exactly
	// (it leaks 12*0.5*mean(x^2)-ish), so check the quartic filter for all three
	// and the cubic one on the cubic part only.
	if got := apply(sgCoefficients(15, 4, 1)); !approx(got, 3, 1e-9) {
		t.Errorf("quartic d1 = %v, want 3", got)
	}
	if got := apply(sgCoefficients(15, 4, 2)); !approx(got, 8, 1e-9) {
		t.Errorf("quartic d2 = %v, want 8", got)
	}
	if got := apply(sgCoefficients(15, 4, 3)); !approx(got, 6, 1e-9) {
		t.Errorf("quartic d3 = %v, want 6", got)
	}
	cubic := func(x float64) float64 { return 2 + 3*x + 4*x*x + x*x*x }
	applyCubic := func(w []float64) float64 {
		m := (len(w) - 1) / 2
		var s float64
		for i, c := range w {
			s += c * cubic(float64(i-m))
		}
		return s
	}
	if got := applyCubic(sgCoefficients(11, 3, 1)); !approx(got, 3, 1e-9) {
		t.Errorf("cubic d1 = %v, want 3", got)
	}
	if got := applyCubic(sgCoefficients(11, 3, 2)); !approx(got, 8, 1e-9) {
		t.Errorf("cubic d2 = %v, want 8", got)
	}
}

func TestMotionDerivativesOfASineMatchTheAnalyticValues(t *testing.T) {
	const amp, period = 5.0, 3.0
	w := 2 * math.Pi / period
	m := newMotionAccumulator()
	feedSine(m, 0, 30, 10, amp, period, 2, 0, nil)
	f := m.Take()

	nearFig(t, "roll rms", f.RollRMSDeg, amp/math.Sqrt2, 0.02)
	nearFig(t, "roll rate peak", f.RollRatePeakDps, amp*w, 0.03)
	nearFig(t, "roll rate rms", f.RollRateRMSDps, amp*w/math.Sqrt2, 0.03)
	nearFig(t, "roll accel peak", f.RollAccelPeakDps2, amp*w*w*sgGain(sgAccel, 2, w), 0.01)
	// p95 of |jerk| for a sine is 0.9969 of its peak.
	nearFig(t, "roll jerk p95", f.RollJerkP95Dps3, 0.9969*amp*w*w*w*sgGain(sgJerk, 3, w), 0.03)
	nearFig(t, "pitch rms", f.PitchRMSDeg, 0.1*amp/math.Sqrt2, 0.02)
	nearFig(t, "pitch accel peak", f.PitchAccelPeakDps2, 0.1*amp*w*w*sgGain(sgAccel, 2, w), 0.01)
	nearFig(t, "pitch jerk p95", f.PitchJerkP95Dps3, 0.9969*0.1*amp*w*w*w*sgGain(sgJerk, 3, w), 0.03)
	nearFig(t, "motion rate", f.MotionRateHz, 10, 0.01)
}

// The sensor's arrival gaps run 13 to 274 ms; its own timestamps are what the
// filter must use. Irregular source times still give the right derivatives.
func TestMotionUsesSourceTimestampsNotEvenSpacing(t *testing.T) {
	const amp, period = 5.0, 3.0
	w := 2 * math.Pi / period
	m := newMotionAccumulator()
	feedSine(m, 0, 30, 10, amp, period, 0, 0.03, rand.New(rand.NewSource(7)))
	f := m.Take()
	nearFig(t, "roll rate peak", f.RollRatePeakDps, amp*w, 0.06)
	nearFig(t, "roll accel peak", f.RollAccelPeakDps2, amp*w*w*sgGain(sgAccel, 2, w), 0.05)
}

// A gap over 0.45 s breaks continuity: nothing is interpolated across it and the
// filter starts again, so the jump cannot show up as a derivative spike.
func TestMotionGapRestartsTheFilterInsteadOfInterpolating(t *testing.T) {
	const amp, period = 5.0, 3.0
	w := 2 * math.Pi / period
	m := newMotionAccumulator()
	feedSine(m, 0, 10, 10, amp, period, 0, 0, nil)
	// 2 s of silence, then the sine resumes at a different phase.
	m.AddAttitude(atSec(12), 4, 0)
	for ts := 12.1; ts < 22; ts += 0.1 {
		m.AddAttitude(atSec(ts), 4+amp*math.Sin(w*ts), 0)
	}
	f := m.Take()
	if f.RollRatePeakDps == nil || *f.RollRatePeakDps > 1.05*amp*w {
		t.Errorf("rate peak %v shows a spike across the gap (limit %.3g)", f.RollRatePeakDps, 1.05*amp*w)
	}
	if f.RollAccelPeakDps2 == nil || *f.RollAccelPeakDps2 > 1.1*amp*w*w {
		t.Errorf("accel peak %v shows a spike across the gap", f.RollAccelPeakDps2)
	}
}

func TestMotionBelowTheRateFloorGivesRmsAndRateButNoDerivatives(t *testing.T) {
	m := newMotionAccumulator()
	feedSine(m, 0, 20, 5, 5, 3, 0, 0, nil) // a 5 Hz device
	for ts := 0.0; ts < 20; ts += 0.2 {
		m.AddHeave(atSec(ts), 0.3*math.Sin(2*math.Pi/3*ts))
	}
	f := m.Take()
	nearFig(t, "motion rate", f.MotionRateHz, 5, 0.01)
	if f.RollRMSDeg == nil || f.PitchRMSDeg == nil || f.HeaveSDM == nil {
		t.Errorf("rms and heave sd are still given at 5 Hz: %+v", f)
	}
	if f.RollRatePeakDps != nil || f.RollRateRMSDps != nil || f.RollAccelPeakDps2 != nil || f.RollJerkP95Dps3 != nil ||
		f.PitchAccelPeakDps2 != nil || f.PitchJerkP95Dps3 != nil || f.VertAccelP95Ms2 != nil || f.VertAccelPeakMs2 != nil {
		t.Errorf("derived figures must be null below 8 Hz: %+v", f)
	}
}

func TestMotionNoAttitudeSamplesIsEverythingNull(t *testing.T) {
	m := newMotionAccumulator()
	if f := m.Take(); f != (motionFigures{}) {
		t.Errorf("empty window: %+v", f)
	}
	// Heave is its own signal with its own rate: it neither needs attitude nor
	// releases attitude's figures.
	for ts := 0.0; ts < 5; ts += 0.1 {
		m.AddHeave(atSec(ts), 0.1*math.Sin(ts))
	}
	f := m.Take()
	if f.MotionRateHz != nil || f.RollRMSDeg != nil || f.RollAccelPeakDps2 != nil {
		t.Errorf("heave alone must give no attitude figure: %+v", f)
	}
	nearFig(t, "heave rate", f.HeaveRateHz, 10, 0.01)
	if f.HeaveSDM == nil || f.VertAccelPeakMs2 == nil {
		t.Errorf("heave figures are given when heave runs at 10 Hz: %+v", f)
	}
}

// Vertical acceleration is released by the heave rate, not the attitude rate.
func TestMotionVerticalAccelerationIsGatedOnTheHeaveRate(t *testing.T) {
	m := newMotionAccumulator()
	feedSine(m, 0, 20, 10, 5, 3, 0, 0, nil) // attitude at 10 Hz
	for ts := 0.0; ts < 20; ts += 0.2 {     // heave at 5 Hz
		m.AddHeave(atSec(ts), 0.3*math.Sin(2*math.Pi/3*ts))
	}
	f := m.Take()
	if f.RollAccelPeakDps2 == nil {
		t.Error("attitude at 10 Hz still gives its derivatives")
	}
	nearFig(t, "heave rate", f.HeaveRateHz, 5, 0.01)
	if f.HeaveSDM == nil {
		t.Error("heave sd is still given at 5 Hz")
	}
	if f.VertAccelP95Ms2 != nil || f.VertAccelPeakMs2 != nil {
		t.Errorf("vertical acceleration must be null at a 5 Hz heave: %+v", f)
	}
}

// The rate is measured over unbroken runs. A long dropout between clean 10 Hz
// stretches neither nulls the window nor drags the rate down.
func TestMotionRateIgnoresDropouts(t *testing.T) {
	m := newMotionAccumulator()
	feedSine(m, 0, 10, 10, 5, 3, 0, 0, nil)
	feedSine(m, 70, 80, 10, 5, 3, 0, 0, nil)
	f := m.Take()
	nearFig(t, "rate across a 60 s dropout", f.MotionRateHz, 10, 0.01)
	if f.RollAccelPeakDps2 == nil {
		t.Error("clean stretches around a dropout keep their derivatives")
	}
}

// A 2 Hz stretch is not interpolated up to 10 Hz: it breaks continuity, so it
// adds no interval and cannot pass the rate gate.
func TestMotionSlowStretchBreaksContinuity(t *testing.T) {
	m := newMotionAccumulator()
	for ts := 0.0; ts < 30; ts += 0.5 {
		m.AddAttitude(atSec(ts), 3*math.Sin(ts), 0)
	}
	f := m.Take()
	if f.MotionRateHz != nil || f.RollRMSDeg != nil {
		t.Errorf("a 2 Hz stream has no continuous run: %+v", f)
	}
	// Mixed: 10 Hz, then 2 Hz, then 10 Hz. Only the 10 Hz time counts.
	feedSine(m, 0, 10, 10, 5, 3, 0, 0, nil)
	for ts := 10.0; ts < 20; ts += 0.5 {
		m.AddAttitude(atSec(ts), 0, 0)
	}
	feedSine(m, 20, 30, 10, 5, 3, 0, 0, nil)
	nearFig(t, "mixed rate", m.Take().MotionRateHz, 10, 0.01)
}

// The edge of continuity: 0.4 s between samples is still one run, 0.5 s is a
// break. The worst jitter measured between update timestamps was 274 ms.
func TestMotionContinuityBoundary(t *testing.T) {
	spaced := func(step float64) motionFigures {
		m := newMotionAccumulator()
		for i := 0; i < 20; i++ {
			m.AddAttitude(atSec(float64(i)*step), float64(i%3), 0)
		}
		return m.Take()
	}
	nearFig(t, "rate at 0.4 s spacing", spaced(0.4).MotionRateHz, 2.5, 0.01)
	if f := spaced(0.5); f.MotionRateHz != nil {
		t.Errorf("0.5 s spacing breaks continuity, got rate %v", *f.MotionRateHz)
	}
}

// The 8 Hz gate is a window average. A stretch of 3 Hz samples bridged by
// interpolation inside a fast window must not feed peaks or percentiles.
func TestMotionSlowStretchInsideAFastWindowFeedsNoDerivatives(t *testing.T) {
	ref := newMotionAccumulator()
	feedSine(ref, 0, 540, 10, 5, 3, 0, 0, nil)
	want := ref.Take()

	m := newMotionAccumulator()
	feedSine(m, 0, 270, 10, 5, 3, 0, 0, nil)
	w := 2 * math.Pi / 3
	for i := 0; ; i++ { // a minute at 3 Hz, spiking in the middle
		ts := 270 + float64(i)/3
		if ts >= 330 {
			break
		}
		roll := 5 * math.Sin(w*ts)
		if ts > 280 && ts < 320 && i%2 == 0 {
			roll += 20
		}
		m.AddAttitude(atSec(ts), roll, 0.1*roll)
	}
	feedSine(m, 330, 600, 10, 5, 3, 0, 0, nil)
	got := m.Take()
	if got.MotionRateHz == nil || *got.MotionRateHz < motionMinRateHz {
		t.Fatalf("test setup: window rate %v must pass the window gate", got.MotionRateHz)
	}
	nearFig(t, "roll rate peak", got.RollRatePeakDps, *want.RollRatePeakDps, 0.03)
	nearFig(t, "roll accel peak", got.RollAccelPeakDps2, *want.RollAccelPeakDps2, 0.03)
	nearFig(t, "roll jerk p95", got.RollJerkP95Dps3, *want.RollJerkP95Dps3, 0.03)
	nearFig(t, "pitch accel peak", got.PitchAccelPeakDps2, *want.PitchAccelPeakDps2, 0.03)
	nearFig(t, "pitch jerk p95", got.PitchJerkP95Dps3, *want.PitchJerkP95Dps3, 0.03)
}

// testClock is Helmcentral's clock for the window, moved by the test.
type testClock struct{ t time.Time }

func (c *testClock) now() time.Time          { return c.t }
func (c *testClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// feedClocked feeds seconds of 10 Hz attitude (roll sine of ampDeg) and/or
// heave, stamped from sensorStart on the sensor's clock, advancing Helmcentral's
// clock 100 ms per sample.
func feedClocked(m *motionAccumulator, c *testClock, attitudeStart, heaveStart float64, seconds, ampDeg float64, att, hv bool) {
	w := 2 * math.Pi / 3
	for i := 0; i < int(seconds*10); i++ {
		c.advance(100 * time.Millisecond)
		if hv {
			ts := heaveStart + float64(i)/10
			m.AddHeave(atSec(ts), 0.3*math.Sin(w*ts))
		}
		if att {
			ts := attitudeStart + float64(i)/10
			m.AddAttitude(atSec(ts), ampDeg*math.Sin(w*ts), 0)
		}
	}
}

// The window is bounded by Helmcentral's clock. A source that switches to a
// clock 40 minutes behind must not hold the 15 minute cap open.
func TestMotionWindowCapsOnTheReceiveClockAcrossASourceSwitch(t *testing.T) {
	c := &testClock{t: motionT0}
	m := newMotionAccumulatorClock(c.now)
	feedClocked(m, c, 1000, 0, 10, 20, true, false) // a big roll early
	c.advance(16 * time.Minute)
	m.RestartAttitude() // the source switches to a clock 40 minutes behind
	feedClocked(m, c, 1000-40*60, 0, 10, 1, true, false)
	f := m.Take()
	if f.RollRMSDeg == nil || *f.RollRMSDeg > 1.5 {
		t.Errorf("figures must describe only the newest window, got roll rms %v", f.RollRMSDeg)
	}
}

// A heave clock stepping back restarts the heave channel only: the attitude
// figures stay in the window.
func TestMotionHeaveClockStepBackKeepsTheAttitudeFigures(t *testing.T) {
	c := &testClock{t: motionT0}
	m := newMotionAccumulatorClock(c.now)
	feedClocked(m, c, 1000, 1000, 10, 20, true, true)
	feedClocked(m, c, 1010, 100, 10, 1, true, true) // heave clock steps back 15 min
	f := m.Take()
	if f.RollRMSDeg == nil || *f.RollRMSDeg < 8 {
		t.Errorf("attitude figures from before the heave step must stay, got roll rms %v", f.RollRMSDeg)
	}
	if f.HeaveSDM == nil {
		t.Error("heave resumes after its restart")
	}
}

// Attitude and heave on clocks 20 minutes apart do not reset the window.
func TestMotionSignalsOnDifferentClocksDoNotResetTheWindow(t *testing.T) {
	c := &testClock{t: motionT0}
	m := newMotionAccumulatorClock(c.now)
	feedClocked(m, c, 5000, 100, 30, 20, true, true)
	f, counts := m.TakeWindow()
	if counts.AttitudeSamples != 300 || counts.HeaveSamples != 300 {
		t.Errorf("both signals must fill one window, got %d attitude and %d heave", counts.AttitudeSamples, counts.HeaveSamples)
	}
	nearFig(t, "roll rms", f.RollRMSDeg, 20/math.Sqrt2, 0.05)
	if f.HeaveSDM == nil {
		t.Error("heave figures missing")
	}
}

func TestMotionClockStepBackRestartsTheChannel(t *testing.T) {
	m := newMotionAccumulator()
	feedSine(m, 100, 110, 10, 5, 3, 0, 0, nil)
	// The clock steps back a minute; samples must not be dropped until it
	// catches up.
	got := m.AddAttitude(atSec(50), 0, 0)
	if got != sampleRestarted {
		t.Fatalf("outcome %v, want restarted", got)
	}
	for ts := 50.1; ts < 55; ts += 0.1 {
		if o := m.AddAttitude(atSec(ts), 0, 0); o != sampleUsed {
			t.Fatalf("sample at %.1f: outcome %v", ts, o)
		}
	}
	// Small steps back are duplicates and are dropped.
	if o := m.AddAttitude(atSec(54.5), 0, 0); o != sampleDropped {
		t.Errorf("a 0.5 s step back is dropped, got %v", o)
	}
}

func TestMotionRestartKeepsTheWindowFigures(t *testing.T) {
	m := newMotionAccumulator()
	feedSine(m, 0, 10, 10, 5, 3, 0, 0, nil)
	m.RestartAttitude()
	f := m.Take()
	if f.RollRMSDeg == nil {
		t.Error("a source switch restarts the filter, not the window")
	}
}

func TestMotionHeaveSpreadAndVerticalAcceleration(t *testing.T) {
	const amp, period = 0.3, 3.0
	w := 2 * math.Pi / period
	m := newMotionAccumulator()
	feedSine(m, 0, 30, 10, 5, period, 0, 0, nil)
	for ts := 0.0; ts < 30; ts += 0.1 {
		m.AddHeave(atSec(ts), 0.5+amp*math.Sin(w*ts))
	}
	f := m.Take()
	nearFig(t, "heave sd", f.HeaveSDM, amp/math.Sqrt2, 0.02)
	nearFig(t, "vertical accel peak", f.VertAccelPeakMs2, amp*w*w*sgGain(sgAccel, 2, w), 0.01)
	nearFig(t, "vertical accel p95", f.VertAccelP95Ms2, 0.9969*amp*w*w*sgGain(sgAccel, 2, w), 0.02)
}

// Taking the window resets the figures but keeps the filter's history, so the
// next window has derivatives from its first sample instead of waiting 1.5 s.
func TestMotionTakeResetsTheWindowButNotTheFilter(t *testing.T) {
	const amp, period = 5.0, 3.0
	w := 2 * math.Pi / period
	m := newMotionAccumulator()
	feedSine(m, 0, 10, 10, amp, period, 0, 0, nil)
	m.Take()
	if f := m.Take(); f != (motionFigures{}) {
		t.Errorf("a second Take with no new samples must be all null: %+v", f)
	}
	// Half a second of data is shorter than the jerk filter's own span.
	feedSine(m, 10, 10.6, 10, amp, period, 0, 0, nil)
	f := m.Take()
	if f.RollJerkP95Dps3 == nil || f.RollAccelPeakDps2 == nil {
		t.Fatalf("derivatives must continue across the boundary: %+v", f)
	}
	nearFig(t, "accel peak after reset", f.RollAccelPeakDps2, amp*w*w*math.Abs(math.Sin(w*10.0)), 0.5)
}

func TestMotionIgnoresRepeatedAndBackwardTimestamps(t *testing.T) {
	m := newMotionAccumulator()
	m.AddAttitude(atSec(1), 1, 1)
	m.AddAttitude(atSec(1), 9, 9)
	m.AddAttitude(atSec(0.5), 9, 9)
	m.AddAttitude(atSec(1.1), 1, 1)
	f := m.Take()
	if f.RollRMSDeg == nil || *f.RollRMSDeg != 0 {
		t.Errorf("rms of a constant must be 0, got %v", f.RollRMSDeg)
	}
}

func TestMotionAccumulatorIsSafeForAWriterAndAReader(t *testing.T) {
	m := newMotionAccumulator()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		feedSine(m, 0, 60, 10, 5, 3, 0, 0, nil)
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			m.Take()
		}
	}()
	wg.Wait()
}
