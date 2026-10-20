package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

const (
	// Resources and Course are v2 SignalK REST APIs (distinct from the v1
	// full/delta data-model tree used elsewhere in this codebase).
	signalkResourcesRoutesPath = "/signalk/v2/api/resources/routes"
	signalkCoursePath          = "/signalk/v2/api/vessels/self/navigation/course"
	signalkRouteHrefPrefix     = "/resources/routes/"
)

// routeWaypoint is one point of the route SignalK is navigating, in the
// route resource's authored order.
type routeWaypoint struct {
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	Name string  `json:"name,omitempty"`
}

// signalkRouteResource is the part of a SignalK Resources API routes/{id}
// body this endpoint reads.
type signalkRouteResource struct {
	Name    string `json:"name"`
	Feature struct {
		Geometry struct {
			Coordinates [][]float64 `json:"coordinates"`
		} `json:"geometry"`
		Properties struct {
			CoordinatesMeta []struct {
				Name string `json:"name"`
			} `json:"coordinatesMeta"`
		} `json:"properties"`
	} `json:"feature"`
}

// routeIDFromSignalKHref extracts the route ID from a Course API href such as
// "/resources/routes/<id>". Returns "" if href isn't a routes resource.
func routeIDFromSignalKHref(href string) string {
	if !strings.HasPrefix(href, signalkRouteHrefPrefix) {
		return ""
	}
	return strings.TrimPrefix(href, signalkRouteHrefPrefix)
}

// fetchSignalKRoute reads a route resource and flips its [lon,lat]
// coordinates to lat/lon, naming each point from coordinatesMeta when present.
// Any failure is returned as is: the caller must not report "inactive" for a
// route SignalK says is active.
func fetchSignalKRoute(signalkURL, routeID string) (string, []routeWaypoint, error) {
	status, body, err := signalkRequestJSON(signalkURL, signalkResourcesRoutesPath+"/"+url.PathEscape(routeID), http.MethodGet, nil, "")
	if err != nil {
		return "", nil, err
	}
	if status != http.StatusOK {
		return "", nil, fmt.Errorf("signalk returned status %d: %s", status, string(body))
	}
	var res signalkRouteResource
	if err := json.Unmarshal(body, &res); err != nil {
		return "", nil, fmt.Errorf("could not read route resource: %w", err)
	}
	coords := res.Feature.Geometry.Coordinates
	waypoints := make([]routeWaypoint, 0, len(coords))
	for i, pair := range coords {
		if len(pair) < 2 {
			return "", nil, fmt.Errorf("route resource point %d has no lon/lat pair", i)
		}
		wp := routeWaypoint{Lat: pair[1], Lon: pair[0]}
		if i < len(res.Feature.Properties.CoordinatesMeta) {
			wp.Name = res.Feature.Properties.CoordinatesMeta[i].Name
		}
		waypoints = append(waypoints, wp)
	}
	return res.Name, waypoints, nil
}

type signalkCourseStatus struct {
	ActiveRouteHref string
	PointIndex      int
	Reverse         bool
	// NextPoint* (ADR 0125): the Course API's `nextPoint`, present whenever
	// the chartplotter has a destination set - whether or not it came from a
	// Helmcentral-activated route. A plain chartplotter go-to (no
	// activeRoute at all) still populates this, which is what lets the
	// clock tile show a trip ETA for that case too.
	HasNextPoint  bool
	NextPointLat  float64
	NextPointLon  float64
	NextPointType string
}

// fetchSignalKCourseStatus GETs the Course API and extracts activeRoute and
// nextPoint info. An absent/null activeRoute is a normal "nothing active"
// result (zero value), not an error - same now for nextPoint.
func fetchSignalKCourseStatus(signalkURL string) (signalkCourseStatus, error) {
	endpoint := strings.TrimRight(signalkURL, "/") + signalkCoursePath

	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(endpoint)
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
		NextPoint *struct {
			Position *struct {
				Latitude  float64 `json:"latitude"`
				Longitude float64 `json:"longitude"`
			} `json:"position"`
			Type string `json:"type"`
		} `json:"nextPoint"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return signalkCourseStatus{}, err
	}

	var status signalkCourseStatus
	if payload.ActiveRoute != nil && payload.ActiveRoute.Href != "" {
		status.ActiveRouteHref = payload.ActiveRoute.Href
		status.PointIndex = payload.ActiveRoute.PointIndex
		status.Reverse = payload.ActiveRoute.Reverse
	}
	if payload.NextPoint != nil && payload.NextPoint.Position != nil {
		status.HasNextPoint = true
		status.NextPointLat = payload.NextPoint.Position.Latitude
		status.NextPointLon = payload.NextPoint.Position.Longitude
		status.NextPointType = payload.NextPoint.Type
	}

	return status, nil
}

// ── Handler ──────────────────────────────────────────────────────────────────

// activeRouteRoutes returns the one /api route for the course SignalK is
// navigating. Each row carries its own auth tier (docs/adr/0177).
func activeRouteRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodGet, "/api/routes/active", tierRead, getActiveRouteHandler},
	}
}

// GET /api/routes/active
//
// Reports exactly what SignalK's Course API says: the active route (read from
// its route resource) and/or the next point. A failed course or resource
// read is a 502, never "inactive".
func getActiveRouteHandler(c echo.Context) error {
	signalkURL, _ := resolveSignalKURL()

	status, err := fetchSignalKCourseStatus(signalkURL)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to fetch active route from signalk: %v", err)})
	}

	result := map[string]any{"active": false}
	if status.ActiveRouteHref != "" {
		routeID := routeIDFromSignalKHref(status.ActiveRouteHref)
		if routeID == "" {
			return c.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("signalk active route %q is not a routes resource", status.ActiveRouteHref)})
		}
		name, waypoints, err := fetchSignalKRoute(signalkURL, routeID)
		if err != nil {
			return c.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to read the active route from signalk: %v", err)})
		}
		result = map[string]any{
			"active":      true,
			"point_index": status.PointIndex,
			"reverse":     status.Reverse,
			"route":       map[string]any{"name": name, "waypoints": waypoints},
		}
	}

	// destination (ADR 0125): the Course API's nextPoint, whenever present.
	// Only added when the Course API actually carries one, so a response with
	// no destination gets no key, not a null one.
	if status.HasNextPoint {
		result["destination"] = map[string]any{
			"lat":  status.NextPointLat,
			"lon":  status.NextPointLon,
			"name": resolveDestinationPlaceName(status.NextPointLat, status.NextPointLon),
		}
	}

	return c.JSON(http.StatusOK, result)
}
