package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

const routineCreateArgs = `{"operations":[{"type":"routine","action":"create","fields":{"name":"Morning summary","cadence":"daily","daily_time":"06:00","focus":"battery, bilge and anything that changed overnight"}}]}`

func TestRoutine_CreateMakesTheConversationAndCardReadsInWords(t *testing.T) {
	env := newProposalEnv(t)
	p := env.propose(t, routineCreateArgs)
	if got, want := p.Ops[0].Description, "Add routine Morning summary: daily at 06:00 (boat time), looking at battery, bilge and anything that changed overnight"; got != want {
		t.Fatalf("description %q, want %q", got, want)
	}
	if p.Ops[0].After["cadence"] != "Daily" || p.Ops[0].After["daily_time"] != "06:00 (boat time)" {
		t.Errorf("the card shows words, got %v", p.Ops[0].After)
	}
	if _, ok := p.Ops[0].After["last_note"]; ok {
		t.Error("the scheduler's bookkeeping is not on the card")
	}
	env.advance()
	if _, err := env.apply(p.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	routines, _ := listRoutineRecords(env.docs, nil)
	if len(routines) != 1 {
		t.Fatalf("expected one routine, got %d", len(routines))
	}
	var r mateRoutine
	_ = env.docs.Read(func(q sqlQueryer) error { var err error; r, err = mateRoutineByIDQ(q, routines[0].ID); return err })
	if !r.Enabled || r.LastRunAt != nil {
		t.Errorf("a new routine is on and has not run: %+v", r)
	}
	conv, ok, err := env.asst.GetConversation(r.ConversationID)
	if err != nil || !ok || conv.Title != "Morning summary" || conv.RoutineID == nil || *conv.RoutineID != r.ID {
		t.Fatalf("routine conversation %+v ok=%v err=%v", conv, ok, err)
	}
	all, _ := env.asst.ListConversations()
	badged := 0
	for _, c := range all {
		if c.RoutineID != nil {
			badged++
		}
	}
	if badged != 1 {
		t.Errorf("the conversation list marks exactly the routine's conversation, got %d", badged)
	}
}

func TestRoutine_ValidationRefusesBadSchedulesAndTooMany(t *testing.T) {
	env := newProposalEnv(t)
	for name, tc := range map[string]struct{ fields, want string }{
		"daily without time": {`"name":"X","cadence":"daily","focus":"f"`, "daily_time:"},
		"bad time":           {`"name":"X","cadence":"daily","daily_time":"24:00","focus":"f"`, "daily_time:"},
		"hourly with time":   {`"name":"X","cadence":"hourly","daily_time":"06:00","focus":"f"`, "daily_time:"},
		"cadence":            {`"name":"X","cadence":"weekly","focus":"f"`, "cadence:"},
		"empty name":         {`"name":" ","cadence":"hourly","focus":"f"`, "name:"},
		"long name":          {`"name":"` + strings.Repeat("n", 61) + `","cadence":"hourly","focus":"f"`, "name:"},
		"no focus":           {`"name":"X","cadence":"hourly"`, "focus: is required"},
		"long focus":         {`"name":"X","cadence":"hourly","focus":"` + strings.Repeat("f", 501) + `"`, "focus:"},
	} {
		msg := proposeError(t, env.deps, `{"operations":[{"type":"routine","action":"create","fields":{`+tc.fields+`}}]}`)
		if !strings.Contains(msg, tc.want) {
			t.Errorf("%s: %q lacks %q", name, msg, tc.want)
		}
	}
	for i := 0; i < maxMateRoutines; i++ {
		env.applyOK(t, `{"operations":[{"type":"routine","action":"create","fields":{"name":"R","cadence":"hourly","focus":"f"}}]}`)
	}
	msg := proposeError(t, env.deps, `{"operations":[{"type":"routine","action":"create","fields":{"name":"Ninth","cadence":"hourly","focus":"f"}}]}`)
	if !strings.Contains(msg, "most there can be") {
		t.Errorf("the ninth routine is refused with a plain reason, got %q", msg)
	}
}

func TestRoutine_UpdateRenamesConversationAndHourlyDropsTheTime(t *testing.T) {
	env := newProposalEnv(t)
	env.applyOK(t, routineCreateArgs)
	routines, _ := listRoutineRecords(env.docs, nil)
	id := routines[0].ID
	env.advance()
	env.applyOK(t, `{"operations":[{"type":"routine","action":"update","id":"`+id+`","fields":{"name":"Hourly look","cadence":"hourly"}}]}`)
	var r mateRoutine
	_ = env.docs.Read(func(q sqlQueryer) error { var err error; r, err = mateRoutineByIDQ(q, id); return err })
	if r.Cadence != routineHourly || r.DailyTime != "" || r.Name != "Hourly look" {
		t.Fatalf("%+v", r)
	}
	conv, _, _ := env.asst.GetConversation(r.ConversationID)
	if conv.Title != "Hourly look" {
		t.Errorf("the conversation follows the name, got %q", conv.Title)
	}
}

func TestListRoutines_ReportsWhatTheBoatHas(t *testing.T) {
	env := newProposalEnv(t)
	env.applyOK(t, routineCreateArgs)
	out, err := env.deps.executeListRoutines(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var res assistantListRoutinesResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	if res.Total != 1 || res.Limit != 8 || res.Routines[0].Schedule != "Daily at 06:00 (boat time)" || res.Routines[0].ID == "" {
		t.Fatalf("%s", out)
	}
}

func TestUnattendedRunsAreNotOfferedProposalTools(t *testing.T) {
	d := assistantToolDeps{unattended: true}
	for _, name := range []string{"propose_changes", "start_watch", "fill_form", "inspect_form"} {
		if mateToolAvailable(d, name) {
			t.Errorf("%s must not be offered to a routine run", name)
		}
		if _, err := d.execute(context.Background(), name, nil); err == nil || !strings.Contains(err.Error(), "routine run") {
			t.Errorf("%s must be refused with the reason, got %v", name, err)
		}
	}
	if !mateToolAvailable(d, "get_vessel_status") || !mateToolAvailable(d, "list_routines") {
		t.Error("the read tools stay on offer")
	}
	if !mateToolAvailable(assistantToolDeps{}, "propose_changes") {
		t.Error("an ordinary turn keeps propose_changes")
	}
}

func TestRoutinePrompt(t *testing.T) {
	pc := assistantPromptContext{Now: time.Date(2026, 10, 29, 6, 0, 0, 0, time.UTC)}
	_, live := assistantSystemPromptParts(pc)
	if !strings.Contains(live, "do not run on their own") {
		t.Errorf("an ordinary turn is told routines are off:\n%s", live)
	}
	pc.RoutinesEnabled = true
	if _, live = assistantSystemPromptParts(pc); !strings.Contains(live, "Routines run on their own") {
		t.Errorf("an ordinary turn is told routines are on")
	}
	pc.RoutineRun = &mateRoutine{Name: "x"}
	stable, live := assistantSystemPromptParts(pc)
	for _, want := range []string{`exactly "Nothing to report."`, "unattended routine run", "at most three short findings", "do not propose changes", "do not raise it again"} {
		if !strings.Contains(live, want) {
			t.Errorf("routine rule lacks %q", want)
		}
	}
	if !strings.Contains(stable, "list_routines") || !strings.Contains(stable, "Let routines run on their own") {
		t.Error("the stable prompt teaches setting up a routine")
	}
}

// ── a real run through the turn launcher ────────────────────────────────

func routineConfig(t *testing.T, settingsPath string) mateRoutineConfig {
	t.Helper()
	cfg, err := readMateRoutineConfig(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func routineRunSetup(t *testing.T) (*proposalEnv, mateRoutine, string) {
	t.Helper()
	env := newProposalEnv(t)
	secrets := withTestSecretsStore(t)
	if err := secrets.Set("OPENROUTER_API_KEY", "sk-test"); err != nil {
		t.Fatal(err)
	}
	settings := writeAssistantSettingsFixture(t, true, "openai/gpt-4o")
	env.applyOK(t, routineCreateArgs)
	routines, _ := listRoutineRecords(env.docs, nil)
	var r mateRoutine
	_ = env.docs.Read(func(q sqlQueryer) error { var err error; r, err = mateRoutineByIDQ(q, routines[0].ID); return err })
	return env, r, settings
}

func TestRunMateRoutineTurn_AppendsTheInstructionAndReturnsTheHeadline(t *testing.T) {
	env, r, settings := routineRunSetup(t)
	var gotLive string
	var gotUnattended bool
	var gotHistory []openRouterMessage
	swapAssistantRunner(t, func(apiKey, model string, tools assistantToolDeps, ar assistantAutoRouterOptions, emit assistantEmitter) assistantRunnerFace {
		gotUnattended = tools.unattended
		return assistantRunnerFunc(func(ctx context.Context, stable, live string, history []openRouterMessage) (assistantReply, error) {
			gotLive, gotHistory = live, history
			return assistantReply{Content: "House bank is at 41%\n- Down 9 points since midnight: is the fridge running?", Model: "openai/gpt-4o", CostUSD: 0.02}, nil
		})
	})
	out, err := runMateRoutineTurn(context.Background(), routineConfig(t, settings), r, time.Date(2026, 10, 29, 6, 0, 0, 0, time.UTC), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if out.Note != "House bank is at 41%" || !out.Finding || out.Skipped || !out.Reached || !out.CostKnown || out.CostUSD != 0.02 {
		t.Fatalf("outcome %+v", out)
	}
	if !gotUnattended || !strings.Contains(gotLive, "unattended routine run") {
		t.Errorf("run is unattended: %v\n%s", gotUnattended, gotLive)
	}
	msgs, _ := env.asst.ListMessages(r.ConversationID)
	if len(msgs) != 2 || msgs[0].Role != "routine" || !strings.Contains(msgs[0].Content, "Focus: battery, bilge") || msgs[1].Role != "assistant" {
		t.Fatalf("messages %+v", msgs)
	}
	if len(gotHistory) != 1 || gotHistory[0].Role != "user" {
		t.Errorf("the instruction reaches the model as a user turn: %+v", gotHistory)
	}
	if _, busy := globalAssistantRuns.get(r.ConversationID); busy {
		t.Error("the run is released")
	}
}

func TestRunMateRoutineTurn_SkipsWhileTheOperatorIsMidTurn(t *testing.T) {
	env, r, settings := routineRunSetup(t)
	swapAssistantRunner(t, func(apiKey, model string, tools assistantToolDeps, ar assistantAutoRouterOptions, emit assistantEmitter) assistantRunnerFace {
		t.Fatal("no turn may start while the conversation is busy")
		return nil
	})
	prev := mateRoutineBusyWait
	mateRoutineBusyWait = 30 * time.Millisecond
	t.Cleanup(func() { mateRoutineBusyWait = prev })
	_, held, ok := globalAssistantRuns.start(r.ConversationID)
	if !ok {
		t.Fatal("could not hold the conversation")
	}
	defer globalAssistantRuns.remove(r.ConversationID, held)
	out, err := runMateRoutineTurn(context.Background(), routineConfig(t, settings), r, time.Now(), time.UTC)
	if err != nil || !out.Skipped || !strings.Contains(out.Note, "question") {
		t.Fatalf("%+v %v", out, err)
	}
	if msgs, _ := env.asst.ListMessages(r.ConversationID); len(msgs) != 0 {
		t.Errorf("a skipped run leaves nothing in the conversation: %+v", msgs)
	}
}

func TestRunMateRoutineTurn_AFailedTurnIsAnErrorNotARun(t *testing.T) {
	_, r, settings := routineRunSetup(t)
	swapAssistantRunner(t, func(apiKey, model string, tools assistantToolDeps, ar assistantAutoRouterOptions, emit assistantEmitter) assistantRunnerFace {
		return assistantRunnerFunc(func(ctx context.Context, stable, live string, history []openRouterMessage) (assistantReply, error) {
			return assistantReply{}, context.DeadlineExceeded
		})
	})
	out, err := runMateRoutineTurn(context.Background(), routineConfig(t, settings), r, time.Now(), time.UTC)
	if err == nil {
		t.Fatal("a turn that produced no reply is reported, not counted as a clean run")
	}
	if !out.Reached {
		t.Error("a turn that reached the model is reported as such, so its cost is on the ledger")
	}
}

func TestRunMateRoutineTurn_ADeletedConversationIsReported(t *testing.T) {
	env, r, settings := routineRunSetup(t)
	if err := env.asst.DeleteConversation(r.ConversationID); err != nil {
		t.Fatal(err)
	}
	if _, err := runMateRoutineTurn(context.Background(), routineConfig(t, settings), r, time.Now(), time.UTC); err == nil || !strings.Contains(err.Error(), "deleted") {
		t.Fatalf("%v", err)
	}
}

func TestMateRoutineHistoryKeepsTheLastEarlierRunsAndTheCurrentInstruction(t *testing.T) {
	var msgs []assistantMessage
	for i := 0; i < 6; i++ {
		msgs = append(msgs, assistantMessage{ID: fmt.Sprintf("i%d", i), Role: "routine"}, assistantMessage{ID: fmt.Sprintf("a%d", i), Role: "assistant"})
	}
	// The current run's instruction is the last row, with no reply yet.
	msgs = append(msgs, assistantMessage{ID: "now", Role: "routine"})
	got := mateRoutineHistory(msgs)
	if got[0].ID != "i3" || got[len(got)-1].ID != "now" || len(got) != 3*2+1 {
		ids := []string{}
		for _, m := range got {
			ids = append(ids, m.ID)
		}
		t.Fatalf("kept %v", ids)
	}
	if short := mateRoutineHistory(msgs[len(msgs)-3:]); len(short) != 3 {
		t.Errorf("a short history is kept whole, got %d", len(short))
	}
}

func TestMateRoutineInstructionPutsTheNameOnItsOwnLine(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 10, 29, 6, 0, 0, 0, time.UTC)
	for _, r := range []mateRoutine{
		{Name: "Morning summary", Cadence: routineDaily, DailyTime: "06:00", Focus: "batteries"},
		{Name: "Hourly look (bilge)", Cadence: routineHourly, Focus: "bilge"},
	} {
		lines := strings.Split(mateRoutineInstruction(r, now, loc), "\n")
		if lines[0] != "Routine run: "+r.Name || !strings.HasPrefix(lines[1], "Schedule: ") {
			t.Errorf("first lines %q", lines[:2])
		}
	}
}

func TestRoutineSpendLedgerSumsTheWindowAndFlagsUnknownCosts(t *testing.T) {
	env := newProposalEnv(t)
	now := env.clock
	add := func(id string, at time.Time, cost float64, known bool) {
		if err := env.docs.RunTx("ledger", func(tx *sql.Tx, _ time.Time) error {
			return insertMateRoutineRunTx(tx, mateRoutineRunRow{RoutineID: id, StartedAt: at, CostUSD: cost, CostKnown: known, Outcome: routineOutcomeRan})
		}); err != nil {
			t.Fatal(err)
		}
	}
	add("r1", now.Add(-30*time.Hour), 5, true)
	add("r1", now.Add(-2*time.Hour), 0.25, true)
	add("r2", now.Add(-1*time.Hour), 0.5, true)
	got, err := mateRoutineSpendSince(env.docs, now.Add(-24*time.Hour))
	if err != nil || got.USD != 0.75 || got.UnknownAt != nil {
		t.Fatalf("spend %+v err %v", got, err)
	}
	add("r1", now.Add(-3*time.Hour), 0, false)
	got, _ = mateRoutineSpendSince(env.docs, now.Add(-24*time.Hour))
	if got.UnknownAt == nil || !got.UnknownAt.Equal(now.Add(-3*time.Hour)) {
		t.Fatalf("an unknown cost is flagged: %+v", got)
	}
}

func TestAssistantCostMeter_KeepsTheCostOfAFailedRun(t *testing.T) {
	round0 := chatResponse(t, http.StatusOK, openRouterChatResponse{
		Model: "m",
		Choices: []openRouterChoice{{Message: openRouterMessage{Role: "assistant", ToolCalls: []openRouterToolCall{
			{ID: "c1", Type: "function", Function: openRouterToolCallFunction{Name: "find_places", Arguments: openRouterArguments(`{}`)}},
		}}}},
		Usage: usage(100, 20, 0.01),
	})
	run := func(second *http.Response, secondErr error) (float64, bool) {
		doer := &queuedChatDoer{responses: []*http.Response{round0, second}, errs: []error{nil, secondErr}}
		// round0 is consumed once; rebuild per run.
		round0 = chatResponse(t, http.StatusOK, openRouterChatResponse{
			Model: "m",
			Choices: []openRouterChoice{{Message: openRouterMessage{Role: "assistant", ToolCalls: []openRouterToolCall{
				{ID: "c1", Type: "function", Function: openRouterToolCallFunction{Name: "find_places", Arguments: openRouterArguments(`{}`)}},
			}}}},
			Usage: usage(100, 20, 0.01),
		})
		meter := &assistantCostMeter{}
		emit, _ := recordingEmitter()
		runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: &fakeToolExecutor{results: map[string]string{"find_places": `{}`}}, emit: emit}
		if _, err := runner.run(withAssistantCostMeter(context.Background(), meter), "system", "", nil); err == nil {
			t.Fatal("the second round fails")
		}
		return meter.snapshot()
	}
	// The connection drops in the second round: what it cost is not known.
	if usd, known := run(nil, errors.New("connection reset")); usd != 0.01 || known {
		t.Errorf("transport failure: %v known=%v", usd, known)
	}
	// OpenRouter refused the second request outright: nothing was charged for it.
	refused := chatResponse(t, http.StatusInternalServerError, openRouterChatResponse{Error: &openRouterAPIError{Message: "boom"}})
	if usd, known := run(refused, nil); usd != 0.01 || !known {
		t.Errorf("refused request: %v known=%v", usd, known)
	}
}

func TestDeletingARoutinesConversationIsRefusedWhileTheRoutineExists(t *testing.T) {
	env, r, _ := routineRunSetup(t)
	rec := routineRequest(t, http.MethodDelete, "/x", "", deleteAssistantConversationHandler, r.ConversationID)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `This conversation belongs to the routine \"Morning summary\". Delete the routine in Mate settings first.`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if _, ok, _ := env.asst.GetConversation(r.ConversationID); !ok {
		t.Fatal("the conversation is still there")
	}
	if err := env.docs.RunTx("del", func(tx *sql.Tx, _ time.Time) error { return deleteMateRoutineTx(tx, r.ID) }); err != nil {
		t.Fatal(err)
	}
	if rec = routineRequest(t, http.MethodDelete, "/x", "", deleteAssistantConversationHandler, r.ConversationID); rec.Code != http.StatusNoContent {
		t.Fatalf("once the routine is gone the conversation deletes normally: %d %s", rec.Code, rec.Body)
	}
}

func TestRoutineCapMustBeReadableInTheSettingsFile(t *testing.T) {
	for _, bad := range []string{`"lots"`, "-1", "0.01", "25"} {
		dir := t.TempDir()
		path := dir + "/settings.yaml"
		if err := os.WriteFile(path, []byte("assistant:\n    enabled: true\n    routines_daily_cap_usd: "+bad+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("SETTINGS_FILE", path)
		rec := routineRequest(t, http.MethodGet, "/api/settings", "", getSettingsHandler, "")
		if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "routines_daily_cap_usd") {
			t.Errorf("%s: GET settings must fail loudly, got %d %s", bad, rec.Code, rec.Body)
		}
		if _, err := readMateRoutineConfig(path); err == nil {
			t.Errorf("%s: the scheduler must not run on an unreadable limit", bad)
		}
	}
}

// ── REST ────────────────────────────────────────────────────────────────

func routineRequest(t *testing.T, method, path, body string, h echo.HandlerFunc, id string) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if id != "" {
		c.SetParamNames("id")
		c.SetParamValues(id)
	}
	if err := h(c); err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestRoutineREST_ListSwitchAndDelete(t *testing.T) {
	env, r, settings := routineRunSetup(t)
	t.Setenv("SETTINGS_FILE", settings)

	rec := routineRequest(t, http.MethodGet, "/api/assistant/routines", "", listMateRoutinesHandler, "")
	var list mateRoutinesResponse
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &list) != nil || len(list.Routines) != 1 || list.Max != 8 || list.DailyCapUSD != defaultRoutinesDailyCapUSD || list.RoutinesEnabled {
		t.Fatalf("list %d %s", rec.Code, rec.Body)
	}

	if rec = routineRequest(t, http.MethodPatch, "/x", `{"enabled":false}`, patchMateRoutineHandler, r.ID); rec.Code != 200 {
		t.Fatalf("patch %d %s", rec.Code, rec.Body)
	}
	_ = env.docs.Read(func(q sqlQueryer) error { var err error; r, err = mateRoutineByIDQ(q, r.ID); return err })
	if r.Enabled {
		t.Error("the switch is off")
	}
	if rec = routineRequest(t, http.MethodPatch, "/x", `{"name":"other"}`, patchMateRoutineHandler, r.ID); rec.Code != 400 {
		t.Errorf("only the switch changes here, got %d", rec.Code)
	}
	if rec = routineRequest(t, http.MethodPatch, "/x", `{"enabled":true}`, patchMateRoutineHandler, "nope"); rec.Code != 404 {
		t.Errorf("unknown id %d", rec.Code)
	}
	if rec = routineRequest(t, http.MethodDelete, "/x", "", deleteMateRoutineHandler, r.ID); rec.Code != 204 {
		t.Fatalf("delete %d", rec.Code)
	}
	if rec = routineRequest(t, http.MethodDelete, "/x", "", deleteMateRoutineHandler, r.ID); rec.Code != 404 {
		t.Errorf("second delete %d", rec.Code)
	}
	if _, ok, _ := env.asst.GetConversation(r.ConversationID); !ok {
		t.Error("the findings stay after the routine is deleted")
	}
}

// ── settings ────────────────────────────────────────────────────────────

func TestRoutineSettings(t *testing.T) {
	payload := buildSettingsPayload(map[string]any{})
	if payload.Assistant.RoutinesEnabled || payload.Assistant.RoutinesDailyCapUSD != 1.0 {
		t.Errorf("defaults: off and $1.00, got %+v", payload.Assistant)
	}
	payload = buildSettingsPayload(map[string]any{"assistant": map[string]any{"routines_enabled": true, "routines_daily_cap_usd": 2.5}})
	if !payload.Assistant.RoutinesEnabled || payload.Assistant.RoutinesDailyCapUSD != 2.5 {
		t.Errorf("stored values: %+v", payload.Assistant)
	}
	var req settingsPayload
	req.Assistant.RoutinesDailyCapUSD = 0
	if got := normalizeSettingsPayload(req).Assistant.RoutinesDailyCapUSD; got != 1.0 {
		t.Errorf("an unset limit takes the default, got %v", got)
	}
	for _, limit := range []float64{0.04, 20.01, -1} {
		next := buildSettingsPayload(map[string]any{})
		next.Assistant.RoutinesDailyCapUSD = limit
		if v := validateSettingsChange(buildSettingsPayload(map[string]any{}), next); v == nil || v.Field != "assistant.routines_daily_cap_usd" {
			t.Errorf("limit %v must be refused, got %+v", limit, v)
		}
	}
	for _, limit := range []float64{0.05, 1, 20} {
		next := buildSettingsPayload(map[string]any{})
		next.Assistant.RoutinesDailyCapUSD = limit
		if v := validateSettingsChange(buildSettingsPayload(map[string]any{}), next); v != nil {
			t.Errorf("limit %v is allowed, got %+v", limit, v)
		}
	}
}
