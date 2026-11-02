package main

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// Dawn projection for the Battery & Power tile (battery-power-tile-pfd-fixes
// plan, Phase 4). The operator's question at anchor is "will the bank hold
// until morning" - this file answers it two ways:
//
//   - history: the median overnight discharge rate from recent nights in
//     InfluxDB, applied to tonight.
//   - linear: the live rate extrapolated to sunrise, used whenever history
//     isn't available.
//
// Sun times are computed (suntimes.go), not fetched - the weather provider
// only ever has forecast-day strings, never a past night's times, and the
// history branch needs several past nights' windows.
//
// The linear basis is this project's first use of the AGENTS.md exception
// rule: a fallback that would otherwise be disallowed (masking "no overnight
// history" with a plausible-looking number), approved by the operator on
// 2026-09-07, gated behind DAWN_LINEAR_FALLBACK, logged at startup and on
// every recompute that lands on it.
//
// The history scan excludes any night shore power or charging ran (the shore
// charger and the inverter the operator picked in the roles), on top of the
// pre-existing gap and net-rise checks. This was added after a marina week
// of shore-power nights passed the net-rise check outright - a charger in
// float holds the state of charge flat, which reads as a quiet night at
// anchor rather than the charged night it actually was, and pulled the
// median toward zero. The scan now looks back up to overnightLookbackNights
// nights, most recent first, and stops once overnightTargetNights usable
// nights are found.
//
// Charging is read from the inverter's AC-in power, not from the generator's
// state number: on this boat the state number stays 0 while the generator
// runs and feeds the Multi, and the Multi's AC-in reads about 8000 W.
//
// The same scan feeds a second, narrower comparison for the morning report's
// overnight_draw finding: each night measured from 22:00 boat time to dawn
// (ruleNightWindow), so a heavy evening with the generator running does not
// make the baseline look lighter than the late night really is.

// nightWindow is the span of one night's discharge sample, offset from the
// raw sunset/sunrise so neither boundary's transient (the evening's last
// loads settling, the morning's first loads starting) pollutes the slope.
type nightWindow struct {
	Start time.Time
	End   time.Time
}

// recentNightWindows returns the last `nights` completed nights at the given
// position, most recent first. Each window starts 30 minutes after sunset
// and ends 15 minutes before the following sunrise. A night that has not
// finished yet (its end is not before now) is not "recent" - it is
// tonight-in-progress, and including it would score a partial discharge as
// if it were a full one.
//
// Day boundaries for the sunTimes lookups are the boat's own calendar days, in
// loc, the zone at the boat's position (ADR 0230).
func recentNightWindows(now time.Time, loc *time.Location, lat, lon float64, nights int) []nightWindow {
	if nights <= 0 {
		return nil
	}

	nowLocal := now.In(loc)

	windows := make([]nightWindow, 0, nights)
	// Scan more days than requested: sunTimes can decline a day (polar
	// day/night) and this must not strand the search short of `nights`
	// results just because one day in the middle had nothing to report.
	// The bound below still terminates the loop - it does not retry
	// forever chasing windows that will never complete.
	maxScan := nights*3 + 14
	for daysBack := 0; len(windows) < nights && daysBack < maxScan; daysBack++ {
		day := nowLocal.AddDate(0, 0, -daysBack)
		_, sunset, sunsetOK := sunTimes(day, lat, lon)
		if !sunsetOK {
			continue
		}
		nextDay := day.AddDate(0, 0, 1)
		sunrise, _, sunriseOK := sunTimes(nextDay, lat, lon)
		if !sunriseOK {
			continue
		}

		w := nightWindow{Start: sunset.Add(30 * time.Minute), End: sunrise.Add(-15 * time.Minute)}
		if !w.End.Before(now) {
			continue // tonight-in-progress, or a clock skewed into the future
		}
		windows = append(windows, w)
	}
	return windows
}

// nightSlopePercentPerHour reduces one night's SoC samples to a single
// percent-per-hour discharge rate.
//
// points ≤ 1 in magnitude are treated as the 0..1 ratio InfluxDB stores
// (electrical.batteries.0.capacity.stateOfCharge, per the plan's captured
// fixture) and scaled to percent; anything already above 1 is assumed to be
// percent already.
//
// A night needs at least 80% of its expected sample count (window length /
// every) to be trusted - a night with the middle of its data missing could
// have its first and last surviving points span a much shorter true
// interval than the window implies, corrupting the slope. A night whose
// slope comes out >= 0 is excluded outright: SoC does not rise overnight on
// its own, so a flat or rising reading means charging
// ran, and that night says nothing about the boat's baseline overnight
// draw.
//
// By the time a night reaches this function, computeOvernight has already
// excluded it for shore power or charging if either of those series showed
// one. The net-rise check here stays anyway as the backstop for a source
// those two paths didn't see - a charger reading, a shared circuit, or a
// misnamed measurement path - so a night that quietly charged never gets
// counted as a discharge just because computeOvernight's own two checks
// missed it.
func nightSlopePercentPerHour(points []telemetryPoint, w nightWindow, every time.Duration) (slope float64, usable bool, reason string) {
	inWindow := pointsIn(points, w)

	duration := w.End.Sub(w.Start)
	if duration <= 0 || every <= 0 {
		return 0, false, "insufficient samples"
	}

	expected := duration.Seconds() / every.Seconds()
	required := math.Ceil(expected * 0.8)
	if float64(len(inWindow)) < required {
		return 0, false, "insufficient samples"
	}

	maxAbs := 0.0
	for _, p := range inWindow {
		if v := math.Abs(p.Value); v > maxAbs {
			maxAbs = v
		}
	}
	scale := 1.0
	if maxAbs <= 1 {
		scale = 100
	}

	first := inWindow[0].Value * scale
	last := inWindow[len(inWindow)-1].Value * scale
	slope = (last - first) / duration.Hours()

	if slope >= 0 {
		return slope, false, "net rise"
	}
	return slope, true, ""
}

// medianNightRate requires at least two usable nights before it will report
// anything: one clean night could be an unusually quiet or unusually heavy
// one, and a "typical" rate needs more than a single sample to mean
// anything.
func medianNightRate(slopes []float64) (float64, bool) {
	if len(slopes) < 2 {
		return 0, false
	}
	sorted := append([]float64(nil), slopes...)
	sort.Float64s(sorted)
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2], true
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2, true
}

// overnightResult is the body of GET /api/electrical/overnight.
type overnightResult struct {
	SocPath                 string    `json:"soc_path"`
	Sunset                  time.Time `json:"sunset"`
	Sunrise                 time.Time `json:"sunrise"`
	Basis                   string    `json:"basis"` // "history" | "linear" | "none"
	NightRatePercentPerHour *float64  `json:"night_rate_percent_per_hour"`
	LookbackNights          int       `json:"lookback_nights"`
	NightsConsidered        int       `json:"nights_considered"`
	NightsUsed              int       `json:"nights_used"`
	// ShoreChecked and ChargingChecked say whether nights were screened for
	// shore power and for charging through the inverter (shore or generator).
	// ShoreChecked is false when the roles name no shore charger.
	// ChargingChecked is false when they name no inverter or the inverter has
	// no history, so a zero count beside either means "not checked", not
	// "none found".
	ShoreChecked           bool      `json:"shore_checked"`
	ChargingChecked        bool      `json:"charging_checked"`
	NightsExcludedShore    int       `json:"nights_excluded_shore"`
	NightsExcludedCharging int       `json:"nights_excluded_charging"`
	// NightsExcludedChargingUnchecked counts nights left out because the
	// inverter logged nothing inside them, though it did on other nights.
	NightsExcludedChargingUnchecked int `json:"nights_excluded_charging_unchecked"`
	NightsExcludedGap      int       `json:"nights_excluded_gap"`
	NightsExcludedRise     int       `json:"nights_excluded_rise"`
	OldestNightStart       time.Time `json:"oldest_night_start"`
	Reason                 *string   `json:"reason"`
	ComputedAt             time.Time `json:"computed_at"`

	// The overnight_draw rule's own comparison, which measures each night from
	// 22:00 boat time to dawn rather than over the whole night above.
	// RuleNights lists every night the scan looked at, most recent first, with
	// its rate or why it was left out; lastNight and priorRates read last night
	// and the usable nights before it from it. LastNightHeaviest is the
	// two-hour stretch holding most of last night's fall, set only when one
	// does. Loc is the boat's zone for the wording. None of them are on the
	// wire.
	RuleNights        []overnightRuleNight `json:"-"`
	LastNightHeaviest *nightWindow         `json:"-"`
	Loc               *time.Location       `json:"-"`
}

// lastNight is the most recent completed night as the rule rated it, nil
// unless that night was usable.
func (r overnightResult) lastNight() *overnightRuleNight {
	if len(r.RuleNights) == 0 || !r.RuleNights[0].Latest || r.RuleNights[0].RatePercentPerHour == nil {
		return nil
	}
	return &r.RuleNights[0]
}

// priorRates are the usable nights' rates before last night, most recent
// first. There are none while last night itself was not usable.
func (r overnightResult) priorRates() []float64 {
	if r.lastNight() == nil {
		return nil
	}
	var rates []float64
	for _, n := range r.RuleNights[1:] {
		if n.RatePercentPerHour != nil {
			rates = append(rates, *n.RatePercentPerHour)
		}
	}
	return rates
}

// Why a night was left out of the rule's comparison.
const (
	leftOutShore    = "shore power"
	leftOutCharging = "charging"
	leftOutGap      = "a gap in the data"
	leftOutRise     = "the charge rose"
	// leftOutChargingUnchecked is a night the inverter logged nothing for: with
	// no samples there is nothing to say it did not charge.
	leftOutChargingUnchecked = "its charging could not be checked"
)

// overnightRuleNight is one night as the overnight_draw rule saw it.
type overnightRuleNight struct {
	// Start and End are the 22:00-to-dawn window. Latest says this is the most
	// recent completed night.
	Start, End time.Time
	Latest     bool
	// RatePercentPerHour is the night's rate when it was used (negative is
	// draining); LeftOut says why it was not when it is nil.
	RatePercentPerHour *float64
	LeftOut            string
}

// date is the boat-local date the night began on.
func (n overnightRuleNight) date(loc *time.Location) string {
	return n.Start.In(loc).Format("2006-01-02")
}

// overnightRuleStartHour is the boat-local hour the rule's window opens. By
// 22:00 the evening's cooking, lights and charging are over and what is left
// is the late-night load the dawn estimate is trying to predict.
const overnightRuleStartHour = 22

// ruleNightWindow narrows a night to the span the overnight_draw rule
// measures: 22:00 boat time on the evening the night began, to the night's own
// dawn end. A night whose window opens after 22:00 (a late summer sunset)
// keeps its own start, and one that ends before 22:00 has nothing to measure.
func ruleNightWindow(w nightWindow, loc *time.Location) (nightWindow, bool) {
	d := w.Start.In(loc)
	if d.Hour() < 12 {
		// The window opened after midnight, so the night began the evening before.
		d = d.AddDate(0, 0, -1)
	}
	start := time.Date(d.Year(), d.Month(), d.Day(), overnightRuleStartHour, 0, 0, 0, loc)
	if start.Before(w.Start) {
		start = w.Start
	}
	if !w.End.After(start) {
		return nightWindow{}, false
	}
	return nightWindow{Start: start, End: w.End}, true
}

// heaviestStretch finds the two-hour stretch of w that holds most of the
// night's fall in charge, for the finding's "most of it fell between" line.
// "Most" is more than half of the whole window's fall. The window is cut into
// two-hour stretches from its start; a last stretch under an hour joins the
// one before it. A window shorter than two stretches, a night that did not
// fall, or an even drain has no heaviest stretch.
func heaviestStretch(points []telemetryPoint, w nightWindow) (nightWindow, bool) {
	const stretch = 2 * time.Hour
	total := w.End.Sub(w.Start)
	n := int(total / stretch)
	if total-time.Duration(n)*stretch >= time.Hour {
		n++
	}
	if n < 2 {
		return nightWindow{}, false
	}
	night := pointsIn(points, w)
	fall := func(from, to time.Time) float64 {
		in := pointsIn(night, nightWindow{Start: from, End: to})
		if len(in) < 2 {
			return 0
		}
		return in[0].Value - in[len(in)-1].Value
	}
	whole := fall(w.Start, w.End)
	if whole <= 0 {
		return nightWindow{}, false
	}
	var best nightWindow
	bestFall := 0.0
	for i := 0; i < n; i++ {
		b := nightWindow{Start: w.Start.Add(time.Duration(i) * stretch), End: w.Start.Add(time.Duration(i+1) * stretch)}
		if i == n-1 {
			b.End = w.End
		}
		if f := fall(b.Start, b.End); f > bestFall {
			best, bestFall = b, f
		}
	}
	if bestFall > whole/2 {
		return best, true
	}
	return nightWindow{}, false
}

// overnightLookbackNights bounds how far back the history scan looks for
// usable nights: up to 30 completed nights, most recent first, before it
// gives up on finding more. overnightTargetNights is how many usable nights
// are enough to stop on - a boat with a long run of clean nights shouldn't
// pay for 30 nights of scanning every time when 7 will do.
//
// Both are constants, not env vars: they shape what "typical" means
// statistically (a wider or narrower lookback changes the answer), which
// isn't something an operator should be tuning per deployment.
const (
	overnightLookbackNights = 30
	overnightTargetNights   = 7
)

// overnightQueryEvery and overnightSampleInterval describe the same
// aggregation cadence in two forms InfluxDB and Go respectively need - the
// first is handed to aggregateWindow, the second is what
// nightSlopePercentPerHour uses to judge how complete a night's data is.
// Variables, not constants, so tests can point computeOvernight at the
// hourly-mean 7-day fixture captured from the live server without needing a
// live 15-minute-resolution series (mirrors the existing pattern in
// telemetry_history.go of history buffers being vars for the same reason).
var overnightQueryEvery = "15m"
var overnightSampleInterval = 15 * time.Minute

func stringPtr(s string) *string { return &s }

// resolveNonHistoryBasis is Decision 5's basis table collapsed to its
// "history didn't work out" half: linear when the operator has approved
// that degradation, none when they haven't.
func resolveNonHistoryBasis(linearFallback bool) string {
	if linearFallback {
		return "linear"
	}
	return "none"
}

// nextSunEvents returns the next sunset and next sunrise strictly after now.
// Both are needed regardless of basis: the tile's footer always shows a
// "Dawn HH:MM", and the frontend's projection arithmetic needs to know how
// far off both sunset and sunrise are, not just whichever comes first.
func nextSunEvents(now time.Time, loc *time.Location, lat, lon float64) (sunset, sunrise time.Time, ok bool) {
	nowLocal := now.In(loc)

	var haveSunset, haveSunrise bool
	for daysOffset := -1; daysOffset <= 2; daysOffset++ {
		day := nowLocal.AddDate(0, 0, daysOffset)
		rise, set, dayOK := sunTimes(day, lat, lon)
		if !dayOK {
			continue
		}
		if !haveSunset && set.After(now) {
			sunset = set
			haveSunset = true
		}
		if !haveSunrise && rise.After(now) {
			sunrise = rise
			haveSunrise = true
		}
	}
	return sunset, sunrise, haveSunset && haveSunrise
}

// anyPointExceedsInWindow reports whether any point of points that falls
// inside w has a value strictly greater than threshold. It backs both the
// shore-power and charging exclusion checks below, which differ only in
// which series they scan and what threshold counts as "running".
func anyPointExceedsInWindow(points []telemetryPoint, w nightWindow, threshold float64) bool {
	for _, p := range pointsIn(points, w) {
		if p.Value > threshold {
			return true
		}
	}
	return false
}

// pointsIn is the part of a series that falls inside w, ends included. A
// series in time order, as the history queries return one, is cut by binary
// search without a copy; one out of order is filtered instead, so the answer
// never depends on the order.
func pointsIn(points []telemetryPoint, w nightWindow) []telemetryPoint {
	inOrder := sort.SliceIsSorted(points, func(i, j int) bool { return points[i].Timestamp.Before(points[j].Timestamp) })
	if !inOrder {
		var in []telemetryPoint
		for _, p := range points {
			if !p.Timestamp.Before(w.Start) && !p.Timestamp.After(w.End) {
				in = append(in, p)
			}
		}
		return in
	}
	lo := sort.Search(len(points), func(i int) bool { return !points[i].Timestamp.Before(w.Start) })
	hi := sort.Search(len(points), func(i int) bool { return points[i].Timestamp.After(w.End) })
	if hi < lo {
		return nil
	}
	return points[lo:hi]
}

// computeOvernight resolves the dawn projection's basis and, when it lands
// on history, the median overnight rate. query is injected so tests never
// need a live InfluxDB - see queryInfluxPathRange, which satisfies this
// signature directly.
//
// The history scan asks for the SoC, shore-power and inverter AC-in series
// each exactly once, covering the whole lookback in a single [start, now]
// range per path, then slices each series per night in Go - not one query per
// night as before. It scans nights most recent first, excluding a night for
// shore power or charging before its slope is even judged, and stops as soon
// as overnightTargetNights usable nights have been collected or the
// overnightLookbackNights bound is exhausted, whichever comes first. The
// rule's 22:00-to-dawn scan (scanRuleNights) runs over the same series.
func computeOvernight(
	now time.Time,
	boat boatTime,
	lat, lon float64,
	paths overnightPaths,
	linearFallback bool,
	influxConfigured bool,
	query func(path string, start, stop time.Time, every string) ([]telemetryPoint, error),
) overnightResult {
	result := overnightResult{
		SocPath:        paths.Soc,
		ShoreChecked:   paths.Shore != "",
		ComputedAt:     now,
		LookbackNights: overnightLookbackNights,
		Loc:            boat.Loc,
	}

	// The boat's own resolved zone, the one the header clock keeps (with the
	// last-fix rule), not a fresh lookup. Before any fix has ever been seen
	// there is no zone, and the days cannot be cut.
	if boat.NoFix {
		result.Basis = "none"
		result.Reason = stringPtr("the boat's time zone is not known yet (no position fix has been seen)")
		return result
	}
	sunset, sunrise, ok := nextSunEvents(now, boat.Loc, lat, lon)
	if !ok {
		result.Basis = "none"
		result.Reason = stringPtr("sun does not rise or set at this position")
		return result
	}
	result.Sunset = sunset
	result.Sunrise = sunrise

	if !influxConfigured {
		result.Basis = resolveNonHistoryBasis(linearFallback)
		result.Reason = stringPtr("influxdb not configured")
		return result
	}

	windows := recentNightWindows(now, boat.Loc, lat, lon, overnightLookbackNights)
	if len(windows) == 0 {
		result.Basis = resolveNonHistoryBasis(linearFallback)
		result.Reason = stringPtr("need 2 usable nights, have 0")
		return result
	}

	// One query per path, covering every night the lookback could possibly
	// need, rather than one query per night - the oldest window in the list
	// is the earliest data any of the nights below could touch.
	queryStart := windows[len(windows)-1].Start
	socSeries, err := query(paths.Soc, queryStart, now, overnightQueryEvery)
	if err != nil {
		result.Basis = resolveNonHistoryBasis(linearFallback)
		result.Reason = stringPtr("influxdb query failed: " + err.Error())
		return result
	}
	// A series is only queried when the operator has chosen the device. A
	// failed query is never read as "it never ran": it fails the history
	// basis exactly as a SoC query failure does.
	var shoreSeries, inverterSeries []telemetryPoint
	if paths.Shore != "" {
		shoreSeries, err = query(paths.Shore, queryStart, now, overnightQueryEvery)
		if err != nil {
			result.Basis = resolveNonHistoryBasis(linearFallback)
			result.Reason = stringPtr("influxdb query failed: " + err.Error())
			return result
		}
	}
	if paths.Inverter != "" {
		inverterSeries, err = query(paths.Inverter, queryStart, now, overnightQueryEvery)
		if err != nil {
			result.Basis = resolveNonHistoryBasis(linearFallback)
			result.Reason = stringPtr("influxdb query failed: " + err.Error())
			return result
		}
	}
	// An inverter with no history at all is not a clean bill: with no points
	// there is nothing to say a night had no charging, so say it was not
	// checked and leave the nights in. With some history, judgeNight still
	// leaves out any single night the inverter logged nothing inside.
	result.ChargingChecked = len(inverterSeries) > 0

	var slopes []float64
	var nightsConsidered, excludedShore, excludedCharging, excludedChargingUnchecked, excludedGap, excludedRise int
	var oldestScanned nightWindow
	for _, w := range windows {
		nightsConsidered++
		oldestScanned = w

		rate, leftOut := judgeNight(w, socSeries, shoreSeries, inverterSeries, result.ChargingChecked)
		switch leftOut {
		case "":
			slopes = append(slopes, rate)
		case leftOutShore:
			excludedShore++
		case leftOutCharging:
			excludedCharging++
		case leftOutChargingUnchecked:
			excludedChargingUnchecked++
		case leftOutGap:
			excludedGap++
		case leftOutRise:
			excludedRise++
		}

		if len(slopes) >= overnightTargetNights {
			break
		}
	}

	result.NightsConsidered = nightsConsidered
	result.NightsUsed = len(slopes)
	result.NightsExcludedShore = excludedShore
	result.NightsExcludedCharging = excludedCharging
	result.NightsExcludedChargingUnchecked = excludedChargingUnchecked
	result.NightsExcludedGap = excludedGap
	result.NightsExcludedRise = excludedRise
	result.OldestNightStart = oldestScanned.Start

	result.RuleNights, result.LastNightHeaviest = scanRuleNights(windows, boat.Loc, socSeries, shoreSeries, inverterSeries, result.ChargingChecked)

	if median, ok := medianNightRate(slopes); ok {
		result.Basis = "history"
		result.NightRatePercentPerHour = &median
		return result
	}

	result.Basis = resolveNonHistoryBasis(linearFallback)
	result.Reason = stringPtr(fmt.Sprintf("need 2 usable nights, have %d", len(slopes)))
	return result
}

// judgeNight rates one window of a night: the SoC slope in percent an hour, or
// why the night is left out. The checks run in this order: shore power,
// charging (skipped when it was not checked at all; when it was, a night with
// no inverter samples inside it is left out as unchecked), then the slope itself, which
// leaves a night out for a gap in the data or a net rise. leftOut is empty
// when rate is the night's rate.
func judgeNight(w nightWindow, soc, shore, inverter []telemetryPoint, chargingChecked bool) (rate float64, leftOut string) {
	switch {
	case anyPointExceedsInWindow(shore, w, shorePowerPresentAmps):
		return 0, leftOutShore
	case chargingChecked && anyPointExceedsInWindow(inverter, w, inverterChargingPresentWatts):
		return 0, leftOutCharging
	}
	slope, usable, reason := nightSlopePercentPerHour(soc, w, overnightSampleInterval)
	switch {
	case usable && chargingChecked && len(pointsIn(inverter, w)) == 0:
		// A clean slope proves nothing about charging when the inverter logged
		// nothing inside the night.
		return slope, leftOutChargingUnchecked
	case usable:
		return slope, ""
	case reason == "net rise":
		return slope, leftOutRise
	default:
		return slope, leftOutGap
	}
}

// scanRuleNights judges each night over its 22:00-to-dawn window for the
// overnight_draw rule, most recent first, with the same exclusions as the
// dawn estimate: shore power, charging, a gap in the data, a net rise. It
// stops at overnightTargetNights usable nights. Charging is left out of the
// screen when it was not checked (chargingChecked false), and the night keeps
// its place in the comparison. The night built from windows[0] is marked
// Latest, so a first window with nothing to measure leaves none marked and an
// older night is never taken for last night. heaviest is the stretch holding
// most of the latest night's fall, nil unless that night was usable and one
// stretch does.
func scanRuleNights(windows []nightWindow, loc *time.Location, soc, shore, inverter []telemetryPoint, chargingChecked bool) (nights []overnightRuleNight, heaviest *nightWindow) {
	usable := 0
	for i, w := range windows {
		rw, ok := ruleNightWindow(w, loc)
		if !ok {
			continue
		}
		n := overnightRuleNight{Start: rw.Start, End: rw.End, Latest: i == 0}
		rate, leftOut := judgeNight(rw, soc, shore, inverter, chargingChecked)
		n.LeftOut = leftOut
		if leftOut == "" {
			n.RatePercentPerHour = &rate
			usable++
			if n.Latest {
				if h, ok := heaviestStretch(soc, rw); ok {
					heaviest = &h
				}
			}
		}
		nights = append(nights, n)
		if usable >= overnightTargetNights {
			break
		}
	}
	return nights, heaviest
}

// overnightPaths is every series the overnight model reads, all taken from
// the roles so the history, the bands and the alarm rules agree. Shore and
// Inverter are the two series a night has to clear before its slope is
// judged: the shore charger's AC input current and the inverter's AC input
// power. Either is empty when the operator has not chosen that device, and
// that exclusion is then skipped.
type overnightPaths struct {
	Soc      string
	Shore    string
	Inverter string
}

// cacheKey tells a cached result which paths it was computed for: choosing
// another bank, charger or inverter must not be served the old projection.
func (p overnightPaths) cacheKey() string {
	return p.Soc + "|" + p.Shore + "|" + p.Inverter
}

// overnightMeasurementPaths resolves the overnight series from Vessel
// settings. The state-of-charge path is the house bank's; the shore path is
// the chosen shore charger's AC input current; the inverter path is the
// chosen inverter's AC input power, which is real power into the Multi from
// shore or from the generator. Each is empty when not set, and the call is an
// error when the vessel block cannot be read.
//
// The generator's stateNumber is not used: on this boat it stays 0 while the
// generator runs (25 Oct 2026, the inverter's AC-in read about 8000 W until
// 19:30 with the state number 0 throughout). The shore charger path stays
// because the Skylla chargers feed DC, so their AC-in current is not the
// inverter's AC-in (the Multi is 60 Hz, and 50 Hz shore power feeds only the
// Skyllas). The shore path has points only while shore power is actually
// connected (a charger in float can hold the state of charge flat for the
// whole night, which passes the net-rise check and would otherwise score a
// marina night as a quiet one at anchor). A misnamed path yields no points,
// and no points means no exclusions, which is why the response says whether
// each check ran.
func overnightMeasurementPaths() (overnightPaths, error) {
	vessel, err := loadVesselView(getEnv("SETTINGS_FILE", "../settings.yaml"))
	if err != nil {
		return overnightPaths{}, err
	}
	paths := overnightPaths{Soc: vessel.houseBankSocPath()}
	if id := vessel.Electrical.ShoreChargerInstance; id != "" {
		paths.Shore = "electrical.chargers." + id + ".acin.1.current"
	}
	if id := vessel.Electrical.InverterInstance; id != "" {
		paths.Inverter = "electrical.inverters." + id + ".acin.power"
	}
	return paths, nil
}

// shorePowerPresentAmps is the current above which the shore path counts as
// "shore power connected" for one sample - comfortably above sensor noise
// on an idle AC-in circuit and comfortably below any real charger current.
const shorePowerPresentAmps = 0.5

// inverterChargingPresentWatts is the power above which the inverter's AC
// input counts as "charging" for one sample - comfortably above sensor noise
// (an idle inverter reads exactly 0 W here) and well below any real charge,
// which was about 8000 W from the generator on 25 Oct 2026.
const inverterChargingPresentWatts = 100.0

// dawnLinearFallbackEnabled resolves DAWN_LINEAR_FALLBACK, defaulting to
// true per Decision 5: without an operator-approved linear fallback, an
// absent or thin InfluxDB history means no dawn figure at all, which is the
// strict behaviour an operator can ask back for. Accepts the casual
// spellings an operator might type by hand; anything else is a
// configuration mistake worth a log line, not a silent default.
func dawnLinearFallbackEnabled() bool {
	raw := strings.ToLower(trimEnvValue(getEnv("DAWN_LINEAR_FALLBACK", "true")))
	switch raw {
	case "true", "1", "on":
		return true
	case "false", "0", "off":
		return false
	default:
		log.Printf("overnight model: invalid DAWN_LINEAR_FALLBACK %q, falling back to true", raw)
		return true
	}
}

// logOvernightStartupMode names, once at boot, which basis the dawn
// projection falls back to when history is unavailable - the AGENTS.md
// exception rule requires the fallback to be visible at startup, not only
// discoverable by reading a response body.
func logOvernightStartupMode() {
	if !influxTelemetryConfigured() {
		if dawnLinearFallbackEnabled() {
			log.Printf("overnight model: InfluxDB not configured; dawn projection will use live-rate extrapolation (DAWN_LINEAR_FALLBACK=true)")
		} else {
			log.Printf("overnight model: InfluxDB not configured; dawn projection will show no estimate until it is (DAWN_LINEAR_FALLBACK=false)")
		}
		return
	}
	log.Printf("overnight model: InfluxDB configured; dawn projection uses overnight history when at least two clean nights exist")
}

// overnightCacheTTL keeps the lookback-spanning history query (three range
// queries - SoC, shore, inverter - each covering up to overnightLookbackNights
// nights) off the request path - the tile polls this endpoint far more often
// than the answer can meaningfully change.
const overnightCacheTTL = 15 * time.Minute

type overnightCache struct {
	mu        sync.Mutex
	result    overnightResult
	expiresAt time.Time
	// Whole-degree position the cached result was computed for. Sunrise
	// moves about four minutes per degree of longitude, so a result is
	// only reusable near where it was made; a passage invalidates it.
	latKey, lonKey int
	// The paths the result was computed for (overnightPaths.cacheKey):
	// choosing another bank, charger or inverter must not be served the old
	// projection.
	key string
}

var globalOvernightCache = &overnightCache{}

// fresh returns the cached result when it has not expired. It does not care
// about position: a fresh result was computed from a valid fix within the
// last fifteen minutes, and the GNSS gate flapping in the meantime (it
// suppresses the position while it waits for a stable fix) does not move
// the sun.
func (c *overnightCache) fresh(now time.Time, key string) (overnightResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now.Before(c.expiresAt) && c.key == key {
		return c.result, true
	}
	return overnightResult{}, false
}

// get returns the cached result if still fresh and computed near this
// position, otherwise recomputes via compute and caches the new result for
// overnightCacheTTL.
func (c *overnightCache) get(now time.Time, lat, lon float64, key string, compute func() overnightResult) overnightResult {
	latKey, lonKey := int(math.Round(lat)), int(math.Round(lon))
	c.mu.Lock()
	defer c.mu.Unlock()
	if now.Before(c.expiresAt) && c.latKey == latKey && c.lonKey == lonKey && c.key == key {
		return c.result
	}
	result := compute()
	c.result = result
	c.expiresAt = now.Add(overnightCacheTTL)
	c.latKey, c.lonKey, c.key = latKey, lonKey, key
	return result
}

// positionForSun is the one place that decides whether the vessel state
// carries a position the sun calculation may use. Three things say no: the
// GNSS validation gate holding the position back (gnss_critical_alert), the
// -1/-1 pair the backend publishes when it has no position at all, and a
// value outside the coordinate range. The range check alone was not enough:
// -1,-1 is a legal coordinate in the Gulf of Guinea, and computing sunrise
// there put every night window twelve hours off the boat's own.
func positionForSun(state vesselStateData) (lat, lon float64, ok bool) {
	if state.GNSSCriticalAlert {
		return 0, 0, false
	}
	if state.Latitude == -1 && state.Longitude == -1 {
		return 0, 0, false
	}
	if state.Latitude < -90 || state.Latitude > 90 || state.Longitude < -180 || state.Longitude > 180 {
		return 0, 0, false
	}
	return state.Latitude, state.Longitude, true
}

// electricalOvernightHandler serves GET /api/electrical/overnight. Position
// comes from the same in-memory SignalK snapshot every other vessel-state
// handler reads (fetchSignalKVesselState) - this never opens its own
// connection to SignalK.
func electricalOvernightHandler(c echo.Context) error {
	now := time.Now().UTC()

	paths, socErr := overnightMeasurementPaths()
	noteVesselRead("overnight", socErr, "overnight model: vessel unreadable, no projection: %v")
	if socErr != nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{
			"error": "vessel unreadable; dawn projection needs the house bank",
		})
	}
	if paths.Soc == "" {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{
			"error": "no house bank is chosen under Settings > SignalK > Roles; dawn projection needs one",
		})
	}
	cacheKey := paths.cacheKey()
	if cached, ok := globalOvernightCache.fresh(now, cacheKey); ok {
		return c.JSON(http.StatusOK, cached)
	}

	state, err := fetchSignalKVesselState()
	lat, lon, havePosition := positionForSun(state)
	if err != nil || !havePosition {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{
			"error": "vessel position unavailable; dawn projection needs it for sunrise",
		})
	}

	result := globalOvernightCache.get(now, lat, lon, cacheKey, func() overnightResult {
		r := computeOvernight(now, boatTimeForState(state, err), lat, lon, paths, dawnLinearFallbackEnabled(), influxTelemetryConfigured(), queryInfluxPathRange)
		if r.Basis == "history" {
			log.Printf(
				"overnight model: basis=history nights_used=%d scanned=%d excluded shore=%d (checked=%t) charging=%d (checked=%t) gap=%d rise=%d oldest=%s",
				r.NightsUsed, r.NightsConsidered, r.NightsExcludedShore, r.ShoreChecked, r.NightsExcludedCharging, r.ChargingChecked, r.NightsExcludedGap, r.NightsExcludedRise, r.OldestNightStart.Format(time.RFC3339),
			)
		} else {
			reason := ""
			if r.Reason != nil {
				reason = *r.Reason
			}
			log.Printf("overnight model: basis=%s reason=%s", r.Basis, reason)
		}
		return r
	})

	return c.JSON(http.StatusOK, result)
}
