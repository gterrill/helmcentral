package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Probing is the one place a SignalK REST read survives (ADR 0037).
//
// Telemetry comes exclusively from the delta stream, but connection *setup*
// has to reach a server that is not the configured one and has no stream open:
// discovery names every host it finds on the LAN, and the settings screen
// verifies a new address before saving it (ADR 0028). Answering either from the
// snapshot would report the currently configured vessel — discovery would label
// every result with the same boat name, and a broken address would validate
// clean and take the dashboard offline on save.

// probeSignalKTree fetches a SignalK subtree directly over HTTP from an
// arbitrary base URL.
func probeSignalKTree(baseURL string, path string) (map[string]any, error) {
	url := strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/")

	response, err := signalKReadClient().Get(url)
	if err != nil {
		return nil, fmt.Errorf("signalk unreachable: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("signalk returned status %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("signalk response unreadable: %w", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("signalk response malformed: %w", err)
	}

	return payload, nil
}

// probeSignalKReachable reports whether a candidate SignalK server answers with
// a usable vessel tree. Used to validate an address change before it is saved.
func probeSignalKReachable(baseURL string, vesselPath string) error {
	_, err := probeSignalKTree(baseURL, vesselPath)
	return err
}

// probeSignalKVesselName returns the vessel name reported by a candidate
// server, or "" if it cannot be reached. Used to label discovery results.
func probeSignalKVesselName(baseURL string, vesselPath string) string {
	payload, err := probeSignalKTree(baseURL, vesselPath)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(firstNonEmptyString(lookupString(payload, "name"), lookupString(payload, "design", "name")))
}
