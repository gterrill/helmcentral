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

func setupDashboardLayoutTest(t *testing.T) {
	t.Helper()
	t.Setenv("DASHBOARD_LAYOUT_FILE", filepath.Join(t.TempDir(), "dashboard-layout.json"))
	loadDashboardLayout()
}

func newDashboardLayoutRequest(t *testing.T, method, path string, body any) (echo.Context, *httptest.ResponseRecorder) {
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

func TestGetDashboardLayoutHandler_EmptyInitially(t *testing.T) {
	setupDashboardLayoutTest(t)

	c, rec := newDashboardLayoutRequest(t, http.MethodGet, "/api/dashboard-layout", nil)
	if err := getDashboardLayoutHandler(c); err != nil {
		t.Fatalf("getDashboardLayoutHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// Verify we get widgets as an empty array, not null
	widgets, ok := payload["widgets"]
	if !ok {
		t.Fatal("expected 'widgets' key in response")
	}

	// Check if it's an array (could be nil or []interface{}{})
	if widgets == nil {
		t.Fatal("expected widgets to be an empty array, got null")
	}
	widgetsArray, ok := widgets.([]interface{})
	if !ok {
		t.Fatalf("expected widgets to be an array, got %T", widgets)
	}
	if len(widgetsArray) != 0 {
		t.Fatalf("expected empty widgets array, got %d items", len(widgetsArray))
	}
}

func TestPutDashboardLayoutHandler_ValidBody_PersistsAndRoundTrips(t *testing.T) {
	setupDashboardLayoutTest(t)

	// First PUT with two widgets
	c, rec := newDashboardLayoutRequest(t, http.MethodPut, "/api/dashboard-layout", map[string]any{
		"widgets": []map[string]any{
			{"id": "wind", "x": 0, "y": 0, "w": 4, "h": 8},
			{"id": "tanks", "x": 4, "y": 0, "w": 4, "h": 4},
		},
	})
	if err := putDashboardLayoutHandler(c); err != nil {
		t.Fatalf("putDashboardLayoutHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var putResp dashboardLayoutData
	if err := json.Unmarshal(rec.Body.Bytes(), &putResp); err != nil {
		t.Fatalf("failed to parse PUT response: %v", err)
	}
	if len(putResp.Widgets) != 2 {
		t.Fatalf("expected 2 widgets in PUT response, got %d", len(putResp.Widgets))
	}

	// GET and verify
	c2, rec2 := newDashboardLayoutRequest(t, http.MethodGet, "/api/dashboard-layout", nil)
	if err := getDashboardLayoutHandler(c2); err != nil {
		t.Fatalf("getDashboardLayoutHandler returned error: %v", err)
	}
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec2.Code)
	}

	var getResp dashboardLayoutData
	if err := json.Unmarshal(rec2.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("failed to parse GET response: %v", err)
	}
	if len(getResp.Widgets) != 2 {
		t.Fatalf("expected 2 widgets in GET response, got %d", len(getResp.Widgets))
	}
	if getResp.Widgets[0].ID != "wind" || getResp.Widgets[1].ID != "tanks" {
		t.Fatal("expected widgets to round-trip correctly")
	}

	// Simulate server restart: reload from disk
	loadDashboardLayout()

	// GET again and confirm data survived
	c3, rec3 := newDashboardLayoutRequest(t, http.MethodGet, "/api/dashboard-layout", nil)
	if err := getDashboardLayoutHandler(c3); err != nil {
		t.Fatalf("getDashboardLayoutHandler returned error: %v", err)
	}
	if rec3.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec3.Code)
	}

	var reloadedResp dashboardLayoutData
	if err := json.Unmarshal(rec3.Body.Bytes(), &reloadedResp); err != nil {
		t.Fatalf("failed to parse reloaded GET response: %v", err)
	}
	if len(reloadedResp.Widgets) != 2 {
		t.Fatalf("expected 2 widgets after reload, got %d", len(reloadedResp.Widgets))
	}
	if reloadedResp.Widgets[0].ID != "wind" || reloadedResp.Widgets[1].ID != "tanks" {
		t.Fatal("expected widgets to survive reload from disk")
	}
}

func TestPutDashboardLayoutHandler_RejectsUnknownWidgetId(t *testing.T) {
	setupDashboardLayoutTest(t)

	c, rec := newDashboardLayoutRequest(t, http.MethodPut, "/api/dashboard-layout", map[string]any{
		"widgets": []map[string]any{
			{"id": "not-a-real-widget", "x": 0, "y": 0, "w": 4, "h": 4},
		},
	})
	if err := putDashboardLayoutHandler(c); err != nil {
		t.Fatalf("putDashboardLayoutHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestPutDashboardLayoutHandler_RejectsDuplicateWidgetId(t *testing.T) {
	setupDashboardLayoutTest(t)

	c, rec := newDashboardLayoutRequest(t, http.MethodPut, "/api/dashboard-layout", map[string]any{
		"widgets": []map[string]any{
			{"id": "wind", "x": 0, "y": 0, "w": 4, "h": 4},
			{"id": "wind", "x": 4, "y": 0, "w": 4, "h": 4},
		},
	})
	if err := putDashboardLayoutHandler(c); err != nil {
		t.Fatalf("putDashboardLayoutHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestPutDashboardLayoutHandler_RejectsNonPositiveWidthOrHeight(t *testing.T) {
	setupDashboardLayoutTest(t)

	// Test w: 0
	c, rec := newDashboardLayoutRequest(t, http.MethodPut, "/api/dashboard-layout", map[string]any{
		"widgets": []map[string]any{
			{"id": "wind", "x": 0, "y": 0, "w": 0, "h": 4},
		},
	})
	if err := putDashboardLayoutHandler(c); err != nil {
		t.Fatalf("putDashboardLayoutHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d for w=0, got %d", http.StatusBadRequest, rec.Code)
	}

	// Test h: -1
	c2, rec2 := newDashboardLayoutRequest(t, http.MethodPut, "/api/dashboard-layout", map[string]any{
		"widgets": []map[string]any{
			{"id": "wind", "x": 0, "y": 0, "w": 4, "h": -1},
		},
	})
	if err := putDashboardLayoutHandler(c2); err != nil {
		t.Fatalf("putDashboardLayoutHandler returned error: %v", err)
	}
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d for h=-1, got %d", http.StatusBadRequest, rec2.Code)
	}
}

func TestPutDashboardLayoutHandler_RejectsNegativeCoordinates(t *testing.T) {
	setupDashboardLayoutTest(t)

	c, rec := newDashboardLayoutRequest(t, http.MethodPut, "/api/dashboard-layout", map[string]any{
		"widgets": []map[string]any{
			{"id": "wind", "x": -1, "y": 0, "w": 4, "h": 4},
		},
	})
	if err := putDashboardLayoutHandler(c); err != nil {
		t.Fatalf("putDashboardLayoutHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestPutDashboardLayoutHandler_AcceptsEmptyWidgetsList(t *testing.T) {
	setupDashboardLayoutTest(t)

	c, rec := newDashboardLayoutRequest(t, http.MethodPut, "/api/dashboard-layout", map[string]any{
		"widgets": []map[string]any{},
	})
	if err := putDashboardLayoutHandler(c); err != nil {
		t.Fatalf("putDashboardLayoutHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	// Verify the response body has widgets as an array
	var resp dashboardLayoutData
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	// After PUT with empty array, we should have empty array (not nil)
	if resp.Widgets == nil {
		t.Fatal("expected widgets to be an empty array, not nil")
	}
	if len(resp.Widgets) != 0 {
		t.Fatalf("expected 0 widgets, got %d", len(resp.Widgets))
	}
}

func TestPutDashboardLayoutHandler_UpdatesUpdatedAtTimestamp(t *testing.T) {
	setupDashboardLayoutTest(t)

	// First PUT
	c1, rec1 := newDashboardLayoutRequest(t, http.MethodPut, "/api/dashboard-layout", map[string]any{
		"widgets": []map[string]any{
			{"id": "wind", "x": 0, "y": 0, "w": 4, "h": 4},
		},
	})
	if err := putDashboardLayoutHandler(c1); err != nil {
		t.Fatalf("putDashboardLayoutHandler returned error: %v", err)
	}
	if rec1.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec1.Code)
	}

	var resp1 dashboardLayoutData
	if err := json.Unmarshal(rec1.Body.Bytes(), &resp1); err != nil {
		t.Fatalf("failed to parse first PUT response: %v", err)
	}
	updatedAt1 := resp1.UpdatedAt

	// Small delay to ensure time difference
	time.Sleep(10 * time.Millisecond)

	// Second PUT with different content
	c2, rec2 := newDashboardLayoutRequest(t, http.MethodPut, "/api/dashboard-layout", map[string]any{
		"widgets": []map[string]any{
			{"id": "tanks", "x": 0, "y": 0, "w": 4, "h": 4},
		},
	})
	if err := putDashboardLayoutHandler(c2); err != nil {
		t.Fatalf("putDashboardLayoutHandler returned error: %v", err)
	}
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec2.Code)
	}

	var resp2 dashboardLayoutData
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("failed to parse second PUT response: %v", err)
	}
	updatedAt2 := resp2.UpdatedAt

	// Verify updatedAt changed (is more recent)
	if !updatedAt2.After(updatedAt1) {
		t.Fatalf("expected updated_at to increase, but got %v <= %v", updatedAt2, updatedAt1)
	}
}
