package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

var validDashboardWidgetIDs = map[string]bool{
	"vessel":         true,
	"wind":           true,
	"depth-tide":     true,
	"position":       true,
	"today-now":      true,
	"anchor-watch":   true,
	"rode-scope":     true,
	"tanks":          true,
	"route":          true,
	"nearby-vessels": true,
	"battery-power":  true,
	"solar":          true,
	"alternator":     true,
	"generator":      true,
	"czone-switches": true,
	"hot-water":      true,
}

const dashboardLayoutMaxCoord = 1000

type dashboardLayoutItem struct {
	ID string `json:"id"`
	X  int    `json:"x"`
	Y  int    `json:"y"`
	W  int    `json:"w"`
	H  int    `json:"h"`
}

// defaultDashboardLayout recreates the pre-bento 3-column arrangement, used to
// synthesize a first page for a genuinely fresh install (mirrors the frontend's
// former App.tsx DEFAULT_DASHBOARD_LAYOUT constant, now owned by the backend).
var defaultDashboardLayout = []dashboardLayoutItem{
	{ID: "vessel", X: 0, Y: 0, W: 12, H: 3},
	{ID: "wind", X: 0, Y: 3, W: 4, H: 8},
	{ID: "depth-tide", X: 0, Y: 11, W: 4, H: 7},
	{ID: "position", X: 0, Y: 18, W: 4, H: 5},
	{ID: "today-now", X: 0, Y: 23, W: 4, H: 5},
	{ID: "anchor-watch", X: 4, Y: 3, W: 4, H: 8},
	{ID: "rode-scope", X: 4, Y: 11, W: 4, H: 6},
	{ID: "tanks", X: 4, Y: 17, W: 4, H: 4},
	{ID: "route", X: 4, Y: 21, W: 4, H: 4},
	{ID: "nearby-vessels", X: 4, Y: 25, W: 4, H: 5},
	{ID: "battery-power", X: 8, Y: 3, W: 4, H: 12},
	{ID: "solar", X: 8, Y: 15, W: 4, H: 6},
	{ID: "alternator", X: 8, Y: 21, W: 4, H: 6},
	{ID: "generator", X: 8, Y: 27, W: 4, H: 5},
	{ID: "czone-switches", X: 8, Y: 32, W: 4, H: 6},
	{ID: "hot-water", X: 8, Y: 38, W: 4, H: 6},
}

type dashboardPageData struct {
	ID        string                `json:"id"`
	Name      string                `json:"name"`
	Widgets   []dashboardLayoutItem `json:"widgets"`
	CreatedAt time.Time             `json:"created_at"`
	UpdatedAt time.Time             `json:"updated_at"`
}

type dashboardPagesFile struct {
	Pages []*dashboardPageData `json:"pages"`
}

var (
	dashboardPagesMu    sync.RWMutex
	dashboardPagesState map[string]*dashboardPageData
)

func dashboardPagesFilePath() string {
	return cacheFilePath("DASHBOARD_PAGES_FILE", "data/dashboard-pages.json")
}

func saveDashboardPagesLocked() error {
	list := make([]*dashboardPageData, 0, len(dashboardPagesState))
	for _, p := range dashboardPagesState {
		list = append(list, p)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].CreatedAt.Before(list[j].CreatedAt)
	})
	return writeJSONFileAtomic(dashboardPagesFilePath(), dashboardPagesFile{Pages: list})
}

func validateDashboardWidgets(widgets []dashboardLayoutItem) string {
	seen := make(map[string]bool, len(widgets))
	for _, w := range widgets {
		if !validDashboardWidgetIDs[w.ID] {
			return "unknown widget id: " + w.ID
		}
		if seen[w.ID] {
			return "duplicate widget id: " + w.ID
		}
		seen[w.ID] = true
		if w.W <= 0 || w.H <= 0 {
			return "widget w/h must be positive"
		}
		if w.X < 0 || w.Y < 0 {
			return "widget x/y must be non-negative"
		}
		if w.X > dashboardLayoutMaxCoord || w.Y > dashboardLayoutMaxCoord ||
			w.W > dashboardLayoutMaxCoord || w.H > dashboardLayoutMaxCoord {
			return "widget coordinate out of bounds"
		}
	}
	return ""
}

// loadDashboardPages loads pages from the new format, or migrates from the legacy single-layout format.
// Migration logic:
// 1. Try reading the new pages file. If it exists and parses, use it. Done.
// 2. Else, try reading the legacy layout file. If it exists and parses, create one page named "Anchored" from it and persist as new format.
// 3. If neither file exists, this is a fresh install: synthesize one default page named "Anchored" from defaultDashboardLayout and persist it.
func loadDashboardPages() {
	dashboardPagesMu.Lock()
	defer dashboardPagesMu.Unlock()

	dashboardPagesState = make(map[string]*dashboardPageData)

	// Try loading new format first
	data, err := os.ReadFile(dashboardPagesFilePath())
	if err == nil {
		// New file exists, try to parse it
		var loaded dashboardPagesFile
		if err := json.Unmarshal(data, &loaded); err == nil {
			for _, p := range loaded.Pages {
				dashboardPagesState[p.ID] = p
			}
			return
		}
		// If new file exists but is corrupt, still return empty (don't fall back to legacy)
		return
	}

	// New file doesn't exist, try legacy migration
	legacyPath := cacheFilePath("DASHBOARD_LAYOUT_FILE", "data/dashboard-layout.json")
	legacyData, err := os.ReadFile(legacyPath)
	if err != nil {
		// Neither file exists: this is a genuinely fresh install. Synthesize
		// one default page so a fresh dashboard has content on first load,
		// without a client-side bootstrap effect.
		now := time.Now().UTC()
		page := &dashboardPageData{
			ID:        uuid.NewString(),
			Name:      "Anchored",
			Widgets:   defaultDashboardLayout,
			CreatedAt: now,
			UpdatedAt: now,
		}
		dashboardPagesState[page.ID] = page

		if err := saveDashboardPagesLocked(); err != nil {
			log.Printf("Failed to persist synthesized default dashboard page: %v", err)
		}
		return
	}

	// Legacy file exists, try to parse it
	type legacyDashboardLayoutData struct {
		Widgets   []dashboardLayoutItem `json:"widgets"`
		UpdatedAt time.Time             `json:"updated_at"`
	}
	var legacy legacyDashboardLayoutData
	if err := json.Unmarshal(legacyData, &legacy); err != nil {
		// Legacy file is corrupt, leave state empty
		return
	}

	// Create one page from legacy data
	now := time.Now().UTC()
	createdAt := legacy.UpdatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	page := &dashboardPageData{
		ID:        uuid.NewString(),
		Name:      "Anchored",
		Widgets:   legacy.Widgets,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}
	dashboardPagesState[page.ID] = page

	// Persist migrated data to the new format
	if err := saveDashboardPagesLocked(); err != nil {
		log.Printf("Failed to persist migrated dashboard page: %v", err)
	}
}

// GET /api/dashboard-pages
func listDashboardPagesHandler(c echo.Context) error {
	dashboardPagesMu.RLock()
	defer dashboardPagesMu.RUnlock()

	list := make([]*dashboardPageData, 0, len(dashboardPagesState))
	for _, p := range dashboardPagesState {
		list = append(list, p)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].CreatedAt.Before(list[j].CreatedAt)
	})

	return c.JSON(http.StatusOK, map[string]any{"pages": list})
}

// POST /api/dashboard-pages
func createDashboardPageHandler(c echo.Context) error {
	var body struct {
		Name    string                `json:"name"`
		Widgets []dashboardLayoutItem `json:"widgets"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "name is required"})
	}

	// Handle nil slice: ensure it's an empty slice for consistency
	if body.Widgets == nil {
		body.Widgets = []dashboardLayoutItem{}
	}

	if msg := validateDashboardWidgets(body.Widgets); msg != "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": msg})
	}

	now := time.Now().UTC()
	page := &dashboardPageData{
		ID:        uuid.NewString(),
		Name:      name,
		Widgets:   body.Widgets,
		CreatedAt: now,
		UpdatedAt: now,
	}

	dashboardPagesMu.Lock()
	dashboardPagesState[page.ID] = page
	err := saveDashboardPagesLocked()
	dashboardPagesMu.Unlock()

	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to persist"})
	}

	return c.JSON(http.StatusCreated, page)
}

// GET /api/dashboard-pages/:id
func getDashboardPageHandler(c echo.Context) error {
	id := c.Param("id")

	dashboardPagesMu.RLock()
	page, ok := dashboardPagesState[id]
	dashboardPagesMu.RUnlock()

	if !ok {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "page not found"})
	}
	return c.JSON(http.StatusOK, page)
}

// PATCH /api/dashboard-pages/:id
func patchDashboardPageHandler(c echo.Context) error {
	id := c.Param("id")

	var body struct {
		Name    *string                `json:"name"`
		Widgets *[]dashboardLayoutItem `json:"widgets"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if body.Name == nil && body.Widgets == nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "no patch fields provided"})
	}
	if body.Name != nil && strings.TrimSpace(*body.Name) == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "name cannot be empty"})
	}
	if body.Widgets != nil {
		if msg := validateDashboardWidgets(*body.Widgets); msg != "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": msg})
		}
	}

	dashboardPagesMu.Lock()
	defer dashboardPagesMu.Unlock()

	current, ok := dashboardPagesState[id]
	if !ok {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "page not found"})
	}

	updated := &dashboardPageData{
		ID:        current.ID,
		Name:      current.Name,
		Widgets:   current.Widgets,
		CreatedAt: current.CreatedAt,
		UpdatedAt: time.Now().UTC(),
	}
	if body.Name != nil {
		updated.Name = strings.TrimSpace(*body.Name)
	}
	if body.Widgets != nil {
		updated.Widgets = *body.Widgets
	}

	dashboardPagesState[id] = updated
	if err := saveDashboardPagesLocked(); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to persist"})
	}

	return c.JSON(http.StatusOK, updated)
}

// DELETE /api/dashboard-pages/:id
func deleteDashboardPageHandler(c echo.Context) error {
	id := c.Param("id")

	dashboardPagesMu.Lock()
	defer dashboardPagesMu.Unlock()

	if _, ok := dashboardPagesState[id]; !ok {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "page not found"})
	}

	// Cannot delete the only remaining page
	if len(dashboardPagesState) == 1 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "cannot delete the only dashboard page"})
	}

	delete(dashboardPagesState, id)
	if err := saveDashboardPagesLocked(); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to persist"})
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "deleted"})
}
