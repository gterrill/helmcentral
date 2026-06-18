package main

import (
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type routeWaypoint struct {
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	Name string  `json:"name,omitempty"`
}

type routeData struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Waypoints []routeWaypoint `json:"waypoints"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type routesFile struct {
	Routes []*routeData `json:"routes"`
}

var (
	routesMu    sync.RWMutex
	routesState map[string]*routeData
)

func routesFilePath() string {
	return cacheFilePath("ROUTES_FILE", "data/routes.json")
}

func loadRoutes() {
	routesMu.Lock()
	defer routesMu.Unlock()

	routesState = make(map[string]*routeData)

	data, err := os.ReadFile(routesFilePath())
	if err != nil {
		return
	}

	var loaded routesFile
	if err := json.Unmarshal(data, &loaded); err != nil {
		return
	}

	for _, r := range loaded.Routes {
		routesState[r.ID] = r
	}
}

// saveRoutesLocked persists the current in-memory state. Callers must hold routesMu.
func saveRoutesLocked() error {
	list := make([]*routeData, 0, len(routesState))
	for _, r := range routesState {
		list = append(list, r)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].CreatedAt.Before(list[j].CreatedAt)
	})
	return writeJSONFileAtomic(routesFilePath(), routesFile{Routes: list})
}

func validateWaypoints(waypoints []routeWaypoint) string {
	if len(waypoints) == 0 {
		return "at least one waypoint is required"
	}
	for _, wp := range waypoints {
		if wp.Lat < -90 || wp.Lat > 90 || wp.Lon < -180 || wp.Lon > 180 {
			return "waypoint lat/lon out of range"
		}
	}
	return ""
}

// GET /api/routes
func listRoutesHandler(c echo.Context) error {
	routesMu.RLock()
	defer routesMu.RUnlock()

	list := make([]*routeData, 0, len(routesState))
	for _, r := range routesState {
		list = append(list, r)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].CreatedAt.Before(list[j].CreatedAt)
	})

	return c.JSON(http.StatusOK, map[string]any{"routes": list})
}

// POST /api/routes
func createRouteHandler(c echo.Context) error {
	var body struct {
		Name      string          `json:"name"`
		Waypoints []routeWaypoint `json:"waypoints"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "name is required"})
	}
	if msg := validateWaypoints(body.Waypoints); msg != "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": msg})
	}

	now := time.Now().UTC()
	route := &routeData{
		ID:        uuid.NewString(),
		Name:      name,
		Waypoints: body.Waypoints,
		CreatedAt: now,
		UpdatedAt: now,
	}

	routesMu.Lock()
	routesState[route.ID] = route
	err := saveRoutesLocked()
	routesMu.Unlock()

	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to persist"})
	}

	return c.JSON(http.StatusCreated, route)
}

// GET /api/routes/:id
func getRouteHandler(c echo.Context) error {
	id := c.Param("id")

	routesMu.RLock()
	route, ok := routesState[id]
	routesMu.RUnlock()

	if !ok {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "route not found"})
	}
	return c.JSON(http.StatusOK, route)
}

// PATCH /api/routes/:id
func patchRouteHandler(c echo.Context) error {
	id := c.Param("id")

	var body struct {
		Name      *string          `json:"name"`
		Waypoints *[]routeWaypoint `json:"waypoints"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if body.Name == nil && body.Waypoints == nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "no patch fields provided"})
	}
	if body.Name != nil && strings.TrimSpace(*body.Name) == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "name cannot be empty"})
	}
	if body.Waypoints != nil {
		if msg := validateWaypoints(*body.Waypoints); msg != "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": msg})
		}
	}

	routesMu.Lock()
	defer routesMu.Unlock()

	current, ok := routesState[id]
	if !ok {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "route not found"})
	}

	updated := &routeData{
		ID:        current.ID,
		Name:      current.Name,
		Waypoints: current.Waypoints,
		CreatedAt: current.CreatedAt,
		UpdatedAt: time.Now().UTC(),
	}
	if body.Name != nil {
		updated.Name = strings.TrimSpace(*body.Name)
	}
	if body.Waypoints != nil {
		updated.Waypoints = *body.Waypoints
	}

	routesState[id] = updated
	if err := saveRoutesLocked(); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to persist"})
	}

	return c.JSON(http.StatusOK, updated)
}

// DELETE /api/routes/:id
func deleteRouteHandler(c echo.Context) error {
	id := c.Param("id")

	routesMu.Lock()
	defer routesMu.Unlock()

	if _, ok := routesState[id]; !ok {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "route not found"})
	}

	delete(routesState, id)
	if err := saveRoutesLocked(); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to persist"})
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "deleted"})
}
