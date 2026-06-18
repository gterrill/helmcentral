package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

const (
	// Resources and Course are v2 SignalK REST APIs (distinct from the v1
	// full/delta data-model tree used elsewhere in this codebase, e.g.
	// putSignalKValue's switch/generator writes). Confirmed against a live
	// SignalK server (v2.24.0, @signalk/resources-provider 1.5.1,
	// @signalk/course-provider 1.4.0) — v1 equivalents 404/405 on that
	// plugin set.
	signalkResourcesRoutesPath   = "/signalk/v2/api/resources/routes"
	signalkCourseActiveRoutePath = "/signalk/v2/api/vessels/self/navigation/course/activeRoute"
	signalkCoursePath            = "/signalk/v2/api/vessels/self/navigation/course"
	signalkRouteHrefPrefix       = "/resources/routes/"
)

// ── Route <-> SignalK resource conversion ───────────────────────────────────

type signalkRouteFeatureGeometry struct {
	Type        string       `json:"type"`
	Coordinates [][2]float64 `json:"coordinates"`
}

type signalkRouteFeature struct {
	Type       string                      `json:"type"`
	Geometry   signalkRouteFeatureGeometry `json:"geometry"`
	Properties map[string]any              `json:"properties"`
}

type signalkRouteResource struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Distance    float64             `json:"distance"`
	Feature     signalkRouteFeature `json:"feature"`
}

// routeDistanceMeters sums haversine distance across consecutive waypoint pairs.
func routeDistanceMeters(waypoints []routeWaypoint) float64 {
	total := 0.0
	for i := 0; i < len(waypoints)-1; i++ {
		total += haversineMeters(waypoints[i].Lat, waypoints[i].Lon, waypoints[i+1].Lat, waypoints[i+1].Lon)
	}
	return total
}

// routeToSignalKResource converts a HelmCentral route into the SignalK
// Resources API routes/{id} body shape, flipping {lat,lon} -> [lon,lat].
func routeToSignalKResource(route *routeData) signalkRouteResource {
	coords := make([][2]float64, len(route.Waypoints))
	for i, wp := range route.Waypoints {
		coords[i] = [2]float64{wp.Lon, wp.Lat}
	}

	return signalkRouteResource{
		Name:     route.Name,
		Distance: routeDistanceMeters(route.Waypoints),
		Feature: signalkRouteFeature{
			Type: "Feature",
			Geometry: signalkRouteFeatureGeometry{
				Type:        "LineString",
				Coordinates: coords,
			},
			Properties: map[string]any{},
		},
	}
}

// ── Route ID <-> SignalK href ────────────────────────────────────────────────

func signalkRouteHref(routeID string) string {
	return signalkRouteHrefPrefix + routeID
}

// routeIDFromSignalKHref extracts the route ID from a Course API href such as
// "/resources/routes/<id>". Returns "" if href isn't a routes resource (e.g.
// a waypoint, or some other resource type).
func routeIDFromSignalKHref(href string) string {
	if !strings.HasPrefix(href, signalkRouteHrefPrefix) {
		return ""
	}
	return strings.TrimPrefix(href, signalkRouteHrefPrefix)
}

// ── Low-level SignalK REST helper (raw body, not {"value":...}-wrapped) ─────

// signalkRequestJSON performs an HTTP request against a SignalK endpoint with a
// raw JSON body (no {"value":...} envelope — unlike putSignalKValue, which is
// for plain SignalK data-model writes). Returns the response status and raw
// body so callers can propagate SignalK's exact error text.
func signalkRequestJSON(signalkURL, path, method string, body any, token string) (int, []byte, error) {
	url := strings.TrimRight(signalkURL, "/") + "/" + strings.TrimLeft(path, "/")

	var reqBody io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reqBody = bytes.NewReader(payload)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()

	respBody, _ := io.ReadAll(response.Body)
	return response.StatusCode, respBody, nil
}

// signalkRequestJSONWithAuth wraps signalkRequestJSON with the same
// acquire-token/retry mechanics as generatorPut, but retries only on an
// auth-looking failure (401/403, or a transport error) rather than on every
// non-2xx, so a genuine 4xx/5xx upstream error surfaces immediately instead of
// being retried pointlessly. Returns an error containing SignalK's raw
// status+body on any non-2xx result, so callers can propagate it verbatim.
func signalkRequestJSONWithAuth(signalkURL, settingsPath, path, method string, body any) error {
	username, password := loadSignalKCredentials(settingsPath)
	token, err := acquireSignalKToken(signalkURL, username, password)
	if err != nil {
		return err
	}

	status, respBody, reqErr := signalkRequestJSON(signalkURL, path, method, body, token)

	needsRetry := reqErr != nil || status == http.StatusUnauthorized || status == http.StatusForbidden
	if needsRetry && token != "" {
		invalidateSignalKToken()
		token, err = acquireSignalKToken(signalkURL, username, password)
		if err != nil {
			return err
		}
		status, respBody, reqErr = signalkRequestJSON(signalkURL, path, method, body, token)
	}

	if reqErr != nil {
		return reqErr
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("signalk returned status %d: %s", status, string(respBody))
	}
	return nil
}

// ── Core actions ─────────────────────────────────────────────────────────────

func activateSignalKRoute(signalkURL, settingsPath string, route *routeData) error {
	resource := routeToSignalKResource(route)
	resourcePath := signalkResourcesRoutesPath + "/" + route.ID
	if err := signalkRequestJSONWithAuth(signalkURL, settingsPath, resourcePath, http.MethodPut, resource); err != nil {
		return fmt.Errorf("failed to upsert route resource: %w", err)
	}

	activation := map[string]any{
		"href":       signalkRouteHref(route.ID),
		"pointIndex": 0,
		"reverse":    false,
	}
	if err := signalkRequestJSONWithAuth(signalkURL, settingsPath, signalkCourseActiveRoutePath, http.MethodPut, activation); err != nil {
		return fmt.Errorf("failed to activate route: %w", err)
	}
	return nil
}

// deactivateSignalKRoute clears the active route pointer only — it does not
// delete the underlying SignalK resource (left stale/harmless). DELETE
// targets the course resource itself (signalkCoursePath), not the
// activeRoute sub-path — confirmed live: DELETE on .../course/activeRoute
// 404s on this server's course-provider plugin, while DELETE on .../course
// correctly clears activeRoute (and the rest of the course state) to null.
func deactivateSignalKRoute(signalkURL, settingsPath string) error {
	if err := signalkRequestJSONWithAuth(signalkURL, settingsPath, signalkCoursePath, http.MethodDelete, nil); err != nil {
		return fmt.Errorf("failed to deactivate route: %w", err)
	}
	return nil
}

type signalkCourseStatus struct {
	ActiveRouteHref string
	PointIndex      int
	Reverse         bool
}

// fetchSignalKCourseStatus GETs the Course API and extracts activeRoute info.
// An absent/null activeRoute is a normal "nothing active" result (zero value),
// not an error.
func fetchSignalKCourseStatus(signalkURL string) (signalkCourseStatus, error) {
	url := strings.TrimRight(signalkURL, "/") + signalkCoursePath

	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		return signalkCourseStatus{}, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return signalkCourseStatus{}, err
	}

	if response.StatusCode != http.StatusOK {
		return signalkCourseStatus{}, fmt.Errorf("signalk returned status %d: %s", response.StatusCode, string(body))
	}

	var payload struct {
		ActiveRoute *struct {
			Href       string `json:"href"`
			PointIndex int    `json:"pointIndex"`
			Reverse    bool   `json:"reverse"`
		} `json:"activeRoute"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return signalkCourseStatus{}, err
	}

	if payload.ActiveRoute == nil || payload.ActiveRoute.Href == "" {
		return signalkCourseStatus{}, nil
	}

	return signalkCourseStatus{
		ActiveRouteHref: payload.ActiveRoute.Href,
		PointIndex:      payload.ActiveRoute.PointIndex,
		Reverse:         payload.ActiveRoute.Reverse,
	}, nil
}

// ── Handlers ─────────────────────────────────────────────────────────────────

func resolveSignalKURL() (string, string) {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}
	return buildSignalKURL(address, port), settingsPath
}

// POST /api/routes/:id/activate
func activateRouteHandler(c echo.Context) error {
	id := c.Param("id")

	routesMu.RLock()
	route, ok := routesState[id]
	routesMu.RUnlock()

	if !ok {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "route not found"})
	}

	signalkURL, settingsPath := resolveSignalKURL()
	if err := activateSignalKRoute(signalkURL, settingsPath, route); err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to activate route on signalk: %v", err)})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"active":   true,
		"route_id": route.ID,
		"href":     signalkRouteHref(route.ID),
	})
}

// POST /api/routes/deactivate
func deactivateRouteHandler(c echo.Context) error {
	signalkURL, settingsPath := resolveSignalKURL()
	if err := deactivateSignalKRoute(signalkURL, settingsPath); err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to deactivate route on signalk: %v", err)})
	}

	return c.JSON(http.StatusOK, map[string]any{"active": false})
}

// GET /api/routes/active
func getActiveRouteHandler(c echo.Context) error {
	signalkURL, _ := resolveSignalKURL()

	status, err := fetchSignalKCourseStatus(signalkURL)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to fetch active route from signalk: %v", err)})
	}

	if status.ActiveRouteHref == "" {
		return c.JSON(http.StatusOK, map[string]any{"active": false})
	}

	routeID := routeIDFromSignalKHref(status.ActiveRouteHref)

	routesMu.RLock()
	route, ok := routesState[routeID]
	routesMu.RUnlock()

	result := map[string]any{
		"active":      true,
		"point_index": status.PointIndex,
		"reverse":     status.Reverse,
	}
	if ok {
		result["route_id"] = route.ID
		result["route_name"] = route.Name
	} else {
		result["route_id"] = nil
	}

	return c.JSON(http.StatusOK, result)
}
