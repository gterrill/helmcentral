// weatherkit.go holds all the logic for the Apple WeatherKit weather-provider
// plugin: ES256 JWT construction/signing, the WeatherKit request URL, and
// mapping WeatherKit's JSON response onto Helmcentral's fetch_forecast guest
// contract (backend/weather_providers.go's top doc comment / the
// wasmWeatherCurrentOutput / wasmWeatherDayOutput / wasmWeatherHourOutput
// structs in backend/wasm_weather_provider.go, mirrored below field-for-
// field). Deliberately has no dependency on "github.com/extism/go-pdk", so
// it (and main_test.go, which exercises it) can be built and tested with the
// plain host Go toolchain (`go test ./...`, no TinyGo/wasm target needed) -
// see main.go's doc comment and docs/examples/tide-plugins/bom/bom.go for
// why that split matters. main.go's //go:wasmexport fetch_forecast calls
// straight into these same functions; nothing here is reimplemented or
// duplicated there.
//
// JWT signing is ported near-verbatim from the feasibility spike at
// backend/testdata/wasm_plugins/src/es256sign/main.go (parse PKCS8 PEM ->
// ecdsa.Sign -> pack r||s into 64 bytes -> base64url), which proved TinyGo
// 0.41.1 targeting wasip1 can do ES256 signing in-guest with stdlib
// crypto/ecdsa + crypto/x509 + crypto/rand + crypto/sha256. This file goes
// further than that spike: it also builds the full three-part JWT (header,
// claims, signature, dot-joined, each base64url) rather than just the
// signature step.
//
// The JWT claims shape (iss/sub/aud/iat/exp, 1-hour expiry, "kid" header)
// and the currentWeather/forecastDaily/forecastHourly field mappings below
// are ported from the now-deleted native backend/weather_tide.go
// (generateWeatherKitJWT, fetchWeatherKitData,
// fetchWeatherKitForecastBundleData, buildDailyWindSeries et al. - see
// `git show <prior-commit>:backend/weather_tide.go`), with three deliberate
// changes from the original:
//
//  1. timezone=UTC instead of the old hardcoded timezone=America/Los_Angeles
//     query param. WeatherKit's RFC3339 timestamps already carry their own
//     UTC offset regardless of that query param - the old hardcoded LA value
//     was a bug/oversight, not a requirement. UTC is unambiguous and avoids
//     it.
//  2. One merged request (dataSets=currentWeather,forecastDaily,
//     forecastHourly) instead of the original's two separate host functions
//     (fetchWeatherKitData for current+daily,
//     fetchWeatherKitForecastBundleData for daily+hourly) - this plugin's
//     single fetch_forecast call needs current+days+hourly all at once.
//  3. moonPhase is dropped entirely - the new host-side contract computes
//     moon phase itself (backend/weather_providers.go's moonPhase), so
//     emitting it here would be dead data.
//
// Unit conversions also differ from the original: the old code converted
// KPH -> knots host-side (kphToKnots) for a kts-based contract. This
// plugin's contract is SI (wind_speed_ms/wind_gust_ms), so wind speeds are
// converted KPH -> m/s (kph / 3.6) instead, and the *host* now derives knots
// (see weather_providers.go's sentinelSpeedKts). Temperatures and
// precipitation intensity are passed through in WeatherKit's native units
// (Celsius, mm/hr) unconverted - the host converts to Fahrenheit itself.
// precipitationChance is a 0-1 fraction in WeatherKit's JSON but the guest
// contract field is named "*_pct", so it is multiplied by 100 here.
package main

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math"
	"strings"
	"time"
)

// --- config resolution ---

// configGetter mirrors github.com/extism/go-pdk's GetConfig(key string)
// (string, bool) signature exactly, so pdk.GetConfig can be passed directly
// as a configGetter with no wrapping, and tests can pass a plain map-backed
// stand-in with no WASM runtime involved.
type configGetter func(key string) (string, bool)

// weatherKitConfigField pairs a config.json key with the environment
// variable name an operator sets to populate it (via configForWasmPlugin's
// os.Expand "${VAR}" mechanism) - used both to resolve the four required
// values and to name exactly which ones are missing in the fail-fast error
// message.
var weatherKitConfigFields = []struct {
	Key    string
	EnvVar string
}{
	{"key_id", "WEATHERKIT_KEY_ID"},
	{"team_id", "WEATHERKIT_TEAM_ID"},
	{"service_id", "WEATHERKIT_SERVICE_ID"},
	{"private_key", "WEATHERKIT_PRIVATE_KEY"},
}

// weatherKitConfig holds the four resolved WeatherKit credential values.
type weatherKitConfig struct {
	KeyID         string
	TeamID        string
	ServiceID     string
	PrivateKeyPEM string
}

// resolveWeatherKitConfig reads all four required config keys via get,
// returning the config-key names (not env var names) of any that are
// entirely absent. A key being entirely absent - get's ok==false - is this
// plugin's ONLY "not configured" signal, per configForWasmPlugin's
// documented behavior of dropping (not empty-stringing) any config key whose
// referenced env var is unset. An empty-but-present value is left as-is and
// is NOT treated as missing here (config().json can theoretically set a
// literal empty string; that's the operator's choice to debug, not this
// plugin's to silently mask).
func resolveWeatherKitConfig(get configGetter) (weatherKitConfig, []string) {
	var cfg weatherKitConfig
	dsts := []*string{&cfg.KeyID, &cfg.TeamID, &cfg.ServiceID, &cfg.PrivateKeyPEM}

	var missing []string
	for i, field := range weatherKitConfigFields {
		v, ok := get(field.Key)
		if !ok {
			missing = append(missing, field.Key)
			continue
		}
		*dsts[i] = v
	}
	return cfg, missing
}

// missingConfigKeyErrorMessage builds the fail-fast, actionable error
// fetch_forecast reports when one or more config keys are missing: exactly
// which config keys are missing, plus the env vars an operator needs to set
// to fix it.
func missingConfigKeyErrorMessage(missing []string) string {
	return fmt.Sprintf(
		"weatherkit: missing config key(s): %s — set WEATHERKIT_KEY_ID / WEATHERKIT_TEAM_ID / WEATHERKIT_SERVICE_ID / WEATHERKIT_PRIVATE_KEY as environment variables on the backend and restart; see this plugin's README for how to obtain them",
		strings.Join(missing, ", "),
	)
}

// --- ES256 JWT construction ---

// jwtHeader/jwtClaims are marshaled as ordinary structs (rather than
// map[string]any) so field order in the resulting JSON is deterministic -
// doesn't affect JWT validity either way (a JSON object's key order carries
// no meaning), but makes output easier to eyeball in tests/logs.
type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

type jwtClaims struct {
	Iss string `json:"iss"`
	Sub string `json:"sub"`
	Aud string `json:"aud"`
	Iat int64  `json:"iat"`
	Exp int64  `json:"exp"`
}

// buildWeatherKitJWT builds and ES256-signs a full three-part JWT
// (header.claims.signature, each segment base64url-encoded, dot-joined) for
// authenticating to the WeatherKit API, replicating the now-deleted native
// generateWeatherKitJWT's exact claims shape: {"iss": teamID, "sub":
// serviceID, "aud": "https://weatherkit.apple.com", "iat": now.Unix(),
// "exp": exp.Unix()} with header {"alg":"ES256","kid": keyID} and a 1-hour
// expiry. The signing step itself (PEM-decode -> PKCS8 parse -> ecdsa.Sign
// -> pack r||s into 64 bytes -> base64url) is ported near-verbatim from
// backend/testdata/wasm_plugins/src/es256sign/main.go.
func buildWeatherKitJWT(keyID, teamID, serviceID, privateKeyPEM string, now time.Time) (string, error) {
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return "", fmt.Errorf("weatherkit: failed to PEM-decode WEATHERKIT_PRIVATE_KEY")
	}

	keyAny, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("weatherkit: failed to parse WEATHERKIT_PRIVATE_KEY as PKCS8: %w", err)
	}

	priv, ok := keyAny.(*ecdsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("weatherkit: WEATHERKIT_PRIVATE_KEY is not an ECDSA private key")
	}

	headerJSON, err := json.Marshal(jwtHeader{Alg: "ES256", Kid: keyID})
	if err != nil {
		return "", fmt.Errorf("weatherkit: failed to marshal JWT header: %w", err)
	}

	exp := now.Add(time.Hour)
	claimsJSON, err := json.Marshal(jwtClaims{
		Iss: teamID,
		Sub: serviceID,
		Aud: "https://weatherkit.apple.com",
		Iat: now.Unix(),
		Exp: exp.Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("weatherkit: failed to marshal JWT claims: %w", err)
	}

	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)

	hash := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, priv, hash[:])
	if err != nil {
		return "", fmt.Errorf("weatherkit: ecdsa.Sign failed: %w", err)
	}

	// RFC 7518 ES256: signature is R||S, each a fixed-width 32-byte
	// big-endian integer (P-256 field size) -- NOT ASN1 DER.
	sigBytes := make([]byte, 64)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	copy(sigBytes[32-len(rBytes):32], rBytes)
	copy(sigBytes[64-len(sBytes):64], sBytes)

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sigBytes), nil
}

// --- WeatherKit request URL ---

// weatherKitRequestURL builds the single merged request URL this plugin
// uses for fetch_forecast: current + daily + hourly datasets in one call
// (the original native code made two separate requests across two host
// functions - see this file's top doc comment). timezone=UTC, not the
// original's hardcoded America/Los_Angeles - see top doc comment point 1.
func weatherKitRequestURL(lat, lon float64) string {
	return fmt.Sprintf(
		"https://weatherkit.apple.com/api/v1/weather/en/%.4f/%.4f?dataSets=currentWeather,forecastDaily,forecastHourly&timezone=UTC",
		lat, lon,
	)
}

// kphToMS converts a KPH value (WeatherKit's native wind-speed unit) to m/s
// (this plugin's SI contract unit): kph * 1000/3600 == kph / 3.6.
func kphToMS(kph float64) float64 {
	return kph / 3.6
}

// --- guest contract output shapes ---
// Field names/JSON tags mirror backend/wasm_weather_provider.go's
// wasmWeatherCurrentOutput / wasmWeatherDayOutput / wasmWeatherHourOutput /
// wasmFetchForecastOutput exactly - this plugin is a separate Go module so
// can't import those directly, but the wire shape must match field-for-
// field.

type fetchForecastInput struct {
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	Days int     `json:"days"`
}

type weatherCurrentOutput struct {
	Time                   string  `json:"time"`
	TemperatureC           float64 `json:"temperature_c"`
	Condition              string  `json:"condition"`
	WindSpeedMS            float64 `json:"wind_speed_ms"`
	WindGustMS             float64 `json:"wind_gust_ms"`
	WindDirectionDeg       float64 `json:"wind_direction_deg"`
	PrecipitationChancePct float64 `json:"precipitation_chance_pct"`
}

type weatherDayOutput struct {
	Start                  string  `json:"start"`
	Condition              string  `json:"condition"`
	TempMaxC               float64 `json:"temp_max_c"`
	TempMinC               float64 `json:"temp_min_c"`
	WindSpeedMS            float64 `json:"wind_speed_ms"`
	WindGustMS             float64 `json:"wind_gust_ms"`
	WindDirectionDeg       float64 `json:"wind_direction_deg"`
	PrecipitationChancePct float64 `json:"precipitation_chance_pct"`
	Sunrise                string  `json:"sunrise"`
	Sunset                 string  `json:"sunset"`
}

type weatherHourOutput struct {
	Time                   string  `json:"time"`
	TemperatureC           float64 `json:"temperature_c"`
	Condition              string  `json:"condition"`
	WindSpeedMS            float64 `json:"wind_speed_ms"`
	WindGustMS             float64 `json:"wind_gust_ms"`
	WindDirectionDeg       float64 `json:"wind_direction_deg"`
	PrecipitationChancePct float64 `json:"precipitation_chance_pct"`
	PrecipitationMM        float64 `json:"precipitation_mm"`
	UVIndex                float64 `json:"uv_index"`
	IsDaylight             bool    `json:"is_daylight"`
}

type fetchForecastOutput struct {
	Current weatherCurrentOutput `json:"current"`
	Days    []weatherDayOutput   `json:"days"`
	Hourly  []weatherHourOutput  `json:"hourly"`
}

// --- WeatherKit's raw JSON response shapes ---
// Pointer fields distinguish "field absent from this response" from "field
// present with a legitimate zero value" - the same distinction the original
// map[string]any + type-assertion code made via its `v, ok := m[k].(T)`
// idiom.

type weatherKitResponse struct {
	CurrentWeather *weatherKitCurrentWeather `json:"currentWeather"`
	ForecastDaily  *weatherKitForecastDaily  `json:"forecastDaily"`
	ForecastHourly *weatherKitForecastHourly `json:"forecastHourly"`
}

type weatherKitCurrentWeather struct {
	AsOf                   string   `json:"asOf"`
	Temperature            float64  `json:"temperature"`
	ConditionCode          string   `json:"conditionCode"`
	WindSpeed              float64  `json:"windSpeed"`
	WindGust               float64  `json:"windGust"`
	WindDirection          float64  `json:"windDirection"`
	PrecipitationChance    *float64 `json:"precipitationChance"`
	PrecipitationIntensity *float64 `json:"precipitationIntensity"`
}

type weatherKitForecastDaily struct {
	Days []weatherKitDayForecast `json:"days"`
}

type weatherKitDayForecast struct {
	ForecastStart       string             `json:"forecastStart"`
	ConditionCode       string             `json:"conditionCode"`
	TemperatureMax      float64            `json:"temperatureMax"`
	TemperatureMin      float64            `json:"temperatureMin"`
	PrecipitationChance *float64           `json:"precipitationChance"`
	WindSpeed           *float64           `json:"windSpeed"`
	WindGustSpeedMax    *float64           `json:"windGustSpeedMax"`
	WindDirection       *float64           `json:"windDirection"`
	Sunrise             string             `json:"sunrise"`
	Sunset              string             `json:"sunset"`
	DaytimeForecast     *weatherKitDaypart `json:"daytimeForecast"`
}

type weatherKitDaypart struct {
	WindSpeed        *float64 `json:"windSpeed"`
	WindGustSpeedMax *float64 `json:"windGustSpeedMax"`
	WindDirection    *float64 `json:"windDirection"`
}

type weatherKitForecastHourly struct {
	Hours []weatherKitHourForecast `json:"hours"`
}

type weatherKitHourForecast struct {
	ForecastStart          string   `json:"forecastStart"`
	ConditionCode          string   `json:"conditionCode"`
	Temperature            float64  `json:"temperature"`
	WindSpeed              float64  `json:"windSpeed"`
	WindGust               *float64 `json:"windGust"`
	WindDirection          float64  `json:"windDirection"`
	PrecipitationChance    *float64 `json:"precipitationChance"`
	PrecipitationIntensity *float64 `json:"precipitationIntensity"`
	UVIndex                float64  `json:"uvIndex"`
	Daylight               bool     `json:"daylight"`
}

// parseWeatherKitResponse parses a raw WeatherKit API response body and maps
// it onto this plugin's fetch_forecast output contract. maxDays caps the
// number of days[] entries returned (fetch_forecast's input "days" field);
// 0 or negative means "no cap" (WeatherKit's forecastDaily already returns
// its own bounded window, same as the original native code's daysCount
// default-to-6 was just a slice cap, not a request parameter).
//
// Fails fast (no partial/fake data) when: the body isn't valid JSON,
// currentWeather is absent or lacks asOf, or forecastDaily.days is absent or
// empty - all conditions where returning zero-valued output would silently
// look like "the vessel is at 0degC with no wind" rather than "WeatherKit
// sent something this plugin didn't expect."
func parseWeatherKitResponse(body []byte, maxDays int) (fetchForecastOutput, error) {
	var out fetchForecastOutput

	var resp weatherKitResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		snippet := string(body)
		if len(snippet) > 500 {
			snippet = snippet[:500]
		}
		return out, fmt.Errorf("weatherkit: failed to parse WeatherKit response: %w; body: %s", err, snippet)
	}

	if resp.CurrentWeather == nil {
		return out, fmt.Errorf("weatherkit: currentWeather missing from response")
	}
	if resp.ForecastDaily == nil || len(resp.ForecastDaily.Days) == 0 {
		return out, fmt.Errorf("weatherkit: forecastDaily.days missing or empty in response")
	}

	current, err := mapCurrentWeather(resp)
	if err != nil {
		return out, err
	}
	out.Current = current

	days, err := mapForecastDays(resp.ForecastDaily.Days, maxDays)
	if err != nil {
		return out, err
	}
	out.Days = days

	if resp.ForecastHourly != nil {
		out.Hourly = mapForecastHours(resp.ForecastHourly.Hours)
	}

	return out, nil
}

// mapCurrentWeather maps currentWeather plus a precipitation-chance fallback
// cascade (current -> forecastDaily.days[0] -> forecastHourly.hours[0]),
// replicating the original fetchWeatherKitData's fallback chain.
// precipitationChance is a 0-1 fraction in WeatherKit's JSON; multiplied by
// 100 for this contract's "*_pct" field. Falls back to
// precipitationIntensity (mm/hr) verbatim when precipitationChance itself is
// absent - the original code's own fallback, kept as-is even though mixing
// an mm/hr value into a "pct" field is a pre-existing quirk, not a new one
// introduced here.
func mapCurrentWeather(resp weatherKitResponse) (weatherCurrentOutput, error) {
	cw := resp.CurrentWeather
	if strings.TrimSpace(cw.AsOf) == "" {
		return weatherCurrentOutput{}, fmt.Errorf("weatherkit: currentWeather.asOf missing from response")
	}

	current := weatherCurrentOutput{
		Time:             cw.AsOf,
		TemperatureC:     cw.Temperature,
		Condition:        cw.ConditionCode,
		WindSpeedMS:      kphToMS(cw.WindSpeed),
		WindGustMS:       kphToMS(cw.WindGust),
		WindDirectionDeg: cw.WindDirection,
	}

	switch {
	case cw.PrecipitationChance != nil:
		current.PrecipitationChancePct = *cw.PrecipitationChance * 100
	case cw.PrecipitationIntensity != nil:
		current.PrecipitationChancePct = math.Max(0, *cw.PrecipitationIntensity)
	case len(resp.ForecastDaily.Days) > 0 && resp.ForecastDaily.Days[0].PrecipitationChance != nil:
		current.PrecipitationChancePct = *resp.ForecastDaily.Days[0].PrecipitationChance * 100
	case resp.ForecastHourly != nil && len(resp.ForecastHourly.Hours) > 0 && resp.ForecastHourly.Hours[0].PrecipitationChance != nil:
		current.PrecipitationChancePct = *resp.ForecastHourly.Hours[0].PrecipitationChance * 100
	}

	return current, nil
}

// mapForecastDays maps forecastDaily.days[] onto weatherDayOutput, preferring
// daytimeForecast's wind fields over the day-level fallback fields exactly
// like the original fetchWeatherKitForecastBundleData did. sunrise/sunset
// are passed through as WeatherKit's own RFC3339 strings unchanged - the
// host now does its own local-timezone formatting
// (weather_providers.go's buildDayData), unlike the original code's
// formatWeatherKitLocalTime.
func mapForecastDays(rawDays []weatherKitDayForecast, maxDays int) ([]weatherDayOutput, error) {
	days := make([]weatherDayOutput, 0, len(rawDays))
	for _, d := range rawDays {
		if maxDays > 0 && len(days) >= maxDays {
			break
		}
		if strings.TrimSpace(d.ForecastStart) == "" {
			return nil, fmt.Errorf("weatherkit: forecastDaily day missing forecastStart")
		}

		var windSpeedMS, windGustMS, windDirectionDeg float64
		if d.DaytimeForecast != nil {
			if d.DaytimeForecast.WindSpeed != nil {
				windSpeedMS = kphToMS(*d.DaytimeForecast.WindSpeed)
			}
			if d.DaytimeForecast.WindGustSpeedMax != nil {
				windGustMS = kphToMS(*d.DaytimeForecast.WindGustSpeedMax)
			}
			if d.DaytimeForecast.WindDirection != nil {
				windDirectionDeg = *d.DaytimeForecast.WindDirection
			}
		}
		if windSpeedMS == 0 && d.WindSpeed != nil {
			windSpeedMS = kphToMS(*d.WindSpeed)
		}
		if windGustMS == 0 && d.WindGustSpeedMax != nil {
			windGustMS = kphToMS(*d.WindGustSpeedMax)
		}
		if windDirectionDeg == 0 && d.WindDirection != nil {
			windDirectionDeg = *d.WindDirection
		}

		var precipPct float64
		if d.PrecipitationChance != nil {
			precipPct = *d.PrecipitationChance * 100
		}

		days = append(days, weatherDayOutput{
			Start:                  d.ForecastStart,
			Condition:              d.ConditionCode,
			TempMaxC:               d.TemperatureMax,
			TempMinC:               d.TemperatureMin,
			WindSpeedMS:            windSpeedMS,
			WindGustMS:             windGustMS,
			WindDirectionDeg:       windDirectionDeg,
			PrecipitationChancePct: precipPct,
			Sunrise:                d.Sunrise,
			Sunset:                 d.Sunset,
		})
	}

	if len(days) == 0 {
		return nil, fmt.Errorf("weatherkit: no forecast days parsed from WeatherKit response")
	}
	return days, nil
}

// mapForecastHours maps forecastHourly.hours[] onto weatherHourOutput.
// Unlike the original code's buildDailyWindSeries (which fell back
// windGust=windSpeed when windGust was absent), the gust fallback is left to
// the host now (weather_providers.go's sentinelGustKts treats an
// exactly-zero WindGustMS as "no data" and falls back to the sustained
// speed itself) - duplicating that fallback here would be redundant, not
// wrong, but the host is the single source of truth for that convention.
// Entries with an empty/missing forecastStart are skipped (not erred) since
// forecastHourly is optional data, unlike forecastDaily.
func mapForecastHours(rawHours []weatherKitHourForecast) []weatherHourOutput {
	hourly := make([]weatherHourOutput, 0, len(rawHours))
	for _, h := range rawHours {
		if strings.TrimSpace(h.ForecastStart) == "" {
			continue
		}

		var windGustMS float64
		if h.WindGust != nil {
			windGustMS = kphToMS(*h.WindGust)
		}

		var precipPct float64
		if h.PrecipitationChance != nil {
			precipPct = *h.PrecipitationChance * 100
		}

		var precipMM float64
		if h.PrecipitationIntensity != nil {
			precipMM = math.Max(0, *h.PrecipitationIntensity)
		}

		hourly = append(hourly, weatherHourOutput{
			Time:                   h.ForecastStart,
			TemperatureC:           h.Temperature,
			Condition:              h.ConditionCode,
			WindSpeedMS:            kphToMS(h.WindSpeed),
			WindGustMS:             windGustMS,
			WindDirectionDeg:       h.WindDirection,
			PrecipitationChancePct: precipPct,
			PrecipitationMM:        precipMM,
			UVIndex:                math.Max(0, h.UVIndex),
			IsDaylight:             h.Daylight,
		})
	}
	return hourly
}
