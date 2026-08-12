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
//	              wind_wave_height_m, swell_wave_height_m}],
//	  "sea_surface_temperature_c": float64 (optional)
//	}
//
// All times are RFC3339. sea_surface_temperature_c is OPTIONAL: a plugin or
// upstream model that genuinely has no sea-temperature data for a location
// omits the field entirely (mapped to a nil *float64 here, never a masked
// zero/placeholder value) - see wasm_wave_provider.go's
// wasmFetchWavesOutput.SeaSurfaceTemperatureC.
//
// Reference implementation context for whoever builds
// docs/examples/wave-plugins/open-meteo-marine (future phase, not this
// one): the wave forecast fetch should pin Open-Meteo Marine's
// `models=ncep_gfswave025` param (NOAA's GFS-Wave / WaveWatch III model) so
// swell height, period and direction line up with other WaveWatch
// III-based swell forecasts for a given coastline - Open-Meteo's default
// "best_match" model blend runs noticeably lower/different. The separate
// sea-surface-temperature fetch should deliberately OMIT that `models=`
// param - the wave-specific NOAA GFS-Wave model does not carry sea surface
// temperature at all (returns null/"undefined" units); only Open-Meteo's
// default model blend does.
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
		})
	}

	return series
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

	if periodCount > 0 {
		periodRounded := int(math.Round(periodTotal / float64(periodCount)))
		return fmt.Sprintf("Significant wave height %s, with a period around %d sec.", heightPhrase, periodRounded)
	}

	return fmt.Sprintf("Significant wave height %s.", heightPhrase)
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
		response = append(response, waveHourlyResponse{
			Label:            entry.Label,
			HourOfDay:        entry.HourOfDay,
			WaveHeightM:      entry.WaveHeightM,
			WavePeriodS:      entry.WavePeriodS,
			WaveDirectionDeg: entry.WaveDirectionDeg,
			WindWaveHeightM:  entry.WindWaveHeightM,
			SwellWaveHeightM: entry.SwellWaveHeightM,
		})
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
