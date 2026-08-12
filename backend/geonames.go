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

// cachedPlaceName returns the cached place name nearest (lat, lon) at the
// same 0.5-degree rounding precision the /api/place-name handler already
// uses, or "" on a cache miss. It never makes a GeoNames HTTP call itself -
// callers that need a "best effort, never block" geoname (like the nearby-
// vessel contact recorder) use this instead of placeName's live lookup,
// relying on the frontend's regular /api/place-name polling to keep the
// cache warm.
func cachedPlaceName(lat, lon float64) string {
	roundedLat := math.Round(lat*2) / 2
	roundedLng := math.Round(lon*2) / 2
	cacheKey := fmt.Sprintf("%.1f,%.1f", roundedLat, roundedLng)

	placeNameCache.mu.RLock()
	defer placeNameCache.mu.RUnlock()
	if cached, ok := placeNameCache.data[cacheKey]; ok && time.Since(cached.cachedAt) < placeNameCacheTTL {
		return cached.name
	}
	return ""
}

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

	if signalkURL == "" {
		return c.JSON(http.StatusOK, map[string]string{"name": ""})
	}

	vesselState, vesselErr := fetchSignalKVesselState()
	if vesselErr != nil || !hasUsableVesselPosition(vesselState.Latitude, vesselState.Longitude) {
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
