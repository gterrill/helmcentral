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
	if msg := validateDashboardWidgets(widgets); msg != "" {
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
	if msg := validateDashboardWidgets(widgets); msg != "" {
		t.Fatalf("expected two distinct embeds to validate, got %q", msg)
	}
}

func TestValidateDashboardWidgets_RejectsDuplicateEmbedInstance(t *testing.T) {
	widgets := []dashboardLayoutItem{
		embedWidget("m1x8abcd", "Windrose", "https://grafana.local/d-solo/a?panelId=1"),
		embedWidget("m1x8abcd", "Windrose Again", "https://grafana.local/d-solo/a?panelId=2"),
	}
	if msg := validateDashboardWidgets(widgets); msg == "" {
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
			if msg := validateDashboardWidgets([]dashboardLayoutItem{tc.widget}); msg == "" {
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
	if msg := validateDashboardWidgets(widgets); msg == "" {
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
	if msg := validateDashboardWidgets(widgets); msg != "" {
		t.Fatalf("expected the autopilot builtin id to be accepted, got %q", msg)
	}
}

func TestValidateDashboardWidgets_RejectsEmbedConfigOnAutopilotWidget(t *testing.T) {
	widgets := []dashboardLayoutItem{
		{ID: "autopilot", X: 0, Y: 0, W: 4, H: 6, Embed: &dashboardEmbedConfig{URL: "https://grafana.local/a"}},
	}
	if msg := validateDashboardWidgets(widgets); msg == "" {
		t.Fatal("expected embed config on the autopilot widget to be rejected")
	}
}

func TestValidateDashboardWidgets_RejectsGaugeConfigOnAutopilotWidget(t *testing.T) {
	widget := dashboardLayoutItem{ID: "autopilot", X: 0, Y: 0, W: 4, H: 6, Gauge: validGaugeConfig()}
	if msg := validateDashboardWidgets([]dashboardLayoutItem{widget}); msg == "" {
		t.Fatal("expected gauge config on the autopilot widget to be rejected")
	}
}

func TestValidateGaugeWidgetAcceptsAWellFormedGauge(t *testing.T) {
	if msg := validateDashboardWidgets([]dashboardLayoutItem{gaugeWidget("gauge:abcd1234", validGaugeConfig())}); msg != "" {
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
		if msg := validateDashboardWidgets([]dashboardLayoutItem{tc.widget}); msg == "" {
			t.Fatalf("%s: expected rejection", tc.name)
		}
	}
}

// min >= max would make the arc maths meaningless rather than merely ugly.
func TestValidateGaugeWidgetRejectsInvertedRange(t *testing.T) {
	config := validGaugeConfig()
	low, high := 100.0, 10.0
	config.Min, config.Max = &low, &high

	if msg := validateDashboardWidgets([]dashboardLayoutItem{gaugeWidget("gauge:abcd1234", config)}); msg == "" {
		t.Fatalf("expected an inverted range to be rejected")
	}
}

// Zones reuse the alarm severity vocabulary (ADR 0038) rather than inventing
// gauge-only colours, so an unknown state is a real error.
func TestValidateGaugeWidgetRejectsUnknownZoneState(t *testing.T) {
	config := validGaugeConfig()
	config.Zones = []gaugeZone{{From: 0, To: 10, State: "spicy"}}

	if msg := validateDashboardWidgets([]dashboardLayoutItem{gaugeWidget("gauge:abcd1234", config)}); msg == "" {
		t.Fatalf("expected an unknown zone state to be rejected")
	}
}

// Reject rather than silently drop, matching the embed precedent: config the
// renderer will never read means the caller has misunderstood the model.
func TestValidateDashboardWidgetsRejectsGaugeConfigOnABuiltinWidget(t *testing.T) {
	widget := dashboardLayoutItem{ID: "wind", X: 0, Y: 0, W: 4, H: 4, Gauge: validGaugeConfig()}

	if msg := validateDashboardWidgets([]dashboardLayoutItem{widget}); msg == "" {
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
