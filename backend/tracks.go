package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// ── Trail storage ─────────────────────────────────────────────────────────────

const maxTrackPoints = 1000

// selfTrack records self-vessel positions at all times (motoring, anchored, etc.).
var (
	trackMu   sync.RWMutex
	selfTrack = newVesselTrail()
)

// motoringTrail is kept separately: only records motoring state fixes,
// starting empty and filling purely from live sampling.
var (
	motoringTrailMu sync.RWMutex
	motoringTrail   = newVesselTrail()
)

// ── Internal recording ────────────────────────────────────────────────────────

func recordTrackSelf(lat, lon float64) {
	trackMu.Lock()
	defer trackMu.Unlock()
	selfTrack.addPoint(lat, lon)
}

func recordMotoringPoint(lat, lon float64) {
	motoringTrailMu.Lock()
	defer motoringTrailMu.Unlock()
	motoringTrail.addPoint(lat, lon)
}

// ── Server-side poller ────────────────────────────────────────────────────────

// startTrackPoller launches a background goroutine that samples both the
// self-vessel and all nearby AIS vessels every pollInterval. The client never
// needs to touch SignalK for trail data — it only calls /api/tracks.
func startTrackPoller(pollInterval time.Duration) {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")

	go func() {
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for range ticker.C {
			sampleTracks(settingsPath)
		}
	}()
}

func sampleTracks(settingsPath string) {
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)
	vesselPath := getEnv("SIGNALK_VESSEL_PATH", "/signalk/v1/api/vessels/self")
	vesselsPath := getEnv("SIGNALK_VESSELS_PATH", "/signalk/v1/api/vessels")

	if signalkURL == "" {
		return
	}

	now := time.Now().UTC() // shared by wind/depth/solar recording this tick

	// Sample self vessel
	state, err := fetchSignalKVesselState(signalkURL, vesselPath)
	if err == nil {
		if state.WindSpeedApparentKts >= 0 {
			windGustHistory.record(state.WindSpeedApparentKts, now)
		}
		if state.Depth >= 0 {
			depthHistory.record(state.Depth, now)
		}
	}

	solar, solarErr := fetchSignalKSolarState(signalkURL, vesselPath)
	if solarErr == nil && solar.CurrentW >= 0 {
		solarStats.record(solar.CurrentW, now)
		solarPowerHistory.record(solar.CurrentW, now)
	}

	if err == nil && state.Latitude >= -90 && state.Latitude <= 90 &&
		state.Longitude >= -180 && state.Longitude <= 180 {
		recordTrackSelf(state.Latitude, state.Longitude)

		// Also record post-anchor ring-buffer and motoring trail
		recordSelfTrailPoint(state.Latitude, state.Longitude)
		if isMotoring(state.Status) {
			recordMotoringPoint(state.Latitude, state.Longitude)
		}

		recordNearbyVesselContacts(signalkURL, vesselsPath, vesselPath, state)
	}
}

// recordNearbyVesselContacts fetches the current set of nearby AIS vessels
// and records a contact for each in globalNearbyContactStore. This runs
// once per server-owned 5-second poll tick (ADR 0001: the server owns
// sampling, independent of client polling), not from the /api/nearby-
// vessels HTTP handler, which can be hit far more often by several open
// browser tabs - recording from the handler would defeat "record once per
// encounter."
func recordNearbyVesselContacts(signalkURL, vesselsPath, vesselPath string, state vesselStateData) {
	if globalNearbyContactStore == nil {
		return
	}

	signalkSelfName := fetchSignalKSelfName(signalkURL, vesselPath)
	excludedNames := []string{signalkSelfName}

	now := time.Now().UTC()
	nearby, err := fetchSignalKNearbyVessels(signalkURL, vesselsPath, state.Latitude, state.Longitude, now, excludedNames)
	if err != nil {
		return
	}

	geoname := cachedPlaceName(state.Latitude, state.Longitude)
	for _, v := range nearby {
		key, ok := vesselContactKey(v.Mmsi)
		if !ok {
			log.Printf("Skipping nearby vessel contact for %q: no MMSI reported", v.Name)
			continue
		}
		if err := globalNearbyContactStore.recordContactIfNew(key, v.Name, v.Lat, v.Lon, geoname, state.Status, now); err != nil {
			log.Printf("Failed to record nearby vessel contact for %s: %v", key, err)
		}
	}
}

func isMotoring(status string) bool {
	s := trimEnvValue(status)
	return s == "motoring" ||
		s == "under way using engine" ||
		s == "under_way_using_engine"
}

// ── HTTP handlers ─────────────────────────────────────────────────────────────

// trackPoint is the wire format for a single timestamped position.
type trackPoint struct {
	Lat       float64   `json:"lat"`
	Lon       float64   `json:"lon"`
	Timestamp time.Time `json:"timestamp"`
}

func toWire(pts []*trailPoint) []trackPoint {
	if len(pts) == 0 {
		return nil
	}
	out := make([]trackPoint, len(pts))
	for i, p := range pts {
		out[i] = trackPoint{Lat: p.Lat, Lon: p.Lon, Timestamp: p.Timestamp}
	}
	return out
}

type signalKTrackSnapshot struct {
	Type        string        `json:"type"`
	Coordinates [][][]float64 `json:"coordinates"`
	Geometry    *struct {
		Type        string        `json:"type"`
		Coordinates [][][]float64 `json:"coordinates"`
	} `json:"geometry,omitempty"`
}

func fetchSignalKAISTrails(settingsPath string) map[string][]trackPoint {
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)
	if signalkURL == "" {
		return map[string][]trackPoint{}
	}

	vesselsPath := getEnv("SIGNALK_VESSELS_PATH", "/signalk/v1/api/vessels")
	tracksPath := getEnv("SIGNALK_TRACKS_PATH", "/signalk/v1/api/tracks")
	tracksURL := strings.TrimRight(signalkURL, "/") + "/" + strings.TrimLeft(tracksPath, "/")

	client := &http.Client{Timeout: 4 * time.Second}
	response, err := client.Get(tracksURL)
	if err != nil {
		return map[string][]trackPoint{}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return map[string][]trackPoint{}
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return map[string][]trackPoint{}
	}

	var payload map[string]signalKTrackSnapshot
	if err := json.Unmarshal(body, &payload); err != nil {
		return map[string][]trackPoint{}
	}

	nameMap, nameErr := fetchSignalKVesselNameMap(signalkURL, vesselsPath)
	if nameErr != nil {
		nameMap = map[string]string{}
	}
	signalkSelfName := strings.ToUpper(strings.TrimSpace(fetchSignalKSelfName(signalkURL, getEnv("SIGNALK_VESSEL_PATH", "/signalk/v1/api/vessels/self"))))

	result := make(map[string][]trackPoint, len(payload))
	now := time.Now().UTC()
	for vesselID, snapshot := range payload {
		var multiCoords [][][]float64
		if snapshot.Geometry != nil && len(snapshot.Geometry.Coordinates) > 0 {
			multiCoords = snapshot.Geometry.Coordinates
		} else if len(snapshot.Coordinates) > 0 {
			multiCoords = snapshot.Coordinates
		}

		if len(multiCoords) == 0 {
			continue
		}

		// Always take the most recent track segment
		coords := multiCoords[len(multiCoords)-1]
		if len(coords) == 0 {
			continue
		}

		// Track contexts are "vessels.<id>" but the vessels endpoint keys by "<id>" alone.
		lookupKey := strings.TrimPrefix(vesselID, "vessels.")
		if lookupKey == "self" {
			continue
		}

		name := strings.ToUpper(strings.TrimSpace(nameMap[lookupKey]))
		if name == "" {
			name = strings.ToUpper(strings.TrimSpace(compactVesselID(vesselID)))
		}
		if name == "SELF" || name == signalkSelfName {
			continue
		}

		start := now.Add(-time.Duration(len(coords)-1) * time.Second)
		points := make([]trackPoint, 0, len(coords))
		for i, coord := range coords {
			if len(coord) < 2 {
				continue
			}
			lon := coord[0]
			lat := coord[1]
			if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
				continue
			}
			points = append(points, trackPoint{Lat: lat, Lon: lon, Timestamp: start.Add(time.Duration(i) * time.Second)})
		}
		if len(points) > 0 {
			result[name] = points
		}
	}

	return result
}

// GET /api/tracks?since=<RFC3339>
// Returns self and AIS vessel tracks for anchor-watch display.
// Clients pass the timestamp of the last point they received so only
// new fixes are returned on subsequent polls.
func getTracksHandler(c echo.Context) error {
	sinceStr := c.QueryParam("since")
	var since time.Time
	if sinceStr != "" {
		var err error
		since, err = time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid since timestamp"})
		}
	}

	trackMu.RLock()
	selfPts := selfTrack.pointsSince(since)
	trackMu.RUnlock()

	aisWire := fetchSignalKAISTrails(getEnv("SETTINGS_FILE", "../settings.yaml"))

	return c.JSON(http.StatusOK, map[string]any{
		"self": toWire(selfPts),
		"ais":  aisWire,
	})
}

// GET /api/tracks/motoring
// Returns the full motoring approach track, built purely from live polling
// while motoring. Fetched once by the client when entering anchor
// reposition mode.
func getMotoringTrackHandler(c echo.Context) error {
	motoringTrailMu.RLock()
	pts := motoringTrail.pointsSince(time.Time{})
	motoringTrailMu.RUnlock()

	return c.JSON(http.StatusOK, map[string]any{
		"points": toWire(pts),
	})
}
