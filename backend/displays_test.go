package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupDisplaysTest mirrors setupDashboardPagesTest: an isolated on-disk
// file per test and a blank slate for both the pages and displays state,
// since a fresh install synthesizes a default "Anchored" page but never a
// display (plan §1: "fresh install and the legacy dashboard-layout.json
// path produce no displays").
func setupDisplaysTest(t *testing.T) {
	t.Helper()
	t.Setenv("DASHBOARD_PAGES_FILE", filepath.Join(t.TempDir(), "dashboard-pages.json"))
	t.Setenv("DASHBOARD_LAYOUT_FILE", filepath.Join(t.TempDir(), "dashboard-layout.json"))
	loadDashboardPages()

	dashboardPagesMu.Lock()
	dashboardPagesState = make(map[string]*dashboardPageData)
	displaysState = make(map[string]*displayData)
	dashboardPagesMu.Unlock()

	t.Cleanup(func() {
		dashboardPagesMu.Lock()
		dashboardRibbonState = nil
		displaysState = nil
		dashboardPagesMu.Unlock()
	})
}

func createTestDisplay(t *testing.T, body map[string]any) displayData {
	t.Helper()
	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/displays", body)
	if err := createDisplayHandler(c); err != nil {
		t.Fatalf("createDisplayHandler returned error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}
	var display displayData
	if err := json.Unmarshal(rec.Body.Bytes(), &display); err != nil {
		t.Fatalf("failed to parse created display: %v", err)
	}
	return display
}

// TestDisplayData_AlwaysSerializesCanvasSize pins the wire shape for the one
// pair of fields on displayData that deliberately has no omitempty: Width
// and Height are "full viewport" at zero, not "absent" - a display record
// serialized with neither key would read back client-side as undefined
// rather than 0, and displayFoldPx (frontend/src/lib/displays.ts) computes
// undefined - 8 = NaN where it must return null. Mirrors
// TestDashboardPageData_OmitsKioskKeysWhenUnset's inverse assertion for the
// kiosk keys, one file over.
func TestDisplayData_AlwaysSerializesCanvasSize(t *testing.T) {
	encoded, err := json.Marshal(&displayData{ID: "d1", Name: "Saloon TV", Slug: "saloon-tv"})
	if err != nil {
		t.Fatalf("failed to marshal display: %v", err)
	}
	for _, key := range []string{`"width":0`, `"height":0`} {
		if !strings.Contains(string(encoded), key) {
			t.Fatalf("expected a full-viewport display to serialize %s explicitly, got %s", key, encoded)
		}
	}
}

// ── GET /api/displays ───────────────────────────────────────────────────────

func TestListDisplaysHandler_EmptyInitially(t *testing.T) {
	setupDisplaysTest(t)

	c, rec := newDashboardPagesRequest(t, http.MethodGet, "/api/displays", nil)
	if err := listDisplaysHandler(c); err != nil {
		t.Fatalf("listDisplaysHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	var payload struct {
		Displays []displayData `json:"displays"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse list response: %v", err)
	}
	if len(payload.Displays) != 0 {
		t.Fatalf("expected 0 displays, got %d", len(payload.Displays))
	}
}

func TestListDisplaysHandler_OrdersByCreatedAtThenID(t *testing.T) {
	setupDisplaysTest(t)
	first := createTestDisplay(t, map[string]any{"name": "Flybridge", "width": 1920, "height": 360, "rotate": 180})
	second := createTestDisplay(t, map[string]any{"name": "Saloon TV", "width": 1280, "height": 720, "scale": 1.5})

	c, rec := newDashboardPagesRequest(t, http.MethodGet, "/api/displays", nil)
	if err := listDisplaysHandler(c); err != nil {
		t.Fatalf("listDisplaysHandler returned error: %v", err)
	}
	var payload struct {
		Displays []displayData `json:"displays"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse list response: %v", err)
	}
	if len(payload.Displays) != 2 || payload.Displays[0].ID != first.ID || payload.Displays[1].ID != second.ID {
		t.Fatalf("expected displays ordered oldest first, got %+v", payload.Displays)
	}
}

// ── POST /api/displays ───────────────────────────────────────────────────────

func TestCreateDisplayHandler_ValidBody(t *testing.T) {
	setupDisplaysTest(t)

	display := createTestDisplay(t, map[string]any{
		"name":        "Flybridge",
		"slug":        "flybridge",
		"width":       1920,
		"height":      360,
		"rotate":      180,
		"pixel_shift": false,
		"wake_lock":   false,
	})
	if display.ID == "" {
		t.Fatal("expected a generated ID")
	}
	if display.Name != "Flybridge" || display.Slug != "flybridge" {
		t.Fatalf("expected name/slug to round-trip, got %+v", display)
	}
	if display.Width != 1920 || display.Height != 360 || display.Rotate != 180 {
		t.Fatalf("expected geometry to round-trip, got %+v", display)
	}
	if display.CreatedAt.IsZero() || display.UpdatedAt.IsZero() {
		t.Fatal("expected created_at/updated_at to be set")
	}
}

func TestCreateDisplayHandler_RejectsEmptyName(t *testing.T) {
	setupDisplaysTest(t)
	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/displays", map[string]any{"name": "  "})
	if err := createDisplayHandler(c); err != nil {
		t.Fatalf("createDisplayHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestCreateDisplayHandler_RejectsNameTooLong(t *testing.T) {
	setupDisplaysTest(t)
	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/displays", map[string]any{
		"name": strings.Repeat("a", displayNameMaxLen+1),
	})
	if err := createDisplayHandler(c); err != nil {
		t.Fatalf("createDisplayHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestCreateDisplayHandler_DerivesSlugFromName(t *testing.T) {
	setupDisplaysTest(t)
	display := createTestDisplay(t, map[string]any{"name": "Saloon TV"})
	if display.Slug != "saloon-tv" {
		t.Fatalf("expected slug derived from name, got %q", display.Slug)
	}
}

func TestCreateDisplayHandler_RejectsSlugTooLong(t *testing.T) {
	setupDisplaysTest(t)
	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/displays", map[string]any{
		"name": "Flybridge",
		"slug": strings.Repeat("a", displaySlugMaxLen+1),
	})
	if err := createDisplayHandler(c); err != nil {
		t.Fatalf("createDisplayHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestCreateDisplayHandler_RejectsInvalidSlugFormat(t *testing.T) {
	setupDisplaysTest(t)
	for _, slug := range []string{"Flybridge", "fly bridge", "-flybridge", "flybridge-", "fly--bridge", "fly_bridge"} {
		c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/displays", map[string]any{
			"name": "Flybridge",
			"slug": slug,
		})
		if err := createDisplayHandler(c); err != nil {
			t.Fatalf("createDisplayHandler returned error: %v", err)
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("slug=%q: expected status %d, got %d: %s", slug, http.StatusBadRequest, rec.Code, rec.Body.String())
		}
	}
}

func TestCreateDisplayHandler_RejectsDuplicateSlug(t *testing.T) {
	setupDisplaysTest(t)
	createTestDisplay(t, map[string]any{"name": "Flybridge", "slug": "flybridge"})

	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/displays", map[string]any{
		"name": "Flybridge Two",
		"slug": "flybridge",
	})
	if err := createDisplayHandler(c); err != nil {
		t.Fatalf("createDisplayHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

// A collision on a derived slug is rejected outright, never auto-suffixed
// (plan §1): "a derived slug that collides is rejected asking for an
// explicit one - never silently -2 suffixed."
func TestCreateDisplayHandler_RejectsDerivedSlugCollision(t *testing.T) {
	setupDisplaysTest(t)
	createTestDisplay(t, map[string]any{"name": "Saloon TV"})

	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/displays", map[string]any{"name": "Saloon TV"})
	if err := createDisplayHandler(c); err != nil {
		t.Fatalf("createDisplayHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
	dashboardPagesMu.RLock()
	count := len(displaysState)
	dashboardPagesMu.RUnlock()
	if count != 1 {
		t.Fatalf("expected the collision to be rejected outright rather than auto-suffixed, got %d displays", count)
	}
}

func TestCreateDisplayHandler_RejectsWidthWithoutHeight(t *testing.T) {
	setupDisplaysTest(t)
	for _, body := range []map[string]any{
		{"name": "A", "width": 1920},
		{"name": "B", "height": 1080},
	} {
		c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/displays", body)
		if err := createDisplayHandler(c); err != nil {
			t.Fatalf("createDisplayHandler returned error: %v", err)
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body=%+v: expected status %d, got %d: %s", body, http.StatusBadRequest, rec.Code, rec.Body.String())
		}
	}
}

func TestCreateDisplayHandler_AcceptsBothZero(t *testing.T) {
	setupDisplaysTest(t)
	display := createTestDisplay(t, map[string]any{"name": "Whatever The Browser Reports"})
	if display.Width != 0 || display.Height != 0 {
		t.Fatalf("expected a full-viewport display, got %+v", display)
	}
}

func TestCreateDisplayHandler_RejectsWidthOutOfRange(t *testing.T) {
	setupDisplaysTest(t)
	for _, width := range []int{displayMinPx - 1, displayMaxWidthPx + 1} {
		c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/displays", map[string]any{
			"name": "Bad", "width": width, "height": 1080,
		})
		if err := createDisplayHandler(c); err != nil {
			t.Fatalf("createDisplayHandler returned error: %v", err)
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("width=%d: expected status %d, got %d: %s", width, http.StatusBadRequest, rec.Code, rec.Body.String())
		}
	}
}

func TestCreateDisplayHandler_RejectsHeightOutOfRange(t *testing.T) {
	setupDisplaysTest(t)
	for _, height := range []int{displayMinPx - 1, displayMaxHeightPx + 1} {
		c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/displays", map[string]any{
			"name": "Bad", "width": 1920, "height": height,
		})
		if err := createDisplayHandler(c); err != nil {
			t.Fatalf("createDisplayHandler returned error: %v", err)
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("height=%d: expected status %d, got %d: %s", height, http.StatusBadRequest, rec.Code, rec.Body.String())
		}
	}
}

func TestCreateDisplayHandler_RejectsScaleOutOfRange(t *testing.T) {
	setupDisplaysTest(t)
	for _, scale := range []float64{displayMinScale - 0.1, displayMaxScale + 0.1} {
		c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/displays", map[string]any{
			"name": "Bad", "width": 1920, "height": 1080, "scale": scale,
		})
		if err := createDisplayHandler(c); err != nil {
			t.Fatalf("createDisplayHandler returned error: %v", err)
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("scale=%v: expected status %d, got %d: %s", scale, http.StatusBadRequest, rec.Code, rec.Body.String())
		}
	}
}

// Scaling a viewport that was never measured produces a board whose extent
// nobody can predict (plan §1), so a non-1.0 scale requires a canvas size.
func TestCreateDisplayHandler_RejectsNonUnitScaleWithZeroCanvas(t *testing.T) {
	setupDisplaysTest(t)
	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/displays", map[string]any{
		"name": "Bad", "scale": 1.5,
	})
	if err := createDisplayHandler(c); err != nil {
		t.Fatalf("createDisplayHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestCreateDisplayHandler_AcceptsUnitScaleWithZeroCanvas(t *testing.T) {
	setupDisplaysTest(t)
	display := createTestDisplay(t, map[string]any{"name": "Fine", "scale": 1.0})
	if display.Width != 0 || display.Height != 0 {
		t.Fatalf("expected a full-viewport display, got %+v", display)
	}
}

func TestCreateDisplayHandler_RejectsUnknownRotation(t *testing.T) {
	setupDisplaysTest(t)
	for _, rotate := range []int{90, 270, -180, 45} {
		c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/displays", map[string]any{
			"name": "Bad", "rotate": rotate,
		})
		if err := createDisplayHandler(c); err != nil {
			t.Fatalf("createDisplayHandler returned error: %v", err)
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("rotate=%d: expected status %d, got %d: %s", rotate, http.StatusBadRequest, rec.Code, rec.Body.String())
		}
	}
}

func TestCreateDisplayHandler_AcceptsRotate180(t *testing.T) {
	setupDisplaysTest(t)
	display := createTestDisplay(t, map[string]any{"name": "Flybridge", "rotate": 180})
	if display.Rotate != 180 {
		t.Fatalf("expected rotate to round-trip, got %d", display.Rotate)
	}
}

func TestCreateDisplayHandler_RejectsPastMaxCount(t *testing.T) {
	setupDisplaysTest(t)
	for i := 0; i < displayMaxCount; i++ {
		createTestDisplay(t, map[string]any{"name": "Display", "slug": strings_repeat_unique(i)})
	}
	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/displays", map[string]any{
		"name": "One Too Many", "slug": "one-too-many",
	})
	if err := createDisplayHandler(c); err != nil {
		t.Fatalf("createDisplayHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

// strings_repeat_unique gives each display in the max-count loop a distinct
// slug (an identical name is fine; slugs must be unique) without pulling in
// strconv purely for this one loop.
func strings_repeat_unique(i int) string {
	return "display-" + string(rune('a'+i))
}

// ── PATCH /api/displays/:id ─────────────────────────────────────────────────

func patchTestDisplay(t *testing.T, id string, body map[string]any) (*displayData, int, string) {
	t.Helper()
	c, rec := newDashboardPagesRequest(t, http.MethodPatch, "/api/displays/"+id, body)
	c.SetParamNames("id")
	c.SetParamValues(id)
	if err := patchDisplayHandler(c); err != nil {
		t.Fatalf("patchDisplayHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		return nil, rec.Code, rec.Body.String()
	}
	var display displayData
	if err := json.Unmarshal(rec.Body.Bytes(), &display); err != nil {
		t.Fatalf("failed to parse patch response: %v", err)
	}
	return &display, rec.Code, rec.Body.String()
}

func TestPatchDisplayHandler_UpdatesFields(t *testing.T) {
	setupDisplaysTest(t)
	display := createTestDisplay(t, map[string]any{"name": "Flybridge", "width": 1920, "height": 360, "rotate": 180})

	updated, code, body := patchTestDisplay(t, display.ID, map[string]any{
		"name": "Flybridge Strip", "scale": 1.2,
	})
	if code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, code, body)
	}
	if updated.Name != "Flybridge Strip" || updated.Scale != 1.2 {
		t.Fatalf("expected patched fields to apply, got %+v", updated)
	}
	if updated.Width != 1920 || updated.Height != 360 || updated.Rotate != 180 {
		t.Fatalf("expected untouched fields to survive, got %+v", updated)
	}
}

func TestPatchDisplayHandler_RejectsNoFields(t *testing.T) {
	setupDisplaysTest(t)
	display := createTestDisplay(t, map[string]any{"name": "Flybridge"})
	_, code, _ := patchTestDisplay(t, display.ID, map[string]any{})
	if code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, code)
	}
}

func TestPatchDisplayHandler_NotFound(t *testing.T) {
	setupDisplaysTest(t)
	_, code, _ := patchTestDisplay(t, "no-such-id", map[string]any{"name": "Anything"})
	if code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, code)
	}
}

func TestPatchDisplayHandler_AllowsKeepingItsOwnSlug(t *testing.T) {
	setupDisplaysTest(t)
	display := createTestDisplay(t, map[string]any{"name": "Flybridge", "slug": "flybridge"})
	_, code, body := patchTestDisplay(t, display.ID, map[string]any{"slug": "flybridge"})
	if code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, code, body)
	}
}

func TestPatchDisplayHandler_RejectsSlugUsedByAnotherDisplay(t *testing.T) {
	setupDisplaysTest(t)
	createTestDisplay(t, map[string]any{"name": "Flybridge", "slug": "flybridge"})
	second := createTestDisplay(t, map[string]any{"name": "Saloon TV", "slug": "saloon-tv"})

	_, code, _ := patchTestDisplay(t, second.ID, map[string]any{"slug": "flybridge"})
	if code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, code)
	}
}

func TestPatchDisplayHandler_RejectsGeometryThatBreaksValidation(t *testing.T) {
	setupDisplaysTest(t)
	display := createTestDisplay(t, map[string]any{"name": "Flybridge", "width": 1920, "height": 360})
	_, code, _ := patchTestDisplay(t, display.ID, map[string]any{"rotate": 90})
	if code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, code)
	}
}

// ── DELETE /api/displays/:id ─────────────────────────────────────────────────

func TestDeleteDisplayHandler_NotFound(t *testing.T) {
	setupDisplaysTest(t)
	c, rec := newDashboardPagesRequest(t, http.MethodDelete, "/api/displays/no-such-id", nil)
	c.SetParamNames("id")
	c.SetParamValues("no-such-id")
	if err := deleteDisplayHandler(c); err != nil {
		t.Fatalf("deleteDisplayHandler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}

// A client iterating released_page_ids directly should never have to guard
// against null, mirroring the "ensure it's an empty slice for consistency"
// stance createDashboardPageHandler already takes for a nil Widgets body.
func TestDeleteDisplayHandler_ReleasedPageIDsIsEmptyArrayNotNull(t *testing.T) {
	setupDisplaysTest(t)
	display := createTestDisplay(t, map[string]any{"name": "Flybridge"})

	c, rec := newDashboardPagesRequest(t, http.MethodDelete, "/api/displays/"+display.ID, nil)
	c.SetParamNames("id")
	c.SetParamValues(display.ID)
	if err := deleteDisplayHandler(c); err != nil {
		t.Fatalf("deleteDisplayHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"released_page_ids":null`) {
		t.Fatalf("expected released_page_ids to serialize as [], got %s", rec.Body.String())
	}
}

// TestDeleteDisplay_ReleasesItsPages is the plan's §3 pin: deleting a
// display clears display_id on every page that referenced it - dwell and
// condition survive untouched - and never deletes the page itself.
func TestDeleteDisplay_ReleasesItsPages(t *testing.T) {
	setupDisplaysTest(t)
	display := createTestDisplay(t, map[string]any{"name": "Flybridge", "width": 1920, "height": 360, "rotate": 180})

	onDisplay := createTestDashboardPage(t, "Wall: Engines", sampleDashboardWidgets())
	c, rec := newDashboardPagesRequest(t, http.MethodPatch, "/api/dashboard-pages/"+onDisplay.ID, map[string]any{
		"display_id": display.ID, "dwell_seconds": 30, "show_when": "anchored",
	})
	c.SetParamNames("id")
	c.SetParamValues(onDisplay.ID)
	if err := patchDashboardPageHandler(c); err != nil {
		t.Fatalf("patchDashboardPageHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	notOnDisplay := createTestDashboardPage(t, "Ordinary Page", sampleDashboardWidgets())

	dc, drec := newDashboardPagesRequest(t, http.MethodDelete, "/api/displays/"+display.ID, nil)
	dc.SetParamNames("id")
	dc.SetParamValues(display.ID)
	if err := deleteDisplayHandler(dc); err != nil {
		t.Fatalf("deleteDisplayHandler returned error: %v", err)
	}
	if drec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, drec.Code, drec.Body.String())
	}
	var payload struct {
		Status          string   `json:"status"`
		ReleasedPageIDs []string `json:"released_page_ids"`
	}
	if err := json.Unmarshal(drec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse delete response: %v", err)
	}
	if payload.Status != "deleted" {
		t.Fatalf("expected status %q, got %q", "deleted", payload.Status)
	}
	if len(payload.ReleasedPageIDs) != 1 || payload.ReleasedPageIDs[0] != onDisplay.ID {
		t.Fatalf("expected exactly the assigned page in released_page_ids, got %v", payload.ReleasedPageIDs)
	}

	dashboardPagesMu.RLock()
	released := dashboardPagesState[onDisplay.ID]
	untouched := dashboardPagesState[notOnDisplay.ID]
	_, stillExists := displaysState[display.ID]
	dashboardPagesMu.RUnlock()

	if released.DisplayID != "" {
		t.Fatalf("expected display_id cleared on the released page, got %q", released.DisplayID)
	}
	if released.DwellSeconds != 30 || released.ShowWhen != "anchored" {
		t.Fatalf("expected dwell and condition to survive release, got dwell=%d when=%q", released.DwellSeconds, released.ShowWhen)
	}
	if untouched.DisplayID != "" {
		t.Fatalf("expected the unrelated page to be untouched, got display_id=%q", untouched.DisplayID)
	}
	if stillExists {
		t.Fatal("expected the display record itself to be removed")
	}
}

// A failed write must leave memory agreeing with disk, the same stance
// reorderDashboardPagesHandler and putDashboardRibbonHandler already take.
// Deleting a display touches several records at once (the display itself
// plus every page that referenced it), so publishing before the write is
// durable is the case where memory and disk can disagree across the widest
// span: the API would report a release that vanishes at the next restart.
func TestDeleteDisplay_FailedWriteLeavesMemoryAndDiskUnchanged(t *testing.T) {
	setupDashboardPagesTest(t)
	display := createTestDisplay(t, map[string]any{"name": "Flybridge", "slug": "flybridge", "width": 1920, "height": 360})
	page := createTestDashboardPage(t, "Wall: Engines", sampleDashboardWidgets())
	assign, _ := newDashboardPagesRequest(t, http.MethodPatch, "/api/dashboard-pages/"+page.ID, map[string]any{
		"display_id": display.ID, "dwell_seconds": 30,
	})
	assign.SetParamNames("id")
	assign.SetParamValues(page.ID)
	if err := patchDashboardPageHandler(assign); err != nil {
		t.Fatal(err)
	}

	path := dashboardPagesFilePath()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// A file cannot be used as a parent directory, even when running as root.
	t.Setenv("DASHBOARD_PAGES_FILE", filepath.Join(path, "impossible.json"))
	c, rec := newDashboardPagesRequest(t, http.MethodDelete, "/api/displays/"+display.ID, nil)
	c.SetParamNames("id")
	c.SetParamValues(display.ID)
	if err := deleteDisplayHandler(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}

	if _, ok := displaysState[display.ID]; !ok {
		t.Error("failed write dropped the display from memory")
	}
	if got := dashboardPagesState[page.ID].DisplayID; got != display.ID {
		t.Errorf("failed write released the page in memory: display_id = %q, want %q", got, display.ID)
	}

	t.Setenv("DASHBOARD_PAGES_FILE", path)
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("failed write changed disk state")
	}
}

// A name is allowed to be longer than a slug (48 vs 32), and the create
// form offers no slug field, so a derived slug that overruns the limit left
// the operator unable to create the display at all with no way to fix it.
// An EXPLICIT slug over the limit is still rejected: they typed it.
func TestCreateDisplay_DerivedSlugIsTruncatedToFit(t *testing.T) {
	setupDisplaysTest(t)

	name := "Saloon Television On The Starboard Bulkhead" // 43 chars
	display := createTestDisplay(t, map[string]any{"name": name, "width": 1920, "height": 1080})

	if len(display.Slug) > displaySlugMaxLen {
		t.Errorf("derived slug %q is %d chars, over the %d limit", display.Slug, len(display.Slug), displaySlugMaxLen)
	}
	if !displaySlugPattern.MatchString(display.Slug) {
		t.Errorf("derived slug %q does not match the slug pattern (a truncation left a trailing hyphen?)", display.Slug)
	}
	if display.Name != name {
		t.Errorf("name = %q, want it stored in full", display.Name)
	}
}

func TestCreateDisplay_ExplicitOverlongSlugIsStillRejected(t *testing.T) {
	setupDisplaysTest(t)

	c, rec := newDashboardPagesRequest(t, http.MethodPost, "/api/displays", map[string]any{
		"name": "TV", "slug": strings.Repeat("a", displaySlugMaxLen+1), "width": 1920, "height": 1080,
	})
	if err := createDisplayHandler(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
