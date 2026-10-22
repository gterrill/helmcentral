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

func main() {
	// migrate-db is a one-off, operator-run subcommand (ADR 0141), never
	// automatic at startup: it renames and copies data, so it has to be a
	// deliberate action, not something that could run again unintentionally
	// on a normal restart. This is the only flag the binary has, so a plain
	// os.Args check is enough - no flag package needed for one subcommand.
	if len(os.Args) > 1 && os.Args[1] == "migrate-db" {
		os.Exit(runMigrateDBCommand())
	}

	// convert-profile-rules is another one-off, operator-run subcommand: it
	// moves the old profile files into the database.
	if len(os.Args) > 1 && os.Args[1] == "convert-profile-rules" {
		os.Exit(runConvertProfileRulesCommand(os.Args[2:]))
	}

	e := echo.New()
	port := getEnv("PORT", "8080")

	// Fail-fast startup guard (ADR 0141): refuse to start against a
	// pre-upgrade data directory that still has documents.sqlite,
	// assistant.sqlite, nearby-contacts.sqlite, alarm-log.sqlite,
	// webpush-subscriptions.sqlite or plugin_overrides.sqlite at the exact
	// paths migrate-db itself resolves them to (legacyDocumentsDBPath and
	// friends - not merely default filenames beside helmcentral.sqlite,
	// since DOCUMENTS_DB_PATH/ASSISTANT_DB_PATH/NEARBY_CONTACTS_DB_PATH/
	// ALARM_LOG_DB/WEBPUSH_DB_PATH/PLUGIN_OVERRIDES_DB_PATH can each point
	// anywhere), rather than either ignoring that history or starting
	// against an empty combined file while it sits untouched at its old
	// path. Runs before any store below opens anything.
	if err := checkForLegacyDatabaseFiles(legacyDocumentsDBPath(), legacyAssistantDBPath(), legacyNearbyContactsDBPath(),
		legacyAlarmLogDBPath(), legacyWebPushDBPath(), legacyPluginOverridesDBPath()); err != nil {
		log.Fatalf("startup: %v", err)
	}

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
	// hand-editing companion JSON files over SSH), one of the six stores
	// sharing helmcentral.sqlite (ADR 0141). Must also be opened before
	// provider registration below, since loadWasm*Providers ->
	// manifestForWasmPlugin -> allowedHostsForWasmPlugin/
	// allowedSecretsForWasmPlugin check this store first. Fail fast on open
	// error, same reasoning as the other stores here - this store has no
	// encryption/integrity check to run, just a normal sqlite open.
	pos, err := newPluginOverridesStore(helmcentralDBPath())
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
	// Nearby Vessels tile), one of the six stores sharing helmcentral.sqlite
	// (ADR 0141). Fail fast on open error, same reasoning as the tile cache
	// above.
	ncs, err := newNearbyContactStore(helmcentralDBPath())
	if err != nil {
		log.Fatalf("failed to open nearby contacts store: %v", err)
	}
	globalNearbyContactStore = ncs

	// Track point log and passages (ADR 0176), in the same file. The recorder
	// is always on; a log that cannot be opened is a startup failure, not a
	// feature quietly turned off.
	pas, err := newPassageStore(helmcentralDBPath())
	if err != nil {
		log.Fatalf("failed to open passage store: %v", err)
	}
	if err := startPassageRecording(pas); err != nil {
		log.Fatalf("failed to start track point recording: %v", err)
	}

	// Alarm log (occurrence history and notification queue behind the alarm
	// centre), another of the six stores sharing helmcentral.sqlite (ADR
	// 0141). Fail fast on open error, same reasoning as the stores above.
	als, err := newAlarmLogStore(helmcentralDBPath())
	if err != nil {
		log.Fatalf("failed to open alarm log store: %v", err)
	}
	globalAlarmLogStore = als

	// Onboard assistant conversation history (ADR 0093), another of the six
	// stores sharing helmcentral.sqlite (ADR 0141). Fail fast on open
	// error, same reasoning as the stores above.
	as, err := newAssistantStore(helmcentralDBPath())
	if err != nil {
		log.Fatalf("failed to open assistant store: %v", err)
	}
	globalAssistantStore = as

	// Document store (ADR 0106): metadata, virtual folders, tags, chunks and
	// FTS5 search behind a flat, hash-named folder of file bytes - another
	// of the six stores sharing helmcentral.sqlite (ADR 0141). Fail fast
	// on open error, same reasoning as the other stores above. The
	// documents directory is created (not just the database's own parent,
	// which newDocumentStore already handles) so the boot sweep below
	// always has somewhere to os.ReadDir, even on a brand new install that
	// has never taken an upload yet.
	ds, err := newDocumentStore(helmcentralDBPath())
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

	// Registered web push devices, another of the six stores sharing
	// helmcentral.sqlite (ADR 0141): durable device registrations whose loss
	// cannot be recovered without physically revisiting every phone, unlike
	// the alarm log's own prunable history and self-expiring queue - a
	// distinction that lives in the row's own lifetime, not in which file
	// it sits in.
	wps, err := newWebPushSubscriptionStore(helmcentralDBPath())
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

	// Unlike every other log.Fatalf above: a damaged anchor_watch.json must
	// fail only the anchor watch, not the whole backend — every other alarm
	// this process runs would go down with it otherwise. The anchor watch
	// alone goes into an explicit error state instead (recordAnchorWatchLoadFailure):
	// GET /api/anchor-watch reports it, a warning is raised through the usual
	// alarm/notification path, and the file is left untouched. Dropping a new
	// anchor or an explicit Raise are the operator's own ways to recover.
	if err := loadAnchorWatch(); err != nil {
		log.Printf("ERROR: anchor watch state unreadable, the anchor watch alone is disabled until a new drop or an explicit Raise: %v", err)
		recordAnchorWatchLoadFailure(err)
	}
	if err := startRestoredAnchorTrail(); err != nil {
		log.Fatalf("failed to start the anchor swing trail for the restored watch: %v", err)
	}
	loadAnchorPlacemarks()
	loadDashboardPages()
	// The built-in profile catalogue is compiled in; an invalid one is a
	// build defect, so stop here rather than serve a broken catalogue.
	if _, err := profileCatalogue(); err != nil {
		log.Fatalf("startup: %v", err)
	}
	ps, err := newProfileStore(globalDocumentStore)
	if err != nil {
		log.Fatalf("profile store: %v", err)
	}
	if err := checkForLegacyProfileFiles(ps, legacyEngineProfilesDir()); err != nil {
		log.Fatalf("startup: %v", err)
	}
	globalProfileStore = ps
	loadEngineProfiles()
	// One-off: the old vessel: settings block, tank labels and hour meter
	// paths become equipment items that own their SignalK source plus the
	// roles block (ADR 0187). Fail fast: a half-converted vessel would read
	// as an unset one. Must run after loadAlarmRules, which it edits.
	if err := convertLegacyVessel(getEnv("SETTINGS_FILE", "../settings.yaml"), globalDocumentStore); err != nil {
		log.Fatalf("failed to convert the vessel settings: %v", err)
	}
	// One-off: dashboard tiles that held full SignalK paths become references
	// to the equipment items that own them (ADR 0187, phase 2). Must run after
	// the vessel conversion (which gives items their sources) and after the
	// profiles load (which receive the zones a tile held).
	if err := convertDashboardTilesToEquipment(); err != nil {
		log.Fatalf("failed to convert the dashboard tiles: %v", err)
	}
	// Offers the anomaly-detection set (sensor health always, the rest as
	// engines and a house bank are set up): unlike the sets above, this
	// one is several independent markers rather than one, seeded again on
	// every vessel-settings save so a detector configured after this boot
	// still gets its rules. Must run after loadEngineProfiles just above --
	// the battery sub-set's readiness check resolves the house bank's linked
	// profile through engineProfilesState, which is empty until that call
	// runs; seeding first meant the full-bank-charging rules could never be
	// seeded at boot no matter how complete the vessel setup was (code
	// review finding 1). A failure here is not fatal, for the same reason
	// as the sets above.
	if vessel, err := loadVesselView(getEnv("SETTINGS_FILE", "../settings.yaml")); err != nil {
		log.Printf("could not load the vessel for anomaly rule seeding: %v", err)
	} else {
		if err := syncRoleBoundRules(vessel); err != nil {
			log.Printf("could not move the house bank alarm rules: %v", err)
		}
		if err := seedAnomalyRules(vessel); err != nil {
			log.Printf("could not seed the anomaly alarm rules: %v", err)
		}
	}
	// Radar (ADR 0062, amended for the mayara SignalK plugin): a target store
	// fed by radarPoller (radar_source.go), which polls the plugin's proxied
	// REST endpoint through the already-configured SignalK connection —
	// there is no separate mayara host/port, and no push path through
	// SignalK to subscribe to instead for targets. Initialised
	// unconditionally, same as the SignalK snapshot below — an absent or
	// unreachable plugin is a live "disabled"/"mayara-unreachable" source in
	// the telemetry payload, not an absent store.
	globalRadarTargetStore = newRadarTargetStore()

	// All vessel data arrives over the SignalK delta stream (ADR 0037). There
	// is no REST read path to fall back to: a dropped stream surfaces as an
	// outage rather than being papered over.
	streamCtx, cancelStream := context.WithCancel(context.Background())
	defer cancelStream()
	// Units each SignalK path last declared, loaded before the stream feeds
	// the snapshot (ADR 0193). Fail fast: without it alarms lose their units
	// whenever a device is off.
	unitStore, err := newSignalKUnitStore(globalDocumentStore)
	if err != nil {
		log.Fatalf("signalk unit store: %v", err)
	}
	if err := globalSignalKSnapshot.useUnitStore(unitStore); err != nil {
		log.Fatalf("signalk unit store: %v", err)
	}
	// Writes the declared units in batches, off the stream goroutine. Pending
	// units reach disk within about 2 s. main has no signal handling, so
	// streamCtx never ends in production and no final flush runs: a restart
	// inside that window loses them until the device declares them again,
	// which SignalK does on every subscribe.
	go globalSignalKSnapshot.runUnitFlusher(streamCtx)
	// Helmcentral's own record of which equipment sources the boat has
	// reported (ADR 0196), so an engine that is switched off is still listed.
	// InfluxDB is only an optional one-time seed for it, never read per request.
	sourceStore, err := newSourceRegistryStore(globalDocumentStore)
	if err != nil {
		log.Fatalf("source registry: %v", err)
	}
	globalSourceRegistry = newSourceRegistry(sourceStore, influxTelemetryConfigured, queryInfluxSourceHistory)
	go globalSourceRegistry.run(streamCtx, globalSignalKSnapshot)
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
	go startSourceDeviceRefresher(streamCtx, sourceDeviceRefreshInterval)
	go startAnomalyDetector(streamCtx, anomalyDetectorInterval)
	go startTwinBaselineRefresher(streamCtx, anomalyBaselineRefreshInterval)
	// Gust ladder + solar Influx queries (Tier 1 #1): buildVesselStatePayload
	// and buildSolarStatePayload used to run these live on every call, up to
	// several times a second per stream client. This refreshes them once on
	// a shared 30s ticker instead; the builders just read the cache.
	go startTelemetryInfluxTicker(streamCtx)
	go runAssistantToolCallPruner(streamCtx, globalAssistantStore)

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

// buildAPIRoutes composes the /api route table from the per-feature route
// functions (docs/adr/0177). Each feature declares its own rows, with the
// auth tier written on every row, next to its handlers; this function only
// concatenates them. registerAPIRoutes (auth_middleware.go) remains the one
// place a route reaches Echo, and TestAPIRouteCoverage_* keep a missing or
// duplicated route, or an unset tier, visible (docs/adr/0040).
//
// tileFetchClient is passed in because it's a plain local value in main()
// (not a package-level global like globalTileCache/
// globalNearbyContactStore, which the handlers read directly at request
// time). It backs the map routes (Esri World Imagery and the Carto
// basemap, ADR 0067): one shared upstream-fetch client.
func buildAPIRoutes(sessions *sessionStore, tileFetchClient *http.Client) []apiRoute {
	var routes []apiRoute
	routes = append(routes, healthRoutes()...)
	routes = append(routes, authRoutes(sessions)...)
	routes = append(routes, displayProbeRoutes()...)
	routes = append(routes, vesselStateRoutes()...)
	routes = append(routes, setupRoutes()...)
	routes = append(routes, electricalRoutes()...)
	routes = append(routes, solarRoutes()...)
	routes = append(routes, tanksRoutes()...)
	routes = append(routes, telemetryStreamRoutes()...)
	routes = append(routes, telemetryHistoryRoutes()...)
	routes = append(routes, telemetrySeriesRoutes()...)
	routes = append(routes, signalkRoutes()...)
	routes = append(routes, settingsRoutes()...)
	routes = append(routes, secretsRoutes()...)
	routes = append(routes, pluginRoutes()...)
	routes = append(routes, alarmRoutes()...)
	routes = append(routes, alarmTransportRoutes()...)
	routes = append(routes, nearbyVesselRoutes()...)
	routes = append(routes, radarRoutes()...)
	routes = append(routes, placeNameRoutes()...)
	routes = append(routes, forecastProviderRoutes()...)
	routes = append(routes, anchorRoutes()...)
	routes = append(routes, dashboardRoutes()...)
	routes = append(routes, displayRoutes()...)
	routes = append(routes, activeRouteRoutes()...)
	routes = append(routes, trackRoutes()...)
	routes = append(routes, passageRoutes()...)
	routes = append(routes, profileRoutes()...)
	routes = append(routes, czoneRoutes()...)
	routes = append(routes, generatorRoutes()...)
	routes = append(routes, autopilotRoutes()...)
	routes = append(routes, mapRoutes(tileFetchClient)...)
	routes = append(routes, assistantRoutes()...)
	routes = append(routes, helpRoutes()...)
	routes = append(routes, logRoutes()...)
	routes = append(routes, documentRoutes()...)
	routes = append(routes, noteRoutes()...)
	routes = append(routes, checklistRunRoutes()...)
	routes = append(routes, manualRoutes()...)
	routes = append(routes, inventoryRoutes()...)
	routes = append(routes, deckRoutes()...)
	routes = append(routes, vesselParticularsRoutes()...)
	routes = append(routes, maintenanceRoutes()...)
	routes = append(routes, importRoutes()...)
	return routes
}

func getEnv(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
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
