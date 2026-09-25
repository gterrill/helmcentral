package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

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
	// defaultMayaraPort is mayara-server's own default REST/WebSocket port.
	// Used only to clamp an out-of-range settings.mayara.port; unlike
	// defaultSignalKAddress there is no equivalent default *address* — see
	// normalizeSettingsPayload.
	defaultMayaraPort = 6502

	// signalKSelfAPIPath is the v1 REST prefix every read and write of the
	// self vessel's data model goes through. Reads default to it through
	// SIGNALK_VESSEL_PATH; writes (czone switches, generatorPut, alarm
	// notifications) hard-code the same prefix, because a PUT to a path
	// outside it reaches no endpoint the server serves.
	signalKSelfAPIPath = "/signalk/v1/api/vessels/self"

	// defaultSignalKReadTimeoutMS bounds every read of SignalK state (vessel,
	// electrical, tanks, nearby vessels, self name, connection probe).
	// Overridable via SIGNALK_READ_TIMEOUT_MS - the boat's server is on the
	// LAN, but a deployment reaching it over a slow link may need more, and
	// tests probing a deliberately unroutable address want far less than a
	// 3s wall-clock wait per case.
	defaultSignalKReadTimeoutMS = 3000
)

// signalKReadTimeout resolves the SignalK read timeout from
// SIGNALK_READ_TIMEOUT_MS, falling back to defaultSignalKReadTimeoutMS on an
// unset or invalid value (logged loudly rather than refusing to talk to the
// vessel over a malformed env var). Mirrors wasmPluginTimeoutMS.
func signalKReadTimeout() time.Duration {
	raw := getEnv("SIGNALK_READ_TIMEOUT_MS", strconv.Itoa(defaultSignalKReadTimeoutMS))
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		log.Printf("signalk: invalid SIGNALK_READ_TIMEOUT_MS %q, falling back to %dms", raw, defaultSignalKReadTimeoutMS)
		return defaultSignalKReadTimeoutMS * time.Millisecond
	}
	return time.Duration(ms) * time.Millisecond
}

// signalKReadClient is the HTTP client for every SignalK state read. Callers
// share one timeout so a single knob covers the whole read path; the client is
// built per call because the timeout is resolved at call time.
func signalKReadClient() *http.Client {
	return &http.Client{Timeout: signalKReadTimeout()}
}

type settingsPayload struct {
	Signalk struct {
		Address string `json:"address"`
		Port    int    `json:"port"`
	} `json:"signalk"`
	Boat struct {
		VesselPrefix           string  `json:"vessel_prefix"`
		Model                  string  `json:"model"`
		HouseBatteryCapacityAh float64 `json:"house_battery_capacity_ah"`
	} `json:"boat"`
	UI struct {
		TankLabels      map[string]string `json:"tank_labels"`
		TideProvider    string            `json:"tide_provider"`
		TideStationID   string            `json:"tide_station_id"`
		TideStationName string            `json:"tide_station_name"`
		TideAutoStation bool              `json:"tide_auto_station"`
		WeatherProvider string            `json:"weather_provider"`
		WaveProvider    string            `json:"wave_provider"`
		POIProvider     string            `json:"poi_provider"`
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
	} `json:"assistant"`
	Auth struct {
		Mode string `json:"mode"`
	} `json:"auth"`
	Units string `json:"units"`
}

func getSettingsHandler(c echo.Context) error {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	settings, err := readSettings(settingsPath)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read settings"})
	}

	return c.JSON(http.StatusOK, buildSettingsPayload(settings))
}

func updateSettingsHandler(c echo.Context) error {
	var req settingsPayload
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
	}

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	settings, err := readSettings(settingsPath)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read settings"})
	}

	normalized := normalizeSettingsPayload(req)
	current := buildSettingsPayload(settings)

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
		"vessel_prefix":             normalized.Boat.VesselPrefix,
		"model":                     normalized.Boat.Model,
		"house_battery_capacity_ah": normalized.Boat.HouseBatteryCapacityAh,
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
	if normalized.UI.TankLabels != nil {
		uiMap["tank_labels"] = normalized.UI.TankLabels
	}
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
	}
	settings["units"] = normalized.Units

	if err := writeSettings(settingsPath, settings); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to save settings"})
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
	// The true default lives here, not in normalizeSettingsPayload's
	// baseline call above: that function also normalizes genuine save
	// requests, where a bare bool can't tell "the operator submitted false"
	// from "the zero-value baseline." Set explicitly here, before the disk
	// overlay below, so an absent key on disk surfaces as true and only an
	// explicit stored value (true or false) overrides it.
	payload.Anchor.AutoRaiseOnMotoring = true
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
		capacity := coerceFloat(boatMap["house_battery_capacity_ah"])
		if capacity > 0 {
			payload.Boat.HouseBatteryCapacityAh = capacity
		}
	}

	if uiMap, ok := settings["ui"].(map[string]any); ok {
		if labelsMap, ok := uiMap["tank_labels"].(map[string]any); ok {
			payload.UI.TankLabels = map[string]string{}
			for key, value := range labelsMap {
				trimmedKey := strings.TrimSpace(key)
				if trimmedKey == "" {
					continue
				}
				payload.UI.TankLabels[trimmedKey] = strings.TrimSpace(coerceString(value))
			}
		}

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
	normalized.Boat.HouseBatteryCapacityAh = req.Boat.HouseBatteryCapacityAh
	if normalized.Boat.HouseBatteryCapacityAh <= 0 {
		normalized.Boat.HouseBatteryCapacityAh = defaultHouseBatteryCapacityAh
	}

	if req.UI.TankLabels != nil {
		normalized.UI.TankLabels = map[string]string{}
		for key, value := range req.UI.TankLabels {
			trimmedKey := strings.TrimSpace(key)
			if trimmedKey == "" {
				continue
			}
			normalized.UI.TankLabels[trimmedKey] = strings.TrimSpace(value)
		}
	}

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

	normalized.Auth.Mode = strings.TrimSpace(req.Auth.Mode)
	if normalized.Auth.Mode == "" {
		normalized.Auth.Mode = authModeNone
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

// criticalVesselState marks state's position/GNSS fields as critical when
// there's no SignalK payload to validate at all — e.g. the connection itself
// failed. It freezes position at the last trusted fix (via resolveGNSSPosition)
// rather than leaving it at a meaningless sentinel, and reports
// gnss_critical_alert correctly so downstream consumers like the anchor watch
// alarm don't mistake "can't reach SignalK" for "vessel has moved."
func criticalVesselState(state vesselStateData, reason string) vesselStateData {
	validation := criticalGNSSValidation(reason, time.Now().UTC())
	state.Latitude, state.Longitude = resolveGNSSPosition(-1, -1, validation)
	state.GNSSQualityIndicator = validation.QualityIndicator
	state.GNSSHDOP = validation.HDOP
	state.GNSSSatellites = validation.Satellites
	state.GNSSValidationState = validation.Status
	state.GNSSValidationReason = validation.Reason
	state.GNSSCriticalAlert = validation.Critical
	return state
}

func fetchSignalKVesselState() (vesselStateData, error) {
	state := vesselStateData{
		Status: "Unknown", Datetime: time.Now().UTC(), Depth: -1, LengthOverallM: -1, Latitude: -1, Longitude: -1,
		HeadingTrue: -1, SpeedOverGroundKts: -1, WindSpeedApparentKts: -1, WindAngleApparentDeg: -1, WindAngleRelativeDeg: -1,
		WindSpeedTrueKts: -1, WindDirectionTrueDeg: -1,
		// Every last_update_age_s field defaults to -1 (unknown), not 0: a
		// return before the payload is even parsed (signalKSelfPayload
		// failing below) must never read as "just measured".
		DepthLastUpdateAge: -1, PositionLastUpdateAge: -1, WindLastUpdateAge: -1,
	}

	// A stream outage lands here the same way an unreachable REST server does,
	// so the position freezes at the last trusted fix rather than reading as a
	// jump to null island and tripping the anchor alarm.
	payload, err := signalKSelfPayload()
	if err != nil {
		return criticalVesselState(state, err.Error()), err
	}

	state.Name = strings.TrimSpace(firstNonEmptyString(lookupString(payload, "name"), lookupString(payload, "design", "name")))

	// ADR 0047: the Rode Planner's swing radius needs the boat's LOA. The
	// REST full tree nests it under a "value" wrapper; some sources publish
	// the bare path instead - same two-step pattern as environment.depth
	// below.
	state.LengthOverallM = lookupNumber(payload, "design", "length", "value", "overall")
	if state.LengthOverallM == -1 {
		state.LengthOverallM = lookupNumber(payload, "design", "length", "overall")
	}

	state.Status = firstNonEmptyString(lookupString(payload, "navigation", "state", "value"), lookupString(payload, "navigation", "state"))
	if state.Status == "" {
		state.Status = "Unknown"
	}

	hasGNSSDatetime := false
	datetimeString := firstNonEmptyString(lookupString(payload, "navigation", "datetime", "value"), lookupString(payload, "navigation", "datetime"), lookupString(payload, "timestamp"))
	if datetimeString != "" {
		parsed, err := time.Parse(time.RFC3339, datetimeString)
		if err == nil {
			state.Datetime = parsed.UTC()
			hasGNSSDatetime = true
		}
	}

	state.Depth = lookupNumber(payload, "environment", "depth", "belowTransducer", "value")
	if state.Depth == -1 {
		state.Depth = lookupNumber(payload, "environment", "depth", "belowTransducer")
	}
	// Scoped to belowTransducer alone, not the whole environment.depth
	// branch: a live belowSurface/belowKeel sensor must never paper over a
	// dead belowTransducer reading, which is the value actually rendered.
	state.DepthLastUpdateAge = freshestTimestampAge(lookupAnyMap(payload, "environment", "depth", "belowTransducer"), state.Datetime)

	currentDriftKts, currentSetDeg, currentDriftImpactKts := parseSignalKCurrent(payload)
	state.CurrentDriftKts = currentDriftKts
	state.CurrentSetDeg = currentSetDeg
	state.CurrentDriftImpactKts = currentDriftImpactKts

	rawLatitude := lookupNumber(payload, "navigation", "position", "value", "latitude")
	if rawLatitude == -1 {
		rawLatitude = lookupNumber(payload, "navigation", "position", "latitude")
	}

	rawLongitude := lookupNumber(payload, "navigation", "position", "value", "longitude")
	if rawLongitude == -1 {
		rawLongitude = lookupNumber(payload, "navigation", "position", "longitude")
	}

	state.Engine0RPM = readEngineRPM(payload, []string{"0", "port", "main", "engine0", "engine-0"})
	state.Engine1RPM = readEngineRPM(payload, []string{"1", "starboard", "secondary", "engine1", "engine-1"})
	validation := parseGNSSPositionValidation(payload)
	validation = applyGNSSHeuristics(validation, gnssObservedSample{
		Latitude:       rawLatitude,
		Longitude:      rawLongitude,
		DepthMeters:    state.Depth,
		Navigation:     state.Status,
		EnginesRunning: (state.Engine0RPM > 0 && !math.IsInf(state.Engine0RPM, 0)) || (state.Engine1RPM > 0 && !math.IsInf(state.Engine1RPM, 0)),
		ObservedAt:     state.Datetime,
		HasObservedAt:  hasGNSSDatetime,
	}, time.Now().UTC())
	state.GNSSQualityIndicator = validation.QualityIndicator
	state.GNSSHDOP = validation.HDOP
	state.GNSSSatellites = validation.Satellites
	state.GNSSValidationState = validation.Status
	state.GNSSValidationReason = validation.Reason
	state.GNSSCriticalAlert = validation.Critical

	state.Latitude, state.Longitude = resolveGNSSPosition(rawLatitude, rawLongitude, validation)

	// The fix (navigation.position) and its quality figures
	// (navigation.gnss) are published under two different parents; either
	// one still ticking means the fix itself is current, so this is the
	// freshest of the two rather than a single subtree walk.
	state.PositionLastUpdateAge = freshestAge(
		freshestTimestampAge(lookupAnyMap(payload, "navigation", "position"), state.Datetime),
		freshestTimestampAge(lookupAnyMap(payload, "navigation", "gnss"), state.Datetime),
	)

	state.HeadingTrue = lookupNumber(payload, "navigation", "headingTrue", "value")
	if state.HeadingTrue == -1 {
		state.HeadingTrue = lookupNumber(payload, "navigation", "headingTrue")
	}

	if state.HeadingTrue >= 0 {
		if state.HeadingTrue <= 2*math.Pi {
			state.HeadingTrue = state.HeadingTrue * 180 / math.Pi
		}
		state.HeadingTrue = normalizeDegrees(state.HeadingTrue)
	}

	windSpeedApparent := lookupNumber(payload, "environment", "wind", "speedApparent", "value")
	if windSpeedApparent == -1 {
		windSpeedApparent = lookupNumber(payload, "environment", "wind", "speedApparent")
	}
	windTimestamp := firstNonEmptyString(lookupString(payload, "environment", "wind", "speedApparent", "timestamp"), lookupString(payload, "environment", "wind", "angleApparent", "timestamp"), lookupString(payload, "environment", "wind", "timestamp"))

	// Scoped to environment.wind only: environment.current is a different
	// sensor with its own health and must not be folded into wind's own
	// freshness. Whole-subtree walk (not windTimestamp's first-found
	// priority list above) so the tile's age tracks whichever of
	// speedApparent/angleApparent actually updated most recently.
	state.WindLastUpdateAge = freshestTimestampAge(lookupAnyMap(payload, "environment", "wind"), state.Datetime)

	windDataRecent := isRecentTimestamp(windTimestamp, defaultWindMaxAge)
	if !windDataRecent {
		windDataRecent = state.Datetime.After(time.Now().UTC().Add(-defaultWindMaxAge))
	}

	if windSpeedApparent >= 0 && windDataRecent {
		state.WindSpeedApparentKts = windSpeedApparent * metersPerSecondToKnots
	} else {
		state.WindSpeedApparentKts = 0
	}

	windAngleApparent := lookupNumber(payload, "environment", "wind", "angleApparent", "value")
	if windAngleApparent == -1 {
		windAngleApparent = lookupNumber(payload, "environment", "wind", "angleApparent")
	}
	if windAngleApparent != -1 && windDataRecent {
		if windAngleApparent >= -2*math.Pi && windAngleApparent <= 2*math.Pi {
			windAngleApparent = windAngleApparent * 180 / math.Pi
		}

		signedAngle := normalizeSignedDegrees(windAngleApparent)
		state.WindAngleApparentDeg = normalizeDegrees(signedAngle)
		state.WindAngleRelativeDeg = math.Abs(signedAngle)
		if signedAngle < 0 {
			state.WindSide = "port"
		} else {
			state.WindSide = "starboard"
		}
	} else {
		state.WindAngleApparentDeg = 0
		state.WindAngleRelativeDeg = 0
		state.WindSide = "starboard"
	}

	// True wind (ADR 0129): the Current Conditions tile's readout, from the
	// live server's own derived-data speedTrue/directionTrue rather than
	// computed here from apparent + heading/SOG. Absent (-1), never the
	// apparent figure, when unpublished or stale (AGENTS.md fallback
	// policy) - unlike WindSpeedApparentKts above, which defaults to 0
	// because "no apparent wind" is itself a real reading; "no true wind
	// source" is not.
	//
	// Gated on each field's OWN timestamp, not windDataRecent: windDataRecent
	// is apparent wind's freshness signal (keyed off speedApparent/
	// angleApparent, falling back to state.Datetime - effectively always
	// "recent" - when apparent carries no timestamp of its own either). The
	// derived-data source computing speedTrue/directionTrue can stop
	// updating while the anemometer keeps publishing a fresh apparent
	// reading right next to it; gating true wind on apparent's freshness
	// would then keep showing a frozen true-wind number as if it were
	// current. No fallback to state.Datetime here either: a missing or
	// unparseable timestamp on the field itself means absent, full stop.
	windSpeedTrueRecent := isRecentTimestamp(lookupString(payload, "environment", "wind", "speedTrue", "timestamp"), defaultWindMaxAge)
	windSpeedTrue := lookupNumber(payload, "environment", "wind", "speedTrue", "value")
	if windSpeedTrue == -1 {
		windSpeedTrue = lookupNumber(payload, "environment", "wind", "speedTrue")
	}
	if windSpeedTrue >= 0 && windSpeedTrueRecent {
		state.WindSpeedTrueKts = windSpeedTrue * metersPerSecondToKnots
	} else {
		state.WindSpeedTrueKts = -1
	}

	windDirectionTrueRecent := isRecentTimestamp(lookupString(payload, "environment", "wind", "directionTrue", "timestamp"), defaultWindMaxAge)
	windDirectionTrue := lookupNumber(payload, "environment", "wind", "directionTrue", "value")
	if windDirectionTrue == -1 {
		windDirectionTrue = lookupNumber(payload, "environment", "wind", "directionTrue")
	}
	if windDirectionTrue != -1 && windDirectionTrueRecent {
		if windDirectionTrue >= -2*math.Pi && windDirectionTrue <= 2*math.Pi {
			windDirectionTrue = windDirectionTrue * 180 / math.Pi
		}
		state.WindDirectionTrueDeg = normalizeDegrees(windDirectionTrue)
	} else {
		state.WindDirectionTrueDeg = -1
	}

	sog := lookupNumber(payload, "navigation", "speedOverGround", "value")
	if sog == -1 {
		sog = lookupNumber(payload, "navigation", "speedOverGround")
	}
	if sog >= 0 {
		state.SpeedOverGroundKts = math.Round(sog*metersPerSecondToKnots*10) / 10
	}

	// Generator state (com.victronenergy.generator.0 via signalk-venus-plugin)
	state.GeneratorState = firstNonEmptyString(
		lookupString(payload, "electrical", "generator", "0", "state", "value"),
		lookupString(payload, "electrical", "generator", "0", "state"),
	)

	if v, ok := lookupBool(payload, "electrical", "generator", "0", "manualStart", "value"); ok {
		state.GeneratorManualStart = v
	} else if v, ok := lookupBool(payload, "electrical", "generator", "0", "manualStart"); ok {
		state.GeneratorManualStart = v
	}

	genTimer := lookupNumber(payload, "electrical", "generator", "0", "manualStartTimer", "value")
	if genTimer == -1 {
		genTimer = lookupNumber(payload, "electrical", "generator", "0", "manualStartTimer")
	}
	if genTimer >= 0 {
		state.GeneratorManualStartTimer = genTimer
	}

	state.GeneratorRunningByCondition = firstNonEmptyString(
		lookupString(payload, "electrical", "generator", "0", "runningByCondition", "value"),
		lookupString(payload, "electrical", "generator", "0", "runningByCondition"),
	)

	genRuntime := lookupNumber(payload, "electrical", "generator", "0", "runtime", "value")
	if genRuntime == -1 {
		genRuntime = lookupNumber(payload, "electrical", "generator", "0", "runtime")
	}
	if genRuntime >= 0 {
		state.GeneratorRuntime = genRuntime
	}

	return state, nil
}

func readEngineRPM(payload map[string]any, aliases []string) float64 {
	for _, alias := range aliases {
		ts := firstNonEmptyString(
			lookupString(payload, "propulsion", alias, "revolutions", "timestamp"),
			lookupString(payload, "propulsion", alias, "rpm", "timestamp"),
			lookupString(payload, "propulsion", alias, "timestamp"),
		)
		if ts != "" && !isRecentTimestamp(ts, defaultRPMMaxAge) {
			return -1
		}

		revPerSecond := lookupFirstNumber(payload,
			[]string{"propulsion", alias, "revolutions", "value"},
			[]string{"propulsion", alias, "revolutions"},
		)
		if revPerSecond >= 0 {
			return roundTo1(revPerSecond * 60)
		}

		rpm := lookupFirstNumber(payload,
			[]string{"propulsion", alias, "rpm", "value"},
			[]string{"propulsion", alias, "rpm"},
		)
		if rpm >= 0 {
			return roundTo1(rpm)
		}
	}

	return -1
}

func parseSignalKCurrent(payload map[string]any) (float64, float64, *float64) {
	current := lookupAnyMap(payload, "environment", "current")
	if current == nil {
		return -1, -1, nil
	}

	drift := lookupNumber(current, "drift", "value")
	if drift == -1 {
		drift = lookupNumber(current, "drift")
	}
	if drift == -1 {
		drift = lookupFirstNumber(current,
			[]string{"drift", "speed"},
			[]string{"drift", "speed", "value"},
			[]string{"drift", "value", "speed"},
		)
	}
	if drift >= 0 && drift <= 20 {
		drift = math.Round((drift*metersPerSecondToKnots)*10) / 10
	} else if drift < 0 {
		drift = -1
	}

	// setTrue is the official SignalK path (environment.current has no plain
	// "set" in the spec). setMagnetic is deliberately NOT read as a stand-in
	// for setTrue: converting it would need a magnetic variation reading
	// this codebase doesn't have, and displaying an unconverted magnetic
	// bearing as true would silently misreport direction.
	setDeg := lookupNumber(current, "setTrue", "value")
	if setDeg == -1 {
		setDeg = lookupNumber(current, "setTrue")
	}
	if setDeg == -1 {
		setDeg = lookupFirstNumber(current,
			[]string{"drift", "direction"},
			[]string{"drift", "direction", "value"},
			[]string{"drift", "angle"},
			[]string{"drift", "angle", "value"},
		)
	}
	// Check the not-found sentinel before the radian/degree heuristic below:
	// -1 falls inside a valid radian range ([-2pi, 2pi]), so checking ranges
	// first would silently reinterpret "no data" as a real angle of -1 rad
	// (~303 deg once normalized) instead of reporting no data.
	if setDeg == -1 {
		// leave as -1 (no data)
	} else if setDeg >= 0 && setDeg <= 2*math.Pi {
		setDeg = normalizeDegrees(setDeg * 180 / math.Pi)
	} else {
		setDeg = normalizeDegrees(setDeg)
	}

	var driftImpactKts *float64
	if lookupAnyMap(current, "driftImpact") != nil {
		if value, ok := current["driftImpact"].(map[string]any)["value"].(float64); ok {
			knots := roundTo1(value * metersPerSecondToKnots)
			driftImpactKts = &knots
		}
	}

	return drift, setDeg, driftImpactKts
}

func fetchSignalKElectricalState() (electricalStateData, error) {
	state := electricalStateData{Datetime: time.Now().UTC(), LastUpdateAge: -1, BatterySocPercent: -1, BatteryCapacityAh: -1, ChargingCurrentA: -1, ChargingPowerW: -1, SolarOutputW: -1, ACOutputW: -1, DC12VPowerW: -1, DC12VCurrentA: -1, DC24VVoltageV: -1, ACLoadsW: -1, Charger0: chargerInstanceData{CurrentA: -1, ACIn1CurrentA: -1}}

	payload, err := signalKSelfPayload()
	if err != nil {
		return state, err
	}

	datetimeString := firstNonEmptyString(lookupString(payload, "timestamp"), lookupString(payload, "navigation", "datetime", "value"), lookupString(payload, "navigation", "datetime"))
	if datetimeString != "" {
		parsed, parseErr := time.Parse(time.RFC3339, datetimeString)
		if parseErr == nil {
			state.Datetime = parsed.UTC()
		}
	}

	// The tile's values come from paths scattered across the electrical
	// subtree, so its freshness is that of the most recent of them. A feed that
	// has stopped freezes every one of these at once, which is what makes a
	// single subtree-wide age meaningful here.
	state.LastUpdateAge = freshestTimestampAge(lookupAnyMap(payload, "electrical"), state.Datetime)

	// Identify the main house battery before reading any values from it.
	mainBattery := lookupMainBattery(payload)

	soc := -1.0
	if mainBattery != nil {
		soc = lookupNumber(mainBattery, "capacity", "stateOfCharge", "value")
	}
	if soc == -1 {
		soc = lookupFirstNumber(payload,
			[]string{"electrical", "batteries", "house", "capacity", "stateOfCharge", "value"},
			[]string{"electrical", "batteries", "house", "capacity", "stateOfCharge"},
			[]string{"electrical", "batteries", "service", "capacity", "stateOfCharge", "value"},
			[]string{"electrical", "batteries", "service", "capacity", "stateOfCharge"},
		)
	}
	if soc == -1 {
		soc = lookupFirstNumber(payload,
			[]string{"electrical", "chargers", "276", "capacity", "stateOfCharge", "value"},
		)
	}
	if soc == -1 {
		soc = lookupNumberFromAnyChild(payload, []string{"electrical", "chargers"}, []string{"capacity", "stateOfCharge", "value"})
	}
	if soc >= 0 {
		if soc <= 1 {
			soc *= 100
		}
		state.BatterySocPercent = math.Max(0, math.Min(100, roundTo1(soc)))
	}

	batteryCapacityAh := lookupFirstNumber(
		payload,
		[]string{"electrical", "batteries", "house", "capacity", "nominal", "value"},
		[]string{"electrical", "batteries", "house", "capacity", "nominal"},
		[]string{"electrical", "batteries", "service", "capacity", "nominal", "value"},
		[]string{"electrical", "batteries", "service", "capacity", "nominal"},
		[]string{"electrical", "batteries", "house", "capacity", "total", "value"},
		[]string{"electrical", "batteries", "house", "capacity", "total"},
		[]string{"electrical", "batteries", "service", "capacity", "total", "value"},
		[]string{"electrical", "batteries", "service", "capacity", "total"},
	)
	if batteryCapacityAh == -1 {
		batteryCapacityAh = lookupNumberFromAnyChild(payload, []string{"electrical", "batteries"}, []string{"capacity", "nominal", "value"})
	}
	if batteryCapacityAh == -1 {
		batteryCapacityAh = lookupNumberFromAnyChild(payload, []string{"electrical", "batteries"}, []string{"capacity", "total", "value"})
	}
	if batteryCapacityAh == -1 {
		batteryCapacityAh = loadHouseBatteryCapacityAh(getEnv("SETTINGS_FILE", "../settings.yaml"))
	}
	if batteryCapacityAh == -1 {
		batteryCapacityAh = defaultHouseBatteryCapacityAh
	}
	if batteryCapacityAh > 0 {
		state.BatteryCapacityAh = roundTo1(batteryCapacityAh)
	}

	// Identify the main house battery: the one with valid SOC and highest absolute current.
	batteryVoltage := -1.0
	if mainBattery != nil {
		batteryVoltage = lookupNumber(mainBattery, "voltage", "value")
	}
	if batteryVoltage == -1 {
		batteryVoltage = lookupFirstNumber(payload,
			[]string{"electrical", "venus", "batteryVoltage", "value"},
			[]string{"electrical", "venus", "batteryVoltage"},
			[]string{"electrical", "batteries", "house", "voltage", "value"},
			[]string{"electrical", "batteries", "house", "voltage"},
			[]string{"electrical", "batteries", "service", "voltage", "value"},
			[]string{"electrical", "batteries", "service", "voltage"},
		)
	}
	if batteryVoltage == -1 {
		batteryVoltage = lookupNumberFromAnyChild(payload, []string{"electrical", "batteries"}, []string{"voltage", "value"})
	}

	current := -1.0
	if mainBattery != nil {
		current = lookupNumber(mainBattery, "current", "value")
	}
	if current == -1 {
		current = lookupFirstNumber(payload,
			[]string{"electrical", "batteries", "house", "current", "value"},
			[]string{"electrical", "batteries", "house", "current"},
			[]string{"electrical", "batteries", "service", "current", "value"},
			[]string{"electrical", "batteries", "service", "current"},
		)
	}
	if current != -1 {
		state.ChargingCurrentA = roundTo1(current)
	}

	power := -1.0
	if mainBattery != nil {
		power = lookupNumber(mainBattery, "power", "value")
	}
	if power == -1 {
		power = lookupFirstNumber(payload,
			[]string{"electrical", "batteries", "house", "power", "value"},
			[]string{"electrical", "batteries", "house", "power"},
			[]string{"electrical", "batteries", "service", "power", "value"},
			[]string{"electrical", "batteries", "service", "power"},
		)
	}
	if power != -1 {
		state.ChargingPowerW = roundTo1(power)
	} else if state.ChargingCurrentA != -1 && batteryVoltage > 0 {
		state.ChargingPowerW = roundTo1(state.ChargingCurrentA * batteryVoltage)
	}
	if state.ChargingCurrentA == -1 && state.ChargingPowerW != -1 && batteryVoltage > 0 {
		state.ChargingCurrentA = roundTo1(state.ChargingPowerW / batteryVoltage)
	}

	// venus.totalPanelPower is the Victron system aggregate; sum individual chargers as fallback.
	solar := lookupFirstNumber(payload,
		[]string{"electrical", "venus", "totalPanelPower", "value"},
		[]string{"electrical", "venus", "totalPanelPower"},
	)
	if solar == -1 {
		solar = sumNumberFromAllChildren(payload, []string{"electrical", "solar"}, []string{"panelPower", "value"})
	}
	if solar >= 0 {
		state.SolarOutputW = roundTo1(solar)
	}

	acOutput := lookupFirstNumber(payload, []string{"electrical", "inverters", "0", "ac", "output", "power", "value"}, []string{"electrical", "inverters", "0", "ac", "output", "power"}, []string{"electrical", "inverters", "0", "acout", "power", "value"}, []string{"electrical", "inverters", "0", "acout", "power"}, []string{"electrical", "inverters", "0", "acOutputPower", "value"}, []string{"electrical", "inverters", "0", "acOutputPower"}, []string{"electrical", "alternators", "0", "ac", "output", "power", "value"}, []string{"electrical", "alternators", "0", "ac", "output", "power"})
	if acOutput == -1 {
		acOutput = lookupNumberFromAnyChild(payload, []string{"electrical", "inverters"}, []string{"acout", "power", "value"})
	}
	if acOutput >= 0 {
		state.ACOutputW = roundTo1(acOutput)
	}

	dc12Power := lookupFirstNumber(payload, []string{"electrical", "venus", "dcPower", "value"}, []string{"electrical", "venus", "dcPower"}, []string{"electrical", "dc", "12v", "power", "value"}, []string{"electrical", "dc", "12v", "power"}, []string{"electrical", "loads", "12v", "power", "value"}, []string{"electrical", "loads", "12v", "power"})
	if dc12Power >= 0 {
		state.DC12VPowerW = roundTo1(dc12Power)
	}

	dc12Current := lookupFirstNumber(payload, []string{"electrical", "venus", "dcCurrent", "value"}, []string{"electrical", "venus", "dcCurrent"}, []string{"electrical", "dc", "12v", "current", "value"}, []string{"electrical", "dc", "12v", "current"}, []string{"electrical", "loads", "12v", "current", "value"}, []string{"electrical", "loads", "12v", "current"})
	if dc12Current >= 0 {
		state.DC12VCurrentA = roundTo1(dc12Current)
	} else if state.DC12VPowerW >= 0 && batteryVoltage > 0 {
		state.DC12VCurrentA = roundTo1(state.DC12VPowerW / batteryVoltage)
	}

	dc24Voltage := lookupFirstNumber(payload, []string{"electrical", "dc", "24v", "voltage", "value"}, []string{"electrical", "dc", "24v", "voltage"}, []string{"electrical", "batteries", "starter", "voltage", "value"}, []string{"electrical", "batteries", "starter", "voltage"})
	if dc24Voltage >= 0 {
		state.DC24VVoltageV = roundTo1(dc24Voltage)
	} else if batteryVoltage >= 0 {
		state.DC24VVoltageV = roundTo1(batteryVoltage)
	}

	acLoads := lookupFirstNumber(payload, []string{"electrical", "ac", "loads", "total", "power", "value"}, []string{"electrical", "ac", "loads", "total", "power"}, []string{"electrical", "ac", "loads", "power", "value"}, []string{"electrical", "ac", "loads", "power"})
	if acLoads >= 0 {
		state.ACLoadsW = roundTo1(acLoads)
	} else if state.ACOutputW >= 0 {
		state.ACLoadsW = state.ACOutputW
	}

	generatorPower := lookupFirstNumber(payload,
		[]string{"electrical", "ac", "1", "phase", "A", "realPower", "value"},
		[]string{"electrical", "ac", "1", "phaseA", "realPower", "value"},
		[]string{"electrical", "ac", "1", "realPower", "value"},
	)
	if generatorPower >= 0 {
		state.GeneratorRealPowerW = roundTo1(generatorPower)
	}

	// Alternators — read port (index 0) and starboard (index 1) separately.
	state.Alternator0 = readAlternatorInstance(payload, "0")
	state.Alternator1 = readAlternatorInstance(payload, "1")
	state.Charger0 = readChargerInstance(payload, "0")

	return state, nil
}

func fetchSignalKSolarState() (solarStateData, error) {
	state := solarStateData{
		Datetime:      time.Now().UTC(),
		CurrentW:      -1,
		TodayKWh:      -1,
		YesterdayKWh:  -1,
		PeakTodayW:    -1,
		LastUpdateAge: -1,
		Controllers:   []solarControllerData{},
		Trend24hTotal: []solarTrendPoint{},
	}

	payload, err := signalKSelfPayload()
	if err != nil {
		return state, err
	}

	datetimeString := firstNonEmptyString(lookupString(payload, "timestamp"), lookupString(payload, "navigation", "datetime", "value"), lookupString(payload, "navigation", "datetime"))
	if datetimeString != "" {
		parsed, parseErr := time.Parse(time.RFC3339, datetimeString)
		if parseErr == nil {
			state.Datetime = parsed.UTC()
		}
	}

	solarMap := lookupAnyMap(payload, "electrical", "solar")
	if solarMap != nil {
		ids := make([]string, 0, len(solarMap))
		for id := range solarMap {
			ids = append(ids, id)
		}
		sort.Strings(ids)

		sumCurrentW := 0.0
		foundCurrent := false
		sumTodayKWh := 0.0
		foundToday := false
		sumYesterdayKWh := 0.0
		foundYesterday := false

		for i, id := range ids {
			entry, ok := solarMap[id].(map[string]any)
			if !ok {
				continue
			}

			controller := readSolarController(entry, id, i, state.Datetime)
			state.Controllers = append(state.Controllers, controller)

			if controller.CurrentW >= 0 {
				sumCurrentW += controller.CurrentW
				foundCurrent = true
			}
			if controller.TodayKWh >= 0 {
				sumTodayKWh += controller.TodayKWh
				foundToday = true
			}
			if controller.YesterdayKWh >= 0 {
				sumYesterdayKWh += controller.YesterdayKWh
				foundYesterday = true
			}
			// A source is only as stale as its most recent update, so the
			// state-level age tracks the freshest controller rather than the
			// oldest. Controllers reporting no timestamp (-1) cannot vouch for
			// freshness and are skipped.
			if controller.LastUpdateAge >= 0 {
				if state.LastUpdateAge < 0 || controller.LastUpdateAge < state.LastUpdateAge {
					state.LastUpdateAge = controller.LastUpdateAge
				}
			}
		}

		if foundCurrent {
			state.CurrentW = roundTo1(sumCurrentW)
		}
		if foundToday {
			state.TodayKWh = roundTo3(sumTodayKWh)
		}
		if foundYesterday {
			state.YesterdayKWh = roundTo3(sumYesterdayKWh)
		}
	}

	venusCurrent := lookupFirstNumber(payload,
		[]string{"electrical", "venus", "totalPanelPower", "value"},
		[]string{"electrical", "venus", "totalPanelPower"},
	)
	if venusCurrent >= 0 {
		state.CurrentW = roundTo1(venusCurrent)
	}

	venusTodayKWh := normalizeYieldToKWh(lookupFirstNumber(payload,
		[]string{"electrical", "venus", "yieldToday", "value"},
		[]string{"electrical", "venus", "yieldToday"},
		[]string{"electrical", "venus", "dailyYield", "value"},
		[]string{"electrical", "venus", "dailyYield"},
	))
	if venusTodayKWh >= 0 {
		state.TodayKWh = roundTo3(venusTodayKWh)
	}

	venusYesterdayKWh := normalizeYieldToKWh(lookupFirstNumber(payload,
		[]string{"electrical", "venus", "yieldYesterday", "value"},
		[]string{"electrical", "venus", "yieldYesterday"},
		[]string{"electrical", "venus", "dailyYieldYesterday", "value"},
		[]string{"electrical", "venus", "dailyYieldYesterday"},
	))
	if venusYesterdayKWh >= 0 {
		state.YesterdayKWh = roundTo3(venusYesterdayKWh)
	}

	venusPeakW := lookupFirstNumber(payload,
		[]string{"electrical", "venus", "maxPanelPowerToday", "value"},
		[]string{"electrical", "venus", "maxPanelPowerToday"},
	)
	if venusPeakW >= 0 {
		state.PeakTodayW = roundTo1(venusPeakW)
	}

	if state.CurrentW > 0 {
		for i := range state.Controllers {
			if state.Controllers[i].CurrentW >= 0 {
				state.Controllers[i].Contribution = roundTo1((state.Controllers[i].CurrentW / state.CurrentW) * 100)
			}
		}
	}

	return state, nil
}

func readSolarController(entry map[string]any, id string, index int, sampleTime time.Time) solarControllerData {
	controller := solarControllerData{
		ID:            id,
		Label:         defaultSolarControllerLabel(index, id),
		CurrentW:      -1,
		TodayKWh:      -1,
		YesterdayKWh:  -1,
		LastUpdateAge: -1,
		Contribution:  -1,
	}

	powerW := lookupFirstNumber(entry,
		[]string{"panelPower", "value"},
		[]string{"panelPower"},
		[]string{"power", "value"},
		[]string{"power"},
	)
	if powerW >= 0 {
		controller.CurrentW = roundTo1(powerW)
	}

	todayKWh := normalizeYieldToKWh(lookupFirstNumber(entry,
		[]string{"yieldToday", "value"},
		[]string{"yieldToday"},
		[]string{"dailyYield", "value"},
		[]string{"dailyYield"},
	))
	if todayKWh >= 0 {
		controller.TodayKWh = roundTo3(todayKWh)
	}

	yesterdayKWh := normalizeYieldToKWh(lookupFirstNumber(entry,
		[]string{"yieldYesterday", "value"},
		[]string{"yieldYesterday"},
		[]string{"dailyYieldYesterday", "value"},
		[]string{"dailyYieldYesterday"},
	))
	if yesterdayKWh >= 0 {
		controller.YesterdayKWh = roundTo3(yesterdayKWh)
	}

	controller.Mode = strings.TrimSpace(firstNonEmptyString(
		lookupString(entry, "chargingMode", "value"),
		lookupString(entry, "chargingMode"),
		lookupString(entry, "state", "value"),
		lookupString(entry, "state"),
		lookupString(entry, "mode", "value"),
		lookupString(entry, "mode"),
	))

	controller.Error = strings.TrimSpace(firstNonEmptyString(
		lookupString(entry, "error", "value"),
		lookupString(entry, "error"),
		lookupString(entry, "alarm", "value"),
		lookupString(entry, "alarm"),
	))

	timestamp := firstNonEmptyString(
		lookupString(entry, "panelPower", "timestamp"),
		lookupString(entry, "yieldToday", "timestamp"),
		lookupString(entry, "timestamp"),
	)
	if timestamp != "" {
		parsed, err := time.Parse(time.RFC3339, timestamp)
		if err == nil {
			age := sampleTime.Sub(parsed.UTC()).Seconds()
			if age >= 0 {
				controller.LastUpdateAge = roundTo1(age)
			}
		}
	}

	return controller
}

func defaultSolarControllerLabel(index int, id string) string {
	switch index {
	case 0:
		return "Port"
	case 1:
		return "Starboard"
	case 2:
		return "Salon"
	default:
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			return fmt.Sprintf("Array %d", index+1)
		}
		return "Array " + strings.ToUpper(trimmed)
	}
}

func normalizeYieldToKWh(raw float64) float64 {
	if raw < 0 {
		return -1
	}
	// Some integrations emit daily yield in Wh; convert to kWh heuristically.
	if raw > 200 {
		return raw / 1000
	}
	return raw
}

func roundTo3(value float64) float64 {
	return math.Round(value*1000) / 1000
}

func readAlternatorInstance(payload map[string]any, index string) alternatorInstanceData {
	inst := alternatorInstanceData{CurrentA: -1, VoltageV: -1, PowerW: -1, TempC: -1}

	current := lookupFirstNumber(payload,
		[]string{"electrical", "alternator", index, "current", "value"},
		[]string{"electrical", "alternator", index, "current"},
	)
	if current >= 0 {
		inst.CurrentA = roundTo1(current)
	}

	voltage := lookupFirstNumber(payload,
		[]string{"electrical", "alternator", index, "voltage", "value"},
		[]string{"electrical", "alternator", index, "voltage"},
	)
	if voltage >= 0 {
		inst.VoltageV = roundTo1(voltage)
	}

	power := lookupFirstNumber(payload,
		[]string{"electrical", "alternator", index, "power", "value"},
		[]string{"electrical", "alternator", index, "power"},
	)
	if power == -1 && inst.CurrentA >= 0 && inst.VoltageV > 0 {
		power = roundTo1(inst.CurrentA * inst.VoltageV)
	}
	if power >= 0 {
		inst.PowerW = roundTo1(power)
	}

	tempK := lookupFirstNumber(payload,
		[]string{"electrical", "alternator", index, "temperature", "value"},
		[]string{"electrical", "alternator", index, "temperature"},
	)
	if tempK >= 0 {
		inst.TempC = roundTo1(tempK - 273.15)
	}

	return inst
}

func readChargerInstance(payload map[string]any, index string) chargerInstanceData {
	inst := chargerInstanceData{CurrentA: -1, ACIn1CurrentA: -1}

	current := lookupFirstNumber(payload,
		[]string{"electrical", "chargers", index, "current", "value"},
		[]string{"electrical", "chargers", index, "current"},
	)
	if current >= 0 {
		inst.CurrentA = roundTo1(current)
	}

	acIn1Current := lookupFirstNumber(payload,
		[]string{"electrical", "chargers", index, "acin", "1", "current", "value"},
		[]string{"electrical", "chargers", index, "acin", "1", "current"},
	)
	if acIn1Current >= 0 {
		inst.ACIn1CurrentA = roundTo1(acIn1Current)
	}

	chargingMode := strings.TrimSpace(firstNonEmptyString(
		lookupString(payload, "electrical", "chargers", index, "chargingMode", "value"),
		lookupString(payload, "electrical", "chargers", index, "chargingMode"),
	))
	if chargingMode != "" {
		inst.ChargingMode = chargingMode
	}

	errorValue := strings.TrimSpace(firstNonEmptyString(
		lookupString(payload, "electrical", "chargers", index, "error", "value"),
		lookupString(payload, "electrical", "chargers", index, "error"),
	))
	if errorValue != "" {
		inst.Error = errorValue
	}

	return inst
}

// nearbyVesselMaxAge drops AIS contacts whose position has not been refreshed
// recently. The snapshot never evicts contexts, so without this a vessel that
// transmitted once and left stays frozen at its last position forever - and
// because the list sorts by range and caps at 10, a nearby ghost crowds out
// live targets. Class A and Class B both transmit at least every 3 minutes
// when stationary, so 10 minutes is ~3 missed reports.
const nearbyVesselMaxAge = 10 * time.Minute

// collisionTargetMaxAge is the cutoff for holding a CPA alarm up. Tighter than
// nearbyVesselMaxAge on purpose: the tile lists stationary neighbours that
// report every ~3 minutes, but a target worth alarming over is moving, and a
// moving target reports every 5 to 30 seconds. Five minutes is a dozen missed
// reports for the case that matters, not one. It costs nothing on the
// stationary case either, because ADR 0058 already stops a stationary target
// tripping this alarm in the first place.
const collisionTargetMaxAge = 5 * time.Minute

// aisTargetPositionFresh reports when a target's navigation.position last
// arrived and whether that is recent enough to trust the rest of its tree.
//
// One predicate, two cutoffs. The tile aged its targets and the CPA alarm did
// not, so a target that left range dropped off the tile while its collision
// alarm stayed lit in the banner until the process restarted.
//
// Position is the reference, never the notification's own path. The
// prioritizer writes notifications.navigation.closestApproach on a state
// change, not on a timer, so ageing that path against itself would silently
// clear an alarm that is still entirely valid — the target could be closing
// steadily for half an hour on one write and this would drop it a maxAge
// after that single write, which is a masking failure dressed up as a fix.
func aisTargetPositionFresh(snapshot *signalKSnapshot, vesselID string, maxAge time.Duration, now time.Time) (time.Time, bool) {
	seen := snapshot.lastSeen(vesselContextPrefix+vesselID, "navigation.position")
	if seen.IsZero() {
		return time.Time{}, false
	}
	return seen, now.Sub(seen) <= maxAge
}

const (
	// nearbyMinRangeMeters drops this vessel itself, which SignalK may publish
	// under a non-self context as well.
	nearbyMinRangeMeters = 9.144 // the 30 ft threshold this replaced
	nearbyMaxRangeMeters = 5000.0
)

// nearbyVesselsDefaultLimit is the map tile's own cap: fetchSignalKNearbyVessels
// (the /api/nearby-vessels handler and the contact poller, tracks.go) has
// always kept only the 10 closest vessels. fetchSignalKNearbyVesselsLimit
// (ADR 0128) generalizes that cap for a caller - Mate's get_nearby_vessels
// tool - that needs more than the tile itself ever shows.
const nearbyVesselsDefaultLimit = 10

// nearbyVesselsUnlimited is fetchSignalKNearbyVesselsLimit's sentinel for
// "keep every vessel currently in range, no cap at all" - get_nearby_vessels'
// (ADR 0128) own use when a name/MMSI filter names one specific vessel: a
// second, merely-larger-but-still-finite cap could still miss a vessel in a
// crowded anchorage, where an outright unlimited search cannot.
const nearbyVesselsUnlimited = -1

func fetchSignalKNearbyVessels(selfLatitude float64, selfLongitude float64, now time.Time, excludedNames []string) ([]nearbyVessel, error) {
	return fetchSignalKNearbyVesselsLimit(selfLatitude, selfLongitude, now, excludedNames, nearbyVesselsDefaultLimit)
}

// fetchSignalKNearbyVesselsLimit is fetchSignalKNearbyVessels with a
// caller-chosen cap on how many of the sorted-by-range results to keep,
// instead of the tile's hard-coded 10 (ADR 0128) - a behaviour-preserving
// split, not a change to fetchSignalKNearbyVessels' own contract.
func fetchSignalKNearbyVesselsLimit(selfLatitude float64, selfLongitude float64, now time.Time, excludedNames []string, limit int) ([]nearbyVessel, error) {
	payload, err := signalKVesselsPayload()
	if err != nil {
		return nil, err
	}

	vessels := make([]nearbyVessel, 0, len(payload))
	for vesselID, raw := range payload {
		vesselMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		if vesselID == "self" {
			continue
		}

		latitude := lookupNumber(vesselMap, "navigation", "position", "value", "latitude")
		if latitude == -1 {
			latitude = lookupNumber(vesselMap, "navigation", "position", "latitude")
		}

		longitude := lookupNumber(vesselMap, "navigation", "position", "value", "longitude")
		if longitude == -1 {
			longitude = lookupNumber(vesselMap, "navigation", "position", "longitude")
		}

		if latitude < -90 || latitude > 90 || longitude < -180 || longitude > 180 {
			continue
		}

		name := firstNonEmptyString(lookupString(vesselMap, "name"), lookupString(vesselMap, "design", "name"))
		if name == "" {
			name = compactVesselID(vesselID)
		}
		if matchesExcludedName(name, excludedNames) {
			continue
		}

		// SignalK servers are inconsistent about whether "mmsi" is a JSON
		// string or a bare JSON number - this app's actual live server
		// sends it as a string (confirmed live), so try that first and
		// fall back to numeric for servers that don't.
		mmsi := strings.TrimSpace(lookupString(vesselMap, "mmsi"))
		if mmsi == "" {
			if rawMmsi := lookupNumber(vesselMap, "mmsi"); rawMmsi >= 0 {
				mmsi = fmt.Sprintf("%.0f", rawMmsi)
			}
		}

		rangeMeters := math.Round(haversineMeters(selfLatitude, selfLongitude, latitude, longitude)*10) / 10
		if rangeMeters < nearbyMinRangeMeters || rangeMeters > nearbyMaxRangeMeters {
			continue
		}

		// Age comes from delta receive time (pathSeen), not the AIS-reported
		// navigation.position.timestamp: receive time is always present for
		// any vessel that exists in the snapshot at all, is immune to
		// transmitter clock skew, and resets cleanly with the process. The
		// old timestamp-parse approach silently left ageSeconds at 0 whenever
		// the field was missing or failed to parse - a masking fallback that
		// reported a dead target as "0s ago" forever, which is exactly the
		// case a staleness filter most needs to catch (see ADR 0042).
		//
		// aisTargetPositionFresh folds two failures into this one continue: no
		// position delta ever received for this context at all (nothing to age
		// against, so the position in the tree cannot be trusted), and a
		// position that arrived but has since gone stale. The collision-alarm
		// gate in collision_ais.go shares this same predicate against a
		// tighter cutoff (ADR 0057).
		positionSeen, fresh := aisTargetPositionFresh(globalSignalKSnapshot, vesselID, nearbyVesselMaxAge, now)
		if !fresh {
			continue
		}
		age := now.Sub(positionSeen)
		ageSeconds := int(age.Seconds())
		if ageSeconds < 0 {
			ageSeconds = 0
		}

		var sogKnots *float64
		sog := lookupNumber(vesselMap, "navigation", "speedOverGround", "value")
		if sog >= 0 {
			knots := math.Round((sog*1.943844)*10) / 10
			sogKnots = &knots
		}

		figures := collisionFiguresFor(vesselMap)

		vessels = append(vessels, nearbyVessel{
			ID: vesselID, Name: strings.ToUpper(name), Mmsi: mmsi,
			RangeM: rangeMeters, AgeSeconds: ageSeconds, SogKnots: sogKnots,
			Lat: latitude, Lon: longitude, PositionSeen: positionSeen,
			CpaM: figures.CpaM, TcpaSeconds: figures.TcpaSeconds, BearingRad: figures.BearingRad,
			CollisionAlarmType: figures.AlarmType, CollisionAlarmState: figures.AlarmState,
		})
	}

	sort.Slice(vessels, func(i int, j int) bool { return vessels[i].RangeM < vessels[j].RangeM })
	switch {
	case limit == nearbyVesselsUnlimited:
		// no trim - every in-range vessel is kept.
	case limit <= 0:
		limit = nearbyVesselsDefaultLimit
		if len(vessels) > limit {
			vessels = vessels[:limit]
		}
	case len(vessels) > limit:
		vessels = vessels[:limit]
	}

	return vessels, nil
}

// ── SignalK History API (ADR 0128) ──────────────────────────────────────

// signalKHistoryContextPrefix is what the SignalK History API expects a
// non-self vessel's context to be namespaced under. This app only ever has a
// bare MMSI on hand (nearbyVessel.Mmsi / vesselContactKey's own identity),
// so vesselHistoryContext builds the same "vessels.urn:mrn:imo:mmsi:<mmsi>"
// form an AIS MMSI's SignalK identity actually takes - confirmed against a
// live server (2026-09-25): /signalk/v2/api/history/contexts lists this
// vessel's own context in exactly this form.
const signalKHistoryContextPrefix = "vessels.urn:mrn:imo:mmsi:"

func vesselHistoryContext(mmsi string) string {
	return signalKHistoryContextPrefix + mmsi
}

// signalKHistoryValuesAPIPath is the v2 History API endpoint that answers
// "where was this context over this time range" - distinct from every other
// SignalK path in this file, which reads the live data-model tree rather
// than a time series. Confirmed against a live server (2026-09-25): it needs
// context, paths, from, to and resolution query parameters, and returns
// {"data":[[iso-time, [lon,lat]], ...]}.
const signalKHistoryValuesAPIPath = "/signalk/v2/api/history/values"

// signalKHistoryPoint is one point of a SignalK History API position series
// - get_nearby_vessels' stationary-since figure (ADR 0128): when the fix was
// recorded, and where.
type signalKHistoryPoint struct {
	Time time.Time
	Lat  float64
	Lon  float64
}

// parseSignalKHistoryValues decodes a /signalk/v2/api/history/values response
// body into a slice of points, in whatever order the server returned them
// (ascending by time on the live server this was confirmed against, but the
// caller sorts explicitly rather than assuming that). Each data row is
// [iso-timestamp, [lon, lat]] - a resolution bucket with nothing sampled in
// it comes back with a null second element rather than being omitted, which
// is skipped rather than failing the whole decode over one empty bucket. An
// empty data array (confirmed live, 2026-09-25, for a vessel this boat's own
// InfluxDB writer has never recorded - see ADR 0128) decodes to an empty,
// nil-error slice: "no history for this vessel" is a normal outcome, not a
// parse failure.
func parseSignalKHistoryValues(body []byte) ([]signalKHistoryPoint, error) {
	var parsed struct {
		Data [][]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode signalk history response: %w", err)
	}

	points := make([]signalKHistoryPoint, 0, len(parsed.Data))
	for _, row := range parsed.Data {
		if len(row) != 2 {
			continue
		}
		var ts string
		if err := json.Unmarshal(row[0], &ts); err != nil {
			continue
		}
		// An empty resolution bucket comes back with a literal JSON null
		// here rather than a [lon,lat] pair. Unmarshaling null into a fixed
		// [2]float64 array is a silent no-op in encoding/json (null only
		// resets interface/map/pointer/slice targets), so this must be
		// checked explicitly - without it, an empty bucket would decode as
		// a bogus fix at (0, 0) instead of being skipped.
		if string(row[1]) == "null" {
			continue
		}
		// Decoded as a slice, not straight into a fixed [2]float64: Go's
		// encoding/json silently zero-fills a JSON array shorter than the Go
		// array it targets, and silently discards extra elements of a longer
		// one - neither is an error, so a malformed one-element [lon] row
		// would otherwise decode into a bogus (lon, 0) point instead of
		// being skipped (code-review finding, 2026-09-25). The explicit
		// length check below is what actually enforces "must be [lon,lat]".
		var lonLat []float64
		if err := json.Unmarshal(row[1], &lonLat); err != nil || len(lonLat) != 2 {
			// Any other shape that isn't exactly a [lon,lat] pair - skip
			// this one row rather than failing the whole response over it.
			continue
		}
		t, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			continue
		}
		points = append(points, signalKHistoryPoint{Time: t, Lon: lonLat[0], Lat: lonLat[1]})
	}

	return points, nil
}

// fetchSignalKPositionHistory reads mmsi's navigation.position history from
// the SignalK History API over [from, to], aggregated to resolutionSeconds
// buckets. A non-2xx status or an undecodable body is returned as an
// explicit error (AGENTS.md's fallback policy) - never a fabricated point;
// an empty result set is not an error (see parseSignalKHistoryValues) since
// today that is the expected outcome for every vessel but self (ADR 0128).
func fetchSignalKPositionHistory(settingsPath, mmsi string, from, to time.Time, resolutionSeconds int) ([]signalKHistoryPoint, error) {
	// Unlike buildNearbyVesselsPayload's use of loadSignalKSettings, a
	// settings-read failure here must not silently fall back to
	// defaultSignalKAddress: this result feeds get_nearby_vessels'
	// per-vessel position_history_error field, and reporting a request
	// against a wrong, made-up host as if it were this boat's actual
	// SignalK server would misattribute the real failure entirely
	// (AGENTS.md's fallback policy; code-review finding, 2026-09-25).
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		return nil, fmt.Errorf("load signalk settings: %w", err)
	}
	signalkURL := buildSignalKURL(address, port)

	query := url.Values{}
	query.Set("context", vesselHistoryContext(mmsi))
	query.Set("paths", "navigation.position")
	query.Set("from", from.UTC().Format(time.RFC3339))
	query.Set("to", to.UTC().Format(time.RFC3339))
	query.Set("resolution", strconv.Itoa(resolutionSeconds))
	path := signalKHistoryValuesAPIPath + "?" + query.Encode()

	status, body, err := signalkRequestJSONWithAuthBody(signalkURL, settingsPath, path, http.MethodGet, nil)
	if err != nil {
		return nil, fmt.Errorf("signalk history request for %s: %w", mmsi, err)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("signalk history endpoint returned status %d for %s: %s", status, mmsi, string(body))
	}

	points, err := parseSignalKHistoryValues(body)
	if err != nil {
		return nil, fmt.Errorf("%w (vessel %s)", err, mmsi)
	}
	return points, nil
}

func fetchSignalKVesselNameMap() (map[string]string, error) {
	payload, err := signalKVesselsPayload()
	if err != nil {
		return nil, err
	}

	names := make(map[string]string, len(payload))
	for vesselID, raw := range payload {
		vesselMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		name := firstNonEmptyString(lookupString(vesselMap, "name"), lookupString(vesselMap, "design", "name"))
		if name == "" {
			name = compactVesselID(vesselID)
		}
		names[vesselID] = strings.ToUpper(strings.TrimSpace(name))
	}

	return names, nil
}

func fetchSignalKTanksState(labelOverrides map[string]string) ([]tankLevelData, time.Time, error) {
	payload, err := signalKSelfPayload()
	if err != nil {
		return nil, time.Now().UTC(), err
	}

	datetime := time.Now().UTC()
	datetimeString := firstNonEmptyString(lookupString(payload, "navigation", "datetime", "value"), lookupString(payload, "navigation", "datetime"), lookupString(payload, "timestamp"))
	if datetimeString != "" {
		parsed, parseErr := time.Parse(time.RFC3339, datetimeString)
		if parseErr == nil {
			datetime = parsed.UTC()
		}
	}

	tanksMap := lookupAnyMap(payload, "tanks")
	if tanksMap == nil {
		return []tankLevelData{}, datetime, nil
	}

	categoryOrder := []string{"freshWater", "fuel", "blackWater", "greyWater", "liveWell", "lubrication", "water", "wasteWater"}
	knownCategory := map[string]struct{}{}
	for _, category := range categoryOrder {
		knownCategory[category] = struct{}{}
	}

	orderedCategories := make([]string, 0, len(tanksMap))
	for _, category := range categoryOrder {
		if _, ok := tanksMap[category]; ok {
			orderedCategories = append(orderedCategories, category)
		}
	}
	for category := range tanksMap {
		if _, ok := knownCategory[category]; ok {
			continue
		}
		orderedCategories = append(orderedCategories, category)
	}

	tanks := make([]tankLevelData, 0)
	for _, category := range orderedCategories {
		categoryRaw, ok := tanksMap[category]
		if !ok {
			continue
		}

		categoryEntries, ok := categoryRaw.(map[string]any)
		if !ok {
			continue
		}

		entryIDs := make([]string, 0, len(categoryEntries))
		for entryID := range categoryEntries {
			entryIDs = append(entryIDs, entryID)
		}
		sort.Strings(entryIDs)

		for _, entryID := range entryIDs {
			rawEntry := categoryEntries[entryID]
			entry, ok := rawEntry.(map[string]any)
			if !ok {
				continue
			}

			level := lookupFirstNumber(entry, []string{"currentLevel", "value"}, []string{"currentLevel"})
			if level < 0 {
				continue
			}

			if level <= 1 {
				level *= 100
			}
			level = math.Max(0, math.Min(100, roundTo1(level)))

			label := tankLabelOverride(labelOverrides, category, entryID)
			if label == "" {
				continue
			}

			// Mirrors readSolarController (ADR 0068): each tank's age comes
			// from its own subtree, so one dead sender does not condemn
			// every tank on the boat.
			lastUpdateAge := freshestTimestampAge(entry, datetime)

			tanks = append(tanks, tankLevelData{ID: category + "." + entryID, Label: label, Category: category, Kind: tankKindFromCategory(category), LevelPercent: level, LastUpdateAge: lastUpdateAge})
		}
	}

	return tanks, datetime, nil
}

func loadTankLabelOverrides(settingsPath string) map[string]string {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return map[string]string{}
	}

	uiMap, ok := settings["ui"].(map[string]any)
	if !ok {
		return map[string]string{}
	}

	rawLabels, ok := uiMap["tank_labels"].(map[string]any)
	if !ok {
		return map[string]string{}
	}

	labels := map[string]string{}
	for key, value := range rawLabels {
		label, ok := value.(string)
		if !ok {
			continue
		}

		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		normalizedLabel := strings.TrimSpace(label)
		if normalizedKey == "" || normalizedLabel == "" {
			continue
		}

		labels[normalizedKey] = normalizedLabel
	}

	return labels
}

func tankLabelOverride(overrides map[string]string, category string, entryID string) string {
	if len(overrides) == 0 {
		return ""
	}

	category = strings.ToLower(strings.TrimSpace(category))
	entryID = strings.TrimSpace(entryID)

	keys := []string{category + "." + strings.ToLower(entryID), category + "/" + strings.ToLower(entryID), strings.ToLower(entryID)}
	for _, key := range keys {
		if value, ok := overrides[key]; ok {
			return value
		}
	}

	return ""
}

func tankKindFromCategory(category string) string {
	normalized := strings.ToLower(strings.TrimSpace(category))
	if strings.Contains(normalized, "fuel") {
		return "fuel"
	}

	if strings.Contains(normalized, "black") || strings.Contains(normalized, "grey") || strings.Contains(normalized, "waste") || strings.Contains(normalized, "sewage") || strings.Contains(normalized, "holding") {
		return "waste"
	}

	return "water"
}

func lookupString(payload map[string]any, keys ...string) string {
	var current any = payload
	for _, key := range keys {
		asMap, ok := current.(map[string]any)
		if !ok {
			return ""
		}

		next, ok := asMap[key]
		if !ok {
			return ""
		}

		current = next
	}

	value, ok := current.(string)
	if !ok {
		return ""
	}

	return value
}

func lookupNumber(payload map[string]any, keys ...string) float64 {
	var current any = payload
	for _, key := range keys {
		asMap, ok := current.(map[string]any)
		if !ok {
			return -1
		}

		next, ok := asMap[key]
		if !ok {
			return -1
		}

		current = next
	}

	switch v := current.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	default:
		return -1
	}
}

func lookupBool(payload map[string]any, keys ...string) (bool, bool) {
	var current any = payload
	for _, key := range keys {
		asMap, ok := current.(map[string]any)
		if !ok {
			return false, false
		}
		next, ok := asMap[key]
		if !ok {
			return false, false
		}
		current = next
	}
	switch v := current.(type) {
	case bool:
		return v, true
	case float64:
		return v != 0, true
	}
	return false, false
}

func lookupFirstNumber(payload map[string]any, paths ...[]string) float64 {
	for _, path := range paths {
		value := lookupNumber(payload, path...)
		if value != -1 {
			return value
		}
	}

	return -1
}

func lookupNumberFromAnyChild(payload map[string]any, prefix []string, suffix []string) float64 {
	parent := lookupAnyMap(payload, prefix...)
	if parent == nil {
		return -1
	}

	for _, rawChild := range parent {
		child, ok := rawChild.(map[string]any)
		if !ok {
			continue
		}

		value := lookupNumber(child, suffix...)
		if value != -1 {
			return value
		}
	}

	return -1
}

// sumNumberFromAllChildren sums a numeric field across all children of a map node.
// Returns -1 if no children have the field.
func sumNumberFromAllChildren(payload map[string]any, prefix []string, suffix []string) float64 {
	parent := lookupAnyMap(payload, prefix...)
	if parent == nil {
		return -1
	}

	sum := 0.0
	found := false
	for _, rawChild := range parent {
		child, ok := rawChild.(map[string]any)
		if !ok {
			continue
		}
		value := lookupNumber(child, suffix...)
		if value != -1 {
			sum += value
			found = true
		}
	}

	if !found {
		return -1
	}
	return sum
}

// lookupMainBattery returns the battery sub-object with valid SOC data and the
// highest absolute current — i.e. the actively monitored house bank.
func lookupMainBattery(payload map[string]any) map[string]any {
	batteries := lookupAnyMap(payload, "electrical", "batteries")
	if batteries == nil {
		return nil
	}

	var best map[string]any
	bestAbsCurrent := -1.0

	for _, rawBattery := range batteries {
		battery, ok := rawBattery.(map[string]any)
		if !ok {
			continue
		}

		soc := lookupNumber(battery, "capacity", "stateOfCharge", "value")
		if soc == -1 || soc < 0 || soc > 1.01 {
			continue
		}

		current := lookupNumber(battery, "current", "value")
		if current == -1 {
			continue
		}

		if absCurrent := math.Abs(current); absCurrent > bestAbsCurrent {
			bestAbsCurrent = absCurrent
			best = battery
		}
	}

	return best
}

// freshestTimestampAge reports how many seconds before sampleTime the most
// recent RFC3339 "timestamp" anywhere in the subtree was written.
//
// It returns -1 when the subtree carries no usable timestamp at all. That is
// deliberately distinct from 0: an age we cannot determine must never be
// presented as freshly measured.
func freshestTimestampAge(node any, sampleTime time.Time) float64 {
	newest := time.Time{}

	var walk func(any)
	walk = func(current any) {
		entry, ok := current.(map[string]any)
		if !ok {
			return
		}
		for key, child := range entry {
			if key == "timestamp" {
				raw, isString := child.(string)
				if !isString {
					continue
				}
				parsed, err := time.Parse(time.RFC3339, raw)
				if err == nil && parsed.After(newest) {
					newest = parsed.UTC()
				}
				continue
			}
			walk(child)
		}
	}
	walk(node)

	if newest.IsZero() {
		return -1
	}

	age := sampleTime.Sub(newest).Seconds()
	if age < 0 {
		// Source clock is marginally ahead of the vessel clock; it is current.
		return 0
	}
	return roundTo1(age)
}

// freshestAge reduces several last_update_age_s values (each -1 for
// "unknown", per freshestTimestampAge) down to the one a tile should show:
// the freshest (minimum) of whichever inputs are actually known. This is
// the same "one dead controller among four does not condemn the total"
// reduction ADR 0068 established for solarStateData.LastUpdateAge across its
// controllers, generalized here so tanks, nearby vessels, and a GNSS fix
// split across navigation.position/navigation.gnss can reuse it instead of
// each re-deriving the same min-ignoring-unknowns logic.
//
// An empty call (or every input unknown) returns -1, not 0: no known ages is
// an absence of evidence, not evidence the source is fresh.
func freshestAge(ages ...float64) float64 {
	result := -1.0
	for _, age := range ages {
		if age < 0 {
			continue
		}
		if result < 0 || age < result {
			result = age
		}
	}
	return result
}

// tanksFeedAge reduces each tank's own last_update_age_s down to the one age
// the Tanks tile itself goes stale on: the freshest of all reporting tanks
// (ADR 0068's "one dead controller does not condemn the total" reduction,
// same as solarStateData.LastUpdateAge across its controllers). Zero tanks
// configured is absence of evidence, not evidence of a dead feed, so an
// empty slice correctly falls through to freshestAge's -1.
func tanksFeedAge(tanks []tankLevelData) float64 {
	ages := make([]float64, len(tanks))
	for i, tank := range tanks {
		ages[i] = tank.LastUpdateAge
	}
	return freshestAge(ages...)
}

// nearbyVesselsFeedAge reduces each contact's own age_seconds (how long
// since that vessel was last seen) down to the one age the Nearby Vessels
// tile itself goes stale on: the freshest contact in range. Zero vessels in
// range is silence, not evidence the AIS/radar feed died, so an empty slice
// correctly falls through to freshestAge's -1 rather than 0.
func nearbyVesselsFeedAge(vessels []nearbyVessel) float64 {
	ages := make([]float64, len(vessels))
	for i, vessel := range vessels {
		ages[i] = float64(vessel.AgeSeconds)
	}
	return freshestAge(ages...)
}

func lookupAnyMap(payload map[string]any, keys ...string) map[string]any {
	var current any = payload
	for _, key := range keys {
		asMap, ok := current.(map[string]any)
		if !ok {
			return nil
		}

		next, ok := asMap[key]
		if !ok {
			return nil
		}

		current = next
	}

	result, ok := current.(map[string]any)
	if !ok {
		return nil
	}

	return result
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}

// ── SignalK authentication token cache ───────────────────────────────────────

type cachedToken struct {
	token     string
	expiresAt time.Time
}

var skTokenMu sync.Mutex
var skTokenCache *cachedToken

// loadSignalKCredentials reads SIGNALK_USERNAME/SIGNALK_PASSWORD from
// globalSecretsStore at point of use (ADR 0023 amendment, 2026-09-19) - the
// boot-time copy into the process environment (LoadIntoEnv) is retired, so
// there is no cache here to go stale when an operator rotates the
// credential from the Secrets panel.
//
// A nil store is treated the same as "no credential configured", not an
// error: it mirrors wasm_plugin.go's configForWasmPlugin, whose own doc
// comment draws this exact distinction for a READ (as opposed to
// clearBoundSecret's WRITE, where a nil store must fail loudly) - a caller
// that cannot reach a secrets store at all simply proceeds unauthenticated,
// which acquireSignalKToken already treats as a supported, legitimate mode.
// A store that IS open but fails to read or decrypt a row is a different,
// worse case: an operator may have a real credential configured, and
// silently discarding it here would degrade SignalK auth to anonymous with
// nothing to show it happened - "a working boat with no alarm writes". That
// case is returned as an error instead, per this repo's fallback policy.
func loadSignalKCredentials(_ string) (username, password string, err error) {
	// A nil store is a programming error, never a deployment state: main()
	// opens it and log.Fatalf's on failure long before anything that reads a
	// credential is started. Reporting it as "no credentials configured"
	// would let SignalK quietly fall back to unauthenticated on a boot
	// ordering mistake - a boat that looks healthy while every notifications.*
	// write is silently refused. Surface it instead.
	if globalSecretsStore == nil {
		return "", "", fmt.Errorf("secrets store unavailable, cannot read SignalK credentials")
	}
	username, _, err = globalSecretsStore.Get("SIGNALK_USERNAME")
	if err != nil {
		return "", "", fmt.Errorf("reading SIGNALK_USERNAME: %w", err)
	}
	password, _, err = globalSecretsStore.Get("SIGNALK_PASSWORD")
	if err != nil {
		return "", "", fmt.Errorf("reading SIGNALK_PASSWORD: %w", err)
	}
	return username, password, nil
}

// acquireSignalKToken returns a cached JWT or fetches a fresh one.
func acquireSignalKToken(signalkURL, username, password string) (string, error) {
	if username == "" || password == "" {
		return "", nil
	}

	skTokenMu.Lock()
	defer skTokenMu.Unlock()

	if skTokenCache != nil && time.Now().Before(skTokenCache.expiresAt) {
		return skTokenCache.token, nil
	}

	url := strings.TrimRight(signalkURL, "/") + "/signalk/v1/auth/login"
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("signalk auth: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("signalk auth returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Token      string  `json:"token"`
		TimeToLive float64 `json:"timeToLive"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil || result.Token == "" {
		return "", fmt.Errorf("signalk auth: could not parse token response")
	}

	ttl := result.TimeToLive
	if ttl <= 0 {
		ttl = 86400 // default 24h
	}
	skTokenCache = &cachedToken{
		token:     result.Token,
		expiresAt: time.Now().Add(time.Duration(ttl-60) * time.Second),
	}
	return result.Token, nil
}

// invalidateSignalKToken clears the cached token so the next call re-authenticates.
func invalidateSignalKToken() {
	skTokenMu.Lock()
	skTokenCache = nil
	skTokenMu.Unlock()
}

func buildSignalKURL(address string, port int) string {
	trimmed := strings.TrimSpace(address)
	if trimmed == "" {
		trimmed = defaultSignalKAddress
	}

	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return strings.TrimRight(trimmed, "/")
	}

	if port <= 0 || port > 65535 {
		port = defaultSignalKPort
	}

	return fmt.Sprintf("http://%s:%d", trimmed, port)
}

func loadSignalKSettings(settingsPath string) (string, int, error) {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return "", 0, err
	}

	signalkMap, ok := settings["signalk"].(map[string]any)
	if !ok {
		return defaultSignalKAddress, defaultSignalKPort, nil
	}

	address, _ := signalkMap["address"].(string)
	if strings.TrimSpace(address) == "" {
		address = defaultSignalKAddress
	}

	port := coercePort(signalkMap["port"])
	if port <= 0 {
		port = defaultSignalKPort
	}

	return address, port, nil
}

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

func normalizeDegrees(value float64) float64 {
	normalized := math.Mod(value, 360)
	if normalized < 0 {
		normalized += 360
	}

	return normalized
}

func normalizeSignedDegrees(value float64) float64 {
	normalized := normalizeDegrees(value)
	if normalized > 180 {
		normalized -= 360
	}

	return normalized
}

func roundTo1(value float64) float64 { return math.Round(value*10) / 10 }

func haversineMeters(lat1 float64, lon1 float64, lat2 float64, lon2 float64) float64 {
	const earthRadiusMeters = 6371000.0
	lat1Rad := lat1 * math.Pi / 180
	lon1Rad := lon1 * math.Pi / 180
	lat2Rad := lat2 * math.Pi / 180
	lon2Rad := lon2 * math.Pi / 180
	deltaLat := lat2Rad - lat1Rad
	deltaLon := lon2Rad - lon1Rad
	a := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) + math.Cos(lat1Rad)*math.Cos(lat2Rad)*math.Sin(deltaLon/2)*math.Sin(deltaLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusMeters * c
}

// destinationPoint projects (lat, lon) forward by distanceM metres along
// bearingRad (radians, 0 = north, clockwise), returning the resulting
// lat/lon in degrees. Spherical model, ported from
// frontend/src/lib/geo.ts's destinationPoint (same R = 6371000, same
// longitude wrap) so the two stay numerically consistent.
func destinationPoint(lat float64, lon float64, bearingRad float64, distanceM float64) (float64, float64) {
	const earthRadiusMeters = 6371000.0
	lat1 := lat * math.Pi / 180
	lon1 := lon * math.Pi / 180

	lat2 := math.Asin(
		math.Sin(lat1)*math.Cos(distanceM/earthRadiusMeters) +
			math.Cos(lat1)*math.Sin(distanceM/earthRadiusMeters)*math.Cos(bearingRad),
	)
	lon2 := lon1 + math.Atan2(
		math.Sin(bearingRad)*math.Sin(distanceM/earthRadiusMeters)*math.Cos(lat1),
		math.Cos(distanceM/earthRadiusMeters)-math.Sin(lat1)*math.Sin(lat2),
	)

	latDeg := lat2 * 180 / math.Pi
	lonDeg := math.Mod(lon2*180/math.Pi+540, 360) - 180

	return latDeg, lonDeg
}

func compactVesselID(vesselID string) string {
	trimmed := strings.TrimSpace(vesselID)
	if trimmed == "" {
		return "UNKNOWN"
	}
	segments := strings.Split(trimmed, ":")
	return segments[len(segments)-1]
}

func loadBoatVesselPrefix(settingsPath string) string {
	return loadSettingString(settingsPath, []string{"boat", "name"})
}

func loadHouseBatteryCapacityAh(settingsPath string) float64 {
	capacity := loadSettingFloat(settingsPath, []string{"boat", "house_battery_capacity_ah"})
	if capacity <= 0 {
		return -1
	}
	return capacity
}

func fetchSignalKSelfName() string {
	payload, err := signalKSelfPayload()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(firstNonEmptyString(lookupString(payload, "name"), lookupString(payload, "design", "name")))
}

func matchesExcludedName(candidate string, excludedNames []string) bool {
	trimmedCandidate := strings.TrimSpace(candidate)
	if trimmedCandidate == "" {
		return false
	}
	for _, excluded := range excludedNames {
		if excluded != "" && strings.EqualFold(trimmedCandidate, strings.TrimSpace(excluded)) {
			return true
		}
	}
	return false
}
