package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

func setupDashboardPagesTest(t *testing.T) {
	t.Helper()
	t.Setenv("DASHBOARD_PAGES_FILE", filepath.Join(t.TempDir(), "dashboard-pages.json"))
	t.Setenv("DASHBOARD_LAYOUT_FILE", filepath.Join(t.TempDir(), "dashboard-layout.json"))
	loadDashboardPages()

	// loadDashboardPages now synthesizes a default "Anchored" page when
	// neither file exists (see TestDashboardPages_MigrationHandlesNeitherFile).
	// Tests using this helper exercise the handlers against a blank slate, so
	// clear that default out here rather than accounting for it in every
	// unrelated handler test.
	dashboardPagesMu.Lock()
	dashboardPagesState = make(map[string]*dashboardPageData)
	dashboardPagesMu.Unlock()

	// dashboardRibbonState and displaysState are package-level state guarded
	// by the same mutex (ADR 0082, ADR 0110), and a test that sets either
	// through the real handlers (or, for displaysState, by writing to the map
	// directly, since displays_test.go's helpers do that before the CRUD
	// handlers exist) has no per-test file to isolate it the way
	// DASHBOARD_PAGES_FILE isolates pages. Without this, state set by one
	// test leaks into whichever test runs next and does not call this
	// helper — in particular the gaugeBoundPaths tests, which manipulate
	// dashboardPagesState directly and would otherwise pick up a stray
	// ribbon's paths.
	t.Cleanup(func() {
		dashboardPagesMu.Lock()
		dashboardRibbonState = nil
		displaysState = nil
		dashboardPagesMu.Unlock()
	})
}

func newDashboardPagesRequest(t *testing.T, method, path string, body any) (echo.Context, *httptest.ResponseRecorder) {
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

func createTestDashboardPage(t *testing.T, name string, widgets []dashboardLayoutItem) dashboardPageData {
	t.Helper()
	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/dashboard-pages", map[string]any{
		"name":    name,
		"widgets": widgets,
	})
	if err := createDashboardPageHandler(c); err != nil {
		t.Fatalf("createDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}

	var page dashboardPageData
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("failed to parse created page: %v", err)
	}
	return page
}

func sampleDashboardWidgets() []dashboardLayoutItem {
	return []dashboardLayoutItem{
		{ID: "wind", X: 0, Y: 0, W: 4, H: 8},
		{ID: "tanks", X: 4, Y: 0, W: 4, H: 4},
	}
}

func TestListDashboardPagesHandler_EmptyInitially(t *testing.T) {
	setupDashboardPagesTest(t)

	c, rec := newDashboardPagesRequest(t, http.MethodGet, "/api/dashboard-pages", nil)
	if err := listDashboardPagesHandler(c); err != nil {
		t.Fatalf("listDashboardPagesHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var payload struct {
		Pages []dashboardPageData `json:"pages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse list response: %v", err)
	}
	if len(payload.Pages) != 0 {
		t.Fatalf("expected 0 pages, got %d", len(payload.Pages))
	}
}

func TestCreateDashboardPageHandler_ValidBody(t *testing.T) {
	setupDashboardPagesTest(t)

	page := createTestDashboardPage(t, "My Dashboard", sampleDashboardWidgets())

	if page.ID == "" {
		t.Fatal("expected page to have a generated ID")
	}
	if page.Name != "My Dashboard" {
		t.Fatalf("expected name to round-trip, got %q", page.Name)
	}
	if len(page.Widgets) != 2 {
		t.Fatalf("expected 2 widgets, got %d", len(page.Widgets))
	}
	if page.CreatedAt.IsZero() || page.UpdatedAt.IsZero() {
		t.Fatal("expected created_at/updated_at to be set")
	}
}

func TestCreateDashboardPageHandler_NilWidgetsBecomesEmptyArray(t *testing.T) {
	setupDashboardPagesTest(t)

	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/dashboard-pages", map[string]any{
		"name": "Empty Page",
	})
	if err := createDashboardPageHandler(c); err != nil {
		t.Fatalf("createDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, rec.Code)
	}

	var page dashboardPageData
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("failed to parse created page: %v", err)
	}
	if page.Widgets == nil {
		t.Fatal("expected widgets to be an empty array, not nil")
	}
	if len(page.Widgets) != 0 {
		t.Fatalf("expected 0 widgets, got %d", len(page.Widgets))
	}
}

func TestCreateDashboardPageHandler_RejectsEmptyName(t *testing.T) {
	setupDashboardPagesTest(t)

	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/dashboard-pages", map[string]any{
		"name":    "   ",
		"widgets": sampleDashboardWidgets(),
	})
	if err := createDashboardPageHandler(c); err != nil {
		t.Fatalf("createDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestCreateDashboardPageHandler_RejectsUnknownWidgetId(t *testing.T) {
	setupDashboardPagesTest(t)

	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/dashboard-pages", map[string]any{
		"name": "Bad Widget",
		"widgets": []map[string]any{
			{"id": "not-a-real-widget", "x": 0, "y": 0, "w": 4, "h": 4},
		},
	})
	if err := createDashboardPageHandler(c); err != nil {
		t.Fatalf("createDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestCreateDashboardPageHandler_RejectsDuplicateWidgetId(t *testing.T) {
	setupDashboardPagesTest(t)

	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/dashboard-pages", map[string]any{
		"name": "Duplicate Widgets",
		"widgets": []map[string]any{
			{"id": "wind", "x": 0, "y": 0, "w": 4, "h": 4},
			{"id": "wind", "x": 4, "y": 0, "w": 4, "h": 4},
		},
	})
	if err := createDashboardPageHandler(c); err != nil {
		t.Fatalf("createDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestGetDashboardPageHandler_FoundAndNotFound(t *testing.T) {
	setupDashboardPagesTest(t)
	page := createTestDashboardPage(t, "Test Page", sampleDashboardWidgets())

	c, rec := newDashboardPagesRequest(t, http.MethodGet, "/api/dashboard-pages/"+page.ID, nil)
	c.SetParamNames("id")
	c.SetParamValues(page.ID)
	if err := getDashboardPageHandler(c); err != nil {
		t.Fatalf("getDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	c2, rec2 := newDashboardPagesRequest(t, http.MethodGet, "/api/dashboard-pages/unknown-id", nil)
	c2.SetParamNames("id")
	c2.SetParamValues("unknown-id")
	if err := getDashboardPageHandler(c2); err != nil {
		t.Fatalf("getDashboardPageHandler returned error: %v", err)
	}
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, rec2.Code)
	}
}

func TestPatchDashboardPageHandler_UpdatesNameAndWidgets(t *testing.T) {
	setupDashboardPagesTest(t)
	page := createTestDashboardPage(t, "Original Name", sampleDashboardWidgets())

	newWidgets := []dashboardLayoutItem{{ID: "route", X: 0, Y: 0, W: 8, H: 8}}
	c, rec := newDashboardPagesRequest(t, http.MethodPatch, "/api/dashboard-pages/"+page.ID, map[string]any{
		"name":    "Renamed Page",
		"widgets": newWidgets,
	})
	c.SetParamNames("id")
	c.SetParamValues(page.ID)
	if err := patchDashboardPageHandler(c); err != nil {
		t.Fatalf("patchDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var updated dashboardPageData
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("failed to parse patch response: %v", err)
	}
	if updated.Name != "Renamed Page" {
		t.Fatalf("expected renamed page, got %q", updated.Name)
	}
	if len(updated.Widgets) != 1 {
		t.Fatalf("expected 1 widget after patch, got %d", len(updated.Widgets))
	}
	if !updated.CreatedAt.Equal(page.CreatedAt) {
		t.Fatal("expected created_at to remain unchanged on patch")
	}
	if !updated.UpdatedAt.After(page.UpdatedAt) && !updated.UpdatedAt.Equal(page.UpdatedAt) {
		t.Fatal("expected updated_at to be bumped on patch")
	}
}

func TestPatchDashboardPageHandler_RequiresAtLeastOneField(t *testing.T) {
	setupDashboardPagesTest(t)
	page := createTestDashboardPage(t, "Test Page", sampleDashboardWidgets())

	c, rec := newDashboardPagesRequest(t, http.MethodPatch, "/api/dashboard-pages/"+page.ID, map[string]any{})
	c.SetParamNames("id")
	c.SetParamValues(page.ID)
	if err := patchDashboardPageHandler(c); err != nil {
		t.Fatalf("patchDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestPatchDashboardPageHandler_RejectsEmptyStringName(t *testing.T) {
	setupDashboardPagesTest(t)
	page := createTestDashboardPage(t, "Test Page", sampleDashboardWidgets())

	c, rec := newDashboardPagesRequest(t, http.MethodPatch, "/api/dashboard-pages/"+page.ID, map[string]any{
		"name": "   ",
	})
	c.SetParamNames("id")
	c.SetParamValues(page.ID)
	if err := patchDashboardPageHandler(c); err != nil {
		t.Fatalf("patchDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestPatchDashboardPageHandler_NotFound(t *testing.T) {
	setupDashboardPagesTest(t)

	c, rec := newDashboardPagesRequest(t, http.MethodPatch, "/api/dashboard-pages/unknown-id", map[string]any{"name": "X"})
	c.SetParamNames("id")
	c.SetParamValues("unknown-id")
	if err := patchDashboardPageHandler(c); err != nil {
		t.Fatalf("patchDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}

// ── per-page skin ─────────────────────────────────────────────────────────
//
// The skin used to live on each cluster widget's config; it now lives on the
// page itself and applies to the whole grid.

func TestCreateDashboardPageHandler_RejectsUnknownSkin(t *testing.T) {
	setupDashboardPagesTest(t)

	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/dashboard-pages", map[string]any{
		"name":    "Bad Skin",
		"widgets": sampleDashboardWidgets(),
		"skin":    "neon",
	})
	if err := createDashboardPageHandler(c); err != nil {
		t.Fatalf("createDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestDashboardPages_SkinRoundTripsThroughPostAndGet(t *testing.T) {
	setupDashboardPagesTest(t)

	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/dashboard-pages", map[string]any{
		"name":    "Round Trip",
		"widgets": sampleDashboardWidgets(),
		"skin":    "instrument",
	})
	if err := createDashboardPageHandler(c); err != nil {
		t.Fatalf("createDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}
	var page dashboardPageData
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("failed to parse created page: %v", err)
	}
	if page.Skin != "instrument" {
		t.Fatalf("expected created page to have skin %q, got %q", "instrument", page.Skin)
	}

	c2, rec2 := newDashboardPagesRequest(t, http.MethodGet, "/api/dashboard-pages/"+page.ID, nil)
	c2.SetParamNames("id")
	c2.SetParamValues(page.ID)
	if err := getDashboardPageHandler(c2); err != nil {
		t.Fatalf("getDashboardPageHandler returned error: %v", err)
	}
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec2.Code)
	}

	var fetched dashboardPageData
	if err := json.Unmarshal(rec2.Body.Bytes(), &fetched); err != nil {
		t.Fatalf("failed to parse get response: %v", err)
	}
	if fetched.Skin != "instrument" {
		t.Fatalf("expected skin to round-trip through GET, got %q", fetched.Skin)
	}
}

func TestPatchDashboardPageHandler_SkinOnlyPatchSucceeds(t *testing.T) {
	setupDashboardPagesTest(t)
	page := createTestDashboardPage(t, "Test Page", sampleDashboardWidgets())

	// A skin-only body has neither name nor widgets; it must not trip the
	// "no patch fields provided" guard.
	c, rec := newDashboardPagesRequest(t, http.MethodPatch, "/api/dashboard-pages/"+page.ID, map[string]any{
		"skin": "default",
	})
	c.SetParamNames("id")
	c.SetParamValues(page.ID)
	if err := patchDashboardPageHandler(c); err != nil {
		t.Fatalf("patchDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var updated dashboardPageData
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("failed to parse patch response: %v", err)
	}
	if updated.Skin != "default" {
		t.Fatalf("expected skin to be updated, got %q", updated.Skin)
	}
}

func TestPatchDashboardPageHandler_RejectsUnknownSkin(t *testing.T) {
	setupDashboardPagesTest(t)
	page := createTestDashboardPage(t, "Test Page", sampleDashboardWidgets())

	c, rec := newDashboardPagesRequest(t, http.MethodPatch, "/api/dashboard-pages/"+page.ID, map[string]any{
		"skin": "neon",
	})
	c.SetParamNames("id")
	c.SetParamValues(page.ID)
	if err := patchDashboardPageHandler(c); err != nil {
		t.Fatalf("patchDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

// TestPatchDashboardPageHandler_PreservesSkinOnWidgetsOnlyPatch guards the
// handler's field-by-field rebuild of the updated page. A widgets-only PATCH
// is what every layout drag sends; if the rebuild forgets to carry Skin
// forward from the current page, the page's skin is silently wiped on the
// very next drag.
func TestPatchDashboardPageHandler_PreservesSkinOnWidgetsOnlyPatch(t *testing.T) {
	setupDashboardPagesTest(t)

	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/dashboard-pages", map[string]any{
		"name":    "Skinned Page",
		"widgets": sampleDashboardWidgets(),
		"skin":    "instrument",
	})
	if err := createDashboardPageHandler(c); err != nil {
		t.Fatalf("createDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}
	var page dashboardPageData
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("failed to parse created page: %v", err)
	}
	if page.Skin != "instrument" {
		t.Fatalf("expected created page to have skin %q, got %q", "instrument", page.Skin)
	}

	newWidgets := []dashboardLayoutItem{{ID: "route", X: 0, Y: 0, W: 8, H: 8}}
	c2, rec2 := newDashboardPagesRequest(t, http.MethodPatch, "/api/dashboard-pages/"+page.ID, map[string]any{
		"widgets": newWidgets,
	})
	c2.SetParamNames("id")
	c2.SetParamValues(page.ID)
	if err := patchDashboardPageHandler(c2); err != nil {
		t.Fatalf("patchDashboardPageHandler returned error: %v", err)
	}
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec2.Code, rec2.Body.String())
	}

	var updated dashboardPageData
	if err := json.Unmarshal(rec2.Body.Bytes(), &updated); err != nil {
		t.Fatalf("failed to parse patch response: %v", err)
	}
	if updated.Skin != "instrument" {
		t.Fatalf("expected skin to survive a widgets-only patch, got %q", updated.Skin)
	}

	dashboardPagesMu.RLock()
	stored := dashboardPagesState[page.ID]
	dashboardPagesMu.RUnlock()
	if stored.Skin != "instrument" {
		t.Fatalf("expected stored page to retain skin after widgets-only patch, got %q", stored.Skin)
	}
}

// ── per-page hero ────────────────────────────────────────────────────────
//
// The hero names the one widget on a page that gets the enlarged, full-width
// treatment (design critique batch, ADR 0072). It follows the same
// validate-fail-closed pattern ADR 0060 established for skin, with one added
// rule: a hero must actually exist among the page's own widgets.

func TestCreateDashboardPageHandler_RejectsHeroNamingAWidgetNotOnThePage(t *testing.T) {
	setupDashboardPagesTest(t)

	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/dashboard-pages", map[string]any{
		"name":    "Bad Hero",
		"widgets": sampleDashboardWidgets(),
		"hero":    "battery-power",
	})
	if err := createDashboardPageHandler(c); err != nil {
		t.Fatalf("createDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestDashboardPages_HeroRoundTripsThroughPostAndGet(t *testing.T) {
	setupDashboardPagesTest(t)

	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/dashboard-pages", map[string]any{
		"name":    "Round Trip",
		"widgets": sampleDashboardWidgets(),
		"hero":    "wind",
	})
	if err := createDashboardPageHandler(c); err != nil {
		t.Fatalf("createDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}
	var page dashboardPageData
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("failed to parse created page: %v", err)
	}
	if page.Hero != "wind" {
		t.Fatalf("expected created page to have hero %q, got %q", "wind", page.Hero)
	}

	c2, rec2 := newDashboardPagesRequest(t, http.MethodGet, "/api/dashboard-pages/"+page.ID, nil)
	c2.SetParamNames("id")
	c2.SetParamValues(page.ID)
	if err := getDashboardPageHandler(c2); err != nil {
		t.Fatalf("getDashboardPageHandler returned error: %v", err)
	}
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec2.Code)
	}

	var fetched dashboardPageData
	if err := json.Unmarshal(rec2.Body.Bytes(), &fetched); err != nil {
		t.Fatalf("failed to parse get response: %v", err)
	}
	if fetched.Hero != "wind" {
		t.Fatalf("expected hero to round-trip through GET, got %q", fetched.Hero)
	}
}

func TestPatchDashboardPageHandler_HeroOnlyPatchSucceeds(t *testing.T) {
	setupDashboardPagesTest(t)
	page := createTestDashboardPage(t, "Test Page", sampleDashboardWidgets())

	// A hero-only body has neither name, skin nor widgets; it must not trip the
	// "no patch fields provided" guard.
	c, rec := newDashboardPagesRequest(t, http.MethodPatch, "/api/dashboard-pages/"+page.ID, map[string]any{
		"hero": "tanks",
	})
	c.SetParamNames("id")
	c.SetParamValues(page.ID)
	if err := patchDashboardPageHandler(c); err != nil {
		t.Fatalf("patchDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var updated dashboardPageData
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("failed to parse patch response: %v", err)
	}
	if updated.Hero != "tanks" {
		t.Fatalf("expected hero to be updated, got %q", updated.Hero)
	}
}

func TestPatchDashboardPageHandler_RejectsHeroNamingAWidgetNotOnThePage(t *testing.T) {
	setupDashboardPagesTest(t)
	page := createTestDashboardPage(t, "Test Page", sampleDashboardWidgets())

	c, rec := newDashboardPagesRequest(t, http.MethodPatch, "/api/dashboard-pages/"+page.ID, map[string]any{
		"hero": "battery-power",
	})
	c.SetParamNames("id")
	c.SetParamValues(page.ID)
	if err := patchDashboardPageHandler(c); err != nil {
		t.Fatalf("patchDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

// TestPatchDashboardPageHandler_PreservesHeroOnWidgetsOnlyPatchThatKeepsIt
// guards the handler's field-by-field rebuild exactly as ADR 0060 §7 guards
// Skin: a widgets-only PATCH (what every layout drag sends) must not wipe an
// unrelated page-level field, as long as the hero widget is still present in
// the new widget list.
func TestPatchDashboardPageHandler_PreservesHeroOnWidgetsOnlyPatchThatKeepsIt(t *testing.T) {
	setupDashboardPagesTest(t)

	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/dashboard-pages", map[string]any{
		"name":    "Heroed Page",
		"widgets": sampleDashboardWidgets(),
		"hero":    "wind",
	})
	if err := createDashboardPageHandler(c); err != nil {
		t.Fatalf("createDashboardPageHandler returned error: %v", err)
	}
	var page dashboardPageData
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("failed to parse created page: %v", err)
	}

	// Reposition "wind" rather than dropping it - the hero widget survives.
	newWidgets := []dashboardLayoutItem{
		{ID: "wind", X: 4, Y: 0, W: 4, H: 8},
		{ID: "tanks", X: 0, Y: 0, W: 4, H: 4},
	}
	c2, rec2 := newDashboardPagesRequest(t, http.MethodPatch, "/api/dashboard-pages/"+page.ID, map[string]any{
		"widgets": newWidgets,
	})
	c2.SetParamNames("id")
	c2.SetParamValues(page.ID)
	if err := patchDashboardPageHandler(c2); err != nil {
		t.Fatalf("patchDashboardPageHandler returned error: %v", err)
	}
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec2.Code, rec2.Body.String())
	}

	var updated dashboardPageData
	if err := json.Unmarshal(rec2.Body.Bytes(), &updated); err != nil {
		t.Fatalf("failed to parse patch response: %v", err)
	}
	if updated.Hero != "wind" {
		t.Fatalf("expected hero to survive a widgets-only patch that keeps it, got %q", updated.Hero)
	}
}

// TestPatchDashboardPageHandler_ClearsHeroWhenWidgetsPatchRemovesTheHeroWidget
// decides the dangling-hero question: removing the widget that is the hero
// must not leave the page pointing at nothing. This is exactly the patch the
// bento grid's "X" remove-widget button sends (widgets only, no hero field),
// so rejecting it would break ordinary widget removal for the unrelated
// reason that it happened to be the hero. The hero is repaired instead, the
// same stance stripRetiredWidgets takes on load: a dangling reference is
// corrected on write, not treated as the caller's error.
func TestPatchDashboardPageHandler_ClearsHeroWhenWidgetsPatchRemovesTheHeroWidget(t *testing.T) {
	setupDashboardPagesTest(t)

	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/dashboard-pages", map[string]any{
		"name":    "Heroed Page",
		"widgets": sampleDashboardWidgets(),
		"hero":    "wind",
	})
	if err := createDashboardPageHandler(c); err != nil {
		t.Fatalf("createDashboardPageHandler returned error: %v", err)
	}
	var page dashboardPageData
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("failed to parse created page: %v", err)
	}

	// Drop "wind" - the hero widget - without mentioning hero at all.
	newWidgets := []dashboardLayoutItem{{ID: "tanks", X: 0, Y: 0, W: 4, H: 4}}
	c2, rec2 := newDashboardPagesRequest(t, http.MethodPatch, "/api/dashboard-pages/"+page.ID, map[string]any{
		"widgets": newWidgets,
	})
	c2.SetParamNames("id")
	c2.SetParamValues(page.ID)
	if err := patchDashboardPageHandler(c2); err != nil {
		t.Fatalf("patchDashboardPageHandler returned error: %v", err)
	}
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec2.Code, rec2.Body.String())
	}

	var updated dashboardPageData
	if err := json.Unmarshal(rec2.Body.Bytes(), &updated); err != nil {
		t.Fatalf("failed to parse patch response: %v", err)
	}
	if updated.Hero != "" {
		t.Fatalf("expected hero to be cleared once its widget is removed, got %q", updated.Hero)
	}

	dashboardPagesMu.RLock()
	stored := dashboardPagesState[page.ID]
	dashboardPagesMu.RUnlock()
	if stored.Hero != "" {
		t.Fatalf("expected stored page to have cleared hero after widget removal, got %q", stored.Hero)
	}
}

// TestPatchDashboardPageHandler_RejectsExplicitHeroPatchThatWidgetsPatchInvalidates
// covers the other order: when hero is set explicitly in the very same
// request that removes its target widget, that is an assertion the caller
// got wrong, not a stale reference to repair - fail closed exactly like an
// unknown skin.
func TestPatchDashboardPageHandler_RejectsExplicitHeroPatchThatWidgetsPatchInvalidates(t *testing.T) {
	setupDashboardPagesTest(t)
	page := createTestDashboardPage(t, "Test Page", sampleDashboardWidgets())

	newWidgets := []dashboardLayoutItem{{ID: "tanks", X: 0, Y: 0, W: 4, H: 4}}
	c, rec := newDashboardPagesRequest(t, http.MethodPatch, "/api/dashboard-pages/"+page.ID, map[string]any{
		"widgets": newWidgets,
		"hero":    "wind",
	})
	c.SetParamNames("id")
	c.SetParamValues(page.ID)
	if err := patchDashboardPageHandler(c); err != nil {
		t.Fatalf("patchDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestDeleteDashboardPageHandler_DeletesAndReports404Afterward(t *testing.T) {
	setupDashboardPagesTest(t)
	page := createTestDashboardPage(t, "Test Page", sampleDashboardWidgets())

	// Create a second page so we don't trigger the "last page" constraint
	createTestDashboardPage(t, "Second Page", sampleDashboardWidgets())

	c, rec := newDashboardPagesRequest(t, http.MethodDelete, "/api/dashboard-pages/"+page.ID, nil)
	c.SetParamNames("id")
	c.SetParamValues(page.ID)
	if err := deleteDashboardPageHandler(c); err != nil {
		t.Fatalf("deleteDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	c2, rec2 := newDashboardPagesRequest(t, http.MethodGet, "/api/dashboard-pages/"+page.ID, nil)
	c2.SetParamNames("id")
	c2.SetParamValues(page.ID)
	if err := getDashboardPageHandler(c2); err != nil {
		t.Fatalf("getDashboardPageHandler returned error: %v", err)
	}
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("expected status %d after delete, got %d", http.StatusNotFound, rec2.Code)
	}
}

func TestDeleteDashboardPageHandler_NotFound(t *testing.T) {
	setupDashboardPagesTest(t)

	c, rec := newDashboardPagesRequest(t, http.MethodDelete, "/api/dashboard-pages/unknown-id", nil)
	c.SetParamNames("id")
	c.SetParamValues("unknown-id")
	if err := deleteDashboardPageHandler(c); err != nil {
		t.Fatalf("deleteDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}

func TestDeleteDashboardPageHandler_RejectsWhenOnlyPageRemaining(t *testing.T) {
	setupDashboardPagesTest(t)
	page := createTestDashboardPage(t, "Only Page", sampleDashboardWidgets())

	c, rec := newDashboardPagesRequest(t, http.MethodDelete, "/api/dashboard-pages/"+page.ID, nil)
	c.SetParamNames("id")
	c.SetParamValues(page.ID)
	if err := deleteDashboardPageHandler(c); err != nil {
		t.Fatalf("deleteDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	// Verify the page still exists
	c2, rec2 := newDashboardPagesRequest(t, http.MethodGet, "/api/dashboard-pages/"+page.ID, nil)
	c2.SetParamNames("id")
	c2.SetParamValues(page.ID)
	if err := getDashboardPageHandler(c2); err != nil {
		t.Fatalf("getDashboardPageHandler returned error: %v", err)
	}
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected page to still exist after rejected delete, got status %d", rec2.Code)
	}
}

func TestDashboardPages_AtomicWriteReloadRoundTrip(t *testing.T) {
	setupDashboardPagesTest(t)

	first := createTestDashboardPage(t, "Page One", sampleDashboardWidgets())
	second := createTestDashboardPage(t, "Page Two", sampleDashboardWidgets())

	// Simulate a process restart: reload state from disk.
	loadDashboardPages()

	dashboardPagesMu.RLock()
	defer dashboardPagesMu.RUnlock()
	if len(dashboardPagesState) != 2 {
		t.Fatalf("expected 2 pages after reload, got %d", len(dashboardPagesState))
	}
	if _, ok := dashboardPagesState[first.ID]; !ok {
		t.Fatal("expected first page to survive reload")
	}
	if _, ok := dashboardPagesState[second.ID]; !ok {
		t.Fatal("expected second page to survive reload")
	}
}

func TestListDashboardPagesHandler_OrdersAscendingByCreatedAt(t *testing.T) {
	setupDashboardPagesTest(t)

	// Create 3 pages
	first := createTestDashboardPage(t, "First", sampleDashboardWidgets())
	second := createTestDashboardPage(t, "Second", sampleDashboardWidgets())
	third := createTestDashboardPage(t, "Third", sampleDashboardWidgets())

	// Directly set their CreatedAt times to ensure deterministic ordering
	// (older to newer: First, Second, Third)
	dashboardPagesMu.Lock()
	dashboardPagesState[first.ID].CreatedAt = time.Now().Add(-2 * time.Hour)
	dashboardPagesState[second.ID].CreatedAt = time.Now().Add(-1 * time.Hour)
	dashboardPagesState[third.ID].CreatedAt = time.Now()
	dashboardPagesMu.Unlock()

	// Call listDashboardPagesHandler and verify ascending ordering
	c, rec := newDashboardPagesRequest(t, http.MethodGet, "/api/dashboard-pages", nil)
	if err := listDashboardPagesHandler(c); err != nil {
		t.Fatalf("listDashboardPagesHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var payload struct {
		Pages []dashboardPageData `json:"pages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse list response: %v", err)
	}

	if len(payload.Pages) != 3 {
		t.Fatalf("expected 3 pages, got %d", len(payload.Pages))
	}

	// Verify order is ascending: First, Second, Third
	if payload.Pages[0].Name != "First" {
		t.Fatalf("expected first page to be 'First' (oldest), got %q", payload.Pages[0].Name)
	}
	if payload.Pages[1].Name != "Second" {
		t.Fatalf("expected second page to be 'Second', got %q", payload.Pages[1].Name)
	}
	if payload.Pages[2].Name != "Third" {
		t.Fatalf("expected third page to be 'Third' (newest), got %q", payload.Pages[2].Name)
	}
}

func TestDashboardPages_MigrationFromLegacyFile(t *testing.T) {
	// Setup: create a temp dir with just the legacy file, pages file path nonexistent
	tempDir := t.TempDir()
	pagesPath := filepath.Join(tempDir, "dashboard-pages.json")
	layoutPath := filepath.Join(tempDir, "dashboard-layout.json")

	// Write legacy file with some widgets
	legacyData := map[string]any{
		"widgets": []map[string]any{
			{"id": "wind", "x": 0, "y": 0, "w": 4, "h": 8},
			{"id": "depth-tide", "x": 4, "y": 0, "w": 4, "h": 4},
		},
		"updated_at": time.Now().UTC().Format(time.RFC3339),
	}
	legacyBytes, err := json.Marshal(legacyData)
	if err != nil {
		t.Fatalf("failed to marshal legacy data: %v", err)
	}
	if err := os.WriteFile(layoutPath, legacyBytes, 0o644); err != nil {
		t.Fatalf("failed to write legacy file: %v", err)
	}

	// Set env vars and load
	t.Setenv("DASHBOARD_PAGES_FILE", pagesPath)
	t.Setenv("DASHBOARD_LAYOUT_FILE", layoutPath)
	loadDashboardPages()

	// Verify exactly one page exists named "Anchored" with the same widgets
	dashboardPagesMu.RLock()
	if len(dashboardPagesState) != 1 {
		t.Fatalf("expected 1 page after migration, got %d", len(dashboardPagesState))
	}
	var page *dashboardPageData
	for _, p := range dashboardPagesState {
		page = p
		break
	}
	dashboardPagesMu.RUnlock()

	if page.Name != "Anchored" {
		t.Fatalf("expected page name 'Anchored', got %q", page.Name)
	}
	if len(page.Widgets) != 2 {
		t.Fatalf("expected 2 widgets, got %d", len(page.Widgets))
	}
	if page.Widgets[0].ID != "wind" || page.Widgets[1].ID != "depth-tide" {
		t.Fatal("expected widgets to match legacy data")
	}

	// Verify the new pages file now exists
	if _, err := os.Stat(pagesPath); os.IsNotExist(err) {
		t.Fatal("expected pages file to be created during migration")
	}

	// Verify the pages file contains the migrated page
	data, err := os.ReadFile(pagesPath)
	if err != nil {
		t.Fatalf("failed to read pages file: %v", err)
	}
	var stored dashboardPagesFile
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("failed to parse pages file: %v", err)
	}
	if len(stored.Pages) != 1 {
		t.Fatalf("expected 1 page in stored file, got %d", len(stored.Pages))
	}
	if stored.Pages[0].Name != "Anchored" {
		t.Fatalf("expected stored page name 'Anchored', got %q", stored.Pages[0].Name)
	}
}

func TestDashboardPages_MigrationIgnoresLegacyIfNewFileExists(t *testing.T) {
	// Setup: create both legacy and new file
	tempDir := t.TempDir()
	pagesPath := filepath.Join(tempDir, "dashboard-pages.json")
	layoutPath := filepath.Join(tempDir, "dashboard-layout.json")

	// Write legacy file with one widget set
	legacyData := map[string]any{
		"widgets": []map[string]any{
			{"id": "wind", "x": 0, "y": 0, "w": 4, "h": 8},
		},
		"updated_at": time.Now().UTC().Format(time.RFC3339),
	}
	legacyBytes, err := json.Marshal(legacyData)
	if err != nil {
		t.Fatalf("failed to marshal legacy data: %v", err)
	}
	if err := os.WriteFile(layoutPath, legacyBytes, 0o644); err != nil {
		t.Fatalf("failed to write legacy file: %v", err)
	}

	// Write new pages file with different content
	pageID := "test-page-id"
	now := time.Now().UTC()
	newData := dashboardPagesFile{
		Pages: []*dashboardPageData{
			{
				ID:   pageID,
				Name: "Existing Page",
				Widgets: []dashboardLayoutItem{
					{ID: "tanks", X: 0, Y: 0, W: 4, H: 4},
				},
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
	}
	newBytes, err := json.Marshal(newData)
	if err != nil {
		t.Fatalf("failed to marshal new data: %v", err)
	}
	if err := os.WriteFile(pagesPath, newBytes, 0o644); err != nil {
		t.Fatalf("failed to write pages file: %v", err)
	}

	// Set env vars and load
	t.Setenv("DASHBOARD_PAGES_FILE", pagesPath)
	t.Setenv("DASHBOARD_LAYOUT_FILE", layoutPath)
	loadDashboardPages()

	// Verify exactly one page exists with the new file's content (not "Anchored")
	dashboardPagesMu.RLock()
	defer dashboardPagesMu.RUnlock()
	if len(dashboardPagesState) != 1 {
		t.Fatalf("expected 1 page after loading, got %d", len(dashboardPagesState))
	}
	page, ok := dashboardPagesState[pageID]
	if !ok {
		t.Fatal("expected page with ID from new file to exist")
	}
	if page.Name != "Existing Page" {
		t.Fatalf("expected page name 'Existing Page' (from new file), got %q", page.Name)
	}
	if len(page.Widgets) != 1 || page.Widgets[0].ID != "tanks" {
		t.Fatal("expected widgets from new file, not legacy")
	}
}

func TestDashboardPages_MigrationHandlesNeitherFile(t *testing.T) {
	// Setup: point at nonexistent files
	tempDir := t.TempDir()
	pagesPath := filepath.Join(tempDir, "dashboard-pages.json")
	layoutPath := filepath.Join(tempDir, "dashboard-layout.json")

	// Set env vars pointing at nonexistent paths
	t.Setenv("DASHBOARD_PAGES_FILE", pagesPath)
	t.Setenv("DASHBOARD_LAYOUT_FILE", layoutPath)
	loadDashboardPages()

	// Verify a single default "Anchored" page was synthesized from
	// defaultDashboardLayout (no panic, no error) — a genuinely fresh
	// install now gets its first page from the backend directly.
	dashboardPagesMu.RLock()
	defer dashboardPagesMu.RUnlock()
	if len(dashboardPagesState) != 1 {
		t.Fatalf("expected 1 default page when no files exist, got %d", len(dashboardPagesState))
	}
	var page *dashboardPageData
	for _, p := range dashboardPagesState {
		page = p
		break
	}
	if page.Name != "Anchored" {
		t.Fatalf("expected default page name 'Anchored', got %q", page.Name)
	}
	if len(page.Widgets) != len(defaultDashboardLayout) {
		t.Fatalf("expected %d widgets in default page, got %d", len(defaultDashboardLayout), len(page.Widgets))
	}
	for i, w := range defaultDashboardLayout {
		if page.Widgets[i] != w {
			t.Fatalf("expected widget %d to be %+v, got %+v", i, w, page.Widgets[i])
		}
	}
	if page.Widgets[0].ID != "vessel" {
		t.Fatalf("expected first widget to be 'vessel', got %q", page.Widgets[0].ID)
	}
	if page.CreatedAt.IsZero() || page.UpdatedAt.IsZero() {
		t.Fatal("expected created_at/updated_at to be set on default page")
	}

	// Verify the pages file now exists, persisting the default page.
	if _, err := os.Stat(pagesPath); os.IsNotExist(err) {
		t.Fatal("expected pages file to be created for the default page")
	}
}

// --- Retired widget ids (ADR 0047) --------------------------------------------

// A saved page from a previous release may still carry "rode-scope" — the
// retired Rode & Scope tile's widget id. validateDashboardWidgets rejects any
// unknown id and would 400 the *entire* PATCH, so an install with a stale id
// in its saved layout would be unable to save any layout change at all.
// loadDashboardPages must strip retired ids on load and rewrite the file, so
// the id can never be seen by validateDashboardWidgets again.
func TestDashboardPages_RetiredWidgetIDStrippedOnLoad(t *testing.T) {
	tempDir := t.TempDir()
	pagesPath := filepath.Join(tempDir, "dashboard-pages.json")
	t.Setenv("DASHBOARD_PAGES_FILE", pagesPath)

	pageID := uuid.NewString()
	now := time.Now().UTC()
	saved := dashboardPagesFile{
		Pages: []*dashboardPageData{
			{
				ID:   pageID,
				Name: "Anchored",
				Widgets: []dashboardLayoutItem{
					{ID: "anchor-watch", X: 4, Y: 3, W: 4, H: 8},
					{ID: "rode-scope", X: 4, Y: 11, W: 4, H: 6},
					{ID: "tanks", X: 4, Y: 17, W: 4, H: 4},
				},
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
	}
	savedBytes, err := json.Marshal(saved)
	if err != nil {
		t.Fatalf("failed to marshal saved pages: %v", err)
	}
	if err := os.WriteFile(pagesPath, savedBytes, 0o644); err != nil {
		t.Fatalf("failed to write pages file: %v", err)
	}

	loadDashboardPages()

	dashboardPagesMu.RLock()
	page, ok := dashboardPagesState[pageID]
	dashboardPagesMu.RUnlock()
	if !ok {
		t.Fatal("expected page to load")
	}
	if len(page.Widgets) != 2 {
		t.Fatalf("expected rode-scope stripped leaving 2 widgets, got %d: %+v", len(page.Widgets), page.Widgets)
	}
	for _, w := range page.Widgets {
		if w.ID == "rode-scope" {
			t.Fatal("expected rode-scope to be stripped from loaded widgets")
		}
	}

	// The stripped state must be persisted back to disk, not just held in memory,
	// so a restart doesn't resurrect the retired id.
	onDiskBytes, err := os.ReadFile(pagesPath)
	if err != nil {
		t.Fatalf("failed to read pages file after load: %v", err)
	}
	var onDisk dashboardPagesFile
	if err := json.Unmarshal(onDiskBytes, &onDisk); err != nil {
		t.Fatalf("failed to parse pages file after load: %v", err)
	}
	if len(onDisk.Pages) != 1 || len(onDisk.Pages[0].Widgets) != 2 {
		t.Fatalf("expected persisted file to have rode-scope stripped, got %+v", onDisk.Pages)
	}

	// A subsequent PATCH must now succeed — this is the actual bug being fixed:
	// validateDashboardWidgets previously rejected the whole PATCH because the
	// saved page still contained the unknown "rode-scope" id.
	e := echo.New()
	newWidgets := []dashboardLayoutItem{
		{ID: "anchor-watch", X: 0, Y: 0, W: 4, H: 8},
		{ID: "tanks", X: 4, Y: 0, W: 4, H: 4},
	}
	body, err := json.Marshal(map[string]any{"widgets": newWidgets})
	if err != nil {
		t.Fatalf("failed to marshal patch body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/dashboard-pages/"+pageID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(pageID)

	if err := patchDashboardPageHandler(c); err != nil {
		t.Fatalf("patch handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected PATCH to succeed with status 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ── the kiosk -> display load-time conversion (multiple-wall-displays plan §1) ─
//
// A v1 file's "kiosk"/"kiosk_seconds"/"kiosk_when" keys are absorbed into
// the display model exactly once, on the first load that finds them and no
// existing displays. dashboardPageData no longer has fields for them, so the
// fixtures below are written as raw JSON (mirroring
// TestDashboardPages_RetiredWidgetIDStrippedOnLoad's own pattern of writing
// the file directly rather than through the current struct shape).

func TestLoadDashboardPages_ConvertsKioskFlaggedPagesToFlybridgeDisplay(t *testing.T) {
	tempDir := t.TempDir()
	pagesPath := filepath.Join(tempDir, "dashboard-pages.json")
	t.Setenv("DASHBOARD_PAGES_FILE", pagesPath)

	fixture := map[string]any{
		"pages": []map[string]any{
			{
				"id": "p1", "name": "Wall: Engines",
				"kiosk": true, "kiosk_seconds": 30,
				"widgets":    []any{},
				"created_at": "2024-01-01T00:00:00Z", "updated_at": "2024-01-01T00:00:00Z",
			},
			{
				"id": "p2", "name": "Wall: Anchor",
				"kiosk": true, "kiosk_seconds": 45, "kiosk_when": "anchored", "hero": "wind",
				"widgets":    []map[string]any{{"id": "wind", "x": 0, "y": 0, "w": 4, "h": 8}},
				"created_at": "2024-01-01T00:00:01Z", "updated_at": "2024-01-01T00:00:01Z",
			},
			{
				"id": "p3", "name": "Ordinary",
				"kiosk_seconds": 20,
				"widgets":       []any{},
				"created_at":    "2024-01-01T00:00:02Z", "updated_at": "2024-01-01T00:00:02Z",
			},
		},
		"ribbon": map[string]any{
			"title": "Status",
			"lamps": []map[string]any{{"path": "electrical.generator.state", "label": "GEN"}},
		},
	}
	fixtureBytes, err := json.Marshal(fixture)
	if err != nil {
		t.Fatalf("failed to marshal fixture: %v", err)
	}
	if err := os.WriteFile(pagesPath, fixtureBytes, 0o644); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	loadDashboardPages()

	dashboardPagesMu.RLock()
	displays := orderedDisplaysLocked()
	p1 := dashboardPagesState["p1"]
	p2 := dashboardPagesState["p2"]
	p3 := dashboardPagesState["p3"]
	ribbon := dashboardRibbonState
	dashboardPagesMu.RUnlock()

	if len(displays) != 1 {
		t.Fatalf("expected exactly one synthesized display, got %d: %+v", len(displays), displays)
	}
	display := displays[0]
	if display.Name != "Flybridge" || display.Slug != "flybridge" {
		t.Fatalf("expected the synthesized display to be named/slugged Flybridge, got %+v", display)
	}
	if display.Width != 1920 || display.Height != 360 || display.Rotate != 180 {
		t.Fatalf("expected the synthesized display's geometry to match the ODROID strip, got %+v", display)
	}

	if p1 == nil || p1.DisplayID != display.ID || p1.DwellSeconds != 30 || p1.ShowWhen != "" {
		t.Fatalf("expected p1 to carry the display id and duration, got %+v", p1)
	}
	if p2 == nil || p2.DisplayID != display.ID || p2.DwellSeconds != 45 || p2.ShowWhen != "anchored" {
		t.Fatalf("expected p2 to carry the display id, duration and condition, got %+v", p2)
	}
	if p2.Hero != "" {
		t.Fatalf("expected p2's hero to be cleared on assignment to a display, got %q", p2.Hero)
	}
	if p3 == nil || p3.DisplayID != "" {
		t.Fatalf("expected p3 (never flagged kiosk) to stay off the display, got %+v", p3)
	}
	if p3.DwellSeconds != 20 {
		t.Fatalf("expected p3's stale kiosk_seconds to still be copied to dwell_seconds even though it was never flagged, got %d", p3.DwellSeconds)
	}
	if ribbon == nil || ribbon.Title != "Status" || len(ribbon.Lamps) != 1 {
		t.Fatalf("expected the ribbon to survive the conversion untouched, got %+v", ribbon)
	}

	onDisk, err := os.ReadFile(pagesPath)
	if err != nil {
		t.Fatalf("failed to read rewritten file: %v", err)
	}
	if strings.Contains(string(onDisk), "kiosk") {
		t.Fatalf("expected no kiosk* key in the rewritten file, got %s", onDisk)
	}
}

func TestLoadDashboardPages_DoesNotReconvertWhenDisplaysExist(t *testing.T) {
	tempDir := t.TempDir()
	pagesPath := filepath.Join(tempDir, "dashboard-pages.json")
	t.Setenv("DASHBOARD_PAGES_FILE", pagesPath)

	fixture := map[string]any{
		"pages": []map[string]any{
			{
				"id": "p1", "name": "Wall: Engines", "kiosk": true, "kiosk_seconds": 30,
				"widgets": []any{}, "created_at": "2024-01-01T00:00:00Z", "updated_at": "2024-01-01T00:00:00Z",
			},
		},
	}
	fixtureBytes, err := json.Marshal(fixture)
	if err != nil {
		t.Fatalf("failed to marshal fixture: %v", err)
	}
	if err := os.WriteFile(pagesPath, fixtureBytes, 0o644); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	loadDashboardPages()
	dashboardPagesMu.RLock()
	firstDisplays := orderedDisplaysLocked()
	dashboardPagesMu.RUnlock()
	if len(firstDisplays) != 1 {
		t.Fatalf("expected the first load to synthesize one display, got %d", len(firstDisplays))
	}
	firstID := firstDisplays[0].ID

	// A second load reads back the already-converted file - no "kiosk" key
	// survives the rewrite, so a second synthesis has nothing to trigger on
	// even before checking loaded.Displays.
	loadDashboardPages()
	dashboardPagesMu.RLock()
	secondDisplays := orderedDisplaysLocked()
	p1 := dashboardPagesState["p1"]
	dashboardPagesMu.RUnlock()
	if len(secondDisplays) != 1 || secondDisplays[0].ID != firstID {
		t.Fatalf("expected the second load to be idempotent, got %+v", secondDisplays)
	}
	if p1.DisplayID != firstID || p1.DwellSeconds != 30 {
		t.Fatalf("expected the page's assignment to survive a second load unchanged, got %+v", p1)
	}
}

func TestLoadDashboardPages_ClearsDanglingDisplayID(t *testing.T) {
	tempDir := t.TempDir()
	pagesPath := filepath.Join(tempDir, "dashboard-pages.json")
	t.Setenv("DASHBOARD_PAGES_FILE", pagesPath)

	now := time.Now().UTC()
	saved := dashboardPagesFile{
		Pages: []*dashboardPageData{
			{ID: "p1", Name: "Wall: Ghost", DisplayID: "ghost-id", DwellSeconds: 30, Widgets: []dashboardLayoutItem{}, CreatedAt: now, UpdatedAt: now},
		},
	}
	savedBytes, err := json.Marshal(saved)
	if err != nil {
		t.Fatalf("failed to marshal fixture: %v", err)
	}
	if err := os.WriteFile(pagesPath, savedBytes, 0o644); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	loadDashboardPages()

	dashboardPagesMu.RLock()
	p1 := dashboardPagesState["p1"]
	dashboardPagesMu.RUnlock()
	if p1.DisplayID != "" {
		t.Fatalf("expected a display_id naming no display to be cleared, got %q", p1.DisplayID)
	}
	if p1.DwellSeconds != 30 {
		t.Fatalf("expected dwell_seconds to survive the repair untouched, got %d", p1.DwellSeconds)
	}

	onDisk, err := os.ReadFile(pagesPath)
	if err != nil {
		t.Fatalf("failed to read rewritten file: %v", err)
	}
	var reloaded dashboardPagesFile
	if err := json.Unmarshal(onDisk, &reloaded); err != nil {
		t.Fatalf("failed to parse rewritten file: %v", err)
	}
	if reloaded.Pages[0].DisplayID != "" {
		t.Fatal("expected the repair to be persisted to disk, not just held in memory")
	}
}

func TestLoadDashboardPages_ClearsHeroOnDisplayPages(t *testing.T) {
	tempDir := t.TempDir()
	pagesPath := filepath.Join(tempDir, "dashboard-pages.json")
	t.Setenv("DASHBOARD_PAGES_FILE", pagesPath)

	now := time.Now().UTC()
	saved := dashboardPagesFile{
		Pages: []*dashboardPageData{
			{
				ID: "p1", Name: "Wall: Engines", DisplayID: "d1", DwellSeconds: 30, Hero: "wind",
				Widgets:   []dashboardLayoutItem{{ID: "wind", X: 0, Y: 0, W: 4, H: 8}},
				CreatedAt: now, UpdatedAt: now,
			},
		},
		Displays: []*displayData{
			{ID: "d1", Name: "Flybridge", Slug: "flybridge", Width: 1920, Height: 360, Rotate: 180, CreatedAt: now, UpdatedAt: now},
		},
	}
	savedBytes, err := json.Marshal(saved)
	if err != nil {
		t.Fatalf("failed to marshal fixture: %v", err)
	}
	if err := os.WriteFile(pagesPath, savedBytes, 0o644); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	loadDashboardPages()

	dashboardPagesMu.RLock()
	p1 := dashboardPagesState["p1"]
	dashboardPagesMu.RUnlock()
	if p1.Hero != "" {
		t.Fatalf("expected hero to be cleared on a hand-edited page that also has a display_id, got %q", p1.Hero)
	}
	if p1.DisplayID != "d1" {
		t.Fatalf("expected display_id to survive the repair, got %q", p1.DisplayID)
	}
}

// Plan §1 step 4: inventing a duration is guessing, so a kiosk-flagged page
// with no stored duration (validation forbids this combination; one should
// never exist) is left off the synthesized display rather than assigned an
// arbitrary one.
func TestLoadDashboardPages_LeavesKioskFlagWithNoDwellUnassigned(t *testing.T) {
	tempDir := t.TempDir()
	pagesPath := filepath.Join(tempDir, "dashboard-pages.json")
	t.Setenv("DASHBOARD_PAGES_FILE", pagesPath)

	fixture := map[string]any{
		"pages": []map[string]any{
			{
				"id": "p1", "name": "Wall: No Duration", "kiosk": true,
				"widgets": []any{}, "created_at": "2024-01-01T00:00:00Z", "updated_at": "2024-01-01T00:00:00Z",
			},
		},
	}
	fixtureBytes, err := json.Marshal(fixture)
	if err != nil {
		t.Fatalf("failed to marshal fixture: %v", err)
	}
	if err := os.WriteFile(pagesPath, fixtureBytes, 0o644); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	loadDashboardPages()

	dashboardPagesMu.RLock()
	displays := orderedDisplaysLocked()
	p1 := dashboardPagesState["p1"]
	dashboardPagesMu.RUnlock()

	if len(displays) != 1 {
		t.Fatalf("expected the display to still be synthesized, got %d", len(displays))
	}
	if p1.DisplayID != "" {
		t.Fatalf("expected the page with no stored duration to be left unassigned, got %q", p1.DisplayID)
	}
}

// --- Embed widget instances (ADR 0031) ---------------------------------------

func embedWidget(token, title, url string) dashboardLayoutItem {
	return dashboardLayoutItem{
		ID: embedWidgetIDPrefix + token, X: 0, Y: 0, W: 6, H: 8,
		Embed: &dashboardEmbedConfig{Title: title, URL: url},
	}
}

func TestValidateDashboardWidgets_AcceptsEmbedInstance(t *testing.T) {
	widgets := []dashboardLayoutItem{
		{ID: "wind", X: 0, Y: 0, W: 4, H: 8},
		embedWidget("m1x8abcd", "Windrose", "http://boat.local:3000/d-solo/abc/windrose?panelId=2"),
	}
	if msg := validateDashboardWidgets(widgets, ""); msg != "" {
		t.Fatalf("expected embed widget to validate, got %q", msg)
	}
}

// The whole point of the embed:<token> id scheme: several embeds on one page.
// The duplicate-id check is untouched and still applies, since tokens differ.
func TestValidateDashboardWidgets_AcceptsMultipleEmbedInstances(t *testing.T) {
	widgets := []dashboardLayoutItem{
		embedWidget("m1x8abcd", "Windrose", "https://grafana.local/d-solo/a?panelId=1"),
		embedWidget("m1x8efgh", "Polars", "https://grafana.local/d-solo/a?panelId=2"),
	}
	if msg := validateDashboardWidgets(widgets, ""); msg != "" {
		t.Fatalf("expected two distinct embeds to validate, got %q", msg)
	}
}

func TestValidateDashboardWidgets_RejectsDuplicateEmbedInstance(t *testing.T) {
	widgets := []dashboardLayoutItem{
		embedWidget("m1x8abcd", "Windrose", "https://grafana.local/d-solo/a?panelId=1"),
		embedWidget("m1x8abcd", "Windrose Again", "https://grafana.local/d-solo/a?panelId=2"),
	}
	if msg := validateDashboardWidgets(widgets, ""); msg == "" {
		t.Fatal("expected duplicate embed token to be rejected")
	}
}

func TestValidateDashboardWidgets_RejectsBadEmbeds(t *testing.T) {
	longURL := "https://grafana.local/?q=" + strings.Repeat("x", 2048)

	cases := []struct {
		name   string
		widget dashboardLayoutItem
	}{
		{"token too short", embedWidget("abc", "T", "https://grafana.local/d-solo/a")},
		{"token has illegal characters", embedWidget("abc/../def", "T", "https://grafana.local/d-solo/a")},
		{"empty token", embedWidget("", "T", "https://grafana.local/d-solo/a")},
		{"blank url", embedWidget("m1x8abcd", "T", "")},
		{"whitespace url", embedWidget("m1x8abcd", "T", "   ")},
		{"javascript scheme", embedWidget("m1x8abcd", "T", "javascript:alert(1)")},
		{"file scheme", embedWidget("m1x8abcd", "T", "file:///etc/passwd")},
		{"data scheme", embedWidget("m1x8abcd", "T", "data:text/html,<script>alert(1)</script>")},
		{"scheme-relative url has no scheme", embedWidget("m1x8abcd", "T", "//grafana.local/d-solo/a")},
		{"http url with no host", embedWidget("m1x8abcd", "T", "http:///d-solo/a")},
		{"url over the length cap", embedWidget("m1x8abcd", "T", longURL)},
		{"title over the length cap", embedWidget("m1x8abcd", strings.Repeat("t", 65), "https://grafana.local/a")},
		{
			"missing embed config",
			dashboardLayoutItem{ID: embedWidgetIDPrefix + "m1x8abcd", X: 0, Y: 0, W: 6, H: 8},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if msg := validateDashboardWidgets([]dashboardLayoutItem{tc.widget}, ""); msg == "" {
				t.Fatalf("expected %s to be rejected", tc.name)
			}
		})
	}
}

// Fail fast rather than silently dropping config the renderer would never read.
func TestValidateDashboardWidgets_RejectsEmbedConfigOnBuiltinWidget(t *testing.T) {
	widgets := []dashboardLayoutItem{
		{ID: "wind", X: 0, Y: 0, W: 4, H: 8, Embed: &dashboardEmbedConfig{URL: "https://grafana.local/a"}},
	}
	if msg := validateDashboardWidgets(widgets, ""); msg == "" {
		t.Fatal("expected embed config on a builtin widget id to be rejected")
	}
}

func TestCreateDashboardPageHandler_RejectsEmbedWithNonHTTPURL(t *testing.T) {
	setupDashboardPagesTest(t)

	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/dashboard-pages", map[string]any{
		"name": "Hostile Embed",
		"widgets": []map[string]any{
			{"id": "embed:m1x8abcd", "x": 0, "y": 0, "w": 6, "h": 8,
				"embed": map[string]any{"title": "X", "url": "javascript:alert(1)"}},
		},
	})
	if err := createDashboardPageHandler(c); err != nil {
		t.Fatalf("createDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestDashboardPages_EmbedConfigSurvivesReload(t *testing.T) {
	setupDashboardPagesTest(t)

	const panelURL = "http://boat.local:3000/d-solo/abc/windrose?panelId=2&kiosk"
	page := createTestDashboardPage(t, "Grafana", []dashboardLayoutItem{
		embedWidget("m1x8abcd", "Windrose", panelURL),
	})

	// Simulate a process restart: reload state from disk.
	loadDashboardPages()

	dashboardPagesMu.RLock()
	defer dashboardPagesMu.RUnlock()
	reloaded, ok := dashboardPagesState[page.ID]
	if !ok {
		t.Fatal("expected page to survive reload")
	}
	if len(reloaded.Widgets) != 1 {
		t.Fatalf("expected 1 widget after reload, got %d", len(reloaded.Widgets))
	}
	got := reloaded.Widgets[0]
	if got.Embed == nil {
		t.Fatal("expected embed config to survive reload")
	}
	if got.Embed.URL != panelURL {
		t.Fatalf("expected url %q, got %q", panelURL, got.Embed.URL)
	}
	if got.Embed.Title != "Windrose" {
		t.Fatalf("expected title %q, got %q", "Windrose", got.Embed.Title)
	}
}

// omitempty keeps existing dashboard-pages.json files byte-identical: a builtin
// widget must not gain an "embed": null key just because the struct grew a field.
func TestDashboardLayoutItem_OmitsEmbedKeyWhenAbsent(t *testing.T) {
	encoded, err := json.Marshal(dashboardLayoutItem{ID: "wind", X: 0, Y: 0, W: 4, H: 8})
	if err != nil {
		t.Fatalf("failed to marshal layout item: %v", err)
	}
	if strings.Contains(string(encoded), "embed") {
		t.Fatalf("expected no embed key for a builtin widget, got %s", encoded)
	}
}

// Frameless mode (a wall-display feature: no title bar, no padding) is
// per-instance like the rest of the embed config, so it must ride the same
// reload path.
func TestDashboardPages_EmbedFramelessSurvivesReload(t *testing.T) {
	setupDashboardPagesTest(t)

	const panelURL = "http://boat.local:3000/d-solo/abc/camera?panelId=2&kiosk"
	page := createTestDashboardPage(t, "Wall Display", []dashboardLayoutItem{
		{
			ID: embedWidgetIDPrefix + "m1x8abcd", X: 0, Y: 0, W: 12, H: 7,
			Embed: &dashboardEmbedConfig{Title: "Camera", URL: panelURL, Frameless: true},
		},
	})

	// Simulate a process restart: reload state from disk.
	loadDashboardPages()

	dashboardPagesMu.RLock()
	defer dashboardPagesMu.RUnlock()
	reloaded, ok := dashboardPagesState[page.ID]
	if !ok {
		t.Fatal("expected page to survive reload")
	}
	if len(reloaded.Widgets) != 1 {
		t.Fatalf("expected 1 widget after reload, got %d", len(reloaded.Widgets))
	}
	got := reloaded.Widgets[0]
	if got.Embed == nil {
		t.Fatal("expected embed config to survive reload")
	}
	if got.Embed.Frameless != true {
		t.Fatalf("expected frameless to survive reload as true, got %v", got.Embed.Frameless)
	}
}

// omitempty keeps existing dashboard-pages.json files byte-identical: an embed
// saved before this field existed (or one left in its default framed mode)
// must not gain a "frameless": false key.
func TestDashboardLayoutItem_OmitsFramelessKeyWhenFalse(t *testing.T) {
	encoded, err := json.Marshal(dashboardEmbedConfig{Title: "X", URL: "http://x"})
	if err != nil {
		t.Fatalf("failed to marshal embed config: %v", err)
	}
	if strings.Contains(string(encoded), "frameless") {
		t.Fatalf("expected no frameless key when false, got %s", encoded)
	}
}

func TestDashboardLayoutItem_IncludesFramelessKeyWhenTrue(t *testing.T) {
	encoded, err := json.Marshal(dashboardEmbedConfig{Title: "X", URL: "http://x", Frameless: true})
	if err != nil {
		t.Fatalf("failed to marshal embed config: %v", err)
	}
	if !strings.Contains(string(encoded), "frameless") {
		t.Fatalf("expected frameless key when true, got %s", encoded)
	}
}

func gaugeWidget(id string, config *dashboardGaugeConfig) dashboardLayoutItem {
	return dashboardLayoutItem{ID: id, X: 0, Y: 0, W: 4, H: 4, Gauge: config}
}

func validGaugeConfig() *dashboardGaugeConfig {
	return &dashboardGaugeConfig{
		Path:     "propulsion.port.oilPressure",
		Label:    "Port oil pressure",
		Display:  "radial",
		Quantity: "pressure",
		Unit:     "psi",
	}
}

// ── autopilot widget registration ────────────────────────────────────────────

func TestValidateDashboardWidgets_AcceptsAutopilotBuiltin(t *testing.T) {
	widgets := []dashboardLayoutItem{{ID: "autopilot", X: 0, Y: 0, W: 4, H: 6}}
	if msg := validateDashboardWidgets(widgets, ""); msg != "" {
		t.Fatalf("expected the autopilot builtin id to be accepted, got %q", msg)
	}
}

// radar-targets has been a real frontend widget (frontend/src/lib/dashboard-widgets.ts,
// rendered by App.tsx's renderWidget and constrained in dashboard-bento-grid.tsx)
// since before this test existed, but was never added to this map - a latent
// 400 on save for any page that placed it. The four wall-display tiles are new
// in the same change that fixes that gap.
func TestValidateDashboardWidgets_AcceptsWallDisplayAndRadarTargetsBuiltins(t *testing.T) {
	for _, id := range []string{"radar-targets", "clock", "current-conditions", "forecast-days", "sea-state"} {
		widgets := []dashboardLayoutItem{{ID: id, X: 0, Y: 0, W: 4, H: 6}}
		if msg := validateDashboardWidgets(widgets, ""); msg != "" {
			t.Errorf("expected builtin id %q to be accepted, got %q", id, msg)
		}
	}
}

func TestValidateDashboardWidgets_RejectsEmbedConfigOnAutopilotWidget(t *testing.T) {
	widgets := []dashboardLayoutItem{
		{ID: "autopilot", X: 0, Y: 0, W: 4, H: 6, Embed: &dashboardEmbedConfig{URL: "https://grafana.local/a"}},
	}
	if msg := validateDashboardWidgets(widgets, ""); msg == "" {
		t.Fatal("expected embed config on the autopilot widget to be rejected")
	}
}

func TestValidateDashboardWidgets_RejectsGaugeConfigOnAutopilotWidget(t *testing.T) {
	widget := dashboardLayoutItem{ID: "autopilot", X: 0, Y: 0, W: 4, H: 6, Gauge: validGaugeConfig()}
	if msg := validateDashboardWidgets([]dashboardLayoutItem{widget}, ""); msg == "" {
		t.Fatal("expected gauge config on the autopilot widget to be rejected")
	}
}

func TestValidateGaugeWidgetAcceptsAWellFormedGauge(t *testing.T) {
	if msg := validateDashboardWidgets([]dashboardLayoutItem{gaugeWidget("gauge:abcd1234", validGaugeConfig())}, ""); msg != "" {
		t.Fatalf("expected a valid gauge to be accepted, got %q", msg)
	}
}

func TestValidateGaugeWidgetRejectsBadInput(t *testing.T) {
	cases := []struct {
		name   string
		widget dashboardLayoutItem
	}{
		{"short token", gaugeWidget("gauge:abc", validGaugeConfig())},
		{"missing config", gaugeWidget("gauge:abcd1234", nil)},
		{"blank path", gaugeWidget("gauge:abcd1234", &dashboardGaugeConfig{Display: "radial"})},
		{"unknown display", gaugeWidget("gauge:abcd1234", &dashboardGaugeConfig{Path: "a.b", Display: "hologram"})},
	}

	for _, tc := range cases {
		if msg := validateDashboardWidgets([]dashboardLayoutItem{tc.widget}, ""); msg == "" {
			t.Fatalf("%s: expected rejection", tc.name)
		}
	}
}

// min >= max would make the arc maths meaningless rather than merely ugly.
func TestValidateGaugeWidgetRejectsInvertedRange(t *testing.T) {
	config := validGaugeConfig()
	low, high := 100.0, 10.0
	config.Min, config.Max = &low, &high

	if msg := validateDashboardWidgets([]dashboardLayoutItem{gaugeWidget("gauge:abcd1234", config)}, ""); msg == "" {
		t.Fatalf("expected an inverted range to be rejected")
	}
}

// Zones reuse the alarm severity vocabulary (ADR 0038) rather than inventing
// gauge-only colours, so an unknown state is a real error.
func TestValidateGaugeWidgetRejectsUnknownZoneState(t *testing.T) {
	config := validGaugeConfig()
	config.Zones = []gaugeZone{{From: 0, To: 10, State: "spicy"}}

	if msg := validateDashboardWidgets([]dashboardLayoutItem{gaugeWidget("gauge:abcd1234", config)}, ""); msg == "" {
		t.Fatalf("expected an unknown zone state to be rejected")
	}
}

// Reject rather than silently drop, matching the embed precedent: config the
// renderer will never read means the caller has misunderstood the model.
func TestValidateDashboardWidgetsRejectsGaugeConfigOnABuiltinWidget(t *testing.T) {
	widget := dashboardLayoutItem{ID: "wind", X: 0, Y: 0, W: 4, H: 4, Gauge: validGaugeConfig()}

	if msg := validateDashboardWidgets([]dashboardLayoutItem{widget}, ""); msg == "" {
		t.Fatalf("expected gauge config on a builtin id to be rejected")
	}
}

func TestGaugeBoundPathsDeduplicatesAcrossPages(t *testing.T) {
	dashboardPagesMu.Lock()
	previous := dashboardPagesState
	dashboardPagesState = map[string]*dashboardPageData{
		"a": {ID: "a", Widgets: []dashboardLayoutItem{
			gaugeWidget("gauge:aaaa1111", &dashboardGaugeConfig{Path: "propulsion.port.oilPressure", Display: "radial"}),
			gaugeWidget("gauge:bbbb2222", &dashboardGaugeConfig{Path: "environment.depth.belowTransducer", Display: "numeric"}),
		}},
		"b": {ID: "b", Widgets: []dashboardLayoutItem{
			gaugeWidget("gauge:cccc3333", &dashboardGaugeConfig{Path: "propulsion.port.oilPressure", Display: "bar"}),
		}},
	}
	dashboardPagesMu.Unlock()
	t.Cleanup(func() {
		dashboardPagesMu.Lock()
		dashboardPagesState = previous
		dashboardPagesMu.Unlock()
	})

	paths := gaugeBoundPaths()
	if len(paths) != 2 {
		t.Fatalf("the same path on two pages is one subscription, got %v", paths)
	}
	if paths[0] != "environment.depth.belowTransducer" || paths[1] != "propulsion.port.oilPressure" {
		t.Fatalf("expected sorted unique paths, got %v", paths)
	}
}

// ── gauge group widget (ADR 0049) ────────────────────────────────────────────

func gaugeGroupWidget(id string, config *dashboardGaugeGroupConfig) dashboardLayoutItem {
	return dashboardLayoutItem{ID: id, X: 0, Y: 0, W: 6, H: 8, GaugeGroup: config}
}

func validGaugeGroupConfig() *dashboardGaugeGroupConfig {
	return &dashboardGaugeGroupConfig{
		Title: "Port",
		Gauges: []dashboardGaugeConfig{
			{Path: "propulsion.port.revolutions", Label: "RPM", Display: "radial", Quantity: "frequency", Unit: "rpm"},
			{Path: "propulsion.port.oilPressure", Label: "Oil", Display: "bar", Quantity: "pressure", Unit: "psi"},
		},
	}
}

func TestValidateGaugeGroupAcceptsAWellFormedGroup(t *testing.T) {
	widget := gaugeGroupWidget("gauge-group:abcd1234", validGaugeGroupConfig())
	if msg := validateDashboardWidgets([]dashboardLayoutItem{widget}, ""); msg != "" {
		t.Fatalf("expected a valid gauge group to be accepted, got %q", msg)
	}
}

func TestValidateGaugeGroupAcceptsHeroIndexInRange(t *testing.T) {
	config := validGaugeGroupConfig()
	hero := 0
	config.Hero = &hero
	widget := gaugeGroupWidget("gauge-group:abcd1234", config)
	if msg := validateDashboardWidgets([]dashboardLayoutItem{widget}, ""); msg != "" {
		t.Fatalf("expected a valid hero index to be accepted, got %q", msg)
	}
}

func TestValidateGaugeGroupRejectsBadInput(t *testing.T) {
	tooMany := validGaugeGroupConfig()
	tooMany.Gauges = make([]dashboardGaugeConfig, gaugeGroupMaxGauges+1)
	for i := range tooMany.Gauges {
		tooMany.Gauges[i] = dashboardGaugeConfig{Path: "a.b", Display: "numeric"}
	}

	blankPath := validGaugeGroupConfig()
	blankPath.Gauges[1].Path = "   "

	badDisplay := validGaugeGroupConfig()
	badDisplay.Gauges[0].Display = "hologram"

	invertedRange := validGaugeGroupConfig()
	low, high := 100.0, 10.0
	invertedRange.Gauges[0].Min, invertedRange.Gauges[0].Max = &low, &high

	badZone := validGaugeGroupConfig()
	badZone.Gauges[0].Zones = []gaugeZone{{From: 0, To: 10, State: "spicy"}}

	longTitle := validGaugeGroupConfig()
	longTitle.Title = strings.Repeat("x", gaugeGroupTitleMaxLen+1)

	zeroColumns := validGaugeGroupConfig()
	zero := 0
	zeroColumns.Columns = &zero

	negativeHero := validGaugeGroupConfig()
	neg := -1
	negativeHero.Hero = &neg

	pastEndHero := validGaugeGroupConfig()
	out := len(pastEndHero.Gauges)
	pastEndHero.Hero = &out

	emptyGauges := validGaugeGroupConfig()
	emptyGauges.Gauges = nil

	cases := []struct {
		name   string
		widget dashboardLayoutItem
	}{
		{"short token", gaugeGroupWidget("gauge-group:abc", validGaugeGroupConfig())},
		{"missing config", gaugeGroupWidget("gauge-group:abcd1234", nil)},
		{"no gauges", gaugeGroupWidget("gauge-group:abcd1234", emptyGauges)},
		{"too many gauges", gaugeGroupWidget("gauge-group:abcd1234", tooMany)},
		{"blank member path", gaugeGroupWidget("gauge-group:abcd1234", blankPath)},
		{"unknown member display", gaugeGroupWidget("gauge-group:abcd1234", badDisplay)},
		{"inverted member range", gaugeGroupWidget("gauge-group:abcd1234", invertedRange)},
		{"unknown member zone state", gaugeGroupWidget("gauge-group:abcd1234", badZone)},
		{"title too long", gaugeGroupWidget("gauge-group:abcd1234", longTitle)},
		{"zero columns", gaugeGroupWidget("gauge-group:abcd1234", zeroColumns)},
		{"negative hero", gaugeGroupWidget("gauge-group:abcd1234", negativeHero)},
		{"hero past end", gaugeGroupWidget("gauge-group:abcd1234", pastEndHero)},
	}

	for _, tc := range cases {
		if msg := validateDashboardWidgets([]dashboardLayoutItem{tc.widget}, ""); msg == "" {
			t.Fatalf("%s: expected rejection", tc.name)
		}
	}
}

// Reject rather than silently drop, matching the embed and gauge precedent.
func TestValidateDashboardWidgetsRejectsMismatchedGroupConfig(t *testing.T) {
	group := validGaugeGroupConfig()

	cases := []struct {
		name   string
		widget dashboardLayoutItem
	}{
		{"group config on a builtin", dashboardLayoutItem{ID: "wind", X: 0, Y: 0, W: 4, H: 4, GaugeGroup: group}},
		{"group config on a gauge", dashboardLayoutItem{ID: "gauge:abcd1234", X: 0, Y: 0, W: 4, H: 4, Gauge: validGaugeConfig(), GaugeGroup: group}},
		{"group config on an embed", dashboardLayoutItem{ID: "embed:abcd1234", X: 0, Y: 0, W: 4, H: 4, Embed: &dashboardEmbedConfig{URL: "https://grafana.local/a"}, GaugeGroup: group}},
		{"gauge config on a group", dashboardLayoutItem{ID: "gauge-group:abcd1234", X: 0, Y: 0, W: 4, H: 4, Gauge: validGaugeConfig(), GaugeGroup: group}},
		{"embed config on a group", dashboardLayoutItem{ID: "gauge-group:abcd1234", X: 0, Y: 0, W: 4, H: 4, Embed: &dashboardEmbedConfig{URL: "https://grafana.local/a"}, GaugeGroup: group}},
	}

	for _, tc := range cases {
		if msg := validateDashboardWidgets([]dashboardLayoutItem{tc.widget}, ""); msg == "" {
			t.Fatalf("%s: expected rejection", tc.name)
		}
	}
}

// The highest-risk omission: a group whose paths are never collected renders
// the structural dash forever, with nothing in any log to say why.
func TestGaugeBoundPathsIncludesGroupMembers(t *testing.T) {
	dashboardPagesMu.Lock()
	previous := dashboardPagesState
	dashboardPagesState = map[string]*dashboardPageData{
		"a": {ID: "a", Widgets: []dashboardLayoutItem{
			gaugeWidget("gauge:aaaa1111", &dashboardGaugeConfig{Path: "propulsion.port.oilPressure", Display: "radial"}),
			gaugeGroupWidget("gauge-group:bbbb2222", &dashboardGaugeGroupConfig{
				Title: "Port",
				Gauges: []dashboardGaugeConfig{
					// Shared with the standalone gauge above: one subscription, not two.
					{Path: "propulsion.port.oilPressure", Display: "bar"},
					{Path: "propulsion.port.revolutions", Display: "radial"},
					{Path: "  environment.depth.belowTransducer  ", Display: "numeric"},
				},
			}),
		}},
	}
	dashboardPagesMu.Unlock()
	t.Cleanup(func() {
		dashboardPagesMu.Lock()
		dashboardPagesState = previous
		dashboardPagesMu.Unlock()
	})

	paths := gaugeBoundPaths()
	want := []string{"environment.depth.belowTransducer", "propulsion.port.oilPressure", "propulsion.port.revolutions"}
	if len(paths) != len(want) {
		t.Fatalf("expected %v, got %v", want, paths)
	}
	for i, p := range want {
		if paths[i] != p {
			t.Fatalf("expected sorted unique paths %v, got %v", want, paths)
		}
	}
}

func TestDashboardPages_GaugeGroupSurvivesReload(t *testing.T) {
	setupDashboardPagesTest(t)

	page := createTestDashboardPage(t, "Engines", []dashboardLayoutItem{
		gaugeGroupWidget("gauge-group:abcd1234", validGaugeGroupConfig()),
	})

	loadDashboardPages()

	dashboardPagesMu.RLock()
	reloaded, ok := dashboardPagesState[page.ID]
	dashboardPagesMu.RUnlock()
	if !ok {
		t.Fatal("expected the page to survive a reload")
	}
	if len(reloaded.Widgets) != 1 || reloaded.Widgets[0].GaugeGroup == nil {
		t.Fatalf("expected the gauge group config to survive, got %+v", reloaded.Widgets)
	}
	group := reloaded.Widgets[0].GaugeGroup
	if group.Title != "Port" || len(group.Gauges) != 2 {
		t.Fatalf("expected the group's title and members to survive, got %+v", group)
	}
	if group.Gauges[1].Path != "propulsion.port.oilPressure" {
		t.Fatalf("expected member paths to survive in order, got %+v", group.Gauges)
	}
}

// omitempty keeps existing dashboard-pages.json files byte-identical.
func TestDashboardLayoutItem_OmitsGaugeGroupKeyWhenAbsent(t *testing.T) {
	encoded, err := json.Marshal(dashboardLayoutItem{ID: "wind", X: 0, Y: 0, W: 4, H: 4})
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	if strings.Contains(string(encoded), "gaugeGroup") {
		t.Fatalf("expected no gaugeGroup key on a widget without one, got %s", encoded)
	}
}

// ── poi map widget (ADR 0091 phase 3b) ───────────────────────────────────────

func poiMapWidget(id string, config *dashboardPoiMapConfig) dashboardLayoutItem {
	return dashboardLayoutItem{ID: id, X: 0, Y: 0, W: 12, H: 7, PoiMap: config}
}

func validPoiMapConfig() *dashboardPoiMapConfig {
	return &dashboardPoiMapConfig{
		Title:      "Nearby",
		RangeNm:    5,
		Categories: []string{"anchorage", "marina", "fuel"},
		Layout:     "split",
	}
}

func TestValidateDashboardWidgets_AcceptsPoiMapInstance(t *testing.T) {
	widget := poiMapWidget("poi-map:m1x8abcd", validPoiMapConfig())
	if msg := validateDashboardWidgets([]dashboardLayoutItem{widget}, ""); msg != "" {
		t.Fatalf("expected a valid poi map widget to be accepted, got %q", msg)
	}
}

func TestValidateDashboardWidgets_AcceptsMultiplePoiMapInstances(t *testing.T) {
	widgets := []dashboardLayoutItem{
		poiMapWidget("poi-map:m1x8abcd", validPoiMapConfig()),
		poiMapWidget("poi-map:m1x8efgh", validPoiMapConfig()),
	}
	if msg := validateDashboardWidgets(widgets, ""); msg != "" {
		t.Fatalf("expected two distinct poi map instances to validate, got %q", msg)
	}
}

func TestValidateDashboardWidgets_RejectsDuplicatePoiMapInstance(t *testing.T) {
	widgets := []dashboardLayoutItem{
		poiMapWidget("poi-map:m1x8abcd", validPoiMapConfig()),
		poiMapWidget("poi-map:m1x8abcd", validPoiMapConfig()),
	}
	if msg := validateDashboardWidgets(widgets, ""); msg == "" {
		t.Fatal("expected duplicate poi map token to be rejected")
	}
}

func TestValidatePoiMapWidget_RejectsBadInput(t *testing.T) {
	tooLongTitle := validPoiMapConfig()
	tooLongTitle.Title = strings.Repeat("t", gaugeGroupTitleMaxLen+1)

	noCategories := validPoiMapConfig()
	noCategories.Categories = nil

	unknownCategory := validPoiMapConfig()
	unknownCategory.Categories = []string{"anchorage", "moon-base"}

	tooSmallRange := validPoiMapConfig()
	tooSmallRange.RangeNm = 0.4

	tooBigRange := validPoiMapConfig()
	tooBigRange.RangeNm = 25.1

	unknownLayout := validPoiMapConfig()
	unknownLayout.Layout = "carousel"

	cases := []struct {
		name   string
		widget dashboardLayoutItem
	}{
		{"short token", poiMapWidget("poi-map:abc", validPoiMapConfig())},
		{"missing config", poiMapWidget("poi-map:m1x8abcd", nil)},
		{"title too long", poiMapWidget("poi-map:m1x8abcd", tooLongTitle)},
		{"no categories", poiMapWidget("poi-map:m1x8abcd", noCategories)},
		{"unknown category", poiMapWidget("poi-map:m1x8abcd", unknownCategory)},
		{"range below minimum", poiMapWidget("poi-map:m1x8abcd", tooSmallRange)},
		{"range above maximum", poiMapWidget("poi-map:m1x8abcd", tooBigRange)},
		{"unknown layout", poiMapWidget("poi-map:m1x8abcd", unknownLayout)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if msg := validateDashboardWidgets([]dashboardLayoutItem{tc.widget}, ""); msg == "" {
				t.Fatalf("expected %s to be rejected", tc.name)
			}
		})
	}
}

// Reject rather than silently drop, matching the embed and gauge-group precedent.
func TestValidateDashboardWidgetsRejectsMismatchedPoiMapConfig(t *testing.T) {
	poiMap := validPoiMapConfig()

	cases := []struct {
		name   string
		widget dashboardLayoutItem
	}{
		{"poi map config on a builtin", dashboardLayoutItem{ID: "wind", X: 0, Y: 0, W: 4, H: 4, PoiMap: poiMap}},
		{"poi map config on a gauge", dashboardLayoutItem{ID: "gauge:abcd1234", X: 0, Y: 0, W: 4, H: 4, Gauge: validGaugeConfig(), PoiMap: poiMap}},
		{"poi map config on an embed", dashboardLayoutItem{ID: "embed:abcd1234", X: 0, Y: 0, W: 4, H: 4, Embed: &dashboardEmbedConfig{URL: "https://grafana.local/a"}, PoiMap: poiMap}},
		{"poi map config on a gauge group", dashboardLayoutItem{ID: "gauge-group:abcd1234", X: 0, Y: 0, W: 4, H: 4, GaugeGroup: validGaugeGroupConfig(), PoiMap: poiMap}},
		{"poi map config on a lamp strip", dashboardLayoutItem{ID: "lamps:abcd1234", X: 0, Y: 0, W: 4, H: 4, Lamps: &dashboardLampStripConfig{Title: "X", ShowCheck: true}, PoiMap: poiMap}},
		{"poi map config on a cluster", dashboardLayoutItem{ID: "cluster:abcd1234", X: 0, Y: 0, W: 4, H: 4, Cluster: &dashboardClusterConfig{Title: "X", Ring: *validGaugeConfig(), Centre: *validGaugeConfig()}, PoiMap: poiMap}},
		{"gauge config on a poi map", poiMapWidget("poi-map:abcd1234", poiMap)},
	}

	for i, tc := range cases {
		// The last case needs its own Gauge field set, which the table's shared
		// shape above can't express without duplicating every other row.
		if i == len(cases)-1 {
			tc.widget.Gauge = validGaugeConfig()
		}
		if msg := validateDashboardWidgets([]dashboardLayoutItem{tc.widget}, ""); msg == "" {
			t.Fatalf("%s: expected rejection", tc.name)
		}
	}
}

func TestDashboardPages_PoiMapConfigSurvivesReload(t *testing.T) {
	setupDashboardPagesTest(t)

	page := createTestDashboardPage(t, "Wall Nearby", []dashboardLayoutItem{
		poiMapWidget("poi-map:m1x8abcd", validPoiMapConfig()),
	})

	loadDashboardPages()

	dashboardPagesMu.RLock()
	reloaded, ok := dashboardPagesState[page.ID]
	dashboardPagesMu.RUnlock()
	if !ok {
		t.Fatal("expected the page to survive a reload")
	}
	if len(reloaded.Widgets) != 1 || reloaded.Widgets[0].PoiMap == nil {
		t.Fatalf("expected the poi map config to survive, got %+v", reloaded.Widgets)
	}
	poiMap := reloaded.Widgets[0].PoiMap
	if poiMap.Title != "Nearby" || poiMap.RangeNm != 5 || poiMap.Layout != "split" {
		t.Fatalf("expected the poi map's fields to survive, got %+v", poiMap)
	}
	if len(poiMap.Categories) != 3 || poiMap.Categories[0] != "anchorage" {
		t.Fatalf("expected category order to survive, got %+v", poiMap.Categories)
	}
}

// omitempty keeps existing dashboard-pages.json files byte-identical.
func TestDashboardLayoutItem_OmitsPoiMapKeyWhenAbsent(t *testing.T) {
	encoded, err := json.Marshal(dashboardLayoutItem{ID: "wind", X: 0, Y: 0, W: 4, H: 4})
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	if strings.Contains(string(encoded), "poiMap") {
		t.Fatalf("expected no poiMap key on a widget without one, got %s", encoded)
	}
}

// ── lamp strip widget (ADR 0052) ─────────────────────────────────────────────

func lampStripWidget(id string, config *dashboardLampStripConfig) dashboardLayoutItem {
	return dashboardLayoutItem{ID: id, X: 0, Y: 0, W: 12, H: 3, Lamps: config}
}

func validLampStripConfig() *dashboardLampStripConfig {
	return &dashboardLampStripConfig{
		Title: "Status",
		Lamps: []dashboardLamp{
			{Path: "electrical.generator.state", Label: "GEN"},
			{Path: "propulsion.wing.revolutions", Label: "WING"},
		},
		ShowCheck: true,
	}
}

func TestValidateLampStripAcceptsAWellFormedStrip(t *testing.T) {
	widget := lampStripWidget("lamps:abcd1234", validLampStripConfig())
	if msg := validateDashboardWidgets([]dashboardLayoutItem{widget}, ""); msg != "" {
		t.Fatalf("expected a valid lamp strip to be accepted, got %q", msg)
	}
}

// A strip with no lamps but a CHK indicator is legitimate: the rollup alone is
// a useful thing to pin to a page.
func TestValidateLampStripAcceptsCheckOnlyStrip(t *testing.T) {
	config := &dashboardLampStripConfig{Title: "Status", ShowCheck: true}
	if msg := validateDashboardWidgets([]dashboardLayoutItem{lampStripWidget("lamps:abcd1234", config)}, ""); msg != "" {
		t.Fatalf("expected a check-only strip to be accepted, got %q", msg)
	}
}

func TestValidateLampStripRejectsBadInput(t *testing.T) {
	tooMany := validLampStripConfig()
	tooMany.Lamps = make([]dashboardLamp, lampStripMaxLamps+1)
	for i := range tooMany.Lamps {
		tooMany.Lamps[i] = dashboardLamp{Path: "a.b", Label: "X"}
	}

	blankPath := validLampStripConfig()
	blankPath.Lamps[1].Path = "  "

	empty := &dashboardLampStripConfig{Title: "Status"}

	longTitle := validLampStripConfig()
	longTitle.Title = strings.Repeat("x", gaugeGroupTitleMaxLen+1)

	cases := []struct {
		name   string
		widget dashboardLayoutItem
	}{
		{"short token", lampStripWidget("lamps:abc", validLampStripConfig())},
		{"missing config", lampStripWidget("lamps:abcd1234", nil)},
		{"no lamps and no check", lampStripWidget("lamps:abcd1234", empty)},
		{"too many lamps", lampStripWidget("lamps:abcd1234", tooMany)},
		{"blank lamp path", lampStripWidget("lamps:abcd1234", blankPath)},
		{"title too long", lampStripWidget("lamps:abcd1234", longTitle)},
	}

	for _, tc := range cases {
		if msg := validateDashboardWidgets([]dashboardLayoutItem{tc.widget}, ""); msg == "" {
			t.Fatalf("%s: expected rejection", tc.name)
		}
	}
}

func TestValidateDashboardWidgetsRejectsMismatchedLampConfig(t *testing.T) {
	lamps := validLampStripConfig()
	cases := []struct {
		name   string
		widget dashboardLayoutItem
	}{
		{"lamps on a builtin", dashboardLayoutItem{ID: "wind", X: 0, Y: 0, W: 4, H: 4, Lamps: lamps}},
		{"lamps on a gauge", dashboardLayoutItem{ID: "gauge:abcd1234", X: 0, Y: 0, W: 4, H: 4, Gauge: validGaugeConfig(), Lamps: lamps}},
		{"lamps on a group", dashboardLayoutItem{ID: "gauge-group:abcd1234", X: 0, Y: 0, W: 4, H: 4, GaugeGroup: validGaugeGroupConfig(), Lamps: lamps}},
		{"gauge config on a strip", dashboardLayoutItem{ID: "lamps:abcd1234", X: 0, Y: 0, W: 4, H: 4, Gauge: validGaugeConfig(), Lamps: lamps}},
	}

	for _, tc := range cases {
		if msg := validateDashboardWidgets([]dashboardLayoutItem{tc.widget}, ""); msg == "" {
			t.Fatalf("%s: expected rejection", tc.name)
		}
	}
}

// The third time this walker has had to learn about a new widget. Miss it and
// every lamp is permanently dark, with nothing in any log to say why.
func TestGaugeBoundPathsIncludesLampPaths(t *testing.T) {
	dashboardPagesMu.Lock()
	previous := dashboardPagesState
	dashboardPagesState = map[string]*dashboardPageData{
		"a": {ID: "a", Widgets: []dashboardLayoutItem{
			gaugeWidget("gauge:aaaa1111", &dashboardGaugeConfig{Path: "propulsion.port.oilPressure", Display: "radial"}),
			lampStripWidget("lamps:bbbb2222", &dashboardLampStripConfig{
				Title: "Status",
				Lamps: []dashboardLamp{
					{Path: "electrical.generator.state", Label: "GEN"},
					// Shared with the gauge above: one subscription, not two.
					{Path: "propulsion.port.oilPressure", Label: "OIL"},
				},
			}),
		}},
	}
	dashboardPagesMu.Unlock()
	t.Cleanup(func() {
		dashboardPagesMu.Lock()
		dashboardPagesState = previous
		dashboardPagesMu.Unlock()
	})

	paths := gaugeBoundPaths()
	want := []string{"electrical.generator.state", "propulsion.port.oilPressure"}
	if len(paths) != len(want) || paths[0] != want[0] || paths[1] != want[1] {
		t.Fatalf("expected %v, got %v", want, paths)
	}
}

func TestDashboardLayoutItem_OmitsLampsKeyWhenAbsent(t *testing.T) {
	encoded, err := json.Marshal(dashboardLayoutItem{ID: "wind", X: 0, Y: 0, W: 4, H: 4})
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	if strings.Contains(string(encoded), "lamps") {
		t.Fatalf("expected no lamps key on a widget without one, got %s", encoded)
	}
}

// ── vessel-level indicator ribbon (ADR 0082) ─────────────────────────────────
//
// The ribbon is the promoted lamp strip: one vessel-level config, not tied to
// any page, sharing validateLampStripConfig with the per-page lamps: widget
// so it can never accept something the widget would have rejected, or the
// reverse. It rides in the same pages file as a sibling of Pages, kept in
// memory under the same mutex — which means every code path that persists
// dashboard state has to remember to carry it forward, the same trap ADR
// 0060 §7 documents for Skin.

func putDashboardRibbon(t *testing.T, ribbon any) (*httptest.ResponseRecorder, string) {
	t.Helper()
	c, rec := newDashboardPagesRequest(t, http.MethodPut, "/api/dashboard-ribbon", map[string]any{"ribbon": ribbon})
	if err := putDashboardRibbonHandler(c); err != nil {
		t.Fatalf("putDashboardRibbonHandler returned error: %v", err)
	}
	return rec, rec.Body.String()
}

func getDashboardRibbon(t *testing.T) (*httptest.ResponseRecorder, *dashboardLampStripConfig) {
	t.Helper()
	c, rec := newDashboardPagesRequest(t, http.MethodGet, "/api/dashboard-ribbon", nil)
	if err := getDashboardRibbonHandler(c); err != nil {
		t.Fatalf("getDashboardRibbonHandler returned error: %v", err)
	}
	var payload struct {
		Ribbon *dashboardLampStripConfig `json:"ribbon"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse ribbon response: %v", err)
	}
	return rec, payload.Ribbon
}

func TestDashboardRibbon_NilInitially(t *testing.T) {
	setupDashboardPagesTest(t)

	rec, ribbon := getDashboardRibbon(t)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if ribbon != nil {
		t.Fatalf("expected no ribbon initially, got %+v", ribbon)
	}
}

func TestDashboardRibbon_RoundTripsThroughPutAndGet(t *testing.T) {
	setupDashboardPagesTest(t)

	rec, body := putDashboardRibbon(t, validLampStripConfig())
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, body)
	}

	getRec, ribbon := getDashboardRibbon(t)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, getRec.Code, getRec.Body.String())
	}
	if ribbon == nil || ribbon.Title != "Status" || len(ribbon.Lamps) != 2 {
		t.Fatalf("expected the saved ribbon to round-trip, got %+v", ribbon)
	}

	dashboardPagesMu.RLock()
	stored := dashboardRibbonState
	dashboardPagesMu.RUnlock()
	if stored == nil || stored.Title != "Status" {
		t.Fatalf("expected the ribbon to be held in memory after PUT, got %+v", stored)
	}
}

func TestDashboardRibbon_PutNullClears(t *testing.T) {
	setupDashboardPagesTest(t)

	if rec, body := putDashboardRibbon(t, validLampStripConfig()); rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, body)
	}

	rec, body := putDashboardRibbon(t, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, body)
	}

	dashboardPagesMu.RLock()
	stored := dashboardRibbonState
	dashboardPagesMu.RUnlock()
	if stored != nil {
		t.Fatalf("expected a null PUT to clear the ribbon, got %+v", stored)
	}

	_, ribbon := getDashboardRibbon(t)
	if ribbon != nil {
		t.Fatalf("expected GET to report the cleared ribbon as null, got %+v", ribbon)
	}
}

func TestDashboardRibbon_RejectsAnEmptyStrip(t *testing.T) {
	setupDashboardPagesTest(t)

	empty := &dashboardLampStripConfig{Title: "Status"}
	rec, body := putDashboardRibbon(t, empty)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, body)
	}
}

func TestDashboardRibbon_RejectsSeventeenLamps(t *testing.T) {
	setupDashboardPagesTest(t)

	tooMany := validLampStripConfig()
	tooMany.Lamps = make([]dashboardLamp, lampStripMaxLamps+1)
	for i := range tooMany.Lamps {
		tooMany.Lamps[i] = dashboardLamp{Path: "a.b", Label: "X"}
	}

	rec, body := putDashboardRibbon(t, tooMany)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, body)
	}
}

// TestDashboardRibbon_SurvivesPatchingAPageWidgets is the ribbon's version of
// TestPatchDashboardPageHandler_PreservesSkinOnWidgetsOnlyPatch: set a
// ribbon, patch an unrelated page's widgets only (exactly what a layout drag
// sends), reload from disk as if the process had restarted, and confirm the
// ribbon is still there.
func TestDashboardRibbon_SurvivesPatchingAPageWidgets(t *testing.T) {
	setupDashboardPagesTest(t)
	page := createTestDashboardPage(t, "Test Page", sampleDashboardWidgets())

	if rec, body := putDashboardRibbon(t, validLampStripConfig()); rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, body)
	}

	newWidgets := []dashboardLayoutItem{{ID: "route", X: 0, Y: 0, W: 8, H: 8}}
	c, rec := newDashboardPagesRequest(t, http.MethodPatch, "/api/dashboard-pages/"+page.ID, map[string]any{
		"widgets": newWidgets,
	})
	c.SetParamNames("id")
	c.SetParamValues(page.ID)
	if err := patchDashboardPageHandler(c); err != nil {
		t.Fatalf("patchDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	loadDashboardPages()

	dashboardPagesMu.RLock()
	ribbon := dashboardRibbonState
	dashboardPagesMu.RUnlock()
	if ribbon == nil || ribbon.Title != "Status" {
		t.Fatalf("expected the ribbon to survive a page widgets-only patch and a reload, got %+v", ribbon)
	}
}

// TestDashboardRibbon_SurvivesReorderingPages guards the same file-rewrite
// trap against reorderDashboardPagesHandler specifically: unlike the other
// page handlers it writes the pages file directly rather than going through
// saveDashboardPagesLocked, so it is exactly the "other code path" that has
// to remember to carry the ribbon forward or silently drop it from disk.
func TestDashboardRibbon_SurvivesReorderingPages(t *testing.T) {
	setupDashboardPagesTest(t)
	first := createTestDashboardPage(t, "First", sampleDashboardWidgets())
	second := createTestDashboardPage(t, "Second", sampleDashboardWidgets())

	if rec, body := putDashboardRibbon(t, validLampStripConfig()); rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, body)
	}

	c, rec := newDashboardPagesRequest(t, http.MethodPut, "/api/dashboard-pages/order", map[string]any{
		"page_ids": []string{second.ID, first.ID},
	})
	if err := reorderDashboardPagesHandler(c); err != nil {
		t.Fatalf("reorderDashboardPagesHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	loadDashboardPages()

	dashboardPagesMu.RLock()
	ribbon := dashboardRibbonState
	dashboardPagesMu.RUnlock()
	if ribbon == nil || ribbon.Title != "Status" {
		t.Fatalf("expected the ribbon to survive reordering pages and a reload, got %+v", ribbon)
	}
}

// ── the snapshot refactor (multiple-wall-displays plan §1) ─────────────────
//
// Three call sites used to write dashboard-pages.json directly, each one
// more thing to remember to carry forward by hand. These two tests are the
// regression guard for collapsing that convention into
// dashboardPagesSnapshotLocked(): they fail before displaysState/displayData
// exist at all, and would fail again if a future direct writer forgot to
// start from the snapshot.

func TestReorderDashboardPages_CarriesDisplaysForward(t *testing.T) {
	setupDashboardPagesTest(t)
	first := createTestDashboardPage(t, "First", sampleDashboardWidgets())
	second := createTestDashboardPage(t, "Second", sampleDashboardWidgets())

	now := time.Now().UTC()
	dashboardPagesMu.Lock()
	displaysState["d1"] = &displayData{
		ID:        "d1",
		Name:      "Flybridge",
		Slug:      "flybridge",
		Width:     1920,
		Height:    360,
		Rotate:    180,
		CreatedAt: now,
		UpdatedAt: now,
	}
	dashboardPagesMu.Unlock()

	c, rec := newDashboardPagesRequest(t, http.MethodPut, "/api/dashboard-pages/order", map[string]any{
		"page_ids": []string{second.ID, first.ID},
	})
	if err := reorderDashboardPagesHandler(c); err != nil {
		t.Fatalf("reorderDashboardPagesHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	raw, err := os.ReadFile(dashboardPagesFilePath())
	if err != nil {
		t.Fatalf("failed to read pages file: %v", err)
	}
	var onDisk dashboardPagesFile
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("failed to parse pages file: %v", err)
	}
	if len(onDisk.Displays) != 1 || onDisk.Displays[0].Slug != "flybridge" {
		t.Fatalf("expected the display to survive reordering pages, got %+v", onDisk.Displays)
	}
}

func TestPutDashboardRibbon_CarriesDisplaysForward(t *testing.T) {
	setupDashboardPagesTest(t)

	now := time.Now().UTC()
	dashboardPagesMu.Lock()
	displaysState["d1"] = &displayData{
		ID:        "d1",
		Name:      "Flybridge",
		Slug:      "flybridge",
		Width:     1920,
		Height:    360,
		Rotate:    180,
		CreatedAt: now,
		UpdatedAt: now,
	}
	dashboardPagesMu.Unlock()

	if rec, body := putDashboardRibbon(t, validLampStripConfig()); rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, body)
	}

	raw, err := os.ReadFile(dashboardPagesFilePath())
	if err != nil {
		t.Fatalf("failed to read pages file: %v", err)
	}
	var onDisk dashboardPagesFile
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("failed to parse pages file: %v", err)
	}
	if len(onDisk.Displays) != 1 || onDisk.Displays[0].Slug != "flybridge" {
		t.Fatalf("expected the display to survive a ribbon PUT, got %+v", onDisk.Displays)
	}
}

// TestListDashboardPages_DoesNotCarryDisplays pins the wire/file split: the
// GET list response must never gain a "displays" key just because the file
// struct grew one, since dashboardPagesFile is the on-disk shape and
// dashboardPagesListResponse is what the handler actually serves.
func TestListDashboardPages_DoesNotCarryDisplays(t *testing.T) {
	setupDashboardPagesTest(t)
	createTestDashboardPage(t, "Test Page", sampleDashboardWidgets())

	now := time.Now().UTC()
	dashboardPagesMu.Lock()
	displaysState["d1"] = &displayData{ID: "d1", Name: "Flybridge", Slug: "flybridge", CreatedAt: now, UpdatedAt: now}
	dashboardPagesMu.Unlock()

	c, rec := newDashboardPagesRequest(t, http.MethodGet, "/api/dashboard-pages", nil)
	if err := listDashboardPagesHandler(c); err != nil {
		t.Fatalf("listDashboardPagesHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if strings.Contains(rec.Body.String(), `"displays"`) {
		t.Fatalf("expected the page list response to never carry displays, got %s", rec.Body.String())
	}
}

// ── page <-> display assignment (multiple-wall-displays plan §1, §3) ───────
//
// DisplayID replaces the old Kiosk bool: a page belongs to at most one
// display, named by id rather than flagged true/false. Dwell and condition
// keep their old validate-together shape (see validatePageDisplayFields),
// with the added rule that a non-empty DisplayID must name a display
// actually on record — the same fail-closed stance heroWidgetExists already
// takes for hero. Hero and display are mutually exclusive (plan §1): a
// patch that assigns a display clears Hero, and a patch that sets a
// non-empty Hero on a page that has a DisplayID is rejected.

// seedTestDisplay inserts a display straight into displaysState rather than
// going through the CRUD handler — displays.go's create/patch/delete
// handlers do not exist yet at this point in the plan's build order, and
// these page-assignment tests only need a display to exist, not to exercise
// how it got there.
func seedTestDisplay(t *testing.T, id, slug string) *displayData {
	t.Helper()
	now := time.Now().UTC()
	d := &displayData{ID: id, Name: slug, Slug: slug, CreatedAt: now, UpdatedAt: now}
	dashboardPagesMu.Lock()
	if displaysState == nil {
		displaysState = make(map[string]*displayData)
	}
	displaysState[id] = d
	dashboardPagesMu.Unlock()
	return d
}

func TestCreateDashboardPageHandler_RejectsUnknownDisplayID(t *testing.T) {
	setupDashboardPagesTest(t)

	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/dashboard-pages", map[string]any{
		"name":          "Wall: Engines",
		"widgets":       sampleDashboardWidgets(),
		"display_id":    "no-such-display",
		"dwell_seconds": 30,
	})
	if err := createDashboardPageHandler(c); err != nil {
		t.Fatalf("createDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestCreateDashboardPageHandler_DisplayRequiresDwell(t *testing.T) {
	setupDashboardPagesTest(t)
	display := seedTestDisplay(t, "d1", "flybridge")

	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/dashboard-pages", map[string]any{
		"name":       "Wall: Engines",
		"widgets":    sampleDashboardWidgets(),
		"display_id": display.ID,
	})
	if err := createDashboardPageHandler(c); err != nil {
		t.Fatalf("createDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestCreateDashboardPageHandler_CreateAcceptsDisplayAssignment(t *testing.T) {
	setupDashboardPagesTest(t)
	display := seedTestDisplay(t, "d1", "flybridge")

	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/dashboard-pages", map[string]any{
		"name":          "Wall: Engines",
		"widgets":       sampleDashboardWidgets(),
		"display_id":    display.ID,
		"dwell_seconds": 30,
		"show_when":     "anchored",
	})
	if err := createDashboardPageHandler(c); err != nil {
		t.Fatalf("createDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}
	var page dashboardPageData
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("failed to parse created page: %v", err)
	}
	if page.DisplayID != display.ID || page.DwellSeconds != 30 || page.ShowWhen != "anchored" {
		t.Fatalf("expected display assignment to round-trip on create, got display_id=%q dwell=%d when=%q", page.DisplayID, page.DwellSeconds, page.ShowWhen)
	}
}

func TestCreateDashboardPageHandler_RejectsHeroAndDisplayTogether(t *testing.T) {
	setupDashboardPagesTest(t)
	display := seedTestDisplay(t, "d1", "flybridge")

	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/dashboard-pages", map[string]any{
		"name":          "Wall: Engines",
		"widgets":       sampleDashboardWidgets(),
		"hero":          "wind",
		"display_id":    display.ID,
		"dwell_seconds": 30,
	})
	if err := createDashboardPageHandler(c); err != nil {
		t.Fatalf("createDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestPatchDashboardPageHandler_ClearingDisplayKeepsDwellAndCondition(t *testing.T) {
	setupDashboardPagesTest(t)
	display := seedTestDisplay(t, "d1", "flybridge")

	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/dashboard-pages", map[string]any{
		"name":          "Wall: Engines",
		"widgets":       sampleDashboardWidgets(),
		"display_id":    display.ID,
		"dwell_seconds": 45,
		"show_when":     "anchored",
	})
	if err := createDashboardPageHandler(c); err != nil {
		t.Fatalf("createDashboardPageHandler returned error: %v", err)
	}
	var page dashboardPageData
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("failed to parse created page: %v", err)
	}

	c2, rec2 := newDashboardPagesRequest(t, http.MethodPatch, "/api/dashboard-pages/"+page.ID, map[string]any{
		"display_id": "",
	})
	c2.SetParamNames("id")
	c2.SetParamValues(page.ID)
	if err := patchDashboardPageHandler(c2); err != nil {
		t.Fatalf("patchDashboardPageHandler returned error: %v", err)
	}
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec2.Code, rec2.Body.String())
	}
	var updated dashboardPageData
	if err := json.Unmarshal(rec2.Body.Bytes(), &updated); err != nil {
		t.Fatalf("failed to parse patch response: %v", err)
	}
	if updated.DisplayID != "" {
		t.Fatalf("expected display_id to clear, got %q", updated.DisplayID)
	}
	if updated.DwellSeconds != 45 || updated.ShowWhen != "anchored" {
		t.Fatalf("expected clearing the display to leave dwell and condition alone, got dwell=%d when=%q", updated.DwellSeconds, updated.ShowWhen)
	}
}

func TestPatchDashboardPageHandler_AssigningDisplayClearsHero(t *testing.T) {
	setupDashboardPagesTest(t)
	display := seedTestDisplay(t, "d1", "flybridge")
	page := createTestDashboardPage(t, "Test Page", sampleDashboardWidgets())

	c, rec := newDashboardPagesRequest(t, http.MethodPatch, "/api/dashboard-pages/"+page.ID, map[string]any{
		"hero": "wind",
	})
	c.SetParamNames("id")
	c.SetParamValues(page.ID)
	if err := patchDashboardPageHandler(c); err != nil {
		t.Fatalf("patchDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	c2, rec2 := newDashboardPagesRequest(t, http.MethodPatch, "/api/dashboard-pages/"+page.ID, map[string]any{
		"display_id":    display.ID,
		"dwell_seconds": 30,
	})
	c2.SetParamNames("id")
	c2.SetParamValues(page.ID)
	if err := patchDashboardPageHandler(c2); err != nil {
		t.Fatalf("patchDashboardPageHandler returned error: %v", err)
	}
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec2.Code, rec2.Body.String())
	}
	var updated dashboardPageData
	if err := json.Unmarshal(rec2.Body.Bytes(), &updated); err != nil {
		t.Fatalf("failed to parse patch response: %v", err)
	}
	if updated.Hero != "" {
		t.Fatalf("expected assigning a display to clear hero, got %q", updated.Hero)
	}
	if updated.DisplayID != display.ID {
		t.Fatalf("expected display_id to be set, got %q", updated.DisplayID)
	}
}

func TestPatchDashboardPageHandler_RejectsHeroOnDisplayPage(t *testing.T) {
	setupDashboardPagesTest(t)
	display := seedTestDisplay(t, "d1", "flybridge")

	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/dashboard-pages", map[string]any{
		"name":          "Wall: Engines",
		"widgets":       sampleDashboardWidgets(),
		"display_id":    display.ID,
		"dwell_seconds": 30,
	})
	if err := createDashboardPageHandler(c); err != nil {
		t.Fatalf("createDashboardPageHandler returned error: %v", err)
	}
	var page dashboardPageData
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("failed to parse created page: %v", err)
	}

	c2, rec2 := newDashboardPagesRequest(t, http.MethodPatch, "/api/dashboard-pages/"+page.ID, map[string]any{
		"hero": "wind",
	})
	c2.SetParamNames("id")
	c2.SetParamValues(page.ID)
	if err := patchDashboardPageHandler(c2); err != nil {
		t.Fatalf("patchDashboardPageHandler returned error: %v", err)
	}
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec2.Code, rec2.Body.String())
	}
}

// The walker's fourth widget kind to learn (ADR 0052, 0049, and twice for
// clusters), and the first that is not itself a widget: the ribbon is
// vessel-level, so it has no widget id and no page, but its lamps are bound
// paths exactly the same way.
func TestGaugeBoundPathsIncludesRibbonLampsDeduplicatedAgainstAPageLamp(t *testing.T) {
	dashboardPagesMu.Lock()
	previousPages := dashboardPagesState
	previousRibbon := dashboardRibbonState
	dashboardPagesState = map[string]*dashboardPageData{
		"a": {ID: "a", Widgets: []dashboardLayoutItem{
			lampStripWidget("lamps:aaaa1111", &dashboardLampStripConfig{
				Title: "Status",
				Lamps: []dashboardLamp{{Path: "electrical.generator.state", Label: "GEN"}},
			}),
		}},
	}
	dashboardRibbonState = &dashboardLampStripConfig{
		Title: "Ribbon",
		Lamps: []dashboardLamp{
			// Shared with the page lamp above: one subscription, not two.
			{Path: "electrical.generator.state", Label: "GEN"},
			{Path: "tanks.fuel.2.currentLevel", Label: "FUEL"},
		},
	}
	dashboardPagesMu.Unlock()
	t.Cleanup(func() {
		dashboardPagesMu.Lock()
		dashboardPagesState = previousPages
		dashboardRibbonState = previousRibbon
		dashboardPagesMu.Unlock()
	})

	paths := gaugeBoundPaths()
	want := []string{"electrical.generator.state", "tanks.fuel.2.currentLevel"}
	if len(paths) != len(want) || paths[0] != want[0] || paths[1] != want[1] {
		t.Fatalf("expected %v, got %v", want, paths)
	}
}

// ── engine cluster widget (ADR 0054) ─────────────────────────────────────────

func clusterWidget(id string, config *dashboardClusterConfig) dashboardLayoutItem {
	return dashboardLayoutItem{ID: id, X: 0, Y: 0, W: 6, H: 10, Cluster: config}
}

func validClusterConfig() *dashboardClusterConfig {
	return &dashboardClusterConfig{
		Title:  "Port",
		Ring:   dashboardGaugeConfig{Path: "propulsion.port.revolutions", Label: "RPM", Display: "radial", Quantity: "frequency", Unit: "rpm"},
		Centre: dashboardGaugeConfig{Path: "propulsion.port.fuel.rate", Label: "Fuel", Display: "numeric", Quantity: "volumetricFlow", Unit: "Lph"},
		Corners: []dashboardClusterCorner{
			{Label: "Oil", Rows: []dashboardGaugeConfig{
				{Path: "propulsion.port.oilPressure", Label: "Oil", Display: "numeric", Quantity: "pressure", Unit: "psi"},
			}},
			{Label: "Temps", Rows: []dashboardGaugeConfig{
				{Path: "propulsion.port.temperature", Label: "Coolant", Display: "numeric", Quantity: "temperature", Unit: "C"},
				{Path: "propulsion.0.exhaustTemperature", Label: "Exhaust", Display: "numeric", Quantity: "temperature", Unit: "C"},
			}},
		},
	}
}

func TestValidateClusterAcceptsAWellFormedCluster(t *testing.T) {
	if msg := validateDashboardWidgets([]dashboardLayoutItem{clusterWidget("cluster:abcd1234", validClusterConfig())}, ""); msg != "" {
		t.Fatalf("expected a valid cluster to be accepted, got %q", msg)
	}
}

func TestValidateClusterRejectsBadInput(t *testing.T) {
	noRing := validClusterConfig()
	noRing.Ring = dashboardGaugeConfig{}

	tooManyCorners := validClusterConfig()
	tooManyCorners.Corners = make([]dashboardClusterCorner, clusterMaxCorners+1)
	for i := range tooManyCorners.Corners {
		tooManyCorners.Corners[i] = dashboardClusterCorner{Label: "X", Rows: []dashboardGaugeConfig{
			{Path: "a.b", Display: "numeric", Quantity: "raw", Unit: "raw"},
		}}
	}

	emptyCorner := validClusterConfig()
	emptyCorner.Corners = []dashboardClusterCorner{{Label: "Empty"}}

	badRow := validClusterConfig()
	badRow.Corners[0].Rows[0].Display = "hologram"

	badTelltale := validClusterConfig()
	badTelltale.Telltales = []dashboardGaugeConfig{{Path: "a.b", Display: "hologram", Quantity: "raw", Unit: "raw"}}

	tooManyTelltales := validClusterConfig()
	tooManyTelltales.Telltales = make([]dashboardGaugeConfig, clusterMaxTelltales+1)
	for i := range tooManyTelltales.Telltales {
		tooManyTelltales.Telltales[i] = dashboardGaugeConfig{Path: "a.b", Display: "numeric", Quantity: "raw", Unit: "raw"}
	}

	cases := []struct {
		name   string
		widget dashboardLayoutItem
	}{
		{"short token", clusterWidget("cluster:abc", validClusterConfig())},
		{"missing config", clusterWidget("cluster:abcd1234", nil)},
		{"ring with no path", clusterWidget("cluster:abcd1234", noRing)},
		{"too many corners", clusterWidget("cluster:abcd1234", tooManyCorners)},
		{"corner with no rows", clusterWidget("cluster:abcd1234", emptyCorner)},
		{"bad row", clusterWidget("cluster:abcd1234", badRow)},
		{"bad telltale", clusterWidget("cluster:abcd1234", badTelltale)},
		{"too many telltales", clusterWidget("cluster:abcd1234", tooManyTelltales)},
	}

	for _, tc := range cases {
		if msg := validateDashboardWidgets([]dashboardLayoutItem{tc.widget}, ""); msg == "" {
			t.Fatalf("%s: expected rejection", tc.name)
		}
	}
}

func TestGaugeBoundPathsIncludesClusterTelltales(t *testing.T) {
	cluster := validClusterConfig()
	cluster.Telltales = []dashboardGaugeConfig{
		{Path: "propulsion.port.temperature", Display: "numeric", Quantity: "temperature", Unit: "C"},
	}

	dashboardPagesMu.Lock()
	previous := dashboardPagesState
	dashboardPagesState = map[string]*dashboardPageData{"p1": {
		ID: "p1", Name: "Telltales",
		Widgets: []dashboardLayoutItem{clusterWidget("cluster:abcd1234", cluster)},
	}}
	dashboardPagesMu.Unlock()
	t.Cleanup(func() {
		dashboardPagesMu.Lock()
		dashboardPagesState = previous
		dashboardPagesMu.Unlock()
	})

	var found bool
	for _, p := range gaugeBoundPaths() {
		if p == "propulsion.port.temperature" {
			found = true
		}
	}
	if !found {
		t.Fatalf("telltale path missing from gaugeBoundPaths: %v", gaugeBoundPaths())
	}
}

func TestValidateDashboardWidgetsRejectsMismatchedClusterConfig(t *testing.T) {
	cluster := validClusterConfig()
	cases := []struct {
		name   string
		widget dashboardLayoutItem
	}{
		{"cluster on a builtin", dashboardLayoutItem{ID: "wind", X: 0, Y: 0, W: 4, H: 4, Cluster: cluster}},
		{"cluster on a gauge", dashboardLayoutItem{ID: "gauge:abcd1234", X: 0, Y: 0, W: 4, H: 4, Gauge: validGaugeConfig(), Cluster: cluster}},
		{"gauge on a cluster", dashboardLayoutItem{ID: "cluster:abcd1234", X: 0, Y: 0, W: 4, H: 4, Gauge: validGaugeConfig(), Cluster: cluster}},
	}
	for _, tc := range cases {
		if msg := validateDashboardWidgets([]dashboardLayoutItem{tc.widget}, ""); msg == "" {
			t.Fatalf("%s: expected rejection", tc.name)
		}
	}
}

// Fifth widget kind to need this walker. Miss it and every reading in the
// cluster is a dash, with nothing in any log to say why.
func TestGaugeBoundPathsIncludesClusterSlots(t *testing.T) {
	dashboardPagesMu.Lock()
	previous := dashboardPagesState
	dashboardPagesState = map[string]*dashboardPageData{
		"a": {ID: "a", Widgets: []dashboardLayoutItem{clusterWidget("cluster:abcd1234", validClusterConfig())}},
	}
	dashboardPagesMu.Unlock()
	t.Cleanup(func() {
		dashboardPagesMu.Lock()
		dashboardPagesState = previous
		dashboardPagesMu.Unlock()
	})

	want := []string{
		"propulsion.0.exhaustTemperature",
		"propulsion.port.fuel.rate",
		"propulsion.port.oilPressure",
		"propulsion.port.revolutions",
		"propulsion.port.temperature",
	}
	paths := gaugeBoundPaths()
	if len(paths) != len(want) {
		t.Fatalf("expected ring, centre and every corner row: %v, got %v", want, paths)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, paths)
		}
	}
}

// Icons are an allowlist mirroring CLUSTER_ICONS in the frontend, so a saved
// config can never name a component that does not exist.
func TestValidateClusterRejectsUnknownIcons(t *testing.T) {
	badCorner := validClusterConfig()
	badCorner.Corners[0].Icon = "aubergine"

	badCentre := validClusterConfig()
	badCentre.CentreIcon = "aubergine"

	for name, config := range map[string]*dashboardClusterConfig{
		"corner icon": badCorner,
		"centre icon": badCentre,
	} {
		if msg := validateDashboardWidgets([]dashboardLayoutItem{clusterWidget("cluster:abcd1234", config)}, ""); msg == "" {
			t.Fatalf("%s: expected rejection", name)
		}
	}

	// Absent is fine: the renderer infers one from what the slot measures.
	ok := validClusterConfig()
	ok.Corners[0].Icon = "cog"
	ok.CentreIcon = ""
	if msg := validateDashboardWidgets([]dashboardLayoutItem{clusterWidget("cluster:abcd1234", ok)}, ""); msg != "" {
		t.Fatalf("expected a known icon and an absent one to be accepted, got %q", msg)
	}
}

/*
Go's JSON decoder drops unknown keys silently, so a field missing from
dashboardGaugeConfig is discarded on every save with no error anywhere. Four
were: ringStyle, readout and labelDivisor (ADR 0054) and window (ADR 0051),
which meant a trend gauge always fell back to its default window and an
instrument ring never got its divided scale.
*/
func TestGaugeConfigRoundTripsEveryRenderedField(t *testing.T) {
	setupDashboardPagesTest(t)

	raw := `{
	  "path": "propulsion.port.revolutions", "label": "RPM", "display": "radial",
	  "quantity": "frequency", "unit": "rpm", "decimals": 0,
	  "min": 0, "max": 3000,
	  "ringStyle": "instrument", "readout": "inside", "labelDivisor": 100,
	  "window": "24h",
	  "zones": [{"from": 2600, "to": 3000, "state": "warn"}]
	}`
	var gauge dashboardGaugeConfig
	if err := json.Unmarshal([]byte(raw), &gauge); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	page := createTestDashboardPage(t, "Engines", []dashboardLayoutItem{
		gaugeWidget("gauge:abcd1234", &gauge),
	})
	loadDashboardPages()

	dashboardPagesMu.RLock()
	reloaded := dashboardPagesState[page.ID]
	dashboardPagesMu.RUnlock()

	got := reloaded.Widgets[0].Gauge
	if got.RingStyle != "instrument" {
		t.Errorf("ringStyle did not survive: %q", got.RingStyle)
	}
	if got.Readout != "inside" {
		t.Errorf("readout did not survive: %q", got.Readout)
	}
	if got.LabelDivisor == nil || *got.LabelDivisor != 100 {
		t.Errorf("labelDivisor did not survive: %v", got.LabelDivisor)
	}
	if got.Window != "24h" {
		t.Errorf("window did not survive: %q", got.Window)
	}
}

func TestValidateGaugeConfigRejectsUnknownRingOptions(t *testing.T) {
	for name, mutate := range map[string]func(*dashboardGaugeConfig){
		"ring style": func(g *dashboardGaugeConfig) { g.RingStyle = "neon" },
		"readout":    func(g *dashboardGaugeConfig) { g.Readout = "sideways" },
		"window":     func(g *dashboardGaugeConfig) { g.Window = "99y" },
	} {
		config := validGaugeConfig()
		mutate(config)
		if msg := validateDashboardWidgets([]dashboardLayoutItem{gaugeWidget("gauge:abcd1234", config)}, ""); msg == "" {
			t.Fatalf("%s: expected rejection", name)
		}
	}
}

// A divisor of zero would divide the scale labels by nothing.
func TestValidateGaugeConfigRejectsANonPositiveLabelDivisor(t *testing.T) {
	config := validGaugeConfig()
	zero := 0.0
	config.LabelDivisor = &zero
	if msg := validateDashboardWidgets([]dashboardLayoutItem{gaugeWidget("gauge:abcd1234", config)}, ""); msg == "" {
		t.Fatal("expected a zero label divisor to be rejected")
	}
}

// --- Fuel rail (ADR 0061) ---------------------------------------------------

// The live vessel's port side: forward and aft, each with a capacity path
// alongside its level. Captured from the running server rather than assumed,
// which is how ADR 0054 found that three of its own suffixes matched nothing.
func validFuelRail() *dashboardClusterFuelRail {
	return &dashboardClusterFuelRail{
		Side: "left",
		Bars: []dashboardClusterFuelBar{
			{
				Level:    dashboardGaugeConfig{Path: "tanks.fuel.5.currentLevel", Label: "Fwd", Display: "bar", Quantity: "ratio", Unit: "percent"},
				Capacity: dashboardGaugeConfig{Path: "tanks.fuel.5.capacity", Display: "numeric", Quantity: "volume", Unit: "L"},
			},
			{
				Level:    dashboardGaugeConfig{Path: "tanks.fuel.4.currentLevel", Label: "Aft", Display: "bar", Quantity: "ratio", Unit: "percent"},
				Capacity: dashboardGaugeConfig{Path: "tanks.fuel.4.capacity", Display: "numeric", Quantity: "volume", Unit: "L"},
			},
		},
	}
}

func clusterWithFuel() *dashboardClusterConfig {
	config := validClusterConfig()
	config.Fuel = validFuelRail()
	return config
}

func TestValidateClusterAcceptsAFuelRail(t *testing.T) {
	if msg := validateDashboardWidgets([]dashboardLayoutItem{clusterWidget("cluster:abcd1234", clusterWithFuel())}, ""); msg != "" {
		t.Fatalf("expected the live vessel's fuel rail to be accepted, got %q", msg)
	}
}

/*
The rule that matters most here.

No tank path on this vessel publishes meta.units, so the path picker preselects
Unitless. Left that way convertFromSI is the identity and the total reads 1.2
where it should read 890. A fuel figure that is wrong but plausible is worse on
a helm than no figure at all, so the config that produces it cannot be saved.
*/
func TestValidateClusterRejectsFuelSlotsWithTheWrongQuantity(t *testing.T) {
	rawLevel := clusterWithFuel()
	rawLevel.Fuel.Bars[0].Level.Quantity = "raw"
	rawLevel.Fuel.Bars[0].Level.Unit = "raw"

	rawCapacity := clusterWithFuel()
	rawCapacity.Fuel.Bars[0].Capacity.Quantity = "raw"
	rawCapacity.Fuel.Bars[0].Capacity.Unit = "raw"

	pressureCapacity := clusterWithFuel()
	pressureCapacity.Fuel.Bars[0].Capacity.Quantity = "pressure"
	pressureCapacity.Fuel.Bars[0].Capacity.Unit = "psi"

	for _, tc := range []struct {
		name   string
		config *dashboardClusterConfig
	}{
		{"level not a ratio", rawLevel},
		{"capacity not a volume", rawCapacity},
		{"capacity measured as pressure", pressureCapacity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if msg := validateDashboardWidgets([]dashboardLayoutItem{clusterWidget("cluster:abcd1234", tc.config)}, ""); msg == "" {
				t.Fatal("expected a fuel slot with the wrong quantity to be rejected")
			}
		})
	}
}

func TestValidateClusterRejectsBadFuelRails(t *testing.T) {
	badSide := clusterWithFuel()
	badSide.Fuel.Side = "port"

	noBars := clusterWithFuel()
	noBars.Fuel.Bars = nil

	tooManyBars := clusterWithFuel()
	for len(tooManyBars.Fuel.Bars) <= clusterMaxFuelBars {
		tooManyBars.Fuel.Bars = append(tooManyBars.Fuel.Bars, validFuelRail().Bars[0])
	}

	noCapacityPath := clusterWithFuel()
	noCapacityPath.Fuel.Bars[1].Capacity.Path = ""

	for _, tc := range []struct {
		name   string
		config *dashboardClusterConfig
	}{
		// "port" names the tanks, not the edge the rail sits on. A starboard
		// cluster laid out on the left of a page wants a left-hand rail.
		{"side names a tank rather than an edge", badSide},
		{"no bars at all", noBars},
		{"too many bars", tooManyBars},
		{"capacity with no path", noCapacityPath},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if msg := validateDashboardWidgets([]dashboardLayoutItem{clusterWidget("cluster:abcd1234", tc.config)}, ""); msg == "" {
				t.Fatal("expected the rail to be rejected")
			}
		})
	}
}

/*
Both paths per bar, not just the level.

Missing the capacity path is the subtler of the two failures: the bars still
draw and only the litres and the total are dashes, which reads as a units
problem rather than a subscription one.
*/
func TestGaugeBoundPathsIncludesFuelLevelsAndCapacities(t *testing.T) {
	dashboardPagesMu.Lock()
	previous := dashboardPagesState
	dashboardPagesState = map[string]*dashboardPageData{
		"a": {ID: "a", Widgets: []dashboardLayoutItem{clusterWidget("cluster:abcd1234", clusterWithFuel())}},
	}
	dashboardPagesMu.Unlock()
	t.Cleanup(func() {
		dashboardPagesMu.Lock()
		dashboardPagesState = previous
		dashboardPagesMu.Unlock()
	})

	paths := gaugeBoundPaths()
	for _, want := range []string{
		"tanks.fuel.4.capacity",
		"tanks.fuel.4.currentLevel",
		"tanks.fuel.5.capacity",
		"tanks.fuel.5.currentLevel",
	} {
		found := false
		for _, p := range paths {
			if p == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected %q among the bound paths, got %v", want, paths)
		}
	}
}

/*
The test that catches a field missing from the Go struct.

Go's decoder drops unknown keys without complaint, so a field the renderer reads
but dashboardClusterFuelRail does not declare is discarded on every save with no
error anywhere. The widget then renders its default and looks like a frontend
bug. Four fields on dashboardGaugeConfig were lost exactly this way before
anyone noticed.
*/
func TestClusterFuelRailRoundTripsEveryRenderedField(t *testing.T) {
	setupDashboardPagesTest(t)

	raw := `{
	  "side": "right",
	  "totalLabel": "Aboard",
	  "bars": [{
	    "level": {
	      "path": "tanks.fuel.2.currentLevel", "label": "Fwd", "display": "bar",
	      "quantity": "ratio", "unit": "percent", "decimals": 0, "min": 0, "max": 100,
	      "zones": [{"from": 0, "to": 15, "state": "warn"}]
	    },
	    "capacity": {
	      "path": "tanks.fuel.2.capacity", "display": "numeric",
	      "quantity": "volume", "unit": "L"
	    }
	  }]
	}`
	var rail dashboardClusterFuelRail
	if err := json.Unmarshal([]byte(raw), &rail); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	config := validClusterConfig()
	config.Fuel = &rail
	page := createTestDashboardPage(t, "Engines", []dashboardLayoutItem{
		clusterWidget("cluster:abcd1234", config),
	})
	loadDashboardPages()

	dashboardPagesMu.RLock()
	reloaded := dashboardPagesState[page.ID]
	dashboardPagesMu.RUnlock()

	got := reloaded.Widgets[0].Cluster.Fuel
	if got == nil {
		t.Fatal("the whole fuel rail was discarded on save")
	}
	if got.Side != "right" {
		t.Errorf("side did not survive: %q", got.Side)
	}
	if got.TotalLabel != "Aboard" {
		t.Errorf("totalLabel did not survive: %q", got.TotalLabel)
	}
	if len(got.Bars) != 1 {
		t.Fatalf("expected one bar, got %d", len(got.Bars))
	}
	if got.Bars[0].Level.Path != "tanks.fuel.2.currentLevel" {
		t.Errorf("level path did not survive: %q", got.Bars[0].Level.Path)
	}
	// The one most likely to be forgotten, and the one whose absence looks
	// like a units bug rather than a persistence bug.
	if got.Bars[0].Capacity.Path != "tanks.fuel.2.capacity" {
		t.Errorf("capacity path did not survive: %q", got.Bars[0].Capacity.Path)
	}
	if got.Bars[0].Capacity.Quantity != "volume" || got.Bars[0].Capacity.Unit != "L" {
		t.Errorf("capacity units did not survive: %q/%q", got.Bars[0].Capacity.Quantity, got.Bars[0].Capacity.Unit)
	}
	if len(got.Bars[0].Level.Zones) != 1 {
		t.Errorf("level zones did not survive: %v", got.Bars[0].Level.Zones)
	}
}

// ── display assignment fields (ADR 0089, superseded in part by ADR 0110) ───
//
// A page belongs to at most one wall display, named by display_id rather
// than a bool flag. They follow the same validate-fail-closed pattern ADR
// 0060 established for skin and ADR 0072 established for hero, with the
// added wrinkle that the three fields are validated together (see
// validatePageDisplayFields and its call sites). RejectsUnknownDisplayID,
// DisplayRequiresDwell, ClearingDisplayKeepsDwellAndCondition,
// AssigningDisplayClearsHero, RejectsHeroOnDisplayPage and
// CreateAcceptsDisplayAssignment live earlier in this file, next to
// seedTestDisplay.

func TestDashboardPages_DisplayAssignmentRoundTripsThroughPostAndGet(t *testing.T) {
	setupDashboardPagesTest(t)
	display := seedTestDisplay(t, "d1", "flybridge")

	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/dashboard-pages", map[string]any{
		"name":          "Wall: Engines",
		"widgets":       sampleDashboardWidgets(),
		"display_id":    display.ID,
		"dwell_seconds": 30,
		"show_when":     "anchored",
	})
	if err := createDashboardPageHandler(c); err != nil {
		t.Fatalf("createDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}
	var page dashboardPageData
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("failed to parse created page: %v", err)
	}
	if page.DisplayID != display.ID || page.DwellSeconds != 30 || page.ShowWhen != "anchored" {
		t.Fatalf("expected display fields to round-trip on create, got display_id=%q dwell=%d when=%q", page.DisplayID, page.DwellSeconds, page.ShowWhen)
	}

	c2, rec2 := newDashboardPagesRequest(t, http.MethodGet, "/api/dashboard-pages/"+page.ID, nil)
	c2.SetParamNames("id")
	c2.SetParamValues(page.ID)
	if err := getDashboardPageHandler(c2); err != nil {
		t.Fatalf("getDashboardPageHandler returned error: %v", err)
	}
	var fetched dashboardPageData
	if err := json.Unmarshal(rec2.Body.Bytes(), &fetched); err != nil {
		t.Fatalf("failed to parse get response: %v", err)
	}
	if fetched.DisplayID != display.ID || fetched.DwellSeconds != 30 || fetched.ShowWhen != "anchored" {
		t.Fatalf("expected display fields to round-trip through GET, got display_id=%q dwell=%d when=%q", fetched.DisplayID, fetched.DwellSeconds, fetched.ShowWhen)
	}
}

func TestCreateDashboardPageHandler_RejectsDwellSecondsOutOfRange(t *testing.T) {
	setupDashboardPagesTest(t)
	display := seedTestDisplay(t, "d1", "flybridge")

	for _, seconds := range []int{4, 3601} {
		c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/dashboard-pages", map[string]any{
			"name":          "Bad Duration",
			"widgets":       sampleDashboardWidgets(),
			"display_id":    display.ID,
			"dwell_seconds": seconds,
		})
		if err := createDashboardPageHandler(c); err != nil {
			t.Fatalf("createDashboardPageHandler returned error: %v", err)
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("dwell_seconds=%d: expected status %d, got %d: %s", seconds, http.StatusBadRequest, rec.Code, rec.Body.String())
		}
	}
}

func TestCreateDashboardPageHandler_RejectsUnknownShowWhen(t *testing.T) {
	setupDashboardPagesTest(t)
	display := seedTestDisplay(t, "d1", "flybridge")

	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/dashboard-pages", map[string]any{
		"name":          "Bad Condition",
		"widgets":       sampleDashboardWidgets(),
		"display_id":    display.ID,
		"dwell_seconds": 30,
		"show_when":     "underway",
	})
	if err := createDashboardPageHandler(c); err != nil {
		t.Fatalf("createDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestCreateDashboardPageHandler_AcceptsAutoStateShowWhen(t *testing.T) {
	setupDashboardPagesTest(t)
	display := seedTestDisplay(t, "d1", "flybridge")

	for _, when := range []string{"motoring", "sailing", "moored"} {
		c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/dashboard-pages", map[string]any{
			"name":          "Wall: " + when,
			"widgets":       sampleDashboardWidgets(),
			"display_id":    display.ID,
			"dwell_seconds": 30,
			"show_when":     when,
		})
		if err := createDashboardPageHandler(c); err != nil {
			t.Fatalf("createDashboardPageHandler returned error: %v", err)
		}
		if rec.Code != http.StatusCreated {
			t.Fatalf("show_when=%q: expected status %d, got %d: %s", when, http.StatusCreated, rec.Code, rec.Body.String())
		}
	}
}

func TestPatchDashboardPageHandler_DisplayOnlyPatchSucceedsAndReusesStoredDwell(t *testing.T) {
	setupDashboardPagesTest(t)
	display := seedTestDisplay(t, "d1", "flybridge")

	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/dashboard-pages", map[string]any{
		"name":          "Cluster preview",
		"widgets":       sampleDashboardWidgets(),
		"display_id":    display.ID,
		"dwell_seconds": 45,
	})
	if err := createDashboardPageHandler(c); err != nil {
		t.Fatalf("createDashboardPageHandler returned error: %v", err)
	}
	var page dashboardPageData
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("failed to parse created page: %v", err)
	}

	// Clear, then re-assign with no dwell in the body at all - a
	// display-only patch has neither name, skin, hero nor widgets, so it
	// must not trip the "no patch fields provided" guard, and it must reuse
	// the 45s already on record rather than requiring the caller to resend
	// it.
	c1, rec1 := newDashboardPagesRequest(t, http.MethodPatch, "/api/dashboard-pages/"+page.ID, map[string]any{
		"display_id": "",
	})
	c1.SetParamNames("id")
	c1.SetParamValues(page.ID)
	if err := patchDashboardPageHandler(c1); err != nil {
		t.Fatalf("patchDashboardPageHandler returned error: %v", err)
	}
	if rec1.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec1.Code, rec1.Body.String())
	}

	c2, rec2 := newDashboardPagesRequest(t, http.MethodPatch, "/api/dashboard-pages/"+page.ID, map[string]any{
		"display_id": display.ID,
	})
	c2.SetParamNames("id")
	c2.SetParamValues(page.ID)
	if err := patchDashboardPageHandler(c2); err != nil {
		t.Fatalf("patchDashboardPageHandler returned error: %v", err)
	}
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec2.Code, rec2.Body.String())
	}
	var updated dashboardPageData
	if err := json.Unmarshal(rec2.Body.Bytes(), &updated); err != nil {
		t.Fatalf("failed to parse patch response: %v", err)
	}
	if updated.DisplayID != display.ID || updated.DwellSeconds != 45 {
		t.Fatalf("expected re-assignment to reuse the stored 45s dwell, got display_id=%q dwell=%d", updated.DisplayID, updated.DwellSeconds)
	}
}

func TestPatchDashboardPageHandler_RejectsDisplayWithNoStoredDwell(t *testing.T) {
	setupDashboardPagesTest(t)
	display := seedTestDisplay(t, "d1", "flybridge")
	page := createTestDashboardPage(t, "Test Page", sampleDashboardWidgets())

	c, rec := newDashboardPagesRequest(t, http.MethodPatch, "/api/dashboard-pages/"+page.ID, map[string]any{
		"display_id": display.ID,
	})
	c.SetParamNames("id")
	c.SetParamValues(page.ID)
	if err := patchDashboardPageHandler(c); err != nil {
		t.Fatalf("patchDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestPatchDashboardPageHandler_WidgetsOnlyPatchPreservesDisplayFields(t *testing.T) {
	setupDashboardPagesTest(t)
	display := seedTestDisplay(t, "d1", "flybridge")

	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/dashboard-pages", map[string]any{
		"name":          "Cluster preview",
		"widgets":       sampleDashboardWidgets(),
		"display_id":    display.ID,
		"dwell_seconds": 30,
		"show_when":     "anchored",
	})
	if err := createDashboardPageHandler(c); err != nil {
		t.Fatalf("createDashboardPageHandler returned error: %v", err)
	}
	var page dashboardPageData
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("failed to parse created page: %v", err)
	}

	c2, rec2 := newDashboardPagesRequest(t, http.MethodPatch, "/api/dashboard-pages/"+page.ID, map[string]any{
		"widgets": sampleDashboardWidgets(),
	})
	c2.SetParamNames("id")
	c2.SetParamValues(page.ID)
	if err := patchDashboardPageHandler(c2); err != nil {
		t.Fatalf("patchDashboardPageHandler returned error: %v", err)
	}
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec2.Code, rec2.Body.String())
	}
	var updated dashboardPageData
	if err := json.Unmarshal(rec2.Body.Bytes(), &updated); err != nil {
		t.Fatalf("failed to parse patch response: %v", err)
	}
	if updated.DisplayID != display.ID || updated.DwellSeconds != 30 || updated.ShowWhen != "anchored" {
		t.Fatalf("expected display fields to survive a widgets-only patch, got display_id=%q dwell=%d when=%q", updated.DisplayID, updated.DwellSeconds, updated.ShowWhen)
	}
}

func TestPatchDashboardPageHandler_AcceptsAutoStateShowWhen(t *testing.T) {
	setupDashboardPagesTest(t)
	display := seedTestDisplay(t, "d1", "flybridge")
	page := createTestDashboardPage(t, "Cluster preview", sampleDashboardWidgets())

	for _, when := range []string{"motoring", "sailing", "moored"} {
		c, rec := newDashboardPagesRequest(t, http.MethodPatch, "/api/dashboard-pages/"+page.ID, map[string]any{
			"display_id":    display.ID,
			"dwell_seconds": 30,
			"show_when":     when,
		})
		c.SetParamNames("id")
		c.SetParamValues(page.ID)
		if err := patchDashboardPageHandler(c); err != nil {
			t.Fatalf("patchDashboardPageHandler returned error: %v", err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("show_when=%q: expected status %d, got %d: %s", when, http.StatusOK, rec.Code, rec.Body.String())
		}
	}
}

// omitempty keeps existing dashboard-pages.json files byte-identical: a page
// with no display must not gain "display_id"/"dwell_seconds"/"show_when"
// keys just because the struct grew them, mirroring
// TestDashboardLayoutItem_OmitsEmbedKeyWhenAbsent.
func TestDashboardPageData_OmitsDisplayKeysWhenUnset(t *testing.T) {
	encoded, err := json.Marshal(dashboardPageData{ID: "p1", Name: "Anchored", Widgets: []dashboardLayoutItem{}})
	if err != nil {
		t.Fatalf("failed to marshal page: %v", err)
	}
	for _, key := range []string{"display_id", "dwell_seconds", "show_when"} {
		if strings.Contains(string(encoded), `"`+key+`"`) {
			t.Fatalf("expected no %q key for a page with no display, got %s", key, encoded)
		}
	}
}

// TestEmbedWidgetRejectsSameOriginURL covers F-1 from the 2026-09-19 security
// audit. embed-tile.tsx renders the embed in an iframe carrying
// allow-scripts + allow-same-origin. That pair is fine for a third-party
// embed - the frame keeps its own origin, so the grant buys it nothing
// against us - but a frame whose URL is OUR origin is then not sandboxed at
// all: it reaches window.top.document, the app's state, and same-origin
// fetches carrying the session cookie, which on this deployment includes
// autopilot and generator control.
//
// The frontend validator rejects these, but a validator that only runs in the
// config dialog is bypassed by posting to the API directly, so the check has
// to exist here too.
func TestEmbedWidgetRejectsSameOriginURL(t *testing.T) {
	widget := dashboardLayoutItem{
		ID:    embedWidgetIDPrefix + "abcd1234",
		X:     0,
		Y:     0,
		W:     4,
		H:     4,
		Embed: &dashboardEmbedConfig{URL: "http://helm.local:9091/anything"},
	}

	if msg := validateDashboardWidgets([]dashboardLayoutItem{widget}, "http://helm.local:9091"); msg == "" {
		t.Fatal("expected a same-origin embed url to be rejected, got no error")
	}

	// A genuinely different origin is the supported case (Grafana, Windy) and
	// must still pass.
	widget.Embed.URL = "https://grafana.example/d/abc"
	if msg := validateDashboardWidgets([]dashboardLayoutItem{widget}, "http://helm.local:9091"); msg != "" {
		t.Fatalf("expected a cross-origin embed url to be accepted, got %q", msg)
	}

	// Same host, different port is a different origin and stays allowed.
	widget.Embed.URL = "http://helm.local:3000/panel"
	if msg := validateDashboardWidgets([]dashboardLayoutItem{widget}, "http://helm.local:9091"); msg != "" {
		t.Fatalf("expected a different-port url to be accepted, got %q", msg)
	}

	// Proves the rejection above is the origin comparison and not some other
	// rule the fixture trips: the identical URL passes when the caller could
	// not determine an own-origin to compare against.
	widget.Embed.URL = "http://helm.local:9091/anything"
	if msg := validateDashboardWidgets([]dashboardLayoutItem{widget}, ""); msg != "" {
		t.Fatalf("expected the same url to pass with no own-origin, got %q", msg)
	}
}
