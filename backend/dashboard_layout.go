package main

import (
	"encoding/json"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

var validDashboardWidgetIDs = map[string]bool{
	"vessel":          true,
	"wind":            true,
	"depth-tide":      true,
	"position":        true,
	"today-now":       true,
	"anchor-watch":    true,
	"rode-scope":      true,
	"tanks":           true,
	"route":           true,
	"nearby-vessels":  true,
	"battery-power":   true,
	"alternator":      true,
	"generator":       true,
	"czone-switches":  true,
}

const dashboardLayoutMaxCoord = 1000

type dashboardLayoutItem struct {
	ID string `json:"id"`
	X  int    `json:"x"`
	Y  int    `json:"y"`
	W  int    `json:"w"`
	H  int    `json:"h"`
}

type dashboardLayoutData struct {
	Widgets   []dashboardLayoutItem `json:"widgets"`
	UpdatedAt time.Time             `json:"updated_at"`
}

var (
	dashboardLayoutMu    sync.RWMutex
	dashboardLayoutState *dashboardLayoutData // nil = no layout saved yet
)

func dashboardLayoutFilePath() string {
	return cacheFilePath("DASHBOARD_LAYOUT_FILE", "data/dashboard-layout.json")
}

func loadDashboardLayout() {
	dashboardLayoutMu.Lock()
	defer dashboardLayoutMu.Unlock()
	dashboardLayoutState = nil

	data, err := os.ReadFile(dashboardLayoutFilePath())
	if err != nil {
		return // no file yet — legitimate first-run state, not an error
	}
	var loaded dashboardLayoutData
	if err := json.Unmarshal(data, &loaded); err != nil {
		return // corrupt file treated same as absent; do not crash startup
	}
	dashboardLayoutState = &loaded
}

func saveDashboardLayout(d *dashboardLayoutData) error {
	return writeJSONFileAtomic(dashboardLayoutFilePath(), d)
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

// GET /api/dashboard-layout
func getDashboardLayoutHandler(c echo.Context) error {
	dashboardLayoutMu.RLock()
	state := dashboardLayoutState
	dashboardLayoutMu.RUnlock()

	if state == nil {
		return c.JSON(http.StatusOK, map[string]any{"widgets": []dashboardLayoutItem{}})
	}
	return c.JSON(http.StatusOK, state)
}

// PUT /api/dashboard-layout
func putDashboardLayoutHandler(c echo.Context) error {
	var body struct {
		Widgets []dashboardLayoutItem `json:"widgets"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}

	// Handle nil slice: ensure it's an empty slice for consistency
	if body.Widgets == nil {
		body.Widgets = []dashboardLayoutItem{}
	}

	if msg := validateDashboardWidgets(body.Widgets); msg != "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": msg})
	}

	updated := &dashboardLayoutData{
		Widgets:   body.Widgets,
		UpdatedAt: time.Now().UTC(),
	}
	if err := saveDashboardLayout(updated); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to persist"})
	}

	dashboardLayoutMu.Lock()
	dashboardLayoutState = updated
	dashboardLayoutMu.Unlock()

	return c.JSON(http.StatusOK, updated)
}
