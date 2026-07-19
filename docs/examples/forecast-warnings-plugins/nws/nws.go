// nws.go holds the JSON-mapping, event-categorization, cancellation-filtering
// and fetch-orchestration logic for the NWS (US National Weather Service)
// forecast-warnings plugin - a reference (non-default) implementation of
// Helmcentral's pluggable Forecast Warnings provider contract
// (backend/forecast_warnings_providers.go).
//
// Unlike ../bom, this plugin talks to a completely open, keyless,
// HTTP-reachable API (api.weather.gov) - no custom host function needed, just
// Extism's built-in HTTP support. The actual network calls still need
// "github.com/extism/go-pdk" (for pdk.NewHTTPRequest), so - mirroring
// ../bom/bom.go's split, which injects its FTP fetch step as a plain func so
// the rest of the file stays host-testable - this file takes its HTTP fetch
// step as an injected httpFetcher, keeping the actual JSON-mapping/
// categorization/filtering logic (and this file as a whole) free of any
// go-pdk dependency, so it (and main_test.go) can be built and tested with
// the plain host Go toolchain (`go test ./...`, no TinyGo/wasm target
// needed). main.go supplies the real fetch implementation, which does depend
// on go-pdk and is gated `//go:build tinygo` for that reason.
//
// NWS endpoints used (shapes confirmed live during planning, see
// /Users/gavinator/.claude/plans/i-want-to-create-wild-fountain.md):
//
//   - Marine zone lookup by point:
//     GET https://api.weather.gov/zones?type=marine&point=<lat>,<lon>
//     -> GeoJSON FeatureCollection; features[0].properties.id is the zone
//     code (e.g. "GMZ554", "AMZ135"), features[0].properties.name is the
//     human-readable zone name. An empty features array means the point
//     isn't inside any NWS marine zone (e.g. outside US coastal waters) -
//     not an error, just zero zones (and therefore zero bulletins) - the
//     same "no coverage here" philosophy as BOM's SA/NT gap.
//
//   - Active alerts for a zone:
//     GET https://api.weather.gov/alerts/active?zone=<zone-id>
//     -> GeoJSON FeatureCollection; each features[i].properties carries
//     (among others) id (a stable "urn:oid:..." identifier), "@id" (the
//     alert's own canonical api.weather.gov URL - reused as details_url
//     since it's a real, resolvable link, unlike a fabricated URL pattern),
//     event, headline, sent (RFC3339), status ("Actual" for real alerts;
//     anything else, e.g. "Test"/"Exercise", is not surfaced), and
//     messageType ("Cancel" means the alert has been cancelled and is
//     excluded, mirroring BOM's cancellation filtering).
//
// This is a teaching example, not a production-hardened client: only the
// first matching marine zone is used (there should generally be exactly one
// for a given point; if a point resolves into multiple zones, the rest are
// silently ignored) and there's no retry/backoff on transient NWS errors - a
// single failed request just fails the call; the host's disk cache
// (wasm_forecast_warnings_provider.go) covers short outages by serving a
// stale result.
package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	nwsZonesURLFmt  = "https://api.weather.gov/zones?type=marine&point=%.4f,%.4f"
	nwsAlertsURLFmt = "https://api.weather.gov/alerts/active?zone=%s"
)

// --- NWS API response shapes (only the fields this plugin uses) ---

type nwsZoneProperties struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type nwsZoneFeature struct {
	Properties nwsZoneProperties `json:"properties"`
}

type nwsZonesResponse struct {
	Features []nwsZoneFeature `json:"features"`
}

type nwsAlertProperties struct {
	ID          string `json:"id"`
	CanonicalID string `json:"@id"`
	Event       string `json:"event"`
	Headline    string `json:"headline"`
	Sent        string `json:"sent"`
	Status      string `json:"status"`
	MessageType string `json:"messageType"`
}

type nwsAlertFeature struct {
	Properties nwsAlertProperties `json:"properties"`
}

type nwsAlertsResponse struct {
	Features []nwsAlertFeature `json:"features"`
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

// httpFetcher is the shape of the injected HTTP GET step - main.go's real
// implementation calls pdk.NewHTTPRequest with a NWS-required User-Agent
// header; tests inject a fake so this whole file stays testable on the plain
// host Go toolchain with no WASM runtime involved. It reports the HTTP
// status code and raw body on a successful round trip; err is reserved for
// genuine transport-level failures (dial/timeout/etc.), not non-2xx statuses
// - callers check status themselves, mirroring how the NOAA tide plugin
// (docs/examples/tide-plugins/noaa/main.go) checks res.Status() rather than
// treating every non-2xx as a Go error.
type httpFetcher func(url string) (status int, body []byte, err error)

// categorizeNWSEvent derives a short, opaque lowercase category tag from an
// NWS alert's "event" field, loosely matching BOM's own "wind"/"surf"
// vocabulary so the frontend's existing "wind"-category special-case keeps
// working across providers. This mapping table is deliberately small and not
// a claim of exhaustive NWS event-type coverage (NWS defines >80 alert
// types; only the marine-relevant wind/sea-state ones are special-cased) -
// anything unmatched falls back to a lowercased, hyphenated slug of the
// event name itself, so the category is always non-empty and reasonable.
func categorizeNWSEvent(event string) string {
	lower := strings.ToLower(event)
	switch {
	case strings.Contains(lower, "wind"), strings.Contains(lower, "gale"), strings.Contains(lower, "storm"), strings.Contains(lower, "craft"):
		return "wind"
	case strings.Contains(lower, "surf"), strings.Contains(lower, "swell"), strings.Contains(lower, "rip"):
		return "surf"
	default:
		return slugifyEventName(event)
	}
}

// slugifyEventName lowercases and hyphenates an NWS event name (e.g.
// "Special Marine Warning" -> "special-marine-warning") as the fallback
// category for event types not covered by categorizeNWSEvent's explicit
// wind/surf buckets.
func slugifyEventName(event string) string {
	trimmed := strings.TrimSpace(event)
	if trimmed == "" {
		return ""
	}
	return strings.Join(strings.Fields(strings.ToLower(trimmed)), "-")
}

// isReportableAlert reports whether an NWS alert should be surfaced:
// status must be "Actual" (excludes "Test"/"Exercise" alerts, which are not
// real warnings) and messageType must not be "Cancel" (a cancelled alert is
// not currently active) - mirroring BOM's hasActiveWarning's exclusion of
// "Cancellation" sections.
func isReportableAlert(p nwsAlertProperties) bool {
	return p.Status == "Actual" && p.MessageType != "Cancel"
}

// nwsAlertsSearchURL builds a fallback public details link for a zone when
// an alert's own "@id" canonical link is unavailable (defensive only -
// every alert observed live during planning had "@id" populated).
func nwsAlertsSearchURL(zoneID string) string {
	return fmt.Sprintf("https://alerts.weather.gov/search?zone=%s", zoneID)
}

// alertToBulletin maps one NWS CAP alert's properties to the contract's
// bulletinOut shape. NWS alerts are point-in-time, not BOM's forward-looking
// multi-day format, so each bulletin has exactly one section with an empty
// Day (day-bucketing is optional/informational per the contract).
func alertToBulletin(p nwsAlertProperties, zoneID string) bulletinOut {
	title := p.Headline
	if title == "" {
		title = p.Event
	}

	detailsURL := p.CanonicalID
	if detailsURL == "" {
		detailsURL = nwsAlertsSearchURL(zoneID)
	}

	return bulletinOut{
		ID:         p.ID,
		Title:      title,
		Category:   categorizeNWSEvent(p.Event),
		IssuedAt:   p.Sent,
		DetailsURL: detailsURL,
		Sections:   []sectionOut{{Day: "", WarningType: p.Event}},
	}
}

// mapZonesResponse parses a raw zones-by-point JSON body into the resolved
// zone's properties. Returns ok=false (not an error) when the response
// parses cleanly but has zero features - a legitimate "outside NWS marine
// zone coverage" result. Only the first matching zone is used - see this
// file's top doc comment.
func mapZonesResponse(body []byte) (nwsZoneProperties, bool, error) {
	var parsed nwsZonesResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nwsZoneProperties{}, false, fmt.Errorf("unparseable NWS zone lookup response: %w", err)
	}
	if len(parsed.Features) == 0 {
		return nwsZoneProperties{}, false, nil
	}
	return parsed.Features[0].Properties, true, nil
}

// mapAlertsResponse parses a raw alerts-for-zone JSON body into bulletins,
// applying isReportableAlert filtering. A genuine parse failure is an error;
// zero surviving alerts is not.
func mapAlertsResponse(body []byte, zoneID string) ([]bulletinOut, error) {
	var parsed nwsAlertsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("unparseable NWS alerts response: %w", err)
	}

	bulletins := make([]bulletinOut, 0, len(parsed.Features))
	for _, f := range parsed.Features {
		if !isReportableAlert(f.Properties) {
			continue
		}
		bulletins = append(bulletins, alertToBulletin(f.Properties, zoneID))
	}
	return bulletins, nil
}

// buildFetchWarningsOutput is the fetch_warnings orchestration logic:
// resolve the containing NWS marine zone for the given position, fetch
// active alerts for that zone, filter to reportable ones, and map each to a
// bulletin. Ported to the "fetch+filter+return" shape wasmForecastWarningsProvider
// expects - the host already owns TTL caching generically (see
// backend/wasm_forecast_warnings_provider.go), this plugin does not cache.
//
// An unmapped position (zero marine zones at this point) is NOT an error -
// it's a legitimate "nothing to report here" result, mirroring BOM's
// unmapped-state/SA-NT handling. A genuine network/parse failure on either
// call IS an error (fail-fast, no fabricated data) - this differs from
// BOM's "one product fails, others still succeed" partial-failure tolerance
// only because NWS's contract has exactly one zone lookup and one alerts
// call, not several independent per-product fetches to partially succeed
// across.
func buildFetchWarningsOutput(lat, lon float64, fetch httpFetcher) (fetchWarningsOutput, error) {
	out := fetchWarningsOutput{Bulletins: []bulletinOut{}}

	zonesURL := fmt.Sprintf(nwsZonesURLFmt, lat, lon)
	status, body, err := fetch(zonesURL)
	if err != nil {
		return fetchWarningsOutput{}, fmt.Errorf("failed to fetch NWS marine zone for %.4f,%.4f: %w", lat, lon, err)
	}
	if status < 200 || status >= 300 {
		return fetchWarningsOutput{}, fmt.Errorf("NWS zone lookup failed for %.4f,%.4f: status %d", lat, lon, status)
	}

	zone, ok, err := mapZonesResponse(body)
	if err != nil {
		return fetchWarningsOutput{}, err
	}
	if !ok {
		return out, nil
	}
	out.Region = zone.Name

	alertsURL := fmt.Sprintf(nwsAlertsURLFmt, zone.ID)
	status, body, err = fetch(alertsURL)
	if err != nil {
		return fetchWarningsOutput{}, fmt.Errorf("failed to fetch NWS alerts for zone %s: %w", zone.ID, err)
	}
	if status < 200 || status >= 300 {
		return fetchWarningsOutput{}, fmt.Errorf("NWS alerts lookup failed for zone %s: status %d", zone.ID, status)
	}

	bulletins, err := mapAlertsResponse(body, zone.ID)
	if err != nil {
		return fetchWarningsOutput{}, err
	}
	out.Bulletins = bulletins

	return out, nil
}
