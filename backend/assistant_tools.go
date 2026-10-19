package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"
)

// This file holds the tool registry (mateTools, ADR 0172), dispatch, and the
// helpers several tools share. The first tools' records and handlers
// (ADR 0093) live in assistant_tools_places.go, _forecast.go and _library.go.
// Every dependency is injected via
// assistantToolDeps so tests never need a live SignalK connection, a
// registered WASM plugin, or a real place-names provider round trip - the same
// injectable-func idiom forecastWarningsFetcher (forecast_warnings_fetcher.go)
// already uses.

// assistantMaxForecastDays bounds get_wind_forecast.days. Ten days matches
// weatherForecast's own cap (weather_providers.go).
const assistantMaxForecastDays = 10

// assistantMaxTideDays bounds get_tides.days. Eight days is what the BOM
// tide provider actually predicts ahead (see tideToday's docs and the
// get_tides note below) - asking for more would silently return a shorter
// window than requested.
const assistantMaxTideDays = 8

// assistantMaxToolResultChars caps one tool result's JSON encoding. This is
// a chat-context budget, not a correctness limit - a huge tool result eats
// into the model's window and cost for no benefit, since a skipper's
// question never needs more than a handful of days of hourly detail. See
// capToolResultJSON.
const assistantMaxToolResultChars = 12000

// assistantFindPlacesMaxQueryRunes bounds find_places.query before it
// reaches the configured place-names provider (osm-overpass or Google
// Places, per settings) and, from there, a live network request to
// whatever host that provider is configured against - the only outbound
// path for attacker-chosen bytes besides openrouter.ai itself (M-4
// finding). A real feature name, including the "Name, Qualifier" two-part
// form the prompt's rung-2 fallback uses (e.g. "Bona Bay, Gloucester
// Island"), never needs anywhere near this many characters; a query this
// long is far more likely to be an attempt to smuggle something else -
// vessel position, document contents - into the mirror operator's logs
// than a genuine search.
const assistantFindPlacesMaxQueryRunes = 80

// assistantFindPlacesQueryPattern is the character allowlist find_places.
// query must match. It covers what a real place name plausibly contains:
// letters in any script (a great many charted names are not ASCII),
// digits, spaces, and the handful of punctuation marks that actually turn
// up in real names or the "Name, Qualifier" query form - apostrophe,
// hyphen, period, comma, ampersand, and parentheses. Anything else
// (control characters, JSON/XML metacharacters, backslashes, quotes) is
// rejected outright rather than stripped, so the model is told plainly
// instead of having its query silently mangled (AGENTS.md's fallback
// policy).
var assistantFindPlacesQueryPattern = regexp.MustCompile(`^[\p{L}\p{N} .,'&()-]+$`)

// assistantToolDeps are every real-world dependency the tools need,
// injected so tests supply fakes/stubs instead of a live SignalK, WASM
// plugin registry, place-names provider or InfluxDB connection.
type assistantToolDeps struct {
	now         func() time.Time
	vesselState func() (vesselStateData, error)
	// pathHasData reports whether a SignalK data path (no notifications.
	// prefix) currently has a value in the live self tree. get_active_alarms
	// uses it so a bus alarm only names a path the history tools can read.
	// nil means no path is known to have data.
	pathHasData func(path string) bool
	weather     func() (weatherProvider, string, error)
	waves       func() (waveProvider, string, error)
	tides       func() (tideProvider, string, error)
	// placeNames resolves the currently configured place-names provider
	// (ui.place_name_provider) for find_places, the same
	// placeNameProviderResolver shape place_name.go uses - read fresh on
	// every call so a Settings change takes effect on the very next lookup.
	placeNames placeNameProviderResolver
	routes     func() []routeData
	// influxRange is estimate_passage's only I/O dependency: any SignalK
	// path's history over an explicit range, aggregated to fixed-size
	// buckets. Production wires queryInfluxPathRange (influx.go); tests
	// supply a canned series with no InfluxDB connection at all.
	influxRange func(path string, start, stop time.Time, every string) ([]telemetryPoint, error)
	// fuelRateInstances names the propulsion instances the operator chose
	// in Vessel settings, so estimate_passage never guesses engine names.
	// nil when none is configured - executeEstimatePassage then says
	// engines are not set up.
	fuelRateInstances func() ([]string, error)
	// fuelAboardM3 is the vessel's current fuel volume (helmcentral.fuel.volume,
	// ADR 0084 - the same derived path the Tanks tile reads), for
	// estimate_passage's fuel margin. ok is false whenever the figure is
	// not currently defined (no tank reporting both a level and a
	// capacity, or the input too stale to publish - see
	// freshEnoughToPublish), which estimate_passage reports as "unknown"
	// rather than a fabricated zero.
	fuelAboardM3 func() (float64, bool)
	// help names the embedded help pages read_help serves
	// (assistant_help.go). Production returns globalHelp, loaded once
	// at startup; tests inject a fixed slice with no embedding involved.
	help func() []helpPage
	// documents is the boat's document library (ADR 0106), read fresh on
	// every call for search_documents and read_document - production
	// returns globalDocumentStore; tests inject a t.TempDir()-backed
	// *documentStore with no upload/indexer pipeline involved. nil (the
	// zero value in a test that never sets it) is a real possibility, not
	// just a test artefact: this store is optional the same way help
	// is, so both tools report a plain error rather than panicking when
	// it's unset.
	documents func() *documentStore
	// documentSearchReadiness is search_documents' seam into
	// hybridDocumentSearch's semantic side (E1d, documents_hybrid.go) -
	// checkAssistantReadiness's own signature, mirroring documentIndexer's
	// readiness field. nil (a test that never sets it, same as every other
	// field here) means semantic search is simply unavailable to this call:
	// hybridDocumentSearch treats that exactly like a Problem, not an error,
	// so every existing search_documents test that predates E1d keeps
	// working unchanged, keyword-only.
	documentSearchReadiness func() (assistantReadiness, string, error)
	// nearbyVessels resolves the live AIS list around the vessel's own
	// position, capped at limit and sorted by range - production wires
	// fetchSignalKNearbyVesselsLimit (signalk.go, ADR 0128), reading fresh
	// on every call the same way vesselState does, so a target that just
	// came into range reaches get_nearby_vessels on the very next question.
	nearbyVessels func(selfLat, selfLon float64, now time.Time, limit int) ([]nearbyVessel, error)
	// contacts is get_nearby_vessels' own seam into the sighting log
	// (globalNearbyContactStore, nearby_contacts.go) for in_range_since and
	// past_sightings - nil (a test that never sets it, same as documents
	// and help above) is a real possibility, not just a test artefact: the
	// store might not be initialised yet, and get_nearby_vessels treats
	// that as "no sighting history available" rather than an error.
	contacts func() *nearbyContactStore
	// webSearch backs search_web (ADR 0159). nil unless the operator has
	// switched web search on, in which case the tool is not offered either;
	// a call that arrives anyway is refused rather than guessed at.
	webSearch func(ctx context.Context, query string) ([]assistantWebResult, error)
	// searchUsage collects the web search sub-requests' usage for the run's
	// cost (withWebSearch). nil when web search is not wired.
	searchUsage *assistantUsageSink
	// signalKPositionHistory is get_nearby_vessels' seam into the SignalK
	// History API (fetchSignalKPositionHistory, signalk.go, ADR 0128) for a
	// vessel's own logged dwell at its current position - a separate,
	// usually more precise figure than the sighting log's in_range_since,
	// that lights up once the boat's InfluxDB writer records other vessels
	// (today it records self only).
	signalKPositionHistory func(mmsi string, from, to time.Time, resolutionSeconds int) ([]signalKHistoryPoint, error)
	// signalKDiagnostics is check_signalk_paths' whole view of the live
	// SignalK snapshot (Mate diagnostics, ADR 0131): connection status, last
	// message time, and every leaf currently in the self tree with its
	// value/source/declared timestamp. Production wires
	// signalKDiagnosticsFromGlobalSnapshot (assistant_diagnostics.go); tests
	// inject a fixed snapshot with no live SignalK connection at all.
	signalKDiagnostics func(now time.Time) signalKDiagnosticsSnapshot
	// activeAlarms is get_active_alarms' only dependency: the same list the
	// alarm card and banner show (ADR 0175). Production wires activeAlarms;
	// nil is an error, never an empty list.
	activeAlarms func() []alarmStatus
	// influxLastRecorded is get_last_recorded's only I/O dependency: the most
	// recent recorded point for every measurement/source pair matching a
	// path-prefix and/or source filter, within a lookback window. Production
	// wires queryInfluxLastRecorded (influx.go); tests supply a canned slice
	// with no InfluxDB connection at all.
	influxLastRecorded func(pathPrefix, source string, lookbackDays int) ([]influxLastRecordedRow, error)
	// influxPathHistoryStat is get_path_history's I/O dependency for one
	// aggregate statistic (min/mean/max) of a path's history over an
	// explicit range, aggregated to fixed-size buckets, optionally scoped to
	// one source. Production wires queryInfluxPathStatRange (influx.go). It
	// takes ctx - executeGetPathHistory's own tool ctx - rather than building
	// its own context.Background() timeout internally, and is called
	// concurrently (min/mean/max, alongside influxPathHistoryFirstLast
	// below) so cancelling that ctx stops every one of them and the worst
	// case is one shared timeout rather than the sum of four sequential ones
	// (a code-review finding, 2026-09-25).
	influxPathHistoryStat func(ctx context.Context, path, source string, start, stop time.Time, every, aggFn string) ([]telemetryPoint, error)
	// influxPathHistoryFirstLast is get_path_history's other I/O dependency:
	// the path's actual first/last recorded sample time over the same range,
	// as opposed to influxPathHistoryStat's bucket-START-labelled series -
	// see queryInfluxPathFirstLast's own doc comment (influx.go) for why the
	// two are not the same timestamp. Production wires queryInfluxPathFirstLast.
	influxPathHistoryFirstLast func(ctx context.Context, path, source string, start, stop time.Time) (first, last time.Time, found bool, err error)
	// today is the operator's own local calendar date (YYYY-MM-DD at UTC
	// midnight), taken from the message POST's required today field
	// (postAssistantMessageHandler) - never the server's clock. The
	// maintenance tools compute every rule status against it (ADR 0145). The
	// zero value means "not supplied", and list_maintenance then fails
	// rather than guess.
	today time.Time
	// watches is start_watch's registry (ADR 0160). Production wires
	// globalAssistantWatches; nil (a test that never sets it) makes the tool
	// fail plainly rather than start nothing.
	watches *assistantWatchRegistry
	// conversations is the chat history store search_conversations and
	// read_conversation read (ADR 0161). Production returns
	// globalAssistantStore; nil, or a nil store, makes both tools fail
	// plainly.
	conversations func() *assistantStore
	// passageLimits reads the operator's go/no-go thresholds
	// (assistant.passage_limits) fresh on every call, for
	// assess_passage_window. nil is an error, never empty limits.
	passageLimits func() (passageLimits, error)
	// cards is the turn's side channel for card payloads too large for the
	// model's tool result. nil means the tools put no card out of band.
	cards *assistantCardSink
}

// assistantCardSink carries a card from the tool that built it to the run,
// beside the capped text the model reads. A tool puts the card under the exact
// result text it returned; the run takes it back out under that same text once
// the round is done. Results that are equal text hold equal cards, so a
// duplicated call hands out one card per call and none is lost or shared.
type assistantCardSink struct {
	mu       sync.Mutex
	byResult map[string][]mateCard
}

func newAssistantCardSink() *assistantCardSink {
	return &assistantCardSink{byResult: map[string][]mateCard{}}
}

func (s *assistantCardSink) put(result string, c mateCard) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byResult[result] = append(s.byResult[result], c)
}

func (s *assistantCardSink) take(result string) []mateCard {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := s.byResult[result]
	if len(q) == 0 {
		return nil
	}
	if len(q) == 1 {
		delete(s.byResult, result)
	} else {
		s.byResult[result] = q[1:]
	}
	return q[:1]
}

// takeCards makes assistantToolDeps an assistantCardSource.
func (d assistantToolDeps) takeCards(result string) []mateCard {
	if d.cards == nil {
		return nil
	}
	return d.cards.take(result)
}

// assistantProductionToolDeps wires the real dependencies: the live vessel
// state, the settings-configured weather/wave/tide/place-names providers,
// and a snapshot of the saved routes.
func assistantProductionToolDeps(settingsPath string) assistantToolDeps {
	return assistantToolDeps{
		now:         time.Now,
		vesselState: fetchSignalKVesselState,
		pathHasData: func(path string) bool {
			return globalSignalKSnapshot.liveSelfTelemetry(time.Now().UTC()).Sample(path).Present
		},
		weather: func() (weatherProvider, string, error) {
			return resolveWeatherProvider(settingsPath)
		},
		waves: func() (waveProvider, string, error) {
			return resolveWaveProvider(settingsPath)
		},
		tides: func() (tideProvider, string, error) {
			return resolveTideProvider(settingsPath)
		},
		placeNames: func() (placeNameProvider, string, error) {
			return resolvePlaceNameProvider(settingsPath)
		},
		routes:            assistantRouteSnapshot,
		influxRange:       queryInfluxPathRange,
		fuelRateInstances: configuredEngineInstances(settingsPath),
		fuelAboardM3:      fuelAboardM3FromDerivedPaths,
		help:              func() []helpPage { return globalHelp },
		documents:         func() *documentStore { return globalDocumentStore },
		documentSearchReadiness: func() (assistantReadiness, string, error) {
			return checkAssistantReadiness(settingsPath)
		},
		nearbyVessels: func(selfLat, selfLon float64, now time.Time, limit int) ([]nearbyVessel, error) {
			excluded := []string{fetchSignalKSelfName()}
			return fetchSignalKNearbyVesselsLimit(selfLat, selfLon, now, excluded, limit)
		},
		contacts: func() *nearbyContactStore { return globalNearbyContactStore },
		signalKPositionHistory: func(mmsi string, from, to time.Time, resolutionSeconds int) ([]signalKHistoryPoint, error) {
			return fetchSignalKPositionHistory(settingsPath, mmsi, from, to, resolutionSeconds)
		},
		signalKDiagnostics:         signalKDiagnosticsFromGlobalSnapshot,
		activeAlarms:               activeAlarms,
		influxLastRecorded:         queryInfluxLastRecorded,
		influxPathHistoryStat:      queryInfluxPathStatRange,
		influxPathHistoryFirstLast: queryInfluxPathFirstLast,
		watches:                    globalAssistantWatches,
		conversations:              func() *assistantStore { return globalAssistantStore },
		passageLimits:              passageLimitsFromSettingsFile(settingsPath),
		cards:                      newAssistantCardSink(),
	}
}

// fuelAboardM3FromDerivedPaths reads helmcentral.fuel.volume (ADR 0084) from
// derivedPathValues, the same map-of-latest-values signalk_paths.go already
// reads from to answer the gauge-values stream. ok is false when the value
// is nil - not currently defined, never a fabricated zero (AGENTS.md's
// fallback policy).
func fuelAboardM3FromDerivedPaths() (float64, bool) {
	value := derivedPathValues()[fuelVolumePath]
	if value == nil {
		return 0, false
	}
	return *value, true
}

// configuredEngineInstances returns a reader of the propulsion instances the
// operator chose in Vessel settings, in the order saved, from settingsPath.
// It returns nil, nil when none is configured, which estimate_passage reports
// as engines not being set up. An unreadable vessel block is an error, kept
// distinct from not-configured.
func configuredEngineInstances(settingsPath string) func() ([]string, error) {
	return func() ([]string, error) {
		vessel, err := loadVesselView(settingsPath)
		if err != nil {
			return nil, err
		}
		var instances []string
		for _, engine := range vessel.Engines {
			if engine.Instance != "" {
				instances = append(instances, engine.Instance)
			}
		}
		return instances, nil
	}
}

// assistantRouteSnapshot copies the saved routes under routesMu's read lock
// (routes.go) so find_places never holds that lock while it does
// place-names provider I/O or JSON work - the same "copy under the lock,
// work outside it"
// discipline resolveAndPinAnchorWatchPlaceName (place_name.go) already
// follows for anchorWatchMu.
func assistantRouteSnapshot() []routeData {
	routesMu.RLock()
	defer routesMu.RUnlock()

	out := make([]routeData, 0, len(routesState))
	for _, r := range routesState {
		out = append(out, *r)
	}
	return out
}

// ── tool definitions (OpenRouter/OpenAI function-calling schemas) ──────────

// ── the tool registry ───────────────────────────────────────────────────

// mateTool is one Mate tool as one record (ADR 0172). The definition offered
// to the model, dispatch, the status line, result collection and availability
// all derive from the records in mateTools, so adding a tool is one record
// and one handler, with no switch to remember.
type mateTool struct {
	// name is the tool's one spelling. The registry stamps it into the
	// definition's function name, so function.Name stays empty.
	name     string
	function openRouterFunctionDef
	execute  func(d assistantToolDeps, ctx context.Context, args json.RawMessage) (string, error)
	// describe renders the one-line status shown while the call runs.
	describe func(args json.RawMessage) string
	// collect, when set, lifts something out of the tool's result into the
	// run's reply after the round (propose_changes, fill_form). A result it
	// cannot read is an error, never skipped.
	collect func(result string, reply *assistantReply) error
	// available, when set, says whether the tool is on offer for these deps.
	// Unavailable means absent from the request and refused by execute (web
	// search off, ADR 0159). Nil means always available.
	available func(d assistantToolDeps) bool
}

// mateTools is every Mate tool, in the order the model is offered them.
// search_web stays last: it is the only conditional tool.
var mateTools = []mateTool{
	mateToolFindPlaces,
	mateToolGetWindForecast,
	mateToolGetTides,
	mateToolEstimatePassage,
	mateToolPlanTidalDeparture,
	mateToolAssessPassageWindow,
	mateToolReadHelp,
	mateToolSearchDocuments,
	mateToolReadDocument,
	mateToolSearchConversations,
	mateToolReadConversation,
	mateToolGetNearbyVessels,
	mateToolGetActiveAlarms,
	mateToolCheckSignalKPaths,
	mateToolGetLastRecorded,
	mateToolGetPathHistory,
	mateToolListPassages,
	mateToolFindEquipment,
	mateToolListMaintenance,
	mateToolGetMaintenanceLog,
	mateToolDescribeRecordType,
	mateToolListRecords,
	mateToolGetRecord,
	mateToolProposeChanges,
	mateToolStartWatch,
	mateToolGetVesselParticulars,
	mateToolInspectForm,
	mateToolFillForm,
	mateToolSearchWeb,
}

func lookupMateTool(name string) *mateTool {
	for i := range mateTools {
		if mateTools[i].name == name {
			return &mateTools[i]
		}
	}
	return nil
}

func mateToolAvailable(d assistantToolDeps, name string) bool {
	t := lookupMateTool(name)
	return t != nil && t.availableTo(d)
}

// availableTo reports whether the tool is on offer for these deps.
func (t mateTool) availableTo(d assistantToolDeps) bool {
	return t.available == nil || t.available(d)
}

// assistantToolDefinitions is the tool list offered to the model: the records
// available to d, rendered as OpenRouter function-calling schemas. Web search
// off means search_web is not in the request at all (ADR 0159).
func assistantToolDefinitions(d assistantToolDeps) []openRouterTool {
	tools := make([]openRouterTool, 0, len(mateTools))
	for _, t := range mateTools {
		if !t.availableTo(d) {
			continue
		}
		fn := t.function
		fn.Name = t.name
		tools = append(tools, openRouterTool{Type: "function", Function: fn})
	}
	return tools
}

func (d assistantToolDeps) definitions() []openRouterTool { return assistantToolDefinitions(d) }

// ── dispatch ────────────────────────────────────────────────────────────

// execute runs one tool call by name and returns its result as compact
// JSON, capped at assistantMaxToolResultChars (see capToolResultJSON). ctx
// is the tool round's context: assistant_run.go's runToolRound now runs one
// round's calls concurrently, one goroutine per call, all sharing this same
// ctx, so a cancelled run (the operator closes the tab, or the run's own
// timeout fires) is visible to every executeXxx below before it starts, and
// between its outbound calls where it makes more than one. None of the
// place-names provider (placeNameProvider.SearchPlaces), InfluxDB
// (queryInfluxPathRange) or the weather/wave/tide provider interfaces
// (weatherProvider.FetchForecast and friends) take a context today, so a
// request already in flight when ctx
// is cancelled still runs to completion - threading a context through those
// layers is out of scope here (backend performance audit, Tier 3).
func (d assistantToolDeps) execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	t := lookupMateTool(name)
	if t == nil {
		return "", fmt.Errorf("unknown tool %q", name)
	}
	if !t.availableTo(d) {
		return "", fmt.Errorf("%s is not available: it is switched off in Settings", name)
	}
	return t.execute(d, ctx, args)
}

// describeAssistantToolCall renders the one-line status the SSE stream
// shows while a tool call is in flight (assistant_run.go), so the operator
// sees "Looking up Tongue Bay…" rather than a bare tool name.
func describeAssistantToolCall(name string, args json.RawMessage) string {
	if t := lookupMateTool(name); t != nil {
		return t.describe(args)
	}
	return fmt.Sprintf("Running %s…", name)
}

// assistantLocationArgs is the {lat, lon, name} shape shared by
// get_wind_forecast and get_tides' arguments.
type assistantLocationArgs struct {
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	Days int     `json:"days"`
	Name string  `json:"name"`
}

// assistantWindForecastArgs is get_wind_forecast's own argument shape:
// assistantLocationArgs plus an optional course_deg, the vessel's intended
// course over ground. get_tides has no use for a course, so it keeps using
// bare assistantLocationArgs; this extra field is exclusive to
// get_wind_forecast. CourseDeg is a pointer so "the model left it out" (no
// relative-angle fields in the result at all) is distinguishable from a
// genuine course of 0 (due north).
type assistantWindForecastArgs struct {
	assistantLocationArgs
	CourseDeg *float64 `json:"course_deg"`
}

// assistantLocationLabel names a location argument set for a status line:
// the model-supplied name when given, otherwise the raw coordinates.
func assistantLocationLabel(args json.RawMessage) string {
	var a assistantLocationArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "the requested position"
	}
	if name := strings.TrimSpace(a.Name); name != "" {
		return name
	}
	return fmt.Sprintf("%.4f,%.4f", a.Lat, a.Lon)
}

// clampAssistantDays applies a tool's days default/ceiling: <=0 becomes
// def, anything past max is capped at max. Never an error - a model that
// asks for 30 days of tides gets 8, not a failed call.
func clampAssistantDays(days, def, max int) int {
	if days <= 0 {
		return def
	}
	if days > max {
		return max
	}
	return days
}

// capToolResultJSON marshals result (a pointer to one of the assistant*
// Result structs below) to compact JSON and, if the encoding exceeds
// assistantMaxToolResultChars, repeatedly calls shrink - which trims one
// item from the least essential list on result (e.g. the oldest hourly row)
// and reports whether it removed anything - re-marshaling after each trim,
// until the result fits or shrink has nothing left to remove.
//
// This is a chat-context budget, not the kind of upstream data problem
// AGENTS.md's fallback policy is about: a tool result truncated with an
// explicit "truncated": true marker is a legible partial answer the model
// can still parse, not a fabricated one. If shrink can't get there (or
// there is nothing to shrink), a minimal, always-valid fallback object is
// returned rather than chopping raw JSON bytes at a byte boundary, which
// could hand the model unparseable text instead of a short one.
func capToolResultJSON(result any, shrink func() bool) (string, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshal tool result: %w", err)
	}

	for len(data) > assistantMaxToolResultChars {
		if shrink == nil || !shrink() {
			return `{"truncated":true,"note":"result too large to return in full even after trimming"}`, nil
		}
		data, err = json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("marshal tool result: %w", err)
		}
	}

	return string(data), nil
}

type assistantStationInfo struct {
	Name       string  `json:"name"`
	ID         string  `json:"id"`
	State      string  `json:"state,omitempty"`
	Timezone   string  `json:"timezone,omitempty"`
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
	DistanceNm float64 `json:"distance_nm"`
	BearingDeg int     `json:"bearing_deg"`
}

func roundTo2(value float64) float64 { return math.Round(value*100) / 100 }

// resolveTideStation picks the nearest station to (lat, lon) from the
// selected tide provider and fetches its chart. Every failure is returned
// with the tool's name in front; nothing is guessed when a piece is missing.
func (d assistantToolDeps) resolveTideStation(tool string, lat, lon float64) (assistantResolvedTideStation, error) {
	provider, providerID, err := d.tides()
	if err != nil {
		return assistantResolvedTideStation{}, fmt.Errorf("%s: %w", tool, err)
	}

	station, ok := nearestStation(provider, lat, lon)
	if !ok {
		return assistantResolvedTideStation{}, fmt.Errorf("%s: tide provider %q has no stations", tool, providerID)
	}

	res, err := provider.FetchTideChart(station.StationID)
	if err != nil {
		return assistantResolvedTideStation{}, fmt.Errorf("%s: tide provider %q: %w", tool, providerID, err)
	}

	// The station's own timezone is preferred (it's the zone tide times are
	// actually meaningful in); vesselLocalLocation's longitude-derived fixed
	// offset is the fallback for a station whose catalog entry carries no
	// timezone or names one this binary's embedded zoneinfo (backend/tzdata.go)
	// doesn't recognise.
	var loc *time.Location
	var timeBasis string
	if tz := strings.TrimSpace(station.Timezone); tz != "" {
		if l, lerr := time.LoadLocation(tz); lerr == nil {
			loc, timeBasis = l, tz
		}
	}
	if loc == nil {
		loc = vesselLocalLocation(station.Lon)
		timeBasis = loc.String()
	}

	return assistantResolvedTideStation{
		providerID: providerID,
		chart:      res,
		loc:        loc,
		timeBasis:  timeBasis,
		info: assistantStationInfo{
			Name:       station.Name,
			ID:         station.StationID,
			State:      station.State,
			Timezone:   station.Timezone,
			Lat:        station.Lat,
			Lon:        station.Lon,
			DistanceNm: roundTo1(haversineMeters(lat, lon, station.Lat, station.Lon) / metersPerNauticalMile),
			BearingDeg: int(math.Round(bearingDeg(lat, lon, station.Lat, station.Lon))),
		},
	}, nil
}

// assistantCitationTitle returns doc/hit's citable display name for the
// structured citation contract (Mate UI cycle: document sources as icons) -
// the system prompt tells Mate to cite a document it found via
// search_documents or read_document as a markdown link whose visible text is
// exactly this string, e.g. `[Yanmar 4JH Service Manual](/documents?document=<id>)`.
// Most uploaded documents never get an operator-set title, so this never
// hands back a blank string - the same title-or-filename fallback
// documentDisplayName (frontend/src/lib/document-display.ts) already applies
// on the Documents panel for the same reason: a blank title would render as
// an empty link.
func assistantCitationTitle(title, filename string) string {
	if t := strings.TrimSpace(title); t != "" {
		return t
	}
	return filename
}

// documentStore resolves d.documents into a ready *documentStore, or the
// same plain "not available" error search_documents and read_document both
// report - whichever of two ways the document library can be unset: d.documents
// itself is nil (assistantToolDeps' own zero value, a real possibility per
// its doc comment - calling a nil func panics, so this is checked first) or
// it returns a nil store (production's own globalDocumentStore, unset when
// no document library has ever been initialised).
func (d assistantToolDeps) documentStore(toolName string) (*documentStore, error) {
	if d.documents == nil {
		return nil, fmt.Errorf("%s: the document library is not available", toolName)
	}
	store := d.documents()
	if store == nil {
		return nil, fmt.Errorf("%s: the document library is not available", toolName)
	}
	return store, nil
}
