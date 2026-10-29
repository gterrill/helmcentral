package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func contextTestDeps(t *testing.T) assistantToolDeps {
	t.Helper()
	withTempAlarmRules(t)
	if _, err := createAlarmRule(validRule()); err != nil {
		t.Fatal(err)
	}
	store := globalDocumentStore
	loa := 14.2
	if _, err := store.SetVesselParticulars(vesselParticulars{
		Builder: "Lagoon", Model: "420", LOAM: &loa, HomeMarina: "Marina One",
		OwnerName: "Pat Skipper", OwnerPhone: "+61 400 111 222", OwnerEmail: "pat@example.com",
		Insurer: "Salty Mutual", PolicyNumber: "POL-99-123", StormDelegate: "Sam Neighbour",
	}); err != nil {
		t.Fatal(err)
	}
	d := statusTestDeps(t)
	d.documents = func() *documentStore { return store }
	d.vesselView = func() (vesselView, error) {
		return vesselView{
			HouseBank: &vesselHouseBankSetting{Path: "electrical.batteries.512", EquipmentID: "b-house", CapacityAh: 800, Cells: 8},
			Engines:   []vesselEngineSetting{{Instance: "0", Name: "Port Engine"}, {Instance: "1", Name: "Starboard Engine"}},
		}, nil
	}
	d.sourceItems = func() ([]sourceItem, error) {
		return []sourceItem{
			{ID: "b-house", Name: "Service Bank", Source: "electrical.batteries.512", Group: sourceBattery, Instance: "512", CapacityAh: 800, Cells: 8},
			{ID: "t-fuel", Name: "Port Fuel", Source: "tanks.fuel.2", Group: sourceTank, Instance: "fuel.2", TankWarnPercent: 25, TankAlarmPercent: 12},
			{ID: "t-water", Name: "Fresh Water", Source: "tanks.freshWater.0", Group: sourceTank, Instance: "freshWater.0"},
		}, nil
	}
	d.siUnit = func(path string) string {
		switch {
		case strings.HasSuffix(path, ".voltage"):
			return "V"
		case strings.HasSuffix(path, ".current"):
			return "A"
		case strings.HasSuffix(path, ".stateOfCharge"):
			return ""
		}
		return ""
	}
	d.displayUnits = func() (string, error) { return "imperial", nil }
	d.now = func() time.Time { return time.Date(2026, 10, 30, 2, 0, 0, 0, time.UTC) }
	return d
}

func TestBoatContext_BuiltFromItemsRolesUnitsRulesAndTime(t *testing.T) {
	md, err := buildBoatContext(contextTestDeps(t), mcpAllOn)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"House bank", "Service Bank", "electrical.batteries.512", "800 Ah",
		"Port Engine", "propulsion.0", "Starboard Engine",
		"Port Fuel", "tanks.fuel.2", "warn 25%", "alarm 12%", "Fresh Water", "warn 20%",
		"imperial", "°F", "gal/h",
		"House bank low", "11.8",
		"Lagoon", "Marina One",
		"Time zone",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("context lacks %q:\n%s", want, md)
		}
	}
}

func TestBoatContext_NoContactDetails(t *testing.T) {
	md, err := buildBoatContext(contextTestDeps(t), mcpAllOn)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"Pat Skipper", "+61 400", "pat@example.com", "Salty Mutual", "POL-99", "Sam Neighbour"} {
		if strings.Contains(md, leak) {
			t.Errorf("context leaks %q", leak)
		}
	}
}

func TestBoatContext_SaysWhyWhenPartIsUnavailable(t *testing.T) {
	d := contextTestDeps(t)
	d.vesselView = func() (vesselView, error) { return vesselView{}, context.DeadlineExceeded }
	md, err := buildBoatContext(d, mcpAllOn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "Not available") || !strings.Contains(md, "roles") {
		t.Errorf("the missing roles are not explained:\n%s", md)
	}
	if !strings.Contains(md, "Port Fuel") {
		t.Errorf("one failing part must not hide the others:\n%s", md)
	}
}

func TestBoatContext_NoHouseBankRoleSaysSo(t *testing.T) {
	d := contextTestDeps(t)
	d.vesselView = func() (vesselView, error) { return vesselView{}, nil }
	md, _ := buildBoatContext(d, mcpAllOn)
	if !strings.Contains(md, "no house bank role is set") {
		t.Errorf("md:\n%s", md)
	}
}

func TestBoatContext_ToolReturnsTheSameText_(t *testing.T) {
	d := contextTestDeps(t)
	def := mcpToolDefByName(mcpBoatContextTool)
	if def == nil {
		t.Fatal("get_boat_context is not offered")
	}
	out, err := def.Run(mcpEnabledCtx(context.Background(), mcpAllOn), d, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	md, _ := buildBoatContext(d, mcpAllOn)
	if out != md {
		t.Errorf("tool and resource differ")
	}
}

func TestMCPInstructions_CarryTheRulesThatApplyOutsideMate(t *testing.T) {
	for _, want := range []string{"operator's units", "missing", "as worded", "never guess", "boat"} {
		if !strings.Contains(strings.ToLower(mcpInstructions), strings.ToLower(want)) {
			t.Errorf("instructions lack %q:\n%s", want, mcpInstructions)
		}
	}
	if strings.Contains(strings.ToLower(mcpInstructions), "widget") {
		t.Error("instructions say widget")
	}
}

func mcpAllOn(string) bool { return true }

func TestBoatContext_FollowsTheTokensToolList(t *testing.T) {
	d := contextTestDeps(t)
	off := func(name string) bool { return name != "get_vessel_particulars" && name != "list_alarm_rules" }
	md, err := buildBoatContext(d, off)
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"Boat particulars", "Lagoon", "Marina One", "Alarm rules", "House bank low"} {
		if strings.Contains(md, gone) {
			t.Errorf("context still carries %q with its tool off:\n%s", gone, md)
		}
	}
	if strings.Contains(md, "Not available") {
		t.Errorf("a left-out section must say nothing:\n%s", md)
	}
	if !strings.Contains(md, "Service Bank") {
		t.Errorf("the rest of the context should remain:\n%s", md)
	}
}

func TestBoatContext_TanksAndEquipmentFollowTheirTools(t *testing.T) {
	d := contextTestDeps(t)
	md, _ := buildBoatContext(d, func(name string) bool { return name == mcpBoatContextTool })
	if strings.Contains(md, "## Tanks") || strings.Contains(md, "Port Fuel") {
		t.Errorf("tanks without get_vessel_status:\n%s", md)
	}
}

func TestBoatContext_ReadsTheVesselStateOnce(t *testing.T) {
	d := contextTestDeps(t)
	calls := 0
	inner := d.vesselState
	d.vesselState = func() (vesselStateData, error) { calls++; return inner() }
	if _, err := buildBoatContext(d, mcpAllOn); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("vessel state read %d times", calls)
	}
}

func TestBoatContext_NoEnabledSetMeansNothingSwitchable(t *testing.T) {
	d := contextTestDeps(t)
	def := mcpToolDefByName(mcpBoatContextTool)
	out, err := def.Run(context.Background(), d, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "Lagoon") || strings.Contains(out, "House bank low") {
		t.Errorf("a call with no token list got switchable sections:\n%s", out)
	}
}
