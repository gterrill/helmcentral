package main

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	// A position-based provider is wrapped once, here, so the rest of the host
	// meets the station contract and never branches on it.
	if _, wrapped := p.(positionTideAdapter); !wrapped && tideProviderPositionBased(p) {
		p = positionTideAdapter{p}
	}
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
	if n, ok := p.(interface {
		NearestStation(lat, lon float64) (tideStation, bool)
	}); ok {
		return n.NearestStation(lat, lon)
	}
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

// defaultTideProviderID is used when ui.tide_provider is blank, as
// defaultWeatherProviderID is for weather (ADR 0273). It is a keyless global
// source, so a fresh install anywhere has a tide before anyone opens Settings.
// An install that already chose a provider keeps it.
const defaultTideProviderID = "open-meteo"

// The datum a provider's tide heights are measured from. A provider that does
// not say is chart datum: BOM and NOAA publish predictions on their charts'
// datum, which is what a depth at chart datum can be raised or lowered by.
// Open-Meteo's modelled sea level is from mean sea level, which is not.
const (
	tideDatumChart        = "chart_datum"
	tideDatumMeanSeaLevel = "mean_sea_level"
)

// tideCorrectsChartDepth says whether a tide's heights can be added to or taken
// from a depth at chart datum. From mean sea level they cannot: the sum would be
// a wrong depth, not an approximate one. The reason is a clause that starts in
// lower case, for callers to place in a sentence.
func tideCorrectsChartDepth(datum string) (ok bool, reason string) {
	if datum == tideDatumMeanSeaLevel {
		return false, "this tide source gives heights from mean sea level, not chart datum, so they can't be used with a depth at chart datum (choose a tide provider with a station in Settings for that)"
	}
	return true, ""
}

// tideDatumNote is what Mate and the screens say about heights from mean sea
// level.
const tideDatumNote = "Heights are metres from mean sea level, not chart datum, so they are routinely negative and are not a depth. " +
	"They include surge and air-pressure effects as well as the tide. Use the rise and fall, not the figure, to judge water under the keel."

// positionTideProvider is the optional side of a tideProvider (ADR 0273).
// PositionBased is true when the provider has no station catalog and takes a
// position as "<lat>,<lon>" in place of a station id (ADR 0033). HeightDatum
// is tideDatumChart or tideDatumMeanSeaLevel. A provider without these is
// station-based on chart datum, so BOM and NOAA need no change.
type positionTideProvider interface {
	PositionBased() bool
	HeightDatum() string
}

func tideProviderPositionBased(p tideProvider) bool {
	t, ok := p.(positionTideProvider)
	return ok && t.PositionBased()
}

func tideProviderHeightDatum(p tideProvider) string {
	if t, ok := p.(positionTideProvider); ok {
		return t.HeightDatum()
	}
	return tideDatumChart
}

// Names for the place a position-based tide is for.
const (
	tideNameBoatPosition = "Boat's position"
	tideNameChosenPlace  = "Selected place"
)

// tideBoatFixNow reads the boat's fix for a tide. A variable so tests can place
// the boat without a SignalK server. Callers that hold a vessel state already
// build the fix with tideBoatFixOf instead of calling it.
var tideBoatFixNow = func() tideBoatFix {
	state, err := tideVesselState()
	return tideBoatFixOf(state, err)
}

// tideVesselState reads the vessel state for a tide; a variable so tests can
// place the boat without a SignalK server.
var tideVesselState = fetchSignalKVesselState

// tideOwnFixNow is the boat's own fix, without the anchor rule: what a station
// provider uses to find its nearest station.
func tideOwnFixNow() tideBoatFix {
	state, err := tideVesselState()
	lat, lon, reason := boatOwnPosition(state, err)
	return tideBoatFix{Lat: lat, Lon: lon, Err: reason}
}

// tidePositionFromState is the point a position-based tide is taken for, or the
// reason there is none. This is the one place it is resolved, so the tide tile,
// the low-water check, Mate and the boat chart agree.
//
// While an anchor watch is set the point is the watch's anchor position, read
// fresh each call because Adjust can move it: a boat swinging on its rode near a
// rounding line must not flip the point, and with it the cache key, mid-stay. A
// watch with no usable anchor position is an error, never a quiet use of the
// boat's fix. With no watch, the boat's fix is used, trusted by
// usableOwnShipFix (a critical GNSS alert and the -1,-1 pair are refused) and
// explained only when it fails.
func tidePositionFromState(state vesselStateData, err error) (lat, lon float64, reason error) {
	w, active, loadErr := currentAnchorWatch()
	if loadErr != "" {
		// The watch file is unreadable, so whether a watch is set is not known;
		// the boat's fix would be a guess at which point the stay is on.
		return 0, 0, fmt.Errorf("the anchor watch could not be read, so the tide's point is not known: %s", loadErr)
	}
	if active {
		if !hasUsableVesselPosition(w.Lat, w.Lon) {
			return 0, 0, fmt.Errorf("an anchor watch is set but has no usable anchor position")
		}
		return w.Lat, w.Lon, nil
	}
	return boatOwnPosition(state, err)
}

// boatOwnPosition is the boat's own trusted fix, or the reason there is none:
// usableOwnShipFix's rule (a critical GNSS alert and the -1,-1 pair are
// refused), explained only when it fails.
func boatOwnPosition(state vesselStateData, err error) (lat, lon float64, reason error) {
	if lat, lon, ok := usableOwnShipFix(state, err); ok {
		return lat, lon, nil
	}
	switch {
	case err != nil:
		return 0, 0, err
	case state.GNSSCriticalAlert:
		// The reason names what the validation found, such as SignalK being
		// unreachable.
		if r := strings.TrimSpace(state.GNSSValidationReason); r != "" {
			return 0, 0, fmt.Errorf("the GNSS fix is flagged unreliable: %s", r)
		}
		return 0, 0, fmt.Errorf("the GNSS fix is flagged unreliable")
	}
	return 0, 0, fmt.Errorf("the boat has no usable position fix yet")
}

// tideBoatFix is the boat's position handed to buildTideToday by its caller,
// or the reason there is none.
type tideBoatFix struct {
	Lat, Lon float64
	Err      error
}

func tideBoatFixOf(state vesselStateData, err error) tideBoatFix {
	lat, lon, reason := tidePositionFromState(state, err)
	return tideBoatFix{Lat: lat, Lon: lon, Err: reason}
}

// tidePointID is the id of a position-based tide's point: the position rounded
// to the nearest 0.05 degree, written "<lat>,<lon>". It is the only formatter,
// and does no zone lookup.
func tidePointID(lat, lon float64) string {
	rlat, rlon := roundTidePosition(lat, lon)
	return fmt.Sprintf("%.2f,%.2f", rlat, rlon)
}

// tidePointStation is the station for a position-based tide, built in this one
// place: the point rounded to the nearest 0.05 degree, its id "<lat>,<lon>" in that
// rounding, the name the screens show, and the zone found there. A zone that
// cannot be found is logged and left off, as hasDoubleTide then logs its UTC
// fallback.
func tidePointStation(lat, lon float64, name string) tideStation {
	rlat, rlon := roundTidePosition(lat, lon)
	station := tideStation{StationID: tidePointID(lat, lon), Name: name, Lat: rlat, Lon: rlon}
	station.Timezone = tidePointZone(station.StationID, rlat, rlon)
	return station
}

// tidePointZones remembers the zone found at a tide point, or that none was
// found, so a point offshore is looked up and logged once and not on every
// fetch (zoneAtCached remembers successes only). The points are on a 0.05 degree
// grid, so the map stays small; it is cleared if it ever grows past a bound.
var (
	tidePointZonesMu sync.Mutex
	tidePointZones   = map[string]string{}
)

const tidePointZonesMax = 4096

func tidePointZone(id string, lat, lon float64) string {
	tidePointZonesMu.Lock()
	defer tidePointZonesMu.Unlock()
	if name, ok := tidePointZones[id]; ok {
		return name
	}
	name := ""
	if z, err := zoneAtCached(lat, lon); err == nil {
		name = z.Name
	} else {
		log.Printf("tide: no time zone for %s: %v", id, err)
	}
	if len(tidePointZones) >= tidePointZonesMax {
		tidePointZones = map[string]string{}
	}
	tidePointZones[id] = name
	return name
}

// parseTidePointID reads the "<lat>,<lon>" a position-based tide is asked for.
func parseTidePointID(id string) (lat, lon float64, ok bool) {
	parts := strings.Split(strings.TrimSpace(id), ",")
	if len(parts) != 2 {
		return 0, 0, false
	}
	lat, latErr := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	lon, lonErr := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if latErr != nil || lonErr != nil || !hasUsableVesselPosition(lat, lon) {
		return 0, 0, false
	}
	return lat, lon, true
}

// tidePointsPerDeg is how coarsely a position-based tide's point is rounded:
// the nearest 0.05 degree, about 5 km. The model's grid is finer than that but
// its sea level changes slowly across it, and a boat under way stays on one
// cache entry for hours instead of missing it every kilometre.
const tidePointsPerDeg = 20 // 1/0.05

func roundTidePosition(lat, lon float64) (float64, float64) {
	r := func(v float64) float64 { return math.Round(v*tidePointsPerDeg)/tidePointsPerDeg + 0 } // + 0 turns -0 into 0
	return r(lat), r(lon)
}

// positionTideAdapter wraps a position-based plugin so it meets the station
// contract (ADR 0273): its nearest station is the point itself, and its chart
// is asked for by "<lat>,<lon>", rounded, with the point and its zone stamped
// on the result in place of whatever station the plugin returned.
type positionTideAdapter struct{ tideProvider }

func (positionTideAdapter) PositionBased() bool { return true }

// Unwrap gives the plugin behind the adapter, for the handlers that need its
// path and companion files.
func (a positionTideAdapter) Unwrap() tideProvider { return a.tideProvider }

func (a positionTideAdapter) HeightDatum() string { return tideProviderHeightDatum(a.tideProvider) }

func (positionTideAdapter) NearestStation(lat, lon float64) (tideStation, bool) {
	if !hasUsableVesselPosition(lat, lon) {
		return tideStation{}, false
	}
	return tidePointStation(lat, lon, tideNameChosenPlace), true
}

func (a positionTideAdapter) FetchTideChart(stationID string) (tideChartResult, error) {
	lat, lon, ok := parseTidePointID(stationID)
	if !ok {
		return tideChartResult{}, fmt.Errorf("%q is not a usable \"<lat>,<lon>\" position", stationID)
	}
	res, err := a.tideProvider.FetchTideChart(tidePointID(lat, lon))
	if err != nil {
		return tideChartResult{}, err
	}
	res.Station = tidePointStation(lat, lon, tideNameChosenPlace)
	return res, nil
}

// fetchBoatTide is the tide at the boat's own point, named for it. Used by the
// tide tile and by the chart's "boat" station, so they read the same way.
func fetchBoatTide(p tideProvider, lat, lon float64) (tideChartResult, error) {
	res, err := p.FetchTideChart(tidePointID(lat, lon))
	if err != nil {
		return tideChartResult{}, err
	}
	res.Station.Name = tideNameBoatPosition
	return res, nil
}

// resolveTideProvider reads ui.tide_provider from settingsPath and resolves
// it against the registry, mirroring resolveWeatherProvider's idiom
// (weather_providers.go) - added for the assistant's get_tides tool (ADR
// 0093), which needs this resolution outside an HTTP handler.
//
// A blank id resolves to defaultTideProviderID (ADR 0273, reversing the "no
// default" stance of ADR 0033). A provider that is not registered, the default
// included, is an error naming plugins/tides, matching resolveWeatherProvider's
// wording for its own unregistered-provider case.
func resolveTideProvider(settingsPath string) (tideProvider, string, error) {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read settings: %w", err)
	}

	uiMap, _ := settings["ui"].(map[string]any)
	return resolveTideProviderFromUI(uiMap)
}

// resolveTideProviderFromUI is the one place the configured tide provider is
// read and defaulted, shared by resolveTideProvider and buildTideToday.
func resolveTideProviderFromUI(uiMap map[string]any) (tideProvider, string, error) {
	configuredProvider := strings.TrimSpace(coerceString(uiMap["tide_provider"]))
	chosen := configuredProvider != ""
	if !chosen {
		configuredProvider = defaultTideProviderID
	}

	provider, ok := getTideProvider(configuredProvider)
	if !ok && !chosen {
		return nil, configuredProvider, fmt.Errorf("no tide provider is chosen and the default worldwide tide plugin %q isn't installed: update the plugin bundle (plugins/tides), or choose a tide provider in Settings", configuredProvider)
	}
	if !ok {
		return nil, configuredProvider, fmt.Errorf("unknown tide provider configured: %q (is the plugin installed in plugins/tides?)", configuredProvider)
	}

	return provider, configuredProvider, nil
}

// cosineTideHeight is the height between two consecutive extremes at t, on the
// half cosine that joins them. t outside them is held at the nearer extreme.
func cosineTideHeight(prev, next *tideExtremePoint, t time.Time) float64 {
	progress := 0.0
	if total := next.Time.Sub(prev.Time).Seconds(); total > 0 {
		progress = math.Max(0, math.Min(1, t.Sub(prev.Time).Seconds()/total))
	}
	return (prev.HeightM+next.HeightM)/2 + (prev.HeightM-next.HeightM)/2*math.Cos(math.Pi*progress)
}

// interpolateTideNow cosine-interpolates the current tide height and
// direction between the extremes that bracket `now`. It is shared by all
// tide providers.
func interpolateTideNow(extremes []tideExtremePoint, now time.Time) (heightM float64, direction string) {
	if len(extremes) == 0 {
		return 0, ","
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

	heightM = cosineTideHeight(prev, next, now)

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
	// PositionBased is true for a provider with no stations to choose: it
	// follows the boat's position. HeightDatum is chart_datum or mean_sea_level.
	PositionBased bool   `json:"position_based"`
	HeightDatum   string `json:"height_datum"`
}

func tideProvidersHandler(c echo.Context) error {
	result := make([]tideProviderInfo, 0, len(tideProviderOrder))
	for _, id := range tideProviderOrder {
		if provider, ok := tideProviderRegistry[id]; ok {
			result = append(result, tideProviderInfo{
				ID: provider.ID(), Name: provider.Name(), Description: provider.Description(),
				PositionBased: tideProviderPositionBased(provider), HeightDatum: tideProviderHeightDatum(provider),
			})
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
	// HeightDatum is chart_datum or mean_sea_level: what height_m is measured from.
	HeightDatum string `json:"height_datum"`
	// Provider and ProviderName say who answered; PositionBased that the tide
	// follows a point, so there is no station to choose.
	Provider      string `json:"provider"`
	ProviderName  string `json:"provider_name"`
	PositionBased bool   `json:"position_based"`
}

func tideNearestHandler(c echo.Context) error {
	providerID := strings.TrimSpace(c.QueryParam("provider"))
	provider, ok := getTideProvider(providerID)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "unknown tide provider"})
	}

	// A named place (a planned stay) is measured from itself; otherwise from
	// the boat.
	lat, lon, explicit, pointErr := explicitForecastPoint(c)
	if pointErr != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": pointErr.Error()})
	}
	if !explicit {
		var err error
		// The anchor's point is for providers that follow a position; a station
		// provider finds its nearest station from the boat itself.
		var fix tideBoatFix
		if tideProviderPositionBased(provider) {
			fix = tideBoatFixNow()
		} else {
			fix = tideOwnFixNow()
		}
		lat, lon, err = fix.Lat, fix.Lon, fix.Err
		if err != nil {
			return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "vessel position unavailable"})
		}
	}

	station, ok := nearestStation(provider, lat, lon)
	if !ok {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "no stations available"})
	}
	if !explicit && tideProviderPositionBased(provider) {
		station.Name = tideNameBoatPosition
	}
	return c.JSON(http.StatusOK, station)
}

// tideChartHandler serves GET /api/tide-chart. With a provider and a station_id
// it is that provider's chart for that station. With neither it is the
// configured chart: the server picks the tide, so a screen never handles a
// station for a provider that follows a position. lat and lon, given together,
// ask for the tide at a place instead of the boat's.
func tideChartHandler(c echo.Context) error {
	providerID := strings.TrimSpace(c.QueryParam("provider"))
	if providerID == "" {
		return configuredTideChartHandler(c)
	}
	provider, ok := getTideProvider(providerID)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "unknown tide provider"})
	}

	stationID := strings.TrimSpace(c.QueryParam("station_id"))
	if stationID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "missing station_id"})
	}
	if tideProviderPositionBased(provider) {
		if _, _, ok := parseTidePointID(stationID); !ok {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "station_id must be \"<lat>,<lon>\" for this provider"})
		}
	}
	result, err := provider.FetchTideChart(stationID)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	return writeTideChart(c, provider, result)
}

// configuredTideChartHandler is the chart the settings call for, or for a place
// when lat and lon are given: the station nearest it, or for a provider that
// follows a position, its point.
func configuredTideChartHandler(c echo.Context) error {
	settings, err := readSettings(getEnv("SETTINGS_FILE", "../settings.yaml"))
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to read settings: %v", err), "reason": string(tideSettingsUnreadable)})
	}
	uiMap, _ := settings["ui"].(map[string]any)

	lat, lon, place, pointErr := explicitForecastPoint(c)
	if pointErr != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": pointErr.Error()})
	}

	var provider tideProvider
	var result tideChartResult
	if place {
		var resolveErr error
		provider, _, resolveErr = resolveTideProviderFromUI(uiMap)
		if resolveErr != nil {
			return c.JSON(http.StatusBadGateway, map[string]string{"error": resolveErr.Error(), "reason": string(tideNoPlugin)})
		}
		station, ok := nearestStation(provider, lat, lon)
		if !ok {
			return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "no tide station found near this place", "reason": string(tideNoStation)})
		}
		if result, err = provider.FetchTideChart(station.StationID); err != nil {
			return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error(), "reason": string(tideUnreachable)})
		}
	} else {
		var te *tideTodayError
		provider, _, result, te = configuredTideChart(uiMap, tideBoatFixNow)
		if te != nil {
			body := map[string]any{"error": te.message, "reason": string(te.reason)}
			// Say what kind of provider failed, so the screen does not guess
			// from a saved station while it waits.
			if p, _, rerr := resolveTideProviderFromUI(uiMap); rerr == nil {
				body["position_based"] = tideProviderPositionBased(p)
			}
			return c.JSON(te.status, body)
		}
	}
	return writeTideChart(c, provider, result)
}

// writeTideChart answers with a provider's chart, saying which provider it is,
// whether it follows a position, and what its heights are measured from.
func writeTideChart(c echo.Context, provider tideProvider, result tideChartResult) error {
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
		HeightDatum:     tideProviderHeightDatum(provider),
		Provider:        provider.ID(),
		ProviderName:    provider.Name(),
		PositionBased:   tideProviderPositionBased(provider),
	})
}
