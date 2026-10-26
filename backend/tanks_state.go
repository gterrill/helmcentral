package main

import (
	"log"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

type tankLevelData struct {
	ID           string  `json:"id"`
	Label        string  `json:"label"`
	Category     string  `json:"category"`
	Kind         string  `json:"kind"`
	LevelPercent float64 `json:"level_percent"`
	// LastUpdateAge mirrors the solar-controller pattern (ADR 0068): each
	// tank carries its own age, scoped to that tank's own subtree, so one
	// dead sender does not condemn every tank on the boat. -1 when the
	// tank's currentLevel carries no timestamp.
	LastUpdateAge float64 `json:"last_update_age_s"`
	// WarnPercent and AlarmPercent are the tank's resolved levels (ADR 0214),
	// the same numbers its alarms use. AlarmsOff only silences the alarms;
	// the tile still colours by the levels.
	WarnPercent  float64 `json:"warn_percent"`
	AlarmPercent float64 `json:"alarm_percent"`
	AlarmsOff    bool    `json:"alarms_off"`
}

// buildTanksStatePayload produces the /api response body. Split from the handler so
// the SSE stream emits the identical shape without the two drifting apart.
func buildTanksStatePayload() map[string]any {
	now := time.Now().UTC()
	tanks := []tankLevelData{}
	source := "backend-fallback"
	tanksError := ""

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)
	// A tank is listed under the name of the equipment item that owns it
	// (ADR 0187); one no item owns is not listed.
	vessel, vesselErr := loadVesselView(settingsPath)
	noteVesselRead("tanks", vesselErr, "tanks: tank names unavailable, no tanks listed: %v")
	labelOverrides := vessel.Tanks

	if signalkURL != "" {
		stateTanks, datetime, fetchErr := fetchSignalKTanksState(labelOverrides)
		if fetchErr == nil {
			tanks = stateTanks
			if err := annotateTankLevels(tanks); err != nil {
				log.Printf("tanks: %v", err)
				tanks = []tankLevelData{}
				tanksError = err.Error()
			}
			now = datetime
			source = "signalk"
		}
	}

	// The three derived fuel figures (ADR 0084), from the same
	// computeDerivedPaths pass a gauge bound to the same helmcentral.fuel.*
	// path reads, so the built-in Tanks tile and a configured gauge can never
	// disagree. Computed against the actual current instant rather than
	// `now` above, which tracks the (possibly REST-reported) tank datetime,
	// not the vessel clock the derivation's own node timestamps are read
	// against.
	derivedValues, derivedAges := computeDerivedPaths(time.Now().UTC())

	return map[string]any{
		"datetime": now.Format(time.RFC3339),
		"source":   source,
		"tanks":    tanks,
		// Set when the tanks could not be given their levels (ADR 0214); the
		// tile says so instead of colouring by numbers the alarms don't use.
		"error": tanksError,
		// The freshest of the per-tank ages above: what the Tanks tile
		// itself goes stale on (ADR 0068).
		"last_update_age_s":    tanksFeedAge(tanks),
		"fuel_volume_m3":       derivedValues[fuelVolumePath],
		"fuel_volume_age_s":    derivedAges[fuelVolumePath],
		"fuel_time_to_empty_s": derivedValues[fuelTimeToEmptyPath],
		"fuel_range_m":         derivedValues[fuelRangeAtCurrentBurnPath],
		// Oldest input age of the range and time figures specifically, not
		// volume's -- an operator watching the footer's range/time readouts
		// go stale wants to know how old the reason for the dash is, and
		// volume has its own age already exposed on fuel_volume_age_s.
		"fuel_derived_age_s": derivedInputAge(derivedAges[fuelTimeToEmptyPath], derivedAges[fuelRangeAtCurrentBurnPath]),
	}
}

func tanksState(c echo.Context) error {
	return c.JSON(http.StatusOK, buildTanksStatePayload())
}

// tanksRoutes returns the /api routes for the tank levels payload.
// Each row carries its own auth tier (docs/adr/0177).
func tanksRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodGet, "/api/tanks-state", tierRead, tanksState},
	}
}
