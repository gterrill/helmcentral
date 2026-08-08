// Package main: pluggable forecast-warnings-provider host system.
//
// This generalizes the old hardcoded BOM marine warnings subsystem
// (bom_marine_warnings.go / bom_marine_zones.go - Australia-only, zero
// abstraction) into a WASM plugin type, exactly like tide/weather/wave: a
// forecastWarningsProvider interface, a registry, HTTP handlers, and a WASM
// adapter (wasm_forecast_warnings_provider.go). Those old files are NOT
// touched by this phase - they get deleted in a later phase once a BOM
// reference plugin built on this new system replaces them; until then they
// coexist unreferenced by any of the code below.
//
// Unlike tide/weather/wave, the host does NO derivation here: no
// day-bucketing, no zone-matching, no active/cancelled filtering. Different
// providers (BOM's state/zone bounding boxes vs. NWS's UGC marine zone
// codes) have fundamentally incompatible zone taxonomies and determine
// "is this warning currently active" differently (BOM: free-text section
// parsing; NWS: structured CAP alert status fields) - there is nothing
// universal to factor out host-side. Each plugin resolves its own zone(s)
// for the given lat/lon, fetches only the relevant bulletins, and returns
// only currently-active ones. The host just maps the bundle straight into
// the HTTP response.
//
// There is no native built-in forecast-warnings provider -
// forecastWarningsProviderRegistry starts empty and stays that way until a
// WASM plugin is installed in plugins/forecast-warnings, matching
// weatherProvider/waveProvider's shape.
//
// Guest contract (fetch_warnings), mirrored below by
// wasmFetchWarningsOutput in wasm_forecast_warnings_provider.go:
//
//	fetch_warnings({"lat": float64, "lon": float64}) -> {
//	  "region": string,
//	  "bulletins": [
//	    {
//	      "id": string,
//	      "title": string,
//	      "category": string,
//	      "issued_at": string,     // RFC3339, OPTIONAL - may be empty/omitted if unknown
//	      "details_url": string,
//	      "sections": [{"day": string, "warning_type": string}]
//	    }
//	  ]
//	}
package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// forecastWarningSection is one bulletin's forward-looking day/warning-type
// pair - BOM's multi-day bulletins map to multiple sections; NWS's
// point-in-time alerts map to exactly one.
type forecastWarningSection struct {
	Day         string
	WarningType string
}

// forecastWarningBulletin is a single active warning bulletin, already
// filtered to the vessel's position by the plugin. IssuedAt is the zero
// time.Time when the plugin genuinely doesn't know it - never a masked
// placeholder timestamp.
type forecastWarningBulletin struct {
	ID         string
	Title      string
	Category   string
	IssuedAt   time.Time
	DetailsURL string
	Sections   []forecastWarningSection
}

// forecastWarningsBundle is one provider round-trip's worth of data - the
// return value of forecastWarningsProvider.FetchWarnings and of a WASM
// plugin's fetch_warnings, mapped to typed Go values. An empty Bulletins
// slice is a legitimate "no warnings currently active" result, not an
// error. Cached/CachedAt describe the adapter's own cache bookkeeping.
type forecastWarningsBundle struct {
	Region    string
	Bulletins []forecastWarningBulletin
	Cached    bool
	CachedAt  time.Time
}

// forecastWarningsProvider is the interface implemented by each pluggable
// forecast-warnings data source. The reference providers (bom, nws) are
// WASM plugins - there is no native built-in, mirroring
// weatherProvider/waveProvider's shape.
type forecastWarningsProvider interface {
	ID() string
	Name() string
	Description() string
	TTLSeconds() int64
	FetchWarnings(lat, lon float64) (forecastWarningsBundle, error)
}

var forecastWarningsProviderRegistry = map[string]forecastWarningsProvider{}
var forecastWarningsProviderOrder []string

func registerForecastWarningsProvider(p forecastWarningsProvider) {
	forecastWarningsProviderRegistry[p.ID()] = p
	forecastWarningsProviderOrder = append(forecastWarningsProviderOrder, p.ID())
}

func getForecastWarningsProvider(id string) (forecastWarningsProvider, bool) {
	p, ok := forecastWarningsProviderRegistry[id]
	return p, ok
}

type forecastWarningsProviderInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// forecastWarningsProvidersHandler serves GET /api/forecast-warnings-providers,
// mirroring waveProvidersHandler.
func forecastWarningsProvidersHandler(c echo.Context) error {
	result := make([]forecastWarningsProviderInfo, 0, len(forecastWarningsProviderOrder))
	for _, id := range forecastWarningsProviderOrder {
		if provider, ok := forecastWarningsProviderRegistry[id]; ok {
			result = append(result, forecastWarningsProviderInfo{ID: provider.ID(), Name: provider.Name(), Description: provider.Description()})
		}
	}
	return c.JSON(http.StatusOK, result)
}

// defaultForecastWarningsProviderID is used when ui.forecast_warnings_provider
// is unset in settings.yaml.
const defaultForecastWarningsProviderID = "bom"

// resolveForecastWarningsProvider reads ui.forecast_warnings_provider from
// settingsPath (defaulting to defaultForecastWarningsProviderID) and
// resolves it against the registry, mirroring resolveWaveProvider's idiom.
// Returns a clear, actionable error - never a fallback provider - when the
// configured id isn't registered, since an unregistered provider most
// likely means the plugin hasn't been installed yet.
func resolveForecastWarningsProvider(settingsPath string) (forecastWarningsProvider, string, error) {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read settings: %w", err)
	}

	uiMap, _ := settings["ui"].(map[string]any)
	configuredProvider := strings.TrimSpace(coerceString(uiMap["forecast_warnings_provider"]))
	if configuredProvider == "" {
		configuredProvider = defaultForecastWarningsProviderID
	}

	provider, ok := getForecastWarningsProvider(configuredProvider)
	if !ok {
		return nil, configuredProvider, fmt.Errorf("unknown forecast warnings provider configured: %q (is the plugin installed in plugins/forecast-warnings?)", configuredProvider)
	}

	return provider, configuredProvider, nil
}

// --- HTTP response shapes ---

type forecastWarningSectionResponse struct {
	Day         string `json:"day"`
	WarningType string `json:"warning_type"`
}

type forecastWarningBulletinResponse struct {
	ID         string                           `json:"id"`
	Title      string                           `json:"title"`
	Category   string                           `json:"category"`
	IssuedAt   string                           `json:"issued_at,omitempty"`
	DetailsURL string                           `json:"details_url"`
	Sections   []forecastWarningSectionResponse `json:"sections"`
}

type forecastWarningsResponse struct {
	Provider   string                            `json:"provider"`
	Region     string                            `json:"region"`
	Bulletins  []forecastWarningBulletinResponse `json:"bulletins"`
	Cached     bool                              `json:"cached"`
	UpdatedAt  string                            `json:"updated_at"`
	TTLSeconds int64                             `json:"ttl_seconds"`
}

// forecastWarningsETagData is the subset of forecastWarningsResponse the
// ETag is computed over - excludes cached/updated_at/ttl_seconds so the
// ETag reflects actual content changes, not the adapter's own cache
// bookkeeping timestamps, mirroring weatherTodayETagData's reasoning.
type forecastWarningsETagData struct {
	Provider  string                            `json:"provider"`
	Region    string                            `json:"region"`
	Bulletins []forecastWarningBulletinResponse `json:"bulletins"`
}

func mapForecastWarningBulletinResponse(b forecastWarningBulletin) forecastWarningBulletinResponse {
	sections := make([]forecastWarningSectionResponse, 0, len(b.Sections))
	for _, s := range b.Sections {
		sections = append(sections, forecastWarningSectionResponse{Day: s.Day, WarningType: s.WarningType})
	}

	issuedAt := ""
	if !b.IssuedAt.IsZero() {
		issuedAt = b.IssuedAt.UTC().Format(time.RFC3339)
	}

	return forecastWarningBulletinResponse{
		ID:         b.ID,
		Title:      b.Title,
		Category:   b.Category,
		IssuedAt:   issuedAt,
		DetailsURL: b.DetailsURL,
		Sections:   sections,
	}
}

// --- HTTP handlers ---

// forecastWarningsHandler serves GET /api/forecast-warnings: SignalK vessel
// position -> provider.FetchWarnings -> response, mirroring waveForecast's
// fail-fast behavior (502 on unknown/misconfigured provider, 502 on fetch
// error - never fake data). Unlike waveForecast, an empty bulletins list is
// NOT an error: "no warnings currently active" is a legitimate successful
// result. No day-bucketing or local-timezone logic is needed - the bundle's
// fields map straight into the response.
func forecastWarningsHandler(c echo.Context) error {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	provider, configuredProvider, err := resolveForecastWarningsProvider(settingsPath)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}

	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}
	signalkURL := buildSignalKURL(address, port)
	vesselPath := getEnv("SIGNALK_VESSEL_PATH", "/signalk/v1/api/vessels/self")
	if signalkURL == "" {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "SignalK URL is not configured"})
	}

	vesselState, vesselErr := fetchSignalKVesselState(signalkURL, vesselPath)
	if vesselErr != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to fetch vessel state: %v", vesselErr)})
	}
	if !hasUsableVesselPosition(vesselState.Latitude, vesselState.Longitude) {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "invalid vessel coordinates from SignalK"})
	}

	bundle, fetchErr := provider.FetchWarnings(vesselState.Latitude, vesselState.Longitude)
	if fetchErr != nil {
		log.Printf("forecast warnings provider %q error: %v", configuredProvider, fetchErr)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("forecast warnings provider %q unavailable: %v", configuredProvider, fetchErr)})
	}

	bulletins := make([]forecastWarningBulletinResponse, 0, len(bundle.Bulletins))
	for _, b := range bundle.Bulletins {
		bulletins = append(bulletins, mapForecastWarningBulletinResponse(b))
	}

	response := forecastWarningsResponse{
		Provider:   configuredProvider,
		Region:     bundle.Region,
		Bulletins:  bulletins,
		Cached:     bundle.Cached,
		UpdatedAt:  bundle.CachedAt.UTC().Format(time.RFC3339),
		TTLSeconds: provider.TTLSeconds(),
	}

	etag, etagErr := weakETagForJSON(forecastWarningsETagData{
		Provider:  response.Provider,
		Region:    response.Region,
		Bulletins: response.Bulletins,
	})
	if etagErr != nil {
		log.Printf("Failed to build forecast warnings ETag: %v", etagErr)
	}
	return respondJSONWithETag(c, http.StatusOK, etag, response)
}
