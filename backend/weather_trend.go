package main

import (
	"math"
	"time"
)

/*
Barometric and wind trends, for the derived paths the heavy-weather alarm
rules bind to (ADR 0070).

The thresholds and the reasoning behind them come from Steve and Linda
Dashew's Surviving the Storm. Its central point about instrumentation is that
the barometer's rate of change carries the warning, not its value, and that
the one weather pattern most likely to catch a yacht out is the one where the
barometer does nothing at all.

Everything here returns absence rather than a number when the history is too
short to support one. A slope drawn through two samples a minute apart can
imply any weather at all, and an alarm that fires off that is an alarm the
operator learns to switch off.
*/

// knotsToMetersPerSecond is the inverse of main.go's metersPerSecondToKnots,
// named separately so call sites read as the conversion they are doing.
const knotsToMetersPerSecond = 1 / metersPerSecondToKnots

const (
	// pascalsPerMillibar: SignalK publishes pressure in pascals, mariners
	// think in millibars, and the book's numbers are all millibars.
	pascalsPerMillibar = 100.0

	// trendMinimumSpan is how much history a slope needs before it means
	// anything. Half an hour of a five-second poll is several hundred samples.
	trendMinimumSpan = 30 * time.Minute

	// pressureTrendWindow is the window every barometric figure is taken over.
	// Three hours is the tendency period every marine forecast already uses,
	// so the number is comparable with a published one.
	pressureTrendWindow = 3 * time.Hour
)

// Squash-zone thresholds (pages 89, 188). The wind rise is what marks the
// event; the flat barometer and steady direction are what distinguish it from
// an ordinary depression arriving.
const (
	squashZoneWindRiseKts    = 10.0
	squashZoneMaxPressureMb  = 1.0
	squashZoneMaxDirectionDg = 30.0
)

// linearSlopePerSecond is the least-squares slope of value against time.
//
// A regression rather than first-versus-last because the barometer is noisy
// at the resolution a sensor reports it, and one spurious sample at either
// end of the window would otherwise set the whole figure.
func linearSlopePerSecond(points []telemetryPoint) (float64, bool) {
	if len(points) < 2 {
		return 0, false
	}

	first, last := points[0].Timestamp, points[len(points)-1].Timestamp
	if last.Sub(first) < trendMinimumSpan {
		return 0, false
	}

	// Seconds relative to the first sample, to keep the sums small.
	var sumX, sumY, sumXY, sumXX float64
	n := float64(len(points))
	for _, p := range points {
		x := p.Timestamp.Sub(first).Seconds()
		sumX += x
		sumY += p.Value
		sumXY += x * p.Value
		sumXX += x * x
	}

	denominator := n*sumXX - sumX*sumX
	if denominator == 0 {
		return 0, false
	}

	return (n*sumXY - sumX*sumY) / denominator, true
}

// changeOverWindow is last minus first, the plain tendency.
func changeOverWindow(points []telemetryPoint) (float64, bool) {
	if len(points) < 2 {
		return 0, false
	}
	return points[len(points)-1].Value - points[0].Value, true
}

// angularSpreadDegrees is how far a direction series wandered, in degrees,
// measured as the largest shortest-way step from the first reading.
//
// Circular, because a wind sitting on north crosses 360 constantly and a
// naive max-minus-min would read that as a frontal passage every time.
func angularSpreadDegrees(points []telemetryPoint) float64 {
	if len(points) < 2 {
		return 0
	}

	reference := points[0].Value
	widest := 0.0
	for _, p := range points[1:] {
		diff := math.Abs(shortestAngleDiffRadians(p.Value, reference))
		if diff > widest {
			widest = diff
		}
	}
	return widest * 180 / math.Pi
}

// shortestAngleDiffRadians wraps a difference into [-pi, pi].
func shortestAngleDiffRadians(a, b float64) float64 {
	diff := math.Mod(a-b+math.Pi, 2*math.Pi)
	if diff < 0 {
		diff += 2 * math.Pi
	}
	return diff - math.Pi
}

/*
squashZoneSignature reports 1 when the three conditions the book describes
hold together, and 0 otherwise.

Returned as a number rather than a bool because the alarm engine compares
values: a rule binds this path with "above 0.5". A boolean path would need a
new operator, and the whole point of a derived path is that nothing in the
rule engine has to change.

Reports 0 rather than absence when the inputs are present but the pattern is
not, since "no squash zone right now" is a real answer. Absence is reserved
for not having enough history to say either way, which the callers check.
*/
func squashZoneSignature(pressure, windSpeed, windDirection []telemetryPoint) float64 {
	windRise, windOK := changeOverWindow(windSpeed)
	pressureChange, pressureOK := changeOverWindow(pressure)
	if !windOK || !pressureOK {
		return 0
	}

	if windRise*metersPerSecondToKnots < squashZoneWindRiseKts {
		return 0
	}
	if math.Abs(pressureChange)/pascalsPerMillibar >= squashZoneMaxPressureMb {
		return 0
	}
	if angularSpreadDegrees(windDirection) >= squashZoneMaxDirectionDg {
		return 0
	}

	return 1
}
