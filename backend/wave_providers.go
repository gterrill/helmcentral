// Package main: pluggable wave-provider host system.
//
// Wave forecasting used to be hardcoded against Open-Meteo Marine directly
// in weather_tide.go. It is now a WASM plugin type, exactly like tides and
// weather (tide_providers.go / wasm_tide_provider.go and
// weather_providers.go / wasm_weather_provider.go): a waveProvider
// interface, a registry, HTTP handlers, and a WASM adapter
// (wasm_wave_provider.go). The host owns local-day bucketing (via
// vesselLocalLocation, exactly like weather) and the derived wave summary; a
// plugin's only job is to answer fetch_waves with SI-unit,
// RFC3339-timestamped data (see the guest contract doc comment below).
//
// There is no native built-in wave provider - waveProviderRegistry starts
// empty and stays that way until a WASM plugin is installed in
// plugins/waves, matching weatherProvider's shape (both reference providers
// in this codebase are WASM-only by design).
//
// Guest contract (fetch_waves), mirrored below by wasmFetchWavesOutput in
// wasm_wave_provider.go:
//
//	fetch_waves({"lat": float64, "lon": float64, "days": int}) -> {
//	  "hourly": [{time, wave_height_m, wave_period_s, wave_direction_deg,
//	              wind_wave_height_m, wind_wave_direction_deg, wind_wave_period_s,
//	              swell_wave_height_m, swell_wave_direction_deg, swell_wave_period_s}],
//	  "sea_surface_temperature_c": float64 (optional)
//	}
//
// The per-component direction and period fields are OPTIONAL in the sense
// that a model without them may omit them; they then read as 0, and a
// component with a zero period is treated as absent rather than as a wave
// train heading due north. That is not a guess - it is how Open-Meteo itself
// reports a flat component, verified against a live response.
//
// All times are RFC3339. sea_surface_temperature_c is OPTIONAL: a plugin or
// upstream model that genuinely has no sea-temperature data for a location
// omits the field entirely (mapped to a nil *float64 here, never a masked
// zero/placeholder value) - see wasm_wave_provider.go's
// wasmFetchWavesOutput.SeaSurfaceTemperatureC.
//
// Why the reference plugin (docs/examples/wave-plugins/open-meteo-marine)
// queries the way it does: its wave forecast fetch pins Open-Meteo Marine's
// `models=ncep_gfswave025` param (NOAA's GFS-Wave / WaveWatch III model) so
// swell height, period and direction line up with other WaveWatch
// III-based swell forecasts for a given coastline - Open-Meteo's default
// "best_match" model blend runs noticeably lower/different. Its separate
// sea-surface-temperature fetch deliberately OMITS that `models=` param -
// the wave-specific NOAA GFS-Wave model does not carry sea surface
// temperature at all (returns null/"undefined" units); only Open-Meteo's
// default model blend does. Any other wave plugin faces the same choice.
package main

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// waveHourPoint is a single hour's wave forecast, SI units, as returned by
// one entry of a plugin's fetch_waves "hourly" field.
type waveHourPoint struct {
	Time             time.Time
	WaveHeightM      float64
	WavePeriodS      float64
	WaveDirectionDeg float64
	WindWaveHeightM  float64
	SwellWaveHeightM float64

	// Per-component direction and period. The combined figures above smear
	// two systems into one, which is exactly what hides a cross sea.
	WindWaveDirectionDeg  float64
	WindWavePeriodS       float64
	SwellWaveDirectionDeg float64
	SwellWavePeriodS      float64
}

// waveForecastBundle is one provider round-trip's worth of data - the
// return value of waveProvider.FetchWaves and of a WASM plugin's
// fetch_waves, mapped to typed Go values. SeaTempC is nil when the
// provider/model genuinely has no sea-temperature data for this location,
// never a masked/placeholder value. Cached/CachedAt describe the adapter's
// own cache bookkeeping, independent of any timestamp inside Hourly.
type waveForecastBundle struct {
	Hourly   []waveHourPoint
	SeaTempC *float64
	Cached   bool
	CachedAt time.Time
}

// waveProvider is the interface implemented by each pluggable wave data
// source. The reference provider (open-meteo-marine) is a WASM plugin -
// there is no native built-in, mirroring weatherProvider's shape.
type waveProvider interface {
	ID() string
	Name() string
	Description() string
	TTLSeconds() int64
	FetchWaves(lat, lon float64, days int) (waveForecastBundle, error)
}

var waveProviderRegistry = map[string]waveProvider{}
var waveProviderOrder []string

func registerWaveProvider(p waveProvider) {
	waveProviderRegistry[p.ID()] = p
	waveProviderOrder = append(waveProviderOrder, p.ID())
}

func getWaveProvider(id string) (waveProvider, bool) {
	p, ok := waveProviderRegistry[id]
	return p, ok
}

type waveProviderInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// waveProvidersHandler serves GET /api/wave-providers, mirroring
// weatherProvidersHandler.
func waveProvidersHandler(c echo.Context) error {
	result := make([]waveProviderInfo, 0, len(waveProviderOrder))
	for _, id := range waveProviderOrder {
		if provider, ok := waveProviderRegistry[id]; ok {
			result = append(result, waveProviderInfo{ID: provider.ID(), Name: provider.Name(), Description: provider.Description()})
		}
	}
	return c.JSON(http.StatusOK, result)
}

// defaultWaveProviderID is used when ui.wave_provider is unset in
// settings.yaml.
const defaultWaveProviderID = "open-meteo-marine"

// resolveWaveProvider reads ui.wave_provider from settingsPath (defaulting
// to defaultWaveProviderID) and resolves it against the registry, mirroring
// resolveWeatherProvider's idiom. Returns a clear, actionable error - never
// a fallback provider - when the configured id isn't registered, since an
// unregistered provider most likely means the plugin hasn't been installed
// yet.
func resolveWaveProvider(settingsPath string) (waveProvider, string, error) {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read settings: %w", err)
	}

	uiMap, _ := settings["ui"].(map[string]any)
	configuredProvider := strings.TrimSpace(coerceString(uiMap["wave_provider"]))
	if configuredProvider == "" {
		configuredProvider = defaultWaveProviderID
	}

	provider, ok := getWaveProvider(configuredProvider)
	if !ok {
		return nil, configuredProvider, fmt.Errorf("unknown wave provider configured: %q (is the plugin installed in plugins/waves?)", configuredProvider)
	}

	return provider, configuredProvider, nil
}

// --- host-side derivation: day bucketing, summaries ---

// waveDayHourlyData is a single hour's wave data bucketed into a local
// calendar day - the wave-only analogue of weatherHourlyWindData etc.
// Label/HourOfDay preserve the same meaning used throughout the weather
// system (local "3PM"-style hour string / local hour 0-23).
type waveDayHourlyData struct {
	Label            string
	HourOfDay        int
	WaveHeightM      float64
	WavePeriodS      float64
	WaveDirectionDeg float64
	WindWaveHeightM  float64
	SwellWaveHeightM float64

	WindWaveDirectionDeg  float64
	WindWavePeriodS       float64
	SwellWaveDirectionDeg float64
	SwellWavePeriodS      float64
}

// buildWaveHourlySeriesByDay buckets a provider's flat hourly points into
// per-day (local date) wave series, keyed by "2006-01-02" in localLocation -
// mirrors buildHourlySeriesByDay in weather_providers.go, but simpler since
// waves have only one hourly series rather than four.
func buildWaveHourlySeriesByDay(hourly []waveHourPoint, localLocation *time.Location) map[string][]waveDayHourlyData {
	series := make(map[string][]waveDayHourlyData)

	for _, hp := range hourly {
		if hp.Time.IsZero() {
			continue
		}
		localTime := hp.Time.In(localLocation)
		dayKey := localTime.Format("2006-01-02")

		series[dayKey] = append(series[dayKey], waveDayHourlyData{
			Label:            localTime.Format("3PM"),
			HourOfDay:        localTime.Hour(),
			WaveHeightM:      hp.WaveHeightM,
			WavePeriodS:      hp.WavePeriodS,
			WaveDirectionDeg: hp.WaveDirectionDeg,
			WindWaveHeightM:  hp.WindWaveHeightM,
			SwellWaveHeightM: hp.SwellWaveHeightM,

			WindWaveDirectionDeg:  hp.WindWaveDirectionDeg,
			WindWavePeriodS:       hp.WindWavePeriodS,
			SwellWaveDirectionDeg: hp.SwellWaveDirectionDeg,
			SwellWavePeriodS:      hp.SwellWavePeriodS,
		})
	}

	return series
}

// --- host-side derivation: wave steepness ---

// Steepness bands. The names are the ones the forecast page renders, so they
// are part of the API rather than an internal detail.
const (
	waveSteepnessRolling  = "rolling"
	waveSteepnessBuilding = "building"
	waveSteepnessSteep    = "steep"
	waveSteepnessBreaking = "breaking"
)

// Band edges as height-over-length ratios.
//
// The breaking edge is 1-in-10 rather than the 1-in-7 a tank test gives.
// Surviving the Storm reports both on page 231 and endorses the former:
// "Theory and tank test data indicate that waves become unstable at a slope
// ratio of 1 to 7 or steeper. However, observations in the real world
// indicate this figure is more like 1 to 10." Page 240 states it as a number:
// "A steepness ratio of 0.1 or 1 in 10 would imply breaking seas."
//
// The rolling edge is 1-in-25, the slope the same book works out on page 252
// and calls "not normally considered steep enough to break". The middle edge
// is interpolated between the two and carries no separate authority.
const (
	waveSteepnessRollingMax  = 0.04 // 1 in 25
	waveSteepnessBuildingMax = 0.07
	waveSteepnessBreakingMin = 0.10 // 1 in 10
)

// gravityMPerS2 is standard gravity, for the deep-water wavelength relation.
const gravityMPerS2 = 9.80665

// waveSteepness is height over deep-water wavelength, and whether the figure
// is defined at all.
//
// Wavelength is the textbook deep-water relation L = gT²/2π, which is the
// same one the book gives in feet on page 218 as "5 times the period
// squared" for swell. It reproduces the worked example on page 252 exactly:
// a 14-second period comes out at 1,004 feet against the book's stated
// "1,000 to 1,300 feet" for 14 to 16 seconds.
//
// Reports absence rather than zero whenever the ratio is undefined - no
// period reported, or the negative marker the rest of this file uses for a
// missing height. A zero steepness would render as a glassy sea, which is a
// measurement rather than the absence of one (the same discipline SeaTempC
// applies to sea temperature).
func waveSteepness(heightM, periodS float64) (float64, bool) {
	if periodS <= 0 || heightM < 0 {
		return 0, false
	}

	wavelengthM := gravityMPerS2 * periodS * periodS / (2 * math.Pi)
	if wavelengthM <= 0 {
		return 0, false
	}

	return heightM / wavelengthM, true
}

// waveSteepnessBand names the band a steepness ratio falls in, for the
// forecast page to colour by.
func waveSteepnessBand(steepness float64) string {
	switch {
	case steepness >= waveSteepnessBreakingMin:
		return waveSteepnessBreaking
	case steepness >= waveSteepnessBuildingMax:
		return waveSteepnessSteep
	case steepness >= waveSteepnessRollingMax:
		return waveSteepnessBuilding
	default:
		return waveSteepnessRolling
	}
}

// formatSteepnessRatio renders a steepness as the "1 in N" slope the book
// and every mariner talks in.
//
// N is floored rather than rounded, so a 1-in-9.8 sea reads as 1:9 and not
// the flatter-sounding 1:10. Rounding the other way would understate the
// slope, and this figure exists to warn.
func formatSteepnessRatio(steepness float64) string {
	if steepness <= 0 {
		return ""
	}
	return fmt.Sprintf("1:%d", int(math.Floor(1/steepness)))
}

// peakWaveSteepness is the steepest hour of a day, and whether any hour had
// a defined steepness at all.
func peakWaveSteepness(hourly []waveDayHourlyData) (float64, bool) {
	peak := 0.0
	found := false
	for _, entry := range hourly {
		steepness, ok := waveSteepness(entry.WaveHeightM, entry.WavePeriodS)
		if !ok {
			continue
		}
		found = true
		if steepness > peak {
			peak = steepness
		}
	}
	return peak, found
}

// --- host-side derivation: leading indicators ---

/*
waveDayIndicatorSet is the set of warning signs Surviving the Storm names as
detectable from a wave forecast alone, evaluated per day.

Each is a documented threshold from the book rather than a house heuristic,
and each is deliberately narrow. A flag that fires most days is a flag the
operator stops reading, which is the same failure the alarm engine's dwell
and hysteresis exist to prevent.
*/
type waveDayIndicatorSet struct {
	// WaveFront is a 3m rise inside any three-hour window - the screening
	// criterion Scott Prosise of the NOAA Marine Prediction Center used to
	// find dynamic-fetch events in a year of buoy data (page 246).
	WaveFront bool `json:"wave_front"`

	// RapidBuild is height and period both up 50% inside an hour, which Lee
	// Chesneau calls "a certain danger signal" (page 250). Both must rise:
	// height alone is an ordinary building sea.
	RapidBuild bool `json:"rapid_build"`

	// PeriodStep is the period lengthening 3s or more in an hour, the "8-second
	// period that suddenly lengthens to 11 seconds" Prosise describes as a
	// leading indicator of a wave front (page 250).
	PeriodStep bool `json:"period_step"`

	// CrossSea is the wind wave and the swell running more than 60 degrees
	// apart. Page 233: a secondary system "not large enough initially to do
	// great harm on their own" is what puts a boat beam-on to the primary
	// seas, which is where the harm comes from.
	CrossSea bool `json:"cross_sea"`
}

// crossSeaThresholdDeg is where two wave trains stop being one system's own
// spread. Page 233 puts a single system at "plus or minus 20 to 30 degrees"
// off the wind axis, so past 60 the simpler explanation is two systems.
const crossSeaThresholdDeg = 60.0

// crossSeaMinSecondaryFraction is how big the smaller train has to be,
// relative to the larger, before the two count as crossing seas.
//
// Found against live data: the model will happily report a 0.02m swell beside
// a 1.44m wind sea 75 degrees away. Page 233's argument for why a secondary
// system matters is that it swings the boat off its alignment with the
// primary one; a component this small cannot. A third is the point where it
// plausibly can.
const crossSeaMinSecondaryFraction = 1.0 / 3.0

/*
crossSeaSeparationDeg is the angle between the wind-wave and swell trains,
and whether both trains actually exist.

The presence test is the period, not the height or the direction. Verified
against a live Open-Meteo Marine response: a component with no waves in it
comes back with height, period AND direction all reported as 0, not null. A
naive angle would then read a flat swell's placeholder "0 degrees" against a
real wind wave and manufacture a large separation on an ordinary
single-system day. A wave train with no period is not a wave train.
*/
func crossSeaSeparationDeg(entry waveDayHourlyData) (float64, bool) {
	if entry.WindWavePeriodS <= 0 || entry.SwellWavePeriodS <= 0 {
		return 0, false
	}
	if entry.WindWaveHeightM <= 0 || entry.SwellWaveHeightM <= 0 {
		return 0, false
	}

	smaller := math.Min(entry.WindWaveHeightM, entry.SwellWaveHeightM)
	larger := math.Max(entry.WindWaveHeightM, entry.SwellWaveHeightM)
	if smaller/larger < crossSeaMinSecondaryFraction {
		return 0, false
	}

	radians := (entry.WindWaveDirectionDeg - entry.SwellWaveDirectionDeg) * math.Pi / 180
	return math.Abs(shortestAngleDiffRadians(radians, 0)) * 180 / math.Pi, true
}

const (
	waveFrontRiseM       = 3.0 // 10 feet, page 246
	waveFrontWindowHours = 3
	rapidBuildFraction   = 1.5 // a 50% increase, page 250
	periodStepS          = 3.0 // 8s to 11s, page 250
)

// waveDayIndicators evaluates every indicator over a day's hourly series.
//
// Hours carrying the negative marker this file uses for missing data are
// skipped rather than treated as zero, so a gap in the feed cannot manufacture
// a 3m rise out of nothing.
func waveDayIndicators(hourly []waveDayHourlyData) waveDayIndicatorSet {
	var set waveDayIndicatorSet

	for i, entry := range hourly {
		if separation, ok := crossSeaSeparationDeg(entry); ok && separation >= crossSeaThresholdDeg {
			set.CrossSea = true
		}

		if entry.WaveHeightM < 0 {
			continue
		}

		// Wave front: compare against every earlier hour still inside the
		// window, which handles a series with gaps in it correctly.
		for j := i - 1; j >= 0 && i-j <= waveFrontWindowHours; j-- {
			prior := hourly[j]
			if prior.WaveHeightM < 0 {
				continue
			}
			if entry.WaveHeightM-prior.WaveHeightM >= waveFrontRiseM {
				set.WaveFront = true
			}
		}

		if i == 0 {
			continue
		}
		prev := hourly[i-1]

		if prev.WavePeriodS > 0 && entry.WavePeriodS > 0 {
			if entry.WavePeriodS-prev.WavePeriodS >= periodStepS {
				set.PeriodStep = true
			}
			if prev.WaveHeightM > 0 &&
				entry.WaveHeightM >= prev.WaveHeightM*rapidBuildFraction &&
				entry.WavePeriodS >= prev.WavePeriodS*rapidBuildFraction {
				set.RapidBuild = true
			}
		}
	}

	return set
}

// buildWaveSummary formats a human-readable sentence describing a day's
// swell height range, direction and period, derived from its hourly wave
// series so it stays numerically consistent with the wave graph. Ported
// unchanged from the pre-Phase-4 weather_tide.go implementation, retyped to
// []waveDayHourlyData.
func buildWaveSummary(hourly []waveDayHourlyData) string {
	minHeight := math.MaxFloat64
	maxHeight := -1.0
	periodTotal := 0.0
	periodCount := 0
	found := false

	for _, entry := range hourly {
		if entry.WaveHeightM < 0 {
			continue
		}
		found = true
		if entry.WaveHeightM < minHeight {
			minHeight = entry.WaveHeightM
		}
		if entry.WaveHeightM > maxHeight {
			maxHeight = entry.WaveHeightM
		}
		if entry.WavePeriodS > 0 {
			periodTotal += entry.WavePeriodS
			periodCount++
		}
	}

	if !found {
		return ""
	}

	heightPhrase := fmt.Sprintf("%.1f to %.1f m", minHeight, maxHeight)
	if math.Round(minHeight*10) == math.Round(maxHeight*10) {
		heightPhrase = fmt.Sprintf("around %.1f m", maxHeight)
	}

	if directionRange := waveDirectionRange(hourly); directionRange != "" {
		heightPhrase = fmt.Sprintf("%s from the %s", heightPhrase, directionRange)
	}

	summary := fmt.Sprintf("Significant wave height %s.", heightPhrase)
	if periodCount > 0 {
		periodRounded := int(math.Round(periodTotal / float64(periodCount)))
		summary = fmt.Sprintf("Significant wave height %s, with a period around %d sec.", heightPhrase, periodRounded)
	}

	return summary + steepnessClause(hourly)
}

// steepnessClause names the day's peak steepness, but only once the seas are
// steep enough for it to be worth saying.
//
// A rolling or building day says nothing, because most days are one of those
// and a sentence repeated on every benign forecast stops being read. The
// threshold for speaking up is the same one the band table draws: seas that
// are getting close to breaking, or are there already.
func steepnessClause(hourly []waveDayHourlyData) string {
	peak, ok := peakWaveSteepness(hourly)
	if !ok {
		return ""
	}

	band := waveSteepnessBand(peak)
	if band != waveSteepnessSteep && band != waveSteepnessBreaking {
		return ""
	}

	return fmt.Sprintf(" Steepness peaks at %s, %s seas.", formatSteepnessRatio(peak), band)
}

// waveDirectionRange describes how a day's swell direction shifts from
// morning to evening, e.g. "ENE-E" if it backs/veers between the first and
// last hourly readings, or a single compass point if it stays steady.
// Ported unchanged from the pre-Phase-4 weather_tide.go implementation,
// retyped to []waveDayHourlyData.
func waveDirectionRange(hourly []waveDayHourlyData) string {
	start := ""
	end := ""

	for _, entry := range hourly {
		if entry.WaveDirectionDeg < 0 {
			continue
		}
		dir := degreesToDirection(entry.WaveDirectionDeg)
		if start == "" {
			start = dir
		}
		end = dir
	}

	if start == "" {
		return ""
	}
	if end == start {
		return start
	}

	return fmt.Sprintf("%s-%s", start, end)
}

// --- HTTP response shapes ---

type waveHourlyResponse struct {
	Label            string  `json:"label"`
	HourOfDay        int     `json:"hour_of_day"`
	WaveHeightM      float64 `json:"wave_height_m"`
	WavePeriodS      float64 `json:"wave_period_s"`
	WaveDirectionDeg float64 `json:"wave_direction_deg"`
	WindWaveHeightM  float64 `json:"wind_wave_height_m"`
	SwellWaveHeightM float64 `json:"swell_wave_height_m"`

	WindWaveDirectionDeg  float64 `json:"wind_wave_direction_deg"`
	WindWavePeriodS       float64 `json:"wind_wave_period_s"`
	SwellWaveDirectionDeg float64 `json:"swell_wave_direction_deg"`
	SwellWavePeriodS      float64 `json:"swell_wave_period_s"`

	// SteepnessRatio is height over deep-water wavelength, nil when the hour
	// carries no period to derive it from. SteepnessBand is the matching band
	// name, empty for the same reason - an hour with no reading must not
	// render as the calmest band.
	SteepnessRatio *float64 `json:"steepness_ratio"`
	SteepnessBand  string   `json:"steepness_band"`
}

// waveDayResponse mirrors weatherForecastDayResponse's day-key/date/day-name
// shape so wave-day cards read consistently with weather-day cards once the
// frontend joins them by day_key in a later phase.
type waveDayResponse struct {
	DayKey      string               `json:"day_key"`
	Date        string               `json:"date"`
	DayName     string               `json:"day_name"`
	WaveSummary string               `json:"wave_summary"`
	HourlyWave  []waveHourlyResponse `json:"hourly_wave"`
	Indicators  waveDayIndicatorSet  `json:"indicators"`
}

type waveForecastResponse struct {
	Provider        string            `json:"provider"`
	Days            []waveDayResponse `json:"days"`
	SeaTemperatureF *float64          `json:"sea_temperature_f"`
	Cached          bool              `json:"cached"`
	UpdatedAt       string            `json:"updated_at"`
	TTLSeconds      int64             `json:"ttl_seconds"`
}

func mapWaveHourlyResponse(entries []waveDayHourlyData) []waveHourlyResponse {
	response := make([]waveHourlyResponse, 0, len(entries))
	for _, entry := range entries {
		mapped := waveHourlyResponse{
			Label:            entry.Label,
			HourOfDay:        entry.HourOfDay,
			WaveHeightM:      entry.WaveHeightM,
			WavePeriodS:      entry.WavePeriodS,
			WaveDirectionDeg: entry.WaveDirectionDeg,
			WindWaveHeightM:  entry.WindWaveHeightM,
			SwellWaveHeightM: entry.SwellWaveHeightM,

			WindWaveDirectionDeg:  entry.WindWaveDirectionDeg,
			WindWavePeriodS:       entry.WindWavePeriodS,
			SwellWaveDirectionDeg: entry.SwellWaveDirectionDeg,
			SwellWavePeriodS:      entry.SwellWavePeriodS,
		}
		if steepness, ok := waveSteepness(entry.WaveHeightM, entry.WavePeriodS); ok {
			mapped.SteepnessRatio = &steepness
			mapped.SteepnessBand = waveSteepnessBand(steepness)
		}
		response = append(response, mapped)
	}
	return response
}

// --- HTTP handlers ---

// waveForecast serves GET /api/wave-forecast: a multi-day wave forecast
// bucketed into local days, mirroring weatherForecast's shape and fail-fast
// behavior. No fake defaults: an unknown provider, a fetch failure, or an
// empty/all-past forecast is a 502, never placeholder wave/sea-temp data. A
// wave outage 502s here explicitly instead of ever being folded into the
// weather payload.
func waveForecast(c echo.Context) error {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	provider, configuredProvider, err := resolveWaveProvider(settingsPath)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}

	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}
	signalkURL := buildSignalKURL(address, port)
	if signalkURL == "" {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "SignalK URL is not configured"})
	}

	vesselState, vesselErr := fetchSignalKVesselState()
	if vesselErr != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to fetch vessel state: %v", vesselErr)})
	}
	if !hasUsableVesselPosition(vesselState.Latitude, vesselState.Longitude) {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "invalid vessel coordinates from SignalK"})
	}

	bundle, fetchErr := provider.FetchWaves(vesselState.Latitude, vesselState.Longitude, 10)
	if fetchErr != nil {
		log.Printf("wave provider %q error: %v", configuredProvider, fetchErr)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("wave provider %q unavailable: %v", configuredProvider, fetchErr)})
	}
	if len(bundle.Hourly) == 0 {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "wave forecast response was empty"})
	}

	localLocation := vesselLocalLocation(vesselState.Longitude)
	referenceDatetime := vesselState.Datetime
	if referenceDatetime.IsZero() {
		referenceDatetime = time.Now().UTC()
	}
	localTodayKey := referenceDatetime.In(localLocation).Format("2006-01-02")

	byDay := buildWaveHourlySeriesByDay(bundle.Hourly, localLocation)

	dayKeys := make([]string, 0, len(byDay))
	for dayKey := range byDay {
		if dayKey < localTodayKey {
			continue
		}
		dayKeys = append(dayKeys, dayKey)
	}
	sort.Strings(dayKeys)

	if len(dayKeys) == 0 {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "wave forecast response contained no days on or after today"})
	}

	dayResponses := make([]waveDayResponse, 0, len(dayKeys))
	for _, dayKey := range dayKeys {
		localDay, parseErr := time.ParseInLocation("2006-01-02", dayKey, localLocation)
		if parseErr != nil {
			continue
		}
		hourly := byDay[dayKey]
		dayResponses = append(dayResponses, waveDayResponse{
			DayKey:      dayKey,
			Date:        localDay.Format("Jan 2"),
			DayName:     localDay.Weekday().String(),
			WaveSummary: buildWaveSummary(hourly),
			HourlyWave:  mapWaveHourlyResponse(hourly),
			Indicators:  waveDayIndicators(hourly),
		})
	}

	var seaTemperatureF *float64
	if bundle.SeaTempC != nil {
		f := *bundle.SeaTempC*9/5 + 32
		seaTemperatureF = &f
	}

	response := waveForecastResponse{
		Provider:        configuredProvider,
		Days:            dayResponses,
		SeaTemperatureF: seaTemperatureF,
		Cached:          bundle.Cached,
		UpdatedAt:       bundle.CachedAt.UTC().Format(time.RFC3339),
		TTLSeconds:      provider.TTLSeconds(),
	}

	etag, etagErr := weakETagForJSON(response)
	if etagErr != nil {
		log.Printf("Failed to build wave forecast ETag: %v", etagErr)
	}
	return respondJSONWithETag(c, http.StatusOK, etag, response)
}
