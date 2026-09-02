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
