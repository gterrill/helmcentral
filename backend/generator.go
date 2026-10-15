package main

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
)

func generatorPut(signalkURL, settingsPath, path string, value any) error {
	username, password, err := loadSignalKCredentials(settingsPath)
	if err != nil {
		return fmt.Errorf("could not read the SignalK credentials: %w", err)
	}
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

// generatorBasePath is the SignalK path of the generator the operator chose
// in Vessel settings. problem is a plain sentence for the operator when there
// is no usable generator. The id is checked again here because it is spliced
// into a URL path that carries a write.
func generatorBasePath(vessel vesselView) (path string, problem string) {
	id := vessel.Electrical.GeneratorInstance
	if id == "" {
		return "", "the generator is not chosen under Settings > SignalK > Roles"
	}
	if !alternatorInstanceIDRe.MatchString(id) {
		return "", "the generator role points at an item with no valid generator source"
	}
	return "/signalk/v1/api/vessels/self/electrical/generator/" + id, ""
}

// resolveGeneratorPath loads Vessel settings and resolves the generator path.
// When it cannot, the reply has already been written and handled is true. A
// vessel block that cannot be read is a server error, not "not set up".
func resolveGeneratorPath(c echo.Context, settingsPath string) (path string, handled bool, err error) {
	vessel, loadErr := loadVesselView(settingsPath)
	if loadErr != nil {
		return "", true, c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read the vessel"})
	}
	path, problem := generatorBasePath(vessel)
	if problem != "" {
		return "", true, c.JSON(http.StatusConflict, map[string]string{"error": problem})
	}
	return path, false, nil
}

func postGeneratorStartHandler(c echo.Context) error {
	var req struct {
		TimerSeconds int `json:"timer_seconds"`
	}
	// Body is optional — ignore bind errors
	_ = c.Bind(&req)

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	basePath, handled, err := resolveGeneratorPath(c, settingsPath)
	if handled {
		return err
	}
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)

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
	basePath, handled, err := resolveGeneratorPath(c, settingsPath)
	if handled {
		return err
	}
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)

	if err := generatorPut(signalkURL, settingsPath, basePath+"/manualStartTimer", 0); err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "failed to clear timer: " + err.Error()})
	}

	if err := generatorPut(signalkURL, settingsPath, basePath+"/manualStart", false); err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "failed to stop generator: " + err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]any{"stopped": true})
}

// generatorRoutes returns the /api routes for generator start and stop.
// Each row carries its own auth tier (docs/adr/0177).
func generatorRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodPost, "/api/generator/start", tierWrite, postGeneratorStartHandler},
		{http.MethodPost, "/api/generator/stop", tierWrite, postGeneratorStopHandler},
	}
}
