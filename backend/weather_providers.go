// Package main: pluggable weather-provider host system.
//
// Weather forecasting used to be hardcoded against Apple WeatherKit directly
// in weather_tide.go. It is now a WASM plugin type, exactly like tides
// (tide_providers.go / wasm_tide_provider.go): a weatherProvider interface,
// a registry, HTTP handlers, and a WASM adapter (wasm_weather_provider.go).
// The host owns every unit conversion, condition-label formatting, local-day
// bucketing and derived summary; a plugin's only job is to answer
// fetch_forecast with SI-unit, RFC3339-timestamped data (see the guest
// contract doc comment below).
//
// Condition vocabulary: every provider's `condition` field (on current/,
// days[]/ and hourly[] alike) must be a WeatherKit-style camelCase or
// lowercase code that, once lowercased and stripped of "_"/"-", matches one
// of the keys formatWeatherConditionAt (backend/main.go) recognizes:
//
//	clear, cloudy, dusty, foggy, haze, mostlycloudy, partlycloudy,
//	mostlyclear (day/night special-cased to "Mostly Sunny"/"Mostly Clear"),
//	smoky, breezy, windy, drizzle, heavyrain, rain, snow, sleet,
//	freezingdrizzle, freezingrain, hail, mixedrainandsnow,
//	mixedrainandsleet, mixedsnowandsleet, thunderstorms, heavysnow, blizzard
//
// An unrecognized code is passed through verbatim (formatWeatherConditionAt's
// fallback), never masked into a fake "known" value.
//
// Guest contract (fetch_forecast), mirrored below by wasmFetchForecastOutput
// in wasm_weather_provider.go:
//
//	fetch_forecast({"lat": float64, "lon": float64, "days": int,
//	                "timezone": string}) -> {
//	  "current": {time, temperature_c, condition, wind_speed_ms, wind_gust_ms,
//	              wind_direction_deg, precipitation_chance_pct},
//	  "days": [{start, condition, temp_max_c, temp_min_c, wind_speed_ms,
//	            wind_gust_ms, wind_direction_deg, precipitation_chance_pct,
//	            sunrise, sunset}],
//	  "hourly": [{time, temperature_c, condition, wind_speed_ms, wind_gust_ms,
//	              wind_direction_deg, precipitation_chance_pct,
//	              precipitation_mm, uv_index, is_daylight}]
//	}
//
// "timezone" is an IANA zone identifier (from vesselLocalTimezoneName in
// weather_tide.go, e.g. "Etc/GMT-10" for a vessel at UTC+10). A plugin whose
// upstream API rolls hourly data up into daily summaries MUST pass it
// through, so days[] boundaries land on the vessel's local midnight - the
// host buckets and labels days in that same local zone, and a provider
// rolling up on a different boundary silently shifts every day summary and
// drops the record covering local midnight to the offset. See
// docs/adr/0035-weather-local-day-boundaries.md.
//
// All times are RFC3339 (UTC "Z" or an explicit offset - either is valid;
// the host does its own local-day bucketing via vesselLocalLocation, never
// trusting an offset embedded in a plugin's timestamp for that purpose). Any
// numeric field may be omitted/zero if the upstream API genuinely lacks it;
// the host treats an exactly-zero value for wind speed/gust/direction and
// temperature as "no data", the same -1-sentinel convention
// weatherHourlyEntryData/weatherHourlyWindData/weatherHourlyCloudData
// already used for WeatherKit's raw JSON.
package main

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// weatherCurrentPoint is a single point-in-time observation/forecast, SI
// units, as returned by a plugin's fetch_forecast "current" field.
type weatherCurrentPoint struct {
	Time                   time.Time
	TemperatureC           float64
	Condition              string
	WindSpeedMS            float64
	WindGustMS             float64
	WindDirectionDeg       float64
	PrecipitationChancePct float64
}

// weatherDayPoint is a single day's forecast summary, SI units, as returned
// by one entry of a plugin's fetch_forecast "days" field.
type weatherDayPoint struct {
	Start                  time.Time
	Condition              string
	TempMaxC               float64
	TempMinC               float64
	WindSpeedMS            float64
	WindGustMS             float64
	WindDirectionDeg       float64
	PrecipitationChancePct float64
	Sunrise                time.Time
	Sunset                 time.Time
}

// weatherHourPoint is a single hour's forecast, SI units, as returned by one
// entry of a plugin's fetch_forecast "hourly" field.
type weatherHourPoint struct {
	Time                   time.Time
	TemperatureC           float64
	Condition              string
	WindSpeedMS            float64
	WindGustMS             float64
	WindDirectionDeg       float64
	PrecipitationChancePct float64
	PrecipitationMM        float64
	UVIndex                float64
	IsDaylight             bool
}

// weatherForecastBundle is one provider round-trip's worth of data - the
// return value of weatherProvider.FetchForecast and of a WASM plugin's
// fetch_forecast, mapped to typed Go values. Cached/CachedAt describe the
// adapter's own cache bookkeeping (see wasmWeatherProvider.FetchForecast),
// independent of any timestamp inside Current/Days/Hourly.
type weatherForecastBundle struct {
	Current  weatherCurrentPoint
	Days     []weatherDayPoint
	Hourly   []weatherHourPoint
	Cached   bool
	CachedAt time.Time
}

// weatherProvider is the interface implemented by each pluggable weather
// data source. Both reference providers (open-meteo, weatherkit) are WASM
// plugins - there is no native built-in, mirroring tideProvider's shape but
// without SearchStations (weather has no station concept).
type weatherProvider interface {
	ID() string
	Name() string
	Description() string
	TTLSeconds() int64
	FetchForecast(lat, lon float64, days int, timezone string) (weatherForecastBundle, error)
}

var weatherProviderRegistry = map[string]weatherProvider{}
var weatherProviderOrder []string

func registerWeatherProvider(p weatherProvider) {
	weatherProviderRegistry[p.ID()] = p
	weatherProviderOrder = append(weatherProviderOrder, p.ID())
}

func getWeatherProvider(id string) (weatherProvider, bool) {
	p, ok := weatherProviderRegistry[id]
	return p, ok
}

type weatherProviderInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// weatherProvidersHandler serves GET /api/weather-providers, mirroring
// tideProvidersHandler.
func weatherProvidersHandler(c echo.Context) error {
	result := make([]weatherProviderInfo, 0, len(weatherProviderOrder))
	for _, id := range weatherProviderOrder {
		if provider, ok := weatherProviderRegistry[id]; ok {
			result = append(result, weatherProviderInfo{ID: provider.ID(), Name: provider.Name(), Description: provider.Description()})
		}
	}
	return c.JSON(http.StatusOK, result)
}

// defaultWeatherProviderID is used when ui.weather_provider is unset in
// settings.yaml, matching the "keyless, so fresh installs get useful
// dashboards immediately" default from the design doc.
const defaultWeatherProviderID = "open-meteo"

// resolveWeatherProvider reads ui.weather_provider from settingsPath
// (defaulting to defaultWeatherProviderID) and resolves it against the
// registry, mirroring tideToday's provider-resolution idiom
// (weather_tide.go's tideToday). Returns a clear, actionable error - never a
// fallback provider - when the configured id isn't registered, since an
// unregistered provider most likely means the plugin hasn't been installed
// yet (expected until Phase 5/8 ship the reference plugins).
func resolveWeatherProvider(settingsPath string) (weatherProvider, string, error) {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read settings: %w", err)
	}

	uiMap, _ := settings["ui"].(map[string]any)
	configuredProvider := strings.TrimSpace(coerceString(uiMap["weather_provider"]))
	if configuredProvider == "" {
		configuredProvider = defaultWeatherProviderID
	}

	provider, ok := getWeatherProvider(configuredProvider)
	if !ok {
		return nil, configuredProvider, fmt.Errorf("unknown weather provider configured: %q (is the plugin installed in plugins/weather?)", configuredProvider)
	}

	return provider, configuredProvider, nil
}

// --- moon phase ---

// moonPhaseEpoch is a real, independently documented new moon
// (2000-01-06T18:14:00Z) used as the reference point for the synodic-cycle
// approximation below - a commonly used epoch for this kind of lightweight
// moon-phase calculation.
var moonPhaseEpoch = time.Date(2000, 1, 6, 18, 14, 0, 0, time.UTC)

// synodicMonthDays is the mean length of a synodic (new-moon-to-new-moon)
// month in days.
const synodicMonthDays = 29.530588853

// moonPhaseNames are the 8 equal-width buckets moonPhase can return, in
// cycle order starting at new moon. Verified against
// frontend/src/components/forecast-drawer.tsx's MOON_PHASE_LABELS - do not
// rename or reorder without updating that map too.
var moonPhaseNames = [8]string{
	"new", "waxingCrescent", "firstQuarter", "waxingGibbous",
	"full", "waningGibbous", "lastQuarter", "waningCrescent",
}

// moonPhase approximates the moon phase at t using a synodic-cycle
// calculation: days since a reference new moon, mod the mean synodic month,
// bucketed into 8 equal-width slices. This is intentionally a "close enough
// for a dashboard icon" approximation, not an ephemeris - real synodic
// months vary by roughly +/-13 hours around the mean, so a date near a
// bucket boundary can land in the neighboring bucket. +/-1 day of tolerance
// at bucket boundaries is expected and acceptable for this use case.
func moonPhase(t time.Time) string {
	daysSinceEpoch := t.UTC().Sub(moonPhaseEpoch).Hours() / 24
	cycles := daysSinceEpoch / synodicMonthDays
	frac := cycles - math.Floor(cycles)
	if frac < 0 {
		frac += 1
	}

	idx := int(math.Round(frac*8)) % 8
	if idx < 0 {
		idx += 8
	}
	return moonPhaseNames[idx]
}

// --- host-side derivation: unit conversion, day bucketing, hourly strip ---

// sentinelSpeedKts converts an SI m/s value to knots, treating an exactly-
// zero input as "no data" (-1 sentinel), matching the existing
// weatherHourlyWindData/weatherHourlyEntryData -1-for-missing convention
// that buildWindSummary/summarizeHourlyForecast's `< 0` skip-checks rely on.
func sentinelSpeedKts(valueMS float64) float64 {
	if valueMS == 0 {
		return -1
	}
	return valueMS * metersPerSecondToKnots
}

// sentinelGustKts converts an SI gust m/s value to knots, falling back to
// the already-converted sustained speed when the gust is missing (zero) -
// same fallback the old WeatherKit-parsing code applied.
func sentinelGustKts(gustMS float64, sustainedKts float64) float64 {
	if gustMS != 0 {
		return gustMS * metersPerSecondToKnots
	}
	if sustainedKts >= 0 {
		return sustainedKts
	}
	return -1
}

// sentinelDirection converts a degrees value into (compass string, degrees),
// treating an exactly-zero input as "no data" - see this file's top doc
// comment for why the flat plugin contract can't distinguish "true north"
// from "omitted".
func sentinelDirection(degrees float64) (string, float64) {
	if degrees == 0 {
		return "—", -1
	}
	return degreesToDirection(degrees), degrees
}

// sentinelTemperatureF converts an SI Celsius value to Fahrenheit, treating
// an exactly-zero input as "no data" (-1 sentinel).
func sentinelTemperatureF(valueC float64) float64 {
	if valueC == 0 {
		return -1
	}
	return valueC*9/5 + 32
}

// hasUsableVesselPosition reports whether a SignalK-derived position is safe
// to hand to a geolocated provider.
//
// A plain lat/lon range check is not enough. fetchSignalKVesselState seeds
// its state with Latitude/Longitude of -1, and resolveGNSSPosition
// (gnss_validation.go) returns -1,-1 when GNSS is untrusted and there is no
// prior trusted fix - both syntactically valid coordinates that sit in the
// Gulf of Guinea. The repo's own weather cache accumulated a real
// "-1.0,-1.0,1" entry that way: a forecast for the wrong hemisphere,
// fetched, cached and shown as if it were local weather.
//
// 0,0 (Null Island) is rejected for the same reason - it is the classic
// "unset coordinates" value, and no vessel this system serves is moored
// there. Genuine positions that merely contain a -1 or 0 component (e.g.
// 51.5,-1.0) stay usable; only the exact sentinel pairs are refused.
func hasUsableVesselPosition(latitude, longitude float64) bool {
	if latitude < -90 || latitude > 90 || longitude < -180 || longitude > 180 {
		return false
	}
	if latitude == -1 && longitude == -1 {
		return false
	}
	if latitude == 0 && longitude == 0 {
		return false
	}
	return true
}

// sentinelPrecipitationPct normalizes a precipitation-chance percentage.
//
// It deliberately breaks the "exactly zero means no data" convention the
// sentinels above use, because for precipitation zero is a legitimate and
// very common reading - a dry day genuinely is 0%. Absence must therefore be
// signalled explicitly by the plugin as a negative value, which this maps to
// the -1 sentinel; a real 0 survives untouched.
//
// Conflating the two is precisely what let a provider that reported nothing
// render as a confident "0% precip" during actual rainfall. Consumers treat
// a negative as "unavailable" and must not display it as a number. See
// docs/adr/0035-weather-local-day-boundaries.md.
func sentinelPrecipitationPct(pct float64) float64 {
	if pct < 0 {
		return -1
	}
	if pct > 100 {
		return 100
	}
	return pct
}

// buildHourlySeriesByDay buckets a provider's flat hourly points into
// per-day (local date) wind/precipitation/UV/cloud series, keyed by
// "2006-01-02" in localLocation - the typed-contract replacement for the old
// buildDailyWindSeries/buildDailyPrecipitationSeries/buildDailyUVSeries/
// buildDailyCloudSeries quartet, which parsed WeatherKit's raw JSON
// directly. Label/HourOfDay preserve their old meaning (local "3PM"-style
// hour string / local hour 0-23).
func buildHourlySeriesByDay(hourly []weatherHourPoint, localLocation *time.Location) (
	map[string][]weatherHourlyWindData,
	map[string][]weatherHourlyPrecipitationData,
	map[string][]weatherHourlyUVData,
	map[string][]weatherHourlyCloudData,
) {
	windSeries := make(map[string][]weatherHourlyWindData)
	precipSeries := make(map[string][]weatherHourlyPrecipitationData)
	uvSeries := make(map[string][]weatherHourlyUVData)
	cloudSeries := make(map[string][]weatherHourlyCloudData)

	for _, hp := range hourly {
		if hp.Time.IsZero() {
			continue
		}
		localTime := hp.Time.In(localLocation)
		dayKey := localTime.Format("2006-01-02")
		label := localTime.Format("3PM")
		hourOfDay := localTime.Hour()

		windSpeedKts := sentinelSpeedKts(hp.WindSpeedMS)
		windGustKts := sentinelGustKts(hp.WindGustMS, windSpeedKts)
		windDirection, windDirectionDeg := sentinelDirection(hp.WindDirectionDeg)

		windSeries[dayKey] = append(windSeries[dayKey], weatherHourlyWindData{
			Label:            label,
			HourOfDay:        hourOfDay,
			WindSpeedKts:     windSpeedKts,
			WindGustKts:      windGustKts,
			WindDirection:    windDirection,
			WindDirectionDeg: windDirectionDeg,
		})

		precipSeries[dayKey] = append(precipSeries[dayKey], weatherHourlyPrecipitationData{
			Label:                    label,
			HourOfDay:                hourOfDay,
			PrecipitationChancePct:   hp.PrecipitationChancePct,
			PrecipitationIntensityMm: math.Max(0, hp.PrecipitationMM),
		})

		uvSeries[dayKey] = append(uvSeries[dayKey], weatherHourlyUVData{
			Label:   label,
			UVIndex: math.Max(0, hp.UVIndex),
		})

		condition := "Unknown"
		if strings.TrimSpace(hp.Condition) != "" {
			condition = formatWeatherConditionAt(hp.Condition, localTime, localLocation, false)
		}
		cloudSeries[dayKey] = append(cloudSeries[dayKey], weatherHourlyCloudData{
			Label:        label,
			HourOfDay:    hourOfDay,
			Condition:    condition,
			TemperatureF: sentinelTemperatureF(hp.TemperatureC),
			IsDaylight:   hp.IsDaylight,
		})
	}

	return windSeries, precipSeries, uvSeries, cloudSeries
}

// buildDayData assembles a weatherForecastDayData from a plugin's per-day
// summary plus that day's already-bucketed hourly series - the typed-
// contract replacement for the day-building loop inside the old
// fetchWeatherKitForecastBundleData. referenceDatetime/localLocation drive
// the condition day/night lookup the same way the old code did.
func buildDayData(
	dayPoint weatherDayPoint,
	referenceDatetime time.Time,
	localLocation *time.Location,
	windSeries []weatherHourlyWindData,
	precipSeries []weatherHourlyPrecipitationData,
	uvSeries []weatherHourlyUVData,
	cloudSeries []weatherHourlyCloudData,
) weatherForecastDayData {
	localStart := dayPoint.Start.In(localLocation)

	condition := "Unknown"
	if strings.TrimSpace(dayPoint.Condition) != "" {
		condition = formatWeatherConditionAt(dayPoint.Condition, referenceDatetime, localLocation, true)
	}

	windSpeedKts := sentinelSpeedKts(dayPoint.WindSpeedMS)
	windGustKts := sentinelGustKts(dayPoint.WindGustMS, windSpeedKts)
	windDirection, _ := sentinelDirection(dayPoint.WindDirectionDeg)

	sunriseTime := ""
	if !dayPoint.Sunrise.IsZero() {
		sunriseTime = dayPoint.Sunrise.In(localLocation).Format("3:04PM")
	}
	sunsetTime := ""
	if !dayPoint.Sunset.IsZero() {
		sunsetTime = dayPoint.Sunset.In(localLocation).Format("3:04PM")
	}

	return weatherForecastDayData{
		Date:                 localStart.Format("Jan 2"),
		DayName:              localStart.Weekday().String(),
		Condition:            condition,
		HighTempF:            sentinelTemperatureF(dayPoint.TempMaxC),
		LowTempF:             sentinelTemperatureF(dayPoint.TempMinC),
		WindSpeedKts:         windSpeedKts,
		WindGustKts:          windGustKts,
		WindDirection:        windDirection,
		WindSummary:          buildWindSummary(windSeries),
		PrecipitationPct:     sentinelPrecipitationPct(dayPoint.PrecipitationChancePct),
		PrecipitationSummary: buildPrecipitationSummary(precipSeries),
		SunriseTime:          sunriseTime,
		SunsetTime:           sunsetTime,
		MoonPhase:            moonPhase(dayPoint.Start),
		HourlyWind:           windSeries,
		HourlyPrecip:         precipSeries,
		HourlyUV:             uvSeries,
		HourlyCloud:          cloudSeries,
	}
}

// buildWeatherHourlyStrip builds the "next up to 12 hours" strip for the
// forecast drawer's today view, with a synthetic "Sunset" entry spliced in
// at the right spot - the typed-contract replacement for the old
// buildWeatherHourlyEntries, operating on a provider's flat hourly points
// instead of raw WeatherKit JSON. Preserves the original's exact semantics:
// entries before the current local hour are skipped, the current hour is
// labeled "Now", a sunset entry (Kind: "sunset", all data sentinel/absent)
// is inserted once in time order (or appended at the end as a fallback if
// no hour after it was found), and the strip is capped at 12 entries.
func buildWeatherHourlyStrip(hourly []weatherHourPoint, referenceDatetime time.Time, localLocation *time.Location, localTodayKey string, sunsetAt time.Time) []weatherHourlyEntryData {
	referenceLocal := referenceDatetime.In(localLocation)
	referenceHour := referenceLocal.Truncate(time.Hour)
	entries := make([]weatherHourlyEntryData, 0, 12)
	insertedSunset := false

	for _, hp := range hourly {
		if len(entries) >= 12 {
			break
		}
		if hp.Time.IsZero() {
			continue
		}
		localTime := hp.Time.In(localLocation)
		if localTime.Before(referenceHour) {
			continue
		}

		if !insertedSunset && !sunsetAt.IsZero() && sunsetAt.After(referenceLocal) && localTime.After(sunsetAt) {
			entries = append(entries, weatherHourlyEntryData{
				Label: sunsetAt.Format("3:04PM"), Condition: "Sunset",
				TemperatureF: -1, WindSpeedKts: -1, WindGustKts: -1,
				WindDirection: "—", WindDirectionDeg: -1, Kind: "sunset",
			})
			insertedSunset = true
			if len(entries) >= 12 {
				break
			}
		}

		condition := "Unknown"
		if strings.TrimSpace(hp.Condition) != "" {
			condition = formatWeatherConditionAt(hp.Condition, localTime, localLocation, false)
		}

		windSpeedKts := sentinelSpeedKts(hp.WindSpeedMS)
		windGustKts := sentinelGustKts(hp.WindGustMS, windSpeedKts)
		windDirection, windDirectionDeg := sentinelDirection(hp.WindDirectionDeg)

		label := localTime.Format("3PM")
		if localTime.Equal(referenceHour) {
			label = "Now"
		}

		entries = append(entries, weatherHourlyEntryData{
			Label: label, Condition: condition, TemperatureF: sentinelTemperatureF(hp.TemperatureC),
			WindSpeedKts: windSpeedKts, WindGustKts: windGustKts,
			WindDirection: windDirection, WindDirectionDeg: windDirectionDeg, Kind: "forecast",
		})
	}

	if !insertedSunset && !sunsetAt.IsZero() && sunsetAt.Format("2006-01-02") == localTodayKey && sunsetAt.After(referenceLocal) && len(entries) < 12 {
		entries = append(entries, weatherHourlyEntryData{Label: sunsetAt.Format("3:04PM"), Condition: "Sunset", TemperatureF: -1, Kind: "sunset"})
	}

	return entries
}

// --- HTTP response shapes ---

type weatherTodayResponse struct {
	Datetime         string  `json:"datetime"`
	TemperatureF     float64 `json:"temperature_f"`
	Condition        string  `json:"condition"`
	WindSpeedKts     float64 `json:"wind_speed_kts"`
	WindGustKts      float64 `json:"wind_gust_kts"`
	WindDirection    string  `json:"wind_direction"`
	PrecipitationPct float64 `json:"precipitation_pct"`
	Provider         string  `json:"provider"`
	Cached           bool    `json:"cached"`
	UpdatedAt        string  `json:"updated_at"`
	TTLSeconds       int64   `json:"ttl_seconds"`
}

type weatherTodayETagData struct {
	TemperatureF     float64 `json:"temperature_f"`
	Condition        string  `json:"condition"`
	WindSpeedKts     float64 `json:"wind_speed_kts"`
	WindGustKts      float64 `json:"wind_gust_kts"`
	WindDirection    string  `json:"wind_direction"`
	PrecipitationPct float64 `json:"precipitation_pct"`
	Provider         string  `json:"provider"`
}

type weatherHourlyEntryResponse struct {
	Label            string  `json:"label"`
	Condition        string  `json:"condition"`
	TemperatureF     float64 `json:"temperature_f"`
	WindSpeedKts     float64 `json:"wind_speed_kts"`
	WindGustKts      float64 `json:"wind_gust_kts"`
	WindDirection    string  `json:"wind_direction"`
	WindDirectionDeg float64 `json:"wind_direction_deg"`
	Kind             string  `json:"kind"`
}

type weatherHourlyWindResponse struct {
	Label            string  `json:"label"`
	HourOfDay        int     `json:"hour_of_day"`
	WindSpeedKts     float64 `json:"wind_speed_kts"`
	WindGustKts      float64 `json:"wind_gust_kts"`
	WindDirection    string  `json:"wind_direction"`
	WindDirectionDeg float64 `json:"wind_direction_deg"`
}

type weatherHourlyPrecipitationResponse struct {
	Label                    string  `json:"label"`
	HourOfDay                int     `json:"hour_of_day"`
	PrecipitationChancePct   float64 `json:"precipitation_chance_pct"`
	PrecipitationIntensityMm float64 `json:"precipitation_intensity_mm"`
}

type weatherHourlyUVResponse struct {
	Label   string  `json:"label"`
	UVIndex float64 `json:"uv_index"`
}

type weatherHourlyCloudResponse struct {
	Label        string  `json:"label"`
	HourOfDay    int     `json:"hour_of_day"`
	Condition    string  `json:"condition"`
	TemperatureF float64 `json:"temperature_f"`
	IsDaylight   bool    `json:"is_daylight"`
}

// weatherForecastDayResponse mirrors weatherForecastDayData for JSON output.
// Unlike the pre-Phase-3 shape, it carries no wave fields (waves get their
// own /api/wave-forecast endpoint in a later phase) and gains DayKey - the
// vessel-local "2006-01-02" join key a future wave-forecast response will
// match against.
type weatherForecastDayResponse struct {
	DayKey               string                               `json:"day_key"`
	Date                 string                               `json:"date"`
	DayName              string                               `json:"day_name"`
	Condition            string                               `json:"condition"`
	HighTempF            float64                              `json:"high_temp_f"`
	LowTempF             float64                              `json:"low_temp_f"`
	WindSpeedKts         float64                              `json:"wind_speed_kts"`
	WindGustKts          float64                              `json:"wind_gust_kts"`
	WindDirection        string                               `json:"wind_direction"`
	WindSummary          string                               `json:"wind_summary"`
	PrecipitationPct     float64                              `json:"precipitation_pct"`
	PrecipitationSummary string                               `json:"precipitation_summary"`
	SunriseTime          string                               `json:"sunrise_time"`
	SunsetTime           string                               `json:"sunset_time"`
	MoonPhase            string                               `json:"moon_phase"`
	HourlyWind           []weatherHourlyWindResponse          `json:"hourly_wind"`
	HourlyPrecip         []weatherHourlyPrecipitationResponse `json:"hourly_precip"`
	HourlyUV             []weatherHourlyUVResponse            `json:"hourly_uv"`
	HourlyCloud          []weatherHourlyCloudResponse         `json:"hourly_cloud"`
}

type weatherForecastResponse struct {
	Days        []weatherForecastDayResponse `json:"days"`
	HourlyToday []weatherHourlyEntryResponse `json:"hourly_today"`
	Summary     string                       `json:"summary"`
	Provider    string                       `json:"provider"`
	Cached      bool                         `json:"cached"`
	UpdatedAt   string                       `json:"updated_at"`
	TTLSeconds  int64                        `json:"ttl_seconds"`
}

func mapWeatherHourlyWindResponse(entries []weatherHourlyWindData) []weatherHourlyWindResponse {
	response := make([]weatherHourlyWindResponse, 0, len(entries))
	for _, entry := range entries {
		response = append(response, weatherHourlyWindResponse{
			Label:            entry.Label,
			HourOfDay:        entry.HourOfDay,
			WindSpeedKts:     entry.WindSpeedKts,
			WindGustKts:      entry.WindGustKts,
			WindDirection:    entry.WindDirection,
			WindDirectionDeg: entry.WindDirectionDeg,
		})
	}
	return response
}

func mapWeatherHourlyPrecipitationResponse(entries []weatherHourlyPrecipitationData) []weatherHourlyPrecipitationResponse {
	response := make([]weatherHourlyPrecipitationResponse, 0, len(entries))
	for _, entry := range entries {
		response = append(response, weatherHourlyPrecipitationResponse{
			Label:                    entry.Label,
			HourOfDay:                entry.HourOfDay,
			PrecipitationChancePct:   sentinelPrecipitationPct(entry.PrecipitationChancePct),
			PrecipitationIntensityMm: entry.PrecipitationIntensityMm,
		})
	}
	return response
}

func mapWeatherHourlyUVResponse(entries []weatherHourlyUVData) []weatherHourlyUVResponse {
	response := make([]weatherHourlyUVResponse, 0, len(entries))
	for _, entry := range entries {
		response = append(response, weatherHourlyUVResponse{Label: entry.Label, UVIndex: entry.UVIndex})
	}
	return response
}

func mapWeatherHourlyCloudResponse(entries []weatherHourlyCloudData) []weatherHourlyCloudResponse {
	response := make([]weatherHourlyCloudResponse, 0, len(entries))
	for _, entry := range entries {
		response = append(response, weatherHourlyCloudResponse{
			Label:        entry.Label,
			HourOfDay:    entry.HourOfDay,
			Condition:    entry.Condition,
			TemperatureF: entry.TemperatureF,
			IsDaylight:   entry.IsDaylight,
		})
	}
	return response
}

func mapWeatherHourlyEntryResponse(entries []weatherHourlyEntryData) []weatherHourlyEntryResponse {
	response := make([]weatherHourlyEntryResponse, 0, len(entries))
	for _, entry := range entries {
		response = append(response, weatherHourlyEntryResponse{
			Label:            entry.Label,
			Condition:        entry.Condition,
			TemperatureF:     entry.TemperatureF,
			WindSpeedKts:     entry.WindSpeedKts,
			WindGustKts:      entry.WindGustKts,
			WindDirection:    entry.WindDirection,
			WindDirectionDeg: entry.WindDirectionDeg,
			Kind:             entry.Kind,
		})
	}
	return response
}

func mapWeatherForecastDayResponse(day weatherForecastDayData, dayKey string) weatherForecastDayResponse {
	return weatherForecastDayResponse{
		DayKey:               dayKey,
		Date:                 day.Date,
		DayName:              day.DayName,
		Condition:            day.Condition,
		HighTempF:            day.HighTempF,
		LowTempF:             day.LowTempF,
		WindSpeedKts:         day.WindSpeedKts,
		WindGustKts:          day.WindGustKts,
		WindDirection:        day.WindDirection,
		WindSummary:          day.WindSummary,
		PrecipitationPct:     day.PrecipitationPct,
		PrecipitationSummary: day.PrecipitationSummary,
		SunriseTime:          day.SunriseTime,
		SunsetTime:           day.SunsetTime,
		MoonPhase:            day.MoonPhase,
		HourlyWind:           mapWeatherHourlyWindResponse(day.HourlyWind),
		HourlyPrecip:         mapWeatherHourlyPrecipitationResponse(day.HourlyPrecip),
		HourlyUV:             mapWeatherHourlyUVResponse(day.HourlyUV),
		HourlyCloud:          mapWeatherHourlyCloudResponse(day.HourlyCloud),
	}
}

// --- HTTP handlers ---

// weatherToday serves GET /api/weather-today from the configured weather
// provider's current-conditions point. No fake defaults: an unknown
// provider or a fetch failure is a 502 with a clear message, never a
// plausible-looking placeholder reading. sea_temperature_f is dropped
// entirely (moves to the wave endpoint in a later phase); high/low
// temperature is dropped too (that belongs to the day bundle in
// /api/weather-forecast's days[0], not a point-in-time "today" reading).
func weatherToday(c echo.Context) error {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	provider, configuredProvider, err := resolveWeatherProvider(settingsPath)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}

	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}
	signalkURL := buildSignalKURL(address, port)
	vesselPath := getEnv("SIGNALK_VESSEL_PATH", "/signalk/v1/api/vessels/self")

	vesselState, vesselErr := fetchSignalKVesselState(signalkURL, vesselPath)
	if vesselErr != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to fetch vessel state: %v", vesselErr)})
	}
	if !hasUsableVesselPosition(vesselState.Latitude, vesselState.Longitude) {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "invalid vessel coordinates from SignalK"})
	}

	bundle, fetchErr := provider.FetchForecast(vesselState.Latitude, vesselState.Longitude, 1, vesselLocalTimezoneName(vesselState.Longitude))
	if fetchErr != nil {
		log.Printf("weather provider %q error: %v", configuredProvider, fetchErr)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("weather provider %q unavailable: %v", configuredProvider, fetchErr)})
	}

	localLocation := vesselLocalLocation(vesselState.Longitude)

	condition := "Unknown"
	if strings.TrimSpace(bundle.Current.Condition) != "" {
		observedAt := bundle.Current.Time
		if observedAt.IsZero() {
			observedAt = time.Now().UTC()
		}
		condition = formatWeatherConditionAt(bundle.Current.Condition, observedAt, localLocation, false)
	}

	windSpeedKts := sentinelSpeedKts(bundle.Current.WindSpeedMS)
	windGustKts := sentinelGustKts(bundle.Current.WindGustMS, windSpeedKts)
	windDirection, _ := sentinelDirection(bundle.Current.WindDirectionDeg)

	datetime := bundle.Current.Time
	if datetime.IsZero() {
		datetime = time.Now().UTC()
	}

	response := weatherTodayResponse{
		Datetime:         datetime.UTC().Format(time.RFC3339),
		TemperatureF:     sentinelTemperatureF(bundle.Current.TemperatureC),
		Condition:        condition,
		WindSpeedKts:     windSpeedKts,
		WindGustKts:      windGustKts,
		WindDirection:    windDirection,
		PrecipitationPct: bundle.Current.PrecipitationChancePct,
		Provider:         configuredProvider,
		Cached:           bundle.Cached,
		UpdatedAt:        bundle.CachedAt.UTC().Format(time.RFC3339),
		TTLSeconds:       provider.TTLSeconds(),
	}

	etag, etagErr := weakETagForJSON(weatherTodayETagData{
		TemperatureF:     response.TemperatureF,
		Condition:        response.Condition,
		WindSpeedKts:     response.WindSpeedKts,
		WindGustKts:      response.WindGustKts,
		WindDirection:    response.WindDirection,
		PrecipitationPct: response.PrecipitationPct,
		Provider:         response.Provider,
	})
	if etagErr != nil {
		log.Printf("Failed to build weather ETag: %v", etagErr)
	}
	return respondJSONWithETag(c, http.StatusOK, etag, response)
}

// weatherForecast serves GET /api/weather-forecast: a multi-day forecast
// bucketed into local days, plus today's next-12-hours strip. No fake
// defaults: an unknown provider, a fetch failure, or an empty/all-past
// forecast is a 502, never the old defaultWeatherForecastDays placeholder.
func weatherForecast(c echo.Context) error {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	provider, configuredProvider, err := resolveWeatherProvider(settingsPath)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}

	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}
	signalkURL := buildSignalKURL(address, port)
	vesselPath := getEnv("SIGNALK_VESSEL_PATH", "/signalk/v1/api/vessels/self")
	if signalkURL == "" {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "SignalK URL is not configured"})
	}

	vesselState, vesselErr := fetchSignalKVesselState(signalkURL, vesselPath)
	if vesselErr != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to fetch vessel state: %v", vesselErr)})
	}
	if !hasUsableVesselPosition(vesselState.Latitude, vesselState.Longitude) {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "invalid vessel coordinates from SignalK"})
	}

	bundle, forecastErr := provider.FetchForecast(vesselState.Latitude, vesselState.Longitude, 10, vesselLocalTimezoneName(vesselState.Longitude))
	if forecastErr != nil {
		log.Printf("weather provider %q forecast error: %v", configuredProvider, forecastErr)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("weather provider %q unavailable: %v", configuredProvider, forecastErr)})
	}
	if len(bundle.Days) == 0 {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "weather forecast response was empty"})
	}

	localLocation := vesselLocalLocation(vesselState.Longitude)
	referenceDatetime := vesselState.Datetime
	if referenceDatetime.IsZero() {
		referenceDatetime = time.Now().UTC()
	}
	localTodayKey := referenceDatetime.In(localLocation).Format("2006-01-02")

	windByDay, precipByDay, uvByDay, cloudByDay := buildHourlySeriesByDay(bundle.Hourly, localLocation)

	var sunsetAt time.Time
	dayResponses := make([]weatherForecastDayResponse, 0, len(bundle.Days))
	for _, dp := range bundle.Days {
		if dp.Start.IsZero() {
			continue
		}
		dayKey := dp.Start.In(localLocation).Format("2006-01-02")
		if dayKey < localTodayKey {
			continue
		}
		if dayKey == localTodayKey && !dp.Sunset.IsZero() {
			sunsetAt = dp.Sunset.In(localLocation)
		}

		dayData := buildDayData(dp, referenceDatetime, localLocation, windByDay[dayKey], precipByDay[dayKey], uvByDay[dayKey], cloudByDay[dayKey])
		dayResponses = append(dayResponses, mapWeatherForecastDayResponse(dayData, dayKey))
	}
	if len(dayResponses) == 0 {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "weather forecast response contained no days on or after today"})
	}

	hourlyToday := buildWeatherHourlyStrip(bundle.Hourly, referenceDatetime, localLocation, localTodayKey, sunsetAt)
	summary := summarizeHourlyForecast(hourlyToday)

	response := weatherForecastResponse{
		Days:        dayResponses,
		HourlyToday: mapWeatherHourlyEntryResponse(hourlyToday),
		Summary:     summary,
		Provider:    configuredProvider,
		Cached:      bundle.Cached,
		UpdatedAt:   bundle.CachedAt.UTC().Format(time.RFC3339),
		TTLSeconds:  provider.TTLSeconds(),
	}

	etag, etagErr := weakETagForJSON(response)
	if etagErr != nil {
		log.Printf("Failed to build weather forecast ETag: %v", etagErr)
	}
	return respondJSONWithETag(c, http.StatusOK, etag, response)
}
