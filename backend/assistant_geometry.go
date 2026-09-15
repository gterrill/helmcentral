package main

import "math"

// This file is the host-side bearing arithmetic get_wind_forecast's optional
// course_deg parameter uses (executeGetWindForecast, assistant_tools.go).
// Asked once about a 303°T passage course against a forecast SE (135°T)
// wind, the model called it "port beam to port quarter" - wrong; at 168°
// off the bow it is a following wind - and called an NE sea "on or abaft
// the beam" when it was forward of the beam. Modular bearing subtraction is
// exactly the kind of arithmetic a language model gets wrong silently, so
// the host computes it once here and hands the model a label instead of
// raw bearings to subtract (ADR 0093 §2).

// relativeAngleDeg returns the angle, normalised into [0,180], between a
// FROM direction (wind or wave direction, both reported by every provider
// in this codebase as the direction the weather comes from) and courseDeg,
// the vessel's intended course over ground. 0 means the wind/wave is dead
// ahead; 180 means it is dead astern. Both arguments are degrees true.
func relativeAngleDeg(courseDeg, fromDeg float64) int {
	diff := math.Mod(fromDeg-courseDeg, 360)
	if diff < 0 {
		diff += 360
	}
	if diff > 180 {
		diff = 360 - diff
	}
	return int(math.Round(diff))
}

// relativeBearingDeg is relativeAngleDeg's signed counterpart: the bearing,
// expressed relative to heading, in the full 0-360 clockwise range rather
// than folded to 0-180. relativeAngleDeg answers "how many degrees off the
// bow"; this answers "which side" too, which the COLREGS encounter
// classifier needs and wind/wave relative angles never did (port and
// starboard are the same folded magnitude, collision_colregs.go, ADR 0098).
func relativeBearingDeg(headingDeg, bearingDeg float64) int {
	diff := math.Mod(bearingDeg-headingDeg, 360)
	if diff < 0 {
		diff += 360
	}
	return int(math.Round(diff)) % 360
}

// relativeAngleLabel names a relativeAngleDeg result on the standard
// five-band helm scale a delivery skipper actually uses: head (dead ahead),
// bow, beam, quarter, following (dead astern). Boundaries are inclusive on
// their upper edge, so a boundary value never falls into two bands at once.
func relativeAngleLabel(deg int) string {
	switch {
	case deg <= 45:
		return "head"
	case deg <= 80:
		return "bow"
	case deg <= 100:
		return "beam"
	case deg <= 135:
		return "quarter"
	default:
		return "following"
	}
}
