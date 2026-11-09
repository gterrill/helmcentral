package main

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// ── spikes and noise ────────────────────────────────────────────────────

func flatMinutes(n int, depth float64) []tideCalReading {
	t0 := time.Date(2026, 11, 8, 1, 0, 0, 0, time.UTC)
	out := make([]tideCalReading, n)
	for i := range out {
		out[i] = tideCalReading{At: t0.Add(time.Duration(i) * time.Minute), DepthM: depth + 0.01*float64(i%3)}
	}
	return out
}

// No filter throws readings away in time order, which would lose real ones when
// the boat swings onto another patch. The Huber weights count spikes down, and an
// estimate with spikes in it stays inside the interval of the clean one.
func TestSpikesMoveTheEstimateLessThanItsInterval(t *testing.T) {
	curve := loadKeppelCurve(t)
	now := keppelDrop.Add(5 * time.Hour)
	clean := readingsUntil(keppelMinutes(t, curve), now)
	want, err := estimateTideCalibration(clean, keppelAnchorLat, keppelAnchorLon, curve, keppelLow, keppelHigh, 5, now)
	if err != nil {
		t.Fatal(err)
	}
	spiked := append([]tideCalReading(nil), clean...)
	for i := 20; i < len(spiked)-5; i += 23 {
		if (i/23)%2 == 0 {
			spiked[i].DepthM += 4 // a fish under the transducer
		} else {
			spiked[i].DepthM = 0.3 // a dropout
		}
	}
	got, err := estimateTideCalibration(spiked, keppelAnchorLat, keppelAnchorLon, curve, keppelLow, keppelHigh, 5, now)
	if err != nil {
		t.Fatal(err)
	}
	if d := math.Abs(got.LowHere.ValueM - want.LowHere.ValueM); d > want.LowHere.HalfWidthM {
		t.Errorf("spikes moved the low from %.2f to %.2f, more than its ±%.2f", want.LowHere.ValueM, got.LowHere.ValueM, want.LowHere.HalfWidthM)
	}
	if math.Abs(got.Fit.K-want.Fit.K) > 0.1 {
		t.Errorf("spikes moved k from %.3f to %.3f", want.Fit.K, got.Fit.K)
	}
}

func TestTheSelectionMedianAgreesWithSorting(t *testing.T) {
	seed := uint32(3)
	for n := 1; n < 60; n++ {
		xs := make([]float64, n)
		for i := range xs {
			seed = seed*1664525 + 1013904223
			xs[i] = float64(seed>>20) / 100
			if i%7 == 3 {
				xs[i] = 5 // ties
			}
		}
		orig := append([]float64(nil), xs...)
		if got, want := selectMedian(make([]float64, n), xs), median(xs); got != want {
			t.Fatalf("n=%d: %v, want %v", n, got, want)
		}
		for i := range xs {
			if xs[i] != orig[i] {
				t.Fatalf("n=%d: the input was reordered", n)
			}
		}
	}
}

// ── readiness ───────────────────────────────────────────────────────────

func TestReadinessIsTheIntervalAgainstTheMargin(t *testing.T) {
	curve := loadKeppelCurve(t)
	minutes := keppelMinutes(t, curve)
	now := keppelDrop.Add(3 * time.Hour)
	rs := readingsUntil(minutes, now)

	wide, err := estimateTideCalibration(rs, keppelAnchorLat, keppelAnchorLon, curve, keppelLow, keppelHigh, 5, now)
	if err != nil {
		t.Fatalf("a 5 m margin: %v", err)
	}
	hw := wide.LowHere.HalfWidthM

	// The same readings, a margin just under their own half width: not ready,
	// and the reason is that more readings are needed, not that the sounder is
	// noisy.
	_, err = estimateTideCalibration(rs, keppelAnchorLat, keppelAnchorLon, curve, keppelLow, keppelHigh, hw*0.99, now)
	if code := notReadyCode(err); code != tideCalTooFewReadings {
		t.Fatalf("a margin under the interval: %v", err)
	}
	if _, err := estimateTideCalibration(rs, keppelAnchorLat, keppelAnchorLon, curve, keppelLow, keppelHigh, hw*1.01, now); err != nil {
		t.Errorf("a margin just over the interval (%.3f) is not ready: %v", hw, err)
	}
}

func TestANoisySounderKeepsTheIntervalWideAndSaysSo(t *testing.T) {
	curve := loadKeppelCurve(t)
	now := keppelDrop.Add(3 * time.Hour)
	rs := readingsUntil(keppelMinutes(t, curve), now)
	// Deterministic scatter of up to +/-0.35 m, not spikes the filter would take
	// out: every reading is off by something.
	seed := uint32(7)
	for i := range rs {
		seed = seed*1664525 + 1013904223
		rs[i].DepthM += (float64(seed>>8)/float64(1<<24) - 0.5) * 0.7
	}
	_, err := estimateTideCalibration(rs, keppelAnchorLat, keppelAnchorLon, curve, keppelLow, keppelHigh, 0.3, now)
	if code := notReadyCode(err); code != tideCalTooScattered {
		t.Errorf("got %v, want not ready as %q", err, tideCalTooScattered)
	}
}

func TestFewReadingsAreAnExplicitState(t *testing.T) {
	curve := loadKeppelCurve(t)
	_, err := estimateTideCalibration(nil, keppelAnchorLat, keppelAnchorLon, curve, keppelLow, keppelHigh, 0.5, keppelDrop)
	var te *tideCalError
	if !errors.As(err, &te) || te.Code != tideCalTooFewReadings {
		t.Errorf("no readings: %v", err)
	}
	rs := readingsUntil(keppelMinutes(t, curve), keppelDrop.Add(8*time.Minute))
	_, err = estimateTideCalibration(rs, keppelAnchorLat, keppelAnchorLon, curve, keppelLow, keppelHigh, 0.5, keppelDrop.Add(8*time.Minute))
	if !errors.As(err, &te) || te.Code != tideCalTooFewReadings {
		t.Errorf("eight minutes: %v", err)
	}
}

func TestReadingsOlderThanTheLatestFewMinutesGiveNoBoatFigure(t *testing.T) {
	curve := loadKeppelCurve(t)
	last := keppelDrop.Add(3 * time.Hour)
	rs := readingsUntil(keppelMinutes(t, curve), last)
	_, err := estimateTideCalibration(rs, keppelAnchorLat, keppelAnchorLon, curve, keppelLow, keppelHigh, 0.5, last.Add(time.Hour))
	if code := notReadyCode(err); code != tideCalNoRecentReading {
		t.Errorf("%v", err)
	}
	// Just inside the sounder's staleness limit and the minute it takes to store a
	// reading, the latest minute still counts.
	if _, err := estimateTideCalibration(rs, keppelAnchorLat, keppelAnchorLon, curve, keppelLow, keppelHigh, 5, last.Add(tideCalRecencyLimit-tideCalRecordInterval)); err != nil {
		t.Errorf("at the limit: %v", err)
	}
}

func TestHighWaterFiguresMirrorTheLowWaterOnes(t *testing.T) {
	curve := loadKeppelCurve(t)
	now := keppelDrop.Add(3 * time.Hour)
	est, err := estimateTideCalibration(readingsUntil(keppelMinutes(t, curve), now), keppelAnchorLat, keppelAnchorLon, curve, keppelLow, keppelHigh, 5, now)
	if err != nil || est.HighHere == nil || est.HighDeepest == nil {
		t.Fatalf("%v", err)
	}
	// The station's range from the 4.45 AM low to the 10.27 AM high is 3.2 m; the
	// water here runs about 0.85 of it.
	if rise := est.HighHere.ValueM - est.LowHere.ValueM; math.Abs(rise-0.85*3.2) > 0.35 {
		t.Errorf("rise from low to high = %.2f m, want about 2.7", rise)
	}
	// The boat's own cell is a candidate, so the deepest spot's lower bound is no
	// lower than its, and the shallowest spot's upper bound no higher.
	if est.HighDeepest.BottomM() < est.HighHere.BottomM()-1e-9 || est.LowShallowest.ValueM+est.LowShallowest.HalfWidthM > est.LowHere.ValueM+est.LowHere.HalfWidthM+1e-9 {
		t.Errorf("deepest at high %+v against the boat's cell %+v; shallowest at low %+v against %+v", est.HighDeepest, est.HighHere, est.LowShallowest, est.LowHere)
	}
	if est.HighDeepest.HalfWidthM <= 0 || est.HighHere.HalfWidthM <= 0 {
		t.Errorf("high-water figures carry no interval: %+v %+v", est.HighHere, est.HighDeepest)
	}
	// No upcoming high is a missing figure, not a zero.
	none, err := estimateTideCalibration(readingsUntil(keppelMinutes(t, curve), now), keppelAnchorLat, keppelAnchorLon, curve, keppelLow, time.Time{}, 5, now)
	if err != nil || none.HighHere != nil || none.HighDeepest != nil {
		t.Errorf("no high: %v", err)
	}
}

// ── the verdict ─────────────────────────────────────────────────────────

// stationVerdict is the station-based result applyCalibration lays on: 2.2 m at
// low with a 1.2 m draft.
func stationVerdict(depthAtLow float64) lowWaterClearance {
	return lowWaterClearance{
		Status: lowWaterOK, DepthNowM: floatPtr(3), DepthAtLowM: floatPtr(depthAtLow), TideFallM: floatPtr(3 - depthAtLow),
		DraftM: floatPtr(1.2), ClearanceM: floatPtr(depthAtLow - 1.2), MarginM: floatPtr(0.5), LowTideTime: "2026-11-08T04:45:00Z",
	}
}

// applyStation lays the calibration on the station verdict for a low at
// depthAtLow, with the station fall that goes with it (3 m now, as the verdict has).
func applyStation(depthAtLow float64, res *calibrationResult, marginM float64) lowWaterClearance {
	res.StationFallM = 3 - depthAtLow
	return applyCalibration(stationVerdict(depthAtLow), res, marginM)
}

func readyEstimate(here, hereHW, shallow, shallowHW float64) *calibrationResult {
	fit := &tideCalFit{tideCalMember: &tideCalMember{K: 0.85, KHalf: 0.02, OffsetMin: 5}}
	return &calibrationResult{
		HoursOfReadings: 4, StationName: "Middle Island", StationDistanceM: floatPtr(11000),
		Est: &tideCalEstimate{
			Fit: fit, LowHere: tideCalPrediction{ValueM: here, HalfWidthM: hereHW, At: keppelLow}, LowShallowest: tideCalPrediction{ValueM: shallow, HalfWidthM: shallowHW, At: keppelLow},
			ShallowestApart: here-shallow >= tideCalCautionM,
		},
	}
}

// notReadyCode is the code of a calibration that is not ready, or "" for any
// other error or none.
func notReadyCode(err error) string {
	var te *tideCalError
	if errors.As(err, &te) {
		return te.Code
	}
	return ""
}

func TestVerdictRestsOnTheBottomOfTheInterval(t *testing.T) {
	// 2.0 m at low less a 1.2 m draft leaves 0.8 m, over the 0.5 m margin; the
	// bottom of a +/-0.6 m interval leaves 0.2 m, under it.
	got := applyStation(2.0, readyEstimate(2.0, 0.6, 2.0, 0.1), 0.5)
	if got.Status != lowWaterTooShallow || !got.Calibration.Ready || got.CautiousSource != lowWaterCautiousBoat {
		t.Errorf("wide interval: %+v", got)
	}
	// The headline is still the boat's own cell, and still shows the middle.
	lwFloat(t, "depth_at_low", got.DepthAtLowM, 2.0, 1e-9)
	lwFloat(t, "clearance", got.ClearanceM, 0.8, 1e-9)
	lwFloat(t, "half width", got.DepthAtLowHalfWidthM, 0.6, 1e-9)
	if got.TideFallM != nil {
		t.Errorf("a calibrated figure carries the station's tide fall: %v", *got.TideFallM)
	}

	narrow := applyStation(2.0, readyEstimate(2.0, 0.1, 2.0, 0.1), 0.5)
	if narrow.Status != lowWaterOK {
		t.Errorf("narrow interval: %+v", narrow)
	}
}

func TestVerdictKeepsTheRulesOfThePage(t *testing.T) {
	// Judged as displayed: a bottom leaving 0.46 m reads 0.5 m and meets a 0.5 m
	// margin; 0.44 m reads 0.4 m and does not.
	if got := applyStation(3, readyEstimate(1.66+0.1, 0.1, 1.76, 0.1), 0.5); got.Status != lowWaterOK {
		t.Errorf("0.46 m under the keel at the bottom: %s", got.Status)
	}
	if got := applyStation(3, readyEstimate(1.64+0.1, 0.1, 1.74, 0.1), 0.5); got.Status != lowWaterTooShallow {
		t.Errorf("0.44 m: %s", got.Status)
	}
	// Below zero is too shallow whatever the margin.
	if got := applyStation(3, readyEstimate(1.3, 0.2, 1.3, 0.1), 0); got.Status != lowWaterTooShallow {
		t.Errorf("keel through the bottom with a zero margin: %s", got.Status)
	}
}

func TestVerdictTakesTheMostCautiousFigure(t *testing.T) {
	// Boat's cell 2.6 +/- 0.1 (bottom 2.5); station 2.1; shallowest 1.5 + 0.2
	// allowance = 1.7 - 0.1 = bottom 1.6. The shallowest spot is the most
	// cautious, and the page is told so and shows it.
	got := applyStation(2.1, readyEstimate(2.6, 0.1, 1.5, 0.1), 0.5)
	if got.CautiousSource != lowWaterCautiousShallowest || got.Status != lowWaterTooShallow {
		t.Errorf("shallowest: %+v", got)
	}
	if !got.Calibration.ShallowestDiffers {
		t.Error("the shallowest spot set the verdict but is not shown")
	}

	// A shallowest spot 0.15 m under the boat's is not worth a line, and with the
	// allowance it is not the cautious figure either.
	near := applyStation(2.9, readyEstimate(2.6, 0.1, 2.45, 0.05), 0.5)
	if near.CautiousSource != lowWaterCautiousBoat || near.Calibration.ShallowestDiffers {
		t.Errorf("near: %s differs=%v", near.CautiousSource, near.Calibration.ShallowestDiffers)
	}

	// The station estimate, with no interval, is the cautious figure when it is the
	// lowest.
	st := applyStation(1.6, readyEstimate(2.6, 0.1, 2.5, 0.1), 0.5)
	if st.CautiousSource != lowWaterCautiousStation || st.Status != lowWaterTooShallow {
		t.Errorf("station: %+v", st)
	}
}

func TestBeforeReadyTheStationEstimateStandsUnderItsOwnLabel(t *testing.T) {
	res := &calibrationResult{HoursOfReadings: 0.5, Err: tideCalErr(tideCalTooFewReadings, "")}
	got := applyStation(2.0, res, 0.5)
	if got.Calibration == nil || got.Calibration.Ready || got.Calibration.NotReady != tideCalTooFewReadings {
		t.Fatalf("%+v", got)
	}
	lwFloat(t, "depth_at_low", got.DepthAtLowM, 2.0, 1e-9)
	if got.DepthAtLowHalfWidthM != nil || got.Calibration.ShallowestSounded != nil || got.Calibration.K != nil {
		t.Errorf("figures given before the calibration is ready: %+v", got.Calibration)
	}
	if got.Calibration.HoursOfReadings != 0.5 {
		t.Errorf("hours = %v", got.Calibration.HoursOfReadings)
	}

	// A calibration that fails says so and does not stand in for anything.
	failed := applyStation(2.0, &calibrationResult{Err: tideCalUnavailableErr(errors.New("the store is closed"))}, 0.5)
	if failed.Calibration.Ready || failed.Calibration.NotReady != tideCalUnavailable || !strings.Contains(failed.Calibration.NotReadyDetail, "store is closed") {
		t.Errorf("%+v", failed.Calibration)
	}
}

// ── the tide-match note ─────────────────────────────────────────────────

func TestStationMatchNoteFiresOnlyPastTheMargin(t *testing.T) {
	// Calibrated 2.6 m against the station's 2.0 m: 0.6 m apart, over a 0.5 m margin.
	got := applyStation(2.0, readyEstimate(2.6, 0.1, 2.6, 0.1), 0.5)
	want := "The tide here doesn't follow Middle Island (11 km away): it runs 85% of its range."
	if got.Calibration.StationMatchNote != want {
		t.Errorf("note = %q, want %q", got.Calibration.StationMatchNote, want)
	}
	// 0.4 m apart: it follows well enough.
	if got := applyStation(2.2, readyEstimate(2.6, 0.1, 2.6, 0.1), 0.5); got.Calibration.StationMatchNote != "" {
		t.Errorf("note under the margin: %q", got.Calibration.StationMatchNote)
	}
	// No distance is left out, not guessed.
	res := readyEstimate(2.6, 0.1, 2.6, 0.1)
	res.StationDistanceM = nil
	if got := applyStation(2.0, res, 0.5); strings.Contains(got.Calibration.StationMatchNote, "away") {
		t.Errorf("a distance that is not known is given: %q", got.Calibration.StationMatchNote)
	}
	// And nearby stations read in tenths of a kilometre.
	res = readyEstimate(2.6, 0.1, 2.6, 0.1)
	res.StationDistanceM = floatPtr(2300)
	if got := applyStation(2.0, res, 0.5); !strings.Contains(got.Calibration.StationMatchNote, "(2.3 km away)") {
		t.Errorf("%q", got.Calibration.StationMatchNote)
	}
}

// ── the stored minutes, the restart, the watch's life ──────────────────

func keppelWatch() anchorWatchData {
	return anchorWatchData{Lat: keppelAnchorLat, Lon: keppelAnchorLon, RadiusMeters: 65, SetAt: keppelDrop}
}

func keppelChart(t *testing.T) func() (tideCalChart, error) {
	curve := loadKeppelCurve(t)
	return func() (tideCalChart, error) {
		return tideCalChart{
			tideChartResult: tideChartResult{
				Station:  tideStation{StationID: "mi", Name: "Middle Island", Lat: -23.08, Lon: 150.9},
				Extremes: []tideExtremePoint(curve),
			},
			Source: "bom/mi",
		}, nil
	}
}

// resetTideCalState clears what the worker and the recorder keep between tests.
func resetTideCalState(t *testing.T) {
	t.Helper()
	clear := func() {
		tideCalStayMu.Lock()
		tideCalStayCur = nil
		tideCalStayMu.Unlock()
		tideCalPublished.Store(nil)
		soundedPublished.Store(nil)
	}
	clear()
	t.Cleanup(clear)
}

// fitAndPublish fits the stay against the Keppel chart and publishes it, as the
// worker does.
func fitAndPublish(t *testing.T, watch anchorWatchData, store *passageStore) *tideCalFitEntry {
	t.Helper()
	chart, _ := keppelChart(t)()
	e, err := tideCalFitNow(watch, store, chart)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func keppelTide() tideTodayResponse {
	return tideTodayResponse{LowTideTime: keppelLow.Format(time.RFC3339), HighTideTime: keppelHigh.Format(time.RFC3339), StationName: "Middle Island"}
}

// A restart loses everything in memory; the fit is rebuilt from the stored
// minutes alone.
func TestCalibrationIsRebuiltFromTheStoredMinutesAfterARestart(t *testing.T) {
	curve := loadKeppelCurve(t)
	store := newTestPassageStore(t)
	now := keppelDrop.Add(3 * time.Hour)
	for _, r := range readingsUntil(keppelMinutes(t, curve), now) {
		if err := store.appendTideCalReading(keppelDrop, r); err != nil {
			t.Fatal(err)
		}
	}
	globalTideCalRecorder.reset()
	resetTideCalState(t)

	// Nothing is published yet: a request says the calibration is being worked
	// out, and does not fit.
	if res := publishedCalibrator(keppelWatch())(keppelTide(), keppelMarginM, now); res.Err == nil || res.Err.Code != tideCalWorking {
		t.Fatalf("before the first fit: %+v", res)
	}
	fitAndPublish(t, keppelWatch(), store)
	res := publishedCalibrator(keppelWatch())(keppelTide(), keppelMarginM, now)
	if res == nil || res.Err != nil || res.Est == nil {
		t.Fatalf("%+v", res)
	}
	if res.HoursOfReadings < 2.9 || res.HoursOfReadings > 3.0 {
		t.Errorf("hours = %.2f", res.HoursOfReadings)
	}
	if res.StationName != "Middle Island" || res.StationDistanceM == nil || *res.StationDistanceM < 10000 || *res.StationDistanceM > 20000 {
		t.Errorf("station %q at %v", res.StationName, res.StationDistanceM)
	}
	if math.Abs(res.Est.LowHere.ValueM-2.36) > 0.15 {
		t.Errorf("low here = %.2f", res.Est.LowHere.ValueM)
	}

	// Through the whole answer: at three hours the page is on the calibrated
	// figure, the station depth it replaces is close, and the tide-match note does
	// not fire.
	got := applyStation(2.22, res, keppelMarginM)
	if !got.Calibration.Ready || got.Calibration.StationMatchNote != "" {
		t.Errorf("ready %v note %q", got.Calibration.Ready, got.Calibration.StationMatchNote)
	}

	// A watch with a different set_at has no minutes: a new drop starts fresh.
	fresh := keppelWatch()
	fresh.SetAt = keppelDrop.Add(48 * time.Hour)
	fitAndPublish(t, fresh, store)
	res = publishedCalibrator(fresh)(keppelTide(), keppelMarginM, now)
	if res == nil || res.Err == nil || res.Err.Code != tideCalTooFewReadings || res.HoursOfReadings != 0 {
		t.Errorf("a new watch: %+v", res)
	}
}

// A fit published for one watch or anchor position is not served for another: an
// Adjust, or a new Drop, waits for its own fit.
func TestAFitIsOnlyServedForTheWatchAndAnchorItWasMadeFor(t *testing.T) {
	resetTideCalState(t)
	curve := loadKeppelCurve(t)
	store := newTestPassageStore(t)
	for _, r := range readingsUntil(keppelMinutes(t, curve), keppelDrop.Add(2*time.Hour)) {
		if err := store.appendTideCalReading(keppelDrop, r); err != nil {
			t.Fatal(err)
		}
	}
	fitAndPublish(t, keppelWatch(), store)
	if !tideCalPublishedFor(keppelWatch()) {
		t.Fatal("the fit was not published")
	}
	moved := keppelWatch()
	moved.Lat += 0.00003
	other := keppelWatch()
	other.SetAt = other.SetAt.Add(time.Hour)
	if tideCalPublishedFor(moved) || tideCalPublishedFor(other) {
		t.Error("a fit was served for another anchor or watch")
	}
	if res := publishedCalibrator(moved)(keppelTide(), keppelMarginM, keppelDrop); res.Err == nil || res.Err.Code != tideCalWorking {
		t.Errorf("after an Adjust: %+v", res)
	}
	// The corrected anchor cuts the same readings into other cells.
	e := fitAndPublish(t, moved, store)
	if e.fit == nil || !tideCalPublishedFor(moved) || tideCalPublishedFor(keppelWatch()) {
		t.Errorf("%+v", e)
	}
}

// feedMinutes gives the recorder samples every ten seconds across n whole
// minutes from start, with the depth of the minute and a spike in the middle one.
func feedMinutes(t *testing.T, start time.Time, n int, depth float64, spike bool) {
	t.Helper()
	for m := 0; m < n; m++ {
		for s := 0; s < 60; s += 10 {
			d := depth
			if spike && s == 30 {
				d = 30
			}
			state := vesselStateData{Depth: d, DepthLastUpdateAge: 1, Latitude: -23.1847, Longitude: 150.9322}
			recordTideCalibrationSample(state, start.Add(time.Duration(m)*time.Minute+time.Duration(s)*time.Second))
		}
	}
}

func withKeppelChart(t *testing.T) {
	t.Helper()
	prev := tideCalChartSource
	tideCalChartSource = keppelChart(t)
	resetTideCalState(t)
	t.Cleanup(func() { tideCalChartSource = prev })
}

func storedFor(t *testing.T, store *passageStore) []tideCalReading {
	t.Helper()
	watch, active, _ := currentAnchorWatch()
	if !active {
		t.Fatal("no watch")
	}
	rs, err := store.tideCalReadings(watch.SetAt)
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

func TestReadingsFollowTheWatchThroughAdjustRaiseAndANewDrop(t *testing.T) {
	store := anchorTrailEnv(t)
	withKeppelChart(t)
	globalTideCalRecorder.reset()
	t0 := time.Date(2026, 11, 8, 1, 0, 0, 0, time.UTC)

	// No watch, no readings.
	feedMinutes(t, t0, 3, 4.0, false)
	var n int
	_ = store.db.QueryRow(`SELECT COUNT(*) FROM tide_calibration_readings`).Scan(&n)
	if n != 0 {
		t.Fatalf("%d readings stored with no watch", n)
	}

	dropAnchor(t, -23.1847, 150.9322)
	first, _, _ := currentAnchorWatch()
	feedMinutes(t, t0.Add(3*time.Minute), 5, 4.0, true)
	rs := storedFor(t, store)
	// Five minutes fed, the fifth still open.
	if len(rs) != 4 {
		t.Fatalf("stored %d minutes, want 4", len(rs))
	}
	for _, r := range rs {
		if r.DepthM != 4.0 {
			t.Errorf("the spike reached a stored minute: %+v", r)
		}
	}

	// Adjust: the same drop, the anchor mark corrected. The readings stay.
	if code, body := postAnchorWatch(t, map[string]any{"lat": -23.1850, "lon": 150.9325}); code != http.StatusOK {
		t.Fatalf("adjust: %d %v", code, body)
	}
	waitForPlaceNameResolveIdle(t)
	adjusted, _, _ := currentAnchorWatch()
	if !adjusted.SetAt.Equal(first.SetAt) || len(storedFor(t, store)) != 4 {
		t.Fatalf("adjust changed the set_at or lost readings: %d", len(storedFor(t, store)))
	}
	feedMinutes(t, t0.Add(8*time.Minute), 2, 4.0, false)
	if got := len(storedFor(t, store)); got != 6 {
		t.Errorf("after adjust and two more minutes: %d readings, want 6", got)
	}

	// Raise: nothing more is gathered, and what was stored is kept.
	if rec := raiseAnchor(t); rec.Code != http.StatusOK {
		t.Fatalf("raise: %d", rec.Code)
	}
	feedMinutes(t, t0.Add(10*time.Minute), 3, 4.0, false)
	kept, err := store.tideCalReadings(first.SetAt)
	if err != nil || len(kept) != 6 {
		t.Fatalf("after raise: %d readings, %v", len(kept), err)
	}

	// A new drop starts fresh, and the old stay's minutes are still there.
	time.Sleep(5 * time.Millisecond)
	dropAnchor(t, -23.1900, 150.9400)
	feedMinutes(t, t0.Add(20*time.Minute), 2, 4.0, false)
	second, _, _ := currentAnchorWatch()
	if second.SetAt.Equal(first.SetAt) {
		t.Fatal("a new drop kept the old set_at")
	}
	if got := len(storedFor(t, store)); got != 1 {
		t.Errorf("new drop: %d readings, want 1", got)
	}
	if old, _ := store.tideCalReadings(first.SetAt); len(old) != 6 {
		t.Errorf("old stay has %d readings, want 6", len(old))
	}
}

func TestAMinuteWithTooFewSamplesIsNotStored(t *testing.T) {
	var r tideCalRecorder
	setAt := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	t0 := time.Date(2026, 11, 8, 1, 0, 0, 0, time.UTC)
	r.observe(t0, setAt, 4, -23.1, 150.9)
	r.observe(t0.Add(10*time.Second), setAt, 4, -23.1, 150.9)
	if _, done := r.observe(t0.Add(time.Minute), setAt, 4, -23.1, 150.9); done {
		t.Error("two samples made a reading")
	}
}

// ── the answer on the wire ──────────────────────────────────────────────

func TestLowWaterEndpointCarriesTheCalibration(t *testing.T) {
	good := func() (vesselStateData, error) {
		return vesselStateData{Status: "anchored", Depth: 2.9, DepthLastUpdateAge: 2, DraftM: 1.2}, nil
	}
	tide := func(_ vesselStateData, now time.Time) (tideTodayResponse, error) {
		r := *lwTide(nil)
		r.Datetime = now.Add(-5 * time.Minute).Format(time.RFC3339)
		r.LowTideTime = now.Add(time.Hour).Format(time.RFC3339)
		return r, nil
	}
	margin := func() (float64, error) { return 0.5, nil }
	calibrate := lowWaterCalibrator(func(tideTodayResponse, float64, time.Time) *calibrationResult {
		return readyEstimate(2.4, 0.2, 2.1, 0.2)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/anchor-watch/low-water", nil)
	if err := serveAnchorLowWater(echo.New().NewContext(req, rec), good, func() bool { return true }, tide, margin, calibrate); err != nil {
		t.Fatal(err)
	}
	var body struct {
		DepthAtLowM float64  `json:"depth_at_low_m"`
		HalfWidthM  *float64 `json:"depth_at_low_half_width_m"`
		Calibration struct {
			Ready           bool    `json:"ready"`
			HoursOfReadings float64 `json:"hours_of_readings"`
			Shallowest      struct {
				ValueM     float64 `json:"value_m"`
				HalfWidthM float64 `json:"half_width_m"`
			} `json:"shallowest_sounded"`
			Differs bool `json:"shallowest_differs"`
		} `json:"calibration"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%q: %v", rec.Body.String(), err)
	}
	if body.DepthAtLowM != 2.4 || body.HalfWidthM == nil || *body.HalfWidthM != 0.2 ||
		!body.Calibration.Ready || body.Calibration.HoursOfReadings != 4 ||
		body.Calibration.Shallowest.ValueM != 2.1 || body.Calibration.Shallowest.HalfWidthM != 0.2 || !body.Calibration.Differs {
		t.Errorf("%s", rec.Body.String())
	}
	// The offset and the station's distance are Mate's, not the page's.
	for _, key := range []string{`"method"`, `"deepest_sounded"`, `"deepest_differs"`, `"station_match_note"`, `"offset_min"`, `"station_distance_m"`, `"station_name"`, `"low_at_boat_m"`} {
		if strings.Contains(rec.Body.String(), key) {
			t.Errorf("the page's answer carries %s: %s", key, rec.Body.String())
		}
	}
}

func TestMateGetsTheCalibrationWithItsMethodAndPlusOrMinus(t *testing.T) {
	d := lowWaterStatusDeps(t)
	d.displayUnits = func() (string, error) { return "imperial", nil }
	d.calibrateLowWater = func(anchorWatchData, bool) lowWaterCalibrator {
		return func(tide tideTodayResponse, _ float64, now time.Time) *calibrationResult {
			res := readyEstimate(2.4, 0.2, 2.1, 0.2)
			res.Est.HighHere = &tideCalPrediction{ValueM: 4.0, HalfWidthM: 0.2, At: now.Add(7 * time.Hour)}
			res.Est.HighDeepest = &tideCalPrediction{ValueM: 4.3, HalfWidthM: 0.25, At: now.Add(7 * time.Hour)}
			res.Est.DeepestApart = true
			return res
		}
	}
	lw := runStatus(t, d).Boat.Anchor.LowWater
	if lw == nil || lw.Method != "calibrated" || lw.Calibration == nil || !lw.Calibration.Ready {
		t.Fatalf("%+v", lw)
	}
	c := lw.Calibration
	// 2.4 m is 7.9 ft; the half width 0.2 m is 0.7 ft; both in the operator's units.
	if lw.DepthAtLow == nil || lw.DepthAtLow.DisplayUnit != "ft" || !near(lw.DepthAtLow.Display, 7.9, 0.01) ||
		lw.DepthAtLowHalfWidth == nil || !near(lw.DepthAtLowHalfWidth.Display, 0.7, 0.01) || lw.DepthAtLowHalfWidth.DisplayUnit != "ft" {
		t.Errorf("depth_at_low %+v +/- %+v", lw.DepthAtLow, lw.DepthAtLowHalfWidth)
	}
	if c.HoursOfReadings != 4 || c.TidePercentOfStation == nil || *c.TidePercentOfStation != 85 || c.TideMinutesLater == nil || *c.TideMinutesLater != -5 ||
		c.ShallowestSounded == nil || c.ShallowestSounded.HalfWidth == nil || c.HighAtBoat == nil || c.DeepestSounded == nil || c.StationName != "Middle Island" ||
		c.StationDistanceM == nil || !strings.Contains(c.HighTideTime, " PM") && !strings.Contains(c.HighTideTime, " AM") {
		t.Errorf("%+v", c)
	}
	if !strings.Contains(lw.Note, "method") || !strings.Contains(lw.Note, "half width") {
		t.Errorf("the note does not tell Mate to name the method and quote the half width: %q", lw.Note)
	}

	// Not ready: the reason in words, and no figures to quote.
	d.calibrateLowWater = func(anchorWatchData, bool) lowWaterCalibrator {
		return func(tideTodayResponse, float64, time.Time) *calibrationResult {
			return &calibrationResult{HoursOfReadings: 1.5, Err: tideCalErr(tideCalTooScattered, "")}
		}
	}
	lw = runStatus(t, d).Boat.Anchor.LowWater
	if lw == nil || lw.Method != "station" || lw.Calibration.Ready || lw.Calibration.NotReadyWhy != calibrationNotReadyWhy[tideCalTooScattered] ||
		lw.Calibration.ShallowestSounded != nil || lw.DepthAtLowHalfWidth != nil || lw.Calibration.HoursOfReadings != 1.5 {
		t.Errorf("%+v / %+v", lw, lw.Calibration)
	}
}

// ── thresholds that come from the data ──────────────────────────────────

func TestNoTideInTheReadingsIsAnImplausibleFit(t *testing.T) {
	curve := loadKeppelCurve(t)
	now := keppelDrop.Add(3 * time.Hour)
	rs := readingsUntil(keppelMinutes(t, curve), now)
	// The same positions and times with a sounder that never saw the tide: the
	// seabed of each cell, and a hundredth of scatter.
	for i := range rs {
		c := tideCalCellAt(keppelAnchorLat, keppelAnchorLon, rs[i].Lat, rs[i].Lon)
		rs[i].DepthM = 3 + 0.1*float64(((c.E%5)+5)%5) + 0.01*float64(i%3)
	}
	_, err := estimateTideCalibration(rs, keppelAnchorLat, keppelAnchorLon, curve, keppelLow, keppelHigh, 5, now)
	if code := notReadyCode(err); code != tideCalImplausibleFit {
		t.Errorf("got %v, want %q", err, tideCalImplausibleFit)
	}
}

// The noise level is the stay's own scatter about its slow tide. A sounder that
// reports in steps of 0.1 m scatters by about a twentieth of a metre, and a fit
// to its steps still finds the tide.
func TestAStepwiseSounderHasANoiseFloorAndStillFitsTheTide(t *testing.T) {
	curve := loadKeppelCurve(t)
	rs := readingsUntil(keppelMinutes(t, curve), keppelDrop.Add(6*time.Hour))
	for i := range rs {
		h, _ := curve.heightAt(rs[i].At)
		rs[i].DepthM = math.Round((2.5+0.85*h)*10) / 10
	}
	floor := noiseFloor(rs)
	if floor < 0.02 || floor > 0.06 {
		t.Errorf("noise floor %.3f for a 0.1 m step sounder, want about 0.04", floor)
	}
	// Fitted as it is, every step of the tide counts and k comes out right.
	fit, ferr := fitTideCalibration(rs, keppelAnchorLat, keppelAnchorLon, curve)
	if ferr != nil {
		t.Fatal(ferr)
	}
	if math.Abs(fit.K-0.85) > 0.05 {
		t.Errorf("k = %.3f from a stepwise sounder, want 0.85", fit.K)
	}
	if got := noiseFloor(flatMinutes(10, 4)); got <= 0 {
		t.Errorf("readings that never differ gave a floor of %v", got)
	}
}

func TestAMinuteNeedsHalfTheSamplesThePollIntervalGives(t *testing.T) {
	if want := int(time.Minute/trackPollInterval) / 2; tideCalMinSamples != want || want < 2 {
		t.Errorf("min samples = %d, want %d", tideCalMinSamples, want)
	}
}

// The offset is found to the minute, not to the coarse grid: depths that follow
// the tide seven minutes late are fitted at seven.
func TestTheOffsetIsFoundToTheMinute(t *testing.T) {
	curve := loadKeppelCurve(t)
	rs := readingsUntil(keppelMinutes(t, curve), keppelDrop.Add(5*time.Hour))
	for i := range rs {
		c := tideCalCellAt(keppelAnchorLat, keppelAnchorLon, rs[i].Lat, rs[i].Lon)
		h, _ := curve.heightAt(rs[i].At.Add(-7 * time.Minute))
		rs[i].DepthM = 2.5 + 0.13*float64(((c.E+2*c.N)%7+7)%7) + 0.85*h
	}
	fit, err := fitTideCalibration(rs, keppelAnchorLat, keppelAnchorLon, curve)
	if err != nil {
		t.Fatal(err)
	}
	// The curve is shifted back by seven minutes, so the fit finds an offset of
	// minus seven: the tide in these readings runs behind the station's.
	if fit.OffsetMin != -7 {
		t.Errorf("offset = %d min, want -7 (k = %.3f)", fit.OffsetMin, fit.K)
	}
}

// ── review fixes ────────────────────────────────────────────────────────

// The fit reads the station's curve at t + offset, so a positive offset is a
// tide that runs earlier than the station's. What Mate is told is the other way
// round: a local tide lagging the station by 20 minutes is 20 minutes later.
func TestMateIsToldWhetherTheTideHereRunsLaterOrEarlier(t *testing.T) {
	curve := loadKeppelCurve(t)
	now := keppelDrop.Add(5 * time.Hour)
	for _, lag := range []int{20, -15} {
		rs := readingsUntil(keppelMinutes(t, curve), now)
		for i := range rs {
			c := tideCalCellAt(keppelAnchorLat, keppelAnchorLon, rs[i].Lat, rs[i].Lon)
			h, _ := curve.heightAt(rs[i].At.Add(-time.Duration(lag) * time.Minute))
			rs[i].DepthM = 2.5 + 0.13*float64(((c.E+2*c.N)%7+7)%7) + 0.85*h
		}
		est, err := estimateTideCalibration(rs, keppelAnchorLat, keppelAnchorLon, curve, keppelLow, keppelHigh, 5, now)
		if err != nil {
			t.Fatal(err)
		}
		got := applyStation(2.2, &calibrationResult{Est: est, HoursOfReadings: 5}, 5)
		later := got.Calibration.TideMinutesLater
		if later == nil || *later != lag {
			t.Errorf("a local tide %d min later than the station's: told %v", lag, later)
		}
	}
}

func TestTheShallowestCellIsRankedByTheSideItsIntervalIsSureOf(t *testing.T) {
	curve := loadKeppelCurve(t)
	member := &tideCalMember{
		K: 0.85, TQ: 2, Sigma: 0.3, Inflation: 1, VarK: 1e-6,
		Cells: map[tideCalCell]*tideCalCellStat{
			{E: 0, N: 0}: {SeabedM: 2.0, HBar: 2, NEff: 50},  // well sounded
			{E: 1, N: 0}: {SeabedM: 1.8, HBar: 2, NEff: 1},   // one reading of jitter, lower but wide
			{E: 2, N: 0}: {SeabedM: 3.0, HBar: 2, NEff: 50},  // deeper
			{E: 3, N: 0}: {SeabedM: 3.5, HBar: 2, NEff: 0.2}, // deepest value, but barely sounded
		},
	}
	fit := &tideCalFit{tideCalMember: member, Members: []*tideCalMember{member}, Curve: curve}
	shallow, err := fit.extremeCell(true, keppelLow)
	if err != nil {
		t.Fatal(err)
	}
	well, _ := fit.predict(tideCalCell{E: 0, N: 0}, keppelLow)
	if shallow != well {
		t.Errorf("shallowest = %+v, want the well-sounded cell %+v", shallow, well)
	}
	deep, _ := fit.extremeCell(false, keppelHigh)
	deeper, _ := fit.predict(tideCalCell{E: 2, N: 0}, keppelHigh)
	if deep != deeper {
		t.Errorf("deepest = %+v, want %+v", deep, deeper)
	}
}

const bomMI = "bom/mi"

func TestExtremesSeenDuringTheStayAreKeptSoReadingsOutliveTheChart(t *testing.T) {
	curve := loadKeppelCurve(t)
	store := newTestPassageStore(t)
	rs := readingsUntil(keppelMinutes(t, curve), keppelDrop.Add(8*time.Hour))
	// Early in the stay the chart reaches back to the day before; later charts
	// start at the 04:45 low, and the stay keeps what the first one said.
	early := []tideExtremePoint(curve)
	late := []tideExtremePoint(curve[2:])
	if err := store.upsertTideCalExtremes(keppelDrop, bomMI, early); err != nil {
		t.Fatal(err)
	}
	// The later chart revises the 10:27 high: a minute later and 0.3 m lower. It
	// replaces the old one, whichever of time and height moved.
	revised := append([]tideExtremePoint(nil), late...)
	revised[1].Time = revised[1].Time.Add(time.Minute)
	revised[1].HeightM -= 0.3
	if err := store.upsertTideCalExtremes(keppelDrop, bomMI, revised); err != nil {
		t.Fatal(err)
	}
	got, err := store.tideCalExtremes(keppelDrop, bomMI)
	if err != nil || len(got) != len(curve) {
		t.Fatalf("%d extremes kept, want %d: %v", len(got), len(curve), err)
	}
	for _, e := range got {
		if e.Time.Equal(curve[3].Time) || (e.Time.Equal(revised[1].Time) && e.HeightM != revised[1].HeightM) {
			t.Errorf("the old high survived the revision: %+v", e)
		}
	}
	// A revised height at the same time is replaced too.
	again := append([]tideExtremePoint(nil), revised...)
	again[1].HeightM += 0.1
	if err := store.upsertTideCalExtremes(keppelDrop, bomMI, again); err != nil {
		t.Fatal(err)
	}
	if got, _ = store.tideCalExtremes(keppelDrop, bomMI); len(got) != len(curve) {
		t.Errorf("%d extremes after a height-only revision, want %d", len(got), len(curve))
	}

	full, ferr := fitTideCalibration(rs, keppelAnchorLat, keppelAnchorLon, newTideCurve(got))
	if ferr != nil {
		t.Fatal(ferr)
	}
	span := rs[len(rs)-1].At.Sub(rs[0].At).Hours()
	if full.Hours() < span-1 {
		t.Errorf("hours = %.2f of a %.2f h stay with every extreme kept", full.Hours(), span)
	}

	// With only the late chart, the readings the curve cannot reach are not used,
	// and the hours say so rather than reporting the whole stay.
	onlyLate, ferr := fitTideCalibration(rs, keppelAnchorLat, keppelAnchorLon, newTideCurve(late))
	if ferr != nil {
		t.Fatal(ferr)
	}
	if onlyLate.Hours() > span-2 {
		t.Errorf("hours = %.2f of a %.2f h stay with the early extremes gone", onlyLate.Hours(), span)
	}
}

// A change of source mid-stay does not splice the new source's predictions into
// the old one's: the fit takes the current source's extremes alone.
func TestAChangeOfSourceDoesNotSpliceTheirPredictions(t *testing.T) {
	curve := loadKeppelCurve(t)
	store := newTestPassageStore(t)
	other := append([]tideExtremePoint(nil), curve[2:]...)
	for i := range other {
		other[i].HeightM += 5 // a model on another datum
	}
	if err := store.upsertTideCalExtremes(keppelDrop, "bom/mi", []tideExtremePoint(curve)); err != nil {
		t.Fatal(err)
	}
	if err := store.upsertTideCalExtremes(keppelDrop, "open-meteo/-23.20,150.95", other); err != nil {
		t.Fatal(err)
	}
	a, _ := store.tideCalExtremes(keppelDrop, "bom/mi")
	b, _ := store.tideCalExtremes(keppelDrop, "open-meteo/-23.20,150.95")
	if len(a) != len(curve) || len(b) != len(other) {
		t.Fatalf("source A has %d extremes, B has %d", len(a), len(b))
	}
	for _, e := range a {
		if e.HeightM > 4.6 {
			t.Errorf("source B's height leaked into A: %+v", e)
		}
	}

	// Fitted on B alone, the readings before B's coverage are not used and the
	// hours reflect that.
	rs := readingsUntil(keppelMinutes(t, curve), keppelDrop.Add(8*time.Hour))
	onB, ferr := fitTideCalibration(rs, keppelAnchorLat, keppelAnchorLon, newTideCurve(b))
	if ferr != nil {
		t.Fatal(ferr)
	}
	if span := rs[len(rs)-1].At.Sub(rs[0].At).Hours(); onB.Hours() > span-2 {
		t.Errorf("hours = %.2f of a %.2f h stay on a source that starts later", onB.Hours(), span)
	}
}

func TestChartsAreToldApartByContent(t *testing.T) {
	curve := []tideExtremePoint(loadKeppelCurve(t))
	same := chartHash("bom/mi", curve)
	if chartHash("bom/mi", curve) != same {
		t.Error("the same chart hashed two ways")
	}
	revised := append([]tideExtremePoint(nil), curve...)
	revised[3].HeightM += 0.01
	later := append([]tideExtremePoint(nil), curve...)
	later[3].Time = later[3].Time.Add(time.Minute)
	if chartHash("bom/mi", revised) == same || chartHash("bom/mi", later) == same || chartHash("open-meteo/1,2", curve) == same {
		t.Error("a revised height, a moved time or another source hashed as the same chart")
	}
}

// A station that is a position, as a position-based provider names it, is a
// source like any other, and has no station to name or measure to.
func TestAPositionIsASourceAndHasNoStation(t *testing.T) {
	resetTideCalState(t)
	store := newTestPassageStore(t)
	curve := loadKeppelCurve(t)
	for _, r := range readingsUntil(keppelMinutes(t, curve), keppelDrop.Add(3*time.Hour)) {
		if err := store.appendTideCalReading(keppelDrop, r); err != nil {
			t.Fatal(err)
		}
	}
	chart := tideCalChart{
		tideChartResult: tideChartResult{Station: tideStation{StationID: "-23.20,150.95", Name: "Boat's position"}, Extremes: []tideExtremePoint(curve)},
		Source:          "open-meteo/-23.20,150.95",
		PositionBased:   true,
	}
	e, err := tideCalFitNow(keppelWatch(), store, chart)
	if err != nil || e.fit == nil {
		t.Fatalf("%+v, %v", e, err)
	}
	if !e.stationIsPoint || e.stationName != "" || e.stationDistanceM != nil {
		t.Errorf("a position was given a station: %+v", e)
	}
	res := publishedCalibrator(keppelWatch())(keppelTide(), keppelMarginM, keppelDrop.Add(3*time.Hour))
	if res.Err != nil || !res.StationIsPoint {
		t.Fatalf("%+v", res)
	}
	want := "The tide here doesn't follow the tide model: it runs 85% of its range."
	if got := stationMatchNote("Boat's position", floatPtr(1234), true, 0.85); got != want {
		t.Errorf("note = %q, want %q", got, want)
	}
}

// Heights from mean sea level run below zero. Nothing in the calibration takes a
// negative height for missing.
func TestNegativeTideHeightsAreOrdinaryHeights(t *testing.T) {
	curve := loadKeppelCurve(t)
	shifted := make([]tideExtremePoint, len(curve))
	for i, e := range curve {
		shifted[i] = e
		shifted[i].HeightM = e.HeightM - 3
	}
	sc := newTideCurve(shifted)
	rs := readingsUntil(keppelMinutes(t, curve), keppelDrop.Add(5*time.Hour))
	for i := range rs {
		c := tideCalCellAt(keppelAnchorLat, keppelAnchorLon, rs[i].Lat, rs[i].Lon)
		h, _ := sc.heightAt(rs[i].At)
		rs[i].DepthM = 6 + 0.13*float64(((c.E+2*c.N)%7+7)%7) + 0.85*h
	}
	now := keppelDrop.Add(5 * time.Hour)
	est, err := estimateTideCalibration(rs, keppelAnchorLat, keppelAnchorLon, sc, keppelLow, keppelHigh, 5, now)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(est.Fit.K-0.85) > 0.02 {
		t.Errorf("k = %.3f on heights below zero, want 0.85", est.Fit.K)
	}
}

func TestAStationListThatComesBackEmptyIsTriedAgainWhenTheChartChanges(t *testing.T) {
	resetTideCalState(t)
	store := newTestPassageStore(t)
	curve := loadKeppelCurve(t)
	searches := 0
	chart := tideCalChart{
		tideChartResult: tideChartResult{Station: tideStation{StationID: "no-coords", Name: "Nowhere"}, Extremes: []tideExtremePoint(curve)},
		Source:          "bom/no-coords",
		Search:          func() []tideStation { searches++; return nil },
	}
	for i := 0; i < 3; i++ {
		e, err := tideCalFitNow(keppelWatch(), store, chart)
		if err != nil || e.stationDistanceM != nil {
			t.Fatalf("%+v, %v", e, err)
		}
	}
	if searches != 1 {
		t.Errorf("searched %d times for an unchanged chart, want 1", searches)
	}
	chart.Extremes = append([]tideExtremePoint(nil), chart.Extremes...)
	chart.Extremes[3].HeightM += 0.05
	chart.Search = func() []tideStation {
		searches++
		return []tideStation{{StationID: "no-coords", Lat: -23.08, Lon: 150.9}}
	}
	e, _ := tideCalFitNow(keppelWatch(), store, chart)
	if searches != 2 || e.stationDistanceM == nil || *e.stationDistanceM < 10000 {
		t.Errorf("after the chart changed: %d searches, distance %v", searches, e.stationDistanceM)
	}
}

// The low is predicted at the time it comes here, the station's time less the
// offset, and a high that can't be predicted leaves the low standing.
func TestTheLowIsAtTheLocalTimeAndAMissingHighDoesNotHoldItBack(t *testing.T) {
	curve := loadKeppelCurve(t)
	now := keppelDrop.Add(5 * time.Hour)
	rs := readingsUntil(keppelMinutes(t, curve), now)
	for i := range rs {
		c := tideCalCellAt(keppelAnchorLat, keppelAnchorLon, rs[i].Lat, rs[i].Lon)
		h, _ := curve.heightAt(rs[i].At.Add(-20 * time.Minute))
		rs[i].DepthM = 2.5 + 0.13*float64(((c.E+2*c.N)%7+7)%7) + 0.85*h
	}
	farHigh := curve[len(curve)-1].Time.Add(6 * time.Hour)
	est, err := estimateTideCalibration(rs, keppelAnchorLat, keppelAnchorLon, curve, keppelLow, farHigh, 5, now)
	if err != nil {
		t.Fatalf("a high the curve doesn't reach held the low back: %v", err)
	}
	if d := est.LowHere.At.Sub(keppelLow.Add(20 * time.Minute)); d < -time.Minute || d > time.Minute {
		t.Errorf("local low at %s, want the station's 04:45 + 20 min", est.LowHere.At)
	}
	if est.HighHere != nil || est.HighUnavailable == nil || est.HighUnavailable.Code != tideCalNoTideCurve {
		t.Errorf("high %+v unavailable %+v", est.HighHere, est.HighUnavailable)
	}
	got := applyStation(2.2, &calibrationResult{Est: est, HoursOfReadings: 5}, 5)
	if !got.Calibration.Ready || got.Calibration.HighNotAvailable != tideCalNoTideCurve || got.Calibration.HighAtBoat != nil {
		t.Errorf("%+v", got.Calibration)
	}
	if want := est.LowHere.At.Format(time.RFC3339); got.LowTideTime != want {
		t.Errorf("low_tide_time = %q, want the local %q", got.LowTideTime, want)
	}
	none, err := estimateTideCalibration(rs, keppelAnchorLat, keppelAnchorLon, curve, keppelLow, time.Time{}, 5, now)
	if err != nil || none.HighUnavailable == nil || none.HighUnavailable.Code != tideCalNoUpcomingHigh {
		t.Errorf("no upcoming high: %+v, %v", none, err)
	}
}

// The worker fits off the tick and publishes; the recorder only stores and wakes
// it.
func TestTheWorkerFitsOffTheTickAndPublishes(t *testing.T) {
	store := anchorTrailEnv(t)
	withKeppelChart(t)
	globalTideCalRecorder.reset()
	t0 := time.Date(2026, 11, 8, 1, 0, 0, 0, time.UTC)
	dropAnchor(t, -23.1847, 150.9322)
	watch, _, _ := currentAnchorWatch()
	if tideCalPublishedFor(watch) {
		t.Fatal("a fit was published before any was made")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := startTideCalibrationWorker(ctx)
	t.Cleanup(func() { cancel(); <-done })

	feedMinutes(t, t0, 4, 4.0, false)
	deadline := time.Now().Add(3 * time.Second)
	for !tideCalPublishedFor(watch) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !tideCalPublishedFor(watch) {
		t.Fatal("the worker did not publish a fit")
	}
	// Its stored extremes are the chart's, under the chart's source.
	if ex, err := store.tideCalExtremes(watch.SetAt, bomMI); err != nil || len(ex) != 8 {
		t.Errorf("%d extremes kept, %v", len(ex), err)
	}
}

func TestNoClearanceSetIsItsOwnReasonNotAFalseWaitForReadings(t *testing.T) {
	curve := loadKeppelCurve(t)
	now := keppelDrop.Add(3 * time.Hour)
	rs := readingsUntil(keppelMinutes(t, curve), now)
	_, err := estimateTideCalibration(rs, keppelAnchorLat, keppelAnchorLon, curve, keppelLow, keppelHigh, 0, now)
	if code := notReadyCode(err); code != tideCalNoClearanceSet {
		t.Errorf("a zero margin: %v", err)
	}
	if want := "set a low-water clearance in Anchor Watch settings to judge the calibration"; calibrationNotReadyWhy[tideCalNoClearanceSet] != want {
		t.Errorf("why = %q", calibrationNotReadyWhy[tideCalNoClearanceSet])
	}
}

// ── third review ────────────────────────────────────────────────────────

// A tide provider that is down is asked at most once for each minute stored,
// however many ticks come between: the worker is woken by a stored minute or a
// changed watch, not by the tick.
func TestADownProviderIsAskedAtMostOncePerStoredMinute(t *testing.T) {
	anchorTrailEnv(t)
	resetTideCalState(t)
	globalTideCalRecorder.reset()
	var calls atomic.Int32
	prev := tideCalChartSource
	tideCalChartSource = func() (tideCalChart, error) {
		calls.Add(1)
		return tideCalChart{}, errors.New("tide service down")
	}
	t.Cleanup(func() { tideCalChartSource = prev })
	dropAnchor(t, -23.1847, 150.9322)
	t0 := time.Date(2026, 11, 8, 1, 0, 0, 0, time.UTC)

	ctx, cancel := context.WithCancel(context.Background())
	done := startTideCalibrationWorker(ctx)
	// 6 minutes of ticks, 6 to the minute: 36 ticks, 5 stored minutes.
	feedMinutes(t, t0, 6, 4.0, false)
	time.Sleep(200 * time.Millisecond) // let the worker drain its last wake
	cancel()
	<-done
	// The worker's own start and the new watch each ask once, and each stored
	// minute once more; the 36 ticks ask for nothing. Wakes coalesce while the
	// worker is busy, so fewer calls is fine: only too many is a failure.
	if got, want := calls.Load(), int32(5+2); got > want {
		t.Errorf("the provider was asked %d times over 36 ticks and 5 stored minutes, want at most %d", got, want)
	}
}

// Nothing calls a plugin with the stay's lock held.
func TestNoPluginIsCalledWithTheStayLockHeld(t *testing.T) {
	resetTideCalState(t)
	store := newTestPassageStore(t)
	curve := loadKeppelCurve(t)
	held := false
	chart := tideCalChart{
		tideChartResult: tideChartResult{Station: tideStation{StationID: "x", Name: "X"}, Extremes: []tideExtremePoint(curve)},
		Source:          "bom/x",
		Search: func() []tideStation {
			if !tideCalStayMu.TryLock() {
				held = true
			} else {
				tideCalStayMu.Unlock()
			}
			return nil
		},
	}
	if _, err := tideCalFitNow(keppelWatch(), store, chart); err != nil {
		t.Fatal(err)
	}
	if held {
		t.Error("the station list was searched with the stay's lock held")
	}
}

func fitWithTideLagging(t *testing.T, lagMin int, until time.Time) *tideCalFit {
	t.Helper()
	curve := loadKeppelCurve(t)
	rs := readingsUntil(keppelMinutes(t, curve), until)
	for i := range rs {
		c := tideCalCellAt(keppelAnchorLat, keppelAnchorLon, rs[i].Lat, rs[i].Lon)
		h, _ := curve.heightAt(rs[i].At.Add(-time.Duration(lagMin) * time.Minute))
		rs[i].DepthM = 2.5 + 0.13*float64(((c.E+2*c.N)%7+7)%7) + 0.85*h
	}
	fit, err := fitTideCalibration(rs, keppelAnchorLat, keppelAnchorLon, curve)
	if err != nil {
		t.Fatal(err)
	}
	return fit
}

// The next low is the first one that comes HERE after now, whichever station low
// that is. A local tide 20 minutes early, with the station low 10 minutes away,
// has had its low 10 minutes ago: the next is the evening one. A local tide 20
// minutes late, just after the station low, has its low still to come.
func TestTheNextLowIsTheFirstLocalOneAheadOfNow(t *testing.T) {
	early := keppelLow.Add(-10 * time.Minute)
	f := fitWithTideLagging(t, -20, early) // depth follows the station 20 min early: offset +20
	if f.OffsetMin < 19 || f.OffsetMin > 21 {
		t.Fatalf("offset %d, want +20", f.OffsetMin)
	}
	low, high := f.nextLocal(early)
	if want := time.Date(2026, 11, 8, 16, 35, 0, 0, time.UTC); !low.Equal(want) {
		t.Errorf("early tide: next low is the station's %s, want %s", low, want)
	}
	if !high.Equal(keppelHigh) {
		t.Errorf("early tide: next high %s, want %s", high, keppelHigh)
	}
	est, err := f.estimate(low, high, 5, early)
	if err != nil || !est.LowHere.At.After(early) {
		t.Errorf("a time that has passed was reported: %+v, %v", est, err)
	}

	late := keppelLow.Add(5 * time.Minute)
	g := fitWithTideLagging(t, 20, late)
	if g.OffsetMin > -19 || g.OffsetMin < -21 {
		t.Fatalf("offset %d, want -20", g.OffsetMin)
	}
	low, _ = g.nextLocal(late)
	if !low.Equal(keppelLow) {
		t.Errorf("late tide: next low is %s, want the station's 04:45 that comes 20 min later here", low)
	}
	est, err = g.estimate(low, keppelHigh, 5, late)
	if err != nil || est.LowHere.At.Sub(late) < 14*time.Minute || est.LowHere.At.Sub(late) > 16*time.Minute {
		t.Errorf("local low %v, want about 15 min ahead of now: %v", est, err)
	}
}

// A minute already read back from the store is not added again by the recorder.
func TestAMinuteReadBackFromTheStoreIsNotAppendedTwice(t *testing.T) {
	resetTideCalState(t)
	store := newTestPassageStore(t)
	curve := loadKeppelCurve(t)
	rs := readingsUntil(keppelMinutes(t, curve), keppelDrop.Add(time.Hour))
	for _, r := range rs {
		if err := store.appendTideCalReading(keppelDrop, r); err != nil {
			t.Fatal(err)
		}
	}
	chart, _ := keppelChart(t)()
	if _, err := tideCalFitNow(keppelWatch(), store, chart); err != nil { // loads the stay, last minute included
		t.Fatal(err)
	}
	tideCalStayAppend(store, keppelDrop, rs[len(rs)-1])
	tideCalStayAppend(store, keppelDrop, rs[len(rs)-5])
	next := rs[len(rs)-1]
	next.At = next.At.Add(time.Minute)
	tideCalStayAppend(store, keppelDrop, next)
	tideCalStayMu.Lock()
	n := len(tideCalStayCur.readings)
	tideCalStayMu.Unlock()
	if n != len(rs)+1 {
		t.Errorf("the stay holds %d minutes, want %d", n, len(rs)+1)
	}
}

// The too-shallow line shows the figure the verdict rests on.
func TestTheVerdictCarriesTheClearanceOfTheFigureBehindIt(t *testing.T) {
	// Boat's cell: its own clearance and interval.
	boat := applyStation(2.0, readyEstimate(2.0, 0.6, 2.0, 0.1), 0.5)
	if boat.CautiousSource != lowWaterCautiousBoat {
		t.Fatalf("%+v", boat)
	}
	lwFloat(t, "boat clearance", boat.VerdictClearanceM, 0.8, 1e-9)
	lwFloat(t, "boat half width", boat.VerdictHalfWidthM, 0.6, 1e-9)
	// Station: its clearance, no interval.
	st := applyStation(1.6, readyEstimate(2.6, 0.1, 2.5, 0.1), 0.5)
	if st.CautiousSource != lowWaterCautiousStation || st.VerdictHalfWidthM != nil {
		t.Fatalf("%+v", st)
	}
	lwFloat(t, "station clearance", st.VerdictClearanceM, 0.4, 1e-9)
	// Shallowest spot, as judged with its allowance, and its own interval.
	sh := applyStation(2.1, readyEstimate(2.6, 0.1, 1.5, 0.3), 0.5)
	if sh.CautiousSource != lowWaterCautiousShallowest {
		t.Fatalf("%+v", sh)
	}
	lwFloat(t, "shallowest clearance", sh.VerdictClearanceM, 1.5+tideCalCautionM-1.2, 1e-9)
	lwFloat(t, "shallowest half width", sh.VerdictHalfWidthM, 0.3, 1e-9)
}

func TestNoReadingIsStoredFromAnAlertedOrStaleFix(t *testing.T) {
	store := anchorTrailEnv(t)
	withKeppelChart(t)
	dropAnchor(t, -23.1847, 150.9322)
	t0 := time.Date(2026, 11, 8, 1, 0, 0, 0, time.UTC)
	stale := vesselStatusFreshAge.Seconds() + 5
	cases := []struct {
		name string
		pos  float64
		gnss bool
		want int
	}{
		{"good fix", 1, false, 2},
		{"gnss critical alert", 1, true, 0},
		{"stale position", stale, false, 0},
		{"unknown position", -1, false, 0},
	}
	for i, c := range cases {
		globalTideCalRecorder.reset()
		before := len(storedFor(t, store))
		state := vesselStateData{GNSSCriticalAlert: c.gnss, PositionLastUpdateAge: c.pos, Depth: 4.0, DepthLastUpdateAge: 1, Latitude: -23.1847, Longitude: 150.9322}
		start := t0.Add(time.Duration(i*10) * time.Minute)
		for m := 0; m < 3; m++ {
			for s := 0; s < 60; s += 10 {
				recordTideCalibrationSample(state, start.Add(time.Duration(m)*time.Minute+time.Duration(s)*time.Second))
			}
		}
		if got := len(storedFor(t, store)) - before; got != c.want {
			t.Errorf("%s: %d readings stored, want %d", c.name, got, c.want)
		}
	}
}

// The offset can straddle now: the station's low is 10 minutes away, and the
// local low, 20 minutes earlier, has already passed, so the local low that comes
// is the next one, about 12 hours on. The station figure the verdict weighs it
// against is the fall to that same station low, not to the imminent one.
func TestStationFigureIsForTheSameLowAsTheLocalOne(t *testing.T) {
	curve := loadKeppelCurve(t)
	store := newTestPassageStore(t)
	now := keppelLow.Add(-10 * time.Minute)
	for _, r := range readingsUntil(keppelMinutes(t, curve), now) {
		if err := store.appendTideCalReading(keppelDrop, r); err != nil {
			t.Fatal(err)
		}
	}
	globalTideCalRecorder.reset()
	resetTideCalState(t)
	e := fitAndPublish(t, keppelWatch(), store)
	if e.fit == nil {
		t.Fatalf("no fit: %+v", e.err)
	}
	e.fit.OffsetMin = 20

	res := publishedCalibrator(keppelWatch())(keppelTide(), keppelMarginM, now)
	if res == nil || res.Err != nil || res.Est == nil {
		t.Fatalf("%+v", res)
	}
	stationLow := res.Est.LowHere.At.Add(20 * time.Minute)
	if !stationLow.After(keppelLow.Add(time.Hour)) {
		t.Fatalf("the local low should be the one after the imminent station low, got station time %s", stationLow)
	}
	nowH, okNow := curve.heightAt(now)
	lowH, okLow := curve.heightAt(stationLow)
	if !okNow || !okLow {
		t.Fatalf("the curve does not reach now or the low at %s", stationLow)
	}
	if want := math.Max(0, nowH-lowH); math.Abs(res.StationFallM-want) > 1e-9 {
		t.Errorf("station fall = %.3f m, want %.3f m to the station low at %s", res.StationFallM, want, stationLow)
	}
}

// A minute stored while the stay is being read from the store is neither lost
// nor held up by the read, and a minute the read already saw is not doubled.
func TestAMinuteStoredDuringTheStayLoadIsKeptAndNotBlocked(t *testing.T) {
	resetTideCalState(t)
	store := newTestPassageStore(t)
	curve := loadKeppelCurve(t)
	rs := readingsUntil(keppelMinutes(t, curve), keppelDrop.Add(time.Hour))
	for _, r := range rs {
		if err := store.appendTideCalReading(keppelDrop, r); err != nil {
			t.Fatal(err)
		}
	}
	late := rs[len(rs)-1]
	late.At = late.At.Add(time.Minute)
	later := late
	later.At = later.At.Add(time.Minute)
	tideCalAfterRead = func() {
		// The recorder stores a minute after the read, and appends it.
		if err := store.appendTideCalReading(keppelDrop, late); err != nil {
			t.Error(err)
		}
		done := make(chan struct{})
		go func() {
			tideCalStayAppend(store, keppelDrop, late)
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("the recorder's append waited on the stay load")
		}
	}
	t.Cleanup(func() { tideCalAfterRead = nil })
	if _, err := tideCalStayLoaded(keppelWatch(), store); err != nil {
		t.Fatal(err)
	}
	tideCalAfterRead = nil
	tideCalStayAppend(store, keppelDrop, later)
	tideCalStayMu.Lock()
	n := len(tideCalStayCur.readings)
	tideCalStayMu.Unlock()
	if n != len(rs)+2 {
		t.Errorf("the stay holds %d minutes, want %d", n, len(rs)+2)
	}
}

// ── the page's calibration line ─────────────────────────────────────────

func TestReadyProjectionScalesTheShrinkingPartAgainstWhatRoomIsLeft(t *testing.T) {
	first := time.Date(2026, 11, 8, 3, 0, 0, 0, time.UTC)
	last := first.Add(3 * time.Hour) // the end of the fit's data
	f := &tideCalFit{FirstAt: first, LastAt: last}
	now := last.Add(time.Minute)
	// 3 h in, half width twice the margin, nothing that does not shrink: four
	// times the readings, 9 h more, counted from the fit's last reading.
	if at := f.projectReady(0.8, 0, 0.4, now); !at.Equal(last.Add(9 * time.Hour)) {
		t.Errorf("ready at %v", at)
	}
	// 0.2 m of the 0.8 m is ensemble disagreement and never shrinks: 0.6 m has to
	// come down to 0.2 m, nine times the readings, 24 h more from the last reading,
	// which is just under a day from now.
	if at := f.projectReady(0.8, 0.2, 0.4, now); !at.Equal(last.Add(24 * time.Hour)) {
		t.Errorf("with a floor: ready at %v", at)
	}
	// A floor at or over the margin means more readings never get there.
	if at := f.projectReady(0.8, 0.4, 0.4, now); !at.IsZero() {
		t.Error("projected with a floor at the margin")
	}
	for name, c := range map[string][3]float64{
		"no half width": {0, 0, 0.4},
		"no margin":     {0.8, 0, 0},
		"infinite":      {math.Inf(1), 0, 0.4},
		"nan":           {math.NaN(), 0, 0.4},
		"nan floor":     {0.8, math.NaN(), 0.4},
	} {
		if at := f.projectReady(c[0], c[1], c[2], now); !at.IsZero() {
			t.Errorf("%s: projected", name)
		}
	}
	if at := (&tideCalFit{FirstAt: first, LastAt: first}).projectReady(0.8, 0, 0.4, now); !at.IsZero() {
		t.Error("projected with no hours")
	}
}

// halfWidthFor is the half width that puts the projection `more` hours past the
// fit's last reading, for a fit of `hours` hours, a margin of 0.4 and no floor.
func halfWidthFor(hours, more float64) float64 {
	return 0.4 * math.Sqrt(1+more/hours)
}

func TestReadyProjectionOnlyGivesATimeBetweenTenMinutesAndADayAhead(t *testing.T) {
	first := time.Date(2026, 11, 8, 3, 0, 0, 0, time.UTC)
	last := first.Add(3 * time.Hour)
	f := &tideCalFit{FirstAt: first, LastAt: last}

	if at := f.projectReady(halfWidthFor(3, 3), 0, 0.4, last); at.IsZero() || at.Sub(last).Round(time.Minute) != 3*time.Hour {
		t.Errorf("3 h ahead: %v", at)
	}
	if at := f.projectReady(halfWidthFor(3, 5.0/60), 0, 0.4, last); !at.IsZero() {
		t.Errorf("5 min ahead: %v", at)
	}
	if at := f.projectReady(halfWidthFor(3, 30), 0, 0.4, last); !at.IsZero() {
		t.Errorf("30 h ahead: %v", at)
	}
	// The window is measured from the request time, not from the last reading: 3 h
	// after the last reading is already behind a request made 2 h 55 min later.
	if at := f.projectReady(halfWidthFor(3, 3), 0, 0.4, last.Add(175*time.Minute)); !at.IsZero() {
		t.Errorf("under 10 min from now: %v", at)
	}
	// Half width equal to the margin: nothing left to narrow, but not ready.
	if at := f.projectReady(0.4, 0, 0.4, last); !at.IsZero() {
		t.Errorf("half width at the margin: %v", at)
	}
	// 10 h of readings with the room used up but for 0.001 m: about 2.5 million
	// hours, which must not be turned into a duration.
	big := &tideCalFit{FirstAt: first, LastAt: first.Add(10 * time.Hour)}
	if at := big.projectReady(1.0, 0.499, 0.5, big.LastAt); !at.IsZero() {
		t.Errorf("overflow input: %v", at)
	}
}

func TestPredictSeparatesTheDisagreementThatDoesNotShrink(t *testing.T) {
	cell := tideCalCell{}
	mk := func(off int, seabed float64) *tideCalMember {
		return &tideCalMember{K: 0.5, OffsetMin: off, Sigma: 0.1, Inflation: 1, TQ: 2, VarK: 0.0001,
			Cells: map[tideCalCell]*tideCalCellStat{cell: {SeabedM: seabed, HBar: 1, NEff: 50}}}
	}
	f := &tideCalFit{Members: []*tideCalMember{mk(0, 1.0), mk(5, 1.3)}, Curve: tideCurve{
		{Time: time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC), High: false, HeightM: 1},
		{Time: time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC), High: true, HeightM: 3},
	}}
	p, err := f.predict(cell, time.Date(2026, 11, 8, 6, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if p.StaticM < 0.29 || p.StaticM > 0.31 || p.HalfWidthM <= p.StaticM {
		t.Errorf("static %v of half width %v", p.StaticM, p.HalfWidthM)
	}
}

func TestStationHighAtTheBoatComesWithTheLow(t *testing.T) {
	now := time.Date(2026, 11, 8, 2, 0, 0, 0, time.UTC)
	cur, low, high := 4.0, 1.0, 10.0
	out := computeLowWaterClearance(lowWaterInput{
		MarginM: 0.5, Now: now, DepthM: floatPtr(3), DraftM: floatPtr(1),
		Tide: &tideTodayResponse{CurrentTideHeightFt: &cur, LowTideHeightFt: &low, LowTideTime: now.Add(4 * time.Hour).Format(time.RFC3339),
			HighTideHeightFt: &high, HighTideTime: now.Add(10 * time.Hour).Format(time.RFC3339)},
	})
	if out.DepthAtHighM == nil || math.Abs(*out.DepthAtHighM-(3+6/metersToFeet)) > 1e-9 || out.HighTideTime != now.Add(10*time.Hour).Format(time.RFC3339) {
		t.Errorf("%+v", out)
	}
	// A high that has passed gives nothing.
	out = computeLowWaterClearance(lowWaterInput{
		MarginM: 0.5, Now: now, DepthM: floatPtr(3), DraftM: floatPtr(1),
		Tide: &tideTodayResponse{CurrentTideHeightFt: &cur, LowTideHeightFt: &low, LowTideTime: now.Add(4 * time.Hour).Format(time.RFC3339),
			HighTideHeightFt: &high, HighTideTime: now.Add(-time.Hour).Format(time.RFC3339)},
	})
	if out.DepthAtHighM != nil || out.HighTideTime != "" {
		t.Errorf("a passed high: %+v", out)
	}
}

func TestPageGetsKAndTheTimingAndTheProjectedReadyTime(t *testing.T) {
	got := applyStation(2.0, readyEstimate(2.4, 0.2, 2.4, 0.2), 0.5)
	b, err := json.Marshal(got.Calibration)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["k"] != 0.85 || m["later_min"] != float64(-5) {
		t.Errorf("k / later_min: %s", b)
	}
	if _, ok := m["k_half_width"]; ok {
		t.Errorf("k half width reached the page: %s", b)
	}
	if _, ok := m["ready_at"]; ok {
		t.Errorf("a ready calibration has a ready_at: %s", b)
	}

	at := time.Date(2026, 11, 8, 8, 40, 0, 0, time.UTC)
	e := tideCalErr(tideCalTooFewReadings, "")
	e.ReadyAt = at
	nr := applyStation(2.0, &calibrationResult{HoursOfReadings: 3, Err: e}, 0.5)
	if nr.Calibration.ReadyAt != at.Format(time.RFC3339) {
		t.Errorf("ready_at = %q", nr.Calibration.ReadyAt)
	}
}

func TestOneHighAndTheDirectionComeFromTheServer(t *testing.T) {
	now := time.Date(2026, 11, 8, 2, 0, 0, 0, time.UTC)
	cur, low, high := 4.0, 1.0, 10.0
	tide := &tideTodayResponse{CurrentTideHeightFt: &cur, LowTideHeightFt: &low, LowTideTime: now.Add(4 * time.Hour).Format(time.RFC3339),
		HighTideHeightFt: &high, HighTideTime: now.Add(10 * time.Hour).Format(time.RFC3339), TideDirection: "Falling"}
	st := computeLowWaterClearance(lowWaterInput{MarginM: 0.5, Now: now, DepthM: floatPtr(3), DraftM: floatPtr(1), Tide: tide})
	if st.TideDirection != "Falling" {
		t.Errorf("station direction = %q", st.TideDirection)
	}

	// Calibrated: the local high (1 h from now) comes before the local low, so the
	// tide here is rising whatever the station says, and the verdict's one high is
	// the calibrated one, with its half width.
	res := readyEstimate(2.4, 0.2, 2.4, 0.2)
	res.Est.LowHere.At = now.Add(5 * time.Hour)
	res.Est.HighHere = &tideCalPrediction{ValueM: 5.1, HalfWidthM: 0.3, At: now.Add(time.Hour)}
	res.Est.HighDeepest = res.Est.HighHere
	got := applyCalibration(st, res, 0.5)
	if got.TideDirection != "Rising" || got.HighTideTime != now.Add(time.Hour).Format(time.RFC3339) ||
		got.DepthAtHighM == nil || *got.DepthAtHighM != 5.1 || got.DepthAtHighHalfWidthM == nil || *got.DepthAtHighHalfWidthM != 0.3 {
		t.Errorf("%+v", got)
	}
	b, _ := json.Marshal(got.Calibration)
	for _, key := range []string{`"high_tide_time"`, `"high_at_boat"`} {
		if strings.Contains(string(b), key) {
			t.Errorf("the page gets a second high, %s: %s", key, b)
		}
	}

	// With no calibrated high there is no high on the verdict, and nothing to
	// compare the low with, so the station's direction stands.
	res2 := readyEstimate(2.4, 0.2, 2.4, 0.2)
	res2.Est.HighUnavailable = tideCalErr(tideCalNoUpcomingHigh, "")
	res2.Est.LowHere.At = now.Add(5 * time.Hour)
	st.TideDirection = "Rising"
	got2 := applyCalibration(st, res2, 0.5)
	if got2.TideDirection != "Rising" || got2.HighTideTime != "" || got2.DepthAtHighM != nil {
		t.Errorf("%+v", got2)
	}
}

func TestStationHighNeverReadsShallowerThanNow(t *testing.T) {
	// Already above the next high's forecast height (the turn is just passing): the
	// depth at the high is the depth now, as the low's fall clamps at zero.
	now := time.Date(2026, 11, 8, 2, 0, 0, 0, time.UTC)
	cur, low, high := 9.0, 1.0, 8.5
	out := computeLowWaterClearance(lowWaterInput{
		MarginM: 0.5, Now: now, DepthM: floatPtr(3), DraftM: floatPtr(1),
		Tide: &tideTodayResponse{CurrentTideHeightFt: &cur, LowTideHeightFt: &low, LowTideTime: now.Add(4 * time.Hour).Format(time.RFC3339),
			HighTideHeightFt: &high, HighTideTime: now.Add(10 * time.Minute).Format(time.RFC3339)},
	})
	if out.DepthAtHighM == nil || *out.DepthAtHighM != 3 {
		t.Errorf("%+v", out.DepthAtHighM)
	}
}

func TestVerdictNamesTheTideStationItRestsOn(t *testing.T) {
	now := time.Date(2026, 11, 8, 2, 0, 0, 0, time.UTC)
	cur, low := 4.0, 1.0
	out := computeLowWaterClearance(lowWaterInput{
		MarginM: 0.5, Now: now, DepthM: floatPtr(3), DraftM: floatPtr(1),
		Tide: &tideTodayResponse{StationName: "Middle Island", CurrentTideHeightFt: &cur, LowTideHeightFt: &low, LowTideTime: now.Add(4 * time.Hour).Format(time.RFC3339)},
	})
	if out.TideStationName != "Middle Island" {
		t.Errorf("station = %q", out.TideStationName)
	}
}
