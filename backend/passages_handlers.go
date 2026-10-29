package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/labstack/echo/v4"
)

// Read-only passage routes (ADR 0176). Editing arrives with the Passages page.

// passageDisplayToleranceMeters is how far the displayed track may stray from
// the logged one.
const passageDisplayToleranceMeters = 10.0

type passageSummary struct {
	passageRecord
	Stats passageStats `json:"stats"`
	// Timezone is the IANA zone at the first recorded point (ADR 0230): the
	// zone the page shows this passage's dates and times in. Null when the
	// passage has no zone, and TimezoneError then says why.
	Timezone      *string `json:"timezone"`
	TimezoneError string  `json:"timezone_error,omitempty"`
}

func startLon(pts []recordedTrackPoint) *float64 {
	if len(pts) == 0 {
		return nil
	}
	lon := pts[0].Lon
	return &lon
}

func startLat(pts []recordedTrackPoint) *float64 {
	if len(pts) == 0 {
		return nil
	}
	lat := pts[0].Lat
	return &lat
}

// startTimezone is the IANA zone at a passage's first point, or the reason
// there is none. It is per passage: a passage with no recorded position, or
// whose position has no zone, says so on its own row (and the reason is logged)
// and never stops the others. It is not read as UTC.
func (f passageFigures) startTimezone(id string) (tz *string, reason string) {
	if f.Timezone != "" {
		return &f.Timezone, ""
	}
	name, reason := f.lookupStartZone(id)
	if name == "" {
		return nil, reason
	}
	return &name, ""
}

// passageZoneAt finds a zone for a position. A variable so a test can count
// lookups or make one fail.
var passageZoneAt = zoneAt

// passageZoneFailures remembers positions whose zone could not be found, so a
// failing passage costs one lookup and one log line per process, not per
// request. The key is the position, so a passage moved by an edit is tried
// again.
var passageZoneFailures sync.Map // "lat,lon" -> reason

// lookupStartZone is the zone name at the start position, or the reason there
// is none.
func (f passageFigures) lookupStartZone(id string) (name, reason string) {
	if f.StartLat == nil || f.StartLon == nil {
		return "", "no track point is stored for this passage, so its time zone is not known"
	}
	key := fmt.Sprintf("%v,%v", *f.StartLat, *f.StartLon)
	if r, ok := passageZoneFailures.Load(key); ok {
		return "", r.(string)
	}
	z, err := passageZoneAt(*f.StartLat, *f.StartLon)
	if err != nil {
		reason := "the time zone for this passage's start position could not be found: " + err.Error()
		if _, loaded := passageZoneFailures.LoadOrStore(key, reason); !loaded {
			log.Printf("passages: %s: %v", id, err)
		}
		return "", reason
	}
	return z.Name, ""
}

type passageDetail struct {
	passageRecord
	Stats passageStats `json:"stats"`
	// Timezone and TimezoneError are as on passageSummary.
	Timezone      *string `json:"timezone"`
	TimezoneError string  `json:"timezone_error,omitempty"`
	// Track is the display track: one list of [lat, lon] per segment.
	Track [][][2]float64 `json:"track"`
	// TrackTimes is the time of each Track point, Unix seconds, same shape.
	TrackTimes [][]int64 `json:"track_times"`
	// ReplayTrack and ReplayTimes are a separate, time-thinned track for the
	// replay: about one point per 30 s, with every crossing of the at-rest
	// speed line kept so stops show. The display line above stays distance-simplified.
	ReplayTrack [][][2]float64 `json:"replay_track"`
	ReplayTimes [][]int64      `json:"replay_times"`
	// PlannedWithMate lists the Mate plans this passage was sailed from.
	// A nil list with PlannedWithMateError set means the plans could not be
	// read; the page shows that rather than "never planned".
	PlannedWithMate      []passagePlan `json:"planned_with_mate"`
	PlannedWithMateError string        `json:"planned_with_mate_error,omitempty"`
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
	out, err := store.ListPassageSummaries()
	if err != nil {
		log.Printf("passages: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "could not list passages"})
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
	figures, err := store.PassageFiguresFor(rec)
	if err != nil {
		log.Printf("passages: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "could not read passage"})
	}
	tz, tzReason := figures.startTimezone(rec.ID)
	// Plans that cannot be read must not hide the passage, and must not look
	// like "never planned": the error travels with the passage.
	var plans []passagePlan
	plansErr := ""
	if globalAssistantStore == nil {
		plansErr = "the assistant store is not running"
	} else if plans, err = globalAssistantStore.PassagePlans(rec.ID); err != nil {
		plansErr = "could not read the Mate plans: " + err.Error()
		plans = nil
	}
	if plansErr != "" {
		log.Printf("passages: planned_with_mate for %s: %s", rec.ID, plansErr)
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
	return c.JSON(http.StatusOK, map[string]any{"passage": passageDetail{passageRecord: rec, Stats: figures.Stats, Timezone: tz, TimezoneError: tzReason, Track: track, TrackTimes: times, ReplayTrack: replay, ReplayTimes: replayTimes, PlannedWithMate: plans, PlannedWithMateError: plansErr}})
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

// passageRoutes returns the /api routes for passages and cruises.
// Each row carries its own auth tier (docs/adr/0177).
func passageRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodGet, "/api/passages", tierRead, listPassagesHandler},
		{http.MethodGet, "/api/passages/inset-view", tierRead, getInsetViewHandler},
		{http.MethodPut, "/api/passages/inset-view", tierWrite, putInsetViewHandler},
		{http.MethodGet, "/api/passages/camera-shots", tierRead, listCameraShotsHandler},
		{http.MethodPost, "/api/passages/camera-shots", tierWrite, createCameraShotHandler},
		{http.MethodPatch, "/api/passages/camera-shots/:id", tierWrite, patchCameraShotHandler},
		{http.MethodDelete, "/api/passages/camera-shots/:id", tierWrite, deleteCameraShotHandler},
		{http.MethodGet, "/api/passages/signs", tierRead, listSignsHandler},
		{http.MethodPost, "/api/passages/signs", tierWrite, createSignHandler},
		{http.MethodPatch, "/api/passages/signs/:id", tierWrite, patchSignHandler},
		{http.MethodDelete, "/api/passages/signs/:id", tierWrite, deleteSignHandler},
		{http.MethodGet, "/api/passages/:id", tierRead, getPassageHandler},
		{http.MethodGet, "/api/cruises", tierRead, listCruisesHandler},
		{http.MethodGet, "/api/cruises/:id", tierRead, getCruiseHandler},
		{http.MethodPost, "/api/passages/import", tierWrite, importPassagesHandler},
		{http.MethodPost, "/api/passages/merge", tierWrite, mergePassagesHandler},
		{http.MethodPatch, "/api/passages/:id", tierWrite, patchPassageHandler},
		{http.MethodPost, "/api/passages/:id/split", tierWrite, splitPassageHandler},
		{http.MethodPost, "/api/passages/:id/trim", tierWrite, trimPassageHandler},
		{http.MethodPost, "/api/cruises", tierWrite, createCruiseHandler},
		{http.MethodPatch, "/api/cruises/:id", tierWrite, patchCruiseHandler},
		{http.MethodDelete, "/api/cruises/:id", tierWrite, deleteCruiseHandler},
		{http.MethodPut, "/api/cruises/:id/water-rises", tierWrite, putWaterRiseLabelHandler},
	}
}
