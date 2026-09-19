package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// newHelpEchoContext mirrors newAssistantEchoContext (assistant_handlers_test.go)
// but sets the "*" wildcard param getHelpPageHandler reads, since a help
// page id contains a slash ("features/dashboard") and cannot be a normal
// echo :param.
func newHelpEchoContext(path, wildcard string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("*")
	c.SetParamValues(wildcard)
	return c, rec
}

func TestGetHelpPageHandler_NestedIDReturns200(t *testing.T) {
	pages, err := loadHelp(testHelpFS(), "help")
	if err != nil {
		t.Fatalf("loadHelp: %v", err)
	}
	handler := getHelpPageHandler(func() []helpPage { return pages })

	c, rec := newHelpEchoContext("/api/help/features/forecast", "features/forecast")
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

func TestGetHelpPageHandler_UnknownIDReturns404(t *testing.T) {
	pages, err := loadHelp(testHelpFS(), "help")
	if err != nil {
		t.Fatalf("loadHelp: %v", err)
	}
	handler := getHelpPageHandler(func() []helpPage { return pages })

	c, rec := newHelpEchoContext("/api/help/nope", "nope")
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
	if want := `unknown help page "nope"`; body["error"] != want {
		t.Errorf("expected error %q, got %q", want, body["error"])
	}
}

func TestGetHelpPageHandler_EmptyIDReturns404(t *testing.T) {
	pages, err := loadHelp(testHelpFS(), "help")
	if err != nil {
		t.Fatalf("loadHelp: %v", err)
	}
	handler := getHelpPageHandler(func() []helpPage { return pages })

	c, rec := newHelpEchoContext("/api/help/", "")
	if err := handler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an empty page id, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestGetHelpPageHandler_PathTraversalIsJustAnUnknownID guards against
// treating ".." specially: help page ids are matched by exact string
// comparison over the loaded slice (getHelpPageHandler), so a
// "../etc/passwd"-shaped id can never resolve to anything outside the
// staged help tree - it simply isn't a known id.
func TestGetHelpPageHandler_PathTraversalIsJustAnUnknownID(t *testing.T) {
	pages, err := loadHelp(testHelpFS(), "help")
	if err != nil {
		t.Fatalf("loadHelp: %v", err)
	}
	handler := getHelpPageHandler(func() []helpPage { return pages })

	c, rec := newHelpEchoContext("/api/help/../etc/passwd", "../etc/passwd")
	if err := handler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestGetHelpPageHandler_EmptyHelpReturns503 covers a build that never
// ran `make help-stage`: the route exists, so this is 503 (a build
// problem the operator can fix), never 404 (an unknown page). Same sentence
// executeReadHelp uses (assistant_tools.go), so the operator sees one
// consistent message regardless of whether they hit this from Mate or the
// Help sheet.
func TestGetHelpPageHandler_EmptyHelpReturns503(t *testing.T) {
	handler := getHelpPageHandler(func() []helpPage { return nil })

	c, rec := newHelpEchoContext("/api/help/index", "index")
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
	if !strings.Contains(body["error"], "make help-stage") {
		t.Errorf("expected the error to name the fix, got %q", body["error"])
	}
}

// TestHelpRoute_RegisteredAtTierReadAndMatchesNestedWildcard registers the
// real production route table (buildAPIRoutes/registerAPIRoutes, the same
// pattern static_routing_test.go's TestStaticHandler_APIRoutesStillWin
// uses) and drives a request through the live Echo router rather than
// calling the handler directly, so a change to the route's tier or its "*"
// wildcard shape fails here rather than only in the handler-level tests
// above.
func TestHelpRoute_RegisteredAtTierReadAndMatchesNestedWildcard(t *testing.T) {
	sessions := newTestSessionStore(t)

	e := echo.New()
	registry := registerAPIRoutes(e, sessions, buildAPIRoutes(sessions, newWorldImageryHTTPClient()))

	tier, ok := registry[http.MethodGet+" /api/help/*"]
	if !ok {
		t.Fatal("expected GET /api/help/* to be registered")
	}
	if tier != tierRead {
		t.Fatalf("expected tierRead, got %v", tier)
	}

	// Default auth.mode ("none" - no settings override in this test) allows
	// the request through with no session cookie, so a non-404 response
	// here proves the wildcard matched a nested id at the router level, not
	// merely that the request was rejected before reaching the handler.
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/help/features/dashboard", nil))
	if rec.Code == http.StatusNotFound && strings.Contains(rec.Body.String(), "Not Found") {
		t.Fatalf("expected the wildcard route to match a nested id, got echo's own 404: %s", rec.Body.String())
	}
}
