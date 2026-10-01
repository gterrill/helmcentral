package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// The vessel block (engines, house bank) now saves through POST /api/settings
// with the rest of the settings page. These tests pin that: the block is
// written and anomaly rules seeded, a payload that omits it leaves it alone,
// and GET /api/settings returns it.

func postRawSettings(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	if err := updateSettingsHandler(e.NewContext(req, rec)); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	return rec
}

func TestUpdateSettings_WritesVesselBlockAndSeedsAnomalyRules(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	withTempAlarmRules(t)

	code, _ := postSettings(t, settingsPath, func(p *settingsPayload) {
		p.Vessel = &vesselSettings{
			Engines: []vesselEngineSetting{{Instance: "port", Name: "Port"}},
		}
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}

	saved, err := loadVesselSettings(settingsPath)
	if err != nil {
		t.Fatalf("loadVesselSettings: %v", err)
	}
	if len(saved.Engines) != 1 || saved.Engines[0].Instance != "port" || saved.Engines[0].Name != "Port" {
		t.Fatalf("expected the engine to persist, got %+v", saved)
	}

	var found bool
	for _, rule := range listAlarmRules() {
		if rule.Path == anomalySensorFrozenCountPath {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the save to seed the frozen rule now that an engine is configured")
	}
}

func TestUpdateSettings_AbsentVesselKeepsTheStoredBlock(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	withTempAlarmRules(t)

	if err := saveVesselSettings(settingsPath, vesselSettings{
		Engines:   []vesselEngineSetting{{Instance: "port", Name: "Port"}},
		HouseBank: &vesselHouseBankSetting{Path: "electrical.batteries.0", CapacityAh: 400, Cells: 8},
	}); err != nil {
		t.Fatalf("seed vessel block: %v", err)
	}

	current, err := readSettings(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	payload := buildSettingsPayload(current)
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	delete(generic, "vessel")
	generic["units"] = "imperial"
	raw, _ = json.Marshal(generic)

	rec := postRawSettings(t, string(raw))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	saved, err := loadVesselSettings(settingsPath)
	if err != nil {
		t.Fatalf("loadVesselSettings: %v", err)
	}
	if len(saved.Engines) != 1 || saved.HouseBank == nil || saved.HouseBank.CapacityAh != 400 {
		t.Fatalf("a payload without vessel must not wipe the block, got %+v", saved)
	}
}

func TestUpdateSettings_ClearingVesselEnginesPersists(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	withTempAlarmRules(t)

	if err := saveVesselSettings(settingsPath, vesselSettings{
		Engines: []vesselEngineSetting{{Instance: "port", Name: "Port"}},
	}); err != nil {
		t.Fatalf("seed vessel block: %v", err)
	}

	code, _ := postSettings(t, settingsPath, func(p *settingsPayload) {
		p.Vessel = &vesselSettings{}
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	saved, err := loadVesselSettings(settingsPath)
	if err != nil {
		t.Fatalf("loadVesselSettings: %v", err)
	}
	if len(saved.Engines) != 0 || saved.HouseBank != nil {
		t.Fatalf("an explicit empty vessel block should clear the setup, got %+v", saved)
	}
}

func TestGetSettings_ReturnsTheVesselBlock(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)

	if err := saveVesselSettings(settingsPath, vesselSettings{
		Engines: []vesselEngineSetting{{Instance: "port", Name: "Port", EquipmentID: "eq-1"}},
	}); err != nil {
		t.Fatalf("seed vessel block: %v", err)
	}

	e := echo.New()
	rec := httptest.NewRecorder()
	if err := getSettingsHandler(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/settings", nil), rec)); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	var got settingsPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Vessel == nil || len(got.Vessel.Engines) != 1 || got.Vessel.Engines[0].EquipmentID != "eq-1" {
		t.Fatalf("GET /api/settings should carry the vessel block, got %+v", got.Vessel)
	}
}

func TestGetSettings_FreshInstallReportsAnEmptyVesselBlock(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	rec := httptest.NewRecorder()
	if err := getSettingsHandler(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/settings", nil), rec)); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	vessel, ok := got["vessel"].(map[string]any)
	if !ok {
		t.Fatalf("expected a vessel object on a fresh install, got %v", got["vessel"])
	}
	if engines, _ := vessel["engines"].([]any); engines == nil || len(engines) != 0 {
		t.Fatalf("expected engines to be an empty list, got %v", vessel["engines"])
	}
}

func TestPostVesselRouteIsRetired(t *testing.T) {
	for _, route := range buildAPIRoutes(newTestSessionStore(t), newWorldImageryHTTPClient()) {
		if route.Path == "/api/vessel" {
			t.Fatalf("%s /api/vessel should be gone; vessel settings save through /api/settings", route.Method)
		}
	}
}

func TestGetSettings_CorruptVesselBlockKeepsTheRestAndSaysWhy(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)

	settings, err := readSettings(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	settings["vessel"] = map[string]any{"engines": "not a list"}
	if err := writeSettings(settingsPath, settings); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	e := echo.New()
	rec := httptest.NewRecorder()
	if err := getSettingsHandler(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/settings", nil), rec)); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("a corrupt vessel block must not take out the rest of the settings, got %d", rec.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, present := got["vessel"]; present {
		t.Fatalf("vessel should be omitted when it cannot be read, got %v", got["vessel"])
	}
	if msg, _ := got["vessel_error"].(string); !strings.Contains(msg, "vessel") {
		t.Fatalf("expected a vessel_error naming the problem, got %v", got["vessel_error"])
	}
	boat, _ := got["boat"].(map[string]any)
	if boat["model"] != "Test Boat" {
		t.Fatalf("other settings should still be present, got boat %v", got["boat"])
	}
}
