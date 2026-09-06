package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func assertDashboardPageOrder(t *testing.T, want ...string) {
	t.Helper()
	c, rec := newDashboardPagesRequest(t, http.MethodGet, "/api/dashboard-pages", nil)
	if err := listDashboardPagesHandler(c); err != nil {
		t.Fatal(err)
	}
	var result dashboardPagesFile
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(result.Pages))
	for _, page := range result.Pages {
		got = append(got, page.ID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("page order = %v, want %v", got, want)
	}
}

func TestDashboardPageOrder_PersistsAndPreservesCRUD(t *testing.T) {
	setupDashboardPagesTest(t)
	a := createTestDashboardPage(t, "Anchored", sampleDashboardWidgets())
	u := createTestDashboardPage(t, "Underway", nil)
	d := createTestDashboardPage(t, "Docked", nil)
	want := []string{a.ID, d.ID, u.ID}
	c, rec := newDashboardPagesRequest(t, http.MethodPut, "/api/dashboard-pages/order", map[string]any{"page_ids": want})
	if err := reorderDashboardPagesHandler(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("reorder: %d %s", rec.Code, rec.Body.String())
	}
	var result dashboardPagesFile
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	for i, page := range result.Pages {
		if page.ID != want[i] || page.Position != i {
			t.Fatalf("response page %d = %+v", i, page)
		}
	}
	loadDashboardPages()
	assertDashboardPageOrder(t, want...)
	if !reflect.DeepEqual(dashboardPagesState[a.ID].Widgets, a.Widgets) {
		t.Fatal("reorder changed page widgets")
	}

	c, rec = newDashboardPagesRequest(t, http.MethodPatch, "/api/dashboard-pages/"+u.ID, map[string]string{"name": "Motoring"})
	c.SetParamNames("id")
	c.SetParamValues(u.ID)
	if err := patchDashboardPageHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("patch: %v %s", err, rec.Body.String())
	}
	assertDashboardPageOrder(t, want...)

	c, rec = newDashboardPagesRequest(t, http.MethodDelete, "/api/dashboard-pages/"+d.ID, nil)
	c.SetParamNames("id")
	c.SetParamValues(d.ID)
	if err := deleteDashboardPageHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("delete: %v %s", err, rec.Body.String())
	}
	p := createTestDashboardPage(t, "Cluster Preview", nil)
	assertDashboardPageOrder(t, a.ID, u.ID, p.ID)
	loadDashboardPages()
	assertDashboardPageOrder(t, a.ID, u.ID, p.ID)
}

func TestDashboardPageOrder_RejectsInvalidListsWithoutChanges(t *testing.T) {
	for _, kind := range []string{"missing", "null", "empty", "omitted", "duplicate", "unknown", "extra", "wrong type"} {
		t.Run(kind, func(t *testing.T) {
			setupDashboardPagesTest(t)
			a := createTestDashboardPage(t, "Anchored", nil)
			b := createTestDashboardPage(t, "Docked", nil)
			bodies := map[string]any{
				"missing":    map[string]any{},
				"null":       map[string]any{"page_ids": nil},
				"empty":      map[string]any{"page_ids": []string{}},
				"omitted":    map[string]any{"page_ids": []string{a.ID}},
				"duplicate":  map[string]any{"page_ids": []string{b.ID, b.ID}},
				"unknown":    map[string]any{"page_ids": []string{b.ID, "unknown"}},
				"extra":      map[string]any{"page_ids": []string{b.ID, a.ID, "unknown"}},
				"wrong type": map[string]any{"page_ids": "not-a-list"},
			}
			before, err := os.ReadFile(dashboardPagesFilePath())
			if err != nil {
				t.Fatal(err)
			}
			c, rec := newDashboardPagesRequest(t, http.MethodPut, "/api/dashboard-pages/order", bodies[kind])
			if err := reorderDashboardPagesHandler(c); err != nil {
				t.Fatal(err)
			}
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
			}
			assertDashboardPageOrder(t, a.ID, b.ID)
			after, err := os.ReadFile(dashboardPagesFilePath())
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("invalid request changed disk state")
			}
		})
	}
}

func TestDashboardPageOrder_FailedWriteLeavesMemoryAndDiskUnchanged(t *testing.T) {
	setupDashboardPagesTest(t)
	a := createTestDashboardPage(t, "Anchored", nil)
	b := createTestDashboardPage(t, "Docked", nil)
	path := dashboardPagesFilePath()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// A file cannot be used as a parent directory, even when running as root.
	t.Setenv("DASHBOARD_PAGES_FILE", filepath.Join(path, "impossible.json"))
	c, rec := newDashboardPagesRequest(t, http.MethodPut, "/api/dashboard-pages/order", map[string]any{"page_ids": []string{b.ID, a.ID}})
	if err := reorderDashboardPagesHandler(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	assertDashboardPageOrder(t, a.ID, b.ID)
	if !dashboardPagesState[b.ID].UpdatedAt.Equal(b.UpdatedAt) {
		t.Fatal("failed write changed timestamp")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("failed write changed disk state")
	}
}

func TestDashboardPageOrder_MigratesUnpositionedPagesDeterministically(t *testing.T) {
	setupDashboardPagesTest(t)
	data := `{"pages":[
		{"id":"c","name":"Newest","widgets":[],"created_at":"2026-09-02T00:00:00Z"},
		{"id":"b","name":"Second","widgets":[],"created_at":"2026-09-01T00:00:00Z"},
		{"id":"a","name":"First","widgets":[],"created_at":"2026-09-01T00:00:00Z"}
	]}`
	if err := os.WriteFile(dashboardPagesFilePath(), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	loadDashboardPages()
	assertDashboardPageOrder(t, "a", "b", "c")
	for i, id := range []string{"a", "b", "c"} {
		if dashboardPagesState[id].Position != i {
			t.Fatalf("position for %s = %d", id, dashboardPagesState[id].Position)
		}
	}
	loadDashboardPages()
	assertDashboardPageOrder(t, "a", "b", "c")
	d := createTestDashboardPage(t, "Fourth", nil)
	assertDashboardPageOrder(t, "a", "b", "c", d.ID)
}

func TestDashboardPageOrder_ProductionRouteRequiresWriteAccess(t *testing.T) {
	sessions := newTestSessionStore(t)
	for _, route := range buildAPIRoutes(sessions, newWorldImageryHTTPClient()) {
		if route.Method == http.MethodPut && route.Path == "/api/dashboard-pages/order" {
			if route.Tier != tierWrite {
				t.Fatalf("reorder route tier = %v, want tierWrite", route.Tier)
			}
			return
		}
	}
	t.Fatal("reorder route missing from production route table")
}
