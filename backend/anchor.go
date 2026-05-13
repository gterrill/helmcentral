package main

import (
	"encoding/json"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

const defaultAnchorWatchRadiusMeters = 45.72 // 150 ft

type anchorWatchData struct {
	Lat           float64   `json:"lat"`
	Lon           float64   `json:"lon"`
	RadiusMeters  float64   `json:"radius_meters"`
	RodeDeployedM float64   `json:"rode_deployed_m"`
	SeaState      string    `json:"sea_state"`
	SeabedType    string    `json:"seabed_type"`
	SetAt         time.Time `json:"set_at"`
}

var (
	anchorWatchMu    sync.RWMutex
	anchorWatchState *anchorWatchData
)

func anchorWatchFilePath() string {
	return cacheFilePath("ANCHOR_WATCH_FILE", "cache/anchor_watch.json")
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
	if body.RadiusMeters != nil && *body.RadiusMeters > 0 {
		radius = *body.RadiusMeters
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
	anchorWatchMu.Unlock()

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

	_ = os.Remove(anchorWatchFilePath())

	return c.JSON(http.StatusOK, map[string]any{"active": false})
}
