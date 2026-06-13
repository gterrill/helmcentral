package main

import (
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
	defaultHouseBatteryCapacityAh = 1440
)

var (
	buildVersion  = "dev"
	buildRevision = "unknown"
)

type vesselStateData struct {
	Name                        string
	Status                      string
	Datetime                    time.Time
	Depth                       float64
	CurrentDriftKts             float64
	CurrentSetDeg               float64
	Latitude                    float64
	Longitude                   float64
	GNSSQualityIndicator        int
	GNSSHDOP                    float64
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

	// Routes
	e.GET("/api/health", healthCheck)
	e.GET("/api/vessel-state", vesselState)
	e.GET("/api/electrical-state", electricalState)
	e.GET("/api/tanks-state", tanksState)
	e.GET("/api/nearby-vessels", nearbyVessels)
	e.GET("/api/weather-today", weatherToday)
	e.GET("/api/weather-forecast", weatherForecast)
	e.GET("/api/tide-today", tideToday)
	e.GET("/api/place-name", placeName)
	e.GET("/api/caches", listCaches)
	e.POST("/api/caches/:name/invalidate", invalidateCache)
	e.GET("/api/settings", getSettingsHandler)
	e.POST("/api/settings", updateSettingsHandler)
	e.GET("/api/settings/signalk", getSignalKSettingsHandler)
	e.POST("/api/settings/signalk", updateSignalKSettingsHandler)
	e.GET("/api/anchor-watch", getAnchorWatch)
	e.POST("/api/anchor-watch", setAnchorWatch)
	e.PATCH("/api/anchor-watch", patchAnchorWatch)
	e.DELETE("/api/anchor-watch", deleteAnchorWatch)
	e.GET("/api/anchor-watch/trails/self", getSelfTrailHandler)
	e.GET("/api/anchor-watch/trails/ais/:id", getAISTrailHandler)
	e.GET("/api/anchor-watch/trails/ais", getAllAISTrailsHandler)
	e.GET("/api/tracks", getTracksHandler)
	e.GET("/api/tracks/motoring", getMotoringTrackHandler)
	e.GET("/api/depth-trend", depthTrend)
	e.GET("/api/czone/switches", getCZoneSwitchesHandler)
	e.PUT("/api/czone/switches/:id/state", putCZoneSwitchStateHandler)
	e.POST("/api/generator/start", postGeneratorStartHandler)
	e.POST("/api/generator/stop", postGeneratorStopHandler)
	e.GET("/api/himawari/:variant/:z/:x/:y", proxyHimawariTileHandler)
	e.GET("/api/world-imagery/:z/:x/:y", proxyWorldImageryTileHandler)

	registerStaticHandler(e)

	loadAnchorWatch()
	go seedMotoringTrailFromInflux()
	go startTrackPoller(5 * time.Second)

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

func depthTrend(c echo.Context) error {
	// If the vessel is currently stationary, find when that began and use it
	// as the start of the depth trend so motoring data is excluded.
	if stationaryStart := queryInfluxLastStationaryStart(); !stationaryStart.IsZero() {
		points := queryInfluxDepthTrendSince(stationaryStart)
		if points == nil {
			points = []depthTrendPoint{}
		}
		return c.JSON(http.StatusOK, points)
	}

	window := c.QueryParam("window")
	if window == "" {
		window = "2h"
	}
	points := queryInfluxDepthTrend(window)
	if points == nil {
		points = []depthTrendPoint{}
	}
	return c.JSON(http.StatusOK, points)
}

func vesselState(c echo.Context) error {
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
	vesselPath := getEnv("SIGNALK_VESSEL_PATH", "/signalk/v1/api/vessels/self")

	if signalkURL != "" {
		signalkState, err := fetchSignalKVesselState(signalkURL, vesselPath)
		if err == nil {
			state = signalkState
			source = "signalk"
		}
	}

	maxGust10mKts := 0.0
	maxGust1hKts := 0.0
	if state.WindSpeedApparentKts > 0 {
		maxGust10mKts = queryInfluxMaxWindGustKts("10m")
		maxGust1hKts = queryInfluxMaxWindGustKts("1h")
		if maxGust10mKts < 0 {
			maxGust10mKts = 0
		}
		if maxGust1hKts < 0 {
			maxGust1hKts = 0
		}
	}

	vesselPrefix := loadBoatVesselPrefix(settingsPath)
	if vesselPrefix == "" {
		vesselPrefix = "M/V"
	}

	return c.JSON(http.StatusOK, map[string]any{
		"name":                           state.Name,
		"vessel_prefix":                  vesselPrefix,
		"status":                         state.Status,
		"datetime":                       state.Datetime.Format(time.RFC3339),
		"depth":                          state.Depth,
		"current_drift_kts":              state.CurrentDriftKts,
		"current_set_deg":                state.CurrentSetDeg,
		"latitude":                       state.Latitude,
		"longitude":                      state.Longitude,
		"gnss_quality_indicator":         state.GNSSQualityIndicator,
		"gnss_hdop":                      state.GNSSHDOP,
		"gnss_validation_state":          state.GNSSValidationState,
		"gnss_validation_reason":         state.GNSSValidationReason,
		"gnss_critical_alert":            state.GNSSCriticalAlert,
		"heading_true":                   state.HeadingTrue,
		"speed_over_ground_kts":          state.SpeedOverGroundKts,
		"wind_speed_apparent_kts":        state.WindSpeedApparentKts,
		"wind_angle_apparent_deg":        state.WindAngleApparentDeg,
		"wind_side":                      state.WindSide,
		"wind_angle_relative_deg":        state.WindAngleRelativeDeg,
		"max_gust_10m_kts":               maxGust10mKts,
		"max_gust_1h_kts":                maxGust1hKts,
		"generator_state":                state.GeneratorState,
		"generator_manual_start":         state.GeneratorManualStart,
		"generator_manual_start_timer":   state.GeneratorManualStartTimer,
		"generator_running_by_condition": state.GeneratorRunningByCondition,
		"generator_runtime":              state.GeneratorRuntime,
		"engine_0_rpm":                   state.Engine0RPM,
		"engine_1_rpm":                   state.Engine1RPM,
		"source":                         source,
	})
}

func electricalState(c echo.Context) error {
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
	}
	source := "backend-fallback"

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	signalkURL := buildSignalKURL(address, port)
	vesselPath := getEnv("SIGNALK_VESSEL_PATH", "/signalk/v1/api/vessels/self")

	if signalkURL != "" {
		electrical, fetchErr := fetchSignalKElectricalState(signalkURL, vesselPath)
		if fetchErr == nil {
			state = electrical
			source = "signalk"
		}
	}

	return c.JSON(http.StatusOK, map[string]any{
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
		"source":                     source,
	})
}

func tanksState(c echo.Context) error {
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
	vesselPath := getEnv("SIGNALK_VESSEL_PATH", "/signalk/v1/api/vessels/self")
	labelOverrides := loadTankLabelOverrides(settingsPath)

	if signalkURL != "" {
		stateTanks, datetime, fetchErr := fetchSignalKTanksState(signalkURL, vesselPath, labelOverrides)
		if fetchErr == nil {
			tanks = stateTanks
			now = datetime
			source = "signalk"
		}
	}

	return c.JSON(http.StatusOK, map[string]any{
		"datetime": now.Format(time.RFC3339),
		"source":   source,
		"tanks":    tanks,
	})
}

type nearbyVessel struct {
	Name       string   `json:"name"`
	RangeFt    int      `json:"range_ft"`
	AgeSeconds int      `json:"age_seconds"`
	SogKnots   *float64 `json:"sog_knots,omitempty"`
	Lat        float64  `json:"lat"`
	Lon        float64  `json:"lon"`
}

func nearbyVessels(c echo.Context) error {
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
	vesselsPath := getEnv("SIGNALK_VESSELS_PATH", "/signalk/v1/api/vessels")
	vesselPath := getEnv("SIGNALK_VESSEL_PATH", "/signalk/v1/api/vessels/self")

	if signalkURL != "" {
		signalkSelfName := fetchSignalKSelfName(signalkURL, vesselPath)
		excludedNames := []string{signalkSelfName}

		state, selfErr := fetchSignalKVesselState(signalkURL, vesselPath)
		if selfErr == nil && state.Latitude >= -90 && state.Latitude <= 90 && state.Longitude >= -180 && state.Longitude <= 180 {
			nearby, nearbyErr := fetchSignalKNearbyVessels(signalkURL, vesselsPath, state.Latitude, state.Longitude, now, excludedNames)
			if nearbyErr == nil {
				vessels = nearby
				source = "signalk"
			}
		}
	}

	return c.JSON(http.StatusOK, map[string]any{
		"datetime": now.Format(time.RFC3339),
		"source":   source,
		"vessels":  vessels,
	})
}

func formatWeatherCondition(code string) string {
	normalized := strings.ToLower(strings.TrimSpace(code))
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, "-", "")

	conditions := map[string]string{
		"clear":             "Clear",
		"cloudy":            "Cloudy",
		"dusty":             "Dusty",
		"foggy":             "Foggy",
		"haze":              "Hazy",
		"mostlyclear":       "Mostly Clear",
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
