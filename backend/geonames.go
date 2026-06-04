package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

const placeNameCacheTTL = 1 * time.Hour

type placeNameCacheStore struct {
	mu   sync.RWMutex
	data map[string]placeNameCacheEntry
}

type placeNameCacheEntry struct {
	name     string
	cachedAt time.Time
}

var placeNameCache = &placeNameCacheStore{data: make(map[string]placeNameCacheEntry)}

type geoNamesResponse struct {
	Geonames []struct {
		Name        string `json:"name"`
		ToponymName string `json:"toponymName"`
	} `json:"geonames"`
}

func placeName(c echo.Context) error {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)
	vesselPath := getEnv("SIGNALK_VESSEL_PATH", "/signalk/v1/api/vessels/self")

	if signalkURL == "" {
		return c.JSON(http.StatusOK, map[string]string{"name": ""})
	}

	vesselState, vesselErr := fetchSignalKVesselState(signalkURL, vesselPath)
	if vesselErr != nil || vesselState.Latitude < -90 || vesselState.Latitude > 90 ||
		vesselState.Longitude < -180 || vesselState.Longitude > 180 {
		return c.JSON(http.StatusOK, map[string]string{"name": ""})
	}

	// Cache at 0.5-degree precision (~55 km) — coarse enough to avoid excessive lookups
	roundedLat := math.Round(vesselState.Latitude*2) / 2
	roundedLng := math.Round(vesselState.Longitude*2) / 2
	cacheKey := fmt.Sprintf("%.1f,%.1f", roundedLat, roundedLng)

	placeNameCache.mu.RLock()
	cached, ok := placeNameCache.data[cacheKey]
	placeNameCache.mu.RUnlock()

	if ok && time.Since(cached.cachedAt) < placeNameCacheTTL {
		return c.JSON(http.StatusOK, map[string]string{"name": cached.name})
	}

	username := getEnv("GEONAMES_USERNAME", "demo")
	url := fmt.Sprintf(
		"http://api.geonames.org/findNearbyPlaceNameJSON?lat=%f&lng=%f&username=%s&maxRows=1&radius=300",
		vesselState.Latitude, vesselState.Longitude, username,
	)

	resp, httpErr := http.Get(url) //nolint:noctx
	if httpErr != nil {
		log.Printf("GeoNames request failed: %v", httpErr)
		return c.JSON(http.StatusOK, map[string]string{"name": ""})
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		log.Printf("GeoNames response read failed: %v", readErr)
		return c.JSON(http.StatusOK, map[string]string{"name": ""})
	}

	var geoResp geoNamesResponse
	if parseErr := json.Unmarshal(body, &geoResp); parseErr != nil {
		log.Printf("GeoNames response parse failed: %v", parseErr)
		return c.JSON(http.StatusOK, map[string]string{"name": ""})
	}

	name := ""
	if len(geoResp.Geonames) > 0 {
		name = geoResp.Geonames[0].Name
		if name == "" {
			name = geoResp.Geonames[0].ToponymName
		}
	}

	placeNameCache.mu.Lock()
	placeNameCache.data[cacheKey] = placeNameCacheEntry{name: name, cachedAt: time.Now()}
	placeNameCache.mu.Unlock()

	return c.JSON(http.StatusOK, map[string]string{"name": name})
}
