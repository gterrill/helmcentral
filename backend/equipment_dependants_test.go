package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// dependantsFixture is a boat with an engine and a house bank. The engine is
// shown on two pages, once in the ribbon and by a fuel bar's neighbour; the
// bank is the house role and three state-of-charge rules follow it.
type dependantsFixture struct {
	engineID, starboardID, bankID, tankID string
}

func setUpDependants(t *testing.T) dependantsFixture {
	t.Helper()
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	withTempAlarmRules(t)
	withTestDocumentStore(t)
	useProfiles(t, engineProfileWithZones())

	var f dependantsFixture
	f.engineID = newTileItem(t, "Port engine", "propulsion.port", "yanmar-4jh")
	f.starboardID = newTileItem(t, "Starboard engine", "propulsion.starboard", "yanmar-4jh")
	f.tankID = newTileItem(t, "Fuel port", "tanks.fuel.0", "")
	bank, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Lithium bank", SignalKSource: "electrical.batteries.512"})
	if err != nil {
		t.Fatal(err)
	}
	f.bankID = bank.ID
	invalidateSourceItems()
	if code, body := postSettings(t, settingsPath, func(p *settingsPayload) { p.Roles = &vesselRoles{HouseBank: f.bankID} }); code != http.StatusOK {
		t.Fatalf("roles: %d %v", code, body)
	}
	for _, label := range []string{"House low", "House critical", "House full"} {
		if _, err := createAlarmRule(alarmRule{Label: label, PathRole: alarmPathRoleHouseBankSoC, Op: "below", Value: 0.2, State: "warn", Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}

	dashboardPagesMu.Lock()
	previousPages, previousRibbon := dashboardPagesState, dashboardRibbonState
	dashboardPagesState = map[string]*dashboardPageData{
		"page-a": {ID: "page-a", Name: "Underway", Position: 0, Widgets: []dashboardLayoutItem{
			{ID: "cluster:abcd1234", W: 6, H: 10, Cluster: &dashboardClusterConfig{
				EquipmentID: f.engineID,
				Title:       "Port",
				Ring:        dashboardGaugeConfig{Suffix: "oilPressure", Display: "radial", Quantity: "pressure", Unit: "psi"},
				Centre:      dashboardGaugeConfig{Suffix: "fuel.rate", Display: "numeric", Quantity: "raw", Unit: "raw"},
				Fuel:        &dashboardClusterFuelRail{Side: "left", Bars: []dashboardClusterFuelBar{{TankID: f.tankID}}},
			}},
			{ID: "cluster:other000", W: 6, H: 10, Cluster: &dashboardClusterConfig{EquipmentID: f.starboardID, Title: "Starboard"}},
		}},
		"page-b": {ID: "page-b", Name: "Wall", Position: 1, Widgets: []dashboardLayoutItem{
			{ID: "gauge-group:bbbb2222", W: 6, H: 8, GaugeGroup: &dashboardGaugeGroupConfig{
				EquipmentKind: "engine", EquipmentID: f.engineID, Title: "Engines: Port",
				Gauges: []dashboardGaugeConfig{{Suffix: "revolutions", Display: "numeric", Quantity: "frequency", Unit: "rpm"}},
			}},
		}},
	}
	dashboardRibbonState = &dashboardLampStripConfig{Title: "Ribbon", Lamps: []dashboardLamp{{EquipmentID: f.engineID, Suffix: "alarm", Label: "PORT"}}}
	dashboardPagesMu.Unlock()
	t.Cleanup(func() {
		dashboardPagesMu.Lock()
		dashboardPagesState, dashboardRibbonState = previousPages, previousRibbon
		dashboardPagesMu.Unlock()
	})
	return f
}

func TestEquipmentDependantsNameTilesByPageAndTitle(t *testing.T) {
	f := setUpDependants(t)
	got, err := equipmentDependantsFor(f.engineID)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, tile := range got.Tiles {
		seen[tile.PageName+"|"+tile.TileID] = tile.Title
	}
	want := map[string]string{
		"Underway|cluster:abcd1234": "Port",
		"Wall|gauge-group:bbbb2222": "Engines: Port",
		"Indicator ribbon|ribbon":   "Ribbon",
	}
	if len(seen) != len(want) {
		t.Fatalf("expected %v, got %+v", want, got.Tiles)
	}
	for k, title := range want {
		if seen[k] != title {
			t.Fatalf("expected %s -> %q, got %+v", k, title, got.Tiles)
		}
	}
	for _, tile := range got.Tiles {
		if tile.TileID == "cluster:other000" {
			t.Fatalf("the starboard engine's tile does not depend on the port engine")
		}
	}
}

func TestEquipmentDependantsTankBarCountsTheClusterOnce(t *testing.T) {
	f := setUpDependants(t)
	got, err := equipmentDependantsFor(f.tankID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tiles) != 1 || got.Tiles[0].TileID != "cluster:abcd1234" || got.Tiles[0].PageName != "Underway" {
		t.Fatalf("a fuel bar's tank is used by its cluster tile, got %+v", got.Tiles)
	}
}

func TestEquipmentDependantsListTheEngineFeaturesAndItsZoneAlarms(t *testing.T) {
	f := setUpDependants(t)
	got, err := equipmentDependantsFor(f.engineID)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Anchor auto-raise", "GNSS position jump check", "Engine checks", "Mate's passage fuel estimate"} {
		if !containsString(got.Features, want) {
			t.Fatalf("expected feature %q in %v", want, got.Features)
		}
	}
	zones := 0
	for _, r := range got.AlarmRules {
		if r.Kind == dependantRuleZone && strings.HasPrefix(r.ID, "zone:item:"+f.engineID+":") {
			zones++
		}
	}
	if zones == 0 {
		t.Fatalf("the profile's oil pressure zone alarm should be listed, got %+v", got.AlarmRules)
	}
	if len(got.Roles) != 0 {
		t.Fatalf("an engine plays no role, got %+v", got.Roles)
	}
}

func TestEquipmentDependantsListTheHouseBankRoleAndItsRules(t *testing.T) {
	f := setUpDependants(t)
	got, err := equipmentDependantsFor(f.bankID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Roles) != 1 || got.Roles[0].Role != "house_bank" || got.Roles[0].Label != "House bank" {
		t.Fatalf("expected the house bank role, got %+v", got.Roles)
	}
	soc := 0
	for _, r := range got.AlarmRules {
		if r.Kind == dependantRuleStateOfCharge {
			soc++
		}
	}
	if soc != 3 {
		t.Fatalf("expected three state-of-charge rules, got %+v", got.AlarmRules)
	}
}

// A rule that follows the role counts even once the bank has no source, so
// deleting the item shows them.
func TestEquipmentDependantsStillListRoleRulesForAnUnmappedBank(t *testing.T) {
	f := setUpDependants(t)
	bank, _ := globalDocumentStore.GetEquipment(f.bankID)
	bank.SignalKSource = ""
	if _, err := globalDocumentStore.UpdateEquipment(f.bankID, bank); err != nil {
		t.Fatal(err)
	}
	invalidateSourceItems()
	got, err := equipmentDependantsFor(f.bankID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Roles) != 1 || len(got.AlarmRules) != 3 || len(got.Features) != 0 {
		t.Fatalf("expected the role and three rules, no features, got %+v", got)
	}
}

func TestEquipmentDependantsIsEmptyForAnUnusedItem(t *testing.T) {
	setUpDependants(t)
	spare, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Spare pump"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := equipmentDependantsFor(spare.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tiles)+len(got.Roles)+len(got.AlarmRules)+len(got.Features) != 0 {
		t.Fatalf("expected nothing, got %+v", got)
	}
	c, rec := newDocumentEchoContext(http.MethodGet, "/api/inventory/equipment/"+spare.ID+"/dependants", "", spare.ID)
	if err := equipmentDependantsHandler(c); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.Body.String(), `"tiles":[]`) || !strings.Contains(rec.Body.String(), `"features":[]`) {
		t.Fatalf("empty groups must be arrays, got %s", rec.Body.String())
	}
}

func TestEquipmentDependantsHandler(t *testing.T) {
	f := setUpDependants(t)
	c, rec := newDocumentEchoContext(http.MethodGet, "/api/inventory/equipment/"+f.engineID+"/dependants", "", f.engineID)
	if err := equipmentDependantsHandler(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Tiles []struct {
			PageName string `json:"page_name"`
			Title    string `json:"title"`
		} `json:"tiles"`
		Features []string `json:"features"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Tiles) != 3 || len(body.Features) == 0 {
		t.Fatalf("unexpected body %s", rec.Body.String())
	}

	c, rec = newDocumentEchoContext(http.MethodGet, "/api/inventory/equipment/nope/dependants", "", "nope")
	if err := equipmentDependantsHandler(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown item is 404, got %d", rec.Code)
	}
}

// Deleting an item clears the roles that named it, so nothing is left
// pointing at an id that no longer exists; tiles and rules stay.
func TestDeletingAnItemClearsItsRoles(t *testing.T) {
	f := setUpDependants(t)
	settingsPath := getEnv("SETTINGS_FILE", "")
	c, rec := newDocumentEchoContext(http.MethodDelete, "/api/inventory/equipment/"+f.bankID, "", f.bankID)
	if err := deleteEquipmentHandler(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	roles, err := loadVesselRoles(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if roles.HouseBank != "" {
		t.Fatalf("the deleted bank must not stay a role, got %+v", roles)
	}
	followers := 0
	for _, r := range listAlarmRules() {
		if r.PathRole == alarmPathRoleHouseBankSoC {
			followers++
			if r.Path != "" {
				t.Fatalf("a rule must not keep watching a deleted bank, got %q", r.Path)
			}
		}
	}
	if followers != 3 {
		t.Fatalf("the rules stay, watching nothing, got %d", followers)
	}
}

func TestDeletingAnItemKeepsOtherRoles(t *testing.T) {
	f := setUpDependants(t)
	settingsPath := getEnv("SETTINGS_FILE", "")
	if code, body := postSettings(t, settingsPath, func(p *settingsPayload) {
		p.Roles = &vesselRoles{HouseBank: f.bankID, VoltageReadoutBattery: f.bankID, GeneratorACInstance: "ac1"}
	}); code != http.StatusOK {
		t.Fatalf("%d %v", code, body)
	}
	c, _ := newDocumentEchoContext(http.MethodDelete, "/api/inventory/equipment/"+f.engineID, "", f.engineID)
	if err := deleteEquipmentHandler(c); err != nil {
		t.Fatal(err)
	}
	roles, _ := loadVesselRoles(settingsPath)
	if roles.HouseBank != f.bankID || roles.VoltageReadoutBattery != f.bankID || roles.GeneratorACInstance != "ac1" {
		t.Fatalf("unrelated roles must be untouched, got %+v", roles)
	}
}
