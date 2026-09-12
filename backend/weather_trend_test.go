package main

import (
	"math"
	"testing"
	"time"
)

var trendNow = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

// ramp builds a series ending at trendNow, one point per minute, walking from
// first to last linearly.
func ramp(first, last float64, over time.Duration) []telemetryPoint {
	steps := int(over / time.Minute)
	if steps < 1 {
		steps = 1
	}
	points := make([]telemetryPoint, 0, steps+1)
	for i := 0; i <= steps; i++ {
		frac := float64(i) / float64(steps)
		points = append(points, telemetryPoint{
			Value:     first + (last-first)*frac,
			Timestamp: trendNow.Add(-over + time.Duration(i)*time.Minute),
		})
	}
	return points
}

// Surviving the Storm reports a fall of 20mb in under 12 hours as a barometer
// that "plummets" (page 128) and 12mb in 4 hours as worth calling in on the
// net (page 459). Both are a couple of millibars an hour, so the slope has to
// come out in the right units to be worth alarming on.
func TestPressureTrend_SlopeInPascalsPerSecond(t *testing.T) {
	// 3mb down over 3 hours is 1mb/hr, which is 100Pa over 3600s.
	points := ramp(101500, 101200, 3*time.Hour)

	slope, ok := linearSlopePerSecond(points)
	if !ok {
		t.Fatalf("expected a defined slope")
	}
	wantPerHour := -100.0
	if math.Abs(slope*3600-wantPerHour) > 1 {
		t.Fatalf("slope = %.4f Pa/s (%.1f Pa/hr), want %.1f Pa/hr", slope, slope*3600, wantPerHour)
	}
}

// Two samples a minute apart can imply any slope at all. Reporting one would
// turn a single noisy reading into a storm warning.
func TestPressureTrend_AbsentWithoutEnoughHistory(t *testing.T) {
	if _, ok := linearSlopePerSecond(nil); ok {
		t.Fatalf("expected absence with no history")
	}
	if _, ok := linearSlopePerSecond(ramp(101500, 101000, 10*time.Minute)); ok {
		t.Fatalf("expected absence with only 10 minutes of history")
	}
	if _, ok := linearSlopePerSecond(ramp(101500, 101400, 45*time.Minute)); !ok {
		t.Fatalf("expected a slope once past the minimum span")
	}
}

func TestChangeOverWindow(t *testing.T) {
	change, ok := changeOverWindow(ramp(101500, 101200, 3*time.Hour))
	if !ok {
		t.Fatalf("expected a defined change")
	}
	if math.Abs(change-(-300)) > 1 {
		t.Fatalf("change = %.1f Pa, want -300", change)
	}

	if _, ok := changeOverWindow([]telemetryPoint{{Value: 1, Timestamp: trendNow}}); ok {
		t.Fatalf("a single point is not a change")
	}
}

/*
The squash-zone signature, pages 89 and 188: "wind direction and barometric
pressure typically remain steady while the wind increases". The book calls
these the cause of the majority of heavy-weather trouble yachts meet, and says
they are the phenomenon onboard data is worst at spotting - precisely because
the barometer, the instrument everyone watches, does nothing.
*/
func TestSquashZoneSignature(t *testing.T) {
	steadyPressure := ramp(101500, 101480, 3*time.Hour) // 0.2mb, flat
	risingWind := ramp(15*knotsToMetersPerSecond, 32*knotsToMetersPerSecond, 3*time.Hour)
	steadyDirection := ramp(2.0, 2.05, 3*time.Hour) // radians, a few degrees

	if got := squashZoneSignature(steadyPressure, risingWind, steadyDirection); got != 1 {
		t.Fatalf("steady pressure with a 17kt wind rise is the signature, got %v", got)
	}

	// A falling barometer means the depression is simply arriving. That is an
	// ordinary blow the barometer already warns about, not a squash zone.
	fallingPressure := ramp(101500, 101100, 3*time.Hour)
	if got := squashZoneSignature(fallingPressure, risingWind, steadyDirection); got != 0 {
		t.Fatalf("a falling barometer is not a squash zone, got %v", got)
	}

	// Wind that veers hard is a frontal passage, again not this.
	veering := ramp(2.0, 3.2, 3*time.Hour) // ~69 degrees
	if got := squashZoneSignature(steadyPressure, risingWind, veering); got != 0 {
		t.Fatalf("a large direction shift is not a squash zone, got %v", got)
	}

	// And a steady barometer with a steady wind is just a nice day.
	steadyWind := ramp(15*knotsToMetersPerSecond, 16*knotsToMetersPerSecond, 3*time.Hour)
	if got := squashZoneSignature(steadyPressure, steadyWind, steadyDirection); got != 0 {
		t.Fatalf("no wind rise is not a squash zone, got %v", got)
	}
}

// Direction is circular: 350 degrees to 10 degrees is a 20-degree shift, not
// a 340-degree one, or every northerly would look like a frontal passage.
func TestAngularSpreadDegrees_WrapsAtNorth(t *testing.T) {
	spread := angularSpreadDegrees([]telemetryPoint{
		{Value: 350 * math.Pi / 180},
		{Value: 355 * math.Pi / 180},
		{Value: 10 * math.Pi / 180},
	})
	if math.Abs(spread-20) > 0.5 {
		t.Fatalf("spread = %.1f degrees, want 20", spread)
	}
}

// A 12-hour or 24-hour tendency has nothing shorter to fall back on while its
// own buffer fills, so it must stay absent until nearly the whole window is
// covered -- not trendMinimumSpan's much looser 30 minutes, or a barely
// half-filled 12-hour buffer would already be reporting a "12 hour" figure
// built from a fraction of one.
func TestTendencyOverWindow_AbsentUntilTheWindowIsNearlyCovered(t *testing.T) {
	if _, ok := tendencyOverWindow(nil, pressureTendency12hWindow); ok {
		t.Fatalf("expected absence with no history")
	}
	// 11 hours is short of the 11.5h gate (12h minus the 30-minute slack).
	if _, ok := tendencyOverWindow(ramp(101500, 100000, 11*time.Hour), pressureTendency12hWindow); ok {
		t.Fatalf("expected absence just short of the 12h window")
	}
	if _, ok := tendencyOverWindow(ramp(101500, 100000, 11*time.Hour+30*time.Minute), pressureTendency12hWindow); !ok {
		t.Fatalf("expected a tendency once the span reaches the 11.5h gate")
	}
}

// The 12-hour and 24-hour tendencies are last-minus-first over their own
// window, the same arithmetic as changeOverWindow, just gated on a longer
// span.
func TestTendencyOverWindow_TwelveAndTwentyFourHourWindows(t *testing.T) {
	twelveHour, ok := tendencyOverWindow(ramp(101500, 99500, pressureTendency12hWindow), pressureTendency12hWindow)
	if !ok {
		t.Fatalf("expected a 12h tendency")
	}
	if math.Abs(twelveHour-(-2000)) > 1 {
		t.Fatalf("12h tendency = %.1f Pa, want -2000", twelveHour)
	}

	twentyFourHour, ok := tendencyOverWindow(ramp(101500, 99000, pressureTendency24hWindow), pressureTendency24hWindow)
	if !ok {
		t.Fatalf("expected a 24h tendency")
	}
	if math.Abs(twentyFourHour-(-2500)) > 1 {
		t.Fatalf("24h tendency = %.1f Pa, want -2500", twentyFourHour)
	}
}

// R. J. Ellis's storm/thunderstorm tier: a 4mb fall in three hours, with the
// barometer already under 1009mb.
func TestStormSignature_FourMillibarFallBelow1009(t *testing.T) {
	cases := []struct {
		change3h, pressurePa, want float64
	}{
		{-400, 100800, 1}, // 4mb fall, 1008mb: both conditions hold
		{-400, 100900, 0}, // exact boundary: pressure must be strictly under 1009mb
		{-500, 100000, 1}, // a bigger fall under a lower pressure
		{-300, 100800, 0}, // only a 3mb fall, short of the 4mb minimum
		{-400, 101000, 0}, // a 4mb fall, but the pressure is still above 1009mb
	}
	for _, c := range cases {
		if got := stormSignature(c.change3h, c.pressurePa); got != c.want {
			t.Fatalf("stormSignature(%v, %v) = %v, want %v", c.change3h, c.pressurePa, got, c.want)
		}
	}
}

// The severe-thunderstorm tier needs both the 3h and the 12h fall past their
// own minimums, and the pressure under the severe tier's lower gate.
func TestSevereThunderstormSignature_NeedsBothWindowsAndThePressure(t *testing.T) {
	// All three conditions hold: a 4mb fall in 3h, an 8mb fall in 12h, under 1005mb.
	if got := severeThunderstormSignature(-400, -800, 100000); got != 1 {
		t.Fatalf("expected the severe-thunderstorm signature, got %v", got)
	}
	// The 3h fall alone is not enough without the matching 12h fall.
	if got := severeThunderstormSignature(-400, -500, 100000); got != 0 {
		t.Fatalf("a 5mb 12h fall is short of the 8mb minimum, got %v", got)
	}
	// The 12h fall alone is not enough without the matching 3h fall.
	if got := severeThunderstormSignature(-200, -800, 100000); got != 0 {
		t.Fatalf("a 2mb 3h fall is short of the 4mb minimum, got %v", got)
	}
	// Both falls present, but the pressure has not dropped enough.
	if got := severeThunderstormSignature(-400, -800, 100600); got != 0 {
		t.Fatalf("pressure above 1005mb should not fire, got %v", got)
	}
	// Exact pressure boundary: strict less-than, same as stormSignature.
	if got := severeThunderstormSignature(-400, -800, 100500); got != 0 {
		t.Fatalf("pressure exactly at 1005mb should not fire (strict <), got %v", got)
	}
}

// The four Law of Storms thresholds are the page's millibar figures
// converted to the pascals the derived paths report in, not left as
// millibars to be misread as one 100th of that.
func TestLawOfStormsThresholdsAreInPascals(t *testing.T) {
	if lawOfStormsStormFall3hPa != -400 {
		t.Fatalf("stormFall3h = %v Pa, want -400", lawOfStormsStormFall3hPa)
	}
	if lawOfStormsStormMaxPressurePa != 100900 {
		t.Fatalf("stormMaxPressure = %v Pa, want 100900", lawOfStormsStormMaxPressurePa)
	}
	if lawOfStormsSevereFall12hPa != -800 {
		t.Fatalf("severeFall12h = %v Pa, want -800", lawOfStormsSevereFall12hPa)
	}
	if lawOfStormsSevereMaxPressurePa != 100500 {
		t.Fatalf("severeMaxPressure = %v Pa, want 100500", lawOfStormsSevereMaxPressurePa)
	}
}
