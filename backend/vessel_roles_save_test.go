package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// The roles block saves through POST /api/settings with the rest of the
// settings page. These tests pin that: the block is written and anomaly rules
// seeded, a payload that omits it leaves it alone, GET /api/settings returns
// it, and alarm rules that follow the house bank move with it.

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

func TestReseedAfterAnEngineItemIsMappedSeedsTheFrozenRule(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	withTempAlarmRules(t)
	store := vesselFixtureStore(t)

	engine, err := store.CreateEquipment(equipmentItem{Name: "Port engine", System: "propulsion"})
	if err != nil {
		t.Fatal(err)
	}
	engine.SignalKSource = "propulsion.port"
	if _, err := store.UpdateEquipment(engine.ID, engine); err != nil {
		t.Fatal(err)
	}
	invalidateSourceItems()
	reseedAnomalyRulesAfterItemSave(engine.ID)

	var found bool
	for _, rule := range listAlarmRules() {
		if rule.Path == anomalySensorFrozenCountPath {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected mapping an engine to seed the frozen rule")
	}
}

func TestUpdateSettings_AbsentRolesKeepsTheStoredBlock(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	withTempAlarmRules(t)

	if err := saveVesselFixture(t, settingsPath, vesselView{
		Engines:   []vesselEngineSetting{{Instance: "port", Name: "Port"}},
		HouseBank: &vesselHouseBankSetting{Path: "electrical.batteries.0", CapacityAh: 400, Cells: 8},
	}); err != nil {
		t.Fatalf("seed vessel: %v", err)
	}

	current, err := readSettings(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	raw, err := json.Marshal(buildSettingsPayload(current))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	delete(generic, "roles")
	generic["units"] = "imperial"
	raw, _ = json.Marshal(generic)

	rec := postRawSettings(t, string(raw))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	saved, err := loadVesselView(settingsPath)
	if err != nil {
		t.Fatalf("loadVesselView: %v", err)
	}
	if len(saved.Engines) != 1 || saved.HouseBank == nil || saved.HouseBank.CapacityAh != 400 {
		t.Fatalf("a payload without roles must not wipe the block, got %+v", saved)
	}
}

func TestUpdateSettings_ClearingRolesPersists(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	withTempAlarmRules(t)

	if err := saveVesselFixture(t, settingsPath, vesselView{
		HouseBank: &vesselHouseBankSetting{Path: "electrical.batteries.0", CapacityAh: 400, Cells: 8},
	}); err != nil {
		t.Fatalf("seed vessel: %v", err)
	}

	code, _ := postSettings(t, settingsPath, func(p *settingsPayload) {
		p.Roles = &vesselRoles{}
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	saved, err := loadVesselView(settingsPath)
	if err != nil {
		t.Fatalf("loadVesselView: %v", err)
	}
	if saved.HouseBank != nil {
		t.Fatalf("an explicit empty roles block should clear the roles, got %+v", saved)
	}
}

func TestGetSettings_ReturnsTheRolesBlock(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)

	if err := saveVesselFixture(t, settingsPath, vesselView{
		HouseBank: &vesselHouseBankSetting{Path: "electrical.batteries.0"},
	}); err != nil {
		t.Fatalf("seed vessel: %v", err)
	}
	view, _ := loadVesselView(settingsPath)

	e := echo.New()
	rec := httptest.NewRecorder()
	if err := getSettingsHandler(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/settings", nil), rec)); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	var got settingsPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Roles == nil || got.Roles.HouseBank == "" || got.Roles.HouseBank != view.HouseBank.EquipmentID {
		t.Fatalf("GET /api/settings should carry the roles block, got %+v", got.Roles)
	}
}

func TestGetSettings_FreshInstallReportsEmptyRoles(t *testing.T) {
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
	roles, ok := got["roles"].(map[string]any)
	if !ok {
		t.Fatalf("expected a roles object on a fresh install, got %v", got["roles"])
	}
	if roles["house_bank"] != "" {
		t.Fatalf("expected every role blank, got %v", roles)
	}
	if _, present := got["vessel"]; present {
		t.Fatalf("the vessel block is gone from the payload, got %v", got["vessel"])
	}
}

func TestPostVesselRouteIsRetired(t *testing.T) {
	for _, route := range buildAPIRoutes(newTestSessionStore(t), newWorldImageryHTTPClient()) {
		if route.Path == "/api/vessel" || route.Path == "/api/vessel/candidates" {
			t.Fatalf("%s %s should be gone; vessel discovery is /api/vessel/discovery and roles save through /api/settings", route.Method, route.Path)
		}
	}
}

func TestGetSettings_CorruptRolesBlockKeepsTheRestAndSaysWhy(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)

	settings, err := readSettings(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	settings["roles"] = map[string]any{"house_bank": []any{"not", "text"}}
	if err := writeSettings(settingsPath, settings); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	e := echo.New()
	rec := httptest.NewRecorder()
	if err := getSettingsHandler(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/settings", nil), rec)); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("a corrupt roles block must not take out the rest of the settings, got %d", rec.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, present := got["roles"]; present {
		t.Fatalf("roles should be omitted when they cannot be read, got %v", got["roles"])
	}
	if msg, _ := got["roles_error"].(string); !strings.Contains(msg, "roles") {
		t.Fatalf("expected a roles_error naming the problem, got %v", got["roles_error"])
	}
	boat, _ := got["boat"].(map[string]any)
	if boat["model"] != "Test Boat" {
		t.Fatalf("other settings should still be present, got boat %v", got["boat"])
	}
}

// An alarm rule that follows the house bank role moves when the role does, and
// stops watching anything when the role is cleared.
func TestChangingTheHouseBankMovesRulesThatFollowIt(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	withTempAlarmRules(t)
	store := vesselFixtureStore(t)
	mk := func(name, source string) string {
		item, err := store.CreateEquipment(equipmentItem{Name: name, SignalKSource: source})
		if err != nil {
			t.Fatal(err)
		}
		return item.ID
	}
	bankA := mk("Lithium bank", "electrical.batteries.512")
	bankB := mk("Other bank", "electrical.batteries.3")

	if code, body := postSettings(t, settingsPath, func(p *settingsPayload) { p.Roles = &vesselRoles{HouseBank: bankA} }); code != http.StatusOK {
		t.Fatalf("got %d %v", code, body)
	}
	rule, err := createAlarmRule(alarmRule{Label: "House low", PathRole: alarmPathRoleHouseBankSoC, Op: "below", Value: 0.2, State: "warn", Enabled: true})
	if err != nil {
		t.Fatalf("createAlarmRule: %v", err)
	}
	if rule.Path != "electrical.batteries.512.capacity.stateOfCharge" {
		t.Fatalf("a role rule takes the role's path, got %q", rule.Path)
	}

	if code, body := postSettings(t, settingsPath, func(p *settingsPayload) { p.Roles = &vesselRoles{HouseBank: bankB} }); code != http.StatusOK {
		t.Fatalf("got %d %v", code, body)
	}
	moved, _ := getAlarmRule(rule.ID)
	if moved.Path != "electrical.batteries.3.capacity.stateOfCharge" {
		t.Fatalf("the rule should follow the new house bank, got %q", moved.Path)
	}

	// Remapping the bank item itself moves the rule too.
	bank, _ := store.GetEquipment(bankB)
	bank.SignalKSource = "electrical.batteries.9"
	if _, err := store.UpdateEquipment(bankB, bank); err != nil {
		t.Fatal(err)
	}
	invalidateSourceItems()
	reseedAnomalyRulesAfterItemSave(bankB)
	moved, _ = getAlarmRule(rule.ID)
	if moved.Path != "electrical.batteries.9.capacity.stateOfCharge" {
		t.Fatalf("the rule should follow the bank item's new source, got %q", moved.Path)
	}

	if code, _ := postSettings(t, settingsPath, func(p *settingsPayload) { p.Roles = &vesselRoles{} }); code != http.StatusOK {
		t.Fatalf("got %d", code)
	}
	cleared, _ := getAlarmRule(rule.ID)
	if cleared.Path != "" {
		t.Fatalf("with no house bank the rule must watch nothing, got %q", cleared.Path)
	}
}

func TestRoleRuleNeedsAHouseBank(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	withTempAlarmRules(t)

	_, err := createAlarmRule(alarmRule{Label: "House low", PathRole: alarmPathRoleHouseBankSoC, Op: "below", Value: 0.2, State: "warn", Enabled: true})
	if err == nil || !strings.Contains(err.Error(), "house bank") {
		t.Fatalf("a rule that follows the house bank needs one chosen first, got %v", err)
	}
	if _, err := createAlarmRule(alarmRule{Label: "x", PathRole: "bogus", Op: "below", Value: 1, State: "warn", Enabled: true}); err == nil {
		t.Fatalf("an unknown role must be refused")
	}
}
