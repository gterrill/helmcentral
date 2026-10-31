package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/labstack/echo/v4"
)

// saveAnswerRun records the run behind answer, with its calls, as the handler
// does once the answer is stored. A run is always recorded, with or without
// calls, so an answer that used no tools is told apart from one with no record.
func saveAnswerRun(t *testing.T, store *assistantStore, answer assistantMessage, startedAt time.Time, calls ...assistantToolCall) {
	t.Helper()
	byRound := map[int][]assistantToolCall{}
	var rounds []assistantTraceRound
	for _, c := range calls {
		if _, ok := byRound[c.Round]; !ok {
			rounds = append(rounds, assistantTraceRound{Index: c.Round})
		}
		byRound[c.Round] = append(byRound[c.Round], c)
	}
	for i := range rounds {
		rounds[i].Calls = byRound[rounds[i].Index]
	}
	if _, err := store.SaveRun(assistantRunTrace{
		ConversationID: answer.ConversationID, AnswerMessageID: &answer.ID, StartedAt: startedAt,
		StopReason: assistantStopAnswered, Budget: currentAssistantTraceBudget(), Rounds: rounds,
	}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
}

// feedbackFixture appends a user question and an assistant answer and returns
// both rows.
func feedbackFixture(t *testing.T, store *assistantStore) (user, answer assistantMessage) {
	t.Helper()
	conv, err := store.CreateConversation("Fuel")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	user, err = store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "user", Content: "How much fuel?"})
	if err != nil {
		t.Fatalf("append user: %v", err)
	}
	answer, err = store.AppendMessage(assistantMessage{
		ConversationID: conv.ID, Role: "assistant", Content: "About 300 L.", Model: "m",
		PromptVersion: "abc123def456",
	})
	if err != nil {
		t.Fatalf("append assistant: %v", err)
	}
	saveAnswerRun(t, store, answer, time.Now(),
		assistantToolCall{Round: 0, Seq: 0, Name: "get_vessel_state", Args: `{"a":1}`, Result: `{"fuel":300}`, ElapsedMs: 12},
		assistantToolCall{Round: 0, Seq: 1, Name: "find_places", Args: `{}`, Result: `{"error":"boom"}`, ElapsedMs: 3, Error: "boom"})
	return user, answer
}

func TestAssistantStore_PersistsToolCallsAndPromptVersion(t *testing.T) {
	store := newTestAssistantStore(t)
	_, answer := feedbackFixture(t, store)

	msgs, err := store.ListMessages(answer.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	if got := msgs[1].PromptVersion; got != "abc123def456" {
		t.Fatalf("prompt_version = %q", got)
	}
	calls := toolCallsForTest(t, store, answer.ID)
	if len(calls) != 2 || calls[0].Name != "get_vessel_state" || calls[1].Error != "boom" || calls[1].Seq != 1 || calls[0].ElapsedMs != 12 || calls[0].Result != `{"fuel":300}` {
		t.Fatalf("unexpected tool calls: %+v", calls)
	}
}

func TestAssistantStore_ToolCallsDeleteWithConversation(t *testing.T) {
	store := newTestAssistantStore(t)
	_, answer := feedbackFixture(t, store)
	if _, err := store.SetMessageFeedback(answer.ID, "up", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteConversation(answer.ConversationID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := store.db.QueryRow(`SELECT (SELECT COUNT(*) FROM assistant_run_calls) + (SELECT COUNT(*) FROM assistant_runs) + (SELECT COUNT(*) FROM message_feedback)`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected cascade to remove %d rows", n)
	}
}

func TestAssistantStore_FeedbackSetReplaceClear(t *testing.T) {
	store := newTestAssistantStore(t)
	t0 := time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC)
	store.now = sequencedClock(t0, t0.Add(time.Minute), t0.Add(2*time.Minute))
	user, answer := feedbackFixture(t, store)

	// feedbackFixture used the real clock for appends; reset for the ratings.
	store.now = sequencedClock(t0, t0.Add(time.Minute))
	fb, err := store.SetMessageFeedback(answer.ID, "down", "wrong tank")
	if err != nil {
		t.Fatal(err)
	}
	if fb.Verdict != "down" || fb.Note != "wrong tank" || !fb.CreatedAt.Equal(t0) {
		t.Fatalf("unexpected feedback %+v", fb)
	}

	fb, err = store.SetMessageFeedback(answer.ID, "up", "")
	if err != nil {
		t.Fatal(err)
	}
	if fb.Verdict != "up" || fb.Note != "" || !fb.CreatedAt.Equal(t0) || !fb.UpdatedAt.Equal(t0.Add(time.Minute)) {
		t.Fatalf("re-rating must replace the verdict and keep created_at: %+v", fb)
	}

	list, _, err := store.ListFeedback(0, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("one row per answer, got %d", len(list))
	}
	e := list[0]
	if e.MessageID != answer.ID || e.Verdict != "up" || e.Question != user.Content || e.Answer != "About 300 L." ||
		e.PromptVersion != "abc123def456" || e.Model != "m" || len(e.ToolCalls) != 2 || e.ConversationID != answer.ConversationID {
		t.Fatalf("unexpected list entry %+v", e)
	}

	if err := store.ClearMessageFeedback(answer.ID); err != nil {
		t.Fatal(err)
	}
	if list, _, _ = store.ListFeedback(0, ""); len(list) != 0 {
		t.Fatalf("expected feedback cleared, got %d", len(list))
	}
	// Clearing again is not an error: the answer exists, it just has no rating.
	if err := store.ClearMessageFeedback(answer.ID); err != nil {
		t.Fatalf("clear of an unrated answer: %v", err)
	}
}

func TestAssistantStore_FeedbackRejections(t *testing.T) {
	store := newTestAssistantStore(t)
	user, answer := feedbackFixture(t, store)

	if _, err := store.SetMessageFeedback("nope", "up", ""); !errors.Is(err, errAssistantMessageNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
	if err := store.ClearMessageFeedback("nope"); !errors.Is(err, errAssistantMessageNotFound) {
		t.Fatalf("unknown id on clear: %v", err)
	}
	if _, err := store.SetMessageFeedback(user.ID, "up", ""); !errors.Is(err, errAssistantFeedbackNotAnswer) {
		t.Fatalf("user message: %v", err)
	}
	if _, err := store.SetMessageFeedback(answer.ID, "meh", ""); !errors.Is(err, errAssistantFeedbackInvalid) {
		t.Fatalf("bad verdict: %v", err)
	}
	if _, err := store.SetMessageFeedback(answer.ID, "up", strings.Repeat("x", assistantFeedbackNoteMax+1)); !errors.Is(err, errAssistantFeedbackInvalid) {
		t.Fatalf("long note: %v", err)
	}
}

func TestAssistantStore_PromptVersionAddedToExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE messages (id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL, seq INTEGER NOT NULL, role TEXT NOT NULL, content TEXT NOT NULL, model TEXT NOT NULL DEFAULT '', prompt_tokens INTEGER NOT NULL DEFAULT 0, completion_tokens INTEGER NOT NULL DEFAULT 0, cost_usd REAL NOT NULL DEFAULT 0, cached_tokens INTEGER NOT NULL DEFAULT 0, cache_write_tokens INTEGER NOT NULL DEFAULT 0, tool_rounds INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	store, err := newAssistantStore(path)
	if err != nil {
		t.Fatalf("open pre-existing database: %v", err)
	}
	defer store.Close()
	var has int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('messages') WHERE name = 'prompt_version'`).Scan(&has); err != nil || has != 1 {
		t.Fatalf("prompt_version column missing: has=%d err=%v", has, err)
	}
}

func TestPostAssistantMessageHandler_PersistsToolCallsAndPromptVersion(t *testing.T) {
	store, _, conv := postAssistantMessageTestSetup(t)
	swapAssistantRunner(t, func(apiKey, model string, tools assistantToolDeps, autoRouter assistantRunnerOptions, emit assistantEmitter) assistantRunnerFace {
		return &fakeAssistantRunner{emit: emit, reply: assistantReply{Content: "ok", Model: "openai/gpt-4o"},
			calls: []assistantToolCall{{Round: 0, Seq: 0, Name: "get_vessel_state", Args: "{}", Result: "{}", ElapsedMs: 5}}}
	})
	c, rec := newAssistantEchoContext(http.MethodPost, "/api/assistant/conversations/"+conv.ID+"/messages", `{"today":"2026-10-03","content":"hi"}`, conv.ID)
	if err := postAssistantMessageHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("handler: %v %d", err, rec.Code)
	}
	msgs, err := store.ListMessages(conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	row := msgs[len(msgs)-1]
	if row.Role != "assistant" || row.PromptVersion == "" {
		t.Fatalf("prompt version %q is empty", row.PromptVersion)
	}
	calls := toolCallsForTest(t, store, row.ID)
	if len(calls) != 1 || calls[0].Name != "get_vessel_state" {
		t.Fatalf("tool calls not persisted: %+v", calls)
	}
}

func feedbackRequest(t *testing.T, method, path, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	c, rec := newAssistantEchoContext(method, path, body, "")
	c.SetParamNames("id")
	c.SetParamValues(id)
	var h echo.HandlerFunc
	switch method {
	case http.MethodPut:
		h = putAssistantFeedbackHandler
	case http.MethodDelete:
		h = deleteAssistantFeedbackHandler
	default:
		h = listAssistantFeedbackHandler
	}
	if err := h(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	return rec
}

func TestAssistantFeedbackHandlers(t *testing.T) {
	store := withTestAssistantStore(t)
	user, answer := feedbackFixture(t, store)
	base := "/api/assistant/messages/" + answer.ID + "/feedback"

	rec := feedbackRequest(t, http.MethodPut, base, answer.ID, `{"verdict":"down","note":"wrong tank"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("put: %d %s", rec.Code, rec.Body.String())
	}
	var put struct {
		Feedback assistantFeedback `json:"feedback"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &put); err != nil || put.Feedback.Verdict != "down" || put.Feedback.Note != "wrong tank" || put.Feedback.MessageID != answer.ID {
		t.Fatalf("put body: %s", rec.Body.String())
	}

	rec = feedbackRequest(t, http.MethodGet, "/api/assistant/feedback", "", "")
	var list struct {
		Feedback []assistantFeedbackEntry `json:"feedback"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &list) != nil || len(list.Feedback) != 1 || list.Feedback[0].Question != "How much fuel?" || len(list.Feedback[0].ToolCalls) != 2 {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}

	for name, tc := range map[string]struct {
		id, body string
		want     int
	}{
		"unknown id":    {"nope", `{"verdict":"up"}`, http.StatusNotFound},
		"user message":  {user.ID, `{"verdict":"up"}`, http.StatusBadRequest},
		"bad verdict":   {answer.ID, `{"verdict":"meh"}`, http.StatusBadRequest},
		"malformed":     {answer.ID, `{`, http.StatusBadRequest},
		"note too long": {answer.ID, `{"verdict":"up","note":"` + strings.Repeat("x", assistantFeedbackNoteMax+1) + `"}`, http.StatusBadRequest},
	} {
		if rec := feedbackRequest(t, http.MethodPut, base, tc.id, tc.body); rec.Code != tc.want {
			t.Errorf("%s: want %d got %d: %s", name, tc.want, rec.Code, rec.Body.String())
		}
	}

	if rec := feedbackRequest(t, http.MethodDelete, base, "nope", ""); rec.Code != http.StatusNotFound {
		t.Errorf("delete unknown: %d", rec.Code)
	}
	if rec := feedbackRequest(t, http.MethodDelete, base, answer.ID, ""); rec.Code != http.StatusNoContent {
		t.Errorf("delete: %d", rec.Code)
	}
	if list, _, _ := store.ListFeedback(0, ""); len(list) != 0 {
		t.Errorf("expected cleared")
	}
}

func TestAssistantStore_ListMessagesCarriesFeedbackOnRatedAnswers(t *testing.T) {
	store := newTestAssistantStore(t)
	user, answer := feedbackFixture(t, store)
	if _, err := store.SetMessageFeedback(answer.ID, "down", "wrong tank"); err != nil {
		t.Fatal(err)
	}
	msgs, err := store.ListMessages(answer.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	if msgs[0].ID != user.ID || msgs[0].Feedback != nil {
		t.Fatalf("user row must carry no feedback: %+v", msgs[0].Feedback)
	}
	if fb := msgs[1].Feedback; fb == nil || fb.Verdict != "down" || fb.Note != "wrong tank" {
		t.Fatalf("rated answer must carry its feedback, got %+v", fb)
	}
	raw, _ := json.Marshal(msgs[1])
	if !strings.Contains(string(raw), `"feedback":{"verdict":"down","note":"wrong tank"}`) {
		t.Fatalf("unexpected JSON %s", raw)
	}
	if err := store.ClearMessageFeedback(answer.ID); err != nil {
		t.Fatal(err)
	}
	msgs, _ = store.ListMessages(answer.ConversationID)
	if msgs[1].Feedback != nil {
		t.Fatal("cleared rating must not be listed")
	}
}

// pruneFixture appends an answer with one tool call at the store's clock.
func pruneFixture(t *testing.T, store *assistantStore, at time.Time) assistantMessage {
	t.Helper()
	store.now = func() time.Time { return at }
	conv, err := store.CreateConversation("x")
	if err != nil {
		t.Fatal(err)
	}
	m, err := store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "assistant", Content: "a",
		ToolRounds: 1})
	if err != nil {
		t.Fatal(err)
	}
	saveAnswerRun(t, store, m, at, assistantToolCall{Name: "t", Args: "{}", Result: "{}"})
	return m
}

func TestAssistantStore_PruneRunsDeletesOnlyUnratedPastTheWindow(t *testing.T) {
	store := newTestAssistantStore(t)
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	old := now.Add(-assistantRunRetention - time.Hour)
	oldUnrated := pruneFixture(t, store, old)
	oldRated := pruneFixture(t, store, old)
	fresh := pruneFixture(t, store, now.Add(-time.Hour))
	if _, err := store.SetMessageFeedback(oldRated.ID, "up", ""); err != nil {
		t.Fatal(err)
	}

	store.now = func() time.Time { return now }
	n, err := store.PruneRuns()
	if err != nil || n != 1 {
		t.Fatalf("pruned %d, err %v", n, err)
	}
	count := func(id string) int { return len(toolCallsForTest(t, store, id)) }
	if count(oldUnrated.ID) != 0 || count(oldRated.ID) != 1 || count(fresh.ID) != 1 {
		t.Fatalf("unexpected survivors: %d %d %d", count(oldUnrated.ID), count(oldRated.ID), count(fresh.ID))
	}

	// Clearing the rating puts the answer back under retention.
	if err := store.ClearMessageFeedback(oldRated.ID); err != nil {
		t.Fatal(err)
	}
	if n, err = store.PruneRuns(); err != nil || n != 1 || count(oldRated.ID) != 0 {
		t.Fatalf("after clear: pruned %d err %v left %d", n, err, count(oldRated.ID))
	}
	if count(fresh.ID) != 1 {
		t.Fatal("fresh answer must keep its calls")
	}
}

func TestAssistantStore_TruncatesOversizeToolCallOnAUTF8Boundary(t *testing.T) {
	store := newTestAssistantStore(t)
	conv, _ := store.CreateConversation("x")
	// "é" is two bytes, so a cut at the cap lands mid-rune when the cap is odd
	// relative to the prefix.
	big := "x" + strings.Repeat("é", assistantToolCallMaxBytes)
	m, err := store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "assistant", Content: "a"})
	if err != nil {
		t.Fatal(err)
	}
	saveAnswerRun(t, store, m, time.Now(),
		assistantToolCall{Name: "big", Args: "{}", Result: big},
		assistantToolCall{Seq: 1, Name: "small", Args: "{}", Result: "ok"})
	calls := toolCallsForTest(t, store, m.ID)
	b, s := calls[0], calls[1]
	if !b.Truncated || b.OriginalBytes != 2+len(big) || len(b.Result) > assistantToolCallMaxBytes || !utf8.ValidString(b.Result) || len(b.Result) < assistantToolCallMaxBytes-3 {
		t.Fatalf("big call: truncated=%v original=%d len=%d valid=%v", b.Truncated, b.OriginalBytes, len(b.Result), utf8.ValidString(b.Result))
	}
	if s.Truncated || s.OriginalBytes != 0 || s.Result != "ok" {
		t.Fatalf("small call must be whole: %+v", s)
	}

	args := strings.Repeat("a", assistantToolCallMaxBytes+10)
	m2, _ := store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "assistant", Content: "a"})
	saveAnswerRun(t, store, m2, time.Now(), assistantToolCall{Name: "bigargs", Args: args, Result: "ok"})
	c2 := toolCallsForTest(t, store, m2.ID)
	if !c2[0].Truncated || len(c2[0].Args) != assistantToolCallMaxBytes || c2[0].OriginalBytes != len(args)+2 {
		t.Fatalf("args truncation: %+v", c2[0].Truncated)
	}
}

func TestAssistantStore_ListFeedbackNamesTheInputThatPromptedTheAnswer(t *testing.T) {
	store := newTestAssistantStore(t)
	conv, _ := store.CreateConversation("x")
	store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "user", Content: "watch the oil pressure"})
	store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "watch", Content: "Watch finished: oil pressure"})
	ans, _ := store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "assistant", Content: "Steady at 4 bar."})
	if _, err := store.SetMessageFeedback(ans.ID, "up", ""); err != nil {
		t.Fatal(err)
	}
	list, _, err := store.ListFeedback(0, "")
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %d", err, len(list))
	}
	if list[0].Question != "Watch finished: oil pressure" || list[0].QuestionRole != "watch" {
		t.Fatalf("got %q (%s)", list[0].Question, list[0].QuestionRole)
	}
}

func TestAssistantStore_RatingDependsOnWhetherTheLookupsAreGoneNotOnAge(t *testing.T) {
	store := newTestAssistantStore(t)
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	longAgo := now.Add(-assistantRunRetention - time.Hour)

	pruned := pruneFixture(t, store, longAgo)
	store.now = func() time.Time { return now }
	if n, err := store.PruneRuns(); err != nil || n != 1 {
		t.Fatalf("the prune takes the first answer's lookups: %d, %v", n, err)
	}

	// Made after the prune ran, so their lookups are still on record although
	// the answers are older than the retention window.
	kept := pruneFixture(t, store, longAgo)
	rated := pruneFixture(t, store, longAgo)
	conv, _ := store.CreateConversation("y")
	store.now = func() time.Time { return longAgo }
	noTools, _ := store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "assistant", Content: "no tools"})
	saveAnswerRun(t, store, noTools, longAgo)
	store.now = func() time.Time { return longAgo.Add(time.Hour) }
	if _, err := store.SetMessageFeedback(rated.ID, "up", ""); err != nil {
		t.Fatalf("rating inside the window: %v", err)
	}
	store.now = func() time.Time { return now }

	if _, err := store.SetMessageFeedback(pruned.ID, "up", ""); !errors.Is(err, errAssistantFeedbackTooOld) {
		t.Fatalf("lookups gone: %v", err)
	}
	if _, err := store.SetMessageFeedback(kept.ID, "up", ""); err != nil {
		t.Fatalf("an old answer whose lookups are still on record can be rated: %v", err)
	}
	// Only the old unrated run of the answer that used no tools goes; the rated
	// and the unrated-but-recent runs stay.
	if n, err := store.PruneRuns(); err != nil || n != 1 {
		t.Fatalf("rated answers keep their lookups: pruned %d, %v", n, err)
	}
	if _, err := store.SetMessageFeedback(noTools.ID, "up", ""); err != nil {
		t.Fatalf("an old answer that used no tools can be rated: %v", err)
	}
	if _, err := store.SetMessageFeedback(rated.ID, "down", "changed my mind"); err != nil {
		t.Fatalf("replacing an existing rating: %v", err)
	}
	if err := store.ClearMessageFeedback(rated.ID); err != nil {
		t.Fatalf("clearing: %v", err)
	}
	if n, err := store.PruneRuns(); err != nil || n != 1 {
		t.Fatalf("clearing puts it back under the retention rule: pruned %d, %v", n, err)
	}
	if _, err := store.SetMessageFeedback(rated.ID, "up", ""); !errors.Is(err, errAssistantFeedbackTooOld) {
		t.Fatalf("re-rating after the lookups went: %v", err)
	}
}

func TestAssistantStore_ToolCallsRecordedFlag(t *testing.T) {
	store := newTestAssistantStore(t)
	conv, _ := store.CreateConversation("x")
	legacy, _ := store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "assistant", Content: "from before recording"})
	noTools, _ := store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "assistant", Content: "no tools"})
	saveAnswerRun(t, store, noTools, time.Now())
	for _, m := range []assistantMessage{legacy, noTools} {
		if _, err := store.SetMessageFeedback(m.ID, "up", ""); err != nil {
			t.Fatal(err)
		}
	}
	list, _, _ := store.ListFeedback(0, "")
	got := map[string]bool{}
	for _, e := range list {
		got[e.MessageID] = e.ToolCallsRecorded
	}
	if got[legacy.ID] || !got[noTools.ID] {
		t.Fatalf("legacy must be unrecorded, a recorded answer with no tools must be recorded: %v", got)
	}
}

func TestAssistantStore_ListFeedbackPages(t *testing.T) {
	store := newTestAssistantStore(t)
	t0 := time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC)
	var ids []string
	for i := 0; i < 3; i++ {
		m := pruneFixture(t, store, t0.Add(time.Duration(i)*time.Minute))
		ids = append(ids, m.ID)
		if _, err := store.SetMessageFeedback(m.ID, "up", ""); err != nil {
			t.Fatal(err)
		}
	}
	page1, next, err := store.ListFeedback(2, "")
	if err != nil || len(page1) != 2 || next == "" {
		t.Fatalf("page 1: %d next=%q err=%v", len(page1), next, err)
	}
	if page1[0].MessageID != ids[2] || page1[1].MessageID != ids[1] {
		t.Fatalf("newest first expected")
	}
	if len(page1[0].ToolCalls) != 1 {
		t.Fatalf("tool calls must ride with each entry")
	}
	page2, next2, err := store.ListFeedback(2, next)
	if err != nil || len(page2) != 1 || page2[0].MessageID != ids[0] || next2 != "" {
		t.Fatalf("page 2: %+v next=%q err=%v", page2, next2, err)
	}
	if _, _, err := store.ListFeedback(0, "garbage"); !errors.Is(err, errAssistantFeedbackInvalid) {
		t.Fatalf("bad cursor: %v", err)
	}
}

func TestAssistantFeedbackHandlers_ListLimitAndConflict(t *testing.T) {
	store := withTestAssistantStore(t)
	_, answer := feedbackFixture(t, store)
	for _, q := range []string{"?limit=0", "?limit=201", "?limit=abc", "?before=garbage"} {
		c, rec := newAssistantEchoContext(http.MethodGet, "/api/assistant/feedback"+q, "", "")
		if err := listAssistantFeedbackHandler(c); err != nil || rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %v %d", q, err, rec.Code)
		}
	}
	c, rec := newAssistantEchoContext(http.MethodGet, "/api/assistant/feedback?limit=200", "", "")
	if err := listAssistantFeedbackHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Errorf("limit=200: %v %d", err, rec.Code)
	}

	now := time.Now().UTC()
	old := pruneFixture(t, store, now.Add(-assistantRunRetention-time.Hour))
	store.now = func() time.Time { return now }
	if _, err := store.PruneRuns(); err != nil {
		t.Fatal(err)
	}
	if rec := feedbackRequest(t, http.MethodPut, "/x", old.ID, `{"verdict":"up"}`); rec.Code != http.StatusConflict {
		t.Errorf("old answer: %d %s", rec.Code, rec.Body.String())
	}
	_ = answer
}

func TestAssistantPromptVersion_IsTheRulesAndToolsNotTheBoat(t *testing.T) {
	tools := assistantToolDefinitions(assistantToolDeps{})
	base := assistantPromptVersion(false, tools)
	if len(base) != 12 || base != assistantPromptVersion(false, tools) {
		t.Fatalf("unstable or wrong length %q", base)
	}
	if assistantPromptVersion(true, tools) == base {
		t.Error("a rule that is switched on must change the version")
	}
	if assistantPromptVersion(false, tools[:len(tools)-1]) == base {
		t.Error("the tool list must change the version")
	}
}

// The version stamped on an answer is computed from the tools the runner was
// handed, so it changes with the offered tool set.
func TestPostAssistantMessageHandler_PromptVersionFollowsTheToolsTheRunnerGets(t *testing.T) {
	store, _, conv := postAssistantMessageTestSetup(t)
	var offered assistantToolDeps
	swapAssistantRunner(t, func(apiKey, model string, tools assistantToolDeps, autoRouter assistantRunnerOptions, emit assistantEmitter) assistantRunnerFace {
		offered = tools
		return &fakeAssistantRunner{emit: emit, reply: assistantReply{Content: "ok", Model: "m"}}
	})
	ask := func(webSearch bool) string {
		body := fmt.Sprintf("assistant:\n    enabled: true\n    model: \"openai/gpt-4o\"\n    web_search: %t\n    web_search_model: \"openai/gpt-4o:online\"\n", webSearch)
		path := filepath.Join(t.TempDir(), "settings.yaml")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("SETTINGS_FILE", path)
		c, rec := newAssistantEchoContext(http.MethodPost, "/api/assistant/conversations/"+conv.ID+"/messages", `{"today":"2026-10-03","content":"hi"}`, conv.ID)
		if err := postAssistantMessageHandler(c); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("handler: %v %d", err, rec.Code)
		}
		msgs, _ := store.ListMessages(conv.ID)
		version := msgs[len(msgs)-1].PromptVersion
		if want := assistantPromptVersion(mateToolAvailable(offered, "search_web"), offered.definitions()); version != want {
			t.Fatalf("web_search=%t: stored version %q is not the hash of the tools the runner got (%q)", webSearch, version, want)
		}
		return version
	}
	on, off := ask(true), ask(false)
	if on == off {
		t.Fatalf("offering search_web must change the version: %q", on)
	}
}

func TestPostAssistantMessageHandler_PromptVersionIgnoresVesselAndOperatorData(t *testing.T) {
	store, _, conv := postAssistantMessageTestSetup(t)
	swapAssistantRunner(t, func(apiKey, model string, tools assistantToolDeps, autoRouter assistantRunnerOptions, emit assistantEmitter) assistantRunnerFace {
		return &fakeAssistantRunner{emit: emit, reply: assistantReply{Content: "ok", Model: "m"}}
	})
	ask := func(notes string) string {
		body := fmt.Sprintf("assistant:\n    enabled: true\n    model: \"openai/gpt-4o\"\n    notes: %q\n", notes)
		path := filepath.Join(t.TempDir(), "settings.yaml")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("SETTINGS_FILE", path)
		c, rec := newAssistantEchoContext(http.MethodPost, "/api/assistant/conversations/"+conv.ID+"/messages", `{"today":"2026-10-03","content":"hi"}`, conv.ID)
		if err := postAssistantMessageHandler(c); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("handler: %v %d", err, rec.Code)
		}
		msgs, _ := store.ListMessages(conv.ID)
		return msgs[len(msgs)-1].PromptVersion
	}
	a, b := ask("Anchorage A is exposed to the south."), ask("Totally different standing notes.")
	if a == "" || a != b {
		t.Fatalf("standing notes are operator data, not a rule: %q vs %q", a, b)
	}
}

func TestAssistantStore_ListFeedbackWithNoPrecedingQuestionIsEmptyNotMissing(t *testing.T) {
	store := newTestAssistantStore(t)
	conv, _ := store.CreateConversation("x")
	ans, _ := store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "assistant", Content: "Unprompted."})
	if _, err := store.SetMessageFeedback(ans.ID, "down", ""); err != nil {
		t.Fatal(err)
	}
	list, _, err := store.ListFeedback(0, "")
	if err != nil || len(list) != 1 || list[0].Question != "" || list[0].QuestionRole != "" || list[0].Answer != "Unprompted." {
		t.Fatalf("got %+v %v", list, err)
	}
}
