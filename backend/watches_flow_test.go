package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// The watch record type, the REST routes and Mate's tools (ADR 0227).

const watchCreateArgs = `{"operations":[{"type":"watch","action":"create","fields":{"name":"Morning report","schedule":"0 6 * * *","sections":["tanks","power"],"look_out_for":"the fridge and the bilge"}}]}`

func firstWatch(t *testing.T, docs *documentStore) savedWatch {
	t.Helper()
	var list []savedWatch
	if err := docs.Read(func(q sqlQueryer) error {
		var err error
		list, err = listSavedWatchesQ(q)
		return err
	}); err != nil || len(list) == 0 {
		t.Fatalf("watches %v err %v", list, err)
	}
	return list[0]
}

func TestWatchRecord_CreateReadsInWordsAndAppliesWithDefaults(t *testing.T) {
	env := newProposalEnv(t)
	p := env.propose(t, watchCreateArgs)
	if got, want := p.Ops[0].Description, "Add watch Morning report: every day at 06:00 boat time, covering power and tanks"; got != want {
		t.Fatalf("description %q, want %q", got, want)
	}
	if p.Ops[0].After["schedule"] != "Every day at 06:00 boat time" {
		t.Errorf("the card shows the schedule in words, got %v", p.Ops[0].After)
	}
	for _, k := range []string{"last_run", "schedule_sentence"} {
		if _, ok := p.Ops[0].After[k]; ok {
			t.Errorf("%s is the scheduler's, not on the card", k)
		}
	}
	env.advance()
	if _, err := env.apply(p.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	w := firstWatch(t, env.docs)
	if w.Kind != watchKindScheduled || !w.Enabled || w.LastRunAt != nil || w.Thresholds != defaultWatchThresholds() ||
		strings.Join(w.Sections, ",") != "power,tanks" || w.LookOutFor != "the fridge and the bilge" || w.Schedule != "0 6 * * *" {
		t.Fatalf("%+v", w)
	}
}

func TestWatchRecord_ValidationRefusesBadInput(t *testing.T) {
	env := newProposalEnv(t)
	for name, tc := range map[string]struct{ fields, want string }{
		"no schedule":     {`"name":"X","sections":["power"]`, "schedule: is required"},
		"no sections":     {`"name":"X","schedule":"0 6 * * *"`, "sections: is required"},
		"empty sections":  {`"name":"X","schedule":"0 6 * * *","sections":[]`, "sections:"},
		"bad section":     {`"name":"X","schedule":"0 6 * * *","sections":["engine"]`, "sections:"},
		"bad cron":        {`"name":"X","schedule":"every day","sections":["power"]`, "schedule:"},
		"every minute":    {`"name":"X","schedule":"* * * * *","sections":["power"]`, "more often than every 15 minutes"},
		"utc prefix":      {`"name":"X","schedule":"TZ=UTC 0 6 * * *","sections":["power"]`, "schedule:"},
		"empty name":      {`"name":" ","schedule":"0 6 * * *","sections":["power"]`, "name:"},
		"long name":       {`"name":"` + strings.Repeat("n", 61) + `","schedule":"0 6 * * *","sections":["power"]`, "name:"},
		"long look out":   {`"name":"X","schedule":"0 6 * * *","sections":["power"],"look_out_for":"` + strings.Repeat("f", 501) + `"`, "look_out_for:"},
		"bad tank change": {`"name":"X","schedule":"0 6 * * *","sections":["power"],"tank_change_points":0`, "tank_change_points:"},
		"bad pressure":    {`"name":"X","schedule":"0 6 * * *","sections":["power"],"pressure_fall_hpa":99`, "pressure_fall_hpa:"},
	} {
		msg := proposeError(t, env.deps, `{"operations":[{"type":"watch","action":"create","fields":{`+tc.fields+`}}]}`)
		if !strings.Contains(msg, tc.want) {
			t.Errorf("%s: %q lacks %q", name, msg, tc.want)
		}
	}
	for i := 0; i < maxScheduledWatches; i++ {
		env.applyOK(t, `{"operations":[{"type":"watch","action":"create","fields":{"name":"W","schedule":"0 * * * *","sections":["power"]}}]}`)
	}
	msg := proposeError(t, env.deps, `{"operations":[{"type":"watch","action":"create","fields":{"name":"21st","schedule":"0 * * * *","sections":["power"]}}]}`)
	if !strings.Contains(msg, "most there can be") {
		t.Errorf("the 21st watch is refused with a plain reason, got %q", msg)
	}
}

func TestWatchRecord_UpdateRestartsTheScheduleClockOnlyWhenTheScheduleChanges(t *testing.T) {
	env := newProposalEnv(t)
	env.applyOK(t, watchCreateArgs)
	w := firstWatch(t, env.docs)
	created := w.ScheduleChangedAt

	env.advance()
	env.applyOK(t, `{"operations":[{"type":"watch","action":"update","id":"`+w.ID+`","fields":{"name":"Dawn report","tank_change_points":15}}]}`)
	w = firstWatch(t, env.docs)
	if w.Name != "Dawn report" || w.Thresholds.TankChangePoints != 15 || w.Thresholds.PressureFallHPa != 3 || !w.ScheduleChangedAt.Equal(created) {
		t.Fatalf("a rename leaves the schedule's clock alone: %+v", w)
	}

	env.advance()
	env.applyOK(t, `{"operations":[{"type":"watch","action":"update","id":"`+w.ID+`","fields":{"schedule":"0 */2 * * *"}}]}`)
	w = firstWatch(t, env.docs)
	if !w.ScheduleChangedAt.After(created) || w.Schedule != "0 */2 * * *" {
		t.Fatalf("a new schedule restarts its clock: %+v", w)
	}

	p := env.propose(t, `{"operations":[{"type":"watch","action":"update","id":"`+w.ID+`","fields":{"enabled":false}}]}`)
	if p.Ops[0].Description != "Switch off watch Dawn report" {
		t.Errorf("description %q", p.Ops[0].Description)
	}
}

func TestWatchRecord_DeleteTakesItsRuns(t *testing.T) {
	env := newProposalEnv(t)
	env.applyOK(t, watchCreateArgs)
	w := firstWatch(t, env.docs)
	if err := env.docs.RunTx("run", func(tx *sql.Tx, _ time.Time) error {
		return insertWatchRunTx(tx, watchRun{WatchID: w.ID, StartedAt: env.clock, FinishedAt: env.clock, Outcome: watchOutcomeRan, CostKnown: true}, true)
	}); err != nil {
		t.Fatal(err)
	}
	env.advance()
	env.applyOK(t, `{"operations":[{"type":"watch","action":"delete","id":"`+w.ID+`"}]}`)
	var runs int
	_ = env.docs.Read(func(q sqlQueryer) error {
		return q.QueryRow(`SELECT COUNT(*) FROM watch_runs`).Scan(&runs)
	})
	if runs != 0 {
		t.Fatalf("a deleted watch's runs go with it: %d", runs)
	}
}

func TestWatchRecord_OneOffWatchesAreNotMatesToEdit(t *testing.T) {
	env := newProposalEnv(t)
	started, ends := env.clock, env.clock.Add(5*time.Minute)
	if err := env.docs.RunTx("one-off", func(tx *sql.Tx, _ time.Time) error {
		return insertSavedWatchTx(tx, savedWatch{ID: "oo", Name: "Depth", Kind: watchKindOneOff, StartedAt: &started, EndsAt: &ends,
			Sections: []string{}, Enabled: true, CreatedAt: started, UpdatedAt: started})
	}); err != nil {
		t.Fatal(err)
	}
	msg := proposeError(t, env.deps, `{"operations":[{"type":"watch","action":"delete","id":"oo"}]}`)
	if !strings.Contains(msg, "no watch with id") {
		t.Errorf("a one-off watch is not a record Mate can change: %q", msg)
	}
	list, _ := listWatchRecords(env.docs, nil)
	if len(list) != 0 {
		t.Errorf("list_records shows scheduled watches only: %v", list)
	}
}

// ── REST ────────────────────────────────────────────────────────────────

func watchRequestTo(method, path, body string, params ...string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	var names, values []string
	for i := 0; i+1 < len(params); i += 2 {
		names, values = append(names, params[i]), append(values, params[i+1])
	}
	c.SetParamNames(names...)
	c.SetParamValues(values...)
	return c, rec
}

func TestWatchREST_CreateListGetPatchDelete(t *testing.T) {
	env := newProposalEnv(t)
	_ = env

	c, rec := watchRequestTo(http.MethodPost, "/api/watches", `{"name":"At anchor","schedule":"0 */2 * * *","sections":["anchor","weather","power"]}`)
	if err := createWatchHandler(c); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("create: %v %d %s", err, rec.Code, rec.Body)
	}
	var created savedWatch
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.ID == "" || !created.Enabled || created.Thresholds != defaultWatchThresholds() || strings.Join(created.Sections, ",") != "power,anchor,weather" {
		t.Fatalf("created %+v", created)
	}

	c, rec = watchRequestTo(http.MethodPost, "/api/watches", `{"name":"Bad","schedule":"* * * * *","sections":["power"]}`)
	if err := createWatchHandler(c); err != nil || rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "schedule:") {
		t.Fatalf("a bad schedule is a 400 naming the field: %d %s", rec.Code, rec.Body)
	}
	c, rec = watchRequestTo(http.MethodPost, "/api/watches", `{"name":"Typo","schedulle":"0 6 * * *"}`)
	if err := createWatchHandler(c); err != nil || rec.Code != http.StatusBadRequest {
		t.Fatalf("an unknown field is refused: %d %s", rec.Code, rec.Body)
	}

	// A run to show in the list and the detail.
	started := env.clock
	if err := env.docs.RunTx("run", func(tx *sql.Tx, _ time.Time) error {
		cost := 0.05
		review := "Quiet."
		return insertWatchRunTx(tx, watchRun{ID: "r1", WatchID: created.ID, StartedAt: started, FinishedAt: started, Outcome: watchOutcomeRan,
			Report: json.RawMessage(`{"kind":"sections"}`), Findings: []watchFinding{{Section: "anchor", Rule: "outside_anchor_radius", Text: "t", By: watchByRules}},
			Review: &review, CostUSD: &cost, CostKnown: true, Model: "m"}, true)
	}); err != nil {
		t.Fatal(err)
	}

	c, rec = watchRequestTo(http.MethodGet, "/api/watches", "")
	if err := listWatchesHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("list: %v %d", err, rec.Code)
	}
	var list watchListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Watches) != 1 || list.Max != 20 || list.Watches[0].ScheduleSentence != "Every 2 hours" || !list.Watches[0].HasFinding ||
		list.Watches[0].LastRun == nil || list.Watches[0].LastRun.FindingCount != 1 {
		t.Fatalf("list %+v", list)
	}
	if list.StorageBytes <= 0 || list.Problem != "" {
		t.Fatalf("storage %d problem %q", list.StorageBytes, list.Problem)
	}
	// The cost total counts runs in the last 24 hours by the real clock.
	_ = list.Cost24h

	c, rec = watchRequestTo(http.MethodGet, "/api/watches/"+created.ID+"?limit=5", "", "id", created.ID)
	if err := getWatchHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("get: %v %d %s", err, rec.Code, rec.Body)
	}
	var detail struct {
		Watch     watchListEntry `json:"watch"`
		Runs      []watchRun     `json:"runs"`
		TotalRuns int            `json:"total_runs"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &detail)
	if detail.TotalRuns != 1 || len(detail.Runs) != 1 || detail.Runs[0].Review == nil || *detail.Runs[0].Review != "Quiet." {
		t.Fatalf("detail %+v", detail)
	}
	c, rec = watchRequestTo(http.MethodGet, "/api/watches/"+created.ID+"?limit=0", "", "id", created.ID)
	_ = getWatchHandler(c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("a limit of 0 is refused, got %d", rec.Code)
	}

	env.advance()
	c, rec = watchRequestTo(http.MethodPatch, "/api/watches/"+created.ID, `{"schedule":"0 6 * * *","thresholds":{"alarms_raised":2,"instrument_issues":1,"tank_change_points":20,"pressure_fall_hpa":4}}`, "id", created.ID)
	if err := patchWatchHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("patch: %v %d %s", err, rec.Code, rec.Body)
	}
	var patched savedWatch
	_ = json.Unmarshal(rec.Body.Bytes(), &patched)
	if patched.Schedule != "0 6 * * *" || patched.Thresholds.TankChangePoints != 20 || patched.Name != "At anchor" || !patched.ScheduleChangedAt.After(created.ScheduleChangedAt) {
		t.Fatalf("patched %+v", patched)
	}
	c, rec = watchRequestTo(http.MethodPatch, "/api/watches/"+created.ID, `{"sections":[]}`, "id", created.ID)
	_ = patchWatchHandler(c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("no sections is refused, got %d", rec.Code)
	}
	c, rec = watchRequestTo(http.MethodPatch, "/api/watches/nope", `{"enabled":false}`, "id", "nope")
	_ = patchWatchHandler(c)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown watch is a 404, got %d", rec.Code)
	}

	c, rec = watchRequestTo(http.MethodDelete, "/api/watches/"+created.ID+"/runs/r1", "", "id", created.ID, "runId", "r1")
	if err := deleteWatchRunHandler(c); err != nil || rec.Code != http.StatusNoContent {
		t.Fatalf("delete run: %v %d", err, rec.Code)
	}
	c, rec = watchRequestTo(http.MethodDelete, "/api/watches/"+created.ID+"/runs/r1", "", "id", created.ID, "runId", "r1")
	_ = deleteWatchRunHandler(c)
	if rec.Code != http.StatusNotFound {
		t.Errorf("a run already gone is a 404, got %d", rec.Code)
	}

	c, rec = watchRequestTo(http.MethodDelete, "/api/watches/"+created.ID, "", "id", created.ID)
	if err := deleteWatchHandler(c); err != nil || rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %v %d", err, rec.Code)
	}
	c, rec = watchRequestTo(http.MethodGet, "/api/watches/"+created.ID, "", "id", created.ID)
	_ = getWatchHandler(c)
	if rec.Code != http.StatusNotFound {
		t.Errorf("deleted, got %d", rec.Code)
	}
}

func TestWatchREST_ListReportsAStoppedScheduler(t *testing.T) {
	newProposalEnv(t)
	prev := activeWatchHost
	activeWatchHost = &watchHost{stopped: "Watches stopped: the run history could not be saved (disk full). Restart Helmcentral after fixing it."}
	t.Cleanup(func() { activeWatchHost = prev })
	c, rec := watchRequestTo(http.MethodGet, "/api/watches", "")
	if err := listWatchesHandler(c); err != nil {
		t.Fatal(err)
	}
	var list watchListResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if !strings.HasPrefix(list.Problem, "Watches stopped: the run history could not be saved") {
		t.Fatalf("problem %q", list.Problem)
	}
	if list.Watches == nil {
		t.Error("an empty list is [], not null")
	}
}

func TestWatchREST_RunNow(t *testing.T) {
	f := newWatchHostFixture(t)
	f.add("Morning", "0 6 * * *")
	prev := activeWatchHost
	activeWatchHost = f.host
	t.Cleanup(func() { activeWatchHost = prev })
	c, rec := watchRequestTo(http.MethodPost, "/api/watches/id-Morning/run", "", "id", "id-Morning")
	if err := runWatchHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("run: %v %d %s", err, rec.Code, rec.Body)
	}
	var run watchRun
	_ = json.Unmarshal(rec.Body.Bytes(), &run)
	if run.Outcome != watchOutcomeRan || run.Review == nil {
		t.Fatalf("run %+v", run)
	}
	f.zoneErr = context.Canceled
	c, rec = watchRequestTo(http.MethodPost, "/api/watches/id-Morning/run", "", "id", "id-Morning")
	_ = runWatchHandler(c)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "No boat position") {
		t.Fatalf("no position: %d %s", rec.Code, rec.Body)
	}
	c, rec = watchRequestTo(http.MethodPost, "/api/watches/nope/run", "", "id", "nope")
	_ = runWatchHandler(c)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown watch: %d", rec.Code)
	}
}

func TestWatchREST_SchedulePreview(t *testing.T) {
	prev := watchBoatLocation
	t.Cleanup(func() { watchBoatLocation = prev })
	watchBoatLocation = func() (*time.Location, error) { return mustZoneAt(-27.47, 153.03).Loc, nil } // Brisbane, UTC+10

	c, rec := watchRequestTo(http.MethodPost, "/api/watches/schedule-preview", `{"schedule":"0 6 * * *"}`)
	if err := watchSchedulePreviewHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatal(err, rec.Code)
	}
	var out watchPreviewResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if !out.Valid || out.Sentence != "Every day at 06:00 boat time" || !out.BoatTimeKnown || len(out.Next) != 3 {
		t.Fatalf("%+v", out)
	}
	if !strings.Contains(out.Next[0].Local, "06:00 AEST") {
		t.Errorf("run times are on the boat's clock: %+v", out.Next)
	}
	if _, err := time.Parse(time.RFC3339, out.Next[0].At); err != nil {
		t.Errorf("at %q: %v", out.Next[0].At, err)
	}

	c, rec = watchRequestTo(http.MethodPost, "/api/watches/schedule-preview", `{"schedule":"*/5 * * * *"}`)
	_ = watchSchedulePreviewHandler(c)
	out = watchPreviewResponse{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Valid || !strings.Contains(out.Error, "more often than every 15 minutes") || len(out.Next) != 0 {
		t.Fatalf("%+v", out)
	}

	watchBoatLocation = func() (*time.Location, error) { return nil, context.Canceled }
	c, rec = watchRequestTo(http.MethodPost, "/api/watches/schedule-preview", `{"schedule":"0 */2 * * *"}`)
	_ = watchSchedulePreviewHandler(c)
	out = watchPreviewResponse{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if !out.Valid || out.Sentence != "Every 2 hours" || out.BoatTimeKnown || len(out.Next) != 0 || out.NextProblem != watchNoPositionReason {
		t.Fatalf("without a position the sentence stands and no UTC time is offered: %+v", out)
	}
}

func TestWatchRoutesAreRegisteredWithTheRightTiers(t *testing.T) {
	want := map[string]apiTier{
		"GET /api/watches": tierRead, "POST /api/watches": tierWrite, "POST /api/watches/schedule-preview": tierRead,
		"GET /api/watches/:id": tierRead, "PATCH /api/watches/:id": tierWrite, "DELETE /api/watches/:id": tierWrite,
		"POST /api/watches/:id/run": tierWrite, "DELETE /api/watches/:id/runs/:runId": tierWrite,
		"GET /api/watches/channels": tierRead, "GET /api/watches/:id/runs/:runId": tierRead,
	}
	for _, r := range watchRoutes() {
		key := r.Method + " " + r.Path
		if tier, ok := want[key]; !ok || tier != r.Tier {
			t.Errorf("%s has tier %v", key, r.Tier)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Errorf("missing routes %v", want)
	}
}

// ── Mate's tools ────────────────────────────────────────────────────────

func TestMateWatchTools_ListReadAndRun(t *testing.T) {
	env := newProposalEnv(t)
	env.applyOK(t, watchCreateArgs)
	w := firstWatch(t, env.docs)

	out, err := env.deps.executeListWatches(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var list assistantListWatchesResult
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 || list.Limit != 20 || list.Watches[0].ScheduleSentence != "Every day at 06:00 boat time" || list.Watches[0].ID != w.ID ||
		list.Watches[0].Schedule != "0 6 * * *" || list.Watches[0].Version == "" || list.Watches[0].LookOutFor != "the fridge and the bilge" {
		t.Fatalf("%s", out)
	}
	if strings.Contains(strings.ToLower(out), "routine") {
		t.Error("the word routine is nowhere in what Mate reads")
	}

	// A run to read.
	review := "All quiet."
	if err := env.docs.RunTx("run", func(tx *sql.Tx, _ time.Time) error {
		return insertWatchRunTx(tx, watchRun{ID: "run-1", WatchID: w.ID, StartedAt: env.clock, FinishedAt: env.clock, Outcome: watchOutcomeRan,
			Report: json.RawMessage(`{"kind":"sections","sections":[]}`), Review: &review, CostKnown: true}, true)
	}); err != nil {
		t.Fatal(err)
	}
	out, err = env.deps.executeGetWatchRuns(context.Background(), json.RawMessage(`{"watch_id":"`+w.ID+`"}`))
	if err != nil || !strings.Contains(out, "All quiet.") || !strings.Contains(out, `"watch":"Morning report"`) {
		t.Fatalf("by watch: %s %v", out, err)
	}
	out, err = env.deps.executeGetWatchRuns(context.Background(), json.RawMessage(`{"run_id":"run-1"}`))
	if err != nil || !strings.Contains(out, "All quiet.") {
		t.Fatalf("by run: %s %v", out, err)
	}
	for name, args := range map[string]string{
		"neither":       `{}`,
		"wrong watch":   `{"watch_id":"other","run_id":"run-1"}`,
		"unknown watch": `{"watch_id":"nope"}`,
		"unknown run":   `{"run_id":"nope"}`,
	} {
		if _, err := env.deps.executeGetWatchRuns(context.Background(), json.RawMessage(args)); err == nil {
			t.Errorf("%s should fail plainly", name)
		}
	}

	// Run now goes through the scheduler's own host.
	f := newWatchHostFixture(t)
	f.docs = env.docs
	f.clock = env.clock
	prev := activeWatchHost
	activeWatchHost = f.host
	t.Cleanup(func() { activeWatchHost = prev })
	out, err = env.deps.executeRunWatchNow(context.Background(), json.RawMessage(`{"watch_id":"`+w.ID+`"}`))
	if err != nil || !strings.Contains(out, `"outcome":"ran"`) || len(f.reports) != 1 {
		t.Fatalf("run now: %s %v reports %v", out, err, f.reports)
	}
	if _, err := env.deps.executeRunWatchNow(context.Background(), json.RawMessage(`{"watch_id":"nope"}`)); err == nil {
		t.Error("an unknown watch fails plainly")
	}
	activeWatchHost = nil
	if _, err := env.deps.executeRunWatchNow(context.Background(), json.RawMessage(`{"watch_id":"`+w.ID+`"}`)); err == nil {
		t.Error("without a scheduler it fails plainly")
	}
}

func TestMateWatchToolsAreRegisteredAndReadOnlyForAnOrdinaryTurn(t *testing.T) {
	for _, name := range []string{"list_watches", "get_watch_runs", "run_watch_now"} {
		if !mateToolAvailable(assistantToolDeps{}, name) {
			t.Errorf("%s should be on offer", name)
		}
	}
}

// ── what the settings no longer carry ───────────────────────────────────

func TestSettingsCarryNoRoutineFields(t *testing.T) {
	raw, err := json.Marshal(buildSettingsPayload(map[string]any{"assistant": map[string]any{"routines_enabled": true, "routines_daily_cap_usd": 5.0}}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(raw)), "routine") {
		t.Fatalf("the settings payload has no routine fields: %s", raw)
	}
}

func TestOldRoutineTablesAreDroppedByTheSchema(t *testing.T) {
	env := newProposalEnv(t)
	if _, err := env.docs.db.Exec(`CREATE TABLE mate_routines (id TEXT); CREATE TABLE mate_routine_runs (id TEXT)`); err != nil {
		t.Fatal(err)
	}
	if err := createWatchesSchema(env.docs.db); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = env.docs.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('mate_routines', 'mate_routine_runs')`).Scan(&n)
	if n != 0 {
		t.Fatalf("%d old tables remain", n)
	}
}
