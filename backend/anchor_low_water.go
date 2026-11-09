package main

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

// The Anchor Watch's low-water clearance (ADR 0135, ADR 0272): the depth at the
// boat's own position, projected forward to the next low tide, less the boat's
// draft, against the water the operator wants under the keel. The server holds
// the one calculation. The Anchor Watch page reads it from
// GET /api/anchor-watch/low-water and Mate reads it in get_vessel_status, so
// the two can never give the operator different figures.
//
// No masking fallback: a missing depth, draft or tide is an explicit unknown
// with a reason, never a zero fall or a zero draft.

const (
	lowWaterOK         = "ok"
	lowWaterTooShallow = "too_shallow"
	lowWaterUnknown    = "unknown"

	lowWaterNoDepth   = "no_depth"
	lowWaterNoDraft   = "no_draft"
	lowWaterTideStale = "tide_stale"
	// The tide is missing for one of three reasons, each its own word: no
	// station is set up, the service could not be reached, or it answered with
	// no usable height or upcoming low.
	lowWaterNoTideStation   = "no_tide_station"
	lowWaterTideUnreachable = "tide_unreachable"
	lowWaterNoTide          = "no_tide"
	// lowWaterNotAnchored is the page's answer when the boat is neither anchored
	// or moored nor under a watch: there is no anchorage to check.
	lowWaterNotAnchored = "not_anchored"

	// A tide whose "next low" has already passed is stale. The server builds the
	// tide fresh on every request, so there is no age rule: a reading cannot be
	// older than the request that asked for it.
)

// lowWaterInput is what the check reasons from. A nil DepthM or DraftM, or one
// the boat reports as the -1 sentinel, is a reading that is not there.
type lowWaterInput struct {
	DepthM  *float64
	Tide    *tideTodayResponse
	DraftM  *float64
	MarginM float64
	Now     time.Time
	// TideReason says why Tide is nil: lowWaterNoTideStation or
	// lowWaterTideUnreachable. Empty means the tide came back without a usable
	// height, which reads as lowWaterNoTide.
	TideReason string
}

// lowWaterClearance is the verdict and the figures behind it, in metres. Status
// is "ok", "too_shallow" or "unknown"; an unknown has a Reason and only the
// inputs that were there. LowTideTime is the next low's RFC 3339 instant.
type lowWaterClearance struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
	// Detail says in words why an input is missing when the reason alone does
	// not, such as which part of the tide setup is absent.
	Detail      string   `json:"detail,omitempty"`
	DepthNowM   *float64 `json:"depth_now_m,omitempty"`
	TideFallM   *float64 `json:"tide_fall_m,omitempty"`
	DepthAtLowM *float64 `json:"depth_at_low_m,omitempty"`
	DraftM      *float64 `json:"draft_m,omitempty"`
	ClearanceM  *float64 `json:"clearance_m,omitempty"`
	MarginM     *float64 `json:"margin_m,omitempty"`
	LowTideTime string   `json:"low_tide_time,omitempty"`
	// While a calibration is ready (Calibration.Ready, ADR 0274) DepthAtLowM is
	// the depth the sounder's own readings at this anchorage point to, with the
	// half width of its 95% interval below. Otherwise it is the station's
	// prediction applied to the depth now, and has no interval.
	// DepthAtLowHalfWidthM is that half-width, in metres.
	DepthAtLowHalfWidthM *float64 `json:"depth_at_low_half_width_m,omitempty"`
	// CautiousSource says which figure the verdict rests on once calibrated:
	// "boat_now", "station" or "shallowest_sounded".
	CautiousSource string `json:"cautious_source,omitempty"`
	// VerdictClearanceM is the clearance of the figure the verdict rests on, with
	// the half width of its interval when it has one (the station estimate has
	// none): what the warning should show, which is not always the headline.
	VerdictClearanceM *float64             `json:"verdict_clearance_m,omitempty"`
	VerdictHalfWidthM *float64             `json:"verdict_half_width_m,omitempty"`
	Calibration       *lowWaterCalibration `json:"calibration,omitempty"`
	// WorkedOutAt is when the server worked this out, RFC 3339, set by the
	// endpoint so the page can say how old a verdict is when a later poll fails.
	WorkedOutAt string `json:"worked_out_at,omitempty"`
}

// usableTideHeightFt is whether a tide height in feet is real data rather than
// the -1 sentinel buildTideToday uses for "not published". The sentinel is
// exactly -1, not "-1 or below": heights below chart datum are real, and a
// spring low can read below -1 ft on some datums.
func usableTideHeightFt(ft float64) bool {
	return finite(ft) && ft != noTideExtremeHeightFt
}

// finite is a real number: not NaN, not infinite.
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// computeLowWaterClearance projects the depth at the boat's position to the next
// low tide and compares the water left under the keel with the margin. The
// transducer's own immersion is not published, so depth under-reads and both
// figures run slightly conservative.
func computeLowWaterClearance(in lowWaterInput) lowWaterClearance {
	margin := in.MarginM
	out := lowWaterClearance{Status: lowWaterUnknown, MarginM: &margin}
	depthOK := in.DepthM != nil && finite(*in.DepthM) && *in.DepthM >= 0
	draftOK := in.DraftM != nil && finite(*in.DraftM) && *in.DraftM > 0
	if depthOK {
		out.DepthNowM = floatPtr(*in.DepthM)
	}
	if draftOK {
		out.DraftM = floatPtr(*in.DraftM)
	}
	if !depthOK {
		out.Reason = lowWaterNoDepth
		return out
	}
	if !draftOK {
		out.Reason = lowWaterNoDraft
		return out
	}
	if in.Tide == nil {
		out.Reason = lowWaterNoTide
		if in.TideReason != "" {
			out.Reason = in.TideReason
		}
		return out
	}
	if !usableTideHeightFt(in.Tide.CurrentTideHeightFt) || !usableTideHeightFt(in.Tide.LowTideHeightFt) {
		out.Reason = lowWaterNoTide
		return out
	}

	// The low the tide names must still be ahead. An unparseable time counts as
	// passed rather than let a zero time produce a false verdict.
	if lowAt, err := time.Parse(time.RFC3339, in.Tide.LowTideTime); err != nil || !lowAt.After(in.Now) {
		out.Reason = lowWaterTideStale
		return out
	}

	// Already at or below the next low's forecast height: the shallowest water
	// before that low is now, so the fall clamps to zero. A negative fall would
	// raise the projected depth, the wrong way for a safety warning.
	fallM := math.Max(0, in.Tide.CurrentTideHeightFt-in.Tide.LowTideHeightFt) / metersToFeet
	depthAtLow := *in.DepthM - fallM
	clearance := depthAtLow - *in.DraftM
	out.Status = lowWaterOK
	// Judged as displayed, as anchorage_details judges it: 0.46 m reads as
	// 0.5 m and meets a 0.5 m margin. A clearance below zero, the keel under the
	// bottom on paper, is too shallow whatever the margin.
	if clearance < 0 || math.Round(clearance*10)/10 < in.MarginM {
		out.Status = lowWaterTooShallow
	}
	out.TideFallM, out.DepthAtLowM, out.ClearanceM = floatPtr(fallM), floatPtr(depthAtLow), floatPtr(clearance)
	out.LowTideTime = in.Tide.LowTideTime
	return out
}

// buildLowWater reads the boat, the tide and the operator's margin and judges
// them. The depth counts only while the sounder is still reporting, the same
// freshness get_vessel_status gives it. A tide that cannot be built is an
// unknown with the reason in words. An unreadable margin setting is an error:
// the check has nothing to compare against, and a default would pass for the
// operator's own figure.
func buildLowWater(state vesselStateData, tide func() (tideTodayResponse, error), margin func() (float64, error), calibrate lowWaterCalibrator, now time.Time) (lowWaterClearance, error) {
	marginM, err := margin()
	if err != nil {
		return lowWaterClearance{}, fmt.Errorf("the clearance wanted at low water could not be read: %w", err)
	}
	in := lowWaterInput{MarginM: marginM, Now: now, DraftM: floatPtr(state.DraftM)}
	// Only the sounder's staleness is judged here; computeLowWaterClearance
	// holds the rules for what a depth or draft may be. The limit and the
	// unknown-age rule are get_vessel_status's own for the depth reading, so
	// the two never disagree: an unknown age is no depth.
	if age, known := secondsAge(state.DepthLastUpdateAge); known && age <= vesselStatusFreshAge {
		in.DepthM = floatPtr(state.Depth)
	}
	// With no depth or no draft there is nothing to project, so the tide is not
	// built.
	if out := computeLowWaterClearance(in); out.Reason == lowWaterNoDepth || out.Reason == lowWaterNoDraft {
		return out, nil
	}
	detail := ""
	resp, terr := tide()
	if terr != nil {
		detail = tideUnavailableSentence(terr)
		in.TideReason = lowWaterTideUnreachable
		var te *tideTodayError
		if errors.As(terr, &te) && te.reason == tideNotSetUp {
			in.TideReason = lowWaterNoTideStation
		}
	} else {
		in.Tide = &resp
	}
	out := computeLowWaterClearance(in)
	if terr != nil {
		out.Detail = detail
	}
	// The local calibration (ADR 0274) lays on a station verdict; with no
	// verdict there is nothing for it to refine, and the reason stands.
	if calibrate != nil && out.Status != lowWaterUnknown {
		if res := calibrate(*in.Tide, marginM, now); res != nil {
			out = applyCalibration(out, res, marginM)
		}
	}
	return out, nil
}

// lowWaterApplies is the one gate for the low-water check, used by the page's
// endpoint and by get_vessel_status: there is an anchorage to check when the
// boat is anchored or moored, or an anchor watch is set.
func lowWaterApplies(state vesselStateData, watchSet bool) bool {
	return watchSet || isAnchoredOrMooredState(state.Status)
}

// lowWaterNotAnchoredWhy is what get_vessel_status says when the gate is shut.
const lowWaterNotAnchoredWhy = "the boat is not anchored and no anchor watch is set"

// getAnchorLowWater is GET /api/anchor-watch/low-water.
func getAnchorLowWater(c echo.Context) error {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	watch, active, _ := currentAnchorWatch()
	return serveAnchorLowWater(c, fetchSignalKVesselState,
		func() bool { return active },
		func(now time.Time) (tideTodayResponse, error) {
			resp, _, err := buildTideToday(settingsPath, now)
			return resp, err
		},
		func() (float64, error) {
			s, err := anchorSettingsFromSettingsFile(settingsPath)()
			return s.MinClearanceAtLowM, err
		}, liveLowWaterCalibrator(watch, active))
}

func serveAnchorLowWater(c echo.Context, vesselState func() (vesselStateData, error), watchSet func() bool, tide func(time.Time) (tideTodayResponse, error), margin func() (float64, error), calibrate lowWaterCalibrator) error {
	state, err := vesselState()
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("the boat's state is unavailable: %v", err)})
	}
	c.Response().Header().Set("Cache-Control", "no-store")
	now := time.Now().UTC()
	if !lowWaterApplies(state, watchSet()) {
		// No tide is built and no margin read: there is nothing to check.
		return c.JSON(http.StatusOK, lowWaterClearance{Status: lowWaterUnknown, Reason: lowWaterNotAnchored, WorkedOutAt: now.Format(time.RFC3339)})
	}
	out, err := buildLowWater(state, func() (tideTodayResponse, error) { return tide(now) }, margin, calibrate, now)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	out.WorkedOutAt = now.Format(time.RFC3339)
	return c.JSON(http.StatusOK, out)
}
