package main

import (
	"math"
	"sort"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

type nearbyVessel struct {
	// ID is the bare SignalK vessel id (the key fetchSignalKNearbyVessels
	// reads from vesselsTree()), always present and unique by construction.
	// It exists because Name is not a real identity - two boats can share a
	// display name, and unnamed vessels all fall back to the same
	// compactVesselID shape - so the frontend needs a field it can use for
	// React reconciliation and marker-selection matching. No omitempty: a
	// missing id here is a bug, not an absent optional value.
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Mmsi       string   `json:"mmsi,omitempty"`
	RangeM     float64  `json:"range_m"`
	AgeSeconds int      `json:"age_seconds"`
	SogKnots   *float64 `json:"sog_knots,omitempty"`
	Lat        float64  `json:"lat"`
	Lon        float64  `json:"lon"`
	SeenCount  int      `json:"seen_count"`
	LastSeenAt string   `json:"last_seen_at,omitempty"`

	// AIS collision figures from signalk-ais-target-prioritizer (ADR 0057),
	// all optional. A target that is opening rather than closing carries a
	// bearing but no CPA at all, and with the plugin uninstalled a target
	// carries none of them - hence pointers, so "not computed" stays
	// distinguishable from a closest approach of zero.
	//
	// BearingRad is radians, converted on ingest from the degrees the plugin
	// actually publishes (collisionBearingRadians).
	CpaM                *float64 `json:"cpa_m,omitempty"`
	TcpaSeconds         *float64 `json:"tcpa_seconds,omitempty"`
	BearingRad          *float64 `json:"bearing_rad,omitempty"`
	CollisionAlarmType  string   `json:"collision_alarm_type,omitempty"`
	CollisionAlarmState string   `json:"collision_alarm_state,omitempty"`

	// PositionSeen is the delta receive time this vessel's position was
	// last refreshed at (see fetchSignalKNearbyVessels), threaded through to
	// recordNearbyVesselContacts -> recordContactIfNew so its confirmation
	// dwell can tell "still ticking on a frozen position" apart from "a
	// fresh AIS report actually arrived" (see nearby_contacts.go's
	// pendingContact). json:"-" keeps it off the wire entirely: it's
	// internal plumbing, not something either the tile or the anchor-watch
	// map has any use for.
	PositionSeen time.Time `json:"-"`
}

// nearbyFeedCap is the most boats the feed carries during an anchor watch: the
// 10 nearest the boat plus the boats near the anchor. A crowded anchorage could
// otherwise send hundreds of markers and rows to a phone or the wall display.
// With no watch the feed stays at the 10 nearest.
const nearbyFeedCap = 40

// nearbyFeedVessels picks what the feed carries from the full list, sorted by
// range from the boat: the 10 nearest the boat, and during an anchor watch
// (watch is not nil) also the boats within aisAnchorRecordRadiusM of the
// anchor, so a recorded boat has a marker however crowded the anchorage is
// (ADR 0263), up to nearbyFeedCap boats in all. When the cap bites it keeps the
// boats nearest the ANCHOR, the ones the watch cares about, and the feed is
// still returned in range order. The second result counts the boats near the
// anchor the cap left out; it is zero with no watch, where the 10-boat limit is
// the design. A recorded boat can still lack a marker when more than the cap
// are inside the circle, and the feed says so through that count.
func nearbyFeedVessels(all []nearbyVessel, watch *anchorWatchData) ([]nearbyVessel, int) {
	if len(all) <= nearbyVesselsDefaultLimit {
		return all, 0
	}
	feed := append([]nearbyVessel(nil), all[:nearbyVesselsDefaultLimit]...)
	if watch == nil {
		return feed, 0
	}
	var extras []nearbyVessel
	for _, v := range all[nearbyVesselsDefaultLimit:] {
		if withinAISAnchorRecordRadius(*watch, v.Lat, v.Lon) {
			extras = append(extras, v)
		}
	}
	room := nearbyFeedCap - len(feed)
	if len(extras) <= room {
		return append(feed, extras...), 0
	}
	sort.SliceStable(extras, func(i, j int) bool {
		return haversineMeters(watch.Lat, watch.Lon, extras[i].Lat, extras[i].Lon) <
			haversineMeters(watch.Lat, watch.Lon, extras[j].Lat, extras[j].Lon)
	})
	feed = append(feed, extras[:room]...)
	sort.SliceStable(feed, func(i, j int) bool { return feed[i].RangeM < feed[j].RangeM })
	return feed, len(extras) - room
}

// buildNearbyVesselsPayload produces the /api response body. Split from the handler so
// the SSE stream emits the identical shape without the two drifting apart.
func buildNearbyVesselsPayload() map[string]any {
	source := "backend-fallback"
	now := time.Now().UTC()
	vessels := []nearbyVessel{}
	cappedCount := 0
	var excludedNames []string

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)

	if signalkURL != "" {
		signalkSelfName := fetchSignalKSelfName()
		excludedNames = []string{signalkSelfName}

		state, selfErr := fetchSignalKVesselState()
		if selfErr == nil && hasUsableVesselPosition(state.Latitude, state.Longitude) {
			// One fetch of the whole list, then the selection: the 10 nearest, plus
			// during an anchor watch every boat within 500 m of the anchor.
			all, nearbyErr := fetchSignalKNearbyVesselsLimit(state.Latitude, state.Longitude, now, excludedNames, nearbyVesselsUnlimited)
			if nearbyErr == nil {
				var watchPtr *anchorWatchData
				if watch, active, _ := currentAnchorWatch(); active {
					watchPtr = &watch
				}
				nearby, capped := nearbyFeedVessels(all, watchPtr)
				if globalNearbyContactStore != nil {
					enrichNearbyVesselsWithContactHistory(globalNearbyContactStore, nearby)
				}

				vessels = nearby
				cappedCount = capped
				source = "signalk"
			}
		}
	}

	return map[string]any{
		"datetime": now.Format(time.RFC3339),
		"source":   source,
		"vessels":  vessels,
		// Whether the AIS feed behind the whole list has died — the
		// freshest contact's age, distinct from each vessel's own
		// age_seconds. Zero vessels in range stays -1 (unknown), never 0:
		// silence is not evidence the feed died (ADR 0068).
		"last_update_age_s": nearbyVesselsFeedAge(vessels),
		// The search radius behind the list, so the empty state can say how
		// far it looked rather than the client repeating a number it was
		// never told.
		"max_range_m": nearbyMaxRangeMeters,
		// How many boats near the anchor the feed left out during a watch
		// because it is capped at nearbyFeedCap. Zero when nothing was left out.
		"capped_count": cappedCount,
		// The cap itself, so no client repeats a number it was never told.
		"feed_cap": nearbyFeedCap,
		// Seconds since any other vessel last reported a position, in range or
		// not; -1 when none has been heard since the process started. It outlives
		// the snapshot's eviction of a silent vessel. Unlike last_update_age_s it
		// still ages when the list is empty, so a silent receiver reads as
		// silent and an empty sea does not.
		"ais_heard_age_s": aisHeardAge(now, excludedNames),
	}
}

func nearbyVessels(c echo.Context) error {
	return c.JSON(http.StatusOK, buildNearbyVesselsPayload())
}

// nearbyVesselRoutes returns the /api routes for nearby AIS contacts and their sighting history.
// Each row carries its own auth tier (docs/adr/0177).
func nearbyVesselRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodGet, "/api/nearby-vessels", tierRead, nearbyVessels},
		{http.MethodGet, "/api/nearby-vessels/:key/sightings", tierRead, getNearbyVesselSightingsHandler(globalNearbyContactStore)},
	}
}

// aisHeardAge is how long ago the snapshot last received a position from any
// vessel other than this one (named in excludedNames), or -1 when it holds none.
func aisHeardAge(now time.Time, excludedNames []string) float64 {
	seen := globalSignalKSnapshot.lastOtherVesselPositionSeen(excludedNames)
	if seen.IsZero() {
		return -1
	}
	age := now.Sub(seen).Seconds()
	if age < 0 {
		age = 0
	}
	return math.Round(age)
}
