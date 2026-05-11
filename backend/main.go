package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
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

	signalkURL := getEnv("SIGNALK_URL", "")
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
