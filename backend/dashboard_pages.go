package main

import (
	"encoding/json"
	"fmt"
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
	"tanks":          true,
	"route":          true,
	"nearby-vessels": true,
	"battery-power":  true,
	"solar":          true,
	"alternator":     true,
	"generator":      true,
	"czone-switches": true,
	"hot-water":      true,
	"autopilot":      true,
}

// Widget ids that existed in a previous release and have been retired. Saved
// pages are stripped of them on load and rewritten, so a stale id can never
// fail validateDashboardWidgets on the next PATCH. See ADR 0047.
var retiredDashboardWidgetIDs = map[string]bool{"rode-scope": true}

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
	"numeric": true, "radial": true, "bar": true, "lamp": true, "trend": true,
}

var validGaugeRingStyles = map[string]bool{"": true, "plain": true, "instrument": true}
var validGaugeReadouts = map[string]bool{"": true, "below": true, "inside": true}

// Gauge groups (ADR 0049) are the third multi-instance widget type. A group is
// a named cluster of gauges in one tile — "Port" holding RPM, oil pressure and
// exhaust temperature — built once and then duplicated for the other engine.
const gaugeGroupWidgetIDPrefix = "gauge-group:"

const (
	gaugeGroupTitleMaxLen = 48
	gaugeGroupMaxGauges   = 12
	gaugeGroupMaxColumns  = 4
)

// dashboardGaugeGroupConfig reuses dashboardGaugeConfig unchanged, so the
// per-gauge validation and the bound-path walker are shared, not forked.
// Lamp strips (ADR 0052) are the fourth multi-instance widget: a dense row of
// indicator lamps plus an optional CHK rollup, pinned to each page the way
// N2KView's indicator ribbon is repeated across every display.
const lampStripWidgetIDPrefix = "lamps:"

const (
	lampStripMaxLamps    = 16
	lampStripLabelMaxLen = 12
)

type dashboardLamp struct {
	Path  string `json:"path"`
	Label string `json:"label"`
	// Invert lights the lamp when the value is zero or absent, for a signal
	// whose healthy state is "off" (a bilge float, a fault line).
	Invert bool `json:"invert,omitempty"`
}

type dashboardLampStripConfig struct {
	Title string          `json:"title"`
	Lamps []dashboardLamp `json:"lamps"`
	// ShowCheck appends the CHK rollup, coloured by the worst active alarm.
	ShowCheck bool `json:"showCheck,omitempty"`
}

// Engine clusters (ADR 0054) are the fifth multi-instance widget: a ticked ring
// with the primary reading inside it and mask-cut cards in the corners, the
// layout wind-tile.tsx established and a marine MFD uses.
const clusterWidgetIDPrefix = "cluster:"

const (
	clusterMaxCorners    = 4
	clusterMaxCornerRows = 4
	clusterMaxTelltales  = 6
)

var validPageSkins = map[string]bool{"": true, "default": true, "instrument": true}

// A closed set, mirroring CLUSTER_ICONS in frontend/src/lib/cluster-icons.ts.
// An allowlist rather than any lucide name, so a saved config can never point
// at a component that does not exist.
var validClusterIcons = map[string]bool{
	"":            true,
	"thermometer": true, "cog": true, "rabbit": true, "clock": true,
	"fuel": true, "gauge": true, "zap": true, "waves": true, "activity": true,
}

// dashboardClusterCorner is one corner card. Rows let a single card stack
// several readings, which is how the temperatures share one box.
type dashboardClusterCorner struct {
	Label string                 `json:"label"`
	Icon  string                 `json:"icon,omitempty"`
	Rows  []dashboardGaugeConfig `json:"rows"`
}

// dashboardClusterConfig reuses dashboardGaugeConfig for every slot, the same
// decision that made gauge groups cheap: zones, units and scales all work per
// slot with no new machinery.
type dashboardClusterConfig struct {
	Title      string                   `json:"title"`
	Ring       dashboardGaugeConfig     `json:"ring"`
	Centre     dashboardGaugeConfig     `json:"centre"`
	CentreIcon string                   `json:"centreIcon,omitempty"`
	Corners    []dashboardClusterCorner `json:"corners,omitempty"`
	// Readings shown as a telltale under the dial rather than a box of their
	// own: icon, label, value, coloured by zone.
	Telltales []dashboardGaugeConfig `json:"telltales,omitempty"`
}

type dashboardGaugeGroupConfig struct {
	Title   string                 `json:"title"`
	Columns *int                   `json:"columns,omitempty"`
	Gauges  []dashboardGaugeConfig `json:"gauges"`
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
	// Present only on `gauge-group:` widgets; rejected on any other id.
	GaugeGroup *dashboardGaugeGroupConfig `json:"gaugeGroup,omitempty"`
	// Present only on `lamps:` widgets; rejected on any other id.
	Lamps *dashboardLampStripConfig `json:"lamps,omitempty"`
	// Present only on `cluster:` widgets; rejected on any other id.
	Cluster *dashboardClusterConfig `json:"cluster,omitempty"`
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

	// Every field the renderer reads has to live here. Go's JSON decoder drops
	// unknown keys without complaint, so one missing from this struct is
	// silently discarded on save and the widget quietly renders its default.
	//
	// Window is the trend history window (ADR 0051); the rest shape an
	// instrument ring (ADR 0054).
	Window       string   `json:"window,omitempty"`
	RingStyle    string   `json:"ringStyle,omitempty"`
	Readout      string   `json:"readout,omitempty"`
	LabelDivisor *float64 `json:"labelDivisor,omitempty"`
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
	ID   string `json:"id"`
	Name string `json:"name"`
	// Selects the token set every widget on this page renders against: the
	// app theme, or the always-dark instrument skin. Was a per-cluster-widget
	// setting until ADR 0060 moved it here, since a page is already a mode
	// ("Anchored", "Underway") and the skin is a property of that, not of one
	// tile. Empty means the app theme, so an unset page needs no value.
	Skin      string                `json:"skin,omitempty"`
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
	if w.GaugeGroup != nil {
		return "gauge group config not allowed on embed widget: " + w.ID
	}
	if w.Lamps != nil {
		return "lamp config not allowed on embed widget: " + w.ID
	}
	if w.Cluster != nil {
		return "cluster config not allowed on embed widget: " + w.ID
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
		} else if strings.HasPrefix(w.ID, clusterWidgetIDPrefix) {
			if msg := validateClusterWidget(w); msg != "" {
				return msg
			}
		} else if strings.HasPrefix(w.ID, lampStripWidgetIDPrefix) {
			if msg := validateLampStripWidget(w); msg != "" {
				return msg
			}
		} else if strings.HasPrefix(w.ID, gaugeGroupWidgetIDPrefix) {
			if msg := validateGaugeGroupWidget(w); msg != "" {
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
			if w.GaugeGroup != nil {
				return "gauge group config not allowed on widget id: " + w.ID
			}
			if w.Lamps != nil {
				return "lamp config not allowed on widget id: " + w.ID
			}
			if w.Cluster != nil {
				return "cluster config not allowed on widget id: " + w.ID
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

// stripRetiredWidgets removes any widget whose id is in retiredDashboardWidgetIDs,
// reporting whether anything was actually removed so the caller only rewrites
// the file when there's a real change to persist.
func stripRetiredWidgets(widgets []dashboardLayoutItem) ([]dashboardLayoutItem, bool) {
	changed := false
	kept := make([]dashboardLayoutItem, 0, len(widgets))
	for _, w := range widgets {
		if retiredDashboardWidgetIDs[w.ID] {
			changed = true
			continue
		}
		kept = append(kept, w)
	}
	return kept, changed
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
			anyStripped := false
			for _, p := range loaded.Pages {
				if kept, changed := stripRetiredWidgets(p.Widgets); changed {
					p.Widgets = kept
					anyStripped = true
				}
				dashboardPagesState[p.ID] = p
			}
			if anyStripped {
				if err := saveDashboardPagesLocked(); err != nil {
					log.Printf("Failed to persist dashboard pages after stripping retired widget ids: %v", err)
				}
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
		Skin    string                `json:"skin"`
		Widgets []dashboardLayoutItem `json:"widgets"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "name is required"})
	}
	if !validPageSkins[body.Skin] {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "unknown page skin: " + body.Skin})
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
		Skin:      body.Skin,
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
		Skin    *string                `json:"skin"`
		Widgets *[]dashboardLayoutItem `json:"widgets"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if body.Name == nil && body.Skin == nil && body.Widgets == nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "no patch fields provided"})
	}
	if body.Name != nil && strings.TrimSpace(*body.Name) == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "name cannot be empty"})
	}
	if body.Skin != nil && !validPageSkins[*body.Skin] {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "unknown page skin: " + *body.Skin})
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
		Skin:      current.Skin,
		Widgets:   current.Widgets,
		CreatedAt: current.CreatedAt,
		UpdatedAt: time.Now().UTC(),
	}
	if body.Name != nil {
		updated.Name = strings.TrimSpace(*body.Name)
	}
	if body.Skin != nil {
		updated.Skin = *body.Skin
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
	if w.GaugeGroup != nil {
		return "gauge group config not allowed on gauge widget: " + w.ID
	}
	if w.Lamps != nil {
		return "lamp config not allowed on gauge widget: " + w.ID
	}
	if w.Cluster != nil {
		return "cluster config not allowed on gauge widget: " + w.ID
	}
	if w.Gauge == nil {
		return "gauge widget requires gauge config: " + w.ID
	}
	return validateGaugeConfig(*w.Gauge, w.ID)
}

// validateGaugeConfig holds the rules for a single bound gauge, whether it is a
// standalone `gauge:` widget or one member of a `gauge-group:` cluster. ctx
// names the offending widget so a rejection points somewhere useful.
func validateGaugeConfig(g dashboardGaugeConfig, ctx string) string {
	path := strings.TrimSpace(g.Path)
	if path == "" {
		return "gauge widget requires a SignalK path: " + ctx
	}
	if len(path) > gaugePathMaxLen {
		return "gauge path too long: " + ctx
	}
	if len(g.Label) > gaugeLabelMaxLen {
		return "gauge label too long: " + ctx
	}
	if !validGaugeDisplays[g.Display] {
		return "unknown gauge display: " + g.Display
	}
	if g.Min != nil && g.Max != nil && *g.Min >= *g.Max {
		return "gauge min must be below max: " + ctx
	}
	if !validGaugeRingStyles[g.RingStyle] {
		return "unknown gauge ring style: " + g.RingStyle
	}
	if !validGaugeReadouts[g.Readout] {
		return "unknown gauge readout: " + g.Readout
	}
	if g.LabelDivisor != nil && *g.LabelDivisor <= 0 {
		return "gauge label divisor must be positive: " + ctx
	}
	if g.Window != "" && !validTelemetryHistoryWindow(g.Window) {
		return "unknown gauge history window: " + g.Window
	}
	for i, zone := range g.Zones {
		if _, ok := alarmStateRank[zone.State]; !ok {
			return "unknown gauge zone state: " + zone.State
		}
		if zone.State == alarmStateNormal {
			continue
		}
		// A zone is the alarm (ADR 0050), so a band that cannot become a
		// threshold is rejected here rather than silently never firing.
		if _, err := gaugeZoneRule("validate", g, zone, i); err != nil {
			return fmt.Sprintf("gauge zone %d cannot raise an alarm (%v): %s", i+1, err, ctx)
		}
	}
	return ""
}

// validateClusterWidget guards the fifth operator-configured widget.
func validateClusterWidget(w dashboardLayoutItem) string {
	token := strings.TrimPrefix(w.ID, clusterWidgetIDPrefix)
	if !embedWidgetTokenPattern.MatchString(token) {
		return "invalid cluster widget id: " + w.ID
	}
	if w.Embed != nil || w.Gauge != nil || w.GaugeGroup != nil || w.Lamps != nil {
		return "only cluster config is allowed on a cluster widget: " + w.ID
	}
	if w.Cluster == nil {
		return "cluster widget requires cluster config: " + w.ID
	}
	if len(w.Cluster.Title) > gaugeGroupTitleMaxLen {
		return "cluster title too long: " + w.ID
	}
	if !validClusterIcons[w.Cluster.CentreIcon] {
		return "unknown cluster icon: " + w.Cluster.CentreIcon
	}
	if msg := validateGaugeConfig(w.Cluster.Ring, w.ID+" ring"); msg != "" {
		return msg
	}
	if msg := validateGaugeConfig(w.Cluster.Centre, w.ID+" centre"); msg != "" {
		return msg
	}
	if len(w.Cluster.Corners) > clusterMaxCorners {
		return "cluster has too many corners: " + w.ID
	}
	for i, corner := range w.Cluster.Corners {
		if !validClusterIcons[corner.Icon] {
			return "unknown cluster icon: " + corner.Icon
		}
		if len(corner.Rows) == 0 {
			return fmt.Sprintf("cluster corner %d has no rows: %s", i+1, w.ID)
		}
		if len(corner.Rows) > clusterMaxCornerRows {
			return fmt.Sprintf("cluster corner %d has too many rows: %s", i+1, w.ID)
		}
		for _, row := range corner.Rows {
			if msg := validateGaugeConfig(row, w.ID); msg != "" {
				return msg
			}
		}
	}
	if len(w.Cluster.Telltales) > clusterMaxTelltales {
		return "cluster has too many telltales: " + w.ID
	}
	for _, telltale := range w.Cluster.Telltales {
		if msg := validateGaugeConfig(telltale, w.ID+" telltale"); msg != "" {
			return msg
		}
	}
	return ""
}

// validateLampStripWidget guards the fourth operator-configured widget.
//
// A strip with no lamps but ShowCheck set is fine — the rollup alone is a
// useful thing to pin to a page — but a strip with neither renders nothing at
// all, which is a mistake rather than a choice.
func validateLampStripWidget(w dashboardLayoutItem) string {
	token := strings.TrimPrefix(w.ID, lampStripWidgetIDPrefix)
	if !embedWidgetTokenPattern.MatchString(token) {
		return "invalid lamp strip widget id: " + w.ID
	}
	if w.Embed != nil || w.Gauge != nil || w.GaugeGroup != nil || w.Cluster != nil {
		return "only lamp config is allowed on a lamp strip widget: " + w.ID
	}
	if w.Lamps == nil {
		return "lamp strip widget requires lamps config: " + w.ID
	}
	if len(w.Lamps.Title) > gaugeGroupTitleMaxLen {
		return "lamp strip title too long: " + w.ID
	}
	if len(w.Lamps.Lamps) == 0 && !w.Lamps.ShowCheck {
		return "lamp strip needs at least one lamp or the check indicator: " + w.ID
	}
	if len(w.Lamps.Lamps) > lampStripMaxLamps {
		return "lamp strip has too many lamps: " + w.ID
	}
	for _, lamp := range w.Lamps.Lamps {
		path := strings.TrimSpace(lamp.Path)
		if path == "" {
			return "lamp requires a SignalK path: " + w.ID
		}
		if len(path) > gaugePathMaxLen {
			return "lamp path too long: " + w.ID
		}
		if len(lamp.Label) > lampStripLabelMaxLen {
			return "lamp label too long: " + w.ID
		}
	}
	return ""
}

// validateGaugeGroupWidget guards the third operator-configured widget. Members
// go through validateGaugeConfig, so a group can never hold a gauge a
// standalone widget would have rejected.
//
// Duplicate paths within a group are deliberately allowed: the same value shown
// twice in two units is a legitimate thing to want.
func validateGaugeGroupWidget(w dashboardLayoutItem) string {
	token := strings.TrimPrefix(w.ID, gaugeGroupWidgetIDPrefix)
	if !embedWidgetTokenPattern.MatchString(token) {
		return "invalid gauge group widget id: " + w.ID
	}
	if w.Embed != nil {
		return "embed config not allowed on gauge group widget: " + w.ID
	}
	if w.Gauge != nil {
		return "gauge config not allowed on gauge group widget: " + w.ID
	}
	if w.Lamps != nil {
		return "lamp config not allowed on gauge group widget: " + w.ID
	}
	if w.Cluster != nil {
		return "cluster config not allowed on gauge group widget: " + w.ID
	}
	if w.GaugeGroup == nil {
		return "gauge group widget requires gaugeGroup config: " + w.ID
	}
	if len(w.GaugeGroup.Title) > gaugeGroupTitleMaxLen {
		return "gauge group title too long: " + w.ID
	}
	if len(w.GaugeGroup.Gauges) == 0 {
		return "gauge group requires at least one gauge: " + w.ID
	}
	if len(w.GaugeGroup.Gauges) > gaugeGroupMaxGauges {
		return "gauge group has too many gauges: " + w.ID
	}
	if w.GaugeGroup.Columns != nil && (*w.GaugeGroup.Columns < 1 || *w.GaugeGroup.Columns > gaugeGroupMaxColumns) {
		return "gauge group columns out of range: " + w.ID
	}
	for _, g := range w.GaugeGroup.Gauges {
		if msg := validateGaugeConfig(g, w.ID); msg != "" {
			return msg
		}
	}
	return ""
}

// gaugeBoundPaths returns every SignalK path any gauge on any page is bound to,
// standalone gauges and gauge-group members alike.
// The stream uses it to push exactly those values and no more — the backend
// already owns the page config, so no subscription protocol is needed.
func gaugeBoundPaths() []string {
	dashboardPagesMu.RLock()
	defer dashboardPagesMu.RUnlock()

	seen := map[string]bool{}
	var paths []string
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		paths = append(paths, path)
	}

	for _, page := range dashboardPagesState {
		for _, widget := range page.Widgets {
			if widget.Gauge != nil {
				add(widget.Gauge.Path)
			}
			// A group's members are bound paths too. Miss them and every gauge
			// in every group renders the structural dash forever, silently.
			if widget.GaugeGroup != nil {
				for _, g := range widget.GaugeGroup.Gauges {
					add(g.Path)
				}
			}
			// Every widget that binds a path must register here. This walker
			// is the single point at which a bound path becomes a pushed
			// value; omitting a widget kind leaves it permanently blank with
			// nothing in any log to say why.
			if widget.Lamps != nil {
				for _, lamp := range widget.Lamps.Lamps {
					add(lamp.Path)
				}
			}
			if widget.Cluster != nil {
				add(widget.Cluster.Ring.Path)
				add(widget.Cluster.Centre.Path)
				for _, corner := range widget.Cluster.Corners {
					for _, row := range corner.Rows {
						add(row.Path)
					}
				}
				for _, telltale := range widget.Cluster.Telltales {
					add(telltale.Path)
				}
			}
		}
	}
	sort.Strings(paths)
	return paths
}
