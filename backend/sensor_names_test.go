package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The fixtures under testdata/sensors were captured from the boat's SignalK
// server on 2026-10-07: GET /signalk/v1/api/sources (trimmed to the battery
// related devices) and GET /signalk/v1/api/vessels/self/electrical/batteries
// (instances 0-3, 278 and 292). Every Victron device on that bus reports
// n2k deviceClass "Electrical Generation", the shunts and the BMS included,
// so only deviceFunction 160 (charger) tells a solar charger apart.

var sensorNamesTestNow = time.Date(2026, 10, 7, 0, 20, 0, 0, time.UTC)

func loadSensorSourcesFixture(t *testing.T) map[string]sourceDevice {
	t.Helper()
	raw, err := os.ReadFile("testdata/sensors/signalk-sources-2026-10-07.json")
	if err != nil {
		t.Fatalf("reading sources fixture: %v", err)
	}
	devices, err := parseSourceDevices(raw)
	if err != nil {
		t.Fatalf("parsing sources fixture: %v", err)
	}
	return devices
}

// loadSensorBatteriesFixture replays the captured batteries tree into a
// snapshot the way the delta stream would: one update per source in each
// leaf's per-source values map, or the leaf's own $source when it has none.
func loadSensorBatteriesFixture(t *testing.T, snapshot *signalKSnapshot) {
	t.Helper()
	raw, err := os.ReadFile("testdata/sensors/signalk-batteries-2026-10-07.json")
	if err != nil {
		t.Fatalf("reading batteries fixture: %v", err)
	}
	instances := map[string]map[string]map[string]any{}
	if err := json.Unmarshal(raw, &instances); err != nil {
		t.Fatalf("parsing batteries fixture: %v", err)
	}
	at := sensorNamesTestNow
	for id, leaves := range instances {
		for quantity, leaf := range leaves {
			value, hasValue := leaf["value"]
			if !hasValue || quantity == "capacity" {
				continue
			}
			path := "electrical.batteries." + id + "." + quantity
			perSource, _ := leaf["values"].(map[string]any)
			if len(perSource) == 0 {
				source, _ := leaf["$source"].(string)
				perSource = map[string]any{source: map[string]any{"value": value}}
			}
			for source, entry := range perSource {
				snapshot.applyDelta(signalKDelta{Context: "vessels.self", Updates: []signalKUpdate{{
					SourceRef: source,
					Timestamp: at.Format(time.RFC3339),
					Values:    []signalKValue{{Path: path, Value: entry.(map[string]any)["value"]}},
				}}}, at)
			}
		}
	}
}

func newSensorNamesFixtureNamer(t *testing.T, vessel vesselSettings) (*sensorNamer, *signalKSnapshot) {
	t.Helper()
	snapshot := newAnomalyTestSnapshot()
	loadSensorBatteriesFixture(t, snapshot)
	return newSensorNamer(snapshot, vessel, loadSensorSourcesFixture(t)), snapshot
}

func TestChargerInputInstanceIsSkippedButBanksAreNot(t *testing.T) {
	namer, _ := newSensorNamesFixtureNamer(t, vesselSettings{})

	// Instance 1 is published only by YachtDevices.36-39, the four BlueSolar
	// MPPT chargers: their 76 V is the panel array, not a battery.
	if !namer.isChargerInstance("1") {
		t.Errorf("instance 1 (four solar chargers only) should be treated as a charger input")
	}
	// Instance 0 mixes chargers with the BMS and the Quattro; 2 and 3 are
	// SmartShunts. All carry deviceClass "Electrical Generation" too, which
	// is why the class alone must not decide.
	for _, id := range []string{"0", "2", "3", "278", "292"} {
		if namer.isChargerInstance(id) {
			t.Errorf("instance %s must still be range-checked as a battery", id)
		}
	}
}

func TestChargerInstanceWithUnknownSourceInfoStaysChecked(t *testing.T) {
	snapshot := newAnomalyTestSnapshot()
	loadSensorBatteriesFixture(t, snapshot)
	// No sources directory at all: the conservative answer is "a battery".
	namer := newSensorNamer(snapshot, vesselSettings{}, nil)
	if namer.isChargerInstance("1") {
		t.Errorf("with no source information instance 1 must keep being checked as a battery")
	}
	// One of its four publishers unknown is the same: not EVERY source is known to be a charger.
	devices := loadSensorSourcesFixture(t)
	delete(devices, "YachtDevices.39")
	if newSensorNamer(snapshot, vesselSettings{}, devices).isChargerInstance("1") {
		t.Errorf("an unknown publisher must keep the instance in the battery check")
	}
}

func TestImpossibleReadingSkipsChargerInputInstanceInTheDetector(t *testing.T) {
	snapshot := newAnomalyTestSnapshot()
	loadSensorBatteriesFixture(t, snapshot)
	settingsPath := filepath.Join(t.TempDir(), "settings.yaml")
	globalSourceDevices.set(loadSensorSourcesFixture(t))
	t.Cleanup(func() { globalSourceDevices.set(nil) })

	reading := computeAnomalyReading(snapshot, settingsPath, newAnomalyTrackers(), &twinSteadinessTrackers{}, sensorNamesTestNow)
	if got := reading.Values[anomalySensorOutOfRangeCountPath]; got != 0 {
		t.Fatalf("live fixture (76 V solar input, healthy banks) must not alarm, count = %v, sensors = %+v", got, reading.Sensors[anomalySensorOutOfRangeCountPath])
	}
}

func TestImpossibleReadingStillFlagsARealBankAt757Volts(t *testing.T) {
	snapshot := newAnomalyTestSnapshot()
	loadSensorBatteriesFixture(t, snapshot)
	// The SmartShunt on instance 3 (PORT START BATT) suddenly reads 75.7 V.
	snapshot.applyDelta(signalKDelta{Context: "vessels.self", Updates: []signalKUpdate{{
		SourceRef: "YachtDevices.224", Timestamp: sensorNamesTestNow.Format(time.RFC3339),
		Values: []signalKValue{{Path: "electrical.batteries.3.voltage", Value: 75.7}},
	}}}, sensorNamesTestNow)
	settingsPath := filepath.Join(t.TempDir(), "settings.yaml")
	globalSourceDevices.set(loadSensorSourcesFixture(t))
	t.Cleanup(func() { globalSourceDevices.set(nil) })

	reading := computeAnomalyReading(snapshot, settingsPath, newAnomalyTrackers(), &twinSteadinessTrackers{}, sensorNamesTestNow)
	entries := reading.Sensors[anomalySensorOutOfRangeCountPath]
	if len(entries) != 1 {
		t.Fatalf("expected one impossible reading, got %+v", entries)
	}
	want := "PORT START BATT voltage 75.7 V · above the 70 V any 12, 24 or 48 V bank reaches"
	if entries[0].Text != want {
		t.Errorf("text = %q, want %q", entries[0].Text, want)
	}
	if entries[0].Identifier != "electrical.batteries.3.voltage" {
		t.Errorf("identifier = %q, the ignore action needs the raw path", entries[0].Identifier)
	}
	if got := reading.Evidence[anomalySensorOutOfRangeCountPath]; got != "electrical.batteries.3.voltage" {
		t.Errorf("evidence = %q, ignore still submits the raw path", got)
	}
}

func TestBatteryNameChain(t *testing.T) {
	vessel := vesselSettings{Batteries: []vesselBatterySetting{{Instance: "3", Name: "Port Engine Starter Battery"}}}
	namer, _ := newSensorNamesFixtureNamer(t, vessel)

	cases := []struct{ path, want string }{
		// 1. The operator's own name wins over everything the bus says.
		{"electrical.batteries.3.voltage", "Port Engine Starter Battery"},
		// 2. The bus's own .name path (Venus GX).
		{"electrical.batteries.292.voltage", "PORT START BATT"},
		// 3. installationDescription1 of the source (SmartShunt, no .name).
		{"electrical.batteries.2.voltage", "STBD START BATT"},
		// 4. Product name of the source: instance 1 is only BlueSolar chargers.
		{"electrical.batteries.1.voltage", "BlueSolar Charger MPPT 100/50 re"},
		// 5. Nothing known about the instance at all.
		{"electrical.batteries.77.voltage", "Battery 77"},
	}
	for _, c := range cases {
		if got := namer.thingName(c.path); got != c.want {
			t.Errorf("thingName(%s) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestBatteryNamePrefersABatteryDeviceOverAChargerOnTheSameInstance(t *testing.T) {
	namer, _ := newSensorNamesFixtureNamer(t, vesselSettings{})
	// Instance 0 is published by chargers, a Quattro and the Batrium BMS; the
	// BMS (function 170, battery) is the one that names the bank.
	if got := namer.thingName("electrical.batteries.0.voltage"); got != "Batrium-BMS (Victron profile)" {
		t.Errorf("thingName(batteries.0) = %q", got)
	}
}

func TestEngineNameChain(t *testing.T) {
	snapshot := newAnomalyTestSnapshot()
	vessel := vesselSettings{Engines: []vesselEngineSetting{{Instance: "port", Name: "Port"}, {Instance: "stbd", Name: "Starboard engine"}}}
	namer := newSensorNamer(snapshot, vessel, nil)
	cases := []struct{ path, want string }{
		{"propulsion.port.temperature", "Port engine"},
		{"propulsion.stbd.temperature", "Starboard engine"},
		{"propulsion.centre.temperature", "Engine centre"},
	}
	for _, c := range cases {
		if got := namer.thingName(c.path); got != c.want {
			t.Errorf("thingName(%s) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestSensorQuantityWords(t *testing.T) {
	cases := map[string]string{
		"electrical.batteries.1.voltage":                "voltage",
		"electrical.batteries.1.current":                "current",
		"electrical.batteries.1.capacity.stateOfCharge": "state of charge",
		"electrical.batteries.1.temperature":            "temperature",
		"propulsion.port.temperature":                   "coolant temperature",
		"propulsion.port.oilPressure":                   "oil pressure",
		"propulsion.port.boostPressure":                 "boost pressure",
		"propulsion.port.engineLoad":                    "engine load",
		"propulsion.port.revolutions":                   "rpm",
		"propulsion.port.transmission.oilPressure":      "gear oil pressure",
		"propulsion.port.transmission.oilTemperature":   "gear oil temperature",
	}
	for path, want := range cases {
		if got := sensorQuantity(path); got != want {
			t.Errorf("sensorQuantity(%s) = %q, want %q", path, got, want)
		}
	}
}

func TestImpossibleReadingTextPerQuantity(t *testing.T) {
	vessel := vesselSettings{Engines: []vesselEngineSetting{{Instance: "port", Name: "Port"}}}
	namer := newSensorNamer(newAnomalyTestSnapshot(), vessel, nil)
	cases := []struct {
		path  string
		value float64
		want  string
	}{
		{"propulsion.port.temperature", 233.15, "Port engine coolant temperature -40 °C · below the -23 °C a running or resting engine reads"},
		{"propulsion.port.temperature", 420, "Port engine coolant temperature 146.9 °C · above the 137 °C no engine coolant reaches"},
		{"propulsion.port.revolutions", 120, "Port engine rpm 7200 RPM · above the 6300 RPM no engine turns"},
		{"propulsion.port.engineLoad", 1.4, "Port engine load 140 % · above the 105 % a load reading can reach"},
		{"electrical.batteries.5.voltage", -2, "Battery 5 voltage -2 V · below the 0 V a bank cannot go under"},
		{"electrical.batteries.5.capacity.stateOfCharge", 1.3, "Battery 5 state of charge 130 % · above the 105 % a charge reading can reach"},
		{"electrical.batteries.5.current", 900, "Battery 5 current 900 A · above the 800 A any bank shunt measures"},
	}
	for _, c := range cases {
		entry := namer.rangeEntry(c.path, c.value)
		if entry.Text != c.want {
			t.Errorf("rangeEntry(%s, %v).Text = %q, want %q", c.path, c.value, entry.Text, c.want)
		}
		if entry.Identifier != c.path {
			t.Errorf("identifier = %q, want raw path", entry.Identifier)
		}
		if strings.Contains(entry.Text, "electrical.") || strings.Contains(entry.Text, "propulsion.") {
			t.Errorf("text leaks a path: %q", entry.Text)
		}
	}
}

func TestFrozenAndSilentSourceTexts(t *testing.T) {
	vessel := vesselSettings{Engines: []vesselEngineSetting{{Instance: "port", Name: "Port"}}}
	namer, _ := newSensorNamesFixtureNamer(t, vessel)

	frozen := namer.frozenEntry("propulsion.port.oilPressure")
	if want := "Port engine oil pressure has not changed in 15 minutes while rpm varied"; frozen.Text != want {
		t.Errorf("frozen text = %q, want %q", frozen.Text, want)
	}
	silent := namer.silentSourceEntry("YachtDevices.224")
	if want := "PORT START BATT has stopped sending"; silent.Text != want {
		t.Errorf("silent text = %q, want %q", silent.Text, want)
	}
	if silent.Identifier != "YachtDevices.224" {
		t.Errorf("silent identifier = %q, ignore needs the $source id", silent.Identifier)
	}
	// A source the sources tree knows nothing about still gets a plain name.
	venus := namer.silentSourceEntry("venus.com.victronenergy.battery.278")
	if want := "STBD START BATT has stopped sending"; venus.Text != want {
		t.Errorf("venus battery source text = %q, want %q (named from the bus's own .name path)", venus.Text, want)
	}
	// Nothing readable is known: a plugin id or a bus address is not shown.
	for _, src := range []string{"derived-data", "ydwg.12"} {
		e := namer.silentSourceEntry(src)
		if e.Text != "An instrument has stopped sending" || e.Name != "An instrument" || e.Label != "An instrument" {
			t.Errorf("%s: name %q label %q text %q, want a plain phrase", src, e.Name, e.Label, e.Text)
		}
		if e.Identifier != src {
			t.Errorf("%s: identifier = %q, ignore needs the $source id", src, e.Identifier)
		}
	}
}

func TestDuplicateNamesAreNumbered(t *testing.T) {
	entries := []sensorHealthEntry{
		{Identifier: "a", Name: "BlueSolar Charger MPPT 100/50 re", Label: "BlueSolar Charger MPPT 100/50 re", Text: "BlueSolar Charger MPPT 100/50 re has stopped sending"},
		{Identifier: "b", Name: "BlueSolar Charger MPPT 100/50 re", Label: "BlueSolar Charger MPPT 100/50 re", Text: "BlueSolar Charger MPPT 100/50 re has stopped sending"},
		{Identifier: "c", Name: "Other", Label: "Other", Text: "Other has stopped sending"},
	}
	out := disambiguateSensorEntries(entries)
	if out[0].Name != "BlueSolar Charger MPPT 100/50 re (1 of 2)" || out[1].Name != "BlueSolar Charger MPPT 100/50 re (2 of 2)" || out[2].Name != "Other" {
		t.Errorf("names = %q %q %q", out[0].Name, out[1].Name, out[2].Name)
	}
	if out[1].Label != "BlueSolar Charger MPPT 100/50 re (2 of 2)" {
		t.Errorf("label = %q", out[1].Label)
	}
	if out[1].Text != "BlueSolar Charger MPPT 100/50 re (2 of 2) has stopped sending" {
		t.Errorf("text = %q", out[1].Text)
	}
}

func TestParseSourceDevicesReadsClassFunctionAndNames(t *testing.T) {
	devices := loadSensorSourcesFixture(t)
	d := devices["YachtDevices.224"]
	if d.DeviceClass != "Electrical Generation" || d.DeviceFunction != 170 || d.InstallationDescription1 != "PORT START BATT" || d.ModelID != "SmartShunt 500A/50mV" {
		t.Errorf("YachtDevices.224 = %+v", d)
	}
	if _, ok := devices["venus.com.victronenergy.battery.278"]; ok {
		t.Errorf("a source with no n2k block carries no device info and must not appear")
	}
}

func TestVesselCandidatesListBatteryInstancesWithBusNamesAndSolarFlag(t *testing.T) {
	globalSourceDevices.set(loadSensorSourcesFixture(t))
	t.Cleanup(func() { globalSourceDevices.set(nil) })
	snapshot := newAnomalyTestSnapshot()
	loadSensorBatteriesFixture(t, snapshot)

	resp := vesselCandidates(snapshot, vesselSettings{Batteries: []vesselBatterySetting{{Instance: "3", Name: "Port Engine Starter Battery"}}})
	byInstance := map[string]vesselBatteryCandidate{}
	for _, b := range resp.Batteries {
		byInstance[b.Instance] = b
	}
	if b := byInstance["1"]; !b.ChargerInput || b.BusName != "BlueSolar Charger MPPT 100/50 re" {
		t.Errorf("instance 1 = %+v", b)
	}
	// The bus name is what the BUS calls it, even when the operator has named it.
	if b := byInstance["3"]; b.ChargerInput || b.BusName != "PORT START BATT" {
		t.Errorf("instance 3 = %+v", b)
	}
	if b := byInstance["292"]; b.BusName != "PORT START BATT" {
		t.Errorf("instance 292 = %+v", b)
	}
}

func TestNormalizeVesselBatteries(t *testing.T) {
	got := normalizeVesselBatteries([]vesselBatterySetting{
		{Instance: " 3 ", Name: "  Port Engine Starter Battery "},
		{Instance: "2", Name: "   "},
		{Instance: "3", Name: "duplicate"},
		{Instance: "", Name: "orphan"},
		{Instance: "1", Name: "Solar array"},
	})
	if len(got) != 2 || got[0] != (vesselBatterySetting{Instance: "1", Name: "Solar array"}) || got[1] != (vesselBatterySetting{Instance: "3", Name: "Port Engine Starter Battery"}) {
		t.Errorf("normalized = %+v", got)
	}
}

func TestVesselBatteryNamesRoundTripThroughTheVesselBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.yaml")
	want := vesselSettings{Batteries: []vesselBatterySetting{{Instance: "3", Name: "Port Engine Starter Battery"}}}
	if err := saveVesselSettings(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadVesselSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Batteries) != 1 || got.Batteries[0] != want.Batteries[0] {
		t.Errorf("round trip = %+v", got.Batteries)
	}
}

func TestIdentifierEntryNamesPathsAndSources(t *testing.T) {
	namer, _ := newSensorNamesFixtureNamer(t, vesselSettings{})
	if e := namer.identifierEntry("electrical.batteries.2.voltage"); e.Label != "STBD START BATT voltage" || e.Identifier != "electrical.batteries.2.voltage" {
		t.Errorf("path entry = %+v", e)
	}
	if e := namer.identifierEntry("YachtDevices.36"); e.Label != "BlueSolar Charger MPPT 100/50 re" {
		t.Errorf("source entry = %+v", e)
	}
}

// A mains charger (function 160 like the MPPTs) that is the only thing on its
// instance, and publishes nothing else, has no output bank to point at: the
// instance stays in the battery check.
func TestLoneChargerInstanceStaysChecked(t *testing.T) {
	snapshot := newAnomalyTestSnapshot()
	snapshot.applyDelta(signalKDelta{Context: "vessels.self", Updates: []signalKUpdate{{
		SourceRef: "YachtDevices.42", Timestamp: sensorNamesTestNow.Format(time.RFC3339),
		Values: []signalKValue{{Path: "electrical.batteries.9.voltage", Value: 76.0}},
	}}}, sensorNamesTestNow)
	namer := newSensorNamer(snapshot, vesselSettings{}, loadSensorSourcesFixture(t))
	if namer.isChargerInstance("9") {
		t.Errorf("a lone Skylla-like source on its only instance must still be range-checked")
	}
	// Publishing a second instance that only chargers publish is not an output bank either.
	snapshot.applyDelta(signalKDelta{Context: "vessels.self", Updates: []signalKUpdate{{
		SourceRef: "YachtDevices.42", Timestamp: sensorNamesTestNow.Format(time.RFC3339),
		Values: []signalKValue{{Path: "electrical.batteries.8.voltage", Value: 28.0}},
	}}}, sensorNamesTestNow)
	if namer.isChargerInstance("9") {
		t.Errorf("an output instance with no non-charger publisher does not qualify")
	}
}

func TestSilentSourceTakesABankNameOnlyAsItsSolePublisher(t *testing.T) {
	namer, _ := newSensorNamesFixtureNamer(t, vesselSettings{})
	// YachtDevices.224 is the only publisher of instance 3: it takes the bank's name.
	if got := namer.silentSourceEntry("YachtDevices.224").Name; got != "PORT START BATT" {
		t.Errorf("sole publisher name = %q", got)
	}
	// A charger that publishes only instance 7 alongside a shunt must not be named after the bank.
	snapshot := newAnomalyTestSnapshot()
	for _, src := range []string{"YachtDevices.36", "YachtDevices.228"} {
		snapshot.applyDelta(signalKDelta{Context: "vessels.self", Updates: []signalKUpdate{{
			SourceRef: src, Timestamp: sensorNamesTestNow.Format(time.RFC3339),
			Values: []signalKValue{{Path: "electrical.batteries.7.voltage", Value: 27.0}},
		}}}, sensorNamesTestNow)
	}
	n := newSensorNamer(snapshot, vesselSettings{Batteries: []vesselBatterySetting{{Instance: "7", Name: "House Bank"}}}, loadSensorSourcesFixture(t))
	if got := n.silentSourceEntry("YachtDevices.36").Text; got != "BlueSolar Charger MPPT 100/50 re has stopped sending" {
		t.Errorf("shared-instance charger text = %q", got)
	}
}

func TestSourceDeviceRefresherRetriesQuicklyUntilFirstRead(t *testing.T) {
	calls := 0
	got := make(chan map[string]sourceDevice, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fetch := func() (map[string]sourceDevice, error) {
		calls++
		if calls < 3 {
			return nil, errors.New("signalk down")
		}
		return map[string]sourceDevice{"a": {}}, nil
	}
	go runSourceDeviceRefresher(ctx, fetch, func(m map[string]sourceDevice) { got <- m }, 5*time.Millisecond, time.Hour)
	select {
	case <-got:
		if calls != 3 {
			t.Errorf("calls = %d, want 3", calls)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("no successful read after %d calls: the retry interval was not used", calls)
	}
}

func TestIgnoredSensorEntriesNumberIdenticalNames(t *testing.T) {
	namer, _ := newSensorNamesFixtureNamer(t, vesselSettings{})
	out := ignoredSensorEntries(namer, []string{"YachtDevices.36", "YachtDevices.37", "YachtDevices.224"})
	if out[0].Label != "BlueSolar Charger MPPT 100/50 re (1 of 2)" || out[1].Label != "BlueSolar Charger MPPT 100/50 re (2 of 2)" || out[2].Label != "PORT START BATT" {
		t.Errorf("labels = %q %q %q", out[0].Label, out[1].Label, out[2].Label)
	}
	if out[1].Identifier != "YachtDevices.37" {
		t.Errorf("identifier must stay raw: %q", out[1].Identifier)
	}
}

func TestSourceNameFallsBackToManufacturer(t *testing.T) {
	namer, _ := newSensorNamesFixtureNamer(t, vesselSettings{})
	namer.devices = map[string]sourceDevice{"ydwg.12": {Manufacturer: "Victron Energy", DeviceClass: "Electrical Generation"}}
	e := namer.silentSourceEntry("ydwg.12")
	if e.Name != "Victron Energy device" || e.Text != "Victron Energy device has stopped sending" || e.Identifier != "ydwg.12" {
		t.Errorf("entry = %+v", e)
	}
}
