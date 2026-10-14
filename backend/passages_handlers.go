package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path"
	"strings"

	"github.com/labstack/echo/v4"
)

// Read-only passage routes (ADR 0173). Editing arrives with the Passages page.

// passageDisplayToleranceMeters is how far the displayed track may stray from
// the logged one.
const passageDisplayToleranceMeters = 10.0

type passageSummary struct {
	passageRecord
	Stats passageStats `json:"stats"`
	// StartLon is the longitude of the first recorded point, which the page
	// uses to show dates in the boat's own zone. Nil when no point was kept.
	StartLon *float64 `json:"start_lon"`
}

func startLon(pts []recordedTrackPoint) *float64 {
	if len(pts) == 0 {
		return nil
	}
	lon := pts[0].Lon
	return &lon
}

type passageDetail struct {
	passageRecord
	Stats    passageStats `json:"stats"`
	StartLon *float64     `json:"start_lon"`
	// Track is the display track: one list of [lat, lon] per segment.
	Track [][][2]float64 `json:"track"`
	// TrackTimes is the time of each Track point, Unix seconds, same shape.
	TrackTimes [][]int64 `json:"track_times"`
	// ReplayTrack and ReplayTimes are a separate, time-thinned track for the
	// replay: about one point per 30 s, with every crossing of the at-rest
	// speed line kept so stops show. The display line above stays distance-simplified.
	ReplayTrack [][][2]float64 `json:"replay_track"`
	ReplayTimes [][]int64      `json:"replay_times"`
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
		out = append(out, passageSummary{passageRecord: r, Stats: computePassageStats(pts), StartLon: startLon(pts)})
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
	track, times := simplifyTrackTimed(pts, passageDisplayToleranceMeters)
	if track == nil {
		track = [][][2]float64{}
		times = [][]int64{}
	}
	replay, replayTimes := replayTrack(pts, passageReplayInterval)
	if replay == nil {
		replay = [][][2]float64{}
		replayTimes = [][]int64{}
	}
	return c.JSON(http.StatusOK, map[string]any{"passage": passageDetail{passageRecord: rec, Stats: computePassageStats(pts), StartLon: startLon(pts), Track: track, TrackTimes: times, ReplayTrack: replay, ReplayTimes: replayTimes}})
}

// importPassagesHandler is POST /api/passages/import: several .gpx files in
// one multipart request. Every file is checked before anything is stored, so a
// bad file leaves the log as it was.
func importPassagesHandler(c echo.Context) error {
	store, ok := passageStoreOrError(c)
	if !ok {
		return nil
	}
	req := c.Request()
	req.Body = http.MaxBytesReader(c.Response(), req.Body, gpxMaxRequestBytes)
	mr, err := req.MultipartReader()
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "expected multipart/form-data with .gpx files"})
	}
	var files []gpxFile
	seg := 0
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "could not read upload: " + err.Error()})
		}
		if part.FileName() == "" {
			part.Close()
			continue
		}
		if len(files) >= gpxMaxFiles {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("at most %d files per import", gpxMaxFiles)})
		}
		var fixes []gpxFix
		fixes, seg, err = parseGPX(part, gpxMaxFileBytes, seg)
		if err != nil {
			status := http.StatusUnprocessableEntity
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				status = http.StatusRequestEntityTooLarge
			}
			return c.JSON(status, map[string]string{"error": filepathBase(part.FileName()) + ": " + err.Error()})
		}
		files = append(files, gpxFile{Name: filepathBase(part.FileName()), Fixes: fixes})
	}
	if len(files) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "no .gpx files in the request"})
	}
	sum, err := store.ImportGPX(files)
	if err != nil {
		log.Printf("passages: import: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "import failed"})
	}
	return c.JSON(http.StatusOK, sum)
}

// filepathBase keeps only the final element of an uploaded name, so a hostile
// filename never reaches a response or a log line as a path.
func filepathBase(name string) string {
	return path.Base(strings.ReplaceAll(name, "\\", "/"))
}
