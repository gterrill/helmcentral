package main

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

type solarControllerData struct {
	ID            string
	Label         string
	CurrentW      float64
	TodayKWh      float64
	YesterdayKWh  float64
	Mode          string
	Error         string
	LastUpdateAge float64
	Contribution  float64
}

type solarStateData struct {
	Datetime      time.Time
	CurrentW      float64
	TodayKWh      float64
	YesterdayKWh  float64
	PeakTodayW    float64
	LastUpdateAge float64
	Controllers   []solarControllerData
	Trend24hTotal []solarTrendPoint
}

type solarTrendPoint struct {
	Time   time.Time `json:"time"`
	TotalW float64   `json:"total_w"`
}

// buildSolarStatePayload produces the /api response body. Split from the handler so
// the SSE stream emits the identical shape without the two drifting apart.
func buildSolarStatePayload() map[string]any {
	state := solarStateData{
		Datetime:      time.Now().UTC(),
		CurrentW:      -1,
		TodayKWh:      -1,
		YesterdayKWh:  -1,
		PeakTodayW:    -1,
		Controllers:   []solarControllerData{},
		Trend24hTotal: []solarTrendPoint{},
	}
	source := "backend-fallback"

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)

	if signalkURL != "" {
		solar, fetchErr := fetchSignalKSolarState()
		if fetchErr == nil {
			state = solar
			source = "signalk"
		}
	}

	state = applyInMemorySolarDefaults(state)
	if influxTelemetryConfigured() {
		state = applyInfluxSolarOverride(state)
	}
	if state.Trend24hTotal == nil {
		state.Trend24hTotal = []solarTrendPoint{}
	}

	controllers := make([]map[string]any, 0, len(state.Controllers))
	for _, controller := range state.Controllers {
		controllers = append(controllers, map[string]any{
			"id":                controller.ID,
			"label":             controller.Label,
			"current_w":         controller.CurrentW,
			"today_kwh":         controller.TodayKWh,
			"yesterday_kwh":     controller.YesterdayKWh,
			"mode":              controller.Mode,
			"error":             controller.Error,
			"last_update_age_s": controller.LastUpdateAge,
			"contribution_pct":  controller.Contribution,
		})
	}

	return map[string]any{
		"datetime":          state.Datetime.Format(time.RFC3339),
		"source":            source,
		"current_w":         state.CurrentW,
		"today_kwh":         state.TodayKWh,
		"yesterday_kwh":     state.YesterdayKWh,
		"peak_today_w":      state.PeakTodayW,
		"last_update_age_s": state.LastUpdateAge,
		"controllers":       controllers,
		"trend_24h_total":   state.Trend24hTotal,
	}
}

func solarState(c echo.Context) error {
	return c.JSON(http.StatusOK, buildSolarStatePayload())
}

// applyInMemorySolarDefaults fills any field SignalK didn't report, from the
// in-memory accumulator/buffer fed by sampleTracks. Always safe to call —
// sentinels pass through unchanged if nothing has been recorded yet.
func applyInMemorySolarDefaults(state solarStateData) solarStateData {
	if state.TodayKWh < 0 {
		state.TodayKWh = inMemorySolarTodayKWh()
	}
	if state.YesterdayKWh < 0 {
		state.YesterdayKWh = inMemorySolarYesterdayKWh()
	}
	if state.PeakTodayW < 0 {
		state.PeakTodayW = inMemorySolarPeakTodayW()
	}
	if len(state.Trend24hTotal) == 0 {
		state.Trend24hTotal = inMemorySolarTrend24h()
	}
	return state
}

// applyInfluxSolarOverride wholesale-replaces the four Influx-backed fields,
// read from the background ticker's cache (telemetry_influx_cache.go)
// rather than queried live — called only when influxTelemetryConfigured(),
// and does not fall back to the in-memory value if the cached result is
// stale or the query behind it failed (same Fallback Policy reasoning as
// ADR-0020: once Influx is enabled, its failures should be visible, not
// silently patched over).
func applyInfluxSolarOverride(state solarStateData) solarStateData {
	return cachedInfluxSolarOverride(state, time.Now().UTC())
}

// solarRoutes returns the /api routes for the solar state payload.
// Each row carries its own auth tier (docs/adr/0177).
func solarRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodGet, "/api/solar-state", tierRead, solarState},
	}
}
