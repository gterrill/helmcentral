package main

import (
	"log"
	"time"
)

func startTideAutoUpdater(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			updateNearestTideStation()
		}
	}()
}

func updateNearestTideStation() {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	settings, err := readSettings(settingsPath)
	if err != nil {
		return
	}

	payload := buildSettingsPayload(settings)
	if !payload.UI.TideAutoStation {
		return
	}

	provider, ok := getTideProvider(payload.UI.TideProvider)
	if !ok {
		return
	}

	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}
	signalkURL := buildSignalKURL(address, port)
	vesselPath := getEnv("SIGNALK_VESSEL_PATH", "/signalk/v1/api/vessels/self")
	vesselState, err := fetchSignalKVesselState(signalkURL, vesselPath)
	if err != nil || vesselState.Latitude < -90 || vesselState.Latitude > 90 {
		return
	}

	station, ok := nearestStation(provider, vesselState.Latitude, vesselState.Longitude)
	if !ok || station.StationID == payload.UI.TideStationID {
		return
	}

	log.Printf("Auto-updating tide station: %s → %s (%s)", payload.UI.TideStationID, station.StationID, station.Name)

	uiMap, _ := settings["ui"].(map[string]any)
	if uiMap == nil {
		uiMap = map[string]any{}
	}
	uiMap["tide_station_id"] = station.StationID
	uiMap["tide_station_name"] = station.Name
	settings["ui"] = uiMap
	if err := writeSettings(settingsPath, settings); err != nil {
		log.Printf("Failed to write updated tide station to settings: %v", err)
	}
}
