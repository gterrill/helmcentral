package main

import (
	"encoding/json"
	"math"
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

func TestVesselCandidatesListsEnginesAndBatteriesFromTheLiveTree(t *testing.T) {
	snapshot := newVesselCandidatesSnapshot(t)

	got := vesselCandidates(snapshot, vesselSettings{})

	if len(got.Engines) != 2 {
		t.Fatalf("expected 2 engine candidates, got %+v", got.Engines)
	}
	byInstance := map[string]vesselEngineCandidate{}
	for _, e := range got.Engines {
		byInstance[e.Instance] = e
	}
	port, ok := byInstance["port"]
	if !ok || port.RPM == nil || *port.RPM != 1800 || port.CoolantC == nil || math.Abs(*port.CoolantC-76.85) > 1e-6 {
		t.Fatalf("unexpected port candidate: %+v", port)
	}

	if len(got.Batteries) != 2 {
		t.Fatalf("expected 2 battery candidates, got %+v", got.Batteries)
	}
	byPath := map[string]vesselBatteryCandidate{}
	for _, b := range got.Batteries {
		byPath[b.Path] = b
	}
	b0, ok := byPath["electrical.batteries.0"]
	if !ok || b0.Voltage == nil || *b0.Voltage != 27.2 || b0.Current == nil || *b0.Current != 15.6 || b0.SoC == nil || *b0.SoC != 0.79 {
		t.Fatalf("unexpected batteries.0 candidate: %+v", b0)
	}
}

func TestVesselCandidatesSingleEngineFixture(t *testing.T) {
	snapshot := newAnomalyTestSnapshot()
	applyNumeric(snapshot, "vessels.self", "propulsion.main.revolutions", 1200.0/60, anomalyDetectorTestNow)
	applyNumeric(snapshot, "vessels.self", "propulsion.main.temperature", 345, anomalyDetectorTestNow)

	got := vesselCandidates(snapshot, vesselSettings{})
	if len(got.Engines) != 1 || got.Engines[0].Instance != "main" {
		t.Fatalf("expected exactly one 'main' engine candidate, got %+v", got.Engines)
	}
	if len(got.Batteries) != 0 {
		t.Fatalf("expected no battery candidates, got %+v", got.Batteries)
	}
}

func TestVesselCandidatesDetectorStatusReflectsConfiguration(t *testing.T) {
	snapshot := newAnomalyTestSnapshot()

	fresh := vesselCandidates(snapshot, vesselSettings{})
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

	oneEngine := vesselCandidates(snapshot, vesselSettings{Engines: []vesselEngineSetting{{Instance: "port"}}})
	if !oneEngine.Detectors["frozen"].Ready {
		t.Fatalf("expected frozen ready with one engine configured: %+v", oneEngine.Detectors["frozen"])
	}
	if oneEngine.Detectors["engines"].Ready {
		t.Fatalf("expected differentials still not ready with only one engine: %+v", oneEngine.Detectors["engines"])
	}

	twoEngines := vesselCandidates(snapshot, vesselSettings{Engines: []vesselEngineSetting{{Instance: "port"}, {Instance: "starboard"}}})
	if !twoEngines.Detectors["engines"].Ready {
		t.Fatalf("expected differentials ready with two engines configured: %+v", twoEngines.Detectors["engines"])
	}
}

func TestVesselCandidatesBatteryReadyOnceProfileComplete(t *testing.T) {
	snapshot := newAnomalyTestSnapshot()
	equipmentID := setupBatteryTestStore(t)
	setupBatteryProfileFixture(t)

	vessel := vesselSettings{HouseBank: &vesselHouseBankSetting{
		Path: "electrical.batteries.512", EquipmentID: equipmentID, CapacityAh: 400, Cells: 8,
	}}
	got := vesselCandidates(snapshot, vessel)
	if !got.Detectors["battery"].Ready {
		t.Fatalf("expected battery detector ready once the bank and profile are complete: %+v", got.Detectors["battery"])
	}
}

// --- HTTP handlers -----------------------------------------------------

func TestGetVesselCandidatesHandler(t *testing.T) {
	prevSnapshot := globalSignalKSnapshot
	globalSignalKSnapshot = newVesselCandidatesSnapshot(t)
	t.Cleanup(func() { globalSignalKSnapshot = prevSnapshot })

	settingsPath := filepath.Join(t.TempDir(), "settings.yaml")
	t.Setenv("SETTINGS_FILE", settingsPath)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/vessel/candidates", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := getVesselCandidatesHandler(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body vesselCandidatesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(body.Engines) != 2 {
		t.Fatalf("expected 2 engines in the handler response, got %+v", body.Engines)
	}
}
