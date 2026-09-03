// Package main: pluggable upper-air (500mb) provider host system.
//
// Upper air gets its own provider category rather than riding on the weather
// provider, for a reason found the hard way: this vessel runs Apple WeatherKit
// for surface weather, and WeatherKit carries no pressure levels at all.
// Folding upper air into fetch_weather would have made the whole feature
// dormant for anyone whose weather source happens not to have it, and would
// have forced a choice between good surface weather and any upper air.
// Separating them lets a boat run WeatherKit for one and Open-Meteo for the
// other (ADR 0071).
//
// Guest contract (fetch_upper_air), mirrored by wasmFetchUpperAirOutput in
// wasm_upper_air_provider.go:
//
//	fetch_upper_air({"lat": float64, "lon": float64, "days": int}) -> {
//	  "hourly": [{time, geopotential_height_500_m, geopotential_height_1000_m,
//	              wind_speed_500_ms, temperature_500_c}],
//	  "grid": [{lat, lon, hourly: [{time, geopotential_height_500_m}]}] (optional)
//	}
//
// All times are RFC3339, all values SI. "grid" is optional and unused today;
// it is in the contract from the start so trough detection over an area can be
// added without a breaking contract change.
//
// A zero geopotential height means "no reading here" rather than sea level: a
// 0m 500mb height is not a measurement of anything, and a model without
// pressure levels reports exactly that. The host treats it as absence.
package main

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// upperAirHourPoint is a single hour above the vessel, SI units.
type upperAirHourPoint struct {
	Time                    time.Time
	GeopotentialHeight500M  float64
	GeopotentialHeight1000M float64
	WindSpeed500MS          float64
	Temperature500C         float64
}

// upperAirBundle is one provider round-trip's worth of data.
type upperAirBundle struct {
	Hourly   []upperAirHourPoint
	Cached   bool
	CachedAt time.Time
}

type upperAirProvider interface {
	ID() string
	Name() string
	Description() string
	TTLSeconds() int64
	FetchUpperAir(lat, lon float64, days int) (upperAirBundle, error)
}

var upperAirProviderRegistry = map[string]upperAirProvider{}
var upperAirProviderOrder []string

func registerUpperAirProvider(p upperAirProvider) {
	upperAirProviderRegistry[p.ID()] = p
	upperAirProviderOrder = append(upperAirProviderOrder, p.ID())
}

func getUpperAirProvider(id string) (upperAirProvider, bool) {
	p, ok := upperAirProviderRegistry[id]
	return p, ok
}

type upperAirProviderInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func upperAirProvidersHandler(c echo.Context) error {
	result := make([]upperAirProviderInfo, 0, len(upperAirProviderOrder))
	for _, id := range upperAirProviderOrder {
		if provider, ok := upperAirProviderRegistry[id]; ok {
			result = append(result, upperAirProviderInfo{ID: provider.ID(), Name: provider.Name(), Description: provider.Description()})
		}
	}
	return c.JSON(http.StatusOK, result)
}

const defaultUpperAirProviderID = "open-meteo-upper"

// resolveUpperAirProvider reads ui.upper_air_provider from settings.
//
// Unlike the other categories this one is genuinely optional: upper air is an
// extra on the forecast page rather than a section that fails visibly without
// it. An unset provider with nothing installed is therefore not an error, and
// returns a nil provider that the handler answers as "no upper air" rather
// than as a fault.
func resolveUpperAirProvider(settingsPath string) (upperAirProvider, string, error) {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read settings: %w", err)
	}

	uiMap, _ := settings["ui"].(map[string]any)
	configured := strings.TrimSpace(coerceString(uiMap["upper_air_provider"]))
	explicit := configured != ""
	if !explicit {
		configured = defaultUpperAirProviderID
	}

	provider, ok := getUpperAirProvider(configured)
	if !ok {
		if explicit {
			// Naming a provider that is not installed is a real mistake and
			// says so, rather than quietly serving nothing.
			return nil, configured, fmt.Errorf("unknown upper-air provider configured: %q (is the plugin installed in plugins/upper-air?)", configured)
		}
		return nil, configured, nil
	}

	return provider, configured, nil
}

// --- HTTP response shapes ---

type upperAirDayResponse struct {
	DayKey  string             `json:"day_key"`
	Date    string             `json:"date"`
	DayName string             `json:"day_name"`
	Outlook upperAirDayOutlook `json:"outlook"`
}

type upperAirResponse struct {
	Provider   string                `json:"provider"`
	Days       []upperAirDayResponse `json:"days"`
	Cached     bool                  `json:"cached"`
	UpdatedAt  string                `json:"updated_at"`
	TTLSeconds int64                 `json:"ttl_seconds"`
}

// upperAirForecast serves GET /api/upper-air.
//
// Answers 200 with an empty day list when no provider is installed, since a
// boat without an upper-air plugin is a normal configuration rather than a
// fault. A provider that is configured but broken still 502s.
func upperAirForecast(c echo.Context) error {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	provider, configured, err := resolveUpperAirProvider(settingsPath)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	if provider == nil {
		return c.JSON(http.StatusOK, upperAirResponse{Provider: "", Days: []upperAirDayResponse{}})
	}

	vesselState, vesselErr := fetchSignalKVesselState()
	if vesselErr != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to fetch vessel state: %v", vesselErr)})
	}
	if !hasUsableVesselPosition(vesselState.Latitude, vesselState.Longitude) {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "invalid vessel coordinates from SignalK"})
	}

	bundle, fetchErr := provider.FetchUpperAir(vesselState.Latitude, vesselState.Longitude, upperAirForecastDays)
	if fetchErr != nil {
		log.Printf("upper-air provider %q error: %v", configured, fetchErr)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("upper-air provider %q unavailable: %v", configured, fetchErr)})
	}

	localLocation := vesselLocalLocation(vesselState.Longitude)
	referenceDatetime := vesselState.Datetime
	if referenceDatetime.IsZero() {
		referenceDatetime = time.Now().UTC()
	}
	localTodayKey := referenceDatetime.In(localLocation).Format("2006-01-02")

	byDay := buildUpperAirInputs(bundle.Hourly, localLocation)
	dayKeys := make([]string, 0, len(byDay))
	for dayKey := range byDay {
		if dayKey < localTodayKey {
			continue
		}
		dayKeys = append(dayKeys, dayKey)
	}
	sort.Strings(dayKeys)

	inputs := make([]upperAirDayInput, 0, len(dayKeys))
	for _, dayKey := range dayKeys {
		inputs = append(inputs, byDay[dayKey])
	}

	outlooks := upperAirOutlook(inputs)
	days := make([]upperAirDayResponse, 0, len(dayKeys))
	for i, dayKey := range dayKeys {
		localDay, parseErr := time.ParseInLocation("2006-01-02", dayKey, localLocation)
		if parseErr != nil {
			continue
		}
		days = append(days, upperAirDayResponse{
			DayKey:  dayKey,
			Date:    localDay.Format("Jan 2"),
			DayName: localDay.Weekday().String(),
			Outlook: outlooks[i],
		})
	}

	response := upperAirResponse{
		Provider:   configured,
		Days:       days,
		Cached:     bundle.Cached,
		UpdatedAt:  bundle.CachedAt.UTC().Format(time.RFC3339),
		TTLSeconds: provider.TTLSeconds(),
	}

	etag, etagErr := weakETagForJSON(response)
	if etagErr != nil {
		log.Printf("Failed to build upper-air ETag: %v", etagErr)
	}
	return respondJSONWithETag(c, http.StatusOK, etag, response)
}

// upperAirForecastDays is the window the outlook is judged across. Surviving
// the Storm says to watch the upper pattern "for ten days to two weeks before
// your departure date" (p62); a global model reaches 16.
const upperAirForecastDays = 16
