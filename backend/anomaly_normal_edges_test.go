package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// Edges the learned normal must hold (ADR 0276).

// A bank on shore power sits at full charge for nearly every minute. Charge places
// the minute in a band, so it may sit on one value.
func TestSelectFeatures_AConditioningReadingMaySitOnOneValue(t *testing.T) {
	gated := selectMinutes(300)
	ev := featureEvidenceFor("soc", "electrical.batteries.1.capacity.stateOfCharge", gated, func(int) (float64, bool) { return 1, true })
	ev.Feature.Required, ev.Feature.AllowConstant = true, true
	v := verdictOf(t, selectFeatures([]featureEvidence{ev}, gated, gated, nil, noLimits), "soc")
	if !v.Used {
		t.Fatalf("verdict = %+v", v)
	}
	soc := normalHouseSubjects(houseVessel())[0].Features[2]
	if soc.Key != "soc" || !soc.AllowConstant {
		t.Fatalf("the house bank's charge is allowed to sit still: %+v", soc)
	}
}

// An alternator that reads zero whenever its engine is off is judged for constancy
// in the bands where the engine runs, not over the whole window.
func TestSelectFeatures_APerRegimeReadingIsNotJudgedConstantOverTheWholeWindow(t *testing.T) {
	gated := selectMinutes(1000)
	ev := featureEvidenceFor("alternator.p", "electrical.alternator.p.current", gated, func(i int) (float64, bool) {
		if i < 5 {
			return 40 + float64(i), true
		}
		return 0, true
	})
	ev.Feature.PerRegime = true
	v := verdictOf(t, selectFeatures([]featureEvidence{ev}, gated, gated, nil, noLimits), "alternator.p")
	if !v.Used {
		t.Fatalf("verdict = %+v", v)
	}
}

// With a bank chosen since, a saved "skipped" is not what scoring or Settings say.
func TestScoreHouse_ASkippedRecordIsNotReportedOnceABankIsChosen(t *testing.T) {
	file := normalModelFile{Subsystems: map[string]normalSubsystemModel{
		normalSubsystemElectrical: {State: normalStateSkipped, Problem: "No house bank is chosen", AttemptedAt: trainNow},
	}}
	v := houseVessel()
	c := newScoreCase(file, nil)
	c.vessel = &v
	res := c.scoreHouse(24 * time.Hour)
	if res.ProblemKind != unusualNeverTrained || res.Problem != normalNoModelYet {
		t.Fatalf("a bank is chosen and nothing is learned yet: %q %q", res.ProblemKind, res.Problem)
	}
	none := newScoreCase(file, nil)
	none.vessel = &vesselView{}
	if res = none.scoreHouse(24 * time.Hour); res.Problem != "no house bank is chosen" {
		t.Fatalf("no bank: %q", res.Problem)
	}
	st := buildNormalStatus(file, nil, nil, nil, &v, time.UTC)
	if st.Note == "" && len(st.Subsystems) == 0 {
		t.Fatalf("status = %+v", st)
	}
	var note string
	for _, g := range st.Subsystems {
		if g.ID == normalSubsystemElectrical {
			note = g.Note
		}
	}
	if note != normalNoModelYet {
		t.Fatalf("Settings with a bank chosen: %q", note)
	}
	st = buildNormalStatus(file, nil, nil, nil, &vesselView{}, time.UTC)
	for _, g := range st.Subsystems {
		if g.ID == normalSubsystemElectrical && g.Note != "No house bank is chosen." {
			t.Fatalf("Settings with no bank: %q", g.Note)
		}
	}
}

func rowsAt(path string, n int, from time.Time) []normalMinuteRow {
	var out []normalMinuteRow
	for i := 0; i < n; i++ {
		out = append(out, normalMinuteRow{Path: path, Source: "x", At: from.Add(time.Duration(i) * time.Minute), Value: 1})
	}
	return out
}

// Of several paths for one reading, the one with the most rows is used, and on a
// tie the one that reported last.
func TestFirstOfEachGroup_TakesTheAlternativeWithTheMostRows(t *testing.T) {
	features := []normalFeature{{Key: "a", Path: "p.a", Group: "g"}, {Key: "b", Path: "p.b", Group: "g"}, {Key: "c", Path: "p.c", Group: "g"}}
	t0 := selectBase
	rows := map[string][]normalMinuteRow{"p.a": rowsAt("p.a", 3, t0), "p.b": rowsAt("p.b", 50, t0), "p.c": rowsAt("p.c", 50, t0.Add(time.Hour))}
	got := firstOfEachGroup(features, rows)
	if len(got["p.a"]) != 0 || len(got["p.b"]) != 0 || len(got["p.c"]) != 50 {
		t.Fatalf("kept: a %d b %d c %d", len(got["p.a"]), len(got["p.b"]), len(got["p.c"]))
	}
}

func TestMergeUnusual_EveryFailureIsReported(t *testing.T) {
	subs := []normalSubsystem{normalEnginesSubsystem(), normalElectricalSubsystem()}
	results := []unusualResult{
		{Model: &unusualModelInfo{Bands: map[string]int{}}, TrainingProblem: "InfluxDB could not be read"},
		{Model: &unusualModelInfo{Bands: map[string]int{}}, TrainingProblem: "the alarm log is not open"},
	}
	got := mergeUnusual(unusualResult{}, subs, results)
	if !strings.Contains(got.TrainingProblem, "Engines: InfluxDB could not be read") || !strings.Contains(got.TrainingProblem, "House bank: the alarm log is not open") {
		t.Fatalf("training problem = %q", got.TrainingProblem)
	}
	one := mergeUnusual(unusualResult{}, subs[:1], results[:1])
	if one.TrainingProblem != "InfluxDB could not be read" {
		t.Fatalf("one subsystem needs no prefix: %q", one.TrainingProblem)
	}
}

// Solar turns on at 100 W and stays on until it falls under 50 W, so cloud at the
// edge does not split a spell.
func TestHouseAnnotate_SolarHasHysteresis(t *testing.T) {
	at := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	watts := []float64{120, 80, 60, 40, 80, 100}
	want := []bool{true, true, true, false, false, true}
	ms := houseMinutes(at, len(watts), func(i int) map[string]float64 { return map[string]float64{"soc": 0.7, "solar.s1": watts[i]} })
	normalHouseAnnotate(ms)
	for i, m := range ms {
		if got := m.Class&houseSolar != 0; got != want[i] {
			t.Errorf("%v W: solar %v, want %v", watts[i], got, want[i])
		}
	}
	// A minute with no solar reading is solar off.
	gap := houseMinutes(at, 2, func(i int) map[string]float64 {
		if i == 0 {
			return map[string]float64{"soc": 0.7, "solar.s1": 300}
		}
		return map[string]float64{"soc": 0.7}
	})
	normalHouseAnnotate(gap)
	if gap[0].Class&houseSolar == 0 || gap[1].Class&houseSolar != 0 {
		t.Errorf("classes = %v %v", gap[0].Class, gap[1].Class)
	}
}

// The house bank and the engines both read the engine's revolutions; the run reads
// them once.
func TestTrainNormal_ASharedReadingIsReadOnceForTheWholeRun(t *testing.T) {
	h := houseHarness(bothFeed(111))
	h.vessel.Engines = []vesselEngineSetting{{Instance: "port", Name: "Port"}}
	if err := trainNormal(context.Background(), t.TempDir()+"/anomaly-normal.json", h.deps()); err != nil {
		t.Fatal(err)
	}
	reads := 0
	for _, paths := range h.requested {
		if slices.Contains(paths, "propulsion.port.revolutions") {
			reads++
		}
	}
	if reads != 1 {
		t.Fatalf("the engine's revolutions were read %d times", reads)
	}
}

// --- the sixth review -------------------------------------------------------------

func twoAlternatorVessel() vesselView {
	v := houseVessel()
	v.Alternators.StarboardInstance = "q"
	v.RoleItems["starboard_alternator"] = "eq-alt-s"
	return v
}

// The alternators are one reading, their sum. Motoring on one engine is judged.
func TestHouseAlternators_TheirSumIsOneJudgedReadingAndOneQuietAlternatorIsNotAFault(t *testing.T) {
	// The starboard alternator reports in only some of the engines minutes (one engine
	// motors at times), so it is not required of every minute.
	spells := partlyBothAlternators()
	v := twoAlternatorVessel()
	h := newTrainHarness(houseFeed(121, spells))
	h.vessel = &v
	model := h.trainHouse(t, normalSubsystemModel{})
	engines := regimeNamed(t, model, "house/engines/soc60")
	if !engines.Judged || !slices.Contains(engines.Features, "alternator") || slices.Contains(engines.Features, "alternator.p") || slices.Contains(engines.Features, "alternator.q") {
		t.Fatalf("engines features = %v", engines.Features)
	}
	c := newScoreCase(normalModelFile{Subsystems: map[string]normalSubsystemModel{normalSubsystemElectrical: model}},
		houseFeed(122, []houseSpell{{Start: scoreRunStart, Minutes: 60, SOC: 0.7, Alt: true}}))
	c.vessel = &v
	res := c.scoreHouse(24 * time.Hour)
	for _, n := range res.NotJudged {
		if n.Kind == "sender_silent" || n.Kind == "missing_reading" {
			t.Fatalf("one alternator on one engine is not a quiet sender: %+v", res.NotJudged)
		}
	}
	if res.JudgedMinutes == 0 {
		t.Fatalf("nothing judged: %+v", res.NotJudged)
	}
	// With none reporting, the reading is missing and the minute is not judged.
	none := newScoreCase(normalModelFile{Subsystems: map[string]normalSubsystemModel{normalSubsystemElectrical: model}},
		houseFeed(123, []houseSpell{{Start: scoreRunStart, Minutes: 60, SOC: 0.7, EngineOn: true}}))
	none.vessel = &v
	if res = none.scoreHouse(24 * time.Hour); res.JudgedMinutes != 0 {
		t.Fatalf("no alternator reports: %d judged", res.JudgedMinutes)
	}
}

// Any alarm under the bank, a charging source, solar or the generator takes the
// bank's minutes out, a BMS fault included.
func TestHouseTraining_AnyAlarmUnderTheBanksRootsTakesMinutesOut(t *testing.T) {
	cases := []struct {
		path string
		out  bool
	}{
		// With no rule of the operator's behind it, an alarm at voltage or charge is a
		// fault (the next test covers a rule that goes below a limit).
		{"notifications.electrical.batteries.1.voltage", true},
		{"notifications.electrical.batteries.1.capacity.stateOfCharge", true},
		{"notifications.electrical.batteries.1.current", true},
		{"notifications.electrical.batteries.1.highCellVoltage", true},
		{"notifications.electrical.chargers.sc.state", true},
		{"notifications.electrical.solar.s1.error", true},
		{"notifications.electrical.generator.g.state", true},
		{"notifications.electrical.alternator.p.temperature", true},
		{"notifications.electrical.inverters.inv.state", true},
		{"helmcentral.anomaly.battery.fullBankCharging", true},
		{"notifications.electrical.batteries.2.voltage", false}, // not this bank
	}
	for _, c := range cases {
		h := houseHarness(houseFeed(124, houseSpells()))
		cleared := daysAgo(10).Add(45 * time.Minute)
		h.alarms = []alarmLogEntry{{ID: "a1", Path: c.path, RaisedAt: daysAgo(10).Add(30 * time.Minute), ClearedAt: &cleared}}
		m := h.trainHouse(t, normalSubsystemModel{})
		if got := len(m.Excluded) > 0; got != c.out {
			t.Errorf("%s: minutes taken out = %v, want %v", c.path, got, c.out)
		}
	}
}

// Solar stays on through the 50 to 100 W band for a hold as long as the scoring
// lead-in, and a gap resets it, so training and scoring agree.
func TestHouseAnnotate_SolarHoldIsBoundedAndAGapResetsIt(t *testing.T) {
	if normalHouseLeadIn < normalSolarHold+normalHouseSteadyMinutes*time.Minute {
		t.Fatalf("the lead-in %v must hold the solar hold and the steady window", normalHouseLeadIn)
	}
	at := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	ms := houseMinutes(at, 30, func(i int) map[string]float64 {
		if i == 0 {
			return map[string]float64{"soc": 0.7, "solar.s1": 120}
		}
		return map[string]float64{"soc": 0.7, "solar.s1": 70}
	})
	normalHouseAnnotate(ms)
	for i, m := range ms {
		want := i <= 15
		if got := m.Class&houseSolar != 0; got != want {
			t.Fatalf("minute %d: solar %v, want %v", i, got, want)
		}
	}
	// 120 W, then ten minutes with no data, then 70 W: the gap ended it.
	gap := []normalMinute{
		{At: at, Values: map[string]float64{"soc": 0.7, "solar.s1": 120}},
		{At: at.Add(11 * time.Minute), Values: map[string]float64{"soc": 0.7, "solar.s1": 70}},
	}
	normalHouseAnnotate(gap)
	if gap[1].Class&houseSolar != 0 {
		t.Fatalf("a gap resets the hold: %v", gap[1].Class)
	}
}

func TestMergeUnusual_WhenNothingCanBeJudgedEverySubsystemSaysWhy(t *testing.T) {
	subs := []normalSubsystem{normalEnginesSubsystem(), normalElectricalSubsystem()}
	results := []unusualResult{
		{ProblemKind: unusualNoModel, Problem: "InfluxDB could not be read"},
		{ProblemKind: unusualNoModel, Problem: "the alarm log is not open"},
	}
	got := mergeUnusual(unusualResult{}, subs, results)
	if got.ProblemKind != unusualNoModel || !strings.Contains(got.Problem, "Engines: InfluxDB could not be read") || !strings.Contains(got.Problem, "House bank: the alarm log is not open") {
		t.Fatalf("problem = %q", got.Problem)
	}
	same := mergeUnusual(unusualResult{}, subs, []unusualResult{{ProblemKind: unusualNoInflux, Problem: "InfluxDB is not set up"}, {ProblemKind: unusualNoInflux, Problem: "InfluxDB is not set up"}})
	if same.Problem != "InfluxDB is not set up" {
		t.Fatalf("one reason said twice is said once: %q", same.Problem)
	}
}

func TestBuildNormalStatus_ASubsystemWithSubjectsAndNoRecordHasAGroup(t *testing.T) {
	file := learnedModel(vesselView{Engines: houseVessel().Engines})
	delete(file.Subsystems, normalSubsystemElectrical)
	v := houseVessel()
	st := buildNormalStatus(file, nil, nil, nil, &v, time.UTC)
	var note string
	var found bool
	for _, g := range st.Subsystems {
		if g.ID == normalSubsystemElectrical {
			found, note = true, g.Note
		}
	}
	if !found || note != normalNoModelYet {
		t.Fatalf("status = %+v", st.Subsystems)
	}
}

// Ignoring a path matters only if the bank judges it on this boat.
func TestHousePathMatters_OnlyTheJudgedReadingsOfThisBoat(t *testing.T) {
	v := houseVessel()
	for path, want := range map[string]bool{
		"electrical.batteries.1.voltage":                true,
		"electrical.batteries.1.capacity.stateOfCharge": true,
		"electrical.batteries.2.voltage":                false, // a starter battery
		"electrical.chargers.sc.current":                true,
		"electrical.inverters.inv.ac.output.power":      true,
		"electrical.alternator.p.current":               true, // a part of the summed reading, and a switch
		"electrical.solar.s1.panelPower":                false,
		"electrical.venus.totalPanelPower":              false,
		"electrical.chargers.sc.acin.1.current":         false,
		"electrical.inverters.inv.acin.power":           false,
	} {
		if got := normalHousePathMatters(v, path); got != want {
			t.Errorf("%s matters = %v, want %v", path, got, want)
		}
	}
}

// --- the seventh review -----------------------------------------------------------

func altSpellsBoth(first float64) []houseSpell {
	spells := houseSpells()
	for i := 0; i < 2; i++ {
		s := gatedHouse(daysAgo(first-float64(i)), 70, 0.7)
		s.Alt, s.Alt2 = true, true
		spells = append(spells, s)
	}
	return spells
}

// An alarm on an alternator's current is an alarm on the summed reading.
func TestHouseTraining_AnAlarmOnAnAlternatorTakesTheBanksMinutesOut(t *testing.T) {
	v := twoAlternatorVessel()
	h := newTrainHarness(houseFeed(131, altSpellsBoth(13)))
	h.vessel = &v
	cleared := daysAgo(13).Add(45 * time.Minute)
	h.alarms = []alarmLogEntry{{ID: "a1", Path: "notifications.electrical.alternator.q.current", RaisedAt: daysAgo(13).Add(30 * time.Minute), ClearedAt: &cleared}}
	m := h.trainHouse(t, normalSubsystemModel{})
	if len(m.Excluded) != 1 || m.Excluded[0].Reason != normalReasonAlarm {
		t.Fatalf("excluded = %+v", m.Excluded)
	}
}

// The alternators are the parts of one judged reading. They follow the ignored
// list, Settings shows one row for them, and a partial sum is never judged.
func TestHouseAlternators_ArePartsOfOneSwitchableReading(t *testing.T) {
	v := twoAlternatorVessel()
	subject := normalHouseSubjects(v)[0]
	byKey := map[string]normalFeature{}
	for _, f := range subject.Features {
		byKey[f.Key] = f
	}
	if f := byKey["alternator.p"]; f.Placing || !f.Component {
		t.Fatalf("a part is read but not placing: %+v", f)
	}
	if !normalHousePathMatters(v, "electrical.alternator.q.current") {
		t.Error("an alternator's current is a switch")
	}

	h := newTrainHarness(houseFeed(132, altSpellsBoth(13)))
	h.vessel = &v
	h.ignored = map[string]bool{"electrical.alternator.q.current": true}
	m := h.trainHouse(t, normalSubsystemModel{})
	for _, paths := range h.requested {
		if slices.Contains(paths, "electrical.alternator.q.current") {
			t.Fatalf("a switched-off alternator is not read: %v", paths)
		}
	}
	var sum normalFeatureVerdict
	for _, f := range m.Subjects["house"].Features {
		if f.Key == "alternator" {
			sum = f
		}
	}
	if sum.Used || sum.Reason != "switched off" {
		t.Fatalf("a partial sum is not judged: %+v", sum)
	}
	if r := regimeNamed(t, m, "house/engines/soc60"); slices.Contains(r.Features, "alternator") {
		t.Fatalf("features = %v", r.Features)
	}

	// Settings shows one row, and its switch is both paths.
	h2 := newTrainHarness(houseFeed(133, altSpellsBoth(13)))
	h2.vessel = &v
	trained := h2.trainHouse(t, normalSubsystemModel{})
	st := buildNormalStatus(normalModelFile{Subsystems: map[string]normalSubsystemModel{normalSubsystemElectrical: trained}}, nil, nil, map[string]bool{"electrical.alternator.p.current": true}, nil, time.UTC)
	var rows []normalStatusReading
	for _, r := range st.Subsystems[0].Subjects[0].Readings {
		if strings.Contains(r.Name, "lternator") {
			rows = append(rows, r)
		}
	}
	if len(rows) != 1 || rows[0].Name != "Alternator current" || !slices.Equal(rows[0].Paths, []string{"electrical.alternator.p.current", "electrical.alternator.q.current"}) || rows[0].On {
		t.Fatalf("rows = %+v", rows)
	}
}

// Skipping keeps the earlier model: a brief glitch in a role does not lose history.
func TestTrainNormal_ASkippedSubsystemKeepsItsEarlierModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anomaly-normal.json")
	h := houseHarness(bothFeed(134))
	if err := trainNormal(context.Background(), path, h.deps()); err != nil {
		t.Fatal(err)
	}
	first, _ := loadNormalModel(path)
	h.vessel.HouseBank = nil
	h.now = trainNow.Add(24 * time.Hour)
	if err := trainNormal(context.Background(), path, h.deps()); err != nil {
		t.Fatal(err)
	}
	second, _ := loadNormalModel(path)
	rec := second.Subsystems[normalSubsystemElectrical]
	if rec.State != normalStateSkipped || rec.Problem != "No house bank is chosen" || !rec.hasModel() || rec.judgedRegimes() != first.Subsystems[normalSubsystemElectrical].judgedRegimes() {
		t.Fatalf("record = %+v", rec)
	}
}

// When nothing can be judged, no subsystem's reason is hidden behind another's.
func TestMergeUnusual_AMixedKindShowsEverySubsystemsReason(t *testing.T) {
	subs := []normalSubsystem{normalEnginesSubsystem(), normalElectricalSubsystem()}
	got := mergeUnusual(unusualResult{}, subs, []unusualResult{
		{ProblemKind: unusualNeverTrained, Problem: normalNoModelYet},
		{ProblemKind: unusualNoModel, Problem: "electrical read failed"},
	})
	note := unusualProblemNote(got.ProblemKind, got.Problem)
	if !strings.Contains(note, "electrical read failed") || !strings.Contains(note, "Engines") {
		t.Fatalf("kind %q note %q", got.ProblemKind, note)
	}
	u := buildWatchUnusual(got, "metric", time.UTC, trainNow)
	if u == nil || !strings.Contains(u.Note, "electrical read failed") {
		t.Fatalf("report = %+v", u)
	}
}

// Dropping a still column is for readings fitted where they report. The engines
// are fitted as they were.
func TestDropStillColumns_OnlyDropsAPerRegimeReading(t *testing.T) {
	rows := [][]float64{{1, 5, 9}, {2, 5, 9}, {3, 5, 9}}
	keys, kept := dropStillColumns([]string{"a", "b", "c"}, rows, map[string]bool{"c": true})
	if !slices.Equal(keys, []string{"a", "b"}) || len(kept[0]) != 2 {
		t.Fatalf("keys = %v rows = %v", keys, kept)
	}
}

// Both subsystems score the engine's revolutions from one read.
func TestScoreAll_ASharedReadingIsReadOnce(t *testing.T) {
	feed := mergeFeeds(steadyScoreFeed(141), sagEnginesFeed(141))
	c := newScoreCase(bothModel(t), feed)
	c.vessel = bothVessel()
	c.scoreAll(unusualSubject{})
	reads := 0
	for _, paths := range c.reads {
		if slices.Contains(paths, "propulsion.port.revolutions") {
			reads++
		}
	}
	if reads != 1 {
		t.Fatalf("the engine's revolutions were read %d times", reads)
	}
}

// A subsystem whose minutes were not classified fails the run with an error, not
// a panic.
func TestTrainSubsystem_MinutesThatWereNotAnnotatedAreAnError(t *testing.T) {
	sub := normalElectricalSubsystem()
	sub.Annotate = func([]normalMinute) {}
	h := houseHarness(houseFeed(142, houseSpells()))
	_, err := trainSubsystem(context.Background(), sub, h.deps(), normalSubsystemModel{})
	if err == nil || !strings.Contains(err.Error(), "annotated") {
		t.Fatalf("err = %v", err)
	}
}

// --- the eighth review ------------------------------------------------------------

// A subsystem cut short by the clock is worded as the budget's sentence, not a raw
// error, even when the other subsystem was judged.
func TestScoreAll_ASubsystemCutShortByTheBudgetSaysSo(t *testing.T) {
	feed := mergeFeeds(steadyScoreFeed(151), sagEnginesFeed(151))
	c := newScoreCase(bothModel(t), feed)
	c.vessel = bothVessel()
	deps := c.deps()
	inner := deps.Minutes
	deps.Minutes = func(ctx context.Context, src normalSource, paths []string, from, to time.Time) (map[string][]normalMinuteRow, error) {
		for _, p := range paths {
			if strings.HasPrefix(p, "electrical.") {
				return nil, context.DeadlineExceeded
			}
		}
		return inner(ctx, src, paths, from, to)
	}
	ctx, cancel := context.WithTimeout(context.Background(), -time.Second)
	defer cancel()
	ctx = withUnusualBudget(ctx, 90*time.Second)
	res := scoreUnusualAll(ctx, deps, trainNow.Add(-24*time.Hour), trainNow, unusualSubject{}, nil)
	var note string
	for _, n := range res.NotJudged {
		if n.Kind == "subject_problem" {
			note = n.Reason
		}
	}
	if note != "it took longer than 90 seconds" {
		t.Fatalf("note = %q, not judged = %+v", note, res.NotJudged)
	}
}

func keptHouseRecord(t *testing.T) normalModelFile {
	t.Helper()
	rec := houseModel(t)
	rec.State, rec.Problem, rec.AttemptedAt = normalStateSkipped, "No house bank is chosen", trainNow
	return normalModelFile{Subsystems: map[string]normalSubsystemModel{normalSubsystemElectrical: rec}}
}

// A kept model is used only while it is the model of what is set up now.
func TestHouse_AKeptModelIsUsedOnlyWhileItMatchesWhatIsSetUp(t *testing.T) {
	file := keptHouseRecord(t)
	otherBank := houseVessel()
	otherBank.HouseBank = &vesselHouseBankSetting{Path: "electrical.batteries.2", EquipmentID: "eq-bank2"}
	for name, c := range map[string]struct {
		vessel vesselView
		note   string
	}{
		"no bank":     {vesselView{}, "No house bank is chosen."},
		"another one": {otherBank, normalNoModelYet},
	} {
		sc := newScoreCase(file, nil)
		sc.vessel = &c.vessel
		if res := sc.scoreHouse(24 * time.Hour); res.JudgedMinutes != 0 || res.ProblemKind == "" {
			t.Errorf("%s: scored against a model of something else: %+v", name, res)
		}
		st := buildNormalStatus(file, nil, nil, nil, &c.vessel, time.UTC)
		var group *normalStatusSubsystem
		for i := range st.Subsystems {
			if st.Subsystems[i].ID == normalSubsystemElectrical {
				group = &st.Subsystems[i]
			}
		}
		if group == nil || group.Note != c.note || len(group.Subjects) != 0 || st.Summary != "" {
			t.Errorf("%s: status = %+v summary %q", name, group, st.Summary)
		}
	}
	// The same bank: the model is used.
	same := houseVessel()
	sc := newScoreCase(file, sagFeed(152))
	sc.vessel = &same
	if res := sc.scoreHouse(24 * time.Hour); res.ProblemKind != "" || res.JudgedMinutes == 0 {
		t.Fatalf("the same bank: %+v", res)
	}
}

// One switch covers every path the inverter's load is published under.
func TestHouseAcLoad_OneSwitchCoversEveryAlternativePath(t *testing.T) {
	v := houseVessel()
	h := newTrainHarness(houseFeed(153, houseSpells()))
	h.vessel = &v
	h.ignored = map[string]bool{"electrical.inverters.inv.acout.power": true}
	m := h.trainHouse(t, normalSubsystemModel{})
	for _, paths := range h.requested {
		for _, p := range paths {
			if strings.HasPrefix(p, "electrical.inverters.inv.ac") && p != "electrical.inverters.inv.acin.power" {
				t.Fatalf("a switched-off load is read from %s", p)
			}
		}
	}
	for _, f := range m.Subjects["house"].Features {
		if strings.HasPrefix(f.Key, "acLoad.") && f.Used {
			t.Fatalf("the load is off: %+v", f)
		}
	}
	st := buildNormalStatus(normalModelFile{Subsystems: map[string]normalSubsystemModel{normalSubsystemElectrical: m}}, nil, nil, h.ignored, nil, time.UTC)
	var rows []normalStatusReading
	for _, r := range st.Subsystems[0].Subjects[0].Readings {
		if r.Name == "Inverter AC load" {
			rows = append(rows, r)
		}
	}
	if len(rows) != 1 || len(rows[0].Paths) != 3 || rows[0].On {
		t.Fatalf("rows = %+v", rows)
	}
}

// The alternators' total is judged on whichever alternators report that minute. A
// dead alternator sender while motoring shows as the total running low, never as a
// quiet sender. With none reporting the total is missing and the minute is not judged.
func TestHouseAlternators_TheTotalIsJudgedOnWhicheverReport(t *testing.T) {
	v := twoAlternatorVessel()
	h := newTrainHarness(houseFeed(154, altSpellsBoth(13)))
	h.vessel = &v
	model := h.trainHouse(t, normalSubsystemModel{})
	score := func(spell houseSpell) unusualResult {
		spell.Start, spell.Minutes, spell.SOC = scoreRunStart, 60, 0.7
		c := newScoreCase(normalModelFile{Subsystems: map[string]normalSubsystemModel{normalSubsystemElectrical: model}}, houseFeed(155, []houseSpell{spell}))
		c.vessel = &v
		return c.scoreHouse(24 * time.Hour)
	}
	one := score(houseSpell{Alt: true})
	for _, n := range one.NotJudged {
		if n.Kind == "sender_silent" || n.Kind == "missing_reading" {
			t.Fatalf("a missing alternator is not a quiet sender: %+v", one.NotJudged)
		}
	}
	if one.JudgedMinutes == 0 || len(one.Stretches) != 1 || one.Stretches[0].Top.Direction != "low" ||
		(one.Stretches[0].Top.Name != "Alternator current" && one.Stretches[0].Top.Name != "House bank current") {
		t.Fatalf("judged %d, stretches %+v", one.JudgedMinutes, one.Stretches)
	}
	none := score(houseSpell{EngineOn: true})
	if none.JudgedMinutes != 0 {
		t.Fatalf("no alternator reports: %d judged", none.JudgedMinutes)
	}
	for _, n := range none.NotJudged {
		if n.Kind == "sender_silent" {
			t.Fatalf("the sum is never a silent sender: %+v", none.NotJudged)
		}
	}
}

// The first minutes of a scored window are gated as training gates them: the
// lead-in holds the solar hold and the steady window.
func TestScoreHouse_TheFirstMinutesAreGatedAsTrainingGatesThem(t *testing.T) {
	at := scoreRunStart
	full := houseMinutes(at, 60, func(i int) map[string]float64 {
		if i == 0 {
			return map[string]float64{"soc": 0.7, "solar.s1": 120}
		}
		return map[string]float64{"soc": 0.7, "solar.s1": 70}
	})
	normalHouseAnnotate(full)
	gate := normalHouseGate(full)
	want := 0
	for i := 20; i < 60; i++ {
		if !gate[i] {
			want++
		}
	}
	feed := houseFeed(156, []houseSpell{{Start: at, Minutes: 60, SOC: 0.7, SolarW: 70}})
	feed["electrical.solar.s1.panelPower"][0].Value = 120
	for i := range feed["electrical.solar.s1.panelPower"][1:] {
		feed["electrical.solar.s1.panelPower"][i+1].Value = 70
	}
	c := houseScoreCase(t, feed)
	res := c.scoreHouse(6*time.Hour - 20*time.Minute)
	got := 0
	for _, n := range res.NotJudged {
		if n.Kind == "not_steady" {
			got = n.Minutes
		}
	}
	if got != want || want == 0 {
		t.Fatalf("scoring left %d minutes unsteady, training's gate %d", got, want)
	}
}

// A subsystem that only has never been trained is worded as that, not as an error.
func TestMergeUnusual_EachSubsystemIsWordedInItsOwnKindsSentence(t *testing.T) {
	subs := []normalSubsystem{normalEnginesSubsystem(), normalElectricalSubsystem()}
	got := mergeUnusual(unusualResult{}, subs, []unusualResult{
		{ProblemKind: unusualNeverTrained, Problem: normalNoModelYet},
		{ProblemKind: unusualNoModel, Problem: "electrical read failed"},
	})
	note := unusualProblemNote(got.ProblemKind, got.Problem)
	if strings.Contains(note, "could not be checked") || !strings.Contains(note, "Engines: No learned normal yet.") || !strings.Contains(note, "House bank: No learned normal: electrical read failed.") {
		t.Fatalf("note = %q", note)
	}
}

// --- the ninth review -------------------------------------------------------------

// What the verdicts say follows the subsystem's words: the bank is not "running".
func TestSelectFeatures_TheVerdictsUseTheSubsystemsWords(t *testing.T) {
	gated := selectMinutes(300)
	still := featureEvidenceFor("current", "electrical.batteries.1.current", gated, func(int) (float64, bool) { return 5, true })
	sparse := featureEvidenceFor("x", "electrical.x", gated, func(i int) (float64, bool) { return 1 + float64(i%7), i%2 == 0 })
	for i, ev := range []featureEvidence{still, sparse} {
		ev.Wording = normalHouseWording
		v := selectFeatures([]featureEvidence{ev}, gated, gated, nil, noLimits)[0]
		if v.Used || strings.Contains(v.Reason, "running") || v.Reason == "" {
			t.Errorf("case %d: reason = %q", i, v.Reason)
		}
	}
}

// An error with the budget still running is a real diagnostic and is kept.
func TestScoreAll_ARealErrorIsKeptWhileTheBudgetRuns(t *testing.T) {
	feed := mergeFeeds(steadyScoreFeed(161), sagEnginesFeed(161))
	c := newScoreCase(bothModel(t), feed)
	c.vessel = bothVessel()
	deps := c.deps()
	inner := deps.Minutes
	deps.Minutes = func(ctx context.Context, src normalSource, paths []string, from, to time.Time) (map[string][]normalMinuteRow, error) {
		for _, p := range paths {
			if strings.HasPrefix(p, "electrical.") {
				return nil, errors.New("bucket not found")
			}
		}
		return inner(ctx, src, paths, from, to)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	res := scoreUnusualAll(withUnusualBudget(ctx, 90*time.Second), deps, trainNow.Add(-24*time.Hour), trainNow, unusualSubject{}, nil)
	var note string
	for _, n := range res.NotJudged {
		if n.Kind == "subject_problem" {
			note = n.Reason
		}
	}
	if !strings.Contains(note, "bucket not found") {
		t.Fatalf("note = %q", note)
	}
}

// The solar fallback is the measurement the rest of Helmcentral reads.
func TestHouseSubjects_TheSolarFallbackIsTheConfiguredMeasurement(t *testing.T) {
	t.Setenv("INFLUX_SOLAR_MEASUREMENT", "electrical.solar.total.power")
	v := vesselView{HouseBank: &vesselHouseBankSetting{Path: "electrical.batteries.1", EquipmentID: "eq"}, RoleItems: map[string]string{"house_bank": "eq"}}
	for _, f := range normalHouseSubjects(v)[0].Features {
		if f.Key == "solar" && f.Path != "electrical.solar.total.power" {
			t.Fatalf("path = %q", f.Path)
		}
	}
}

// Switching a reading with several parts is one request that changes them all or
// none.
func TestIgnoredSensors_ABatchChangesEveryPathOrNone(t *testing.T) {
	withTempIgnoreStore(t)
	put := func(body string) int {
		e := echo.New()
		req := httptest.NewRequest(http.MethodPut, "/api/alarms/ignored-sensors", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		_ = setIgnoredSensorsHandler(e.NewContext(req, rec))
		return rec.Code
	}
	if code := put(`{"identifiers":["a.one","a.two"],"ignored":true}`); code != http.StatusOK {
		t.Fatalf("code %d", code)
	}
	if got := listIgnoredSensors(); !slices.Equal(got, []string{"a.one", "a.two"}) {
		t.Fatalf("ignored = %v", got)
	}
	if code := put(`{"identifiers":["a.three",""],"ignored":true}`); code != http.StatusBadRequest {
		t.Fatalf("a bad identifier refuses the whole batch: %d", code)
	}
	if got := listIgnoredSensors(); len(got) != 2 {
		t.Fatalf("none of a refused batch is applied: %v", got)
	}
	if code := put(`{"identifiers":["a.one","a.two"],"ignored":false}`); code != http.StatusOK || len(listIgnoredSensors()) != 0 {
		t.Fatalf("code %d ignored %v", code, listIgnoredSensors())
	}
}

// --- the tenth review -------------------------------------------------------------

// A model that no longer matches the boat, after a failed attempt, shows the failure.
func TestHouse_AFailedAttemptIsShownWhenTheKeptModelNoLongerMatches(t *testing.T) {
	file := keptHouseRecord(t)
	rec := file.Subsystems[normalSubsystemElectrical]
	rec.State, rec.Problem = normalStateFailed, "electrical read failed"
	file.Subsystems[normalSubsystemElectrical] = rec
	other := houseVessel()
	other.HouseBank = &vesselHouseBankSetting{Path: "electrical.batteries.2", EquipmentID: "eq-bank2"}
	sc := newScoreCase(file, nil)
	sc.vessel = &other
	if res := sc.scoreHouse(24 * time.Hour); res.ProblemKind != unusualNoModel || res.Problem != "electrical read failed" {
		t.Fatalf("score: %q %q", res.ProblemKind, res.Problem)
	}
	st := buildNormalStatus(file, nil, nil, nil, &other, time.UTC)
	for _, g := range st.Subsystems {
		if g.ID == normalSubsystemElectrical && g.Note != "No learned normal: electrical read failed." {
			t.Fatalf("status note = %q", g.Note)
		}
	}
}

// A threshold rule for the bank going below a limit is the bank being used. Any
// other alarm at its voltage or charge, a high one or one the boat's own monitor
// raised, is a fault.
func TestHouseTraining_OnlyALowThresholdOnTheBanksVoltageOrChargeIsLeftIn(t *testing.T) {
	withTempAlarmRules(t)
	below, err := createAlarmRule(alarmRule{Enabled: true, Path: "electrical.batteries.1.voltage", Label: "Low", Op: alarmOpBelow, Value: 24, State: alarmStateWarn})
	if err != nil {
		t.Fatal(err)
	}
	above, err := createAlarmRule(alarmRule{Enabled: true, Path: "electrical.batteries.1.voltage", Label: "High", Op: alarmOpAbove, Value: 29, State: alarmStateWarn})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, path, rule string
		out              bool
	}{
		{"low voltage rule", "notifications.electrical.batteries.1.voltage", below.ID, false},
		{"high voltage rule", "notifications.electrical.batteries.1.voltage", above.ID, true},
		{"the boat's monitor", "notifications.electrical.batteries.1.voltage", "", true},
		{"a rule since deleted", "notifications.electrical.batteries.1.voltage", "gone", true},
	}
	for _, c := range cases {
		h := houseHarness(houseFeed(171, houseSpells()))
		cleared := daysAgo(10).Add(45 * time.Minute)
		h.alarms = []alarmLogEntry{{ID: "a1", RuleID: c.rule, Path: c.path, RaisedAt: daysAgo(10).Add(30 * time.Minute), ClearedAt: &cleared}}
		m := h.trainHouse(t, normalSubsystemModel{})
		if got := len(m.Excluded) > 0; got != c.out {
			t.Errorf("%s: minutes taken out = %v, want %v", c.name, got, c.out)
		}
	}
}

// A vessel that cannot be read does not hide a change to a path the bank might use.
func TestNormalPathMatters_AnUnreadableVesselWakesForABankPath(t *testing.T) {
	old := normalPathVessel
	normalPathVessel = func() (vesselView, error) { return vesselView{}, errors.New("settings.yaml is unreadable") }
	t.Cleanup(func() { normalPathVessel = old })
	if !normalPathMatters("electrical.batteries.1.voltage") {
		t.Error("the trainer must hear of a change it cannot place")
	}
	if normalPathMatters("environment.wind.speedApparent") {
		t.Error("a wind sensor is nothing to do with it")
	}
}

// A mixed result keeps both reasons; the report does not replace it whole.
func TestGatherWatchReportInput_AMixedResultKeepsEverySubsystemsReason(t *testing.T) {
	old := watchUnusualBudget
	watchUnusualBudget = 30 * time.Millisecond
	defer func() { watchUnusualBudget = old }()
	in := gatherWithUnusual(t, context.Background(), func(c context.Context, _, _ time.Time, _ unusualSubject, _ []normalAnswer) unusualResult {
		<-c.Done()
		return unusualResult{ProblemKind: unusualMixed, Problem: "Engines: No learned normal yet. House bank: it took longer than 90 seconds."}
	})
	if in.Unusual == nil || !strings.Contains(in.Unusual.Problem, "Engines: No learned normal yet.") {
		t.Fatalf("Unusual = %+v", in.Unusual)
	}
}

// Ignoring all the parts of a reading checks the boat once and wakes once.
func TestIgnoredSensors_ABatchChecksTheVesselOnce(t *testing.T) {
	withTempIgnoreStore(t)
	calls := 0
	old := normalPathVessel
	normalPathVessel = func() (vesselView, error) { calls++; return houseVessel(), nil }
	t.Cleanup(func() { normalPathVessel = old })
	drainNormalWake()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/api/alarms/ignored-sensors", strings.NewReader(`{"identifiers":["electrical.alternator.p.current","electrical.alternator.q.current","electrical.batteries.1.voltage"],"ignored":true}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if err := setIgnoredSensorsHandler(e.NewContext(req, httptest.NewRecorder())); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !woke() {
		t.Fatalf("vessel read %d times", calls)
	}
}

func partlyBothAlternators() []houseSpell {
	spells := houseSpells()
	for i, both := range []bool{true, true, false, false} {
		s := gatedHouse(daysAgo(13-float64(i)), 70, 0.7)
		s.Alt, s.Alt2 = true, both
		spells = append(spells, s)
	}
	return spells
}

// --- the final review ----------------------------------------------------------------

// A bank with no charge reading says which reading is missing.
func TestTrain_ARequiredReadingWithNoRowsIsNamed(t *testing.T) {
	feed := houseFeed(181, houseSpells())
	delete(feed, "electrical.batteries.1.capacity.stateOfCharge")
	h := houseHarness(feed)
	m := h.trainHouse(t, normalSubsystemModel{})
	if got := m.Subjects["house"].Problem; got != "House bank state of charge: no readings in the last 90 days" {
		t.Fatalf("problem = %q", got)
	}
}

// A read of a shared reading in flight does not hold up a read of another.
func TestShareMinutes_ASharedReadInFlightDoesNotBlockAnotherRead(t *testing.T) {
	released := make(chan struct{})
	read := func(ctx context.Context, _ normalSource, paths []string, _, _ time.Time) (map[string][]normalMinuteRow, error) {
		out := map[string][]normalMinuteRow{}
		for _, p := range paths {
			if p == "shared" {
				select {
				case <-released:
				case <-time.After(3 * time.Second):
				}
			} else {
				close(released)
			}
			out[p] = nil
		}
		return out, nil
	}
	share := shareMinutes(read, map[string]bool{"shared": true})
	done := make(chan struct{}, 2)
	at := time.Now()
	go func() {
		_, _ = share(context.Background(), normalSource{}, []string{"shared"}, at, at)
		done <- struct{}{}
	}()
	time.Sleep(50 * time.Millisecond)
	go func() {
		_, _ = share(context.Background(), normalSource{}, []string{"other"}, at, at)
		done <- struct{}{}
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("a read was held up behind another")
		}
	}
}

// Every subsystem supplies its own words.
func TestSelectFeatures_EvidenceWithoutWordingHasNoDefault(t *testing.T) {
	gated := selectMinutes(300)
	ev := featureEvidenceFor("x", "electrical.x", gated, func(int) (float64, bool) { return 5, true })
	ev.Wording = normalWording{}
	v := selectFeatures([]featureEvidence{ev}, gated, gated, nil, noLimits)[0]
	if strings.Contains(v.Reason, "running") {
		t.Fatalf("an engine's words were put in a subsystem's mouth: %q", v.Reason)
	}
}

// An unavailable store is a server error, and a batch has a limit.
func TestIgnoredSensors_ABatchHasALimitAndAnUnavailableStoreIsAServerError(t *testing.T) {
	put := func(body string) int {
		e := echo.New()
		req := httptest.NewRequest(http.MethodPut, "/api/alarms/ignored-sensors", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		_ = setIgnoredSensorsHandler(e.NewContext(req, rec))
		return rec.Code
	}
	prev := globalIgnoreStore
	globalIgnoreStore = nil
	t.Cleanup(func() { globalIgnoreStore = prev })
	if code := put(`{"identifiers":["a"],"ignored":true}`); code != http.StatusInternalServerError {
		t.Errorf("no store: %d", code)
	}
	withTempIgnoreStore(t)
	var many []string
	for i := 0; i < 65; i++ {
		many = append(many, fmt.Sprintf("p.%d", i))
	}
	body, _ := json.Marshal(map[string]any{"identifiers": many, "ignored": true})
	if code := put(string(body)); code != http.StatusBadRequest {
		t.Errorf("65 identifiers: %d", code)
	}
}

// A vessel that cannot be read wakes the trainer for an engine path too.
func TestNormalPathMatters_AnUnreadableVesselWakesForAnEnginePath(t *testing.T) {
	old := normalPathVessel
	normalPathVessel = func() (vesselView, error) { return vesselView{}, errors.New("settings.yaml is unreadable") }
	t.Cleanup(func() { normalPathVessel = old })
	if !normalPathMatters("propulsion.port.oilPressure") {
		t.Error("the trainer must hear of an engine change it cannot place")
	}
}
