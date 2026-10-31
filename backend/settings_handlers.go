package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/labstack/echo/v4"
	"gopkg.in/yaml.v3"
)

const (
	defaultDistanceUnits    = "metric"
	defaultBowRollerHeightM = 1.5
	defaultChainSizeMM      = 12
	defaultChainOnboardM    = 150
	defaultHullType         = "power_cat"
	defaultScopeMethod      = "ratio"
	defaultWindageAreaM2    = 35
	// defaultMinClearanceAtLowM is the generic seamanship margin Anchor
	// Watch's low-water clearance warning uses until the operator sets
	// their own (ADR 0135) - not a vessel-specific value.
	defaultMinClearanceAtLowM = 0.5
	// defaultMayaraPort is mayara-server's own default REST/WebSocket port.
	// Used only to clamp an out-of-range settings.mayara.port; unlike
	// defaultSignalKAddress there is no equivalent default *address* — see
	// normalizeSettingsPayload.
	defaultMayaraPort = 6502
)

type settingsPayload struct {
	Signalk struct {
		Address string `json:"address"`
		Port    int    `json:"port"`
	} `json:"signalk"`
	Boat struct {
		VesselPrefix string `json:"vessel_prefix"`
		Model        string `json:"model"`
	} `json:"boat"`
	UI struct {
		TideProvider    string `json:"tide_provider"`
		TideStationID   string `json:"tide_station_id"`
		TideStationName string `json:"tide_station_name"`
		TideAutoStation bool   `json:"tide_auto_station"`
		WeatherProvider string `json:"weather_provider"`
		WaveProvider    string `json:"wave_provider"`
		POIProvider     string `json:"poi_provider"`
		// PlaceNameProvider (ADR 0101) selects which installed POI plugin
		// answers place-name questions (the position tile, the anchor pin,
		// find_places) - independent of POIProvider (the Nearby widget),
		// though the two commonly name the same plugin.
		PlaceNameProvider        string `json:"place_name_provider"`
		ForecastWarningsProvider string `json:"forecast_warnings_provider"`
	} `json:"ui"`
	Anchor struct {
		BowRollerHeightM float64 `json:"bow_roller_height_m"`
		ChainSizeMM      float64 `json:"chain_size_mm"`
		ChainOnboardM    float64 `json:"chain_onboard_m"`
		HullType         string  `json:"hull_type"`
		ScopeMethod      string  `json:"scope_method"`
		WindageAreaM2    float64 `json:"windage_area_m2"`
		GPSFromBowM      float64 `json:"gps_from_bow_m"`
		LOAM             float64 `json:"loa_m"`
		// AutoRaiseOnMotoring gates the server-side anchor auto-raise
		// watcher (anchor_auto_raise.go, ADR 0099): true unless the
		// operator has explicitly turned it off. Unlike every other field
		// on this struct, its zero value is NOT its default - a plain bool
		// can't tell "the operator saved false" from "this key predates the
		// feature and was never in the request at all." So the true
		// default lives only in buildSettingsPayload (applied before the
		// settings.yaml overlay, below); this field, and
		// normalizeSettingsPayload's handling of it, both pass the
		// submitted value straight through with no override.
		AutoRaiseOnMotoring bool `json:"auto_raise_on_motoring"`
		// MinClearanceAtLowM is the water the operator wants under the keel
		// at the next low tide (ADR 0135) before Anchor Watch's low-water
		// clearance warning fires. Defaults to a generic 0.5m seamanship
		// margin, not a vessel-specific value. Zero is a valid, deliberate
		// choice (warn only once the keel would touch), so - like
		// AutoRaiseOnMotoring above - its zero value is NOT its default: a
		// bare 0 can't tell "the operator saved zero" from "this
		// settings.yaml predates the feature." The true default lives only
		// in buildSettingsPayload (applied before the disk overlay); this
		// field, and normalizeSettingsPayload's handling of it, only ever
		// reject a negative or non-finite submission by replacing it with
		// that same default - a genuine zero or positive value passes
		// straight through.
		MinClearanceAtLowM float64 `json:"min_clearance_at_low_m"`
	} `json:"anchor"`
	Influxdb struct {
		Enabled bool   `json:"enabled"`
		URL     string `json:"url"`
		Org     string `json:"org"`
		Bucket  string `json:"bucket"`
	} `json:"influxdb"`
	// Mayara is the mayara-server address, used only by the radar picture
	// overlay. Targets do not need it: they arrive through the SignalK plugin
	// (ADR 0062 amendment). The overlay does, because the plugin returns 404
	// for the spoke stream.
	Mayara struct {
		Address string `json:"address"`
		Port    int    `json:"port"`
	} `json:"mayara"`
	// Assistant configures the onboard OpenRouter-backed assistant, Mate
	// (ADR 0093): off by default, BYOK (the key lives in the secrets store,
	// not here), and Notes are operator standing notes injected verbatim
	// into every system prompt. VoiceInput/ReadAloud/WakeWord gate the
	// frontend's voice features (mate-voice-assistant plan phase 1/2); the
	// backend stores and round-trips them but does not otherwise act on
	// them.
	Assistant struct {
		Enabled bool   `json:"enabled"`
		Model   string `json:"model"`
		// DocumentModel is the model the document indexer's enrich stage
		// (ADR 0106) uses for OCR and suggested title/summary/tags - a
		// separate setting from Model since it runs unattended on every
		// upload rather than being picked per conversation.
		DocumentModel string `json:"document_model"`
		// EmbeddingModel is the model the document library's semantic-search
		// embedding stage (E1b) uses to vectorise chunk text - a separate
		// setting from both Model and DocumentModel, since it runs over
		// every chunk of every consented document rather than per question
		// or per upload. A blank value turns semantic search off entirely
		// (FTS5 stays the only retriever); it does not fall back to either
		// of the other two models.
		EmbeddingModel string `json:"embedding_model"`
		// EmbeddingDimensions is the vector length requested alongside
		// EmbeddingModel. Smaller than a model's native size trades a
		// little retrieval accuracy for less work per chunk scanned - the
		// whole index is scanned in Go, not by a vector database.
		EmbeddingDimensions int      `json:"embedding_dimensions"`
		Notes               string   `json:"notes"`
		AllowedModels       []string `json:"allowed_models"`
		ExcludedModels      []string `json:"excluded_models"`
		CostTier            string   `json:"cost_tier"`
		VoiceInput          bool     `json:"voice_input"`
		ReadAloud           bool     `json:"read_aloud"`
		WakeWord            bool     `json:"wake_word"`
		// VerboseRunLog writes one log line for every round of every Mate run,
		// for use while Mate's behaviour is being worked on. Off by default.
		VerboseRunLog bool `json:"verbose_run_log"`
		// WebSearch lets Mate search the web through OpenRouter when it needs
		// outside information (search_web). Off by default; when off the tool
		// is not offered to the model at all.
		WebSearch bool `json:"web_search"`
		// WebSearchModel carries the search sub-request (ADR 0159). Required
		// while WebSearch is on: a blank value is rejected on save and never
		// replaced by the default at search time.
		WebSearchModel string `json:"web_search_model"`
		// PassageLimits are the operator's go/no-go thresholds for Mate's
		// passage assessment (ADR 0191). No defaults: every value is unset
		// until the operator enters it.
		PassageLimits passageLimits `json:"passage_limits"`
	} `json:"assistant"`
	// OfflineCoastline is the on-board shoreline Mate's shelter estimate and
	// later features read (ADR 0203). Off by default: it downloads from the
	// internet in the background.
	OfflineCoastline struct {
		Enabled bool `json:"enabled"`
	} `json:"offline_coastline"`
	Auth struct {
		Mode string `json:"mode"`
	} `json:"auth"`
	Units string `json:"units"`
	// Roles is the roles block (vessel_roles.go): which equipment item is the
	// house bank, the port alternator, and so on. A pointer so a save that
	// omits it leaves the stored block alone: every other section here is
	// rewritten wholesale from the payload, but an older client or a scripted
	// partial post must not wipe the roles by not mentioning it. GET always
	// returns it.
	Roles *vesselRoles `json:"roles,omitempty"`
	// RolesError is set by GET only, when the stored roles block cannot be
	// parsed: the block is then omitted and this says why. The rest of the
	// settings still load, so one corrupt section cannot make the page show
	// defaults and overwrite the others on its next save. Ignored on POST.
	RolesError string `json:"roles_error,omitempty"`
}

func getSettingsHandler(c echo.Context) error {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	settings, err := readSettings(settingsPath)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read settings"})
	}

	// Unreadable passage limits fail the read: blanks sent to the page would
	// be saved straight back over the operator's values.
	if assistantMap, ok := settings["assistant"].(map[string]any); ok {
		if _, err := passageLimitsFromSettings(assistantMap["passage_limits"]); err != nil {
			log.Printf("settings: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read settings: passage_limits is unreadable"})
		}
	}

	payload := buildSettingsPayload(settings)
	// A corrupt roles block is surfaced, not masked: it is omitted and named
	// in roles_error, while every other setting is still returned.
	roles, err := parseRolesBlock(settings)
	if err != nil {
		log.Printf("settings: %v", err)
		payload.RolesError = err.Error()
		return c.JSON(http.StatusOK, payload)
	}
	payload.Roles = &roles

	return c.JSON(http.StatusOK, payload)
}

// checkPassageLimitsShape refuses a passage_limits block the current shape
// cannot read, such as the one a tab on an older bundle still posts. The
// binder ignores unknown fields, so without this the block would decode to
// empty limits and be saved over the operator's values.
func checkPassageLimitsShape(body []byte) error {
	var probe struct {
		Assistant struct {
			PassageLimits json.RawMessage `json:"passage_limits"`
		} `json:"assistant"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return nil // c.Bind reports an unreadable body
	}
	raw := probe.Assistant.PassageLimits
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var l passageLimits
	if err := dec.Decode(&l); err != nil {
		log.Printf("settings: assistant.passage_limits refused: %v", err)
		return errors.New("assistant.passage_limits: Passage limits were sent by a page from before the last update. Reload the page and save again")
	}
	return nil
}

func updateSettingsHandler(c echo.Context) error {
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
	}
	c.Request().Body = io.NopCloser(bytes.NewReader(body))
	if err := checkPassageLimitsShape(body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"field": "assistant.passage_limits", "error": err.Error()})
	}
	var req settingsPayload
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
	}

	settingsWriteMu.Lock()
	defer settingsWriteMu.Unlock()

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	settings, err := readSettings(settingsPath)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read settings"})
	}

	normalized := normalizeSettingsPayload(req)
	current := buildSettingsPayload(settings)
	// A roles block that cannot be read is treated as nothing saved, so a save
	// that sets the roles again repairs it.
	if saved, err := parseRolesBlock(settings); err == nil {
		current.Roles = &saved
	}

	if invalid := validateSettingsChange(current, normalized); invalid != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{
			"field": invalid.Field,
			"error": invalid.Message,
		})
	}

	// E-1 (2026-09-19 security audit, ADR 0111 amendment): influxdb.url and
	// signalk.address/port are free text naming where INFLUXDB_TOKEN and the
	// SignalK credential pair get sent. Repointing either at a destination
	// you control is enough to have the credential handed to it on the next
	// query or auth login - see influx.go's Authorization header and
	// signalk.go's acquireSignalKToken, which sends username/password in a
	// cleartext JSON body. Clearing the bound secret the moment its
	// destination changes (Option B) means the new destination gets nothing
	// until the credential is re-entered.
	//
	// This runs after validateSettingsChange (so a rejected save, e.g. an
	// unreachable new address, clears nothing - the old destination and old
	// secret stay paired) and before settings is written below (so a clear
	// failure aborts the save rather than ever persisting a new destination
	// next to a secret that should have gone with the old one).
	if destinationChanged(current.Influxdb.URL, normalized.Influxdb.URL) {
		reason := fmt.Sprintf("influxdb.url changed from %q to %q", current.Influxdb.URL, normalized.Influxdb.URL)
		if err := clearBoundSecret("INFLUXDB_TOKEN", reason); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to clear a secret bound to the previous destination; settings not saved"})
		}
	}
	if destinationChanged(current.Signalk.Address, normalized.Signalk.Address) || current.Signalk.Port != normalized.Signalk.Port {
		reason := fmt.Sprintf("signalk address changed from %s:%d to %s:%d",
			current.Signalk.Address, current.Signalk.Port, normalized.Signalk.Address, normalized.Signalk.Port)
		if err := clearBoundSecret("SIGNALK_USERNAME", reason); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to clear a secret bound to the previous destination; settings not saved"})
		}
		// Two separate clears rather than one call for the pair: if this one
		// fails after SIGNALK_USERNAME already cleared above, the save still
		// aborts below and the username stays cleared - safe-biased (an
		// over-cleared secret, never a leaked one), and worth the small
		// inconsistency it can leave in the secrets store.
		if err := clearBoundSecret("SIGNALK_PASSWORD", reason); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to clear a secret bound to the previous destination; settings not saved"})
		}
	}

	settings["signalk"] = map[string]any{
		"address": normalized.Signalk.Address,
		"port":    normalized.Signalk.Port,
	}
	settings["boat"] = map[string]any{
		"vessel_prefix": normalized.Boat.VesselPrefix,
		"model":         normalized.Boat.Model,
	}

	uiMap := map[string]any{}
	if existingUI, ok := settings["ui"].(map[string]any); ok {
		for key, value := range existingUI {
			uiMap[key] = value
		}
	}
	// The boat's settings.yaml may still carry this retired key from before
	// the setting was removed. readSettings decodes into a plain map, so a
	// stale key never breaks loading, but every save must still drop it from
	// the file for good instead of copying it forward via the loop above.
	delete(uiMap, "vessel_state_refresh_seconds")
	// Tank labels moved onto equipment items (ADR 0187); drop the retired key.
	delete(uiMap, "tank_labels")
	uiMap["tide_provider"] = normalized.UI.TideProvider
	uiMap["tide_station_id"] = normalized.UI.TideStationID
	uiMap["tide_station_name"] = normalized.UI.TideStationName
	uiMap["tide_auto_station"] = normalized.UI.TideAutoStation
	uiMap["weather_provider"] = normalized.UI.WeatherProvider
	uiMap["wave_provider"] = normalized.UI.WaveProvider
	uiMap["poi_provider"] = normalized.UI.POIProvider
	uiMap["place_name_provider"] = normalized.UI.PlaceNameProvider
	uiMap["forecast_warnings_provider"] = normalized.UI.ForecastWarningsProvider
	settings["ui"] = uiMap

	settings["anchor"] = map[string]any{
		"bow_roller_height_m":    normalized.Anchor.BowRollerHeightM,
		"chain_size_mm":          normalized.Anchor.ChainSizeMM,
		"chain_onboard_m":        normalized.Anchor.ChainOnboardM,
		"hull_type":              normalized.Anchor.HullType,
		"scope_method":           normalized.Anchor.ScopeMethod,
		"windage_area_m2":        normalized.Anchor.WindageAreaM2,
		"gps_from_bow_m":         normalized.Anchor.GPSFromBowM,
		"loa_m":                  normalized.Anchor.LOAM,
		"auto_raise_on_motoring": normalized.Anchor.AutoRaiseOnMotoring,
		"min_clearance_at_low_m": normalized.Anchor.MinClearanceAtLowM,
	}
	settings["auth"] = map[string]any{
		"mode": normalized.Auth.Mode,
	}
	settings["influxdb"] = map[string]any{
		"enabled": normalized.Influxdb.Enabled,
		"url":     normalized.Influxdb.URL,
		"org":     normalized.Influxdb.Org,
		"bucket":  normalized.Influxdb.Bucket,
	}
	settings["mayara"] = map[string]any{
		"address": normalized.Mayara.Address,
		"port":    normalized.Mayara.Port,
	}
	settings["assistant"] = map[string]any{
		"enabled":              normalized.Assistant.Enabled,
		"model":                normalized.Assistant.Model,
		"document_model":       normalized.Assistant.DocumentModel,
		"embedding_model":      normalized.Assistant.EmbeddingModel,
		"embedding_dimensions": normalized.Assistant.EmbeddingDimensions,
		"notes":                normalized.Assistant.Notes,
		"allowed_models":       normalized.Assistant.AllowedModels,
		"excluded_models":      normalized.Assistant.ExcludedModels,
		"cost_tier":            normalized.Assistant.CostTier,
		"voice_input":          normalized.Assistant.VoiceInput,
		"read_aloud":           normalized.Assistant.ReadAloud,
		"wake_word":            normalized.Assistant.WakeWord,
		"verbose_run_log":      normalized.Assistant.VerboseRunLog,
		"web_search":           normalized.Assistant.WebSearch,
		"web_search_model":     normalized.Assistant.WebSearchModel,
	}
	// No limit set: the key stays out of settings.yaml altogether.
	if normalized.Assistant.PassageLimits != (passageLimits{}) {
		block, err := passageLimitsToSettings(normalized.Assistant.PassageLimits)
		if err != nil {
			log.Printf("settings: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to save settings: passage_limits could not be converted"})
		}
		settings["assistant"].(map[string]any)["passage_limits"] = block
	}
	settings["offline_coastline"] = map[string]any{
		"enabled": normalized.OfflineCoastline.Enabled,
	}
	settings["units"] = normalized.Units

	// roles is only touched when the request carries it (see the Roles
	// field's comment); an explicit empty block clears them.
	if normalized.Roles != nil {
		block, err := rolesBlockMap(*normalized.Roles)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to save settings"})
		}
		settings["roles"] = block
	}

	if err := writeSettings(settingsPath, settings); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to save settings"})
	}
	if globalCoastlineManager != nil {
		// The Offline coastline switch may have changed: the page refetches
		// status right after this save and must see the new switch.
		globalCoastlineManager.SettingsChanged()
	}

	// A house bank may have just been chosen or changed: move the alarm rules
	// that follow it, then seed whichever anomaly-v1 sub-sets that unlocks. A
	// failure is logged, not returned: the save itself succeeded, every
	// sub-set's marker makes seeding idempotent, and main.go retries it at the
	// next start.
	if normalized.Roles != nil {
		invalidateSourceItems()
		vessel, err := loadVesselView(settingsPath)
		if err != nil {
			log.Printf("could not load the vessel after a settings save: %v", err)
		} else {
			if err := syncRoleBoundRules(vessel); err != nil {
				log.Printf("could not move the house bank alarm rules after a settings save: %v", err)
			}
			if err := seedAnomalyRules(vessel); err != nil {
				log.Printf("could not seed the anomaly alarm rules after a settings save: %v", err)
			}
		}
	}

	// ADR 0120: this save may be the moment Mate just became ready (a key
	// pasted in, document/embedding models chosen) - sweep the enrich=0
	// backlog now rather than waiting for the next boot. sweepDocumentsIfReady
	// re-reads readiness itself and is a no-op whenever this save didn't
	// change anything about it, so it is called on every settings save rather
	// than diffing old vs. new readiness here. It runs in the background: it
	// waits on the document store's lock, which a long indexing write can
	// hold, and a settings save has no business waiting on that. A sweep
	// failure is logged inside sweepDocumentsIfReady and never touches the
	// save, which has already succeeded.
	go sweepDocumentsIfReady()

	return c.JSON(http.StatusOK, normalized)
}

// settingsValidationError identifies which field of a bulk settings save was
// rejected, so the UI can surface the message against the offending input
// rather than as a page-level "save failed".
type settingsValidationError struct {
	Field   string
	Message string
}

func (e *settingsValidationError) Error() string { return e.Message }

// validateSettingsChange gates POST /api/settings, which is a full-payload
// replace: without it, any value the form happens to be holding gets
// persisted verbatim — which is how a browser test that filled the address
// field and clicked "Save and Continue" once repointed the live dashboard at
// a dead host (docs/adr/0026). Since ADR 0028 removed the old
// POST /api/settings/signalk, this is the only path that writes the address,
// so this check is the only thing standing between a typo and an offline
// dashboard.
//
// Checks fire only on *change*. Probing SignalK on every save would mean an
// unreachable vessel — an entirely normal state, e.g. configuring from home
// or while the boat is powered down — blocks edits to tank labels, anchor
// geometry and units that have nothing to do with SignalK. Guarding the
// transition, not the steady state, is what makes the check safe to apply to
// an endpoint that saves everything at once.
func validateSettingsChange(current, next settingsPayload) *settingsValidationError {
	// Checked on every save, not only on change: web search on with no search
	// model can never work, and substituting a default here or at search time
	// would hide the gap (ADR 0159).
	if next.Assistant.WebSearch && next.Assistant.WebSearchModel == "" {
		return &settingsValidationError{
			Field:   "assistant.web_search_model",
			Message: "choose a search model before turning on web search",
		}
	}

	if invalid := validatePassageLimits(next.Assistant.PassageLimits); invalid != nil {
		return invalid
	}

	if next.Roles != nil {
		items, err := listSourceItems()
		if err != nil {
			return &settingsValidationError{Field: "roles", Message: "could not read the equipment items: " + err.Error()}
		}
		if invalid := validateVesselRoles(*next.Roles, items, current.Roles); invalid != nil {
			return invalid
		}
	}

	if next.Signalk.Address != current.Signalk.Address || next.Signalk.Port != current.Signalk.Port {
		signalkURL := buildSignalKURL(next.Signalk.Address, next.Signalk.Port)
		vesselPath := getEnv("SIGNALK_VESSEL_PATH", "/signalk/v1/api/vessels/self")
		if err := probeSignalKReachable(signalkURL, vesselPath); err != nil {
			return &settingsValidationError{
				Field:   "signalk.address",
				Message: fmt.Sprintf("unable to connect to SignalK at %s", signalkURL),
			}
		}
	}

	if next.Auth.Mode != current.Auth.Mode {
		if !validAuthModes[next.Auth.Mode] {
			return &settingsValidationError{
				Field:   "auth.mode",
				Message: fmt.Sprintf("unknown authentication mode %q", next.Auth.Mode),
			}
		}

		// Turning auth off is never gated: it is the way out of a lockout, and
		// requiring a reachable SignalK to disable it would make a broken
		// server unrecoverable from the UI.
		if next.Auth.Mode == authModeSignalK {
			signalkURL := buildSignalKURL(next.Signalk.Address, next.Signalk.Port)
			enabled, err := probeSignalKSecurityEnabled(signalkURL)
			if err != nil {
				return &settingsValidationError{
					Field:   "auth.mode",
					Message: fmt.Sprintf("could not check SignalK security at %s — is the server reachable?", signalkURL),
				}
			}
			if !enabled {
				// Refuse at save time rather than at the next restart. Saving an
				// unsatisfiable config and only finding out on reboot is exactly
				// how an operator locks themselves out of a running boat.
				return &settingsValidationError{
					Field:   "auth.mode",
					Message: fmt.Sprintf("SignalK at %s has security disabled — enable it there (Server → Security) before requiring login", signalkURL),
				}
			}
		}
	}

	return nil
}

func buildSettingsPayload(settings map[string]any) settingsPayload {
	payload := normalizeSettingsPayload(settingsPayload{})
	// An absent web_search_model defaults; a stored blank stays blank (see the
	// override in the assistant block below).
	payload.Assistant.WebSearchModel = defaultWebSearchModel
	// The true default lives here, not in normalizeSettingsPayload's
	// baseline call above: that function also normalizes genuine save
	// requests, where a bare bool can't tell "the operator submitted false"
	// from "the zero-value baseline." Set explicitly here, before the disk
	// overlay below, so an absent key on disk surfaces as true and only an
	// explicit stored value (true or false) overrides it.
	payload.Anchor.AutoRaiseOnMotoring = true
	// Same split, same reason as AutoRaiseOnMotoring immediately above: the
	// true default (a generic 0.5m margin) lives here, not in
	// normalizeSettingsPayload, because a submitted zero is indistinguishable
	// from an unset Go zero value once it reaches that function.
	payload.Anchor.MinClearanceAtLowM = defaultMinClearanceAtLowM
	// Same split, same reason (see normalizeSettingsPayload's own comment on
	// EmbeddingModel): an absent embedding_model is a settings.yaml written
	// before the key existed and defaults here, while a key present and blank
	// is an operator who turned semantic search off and must stay off.
	payload.Assistant.EmbeddingModel = defaultEmbeddingModel

	if signalkMap, ok := settings["signalk"].(map[string]any); ok {
		address := coerceString(signalkMap["address"])
		if strings.TrimSpace(address) != "" {
			payload.Signalk.Address = strings.TrimSpace(address)
		}
		port := coercePort(signalkMap["port"])
		if port > 0 && port <= 65535 {
			payload.Signalk.Port = port
		}
	}

	if boatMap, ok := settings["boat"].(map[string]any); ok {
		if prefix := strings.TrimSpace(coerceString(boatMap["vessel_prefix"])); prefix != "" {
			payload.Boat.VesselPrefix = prefix
		}
		if model := strings.TrimSpace(coerceString(boatMap["model"])); model != "" {
			payload.Boat.Model = model
		}
	}

	if uiMap, ok := settings["ui"].(map[string]any); ok {
		payload.UI.TideProvider = strings.TrimSpace(coerceString(uiMap["tide_provider"]))
		payload.UI.TideStationID = strings.TrimSpace(coerceString(uiMap["tide_station_id"]))
		payload.UI.TideStationName = strings.TrimSpace(coerceString(uiMap["tide_station_name"]))
		if v, ok := uiMap["tide_auto_station"].(bool); ok {
			payload.UI.TideAutoStation = v
		}
		payload.UI.WeatherProvider = strings.TrimSpace(coerceString(uiMap["weather_provider"]))
		payload.UI.WaveProvider = strings.TrimSpace(coerceString(uiMap["wave_provider"]))
		payload.UI.POIProvider = strings.TrimSpace(coerceString(uiMap["poi_provider"]))
		payload.UI.PlaceNameProvider = strings.TrimSpace(coerceString(uiMap["place_name_provider"]))
		payload.UI.ForecastWarningsProvider = strings.TrimSpace(coerceString(uiMap["forecast_warnings_provider"]))
	}

	if anchorMap, ok := settings["anchor"].(map[string]any); ok {
		if value := coerceFloat(anchorMap["bow_roller_height_m"]); value > 0 {
			payload.Anchor.BowRollerHeightM = value
		}
		if value := coerceFloat(anchorMap["chain_size_mm"]); value > 0 {
			payload.Anchor.ChainSizeMM = value
		}
		if value := coerceFloat(anchorMap["chain_onboard_m"]); value > 0 {
			payload.Anchor.ChainOnboardM = value
		}
		if hullType := strings.TrimSpace(coerceString(anchorMap["hull_type"])); isSupportedHullType(hullType) {
			payload.Anchor.HullType = hullType
		}
		if scopeMethod := strings.TrimSpace(coerceString(anchorMap["scope_method"])); isSupportedScopeMethod(scopeMethod) {
			payload.Anchor.ScopeMethod = scopeMethod
		}
		if value := coerceFloat(anchorMap["windage_area_m2"]); value > 0 {
			payload.Anchor.WindageAreaM2 = value
		}
		// gps_from_bow_m defaults to 0 ("no correction"), so 0 is itself a
		// meaningful, explicit value — unlike the other anchor fields above, it
		// must not be treated the same as "absent". coerceFloat returns -1 for
		// a missing/unparseable key, so >= 0 is the correct guard here.
		if value := coerceFloat(anchorMap["gps_from_bow_m"]); value >= 0 {
			payload.Anchor.GPSFromBowM = value
		}
		if value := coerceFloat(anchorMap["loa_m"]); value > 0 {
			payload.Anchor.LOAM = value
		}
		// Presence, not truthiness, is what distinguishes "the operator
		// explicitly turned this off" from "this settings.yaml predates the
		// feature" - the type assertion's ok is the only signal available
		// for a bool, so an absent key leaves the true default set above
		// untouched, and only a present key (true or false) overrides it.
		if v, ok := anchorMap["auto_raise_on_motoring"].(bool); ok {
			payload.Anchor.AutoRaiseOnMotoring = v
		}
		// Presence, not value, distinguishes "the operator explicitly saved
		// zero" from "this settings.yaml predates the feature" - coerceFloat
		// returns -1 for a missing/unparseable key, so a non-negative,
		// finite value is the presence gate (same >= 0 pattern gps_from_bow_m
		// above uses, plus an explicit +Inf guard since coerceFloat can
		// return it verbatim from a YAML .inf literal, and +Inf >= 0 is true).
		if value := coerceFloat(anchorMap["min_clearance_at_low_m"]); value >= 0 && !math.IsInf(value, 0) {
			payload.Anchor.MinClearanceAtLowM = value
		}
	}

	if influxMap, ok := settings["influxdb"].(map[string]any); ok {
		if v, ok := influxMap["enabled"].(bool); ok {
			payload.Influxdb.Enabled = v
		}
		payload.Influxdb.URL = strings.TrimSpace(coerceString(influxMap["url"]))
		payload.Influxdb.Org = strings.TrimSpace(coerceString(influxMap["org"]))
		payload.Influxdb.Bucket = strings.TrimSpace(coerceString(influxMap["bucket"]))
	}

	if mayaraMap, ok := settings["mayara"].(map[string]any); ok {
		// Unconditional, like the influxdb fields above: an address stored as
		// blank must surface as blank, not be mistaken for "absent" and papered
		// over with whatever normalizeSettingsPayload({}) defaulted payload to.
		payload.Mayara.Address = strings.TrimSpace(coerceString(mayaraMap["address"]))
		if port := coercePort(mayaraMap["port"]); port > 0 && port <= 65535 {
			payload.Mayara.Port = port
		}
	}

	if assistantMap, ok := settings["assistant"].(map[string]any); ok {
		if v, ok := assistantMap["enabled"].(bool); ok {
			payload.Assistant.Enabled = v
		}
		// Unconditional, like the mayara address above: a model or notes
		// value stored as blank must surface as blank, not be mistaken for
		// "absent" and papered over with normalizeSettingsPayload({})'s
		// default model.
		payload.Assistant.Model = strings.TrimSpace(coerceString(assistantMap["model"]))
		// document_model (ADR 0106) is newer than the assistant block itself,
		// so unlike model above, a settings.yaml written before it existed
		// has the key genuinely absent rather than present-and-blank - and a
		// plain map lookup can't tell those apart (both come back as the
		// zero value ""). Only an explicit key, present() checked before
		// coercing, overrides the normalizeSettingsPayload({}) default set
		// above; an absent key leaves that default in place, the same
		// presence-vs-value distinction auto_raise_on_motoring uses below.
		if raw, ok := assistantMap["document_model"]; ok {
			payload.Assistant.DocumentModel = strings.TrimSpace(coerceString(raw))
		}
		// embedding_model (E1b) is newer still than document_model, so the
		// same presence-vs-value distinction applies: a settings.yaml
		// written before it existed has the key genuinely absent, and that
		// must default (below), not surface as the blank that turns
		// semantic search off for every installed boat at once.
		if raw, ok := assistantMap["embedding_model"]; ok {
			payload.Assistant.EmbeddingModel = strings.TrimSpace(coerceString(raw))
		}
		// embedding_dimensions is a number rather than a string, but the
		// same presence check applies: an absent key must default (below),
		// not surface as the zero value coercePort already returns for a
		// missing map entry. A present-but-invalid value (<= 0, or above the
		// 4096 cap - see normalizeSettingsPayload) is normalised the same
		// way right here, since buildSettingsPayload never re-runs the
		// disk-read result back through normalizeSettingsPayload.
		if raw, ok := assistantMap["embedding_dimensions"]; ok {
			dims := coercePort(raw)
			if dims <= 0 {
				dims = defaultEmbeddingDimensions
			}
			if dims > 4096 {
				dims = 4096
			}
			payload.Assistant.EmbeddingDimensions = dims
		}
		payload.Assistant.Notes = coerceString(assistantMap["notes"])
		payload.Assistant.AllowedModels = coerceStringList(assistantMap["allowed_models"])
		payload.Assistant.ExcludedModels = coerceStringList(assistantMap["excluded_models"])
		payload.Assistant.CostTier = strings.TrimSpace(strings.ToLower(coerceString(assistantMap["cost_tier"])))
		// The three voice switches all default false, the same as Enabled -
		// a settings.yaml written before they existed (or one where the
		// operator has simply never turned them on) must surface as off,
		// never a guessed true.
		if v, ok := assistantMap["voice_input"].(bool); ok {
			payload.Assistant.VoiceInput = v
		}
		if v, ok := assistantMap["read_aloud"].(bool); ok {
			payload.Assistant.ReadAloud = v
		}
		if v, ok := assistantMap["wake_word"].(bool); ok {
			payload.Assistant.WakeWord = v
		}
		if v, ok := assistantMap["verbose_run_log"].(bool); ok {
			payload.Assistant.VerboseRunLog = v
		}
		if raw, ok := assistantMap["web_search_model"]; ok {
			payload.Assistant.WebSearchModel = strings.TrimSpace(coerceString(raw))
		}
		if v, ok := assistantMap["web_search"].(bool); ok {
			payload.Assistant.WebSearch = v
		}
		// Callers that must not proceed on a bad block (GET /api/settings, the
		// setup status) decode it themselves with passageLimitsFromSettings
		// and fail; here an unreadable block is logged and left unset.
		limits, err := passageLimitsFromSettings(assistantMap["passage_limits"])
		if err != nil {
			log.Printf("settings: %v", err)
		}
		payload.Assistant.PassageLimits = limits
	}

	if coastMap, ok := settings["offline_coastline"].(map[string]any); ok {
		if v, ok := coastMap["enabled"].(bool); ok {
			payload.OfflineCoastline.Enabled = v
		}
	}

	if authMap, ok := settings["auth"].(map[string]any); ok {
		payload.Auth.Mode = strings.TrimSpace(coerceString(authMap["mode"]))
	}

	units := strings.TrimSpace(coerceString(settings["units"]))
	if strings.EqualFold(units, "metric") || strings.EqualFold(units, "imperial") {
		payload.Units = strings.ToLower(units)
	}

	return payload
}

func normalizeSettingsPayload(req settingsPayload) settingsPayload {
	normalized := settingsPayload{}
	normalized.Signalk.Address = strings.TrimSpace(req.Signalk.Address)
	if normalized.Signalk.Address == "" {
		normalized.Signalk.Address = defaultSignalKAddress
	}
	normalized.Signalk.Port = req.Signalk.Port
	if normalized.Signalk.Port <= 0 || normalized.Signalk.Port > 65535 {
		normalized.Signalk.Port = defaultSignalKPort
	}

	normalized.Boat.VesselPrefix = strings.TrimSpace(req.Boat.VesselPrefix)
	if normalized.Boat.VesselPrefix == "" {
		normalized.Boat.VesselPrefix = "M/V"
	}
	normalized.Boat.Model = strings.TrimSpace(req.Boat.Model)

	normalized.UI.TideProvider = strings.TrimSpace(req.UI.TideProvider)
	normalized.UI.TideStationID = strings.TrimSpace(req.UI.TideStationID)
	normalized.UI.TideStationName = strings.TrimSpace(req.UI.TideStationName)
	normalized.UI.TideAutoStation = req.UI.TideAutoStation
	normalized.UI.WeatherProvider = strings.TrimSpace(req.UI.WeatherProvider)
	normalized.UI.WaveProvider = strings.TrimSpace(req.UI.WaveProvider)
	normalized.UI.POIProvider = strings.TrimSpace(req.UI.POIProvider)
	normalized.UI.PlaceNameProvider = strings.TrimSpace(req.UI.PlaceNameProvider)
	normalized.UI.ForecastWarningsProvider = strings.TrimSpace(req.UI.ForecastWarningsProvider)

	normalized.Anchor.BowRollerHeightM = req.Anchor.BowRollerHeightM
	if normalized.Anchor.BowRollerHeightM <= 0 {
		normalized.Anchor.BowRollerHeightM = defaultBowRollerHeightM
	}
	normalized.Anchor.ChainSizeMM = req.Anchor.ChainSizeMM
	if normalized.Anchor.ChainSizeMM <= 0 {
		normalized.Anchor.ChainSizeMM = defaultChainSizeMM
	}
	normalized.Anchor.ChainOnboardM = req.Anchor.ChainOnboardM
	if normalized.Anchor.ChainOnboardM <= 0 {
		normalized.Anchor.ChainOnboardM = defaultChainOnboardM
	}
	normalized.Anchor.WindageAreaM2 = req.Anchor.WindageAreaM2
	if normalized.Anchor.WindageAreaM2 <= 0 {
		normalized.Anchor.WindageAreaM2 = defaultWindageAreaM2
	}
	normalized.Anchor.HullType = strings.TrimSpace(req.Anchor.HullType)
	if !isSupportedHullType(normalized.Anchor.HullType) {
		normalized.Anchor.HullType = defaultHullType
	}
	normalized.Anchor.ScopeMethod = strings.TrimSpace(req.Anchor.ScopeMethod)
	if !isSupportedScopeMethod(normalized.Anchor.ScopeMethod) {
		normalized.Anchor.ScopeMethod = defaultScopeMethod
	}
	// gps_from_bow_m and loa_m assign straight through, with no positive-default
	// clamp: both default to 0 ("no correction" / "not entered"), and 0 must
	// survive the round-trip rather than being replaced by a guessed value. A
	// negative submission is still nonsensical, so floor it at 0.
	normalized.Anchor.GPSFromBowM = req.Anchor.GPSFromBowM
	if normalized.Anchor.GPSFromBowM < 0 {
		normalized.Anchor.GPSFromBowM = 0
	}
	normalized.Anchor.LOAM = req.Anchor.LOAM
	if normalized.Anchor.LOAM < 0 {
		normalized.Anchor.LOAM = 0
	}
	// Straight passthrough, deliberately with no override in either
	// direction - see the comment on the field itself for why this one
	// field can't use the "default when invalid/absent" pattern every other
	// field in this function does.
	normalized.Anchor.AutoRaiseOnMotoring = req.Anchor.AutoRaiseOnMotoring
	// MinClearanceAtLowM (ADR 0135): unlike BowRollerHeightM/ChainSizeMM/
	// WindageAreaM2 above, zero is not floored away here - it is a valid,
	// deliberate margin (warn only once the keel is at the seabed). Only a
	// negative or non-finite submission gets replaced by the default;
	// "absent" is handled separately, by buildSettingsPayload's disk-presence
	// check, for the same reason AutoRaiseOnMotoring's default lives there.
	normalized.Anchor.MinClearanceAtLowM = req.Anchor.MinClearanceAtLowM
	if normalized.Anchor.MinClearanceAtLowM < 0 || math.IsNaN(normalized.Anchor.MinClearanceAtLowM) || math.IsInf(normalized.Anchor.MinClearanceAtLowM, 0) {
		normalized.Anchor.MinClearanceAtLowM = defaultMinClearanceAtLowM
	}

	normalized.Influxdb.Enabled = req.Influxdb.Enabled
	normalized.Influxdb.URL = strings.TrimSpace(req.Influxdb.URL)
	normalized.Influxdb.Org = strings.TrimSpace(req.Influxdb.Org)
	normalized.Influxdb.Bucket = strings.TrimSpace(req.Influxdb.Bucket)

	// Unlike Signalk.Address just above, a blank mayara address stays blank
	// rather than defaulting to a host: there is no sane address to guess for
	// a radar, and an unconfigured mayara address is exactly how the radar
	// picture overlay reports itself as off (plan: "Backend" section).
	normalized.Mayara.Address = strings.TrimSpace(req.Mayara.Address)
	normalized.Mayara.Port = req.Mayara.Port
	if normalized.Mayara.Port <= 0 || normalized.Mayara.Port > 65535 {
		normalized.Mayara.Port = defaultMayaraPort
	}

	normalized.Assistant.Enabled = req.Assistant.Enabled
	normalized.Assistant.Model = strings.TrimSpace(req.Assistant.Model)
	if normalized.Assistant.Model == "" {
		normalized.Assistant.Model = defaultAssistantModel
	}
	normalized.Assistant.DocumentModel = strings.TrimSpace(req.Assistant.DocumentModel)
	if normalized.Assistant.DocumentModel == "" {
		normalized.Assistant.DocumentModel = defaultDocumentModel
	}
	// Trimmed, but deliberately NOT defaulted the way Model and
	// DocumentModel are just above. A blank embedding model is the
	// documented off switch for semantic search (ADR 0108), so defaulting it
	// here would mean an operator could never turn it off through the
	// settings API, and that the first save after an upgrade would silently
	// switch paid embedding on for every consented document. The default for
	// an ABSENT key is applied by buildSettingsPayload instead - the same
	// split Anchor.AutoRaiseOnMotoring already uses, and for the same reason:
	// this function also normalises genuine save requests, where it cannot
	// tell "the operator cleared the field" from "the zero-value baseline".
	normalized.Assistant.EmbeddingModel = strings.TrimSpace(req.Assistant.EmbeddingModel)
	normalized.Assistant.EmbeddingDimensions = req.Assistant.EmbeddingDimensions
	if normalized.Assistant.EmbeddingDimensions <= 0 {
		normalized.Assistant.EmbeddingDimensions = defaultEmbeddingDimensions
	}
	// A larger vector is a configuration mistake, not a preference: the
	// whole index is scanned in Go on an armv7 box, and every extra
	// dimension is work multiplied by however many chunks are in the
	// library. Capped, not reset to the default, so an operator dialling
	// dimensions up for accuracy lands at the ceiling rather than being
	// bounced back down to 512.
	if normalized.Assistant.EmbeddingDimensions > 4096 {
		normalized.Assistant.EmbeddingDimensions = 4096
	}
	normalized.Assistant.Notes = strings.TrimSpace(req.Assistant.Notes)
	normalized.Assistant.AllowedModels = normalizeAssistantModelPatterns(req.Assistant.AllowedModels)
	normalized.Assistant.ExcludedModels = normalizeAssistantModelPatterns(req.Assistant.ExcludedModels)
	normalized.Assistant.CostTier = normalizeAssistantCostTier(req.Assistant.CostTier)
	normalized.Assistant.VoiceInput = req.Assistant.VoiceInput
	normalized.Assistant.ReadAloud = req.Assistant.ReadAloud
	normalized.Assistant.WakeWord = req.Assistant.WakeWord
	normalized.Assistant.VerboseRunLog = req.Assistant.VerboseRunLog
	normalized.Assistant.WebSearch = req.Assistant.WebSearch
	normalized.Assistant.WebSearchModel = strings.TrimSpace(req.Assistant.WebSearchModel)
	normalized.Assistant.PassageLimits = req.Assistant.PassageLimits

	normalized.OfflineCoastline.Enabled = req.OfflineCoastline.Enabled

	normalized.Auth.Mode = strings.TrimSpace(req.Auth.Mode)
	if normalized.Auth.Mode == "" {
		normalized.Auth.Mode = authModeNone
	}

	if req.Roles != nil {
		roles := normalizeVesselRoles(*req.Roles)
		normalized.Roles = &roles
	}

	normalized.Units = strings.ToLower(strings.TrimSpace(req.Units))
	if normalized.Units != "metric" && normalized.Units != "imperial" {
		normalized.Units = defaultDistanceUnits
	}

	return normalized
}

func isSupportedHullType(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed == "power_cat" || trimmed == "sail_mono" || trimmed == "power_mono" || trimmed == "sail_cat"
}

func isSupportedScopeMethod(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed == "catenary" || trimmed == "ratio"
}

func normalizeAssistantModelPatterns(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return []string{}
	}
	return out
}

func normalizeAssistantCostTier(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "low", "medium", "high", "xhigh", "max":
		return strings.TrimSpace(strings.ToLower(value))
	default:
		return ""
	}
}

func coerceStringList(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(coerceString(item))
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return []string{}
	}
	return out
}

func getSignalKSettingsHandler(c echo.Context) error {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}

	return c.JSON(http.StatusOK, map[string]any{
		"address": address,
		"port":    port,
		"url":     buildSignalKURL(address, port),
	})
}

// testSignalKConnectionHandler backs the "Test Connection" button. It is a
// pure read: it probes the address in the request body and reports what it
// found, without touching settings.yaml.
//
// Its predecessor (POST /api/settings/signalk) both probed and persisted,
// which gave the SignalK address two independent write paths — the bulk save
// and this one — and meant a button labelled "Connect" quietly rewrote
// config as a side effect of a connectivity check. Persisting is now solely
// the bulk save's job, where validateSettingsChange applies the same probe
// (ADR 0028). Because nothing here writes, an operator can check an address
// before committing to it, which the old behaviour made impossible.
func testSignalKConnectionHandler(c echo.Context) error {
	var req struct {
		Address string `json:"address"`
		Port    int    `json:"port"`
	}

	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
	}

	// No defaulting here, unlike the old handler: silently probing
	// "localhost" when the field is blank reports a connectivity failure for
	// an address the operator never entered, sending them after the wrong
	// problem. Blank input is input error, and says so.
	address := strings.TrimSpace(req.Address)
	if address == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"field": "signalk.address",
			"error": "enter a SignalK address to test",
		})
	}

	if req.Port <= 0 || req.Port > 65535 {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"field": "signalk.port",
			"error": fmt.Sprintf("port %d is out of range (1-65535)", req.Port),
		})
	}

	signalkURL := buildSignalKURL(address, req.Port)
	vesselPath := getEnv("SIGNALK_VESSEL_PATH", "/signalk/v1/api/vessels/self")

	// A direct REST probe, not the delta-stream snapshot: the address under
	// test is not the configured one and has no stream open, so the snapshot
	// would answer for the currently configured server no matter what was typed.
	payload, err := probeSignalKTree(signalkURL, vesselPath)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]any{
			"field":     "signalk.address",
			"error":     fmt.Sprintf("unable to connect to SignalK at %s", signalkURL),
			"connected": false,
		})
	}

	// The vessel name is the useful half of the answer: "something responded"
	// does not distinguish your boat from a neighbour's server on the same
	// marina wifi, and this is the one place that distinction can be shown.
	return c.JSON(http.StatusOK, map[string]any{
		"connected":   true,
		"url":         signalkURL,
		"vessel_name": strings.TrimSpace(firstNonEmptyString(lookupString(payload, "name"), lookupString(payload, "design", "name"))),
	})
}

// settingsWriteMu serialises every read-modify-write of settings.yaml: the
// settings save, the tide station auto-update and the role clear on an item
// delete. Without it, two of them read the same file and the later write
// silently undoes the earlier one. Hold it from the read to the write.
var settingsWriteMu sync.Mutex

func writeSettings(settingsPath string, settings map[string]any) error {

	content, err := yaml.Marshal(settings)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}

	if err := os.WriteFile(settingsPath, content, 0o644); err != nil {
		return err
	}

	// Cached bytes-and-mtime alone would not reliably catch a rewrite that
	// lands on the same size within the filesystem's mtime resolution, so
	// the write path forces a miss explicitly rather than trusting that.
	invalidateSettingsCache(settingsPath)
	return nil
}

// readSettings reads and YAML-parses settingsPath, serving a deep copy of a
// cached parse (settings_cache.go) when the file's mtime and size have not
// changed since it was last parsed. Stat/read errors surface exactly as
// they did before caching existed: a missing file returns an empty map with
// no error (a fresh install's normal state), any other stat or read error
// returns nil and that error.
func readSettings(settingsPath string) (map[string]any, error) {
	info, err := os.Stat(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			invalidateSettingsCache(settingsPath)
			return map[string]any{}, nil
		}
		return nil, err
	}

	if cached, ok := cachedSettingsFor(settingsPath, info); ok {
		return cached, nil
	}

	content, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}

		return nil, err
	}

	settings := map[string]any{}
	if len(content) > 0 {
		if err := yaml.Unmarshal(content, &settings); err != nil {
			return nil, err
		}
	}

	// Cache against the Stat result taken before the read, not a fresh
	// stat after: if the file changed in between, the next call's own Stat
	// will simply miss this entry and re-parse, which is correct either way.
	storeSettingsCache(settingsPath, info, settings)
	return deepCopyMap(settings), nil
}

func coercePort(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		var parsed int
		if _, err := fmt.Sscanf(strings.TrimSpace(typed), "%d", &parsed); err == nil {
			return parsed
		}
	}

	return 0
}

func coerceString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	}

	return ""
}

func coerceFloat(value any) float64 {
	switch typed := value.(type) {
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case float64:
		return typed
	case string:
		var parsed float64
		if _, err := fmt.Sscanf(strings.TrimSpace(typed), "%f", &parsed); err == nil {
			return parsed
		}
	}

	return -1
}

// loadSettingString retrieves a string value from nested settings by path (e.g., []string{"boat", "name"}).
// Returns empty string on error or missing path.
func loadSettingString(settingsPath string, keyPath []string) string {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return ""
	}

	value := any(settings)
	for _, key := range keyPath {
		m, ok := value.(map[string]any)
		if !ok {
			return ""
		}
		value = m[key]
	}

	return strings.TrimSpace(coerceString(value))
}

// loadSettingFloat retrieves a float64 value from nested settings by path.
// Returns -1 on error or missing path.
func loadSettingFloat(settingsPath string, keyPath []string) float64 {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return -1
	}

	value := any(settings)
	for _, key := range keyPath {
		m, ok := value.(map[string]any)
		if !ok {
			return -1
		}
		value = m[key]
	}

	return coerceFloat(value)
}

// settingsRoutes returns the /api routes for the admin settings document.
// Each row carries its own auth tier (docs/adr/0177).
func settingsRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodGet, "/api/settings", tierAdmin, getSettingsHandler},
		{http.MethodPost, "/api/settings", tierAdmin, updateSettingsHandler},

		// Settings -> SignalK: discovery is a pure read
		// of live instances for the picker. The vessel.* block itself
		// loads and saves through /api/settings.
		{http.MethodGet, "/api/vessel/discovery", tierAdmin, getVesselDiscoveryHandler},
	}
}
