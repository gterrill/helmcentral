package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func setupRouteActivationTest(t *testing.T) {
	t.Helper()
	setupRoutesTest(t)
	t.Setenv("SIGNALK_USERNAME", "")
	t.Setenv("SIGNALK_PASSWORD", "")
	invalidateSignalKToken()
	t.Cleanup(invalidateSignalKToken)
}

type capturedRequest struct {
	Method string
	Path   string
	Body   []byte
}

// recordingServer captures every request it receives and dispatches by exact
// "METHOD path" key to a configurable response. Unmatched requests 404.
type recordingServer struct {
	mu        sync.Mutex
	requests  []capturedRequest
	responses map[string]struct {
		status int
		body   string
	}
}

func newRecordingServer(t *testing.T) (*httptest.Server, *recordingServer) {
	t.Helper()
	rs := &recordingServer{responses: map[string]struct {
		status int
		body   string
	}{}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		rs.mu.Lock()
		rs.requests = append(rs.requests, capturedRequest{Method: r.Method, Path: r.URL.Path, Body: body})
		resp, ok := rs.responses[r.Method+" "+r.URL.Path]
		rs.mu.Unlock()

		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.status)
		_, _ = w.Write([]byte(resp.body))
	}))
	return srv, rs
}

func (rs *recordingServer) on(method, path string, status int, body string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.responses[method+" "+path] = struct {
		status int
		body   string
	}{status: status, body: body}
}

func (rs *recordingServer) calls() []capturedRequest {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return append([]capturedRequest(nil), rs.requests...)
}

const (
	resourcePathPrefix = "/signalk/v2/api/resources/routes/"
	coursePutPath      = "/signalk/v2/api/vessels/self/navigation/course/activeRoute"
	courseGetPath      = "/signalk/v2/api/vessels/self/navigation/course"
)

// ── Pure function tests ──────────────────────────────────────────────────────

func TestRouteDistanceMeters_SumsConsecutiveLegs(t *testing.T) {
	waypoints := []routeWaypoint{{Lat: 0, Lon: 0}, {Lat: 1, Lon: 0}, {Lat: 2, Lon: 0}}
	expected := haversineMeters(0, 0, 1, 0) + haversineMeters(1, 0, 2, 0)
	if got := routeDistanceMeters(waypoints); got != expected {
		t.Fatalf("expected %f, got %f", expected, got)
	}
}

func TestRouteDistanceMeters_ZeroForSingleWaypoint(t *testing.T) {
	if got := routeDistanceMeters([]routeWaypoint{{Lat: 0, Lon: 0}}); got != 0 {
		t.Fatalf("expected 0, got %f", got)
	}
}

func TestRouteToSignalKResource_FlipsCoordinatesAndComputesDistance(t *testing.T) {
	route := &routeData{
		ID:   "abc",
		Name: "Test Route",
		Waypoints: []routeWaypoint{
			{Lat: 10, Lon: 20},
			{Lat: 11, Lon: 21},
		},
	}

	resource := routeToSignalKResource(route)

	if resource.Name != "Test Route" {
		t.Fatalf("expected name to round-trip, got %q", resource.Name)
	}
	if resource.Feature.Type != "Feature" || resource.Feature.Geometry.Type != "LineString" {
		t.Fatalf("expected GeoJSON Feature/LineString, got %+v", resource.Feature)
	}
	if len(resource.Feature.Geometry.Coordinates) != 2 {
		t.Fatalf("expected 2 coordinate pairs, got %d", len(resource.Feature.Geometry.Coordinates))
	}
	if resource.Feature.Geometry.Coordinates[0] != [2]float64{20, 10} {
		t.Fatalf("expected [lon,lat] = [20,10], got %v", resource.Feature.Geometry.Coordinates[0])
	}
	expectedDistance := haversineMeters(10, 20, 11, 21)
	if resource.Distance != expectedDistance {
		t.Fatalf("expected distance %f, got %f", expectedDistance, resource.Distance)
	}
}

func TestSignalkRouteHref_RoundTrips(t *testing.T) {
	if got := signalkRouteHref("abc"); got != "/resources/routes/abc" {
		t.Fatalf("expected /resources/routes/abc, got %q", got)
	}
	if got := routeIDFromSignalKHref("/resources/routes/abc"); got != "abc" {
		t.Fatalf("expected abc, got %q", got)
	}
	if got := routeIDFromSignalKHref("/resources/waypoints/xyz"); got != "" {
		t.Fatalf("expected empty string for non-route href, got %q", got)
	}
}

// ── activateRouteHandler ──────────────────────────────────────────────────────

func TestActivateRouteHandler_Success(t *testing.T) {
	setupRouteActivationTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	route := createTestRoute(t, "Marina to Anchorage", sampleWaypoints(), 8)
	rs.on(http.MethodPut, resourcePathPrefix+route.ID, http.StatusOK, "{}")
	rs.on(http.MethodPut, coursePutPath, http.StatusOK, "{}")

	settingsPath := settingsFileForServer(t, srv.URL)
	c, rec := newRoutesRequest(t, http.MethodPost, "/api/routes/"+route.ID+"/activate", nil)
	c.SetParamNames("id")
	c.SetParamValues(route.ID)

	t.Setenv("SETTINGS_FILE", settingsPath)
	if err := activateRouteHandler(c); err != nil {
		t.Fatalf("activateRouteHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	calls := rs.calls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 upstream calls, got %d: %+v", len(calls), calls)
	}

	var resourceBody map[string]any
	if err := json.Unmarshal(calls[0].Body, &resourceBody); err != nil {
		t.Fatalf("failed to parse resource PUT body: %v", err)
	}
	if _, wrapped := resourceBody["value"]; wrapped {
		t.Fatal("resource PUT body must not be wrapped in {\"value\": ...}")
	}
	if resourceBody["feature"] == nil {
		t.Fatalf("expected resource body to contain a feature key, got %+v", resourceBody)
	}

	var courseBody map[string]any
	if err := json.Unmarshal(calls[1].Body, &courseBody); err != nil {
		t.Fatalf("failed to parse course PUT body: %v", err)
	}
	if courseBody["href"] != signalkRouteHref(route.ID) {
		t.Fatalf("expected href %q, got %v", signalkRouteHref(route.ID), courseBody["href"])
	}
}

func TestActivateRouteHandler_RouteNotFound(t *testing.T) {
	setupRouteActivationTest(t)

	c, rec := newRoutesRequest(t, http.MethodPost, "/api/routes/unknown-id/activate", nil)
	c.SetParamNames("id")
	c.SetParamValues("unknown-id")

	if err := activateRouteHandler(c); err != nil {
		t.Fatalf("activateRouteHandler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}

func TestActivateRouteHandler_PropagatesSignalKErrorBody(t *testing.T) {
	setupRouteActivationTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	route := createTestRoute(t, "Test Route", sampleWaypoints(), 8)
	rs.on(http.MethodPut, resourcePathPrefix+route.ID, http.StatusBadRequest, `{"message":"bad geometry"}`)

	settingsPath := settingsFileForServer(t, srv.URL)
	c, rec := newRoutesRequest(t, http.MethodPost, "/api/routes/"+route.ID+"/activate", nil)
	c.SetParamNames("id")
	c.SetParamValues(route.ID)

	t.Setenv("SETTINGS_FILE", settingsPath)
	if err := activateRouteHandler(c); err != nil {
		t.Fatalf("activateRouteHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status %d, got %d", http.StatusBadGateway, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "bad geometry") {
		t.Fatalf("expected raw signalk error body to be surfaced, got %s", rec.Body.String())
	}
}

func TestActivateRouteHandler_ResourcePutSucceedsButCoursePutFails(t *testing.T) {
	setupRouteActivationTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	route := createTestRoute(t, "Test Route", sampleWaypoints(), 8)
	rs.on(http.MethodPut, resourcePathPrefix+route.ID, http.StatusOK, "{}")
	rs.on(http.MethodPut, coursePutPath, http.StatusInternalServerError, `{"message":"course rejected"}`)

	settingsPath := settingsFileForServer(t, srv.URL)
	c, rec := newRoutesRequest(t, http.MethodPost, "/api/routes/"+route.ID+"/activate", nil)
	c.SetParamNames("id")
	c.SetParamValues(route.ID)

	t.Setenv("SETTINGS_FILE", settingsPath)
	if err := activateRouteHandler(c); err != nil {
		t.Fatalf("activateRouteHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status %d, got %d", http.StatusBadGateway, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "course rejected") {
		t.Fatalf("expected course PUT error to be surfaced, got %s", rec.Body.String())
	}
}

// ── deactivateRouteHandler ────────────────────────────────────────────────────

func TestDeactivateRouteHandler_Success(t *testing.T) {
	setupRouteActivationTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	rs.on(http.MethodDelete, courseGetPath, http.StatusOK, "{}")

	settingsPath := settingsFileForServer(t, srv.URL)
	c, rec := newRoutesRequest(t, http.MethodPost, "/api/routes/deactivate", nil)

	t.Setenv("SETTINGS_FILE", settingsPath)
	if err := deactivateRouteHandler(c); err != nil {
		t.Fatalf("deactivateRouteHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestDeactivateRouteHandler_PropagatesSignalKErrorBody(t *testing.T) {
	setupRouteActivationTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	rs.on(http.MethodDelete, courseGetPath, http.StatusInternalServerError, `{"message":"boom"}`)

	settingsPath := settingsFileForServer(t, srv.URL)
	c, rec := newRoutesRequest(t, http.MethodPost, "/api/routes/deactivate", nil)

	t.Setenv("SETTINGS_FILE", settingsPath)
	if err := deactivateRouteHandler(c); err != nil {
		t.Fatalf("deactivateRouteHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status %d, got %d", http.StatusBadGateway, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "boom") {
		t.Fatalf("expected raw signalk error body to be surfaced, got %s", rec.Body.String())
	}
}

// ── getActiveRouteHandler ──────────────────────────────────────────────────────

func TestGetActiveRouteHandler_NoneActive(t *testing.T) {
	setupRouteActivationTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	rs.on(http.MethodGet, courseGetPath, http.StatusOK, `{}`)

	settingsPath := settingsFileForServer(t, srv.URL)
	c, rec := newRoutesRequest(t, http.MethodGet, "/api/routes/active", nil)

	t.Setenv("SETTINGS_FILE", settingsPath)
	if err := getActiveRouteHandler(c); err != nil {
		t.Fatalf("getActiveRouteHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if payload["active"] != false {
		t.Fatalf("expected active=false, got %v", payload["active"])
	}
}

func TestGetActiveRouteHandler_ActiveMatchesLocalRoute(t *testing.T) {
	setupRouteActivationTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	route := createTestRoute(t, "Known Route", sampleWaypoints(), 8)
	rs.on(http.MethodGet, courseGetPath, http.StatusOK,
		`{"activeRoute":{"href":"`+signalkRouteHref(route.ID)+`","pointIndex":1,"reverse":false}}`)

	settingsPath := settingsFileForServer(t, srv.URL)
	c, rec := newRoutesRequest(t, http.MethodGet, "/api/routes/active", nil)

	t.Setenv("SETTINGS_FILE", settingsPath)
	if err := getActiveRouteHandler(c); err != nil {
		t.Fatalf("getActiveRouteHandler returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if payload["active"] != true {
		t.Fatalf("expected active=true, got %v", payload["active"])
	}
	if payload["route_id"] != route.ID {
		t.Fatalf("expected route_id %q, got %v", route.ID, payload["route_id"])
	}
	if payload["route_name"] != "Known Route" {
		t.Fatalf("expected route_name 'Known Route', got %v", payload["route_name"])
	}
	if payload["point_index"] != float64(1) {
		t.Fatalf("expected point_index 1, got %v", payload["point_index"])
	}
}

func TestGetActiveRouteHandler_ActiveHrefNotLocallyKnown(t *testing.T) {
	setupRouteActivationTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	rs.on(http.MethodGet, courseGetPath, http.StatusOK,
		`{"activeRoute":{"href":"/resources/routes/some-foreign-id","pointIndex":0,"reverse":false}}`)

	settingsPath := settingsFileForServer(t, srv.URL)
	c, rec := newRoutesRequest(t, http.MethodGet, "/api/routes/active", nil)

	t.Setenv("SETTINGS_FILE", settingsPath)
	if err := getActiveRouteHandler(c); err != nil {
		t.Fatalf("getActiveRouteHandler returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if payload["active"] != true {
		t.Fatalf("expected active=true, got %v", payload["active"])
	}
	if payload["route_id"] != nil {
		t.Fatalf("expected route_id to be nil for a foreign href, got %v", payload["route_id"])
	}
}

func TestGetActiveRouteHandler_SignalKUnreachable(t *testing.T) {
	setupRouteActivationTest(t)

	// A closed server: connections fail immediately.
	srv, _ := newRecordingServer(t)
	srv.Close()

	settingsPath := settingsFileForServer(t, srv.URL)
	c, rec := newRoutesRequest(t, http.MethodGet, "/api/routes/active", nil)

	t.Setenv("SETTINGS_FILE", settingsPath)
	if err := getActiveRouteHandler(c); err != nil {
		t.Fatalf("getActiveRouteHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status %d (not 200 active:false) when signalk is unreachable, got %d", http.StatusBadGateway, rec.Code)
	}
}

// ── auth retry ────────────────────────────────────────────────────────────────

func TestSignalkRequestJSONWithAuth_RetriesOnAuthFailure(t *testing.T) {
	setupRouteActivationTest(t)
	t.Setenv("SIGNALK_USERNAME", "u")
	t.Setenv("SIGNALK_PASSWORD", "p")

	srv, rs := newRecordingServer(t)
	defer srv.Close()

	rs.on(http.MethodPost, "/signalk/v1/auth/login", http.StatusOK, `{"token":"test-token","timeToLive":3600}`)

	callCount := 0
	var mu sync.Mutex
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/signalk/v1/auth/login" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"token":"test-token","timeToLive":3600}`))
			return
		}
		if r.Method == http.MethodPut && r.URL.Path == coursePutPath {
			mu.Lock()
			callCount++
			n := callCount
			mu.Unlock()
			if n == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	})

	settingsPath := settingsFileForServer(t, srv.URL)
	err := signalkRequestJSONWithAuth(srv.URL, settingsPath, coursePutPath, http.MethodPut, map[string]any{"href": "/resources/routes/x"})
	if err != nil {
		t.Fatalf("expected retry to succeed, got error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if callCount != 2 {
		t.Fatalf("expected 2 attempts (401 then success), got %d", callCount)
	}
}
