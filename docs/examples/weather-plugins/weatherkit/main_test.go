package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"
)

// ecdsaTestRSAKeyPEM generates a throwaway RSA key, PKCS8-PEM-encodes it,
// and returns the PEM string - used to prove buildWeatherKitJWT rejects a
// syntactically-valid-PKCS8-but-wrong-algorithm key rather than producing an
// unsignable/garbage token.
func ecdsaTestRSAKeyPEM() (string, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", err
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), nil
}

// --- JWT construction/signing ---

// TestBuildWeatherKitJWT_ProducesCryptographicallyVerifiableToken ports the
// verification approach from backend/wasm_es256_spike_test.go: generate a
// throwaway P-256 key, build+sign a JWT with it, then verify the resulting
// signature against the same key's public half with ecdsa.Verify. Producing
// three non-empty dot-separated segments is NOT sufficient on its own -
// this test only passes if the signature actually verifies.
func TestBuildWeatherKitJWT_ProducesCryptographicallyVerifiableToken(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey failed: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	token, err := buildWeatherKitJWT("TESTKID1234", "TESTTEAMID", "com.example.weatherkit", string(pemBytes), now)
	if err != nil {
		t.Fatalf("buildWeatherKitJWT failed: %v", err)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected a 3-part JWT (header.claims.signature), got %d parts: %q", len(parts), token)
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("failed to decode header segment: %v", err)
	}
	var header jwtHeader
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		t.Fatalf("failed to unmarshal header: %v", err)
	}
	if header.Alg != "ES256" {
		t.Errorf("expected header.alg=ES256, got %q", header.Alg)
	}
	if header.Kid != "TESTKID1234" {
		t.Errorf("expected header.kid=TESTKID1234, got %q", header.Kid)
	}

	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("failed to decode claims segment: %v", err)
	}
	var claims jwtClaims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		t.Fatalf("failed to unmarshal claims: %v", err)
	}
	if claims.Iss != "TESTTEAMID" {
		t.Errorf("expected claims.iss=TESTTEAMID, got %q", claims.Iss)
	}
	if claims.Sub != "com.example.weatherkit" {
		t.Errorf("expected claims.sub=com.example.weatherkit, got %q", claims.Sub)
	}
	if claims.Aud != "https://weatherkit.apple.com" {
		t.Errorf("expected claims.aud=https://weatherkit.apple.com, got %q", claims.Aud)
	}
	if claims.Iat != now.Unix() {
		t.Errorf("expected claims.iat=%d, got %d", now.Unix(), claims.Iat)
	}
	wantExp := now.Add(time.Hour).Unix()
	if claims.Exp != wantExp {
		t.Errorf("expected claims.exp=%d (1h expiry), got %d", wantExp, claims.Exp)
	}

	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("failed to decode signature segment: %v", err)
	}
	if len(sigBytes) != 64 {
		t.Fatalf("expected a 64-byte R||S ES256 signature, got %d bytes", len(sigBytes))
	}

	signingInput := parts[0] + "." + parts[1]
	hash := sha256.Sum256([]byte(signingInput))
	r := new(big.Int).SetBytes(sigBytes[:32])
	s := new(big.Int).SetBytes(sigBytes[32:])
	if !ecdsa.Verify(&priv.PublicKey, hash[:], r, s) {
		t.Fatalf("JWT signature does NOT verify against the host public key")
	}
}

func TestBuildWeatherKitJWT_RejectsGarbagePEM(t *testing.T) {
	if _, err := buildWeatherKitJWT("kid", "team", "svc", "not a pem block", time.Now()); err == nil {
		t.Fatalf("expected an error for an unparseable PEM key")
	}
}

func TestBuildWeatherKitJWT_RejectsNonECDSAKey(t *testing.T) {
	// An RSA key PKCS8-encoded is syntactically valid PKCS8 but not an
	// ECDSA key - buildWeatherKitJWT must reject it, not silently produce
	// an unsignable/garbage token.
	priv, err := ecdsaTestRSAKeyPEM()
	if err != nil {
		t.Fatalf("failed to build test RSA key: %v", err)
	}
	if _, err := buildWeatherKitJWT("kid", "team", "svc", priv, time.Now()); err == nil {
		t.Fatalf("expected an error for a non-ECDSA private key")
	}
}

// --- missing-config error path ---

func TestResolveWeatherKitConfig_AllPresent(t *testing.T) {
	values := map[string]string{
		"key_id":      "KID123",
		"team_id":     "TEAM123",
		"service_id":  "com.example.svc",
		"private_key": "-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----",
	}
	get := func(key string) (string, bool) {
		v, ok := values[key]
		return v, ok
	}

	cfg, missing := resolveWeatherKitConfig(get)
	if len(missing) != 0 {
		t.Fatalf("expected no missing keys, got %v", missing)
	}
	if cfg.KeyID != "KID123" || cfg.TeamID != "TEAM123" || cfg.ServiceID != "com.example.svc" {
		t.Fatalf("unexpected resolved config: %+v", cfg)
	}
}

func TestResolveWeatherKitConfig_ReportsExactlyWhichKeysAreMissing(t *testing.T) {
	// Only key_id and private_key are present; team_id and service_id are
	// entirely absent from the config map, mirroring configForWasmPlugin's
	// documented behavior of dropping (not empty-stringing) any config key
	// whose referenced env var is unset.
	values := map[string]string{
		"key_id":      "KID123",
		"private_key": "PEMDATA",
	}
	get := func(key string) (string, bool) {
		v, ok := values[key]
		return v, ok
	}

	_, missing := resolveWeatherKitConfig(get)
	if len(missing) != 2 {
		t.Fatalf("expected exactly 2 missing keys, got %v", missing)
	}
	if missing[0] != "team_id" || missing[1] != "service_id" {
		t.Fatalf("expected missing=[team_id service_id], got %v", missing)
	}

	msg := missingConfigKeyErrorMessage(missing)
	if !strings.Contains(msg, "team_id") || !strings.Contains(msg, "service_id") {
		t.Fatalf("expected error message to name the specific missing keys, got: %s", msg)
	}
	if strings.Contains(msg, "key_id,") || strings.Contains(msg, ", key_id") {
		t.Fatalf("expected error message to NOT list key_id (it was present), got: %s", msg)
	}
	if !strings.Contains(msg, "WEATHERKIT_TEAM_ID") || !strings.Contains(msg, "WEATHERKIT_SERVICE_ID") {
		t.Fatalf("expected error message to mention the corresponding env vars, got: %s", msg)
	}
}

func TestResolveWeatherKitConfig_AllMissing(t *testing.T) {
	get := func(key string) (string, bool) { return "", false }

	_, missing := resolveWeatherKitConfig(get)
	if len(missing) != 4 {
		t.Fatalf("expected all 4 keys missing, got %v", missing)
	}

	msg := missingConfigKeyErrorMessage(missing)
	for _, want := range []string{"key_id", "team_id", "service_id", "private_key"} {
		if !strings.Contains(msg, want) {
			t.Errorf("expected error message to contain %q, got: %s", want, msg)
		}
	}
}

// --- WeatherKit response parsing / unit conversion ---

func TestParseWeatherKitResponse_MapsCannedResponseCorrectly(t *testing.T) {
	body, err := os.ReadFile("testdata/weatherkit_response_sample.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	out, err := parseWeatherKitResponse(body, 0)
	if err != nil {
		t.Fatalf("parseWeatherKitResponse returned error: %v", err)
	}

	// --- current ---
	if out.Current.Time != "2026-07-19T10:00:00Z" {
		t.Errorf("expected current.time=2026-07-19T10:00:00Z, got %q", out.Current.Time)
	}
	if out.Current.TemperatureC != 18.5 {
		t.Errorf("expected current.temperature_c=18.5, got %v", out.Current.TemperatureC)
	}
	if out.Current.Condition != "Clear" {
		t.Errorf("expected current.condition=Clear (pass-through as-is), got %q", out.Current.Condition)
	}
	// 18 kph / 3.6 = 5.0 m/s
	if out.Current.WindSpeedMS != 5.0 {
		t.Errorf("expected current.wind_speed_ms=5.0 (18kph->m/s), got %v", out.Current.WindSpeedMS)
	}
	// 36 kph / 3.6 = 10.0 m/s
	if out.Current.WindGustMS != 10.0 {
		t.Errorf("expected current.wind_gust_ms=10.0 (36kph->m/s), got %v", out.Current.WindGustMS)
	}
	if out.Current.WindDirectionDeg != 270 {
		t.Errorf("expected current.wind_direction_deg=270, got %v", out.Current.WindDirectionDeg)
	}
	// 0.25 fraction -> 25 pct
	if out.Current.PrecipitationChancePct != 25 {
		t.Errorf("expected current.precipitation_chance_pct=25, got %v", out.Current.PrecipitationChancePct)
	}

	// --- days ---
	if len(out.Days) != 2 {
		t.Fatalf("expected 2 forecast days, got %d", len(out.Days))
	}
	day0 := out.Days[0]
	if day0.Start != "2026-07-19T00:00:00Z" {
		t.Errorf("expected days[0].start=2026-07-19T00:00:00Z, got %q", day0.Start)
	}
	if day0.Condition != "MostlyClear" {
		t.Errorf("expected days[0].condition=MostlyClear, got %q", day0.Condition)
	}
	if day0.TempMaxC != 22.0 || day0.TempMinC != 14.0 {
		t.Errorf("expected days[0] temp range 14.0-22.0C, got %v-%v", day0.TempMinC, day0.TempMaxC)
	}
	// daytimeForecast.windSpeed=25.2kph preferred over day-level windSpeed=21.6kph
	// 25.2 / 3.6 = 7.0 m/s
	if day0.WindSpeedMS != 7.0 {
		t.Errorf("expected days[0].wind_speed_ms=7.0 (daytimeForecast preferred over day-level), got %v", day0.WindSpeedMS)
	}
	// 43.2 / 3.6 = 12.0 m/s
	if day0.WindGustMS != 12.0 {
		t.Errorf("expected days[0].wind_gust_ms=12.0, got %v", day0.WindGustMS)
	}
	if day0.WindDirectionDeg != 180 {
		t.Errorf("expected days[0].wind_direction_deg=180 (daytimeForecast preferred), got %v", day0.WindDirectionDeg)
	}
	if day0.PrecipitationChancePct != 10 {
		t.Errorf("expected days[0].precipitation_chance_pct=10, got %v", day0.PrecipitationChancePct)
	}
	if day0.Sunrise != "2026-07-19T06:00:00Z" || day0.Sunset != "2026-07-19T20:00:00Z" {
		t.Errorf("expected sunrise/sunset passed through as-is, got sunrise=%q sunset=%q", day0.Sunrise, day0.Sunset)
	}

	day1 := out.Days[1]
	// day1's daytimeForecast: windSpeed=14.4kph->4.0m/s,
	// windGustSpeedMax=28.8kph->8.0m/s, windDirection=90; no day-level
	// wind fields present at all for day1, so daytimeForecast is the only
	// source (also proves the daytimeForecast path works with no day-level
	// fallback data present).
	if day1.WindSpeedMS != 4.0 || day1.WindGustMS != 8.0 || day1.WindDirectionDeg != 90 {
		t.Errorf("expected days[1] wind from daytimeForecast (speed=4.0 gust=8.0 dir=90), got speed=%v gust=%v dir=%v", day1.WindSpeedMS, day1.WindGustMS, day1.WindDirectionDeg)
	}

	// --- hourly ---
	if len(out.Hourly) != 2 {
		t.Fatalf("expected 2 hourly entries, got %d", len(out.Hourly))
	}
	hour0 := out.Hourly[0]
	if hour0.Time != "2026-07-19T11:00:00Z" {
		t.Errorf("expected hourly[0].time=2026-07-19T11:00:00Z, got %q", hour0.Time)
	}
	if hour0.Condition != "PartlyCloudy" {
		t.Errorf("expected hourly[0].condition=PartlyCloudy, got %q", hour0.Condition)
	}
	if hour0.TemperatureC != 19.0 {
		t.Errorf("expected hourly[0].temperature_c=19.0, got %v", hour0.TemperatureC)
	}
	// 10.8 / 3.6 = 3.0 m/s
	if hour0.WindSpeedMS != 3.0 {
		t.Errorf("expected hourly[0].wind_speed_ms=3.0, got %v", hour0.WindSpeedMS)
	}
	// 21.6 / 3.6 = 6.0 m/s
	if hour0.WindGustMS != 6.0 {
		t.Errorf("expected hourly[0].wind_gust_ms=6.0, got %v", hour0.WindGustMS)
	}
	if hour0.WindDirectionDeg != 200 {
		t.Errorf("expected hourly[0].wind_direction_deg=200, got %v", hour0.WindDirectionDeg)
	}
	if hour0.PrecipitationChancePct != 5 {
		t.Errorf("expected hourly[0].precipitation_chance_pct=5, got %v", hour0.PrecipitationChancePct)
	}
	if hour0.PrecipitationMM != 0.2 {
		t.Errorf("expected hourly[0].precipitation_mm=0.2, got %v", hour0.PrecipitationMM)
	}
	if hour0.UVIndex != 6 {
		t.Errorf("expected hourly[0].uv_index=6, got %v", hour0.UVIndex)
	}
	if !hour0.IsDaylight {
		t.Errorf("expected hourly[0].is_daylight=true")
	}

	hour1 := out.Hourly[1]
	// hour1 has no windGust field at all -> sentinel-zero, left for the
	// host to fall back on (see mapForecastHours' doc comment).
	if hour1.WindGustMS != 0 {
		t.Errorf("expected hourly[1].wind_gust_ms=0 (absent from source, no self-fallback), got %v", hour1.WindGustMS)
	}
}

func TestParseWeatherKitResponse_CapsDaysAtInputDaysWhenPositive(t *testing.T) {
	body, err := os.ReadFile("testdata/weatherkit_response_sample.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	out, err := parseWeatherKitResponse(body, 1)
	if err != nil {
		t.Fatalf("parseWeatherKitResponse returned error: %v", err)
	}
	if len(out.Days) != 1 {
		t.Fatalf("expected days capped at 1, got %d", len(out.Days))
	}
}

func TestParseWeatherKitResponse_ErrorsOnMissingCurrentWeather(t *testing.T) {
	body := []byte(`{"forecastDaily":{"days":[{"forecastStart":"2026-07-19T00:00:00Z"}]}}`)
	if _, err := parseWeatherKitResponse(body, 0); err == nil {
		t.Fatalf("expected an error when currentWeather is missing")
	}
}

func TestParseWeatherKitResponse_ErrorsOnEmptyForecastDaily(t *testing.T) {
	body := []byte(`{"currentWeather":{"asOf":"2026-07-19T10:00:00Z"},"forecastDaily":{"days":[]}}`)
	if _, err := parseWeatherKitResponse(body, 0); err == nil {
		t.Fatalf("expected an error when forecastDaily.days is empty")
	}
}

func TestParseWeatherKitResponse_ErrorsOnUnparseableJSON(t *testing.T) {
	if _, err := parseWeatherKitResponse([]byte("not json"), 0); err == nil {
		t.Fatalf("expected an error for unparseable response body")
	}
}

// --- request URL ---

// WeatherKit rolls forecastDaily up on the timezone named in the request.
// The host buckets and labels days in the vessel's local zone, so requesting
// a different zone shifts every day summary by the offset and drops the
// record covering local midnight to that offset - which is exactly how a
// rainy morning at UTC+10 ended up displayed as a dry "today". The zone must
// therefore come from the caller, never be hardcoded.
func TestWeatherKitRequestURL_UsesCallerTimezoneAndMergedDataSets(t *testing.T) {
	url := weatherKitRequestURL(37.8199, -122.4783, "Etc/GMT-10")
	if !strings.Contains(url, "timezone=Etc%2FGMT-10") {
		t.Errorf("expected request URL to carry the caller's escaped timezone, got: %s", url)
	}
	if strings.Contains(url, "timezone=UTC") {
		t.Errorf("expected the hardcoded timezone=UTC to be gone, got: %s", url)
	}
	if !strings.Contains(url, "dataSets=currentWeather,forecastDaily,forecastHourly") {
		t.Errorf("expected request URL to request all three datasets in one call, got: %s", url)
	}
	if !strings.Contains(url, "37.8199") || !strings.Contains(url, "-122.4783") {
		t.Errorf("expected request URL to contain the lat/lon, got: %s", url)
	}
}

// An absent timezone is a host contract violation, not something to paper
// over with a UTC default - defaulting is what silently produced misaligned
// days in the first place. Fail loudly instead (AGENTS.md fallback policy).
func TestValidateFetchForecastInput_RejectsMissingTimezone(t *testing.T) {
	if err := validateFetchForecastInput(fetchForecastInput{Lat: -21.1, Lon: 149.2, Days: 10}); err == nil {
		t.Fatal("expected an error when timezone is absent, got nil")
	}
	if err := validateFetchForecastInput(fetchForecastInput{Lat: -21.1, Lon: 149.2, Days: 10, Timezone: "   "}); err == nil {
		t.Fatal("expected an error when timezone is blank, got nil")
	}
	if err := validateFetchForecastInput(fetchForecastInput{Lat: -21.1, Lon: 149.2, Days: 10, Timezone: "Etc/GMT-10"}); err != nil {
		t.Fatalf("expected a valid input to pass, got: %v", err)
	}
}

// --- absent precipitation must not read as 0% ---

// Real capture from Mackay, 2026-08-09: WeatherKit omitted
// currentWeather.precipitationChance entirely and reported 0.0 daily chance
// for the near-term days while it was actually drizzling. The plugin must
// distinguish "upstream said 0" from "upstream said nothing" - a -1 sentinel
// for the latter - so the UI can show "unavailable" instead of a confident
// 0%. See docs/adr/0035-weather-local-day-boundaries.md.
func TestParseWeatherKitResponse_AbsentPrecipitationChanceBecomesSentinel(t *testing.T) {
	body, err := os.ReadFile("testdata/weatherkit_response_dry_nearterm.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	out, err := parseWeatherKitResponse(body, 0)
	if err != nil {
		t.Fatalf("parseWeatherKitResponse failed: %v", err)
	}

	// currentWeather.precipitationChance is absent in this capture.
	if out.Current.PrecipitationChancePct != -1 {
		t.Errorf("expected absent current precipitationChance to map to -1, got %v", out.Current.PrecipitationChancePct)
	}

	// forecastDaily days DO carry an explicit 0.0 here - that is a real
	// reading and must survive as 0, not be turned into the sentinel.
	if out.Days[0].PrecipitationChancePct != 0 {
		t.Errorf("expected an explicit daily 0.0 to stay 0, got %v", out.Days[0].PrecipitationChancePct)
	}
	// ...and a real non-zero day still converts fraction -> percent.
	if out.Days[6].PrecipitationChancePct != 43 {
		t.Errorf("expected day 6 chance 0.43 -> 43, got %v", out.Days[6].PrecipitationChancePct)
	}
}

func TestMapForecastDays_MissingPrecipitationChanceBecomesSentinel(t *testing.T) {
	days, err := mapForecastDays([]weatherKitDayForecast{
		{ForecastStart: "2026-08-09T14:00:00Z", ConditionCode: "Drizzle"}, // no precipitationChance
	}, 0)
	if err != nil {
		t.Fatalf("mapForecastDays failed: %v", err)
	}
	if days[0].PrecipitationChancePct != -1 {
		t.Errorf("expected a missing daily precipitationChance to map to -1, got %v", days[0].PrecipitationChancePct)
	}
}

func TestMapForecastHours_MissingPrecipitationChanceBecomesSentinel(t *testing.T) {
	hours := mapForecastHours([]weatherKitHourForecast{
		{ForecastStart: "2026-08-09T14:00:00Z", ConditionCode: "Drizzle"}, // no precipitationChance
	})
	if len(hours) != 1 {
		t.Fatalf("expected 1 hour, got %d", len(hours))
	}
	if hours[0].PrecipitationChancePct != -1 {
		t.Errorf("expected a missing hourly precipitationChance to map to -1, got %v", hours[0].PrecipitationChancePct)
	}
}

// The old mapCurrentWeather fell back to precipitationIntensity (mm/hr) when
// precipitationChance was absent, writing a rainfall rate into a percentage
// field, then to other time windows' values. Both invent a reading the
// provider never gave; AGENTS.md's fallback policy forbids exactly that.
func TestMapCurrentWeather_DoesNotSubstituteIntensityForAbsentChance(t *testing.T) {
	intensity := 4.2
	dailyChance := 0.9
	out, err := mapCurrentWeather(weatherKitResponse{
		CurrentWeather: &weatherKitCurrentWeather{
			AsOf:                   "2026-08-09T20:00:00Z",
			ConditionCode:          "Drizzle",
			PrecipitationIntensity: &intensity,
		},
		ForecastDaily: &weatherKitForecastDaily{
			Days: []weatherKitDayForecast{{ForecastStart: "2026-08-09T14:00:00Z", PrecipitationChance: &dailyChance}},
		},
	})
	if err != nil {
		t.Fatalf("mapCurrentWeather failed: %v", err)
	}
	if out.PrecipitationChancePct != -1 {
		t.Errorf("expected absent current chance to stay absent (-1), got %v - a %v mm/hr intensity or another window's %v must not be substituted",
			out.PrecipitationChancePct, intensity, dailyChance)
	}
}
