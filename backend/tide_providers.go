package main

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// tideStation describes a single tide prediction location: a
// provider-defined station (e.g. a BOM port or a NOAA station).
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
	// TidalPhase, DoubleHighToday, and DoubleLowToday are computed centrally
	// by classifyTidalPhase/hasDoubleTide from the two HTTP handlers
	// (tideChartHandler, tideToday) - never by individual providers - so
	// every provider (native or WASM plugin) gets identical behavior with
	// zero risk of drift. See classifyTidalPhase/hasDoubleTide below.
	TidalPhase      string `json:"tidal_phase,omitempty"`
	DoubleHighToday bool   `json:"double_high_today,omitempty"`
	DoubleLowToday  bool   `json:"double_low_today,omitempty"`
}

// tideProvider is the interface implemented by each pluggable tide data
// source (BOM, NOAA, ...) - every one a WASM plugin, there is no built-in
// native implementation.
//
// Convention: SearchStations("", limit) must return up to limit stations
// from the provider's full catalog, with real Lat/Lon populated on each
// station. This isn't just a typeahead-search nicety - nearestStation below
// relies on it generically to do geo lookup (nearest-station) for any
// provider, not only BOM. Every provider already follows it: the BOM and
// NOAA WASM plugins (docs/examples/tide-plugins/bom/bom.go,
// docs/examples/tide-plugins/noaa/main.go).
type tideProvider interface {
	ID() string
	Name() string
	Description() string
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

// maxStationsForNearestLookup bounds the catalog fetch nearestStation makes
// via SearchStations("", limit) - comfortably covers NOAA's ~3500 stations
// and BOM's smaller list.
const maxStationsForNearestLookup = 5000

// nearestStation finds the station in p's full catalog closest to (lat, lon)
// by great-circle distance, using the existing shared haversineMeters
// (backend/signalk.go). Relies on the tideProvider interface's documented
// convention that SearchStations("", limit) returns up to limit stations
// from the provider's full catalog with real Lat/Lon populated. Returns
// ok=false if the provider has no stations at all.
func nearestStation(p tideProvider, lat, lon float64) (tideStation, bool) {
	stations := p.SearchStations("", maxStationsForNearestLookup)
	if len(stations) == 0 {
		return tideStation{}, false
	}
	best := stations[0]
	bestDist := haversineMeters(lat, lon, best.Lat, best.Lon)
	for _, s := range stations[1:] {
		if d := haversineMeters(lat, lon, s.Lat, s.Lon); d < bestDist {
			bestDist, best = d, s
		}
	}
	return best, true
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

// minExtremesForTidalPhase is the smallest extremes count classifyTidalPhase
// will attempt to classify from. A tidal "phase" is inherently a comparison
// between two consecutive-extreme pairs (the widest range vs. the narrowest
// range) - with fewer than 4 extremes there are fewer than 3 pairs, too
// little resolution to distinguish a meaningful spring/neap swing from
// nearest-neighbor noise. This mirrors the bail-out spirit of the frontend
// heuristic (frontend/src/lib/tide-phase.ts, which requires at least 2
// extremes to have any pair at all), but sets a higher floor here because
// this function also has to resolve a day offset, not just a within-window
// position.
const minExtremesForTidalPhase = 4

// classifyTidalPhase labels the local spring/neap tide phase nearest to now,
// shared by all tide providers (native and WASM plugin alike) and invoked
// only from the two HTTP handlers (tideChartHandler, tideToday) - see
// tideChartResult's TidalPhase field doc comment.
//
// It finds the consecutive-extreme pair with the largest height range
// (nearest local springs) and the pair with the smallest range (nearest
// local neaps) within the given extremes, then labels whichever pair's
// midpoint time is temporally closer to now: "springs"/"neaps" if that
// midpoint falls on the same calendar day as now (in now's own location),
// otherwise "springs+N"/"springs-N"/"neaps+N"/"neaps-N" for N whole days
// offset (future is positive, past is negative).
//
// This is an approximation - "days from the nearest locally-observed range
// extremum" - not true astronomical spring/neap phase (which tracks lunar
// synodic position; coastal/amphidromic effects can distort locally observed
// range at some sites). Same simplification the frontend heuristic already
// makes.
//
// Returns "" when there isn't enough data to resolve a meaningful local
// extremum (fewer than minExtremesForTidalPhase extremes, or no range
// variation between any pairs).
func classifyTidalPhase(extremes []tideExtremePoint, now time.Time) string {
	if len(extremes) < minExtremesForTidalPhase {
		return ""
	}

	sorted := make([]tideExtremePoint, len(extremes))
	copy(sorted, extremes)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Time.Before(sorted[j].Time) })

	type extremePair struct {
		rangeM  float64
		midTime time.Time
	}

	pairs := make([]extremePair, 0, len(sorted)-1)
	for i := 0; i < len(sorted)-1; i++ {
		a, b := sorted[i], sorted[i+1]
		pairs = append(pairs, extremePair{
			rangeM:  math.Abs(b.HeightM - a.HeightM),
			midTime: a.Time.Add(b.Time.Sub(a.Time) / 2),
		})
	}

	maxIdx, minIdx := 0, 0
	for i, p := range pairs {
		if p.rangeM > pairs[maxIdx].rangeM {
			maxIdx = i
		}
		if p.rangeM < pairs[minIdx].rangeM {
			minIdx = i
		}
	}

	if pairs[maxIdx].rangeM == pairs[minIdx].rangeM {
		return "" // no range variation to distinguish springs from neaps
	}

	springsPair := pairs[maxIdx]
	neapsPair := pairs[minIdx]

	absDuration := func(d time.Duration) time.Duration {
		if d < 0 {
			return -d
		}
		return d
	}

	base := "springs"
	repTime := springsPair.midTime
	if absDuration(now.Sub(neapsPair.midTime)) < absDuration(now.Sub(springsPair.midTime)) {
		base = "neaps"
		repTime = neapsPair.midTime
	}

	repTime = repTime.In(now.Location())
	nowDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	repDate := time.Date(repTime.Year(), repTime.Month(), repTime.Day(), 0, 0, 0, 0, now.Location())
	daysDiff := int(math.Round(repDate.Sub(nowDate).Hours() / 24))

	if daysDiff == 0 {
		return base
	}
	return fmt.Sprintf("%s%+d", base, daysDiff)
}

// hasDoubleTide reports whether the local calendar day containing now (per
// station.Timezone, falling back to UTC with a logged warning if the
// timezone is empty or fails to load) has two or more predicted high tides
// and/or two or more predicted low tides. Shared by all tide providers and
// invoked only from the two HTTP handlers, same as classifyTidalPhase.
func hasDoubleTide(extremes []tideExtremePoint, station tideStation, now time.Time) (doubleHigh, doubleLow bool) {
	loc := time.UTC
	switch {
	case station.Timezone == "":
		log.Printf("hasDoubleTide: station %q has no timezone configured, falling back to UTC", station.StationID)
	default:
		l, err := time.LoadLocation(station.Timezone)
		if err != nil {
			log.Printf("hasDoubleTide: failed to load timezone %q for station %q: %v, falling back to UTC", station.Timezone, station.StationID, err)
		} else {
			loc = l
		}
	}

	year, month, day := now.In(loc).Date()

	var highCount, lowCount int
	for _, e := range extremes {
		ey, em, ed := e.Time.In(loc).Date()
		if ey != year || em != month || ed != day {
			continue
		}
		if e.High {
			highCount++
		} else {
			lowCount++
		}
	}

	return highCount >= 2, lowCount >= 2
}

type tideProviderInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func tideProvidersHandler(c echo.Context) error {
	result := make([]tideProviderInfo, 0, len(tideProviderOrder))
	for _, id := range tideProviderOrder {
		if provider, ok := tideProviderRegistry[id]; ok {
			result = append(result, tideProviderInfo{ID: provider.ID(), Name: provider.Name(), Description: provider.Description()})
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
	Station         tideStation                `json:"station"`
	Extremes        []tideExtremePointResponse `json:"extremes"`
	CurrentHeightM  float64                    `json:"current_height_m"`
	Direction       string                     `json:"direction"`
	Cached          bool                       `json:"cached"`
	UpdatedAt       string                     `json:"updated_at"`
	TTLSeconds      int64                      `json:"ttl_seconds"`
	TidalPhase      string                     `json:"tidal_phase,omitempty"`
	DoubleHighToday bool                       `json:"double_high_today,omitempty"`
	DoubleLowToday  bool                       `json:"double_low_today,omitempty"`
}

func tideNearestHandler(c echo.Context) error {
	providerID := strings.TrimSpace(c.QueryParam("provider"))
	provider, ok := getTideProvider(providerID)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "unknown tide provider"})
	}

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}
	signalkURL := buildSignalKURL(address, port)
	vesselPath := getEnv("SIGNALK_VESSEL_PATH", "/signalk/v1/api/vessels/self")
	vesselState, err := fetchSignalKVesselState(signalkURL, vesselPath)
	if err != nil || !hasUsableVesselPosition(vesselState.Latitude, vesselState.Longitude) {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "vessel position unavailable"})
	}

	station, ok := nearestStation(provider, vesselState.Latitude, vesselState.Longitude)
	if !ok {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "no stations available"})
	}
	return c.JSON(http.StatusOK, station)
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

	now := time.Now().UTC()
	tidalPhase := classifyTidalPhase(result.Extremes, now)
	doubleHigh, doubleLow := hasDoubleTide(result.Extremes, result.Station, now)

	return c.JSON(http.StatusOK, tideChartResponse{
		Station:         result.Station,
		Extremes:        extremes,
		CurrentHeightM:  result.CurrentHeightM,
		Direction:       result.Direction,
		Cached:          result.Cached,
		UpdatedAt:       result.CachedAt.UTC().Format(time.RFC3339),
		TTLSeconds:      provider.TTLSeconds(),
		TidalPhase:      tidalPhase,
		DoubleHighToday: doubleHigh,
		DoubleLowToday:  doubleLow,
	})
}
