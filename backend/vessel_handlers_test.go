package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/labstack/echo/v4"
)

func newVesselCandidatesSnapshot(t *testing.T) *signalKSnapshot {
	t.Helper()
	snapshot := newAnomalyTestSnapshot()
	applyNumeric(snapshot, "vessels.self", "propulsion.port.revolutions", 1800.0/60, anomalyDetectorTestNow)
	applyNumeric(snapshot, "vessels.self", "propulsion.port.temperature", 350, anomalyDetectorTestNow)
	applyNumeric(snapshot, "vessels.self", "propulsion.starboard.revolutions", 1810.0/60, anomalyDetectorTestNow)
	applyNumeric(snapshot, "vessels.self", "propulsion.starboard.temperature", 349, anomalyDetectorTestNow)
	applyNumeric(snapshot, "vessels.self", "electrical.batteries.0.voltage", 27.2, anomalyDetectorTestNow)
	applyNumeric(snapshot, "vessels.self", "electrical.batteries.0.current", 15.6, anomalyDetectorTestNow)
	applyNumeric(snapshot, "vessels.self", "electrical.batteries.0.capacity.stateOfCharge", 0.79, anomalyDetectorTestNow)
	applyNumeric(snapshot, "vessels.self", "electrical.batteries.512.voltage", 27.21, anomalyDetectorTestNow)
	applyNumeric(snapshot, "vessels.self", "electrical.batteries.512.current", 267.6, anomalyDetectorTestNow)
	return snapshot
}

func TestVesselDiscoveryListsEnginesAndBatteriesFromTheLiveTree(t *testing.T) {
	snapshot := newVesselCandidatesSnapshot(t)

	got := vesselDiscovery(snapshot, vesselView{}, nil, sourceRecord{})

	engines := discoveryGroupRows(got, "engine")
	if len(engines) != 2 {
		t.Fatalf("expected 2 engine rows, got %+v", engines)
	}
	byInstance := map[string]discoveryRow{}
	for _, e := range engines {
		byInstance[e.Instance] = e
	}
	port, ok := byInstance["port"]
	if !ok || port.Source != "propulsion.port" || !port.Reporting || len(port.Metrics) != 2 ||
		port.Metrics[0].Text != "1800 rpm" || port.Metrics[1].Text != "77 °C" {
		t.Fatalf("unexpected port row: %+v", port)
	}

	batteries := discoveryGroupRows(got, "battery")
	if len(batteries) != 2 {
		t.Fatalf("expected 2 battery rows, got %+v", batteries)
	}
	bySource := map[string]discoveryRow{}
	for _, b := range batteries {
		bySource[b.Source] = b
	}
	b0, ok := bySource["electrical.batteries.0"]
	if !ok || len(b0.Metrics) != 3 || b0.Metrics[0].Text != "27.20 V" || b0.Metrics[1].Text != "15.6 A" || b0.Metrics[2].Text != "79%" {
		t.Fatalf("unexpected batteries.0 row: %+v", b0)
	}
}

func TestVesselDiscoverySingleEngineFixture(t *testing.T) {
	snapshot := newAnomalyTestSnapshot()
	applyNumeric(snapshot, "vessels.self", "propulsion.main.revolutions", 1200.0/60, anomalyDetectorTestNow)
	applyNumeric(snapshot, "vessels.self", "propulsion.main.temperature", 345, anomalyDetectorTestNow)

	got := vesselDiscovery(snapshot, vesselView{}, nil, sourceRecord{})
	engines := discoveryGroupRows(got, "engine")
	if len(engines) != 1 || engines[0].Instance != "main" {
		t.Fatalf("expected exactly one 'main' engine row, got %+v", engines)
	}
	if len(discoveryGroupRows(got, "battery")) != 0 {
		t.Fatalf("expected no battery rows, got %+v", discoveryGroupRows(got, "battery"))
	}
}

// A row says which item owns its source, and a source an item still owns that
// the boat no longer reports is listed as not reporting rather than dropped.
func TestVesselDiscoveryShowsOwnersAndQuietSources(t *testing.T) {
	snapshot := newVesselCandidatesSnapshot(t)
	items := []sourceItem{
		{ID: "e1", Name: "Port engine", Source: "propulsion.port", Group: sourceEngine, Instance: "port"},
		{ID: "e9", Name: "Old engine", Source: "propulsion.centre", Group: sourceEngine, Instance: "centre"},
	}

	engines := discoveryGroupRows(vesselDiscovery(snapshot, vesselView{}, items, sourceRecord{}), "engine")
	byInstance := map[string]discoveryRow{}
	for _, e := range engines {
		byInstance[e.Instance] = e
	}
	if len(engines) != 3 {
		t.Fatalf("two live engines plus one quiet one, got %+v", engines)
	}
	if r := byInstance["port"]; r.Item == nil || r.Item.ID != "e1" || r.Item.Name != "Port engine" || !r.Reporting {
		t.Errorf("port: %+v", r)
	}
	if r := byInstance["starboard"]; r.Item != nil || !r.Reporting {
		t.Errorf("starboard has no item yet: %+v", r)
	}
	if r := byInstance["centre"]; r.Item == nil || r.Reporting || len(r.Metrics) != 0 {
		t.Errorf("a quiet source shows its item and no figures: %+v", r)
	}
}

func TestVesselDiscoveryDetectorStatusReflectsConfiguration(t *testing.T) {
	snapshot := newAnomalyTestSnapshot()

	fresh := vesselDiscovery(snapshot, vesselView{}, nil, sourceRecord{})
	if fresh.Detectors["frozen"].Ready {
		t.Fatalf("expected frozen not ready with no engines configured: %+v", fresh.Detectors["frozen"])
	}
	if fresh.Detectors["frozen"].Missing == "" {
		t.Fatalf("expected a missing-what message for an unconfigured detector")
	}
	if fresh.Detectors["battery"].Ready {
		t.Fatalf("expected battery not ready with no house bank configured: %+v", fresh.Detectors["battery"])
	}
	if fresh.Detectors["engines"].Ready {
		t.Fatalf("expected engines not ready with fewer than two configured: %+v", fresh.Detectors["engines"])
	}

	oneEngine := vesselDiscovery(snapshot, vesselView{Engines: []vesselEngineSetting{{Instance: "port"}}}, nil, sourceRecord{})
	if !oneEngine.Detectors["frozen"].Ready {
		t.Fatalf("expected frozen ready with one engine configured: %+v", oneEngine.Detectors["frozen"])
	}
	if oneEngine.Detectors["engines"].Ready {
		t.Fatalf("expected differentials still not ready with only one engine: %+v", oneEngine.Detectors["engines"])
	}

	twoEngines := vesselDiscovery(snapshot, vesselView{Engines: []vesselEngineSetting{{Instance: "port"}, {Instance: "starboard"}}}, nil, sourceRecord{})
	if !twoEngines.Detectors["engines"].Ready {
		t.Fatalf("expected differentials ready with two engines configured: %+v", twoEngines.Detectors["engines"])
	}
}

func TestVesselDiscoveryBatteryReadyOnceProfileComplete(t *testing.T) {
	snapshot := newAnomalyTestSnapshot()
	equipmentID := setupBatteryTestStore(t)
	setupBatteryProfileFixture(t)

	vessel := vesselView{HouseBank: &vesselHouseBankSetting{
		Path: "electrical.batteries.512", EquipmentID: equipmentID, CapacityAh: 400, Cells: 8,
	}}
	got := vesselDiscovery(snapshot, vessel, nil, sourceRecord{})
	if !got.Detectors["battery"].Ready {
		t.Fatalf("expected battery detector ready once the bank and profile are complete: %+v", got.Detectors["battery"])
	}
}

// --- HTTP handlers -----------------------------------------------------

func TestGetVesselDiscoveryHandler(t *testing.T) {
	prevSnapshot := globalSignalKSnapshot
	globalSignalKSnapshot = newVesselCandidatesSnapshot(t)
	t.Cleanup(func() { globalSignalKSnapshot = prevSnapshot })

	settingsPath := filepath.Join(t.TempDir(), "settings.yaml")
	t.Setenv("SETTINGS_FILE", settingsPath)
	store := vesselFixtureStore(t)
	if _, err := store.CreateEquipment(equipmentItem{Name: "Port engine", SignalKSource: "propulsion.port"}); err != nil {
		t.Fatal(err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/vessel/discovery", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := getVesselDiscoveryHandler(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body discoveryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	engines := discoveryGroupRows(body, "engine")
	if len(engines) != 2 {
		t.Fatalf("expected 2 engines in the handler response, got %+v", engines)
	}
	if engines[0].Instance != "port" || engines[0].Item == nil || engines[0].Item.Name != "Port engine" {
		t.Fatalf("the response should say which item owns port: %+v", engines[0])
	}
}
