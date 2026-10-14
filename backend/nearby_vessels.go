package main

import (
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

// buildNearbyVesselsPayload produces the /api response body. Split from the handler so
// the SSE stream emits the identical shape without the two drifting apart.
func buildNearbyVesselsPayload() map[string]any {
	source := "backend-fallback"
	now := time.Now().UTC()
	vessels := []nearbyVessel{}

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)

	if signalkURL != "" {
		signalkSelfName := fetchSignalKSelfName()
		excludedNames := []string{signalkSelfName}

		state, selfErr := fetchSignalKVesselState()
		if selfErr == nil && state.Latitude >= -90 && state.Latitude <= 90 && state.Longitude >= -180 && state.Longitude <= 180 {
			nearby, nearbyErr := fetchSignalKNearbyVessels(state.Latitude, state.Longitude, now, excludedNames)
			if nearbyErr == nil {
				if globalNearbyContactStore != nil {
					enrichNearbyVesselsWithContactHistory(globalNearbyContactStore, nearby)
				}

				vessels = nearby
				source = "signalk"
			}
		}
	}

	return map[string]any{
		"datetime": now.Format(time.RFC3339),
		"source":   source,
		"vessels":  vessels,
		// Whether the AIS/radar feed behind the whole list has died — the
		// freshest contact's age, distinct from each vessel's own
		// age_seconds. Zero vessels in range stays -1 (unknown), never 0:
		// silence is not evidence the feed died (ADR 0068).
		"last_update_age_s": nearbyVesselsFeedAge(vessels),
		// The search radius behind the list, so the empty state can say how
		// far it looked rather than the client repeating a number it was
		// never told.
		"max_range_m": nearbyMaxRangeMeters,
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
