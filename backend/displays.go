package main

import (
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// displayData is one physical wall screen (ADR 0110): the ODROID strip in
// the flybridge, an LG C5 in the saloon, and whatever comes after. A page
// belongs to at most one display via dashboardPageData.DisplayID; a display
// owns no ordered list of pages itself (that would be two things to keep in
// sync — see the plan's rejected list).
type displayData struct {
	ID   string `json:"id"`
	Name string `json:"name"` // "Flybridge", "Saloon TV"
	Slug string `json:"slug"` // the URL segment: /display/<slug>

	// The logical canvas a page is authored against, in CSS px. Both zero
	// means "whatever this browser reports": the shell claims the full
	// viewport and no fold guide is drawn. No omitempty: zero is a
	// meaningful value here ("full viewport"), not an absence, and the
	// frontend must be able to tell "0" from "key not sent" - a full-
	// viewport display has to serialize width/height explicitly as 0 or
	// displayFoldPx reads undefined instead of 0 and computes NaN.
	Width  int `json:"width"`
	Height int `json:"height"`
	// On-screen magnification of that canvas. A 55" TV read at 3-4m needs
	// 1.5x-2x what a strip read at arm's length does; authoring a 1280x720
	// canvas at 1.5x is far better than authoring giant tiles on 1920x1080.
	Scale  float64 `json:"scale,omitempty"`  // 0 or absent means 1.0
	Rotate int     `json:"rotate,omitempty"` // 0 or 180

	// Shift the whole board a few px on a slow cycle. Named for what it
	// does; labelled "OLED panel" in the UI, which is why you'd want it.
	PixelShift bool `json:"pixel_shift,omitempty"`
	WakeLock   bool `json:"wake_lock,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// displaysState is guarded by dashboardPagesMu, the same lock that guards
// dashboardPagesState and dashboardRibbonState — all three live in the one
// dashboard-pages.json file (ADR 0082 set this precedent for the ribbon),
// so every writer of that file must go through dashboardPagesSnapshotLocked
// rather than assembling dashboardPagesFile by hand.
var displaysState map[string]*displayData

// orderedDisplaysLocked returns every display, oldest first (CreatedAt then
// ID breaking ties), mirroring orderedDashboardPagesLocked's own ordering
// rule. There is no reorder endpoint: a display's own position in the
// sidebar group is its creation order, not something the operator drags.
func orderedDisplaysLocked() []*displayData {
	list := make([]*displayData, 0, len(displaysState))
	for _, d := range displaysState {
		list = append(list, d)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].CreatedAt.Equal(list[j].CreatedAt) {
			return list[i].ID < list[j].ID
		}
		return list[i].CreatedAt.Before(list[j].CreatedAt)
	})
	return list
}

// displayExistsLocked reports whether id names a display currently on
// record. Callers must hold dashboardPagesMu (read or write). Named for the
// fail-closed stance heroWidgetExists already takes: a non-empty reference
// to something that does not exist is rejected, never silently accepted.
func displayExistsLocked(id string) bool {
	_, ok := displaysState[id]
	return ok
}

const (
	displayNameMaxLen = 48
	displaySlugMaxLen = 32
	displayMaxCount   = 8

	displayMinPx       = 200
	displayMaxWidthPx  = 7680
	displayMaxHeightPx = 4320

	displayMinScale = 0.5
	displayMaxScale = 4.0
)

var displaySlugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

var validDisplayRotations = map[int]bool{0: true, 180: true}

// slugify derives a candidate slug from a display name: lowercase, spaces
// (and anything else outside [a-z0-9-]) collapsed to a single hyphen, edge
// hyphens trimmed, and capped at displaySlugMaxLen.
//
// The cap matters because a name may be longer than a slug (48 against 32)
// and the create form offers no slug field: without it, "Saloon Television
// On The Starboard Bulkhead" is a legal name whose derived slug is refused,
// leaving the operator unable to create that display at all. Truncation is
// deterministic and preserves the leading words, which is what makes the
// result still recognisable; an EXPLICIT slug over the limit is still
// rejected by the caller, since the operator typed that one.
//
// A collision after truncation is rejected like any other, never
// auto-suffixed (ADR 0110).
func slugify(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	prevHyphen := false
	for _, r := range lower {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			prevHyphen = false
		default:
			if !prevHyphen && b.Len() > 0 {
				b.WriteRune('-')
				prevHyphen = true
			}
		}
	}
	slug := b.String()
	if len(slug) > displaySlugMaxLen {
		slug = slug[:displaySlugMaxLen]
	}
	// After trimming, and after a cut that may have landed mid-hyphen.
	return strings.TrimRight(slug, "-")
}

// displaySlugTakenLocked reports whether slug is already used by a display
// other than excludeID (the record being patched, if any). Callers must
// hold dashboardPagesMu for writing.
func displaySlugTakenLocked(slug, excludeID string) bool {
	for _, d := range displaysState {
		if d.ID != excludeID && d.Slug == slug {
			return true
		}
	}
	return false
}

// validateDisplayGeometry holds the width/height/scale/rotate rules
// together (plan §1 "Validation"), shared by create and patch so both fail
// closed the same way.
func validateDisplayGeometry(width, height int, scale float64, rotate int) string {
	if (width == 0) != (height == 0) {
		return "display width and height must both be zero or both be set"
	}
	if width != 0 {
		if width < displayMinPx || width > displayMaxWidthPx {
			return "display width out of range"
		}
		if height < displayMinPx || height > displayMaxHeightPx {
			return "display height out of range"
		}
	}
	if scale != 0 {
		if scale < displayMinScale || scale > displayMaxScale {
			return "display scale out of range"
		}
		if scale != 1 && width == 0 {
			return "display scale requires a canvas size (width and height)"
		}
	}
	if !validDisplayRotations[rotate] {
		return "display rotate must be 0 or 180"
	}
	return ""
}

// GET /api/displays
func listDisplaysHandler(c echo.Context) error {
	dashboardPagesMu.RLock()
	defer dashboardPagesMu.RUnlock()
	return c.JSON(http.StatusOK, map[string][]*displayData{"displays": orderedDisplaysLocked()})
}

// POST /api/displays
func createDisplayHandler(c echo.Context) error {
	var body struct {
		Name       string  `json:"name"`
		Slug       string  `json:"slug"`
		Width      int     `json:"width"`
		Height     int     `json:"height"`
		Scale      float64 `json:"scale"`
		Rotate     int     `json:"rotate"`
		PixelShift bool    `json:"pixel_shift"`
		WakeLock   bool    `json:"wake_lock"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "name is required"})
	}
	if len(name) > displayNameMaxLen {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "display name is too long"})
	}
	if msg := validateDisplayGeometry(body.Width, body.Height, body.Scale, body.Rotate); msg != "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": msg})
	}

	slug := strings.TrimSpace(body.Slug)
	derived := false
	if slug == "" {
		slug = slugify(name)
		derived = true
	}
	if len(slug) > displaySlugMaxLen {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "display slug is too long"})
	}
	if !displaySlugPattern.MatchString(slug) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "display slug must be lowercase letters, digits and single hyphens"})
	}

	dashboardPagesMu.Lock()
	defer dashboardPagesMu.Unlock()

	if len(displaysState) >= displayMaxCount {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "maximum number of displays reached"})
	}
	if displaySlugTakenLocked(slug, "") {
		if derived {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "a display already uses the slug derived from this name (\"" + slug + "\"); choose an explicit slug"})
		}
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "a display already uses this slug: " + slug})
	}

	now := time.Now().UTC()
	display := &displayData{
		ID:         uuid.NewString(),
		Name:       name,
		Slug:       slug,
		Width:      body.Width,
		Height:     body.Height,
		Scale:      body.Scale,
		Rotate:     body.Rotate,
		PixelShift: body.PixelShift,
		WakeLock:   body.WakeLock,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	displaysState[display.ID] = display
	if err := saveDashboardPagesLocked(); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to persist"})
	}
	return c.JSON(http.StatusCreated, display)
}

// PATCH /api/displays/:id
func patchDisplayHandler(c echo.Context) error {
	id := c.Param("id")

	var body struct {
		Name       *string  `json:"name"`
		Slug       *string  `json:"slug"`
		Width      *int     `json:"width"`
		Height     *int     `json:"height"`
		Scale      *float64 `json:"scale"`
		Rotate     *int     `json:"rotate"`
		PixelShift *bool    `json:"pixel_shift"`
		WakeLock   *bool    `json:"wake_lock"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if body.Name == nil && body.Slug == nil && body.Width == nil && body.Height == nil &&
		body.Scale == nil && body.Rotate == nil && body.PixelShift == nil && body.WakeLock == nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "no patch fields provided"})
	}

	dashboardPagesMu.Lock()
	defer dashboardPagesMu.Unlock()

	current, ok := displaysState[id]
	if !ok {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "display not found"})
	}

	updated := *current
	updated.UpdatedAt = time.Now().UTC()
	if body.Name != nil {
		name := strings.TrimSpace(*body.Name)
		if name == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "name cannot be empty"})
		}
		if len(name) > displayNameMaxLen {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "display name is too long"})
		}
		updated.Name = name
	}
	if body.Slug != nil {
		slug := strings.TrimSpace(*body.Slug)
		if slug == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "slug cannot be empty"})
		}
		if len(slug) > displaySlugMaxLen {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "display slug is too long"})
		}
		if !displaySlugPattern.MatchString(slug) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "display slug must be lowercase letters, digits and single hyphens"})
		}
		updated.Slug = slug
	}
	if displaySlugTakenLocked(updated.Slug, id) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "a display already uses this slug: " + updated.Slug})
	}
	if body.Width != nil {
		updated.Width = *body.Width
	}
	if body.Height != nil {
		updated.Height = *body.Height
	}
	if body.Scale != nil {
		updated.Scale = *body.Scale
	}
	if body.Rotate != nil {
		updated.Rotate = *body.Rotate
	}
	if msg := validateDisplayGeometry(updated.Width, updated.Height, updated.Scale, updated.Rotate); msg != "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": msg})
	}
	if body.PixelShift != nil {
		updated.PixelShift = *body.PixelShift
	}
	if body.WakeLock != nil {
		updated.WakeLock = *body.WakeLock
	}

	displaysState[id] = &updated
	if err := saveDashboardPagesLocked(); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to persist"})
	}
	return c.JSON(http.StatusOK, &updated)
}

// DELETE /api/displays/:id releases every page that referenced this display
// (clearing DisplayID, never deleting the page) in the same locked write
// that removes the display record (plan §3).
// withoutDisplay returns the snapshot's display list with one id removed,
// leaving the caller's slice alone so nothing is published before the write.
func withoutDisplay(displays []*displayData, id string) []*displayData {
	out := make([]*displayData, 0, len(displays))
	for _, d := range displays {
		if d.ID != id {
			out = append(out, d)
		}
	}
	return out
}

// withReleasedPages substitutes the released copies into the snapshot's page
// list, preserving its order.
func withReleasedPages(pages []*dashboardPageData, released []*dashboardPageData) []*dashboardPageData {
	if len(released) == 0 {
		return pages
	}
	byID := make(map[string]*dashboardPageData, len(released))
	for _, page := range released {
		byID[page.ID] = page
	}
	out := make([]*dashboardPageData, 0, len(pages))
	for _, page := range pages {
		if replacement, ok := byID[page.ID]; ok {
			out = append(out, replacement)
			continue
		}
		out = append(out, page)
	}
	return out
}

func deleteDisplayHandler(c echo.Context) error {
	id := c.Param("id")

	dashboardPagesMu.Lock()
	defer dashboardPagesMu.Unlock()

	if _, ok := displaysState[id]; !ok {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "display not found"})
	}

	now := time.Now().UTC()
	// Non-nil even when nothing is released, so the response always carries
	// a JSON array rather than null for a client iterating it directly -
	// the same "ensure it's an empty slice for consistency" stance
	// createDashboardPageHandler already takes for a nil Widgets body.
	releasedPageIDs := []string{}
	released := make([]*dashboardPageData, 0)
	for _, page := range dashboardPagesState {
		if page.DisplayID != id {
			continue
		}
		// A copy, so a failed write below leaves the live record untouched.
		updated := *page
		updated.DisplayID = ""
		updated.UpdatedAt = now
		released = append(released, &updated)
		releasedPageIDs = append(releasedPageIDs, page.ID)
	}
	sort.Strings(releasedPageIDs)

	// Publish only after the write is durable, the same stance
	// reorderDashboardPagesHandler and putDashboardRibbonHandler take. This
	// delete is the widest of the three: it drops a display AND releases
	// every page that referenced it, so publishing first would let a failed
	// write leave the API reporting a release that vanishes at the next
	// restart, across several records at once.
	snap := dashboardPagesSnapshotLocked()
	snap.Displays = withoutDisplay(snap.Displays, id)
	snap.Pages = withReleasedPages(snap.Pages, released)
	if err := writeJSONFileAtomic(dashboardPagesFilePath(), snap); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to persist"})
	}

	delete(displaysState, id)
	for _, page := range released {
		dashboardPagesState[page.ID] = page
	}

	return c.JSON(http.StatusOK, map[string]any{
		"status":            "deleted",
		"released_page_ids": releasedPageIDs,
	})
}
