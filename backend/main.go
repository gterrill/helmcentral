package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

const (
	defaultSignalKAddress         = "localhost"
	defaultSignalKPort            = 3000
	metersPerSecondToKnots        = 1.943844
	defaultWindMaxAge             = 5 * time.Minute
	defaultRPMMaxAge              = 30 * time.Second
	defaultHouseBatteryCapacityAh = 1440
)

var (
	buildVersion  = "dev"
	buildRevision = "unknown"
)

// gustWindowLadder is the shared, ordered set of "max gust" windows exposed
// via the vessel-state API's max_gust_kts field (and the frontend's MAX GUST
// cards, which cycle through it shortest-to-longest). It is the single
// source of truth for these windows - do not duplicate this literal
// elsewhere in the package.
var gustWindowLadder = []string{"10m", "30m", "1h", "24h"}

type vesselStateData struct {
	Name                        string
	Status                      string
	Datetime                    time.Time
	Depth                       float64
	CurrentDriftKts             float64
	CurrentSetDeg               float64
	CurrentDriftImpactKts       *float64
	Latitude                    float64
	Longitude                   float64
	GNSSQualityIndicator        int
	GNSSHDOP                    float64
	GNSSSatellites              int
	GNSSValidationState         string
	GNSSValidationReason        string
	GNSSCriticalAlert           bool
	HeadingTrue                 float64
	SpeedOverGroundKts          float64
	WindSpeedApparentKts        float64
	WindAngleApparentDeg        float64
	WindSide                    string
	WindAngleRelativeDeg        float64
	GeneratorState              string
	GeneratorManualStart        bool
	GeneratorManualStartTimer   float64
	GeneratorRunningByCondition string
	GeneratorRuntime            float64
	Engine0RPM                  float64
	Engine1RPM                  float64
}

type alternatorInstanceData struct {
	CurrentA float64
	VoltageV float64
	PowerW   float64
	TempC    float64
}

type chargerInstanceData struct {
	CurrentA      float64
	ACIn1CurrentA float64
	ChargingMode  string
	Error         string
}

type solarControllerData struct {
	ID            string
	Label         string
	CurrentW      float64
	TodayKWh      float64
	YesterdayKWh  float64
	Mode          string
	Error         string
	LastUpdateAge float64
	Contribution  float64
}

type solarStateData struct {
	Datetime      time.Time
	CurrentW      float64
	TodayKWh      float64
	YesterdayKWh  float64
	PeakTodayW    float64
	Controllers   []solarControllerData
	Trend24hTotal []solarTrendPoint
}

type solarTrendPoint struct {
	Time   time.Time `json:"time"`
	TotalW float64   `json:"total_w"`
}

type electricalStateData struct {
	Datetime            time.Time
	BatterySocPercent   float64
	BatteryCapacityAh   float64
	ChargingCurrentA    float64
	ChargingPowerW      float64
	SolarOutputW        float64
	ACOutputW           float64
	DC12VPowerW         float64
	DC12VCurrentA       float64
	DC24VVoltageV       float64
	ACLoadsW            float64
	GeneratorRealPowerW float64
	Alternator0         alternatorInstanceData
	Alternator1         alternatorInstanceData
	Charger0            chargerInstanceData
}

type tankLevelData struct {
	ID           string  `json:"id"`
	Label        string  `json:"label"`
	Category     string  `json:"category"`
	Kind         string  `json:"kind"`
	LevelPercent float64 `json:"level_percent"`
}

func main() {
	e := echo.New()
	port := getEnv("PORT", "8080")

	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete},
	}))

	// Encrypted secrets store. Must be opened and loaded into the process
	// environment before any provider registration below, since SignalK and
	// GeoNames code paths read their secrets via getEnv/os.Getenv. Fail fast
	// on open error (including a master-key mismatch against existing
	// encrypted rows) rather than silently running with secrets unavailable.
	ss, err := newSecretsStore(secretsDBPath(), secretsKeyPath())
	if err != nil {
		log.Fatalf("secrets store: %v", err)
	}
	globalSecretsStore = ss
	if err := globalSecretsStore.LoadIntoEnv(); err != nil {
		log.Fatalf("secrets store: %v", err)
	}

	// Plugin allowlist override store (per-plugin allowed_hosts/
	// allowed_secrets overrides settable from the Settings UI instead of
	// hand-editing companion JSON files over SSH). Must also be opened
	// before provider registration below, since loadWasm*Providers ->
	// manifestForWasmPlugin -> allowedHostsForWasmPlugin/
	// allowedSecretsForWasmPlugin check this store first. Fail fast on open
	// error, same reasoning as the other stores here - this store has no
	// encryption/integrity check to run, just a normal sqlite open.
	pos, err := newPluginOverridesStore(pluginOverridesDBPath())
	if err != nil {
		log.Fatalf("plugin overrides store: %v", err)
	}
	globalPluginOverridesStore = pos

	// Tide providers - WASM-plugin-only (no native built-in); registry stays
	// empty (tideToday correctly 502s) until plugins/tides/*.wasm exists,
	// mirroring the weather/wave providers below.
	loadWasmTideProviders(pluginsTidesDir())

	// Weather providers - WASM-plugin-only (no native built-in); registry
	// stays empty (weatherToday/weatherForecast correctly 502) until
	// plugins/weather/*.wasm exists, which a later phase's Docker build step
	// creates.
	loadWasmWeatherProviders(pluginsWeatherDir())

	// Wave providers - WASM-plugin-only (no native built-in), same reasoning
	// as weather above; registry stays empty (waveForecast correctly 502s)
	// until plugins/waves/*.wasm exists.
	loadWasmWaveProviders(pluginsWavesDir())

	// Forecast warnings providers - WASM-plugin-only (no native built-in),
	// same reasoning as weather/wave above; registry stays empty
	// (forecastWarningsHandler correctly 502s) until
	// plugins/forecast-warnings/*.wasm exists.
	loadWasmForecastWarningsProviders(pluginsForecastWarningsDir())

	// Tile cache (backs the Esri World Imagery proxy + area prefetch).
	// Fail fast if it can't be opened rather than silently running with a
	// nil/broken cache.
	tc, err := newTileCache(tileCachePath())
	if err != nil {
		log.Fatalf("failed to open tile cache: %v", err)
	}
	globalTileCache = tc

	// Nearby-vessel contact store (backs the "seen before" history on the
	// Nearby Vessels tile). Fail fast on open error, same reasoning as the
	// tile cache above.
	ncs, err := newNearbyContactStore(nearbyContactsDBPath())
	if err != nil {
		log.Fatalf("failed to open nearby contacts store: %v", err)
	}
	globalNearbyContactStore = ncs

	// World imagery HTTP client for tile fetches (with timeout to prevent hangs).
	worldImageryClient := newWorldImageryHTTPClient()

	// Routes
	e.GET("/api/health", healthCheck)
	e.GET("/api/vessel-state", vesselState)
	e.GET("/api/stream", telemetryStream)
	e.GET("/api/electrical-state", electricalState)
	e.GET("/api/solar-state", solarState)
	e.GET("/api/tanks-state", tanksState)
	e.GET("/api/nearby-vessels", nearbyVessels)
	e.GET("/api/nearby-vessels/:key/sightings", getNearbyVesselSightingsHandler(globalNearbyContactStore))
	e.GET("/api/weather-today", weatherToday)
	e.GET("/api/weather-forecast", weatherForecast)
	e.GET("/api/weather-providers", weatherProvidersHandler)
	e.GET("/api/wave-forecast", waveForecast)
	e.GET("/api/wave-providers", waveProvidersHandler)
	e.GET("/api/forecast-warnings", forecastWarningsHandler)
	e.GET("/api/forecast-warnings-providers", forecastWarningsProvidersHandler)
	e.GET("/api/tide-today", tideToday)
	e.GET("/api/tide-providers", tideProvidersHandler)
	e.GET("/api/tide-stations", tideStationsHandler)
	e.GET("/api/tide-chart", tideChartHandler)
	e.GET("/api/tide-nearest", tideNearestHandler)
	e.GET("/api/place-name", placeName)
	e.GET("/api/settings", getSettingsHandler)
	e.POST("/api/settings", updateSettingsHandler)
	e.GET("/api/settings/signalk", getSignalKSettingsHandler)
	// Probe only — persisting the address is POST /api/settings' job alone
	// (ADR 0028). There is deliberately no POST /api/settings/signalk.
	e.POST("/api/settings/signalk/test", testSignalKConnectionHandler)
	// Also a pure read: sweeps the local network and reports what it found.
	// Accepting a result saves through POST /api/settings like any other write.
	e.POST("/api/signalk/discover", discoverSignalKHandler)
	e.GET("/api/settings/secrets", getSecretsSettingsHandler)
	e.POST("/api/settings/secrets", updateSecretsSettingsHandler)
	e.POST("/api/settings/secrets/import-env", importEnvSecretsHandler)
	e.GET("/api/plugins/:type/:id", getPluginInfoHandler)
	e.POST("/api/plugins/:type/:id/overrides", postPluginOverridesHandler)
	e.DELETE("/api/plugins/:type/:id/overrides", deletePluginOverridesHandler)
	e.GET("/api/anchor-watch", getAnchorWatch)
	e.POST("/api/anchor-watch", setAnchorWatch)
	e.PATCH("/api/anchor-watch", patchAnchorWatch)
	e.DELETE("/api/anchor-watch", deleteAnchorWatch)
	e.GET("/api/anchor-watch/trails/self", getSelfTrailHandler)
	e.GET("/api/anchor-watch/trails/ais/:id", getAISTrailHandler)
	e.GET("/api/anchor-watch/trails/ais", getAllAISTrailsHandler)
	e.GET("/api/dashboard-pages", listDashboardPagesHandler)
	e.POST("/api/dashboard-pages", createDashboardPageHandler)
	e.GET("/api/dashboard-pages/:id", getDashboardPageHandler)
	e.PATCH("/api/dashboard-pages/:id", patchDashboardPageHandler)
	e.DELETE("/api/dashboard-pages/:id", deleteDashboardPageHandler)
	e.GET("/api/routes", listRoutesHandler)
	e.POST("/api/routes", createRouteHandler)
	e.GET("/api/routes/:id", getRouteHandler)
	e.PATCH("/api/routes/:id", patchRouteHandler)
	e.DELETE("/api/routes/:id", deleteRouteHandler)
	e.POST("/api/routes/:id/activate", activateRouteHandler)
	e.POST("/api/routes/deactivate", deactivateRouteHandler)
	e.GET("/api/routes/active", getActiveRouteHandler)
	e.GET("/api/tracks", getTracksHandler)
	e.GET("/api/tracks/motoring", getMotoringTrackHandler)
	e.GET("/api/depth-trend", depthTrend)
	e.GET("/api/czone/switches", getCZoneSwitchesHandler)
	e.PUT("/api/czone/switches/:id/state", putCZoneSwitchStateHandler)
	e.POST("/api/generator/start", postGeneratorStartHandler)
	e.POST("/api/generator/stop", postGeneratorStopHandler)
	e.GET("/api/world-imagery/:z/:x/:y", proxyWorldImageryTileHandler(globalTileCache, worldImageryClient))
	e.POST("/api/world-imagery/prefetch", prefetchWorldImageryHandler(globalTileCache, worldImageryClient))
	e.GET("/api/world-imagery/prefetch/:jobId", prefetchStatusHandler())
	e.DELETE("/api/world-imagery/cache", deleteWorldImageryCacheHandler(globalTileCache))
	e.GET("/api/gshhg-coastline", gshhgCoastlineHandler)
	e.POST("/api/sat-charts", uploadSatChartHandler)
	e.GET("/api/sat-charts", listSatChartsHandler)
	e.DELETE("/api/sat-charts/:id", deleteSatChartHandler)
	e.GET("/api/sat-charts/:id/:z/:x/:y", satChartTileHandler)

	registerStaticHandler(e)

	loadAnchorWatch()
	loadRoutes()
	loadDashboardPages()
	// All vessel data arrives over the SignalK delta stream (ADR 0037). There
	// is no REST read path to fall back to: a dropped stream surfaces as an
	// outage rather than being papered over.
	streamCtx, cancelStream := context.WithCancel(context.Background())
	defer cancelStream()
	go newSignalKStreamClient(globalSignalKSnapshot, getEnv("SETTINGS_FILE", "../settings.yaml")).run(streamCtx)

	go startTrackPoller(5 * time.Second)
	go startTideAutoUpdater(30 * time.Minute)

	addr := fmt.Sprintf(":%s", port)
	log.Printf("Starting server on %s", addr)
	if err := e.Start(addr); err != nil && err != http.ErrServerClosed {
		log.Fatalf("error starting server: %v", err)
	}
}

func getEnv(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}

func healthCheck(c echo.Context) error {
	version, revision := resolveBuildMetadata()

	return c.JSON(http.StatusOK, map[string]string{
		"status":   "ok",
		"version":  version,
		"revision": revision,
	})
}

func resolveBuildMetadata() (string, string) {
	version := strings.TrimSpace(buildVersion)
	if envVersion := strings.TrimSpace(os.Getenv("APP_VERSION")); envVersion != "" {
		version = envVersion
	}

	revision := strings.TrimSpace(buildRevision)
	if envRevision := strings.TrimSpace(os.Getenv("APP_REVISION")); envRevision != "" {
		revision = envRevision
	}

	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				if revision == "" || revision == "unknown" {
					revision = setting.Value
				}
			case "vcs.tag":
				if version == "" || version == "dev" || version == "(devel)" {
					version = setting.Value
				}
			}
		}

		if (version == "" || version == "dev" || version == "(devel)") && info.Main.Version != "" {
			version = info.Main.Version
		}
	}

	if version == "" {
		version = "dev"
	}
	if revision == "" {
		revision = "unknown"
	}

	return version, revision
}

type depthTrendResponse struct {
	Points     []depthTrendPoint `json:"points"`
	Since      string            `json:"since"`
	TideType   string            `json:"tide_type,omitempty"`
	TideDepthM float64           `json:"tide_depth_m,omitempty"`
}

func depthTrend(c echo.Context) error {
	window := c.QueryParam("window")
	if window == "" {
		window = "3h"
	}
	points := inMemoryDepthTrend(window)
	if influxTelemetryConfigured() {
		points = queryInfluxDepthTrend(window)
	}
	if points == nil {
		points = []depthTrendPoint{}
	}

	if turn, ok := findLastTideTurningPoint(points); ok {
		tideType := "low"
		if turn.IsHigh {
			tideType = "high"
		}
		return c.JSON(http.StatusOK, depthTrendResponse{Points: points, Since: "tide", TideType: tideType, TideDepthM: turn.DepthM})
	}

	return c.JSON(http.StatusOK, depthTrendResponse{Points: points, Since: "window"})
}

// computeMaxGustKtsFor picks exactly one source for the gust ladder per
// request: Influx when configured (queryInfluxMaxWindGustKtsFor), otherwise
// the in-memory ring buffer (inMemoryMaxWindGustKtsFor). Previously both were
// computed unconditionally and the in-memory result discarded whenever Influx
// was configured - wasted CPU/allocations on every /api/vessel-state request
// on Influx-backed deployments.
func computeMaxGustKtsFor(windows []string) map[string]float64 {
	if influxTelemetryConfigured() {
		return queryInfluxMaxWindGustKtsFor(windows)
	}
	return inMemoryMaxWindGustKtsFor(windows)
}

// buildVesselStatePayload produces the /api/vessel-state body. It is separate
// from the handler so the SSE stream can emit the identical shape without the
// two drifting apart.
func buildVesselStatePayload() map[string]any {
	state := vesselStateData{
		Status:               getEnv("VESSEL_STATUS", "At Anchor"),
		Datetime:             time.Now().UTC(),
		Depth:                -1,
		CurrentDriftKts:      -1,
		CurrentSetDeg:        -1,
		Latitude:             -1,
		Longitude:            -1,
		HeadingTrue:          -1,
		WindSpeedApparentKts: -1,
		WindAngleApparentDeg: -1,
		WindAngleRelativeDeg: -1,
		Engine0RPM:           -1,
		Engine1RPM:           -1,
	}
	source := "backend-fallback"

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)

	if signalkURL != "" {
		signalkState, err := fetchSignalKVesselState()
		// Always adopt signalkState, even on error: fetchSignalKVesselState
		// now always returns position/GNSS fields correctly marked critical
		// (and frozen at the last trusted fix) rather than a bare sentinel,
		// so the anchor watch alarm can tell "SignalK unreachable" apart from
		// "vessel has actually moved."
		state = signalkState
		if err == nil {
			source = "signalk"
		} else {
			source = "signalk-unreachable"
		}
	} else {
		state = criticalVesselState(state, "signalk not configured")
	}

	maxGustKts := computeMaxGustKtsFor(gustWindowLadder)
	// Clamp walking the ladder shortest-to-longest: the shortest window's -1
	// sentinel (no data) clamps to 0, then each subsequent (longer) window is
	// clamped to be >= the previous, already-clamped window's value - a
	// longer window's max gust can never be less than a shorter window's,
	// generalizing the previous 10m/1h-only clamp across the full ladder.
	previous := 0.0
	for i, window := range gustWindowLadder {
		value := maxGustKts[window]
		if i == 0 && value < 0 {
			value = 0
		}
		if value < previous {
			value = previous
		}
		maxGustKts[window] = value
		previous = value
	}

	vesselPrefix := loadBoatVesselPrefix(settingsPath)
	if vesselPrefix == "" {
		vesselPrefix = "M/V"
	}

	return map[string]any{
		"name":                           state.Name,
		"vessel_prefix":                  vesselPrefix,
		"status":                         state.Status,
		"datetime":                       state.Datetime.Format(time.RFC3339),
		"depth":                          state.Depth,
		"current_drift_kts":              state.CurrentDriftKts,
		"current_set_deg":                state.CurrentSetDeg,
		"current_drift_impact_kts":       state.CurrentDriftImpactKts,
		"latitude":                       state.Latitude,
		"longitude":                      state.Longitude,
		"gnss_quality_indicator":         state.GNSSQualityIndicator,
		"gnss_hdop":                      state.GNSSHDOP,
		"gnss_satellites":                state.GNSSSatellites,
		"gnss_validation_state":          state.GNSSValidationState,
		"gnss_validation_reason":         state.GNSSValidationReason,
		"gnss_critical_alert":            state.GNSSCriticalAlert,
		"heading_true":                   state.HeadingTrue,
		"speed_over_ground_kts":          state.SpeedOverGroundKts,
		"wind_speed_apparent_kts":        state.WindSpeedApparentKts,
		"wind_angle_apparent_deg":        state.WindAngleApparentDeg,
		"wind_side":                      state.WindSide,
		"wind_angle_relative_deg":        state.WindAngleRelativeDeg,
		"max_gust_kts":                   maxGustKts,
		"generator_state":                state.GeneratorState,
		"generator_manual_start":         state.GeneratorManualStart,
		"generator_manual_start_timer":   state.GeneratorManualStartTimer,
		"generator_running_by_condition": state.GeneratorRunningByCondition,
		"generator_runtime":              state.GeneratorRuntime,
		"engine_0_rpm":                   state.Engine0RPM,
		"engine_1_rpm":                   state.Engine1RPM,
		"source":                         source,
	}
}

func vesselState(c echo.Context) error {
	return c.JSON(http.StatusOK, buildVesselStatePayload())
}

// buildElectricalStatePayload produces the /api response body. Split from the handler so
// the SSE stream emits the identical shape without the two drifting apart.
func buildElectricalStatePayload() map[string]any {
	state := electricalStateData{
		Datetime:            time.Now().UTC(),
		BatterySocPercent:   -1,
		BatteryCapacityAh:   -1,
		ChargingCurrentA:    -1,
		ChargingPowerW:      -1,
		SolarOutputW:        -1,
		ACOutputW:           -1,
		DC12VPowerW:         -1,
		DC12VCurrentA:       -1,
		DC24VVoltageV:       -1,
		ACLoadsW:            -1,
		GeneratorRealPowerW: -1,
		Alternator0:         alternatorInstanceData{CurrentA: -1, VoltageV: -1, PowerW: -1, TempC: -1},
		Alternator1:         alternatorInstanceData{CurrentA: -1, VoltageV: -1, PowerW: -1, TempC: -1},
		Charger0:            chargerInstanceData{CurrentA: -1, ACIn1CurrentA: -1, ChargingMode: "", Error: ""},
	}
	source := "backend-fallback"

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)

	if signalkURL != "" {
		electrical, fetchErr := fetchSignalKElectricalState()
		if fetchErr == nil {
			state = electrical
			source = "signalk"
		}
	}

	return map[string]any{
		"datetime":                   state.Datetime.Format(time.RFC3339),
		"battery_soc_percent":        state.BatterySocPercent,
		"battery_capacity_ah":        state.BatteryCapacityAh,
		"charging_current_a":         state.ChargingCurrentA,
		"charging_power_w":           state.ChargingPowerW,
		"solar_output_w":             state.SolarOutputW,
		"ac_output_w":                state.ACOutputW,
		"dc_power_w":                 state.DC12VPowerW,
		"dc_current_a":               state.DC12VCurrentA,
		"dc_12v_power_w":             state.DC12VPowerW,
		"dc_12v_current_a":           state.DC12VCurrentA,
		"dc_24v_voltage_v":           state.DC24VVoltageV,
		"ac_loads_w":                 state.ACLoadsW,
		"generator_real_power_w":     state.GeneratorRealPowerW,
		"alternator_0_current_a":     state.Alternator0.CurrentA,
		"alternator_0_voltage_v":     state.Alternator0.VoltageV,
		"alternator_0_power_w":       state.Alternator0.PowerW,
		"alternator_0_temperature_c": state.Alternator0.TempC,
		"alternator_1_current_a":     state.Alternator1.CurrentA,
		"alternator_1_voltage_v":     state.Alternator1.VoltageV,
		"alternator_1_power_w":       state.Alternator1.PowerW,
		"alternator_1_temperature_c": state.Alternator1.TempC,
		"charger_0_current_a":        state.Charger0.CurrentA,
		"charger_0_acin_1_current_a": state.Charger0.ACIn1CurrentA,
		"charger_0_charging_mode":    state.Charger0.ChargingMode,
		"charger_0_error":            state.Charger0.Error,
		"source":                     source,
	}
}
func electricalState(c echo.Context) error {
	return c.JSON(http.StatusOK, buildElectricalStatePayload())
}

// buildSolarStatePayload produces the /api response body. Split from the handler so
// the SSE stream emits the identical shape without the two drifting apart.
func buildSolarStatePayload() map[string]any {
	state := solarStateData{
		Datetime:      time.Now().UTC(),
		CurrentW:      -1,
		TodayKWh:      -1,
		YesterdayKWh:  -1,
		PeakTodayW:    -1,
		Controllers:   []solarControllerData{},
		Trend24hTotal: []solarTrendPoint{},
	}
	source := "backend-fallback"

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)

	if signalkURL != "" {
		solar, fetchErr := fetchSignalKSolarState()
		if fetchErr == nil {
			state = solar
			source = "signalk"
		}
	}

	state = applyInMemorySolarDefaults(state)
	if influxTelemetryConfigured() {
		state = applyInfluxSolarOverride(state)
	}
	if state.Trend24hTotal == nil {
		state.Trend24hTotal = []solarTrendPoint{}
	}

	controllers := make([]map[string]any, 0, len(state.Controllers))
	for _, controller := range state.Controllers {
		controllers = append(controllers, map[string]any{
			"id":                controller.ID,
			"label":             controller.Label,
			"current_w":         controller.CurrentW,
			"today_kwh":         controller.TodayKWh,
			"yesterday_kwh":     controller.YesterdayKWh,
			"mode":              controller.Mode,
			"error":             controller.Error,
			"last_update_age_s": controller.LastUpdateAge,
			"contribution_pct":  controller.Contribution,
		})
	}

	return map[string]any{
		"datetime":        state.Datetime.Format(time.RFC3339),
		"source":          source,
		"current_w":       state.CurrentW,
		"today_kwh":       state.TodayKWh,
		"yesterday_kwh":   state.YesterdayKWh,
		"peak_today_w":    state.PeakTodayW,
		"controllers":     controllers,
		"trend_24h_total": state.Trend24hTotal,
	}
}
func solarState(c echo.Context) error {
	return c.JSON(http.StatusOK, buildSolarStatePayload())
}

// applyInMemorySolarDefaults fills any field SignalK didn't report, from the
// in-memory accumulator/buffer fed by sampleTracks. Always safe to call —
// sentinels pass through unchanged if nothing has been recorded yet.
func applyInMemorySolarDefaults(state solarStateData) solarStateData {
	if state.TodayKWh < 0 {
		state.TodayKWh = inMemorySolarTodayKWh()
	}
	if state.YesterdayKWh < 0 {
		state.YesterdayKWh = inMemorySolarYesterdayKWh()
	}
	if state.PeakTodayW < 0 {
		state.PeakTodayW = inMemorySolarPeakTodayW()
	}
	if len(state.Trend24hTotal) == 0 {
		state.Trend24hTotal = inMemorySolarTrend24h()
	}
	return state
}

// applyInfluxSolarOverride wholesale-replaces the four Influx-backed fields,
// matching queryInfluxMaxWindGustKtsFor/queryInfluxDepthTrend's override
// pattern in vesselState()/depthTrend() — called only when
// influxTelemetryConfigured(), and does not fall back to the in-memory
// value if the Influx query itself fails (same Fallback Policy reasoning as
// ADR-0020: once Influx is enabled, its failures should be visible, not
// silently patched over).
func applyInfluxSolarOverride(state solarStateData) solarStateData {
	now := state.Datetime
	if now.IsZero() {
		now = time.Now().UTC()
	}
	state.TodayKWh = queryInfluxSolarTodayKWh(now)
	state.YesterdayKWh = queryInfluxSolarYesterdayKWh(now)
	state.PeakTodayW = queryInfluxSolarPeakTodayW(now)
	state.Trend24hTotal = queryInfluxSolarTrend24h(now)
	return state
}

// buildTanksStatePayload produces the /api response body. Split from the handler so
// the SSE stream emits the identical shape without the two drifting apart.
func buildTanksStatePayload() map[string]any {
	now := time.Now().UTC()
	tanks := []tankLevelData{}
	source := "backend-fallback"

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)
	labelOverrides := loadTankLabelOverrides(settingsPath)

	if signalkURL != "" {
		stateTanks, datetime, fetchErr := fetchSignalKTanksState(labelOverrides)
		if fetchErr == nil {
			tanks = stateTanks
			now = datetime
			source = "signalk"
		}
	}

	return map[string]any{
		"datetime": now.Format(time.RFC3339),
		"source":   source,
		"tanks":    tanks,
	}
}
func tanksState(c echo.Context) error {
	return c.JSON(http.StatusOK, buildTanksStatePayload())
}

type nearbyVessel struct {
	Name       string   `json:"name"`
	Mmsi       string   `json:"mmsi,omitempty"`
	RangeFt    int      `json:"range_ft"`
	AgeSeconds int      `json:"age_seconds"`
	SogKnots   *float64 `json:"sog_knots,omitempty"`
	Lat        float64  `json:"lat"`
	Lon        float64  `json:"lon"`
	SeenCount  int      `json:"seen_count"`
	LastSeenAt string   `json:"last_seen_at,omitempty"`
}

// buildNearbyVesselsPayload produces the /api response body. Split from the handler so
// the SSE stream emits the identical shape without the two drifting apart.
func buildNearbyVesselsPayload() map[string]any {
	source := "backend-fallback"
	now := time.Now().UTC()
	vessels := []nearbyVessel{}

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)

	if signalkURL != "" {
		signalkSelfName := fetchSignalKSelfName()
		excludedNames := []string{signalkSelfName}

		state, selfErr := fetchSignalKVesselState()
		if selfErr == nil && state.Latitude >= -90 && state.Latitude <= 90 && state.Longitude >= -180 && state.Longitude <= 180 {
			nearby, nearbyErr := fetchSignalKNearbyVessels(state.Latitude, state.Longitude, now, excludedNames)
			if nearbyErr == nil {
				if globalNearbyContactStore != nil {
					for i := range nearby {
						key, ok := vesselContactKey(nearby[i].Mmsi)
						if !ok {
							log.Printf("Skipping sighting-history enrichment for %q: no MMSI reported", nearby[i].Name)
							continue
						}
						seenCount, lastSeenAt, summaryErr := globalNearbyContactStore.summary(key)
						if summaryErr != nil {
							log.Printf("Failed to read nearby vessel contact summary for %s: %v", key, summaryErr)
							continue
						}
						nearby[i].SeenCount = seenCount
						if !lastSeenAt.IsZero() {
							nearby[i].LastSeenAt = lastSeenAt.Format(time.RFC3339)
						}
					}
				}

				vessels = nearby
				source = "signalk"
			}
		}
	}

	return map[string]any{
		"datetime": now.Format(time.RFC3339),
		"source":   source,
		"vessels":  vessels,
	}
}
func nearbyVessels(c echo.Context) error {
	return c.JSON(http.StatusOK, buildNearbyVesselsPayload())
}

func formatWeatherCondition(code string) string {
	return formatWeatherConditionAt(code, time.Time{}, nil, false)
}

func formatWeatherConditionAt(code string, observedAt time.Time, location *time.Location, preferDaytime bool) string {
	normalized := strings.ToLower(strings.TrimSpace(code))
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, "-", "")

	if normalized == "mostlyclear" {
		if preferDaytime || isDaytimeWeatherObservation(observedAt, location) {
			return "Mostly Sunny"
		}
		return "Mostly Clear"
	}

	conditions := map[string]string{
		"clear":             "Clear",
		"cloudy":            "Cloudy",
		"dusty":             "Dusty",
		"foggy":             "Foggy",
		"haze":              "Hazy",
		"mostlycloudy":      "Mostly Cloudy",
		"partlycloudy":      "Partly Cloudy",
		"smoky":             "Smoky",
		"breezy":            "Breezy",
		"windy":             "Windy",
		"drizzle":           "Drizzle",
		"heavyrain":         "Heavy Rain",
		"rain":              "Rain",
		"snow":              "Snow",
		"sleet":             "Sleet",
		"freezingdrizzle":   "Freezing Drizzle",
		"freezingrain":      "Freezing Rain",
		"hail":              "Hail",
		"mixedrainandsnow":  "Mixed Rain & Snow",
		"mixedrainandsleet": "Mixed Rain & Sleet",
		"mixedsnowandsleet": "Mixed Snow & Sleet",
		"thunderstorms":     "Thunderstorms",
		"heavysnow":         "Heavy Snow",
		"blizzard":          "Blizzard",
	}

	if condition, ok := conditions[normalized]; ok {
		return condition
	}
	if code == "" {
		return "Unknown"
	}
	return code
}

func isDaytimeWeatherObservation(observedAt time.Time, location *time.Location) bool {
	if observedAt.IsZero() {
		return false
	}
	if location == nil {
		location = time.UTC
	}

	hour := observedAt.In(location).Hour()
	return hour >= 6 && hour < 18
}

func degreesToDirection(degrees float64) string {
	// Normalize to 0-360
	for degrees < 0 {
		degrees += 360
	}
	for degrees >= 360 {
		degrees -= 360
	}

	directions := []string{"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE",
		"S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"}

	// Each direction covers 22.5 degrees
	index := int((degrees+11.25)/22.5) % 16
	return directions[index]
}
