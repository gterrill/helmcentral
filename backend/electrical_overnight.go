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
// The vessel has no named IANA timezone, only a position, so day boundaries
// for the sunTimes lookups are picked using a fixed offset derived from
// longitude (15 degrees per hour) rather than a real civil calendar. This
// is accurate enough to find the right nights; it is never surfaced to an
// operator as a clock.
func recentNightWindows(now time.Time, lat, lon float64, nights int) []nightWindow {
	if nights <= 0 {
		return nil
	}

	shipLoc := time.FixedZone("ship", int(math.Round(lon/15))*3600)
	nowLocal := now.In(shipLoc)

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
	NightsUsed              int       `json:"nights_used"`
	NightsConsidered        int       `json:"nights_considered"`
	Reason                  *string   `json:"reason"`
	ComputedAt              time.Time `json:"computed_at"`
}

// overnightNightsConsidered is how many trailing nights the history branch
// scans for usable slopes (Phase 4 of the plan: "the last 7 nights").
const overnightNightsConsidered = 7

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
func nextSunEvents(now time.Time, lat, lon float64) (sunset, sunrise time.Time, ok bool) {
	shipLoc := time.FixedZone("ship", int(math.Round(lon/15))*3600)
	nowLocal := now.In(shipLoc)

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

// computeOvernight resolves the dawn projection's basis and, when it lands
// on history, the median overnight rate. query is injected so tests never
// need a live InfluxDB - see queryInfluxPathRange, which satisfies this
// signature directly.
func computeOvernight(
	now time.Time,
	lat, lon float64,
	socPath string,
	linearFallback bool,
	influxConfigured bool,
	query func(path string, start, stop time.Time, every string) ([]telemetryPoint, error),
) overnightResult {
	result := overnightResult{
		SocPath:    socPath,
		ComputedAt: now,
	}

	sunset, sunrise, ok := nextSunEvents(now, lat, lon)
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

	windows := recentNightWindows(now, lat, lon, overnightNightsConsidered)
	result.NightsConsidered = len(windows)

	var slopes []float64
	for _, w := range windows {
		points, err := query(socPath, w.Start, w.End, overnightQueryEvery)
		if err != nil {
			// Fail fast rather than skip this night and keep going: a
			// failing query almost always means Influx itself is down, and
			// silently scoring fewer nights would misreport "not enough
			// clean nights" when the real problem is "couldn't ask".
			result.Basis = resolveNonHistoryBasis(linearFallback)
			result.Reason = stringPtr("influxdb query failed: " + err.Error())
			return result
		}
		slope, usable, _ := nightSlopePercentPerHour(points, w, overnightSampleInterval)
		if usable {
			slopes = append(slopes, slope)
		}
	}
	result.NightsUsed = len(slopes)

	if median, ok := medianNightRate(slopes); ok {
		result.Basis = "history"
		result.NightRatePercentPerHour = &median
		return result
	}

	result.Basis = resolveNonHistoryBasis(linearFallback)
	result.Reason = stringPtr(fmt.Sprintf("need 2 usable nights, have %d", len(slopes)))
	return result
}

// defaultSocMeasurement matches the alarm rule path this plan pins the SoC
// bands to (Decision 7), so history, bands and the alarm rule all agree on
// which battery instance the boat's "the bank" refers to.
const defaultSocMeasurement = "electrical.batteries.0.capacity.stateOfCharge"

func socMeasurementPath() string {
	return trimEnvValue(getEnv("INFLUX_SOC_MEASUREMENT", defaultSocMeasurement))
}

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

// overnightCacheTTL keeps the 7-day-spanning history query (up to 7 range
// queries, one per night) off the request path - the tile polls this
// endpoint far more often than the answer can meaningfully change.
const overnightCacheTTL = 15 * time.Minute

type overnightCache struct {
	mu        sync.Mutex
	result    overnightResult
	expiresAt time.Time
}

var globalOvernightCache = &overnightCache{}

// get returns the cached result if still fresh, otherwise recomputes via
// compute and caches the new result for overnightCacheTTL.
func (c *overnightCache) get(now time.Time, compute func() overnightResult) overnightResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now.Before(c.expiresAt) {
		return c.result
	}
	result := compute()
	c.result = result
	c.expiresAt = now.Add(overnightCacheTTL)
	return result
}

// electricalOvernightHandler serves GET /api/electrical/overnight. Position
// comes from the same in-memory SignalK snapshot every other vessel-state
// handler reads (fetchSignalKVesselState) - this never opens its own
// connection to SignalK.
func electricalOvernightHandler(c echo.Context) error {
	state, err := fetchSignalKVesselState()
	if err != nil || state.Latitude < -90 || state.Latitude > 90 || state.Longitude < -180 || state.Longitude > 180 {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{
			"error": "vessel position unavailable; dawn projection needs it for sunrise",
		})
	}

	now := time.Now().UTC()
	result := globalOvernightCache.get(now, func() overnightResult {
		r := computeOvernight(now, state.Latitude, state.Longitude, socMeasurementPath(), dawnLinearFallbackEnabled(), influxTelemetryConfigured(), queryInfluxPathRange)
		if r.Basis != "history" {
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
