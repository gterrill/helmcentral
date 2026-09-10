// Package main: pluggable points-of-interest (POI) provider host system.
//
// This mirrors the wave provider (wave_providers.go / wasm_wave_provider.go)
// almost exactly: a poiProvider interface, a registry, HTTP handlers, and a
// WASM adapter (wasm_poi_provider.go). There is no native built-in - the
// registry starts empty and stays that way until a WASM plugin is installed
// in plugins/poi, matching every other provider kind in this codebase.
//
// Guest contract (fetch_poi), mirrored below by wasmFetchPOIOutput in
// wasm_poi_provider.go:
//
//	fetch_poi({"lat": float64, "lon": float64, "radius_m": int,
//	           "categories": [string], "limit": int}) -> {
//	  "features": [{id, category, name, lat, lon, detail, source_url}],
//	  "truncated": [string],
//	  "unsupported": [string]
//	}
//
// A plugin returns raw, undecorated features only - no distance, no bearing,
// no ranking. The host is the one place every provider's output has to agree
// on units and behaviour, so it alone computes distance and bearing (from
// the vessel's live position, via haversineMeters and bearingDeg), drops
// features outside the requested radius, dedupes near-identical features
// (the same real-world feature is a common double-hit across OSM tag
// combinations), sorts by distance, and applies the requested limit. A
// plugin that tried to do any of that itself would just be redoing the same
// arithmetic slightly differently from every other plugin.
//
// "truncated" names categories where the plugin's own per-category cap was
// hit (there were more matching features upstream than it returned) and
// "unsupported" names categories the plugin has no mapping for at all (e.g.
// a Google Places category with no matching place type) - both pass through
// to the response unchanged, since only the plugin knows which of its own
// limits it hit.
package main

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// poiCategoryIDs is the fixed, ordered set of POI categories every provider
// is expected to understand. A plugin that cannot serve a requested category
// reports it in "unsupported" rather than the host silently dropping it.
var poiCategoryIDs = []string{
	"anchorage", "bay", "island", "marina", "fuel", "ramp",
	"mooring", "historic", "viewpoint", "dive", "trail",
}

var poiCategorySet = func() map[string]bool {
	set := make(map[string]bool, len(poiCategoryIDs))
	for _, id := range poiCategoryIDs {
		set[id] = true
	}
	return set
}()

func isValidPOICategory(id string) bool { return poiCategorySet[id] }

// poiFeatureRaw is one feature exactly as a plugin returned it: identity,
// category, position and optional enrichment, with no host-derived fields
// yet (see poiFeature for the decorated, wire-facing shape).
type poiFeatureRaw struct {
	ID        string
	Category  string
	Name      string
	Lat       float64
	Lon       float64
	Detail    string
	SourceURL string
}

// poiFetchResult is one provider round-trip's worth of data - the return
// value of poiProvider.FetchPOI and of a WASM plugin's fetch_poi, mapped to
// typed Go values. Cached/CachedAt describe the adapter's own cache
// bookkeeping, independent of anything inside Features.
type poiFetchResult struct {
	Features    []poiFeatureRaw
	Truncated   []string
	Unsupported []string
	Cached      bool
	CachedAt    time.Time
}

// poiProvider is the interface implemented by each pluggable POI data
// source. radiusM/categories/limit are passed straight through to the
// plugin as a courtesy upper bound (so e.g. a paid API need not overfetch);
// the host applies its own authoritative radius/limit filtering afterwards
// regardless of what the plugin returns.
type poiProvider interface {
	ID() string
	Name() string
	Description() string
	TTLSeconds() int64
	FetchPOI(lat, lon float64, radiusM int, categories []string, limit int) (poiFetchResult, error)
}

var poiProviderRegistry = map[string]poiProvider{}
var poiProviderOrder []string

func registerPOIProvider(p poiProvider) {
	poiProviderRegistry[p.ID()] = p
	poiProviderOrder = append(poiProviderOrder, p.ID())
}

func getPOIProvider(id string) (poiProvider, bool) {
	p, ok := poiProviderRegistry[id]
	return p, ok
}

type poiProviderInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// poiProvidersHandler serves GET /api/poi-providers, mirroring
// waveProvidersHandler.
func poiProvidersHandler(c echo.Context) error {
	result := make([]poiProviderInfo, 0, len(poiProviderOrder))
	for _, id := range poiProviderOrder {
		if provider, ok := poiProviderRegistry[id]; ok {
			result = append(result, poiProviderInfo{ID: provider.ID(), Name: provider.Name(), Description: provider.Description()})
		}
	}
	return c.JSON(http.StatusOK, result)
}

// defaultPOIProviderID is used when ui.poi_provider is unset in
// settings.yaml. OpenStreetMap via Overpass needs no API key, so a fresh
// install gets a working Nearby feature with zero configuration - the same
// reasoning ADR 0018 gives for Open-Meteo/Open-Meteo Marine.
const defaultPOIProviderID = "osm-overpass"

// resolvePOIProvider reads ui.poi_provider from settingsPath (defaulting to
// defaultPOIProviderID) and resolves it against the registry, mirroring
// resolveWaveProvider's idiom. Returns a clear, actionable error - never a
// fallback provider - when the configured id isn't registered.
func resolvePOIProvider(settingsPath string) (poiProvider, string, error) {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read settings: %w", err)
	}

	uiMap, _ := settings["ui"].(map[string]any)
	configuredProvider := strings.TrimSpace(coerceString(uiMap["poi_provider"]))
	if configuredProvider == "" {
		configuredProvider = defaultPOIProviderID
	}

	provider, ok := getPOIProvider(configuredProvider)
	if !ok {
		return nil, configuredProvider, fmt.Errorf("unknown POI provider configured: %q (is the plugin installed in plugins/poi?)", configuredProvider)
	}

	return provider, configuredProvider, nil
}

// ── host-side derivation: distance, bearing ────────────────────────────────

// bearingDeg is the initial great-circle bearing from (lat1, lon1) to
// (lat2, lon2), in compass degrees (0 = north, clockwise, 0-360). Ported
// from frontend/src/lib/geo.ts's bearingDeg, which this codebase already
// relies on client-side (the anchor-watch map, route planner) but which had
// no backend equivalent before this - signalk.go's destinationPoint solves
// the opposite problem (project forward from a bearing), and haversineMeters
// gives distance only. The POI feature list is the first host-side consumer
// that needs an initial bearing between two known points.
func bearingDeg(lat1, lon1, lat2, lon2 float64) float64 {
	toRad := func(d float64) float64 { return d * math.Pi / 180 }
	toDeg := func(r float64) float64 { return r * 180 / math.Pi }

	dLon := toRad(lon2 - lon1)
	y := math.Sin(dLon) * math.Cos(toRad(lat2))
	x := math.Cos(toRad(lat1))*math.Sin(toRad(lat2)) - math.Sin(toRad(lat1))*math.Cos(toRad(lat2))*math.Cos(dLon)

	deg := math.Mod(toDeg(math.Atan2(y, x))+360, 360)
	return deg
}

// ── HTTP response shapes ────────────────────────────────────────────────────

type poiFeature struct {
	ID         string  `json:"id"`
	Category   string  `json:"category"`
	Name       string  `json:"name"`
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
	DistanceM  float64 `json:"distance_m"`
	BearingDeg float64 `json:"bearing_deg"`
	Detail     string  `json:"detail"`
	SourceURL  string  `json:"source_url"`
}

type poiCenterResponse struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type poiResponse struct {
	Center      poiCenterResponse `json:"center"`
	RadiusNm    float64           `json:"radius_nm"`
	Categories  []string          `json:"categories"`
	Provider    string            `json:"provider"`
	Cached      bool              `json:"cached"`
	FetchedAt   string            `json:"fetched_at"`
	Features    []poiFeature      `json:"features"`
	Truncated   []string          `json:"truncated"`
	Unsupported []string          `json:"unsupported"`
}

const (
	poiRadiusNmMin  float64 = 0.5
	poiRadiusNmMax  float64 = 25
	poiLimitMin             = 1
	poiLimitMax             = 100
	poiLimitDefault         = 50

	// poiDedupeRoundingDP is the decimal-place precision two features'
	// positions must match to, in addition to sharing a name, before the
	// later one is treated as a duplicate of the earlier one. 5dp is about
	// 1.1m at the equator - tight enough to only catch the same real-world
	// feature returned twice (e.g. once as a node, once as the "center" of
	// an overlapping way), never two genuinely distinct features that
	// happen to share a name.
	poiDedupeRoundingDP = 5
)

// poiDedupeKey builds the dedupe key for one feature: lowercased, trimmed
// name plus position rounded to poiDedupeRoundingDP decimal places, and
// whether the key is usable at all. A feature with no name is never deduped
// against anything - an empty name is not a useful identity to collapse on,
// and doing so would merge unrelated unnamed features onto a single "".
func poiDedupeKey(name string, lat, lon float64) (key string, usable bool) {
	trimmed := strings.ToLower(strings.TrimSpace(name))
	if trimmed == "" {
		return "", false
	}
	scale := math.Pow(10, poiDedupeRoundingDP)
	roundedLat := math.Round(lat*scale) / scale
	roundedLon := math.Round(lon*scale) / scale
	return fmt.Sprintf("%s|%.5f|%.5f", trimmed, roundedLat, roundedLon), true
}

// parsePOICategories validates the comma-separated categories query param:
// non-empty, every entry a known category id, duplicates collapsed.
func parsePOICategories(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("missing categories")
	}

	seen := make(map[string]bool)
	var categories []string
	for _, part := range strings.Split(raw, ",") {
		id := strings.TrimSpace(part)
		if id == "" {
			continue
		}
		if !isValidPOICategory(id) {
			return nil, fmt.Errorf("unknown category %q", id)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		categories = append(categories, id)
	}
	if len(categories) == 0 {
		return nil, fmt.Errorf("missing categories")
	}
	return categories, nil
}

// poiNearby serves GET /api/poi?radius_nm=&categories=&limit=: POIs near the
// vessel's live position, host-ranked from whatever the configured provider
// returns. Position comes from fetchSignalKVesselState, gated by
// hasUsableVesselPosition exactly as waveForecast does (502, never a fake
// position). radius_nm and categories are required; limit defaults to 50.
func poiNearby(c echo.Context) error {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	provider, configuredProvider, err := resolvePOIProvider(settingsPath)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}

	radiusNmRaw := strings.TrimSpace(c.QueryParam("radius_nm"))
	if radiusNmRaw == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "missing radius_nm"})
	}
	radiusNm, err := strconv.ParseFloat(radiusNmRaw, 64)
	if err != nil || radiusNm < poiRadiusNmMin || radiusNm > poiRadiusNmMax {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("radius_nm must be between %g and %g", poiRadiusNmMin, poiRadiusNmMax)})
	}

	categories, err := parsePOICategories(c.QueryParam("categories"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	limit := poiLimitDefault
	if limitRaw := strings.TrimSpace(c.QueryParam("limit")); limitRaw != "" {
		limit, err = strconv.Atoi(limitRaw)
		if err != nil || limit < poiLimitMin || limit > poiLimitMax {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("limit must be an integer between %d and %d", poiLimitMin, poiLimitMax)})
		}
	}

	vesselState, vesselErr := fetchSignalKVesselState()
	if vesselErr != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to fetch vessel state: %v", vesselErr)})
	}
	if !hasUsableVesselPosition(vesselState.Latitude, vesselState.Longitude) {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "invalid vessel coordinates from SignalK"})
	}

	radiusM := int(math.Round(radiusNm * metersPerNauticalMile))

	result, fetchErr := provider.FetchPOI(vesselState.Latitude, vesselState.Longitude, radiusM, categories, limit)
	if fetchErr != nil {
		log.Printf("POI provider %q error: %v", configuredProvider, fetchErr)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("POI provider %q unavailable: %v", configuredProvider, fetchErr)})
	}

	features := decorateAndRankPOIFeatures(result.Features, vesselState.Latitude, vesselState.Longitude, float64(radiusM), limit)

	response := poiResponse{
		Center:      poiCenterResponse{Lat: vesselState.Latitude, Lon: vesselState.Longitude},
		RadiusNm:    radiusNm,
		Categories:  categories,
		Provider:    configuredProvider,
		Cached:      result.Cached,
		FetchedAt:   result.CachedAt.UTC().Format(time.RFC3339),
		Features:    features,
		Truncated:   nonNilStrings(result.Truncated),
		Unsupported: nonNilStrings(result.Unsupported),
	}

	etag, etagErr := weakETagForJSON(response)
	if etagErr != nil {
		log.Printf("Failed to build POI ETag: %v", etagErr)
	}
	return respondJSONWithETag(c, http.StatusOK, etag, response)
}

// nonNilStrings returns an empty, non-nil slice for a nil input so the wire
// shape is always a JSON array, never null.
func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// decorateAndRankPOIFeatures is the host-side derivation step every plugin
// is deliberately kept out of: compute distance and bearing from the
// vessel's position, drop anything outside radiusM, dedupe near-identical
// features (see poiDedupeKey), sort by distance ascending, and cap at limit.
func decorateAndRankPOIFeatures(raw []poiFeatureRaw, vesselLat, vesselLon, radiusM float64, limit int) []poiFeature {
	seen := make(map[string]bool, len(raw))
	decorated := make([]poiFeature, 0, len(raw))

	for _, f := range raw {
		distanceM := haversineMeters(vesselLat, vesselLon, f.Lat, f.Lon)
		if distanceM > radiusM {
			continue
		}

		if key, usable := poiDedupeKey(f.Name, f.Lat, f.Lon); usable {
			if seen[key] {
				continue
			}
			seen[key] = true
		}

		decorated = append(decorated, poiFeature{
			ID:         f.ID,
			Category:   f.Category,
			Name:       f.Name,
			Lat:        f.Lat,
			Lon:        f.Lon,
			DistanceM:  distanceM,
			BearingDeg: bearingDeg(vesselLat, vesselLon, f.Lat, f.Lon),
			Detail:     f.Detail,
			SourceURL:  f.SourceURL,
		})
	}

	sort.Slice(decorated, func(i, j int) bool { return decorated[i].DistanceM < decorated[j].DistanceM })

	if len(decorated) > limit {
		decorated = decorated[:limit]
	}
	return decorated
}
