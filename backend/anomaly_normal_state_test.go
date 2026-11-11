package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// One saved record per subsystem (ADR 0276): trained, skipped with its reason, or
// failed with its problem while the earlier model is kept.

// bothFeed is an engine that ran well before the bank's spells and the bank's own
// feed, so the two never publish the same minute of the engine's revolutions.
func bothFeed(seed int64) map[string][]normalMinuteRow {
	engine := synthFeed("port", seed, []synthRun{gatedRun(daysAgo(30), 70, 1900), gatedRun(daysAgo(25), 70, 1900)})
	return mergeFeeds(engine, houseFeed(seed, houseSpells()))
}

func TestTrainNormal_KeepsOneRecordPerSubsystem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anomaly-normal.json")
	h := newTrainHarness(synthFeed("port", 13, goodRuns()), "port")
	if err := trainNormal(context.Background(), path, h.deps()); err != nil {
		t.Fatal(err)
	}
	got, _ := loadNormalModel(path)
	eng, house := got.Subsystems[normalSubsystemEngines], got.Subsystems[normalSubsystemElectrical]
	if eng.State != normalStateTrained || !eng.AttemptedAt.Equal(trainNow) || eng.Source.Kind != normalSourceMainBucket || eng.Problem != "" {
		t.Fatalf("engines = %+v", eng)
	}
	if house.State != normalStateSkipped || house.Problem != "No house bank is chosen" || house.hasModel() || !house.AttemptedAt.Equal(trainNow) {
		t.Fatalf("house = %+v", house)
	}
}

func TestTrainNormal_AFailedSubsystemKeepsItsEarlierModelAndSaysWhy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anomaly-normal.json")
	h := houseHarness(bothFeed(9))
	h.vessel.Engines = []vesselEngineSetting{{Instance: "port", Name: "Port"}}
	if err := trainNormal(context.Background(), path, h.deps()); err != nil {
		t.Fatal(err)
	}
	first, _ := loadNormalModel(path)
	if first.Subsystems[normalSubsystemElectrical].State != normalStateTrained {
		t.Fatalf("setup: %+v", first.Subsystems[normalSubsystemElectrical])
	}

	h.now = trainNow.Add(24 * time.Hour)
	h.failPathPrefix = "electrical."
	if err := trainNormal(context.Background(), path, h.deps()); err == nil {
		t.Fatal("the bank's failure is the run's")
	}
	second, _ := loadNormalModel(path)
	house, eng := second.Subsystems[normalSubsystemElectrical], second.Subsystems[normalSubsystemEngines]
	if house.State != normalStateFailed || house.Problem != "electrical read failed" || !house.AttemptedAt.Equal(h.now) {
		t.Fatalf("house = %+v", house)
	}
	if !house.TrainedAt.Equal(first.Subsystems[normalSubsystemElectrical].TrainedAt) || house.judgedRegimes() != first.Subsystems[normalSubsystemElectrical].judgedRegimes() {
		t.Fatalf("the earlier model is kept: %+v", house)
	}
	if eng.State != normalStateTrained || eng.Problem != "" || !eng.AttemptedAt.Equal(h.now) {
		t.Fatalf("the engines carry nothing of the bank's failure: %+v", eng)
	}

	h.now = trainNow.Add(48 * time.Hour)
	h.failPathPrefix = ""
	if err := trainNormal(context.Background(), path, h.deps()); err != nil {
		t.Fatal(err)
	}
	third, _ := loadNormalModel(path)
	if s := third.Subsystems[normalSubsystemElectrical]; s.State != normalStateTrained || s.Problem != "" {
		t.Fatalf("a good run clears the failure: %+v", s)
	}
}

// A subsystem that fails the first time has a record too, with its problem, and
// Settings gives it its group.
func TestTrainNormal_ASubsystemThatFailsAtFirstStillHasARecordAndAGroup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anomaly-normal.json")
	h := houseHarness(bothFeed(14))
	h.vessel.Engines = []vesselEngineSetting{{Instance: "port", Name: "Port"}}
	h.failPathPrefix = "electrical."
	_ = trainNormal(context.Background(), path, h.deps())
	got, _ := loadNormalModel(path)
	house := got.Subsystems[normalSubsystemElectrical]
	if house.State != normalStateFailed || house.hasModel() || house.Problem != "electrical read failed" {
		t.Fatalf("house = %+v", house)
	}
	st := buildNormalStatus(got, nil, nil, nil, nil, time.UTC)
	var group *normalStatusSubsystem
	for i := range st.Subsystems {
		if st.Subsystems[i].ID == normalSubsystemElectrical {
			group = &st.Subsystems[i]
		}
	}
	if group == nil || group.Note != "No learned normal: electrical read failed." {
		t.Fatalf("status = %+v", st)
	}
}

// Failing clears skipped, and a good run clears both.
func TestTrainNormal_ASkippedSubsystemBecomesFailedThenTrained(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anomaly-normal.json")
	h := newTrainHarness(synthFeed("port", 13, goodRuns()), "port")
	if err := trainNormal(context.Background(), path, h.deps()); err != nil {
		t.Fatal(err)
	}
	if s, _ := loadNormalModel(path); s.Subsystems[normalSubsystemElectrical].State != normalStateSkipped {
		t.Fatalf("setup: %+v", s.Subsystems[normalSubsystemElectrical])
	}
	hh := houseHarness(bothFeed(15))
	hh.vessel.Engines = []vesselEngineSetting{{Instance: "port", Name: "Port"}}
	hh.failPathPrefix = "electrical."
	_ = trainNormal(context.Background(), path, hh.deps())
	if s, _ := loadNormalModel(path); s.Subsystems[normalSubsystemElectrical].State != normalStateFailed || s.Subsystems[normalSubsystemElectrical].Problem == "" {
		t.Fatalf("failing clears skipped: %+v", s.Subsystems[normalSubsystemElectrical])
	}
}

// The wake check asks each subsystem about its own last attempt's inputs, not its
// model, so one that keeps failing does not retrain on every equipment save.
func TestWakeNormalIfInputsChanged_ComparesWithTheLastAttemptNotTheModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anomaly-normal.json")
	t.Setenv("ANOMALY_NORMAL_FILE", path)
	vessel := houseVessel()
	file := learnedModel(vessel)
	// The bank failed at first: it has a record with its inputs and no model.
	file.Subsystems[normalSubsystemElectrical] = normalSubsystemModel{
		State: normalStateFailed, Problem: "electrical read failed", AttemptedAt: trainNow,
		Inputs: normalInputsOf(normalHouseSubjects(vessel)),
	}
	if err := saveNormalModel(path, file); err != nil {
		t.Fatal(err)
	}
	drainNormalWake()
	wakeNormalIfInputsChanged(vessel)
	if woke() {
		t.Fatal("a subsystem that keeps failing, with the same inputs, is not retrained for an unrelated save")
	}
	other := houseVessel()
	other.RoleItems = map[string]string{"house_bank": "eq-bank", "port_alternator": "eq-alt2", "shore_charger": "eq-sc", "inverter": "eq-inv", "generator": "eq-gen"}
	wakeNormalIfInputsChanged(other)
	if !woke() {
		t.Fatal("an input of its own that changed wakes it")
	}
	// A skipped subsystem is compared the same way.
	skipped := learnedModel(vesselView{Engines: vessel.Engines})
	skipped.Subsystems[normalSubsystemElectrical] = normalSubsystemModel{State: normalStateSkipped, Problem: "No house bank is chosen", AttemptedAt: trainNow}
	if err := saveNormalModel(path, skipped); err != nil {
		t.Fatal(err)
	}
	drainNormalWake()
	wakeNormalIfInputsChanged(vesselView{Engines: vessel.Engines})
	if woke() {
		t.Fatal("still no house bank: nothing changed")
	}
	wakeNormalIfInputsChanged(vessel)
	if !woke() {
		t.Fatal("a house bank chosen wakes a skipped subsystem")
	}
}

// Only the roles the learned normal reads wake it.
func TestNormalInputsChangedBySave_OnlyTheRolesTheNormalReadsWakeIt(t *testing.T) {
	base := settingsPayload{Roles: &vesselRoles{HouseBank: "a"}}
	readout := settingsPayload{Roles: &vesselRoles{HouseBank: "a", VoltageReadoutBattery: "b"}}
	if normalInputsChangedBySave(base, readout) {
		t.Error("the voltage readout battery is not part of the house bank's normal")
	}
	alt := settingsPayload{Roles: &vesselRoles{HouseBank: "a", PortAlternator: "c"}}
	if !normalInputsChangedBySave(base, alt) {
		t.Error("an alternator is")
	}
	ac := settingsPayload{Roles: &vesselRoles{HouseBank: "a", GeneratorACInstance: "1"}}
	if normalInputsChangedBySave(base, ac) {
		t.Error("the generator's AC meter is not read")
	}
}
