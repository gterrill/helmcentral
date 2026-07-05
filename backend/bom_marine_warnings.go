package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jlaffaye/ftp"
	"github.com/labstack/echo/v4"
)

const (
	bomMarineWarningsCacheTTL         = 90 * time.Minute
	defaultBomMarineWarningsCacheFile = "cache/bom_marine_warnings_cache.json"
	bomFTPAddress                     = "ftp.bom.gov.au:21"
	bomFTPDialTimeout                 = 10 * time.Second
	bomFTPProductPath                 = "/anon/gen/fwo/%s.txt"
	bomMarineWindWarningSlug          = "marine-wind-warning"
	bomHazardousSurfWarningSlug       = "hazardous-surf-warning"
)

// bomMarineWarningProductSet is the set of BOM FTP product IDs that carry
// marine wind/surf hazard warnings for a given state. Keyed by state code so
// SA/NT can be added later with zero code changes — just add a table entry
// once their product IDs are found.
type bomMarineWarningProductSet struct {
	MarineWindWarningID    string
	HazardousSurfWarningID string
}

// bomMarineProductRef pairs a product ID with its warning-type slug for
// downstream URL construction, plus a Category ("wind"/"surf") so callers
// (and the frontend, via the JSON response) can distinguish warning types
// without fragile parsing of the bulletin title.
type bomMarineProductRef struct {
	ProductID string
	Slug      string
	Category  string
}

var bomMarineWarningProducts = map[string]bomMarineWarningProductSet{
	"QLD": {MarineWindWarningID: "IDQ20085", HazardousSurfWarningID: "IDQ28522"},
	"NSW": {MarineWindWarningID: "IDN20400", HazardousSurfWarningID: "IDN28522"},
	"VIC": {MarineWindWarningID: "IDV20600"},
	"TAS": {MarineWindWarningID: "IDT20100"},
	"WA":  {MarineWindWarningID: "IDW20100"},
	// SA/NT intentionally omitted — no product ID found for them yet.
}

// bomMarineWarningSection represents one day-section of a bulletin, e.g.
// "Strong Wind Warning for the following areas: X, Y and Z" or a
// "Cancellation for the following area: X".
type bomMarineWarningSection struct {
	Day         string   `json:"day"`
	WarningType string   `json:"warning_type"`
	Zones       []string `json:"zones"`
}

// bomMarineWarningBulletin is the parsed form of a single BOM FTP marine
// warning text product.
type bomMarineWarningBulletin struct {
	ProductID   string                    `json:"product_id"`
	Title       string                    `json:"title"`
	IssuedAtRaw string                    `json:"issued_at_raw"`
	IssuedAt    time.Time                 `json:"issued_at,omitempty"`
	Sections    []bomMarineWarningSection `json:"sections"`
	DetailsURL  string                    `json:"details_url"`
	Category    string                    `json:"category"`
	RawText     string                    `json:"-"`
}

var (
	bomMarineTitleRegex    = regexp.MustCompile(`(?m)^((?:Marine Wind Warning Summary|Hazardous Surf Warning)\s+for\s+.+)$`)
	bomMarineIssuedAtRegex = regexp.MustCompile(`Issued at (.+?)\s*\n`)
	// Matches a day-of-week heading line, e.g. "Wind Warnings for Sunday 5 July",
	// "Monday 6 July", or a bare weekday+date line used as its own section header.
	bomMarineDayHeadingRegex = regexp.MustCompile(`(?m)^(?:Wind Warnings for\s+)?((?:Sunday|Monday|Tuesday|Wednesday|Thursday|Friday|Saturday)\s+\d{1,2}\s+\w+)\s*$`)
	// Matches a warning-type line followed by "for the following area(s):" or
	// "for:", optionally on the same line.
	bomMarineWarningTypeRegex = regexp.MustCompile(`(?m)^(Strong Wind Warning|Gale Warning|Storm Warning|Hazardous Surf Warning|Cancellation)\s+for(?: the following areas?)?:?\s*$`)
)

// parseBomMarineWarningText is a pure function that extracts structured
// bulletin data from a raw BOM marine warning text product. It follows this
// codebase's defensive parsing style: best-effort extraction, no panics on
// malformed input, and both a raw string and a best-effort parsed time.Time
// for the issued-at field.
func parseBomMarineWarningText(raw string) bomMarineWarningBulletin {
	bulletin := bomMarineWarningBulletin{RawText: raw}

	if m := bomMarineTitleRegex.FindStringSubmatch(raw); len(m) == 2 {
		bulletin.Title = strings.TrimSpace(m[1])
	}

	if m := bomMarineIssuedAtRegex.FindStringSubmatch(raw); len(m) == 2 {
		bulletin.IssuedAtRaw = strings.TrimSpace(m[1])
		if t, ok := parseBomIssuedAtTime(bulletin.IssuedAtRaw); ok {
			bulletin.IssuedAt = t
		}
	}

	bulletin.Sections = parseBomMarineWarningSections(raw)

	return bulletin
}

// bomWarningDetailsURL builds BOM's public per-warning deep-link page,
// e.g. https://www.bom.gov.au/warning/marine-wind-warning/IDQ20085 -
// verified live to return the correct warning-specific page for a given
// product ID and warning-type slug.
func bomWarningDetailsURL(slug, productID string) string {
	return fmt.Sprintf("https://www.bom.gov.au/warning/%s/%s", slug, productID)
}

// parseBomIssuedAtTime attempts to parse strings like
// "11:51 am EST on Sunday 5 July 2026" into a time.Time. Best-effort: BOM's
// abbreviations (EST/AEST/etc.) aren't recognized by Go's time package as
// numeric offsets, so we strip the zone name and parse the rest, following
// this codebase's "if x, ok := ...; ok" defensive style rather than
// panicking or erroring on a failed parse.
func parseBomIssuedAtTime(raw string) (time.Time, bool) {
	// Drop the leading "on " if present after the zone abbreviation, and the
	// zone abbreviation itself, e.g. "11:51 am EST on Sunday 5 July 2026" ->
	// "11:51 am Sunday 5 July 2026".
	cleaned := raw
	if idx := strings.Index(cleaned, " on "); idx != -1 {
		beforeOn := cleaned[:idx]
		afterOn := cleaned[idx+len(" on "):]
		// beforeOn is like "11:51 am EST" - drop the trailing zone token.
		fields := strings.Fields(beforeOn)
		if len(fields) >= 2 {
			beforeOn = strings.Join(fields[:len(fields)-1], " ")
		}
		cleaned = beforeOn + " " + afterOn
	}
	cleaned = strings.TrimSpace(cleaned)

	layouts := []string{
		"3:04 pm Monday 2 January 2006",
		"3:04 PM Monday 2 January 2006",
		"15:04 Monday 2 January 2006",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, cleaned); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// parseBomMarineWarningSections extracts day-sections from a raw bulletin
// body. It handles the "Wind Warnings for <day>" heading style (used by
// Marine Wind Warning Summary products) as well as the bare "<day>" heading
// style (used by Hazardous Surf Warning products), each followed by a
// warning-type line and a zone list that may wrap across multiple lines.
func parseBomMarineWarningSections(raw string) []bomMarineWarningSection {
	lines := strings.Split(raw, "\n")

	var sections []bomMarineWarningSection
	currentDay := ""

	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r")
		trimmed := strings.TrimSpace(line)

		if dayMatch := bomMarineDayHeadingRegex.FindStringSubmatch(trimmed); dayMatch != nil {
			currentDay = strings.TrimSpace(dayMatch[1])
			continue
		}

		typeMatch := bomMarineWarningTypeRegex.FindStringSubmatch(trimmed)
		if typeMatch == nil || currentDay == "" {
			continue
		}

		warningType := typeMatch[1]

		// Collect the zone list from the following non-empty lines until a
		// blank line or end of input (BOM wraps long zone lists across
		// multiple lines).
		var zoneLines []string
		j := i + 1
		for j < len(lines) {
			next := strings.TrimSpace(strings.TrimRight(lines[j], "\r"))
			if next == "" {
				break
			}
			zoneLines = append(zoneLines, next)
			j++
		}

		zones := parseBomZoneList(strings.Join(zoneLines, " "))

		sections = append(sections, bomMarineWarningSection{
			Day:         currentDay,
			WarningType: warningType,
			Zones:       zones,
		})

		i = j
	}

	return sections
}

// parseBomZoneList splits a zone list like "X, Y and Z" or a single zone
// "X" into individual zone names. Handles comma-separated lists and an
// "and" before the final item.
func parseBomZoneList(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	// Normalize " and " (with surrounding whitespace) to a comma so we can
	// split uniformly, but only as a word-boundary replacement to avoid
	// mangling zone names that might contain "and" as a substring.
	normalized := regexp.MustCompile(`\s+and\s+`).ReplaceAllString(text, ", ")

	rawParts := strings.Split(normalized, ",")
	zones := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		zone := strings.TrimSpace(part)
		zone = strings.TrimSuffix(zone, ".")
		if zone == "" {
			continue
		}
		zones = append(zones, zone)
	}

	return zones
}

// hasActiveWarning reports whether a bulletin contains at least one
// non-cancellation warning section anywhere in the state. A bulletin
// containing only "Cancellation" sections means previously active warnings
// were lifted, not a new hazard, so it must not be treated as an active
// hazard by callers.
func hasActiveWarning(bulletin bomMarineWarningBulletin) bool {
	for _, section := range bulletin.Sections {
		if !strings.EqualFold(section.WarningType, "Cancellation") {
			return true
		}
	}
	return false
}

// hasActiveWarningForZone reports whether a bulletin contains a
// non-cancellation warning section that specifically names the given zone.
// This is the vessel-relevant signal — a state-wide bulletin can have an
// active warning for a coast hundreds of km away that shouldn't alert a
// vessel elsewhere in the same state. Returns false if zone is empty (no
// matched zone for this position, e.g. TAS/WA today).
func hasActiveWarningForZone(bulletin bomMarineWarningBulletin, zone string) bool {
	if zone == "" {
		return false
	}
	for _, section := range bulletin.Sections {
		if strings.EqualFold(section.WarningType, "Cancellation") {
			continue
		}
		for _, z := range section.Zones {
			if strings.EqualFold(z, zone) {
				return true
			}
		}
	}
	return false
}

// fetchBomMarineWarningProduct connects to the BOM anonymous FTP mirror and
// retrieves the raw text of a single product ID. Plain HTTP scraping of
// www.bom.gov.au is bot-blocked; this anonymous FTP mirror is not.
func fetchBomMarineWarningProduct(productID string) (string, error) {
	conn, err := ftp.Dial(bomFTPAddress, ftp.DialWithTimeout(bomFTPDialTimeout))
	if err != nil {
		return "", fmt.Errorf("failed to connect to BOM FTP mirror: %v", err)
	}
	defer conn.Quit()

	if err := conn.Login("anonymous", "anonymous@"); err != nil {
		return "", fmt.Errorf("failed to log in to BOM FTP mirror: %v", err)
	}

	resp, err := conn.Retr(fmt.Sprintf(bomFTPProductPath, productID))
	if err != nil {
		return "", fmt.Errorf("failed to retrieve BOM product %s: %v", productID, err)
	}
	defer resp.Close()

	body, err := io.ReadAll(resp)
	if err != nil {
		return "", fmt.Errorf("failed to read BOM product %s: %v", productID, err)
	}

	return string(body), nil
}

// bomMarineWarningsCacheEntry holds the cached bulletins for a single state,
// keyed by product ID (e.g. "IDQ20085").
type bomMarineWarningsCacheEntry struct {
	State     string                              `json:"state"`
	Zone      string                              `json:"zone"`
	Bulletins map[string]bomMarineWarningBulletin `json:"bulletins"`
	CachedAt  time.Time                           `json:"cached_at"`
}

type bomMarineWarningsCacheStore struct {
	mu   sync.RWMutex
	data map[string]bomMarineWarningsCacheEntry
}

var bomMarineWarningsStore = &bomMarineWarningsCacheStore{data: make(map[string]bomMarineWarningsCacheEntry)}

func loadBomMarineWarningsCacheFromDisk() {
	path := cacheFilePath("BOM_MARINE_WARNINGS_CACHE_FILE", defaultBomMarineWarningsCacheFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("Failed to read BOM marine warnings cache file %s: %v", path, err)
		}
		return
	}

	payload := map[string]bomMarineWarningsCacheEntry{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		log.Printf("Failed to parse BOM marine warnings cache file %s: %v", path, err)
		return
	}

	now := time.Now().UTC()
	loaded := make(map[string]bomMarineWarningsCacheEntry, len(payload))
	for key, entry := range payload {
		if now.Sub(entry.CachedAt) >= bomMarineWarningsCacheTTL {
			continue
		}
		loaded[key] = entry
	}

	bomMarineWarningsStore.mu.Lock()
	bomMarineWarningsStore.data = loaded
	bomMarineWarningsStore.mu.Unlock()
	log.Printf("Loaded %d BOM marine warnings cache entries from %s", len(loaded), path)
}

func persistBomMarineWarningsCacheToDisk() {
	path := cacheFilePath("BOM_MARINE_WARNINGS_CACHE_FILE", defaultBomMarineWarningsCacheFile)

	bomMarineWarningsStore.mu.RLock()
	payload := make(map[string]bomMarineWarningsCacheEntry, len(bomMarineWarningsStore.data))
	for key, entry := range bomMarineWarningsStore.data {
		payload[key] = entry
	}
	bomMarineWarningsStore.mu.RUnlock()

	if err := writeJSONFileAtomic(path, payload); err != nil {
		log.Printf("Failed to persist BOM marine warnings cache to %s: %v", path, err)
	}
}

// refreshBomMarineWarningsForState fetches and caches the marine warning
// bulletins for a single state's registered product IDs. Fail-fast: if an
// upstream FTP fetch fails, the error is logged and that product is skipped
// rather than silently masked with stale/fabricated data — the existing
// cache entry (if any) is left untouched so callers still get the last-known
// good data along with its real age.
func refreshBomMarineWarningsForState(state, zone string) {
	products, ok := bomMarineWarningProducts[state]
	if !ok {
		return
	}

	var productRefs []bomMarineProductRef
	if products.MarineWindWarningID != "" {
		productRefs = append(productRefs, bomMarineProductRef{ProductID: products.MarineWindWarningID, Slug: bomMarineWindWarningSlug, Category: "wind"})
	}
	if products.HazardousSurfWarningID != "" {
		productRefs = append(productRefs, bomMarineProductRef{ProductID: products.HazardousSurfWarningID, Slug: bomHazardousSurfWarningSlug, Category: "surf"})
	}
	if len(productRefs) == 0 {
		return
	}

	bulletins := make(map[string]bomMarineWarningBulletin, len(productRefs))
	for _, ref := range productRefs {
		raw, err := fetchBomMarineWarningProduct(ref.ProductID)
		if err != nil {
			log.Printf("Failed to fetch BOM marine warning product %s: %v", ref.ProductID, err)
			continue
		}
		bulletin := parseBomMarineWarningText(raw)
		bulletin.ProductID = ref.ProductID
		bulletin.DetailsURL = bomWarningDetailsURL(ref.Slug, ref.ProductID)
		bulletin.Category = ref.Category
		bulletins[ref.ProductID] = bulletin
	}

	if len(bulletins) == 0 {
		return
	}

	bomMarineWarningsStore.mu.Lock()
	bomMarineWarningsStore.data[state] = bomMarineWarningsCacheEntry{
		State:     state,
		Zone:      zone,
		Bulletins: bulletins,
		CachedAt:  time.Now().UTC(),
	}
	bomMarineWarningsStore.mu.Unlock()
	persistBomMarineWarningsCacheToDisk()
}

// startBomMarineWarningsPoller starts a ticker goroutine that polls the BOM
// FTP mirror for marine warnings relevant to the vessel's current position,
// mirroring the shape of startTideAutoUpdater.
func startBomMarineWarningsPoller(interval time.Duration) {
	go func() {
		// Fetch immediately on startup rather than waiting for the first
		// tick - an hourly-only ticker would otherwise leave the cache
		// empty (and the UI silent) for up to an hour after every restart,
		// which defeats the point of a safety alert.
		updateBomMarineWarningsForVesselPosition()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			updateBomMarineWarningsForVesselPosition()
		}
	}()
}

func updateBomMarineWarningsForVesselPosition() {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")

	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}
	signalkURL := buildSignalKURL(address, port)
	vesselPath := getEnv("SIGNALK_VESSEL_PATH", "/signalk/v1/api/vessels/self")

	vesselState, err := fetchSignalKVesselState(signalkURL, vesselPath)
	if err != nil || vesselState.Latitude < -90 || vesselState.Latitude > 90 {
		return
	}

	state, ok := stateForPosition(vesselState.Latitude, vesselState.Longitude)
	if !ok {
		return
	}

	zone, _ := zoneForPosition(state, vesselState.Latitude, vesselState.Longitude)

	refreshBomMarineWarningsForState(state, zone)
}

// bomMarineWarningSectionResponse and friends mirror this codebase's
// snake_case JSON response convention (see weatherForecastResponse).
type bomMarineWarningSectionResponse struct {
	Day         string   `json:"day"`
	WarningType string   `json:"warning_type"`
	Zones       []string `json:"zones"`
}

type bomMarineWarningBulletinResponse struct {
	ProductID        string                            `json:"product_id"`
	Title            string                            `json:"title"`
	IssuedAtRaw      string                            `json:"issued_at_raw"`
	IssuedAt         string                            `json:"issued_at,omitempty"`
	Sections         []bomMarineWarningSectionResponse `json:"sections"`
	DetailsURL       string                            `json:"details_url"`
	Category         string                            `json:"category"`
	HasActive        bool                              `json:"has_active_warning"`
	HasActiveForZone bool                              `json:"has_active_warning_for_zone"`
}

type marineWarningsResponse struct {
	State     string                             `json:"state"`
	Zone      string                             `json:"zone"`
	Bulletins []bomMarineWarningBulletinResponse `json:"bulletins"`
	// HasActive is true only when the vessel's own matched zone is named in
	// an active (non-cancellation) section - NOT "any warning anywhere in
	// the state". A warning for a coast hundreds of km away must not alert
	// a vessel elsewhere in the same state.
	HasActive  bool   `json:"has_active_warning"`
	Cached     bool   `json:"cached"`
	UpdatedAt  string `json:"updated_at"`
	TTLSeconds int64  `json:"ttl_seconds"`
}

// marineWarningsHandler serves GET /api/marine-warnings. If there is no
// vessel position, no matching zone, or no cached data yet, it returns a
// sensible empty 200 response rather than an error, matching how other
// handlers in this codebase degrade (e.g. weatherForecast's cache-miss path
// still returns 200 once data is available; here we return an empty but
// well-formed payload rather than blocking on a live fetch in the request
// path).
func marineWarningsHandler(c echo.Context) error {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}
	signalkURL := buildSignalKURL(address, port)
	vesselPath := getEnv("SIGNALK_VESSEL_PATH", "/signalk/v1/api/vessels/self")

	vesselState, vesselErr := fetchSignalKVesselState(signalkURL, vesselPath)
	if vesselErr != nil || vesselState.Latitude < -90 || vesselState.Latitude > 90 {
		return c.JSON(200, emptyMarineWarningsResponse())
	}

	state, ok := stateForPosition(vesselState.Latitude, vesselState.Longitude)
	if !ok {
		return c.JSON(200, emptyMarineWarningsResponse())
	}
	zone, _ := zoneForPosition(state, vesselState.Latitude, vesselState.Longitude)

	bomMarineWarningsStore.mu.RLock()
	entry, ok := bomMarineWarningsStore.data[state]
	bomMarineWarningsStore.mu.RUnlock()
	if !ok {
		return c.JSON(200, marineWarningsResponse{
			State:      state,
			Zone:       zone,
			Bulletins:  []bomMarineWarningBulletinResponse{},
			TTLSeconds: int64(bomMarineWarningsCacheTTL / time.Second),
		})
	}

	response := buildMarineWarningsResponse(entry)
	return c.JSON(200, response)
}

func emptyMarineWarningsResponse() marineWarningsResponse {
	return marineWarningsResponse{
		Bulletins:  []bomMarineWarningBulletinResponse{},
		TTLSeconds: int64(bomMarineWarningsCacheTTL / time.Second),
	}
}

func buildMarineWarningsResponse(entry bomMarineWarningsCacheEntry) marineWarningsResponse {
	// Sort by product ID for stable output — map iteration order is
	// otherwise randomized per-request, which would make the bulletin list
	// order flicker between requests for no reason.
	productIDs := make([]string, 0, len(entry.Bulletins))
	for productID := range entry.Bulletins {
		productIDs = append(productIDs, productID)
	}
	sort.Strings(productIDs)

	bulletins := make([]bomMarineWarningBulletinResponse, 0, len(entry.Bulletins))
	anyActiveForZone := false
	for _, productID := range productIDs {
		bulletin := entry.Bulletins[productID]
		sections := make([]bomMarineWarningSectionResponse, 0, len(bulletin.Sections))
		for _, section := range bulletin.Sections {
			sections = append(sections, bomMarineWarningSectionResponse{
				Day:         section.Day,
				WarningType: section.WarningType,
				Zones:       section.Zones,
			})
		}

		activeForZone := hasActiveWarningForZone(bulletin, entry.Zone)
		if activeForZone {
			anyActiveForZone = true
		}

		issuedAt := ""
		if !bulletin.IssuedAt.IsZero() {
			issuedAt = bulletin.IssuedAt.UTC().Format(time.RFC3339)
		}

		bulletins = append(bulletins, bomMarineWarningBulletinResponse{
			ProductID:        bulletin.ProductID,
			Title:            bulletin.Title,
			IssuedAtRaw:      bulletin.IssuedAtRaw,
			IssuedAt:         issuedAt,
			Sections:         sections,
			DetailsURL:       bulletin.DetailsURL,
			Category:         bulletin.Category,
			HasActive:        hasActiveWarning(bulletin),
			HasActiveForZone: activeForZone,
		})
	}

	return marineWarningsResponse{
		State:      entry.State,
		Zone:       entry.Zone,
		Bulletins:  bulletins,
		HasActive:  anyActiveForZone,
		Cached:     true,
		UpdatedAt:  entry.CachedAt.UTC().Format(time.RFC3339),
		TTLSeconds: int64(bomMarineWarningsCacheTTL / time.Second),
	}
}
