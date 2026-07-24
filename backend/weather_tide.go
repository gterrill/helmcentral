package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/labstack/echo/v4"
)

const (
	defaultTideCacheFile = "cache/tide_cache.json"
	metersToFeet         = 3.28084
)

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

// jsonCacheDescriptors only carries the "tide" entry now - the weather_today
// and weather_forecast fixed-TTL JSON caches were deleted in favor of the
// WASM plugin adapter's own per-plugin wasmPluginCache[T] (see
// wasm_weather_provider.go), the same way tide's WASM plugins already work.
var jsonCacheDescriptors = []jsonCacheDescriptor{
	{Name: "tide", EnvKey: "TIDE_CACHE_FILE", Fallback: defaultTideCacheFile, TTL: stormGlassTideCacheTTL},
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
	case "tide":
		tideCacheStore.mu.Lock()
		tideCacheStore.data = make(map[string]tideChartResult)
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

// weatherForecastDayData is the host's per-day forecast assembly, built by
// weather_providers.go's buildDayData from a weatherProvider's
// weatherDayPoint plus that day's bucketed hourly series. It carries no wave
// fields (WaveSummary/HourlyWave were removed here - waves get their own
// provider/endpoint in a later phase; weatherHourlyWaveData itself is left
// untouched below since a future wave phase still needs it).
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
	PrecipitationPct     float64
	PrecipitationSummary string
	SunriseTime          string
	SunsetTime           string
	MoonPhase            string
	HourlyWind           []weatherHourlyWindData
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
	loadTideCacheFromDisk()
}

// cacheFilePath resolves where a piece of runtime state lives. Precedence:
// an explicit per-file env override wins outright, otherwise the built-in
// relative fallback is rooted at HELMCENTRAL_STATE_DIR when one is set.
//
// The state dir exists so a non-primary stack (the E2E profile in
// docker-compose.dev.yml) can redirect *all* of its writes — routes,
// dashboard pages, secrets, caches — with one variable, and so state paths
// added later are isolated automatically rather than silently landing back
// in the developer's working tree.
func cacheFilePath(envKey, fallback string) string {
	if custom := strings.TrimSpace(os.Getenv(envKey)); custom != "" {
		return custom
	}
	if stateDir := strings.TrimSpace(os.Getenv("HELMCENTRAL_STATE_DIR")); stateDir != "" && !filepath.IsAbs(fallback) {
		return filepath.Join(stateDir, fallback)
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

func tideToday(c echo.Context) error {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	settings, err := readSettings(settingsPath)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to read settings: %v", err)})
	}

	uiMap, _ := settings["ui"].(map[string]any)

	configuredProvider := strings.TrimSpace(coerceString(uiMap["tide_provider"]))
	if configuredProvider == "" {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "no tide provider configured — set ui.tide_provider in Settings (e.g. \"stormglass\" with STORMGLASS_API_KEY set, \"bom\" for Australia, \"noaa\" for the US, or install another plugin; see README)"})
	}

	provider, ok := getTideProvider(configuredProvider)
	if !ok {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("unknown tide provider configured: %q (is the plugin installed in plugins/tides?)", configuredProvider)})
	}

	configuredStation := strings.TrimSpace(coerceString(uiMap["tide_station_id"]))
	if configuredStation == "" {
		if configuredProvider == "stormglass" {
			configuredStation = stormGlassVesselStationID
		} else {
			return c.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("no tide station configured for provider %q — select one in Settings", configuredProvider)})
		}
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
