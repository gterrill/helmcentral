package main

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// Per-path telemetry history for trend gauges (ADR 0051).
//
// History comes from InfluxDB rather than a store built here. The
// signalk-to-influxdb-v2 plugin already writes the SignalK path as the Influx
// measurement name, so a per-path query is the depth query with its hardcoded
// measurement threaded through as a parameter.
//
// When InfluxDB is not configured this refuses with 503 rather than answering
// 200 with an empty series. AGENTS.md's fallback policy is explicit that a
// missing upstream source is surfaced, not masked, and a trend gauge drawing a
// flat line because no database exists is indistinguishable from a sensor
// reading a steady value — the dangerous kind of wrong.

// telemetryHistoryWindows is an allowlist because window is interpolated into
// a Flux query. A parse would accept "1h) |> yield(" as far as the duration
// grammar is concerned.
var telemetryHistoryWindows = []string{"1h", "3h", "6h", "24h", "7d"}

const defaultTelemetryHistoryWindow = "3h"

func validTelemetryHistoryWindow(window string) bool {
	for _, allowed := range telemetryHistoryWindows {
		if window == allowed {
			return true
		}
	}
	return false
}

type telemetryHistoryPoint struct {
	Time  string  `json:"time"`
	Value float64 `json:"value"`
}

// GET /api/telemetry/history?path=<dotted>&window=3h
func telemetryHistoryHandler(c echo.Context) error {
	path := strings.TrimSpace(c.QueryParam("path"))
	if path == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "path is required"})
	}
	if len(path) > gaugePathMaxLen {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "path is too long"})
	}
	// path is interpolated into a Flux string literal too (queryInfluxPathTrend
	// uses it as the _measurement filter, ADR 0051), and unlike window it can't
	// be an allowlist -- a SignalK path is open-ended. %q's escaping stops it
	// breaking the literal open into a new pipeline stage, but does nothing
	// about Flux's OWN "${...}" string-interpolation syntax, which InfluxDB
	// evaluates once it parses that literal (E-4). Rejected here at the HTTP
	// boundary and again in influx.go's fluxStringLiteral, which is also what
	// Mate's estimate_passage tool goes through with no HTTP handler in front
	// of it.
	if containsFluxInterpolationSyntax(path) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "path must not contain '$' or '{'"})
	}

	window := c.QueryParam("window")
	if window == "" {
		window = defaultTelemetryHistoryWindow
	}
	if !validTelemetryHistoryWindow(window) {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "window must be one of " + strings.Join(telemetryHistoryWindows, ", "),
		})
	}

	if !influxTelemetryConfigured() {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{
			"error": "history needs InfluxDB, which is not configured. Enable it in Settings.",
		})
	}

	points, err := queryInfluxPathTrend(path, window)
	if err != nil {
		// A configured-but-failing Influx is a real problem and says so,
		// rather than degrading to an empty chart.
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "InfluxDB query failed: " + err.Error()})
	}

	out := make([]telemetryHistoryPoint, 0, len(points))
	for _, p := range points {
		out = append(out, telemetryHistoryPoint{Time: p.Timestamp.UTC().Format("2006-01-02T15:04:05Z"), Value: p.Value})
	}

	return c.JSON(http.StatusOK, map[string]any{"path": path, "window": window, "points": out, "source": "influx"})
}
