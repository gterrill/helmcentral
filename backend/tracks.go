package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/labstack/echo/v4"
)

// ── Trail storage ─────────────────────────────────────────────────────────────

// selfTrack records self-vessel positions at all times (motoring, anchored, etc.).
var (
	trackMu   sync.RWMutex
	selfTrack = newVesselTrail()
)

const tracksAISTrailsCacheMaxAge = 15 * time.Second

type tracksAISTrailsCacheSnapshot struct {
	fetchedAt time.Time
	trails    map[string][]trackPoint
}

type tracksAISTrailsCache struct {
	mu         sync.Mutex
	cond       *sync.Cond
	refreshing bool
	snapshot   tracksAISTrailsCacheSnapshot
}

var tracksAISTrails = newTracksAISTrailsCache()

// tracksEndpointMissing remembers whether the last AIS trails fetch got a 404,
// so the missing-plugin log line is written when that changes rather than on
// every refresh.
var tracksEndpointMissing atomic.Bool

func newTracksAISTrailsCache() *tracksAISTrailsCache {
	c := &tracksAISTrailsCache{}
	c.cond = sync.NewCond(&c.mu)
	return c
}

func cloneTrackPointMap(src map[string][]trackPoint) map[string][]trackPoint {
	if len(src) == 0 {
		return map[string][]trackPoint{}
	}
	cloned := make(map[string][]trackPoint, len(src))
	for vesselID, pts := range src {
		copied := make([]trackPoint, len(pts))
		copy(copied, pts)
		cloned[vesselID] = copied
	}
	return cloned
}

func (s tracksAISTrailsCacheSnapshot) fresh() bool {
	return !s.fetchedAt.IsZero() && time.Since(s.fetchedAt) < tracksAISTrailsCacheMaxAge
}

func (c *tracksAISTrailsCache) get(settingsPath string) map[string][]trackPoint {
	c.mu.Lock()
	for c.refreshing {
		if len(c.snapshot.trails) > 0 {
			trails := cloneTrackPointMap(c.snapshot.trails)
			c.mu.Unlock()
			return trails
		}
		c.cond.Wait()
	}
	if c.snapshot.fresh() {
		trails := cloneTrackPointMap(c.snapshot.trails)
		c.mu.Unlock()
		return trails
	}
	c.refreshing = true
	c.mu.Unlock()

	trails, err := fetchSignalKAISTrailsResult(settingsPath)
	return c.finishRefresh(trails, err)
}

func (c *tracksAISTrailsCache) finishRefresh(trails map[string][]trackPoint, err error) map[string][]trackPoint {
	c.mu.Lock()
	defer func() {
		c.refreshing = false
		c.cond.Broadcast()
		c.mu.Unlock()
	}()

	if err == nil {
		c.snapshot = tracksAISTrailsCacheSnapshot{fetchedAt: time.Now().UTC(), trails: cloneTrackPointMap(trails)}
	} else if len(c.snapshot.trails) == 0 {
		c.snapshot = tracksAISTrailsCacheSnapshot{fetchedAt: time.Now().UTC(), trails: map[string][]trackPoint{}}
	}

	return cloneTrackPointMap(c.snapshot.trails)
}

// motoringTrail is kept separately: only records motoring state fixes,
// starting empty and filling purely from live sampling.
var (
	motoringTrailMu sync.RWMutex
	motoringTrail   = newVesselTrail()
)

// ── Internal recording ────────────────────────────────────────────────────────

func recordTrackSelf(lat, lon float64) {
	trackMu.Lock()
	defer trackMu.Unlock()
	selfTrack.addPoint(lat, lon)
}

func recordMotoringPoint(lat, lon float64) {
	motoringTrailMu.Lock()
	defer motoringTrailMu.Unlock()
	motoringTrail.addPoint(lat, lon)
}

// ── Server-side poller ────────────────────────────────────────────────────────

// startTrackPoller launches a background goroutine that samples both the
// self-vessel and all nearby AIS vessels every pollInterval, until ctx is
// cancelled. The client never needs to touch SignalK for trail data — it
// only calls /api/tracks.
//
// The returned channel closes once the goroutine has actually returned.
// main.go has no need for it (a fire-and-forget "go startTrackPoller(...)"
// simply discards it), but it gives a test a properly synchronized way to
// know shutdown is complete, mirroring radarSpokeRelay.done
// (radar_spoke_relay.go) for the same reason: a plain sleep-and-hope after
// cancel gives no happens-before guarantee that this goroutine's last read
// of a package-level global (globalSignalKSnapshot, mutated by other tests)
// has actually finished.
func startTrackPoller(ctx context.Context, pollInterval time.Duration) <-chan struct{} {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	done := make(chan struct{})

	go func() {
		defer close(done)
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// select does not prefer ctx.Done() over a ticker tick that
				// becomes ready at the same instant, so without this a
				// cancellation racing a tick could keep losing that draw and
				// sample once more (in principle repeatedly) after shutdown
				// was requested. Re-checking here bounds that to at most one
				// tick already in flight when cancellation lands.
				if ctx.Err() != nil {
					return
				}
				sampleTracks(settingsPath)
			}
		}
	}()

	return done
}

func sampleTracks(settingsPath string) {
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)
	vesselPath := getEnv("SIGNALK_VESSEL_PATH", "/signalk/v1/api/vessels/self")
	vesselsPath := getEnv("SIGNALK_VESSELS_PATH", "/signalk/v1/api/vessels")

	if signalkURL == "" {
		return
	}

	now := time.Now().UTC() // shared by wind/depth/solar recording this tick

	// Sample self vessel
	state, err := fetchSignalKVesselState()
	if err == nil {
		if state.WindSpeedApparentKts >= 0 {
			windGustHistory.record(state.WindSpeedApparentKts, now)
		}
		if state.Depth >= 0 {
			depthHistory.record(state.Depth, now)
		}
	}

	/*
	 * Heavy-weather trends (ADR 0070), read straight off the delta-stream
	 * snapshot rather than the vessel-state fetch, because none of these three
	 * are in that struct and none of them needs to be - nothing but the
	 * derived paths consumes them.
	 *
	 * A path the boat does not publish simply records nothing, and the derived
	 * value stays absent rather than becoming a confident zero.
	 */
	trend := derivedAwareAlarmReader(globalSignalKSnapshot)
	if pressure := trend(outsidePressurePath); pressure.Present {
		barometerHistory.record(pressure.Value, now)
	}
	// Present alone is not enough here: derived-data can stop producing
	// speedTrue/directionTrue while the rest of the snapshot - including the
	// anemometer's own apparent-wind path - keeps updating, and a bare
	// Present check would keep re-recording that stalled path's last value
	// on every tick forever, pinning the Current Conditions tile's 1h "obs"
	// marker (and the storm/squash-zone derived paths these two buffers also
	// feed) on a frozen number. alarmSampleAge reports each sample's own
	// LastSeen age (-1 when never seen), the same fact the alarm engine and
	// the gauge-values stream already gate staleness on, so a stalled
	// speedTrue is caught even while directionTrue (or apparent wind) keeps
	// arriving on its own, unrelated path.
	if windSpeed := trend(windSpeedTruePath); windSpeed.Present {
		if age := alarmSampleAge(windSpeed, now); age >= 0 && age <= defaultWindMaxAge.Seconds() {
			trueWindSpeedHistory.record(windSpeed.Value, now)
		}
	}
	if windDirection := trend(windDirectionTruePath); windDirection.Present {
		if age := alarmSampleAge(windDirection, now); age >= 0 && age <= defaultWindMaxAge.Seconds() {
			trueWindDirectionHistory.record(windDirection.Value, now)
		}
	}

	solar, solarErr := fetchSignalKSolarState()
	if solarErr == nil && solar.CurrentW >= 0 {
		if hasUsableVesselPosition(state.Latitude, state.Longitude) {
			solarStats.mu.Lock()
			solarStats.loc = vesselLocalLocation(state.Longitude)
			solarStats.mu.Unlock()
		}
		solarStats.record(solar.CurrentW, now)
		solarPowerHistory.record(solar.CurrentW, now)
	}

	if err == nil && state.Latitude >= -90 && state.Latitude <= 90 &&
		state.Longitude >= -180 && state.Longitude <= 180 {
		recordTrackSelf(state.Latitude, state.Longitude)

		// Also record post-anchor ring-buffer and motoring trail
		recordSelfTrailPoint(state.Latitude, state.Longitude)

		// AIS trails are no longer refreshed on this tick: getTracksHandler's
		// own tracksAISTrails.get() already refreshes on demand within its
		// 15s freshness window (backend perf audit #10), so refreshing here
		// too just doubled the upstream GET to /signalk/v1/api/tracks -- every
		// 5s from the poller regardless of whether any client had the map
		// open, on top of whatever the handler already did.
		if isMotoring(state.Status) {
			recordMotoringPoint(state.Latitude, state.Longitude)
		}

		// Place name resolution (place_name.go) runs here, on the server's
		// own poll tick, per docs/adr/0056 - it needs a real fix (not the
		// -1,-1/0,0 "no data yet" sentinels the loose range check above
		// still lets through), so it's gated separately.
		geoname := ""
		if hasUsableVesselPosition(state.Latitude, state.Longitude) {
			geoname = updateTickPlaceName(state.Latitude, state.Longitude)
		}

		recordNearbyVesselContacts(signalkURL, vesselsPath, vesselPath, state, geoname)
	}
}

// recordNearbyVesselContacts fetches the current set of nearby AIS vessels
// and records a contact for each in globalNearbyContactStore. This runs
// once per server-owned 5-second poll tick (ADR 0001: the server owns
// sampling, independent of client polling), not from the /api/nearby-
// vessels HTTP handler, which can be hit far more often by several open
// browser tabs - recording from the handler would defeat "record once per
// encounter." geoname is this tick's already-resolved place name (see
// updateTickPlaceName in place_name.go) rather than looked up again here -
// since resolution now runs live on every tick (docs/adr/0056), a second
// independent lookup would be redundant and the caller already knows the
// answer.
func recordNearbyVesselContacts(signalkURL, vesselsPath, vesselPath string, state vesselStateData, geoname string) {
	if globalNearbyContactStore == nil {
		return
	}

	signalkSelfName := fetchSignalKSelfName()
	excludedNames := []string{signalkSelfName}

	now := time.Now().UTC()
	nearby, err := fetchSignalKNearbyVessels(state.Latitude, state.Longitude, now, excludedNames)
	if err != nil {
		return
	}

	for _, v := range nearby {
		key, ok := vesselContactKey(v.Mmsi)
		if !ok {
			if _, already := missingMMSILoggedForContactRecording.LoadOrStore(v.ID, struct{}{}); !already {
				log.Printf("Skipping nearby vessel contact for %q: no MMSI reported", v.Name)
			}
			continue
		}
		if err := globalNearbyContactStore.recordContactIfNew(key, v.Name, v.Lat, v.Lon, geoname, state.Status, v.PositionSeen, now); err != nil {
			log.Printf("Failed to record nearby vessel contact for %s: %v", key, err)
		}
	}
}

// missingMMSILoggedForContactRecording is recordNearbyVesselContacts' own
// copy of nearby_contacts.go's missingMMSILoggedForContactHistory dedup:
// same "log once per vessel id, not once per 5s poll tick" fix (backend
// perf audit Tier 3), kept as a separate set because this is a different
// fact about the vessel (contact recording, not sighting-history
// enrichment) and the two call sites should not suppress each other's
// first line.
var missingMMSILoggedForContactRecording sync.Map // key: vessel id (string)

func isMotoring(status string) bool {
	s := trimEnvValue(status)
	return s == "motoring" ||
		s == "under way using engine" ||
		s == "under_way_using_engine"
}

// ── HTTP handlers ─────────────────────────────────────────────────────────────

// trackPoint is the wire format for a single timestamped position.
type trackPoint struct {
	Lat       float64   `json:"lat"`
	Lon       float64   `json:"lon"`
	Timestamp time.Time `json:"timestamp"`
}

func toWire(pts []*trailPoint) []trackPoint {
	if len(pts) == 0 {
		return nil
	}
	out := make([]trackPoint, len(pts))
	for i, p := range pts {
		out[i] = trackPoint{Lat: p.Lat, Lon: p.Lon, Timestamp: p.Timestamp}
	}
	return out
}

type signalKTrackSnapshot struct {
	Type        string        `json:"type"`
	Coordinates [][][]float64 `json:"coordinates"`
	Geometry    *struct {
		Type        string        `json:"type"`
		Coordinates [][][]float64 `json:"coordinates"`
	} `json:"geometry,omitempty"`
}

func fetchSignalKAISTrailsResult(settingsPath string) (map[string][]trackPoint, error) {
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)
	if signalkURL == "" {
		return map[string][]trackPoint{}, nil
	}

	tracksPath := getEnv("SIGNALK_TRACKS_PATH", "/signalk/v1/api/tracks")
	tracksURL := strings.TrimRight(signalkURL, "/") + "/" + strings.TrimLeft(tracksPath, "/")

	client := &http.Client{Timeout: 4 * time.Second}
	response, err := client.Get(tracksURL)
	if err != nil {
		return map[string][]trackPoint{}, err
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotFound {
		// Drain so the connection can be reused rather than left half-read,
		// same as the error branch just below. A 404 here is a fact about the
		// boat's configuration, not a failure: the tracks plugin simply is not
		// installed, the same idiom fetchSignalKNotificationsTree already
		// uses for a 404'd sub-resource (notification_sync.go). It must still
		// surface rather than vanish silently, so it is logged; the trails
		// stay empty rather than becoming an error, since "the plugin is not
		// there" is not something a retry or a stale cache fixes.
		// Logged once per change of state: /api/tracks refreshes this on
		// demand every 15 s while a map is open, and a line per refresh
		// would bury everything else in the log buffer.
		io.Copy(io.Discard, response.Body)
		if tracksEndpointMissing.CompareAndSwap(false, true) {
			log.Printf("tracks: signalk has no %s endpoint (404); AIS trails will stay empty until it appears", tracksPath)
		}
		return map[string][]trackPoint{}, nil
	}
	if tracksEndpointMissing.CompareAndSwap(true, false) {
		log.Printf("tracks: signalk %s endpoint answered again", tracksPath)
	}
	if response.StatusCode != http.StatusOK {
		// An unexpected status (5xx, a proxy timeout page, ...) is a real
		// failure, unlike the 404 above -- returning it as an error rather
		// than a silent empty success lets tracksAISTrailsCache.finishRefresh
		// keep serving the last-good trails instead of blanking them on a
		// blip (AGENTS.md fallback policy).
		body, _ := io.ReadAll(response.Body)
		return map[string][]trackPoint{}, fmt.Errorf("signalk returned status %d fetching AIS trails: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return map[string][]trackPoint{}, err
	}

	var payload map[string]signalKTrackSnapshot
	if err := json.Unmarshal(body, &payload); err != nil {
		return map[string][]trackPoint{}, err
	}

	nameMap, nameErr := fetchSignalKVesselNameMap()
	if nameErr != nil {
		nameMap = map[string]string{}
	}
	signalkSelfName := strings.ToUpper(strings.TrimSpace(fetchSignalKSelfName()))

	result := make(map[string][]trackPoint, len(payload))
	// Truncated to millisecond precision for the same reason as
	// vesselTrail.addPoint (anchor.go): the client feeds the max AIS
	// timestamp it has seen back into `since` as a millisecond-precision
	// JS Date string, and a nanosecond-precision synthesized timestamp
	// here would never match it exactly.
	now := time.Now().UTC().Truncate(time.Millisecond)
	for vesselID, snapshot := range payload {
		var multiCoords [][][]float64
		if snapshot.Geometry != nil && len(snapshot.Geometry.Coordinates) > 0 {
			multiCoords = snapshot.Geometry.Coordinates
		} else if len(snapshot.Coordinates) > 0 {
			multiCoords = snapshot.Coordinates
		}

		if len(multiCoords) == 0 {
			continue
		}

		// Always take the most recent track segment
		coords := multiCoords[len(multiCoords)-1]
		if len(coords) == 0 {
			continue
		}

		// Track contexts are "vessels.<id>" but the vessels endpoint keys by "<id>" alone.
		lookupKey := strings.TrimPrefix(vesselID, "vessels.")
		if lookupKey == "self" {
			continue
		}

		name := strings.ToUpper(strings.TrimSpace(nameMap[lookupKey]))
		if name == "" {
			name = strings.ToUpper(strings.TrimSpace(compactVesselID(vesselID)))
		}
		if name == "SELF" || name == signalkSelfName {
			continue
		}

		start := now.Add(-time.Duration(len(coords)-1) * time.Second)
		points := make([]trackPoint, 0, len(coords))
		for i, coord := range coords {
			if len(coord) < 2 {
				continue
			}
			lon := coord[0]
			lat := coord[1]
			if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
				continue
			}
			points = append(points, trackPoint{Lat: lat, Lon: lon, Timestamp: start.Add(time.Duration(i) * time.Second)})
		}
		if len(points) > 0 {
			result[name] = points
		}
	}

	return result, nil
}

func fetchSignalKAISTrails(settingsPath string) map[string][]trackPoint {
	trails, err := fetchSignalKAISTrailsResult(settingsPath)
	if err != nil {
		return map[string][]trackPoint{}
	}
	return trails
}

// GET /api/tracks?since=<RFC3339>
// Returns self and AIS vessel tracks for anchor-watch display.
// Clients pass the timestamp of the last point they received so only
// new fixes are returned on subsequent polls.
func getTracksHandler(c echo.Context) error {
	sinceStr := c.QueryParam("since")
	var since time.Time
	if sinceStr != "" {
		var err error
		since, err = time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid since timestamp"})
		}
	}

	trackMu.RLock()
	selfPts := selfTrack.pointsSince(since)
	trackMu.RUnlock()

	aisWire := tracksAISTrails.get(getEnv("SETTINGS_FILE", "../settings.yaml"))
	payload := map[string]any{
		"self": toWire(selfPts),
		"ais":  aisWire,
	}
	etag, etagErr := weakETagForJSON(payload)
	if etagErr != nil {
		log.Printf("Failed to build tracks ETag: %v", etagErr)
	}
	return respondJSONWithETag(c, http.StatusOK, etag, payload)
}

// GET /api/tracks/motoring
// Returns the full motoring approach track, built purely from live polling
// while motoring. Fetched once by the client when entering anchor
// reposition mode.
func getMotoringTrackHandler(c echo.Context) error {
	motoringTrailMu.RLock()
	pts := motoringTrail.pointsSince(time.Time{})
	motoringTrailMu.RUnlock()

	return c.JSON(http.StatusOK, map[string]any{
		"points": toWire(pts),
	})
}
