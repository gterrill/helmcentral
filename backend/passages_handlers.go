package main

import (
	"errors"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

// Read-only passage routes (ADR 0173). Editing arrives with the Passages page.

// passageDisplayToleranceMeters is how far the displayed track may stray from
// the logged one.
const passageDisplayToleranceMeters = 10.0

type passageSummary struct {
	passageRecord
	Stats passageStats `json:"stats"`
}

type passageDetail struct {
	passageRecord
	Stats passageStats `json:"stats"`
	// Track is the display track: one list of [lat, lon] per segment.
	Track [][][2]float64 `json:"track"`
}

func passageStoreOrError(c echo.Context) (*passageStore, bool) {
	if globalPassageStore == nil {
		_ = c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "passage log is not running"})
		return nil, false
	}
	return globalPassageStore, true
}

// listPassagesHandler is GET /api/passages: visible passages, newest first.
func listPassagesHandler(c echo.Context) error {
	store, ok := passageStoreOrError(c)
	if !ok {
		return nil
	}
	records, err := store.ListPassages()
	if err != nil {
		log.Printf("passages: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "could not list passages"})
	}
	out := make([]passageSummary, 0, len(records))
	for _, r := range records {
		pts, err := store.TrackPointsBetween(r.Start, r.End)
		if err != nil {
			log.Printf("passages: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "could not read passage track"})
		}
		out = append(out, passageSummary{passageRecord: r, Stats: computePassageStats(pts)})
	}
	return c.JSON(http.StatusOK, map[string]any{"passages": out})
}

// getPassageHandler is GET /api/passages/:id.
func getPassageHandler(c echo.Context) error {
	store, ok := passageStoreOrError(c)
	if !ok {
		return nil
	}
	rec, err := store.GetPassage(c.Param("id"))
	if errors.Is(err, errPassageNotFound) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "passage not found"})
	}
	if err != nil {
		log.Printf("passages: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "could not read passage"})
	}
	pts, err := store.TrackPointsBetween(rec.Start, rec.End)
	if err != nil {
		log.Printf("passages: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "could not read passage track"})
	}
	track := simplifyTrack(pts, passageDisplayToleranceMeters)
	if track == nil {
		track = [][][2]float64{}
	}
	return c.JSON(http.StatusOK, map[string]any{"passage": passageDetail{passageRecord: rec, Stats: computePassageStats(pts), Track: track}})
}
