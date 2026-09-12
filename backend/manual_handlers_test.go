package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// newManualEchoContext mirrors newAssistantEchoContext (assistant_handlers_test.go)
// but sets the "*" wildcard param getManualPageHandler reads, since a manual
// page id contains a slash ("features/dashboard") and cannot be a normal
// echo :param.
func newManualEchoContext(path, wildcard string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("*")
	c.SetParamValues(wildcard)
	return c, rec
}

func TestGetManualPageHandler_NestedIDReturns200(t *testing.T) {
	pages, err := loadManual(testManualFS(), "manual")
	if err != nil {
		t.Fatalf("loadManual: %v", err)
	}
	handler := getManualPageHandler(func() []manualPage { return pages })

	c, rec := newManualEchoContext("/api/manual/features/forecast", "features/forecast")
	if err := handler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.ID != "features/forecast" {
		t.Errorf("expected id %q, got %q", "features/forecast", body.ID)
	}
	if body.Title != "Forecast" {
		t.Errorf("expected title %q, got %q", "Forecast", body.Title)
	}
	if !strings.Contains(body.Body, "Upper air, days ahead") {
		t.Errorf("expected the whole page body, got %q", body.Body)
	}
}

func TestGetManualPageHandler_UnknownIDReturns404(t *testing.T) {
	pages, err := loadManual(testManualFS(), "manual")
	if err != nil {
		t.Fatalf("loadManual: %v", err)
	}
	handler := getManualPageHandler(func() []manualPage { return pages })

	c, rec := newManualEchoContext("/api/manual/nope", "nope")
	if err := handler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if want := `unknown manual page "nope"`; body["error"] != want {
		t.Errorf("expected error %q, got %q", want, body["error"])
	}
}

func TestGetManualPageHandler_EmptyIDReturns404(t *testing.T) {
	pages, err := loadManual(testManualFS(), "manual")
	if err != nil {
		t.Fatalf("loadManual: %v", err)
	}
	handler := getManualPageHandler(func() []manualPage { return pages })

	c, rec := newManualEchoContext("/api/manual/", "")
	if err := handler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an empty page id, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestGetManualPageHandler_PathTraversalIsJustAnUnknownID guards against
// treating ".." specially: manual page ids are matched by exact string
// comparison over the loaded slice (getManualPageHandler), so a
// "../etc/passwd"-shaped id can never resolve to anything outside the
// staged manual tree - it simply isn't a known id.
func TestGetManualPageHandler_PathTraversalIsJustAnUnknownID(t *testing.T) {
	pages, err := loadManual(testManualFS(), "manual")
	if err != nil {
		t.Fatalf("loadManual: %v", err)
	}
	handler := getManualPageHandler(func() []manualPage { return pages })

	c, rec := newManualEchoContext("/api/manual/../etc/passwd", "../etc/passwd")
	if err := handler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestGetManualPageHandler_EmptyManualReturns503 covers a build that never
// ran `make manual-stage`: the route exists, so this is 503 (a build
// problem the operator can fix), never 404 (an unknown page). Same sentence
// executeReadManual uses (assistant_tools.go), so the operator sees one
// consistent message regardless of whether they hit this from Mate or the
// Manual sheet.
func TestGetManualPageHandler_EmptyManualReturns503(t *testing.T) {
	handler := getManualPageHandler(func() []manualPage { return nil })

	c, rec := newManualEchoContext("/api/manual/index", "index")
	if err := handler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(body["error"], "make manual-stage") {
		t.Errorf("expected the error to name the fix, got %q", body["error"])
	}
}

// TestManualRoute_RegisteredAtTierReadAndMatchesNestedWildcard registers the
// real production route table (buildAPIRoutes/registerAPIRoutes, the same
// pattern static_routing_test.go's TestStaticHandler_APIRoutesStillWin
// uses) and drives a request through the live Echo router rather than
// calling the handler directly, so a change to the route's tier or its "*"
// wildcard shape fails here rather than only in the handler-level tests
// above.
func TestManualRoute_RegisteredAtTierReadAndMatchesNestedWildcard(t *testing.T) {
	sessions := newTestSessionStore(t)

	e := echo.New()
	registry := registerAPIRoutes(e, sessions, buildAPIRoutes(sessions, newWorldImageryHTTPClient()))

	tier, ok := registry[http.MethodGet+" /api/manual/*"]
	if !ok {
		t.Fatal("expected GET /api/manual/* to be registered")
	}
	if tier != tierRead {
		t.Fatalf("expected tierRead, got %v", tier)
	}

	// Default auth.mode ("none" - no settings override in this test) allows
	// the request through with no session cookie, so a non-404 response
	// here proves the wildcard matched a nested id at the router level, not
	// merely that the request was rejected before reaching the handler.
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/manual/features/dashboard", nil))
	if rec.Code == http.StatusNotFound && strings.Contains(rec.Body.String(), "Not Found") {
		t.Fatalf("expected the wildcard route to match a nested id, got echo's own 404: %s", rec.Body.String())
	}
}
