package main

import (
	"context"
	"math/rand"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The house bank as the second subsystem of the learned normal (ADR 0276): one
// subject, regimes by what is charging and the state of charge.

// houseVessel is a boat with a house bank, one alternator, a shore charger, an
// inverter and a solar controller.
func houseVessel() vesselView {
	return vesselView{
		HouseBank:   &vesselHouseBankSetting{Path: "electrical.batteries.1", EquipmentID: "eq-bank"},
		Alternators: vesselAlternatorSettings{PortInstance: "p"},
		Electrical:  vesselElectricalSettings{ShoreChargerInstance: "sc", InverterInstance: "inv", GeneratorInstance: "g"},
		Engines:     []vesselEngineSetting{{Instance: "port", Name: "Port"}},
		Solar:       []vesselSolarSetting{{Instance: "s1", Name: "Roof"}},
		RoleItems: map[string]string{
			"house_bank": "eq-bank", "port_alternator": "eq-alt", "shore_charger": "eq-sc", "inverter": "eq-inv", "generator": "eq-gen",
		},
	}
}

// houseSpell is one spell with one set of things charging. Minutes counts every
// minute of the spell; the first nine fall to the ten-minute gate, so a spell of
// Minutes judges Minutes-9 of them.
type houseSpell struct {
	Start    time.Time
	Minutes  int
	SOC      float64
	Shore    bool
	Inverter bool // AC input from the generator
	Alt      bool
	SolarW   float64
	// EngineOn publishes the port engine's revolutions. Alt implies it.
	EngineOn bool
	// Alt2 is the starboard alternator reporting too.
	Alt2 bool
	// NoRPM leaves the engine's revolutions out while it runs, as a gap in its feed.
	NoRPM bool
	// NoSolar leaves the solar controller out, as it publishes nothing at night.
	NoSolar bool
	// Sag lowers the bank voltage by that many volts: the unusual reading.
	Sag float64
}

// gatedHouse is a spell that leaves exactly n judged minutes.
func gatedHouse(start time.Time, n int, soc float64) houseSpell {
	return houseSpell{Start: start, Minutes: n + 9, SOC: soc}
}

// houseFeed builds the bank's one-minute rows by path. An alternator and a
// shore charger publish only while they work; the inverter and the solar
// controller publish zero when idle, as they do on the boat.
func houseFeed(seed int64, spells []houseSpell) map[string][]normalMinuteRow {
	rng := rand.New(rand.NewSource(seed))
	out := map[string][]normalMinuteRow{}
	add := func(path string, at time.Time, v float64) {
		out[path] = append(out[path], normalMinuteRow{Path: path, Source: "can0.1", At: at, Value: v})
	}
	for _, s := range spells {
		for i := 0; i < s.Minutes; i++ {
			at := s.Start.Add(time.Duration(i) * time.Minute).Truncate(time.Minute)
			current := -12 + 1.5*rng.NormFloat64()
			if s.Shore {
				current += 25
			}
			if s.Inverter {
				current += 60
			}
			if s.Alt {
				current += 45
			}
			if s.Alt2 {
				current += 35
			}
			current += s.SolarW / 26
			add("electrical.batteries.1.voltage", at, 25+2*s.SOC+0.02*current-s.Sag+0.03*rng.NormFloat64())
			add("electrical.batteries.1.current", at, current)
			add("electrical.batteries.1.capacity.stateOfCharge", at, s.SOC+0.001*rng.NormFloat64())
			add("electrical.inverters.inv.acin.power", at, map[bool]float64{true: 8000 + 150*rng.NormFloat64(), false: 0}[s.Inverter])
			add("electrical.inverters.inv.ac.output.power", at, 400+40*rng.NormFloat64())
			if !s.NoSolar {
				add("electrical.solar.s1.panelPower", at, max(0, s.SolarW+10*rng.NormFloat64()*min(1, s.SolarW)))
			}
			if s.Shore {
				add("electrical.chargers.sc.acin.1.current", at, 12+0.8*rng.NormFloat64())
				add("electrical.chargers.sc.current", at, 25+1.2*rng.NormFloat64())
			}
			// The engine publishes nothing while it is off. NoRPM is a gap while it runs.
			if (s.Alt || s.Alt2 || s.EngineOn) && !s.NoRPM {
				add("propulsion.port.revolutions", at, 30+0.05*rng.NormFloat64())
			}
			if s.Alt {
				add("electrical.alternator.p.current", at, 45+3*rng.NormFloat64())
			}
			if s.Alt2 {
				add("electrical.alternator.q.current", at, 35+3*rng.NormFloat64())
			}
		}
	}
	return out
}

// houseHarness is a training harness for the house vessel.
func houseHarness(feed map[string][]normalMinuteRow) *trainHarness {
	h := newTrainHarness(feed)
	v := houseVessel()
	h.vessel = &v
	return h
}

func (h *trainHarness) trainHouse(t *testing.T, prev normalSubsystemModel) normalSubsystemModel {
	t.Helper()
	m, err := trainSubsystem(context.Background(), normalElectricalSubsystem(), h.deps(), prev)
	if err != nil {
		t.Fatalf("trainSubsystem: %v", err)
	}
	return m
}

// houseDays are two spells of each of nothing charging and the generator, a day
// apart, each leaving 70 judged minutes.
func houseSpells() []houseSpell {
	gen := func(d float64) houseSpell {
		s := gatedHouse(daysAgo(d), 70, 0.7)
		s.Inverter = true
		return s
	}
	return []houseSpell{gatedHouse(daysAgo(10), 70, 0.7), gatedHouse(daysAgo(8), 70, 0.7), gen(9), gen(7)}
}

func TestHouseSubjects_AreOneBankMadeOfItsRoleItemsAndReadings(t *testing.T) {
	sub := normalElectricalSubsystem()
	if sub.Name != "electrical" || sub.Title != "House bank" {
		t.Fatalf("subsystem = %q %q", sub.Name, sub.Title)
	}
	if got := sub.Subjects(vesselView{}); len(got) != 0 {
		t.Fatalf("a boat with no house bank has no subject: %+v", got)
	}
	subjects := sub.Subjects(houseVessel())
	if len(subjects) != 1 || subjects[0].ID != "house" || subjects[0].Name != "House bank" {
		t.Fatalf("subjects = %+v", subjects)
	}
	s := subjects[0]
	if want := []string{"eq-bank", "eq-alt", "eq-sc", "eq-inv", "eq-gen"}; !slices.Equal(s.EquipmentIDs, want) {
		t.Fatalf("a repair to the bank or any item that charges it counts: %v, want %v", s.EquipmentIDs, want)
	}
	byKey := map[string]normalFeature{}
	for _, f := range s.Features {
		byKey[f.Key] = f
	}
	for key, want := range map[string]struct {
		path        string
		required    bool
		explain     bool
		placing     bool
		minChange   float64
		displayName string
	}{
		"voltage":        {"electrical.batteries.1.voltage", true, true, false, 0.05, "House bank voltage"},
		"current":        {"electrical.batteries.1.current", true, true, false, 2, "House bank current"},
		"soc":            {"electrical.batteries.1.capacity.stateOfCharge", true, false, false, 0.01, "House bank state of charge"},
		"solar.s1":       {"electrical.solar.s1.panelPower", false, false, true, 0, "Roof solar power"},
		"alternator.p":   {"electrical.alternator.p.current", false, false, false, 0, "Port alternator current"},
		"alternator":     {"", false, true, false, 2, "Alternator current"},
		"charger":        {"electrical.chargers.sc.current", false, true, false, 2, "Shore charger current"},
		"shoreIn":        {"electrical.chargers.sc.acin.1.current", false, false, true, 0, "Shore power input current"},
		"inverterIn":     {"electrical.inverters.inv.acin.power", false, false, true, 0, "Inverter AC input power"},
		"acLoad.output":  {"electrical.inverters.inv.ac.output.power", false, true, false, 50, "Inverter AC load"},
		"acLoad.acout":   {"electrical.inverters.inv.acout.power", false, true, false, 50, "Inverter AC load"},
		"acLoad.acOutPw": {"electrical.inverters.inv.acOutputPower", false, true, false, 50, "Inverter AC load"},
	} {
		f, ok := byKey[key]
		if !ok {
			t.Errorf("no reading %s among %v", key, keysOfFeatures(s.Features))
			continue
		}
		if f.Path != want.path || f.Required != want.required || f.Explain != want.explain || f.Placing != want.placing || f.MinChange != want.minChange || f.Name != want.displayName {
			t.Errorf("%s = %+v, want %+v", key, f, want)
		}
	}
	for _, key := range []string{"alternator", "charger", "acLoad.output"} {
		if f := byKey[key]; !f.PerRegime || f.Placing || f.Required {
			t.Errorf("%s is explained, never required or placing, and fitted where it reports: %+v", key, f)
		}
	}
	if f := byKey["rpm.port"]; !f.Placing || f.Path != "propulsion.port.revolutions" {
		t.Errorf("each engine's revolutions place the minute: %+v", f)
	}
	for _, key := range []string{"shoreIn", "solar.s1", "inverterIn", "rpm.port"} {
		if f := byKey[key]; !f.Placing || f.Explain {
			t.Errorf("%s only places the minute: %+v", key, f)
		}
	}
	if f := byKey["acLoad.output"]; f.Group == "" || f.Group != byKey["acLoad.acout"].Group || f.Group != byKey["acLoad.acOutPw"].Group {
		t.Errorf("the three AC load paths are alternatives for one reading: %+v", f)
	}
}

func keysOfFeatures(fs []normalFeature) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Key)
	}
	return out
}

// With no solar controller set up the Victron system total stands in, and with no
// inverter, charger or alternator the boat is offered none of their readings.
func TestHouseSubjects_SolarFallsBackToTheSystemTotalAndAbsentPartsAreNotOffered(t *testing.T) {
	v := vesselView{HouseBank: &vesselHouseBankSetting{Path: "electrical.batteries.3", EquipmentID: "eq-bank"}, RoleItems: map[string]string{"house_bank": "eq-bank"}}
	s := normalElectricalSubsystem().Subjects(v)[0]
	var got []string
	for _, f := range s.Features {
		got = append(got, f.Key+"="+f.Path)
	}
	want := []string{
		"voltage=electrical.batteries.3.voltage", "current=electrical.batteries.3.current", "soc=electrical.batteries.3.capacity.stateOfCharge",
		"solar=electrical.venus.totalPanelPower",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("features = %v, want %v", got, want)
	}
	if !slices.Equal(s.EquipmentIDs, []string{"eq-bank"}) {
		t.Fatalf("equipment = %v", s.EquipmentIDs)
	}
}

func TestHouseSources_AreReadFromTheMinutesWithTheOvernightModelsThresholds(t *testing.T) {
	sub := normalElectricalSubsystem()
	s := sub.Subjects(houseVessel())[0]
	at := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		values map[string]float64
		key    string
		label  string
	}{
		{"nothing", map[string]float64{"soc": 0.7, "shoreIn": 0.5, "inverterIn": 100, "rpm.port": 0, "solar.s1": 99}, "house/none/soc60", "with nothing charging, 60 to 80 % charge"},
		{"shore above half an amp", map[string]float64{"soc": 0.7, "shoreIn": 0.6}, "house/shore/soc60", "on shore power, 60 to 80 % charge"},
		{"generator above 100 W", map[string]float64{"soc": 0.7, "inverterIn": 101}, "house/inv/soc60", "on generator, 60 to 80 % charge"},
		{"any engine turning", map[string]float64{"soc": 0.7, "rpm.port": 0.1}, "house/engines/soc60", "on engines, 60 to 80 % charge"},
		{"engines stopped", map[string]float64{"soc": 0.7, "rpm.port": 0, "alternator.p": 40}, "house/none/soc60", "with nothing charging, 60 to 80 % charge"},
		{"solar from 100 W", map[string]float64{"soc": 0.7, "solar.s1": 100}, "house/solar/soc60", "on solar, 60 to 80 % charge"},
		{"two controllers add up", map[string]float64{"soc": 0.7, "solar.s1": 60, "solar.s2": 60}, "house/solar/soc60", "on solar, 60 to 80 % charge"},
		{"solar and the generator", map[string]float64{"soc": 0.65, "solar.s1": 300, "inverterIn": 8000}, "house/solar+inv/soc60", "on solar and generator, 60 to 80 % charge"},
		{"all four", map[string]float64{"soc": 0.3, "shoreIn": 12, "inverterIn": 8000, "rpm.port": 30, "solar.s1": 300}, "house/shore+solar+inv+engines/soc0", "on shore power, solar, generator and engines, under 40 % charge"},
		{"band edges", map[string]float64{"soc": 0.4}, "house/none/soc40", "with nothing charging, 40 to 60 % charge"},
		{"top band", map[string]float64{"soc": 1.0}, "house/none/soc80", "with nothing charging, 80 to 100 % charge"},
	}
	for _, c := range cases {
		key, label, ok := sub.Regime(s, annotated([]normalMinute{{At: at, Values: c.values}})[0])
		if !ok || key != c.key || label != c.label {
			t.Errorf("%s: regime = %q %q %v, want %q %q", c.name, key, label, ok, c.key, c.label)
		}
	}
}

// With no generator chosen, the inverter's AC input is only "AC input".
func TestHouseSources_AnInverterInputIsTheGeneratorOnlyWhenOneIsSet(t *testing.T) {
	v := houseVessel()
	v.Electrical.GeneratorInstance = ""
	delete(v.RoleItems, "generator")
	sub := normalElectricalSubsystem()
	s := sub.Subjects(v)[0]
	key, label, ok := sub.Regime(s, annotated([]normalMinute{{At: time.Now(), Values: map[string]float64{"soc": 0.7, "inverterIn": 8000}}})[0])
	if !ok || key != "house/inv/soc60" || label != "on AC input, 60 to 80 % charge" {
		t.Fatalf("regime = %q %q %v", key, label, ok)
	}
}

// annotated is the minutes classified, as the trainer and the scorer give them.
func annotated(ms []normalMinute) []normalMinute {
	normalHouseAnnotate(ms)
	return ms
}

func houseMinutes(start time.Time, n int, set func(i int) map[string]float64) []normalMinute {
	out := make([]normalMinute, n)
	for i := range out {
		out[i] = normalMinute{At: start.Add(time.Duration(i) * time.Minute), Values: set(i)}
	}
	return out
}

func TestHouseGate_NeedsTenContiguousMinutesOfOneSourceSet(t *testing.T) {
	sub := normalElectricalSubsystem()
	start := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	still := func(int) map[string]float64 { return map[string]float64{"soc": 0.7, "shoreIn": 0} }
	got := sub.Gate(annotated(houseMinutes(start, 12, still)))
	for i, g := range got {
		if g != (i >= 9) {
			t.Fatalf("minute %d: gate %v, the tenth minute is the first judged: %v", i, g, got)
		}
	}
	// The source set changes at minute 12: a new spell, and ten more minutes.
	changes := func(i int) map[string]float64 {
		if i >= 12 {
			return map[string]float64{"soc": 0.7, "shoreIn": 12}
		}
		return still(i)
	}
	got = sub.Gate(annotated(houseMinutes(start, 24, changes)))
	for i, g := range got {
		want := (i >= 9 && i < 12) || i >= 21
		if g != want {
			t.Fatalf("minute %d: gate %v, want %v: %v", i, g, want, got)
		}
	}
	// A missing minute starts the count again.
	ms := houseMinutes(start, 24, still)
	ms = append(ms[:12], ms[13:]...)
	got = sub.Gate(annotated(ms))
	for i, g := range got {
		want := (i >= 9 && i < 12) || i >= 21
		if g != want {
			t.Fatalf("with a gap, minute %d: gate %v, want %v: %v", i, g, want, got)
		}
	}
}

func TestHouseRuns_AreUnbrokenSpellsOfOneSourceSetWithShortGapsJoined(t *testing.T) {
	sub := normalElectricalSubsystem()
	start := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	still := func(int) map[string]float64 { return map[string]float64{"soc": 0.7} }
	ms := houseMinutes(start, 30, still)
	// A five minute step is one run, a six minute step is two.
	ms = slices.DeleteFunc(ms, func(m normalMinute) bool {
		d := int(m.At.Sub(start) / time.Minute)
		return (d >= 5 && d < 9) || (d >= 15 && d < 20)
	})
	runs := sub.Run(annotated(ms))
	seen := map[int]bool{}
	for _, r := range runs {
		seen[r] = true
	}
	// Minutes 0-4 and 9-14 are one run (a four minute hole), 20-29 is another.
	if len(seen) != 2 || runs[0] != runs[5] || runs[0] == runs[len(runs)-1] {
		t.Fatalf("runs = %v", runs)
	}
	// A change of what is charging ends a run with no gap at all.
	changed := houseMinutes(start, 20, func(i int) map[string]float64 {
		if i >= 10 {
			return map[string]float64{"soc": 0.7, "shoreIn": 12}
		}
		return still(i)
	})
	runs = sub.Run(annotated(changed))
	if runs[9] == runs[10] || runs[0] != runs[9] || runs[10] != runs[19] {
		t.Fatalf("runs = %v", runs)
	}
}

func TestHouseTraining_LearnsABandPerSourceSetAndLeavesOutReadingsThatDoNotMoveInIt(t *testing.T) {
	h := houseHarness(houseFeed(3, houseSpells()))
	m := h.trainHouse(t, normalSubsystemModel{})
	none := regimeNamed(t, m, "house/none/soc60")
	if !none.Judged || none.Minutes != 140 || none.Runs != 2 || none.Label != "with nothing charging, 60 to 80 % charge" {
		t.Fatalf("regime = %+v", none)
	}
	if want := []string{"voltage", "current", "soc", "acLoad.output"}; !slices.Equal(none.Features, want) {
		t.Fatalf("with nothing charging only the bank and the load move: %v, want %v", none.Features, want)
	}
	gen := regimeNamed(t, m, "house/inv/soc60")
	if !gen.Judged || gen.Label != "on generator, 60 to 80 % charge" {
		t.Fatalf("regime = %+v", gen)
	}
	if slices.Contains(gen.Features, "inverterIn") || slices.Contains(gen.Features, "alternator") {
		t.Fatalf("generator features = %v", gen.Features)
	}
	subj := m.Subjects["house"]
	if subj.Name != "House bank" || subj.GatedMinutes != 280 || !slices.Equal(subj.EquipmentIDs, []string{"eq-bank", "eq-alt", "eq-sc", "eq-inv", "eq-gen"}) {
		t.Fatalf("subject = %+v", subj)
	}
	verdicts := map[string]normalFeatureVerdict{}
	for _, v := range subj.Features {
		verdicts[v.Key] = v
	}
	if v := verdicts["alternator.p"]; v.Used {
		t.Fatalf("an alternator that never reported while its engine was off is all zeros: %+v", v)
	}
	if v := verdicts["rpm.port"]; !v.Placing || v.Used {
		t.Fatalf("the engine's revolutions are read to place the alternator and never judged: %+v", v)
	}
	if v := verdicts["solar.s1"]; v.Used {
		t.Fatalf("solar never moved above zero in this feed: %+v", v)
	}
	for _, key := range []string{"acLoad.acout", "acLoad.acOutPw"} {
		if v := verdicts[key]; v.Used {
			t.Fatalf("only the first AC load path with rows is used: %s %+v", key, v)
		}
	}
	if v := verdicts["acLoad.output"]; !v.Used || v.Name != "Inverter AC load" {
		t.Fatalf("ac load = %+v", v)
	}
}

// A charging source that is off publishes nothing, which is zero. It is not a
// reading that is missing too often.
func TestHouseTraining_AnAlternatorThatIsOffMostOfTheTimeIsStillUsed(t *testing.T) {
	spells := houseSpells()
	for i := 0; i < 2; i++ {
		s := gatedHouse(daysAgo(13-float64(i)), 70, 0.7)
		s.Alt = true
		spells = append(spells, s)
	}
	m := houseHarness(houseFeed(4, spells)).trainHouse(t, normalSubsystemModel{})
	v := map[string]normalFeatureVerdict{}
	for _, f := range m.Subjects["house"].Features {
		v[f.Key] = f
	}
	if !v["alternator"].Used {
		t.Fatalf("alternator = %+v", v["alternator"])
	}
	engines := regimeNamed(t, m, "house/engines/soc60")
	if !engines.Judged || !slices.Contains(engines.Features, "alternator") {
		t.Fatalf("engines band = %+v", engines)
	}
}

func TestHouseTraining_AnHourFromTwoSpellsIsJudgedAndFiftyNineMinutesAreNot(t *testing.T) {
	train := func(a, b int) normalRegime {
		spells := []houseSpell{gatedHouse(daysAgo(10), a, 0.7), gatedHouse(daysAgo(8), b, 0.7)}
		return regimeNamed(t, houseHarness(houseFeed(5, spells)).trainHouse(t, normalSubsystemModel{}), "house/none/soc60")
	}
	if r := train(30, 30); !r.Judged || r.Minutes != 60 {
		t.Fatalf("60 minutes = %+v", r)
	}
	r := train(30, 29)
	if r.Judged || r.Reason != "59 minutes learned, needs 60" {
		t.Fatalf("59 minutes = %+v", r)
	}
	r = train(70, 0)
	if r.Judged || r.Reason != "70 minutes from 1 run, needs 2 runs" {
		t.Fatalf("one spell = %+v", r)
	}
}

func TestHouseAlarms_BelongToTheBankItsChargersAndTheFullBankDetectorOnly(t *testing.T) {
	sub := normalElectricalSubsystem()
	s := sub.Subjects(houseVessel())[0]
	for path, want := range map[string]bool{
		"notifications.electrical.batteries.1.voltage":               true,
		"electrical.batteries.1.capacity.stateOfCharge":              true,
		"electrical.batteries.2.voltage":                             false, // a starter battery
		"notifications.electrical.alternator.p.current":              true,
		"electrical.alternator.q.current":                            false,
		"electrical.chargers.sc.current":                             true,
		"notifications.electrical.inverters.inv.acin.power":          true,
		"helmcentral.anomaly.battery.fullBankCharging":               true,
		"notifications.helmcentral.anomaly.battery.fullBankCharging": true,
		"propulsion.port.oilPressure":                                false,
		"electrical.batteries.10.voltage":                            false, // not a child of battery 1
	} {
		if got := normalAlarmBelongs(s, path); got != want {
			t.Errorf("%s belongs = %v, want %v", path, got, want)
		}
	}
}

func TestHouseTraining_AnAlarmOnTheBankTakesItsMinutesOutOfTheFit(t *testing.T) {
	h := houseHarness(houseFeed(6, houseSpells()))
	cleared := daysAgo(10).Add(45 * time.Minute)
	h.alarms = []alarmLogEntry{{ID: "a1", Label: "House bank current high", Path: "notifications.electrical.batteries.1.current", RaisedAt: daysAgo(10).Add(30 * time.Minute), ClearedAt: &cleared}}
	m := h.trainHouse(t, normalSubsystemModel{})
	r := regimeNamed(t, m, "house/none/soc60")
	if r.ExcludedMinutes == 0 || len(m.Excluded) != 1 || m.Excluded[0].Subject != "house" || m.Excluded[0].Reason != normalReasonAlarm {
		t.Fatalf("regime = %+v excluded = %+v", r, m.Excluded)
	}
}

// A repair to an alternator is a fault range for the house bank.
func TestHouseTraining_ARepairOnAChargingItemLeavesOutTheBanksRange(t *testing.T) {
	h := houseHarness(houseFeed(7, houseSpells()))
	h.boatLoc = time.UTC
	day := daysAgo(10).Format("2006-01-02")
	h.repairs = []maintenanceRepair{{ID: "log1", EquipmentID: "eq-alt", StartedAt: day, PerformedAt: day, Description: "New alternator regulator"}}
	m := h.trainHouse(t, normalSubsystemModel{})
	if len(m.Excluded) != 1 || m.Excluded[0].Reason != normalReasonRepair || m.Excluded[0].Subject != "house" {
		t.Fatalf("excluded = %+v", m.Excluded)
	}
}

// --- the file-level run ------------------------------------------------------

func TestTrainNormal_ABoatWithNoHouseBankStillTrainsItsEngines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anomaly-normal.json")
	h := newTrainHarness(synthFeed("port", 13, goodRuns()), "port")
	if err := trainNormal(context.Background(), path, h.deps()); err != nil {
		t.Fatalf("a boat without a house bank is not a failed run: %v", err)
	}
	got, _ := loadNormalModel(path)
	if got.Subsystems[normalSubsystemEngines].judgedRegimes() != 1 {
		t.Fatalf("engines = %+v", got.Subsystems[normalSubsystemEngines])
	}
	if rec := got.Subsystems[normalSubsystemElectrical]; rec.hasModel() || rec.State != normalStateSkipped || rec.Problem != "No house bank is chosen" {
		t.Fatalf("nothing was learned for the house bank: %+v", rec)
	}
}

func TestTrainNormal_ABoatWithABankAndNoEnginesTrainsTheBank(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anomaly-normal.json")
	h := houseHarness(houseFeed(8, houseSpells()))
	// No engines, so no alternator can be told to be off.
	h.vessel.Engines, h.vessel.Alternators = nil, vesselAlternatorSettings{}
	if err := trainNormal(context.Background(), path, h.deps()); err != nil {
		t.Fatal(err)
	}
	got, _ := loadNormalModel(path)
	if got.Subsystems[normalSubsystemElectrical].judgedRegimes() != 2 {
		t.Fatalf("electrical = %+v", got.Subsystems[normalSubsystemElectrical])
	}
	if rec := got.Subsystems[normalSubsystemEngines]; rec.State != normalStateSkipped || rec.Problem != "No engines are set up" {
		t.Fatalf("engines = %+v", rec)
	}
}

// A subsystem that fails does not cost the other its run, and the first failure
// is still the run's.
func TestTrainNormal_AFailureInOneSubsystemDoesNotCostTheOtherItsTraining(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anomaly-normal.json")
	h := houseHarness(bothFeed(9))
	h.vessel.Engines = []vesselEngineSetting{{Instance: "port", Name: "Port"}}
	// The engines' read fails; the bank's does not.
	h.failPathPrefix = "propulsion.port.oil"
	err := trainNormal(context.Background(), path, h.deps())
	if err == nil || !strings.Contains(err.Error(), "read failed") {
		t.Fatalf("err = %v", err)
	}
	got, _ := loadNormalModel(path)
	if got.Subsystems[normalSubsystemElectrical].judgedRegimes() != 2 {
		t.Fatalf("the bank was trained: %+v", got.Subsystems[normalSubsystemElectrical])
	}
	if rec := got.Subsystems[normalSubsystemEngines]; rec.State != normalStateFailed || rec.Problem == "" {
		t.Fatalf("engines = %+v", rec)
	}
}

func TestTrainNormal_ABoatWithNothingSetUpSaysSo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anomaly-normal.json")
	err := trainNormal(context.Background(), path, newTrainHarness(nil).deps())
	if err == nil || !strings.Contains(err.Error(), "no engines are set up") {
		t.Fatalf("err = %v", err)
	}
}

// --- review fixes ------------------------------------------------------------

// A charging source that never moved while the model was trained is in no band,
// but the minute is still placed by it. A bank on the generator is not "with
// nothing charging".
func TestScoreHouse_AChargingSourceNoBandUsesStillPlacesTheMinute(t *testing.T) {
	model := houseHarness(houseFeed(61, []houseSpell{gatedHouse(daysAgo(10), 70, 0.7), gatedHouse(daysAgo(8), 70, 0.7)})).trainHouse(t, normalSubsystemModel{})
	if slices.Contains(model.Regimes["house/none/soc60"].Features, "inverterIn") {
		t.Fatalf("setup: the generator never ran in training: %+v", model.Regimes["house/none/soc60"].Features)
	}
	v := houseVessel()
	c := newScoreCase(normalModelFile{Subsystems: map[string]normalSubsystemModel{normalSubsystemElectrical: model}},
		houseFeed(62, []houseSpell{{Start: scoreRunStart, Minutes: 60, SOC: 0.7, Inverter: true}}))
	c.vessel = &v
	res := c.scoreHouse(24 * time.Hour)
	if !slices.Contains(c.paths, "electrical.inverters.inv.acin.power") {
		t.Fatalf("the readings the regime is placed by are always asked for: %v", c.paths)
	}
	if len(res.Stretches) != 0 || res.JudgedMinutes != 0 {
		t.Fatalf("a generator minute is not judged against the nothing-charging band: judged %d, %+v", res.JudgedMinutes, res.Stretches)
	}
	var thin *unusualNotJudged
	for i := range res.NotJudged {
		if res.NotJudged[i].Kind == "thin_band" {
			thin = &res.NotJudged[i]
		}
	}
	if thin == nil || !strings.Contains(thin.Label, "generator") {
		t.Fatalf("not judged = %+v", res.NotJudged)
	}
}

// The model also knows each reading's alternatives.
func TestHouseTraining_AnAlternativeReadingNamesItsGroupInTheVerdict(t *testing.T) {
	m := houseHarness(houseFeed(63, houseSpells())).trainHouse(t, normalSubsystemModel{})
	for _, v := range m.Subjects["house"].Features {
		if strings.HasPrefix(v.Key, "acLoad.") && v.Group != "acLoad" {
			t.Fatalf("%s has no group: %+v", v.Key, v)
		}
	}
}

// Adding, removing or renumbering a solar controller changes what the bank is
// offered, so the trainer must hear of it.
func TestWakeNormalIfInputsChanged_NoticesASolarControllerAddedOrRemoved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anomaly-normal.json")
	t.Setenv("ANOMALY_NORMAL_FILE", path)
	learned := houseVessel()
	if err := saveNormalModel(path, learnedModel(learned)); err != nil {
		t.Fatal(err)
	}
	drainNormalWake()
	wakeNormalIfInputsChanged(learned)
	if woke() {
		t.Fatal("the same boat wakes nothing")
	}
	for name, edit := range map[string]func(v *vesselView){
		"added":      func(v *vesselView) { v.Solar = append(v.Solar, vesselSolarSetting{Instance: "s2"}) },
		"removed":    func(v *vesselView) { v.Solar = nil },
		"renumbered": func(v *vesselView) { v.Solar = []vesselSolarSetting{{Instance: "s9", Name: "Roof"}} },
	} {
		v := houseVessel()
		edit(&v)
		drainNormalWake()
		wakeNormalIfInputsChanged(v)
		if !woke() {
			t.Errorf("a solar controller %s must wake the trainer", name)
		}
	}
}

// Each subsystem keeps its own outcome: the engines' result never carries the
// house bank's failure.
func TestTrainNormal_EachSubsystemKeepsItsOwnFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anomaly-normal.json")
	feed := mergeFeeds(synthFeed("port", 13, goodRuns()), houseFeed(9, houseSpells()))
	h := houseHarness(feed)
	h.vessel.Engines = []vesselEngineSetting{{Instance: "port", Name: "Port"}}
	h.failPathPrefix = "electrical."
	if err := trainNormal(context.Background(), path, h.deps()); err == nil {
		t.Fatal("the bank's failure is still the run's")
	}
	file, err := loadNormalModel(path)
	if err != nil {
		t.Fatal(err)
	}
	c := newScoreCase(file, steadyScoreFeed(64))
	v := houseVessel()
	v.Engines = []vesselEngineSetting{{Instance: "port", Name: "Port"}}
	c.vessel = &v
	engines := c.score(24 * time.Hour)
	if engines.TrainingProblem != "" {
		t.Fatalf("the engines trained fine: %q", engines.TrainingProblem)
	}
	house := c.scoreHouse(24 * time.Hour)
	if house.ProblemKind != unusualNoModel || house.Problem != "electrical read failed" {
		t.Fatalf("house = %q %q", house.ProblemKind, house.Problem)
	}
}

func TestHouseSubjects_SolarControllersAndTheGeneratorAreAmongTheBanksItemsAndAlarms(t *testing.T) {
	v := houseVessel()
	v.Solar = []vesselSolarSetting{{Instance: "s1", Name: "Roof", EquipmentID: "eq-solar"}}
	s := normalHouseSubjects(v)[0]
	if !slices.Contains(s.EquipmentIDs, "eq-solar") {
		t.Fatalf("a repair on a solar controller counts: %v", s.EquipmentIDs)
	}
	for _, path := range []string{"notifications.electrical.solar.s1.panelPower", "electrical.generator.g.state"} {
		if !normalAlarmBelongs(s, path) {
			t.Errorf("%s takes the bank's minutes out", path)
		}
	}
	if normalAlarmBelongs(s, "electrical.solar.s2.panelPower") {
		t.Error("another controller is not this boat's")
	}
}

func TestResolveVesselView_SolarSettingsKnowTheirItems(t *testing.T) {
	v := resolveVesselView(vesselRoles{}, []sourceItem{{ID: "eq-solar", Name: "Roof", Group: sourceSolar, Instance: "s1", Source: "electrical.solar.s1"}})
	if len(v.Solar) != 1 || v.Solar[0].EquipmentID != "eq-solar" {
		t.Fatalf("solar = %+v", v.Solar)
	}
}

// The minute's sources are worked out once, when the minutes are joined, and the
// gate, the runs and the regime read that.
func TestHouseSources_AreReadFromTheMinutesOwnClassification(t *testing.T) {
	sub := normalElectricalSubsystem()
	if sub.Annotate == nil {
		t.Fatal("the subsystem classifies its minutes once")
	}
	s := sub.Subjects(houseVessel())[0]
	at := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	ms := houseMinutes(at, 12, func(int) map[string]float64 { return map[string]float64{"soc": 0.7, "shoreIn": 12} })
	sub.Annotate(ms)
	for _, m := range ms {
		if !m.Classified || m.Class != houseShore {
			t.Fatalf("minute = %+v", m)
		}
	}
	// A classification that disagrees with the values is what the readers go by.
	ms[0].Class = houseSolar
	if _, label, _ := sub.Regime(s, ms[0]); label != "on solar, 60 to 80 % charge" {
		t.Fatalf("regime read the values again: %q", label)
	}
	if got := sub.Gate(ms); got[9] || got[10] != true {
		t.Fatalf("the gate read the values again: %v", got)
	}
	if runs := sub.Run(ms); runs[0] == runs[1] {
		t.Fatalf("the runs read the values again: %v", runs)
	}
}

// learnedModel is a saved model that holds every subject of the vessel, as its
// training would have left it.
func learnedModel(vessel vesselView) normalModelFile {
	file := normalModelFile{Subsystems: map[string]normalSubsystemModel{}}
	for _, sub := range normalSubsystems() {
		model := normalSubsystemModel{Subjects: map[string]normalSubjectModel{}}
		for _, s := range sub.Subjects(vessel) {
			model.Subjects[s.ID] = normalSubjectModel{Name: s.Name, EquipmentIDs: s.EquipmentIDs, Features: learnedVerdicts(s.Features)}
		}
		model.State, model.AttemptedAt, model.Inputs = normalStateTrained, trainNow, normalInputsOf(sub.Subjects(vessel))
		if len(model.Subjects) == 0 {
			model = normalSubsystemModel{State: normalStateSkipped, AttemptedAt: trainNow, Problem: sub.NoSubjects}
		}
		file.Subsystems[sub.Name] = model
	}
	return file
}
