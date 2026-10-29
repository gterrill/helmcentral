package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// The watch scheduler (ADR 0227): what is due, what a run records, and what
// stops it.

func watchTestTime(day, h, m int) time.Time {
	return time.Date(2026, 10, day, h, m, 0, 0, time.UTC)
}

type watchHostFixture struct {
	t         *testing.T
	docs      *documentStore
	clock     time.Time
	cfg       watchConfig
	cfgErr    error
	lonErr    error
	posCalls  int
	cfgCalls  int
	reports   []string
	findings  []watchFinding
	reportErr error
	review    watchReviewOutcome
	reviewed  []string
	onReport  func(savedWatch)
	host      *watchHost
}

func newWatchHostFixture(t *testing.T) *watchHostFixture {
	t.Helper()
	env := newProposalEnv(t)
	f := &watchHostFixture{
		t: t, docs: env.docs, clock: watchTestTime(29, 10, 0),
		review: watchReviewOutcome{Review: "Nothing needs attention.", Reached: true, CostUSD: 0.02, CostKnown: true, Model: "test/model"},
	}
	f.host = &watchHost{
		store:    func() *documentStore { return f.docs },
		now:      func() time.Time { return f.clock },
		config:   func() (watchConfig, error) { f.cfgCalls++; return f.cfg, f.cfgErr },
		position: func() (float64, error) { f.posCalls++; return 0, f.lonErr },
		report: func(_ context.Context, w savedWatch, now time.Time, loc *time.Location) (watchReport, []watchFinding, error) {
			f.reports = append(f.reports, w.Name)
			if f.onReport != nil {
				f.onReport(w)
			}
			return watchReport{Kind: watchReportKindSections, BoatTime: "now", Sections: []watchReportSection{{Name: "power", Title: "Power"}}}, f.findings, f.reportErr
		},
		review: func(_ context.Context, _ watchConfig, w savedWatch, _ watchReport, _ []watchFinding, _ time.Time, _ *time.Location) watchReviewOutcome {
			f.reviewed = append(f.reviewed, w.Name)
			return f.review
		},
	}
	return f
}

// add saves a scheduled watch made two days before the fixture's clock.
func (f *watchHostFixture) add(name, schedule string) savedWatch {
	f.t.Helper()
	created := f.clock.Add(-48 * time.Hour)
	w := savedWatch{ID: "id-" + name, Name: name, Kind: watchKindScheduled, Schedule: schedule, Sections: []string{"power"},
		Thresholds: defaultWatchThresholds(), Enabled: true, CreatedAt: created, UpdatedAt: created, ScheduleChangedAt: created}
	if err := f.docs.RunTx("test watch", func(tx *sql.Tx, _ time.Time) error { return insertSavedWatchTx(tx, w) }); err != nil {
		f.t.Fatal(err)
	}
	return w
}

// ran leaves a previous run on the watch.
func (f *watchHostFixture) ran(id string, at time.Time) {
	f.t.Helper()
	err := f.docs.RunTx("seed run", func(tx *sql.Tx, _ time.Time) error {
		return insertWatchRunTx(tx, watchRun{WatchID: id, StartedAt: at, FinishedAt: at, Outcome: watchOutcomeRan, CostKnown: true}, true)
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *watchHostFixture) get(id string) savedWatch {
	f.t.Helper()
	var w savedWatch
	if err := f.docs.Read(func(q sqlQueryer) error {
		var err error
		w, err = savedWatchByIDQ(q, id)
		return err
	}); err != nil {
		f.t.Fatal(err)
	}
	return w
}

func (f *watchHostFixture) runsOf(id string) []watchRun {
	f.t.Helper()
	var runs []watchRun
	if err := f.docs.Read(func(q sqlQueryer) error {
		var err error
		runs, err = watchRunsQ(q, id, 50, 0)
		return err
	}); err != nil {
		f.t.Fatal(err)
	}
	return runs
}

func TestWatchTick_RunsADueWatchOnceAndRecordsReportFindingsAndReview(t *testing.T) {
	f := newWatchHostFixture(t)
	f.add("Hourly", "0 * * * *")
	f.findings = []watchFinding{{Section: "power", Rule: "overnight_draw", Text: "The bank drained fast.", By: watchByRules}}
	f.review.Added = []watchFinding{{Section: "tanks", Rule: "mate_review", Text: "Diesel looks lower than expected.", By: watchByMate}}
	f.host.tick(context.Background())
	f.host.tick(context.Background()) // a second tick the same minute does nothing

	if len(f.reports) != 1 || len(f.reviewed) != 1 {
		t.Fatalf("reports %v reviewed %v", f.reports, f.reviewed)
	}
	runs := f.runsOf("id-Hourly")
	if len(runs) != 1 {
		t.Fatalf("runs %+v", runs)
	}
	r := runs[0]
	if r.Outcome != watchOutcomeRan || !r.StartedAt.Equal(f.clock) || r.Review == nil || *r.Review != "Nothing needs attention." || r.ReviewProblem != nil {
		t.Fatalf("run %+v", r)
	}
	if len(r.Findings) != 2 || r.Findings[0].By != watchByRules || r.Findings[1].By != watchByMate {
		t.Fatalf("the rules' findings come first, Mate's after: %+v", r.Findings)
	}
	if r.CostUSD == nil || *r.CostUSD != 0.02 || !r.CostKnown || r.Model != "test/model" {
		t.Fatalf("cost %+v known %v model %q", r.CostUSD, r.CostKnown, r.Model)
	}
	var rep watchReport
	if err := json.Unmarshal(r.Report, &rep); err != nil || len(rep.Sections) != 1 {
		t.Fatalf("report %s: %v", r.Report, err)
	}
	if w := f.get("id-Hourly"); w.LastRunAt == nil || !w.LastRunAt.Equal(f.clock) {
		t.Fatalf("last run %+v", w.LastRunAt)
	}
}

func TestWatchTick_NotDueIsLeftAlone(t *testing.T) {
	f := newWatchHostFixture(t)
	f.add("Daily", "0 6 * * *")
	f.ran("id-Daily", watchTestTime(29, 6, 0))
	f.host.tick(context.Background())
	if len(f.reports) != 0 {
		t.Fatalf("ran %v", f.reports)
	}
	if f.cfgCalls != 0 {
		t.Errorf("Mate's readiness is read only when a watch is due, read %d times", f.cfgCalls)
	}
}

func TestWatchTick_MateNotReadyStillRecordsTheReportAndSaysWhyThereIsNoReview(t *testing.T) {
	f := newWatchHostFixture(t)
	f.add("Hourly", "0 * * * *")
	f.review = watchReviewOutcome{Problem: "Mate is not ready (no model chosen)", CostKnown: true}
	f.host.tick(context.Background())
	runs := f.runsOf("id-Hourly")
	if len(runs) != 1 || runs[0].Outcome != watchOutcomeRan || runs[0].Review != nil ||
		runs[0].ReviewProblem == nil || *runs[0].ReviewProblem != "Mate is not ready (no model chosen)" || runs[0].CostUSD != nil || !runs[0].CostKnown {
		t.Fatalf("%+v", runs)
	}
}

func TestWatchTick_AReviewWithoutAProblemOrTextIsStillExplained(t *testing.T) {
	f := newWatchHostFixture(t)
	f.add("Hourly", "0 * * * *")
	f.review = watchReviewOutcome{}
	f.host.tick(context.Background())
	if r := f.runsOf("id-Hourly")[0]; r.ReviewProblem == nil || *r.ReviewProblem == "" {
		t.Fatalf("a run is never left with neither a review nor a reason: %+v", r)
	}
}

func TestWatchTick_AnUnknownCostIsRecordedAndPausesNothing(t *testing.T) {
	f := newWatchHostFixture(t)
	f.add("Hourly", "0 * * * *")
	f.review = watchReviewOutcome{Problem: "Mate's review failed: connection reset", Reached: true, CostUSD: 0.01, CostKnown: false}
	f.host.tick(context.Background())
	r := f.runsOf("id-Hourly")[0]
	if r.CostKnown || r.CostUSD == nil {
		t.Fatalf("%+v", r)
	}
	f.review = watchReviewOutcome{Review: "ok", Reached: true, CostUSD: 0.02, CostKnown: true}
	f.clock = f.clock.Add(time.Hour)
	f.host.tick(context.Background())
	if len(f.runsOf("id-Hourly")) != 2 {
		t.Fatal("the next hour's run happens: nothing is paused for an unknown cost")
	}
	var cost watchCost
	if err := f.docs.Read(func(q sqlQueryer) error {
		var err error
		cost, err = watchCostSinceQ(q, f.clock.Add(-24*time.Hour))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if cost.UnknownRuns != 1 || cost.USD < 0.0299 || cost.USD > 0.0301 {
		t.Fatalf("cost %+v", cost)
	}
}

func TestWatchTick_NoPositionSkipsOncePerMissedInstantAndTheRunStaysOwed(t *testing.T) {
	f := newWatchHostFixture(t)
	f.add("Morning", "0 6 * * *")
	f.ran("id-Morning", watchTestTime(28, 6, 0))
	f.lonErr = errors.New("no usable vessel position")

	f.clock = watchTestTime(29, 5, 0) // UTC has not reached 06:00 again, so some zone has not either
	f.host.tick(context.Background())
	if len(f.runsOf("id-Morning")) != 1 {
		t.Fatal("an instant that only some time zones have passed is not a missed one")
	}

	f.clock = watchTestTime(29, 8, 1) // every zone has passed it
	f.host.tick(context.Background())
	runs := f.runsOf("id-Morning")
	if len(runs) != 2 || runs[0].Outcome != watchOutcomeSkipped || runs[0].SkipReason != "No boat position, so the boat's time is not known" {
		t.Fatalf("runs %+v", runs)
	}
	w := f.get("id-Morning")
	if w.LastSkipAt == nil || !w.LastRunAt.Equal(watchTestTime(28, 6, 0)) {
		t.Fatalf("a skip is not a run: %+v", w)
	}

	f.clock = f.clock.Add(time.Minute)
	f.host.tick(context.Background())
	if len(f.runsOf("id-Morning")) != 2 {
		t.Fatal("the note is left once, not every minute")
	}

	f.lonErr = nil
	f.clock = f.clock.Add(time.Minute)
	f.host.tick(context.Background())
	if len(f.reports) != 1 {
		t.Fatalf("the run still happens when the position returns: %v", f.reports)
	}
	if len(f.reports) == 1 && f.posCalls == 0 {
		t.Fatal("position was never read")
	}
}

func TestWatchTick_ReadsThePositionAndMateOncePerTick(t *testing.T) {
	f := newWatchHostFixture(t)
	f.add("A", "0 * * * *")
	f.add("B", "0 * * * *")
	f.add("C", "0 * * * *")
	f.host.tick(context.Background())
	if f.posCalls != 1 || f.cfgCalls != 1 {
		t.Fatalf("position read %d times, Mate's readiness %d", f.posCalls, f.cfgCalls)
	}
	if len(f.reports) != 3 || f.reports[0] != "A" || f.reports[2] != "C" {
		t.Fatalf("one at a time, in creation order: %v", f.reports)
	}
}

func TestWatchTick_DisabledAndOneOffWatchesAreLeftAlone(t *testing.T) {
	f := newWatchHostFixture(t)
	f.add("Off", "0 * * * *")
	if err := f.docs.RunTx("off", func(tx *sql.Tx, now time.Time) error {
		w, err := savedWatchByIDQ(tx, "id-Off")
		w.Enabled, w.UpdatedAt = false, now
		if err != nil {
			return err
		}
		return updateSavedWatchTx(tx, w)
	}); err != nil {
		t.Fatal(err)
	}
	started, ends := f.clock.Add(-time.Minute), f.clock.Add(5*time.Minute)
	oneOff := savedWatch{ID: "oo", Name: "Depth", Kind: watchKindOneOff, StartedAt: &started, EndsAt: &ends, Sections: []string{}, Enabled: true, CreatedAt: started, UpdatedAt: started}
	if err := f.docs.RunTx("one-off", func(tx *sql.Tx, _ time.Time) error { return insertSavedWatchTx(tx, oneOff) }); err != nil {
		t.Fatal(err)
	}
	f.host.tick(context.Background())
	if len(f.reports) != 0 {
		t.Fatalf("ran %v", f.reports)
	}
}

func TestWatchTick_AWatchSwitchedOffDuringAnEarlierRunIsNotRun(t *testing.T) {
	f := newWatchHostFixture(t)
	f.add("A", "0 * * * *")
	f.add("B", "0 * * * *")
	f.onReport = func(w savedWatch) {
		if w.Name != "A" {
			return
		}
		_ = f.docs.RunTx("off", func(tx *sql.Tx, now time.Time) error {
			b, _ := savedWatchByIDQ(tx, "id-B")
			b.Enabled, b.UpdatedAt = false, now
			return updateSavedWatchTx(tx, b)
		})
	}
	f.host.tick(context.Background())
	if strings.Join(f.reports, ",") != "A" {
		t.Fatalf("ran %v", f.reports)
	}
}

func TestWatchTick_AFailedRecordStopsEveryRunUntilRestart(t *testing.T) {
	f := newWatchHostFixture(t)
	f.add("A", "0 * * * *")
	f.add("B", "0 * * * *")
	if _, err := f.docs.db.Exec(`DROP TABLE watch_runs`); err != nil {
		t.Fatal(err)
	}
	f.host.tick(context.Background())
	if got := strings.Join(f.reports, ","); got != "A" {
		t.Fatalf("after the history failed no further watch runs, ran %q", got)
	}
	stop := f.host.stopReason()
	if !strings.HasPrefix(stop, "Watches stopped: the run history could not be saved (") || !strings.HasSuffix(stop, "). Restart Helmcentral after fixing it.") {
		t.Fatalf("stop reason %q", stop)
	}
	f.clock = f.clock.Add(3 * time.Hour)
	f.host.tick(context.Background())
	if len(f.reports) != 1 {
		t.Fatalf("still stopped on the next tick: %v", f.reports)
	}
	if _, err := f.host.RunNow(context.Background(), f.get("id-A")); err == nil || !strings.Contains(err.Error(), "Watches stopped") {
		t.Fatalf("run now refuses while stopped: %v", err)
	}
}

func TestWatchTick_AWatchDeletedMidRunIsNotAFailure(t *testing.T) {
	f := newWatchHostFixture(t)
	f.add("A", "0 * * * *")
	f.onReport = func(w savedWatch) {
		_ = f.docs.RunTx("del", func(tx *sql.Tx, _ time.Time) error { return deleteSavedWatchTx(tx, w.ID) })
	}
	f.host.tick(context.Background())
	if f.host.stopReason() != "" {
		t.Fatalf("a watch deleted mid-run is not a failure: %q", f.host.stopReason())
	}
}

func TestWatchTick_AReportThatCannotBeBuiltIsNotRecordedAsARun(t *testing.T) {
	f := newWatchHostFixture(t)
	f.add("A", "0 * * * *")
	f.reportErr = errors.New("the record store is busy")
	f.host.tick(context.Background())
	if len(f.runsOf("id-A")) != 0 || len(f.reviewed) != 0 {
		t.Fatal("no report, no review, no run: it is tried again next tick")
	}
	if f.host.stopReason() != "" {
		t.Fatal("this is not a failure to save history")
	}
}

func TestWatchTick_MatesSettingsUnreadableIsAReviewProblemNotAStop(t *testing.T) {
	f := newWatchHostFixture(t)
	f.add("A", "0 * * * *")
	f.cfgErr = errors.New("settings.yaml: bad indent")
	var seen watchConfig
	f.host.review = func(_ context.Context, cfg watchConfig, _ savedWatch, _ watchReport, _ []watchFinding, _ time.Time, _ *time.Location) watchReviewOutcome {
		seen = cfg
		return runWatchReview(context.Background(), cfg, savedWatch{}, watchReport{}, nil, f.clock, time.UTC)
	}
	f.host.tick(context.Background())
	if !strings.Contains(seen.NotReady, "settings could not be read") {
		t.Fatalf("config %+v", seen)
	}
	r := f.runsOf("id-A")[0]
	if r.ReviewProblem == nil || !strings.Contains(*r.ReviewProblem, "Mate is not ready") {
		t.Fatalf("%+v", r)
	}
}

func TestWatchRunNow_RunsAndCountsAsTheLatestRun(t *testing.T) {
	f := newWatchHostFixture(t)
	f.add("Morning", "0 6 * * *")
	run, err := f.host.RunNow(context.Background(), f.get("id-Morning"))
	if err != nil || run == nil || run.Outcome != watchOutcomeRan {
		t.Fatalf("run %+v err %v", run, err)
	}
	if w := f.get("id-Morning"); w.LastRunAt == nil || !w.LastRunAt.Equal(f.clock) {
		t.Fatalf("%+v", w)
	}
	f.lonErr = errors.New("no fix")
	if _, err := f.host.RunNow(context.Background(), f.get("id-Morning")); err == nil || err.Error() != watchNoPositionReason {
		t.Fatalf("without a position the boat's time is not known: %v", err)
	}
}

func TestPruneWatchHistory_DropsQuietRunsOverAYearOldAndKeepsFindings(t *testing.T) {
	f := newWatchHostFixture(t)
	f.add("A", "0 * * * *")
	old := f.clock.Add(-400 * 24 * time.Hour)
	recent := f.clock.Add(-10 * 24 * time.Hour)
	seed := func(id string, at time.Time, findings []watchFinding) {
		if err := f.docs.RunTx("seed", func(tx *sql.Tx, _ time.Time) error {
			return insertWatchRunTx(tx, watchRun{ID: id, WatchID: "id-A", StartedAt: at, FinishedAt: at, Outcome: watchOutcomeRan, Findings: findings, CostKnown: true}, true)
		}); err != nil {
			t.Fatal(err)
		}
	}
	seed("old-quiet", old, nil)
	seed("old-found", old.Add(time.Hour), []watchFinding{{Section: "power", Rule: "x", Text: "t", By: watchByRules}})
	seed("recent-quiet", recent, nil)
	// A one-off watch left with no runs goes with them.
	started := old
	if err := f.docs.RunTx("one-off", func(tx *sql.Tx, _ time.Time) error {
		return insertSavedWatchTx(tx, savedWatch{ID: "oo", Name: "Depth", Kind: watchKindOneOff, StartedAt: &started, Sections: []string{}, CreatedAt: old, UpdatedAt: old})
	}); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := f.docs.RunTx("prune", func(tx *sql.Tx, _ time.Time) error {
		var err error
		n, err = pruneWatchRunsTx(tx, f.clock.Add(-watchRunRetention))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("dropped %d", n)
	}
	var ids []string
	for _, r := range f.runsOf("id-A") {
		ids = append(ids, r.ID)
	}
	if strings.Join(ids, ",") != "recent-quiet,old-found" {
		t.Fatalf("kept %v", ids)
	}
	for _, w := range func() []savedWatch {
		var l []savedWatch
		_ = f.docs.Read(func(q sqlQueryer) error { l, _ = listSavedWatchesQ(q); return nil })
		return l
	}() {
		if w.ID == "oo" {
			t.Fatal("the spent one-off watch is cleared with its runs")
		}
	}
}

func TestWatchBoatLongitude_RefusesWhatTheRestOfTheAppRefuses(t *testing.T) {
	good := vesselStateData{Latitude: -36.8, Longitude: 174.8}
	if lon, err := watchBoatLongitudeOf(good, nil); err != nil || lon != 174.8 {
		t.Fatalf("a good fix: %v %v", lon, err)
	}
	sentinel := vesselStateData{Latitude: -1, Longitude: -1}
	if _, err := watchBoatLongitudeOf(sentinel, nil); err == nil {
		t.Error("the -1,-1 pair is the no-position sentinel, not a place")
	}
	alert := good
	alert.GNSSCriticalAlert = true
	if _, err := watchBoatLongitudeOf(alert, nil); err == nil {
		t.Error("a critical GNSS alert means the position is not to be trusted")
	}
	if _, err := watchBoatLongitudeOf(good, errors.New("signalk down")); err == nil || !strings.Contains(err.Error(), "signalk down") {
		t.Errorf("a failed read is reported with its cause: %v", err)
	}
}
