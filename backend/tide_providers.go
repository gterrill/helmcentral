package main

import (
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// tideStation describes a single tide prediction location, either a
// provider-defined station (e.g. a BOM port) or a pseudo-station such as
// Storm Glass's "current vessel position".
type tideStation struct {
	StationID string  `json:"station_id"`
	Name      string  `json:"name"`
	State     string  `json:"state,omitempty"`
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
	Timezone  string  `json:"timezone,omitempty"`
}

// tideExtremePoint is a single predicted high or low tide.
type tideExtremePoint struct {
	Time    time.Time `json:"time"`
	HeightM float64   `json:"height_m"`
	High    bool      `json:"high"`
}

// tideChartResult is the data needed to render a tide chart and the
// "Tide Now" summary for a station.
type tideChartResult struct {
	Station        tideStation        `json:"station"`
	Extremes       []tideExtremePoint `json:"extremes"`
	CurrentHeightM float64            `json:"current_height_m"`
	Direction      string             `json:"direction"`
	Cached         bool               `json:"cached"`
	CachedAt       time.Time          `json:"cached_at"`
}

// tideProvider is the interface implemented by each pluggable tide data
// source (BOM, Storm Glass, ...).
type tideProvider interface {
	ID() string
	Name() string
	TTLSeconds() int64
	SearchStations(query string, limit int) []tideStation
	FetchTideChart(stationID string) (tideChartResult, error)
}

var tideProviderRegistry = map[string]tideProvider{}
var tideProviderOrder []string

func registerTideProvider(p tideProvider) {
	tideProviderRegistry[p.ID()] = p
	tideProviderOrder = append(tideProviderOrder, p.ID())
}

func getTideProvider(id string) (tideProvider, bool) {
	p, ok := tideProviderRegistry[id]
	return p, ok
}

// interpolateTideNow cosine-interpolates the current tide height and
// direction between the extremes that bracket `now`. It is shared by all
// tide providers.
func interpolateTideNow(extremes []tideExtremePoint, now time.Time) (heightM float64, direction string) {
	if len(extremes) == 0 {
		return 0, "—"
	}

	sorted := make([]tideExtremePoint, len(extremes))
	copy(sorted, extremes)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Time.Before(sorted[j].Time) })

	var prev, next *tideExtremePoint
	for i := range sorted {
		if sorted[i].Time.After(now) {
			next = &sorted[i]
			if i > 0 {
				prev = &sorted[i-1]
			}
			break
		}
	}

	if prev == nil || next == nil {
		if next != nil {
			direction = "Falling"
			if next.High {
				direction = "Rising"
			}
			return next.HeightM, direction
		}
		last := sorted[len(sorted)-1]
		direction = "Rising"
		if last.High {
			direction = "Falling"
		}
		return last.HeightM, direction
	}

	totalSeconds := next.Time.Sub(prev.Time).Seconds()
	progress := 0.0
	if totalSeconds > 0 {
		progress = now.Sub(prev.Time).Seconds() / totalSeconds
	}
	progress = math.Max(0, math.Min(1, progress))

	heightM = (prev.HeightM+next.HeightM)/2 + (prev.HeightM-next.HeightM)/2*math.Cos(math.Pi*progress)

	direction = "Falling"
	if next.HeightM > prev.HeightM {
		direction = "Rising"
	}

	return heightM, direction
}

type tideProviderInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func tideProvidersHandler(c echo.Context) error {
	result := make([]tideProviderInfo, 0, len(tideProviderOrder))
	for _, id := range tideProviderOrder {
		if provider, ok := tideProviderRegistry[id]; ok {
			result = append(result, tideProviderInfo{ID: provider.ID(), Name: provider.Name()})
		}
	}
	return c.JSON(http.StatusOK, result)
}

func tideStationsHandler(c echo.Context) error {
	providerID := strings.TrimSpace(c.QueryParam("provider"))
	provider, ok := getTideProvider(providerID)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "unknown tide provider"})
	}

	query := c.QueryParam("q")
	return c.JSON(http.StatusOK, provider.SearchStations(query, 20))
}

type tideExtremePointResponse struct {
	Time    string  `json:"time"`
	HeightM float64 `json:"height_m"`
	High    bool    `json:"high"`
}

type tideChartResponse struct {
	Station        tideStation                `json:"station"`
	Extremes       []tideExtremePointResponse `json:"extremes"`
	CurrentHeightM float64                    `json:"current_height_m"`
	Direction      string                     `json:"direction"`
	Cached         bool                       `json:"cached"`
	UpdatedAt      string                     `json:"updated_at"`
	TTLSeconds     int64                      `json:"ttl_seconds"`
}

func tideChartHandler(c echo.Context) error {
	providerID := strings.TrimSpace(c.QueryParam("provider"))
	provider, ok := getTideProvider(providerID)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "unknown tide provider"})
	}

	stationID := strings.TrimSpace(c.QueryParam("station_id"))
	if stationID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "missing station_id"})
	}

	result, err := provider.FetchTideChart(stationID)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}

	extremes := make([]tideExtremePointResponse, 0, len(result.Extremes))
	for _, extreme := range result.Extremes {
		extremes = append(extremes, tideExtremePointResponse{
			Time:    extreme.Time.UTC().Format(time.RFC3339),
			HeightM: extreme.HeightM,
			High:    extreme.High,
		})
	}

	return c.JSON(http.StatusOK, tideChartResponse{
		Station:        result.Station,
		Extremes:       extremes,
		CurrentHeightM: result.CurrentHeightM,
		Direction:      result.Direction,
		Cached:         result.Cached,
		UpdatedAt:      result.CachedAt.UTC().Format(time.RFC3339),
		TTLSeconds:     provider.TTLSeconds(),
	})
}
