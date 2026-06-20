package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"gopkg.in/yaml.v3"
)

const (
	defaultDistanceUnits             = "metric"
	defaultVesselStateRefreshSeconds = 10
	defaultForecastRefreshSeconds    = 3600
	defaultBowRollerHeightM          = 1.5
	defaultChainSizeMM               = 12
	defaultChainOnboardM             = 150
	defaultHullType                  = "power_cat"
	defaultWindageAreaM2             = 35
)

type settingsPayload struct {
	Signalk struct {
		Address string `json:"address"`
		Port    int    `json:"port"`
	} `json:"signalk"`
	Boat struct {
		VesselPrefix           string  `json:"vessel_prefix"`
		Model                  string  `json:"model"`
		HouseBatteryCapacityAh float64 `json:"house_battery_capacity_ah"`
	} `json:"boat"`
	UI struct {
		VesselStateRefreshSeconds int               `json:"vessel_state_refresh_seconds"`
		ForecastRefreshSeconds    int               `json:"forecast_refresh_seconds"`
		TankLabels                map[string]string `json:"tank_labels"`
		TideProvider              string            `json:"tide_provider"`
		TideStationID             string            `json:"tide_station_id"`
		TideStationName           string            `json:"tide_station_name"`
		TideAutoStation           bool              `json:"tide_auto_station"`
	} `json:"ui"`
	Anchor struct {
		BowRollerHeightM float64 `json:"bow_roller_height_m"`
		ChainSizeMM      float64 `json:"chain_size_mm"`
		ChainOnboardM    float64 `json:"chain_onboard_m"`
		HullType         string  `json:"hull_type"`
		WindageAreaM2    float64 `json:"windage_area_m2"`
	} `json:"anchor"`
	Units string `json:"units"`
}

func getSettingsHandler(c echo.Context) error {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	settings, err := readSettings(settingsPath)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read settings"})
	}

	return c.JSON(http.StatusOK, buildSettingsPayload(settings))
}

func updateSettingsHandler(c echo.Context) error {
	var req settingsPayload
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
	}

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	settings, err := readSettings(settingsPath)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read settings"})
	}

	normalized := normalizeSettingsPayload(req)

	settings["signalk"] = map[string]any{
		"address": normalized.Signalk.Address,
		"port":    normalized.Signalk.Port,
	}
	settings["boat"] = map[string]any{
		"vessel_prefix":             normalized.Boat.VesselPrefix,
		"model":                     normalized.Boat.Model,
		"house_battery_capacity_ah": normalized.Boat.HouseBatteryCapacityAh,
	}

	uiMap := map[string]any{}
	if existingUI, ok := settings["ui"].(map[string]any); ok {
		for key, value := range existingUI {
			uiMap[key] = value
		}
	}
	uiMap["vessel_state_refresh_seconds"] = normalized.UI.VesselStateRefreshSeconds
	uiMap["forecast_refresh_seconds"] = normalized.UI.ForecastRefreshSeconds
	if normalized.UI.TankLabels != nil {
		uiMap["tank_labels"] = normalized.UI.TankLabels
	}
	uiMap["tide_provider"] = normalized.UI.TideProvider
	uiMap["tide_station_id"] = normalized.UI.TideStationID
	uiMap["tide_station_name"] = normalized.UI.TideStationName
	uiMap["tide_auto_station"] = normalized.UI.TideAutoStation
	settings["ui"] = uiMap

	settings["anchor"] = map[string]any{
		"bow_roller_height_m": normalized.Anchor.BowRollerHeightM,
		"chain_size_mm":       normalized.Anchor.ChainSizeMM,
		"chain_onboard_m":     normalized.Anchor.ChainOnboardM,
		"hull_type":           normalized.Anchor.HullType,
		"windage_area_m2":     normalized.Anchor.WindageAreaM2,
	}
	settings["units"] = normalized.Units

	if err := writeSettings(settingsPath, settings); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to save settings"})
	}

	return c.JSON(http.StatusOK, normalized)
}

func buildSettingsPayload(settings map[string]any) settingsPayload {
	payload := normalizeSettingsPayload(settingsPayload{})

	if signalkMap, ok := settings["signalk"].(map[string]any); ok {
		address := coerceString(signalkMap["address"])
		if strings.TrimSpace(address) != "" {
			payload.Signalk.Address = strings.TrimSpace(address)
		}
		port := coercePort(signalkMap["port"])
		if port > 0 && port <= 65535 {
			payload.Signalk.Port = port
		}
	}

	if boatMap, ok := settings["boat"].(map[string]any); ok {
		if prefix := strings.TrimSpace(coerceString(boatMap["vessel_prefix"])); prefix != "" {
			payload.Boat.VesselPrefix = prefix
		}
		if model := strings.TrimSpace(coerceString(boatMap["model"])); model != "" {
			payload.Boat.Model = model
		}
		capacity := coerceFloat(boatMap["house_battery_capacity_ah"])
		if capacity > 0 {
			payload.Boat.HouseBatteryCapacityAh = capacity
		}
	}

	if uiMap, ok := settings["ui"].(map[string]any); ok {
		seconds := coercePort(uiMap["vessel_state_refresh_seconds"])
		if seconds > 0 {
			payload.UI.VesselStateRefreshSeconds = seconds
		}

		forecastSeconds := coercePort(uiMap["forecast_refresh_seconds"])
		if forecastSeconds > 0 {
			payload.UI.ForecastRefreshSeconds = forecastSeconds
		}

		if labelsMap, ok := uiMap["tank_labels"].(map[string]any); ok {
			payload.UI.TankLabels = map[string]string{}
			for key, value := range labelsMap {
				trimmedKey := strings.TrimSpace(key)
				if trimmedKey == "" {
					continue
				}
				payload.UI.TankLabels[trimmedKey] = strings.TrimSpace(coerceString(value))
			}
		}

		if tideProvider := strings.TrimSpace(coerceString(uiMap["tide_provider"])); tideProvider != "" {
			if _, ok := getTideProvider(tideProvider); ok {
				payload.UI.TideProvider = tideProvider
			}
		}
		payload.UI.TideStationID = strings.TrimSpace(coerceString(uiMap["tide_station_id"]))
		payload.UI.TideStationName = strings.TrimSpace(coerceString(uiMap["tide_station_name"]))
		if v, ok := uiMap["tide_auto_station"].(bool); ok {
			payload.UI.TideAutoStation = v
		}
	}

	if anchorMap, ok := settings["anchor"].(map[string]any); ok {
		if value := coerceFloat(anchorMap["bow_roller_height_m"]); value > 0 {
			payload.Anchor.BowRollerHeightM = value
		}
		if value := coerceFloat(anchorMap["chain_size_mm"]); value > 0 {
			payload.Anchor.ChainSizeMM = value
		}
		if value := coerceFloat(anchorMap["chain_onboard_m"]); value > 0 {
			payload.Anchor.ChainOnboardM = value
		}
		if hullType := strings.TrimSpace(coerceString(anchorMap["hull_type"])); isSupportedHullType(hullType) {
			payload.Anchor.HullType = hullType
		}
		if value := coerceFloat(anchorMap["windage_area_m2"]); value > 0 {
			payload.Anchor.WindageAreaM2 = value
		}
	}

	units := strings.TrimSpace(coerceString(settings["units"]))
	if strings.EqualFold(units, "metric") || strings.EqualFold(units, "imperial") {
		payload.Units = strings.ToLower(units)
	}

	return payload
}

func normalizeSettingsPayload(req settingsPayload) settingsPayload {
	normalized := settingsPayload{}
	normalized.Signalk.Address = strings.TrimSpace(req.Signalk.Address)
	if normalized.Signalk.Address == "" {
		normalized.Signalk.Address = defaultSignalKAddress
	}
	normalized.Signalk.Port = req.Signalk.Port
	if normalized.Signalk.Port <= 0 || normalized.Signalk.Port > 65535 {
		normalized.Signalk.Port = defaultSignalKPort
	}

	normalized.Boat.VesselPrefix = strings.TrimSpace(req.Boat.VesselPrefix)
	if normalized.Boat.VesselPrefix == "" {
		normalized.Boat.VesselPrefix = "M/V"
	}
	normalized.Boat.Model = strings.TrimSpace(req.Boat.Model)
	normalized.Boat.HouseBatteryCapacityAh = req.Boat.HouseBatteryCapacityAh
	if normalized.Boat.HouseBatteryCapacityAh <= 0 {
		normalized.Boat.HouseBatteryCapacityAh = defaultHouseBatteryCapacityAh
	}

	normalized.UI.VesselStateRefreshSeconds = req.UI.VesselStateRefreshSeconds
	if normalized.UI.VesselStateRefreshSeconds <= 0 {
		normalized.UI.VesselStateRefreshSeconds = defaultVesselStateRefreshSeconds
	}

	normalized.UI.ForecastRefreshSeconds = req.UI.ForecastRefreshSeconds
	if normalized.UI.ForecastRefreshSeconds <= 0 {
		normalized.UI.ForecastRefreshSeconds = defaultForecastRefreshSeconds
	}

	if req.UI.TankLabels != nil {
		normalized.UI.TankLabels = map[string]string{}
		for key, value := range req.UI.TankLabels {
			trimmedKey := strings.TrimSpace(key)
			if trimmedKey == "" {
				continue
			}
			normalized.UI.TankLabels[trimmedKey] = strings.TrimSpace(value)
		}
	}

	normalized.UI.TideProvider = strings.TrimSpace(req.UI.TideProvider)
	if _, ok := getTideProvider(normalized.UI.TideProvider); !ok {
		normalized.UI.TideProvider = "stormglass"
	}
	normalized.UI.TideStationID = strings.TrimSpace(req.UI.TideStationID)
	normalized.UI.TideStationName = strings.TrimSpace(req.UI.TideStationName)
	normalized.UI.TideAutoStation = req.UI.TideAutoStation

	normalized.Anchor.BowRollerHeightM = req.Anchor.BowRollerHeightM
	if normalized.Anchor.BowRollerHeightM <= 0 {
		normalized.Anchor.BowRollerHeightM = defaultBowRollerHeightM
	}
	normalized.Anchor.ChainSizeMM = req.Anchor.ChainSizeMM
	if normalized.Anchor.ChainSizeMM <= 0 {
		normalized.Anchor.ChainSizeMM = defaultChainSizeMM
	}
	normalized.Anchor.ChainOnboardM = req.Anchor.ChainOnboardM
	if normalized.Anchor.ChainOnboardM <= 0 {
		normalized.Anchor.ChainOnboardM = defaultChainOnboardM
	}
	normalized.Anchor.WindageAreaM2 = req.Anchor.WindageAreaM2
	if normalized.Anchor.WindageAreaM2 <= 0 {
		normalized.Anchor.WindageAreaM2 = defaultWindageAreaM2
	}
	normalized.Anchor.HullType = strings.TrimSpace(req.Anchor.HullType)
	if !isSupportedHullType(normalized.Anchor.HullType) {
		normalized.Anchor.HullType = defaultHullType
	}

	normalized.Units = strings.ToLower(strings.TrimSpace(req.Units))
	if normalized.Units != "metric" && normalized.Units != "imperial" {
		normalized.Units = defaultDistanceUnits
	}

	return normalized
}

func isSupportedHullType(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed == "power_cat" || trimmed == "sail_mono" || trimmed == "power_mono" || trimmed == "sail_cat"
}

func getSignalKSettingsHandler(c echo.Context) error {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	return c.JSON(http.StatusOK, map[string]any{
		"address": address,
		"port":    port,
		"url":     buildSignalKURL(address, port),
	})
}

func updateSignalKSettingsHandler(c echo.Context) error {
	var req struct {
		Address string `json:"address"`
		Port    int    `json:"port"`
	}

	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
	}

	address := strings.TrimSpace(req.Address)
	if address == "" {
		address = defaultSignalKAddress
	}

	port := req.Port
	if port <= 0 || port > 65535 {
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)
	vesselPath := getEnv("SIGNALK_VESSEL_PATH", "/signalk/v1/api/vessels/self")

	if _, err := fetchSignalKVesselState(signalkURL, vesselPath); err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("unable to connect to SignalK at %s", signalkURL)})
	}

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	if err := saveSignalKSettings(settingsPath, address, port); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "connected to SignalK, but failed to persist settings"})
	}

	return c.JSON(http.StatusOK, map[string]any{"address": address, "port": port, "url": signalkURL, "connected": true})
}

// criticalVesselState marks state's position/GNSS fields as critical when
// there's no SignalK payload to validate at all — e.g. the connection itself
// failed. It freezes position at the last trusted fix (via resolveGNSSPosition)
// rather than leaving it at a meaningless sentinel, and reports
// gnss_critical_alert correctly so downstream consumers like the anchor watch
// alarm don't mistake "can't reach SignalK" for "vessel has moved."
func criticalVesselState(state vesselStateData, reason string) vesselStateData {
	validation := criticalGNSSValidation(reason, time.Now().UTC())
	state.Latitude, state.Longitude = resolveGNSSPosition(-1, -1, validation)
	state.GNSSQualityIndicator = validation.QualityIndicator
	state.GNSSHDOP = validation.HDOP
	state.GNSSValidationState = validation.Status
	state.GNSSValidationReason = validation.Reason
	state.GNSSCriticalAlert = validation.Critical
	return state
}

func fetchSignalKVesselState(signalkURL string, vesselPath string) (vesselStateData, error) {
	url := strings.TrimRight(signalkURL, "/") + "/" + strings.TrimLeft(vesselPath, "/")

	state := vesselStateData{Status: "Unknown", Datetime: time.Now().UTC(), Depth: -1, Latitude: -1, Longitude: -1, HeadingTrue: -1, SpeedOverGroundKts: -1, WindSpeedApparentKts: -1, WindAngleApparentDeg: -1, WindAngleRelativeDeg: -1}

	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		return criticalVesselState(state, fmt.Sprintf("signalk unreachable: %v", err)), err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		err := fmt.Errorf("signalk returned status %d", response.StatusCode)
		return criticalVesselState(state, err.Error()), err
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return criticalVesselState(state, fmt.Sprintf("signalk response unreadable: %v", err)), err
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return criticalVesselState(state, fmt.Sprintf("signalk response malformed: %v", err)), err
	}

	state.Name = strings.TrimSpace(firstNonEmptyString(lookupString(payload, "name"), lookupString(payload, "design", "name")))

	state.Status = firstNonEmptyString(lookupString(payload, "navigation", "state", "value"), lookupString(payload, "navigation", "state"))
	if state.Status == "" {
		state.Status = "Unknown"
	}

	hasGNSSDatetime := false
	datetimeString := firstNonEmptyString(lookupString(payload, "navigation", "datetime", "value"), lookupString(payload, "navigation", "datetime"), lookupString(payload, "timestamp"))
	if datetimeString != "" {
		parsed, err := time.Parse(time.RFC3339, datetimeString)
		if err == nil {
			state.Datetime = parsed.UTC()
			hasGNSSDatetime = true
		}
	}

	state.Depth = lookupNumber(payload, "environment", "depth", "belowTransducer", "value")
	if state.Depth == -1 {
		state.Depth = lookupNumber(payload, "environment", "depth", "belowTransducer")
	}

	currentDriftKts, currentSetDeg := parseSignalKCurrent(payload)
	state.CurrentDriftKts = currentDriftKts
	state.CurrentSetDeg = currentSetDeg

	rawLatitude := lookupNumber(payload, "navigation", "position", "value", "latitude")
	if rawLatitude == -1 {
		rawLatitude = lookupNumber(payload, "navigation", "position", "latitude")
	}

	rawLongitude := lookupNumber(payload, "navigation", "position", "value", "longitude")
	if rawLongitude == -1 {
		rawLongitude = lookupNumber(payload, "navigation", "position", "longitude")
	}

	validation := parseGNSSPositionValidation(payload)
	validation = applyGNSSHeuristics(validation, gnssObservedSample{
		Latitude:      rawLatitude,
		Longitude:     rawLongitude,
		DepthMeters:   state.Depth,
		Navigation:    state.Status,
		ObservedAt:    state.Datetime,
		HasObservedAt: hasGNSSDatetime,
	}, time.Now().UTC())
	state.GNSSQualityIndicator = validation.QualityIndicator
	state.GNSSHDOP = validation.HDOP
	state.GNSSValidationState = validation.Status
	state.GNSSValidationReason = validation.Reason
	state.GNSSCriticalAlert = validation.Critical

	state.Latitude, state.Longitude = resolveGNSSPosition(rawLatitude, rawLongitude, validation)

	state.HeadingTrue = lookupNumber(payload, "navigation", "headingTrue", "value")
	if state.HeadingTrue == -1 {
		state.HeadingTrue = lookupNumber(payload, "navigation", "headingTrue")
	}

	if state.HeadingTrue >= 0 {
		if state.HeadingTrue <= 2*math.Pi {
			state.HeadingTrue = state.HeadingTrue * 180 / math.Pi
		}
		state.HeadingTrue = normalizeDegrees(state.HeadingTrue)
	}

	windSpeedApparent := lookupNumber(payload, "environment", "wind", "speedApparent", "value")
	if windSpeedApparent == -1 {
		windSpeedApparent = lookupNumber(payload, "environment", "wind", "speedApparent")
	}
	windTimestamp := firstNonEmptyString(lookupString(payload, "environment", "wind", "speedApparent", "timestamp"), lookupString(payload, "environment", "wind", "angleApparent", "timestamp"), lookupString(payload, "environment", "wind", "timestamp"))

	windDataRecent := isRecentTimestamp(windTimestamp, defaultWindMaxAge)
	if !windDataRecent {
		windDataRecent = state.Datetime.After(time.Now().UTC().Add(-defaultWindMaxAge))
	}

	if windSpeedApparent >= 0 && windDataRecent {
		state.WindSpeedApparentKts = windSpeedApparent * metersPerSecondToKnots
	} else {
		state.WindSpeedApparentKts = 0
	}

	windAngleApparent := lookupNumber(payload, "environment", "wind", "angleApparent", "value")
	if windAngleApparent == -1 {
		windAngleApparent = lookupNumber(payload, "environment", "wind", "angleApparent")
	}
	if windAngleApparent != -1 && windDataRecent {
		if windAngleApparent >= -2*math.Pi && windAngleApparent <= 2*math.Pi {
			windAngleApparent = windAngleApparent * 180 / math.Pi
		}

		signedAngle := normalizeSignedDegrees(windAngleApparent)
		state.WindAngleApparentDeg = normalizeDegrees(signedAngle)
		state.WindAngleRelativeDeg = math.Abs(signedAngle)
		if signedAngle < 0 {
			state.WindSide = "port"
		} else {
			state.WindSide = "starboard"
		}
	} else {
		state.WindAngleApparentDeg = 0
		state.WindAngleRelativeDeg = 0
		state.WindSide = "starboard"
	}

	sog := lookupNumber(payload, "navigation", "speedOverGround", "value")
	if sog == -1 {
		sog = lookupNumber(payload, "navigation", "speedOverGround")
	}
	if sog >= 0 {
		state.SpeedOverGroundKts = math.Round(sog*metersPerSecondToKnots*10) / 10
	}

	// Generator state (com.victronenergy.generator.0 via signalk-venus-plugin)
	state.GeneratorState = firstNonEmptyString(
		lookupString(payload, "electrical", "generator", "0", "state", "value"),
		lookupString(payload, "electrical", "generator", "0", "state"),
	)

	if v, ok := lookupBool(payload, "electrical", "generator", "0", "manualStart", "value"); ok {
		state.GeneratorManualStart = v
	} else if v, ok := lookupBool(payload, "electrical", "generator", "0", "manualStart"); ok {
		state.GeneratorManualStart = v
	}

	genTimer := lookupNumber(payload, "electrical", "generator", "0", "manualStartTimer", "value")
	if genTimer == -1 {
		genTimer = lookupNumber(payload, "electrical", "generator", "0", "manualStartTimer")
	}
	if genTimer >= 0 {
		state.GeneratorManualStartTimer = genTimer
	}

	state.GeneratorRunningByCondition = firstNonEmptyString(
		lookupString(payload, "electrical", "generator", "0", "runningByCondition", "value"),
		lookupString(payload, "electrical", "generator", "0", "runningByCondition"),
	)

	genRuntime := lookupNumber(payload, "electrical", "generator", "0", "runtime", "value")
	if genRuntime == -1 {
		genRuntime = lookupNumber(payload, "electrical", "generator", "0", "runtime")
	}
	if genRuntime >= 0 {
		state.GeneratorRuntime = genRuntime
	}

	state.Engine0RPM = readEngineRPM(payload, []string{"0", "port", "main", "engine0", "engine-0"})
	state.Engine1RPM = readEngineRPM(payload, []string{"1", "starboard", "secondary", "engine1", "engine-1"})

	return state, nil
}

func readEngineRPM(payload map[string]any, aliases []string) float64 {
	for _, alias := range aliases {
		ts := firstNonEmptyString(
			lookupString(payload, "propulsion", alias, "revolutions", "timestamp"),
			lookupString(payload, "propulsion", alias, "rpm", "timestamp"),
			lookupString(payload, "propulsion", alias, "timestamp"),
		)
		if ts != "" && !isRecentTimestamp(ts, defaultRPMMaxAge) {
			return -1
		}

		revPerSecond := lookupFirstNumber(payload,
			[]string{"propulsion", alias, "revolutions", "value"},
			[]string{"propulsion", alias, "revolutions"},
		)
		if revPerSecond >= 0 {
			return roundTo1(revPerSecond * 60)
		}

		rpm := lookupFirstNumber(payload,
			[]string{"propulsion", alias, "rpm", "value"},
			[]string{"propulsion", alias, "rpm"},
		)
		if rpm >= 0 {
			return roundTo1(rpm)
		}
	}

	return -1
}

func parseSignalKCurrent(payload map[string]any) (float64, float64) {
	current := lookupAnyMap(payload, "environment", "current")
	if current == nil {
		return -1, -1
	}

	drift := lookupNumber(current, "drift", "value")
	if drift == -1 {
		drift = lookupNumber(current, "drift")
	}
	if drift == -1 {
		drift = lookupFirstNumber(current,
			[]string{"drift", "speed"},
			[]string{"drift", "speed", "value"},
			[]string{"drift", "value", "speed"},
		)
	}
	if drift >= 0 && drift <= 20 {
		drift = math.Round((drift*metersPerSecondToKnots)*10) / 10
	} else if drift < 0 {
		drift = -1
	}

	setDeg := lookupNumber(current, "set", "value")
	if setDeg == -1 {
		setDeg = lookupNumber(current, "set")
	}
	if setDeg == -1 {
		setDeg = lookupFirstNumber(current,
			[]string{"drift", "direction"},
			[]string{"drift", "direction", "value"},
			[]string{"drift", "angle"},
			[]string{"drift", "angle", "value"},
		)
	}
	if setDeg >= -2*math.Pi && setDeg <= 2*math.Pi {
		setDeg = normalizeDegrees(setDeg * 180 / math.Pi)
	} else if setDeg >= 0 {
		setDeg = normalizeDegrees(setDeg)
	} else {
		setDeg = -1
	}

	return drift, setDeg
}

func fetchSignalKElectricalState(signalkURL string, vesselPath string) (electricalStateData, error) {
	url := strings.TrimRight(signalkURL, "/") + "/" + strings.TrimLeft(vesselPath, "/")

	state := electricalStateData{Datetime: time.Now().UTC(), BatterySocPercent: -1, BatteryCapacityAh: -1, ChargingCurrentA: -1, ChargingPowerW: -1, SolarOutputW: -1, ACOutputW: -1, DC12VPowerW: -1, DC12VCurrentA: -1, DC24VVoltageV: -1, ACLoadsW: -1}

	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		return state, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return state, fmt.Errorf("signalk returned status %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return state, err
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return state, err
	}

	datetimeString := firstNonEmptyString(lookupString(payload, "timestamp"), lookupString(payload, "navigation", "datetime", "value"), lookupString(payload, "navigation", "datetime"))
	if datetimeString != "" {
		parsed, parseErr := time.Parse(time.RFC3339, datetimeString)
		if parseErr == nil {
			state.Datetime = parsed.UTC()
		}
	}

	// Identify the main house battery before reading any values from it.
	mainBattery := lookupMainBattery(payload)

	soc := -1.0
	if mainBattery != nil {
		soc = lookupNumber(mainBattery, "capacity", "stateOfCharge", "value")
	}
	if soc == -1 {
		soc = lookupFirstNumber(payload,
			[]string{"electrical", "batteries", "house", "capacity", "stateOfCharge", "value"},
			[]string{"electrical", "batteries", "house", "capacity", "stateOfCharge"},
			[]string{"electrical", "batteries", "service", "capacity", "stateOfCharge", "value"},
			[]string{"electrical", "batteries", "service", "capacity", "stateOfCharge"},
		)
	}
	if soc == -1 {
		soc = lookupFirstNumber(payload,
			[]string{"electrical", "chargers", "276", "capacity", "stateOfCharge", "value"},
		)
	}
	if soc == -1 {
		soc = lookupNumberFromAnyChild(payload, []string{"electrical", "chargers"}, []string{"capacity", "stateOfCharge", "value"})
	}
	if soc >= 0 {
		if soc <= 1 {
			soc *= 100
		}
		state.BatterySocPercent = math.Max(0, math.Min(100, roundTo1(soc)))
	}

	batteryCapacityAh := lookupFirstNumber(
		payload,
		[]string{"electrical", "batteries", "house", "capacity", "nominal", "value"},
		[]string{"electrical", "batteries", "house", "capacity", "nominal"},
		[]string{"electrical", "batteries", "service", "capacity", "nominal", "value"},
		[]string{"electrical", "batteries", "service", "capacity", "nominal"},
		[]string{"electrical", "batteries", "house", "capacity", "total", "value"},
		[]string{"electrical", "batteries", "house", "capacity", "total"},
		[]string{"electrical", "batteries", "service", "capacity", "total", "value"},
		[]string{"electrical", "batteries", "service", "capacity", "total"},
	)
	if batteryCapacityAh == -1 {
		batteryCapacityAh = lookupNumberFromAnyChild(payload, []string{"electrical", "batteries"}, []string{"capacity", "nominal", "value"})
	}
	if batteryCapacityAh == -1 {
		batteryCapacityAh = lookupNumberFromAnyChild(payload, []string{"electrical", "batteries"}, []string{"capacity", "total", "value"})
	}
	if batteryCapacityAh == -1 {
		batteryCapacityAh = loadHouseBatteryCapacityAh(getEnv("SETTINGS_FILE", "../settings.yaml"))
	}
	if batteryCapacityAh == -1 {
		batteryCapacityAh = defaultHouseBatteryCapacityAh
	}
	if batteryCapacityAh > 0 {
		state.BatteryCapacityAh = roundTo1(batteryCapacityAh)
	}

	// Identify the main house battery: the one with valid SOC and highest absolute current.
	batteryVoltage := -1.0
	if mainBattery != nil {
		batteryVoltage = lookupNumber(mainBattery, "voltage", "value")
	}
	if batteryVoltage == -1 {
		batteryVoltage = lookupFirstNumber(payload,
			[]string{"electrical", "venus", "batteryVoltage", "value"},
			[]string{"electrical", "venus", "batteryVoltage"},
			[]string{"electrical", "batteries", "house", "voltage", "value"},
			[]string{"electrical", "batteries", "house", "voltage"},
			[]string{"electrical", "batteries", "service", "voltage", "value"},
			[]string{"electrical", "batteries", "service", "voltage"},
		)
	}
	if batteryVoltage == -1 {
		batteryVoltage = lookupNumberFromAnyChild(payload, []string{"electrical", "batteries"}, []string{"voltage", "value"})
	}

	current := -1.0
	if mainBattery != nil {
		current = lookupNumber(mainBattery, "current", "value")
	}
	if current == -1 {
		current = lookupFirstNumber(payload,
			[]string{"electrical", "batteries", "house", "current", "value"},
			[]string{"electrical", "batteries", "house", "current"},
			[]string{"electrical", "batteries", "service", "current", "value"},
			[]string{"electrical", "batteries", "service", "current"},
		)
	}
	if current != -1 {
		state.ChargingCurrentA = roundTo1(current)
	}

	power := -1.0
	if mainBattery != nil {
		power = lookupNumber(mainBattery, "power", "value")
	}
	if power == -1 {
		power = lookupFirstNumber(payload,
			[]string{"electrical", "batteries", "house", "power", "value"},
			[]string{"electrical", "batteries", "house", "power"},
			[]string{"electrical", "batteries", "service", "power", "value"},
			[]string{"electrical", "batteries", "service", "power"},
		)
	}
	if power != -1 {
		state.ChargingPowerW = roundTo1(power)
	} else if state.ChargingCurrentA != -1 && batteryVoltage > 0 {
		state.ChargingPowerW = roundTo1(state.ChargingCurrentA * batteryVoltage)
	}
	if state.ChargingCurrentA == -1 && state.ChargingPowerW != -1 && batteryVoltage > 0 {
		state.ChargingCurrentA = roundTo1(state.ChargingPowerW / batteryVoltage)
	}

	// venus.totalPanelPower is the Victron system aggregate; sum individual chargers as fallback.
	solar := lookupFirstNumber(payload,
		[]string{"electrical", "venus", "totalPanelPower", "value"},
		[]string{"electrical", "venus", "totalPanelPower"},
	)
	if solar == -1 {
		solar = sumNumberFromAllChildren(payload, []string{"electrical", "solar"}, []string{"panelPower", "value"})
	}
	if solar >= 0 {
		state.SolarOutputW = roundTo1(solar)
	}

	acOutput := lookupFirstNumber(payload, []string{"electrical", "inverters", "0", "ac", "output", "power", "value"}, []string{"electrical", "inverters", "0", "ac", "output", "power"}, []string{"electrical", "inverters", "0", "acout", "power", "value"}, []string{"electrical", "inverters", "0", "acout", "power"}, []string{"electrical", "inverters", "0", "acOutputPower", "value"}, []string{"electrical", "inverters", "0", "acOutputPower"}, []string{"electrical", "alternators", "0", "ac", "output", "power", "value"}, []string{"electrical", "alternators", "0", "ac", "output", "power"})
	if acOutput == -1 {
		acOutput = lookupNumberFromAnyChild(payload, []string{"electrical", "inverters"}, []string{"acout", "power", "value"})
	}
	if acOutput >= 0 {
		state.ACOutputW = roundTo1(acOutput)
	}

	dc12Power := lookupFirstNumber(payload, []string{"electrical", "venus", "dcPower", "value"}, []string{"electrical", "venus", "dcPower"}, []string{"electrical", "dc", "12v", "power", "value"}, []string{"electrical", "dc", "12v", "power"}, []string{"electrical", "loads", "12v", "power", "value"}, []string{"electrical", "loads", "12v", "power"})
	if dc12Power >= 0 {
		state.DC12VPowerW = roundTo1(dc12Power)
	}

	dc12Current := lookupFirstNumber(payload, []string{"electrical", "venus", "dcCurrent", "value"}, []string{"electrical", "venus", "dcCurrent"}, []string{"electrical", "dc", "12v", "current", "value"}, []string{"electrical", "dc", "12v", "current"}, []string{"electrical", "loads", "12v", "current", "value"}, []string{"electrical", "loads", "12v", "current"})
	if dc12Current >= 0 {
		state.DC12VCurrentA = roundTo1(dc12Current)
	} else if state.DC12VPowerW >= 0 && batteryVoltage > 0 {
		state.DC12VCurrentA = roundTo1(state.DC12VPowerW / batteryVoltage)
	}

	dc24Voltage := lookupFirstNumber(payload, []string{"electrical", "dc", "24v", "voltage", "value"}, []string{"electrical", "dc", "24v", "voltage"}, []string{"electrical", "batteries", "starter", "voltage", "value"}, []string{"electrical", "batteries", "starter", "voltage"})
	if dc24Voltage >= 0 {
		state.DC24VVoltageV = roundTo1(dc24Voltage)
	} else if batteryVoltage >= 0 {
		state.DC24VVoltageV = roundTo1(batteryVoltage)
	}

	acLoads := lookupFirstNumber(payload, []string{"electrical", "ac", "loads", "total", "power", "value"}, []string{"electrical", "ac", "loads", "total", "power"}, []string{"electrical", "ac", "loads", "power", "value"}, []string{"electrical", "ac", "loads", "power"})
	if acLoads >= 0 {
		state.ACLoadsW = roundTo1(acLoads)
	} else if state.ACOutputW >= 0 {
		state.ACLoadsW = state.ACOutputW
	}

	generatorPower := lookupFirstNumber(payload,
		[]string{"electrical", "ac", "1", "phase", "A", "realPower", "value"},
		[]string{"electrical", "ac", "1", "phaseA", "realPower", "value"},
		[]string{"electrical", "ac", "1", "realPower", "value"},
	)
	if generatorPower >= 0 {
		state.GeneratorRealPowerW = roundTo1(generatorPower)
	}

	// Alternators — read port (index 0) and starboard (index 1) separately.
	state.Alternator0 = readAlternatorInstance(payload, "0")
	state.Alternator1 = readAlternatorInstance(payload, "1")

	return state, nil
}

func readAlternatorInstance(payload map[string]any, index string) alternatorInstanceData {
	inst := alternatorInstanceData{CurrentA: -1, VoltageV: -1, PowerW: -1, TempC: -1}

	current := lookupFirstNumber(payload,
		[]string{"electrical", "alternator", index, "current", "value"},
		[]string{"electrical", "alternator", index, "current"},
	)
	if current >= 0 {
		inst.CurrentA = roundTo1(current)
	}

	voltage := lookupFirstNumber(payload,
		[]string{"electrical", "alternator", index, "voltage", "value"},
		[]string{"electrical", "alternator", index, "voltage"},
	)
	if voltage >= 0 {
		inst.VoltageV = roundTo1(voltage)
	}

	power := lookupFirstNumber(payload,
		[]string{"electrical", "alternator", index, "power", "value"},
		[]string{"electrical", "alternator", index, "power"},
	)
	if power == -1 && inst.CurrentA >= 0 && inst.VoltageV > 0 {
		power = roundTo1(inst.CurrentA * inst.VoltageV)
	}
	if power >= 0 {
		inst.PowerW = roundTo1(power)
	}

	tempK := lookupFirstNumber(payload,
		[]string{"electrical", "alternator", index, "temperature", "value"},
		[]string{"electrical", "alternator", index, "temperature"},
	)
	if tempK >= 0 {
		inst.TempC = roundTo1(tempK - 273.15)
	}

	return inst
}

func fetchSignalKNearbyVessels(signalkURL string, vesselsPath string, selfLatitude float64, selfLongitude float64, now time.Time, excludedNames []string) ([]nearbyVessel, error) {
	url := strings.TrimRight(signalkURL, "/") + "/" + strings.TrimLeft(vesselsPath, "/")

	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("signalk returned status %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}

	vessels := make([]nearbyVessel, 0, len(payload))
	for vesselID, raw := range payload {
		vesselMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		if vesselID == "self" {
			continue
		}

		latitude := lookupNumber(vesselMap, "navigation", "position", "value", "latitude")
		if latitude == -1 {
			latitude = lookupNumber(vesselMap, "navigation", "position", "latitude")
		}

		longitude := lookupNumber(vesselMap, "navigation", "position", "value", "longitude")
		if longitude == -1 {
			longitude = lookupNumber(vesselMap, "navigation", "position", "longitude")
		}

		if latitude < -90 || latitude > 90 || longitude < -180 || longitude > 180 {
			continue
		}

		name := firstNonEmptyString(lookupString(vesselMap, "name"), lookupString(vesselMap, "design", "name"))
		if name == "" {
			name = compactVesselID(vesselID)
		}
		if matchesExcludedName(name, excludedNames) {
			continue
		}

		rangeFeet := int(math.Round(haversineMeters(selfLatitude, selfLongitude, latitude, longitude) * 3.28084))
		if rangeFeet < 30 || rangeFeet > 16404 { // ignore <30ft (self) and >5km
			continue
		}

		ageSeconds := 0
		timestamp := firstNonEmptyString(lookupString(vesselMap, "navigation", "position", "timestamp"), lookupString(vesselMap, "navigation", "position", "value", "timestamp"), lookupString(vesselMap, "timestamp"))
		if timestamp != "" {
			parsed, parseErr := time.Parse(time.RFC3339, timestamp)
			if parseErr == nil {
				delta := int(now.Sub(parsed.UTC()).Seconds())
				if delta > 0 {
					ageSeconds = delta
				}
			}
		}

		var sogKnots *float64
		sog := lookupNumber(vesselMap, "navigation", "speedOverGround", "value")
		if sog >= 0 {
			knots := math.Round((sog*1.943844)*10) / 10
			sogKnots = &knots
		}

		vessels = append(vessels, nearbyVessel{Name: strings.ToUpper(name), RangeFt: rangeFeet, AgeSeconds: ageSeconds, SogKnots: sogKnots, Lat: latitude, Lon: longitude})
	}

	sort.Slice(vessels, func(i int, j int) bool { return vessels[i].RangeFt < vessels[j].RangeFt })
	if len(vessels) > 10 {
		vessels = vessels[:10]
	}

	return vessels, nil
}

func fetchSignalKVesselNameMap(signalkURL string, vesselsPath string) (map[string]string, error) {
	url := strings.TrimRight(signalkURL, "/") + "/" + strings.TrimLeft(vesselsPath, "/")

	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("signalk returned status %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}

	names := make(map[string]string, len(payload))
	for vesselID, raw := range payload {
		vesselMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		name := firstNonEmptyString(lookupString(vesselMap, "name"), lookupString(vesselMap, "design", "name"))
		if name == "" {
			name = compactVesselID(vesselID)
		}
		names[vesselID] = strings.ToUpper(strings.TrimSpace(name))
	}

	return names, nil
}

func fetchSignalKTanksState(signalkURL string, vesselPath string, labelOverrides map[string]string) ([]tankLevelData, time.Time, error) {
	url := strings.TrimRight(signalkURL, "/") + "/" + strings.TrimLeft(vesselPath, "/")

	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		return nil, time.Now().UTC(), err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, time.Now().UTC(), fmt.Errorf("signalk returned status %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, time.Now().UTC(), err
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, time.Now().UTC(), err
	}

	datetime := time.Now().UTC()
	datetimeString := firstNonEmptyString(lookupString(payload, "navigation", "datetime", "value"), lookupString(payload, "navigation", "datetime"), lookupString(payload, "timestamp"))
	if datetimeString != "" {
		parsed, parseErr := time.Parse(time.RFC3339, datetimeString)
		if parseErr == nil {
			datetime = parsed.UTC()
		}
	}

	tanksMap := lookupAnyMap(payload, "tanks")
	if tanksMap == nil {
		return []tankLevelData{}, datetime, nil
	}

	categoryOrder := []string{"freshWater", "fuel", "blackWater", "greyWater", "liveWell", "lubrication", "water", "wasteWater"}
	knownCategory := map[string]struct{}{}
	for _, category := range categoryOrder {
		knownCategory[category] = struct{}{}
	}

	orderedCategories := make([]string, 0, len(tanksMap))
	for _, category := range categoryOrder {
		if _, ok := tanksMap[category]; ok {
			orderedCategories = append(orderedCategories, category)
		}
	}
	for category := range tanksMap {
		if _, ok := knownCategory[category]; ok {
			continue
		}
		orderedCategories = append(orderedCategories, category)
	}

	tanks := make([]tankLevelData, 0)
	for _, category := range orderedCategories {
		categoryRaw, ok := tanksMap[category]
		if !ok {
			continue
		}

		categoryEntries, ok := categoryRaw.(map[string]any)
		if !ok {
			continue
		}

		entryIDs := make([]string, 0, len(categoryEntries))
		for entryID := range categoryEntries {
			entryIDs = append(entryIDs, entryID)
		}
		sort.Strings(entryIDs)

		for _, entryID := range entryIDs {
			rawEntry := categoryEntries[entryID]
			entry, ok := rawEntry.(map[string]any)
			if !ok {
				continue
			}

			level := lookupFirstNumber(entry, []string{"currentLevel", "value"}, []string{"currentLevel"})
			if level < 0 {
				continue
			}

			if level <= 1 {
				level *= 100
			}
			level = math.Max(0, math.Min(100, roundTo1(level)))

			label := tankLabelOverride(labelOverrides, category, entryID)
			if label == "" {
				continue
			}

			tanks = append(tanks, tankLevelData{ID: category + "." + entryID, Label: label, Category: category, Kind: tankKindFromCategory(category), LevelPercent: level})
		}
	}

	return tanks, datetime, nil
}

func loadTankLabelOverrides(settingsPath string) map[string]string {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return map[string]string{}
	}

	uiMap, ok := settings["ui"].(map[string]any)
	if !ok {
		return map[string]string{}
	}

	rawLabels, ok := uiMap["tank_labels"].(map[string]any)
	if !ok {
		return map[string]string{}
	}

	labels := map[string]string{}
	for key, value := range rawLabels {
		label, ok := value.(string)
		if !ok {
			continue
		}

		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		normalizedLabel := strings.TrimSpace(label)
		if normalizedKey == "" || normalizedLabel == "" {
			continue
		}

		labels[normalizedKey] = normalizedLabel
	}

	return labels
}

func tankLabelOverride(overrides map[string]string, category string, entryID string) string {
	if len(overrides) == 0 {
		return ""
	}

	category = strings.ToLower(strings.TrimSpace(category))
	entryID = strings.TrimSpace(entryID)

	keys := []string{category + "." + strings.ToLower(entryID), category + "/" + strings.ToLower(entryID), strings.ToLower(entryID)}
	for _, key := range keys {
		if value, ok := overrides[key]; ok {
			return value
		}
	}

	return ""
}

func buildTankLabel(category string, entryID string) string {
	base := humanizeCategory(category)
	if strings.TrimSpace(entryID) == "" {
		return base
	}

	return fmt.Sprintf("%s %s", base, strings.ToUpper(strings.TrimSpace(entryID)))
}

func tankKindFromCategory(category string) string {
	normalized := strings.ToLower(strings.TrimSpace(category))
	if strings.Contains(normalized, "fuel") {
		return "fuel"
	}

	if strings.Contains(normalized, "black") || strings.Contains(normalized, "grey") || strings.Contains(normalized, "waste") || strings.Contains(normalized, "sewage") || strings.Contains(normalized, "holding") {
		return "waste"
	}

	return "water"
}

func humanizeCategory(category string) string {
	if strings.TrimSpace(category) == "" {
		return "Tank"
	}

	r := strings.NewReplacer("freshWater", "Fresh Water", "blackWater", "Black Water", "greyWater", "Grey Water", "wasteWater", "Waste Water", "liveWell", "Live Well")
	converted := r.Replace(category)

	if strings.EqualFold(converted, category) {
		converted = strings.ReplaceAll(converted, "_", " ")
	}

	parts := strings.Fields(converted)
	for i, part := range parts {
		if len(part) == 0 {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
	}

	return strings.Join(parts, " ")
}

func lookupString(payload map[string]any, keys ...string) string {
	var current any = payload
	for _, key := range keys {
		asMap, ok := current.(map[string]any)
		if !ok {
			return ""
		}

		next, ok := asMap[key]
		if !ok {
			return ""
		}

		current = next
	}

	value, ok := current.(string)
	if !ok {
		return ""
	}

	return value
}

func lookupNumber(payload map[string]any, keys ...string) float64 {
	var current any = payload
	for _, key := range keys {
		asMap, ok := current.(map[string]any)
		if !ok {
			return -1
		}

		next, ok := asMap[key]
		if !ok {
			return -1
		}

		current = next
	}

	switch v := current.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	default:
		return -1
	}
}

func lookupBool(payload map[string]any, keys ...string) (bool, bool) {
	var current any = payload
	for _, key := range keys {
		asMap, ok := current.(map[string]any)
		if !ok {
			return false, false
		}
		next, ok := asMap[key]
		if !ok {
			return false, false
		}
		current = next
	}
	switch v := current.(type) {
	case bool:
		return v, true
	case float64:
		return v != 0, true
	}
	return false, false
}

func lookupFirstNumber(payload map[string]any, paths ...[]string) float64 {
	for _, path := range paths {
		value := lookupNumber(payload, path...)
		if value != -1 {
			return value
		}
	}

	return -1
}

func lookupNumberFromAnyChild(payload map[string]any, prefix []string, suffix []string) float64 {
	parent := lookupAnyMap(payload, prefix...)
	if parent == nil {
		return -1
	}

	for _, rawChild := range parent {
		child, ok := rawChild.(map[string]any)
		if !ok {
			continue
		}

		value := lookupNumber(child, suffix...)
		if value != -1 {
			return value
		}
	}

	return -1
}

// sumNumberFromAllChildren sums a numeric field across all children of a map node.
// Returns -1 if no children have the field.
func sumNumberFromAllChildren(payload map[string]any, prefix []string, suffix []string) float64 {
	parent := lookupAnyMap(payload, prefix...)
	if parent == nil {
		return -1
	}

	sum := 0.0
	found := false
	for _, rawChild := range parent {
		child, ok := rawChild.(map[string]any)
		if !ok {
			continue
		}
		value := lookupNumber(child, suffix...)
		if value != -1 {
			sum += value
			found = true
		}
	}

	if !found {
		return -1
	}
	return sum
}

// lookupMainBattery returns the battery sub-object with valid SOC data and the
// highest absolute current — i.e. the actively monitored house bank.
func lookupMainBattery(payload map[string]any) map[string]any {
	batteries := lookupAnyMap(payload, "electrical", "batteries")
	if batteries == nil {
		return nil
	}

	var best map[string]any
	bestAbsCurrent := -1.0

	for _, rawBattery := range batteries {
		battery, ok := rawBattery.(map[string]any)
		if !ok {
			continue
		}

		soc := lookupNumber(battery, "capacity", "stateOfCharge", "value")
		if soc == -1 || soc < 0 || soc > 1.01 {
			continue
		}

		current := lookupNumber(battery, "current", "value")
		if current == -1 {
			continue
		}

		if absCurrent := math.Abs(current); absCurrent > bestAbsCurrent {
			bestAbsCurrent = absCurrent
			best = battery
		}
	}

	return best
}

func lookupAnyMap(payload map[string]any, keys ...string) map[string]any {
	var current any = payload
	for _, key := range keys {
		asMap, ok := current.(map[string]any)
		if !ok {
			return nil
		}

		next, ok := asMap[key]
		if !ok {
			return nil
		}

		current = next
	}

	result, ok := current.(map[string]any)
	if !ok {
		return nil
	}

	return result
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}

// ── SignalK authentication token cache ───────────────────────────────────────

type cachedToken struct {
	token     string
	expiresAt time.Time
}

var skTokenMu sync.Mutex
var skTokenCache *cachedToken

// loadSignalKCredentials reads username/password from environment variables.
func loadSignalKCredentials(_ string) (username, password string) {
	username = getEnv("SIGNALK_USERNAME", "")
	password = getEnv("SIGNALK_PASSWORD", "")
	return
}

// acquireSignalKToken returns a cached JWT or fetches a fresh one.
func acquireSignalKToken(signalkURL, username, password string) (string, error) {
	if username == "" || password == "" {
		return "", nil
	}

	skTokenMu.Lock()
	defer skTokenMu.Unlock()

	if skTokenCache != nil && time.Now().Before(skTokenCache.expiresAt) {
		return skTokenCache.token, nil
	}

	url := strings.TrimRight(signalkURL, "/") + "/signalk/v1/auth/login"
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("signalk auth: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("signalk auth returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Token      string  `json:"token"`
		TimeToLive float64 `json:"timeToLive"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil || result.Token == "" {
		return "", fmt.Errorf("signalk auth: could not parse token response")
	}

	ttl := result.TimeToLive
	if ttl <= 0 {
		ttl = 86400 // default 24h
	}
	skTokenCache = &cachedToken{
		token:     result.Token,
		expiresAt: time.Now().Add(time.Duration(ttl-60) * time.Second),
	}
	return result.Token, nil
}

// invalidateSignalKToken clears the cached token so the next call re-authenticates.
func invalidateSignalKToken() {
	skTokenMu.Lock()
	skTokenCache = nil
	skTokenMu.Unlock()
}

func buildSignalKURL(address string, port int) string {
	trimmed := strings.TrimSpace(address)
	if trimmed == "" {
		trimmed = defaultSignalKAddress
	}

	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return strings.TrimRight(trimmed, "/")
	}

	if port <= 0 || port > 65535 {
		port = defaultSignalKPort
	}

	return fmt.Sprintf("http://%s:%d", trimmed, port)
}

func loadSignalKSettings(settingsPath string) (string, int, error) {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return "", 0, err
	}

	signalkMap, ok := settings["signalk"].(map[string]any)
	if !ok {
		return defaultSignalKAddress, defaultSignalKPort, nil
	}

	address, _ := signalkMap["address"].(string)
	if strings.TrimSpace(address) == "" {
		address = defaultSignalKAddress
	}

	port := coercePort(signalkMap["port"])
	if port <= 0 {
		port = defaultSignalKPort
	}

	return address, port, nil
}

func saveSignalKSettings(settingsPath string, address string, port int) error {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return err
	}

	signalkMap := map[string]any{"address": strings.TrimSpace(address), "port": port}
	settings["signalk"] = signalkMap

	return writeSettings(settingsPath, settings)
}

func writeSettings(settingsPath string, settings map[string]any) error {

	content, err := yaml.Marshal(settings)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}

	return os.WriteFile(settingsPath, content, 0o644)
}

func readSettings(settingsPath string) (map[string]any, error) {
	content, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}

		return nil, err
	}

	if len(content) == 0 {
		return map[string]any{}, nil
	}

	settings := map[string]any{}
	if err := yaml.Unmarshal(content, &settings); err != nil {
		return nil, err
	}

	return settings, nil
}

func coercePort(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		var parsed int
		if _, err := fmt.Sscanf(strings.TrimSpace(typed), "%d", &parsed); err == nil {
			return parsed
		}
	}

	return 0
}

func coerceString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	}

	return ""
}

func coerceFloat(value any) float64 {
	switch typed := value.(type) {
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case float64:
		return typed
	case string:
		var parsed float64
		if _, err := fmt.Sscanf(strings.TrimSpace(typed), "%f", &parsed); err == nil {
			return parsed
		}
	}

	return -1
}

// loadSettingString retrieves a string value from nested settings by path (e.g., []string{"boat", "name"}).
// Returns empty string on error or missing path.
func loadSettingString(settingsPath string, keyPath []string) string {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return ""
	}

	value := any(settings)
	for _, key := range keyPath {
		m, ok := value.(map[string]any)
		if !ok {
			return ""
		}
		value = m[key]
	}

	return strings.TrimSpace(coerceString(value))
}

// loadSettingFloat retrieves a float64 value from nested settings by path.
// Returns -1 on error or missing path.
func loadSettingFloat(settingsPath string, keyPath []string) float64 {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return -1
	}

	value := any(settings)
	for _, key := range keyPath {
		m, ok := value.(map[string]any)
		if !ok {
			return -1
		}
		value = m[key]
	}

	return coerceFloat(value)
}

func normalizeDegrees(value float64) float64 {
	normalized := math.Mod(value, 360)
	if normalized < 0 {
		normalized += 360
	}

	return normalized
}

func normalizeSignedDegrees(value float64) float64 {
	normalized := normalizeDegrees(value)
	if normalized > 180 {
		normalized -= 360
	}

	return normalized
}

func roundTo1(value float64) float64 { return math.Round(value*10) / 10 }

func haversineMeters(lat1 float64, lon1 float64, lat2 float64, lon2 float64) float64 {
	const earthRadiusMeters = 6371000.0
	lat1Rad := lat1 * math.Pi / 180
	lon1Rad := lon1 * math.Pi / 180
	lat2Rad := lat2 * math.Pi / 180
	lon2Rad := lon2 * math.Pi / 180
	deltaLat := lat2Rad - lat1Rad
	deltaLon := lon2Rad - lon1Rad
	a := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) + math.Cos(lat1Rad)*math.Cos(lat2Rad)*math.Sin(deltaLon/2)*math.Sin(deltaLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusMeters * c
}

func compactVesselID(vesselID string) string {
	trimmed := strings.TrimSpace(vesselID)
	if trimmed == "" {
		return "UNKNOWN"
	}
	segments := strings.Split(trimmed, ":")
	return segments[len(segments)-1]
}

func loadBoatVesselPrefix(settingsPath string) string {
	return loadSettingString(settingsPath, []string{"boat", "name"})
}

func loadHouseBatteryCapacityAh(settingsPath string) float64 {
	capacity := loadSettingFloat(settingsPath, []string{"boat", "house_battery_capacity_ah"})
	if capacity <= 0 {
		return -1
	}
	return capacity
}

func fetchSignalKSelfName(signalkURL string, vesselPath string) string {
	url := strings.TrimRight(signalkURL, "/") + "/" + strings.TrimLeft(vesselPath, "/")
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		return ""
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(firstNonEmptyString(lookupString(payload, "name"), lookupString(payload, "design", "name")))
}

func matchesExcludedName(candidate string, excludedNames []string) bool {
	trimmedCandidate := strings.TrimSpace(candidate)
	if trimmedCandidate == "" {
		return false
	}
	for _, excluded := range excludedNames {
		if excluded != "" && strings.EqualFold(trimmedCandidate, strings.TrimSpace(excluded)) {
			return true
		}
	}
	return false
}
