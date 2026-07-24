package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const (
	stormGlassTideCacheTTL    = 72 * time.Hour
	stormGlassVesselStationID = "vessel-position"
)

type tideChartCache struct {
	mu   sync.RWMutex
	data map[string]tideChartResult
}

type tideChartResultDisk struct {
	Station        tideStation        `json:"station"`
	Extremes       []tideExtremePoint `json:"extremes"`
	CurrentHeightM float64            `json:"current_height_m"`
	Direction      string             `json:"direction"`
	CachedAt       time.Time          `json:"cached_at"`
}

var tideCacheStore = &tideChartCache{data: make(map[string]tideChartResult)}
var tideCacheHits uint64
var tideCacheMisses uint64

type stormGlassTideProvider struct{}

func newStormGlassTideProvider() *stormGlassTideProvider {
	return &stormGlassTideProvider{}
}

func (p *stormGlassTideProvider) ID() string   { return "stormglass" }
func (p *stormGlassTideProvider) Name() string { return "Storm Glass (current position)" }

// Description is hand-written (not WASM-derived) since this provider has no
// guest contract to export it from - see wasm_plugin.go's description()
// export for the WASM-plugin equivalent.
func (p *stormGlassTideProvider) Description() string {
	return "Fetches tide extremes for the vessel's current GPS position via the Storm Glass API (requires a paid API key). No fixed station list — always tracks the vessel's live position."
}

func (p *stormGlassTideProvider) TTLSeconds() int64 {
	return int64(stormGlassTideCacheTTL / time.Second)
}

func (p *stormGlassTideProvider) SearchStations(query string, limit int) []tideStation {
	return []tideStation{{StationID: stormGlassVesselStationID, Name: "Current Vessel Position"}}
}

func (p *stormGlassTideProvider) FetchTideChart(stationID string) (tideChartResult, error) {
	if stationID != stormGlassVesselStationID {
		return tideChartResult{}, fmt.Errorf("unknown Storm Glass tide station: %s", stationID)
	}

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)
	if signalkURL == "" {
		return tideChartResult{}, fmt.Errorf("SignalK URL is not configured")
	}

	vesselPath := getEnv("SIGNALK_VESSEL_PATH", "/signalk/v1/api/vessels/self")
	vesselState, err := fetchSignalKVesselState(signalkURL, vesselPath)
	if err != nil {
		return tideChartResult{}, fmt.Errorf("failed to fetch vessel position: %v", err)
	}
	if vesselState.Latitude < -90 || vesselState.Latitude > 90 || vesselState.Longitude < -180 || vesselState.Longitude > 180 {
		return tideChartResult{}, fmt.Errorf("invalid vessel coordinates from SignalK")
	}

	now := time.Now().UTC()
	roundedLat := math.Round(vesselState.Latitude*10) / 10
	roundedLng := math.Round(vesselState.Longitude*10) / 10
	cacheKey := fmt.Sprintf("%.1f,%.1f,%s", roundedLat, roundedLng, now.Format("2006-01-02"))

	tideCacheStore.mu.RLock()
	if cached, ok := tideCacheStore.data[cacheKey]; ok {
		tideCacheStore.mu.RUnlock()
		atomic.AddUint64(&tideCacheHits, 1)
		cached.Cached = true
		cached.CurrentHeightM, cached.Direction = interpolateTideNow(cached.Extremes, now)
		return cached, nil
	}
	tideCacheStore.mu.RUnlock()
	atomic.AddUint64(&tideCacheMisses, 1)

	result, err := fetchStormGlassTideChart(roundedLat, roundedLng, now)
	if err != nil {
		return tideChartResult{}, err
	}

	result.Station = tideStation{StationID: stormGlassVesselStationID, Name: "Current Vessel Position", Lat: vesselState.Latitude, Lon: vesselState.Longitude}
	result.CachedAt = now
	result.Cached = false

	tideCacheStore.mu.Lock()
	tideCacheStore.data[cacheKey] = result
	tideCacheStore.mu.Unlock()
	persistTideCacheToDisk()
	log.Printf("Cached Storm Glass tide data for %s", cacheKey)

	return result, nil
}

// fetchStormGlassTideChart fetches a week of tide extremes plus the current
// sea level for the given coordinates from the Storm Glass API.
func fetchStormGlassTideChart(lat, lng float64, now time.Time) (tideChartResult, error) {
	apiKey := getEnv("STORMGLASS_API_KEY", "")
	if apiKey == "" {
		return tideChartResult{}, fmt.Errorf("Storm Glass API key not configured")
	}

	client := &http.Client{Timeout: 10 * time.Second}

	startTime := now.Truncate(24 * time.Hour)
	extremesURL := fmt.Sprintf("https://api.stormglass.io/v2/tide/extremes/point?lat=%.1f&lng=%.1f&start=%d&end=%d", lat, lng, startTime.Unix(), startTime.Add(7*24*time.Hour).Unix())
	req, err := http.NewRequest("GET", extremesURL, nil)
	if err != nil {
		return tideChartResult{}, fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Authorization", apiKey)

	log.Printf("Fetching Storm Glass tide extremes for %.1f,%.1f", lat, lng)
	resp, err := client.Do(req)
	if err != nil {
		return tideChartResult{}, fmt.Errorf("failed to fetch tide extremes: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return tideChartResult{}, fmt.Errorf("failed to read response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return tideChartResult{}, fmt.Errorf("Storm Glass API returned %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result map[string]any
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return tideChartResult{}, fmt.Errorf("failed to parse response: %v", err)
	}

	extremes := []tideExtremePoint{}
	if data, ok := result["data"].([]any); ok {
		for _, item := range data {
			extreme, ok := item.(map[string]any)
			if !ok {
				continue
			}
			typeVal, _ := extreme["type"].(string)
			timeStr, _ := extreme["time"].(string)
			height, _ := extreme["height"].(float64)
			t, parseErr := time.Parse(time.RFC3339, timeStr)
			if parseErr != nil {
				continue
			}
			extremes = append(extremes, tideExtremePoint{Time: t, HeightM: height, High: typeVal == "high"})
		}
	}

	if len(extremes) == 0 {
		return tideChartResult{}, fmt.Errorf("no tide extremes returned by Storm Glass")
	}

	sort.Slice(extremes, func(i, j int) bool { return extremes[i].Time.Before(extremes[j].Time) })

	heightM, direction := interpolateTideNow(extremes, now)

	// The sea-level API gives a more accurate "now" reading than interpolation
	// between extremes, so use it when available.
	seaLevelURL := fmt.Sprintf("https://api.stormglass.io/v2/tide/sea-level/point?lat=%.2f&lng=%.2f&start=%d", lat, lng, now.Unix())
	req2, err := http.NewRequest("GET", seaLevelURL, nil)
	if err == nil {
		req2.Header.Set("Authorization", apiKey)
		if resp2, err := client.Do(req2); err == nil {
			defer resp2.Body.Close()
			if resp2.StatusCode == http.StatusOK {
				bodyBytes2, _ := io.ReadAll(resp2.Body)
				var seaResult map[string]any
				if json.Unmarshal(bodyBytes2, &seaResult) == nil {
					if data, ok := seaResult["data"].([]any); ok && len(data) > 0 {
						if current, ok := data[0].(map[string]any); ok {
							if seaHeight, ok := current["height"].(float64); ok {
								heightM = seaHeight
							}
						}
					}
				}
			}
		}
	}

	return tideChartResult{Extremes: extremes, CurrentHeightM: heightM, Direction: direction}, nil
}

func loadTideCacheFromDisk() {
	path := cacheFilePath("TIDE_CACHE_FILE", defaultTideCacheFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("Failed to read tide cache file %s: %v", path, err)
		}
		return
	}

	payload := map[string]tideChartResultDisk{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		log.Printf("Failed to parse tide cache file %s: %v", path, err)
		return
	}

	now := time.Now().UTC()
	loaded := make(map[string]tideChartResult, len(payload))
	for key, item := range payload {
		if now.Sub(item.CachedAt) >= stormGlassTideCacheTTL {
			continue
		}
		loaded[key] = tideChartResult{Station: item.Station, Extremes: item.Extremes, CurrentHeightM: item.CurrentHeightM, Direction: item.Direction, CachedAt: item.CachedAt}
	}

	tideCacheStore.mu.Lock()
	tideCacheStore.data = loaded
	tideCacheStore.mu.Unlock()
	log.Printf("Loaded %d tide cache entries from %s", len(loaded), path)
}

func persistTideCacheToDisk() {
	path := cacheFilePath("TIDE_CACHE_FILE", defaultTideCacheFile)

	tideCacheStore.mu.RLock()
	payload := make(map[string]tideChartResultDisk, len(tideCacheStore.data))
	for key, item := range tideCacheStore.data {
		payload[key] = tideChartResultDisk{Station: item.Station, Extremes: item.Extremes, CurrentHeightM: item.CurrentHeightM, Direction: item.Direction, CachedAt: item.CachedAt}
	}
	tideCacheStore.mu.RUnlock()

	if err := writeJSONFileAtomic(path, payload); err != nil {
		log.Printf("Failed to persist tide cache to %s: %v", path, err)
	}
}
