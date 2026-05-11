package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"gopkg.in/yaml.v3"
)

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

func fetchSignalKVesselState(signalkURL string, vesselPath string) (vesselStateData, error) {
	url := strings.TrimRight(signalkURL, "/") + "/" + strings.TrimLeft(vesselPath, "/")

	state := vesselStateData{Status: "Unknown", Datetime: time.Now().UTC(), Depth: -1, Latitude: -1, Longitude: -1, HeadingTrue: -1, WindSpeedApparentKts: -1, WindAngleApparentDeg: -1, WindAngleRelativeDeg: -1}

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

	state.Status = firstNonEmptyString(lookupString(payload, "navigation", "state", "value"), lookupString(payload, "navigation", "state"))
	if state.Status == "" {
		state.Status = "Unknown"
	}

	datetimeString := firstNonEmptyString(lookupString(payload, "navigation", "datetime", "value"), lookupString(payload, "navigation", "datetime"), lookupString(payload, "timestamp"))
	if datetimeString != "" {
		parsed, err := time.Parse(time.RFC3339, datetimeString)
		if err == nil {
			state.Datetime = parsed.UTC()
		}
	}

	state.Depth = lookupNumber(payload, "environment", "depth", "belowTransducer", "value")
	if state.Depth == -1 {
		state.Depth = lookupNumber(payload, "environment", "depth", "belowTransducer")
	}

	state.Latitude = lookupNumber(payload, "navigation", "position", "value", "latitude")
	if state.Latitude == -1 {
		state.Latitude = lookupNumber(payload, "navigation", "position", "latitude")
	}

	state.Longitude = lookupNumber(payload, "navigation", "position", "value", "longitude")
	if state.Longitude == -1 {
		state.Longitude = lookupNumber(payload, "navigation", "position", "longitude")
	}

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

	return state, nil
}

func fetchSignalKElectricalState(signalkURL string, vesselPath string) (electricalStateData, error) {
	url := strings.TrimRight(signalkURL, "/") + "/" + strings.TrimLeft(vesselPath, "/")

	state := electricalStateData{Datetime: time.Now().UTC(), BatterySocPercent: -1, ChargingCurrentA: -1, ChargingPowerW: -1, SolarOutputW: -1, ACOutputW: -1, DC12VPowerW: -1, DC12VCurrentA: -1, DC24VVoltageV: -1, ACLoadsW: -1}

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

	soc := lookupFirstNumber(payload, []string{"electrical", "batteries", "house", "capacity", "stateOfCharge", "value"}, []string{"electrical", "batteries", "house", "capacity", "stateOfCharge"}, []string{"electrical", "batteries", "service", "capacity", "stateOfCharge", "value"}, []string{"electrical", "batteries", "service", "capacity", "stateOfCharge"})
	if soc == -1 {
		soc = lookupNumberFromAnyChild(payload, []string{"electrical", "batteries"}, []string{"capacity", "stateOfCharge", "value"})
	}
	if soc >= 0 {
		if soc <= 1 {
			soc *= 100
		}
		state.BatterySocPercent = math.Max(0, math.Min(100, roundTo1(soc)))
	}

	batteryVoltage := lookupFirstNumber(payload, []string{"electrical", "venus", "batteryVoltage", "value"}, []string{"electrical", "venus", "batteryVoltage"}, []string{"electrical", "batteries", "house", "voltage", "value"}, []string{"electrical", "batteries", "house", "voltage"}, []string{"electrical", "batteries", "service", "voltage", "value"}, []string{"electrical", "batteries", "service", "voltage"})
	if batteryVoltage == -1 {
		batteryVoltage = lookupNumberFromAnyChild(payload, []string{"electrical", "batteries"}, []string{"voltage", "value"})
	}

	current := lookupFirstNumber(payload, []string{"electrical", "batteries", "house", "current", "value"}, []string{"electrical", "batteries", "house", "current"}, []string{"electrical", "batteries", "service", "current", "value"}, []string{"electrical", "batteries", "service", "current"})
	if current == -1 {
		current = lookupNumberFromAnyChild(payload, []string{"electrical", "batteries"}, []string{"current", "value"})
	}
	if current == -1 {
		state.ChargingCurrentA = -1
	} else if current >= 0 {
		state.ChargingCurrentA = roundTo1(current)
	} else {
		state.ChargingCurrentA = 0
	}

	power := lookupFirstNumber(payload, []string{"electrical", "batteries", "house", "power", "value"}, []string{"electrical", "batteries", "house", "power"}, []string{"electrical", "batteries", "service", "power", "value"}, []string{"electrical", "batteries", "service", "power"})
	if power == -1 {
		power = lookupNumberFromAnyChild(payload, []string{"electrical", "batteries"}, []string{"power", "value"})
	}
	if power == -1 {
		state.ChargingPowerW = -1
	} else if power >= 0 {
		state.ChargingPowerW = roundTo1(power)
	} else {
		state.ChargingPowerW = 0
	}

	if state.ChargingPowerW == -1 && state.ChargingCurrentA >= 0 && batteryVoltage > 0 {
		state.ChargingPowerW = roundTo1(state.ChargingCurrentA * batteryVoltage)
	}
	if state.ChargingCurrentA == -1 && state.ChargingPowerW >= 0 && batteryVoltage > 0 {
		state.ChargingCurrentA = roundTo1(state.ChargingPowerW / batteryVoltage)
	}

	solar := lookupFirstNumber(payload, []string{"electrical", "solar", "0", "panelPower", "value"}, []string{"electrical", "solar", "0", "panelPower"}, []string{"electrical", "solar", "0", "power", "value"}, []string{"electrical", "solar", "0", "power"}, []string{"electrical", "solar", "panelPower", "value"}, []string{"electrical", "solar", "panelPower"})
	if solar == -1 {
		solar = lookupNumberFromAnyChild(payload, []string{"electrical", "solar"}, []string{"panelPower", "value"})
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

	return state, nil
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
		if rangeFeet < 30 {
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

		vessels = append(vessels, nearbyVessel{Name: strings.ToUpper(name), RangeFt: rangeFeet, AgeSeconds: ageSeconds, SogKnots: sogKnots})
	}

	sort.Slice(vessels, func(i int, j int) bool { return vessels[i].RangeFt < vessels[j].RangeFt })
	if len(vessels) > 10 {
		vessels = vessels[:10]
	}

	return vessels, nil
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

			label := firstNonEmptyString(lookupString(entry, "name", "value"), lookupString(entry, "name"), lookupString(entry, "displayName", "value"), lookupString(entry, "displayName"))
			override := tankLabelOverride(labelOverrides, category, entryID)
			if override != "" {
				label = override
			}
			if label == "" {
				label = buildTankLabel(category, entryID)
			}

			tanks = append(tanks, tankLevelData{ID: category + "." + entryID, Label: strings.TrimSpace(label), Category: category, Kind: tankKindFromCategory(category), LevelPercent: level})
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

func loadBoatName(settingsPath string) string {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return ""
	}
	boatMap, ok := settings["boat"].(map[string]any)
	if !ok {
		return ""
	}
	name, _ := boatMap["name"].(string)
	return strings.TrimSpace(name)
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
