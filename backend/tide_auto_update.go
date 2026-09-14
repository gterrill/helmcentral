package main

import (
	"context"
	"log"
	"time"
)

func startTideAutoUpdater(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// See startTrackPoller (tracks.go) for why this recheck
				// matters: select does not prefer ctx.Done() over a tick
				// ready at the same instant.
				if ctx.Err() != nil {
					return
				}
				updateNearestTideStation()
			}
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

	vesselState, err := fetchSignalKVesselState()
	if err != nil || !hasUsableVesselPosition(vesselState.Latitude, vesselState.Longitude) {
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
