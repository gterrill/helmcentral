package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// maxPlacemarkLabelLen bounds what a client can store per pin. The label is
// a short hand-typed hazard note ("bombie", "shore"), not free-form text.
const maxPlacemarkLabelLen = 40

// maxPlacemarks bounds the whole set, so a stuck client tapping the map
// can't grow the state file without limit.
const maxPlacemarks = 50

// placemark is a user-dropped reference point on the anchor-watch map —
// typically a bombie or the nearest shoreline, so the crew can watch how
// close the swing is taking them to it.
//
// Only the position is stored. Range and bearing are deliberately NOT
// persisted: they change every time the boat swings, so each client
// recomputes them from its own current vessel fix on render. Storing them
// would freeze the number at drop time, which is the opposite of what the
// pin is for.
type placemark struct {
	ID        string    `json:"id"`
	Lat       float64   `json:"lat"`
	Lon       float64   `json:"lon"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"created_at"`
}

var (
	placemarkMu sync.RWMutex
	placemarks  []*placemark
)

func anchorPlacemarksFilePath() string {
	return cacheFilePath("ANCHOR_PLACEMARKS_FILE", "cache/anchor_placemarks.json")
}

func anchorWatchActive() bool {
	anchorWatchMu.RLock()
	defer anchorWatchMu.RUnlock()
	return anchorWatchState != nil
}

// savePlacemarksLocked persists the current set. Callers must hold
// placemarkMu for writing.
func savePlacemarksLocked() error {
	return writeJSONFileAtomic(anchorPlacemarksFilePath(), placemarks)
}

// loadAnchorPlacemarks restores pins on startup. Pins belong to one anchoring
// session, so a file found with no watch running is a leftover from a session
// that ended while the backend was down — it is discarded, not inherited.
// Call it after loadAnchorWatch.
func loadAnchorPlacemarks() {
	path := anchorPlacemarksFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	if !anchorWatchActive() {
		_ = os.Remove(path)
		return
	}

	var loaded []*placemark
	if err := json.Unmarshal(data, &loaded); err != nil {
		return
	}

	placemarkMu.Lock()
	placemarks = loaded
	placemarkMu.Unlock()
}

// clearPlacemarks drops every pin and its state file. Called when the anchor
// session ends.
func clearPlacemarks() {
	placemarkMu.Lock()
	placemarks = nil
	placemarkMu.Unlock()
	_ = os.Remove(anchorPlacemarksFilePath())
}

// GET /api/anchor-watch/placemarks
func listPlacemarksHandler(c echo.Context) error {
	placemarkMu.RLock()
	out := make([]*placemark, len(placemarks))
	copy(out, placemarks)
	placemarkMu.RUnlock()

	return c.JSON(http.StatusOK, map[string]any{"placemarks": out})
}

// POST /api/anchor-watch/placemarks
func createPlacemarkHandler(c echo.Context) error {
	var body struct {
		Lat   float64 `json:"lat"`
		Lon   float64 `json:"lon"`
		Label string  `json:"label"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if body.Lat < -90 || body.Lat > 90 || body.Lon < -180 || body.Lon > 180 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "lat/lon out of range"})
	}
	// A pin is bound to the running session. With no session there is
	// nothing to bind it to, so say so rather than storing an orphan the
	// next anchoring would silently inherit.
	if !anchorWatchActive() {
		return c.JSON(http.StatusConflict, map[string]string{"error": "no active anchor watch"})
	}

	label := strings.TrimSpace(body.Label)
	if len(label) > maxPlacemarkLabelLen {
		label = label[:maxPlacemarkLabelLen]
	}

	pm := &placemark{
		ID:        uuid.NewString(),
		Lat:       body.Lat,
		Lon:       body.Lon,
		Label:     label,
		CreatedAt: time.Now().UTC(),
	}

	placemarkMu.Lock()
	if len(placemarks) >= maxPlacemarks {
		placemarkMu.Unlock()
		return c.JSON(http.StatusConflict, map[string]string{"error": "placemark limit reached"})
	}
	placemarks = append(placemarks, pm)
	err := savePlacemarksLocked()
	placemarkMu.Unlock()

	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to persist"})
	}
	return c.JSON(http.StatusCreated, pm)
}

// DELETE /api/anchor-watch/placemarks/:id
func deletePlacemarkHandler(c echo.Context) error {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
	}

	placemarkMu.Lock()
	idx := -1
	for i, pm := range placemarks {
		if pm.ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		placemarkMu.Unlock()
		return c.JSON(http.StatusNotFound, map[string]string{"error": "placemark not found"})
	}
	placemarks = append(placemarks[:idx], placemarks[idx+1:]...)
	err := savePlacemarksLocked()
	placemarkMu.Unlock()

	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to persist"})
	}
	return c.JSON(http.StatusOK, map[string]any{"id": id, "removed": true})
}
