package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
)

/*
The sensor-health checks' own exclusion list (anomaly_sensor_health.go's
frozenVerdict/silentSources), reached from the frozen/impossible/silent-
source alarm card's own "Ignore this sensor" action -- not a settings list,
per the plan: "The frozen/impossible alarm card gets an Ignore this sensor
action, stored with the alarm rules in data/. Pikorua's dead exhaust senders
are excluded that way, on the boat."

An identifier is either a SignalK path (excludes it from the frozen check)
or a $source id (excludes it from the silent-source check) -- the same
persisted list serves both, since computeAnomalyReading (anomaly_detector.go)
reads it once per tick and hands the same set to each check as its
`excluded` parameter. Nothing here validates which kind an identifier is:
a path that happens to also match a $source string, or vice versa, simply
never matches the other check's own lookups, so there is nothing to
disambiguate.
*/

// ignoredSensorSet returns a copy of the currently ignored identifiers as a
// set, the shape frozenVerdict/silentSources' own excluded parameter wants.
func ignoredSensorSet() map[string]bool {
	alarmRulesMu.RLock()
	defer alarmRulesMu.RUnlock()

	out := make(map[string]bool, len(alarmRulesIgnoredSensors))
	for _, s := range alarmRulesIgnoredSensors {
		out[s] = true
	}
	return out
}

// listIgnoredSensors returns a copy of the raw list, for the API response.
func listIgnoredSensors() []string {
	alarmRulesMu.RLock()
	defer alarmRulesMu.RUnlock()
	return append([]string(nil), alarmRulesIgnoredSensors...)
}

// ignoreSensor adds identifier to the ignore list and persists it.
// Idempotent: ignoring an already-ignored identifier is a no-op, not an
// error, since the operator may click the card's action more than once.
func ignoreSensor(identifier string) error {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return fmt.Errorf("identifier required")
	}

	alarmRulesMu.Lock()
	defer alarmRulesMu.Unlock()

	for _, s := range alarmRulesIgnoredSensors {
		if s == identifier {
			return nil
		}
	}
	alarmRulesIgnoredSensors = append(alarmRulesIgnoredSensors, identifier)
	return saveAlarmRulesLocked()
}

// unignoreSensor removes identifier from the ignore list. Also idempotent:
// removing an identifier that was never ignored is a no-op.
func unignoreSensor(identifier string) error {
	identifier = strings.TrimSpace(identifier)

	alarmRulesMu.Lock()
	defer alarmRulesMu.Unlock()

	next := make([]string, 0, len(alarmRulesIgnoredSensors))
	for _, s := range alarmRulesIgnoredSensors {
		if s != identifier {
			next = append(next, s)
		}
	}
	alarmRulesIgnoredSensors = next
	return saveAlarmRulesLocked()
}

// ignoredSensorsResponse is the body of every ignored-sensors endpoint:
// the raw identifiers (what DELETE takes) and, for each, the name the
// operator knows it by -- the settings list shows names and never an id.
func ignoredSensorsResponse() map[string]any {
	identifiers := listIgnoredSensors()
	namer := newSensorNamer(globalSignalKSnapshot, vesselSettingsForNaming(), globalSourceDevices.get())
	return map[string]any{"identifiers": identifiers, "sensors": ignoredSensorEntries(namer, identifiers)}
}

// ignoredSensorEntries names each identifier, numbering identical names the
// way the alarm card does so the rows can be told apart.
func ignoredSensorEntries(namer *sensorNamer, identifiers []string) []sensorHealthEntry {
	sensors := make([]sensorHealthEntry, 0, len(identifiers))
	for _, id := range identifiers {
		sensors = append(sensors, namer.identifierEntry(id))
	}
	return disambiguateSensorEntries(sensors)
}

// vesselSettingsForNaming loads the vessel block for naming only. A block
// that cannot be read is logged and names fall back to the bus's own: this
// is a label, and the settings page shows the read error itself.
func vesselSettingsForNaming() vesselSettings {
	vessel, err := loadVesselSettings(getEnv("SETTINGS_FILE", "../settings.yaml"))
	if err != nil {
		log.Printf("sensor names: vessel settings unreadable, using the bus's own names: %v", err)
		return vesselSettings{}
	}
	return vessel
}

// GET /api/alarms/ignored-sensors
func listIgnoredSensorsHandler(c echo.Context) error {
	return c.JSON(http.StatusOK, ignoredSensorsResponse())
}

// POST /api/alarms/ignored-sensors — the alarm card's "Ignore this sensor" action.
func ignoreSensorHandler(c echo.Context) error {
	var req struct {
		Identifier string `json:"identifier"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
	}
	if err := ignoreSensor(req.Identifier); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, ignoredSensorsResponse())
}

// DELETE /api/alarms/ignored-sensors/:identifier
func unignoreSensorHandler(c echo.Context) error {
	// Echo prefers URL.RawPath, so a param arrives still percent-encoded --
	// the same trap alarm_service.go's alarmActionHandler and
	// engine_profiles.go's parseProfileID already unescape against. The
	// client sends encodeURIComponent(identifier) (use-ignored-sensors.ts),
	// and a $source id or SignalK path containing a colon left escaped
	// matches nothing in the ignore list, so the identifier is never
	// actually removed (code review finding 9).
	identifier, err := url.PathUnescape(c.Param("identifier"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "malformed identifier"})
	}
	if err := unignoreSensor(identifier); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}
