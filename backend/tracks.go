package main

import (
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// ── Trail storage ─────────────────────────────────────────────────────────────

const maxTrackPoints = 1000

// selfTrack records self-vessel positions at all times (motoring, anchored, etc.)
// aisTrackMap holds per-vessel trails keyed by vessel name.
var (
	trackMu     sync.RWMutex
	selfTrack   = newVesselTrail()
	aisTrackMap = make(map[string]*vesselTrail)
)

// motoringTrail is kept separately: only records motoring state fixes
// and is pre-seeded from Influx on startup so reposition mode has history.
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

func recordTrackAIS(name string, lat, lon float64) {
	trackMu.Lock()
	defer trackMu.Unlock()
	if aisTrackMap[name] == nil {
		aisTrackMap[name] = newVesselTrail()
	}
	aisTrackMap[name].addPoint(lat, lon)
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

	// Sample self vessel
	state, err := fetchSignalKVesselState(signalkURL, vesselPath)
	if err == nil && state.Latitude >= -90 && state.Latitude <= 90 &&
		state.Longitude >= -180 && state.Longitude <= 180 {
		recordTrackSelf(state.Latitude, state.Longitude)

		// Also record post-anchor ring-buffer and motoring trail
		recordSelfTrailPoint(state.Latitude, state.Longitude)
		if isMotoring(state.Status) {
			recordMotoringPoint(state.Latitude, state.Longitude)
		}

		// Sample nearby AIS vessels
		ownName := loadBoatName(settingsPath)
		selfName := fetchSignalKSelfName(signalkURL, vesselPath)
		excluded := []string{ownName, selfName}
		nearby, nerr := fetchSignalKNearbyVessels(
			signalkURL, vesselsPath,
			state.Latitude, state.Longitude,
			time.Now().UTC(), excluded,
		)
		if nerr == nil {
			for _, v := range nearby {
				if v.Lat >= -90 && v.Lat <= 90 && v.Lon >= -180 && v.Lon <= 180 {
					recordTrackAIS(v.Name, v.Lat, v.Lon)
					recordAISTrailPoint(v.Name, v.Lat, v.Lon)
				}
			}
		}
	}
}

func isMotoring(status string) bool {
	s := trimEnvValue(status)
	return s == "motoring" ||
		s == "under way using engine" ||
		s == "under_way_using_engine"
}

// ── Influx seed ───────────────────────────────────────────────────────────────

func seedMotoringTrailFromInflux() {
	transitionAt := queryInfluxLastMotoringToStationaryTransition()

	var start, end time.Time
	if !transitionAt.IsZero() {
		start = transitionAt.Add(-2 * time.Hour)
		end = transitionAt
	} else {
		end = time.Now().UTC()
		start = end.Add(-2 * time.Hour)
	}

	points := queryInfluxMotoringTrailDownsampled(start, end, 15)
	if len(points) == 0 {
		return
	}

	motoringTrailMu.Lock()
	defer motoringTrailMu.Unlock()
	for _, p := range points {
		motoringTrail.addPointWithTimestamp(p.Lat, p.Lon, p.Timestamp)
	}
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
	aisPts := make(map[string][]*trailPoint)
	for name, trail := range aisTrackMap {
		if pts := trail.pointsSince(since); len(pts) > 0 {
			aisPts[name] = pts
		}
	}
	trackMu.RUnlock()

	aisWire := make(map[string][]trackPoint, len(aisPts))
	for name, pts := range aisPts {
		aisWire[name] = toWire(pts)
	}

	return c.JSON(http.StatusOK, map[string]any{
		"self": toWire(selfPts),
		"ais":  aisWire,
	})
}

// GET /api/tracks/motoring
// Returns the full motoring approach track, seeded from Influx at startup
// and appended to by live polling while motoring.
// Fetched once by the client when entering anchor reposition mode.
func getMotoringTrackHandler(c echo.Context) error {
	motoringTrailMu.RLock()
	pts := motoringTrail.pointsSince(time.Time{})
	motoringTrailMu.RUnlock()

	return c.JSON(http.StatusOK, map[string]any{
		"points": toWire(pts),
	})
}
