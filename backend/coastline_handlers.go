package main

import (
	"net/http"

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
	})
}

// offlineCoastlineReaderFor is the production reader behind anchorage_shelter.
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

// coastlineRoutes returns the /api routes for the offline coastline. Status
// is readable by any signed-in role; changing it is a settings action.
// Each row carries its own auth tier (docs/adr/0177).
func coastlineRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodGet, "/api/offline-coastline", tierRead, getOfflineCoastlineHandler},
		{http.MethodPost, "/api/offline-coastline/update", tierAdmin, updateOfflineCoastlineHandler},
		{http.MethodDelete, "/api/offline-coastline/coverage", tierAdmin, clearOfflineCoastlineHandler},
	}
}
