package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
)

// The REST side of watches (ADR 0227). The Watches page makes, changes, runs
// and deletes watches directly; the validation is the record type's own.

func watchRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodGet, "/api/watches", tierRead, listWatchesHandler},
		{http.MethodPost, "/api/watches", tierWrite, createWatchHandler},
		{http.MethodPost, "/api/watches/schedule-preview", tierRead, watchSchedulePreviewHandler},
		{http.MethodGet, "/api/watches/channels", tierRead, watchChannelsHandler},
		{http.MethodGet, "/api/watches/:id", tierRead, getWatchHandler},
		{http.MethodPatch, "/api/watches/:id", tierWrite, patchWatchHandler},
		{http.MethodDelete, "/api/watches/:id", tierWrite, deleteWatchHandler},
		{http.MethodPost, "/api/watches/:id/run", tierWrite, runWatchHandler},
		{http.MethodGet, "/api/watches/:id/runs/:runId", tierRead, getWatchRunHandler},
		{http.MethodDelete, "/api/watches/:id/runs/:runId", tierWrite, deleteWatchRunHandler},
	}
}

// watchListEntry is a watch as the index shows it.
type watchListEntry struct {
	savedWatch
	ScheduleSentence string           `json:"schedule_sentence"`
	LastRun          *watchRunSummary `json:"last_run"`
	// HasFinding says the latest run found something.
	HasFinding bool `json:"has_finding"`
	// Running is true for a one-off watch still sampling.
	Running bool `json:"running,omitempty"`
	// StartedBoatTime and EndsBoatTime are a one-off watch's start and end on
	// the boat's clock as it was when the watch began, or the UTC time said to
	// be UTC when the boat's zone was not known then.
	StartedBoatTime string `json:"started_boat_time,omitempty"`
	EndsBoatTime    string `json:"ends_boat_time,omitempty"`
}

type watchListResponse struct {
	Watches      []watchListEntry `json:"watches"`
	Max          int              `json:"max"`
	Cost24h      watchCost        `json:"cost_24h"`
	StorageBytes int64            `json:"storage_bytes"`
	// Problem says in full why watches are not running, "" when nothing is wrong.
	Problem string `json:"problem,omitempty"`
}

func watchStore() (*documentStore, error) {
	if globalDocumentStore == nil {
		return nil, errWatchesNoStore
	}
	return globalDocumentStore, nil
}

func watchErrorStatus(err error) int {
	var fe *fieldedError
	switch {
	case errors.Is(err, errWatchNotFound), errors.Is(err, errWatchRunNotFound):
		return http.StatusNotFound
	case errors.As(err, &fe):
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func watchError(c echo.Context, err error) error {
	return c.JSON(watchErrorStatus(err), map[string]string{"error": err.Error()})
}

// watchOneOffLocation is the boat's clock a one-off watch began on, nil when
// its zone was not known then.
func watchOneOffLocation(w savedWatch) *time.Location {
	var cfg watchOneOffConfig
	if len(w.Config) == 0 || json.Unmarshal(w.Config, &cfg) != nil || cfg.ZoneName == "" || cfg.ZoneName == "UTC" {
		return nil
	}
	return time.FixedZone(cfg.ZoneName, cfg.OffsetSeconds)
}

func watchScheduleLabel(w savedWatch) string {
	if w.Kind == watchKindOneOff {
		return "One-off · " + strconv.Itoa(w.DurationMinutes) + " " + watchPlural(w.DurationMinutes, "minute", "minutes")
	}
	return watchSentenceOrCron(w)
}

func watchEntryFor(q sqlQueryer, w savedWatch, now time.Time) (watchListEntry, error) {
	last, err := lastRunSummaryQ(q, w.ID)
	if err != nil {
		return watchListEntry{}, err
	}
	e := watchListEntry{savedWatch: w, ScheduleSentence: watchScheduleLabel(w), LastRun: last}
	e.HasFinding = last != nil && last.FindingCount > 0
	e.Running = w.Kind == watchKindOneOff && w.Enabled && w.EndsAt != nil && w.EndsAt.After(now)
	if w.Kind == watchKindOneOff {
		loc := watchOneOffLocation(w)
		if w.StartedAt != nil {
			e.StartedBoatTime = watchBoatClock(*w.StartedAt, loc)
		}
		if w.EndsAt != nil {
			e.EndsBoatTime = watchBoatClock(*w.EndsAt, loc)
		}
	}
	return e, nil
}

// GET /api/watches
func listWatchesHandler(c echo.Context) error {
	docs, err := watchStore()
	if err != nil {
		return watchError(c, err)
	}
	resp := watchListResponse{Watches: []watchListEntry{}, Max: maxScheduledWatches}
	now := time.Now()
	err = docs.Read(func(q sqlQueryer) error {
		list, err := listSavedWatchesQ(q)
		if err != nil {
			return err
		}
		for _, w := range list {
			e, err := watchEntryFor(q, w, now)
			if err != nil {
				return err
			}
			resp.Watches = append(resp.Watches, e)
		}
		if resp.Cost24h, err = watchCostSinceQ(q, now.Add(-24*time.Hour)); err != nil {
			return err
		}
		resp.StorageBytes, err = watchStorageBytesQ(q)
		return err
	})
	if err != nil {
		return watchError(c, err)
	}
	if host := activeWatchHost; host != nil {
		resp.Problem = host.stopReason()
	}
	return c.JSON(http.StatusOK, resp)
}

// GET /api/watches/:id?limit=&before= pages the runs, newest first. before is
// started_at,id of the last run already shown.
func getWatchHandler(c echo.Context) error {
	docs, err := watchStore()
	if err != nil {
		return watchError(c, err)
	}
	limit := 20
	if v := c.QueryParam("limit"); v != "" {
		if limit, err = strconv.Atoi(v); err != nil || limit < 1 || limit > 100 {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "limit must be a whole number from 1 to 100"})
		}
	}
	if _, has := c.QueryParams()["offset"]; has {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "pass before (started_at,id of the last run shown) instead of offset"})
	}
	var before *watchRunCursor
	if v := c.QueryParam("before"); v != "" {
		if before, err = parseWatchRunCursor(v); err != nil {
			return watchError(c, err)
		}
	}
	var resp struct {
		Watch     watchListEntry `json:"watch"`
		Runs      []watchRun     `json:"runs"`
		TotalRuns int            `json:"total_runs"`
		// HasOlder says there are runs older than the last one returned.
		HasOlder bool `json:"has_older"`
	}
	err = docs.Read(func(q sqlQueryer) error {
		w, err := savedWatchByIDQ(q, c.Param("id"))
		if err != nil {
			return err
		}
		if resp.Watch, err = watchEntryFor(q, w, time.Now()); err != nil {
			return err
		}
		if resp.Runs, err = watchRunsQ(q, w.ID, limit+1, before); err != nil {
			return err
		}
		if len(resp.Runs) > limit {
			resp.Runs, resp.HasOlder = resp.Runs[:limit], true
		}
		return q.QueryRow(`SELECT COUNT(*) FROM watch_runs WHERE watch_id = ?`, w.ID).Scan(&resp.TotalRuns)
	})
	if err != nil {
		return watchError(c, err)
	}
	if err := attachWatchNotifyPending(resp.Watch.ID, resp.Runs); err != nil {
		// The runs are still right; only the "not delivered yet" lines are missing.
		log.Printf("watches: could not read the notifications still being retried for %q: %v", resp.Watch.Name, err)
	}
	return c.JSON(http.StatusOK, resp)
}

// savedWatchRequest is the body of a create or change. Absent fields are left as
// they are on a change and take their defaults on a create.
type savedWatchRequest struct {
	Name       *string                 `json:"name"`
	Schedule   *string                 `json:"schedule"`
	Sections   *[]string               `json:"sections"`
	LookOutFor *string                 `json:"look_out_for"`
	Enabled    *bool                   `json:"enabled"`
	Thresholds *watchThresholdsRequest `json:"thresholds"`
	Notify     *[]string               `json:"notify"`
	Send       *string                 `json:"send"`
}

// watchThresholdsRequest is the thresholds of a create or change. A field not
// sent keeps its stored value (its default on a create), the same as on the
// record type. A zero is sent as zero and refused by the validator.
type watchThresholdsRequest struct {
	AlarmsRaised     *int     `json:"alarms_raised"`
	InstrumentIssues *int     `json:"instrument_issues"`
	TankChangePoints *float64 `json:"tank_change_points"`
	PressureFallHPa  *float64 `json:"pressure_fall_hpa"`
}

func (r watchThresholdsRequest) applyTo(t watchThresholds) watchThresholds {
	if r.AlarmsRaised != nil {
		t.AlarmsRaised = *r.AlarmsRaised
	}
	if r.InstrumentIssues != nil {
		t.InstrumentIssues = *r.InstrumentIssues
	}
	if r.TankChangePoints != nil {
		t.TankChangePoints = *r.TankChangePoints
	}
	if r.PressureFallHPa != nil {
		t.PressureFallHPa = *r.PressureFallHPa
	}
	return t
}

func decodeSavedWatchRequest(c echo.Context) (savedWatchRequest, error) {
	var req savedWatchRequest
	dec := json.NewDecoder(c.Request().Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return req, fieldErr("body", "send name, schedule, sections, look_out_for, enabled, thresholds, notify and send as JSON: %s", err.Error())
	}
	return req, nil
}

func (req savedWatchRequest) applyTo(w savedWatch) savedWatch {
	if req.Name != nil {
		w.Name = *req.Name
	}
	if req.Schedule != nil {
		w.Schedule = *req.Schedule
	}
	if req.Sections != nil {
		w.Sections = *req.Sections
	}
	if req.LookOutFor != nil {
		w.LookOutFor = *req.LookOutFor
	}
	if req.Enabled != nil {
		w.Enabled = *req.Enabled
	}
	if req.Thresholds != nil {
		w.Thresholds = req.Thresholds.applyTo(w.Thresholds)
	}
	if req.Notify != nil {
		w.Notify = *req.Notify
	}
	if req.Send != nil {
		w.Send = *req.Send
	}
	return w
}

// POST /api/watches
func createWatchHandler(c echo.Context) error {
	docs, err := watchStore()
	if err != nil {
		return watchError(c, err)
	}
	req, err := decodeSavedWatchRequest(c)
	if err != nil {
		return watchError(c, err)
	}
	var created savedWatch
	err = docs.RunTx("create watch", func(tx *sql.Tx, now time.Time) error {
		var err error
		created, err = createSavedWatchTx(tx, req.applyTo(newSavedWatchDefaults()), now)
		return err
	})
	if err != nil {
		return watchError(c, err)
	}
	return c.JSON(http.StatusCreated, created)
}

// PATCH /api/watches/:id
func patchWatchHandler(c echo.Context) error {
	docs, err := watchStore()
	if err != nil {
		return watchError(c, err)
	}
	req, err := decodeSavedWatchRequest(c)
	if err != nil {
		return watchError(c, err)
	}
	var updated savedWatch
	err = docs.RunTx("change watch", func(tx *sql.Tx, now time.Time) error {
		before, err := savedWatchByIDQ(tx, c.Param("id"))
		if err != nil {
			return err
		}
		updated, err = updateSavedWatchFromTx(tx, before, req.applyTo(before), now)
		return err
	})
	if err != nil {
		return watchError(c, err)
	}
	return c.JSON(http.StatusOK, updated)
}

// DELETE /api/watches/:id removes the watch and its runs.
func deleteWatchHandler(c echo.Context) error {
	docs, err := watchStore()
	if err != nil {
		return watchError(c, err)
	}
	// A one-off watch still sampling is stopped first, so it does not go on
	// reading, saving samples to a watch that no longer exists, or posting a
	// report to the chat for something the operator removed.
	if err := stopRunningOneOffWatch(docs, c.Param("id")); err != nil {
		return watchError(c, err)
	}
	if err := docs.RunTx("delete watch", func(tx *sql.Tx, _ time.Time) error {
		return deleteSavedWatchTx(tx, c.Param("id"))
	}); err != nil {
		return watchError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// stopRunningOneOffWatch stops the sampling of a one-off watch that is being
// deleted. A watch that is already handing its report to Mate cannot be
// stopped; its report is on its way and the delete goes ahead.
func stopRunningOneOffWatch(docs *documentStore, id string) error {
	var w savedWatch
	if err := docs.Read(func(q sqlQueryer) error {
		var err error
		w, err = savedWatchByIDQ(q, id)
		return err
	}); err != nil {
		return err
	}
	if w.Kind != watchKindOneOff || w.ConversationID == "" {
		return nil
	}
	reg := globalAssistantWatches
	if info, ok := reg.get(w.ConversationID); !ok || info.ID != w.ID {
		return nil
	}
	if _, err := reg.stop(w.ConversationID); err != nil && !errors.Is(err, errAssistantWatchReporting) {
		return err
	}
	return nil
}

// DELETE /api/watches/:id/runs/:runId
func deleteWatchRunHandler(c echo.Context) error {
	docs, err := watchStore()
	if err != nil {
		return watchError(c, err)
	}
	if err := docs.RunTx("delete watch run", func(tx *sql.Tx, _ time.Time) error {
		return deleteWatchRunTx(tx, c.Param("id"), c.Param("runId"))
	}); err != nil {
		return watchError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// POST /api/watches/:id/run makes a run now and answers with it. It waits for
// Mate's review, which can take a minute.
func runWatchHandler(c echo.Context) error {
	docs, err := watchStore()
	if err != nil {
		return watchError(c, err)
	}
	host := activeWatchHost
	if host == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "the watch scheduler is not running"})
	}
	var w savedWatch
	if err := docs.Read(func(q sqlQueryer) error {
		var err error
		w, err = savedWatchByIDQ(q, c.Param("id"))
		return err
	}); err != nil {
		return watchError(c, err)
	}
	run, err := host.RunNow(c.Request().Context(), w)
	if err != nil {
		return c.JSON(http.StatusConflict, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, run)
}

type watchPreviewTime struct {
	At    string `json:"at"`
	Local string `json:"local"`
}

type watchPreviewResponse struct {
	Valid    bool   `json:"valid"`
	Error    string `json:"error,omitempty"`
	Sentence string `json:"sentence,omitempty"`
	// BoatTimeKnown is false without a boat position. The sentence still
	// stands; the run times cannot be given, and are never given in UTC.
	BoatTimeKnown bool               `json:"boat_time_known"`
	NextProblem   string             `json:"next_problem,omitempty"`
	Next          []watchPreviewTime `json:"next"`
}

// POST /api/watches/schedule-preview {schedule}
func watchSchedulePreviewHandler(c echo.Context) error {
	var body struct {
		Schedule string `json:"schedule"`
	}
	dec := json.NewDecoder(c.Request().Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "send {\"schedule\": \"0 6 * * *\"}"})
	}
	resp := watchPreviewResponse{Next: []watchPreviewTime{}}
	if err := validateWatchSchedule(body.Schedule); err != nil {
		resp.Error = err.Error()
		return c.JSON(http.StatusOK, resp)
	}
	resp.Valid = true
	resp.Sentence, _ = watchScheduleSentence(body.Schedule)
	lon, err := watchBoatLongitude()
	if err != nil {
		resp.NextProblem = watchNoPositionReason
		return c.JSON(http.StatusOK, resp)
	}
	resp.BoatTimeKnown = true
	loc := vesselLocalLocation(lon)
	next, err := watchNextRuns(body.Schedule, time.Now(), loc, 3)
	if err != nil {
		resp.NextProblem = err.Error()
		return c.JSON(http.StatusOK, resp)
	}
	for _, t := range next {
		resp.Next = append(resp.Next, watchPreviewTime{At: t.UTC().Format(time.RFC3339), Local: t.In(loc).Format("Mon 2 Jan 15:04 MST")})
	}
	return c.JSON(http.StatusOK, resp)
}

// GET /api/watches/channels says which notification channels a watch can use
// and which of them are switched on. It is a read-tier route so anyone who can
// open the page can pick channels, and it says nothing about their settings.
func watchChannelsHandler(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string][]string{
		"offered": watchNotifyChannels,
		"enabled": defaultWatchNotify(getAlarmTransports()),
	})
}

// GET /api/watches/:id/runs/:runId is one run, as a link to it opens it.
func getWatchRunHandler(c echo.Context) error {
	docs, err := watchStore()
	if err != nil {
		return watchError(c, err)
	}
	var run watchRun
	if err := docs.Read(func(q sqlQueryer) error {
		var err error
		run, err = watchRunByIDQ(q, c.Param("id"), c.Param("runId"))
		return err
	}); err != nil {
		return watchError(c, err)
	}
	runs := []watchRun{run}
	if err := attachWatchNotifyPending(run.WatchID, runs); err != nil {
		log.Printf("watches: could not read the notifications still being retried for run %s: %v", run.ID, err)
	}
	return c.JSON(http.StatusOK, runs[0])
}
