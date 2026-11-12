// Package main: pluggable lightning-probability provider host system (ADR 0284).
//
// Lightning is its own provider category for the reason upper air is (ADR
// 0071): the surface weather providers do not answer it, so folding it into
// fetch_forecast would tie it to whichever weather source the operator picked.
//
// Guest contract (fetch_lightning), mirrored by wasmFetchLightningOutput in
// wasm_lightning_provider.go:
//
//	fetch_lightning({"lat": float64, "lon": float64, "from": RFC3339, "to": RFC3339, "run": RFC3339 (optional)}) -> {
//	  "run": RFC3339,
//	  "windows": [{start, end, probability}]
//	}
//
// probability is 0..1 for the grid cell holding (lat, lon), one entry per
// 3-hour window whose END falls in (from, to]. The plugin does not bucket or
// smooth. A window missing from a complete run is left out, never filled in;
// any other fetch or parse failure is an error. When the host passes "run"
// the plugin reads that run without looking for the newest.
//
// One window costs a whole-grid download, so the host walks the trace in
// spans of 24 hours (eight windows), one call each, under a 60 second
// background budget (lightning_schedule.go). It finds the run on the first
// span of a refresh and passes it to the rest.
package main

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// lightningWindow is one window of probability at a point.
type lightningWindow struct {
	Start, End  time.Time
	Probability float64
}

// lightningSpan is one plugin call's answer: the run the windows were read
// from and the windows that run has inside the span asked for.
type lightningSpan struct {
	Run     time.Time
	Windows []lightningWindow
}

type lightningProvider interface {
	ID() string
	Name() string
	Description() string
	TTLSeconds() int64
	// FetchLightning returns the windows ending in (from, to] at the point. A
	// non-zero run names the forecast run to read, so the plugin need not
	// look for the newest; zero means find it.
	FetchLightning(lat, lon float64, from, to, run time.Time) (lightningSpan, error)
}

var lightningProviderRegistry = map[string]lightningProvider{}
var lightningProviderOrder []string

func registerLightningProvider(p lightningProvider) {
	lightningProviderRegistry[p.ID()] = p
	lightningProviderOrder = append(lightningProviderOrder, p.ID())
}

func getLightningProvider(id string) (lightningProvider, bool) {
	p, ok := lightningProviderRegistry[id]
	return p, ok
}

type lightningProviderInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func lightningProvidersHandler(c echo.Context) error {
	result := make([]lightningProviderInfo, 0, len(lightningProviderOrder))
	for _, id := range lightningProviderOrder {
		if provider, ok := lightningProviderRegistry[id]; ok {
			result = append(result, lightningProviderInfo{ID: provider.ID(), Name: provider.Name(), Description: provider.Description()})
		}
	}
	return c.JSON(http.StatusOK, result)
}

const defaultLightningProviderID = "ukmo-lightning"

// lightningProviderFromSettings reads ui.lightning_provider from settings
// already read.
//
// Like upper air, lightning is an extra rather than a section that fails
// visibly without it: an unset provider with nothing installed returns a nil
// provider, which the handler answers as "no lightning". Naming a provider
// that is not installed is a real mistake and is an error.
func lightningProviderFromSettings(settings map[string]any) (lightningProvider, string, error) {
	uiMap, _ := settings["ui"].(map[string]any)
	configured := strings.TrimSpace(coerceString(uiMap["lightning_provider"]))
	explicit := configured != ""
	if !explicit {
		configured = defaultLightningProviderID
	}

	provider, ok := getLightningProvider(configured)
	if !ok {
		if explicit {
			return nil, configured, fmt.Errorf("unknown lightning provider configured: %q (is the plugin installed in plugins/lightning?)", configured)
		}
		return nil, configured, nil
	}
	return provider, configured, nil
}

// The Met Office grid the cache is keyed by (ADR 0284): 0.1875 degrees of
// latitude by 0.28125 of longitude, cell edges at -90 and -180. A plugin on
// another grid still caches correctly by this key, since a cell here is only
// the unit the boat is considered to have moved by.
const (
	lightningCellLatStep = 0.1875
	lightningCellLonStep = 0.28125
	lightningCellRows    = 960
	lightningCellCols    = 1280
)

// lightningCellIndex returns the grid cell holding a position. Latitude 90
// falls in the northernmost row and longitude 180 wraps to column 0.
func lightningCellIndex(lat, lon float64) (row, col int) {
	row = int(math.Floor((lat + 90) / lightningCellLatStep))
	if row < 0 {
		row = 0
	}
	if row >= lightningCellRows {
		row = lightningCellRows - 1
	}
	x := math.Mod(lon+180, 360)
	if x < 0 {
		x += 360
	}
	col = int(math.Floor(x / lightningCellLonStep))
	if col >= lightningCellCols {
		col = lightningCellCols - 1
	}
	return row, col
}

// --- HTTP response shapes ---

type lightningWindowResponse struct {
	Start       string  `json:"start"`
	End         string  `json:"end"`
	Probability float64 `json:"probability"`
	// Run is the forecast run this window was read from. Windows held across a
	// failed refresh come from older runs than their neighbours, and the age of
	// the run is what the page labels stale by.
	Run string `json:"run"`
}

type lightningResponse struct {
	Provider string `json:"provider"`
	// Run is the newest run any window came from; empty with no windows.
	Run     string                    `json:"run"`
	Windows []lightningWindowResponse `json:"windows"`
	// UpdatedAt is when the near windows were last refreshed; empty if never.
	UpdatedAt  string `json:"updated_at"`
	TTLSeconds int64  `json:"ttl_seconds"`
	// Refreshing is true while a fetch is running. The first request for a
	// place answers at once with whatever is held, which may be nothing, and
	// the page asks again.
	Refreshing bool `json:"refreshing"`
	// RefreshError is why the last refresh failed, or why part of the far range
	// is missing. With nothing held and no refresh running a failure is a 502
	// instead.
	RefreshError string `json:"refresh_error,omitempty"`
}

// lightningForecast serves GET /api/lightning for the boat's own position.
//
// There is no lat/lon parameter: the far windows are refreshed once a day for
// the cell the boat is in (ADR 0284), so an arbitrary place would cost a
// full-range download of its own. The position is gated like waveForecast's:
// SignalK's -1,-1 and 0,0 placeholders are not a position.
//
// Answers 200 with no windows when no provider is installed, or when nothing
// is held yet but a refresh is running. A provider that is configured but
// broken, with nothing held and nothing running, is a 502.
func lightningForecast(c echo.Context) error {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	// The settings are read once and shared by the provider choice and the
	// SignalK URL check.
	settings, err := readSettings(settingsPath)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to read settings: %v", err)})
	}
	provider, configured, err := lightningProviderFromSettings(settings)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	if provider == nil {
		return c.JSON(http.StatusOK, lightningResponse{Windows: []lightningWindowResponse{}})
	}

	vesselState, status, msg := ownShipStateFromSettings(settings, true)
	if status != 0 {
		return c.JSON(status, map[string]string{"error": msg})
	}

	view := lightningSchedulerFor(provider).view(provider, vesselState.Latitude, vesselState.Longitude)
	// Nothing held and nothing running is the only 502: while a refresh runs the
	// page is told so, with the last error if there was one, and asks again.
	if len(view.Windows) == 0 && view.RefreshError != "" && !view.Refreshing {
		log.Printf("lightning provider %q error: %s", configured, view.RefreshError)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("lightning provider %q unavailable: %s", configured, view.RefreshError)})
	}

	response := lightningResponse{
		Provider:     configured,
		Windows:      make([]lightningWindowResponse, 0, len(view.Windows)),
		TTLSeconds:   provider.TTLSeconds(),
		Refreshing:   view.Refreshing,
		RefreshError: view.RefreshError,
	}
	var newest time.Time
	for _, w := range view.Windows {
		response.Windows = append(response.Windows, lightningWindowResponse{
			Start:       w.Start.UTC().Format(time.RFC3339),
			End:         w.End.UTC().Format(time.RFC3339),
			Probability: w.Probability,
			Run:         w.Run.UTC().Format(time.RFC3339),
		})
		if w.Run.After(newest) {
			newest = w.Run
		}
	}
	if !newest.IsZero() {
		response.Run = newest.UTC().Format(time.RFC3339)
	}
	if !view.NearAt.IsZero() {
		response.UpdatedAt = view.NearAt.UTC().Format(time.RFC3339)
	}

	etag, etagErr := weakETagForJSON(response)
	if etagErr != nil {
		log.Printf("Failed to build lightning ETag: %v", etagErr)
	}
	return respondJSONWithETag(c, http.StatusOK, etag, response)
}
