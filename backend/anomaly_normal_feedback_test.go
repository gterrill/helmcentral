package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// What the operator says about a stretch, and what the maintenance log says
// about a repair, change what the learned normal is trained on (ADR 0276,
// Feedback).

func answerFor(id string, from time.Time, minutes int, answer string) normalAnswer {
	return normalAnswer{StretchID: id, Subsystem: normalSubsystemEngines, Subject: "port", From: from, To: from.Add(time.Duration(minutes) * time.Minute), Answer: answer}
}

func threeRunFeed() (feed map[string][]normalMinuteRow, r1, r2, r3 time.Time) {
	return longRunFeed(100)
}

// longRunFeed is three runs of the given length at 1,900 rpm, each judged for 10
// minutes fewer.
func longRunFeed(minutes int) (feed map[string][]normalMinuteRow, r1, r2, r3 time.Time) {
	r1, r2, r3 = daysAgo(5), daysAgo(3), daysAgo(1)
	feed = synthFeed("port", 1, []synthRun{{Start: r1, Minutes: minutes, RPM: 1900}, {Start: r2, Minutes: minutes, RPM: 1900}, {Start: r3, Minutes: minutes, RPM: 1900}})
	return feed, r1, r2, r3
}

// dropOilPressure pulls the oil pressure of [from, from+minutes) far below the
// engine's usual at that rpm: a fault the reweighting pass would trim.
func dropOilPressure(feed map[string][]normalMinuteRow, from time.Time, minutes int) {
	rows := feed["propulsion.port.oilPressure"]
	for i := range rows {
		if !rows[i].At.Before(from) && rows[i].At.Before(from.Add(time.Duration(minutes)*time.Minute)) {
			rows[i].Value -= 120000
		}
	}
}

func TestTrain_AStretchAnsweredRealIsLeftOutLikeAnAlarm(t *testing.T) {
	feed, r1, _, _ := threeRunFeed()
	h := newTrainHarness(feed, "port")
	h.answers = []normalAnswer{answerFor("engines:port@x", r1.Add(50*time.Minute), 20, normalAnswerReal)}
	m := h.train(t, normalSubsystemModel{})
	port := regimeNamed(t, m, "port/rpm1800")
	if port.Minutes != 270-20 || port.ExcludedMinutes != 20 {
		t.Fatalf("minutes %d excluded %d, want 250 and 20", port.Minutes, port.ExcludedMinutes)
	}
	if len(m.Excluded) != 1 || len(m.Labelled) != 0 {
		t.Fatalf("excluded %+v labelled %+v", m.Excluded, m.Labelled)
	}
	ex := m.Excluded[0]
	// The stretch's end is the instant after its last minute, so the minute that
	// starts there is kept: 20 minutes are out, not 21.
	if ex.Subject != "port" || ex.Reason != normalReasonAnsweredReal || ex.Ref != "engines:port@x" ||
		!ex.From.Equal(r1.Add(50*time.Minute)) || !ex.To.Equal(r1.Add(70*time.Minute)) {
		t.Fatalf("excluded entry = %+v", ex)
	}
}

func TestTrain_AnUnknownAnswerOrAnotherEnginesAnswerChangesNothing(t *testing.T) {
	feed, r1, _, _ := threeRunFeed()
	h := newTrainHarness(feed, "port")
	other := answerFor("engines:starboard@x", r1.Add(50*time.Minute), 20, normalAnswerReal)
	other.Subject = "starboard"
	elsewhere := answerFor("house:port@x", r1.Add(50*time.Minute), 20, normalAnswerReal)
	elsewhere.Subsystem = "house"
	h.answers = []normalAnswer{answerFor("engines:port@x", r1.Add(50*time.Minute), 20, normalAnswerUnknown), other, elsewhere}
	m := h.train(t, normalSubsystemModel{})
	if port := regimeNamed(t, m, "port/rpm1800"); port.Minutes != 270 || port.ExcludedMinutes != 0 {
		t.Fatalf("minutes %d excluded %d", port.Minutes, port.ExcludedMinutes)
	}
	if len(m.Excluded) != 0 || len(m.Labelled) != 0 {
		t.Fatalf("excluded %+v labelled %+v", m.Excluded, m.Labelled)
	}
}

func TestTrain_AStretchAnsweredNormalSurvivesReweighting(t *testing.T) {
	feed, _, r2, _ := longRunFeed(210)
	fault := r2.Add(100 * time.Minute)
	dropOilPressure(feed, fault, 6)

	plain := newTrainHarness(feed, "port").train(t, normalSubsystemModel{})
	if trimmed := regimeNamed(t, plain, "port/rpm1800").Trimmed; trimmed < 6 {
		t.Fatalf("setup: the unlabelled fault should be trimmed, trimmed %d", trimmed)
	}

	h := newTrainHarness(feed, "port")
	h.answers = []normalAnswer{answerFor("engines:port@y", fault, 6, normalAnswerNormal)}
	m := h.train(t, normalSubsystemModel{})
	port := regimeNamed(t, m, "port/rpm1800")
	if port.Trimmed >= 3 {
		t.Fatalf("a stretch the operator called normal must not be trimmed, trimmed %d", port.Trimmed)
	}
	if port.Minutes != 600 {
		t.Fatalf("every minute is still a minute of the band, got %d", port.Minutes)
	}
	// Labelled is not Excluded: the minutes are kept, not taken out.
	if len(m.Excluded) != 0 || len(m.Labelled) != 1 {
		t.Fatalf("excluded %+v labelled %+v", m.Excluded, m.Labelled)
	}
	if l := m.Labelled[0]; l.Reason != normalReasonAnsweredNormal || l.Ref != "engines:port@y" || l.Subject != "port" ||
		!l.From.Equal(fault) || !l.To.Equal(fault.Add(6*time.Minute)) {
		t.Fatalf("labelled entry = %+v", l)
	}
}

func TestTrain_AnAlarmBeatsANormalAnswerOverTheSameMinutes(t *testing.T) {
	feed, _, r2, _ := longRunFeed(210)
	fault := r2.Add(100 * time.Minute)
	dropOilPressure(feed, fault, 6)
	cleared := r2.Add(108 * time.Minute)
	alarm := []alarmLogEntry{{ID: "a1", Label: "Port Engine Oil Pressure Low", Path: "propulsion.port.oilPressure", RaisedAt: r2.Add(101 * time.Minute), ClearedAt: &cleared}}

	alone := newTrainHarness(feed, "port")
	alone.alarms = alarm
	want := regimeNamed(t, alone.train(t, normalSubsystemModel{}), "port/rpm1800")

	h := newTrainHarness(feed, "port")
	h.alarms = alarm
	h.answers = []normalAnswer{answerFor("engines:port@y", fault, 6, normalAnswerNormal)}
	got := regimeNamed(t, h.train(t, normalSubsystemModel{}), "port/rpm1800")
	if got.Minutes != want.Minutes || got.ExcludedMinutes != want.ExcludedMinutes || got.Trimmed != want.Trimmed {
		t.Fatalf("with the answer %d minutes, %d excluded, %d trimmed; the alarm alone gave %d, %d, %d",
			got.Minutes, got.ExcludedMinutes, got.Trimmed, want.Minutes, want.ExcludedMinutes, want.Trimmed)
	}
}

func TestTrain_AnswersAndMaintenanceThatCannotBeReadStopTheRun(t *testing.T) {
	feed, _, _, _ := threeRunFeed()
	h := newTrainHarness(feed, "port")
	h.answersErr = errors.New("parsing answers.json: invalid character")
	if _, err := trainSubsystem(context.Background(), normalEnginesSubsystem(), h.deps(), normalSubsystemModel{}); err == nil || !strings.Contains(err.Error(), "answers about unusual stretches could not be read") {
		t.Fatalf("err = %v", err)
	}
	h = newTrainHarness(feed, "port")
	h.repairsErr = errors.New("the maintenance log is not open")
	if _, err := trainSubsystem(context.Background(), normalEnginesSubsystem(), h.deps(), normalSubsystemModel{}); err == nil || !strings.Contains(err.Error(), "maintenance log could not be read") {
		t.Fatalf("err = %v", err)
	}
}

func repairEntry(id, equipment, startedAt, performedAt string) maintenanceRepair {
	return maintenanceRepair{ID: id, EquipmentID: equipment, StartedAt: startedAt, PerformedAt: performedAt, Description: "Replaced the oil pressure sender"}
}

func TestTrain_ARepairLeavesOutTheEnginesFaultRange(t *testing.T) {
	feed, r1, _, _ := threeRunFeed()
	zone := time.FixedZone("UTC+12", 12*3600)
	h := newTrainHarness(feed, "port")
	h.engines[0].EquipmentID = "eq-port"
	h.boatLoc = zone
	// Run 1 is 3.00 AM UTC on the 5th: 3.00 PM that day on the boat.
	day := r1.In(zone).Format("2006-01-02")
	h.repairs = []maintenanceRepair{repairEntry("log1", "eq-port", day, day)}
	m := h.train(t, normalSubsystemModel{})

	port := regimeNamed(t, m, "port/rpm1800")
	if port.Minutes != 180 || port.ExcludedMinutes != 90 {
		t.Fatalf("minutes %d excluded %d, want 180 and 90", port.Minutes, port.ExcludedMinutes)
	}
	if len(m.Excluded) != 1 {
		t.Fatalf("excluded = %+v", m.Excluded)
	}
	ex := m.Excluded[0]
	start := time.Date(r1.In(zone).Year(), r1.In(zone).Month(), r1.In(zone).Day(), 0, 0, 0, 0, zone)
	if ex.Subject != "port" || ex.Reason != normalReasonRepair || ex.Ref != "log1" || ex.Title != "Replaced the oil pressure sender" ||
		!ex.From.Equal(start) || !ex.To.Equal(start.AddDate(0, 0, 1)) {
		t.Fatalf("excluded entry = %+v (want %s to %s)", ex, start, start.AddDate(0, 0, 1))
	}
}

func TestTrain_ARepairOnAnotherItemLeavesThisEngineAlone(t *testing.T) {
	feed, r1, _, _ := threeRunFeed()
	day := r1.Format("2006-01-02")
	h := newTrainHarness(feed, "port")
	h.engines[0].EquipmentID = "eq-port"
	h.boatLoc = time.UTC
	h.repairs = []maintenanceRepair{repairEntry("log2", "eq-starboard", day, day)}
	m := h.train(t, normalSubsystemModel{})
	if len(m.Excluded) != 0 || regimeNamed(t, m, "port/rpm1800").Minutes != 270 {
		t.Fatalf("excluded %+v", m.Excluded)
	}
}

func TestTrain_ARepairWithADateThatDoesNotParseStopsTheRun(t *testing.T) {
	feed, _, _, _ := threeRunFeed()
	h := newTrainHarness(feed, "port")
	h.engines[0].EquipmentID = "eq-port"
	h.boatLoc = time.UTC
	h.repairs = []maintenanceRepair{repairEntry("log1", "eq-port", "last week", "2026-11-05")}
	if _, err := trainSubsystem(context.Background(), normalEnginesSubsystem(), h.deps(), normalSubsystemModel{}); err == nil || !strings.Contains(err.Error(), "log1") {
		t.Fatalf("err = %v", err)
	}
}

// A repair's dates are days on the boat's clock, and a zone with summer time has
// a different offset on different dates, so each date takes the offset in force
// on it. The boat keeps no history of where it was, so a repair dated before the
// boat crossed into another zone is still read in the zone it is in now.
func TestNormalRepairRanges_EachDateTakesTheOffsetInForceOnIt(t *testing.T) {
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("no zone database: %v", err)
	}
	got, err := normalRepairRanges([]maintenanceRepair{repairEntry("a", "eq", "2026-07-01", "2026-07-02"), repairEntry("b", "eq", "2026-11-05", "2026-11-05")}, func() (*time.Location, error) { return zone, nil })
	if err != nil || len(got) != 2 {
		t.Fatalf("ranges %+v err %v", got, err)
	}
	// Summer: UTC-4. November, after the clocks went back: UTC-5.
	if want := time.Date(2026, 7, 1, 4, 0, 0, 0, time.UTC); !got[0].From.Equal(want) || !got[0].To.Equal(want.AddDate(0, 0, 2)) {
		t.Fatalf("summer range = %v to %v", got[0].From, got[0].To)
	}
	if want := time.Date(2026, 11, 5, 5, 0, 0, 0, time.UTC); !got[1].From.Equal(want) || !got[1].To.Equal(want.Add(24*time.Hour)) {
		t.Fatalf("autumn range = %v to %v", got[1].From, got[1].To)
	}
}

const repairZoneProblem = "The boat's time zone is unknown, so repair dates could not be placed."

func TestTrain_AnUnknownZoneStopsTheRunOnlyWhenARepairNeedsPlacing(t *testing.T) {
	feed, r1, _, _ := threeRunFeed()
	day := r1.Format("2006-01-02")
	path := filepath.Join(t.TempDir(), "anomaly-normal.json")
	good := newTrainHarness(feed, "port")
	good.engines[0].EquipmentID = "eq-port"
	good.boatLoc = time.UTC
	if err := trainNormal(context.Background(), path, good.deps()); err != nil {
		t.Fatal(err)
	}
	before, _ := loadNormalModel(path)

	h := newTrainHarness(feed, "port")
	h.engines[0].EquipmentID = "eq-port"
	h.boatLocErr = errors.New("no fix")
	h.repairs = []maintenanceRepair{repairEntry("log1", "eq-port", day, day)}
	err := trainNormal(context.Background(), path, h.deps())
	if err == nil || err.Error() != repairZoneProblem {
		t.Fatalf("err = %v, want %q", err, repairZoneProblem)
	}
	after, _ := loadNormalModel(path)
	if rec := after.Subsystems[normalSubsystemEngines]; rec.State != normalStateFailed || rec.Problem != repairZoneProblem {
		t.Fatalf("record = %+v", rec)
	}
	if len(after.Subsystems[normalSubsystemEngines].Regimes) != len(before.Subsystems[normalSubsystemEngines].Regimes) || len(before.Subsystems[normalSubsystemEngines].Regimes) == 0 {
		t.Fatal("the last good model must be kept")
	}

	// With no repair to place, the zone does not matter.
	h.repairs = nil
	if err := trainNormal(context.Background(), path, h.deps()); err != nil {
		t.Fatalf("no repairs, no zone needed: %v", err)
	}
}

// A band that exclusions leave too thin is not rescued by its previous normal when
// that normal was learned with the very minutes the operator or the log has since
// called a fault.
func TestTrain_ABandThinnedByExclusionsIsNotCarriedFromAFitThatHadTheFault(t *testing.T) {
	feed := synthFeed("port", 5, goodRuns())
	h := newTrainHarness(feed, "port")
	prev := h.train(t, normalSubsystemModel{})
	if r := regimeNamed(t, prev, "port/rpm1800"); !r.Judged || r.Minutes != 140 {
		t.Fatalf("setup: %+v", r)
	}

	// 45 of each run's 70 judged minutes are a fault: 50 minutes are left.
	runA, runB := daysAgo(10), daysAgo(6)
	h.answers = []normalAnswer{
		answerFor("engines:port@a", runA.Add(20*time.Minute), 45, normalAnswerReal),
		answerFor("engines:port@b", runB.Add(20*time.Minute), 45, normalAnswerReal),
	}
	m := h.train(t, prev)
	r := regimeNamed(t, m, "port/rpm1800")
	if r.CarriedForward || r.Judged || r.Minutes != 50 || !strings.Contains(r.Reason, "minutes learned, needs 60") {
		t.Fatalf("the band should be thin, not carried: %+v", r)
	}

	// Exclusions that fall after the previous fit do not taint it: those minutes
	// were never in it.
	later := newTrainHarness(feed, "port")
	later.now = trainNow.Add(48 * time.Hour)
	later.answers = []normalAnswer{answerFor("engines:port@c", later.now.Add(-time.Hour), 30, normalAnswerReal)}
	if m := later.train(t, prev); !regimeNamed(t, m, "port/rpm1800").Judged {
		t.Fatal("a later fault leaves a judged band alone")
	}
}

// A repair on a watermaker has nothing to say about an engine, so the engine's
// training must not stop on a zone it never needed.
func TestTrain_ARepairOnAnotherItemNeverAsksForTheZone(t *testing.T) {
	feed, r1, _, _ := threeRunFeed()
	day := r1.Format("2006-01-02")
	h := newTrainHarness(feed, "port")
	h.engines[0].EquipmentID = "eq-port"
	h.boatLocErr = errors.New("no fix")
	h.repairs = []maintenanceRepair{repairEntry("log1", "eq-watermaker", day, day)}
	m := h.train(t, normalSubsystemModel{})
	if len(m.Excluded) != 0 || regimeNamed(t, m, "port/rpm1800").Minutes != 270 {
		t.Fatalf("excluded %+v", m.Excluded)
	}
}

// An old normal is never reused if a real answer or a repair for the engine
// overlaps the window it was learned from, however old that window is. An alarm
// never stops it: the previous fit left the alarm out too.
func TestTrain_AnAlarmNeverStopsCarryForwardButARealAnswerOrRepairInItsWindowDoes(t *testing.T) {
	runA := daysAgo(10)
	full := synthFeed("port", 5, goodRuns())
	onlyA := synthFeed("port", 5, []synthRun{gatedRun(runA, 70, 1900)})
	cleared := runA.Add(40 * time.Minute)
	alarm := []alarmLogEntry{{ID: "a1", Label: "Port Engine Oil Pressure Low", Path: "propulsion.port.oilPressure", RaisedAt: runA.Add(30 * time.Minute), ClearedAt: &cleared}}

	prevH := newTrainHarness(full, "port")
	prevH.alarms = alarm
	prev := prevH.train(t, normalSubsystemModel{})
	if r := regimeNamed(t, prev, "port/rpm1800"); !r.Judged {
		t.Fatalf("setup: %+v", r)
	}
	// Run B has left the data: one run is too thin to fit, so the band is reused or not.
	thin := func(h *trainHarness) normalRegime {
		return regimeNamed(t, h.train(t, prev), "port/rpm1800")
	}

	h := newTrainHarness(onlyA, "port")
	h.alarms = alarm
	if r := thin(h); !r.CarriedForward || !r.Judged {
		t.Fatalf("an alarm never stops the reuse: %+v", r)
	}

	h.answers = []normalAnswer{answerFor("engines:port@new", runA.Add(15*time.Minute), 10, normalAnswerReal)}
	if r := thin(h); r.CarriedForward || r.Judged || !strings.Contains(r.Reason, "minutes learned, needs 60") {
		t.Fatalf("a real answer inside the old window stops it: %+v", r)
	}

	// The answer is not inside the current window at all, only the old one: it still stops it.
	h.answers = []normalAnswer{answerFor("engines:port@ancient", prev.Regimes["port/rpm1800"].Window.From.Add(time.Hour), 10, normalAnswerReal)}
	if r := thin(h); r.CarriedForward || r.Judged {
		t.Fatalf("an answer in the old window, before the current one, stops it: %+v", r)
	}
}

func TestTrain_ARepairInsideAnOldCarriedWindowStopsTheCarry(t *testing.T) {
	prevH := newTrainHarness(synthFeed("port", 5, goodRuns()), "port")
	prev := prevH.train(t, normalSubsystemModel{})
	// The regime was learned from a window that ended 20 days ago and began 170 days ago.
	r := prev.Regimes["port/rpm1800"]
	r.LearnedAt = trainNow.Add(-20 * 24 * time.Hour)
	r.Window = normalWindow{From: trainNow.Add(-170 * 24 * time.Hour), To: trainNow.Add(-20 * 24 * time.Hour)}
	prev.Regimes["port/rpm1800"] = r

	onlyA := synthFeed("port", 5, []synthRun{gatedRun(daysAgo(10), 70, 1900)})
	h := newTrainHarness(onlyA, "port")
	h.engines[0].EquipmentID = "eq-port"
	h.boatLoc = time.UTC
	if got := regimeNamed(t, h.train(t, prev), "port/rpm1800"); !got.CarriedForward {
		t.Fatalf("setup: with nothing against it the old normal is reused: %+v", got)
	}
	// 120 days ago: before this run's 90 days, inside the old regime's window.
	day := trainNow.Add(-120 * 24 * time.Hour).Format("2006-01-02")
	h.repairs = []maintenanceRepair{repairEntry("old", "eq-port", day, day)}
	if got := regimeNamed(t, h.train(t, prev), "port/rpm1800"); got.CarriedForward || got.Judged {
		t.Fatalf("a repair in the old window stops the reuse: %+v", got)
	}
}

// With every steady minute left out there is nothing to fit, and the old normal
// is the only thing left to show. It is not shown if a repair covers its window.
func TestTrain_WhenEverySteadyMinuteIsLeftOutAFaultInTheOldWindowStillStopsTheCarry(t *testing.T) {
	feed := synthFeed("port", 5, goodRuns())
	prev := newTrainHarness(feed, "port").train(t, normalSubsystemModel{})

	h := newTrainHarness(feed, "port")
	h.engines[0].EquipmentID = "eq-port"
	h.boatLoc = time.UTC
	h.train(t, prev) // with nothing against it, this run learns the bands again
	first, last := daysAgo(11).Format("2006-01-02"), daysAgo(5).Format("2006-01-02")
	h.repairs = []maintenanceRepair{repairEntry("all", "eq-port", first, last)}
	m := h.train(t, prev)
	if r, has := m.Regimes["port/rpm1800"]; has {
		t.Fatalf("no steady minute is left and a repair covers the old window, so nothing is carried: %+v", r)
	}
	if !strings.Contains(m.Subjects["port"].Problem, "were left out") {
		t.Fatalf("problem = %q", m.Subjects["port"].Problem)
	}

	// An alarm over every steady minute is the same, bar the carry: it is carried.
	alarmed := newTrainHarness(feed, "port")
	start, end := daysAgo(11), daysAgo(5).Add(2*time.Hour)
	alarmed.alarms = []alarmLogEntry{{ID: "all", Label: "Port Engine Temperature High", Path: "propulsion.port.temperature", RaisedAt: start, ClearedAt: &end}}
	m = alarmed.train(t, prev)
	if r := m.Regimes["port/rpm1800"]; !r.CarriedForward {
		t.Fatalf("an alarm never stops the reuse: %+v", r)
	}
}

// A repair from two years ago ended long before anything this run or an old normal
// was learned from, so it never needs the boat's zone.
func TestTrain_AnOldRepairNeverAsksForTheZone(t *testing.T) {
	feed, _, _, _ := threeRunFeed()
	old := trainNow.Add(-2 * 365 * 24 * time.Hour).Format("2006-01-02")
	h := newTrainHarness(feed, "port")
	h.engines[0].EquipmentID = "eq-port"
	h.boatLocErr = errors.New("no fix")
	h.repairs = []maintenanceRepair{repairEntry("log1", "eq-port", old, old)}
	if m := h.train(t, normalSubsystemModel{}); regimeNamed(t, m, "port/rpm1800").Minutes != 270 {
		t.Fatalf("regime = %+v", m.Regimes)
	}

	// One inside an old normal's window is still read.
	prev := h.train(t, normalSubsystemModel{})
	r := prev.Regimes["port/rpm1800"]
	r.Window = normalWindow{From: trainNow.Add(-2*365*24*time.Hour - 24*time.Hour), To: r.Window.To}
	prev.Regimes["port/rpm1800"] = r
	if _, err := trainSubsystem(context.Background(), normalEnginesSubsystem(), h.deps(), prev); err == nil || err.Error() != repairZoneProblem {
		t.Fatalf("err = %v", err)
	}
}
