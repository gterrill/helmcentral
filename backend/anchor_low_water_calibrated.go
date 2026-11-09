package main

import (
	"fmt"
	"math"
	"time"
)

// The low-water check with the local tide calibration (ADR 0274) laid on it.
// Until the calibration is ready the check is the station estimate, labelled as
// such. Once ready, the headline figure is the calibrated depth over the cell the
// boat lies in, with its 95% interval, and the verdict is judged on the bottom of
// the interval of the most cautious figure. A ready calibration is the one with
// calibration.ready set; there is no separate method field.

// Which figure the verdict rests on.
const (
	lowWaterCautiousBoat       = "boat_now"
	lowWaterCautiousStation    = "station"
	lowWaterCautiousShallowest = "shallowest_sounded"
)

// lowWaterFigure is a depth in metres with the half-width of its 95% interval.
type lowWaterFigure struct {
	ValueM     float64 `json:"value_m"`
	HalfWidthM float64 `json:"half_width_m"`
}

func figureOf(p tideCalPrediction) *lowWaterFigure {
	return &lowWaterFigure{ValueM: p.ValueM, HalfWidthM: p.HalfWidthM}
}

// lowWaterCalibration is the calibration's part of the answer. The figures are
// there only when it is ready: a calibration that is not ready gives its reason
// and how many hours of readings it has, and nothing to quote. The fields tagged
// "-" are for Mate and never reach the page.
type lowWaterCalibration struct {
	Ready           bool    `json:"ready"`
	NotReady        string  `json:"not_ready,omitempty"`
	NotReadyDetail  string  `json:"not_ready_detail,omitempty"`
	HoursOfReadings float64 `json:"hours_of_readings"`

	// The shallowest cell sounded, projected to the next low. ShallowestDiffers is
	// set when it is worth a line of its own.
	ShallowestSounded *lowWaterFigure `json:"shallowest_sounded,omitempty"`
	ShallowestDiffers bool            `json:"shallowest_differs,omitempty"`

	// The next high, the boat's cell and the deepest cell sounded. There is no
	// verdict on it.
	HighTideTime string `json:"high_tide_time,omitempty"`
	// HighNotAvailable is why there is no high-water figure while the low stands:
	// a reason code, with its detail for Mate.
	HighNotAvailable       string          `json:"high_not_available,omitempty"`
	HighNotAvailableDetail string          `json:"-"`
	HighAtBoat             *lowWaterFigure `json:"high_at_boat,omitempty"`
	DeepestSounded         *lowWaterFigure `json:"deepest_sounded,omitempty"`
	DeepestDiffers         bool            `json:"deepest_differs,omitempty"`

	// When the calibrated and station depths for the low differ by more than the
	// margin, what the tide here does instead.
	StationMatchNote string `json:"station_match_note,omitempty"`

	// For Mate: k is the share of the station's tide range the water here runs,
	// with the half-width of its 95% interval, and TideMinutesLater how many
	// minutes later the tide here runs than the station's (negative: earlier).
	// That is the fit's offset with its sign turned: the fit reads the station's
	// curve at t + offset, so a positive offset is a tide that runs earlier.
	K                  *float64 `json:"-"`
	KHalfWidth         *float64 `json:"-"`
	TideMinutesLater   *int     `json:"-"`
	StationName        string   `json:"-"`
	StationDistanceM   *float64 `json:"-"`
	StationDepthAtLowM *float64 `json:"-"`
}

// calibrationResult is what a lowWaterCalibrator found: an estimate when the
// calibration is ready, otherwise the one reason it is not.
type calibrationResult struct {
	Est              *tideCalEstimate
	Err              *tideCalError
	HoursOfReadings  float64
	StationName      string
	StationDistanceM *float64
	// StationIsPoint is set when the tide comes from a position and not a
	// station, so the note names no station and no distance.
	StationIsPoint bool
	// StationFallM is how far the station's curve falls from now to the station
	// low the local low was shifted from, never below zero. The station figure
	// for the verdict is the depth now less this, so it and the calibrated depth
	// are for the same tide.
	StationFallM float64
}

// lowWaterCalibrator works out the calibration for the tide the check used. It
// is built for the watch that is set, so it never reads the watch itself.
type lowWaterCalibrator func(tide tideTodayResponse, marginM float64, now time.Time) *calibrationResult

func roundedPtr(v float64, places int) *float64 { return floatPtr(roundTo(v, places)) }

// applyCalibration lays the calibration on the station-based check in out, which
// is an ok or too_shallow verdict. Before the calibration is ready the verdict
// stays the station's, with the calibration's state beside it.
func applyCalibration(out lowWaterClearance, res *calibrationResult, marginM float64) lowWaterClearance {
	cal := &lowWaterCalibration{HoursOfReadings: roundTo(res.HoursOfReadings, 2), StationName: res.StationName, StationDistanceM: res.StationDistanceM}
	if res.StationIsPoint {
		cal.StationName, cal.StationDistanceM = "", nil
	}
	out.Calibration = cal
	if res.Err != nil {
		cal.NotReady, cal.NotReadyDetail = res.Err.Code, res.Err.Detail
		return out
	}
	est := res.Est

	cal.Ready = true
	fit := est.Fit
	cal.K = roundedPtr(fit.K, 3)
	cal.KHalfWidth = roundedPtr(fit.KHalf, 3)
	later := -fit.OffsetMin
	cal.TideMinutesLater = &later
	// The station's depth at the same low the calibrated depth is for, which is
	// not always the tide response's next station low: see publishedCalibrator.
	stationDepth := *out.DepthNowM - res.StationFallM
	cal.StationDepthAtLowM = floatPtr(stationDepth)
	here, shallow := est.LowHere, est.LowShallowest
	cal.ShallowestSounded = figureOf(shallow)
	if est.HighHere != nil {
		cal.HighTideTime = est.HighHere.At.Format(time.RFC3339)
		cal.HighAtBoat, cal.DeepestSounded = figureOf(*est.HighHere), figureOf(*est.HighDeepest)
		cal.DeepestDiffers = est.DeepestApart
	} else if est.HighUnavailable != nil {
		cal.HighNotAvailable, cal.HighNotAvailableDetail = est.HighUnavailable.Code, est.HighUnavailable.Detail
	}

	// The verdict rests on the bottom of the interval of the most cautious figure:
	// the boat's own cell, the station estimate (which has no interval, so its
	// bottom is itself), or the shallowest cell sounded with the allowance the
	// backtest called for. On a tie the first listed wins.
	type candidate struct {
		source string
		bottom float64
	}
	cautious := candidate{lowWaterCautiousBoat, here.BottomM()}
	for _, c := range []candidate{
		{lowWaterCautiousStation, stationDepth},
		{lowWaterCautiousShallowest, shallow.BottomM() + tideCalCautionM},
	} {
		if c.bottom < cautious.bottom {
			cautious = c
		}
	}
	clearanceBottom := cautious.bottom - *out.DraftM
	out.Status = lowWaterOK
	if clearanceBottom < 0 || math.Round(clearanceBottom*10)/10 < marginM {
		out.Status = lowWaterTooShallow
	}
	out.CautiousSource = cautious.source
	switch cautious.source {
	case lowWaterCautiousBoat:
		out.VerdictClearanceM, out.VerdictHalfWidthM = floatPtr(here.ValueM-*out.DraftM), floatPtr(here.HalfWidthM)
	case lowWaterCautiousStation:
		out.VerdictClearanceM = floatPtr(stationDepth - *out.DraftM)
	default:
		// The shallowest spot as the verdict judged it, with its allowance.
		out.VerdictClearanceM, out.VerdictHalfWidthM = floatPtr(shallow.ValueM+tideCalCautionM-*out.DraftM), floatPtr(shallow.HalfWidthM)
	}
	cal.ShallowestDiffers = est.ShallowestApart || cautious.source == lowWaterCautiousShallowest

	// The headline stays the figure for where the boat lies now.
	// The low and the high are reported at the time they come here, which is the
	// station's time less the offset.
	out.LowTideTime = here.At.Format(time.RFC3339)
	out.TideFallM = nil
	out.DepthAtLowM = floatPtr(here.ValueM)
	out.DepthAtLowHalfWidthM = floatPtr(here.HalfWidthM)
	out.ClearanceM = floatPtr(here.ValueM - *out.DraftM)

	if math.Abs(here.ValueM-stationDepth) > marginM {
		cal.StationMatchNote = stationMatchNote(res.StationName, res.StationDistanceM, res.StationIsPoint, fit.K)
	}
	return out
}

// stationMatchNote says the tide here does not follow the prediction's: k is the
// share of its range the water here runs. A tide from a position, with no
// station, is called the tide model and has no distance.
func stationMatchNote(name string, distanceM *float64, isPoint bool, k float64) string {
	what := name
	switch {
	case isPoint:
		what = "the tide model"
	case name == "":
		what = "the tide station"
	}
	away := ""
	if distanceM != nil && !isPoint {
		km := *distanceM / 1000
		if km >= 10 {
			away = fmt.Sprintf(" (%.0f km away)", km)
		} else {
			away = fmt.Sprintf(" (%.1f km away)", km)
		}
	}
	return fmt.Sprintf("The tide here doesn't follow %s%s: it runs %.0f%% of its range.", what, away, k*100)
}

// publishedCalibrator is the calibrator for the watch that is set. It reads the
// fit the worker published and predicts from it: no chart, no store, no lock the
// recorder holds. Before the worker has published a fit for this watch and
// anchor it says the calibration is being worked out.
func publishedCalibrator(watch anchorWatchData) lowWaterCalibrator {
	return func(tide tideTodayResponse, marginM float64, now time.Time) *calibrationResult {
		res := &calibrationResult{}
		e := tideCalPublishedEntry(watch)
		if e == nil {
			res.Err = tideCalErr(tideCalWorking, "")
			return res
		}
		res.HoursOfReadings, res.StationName, res.StationDistanceM, res.StationIsPoint = e.hours, e.stationName, e.stationDistanceM, e.stationIsPoint
		if e.err != nil {
			res.Err = e.err
			return res
		}
		// The next low and high are the first that come here after now, which is
		// not always the station's next: the station's low may have passed while
		// the local one is still to come, or the other way round.
		lowAt, highAt := e.fit.nextLocal(now)
		if lowAt.IsZero() {
			res.Err = tideCalErr(tideCalNoTideCurve, "no low tide ahead in the stored predictions")
			return res
		}
		nowH, okNow := e.fit.Curve.heightAt(now)
		if !okNow {
			res.Err = tideCalErr(tideCalNoTideCurve, "the station's tide curve does not reach now, %s", now.UTC().Format(time.RFC3339))
			return res
		}
		lowH, okLow := e.fit.Curve.heightAt(lowAt)
		if !okLow {
			res.Err = tideCalErr(tideCalNoTideCurve, "the station's tide curve does not reach the low at %s", lowAt.UTC().Format(time.RFC3339))
			return res
		}
		res.StationFallM = math.Max(0, nowH-lowH)
		res.Est, res.Err = e.fit.estimate(lowAt, highAt, marginM, now)
		return res
	}
}

// liveLowWaterCalibrator calibrates the watch the caller read, or answers nil when
// no watch is set.
func liveLowWaterCalibrator(watch anchorWatchData, active bool) lowWaterCalibrator {
	if !active {
		return nil
	}
	return publishedCalibrator(watch)
}
