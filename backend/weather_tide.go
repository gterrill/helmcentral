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
	weatherTodayCacheTTL     = 10 * time.Minute
	weatherForecastCacheTTL  = 60 * time.Minute
	defaultWeatherCacheFile  = "cache/weather_today_cache.json"
	defaultForecastCacheFile = "cache/weather_forecast_cache.json"
	defaultTideCacheFile     = "cache/tide_cache.json"
	kphToKnots               = 0.539957
	metersToFeet             = 3.28084
)

type weatherTodayCache struct {
	mu   sync.RWMutex
	data map[string]weatherTodayCacheEntry
}

type weatherForecastCache struct {
	mu   sync.RWMutex
	data map[string]weatherForecastCacheEntry
}

type weatherTodayCacheEntry struct {
	state    weatherTodayData
	cachedAt time.Time
}

type weatherForecastCacheEntry struct {
	state    weatherForecastDataBundle
	cachedAt time.Time
}

type weatherTodayCacheEntryDisk struct {
	State    weatherTodayData `json:"state"`
	CachedAt time.Time        `json:"cached_at"`
}

type weatherForecastCacheEntryDisk struct {
	State    weatherForecastDataBundle `json:"state"`
	CachedAt time.Time                 `json:"cached_at"`
}

var weatherTodayCacheStore = &weatherTodayCache{data: make(map[string]weatherTodayCacheEntry)}
var weatherForecastCacheStore = &weatherForecastCache{data: make(map[string]weatherForecastCacheEntry)}

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
var weatherForecastCacheHits uint64
var weatherForecastCacheMisses uint64

var jsonCacheDescriptors = []jsonCacheDescriptor{
	{Name: "weather_today", EnvKey: "WEATHER_TODAY_CACHE_FILE", Fallback: defaultWeatherCacheFile, TTL: weatherTodayCacheTTL},
	{Name: "weather_forecast", EnvKey: "WEATHER_FORECAST_CACHE_FILE", Fallback: defaultForecastCacheFile, TTL: weatherForecastCacheTTL},
	{Name: "tide", EnvKey: "TIDE_CACHE_FILE", Fallback: defaultTideCacheFile, TTL: stormGlassTideCacheTTL},
	{Name: "tide_bom", EnvKey: "TIDE_BOM_CACHE_FILE", Fallback: defaultBomTideCacheFile, TTL: bomTideCacheTTL},
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
		} else if !errors.Is(err, os.ErrNotExist) {
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
		case "weather_forecast":
			weatherForecastCacheStore.mu.RLock()
			inMemoryEntries = len(weatherForecastCacheStore.data)
			weatherForecastCacheStore.mu.RUnlock()
			cacheHits = atomic.LoadUint64(&weatherForecastCacheHits)
			cacheMisses = atomic.LoadUint64(&weatherForecastCacheMisses)
		case "tide":
			tideCacheStore.mu.RLock()
			inMemoryEntries = len(tideCacheStore.data)
			tideCacheStore.mu.RUnlock()
			cacheHits = atomic.LoadUint64(&tideCacheHits)
			cacheMisses = atomic.LoadUint64(&tideCacheMisses)
		case "tide_bom":
			bomTideCacheStore.mu.RLock()
			inMemoryEntries = len(bomTideCacheStore.data)
			bomTideCacheStore.mu.RUnlock()
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
	case "weather_forecast":
		weatherForecastCacheStore.mu.Lock()
		weatherForecastCacheStore.data = make(map[string]weatherForecastCacheEntry)
		weatherForecastCacheStore.mu.Unlock()
	case "tide":
		tideCacheStore.mu.Lock()
		tideCacheStore.data = make(map[string]tideChartResult)
		tideCacheStore.mu.Unlock()
	case "tide_bom":
		bomTideCacheStore.mu.Lock()
		bomTideCacheStore.data = make(map[string]tideChartResult)
		bomTideCacheStore.mu.Unlock()
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
	SeaTemperatureF  float64
}

type weatherForecastDayData struct {
	Date                 string
	DayName              string
	Condition            string
	HighTempF            float64
	LowTempF             float64
	WindSpeedKts         float64
	WindGustKts          float64
	WindDirection        string
	WindSummary          string
	WaveSummary          string
	PrecipitationPct     float64
	PrecipitationSummary string
	SunriseTime          string
	SunsetTime           string
	MoonPhase            string
	HourlyWind           []weatherHourlyWindData
	HourlyWave           []weatherHourlyWaveData
	HourlyPrecip         []weatherHourlyPrecipitationData
	HourlyUV             []weatherHourlyUVData
	HourlyCloud          []weatherHourlyCloudData
}

type weatherHourlyEntryData struct {
	Label            string
	Condition        string
	TemperatureF     float64
	WindSpeedKts     float64
	WindGustKts      float64
	WindDirection    string
	WindDirectionDeg float64
	Kind             string
}

type weatherHourlyWindData struct {
	Label            string
	HourOfDay        int
	WindSpeedKts     float64
	WindGustKts      float64
	WindDirection    string
	WindDirectionDeg float64
}

type weatherHourlyWaveData struct {
	Label            string
	HourOfDay        int
	WaveHeightM      float64
	WavePeriodS      float64
	WaveDirectionDeg float64
	WindWaveHeightM  float64
	SwellWaveHeightM float64
}

type weatherHourlyPrecipitationData struct {
	Label                    string
	HourOfDay                int
	PrecipitationChancePct   float64
	PrecipitationIntensityMm float64
}

type weatherHourlyUVData struct {
	Label   string
	UVIndex float64
}

type weatherHourlyCloudData struct {
	Label        string
	HourOfDay    int
	Condition    string
	TemperatureF float64
	IsDaylight   bool
}

type weatherForecastDataBundle struct {
	Days        []weatherForecastDayData
	HourlyToday []weatherHourlyEntryData
	Summary     string
}

type tideTodayData struct {
	Datetime            time.Time
	CurrentTideHeightFt float64
	TideDirection       string
	HighTideTime        time.Time
	HighTideHeightFt    float64
	LowTideTime         time.Time
	LowTideHeightFt     float64
	TidalPhase          string
	DoubleHighToday     bool
	DoubleLowToday      bool
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
	SeaTemperatureF  float64 `json:"sea_temperature_f"`
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
	SeaTemperatureF  float64 `json:"sea_temperature_f"`
}

type weatherForecastDayResponse struct {
	Date                 string                               `json:"date"`
	DayName              string                               `json:"day_name"`
	Condition            string                               `json:"condition"`
	HighTempF            float64                              `json:"high_temp_f"`
	LowTempF             float64                              `json:"low_temp_f"`
	WindSpeedKts         float64                              `json:"wind_speed_kts"`
	WindGustKts          float64                              `json:"wind_gust_kts"`
	WindDirection        string                               `json:"wind_direction"`
	WindSummary          string                               `json:"wind_summary"`
	WaveSummary          string                               `json:"wave_summary"`
	PrecipitationPct     float64                              `json:"precipitation_pct"`
	PrecipitationSummary string                               `json:"precipitation_summary"`
	SunriseTime          string                               `json:"sunrise_time"`
	SunsetTime           string                               `json:"sunset_time"`
	MoonPhase            string                               `json:"moon_phase"`
	HourlyWind           []weatherHourlyWindResponse          `json:"hourly_wind"`
	HourlyWave           []weatherHourlyWaveResponse          `json:"hourly_wave"`
	HourlyPrecip         []weatherHourlyPrecipitationResponse `json:"hourly_precip"`
	HourlyUV             []weatherHourlyUVResponse            `json:"hourly_uv"`
	HourlyCloud          []weatherHourlyCloudResponse         `json:"hourly_cloud"`
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

type weatherHourlyWaveResponse struct {
	Label            string  `json:"label"`
	HourOfDay        int     `json:"hour_of_day"`
	WaveHeightM      float64 `json:"wave_height_m"`
	WavePeriodS      float64 `json:"wave_period_s"`
	WaveDirectionDeg float64 `json:"wave_direction_deg"`
	WindWaveHeightM  float64 `json:"wind_wave_height_m"`
	SwellWaveHeightM float64 `json:"swell_wave_height_m"`
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

type weatherForecastResponse struct {
	Days        []weatherForecastDayResponse `json:"days"`
	HourlyToday []weatherHourlyEntryResponse `json:"hourly_today"`
	Summary     string                       `json:"summary"`
	Cached      bool                         `json:"cached"`
	UpdatedAt   string                       `json:"updated_at"`
	TTLSeconds  int64                        `json:"ttl_seconds"`
}

type tideTodayResponse struct {
	Datetime            string  `json:"datetime"`
	CurrentTideHeightFt float64 `json:"current_tide_height_ft"`
	TideDirection       string  `json:"tide_direction"`
	HighTideTime        string  `json:"high_tide_time"`
	HighTideHeightFt    float64 `json:"high_tide_height_ft"`
	LowTideTime         string  `json:"low_tide_time"`
	LowTideHeightFt     float64 `json:"low_tide_height_ft"`
	StationName         string  `json:"station_name,omitempty"`
	Provider            string  `json:"provider,omitempty"`
	TidalPhase          string  `json:"tidal_phase,omitempty"`
	DoubleHighToday     bool    `json:"double_high_today,omitempty"`
	DoubleLowToday      bool    `json:"double_low_today,omitempty"`
}

type tideTodayETagData struct {
	CurrentTideHeightFt float64   `json:"current_tide_height_ft"`
	TideDirection       string    `json:"tide_direction"`
	HighTideTime        time.Time `json:"high_tide_time"`
	HighTideHeightFt    float64   `json:"high_tide_height_ft"`
	LowTideTime         time.Time `json:"low_tide_time"`
	LowTideHeightFt     float64   `json:"low_tide_height_ft"`
	StationName         string    `json:"station_name,omitempty"`
	Provider            string    `json:"provider,omitempty"`
	TidalPhase          string    `json:"tidal_phase,omitempty"`
	DoubleHighToday     bool      `json:"double_high_today,omitempty"`
	DoubleLowToday      bool      `json:"double_low_today,omitempty"`
}

func init() {
	loadWeatherTodayCacheFromDisk()
	loadWeatherForecastCacheFromDisk()
	loadTideCacheFromDisk()
	loadBomTideCacheFromDisk()
	loadBomMarineWarningsCacheFromDisk()
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

func loadWeatherForecastCacheFromDisk() {
	path := cacheFilePath("WEATHER_FORECAST_CACHE_FILE", defaultForecastCacheFile)
	payload := map[string]weatherForecastCacheEntryDisk{}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		log.Printf("Failed to read weather forecast cache file %s: %v", path, err)
		return
	}

	if err := json.Unmarshal(raw, &payload); err != nil {
		log.Printf("Failed to parse weather forecast cache file %s: %v", path, err)
		return
	}

	now := time.Now().UTC()
	loaded := make(map[string]weatherForecastCacheEntry, len(payload))
	for key, item := range payload {
		if now.Sub(item.CachedAt) >= weatherForecastCacheTTL {
			continue
		}

		stateCopy := cloneWeatherForecastBundle(item.State)
		loaded[key] = weatherForecastCacheEntry{state: stateCopy, cachedAt: item.CachedAt}
	}

	weatherForecastCacheStore.mu.Lock()
	weatherForecastCacheStore.data = loaded
	weatherForecastCacheStore.mu.Unlock()
	log.Printf("Loaded %d weather forecast cache entries from %s", len(loaded), path)
}

func persistWeatherForecastCacheToDisk() {
	path := cacheFilePath("WEATHER_FORECAST_CACHE_FILE", defaultForecastCacheFile)

	weatherForecastCacheStore.mu.RLock()
	payload := make(map[string]weatherForecastCacheEntryDisk, len(weatherForecastCacheStore.data))
	for key, item := range weatherForecastCacheStore.data {
		stateCopy := cloneWeatherForecastBundle(item.state)
		payload[key] = weatherForecastCacheEntryDisk{State: stateCopy, CachedAt: item.cachedAt}
	}
	weatherForecastCacheStore.mu.RUnlock()

	if err := writeJSONFileAtomic(path, payload); err != nil {
		log.Printf("Failed to persist weather forecast cache to %s: %v", path, err)
	}
}

func cloneWeatherForecastBundle(bundle weatherForecastDataBundle) weatherForecastDataBundle {
	days := make([]weatherForecastDayData, len(bundle.Days))
	for i, day := range bundle.Days {
		day.HourlyWind = append([]weatherHourlyWindData(nil), day.HourlyWind...)
		day.HourlyWave = append([]weatherHourlyWaveData(nil), day.HourlyWave...)
		days[i] = day
	}

	return weatherForecastDataBundle{
		Days:        days,
		HourlyToday: append([]weatherHourlyEntryData(nil), bundle.HourlyToday...),
		Summary:     bundle.Summary,
	}
}

func weatherToday(c echo.Context) error {
	state := weatherTodayData{Datetime: time.Now().UTC(), TemperatureF: 72, Condition: "Partly Cloudy", HighTempF: 76, LowTempF: 64, WindSpeedKts: 12.5, WindGustKts: 18, WindDirection: "NE", PrecipitationPct: 15, SeaTemperatureF: -1}

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
				response := weatherTodayResponse{Datetime: state.Datetime.Format(time.RFC3339), TemperatureF: state.TemperatureF, Condition: state.Condition, HighTempF: state.HighTempF, LowTempF: state.LowTempF, WindSpeedKts: state.WindSpeedKts, WindGustKts: state.WindGustKts, WindDirection: state.WindDirection, PrecipitationPct: state.PrecipitationPct, SeaTemperatureF: state.SeaTemperatureF}
				etag, err := weakETagForJSON(weatherTodayETagData{TemperatureF: state.TemperatureF, Condition: state.Condition, HighTempF: state.HighTempF, LowTempF: state.LowTempF, WindSpeedKts: state.WindSpeedKts, WindGustKts: state.WindGustKts, WindDirection: state.WindDirection, PrecipitationPct: state.PrecipitationPct, SeaTemperatureF: state.SeaTemperatureF})
				if err != nil {
					log.Printf("Failed to build weather ETag: %v", err)
				}
				return respondJSONWithETag(c, http.StatusOK, etag, response)
			}

			atomic.AddUint64(&weatherTodayCacheMisses, 1)

			fetchedWeather := false
			weather, weatherErr := fetchWeatherKitData(vesselState.Latitude, vesselState.Longitude, vesselState.Datetime, vesselLocalLocation(vesselState.Longitude))
			if weatherErr == nil {
				state = weather
				state.Datetime = time.Now().UTC()
				fetchedWeather = true

				seaTemp, seaTempErr := fetchOpenMeteoSeaTemperatureF(vesselState.Latitude, vesselState.Longitude)
				if seaTempErr == nil {
					state.SeaTemperatureF = seaTemp
				} else {
					log.Printf("Open-Meteo sea temperature error: %v", seaTempErr)
					state.SeaTemperatureF = -1
				}
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

	response := weatherTodayResponse{Datetime: state.Datetime.Format(time.RFC3339), TemperatureF: state.TemperatureF, Condition: state.Condition, HighTempF: state.HighTempF, LowTempF: state.LowTempF, WindSpeedKts: state.WindSpeedKts, WindGustKts: state.WindGustKts, WindDirection: state.WindDirection, PrecipitationPct: state.PrecipitationPct, SeaTemperatureF: state.SeaTemperatureF}
	etag, err := weakETagForJSON(weatherTodayETagData{TemperatureF: state.TemperatureF, Condition: state.Condition, HighTempF: state.HighTempF, LowTempF: state.LowTempF, WindSpeedKts: state.WindSpeedKts, WindGustKts: state.WindGustKts, WindDirection: state.WindDirection, PrecipitationPct: state.PrecipitationPct, SeaTemperatureF: state.SeaTemperatureF})
	if err != nil {
		log.Printf("Failed to build weather ETag: %v", err)
	}
	return respondJSONWithETag(c, http.StatusOK, etag, response)
}

func weatherForecast(c echo.Context) error {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
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

	if vesselState.Latitude < -90 || vesselState.Latitude > 90 || vesselState.Longitude < -180 || vesselState.Longitude > 180 {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "invalid vessel coordinates from SignalK"})
	}

	roundedLat := math.Round(vesselState.Latitude*10) / 10
	roundedLng := math.Round(vesselState.Longitude*10) / 10
	vesselLocalLocation := vesselLocalLocation(vesselState.Longitude)
	localTodayKey := vesselState.Datetime.In(vesselLocalLocation).Format("2006-01-02")
	cacheKey := fmt.Sprintf("%.1f,%.1f,%s", roundedLat, roundedLng, localTodayKey)

	weatherForecastCacheStore.mu.RLock()
	cached, ok := weatherForecastCacheStore.data[cacheKey]
	weatherForecastCacheStore.mu.RUnlock()
	if ok && time.Since(cached.cachedAt) < weatherForecastCacheTTL && len(cached.state.Days) > 0 {
		atomic.AddUint64(&weatherForecastCacheHits, 1)
		response := buildWeatherForecastResponse(cached.state, true, cached.cachedAt)
		etag, etagErr := weakETagForJSON(response)
		if etagErr != nil {
			log.Printf("Failed to build weather forecast ETag: %v", etagErr)
		}
		return respondJSONWithETag(c, http.StatusOK, etag, response)
	}

	atomic.AddUint64(&weatherForecastCacheMisses, 1)

	bundle, forecastErr := fetchWeatherKitForecastBundleData(vesselState.Latitude, vesselState.Longitude, 10, vesselState.Datetime, vesselLocalLocation)
	if forecastErr != nil {
		log.Printf("WeatherKit forecast API error: %v", forecastErr)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "failed to fetch weather forecast"})
	}
	if len(bundle.Days) == 0 {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "weather forecast response was empty"})
	}

	weatherForecastCacheStore.mu.Lock()
	cachedAt := time.Now().UTC()
	weatherForecastCacheStore.data[cacheKey] = weatherForecastCacheEntry{state: cloneWeatherForecastBundle(bundle), cachedAt: cachedAt}
	weatherForecastCacheStore.mu.Unlock()
	persistWeatherForecastCacheToDisk()

	response := buildWeatherForecastResponse(bundle, false, cachedAt)

	etag, etagErr := weakETagForJSON(response)
	if etagErr != nil {
		log.Printf("Failed to build weather forecast ETag: %v", etagErr)
	}

	return respondJSONWithETag(c, http.StatusOK, etag, response)
}

func buildWeatherForecastResponse(bundle weatherForecastDataBundle, cached bool, updatedAt time.Time) weatherForecastResponse {
	return weatherForecastResponse{
		Days:        mapForecastResponse(bundle.Days),
		HourlyToday: mapWeatherHourlyResponse(bundle.HourlyToday),
		Summary:     bundle.Summary,
		Cached:      cached,
		UpdatedAt:   updatedAt.UTC().Format(time.RFC3339),
		TTLSeconds:  int64(weatherForecastCacheTTL / time.Second),
	}
}

func mapForecastResponse(state []weatherForecastDayData) []weatherForecastDayResponse {
	response := make([]weatherForecastDayResponse, 0, len(state))
	for _, day := range state {
		response = append(response, weatherForecastDayResponse{
			Date:                 day.Date,
			DayName:              day.DayName,
			Condition:            day.Condition,
			HighTempF:            day.HighTempF,
			LowTempF:             day.LowTempF,
			WindSpeedKts:         day.WindSpeedKts,
			WindGustKts:          day.WindGustKts,
			WindDirection:        day.WindDirection,
			WindSummary:          day.WindSummary,
			WaveSummary:          day.WaveSummary,
			PrecipitationPct:     day.PrecipitationPct,
			PrecipitationSummary: day.PrecipitationSummary,
			SunriseTime:          day.SunriseTime,
			SunsetTime:           day.SunsetTime,
			MoonPhase:            day.MoonPhase,
			HourlyWind:           mapHourlyWindResponse(day.HourlyWind),
			HourlyWave:           mapHourlyWaveResponse(day.HourlyWave),
			HourlyPrecip:         mapHourlyPrecipitationResponse(day.HourlyPrecip),
			HourlyUV:             mapHourlyUVResponse(day.HourlyUV),
			HourlyCloud:          mapHourlyCloudResponse(day.HourlyCloud),
		})
	}
	return response
}

func mapHourlyWindResponse(entries []weatherHourlyWindData) []weatherHourlyWindResponse {
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

func mapHourlyWaveResponse(entries []weatherHourlyWaveData) []weatherHourlyWaveResponse {
	response := make([]weatherHourlyWaveResponse, 0, len(entries))
	for _, entry := range entries {
		response = append(response, weatherHourlyWaveResponse{
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

func mapHourlyPrecipitationResponse(entries []weatherHourlyPrecipitationData) []weatherHourlyPrecipitationResponse {
	response := make([]weatherHourlyPrecipitationResponse, 0, len(entries))
	for _, entry := range entries {
		response = append(response, weatherHourlyPrecipitationResponse{
			Label:                    entry.Label,
			HourOfDay:                entry.HourOfDay,
			PrecipitationChancePct:   entry.PrecipitationChancePct,
			PrecipitationIntensityMm: entry.PrecipitationIntensityMm,
		})
	}
	return response
}

func mapHourlyUVResponse(entries []weatherHourlyUVData) []weatherHourlyUVResponse {
	response := make([]weatherHourlyUVResponse, 0, len(entries))
	for _, entry := range entries {
		response = append(response, weatherHourlyUVResponse{
			Label:   entry.Label,
			UVIndex: entry.UVIndex,
		})
	}
	return response
}

func mapHourlyCloudResponse(entries []weatherHourlyCloudData) []weatherHourlyCloudResponse {
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

func mapWeatherHourlyResponse(entries []weatherHourlyEntryData) []weatherHourlyEntryResponse {
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

func tideToday(c echo.Context) error {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	settings, err := readSettings(settingsPath)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to read settings: %v", err)})
	}

	uiMap, _ := settings["ui"].(map[string]any)

	configuredProvider := strings.TrimSpace(coerceString(uiMap["tide_provider"]))
	if configuredProvider == "" {
		configuredProvider = "stormglass"
	}
	configuredStation := strings.TrimSpace(coerceString(uiMap["tide_station_id"]))
	if configuredStation == "" {
		configuredStation = stormGlassVesselStationID
	}

	provider, ok := getTideProvider(configuredProvider)
	if !ok {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("unknown tide provider configured: %q", configuredProvider)})
	}

	result, fetchErr := provider.FetchTideChart(configuredStation)
	if fetchErr != nil {
		log.Printf("Tide provider %q error: %v", configuredProvider, fetchErr)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("tide provider %q unavailable: %v", configuredProvider, fetchErr)})
	}

	now := time.Now().UTC()
	tidalPhase := classifyTidalPhase(result.Extremes, now)
	doubleHigh, doubleLow := hasDoubleTide(result.Extremes, result.Station, now)
	state := tideTodayData{
		Datetime:            now,
		CurrentTideHeightFt: result.CurrentHeightM * metersToFeet,
		TideDirection:       result.Direction,
		HighTideTime:        now,
		LowTideTime:         now.Add(24 * time.Hour),
		TidalPhase:          tidalPhase,
		DoubleHighToday:     doubleHigh,
		DoubleLowToday:      doubleLow,
	}

	for _, extreme := range result.Extremes {
		if !extreme.Time.After(now) {
			continue
		}
		if extreme.High && state.HighTideHeightFt == 0 {
			state.HighTideTime = extreme.Time
			state.HighTideHeightFt = extreme.HeightM * metersToFeet
		} else if !extreme.High && state.LowTideHeightFt == 0 {
			state.LowTideTime = extreme.Time
			state.LowTideHeightFt = extreme.HeightM * metersToFeet
		}
		if state.HighTideHeightFt != 0 && state.LowTideHeightFt != 0 {
			break
		}
	}

	response := tideTodayResponse{Datetime: state.Datetime.Format(time.RFC3339), CurrentTideHeightFt: state.CurrentTideHeightFt, TideDirection: state.TideDirection, HighTideTime: state.HighTideTime.Format(time.RFC3339), HighTideHeightFt: state.HighTideHeightFt, LowTideTime: state.LowTideTime.Format(time.RFC3339), LowTideHeightFt: state.LowTideHeightFt, StationName: result.Station.Name, Provider: configuredProvider, TidalPhase: state.TidalPhase, DoubleHighToday: state.DoubleHighToday, DoubleLowToday: state.DoubleLowToday}
	etag, err := weakETagForJSON(tideTodayETagData{CurrentTideHeightFt: state.CurrentTideHeightFt, TideDirection: state.TideDirection, HighTideTime: state.HighTideTime, HighTideHeightFt: state.HighTideHeightFt, LowTideTime: state.LowTideTime, LowTideHeightFt: state.LowTideHeightFt, StationName: result.Station.Name, Provider: configuredProvider, TidalPhase: state.TidalPhase, DoubleHighToday: state.DoubleHighToday, DoubleLowToday: state.DoubleLowToday})
	if err != nil {
		log.Printf("Failed to build tide ETag: %v", err)
	}
	return respondJSONWithETag(c, http.StatusOK, etag, response)
}

func fetchWeatherKitData(latitude, longitude float64, observedAt time.Time, localLocation *time.Location) (weatherTodayData, error) {
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
			data.Condition = formatWeatherConditionAt(condition, observedAt, localLocation, false)
		} else if condition, ok := current["condition"].(string); ok {
			data.Condition = formatWeatherConditionAt(condition, observedAt, localLocation, false)
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

func fetchWeatherKitForecastBundleData(latitude, longitude float64, daysCount int, referenceDatetime time.Time, localLocation *time.Location) (weatherForecastDataBundle, error) {
	bundle := weatherForecastDataBundle{}
	if daysCount <= 0 {
		daysCount = 6
	}

	if referenceDatetime.IsZero() {
		return bundle, fmt.Errorf("missing SignalK navigation.datetime reference")
	}

	if localLocation == nil {
		localLocation = time.Local
	}

	keyID := getEnv("WEATHERKIT_KEY_ID", "")
	teamID := getEnv("WEATHERKIT_TEAM_ID", "")
	serviceID := getEnv("WEATHERKIT_SERVICE_ID", "")
	privateKeyPEM := getEnv("WEATHERKIT_PRIVATE_KEY", "")

	if keyID == "" || teamID == "" || serviceID == "" || privateKeyPEM == "" {
		return bundle, fmt.Errorf("WeatherKit credentials not configured")
	}

	token, err := generateWeatherKitJWT(keyID, teamID, serviceID, privateKeyPEM)
	if err != nil {
		return bundle, fmt.Errorf("failed to generate JWT: %v", err)
	}

	requestURL := fmt.Sprintf("https://weatherkit.apple.com/api/v1/weather/en/%.4f/%.4f?dataSets=forecastDaily,forecastHourly", latitude, longitude)
	req, err := http.NewRequest("GET", requestURL, nil)
	if err != nil {
		return bundle, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return bundle, fmt.Errorf("failed to fetch weather forecast: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return bundle, fmt.Errorf("WeatherKit forecast API returned %d: %s", resp.StatusCode, string(body))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return bundle, fmt.Errorf("failed to read forecast response body: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		snippet := string(bodyBytes)
		if len(snippet) > 500 {
			snippet = snippet[:500]
		}
		return bundle, fmt.Errorf("failed to parse forecast response: %v; body: %s", err, snippet)
	}

	forecastDaily, ok := result["forecastDaily"].(map[string]any)
	if !ok {
		return bundle, fmt.Errorf("forecastDaily missing from WeatherKit response")
	}

	localTodayKey := referenceDatetime.In(localLocation).Format("2006-01-02")

	rawDays, ok := forecastDaily["days"].([]any)
	if !ok || len(rawDays) == 0 {
		return bundle, fmt.Errorf("forecastDaily.days missing from WeatherKit response")
	}

	windSeriesByDay := buildDailyWindSeries(result, localLocation)
	precipSeriesByDay := buildDailyPrecipitationSeries(result, localLocation)
	uvSeriesByDay := buildDailyUVSeries(result, localLocation)
	cloudSeriesByDay := buildDailyCloudSeries(result, localLocation)

	waveSeriesByDay, waveErr := fetchOpenMeteoMarineForecast(latitude, longitude)
	if waveErr != nil {
		log.Printf("Open-Meteo marine forecast error: %v", waveErr)
	}

	forecast := make([]weatherForecastDayData, 0, daysCount)
	var sunsetAt time.Time
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
		dayKey := ""
		if forecastStart, ok := dayMap["forecastStart"].(string); ok {
			if parsed, parseErr := time.Parse(time.RFC3339, forecastStart); parseErr == nil {
				localized := parsed.In(localLocation)
				dayKey = localized.Format("2006-01-02")
				dateLabel = localized.Format("Jan 2")
				dayName = localized.Weekday().String()
			}
		}

		if dayKey != "" && dayKey < localTodayKey {
			continue
		}

		if dayKey == localTodayKey {
			if sunsetValue, ok := dayMap["sunset"].(string); ok {
				if parsedSunset, parseErr := time.Parse(time.RFC3339, sunsetValue); parseErr == nil {
					sunsetAt = parsedSunset.In(localLocation)
				}
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
			condition = formatWeatherConditionAt(code, referenceDatetime, localLocation, true)
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
			if gust, ok := daytimeForecast["windGustSpeedMax"].(float64); ok {
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
			if gust, ok := dayMap["windGustSpeedMax"].(float64); ok {
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

		sunriseTime := formatWeatherKitLocalTime(dayMap, "sunrise", localLocation)
		sunsetTime := formatWeatherKitLocalTime(dayMap, "sunset", localLocation)

		moonPhase := ""
		if phase, ok := dayMap["moonPhase"].(string); ok {
			moonPhase = phase
		}

		forecast = append(forecast, weatherForecastDayData{
			Date:                 dateLabel,
			DayName:              dayName,
			Condition:            condition,
			HighTempF:            highTempF,
			LowTempF:             lowTempF,
			WindSpeedKts:         windSpeedKts,
			WindGustKts:          windGustKts,
			WindDirection:        windDirection,
			WindSummary:          buildWindSummary(windSeriesByDay[dayKey]),
			WaveSummary:          buildWaveSummary(waveSeriesByDay[dayKey]),
			PrecipitationPct:     precipPct,
			PrecipitationSummary: buildPrecipitationSummary(precipSeriesByDay[dayKey]),
			SunriseTime:          sunriseTime,
			SunsetTime:           sunsetTime,
			MoonPhase:            moonPhase,
			HourlyWind:           windSeriesByDay[dayKey],
			HourlyWave:           waveSeriesByDay[dayKey],
			HourlyPrecip:         precipSeriesByDay[dayKey],
			HourlyUV:             uvSeriesByDay[dayKey],
			HourlyCloud:          cloudSeriesByDay[dayKey],
		})
	}

	if len(forecast) == 0 {
		return bundle, fmt.Errorf("no forecast days parsed from WeatherKit response")
	}

	bundle.Days = forecast
	bundle.HourlyToday = buildWeatherHourlyEntries(result, referenceDatetime, localLocation, localTodayKey, sunsetAt)
	bundle.Summary = summarizeHourlyForecast(bundle.HourlyToday)

	return bundle, nil
}

// buildDailyWindSeries buckets WeatherKit's hourly forecast into per-day
// (local date) hourly wind series, keyed by "2006-01-02".
func buildDailyWindSeries(result map[string]any, localLocation *time.Location) map[string][]weatherHourlyWindData {
	forecastHourly, ok := result["forecastHourly"].(map[string]any)
	if !ok {
		return nil
	}

	rawHours, ok := forecastHourly["hours"].([]any)
	if !ok {
		return nil
	}

	series := make(map[string][]weatherHourlyWindData)
	for _, rawHour := range rawHours {
		hourMap, ok := rawHour.(map[string]any)
		if !ok {
			continue
		}

		forecastStart, ok := hourMap["forecastStart"].(string)
		if !ok {
			continue
		}

		parsed, err := time.Parse(time.RFC3339, forecastStart)
		if err != nil {
			continue
		}
		localTime := parsed.In(localLocation)

		windSpeedKts := -1.0
		if speed, ok := hourMap["windSpeed"].(float64); ok {
			windSpeedKts = speed * kphToKnots
		}

		windGustKts := -1.0
		if gust, ok := hourMap["windGust"].(float64); ok {
			windGustKts = gust * kphToKnots
		} else if windSpeedKts >= 0 {
			windGustKts = windSpeedKts
		}

		windDirection := "—"
		windDirectionDeg := -1.0
		if directionDeg, ok := hourMap["windDirection"].(float64); ok {
			windDirection = degreesToDirection(directionDeg)
			windDirectionDeg = directionDeg
		}

		dayKey := localTime.Format("2006-01-02")
		series[dayKey] = append(series[dayKey], weatherHourlyWindData{
			Label:            localTime.Format("3PM"),
			HourOfDay:        localTime.Hour(),
			WindSpeedKts:     windSpeedKts,
			WindGustKts:      windGustKts,
			WindDirection:    windDirection,
			WindDirectionDeg: windDirectionDeg,
		})
	}

	return series
}

// buildDailyPrecipitationSeries buckets WeatherKit's hourly forecast into
// per-day (local date) hourly precipitation series, keyed by "2006-01-02".
func buildDailyPrecipitationSeries(result map[string]any, localLocation *time.Location) map[string][]weatherHourlyPrecipitationData {
	forecastHourly, ok := result["forecastHourly"].(map[string]any)
	if !ok {
		return nil
	}

	rawHours, ok := forecastHourly["hours"].([]any)
	if !ok {
		return nil
	}

	series := make(map[string][]weatherHourlyPrecipitationData)
	for _, rawHour := range rawHours {
		hourMap, ok := rawHour.(map[string]any)
		if !ok {
			continue
		}

		forecastStart, ok := hourMap["forecastStart"].(string)
		if !ok {
			continue
		}

		parsed, err := time.Parse(time.RFC3339, forecastStart)
		if err != nil {
			continue
		}
		localTime := parsed.In(localLocation)

		chancePct := 0.0
		if chance, ok := hourMap["precipitationChance"].(float64); ok {
			chancePct = chance * 100
		}

		intensityMm := 0.0
		if intensity, ok := hourMap["precipitationIntensity"].(float64); ok {
			intensityMm = math.Max(0, intensity)
		}

		dayKey := localTime.Format("2006-01-02")
		series[dayKey] = append(series[dayKey], weatherHourlyPrecipitationData{
			Label:                    localTime.Format("3PM"),
			HourOfDay:                localTime.Hour(),
			PrecipitationChancePct:   chancePct,
			PrecipitationIntensityMm: intensityMm,
		})
	}

	return series
}

// buildDailyUVSeries buckets WeatherKit's hourly forecast into per-day
// (local date) hourly UV index series, keyed by "2006-01-02".
func buildDailyUVSeries(result map[string]any, localLocation *time.Location) map[string][]weatherHourlyUVData {
	forecastHourly, ok := result["forecastHourly"].(map[string]any)
	if !ok {
		return nil
	}

	rawHours, ok := forecastHourly["hours"].([]any)
	if !ok {
		return nil
	}

	series := make(map[string][]weatherHourlyUVData)
	for _, rawHour := range rawHours {
		hourMap, ok := rawHour.(map[string]any)
		if !ok {
			continue
		}

		forecastStart, ok := hourMap["forecastStart"].(string)
		if !ok {
			continue
		}

		parsed, err := time.Parse(time.RFC3339, forecastStart)
		if err != nil {
			continue
		}
		localTime := parsed.In(localLocation)

		uvValue := 0.0
		if uv, ok := hourMap["uvIndex"].(float64); ok {
			uvValue = math.Max(0, uv)
		}

		dayKey := localTime.Format("2006-01-02")
		series[dayKey] = append(series[dayKey], weatherHourlyUVData{
			Label:   localTime.Format("3PM"),
			UVIndex: uvValue,
		})
	}

	return series
}

// buildDailyCloudSeries buckets WeatherKit's hourly forecast into per-day
// (local date) hourly cloud-condition/temperature series, keyed by
// "2006-01-02". Condition and daylight come straight from WeatherKit's own
// per-hour conditionCode/daylight fields rather than being re-derived
// client-side, so the frontend can reuse the existing condition-to-icon
// mapping unchanged.
func buildDailyCloudSeries(result map[string]any, localLocation *time.Location) map[string][]weatherHourlyCloudData {
	forecastHourly, ok := result["forecastHourly"].(map[string]any)
	if !ok {
		return nil
	}

	rawHours, ok := forecastHourly["hours"].([]any)
	if !ok {
		return nil
	}

	series := make(map[string][]weatherHourlyCloudData)
	for _, rawHour := range rawHours {
		hourMap, ok := rawHour.(map[string]any)
		if !ok {
			continue
		}

		forecastStart, ok := hourMap["forecastStart"].(string)
		if !ok {
			continue
		}

		parsed, err := time.Parse(time.RFC3339, forecastStart)
		if err != nil {
			continue
		}
		localTime := parsed.In(localLocation)

		isDaylight := false
		if daylight, ok := hourMap["daylight"].(bool); ok {
			isDaylight = daylight
		}

		condition := "Unknown"
		if code, ok := hourMap["conditionCode"].(string); ok {
			condition = formatWeatherConditionAt(code, localTime, localLocation, false)
		}

		temperatureF := -1.0
		if tempC, ok := hourMap["temperature"].(float64); ok {
			temperatureF = (tempC * 9 / 5) + 32
		}

		dayKey := localTime.Format("2006-01-02")
		series[dayKey] = append(series[dayKey], weatherHourlyCloudData{
			Label:        localTime.Format("3PM"),
			HourOfDay:    localTime.Hour(),
			Condition:    condition,
			TemperatureF: temperatureF,
			IsDaylight:   isDaylight,
		})
	}

	return series
}

// formatWeatherKitLocalTime parses an RFC3339 timestamp from the given
// WeatherKit day field and formats it in the vessel's local timezone using
// the same "3:04PM" convention used elsewhere for sunset markers.
func formatWeatherKitLocalTime(dayMap map[string]any, field string, localLocation *time.Location) string {
	value, ok := dayMap[field].(string)
	if !ok {
		return ""
	}

	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return ""
	}

	return parsed.In(localLocation).Format("3:04PM")
}

// buildWindSummary formats a human-readable sentence describing a day's wind
// speed range, direction and peak gust, derived from its hourly wind series so
// it stays numerically consistent with the wind graph.
func buildWindSummary(hourly []weatherHourlyWindData) string {
	minSpeed := math.MaxFloat64
	maxSpeed := -1.0
	maxGust := -1.0
	found := false

	for _, entry := range hourly {
		if entry.WindSpeedKts < 0 {
			continue
		}
		found = true
		if entry.WindSpeedKts < minSpeed {
			minSpeed = entry.WindSpeedKts
		}
		if entry.WindSpeedKts > maxSpeed {
			maxSpeed = entry.WindSpeedKts
		}
		if entry.WindGustKts > maxGust {
			maxGust = entry.WindGustKts
		}
	}

	if !found {
		return ""
	}

	minRounded := int(math.Round(minSpeed))
	maxRounded := int(math.Round(maxSpeed))
	gustRounded := int(math.Round(maxGust))

	speedPhrase := fmt.Sprintf("%d to %d kts", minRounded, maxRounded)
	if minRounded == maxRounded {
		speedPhrase = fmt.Sprintf("around %d kts", maxRounded)
	}

	if directionRange := windDirectionRange(hourly); directionRange != "" {
		speedPhrase = fmt.Sprintf("%s from the %s", speedPhrase, directionRange)
	}

	if gustRounded > maxRounded {
		return fmt.Sprintf("Winds %s, gusting to %d kts.", speedPhrase, gustRounded)
	}

	return fmt.Sprintf("Winds %s.", speedPhrase)
}

// windDirectionRange describes how a day's wind direction shifts from
// morning to evening, e.g. "S-SE" if it backs/veers between the first and
// last hourly readings, or a single compass point if it stays steady.
func windDirectionRange(hourly []weatherHourlyWindData) string {
	start := ""
	end := ""

	for _, entry := range hourly {
		if entry.WindDirection == "" || entry.WindDirection == "—" {
			continue
		}
		if start == "" {
			start = entry.WindDirection
		}
		end = entry.WindDirection
	}

	if start == "" {
		return ""
	}
	if end == start {
		return start
	}

	return fmt.Sprintf("%s-%s", start, end)
}

// buildWaveSummary formats a human-readable sentence describing a day's swell
// height range, direction and period, derived from its hourly wave series so
// it stays numerically consistent with the wave graph.
func buildWaveSummary(hourly []weatherHourlyWaveData) string {
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

// buildPrecipitationSummary describes a day's rain outlook in a single
// sentence, e.g. "Slight chance of rain after 5PM." or "Little to no rain
// is expected.", mirroring buildWindSummary/buildWaveSummary.
func buildPrecipitationSummary(hourly []weatherHourlyPrecipitationData) string {
	maxChance := -1.0
	maxIntensity := 0.0
	firstNotableIdx := -1
	found := false

	for i, entry := range hourly {
		found = true
		if entry.PrecipitationChancePct > maxChance {
			maxChance = entry.PrecipitationChancePct
		}
		if entry.PrecipitationIntensityMm > maxIntensity {
			maxIntensity = entry.PrecipitationIntensityMm
		}
		if firstNotableIdx == -1 && entry.PrecipitationChancePct >= 30 {
			firstNotableIdx = i
		}
	}

	if !found {
		return ""
	}

	if maxChance < 30 {
		return "Little to no rain is expected."
	}

	when := "throughout the day"
	if firstNotableIdx > 0 {
		when = fmt.Sprintf("after %s", hourly[firstNotableIdx].Label)
	}

	if maxChance < 60 {
		return fmt.Sprintf("Slight chance of rain %s.", when)
	}

	phrase := "Showers"
	if maxIntensity >= 7.6 {
		phrase = "Heavy rain"
	} else if maxIntensity >= 2.5 {
		phrase = "Rain"
	}

	return fmt.Sprintf("%s expected %s.", phrase, when)
}

// waveDirectionRange describes how a day's swell direction shifts from
// morning to evening, e.g. "ENE-E" if it backs/veers between the first and
// last hourly readings, or a single compass point if it stays steady.
func waveDirectionRange(hourly []weatherHourlyWaveData) string {
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

// fetchOpenMeteoMarineForecast fetches an hourly wave forecast from the free,
// keyless Open-Meteo Marine API and buckets it per-day (local date), keyed by
// "2006-01-02".
func fetchOpenMeteoMarineForecast(latitude, longitude float64) (map[string][]weatherHourlyWaveData, error) {
	// Pinned to NOAA's GFS-Wave (WaveWatch III) model so swell height, period
	// and direction line up with other WaveWatch III-based swell forecasts
	// for this coastline; Open-Meteo's default "best_match" model runs
	// noticeably lower/different here.
	requestURL := fmt.Sprintf("https://marine-api.open-meteo.com/v1/marine?latitude=%.4f&longitude=%.4f&hourly=wave_height,wave_direction,wave_period,wind_wave_height,swell_wave_height&timezone=auto&forecast_days=10&models=ncep_gfswave025", latitude, longitude)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(requestURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch marine forecast: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read marine forecast response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("open-meteo marine API returned %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result map[string]any
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to parse marine forecast response: %v", err)
	}

	return parseOpenMeteoMarineResponse(result)
}

// fetchOpenMeteoSeaTemperatureF fetches the current sea surface temperature
// from the free, keyless Open-Meteo Marine API. Deliberately does not pin a
// `models=` param (unlike fetchOpenMeteoMarineForecast) — the wave-specific
// NOAA GFS-Wave model that wave data is pinned to does not carry sea surface
// temperature at all (returns null/"undefined" units); only Open-Meteo's
// default model blend does.
func fetchOpenMeteoSeaTemperatureF(latitude, longitude float64) (float64, error) {
	requestURL := fmt.Sprintf("https://marine-api.open-meteo.com/v1/marine?latitude=%.4f&longitude=%.4f&current=sea_surface_temperature&temperature_unit=fahrenheit", latitude, longitude)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(requestURL)
	if err != nil {
		return -1, fmt.Errorf("failed to fetch sea temperature: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return -1, fmt.Errorf("failed to read sea temperature response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return -1, fmt.Errorf("open-meteo marine API returned %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result map[string]any
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return -1, fmt.Errorf("failed to parse sea temperature response: %v", err)
	}

	return parseOpenMeteoSeaTemperatureResponse(result)
}

func parseOpenMeteoSeaTemperatureResponse(result map[string]any) (float64, error) {
	current, ok := result["current"].(map[string]any)
	if !ok {
		return -1, fmt.Errorf("current missing from sea temperature response")
	}

	temp, ok := current["sea_surface_temperature"].(float64)
	if !ok {
		return -1, fmt.Errorf("sea_surface_temperature missing or unavailable for this location")
	}

	return temp, nil
}

func parseOpenMeteoMarineResponse(result map[string]any) (map[string][]weatherHourlyWaveData, error) {
	hourly, ok := result["hourly"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("hourly missing from marine forecast response")
	}

	times, ok := hourly["time"].([]any)
	if !ok {
		return nil, fmt.Errorf("hourly.time missing from marine forecast response")
	}

	heights, _ := hourly["wave_height"].([]any)
	periods, _ := hourly["wave_period"].([]any)
	directions, _ := hourly["wave_direction"].([]any)
	windWaveHeights, _ := hourly["wind_wave_height"].([]any)
	swellWaveHeights, _ := hourly["swell_wave_height"].([]any)

	series := make(map[string][]weatherHourlyWaveData)
	for i, rawTime := range times {
		timeStr, ok := rawTime.(string)
		if !ok {
			continue
		}

		// Open-Meteo returns local time without a UTC offset, e.g. "2026-06-15T00:00".
		parsed, err := time.Parse("2006-01-02T15:04", timeStr)
		if err != nil {
			continue
		}

		point := weatherHourlyWaveData{Label: parsed.Format("3PM"), HourOfDay: parsed.Hour()}
		if i < len(heights) {
			if h, ok := heights[i].(float64); ok {
				point.WaveHeightM = h
			}
		}
		if i < len(periods) {
			if p, ok := periods[i].(float64); ok {
				point.WavePeriodS = p
			}
		}
		if i < len(directions) {
			if d, ok := directions[i].(float64); ok {
				point.WaveDirectionDeg = d
			}
		}
		if i < len(windWaveHeights) {
			if w, ok := windWaveHeights[i].(float64); ok {
				point.WindWaveHeightM = w
			}
		}
		if i < len(swellWaveHeights) {
			if s, ok := swellWaveHeights[i].(float64); ok {
				point.SwellWaveHeightM = s
			}
		}

		dayKey := parsed.Format("2006-01-02")
		series[dayKey] = append(series[dayKey], point)
	}

	return series, nil
}

func fetchWeatherKitForecastData(latitude, longitude float64, daysCount int, referenceDatetime time.Time, localLocation *time.Location) ([]weatherForecastDayData, error) {
	bundle, err := fetchWeatherKitForecastBundleData(latitude, longitude, daysCount, referenceDatetime, localLocation)
	if err != nil {
		return nil, err
	}
	return bundle.Days, nil
}

func buildWeatherHourlyEntries(result map[string]any, referenceDatetime time.Time, localLocation *time.Location, localTodayKey string, sunsetAt time.Time) []weatherHourlyEntryData {
	forecastHourly, ok := result["forecastHourly"].(map[string]any)
	if !ok {
		return nil
	}
	rawHours, ok := forecastHourly["hours"].([]any)
	if !ok || len(rawHours) == 0 {
		return nil
	}

	referenceLocal := referenceDatetime.In(localLocation)
	referenceHour := referenceLocal.Truncate(time.Hour)
	entries := make([]weatherHourlyEntryData, 0, 12)
	insertedSunset := false

	for _, rawHour := range rawHours {
		if len(entries) >= 12 {
			break
		}
		hourMap, ok := rawHour.(map[string]any)
		if !ok {
			continue
		}
		forecastStart, ok := hourMap["forecastStart"].(string)
		if !ok {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, forecastStart)
		if err != nil {
			continue
		}
		localTime := parsed.In(localLocation)
		if localTime.Before(referenceHour) {
			continue
		}

		if !insertedSunset && !sunsetAt.IsZero() && sunsetAt.After(referenceLocal) && localTime.After(sunsetAt) {
			entries = append(entries, weatherHourlyEntryData{Label: sunsetAt.Format("3:04PM"), Condition: "Sunset", TemperatureF: -1, WindSpeedKts: -1, WindGustKts: -1, WindDirection: "—", WindDirectionDeg: -1, Kind: "sunset"})
			insertedSunset = true
			if len(entries) >= 12 {
				break
			}
		}

		condition := "Unknown"
		if code, ok := hourMap["conditionCode"].(string); ok {
			condition = formatWeatherConditionAt(code, localTime, localLocation, false)
		}

		temperatureF := -1.0
		if tempC, ok := hourMap["temperature"].(float64); ok {
			temperatureF = (tempC * 9 / 5) + 32
		}

		windSpeedKts := -1.0
		if speed, ok := hourMap["windSpeed"].(float64); ok {
			windSpeedKts = speed * kphToKnots
		}

		windGustKts := -1.0
		if gust, ok := hourMap["windGust"].(float64); ok {
			windGustKts = gust * kphToKnots
		} else if windSpeedKts >= 0 {
			windGustKts = windSpeedKts
		}

		windDirection := "—"
		windDirectionDeg := -1.0
		if directionDeg, ok := hourMap["windDirection"].(float64); ok {
			windDirection = degreesToDirection(directionDeg)
			windDirectionDeg = directionDeg
		}

		label := localTime.Format("3PM")
		if localTime.Equal(referenceHour) {
			label = "Now"
		}

		entries = append(entries, weatherHourlyEntryData{Label: label, Condition: condition, TemperatureF: temperatureF, WindSpeedKts: windSpeedKts, WindGustKts: windGustKts, WindDirection: windDirection, WindDirectionDeg: windDirectionDeg, Kind: "forecast"})
	}

	if !insertedSunset && !sunsetAt.IsZero() && sunsetAt.Format("2006-01-02") == localTodayKey && sunsetAt.After(referenceLocal) && len(entries) < 12 {
		entries = append(entries, weatherHourlyEntryData{Label: sunsetAt.Format("3:04PM"), Condition: "Sunset", TemperatureF: -1, Kind: "sunset"})
	}

	return entries
}

func summarizeHourlyForecast(entries []weatherHourlyEntryData) string {
	conditionCounts := map[string]int{}
	bestCondition := ""
	bestCount := 0
	windSamples := 0
	totalWindSpeed := 0.0
	maxWindGust := 0.0

	for _, entry := range entries {
		if entry.Kind != "forecast" {
			continue
		}
		if entry.Condition != "Unknown" {
			conditionCounts[entry.Condition]++
			if conditionCounts[entry.Condition] > bestCount {
				bestCount = conditionCounts[entry.Condition]
				bestCondition = entry.Condition
			}
		}
		if entry.WindSpeedKts >= 0 {
			totalWindSpeed += entry.WindSpeedKts
			windSamples++
		}
		if entry.WindGustKts > maxWindGust {
			maxWindGust = entry.WindGustKts
		}
	}

	if bestCondition == "" {
		return "Today's hourly forecast"
	}

	if windSamples == 0 {
		return fmt.Sprintf("%s conditions will continue all day.", bestCondition)
	}

	typicalWind := int(math.Round(totalWindSpeed / float64(windSamples)))
	gustWind := int(math.Round(maxWindGust))
	if gustWind < typicalWind {
		gustWind = typicalWind
	}

	return fmt.Sprintf("%s conditions will continue all day. Winds around %d kts with gusts up to %d kts.", bestCondition, typicalWind, gustWind)
}

func weatherKitForecastLocation(result map[string]any, forecastDaily map[string]any) *time.Location {
	if tz, ok := result["timezone"].(string); ok {
		if loc, err := time.LoadLocation(strings.TrimSpace(tz)); err == nil {
			return loc
		}
	}

	if metadata, ok := forecastDaily["metadata"].(map[string]any); ok {
		if tz, ok := metadata["timezone"].(string); ok {
			if loc, err := time.LoadLocation(strings.TrimSpace(tz)); err == nil {
				return loc
			}
		}
	}

	return time.UTC
}

func vesselLocalLocation(longitude float64) *time.Location {
	if longitude < -180 || longitude > 180 {
		return time.UTC
	}

	offsetHours := int(math.Round(longitude / 15))
	if offsetHours < -12 {
		offsetHours = -12
	}
	if offsetHours > 14 {
		offsetHours = 14
	}

	zoneLabel := fmt.Sprintf("UTC%+d", offsetHours)
	return time.FixedZone(zoneLabel, offsetHours*3600)
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
