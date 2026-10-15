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

func configuredAlternators(t *testing.T) vesselAlternatorSettings {
	t.Helper()
	alt, err := loadVesselAlternators()
	if err != nil {
		t.Fatal(err)
	}
	return alt
}

func TestElectricalPayloadLogsAnUnreadableVesselBlockAndKeepsOtherFigures(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	if err := os.WriteFile(settingsPath, []byte("vessel: [not, a, block]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	seedSelfTree(t, alternatorTree)

	payload := buildElectricalStatePayload()
	if !strings.Contains(buf.String(), "alternator instances unreadable") {
		t.Fatalf("expected a log line, got %q", buf.String())
	}
	if payload["alternator_0_current_a"] != -1.0 {
		t.Fatalf("alternators must read as unset, got %v", payload["alternator_0_current_a"])
	}
}

func TestVesselCandidatesSortAlternatorsNumerically(t *testing.T) {
	seedSelfTree(t, alternatorTree)
	got := vesselCandidates(globalSignalKSnapshot, vesselSettings{})
	if len(got.Alternators) != 3 || got.Alternators[0].Instance != "1" || got.Alternators[1].Instance != "2" || got.Alternators[2].Instance != "12" {
		t.Fatalf("got %+v", got.Alternators)
	}
}

func TestElectricalStateReadsTheConfiguredAlternatorInstances(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	if err := saveVesselSettings(settingsPath, vesselSettings{Alternators: vesselAlternatorSettings{PortInstance: "2", StarboardInstance: "12"}}); err != nil {
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
	if err := saveVesselSettings(settingsPath, vesselSettings{Alternators: vesselAlternatorSettings{StarboardInstance: "12"}}); err != nil {
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

func TestValidateVesselAlternators(t *testing.T) {
	cases := []struct {
		name    string
		in      vesselAlternatorSettings
		wantErr string
	}{
		{"both unset", vesselAlternatorSettings{}, ""},
		{"one set", vesselAlternatorSettings{PortInstance: "2"}, ""},
		{"zero is an instance", vesselAlternatorSettings{PortInstance: "0", StarboardInstance: "1"}, ""},
		{"different", vesselAlternatorSettings{PortInstance: "2", StarboardInstance: "12"}, ""},
		{"same", vesselAlternatorSettings{PortInstance: "2", StarboardInstance: "2"}, "vessel.alternators.starboard_instance"},
		{"dotted", vesselAlternatorSettings{PortInstance: "2.current"}, "vessel.alternators.port_instance"},
		{"slash", vesselAlternatorSettings{StarboardInstance: "a/b"}, "vessel.alternators.starboard_instance"},
	}
	for _, c := range cases {
		got := validateVesselAlternators(c.in)
		switch {
		case c.wantErr == "" && got != nil:
			t.Errorf("%s: unexpected error %+v", c.name, got)
		case c.wantErr != "" && (got == nil || got.Field != c.wantErr):
			t.Errorf("%s: got %+v, want field %s", c.name, got, c.wantErr)
		}
	}
}

func TestUpdateSettingsSavesAndRejectsAlternators(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	withTempAlarmRules(t)

	code, _ := postSettings(t, settingsPath, func(p *settingsPayload) {
		p.Vessel = &vesselSettings{Alternators: vesselAlternatorSettings{PortInstance: " 2 ", StarboardInstance: "12"}}
	})
	if code != http.StatusOK {
		t.Fatalf("got %d", code)
	}
	saved, err := loadVesselSettings(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Alternators.PortInstance != "2" || saved.Alternators.StarboardInstance != "12" {
		t.Fatalf("got %+v", saved.Alternators)
	}

	code, body := postSettings(t, settingsPath, func(p *settingsPayload) {
		p.Vessel = &vesselSettings{Alternators: vesselAlternatorSettings{PortInstance: "2", StarboardInstance: "2"}}
	})
	if code == http.StatusOK {
		t.Fatalf("same instance on both sides must be rejected, body %v", body)
	}
	saved, _ = loadVesselSettings(settingsPath)
	if saved.Alternators.StarboardInstance != "12" {
		t.Fatalf("a rejected save must not change the stored value, got %+v", saved.Alternators)
	}
}

func TestVesselCandidatesListAlternatorInstancesWithLiveCurrent(t *testing.T) {
	seedSelfTree(t, alternatorTree)
	got := vesselCandidates(globalSignalKSnapshot, vesselSettings{})
	var ids []string
	cur := map[string]*float64{}
	for _, a := range got.Alternators {
		ids = append(ids, a.Instance)
		cur[a.Instance] = a.Current
	}
	if len(ids) != 3 {
		t.Fatalf("instances: %v", ids)
	}
	if cur["12"] == nil || *cur["12"] != 28.5 {
		t.Fatalf("instance 12 current: %v", cur["12"])
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
