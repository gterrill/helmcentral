package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Alarm rules as a registered record type (ADR 0225): Mate proposes, the
// operator taps Apply, and the rule is written by the commands the REST path
// uses. Values are given and shown in the operator's unit; the store is SI.

const testSoCPath = "electrical.batteries.house.capacity.stateOfCharge"

func alarmProposalEnv(t *testing.T) *proposalEnv {
	t.Helper()
	env := newProposalEnv(t)
	withTempAlarmRules(t)
	prev := alarmRuleUnitFor
	alarmRuleUnitFor = func(path string) string {
		switch path {
		case testSoCPath:
			return "ratio"
		case "environment.outside.temperature":
			return "K"
		}
		return ""
	}
	t.Cleanup(func() { alarmRuleUnitFor = prev })
	return env
}

func seedBuiltInRule(t *testing.T, r alarmRule) alarmRule {
	t.Helper()
	if err := validateAlarmRule(&r); err != nil {
		t.Fatal(err)
	}
	r.ID, r.SeededBy = "built-in-"+r.Label, "test-set"
	err := globalDocumentStore.writeAlarmRules("seed", func(tx *sql.Tx, now time.Time) error {
		r.CreatedAt, r.UpdatedAt = now, now
		return insertAlarmRuleTx(tx, r)
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAlarmRule_CreateIsProposedInOperatorUnitsAndAppliedInSI(t *testing.T) {
	env := alarmProposalEnv(t)
	p := env.propose(t, fmt.Sprintf(`{"operations":[{"type":"alarm_rule","action":"create","fields":{
		"label":"House Bank Low","path":%q,"op":"below","value":50,"clear_value":55,"state":"warn","dwell_seconds":30}}]}`, testSoCPath))
	if got, want := p.Ops[0].Description, "Add alarm House Bank Low: a warning when it falls below 50 %, clearing above 55 %, after 30 s"; got != want {
		t.Fatalf("description %q, want %q", got, want)
	}
	if p.Ops[0].After["value"] != "50 %" || p.Ops[0].After["clear_value"] != "55 %" || p.Ops[0].After["dwell_seconds"] != "30 s" {
		t.Errorf("the card shows operator units, got %v", p.Ops[0].After)
	}
	if n := len(listAlarmRules()); n != 0 {
		t.Fatalf("propose writes nothing, found %d rules", n)
	}
	env.advance()
	if _, err := env.apply(p.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	rules := listAlarmRules()
	if len(rules) != 1 {
		t.Fatalf("expected the rule live in the cache after Apply, got %d", len(rules))
	}
	r := rules[0]
	if r.Value != 0.5 || fmt.Sprintf("%.4f", r.Hysteresis) != "0.0500" || r.State != alarmStateWarn || r.DwellSeconds != 30 || !r.Enabled || r.SeededBy != "" {
		t.Errorf("stored rule %+v", r)
	}
	if strings.Join(r.Methods, ",") != "visual,sound" {
		t.Errorf("a proposed alarm is audible and visible, got %v", r.Methods)
	}
}

func TestAlarmRule_CreateUsesTheRESTValidator(t *testing.T) {
	env := alarmProposalEnv(t)
	for name, tc := range map[string]struct{ fields, want string }{
		"state":       {`"label":"X","path":"` + testSoCPath + `","op":"above","value":1,"state":"normal","dwell_seconds":5`, "state:"},
		"no path":     {`"label":"X","path":" ","op":"above","value":1,"state":"warn","dwell_seconds":5`, "path:"},
		"missing":     {`"label":"X","path":"` + testSoCPath + `","op":"above","state":"warn","dwell_seconds":5`, "value: is required"},
		"wrong clear": {`"label":"X","path":"` + testSoCPath + `","op":"below","value":10,"clear_value":8,"state":"warn","dwell_seconds":5`, "clear_value:"},
	} {
		msg := proposeError(t, env.deps, `{"operations":[{"type":"alarm_rule","action":"create","fields":{`+tc.fields+`}}]}`)
		if !strings.Contains(msg, tc.want) {
			t.Errorf("%s: %q lacks %q", name, msg, tc.want)
		}
	}
}

func TestAlarmRule_UpdatingABuiltInRuleSaysSoAndIsCheckedForFreshness(t *testing.T) {
	env := alarmProposalEnv(t)
	seed := seedBuiltInRule(t, alarmRule{Enabled: true, Path: testSoCPath, Label: "House Bank Low", Op: alarmOpBelow, Value: 0.5, Hysteresis: 0.05, DwellSeconds: 30, State: alarmStateWarn})
	env.advance()
	p := env.propose(t, fmt.Sprintf(`{"operations":[{"type":"alarm_rule","action":"update","id":%q,"fields":{"value":40,"clear_value":45}}]}`, seed.ID))
	d := p.Ops[0].Description
	if !strings.Contains(d, "built-in") || !strings.Contains(d, "below 40 %") || !strings.Contains(d, "above 45 %") {
		t.Fatalf("description %q", d)
	}
	if p.Ops[0].Before["value"] != "50 %" || p.Ops[0].After["value"] != "40 %" {
		t.Errorf("before/after %v -> %v", p.Ops[0].Before, p.Ops[0].After)
	}

	// The operator edits the rule in the drawer before tapping Apply.
	edited := seed
	edited.Label = "House Bank Very Low"
	if _, err := updateAlarmRule(seed.ID, edited); err != nil {
		t.Fatal(err)
	}
	env.advance()
	if _, err := env.apply(p.ID); err == nil {
		t.Fatal("a rule edited since the proposal must make it stale")
	}
	if got, _ := getAlarmRule(seed.ID); got.Value != 0.5 {
		t.Errorf("a stale proposal writes nothing, got %v", got.Value)
	}
}

func TestAlarmRule_UpdateAppliesAndKeepsOrigin(t *testing.T) {
	env := alarmProposalEnv(t)
	seed := seedBuiltInRule(t, alarmRule{Enabled: true, Path: testSoCPath, Label: "House Bank Low", Op: alarmOpBelow, Value: 0.5, DwellSeconds: 30, State: alarmStateWarn})
	env.advance()
	p := env.propose(t, fmt.Sprintf(`{"operations":[{"type":"alarm_rule","action":"update","id":%q,"fields":{"enabled":false}}]}`, seed.ID))
	if !strings.HasPrefix(p.Ops[0].Description, "Switch off alarm House Bank Low") {
		t.Errorf("description %q", p.Ops[0].Description)
	}
	env.advance()
	if _, err := env.apply(p.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := getAlarmRule(seed.ID)
	if got.Enabled || got.SeededBy != "test-set" {
		t.Errorf("got %+v", got)
	}
}

func TestAlarmRule_TemperatureConvertsThroughTheOffset(t *testing.T) {
	env := alarmProposalEnv(t)
	p := env.propose(t, `{"operations":[{"type":"alarm_rule","action":"create","fields":{
		"label":"Fridge Warm","path":"environment.outside.temperature","op":"above","value":8,"clear_value":6,"state":"alert","dwell_seconds":600}}]}`)
	env.advance()
	if _, err := env.apply(p.ID); err != nil {
		t.Fatal(err)
	}
	r := listAlarmRules()[0]
	if fmt.Sprintf("%.2f %.2f", r.Value, r.Hysteresis) != "281.15 2.00" {
		t.Errorf("8 °C is 281.15 K and a 2 degree deadband is 2 K, got %v %v", r.Value, r.Hysteresis)
	}
}

func TestAlarmRule_PathCannotChangeOnUpdate(t *testing.T) {
	env := alarmProposalEnv(t)
	seed := seedBuiltInRule(t, alarmRule{Enabled: true, Path: testSoCPath, Label: "House Bank Low", Op: alarmOpBelow, Value: 0.5, DwellSeconds: 30, State: alarmStateWarn})
	msg := proposeError(t, env.deps, fmt.Sprintf(`{"operations":[{"type":"alarm_rule","action":"update","id":%q,"fields":{"path":"other.path"}}]}`, seed.ID))
	if !strings.Contains(msg, "path:") || !strings.Contains(msg, "new alarm") {
		t.Errorf("got %q", msg)
	}
}

func TestAlarmRule_DerivedRulesAreRefusedWithAReason(t *testing.T) {
	env := alarmProposalEnv(t)
	msg := proposeError(t, env.deps, `{"operations":[{"type":"alarm_rule","action":"update","id":"tank:abc","fields":{"enabled":false}}]}`)
	for _, want := range []string{"tank", "tank_warn_percent", "tank_alarm_percent", "tank_alarms_off"} {
		if !strings.Contains(msg, want) {
			t.Errorf("tank refusal %q lacks %q", msg, want)
		}
	}
	msg = proposeError(t, env.deps, `{"operations":[{"type":"alarm_rule","action":"delete","id":"zone:x:1"}]}`)
	if !strings.Contains(msg, "gauge zone") {
		t.Errorf("zone refusal %q", msg)
	}
}

func TestAlarmRule_DeletingABuiltInRuleIsAllowedAndStaysDeleted(t *testing.T) {
	env := alarmProposalEnv(t)
	set := []alarmRule{{Enabled: true, Path: testSoCPath, Label: "House Bank Low", Op: alarmOpBelow, Value: 0.5, DwellSeconds: 30, State: alarmStateWarn}}
	if err := seedAlarmRuleSet("test-set", set, false); err != nil {
		t.Fatal(err)
	}
	seed := listAlarmRules()[0]
	env.advance()
	p := env.propose(t, fmt.Sprintf(`{"operations":[{"type":"alarm_rule","action":"delete","id":%q}]}`, seed.ID))
	if d := p.Ops[0].Description; !strings.Contains(d, "House Bank Low") || !strings.Contains(d, "built-in") {
		t.Errorf("description %q", d)
	}
	env.advance()
	if _, err := env.apply(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := getAlarmRule(seed.ID); ok {
		t.Error("rule still live after delete")
	}
	if err := seedAlarmRuleSet("test-set", set, false); err != nil {
		t.Fatal(err)
	}
	if n := len(listAlarmRules()); n != 0 {
		t.Fatalf("a deleted built-in must not be seeded again, got %d rules", n)
	}
}

func TestListAlarmRules_ShowsStoredRulesInOperatorUnitsAndDerivedAsReadOnly(t *testing.T) {
	env := alarmProposalEnv(t)
	seedBuiltInRule(t, alarmRule{Enabled: true, Path: testSoCPath, Label: "House Bank Low", Op: alarmOpBelow, Value: 0.5, Hysteresis: 0.05, DwellSeconds: 30, State: alarmStateWarn})
	own, err := createAlarmRule(alarmRule{Enabled: false, Path: "a.b", Label: "Mine", Op: alarmOpAbove, Value: 3, State: alarmStateAlarm})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := env.deps.execute(context.Background(), "list_alarm_rules", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Rules []struct {
			ID, Label, Origin, Version string
			DisplayUnit                string `json:"display_unit"`
			Value                      float64
			ClearValue                 *float64 `json:"clear_value"`
			Enabled                    bool
			ReadOnly                   bool `json:"read_only"`
		}
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	byLabel := map[string]int{}
	for i, r := range out.Rules {
		byLabel[r.Label] = i
	}
	hb := out.Rules[byLabel["House Bank Low"]]
	if hb.Origin != "built-in" || hb.Value != 50 || hb.DisplayUnit != "%" || hb.ClearValue == nil || *hb.ClearValue != 55 || hb.Version == "" || hb.ReadOnly {
		t.Errorf("house bank row %+v", hb)
	}
	if m := out.Rules[byLabel["Mine"]]; m.ID != own.ID || m.Origin != "own" || m.Enabled {
		t.Errorf("own row %+v", m)
	}
}

func TestAlarmRuleListEntry_DerivedTankPointsAtTheEquipmentRecord(t *testing.T) {
	prev := alarmRuleUnitFor
	alarmRuleUnitFor = func(string) string { return "ratio" }
	t.Cleanup(func() { alarmRuleUnitFor = prev })
	alarm := 0.1
	e := alarmRuleListEntry(alarmRule{ID: "tank:x", Label: "Fuel Port Low", Path: "tanks.fuel.0.currentLevel", Op: alarmOpBelow, Value: 0.25, AlarmLevelValue: &alarm, Hysteresis: 0.05, Enabled: true}, true)
	if !e.ReadOnly || !strings.Contains(e.ChangeIt, "tank_warn_percent") || e.Value != 25 || e.AlarmLevel == nil || *e.AlarmLevel != 10 {
		t.Errorf("entry %+v", e)
	}
}

// ── review fixes ────────────────────────────────────────────────────────

func TestAlarmRule_UpdatingARoleBoundRuleDoesNotReenterTheStoreLock(t *testing.T) {
	env := alarmProposalEnv(t)
	t.Setenv("SETTINGS_FILE", t.TempDir()+"/none.yaml")
	r := alarmRule{ID: "role-rule", Enabled: true, Path: "", PathRole: alarmPathRoleHouseBankSoC, Label: "House Bank Low", Op: alarmOpBelow, Value: 0.5, DwellSeconds: 30, State: alarmStateWarn, Methods: []string{"visual"}}
	err := globalDocumentStore.writeAlarmRules("seed", func(tx *sql.Tx, now time.Time) error {
		r.CreatedAt, r.UpdatedAt = now, now
		return insertAlarmRuleTx(tx, r)
	})
	if err != nil {
		t.Fatal(err)
	}
	invalidateSourceItems()
	env.advance()
	done := make(chan assistantProposal, 1)
	go func() {
		done <- env.propose(t, `{"operations":[{"type":"alarm_rule","action":"update","id":"role-rule","fields":{"enabled":false}}]}`)
	}()
	select {
	case p := <-done:
		invalidateSourceItems()
		env.advance()
		go func() {
			_, _ = env.apply(p.ID)
			done <- p
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("apply deadlocked")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("propose deadlocked")
	}
	if got, _ := getAlarmRule("role-rule"); got.Enabled {
		t.Error("the rule should be switched off")
	}
}

func TestAlarmRule_UnknownUnitRefusesNumbersAndNamesThePath(t *testing.T) {
	env := alarmProposalEnv(t)
	msg := proposeError(t, env.deps, `{"operations":[{"type":"alarm_rule","action":"create","fields":{
		"label":"X","path":"a.b","op":"above","value":3,"state":"warn","dwell_seconds":5}}]}`)
	if !strings.Contains(msg, "value:") || !strings.Contains(msg, "a.b") || !strings.Contains(msg, "unit") {
		t.Errorf("got %q", msg)
	}
	// Without a number there is nothing to convert: switching a rule off is fine.
	own, err := createAlarmRule(alarmRule{Enabled: true, Path: "a.b", Label: "Mine", Op: alarmOpAbove, Value: 3, State: alarmStateAlarm})
	if err != nil {
		t.Fatal(err)
	}
	env.advance()
	env.propose(t, fmt.Sprintf(`{"operations":[{"type":"alarm_rule","action":"update","id":%q,"fields":{"enabled":false}}]}`, own.ID))
}

func TestAlarmRule_HouseBankRoleIsARatio(t *testing.T) {
	env := alarmProposalEnv(t)
	r := alarmRule{ID: "role-rule", Enabled: true, Path: "", PathRole: alarmPathRoleHouseBankSoC, Label: "House Bank Low", Op: alarmOpBelow, Value: 0.5, Hysteresis: 0.05, DwellSeconds: 30, State: alarmStateWarn, Methods: []string{"visual"}}
	if err := globalDocumentStore.writeAlarmRules("seed", func(tx *sql.Tx, now time.Time) error {
		r.CreatedAt, r.UpdatedAt = now, now
		return insertAlarmRuleTx(tx, r)
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SETTINGS_FILE", t.TempDir()+"/none.yaml")
	env.advance()
	p := env.propose(t, `{"operations":[{"type":"alarm_rule","action":"update","id":"role-rule","fields":{"value":40}}]}`)
	if p.Ops[0].After["value"] != "40 %" {
		t.Errorf("a rule that follows the house bank is a percentage, got %v", p.Ops[0].After)
	}
}

func TestAlarmRule_UnitChangeBetweenProposeAndApplyMakesItStale(t *testing.T) {
	env := alarmProposalEnv(t)
	p := env.propose(t, fmt.Sprintf(`{"operations":[{"type":"alarm_rule","action":"create","fields":{
		"label":"House Bank Low","path":%q,"op":"below","value":50,"state":"warn","dwell_seconds":30}}]}`, testSoCPath))
	prev := alarmRuleUnitFor
	alarmRuleUnitFor = func(string) string { return "Pa" }
	t.Cleanup(func() { alarmRuleUnitFor = prev })
	env.advance()
	_, err := env.apply(p.ID)
	if err == nil || !strings.Contains(err.Error(), "unit") {
		t.Fatalf("expected a stale refusal naming the unit, got %v", err)
	}
	if n := len(listAlarmRules()); n != 0 {
		t.Errorf("nothing is written, got %d", n)
	}
}

func TestAlarmRule_ReloadFailureIsReportedOnApply(t *testing.T) {
	env := alarmProposalEnv(t)
	other, err := createAlarmRule(alarmRule{Enabled: true, Path: testSoCPath, Label: "Other", Op: alarmOpBelow, Value: 0.2, State: alarmStateWarn})
	if err != nil {
		t.Fatal(err)
	}
	p := env.propose(t, fmt.Sprintf(`{"operations":[{"type":"alarm_rule","action":"create","fields":{
		"label":"House Bank Low","path":%q,"op":"below","value":50,"state":"warn","dwell_seconds":30}}]}`, testSoCPath))
	if _, err := globalDocumentStore.db.Exec(`UPDATE alarm_rules SET methods_json = '{bad' WHERE id = ?`, other.ID); err != nil {
		t.Fatal(err)
	}
	env.advance()
	got, err := env.apply(p.ID)
	if err == nil || !errors.Is(err, errAlarmRulesNotReloaded) {
		t.Fatalf("expected the not-reloaded error, got %v", err)
	}
	if got.Status != assistantProposalApplied {
		t.Errorf("the change was saved, so the proposal is applied, got %q", got.Status)
	}
}

func TestAlarmRule_ExplicitNullClearPointIsAcceptedOnAnEqualAlarm(t *testing.T) {
	env := alarmProposalEnv(t)
	own, err := createAlarmRule(alarmRule{Enabled: true, Path: testSoCPath, Label: "Mine", Op: alarmOpEqual, Value: 1, DwellSeconds: 5, State: alarmStateWarn})
	if err != nil {
		t.Fatal(err)
	}
	env.advance()
	env.propose(t, fmt.Sprintf(`{"operations":[{"type":"alarm_rule","action":"update","id":%q,"fields":{"clear_value":null,"dwell_seconds":9}}]}`, own.ID))
	msg := proposeError(t, env.deps, fmt.Sprintf(`{"operations":[{"type":"alarm_rule","action":"update","id":%q,"fields":{"clear_value":60}}]}`, own.ID))
	if !strings.Contains(msg, "no clear point") {
		t.Errorf("got %q", msg)
	}
}

func TestAlarmRule_CardShowsWordsForConditionAndSeverity(t *testing.T) {
	env := alarmProposalEnv(t)
	p := env.propose(t, fmt.Sprintf(`{"operations":[{"type":"alarm_rule","action":"create","fields":{
		"label":"Bank Odd","path":%q,"op":"notEqual","value":100,"state":"warn","dwell_seconds":5}}]}`, testSoCPath))
	if p.Ops[0].After["op"] != "differs from" || p.Ops[0].After["state"] != "Warning" {
		t.Errorf("got %v", p.Ops[0].After)
	}
}
