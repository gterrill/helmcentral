package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// What a run records of its own thinking (ADR 0240 follow-up, the activity
// view): each round's visible reasoning, its chatter, the triggers that sent a
// draft back, and how long the run thought before the answer began. Nothing
// here reaches the operator's stream or the conversation.

func activityChunk(delta string) string {
	return `data: {"id":"g","model":"m","choices":[{"index":0,"delta":` + delta + `}]}` + "\n\n"
}

func activityFinish(reason string) string {
	return `data: {"id":"g","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"` + reason + `"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}` + "\n\n" + "data: [DONE]\n\n"
}

func activityText(text string) string {
	b, _ := json.Marshal(text)
	return `{"reasoning_details":[{"type":"reasoning.text","text":` + string(b) + `,"index":0}]}`
}

func activityContent(text string) string {
	b, _ := json.Marshal(text)
	return `{"content":` + string(b) + `}`
}

const activityToolDelta = `{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_tides","arguments":"{\"lat\":1}"}}]}`

func activityResponse(parts ...string) *http.Response {
	return openRouterFakeResponse(200, strings.Join(parts, ""))
}

// brokenStream sends its first chunk, then either fails or waits for the
// request to be cancelled. reachedEnd is closed when the reader is asked for
// more after the chunk, so a test knows the chunk has been handled.
type brokenStream struct {
	ctx        context.Context
	chunk      string
	sent       bool
	failWith   error
	reachedEnd chan struct{}
}

func (b *brokenStream) Read(p []byte) (int, error) {
	if !b.sent {
		b.sent = true
		return copy(p, b.chunk), nil
	}
	close(b.reachedEnd)
	if b.failWith != nil {
		return 0, b.failWith
	}
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *brokenStream) Close() error { return nil }

func runWithRecorder(t *testing.T, doer openRouterDoer, tools assistantToolExecutor, opts assistantRunnerOptions) (assistantReply, error, *assistantRunRecorder) {
	t.Helper()
	emit, _ := recordingEmitter()
	rec := &assistantRunRecorder{}
	runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: tools, emit: emit, options: opts}
	reply, err := runner.run(withAssistantRunRecorder(context.Background(), rec), "system", "", nil)
	return reply, err, rec
}

func TestActivity_AReasoningRoundWithNoToolsIsRecordedAsText(t *testing.T) {
	doer := queued(activityResponse(
		activityChunk(`{"role":"assistant",`+activityText("need ")[1:]),
		activityChunk(activityText("the tide")),
		activityChunk(activityContent("High at six.")),
		activityFinish("stop"),
	))
	reply, err, rec := runWithRecorder(t, doer, &fakeToolExecutor{}, assistantRunnerOptions{})
	if err != nil || reply.Content != "High at six." {
		t.Fatalf("reply %q err %v", reply.Content, err)
	}
	rounds, _ := rec.result()
	if len(rounds) != 1 {
		t.Fatalf("rounds = %+v", rounds)
	}
	r := rounds[0]
	if r.Reasoning != "need the tide" || r.ReasoningKind != "text" || r.ReasoningBytes != len("need the tide") || r.Chatter != "" {
		t.Fatalf("round = %+v", r)
	}
	if ms := assistantAnswerThought(rounds); ms == nil || *ms < 0 {
		t.Fatalf("a run that answered must have a thought time, got %v", ms)
	}
}

func TestActivity_ToolsWithNoReasoningAreKindNone(t *testing.T) {
	doer := queued(newCallRound(t, 0), finalResponse(t, "done", "m", openRouterUsage{}))
	_, err, rec := runWithRecorder(t, doer, &fakeToolExecutor{results: map[string]string{"get_tides": `{}`}}, assistantRunnerOptions{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	rounds, _ := rec.result()
	for _, r := range rounds {
		if r.ReasoningKind != "none" || r.Reasoning != "" || r.ReasoningBytes != 0 {
			t.Errorf("round %d = %+v, want kind none and no text", r.Index, r)
		}
	}
}

func TestActivity_ARoundThatNeverStreamedIsNotRecordedRatherThanNone(t *testing.T) {
	doer := queued(openRouterFakeResponse(500, `{"error":{"message":"upstream down"}}`))
	_, err, rec := runWithRecorder(t, doer, &fakeToolExecutor{}, assistantRunnerOptions{})
	if err == nil {
		t.Fatal("expected the completion to fail")
	}
	rounds, _ := rec.result()
	if len(rounds) != 1 || rounds[0].Kind != assistantRoundFailed || rounds[0].ReasoningKind != "" {
		t.Fatalf("rounds = %+v, want one failed round with reasoning_kind not recorded", rounds)
	}
	if got := assistantRunReasoningKind(rounds); got != "" {
		t.Errorf("run kind = %q, want not recorded", got)
	}
}

func activitySummary(text string) string {
	b, _ := json.Marshal(text)
	return `{"reasoning_details":[{"type":"reasoning.summary","summary":` + string(b) + `,"index":0}]}`
}

const activityEncrypted = `{"reasoning_details":[{"type":"reasoning.encrypted","data":"opaque","index":1}]}`

func TestActivity_SeparateReasoningEntriesAreKeptApartAndOneEntryIsNot(t *testing.T) {
	doer := queued(activityResponse(
		activityChunk(`{"role":"assistant",`+activitySummary("Check")[1:]),
		activityChunk(activitySummary("ing tides.")),
		activityChunk(activityEncrypted),
		activityChunk(activitySummary("Now wind.")),
		activityChunk(activityContent("High at six.")),
		activityFinish("stop"),
	))
	_, err, rec := runWithRecorder(t, doer, &fakeToolExecutor{}, assistantRunnerOptions{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	rounds, _ := rec.result()
	want := "Checking tides.\n\nNow wind."
	if r := rounds[0]; r.Reasoning != want || r.ReasoningBytes != len("Checking tides.Now wind.") {
		t.Fatalf("reasoning = %q (%d bytes), want %q: token fragments of one entry join, separate entries get a blank line that is not counted", r.Reasoning, r.ReasoningBytes, want)
	}
}

func TestActivity_AnAnswerThatStartsWithWhitespaceStampsTheFirstRealText(t *testing.T) {
	c := newAssistantRoundCapture(func(string) {}, time.Now())
	hooks := c.hooks()
	hooks.Content("\n")
	time.Sleep(40 * time.Millisecond)
	hooks.Content("Answer")
	var tr assistantTraceRound
	c.finish(&tr)
	if tr.ContentStartMs == nil || *tr.ContentStartMs < 40 {
		t.Fatalf("content_start_ms = %v, want at least 40 (the second fragment)", tr.ContentStartMs)
	}
	if tr.Chatter != "\nAnswer" {
		t.Errorf("chatter = %q", tr.Chatter)
	}
}

func TestActivity_SaveRunReturnsWhatItStored(t *testing.T) {
	store := newTestAssistantStore(t)
	conv, _ := store.CreateConversation("Passage")
	msg, _ := store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "assistant", Content: "ok"})
	in := sampleRunTrace(conv.ID, 1, &msg.ID, assistantStopAnswered)
	thought := int64(4200)
	in.Rounds[1].ContentStartMs = &thought
	in.Rounds[0].ReasoningKind = "private"
	saved, err := store.SaveRun(in)
	if err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	stored, _, _ := store.GetRun(saved.ID)
	if saved.ThoughtMs == nil || *saved.ThoughtMs != 4200 || saved.ReasoningKind != "private" || saved.LookupCount != stored.LookupCount || saved.LookupCount == 0 {
		t.Errorf("returned run = thought %v lookups %d kind %q, stored lookups %d", saved.ThoughtMs, saved.LookupCount, saved.ReasoningKind, stored.LookupCount)
	}
}

func TestActivity_EveryNotRunErrorIsSharedWithTheLookupCounter(t *testing.T) {
	for _, e := range []string{assistantRoundFullError, assistantToolWithheldError} {
		if !strings.HasPrefix(e, assistantNotRunPrefix) {
			t.Errorf("%q does not start with the shared prefix", e)
		}
		rounds := []assistantTraceRound{{Calls: []assistantToolCall{{Name: "get_tides", Error: e}, {Name: "get_tides"}, {Name: "get_tides", Error: "boom"}}}}
		if got := assistantRunLookupCount(rounds); got != 2 {
			t.Errorf("%q: lookups = %d, want 2 (the not-run call is not one, a failed call is)", e, got)
		}
	}
}

func TestActivity_TheRunJSONKeepsItsKindApartFromTheRoundsText(t *testing.T) {
	b, err := json.Marshal(assistantRunTrace{ReasoningKind: "text", Rounds: []assistantTraceRound{{Reasoning: "x"}}})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Kind   string `json:"reasoning_kind"`
		Rounds []struct {
			Reasoning string `json:"reasoning"`
		} `json:"rounds"`
	}
	if err := json.Unmarshal(b, &got); err != nil || got.Kind != "text" || got.Rounds[0].Reasoning != "x" {
		t.Fatalf("json = %s", b)
	}
	if strings.Contains(string(b), `"reasoning":"text"`) {
		t.Errorf("the run's kind must not use the key the rounds use for text: %s", b)
	}
}

func TestActivity_EncryptedOnlyReasoningIsPrivateAndStillRoundTrips(t *testing.T) {
	body, err := os.ReadFile("testdata/openrouter_stream_reasoning_toolcall.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	doer := queued(openRouterFakeResponse(200, string(body)), finalResponse(t, "done", "m", openRouterUsage{}))
	_, runErr, rec := runWithRecorder(t, doer, &fakeToolExecutor{results: map[string]string{"get_tide_height": `{}`}}, assistantRunnerOptions{})
	if runErr != nil {
		t.Fatalf("run: %v", runErr)
	}
	rounds, _ := rec.result()
	if rounds[0].ReasoningKind != "private" || rounds[0].Reasoning != "" || rounds[0].ReasoningBytes != 0 {
		t.Fatalf("round 0 = %+v, want private with no text", rounds[0])
	}
	if !strings.Contains(string(doer.rawBodies[1]), `"reasoning.encrypted"`) {
		t.Fatalf("the encrypted block must still go back on the next request: %s", doer.rawBodies[1])
	}
}

func TestActivity_ACancelMidReasoningKeepsWhatWasThought(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reached := make(chan struct{})
	doer := budgetDoerFunc(func(req *http.Request) (*http.Response, error) {
		body := &brokenStream{ctx: req.Context(), chunk: activityChunk(`{"role":"assistant",` + activityText("partial thought")[1:]), reachedEnd: reached}
		return &http.Response{StatusCode: 200, Body: body, Header: make(http.Header)}, nil
	})
	go func() {
		<-reached
		cancel()
	}()
	emit, _ := recordingEmitter()
	rec := &assistantRunRecorder{}
	runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: &fakeToolExecutor{}, emit: emit}
	_, err := runner.run(withAssistantRunRecorder(ctx, rec), "system", "", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want a cancel", err)
	}
	rounds, stop := rec.result()
	if stop != assistantStopCancelled || len(rounds) != 1 {
		t.Fatalf("stop %q rounds %+v", stop, rounds)
	}
	if r := rounds[0]; r.Kind != assistantRoundFailed || r.Reasoning != "partial thought" || r.ReasoningKind != "text" {
		t.Fatalf("round = %+v, want a failed round holding the partial reasoning", r)
	}
	if assistantAnswerThought(rounds) != nil {
		t.Fatal("a run with no answer has no thought time")
	}
}

func TestActivity_ACompletionErrorMidReasoningSavesTheRoundAsFailed(t *testing.T) {
	reached := make(chan struct{})
	doer := budgetDoerFunc(func(req *http.Request) (*http.Response, error) {
		body := &brokenStream{ctx: req.Context(), chunk: activityChunk(`{"role":"assistant",` + activityText("half a thought")[1:]), failWith: errors.New("connection reset"), reachedEnd: reached}
		return &http.Response{StatusCode: 200, Body: body, Header: make(http.Header)}, nil
	})
	_, err, rec := runWithRecorder(t, doer, &fakeToolExecutor{}, assistantRunnerOptions{})
	if err == nil {
		t.Fatal("expected the stream error")
	}
	rounds, _ := rec.result()
	if len(rounds) != 1 || rounds[0].Kind != assistantRoundFailed || rounds[0].Error == "" || rounds[0].Reasoning != "half a thought" || rounds[0].ReasoningKind != "text" {
		t.Fatalf("rounds = %+v", rounds)
	}
}

func TestActivity_ARoundThatFailsMidStreamKeepsItsPartialTextAsChatter(t *testing.T) {
	reached := make(chan struct{})
	doer := budgetDoerFunc(func(req *http.Request) (*http.Response, error) {
		body := &brokenStream{ctx: req.Context(), chunk: activityChunk(`{"role":"assistant",` + activityContent("Let me chec")[1:]), failWith: errors.New("connection reset"), reachedEnd: reached}
		return &http.Response{StatusCode: 200, Body: body, Header: make(http.Header)}, nil
	})
	_, err, rec := runWithRecorder(t, doer, &fakeToolExecutor{}, assistantRunnerOptions{})
	if err == nil {
		t.Fatal("expected the stream error")
	}
	rounds, _ := rec.result()
	if len(rounds) != 1 || rounds[0].Kind != assistantRoundFailed || rounds[0].Chatter != "Let me chec" {
		t.Fatalf("rounds = %+v, want a failed round keeping its partial text as chatter", rounds)
	}
}

func TestActivity_ReasoningIsCutAtThirtyTwoKiBKeepingTheHead(t *testing.T) {
	long := strings.Repeat("é", 20000) // two bytes each, 40000 in all
	doer := queued(activityResponse(
		activityChunk(`{"role":"assistant",`+activityText(long)[1:]),
		activityChunk(activityContent("ok")),
		activityFinish("stop"),
	))
	_, err, rec := runWithRecorder(t, doer, &fakeToolExecutor{}, assistantRunnerOptions{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	r := func() assistantTraceRound { rs, _ := rec.result(); return rs[0] }()
	if r.ReasoningBytes != len(long) {
		t.Errorf("reasoning_bytes = %d, want the original %d", r.ReasoningBytes, len(long))
	}
	// The runner keeps the head and its size; only the store cuts it to the cap.
	if r.Reasoning == "" || !strings.HasPrefix(long, r.Reasoning) || !utf8.ValidString(r.Reasoning) {
		t.Errorf("reasoning must be a valid head of what streamed, got %d bytes", len(r.Reasoning))
	}
	store := newTestAssistantStore(t)
	conv, _ := store.CreateConversation("Passage")
	in := sampleRunTrace(conv.ID, 1, nil, assistantStopAnswered)
	in.Rounds[0].Reasoning = r.Reasoning
	saved, _ := store.SaveRun(in)
	got, _, _ := store.GetRun(saved.ID)
	if s := got.Rounds[0].Reasoning; len(s) == 0 || len(s) > assistantReasoningMaxBytes || !strings.HasPrefix(long, s) || !utf8.ValidString(s) {
		t.Errorf("stored reasoning must be a valid head of at most %d bytes, got %d bytes", assistantReasoningMaxBytes, len(s))
	}
}

func TestActivity_ChatterIsTheTextOfARoundThatEndedInToolCalls(t *testing.T) {
	round0 := activityResponse(
		activityChunk(`{"role":"assistant",`+activityContent("Let me check the tide.")[1:]),
		activityChunk(activityToolDelta),
		activityFinish("tool_calls"),
	)
	doer := queued(round0, finalResponse(t, "High at six.", "m", openRouterUsage{}))
	reply, err, rec := runWithRecorder(t, doer, &fakeToolExecutor{results: map[string]string{"get_tides": `{}`}}, assistantRunnerOptions{})
	if err != nil || reply.Content != "High at six." {
		t.Fatalf("reply %q err %v", reply.Content, err)
	}
	rounds, _ := rec.result()
	if rounds[0].Chatter != "Let me check the tide." || rounds[1].Chatter != "" {
		t.Fatalf("chatter = %q then %q", rounds[0].Chatter, rounds[1].Chatter)
	}
}

func TestActivity_ASentBackDraftKeepsItsTriggersAndIsNotChatter(t *testing.T) {
	_, _, rec, _ := thursdayRun(t, thursdayDraft, thursdayAsk)
	rounds, _ := rec.result()
	var found bool
	for _, r := range rounds {
		if r.Kind != assistantRoundCorrection {
			if len(r.CheckTriggers) != 0 {
				t.Errorf("only the correction round carries triggers: %+v", r)
			}
			continue
		}
		found = true
		if len(r.CheckTriggers) != 1 || r.CheckTriggers[0] != "anchorage shelter" || r.CheckTrigger != "" {
			t.Errorf("the list is the only source the runner sets: %v / %q", r.CheckTriggers, r.CheckTrigger)
		}
		if r.CheckDraft != thursdayDraft || r.Chatter != "" {
			t.Errorf("the draft belongs in check_draft only: draft %q chatter %q", r.CheckDraft, r.Chatter)
		}
	}
	if !found {
		t.Fatalf("no correction round: %+v", rounds)
	}
	// The draft was sent back and then answered: the run thought until the
	// second round's text began, not the first's.
	last := rounds[len(rounds)-1]
	if got := assistantAnswerThought(rounds); got == nil || last.ContentStartMs == nil || got != last.ContentStartMs {
		t.Errorf("thought = %v, want the answer round's %v", got, last.ContentStartMs)
	}
}

func TestActivity_TwoTriggersAreAListAndTheOldColumnStaysJoined(t *testing.T) {
	text := "A northerly gives you a head sea, and the bay is sheltered from the north."
	triggers, _ := pwToolDepsWithCoastline(passageLimits{}).checkDraftTriggers(text, nil)
	if len(triggers) != 2 || triggers[0] != "relative wind" || triggers[1] != "anchorage shelter" {
		t.Fatalf("triggers = %v", triggers)
	}
	store := newTestAssistantStore(t)
	conv, _ := store.CreateConversation("Passage")
	in := sampleRunTrace(conv.ID, 1, nil, assistantStopAnswered)
	in.Rounds[1].CheckTriggers = triggers
	saved, err := store.SaveRun(in)
	if err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	got, _, _ := store.GetRun(saved.ID)
	if r := got.Rounds[1]; r.CheckTrigger != "relative wind and anchorage shelter" || len(r.CheckTriggers) != 2 || r.CheckTriggers[1] != "anchorage shelter" {
		t.Fatalf("round = %+v", r)
	}
	if r := got.Rounds[0]; r.CheckTriggers == nil || len(r.CheckTriggers) != 0 {
		t.Fatalf("a round with none reads an empty list, not null: %#v", r.CheckTriggers)
	}
}

func TestActivity_TheStoreKeepsTheNewFieldsAndSummarisesTheRun(t *testing.T) {
	store := newTestAssistantStore(t)
	conv, _ := store.CreateConversation("Passage")
	msg, _ := store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "assistant", Content: "ok"})
	in := sampleRunTrace(conv.ID, 1, &msg.ID, assistantStopAnswered)
	// The run's thought time is the answer round's, not an earlier round's.
	early, thought := int64(100), int64(4200)
	in.Rounds[0].ContentStartMs, in.Rounds[1].ContentStartMs = &early, &thought
	in.Rounds[0].Reasoning, in.Rounds[0].ReasoningKind, in.Rounds[0].ReasoningBytes = "thinking", "text", 900
	in.Rounds[0].Chatter = "checking"
	in.Rounds[0].Calls = append(in.Rounds[0].Calls, assistantToolCall{Seq: 2, Name: "get_tides", Error: assistantToolWithheldError})
	in.Rounds[1].ReasoningKind = "private"
	saved, err := store.SaveRun(in)
	if err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	got, ok, err := store.GetRun(saved.ID)
	if err != nil || !ok {
		t.Fatalf("GetRun: %v %v", ok, err)
	}
	if r := got.Rounds[0]; r.Reasoning != "thinking" || r.ReasoningKind != "text" || r.ReasoningBytes != 900 || r.Chatter != "checking" {
		t.Errorf("round 0 = %+v", r)
	}
	if got.ThoughtMs == nil || *got.ThoughtMs != 4200 || got.LookupCount != 2 || got.ReasoningKind != "text" {
		t.Errorf("run = thought %v lookups %d reasoning %q, want 4200 / 2 (the withheld call does not count) / text", got.ThoughtMs, got.LookupCount, got.ReasoningKind)
	}
	runs, _ := store.ListRuns(conv.ID)
	if len(runs) != 1 || runs[0].ThoughtMs == nil || *runs[0].ThoughtMs != 4200 || runs[0].LookupCount != 2 || runs[0].ReasoningKind != "text" || runs[0].Rounds != nil {
		t.Errorf("list = %+v", runs)
	}

	// Run-level kind: text beats private beats none beats not recorded.
	for _, tc := range []struct {
		kinds [2]string
		want  string
	}{
		{[2]string{"none", "private"}, "private"},
		{[2]string{"none", "none"}, "none"},
		{[2]string{"", ""}, ""},
	} {
		in := sampleRunTrace(conv.ID, 2, nil, assistantStopAnswered)
		in.Rounds[0].ReasoningKind, in.Rounds[1].ReasoningKind = tc.kinds[0], tc.kinds[1]
		s, _ := store.SaveRun(in)
		if g, _, _ := store.GetRun(s.ID); g.ReasoningKind != tc.want {
			t.Errorf("kinds %v: run reasoning = %q, want %q", tc.kinds, g.ReasoningKind, tc.want)
		}
	}
}

func TestActivity_TheStoreCapsReasoningAndChatterItself(t *testing.T) {
	store := newTestAssistantStore(t)
	conv, _ := store.CreateConversation("Passage")
	in := sampleRunTrace(conv.ID, 1, nil, assistantStopAnswered)
	in.Rounds[0].Reasoning = strings.Repeat("r", assistantReasoningMaxBytes+500)
	in.Rounds[0].Chatter = strings.Repeat("c", assistantToolCallMaxBytes+500)
	saved, _ := store.SaveRun(in)
	got, _, _ := store.GetRun(saved.ID)
	if len(got.Rounds[0].Reasoning) != assistantReasoningMaxBytes || len(got.Rounds[0].Chatter) != assistantToolCallMaxBytes {
		t.Fatalf("lengths = %d, %d", len(got.Rounds[0].Reasoning), len(got.Rounds[0].Chatter))
	}
}

func TestActivity_OldRowsReadAsNotRecorded(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "old.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range []string{
		`CREATE TABLE assistant_runs (id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL, after_seq INTEGER NOT NULL, answer_message_id TEXT, model TEXT NOT NULL DEFAULT '', started_at INTEGER NOT NULL, duration_ms INTEGER NOT NULL, stop_reason TEXT NOT NULL, error TEXT NOT NULL DEFAULT '', budget TEXT NOT NULL)`,
		`CREATE TABLE assistant_run_rounds (run_id TEXT NOT NULL REFERENCES assistant_runs(id) ON DELETE CASCADE, idx INTEGER NOT NULL, kind TEXT NOT NULL DEFAULT '', start_offset_ms INTEGER NOT NULL, latency_ms INTEGER NOT NULL, prompt_tokens INTEGER NOT NULL DEFAULT 0, completion_tokens INTEGER NOT NULL DEFAULT 0, cached_tokens INTEGER NOT NULL DEFAULT 0, cost_usd REAL NOT NULL DEFAULT 0, forced_final INTEGER NOT NULL DEFAULT 0, progress INTEGER NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT '', check_trigger TEXT NOT NULL DEFAULT '', check_draft TEXT NOT NULL DEFAULT '', PRIMARY KEY (run_id, idx))`,
		`INSERT INTO assistant_runs VALUES ('old', 'c', 1, 'm1', 'm', 1, 1, 'answered', '', '{}')`,
		`INSERT INTO assistant_run_rounds (run_id, idx, kind, start_offset_ms, latency_ms, check_trigger) VALUES ('old', 0, 'correction', 0, 5, 'relative wind and anchorage shelter')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := createAssistantRunsSchema(db); err != nil {
			t.Fatalf("pass %d: %v", i, err)
		}
	}
	store := &assistantStore{db: db, now: time.Now}
	got, ok, err := store.GetRun("old")
	if err != nil || !ok {
		t.Fatalf("GetRun: %v %v", ok, err)
	}
	r := got.Rounds[0]
	if r.Reasoning != "" || r.ReasoningKind != "" || r.ReasoningBytes != 0 || r.Chatter != "" || r.CheckTriggers == nil || len(r.CheckTriggers) != 0 || r.CheckTrigger != "relative wind and anchorage shelter" {
		t.Errorf("old round = %+v", r)
	}
	if got.ThoughtMs != nil || got.LookupCount != 0 || got.ReasoningKind != "" {
		t.Errorf("old run = thought %v lookups %d reasoning %q", got.ThoughtMs, got.LookupCount, got.ReasoningKind)
	}
	runs, err := store.ListRuns("c")
	if err != nil || len(runs) != 1 || runs[0].ThoughtMs != nil || runs[0].ReasoningKind != "" {
		t.Errorf("old run list = %+v err %v", runs, err)
	}
}

func TestActivity_TheConversationListsThoughtLookupsAndReasoningForAnAnswer(t *testing.T) {
	store, conv := readyAssistant(t)
	swapAssistantRunner(t, func(apiKey, model string, tools assistantToolDeps, options assistantRunnerOptions, emit assistantEmitter) assistantRunnerFace {
		doer := queued(
			activityResponse(activityChunk(`{"role":"assistant",`+activityText("thinking about tides")[1:]), activityChunk(activityToolDelta), activityFinish("tool_calls")),
			finalResponse(t, "High at six.", "m", usage(1, 1, 0)),
		)
		return &assistantRunner{doer: doer, apiKey: apiKey, model: model, tools: &fakeToolExecutor{results: map[string]string{"get_tides": `{}`}}, emit: emit}
	})
	postQuestion(t, conv.ID)

	c, rec := newAssistantEchoContext(http.MethodGet, "/api/assistant/conversations/"+conv.ID, "", conv.ID)
	if err := getAssistantConversationHandler(c); err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	var body struct {
		Runs []map[string]any `json:"runs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body.Runs) != 1 {
		t.Fatalf("runs = %v err %v", body.Runs, err)
	}
	got := body.Runs[0]
	if _, ok := got["thought_ms"].(float64); !ok || got["lookups"] != float64(1) || got["reasoning_kind"] != "text" {
		t.Fatalf("ref = %v, want thought_ms, lookups 1 and reasoning_kind text", got)
	}
	messages, _ := store.ListMessages(conv.ID)
	if len(messages) != 2 || messages[1].Content != "High at six." {
		t.Fatalf("messages = %+v", messages)
	}
}

func TestActivity_NeitherReasoningNorChatterReachesTheConversationOrTheNextTurn(t *testing.T) {
	store, conv := readyAssistant(t)
	swapAssistantRunner(t, func(apiKey, model string, tools assistantToolDeps, options assistantRunnerOptions, emit assistantEmitter) assistantRunnerFace {
		doer := queued(
			activityResponse(
				activityChunk(`{"role":"assistant",`+activityText("SECRETREASONING")[1:]),
				activityChunk(activityContent("CHATTERWORDS")),
				activityChunk(activityToolDelta),
				activityFinish("tool_calls"),
			),
			activityResponse(activityChunk(`{"role":"assistant",`+activityContent("The answer.")[1:]), activityFinish("stop")),
		)
		return &assistantRunner{doer: doer, apiKey: apiKey, model: model, tools: &fakeToolExecutor{results: map[string]string{"get_tides": `{}`}}, emit: emit}
	})
	postQuestion(t, conv.ID)
	messages, _ := store.ListMessages(conv.ID)
	if len(messages) != 2 || messages[1].Content != "The answer." {
		t.Fatalf("the stored answer must be the answer alone: %+v", messages)
	}

	var next *queuedChatDoer
	swapAssistantRunner(t, func(apiKey, model string, tools assistantToolDeps, options assistantRunnerOptions, emit assistantEmitter) assistantRunnerFace {
		next = queued(finalResponse(t, "Again.", "m", usage(1, 1, 0)))
		return &assistantRunner{doer: next, apiKey: apiKey, model: model, tools: &fakeToolExecutor{}, emit: emit}
	})
	postQuestion(t, conv.ID)
	if next == nil || len(next.rawBodies) != 1 {
		t.Fatal("the second turn did not run")
	}
	sent := string(next.rawBodies[0])
	for _, banned := range []string{"SECRETREASONING", "CHATTERWORDS", "reasoning_details"} {
		if strings.Contains(sent, banned) {
			t.Errorf("the next turn's history carries %q", banned)
		}
	}
	if !strings.Contains(sent, "The answer.") {
		t.Errorf("the answer itself should be in the history: %s", sent)
	}
}

func TestActivity_AnUnattendedReviewWithAReasoningStreamSavesNoTrace(t *testing.T) {
	store := withTestAssistantStore(t)
	doer := queued(activityResponse(
		activityChunk(`{"role":"assistant",`+activityText("thinking quietly")[1:]),
		activityChunk(activityContent("Fridge fine.")),
		activityFinish("stop"),
	))
	emit, _ := recordingEmitter()
	runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: &fakeToolExecutor{}, emit: emit, options: assistantRunnerOptions{Unattended: true}}
	reply, err := runner.run(context.Background(), "system", "", nil)
	if err != nil || reply.Content != "Fridge fine." {
		t.Fatalf("reply %q err %v", reply.Content, err)
	}
	var n int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM assistant_runs`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("an unattended review stored %d runs (err %v)", n, err)
	}
	// And its reasoning is not in its reply.
	if strings.Contains(reply.Content, "thinking quietly") {
		t.Fatal("reasoning leaked into the reply")
	}
}

// joinedDraftTrigger is the joined form the older guard tests compare against:
// the triggers in one string, as the check_trigger column and the log carry them.
func joinedDraftTrigger(d assistantToolDeps, draft string, run mateRunRecord) (trigger, instruction string) {
	triggers, instruction := d.checkDraftTriggers(draft, run)
	return strings.Join(triggers, " and "), instruction
}

// Review pass 2 fixes.

func TestActivity_AnEmptyOpeningFragmentStillSeparatesTheNextEntry(t *testing.T) {
	first := `{"reasoning_details":[{"type":"reasoning.text","text":"A","signature":"sig","index":0}]}`
	empty := `{"reasoning_details":[{"type":"reasoning.text","text":"","index":1}]}`
	doer := queued(activityResponse(
		activityChunk(`{"role":"assistant",`+first[1:]),
		activityChunk(empty),
		activityChunk(activityText("B")),
		activityChunk(activityContent("ok")),
		activityFinish("stop"),
	))
	_, err, rec := runWithRecorder(t, doer, &fakeToolExecutor{}, assistantRunnerOptions{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	rounds, _ := rec.result()
	if r := rounds[0]; r.Reasoning != "A\n\nB" {
		t.Fatalf("reasoning = %q, want the entries kept apart", r.Reasoning)
	}
	// The model's own text only: the break is not part of its size.
	if r := rounds[0]; r.ReasoningBytes != 2 {
		t.Fatalf("reasoning bytes = %d, want 2 (the inserted break is not the model's text)", r.ReasoningBytes)
	}
}

func TestActivity_ThoughtTimeIsStoredOnlyForASavedAnswer(t *testing.T) {
	store := newTestAssistantStore(t)
	conv, _ := store.CreateConversation("Passage")
	thought := int64(4200)

	noAnswer := sampleRunTrace(conv.ID, 1, nil, assistantStopError)
	noAnswer.Rounds[1].Kind, noAnswer.Rounds[1].ContentStartMs = assistantRoundAnswer, &thought
	saved, err := store.SaveRun(noAnswer)
	if err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	if got, _, _ := store.GetRun(saved.ID); saved.ThoughtMs != nil || got.ThoughtMs != nil {
		t.Errorf("a run whose answer was not saved has no thought time, got %v / %v", saved.ThoughtMs, got.ThoughtMs)
	}

	msg, _ := store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "assistant", Content: "ok"})
	withAnswer := sampleRunTrace(conv.ID, 2, &msg.ID, assistantStopAnswered)
	withAnswer.Rounds[1].Kind, withAnswer.Rounds[1].ContentStartMs = assistantRoundAnswer, &thought
	saved, err = store.SaveRun(withAnswer)
	if err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	if got, _, _ := store.GetRun(saved.ID); got.ThoughtMs == nil || *got.ThoughtMs != 4200 {
		t.Errorf("a saved answer keeps its thought time, got %v", got.ThoughtMs)
	}
}

func TestActivity_ContentStartIsMeasuredFromTheRunsOwnStart(t *testing.T) {
	doer := queued(activityResponse(activityChunk(activityContent("High at six.")), activityFinish("stop")))
	emit, _ := recordingEmitter()
	// The handler started the run two seconds before the runner did.
	rec := &assistantRunRecorder{startedAt: time.Now().Add(-2 * time.Second)}
	runner := &assistantRunner{doer: doer, apiKey: "k", model: "m", tools: &fakeToolExecutor{}, emit: emit}
	if _, err := runner.run(withAssistantRunRecorder(context.Background(), rec), "system", "", nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	rounds, _ := rec.result()
	if ms := rounds[0].ContentStartMs; ms == nil || *ms < 2000 {
		t.Fatalf("content_start_ms = %v, want at least 2000 (the gap before the runner started counts)", ms)
	}
}

func TestActivity_ACaptureWithNoLiveCallbackDoesNotPanic(t *testing.T) {
	c := newAssistantRoundCapture(nil, time.Now())
	c.hooks().Content("text")
	var tr assistantTraceRound
	c.finish(&tr)
	if tr.Chatter != "text" {
		t.Fatalf("chatter = %q", tr.Chatter)
	}
}

func TestActivity_TriggersOnADraftAcceptedAfterOneSendBackAreRecorded(t *testing.T) {
	_, _, rec, _ := thursdayRun(t, thursdayDraft, thursdayDraft)
	rounds, _ := rec.result()
	last := rounds[len(rounds)-1]
	if last.Kind != assistantRoundAnswer || len(last.CheckTriggers) != 1 || last.CheckTriggers[0] != "anchorage shelter" {
		t.Fatalf("answer round = %+v, want the triggers it was accepted with", last)
	}
	if last.CheckDraft != "" || last.Chatter != "" {
		t.Errorf("the draft is the answer, so check_draft and chatter stay empty: %q / %q", last.CheckDraft, last.Chatter)
	}
}

// pausingBody gives its first part, waits, then the rest.
type pausingBody struct {
	parts []string
	pause time.Duration
	i     int
}

func (p *pausingBody) Read(b []byte) (int, error) {
	if p.i >= len(p.parts) {
		return 0, io.EOF
	}
	if p.i == 1 {
		time.Sleep(p.pause)
	}
	n := copy(b, p.parts[p.i])
	p.i++
	return n, nil
}

func (p *pausingBody) Close() error { return nil }

func TestActivity_TriggersOnADraftAcceptedBecauseTheBudgetRanOutAreRecorded(t *testing.T) {
	shortBudget(t, 60*time.Millisecond, 5*time.Second)
	resp := &http.Response{StatusCode: 200, Header: make(http.Header), Body: &pausingBody{
		parts: []string{activityChunk(activityContent(thursdayDraft)), activityFinish("stop")},
		pause: 200 * time.Millisecond,
	}}
	emit, _ := recordingEmitter()
	rec := &assistantRunRecorder{}
	runner := &assistantRunner{doer: queued(resp), apiKey: "k", model: "m", tools: pwToolDepsWithCoastline(pwLimitsAll()), emit: emit}
	reply, err := runner.run(withAssistantRunRecorder(context.Background(), rec), "system", "", thursdayHistory())
	if err != nil || reply.Content != thursdayDraft {
		t.Fatalf("reply %q err %v", reply.Content, err)
	}
	rounds, _ := rec.result()
	if len(rounds) != 1 || rounds[0].Kind != assistantRoundAnswer || len(rounds[0].CheckTriggers) != 1 || rounds[0].CheckTriggers[0] != "anchorage shelter" {
		t.Fatalf("rounds = %+v, want one answer round carrying the triggers", rounds)
	}
	if rounds[0].CheckDraft != "" {
		t.Errorf("check_draft = %q, want empty", rounds[0].CheckDraft)
	}
}

func TestActivity_AnOldRoundWithOnlyACheckTriggerKeepsItWhenSavedAgain(t *testing.T) {
	store := newTestAssistantStore(t)
	conv, _ := store.CreateConversation("Passage")
	in := sampleRunTrace(conv.ID, 1, nil, assistantStopAnswered)
	in.Rounds[1].CheckTrigger = "relative wind"
	saved, err := store.SaveRun(in)
	if err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	got, _, _ := store.GetRun(saved.ID)
	if r := got.Rounds[1]; r.CheckTrigger != "relative wind" {
		t.Fatalf("check_trigger = %q, want it kept", r.CheckTrigger)
	}
}
