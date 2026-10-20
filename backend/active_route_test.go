package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

const (
	courseGetPath      = "/signalk/v2/api/vessels/self/navigation/course"
	resourcePathPrefix = "/signalk/v2/api/resources/routes/"
)

func newActiveRouteRequest(t *testing.T, method, path string, body any) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	return echo.New().NewContext(req, rec), rec
}

func getActive(t *testing.T, rs *recordingServer, srvURL string) (int, map[string]any) {
	t.Helper()
	t.Setenv("SETTINGS_FILE", settingsFileForServer(t, srvURL))
	c, rec := newActiveRouteRequest(t, http.MethodGet, "/api/routes/active", nil)
	if err := getActiveRouteHandler(c); err != nil {
		t.Fatalf("getActiveRouteHandler returned error: %v", err)
	}
	var payload map[string]any
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
	} else {
		payload = map[string]any{"raw": rec.Body.String()}
	}
	return rec.Code, payload
}

// ── GET /api/routes/active: route resource ──────────────────────────────────

func TestGetActiveRouteHandler_ReadsRouteResourceWithCoordinatesMeta(t *testing.T) {
	setupSignalKRestTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	rs.on(http.MethodGet, courseGetPath, http.StatusOK,
		`{"activeRoute":{"href":"/resources/routes/abc-123","pointIndex":1,"reverse":false}}`)
	rs.on(http.MethodGet, resourcePathPrefix+"abc-123", http.StatusOK,
		`{"name":"Whitsunday Run","feature":{"type":"Feature","geometry":{"type":"LineString","coordinates":[[146.8,-19.2],[148.1,-20.3],[149.0,-20.9]]},`+
			`"properties":{"coordinatesMeta":[{"name":"Start"},{},{"name":"Finish"}]}}}`)

	code, payload := getActive(t, rs, srv.URL)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", code, payload)
	}
	if payload["active"] != true || payload["point_index"] != float64(1) || payload["reverse"] != false {
		t.Fatalf("unexpected active fields: %+v", payload)
	}
	route, ok := payload["route"].(map[string]any)
	if !ok {
		t.Fatalf("expected a route object, got %+v", payload)
	}
	if route["name"] != "Whitsunday Run" {
		t.Fatalf("expected route name, got %v", route["name"])
	}
	wps, _ := route["waypoints"].([]any)
	if len(wps) != 3 {
		t.Fatalf("expected 3 waypoints, got %+v", route["waypoints"])
	}
	first := wps[0].(map[string]any)
	if first["lat"] != -19.2 || first["lon"] != 146.8 || first["name"] != "Start" {
		t.Fatalf("expected [lon,lat] flipped to lat/lon with name, got %+v", first)
	}
	second := wps[1].(map[string]any)
	if second["lat"] != -20.3 || second["lon"] != 148.1 {
		t.Fatalf("unexpected second waypoint %+v", second)
	}
	if name, has := second["name"]; has && name != "" {
		t.Fatalf("expected no name for an unnamed point, got %v", name)
	}
	if wps[2].(map[string]any)["name"] != "Finish" {
		t.Fatalf("expected third name Finish, got %+v", wps[2])
	}
}

func TestGetActiveRouteHandler_RouteResourceWithoutCoordinatesMetaAndReverse(t *testing.T) {
	setupSignalKRestTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	rs.on(http.MethodGet, courseGetPath, http.StatusOK,
		`{"activeRoute":{"href":"/resources/routes/r2","pointIndex":0,"reverse":true}}`)
	rs.on(http.MethodGet, resourcePathPrefix+"r2", http.StatusOK,
		`{"name":"Back Home","feature":{"type":"Feature","geometry":{"type":"LineString","coordinates":[[1,2],[3,4]]},"properties":{}}}`)

	code, payload := getActive(t, rs, srv.URL)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", code, payload)
	}
	if payload["reverse"] != true {
		t.Fatalf("expected reverse=true passed through, got %v", payload["reverse"])
	}
	route := payload["route"].(map[string]any)
	wps := route["waypoints"].([]any)
	// Served in authored order; the frontend applies reverse.
	if len(wps) != 2 || wps[0].(map[string]any)["lat"] != float64(2) || wps[0].(map[string]any)["lon"] != float64(1) {
		t.Fatalf("expected authored order lat/lon, got %+v", wps)
	}
}

func TestGetActiveRouteHandler_RouteResourceFetchFailureIs502(t *testing.T) {
	setupSignalKRestTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	rs.on(http.MethodGet, courseGetPath, http.StatusOK,
		`{"activeRoute":{"href":"/resources/routes/gone","pointIndex":0,"reverse":false}}`)
	rs.on(http.MethodGet, resourcePathPrefix+"gone", http.StatusInternalServerError, `{"message":"resource provider exploded"}`)

	code, payload := getActive(t, rs, srv.URL)
	if code != http.StatusBadGateway {
		t.Fatalf("expected 502 when the route resource can't be read, got %d: %v", code, payload)
	}
	if raw, _ := payload["raw"].(string); !strings.Contains(raw, "resource provider exploded") {
		t.Fatalf("expected SignalK's error text to be surfaced, got %v", payload)
	}
}

func TestGetActiveRouteHandler_RouteResourceNotFoundIs502(t *testing.T) {
	setupSignalKRestTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	rs.on(http.MethodGet, courseGetPath, http.StatusOK,
		`{"activeRoute":{"href":"/resources/routes/missing","pointIndex":0,"reverse":false}}`)

	code, _ := getActive(t, rs, srv.URL)
	if code != http.StatusBadGateway {
		t.Fatalf("expected 502 for a 404 route resource, got %d", code)
	}
}

func TestGetActiveRouteHandler_RouteWithNextPointAlsoCarriesDestination(t *testing.T) {
	setupSignalKRestTest(t)
	resetPlaceNameCache(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	rs.on(http.MethodGet, courseGetPath, http.StatusOK,
		`{"activeRoute":{"href":"/resources/routes/r3","pointIndex":0,"reverse":false},`+
			`"nextPoint":{"position":{"latitude":-18.6675,"longitude":146.48466666666667},"type":"RoutePoint"}}`)
	rs.on(http.MethodGet, resourcePathPrefix+"r3", http.StatusOK,
		`{"name":"R3","feature":{"type":"Feature","geometry":{"type":"LineString","coordinates":[[146.48466666666667,-18.6675],[147,-19]]},"properties":{}}}`)

	code, payload := getActive(t, rs, srv.URL)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", code, payload)
	}
	dest, ok := payload["destination"].(map[string]any)
	if !ok || dest["lat"] != -18.6675 {
		t.Fatalf("expected destination alongside the route, got %+v", payload)
	}
}

func TestGetActiveRouteHandler_NoHrefWithNextPointIsDestinationOnly(t *testing.T) {
	setupSignalKRestTest(t)
	resetPlaceNameCache(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	rs.on(http.MethodGet, courseGetPath, http.StatusOK,
		`{"activeRoute":null,"nextPoint":{"position":{"latitude":-18.6675,"longitude":146.48466666666667},"type":"Location"}}`)

	code, payload := getActive(t, rs, srv.URL)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", code, payload)
	}
	if payload["active"] != false {
		t.Fatalf("expected active=false, got %v", payload["active"])
	}
	if _, has := payload["route"]; has {
		t.Fatalf("expected no route key without an active route, got %+v", payload)
	}
	if _, ok := payload["destination"].(map[string]any); !ok {
		t.Fatalf("expected destination, got %+v", payload)
	}
	for _, call := range rs.calls() {
		if call.Path != courseGetPath {
			t.Fatalf("expected only the course to be read, also saw %s", call.Path)
		}
	}
}

func TestGetActiveRouteHandler_ForeignHrefIsNotFetchedAsRoute(t *testing.T) {
	setupSignalKRestTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	rs.on(http.MethodGet, courseGetPath, http.StatusOK,
		`{"activeRoute":{"href":"/resources/waypoints/w1","pointIndex":0,"reverse":false}}`)

	code, _ := getActive(t, rs, srv.URL)
	if code != http.StatusBadGateway {
		t.Fatalf("expected 502 for an active route that is not a routes resource, got %d", code)
	}
}

func TestGetActiveRouteHandler_NoneActive(t *testing.T) {
	setupSignalKRestTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	rs.on(http.MethodGet, courseGetPath, http.StatusOK, `{}`)

	settingsPath := settingsFileForServer(t, srv.URL)
	c, rec := newActiveRouteRequest(t, http.MethodGet, "/api/routes/active", nil)

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

func TestGetActiveRouteHandler_NoActiveRouteWithNextPointReturnsDestination(t *testing.T) {
	setupSignalKRestTest(t)
	resetPlaceNameCache(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	rs.on(http.MethodGet, courseGetPath, http.StatusOK,
		`{"activeRoute":null,"nextPoint":{"position":{"latitude":-18.6675,"longitude":146.48466666666667},"type":"Location"}}`)

	settingsPath := settingsFileForServer(t, srv.URL)
	c, rec := newActiveRouteRequest(t, http.MethodGet, "/api/routes/active", nil)

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
	setupSignalKRestTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	rs.on(http.MethodGet, courseGetPath, http.StatusOK, `{}`)

	settingsPath := settingsFileForServer(t, srv.URL)
	c, rec := newActiveRouteRequest(t, http.MethodGet, "/api/routes/active", nil)

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
	setupSignalKRestTest(t)
	resetPlaceNameCache(t)

	placeNameCache.put(placeNameCacheKey(-18.6675, 146.48466666666667), "Hook Island")

	srv, rs := newRecordingServer(t)
	defer srv.Close()
	rs.on(http.MethodGet, courseGetPath, http.StatusOK,
		`{"activeRoute":null,"nextPoint":{"position":{"latitude":-18.6675,"longitude":146.48466666666667},"type":"Location"}}`)

	settingsPath := settingsFileForServer(t, srv.URL)
	c, rec := newActiveRouteRequest(t, http.MethodGet, "/api/routes/active", nil)

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

func TestGetActiveRouteHandler_SignalKUnreachable(t *testing.T) {
	setupSignalKRestTest(t)

	// A closed server: connections fail immediately.
	srv, _ := newRecordingServer(t)
	srv.Close()

	settingsPath := settingsFileForServer(t, srv.URL)
	c, rec := newActiveRouteRequest(t, http.MethodGet, "/api/routes/active", nil)

	t.Setenv("SETTINGS_FILE", settingsPath)
	if err := getActiveRouteHandler(c); err != nil {
		t.Fatalf("getActiveRouteHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status %d (not 200 active:false) when signalk is unreachable, got %d", http.StatusBadGateway, rec.Code)
	}
}
