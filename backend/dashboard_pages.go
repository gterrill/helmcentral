package main

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
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

// Embed widgets (ADR 0031) are the one widget type that can appear more than
// once on a page, so their id carries a per-instance token: "embed:<token>".
// The token only needs to be unique within a page — it is a layout key, not a
// secret — which is why the frontend mints it without crypto.randomUUID (that
// API is secure-context-only and undefined over plain HTTP on a boat LAN).
const embedWidgetIDPrefix = "embed:"

// Gauge widgets (ADR 0039) are the second multi-instance widget type, following
// exactly the precedent ADR 0031 set for embeds: a per-instance token in the id
// and per-instance config riding on the layout item.
const gaugeWidgetIDPrefix = "gauge:"

const (
	gaugeLabelMaxLen = 48
	gaugePathMaxLen  = 256
)

// Display kinds a gauge can render as.
var validGaugeDisplays = map[string]bool{
	"numeric": true, "radial": true, "bar": true, "lamp": true,
}

const (
	embedURLMaxLen   = 2048
	embedTitleMaxLen = 64
)

var embedWidgetTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

type dashboardLayoutItem struct {
	ID    string                `json:"id"`
	X     int                   `json:"x"`
	Y     int                   `json:"y"`
	W     int                   `json:"w"`
	H     int                   `json:"h"`
	Embed *dashboardEmbedConfig `json:"embed,omitempty"`
	Gauge *dashboardGaugeConfig `json:"gauge,omitempty"`
}

// dashboardGaugeConfig binds one widget to one SignalK path. Like the embed
// config it is per-instance and per-page, so it rides on the layout item.
// omitempty keeps existing dashboard-pages.json files byte-identical.
type dashboardGaugeConfig struct {
	Path     string      `json:"path"`
	Label    string      `json:"label"`
	Display  string      `json:"display"`
	Quantity string      `json:"quantity"`
	Unit     string      `json:"unit"`
	Decimals *int        `json:"decimals,omitempty"`
	Min      *float64    `json:"min,omitempty"`
	Max      *float64    `json:"max,omitempty"`
	Zones    []gaugeZone `json:"zones,omitempty"`
}

// gaugeZone colours a band of the range by alarm severity, reusing the same
// vocabulary as alarms (ADR 0038) rather than inventing gauge-only colours.
type gaugeZone struct {
	From  float64 `json:"from"`
	To    float64 `json:"to"`
	State string  `json:"state"`
}

// dashboardEmbedConfig is per-instance and per-page, so it rides on the layout
// item rather than living in settings.yaml.
type dashboardEmbedConfig struct {
	Title string `json:"title"`
	URL   string `json:"url"`
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

// validateEmbedWidget guards the one widget whose content is operator-supplied.
// The frontend's isValidEmbedUrl in lib/dashboard-widgets.ts applies the same
// rules for synchronous feedback in the config dialog; keep the two in step.
func validateEmbedWidget(w dashboardLayoutItem) string {
	token := strings.TrimPrefix(w.ID, embedWidgetIDPrefix)
	if !embedWidgetTokenPattern.MatchString(token) {
		return "invalid embed widget id: " + w.ID
	}
	if w.Embed == nil {
		return "embed widget requires embed config: " + w.ID
	}
	if len(w.Embed.URL) > embedURLMaxLen {
		return "embed url is too long"
	}
	if len(w.Embed.Title) > embedTitleMaxLen {
		return "embed title is too long"
	}

	parsed, err := url.Parse(strings.TrimSpace(w.Embed.URL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "embed url must be an http(s) URL"
	}
	return ""
}

func validateDashboardWidgets(widgets []dashboardLayoutItem) string {
	seen := make(map[string]bool, len(widgets))
	for _, w := range widgets {
		if strings.HasPrefix(w.ID, embedWidgetIDPrefix) {
			if msg := validateEmbedWidget(w); msg != "" {
				return msg
			}
		} else if strings.HasPrefix(w.ID, gaugeWidgetIDPrefix) {
			if msg := validateGaugeWidget(w); msg != "" {
				return msg
			}
		} else {
			if !validDashboardWidgetIDs[w.ID] {
				return "unknown widget id: " + w.ID
			}
			// Reject rather than silently drop: config the renderer will never
			// read means the caller has misunderstood the model.
			if w.Embed != nil {
				return "embed config not allowed on widget id: " + w.ID
			}
			if w.Gauge != nil {
				return "gauge config not allowed on widget id: " + w.ID
			}
		}
		// Embed tokens are unique per instance, so the duplicate check below
		// covers both widget kinds unchanged.
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

// validateGaugeWidget guards the second operator-configured widget. It mirrors
// validateEmbedWidget's reject-rather-than-drop stance: config the renderer
// would never read means the caller has misunderstood the model.
func validateGaugeWidget(w dashboardLayoutItem) string {
	token := strings.TrimPrefix(w.ID, gaugeWidgetIDPrefix)
	if !embedWidgetTokenPattern.MatchString(token) {
		return "invalid gauge widget id: " + w.ID
	}
	if w.Embed != nil {
		return "embed config not allowed on gauge widget: " + w.ID
	}
	if w.Gauge == nil {
		return "gauge widget requires gauge config: " + w.ID
	}

	path := strings.TrimSpace(w.Gauge.Path)
	if path == "" {
		return "gauge widget requires a SignalK path: " + w.ID
	}
	if len(path) > gaugePathMaxLen {
		return "gauge path too long: " + w.ID
	}
	if len(w.Gauge.Label) > gaugeLabelMaxLen {
		return "gauge label too long: " + w.ID
	}
	if !validGaugeDisplays[w.Gauge.Display] {
		return "unknown gauge display: " + w.Gauge.Display
	}
	if w.Gauge.Min != nil && w.Gauge.Max != nil && *w.Gauge.Min >= *w.Gauge.Max {
		return "gauge min must be below max: " + w.ID
	}
	for _, zone := range w.Gauge.Zones {
		if _, ok := alarmStateRank[zone.State]; !ok {
			return "unknown gauge zone state: " + zone.State
		}
	}
	return ""
}

// gaugeBoundPaths returns every SignalK path any gauge on any page is bound to.
// The stream uses it to push exactly those values and no more — the backend
// already owns the page config, so no subscription protocol is needed.
func gaugeBoundPaths() []string {
	dashboardPagesMu.RLock()
	defer dashboardPagesMu.RUnlock()

	seen := map[string]bool{}
	var paths []string
	for _, page := range dashboardPagesState {
		for _, widget := range page.Widgets {
			if widget.Gauge == nil {
				continue
			}
			path := strings.TrimSpace(widget.Gauge.Path)
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths
}
