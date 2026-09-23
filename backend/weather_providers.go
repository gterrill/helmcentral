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
//	              precipitation_mm, uv_index, is_daylight,
//	              humidity_pct, visibility_m}],
//	  "next_hour": [{time, precipitation_chance_pct, precipitation_mm_per_h}],
//	  "next_hour_source": "nowcast" | "hourly"
//	}
//
// "next_hour" is an OPTIONAL minute-by-minute (or whatever finer-than-hourly
// resolution the provider has) nowcast, in time order, covering roughly the
// next hour from whenever the provider fetched it. Absent or empty means
// this provider has no nowcast coverage here - a WeatherKit request outside
// its forecastNextHour region, or a provider (like Open-Meteo without its
// minutely_15 block requested) that never has one - and that must be
// surfaced as "no nowcast", never backfilled from hourly/daily data inside
// the plugin itself (fallback policy: a provider that doesn't supply
// next-hour data means "no nowcast", shown honestly by the host/frontend,
// never faked from something else without saying so). precipitation_chance_pct
// follows the same negative-is-absent convention as the other
// precipitation_chance_pct fields in this contract (a provider whose
// nowcast has intensity but no probability at this resolution, e.g.
// Open-Meteo's minutely_15, sends a negative chance rather than inventing
// one). The host does not require or infer any particular cadence: it
// infers the step (e.g. 1 minute for WeatherKit, 15 minutes for Open-Meteo)
// from the gap between the first two points, and a single point has no
// inferrable step at all. See buildWeatherNextHourResponse below.
//
// Each next_hour point's "time" marks the START of the interval it
// describes: a point covers [time, time+step) counting FORWARD from its own
// timestamp, matching how the host and frontend (lib/nowcast.ts's
// buildNowcastBars) both read it. This is not every upstream API's own
// convention - Open-Meteo documents minutely_15.precipitation as a
// "preceding 15 minutes sum" (the value at timestamp T covers
// [T-step, T), backward from T) - so a plugin whose source data is a
// trailing/preceding-window statistic must shift its emitted "time" back by
// one step before it reaches this contract, not pass the upstream timestamp
// through unchanged. See docs/examples/weather-plugins/open-meteo/open-meteo.go's
// minutely_15 mapping for a worked example.
//
// "next_hour_source" is REQUIRED whenever "next_hour" is non-empty (an
// unknown or missing value is a hard error - mapWasmFetchForecastOutput in
// wasm_weather_provider.go - never a silent default), and is one of:
//
//   - "nowcast": genuine short-range/high-resolution data - WeatherKit's
//     forecastNextHour, or Open-Meteo's minutely_15 in the regions it
//     documents as natively modelled at 15-minute resolution rather than
//     interpolated.
//   - "hourly": the provider only has an hourly-resolution model here, and
//     next_hour's finer timestamps are interpolated from it - e.g.
//     Open-Meteo's minutely_15 outside its native-resolution regions, which
//     reads as smoothly-stepping values between the surrounding hourly
//     readings rather than independent short-range data, confirmed live at
//     multiple positions (see that plugin's own doc comment).
//
// A provider with no next_hour coverage at all omits next_hour_source too
// (or sends anything - the host does not look at it when next_hour is
// empty). See buildWeatherNextHourResponse's Source field, which carries
// this straight through to GET /api/weather-forecast's next_hour.source so
// the frontend can caption a "hourly" strip honestly rather than presenting
// it as a true nowcast (AGENTS.md fallback policy).
//
// humidity_pct/visibility_m are hourly-only - there is no daily aggregate in
// this contract, deliberately: the host derives the day figure itself
// (humidity as the mean of the day's valid hourly samples, visibility as
// their minimum - see buildDayData/reduceHumidityPct/reduceVisibilityNm)
// rather than trusting a provider's own daily rollup, so both reference
// providers reduce the same way regardless of what daily aggregates they do
// or don't expose.
//
// humidity_pct/visibility_m are the ONE exception to the "any numeric field
// may be omitted/zero" rule below: on the WASM host boundary
// (wasmWeatherHourOutput in wasm_weather_provider.go) they are *float64, not
// float64, so "the plugin never sends this field" (nil) is distinguishable
// from "the plugin sent a genuine zero". A bare float64 would decode a
// missing JSON key to exactly 0.0 - 0% humidity or 0.0 nm visibility - and
// 0.0 nm is a catastrophic false reading on a helm display: it means the bow
// is out of sight. See sentinelHumidityPct/sentinelVisibilityNm below.
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
// already used for WeatherKit's raw JSON. precipitation_chance_pct,
// humidity_pct and visibility_m are the exceptions: each has a legitimate
// real-zero reading (a dry day, a still-air/calm humidity trough, dense fog),
// so "omitted" must be signalled some other way rather than by zero - a
// plugin-emitted negative for precipitation_chance_pct, or the wire's
// *float64 nil for humidity_pct/visibility_m - see
// sentinelPrecipitationPct/sentinelHumidityPct/sentinelVisibilityNm below.
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
//
// HumidityPct/VisibilityNm are already the final -1-for-absent sentinel by
// the time they land here (sentinelHumidityPct/sentinelVisibilityNm run in
// mapWasmFetchForecastOutput, off the wire's *float64 fields) - unlike
// PrecipitationChancePct, whose raw plugin-emitted value already IS the
// sentinel convention and needs no further pointer indirection. See this
// file's top doc comment and sentinelHumidityPct/sentinelVisibilityNm below
// for why humidity/visibility need the pointer step precipitation does not.
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
	HumidityPct            float64
	VisibilityNm           float64
}

// weatherNextHourPoint is a single time-stamped nowcast sample, SI units, as
// returned by one entry of a plugin's fetch_forecast "next_hour" field. A
// nil/empty NextHour on weatherForecastBundle means the provider has no
// nowcast coverage for this position, not "checked, found nothing to
// report" - see this file's top doc comment.
type weatherNextHourPoint struct {
	Time time.Time
	// PrecipitationChancePct follows the same negative-is-absent convention
	// as weatherCurrentPoint/weatherDayPoint/weatherHourPoint's own field of
	// the same name - see sentinelPrecipitationPct's doc comment. Unlike
	// those fields, the host does NOT run it through sentinelPrecipitationPct
	// here: that function clamps into [0,100] and would turn a negative
	// "not supplied" reading into a false 0%. next_hour keeps the plugin's
	// raw value so buildWeatherNextHourResponse can pass the sentinel
	// straight through to the wire.
	PrecipitationChancePct float64
	PrecipitationMMPerH    float64
}

// weatherForecastBundle is one provider round-trip's worth of data - the
// return value of weatherProvider.FetchForecast and of a WASM plugin's
// fetch_forecast, mapped to typed Go values. Cached/CachedAt describe the
// adapter's own cache bookkeeping (see wasmWeatherProvider.FetchForecast),
// independent of any timestamp inside Current/Days/Hourly/NextHour.
type weatherForecastBundle struct {
	Current  weatherCurrentPoint
	Days     []weatherDayPoint
	Hourly   []weatherHourPoint
	NextHour []weatherNextHourPoint
	// NextHourSource is "nowcast" or "hourly" whenever NextHour is non-empty
	// (validated in mapWasmFetchForecastOutput) - see this file's top doc
	// comment's next_hour_source section. Meaningless/unset when NextHour is
	// empty.
	NextHourSource string
	Cached         bool
	CachedAt       time.Time
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

// metersPerNauticalMile converts a metres reading to nautical miles
// (1nm == 1852m exactly, by international definition), the same way
// sentinelSpeedKts uses metersPerSecondToKnots.
const metersPerNauticalMile = 1852.0

// sentinelHumidityPct normalizes an optional relative-humidity percentage,
// taking a POINTER input deliberately - unlike sentinelPrecipitationPct,
// which normalizes a bare float64 the plugin itself already encodes -1 into.
//
// Humidity's wire field (wasmWeatherHourOutput.HumidityPct) is a *float64
// because there are two distinct kinds of "no data" to represent, not one:
// a plugin built before this field existed simply omits the JSON key
// (decodes to nil), while a plugin that HAS the field but lacks a value for
// a particular hour emits an explicit negative (mirroring the precipitation
// convention). A bare float64 could only ever represent the second case -
// omission would silently decode to exactly 0.0, indistinguishable from a
// real (if unusual) 0% humidity reading. Both nil and negative collapse to
// the -1 sentinel here; a genuine 0 survives. See docs/adr/0035-weather-local-day-boundaries.md
// for the "0% precip during actual rainfall" incident this same collapse
// caused for a different field.
func sentinelHumidityPct(pct *float64) float64 {
	if pct == nil || *pct < 0 {
		return -1
	}
	if *pct > 100 {
		return 100
	}
	return *pct
}

// sentinelVisibilityNm normalizes an optional visibility reading (metres, the
// wire unit) to nautical miles, applying the same nil-or-negative -> -1 rule
// as sentinelHumidityPct - for a graver reason. Visibility is a navigation-
// safety number: a real 0.0nm reading means fog thick enough that the bow is
// out of sight, and it MUST survive as "0.0 nm", never collapse into the same
// sentinel as "the provider never sent this field". That collapse is exactly
// what a bare (non-pointer) float64 would force, since JSON-absent and
// JSON-zero both decode to 0.0 - the same failure mode ADR 0035 records for
// precipitation ("0% precip" rendered during actual rainfall because absence
// and zero were conflated), one step more dangerous here because the
// consequence is a false "you can see clearly" on a helm display instead of
// a false "it won't rain". Only nil (field missing from the wire entirely)
// or an explicit negative (the plugin's own "no value this hour" signal, per
// the guest contract doc comment above) maps to -1; a genuine 0.0 metres
// passes through as 0.0nm.
func sentinelVisibilityNm(metres *float64) float64 {
	if metres == nil || *metres < 0 {
		return -1
	}
	return *metres / metersPerNauticalMile
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
			HumidityPct:  hp.HumidityPct,
			VisibilityNm: hp.VisibilityNm,
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
		HumidityPct:          reduceHumidityPct(cloudSeries),
		VisibilityNm:         reduceVisibilityNm(cloudSeries),
		HourlyWind:           windSeries,
		HourlyPrecip:         precipSeries,
		HourlyUV:             uvSeries,
		HourlyCloud:          cloudSeries,
	}
}

// weatherHourlyStripHours is the strip's span - the "24" the forecast
// drawer's Today panel labels itself with (spanLabel="Next 24 hours"). It
// used to be 12, which read as a full day's plan while actually stopping
// around 9PM for anyone reading it at midday: daylight hours only, no
// overnight, on the one panel a skipper uses to judge whether tonight is
// safe.
const weatherHourlyStripHours = 24

// buildWeatherHourlyStrip builds the "next up to 24 hours" strip for the
// forecast drawer's today view, with a synthetic "Sunset" entry spliced in
// at the right spot - the typed-contract replacement for the old
// buildWeatherHourlyEntries, operating on a provider's flat hourly points
// instead of raw WeatherKit JSON. Preserves the original's exact semantics:
// entries before the current local hour are skipped, the current hour is
// labeled "Now", a sunset entry (Kind: "sunset", all data sentinel/absent)
// is inserted once in time order (or appended at the end as a fallback if
// no hour after it was found), and the strip is capped at
// weatherHourlyStripHours entries.
func buildWeatherHourlyStrip(hourly []weatherHourPoint, referenceDatetime time.Time, localLocation *time.Location, localTodayKey string, sunsetAt time.Time) []weatherHourlyEntryData {
	referenceLocal := referenceDatetime.In(localLocation)
	referenceHour := referenceLocal.Truncate(time.Hour)
	entries := make([]weatherHourlyEntryData, 0, weatherHourlyStripHours)
	insertedSunset := false

	for _, hp := range hourly {
		if len(entries) >= weatherHourlyStripHours {
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
			if len(entries) >= weatherHourlyStripHours {
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
			// The frontend's night styling reads this per hour rather than
			// latching "night" on for good the first time the sunset entry
			// above appears - a latch that never turns back off is wrong
			// once the strip is long enough to reach the following sunrise.
			IsDaylight: hp.IsDaylight,
		})
	}

	if !insertedSunset && !sunsetAt.IsZero() && sunsetAt.Format("2006-01-02") == localTodayKey && sunsetAt.After(referenceLocal) && len(entries) < weatherHourlyStripHours {
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
	IsDaylight       bool    `json:"is_daylight"`
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
	HumidityPct  float64 `json:"humidity_pct"`
	VisibilityNm float64 `json:"visibility_nm"`
}

// weatherForecastDayResponse mirrors weatherForecastDayData for JSON output.
// Unlike the pre-Phase-3 shape, it carries no wave fields (waves have their
// own /api/wave-forecast endpoint) and gains DayKey - the vessel-local
// "2006-01-02" join key the wave-forecast response matches against.
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
	HumidityPct          float64                              `json:"humidity_pct"`
	VisibilityNm         float64                              `json:"visibility_nm"`
	HourlyWind           []weatherHourlyWindResponse          `json:"hourly_wind"`
	HourlyPrecip         []weatherHourlyPrecipitationResponse `json:"hourly_precip"`
	HourlyUV             []weatherHourlyUVResponse            `json:"hourly_uv"`
	HourlyCloud          []weatherHourlyCloudResponse         `json:"hourly_cloud"`
}

// weatherNextHourPointResponse mirrors weatherNextHourPoint for JSON output.
// ChancePct keeps the plugin's raw negative-is-absent value unchanged (see
// weatherNextHourPoint's doc comment) rather than running it through
// sentinelPrecipitationPct.
type weatherNextHourPointResponse struct {
	Time      string  `json:"time"`
	ChancePct float64 `json:"chance_pct"`
	MMPerH    float64 `json:"mm_per_h"`
}

// weatherNextHourResponse is the wire shape of GET /api/weather-forecast's
// top-level "next_hour" nowcast field - nil (and therefore omitted by
// weatherForecastResponse's `omitempty`) when the provider supplied none.
// Start is the first point's own timestamp, not the time the bundle was
// fetched/cached: a nowcast goes stale far faster than the hourly/daily
// data around it (a cached bundle can be many minutes old), so the frontend
// must slide its display window against each point's own Time compared to
// the viewer's current clock, never against when the response left the
// server - see lib/nowcast.ts.
type weatherNextHourResponse struct {
	Start       string `json:"start"`
	StepMinutes int    `json:"step_minutes"`
	// Source is "nowcast" or "hourly" - see this file's top doc comment's
	// next_hour_source section. Always populated when Points is non-empty
	// (buildWeatherNextHourResponse only returns non-nil in that case, and
	// mapWasmFetchForecastOutput already validated the bundle's
	// NextHourSource by the time it gets here).
	Source string                         `json:"source"`
	Points []weatherNextHourPointResponse `json:"points"`
}

// buildWeatherNextHourResponse maps a bundle's NextHour points (and their
// shared NextHourSource) onto the wire shape, inferring StepMinutes from the
// gap between the first two points (the guest contract deliberately carries
// no explicit step field - see this file's top doc comment). Returns nil for
// a nil or empty points slice, so a provider with no nowcast coverage
// produces no "next_hour" key at all, distinguishable from "nowcast checked,
// all dry" (a non-nil response whose points all read 0% chance).
func buildWeatherNextHourResponse(points []weatherNextHourPoint, source string) *weatherNextHourResponse {
	if len(points) == 0 {
		return nil
	}

	stepMinutes := 0
	if len(points) >= 2 {
		stepMinutes = int(points[1].Time.Sub(points[0].Time).Round(time.Minute) / time.Minute)
	}

	mapped := make([]weatherNextHourPointResponse, 0, len(points))
	for _, p := range points {
		mapped = append(mapped, weatherNextHourPointResponse{
			Time:      p.Time.UTC().Format(time.RFC3339),
			ChancePct: p.PrecipitationChancePct,
			MMPerH:    p.PrecipitationMMPerH,
		})
	}

	return &weatherNextHourResponse{
		Start:       points[0].Time.UTC().Format(time.RFC3339),
		StepMinutes: stepMinutes,
		Source:      source,
		Points:      mapped,
	}
}

type weatherForecastResponse struct {
	Days        []weatherForecastDayResponse `json:"days"`
	HourlyToday []weatherHourlyEntryResponse `json:"hourly_today"`
	// NextHour is the nowcast (see buildWeatherNextHourResponse) - nil, and
	// therefore omitted, when the configured provider has no next-hour
	// coverage for this position.
	NextHour   *weatherNextHourResponse `json:"next_hour,omitempty"`
	Summary    string                   `json:"summary"`
	Provider   string                   `json:"provider"`
	Cached     bool                     `json:"cached"`
	UpdatedAt  string                   `json:"updated_at"`
	TTLSeconds int64                    `json:"ttl_seconds"`
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
			HumidityPct:  entry.HumidityPct,
			VisibilityNm: entry.VisibilityNm,
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
			IsDaylight:       entry.IsDaylight,
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
		HumidityPct:          day.HumidityPct,
		VisibilityNm:         day.VisibilityNm,
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
// entirely (it lives on the wave endpoint instead); high/low
// temperature is dropped too (that belongs to the day bundle in
// /api/weather-forecast's days[0], not a point-in-time "today" reading).
func weatherToday(c echo.Context) error {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	provider, configuredProvider, err := resolveWeatherProvider(settingsPath)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}

	vesselState, vesselErr := fetchSignalKVesselState()
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
		NextHour:    buildWeatherNextHourResponse(bundle.NextHour, bundle.NextHourSource),
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
