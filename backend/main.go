package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"gopkg.in/yaml.v3"
)

const (
	defaultSignalKAddress = "localhost"
	defaultSignalKPort    = 3000
)

func main() {
	e := echo.New()
	port := getEnv("PORT", "8080")

	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete},
	}))

	// Routes
	e.GET("/api/health", healthCheck)
	e.GET("/api/vessel-state", vesselState)
	e.GET("/api/settings/signalk", getSignalKSettingsHandler)
	e.POST("/api/settings/signalk", updateSignalKSettingsHandler)

	addr := fmt.Sprintf(":%s", port)
	log.Printf("Starting server on %s", addr)
	if err := e.Start(addr); err != nil && err != http.ErrServerClosed {
		log.Fatalf("error starting server: %v", err)
	}
}

func getEnv(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}

func healthCheck(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"status": "ok",
	})
}

func vesselState(c echo.Context) error {
	status := getEnv("VESSEL_STATUS", "At Anchor")
	datetime := time.Now().UTC()
	source := "backend-fallback"

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)
	vesselPath := getEnv("SIGNALK_VESSEL_PATH", "/signalk/v1/api/vessels/self")

	if signalkURL != "" {
		signalkStatus, signalkDatetime, err := fetchSignalKVesselState(signalkURL, vesselPath)
		if err == nil {
			status = signalkStatus
			datetime = signalkDatetime
			source = "signalk"
		}
	}

	return c.JSON(http.StatusOK, map[string]string{
		"status":   status,
		"datetime": datetime.Format(time.RFC3339),
		"source":   source,
	})
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
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid request payload",
		})
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

	if _, _, err := fetchSignalKVesselState(signalkURL, vesselPath); err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{
			"error": fmt.Sprintf("unable to connect to SignalK at %s", signalkURL),
		})
	}

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	if err := saveSignalKSettings(settingsPath, address, port); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "connected to SignalK, but failed to persist settings",
		})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"address":   address,
		"port":      port,
		"url":       signalkURL,
		"connected": true,
	})
}

func fetchSignalKVesselState(signalkURL string, vesselPath string) (string, time.Time, error) {
	url := strings.TrimRight(signalkURL, "/") + "/" + strings.TrimLeft(vesselPath, "/")

	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		return "", time.Time{}, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("signalk returned status %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", time.Time{}, err
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", time.Time{}, err
	}

	status := firstNonEmptyString(
		lookupString(payload, "navigation", "state", "value"),
		lookupString(payload, "navigation", "state"),
	)
	if status == "" {
		status = "Unknown"
	}

	datetimeString := firstNonEmptyString(
		lookupString(payload, "navigation", "datetime", "value"),
		lookupString(payload, "navigation", "datetime"),
		lookupString(payload, "timestamp"),
	)

	datetime := time.Now().UTC()
	if datetimeString != "" {
		parsed, err := time.Parse(time.RFC3339, datetimeString)
		if err == nil {
			datetime = parsed.UTC()
		}
	}

	return status, datetime, nil
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

	signalkMap := map[string]any{
		"address": strings.TrimSpace(address),
		"port":    port,
	}
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
