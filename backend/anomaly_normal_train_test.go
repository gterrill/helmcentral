package main

import (
	"context"
	"errors"
	"math/rand"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- synthetic engines -------------------------------------------------------

var trainNow = time.Date(2026, 11, 10, 3, 0, 0, 0, time.UTC)

// synthRun is one spell of an engine running at one rpm. Minutes counts every
// minute of the spell; the first ten fall to the running-time gate, so a run of
// Minutes judges Minutes-10 of them.
type synthRun struct {
	Start   time.Time
	Minutes int
	RPM     float64
	// RPMEnd, when set, ramps rpm in a straight line from RPM to RPMEnd over the run.
	RPMEnd  float64
	Exhaust bool
}

// gatedRun is a run that leaves exactly n judged minutes.
func gatedRun(start time.Time, n int, rpm float64) synthRun {
	return synthRun{Start: start, Minutes: n + 10, RPM: rpm}
}

// synthFeed builds one engine's one-minute rows by path.
func synthFeed(instance string, seed int64, runs []synthRun) map[string][]normalMinuteRow {
	rng := rand.New(rand.NewSource(seed))
	out := map[string][]normalMinuteRow{}
	add := func(key string, at time.Time, v float64) {
		path := "propulsion." + instance + "." + key
		out[path] = append(out[path], normalMinuteRow{Path: path, Source: "can0.1", At: at, Value: v})
	}
	for _, r := range runs {
		for i := 0; i < r.Minutes; i++ {
			at := r.Start.Add(time.Duration(i) * time.Minute).Truncate(time.Minute)
			rpm := r.RPM
			if r.RPMEnd != 0 {
				rpm = r.RPM + (r.RPMEnd-r.RPM)*float64(i)/float64(r.Minutes)
			}
			add("revolutions", at, rpm/60+0.05*rng.NormFloat64())
			add("temperature", at, 353.15+0.4*rng.NormFloat64())
			add("oilPressure", at, 250000+60*(rpm-1000)+8000*rng.NormFloat64())
			add("boostPressure", at, 20000+30*(rpm-900)+1500*rng.NormFloat64())
			add("engineLoad", at, 0.2+0.0002*(rpm-900)+0.02*rng.NormFloat64())
			if r.Exhaust {
				add("exhaustTemperature", at, 600+0.1*(rpm-900)+15*rng.NormFloat64())
			}
		}
	}
	return out
}

func mergeFeeds(feeds ...map[string][]normalMinuteRow) map[string][]normalMinuteRow {
	out := map[string][]normalMinuteRow{}
	for _, f := range feeds {
		for p, rows := range f {
			out[p] = append(out[p], rows...)
		}
	}
	return out
}

type trainHarness struct {
	feed    map[string][]normalMinuteRow
	engines []vesselEngineSetting
	alarms  []alarmLogEntry
	alarmEr error
	// answers and repairs are the operator's answers and the maintenance log.
	answers    []normalAnswer
	answersErr error
	repairs    []maintenanceRepair
	repairsErr error
	boatLoc    *time.Location
	boatLocErr error
	ignored    map[string]bool
	source     normalSource
	srcErr     error
	minErr     error
	now        time.Time
	asked      []normalSource
	// requested is the paths of each read, one entry per read.
	requested [][]string
	// vessel, when set, is the whole vessel, in place of the engines above.
	vessel *vesselView
	// failPathPrefix makes a read that asks for a path with it fail.
	failPathPrefix string
	// vesselErr makes the vessel unreadable.
	vesselErr error
	// nowStep advances the clock by that much each time it is read.
	nowStep time.Duration
	// alarmReads counts reads of the alarm log.
	alarmReads int
}

func newTrainHarness(feed map[string][]normalMinuteRow, engines ...string) *trainHarness {
	h := &trainHarness{feed: feed, now: trainNow, source: normalSource{Kind: normalSourceMainBucket}}
	for _, e := range engines {
		h.engines = append(h.engines, vesselEngineSetting{Instance: e, Name: strings.ToUpper(e[:1]) + e[1:]})
	}
	return h
}

func (h *trainHarness) deps() normalTrainDeps {
	return normalTrainDeps{
		Now: func() time.Time {
			at := h.now
			h.now = h.now.Add(h.nowStep)
			return at
		},
		Source: func() (normalSource, error) { return h.source, h.srcErr },
		Minutes: func(ctx context.Context, src normalSource, paths []string, start, stop time.Time) (map[string][]normalMinuteRow, error) {
			h.asked = append(h.asked, src)
			h.requested = append(h.requested, paths)
			if h.minErr != nil {
				return nil, h.minErr
			}
			for _, p := range paths {
				if h.failPathPrefix != "" && strings.HasPrefix(p, h.failPathPrefix) {
					return nil, errors.New(strings.TrimSuffix(h.failPathPrefix, ".") + " read failed")
				}
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return minutesFrom(h.feed, paths, start, stop), nil
		},
		Vessel: func() (vesselView, error) {
			if h.vesselErr != nil {
				return vesselView{}, h.vesselErr
			}
			if h.vessel != nil {
				return *h.vessel, nil
			}
			return vesselView{Engines: h.engines}, nil
		},
		Ignored: func() map[string]bool { return h.ignored },
		Alarms: func(from, to time.Time) ([]alarmLogEntry, error) {
			h.alarmReads++
			return h.alarms, h.alarmEr
		},
		Answers:  func() ([]normalAnswer, error) { return h.answers, h.answersErr },
		Repairs:  func() ([]maintenanceRepair, error) { return h.repairs, h.repairsErr },
		BoatLoc:  func() (*time.Location, error) { return h.boatLoc, h.boatLocErr },
		Classify: classifyRange,
	}
}

func (h *trainHarness) train(t *testing.T, prev normalSubsystemModel) normalSubsystemModel {
	t.Helper()
	m, err := trainSubsystem(context.Background(), normalEnginesSubsystem(), h.deps(), prev)
	if err != nil {
		t.Fatalf("trainSubsystem: %v", err)
	}
	return m
}

func regimeNamed(t *testing.T, m normalSubsystemModel, key string) normalRegime {
	t.Helper()
	r, ok := m.Regimes[key]
	if !ok {
		keys := []string{}
		for k := range m.Regimes {
			keys = append(keys, k)
		}
		t.Fatalf("no regime %s among %v", key, keys)
	}
	return r
}

func daysAgo(d float64) time.Time { return trainNow.Add(-time.Duration(d * 24 * float64(time.Hour))) }

// --- tests -------------------------------------------------------------------

// A boat with one engine has no peer to compare with. The engine is compared
// with itself and gets bands.
func TestTrain_AOneEngineBoatLearnsBands(t *testing.T) {
	feed := synthFeed("0", 1, []synthRun{
		gatedRun(daysAgo(10), 70, 1900), gatedRun(daysAgo(6), 70, 1900), gatedRun(daysAgo(2), 70, 1900),
	})
	m := newTrainHarness(feed, "0").train(t, normalSubsystemModel{})
	r := regimeNamed(t, m, "0/rpm1800")
	if !r.Judged || r.Minutes != 210 || r.Runs != 3 || r.Label != "1,800 to 2,000 rpm" {
		t.Fatalf("regime = %+v", r)
	}
	want := []string{"revolutions", "temperature", "oilPressure", "boostPressure", "engineLoad"}
	if strings.Join(r.Features, ",") != strings.Join(want, ",") {
		t.Fatalf("features = %v, want %v", r.Features, want)
	}
	if r.Threshold <= 0 || len(r.Mean) != 5 || len(r.Corr) != 5 {
		t.Fatalf("learned figures missing: %+v", r.normalRegimeStats)
	}
	subj := m.Subjects["0"]
	if subj.Name != "0 engine" || subj.GatedMinutes != 210 {
		t.Fatalf("subject = %+v", subj)
	}
	var fuel normalFeatureVerdict
	for _, f := range subj.Features {
		if f.Key == "fuel.rate" {
			fuel = f
		}
	}
	if fuel.Used || fuel.Reason != "no readings" {
		t.Fatalf("a reading the engine never sent is stated, not filled in: %+v", fuel)
	}
	if m.Source.Kind != normalSourceMainBucket || !m.Window.To.Equal(trainNow) {
		t.Fatalf("source/window = %+v %+v", m.Source, m.Window)
	}
}

func TestTrain_AnAlarmForOneEngineTakesOutOnlyThatEnginesMinutes(t *testing.T) {
	r1, r2 := daysAgo(5), daysAgo(3)
	feed := mergeFeeds(
		synthFeed("port", 1, []synthRun{{Start: r1, Minutes: 100, RPM: 1900}, {Start: r2, Minutes: 100, RPM: 1900}}),
		synthFeed("starboard", 2, []synthRun{{Start: r1, Minutes: 100, RPM: 1900}, {Start: r2, Minutes: 100, RPM: 1900}}),
	)
	h := newTrainHarness(feed, "port", "starboard")
	// Raised at minute 50 of the first run, cleared at 70. Padding takes the
	// minutes from 40 to 75 out: 36 of the 90 judged ones.
	cleared := r1.Add(70 * time.Minute)
	h.alarms = []alarmLogEntry{
		{ID: "a1", RuleID: "r1", Label: "Port Engine Oil Pressure Low", Path: "notifications.propulsion.port.oilPressure", RaisedAt: r1.Add(50 * time.Minute), ClearedAt: &cleared},
		{ID: "a2", RuleID: "r2", Label: "House Bank Low", Path: "electrical.batteries.512.voltage", RaisedAt: r1.Add(50 * time.Minute), ClearedAt: &cleared},
	}
	m := h.train(t, normalSubsystemModel{})
	port, stbd := regimeNamed(t, m, "port/rpm1800"), regimeNamed(t, m, "starboard/rpm1800")
	if port.Minutes != 90-36+90 {
		t.Errorf("port minutes = %d, want %d", port.Minutes, 90-36+90)
	}
	if port.ExcludedMinutes != 36 {
		t.Errorf("port excluded minutes = %d, want 36", port.ExcludedMinutes)
	}
	if stbd.Minutes != 180 || stbd.ExcludedMinutes != 0 {
		t.Errorf("starboard is untouched by port's alarm: %+v", stbd)
	}
	if len(m.Excluded) != 1 {
		t.Fatalf("excluded = %+v", m.Excluded)
	}
	// An excluded range includes its start and not its end, so the last minute taken
	// out is the one before To.
	ex := m.Excluded[0]
	if ex.Subject != "port" || ex.Reason != "alarm" || ex.Ref != "a1" || ex.Title != "Port Engine Oil Pressure Low" ||
		!ex.From.Equal(r1.Add(40*time.Minute)) || !ex.To.Equal(cleared.Add(5*time.Minute+time.Minute)) {
		t.Errorf("excluded entry = %+v", ex)
	}
}

// An alarm still open runs to the end of the window, so it takes every minute
// after its start out of training.
// An alarm that never cleared takes out a day after it was raised and no more:
// the alarm log keeps no last-seen time, and an alarm forgotten open would
// otherwise take out everything after it for good.
func TestTrain_AnOpenAlarmTakesOutADayAfterItWasRaised(t *testing.T) {
	r1, r2 := daysAgo(5), daysAgo(3)
	feed := synthFeed("port", 1, []synthRun{{Start: r1, Minutes: 100, RPM: 1900}, {Start: r2, Minutes: 100, RPM: 1900}})
	h := newTrainHarness(feed, "port")
	h.alarms = []alarmLogEntry{{ID: "open", Label: "Port Engine Temperature High", Path: "propulsion.port.temperature", RaisedAt: r1.Add(60 * time.Minute)}}
	m := h.train(t, normalSubsystemModel{})
	// Judged minutes 10..49 of the first run survive, and so does the second run.
	if got := m.Subjects["port"].GatedMinutes; got != 130 {
		t.Fatalf("minutes left = %d, want 130", got)
	}
	if len(m.Excluded) != 1 || !m.Excluded[0].To.Equal(r1.Add(60*time.Minute+24*time.Hour+time.Minute)) {
		t.Fatalf("excluded = %+v", m.Excluded)
	}
}

func TestTrain_ABandNeedsSixtyMinutesFromTwoRuns(t *testing.T) {
	cases := []struct {
		name   string
		runs   []int
		judged bool
		reason string
	}{
		{"59 minutes from two runs", []int{30, 29}, false, "59 minutes learned, needs 60"},
		{"60 minutes from two runs", []int{30, 30}, true, ""},
		{"many minutes from one run", []int{200}, false, "200 minutes from 1 run, needs 2 runs"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var runs []synthRun
			for i, n := range c.runs {
				runs = append(runs, gatedRun(daysAgo(float64(10-3*i)), n, 1900))
			}
			m := newTrainHarness(synthFeed("port", 3, runs), "port").train(t, normalSubsystemModel{})
			r := regimeNamed(t, m, "port/rpm1800")
			if r.Judged != c.judged || r.Reason != c.reason {
				t.Fatalf("judged=%v reason=%q, want %v %q", r.Judged, r.Reason, c.judged, c.reason)
			}
			if !r.Judged && len(r.Mean) != 0 {
				t.Fatal("an unjudged band carries no figures")
			}
		})
	}
}

// A band the engine did not use in the window keeps its last normal with the
// date it was learned. Past 183 days it is dropped.
func TestTrain_ABandWithNoNewDataIsCarriedForwardUntilItExpires(t *testing.T) {
	first := newTrainHarness(synthFeed("port", 4, []synthRun{
		gatedRun(daysAgo(20), 70, 1500), gatedRun(daysAgo(18), 70, 1500),
		gatedRun(daysAgo(12), 70, 1900), gatedRun(daysAgo(10), 70, 1900),
	}), "port")
	prev := first.train(t, normalSubsystemModel{})
	old := regimeNamed(t, prev, "port/rpm1400")
	if !old.Judged {
		t.Fatalf("setup: %+v", old)
	}

	for _, c := range []struct {
		afterDays float64
		kept      bool
	}{{100, true}, {183, true}, {184, false}} {
		later := newTrainHarness(synthFeed("port", 5, []synthRun{gatedRun(daysAgo(-c.afterDays+2), 70, 1900), gatedRun(daysAgo(-c.afterDays+1), 70, 1900)}), "port")
		later.now = trainNow.Add(time.Duration(c.afterDays*24) * time.Hour)
		m := later.train(t, prev)
		r, has := m.Regimes["port/rpm1400"]
		if has != c.kept {
			t.Fatalf("%v days on: kept=%v, want %v", c.afterDays, has, c.kept)
		}
		if has {
			if !r.CarriedForward || !r.LearnedAt.Equal(old.LearnedAt) || r.Shift != nil {
				t.Errorf("a carried band keeps its learned date and has no shift: %+v", r)
			}
		}
		fresh := regimeNamed(t, m, "port/rpm1800")
		if fresh.CarriedForward || !fresh.LearnedAt.Equal(later.now) {
			t.Errorf("a refitted band is learned now: %+v", fresh)
		}
		if fresh.Shift == nil || !fresh.Shift.FromLearnedAt.Equal(regimeNamed(t, prev, "port/rpm1800").LearnedAt) {
			t.Errorf("a refit saves its shift from the previous model: %+v", fresh.Shift)
		}
	}
}

// A repaired sender: the exhaust reading starts part-way through the window. The
// band keeps its previous normal, without it, until the new fit has an hour from
// two runs, and the status says how far it has got.
func TestTrain_AReadingThatStartsWorkingJoinsAfterAnHourFromTwoRuns(t *testing.T) {
	base := []synthRun{gatedRun(daysAgo(30), 70, 1900), gatedRun(daysAgo(28), 70, 1900)}
	prev := newTrainHarness(synthFeed("port", 6, base), "port").train(t, normalSubsystemModel{})
	if strings.Contains(strings.Join(regimeNamed(t, prev, "port/rpm1800").Features, ","), "exhaustTemperature") {
		t.Fatal("setup: no exhaust sender yet")
	}

	// A day later the sender reports on a 24 minute run only.
	short := synthRun{Start: daysAgo(1), Minutes: 24, RPM: 1900, Exhaust: true}
	h := newTrainHarness(synthFeed("port", 7, append(append([]synthRun{}, base...), short)), "port")
	h.now = trainNow
	m := h.train(t, prev)
	r := regimeNamed(t, m, "port/rpm1800")
	if !r.CarriedForward || len(r.Joining) != 1 || r.Joining[0] != "exhaustTemperature" || r.JoiningMinutes != 14 {
		t.Fatalf("regime = carried %v joining %v (%d minutes)", r.CarriedForward, r.Joining, r.JoiningMinutes)
	}
	var ex normalFeatureVerdict
	for _, f := range m.Subjects["port"].Features {
		if f.Key == "exhaustTemperature" {
			ex = f
		}
	}
	if !ex.Used || !ex.Joining || ex.Status() != "joins after 60 minutes, 14 so far" {
		t.Fatalf("exhaust verdict = %+v (%q)", ex, ex.Status())
	}

	// Two more runs make an hour and the band refits with it.
	more := append(append([]synthRun{}, base...), short,
		synthRun{Start: daysAgo(0.7), Minutes: 50, RPM: 1900, Exhaust: true}, synthRun{Start: daysAgo(0.4), Minutes: 50, RPM: 1900, Exhaust: true})
	h2 := newTrainHarness(synthFeed("port", 8, more), "port")
	m2 := h2.train(t, m)
	r2 := regimeNamed(t, m2, "port/rpm1800")
	if r2.CarriedForward || !r2.Judged || r2.Features[len(r2.Features)-1] != "exhaustTemperature" || len(r2.Joining) != 0 {
		t.Fatalf("regime = %+v", r2)
	}
}

// With no earlier model to carry, a band fits on the readings it can and says
// which one is still joining.
func TestTrain_WithNothingToCarryABandFitsWithoutTheJoiningReading(t *testing.T) {
	runs := []synthRun{gatedRun(daysAgo(30), 70, 1900), gatedRun(daysAgo(28), 70, 1900), {Start: daysAgo(1), Minutes: 24, RPM: 1900, Exhaust: true}}
	m := newTrainHarness(synthFeed("port", 9, runs), "port").train(t, normalSubsystemModel{})
	r := regimeNamed(t, m, "port/rpm1800")
	if !r.Judged || r.CarriedForward || len(r.Joining) != 1 || strings.Contains(strings.Join(r.Features, ","), "exhaust") {
		t.Fatalf("regime = %+v", r)
	}
}

func TestTrain_AnIgnoredSensorIsSwitchedOffAndNotFitted(t *testing.T) {
	feed := synthFeed("port", 10, []synthRun{gatedRun(daysAgo(10), 70, 1900), gatedRun(daysAgo(6), 70, 1900)})
	h := newTrainHarness(feed, "port")
	h.ignored = map[string]bool{"propulsion.port.boostPressure": true}
	m := h.train(t, normalSubsystemModel{})
	r := regimeNamed(t, m, "port/rpm1800")
	if strings.Contains(strings.Join(r.Features, ","), "boostPressure") {
		t.Fatalf("a switched-off reading must not be fitted: %v", r.Features)
	}
	for _, f := range m.Subjects["port"].Features {
		if f.Key == "boostPressure" && (f.Used || f.Reason != "switched off") {
			t.Fatalf("verdict = %+v", f)
		}
	}
}

// Two gateways reporting rpm leave no single rpm to gate on. The engine is not
// judged, and the subject says why.
func TestTrain_AGateReadingFromTwoSourcesStopsTheEngineAndSaysWhy(t *testing.T) {
	feed := synthFeed("port", 11, []synthRun{gatedRun(daysAgo(10), 70, 1900), gatedRun(daysAgo(6), 70, 1900)})
	var dup []normalMinuteRow
	for _, r := range feed["propulsion.port.revolutions"] {
		dup = append(dup, normalMinuteRow{Source: "other.7", At: r.At, Value: r.Value})
	}
	feed["propulsion.port.revolutions"] = append(feed["propulsion.port.revolutions"], dup...)
	m := newTrainHarness(feed, "port").train(t, normalSubsystemModel{})
	if len(m.Regimes) != 0 {
		t.Fatalf("regimes = %+v", m.Regimes)
	}
	if got := m.Subjects["port"].Problem; !strings.Contains(got, "rpm") || !strings.Contains(got, "two sources report it") {
		t.Fatalf("problem = %q", got)
	}
}

func TestTrain_NoRunningMinutesIsStatedForTheSubject(t *testing.T) {
	m := newTrainHarness(map[string][]normalMinuteRow{}, "port").train(t, normalSubsystemModel{})
	if got := m.Subjects["port"].Problem; got != "no steady running minutes in the last 90 days" {
		t.Fatalf("problem = %q", got)
	}
}

func TestTrain_NoEnginesIsAnError(t *testing.T) {
	_, err := trainSubsystem(context.Background(), normalEnginesSubsystem(), newTrainHarness(nil).deps(), normalSubsystemModel{})
	if err == nil || !strings.Contains(err.Error(), "no engines are set up") {
		t.Fatalf("err = %v", err)
	}
}

func TestTrain_AnAlarmLogThatCannotBeReadStopsTraining(t *testing.T) {
	h := newTrainHarness(synthFeed("port", 12, []synthRun{gatedRun(daysAgo(10), 70, 1900)}), "port")
	h.alarmEr = errors.New("the alarm log is not open")
	if _, err := trainSubsystem(context.Background(), normalEnginesSubsystem(), h.deps(), normalSubsystemModel{}); err == nil || !strings.Contains(err.Error(), "the alarm log is not open") {
		t.Fatalf("err = %v", err)
	}
}

// --- the file-level run ------------------------------------------------------

func goodRuns() []synthRun {
	return []synthRun{gatedRun(daysAgo(10), 70, 1900), gatedRun(daysAgo(6), 70, 1900)}
}

func TestTrainNormal_SavesTheModelAndRecordsTheAttempt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anomaly-normal.json")
	// A subsystem this build does not train survives the save.
	if err := saveNormalModel(path, normalModelFile{Subsystems: map[string]normalSubsystemModel{"house": {TrainedAt: trainNow}}}); err != nil {
		t.Fatal(err)
	}
	h := newTrainHarness(synthFeed("port", 13, goodRuns()), "port")
	if err := trainNormal(context.Background(), path, h.deps()); err != nil {
		t.Fatal(err)
	}
	got, err := loadNormalModel(path)
	if err != nil {
		t.Fatal(err)
	}
	if rec := got.Subsystems[normalSubsystemEngines]; rec.State != normalStateTrained || !rec.AttemptedAt.Equal(trainNow) {
		t.Fatalf("record = %+v", rec)
	}
	if got.Subsystems[normalSubsystemEngines].judgedRegimes() != 1 {
		t.Fatalf("engines = %+v", got.Subsystems[normalSubsystemEngines])
	}
	if _, ok := got.Subsystems["house"]; !ok {
		t.Fatal("the house subsystem was dropped")
	}
}

func TestTrainNormal_ANamedBucketThatFailsKeepsTheOldModelAndRecordsWhy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anomaly-normal.json")
	h := newTrainHarness(synthFeed("port", 14, goodRuns()), "port")
	h.source = normalSource{Kind: normalSourceMinuteBucket, Bucket: "SignalK_1m"}
	if err := trainNormal(context.Background(), path, h.deps()); err != nil {
		t.Fatal(err)
	}
	good, _ := loadNormalModel(path)

	h.minErr = errors.New(`the one-minute bucket "SignalK_1m" could not be read: bucket not found`)
	h.now = trainNow.Add(24 * time.Hour)
	err := trainNormal(context.Background(), path, h.deps())
	if err == nil {
		t.Fatal("a failed read must be returned")
	}
	for _, s := range h.asked {
		if s.Kind != normalSourceMinuteBucket {
			t.Fatalf("it fell back to %+v", s)
		}
	}
	after, _ := loadNormalModel(path)
	rec := after.Subsystems[normalSubsystemEngines]
	if rec.State != normalStateFailed || !strings.Contains(rec.Problem, `the one-minute bucket "SignalK_1m" could not be read`) {
		t.Fatalf("record = %+v", rec)
	}
	if !rec.AttemptedAt.Equal(h.now) {
		t.Fatalf("attempt time = %v", rec.AttemptedAt)
	}
	if after.Subsystems[normalSubsystemEngines].TrainedAt != good.Subsystems[normalSubsystemEngines].TrainedAt || after.Subsystems[normalSubsystemEngines].judgedRegimes() != 1 {
		t.Fatalf("the good model must be left as it was: %+v", after.Subsystems[normalSubsystemEngines])
	}
}

func TestTrainNormal_ABlankMinuteBucketReadsTheMainBucket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anomaly-normal.json")
	h := newTrainHarness(synthFeed("port", 15, goodRuns()), "port")
	if err := trainNormal(context.Background(), path, h.deps()); err != nil {
		t.Fatal(err)
	}
	if len(h.asked) == 0 {
		t.Fatal("nothing was read")
	}
	for _, s := range h.asked {
		if s.Kind != normalSourceMainBucket {
			t.Fatalf("asked %+v", s)
		}
	}
	got, _ := loadNormalModel(path)
	if got.Subsystems[normalSubsystemEngines].Source.Kind != normalSourceMainBucket {
		t.Fatalf("source = %+v", got.Subsystems[normalSubsystemEngines].Source)
	}
}

func TestTrainNormal_NoInfluxIsSaidPlainly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anomaly-normal.json")
	h := newTrainHarness(nil, "port")
	h.srcErr = errNormalNoInflux
	if err := trainNormal(context.Background(), path, h.deps()); !errors.Is(err, errNormalNoInflux) {
		t.Fatalf("err = %v", err)
	}
	got, _ := loadNormalModel(path)
	for name, rec := range got.Subsystems {
		if rec.State != normalStateFailed || rec.Problem != "InfluxDB is not set up" || rec.hasModel() {
			t.Fatalf("%s: nothing was learned, and each says why: %+v", name, rec)
		}
	}
	if len(got.Subsystems) == 0 {
		t.Fatalf("every subsystem has a record: %+v", got.Subsystems)
	}
}

// A run that learns no band must not replace a model that has some.
func TestTrainNormal_AnEmptyRunDoesNotOverwriteAGoodModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anomaly-normal.json")
	h := newTrainHarness(synthFeed("port", 16, goodRuns()), "port")
	if err := trainNormal(context.Background(), path, h.deps()); err != nil {
		t.Fatal(err)
	}
	// Two hundred days on, with no data: the old bands have expired and nothing
	// new was learned.
	later := newTrainHarness(map[string][]normalMinuteRow{}, "port")
	later.now = trainNow.Add(200 * 24 * time.Hour)
	err := trainNormal(context.Background(), path, later.deps())
	if err == nil || !strings.Contains(err.Error(), "learned no band") {
		t.Fatalf("err = %v", err)
	}
	got, _ := loadNormalModel(path)
	if got.Subsystems[normalSubsystemEngines].judgedRegimes() != 1 {
		t.Fatalf("the model with a band was overwritten: %+v", got.Subsystems[normalSubsystemEngines])
	}
	if rec := got.Subsystems[normalSubsystemEngines]; rec.State != normalStateFailed {
		t.Fatalf("record = %+v", rec)
	}
}

func TestTrainNormal_ACorruptFileIsReportedAndLeftAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anomaly-normal.json")
	if err := writeNormalTestFile(path, `{"subsystems": {`); err != nil {
		t.Fatal(err)
	}
	h := newTrainHarness(synthFeed("port", 17, goodRuns()), "port")
	if err := trainNormal(context.Background(), path, h.deps()); err == nil || !strings.Contains(err.Error(), "parsing") {
		t.Fatalf("err = %v", err)
	}
	raw, _ := readNormalTestFile(path)
	if raw != `{"subsystems": {` {
		t.Fatalf("the corrupt file was overwritten: %q", raw)
	}
}

// --- reads: one query, no switched-off readings, a stoppable run --------------

// All of an engine's readings are asked for in one read, and a reading that is
// switched off is not asked for at all. The gate's own readings always are.
func TestTrain_OneReadPerEngineAndNothingForASwitchedOffReading(t *testing.T) {
	h := newTrainHarness(mergeFeeds(
		synthFeed("port", 1, goodRuns()), synthFeed("starboard", 2, goodRuns())), "port", "starboard")
	h.ignored = map[string]bool{"propulsion.port.boostPressure": true, "propulsion.port.revolutions": true}
	m := h.train(t, normalSubsystemModel{})
	if len(h.requested) != 2 {
		t.Fatalf("%d reads for two engines, want 2: %v", len(h.requested), h.requested)
	}
	has := func(paths []string, p string) bool {
		for _, x := range paths {
			if x == p {
				return true
			}
		}
		return false
	}
	port := h.requested[0]
	if has(port, "propulsion.port.boostPressure") {
		t.Errorf("a switched-off reading was read: %v", port)
	}
	if !has(port, "propulsion.port.revolutions") || !has(port, "propulsion.port.oilPressure") {
		t.Errorf("port read %v", port)
	}
	if !has(h.requested[1], "propulsion.starboard.boostPressure") {
		t.Errorf("starboard still reads boost: %v", h.requested[1])
	}
	for _, f := range m.Subjects["port"].Features {
		if f.Key == "boostPressure" && (f.Used || f.Reason != "switched off" || !f.SwitchedOff) {
			t.Errorf("the verdict is unchanged by not reading it: %+v", f)
		}
	}
}

func TestTrain_AStoppedContextEndsTheRunAndSavesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anomaly-normal.json")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h := newTrainHarness(synthFeed("port", 1, goodRuns()), "port")
	if err := trainNormal(ctx, path, h.deps()); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	got, _ := loadNormalModel(path)
	if _, has := got.Subsystems[normalSubsystemEngines]; has {
		t.Fatal("a stopped run saved a model")
	}
}

func TestTrainNormal_TheAttemptRecordsWhereItRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anomaly-normal.json")
	h := newTrainHarness(synthFeed("port", 1, goodRuns()), "port")
	h.source = normalSource{Kind: normalSourceMinuteBucket, Bucket: "b"}
	if err := trainNormal(context.Background(), path, h.deps()); err != nil {
		t.Fatal(err)
	}
	got, _ := loadNormalModel(path)
	if got.Subsystems[normalSubsystemEngines].Source != h.source {
		t.Fatalf("record = %+v", got.Subsystems[normalSubsystemEngines])
	}
}

func TestNormalStartDelay(t *testing.T) {
	src := normalSource{Kind: normalSourceMainBucket}
	at := trainNow.Add(-6 * time.Hour)
	record := func(state string, source normalSource) normalModelFile {
		file := normalModelFile{Subsystems: map[string]normalSubsystemModel{}}
		for _, sub := range normalSubsystems() {
			file.Subsystems[sub.Name] = normalSubsystemModel{State: state, AttemptedAt: at, Source: source}
		}
		return file
	}
	ok := record(normalStateTrained, src)
	if got := normalStartDelay(ok, trainNow, src, 24*time.Hour); got != 18*time.Hour {
		t.Errorf("a run 6 hours ago waits out the other 18, got %v", got)
	}
	if got := normalStartDelay(ok, trainNow.Add(30*time.Hour), src, 24*time.Hour); got != 0 {
		t.Errorf("a run over a day ago runs now, got %v", got)
	}
	if got := normalStartDelay(ok, trainNow, normalSource{Kind: normalSourceMinuteBucket, Bucket: "b"}, 24*time.Hour); got != 0 {
		t.Errorf("a changed source runs now, got %v", got)
	}
	// One schedule: a subsystem that is due, or failed, trains them all.
	mixed := record(normalStateTrained, src)
	mixed.Subsystems[normalSubsystemElectrical] = normalSubsystemModel{State: normalStateFailed, AttemptedAt: at}
	if got := normalStartDelay(mixed, trainNow, src, 24*time.Hour); got != 0 {
		t.Errorf("a failed subsystem is tried again at start, got %v", got)
	}
	if got := normalStartDelay(record(normalStateSkipped, normalSource{}), trainNow, src, 24*time.Hour); got != 18*time.Hour {
		t.Errorf("a skipped subsystem read nothing, so no source changed, got %v", got)
	}
	if got := normalStartDelay(normalModelFile{}, trainNow, src, 24*time.Hour); got != 0 {
		t.Errorf("no run yet runs now, got %v", got)
	}
}

// A burst of wakes is one run: the wait restarts with each and ends once they stop.
func TestWaitQuiet_ABurstOfWakesIsOneWait(t *testing.T) {
	wake := make(chan struct{}, 1)
	done := make(chan bool, 1)
	start := time.Now()
	go func() { done <- waitQuiet(context.Background(), wake, 80*time.Millisecond) }()
	for i := 0; i < 4; i++ {
		time.Sleep(30 * time.Millisecond)
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	if ok := <-done; !ok {
		t.Fatal("waitQuiet gave up")
	}
	if elapsed := time.Since(start); elapsed < 190*time.Millisecond {
		t.Fatalf("returned after %v: each wake must restart the wait", elapsed)
	}
}

func TestWaitQuiet_AStoppedContextEndsTheWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if waitQuiet(ctx, make(chan struct{}), time.Hour) {
		t.Fatal("a stopped context must not report quiet")
	}
}

func TestNormalPathMatters_AskedOfTheSubsystems(t *testing.T) {
	withVessel(t, houseVessel())
	for path, want := range map[string]bool{
		"propulsion.port.oilPressure":        true,
		"propulsion.0.exhaustTemperature":    true,
		"electrical.batteries.1.voltage":     true,
		"electrical.batteries.512.voltage":   false,
		"electrical.alternator.p.current":    true,
		"electrical.chargers.sc.current":     true,
		"navigation.speedOverGround":         false,
		"helmcentral.anomaly.engines.port.x": false,
		"propulsion":                         false,
	} {
		if got := normalPathMatters(path); got != want {
			t.Errorf("normalPathMatters(%q) = %v, want %v", path, got, want)
		}
	}
}
