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
	"vessel":             true,
	"wind":               true,
	"depth-tide":         true,
	"position":           true,
	"today-now":          true,
	"anchor-watch":       true,
	"tanks":              true,
	"route":              true,
	"nearby-vessels":     true,
	"radar-targets":      true,
	"battery-power":      true,
	"solar":              true,
	"alternator":         true,
	"generator":          true,
	"czone-switches":     true,
	"hot-water":          true,
	"autopilot":          true,
	"clock":              true,
	"current-conditions": true,
	"forecast-days":      true,
	"sea-state":          true,
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
	clusterMaxFuelBars   = 4
)

// Which edge of the tile the fuel rail sits on, not which side of the boat the
// tanks are. A starboard cluster laid out on the left of a page wants its rail
// on the left; the tanks' side is carried by their paths and the tile's title.
var validClusterFuelSides = map[string]bool{"left": true, "right": true}

var validPageSkins = map[string]bool{"": true, "default": true, "instrument": true}

// Kiosk fields (ADR 0089) turn an ordinary page into one that can appear in
// the wall-display rotation at /kiosk: a bool flag, how long it shows, and
// an optional condition. kioskSecondsMin/Max bound the duration to something
// a human will actually notice cycle; 5s is fast but visible, 3600s (an
// hour) is long enough that "kiosk" without a duration would just mean
// "forever" by another name, which is what the zero value already means.
const (
	kioskSecondsMin = 5
	kioskSecondsMax = 3600
)

// Empty means "always" - an unset page needs no value, matching skin and
// hero's own empty-string-means-default convention.
var validKioskWhen = map[string]bool{"": true, "always": true, "anchored": true}

// validateKioskFields holds the rules for the three kiosk fields together,
// shared by create (an all-at-once body) and patch (a field-by-field
// rebuild, see patchDashboardPageHandler) so both fail closed the same way.
//
// seconds is validated whenever it is non-zero, independent of the kiosk
// flag: a stored duration that survives an untick (see patch's carry-forward
// comment) must already have been a valid one, and a caller setting seconds
// without also setting kiosk in the same request should not be able to smuggle
// in a value that would be rejected the moment kiosk actually flips on.
func validateKioskFields(kiosk bool, seconds int, when string) string {
	if !validKioskWhen[when] {
		return "unknown kiosk condition: " + when
	}
	if seconds != 0 && (seconds < kioskSecondsMin || seconds > kioskSecondsMax) {
		return fmt.Sprintf("kiosk_seconds must be between %d and %d", kioskSecondsMin, kioskSecondsMax)
	}
	if kiosk && seconds == 0 {
		return "kiosk requires kiosk_seconds"
	}
	return ""
}

// heroWidgetExists reports whether hero is unset, or names a widget actually
// present in widgets. Shared by create and patch so both fail closed the same
// way (ADR 0072, following the precedent ADR 0060 set for skin).
func heroWidgetExists(hero string, widgets []dashboardLayoutItem) bool {
	if hero == "" {
		return true
	}
	for _, w := range widgets {
		if w.ID == hero {
			return true
		}
	}
	return false
}

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
	// A fuel rail down one edge (ADR 0061). Every field the renderer reads has
	// to reach the struct below: the decoder drops unknown keys in silence, so
	// one left out is discarded on save and the rail renders its default.
	Fuel *dashboardClusterFuelRail `json:"fuel,omitempty"`
}

// dashboardClusterFuelBar is one tank. Two gauge slots rather than one because
// litres needs two readings: a 0..1 ratio and a capacity in m3.
type dashboardClusterFuelBar struct {
	Level    dashboardGaugeConfig `json:"level"`
	Capacity dashboardGaugeConfig `json:"capacity"`
}

// dashboardClusterFuelRail is the curved bar gauge down one edge of the tile
// (ADR 0061).
type dashboardClusterFuelRail struct {
	Side       string                    `json:"side"`
	Bars       []dashboardClusterFuelBar `json:"bars"`
	TotalLabel string                    `json:"totalLabel,omitempty"`
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
	// Frameless drops the Tile title bar and padding so the embed fills the
	// widget, for wall-display strips where chrome is wasted space. It is
	// ignored by the frontend while the dashboard is in layout-editing mode,
	// since editing still needs the gear icon and title reachable.
	// omitempty keeps existing dashboard-pages.json files byte-identical.
	Frameless bool `json:"frameless,omitempty"`
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
	// H:10 fits the always-on map at full height plus the rode readout and
	// Drop/Raise button (ADR 0059); at H:8 the tile's map has to shrink.
	{ID: "anchor-watch", X: 4, Y: 3, W: 4, H: 10},
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
	// Shared navigation order. Older files omit this field (zero); creation
	// time and ID break ties until load normalizes the positions.
	Position int `json:"position"`
	// Selects the token set every widget on this page renders against: the
	// app theme, or the always-dark instrument skin. Was a per-cluster-widget
	// setting until ADR 0060 moved it here, since a page is already a mode
	// ("Anchored", "Underway") and the skin is a property of that, not of one
	// tile. Empty means the app theme, so an unset page needs no value.
	Skin string `json:"skin,omitempty"`
	// Hero names the one widget on this page that gets the enlarged,
	// full-width treatment (design critique batch, ADR 0072). Empty means no
	// hero. A dangling hero can never persist: an explicit patch that names a
	// widget not on the page is rejected the same way an unknown skin is, and
	// a widgets-only patch that removes the hero's own widget clears it
	// automatically rather than failing an otherwise-ordinary widget removal
	// (see patchDashboardPageHandler).
	Hero string `json:"hero,omitempty"`
	// Kiosk fields (ADR 0089): Kiosk marks this page as part of the wall
	// display rotation at /kiosk; KioskSeconds is how long it shows before
	// the rotation advances; KioskWhen is an optional condition ("anchored"
	// restricts the page to while the anchor watch is active, empty means
	// always). omitempty on all three keeps existing files byte-identical -
	// an unflagged page never gains any of these keys just because the
	// struct grew them.
	Kiosk        bool                  `json:"kiosk,omitempty"`
	KioskSeconds int                   `json:"kiosk_seconds,omitempty"`
	KioskWhen    string                `json:"kiosk_when,omitempty"`
	Widgets      []dashboardLayoutItem `json:"widgets"`
	CreatedAt    time.Time             `json:"created_at"`
	UpdatedAt    time.Time             `json:"updated_at"`
}

type dashboardPagesFile struct {
	Pages []*dashboardPageData `json:"pages"`
	// Ribbon is the one vessel-level indicator ribbon (ADR 0082): a lamp
	// strip promoted out of the per-page widget so it renders identically on
	// every page instead of being copied by hand onto each one. Nil when the
	// operator has not pinned one. omitempty keeps a file with no ribbon
	// byte-identical to one written before this field existed.
	Ribbon *dashboardLampStripConfig `json:"ribbon,omitempty"`
}

var (
	dashboardPagesMu    sync.RWMutex
	dashboardPagesState map[string]*dashboardPageData
	// dashboardRibbonState is guarded by dashboardPagesMu, the same lock that
	// guards dashboardPagesState — it lives in the same file and every writer
	// of that file must carry it forward (see saveDashboardPagesLocked and
	// reorderDashboardPagesHandler, the two places that actually touch disk).
	dashboardRibbonState *dashboardLampStripConfig
)

func dashboardPagesFilePath() string {
	return cacheFilePath("DASHBOARD_PAGES_FILE", "data/dashboard-pages.json")
}

func orderedDashboardPagesLocked() []*dashboardPageData {
	list := make([]*dashboardPageData, 0, len(dashboardPagesState))
	for _, p := range dashboardPagesState {
		list = append(list, p)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Position != list[j].Position {
			return list[i].Position < list[j].Position
		}
		if list[i].CreatedAt.Equal(list[j].CreatedAt) {
			return list[i].ID < list[j].ID
		}
		return list[i].CreatedAt.Before(list[j].CreatedAt)
	})
	return list
}

func saveDashboardPagesLocked() error {
	return writeJSONFileAtomic(dashboardPagesFilePath(), dashboardPagesFile{
		Pages:  orderedDashboardPagesLocked(),
		Ribbon: dashboardRibbonState,
	})
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
	dashboardRibbonState = nil

	// Try loading new format first
	data, err := os.ReadFile(dashboardPagesFilePath())
	if err == nil {
		// New file exists, try to parse it
		var loaded dashboardPagesFile
		if err := json.Unmarshal(data, &loaded); err == nil {
			dashboardRibbonState = loaded.Ribbon
			anyStripped := false
			for _, p := range loaded.Pages {
				if kept, changed := stripRetiredWidgets(p.Widgets); changed {
					p.Widgets = kept
					anyStripped = true
				}
				dashboardPagesState[p.ID] = p
			}
			// Normalize legacy creation order and gaps left by page deletion.
			// Existing explicit positions take precedence over creation time.
			orderChanged := false
			for i, p := range orderedDashboardPagesLocked() {
				if p.Position != i {
					p.Position = i
					orderChanged = true
				}
			}
			if anyStripped || orderChanged {
				if err := saveDashboardPagesLocked(); err != nil {
					log.Printf("Failed to persist migrated dashboard pages: %v", err)
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

	return c.JSON(http.StatusOK, dashboardPagesFile{Pages: orderedDashboardPagesLocked()})
}

// PUT /api/dashboard-pages/order replaces the complete shared order. Requiring
// every current ID rejects stale lists after a concurrent create or delete.
func reorderDashboardPagesHandler(c echo.Context) error {
	var body struct {
		PageIDs []string `json:"page_ids"`
	}
	if err := c.Bind(&body); err != nil || body.PageIDs == nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "page_ids must be an array of every current page ID"})
	}

	dashboardPagesMu.Lock()
	defer dashboardPagesMu.Unlock()
	if len(body.PageIDs) != len(dashboardPagesState) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "page_ids must include every current page exactly once; reload the page list and try again"})
	}
	seen := make(map[string]bool, len(body.PageIDs))
	list := make([]*dashboardPageData, 0, len(body.PageIDs))
	now := time.Now().UTC()
	for i, id := range body.PageIDs {
		page, ok := dashboardPagesState[id]
		if !ok || seen[id] {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "unknown or duplicate page ID: " + id + "; reload the page list and try again"})
		}
		seen[id] = true
		updated := *page
		if updated.Position != i {
			updated.Position = i
			updated.UpdatedAt = now
		}
		list = append(list, &updated)
	}

	// Publish only after the entire order is durable. A failed write must not
	// leave readers seeing an order that will vanish at the next restart.
	// Ribbon rides along explicitly: this handler writes the pages file
	// directly rather than through saveDashboardPagesLocked, so it is exactly
	// the "other code path" that has to remember to carry the ribbon forward
	// or silently drop it from disk on the next restart.
	if err := writeJSONFileAtomic(dashboardPagesFilePath(), dashboardPagesFile{Pages: list, Ribbon: dashboardRibbonState}); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to persist page order"})
	}
	for _, page := range list {
		dashboardPagesState[page.ID] = page
	}
	return c.JSON(http.StatusOK, dashboardPagesFile{Pages: list})
}

// POST /api/dashboard-pages
func createDashboardPageHandler(c echo.Context) error {
	var body struct {
		Name         string                `json:"name"`
		Skin         string                `json:"skin"`
		Hero         string                `json:"hero"`
		Kiosk        bool                  `json:"kiosk"`
		KioskSeconds int                   `json:"kiosk_seconds"`
		KioskWhen    string                `json:"kiosk_when"`
		Widgets      []dashboardLayoutItem `json:"widgets"`
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
	if msg := validateKioskFields(body.Kiosk, body.KioskSeconds, body.KioskWhen); msg != "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": msg})
	}

	// Handle nil slice: ensure it's an empty slice for consistency
	if body.Widgets == nil {
		body.Widgets = []dashboardLayoutItem{}
	}

	if msg := validateDashboardWidgets(body.Widgets); msg != "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": msg})
	}

	if !heroWidgetExists(body.Hero, body.Widgets) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "hero must name a widget on this page: " + body.Hero})
	}

	now := time.Now().UTC()
	page := &dashboardPageData{
		ID:           uuid.NewString(),
		Name:         name,
		Skin:         body.Skin,
		Hero:         body.Hero,
		Kiosk:        body.Kiosk,
		KioskSeconds: body.KioskSeconds,
		KioskWhen:    body.KioskWhen,
		Widgets:      body.Widgets,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	dashboardPagesMu.Lock()
	for _, existing := range dashboardPagesState {
		if existing.Position >= page.Position {
			page.Position = existing.Position + 1
		}
	}
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
		Name         *string                `json:"name"`
		Skin         *string                `json:"skin"`
		Hero         *string                `json:"hero"`
		Kiosk        *bool                  `json:"kiosk"`
		KioskSeconds *int                   `json:"kiosk_seconds"`
		KioskWhen    *string                `json:"kiosk_when"`
		Widgets      *[]dashboardLayoutItem `json:"widgets"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if body.Name == nil && body.Skin == nil && body.Hero == nil && body.Widgets == nil &&
		body.Kiosk == nil && body.KioskSeconds == nil && body.KioskWhen == nil {
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

	finalWidgets := current.Widgets
	if body.Widgets != nil {
		finalWidgets = *body.Widgets
	}
	// An explicit hero patch is validated against the widget set it will land
	// on, fail closed exactly like an unknown skin.
	if body.Hero != nil && !heroWidgetExists(*body.Hero, finalWidgets) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "hero must name a widget on this page: " + *body.Hero})
	}

	updated := &dashboardPageData{
		ID:           current.ID,
		Name:         current.Name,
		Position:     current.Position,
		Skin:         current.Skin,
		Hero:         current.Hero,
		Kiosk:        current.Kiosk,
		KioskSeconds: current.KioskSeconds,
		KioskWhen:    current.KioskWhen,
		Widgets:      current.Widgets,
		CreatedAt:    current.CreatedAt,
		UpdatedAt:    time.Now().UTC(),
	}
	if body.Name != nil {
		updated.Name = strings.TrimSpace(*body.Name)
	}
	if body.Skin != nil {
		updated.Skin = *body.Skin
	}
	if body.Hero != nil {
		updated.Hero = *body.Hero
	}
	if body.Kiosk != nil {
		updated.Kiosk = *body.Kiosk
	}
	if body.KioskSeconds != nil {
		updated.KioskSeconds = *body.KioskSeconds
	}
	if body.KioskWhen != nil {
		updated.KioskWhen = *body.KioskWhen
	}
	if body.Widgets != nil {
		updated.Widgets = *body.Widgets
	}
	// Validated on the merged triple, not the raw patch fields: {"kiosk":true}
	// alone must succeed when seconds are already on record from an earlier
	// save, and {"kiosk":false} alone must leave seconds and condition alone
	// so re-ticking the box remembers them (see the carry-forward above) -
	// neither of those is visible from body.Kiosk/body.KioskSeconds/body.KioskWhen
	// in isolation.
	if msg := validateKioskFields(updated.Kiosk, updated.KioskSeconds, updated.KioskWhen); msg != "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": msg})
	}
	// A hero not named explicitly in this patch can still be orphaned by it,
	// e.g. a widgets-only patch that drops the widget that was the hero (this
	// is exactly what the X button's remove-widget patch sends). Rather than
	// rejecting an ordinary widget removal because of an unrelated field, the
	// stale reference is repaired here - the same stance stripRetiredWidgets
	// takes on load: a dangling reference is corrected on write, not treated
	// as the caller's error.
	if body.Hero == nil && !heroWidgetExists(updated.Hero, updated.Widgets) {
		updated.Hero = ""
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

// GET /api/dashboard-ribbon
func getDashboardRibbonHandler(c echo.Context) error {
	dashboardPagesMu.RLock()
	defer dashboardPagesMu.RUnlock()
	return c.JSON(http.StatusOK, map[string]*dashboardLampStripConfig{"ribbon": dashboardRibbonState})
}

// PUT /api/dashboard-ribbon sets or clears the one vessel-level indicator
// ribbon (ADR 0082). A null ribbon clears it; anything else is validated with
// the same rules a per-page lamps: widget uses.
func putDashboardRibbonHandler(c echo.Context) error {
	var body struct {
		Ribbon *dashboardLampStripConfig `json:"ribbon"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if body.Ribbon != nil {
		if msg := validateLampStripConfig(body.Ribbon, "ribbon"); msg != "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": msg})
		}
	}

	dashboardPagesMu.Lock()
	defer dashboardPagesMu.Unlock()

	// Publish only after the write is durable, the same stance
	// reorderDashboardPagesHandler takes on the page order: a failed write
	// must not leave memory holding a ribbon that will vanish at the next
	// restart.
	if err := writeJSONFileAtomic(dashboardPagesFilePath(), dashboardPagesFile{
		Pages:  orderedDashboardPagesLocked(),
		Ribbon: body.Ribbon,
	}); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to persist"})
	}
	dashboardRibbonState = body.Ribbon
	return c.JSON(http.StatusOK, map[string]*dashboardLampStripConfig{"ribbon": dashboardRibbonState})
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
	if msg := validateClusterFuelRail(w.Cluster.Fuel, w.ID); msg != "" {
		return msg
	}
	return ""
}

/*
validateClusterFuelRail guards the fuel rail (ADR 0061).

The quantity rules are the load-bearing part. Not one tank path on this vessel
publishes meta.units, so the config dialog's path picker has nothing to infer
from and preselects Unitless. Left that way convertFromSI is the identity and
the rail reads 1.2 where it should read 890. A fuel figure that is wrong but
plausible is worse on a helm than no figure at all, so rather than guessing a
unit this refuses the save.
*/
func validateClusterFuelRail(rail *dashboardClusterFuelRail, id string) string {
	if rail == nil {
		return ""
	}
	if !validClusterFuelSides[rail.Side] {
		return "unknown cluster fuel side: " + rail.Side
	}
	// An empty rail is a mistake rather than a choice: it takes the width and
	// draws nothing. Configure no rail instead.
	if len(rail.Bars) == 0 {
		return "cluster fuel rail has no bars: " + id
	}
	if len(rail.Bars) > clusterMaxFuelBars {
		return "cluster fuel rail has too many bars: " + id
	}
	if len(rail.TotalLabel) > gaugeLabelMaxLen {
		return "cluster fuel total label too long: " + id
	}
	for i, bar := range rail.Bars {
		ctx := fmt.Sprintf("%s fuel bar %d", id, i+1)
		if msg := validateGaugeConfig(bar.Level, ctx+" level"); msg != "" {
			return msg
		}
		if msg := validateGaugeConfig(bar.Capacity, ctx+" capacity"); msg != "" {
			return msg
		}
		if bar.Level.Quantity != "ratio" {
			return "cluster fuel level must be measured as a ratio: " + ctx
		}
		if bar.Capacity.Quantity != "volume" {
			return "cluster fuel capacity must be measured as a volume: " + ctx
		}
	}
	return ""
}

// validateLampStripWidget guards the fourth operator-configured widget.
//
// The per-config rules live in validateLampStripConfig, shared with the
// vessel-level ribbon (ADR 0082) so a strip can never accept, on one side or
// the other, something the other would have rejected. This function keeps
// only what is specific to a *widget*: its id and which sibling configs are
// disallowed on it.
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
	return validateLampStripConfig(w.Lamps, w.ID)
}

// validateLampStripConfig holds the rules for a lamp strip's contents,
// whether it is a per-page `lamps:` widget or the one vessel-level ribbon.
// where names the offending config in a rejection message.
//
// A strip with no lamps but ShowCheck set is fine — the rollup alone is a
// useful thing to pin to a page — but a strip with neither renders nothing at
// all, which is a mistake rather than a choice.
func validateLampStripConfig(cfg *dashboardLampStripConfig, where string) string {
	if cfg == nil {
		return "lamp strip requires config: " + where
	}
	if len(cfg.Title) > gaugeGroupTitleMaxLen {
		return "lamp strip title too long: " + where
	}
	if len(cfg.Lamps) == 0 && !cfg.ShowCheck {
		return "lamp strip needs at least one lamp or the check indicator: " + where
	}
	if len(cfg.Lamps) > lampStripMaxLamps {
		return "lamp strip has too many lamps: " + where
	}
	for _, lamp := range cfg.Lamps {
		path := strings.TrimSpace(lamp.Path)
		if path == "" {
			return "lamp requires a SignalK path: " + where
		}
		if len(path) > gaugePathMaxLen {
			return "lamp path too long: " + where
		}
		if len(lamp.Label) > lampStripLabelMaxLen {
			return "lamp label too long: " + where
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
				if widget.Cluster.Fuel != nil {
					for _, bar := range widget.Cluster.Fuel.Bars {
						add(bar.Level.Path)
						// The capacity too. Forgetting it is the subtler
						// failure: the bars still draw and only the litres and
						// the total are dashes, which reads as a units problem
						// rather than a missing subscription.
						add(bar.Capacity.Path)
					}
				}
			}
		}
	}

	// The ribbon (ADR 0082) is vessel-level rather than per-page, but its
	// lamps are bound paths exactly like a page lamp strip's: miss it here
	// and every ribbon lamp renders permanently blank with nothing in any
	// log to say why, the same failure mode this walker exists to prevent.
	if dashboardRibbonState != nil {
		for _, lamp := range dashboardRibbonState.Lamps {
			add(lamp.Path)
		}
	}

	sort.Strings(paths)
	return paths
}
