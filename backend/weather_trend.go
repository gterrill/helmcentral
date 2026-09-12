package main

import (
	"math"
	"time"
)

/*
Barometric and wind trends, for the derived paths the barometer alarm rules
bind to (ADR 0070, amended by ADR 0095).

Two sources feed different parts of this file. Steve and Linda Dashew's
Surviving the Storm is where pressureRate, squashZoneSignature and the
three-hour window itself come from; its central point about instrumentation
is that the barometer's rate of change carries the warning, not its value,
and that the one weather pattern most likely to catch a yacht out is the one
where the barometer does nothing at all. R. J. Ellis's "Secret Law of
Storms" (worldstormcentral.co, rules-for-storms-and-gales page) is where the
12h and 24h tendency windows and stormSignature / severeThunderstormSignature
come from: a ladder of tendency thresholds tied to the same three-hour
reading every marine forecast already quotes, rather than one crew's read of
a rate.

Everything here returns absence rather than a number when the history is too
short to support one. A slope, or a tendency, drawn through too short a span
can imply any weather at all, and an alarm that fires off that is an alarm
the operator learns to switch off.
*/

// knotsToMetersPerSecond is the inverse of main.go's metersPerSecondToKnots,
// named separately so call sites read as the conversion they are doing.
const knotsToMetersPerSecond = 1 / metersPerSecondToKnots

const (
	// pascalsPerMillibar: SignalK publishes pressure in pascals, mariners
	// think in millibars, and both sources this file draws on talk in
	// millibars.
	pascalsPerMillibar = 100.0

	// trendMinimumSpan is how much history a slope needs before it means
	// anything. Half an hour of a five-second poll is several hundred samples.
	trendMinimumSpan = 30 * time.Minute

	// pressureTrendWindow is the window every barometric figure is taken over.
	// Three hours is the tendency period every marine forecast already uses,
	// so the number is comparable with a published one.
	pressureTrendWindow = 3 * time.Hour

	// pressureTendency12hWindow and pressureTendency24hWindow are the two
	// longer windows the Law of Storms ladder adds on top of the three-hour
	// tendency above (ADR 0095): the severe-thunderstorm tier reads the 12h
	// figure, the weather-bomb tier the 24h one.
	pressureTendency12hWindow = 12 * time.Hour
	pressureTendency24hWindow = 24 * time.Hour

	// tendencySpanSlack is how far short of a full window tendencyOverWindow
	// will still report a figure: 12h reports after 11.5h of history, 24h
	// after 23.5h. Unlike trendMinimumSpan's 30 minutes, this gate sits right
	// at the window's edge -- a 12h or 24h figure has nothing shorter to lean
	// on while its own buffer fills, so reporting much earlier than the
	// window itself would be presenting a partial fall as the whole one.
	tendencySpanSlack = 30 * time.Minute
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

// newestSampleAge is freshestTimestampAge's ring-buffer counterpart: how long
// before now the most recent point in a window was recorded, rounded to one
// decimal place, -1 for an empty window.
//
// A trend's slope covers the whole window, but it is only as fresh as the
// newest point feeding it. A buffer that stopped receiving samples makes the
// slope stale even though every point behind it still carries a real
// timestamp from inside the window.
func newestSampleAge(points []telemetryPoint, now time.Time) float64 {
	if len(points) == 0 {
		return -1
	}
	newest := points[0].Timestamp
	for _, p := range points[1:] {
		if p.Timestamp.After(newest) {
			newest = p.Timestamp
		}
	}
	age := now.Sub(newest).Seconds()
	if age < 0 {
		return 0
	}
	return roundTo1(age)
}

// changeOverWindow is last minus first, the plain tendency.
func changeOverWindow(points []telemetryPoint) (float64, bool) {
	if len(points) < 2 {
		return 0, false
	}
	return points[len(points)-1].Value - points[0].Value, true
}

// tendencyOverWindow is changeOverWindow's counterpart for a window with
// nothing shorter to lean on while its buffer fills (ADR 0095): last minus
// first, present only once the span covers window minus tendencySpanSlack.
// The three-hour figure does not go through this -- see pressureChange3hPath's
// own call site in addWeatherTrendValues -- because it can reuse pressureRate's
// much looser trendMinimumSpan instead.
func tendencyOverWindow(points []telemetryPoint, window time.Duration) (float64, bool) {
	if len(points) < 2 {
		return 0, false
	}
	first, last := points[0].Timestamp, points[len(points)-1].Timestamp
	if last.Sub(first) < window-tendencySpanSlack {
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

/*
Law of Storms barometer thresholds (ADR 0095), from R. J. Ellis, "Secret Law
of Storms", worldstormcentral.co, rules-for-storms-and-gales page. Unlike the
Dashew rate thresholds above, these read as a tendency over a stated window
and gate the sharper falls on an absolute pressure, the way the page itself
states them: a 4mb fall means something different starting from 1030mb than
it does starting from 995mb.
*/
const (
	// lawOfStormsStormFall3hPa is the page's minimum for its storm /
	// thunderstorm tier: a 3mb fall in three hours is the floor it names, 4mb
	// "a margin of comfort".
	lawOfStormsStormFall3hPa = -4 * pascalsPerMillibar

	// lawOfStormsStormMaxPressurePa: the fall alone is not the signature, or
	// a 4mb fall from 1030mb would read as a storm warning. The page pairs
	// the fall with pressure already down under 1009mb.
	lawOfStormsStormMaxPressurePa = 1009 * pascalsPerMillibar

	// lawOfStormsSevereFall12hPa is the severe-thunderstorm tier's 12h
	// companion to the same 4mb 3h fall: 8mb over twelve hours.
	lawOfStormsSevereFall12hPa = -8 * pascalsPerMillibar

	// lawOfStormsSevereMaxPressurePa is the severe tier's own pressure gate,
	// lower than the plain storm tier's: 1005mb rather than 1009mb.
	lawOfStormsSevereMaxPressurePa = 1005 * pascalsPerMillibar
)

/*
stormSignature reports 1 when a three-hour fall of at least
lawOfStormsStormFall3hPa has brought the barometer under
lawOfStormsStormMaxPressurePa, and 0 otherwise -- squashZoneSignature's own
idiom: a number a rule can bind "above 0.5" to, with 0 a real "not this
pattern right now" answer rather than absence. Absence -- not having a
defined three-hour change to call this on at all -- is the caller's job, the
same as squashZoneSignature.

<= on the fall and strict < on the pressure: a fall of exactly 4mb qualifies,
but a barometer sitting exactly at 1009mb has not yet gone under it.
*/
func stormSignature(change3h, pressurePa float64) float64 {
	if change3h <= lawOfStormsStormFall3hPa && pressurePa < lawOfStormsStormMaxPressurePa {
		return 1
	}
	return 0
}

// severeThunderstormSignature is stormSignature's harder tier: it additionally
// needs the 12h fall past lawOfStormsSevereFall12hPa and the pressure under
// the severe tier's own, lower, lawOfStormsSevereMaxPressurePa. All three
// conditions, the same <=/< boundaries as stormSignature.
func severeThunderstormSignature(change3h, change12h, pressurePa float64) float64 {
	if change3h <= lawOfStormsStormFall3hPa &&
		change12h <= lawOfStormsSevereFall12hPa &&
		pressurePa < lawOfStormsSevereMaxPressurePa {
		return 1
	}
	return 0
}
