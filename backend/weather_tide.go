package main

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang-jwt/jwt"
	"github.com/labstack/echo/v4"
)

const (
	weatherTodayCacheTTL    = 10 * time.Minute
	defaultWeatherCacheFile = "cache/weather_today_cache.json"
	defaultTideCacheFile    = "cache/tide_cache.json"
	kphToKnots              = 0.539957
)

type tideCache struct {
	mu   sync.RWMutex
	data map[string]tideData
}

type tideData struct {
	currentHeight float64
	direction     string
	highTime      time.Time
	highHeight    float64
	lowTime       time.Time
	lowHeight     float64
	cachedAt      time.Time
}

type tideDataDisk struct {
	CurrentHeight float64   `json:"current_height"`
	Direction     string    `json:"direction"`
	HighTime      time.Time `json:"high_time"`
	HighHeight    float64   `json:"high_height"`
	LowTime       time.Time `json:"low_time"`
	LowHeight     float64   `json:"low_height"`
	CachedAt      time.Time `json:"cached_at"`
}

var tideCacheStore = &tideCache{data: make(map[string]tideData)}

type weatherTodayCache struct {
	mu   sync.RWMutex
	data map[string]weatherTodayCacheEntry
}

type weatherTodayCacheEntry struct {
	state    weatherTodayData
	cachedAt time.Time
}

type weatherTodayCacheEntryDisk struct {
	State    weatherTodayData `json:"state"`
	CachedAt time.Time        `json:"cached_at"`
}

var weatherTodayCacheStore = &weatherTodayCache{data: make(map[string]weatherTodayCacheEntry)}

type jsonCacheDescriptor struct {
	Name     string
	EnvKey   string
	Fallback string
	TTL      time.Duration
}

type cacheInfoResponse struct {
	Name            string  `json:"name"`
	FilePath        string  `json:"file_path"`
	TTLSeconds      int64   `json:"ttl_seconds"`
	Exists          bool    `json:"exists"`
	SizeBytes       int64   `json:"size_bytes"`
	ModifiedAt      *string `json:"modified_at"`
	InMemoryEntries int     `json:"in_memory_entries"`
	CacheHits       uint64  `json:"cache_hits"`
	CacheMisses     uint64  `json:"cache_misses"`
}

var weatherTodayCacheHits uint64
var weatherTodayCacheMisses uint64
var tideCacheHits uint64
var tideCacheMisses uint64

var jsonCacheDescriptors = []jsonCacheDescriptor{
	{Name: "weather_today", EnvKey: "WEATHER_TODAY_CACHE_FILE", Fallback: defaultWeatherCacheFile, TTL: weatherTodayCacheTTL},
	{Name: "tide", EnvKey: "TIDE_CACHE_FILE", Fallback: defaultTideCacheFile, TTL: 72 * time.Hour},
}

func listCaches(c echo.Context) error {
	result := make([]cacheInfoResponse, 0, len(jsonCacheDescriptors))

	for _, descriptor := range jsonCacheDescriptors {
		filePath := cacheFilePath(descriptor.EnvKey, descriptor.Fallback)
		info, err := os.Stat(filePath)
		exists := err == nil
		sizeBytes := int64(0)
		var modifiedAt *string

		if exists {
			sizeBytes = info.Size()
			timestamp := info.ModTime().UTC().Format(time.RFC3339)
			modifiedAt = &timestamp
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("Failed to stat cache file %s: %v", filePath, err)
		}

		inMemoryEntries := 0
		cacheHits := uint64(0)
		cacheMisses := uint64(0)
		switch descriptor.Name {
		case "weather_today":
			weatherTodayCacheStore.mu.RLock()
			inMemoryEntries = len(weatherTodayCacheStore.data)
			weatherTodayCacheStore.mu.RUnlock()
			cacheHits = atomic.LoadUint64(&weatherTodayCacheHits)
			cacheMisses = atomic.LoadUint64(&weatherTodayCacheMisses)
		case "tide":
			tideCacheStore.mu.RLock()
			inMemoryEntries = len(tideCacheStore.data)
			tideCacheStore.mu.RUnlock()
			cacheHits = atomic.LoadUint64(&tideCacheHits)
			cacheMisses = atomic.LoadUint64(&tideCacheMisses)
		}

		result = append(result, cacheInfoResponse{
			Name:            descriptor.Name,
			FilePath:        filePath,
			TTLSeconds:      int64(descriptor.TTL / time.Second),
			Exists:          exists,
			SizeBytes:       sizeBytes,
			ModifiedAt:      modifiedAt,
			InMemoryEntries: inMemoryEntries,
			CacheHits:       cacheHits,
			CacheMisses:     cacheMisses,
		})
	}

	return c.JSON(http.StatusOK, result)
}

func invalidateCache(c echo.Context) error {
	name := strings.TrimSpace(c.Param("name"))
	if name == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "missing cache name"})
	}

	var descriptor *jsonCacheDescriptor
	for idx := range jsonCacheDescriptors {
		if jsonCacheDescriptors[idx].Name == name {
			descriptor = &jsonCacheDescriptors[idx]
			break
		}
	}

	if descriptor == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "cache not found"})
	}

	switch descriptor.Name {
	case "weather_today":
		weatherTodayCacheStore.mu.Lock()
		weatherTodayCacheStore.data = make(map[string]weatherTodayCacheEntry)
		weatherTodayCacheStore.mu.Unlock()
	case "tide":
		tideCacheStore.mu.Lock()
		tideCacheStore.data = make(map[string]tideData)
		tideCacheStore.mu.Unlock()
	}

	filePath := cacheFilePath(descriptor.EnvKey, descriptor.Fallback)
	if err := os.Remove(filePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to remove cache file: %v", err)})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"status": "ok",
		"cache":  descriptor.Name,
	})
}

type weatherTodayData struct {
	Datetime         time.Time
	TemperatureF     float64
	Condition        string
	HighTempF        float64
	LowTempF         float64
	WindSpeedKts     float64
	WindGustKts      float64
	WindDirection    string
	PrecipitationPct float64
}

type weatherForecastDayData struct {
	Date             string
	DayName          string
	Condition        string
	HighTempF        float64
	LowTempF         float64
	WindSpeedKts     float64
	WindGustKts      float64
	WindDirection    string
	PrecipitationPct float64
}

type tideTodayData struct {
	Datetime            time.Time
	CurrentTideHeightFt float64
	TideDirection       string
	HighTideTime        time.Time
	HighTideHeightFt    float64
	LowTideTime         time.Time
	LowTideHeightFt     float64
}

type weatherTodayResponse struct {
	Datetime         string  `json:"datetime"`
	TemperatureF     float64 `json:"temperature_f"`
	Condition        string  `json:"condition"`
	HighTempF        float64 `json:"high_temp_f"`
	LowTempF         float64 `json:"low_temp_f"`
	WindSpeedKts     float64 `json:"wind_speed_kts"`
	WindGustKts      float64 `json:"wind_gust_kts"`
	WindDirection    string  `json:"wind_direction"`
	PrecipitationPct float64 `json:"precipitation_pct"`
}

type weatherTodayETagData struct {
	TemperatureF     float64 `json:"temperature_f"`
	Condition        string  `json:"condition"`
	HighTempF        float64 `json:"high_temp_f"`
	LowTempF         float64 `json:"low_temp_f"`
	WindSpeedKts     float64 `json:"wind_speed_kts"`
	WindGustKts      float64 `json:"wind_gust_kts"`
	WindDirection    string  `json:"wind_direction"`
	PrecipitationPct float64 `json:"precipitation_pct"`
}

type weatherForecastDayResponse struct {
	Date             string  `json:"date"`
	DayName          string  `json:"day_name"`
	Condition        string  `json:"condition"`
	HighTempF        float64 `json:"high_temp_f"`
	LowTempF         float64 `json:"low_temp_f"`
	WindSpeedKts     float64 `json:"wind_speed_kts"`
	WindGustKts      float64 `json:"wind_gust_kts"`
	WindDirection    string  `json:"wind_direction"`
	PrecipitationPct float64 `json:"precipitation_pct"`
}

type tideTodayResponse struct {
	Datetime            string  `json:"datetime"`
	CurrentTideHeightFt float64 `json:"current_tide_height_ft"`
	TideDirection       string  `json:"tide_direction"`
	HighTideTime        string  `json:"high_tide_time"`
	HighTideHeightFt    float64 `json:"high_tide_height_ft"`
	LowTideTime         string  `json:"low_tide_time"`
	LowTideHeightFt     float64 `json:"low_tide_height_ft"`
}

type tideTodayETagData struct {
	CurrentTideHeightFt float64   `json:"current_tide_height_ft"`
	TideDirection       string    `json:"tide_direction"`
	HighTideTime        time.Time `json:"high_tide_time"`
	HighTideHeightFt    float64   `json:"high_tide_height_ft"`
	LowTideTime         time.Time `json:"low_tide_time"`
	LowTideHeightFt     float64   `json:"low_tide_height_ft"`
}

func init() {
	loadWeatherTodayCacheFromDisk()
	loadTideCacheFromDisk()
}

func cacheFilePath(envKey, fallback string) string {
	if custom := strings.TrimSpace(os.Getenv(envKey)); custom != "" {
		return custom
	}
	return fallback
}

func writeJSONFileAtomic(path string, value any) error {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, payload, 0o644); err != nil {
		return err
	}

	return os.Rename(tmpPath, path)
}

func weakETagForJSON(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("W/\"%s\"", hex.EncodeToString(sum[:])), nil
}

func requestHasETag(c echo.Context, etag string) bool {
	if etag == "" {
		return false
	}
	ifNoneMatch := c.Request().Header.Get("If-None-Match")
	if ifNoneMatch == "" {
		return false
	}

	for _, candidate := range strings.Split(ifNoneMatch, ",") {
		if strings.TrimSpace(candidate) == etag {
			return true
		}
	}

	return false
}

func respondJSONWithETag(c echo.Context, status int, etag string, payload any) error {
	if etag != "" {
		c.Response().Header().Set("ETag", etag)
		c.Response().Header().Set("Cache-Control", "private, no-cache")
		if requestHasETag(c, etag) {
			return c.NoContent(http.StatusNotModified)
		}
	}

	return c.JSON(status, payload)
}

func loadWeatherTodayCacheFromDisk() {
	path := cacheFilePath("WEATHER_TODAY_CACHE_FILE", defaultWeatherCacheFile)
	payload := map[string]weatherTodayCacheEntryDisk{}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		log.Printf("Failed to read weather cache file %s: %v", path, err)
		return
	}

	if err := json.Unmarshal(raw, &payload); err != nil {
		log.Printf("Failed to parse weather cache file %s: %v", path, err)
		return
	}

	now := time.Now().UTC()
	loaded := make(map[string]weatherTodayCacheEntry, len(payload))
	for key, item := range payload {
		if now.Sub(item.CachedAt) >= weatherTodayCacheTTL {
			continue
		}

		loaded[key] = weatherTodayCacheEntry{state: item.State, cachedAt: item.CachedAt}
	}

	weatherTodayCacheStore.mu.Lock()
	weatherTodayCacheStore.data = loaded
	weatherTodayCacheStore.mu.Unlock()
	log.Printf("Loaded %d weather cache entries from %s", len(loaded), path)
}

func persistWeatherTodayCacheToDisk() {
	path := cacheFilePath("WEATHER_TODAY_CACHE_FILE", defaultWeatherCacheFile)

	weatherTodayCacheStore.mu.RLock()
	payload := make(map[string]weatherTodayCacheEntryDisk, len(weatherTodayCacheStore.data))
	for key, item := range weatherTodayCacheStore.data {
		payload[key] = weatherTodayCacheEntryDisk{State: item.state, CachedAt: item.cachedAt}
	}
	weatherTodayCacheStore.mu.RUnlock()

	if err := writeJSONFileAtomic(path, payload); err != nil {
		log.Printf("Failed to persist weather cache to %s: %v", path, err)
	}
}

func loadTideCacheFromDisk() {
	path := cacheFilePath("TIDE_CACHE_FILE", defaultTideCacheFile)
	payload := map[string]tideDataDisk{}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		log.Printf("Failed to read tide cache file %s: %v", path, err)
		return
	}

	if err := json.Unmarshal(raw, &payload); err != nil {
		log.Printf("Failed to parse tide cache file %s: %v", path, err)
		return
	}

	now := time.Now().UTC()
	loaded := make(map[string]tideData, len(payload))
	for key, item := range payload {
		if now.Sub(item.CachedAt) < 72*time.Hour {
			loaded[key] = tideData{currentHeight: item.CurrentHeight, direction: item.Direction, highTime: item.HighTime, highHeight: item.HighHeight, lowTime: item.LowTime, lowHeight: item.LowHeight, cachedAt: item.CachedAt}
		}
	}

	tideCacheStore.mu.Lock()
	tideCacheStore.data = loaded
	tideCacheStore.mu.Unlock()
	log.Printf("Loaded %d tide cache entries from %s", len(loaded), path)
}

func persistTideCacheToDisk() {
	path := cacheFilePath("TIDE_CACHE_FILE", defaultTideCacheFile)

	tideCacheStore.mu.RLock()
	payload := make(map[string]tideDataDisk, len(tideCacheStore.data))
	for key, item := range tideCacheStore.data {
		payload[key] = tideDataDisk{CurrentHeight: item.currentHeight, Direction: item.direction, HighTime: item.highTime, HighHeight: item.highHeight, LowTime: item.lowTime, LowHeight: item.lowHeight, CachedAt: item.cachedAt}
	}
	tideCacheStore.mu.RUnlock()

	if err := writeJSONFileAtomic(path, payload); err != nil {
		log.Printf("Failed to persist tide cache to %s: %v", path, err)
	}
}

func weatherToday(c echo.Context) error {
	state := weatherTodayData{Datetime: time.Now().UTC(), TemperatureF: 72, Condition: "Partly Cloudy", HighTempF: 76, LowTempF: 64, WindSpeedKts: 12.5, WindGustKts: 18, WindDirection: "NE", PrecipitationPct: 15}

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
		if err == nil && vesselState.Latitude >= -90 && vesselState.Latitude <= 90 && vesselState.Longitude >= -180 && vesselState.Longitude <= 180 {
			roundedLat := math.Round(vesselState.Latitude*10) / 10
			roundedLng := math.Round(vesselState.Longitude*10) / 10
			cacheKey := fmt.Sprintf("%.1f,%.1f", roundedLat, roundedLng)

			weatherTodayCacheStore.mu.RLock()
			cached, ok := weatherTodayCacheStore.data[cacheKey]
			weatherTodayCacheStore.mu.RUnlock()
			if ok && time.Since(cached.cachedAt) < weatherTodayCacheTTL {
				atomic.AddUint64(&weatherTodayCacheHits, 1)
				state = cached.state
				state.Datetime = time.Now().UTC()
				response := weatherTodayResponse{Datetime: state.Datetime.Format(time.RFC3339), TemperatureF: state.TemperatureF, Condition: state.Condition, HighTempF: state.HighTempF, LowTempF: state.LowTempF, WindSpeedKts: state.WindSpeedKts, WindGustKts: state.WindGustKts, WindDirection: state.WindDirection, PrecipitationPct: state.PrecipitationPct}
				etag, err := weakETagForJSON(weatherTodayETagData{TemperatureF: state.TemperatureF, Condition: state.Condition, HighTempF: state.HighTempF, LowTempF: state.LowTempF, WindSpeedKts: state.WindSpeedKts, WindGustKts: state.WindGustKts, WindDirection: state.WindDirection, PrecipitationPct: state.PrecipitationPct})
				if err != nil {
					log.Printf("Failed to build weather ETag: %v", err)
				}
				return respondJSONWithETag(c, http.StatusOK, etag, response)
			}

			atomic.AddUint64(&weatherTodayCacheMisses, 1)

			fetchedWeather := false
			weather, weatherErr := fetchWeatherKitData(vesselState.Latitude, vesselState.Longitude)
			if weatherErr == nil {
				state = weather
				state.Datetime = time.Now().UTC()
				fetchedWeather = true
			} else {
				log.Printf("WeatherKit API error: %v", weatherErr)
			}

			if fetchedWeather {
				weatherTodayCacheStore.mu.Lock()
				weatherTodayCacheStore.data[cacheKey] = weatherTodayCacheEntry{state: state, cachedAt: time.Now().UTC()}
				weatherTodayCacheStore.mu.Unlock()
				persistWeatherTodayCacheToDisk()
			}
		}
	}

	response := weatherTodayResponse{Datetime: state.Datetime.Format(time.RFC3339), TemperatureF: state.TemperatureF, Condition: state.Condition, HighTempF: state.HighTempF, LowTempF: state.LowTempF, WindSpeedKts: state.WindSpeedKts, WindGustKts: state.WindGustKts, WindDirection: state.WindDirection, PrecipitationPct: state.PrecipitationPct}
	etag, err := weakETagForJSON(weatherTodayETagData{TemperatureF: state.TemperatureF, Condition: state.Condition, HighTempF: state.HighTempF, LowTempF: state.LowTempF, WindSpeedKts: state.WindSpeedKts, WindGustKts: state.WindGustKts, WindDirection: state.WindDirection, PrecipitationPct: state.PrecipitationPct})
	if err != nil {
		log.Printf("Failed to build weather ETag: %v", err)
	}
	return respondJSONWithETag(c, http.StatusOK, etag, response)
}

func weatherForecast(c echo.Context) error {
	state := defaultWeatherForecastDays()

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)
	vesselPath := getEnv("SIGNALK_VESSEL_PATH", "/signalk/v1/api/vessels/self")

	if signalkURL != "" {
		vesselState, vesselErr := fetchSignalKVesselState(signalkURL, vesselPath)
		if vesselErr == nil && vesselState.Latitude >= -90 && vesselState.Latitude <= 90 && vesselState.Longitude >= -180 && vesselState.Longitude <= 180 {
			forecast, forecastErr := fetchWeatherKitForecastData(vesselState.Latitude, vesselState.Longitude, 6)
			if forecastErr == nil && len(forecast) > 0 {
				state = forecast
			} else if forecastErr != nil {
				log.Printf("WeatherKit forecast API error: %v", forecastErr)
			}
		}
	}

	response := make([]weatherForecastDayResponse, 0, len(state))
	for _, day := range state {
		response = append(response, weatherForecastDayResponse{
			Date:             day.Date,
			DayName:          day.DayName,
			Condition:        day.Condition,
			HighTempF:        day.HighTempF,
			LowTempF:         day.LowTempF,
			WindSpeedKts:     day.WindSpeedKts,
			WindGustKts:      day.WindGustKts,
			WindDirection:    day.WindDirection,
			PrecipitationPct: day.PrecipitationPct,
		})
	}

	etag, etagErr := weakETagForJSON(response)
	if etagErr != nil {
		log.Printf("Failed to build weather forecast ETag: %v", etagErr)
	}

	return respondJSONWithETag(c, http.StatusOK, etag, response)
}

func tideToday(c echo.Context) error {
	state := tideTodayData{Datetime: time.Now().UTC(), CurrentTideHeightFt: 1.5, TideDirection: "Rising", HighTideTime: time.Now().Add(12*time.Hour + 57*time.Minute), HighTideHeightFt: 1.9, LowTideTime: time.Now().Add(19*time.Hour + 11*time.Minute), LowTideHeightFt: -0.1}

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)
	vesselPath := getEnv("SIGNALK_VESSEL_PATH", "/signalk/v1/api/vessels/self")

	if signalkURL != "" {
		vesselState, vesselErr := fetchSignalKVesselState(signalkURL, vesselPath)
		if vesselErr == nil && vesselState.Latitude >= -90 && vesselState.Latitude <= 90 && vesselState.Longitude >= -180 && vesselState.Longitude <= 180 {
			currentTide, tideDir, highTideTime, highTideHeight, lowTideTime, lowTideHeight, tideErr := fetchStormGlassTideData(vesselState.Latitude, vesselState.Longitude)
			if tideErr == nil {
				state.CurrentTideHeightFt = currentTide
				state.TideDirection = tideDir
				state.HighTideTime = highTideTime
				state.HighTideHeightFt = highTideHeight
				state.LowTideTime = lowTideTime
				state.LowTideHeightFt = lowTideHeight
			} else {
				log.Printf("Storm Glass API error: %v", tideErr)
			}
		}
	}

	state.Datetime = time.Now().UTC()
	response := tideTodayResponse{Datetime: state.Datetime.Format(time.RFC3339), CurrentTideHeightFt: state.CurrentTideHeightFt, TideDirection: state.TideDirection, HighTideTime: state.HighTideTime.Format(time.RFC3339), HighTideHeightFt: state.HighTideHeightFt, LowTideTime: state.LowTideTime.Format(time.RFC3339), LowTideHeightFt: state.LowTideHeightFt}
	etag, err := weakETagForJSON(tideTodayETagData{CurrentTideHeightFt: state.CurrentTideHeightFt, TideDirection: state.TideDirection, HighTideTime: state.HighTideTime, HighTideHeightFt: state.HighTideHeightFt, LowTideTime: state.LowTideTime, LowTideHeightFt: state.LowTideHeightFt})
	if err != nil {
		log.Printf("Failed to build tide ETag: %v", err)
	}
	return respondJSONWithETag(c, http.StatusOK, etag, response)
}

func fetchWeatherKitData(latitude, longitude float64) (weatherTodayData, error) {
	data := weatherTodayData{TemperatureF: -1, Condition: "Unknown", HighTempF: -1, LowTempF: -1, WindSpeedKts: -1, WindGustKts: -1, WindDirection: "—", PrecipitationPct: -1}

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

	requestURL := fmt.Sprintf("https://weatherkit.apple.com/api/v1/weather/en/%.4f/%.4f?dataSets=currentWeather,forecastDaily&timezone=America/Los_Angeles", latitude, longitude)
	req, err := http.NewRequest("GET", requestURL, nil)
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

	log.Printf("WeatherKit API status: %d, content-type: %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return data, fmt.Errorf("failed to read response body: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		snippet := string(bodyBytes)
		if len(snippet) > 500 {
			snippet = snippet[:500]
		}
		return data, fmt.Errorf("failed to parse response: %v; body: %s", err, snippet)
	}

	if current, ok := result["currentWeather"].(map[string]any); ok {
		if temp, ok := current["temperature"].(float64); ok {
			data.TemperatureF = (temp * 9 / 5) + 32
		}
		if condition, ok := current["conditionCode"].(string); ok {
			data.Condition = formatWeatherCondition(condition)
		} else if condition, ok := current["condition"].(string); ok {
			data.Condition = formatWeatherCondition(condition)
		}
		if windSpeed, ok := current["windSpeed"].(float64); ok {
			data.WindSpeedKts = windSpeed * kphToKnots
		}
		if windGust, ok := current["windGust"].(float64); ok {
			data.WindGustKts = windGust * kphToKnots
		}
		if windDir, ok := current["windDirection"].(float64); ok {
			data.WindDirection = degreesToDirection(windDir)
		}
		if precip, ok := current["precipitationChance"].(float64); ok {
			data.PrecipitationPct = precip * 100
		} else if precip, ok := current["precipitationIntensity"].(float64); ok {
			data.PrecipitationPct = math.Max(0, precip)
		}
	}

	if daily, ok := result["forecastDaily"].(map[string]any); ok {
		if days, ok := daily["days"].([]any); ok && len(days) > 0 {
			if day, ok := days[0].(map[string]any); ok {
				if high, ok := day["temperatureMax"].(float64); ok {
					data.HighTempF = (high * 9 / 5) + 32
				}
				if low, ok := day["temperatureMin"].(float64); ok {
					data.LowTempF = (low * 9 / 5) + 32
				}
				if data.PrecipitationPct < 0 {
					if precip, ok := day["precipitationChance"].(float64); ok {
						data.PrecipitationPct = precip * 100
					}
				}
			}
		}
	}

	if data.PrecipitationPct < 0 {
		if hourly, ok := result["forecastHourly"].(map[string]any); ok {
			if hours, ok := hourly["hours"].([]any); ok && len(hours) > 0 {
				if hour0, ok := hours[0].(map[string]any); ok {
					if precip, ok := hour0["precipitationChance"].(float64); ok {
						data.PrecipitationPct = precip * 100
					}
				}
			}
		}
	}

	return data, nil
}

func fetchWeatherKitForecastData(latitude, longitude float64, daysCount int) ([]weatherForecastDayData, error) {
	if daysCount <= 0 {
		daysCount = 6
	}

	keyID := getEnv("WEATHERKIT_KEY_ID", "")
	teamID := getEnv("WEATHERKIT_TEAM_ID", "")
	serviceID := getEnv("WEATHERKIT_SERVICE_ID", "")
	privateKeyPEM := getEnv("WEATHERKIT_PRIVATE_KEY", "")

	if keyID == "" || teamID == "" || serviceID == "" || privateKeyPEM == "" {
		return nil, fmt.Errorf("WeatherKit credentials not configured")
	}

	token, err := generateWeatherKitJWT(keyID, teamID, serviceID, privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to generate JWT: %v", err)
	}

	requestURL := fmt.Sprintf("https://weatherkit.apple.com/api/v1/weather/en/%.4f/%.4f?dataSets=forecastDaily&timezone=America/Los_Angeles", latitude, longitude)
	req, err := http.NewRequest("GET", requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch weather forecast: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("WeatherKit forecast API returned %d: %s", resp.StatusCode, string(body))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read forecast response body: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		snippet := string(bodyBytes)
		if len(snippet) > 500 {
			snippet = snippet[:500]
		}
		return nil, fmt.Errorf("failed to parse forecast response: %v; body: %s", err, snippet)
	}

	forecastDaily, ok := result["forecastDaily"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("forecastDaily missing from WeatherKit response")
	}

	rawDays, ok := forecastDaily["days"].([]any)
	if !ok || len(rawDays) == 0 {
		return nil, fmt.Errorf("forecastDaily.days missing from WeatherKit response")
	}

	forecast := make([]weatherForecastDayData, 0, daysCount)
	for _, rawDay := range rawDays {
		if len(forecast) >= daysCount {
			break
		}

		dayMap, ok := rawDay.(map[string]any)
		if !ok {
			continue
		}

		dateLabel := ""
		dayName := ""
		if forecastStart, ok := dayMap["forecastStart"].(string); ok {
			if parsed, parseErr := time.Parse(time.RFC3339, forecastStart); parseErr == nil {
				dateLabel = parsed.Format("Jan 2")
				dayName = parsed.Weekday().String()
			}
		}

		highTempF := -1.0
		if highC, ok := dayMap["temperatureMax"].(float64); ok {
			highTempF = (highC * 9 / 5) + 32
		}

		lowTempF := -1.0
		if lowC, ok := dayMap["temperatureMin"].(float64); ok {
			lowTempF = (lowC * 9 / 5) + 32
		}

		condition := "Unknown"
		if code, ok := dayMap["conditionCode"].(string); ok && strings.TrimSpace(code) != "" {
			condition = formatWeatherCondition(code)
		}

		precipPct := 0.0
		if precipChance, ok := dayMap["precipitationChance"].(float64); ok {
			precipPct = precipChance * 100
		}

		windSpeedKts := -1.0
		windGustKts := -1.0
		windDirection := "—"

		if daytimeForecast, ok := dayMap["daytimeForecast"].(map[string]any); ok {
			if speed, ok := daytimeForecast["windSpeed"].(float64); ok {
				windSpeedKts = speed * kphToKnots
			}
			if gust, ok := daytimeForecast["windGust"].(float64); ok {
				windGustKts = gust * kphToKnots
			}
			if directionDeg, ok := daytimeForecast["windDirection"].(float64); ok {
				windDirection = degreesToDirection(directionDeg)
			}
		}

		if windSpeedKts < 0 {
			if speed, ok := dayMap["windSpeed"].(float64); ok {
				windSpeedKts = speed * kphToKnots
			}
		}

		if windGustKts < 0 {
			if gust, ok := dayMap["windGust"].(float64); ok {
				windGustKts = gust * kphToKnots
			}
		}

		if windDirection == "—" {
			if directionDeg, ok := dayMap["windDirection"].(float64); ok {
				windDirection = degreesToDirection(directionDeg)
			}
		}

		if windGustKts < 0 && windSpeedKts >= 0 {
			windGustKts = windSpeedKts
		}

		forecast = append(forecast, weatherForecastDayData{
			Date:             dateLabel,
			DayName:          dayName,
			Condition:        condition,
			HighTempF:        highTempF,
			LowTempF:         lowTempF,
			WindSpeedKts:     windSpeedKts,
			WindGustKts:      windGustKts,
			WindDirection:    windDirection,
			PrecipitationPct: precipPct,
		})
	}

	if len(forecast) == 0 {
		return nil, fmt.Errorf("no forecast days parsed from WeatherKit response")
	}

	return forecast, nil
}

func defaultWeatherForecastDays() []weatherForecastDayData {
	days := []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday"}
	conditions := []string{"Mostly Clear", "Clear", "Overcast", "Partly Cloudy", "Cloudy", "Drizzle"}
	directions := []string{"NE", "E", "SE", "S", "SW", "W"}
	today := time.Now()
	forecast := make([]weatherForecastDayData, 0, len(days))

	for idx, dayName := range days {
		date := today.AddDate(0, 0, idx)
		steady := 10 + float64(idx*2)
		gust := steady + 6
		forecast = append(forecast, weatherForecastDayData{
			Date:             date.Format("Jan 2"),
			DayName:          dayName,
			Condition:        conditions[idx%len(conditions)],
			HighTempF:        72 + float64(idx%4),
			LowTempF:         62 + float64(idx%3),
			WindSpeedKts:     steady,
			WindGustKts:      gust,
			WindDirection:    directions[idx%len(directions)],
			PrecipitationPct: float64((idx % 4) * 15),
		})
	}

	return forecast
}

func generateWeatherKitJWT(keyID, teamID, serviceID, privateKeyPEM string) (string, error) {
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

	now := time.Now()
	exp := now.Add(time.Hour)
	claims := jwt.MapClaims{"iss": teamID, "sub": serviceID, "aud": "https://weatherkit.apple.com", "iat": now.Unix(), "exp": exp.Unix()}
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = keyID
	tokenString, err := token.SignedString(ecdsaKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %v", err)
	}

	return tokenString, nil
}

func fetchStormGlassTideData(latitude, longitude float64) (currentHeight float64, direction string, highTime time.Time, highHeight float64, lowTime time.Time, lowHeight float64, err error) {
	currentHeight = 0
	direction = "—"
	highTime = time.Now()
	highHeight = 0
	lowTime = time.Now().Add(24 * time.Hour)
	lowHeight = 0

	now := time.Now().UTC()
	dateKey := now.Format("2006-01-02")
	roundedLat := math.Round(latitude*10) / 10
	roundedLng := math.Round(longitude*10) / 10
	cacheKey := fmt.Sprintf("%.1f,%.1f,%s", roundedLat, roundedLng, dateKey)
	tideCacheStore.mu.RLock()
	if cached, ok := tideCacheStore.data[cacheKey]; ok {
		tideCacheStore.mu.RUnlock()
		atomic.AddUint64(&tideCacheHits, 1)
		log.Printf("Using cached tide data for %s", cacheKey)
		return cached.currentHeight, cached.direction, cached.highTime, cached.highHeight, cached.lowTime, cached.lowHeight, nil
	}
	tideCacheStore.mu.RUnlock()
	atomic.AddUint64(&tideCacheMisses, 1)

	apiKey := getEnv("STORMGLASS_API_KEY", "")
	if apiKey == "" {
		return currentHeight, direction, highTime, highHeight, lowTime, lowHeight, fmt.Errorf("Storm Glass API key not configured")
	}

	startTime := now.Truncate(24 * time.Hour)
	extremesURL := fmt.Sprintf("https://api.stormglass.io/v2/tide/extremes/point?lat=%.1f&lng=%.1f&start=%d&end=%d", roundedLat, roundedLng, startTime.Unix(), startTime.Add(48*time.Hour).Unix())
	req, err := http.NewRequest("GET", extremesURL, nil)
	if err != nil {
		return currentHeight, direction, highTime, highHeight, lowTime, lowHeight, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Authorization", apiKey)
	log.Printf("Fetching Storm Glass tide data for %s", cacheKey)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return currentHeight, direction, highTime, highHeight, lowTime, lowHeight, fmt.Errorf("failed to fetch tide data: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return currentHeight, direction, highTime, highHeight, lowTime, lowHeight, fmt.Errorf("Storm Glass API returned %d: %s", resp.StatusCode, string(body))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return currentHeight, direction, highTime, highHeight, lowTime, lowHeight, fmt.Errorf("failed to read response body: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return currentHeight, direction, highTime, highHeight, lowTime, lowHeight, fmt.Errorf("failed to parse response: %v", err)
	}

	if data, ok := result["data"].([]any); ok {
		for _, item := range data {
			if extreme, ok := item.(map[string]any); ok {
				typeVal, _ := extreme["type"].(string)
				timeStr, _ := extreme["time"].(string)
				height, _ := extreme["height"].(float64)
				if t, parseErr := time.Parse(time.RFC3339, timeStr); parseErr == nil {
					heightFt := height * 3.28084
					if typeVal == "high" && t.After(now) && highHeight == 0 {
						highTime = t
						highHeight = heightFt
					} else if typeVal == "low" && t.After(now) && lowHeight == 0 {
						lowTime = t
						lowHeight = heightFt
					}
					if t.Sub(now).Abs() < time.Minute && currentHeight == 0 {
						currentHeight = heightFt
					}
				}
			}
		}
	}

	seaLevelURL := fmt.Sprintf("https://api.stormglass.io/v2/tide/sea-level/point?lat=%.2f&lng=%.2f&start=%d", roundedLat, roundedLng, now.Unix())
	req2, err := http.NewRequest("GET", seaLevelURL, nil)
	if err == nil {
		req2.Header.Set("Authorization", apiKey)
		resp2, err := client.Do(req2)
		if err == nil && resp2.StatusCode == http.StatusOK {
			defer resp2.Body.Close()
			bodyBytes2, _ := io.ReadAll(resp2.Body)
			var seaResult map[string]any
			if json.Unmarshal(bodyBytes2, &seaResult) == nil {
				if data, ok := seaResult["data"].([]any); ok && len(data) > 0 {
					if current, ok := data[0].(map[string]any); ok {
						if height, ok := current["height"].(float64); ok {
							currentHeight = height * 3.28084
						}
					}
				}
			}
		}
	}

	if currentHeight > 0 {
		direction = "Rising"
	} else {
		direction = "Falling"
	}

	tideCacheStore.mu.Lock()
	tideCacheStore.data[cacheKey] = tideData{currentHeight: currentHeight, direction: direction, highTime: highTime, highHeight: highHeight, lowTime: lowTime, lowHeight: lowHeight, cachedAt: now}
	tideCacheStore.mu.Unlock()
	persistTideCacheToDisk()
	log.Printf("Cached tide data for %s", cacheKey)

	return currentHeight, direction, highTime, highHeight, lowTime, lowHeight, nil
}
