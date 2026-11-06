package main

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

// The offline coastline's own state: the squares in helmcentral.sqlite and
// the worker that keeps them current (ADR 0203). Nil until main opens them.
var (
	globalCoastlineStore   *coastlineStore
	globalCoastlineManager *coastlineManager
)

// offlineCoastlineEnabledFromSettingsFile reads offline_coastline.enabled
// fresh on every call, so a change on the Settings page applies to the next
// step of the worker and the next question to Mate.
func offlineCoastlineEnabledFromSettingsFile(settingsPath string) func() (bool, error) {
	return func() (bool, error) {
		settings, err := readSettings(settingsPath)
		if err != nil {
			return false, err
		}
		return buildSettingsPayload(settings).OfflineCoastline.Enabled, nil
	}
}

// ownShipFix is the boat's position when it can be trusted (see
// usableOwnShipFix).
func ownShipFix() (float64, float64, bool) {
	state, err := fetchSignalKVesselState()
	return usableOwnShipFix(state, err)
}

// initOfflineCoastline builds the manager over store. It is called before the
// server starts so handlers never see a half-built global; the caller then
// runs globalCoastlineManager.Run.
func initOfflineCoastline(store *coastlineStore, settingsPath string) {
	globalCoastlineStore = store
	globalCoastlineManager = newCoastlineManager(coastlineManagerConfig{
		Store:    store,
		Enabled:  offlineCoastlineEnabledFromSettingsFile(settingsPath),
		Position: ownShipFix,
		Source:   resolveCoastlineSource,
		Zone: func() *time.Location {
			lat, lon, ok := ownShipFix()
			return boatTimeAt(lat, lon, ok).Loc
		},
	})
}

// offlineCoastlineReaderFor is the production reader behind anchorage_details.
func offlineCoastlineReaderFor(settingsPath string) func() *offlineCoastlineReader {
	enabled := offlineCoastlineEnabledFromSettingsFile(settingsPath)
	return func() *offlineCoastlineReader {
		if globalCoastlineStore == nil {
			return nil
		}
		return &offlineCoastlineReader{Enabled: enabled, Store: globalCoastlineStore, Boat: ownShipFix}
	}
}

func coastlineUnavailable(c echo.Context) error {
	return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "Offline coastline is not running on this install"})
}

func getOfflineCoastlineHandler(c echo.Context) error {
	if globalCoastlineManager == nil {
		return coastlineUnavailable(c)
	}
	st, err := globalCoastlineManager.Status()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "could not read Offline coastline: " + err.Error()})
	}
	return c.JSON(http.StatusOK, st)
}

func updateOfflineCoastlineHandler(c echo.Context) error {
	if globalCoastlineManager == nil {
		return coastlineUnavailable(c)
	}
	n, err := globalCoastlineManager.Update()
	if err != nil {
		return c.JSON(http.StatusConflict, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusAccepted, map[string]int{"updating": n})
}

func clearOfflineCoastlineHandler(c echo.Context) error {
	if globalCoastlineManager == nil {
		return coastlineUnavailable(c)
	}
	if err := globalCoastlineManager.Clear(); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "could not clear coverage: " + err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

// coastlineSquaresBody is the body of a request or remove call: squares named
// "lat,lon" by their south-west corner, in the order selected.
type coastlineSquaresBody struct {
	Squares []string `json:"squares"`
	// DryRun asks what a request would do (counts only) and stores nothing.
	DryRun bool `json:"dry_run,omitempty"`
}

// coastlineMaxBodyBytes bounds a request body: coastlineMaxRequestInput ids of
// a few bytes each, with room to spare.
const coastlineMaxBodyBytes = 64 << 10

// badCoastlineBody is a 400 whose body is {"error": msg}, the shape the page
// reads.
func badCoastlineBody(msg string) *echo.HTTPError {
	return echo.NewHTTPError(http.StatusBadRequest, map[string]string{"error": msg})
}

// bindCoastlineSquares reads the body. A body it cannot use comes back as a
// 400 for the handler to return.
func bindCoastlineSquares(c echo.Context) (coastlineSquaresBody, []coverageSquare, error) {
	var body coastlineSquaresBody
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, coastlineMaxBodyBytes)
	if err := c.Bind(&body); err != nil {
		return body, nil, badCoastlineBody("body must be {\"squares\": [\"lat,lon\", ...]}")
	}
	if len(body.Squares) > coastlineMaxRequestInput {
		return body, nil, badCoastlineBody(fmt.Sprintf("%d squares are selected, and one call can name at most %d. Select fewer.", len(body.Squares), coastlineMaxRequestInput))
	}
	out := make([]coverageSquare, 0, len(body.Squares))
	for _, id := range body.Squares {
		sq, err := parseCoverageSquareID(id)
		if err != nil {
			return body, nil, badCoastlineBody(err.Error())
		}
		out = append(out, sq)
	}
	return body, out, nil
}

// coastlineFailure maps a manager error to its status: what the operator named
// is 400, the switch being off is 409, anything else is the server's.
func coastlineFailure(c echo.Context, err error) error {
	switch {
	case errors.Is(err, errCoastlineBadRequest):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, errCoastlineOff):
		return c.JSON(http.StatusConflict, map[string]string{"error": err.Error()})
	default:
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}

func requestOfflineCoastlineHandler(c echo.Context) error {
	if globalCoastlineManager == nil {
		return coastlineUnavailable(c)
	}
	body, squares, err := bindCoastlineSquares(c)
	if err != nil {
		return err
	}
	res, err := globalCoastlineManager.Request(squares, body.DryRun)
	if err != nil {
		return coastlineFailure(c, err)
	}
	return c.JSON(http.StatusAccepted, res)
}

func removeOfflineCoastlineSquaresHandler(c echo.Context) error {
	if globalCoastlineManager == nil {
		return coastlineUnavailable(c)
	}
	_, squares, err := bindCoastlineSquares(c)
	if err != nil {
		return err
	}
	n, err := globalCoastlineManager.Remove(squares)
	if err != nil {
		return coastlineFailure(c, err)
	}
	return c.JSON(http.StatusOK, map[string]int{"removed": n})
}

// coastlineRoutes returns the /api routes for the offline coastline. Status
// is readable by any signed-in role; changing it is a settings action.
// Each row carries its own auth tier (docs/adr/0177).
func coastlineRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodGet, "/api/offline-coastline", tierRead, getOfflineCoastlineHandler},
		{http.MethodPost, "/api/offline-coastline/update", tierAdmin, updateOfflineCoastlineHandler},
		{http.MethodPost, "/api/offline-coastline/request", tierAdmin, requestOfflineCoastlineHandler},
		{http.MethodPost, "/api/offline-coastline/remove", tierAdmin, removeOfflineCoastlineSquaresHandler},
		{http.MethodDelete, "/api/offline-coastline/coverage", tierAdmin, clearOfflineCoastlineHandler},
	}
}
