package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Second review pass: one rule for switched-off readings, one pinned sender per
// reading, and a handful of smaller fixes.

func notJudgedOfKind(res unusualResult, kind string) []unusualNotJudged {
	var out []unusualNotJudged
	for _, n := range res.NotJudged {
		if n.Kind == kind {
			out = append(out, n)
		}
	}
	return out
}

// --- A. switched-off readings ---------------------------------------------------

// A reading switched off after the last training run is never judged or named,
// whatever the saved model holds. Every band that uses it says why it was not
// judged.
func TestScore_AReadingSwitchedOffAfterTrainingIsNeverJudgedOrNamed(t *testing.T) {
	feed := steadyScoreFeed(90)
	bend(feed, "propulsion.port.boostPressure", 40, 90, func(v float64) float64 { return v * 3 })
	c := newScoreCase(trainedPort(t), feed)
	c.ignored = map[string]bool{"propulsion.port.boostPressure": true}
	res := c.score(24 * time.Hour)
	if len(res.Stretches) != 0 || res.JudgedMinutes != 0 {
		t.Fatalf("stretches %+v, judged %d: a switched-off reading must not be judged", res.Stretches, res.JudgedMinutes)
	}
	off := notJudgedOfKind(res, "switched_off")
	if len(off) == 0 || off[0].Reason != "boost pressure is switched off; relearning at the next training run" {
		t.Fatalf("not judged = %+v", res.NotJudged)
	}
	for _, p := range c.paths {
		if strings.HasSuffix(p, "boostPressure") {
			t.Fatalf("a switched-off reading must not be read: %v", c.paths)
		}
	}
}

// An engine whose training left a problem is reported with that problem. It is
// never "no history yet".
func TestScore_AnEnginesTrainingProblemIsWhatItIsReportedWith(t *testing.T) {
	model := trainedPort(t)
	eng := model.Subsystems[normalSubsystemEngines]
	eng.Regimes = map[string]normalRegime{}
	sm := eng.Subjects["port"]
	sm.Problem = "coolant temperature is switched off, so this engine cannot be judged"
	eng.Subjects["port"] = sm
	model.Subsystems[normalSubsystemEngines] = eng
	res := newScoreCase(model, steadyScoreFeed(91)).score(24 * time.Hour)
	if len(notJudgedOfKind(res, "thin_band")) != 0 {
		t.Fatalf("an engine with a problem is not a thin band: %+v", res.NotJudged)
	}
	got := notJudgedOfKind(res, "engine_problem")
	if len(got) != 1 || got[0].Reason != sm.Problem || got[0].SubjectName != "Port engine" {
		t.Fatalf("not judged = %+v", res.NotJudged)
	}
}

// A reading the gate needs, switched off since training, stops the engine at
// scoring too, naming the reading.
func TestScore_AGateReadingSwitchedOffAfterTrainingStopsTheEngine(t *testing.T) {
	c := newScoreCase(trainedPort(t), steadyScoreFeed(92))
	c.ignored = map[string]bool{"propulsion.port.temperature": true}
	res := c.score(24 * time.Hour)
	got := notJudgedOfKind(res, "engine_problem")
	if res.JudgedMinutes != 0 || len(got) != 1 || got[0].Reason != "coolant temperature is switched off, so this engine cannot be judged" {
		t.Fatalf("judged %d, not judged = %+v", res.JudgedMinutes, res.NotJudged)
	}
}

// --- B. one pinned sender per reading ----------------------------------------------

// retag gives the rows of one path from the given time on another sender.
func retag(feed map[string][]normalMinuteRow, path, source string, from time.Time) {
	for i, r := range feed[path] {
		if !r.At.Before(from) {
			feed[path][i].Source = source
		}
	}
}

func verdictFor(t *testing.T, m normalSubsystemModel, subject, key string) normalFeatureVerdict {
	t.Helper()
	for _, f := range m.Subjects[subject].Features {
		if f.Key == key {
			return f
		}
	}
	t.Fatalf("no verdict for %s in %+v", key, m.Subjects[subject].Features)
	return normalFeatureVerdict{}
}

func handoverRuns(lastRunMinutes int) []synthRun {
	return []synthRun{gatedRun(daysAgo(10), 70, 1900), gatedRun(daysAgo(6), 70, 1900), {Start: daysAgo(1), Minutes: lastRunMinutes, RPM: 1900}}
}

// A reading handed to a new sender with under an hour of history is relearning.
// The bands are fitted without it and the verdict says so.
func TestTrain_ASenderWithTooLittleHistoryIsRelearningAndLeftOut(t *testing.T) {
	feed := synthFeed("port", 93, handoverRuns(40))
	retag(feed, "propulsion.port.oilPressure", "new.sender", daysAgo(2))
	m := newTrainHarness(feed, "port").train(t, normalSubsystemModel{})
	v := verdictFor(t, m, "port", "oilPressure")
	if v.Used || v.Reason != "sender changed, relearning" || v.Source != "new.sender" {
		t.Fatalf("verdict = %+v", v)
	}
	r := regimeNamed(t, m, "port/rpm1800")
	if !r.Judged || strings.Contains(strings.Join(r.Features, ","), "oilPressure") {
		t.Fatalf("regime = %+v", r)
	}
}

// Once the latest sender has an hour, it is the reading's sender and is saved
// on the verdict.
func TestTrain_TheLatestSenderIsPinnedOnTheVerdictOnceItHasAnHour(t *testing.T) {
	feed := synthFeed("port", 94, handoverRuns(80))
	retag(feed, "propulsion.port.oilPressure", "new.sender", daysAgo(2))
	m := newTrainHarness(feed, "port").train(t, normalSubsystemModel{})
	v := verdictFor(t, m, "port", "oilPressure")
	if !v.Used || v.Source != "new.sender" {
		t.Fatalf("verdict = %+v", v)
	}
}

// A reading the gate needs that changed sender and is still relearning leaves
// the engine with a problem that names it.
func TestTrain_ARequiredReadingRelearningItsSenderStopsTheEngineAndSaysWhich(t *testing.T) {
	feed := synthFeed("port", 95, handoverRuns(40))
	retag(feed, "propulsion.port.revolutions", "new.sender", daysAgo(2))
	m := newTrainHarness(feed, "port").train(t, normalSubsystemModel{})
	if got := m.Subjects["port"].Problem; got != "rpm: sender changed, relearning" {
		t.Fatalf("problem = %q", got)
	}
}

// Scoring reads only the sender the training pinned. Another sender with more
// minutes is not taken instead.
func TestScore_OnlyThePinnedSenderIsRead(t *testing.T) {
	feed := steadyScoreFeed(96)
	path := "propulsion.port.oilPressure"
	for i := -30; i < 130; i++ {
		feed[path] = append(feed[path], normalMinuteRow{Path: path, Source: "ghost", At: scoreRunStart.Add(time.Duration(i) * time.Minute), Value: 900000})
	}
	res := newScoreCase(trainedPort(t), feed).score(24 * time.Hour)
	if len(res.Stretches) != 0 || res.JudgedMinutes == 0 {
		t.Fatalf("judged %d, stretches %+v: the ghost sender must not be read", res.JudgedMinutes, res.Stretches)
	}
}

// A pinned sender that is silent leaves the minutes unjudged. The other sender's
// figures are not used in its place, and the quiet sender is said (see
// TestScore_APinnedSenderThatHasGoneQuietIsSaidAndAsksForTraining).
func TestScore_AFallenSilentPinnedSenderIsNotReplaced(t *testing.T) {
	feed := steadyScoreFeed(97)
	retag(feed, "propulsion.port.oilPressure", "other.sender", scoreRunStart.Add(-time.Hour))
	res := newScoreCase(trainedPort(t), feed).score(24 * time.Hour)
	if res.JudgedMinutes != 0 || len(notJudgedOfKind(res, "sender_silent")) == 0 {
		t.Fatalf("judged %d, not judged = %+v", res.JudgedMinutes, res.NotJudged)
	}
}

// --- C. no InfluxDB ----------------------------------------------------------------

func TestGatherWatchReportInput_NoInfluxLeavesTheUnusualBlockOut(t *testing.T) {
	for _, tc := range []struct {
		name    string
		res     unusualResult
		wantNil bool
	}{
		{"not set up", unusualResult{ProblemKind: unusualNoInflux, Problem: "InfluxDB is not set up"}, true},
		{"set up but failing", unusualResult{ProblemKind: unusualError, Problem: "the main bucket could not be read"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newProposalEnv(t)
			d := statusTestDeps(t)
			d.alarmLog = func() *alarmLogStore { return nil }
			d.unusualReadings = func(context.Context, time.Time, time.Time, string) unusualResult { return tc.res }
			w := savedWatch{ID: "w1", Name: "Morning", Kind: watchKindScheduled, Sections: []string{"instruments"}, Thresholds: defaultWatchThresholds()}
			in, err := gatherWatchReportInput(context.Background(), d, env.docs, w, vesselStatusTestNow, time.UTC)
			if err != nil {
				t.Fatal(err)
			}
			if (in.Unusual == nil) != tc.wantNil {
				t.Fatalf("Unusual = %+v, want nil %v", in.Unusual, tc.wantNil)
			}
		})
	}
}

// --- D. what wakes the trainer -------------------------------------------------------

func TestNormalInputsChangedBySave_RolesNoLongerWakeTheTrainer(t *testing.T) {
	base := settingsPayload{}
	withRoles := base
	withRoles.Roles = &vesselRoles{HouseBank: "a"}
	otherRoles := base
	otherRoles.Roles = &vesselRoles{HouseBank: "b"}
	if normalInputsChangedBySave(base, withRoles) || normalInputsChangedBySave(withRoles, otherRoles) {
		t.Error("a house bank or alternator is not an engine: a role change wakes nothing")
	}
	influx := base
	influx.Influxdb.URL = "http://x"
	if !normalInputsChangedBySave(base, influx) {
		t.Error("the InfluxDB block still wakes the trainer")
	}
}

func saveModelWithEngines(t *testing.T, path string, ids ...string) {
	t.Helper()
	sm := normalSubsystemModel{Subjects: map[string]normalSubjectModel{}, Regimes: map[string]normalRegime{}}
	for _, id := range ids {
		sm.Subjects[id] = normalSubjectModel{Name: id}
	}
	if err := saveNormalModel(path, normalModelFile{Subsystems: map[string]normalSubsystemModel{normalSubsystemEngines: sm}}); err != nil {
		t.Fatal(err)
	}
}

func TestReseedAfterAnItemSave_WakesTheTrainerOnlyWhenTheEnginesChange(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	modelPath := filepath.Join(t.TempDir(), "anomaly-normal.json")
	t.Setenv("ANOMALY_NORMAL_FILE", modelPath)
	withTempAlarmRules(t)
	store := vesselFixtureStore(t)

	port, err := store.CreateEquipment(equipmentItem{Name: "Port engine", System: "propulsion"})
	if err != nil {
		t.Fatal(err)
	}
	port.SignalKSource = "propulsion.port"
	if _, err := store.UpdateEquipment(port.ID, port); err != nil {
		t.Fatal(err)
	}

	saveModelWithEngines(t, modelPath, "port")
	drainNormalWake()
	reseedAnomalyRulesAfterItemSave(port.ID)
	if woke() {
		t.Fatal("the engines are the ones the model was learned from: nothing to hear")
	}

	// An engine added.
	stbd, err := store.CreateEquipment(equipmentItem{Name: "Starboard engine", System: "propulsion"})
	if err != nil {
		t.Fatal(err)
	}
	stbd.SignalKSource = "propulsion.starboard"
	if _, err := store.UpdateEquipment(stbd.ID, stbd); err != nil {
		t.Fatal(err)
	}
	reseedAnomalyRulesAfterItemSave(stbd.ID)
	if !woke() {
		t.Fatal("a new engine must wake the trainer")
	}

	// An engine removed since the model was learned.
	saveModelWithEngines(t, modelPath, "port", "starboard", "centre")
	reseedAnomalyRulesAfterItemSave(stbd.ID)
	if !woke() {
		t.Fatal("a removed engine must wake the trainer")
	}

	// An engine re-instanced.
	saveModelWithEngines(t, modelPath, "port", "stbd")
	reseedAnomalyRulesAfterItemSave(stbd.ID)
	if !woke() {
		t.Fatal("a re-instanced engine must wake the trainer")
	}

	// Saving an item that is not an engine, with the engines as they were learned.
	saveModelWithEngines(t, modelPath, "port", "starboard")
	bank, err := store.CreateEquipment(equipmentItem{Name: "House bank", System: "electrical"})
	if err != nil {
		t.Fatal(err)
	}
	reseedAnomalyRulesAfterItemSave(bank.ID)
	if woke() {
		t.Fatal("a non-engine item changes nothing the normal learns from")
	}
}

// --- E. the seven-day window ---------------------------------------------------------

func TestBuildWatchUnusual_AClippedWindowSaysOnlyTheLastSevenDaysWereJudged(t *testing.T) {
	res := unusualResult{Since: vesselStatusTestNow.Add(-7 * 24 * time.Hour), Until: vesselStatusTestNow, Clipped: true, JudgedMinutes: 300}
	u := buildWatchUnusual(res, "metric", time.UTC, vesselStatusTestNow)
	if u == nil || !strings.Contains(u.Note, "Only the last 7 days were judged.") {
		t.Fatalf("block = %+v", u)
	}
}

// An engine that could not be judged is said even when the other engine was.
func TestBuildWatchUnusual_AnEngineWithAProblemIsSaidBesideOneThatWasJudged(t *testing.T) {
	res := unusualResult{JudgedMinutes: 300, NotJudged: []unusualNotJudged{
		{Kind: "engine_problem", Subject: "port", SubjectName: "Port", Reason: "coolant temperature is switched off, so this engine cannot be judged"},
	}}
	u := buildWatchUnusual(res, "metric", time.UTC, vesselStatusTestNow)
	if u == nil || !strings.Contains(u.Note, "Port: coolant temperature is switched off, so this engine cannot be judged.") {
		t.Fatalf("block = %+v", u)
	}
}

// --- F. settings validation ----------------------------------------------------------

// The organization never enters a Flux literal, so it is not refused. A value
// already stored does not block an unrelated save.
func TestValidateSettingsChange_OrgIsFreeAndAStoredBucketDoesNotBlockOtherSaves(t *testing.T) {
	cur := settingsPayload{}
	cur.Influxdb.Org = "o{x"
	cur.Influxdb.Bucket = "b${stored}"
	cur.Influxdb.MinuteBucket = "m${stored}"
	next := cur
	next.Anchor.ChainOnboardM = 55
	if err := validateSettingsChange(cur, next); err != nil {
		t.Fatalf("an unrelated save was refused: %+v", err)
	}
	changed := cur
	changed.Influxdb.Bucket = "b${new}"
	if err := validateSettingsChange(cur, changed); err == nil || err.Field != "influxdb.bucket" {
		t.Fatalf("a changed bucket with ${ must still be refused: %+v", err)
	}
	newOrg := cur
	newOrg.Influxdb.Org = "o${x}"
	if err := validateSettingsChange(cur, newOrg); err != nil {
		t.Fatalf("the organization is not a Flux literal: %+v", err)
	}
}

// --- G. one resolution per run --------------------------------------------------------

// A secrets store that cannot be read is said as that. It is not "InfluxDB is
// not set up".
func TestQueryNormalMinutes_ASecretsReadFailureIsNotReportedAsNotSetUp(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	settings, err := readSettings(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	settings["influxdb"] = map[string]any{"enabled": true, "url": "http://192.168.50.20:8086", "org": "o", "bucket": "b"}
	if err := writeSettings(settingsPath, settings); err != nil {
		t.Fatal(err)
	}
	prev := globalSecretsStore
	globalSecretsStore = nil
	defer func() { globalSecretsStore = prev }()

	_, err = queryNormalMinutes(context.Background(), settingsPath, normalSource{Kind: normalSourceMainBucket}, []string{"propulsion.port.revolutions"}, trainNow.Add(-time.Hour), trainNow)
	if err == nil || errors.Is(err, errNormalNoInflux) || !strings.Contains(err.Error(), "secrets store") {
		t.Fatalf("err = %v", err)
	}
}

// The connection is resolved once for a run, whatever number of chunks it has.
func TestQueryNormalMinutes_ResolvesTheConnectionOncePerRun(t *testing.T) {
	opens, runs := 0, 0
	oldOpen, oldRun := openNormalInflux, runNormalMinutesQuery
	defer func() { openNormalInflux, runNormalMinutesQuery = oldOpen, oldRun }()
	openNormalInflux = func(string) (normalInflux, error) { opens++; return normalInflux{org: "o"}, nil }
	runNormalMinutesQuery = func(_ context.Context, in normalInflux, _ normalSource, _ []string, _, _ time.Time) ([]normalMinuteRow, error) {
		runs++
		if in.org != "o" {
			t.Errorf("a chunk was not given the resolved connection: %+v", in)
		}
		return nil, nil
	}
	if _, err := queryNormalMinutes(context.Background(), "settings.yaml", normalSource{Kind: normalSourceMainBucket}, []string{"p"}, trainNow.Add(-5*24*time.Hour), trainNow); err != nil {
		t.Fatal(err)
	}
	if opens != 1 || runs != 5 {
		t.Fatalf("opened %d times for %d chunks", opens, runs)
	}
}

// --- H. each minute once -----------------------------------------------------------

func TestScore_AMinuteMissingTwoReadingsIsCountedOnce(t *testing.T) {
	feed := steadyScoreFeed(98)
	drop := func(path string) {
		var kept []normalMinuteRow
		for _, r := range feed[path] {
			if off := int(r.At.Sub(scoreRunStart) / time.Minute); off >= 60 && off < 80 {
				continue
			}
			kept = append(kept, r)
		}
		feed[path] = kept
	}
	drop("propulsion.port.oilPressure")
	drop("propulsion.port.boostPressure")
	res := newScoreCase(trainedPort(t), feed).score(24 * time.Hour)
	missing := notJudgedOfKind(res, "missing_reading")
	total := 0
	for _, n := range missing {
		total += n.Minutes
	}
	if total != 20 || len(missing) != 1 || !strings.Contains(missing[0].Reason, "oil pressure") || !strings.Contains(missing[0].Reason, "boost pressure") {
		t.Fatalf("missing = %+v", missing)
	}
}
