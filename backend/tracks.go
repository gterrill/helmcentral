package main

import (
	"context"
	"log"
	"net/http"
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

// solarZoneMissing is set while a good fix has no time zone, so that is logged once.
var solarZoneMissing atomic.Bool

// motoringTrail is kept separately: only records motoring state fixes,
// starting empty and filling purely from live sampling.
//
// Its own read side, GET /api/tracks/motoring, was deleted as dead code
// once ADR 0133 removed the map's drag-to-reposition gesture that was its
// only caller -- this write side was deliberately left in place, since that
// ADR's planned Phase 3 Adjust sheet needs a recent motoring breadcrumb
// again for its own drag-to-fine-tune handle. Restoring the read endpoint
// then is a small, self-contained change; recording is cheap enough
// (`recordMotoringPoint`, called from the track poller below) to keep
// running with nothing currently reading it.
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
	now := time.Now().UTC() // shared by wind/depth/solar recording this tick

	// Sample self vessel
	state, err := fetchSignalKVesselState()
	if err == nil {
		if state.WindSpeedApparentKts >= 0 {
			windGustHistory.record(state.WindSpeedApparentKts, now)
		}
		// True wind's own gust history (ADR 0130), recorded on the same tick
		// alongside the apparent one above so the two MAX GUST ladders stay
		// directly comparable. Gated on its own sentinel, independently of
		// whether apparent wind is present this tick.
		if state.WindSpeedTrueKts >= 0 {
			trueWindGustHistory.record(state.WindSpeedTrueKts, now)
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
	// The barometer gets the same age gate as the true wind below: a sensor
	// that has gone quiet must not keep re-recording its last value, which
	// would hold the Law of Storms figures and the status snapshot's pressure
	// change on a frozen number (ADR 0222).
	if pressure := trend(outsidePressurePath); pressure.Present {
		if age := alarmSampleAge(pressure, now); age >= 0 && age <= barometerMaxAge.Seconds() {
			barometerHistory.record(pressure.Value, now)
		}
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

	// Only the total is recorded here, so controller names do not matter.
	solar, solarErr := fetchSignalKSolarState(vesselView{})
	if solarErr == nil && solar.CurrentW >= 0 {
		if lat, lon, ok := usableOwnShipFix(state, nil); ok {
			if boat := boatTimeAt(lat, lon, true); boat.LastFix || boat.NoFix {
				// No zone for a good fix: leave the solar day boundary where it
				// was rather than bucket it at UTC.
				if solarZoneMissing.CompareAndSwap(false, true) {
					log.Printf("solar: no time zone could be found for the vessel position; the local-day boundary is left as it was")
				}
			} else {
				solarZoneMissing.Store(false)
				solarStats.mu.Lock()
				solarStats.loc = boat.Loc
				solarStats.mu.Unlock()
			}
		}
		solarStats.record(solar.CurrentW, now)
		solarPowerHistory.record(solar.CurrentW, now)
	}

	// The always-on track point log (ADR 0176) does its own fix filtering, so
	// it sees the position before the loose range check below.
	if err == nil {
		observeLiveTrack(globalSignalKSnapshot, state, settingsPath, now)
	}

	if err == nil && hasUsableVesselPosition(state.Latitude, state.Longitude) {
		recordTrackSelf(state.Latitude, state.Longitude)

		// The anchor swing trail (ADR 0199) is stored only while a watch is on.
		recordAnchorTrailPoint(state.Latitude, state.Longitude, now)

		if isMotoring(state.Status) {
			recordMotoringPoint(state.Latitude, state.Longitude)
		}

		// Place name resolution (place_name.go) runs here, on the server's
		// own poll tick, per docs/adr/0056.
		geoname := updateTickPlaceName(state.Latitude, state.Longitude)

		// One fetch of the nearby vessels serves both the sighting log and the
		// anchor watch's AIS tracks (ADR 0263); each recorder guards itself.
		//
		// Fetches with nearbyVesselsUnlimited, not fetchSignalKNearbyVessels' own
		// default 10 (the map tile's own display cap) - a code-review finding,
		// 2026-09-25: get_nearby_vessels (ADR 0128) lists up to
		// assistantNearbyVesselsMaxMaxResults (25), so a vessel ranked 11th-25th
		// nearest never got a sighting-log row at all, leaving in_range_since
		// missing or wrong for exactly the vessels a crowded anchorage most needs
		// it for. Both recorders are authoritative records of "who has been in
		// range," independent of any one caller's own display cap.
		nearby, nearbyErr := fetchSignalKNearbyVesselsLimit(state.Latitude, state.Longitude, now, []string{fetchSignalKSelfName()}, nearbyVesselsUnlimited)
		noteNearbyFetch(nearbyErr)
		if nearbyErr == nil {
			recordNearbyVesselContacts(nearby, state, geoname, now)
			recordAISAnchorTracks(nearby, now)
		}
	}
}

// nearbyFetchFailing is set while the tick's nearby-vessel fetch is failing, so
// the failure and the recovery are each logged once, not every tick.
var nearbyFetchFailing atomic.Bool

// noteNearbyFetch logs a failed fetch, which skips both recorders for the
// tick, once per change of state.
func noteNearbyFetch(err error) {
	if err != nil {
		if nearbyFetchFailing.CompareAndSwap(false, true) {
			log.Printf("nearby vessels: fetch failed, the sighting log and the anchor watch AIS tracks skip ticks until it recovers: %v", err)
		}
		return
	}
	if nearbyFetchFailing.CompareAndSwap(true, false) {
		log.Printf("nearby vessels: fetch recovered")
	}
}

// recordNearbyVesselContacts records a contact for each vessel in nearby in
// globalNearbyContactStore. This runs once per server-owned poll tick (ADR
// 0001: the server owns sampling, independent of client polling), not
// from the /api/nearby-vessels HTTP handler, which can be hit far more often
// by several open browser tabs - recording from the handler would defeat
// "record once per encounter." geoname is this tick's already-resolved place
// name (see updateTickPlaceName in place_name.go) rather than looked up again
// here. recordContactIfNew is one indexed SQLite read plus, only on a
// genuinely new encounter, one insert (WAL mode, ADR-documented in
// nearby_contacts.go), negligible next to what this poll tick already does.
func recordNearbyVesselContacts(nearby []nearbyVessel, state vesselStateData, geoname string, now time.Time) {
	if globalNearbyContactStore == nil {
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

// GET /api/tracks?since=<RFC3339>
// Returns the self track. Clients pass the timestamp of the last point they
// received so only new fixes are returned on subsequent polls. The AIS tracks
// recorded during an anchor watch are at /api/anchor-watch/trails/ais.
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

	payload := map[string]any{
		"self": toWire(selfPts),
	}
	etag, etagErr := weakETagForJSON(payload)
	if etagErr != nil {
		log.Printf("Failed to build tracks ETag: %v", etagErr)
	}
	return respondJSONWithETag(c, http.StatusOK, etag, payload)
}

// trackRoutes returns the /api routes for vessel track history.
// Each row carries its own auth tier (docs/adr/0177).
func trackRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodGet, "/api/tracks", tierRead, getTracksHandler},
	}
}
