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
	defaultSignalKAddress  = "localhost"
	defaultSignalKPort     = 3000
	metersPerSecondToKnots = 1.943844
	defaultWindMaxAge      = 5 * time.Minute
	defaultRPMMaxAge       = 30 * time.Second

	// trackPollInterval is startTrackPoller's cadence, the same poll every
	// ring buffer in telemetry_history.go is sized against (windGustHistoryCapacity
	// is 17280 samples at this interval, exactly 24h -- see
	// TestBarometerHistoryCoversTheTwentyFourHourWindow). A named constant
	// rather than a literal at the call site so that test can reference the
	// same number this actually runs at, not a second copy of it.
	trackPollInterval = 5 * time.Second
)

var (
	buildVersion  = "dev"
	buildRevision = "unknown"
)

// globalRadarTargetStore holds the current set of ARPA radar targets fed by
// radarPoller (radar_source.go), read by the "radar-targets" telemetry
// emitter and GET /api/radar/targets. nil until initialised in main(),
// mirroring globalNearbyContactStore (nearby_contacts.go).
var globalRadarTargetStore *radarTargetStore

// gustWindowLadder is the shared, ordered set of "max gust" windows exposed
// via the vessel-state API's max_gust_kts field (and the frontend's MAX GUST
// cards, which cycle through it shortest-to-longest). It is the single
// source of truth for these windows - do not duplicate this literal
// elsewhere in the package.
var gustWindowLadder = []string{"10m", "30m", "1h", "24h"}

type vesselStateData struct {
	Name     string
	Status   string
	Datetime time.Time
	Depth    float64
	// DepthLastUpdateAge is how many seconds old the environment.depth.
	// belowTransducer reading is, per the ADR 0068 last_update_age_s
	// mechanism (-1 when the source carries no timestamp). Scoped to
	// belowTransducer specifically, not the whole environment.depth branch,
	// so a live belowSurface/belowKeel sensor can never mask a dead
	// belowTransducer reading — belowTransducer is the value this tile
	// actually renders, and depth is the number a dead transducer leaves the
	// operator to run aground on.
	DepthLastUpdateAge float64
	// LengthOverallM is the vessel's LOA read from SignalK's design.length
	// branch (ADR 0047). It carries the lookupNumber -1 sentinel when
	// unpublished; buildVesselStatePayload nils it out rather than letting
	// -1 leak into the API response.
	LengthOverallM        float64
	CurrentDriftKts       float64
	CurrentSetDeg         float64
	CurrentDriftImpactKts *float64
	Latitude              float64
	Longitude             float64
	// PositionLastUpdateAge is the freshest of navigation.position's and
	// navigation.gnss's own last_update_age_s (ADR 0068): the fix itself and
	// its quality/HDOP/satellite-count figures are published under two
	// different parents, and either one still updating means the fix is
	// current. -1 when neither subtree carries a timestamp.
	PositionLastUpdateAge float64
	GNSSQualityIndicator  int
	GNSSHDOP              float64
	GNSSSatellites        int
	GNSSValidationState   string
	GNSSValidationReason  string
	GNSSCriticalAlert     bool
	HeadingTrue           float64
	SpeedOverGroundKts    float64
	WindSpeedApparentKts  float64
	WindAngleApparentDeg  float64
	WindSide              string
	WindAngleRelativeDeg  float64
	// True wind: environment.wind.speedTrue/angleTrueWater/directionTrue,
	// parsed the same way as the Apparent fields above but never derived
	// from them or from each other - see fetchSignalKVesselState's true-wind
	// block. Two tiles read these: the Current Conditions tile's readout
	// (ADR 0129: speedTrue/directionTrue) and the Wind tile's True mode
	// (ADR 0130: adds angleTrueWater's WindAngleTrueDeg/WindSideTrue/
	// WindAngleTrueRelativeDeg on top). All five carry the lookupNumber -1
	// (WindSideTrue: "") sentinel when the boat publishes no true-wind
	// source, or that particular leaf's own reading has gone stale, rather
	// than falling back to 0/starboard the way the Apparent fields
	// historically do - a boat with no true-wind source must show a dash,
	// never the apparent figure relabelled (AGENTS.md fallback policy).
	WindSpeedTrueKts         float64
	WindAngleTrueDeg         float64
	WindSideTrue             string
	WindAngleTrueRelativeDeg float64
	// WindDirectionTrueDeg is the compass bearing (0-360, true north) the
	// wind is blowing FROM - environment.wind.directionTrue, already
	// absolute rather than bow-relative, so it carries no "side."
	WindDirectionTrueDeg float64
	// WindLastUpdateAge is the freshest last_update_age_s found anywhere
	// under environment.wind (ADR 0068), scoped to that subtree only —
	// environment.current is a different sensor with its own health and
	// must not be folded in. -1 when environment.wind carries no timestamp.
	// Shared by true and apparent wind: both live in the same subtree.
	WindLastUpdateAge           float64
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
	LastUpdateAge float64
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
	LastUpdateAge       float64
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
	// LastUpdateAge mirrors the solar-controller pattern (ADR 0068): each
	// tank carries its own age, scoped to that tank's own subtree, so one
	// dead sender does not condemn every tank on the boat. -1 when the
	// tank's currentLevel carries no timestamp.
	LastUpdateAge float64 `json:"last_update_age_s"`
}

func main() {
	e := echo.New()
	port := getEnv("PORT", "8080")

	// Place-name resolution (place_name.go) and the assistant's find_places
	// tool (assistant_tools.go) both go through whichever installed POI
	// plugin ui.place_name_provider names (ADR 0101) - resolved fresh on
	// every lookup via resolvePlaceNameProvider, not once here at startup.
	// There is no backend Overpass client: a plugin that wants to use
	// Overpass (osm-overpass, by default) owns its own endpoint as a
	// plugin config value (ADR 0100).

	// Capture logs to in-memory ring buffer for Settings -> Logs viewer
	logWriter := initLogCapture()
	e.Logger.SetOutput(logWriter)

	// Middleware
	// accessLogSkipWriter (access_log.go) drops successful GET/HEAD lines
	// after the fact - Echo's LoggerConfig.Skipper runs before next(c) and
	// so cannot see the response status, which is what a "skip 2xx GETs"
	// decision needs (backend perf audit Tier 3: two thirds of the logs
	// stream's bytes were exactly these lines).
	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Output: accessLogSkipWriter{underlying: logWriter},
	}))
	e.Use(middleware.Recover())
	// corsMiddleware (cors.go) replaces AllowOrigins: []string{"*"}: that
	// combined with credentials is rejected by every browser anyway, and was
	// half the README's security warning (docs/adr/0040).
	e.Use(corsMiddleware())
	// gzip response compression (compression.go), skipping SSE streams, the
	// radar websocket upgrade, and the already-compressed tile/image/font
	// proxy endpoints - see compressionSkipper's doc comment for the full
	// list and why each is excluded.
	registerCompressionMiddleware(e)

	// Encrypted secrets store. Must be opened before any provider
	// registration below: SignalK and InfluxDB read their secrets from
	// globalSecretsStore directly at point of use (loadSignalKCredentials,
	// loadInfluxSettings), not via getEnv/os.Getenv, since the ADR 0023
	// amendment retired the boot-time process-environment copy. Fail fast on
	// open error (including a master-key mismatch against existing
	// encrypted rows) rather than silently running with secrets unavailable.
	ss, err := newSecretsStore(secretsDBPath(), secretsKeyPath())
	if err != nil {
		log.Fatalf("secrets store: %v", err)
	}
	globalSecretsStore = ss

	// Session store for SignalK delegated authentication (docs/adr/0040).
	// Fail fast on open error, same reasoning as every other SQLite store
	// here (secrets_store.go's precedent): a session store that silently
	// doesn't persist would make every login look like it worked and then
	// vanish.
	sessions, err := newSessionStore(sessionsDBPath())
	if err != nil {
		log.Fatalf("session store: %v", err)
	}

	// auth.mode validation. mode:none (this release's default) only logs a
	// warning naming the risk; mode:signalk additionally probes SignalK's
	// security status once and fails fast if it's off, since "Helmcentral
	// requires login" against a server with no login to require is an
	// unsatisfiable combination that must not boot into a half-state.
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	authMode, err := checkAuthModeAtStartup(settingsPath)
	if err != nil {
		log.Fatalf("auth: %v", err)
	}
	if authMode == authModeNone {
		log.Printf("WARNING: auth.mode is 'none' — Helmcentral is running without authentication. " +
			"Any device on this boat's network can read and control everything the API exposes, " +
			"including starting the generator and switching CZone outputs. " +
			"Set auth.mode: signalk in settings.yaml to require SignalK login (docs/adr/0040-signalk-delegated-authentication.md).")
	} else {
		log.Printf("auth.mode is 'signalk' — SignalK login required for read/write/admin API access.")
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
	// plugins/weather/*.wasm exists, which the plugins-builder Compose
	// service creates.
	loadWasmWeatherProviders(pluginsWeatherDir())

	// Wave providers - WASM-plugin-only (no native built-in), same reasoning
	// as weather above; registry stays empty (waveForecast correctly 502s)
	// until plugins/waves/*.wasm exists.
	loadWasmWaveProviders(pluginsWavesDir())

	// POI (points of interest) providers - WASM-plugin-only (no native
	// built-in), same reasoning as wave above; registry stays empty
	// (poiNearby correctly 502s) until plugins/poi/*.wasm exists.
	loadWasmPOIProviders(pluginsPOIDir())

	// Upper air is its own category rather than part of the weather provider,
	// so a boat can run WeatherKit for surface weather (which has no pressure
	// levels) and still get a 500mb outlook (ADR 0071).
	loadWasmUpperAirProviders(pluginsUpperAirDir())

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

	// Alarm log (occurrence history behind the alarm centre). Fail fast on open
	// error, same reasoning as the stores above.
	als, err := newAlarmLogStore(alarmLogDBPath())
	if err != nil {
		log.Fatalf("failed to open alarm log store: %v", err)
	}
	globalAlarmLogStore = als

	// Onboard assistant conversation history (ADR 0093). Fail fast on open
	// error, same reasoning as the stores above.
	as, err := newAssistantStore(assistantDBPath())
	if err != nil {
		log.Fatalf("failed to open assistant store: %v", err)
	}
	globalAssistantStore = as

	// Document store (ADR 0106): metadata, virtual folders, tags, chunks and
	// FTS5 search behind a flat, hash-named folder of file bytes. Fail fast
	// on open error, same reasoning as the other stores above. The
	// documents directory is created (not just the database's own parent,
	// which newDocumentStore already handles) so the boot sweep below
	// always has somewhere to os.ReadDir, even on a brand new install that
	// has never taken an upload yet.
	ds, err := newDocumentStore(documentsDBPath())
	if err != nil {
		log.Fatalf("failed to open document store: %v", err)
	}
	globalDocumentStore = ds
	if err := os.MkdirAll(documentsDirPath(), 0o755); err != nil {
		log.Fatalf("failed to create documents directory: %v", err)
	}
	if sweep, err := sweepDocumentsDir(documentsDirPath(), globalDocumentStore); err != nil {
		log.Fatalf("failed to sweep documents directory: %v", err)
	} else if sweep.RemovedTemp > 0 || sweep.OrphanFiles > 0 || sweep.MissingFiles > 0 {
		log.Printf("documents: swept %d abandoned upload(s); %d orphan file(s) and %d missing file(s) logged above",
			sweep.RemovedTemp, sweep.OrphanFiles, sweep.MissingFiles)
	}

	// Document indexer (ADR 0106, B4): local extraction always, Mate's paid
	// OCR/summarise-and-tag enrichment only when a document's own consent
	// flag says so. Its own *http.Client, not openRouterHTTPClient (the
	// streaming assistant client's, whose ResponseHeaderTimeout is 60s) -
	// a non-streaming OCR call on a many-page scan can take minutes to
	// return headers at all, well past what the interactive assistant ever
	// tolerates. documentIndexerWake (documents_handlers.go) starts out nil
	// (a no-op) so upload/reindex handlers work identically before this
	// runs; assigning it here is what actually connects them to the queue.
	documentsTransport := http.DefaultTransport.(*http.Transport).Clone()
	documentsTransport.ResponseHeaderTimeout = 10 * time.Minute
	documentsHTTPClient := &http.Client{Transport: documentsTransport}
	docIndexer := newDocumentIndexer(
		globalDocumentStore,
		documentsDirPath(),
		func() (assistantReadiness, string, error) { return checkAssistantReadiness(settingsPath) },
		documentsHTTPClient,
		func() (string, error) {
			settings, err := readSettings(settingsPath)
			if err != nil {
				return "", err
			}
			return buildSettingsPayload(settings).Assistant.DocumentModel, nil
		},
	)
	documentIndexerWake = docIndexer.Wake
	// documentIndexerBackfillStatus (documents_handlers.go, E1c) connects
	// GET /api/documents/embeddings to this same indexer's backfill state -
	// the same wiring as documentIndexerWake immediately above.
	documentIndexerBackfillStatus = docIndexer.BackfillStatus
	// documentIndexerSweepIfReady (ADR 0120) replaces what used to be POST
	// /api/documents/embeddings/backfill and the two notes backfill
	// endpoints: turning Mate on is the operator's consent, so the sweep
	// runs itself rather than waiting on a button click. Wired here the
	// same nil-until-assigned way as the lines above, then run once
	// immediately - the boot-time pass that catches a library that was
	// already sitting there, enrich=0, when Mate was configured on a
	// previous run. updateSettingsHandler (signalk.go) runs it again every
	// time settings are saved into a working configuration. The boot pass
	// runs in the background so the HTTP server doesn't wait on it.
	documentIndexerSweepIfReady = docIndexer.SweepIfReady
	go documentIndexerSweepIfReady()

	// The assistant's read_help tool (mate-voice-assistant plan, "App-wide
	// voice"): docs/features, docs/how-to and docs/reference staged into
	// backend/help (Makefile's help-stage target, the Dockerfile and
	// .goreleaser.yaml, mirroring how backend/dist stages the frontend for
	// static.go's //go:embed). Not fatal when empty: a host `go build` with
	// no staging step is a legitimate developer build, and read_help
	// itself reports the gap to the model rather than failing startup.
	helpPages, err := loadHelp(helpFS, "help")
	if err != nil {
		log.Fatalf("failed to load the embedded help: %v", err)
	}
	globalHelp = helpPages
	if len(globalHelp) == 0 {
		log.Printf("help not staged; read_help will report it")
	} else {
		log.Printf("loaded %d help page(s)", len(globalHelp))
	}

	// Registered web push devices. Its own file rather than the alarm log's:
	// these are durable device registrations whose loss cannot be recovered
	// without physically revisiting every phone, unlike the log's prunable
	// history and self-expiring queue.
	wps, err := newWebPushSubscriptionStore(webPushDBPath())
	if err != nil {
		log.Fatalf("failed to open web push subscription store: %v", err)
	}
	globalWebPushSubscriptionStore = wps

	// Mint the VAPID keypair on first run. Fail fast: a half-written pair signs
	// with one key and advertises another, which every push service rejects
	// with an opaque 403.
	vapidPublicKey, err := ensureVAPIDKeys(globalSecretsStore)
	if err != nil {
		log.Fatalf("web push: %v", err)
	}
	// Devices registered against a previous keypair can never be delivered to
	// again, so discard them loudly rather than letting every push fail
	// silently forever.
	if discarded, err := globalWebPushSubscriptionStore.DeleteWhereKeyNot(vapidPublicKey); err != nil {
		log.Printf("web push: could not check registered devices against the current VAPID key: %v", err)
	} else if discarded > 0 {
		log.Printf("web push: discarded %d subscription(s) registered against a previous VAPID key; those devices must re-subscribe", discarded)
	}

	if err := loadAlarmRules(); err != nil {
		log.Fatalf("failed to load alarm rules: %v", err)
	}
	// Offers the heavy-weather set once per installation, disabled, for the
	// operator to enable and tune (ADR 0070). A failure here is not fatal: it
	// leaves the alarm centre exactly as it was, which is a working state.
	if err := seedHeavyWeatherRules(); err != nil {
		log.Printf("could not seed the heavy-weather alarm rules: %v", err)
	}
	// Offers the forecast-warnings set once per installation, enabled by
	// default (ADR 0087): unlike the heavy-weather set above, these four
	// rules bind to the official met-service warning itself, not an
	// uncalibrated heuristic. A failure here is not fatal for the same
	// reason as above: it leaves the alarm centre exactly as it was.
	if err := seedForecastWarningsRules(); err != nil {
		log.Printf("could not seed the forecast-warnings alarm rules: %v", err)
	}
	// Offers the Law of Storms ladder once per installation (ADR 0095): a
	// coherent tendency ladder that replaces three of the heavy-weather
	// set's five rules above. Most of its rules ship enabled -- unlike the
	// heavy-weather set, this reads off a published ladder rather than one
	// crew's uncalibrated numbers -- except the storm-signature tier, which
	// the source page itself hedges. A failure here is not fatal, for the
	// same reason as above.
	if err := seedLawOfStormsRules(); err != nil {
		log.Printf("could not seed the law-of-storms alarm rules: %v", err)
	}
	if err := loadAlarmTransports(); err != nil {
		log.Fatalf("failed to load alarm transports: %v", err)
	}
	globalAlarmDispatcher = newAlarmDispatcher()

	// Shared HTTP client for tile/basemap-asset fetches (with timeout to
	// prevent hangs), used both by the Esri World Imagery proxy and the
	// Carto basemap proxy (ADR 0067) - one upstream-fetch client for both,
	// same as they already share one SQLite-backed tileCache.
	tileFetchClient := newWorldImageryHTTPClient()

	// Every /api route, tiered per docs/adr/0040 §3 and registered through
	// registerAPIRoutes — the one place a route reaches Echo at all. See
	// buildAPIRoutes below for the full table and its tier assignments.
	registerAPIRoutes(e, sessions, buildAPIRoutes(sessions, tileFetchClient))

	// Off unless HELMCENTRAL_PPROF=1 (pprof.go); see registerPprofRoutes's
	// doc comment for why it defaults off.
	registerPprofRoutes(e)

	registerStaticHandler(e)

	loadAnchorWatch()
	loadAnchorPlacemarks()
	loadRoutes()
	loadDashboardPages()
	loadEngineProfiles()
	// Offers the anomaly-detection set (sensor health always, the rest as
	// vessel.engines/house_bank are completed): unlike the sets above, this
	// one is several independent markers rather than one, seeded again on
	// every vessel-settings save so a detector configured after this boot
	// still gets its rules. Must run after loadEngineProfiles just above --
	// the battery sub-set's readiness check resolves the house bank's linked
	// profile through engineProfilesState, which is empty until that call
	// runs; seeding first meant the full-bank-charging rules could never be
	// seeded at boot no matter how complete the vessel setup was (code
	// review finding 1). A failure here is not fatal, for the same reason
	// as the sets above.
	if vessel, err := loadVesselSettings(getEnv("SETTINGS_FILE", "../settings.yaml")); err != nil {
		log.Printf("could not load vessel settings for anomaly rule seeding: %v", err)
	} else if err := seedAnomalyRules(vessel); err != nil {
		log.Printf("could not seed the anomaly alarm rules: %v", err)
	}
	// Radar (ADR 0062, amended for the mayara SignalK plugin): a target store
	// fed by radarPoller (radar_source.go), which polls the plugin's proxied
	// REST endpoint through the already-configured SignalK connection —
	// there is no separate mayara host/port, and no push path through
	// SignalK to subscribe to instead. Initialised unconditionally, same as
	// the SignalK snapshot below — an absent or unreachable plugin is a live
	// "disabled"/"mayara-unreachable" source in the telemetry payload, not
	// an absent store.
	globalRadarTargetStore = newRadarTargetStore()

	// All vessel data arrives over the SignalK delta stream (ADR 0037). There
	// is no REST read path to fall back to: a dropped stream surfaces as an
	// outage rather than being papered over.
	streamCtx, cancelStream := context.WithCancel(context.Background())
	defer cancelStream()
	go newSignalKStreamClient(globalSignalKSnapshot, getEnv("SETTINGS_FILE", "../settings.yaml")).run(streamCtx)
	go newRadarPoller(globalRadarTargetStore, getEnv("SETTINGS_FILE", "../settings.yaml")).run(streamCtx)
	// The document indexer (above): started once at boot and woken
	// immediately so any document left pending/extract or pending/enrich by
	// a crash or restart resumes right away rather than waiting for the
	// next upload or reindex to wake it.
	go docIndexer.Run(streamCtx)
	docIndexer.Wake()
	// Drops non-self vessel contexts (AIS targets) this box has not heard
	// from in an hour, so every whole-tree copy of the snapshot does not get
	// a little slower every week it runs (backend-perf-audit.md Tier 1 #3).
	go startVesselContextSweeper(streamCtx, vesselContextSweepInterval)
	go startAlarmEvaluator(streamCtx, alarmEvaluationInterval)
	go globalAlarmDispatcher.run(streamCtx)
	go startNotificationDrainer(streamCtx, notifyDrainInterval)
	go startStreamWatchdog(streamCtx, watchdogCheckInterval)
	go startHeartbeat(streamCtx, heartbeatCheckInterval)
	go startAnchorDragWatcher(streamCtx, anchorDragCheckInterval)
	go startAnchorAutoRaiseWatcher(streamCtx, autoRaiseCheckInterval)
	go startForecastWarningsFetcher(streamCtx, forecastWarningsFetchInterval)
	// Anomaly detection (sensor health, full-bank charging, engine
	// differentials): a failed baseline load is logged and leaves twin
	// residuals absent rather than reporting zero offsets (see
	// loadEngineBaseline's own doc comment); the refresher then keeps it
	// current every 24h.
	if b, err := loadEngineBaseline(anomalyTwinBaselinePath()); err != nil {
		log.Printf("anomaly engine baseline: could not load at startup, twin residuals stay absent until the next refresh: %v", err)
	} else if len(b.Engines) > 0 {
		globalEngineBaselineCache.set(b)
	}
	go startAnomalyDetector(streamCtx, anomalyDetectorInterval)
	go startTwinBaselineRefresher(streamCtx, anomalyBaselineRefreshInterval)
	// Gust ladder + solar Influx queries (Tier 1 #1): buildVesselStatePayload
	// and buildSolarStatePayload used to run these live on every call, up to
	// several times a second per stream client. This refreshes them once on
	// a shared 30s ticker instead; the builders just read the cache.
	go startTelemetryInfluxTicker(streamCtx)

	go startTrackPoller(streamCtx, trackPollInterval)
	go startTideAutoUpdater(streamCtx, 30*time.Minute)
	// Sweeps expired sessions once at startup and hourly thereafter
	// (docs/adr/0040). Runs regardless of auth.mode — a mode:none boat can
	// still have leftover session rows from a previous mode:signalk run, and
	// this is cheap to run unconditionally.
	go startSessionSweeper(streamCtx, sessions, sessionSweepInterval)

	// Battery & Power tile dawn projection (battery-power-tile-pfd-fixes
	// plan, Phase 4): names which basis the tile falls back to before the
	// first request ever asks, per the AGENTS.md exception rule for the
	// linear (live-rate) fallback.
	logOvernightStartupMode()

	addr := fmt.Sprintf(":%s", port)
	log.Printf("Starting server on %s", addr)
	if err := e.Start(addr); err != nil && err != http.ErrServerClosed {
		log.Fatalf("error starting server: %v", err)
	}
}

// buildAPIRoutes is the single source of truth for every /api endpoint and
// its auth tier (docs/adr/0040 §3). It is data, not a sequence of e.GET/
// e.POST calls: a route registered in the wrong tier here is a visible
// mistake in this table, whereas a scattered per-call tag would silently
// default to open if forgotten. main() is the only caller in production;
// tests call it directly to build and register the exact same table
// (auth_middleware_test.go's route-coverage walk, most notably).
//
// tileFetchClient is passed in because it's a plain local value in main()
// (not a package-level global like globalTileCache/
// globalNearbyContactStore, which the closures below read directly). It
// backs both the Esri World Imagery routes and the Carto basemap routes
// (ADR 0067) - one shared upstream-fetch client, same reasoning as the one
// shared tileCache both write into.
func buildAPIRoutes(sessions *sessionStore, tileFetchClient *http.Client) []apiRoute {
	return []apiRoute{
		// ── public: no session required ─────────────────────────────────
		{http.MethodGet, "/api/health", tierPublic, healthCheck},
		{http.MethodPost, "/api/auth/login", tierPublic, loginHandler(sessions)},
		{http.MethodPost, "/api/auth/logout", tierPublic, logoutHandler(sessions)},
		{http.MethodGet, "/api/auth/me", tierPublic, meHandler(sessions)},
		{http.MethodGet, "/api/auth/mode", tierPublic, authModeHandler},
		// The wall display browser's capability probe
		// (frontend/public/display-probe.html) reports into the backend log
		// rather than rendering on a 1920x360 screen with no keyboard or
		// mouse. Public tier: the screen may never sign in, and the only
		// effect is a log line.
		{http.MethodPost, "/api/display-probe", tierPublic, displayProbeHandler},

		// ── read: readonly and above ────────────────────────────────────
		{http.MethodGet, "/api/vessel-state", tierRead, vesselState},
		{http.MethodGet, "/api/stream", tierRead, telemetryStream},
		{http.MethodGet, "/api/signalk/paths", tierRead, signalKPathsHandler},
		{http.MethodGet, "/api/alarms", tierRead, alarmsHandler},
		{http.MethodGet, "/api/alarms/log", tierRead, alarmLogHandler},
		{http.MethodGet, "/api/alarm-rules", tierRead, listAlarmRulesHandler},
		// The frozen/impossible/silent-source alarm card's own "Ignore this
		// sensor" action (alarm_ignored_sensors.go) -- not a settings list.
		{http.MethodGet, "/api/alarms/ignored-sensors", tierRead, listIgnoredSensorsHandler},
		{http.MethodGet, "/api/electrical-state", tierRead, electricalState},
		{http.MethodGet, "/api/electrical/overnight", tierRead, electricalOvernightHandler},
		{http.MethodGet, "/api/solar-state", tierRead, solarState},
		{http.MethodGet, "/api/tanks-state", tierRead, tanksState},
		{http.MethodGet, "/api/nearby-vessels", tierRead, nearbyVessels},
		{http.MethodGet, "/api/nearby-vessels/:key/sightings", tierRead, getNearbyVesselSightingsHandler(globalNearbyContactStore)},
		// mayara ARPA radar targets, polled through the mayara SignalK plugin
		// (ADR 0062 amendment). Read-only, same tier as nearby-vessels: this
		// is the REST equivalent of the "radar-targets" telemetry event,
		// curl-able before any UI exists.
		{http.MethodGet, "/api/radar/targets", tierRead, radarTargetsHandler},
		// The radar picture overlay's capabilities proxy (legend, spoke
		// geometry, range table), same tier and same plugin path shape as
		// targets above but a distinct endpoint: mayara's REST surface, not
		// the delta stream (radar_capabilities.go).
		{http.MethodGet, "/api/radar/capabilities", tierRead, radarCapabilitiesHandler},
		// The radar picture overlay's spoke stream: mayara-server itself, not
		// the SignalK plugin (which 404s for spokes — plan: "Why the backend
		// relays rather than the browser connecting direct"). Upgraded to
		// WebSocket inside the handler rather than declared as one here,
		// same tier as the REST radar routes above since it is equally
		// read-only (radar_spoke_relay.go).
		{http.MethodGet, "/api/radar/spokes", tierRead, radarSpokeRelayHandler},
		{http.MethodGet, "/api/weather-today", tierRead, weatherToday},
		{http.MethodGet, "/api/weather-forecast", tierRead, weatherForecast},
		{http.MethodGet, "/api/weather-providers", tierRead, weatherProvidersHandler},
		{http.MethodGet, "/api/wave-forecast", tierRead, waveForecast},
		{http.MethodGet, "/api/wave-providers", tierRead, waveProvidersHandler},
		{http.MethodGet, "/api/poi", tierRead, poiNearby},
		{http.MethodGet, "/api/poi-providers", tierRead, poiProvidersHandler},
		{http.MethodGet, "/api/upper-air", tierRead, upperAirForecast},
		{http.MethodGet, "/api/upper-air-providers", tierRead, upperAirProvidersHandler},
		{http.MethodGet, "/api/forecast-warnings", tierRead, forecastWarningsHandler},
		{http.MethodGet, "/api/forecast-warnings-providers", tierRead, forecastWarningsProvidersHandler},
		{http.MethodGet, "/api/tide-today", tierRead, tideToday},
		{http.MethodGet, "/api/tide-providers", tierRead, tideProvidersHandler},
		{http.MethodGet, "/api/tide-stations", tierRead, tideStationsHandler},
		{http.MethodGet, "/api/tide-chart", tierRead, tideChartHandler},
		{http.MethodGet, "/api/tide-nearest", tierRead, tideNearestHandler},
		{http.MethodGet, "/api/place-name", tierRead, placeName},
		{http.MethodGet, "/api/place-name-providers", tierRead, placeNameProvidersHandler},
		{http.MethodGet, "/api/anchor-watch", tierRead, getAnchorWatch},
		{http.MethodGet, "/api/anchor-watch/placemarks", tierRead, listPlacemarksHandler},
		{http.MethodGet, "/api/anchor-watch/trails/self", tierRead, getSelfTrailHandler},
		{http.MethodGet, "/api/anchor-watch/trails/ais/:id", tierRead, getAISTrailHandler},
		{http.MethodGet, "/api/anchor-watch/trails/ais", tierRead, getAllAISTrailsHandler},
		{http.MethodGet, "/api/dashboard-pages", tierRead, listDashboardPagesHandler},
		{http.MethodGet, "/api/dashboard-pages/:id", tierRead, getDashboardPageHandler},
		// The pinned indicator ribbon (ADR 0082): one vessel-level lamp strip,
		// promoted out of the per-page widget above.
		{http.MethodGet, "/api/dashboard-ribbon", tierRead, getDashboardRibbonHandler},
		// Wall displays (ADR 0110): the physical screens a page can be
		// assigned to, a sibling of dashboard pages in the same locked file
		// (displays.go). No get-by-id or lookup-by-slug — the list is one
		// small array the wall fetches on mount and slug resolution is a
		// client-side find.
		{http.MethodGet, "/api/displays", tierRead, listDisplaysHandler},
		{http.MethodGet, "/api/routes", tierRead, listRoutesHandler},
		{http.MethodGet, "/api/routes/:id", tierRead, getRouteHandler},
		{http.MethodGet, "/api/routes/active", tierRead, getActiveRouteHandler},
		{http.MethodGet, "/api/tracks", tierRead, getTracksHandler},
		{http.MethodGet, "/api/tracks/motoring", tierRead, getMotoringTrackHandler},
		{http.MethodGet, "/api/depth-trend", tierRead, depthTrend},
		{http.MethodGet, "/api/telemetry/history", tierRead, telemetryHistoryHandler},
		{http.MethodGet, "/api/equipment-profiles", tierRead, equipmentProfilesHandler},
		{http.MethodGet, "/api/equipment-profiles/:id", tierRead, getEquipmentProfileHandler},
		{http.MethodGet, "/api/equipment-profiles/:id/download", tierRead, downloadEquipmentProfileHandler},
		{http.MethodGet, "/api/engine-profiles", tierRead, engineProfilesHandler},
		{http.MethodGet, "/api/czone/switches", tierRead, getCZoneSwitchesHandler},
		{http.MethodGet, "/api/autopilot", tierRead, getAutopilotHandler},
		{http.MethodGet, "/api/world-imagery/:z/:x/:y", tierRead, proxyWorldImageryTileHandler(globalTileCache, tileFetchClient)},
		{http.MethodGet, "/api/world-imagery/prefetch/:jobId", tierRead, prefetchStatusHandler()},
		{http.MethodGet, "/api/gshhg-coastline", tierRead, gshhgCoastlineHandler},
		// Carto vector basemap proxy + offline cache (ADR 0067) - same
		// tierRead as world-imagery above: read-only, no session write
		// implied by fetching a map tile/style/font/sprite.
		{http.MethodGet, "/api/basemap/style/:name", tierRead, basemapStyleHandler(globalTileCache, tileFetchClient)},
		{http.MethodGet, "/api/basemap/tilejson", tierRead, basemapTileJSONHandler(globalTileCache, tileFetchClient)},
		{http.MethodGet, "/api/basemap/tiles/:z/:x/:y", tierRead, basemapVectorTileHandler(globalTileCache, tileFetchClient)},
		{http.MethodGet, "/api/basemap/fonts/:fontstack/:range", tierRead, basemapFontsHandler(globalTileCache, tileFetchClient)},
		{http.MethodGet, "/api/basemap/sprite/:name", tierRead, basemapSpriteHandler(globalTileCache, tileFetchClient)},
		// Onboard OpenRouter-backed assistant (ADR 0093). Status and reading
		// a conversation's history are read-only, same tier as everything
		// else above.
		{http.MethodGet, "/api/assistant/status", tierRead, assistantStatusHandler},
		{http.MethodGet, "/api/assistant/models", tierAdmin, assistantModelsHandler},
		{http.MethodGet, "/api/assistant/conversations", tierRead, listAssistantConversationsHandler},
		{http.MethodGet, "/api/assistant/conversations/:id", tierRead, getAssistantConversationHandler},
		// Rejoining a reply still being written (ADR 0105): replay-then-live
		// SSE if a run is in flight, 204 if not - read-only, same tier as
		// the conversation read above.
		{http.MethodGet, "/api/assistant/conversations/:id/run", tierRead, getAssistantRunHandler},
		// The in-app Help sheet's page fetch (ADR 0095), same embedded
		// pages as read_help above, reached by direct id instead of a tool
		// call. Wildcard path, not a :id param: page ids contain a slash
		// ("features/dashboard").
		{http.MethodGet, "/api/help/*", tierRead, getHelpPageHandler(func() []helpPage { return globalHelp })},
		// Settings -> Logs: in-memory log buffer retrieval and live SSE stream
		{http.MethodGet, "/api/logs", tierRead, getLogsHandler},
		{http.MethodGet, "/api/logs/stream", tierRead, logsStreamHandler},

		// Document library (ADR 0106): metadata, virtual folders, tags,
		// content bytes and full text, backed by globalDocumentStore
		// (documents_store.go) and a flat hash-named folder on disk
		// (documents_handlers.go). The two static routes ("move", "tags")
		// are listed ahead of the "/:id" routes below purely for
		// readability - Echo's router already prioritises a static segment
		// over a param one regardless of registration order (confirmed
		// against vendored router.go: "Search order/priority is: static >
		// param > any"), so "GET /api/documents/tags" can never be captured
		// by "GET /api/documents/:id" either way.
		{http.MethodGet, "/api/documents", tierRead, listDocumentsHandler},
		{http.MethodGet, "/api/documents/tags", tierRead, documentTagsHandler},
		// E1c: semantic-search status. Also a static segment ahead of
		// "/:id" for the same reason "tags" is - see the comment above.
		{http.MethodGet, "/api/documents/embeddings", tierRead, documentsEmbeddingsStatusHandler},
		{http.MethodGet, "/api/documents/:id", tierRead, getDocumentHandler},
		{http.MethodGet, "/api/documents/:id/content", tierRead, documentContentHandler},
		{http.MethodGet, "/api/documents/:id/text", tierRead, documentTextHandler},
		{http.MethodGet, "/api/document-folders", tierRead, listDocumentFoldersHandler},

		// Notes (plan "Notes and the Boat's Manual", ADR 0114): a note is a
		// documents row with kind='note' (notes_store.go), so it shares the
		// document library's store, folders, tags, search and this route
		// table's tiers wholesale. "/api/notes/:id" registered after the
		// bare "/api/notes" for readability only - see the read-tier
		// comment above the document library's own routes on why Echo's
		// router never needs that ordering.
		{http.MethodGet, "/api/notes", tierRead, listNotesHandler},
		{http.MethodGet, "/api/notes/:id", tierRead, getNoteHandler},

		// Checklist runs (plan "Notes and the Boat's Manual" §3/§4, ADR
		// 0118): a run stores which of a note's checklist items are
		// ticked (checklist_runs_store.go), never the item list itself.
		{http.MethodGet, "/api/notes/:id/checklist-runs/active", tierRead, getActiveChecklistRunHandler},

		// Manuals (plan "Notes and the Boat's Manual" §4, ADR 0114/0116): a
		// manual is a document_folders row with role='manual'
		// (manuals_store.go). "/api/manuals/:id/tree" registered after the
		// bare "/api/manuals" for readability only - see the read-tier
		// comment above the document library's own routes on why Echo's
		// router never needs that ordering.
		{http.MethodGet, "/api/manuals", tierRead, listManualsHandler},
		{http.MethodGet, "/api/manuals/:id/tree", tierRead, manualTreeHandler},

		// Inventory (plan "Inventory: the equipment registry and locations"):
		// the equipment registry's own API family, under /api/inventory/ -
		// deliberately separate from /api/equipment-profiles just above,
		// which stays exactly where it is (plan's own decision: profiles
		// move in the UI only, in a later phase; the backend route never
		// changes). "/api/inventory/equipment/:id" registered after the bare
		// "/api/inventory/equipment" for readability only - see the
		// read-tier comment above the document library's own routes on why
		// Echo's router never needs that ordering.
		{http.MethodGet, "/api/inventory/zones", tierRead, listZonesHandler},
		{http.MethodGet, "/api/inventory/equipment", tierRead, listEquipmentHandler},
		{http.MethodGet, "/api/inventory/equipment/:id", tierRead, getEquipmentHandler},

		// ── write: readwrite and above — commands equipment or changes
		//           stored state that isn't itself a security setting ────
		{http.MethodPost, "/api/alarms/:id/acknowledge", tierWrite, acknowledgeAlarmHandler},
		{http.MethodPost, "/api/alarms/:id/silence", tierWrite, silenceAlarmHandler},
		{http.MethodPost, "/api/alarm-rules", tierWrite, createAlarmRuleHandler},
		{http.MethodPut, "/api/alarm-rules/:id", tierWrite, updateAlarmRuleHandler},
		{http.MethodDelete, "/api/alarm-rules/:id", tierWrite, deleteAlarmRuleHandler},
		{http.MethodPost, "/api/alarms/ignored-sensors", tierWrite, ignoreSensorHandler},
		{http.MethodDelete, "/api/alarms/ignored-sensors/:identifier", tierWrite, unignoreSensorHandler},
		{http.MethodPost, "/api/anchor-watch", tierWrite, setAnchorWatch},
		{http.MethodPatch, "/api/anchor-watch", tierWrite, patchAnchorWatch},
		{http.MethodDelete, "/api/anchor-watch", tierWrite, deleteAnchorWatch},
		{http.MethodPost, "/api/anchor-watch/placemarks", tierWrite, createPlacemarkHandler},
		{http.MethodDelete, "/api/anchor-watch/placemarks/:id", tierWrite, deletePlacemarkHandler},
		{http.MethodPost, "/api/dashboard-pages", tierWrite, createDashboardPageHandler},
		{http.MethodPut, "/api/dashboard-pages/order", tierWrite, reorderDashboardPagesHandler},
		{http.MethodPatch, "/api/dashboard-pages/:id", tierWrite, patchDashboardPageHandler},
		{http.MethodDelete, "/api/dashboard-pages/:id", tierWrite, deleteDashboardPageHandler},
		{http.MethodPut, "/api/dashboard-ribbon", tierWrite, putDashboardRibbonHandler},
		{http.MethodPost, "/api/displays", tierWrite, createDisplayHandler},
		{http.MethodPatch, "/api/displays/:id", tierWrite, patchDisplayHandler},
		{http.MethodDelete, "/api/displays/:id", tierWrite, deleteDisplayHandler},
		{http.MethodPost, "/api/routes", tierWrite, createRouteHandler},
		{http.MethodPatch, "/api/routes/:id", tierWrite, patchRouteHandler},
		{http.MethodDelete, "/api/routes/:id", tierWrite, deleteRouteHandler},
		{http.MethodPost, "/api/equipment-profiles", tierWrite, createEquipmentProfileHandler},
		{http.MethodDelete, "/api/equipment-profiles/:id", tierWrite, deleteEquipmentProfileHandler},
		{http.MethodPut, "/api/equipment-profiles/:id", tierWrite, updateEquipmentProfileHandler},
		{http.MethodPut, "/api/engine-profiles/:id", tierWrite, updateEngineProfileHandler},
		{http.MethodPost, "/api/routes/:id/activate", tierWrite, activateRouteHandler},
		{http.MethodPost, "/api/routes/deactivate", tierWrite, deactivateRouteHandler},
		{http.MethodPut, "/api/czone/switches/:id/state", tierWrite, putCZoneSwitchStateHandler},
		{http.MethodPost, "/api/generator/start", tierWrite, postGeneratorStartHandler},
		{http.MethodPost, "/api/generator/stop", tierWrite, postGeneratorStopHandler},
		{http.MethodPost, "/api/autopilot/engage", tierWrite, postAutopilotEngageHandler},
		{http.MethodPost, "/api/autopilot/disengage", tierWrite, postAutopilotDisengageHandler},
		{http.MethodPut, "/api/autopilot/state", tierWrite, putAutopilotStateHandler},
		{http.MethodPut, "/api/autopilot/mode", tierWrite, putAutopilotModeHandler},
		{http.MethodPut, "/api/autopilot/target", tierWrite, putAutopilotTargetHandler},
		{http.MethodPut, "/api/autopilot/target/adjust", tierWrite, putAutopilotTargetAdjustHandler},
		{http.MethodPost, "/api/autopilot/tack/:side", tierWrite, postAutopilotTackHandler},
		{http.MethodPost, "/api/autopilot/gybe/:side", tierWrite, postAutopilotGybeHandler},
		{http.MethodPut, "/api/autopilot/dodge", tierWrite, putAutopilotDodgeHandler},
		{http.MethodDelete, "/api/autopilot/dodge", tierWrite, deleteAutopilotDodgeHandler},
		{http.MethodPost, "/api/world-imagery/prefetch", tierWrite, prefetchWorldImageryHandler(globalTileCache, tileFetchClient)},
		{http.MethodDelete, "/api/world-imagery/cache", tierWrite, deleteWorldImageryCacheHandler(globalTileCache)},
		{http.MethodPost, "/api/assistant/conversations", tierWrite, createAssistantConversationHandler},
		{http.MethodDelete, "/api/assistant/conversations/:id", tierWrite, deleteAssistantConversationHandler},
		// Every tool the assistant can call is read-only, but this is write
		// tier anyway: it spends the operator's OpenRouter credit and stores
		// state (a new message row), which a readonly session must not
		// trigger (ADR 0093).
		{http.MethodPost, "/api/assistant/conversations/:id/messages", tierWrite, postAssistantMessageHandler},
		// Stop, for real (ADR 0105): cancels the in-flight run itself, not
		// just this tab's local stream. Write tier: it ends a run that
		// spent the operator's OpenRouter credit, the same reasoning as the
		// POST above.
		{http.MethodPost, "/api/assistant/conversations/:id/run/cancel", tierWrite, postAssistantRunCancelHandler},

		// Document library writes (ADR 0106). "move" and "tags" ahead of the
		// "/:id" routes purely for readability - see the read-tier comment
		// above on why Echo's router never needs that ordering.
		{http.MethodPost, "/api/documents", tierWrite, uploadDocumentHandler},
		{http.MethodPost, "/api/documents/move", tierWrite, moveDocumentsHandler},
		{http.MethodPatch, "/api/documents/:id", tierWrite, patchDocumentHandler},
		{http.MethodDelete, "/api/documents/:id", tierWrite, deleteDocumentHandler},
		{http.MethodPost, "/api/documents/:id/reindex", tierWrite, reindexDocumentHandler},
		{http.MethodPost, "/api/document-folders", tierWrite, createDocumentFolderHandler},
		{http.MethodPatch, "/api/document-folders/:id", tierWrite, patchDocumentFolderHandler},
		{http.MethodDelete, "/api/document-folders/:id", tierWrite, deleteDocumentFolderHandler},

		// Notes writes (plan "Notes and the Boat's Manual", ADR 0114).
		{http.MethodPost, "/api/notes", tierWrite, createNoteHandler},
		{http.MethodPatch, "/api/notes/:id", tierWrite, patchNoteHandler},

		// Checklist runs writes (plan "Notes and the Boat's Manual" §3/§4,
		// ADR 0118). DELETE clears abandoned_at only - see
		// abandonChecklistRunHandler's own doc comment
		// (checklist_runs_handlers.go).
		{http.MethodPost, "/api/notes/:id/checklist-runs", tierWrite, createChecklistRunHandler},
		{http.MethodPatch, "/api/checklist-runs/:runId/items", tierWrite, tickChecklistItemHandler},
		{http.MethodPost, "/api/checklist-runs/:runId/complete", tierWrite, completeChecklistRunHandler},
		{http.MethodDelete, "/api/checklist-runs/:runId", tierWrite, abandonChecklistRunHandler},

		// Manuals writes (plan "Notes and the Boat's Manual" §4, ADR
		// 0114/0116). DELETE clears the role flag only - see
		// clearManualHandler's own doc comment (manuals_handlers.go).
		{http.MethodPost, "/api/manuals", tierWrite, createManualHandler},
		{http.MethodDelete, "/api/manuals/:id", tierWrite, clearManualHandler},
		{http.MethodPost, "/api/manuals/:id/reorder", tierWrite, reorderManualHandler},

		// Inventory writes (plan "Inventory: the equipment registry and
		// locations"). DELETE on a zone/bin still in use answers 409 with
		// {error, in_use} rather than the ordinary writeDocumentError
		// mapping - see deleteZoneHandler/deleteBinHandler's own doc
		// comments (inventory_handlers.go).
		{http.MethodPost, "/api/inventory/zones", tierWrite, createZoneHandler},
		{http.MethodPut, "/api/inventory/zones/:id", tierWrite, updateZoneHandler},
		{http.MethodDelete, "/api/inventory/zones/:id", tierWrite, deleteZoneHandler},
		{http.MethodPost, "/api/inventory/bins", tierWrite, createBinHandler},
		{http.MethodPut, "/api/inventory/bins/:id", tierWrite, updateBinHandler},
		{http.MethodDelete, "/api/inventory/bins/:id", tierWrite, deleteBinHandler},
		{http.MethodPost, "/api/inventory/equipment", tierWrite, createEquipmentHandler},
		{http.MethodPut, "/api/inventory/equipment/:id", tierWrite, updateEquipmentHandler},
		{http.MethodDelete, "/api/inventory/equipment/:id", tierWrite, deleteEquipmentHandler},
		{http.MethodPatch, "/api/inventory/equipment/:id/documents", tierWrite, patchEquipmentDocumentsHandler},
		// Photo routes (ADR 0127): a write-tier file intake, same size limit
		// and MIME sniffing as POST /api/documents just above - see
		// uploadEquipmentPhotoHandler's own doc comment (inventory_handlers.go).
		{http.MethodPost, "/api/inventory/equipment/:id/photos", tierWrite, uploadEquipmentPhotoHandler},
		{http.MethodPut, "/api/inventory/equipment/:id/photos", tierWrite, setEquipmentPhotoOrderHandler},
		{http.MethodDelete, "/api/inventory/equipment/:id/photos/:documentId", tierWrite, deleteEquipmentPhotoHandler},

		// ── admin: settings, secrets, plugin config, alarm transports ───
		{http.MethodGet, "/api/settings", tierAdmin, getSettingsHandler},
		{http.MethodPost, "/api/settings", tierAdmin, updateSettingsHandler},
		// Settings -> Vessel (anomaly detection): candidates is a pure read
		// of live instances for the picker; GET/POST the vessel.* block
		// itself, admin-tiered alongside settings since it is one.
		{http.MethodGet, "/api/vessel/candidates", tierAdmin, getVesselCandidatesHandler},
		{http.MethodGet, "/api/vessel", tierAdmin, getVesselSettingsHandler},
		{http.MethodPost, "/api/vessel", tierAdmin, postVesselSettingsHandler},
		{http.MethodGet, "/api/settings/signalk", tierAdmin, getSignalKSettingsHandler},
		// Probe only — persisting the address is POST /api/settings' job
		// alone (ADR 0028). There is deliberately no POST /api/settings/signalk.
		{http.MethodPost, "/api/settings/signalk/test", tierAdmin, testSignalKConnectionHandler},
		// Also a pure read: sweeps the local network and reports what it
		// found. Accepting a result saves through POST /api/settings like
		// any other write. Gated admin, alongside the rest of the settings
		// workflow it belongs to.
		{http.MethodPost, "/api/signalk/discover", tierAdmin, discoverSignalKHandler},
		{http.MethodGet, "/api/settings/secrets", tierAdmin, getSecretsSettingsHandler},
		{http.MethodPost, "/api/settings/secrets", tierAdmin, updateSecretsSettingsHandler},
		{http.MethodPost, "/api/settings/secrets/import-env", tierAdmin, importEnvSecretsHandler},
		{http.MethodGet, "/api/plugins/:type/:id", tierAdmin, getPluginInfoHandler},
		{http.MethodPost, "/api/plugins/:type/:id/overrides", tierAdmin, postPluginOverridesHandler},
		{http.MethodDelete, "/api/plugins/:type/:id/overrides", tierAdmin, deletePluginOverridesHandler},
		{http.MethodPost, "/api/plugins/:type/:id/config", tierAdmin, postPluginConfigHandler},
		{http.MethodGet, "/api/alarm-transports", tierAdmin, getAlarmTransportsHandler},
		{http.MethodPost, "/api/alarm-transports", tierAdmin, setAlarmTransportsHandler},
		{http.MethodPost, "/api/alarm-transports/test", tierAdmin, testAlarmTransportsHandler},
		{http.MethodGet, "/api/alarm-transports/webpush/key", tierAdmin, webPushKeyHandler},
		{http.MethodPost, "/api/alarm-transports/webpush/subscribe", tierAdmin, subscribeWebPushHandler},
		{http.MethodPost, "/api/alarm-transports/webpush/unsubscribe", tierAdmin, unsubscribeWebPushHandler},
		{http.MethodGet, "/api/alarm-transports/webpush/subscriptions", tierAdmin, listWebPushSubscriptionsHandler},
		{http.MethodDelete, "/api/alarm-transports/webpush/subscriptions/:id", tierAdmin, deleteWebPushSubscriptionHandler},
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
// request: Influx when configured, read from the background ticker's cache
// (telemetry_influx_cache.go) rather than queried live, otherwise the
// in-memory ring buffer (inMemoryMaxWindGustKtsFor). Previously both were
// computed unconditionally and the in-memory result discarded whenever Influx
// was configured - wasted CPU/allocations on every /api/vessel-state request
// on Influx-backed deployments.
func computeMaxGustKtsFor(windows []string) map[string]float64 {
	if influxTelemetryConfigured() {
		return cachedMaxGustKtsFor(windows, time.Now())
	}
	return inMemoryMaxWindGustKtsFor(windows)
}

// computeMaxGustTrueKtsFor is computeMaxGustKtsFor's true-wind counterpart
// (ADR 0130). Unlike the apparent ladder, it has no Influx-backed path yet —
// only the in-memory ring buffer (trueWindGustHistory, recorded alongside
// the apparent one in sampleTracks) — so a true-wind gust history does not
// survive a backend restart on an Influx-configured boat the way the
// apparent one does. Revisit if that gap matters in practice.
func computeMaxGustTrueKtsFor(windows []string) map[string]float64 {
	return inMemoryMaxTrueWindGustKtsFor(windows)
}

// buildVesselStatePayload produces the /api/vessel-state body. It is separate
// from the handler so the SSE stream can emit the identical shape without the
// two drifting apart.
func buildVesselStatePayload() map[string]any {
	state := vesselStateData{
		Status:               getEnv("VESSEL_STATUS", "At Anchor"),
		Datetime:             time.Now().UTC(),
		Depth:                -1,
		LengthOverallM:       -1,
		CurrentDriftKts:      -1,
		CurrentSetDeg:        -1,
		Latitude:             -1,
		Longitude:            -1,
		HeadingTrue:          -1,
		WindSpeedApparentKts: -1,
		WindAngleApparentDeg: -1,
		WindAngleRelativeDeg: -1,
		// True wind (ADR 0129 / ADR 0130) — see fetchSignalKVesselState's
		// own comment on these fields for why they default to -1/"" rather
		// than the apparent fields' 0/starboard fallback.
		WindSpeedTrueKts:         -1,
		WindAngleTrueDeg:         -1,
		WindAngleTrueRelativeDeg: -1,
		WindDirectionTrueDeg:     -1,
		Engine0RPM:               -1,
		Engine1RPM:               -1,
		// Unknown, not zero, when SignalK is unconfigured and this fallback
		// literal never gets overwritten below.
		DepthLastUpdateAge:    -1,
		PositionLastUpdateAge: -1,
		WindLastUpdateAge:     -1,
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

	// True wind's own MAX GUST ladder (ADR 0130), same window ladder as the
	// apparent one above but NOT the same "no data clamps to 0" treatment:
	// a boat with no true-wind source, or one that just restarted with an
	// empty ring buffer, must read as unknown (-1, which the frontend maps
	// to '—'), not as a confident 0kt dead calm. So a window that itself has
	// no samples keeps its -1 sentinel untouched, and the monotonic
	// (longer-window-never-less-than-shorter) clamp only starts applying
	// once a shorter window has actually produced a real (>=0) value -
	// previousTrue itself starts at -1 (nothing to enforce yet) and is only
	// ever updated from a real value, never from an untouched sentinel.
	//
	// This ladder's own "1h" entry IS the Current Conditions tile's "obs"
	// marker too (ADR 0129), not a separate maxTrueWindKts1h field anymore:
	// that field used to read a second, independent buffer
	// (trueWindSpeedHistory, raw m/s, gated by the heavy-weather trend's own
	// staleness check) behind a DIFFERENT freshness gate than this ladder's
	// trueWindGustHistory (already-knots, gated on state.WindSpeedTrueKts
	// >= 0), so the two tiles could show two different "last hour" true wind
	// figures for the same boat at the same moment - a code-review finding,
	// 2026-09-25; see docs/adr/0130's amendment. The frontend now reads
	// max_gust_true_kts['1h'] directly for both.
	maxGustTrueKts := computeMaxGustTrueKtsFor(gustWindowLadder)
	previousTrue := -1.0
	for _, window := range gustWindowLadder {
		value := maxGustTrueKts[window]
		if value >= 0 {
			if value < previousTrue {
				value = previousTrue
			}
			previousTrue = value
		}
		maxGustTrueKts[window] = value
	}

	vesselPrefix := loadBoatVesselPrefix(settingsPath)
	if vesselPrefix == "" {
		vesselPrefix = "M/V"
	}

	// No masking fallback: an unpublished LOA must reach the frontend as
	// JSON null, not the internal lookupNumber -1 sentinel, which could be
	// mistaken for a real (negative) length.
	var lengthOverallM any
	if state.LengthOverallM > 0 {
		lengthOverallM = state.LengthOverallM
	}

	return map[string]any{
		"name":                           state.Name,
		"vessel_prefix":                  vesselPrefix,
		"status":                         state.Status,
		"datetime":                       state.Datetime.Format(time.RFC3339),
		"timezone":                       vesselLocalTimezoneName(state.Longitude),
		"depth":                          state.Depth,
		"depth_last_update_age_s":        state.DepthLastUpdateAge,
		"length_overall_m":               lengthOverallM,
		"current_drift_kts":              state.CurrentDriftKts,
		"current_set_deg":                state.CurrentSetDeg,
		"current_drift_impact_kts":       state.CurrentDriftImpactKts,
		"latitude":                       state.Latitude,
		"longitude":                      state.Longitude,
		"position_last_update_age_s":     state.PositionLastUpdateAge,
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
		"wind_speed_true_kts":            state.WindSpeedTrueKts,
		"wind_angle_true_deg":            state.WindAngleTrueDeg,
		"wind_side_true":                 state.WindSideTrue,
		"wind_angle_true_relative_deg":   state.WindAngleTrueRelativeDeg,
		"wind_direction_true_deg":        state.WindDirectionTrueDeg,
		"wind_last_update_age_s":         state.WindLastUpdateAge,
		"max_gust_kts":                   maxGustKts,
		"max_gust_true_kts":              maxGustTrueKts,
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
		"last_update_age_s":          state.LastUpdateAge,
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
		"datetime":          state.Datetime.Format(time.RFC3339),
		"source":            source,
		"current_w":         state.CurrentW,
		"today_kwh":         state.TodayKWh,
		"yesterday_kwh":     state.YesterdayKWh,
		"peak_today_w":      state.PeakTodayW,
		"last_update_age_s": state.LastUpdateAge,
		"controllers":       controllers,
		"trend_24h_total":   state.Trend24hTotal,
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
// read from the background ticker's cache (telemetry_influx_cache.go)
// rather than queried live — called only when influxTelemetryConfigured(),
// and does not fall back to the in-memory value if the cached result is
// stale or the query behind it failed (same Fallback Policy reasoning as
// ADR-0020: once Influx is enabled, its failures should be visible, not
// silently patched over).
func applyInfluxSolarOverride(state solarStateData) solarStateData {
	return cachedInfluxSolarOverride(state, time.Now().UTC())
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

	// The three derived fuel figures (ADR 0084), from the same
	// computeDerivedPaths pass a gauge bound to the same helmcentral.fuel.*
	// path reads, so the built-in Tanks tile and a configured gauge can never
	// disagree. Computed against the actual current instant rather than
	// `now` above, which tracks the (possibly REST-reported) tank datetime,
	// not the vessel clock the derivation's own node timestamps are read
	// against.
	derivedValues, derivedAges := computeDerivedPaths(time.Now().UTC())

	return map[string]any{
		"datetime": now.Format(time.RFC3339),
		"source":   source,
		"tanks":    tanks,
		// The freshest of the per-tank ages above: what the Tanks tile
		// itself goes stale on (ADR 0068).
		"last_update_age_s":    tanksFeedAge(tanks),
		"fuel_volume_m3":       derivedValues[fuelVolumePath],
		"fuel_volume_age_s":    derivedAges[fuelVolumePath],
		"fuel_time_to_empty_s": derivedValues[fuelTimeToEmptyPath],
		"fuel_range_m":         derivedValues[fuelRangeAtCurrentBurnPath],
		// Oldest input age of the range and time figures specifically, not
		// volume's -- an operator watching the footer's range/time readouts
		// go stale wants to know how old the reason for the dash is, and
		// volume has its own age already exposed on fuel_volume_age_s.
		"fuel_derived_age_s": derivedInputAge(derivedAges[fuelTimeToEmptyPath], derivedAges[fuelRangeAtCurrentBurnPath]),
	}
}
func tanksState(c echo.Context) error {
	return c.JSON(http.StatusOK, buildTanksStatePayload())
}

type nearbyVessel struct {
	// ID is the bare SignalK vessel id (the key fetchSignalKNearbyVessels
	// reads from vesselsTree()), always present and unique by construction.
	// It exists because Name is not a real identity - two boats can share a
	// display name, and unnamed vessels all fall back to the same
	// compactVesselID shape - so the frontend needs a field it can use for
	// React reconciliation and marker-selection matching. No omitempty: a
	// missing id here is a bug, not an absent optional value.
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Mmsi       string   `json:"mmsi,omitempty"`
	RangeM     float64  `json:"range_m"`
	AgeSeconds int      `json:"age_seconds"`
	SogKnots   *float64 `json:"sog_knots,omitempty"`
	Lat        float64  `json:"lat"`
	Lon        float64  `json:"lon"`
	SeenCount  int      `json:"seen_count"`
	LastSeenAt string   `json:"last_seen_at,omitempty"`

	// AIS collision figures from signalk-ais-target-prioritizer (ADR 0057),
	// all optional. A target that is opening rather than closing carries a
	// bearing but no CPA at all, and with the plugin uninstalled a target
	// carries none of them - hence pointers, so "not computed" stays
	// distinguishable from a closest approach of zero.
	//
	// BearingRad is radians, converted on ingest from the degrees the plugin
	// actually publishes (collisionBearingRadians).
	CpaM                *float64 `json:"cpa_m,omitempty"`
	TcpaSeconds         *float64 `json:"tcpa_seconds,omitempty"`
	BearingRad          *float64 `json:"bearing_rad,omitempty"`
	CollisionAlarmType  string   `json:"collision_alarm_type,omitempty"`
	CollisionAlarmState string   `json:"collision_alarm_state,omitempty"`

	// PositionSeen is the delta receive time this vessel's position was
	// last refreshed at (see fetchSignalKNearbyVessels), threaded through to
	// recordNearbyVesselContacts -> recordContactIfNew so its confirmation
	// dwell can tell "still ticking on a frozen position" apart from "a
	// fresh AIS report actually arrived" (see nearby_contacts.go's
	// pendingContact). json:"-" keeps it off the wire entirely: it's
	// internal plumbing, not something either the tile or the anchor-watch
	// map has any use for.
	PositionSeen time.Time `json:"-"`
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
					enrichNearbyVesselsWithContactHistory(globalNearbyContactStore, nearby)
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
		// Whether the AIS/radar feed behind the whole list has died — the
		// freshest contact's age, distinct from each vessel's own
		// age_seconds. Zero vessels in range stays -1 (unknown), never 0:
		// silence is not evidence the feed died (ADR 0068).
		"last_update_age_s": nearbyVesselsFeedAge(vessels),
	}
}
func nearbyVessels(c echo.Context) error {
	return c.JSON(http.StatusOK, buildNearbyVesselsPayload())
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
