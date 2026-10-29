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
// The history scan excludes any night shore power or the generator ran
// (the shore charger and generator the operator picked in the roles), on top of the
// pre-existing gap and net-rise checks. This was added after a marina week
// of shore-power nights passed the net-rise check outright - a charger in
// float holds the state of charge flat, which reads as a quiet night at
// anchor rather than the charged night it actually was, and pulled the
// median toward zero. The scan now looks back up to overnightLookbackNights
// nights, most recent first, and stops once overnightTargetNights usable
// nights are found.

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
// its own, so a flat or rising reading means a generator or shore charger
// ran, and that night says nothing about the boat's baseline overnight
// draw.
//
// By the time a night reaches this function, computeOvernight has already
// excluded it for shore power or the generator if either of those series
// showed one. The net-rise check here stays anyway as the backstop for a
// source those two paths didn't see - a charger reading, a shared circuit,
// or a misnamed measurement path - so a night that quietly charged never
// gets counted as a discharge just because computeOvernight's own two
// checks missed it.
func nightSlopePercentPerHour(points []telemetryPoint, w nightWindow, every time.Duration) (slope float64, usable bool, reason string) {
	var inWindow []telemetryPoint
	for _, p := range points {
		if !p.Timestamp.Before(w.Start) && !p.Timestamp.After(w.End) {
			inWindow = append(inWindow, p)
		}
	}

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
	// ShoreChecked and GeneratorChecked say whether nights were screened for
	// shore power and generator running. They are false when the roles
	// name no shore charger or generator, so a zero count beside them means
	// "not checked", not "none found".
	ShoreChecked            bool      `json:"shore_checked"`
	GeneratorChecked        bool      `json:"generator_checked"`
	NightsExcludedShore     int       `json:"nights_excluded_shore"`
	NightsExcludedGenerator int       `json:"nights_excluded_generator"`
	NightsExcludedGap       int       `json:"nights_excluded_gap"`
	NightsExcludedRise      int       `json:"nights_excluded_rise"`
	OldestNightStart        time.Time `json:"oldest_night_start"`
	Reason                  *string   `json:"reason"`
	ComputedAt              time.Time `json:"computed_at"`

	// LastNightRatePercentPerHour is the most recent completed night's own
	// rate, set only when that night was usable, and LastNightStart is when its
	// window began. PriorNightRatesPercentPerHour are the usable nights before
	// it, most recent first. get_vessel_status compares the one with the
	// median of the others (ADR 0222). None of them are on the wire.
	LastNightStart                time.Time `json:"-"`
	LastNightEnd                  time.Time `json:"-"`
	LastNightRatePercentPerHour   *float64  `json:"-"`
	PriorNightRatesPercentPerHour []float64 `json:"-"`
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
// shore-power and generator exclusion checks below, which differ only in
// which series they scan and what threshold counts as "running".
func anyPointExceedsInWindow(points []telemetryPoint, w nightWindow, threshold float64) bool {
	for _, p := range points {
		if p.Timestamp.Before(w.Start) || p.Timestamp.After(w.End) {
			continue
		}
		if p.Value > threshold {
			return true
		}
	}
	return false
}

// computeOvernight resolves the dawn projection's basis and, when it lands
// on history, the median overnight rate. query is injected so tests never
// need a live InfluxDB - see queryInfluxPathRange, which satisfies this
// signature directly.
//
// The history scan asks for the SoC, shore-power and generator series each
// exactly once, covering the whole lookback in a single [start, now] range
// per path, then slices each series per night in Go - not one query per
// night as before. It scans nights most recent first, excluding a night for
// shore power or a generator before its slope is even judged, and stops as
// soon as overnightTargetNights usable nights have been collected or the
// overnightLookbackNights bound is exhausted, whichever comes first.
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
		SocPath:          paths.Soc,
		ShoreChecked:     paths.Shore != "",
		GeneratorChecked: paths.Generator != "",
		ComputedAt:       now,
		LookbackNights:   overnightLookbackNights,
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
	var shoreSeries, generatorSeries []telemetryPoint
	if paths.Shore != "" {
		shoreSeries, err = query(paths.Shore, queryStart, now, overnightQueryEvery)
		if err != nil {
			result.Basis = resolveNonHistoryBasis(linearFallback)
			result.Reason = stringPtr("influxdb query failed: " + err.Error())
			return result
		}
	}
	if paths.Generator != "" {
		generatorSeries, err = query(paths.Generator, queryStart, now, overnightQueryEvery)
		if err != nil {
			result.Basis = resolveNonHistoryBasis(linearFallback)
			result.Reason = stringPtr("influxdb query failed: " + err.Error())
			return result
		}
	}

	var slopes []float64
	var lastRate float64
	var lastStart, lastEnd time.Time
	var lastUsable bool
	var nightsConsidered, excludedShore, excludedGenerator, excludedGap, excludedRise int
	var oldestScanned nightWindow
	for _, w := range windows {
		nightsConsidered++
		oldestScanned = w

		switch {
		case anyPointExceedsInWindow(shoreSeries, w, shorePowerPresentAmps):
			excludedShore++
		case anyPointExceedsInWindow(generatorSeries, w, 0):
			excludedGenerator++
		default:
			slope, usable, reason := nightSlopePercentPerHour(socSeries, w, overnightSampleInterval)
			switch {
			case usable:
				if nightsConsidered == 1 {
					lastRate, lastStart, lastEnd = slope, w.Start, w.End
					lastUsable = true
				}
				slopes = append(slopes, slope)
			case reason == "insufficient samples":
				excludedGap++
			case reason == "net rise":
				excludedRise++
			}
		}

		if len(slopes) >= overnightTargetNights {
			break
		}
	}

	result.NightsConsidered = nightsConsidered
	result.NightsUsed = len(slopes)
	result.NightsExcludedShore = excludedShore
	result.NightsExcludedGenerator = excludedGenerator
	result.NightsExcludedGap = excludedGap
	result.NightsExcludedRise = excludedRise
	result.OldestNightStart = oldestScanned.Start
	if lastUsable {
		result.LastNightRatePercentPerHour = &lastRate
		result.LastNightStart, result.LastNightEnd = lastStart, lastEnd
		result.PriorNightRatesPercentPerHour = append([]float64(nil), slopes[1:]...)
	}

	if median, ok := medianNightRate(slopes); ok {
		result.Basis = "history"
		result.NightRatePercentPerHour = &median
		return result
	}

	result.Basis = resolveNonHistoryBasis(linearFallback)
	result.Reason = stringPtr(fmt.Sprintf("need 2 usable nights, have %d", len(slopes)))
	return result
}

// overnightPaths is every series the overnight model reads, all taken from
// the roles so the history, the bands and the alarm rules agree. Shore
// and Generator are the two series a night has to clear before its slope is
// judged: the shore charger's AC input current and the generator's state.
// Either is empty when the operator has not chosen that device, and that
// exclusion is then skipped.
type overnightPaths struct {
	Soc       string
	Shore     string
	Generator string
}

// cacheKey tells a cached result which paths it was computed for: choosing
// another bank, charger or generator must not be served the old projection.
func (p overnightPaths) cacheKey() string {
	return p.Soc + "|" + p.Shore + "|" + p.Generator
}

// overnightMeasurementPaths resolves the overnight series from Vessel
// settings. The state-of-charge path is the house bank's; the shore path is
// the chosen shore charger's AC input current; the generator path is the
// chosen generator's state number. Each is empty when not set, and the call
// is an error when the vessel block cannot be read.
//
// Confirmed against the live InfluxDB on 2026-09-07: the shore path has
// points only while shore power is actually connected (a charger in float
// can hold the state of charge flat for the whole night, which passes the
// net-rise check and would otherwise score a marina night as a quiet one at
// anchor); the generator path is recorded continuously and reads > 0 while
// the generator runs. A misnamed path yields no points, and no points means
// no exclusions, which is why the response carries the excluded counts.
func overnightMeasurementPaths() (overnightPaths, error) {
	vessel, err := loadVesselView(getEnv("SETTINGS_FILE", "../settings.yaml"))
	if err != nil {
		return overnightPaths{}, err
	}
	paths := overnightPaths{Soc: vessel.houseBankSocPath()}
	if id := vessel.Electrical.ShoreChargerInstance; id != "" {
		paths.Shore = "electrical.chargers." + id + ".acin.1.current"
	}
	if id := vessel.Electrical.GeneratorInstance; id != "" {
		paths.Generator = "electrical.generator." + id + ".stateNumber"
	}
	return paths, nil
}

// shorePowerPresentAmps is the current above which the shore path counts as
// "shore power connected" for one sample - comfortably above sensor noise
// on an idle AC-in circuit and comfortably below any real charger current.
const shorePowerPresentAmps = 0.5

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
// queries - SoC, shore, generator - each covering up to overnightLookbackNights
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
	// choosing another bank, charger or generator must not be served the old
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
				"overnight model: basis=history nights_used=%d scanned=%d excluded shore=%d (checked=%t) generator=%d (checked=%t) gap=%d rise=%d oldest=%s",
				r.NightsUsed, r.NightsConsidered, r.NightsExcludedShore, r.ShoreChecked, r.NightsExcludedGenerator, r.GeneratorChecked, r.NightsExcludedGap, r.NightsExcludedRise, r.OldestNightStart.Format(time.RFC3339),
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
