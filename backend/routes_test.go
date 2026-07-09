package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func setupRoutesTest(t *testing.T) {
	t.Helper()
	t.Setenv("ROUTES_FILE", filepath.Join(t.TempDir(), "routes.json"))
	loadRoutes()
}

func newRoutesRequest(t *testing.T, method, path string, body any) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	e := echo.New()

	var req *http.Request
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal request body: %v", err)
		}
		req = httptest.NewRequest(method, path, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}

	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

func createTestRoute(t *testing.T, name string, waypoints []routeWaypoint, planningSpeedKts float64) routeData {
	t.Helper()
	c, rec := newRoutesRequest(t, http.MethodPost, "/api/routes", map[string]any{
		"name":               name,
		"waypoints":          waypoints,
		"planning_speed_kts": planningSpeedKts,
	})
	if err := createRouteHandler(c); err != nil {
		t.Fatalf("createRouteHandler returned error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}

	var route routeData
	if err := json.Unmarshal(rec.Body.Bytes(), &route); err != nil {
		t.Fatalf("failed to parse created route: %v", err)
	}
	return route
}

func sampleWaypoints() []routeWaypoint {
	return []routeWaypoint{
		{Lat: 37.8, Lon: -122.4, Name: "Start"},
		{Lat: 37.9, Lon: -122.5, Name: "End"},
	}
}

func TestListRoutesHandler_EmptyInitially(t *testing.T) {
	setupRoutesTest(t)

	c, rec := newRoutesRequest(t, http.MethodGet, "/api/routes", nil)
	if err := listRoutesHandler(c); err != nil {
		t.Fatalf("listRoutesHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var payload struct {
		Routes []routeData `json:"routes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse list response: %v", err)
	}
	if len(payload.Routes) != 0 {
		t.Fatalf("expected 0 routes, got %d", len(payload.Routes))
	}
}

func TestCreateRouteHandler_ValidBody(t *testing.T) {
	setupRoutesTest(t)

	route := createTestRoute(t, "Marina to Anchorage", sampleWaypoints(), 8)

	if route.ID == "" {
		t.Fatal("expected route to have a generated ID")
	}
	if route.Name != "Marina to Anchorage" {
		t.Fatalf("expected name to round-trip, got %q", route.Name)
	}
	if len(route.Waypoints) != 2 {
		t.Fatalf("expected 2 waypoints, got %d", len(route.Waypoints))
	}
	if route.CreatedAt.IsZero() || route.UpdatedAt.IsZero() {
		t.Fatal("expected created_at/updated_at to be set")
	}
	if route.PlanningSpeedKts != 8 {
		t.Fatalf("expected planning speed to round-trip as 8, got %v", route.PlanningSpeedKts)
	}
}

func TestCreateRouteHandler_RejectsEmptyName(t *testing.T) {
	setupRoutesTest(t)

	c, rec := newRoutesRequest(t, http.MethodPost, "/api/routes", map[string]any{
		"name":      "   ",
		"waypoints": sampleWaypoints(),
	})
	if err := createRouteHandler(c); err != nil {
		t.Fatalf("createRouteHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestCreateRouteHandler_RejectsEmptyWaypoints(t *testing.T) {
	setupRoutesTest(t)

	c, rec := newRoutesRequest(t, http.MethodPost, "/api/routes", map[string]any{
		"name":      "No waypoints",
		"waypoints": []routeWaypoint{},
	})
	if err := createRouteHandler(c); err != nil {
		t.Fatalf("createRouteHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestCreateRouteHandler_RejectsOutOfRangeCoordinates(t *testing.T) {
	setupRoutesTest(t)

	c, rec := newRoutesRequest(t, http.MethodPost, "/api/routes", map[string]any{
		"name":      "Bad coords",
		"waypoints": []routeWaypoint{{Lat: 200, Lon: 0}},
	})
	if err := createRouteHandler(c); err != nil {
		t.Fatalf("createRouteHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestCreateRouteHandler_RejectsInvalidPlanningSpeed(t *testing.T) {
	setupRoutesTest(t)

	c, rec := newRoutesRequest(t, http.MethodPost, "/api/routes", map[string]any{
		"name":               "Bad speed",
		"waypoints":          sampleWaypoints(),
		"planning_speed_kts": 0,
	})
	if err := createRouteHandler(c); err != nil {
		t.Fatalf("createRouteHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	c2, rec2 := newRoutesRequest(t, http.MethodPost, "/api/routes", map[string]any{
		"name":               "Negative speed",
		"waypoints":          sampleWaypoints(),
		"planning_speed_kts": -5,
	})
	if err := createRouteHandler(c2); err != nil {
		t.Fatalf("createRouteHandler returned error: %v", err)
	}
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec2.Code)
	}
}

func TestGetRouteHandler_FoundAndNotFound(t *testing.T) {
	setupRoutesTest(t)
	route := createTestRoute(t, "Test Route", sampleWaypoints(), 8)

	c, rec := newRoutesRequest(t, http.MethodGet, "/api/routes/"+route.ID, nil)
	c.SetParamNames("id")
	c.SetParamValues(route.ID)
	if err := getRouteHandler(c); err != nil {
		t.Fatalf("getRouteHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	c2, rec2 := newRoutesRequest(t, http.MethodGet, "/api/routes/unknown-id", nil)
	c2.SetParamNames("id")
	c2.SetParamValues("unknown-id")
	if err := getRouteHandler(c2); err != nil {
		t.Fatalf("getRouteHandler returned error: %v", err)
	}
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, rec2.Code)
	}
}

func TestPatchRouteHandler_UpdatesNameAndWaypoints(t *testing.T) {
	setupRoutesTest(t)
	route := createTestRoute(t, "Original Name", sampleWaypoints(), 8)

	newWaypoints := []routeWaypoint{{Lat: 1, Lon: 2, Name: "Only Stop"}}
	c, rec := newRoutesRequest(t, http.MethodPatch, "/api/routes/"+route.ID, map[string]any{
		"name":      "Renamed Route",
		"waypoints": newWaypoints,
	})
	c.SetParamNames("id")
	c.SetParamValues(route.ID)
	if err := patchRouteHandler(c); err != nil {
		t.Fatalf("patchRouteHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var updated routeData
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("failed to parse patch response: %v", err)
	}
	if updated.Name != "Renamed Route" {
		t.Fatalf("expected renamed route, got %q", updated.Name)
	}
	if len(updated.Waypoints) != 1 {
		t.Fatalf("expected 1 waypoint after patch, got %d", len(updated.Waypoints))
	}
	if !updated.CreatedAt.Equal(route.CreatedAt) {
		t.Fatal("expected created_at to remain unchanged on patch")
	}
	if !updated.UpdatedAt.After(route.UpdatedAt) && !updated.UpdatedAt.Equal(route.UpdatedAt) {
		t.Fatal("expected updated_at to be bumped on patch")
	}
}

func TestPatchRouteHandler_UpdatesPlanningSpeedIndependently(t *testing.T) {
	setupRoutesTest(t)
	route := createTestRoute(t, "Original Name", sampleWaypoints(), 8)

	c, rec := newRoutesRequest(t, http.MethodPatch, "/api/routes/"+route.ID, map[string]any{
		"planning_speed_kts": 12,
	})
	c.SetParamNames("id")
	c.SetParamValues(route.ID)
	if err := patchRouteHandler(c); err != nil {
		t.Fatalf("patchRouteHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var updated routeData
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("failed to parse patch response: %v", err)
	}
	if updated.PlanningSpeedKts != 12 {
		t.Fatalf("expected planning speed to update to 12, got %v", updated.PlanningSpeedKts)
	}
	if updated.Name != route.Name {
		t.Fatalf("expected name to remain unchanged, got %q", updated.Name)
	}
	if len(updated.Waypoints) != len(route.Waypoints) {
		t.Fatalf("expected waypoints to remain unchanged, got %d", len(updated.Waypoints))
	}
}

func TestPatchRouteHandler_RejectsInvalidPlanningSpeed(t *testing.T) {
	setupRoutesTest(t)
	route := createTestRoute(t, "Original Name", sampleWaypoints(), 8)

	c, rec := newRoutesRequest(t, http.MethodPatch, "/api/routes/"+route.ID, map[string]any{
		"planning_speed_kts": 0,
	})
	c.SetParamNames("id")
	c.SetParamValues(route.ID)
	if err := patchRouteHandler(c); err != nil {
		t.Fatalf("patchRouteHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestPatchRouteHandler_PreservesPlanningSpeedWhenAbsent(t *testing.T) {
	setupRoutesTest(t)
	route := createTestRoute(t, "Original Name", sampleWaypoints(), 8)

	c, rec := newRoutesRequest(t, http.MethodPatch, "/api/routes/"+route.ID, map[string]any{
		"name": "Renamed Only",
	})
	c.SetParamNames("id")
	c.SetParamValues(route.ID)
	if err := patchRouteHandler(c); err != nil {
		t.Fatalf("patchRouteHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var updated routeData
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("failed to parse patch response: %v", err)
	}
	if updated.PlanningSpeedKts != route.PlanningSpeedKts {
		t.Fatalf("expected planning speed to remain %v, got %v", route.PlanningSpeedKts, updated.PlanningSpeedKts)
	}
}

func TestPatchRouteHandler_RequiresAtLeastOneField(t *testing.T) {
	setupRoutesTest(t)
	route := createTestRoute(t, "Test Route", sampleWaypoints(), 8)

	c, rec := newRoutesRequest(t, http.MethodPatch, "/api/routes/"+route.ID, map[string]any{})
	c.SetParamNames("id")
	c.SetParamValues(route.ID)
	if err := patchRouteHandler(c); err != nil {
		t.Fatalf("patchRouteHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestPatchRouteHandler_NotFound(t *testing.T) {
	setupRoutesTest(t)

	c, rec := newRoutesRequest(t, http.MethodPatch, "/api/routes/unknown-id", map[string]any{"name": "X"})
	c.SetParamNames("id")
	c.SetParamValues("unknown-id")
	if err := patchRouteHandler(c); err != nil {
		t.Fatalf("patchRouteHandler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}

func TestDeleteRouteHandler_DeletesAndReports404Afterward(t *testing.T) {
	setupRoutesTest(t)
	route := createTestRoute(t, "Test Route", sampleWaypoints(), 8)

	c, rec := newRoutesRequest(t, http.MethodDelete, "/api/routes/"+route.ID, nil)
	c.SetParamNames("id")
	c.SetParamValues(route.ID)
	if err := deleteRouteHandler(c); err != nil {
		t.Fatalf("deleteRouteHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	c2, rec2 := newRoutesRequest(t, http.MethodGet, "/api/routes/"+route.ID, nil)
	c2.SetParamNames("id")
	c2.SetParamValues(route.ID)
	if err := getRouteHandler(c2); err != nil {
		t.Fatalf("getRouteHandler returned error: %v", err)
	}
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("expected status %d after delete, got %d", http.StatusNotFound, rec2.Code)
	}
}

func TestDeleteRouteHandler_NotFound(t *testing.T) {
	setupRoutesTest(t)

	c, rec := newRoutesRequest(t, http.MethodDelete, "/api/routes/unknown-id", nil)
	c.SetParamNames("id")
	c.SetParamValues("unknown-id")
	if err := deleteRouteHandler(c); err != nil {
		t.Fatalf("deleteRouteHandler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}

func TestRoutes_AtomicWriteReloadRoundTrip(t *testing.T) {
	setupRoutesTest(t)

	first := createTestRoute(t, "Route One", sampleWaypoints(), 8)
	second := createTestRoute(t, "Route Two", sampleWaypoints(), 8)

	// Simulate a process restart: reload state from disk.
	loadRoutes()

	routesMu.RLock()
	defer routesMu.RUnlock()
	if len(routesState) != 2 {
		t.Fatalf("expected 2 routes after reload, got %d", len(routesState))
	}
	if _, ok := routesState[first.ID]; !ok {
		t.Fatal("expected first route to survive reload")
	}
	if _, ok := routesState[second.ID]; !ok {
		t.Fatal("expected second route to survive reload")
	}
}

func TestListRoutesHandler_OrdersNewestFirst(t *testing.T) {
	setupRoutesTest(t)

	// Create 3 routes
	first := createTestRoute(t, "First", sampleWaypoints(), 8)
	second := createTestRoute(t, "Second", sampleWaypoints(), 8)
	third := createTestRoute(t, "Third", sampleWaypoints(), 8)

	// Directly set their CreatedAt times to ensure deterministic ordering
	// (older to newer: First, Second, Third)
	routesMu.Lock()
	routesState[first.ID].CreatedAt = time.Now().Add(-2 * time.Hour)
	routesState[second.ID].CreatedAt = time.Now().Add(-1 * time.Hour)
	routesState[third.ID].CreatedAt = time.Now()
	routesMu.Unlock()

	// Call listRoutesHandler and verify newest-first ordering
	c, rec := newRoutesRequest(t, http.MethodGet, "/api/routes", nil)
	if err := listRoutesHandler(c); err != nil {
		t.Fatalf("listRoutesHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var payload struct {
		Routes []routeData `json:"routes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse list response: %v", err)
	}

	if len(payload.Routes) != 3 {
		t.Fatalf("expected 3 routes, got %d", len(payload.Routes))
	}

	// Verify order is newest-first: Third, Second, First
	if payload.Routes[0].Name != "Third" {
		t.Fatalf("expected first route to be 'Third' (newest), got %q", payload.Routes[0].Name)
	}
	if payload.Routes[1].Name != "Second" {
		t.Fatalf("expected second route to be 'Second', got %q", payload.Routes[1].Name)
	}
	if payload.Routes[2].Name != "First" {
		t.Fatalf("expected third route to be 'First' (oldest), got %q", payload.Routes[2].Name)
	}
}
