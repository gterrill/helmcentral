package main

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

func generatorPut(signalkURL, settingsPath, path string, value any) error {
	username, password := loadSignalKCredentials(settingsPath)
	token, err := acquireSignalKToken(signalkURL, username, password)
	if err != nil {
		return err
	}

	err = putSignalKValue(signalkURL, path, value, token)
	if err != nil && token != "" {
		// Token may have expired server-side — invalidate and retry once
		invalidateSignalKToken()
		token, err = acquireSignalKToken(signalkURL, username, password)
		if err != nil {
			return err
		}
		err = putSignalKValue(signalkURL, path, value, token)
	}
	return err
}

func postGeneratorStartHandler(c echo.Context) error {
	var req struct {
		TimerSeconds int `json:"timer_seconds"`
	}
	// Body is optional — ignore bind errors
	_ = c.Bind(&req)

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)
	basePath := "/signalk/v1/api/vessels/self/electrical/generator/0"

	// Set timer first (0 = run indefinitely)
	if err := generatorPut(signalkURL, settingsPath, basePath+"/manualStartTimer", req.TimerSeconds); err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "failed to set timer: " + err.Error()})
	}

	if err := generatorPut(signalkURL, settingsPath, basePath+"/manualStart", true); err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "failed to start generator: " + err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]any{"started": true, "timer_seconds": req.TimerSeconds})
}

func postGeneratorStopHandler(c echo.Context) error {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)
	basePath := "/signalk/v1/api/vessels/self/electrical/generator/0"

	if err := generatorPut(signalkURL, settingsPath, basePath+"/manualStartTimer", 0); err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "failed to clear timer: " + err.Error()})
	}

	if err := generatorPut(signalkURL, settingsPath, basePath+"/manualStart", false); err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "failed to stop generator: " + err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]any{"stopped": true})
}
