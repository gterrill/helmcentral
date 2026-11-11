package main

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Place by steady readings (ADR 0276): which sources are on comes from the
// engines' revolutions, the inverter, the shore AC input and solar, never from a
// reading that was filled in.

// numericEngines renames the port engine's feed to engines 0 and 1, with any names.
func numericEngines(feed map[string][]normalMinuteRow) (map[string][]normalMinuteRow, vesselView) {
	out := map[string][]normalMinuteRow{}
	for path, rows := range feed {
		if rest, ok := strings.CutPrefix(path, "propulsion.port."); ok {
			for i := range rows {
				rows[i].Path = "propulsion.0." + rest
			}
			out["propulsion.0."+rest] = rows
			zero := make([]normalMinuteRow, len(rows))
			for i, r := range rows {
				zero[i] = normalMinuteRow{Path: "propulsion.1." + rest, Source: r.Source, At: r.At}
			}
			out["propulsion.1."+rest] = zero
			continue
		}
		out[path] = rows
	}
	v := houseVessel()
	v.Engines = []vesselEngineSetting{{Instance: "0", Name: "Wally"}, {Instance: "1", Name: "Bob"}}
	return out, v
}

func altSpells(first float64) []houseSpell {
	spells := houseSpells()
	for i := 0; i < 2; i++ {
		s := gatedHouse(daysAgo(first-float64(i)), 70, 0.7)
		s.Alt = true
		spells = append(spells, s)
	}
	return spells
}

func TestHousePlacement_AnAlternatorOnNumericEnginesWithAnyNamesIsLearned(t *testing.T) {
	feed, vessel := numericEngines(houseFeed(81, altSpells(13)))
	h := newTrainHarness(feed)
	h.vessel = &vessel
	m := h.trainHouse(t, normalSubsystemModel{})
	none := regimeNamed(t, m, "house/none/soc60")
	engines := regimeNamed(t, m, "house/engines/soc60")
	if !none.Judged || none.Minutes != 140 {
		t.Fatalf("nothing charging keeps its minutes though the alternator says nothing there: %+v", none)
	}
	if !engines.Judged || !slices.Contains(engines.Features, "alternator") || slices.Contains(none.Features, "alternator.p") {
		t.Fatalf("the alternator is fitted where it reports: engines %v, none %v", engines.Features, none.Features)
	}
}

// A reading that places the minute is never fitted, and a reading is fitted only
// in the regimes where it normally reports.
func TestHousePlacement_ReadingsAreFittedWhereTheyReport(t *testing.T) {
	spells := altSpells(13)
	for i := 0; i < 2; i++ {
		s := gatedHouse(daysAgo(6-float64(i)), 70, 0.7)
		s.Shore = true
		spells = append(spells, s)
	}
	m := houseHarness(houseFeed(82, spells)).trainHouse(t, normalSubsystemModel{})
	shore := regimeNamed(t, m, "house/shore/soc60")
	none := regimeNamed(t, m, "house/none/soc60")
	if !shore.Judged || !slices.Contains(shore.Features, "charger") || slices.Contains(shore.Features, "shoreIn") {
		t.Fatalf("the charger is fitted on shore power, and the shore input only places the minute: %v", shore.Features)
	}
	if slices.Contains(none.Features, "charger") || slices.Contains(none.Features, "alternator") {
		t.Fatalf("with nothing charging neither reports: %v", none.Features)
	}
}

// A solar controller added after training changes what the bank is made of: the
// model waits for the next training run, and scoring says so instead of failing.
func TestScoreHouse_ASolarControllerAddedAfterTrainingWaitsForTheNextRun(t *testing.T) {
	v := houseVessel()
	v.Solar = append(v.Solar, vesselSolarSetting{Instance: "s2", Name: "Rail"})
	c := newScoreCase(normalModelFile{Subsystems: map[string]normalSubsystemModel{normalSubsystemElectrical: houseModel(t)}}, sagFeed(85))
	c.vessel = &v
	res := c.scoreHouse(24 * time.Hour)
	if res.ProblemKind != unusualNeverTrained || res.Problem != normalNoModelYet {
		t.Fatalf("problem %q %q", res.ProblemKind, res.Problem)
	}
}

// With no alternator reporting while the engine turns, the total is missing: the
// minutes in the engines band are not judged and counted, and nothing is said of a
// quiet sender.
func TestScoreHouse_NoAlternatorReportingInAnEnginesBandLeavesTheMinutesUnjudged(t *testing.T) {
	model := houseHarness(houseFeed(88, altSpells(13))).trainHouse(t, normalSubsystemModel{})
	v := houseVessel()
	c := newScoreCase(normalModelFile{Subsystems: map[string]normalSubsystemModel{normalSubsystemElectrical: model}},
		houseFeed(89, []houseSpell{{Start: scoreRunStart, Minutes: 60, SOC: 0.7, EngineOn: true}}))
	c.vessel = &v
	res := c.scoreHouse(24 * time.Hour)
	var missing bool
	for _, n := range res.NotJudged {
		if n.Kind == "sender_silent" {
			t.Fatalf("the alternators' total is never a silent sender: %+v", res.NotJudged)
		}
		missing = missing || (n.Kind == "missing_reading" && strings.Contains(n.Reason, "Alternator current"))
	}
	if !missing || res.JudgedMinutes != 0 || c.wakes != 0 {
		t.Fatalf("judged %d wakes %d not judged %+v", res.JudgedMinutes, c.wakes, res.NotJudged)
	}
}

// Nothing is ever filled: a missing reading stays missing.
func TestJoinNormalMinutes_NeverFillsAMissingReading(t *testing.T) {
	features := []normalFeature{{Key: "voltage", Required: true}, {Key: "alt"}}
	t0 := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	got := joinNormalMinutes(features, map[string]map[time.Time]float64{"voltage": {t0: 26}, "alt": {}}, func(string) bool { return true })
	if len(got) != 1 {
		t.Fatalf("minutes = %+v", got)
	}
	if _, ok := got[0].Values["alt"]; ok {
		t.Fatalf("a missing reading was filled: %+v", got[0].Values)
	}
}

// --- the third review ------------------------------------------------------------

// The engine publishes nothing while it is off, and the shore input likewise: a
// minute with neither is a minute with the engines off.
func TestHousePlacement_AnEngineThatPublishesNothingWhileOffIsOff(t *testing.T) {
	m := houseHarness(houseFeed(91, altSpells(13))).trainHouse(t, normalSubsystemModel{})
	none := regimeNamed(t, m, "house/none/soc60")
	if !none.Judged || none.Minutes != 140 {
		t.Fatalf("the minutes at anchor are placed: %+v", none)
	}
}

// Placing readings are read from any sender, not the one pinned.
func TestHousePlacement_RevolutionsAreReadFromAnySender(t *testing.T) {
	feed := houseFeed(92, altSpells(13))
	rows := feed["propulsion.port.revolutions"]
	for i := range rows {
		if rows[i].At.Before(daysAgo(12.5)) {
			rows[i].Source = "can0.2"
		}
	}
	m := houseHarness(feed).trainHouse(t, normalSubsystemModel{})
	engines, none := regimeNamed(t, m, "house/engines/soc60"), regimeNamed(t, m, "house/none/soc60")
	if !engines.Judged || engines.Minutes != 140 || none.Minutes != 140 {
		t.Fatalf("both senders place the minute: engines %d, none %d", engines.Minutes, none.Minutes)
	}
}

// Switching a placing reading off in the ignored list does not drop it.
func TestHousePlacement_ThePlacingReadingsAreNotOnTheIgnoredList(t *testing.T) {
	h := houseHarness(houseFeed(93, altSpells(13)))
	h.ignored = map[string]bool{"propulsion.port.revolutions": true, "electrical.solar.s1.panelPower": true, "electrical.inverters.inv.acin.power": true}
	m := h.trainHouse(t, normalSubsystemModel{})
	if r := regimeNamed(t, m, "house/engines/soc60"); !r.Judged {
		t.Fatalf("the engines are still placed: %+v", r)
	}
	st := buildNormalStatus(normalModelFile{Subsystems: map[string]normalSubsystemModel{normalSubsystemElectrical: m}}, nil, nil, nil, nil, time.UTC)
	for _, r := range st.Subsystems[0].Subjects[0].Readings {
		if strings.Contains(r.Key, "solar") || strings.HasPrefix(r.Key, "rpm.") || r.Key == "inverterIn" || r.Key == "shoreIn" {
			t.Fatalf("a reading that places the minute has no switch: %+v", r)
		}
	}
}

// The inverter's input has one regime key, whether or not a generator is set.
func TestHouseRegimes_TheKeyDoesNotChangeWhenAGeneratorIsChosen(t *testing.T) {
	at := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	minute := annotated([]normalMinute{{At: at, Values: map[string]float64{"soc": 0.7, "inverterIn": 8000}}})[0]
	sub := normalElectricalSubsystem()
	with := houseVessel()
	without := houseVessel()
	without.Electrical.GeneratorInstance = ""
	k1, l1, _ := sub.Regime(sub.Subjects(with)[0], minute)
	k2, l2, _ := sub.Regime(sub.Subjects(without)[0], minute)
	if k1 != k2 || l1 == l2 {
		t.Fatalf("keys %q %q, labels %q %q: only the label varies", k1, k2, l1, l2)
	}
}

// A band carried forward must not keep a reading this run leaves out of its fit.
func TestTrain_ACarriedBandDoesNotKeepAReadingThisRunPrunedFromIt(t *testing.T) {
	first := houseHarness(houseFeed(96, altSpells(13))).trainHouse(t, normalSubsystemModel{})
	if r := first.Regimes["house/engines/soc60"]; !r.Judged || !slices.Contains(r.Features, "alternator") {
		t.Fatalf("setup: %+v", r)
	}
	later := trainNow.Add(100 * 24 * time.Hour)
	at := func(d float64) time.Time { return later.Add(-time.Duration(d * 24 * float64(time.Hour))) }
	spells := []houseSpell{gatedHouse(at(10), 70, 0.7), gatedHouse(at(8), 70, 0.7)}
	// The alternator still reports, in another band, so it stays in use.
	for i := 0; i < 2; i++ {
		s := gatedHouse(at(6-float64(i)), 70, 0.5)
		s.Alt = true
		spells = append(spells, s)
	}
	// In the old band the engine runs for a short spell and the alternator says nothing.
	thin := gatedHouse(at(3), 30, 0.7)
	thin.EngineOn = true
	spells = append(spells, thin)
	h := houseHarness(houseFeed(97, spells))
	h.now = later
	second := h.trainHouse(t, first)
	r := second.Regimes["house/engines/soc60"]
	if r.CarriedForward || r.Judged {
		t.Fatalf("the old band had the alternator and this run's data has none: %+v", r)
	}
}

// All subsystems share the run's window, so the alarm log is read once.
func TestTrainNormal_TheWindowIsTheRunsAndTheAlarmLogIsReadOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anomaly-normal.json")
	h := houseHarness(bothFeed(98))
	h.vessel.Engines = []vesselEngineSetting{{Instance: "port", Name: "Port"}}
	h.nowStep = time.Minute
	if err := trainNormal(context.Background(), path, h.deps()); err != nil {
		t.Fatal(err)
	}
	if h.alarmReads != 1 {
		t.Fatalf("the alarm log was read %d times", h.alarmReads)
	}
	got, _ := loadNormalModel(path)
	if !got.Subsystems[normalSubsystemEngines].Window.To.Equal(got.Subsystems[normalSubsystemElectrical].Window.To) {
		t.Fatalf("windows: %+v %+v", got.Subsystems[normalSubsystemEngines].Window, got.Subsystems[normalSubsystemElectrical].Window)
	}
}

// The line describes one record: the oldest, with its own source.
func TestMergeUnusual_TheModelLineTakesTheTimeAndSourceFromOneRecord(t *testing.T) {
	older := time.Date(2026, 11, 9, 3, 0, 0, 0, time.UTC)
	newer := older.Add(24 * time.Hour)
	subs := []normalSubsystem{normalEnginesSubsystem(), normalElectricalSubsystem()}
	results := []unusualResult{
		{Model: &unusualModelInfo{TrainedAt: newer, Source: normalSource{Kind: normalSourceMainBucket}, Bands: map[string]int{"Port": 1}}},
		{Model: &unusualModelInfo{TrainedAt: older, Source: normalSource{Kind: normalSourceMinuteBucket, Bucket: "b"}, Bands: map[string]int{"House bank": 2}}},
	}
	got := mergeUnusual(unusualResult{}, subs, results)
	if !got.Model.TrainedAt.Equal(older) || got.Model.Source.Bucket != "b" || got.Model.Bands["Port"] != 1 || got.Model.Bands["House bank"] != 2 {
		t.Fatalf("model = %+v", got.Model)
	}
}

// --- the fourth review: every source reading only places the minute --------------

func TestHousePlacement_ABoatWithNoSolarDataLearnsItsBands(t *testing.T) {
	spells := houseSpells()
	for i := range spells {
		spells[i].NoSolar = true
	}
	m := houseHarness(houseFeed(101, spells)).trainHouse(t, normalSubsystemModel{})
	if r := regimeNamed(t, m, "house/none/soc60"); !r.Judged || r.Minutes != 140 {
		t.Fatalf("regime = %+v", r)
	}
}

// A controller that publishes nothing at night: the night minutes are "nothing
// charging", and the day's are on solar.
func TestHousePlacement_ASolarControllerSilentAtNightStillLearnsTheNight(t *testing.T) {
	var spells []houseSpell
	for i := 0; i < 2; i++ {
		night := gatedHouse(daysAgo(10-float64(i)), 70, 0.7)
		night.NoSolar = true
		day := gatedHouse(daysAgo(6-float64(i)), 70, 0.7)
		day.SolarW = 300
		spells = append(spells, night, day)
	}
	m := houseHarness(houseFeed(102, spells)).trainHouse(t, normalSubsystemModel{})
	night, day := regimeNamed(t, m, "house/none/soc60"), regimeNamed(t, m, "house/solar/soc60")
	if !night.Judged || night.Minutes != 140 || !day.Judged || day.Minutes != 140 {
		t.Fatalf("night %+v day %+v", night, day)
	}
}

// Solar power only says whether solar is charging. It is not a judged reading.
func TestHousePlacement_SolarPowerIsNotAJudgedReading(t *testing.T) {
	var spells []houseSpell
	for i := 0; i < 2; i++ {
		day := gatedHouse(daysAgo(6-float64(i)), 70, 0.7)
		day.SolarW = 300
		spells = append(spells, day, gatedHouse(daysAgo(10-float64(i)), 70, 0.7))
	}
	m := houseHarness(houseFeed(103, spells)).trainHouse(t, normalSubsystemModel{})
	day := regimeNamed(t, m, "house/solar/soc60")
	if !day.Judged || slices.Contains(day.Features, "solar.s1") || slices.Contains(day.Features, "inverterIn") {
		t.Fatalf("features = %v", day.Features)
	}
	for _, v := range m.Subjects["house"].Features {
		if v.Key == "solar.s1" && (v.Used || !v.Placing) {
			t.Fatalf("solar = %+v", v)
		}
	}
}

// A minute is never dropped for a missing placing reading.
func TestScoreHouse_AnAlternatorWithNoRevolutionsIsStillJudged(t *testing.T) {
	model := houseHarness(houseFeed(104, altSpells(13))).trainHouse(t, normalSubsystemModel{})
	v := houseVessel()
	c := newScoreCase(normalModelFile{Subsystems: map[string]normalSubsystemModel{normalSubsystemElectrical: model}},
		houseFeed(105, []houseSpell{{Start: scoreRunStart, Minutes: 60, SOC: 0.7, Alt: true, NoRPM: true}}))
	c.vessel = &v
	res := c.scoreHouse(24 * time.Hour)
	for _, n := range res.NotJudged {
		if n.Kind == "unplaced" {
			t.Fatalf("not judged = %+v", res.NotJudged)
		}
	}
	if res.JudgedMinutes == 0 {
		t.Fatalf("nothing was judged: %+v", res.NotJudged)
	}
}

// A vessel that cannot be read keeps each subsystem's earlier inputs, so the next
// equipment save does not retrain for an input nobody changed.
func TestTrainNormal_AFailedVesselReadKeepsTheEarlierInputs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anomaly-normal.json")
	h := houseHarness(bothFeed(106))
	if err := trainNormal(context.Background(), path, h.deps()); err != nil {
		t.Fatal(err)
	}
	first, _ := loadNormalModel(path)
	h.vesselErr = errors.New("settings.yaml is unreadable")
	h.now = trainNow.Add(24 * time.Hour)
	if err := trainNormal(context.Background(), path, h.deps()); err == nil {
		t.Fatal("an unreadable vessel is a failed run")
	}
	second, _ := loadNormalModel(path)
	for _, name := range []string{normalSubsystemEngines, normalSubsystemElectrical} {
		if len(first.Subsystems[name].Inputs) == 0 || !sameNormalInputs(first.Subsystems[name].Inputs, second.Subsystems[name].Inputs) || second.Subsystems[name].State != normalStateFailed {
			t.Fatalf("%s: inputs %+v then %+v", name, first.Subsystems[name], second.Subsystems[name])
		}
	}
}

func TestNormalInputsOf_DoesNotDependOnTheOrderOfTheItems(t *testing.T) {
	a := normalInputsOf([]normalSubject{{ID: "house", EquipmentIDs: []string{"b", "a"}}})
	b := normalInputsOf([]normalSubject{{ID: "house", EquipmentIDs: []string{"a", "b"}}})
	if !sameNormalInputs(a, b) {
		t.Fatalf("%+v vs %+v", a, b)
	}
}
