package main

import "math"

// The anchor rode planner's ratio method and swing maths, ported from
// frontend/src/lib/rode-plan.ts so that the server can answer "how much chain at
// the highest water, and how far does the boat swing" for an anchorage it has
// not reached. The TypeScript is the reference:
// backend/testdata/rode_plan_vectors.json holds inputs and the outputs the
// TypeScript gives, and both suites run every case (rode-plan.vectors.test.ts,
// anchorage_rode_test.go), so the two cannot drift. Change a figure here only
// with the reference, and regenerate the vectors from the reference.
//
// Only the ratio method is ported. The catenary method needs the sea state and
// the seabed, which a stay that has not begun does not have.
//
// The reference also corrects a sounder reading for the tide; here the depth is
// already the depth at the highest water, so the planning depth comes in whole.

const (
	rodeRatioWindThresholdKts = 20.0
	rodeRatioHigh             = 7.0
	rodeRatioNormal           = 5.0
)

// rodeWindBands are the forecast bands the planner offers, as the top of each
// band: a forecast of 15 to 20 means rigging for 20. The open band plans at its
// floor. Bands are lower-inclusive.
var rodeWindBands = []struct{ min, max, plan float64 }{
	{0, 10, 10}, {10, 15, 15}, {15, 20, 20}, {20, 25, 25}, {25, 30, 30}, {30, 40, 40}, {40, 50, 50}, {50, math.Inf(1), 50},
}

// rodePlanWindKts is the wind a kts reading is planned at: the top of its band.
// A reading below the first band (only a nonsensical negative) takes the calmest.
func rodePlanWindKts(kts float64) float64 {
	for _, b := range rodeWindBands {
		if kts >= b.min && kts < b.max {
			return b.plan
		}
	}
	return rodeWindBands[0].plan
}

// rodePlanInput is what the ratio method reads, with the planning depth already
// the depth at the water the rode is planned for. WindKts is nil when no wind
// is known. The JSON names are the reference's, so the shared vectors decode as
// they are.
type rodePlanInput struct {
	PlanningDepthM   float64  `json:"planningDepthM"`
	BowRollerHeightM float64  `json:"bowRollerHeightM"`
	WindKts          *float64 `json:"windKts"`
}

// rodeMethodResult is one method's answer, with the reason when it has none.
type rodeMethodResult struct {
	RecommendedRodeM  float64
	ScopeRatio        float64
	UnavailableReason string
}

// rodeRatioMethod is ratioMethod: (depth + bow roller height) x 7 from 20 knots
// of wind, x 5 below. It reads only depth, wind and bow roller height.
func rodeRatioMethod(in rodePlanInput) rodeMethodResult {
	switch {
	case in.WindKts == nil:
		return rodeMethodResult{UnavailableReason: "no wind data"}
	case in.BowRollerHeightM <= 0:
		return rodeMethodResult{UnavailableReason: "bow roller height not configured"}
	}
	ratio := rodeRatioNormal
	if *in.WindKts >= rodeRatioWindThresholdKts {
		ratio = rodeRatioHigh
	}
	return rodeMethodResult{RecommendedRodeM: (in.PlanningDepthM + in.BowRollerHeightM) * ratio, ScopeRatio: ratio}
}

// resolveLoaM is rode-plan.ts's resolveLoaM: the settings figure when set,
// else the boat's own length overall, else unknown (never zero).
func resolveLoaM(settingsLoaM float64, boatLoaM *float64) (float64, bool) {
	if settingsLoaM > 0 {
		return settingsLoaM, true
	}
	if boatLoaM != nil && *boatLoaM > 0 {
		return *boatLoaM, true
	}
	return 0, false
}

// computeSwingRadiusM is rode + bow offset + the hull's own length.
func computeSwingRadiusM(recommendedRodeM, bowOffsetM, loaM float64) float64 {
	return recommendedRodeM + bowOffsetM + loaM
}
