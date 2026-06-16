package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed data/bom_tide_sites.json
var bomTideSitesJSON []byte

const (
	bomTideCacheTTL         = 12 * time.Hour
	defaultBomTideCacheFile = "cache/tide_bom_cache.json"
	bomUserAgent            = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

type bomTideCache struct {
	mu   sync.RWMutex
	data map[string]tideChartResult
}

type bomTideCacheDisk struct {
	Station        tideStation        `json:"station"`
	Extremes       []tideExtremePoint `json:"extremes"`
	CurrentHeightM float64            `json:"current_height_m"`
	Direction      string             `json:"direction"`
	CachedAt       time.Time          `json:"cached_at"`
}

var bomTideCacheStore = &bomTideCache{data: make(map[string]tideChartResult)}

type bomGeoJSON struct {
	Features []struct {
		Properties struct {
			PortName  string  `json:"PORT_NAME"`
			AAC       string  `json:"AAC"`
			StateName string  `json:"STATE_NAME"`
			TimeZone  string  `json:"TIME_ZONE"`
			Lat       float64 `json:"LAT"`
			Lon       float64 `json:"LON"`
			Type      string  `json:"TYPE"`
		} `json:"properties"`
	} `json:"features"`
}

type bomTideProvider struct {
	stations []tideStation
}

func newBomTideProvider() *bomTideProvider {
	p := &bomTideProvider{}

	var parsed bomGeoJSON
	if err := json.Unmarshal(bomTideSitesJSON, &parsed); err != nil {
		log.Printf("Failed to parse embedded BOM tide site list: %v", err)
		return p
	}

	for _, feature := range parsed.Features {
		props := feature.Properties
		if props.Type != "TIDE" || props.AAC == "" {
			continue
		}
		p.stations = append(p.stations, tideStation{
			StationID: props.AAC,
			Name:      props.PortName,
			State:     props.StateName,
			Lat:       props.Lat,
			Lon:       props.Lon,
			Timezone:  props.TimeZone,
		})
	}

	return p
}

func (p *bomTideProvider) ID() string        { return "bom" }
func (p *bomTideProvider) Name() string      { return "Bureau of Meteorology (Australia)" }
func (p *bomTideProvider) TTLSeconds() int64 { return int64(bomTideCacheTTL / time.Second) }

func (p *bomTideProvider) SearchStations(query string, limit int) []tideStation {
	query = strings.ToLower(strings.TrimSpace(query))
	if limit <= 0 {
		limit = 20
	}

	results := make([]tideStation, 0, limit)
	for _, station := range p.stations {
		if query != "" &&
			!strings.Contains(strings.ToLower(station.Name), query) &&
			!strings.Contains(strings.ToLower(station.StationID), query) &&
			!strings.Contains(strings.ToLower(station.State), query) {
			continue
		}
		results = append(results, station)
		if len(results) >= limit {
			break
		}
	}

	return results
}

func (p *bomTideProvider) findStation(stationID string) (tideStation, bool) {
	for _, station := range p.stations {
		if station.StationID == stationID {
			return station, true
		}
	}
	return tideStation{}, false
}

var (
	bomTideTimeRegex   = regexp.MustCompile(`data-time-utc="([^"]+)"[^>]*class="localtime (high|low)-tide"`)
	bomTideHeightRegex = regexp.MustCompile(`class="height (?:high|low)-tide">([\d.]+)\s*m`)
)

func (p *bomTideProvider) FetchTideChart(stationID string) (tideChartResult, error) {
	station, ok := p.findStation(stationID)
	if !ok {
		return tideChartResult{}, fmt.Errorf("unknown BOM tide station: %s", stationID)
	}

	bomTideCacheStore.mu.RLock()
	if cached, ok := bomTideCacheStore.data[stationID]; ok && time.Since(cached.CachedAt) < bomTideCacheTTL {
		bomTideCacheStore.mu.RUnlock()
		cached.Cached = true
		cached.CurrentHeightM, cached.Direction = interpolateTideNow(cached.Extremes, time.Now().UTC())
		return cached, nil
	}
	bomTideCacheStore.mu.RUnlock()

	result, err := fetchBomTidesTable(station)
	if err != nil {
		bomTideCacheStore.mu.RLock()
		if cached, ok := bomTideCacheStore.data[stationID]; ok {
			bomTideCacheStore.mu.RUnlock()
			cached.Cached = true
			cached.CurrentHeightM, cached.Direction = interpolateTideNow(cached.Extremes, time.Now().UTC())
			return cached, nil
		}
		bomTideCacheStore.mu.RUnlock()
		return tideChartResult{}, err
	}

	result.CachedAt = time.Now().UTC()
	result.Cached = false

	bomTideCacheStore.mu.Lock()
	bomTideCacheStore.data[stationID] = result
	bomTideCacheStore.mu.Unlock()
	persistBomTideCacheToDisk()

	return result, nil
}

func fetchBomTidesTable(station tideStation) (tideChartResult, error) {
	now := time.Now().UTC()
	startDate := now.AddDate(0, 0, -1).Format("02-01-2006")

	requestURL := fmt.Sprintf("https://www.bom.gov.au/australia/tides/scripts/getTidesTable.php?aac=%s&date=%s&days=8&offset=0&type=tide", station.StationID, startDate)
	req, err := http.NewRequest("GET", requestURL, nil)
	if err != nil {
		return tideChartResult{}, fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("User-Agent", bomUserAgent)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return tideChartResult{}, fmt.Errorf("failed to fetch BOM tide table: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return tideChartResult{}, fmt.Errorf("failed to read BOM tide table response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return tideChartResult{}, fmt.Errorf("BOM tide table returned %d: %s", resp.StatusCode, string(bodyBytes))
	}

	extremes, err := parseBomTidesTable(string(bodyBytes))
	if err != nil {
		return tideChartResult{}, err
	}

	heightM, direction := interpolateTideNow(extremes, now)

	return tideChartResult{
		Station:        station,
		Extremes:       extremes,
		CurrentHeightM: heightM,
		Direction:      direction,
	}, nil
}

func parseBomTidesTable(html string) ([]tideExtremePoint, error) {
	timeMatches := bomTideTimeRegex.FindAllStringSubmatch(html, -1)
	heightMatches := bomTideHeightRegex.FindAllStringSubmatch(html, -1)

	if len(timeMatches) == 0 || len(timeMatches) != len(heightMatches) {
		return nil, fmt.Errorf("unexpected BOM tide table format (%d times, %d heights)", len(timeMatches), len(heightMatches))
	}

	extremes := make([]tideExtremePoint, 0, len(timeMatches))
	for i, timeMatch := range timeMatches {
		t, err := time.Parse(time.RFC3339, timeMatch[1])
		if err != nil {
			continue
		}
		height, err := strconv.ParseFloat(heightMatches[i][1], 64)
		if err != nil {
			continue
		}
		extremes = append(extremes, tideExtremePoint{
			Time:    t,
			HeightM: height,
			High:    timeMatch[2] == "high",
		})
	}

	sort.Slice(extremes, func(i, j int) bool { return extremes[i].Time.Before(extremes[j].Time) })

	return extremes, nil
}

func loadBomTideCacheFromDisk() {
	path := cacheFilePath("TIDE_BOM_CACHE_FILE", defaultBomTideCacheFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("Failed to read BOM tide cache file %s: %v", path, err)
		}
		return
	}

	payload := map[string]bomTideCacheDisk{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		log.Printf("Failed to parse BOM tide cache file %s: %v", path, err)
		return
	}

	now := time.Now().UTC()
	loaded := make(map[string]tideChartResult, len(payload))
	for key, item := range payload {
		if now.Sub(item.CachedAt) >= bomTideCacheTTL {
			continue
		}
		loaded[key] = tideChartResult{Station: item.Station, Extremes: item.Extremes, CurrentHeightM: item.CurrentHeightM, Direction: item.Direction, CachedAt: item.CachedAt}
	}

	bomTideCacheStore.mu.Lock()
	bomTideCacheStore.data = loaded
	bomTideCacheStore.mu.Unlock()
	log.Printf("Loaded %d BOM tide cache entries from %s", len(loaded), path)
}

func persistBomTideCacheToDisk() {
	path := cacheFilePath("TIDE_BOM_CACHE_FILE", defaultBomTideCacheFile)

	bomTideCacheStore.mu.RLock()
	payload := make(map[string]bomTideCacheDisk, len(bomTideCacheStore.data))
	for key, item := range bomTideCacheStore.data {
		payload[key] = bomTideCacheDisk{Station: item.Station, Extremes: item.Extremes, CurrentHeightM: item.CurrentHeightM, Direction: item.Direction, CachedAt: item.CachedAt}
	}
	bomTideCacheStore.mu.RUnlock()

	if err := writeJSONFileAtomic(path, payload); err != nil {
		log.Printf("Failed to persist BOM tide cache to %s: %v", path, err)
	}
}
