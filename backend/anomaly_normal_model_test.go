package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestLoadNormalModel_MissingFileIsEmptyNotAnError(t *testing.T) {
	m, err := loadNormalModel(filepath.Join(t.TempDir(), "anomaly-normal.json"))
	if err != nil {
		t.Fatalf("a missing file is a boat that has not trained yet: %v", err)
	}
	if len(m.Subsystems) != 0 {
		t.Fatalf("want empty, got %+v", m)
	}
}

func TestLoadNormalModel_CorruptFileIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anomaly-normal.json")
	if err := os.WriteFile(path, []byte(`{"subsystems": {`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadNormalModel(path); err == nil {
		t.Fatal("a file that does not parse must be reported, not read as no model")
	}
}

func TestNormalModel_RoundTripKeepsAnUnknownSubsystem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anomaly-normal.json")
	at := time.Date(2026, 11, 10, 3, 0, 0, 0, time.UTC)
	want := normalModelFile{
		Subsystems: map[string]normalSubsystemModel{
			"engines": {
				State: normalStateFailed, AttemptedAt: at, Problem: "the one-minute bucket \"x\" could not be read: nope",
				Inputs:    []normalSubjectInputs{{Subject: "port", EquipmentIDs: []string{"eq"}, Paths: []string{"propulsion.port.oilPressure"}}},
				TrainedAt: at,
				Window:    normalWindow{From: at.Add(-90 * 24 * time.Hour), To: at},
				Source:    normalSource{Kind: normalSourceMinuteBucket, Bucket: "b"},
				Subjects: map[string]normalSubjectModel{"port": {Name: "Port engine", GatedMinutes: 812, Features: []normalFeatureVerdict{
					{Key: "oilPressure", Path: "propulsion.port.oilPressure", SIUnit: "Pa", Source: "can0.1", Used: true},
					{Key: "transmission.oilTemperature", Path: "p", Used: false, Reason: "never changed while running"},
				}}},
				Regimes: map[string]normalRegime{"port/rpm1800": {
					Subject: "port", Label: "1,800 to 2,000 rpm", LearnedAt: at, Window: normalWindow{From: at.Add(-time.Hour), To: at},
					Minutes: 812, Runs: 9, Judged: true, Features: []string{"oilPressure"},
					normalRegimeStats: normalRegimeStats{Mean: []float64{3e5}, SD: []float64{1e4}, Corr: [][]float64{{1}}, Ridge: 0.1, Threshold: 21.4, TrainD2P50: 6.1},
					Shift:             &normalShift{FromLearnedAt: at.Add(-24 * time.Hour), MeanDistance: 0.8},
				}},
				Excluded: []normalExcluded{{Subject: "port", From: at, To: at.Add(time.Hour), Reason: "alarm", Ref: "a1", Title: "Oil pressure low"}},
			},
			"house": {TrainedAt: at, Regimes: map[string]normalRegime{"x": {Subject: "bank", Label: "charging"}}},
		},
	}
	if err := saveNormalModel(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadNormalModel(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip changed the model:\n got  %+v\n want %+v", got, want)
	}
	if _, ok := got.Subsystems["house"]; !ok {
		t.Fatal("a subsystem this build does not train must survive a load and save")
	}
}
