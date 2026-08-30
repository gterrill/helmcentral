package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/labstack/echo/v4"
)

// mayaraTargetsAPIPathTemplate is the mayara SignalK plugin's proxied
// endpoint for one radar's current ARPA target list, verified against the
// live plugin 2026-08-29 (backend/testdata/mayara/targets-fur6424A.json was
// captured from exactly this path). %s is the radar id radarsFromSnapshot
// reports, e.g. "fur6424A".
const mayaraTargetsAPIPathTemplate = "/signalk/v2/api/vessels/self/radars/%s/targets"

// radarPollInterval is how often radarPoller re-fetches the polled radar's
// target list. mayara's own WebSocket broadcasts in 60-second bursts
// (measured, backend/testdata/mayara/target-deltas-timed.jsonl), so polling
// the REST endpoint every 2s reads the tracker's live state directly, which
// is fresher than the push stream it replaces (ADR 0062 amendment).
const radarPollInterval = 2 * time.Second

// radarInfo is one radar the mayara SignalK plugin has published, in the
// shape the telemetry payload needs. ID is the mayara radar key
// ("fur6424A"), not a field the plugin publishes itself — dual range means
// one physical radar presents two of these, and both are reported, never
// deduplicated (ADR 0062).
type radarInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Transmitting is mayara's power control reading 2 (Transmit). When it is
	// false the radar is off, in standby, preparing or faulted, and any target
	// mayara is still serving is a Kalman extrapolation rather than an
	// observation. Carried to the wire so the tile can say STANDBY instead of
	// letting an empty target list imply clear water.
	//
	// The control re-sends about every thirty seconds and on subscribe, so
	// after a restart there is a window where the power state is simply not
	// known yet and this reads false. Serving no targets until we know is the
	// right way round: the alternative is plotting extrapolated contacts from
	// a radar that may well be switched off.
	Transmitting bool `json:"transmitting"`
	// clockNow is mayara's own current time for this radar, taken as the
	// freshest control timestamp it has published. Unexported, so it never
	// reaches the payload: it exists only so a target's lastSeen can be
	// compared against mayara's clock rather than ours.
	clockNow time.Time
}

// mayaraTransmitPowerValue is the power control's "Transmit" enum member.
// Captured from the live DRS4D-NXT's capabilities: 0 Off, 1 Standby,
// 2 Transmit, 3 Preparing, 4 Fault.
const mayaraTransmitPowerValue = 2

// radarObservationMaxAge is how far a target's lastSeen may trail the radar's
// own clock before we stop believing it.
//
// Both sides of that comparison are mayara's timestamps, so its clock skew
// against ours is irrelevant. ADR 0062 decision 5 forbids ageing against
// lastSeen in absolute terms, on a Windows box we do not synchronise, and
// this does not do that: a difference between two of mayara's own readings is
// sound however far its clock has drifted from ours.
//
// Sixty seconds is comfortably more than a few missed antenna rotations and
// far less than the half hour of extrapolated contacts measured on
// 2026-08-28 (testdata/mayara/targets-standby-stale.json).
const radarObservationMaxAge = 60 * time.Second

// radarsFromSnapshot reports the radars the mayara SignalK plugin has
// published, or nil when the plugin is absent. Presence is the availability
// signal: no "radars" key in the self tree means no radar support, the same
// capability-detection shape autopilot.go uses for
// steering.autopilot.availableActions rather than a configured flag
// (buildAutopilotPayload, backend/autopilot.go).
//
// This walks the delta-reassembled snapshot rather than calling the
// plugin's own GET .../radars: that endpoint answers with a bare array
// carrying no radar ids at all (measured 2026-08-29), while the ids exist
// only as keys in the tree — vessels.self.radars.<id>.controls.* — which
// Helmcentral already receives for free on its existing subscription
// (context vessels.*, path *).
// radarPresenceMaxAge is how long a radar keeps counting as present after
// its last control delta.
//
// The SignalK tree never evicts, so a radar published once stays in it
// forever. Measured 2026-08-31 with the ship's computer off overnight: the
// tree still listed both radars with controls 17.9 hours old and power frozen
// at Transmit, while mayara's own radar list was empty. Reading presence
// straight off the tree therefore reports radars that no longer exist and,
// worse, feeds an eighteen-hour-old power value to the standby gate.
//
// Controls re-publish roughly every thirty seconds while mayara holds the
// radar, so five minutes is many missed rounds rather than a near miss.
// Judged on our own receive clock via snapshot.lastSeen, never on the
// timestamps inside the control values, which are mayara's (ADR 0062
// decision 5). Same discipline as aisTargetPositionFresh (signalk.go).
const radarPresenceMaxAge = 5 * time.Minute

func radarsFromSnapshot(snapshot *signalKSnapshot, now time.Time) []radarInfo {
	tree := snapshot.selfTree()
	if tree == nil {
		return nil
	}
	selfCtx := snapshot.selfContext()

	radarsTree, ok := tree["radars"].(map[string]any)
	if !ok || len(radarsTree) == 0 {
		return nil
	}

	ids := make([]string, 0, len(radarsTree))
	for id := range radarsTree {
		ids = append(ids, id)
	}
	// Map iteration order is random; a stable order keeps the payload from
	// reshuffling between polls for no reason, and makes "poll only the
	// first radar" (radarPoller.pollOnce) deterministic rather than a coin
	// flip between fur6424A and fur6424B.
	sort.Strings(ids)

	radars := make([]radarInfo, 0, len(ids))
	for _, id := range ids {
		// Two "value" hops, not one. The plugin's delta value for a control is
		// itself an object, {"value": ..., "timestamp": ...}, and applyDelta
		// stores that object under its own "value" key. mayara's REST shape is
		// flat by comparison, so a fixture taken from REST, or hand-built with
		// a bare scalar, reads one level too shallow and silently yields "".
		// Pinned by TestRadarsFromSnapshotReadsTheCapturedControlDeltaShape.
		name := firstNonEmptyString(
			lookupString(radarsTree, id, "controls", "userName", "value", "value"),
			lookupString(radarsTree, id, "controls", "modelName", "value", "value"),
		)
		controls := lookupAnyMap(radarsTree, id, "controls")

		// Presence is a freshness question, not a lookup.
		seen := newestControlReceipt(snapshot, selfCtx, id, controls)
		if seen.IsZero() || now.Sub(seen) > radarPresenceMaxAge {
			continue
		}

		radars = append(radars, radarInfo{
			ID:           id,
			Name:         name,
			Transmitting: lookupNumber(radarsTree, id, "controls", "power", "value", "value") == mayaraTransmitPowerValue,
			clockNow:     freshestControlTimestamp(controls),
		})
	}
	return radars
}

// radarPoller keeps a radarTargetStore fed by polling the mayara SignalK
// plugin's proxied REST endpoint for one radar's current target list.
//
// This replaces radarStreamClient's WebSocket connection to mayara directly
// (ADR 0062 amendment): the plugin proxies mayara's REST surface but does
// not publish ARPA targets as deltas, so there is no push path through
// SignalK to subscribe to. This is not the REST fallback ADR 0037 warns
// against — that decision is about vessel data that already arrives on the
// delta stream, where a REST read path would mask a broken stream as a
// working one. No delta carries radar targets at all, so REST here is the
// only path, not a fallback masking one.
type radarPoller struct {
	store        *radarTargetStore
	settingsPath string
	now          func() time.Time

	// Injectable so tests need neither a live SignalK server nor settings on
	// disk, the same seam radarStreamClient used.
	radars       func() []radarInfo
	fetchTargets func(radarID string) ([]mayaraArpaTarget, error)

	// pollFailing tracks whether the last poll failed, so a sustained outage
	// is reported on its edges rather than on every tick.
	pollFailing bool
	ownShipFix  func() (lat, lon float64, ok bool)
}

// newRadarPoller wires real defaults: radarsFromSnapshot against the live
// SignalK snapshot, fetchTargets through the already-configured SignalK
// connection (fetchMayaraTargets below), and an own-ship fix read the same
// way buildNearbyVesselsPayload does (backend/main.go).
func newRadarPoller(store *radarTargetStore, settingsPath string) *radarPoller {
	return &radarPoller{
		store:        store,
		settingsPath: settingsPath,
		now:          func() time.Time { return time.Now().UTC() },
		radars: func() []radarInfo {
			return radarsFromSnapshot(globalSignalKSnapshot, time.Now().UTC())
		},
		fetchTargets: func(radarID string) ([]mayaraArpaTarget, error) {
			return fetchMayaraTargets(settingsPath, radarID)
		},
		ownShipFix: func() (float64, float64, bool) {
			state, err := fetchSignalKVesselState()
			if err != nil {
				return 0, 0, false
			}
			return ownShipFixFromVesselState(state)
		},
	}
}

// fetchMayaraTargets GETs one radar's current target list through the
// already-configured SignalK connection (signalkRequestJSONWithAuthBody,
// route_activation.go:144). mayara has no host or port of its own in
// Helmcentral — only the plugin's proxy path, reached at the SignalK address
// already configured for everything else (ADR 0062 amendment: "no mayara
// host or port in Helmcentral"). The endpoint answered unauthenticated in
// testing, but going through the authenticated helper costs nothing and
// survives SignalK security being turned on.
func fetchMayaraTargets(settingsPath, radarID string) ([]mayaraArpaTarget, error) {
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}
	signalkURL := buildSignalKURL(address, port)
	path := fmt.Sprintf(mayaraTargetsAPIPathTemplate, radarID)

	status, body, err := signalkRequestJSONWithAuthBody(signalkURL, settingsPath, path, http.MethodGet, nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("mayara targets endpoint %s returned status %d: %s", path, status, string(body))
	}

	var targets []mayaraArpaTarget
	if err := json.Unmarshal(body, &targets); err != nil {
		return nil, fmt.Errorf("decoding mayara targets response: %w", err)
	}
	return targets, nil
}

// run blocks until ctx is cancelled, polling on a fixed ticker. Mirrors
// startAlarmEvaluator/startAnchorDragWatcher (alarm_service.go,
// alarm_anchor.go): no eager poll on start, just the ticker loop, so this
// reads the same as every other interval-driven background task in main().
func (p *radarPoller) run(ctx context.Context) {
	ticker := time.NewTicker(radarPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.pollOnce(p.now()); err != nil && p.shouldLogPollFailure() {
				log.Printf("radar poll: %v (further identical failures suppressed until it recovers)", err)
			}
		}
	}
}

// pollOnce polls the first radar radarsFromSnapshot reports and replaces its
// target set in the store wholesale. Only the first radar: dual range
// returns an IDENTICAL target list under both radar ids (verified against
// the live rig), so polling both would double the load and double-count
// with nothing gained.
//
// No radars in the snapshot is not a failure — that is the "disabled"
// state buildRadarTargetsPayload reports directly from radarsFromSnapshot,
// so pollOnce simply has nothing to do and leaves the store untouched.
//
// A failing poll marks the store disconnected (buildRadarTargetsPayload
// then reports source "mayara-unreachable") but does not purge: a blip must
// not blank the map, same reasoning the old stream client's disconnection
// handling used.
func (p *radarPoller) pollOnce(now time.Time) error {
	radars := p.radars()
	if len(radars) == 0 {
		return nil
	}

	radarID := radars[0].ID

	arpaTargets, err := p.fetchTargets(radarID)
	if err != nil {
		p.store.setConnected(false)
		return fmt.Errorf("polling radar %s: %w", radarID, err)
	}

	selfLat, selfLon, selfOK := p.ownShipFix()

	arpaTargets = observedTargets(arpaTargets, radars[0])

	// Nothing being observed is the session boundary polling otherwise
	// lacks: the radar has gone quiet, so the next contacts it acquires are
	// a fresh population and the projection cross-check's rate limit should
	// start over for them. Without this the dedup map keyed on target id
	// would grow for the life of the process, since mayara's ids only climb.
	if len(arpaTargets) == 0 {
		resetRadarProjectionMismatchLog()
	}

	targets := make([]radarTarget, 0, len(arpaTargets))
	for _, arpa := range arpaTargets {
		targets = append(targets, radarTargetFromArpa(radarID, arpa, selfLat, selfLon, selfOK, now))
	}

	p.store.replace(radarID, targets, now)
	p.store.setConnected(true)
	p.notePollRecovered()
	return nil
}

// radarTargetsPayloadCap bounds the payload to the nearest 20 targets by
// range, mirroring fetchSignalKNearbyVessels' cap of 10 (signalk.go).
// store.list(now) already sorts ascending on RangeM, so this keeps the
// nearest and drops the rest.
//
// Presentation-only. Alarm evaluation (a later phase) must read
// globalRadarTargetStore.list(now) directly and never this payload: the
// nearest contact is not always the most dangerous one, and evaluating
// against a capped list would be a silent safety hole (ADR 0062).
const radarTargetsPayloadCap = 20

// buildRadarTargetsPayload produces both the SSE "radar-targets" event body
// and the GET /api/radar/targets response — split out so the two can never
// drift apart, mirroring buildNearbyVesselsPayload.
//
// source carries three values rather than two, so a disabled integration and
// a broken one are never confused (AGENTS.md fallback policy, ADR 0062):
//   - "disabled": radarsFromSnapshot reports no radars — the mayara SignalK
//     plugin is not installed, or not yet publishing. targets stay empty
//     regardless of what the store might still hold from before.
//   - "mayara-unreachable": radars are present, but the last poll failed.
//     targets still come from the store: a target fades over
//     radarTargetMaxAge rather than vanishing the instant one poll blips.
//   - "mayara": the last poll succeeded.
func buildRadarTargetsPayload() map[string]any {
	now := time.Now().UTC()

	radars := radarsFromSnapshot(globalSignalKSnapshot, now)
	if radars == nil {
		radars = []radarInfo{}
	}

	source := "disabled"
	targets := []radarTarget{}

	if len(radars) > 0 {
		connected := false
		if globalRadarTargetStore != nil {
			connected, _ = globalRadarTargetStore.status()
			targets = globalRadarTargetStore.list(now)
		}

		if connected {
			source = "mayara"
		} else {
			source = "mayara-unreachable"
		}
	}

	if len(targets) > radarTargetsPayloadCap {
		targets = targets[:radarTargetsPayloadCap]
	}

	return map[string]any{
		"datetime": now.Format(time.RFC3339),
		"source":   source,
		"radars":   radars,
		"targets":  targets,
	}
}

// radarTargetsHandler backs GET /api/radar/targets (tierRead), so the
// payload is curl-able before any frontend UI consumes it.
func radarTargetsHandler(c echo.Context) error {
	return c.JSON(http.StatusOK, buildRadarTargetsPayload())
}

// ownShipFixFromVesselState extracts a usable own-ship position, or reports
// that there is not one.
//
// A plain range check is not enough. fetchSignalKVesselState initialises
// vesselStateData with Latitude: -1, Longitude: -1 to mean "nothing known
// yet", and -1,-1 sits inside [-90,90] and [-180,180]: it is a real point in
// the Gulf of Guinea. Range-checking alone hands that back as a fix.
//
// The AIS path survives the same sentinel by accident, because every genuine
// contact then falls outside its 5 km range gate and the list comes back
// empty. Radar projects target positions FROM own ship (radarTargetFromArpa),
// so a sentinel treated as a fix would place every derived contact off West
// Africa and plot it with full confidence. Returning no fix leaves Lat/Lon
// nil and the target unplotted, which is the honest answer.
//
// Null island is deliberately still a fix. It is improbable but it is a real
// coordinate, and the sentinel is what we actually need to exclude.
func ownShipFixFromVesselState(state vesselStateData) (lat float64, lon float64, ok bool) {
	// -1 is the "nothing known yet" sentinel the rest of vesselStateData uses
	// (Depth, HeadingTrue, SpeedOverGroundKts and the wind fields all carry
	// it), written as a bare literal here to match how every other site in
	// this package spells it.
	if state.Latitude == -1 || state.Longitude == -1 {
		return 0, 0, false
	}
	if state.Latitude < -90 || state.Latitude > 90 || state.Longitude < -180 || state.Longitude > 180 {
		return 0, 0, false
	}
	return state.Latitude, state.Longitude, true
}

// observedTargets drops everything mayara is reporting that it is not
// currently seeing.
//
// mayara keeps tracking after the antenna stops. Measured 2026-08-28 21:50Z,
// half an hour into standby, it was still serving 50 targets with five
// flagged is_dangerous and positions drifting under Kalman extrapolation.
// Ageing against our own receive time cannot catch that, because the poll
// keeps succeeding and keeps returning them: this is ADR 0057 section 6 in a
// third costume, after a plugin writing once on state change and mayara
// bursting every sixty seconds.
//
// Two rules, both free because the controls already arrive as deltas:
//
//  1. Not transmitting means nothing is being observed, so nothing survives.
//  2. Transmitting, but a contact whose lastSeen trails the radar's own clock
//     is one the radar has stopped painting.
//
// A radar with no control timestamps yet leaves clockNow zero, and rule 2 is
// skipped rather than guessed at: with no reference there is no basis to
// judge, and rule 1 still applies.
func observedTargets(targets []mayaraArpaTarget, radar radarInfo) []mayaraArpaTarget {
	if !radar.Transmitting {
		return nil
	}
	if radar.clockNow.IsZero() {
		return targets
	}

	kept := make([]mayaraArpaTarget, 0, len(targets))
	for _, target := range targets {
		seen, err := time.Parse(time.RFC3339Nano, target.LastSeen)
		if err != nil {
			// An unparseable lastSeen is not evidence of staleness. Keeping it
			// matches collisionBearingRadians' stance of refusing to
			// reinterpret a value rather than inventing one.
			kept = append(kept, target)
			continue
		}
		if radar.clockNow.Sub(seen) > radarObservationMaxAge {
			continue
		}
		kept = append(kept, target)
	}
	return kept
}

// freshestControlTimestamp reads mayara's current time off the newest control
// it has published for a radar. Controls stream continuously while mayara is
// connected to the radar, so this advances even when no target does, which is
// exactly what makes it a usable reference for target staleness.
func freshestControlTimestamp(controls map[string]any) time.Time {
	var newest time.Time
	for _, raw := range controls {
		control, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		// mayara's own timestamp travels inside the control's value object.
		// applyDelta also writes an envelope timestamp at control["timestamp"],
		// but that is the delta's, so prefer the inner one and fall back.
		stamp, ok := "", false
		if inner, innerOK := control["value"].(map[string]any); innerOK {
			stamp, ok = inner["timestamp"].(string)
		}
		if !ok {
			stamp, ok = control["timestamp"].(string)
		}
		if !ok {
			continue
		}
		parsed, err := time.Parse(time.RFC3339Nano, stamp)
		if err != nil {
			continue
		}
		if parsed.After(newest) {
			newest = parsed
		}
	}
	return newest
}

// newestControlReceipt reports when we last received any control for this
// radar, on our own clock. snapshot.lastSeen records local receive time per
// path, which is what makes this immune to mayara's clock (ADR 0062).
func newestControlReceipt(snapshot *signalKSnapshot, selfCtx, radarID string, controls map[string]any) time.Time {
	var newest time.Time
	for name := range controls {
		seen := snapshot.lastSeen(selfCtx, "radars."+radarID+".controls."+name)
		if seen.After(newest) {
			newest = seen
		}
	}
	return newest
}

// shouldLogPollFailure reports whether this failure is worth a log line: the
// first of an outage, not the five-hundredth.
//
// The fallback policy (AGENTS.md) requires retry behaviour to be loud, and
// this stays loud in the sense that matters. The transition into failure is
// reported, and so is the next one after a recovery. What is dropped is the
// identical line repeated every two seconds for as long as the radar is off,
// which was filling the live server's log while telling the operator nothing
// it had not already said.
func (p *radarPoller) shouldLogPollFailure() bool {
	if p.pollFailing {
		return false
	}
	p.pollFailing = true
	return true
}

// notePollRecovered re-arms the failure log, so a later outage is reported
// rather than swallowed by the previous one.
func (p *radarPoller) notePollRecovered() {
	if p.pollFailing {
		log.Printf("radar poll: recovered")
	}
	p.pollFailing = false
}
