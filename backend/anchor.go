package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

const defaultAnchorWatchRadiusMeters = 20.0
const maxTrailPoints = 1000

type anchorWatchData struct {
	Lat           float64   `json:"lat"`
	Lon           float64   `json:"lon"`
	RadiusMeters  float64   `json:"radius_meters"`
	RodeDeployedM float64   `json:"rode_deployed_m"`
	SeaState      string    `json:"sea_state"`
	SeabedType    string    `json:"seabed_type"`
	SetAt         time.Time `json:"set_at"`
}

type trailPoint struct {
	Lat       float64   `json:"lat"`
	Lon       float64   `json:"lon"`
	Timestamp time.Time `json:"timestamp"`
}

type vesselTrail struct {
	points []*trailPoint
	index  int  // current write position in ring buffer
	full   bool // whether the buffer has wrapped around
}

func newVesselTrail() *vesselTrail {
	return &vesselTrail{
		points: make([]*trailPoint, maxTrailPoints),
		index:  0,
		full:   false,
	}
}

func (vt *vesselTrail) addPoint(lat, lon float64) {
	vt.addPointWithTimestamp(lat, lon, time.Now().UTC())
}

func (vt *vesselTrail) addPointWithTimestamp(lat, lon float64, ts time.Time) {
	vt.points[vt.index] = &trailPoint{
		Lat:       lat,
		Lon:       lon,
		Timestamp: ts,
	}
	vt.index = (vt.index + 1) % maxTrailPoints
	if vt.index == 0 {
		vt.full = true
	}
}

func (vt *vesselTrail) orderedPoints() []*trailPoint {
	if !vt.full && vt.index == 0 {
		return nil
	}

	result := make([]*trailPoint, 0, maxTrailPoints)
	if vt.full {
		start := vt.index
		for i := 0; i < maxTrailPoints; i++ {
			if p := vt.points[(start+i)%maxTrailPoints]; p != nil {
				result = append(result, p)
			}
		}
		return result
	}

	for i := 0; i < vt.index; i++ {
		if vt.points[i] != nil {
			result = append(result, vt.points[i])
		}
	}
	return result
}

// pointsSince returns all trail points after the given timestamp, in chronological order.
func (vt *vesselTrail) pointsSince(since time.Time) []*trailPoint {
	if !vt.full && vt.index == 0 {
		// No points yet
		return nil
	}

	var result []*trailPoint

	// If buffer is full, we have all maxTrailPoints entries
	if vt.full {
		// Start from index (oldest point) and go around
		start := vt.index
		for i := 0; i < maxTrailPoints; i++ {
			p := vt.points[(start+i)%maxTrailPoints]
			if p != nil && p.Timestamp.After(since) {
				result = append(result, p)
			}
		}
	} else {
		// Buffer not full yet, points are from 0 to index-1
		for i := 0; i < vt.index; i++ {
			if vt.points[i] != nil && vt.points[i].Timestamp.After(since) {
				result = append(result, vt.points[i])
			}
		}
	}

	return result
}

var (
	anchorWatchMu               sync.RWMutex
	anchorWatchState            *anchorWatchData
	lastAnchorWatchRadiusMeters = defaultAnchorWatchRadiusMeters

	trailMu   sync.RWMutex
	selfTrail *vesselTrail
)

func anchorWatchFilePath() string {
	return cacheFilePath("ANCHOR_WATCH_FILE", "cache/anchor_watch.json")
}

// recordSelfTrailPoint appends to post-anchor ring buffer only when active.
func recordSelfTrailPoint(lat, lon float64) {
	anchorWatchMu.RLock()
	active := anchorWatchState != nil
	anchorWatchMu.RUnlock()
	if !active {
		return
	}

	trailMu.Lock()
	defer trailMu.Unlock()
	if selfTrail != nil {
		selfTrail.addPoint(lat, lon)
	}
}

// getSelfTrailSince returns post-anchor ring-buffer points after the given timestamp.
func getSelfTrailSince(since time.Time) []*trailPoint {
	trailMu.RLock()
	defer trailMu.RUnlock()

	if selfTrail == nil {
		return nil
	}
	return selfTrail.pointsSince(since)
}

func loadAnchorWatch() {
	path := anchorWatchFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		// No file yet — start with no watch active.
		return
	}

	var loaded anchorWatchData
	if err := json.Unmarshal(data, &loaded); err != nil {
		return
	}

	anchorWatchMu.Lock()
	anchorWatchState = &loaded
	if loaded.RadiusMeters > 0 {
		lastAnchorWatchRadiusMeters = loaded.RadiusMeters
	}
	anchorWatchMu.Unlock()
}

func saveAnchorWatch(aw *anchorWatchData) error {
	return writeJSONFileAtomic(anchorWatchFilePath(), aw)
}

// GET /api/anchor-watch
func getAnchorWatch(c echo.Context) error {
	anchorWatchMu.RLock()
	state := anchorWatchState
	anchorWatchMu.RUnlock()

	if state == nil {
		return c.JSON(http.StatusOK, map[string]any{"active": false})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"active":          true,
		"lat":             state.Lat,
		"lon":             state.Lon,
		"radius_meters":   state.RadiusMeters,
		"rode_deployed_m": state.RodeDeployedM,
		"sea_state":       state.SeaState,
		"seabed_type":     state.SeabedType,
		"set_at":          state.SetAt.Format(time.RFC3339),
	})
}

// POST /api/anchor-watch
func setAnchorWatch(c echo.Context) error {
	var body struct {
		Lat          float64  `json:"lat"`
		Lon          float64  `json:"lon"`
		RadiusMeters *float64 `json:"radius_meters"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if body.Lat < -90 || body.Lat > 90 || body.Lon < -180 || body.Lon > 180 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "lat/lon out of range"})
	}

	radius := defaultAnchorWatchRadiusMeters
	anchorWatchMu.RLock()
	current := anchorWatchState
	previousRadius := lastAnchorWatchRadiusMeters
	anchorWatchMu.RUnlock()

	if body.RadiusMeters != nil && *body.RadiusMeters > 0 {
		radius = *body.RadiusMeters
	} else if current != nil && current.RadiusMeters > 0 {
		radius = current.RadiusMeters
	} else if previousRadius > 0 {
		radius = previousRadius
	}

	aw := &anchorWatchData{
		Lat:           body.Lat,
		Lon:           body.Lon,
		RadiusMeters:  radius,
		RodeDeployedM: 0,
		SeaState:      "calm",
		SeabedType:    "sand",
		SetAt:         time.Now().UTC(),
	}

	if err := saveAnchorWatch(aw); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to persist"})
	}

	anchorWatchMu.Lock()
	anchorWatchState = aw
	lastAnchorWatchRadiusMeters = aw.RadiusMeters
	anchorWatchMu.Unlock()

	// Reset post-anchor ring buffer.
	trailMu.Lock()
	selfTrail = newVesselTrail()
	trailMu.Unlock()

	return c.JSON(http.StatusOK, map[string]any{
		"active":          true,
		"lat":             aw.Lat,
		"lon":             aw.Lon,
		"radius_meters":   aw.RadiusMeters,
		"rode_deployed_m": aw.RodeDeployedM,
		"sea_state":       aw.SeaState,
		"seabed_type":     aw.SeabedType,
		"set_at":          aw.SetAt.Format(time.RFC3339),
	})
}

// PATCH /api/anchor-watch — update radius only (multi-client adjustment)
func patchAnchorWatch(c echo.Context) error {
	anchorWatchMu.RLock()
	current := anchorWatchState
	anchorWatchMu.RUnlock()

	if current == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "no active anchor watch"})
	}

	var body struct {
		RadiusMeters  *float64 `json:"radius_meters"`
		RodeDeployedM *float64 `json:"rode_deployed_m"`
		SeaState      *string  `json:"sea_state"`
		SeabedType    *string  `json:"seabed_type"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if body.RadiusMeters == nil && body.RodeDeployedM == nil && body.SeaState == nil && body.SeabedType == nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "no patch fields provided"})
	}
	if body.RadiusMeters != nil && *body.RadiusMeters <= 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "radius_meters must be positive"})
	}
	if body.RodeDeployedM != nil && *body.RodeDeployedM < 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "rode_deployed_m must be non-negative"})
	}
	if body.SeaState != nil {
		switch *body.SeaState {
		case "calm", "choppy", "rough", "storm":
		default:
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid sea_state"})
		}
	}
	if body.SeabedType != nil {
		switch *body.SeabedType {
		case "sand", "mud", "rock", "grass":
		default:
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid seabed_type"})
		}
	}

	updated := &anchorWatchData{
		Lat:           current.Lat,
		Lon:           current.Lon,
		RadiusMeters:  current.RadiusMeters,
		RodeDeployedM: current.RodeDeployedM,
		SeaState:      current.SeaState,
		SeabedType:    current.SeabedType,
		SetAt:         current.SetAt,
	}

	if body.RadiusMeters != nil {
		updated.RadiusMeters = *body.RadiusMeters
	}
	if body.RodeDeployedM != nil {
		updated.RodeDeployedM = *body.RodeDeployedM
	}
	if body.SeaState != nil {
		updated.SeaState = *body.SeaState
	}
	if body.SeabedType != nil {
		updated.SeabedType = *body.SeabedType
	}

	if err := saveAnchorWatch(updated); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to persist"})
	}

	anchorWatchMu.Lock()
	anchorWatchState = updated
	if updated.RadiusMeters > 0 {
		lastAnchorWatchRadiusMeters = updated.RadiusMeters
	}
	anchorWatchMu.Unlock()

	return c.JSON(http.StatusOK, map[string]any{
		"active":          true,
		"lat":             updated.Lat,
		"lon":             updated.Lon,
		"radius_meters":   updated.RadiusMeters,
		"rode_deployed_m": updated.RodeDeployedM,
		"sea_state":       updated.SeaState,
		"seabed_type":     updated.SeabedType,
		"set_at":          updated.SetAt.Format(time.RFC3339),
	})
}

// DELETE /api/anchor-watch
func deleteAnchorWatch(c echo.Context) error {
	anchorWatchMu.Lock()
	anchorWatchState = nil
	anchorWatchMu.Unlock()

	trailMu.Lock()
	selfTrail = nil
	trailMu.Unlock()

	_ = os.Remove(anchorWatchFilePath())

	return c.JSON(http.StatusOK, map[string]any{"active": false})
}

// GET /api/anchor-watch/trails/self?since=<RFC3339-timestamp>
// Returns post-anchor ring-buffer positions since the given timestamp.
func getSelfTrailHandler(c echo.Context) error {
	sinceStr := c.QueryParam("since")
	var since time.Time
	if sinceStr != "" {
		var err error
		since, err = time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid since timestamp"})
		}
	}

	points := getSelfTrailSince(since)
	return c.JSON(http.StatusOK, map[string]any{
		"points": points,
	})
}

// GET /api/anchor-watch/trails/ais/:id?since=<RFC3339-timestamp>
func getAISTrailHandler(c echo.Context) error {
	id := c.Param("id")
	trails := fetchSignalKAISTrails(getEnv("SETTINGS_FILE", "../settings.yaml"))
	points := trails[strings.ToUpper(strings.TrimSpace(id))]
	return c.JSON(http.StatusOK, map[string]any{
		"points": points,
	})
}

// GET /api/anchor-watch/trails/ais?since=<RFC3339-timestamp>
// Returns all AIS trails with new points since the given timestamp.
func getAllAISTrailsHandler(c echo.Context) error {
	trails := fetchSignalKAISTrails(getEnv("SETTINGS_FILE", "../settings.yaml"))
	return c.JSON(http.StatusOK, map[string]any{
		"trails": trails,
	})
}
