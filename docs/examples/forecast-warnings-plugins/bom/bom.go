// bom.go holds the parsing, filtering, and fetch-orchestration logic for the
// BOM (Bureau of Meteorology, Australia) forecast-warnings plugin - the
// default ("bom") implementation of Helmcentral's pluggable Forecast
// Warnings provider contract. It is a 1:1 port of the formerly-hardcoded
// backend/bom_marine_warnings.go, generalized behind the same
// fetch-over-FTP shape every plugin now gets via the host's generic
// "ftp_fetch" custom host function (see main.go's doc comment - a plain Go
// FTP client can't be used here because a TinyGo WASM guest cannot open raw
// sockets).
//
// This file has no dependency on "github.com/extism/go-pdk" (the fetch step
// is injected as a plain func(host, path string) (string, error)), so it
// (and main_test.go, which exercises it) can be built and tested with the
// plain host Go toolchain (`go test ./...`, no TinyGo/wasm target needed) -
// see main.go's doc comment for why that split matters, mirroring
// docs/examples/tide-plugins/bom/bom.go's identical approach.
package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	// bomFTPHost and bomFTPProductPathFmt mirror the formerly-native
	// backend/bom_marine_warnings.go's bomFTPAddress/bomFTPProductPath
	// constants exactly.
	bomFTPHost           = "ftp.bom.gov.au:21"
	bomFTPProductPathFmt = "/anon/gen/fwo/%s.txt"

	bomMarineWindWarningSlug    = "marine-wind-warning"
	bomHazardousSurfWarningSlug = "hazardous-surf-warning"
)

// bomMarineWarningProductSet is the set of BOM FTP product IDs that carry
// marine wind/surf hazard warnings for a given state. Keyed by state code so
// SA/NT can be added later with zero code changes - just add a table entry
// once their product IDs are found. Ported verbatim from
// backend/bom_marine_warnings.go's identical table.
type bomMarineWarningProductSet struct {
	MarineWindWarningID    string
	HazardousSurfWarningID string
}

// bomMarineProductRef pairs a product ID with its warning-type slug for
// downstream URL construction, plus a Category ("wind"/"surf") so the
// fetch_warnings response can distinguish warning types without fragile
// parsing of the bulletin title.
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
	// SA/NT intentionally omitted - no product ID found for them yet.
}

// bomMarineWarningSection represents one day-section of a bulletin, e.g.
// "Strong Wind Warning for the following areas: X, Y and Z" or a
// "Cancellation for the following area: X". Zones is kept internally (unlike
// the plugin's public sectionOut contract type) since filtering by zone
// happens after parsing, in buildFetchWarningsOutput.
type bomMarineWarningSection struct {
	Day         string
	WarningType string
	Zones       []string
}

// bomMarineWarningBulletin is the parsed form of a single BOM FTP marine
// warning text product.
type bomMarineWarningBulletin struct {
	ProductID   string
	Title       string
	IssuedAtRaw string
	IssuedAt    time.Time
	Sections    []bomMarineWarningSection
	DetailsURL  string
	Category    string
	RawText     string
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
	// bomMarineAndRegex normalizes " and " (word-boundary only) to a comma in
	// zone lists, split out as a package-level var (rather than compiled
	// inline on every call, as the native version did) purely to avoid
	// recompiling the same pattern on every parseBomZoneList call.
	bomMarineAndRegex = regexp.MustCompile(`\s+and\s+`)
)

// parseBomMarineWarningText is a pure function that extracts structured
// bulletin data from a raw BOM marine warning text product. It follows this
// codebase's defensive parsing style: best-effort extraction, no panics on
// malformed input, and both a raw string and a best-effort parsed time.Time
// for the issued-at field. Ported verbatim from
// backend/bom_marine_warnings.go.
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
// e.g. https://www.bom.gov.au/warning/marine-wind-warning/IDQ20085 - ported
// verbatim from backend/bom_marine_warnings.go.
func bomWarningDetailsURL(slug, productID string) string {
	return fmt.Sprintf("https://www.bom.gov.au/warning/%s/%s", slug, productID)
}

// parseBomIssuedAtTime attempts to parse strings like
// "11:51 am EST on Sunday 5 July 2026" into a time.Time. Best-effort: BOM's
// abbreviations (EST/AEST/etc.) aren't recognized by Go's time package as
// numeric offsets, so we strip the zone name and parse the rest. Ported
// verbatim from backend/bom_marine_warnings.go.
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
// Ported verbatim from backend/bom_marine_warnings.go.
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
// "and" before the final item. Ported verbatim from
// backend/bom_marine_warnings.go.
func parseBomZoneList(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	// Normalize " and " (with surrounding whitespace) to a comma so we can
	// split uniformly, but only as a word-boundary replacement to avoid
	// mangling zone names that might contain "and" as a substring.
	normalized := bomMarineAndRegex.ReplaceAllString(text, ", ")

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
// non-cancellation warning section anywhere in the state. Ported verbatim
// from backend/bom_marine_warnings.go.
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
// Returns false if zone is empty (no matched zone for this position, e.g.
// TAS/WA today). Ported verbatim from backend/bom_marine_warnings.go.
func hasActiveWarningForZone(bulletin bomMarineWarningBulletin, zone string) bool {
	if zone == "" {
		return false
	}
	for _, section := range bulletin.Sections {
		if strings.EqualFold(section.WarningType, "Cancellation") {
			continue
		}
		if containsZoneFold(section.Zones, zone) {
			return true
		}
	}
	return false
}

// containsZoneFold reports whether zones contains target, case-insensitively.
func containsZoneFold(zones []string, target string) bool {
	for _, z := range zones {
		if strings.EqualFold(z, target) {
			return true
		}
	}
	return false
}

// --- Helmcentral Forecast Warnings plugin contract shapes
// (backend/forecast_warnings_providers.go / wasm_forecast_warnings_provider.go) ---

type fetchWarningsInput struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type sectionOut struct {
	Day         string `json:"day"`
	WarningType string `json:"warning_type"`
}

type bulletinOut struct {
	ID         string       `json:"id"`
	Title      string       `json:"title"`
	Category   string       `json:"category"`
	IssuedAt   string       `json:"issued_at"`
	DetailsURL string       `json:"details_url"`
	Sections   []sectionOut `json:"sections"`
}

type fetchWarningsOutput struct {
	Region    string        `json:"region"`
	Bulletins []bulletinOut `json:"bulletins"`
}

// ftpFetcher is the shape of the injected fetch step - main.go's real
// implementation calls the host's "ftp_fetch" custom host function; tests
// inject a fake so this whole file stays testable on the plain host Go
// toolchain with no WASM runtime involved.
type ftpFetcher func(host, path string) (string, error)

// activeSectionsForZone filters a parsed bulletin's sections down to the
// ones that are (a) not a cancellation and (b) name the given zone - the
// same "vessel-relevant" test hasActiveWarningForZone applies, but
// collecting the surviving sections themselves rather than a single bool,
// since the fetch_warnings contract returns only currently-active sections
// (see forecast_warnings_providers.go's doc comment: the host does no
// zone-matching or active/cancelled filtering of its own - each plugin does
// its own). Returns nil (no sections) when zone is empty, exactly mirroring
// hasActiveWarningForZone's "no matched zone never alerts" rule.
func activeSectionsForZone(sections []bomMarineWarningSection, zone string) []sectionOut {
	if zone == "" {
		return nil
	}

	var out []sectionOut
	for _, s := range sections {
		if strings.EqualFold(s.WarningType, "Cancellation") {
			continue
		}
		if !containsZoneFold(s.Zones, zone) {
			continue
		}
		out = append(out, sectionOut{Day: s.Day, WarningType: s.WarningType})
	}
	return out
}

// buildFetchWarningsOutput is the fetch_warnings orchestration logic: resolve
// state+zone for the given position, fetch every applicable BOM product ID
// for that state via fetch, parse each, filter to currently-active sections
// for the resolved zone, and only include a bulletin when it has at least
// one surviving section. Ported from backend/bom_marine_warnings.go's
// refreshBomMarineWarningsForState, adapted from "fetch+cache" to
// "fetch+filter+return" since the WASM host (wasmForecastWarningsProvider)
// already owns TTL caching generically - this plugin does not cache.
//
// Unmapped position (stateForPosition fails) or a state with no registered
// product IDs (e.g. SA/NT) is NOT an error - it's a legitimate "nothing to
// report here" result: fewer/zero bulletins, err == nil. A genuine fetch
// failure for one applicable product does not fail the whole call either -
// that product's bulletin is just omitted, mirroring the host ftp_fetch
// function's own "structured error, not everything-fails" philosophy. Only
// when EVERY applicable product's fetch fails (zero successes) is this
// treated as a real "couldn't get any data" operational failure worth
// surfacing as an error.
func buildFetchWarningsOutput(lat, lon float64, fetch ftpFetcher) (fetchWarningsOutput, error) {
	out := fetchWarningsOutput{Bulletins: []bulletinOut{}}

	state, ok := stateForPosition(lat, lon)
	if !ok {
		return out, nil
	}

	zone, zoneOk := zoneForPosition(state, lat, lon)
	if zoneOk {
		out.Region = fmt.Sprintf("%s — %s", state, zone)
	} else {
		out.Region = state
	}

	products, ok := bomMarineWarningProducts[state]
	if !ok {
		return out, nil
	}

	var productRefs []bomMarineProductRef
	if products.MarineWindWarningID != "" {
		productRefs = append(productRefs, bomMarineProductRef{ProductID: products.MarineWindWarningID, Slug: bomMarineWindWarningSlug, Category: "wind"})
	}
	if products.HazardousSurfWarningID != "" {
		productRefs = append(productRefs, bomMarineProductRef{ProductID: products.HazardousSurfWarningID, Slug: bomHazardousSurfWarningSlug, Category: "surf"})
	}
	if len(productRefs) == 0 {
		return out, nil
	}

	var lastErr error
	successCount := 0
	for _, ref := range productRefs {
		raw, err := fetch(bomFTPHost, fmt.Sprintf(bomFTPProductPathFmt, ref.ProductID))
		if err != nil {
			lastErr = fmt.Errorf("failed to fetch BOM product %s: %w", ref.ProductID, err)
			continue
		}
		successCount++

		bulletin := parseBomMarineWarningText(raw)
		bulletin.ProductID = ref.ProductID
		bulletin.DetailsURL = bomWarningDetailsURL(ref.Slug, ref.ProductID)
		bulletin.Category = ref.Category

		sections := activeSectionsForZone(bulletin.Sections, zone)
		if len(sections) == 0 {
			continue
		}

		issuedAt := ""
		if !bulletin.IssuedAt.IsZero() {
			issuedAt = bulletin.IssuedAt.UTC().Format(time.RFC3339)
		}

		out.Bulletins = append(out.Bulletins, bulletinOut{
			ID:         bulletin.ProductID,
			Title:      bulletin.Title,
			Category:   bulletin.Category,
			IssuedAt:   issuedAt,
			DetailsURL: bulletin.DetailsURL,
			Sections:   sections,
		})
	}

	if successCount == 0 {
		return fetchWarningsOutput{}, fmt.Errorf("failed to fetch any BOM marine warning products for state %s: %w", state, lastErr)
	}

	return out, nil
}
