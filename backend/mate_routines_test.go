package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Mate routines (ADR 0227): the schedule, the daily spend limit and what a
// skipped run records.

func routineTime(h, m int) time.Time {
	return time.Date(2026, 10, 29, h, m, 0, 0, time.UTC)
}

func TestMateRoutineDue(t *testing.T) {
	loc := time.FixedZone("UTC+13", 13*3600)
	created := routineTime(0, 0)
	ptr := func(t time.Time) *time.Time { return &t }
	for name, tc := range map[string]struct {
		r    mateRoutine
		now  time.Time
		want bool
	}{
		"hourly never run":              {mateRoutine{Cadence: routineHourly, CreatedAt: created}, routineTime(1, 0), true},
		"hourly 58 minutes ago":         {mateRoutine{Cadence: routineHourly, CreatedAt: created, LastRunAt: ptr(routineTime(1, 2))}, routineTime(2, 0), false},
		"hourly 59m59.9s ago":           {mateRoutine{Cadence: routineHourly, CreatedAt: created, LastRunAt: ptr(routineTime(1, 0))}, routineTime(1, 0).Add(59*time.Minute + 59*time.Second + 900*time.Millisecond), true},
		"hourly 60 minutes ago":         {mateRoutine{Cadence: routineHourly, CreatedAt: created, LastRunAt: ptr(routineTime(1, 0))}, routineTime(2, 0), true},
		"hourly after long downtime":    {mateRoutine{Cadence: routineHourly, CreatedAt: created, LastRunAt: ptr(routineTime(1, 0).AddDate(0, 0, -3))}, routineTime(2, 0), true},
		"daily before its time":         {mateRoutine{Cadence: routineDaily, DailyTime: "06:00", CreatedAt: created.AddDate(0, 0, -5), LastRunAt: ptr(routineTime(0, 0).AddDate(0, 0, -1))}, time.Date(2026, 10, 29, 5, 59, 0, 0, loc), false},
		"daily at its time":             {mateRoutine{Cadence: routineDaily, DailyTime: "06:00", CreatedAt: created.AddDate(0, 0, -5), LastRunAt: ptr(routineTime(0, 0).AddDate(0, 0, -1))}, time.Date(2026, 10, 29, 6, 0, 0, 0, loc), true},
		"daily already ran today":       {mateRoutine{Cadence: routineDaily, DailyTime: "06:00", CreatedAt: created.AddDate(0, 0, -5), LastRunAt: ptr(time.Date(2026, 10, 29, 6, 1, 0, 0, loc))}, time.Date(2026, 10, 29, 9, 0, 0, 0, loc), false},
		"daily missed days runs once":   {mateRoutine{Cadence: routineDaily, DailyTime: "06:00", CreatedAt: created.AddDate(0, 0, -9), LastRunAt: ptr(time.Date(2026, 10, 24, 6, 0, 0, 0, loc))}, time.Date(2026, 10, 29, 12, 0, 0, 0, loc), true},
		"new daily waits for its time":  {mateRoutine{Cadence: routineDaily, DailyTime: "06:00", CreatedAt: time.Date(2026, 10, 29, 7, 0, 0, 0, loc)}, time.Date(2026, 10, 29, 9, 0, 0, 0, loc), false},
		"new daily runs at next 06:00":  {mateRoutine{Cadence: routineDaily, DailyTime: "06:00", CreatedAt: time.Date(2026, 10, 29, 7, 0, 0, 0, loc)}, time.Date(2026, 10, 30, 6, 0, 0, 0, loc), true},
		"daily created before its time": {mateRoutine{Cadence: routineDaily, DailyTime: "06:00", CreatedAt: time.Date(2026, 10, 29, 5, 0, 0, 0, loc)}, time.Date(2026, 10, 29, 6, 0, 0, 0, loc), true},
	} {
		got, err := mateRoutineDue(tc.r, tc.now, loc)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got != tc.want {
			t.Errorf("%s: due = %v, want %v", name, got, tc.want)
		}
	}
}

type routineHostFixture struct {
	t        *testing.T
	docs     *documentStore
	asst     *assistantStore
	clock    time.Time
	cfg      mateRoutineConfig
	lonErr   error
	spent    routineSpend
	posCalls int
	ran      []string
	result   mateRoutineOutcome
	runErr   error
	host     *mateRoutineHost
	onRun    func(mateRoutine)
	convIDs  map[string]string
}

func newRoutineHostFixture(t *testing.T) *routineHostFixture {
	t.Helper()
	env := newProposalEnv(t)
	f := &routineHostFixture{
		t: t, docs: env.docs, asst: env.asst, clock: routineTime(10, 0),
		cfg:    mateRoutineConfig{Enabled: true, DailyCapUSD: 1},
		result: mateRoutineOutcome{Note: "Nothing to report."},
	}
	f.host = &mateRoutineHost{
		store:    func() *documentStore { return f.docs },
		now:      func() time.Time { return f.clock },
		config:   func() (mateRoutineConfig, error) { return f.cfg, nil },
		position: func() (float64, error) { f.posCalls++; return 0, f.lonErr },
		spend:    func(time.Time) (routineSpend, error) { return f.spent, nil },
		run: func(_ context.Context, _ mateRoutineConfig, r mateRoutine, _ time.Time, _ *time.Location) (mateRoutineOutcome, error) {
			f.ran = append(f.ran, r.Name)
			if f.onRun != nil {
				f.onRun(r)
			}
			return f.result, f.runErr
		},
	}
	return f
}

func (f *routineHostFixture) add(name, cadence, daily string) mateRoutine {
	f.t.Helper()
	r := mateRoutine{ID: "id-" + name, Name: name, Cadence: cadence, DailyTime: daily, Focus: "batteries", Enabled: true, ConversationID: "c-" + name, CreatedAt: f.clock.Add(-48 * time.Hour)}
	r.UpdatedAt = r.CreatedAt
	if err := f.docs.RunTx("test routine", func(tx *sql.Tx, _ time.Time) error { return insertMateRoutineTx(tx, r) }); err != nil {
		f.t.Fatal(err)
	}
	return r
}

// record leaves a previous run on the routine.
func (f *routineHostFixture) record(id string, at time.Time) {
	f.t.Helper()
	err := f.docs.RunTx("seed run", func(tx *sql.Tx, _ time.Time) error {
		return recordMateRoutineRunTx(tx, id, at, routineOutcomeRan, "Nothing to report.", false)
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *routineHostFixture) get(id string) mateRoutine {
	f.t.Helper()
	var r mateRoutine
	err := f.docs.Read(func(q sqlQueryer) error {
		var err error
		r, err = mateRoutineByIDQ(q, id)
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}

func TestMateRoutineTick_RunsADueRoutineOnceAndRecordsIt(t *testing.T) {
	f := newRoutineHostFixture(t)
	f.add("Hourly", routineHourly, "")
	f.host.tick(context.Background())
	f.host.tick(context.Background())
	if len(f.ran) != 1 {
		t.Fatalf("one due run, got %v", f.ran)
	}
	r := f.get("id-Hourly")
	if r.LastOutcome != routineOutcomeRan || r.LastNote != "Nothing to report." || r.LastRunAt == nil || !r.LastRunAt.Equal(f.clock) || r.LastFindingAt != nil {
		t.Fatalf("recorded %+v", r)
	}
	f.clock = f.clock.Add(61 * time.Minute)
	f.result = mateRoutineOutcome{Note: "House bank is at 41%", Finding: true}
	f.host.tick(context.Background())
	r = f.get("id-Hourly")
	if len(f.ran) != 2 || r.LastNote != "House bank is at 41%" || r.LastFindingAt == nil {
		t.Fatalf("second run %v %+v", f.ran, r)
	}
}

func TestMateRoutineTick_DoesNothingWhileRoutinesAreOff(t *testing.T) {
	f := newRoutineHostFixture(t)
	f.add("Hourly", routineHourly, "")
	f.cfg.Enabled = false
	f.host.tick(context.Background())
	if r := f.get("id-Hourly"); len(f.ran) != 0 || r.LastRunAt != nil || r.LastOutcome != "" {
		t.Fatalf("nothing runs and nothing is recorded while off: %v %+v", f.ran, r)
	}
}

func TestMateRoutineTick_SkipsWhenMateIsNotReady(t *testing.T) {
	f := newRoutineHostFixture(t)
	f.add("Hourly", routineHourly, "")
	f.cfg.NotReady = "The assistant is switched off."
	f.host.tick(context.Background())
	f.host.tick(context.Background())
	r := f.get("id-Hourly")
	if len(f.ran) != 0 || r.LastOutcome != routineOutcomeSkipped || !strings.HasPrefix(r.LastNote, "Skipped: Mate is not ready") || !strings.Contains(r.LastNote, "switched off") {
		t.Fatalf("%v %+v", f.ran, r)
	}
}

func TestMateRoutineTick_SkipsOverTheDailyLimitOncePerDueRun(t *testing.T) {
	f := newRoutineHostFixture(t)
	f.add("Hourly", routineHourly, "")
	f.spent = routineSpend{USD: 1.0}
	f.host.tick(context.Background())
	r := f.get("id-Hourly")
	if len(f.ran) != 0 || r.LastOutcome != routineOutcomeSkipped || r.LastNote != "Skipped: routines spent $1.00 of the $1.00 daily limit" {
		t.Fatalf("%v %+v", f.ran, r)
	}
	first := *r.LastRunAt
	f.clock = f.clock.Add(time.Minute)
	f.host.tick(context.Background())
	if r = f.get("id-Hourly"); !r.LastRunAt.Equal(first) {
		t.Fatal("a skipped run counts as the hour's run: it is not re-recorded every minute")
	}
}

func TestMateRoutineTick_DailyNeedsABoatPosition(t *testing.T) {
	f := newRoutineHostFixture(t)
	f.add("Morning", routineDaily, "06:00")
	f.lonErr = errors.New("no usable vessel position")
	f.host.tick(context.Background())
	r := f.get("id-Morning")
	if len(f.ran) != 0 || r.LastNote != "Skipped: no boat position, so the boat's time is not known" || r.LastOutcome != routineOutcomeSkipped {
		t.Fatalf("%v %+v", f.ran, r)
	}
}

func TestMateRoutineTick_DailyRunsInBoatTime(t *testing.T) {
	f := newRoutineHostFixture(t)
	f.add("Morning", routineDaily, "06:00")
	// 10:00 UTC is 04:00 at 90 degrees west: before the routine's 06:00.
	f.host.position = func() (float64, error) { return -90, nil }
	f.host.tick(context.Background())
	if len(f.ran) != 0 {
		t.Fatalf("04:00 boat time is not due: %v", f.ran)
	}
	f.host.position = func() (float64, error) { return 0, nil }
	f.host.tick(context.Background())
	if len(f.ran) != 1 {
		t.Fatalf("10:00 boat time is past 06:00: %v", f.ran)
	}
}

func TestMateRoutineTick_RunsOneAtATimeInCreationOrder(t *testing.T) {
	f := newRoutineHostFixture(t)
	f.add("B", routineHourly, "")
	f.add("A", routineHourly, "")
	// Same created_at: ordered by id.
	f.host.tick(context.Background())
	if got := strings.Join(f.ran, ","); got != "A,B" {
		t.Fatalf("order %q", got)
	}
}

func TestMateRoutineTick_AFailedRunIsRecordedAsSkippedWithWhy(t *testing.T) {
	f := newRoutineHostFixture(t)
	f.add("Hourly", routineHourly, "")
	f.runErr = fmt.Errorf("the model is down")
	f.host.tick(context.Background())
	r := f.get("id-Hourly")
	if r.LastOutcome != routineOutcomeSkipped || r.LastNote != "Skipped: the model is down" {
		t.Fatalf("%+v", r)
	}
}

func TestMateRoutineTick_DisabledRoutinesAreLeftAlone(t *testing.T) {
	f := newRoutineHostFixture(t)
	r := f.add("Off", routineHourly, "")
	if err := f.docs.RunTx("off", func(tx *sql.Tx, now time.Time) error {
		r.Enabled = false
		return updateMateRoutineTx(tx, r)
	}); err != nil {
		t.Fatal(err)
	}
	f.host.tick(context.Background())
	if len(f.ran) != 0 || f.get("id-Off").LastRunAt != nil {
		t.Fatal("a switched-off routine neither runs nor records")
	}
}

func TestMateRoutineFirstLine(t *testing.T) {
	for in, want := range map[string]string{
		"Nothing to report.\n":          "Nothing to report.",
		"\n\n  House bank low  \n- a":   "House bank low",
		"**Nothing to report.**\nx":     "**Nothing to report.**",
		strings.Repeat("x", 300) + "\n": strings.Repeat("x", 197) + "...",
	} {
		if got := mateRoutineFirstLine(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestMateRoutineIsFinding(t *testing.T) {
	for headline, want := range map[string]bool{
		"Nothing to report":              false,
		"Nothing to report.":             false,
		"nothing to report!":             false,
		"Nothing to report. All normal.": false,
		"**Nothing to report.**":         false,
		"_Nothing to report_":            false,
		"  `Nothing to report.`  ":       false,
		"House bank is at 41%":           true,
		"There is nothing to report on the bilge, but the fridge is warm": true,
		"": true,
	} {
		if got := mateRoutineIsFinding(headline); got != want {
			t.Errorf("%q finding = %v, want %v", headline, got, want)
		}
	}
}

func TestMateRoutineTick_StaleListIsNotRun(t *testing.T) {
	f := newRoutineHostFixture(t)
	f.add("A", routineHourly, "")
	b := f.add("B", routineHourly, "")
	f.onRun = func(r mateRoutine) {
		if r.Name != "A" {
			return
		}
		b.Enabled = false
		if err := f.docs.RunTx("off", func(tx *sql.Tx, now time.Time) error { return updateMateRoutineTx(tx, b) }); err != nil {
			t.Fatal(err)
		}
	}
	f.host.tick(context.Background())
	if got := strings.Join(f.ran, ","); got != "A" {
		t.Fatalf("a routine switched off after the list was read must not run, ran %q", got)
	}
	f2 := newRoutineHostFixture(t)
	f2.add("A", routineHourly, "")
	f2.add("B", routineHourly, "")
	f2.onRun = func(r mateRoutine) {
		if r.Name == "A" {
			_ = f2.docs.RunTx("del", func(tx *sql.Tx, _ time.Time) error { return deleteMateRoutineTx(tx, "id-B") })
		}
	}
	f2.host.tick(context.Background())
	if got := strings.Join(f2.ran, ","); got != "A" {
		t.Fatalf("a deleted routine must not run, ran %q", got)
	}
}

func TestMateRoutineTick_NoPositionOnlySkipsAfterAWholeDayAndAHalf(t *testing.T) {
	f := newRoutineHostFixture(t)
	f.add("Morning", routineDaily, "06:00")
	f.lonErr = errors.New("no usable vessel position")
	// Ran at 18:00 UTC yesterday (06:00 next day at UTC+12), position drops out at 06:30 UTC.
	ranAt := time.Date(2026, 10, 28, 18, 0, 0, 0, time.UTC)
	f.record("id-Morning", ranAt)
	f.clock = time.Date(2026, 10, 29, 6, 30, 0, 0, time.UTC)
	f.host.tick(context.Background())
	if r := f.get("id-Morning"); r.LastNote != "Nothing to report." || !r.LastRunAt.Equal(ranAt) {
		t.Fatalf("a run that may not be due yet writes nothing: %+v", r)
	}
	f.clock = ranAt.Add(26*time.Hour + time.Minute)
	f.host.tick(context.Background())
	if r := f.get("id-Morning"); r.LastNote != "Skipped: no boat position, so the boat's time is not known" {
		t.Fatalf("past 26 hours the skip is recorded: %+v", r)
	}
}

func TestMateRoutineTick_OnePositionFetchPerTick(t *testing.T) {
	f := newRoutineHostFixture(t)
	f.add("A", routineHourly, "")
	f.add("B", routineHourly, "")
	f.add("C", routineDaily, "06:00")
	f.host.tick(context.Background())
	if f.posCalls != 1 {
		t.Fatalf("position fetched %d times in one tick", f.posCalls)
	}
}

func TestMateRoutineTick_LedgerCountsAFailedRunsCost(t *testing.T) {
	f := newRoutineHostFixture(t)
	f.add("Hourly", routineHourly, "")
	f.result = mateRoutineOutcome{Reached: true, CostUSD: 0.4, CostKnown: true}
	f.runErr = errors.New("Mate did not finish the run")
	f.host.tick(context.Background())
	got, err := mateRoutineSpendSince(f.docs, f.clock.Add(-24*time.Hour))
	if err != nil || got.USD != 0.4 || got.UnknownAt != nil {
		t.Fatalf("spend %+v err %v", got, err)
	}
	// A run that never reached the model costs nothing and leaves no row.
	f2 := newRoutineHostFixture(t)
	f2.add("Hourly", routineHourly, "")
	f2.runErr = errors.New("not ready")
	f2.host.tick(context.Background())
	if got, _ := mateRoutineSpendSince(f2.docs, f2.clock.Add(-24*time.Hour)); got.USD != 0 || got.UnknownAt != nil {
		t.Fatalf("%+v", got)
	}
}

func TestMateRoutineTick_AnUnknownCostPausesRoutinesForADay(t *testing.T) {
	f := newRoutineHostFixture(t)
	f.add("Hourly", routineHourly, "")
	f.host.spend = func(since time.Time) (routineSpend, error) { return mateRoutineSpendSince(f.docs, since) }
	f.result = mateRoutineOutcome{Reached: true, CostKnown: false}
	f.runErr = errors.New("openrouter stream ended early")
	f.host.tick(context.Background())
	failedAt := f.clock
	f.clock = f.clock.Add(2 * time.Hour)
	f.ran = nil
	f.host.tick(context.Background())
	r := f.get("id-Hourly")
	want := "Skipped: a run failed and its cost is not known, so routines are paused until " + failedAt.Add(24*time.Hour).In(vesselLocalLocation(0)).Format("15:04 on 2 Jan (MST)")
	if len(f.ran) != 0 || r.LastNote != want {
		t.Fatalf("ran %v note %q, want %q", f.ran, r.LastNote, want)
	}
	f.clock = failedAt.Add(24*time.Hour + time.Minute)
	f.result = mateRoutineOutcome{Note: "Nothing to report."}
	f.runErr = nil
	f.host.tick(context.Background())
	if len(f.ran) != 1 {
		t.Fatal("routines resume after the 24 hours")
	}
}
