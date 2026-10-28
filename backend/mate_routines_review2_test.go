package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// ── what a failed request cost ──────────────────────────────────────────

func TestOpenRouterFailureCharge(t *testing.T) {
	dial := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	read := &net.OpError{Op: "read", Err: errors.New("connection reset")}
	for name, tc := range map[string]struct {
		err  error
		want openRouterCharge
		id   string
	}{
		"refused by status":        {&openRouterStatusError{text: "openrouter status 500: boom"}, chargeNone, ""},
		"dial error":               {&openRouterRequestError{err: dial}, chargeNone, ""},
		"dns error":                {&openRouterRequestError{err: &net.DNSError{Err: "no such host", Name: "openrouter.ai"}}, chargeNone, ""},
		"timeout waiting":          {&openRouterRequestError{err: context.DeadlineExceeded}, chargeUnknown, ""},
		"reset after sending":      {&openRouterRequestError{err: read}, chargeUnknown, ""},
		"stream cut with id":       {&openRouterPartialError{GenerationID: "gen-1", err: errors.New("stream ended early")}, chargeLookup, "gen-1"},
		"stream cut without id":    {&openRouterPartialError{err: errors.New("stream ended early")}, chargeUnknown, ""},
		"wrapped like the callers": {fmt.Errorf("web search failed: %w", &openRouterRequestError{err: dial}), chargeNone, ""},
		"not an openrouter error":  {errors.New("query must not be empty"), chargeNone, ""},
	} {
		got, id := openRouterFailureCharge(tc.err)
		if got != tc.want || id != tc.id {
			t.Errorf("%s: charge %v id %q, want %v %q", name, got, id, tc.want, tc.id)
		}
	}
}

func TestAssistantCostMeter_ResolveLooksUpPendingGenerations(t *testing.T) {
	prev := routineCostLookupDelays
	routineCostLookupDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { routineCostLookupDelays = prev })

	calls := 0
	m := &assistantCostMeter{}
	m.add(0.01)
	m.addPending("gen-1")
	m.addPending("gen-1") // the same generation is looked up once
	usd, known := m.resolve(context.Background(), func(_ context.Context, id string) (float64, error) {
		calls++
		if calls < 3 {
			return 0, errors.New("not found yet")
		}
		return 0.04, nil
	})
	if !known || usd < 0.0499 || usd > 0.0501 || calls != 3 {
		t.Fatalf("usd %v known %v after %d lookups", usd, known, calls)
	}

	m = &assistantCostMeter{}
	m.addPending("gen-2")
	if _, known := m.resolve(context.Background(), func(context.Context, string) (float64, error) { return 0, errors.New("no") }); known {
		t.Fatal("a cost that cannot be found is unknown, never zero")
	}
	m = &assistantCostMeter{}
	m.markUnknown()
	if _, known := m.resolve(context.Background(), nil); known {
		t.Fatal("an unknown stays unknown")
	}
}

func TestOpenRouterGenerationCost(t *testing.T) {
	var gotURL, gotAuth string
	doer := doerFunc(func(req *http.Request) (*http.Response, error) {
		gotURL, gotAuth = req.URL.String(), req.Header.Get("Authorization")
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":{"id":"gen-9","total_cost":0.0123}}`)), Header: http.Header{}}, nil
	})
	cost, err := openRouterGenerationCost(context.Background(), doer, "sk-test", "gen-9")
	if err != nil || cost != 0.0123 || gotURL != "https://openrouter.ai/api/v1/generation?id=gen-9" || gotAuth != "Bearer sk-test" {
		t.Fatalf("cost %v err %v url %s auth %s", cost, err, gotURL, gotAuth)
	}
	missing := doerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"not found"}}`)), Header: http.Header{}}, nil
	})
	if _, err := openRouterGenerationCost(context.Background(), missing, "k", "gen-9"); err == nil {
		t.Fatal("a 404 (OpenRouter has not caught up yet) is an error to retry")
	}
}

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

func TestAssistantRunner_TransportFailuresAreChargedAccordingToHowFarTheyGot(t *testing.T) {
	run := func(t *testing.T, resp *http.Response, doErr error) (usd float64, known bool, pending []string) {
		t.Helper()
		doer := &queuedChatDoer{responses: []*http.Response{resp}, errs: []error{doErr}}
		meter := &assistantCostMeter{}
		emit, _ := recordingEmitter()
		runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: &fakeToolExecutor{}, emit: emit}
		if _, err := runner.run(withAssistantCostMeter(context.Background(), meter), "system", "", nil); err == nil {
			t.Fatal("the run must fail")
		}
		usd, known = meter.snapshot()
		return usd, known, meter.pendingIDs()
	}

	t.Run("connection refused costs nothing", func(t *testing.T) {
		usd, known, pending := run(t, nil, &net.OpError{Op: "dial", Err: errors.New("connection refused")})
		if usd != 0 || !known || len(pending) != 0 {
			t.Fatalf("usd %v known %v pending %v", usd, known, pending)
		}
	})
	t.Run("a stream cut after the first chunk is looked up by its generation id", func(t *testing.T) {
		body := `data: {"id":"gen-77","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"par"}}]}` + "\n\n"
		usd, known, pending := run(t, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil)
		if usd != 0 || !known || len(pending) != 1 || pending[0] != "gen-77" {
			t.Fatalf("usd %v known %v pending %v", usd, known, pending)
		}
	})
	t.Run("a timeout waiting for the response is unknown", func(t *testing.T) {
		_, known, pending := run(t, nil, context.DeadlineExceeded)
		if known || len(pending) != 0 {
			t.Fatalf("known %v pending %v", known, pending)
		}
	})
}

type searchSpendingTools struct {
	fakeToolExecutor
	usage openRouterUsage
	ids   []string
}

func (s *searchSpendingTools) drainUsage() openRouterUsage {
	u := s.usage
	s.usage = openRouterUsage{}
	return u
}

func (s *searchSpendingTools) drainRisk() ([]string, bool) {
	ids := s.ids
	s.ids = nil
	return ids, false
}

func TestAssistantRunner_AFailedToolRoundStillCountsSearchSpend(t *testing.T) {
	round0 := chatResponse(t, http.StatusOK, openRouterChatResponse{
		Model: "m",
		Choices: []openRouterChoice{{Message: openRouterMessage{Role: "assistant", ToolCalls: []openRouterToolCall{
			// No id: runToolRound refuses the round.
			{ID: "", Type: "function", Function: openRouterToolCallFunction{Name: "search_web", Arguments: openRouterArguments(`{}`)}},
		}}}},
		Usage: usage(10, 5, 0.01),
	})
	tools := &searchSpendingTools{usage: openRouterUsage{Cost: 0.05}, ids: []string{"gen-search"}}
	meter := &assistantCostMeter{}
	emit, _ := recordingEmitter()
	runner := &assistantRunner{doer: &queuedChatDoer{responses: []*http.Response{round0}, errs: []error{nil}}, apiKey: "k", model: "m", tools: tools, emit: emit}
	if _, err := runner.run(withAssistantCostMeter(context.Background(), meter), "system", "", nil); err == nil {
		t.Fatal("the round fails")
	}
	usd, known := meter.snapshot()
	if usd < 0.0599 || usd > 0.0601 || !known || len(meter.pendingIDs()) != 1 {
		t.Fatalf("usd %v known %v pending %v", usd, known, meter.pendingIDs())
	}
}

func TestSearchFailuresReachTheRunsMeter(t *testing.T) {
	dial := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	deps := assistantToolDeps{}.withWebSearch(doerFunc(func(*http.Request) (*http.Response, error) { return nil, dial }), "k", func() (string, error) { return "m", nil })
	if _, err := deps.webSearch(context.Background(), "tides"); err == nil {
		t.Fatal("search fails")
	}
	if ids, unknown := deps.drainRisk(); len(ids) != 0 || unknown {
		t.Fatalf("a search that never connected cost nothing: %v %v", ids, unknown)
	}
	cut := doerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":"gen-s","choices":`)), Header: http.Header{}}, nil
	})
	deps = assistantToolDeps{}.withWebSearch(cut, "k", func() (string, error) { return "m", nil })
	_, _ = deps.webSearch(context.Background(), "tides")
	if ids, unknown := deps.drainRisk(); len(ids) != 0 || !unknown {
		t.Fatalf("a body cut off before it parsed has no id, so its cost is unknown: %v %v", ids, unknown)
	}
}

// ── the scheduler ───────────────────────────────────────────────────────

func TestMateRoutineTick_AFailedRecordStopsEveryRunUntilRestart(t *testing.T) {
	f := newRoutineHostFixture(t)
	f.add("A", routineHourly, "")
	f.add("B", routineHourly, "")
	f.result = mateRoutineOutcome{Reached: true, CostUSD: 0.1, CostKnown: true, Note: "Nothing to report."}
	if _, err := f.docs.db.Exec(`DROP TABLE mate_routine_runs`); err != nil {
		t.Fatal(err)
	}
	f.host.tick(context.Background())
	if got := strings.Join(f.ran, ","); got != "A" {
		t.Fatalf("after the log failed no further routine runs this tick, ran %q", got)
	}
	stop := f.host.stopReason()
	if !strings.HasPrefix(stop, "Routines stopped: the run log could not be saved (") || !strings.HasSuffix(stop, "). Restart Helmcentral after fixing it.") {
		t.Fatalf("stop reason %q", stop)
	}
	f.clock = f.clock.Add(3 * time.Hour)
	f.host.tick(context.Background())
	if len(f.ran) != 1 {
		t.Fatalf("still stopped on the next tick: %v", f.ran)
	}
}

func TestMateRoutineTick_ARoutineDeletedMidRunStillLeavesItsLedgerRow(t *testing.T) {
	f := newRoutineHostFixture(t)
	f.add("A", routineHourly, "")
	f.result = mateRoutineOutcome{Reached: true, CostUSD: 0.3, CostKnown: true, Note: "Nothing to report."}
	f.onRun = func(r mateRoutine) {
		_ = f.docs.RunTx("del", func(tx *sql.Tx, _ time.Time) error { return deleteMateRoutineTx(tx, r.ID) })
	}
	f.host.tick(context.Background())
	spent, err := mateRoutineSpendSince(f.docs, f.clock.Add(-time.Hour))
	if err != nil || spent.USD != 0.3 {
		t.Fatalf("spend %+v err %v", spent, err)
	}
	if f.host.stopReason() != "" {
		t.Fatalf("a routine deleted mid-run is not a failure: %q", f.host.stopReason())
	}
}

func TestMateRoutineTick_NoPositionSkipLeavesTheRunOwed(t *testing.T) {
	f := newRoutineHostFixture(t)
	f.add("Morning", routineDaily, "06:00")
	f.lonErr = errors.New("no usable vessel position")
	f.clock = time.Date(2026, 10, 29, 8, 1, 0, 0, time.UTC)
	f.host.tick(context.Background())
	r := f.get("id-Morning")
	if r.LastNote != "Skipped: no boat position, so the boat's time is not known" || r.LastRunAt != nil || r.LastSkipAt == nil {
		t.Fatalf("the note is left without counting as a run: %+v", r)
	}
	f.clock = f.clock.Add(time.Minute)
	f.host.tick(context.Background())
	if r = f.get("id-Morning"); !r.LastSkipAt.Equal(time.Date(2026, 10, 29, 8, 1, 0, 0, time.UTC)) {
		t.Fatal("the note is left once a day, not every minute")
	}
	f.lonErr = nil
	f.clock = time.Date(2026, 10, 29, 8, 5, 0, 0, time.UTC)
	f.host.tick(context.Background())
	if len(f.ran) != 1 {
		t.Fatalf("the run still happens when the position returns: %v", f.ran)
	}
}

func TestMateRoutineTick_AHourlyRoutineRunsOnTheFirstTickAfterItIsMade(t *testing.T) {
	f := newRoutineHostFixture(t)
	r := f.add("New", routineHourly, "")
	r.CreatedAt, r.ScheduleChangedAt = f.clock, f.clock
	if err := f.docs.RunTx("x", func(tx *sql.Tx, _ time.Time) error {
		if _, err := tx.Exec(`DELETE FROM mate_routines`); err != nil {
			return err
		}
		return insertMateRoutineTx(tx, r)
	}); err != nil {
		t.Fatal(err)
	}
	f.host.tick(context.Background())
	if len(f.ran) != 1 {
		t.Fatalf("the operator just asked for it: %v", f.ran)
	}
}

func TestMateRoutineDue_DailyRunsOncePerBoatDayAndFollowsScheduleEdits(t *testing.T) {
	loc := time.UTC
	at := func(day, h, m int) time.Time { return time.Date(2026, 10, day, h, m, 0, 0, loc) }
	ptr := func(t time.Time) *time.Time { return &t }
	base := mateRoutine{Cadence: routineDaily, CreatedAt: at(1, 0, 0)}
	for name, tc := range map[string]struct {
		r    mateRoutine
		now  time.Time
		want bool
	}{
		"moved later after today's run: not again today": {mateRoutine{Cadence: base.Cadence, CreatedAt: base.CreatedAt, DailyTime: "18:00", LastRunAt: ptr(at(29, 6, 0)), ScheduleChangedAt: at(29, 10, 0)}, at(29, 18, 30), false},
		"moved later: runs at the new time tomorrow":     {mateRoutine{Cadence: base.Cadence, CreatedAt: base.CreatedAt, DailyTime: "18:00", LastRunAt: ptr(at(29, 6, 0)), ScheduleChangedAt: at(29, 10, 0)}, at(30, 18, 0), true},
		"moved earlier after the new time has passed":    {mateRoutine{Cadence: base.Cadence, CreatedAt: base.CreatedAt, DailyTime: "07:00", LastRunAt: ptr(at(28, 6, 0)), ScheduleChangedAt: at(29, 8, 0)}, at(29, 9, 0), false},
		"moved earlier: runs tomorrow at the new time":   {mateRoutine{Cadence: base.Cadence, CreatedAt: base.CreatedAt, DailyTime: "07:00", LastRunAt: ptr(at(28, 6, 0)), ScheduleChangedAt: at(29, 8, 0)}, at(30, 7, 0), true},
		"moved to a time still ahead today, not yet run": {mateRoutine{Cadence: base.Cadence, CreatedAt: base.CreatedAt, DailyTime: "18:00", LastRunAt: ptr(at(28, 6, 0)), ScheduleChangedAt: at(29, 10, 0)}, at(29, 18, 0), true},
	} {
		got, err := mateRoutineDue(tc.r, tc.now, loc)
		if err != nil || got != tc.want {
			t.Errorf("%s: due %v err %v, want %v", name, got, err, tc.want)
		}
	}
}

func TestRoutine_ChangingTheScheduleMarksIt(t *testing.T) {
	env := newProposalEnv(t)
	env.applyOK(t, routineCreateArgs)
	routines, _ := listRoutineRecords(env.docs, nil)
	id := routines[0].ID
	read := func() mateRoutine {
		var r mateRoutine
		_ = env.docs.Read(func(q sqlQueryer) error { var err error; r, err = mateRoutineByIDQ(q, id); return err })
		return r
	}
	created := read()
	if !created.ScheduleChangedAt.Equal(created.CreatedAt) {
		t.Fatalf("a new routine's schedule dates from its creation: %+v", created)
	}
	env.advance()
	env.applyOK(t, `{"operations":[{"type":"routine","action":"update","id":"`+id+`","fields":{"name":"Renamed"}}]}`)
	if r := read(); !r.ScheduleChangedAt.Equal(created.ScheduleChangedAt) {
		t.Error("a rename does not move the schedule")
	}
	env.advance()
	env.applyOK(t, `{"operations":[{"type":"routine","action":"update","id":"`+id+`","fields":{"daily_time":"18:00"}}]}`)
	if r := read(); !r.ScheduleChangedAt.After(created.ScheduleChangedAt) {
		t.Error("a new time of day moves the schedule")
	}
}

// ── what the panel is told ──────────────────────────────────────────────

func TestRoutinesList_ReportsWhyRoutinesAreNotRunning(t *testing.T) {
	env, _, _ := routineRunSetup(t)
	_ = env
	path := t.TempDir() + "/settings.yaml"
	if err := os.WriteFile(path, []byte("assistant:\n    enabled: true\n    routines_enabled: true\n    routines_daily_cap_usd: 99\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SETTINGS_FILE", path)
	rec := routineRequest(t, http.MethodGet, "/api/assistant/routines", "", listMateRoutinesHandler, "")
	var list mateRoutinesResponse
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &list) != nil || !strings.HasPrefix(list.Problem, "Routines are not running: ") || !strings.Contains(list.Problem, "routines_daily_cap_usd") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestRoutinesList_ReportsAStoppedScheduler(t *testing.T) {
	routineRunSetup(t)
	host := &mateRoutineHost{}
	host.halt(errors.New("disk full"))
	prev := activeMateRoutineHost
	activeMateRoutineHost = host
	t.Cleanup(func() { activeMateRoutineHost = prev })
	rec := routineRequest(t, http.MethodGet, "/api/assistant/routines", "", listMateRoutinesHandler, "")
	var list mateRoutinesResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if list.Problem != "Routines stopped: the run log could not be saved (disk full). Restart Helmcentral after fixing it." {
		t.Fatalf("%q", list.Problem)
	}
}

func TestRoutineCapNullIsMissing(t *testing.T) {
	got, err := routinesDailyCapFromSettings(map[string]any{"assistant": map[string]any{"routines_daily_cap_usd": nil}})
	if err != nil || got != defaultRoutinesDailyCapUSD {
		t.Fatalf("%v %v", got, err)
	}
	if _, err := routinesDailyCapFromSettings(map[string]any{"assistant": map[string]any{"routines_daily_cap_usd": "lots"}}); err == nil {
		t.Fatal("a real non-number still fails")
	}
}

func TestMateRoutineTick_AFailedRunIsPricedFromItsGenerationWhenRecorded(t *testing.T) {
	prevLookup, prevDelays := routineCostLookup, routineCostLookupDelays
	routineCostLookupDelays = []time.Duration{time.Millisecond}
	t.Cleanup(func() { routineCostLookup, routineCostLookupDelays = prevLookup, prevDelays })

	for name, tc := range map[string]struct {
		lookup    func(context.Context, string, string) (float64, error)
		wantUSD   float64
		wantKnown bool
	}{
		"found":     {func(_ context.Context, key, id string) (float64, error) { return 0.2, nil }, 0.2, true},
		"not found": {func(context.Context, string, string) (float64, error) { return 0, errors.New("404") }, 0, false},
	} {
		f := newRoutineHostFixture(t)
		f.add("Hourly", routineHourly, "")
		routineCostLookup = tc.lookup
		meter := &assistantCostMeter{}
		meter.addPending("gen-1")
		f.result = mateRoutineOutcome{Reached: true, Meter: meter}
		f.runErr = errors.New("openrouter stream ended early")
		f.host.spend = func(since time.Time) (routineSpend, error) { return mateRoutineSpendSince(f.docs, since) }
		f.host.tick(context.Background())
		got, err := mateRoutineSpendSince(f.docs, f.clock.Add(-time.Hour))
		if err != nil || got.USD != tc.wantUSD || (got.UnknownAt == nil) != tc.wantKnown {
			t.Errorf("%s: spend %+v err %v", name, got, err)
		}
	}
}
