package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func trustedSignalKPayload(latitude, longitude float64) string {
	return fmt.Sprintf(`{
		"navigation": {
			"datetime": {"value": %q},
			"state": {"value": "anchored"},
			"position": {"value": {"latitude": %f, "longitude": %f}},
			"gnss": {
				"methodQuality": {"value": 1},
				"horizontalDilution": {"value": 0.9},
				"satellites": {"value": 8}
			}
		}
	}`, time.Now().UTC().Format(time.RFC3339), latitude, longitude)
}

// trustedSignalKPayloadServer both seeds the delta-stream snapshot and serves
// the same payload over HTTP. Telemetry reads the snapshot (ADR 0037) while
// probe and settings-validation tests still need a real address to fetch, and
// callers of this helper span both.
func trustedSignalKPayloadServer(t *testing.T, latitude, longitude float64) *httptest.Server {
	t.Helper()
	body := trustedSignalKPayload(latitude, longitude)
	seedSelfTree(t, body)

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
}

func approxEqual(a, b, tolerance float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= tolerance
}

// TestNormalizeSettingsPayload_PersistsUnregisteredTideProviderAsSubmitted
// guards against a regression of the bug where any tide_provider value not
// currently in the registry (e.g. a WASM plugin not yet built/loaded, or a
// simple typo) was silently rewritten to a hardcoded default provider id on
// every settings save - discarding the caller's choice without any error.
// tide_provider should round-trip exactly like tide_station_id/tide_station_name already
// do a few lines below it; registry validation belongs at read time
// (tideToday, tideChartHandler, ...), not at write time.
func TestNormalizeSettingsPayload_PersistsUnregisteredTideProviderAsSubmitted(t *testing.T) {
	req := settingsPayload{}
	req.UI.TideProvider = "not-a-real-provider"

	normalized := normalizeSettingsPayload(req)

	if normalized.UI.TideProvider != "not-a-real-provider" {
		t.Fatalf("expected tide_provider to be persisted as submitted, got %q", normalized.UI.TideProvider)
	}
}

// TestBuildSettingsPayload_SurfacesUnregisteredTideProviderFromDisk guards
// the read-side counterpart: GET /api/settings must reflect whatever is
// actually stored in settings.yaml, even if the configured provider isn't
// currently registered (e.g. between a settings save and a container
// restart that loads the new plugin) - not silently substitute a hardcoded
// default provider id, which would both mislead the Settings UI and risk
// permanently clobbering the real value on the next save.
func TestBuildSettingsPayload_SurfacesUnregisteredTideProviderFromDisk(t *testing.T) {
	settings := map[string]any{
		"ui": map[string]any{
			"tide_provider": "not-a-real-provider",
		},
	}

	payload := buildSettingsPayload(settings)

	if payload.UI.TideProvider != "not-a-real-provider" {
		t.Fatalf("expected tide_provider to surface the stored value, got %q", payload.UI.TideProvider)
	}
}

// TestNormalizeSettingsPayload_PersistsUnregisteredPOIProviderAsSubmitted
// mirrors the tide-provider round-trip test above for poi_provider - added
// alongside the poi plugin kind (docs/adr/0091).
func TestNormalizeSettingsPayload_PersistsUnregisteredPOIProviderAsSubmitted(t *testing.T) {
	req := settingsPayload{}
	req.UI.POIProvider = "not-a-real-provider"

	normalized := normalizeSettingsPayload(req)

	if normalized.UI.POIProvider != "not-a-real-provider" {
		t.Fatalf("expected poi_provider to be persisted as submitted, got %q", normalized.UI.POIProvider)
	}
}

// TestBuildSettingsPayload_SurfacesUnregisteredPOIProviderFromDisk is the
// read-side counterpart, mirroring
// TestBuildSettingsPayload_SurfacesUnregisteredTideProviderFromDisk.
func TestBuildSettingsPayload_SurfacesUnregisteredPOIProviderFromDisk(t *testing.T) {
	settings := map[string]any{
		"ui": map[string]any{
			"poi_provider": "not-a-real-provider",
		},
	}

	payload := buildSettingsPayload(settings)

	if payload.UI.POIProvider != "not-a-real-provider" {
		t.Fatalf("expected poi_provider to surface the stored value, got %q", payload.UI.POIProvider)
	}
}

// TestNormalizeSettingsPayload_PersistsUnregisteredPlaceNameProviderAsSubmitted
// mirrors the poi-provider round-trip test above for place_name_provider
// (docs/adr/0101): the operator's choice of place-names plugin is a
// separate setting from ui.poi_provider and must round-trip the same way,
// even when the named plugin isn't currently registered.
func TestNormalizeSettingsPayload_PersistsUnregisteredPlaceNameProviderAsSubmitted(t *testing.T) {
	req := settingsPayload{}
	req.UI.PlaceNameProvider = "not-a-real-provider"

	normalized := normalizeSettingsPayload(req)

	if normalized.UI.PlaceNameProvider != "not-a-real-provider" {
		t.Fatalf("expected place_name_provider to be persisted as submitted, got %q", normalized.UI.PlaceNameProvider)
	}
}

// TestBuildSettingsPayload_SurfacesUnregisteredPlaceNameProviderFromDisk is
// the read-side counterpart, mirroring
// TestBuildSettingsPayload_SurfacesUnregisteredPOIProviderFromDisk.
func TestBuildSettingsPayload_SurfacesUnregisteredPlaceNameProviderFromDisk(t *testing.T) {
	settings := map[string]any{
		"ui": map[string]any{
			"place_name_provider": "not-a-real-provider",
		},
	}

	payload := buildSettingsPayload(settings)

	if payload.UI.PlaceNameProvider != "not-a-real-provider" {
		t.Fatalf("expected place_name_provider to surface the stored value, got %q", payload.UI.PlaceNameProvider)
	}
}

// TestNormalizeSettingsPayload_RoundTripsInfluxdbSection mirrors the
// tide-provider round-trip test above: the influxdb section (enabled/url/
// org/bucket) must be persisted as submitted, trimmed of whitespace, with no
// silent defaulting or dropping of fields. Note there is deliberately no
// token field here - INFLUXDB_TOKEN is env-only and never round-trips
// through settings.
func TestNormalizeSettingsPayload_RoundTripsInfluxdbSection(t *testing.T) {
	req := settingsPayload{}
	req.Influxdb.Enabled = true
	req.Influxdb.URL = " http://localhost:8086 "
	req.Influxdb.Org = " myorg "
	req.Influxdb.Bucket = " mybucket "

	normalized := normalizeSettingsPayload(req)

	if !normalized.Influxdb.Enabled {
		t.Fatalf("expected influxdb.enabled to round-trip true")
	}
	if normalized.Influxdb.URL != "http://localhost:8086" {
		t.Fatalf("expected influxdb.url to be trimmed, got %q", normalized.Influxdb.URL)
	}
	if normalized.Influxdb.Org != "myorg" {
		t.Fatalf("expected influxdb.org to be trimmed, got %q", normalized.Influxdb.Org)
	}
	if normalized.Influxdb.Bucket != "mybucket" {
		t.Fatalf("expected influxdb.bucket to be trimmed, got %q", normalized.Influxdb.Bucket)
	}
}

// TestBuildSettingsPayload_SurfacesInfluxdbSectionFromDisk is the read-side
// counterpart: GET /api/settings must reflect whatever influxdb section is
// actually stored in settings.yaml.
func TestBuildSettingsPayload_SurfacesInfluxdbSectionFromDisk(t *testing.T) {
	settings := map[string]any{
		"influxdb": map[string]any{
			"enabled": true,
			"url":     "http://localhost:8086",
			"org":     "myorg",
			"bucket":  "mybucket",
		},
	}

	payload := buildSettingsPayload(settings)

	if !payload.Influxdb.Enabled {
		t.Fatalf("expected influxdb.enabled to surface as true")
	}
	if payload.Influxdb.URL != "http://localhost:8086" || payload.Influxdb.Org != "myorg" || payload.Influxdb.Bucket != "mybucket" {
		t.Fatalf("unexpected influxdb section: %+v", payload.Influxdb)
	}
}

// TestSettingsPayloadRoundTripsMayaraBlock drives a real write/read cycle
// through updateSettingsHandler (postSettings, settings_validation_test.go),
// the same path the Settings UI uses, mirroring the Influxdb round trip
// above. Mayara has no address on SignalK's own connection (fetchMayaraTargets,
// radar_source.go) — it needs its own, used only by the radar picture
// overlay — so this pins that address/port survive the save, and that
// saving them does not clobber the rest of the file.
func TestSettingsPayloadRoundTripsMayaraBlock(t *testing.T) {
	settingsPath := writeTestSettings(t, "203.0.113.1", 3000)

	code, body := postSettings(t, settingsPath, func(p *settingsPayload) {
		p.Mayara.Address = "192.168.50.81"
		p.Mayara.Port = 6502
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body %v)", code, body)
	}

	saved, err := readSettings(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	payload := buildSettingsPayload(saved)
	if payload.Mayara.Address != "192.168.50.81" {
		t.Fatalf("expected mayara.address to round-trip, got %q", payload.Mayara.Address)
	}
	if payload.Mayara.Port != 6502 {
		t.Fatalf("expected mayara.port to round-trip, got %d", payload.Mayara.Port)
	}

	// Saving the mayara block must not disturb unrelated fields already on
	// disk (writeTestSettings, settings_validation_test.go).
	if payload.Boat.Model != "Test Boat" {
		t.Fatalf("expected boat.model to survive untouched, got %q", payload.Boat.Model)
	}
	if payload.Boat.VesselPrefix != "M/V" {
		t.Fatalf("expected boat.vessel_prefix to survive untouched, got %q", payload.Boat.VesselPrefix)
	}
	if payload.UI.TankLabels["fuel.2"] != "PORT FWD" {
		t.Fatalf("expected ui.tank_labels to survive untouched, got %+v", payload.UI.TankLabels)
	}
	address, port := persistedSignalK(t, settingsPath)
	if address != "203.0.113.1" || port != 3000 {
		t.Fatalf("expected signalk.address/port to survive untouched, got %s:%d", address, port)
	}
}

// TestNormalizeSettingsPayloadDefaultsMayaraPort pins the port clamp (0 or
// out of range -> 6502, mayara's default REST port) and, separately, that a
// blank address is left blank rather than defaulting to a host the way
// signalk.address defaults to defaultSignalKAddress — there is no sane
// address to guess for a radar, and a guessed one would silently point the
// overlay at the wrong box.
func TestNormalizeSettingsPayloadDefaultsMayaraPort(t *testing.T) {
	for _, tc := range []struct {
		name string
		port int
		want int
	}{
		{"zero clamps to 6502", 0, 6502},
		{"negative clamps to 6502", -1, 6502},
		{"out of range clamps to 6502", 99999, 6502},
		{"a valid port passes through unchanged", 6502, 6502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := settingsPayload{}
			req.Mayara.Port = tc.port
			if got := normalizeSettingsPayload(req).Mayara.Port; got != tc.want {
				t.Fatalf("port %d: got %d, want %d", tc.port, got, tc.want)
			}
		})
	}

	blank := normalizeSettingsPayload(settingsPayload{})
	if blank.Mayara.Address != "" {
		t.Fatalf("expected a blank mayara address to stay blank, got %q", blank.Mayara.Address)
	}
}

// TestValidateSettingsChangeDoesNotProbeMayara pins the deliberate absence of
// a mayara probe: a radar that is switched off, or a mayara box that is
// simply unreachable, must never block an unrelated settings save the way an
// unreachable new SignalK address does (validateSettingsChange above). Both
// addresses are TEST-NET-3 (RFC 5737, see hostPort/postSettings above) so
// this fails fast and without depending on the test machine's network if
// anyone adds a probe here later.
func TestValidateSettingsChangeDoesNotProbeMayara(t *testing.T) {
	current := settingsPayload{}
	current.Signalk.Address = "203.0.113.1"
	current.Signalk.Port = 3000
	current.Mayara.Address = "203.0.113.2"
	current.Mayara.Port = 6502

	next := current
	next.Mayara.Address = "203.0.113.3"
	next.Mayara.Port = 6503

	if invalid := validateSettingsChange(current, next); invalid != nil {
		t.Fatalf("a mayara address/port change must never be probed, got %+v", invalid)
	}
}

// TestSettingsPayloadRoundTripsAssistantBlock drives a real write/read cycle
// through updateSettingsHandler, mirroring the mayara round trip above. It
// also proves multi-line operator standing notes survive a YAML round trip
// (yaml.v3 emits a block scalar for a string containing newlines, and
// readSettings must decode it back to the exact same string).
func TestSettingsPayloadRoundTripsAssistantBlock(t *testing.T) {
	settingsPath := writeTestSettings(t, "203.0.113.1", 3000)
	notes := "Queenfish fish Hill Inlet on a rising tide.\nSnorkel Blue Pearl Bay in the last 1-2h of flood."

	code, body := postSettings(t, settingsPath, func(p *settingsPayload) {
		p.Assistant.Enabled = true
		p.Assistant.Model = "openai/gpt-4o"
		p.Assistant.Notes = notes
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body %v)", code, body)
	}

	saved, err := readSettings(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	payload := buildSettingsPayload(saved)
	if !payload.Assistant.Enabled {
		t.Fatalf("expected assistant.enabled to round-trip true")
	}
	if payload.Assistant.Model != "openai/gpt-4o" {
		t.Fatalf("expected assistant.model to round-trip, got %q", payload.Assistant.Model)
	}
	if payload.Assistant.Notes != notes {
		t.Fatalf("expected multi-line assistant.notes to survive a YAML round trip, got %q", payload.Assistant.Notes)
	}

	// Saving the assistant block must not disturb unrelated fields already on
	// disk (writeTestSettings, settings_validation_test.go).
	if payload.Boat.Model != "Test Boat" {
		t.Fatalf("expected boat.model to survive untouched, got %q", payload.Boat.Model)
	}
	address, port := persistedSignalK(t, settingsPath)
	if address != "203.0.113.1" || port != 3000 {
		t.Fatalf("expected signalk.address/port to survive untouched, got %s:%d", address, port)
	}
}

// TestNormalizeSettingsPayloadDefaultsAssistantModel pins a blank model to
// defaultAssistantModel (the assistant cannot make a chat-completions call
// with no model id) and confirms notes are trimmed but otherwise passed
// through untouched.
func TestNormalizeSettingsPayloadDefaultsAssistantModel(t *testing.T) {
	blank := normalizeSettingsPayload(settingsPayload{})
	if blank.Assistant.Model != defaultAssistantModel {
		t.Fatalf("expected a blank assistant model to default to %q, got %q", defaultAssistantModel, blank.Assistant.Model)
	}
	if blank.Assistant.Enabled {
		t.Fatalf("expected assistant.enabled to default to false")
	}

	req := settingsPayload{}
	req.Assistant.Model = "  openai/gpt-4o  "
	req.Assistant.Notes = "  keep the tide rules handy  "
	normalized := normalizeSettingsPayload(req)
	if normalized.Assistant.Model != "openai/gpt-4o" {
		t.Fatalf("expected assistant.model to be trimmed, got %q", normalized.Assistant.Model)
	}
	if normalized.Assistant.Notes != "keep the tide rules handy" {
		t.Fatalf("expected assistant.notes to be trimmed, got %q", normalized.Assistant.Notes)
	}
}

// TestBuildSettingsPayload_SurfacesBlankAssistantModelFromDisk pins the
// mayara-style unconditional assign: a model explicitly saved as "" on disk
// must surface as "", not be mistaken for "absent" and papered over with
// defaultAssistantModel.
func TestBuildSettingsPayload_SurfacesBlankAssistantModelFromDisk(t *testing.T) {
	settings := map[string]any{
		"assistant": map[string]any{
			"enabled": true,
			"model":   "",
			"notes":   "",
		},
	}

	payload := buildSettingsPayload(settings)
	if payload.Assistant.Model != "" {
		t.Fatalf("expected a blank assistant.model on disk to surface as blank, got %q", payload.Assistant.Model)
	}
	if !payload.Assistant.Enabled {
		t.Fatalf("expected assistant.enabled to surface as true")
	}
}

// TestBuildSettingsPayload_AbsentAssistantBlockDefaults pins the read side
// for settings.yaml files written before the assistant block existed: no
// assistant key at all must yield the same defaults normalizeSettingsPayload
// gives an empty payload, not a zero-value Model that would fail a chat
// completion outright.
func TestBuildSettingsPayload_AbsentAssistantBlockDefaults(t *testing.T) {
	payload := buildSettingsPayload(map[string]any{})
	if payload.Assistant.Enabled {
		t.Fatalf("expected assistant.enabled to default to false when the block is absent")
	}
	if payload.Assistant.Model != defaultAssistantModel {
		t.Fatalf("expected assistant.model to default to %q when the block is absent, got %q", defaultAssistantModel, payload.Assistant.Model)
	}
}

// TestSettingsPayloadRoundTripsAssistantVoiceSettings drives the same
// write/read cycle as TestSettingsPayloadRoundTripsAssistantBlock above for
// the three voice switches (mate-voice-assistant plan phase 1/2): all three
// must round-trip true independently of assistant.enabled/model/notes.
func TestSettingsPayloadRoundTripsAssistantVoiceSettings(t *testing.T) {
	settingsPath := writeTestSettings(t, "203.0.113.1", 3000)

	code, body := postSettings(t, settingsPath, func(p *settingsPayload) {
		p.Assistant.VoiceInput = true
		p.Assistant.ReadAloud = true
		p.Assistant.WakeWord = true
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body %v)", code, body)
	}

	saved, err := readSettings(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	payload := buildSettingsPayload(saved)
	if !payload.Assistant.VoiceInput {
		t.Fatalf("expected assistant.voice_input to round-trip true")
	}
	if !payload.Assistant.ReadAloud {
		t.Fatalf("expected assistant.read_aloud to round-trip true")
	}
	if !payload.Assistant.WakeWord {
		t.Fatalf("expected assistant.wake_word to round-trip true")
	}
}

// TestSettingsPayloadRoundTripsAssistantAutoRouterSettings proves the three
// OpenRouter Auto Router knobs persist through updateSettingsHandler and read
// back through buildSettingsPayload unchanged.
func TestSettingsPayloadRoundTripsAssistantAutoRouterSettings(t *testing.T) {
	settingsPath := writeTestSettings(t, "203.0.113.1", 3000)

	code, body := postSettings(t, settingsPath, func(p *settingsPayload) {
		p.Assistant.AllowedModels = []string{"anthropic/*", "openai/gpt-5*"}
		p.Assistant.ExcludedModels = []string{"openai/gpt-4o-mini"}
		p.Assistant.CostTier = "xhigh"
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body %v)", code, body)
	}

	saved, err := readSettings(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	payload := buildSettingsPayload(saved)
	if got, want := payload.Assistant.AllowedModels, []string{"anthropic/*", "openai/gpt-5*"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("expected assistant.allowed_models to round-trip %v, got %v", want, got)
	}
	if got, want := payload.Assistant.ExcludedModels, []string{"openai/gpt-4o-mini"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("expected assistant.excluded_models to round-trip %v, got %v", want, got)
	}
	if payload.Assistant.CostTier != "xhigh" {
		t.Fatalf("expected assistant.cost_tier to round-trip xhigh, got %q", payload.Assistant.CostTier)
	}
}

// TestSettingsPayloadRoundTripsAssistantDocumentModel mirrors
// TestSettingsPayloadRoundTripsAssistantBlock above for
// assistant.document_model (ADR 0106): a separate model id from the chat
// model, used only by the document indexer's enrich stage.
func TestSettingsPayloadRoundTripsAssistantDocumentModel(t *testing.T) {
	settingsPath := writeTestSettings(t, "203.0.113.1", 3000)

	code, body := postSettings(t, settingsPath, func(p *settingsPayload) {
		p.Assistant.Model = "openai/gpt-4o"
		p.Assistant.DocumentModel = "openai/gpt-4o-mini"
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body %v)", code, body)
	}

	saved, err := readSettings(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	payload := buildSettingsPayload(saved)
	if payload.Assistant.DocumentModel != "openai/gpt-4o-mini" {
		t.Fatalf("expected assistant.document_model to round-trip, got %q", payload.Assistant.DocumentModel)
	}
	if payload.Assistant.Model != "openai/gpt-4o" {
		t.Fatalf("expected assistant.model to survive untouched, got %q", payload.Assistant.Model)
	}
}

// ── ADR 0120: a settings save sweeps the enrich=0 backlog ────────────────
//
// updateSettingsHandler is where the operator's save might be the very
// moment Mate became ready (a key pasted in, a document model chosen), so
// it calls sweepDocumentsIfReady itself rather than waiting for the next
// boot. These tests swap documentIndexerSweepIfReady for a spy - the same
// nil-until-wired package var main() assigns to the real indexer's
// SweepIfReady method (documents_embed_test.go covers what that method
// itself does; this only covers that the handler calls it).

func TestUpdateSettingsHandler_SuccessfulSaveTriggersTheReadySweep(t *testing.T) {
	settingsPath := writeTestSettings(t, "203.0.113.1", 3000)

	swept := make(chan struct{}, 1)
	prev := documentIndexerSweepIfReady
	documentIndexerSweepIfReady = func() { swept <- struct{}{} }
	t.Cleanup(func() { documentIndexerSweepIfReady = prev })

	code, body := postSettings(t, settingsPath, func(p *settingsPayload) {
		p.Assistant.DocumentModel = "openai/gpt-4o-mini"
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body %v)", code, body)
	}
	select {
	case <-swept:
	case <-time.After(2 * time.Second):
		t.Fatal("expected the ready sweep to run after a successful save")
	}
}

// The sweep waits on the document store's lock, which a long indexing write
// can hold for a while. A settings save has nothing to do with that, so it
// must return without waiting for the sweep to finish.
func TestUpdateSettingsHandler_SaveDoesNotWaitForTheReadySweep(t *testing.T) {
	settingsPath := writeTestSettings(t, "203.0.113.1", 3000)

	release := make(chan struct{})
	done := make(chan struct{})
	prev := documentIndexerSweepIfReady
	documentIndexerSweepIfReady = func() { <-release; close(done) }
	t.Cleanup(func() { documentIndexerSweepIfReady = prev })

	returned := make(chan int, 1)
	go func() {
		code, _ := postSettings(t, settingsPath, func(p *settingsPayload) {
			p.Assistant.DocumentModel = "openai/gpt-4o-mini"
		})
		returned <- code
	}()
	select {
	case code := <-returned:
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d", code)
		}
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("settings save blocked on the ready sweep")
	}
	close(release)
	<-done
}

// A save validateSettingsChange refuses must not sweep - nothing was
// actually written, so there is nothing new for Mate to catch up on, and
// running it anyway would be a pointless (if harmless) query on every
// rejected save.
func TestUpdateSettingsHandler_RejectedSaveDoesNotTriggerTheReadySweep(t *testing.T) {
	settingsPath := writeTestSettings(t, "203.0.113.1", 3000)

	var swept int32
	prev := documentIndexerSweepIfReady
	documentIndexerSweepIfReady = func() { atomic.AddInt32(&swept, 1) }
	t.Cleanup(func() { documentIndexerSweepIfReady = prev })

	// current.Auth.Mode defaults to "none" (writeTestSettings' fixture has
	// no auth: block); "sometimes" is not a valid mode, and validating it
	// needs no network probe (TestValidateSettingsChange_RejectsUnknownAuthMode
	// exercises the same check directly).
	code, body := postSettings(t, settingsPath, func(p *settingsPayload) {
		p.Auth.Mode = "sometimes"
	})
	if code != http.StatusBadGateway {
		t.Fatalf("expected 502 for an unknown auth mode, got %d (body %v)", code, body)
	}
	time.Sleep(50 * time.Millisecond) // a wrongly-started background sweep would have run by now
	if atomic.LoadInt32(&swept) != 0 {
		t.Fatalf("expected a rejected save to run no ready sweep, got %d", atomic.LoadInt32(&swept))
	}
}

// TestNormalizeSettingsPayloadDefaultsDocumentModel pins a blank
// document_model to defaultDocumentModel, the same "blank means default"
// pattern defaultAssistantModel uses for the chat model.
func TestNormalizeSettingsPayloadDefaultsDocumentModel(t *testing.T) {
	blank := normalizeSettingsPayload(settingsPayload{})
	if blank.Assistant.DocumentModel != defaultDocumentModel {
		t.Fatalf("expected a blank document model to default to %q, got %q", defaultDocumentModel, blank.Assistant.DocumentModel)
	}

	req := settingsPayload{}
	req.Assistant.DocumentModel = "  openai/gpt-4o-mini  "
	normalized := normalizeSettingsPayload(req)
	if normalized.Assistant.DocumentModel != "openai/gpt-4o-mini" {
		t.Fatalf("expected assistant.document_model to be trimmed, got %q", normalized.Assistant.DocumentModel)
	}
}

// TestBuildSettingsPayload_SurfacesBlankDocumentModelFromDisk mirrors
// TestBuildSettingsPayload_SurfacesBlankAssistantModelFromDisk: a
// document_model explicitly saved as "" on disk must surface as "", not be
// mistaken for "absent" and papered over with defaultDocumentModel.
func TestBuildSettingsPayload_SurfacesBlankDocumentModelFromDisk(t *testing.T) {
	settings := map[string]any{
		"assistant": map[string]any{
			"enabled":        true,
			"model":          "openai/gpt-4o",
			"document_model": "",
		},
	}

	payload := buildSettingsPayload(settings)
	if payload.Assistant.DocumentModel != "" {
		t.Fatalf("expected a blank assistant.document_model on disk to surface as blank, got %q", payload.Assistant.DocumentModel)
	}
}

// TestBuildSettingsPayload_AbsentAssistantBlockDefaultsDocumentModel mirrors
// TestBuildSettingsPayload_AbsentAssistantBlockDefaults: no assistant key at
// all must yield defaultDocumentModel, not a zero-value id that would fail
// an enrich-stage call outright.
func TestBuildSettingsPayload_AbsentAssistantBlockDefaultsDocumentModel(t *testing.T) {
	payload := buildSettingsPayload(map[string]any{})
	if payload.Assistant.DocumentModel != defaultDocumentModel {
		t.Fatalf("expected assistant.document_model to default to %q when the block is absent, got %q", defaultDocumentModel, payload.Assistant.DocumentModel)
	}
}

// TestBuildSettingsPayload_AssistantBlockPredatesDocumentModelKeyDefaults
// pins the actual bug found in review: a settings.yaml written before
// document_model existed (ADR 0106) has an assistant: block - enabled,
// model, notes - but no document_model key at all. Unlike the
// entirely-absent-block case above, assistantMap's own type assertion
// succeeds here, so a naive unconditional coerceString of a missing map key
// returns "" indistinguishably from an operator-saved blank - and that ""
// must NOT win over defaultDocumentModel, or every enrich call on this
// install goes to OpenRouter with "model": "".
func TestBuildSettingsPayload_AssistantBlockPredatesDocumentModelKeyDefaults(t *testing.T) {
	settings := map[string]any{
		"assistant": map[string]any{
			"enabled": true,
			"model":   "openai/gpt-4o",
			"notes":   "",
		},
	}

	payload := buildSettingsPayload(settings)
	if payload.Assistant.DocumentModel != defaultDocumentModel {
		t.Fatalf("expected a document_model-less assistant block to default to %q, got %q", defaultDocumentModel, payload.Assistant.DocumentModel)
	}
	if payload.Assistant.Model != "openai/gpt-4o" {
		t.Fatalf("expected assistant.model to survive untouched, got %q", payload.Assistant.Model)
	}
}

// TestSettingsPayloadRoundTripsAssistantEmbeddingSettings mirrors
// TestSettingsPayloadRoundTripsAssistantDocumentModel above for
// assistant.embedding_model and assistant.embedding_dimensions (E1b): the
// document indexer's semantic-search embedding model and vector size, both
// separate from the chat and document models.
func TestSettingsPayloadRoundTripsAssistantEmbeddingSettings(t *testing.T) {
	settingsPath := writeTestSettings(t, "203.0.113.1", 3000)

	code, body := postSettings(t, settingsPath, func(p *settingsPayload) {
		p.Assistant.Model = "openai/gpt-4o"
		p.Assistant.EmbeddingModel = "openai/text-embedding-3-large"
		p.Assistant.EmbeddingDimensions = 1024
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body %v)", code, body)
	}

	saved, err := readSettings(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	payload := buildSettingsPayload(saved)
	if payload.Assistant.EmbeddingModel != "openai/text-embedding-3-large" {
		t.Fatalf("expected assistant.embedding_model to round-trip, got %q", payload.Assistant.EmbeddingModel)
	}
	if payload.Assistant.EmbeddingDimensions != 1024 {
		t.Fatalf("expected assistant.embedding_dimensions to round-trip, got %d", payload.Assistant.EmbeddingDimensions)
	}
	if payload.Assistant.Model != "openai/gpt-4o" {
		t.Fatalf("expected assistant.model to survive untouched, got %q", payload.Assistant.Model)
	}
}

// TestNormalizeSettingsPayloadKeepsABlankEmbeddingModelBlank pins the one
// place embedding_model must NOT follow document_model's "blank means
// default" rule. A blank embedding model is the documented off switch (ADR
// 0108, settings.example.yaml, docs/reference/configuration.md): defaulting
// it on a save would mean the operator can never turn semantic search off
// through the settings API, and worse, that the first save after an upgrade
// silently switches paid embedding on for every consented document. The
// default for an absent key is applied by buildSettingsPayload instead - the
// same split Anchor.AutoRaiseOnMotoring already uses for exactly this
// "can't tell absent from explicitly off" reason.
func TestNormalizeSettingsPayloadKeepsABlankEmbeddingModelBlank(t *testing.T) {
	blank := normalizeSettingsPayload(settingsPayload{})
	if blank.Assistant.EmbeddingModel != "" {
		t.Fatalf("expected a blank embedding model to stay blank, got %q", blank.Assistant.EmbeddingModel)
	}

	req := settingsPayload{}
	req.Assistant.EmbeddingModel = "   "
	if got := normalizeSettingsPayload(req).Assistant.EmbeddingModel; got != "" {
		t.Fatalf("expected a whitespace-only embedding model to normalise to blank, got %q", got)
	}

	req.Assistant.EmbeddingModel = "  openai/text-embedding-3-large  "
	normalized := normalizeSettingsPayload(req)
	if normalized.Assistant.EmbeddingModel != "openai/text-embedding-3-large" {
		t.Fatalf("expected assistant.embedding_model to be trimmed, got %q", normalized.Assistant.EmbeddingModel)
	}
}

// TestBuildSettingsPayloadEmbeddingModelAbsentVsExplicitlyBlank is the other
// half of the rule above: an absent key is a settings.yaml written before
// embedding_model existed and must default, while a key present and blank is
// an operator who turned semantic search off and must stay off.
func TestBuildSettingsPayloadEmbeddingModelAbsentVsExplicitlyBlank(t *testing.T) {
	absent := buildSettingsPayload(map[string]any{"assistant": map[string]any{"model": "m"}})
	if absent.Assistant.EmbeddingModel != defaultEmbeddingModel {
		t.Fatalf("expected an absent embedding_model to default to %q, got %q", defaultEmbeddingModel, absent.Assistant.EmbeddingModel)
	}

	off := buildSettingsPayload(map[string]any{"assistant": map[string]any{"model": "m", "embedding_model": ""}})
	if off.Assistant.EmbeddingModel != "" {
		t.Fatalf("expected an explicitly blank embedding_model to stay blank, got %q", off.Assistant.EmbeddingModel)
	}
}

// TestNormalizeSettingsPayloadDefaultsEmbeddingDimensions pins the embedding
// dimensions rule: <= 0 defaults to defaultEmbeddingDimensions (there is no
// sane zero/negative vector size), and anything above 4096 is capped there
// rather than reset to the default - a larger vector is a configuration
// mistake, not a preference, and every one of them is scanned in Go on an
// armv7 box.
func TestNormalizeSettingsPayloadDefaultsEmbeddingDimensions(t *testing.T) {
	for _, tc := range []struct {
		name string
		dims int
		want int
	}{
		{"zero defaults", 0, defaultEmbeddingDimensions},
		{"negative defaults", -1, defaultEmbeddingDimensions},
		{"in range passes through unchanged", 768, 768},
		{"exactly at the cap passes through unchanged", 4096, 4096},
		{"above the cap is capped, not defaulted", 8192, 4096},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := settingsPayload{}
			req.Assistant.EmbeddingDimensions = tc.dims
			if got := normalizeSettingsPayload(req).Assistant.EmbeddingDimensions; got != tc.want {
				t.Fatalf("dims %d: got %d, want %d", tc.dims, got, tc.want)
			}
		})
	}
}

// TestBuildSettingsPayload_SurfacesBlankEmbeddingModelFromDisk mirrors
// TestBuildSettingsPayload_SurfacesBlankDocumentModelFromDisk: an
// embedding_model explicitly saved as "" on disk must surface as "" (turning
// semantic search off, per the settings doc comment), not be mistaken for
// "absent" and papered over with defaultEmbeddingModel.
func TestBuildSettingsPayload_SurfacesBlankEmbeddingModelFromDisk(t *testing.T) {
	settings := map[string]any{
		"assistant": map[string]any{
			"enabled":         true,
			"model":           "openai/gpt-4o",
			"document_model":  "openai/gpt-4o-mini",
			"embedding_model": "",
		},
	}

	payload := buildSettingsPayload(settings)
	if payload.Assistant.EmbeddingModel != "" {
		t.Fatalf("expected a blank assistant.embedding_model on disk to surface as blank, got %q", payload.Assistant.EmbeddingModel)
	}
}

// TestBuildSettingsPayload_AbsentAssistantBlockDefaultsEmbeddingSettings
// mirrors TestBuildSettingsPayload_AbsentAssistantBlockDefaultsDocumentModel:
// no assistant key at all must yield the same defaults
// normalizeSettingsPayload gives an empty payload.
func TestBuildSettingsPayload_AbsentAssistantBlockDefaultsEmbeddingSettings(t *testing.T) {
	payload := buildSettingsPayload(map[string]any{})
	if payload.Assistant.EmbeddingModel != defaultEmbeddingModel {
		t.Fatalf("expected assistant.embedding_model to default to %q when the block is absent, got %q", defaultEmbeddingModel, payload.Assistant.EmbeddingModel)
	}
	if payload.Assistant.EmbeddingDimensions != defaultEmbeddingDimensions {
		t.Fatalf("expected assistant.embedding_dimensions to default to %d when the block is absent, got %d", defaultEmbeddingDimensions, payload.Assistant.EmbeddingDimensions)
	}
}

// TestBuildSettingsPayload_AssistantBlockPredatesEmbeddingModelKeyDefaults
// mirrors TestBuildSettingsPayload_AssistantBlockPredatesDocumentModelKeyDefaults:
// a settings.yaml written before embedding_model existed (E1b) has an
// assistant: block - enabled, model, document_model - but no embedding_model
// key at all. A naive unconditional coerceString of a missing map key
// returns "" indistinguishably from an operator-saved blank, and that ""
// must NOT win over defaultEmbeddingModel, or semantic search silently
// turns itself off on every install that predates this setting.
func TestBuildSettingsPayload_AssistantBlockPredatesEmbeddingModelKeyDefaults(t *testing.T) {
	settings := map[string]any{
		"assistant": map[string]any{
			"enabled":        true,
			"model":          "openai/gpt-4o",
			"document_model": "google/gemini-2.5-flash",
		},
	}

	payload := buildSettingsPayload(settings)
	if payload.Assistant.EmbeddingModel != defaultEmbeddingModel {
		t.Fatalf("expected an embedding_model-less assistant block to default to %q, got %q", defaultEmbeddingModel, payload.Assistant.EmbeddingModel)
	}
	if payload.Assistant.DocumentModel != "google/gemini-2.5-flash" {
		t.Fatalf("expected assistant.document_model to survive untouched, got %q", payload.Assistant.DocumentModel)
	}
}

// TestBuildSettingsPayload_SurfacesEmbeddingDimensionsFromDisk mirrors
// TestNormalizeSettingsPayloadDefaultsMayaraPort's clamp table for the
// read-from-disk path: a valid on-disk value survives, an invalid or absent
// one falls back to the default, and an above-cap value is capped rather
// than defaulted.
func TestBuildSettingsPayload_SurfacesEmbeddingDimensionsFromDisk(t *testing.T) {
	for _, tc := range []struct {
		name string
		dims any
		want int
	}{
		{"a valid value passes through", 768, 768},
		{"zero defaults", 0, defaultEmbeddingDimensions},
		{"negative defaults", -1, defaultEmbeddingDimensions},
		{"above the cap is capped, not defaulted", 8192, 4096},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := map[string]any{
				"assistant": map[string]any{
					"enabled":              true,
					"embedding_dimensions": tc.dims,
				},
			}
			payload := buildSettingsPayload(settings)
			if payload.Assistant.EmbeddingDimensions != tc.want {
				t.Fatalf("embedding_dimensions %v: got %d, want %d", tc.dims, payload.Assistant.EmbeddingDimensions, tc.want)
			}
		})
	}

	// Absent key: no embedding_dimensions at all must default the same way
	// an absent document_model defaults, not surface as 0.
	payload := buildSettingsPayload(map[string]any{"assistant": map[string]any{"enabled": true}})
	if payload.Assistant.EmbeddingDimensions != defaultEmbeddingDimensions {
		t.Fatalf("expected an absent embedding_dimensions key to default to %d, got %d", defaultEmbeddingDimensions, payload.Assistant.EmbeddingDimensions)
	}
}

func TestNormalizeSettingsPayload_AssistantAutoRouterFieldsTrimmedAndValidated(t *testing.T) {
	req := settingsPayload{}
	req.Assistant.AllowedModels = []string{"  anthropic/*  ", "", "  openai/gpt-5*"}
	req.Assistant.ExcludedModels = []string{"", "  openai/gpt-4o-mini  "}
	req.Assistant.CostTier = "  XHIGH  "

	normalized := normalizeSettingsPayload(req)

	if got, want := normalized.Assistant.AllowedModels, []string{"anthropic/*", "openai/gpt-5*"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("expected trimmed assistant.allowed_models %v, got %v", want, got)
	}
	if got, want := normalized.Assistant.ExcludedModels, []string{"openai/gpt-4o-mini"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("expected trimmed assistant.excluded_models %v, got %v", want, got)
	}
	if normalized.Assistant.CostTier != "xhigh" {
		t.Fatalf("expected lowercase assistant.cost_tier xhigh, got %q", normalized.Assistant.CostTier)
	}

	req.Assistant.CostTier = "not-a-tier"
	normalized = normalizeSettingsPayload(req)
	if normalized.Assistant.CostTier != "" {
		t.Fatalf("expected invalid assistant.cost_tier to normalize to blank, got %q", normalized.Assistant.CostTier)
	}
}

// TestBuildSettingsPayload_AbsentAssistantBlockDefaultsVoiceSettingsFalse
// mirrors TestBuildSettingsPayload_AbsentAssistantBlockDefaults above for the
// three voice switches: a settings.yaml written before they existed (or with
// no assistant block at all) must surface every one of them as false, never
// a guessed true.
func TestBuildSettingsPayload_AbsentAssistantBlockDefaultsVoiceSettingsFalse(t *testing.T) {
	payload := buildSettingsPayload(map[string]any{})
	if payload.Assistant.VoiceInput {
		t.Fatalf("expected assistant.voice_input to default to false when the block is absent")
	}
	if payload.Assistant.ReadAloud {
		t.Fatalf("expected assistant.read_aloud to default to false when the block is absent")
	}
	if payload.Assistant.WakeWord {
		t.Fatalf("expected assistant.wake_word to default to false when the block is absent")
	}
}

// TestNormalizeSettingsPayload_PreservesGPSFromBowMZero guards the anchor
// bow-offset correction (see docs on setAnchorWatch): gps_from_bow_m
// defaults to 0, meaning "no correction", so 0 is a meaningful explicit
// value rather than an absent one. Unlike every other anchor field, it must
// survive load -> normalize -> emit without being clamped up to a
// non-zero default — a guessed antenna position is worse than none.
func TestNormalizeSettingsPayload_PreservesGPSFromBowMZero(t *testing.T) {
	req := settingsPayload{}
	req.Anchor.GPSFromBowM = 0

	normalized := normalizeSettingsPayload(req)

	if normalized.Anchor.GPSFromBowM != 0 {
		t.Fatalf("expected gps_from_bow_m to stay 0, got %v", normalized.Anchor.GPSFromBowM)
	}
}

// TestNormalizeSettingsPayload_RoundTripsGPSFromBowMNonZero is the
// counterpart: a configured, non-zero value must also round-trip exactly.
func TestNormalizeSettingsPayload_RoundTripsGPSFromBowMNonZero(t *testing.T) {
	req := settingsPayload{}
	req.Anchor.GPSFromBowM = 8.2

	normalized := normalizeSettingsPayload(req)

	if normalized.Anchor.GPSFromBowM != 8.2 {
		t.Fatalf("expected gps_from_bow_m to round-trip as 8.2, got %v", normalized.Anchor.GPSFromBowM)
	}
}

// TestBuildSettingsPayload_SurfacesGPSFromBowMZeroFromDisk is the read-side
// counterpart: an explicit 0 stored on disk must surface as 0, not be
// mistaken for "unset" and replaced with a default.
func TestBuildSettingsPayload_SurfacesGPSFromBowMZeroFromDisk(t *testing.T) {
	settings := map[string]any{
		"anchor": map[string]any{
			"gps_from_bow_m": 0,
		},
	}

	payload := buildSettingsPayload(settings)

	if payload.Anchor.GPSFromBowM != 0 {
		t.Fatalf("expected gps_from_bow_m to surface as 0, got %v", payload.Anchor.GPSFromBowM)
	}
}

// TestBuildSettingsPayload_SurfacesGPSFromBowMFromDisk mirrors the above for
// a configured non-zero value.
func TestBuildSettingsPayload_SurfacesGPSFromBowMFromDisk(t *testing.T) {
	settings := map[string]any{
		"anchor": map[string]any{
			"gps_from_bow_m": 8.5,
		},
	}

	payload := buildSettingsPayload(settings)

	if payload.Anchor.GPSFromBowM != 8.5 {
		t.Fatalf("expected gps_from_bow_m to surface as 8.5, got %v", payload.Anchor.GPSFromBowM)
	}
}

// TestBuildSettingsPayload_DefaultsAutoRaiseOnMotoringTrueWhenAbsent guards
// the auto-raise watcher's setting (anchor_auto_raise.go, ADR 0099). Unlike
// gps_from_bow_m (whose meaningful default is 0), this field's default is
// true — the feature ships on for every existing installation — so a
// settings.yaml written before it existed, or with an anchor block that
// simply omits the key, must surface true, never a guessed false.
func TestBuildSettingsPayload_DefaultsAutoRaiseOnMotoringTrueWhenAbsent(t *testing.T) {
	if got := buildSettingsPayload(map[string]any{}).Anchor.AutoRaiseOnMotoring; !got {
		t.Fatalf("expected auto_raise_on_motoring to default true with no anchor block at all, got %v", got)
	}

	settings := map[string]any{"anchor": map[string]any{"gps_from_bow_m": 8.0}}
	if got := buildSettingsPayload(settings).Anchor.AutoRaiseOnMotoring; !got {
		t.Fatalf("expected auto_raise_on_motoring to default true when the anchor block omits it, got %v", got)
	}
}

// TestBuildSettingsPayload_SurfacesAutoRaiseOnMotoringExplicitValue is the
// counterpart: an operator's explicit choice, in either direction, must
// survive — a false must not be defaulted back to true, and a true must not
// be mistaken for "unset."
func TestBuildSettingsPayload_SurfacesAutoRaiseOnMotoringExplicitValue(t *testing.T) {
	off := map[string]any{"anchor": map[string]any{"auto_raise_on_motoring": false}}
	if got := buildSettingsPayload(off).Anchor.AutoRaiseOnMotoring; got {
		t.Fatalf("expected an explicit false on disk to surface as false, got %v", got)
	}

	on := map[string]any{"anchor": map[string]any{"auto_raise_on_motoring": true}}
	if got := buildSettingsPayload(on).Anchor.AutoRaiseOnMotoring; !got {
		t.Fatalf("expected an explicit true on disk to surface as true, got %v", got)
	}
}

// TestNormalizeSettingsPayload_RoundTripsAutoRaiseOnMotoring pins that
// normalizeSettingsPayload passes the submitted value straight through with
// no override in either direction. The default-true behaviour lives only in
// buildSettingsPayload's disk-read step above — a plain bool can't tell "the
// operator submitted false" from "this field is absent from the request," so
// normalizeSettingsPayload (which also produces buildSettingsPayload's
// from-empty baseline) must not try to default it itself.
func TestNormalizeSettingsPayload_RoundTripsAutoRaiseOnMotoring(t *testing.T) {
	req := settingsPayload{}
	req.Anchor.AutoRaiseOnMotoring = true
	if got := normalizeSettingsPayload(req).Anchor.AutoRaiseOnMotoring; !got {
		t.Fatalf("expected true to round-trip as true, got %v", got)
	}

	req.Anchor.AutoRaiseOnMotoring = false
	if got := normalizeSettingsPayload(req).Anchor.AutoRaiseOnMotoring; got {
		t.Fatalf("expected false to round-trip as false, got %v", got)
	}
}

func TestParseSignalKCurrent_ReadsSetTrueAndDrift(t *testing.T) {
	payload := map[string]any{
		"environment": map[string]any{
			"current": map[string]any{
				"drift":   map[string]any{"value": 0.643},  // m/s -> ~1.2 kt
				"setTrue": map[string]any{"value": 2.4697}, // radians -> ~141.5 deg
			},
		},
	}

	drift, setDeg, _ := parseSignalKCurrent(payload)
	if !approxEqual(drift, 1.2, 0.05) {
		t.Fatalf("expected drift ~1.2 kt, got %v", drift)
	}
	if !approxEqual(setDeg, 141.5, 0.1) {
		t.Fatalf("expected set ~141.5 deg, got %v", setDeg)
	}
}

func TestParseSignalKCurrent_DoesNotTreatSetMagneticAsTrue(t *testing.T) {
	payload := map[string]any{
		"environment": map[string]any{
			"current": map[string]any{
				"drift":       map[string]any{"value": 0.5},
				"setMagnetic": map[string]any{"value": 1.5707963267948966}, // 90 deg magnetic
			},
		},
	}

	_, setDeg, _ := parseSignalKCurrent(payload)
	if setDeg != -1 {
		t.Fatalf("expected -1 (no reliable true bearing) when only setMagnetic is present, got %v", setDeg)
	}
}

func TestParseSignalKCurrent_ReadsDriftImpact(t *testing.T) {
	payload := map[string]any{
		"environment": map[string]any{
			"current": map[string]any{
				"drift":       map[string]any{"value": 0.5},
				"driftImpact": map[string]any{"value": -0.643}, // m/s -> ~-1.2 kt
			},
		},
	}

	_, _, driftImpactKts := parseSignalKCurrent(payload)
	if driftImpactKts == nil {
		t.Fatalf("expected driftImpactKts to be present")
	}
	if !approxEqual(*driftImpactKts, -1.2, 0.05) {
		t.Fatalf("expected driftImpactKts ~-1.2 kt, got %v", *driftImpactKts)
	}
}

func TestParseSignalKCurrent_NilDriftImpactWhenMissing(t *testing.T) {
	payload := map[string]any{
		"environment": map[string]any{
			"current": map[string]any{
				"drift": map[string]any{"value": 0.5},
			},
		},
	}

	_, _, driftImpactKts := parseSignalKCurrent(payload)
	if driftImpactKts != nil {
		t.Fatalf("expected nil driftImpactKts when driftImpact is absent, got %v", *driftImpactKts)
	}
}

// With no stream data and no prior fix there is nothing to freeze at, so the
// position must read as the unknown sentinel rather than a plausible-looking
// coordinate.
func TestFetchSignalKVesselState_MarksCriticalWithNoStreamDataAndNoPriorFix(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)
	withGlobalSnapshot(t, newSignalKSnapshot())

	state, err := fetchSignalKVesselState()
	if err == nil {
		t.Fatalf("expected error when the stream carries no data")
	}
	if !state.GNSSCriticalAlert {
		t.Fatalf("expected gnss critical alert when the stream carries no data")
	}
	if state.GNSSValidationReason == "" {
		t.Fatalf("expected a validation reason explaining the failure")
	}
	if state.GNSSSatellites != -1 {
		t.Fatalf("expected -1 satellites with no stream data, got %d", state.GNSSSatellites)
	}
	if state.Latitude != -1 || state.Longitude != -1 {
		t.Fatalf("expected -1,-1 with no prior trusted fix, got %.4f %.4f", state.Latitude, state.Longitude)
	}
}

// The anchor alarm must be able to tell "the stream went away" apart from "the
// vessel moved". Losing the stream has to freeze position at the last trusted
// fix, never report a jump.
func TestFetchSignalKVesselState_FreezesLastTrustedPositionWhenStreamStops(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	seedSelfTree(t, trustedSignalKPayload(-25.2939, 152.9103))

	state, err := fetchSignalKVesselState()
	if err != nil {
		t.Fatalf("unexpected error establishing trusted fix: %v", err)
	}
	if state.GNSSCriticalAlert {
		t.Fatalf("expected trusted fix to not be critical")
	}
	if state.Latitude != -25.2939 || state.Longitude != 152.9103 {
		t.Fatalf("expected trusted fix to pass through, got %.4f %.4f", state.Latitude, state.Longitude)
	}
	if state.GNSSSatellites != 8 {
		t.Fatalf("expected 8 satellites from trusted payload, got %d", state.GNSSSatellites)
	}

	// The stream drops: no data for any context.
	withGlobalSnapshot(t, newSignalKSnapshot())

	state, err = fetchSignalKVesselState()
	if err == nil {
		t.Fatalf("expected error once the stream stops carrying data")
	}
	if !state.GNSSCriticalAlert {
		t.Fatalf("expected gnss critical alert once the stream stops")
	}
	if state.Latitude != -25.2939 || state.Longitude != 152.9103 {
		t.Fatalf("expected position frozen at last trusted fix, got %.4f %.4f", state.Latitude, state.Longitude)
	}
}

// TestFetchSignalKNearbyVessels_ParsesStringMMSI is a regression test for a
// real bug found via live verification against this app's actual SignalK
// server: it encodes "mmsi" as a JSON string (e.g. "316042555"), not a JSON
// number. lookupNumber only handles float64/int, so it silently returned -1
// for every real-world vessel and Mmsi was always "" - contact tracking was
// unknowingly running entirely on the name-based fallback key. The fix must
// read mmsi as a string first, since that's the real-world format.
func TestFetchSignalKNearbyVessels_ParsesStringMMSI(t *testing.T) {
	body := []byte(`{
		"self": {
			"mmsi": "518999323",
			"name": "Pikorua",
			"navigation": {"position": {"value": {"latitude": -21.595297, "longitude": 149.796444}}}
		},
		"urn:mrn:imo:mmsi:316042555": {
			"mmsi": "316042555",
			"name": "TAKU X",
			"navigation": {"position": {"value": {"latitude": -21.592353, "longitude": 149.780485}}}
		}
	}`)

	seedVesselTrees(t, string(body))

	vessels, err := fetchSignalKNearbyVessels(-21.595297, 149.796444, time.Now().UTC(), nil)
	if err != nil {
		t.Fatalf("fetchSignalKNearbyVessels: %v", err)
	}
	if len(vessels) != 1 {
		t.Fatalf("expected 1 nearby vessel, got %d", len(vessels))
	}
	if vessels[0].Mmsi != "316042555" {
		t.Fatalf("expected MMSI '316042555' parsed from a JSON string field, got %q", vessels[0].Mmsi)
	}
}

// TestFetchSignalKNearbyVessels_ParsesNumericMMSI covers the other valid
// SignalK encoding (a bare JSON number), so the fix doesn't regress a
// server that sends mmsi that way instead.
func TestFetchSignalKNearbyVessels_ParsesNumericMMSI(t *testing.T) {
	body := []byte(`{
		"self": {
			"mmsi": 518999323,
			"name": "Pikorua",
			"navigation": {"position": {"value": {"latitude": -21.595297, "longitude": 149.796444}}}
		},
		"urn:mrn:imo:mmsi:316042555": {
			"mmsi": 316042555,
			"name": "TAKU X",
			"navigation": {"position": {"value": {"latitude": -21.592353, "longitude": 149.780485}}}
		}
	}`)

	seedVesselTrees(t, string(body))

	vessels, err := fetchSignalKNearbyVessels(-21.595297, 149.796444, time.Now().UTC(), nil)
	if err != nil {
		t.Fatalf("fetchSignalKNearbyVessels: %v", err)
	}
	if len(vessels) != 1 {
		t.Fatalf("expected 1 nearby vessel, got %d", len(vessels))
	}
	if vessels[0].Mmsi != "316042555" {
		t.Fatalf("expected MMSI '316042555' parsed from a JSON number field, got %q", vessels[0].Mmsi)
	}
}

// vesselTreeAt returns a single-vessel GET .../vessels-shaped fixture at the
// given position, for the staleness tests below where each vessel's age is
// what's under test, not its lat/lon.
func vesselTreeAt(id, name string, latitude, longitude float64) string {
	return fmt.Sprintf(`{%q: {"name": %q, "navigation": {"position": {"value": {"latitude": %f, "longitude": %f}}}}}`, id, name, latitude, longitude)
}

// TestFetchSignalKNearbyVessels_DropsStaleVessels is the core regression test
// for the ghost-contact bug: a vessel whose position hasn't been refreshed in
// over nearbyVesselMaxAge must not be reported as a live target.
func TestFetchSignalKNearbyVessels_DropsStaleVessels(t *testing.T) {
	body := `{
		"fresh-vessel": {"name": "FRESH", "navigation": {"position": {"value": {"latitude": -21.592353, "longitude": 149.780485}}}},
		"stale-vessel": {"name": "GHOST", "navigation": {"position": {"value": {"latitude": -21.592453, "longitude": 149.780585}}}}
	}`
	now := time.Now().UTC()
	seedVesselTreesAged(t, body, map[string]time.Duration{
		"fresh-vessel": 30 * time.Second,
		"stale-vessel": 11 * time.Minute,
	}, now)

	vessels, err := fetchSignalKNearbyVessels(-21.595297, 149.796444, now, nil)
	if err != nil {
		t.Fatalf("fetchSignalKNearbyVessels: %v", err)
	}
	if len(vessels) != 1 {
		t.Fatalf("expected 1 nearby vessel (the fresh one), got %d: %+v", len(vessels), vessels)
	}
	if vessels[0].Name != "FRESH" {
		t.Fatalf("expected the fresh vessel to survive, got %q", vessels[0].Name)
	}
}

// TestFetchSignalKNearbyVessels_KeepsVesselAtCutoffBoundary asserts the
// comparison is strictly greater-than: a vessel aged exactly nearbyVesselMaxAge
// has not yet exceeded it and must still be reported.
func TestFetchSignalKNearbyVessels_KeepsVesselAtCutoffBoundary(t *testing.T) {
	body := vesselTreeAt("boundary-vessel", "BOUNDARY", -21.592353, 149.780485)
	now := time.Now().UTC()
	seedVesselTreesAged(t, body, map[string]time.Duration{"boundary-vessel": nearbyVesselMaxAge}, now)

	vessels, err := fetchSignalKNearbyVessels(-21.595297, 149.796444, now, nil)
	if err != nil {
		t.Fatalf("fetchSignalKNearbyVessels: %v", err)
	}
	if len(vessels) != 1 {
		t.Fatalf("expected the vessel at exactly the cutoff to be kept, got %d vessels", len(vessels))
	}
}

// TestFetchSignalKNearbyVessels_DropsVesselWithNoPositionDelta covers a
// vessel context that exists in the tree but has never had a
// navigation.position delta recorded in pathSeen. There is nothing to age
// against, so it must be dropped rather than silently reported as age 0 -
// the same masking fallback the old ageSeconds parse failure produced.
func TestFetchSignalKNearbyVessels_DropsVesselWithNoPositionDelta(t *testing.T) {
	body := vesselTreeAt("no-delta-vessel", "NODELTA", -21.592353, 149.780485)
	seedVesselTreesAged(t, body, map[string]time.Duration{}, time.Now().UTC())
	// Remove the freshness stamp seedVesselTreesAged would otherwise apply,
	// reproducing a tree entry with no recorded position delta at all.
	delete(globalSignalKSnapshot.pathSeen, vesselContextPrefix+"no-delta-vessel|navigation.position")

	vessels, err := fetchSignalKNearbyVessels(-21.595297, 149.796444, time.Now().UTC(), nil)
	if err != nil {
		t.Fatalf("fetchSignalKNearbyVessels: %v", err)
	}
	if len(vessels) != 0 {
		t.Fatalf("expected a vessel with no position delta to be dropped, got %d: %+v", len(vessels), vessels)
	}
}

// TestFetchSignalKNearbyVessels_AgeSecondsFromReceiveTime asserts age_seconds
// tracks delta receive time, not the AIS-reported navigation.position.timestamp
// - including when that timestamp disagrees with receive time, and when it is
// unparseable (which must be filtered normally on receive time, not silently
// reported as age 0 as the old timestamp-parse fallback did).
func TestFetchSignalKNearbyVessels_AgeSecondsFromReceiveTime(t *testing.T) {
	now := time.Now().UTC()
	body := fmt.Sprintf(`{
		"disagreeing-vessel": {
			"name": "DISAGREE",
			"navigation": {"position": {"value": {"latitude": -21.592353, "longitude": 149.780485}, "timestamp": %q}}
		},
		"unparseable-vessel": {
			"name": "BADTS",
			"navigation": {"position": {"value": {"latitude": -21.592453, "longitude": 149.780585}, "timestamp": "not-a-timestamp"}}
		}
	}`, now.Add(-2*time.Hour).Format(time.RFC3339))

	seedVesselTreesAged(t, body, map[string]time.Duration{
		"disagreeing-vessel": 45 * time.Second,
		"unparseable-vessel": 45 * time.Second,
	}, now)

	vessels, err := fetchSignalKNearbyVessels(-21.595297, 149.796444, now, nil)
	if err != nil {
		t.Fatalf("fetchSignalKNearbyVessels: %v", err)
	}
	if len(vessels) != 2 {
		t.Fatalf("expected both vessels within the age cutoff to be reported, got %d: %+v", len(vessels), vessels)
	}
	for _, v := range vessels {
		if v.AgeSeconds < 44 || v.AgeSeconds > 46 {
			t.Fatalf("expected age_seconds ~45s from receive time regardless of the AIS timestamp field, got %d for %q", v.AgeSeconds, v.Name)
		}
	}
}

// TestFetchSignalKNearbyVessels_StaleFilterAppliedBeforeTopTenCap is the
// regression test for the reported screenshot: 11 stale, close ghosts must
// not crowd a fresh, more distant vessel out of the top-10 truncation.
func TestFetchSignalKNearbyVessels_StaleFilterAppliedBeforeTopTenCap(t *testing.T) {
	now := time.Now().UTC()
	trees := make([]string, 0, 12)
	ages := map[string]time.Duration{}
	for i := 0; i < 11; i++ {
		id := fmt.Sprintf("ghost-%d", i)
		// A tiny lat offset per ghost keeps them all close (well under 1km)
		// and at distinct positions, all closer than the fresh vessel below.
		trees = append(trees, fmt.Sprintf(`%q: {"name": "GHOST%d", "navigation": {"position": {"value": {"latitude": %f, "longitude": 149.780485}}}}`, id, i, -21.593000-float64(i)*0.0001))
		ages[id] = 11 * time.Minute
	}
	// A fresh vessel roughly 3km out - farther than every ghost, but it must
	// still survive because the ghosts are dropped before the top-10 cap.
	trees = append(trees, `"fresh-distant": {"name": "FARAWAY", "navigation": {"position": {"value": {"latitude": -21.622000, "longitude": 149.780485}}}}`)
	ages["fresh-distant"] = 30 * time.Second

	body := "{" + strings.Join(trees, ",") + "}"
	seedVesselTreesAged(t, body, ages, now)

	vessels, err := fetchSignalKNearbyVessels(-21.595297, 149.796444, now, nil)
	if err != nil {
		t.Fatalf("fetchSignalKNearbyVessels: %v", err)
	}
	if len(vessels) != 1 {
		t.Fatalf("expected only the fresh distant vessel to survive, got %d: %+v", len(vessels), vessels)
	}
	if vessels[0].Name != "FARAWAY" {
		t.Fatalf("expected FARAWAY to survive the stale-then-cap filtering, got %q", vessels[0].Name)
	}
}

// TestFetchSignalKNearbyVessels_ReportsRangeInMeters asserts RangeM against a
// known haversine distance, and that the 5000m cutoff is a metres boundary,
// not a feet-derived one.
func TestFetchSignalKNearbyVessels_ReportsRangeInMeters(t *testing.T) {
	const selfLat, selfLon = -21.595297, 149.796444
	const otherLat, otherLon = -21.592353, 149.780485
	wantRangeM := math.Round(haversineMeters(selfLat, selfLon, otherLat, otherLon)*10) / 10

	body := vesselTreeAt("known-distance-vessel", "KNOWNDIST", otherLat, otherLon)
	now := time.Now().UTC()
	seedVesselTreesAged(t, body, nil, now)

	vessels, err := fetchSignalKNearbyVessels(selfLat, selfLon, now, nil)
	if err != nil {
		t.Fatalf("fetchSignalKNearbyVessels: %v", err)
	}
	if len(vessels) != 1 {
		t.Fatalf("expected 1 nearby vessel, got %d", len(vessels))
	}
	if vessels[0].RangeM != wantRangeM {
		t.Fatalf("expected RangeM %v, got %v", wantRangeM, vessels[0].RangeM)
	}

	// Just inside vs. just beyond the 5000m horizon.
	const closeLat = -21.595297
	nearOffsetDeg := 4900.0 / 111320.0 // ~4900m north
	farOffsetDeg := 5100.0 / 111320.0  // ~5100m north
	insideBody := vesselTreeAt("inside-vessel", "INSIDE", closeLat+nearOffsetDeg, selfLon)
	outsideBody := vesselTreeAt("outside-vessel", "OUTSIDE", closeLat+farOffsetDeg, selfLon)
	combined := `{` +
		`"inside-vessel": ` + mustExtractSingleVesselTree(t, insideBody, "inside-vessel") + `,` +
		`"outside-vessel": ` + mustExtractSingleVesselTree(t, outsideBody, "outside-vessel") +
		`}`
	seedVesselTreesAged(t, combined, nil, now)

	vessels, err = fetchSignalKNearbyVessels(selfLat, selfLon, now, nil)
	if err != nil {
		t.Fatalf("fetchSignalKNearbyVessels: %v", err)
	}
	if len(vessels) != 1 || vessels[0].Name != "INSIDE" {
		t.Fatalf("expected only the vessel inside 5000m to be kept, got %+v", vessels)
	}
}

// mustExtractSingleVesselTree pulls the inner vessel object back out of a
// vesselTreeAt fixture, so two single-vessel fixtures can be recombined into
// one multi-vessel body without hand-duplicating the JSON.
func mustExtractSingleVesselTree(t *testing.T, body, id string) string {
	t.Helper()
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("mustExtractSingleVesselTree: %v", err)
	}
	raw, ok := payload[id]
	if !ok {
		t.Fatalf("mustExtractSingleVesselTree: id %q not found in body", id)
	}
	return string(raw)
}

// TestFetchSignalKNearbyVessels_PopulatesStableID reproduces the duplicate
// React key bug at the seam where it originates: two vessels sharing a name
// must still come back with distinct, non-empty IDs.
func TestFetchSignalKNearbyVessels_PopulatesStableID(t *testing.T) {
	body := `{
		"urn:mrn:imo:mmsi:111111111": {"name": "SAME NAME", "navigation": {"position": {"value": {"latitude": -21.592353, "longitude": 149.780485}}}},
		"urn:mrn:imo:mmsi:222222222": {"name": "SAME NAME", "navigation": {"position": {"value": {"latitude": -21.592453, "longitude": 149.780585}}}}
	}`
	now := time.Now().UTC()
	seedVesselTreesAged(t, body, nil, now)

	vessels, err := fetchSignalKNearbyVessels(-21.595297, 149.796444, now, nil)
	if err != nil {
		t.Fatalf("fetchSignalKNearbyVessels: %v", err)
	}
	if len(vessels) != 2 {
		t.Fatalf("expected 2 nearby vessels, got %d", len(vessels))
	}
	if vessels[0].ID == "" || vessels[1].ID == "" {
		t.Fatalf("expected every vessel to have a non-empty ID, got %+v", vessels)
	}
	if vessels[0].ID == vessels[1].ID {
		t.Fatalf("expected distinct IDs for two vessels sharing a name, both got %q", vessels[0].ID)
	}
}

func TestFetchSignalKElectricalState_ReadsCharger0Fields(t *testing.T) {
	body := []byte(`{
		"timestamp": "2026-07-22T00:00:00Z",
		"electrical": {
			"chargers": {
				"0": {
					"current": {"value": 42.39},
					"acin": {
						"1": {
							"current": {"value": 8.14}
						}
					},
					"chargingMode": {"value": "bulk"},
					"error": {"value": "none"}
				}
			}
		}
	}`)

	seedSelfTree(t, string(body))

	state, err := fetchSignalKElectricalState()
	if err != nil {
		t.Fatalf("fetchSignalKElectricalState: %v", err)
	}

	if !approxEqual(state.Charger0.CurrentA, 42.4, 0.01) {
		t.Fatalf("expected charger_0_current_a 42.4, got %v", state.Charger0.CurrentA)
	}
	if !approxEqual(state.Charger0.ACIn1CurrentA, 8.1, 0.01) {
		t.Fatalf("expected charger_0_acin_1_current_a 8.1, got %v", state.Charger0.ACIn1CurrentA)
	}
	if state.Charger0.ChargingMode != "bulk" {
		t.Fatalf("expected charging mode 'bulk', got %q", state.Charger0.ChargingMode)
	}
	if state.Charger0.Error != "none" {
		t.Fatalf("expected error 'none', got %q", state.Charger0.Error)
	}
}

func TestFetchSignalKElectricalState_ReadsCharger0MixedShapes(t *testing.T) {
	body := []byte(`{
		"timestamp": "2026-07-22T00:00:00Z",
		"electrical": {
			"chargers": {
				"0": {
					"current": 17.76,
					"acin": {
						"1": {
							"current": 5.26
						}
					},
					"chargingMode": "float",
					"error": ""
				}
			}
		}
	}`)

	seedSelfTree(t, string(body))

	state, err := fetchSignalKElectricalState()
	if err != nil {
		t.Fatalf("fetchSignalKElectricalState: %v", err)
	}

	if !approxEqual(state.Charger0.CurrentA, 17.8, 0.01) {
		t.Fatalf("expected charger_0_current_a 17.8, got %v", state.Charger0.CurrentA)
	}
	if !approxEqual(state.Charger0.ACIn1CurrentA, 5.3, 0.01) {
		t.Fatalf("expected charger_0_acin_1_current_a 5.3, got %v", state.Charger0.ACIn1CurrentA)
	}
	if state.Charger0.ChargingMode != "float" {
		t.Fatalf("expected charging mode 'float', got %q", state.Charger0.ChargingMode)
	}
	if state.Charger0.Error != "" {
		t.Fatalf("expected empty error string when absent, got %q", state.Charger0.Error)
	}
}

func TestFetchSignalKSolarState_ReadsControllersAndAggregate(t *testing.T) {
	body := []byte(`{
		"timestamp": "2026-07-22T00:00:00Z",
		"electrical": {
			"venus": {
				"totalPanelPower": {"value": 1120.4}
			},
			"solar": {
				"0": {
					"panelPower": {"value": 410.3},
					"yieldToday": {"value": 1.7},
					"yieldYesterday": {"value": 1.6},
					"chargingMode": {"value": "bulk"},
					"error": {"value": "none"}
				},
				"1": {
					"panelPower": {"value": 370.7},
					"yieldToday": {"value": 1.4},
					"yieldYesterday": {"value": 1.5},
					"mode": {"value": "absorption"},
					"error": {"value": ""}
				}
			}
		}
	}`)

	seedSelfTree(t, string(body))

	state, err := fetchSignalKSolarState()
	if err != nil {
		t.Fatalf("fetchSignalKSolarState: %v", err)
	}

	if !approxEqual(state.CurrentW, 1120.4, 0.01) {
		t.Fatalf("expected aggregate current from venus 1120.4, got %v", state.CurrentW)
	}
	if !approxEqual(state.TodayKWh, 3.1, 0.01) {
		t.Fatalf("expected today_kwh 3.1, got %v", state.TodayKWh)
	}
	if !approxEqual(state.YesterdayKWh, 3.1, 0.01) {
		t.Fatalf("expected yesterday_kwh 3.1, got %v", state.YesterdayKWh)
	}
	if len(state.Controllers) != 2 {
		t.Fatalf("expected 2 controllers, got %d", len(state.Controllers))
	}
	if state.Controllers[0].Label != "Port" {
		t.Fatalf("expected first controller label Port, got %q", state.Controllers[0].Label)
	}
	if state.Controllers[1].Mode != "absorption" {
		t.Fatalf("expected second controller mode absorption, got %q", state.Controllers[1].Mode)
	}
}

func TestFetchSignalKSolarState_NormalizesWhYieldToKWh(t *testing.T) {
	body := []byte(`{
		"timestamp": "2026-07-22T00:00:00Z",
		"electrical": {
			"solar": {
				"0": {
					"panelPower": 250,
					"yieldToday": 1450,
					"yieldYesterday": 1300
				}
			}
		}
	}`)

	seedSelfTree(t, string(body))

	state, err := fetchSignalKSolarState()
	if err != nil {
		t.Fatalf("fetchSignalKSolarState: %v", err)
	}

	if !approxEqual(state.TodayKWh, 1.45, 0.001) {
		t.Fatalf("expected yieldToday converted to 1.45 kWh, got %v", state.TodayKWh)
	}
	if !approxEqual(state.YesterdayKWh, 1.3, 0.001) {
		t.Fatalf("expected yieldYesterday converted to 1.3 kWh, got %v", state.YesterdayKWh)
	}
}

func TestFetchSignalKSolarState_ReportsAgeOfFreshestController(t *testing.T) {
	// Sample time is 2026-09-02T00:00:00Z; the two controllers last reported
	// 600s and 300s before that. The state-level age tracks the freshest of
	// them, because a source is only as stale as its most recent update.
	body := []byte(`{
		"timestamp": "2026-09-02T00:00:00Z",
		"electrical": {
			"solar": {
				"0": {
					"panelPower": {"value": 0, "timestamp": "2026-09-01T23:50:00Z"}
				},
				"1": {
					"panelPower": {"value": 0, "timestamp": "2026-09-01T23:55:00Z"}
				}
			}
		}
	}`)

	seedSelfTree(t, string(body))

	state, err := fetchSignalKSolarState()
	if err != nil {
		t.Fatalf("fetchSignalKSolarState: %v", err)
	}

	if !approxEqual(state.Controllers[0].LastUpdateAge, 600, 0.01) {
		t.Fatalf("expected controller 0 age 600s, got %v", state.Controllers[0].LastUpdateAge)
	}
	if !approxEqual(state.Controllers[1].LastUpdateAge, 300, 0.01) {
		t.Fatalf("expected controller 1 age 300s, got %v", state.Controllers[1].LastUpdateAge)
	}
	if !approxEqual(state.LastUpdateAge, 300, 0.01) {
		t.Fatalf("expected state age to track freshest controller (300s), got %v", state.LastUpdateAge)
	}
}

func TestFetchSignalKSolarState_AgeIsUnknownWithoutTimestamps(t *testing.T) {
	// No per-controller timestamps means we cannot claim the data is fresh.
	// -1 is this codebase's "unknown", and must not be confused with 0s old.
	body := []byte(`{
		"timestamp": "2026-09-02T00:00:00Z",
		"electrical": {
			"solar": {
				"0": {"panelPower": {"value": 240.0}}
			}
		}
	}`)

	seedSelfTree(t, string(body))

	state, err := fetchSignalKSolarState()
	if err != nil {
		t.Fatalf("fetchSignalKSolarState: %v", err)
	}

	if state.LastUpdateAge != -1 {
		t.Fatalf("expected unknown age (-1) when no controller carries a timestamp, got %v", state.LastUpdateAge)
	}
}

func TestFetchSignalKElectricalState_ReportsFreshestElectricalTimestamp(t *testing.T) {
	// The electrical tile draws from paths scattered across the electrical
	// subtree, so its freshness is that of the most recent one: if anything
	// under electrical is still reporting, the feed is alive.
	body := []byte(`{
		"timestamp": "2026-09-02T00:00:00Z",
		"electrical": {
			"batteries": {
				"house": {
					"capacity": {"stateOfCharge": {"value": 0.46, "timestamp": "2026-09-01T23:50:00Z"}}
				}
			},
			"solar": {
				"0": {"panelPower": {"value": 0, "timestamp": "2026-09-01T23:58:00Z"}}
			}
		}
	}`)

	seedSelfTree(t, string(body))

	state, err := fetchSignalKElectricalState()
	if err != nil {
		t.Fatalf("fetchSignalKElectricalState: %v", err)
	}

	if !approxEqual(state.LastUpdateAge, 120, 0.01) {
		t.Fatalf("expected age of freshest electrical timestamp (120s), got %v", state.LastUpdateAge)
	}
}

func TestFetchSignalKElectricalState_AgeIsUnknownWithoutTimestamps(t *testing.T) {
	body := []byte(`{
		"timestamp": "2026-09-02T00:00:00Z",
		"electrical": {
			"batteries": {"house": {"capacity": {"stateOfCharge": {"value": 0.46}}}}
		}
	}`)

	seedSelfTree(t, string(body))

	state, err := fetchSignalKElectricalState()
	if err != nil {
		t.Fatalf("fetchSignalKElectricalState: %v", err)
	}

	if state.LastUpdateAge != -1 {
		t.Fatalf("expected unknown age (-1) with no timestamps present, got %v", state.LastUpdateAge)
	}
}

// TestFetchSignalKVesselState_ReportsDepthLastUpdateAge extends the
// last_update_age_s mechanism (ADR 0068) to depth: the number an operator
// runs aground on if a dead transducer freezes it forever. Scoped to
// environment.depth.belowTransducer specifically (not the whole
// environment.depth branch) so a live belowSurface/belowKeel sensor cannot
// paper over a dead belowTransducer reading, which is the value this tile
// actually renders.
func TestFetchSignalKVesselState_ReportsDepthLastUpdateAge(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	seedSelfTree(t, `{
		"timestamp": "2026-09-05T00:00:00Z",
		"environment": {"depth": {"belowTransducer": {"value": 4.8, "timestamp": "2026-09-04T23:58:00Z"}}}
	}`)

	state, err := fetchSignalKVesselState()
	if err != nil {
		t.Fatalf("fetchSignalKVesselState: %v", err)
	}

	if !approxEqual(state.DepthLastUpdateAge, 120, 0.01) {
		t.Fatalf("expected depth age 120s, got %v", state.DepthLastUpdateAge)
	}
}

func TestFetchSignalKVesselState_DepthLastUpdateAgeUnknownWithoutTimestamp(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	seedSelfTree(t, `{
		"timestamp": "2026-09-05T00:00:00Z",
		"environment": {"depth": {"belowTransducer": {"value": 4.8}}}
	}`)

	state, err := fetchSignalKVesselState()
	if err != nil {
		t.Fatalf("fetchSignalKVesselState: %v", err)
	}

	if state.DepthLastUpdateAge != -1 {
		t.Fatalf("expected unknown depth age (-1) with no timestamp present, got %v", state.DepthLastUpdateAge)
	}
}

// TestFetchSignalKVesselState_ReportsPositionLastUpdateAge covers the GNSS
// fix age used by both the Position tile and (via the same age, plumbed
// through separately) the Anchor Watch tile's drag detector. The fix's
// freshness depends on paths under two different parents -
// navigation.position (the lat/lon itself) and navigation.gnss (quality,
// HDOP, satellite count) - so the age is the freshest of the two: either one
// still updating means the fix is current.
func TestFetchSignalKVesselState_ReportsPositionLastUpdateAge(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	seedSelfTree(t, `{
		"timestamp": "2026-09-05T00:00:00Z",
		"navigation": {
			"position": {"value": {"latitude": 1.0, "longitude": 2.0}, "timestamp": "2026-09-04T23:55:00Z"},
			"gnss": {"satellites": {"value": 9, "timestamp": "2026-09-04T23:59:00Z"}}
		}
	}`)

	state, err := fetchSignalKVesselState()
	if err != nil {
		t.Fatalf("fetchSignalKVesselState: %v", err)
	}

	if !approxEqual(state.PositionLastUpdateAge, 60, 0.01) {
		t.Fatalf("expected position age to track the freshest of position/gnss (60s), got %v", state.PositionLastUpdateAge)
	}
}

func TestFetchSignalKVesselState_PositionLastUpdateAgeUnknownWithoutTimestamp(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	seedSelfTree(t, `{
		"timestamp": "2026-09-05T00:00:00Z",
		"navigation": {"position": {"value": {"latitude": 1.0, "longitude": 2.0}}}
	}`)

	state, err := fetchSignalKVesselState()
	if err != nil {
		t.Fatalf("fetchSignalKVesselState: %v", err)
	}

	if state.PositionLastUpdateAge != -1 {
		t.Fatalf("expected unknown position age (-1) with no timestamp present, got %v", state.PositionLastUpdateAge)
	}
}

// TestFetchSignalKVesselState_ReportsWindLastUpdateAge covers the Wind
// tile's age, scoped to the environment.wind subtree only - current
// (environment.current) is a different sensor with its own health and must
// not be folded into wind's freshness.
func TestFetchSignalKVesselState_ReportsWindLastUpdateAge(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	seedSelfTree(t, `{
		"timestamp": "2026-09-05T00:00:00Z",
		"environment": {
			"wind": {
				"speedApparent": {"value": 5.1, "timestamp": "2026-09-04T23:59:30Z"},
				"angleApparent": {"value": 0.5, "timestamp": "2026-09-04T23:59:00Z"}
			}
		}
	}`)

	state, err := fetchSignalKVesselState()
	if err != nil {
		t.Fatalf("fetchSignalKVesselState: %v", err)
	}

	if !approxEqual(state.WindLastUpdateAge, 30, 0.01) {
		t.Fatalf("expected wind age to track the freshest wind timestamp (30s), got %v", state.WindLastUpdateAge)
	}
}

func TestFetchSignalKVesselState_WindLastUpdateAgeUnknownWithoutTimestamp(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	seedSelfTree(t, `{
		"timestamp": "2026-09-05T00:00:00Z",
		"environment": {"wind": {"speedApparent": {"value": 5.1}}}
	}`)

	state, err := fetchSignalKVesselState()
	if err != nil {
		t.Fatalf("fetchSignalKVesselState: %v", err)
	}

	if state.WindLastUpdateAge != -1 {
		t.Fatalf("expected unknown wind age (-1) with no timestamp present, got %v", state.WindLastUpdateAge)
	}
}

// TestFetchSignalKTanksState_ReportsPerTankLastUpdateAge mirrors the solar
// controller pattern (ADR 0068): each tank carries its own age, scoped to
// that tank's own subtree, exactly as each solar controller does.
func TestFetchSignalKTanksState_ReportsPerTankLastUpdateAge(t *testing.T) {
	seedSelfTree(t, `{
		"timestamp": "2026-09-05T00:00:00Z",
		"tanks": {
			"freshWater": {
				"0": {"currentLevel": {"value": 0.8, "timestamp": "2026-09-04T23:58:00Z"}}
			},
			"fuel": {
				"0": {"currentLevel": {"value": 0.5, "timestamp": "2026-09-04T23:55:00Z"}}
			}
		}
	}`)

	overrides := map[string]string{"freshwater.0": "Fresh Water", "fuel.0": "Diesel"}
	tanks, _, err := fetchSignalKTanksState(overrides)
	if err != nil {
		t.Fatalf("fetchSignalKTanksState: %v", err)
	}
	if len(tanks) != 2 {
		t.Fatalf("expected 2 tanks, got %d: %+v", len(tanks), tanks)
	}

	byID := map[string]tankLevelData{}
	for _, tank := range tanks {
		byID[tank.ID] = tank
	}

	freshWater, ok := byID["freshWater.0"]
	if !ok {
		t.Fatalf("expected a freshWater.0 tank, got %+v", tanks)
	}
	if !approxEqual(freshWater.LastUpdateAge, 120, 0.01) {
		t.Fatalf("expected freshWater.0 age 120s, got %v", freshWater.LastUpdateAge)
	}

	fuel, ok := byID["fuel.0"]
	if !ok {
		t.Fatalf("expected a fuel.0 tank, got %+v", tanks)
	}
	if !approxEqual(fuel.LastUpdateAge, 300, 0.01) {
		t.Fatalf("expected fuel.0 age 300s, got %v", fuel.LastUpdateAge)
	}
}

func TestFetchSignalKTanksState_LastUpdateAgeUnknownWithoutTimestamp(t *testing.T) {
	seedSelfTree(t, `{
		"timestamp": "2026-09-05T00:00:00Z",
		"tanks": {"freshWater": {"0": {"currentLevel": {"value": 0.8}}}}
	}`)

	overrides := map[string]string{"freshwater.0": "Fresh Water"}
	tanks, _, err := fetchSignalKTanksState(overrides)
	if err != nil {
		t.Fatalf("fetchSignalKTanksState: %v", err)
	}
	if len(tanks) != 1 {
		t.Fatalf("expected 1 tank, got %d: %+v", len(tanks), tanks)
	}
	if tanks[0].LastUpdateAge != -1 {
		t.Fatalf("expected unknown tank age (-1) with no timestamp present, got %v", tanks[0].LastUpdateAge)
	}
}

func TestFreshestAge_ReturnsMinimumOfKnownAges(t *testing.T) {
	if got := freshestAge(300, 120, 600); got != 120 {
		t.Fatalf("expected freshest (minimum) age 120, got %v", got)
	}
}

func TestFreshestAge_IgnoresUnknownEntries(t *testing.T) {
	if got := freshestAge(-1, 45, -1); got != 45 {
		t.Fatalf("expected the one known age (45) to win over unknowns, got %v", got)
	}
}

func TestFreshestAge_AllUnknownReturnsUnknown(t *testing.T) {
	if got := freshestAge(-1, -1); got != -1 {
		t.Fatalf("expected -1 when no age is known, got %v", got)
	}
}

// TestFreshestAge_NoArgumentsReturnsUnknown matters for nearby vessels: zero
// vessels in range is silence, not evidence the AIS feed died, so the
// aggregate age must stay -1 rather than defaulting to 0 ("just measured").
func TestFreshestAge_NoArgumentsReturnsUnknown(t *testing.T) {
	if got := freshestAge(); got != -1 {
		t.Fatalf("expected -1 for an empty set of ages, got %v", got)
	}
}
