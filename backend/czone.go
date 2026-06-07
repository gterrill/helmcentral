package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// validSwitchIDRegexp ensures switch IDs like "banks.1.1" cannot contain path traversal characters.
var validSwitchIDRegexp = regexp.MustCompile(`^[a-zA-Z0-9]+(\.[a-zA-Z0-9]+)*$`)

type czoneSwitch struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	State       int    `json:"state"` // 0 = off, 1 = on
}

func getCZoneSwitchesHandler(c echo.Context) error {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)
	switchesPath := "/signalk/v1/api/vessels/self/electrical/switches"

	switches, err := fetchSignalKSwitches(signalkURL, switchesPath)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("unable to fetch CZone switches: %v", err)})
	}

	return c.JSON(http.StatusOK, map[string]any{"switches": switches})
}

func putCZoneSwitchStateHandler(c echo.Context) error {
	id := c.Param("id")
	if !validSwitchIDRegexp.MatchString(id) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid switch id"})
	}

	var req struct {
		State int `json:"state"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}
	if req.State != 0 && req.State != 1 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "state must be 0 or 1"})
	}

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)

	// Convert id "banks.1.1" → path component "banks/1/1".
	pathSuffix := strings.ReplaceAll(id, ".", "/")
	path := fmt.Sprintf("/signalk/v1/api/vessels/self/electrical/switches/%s/state", pathSuffix)

	value := req.State == 1
	if err := putSignalKValue(signalkURL, path, value); err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("unable to control switch: %v", err)})
	}

	return c.JSON(http.StatusOK, map[string]any{"id": id, "state": req.State})
}

func fetchSignalKSwitches(signalkURL string, switchesPath string) ([]czoneSwitch, error) {
	url := strings.TrimRight(signalkURL, "/") + "/" + strings.TrimLeft(switchesPath, "/")

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

	switches := make([]czoneSwitch, 0)

	banksMap, ok := payload["banks"].(map[string]any)
	if !ok {
		return switches, nil
	}

	bankIDs := make([]string, 0, len(banksMap))
	for bankID := range banksMap {
		bankIDs = append(bankIDs, bankID)
	}
	sort.Strings(bankIDs)

	for _, bankID := range bankIDs {
		bank, ok := banksMap[bankID].(map[string]any)
		if !ok {
			continue
		}

		circuitIDs := make([]string, 0, len(bank))
		for circuitID := range bank {
			circuitIDs = append(circuitIDs, circuitID)
		}
		sort.Strings(circuitIDs)

		for _, circuitID := range circuitIDs {
			circuit, ok := bank[circuitID].(map[string]any)
			if !ok {
				continue
			}

			state := parseSwitchState(circuit)
			if state == -1 {
				continue
			}

			displayName := lookupString(circuit, "meta", "displayName")
			if displayName == "" {
				displayName = fmt.Sprintf("Bank %s Circuit %s", bankID, circuitID)
			}

			switches = append(switches, czoneSwitch{
				ID:          fmt.Sprintf("banks.%s.%s", bankID, circuitID),
				DisplayName: displayName,
				State:       state,
			})
		}
	}

	return switches, nil
}

// parseSwitchState extracts an on/off integer (1/0) from a SignalK switch circuit
// object, handling both boolean true/false and numeric 1/0 value representations.
// Returns -1 if no valid state can be determined.
func parseSwitchState(circuit map[string]any) int {
	// Nested form: {"state": {"value": true}}
	if stateMap, ok := circuit["state"].(map[string]any); ok {
		switch v := stateMap["value"].(type) {
		case bool:
			if v {
				return 1
			}
			return 0
		case float64:
			if v >= 0.5 {
				return 1
			}
			return 0
		}
	}

	// Flat form: {"state": true}
	switch v := circuit["state"].(type) {
	case bool:
		if v {
			return 1
		}
		return 0
	case float64:
		if v >= 0.5 {
			return 1
		}
		return 0
	}

	return -1
}

func putSignalKValue(signalkURL string, path string, value any, token ...string) error {
	url := strings.TrimRight(signalkURL, "/") + "/" + strings.TrimLeft(path, "/")

	body, err := json.Marshal(map[string]any{"value": value})
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if len(token) > 0 && token[0] != "" {
		req.Header.Set("Authorization", "Bearer "+token[0])
	}

	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		respBody, _ := io.ReadAll(response.Body)
		return fmt.Errorf("signalk returned status %d: %s", response.StatusCode, string(respBody))
	}

	return nil
}
