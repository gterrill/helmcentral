package main

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/csv"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/golang-jwt/jwt"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"gopkg.in/yaml.v3"
)

const (
	defaultSignalKAddress  = "localhost"
	defaultSignalKPort     = 3000
	metersPerSecondToKnots = 1.943844
	defaultWindMaxAge      = 5 * time.Minute
)

type vesselStateData struct {
	Status               string
	Datetime             time.Time
	Depth                float64
	Latitude             float64
	Longitude            float64
	HeadingTrue          float64
	WindSpeedApparentKts float64
	WindAngleApparentDeg float64
	WindSide             string
	WindAngleRelativeDeg float64
}

type electricalStateData struct {
	Datetime          time.Time
	BatterySocPercent float64
	ChargingCurrentA  float64
	ChargingPowerW    float64
	SolarOutputW      float64
	ACOutputW         float64
	DC12VPowerW       float64
	DC12VCurrentA     float64
	DC24VVoltageV     float64
	ACLoadsW          float64
}

type tankLevelData struct {
	ID           string  `json:"id"`
	Label        string  `json:"label"`
	Category     string  `json:"category"`
	Kind         string  `json:"kind"`
	LevelPercent float64 `json:"level_percent"`
}

type weatherTodayData struct {
	Datetime            time.Time
	TemperatureF        float64
	Condition           string
	HighTempF           float64
	LowTempF            float64
	WindSpeedKts        float64
	WindDirection       string
	PrecipitationPct    float64
	CurrentTideHeightFt float64
	TideDirection       string
	HighTideTime        time.Time
	HighTideHeightFt    float64
	LowTideTime         time.Time
	LowTideHeightFt     float64
}

func main() {
	e := echo.New()
	port := getEnv("PORT", "8080")

	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete},
	}))

	// Routes
	e.GET("/api/health", healthCheck)
	e.GET("/api/vessel-state", vesselState)
	e.GET("/api/electrical-state", electricalState)
	e.GET("/api/tanks-state", tanksState)
	e.GET("/api/nearby-vessels", nearbyVessels)
	e.GET("/api/weather-today", weatherToday)
	e.GET("/api/settings/signalk", getSignalKSettingsHandler)
	e.POST("/api/settings/signalk", updateSignalKSettingsHandler)

	addr := fmt.Sprintf(":%s", port)
	log.Printf("Starting server on %s", addr)
	if err := e.Start(addr); err != nil && err != http.ErrServerClosed {
		log.Fatalf("error starting server: %v", err)
	}
}

func getEnv(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}

func healthCheck(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"status": "ok",
	})
}

func vesselState(c echo.Context) error {
	state := vesselStateData{
		Status:               getEnv("VESSEL_STATUS", "At Anchor"),
		Datetime:             time.Now().UTC(),
		Depth:                -1,
		Latitude:             -1,
		Longitude:            -1,
		HeadingTrue:          -1,
		WindSpeedApparentKts: -1,
		WindAngleApparentDeg: -1,
		WindAngleRelativeDeg: -1,
	}
	source := "backend-fallback"

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)
	vesselPath := getEnv("SIGNALK_VESSEL_PATH", "/signalk/v1/api/vessels/self")

	if signalkURL != "" {
		signalkState, err := fetchSignalKVesselState(signalkURL, vesselPath)
		if err == nil {
			state = signalkState
			source = "signalk"
		}
	}

	maxGust10mKts := 0.0
	maxGust1hKts := 0.0
	if state.WindSpeedApparentKts > 0 {
		maxGust10mKts = queryInfluxMaxWindGustKts("10m")
		maxGust1hKts = queryInfluxMaxWindGustKts("1h")
		if maxGust10mKts < 0 {
			maxGust10mKts = 0
		}
		if maxGust1hKts < 0 {
			maxGust1hKts = 0
		}
	}

	return c.JSON(http.StatusOK, map[string]any{
		"status":                  state.Status,
		"datetime":                state.Datetime.Format(time.RFC3339),
		"depth":                   state.Depth,
		"latitude":                state.Latitude,
		"longitude":               state.Longitude,
		"heading_true":            state.HeadingTrue,
		"wind_speed_apparent_kts": state.WindSpeedApparentKts,
		"wind_angle_apparent_deg": state.WindAngleApparentDeg,
		"wind_side":               state.WindSide,
		"wind_angle_relative_deg": state.WindAngleRelativeDeg,
		"max_gust_10m_kts":        maxGust10mKts,
		"max_gust_1h_kts":         maxGust1hKts,
		"source":                  source,
	})
}

func electricalState(c echo.Context) error {
	state := electricalStateData{
		Datetime:          time.Now().UTC(),
		BatterySocPercent: -1,
		ChargingCurrentA:  -1,
		ChargingPowerW:    -1,
		SolarOutputW:      -1,
		ACOutputW:         -1,
		DC12VPowerW:       -1,
		DC12VCurrentA:     -1,
		DC24VVoltageV:     -1,
		ACLoadsW:          -1,
	}
	source := "backend-fallback"

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)
	vesselPath := getEnv("SIGNALK_VESSEL_PATH", "/signalk/v1/api/vessels/self")

	if signalkURL != "" {
		electrical, fetchErr := fetchSignalKElectricalState(signalkURL, vesselPath)
		if fetchErr == nil {
			state = electrical
			source = "signalk"
		}
	}

	return c.JSON(http.StatusOK, map[string]any{
		"datetime":            state.Datetime.Format(time.RFC3339),
		"battery_soc_percent": state.BatterySocPercent,
		"charging_current_a":  state.ChargingCurrentA,
		"charging_power_w":    state.ChargingPowerW,
		"solar_output_w":      state.SolarOutputW,
		"ac_output_w":         state.ACOutputW,
		"dc_power_w":          state.DC12VPowerW,
		"dc_current_a":        state.DC12VCurrentA,
		"dc_12v_power_w":      state.DC12VPowerW,
		"dc_12v_current_a":    state.DC12VCurrentA,
		"dc_24v_voltage_v":    state.DC24VVoltageV,
		"ac_loads_w":          state.ACLoadsW,
		"source":              source,
	})
}

func tanksState(c echo.Context) error {
	now := time.Now().UTC()
	tanks := []tankLevelData{}
	source := "backend-fallback"

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)
	vesselPath := getEnv("SIGNALK_VESSEL_PATH", "/signalk/v1/api/vessels/self")
	labelOverrides := loadTankLabelOverrides(settingsPath)

	if signalkURL != "" {
		stateTanks, datetime, fetchErr := fetchSignalKTanksState(signalkURL, vesselPath, labelOverrides)
		if fetchErr == nil {
			tanks = stateTanks
			now = datetime
			source = "signalk"
		}
	}

	return c.JSON(http.StatusOK, map[string]any{
		"datetime": now.Format(time.RFC3339),
		"source":   source,
		"tanks":    tanks,
	})
}

type nearbyVessel struct {
	Name       string   `json:"name"`
	RangeFt    int      `json:"range_ft"`
	AgeSeconds int      `json:"age_seconds"`
	SogKnots   *float64 `json:"sog_knots,omitempty"`
}

func nearbyVessels(c echo.Context) error {
	source := "backend-fallback"
	now := time.Now().UTC()
	vessels := []nearbyVessel{}

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}
	ownVesselName := loadBoatName(settingsPath)

	signalkURL := buildSignalKURL(address, port)
	vesselPath := getEnv("SIGNALK_VESSEL_PATH", "/signalk/v1/api/vessels/self")
	vesselsPath := getEnv("SIGNALK_VESSELS_PATH", "/signalk/v1/api/vessels")

	if signalkURL != "" {
		signalkSelfName := fetchSignalKSelfName(signalkURL, vesselPath)
		excludedNames := []string{ownVesselName, signalkSelfName}

		state, selfErr := fetchSignalKVesselState(signalkURL, vesselPath)
		if selfErr == nil && state.Latitude >= -90 && state.Latitude <= 90 && state.Longitude >= -180 && state.Longitude <= 180 {
			nearby, nearbyErr := fetchSignalKNearbyVessels(signalkURL, vesselsPath, state.Latitude, state.Longitude, now, excludedNames)
			if nearbyErr == nil {
				vessels = nearby
				source = "signalk"
			}
		}
	}

	return c.JSON(http.StatusOK, map[string]any{
		"datetime": now.Format(time.RFC3339),
		"source":   source,
		"vessels":  vessels,
	})
}

func weatherToday(c echo.Context) error {
	// Demo data - replace with real WeatherKit data when credentials are configured
	state := weatherTodayData{
		Datetime:            time.Now().UTC(),
		TemperatureF:        72,
		Condition:           "Partly Cloudy",
		HighTempF:           76,
		LowTempF:            64,
		WindSpeedKts:        12.5,
		WindDirection:       "NE",
		PrecipitationPct:    15,
		CurrentTideHeightFt: 1.5,
		TideDirection:       "Rising",
	}

	// Get vessel location from SignalK
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)
	vesselPath := getEnv("SIGNALK_VESSEL_PATH", "/signalk/v1/api/vessels/self")

	if signalkURL != "" {
		vesselState, err := fetchSignalKVesselState(signalkURL, vesselPath)
		if err == nil && vesselState.Latitude >= -90 && vesselState.Latitude <= 90 &&
			vesselState.Longitude >= -180 && vesselState.Longitude <= 180 {
			// Fetch weather from WeatherKit
			weather, weatherErr := fetchWeatherKitData(vesselState.Latitude, vesselState.Longitude)
			if weatherErr == nil {
				state = weather
				state.Datetime = time.Now().UTC()
			} else {
				log.Printf("WeatherKit API error: %v", weatherErr)
			}
		}
	}

	// Parse tide data (placeholder for now - would come from a tide service)
	// For now using mock data that matches the UI
	state.CurrentTideHeightFt = 1.5
	state.TideDirection = "Rising"
	state.HighTideTime = time.Now().AddDate(0, 0, 0).Add(12*time.Hour + 57*time.Minute)
	state.HighTideHeightFt = 1.9
	state.LowTideTime = time.Now().AddDate(0, 0, 0).Add(19*time.Hour + 11*time.Minute)
	state.LowTideHeightFt = -0.1

	return c.JSON(http.StatusOK, map[string]any{
		"datetime":               state.Datetime.Format(time.RFC3339),
		"temperature_f":          state.TemperatureF,
		"condition":              state.Condition,
		"high_temp_f":            state.HighTempF,
		"low_temp_f":             state.LowTempF,
		"wind_speed_kts":         state.WindSpeedKts,
		"wind_direction":         state.WindDirection,
		"precipitation_pct":      state.PrecipitationPct,
		"current_tide_height_ft": state.CurrentTideHeightFt,
		"tide_direction":         state.TideDirection,
		"high_tide_time":         state.HighTideTime.Format(time.RFC3339),
		"high_tide_height_ft":    state.HighTideHeightFt,
		"low_tide_time":          state.LowTideTime.Format(time.RFC3339),
		"low_tide_height_ft":     state.LowTideHeightFt,
	})
}

func fetchWeatherKitData(latitude, longitude float64) (weatherTodayData, error) {
	data := weatherTodayData{
		TemperatureF:     -1,
		Condition:        "Unknown",
		HighTempF:        -1,
		LowTempF:         -1,
		WindSpeedKts:     -1,
		WindDirection:    "—",
		PrecipitationPct: -1,
	}

	keyID := getEnv("WEATHERKIT_KEY_ID", "")
	teamID := getEnv("WEATHERKIT_TEAM_ID", "")
	serviceID := getEnv("WEATHERKIT_SERVICE_ID", "")
	privateKeyPEM := getEnv("WEATHERKIT_PRIVATE_KEY", "")

	if keyID == "" || teamID == "" || serviceID == "" || privateKeyPEM == "" {
		return data, fmt.Errorf("WeatherKit credentials not configured")
	}

	token, err := generateWeatherKitJWT(keyID, teamID, serviceID, privateKeyPEM)
	if err != nil {
		return data, fmt.Errorf("failed to generate JWT: %v", err)
	}

	url := fmt.Sprintf(
		"https://weatherkit.apple.com/api/v1/weather?latitude=%.4f&longitude=%.4f&dataSets=currentWeather,forecastDaily&timezone=America/Los_Angeles",
		latitude, longitude,
	)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return data, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return data, fmt.Errorf("failed to fetch weather: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return data, fmt.Errorf("WeatherKit API returned %d: %s", resp.StatusCode, string(body))
	}

	// Log the response status and content type for debugging
	log.Printf("WeatherKit API status: %d, content-type: %s", resp.StatusCode, resp.Header.Get("Content-Type"))

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return data, fmt.Errorf("failed to parse response: %v", err)
		// Read response body for logging
		bodyBytes, _ := io.ReadAll(resp.Body)
		if len(bodyBytes) > 500 {
			log.Printf("WeatherKit API response (first 500 chars): %s", string(bodyBytes[:500]))
		} else {
			log.Printf("WeatherKit API response: %s", string(bodyBytes))
		}

		var result map[string]any
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			return data, fmt.Errorf("failed to parse response: %v", err)
		}
	}

	// Parse current weather
	if current, ok := result["currentWeather"].(map[string]any); ok {
		if temp, ok := current["temperature"].(float64); ok {
			data.TemperatureF = (temp * 9 / 5) + 32 // Convert Celsius to Fahrenheit
		}
		if condition, ok := current["conditionCode"].(string); ok {
			data.Condition = formatWeatherCondition(condition)
		}
		if windSpeed, ok := current["windSpeed"].(float64); ok {
			data.WindSpeedKts = windSpeed / 0.51444 // Convert m/s to knots
		}
		if windDir, ok := current["windDirection"].(float64); ok {
			data.WindDirection = degreesToDirection(windDir)
		}
		if precip, ok := current["precipitationChance"].(float64); ok {
			data.PrecipitationPct = precip * 100
		}
	}

	// Parse daily forecast for high/low temps
	if daily, ok := result["forecastDaily"].(map[string]any); ok {
		if days, ok := daily["days"].([]any); ok && len(days) > 0 {
			if day, ok := days[0].(map[string]any); ok {
				if high, ok := day["temperatureMax"].(float64); ok {
					data.HighTempF = (high * 9 / 5) + 32
				}
				if low, ok := day["temperatureMin"].(float64); ok {
					data.LowTempF = (low * 9 / 5) + 32
				}
			}
		}
	}

	return data, nil
}

func generateWeatherKitJWT(keyID, teamID, serviceID, privateKeyPEM string) (string, error) {
	// Parse the EC private key from PEM format
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return "", fmt.Errorf("failed to parse PEM block containing the key")
	}

	privateKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %v", err)
	}

	ecdsaKey, ok := privateKey.(*ecdsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("key is not an ECDSA key")
	}

	// Create JWT claims
	now := time.Now()
	exp := now.Add(time.Hour) // Token valid for 1 hour

	claims := jwt.MapClaims{
		"iss": teamID,
		"sub": serviceID,
		"aud": "https://weatherkit.apple.com",
		"iat": now.Unix(),
		"exp": exp.Unix(),
	}

	// Create token with ES256 (ECDSA SHA-256)
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = keyID

	// Sign with the ECDSA private key
	tokenString, err := token.SignedString(ecdsaKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %v", err)
	}

	return tokenString, nil
}

func formatWeatherCondition(code string) string {
	conditions := map[string]string{
		"clear":             "Clear",
		"cloudy":            "Cloudy",
		"dusty":             "Dusty",
		"foggy":             "Foggy",
		"haze":              "Hazy",
		"mostlyClear":       "Mostly Clear",
		"mostlyCloudy":      "Mostly Cloudy",
		"partlyCloudy":      "Partly Cloudy",
		"smoky":             "Smoky",
		"breezy":            "Breezy",
		"windy":             "Windy",
		"drizzle":           "Drizzle",
		"heavyRain":         "Heavy Rain",
		"rain":              "Rain",
		"snow":              "Snow",
		"sleet":             "Sleet",
		"freezingDrizzle":   "Freezing Drizzle",
		"freezingRain":      "Freezing Rain",
		"hail":              "Hail",
		"mixedRainAndSnow":  "Mixed Rain & Snow",
		"mixedRainAndSleet": "Mixed Rain & Sleet",
		"mixedSnowAndSleet": "Mixed Snow & Sleet",
		"thunderstorms":     "Thunderstorms",
		"heavySnow":         "Heavy Snow",
		"blizzard":          "Blizzard",
	}

	if condition, ok := conditions[code]; ok {
		return condition
	}
	return "Unknown"
}

func degreesToDirection(degrees float64) string {
	// Normalize to 0-360
	for degrees < 0 {
		degrees += 360
	}
	for degrees >= 360 {
		degrees -= 360
	}

	directions := []string{"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE",
		"S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"}

	// Each direction covers 22.5 degrees
	index := int((degrees+11.25)/22.5) % 16
	return directions[index]
}

func getSignalKSettingsHandler(c echo.Context) error {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	return c.JSON(http.StatusOK, map[string]any{
		"address": address,
		"port":    port,
		"url":     buildSignalKURL(address, port),
	})
}

func updateSignalKSettingsHandler(c echo.Context) error {
	var req struct {
		Address string `json:"address"`
		Port    int    `json:"port"`
	}

	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid request payload",
		})
	}

	address := strings.TrimSpace(req.Address)
	if address == "" {
		address = defaultSignalKAddress
	}

	port := req.Port
	if port <= 0 || port > 65535 {
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)
	vesselPath := getEnv("SIGNALK_VESSEL_PATH", "/signalk/v1/api/vessels/self")

	if _, err := fetchSignalKVesselState(signalkURL, vesselPath); err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{
			"error": fmt.Sprintf("unable to connect to SignalK at %s", signalkURL),
		})
	}

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	if err := saveSignalKSettings(settingsPath, address, port); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "connected to SignalK, but failed to persist settings",
		})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"address":   address,
		"port":      port,
		"url":       signalkURL,
		"connected": true,
	})
}

func fetchSignalKVesselState(signalkURL string, vesselPath string) (vesselStateData, error) {
	url := strings.TrimRight(signalkURL, "/") + "/" + strings.TrimLeft(vesselPath, "/")

	state := vesselStateData{
		Status:               "Unknown",
		Datetime:             time.Now().UTC(),
		Depth:                -1,
		Latitude:             -1,
		Longitude:            -1,
		HeadingTrue:          -1,
		WindSpeedApparentKts: -1,
		WindAngleApparentDeg: -1,
		WindAngleRelativeDeg: -1,
	}

	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		return state, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return state, fmt.Errorf("signalk returned status %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return state, err
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return state, err
	}

	state.Status = firstNonEmptyString(
		lookupString(payload, "navigation", "state", "value"),
		lookupString(payload, "navigation", "state"),
	)
	if state.Status == "" {
		state.Status = "Unknown"
	}

	datetimeString := firstNonEmptyString(
		lookupString(payload, "navigation", "datetime", "value"),
		lookupString(payload, "navigation", "datetime"),
		lookupString(payload, "timestamp"),
	)

	if datetimeString != "" {
		parsed, err := time.Parse(time.RFC3339, datetimeString)
		if err == nil {
			state.Datetime = parsed.UTC()
		}
	}

	state.Depth = lookupNumber(payload, "environment", "depth", "belowTransducer", "value")
	if state.Depth == -1 {
		state.Depth = lookupNumber(payload, "environment", "depth", "belowTransducer")
	}

	state.Latitude = lookupNumber(payload, "navigation", "position", "value", "latitude")
	if state.Latitude == -1 {
		state.Latitude = lookupNumber(payload, "navigation", "position", "latitude")
	}

	state.Longitude = lookupNumber(payload, "navigation", "position", "value", "longitude")
	if state.Longitude == -1 {
		state.Longitude = lookupNumber(payload, "navigation", "position", "longitude")
	}

	state.HeadingTrue = lookupNumber(payload, "navigation", "headingTrue", "value")
	if state.HeadingTrue == -1 {
		state.HeadingTrue = lookupNumber(payload, "navigation", "headingTrue")
	}

	if state.HeadingTrue >= 0 {
		if state.HeadingTrue <= 2*math.Pi {
			state.HeadingTrue = state.HeadingTrue * 180 / math.Pi
		}
		state.HeadingTrue = normalizeDegrees(state.HeadingTrue)
	}

	windSpeedApparent := lookupNumber(payload, "environment", "wind", "speedApparent", "value")
	if windSpeedApparent == -1 {
		windSpeedApparent = lookupNumber(payload, "environment", "wind", "speedApparent")
	}
	windTimestamp := firstNonEmptyString(
		lookupString(payload, "environment", "wind", "speedApparent", "timestamp"),
		lookupString(payload, "environment", "wind", "angleApparent", "timestamp"),
		lookupString(payload, "environment", "wind", "timestamp"),
	)

	windDataRecent := isRecentTimestamp(windTimestamp, defaultWindMaxAge)
	if !windDataRecent {
		windDataRecent = state.Datetime.After(time.Now().UTC().Add(-defaultWindMaxAge))
	}

	if windSpeedApparent >= 0 && windDataRecent {
		state.WindSpeedApparentKts = windSpeedApparent * metersPerSecondToKnots
	} else {
		state.WindSpeedApparentKts = 0
	}

	windAngleApparent := lookupNumber(payload, "environment", "wind", "angleApparent", "value")
	if windAngleApparent == -1 {
		windAngleApparent = lookupNumber(payload, "environment", "wind", "angleApparent")
	}
	if windAngleApparent != -1 && windDataRecent {
		if windAngleApparent >= -2*math.Pi && windAngleApparent <= 2*math.Pi {
			windAngleApparent = windAngleApparent * 180 / math.Pi
		}

		signedAngle := normalizeSignedDegrees(windAngleApparent)
		state.WindAngleApparentDeg = normalizeDegrees(signedAngle)
		state.WindAngleRelativeDeg = math.Abs(signedAngle)
		if signedAngle < 0 {
			state.WindSide = "port"
		} else {
			state.WindSide = "starboard"
		}
	} else {
		state.WindAngleApparentDeg = 0
		state.WindAngleRelativeDeg = 0
		state.WindSide = "starboard"
	}

	return state, nil
}

func fetchSignalKElectricalState(signalkURL string, vesselPath string) (electricalStateData, error) {
	url := strings.TrimRight(signalkURL, "/") + "/" + strings.TrimLeft(vesselPath, "/")

	state := electricalStateData{
		Datetime:          time.Now().UTC(),
		BatterySocPercent: -1,
		ChargingCurrentA:  -1,
		ChargingPowerW:    -1,
		SolarOutputW:      -1,
		ACOutputW:         -1,
		DC12VPowerW:       -1,
		DC12VCurrentA:     -1,
		DC24VVoltageV:     -1,
		ACLoadsW:          -1,
	}

	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		return state, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return state, fmt.Errorf("signalk returned status %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return state, err
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return state, err
	}

	datetimeString := firstNonEmptyString(
		lookupString(payload, "timestamp"),
		lookupString(payload, "navigation", "datetime", "value"),
		lookupString(payload, "navigation", "datetime"),
	)
	if datetimeString != "" {
		parsed, parseErr := time.Parse(time.RFC3339, datetimeString)
		if parseErr == nil {
			state.Datetime = parsed.UTC()
		}
	}

	soc := lookupFirstNumber(payload,
		[]string{"electrical", "batteries", "house", "capacity", "stateOfCharge", "value"},
		[]string{"electrical", "batteries", "house", "capacity", "stateOfCharge"},
		[]string{"electrical", "batteries", "service", "capacity", "stateOfCharge", "value"},
		[]string{"electrical", "batteries", "service", "capacity", "stateOfCharge"},
	)
	if soc == -1 {
		soc = lookupNumberFromAnyChild(payload,
			[]string{"electrical", "batteries"},
			[]string{"capacity", "stateOfCharge", "value"},
		)
	}
	if soc >= 0 {
		if soc <= 1 {
			soc *= 100
		}
		state.BatterySocPercent = math.Max(0, math.Min(100, roundTo1(soc)))
	}

	batteryVoltage := lookupFirstNumber(payload,
		[]string{"electrical", "venus", "batteryVoltage", "value"},
		[]string{"electrical", "venus", "batteryVoltage"},
		[]string{"electrical", "batteries", "house", "voltage", "value"},
		[]string{"electrical", "batteries", "house", "voltage"},
		[]string{"electrical", "batteries", "service", "voltage", "value"},
		[]string{"electrical", "batteries", "service", "voltage"},
	)
	if batteryVoltage == -1 {
		batteryVoltage = lookupNumberFromAnyChild(payload,
			[]string{"electrical", "batteries"},
			[]string{"voltage", "value"},
		)
	}

	current := lookupFirstNumber(payload,
		[]string{"electrical", "batteries", "house", "current", "value"},
		[]string{"electrical", "batteries", "house", "current"},
		[]string{"electrical", "batteries", "service", "current", "value"},
		[]string{"electrical", "batteries", "service", "current"},
	)
	if current == -1 {
		current = lookupNumberFromAnyChild(payload,
			[]string{"electrical", "batteries"},
			[]string{"current", "value"},
		)
	}
	if current == -1 {
		state.ChargingCurrentA = -1
	} else if current >= 0 {
		state.ChargingCurrentA = roundTo1(current)
	} else {
		state.ChargingCurrentA = 0
	}

	power := lookupFirstNumber(payload,
		[]string{"electrical", "batteries", "house", "power", "value"},
		[]string{"electrical", "batteries", "house", "power"},
		[]string{"electrical", "batteries", "service", "power", "value"},
		[]string{"electrical", "batteries", "service", "power"},
	)
	if power == -1 {
		power = lookupNumberFromAnyChild(payload,
			[]string{"electrical", "batteries"},
			[]string{"power", "value"},
		)
	}
	if power == -1 {
		state.ChargingPowerW = -1
	} else if power >= 0 {
		state.ChargingPowerW = roundTo1(power)
	} else {
		state.ChargingPowerW = 0
	}

	if state.ChargingPowerW == -1 && state.ChargingCurrentA >= 0 && batteryVoltage > 0 {
		state.ChargingPowerW = roundTo1(state.ChargingCurrentA * batteryVoltage)
	}
	if state.ChargingCurrentA == -1 && state.ChargingPowerW >= 0 && batteryVoltage > 0 {
		state.ChargingCurrentA = roundTo1(state.ChargingPowerW / batteryVoltage)
	}

	solar := lookupFirstNumber(payload,
		[]string{"electrical", "solar", "0", "panelPower", "value"},
		[]string{"electrical", "solar", "0", "panelPower"},
		[]string{"electrical", "solar", "0", "power", "value"},
		[]string{"electrical", "solar", "0", "power"},
		[]string{"electrical", "solar", "panelPower", "value"},
		[]string{"electrical", "solar", "panelPower"},
	)
	if solar == -1 {
		solar = lookupNumberFromAnyChild(payload,
			[]string{"electrical", "solar"},
			[]string{"panelPower", "value"},
		)
	}
	if solar >= 0 {
		state.SolarOutputW = roundTo1(solar)
	}

	acOutput := lookupFirstNumber(payload,
		[]string{"electrical", "inverters", "0", "ac", "output", "power", "value"},
		[]string{"electrical", "inverters", "0", "ac", "output", "power"},
		[]string{"electrical", "inverters", "0", "acout", "power", "value"},
		[]string{"electrical", "inverters", "0", "acout", "power"},
		[]string{"electrical", "inverters", "0", "acOutputPower", "value"},
		[]string{"electrical", "inverters", "0", "acOutputPower"},
		[]string{"electrical", "alternators", "0", "ac", "output", "power", "value"},
		[]string{"electrical", "alternators", "0", "ac", "output", "power"},
	)
	if acOutput == -1 {
		acOutput = lookupNumberFromAnyChild(payload,
			[]string{"electrical", "inverters"},
			[]string{"acout", "power", "value"},
		)
	}
	if acOutput >= 0 {
		state.ACOutputW = roundTo1(acOutput)
	}

	dc12Power := lookupFirstNumber(payload,
		[]string{"electrical", "venus", "dcPower", "value"},
		[]string{"electrical", "venus", "dcPower"},
		[]string{"electrical", "dc", "12v", "power", "value"},
		[]string{"electrical", "dc", "12v", "power"},
		[]string{"electrical", "loads", "12v", "power", "value"},
		[]string{"electrical", "loads", "12v", "power"},
	)
	if dc12Power >= 0 {
		state.DC12VPowerW = roundTo1(dc12Power)
	}

	dc12Current := lookupFirstNumber(payload,
		[]string{"electrical", "venus", "dcCurrent", "value"},
		[]string{"electrical", "venus", "dcCurrent"},
		[]string{"electrical", "dc", "12v", "current", "value"},
		[]string{"electrical", "dc", "12v", "current"},
		[]string{"electrical", "loads", "12v", "current", "value"},
		[]string{"electrical", "loads", "12v", "current"},
	)
	if dc12Current >= 0 {
		state.DC12VCurrentA = roundTo1(dc12Current)
	} else if state.DC12VPowerW >= 0 && batteryVoltage > 0 {
		state.DC12VCurrentA = roundTo1(state.DC12VPowerW / batteryVoltage)
	}

	dc24Voltage := lookupFirstNumber(payload,
		[]string{"electrical", "dc", "24v", "voltage", "value"},
		[]string{"electrical", "dc", "24v", "voltage"},
		[]string{"electrical", "batteries", "starter", "voltage", "value"},
		[]string{"electrical", "batteries", "starter", "voltage"},
	)
	if dc24Voltage >= 0 {
		state.DC24VVoltageV = roundTo1(dc24Voltage)
	} else if batteryVoltage >= 0 {
		state.DC24VVoltageV = roundTo1(batteryVoltage)
	}

	acLoads := lookupFirstNumber(payload,
		[]string{"electrical", "ac", "loads", "total", "power", "value"},
		[]string{"electrical", "ac", "loads", "total", "power"},
		[]string{"electrical", "ac", "loads", "power", "value"},
		[]string{"electrical", "ac", "loads", "power"},
	)
	if acLoads >= 0 {
		state.ACLoadsW = roundTo1(acLoads)
	} else if state.ACOutputW >= 0 {
		state.ACLoadsW = state.ACOutputW
	}

	return state, nil
}

func fetchSignalKNearbyVessels(signalkURL string, vesselsPath string, selfLatitude float64, selfLongitude float64, now time.Time, excludedNames []string) ([]nearbyVessel, error) {
	url := strings.TrimRight(signalkURL, "/") + "/" + strings.TrimLeft(vesselsPath, "/")

	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("signalk returned status %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}

	vessels := make([]nearbyVessel, 0, len(payload))
	for vesselID, raw := range payload {
		vesselMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		if vesselID == "self" {
			continue
		}

		latitude := lookupNumber(vesselMap, "navigation", "position", "value", "latitude")
		if latitude == -1 {
			latitude = lookupNumber(vesselMap, "navigation", "position", "latitude")
		}

		longitude := lookupNumber(vesselMap, "navigation", "position", "value", "longitude")
		if longitude == -1 {
			longitude = lookupNumber(vesselMap, "navigation", "position", "longitude")
		}

		if latitude < -90 || latitude > 90 || longitude < -180 || longitude > 180 {
			continue
		}

		name := firstNonEmptyString(
			lookupString(vesselMap, "name"),
			lookupString(vesselMap, "design", "name"),
		)
		if name == "" {
			name = compactVesselID(vesselID)
		}
		if matchesExcludedName(name, excludedNames) {
			continue
		}

		rangeFeet := int(math.Round(haversineMeters(selfLatitude, selfLongitude, latitude, longitude) * 3.28084))
		if rangeFeet < 30 {
			continue
		}

		ageSeconds := 0
		timestamp := firstNonEmptyString(
			lookupString(vesselMap, "navigation", "position", "timestamp"),
			lookupString(vesselMap, "navigation", "position", "value", "timestamp"),
			lookupString(vesselMap, "timestamp"),
		)
		if timestamp != "" {
			parsed, parseErr := time.Parse(time.RFC3339, timestamp)
			if parseErr == nil {
				delta := int(now.Sub(parsed.UTC()).Seconds())
				if delta > 0 {
					ageSeconds = delta
				}
			}
		}

		var sogKnots *float64
		sog := lookupNumber(vesselMap, "navigation", "speedOverGround", "value")
		if sog >= 0 {
			knots := math.Round((sog*1.943844)*10) / 10
			sogKnots = &knots
		}

		vessels = append(vessels, nearbyVessel{
			Name:       strings.ToUpper(name),
			RangeFt:    rangeFeet,
			AgeSeconds: ageSeconds,
			SogKnots:   sogKnots,
		})
	}

	sort.Slice(vessels, func(i int, j int) bool {
		return vessels[i].RangeFt < vessels[j].RangeFt
	})

	if len(vessels) > 10 {
		vessels = vessels[:10]
	}

	return vessels, nil
}

func fetchSignalKTanksState(signalkURL string, vesselPath string, labelOverrides map[string]string) ([]tankLevelData, time.Time, error) {
	url := strings.TrimRight(signalkURL, "/") + "/" + strings.TrimLeft(vesselPath, "/")

	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		return nil, time.Now().UTC(), err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, time.Now().UTC(), fmt.Errorf("signalk returned status %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, time.Now().UTC(), err
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, time.Now().UTC(), err
	}

	datetime := time.Now().UTC()
	datetimeString := firstNonEmptyString(
		lookupString(payload, "navigation", "datetime", "value"),
		lookupString(payload, "navigation", "datetime"),
		lookupString(payload, "timestamp"),
	)
	if datetimeString != "" {
		parsed, parseErr := time.Parse(time.RFC3339, datetimeString)
		if parseErr == nil {
			datetime = parsed.UTC()
		}
	}

	tanksMap := lookupAnyMap(payload, "tanks")
	if tanksMap == nil {
		return []tankLevelData{}, datetime, nil
	}

	categoryOrder := []string{"freshWater", "fuel", "blackWater", "greyWater", "liveWell", "lubrication", "water", "wasteWater"}
	knownCategory := map[string]struct{}{}
	for _, category := range categoryOrder {
		knownCategory[category] = struct{}{}
	}

	orderedCategories := make([]string, 0, len(tanksMap))
	for _, category := range categoryOrder {
		if _, ok := tanksMap[category]; ok {
			orderedCategories = append(orderedCategories, category)
		}
	}
	for category := range tanksMap {
		if _, ok := knownCategory[category]; ok {
			continue
		}
		orderedCategories = append(orderedCategories, category)
	}

	tanks := make([]tankLevelData, 0)
	for _, category := range orderedCategories {
		categoryRaw, ok := tanksMap[category]
		if !ok {
			continue
		}

		categoryEntries, ok := categoryRaw.(map[string]any)
		if !ok {
			continue
		}

		entryIDs := make([]string, 0, len(categoryEntries))
		for entryID := range categoryEntries {
			entryIDs = append(entryIDs, entryID)
		}
		sort.Strings(entryIDs)

		for _, entryID := range entryIDs {
			rawEntry := categoryEntries[entryID]
			entry, ok := rawEntry.(map[string]any)
			if !ok {
				continue
			}

			level := lookupFirstNumber(entry,
				[]string{"currentLevel", "value"},
				[]string{"currentLevel"},
			)
			if level < 0 {
				continue
			}

			if level <= 1 {
				level *= 100
			}
			level = math.Max(0, math.Min(100, roundTo1(level)))

			label := firstNonEmptyString(
				lookupString(entry, "name", "value"),
				lookupString(entry, "name"),
				lookupString(entry, "displayName", "value"),
				lookupString(entry, "displayName"),
			)
			override := tankLabelOverride(labelOverrides, category, entryID)
			if override != "" {
				label = override
			}
			if label == "" {
				label = buildTankLabel(category, entryID)
			}

			tanks = append(tanks, tankLevelData{
				ID:           category + "." + entryID,
				Label:        strings.TrimSpace(label),
				Category:     category,
				Kind:         tankKindFromCategory(category),
				LevelPercent: level,
			})
		}
	}

	return tanks, datetime, nil
}

func loadTankLabelOverrides(settingsPath string) map[string]string {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return map[string]string{}
	}

	uiMap, ok := settings["ui"].(map[string]any)
	if !ok {
		return map[string]string{}
	}

	rawLabels, ok := uiMap["tank_labels"].(map[string]any)
	if !ok {
		return map[string]string{}
	}

	labels := map[string]string{}
	for key, value := range rawLabels {
		label, ok := value.(string)
		if !ok {
			continue
		}

		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		normalizedLabel := strings.TrimSpace(label)
		if normalizedKey == "" || normalizedLabel == "" {
			continue
		}

		labels[normalizedKey] = normalizedLabel
	}

	return labels
}

func tankLabelOverride(overrides map[string]string, category string, entryID string) string {
	if len(overrides) == 0 {
		return ""
	}

	category = strings.ToLower(strings.TrimSpace(category))
	entryID = strings.TrimSpace(entryID)

	keys := []string{
		category + "." + strings.ToLower(entryID),
		category + "/" + strings.ToLower(entryID),
		strings.ToLower(entryID),
	}

	for _, key := range keys {
		if value, ok := overrides[key]; ok {
			return value
		}
	}

	return ""
}

func lookupString(payload map[string]any, keys ...string) string {
	var current any = payload
	for _, key := range keys {
		asMap, ok := current.(map[string]any)
		if !ok {
			return ""
		}

		next, ok := asMap[key]
		if !ok {
			return ""
		}

		current = next
	}

	value, ok := current.(string)
	if !ok {
		return ""
	}

	return value
}

func lookupNumber(payload map[string]any, keys ...string) float64 {
	var current any = payload
	for _, key := range keys {
		asMap, ok := current.(map[string]any)
		if !ok {
			return -1
		}

		next, ok := asMap[key]
		if !ok {
			return -1
		}

		current = next
	}

	switch v := current.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	default:
		return -1
	}
}

func lookupFirstNumber(payload map[string]any, paths ...[]string) float64 {
	for _, path := range paths {
		value := lookupNumber(payload, path...)
		if value != -1 {
			return value
		}
	}

	return -1
}

func lookupNumberFromAnyChild(payload map[string]any, prefix []string, suffix []string) float64 {
	parent := lookupAnyMap(payload, prefix...)
	if parent == nil {
		return -1
	}

	for _, rawChild := range parent {
		child, ok := rawChild.(map[string]any)
		if !ok {
			continue
		}

		value := lookupNumber(child, suffix...)
		if value != -1 {
			return value
		}
	}

	return -1
}

func lookupAnyMap(payload map[string]any, keys ...string) map[string]any {
	var current any = payload
	for _, key := range keys {
		asMap, ok := current.(map[string]any)
		if !ok {
			return nil
		}

		next, ok := asMap[key]
		if !ok {
			return nil
		}

		current = next
	}

	result, ok := current.(map[string]any)
	if !ok {
		return nil
	}

	return result
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}

func buildSignalKURL(address string, port int) string {
	trimmed := strings.TrimSpace(address)
	if trimmed == "" {
		trimmed = defaultSignalKAddress
	}

	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return strings.TrimRight(trimmed, "/")
	}

	if port <= 0 || port > 65535 {
		port = defaultSignalKPort
	}

	return fmt.Sprintf("http://%s:%d", trimmed, port)
}

func loadSignalKSettings(settingsPath string) (string, int, error) {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return "", 0, err
	}

	signalkMap, ok := settings["signalk"].(map[string]any)
	if !ok {
		return defaultSignalKAddress, defaultSignalKPort, nil
	}

	address, _ := signalkMap["address"].(string)
	if strings.TrimSpace(address) == "" {
		address = defaultSignalKAddress
	}

	port := coercePort(signalkMap["port"])
	if port <= 0 {
		port = defaultSignalKPort
	}

	return address, port, nil
}

func saveSignalKSettings(settingsPath string, address string, port int) error {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return err
	}

	signalkMap := map[string]any{
		"address": strings.TrimSpace(address),
		"port":    port,
	}
	settings["signalk"] = signalkMap

	content, err := yaml.Marshal(settings)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}

	return os.WriteFile(settingsPath, content, 0o644)
}

func readSettings(settingsPath string) (map[string]any, error) {
	content, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}

		return nil, err
	}

	if len(content) == 0 {
		return map[string]any{}, nil
	}

	settings := map[string]any{}
	if err := yaml.Unmarshal(content, &settings); err != nil {
		return nil, err
	}

	return settings, nil
}

func coercePort(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		var parsed int
		if _, err := fmt.Sscanf(strings.TrimSpace(typed), "%d", &parsed); err == nil {
			return parsed
		}
	}

	return 0
}

func normalizeDegrees(value float64) float64 {
	normalized := math.Mod(value, 360)
	if normalized < 0 {
		normalized += 360
	}

	return normalized
}

func normalizeSignedDegrees(value float64) float64 {
	normalized := normalizeDegrees(value)
	if normalized > 180 {
		normalized -= 360
	}

	return normalized
}

func roundTo1(value float64) float64 {
	return math.Round(value*10) / 10
}

func haversineMeters(lat1 float64, lon1 float64, lat2 float64, lon2 float64) float64 {
	const earthRadiusMeters = 6371000.0

	lat1Rad := lat1 * math.Pi / 180
	lon1Rad := lon1 * math.Pi / 180
	lat2Rad := lat2 * math.Pi / 180
	lon2Rad := lon2 * math.Pi / 180

	deltaLat := lat2Rad - lat1Rad
	deltaLon := lon2Rad - lon1Rad

	a := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) + math.Cos(lat1Rad)*math.Cos(lat2Rad)*math.Sin(deltaLon/2)*math.Sin(deltaLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadiusMeters * c
}

func compactVesselID(vesselID string) string {
	trimmed := strings.TrimSpace(vesselID)
	if trimmed == "" {
		return "UNKNOWN"
	}

	segments := strings.Split(trimmed, ":")
	return segments[len(segments)-1]
}

func loadBoatName(settingsPath string) string {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return ""
	}

	boatMap, ok := settings["boat"].(map[string]any)
	if !ok {
		return ""
	}

	name, _ := boatMap["name"].(string)
	return strings.TrimSpace(name)
}

func fetchSignalKSelfName(signalkURL string, vesselPath string) string {
	url := strings.TrimRight(signalkURL, "/") + "/" + strings.TrimLeft(vesselPath, "/")

	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		return ""
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return ""
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return ""
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}

	return strings.TrimSpace(firstNonEmptyString(
		lookupString(payload, "name"),
		lookupString(payload, "design", "name"),
	))
}

func matchesExcludedName(candidate string, excludedNames []string) bool {
	trimmedCandidate := strings.TrimSpace(candidate)
	if trimmedCandidate == "" {
		return false
	}

	for _, excluded := range excludedNames {
		if excluded != "" && strings.EqualFold(trimmedCandidate, strings.TrimSpace(excluded)) {
			return true
		}
	}

	return false
}

func queryInfluxMaxWindGustKts(window string) float64 {
	influxURL := trimEnvValue(getEnv("INFLUXDB_URL", ""))
	org := trimEnvValue(getEnv("INFLUXDB_ORG", ""))
	bucket := trimEnvValue(getEnv("INFLUXDB_BUCKET", ""))
	token := trimEnvValue(getEnv("INFLUXDB_TOKEN", ""))
	measurement := trimEnvValue(getEnv("INFLUX_WIND_MEASUREMENT", "environment.wind.speedApparent"))
	field := trimEnvValue(getEnv("INFLUX_WIND_FIELD", "value"))

	if influxURL == "" || org == "" || bucket == "" || token == "" {
		return -1
	}

	flux := fmt.Sprintf(
		`from(bucket: %q) |> range(start: -%s) |> filter(fn: (r) => r._measurement == %q and r._field == %q) |> max(column: "_value") |> keep(columns: ["_value"])`,
		bucket,
		window,
		measurement,
		field,
	)

	bodyBytes, err := json.Marshal(map[string]string{
		"query": flux,
		"type":  "flux",
	})
	if err != nil {
		return -1
	}

	queryURL := strings.TrimRight(influxURL, "/") + "/api/v2/query?org=" + url.QueryEscape(org)
	request, err := http.NewRequest(http.MethodPost, queryURL, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return -1
	}

	request.Header.Set("Authorization", "Token "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/csv")

	client := &http.Client{Timeout: 4 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return -1
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return -1
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return -1
	}

	csvReader := csv.NewReader(strings.NewReader(string(body)))
	records, err := csvReader.ReadAll()
	if err != nil {
		return -1
	}

	maxMS := -1.0
	for _, record := range records {
		if len(record) == 0 || strings.HasPrefix(record[0], "#") {
			continue
		}

		valueCandidate := strings.TrimSpace(record[len(record)-1])
		value, parseErr := parseFloat(valueCandidate)
		if parseErr == nil {
			maxMS = value
		}
	}

	if maxMS < 0 {
		return -1
	}

	return math.Round((maxMS*metersPerSecondToKnots)*10) / 10
}

func trimEnvValue(value string) string {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.Trim(trimmed, `"`)
	return strings.TrimSpace(trimmed)
}

func parseFloat(value string) (float64, error) {
	var parsed float64
	_, err := fmt.Sscanf(value, "%f", &parsed)
	if err != nil {
		return 0, err
	}

	return parsed, nil
}

func buildTankLabel(category string, entryID string) string {
	base := humanizeCategory(category)
	if strings.TrimSpace(entryID) == "" {
		return base
	}

	return fmt.Sprintf("%s %s", base, strings.ToUpper(strings.TrimSpace(entryID)))
}

func tankKindFromCategory(category string) string {
	normalized := strings.ToLower(strings.TrimSpace(category))
	if strings.Contains(normalized, "fuel") {
		return "fuel"
	}

	if strings.Contains(normalized, "black") || strings.Contains(normalized, "grey") || strings.Contains(normalized, "waste") || strings.Contains(normalized, "sewage") || strings.Contains(normalized, "holding") {
		return "waste"
	}

	return "water"
}

func humanizeCategory(category string) string {
	if strings.TrimSpace(category) == "" {
		return "Tank"
	}

	r := strings.NewReplacer(
		"freshWater", "Fresh Water",
		"blackWater", "Black Water",
		"greyWater", "Grey Water",
		"wasteWater", "Waste Water",
		"liveWell", "Live Well",
	)
	converted := r.Replace(category)

	if strings.EqualFold(converted, category) {
		converted = strings.ReplaceAll(converted, "_", " ")
	}

	parts := strings.Fields(converted)
	for i, part := range parts {
		if len(part) == 0 {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
	}

	return strings.Join(parts, " ")
}

func isRecentTimestamp(value string, maxAge time.Duration) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}

	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return false
	}

	age := time.Since(parsed.UTC())
	return age >= 0 && age <= maxAge
}
