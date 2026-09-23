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
	withTestSecretsStore(t) // a store with nothing in it: no credential configured
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

// ── nextPoint parsing (ADR 0125: the trip ETA for a bare chartplotter
// go-to destination, not just a Helmcentral-activated route) ───────────────

func TestFetchSignalKCourseStatus_ParsesNextPointWhenNoActiveRoute(t *testing.T) {
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	// The real payload shape observed live: activeRoute null, nextPoint set
	// from a chartplotter go-to.
	rs.on(http.MethodGet, courseGetPath, http.StatusOK,
		`{"activeRoute":null,"nextPoint":{"position":{"latitude":-18.6675,"longitude":146.48466666666667},"type":"Location"}}`)

	status, err := fetchSignalKCourseStatus(srv.URL)
	if err != nil {
		t.Fatalf("fetchSignalKCourseStatus returned error: %v", err)
	}
	if status.ActiveRouteHref != "" {
		t.Fatalf("expected no active route href, got %q", status.ActiveRouteHref)
	}
	if !status.HasNextPoint {
		t.Fatal("expected HasNextPoint true for a chartplotter go-to destination")
	}
	if status.NextPointLat != -18.6675 || status.NextPointLon != 146.48466666666667 {
		t.Fatalf("expected next point position to round-trip, got lat=%v lon=%v", status.NextPointLat, status.NextPointLon)
	}
	if status.NextPointType != "Location" {
		t.Fatalf("expected next point type %q, got %q", "Location", status.NextPointType)
	}
}

func TestFetchSignalKCourseStatus_NoCourseAtAll(t *testing.T) {
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	rs.on(http.MethodGet, courseGetPath, http.StatusOK, `{"activeRoute":null,"nextPoint":null}`)

	status, err := fetchSignalKCourseStatus(srv.URL)
	if err != nil {
		t.Fatalf("fetchSignalKCourseStatus returned error: %v", err)
	}
	if status.ActiveRouteHref != "" {
		t.Fatalf("expected no active route href, got %q", status.ActiveRouteHref)
	}
	if status.HasNextPoint {
		t.Fatal("expected HasNextPoint false when nextPoint is null")
	}
}

func TestFetchSignalKCourseStatus_ActiveRouteResponseUnaffectedByNextPointParsing(t *testing.T) {
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	// An active-route payload with no nextPoint key at all (the shape the
	// existing active-route tests already use) must round-trip exactly as
	// before - adding nextPoint parsing must not disturb it.
	rs.on(http.MethodGet, courseGetPath, http.StatusOK,
		`{"activeRoute":{"href":"/resources/routes/abc","pointIndex":2,"reverse":true}}`)

	status, err := fetchSignalKCourseStatus(srv.URL)
	if err != nil {
		t.Fatalf("fetchSignalKCourseStatus returned error: %v", err)
	}
	if status.ActiveRouteHref != "/resources/routes/abc" || status.PointIndex != 2 || !status.Reverse {
		t.Fatalf("expected active route fields to round-trip unchanged, got %+v", status)
	}
	if status.HasNextPoint {
		t.Fatal("expected HasNextPoint false when the payload carries no nextPoint")
	}
}

// TestGetActiveRouteHandler_ActiveHrefNotLocallyKnownWithNextPointReturnsDestination
// is the regression for the bug this fix targets: a route activated OUTSIDE
// Helmcentral (activeRoute.href doesn't match anything in routesState, so
// route_id comes back nil) used to get no destination at all, even though
// the Course API's nextPoint was right there - destination was only ever
// added on the "nothing active" branch. The clock tile's trip ETA needs the
// destination in this case exactly as much as the inactive one.
func TestGetActiveRouteHandler_ActiveHrefNotLocallyKnownWithNextPointReturnsDestination(t *testing.T) {
	setupRouteActivationTest(t)
	resetPlaceNameCache(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	rs.on(http.MethodGet, courseGetPath, http.StatusOK,
		`{"activeRoute":{"href":"/resources/routes/some-foreign-id","pointIndex":0,"reverse":false},`+
			`"nextPoint":{"position":{"latitude":-18.6675,"longitude":146.48466666666667},"type":"Location"}}`)

	settingsPath := settingsFileForServer(t, srv.URL)
	c, rec := newRoutesRequest(t, http.MethodGet, "/api/routes/active", nil)

	t.Setenv("SETTINGS_FILE", settingsPath)
	if err := getActiveRouteHandler(c); err != nil {
		t.Fatalf("getActiveRouteHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if payload["active"] != true {
		t.Fatalf("expected active=true, got %v", payload["active"])
	}
	if payload["route_id"] != nil {
		t.Fatalf("expected route_id nil for a foreign href, got %v", payload["route_id"])
	}
	dest, ok := payload["destination"].(map[string]any)
	if !ok {
		t.Fatalf("expected a destination object even though the active route isn't a local one, got %+v", payload)
	}
	if dest["lat"] != -18.6675 || dest["lon"] != 146.48466666666667 {
		t.Fatalf("expected destination position to round-trip, got %+v", dest)
	}
}

// ── GET /api/routes/active: destination (ADR 0125) ─────────────────────────

func TestGetActiveRouteHandler_NoActiveRouteWithNextPointReturnsDestination(t *testing.T) {
	setupRouteActivationTest(t)
	resetPlaceNameCache(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	rs.on(http.MethodGet, courseGetPath, http.StatusOK,
		`{"activeRoute":null,"nextPoint":{"position":{"latitude":-18.6675,"longitude":146.48466666666667},"type":"Location"}}`)

	settingsPath := settingsFileForServer(t, srv.URL)
	c, rec := newRoutesRequest(t, http.MethodGet, "/api/routes/active", nil)

	t.Setenv("SETTINGS_FILE", settingsPath)
	if err := getActiveRouteHandler(c); err != nil {
		t.Fatalf("getActiveRouteHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if payload["active"] != false {
		t.Fatalf("expected active=false, got %v", payload["active"])
	}
	dest, ok := payload["destination"].(map[string]any)
	if !ok {
		t.Fatalf("expected a destination object, got %+v", payload)
	}
	if dest["lat"] != -18.6675 || dest["lon"] != 146.48466666666667 {
		t.Fatalf("expected destination position to round-trip, got %+v", dest)
	}
	if _, hasName := dest["name"]; !hasName {
		t.Fatalf("expected a name key (empty string on a cache miss) to be present, got %+v", dest)
	}
}

func TestGetActiveRouteHandler_NoActiveRouteNoNextPointOmitsDestination(t *testing.T) {
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

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if _, hasDest := payload["destination"]; hasDest {
		t.Fatalf("expected no destination key when there is no next point, got %+v", payload)
	}
}

func TestGetActiveRouteHandler_DestinationServesCachedNameWithoutBlocking(t *testing.T) {
	setupRouteActivationTest(t)
	resetPlaceNameCache(t)

	placeNameCache.put(placeNameCacheKey(-18.6675, 146.48466666666667), "Hook Island")

	srv, rs := newRecordingServer(t)
	defer srv.Close()
	rs.on(http.MethodGet, courseGetPath, http.StatusOK,
		`{"activeRoute":null,"nextPoint":{"position":{"latitude":-18.6675,"longitude":146.48466666666667},"type":"Location"}}`)

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
	dest, ok := payload["destination"].(map[string]any)
	if !ok {
		t.Fatalf("expected a destination object, got %+v", payload)
	}
	if dest["name"] != "Hook Island" {
		t.Fatalf("expected the cached name to be served, got %+v", dest)
	}
}

func TestGetActiveRouteHandler_ActiveRouteResponseUnaffectedByDestinationField(t *testing.T) {
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
	if _, hasDest := payload["destination"]; hasDest {
		t.Fatalf("expected no destination key on an active-route response, got %+v", payload)
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
	withSeededSecretsStore(t, map[string]string{
		"SIGNALK_USERNAME": "u",
		"SIGNALK_PASSWORD": "p",
	})

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
