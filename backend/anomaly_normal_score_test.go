package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- a trained model to score against -----------------------------------------

func bandRuns(rpmFrom, rpmTo float64) []synthRun {
	return []synthRun{
		{Start: daysAgo(12), Minutes: 100, RPM: rpmFrom, RPMEnd: rpmTo},
		{Start: daysAgo(9), Minutes: 100, RPM: rpmFrom, RPMEnd: rpmTo},
		{Start: daysAgo(6), Minutes: 100, RPM: rpmFrom, RPMEnd: rpmTo},
	}
}

// trainedPort trains one engine, "port", in the 1,800 and 2,000 rpm bands, each
// with rpm spread across the band as a real engine's is.
func trainedPort(t *testing.T) normalModelFile {
	t.Helper()
	runs := append(bandRuns(1810, 1990), bandRuns(2010, 2190)...)
	for i := range runs {
		runs[i].Start = runs[i].Start.Add(time.Duration(i) * 7 * time.Hour)
	}
	model := newTrainHarness(synthFeed("port", 21, runs), "port").train(t, normalSubsystemModel{})
	for _, key := range []string{"port/rpm1800", "port/rpm2000"} {
		if !model.Regimes[key].Judged {
			t.Fatalf("setup: %s = %+v", key, model.Regimes[key])
		}
	}
	return normalModelFile{Subsystems: map[string]normalSubsystemModel{normalSubsystemEngines: model}}
}

var scoreRunStart = trainNow.Add(-6 * time.Hour)

type scoreCase struct {
	feed    map[string][]normalMinuteRow
	model   normalModelFile
	modelEr error
	src     normalSource
	srcErr  error
	minErr  error
	engines []string
	asked   int
	paths   []string        // the readings asked for in the last read
	reads   [][]string      // the readings asked for in every read
	mu      sync.Mutex      // subsystems are scored together
	since   time.Time       // the start of the last read
	ctx     context.Context // the context of the last read
	ignored map[string]bool // the ignored-sensor list at score time
	wakes   int             // how often scoring asked for a training run
	answers []normalAnswer  // the operator's answers about stretches
	// vessel, when set, is the whole vessel, in place of the engines above.
	vessel *vesselView
}

func newScoreCase(model normalModelFile, feed map[string][]normalMinuteRow, engines ...string) *scoreCase {
	if len(engines) == 0 {
		engines = []string{"port"}
	}
	return &scoreCase{feed: feed, model: model, src: normalSource{Kind: normalSourceMainBucket}, engines: engines}
}

func (c *scoreCase) deps() normalScoreDeps {
	return normalScoreDeps{
		Now:    func() time.Time { return trainNow },
		Source: func() (normalSource, error) { return c.src, c.srcErr },
		Minutes: func(ctx context.Context, src normalSource, paths []string, start, stop time.Time) (map[string][]normalMinuteRow, error) {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.asked++
			c.paths = paths
			c.reads = append(c.reads, paths)
			c.since = start
			c.ctx = ctx
			if c.minErr != nil {
				return nil, c.minErr
			}
			return minutesFrom(c.feed, paths, start, stop), nil
		},
		Vessel: func() (vesselView, error) {
			if c.vessel != nil {
				return *c.vessel, nil
			}
			v := vesselView{}
			for _, e := range c.engines {
				v.Engines = append(v.Engines, vesselEngineSetting{Instance: e, Name: strings.ToUpper(e[:1]) + e[1:]})
			}
			return v, nil
		},
		Model:   func() (normalModelFile, error) { return c.model, c.modelEr },
		Ignored: func() map[string]bool { return c.ignored },
		Wake:    func() { c.wakes++ },
	}
}

func (c *scoreCase) score(since time.Duration) unusualResult {
	return scoreOnly(context.Background(), normalEnginesSubsystem(), c.deps(), trainNow.Add(-since), trainNow, "", c.answers)
}

// bend changes one reading over minutes [from, to) of the run.
func bend(feed map[string][]normalMinuteRow, path string, from, to int, fn func(float64) float64) {
	for i, r := range feed[path] {
		off := int(r.At.Sub(scoreRunStart) / time.Minute)
		if off >= from && off < to {
			feed[path][i].Value = fn(r.Value)
		}
	}
}

func halve(v float64) float64 { return v * 0.5 }

func steadyScoreFeed(seed int64) map[string][]normalMinuteRow {
	return synthFeed("port", seed, []synthRun{{Start: scoreRunStart, Minutes: 120, RPM: 1900}})
}

// --- tests -------------------------------------------------------------------

func TestScore_AnEngineRunningNormallyHasNoStretch(t *testing.T) {
	res := newScoreCase(trainedPort(t), steadyScoreFeed(31)).score(24 * time.Hour)
	if res.Problem != "" {
		t.Fatalf("problem = %q", res.Problem)
	}
	if res.JudgedMinutes != 110 {
		t.Fatalf("judged %d minutes, want the 110 after the first ten", res.JudgedMinutes)
	}
	if len(res.Stretches) != 0 {
		t.Fatalf("stretches = %+v", res.Stretches)
	}
}

func TestScore_AnOilPressureDropIsAStretchNamingOilPressure(t *testing.T) {
	feed := steadyScoreFeed(32)
	bend(feed, "propulsion.port.oilPressure", 40, 70, halve)
	res := newScoreCase(trainedPort(t), feed).score(24 * time.Hour)
	if len(res.Stretches) != 1 {
		t.Fatalf("stretches = %+v", res.Stretches)
	}
	s := res.Stretches[0]
	wantStart := scoreRunStart.Add(40 * time.Minute)
	if !s.Start.Equal(wantStart) || s.Minutes != 30 || !s.End.Equal(wantStart.Add(30*time.Minute)) {
		t.Fatalf("stretch = %+v", s)
	}
	if s.ID != "engines:port@"+wantStart.Format(time.RFC3339) {
		t.Fatalf("id = %q", s.ID)
	}
	if s.Top.Key != "oilPressure" || s.Top.Direction != "low" || s.Top.SIUnit != "Pa" {
		t.Fatalf("top = %+v", s.Top)
	}
	if s.Top.Usual < 290000 || s.Top.Usual > 320000 || s.Top.StretchMean > 0.55*s.Top.Usual {
		t.Fatalf("usual %v, stretch mean %v: the drop was to half", s.Top.Usual, s.Top.StretchMean)
	}
	if s.Band != "1,800 to 2,000 rpm" || s.Subject != "port" || s.SubjectName != "Port engine" || s.PeakRatio <= 1 {
		t.Fatalf("stretch = %+v", s)
	}
	if res.JudgedMinutes != 110 {
		t.Fatalf("judged = %d", res.JudgedMinutes)
	}
}

func TestScore_TwoUnusualMinutesAreNotAStretchAndThreeAre(t *testing.T) {
	for _, c := range []struct {
		to   int
		want int
	}{{42, 0}, {43, 1}} {
		feed := steadyScoreFeed(33)
		bend(feed, "propulsion.port.oilPressure", 40, c.to, halve)
		res := newScoreCase(trainedPort(t), feed).score(24 * time.Hour)
		if len(res.Stretches) != c.want {
			t.Errorf("minutes 40 to %d: %d stretches, want %d", c.to-1, len(res.Stretches), c.want)
		}
	}
}

func TestScore_AStretchMayCrossABandAndNamesTheBandMostOfItWasIn(t *testing.T) {
	feed := synthFeed("port", 34, []synthRun{{Start: scoreRunStart, Minutes: 120, RPM: 1950, RPMEnd: 2070}})
	bend(feed, "propulsion.port.oilPressure", 40, 80, halve) // rpm 1950+40..80 = 1990..2030 crosses 2000
	res := newScoreCase(trainedPort(t), feed).score(24 * time.Hour)
	if len(res.Stretches) != 1 {
		t.Fatalf("a stretch that crosses a band stays one: %+v", res.Stretches)
	}
	s := res.Stretches[0]
	if s.Minutes != 40 || s.Top.Key != "oilPressure" {
		t.Fatalf("stretch = %+v", s)
	}
	if s.Band != "2,000 to 2,200 rpm" {
		t.Fatalf("band = %q, want the band holding 30 of the 40 minutes", s.Band)
	}
}

func TestScore_StretchesAreRankedByPeakThenLength(t *testing.T) {
	feed := steadyScoreFeed(35)
	bend(feed, "propulsion.port.oilPressure", 30, 40, func(v float64) float64 { return v * 0.2 })  // short, extreme
	bend(feed, "propulsion.port.oilPressure", 70, 100, func(v float64) float64 { return v * 0.7 }) // long, mild: 90 kPa, nine times the smallest change worth a look
	res := newScoreCase(trainedPort(t), feed).score(24 * time.Hour)
	if len(res.Stretches) != 2 {
		t.Fatalf("stretches = %+v", res.Stretches)
	}
	if res.Stretches[0].Minutes != 10 || res.Stretches[1].Minutes != 30 || res.Stretches[0].PeakRatio <= res.Stretches[1].PeakRatio {
		t.Fatalf("order = %+v", res.Stretches)
	}
}

func TestRankStretches_TiesGoToTheLongerThenTheEarlier(t *testing.T) {
	at := trainNow
	got := []unusualStretch{
		{ID: "c", Start: at.Add(2 * time.Hour), Minutes: 5, PeakRatio: 2},
		{ID: "b", Start: at.Add(time.Hour), Minutes: 9, PeakRatio: 2},
		{ID: "a", Start: at, Minutes: 5, PeakRatio: 2},
		{ID: "top", Start: at, Minutes: 3, PeakRatio: 3},
	}
	rankStretches(got)
	order := got[0].ID + got[1].ID + got[2].ID + got[3].ID
	if order != "topbac" {
		t.Fatalf("order = %s, want topbac", order)
	}
}

func TestScore_MinutesBeforeTheGateAreCountedAsNotRunningSteadily(t *testing.T) {
	res := newScoreCase(trainedPort(t), steadyScoreFeed(36)).score(24 * time.Hour)
	var steady *unusualNotJudged
	for i := range res.NotJudged {
		if res.NotJudged[i].Kind == "not_steady" {
			steady = &res.NotJudged[i]
		}
	}
	if steady == nil || steady.Minutes != 10 || steady.Reason != "not running steadily" {
		t.Fatalf("not judged = %+v", res.NotJudged)
	}
}

// The gate asks ten minutes of running, so a window that starts mid-run needs
// the minutes before it. Scoring reads a lead-in and judges only the window.
func TestScore_AWindowThatStartsMidRunIsJudgedFromItsFirstMinute(t *testing.T) {
	c := newScoreCase(trainedPort(t), steadyScoreFeed(37))
	res := scoreOnly(context.Background(), normalEnginesSubsystem(), c.deps(), scoreRunStart.Add(60*time.Minute), trainNow, "", nil)
	if res.JudgedMinutes != 60 {
		t.Fatalf("judged %d, want the whole 60 minute window", res.JudgedMinutes)
	}
}

// A minute missing a reading of its regime is not judged and not filled in.
func TestScore_AMinuteMissingAReadingIsCountedAndBreaksTheStretch(t *testing.T) {
	feed := steadyScoreFeed(38)
	var kept []normalMinuteRow
	for _, r := range feed["propulsion.port.oilPressure"] {
		off := int(r.At.Sub(scoreRunStart) / time.Minute)
		if off >= 55 && off < 60 {
			continue
		}
		kept = append(kept, r)
	}
	feed["propulsion.port.oilPressure"] = kept
	bend(feed, "propulsion.port.boostPressure", 40, 80, func(v float64) float64 { return v * 0.3 })
	res := newScoreCase(trainedPort(t), feed).score(24 * time.Hour)

	var missing *unusualNotJudged
	for i := range res.NotJudged {
		if res.NotJudged[i].Kind == "missing_reading" {
			missing = &res.NotJudged[i]
		}
	}
	if missing == nil || missing.Minutes != 5 || missing.Reason != "no oil pressure reading" {
		t.Fatalf("not judged = %+v", res.NotJudged)
	}
	if res.JudgedMinutes != 105 {
		t.Fatalf("judged = %d, want 110 less the five", res.JudgedMinutes)
	}
	if len(res.Stretches) != 2 || res.Stretches[0].Top.Key != "boostPressure" {
		t.Fatalf("the gap splits the fault into two stretches: %+v", res.Stretches)
	}
}

func TestScore_ABandWithTooLittleHistoryIsCountedWithWhatWasLearned(t *testing.T) {
	model := trainedPort(t)
	eng := model.Subsystems[normalSubsystemEngines]
	eng.Regimes["port/rpm1800"] = normalRegime{Subject: "port", Label: "1,800 to 2,000 rpm", Minutes: 22, Runs: 1, Judged: false, Reason: "22 minutes learned, needs 60"}
	model.Subsystems[normalSubsystemEngines] = eng
	res := newScoreCase(model, steadyScoreFeed(39)).score(24 * time.Hour)
	if res.JudgedMinutes != 0 {
		t.Fatalf("judged = %d", res.JudgedMinutes)
	}
	var thin *unusualNotJudged
	for i := range res.NotJudged {
		if res.NotJudged[i].Kind == "thin_band" {
			thin = &res.NotJudged[i]
		}
	}
	if thin == nil || thin.Minutes != 110 || thin.Label != "1,800 to 2,000 rpm" ||
		thin.Reason != "not enough history for 1,800 to 2,000 rpm: 22 minutes learned, needs 60" {
		t.Fatalf("not judged = %+v", res.NotJudged)
	}
}

func TestScore_ABandNeverLearnedSaysNothingWasLearned(t *testing.T) {
	model := trainedPort(t)
	eng := model.Subsystems[normalSubsystemEngines]
	delete(eng.Regimes, "port/rpm1800")
	model.Subsystems[normalSubsystemEngines] = eng
	res := newScoreCase(model, steadyScoreFeed(40)).score(24 * time.Hour)
	var thin *unusualNotJudged
	for i := range res.NotJudged {
		if res.NotJudged[i].Kind == "thin_band" {
			thin = &res.NotJudged[i]
		}
	}
	if thin == nil || thin.Reason != "no history yet for 1,800 to 2,000 rpm" {
		t.Fatalf("not judged = %+v", res.NotJudged)
	}
}

func TestScore_OnlyTheNamedEngineIsRead(t *testing.T) {
	feed := mergeFeeds(synthFeed("port", 41, []synthRun{{Start: scoreRunStart, Minutes: 120, RPM: 1900}}),
		synthFeed("starboard", 42, []synthRun{{Start: scoreRunStart, Minutes: 120, RPM: 1900}}))
	c := newScoreCase(trainedPort(t), feed, "port", "starboard")
	res := scoreOnly(context.Background(), normalEnginesSubsystem(), c.deps(), trainNow.Add(-24*time.Hour), trainNow, "starboard", nil)
	// Starboard has no model, so none of its minutes are judged; port is not read.
	if res.JudgedMinutes != 0 {
		t.Fatalf("judged = %d", res.JudgedMinutes)
	}
	for _, n := range res.NotJudged {
		if n.Subject == "port" {
			t.Fatalf("port was read: %+v", res.NotJudged)
		}
	}
}

func TestScore_ProblemsAreStatedAndNeverBecomeAnEmptyResult(t *testing.T) {
	good := trainedPort(t)
	cases := []struct {
		name string
		c    *scoreCase
		kind string
		text string
	}{
		{"no influx", func() *scoreCase { c := newScoreCase(good, nil); c.srcErr = errNormalNoInflux; return c }(), "no_influx", "InfluxDB is not set up"},
		{"unreadable file", func() *scoreCase {
			c := newScoreCase(normalModelFile{}, nil)
			c.modelEr = errors.New("parsing x: bad")
			return c
		}(), "unreadable", "parsing x: bad"},
		{"no model yet", newScoreCase(normalModelFile{Subsystems: map[string]normalSubsystemModel{}}, nil), unusualNeverTrained, normalNoModelYet},
		{"a read fails", func() *scoreCase {
			c := newScoreCase(good, nil)
			c.minErr = errors.New("InfluxDB could not be read: timeout")
			return c
		}(), "error", "InfluxDB could not be read: timeout"},
	}
	for _, tc := range cases {
		res := tc.c.score(24 * time.Hour)
		if res.ProblemKind != tc.kind || res.Problem != tc.text {
			t.Errorf("%s: kind %q problem %q; want %q %q", tc.name, res.ProblemKind, res.Problem, tc.kind, tc.text)
		}
	}
}

// With no model but a failed training run on record, the stored problem is the
// reason.
func TestScore_NoModelAfterAFailedRunSaysWhy(t *testing.T) {
	m := normalModelFile{Subsystems: map[string]normalSubsystemModel{normalSubsystemEngines: {State: normalStateFailed, AttemptedAt: trainNow, Problem: `the one-minute bucket "b" could not be read: nope`}}}
	res := newScoreCase(m, nil).score(time.Hour)
	if res.ProblemKind != unusualNoModel || res.Problem != `the one-minute bucket "b" could not be read: nope` {
		t.Fatalf("result = %q %q", res.ProblemKind, res.Problem)
	}
}

func TestScore_AGoodModelWithAFailedLatestRunCarriesTheFailure(t *testing.T) {
	m := trainedPort(t)
	rec := m.Subsystems[normalSubsystemEngines]
	rec.State, rec.AttemptedAt, rec.Problem = normalStateFailed, trainNow, "InfluxDB could not be read: timeout"
	m.Subsystems[normalSubsystemEngines] = rec
	res := newScoreCase(m, steadyScoreFeed(43)).score(24 * time.Hour)
	if res.TrainingProblem != "InfluxDB could not be read: timeout" || res.JudgedMinutes == 0 {
		t.Fatalf("result = %+v", res)
	}
}

func TestScore_ModelInfoCarriesTheLearnedDateAndBands(t *testing.T) {
	res := newScoreCase(trainedPort(t), steadyScoreFeed(44)).score(24 * time.Hour)
	if res.Model == nil || !res.Model.TrainedAt.Equal(trainNow) || res.Model.Source.Kind != normalSourceMainBucket {
		t.Fatalf("model = %+v", res.Model)
	}
	if res.Model.Bands["Port engine"] != 2 || len(res.Model.Bands) != 1 {
		t.Fatalf("bands are counted by the engine's name: %v", res.Model.Bands)
	}
}

// The window is cut to seven days inside the score, and the result says so.
func TestScore_AWindowOverSevenDaysIsCutAndSaidSo(t *testing.T) {
	c := newScoreCase(trainedPort(t), steadyScoreFeed(45))
	res := scoreOnly(context.Background(), normalEnginesSubsystem(), c.deps(), trainNow.Add(-30*24*time.Hour), trainNow, "", nil)
	if !res.Clipped || !res.Since.Equal(trainNow.Add(-7*24*time.Hour)) {
		t.Fatalf("clipped %v since %v", res.Clipped, res.Since)
	}
	if want := trainNow.Add(-7*24*time.Hour - normalScoreLeadIn); !c.since.Equal(want) {
		t.Fatalf("the read starts at %v, want %v", c.since, want)
	}
	if res = scoreOnly(context.Background(), normalEnginesSubsystem(), c.deps(), trainNow.Add(-24*time.Hour), trainNow, "", nil); res.Clipped {
		t.Fatal("a day is not clipped")
	}
}

// One read gets every reading the judged bands use, with the caller's context.
func TestScore_OneReadWithTheCallersContext(t *testing.T) {
	type key struct{}
	c := newScoreCase(trainedPort(t), steadyScoreFeed(46))
	ctx := context.WithValue(context.Background(), key{}, "mark")
	scoreOnly(ctx, normalEnginesSubsystem(), c.deps(), trainNow.Add(-24*time.Hour), trainNow, "", nil)
	if c.asked != 1 || len(c.paths) < 5 {
		t.Fatalf("%d reads of %v, want 1 read of the engine's readings", c.asked, c.paths)
	}
	if c.ctx == nil || c.ctx.Value(key{}) != "mark" {
		t.Fatal("the caller's context did not reach the read")
	}
}

func answeredPort(from, to int, answer string) normalAnswer {
	return normalAnswer{
		StretchID: "engines:port@x", Subsystem: normalSubsystemEngines, Subject: "port", Answer: answer,
		From: scoreRunStart.Add(time.Duration(from) * time.Minute), To: scoreRunStart.Add(time.Duration(to) * time.Minute),
	}
}

func TestScore_MinutesAnsweredNormalAreNeverFlagged(t *testing.T) {
	feed := steadyScoreFeed(32)
	bend(feed, "propulsion.port.oilPressure", 40, 70, halve)

	c := newScoreCase(trainedPort(t), feed)
	c.answers = []normalAnswer{answeredPort(40, 70, normalAnswerNormal)}
	res := c.score(24 * time.Hour)
	if len(res.Stretches) != 0 || res.JudgedMinutes != 110 {
		t.Fatalf("answered normal: stretches %+v, judged %d", res.Stretches, res.JudgedMinutes)
	}

	// Only the minutes inside the range are cleared; the rest of the fault still shows.
	c.answers = []normalAnswer{answeredPort(40, 60, normalAnswerNormal)}
	res = c.score(24 * time.Hour)
	if len(res.Stretches) != 1 || res.Stretches[0].Minutes != 10 || !res.Stretches[0].Start.Equal(scoreRunStart.Add(60*time.Minute)) {
		t.Fatalf("partly answered: stretches %+v", res.Stretches)
	}
}

func TestScore_OnlyANormalAnswerForThisEngineClearsMinutes(t *testing.T) {
	feed := steadyScoreFeed(32)
	bend(feed, "propulsion.port.oilPressure", 40, 70, halve)
	other := answeredPort(40, 70, normalAnswerNormal)
	other.Subject = "starboard"
	elsewhere := answeredPort(40, 70, normalAnswerNormal)
	elsewhere.Subsystem = "house"
	c := newScoreCase(trainedPort(t), feed)
	c.answers = []normalAnswer{other, elsewhere, answeredPort(40, 70, normalAnswerReal), answeredPort(40, 70, normalAnswerUnknown)}
	if res := c.score(24 * time.Hour); len(res.Stretches) != 1 || res.Stretches[0].Minutes != 30 {
		t.Fatalf("stretches = %+v", res.Stretches)
	}
}

// A normal answer with a real one over part of it, saved as the file saves them:
// the real answer takes its minutes, so those are flagged again.
func TestScore_ARealAnswerOverPartOfANormalOneFlagsThoseMinutesAgain(t *testing.T) {
	feed := steadyScoreFeed(32)
	bend(feed, "propulsion.port.oilPressure", 40, 70, halve)
	normal := answeredPort(40, 70, normalAnswerNormal)
	normal.AnsweredAt = trainNow.Add(-2 * time.Hour)
	real := answeredPort(50, 60, normalAnswerReal)
	real.StretchID = "engines:port@y"
	real.AnsweredAt = trainNow.Add(-time.Hour)
	path := filepath.Join(t.TempDir(), "answers.json")
	for _, a := range []normalAnswer{normal, real} {
		if _, err := saveNormalAnswer(path, a); err != nil {
			t.Fatal(err)
		}
	}
	c := newScoreCase(trainedPort(t), feed)
	var err error
	if c.answers, err = loadNormalAnswersList(path); err != nil {
		t.Fatal(err)
	}
	res := c.score(24 * time.Hour)
	if len(res.Stretches) != 1 || res.Stretches[0].Minutes != 10 || !res.Stretches[0].Start.Equal(scoreRunStart.Add(50*time.Minute)) {
		t.Fatalf("stretches = %+v", res.Stretches)
	}
}
