package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// Target is the SignalK v2 Autopilot API
// (https://demo.signalk.org/documentation/Developing/REST_APIs/Autopilot_API.html)
// only. The legacy steering.autopilot.* PUT interface is deliberately not
// implemented and never fallen back to (docs/adr/0041-autopilot-widget.md):
// v2 providers advertise availableActions, which is what lets the tile grey
// out what a given pilot cannot do instead of guessing.
//
// Endpoint shapes here follow the community v2 spec as documented (list,
// _providers/_default, per-id status/state/mode/target/target-adjust/
// engage/disengage/tack/gybe/dodge). They could not be verified against a
// live SK 2.x autopilot provider in this environment — see the ADR's
// Verification section for the manual smoke test this still needs before
// being trusted against real steering gear.
const signalkAutopilotsBasePath = "/signalk/v2/api/vessels/self/autopilots"

// errAutopilotNotPresent distinguishes "SignalK has no autopilot provider at
// all" from a transport/upstream error — the two need different HTTP
// statuses and different messages to the operator.
var errAutopilotNotPresent = errors.New("no autopilot provider present")

// ── Default pilot id resolution & cache ──────────────────────────────────────

// autopilotIDCacheTTL bounds how long a resolved default pilot id is trusted
// before re-resolving. Short enough that a provider restart (a new default)
// is picked up quickly, long enough that every control tap doesn't cost an
// extra round trip.
const autopilotIDCacheTTL = 30 * time.Second

type cachedAutopilotID struct {
	id        string
	expiresAt time.Time
}

var (
	autopilotIDMu    sync.Mutex
	autopilotIDCache *cachedAutopilotID
)

// invalidateAutopilotIDCache clears the cached default pilot id so the next
// call re-resolves. Exported for tests; production callers don't need to
// call this (the TTL handles staleness).
func invalidateAutopilotIDCache() {
	autopilotIDMu.Lock()
	autopilotIDCache = nil
	autopilotIDMu.Unlock()
}

// autopilotDefaultIDResponse is the body of GET .../autopilots/_providers/_default.
type autopilotDefaultIDResponse struct {
	ID string `json:"id"`
}

// resolveAutopilotID returns the pilot id every autopilot handler should
// target. The frontend never carries a pilot id (per the plan) — this is
// the one place that decides it:
//
//  1. An explicit ui.autopilot_id setting always wins, and skips the network
//     entirely (the two-pilot case, or a provider whose "default" flag is
//     unreliable).
//  2. Otherwise, the cached id if still fresh.
//  3. Otherwise, GET .../autopilots to confirm a provider actually exists
//     (an empty result is errAutopilotNotPresent, not a zero-value id) and
//     GET .../autopilots/_providers/_default to resolve which one, caching
//     the result.
func resolveAutopilotID(signalkURL, settingsPath string) (string, error) {
	if configured := loadSettingString(settingsPath, []string{"ui", "autopilot_id"}); configured != "" {
		return configured, nil
	}

	autopilotIDMu.Lock()
	if autopilotIDCache != nil && time.Now().Before(autopilotIDCache.expiresAt) {
		id := autopilotIDCache.id
		autopilotIDMu.Unlock()
		return id, nil
	}
	autopilotIDMu.Unlock()

	list, err := fetchAutopilotList(signalkURL, settingsPath)
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "", errAutopilotNotPresent
	}

	status, body, err := signalkRequestJSONWithAuthBody(signalkURL, settingsPath, signalkAutopilotsBasePath+"/_providers/_default", http.MethodGet, nil)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", autopilotUpstreamStatusError(status, body)
	}

	var resolved autopilotDefaultIDResponse
	if err := json.Unmarshal(body, &resolved); err != nil || resolved.ID == "" {
		return "", errors.New("signalk: could not parse default autopilot id from response: " + string(body))
	}

	autopilotIDMu.Lock()
	autopilotIDCache = &cachedAutopilotID{id: resolved.ID, expiresAt: time.Now().Add(autopilotIDCacheTTL)}
	autopilotIDMu.Unlock()

	return resolved.ID, nil
}

// fetchAutopilotList GETs .../autopilots. An empty map is a normal "no
// provider registered" result, not an error — the caller decides what that
// means (resolveAutopilotID turns it into errAutopilotNotPresent).
func fetchAutopilotList(signalkURL, settingsPath string) (map[string]any, error) {
	status, body, err := signalkRequestJSONWithAuthBody(signalkURL, settingsPath, signalkAutopilotsBasePath, http.MethodGet, nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, autopilotUpstreamStatusError(status, body)
	}
	var list map[string]any
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, errors.New("signalk: could not parse autopilots list: " + string(body))
	}
	return list, nil
}

func autopilotUpstreamStatusError(status int, body []byte) error {
	return &autopilotUpstreamError{status: status, body: body}
}

// autopilotUpstreamError carries SignalK's exact status+body so handlers can
// proxy it verbatim instead of flattening every upstream failure to a
// generic 502 (transport errors, which have no status, still fall back to
// 502 — see writeAutopilotError).
type autopilotUpstreamError struct {
	status int
	body   []byte
}

func (e *autopilotUpstreamError) Error() string {
	return "signalk returned status " + http.StatusText(e.status) + ": " + string(e.body)
}

// writeAutopilotError answers an upstream failure with SignalK's own status
// and body when available, or 502 for a genuine transport failure (no
// status to proxy) — never a swallowed/generic error either way.
func writeAutopilotError(c echo.Context, err error) error {
	var upstream *autopilotUpstreamError
	if errors.As(err, &upstream) {
		return c.JSON(upstream.status, map[string]string{"error": string(upstream.body)})
	}
	return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
}

// ── GET /api/autopilot ───────────────────────────────────────────────────────

// getAutopilotHandler proxies GET .../autopilots/{id}, adding "present" and
// "id" but otherwise relaying SignalK's own field names verbatim rather than
// re-modelling a v2 status shape this codebase can't verify against a live
// provider. Absence is not a value: no provider at all answers
// {"present": false}, never a synthesized disengaged pilot.
func getAutopilotHandler(c echo.Context) error {
	signalkURL, settingsPath := resolveSignalKURL()

	id, err := resolveAutopilotID(signalkURL, settingsPath)
	if errors.Is(err, errAutopilotNotPresent) {
		return c.JSON(http.StatusOK, map[string]any{"present": false})
	}
	if err != nil {
		return writeAutopilotError(c, err)
	}

	status, body, err := signalkRequestJSONWithAuthBody(signalkURL, settingsPath, signalkAutopilotsBasePath+"/"+id, http.MethodGet, nil)
	if err != nil {
		return writeAutopilotError(c, err)
	}
	if status < 200 || status >= 300 {
		return c.JSON(status, map[string]string{"error": string(body)})
	}

	var upstream map[string]any
	if err := json.Unmarshal(body, &upstream); err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "could not parse signalk autopilot status: " + err.Error()})
	}
	upstream["present"] = true
	upstream["id"] = id

	return c.JSON(http.StatusOK, upstream)
}

// ── Control endpoints ────────────────────────────────────────────────────────

// autopilotControlHandler resolves the target pilot and issues one SignalK
// v2 request against it, proxying the result. method/suffix/body describe
// the exact SignalK call per the endpoint table in the plan; body is nil for
// a raw bodyless request (engage/disengage/tack/gybe/dodge-cancel) and a raw
// (unwrapped-by-this-function) JSON value otherwise — callers pre-wrap it in
// {"value": ...} per SignalK's v2 PUT convention, since that envelope is
// endpoint-specific (present on state/mode/target/dodge, absent on the
// POST actions) rather than something this helper can apply uniformly.
func autopilotControlHandler(c echo.Context, method, suffix string, body any) error {
	signalkURL, settingsPath := resolveSignalKURL()

	id, err := resolveAutopilotID(signalkURL, settingsPath)
	if errors.Is(err, errAutopilotNotPresent) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "no autopilot provider present"})
	}
	if err != nil {
		return writeAutopilotError(c, err)
	}

	status, respBody, err := signalkRequestJSONWithAuthBody(signalkURL, settingsPath, signalkAutopilotsBasePath+"/"+id+suffix, method, body)
	if err != nil {
		return writeAutopilotError(c, err)
	}
	if status < 200 || status >= 300 {
		return c.JSON(status, map[string]string{"error": string(respBody)})
	}

	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}

func postAutopilotEngageHandler(c echo.Context) error {
	return autopilotControlHandler(c, http.MethodPost, "/engage", nil)
}

func postAutopilotDisengageHandler(c echo.Context) error {
	return autopilotControlHandler(c, http.MethodPost, "/disengage", nil)
}

func putAutopilotStateHandler(c echo.Context) error {
	var req struct {
		State string `json:"state"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	if strings.TrimSpace(req.State) == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "state is required"})
	}
	return autopilotControlHandler(c, http.MethodPut, "/state", map[string]any{"value": req.State})
}

func putAutopilotModeHandler(c echo.Context) error {
	var req struct {
		Mode string `json:"mode"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	if strings.TrimSpace(req.Mode) == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "mode is required"})
	}
	return autopilotControlHandler(c, http.MethodPut, "/mode", map[string]any{"value": req.Mode})
}

// Helmcentral's own /target and /target/adjust bodies are always plain
// degrees ({"value": n}) — the tile has no radians mode — and this handler
// adds the "units":"deg" SignalK expects rather than exposing that detail to
// the frontend.
func putAutopilotTargetHandler(c echo.Context) error {
	var req struct {
		Value float64 `json:"value"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	return autopilotControlHandler(c, http.MethodPut, "/target", map[string]any{"value": req.Value, "units": "deg"})
}

func putAutopilotTargetAdjustHandler(c echo.Context) error {
	var req struct {
		Value float64 `json:"value"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	return autopilotControlHandler(c, http.MethodPut, "/target/adjust", map[string]any{"value": req.Value, "units": "deg"})
}

var validAutopilotSides = map[string]bool{"port": true, "starboard": true}

func postAutopilotTackHandler(c echo.Context) error {
	side := c.Param("side")
	if !validAutopilotSides[side] {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "side must be port or starboard"})
	}
	return autopilotControlHandler(c, http.MethodPost, "/tack/"+side, nil)
}

func postAutopilotGybeHandler(c echo.Context) error {
	side := c.Param("side")
	if !validAutopilotSides[side] {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "side must be port or starboard"})
	}
	return autopilotControlHandler(c, http.MethodPost, "/gybe/"+side, nil)
}

func putAutopilotDodgeHandler(c echo.Context) error {
	var req struct {
		Value float64 `json:"value"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	return autopilotControlHandler(c, http.MethodPut, "/dodge", map[string]any{"value": req.Value, "units": "deg"})
}

func deleteAutopilotDodgeHandler(c echo.Context) error {
	return autopilotControlHandler(c, http.MethodDelete, "/dodge", nil)
}

// ── Live state for the telemetry stream ──────────────────────────────────────

// autopilotStaleAfter bounds how long steering.autopilot.* can go quiet
// before the tile stops trusting its last-known value. Engaged/disengaged is
// a safety-relevant fact, so silence must become visible rather than being
// mistaken for "nothing changed" — the same reasoning as the anchor drag
// watchdog (ADR 0038).
const autopilotStaleAfter = 5 * time.Second

// buildAutopilotPayload produces the "autopilot" SSE event body from the
// existing delta-stream snapshot (ADR 0037) — no polling, no second
// ingestion path. Absence is not a value: no steering.autopilot.engaged on
// the stream at all means no autopilot is present, and the payload says so
// explicitly rather than reporting a synthesized disengaged pilot.
func buildAutopilotPayload() map[string]any {
	tree := globalSignalKSnapshot.selfTree()
	if tree == nil {
		return map[string]any{"present": false}
	}
	if _, ok := lookupAnyValue(tree, "steering", "autopilot", "engaged", "value"); !ok {
		return map[string]any{"present": false}
	}

	engaged, _ := lookupBool(tree, "steering", "autopilot", "engaged", "value")
	state := lookupString(tree, "steering", "autopilot", "state", "value")
	mode := lookupString(tree, "steering", "autopilot", "mode", "value")
	target := lookupNumber(tree, "steering", "autopilot", "target", "value")

	availableActions := []string{}
	if raw, ok := lookupAnyValue(tree, "steering", "autopilot", "availableActions", "value"); ok {
		if list, ok := raw.([]any); ok {
			for _, item := range list {
				if id, ok := item.(string); ok {
					availableActions = append(availableActions, id)
				}
			}
		}
	}

	context := globalSignalKSnapshot.selfContext()
	stale := globalSignalKSnapshot.stale(context, "steering.autopilot.engaged", autopilotStaleAfter, time.Now())

	return map[string]any{
		"present":           true,
		"engaged":           engaged,
		"state":             state,
		"mode":              mode,
		"target":            target,
		"available_actions": availableActions,
		"stale":             stale,
	}
}
