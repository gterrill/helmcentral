package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// validSwitchIDRegexp ensures switch IDs like "bank.0.2" or "venus-0" cannot
// contain path traversal characters. Segments stay non-empty and
// alphanumeric-plus-hyphen, so a bare ".." segment remains impossible.
var validSwitchIDRegexp = regexp.MustCompile(`^[a-zA-Z0-9-]+(\.[a-zA-Z0-9-]+)*$`)

type czoneSwitch struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	State       int    `json:"state"`    // 0 = off, 1 = on
	Writable    bool   `json:"writable"` // meta.supportsPut: never claim a control works when the metadata doesn't say so
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

	// The wire ID is exactly the dotted SignalK sub-path (e.g. "bank.0.2",
	// "venus-0", "gx.gxInternalRelay1"), so this is a straight dot-to-slash
	// conversion: "bank.0.2" → "bank/0/2".
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
	walkSwitchTree(payload, "", &switches)

	if len(switches) == 0 {
		keys := make([]string, 0, len(payload))
		for key := range payload {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return nil, fmt.Errorf("electrical/switches payload has no recognisable switch node (keys present: %s)", strings.Join(keys, ", "))
	}

	return switches, nil
}

// walkSwitchTree recursively walks the electrical/switches subtree in
// deterministic order, emitting a czoneSwitch for every node whose "state"
// leaf parseSwitchState can read, and recursing into every node that can't.
// This naturally yields ids like "bank.0.2", "venus-0", and
// "gx.gxInternalRelay1" from one generic walk, without hand-coding each
// vendor's shape (CZone banks, Venus relays, GX internal relays, ...).
func walkSwitchTree(node map[string]any, prefix string, out *[]czoneSwitch) {
	childIDs := make([]string, 0, len(node))
	for key, val := range node {
		if _, ok := val.(map[string]any); ok {
			childIDs = append(childIDs, key)
		}
	}
	sortIDsNumerically(childIDs, func(id string) (float64, bool) {
		child, ok := node[id].(map[string]any)
		if !ok {
			return 0, false
		}
		return circuitOrderKey(id, child)
	})

	for _, id := range childIDs {
		child, ok := node[id].(map[string]any)
		if !ok {
			continue
		}

		fullID := id
		if prefix != "" {
			fullID = prefix + "." + id
		}

		state := parseSwitchState(child)
		if state == -1 {
			// Not a switch value node itself — recurse to look for switches
			// beneath it (e.g. a bank, or the gx container).
			walkSwitchTree(child, fullID, out)
			continue
		}

		// meta is nested INSIDE the "state" value node (state.meta), not a
		// sibling of "state" — confirmed against the live vessel: the only
		// key under a switch node is "state", and state.meta carries
		// displayName/supportsPut.
		displayName := lookupString(child, "state", "meta", "displayName")
		if displayName == "" {
			displayName = defaultSwitchDisplayName(fullID, id)
		}
		writable, _ := lookupBool(child, "state", "meta", "supportsPut")

		*out = append(*out, czoneSwitch{
			ID:          fullID,
			DisplayName: displayName,
			State:       state,
			Writable:    writable,
		})
	}
}

// defaultSwitchDisplayName is the fallback used when meta.displayName is
// absent. "Bank N Circuit M" only makes sense for CZone bank circuits;
// anything else (venus-0, gx.gxInternalRelay1, ...) falls back to its own
// last path segment rather than an invented decorative name.
func defaultSwitchDisplayName(fullID string, lastSegment string) string {
	parts := strings.Split(fullID, ".")
	if len(parts) == 3 && parts[0] == "bank" {
		return fmt.Sprintf("Bank %s Circuit %s", parts[1], parts[2])
	}
	return lastSegment
}

// sortIDsNumerically sorts ids in place using key for the primary numeric
// comparison, falling back to a plain string comparison when key is
// unavailable for either id, or when both keys agree — so IDs that aren't
// numeric, and ties, still sort deterministically.
func sortIDsNumerically(ids []string, key func(id string) (float64, bool)) {
	sort.SliceStable(ids, func(i, j int) bool {
		ki, oki := key(ids[i])
		kj, okj := key(ids[j])
		if oki && okj && ki != kj {
			return ki < kj
		}
		return ids[i] < ids[j]
	})
}

// numericID parses a bank/circuit ID as an integer for numeric sorting. ok is
// false for a non-numeric ID, in which case the caller falls back to string
// comparison.
func numericID(id string) (float64, bool) {
	n, err := strconv.Atoi(id)
	if err != nil {
		return 0, false
	}
	return float64(n), true
}

// circuitOrderKey returns the sort key for a node at any level of the switch
// tree walk: the installer's configured display sequence (order.value,
// sharing the identical nested {"value": N} shape parseSwitchState reads for
// "state"), when present — currently only CZone bank circuits carry one.
// CZone installs with Third Party Mode enabled can have a panel order that
// diverges from the bus index, so order.value must win over the ID when both
// are present. Falls back to the node's own ID parsed as an integer when no
// order leaf is present (which is every non-bank-circuit node).
func circuitOrderKey(id string, circuit map[string]any) (float64, bool) {
	if order := lookupNumber(circuit, "order", "value"); order != -1 {
		return order, true
	}
	return numericID(id)
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
