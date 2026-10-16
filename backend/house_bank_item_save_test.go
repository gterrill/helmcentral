package main

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// houseBankRuleFixture sets up a house bank item that owns a source, the role
// naming it, and a rule that follows the role.
func houseBankRuleFixture(t *testing.T) (bankID string, rule alarmRule) {
	t.Helper()
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	withTempAlarmRules(t)
	withTestDocumentStore(t)
	bank, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Lithium bank", SignalKSource: "electrical.batteries.512"})
	if err != nil {
		t.Fatal(err)
	}
	invalidateSourceItems()
	if code, body := postSettings(t, settingsPath, func(p *settingsPayload) { p.Roles = &vesselRoles{HouseBank: bank.ID} }); code != http.StatusOK {
		t.Fatalf("got %d %v", code, body)
	}
	rule, err = createAlarmRule(alarmRule{Label: "House low", PathRole: alarmPathRoleHouseBankSoC, Op: "below", Value: 0.2, State: "warn", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return bank.ID, rule
}

// Clearing the bank item's source through the item page must stop the rule
// watching the old path, with no help from a manual cache reset.
func TestClearingTheHouseBankSourceThroughTheHandlerEmptiesTheRulePath(t *testing.T) {
	bankID, rule := houseBankRuleFixture(t)

	body := `{"name":"Lithium bank","signalk_source":""}`
	c, rec := newDocumentEchoContext(http.MethodPut, "/api/inventory/equipment/"+bankID, body, bankID)
	if err := updateEquipmentHandler(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	got, _ := getAlarmRule(rule.ID)
	if got.Path != "" {
		t.Fatalf("the rule must not keep watching the old bank, got %q", got.Path)
	}
}

func TestMovingTheHouseBankSourceThroughTheHandlerMovesTheRule(t *testing.T) {
	bankID, rule := houseBankRuleFixture(t)

	body := `{"name":"Lithium bank","signalk_source":"electrical.batteries.9"}`
	c, _ := newDocumentEchoContext(http.MethodPut, "/api/inventory/equipment/"+bankID, body, bankID)
	if err := updateEquipmentHandler(c); err != nil {
		t.Fatal(err)
	}
	got, _ := getAlarmRule(rule.ID)
	if got.Path != "electrical.batteries.9.capacity.stateOfCharge" {
		t.Fatalf("the rule should follow the bank's new source, got %q", got.Path)
	}
}

func TestDeletingTheHouseBankItemEmptiesTheRulePath(t *testing.T) {
	bankID, rule := houseBankRuleFixture(t)

	c, rec := newDocumentEchoContext(http.MethodDelete, "/api/inventory/equipment/"+bankID, "", bankID)
	if err := deleteEquipmentHandler(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	got, _ := getAlarmRule(rule.ID)
	if got.Path != "" {
		t.Fatalf("the rule must not keep watching a deleted bank, got %q", got.Path)
	}
}

// With no bank chosen the rule still has to be editable and disableable.
func TestARoleRuleCanBeEditedAndDisabledWhileTheHouseBankIsUnset(t *testing.T) {
	_, rule := houseBankRuleFixture(t)
	settingsPath := getEnv("SETTINGS_FILE", "")
	if code, _ := postSettings(t, settingsPath, func(p *settingsPayload) { p.Roles = &vesselRoles{} }); code != http.StatusOK {
		t.Fatal("could not clear the role")
	}

	edited := rule
	edited.Enabled = false
	edited.Value = 0.3
	saved, err := updateAlarmRule(rule.ID, edited)
	if err != nil {
		t.Fatalf("disabling a role rule with no bank must work: %v", err)
	}
	if saved.Path != "" || saved.Enabled {
		t.Fatalf("expected a disabled rule with no path, got %+v", saved)
	}

	edited.Enabled = true
	edited.Value = 0.25
	if _, err := updateAlarmRule(rule.ID, edited); err != nil {
		t.Fatalf("editing an existing role rule with no bank must work: %v", err)
	}

	if _, err := createAlarmRule(alarmRule{Label: "Another", PathRole: alarmPathRoleHouseBankSoC, Op: "below", Value: 0.2, State: "warn", Enabled: true}); err == nil || !strings.Contains(err.Error(), "house bank") {
		t.Fatalf("a new enabled rule still needs a bank, got %v", err)
	}
	if _, err := createAlarmRule(alarmRule{Label: "Parked", PathRole: alarmPathRoleHouseBankSoC, Op: "below", Value: 0.2, State: "warn", Enabled: false}); err != nil {
		t.Fatalf("a new disabled rule can wait for a bank: %v", err)
	}
}

// A rule with no path does not evaluate, so a stale op cannot fire on nothing.
func TestARuleWithNoPathDoesNotEvaluate(t *testing.T) {
	engine := newAlarmEngine()
	rule := alarmRule{ID: "r1", Enabled: true, PathRole: alarmPathRoleHouseBankSoC, Label: "House low", Op: alarmOpStale, StaleAfterSeconds: 1, State: alarmStateWarn}
	read := func(string) alarmSample { return alarmSample{} }
	now := time.Now()
	engine.evaluate([]alarmRule{rule}, read, now)
	engine.evaluate([]alarmRule{rule}, read, now.Add(time.Minute))
	if len(engine.active()) != 0 {
		t.Fatalf("a rule watching nothing must stay quiet, got %+v", engine.active())
	}
}
