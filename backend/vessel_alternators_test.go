package main

import (
	"bytes"
	"log"
	"net/http"
	"os"
	"strings"
	"testing"
)

// Shape taken from the live SignalK tree: the Victron GX publishes one
// electrical.alternator.<n> per alternator, and a retired instance stays in
// the tree with null values.
const alternatorTree = `{"electrical":{"alternator":{
	"1":{"current":{"value":null},"voltage":{"value":null}},
	"2":{"current":{"value":31.5},"voltage":{"value":28.4},"temperature":{"value":330.15}},
	"12":{"current":{"value":28.5},"voltage":{"value":28.3},"temperature":{"value":325.15}}
}}}`

func configuredAlternators(t *testing.T) vesselView {
	t.Helper()
	vessel, err := loadVesselView(getEnv("SETTINGS_FILE", "../settings.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return vessel
}

func TestElectricalPayloadLogsAnUnreadableRolesBlockAndKeepsOtherFigures(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	if err := os.WriteFile(settingsPath, []byte("roles: [not, a, block]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	resetVesselReadLog()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	seedSelfTree(t, alternatorTree)

	payload := buildElectricalStatePayload()
	if !strings.Contains(buf.String(), "vessel unreadable") {
		t.Fatalf("expected a log line, got %q", buf.String())
	}
	if payload["alternator_0_current_a"] != -1.0 {
		t.Fatalf("alternators must read as unset, got %v", payload["alternator_0_current_a"])
	}
}

// discoveryGroupRows is the rows of one kind in a discovery response.
func discoveryGroupRows(r discoveryResponse, kind string) []discoveryRow {
	for _, g := range r.Groups {
		if g.Kind == kind {
			return g.Rows
		}
	}
	return nil
}

func TestVesselDiscoverySortsAlternatorsNumerically(t *testing.T) {
	seedSelfTree(t, alternatorTree)
	got := discoveryGroupRows(vesselDiscovery(globalSignalKSnapshot, vesselView{}, nil), "alternator")
	if len(got) != 3 || got[0].Instance != "1" || got[1].Instance != "2" || got[2].Instance != "12" {
		t.Fatalf("got %+v", got)
	}
	if got[0].Source != "electrical.alternator.1" {
		t.Fatalf("a row carries the source an item would own: %+v", got[0])
	}
}

func TestElectricalStateReadsTheConfiguredAlternatorInstances(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	if err := saveVesselFixture(t, settingsPath, vesselView{Alternators: vesselAlternatorSettings{PortInstance: "2", StarboardInstance: "12"}}); err != nil {
		t.Fatal(err)
	}
	seedSelfTree(t, alternatorTree)

	state, err := fetchSignalKElectricalState(configuredAlternators(t))
	if err != nil {
		t.Fatal(err)
	}
	if state.Alternator0.CurrentA != 31.5 || state.Alternator0.VoltageV != 28.4 || state.Alternator0.TempC != 57 {
		t.Fatalf("port: got %+v", state.Alternator0)
	}
	if state.Alternator1.CurrentA != 28.5 || state.Alternator1.VoltageV != 28.3 {
		t.Fatalf("starboard: got %+v", state.Alternator1)
	}
}

func TestElectricalStateWithNoAlternatorsSetHasNoAlternatorData(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	seedSelfTree(t, alternatorTree)

	state, err := fetchSignalKElectricalState(configuredAlternators(t))
	if err != nil {
		t.Fatal(err)
	}
	want := alternatorInstanceData{CurrentA: -1, VoltageV: -1, PowerW: -1, TempC: -1}
	if state.Alternator0 != want || state.Alternator1 != want {
		t.Fatalf("unset sides must read -1, got port %+v starboard %+v", state.Alternator0, state.Alternator1)
	}
}

func TestElectricalStateWithOnlyStarboardSet(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	if err := saveVesselFixture(t, settingsPath, vesselView{Alternators: vesselAlternatorSettings{StarboardInstance: "12"}}); err != nil {
		t.Fatal(err)
	}
	seedSelfTree(t, alternatorTree)

	state, err := fetchSignalKElectricalState(configuredAlternators(t))
	if err != nil {
		t.Fatal(err)
	}
	if state.Alternator0.CurrentA != -1 || state.Alternator1.CurrentA != 28.5 {
		t.Fatalf("got port %+v starboard %+v", state.Alternator0, state.Alternator1)
	}
}

func TestValidateVesselRolesAlternators(t *testing.T) {
	items := []sourceItem{
		{ID: "a2", Name: "Port alt", Source: "electrical.alternator.2", Group: sourceAlternator, Instance: "2"},
		{ID: "a12", Name: "Stbd alt", Source: "electrical.alternator.12", Group: sourceAlternator, Instance: "12"},
		{ID: "b1", Name: "Bank", Source: "electrical.batteries.1", Group: sourceBattery, Instance: "1"},
	}
	cases := []struct {
		name    string
		in      vesselRoles
		wantErr string
	}{
		{"both unset", vesselRoles{}, ""},
		{"one set", vesselRoles{PortAlternator: "a2"}, ""},
		{"different", vesselRoles{PortAlternator: "a2", StarboardAlternator: "a12"}, ""},
		{"same", vesselRoles{PortAlternator: "a2", StarboardAlternator: "a2"}, "roles.starboard_alternator"},
		{"not an item", vesselRoles{PortAlternator: "nope"}, "roles.port_alternator"},
		{"wrong kind", vesselRoles{PortAlternator: "b1"}, "roles.port_alternator"},
		{"dotted ac instance", vesselRoles{GeneratorACInstance: "2.current"}, "roles.generator_ac_instance"},
	}
	for _, c := range cases {
		got := validateVesselRoles(c.in, items)
		switch {
		case c.wantErr == "" && got != nil:
			t.Errorf("%s: unexpected error %+v", c.name, got)
		case c.wantErr != "" && (got == nil || got.Field != c.wantErr):
			t.Errorf("%s: got %+v, want field %s", c.name, got, c.wantErr)
		}
	}
}

func TestUpdateSettingsSavesAndRejectsAlternatorRoles(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	withTempAlarmRules(t)
	store := vesselFixtureStore(t)
	port, err := store.CreateEquipment(equipmentItem{Name: "Port alternator", SignalKSource: "electrical.alternator.2"})
	if err != nil {
		t.Fatal(err)
	}
	stbd, err := store.CreateEquipment(equipmentItem{Name: "Starboard alternator", SignalKSource: "electrical.alternator.12"})
	if err != nil {
		t.Fatal(err)
	}

	code, _ := postSettings(t, settingsPath, func(p *settingsPayload) {
		p.Roles = &vesselRoles{PortAlternator: " " + port.ID + " ", StarboardAlternator: stbd.ID}
	})
	if code != http.StatusOK {
		t.Fatalf("got %d", code)
	}
	saved, err := loadVesselView(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Alternators.PortInstance != "2" || saved.Alternators.StarboardInstance != "12" {
		t.Fatalf("got %+v", saved.Alternators)
	}

	code, body := postSettings(t, settingsPath, func(p *settingsPayload) {
		p.Roles = &vesselRoles{PortAlternator: port.ID, StarboardAlternator: port.ID}
	})
	if code == http.StatusOK {
		t.Fatalf("same alternator on both sides must be rejected, body %v", body)
	}
	saved, _ = loadVesselView(settingsPath)
	if saved.Alternators.StarboardInstance != "12" {
		t.Fatalf("a rejected save must not change the stored value, got %+v", saved.Alternators)
	}
}

func TestVesselDiscoveryListsAlternatorInstancesWithLiveCurrent(t *testing.T) {
	seedSelfTree(t, alternatorTree)
	rows := discoveryGroupRows(vesselDiscovery(globalSignalKSnapshot, vesselView{}, nil), "alternator")
	if len(rows) != 3 {
		t.Fatalf("instances: %+v", rows)
	}
	var twelve discoveryRow
	for _, r := range rows {
		if r.Instance == "12" {
			twelve = r
		}
	}
	if len(twelve.Metrics) != 1 || twelve.Metrics[0].Text != "28.5 A" {
		t.Fatalf("instance 12 current: %+v", twelve.Metrics)
	}
}

func TestChargeSourcesNameTheConfiguredSides(t *testing.T) {
	seedSelfTree(t, alternatorTree)
	got := discoverChargeSources(globalSignalKSnapshot, vesselAlternatorSettings{PortInstance: "2", StarboardInstance: "12"})
	if got["port alternator"] != 31.5 || got["starboard alternator"] != 28.5 {
		t.Fatalf("got %v", got)
	}
	got = discoverChargeSources(globalSignalKSnapshot, vesselAlternatorSettings{})
	if _, ok := got["alternator 2"]; !ok {
		t.Fatalf("unset sides keep their instance label, got %v", got)
	}
}
