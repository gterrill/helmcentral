package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

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
		ToolCalls: []assistantToolCall{
			{Round: 0, Seq: 0, Name: "get_vessel_state", Args: `{"a":1}`, Result: `{"fuel":300}`, ElapsedMs: 12},
			{Round: 0, Seq: 1, Name: "find_places", Args: `{}`, Result: `{"error":"boom"}`, ElapsedMs: 3, Error: "boom"},
		},
	})
	if err != nil {
		t.Fatalf("append assistant: %v", err)
	}
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
	calls, err := store.ListToolCalls(answer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0].Name != "get_vessel_state" || calls[1].Error != "boom" || calls[1].Seq != 1 || calls[0].ElapsedMs != 12 || calls[0].Result != `{"fuel":300}` {
		t.Fatalf("unexpected tool calls: %+v", calls)
	}
}

func TestAssistantStore_ToolCallsOnlyOnAssistantMessages(t *testing.T) {
	store := newTestAssistantStore(t)
	conv, _ := store.CreateConversation("x")
	_, err := store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "user", Content: "q", ToolCalls: []assistantToolCall{{Name: "x"}}})
	if err == nil {
		t.Fatal("expected an error for tool calls on a user message")
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
	if err := store.db.QueryRow(`SELECT (SELECT COUNT(*) FROM message_tool_calls) + (SELECT COUNT(*) FROM message_feedback)`).Scan(&n); err != nil {
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

	list, err := store.ListFeedback()
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
	if list, _ = store.ListFeedback(); len(list) != 0 {
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

func TestAssistantPromptVersion(t *testing.T) {
	a := assistantPromptVersion("stable prompt")
	if len(a) != 12 || a != assistantPromptVersion("stable prompt") || a == assistantPromptVersion("stable prompt!") {
		t.Fatalf("unexpected version %q", a)
	}
}

func TestPostAssistantMessageHandler_PersistsToolCallsAndPromptVersion(t *testing.T) {
	store, _, conv := postAssistantMessageTestSetup(t)
	var gotStable string
	swapAssistantRunner(t, func(apiKey, model, settingsPath string, autoRouter assistantAutoRouterOptions, webSearch bool, today time.Time, emit assistantEmitter) assistantRunnerFace {
		return &fakeAssistantRunner{emit: emit, gotStable: &gotStable, reply: assistantReply{
			Content: "ok", Model: "openai/gpt-4o",
			ToolCalls: []assistantToolCall{{Round: 0, Seq: 0, Name: "get_vessel_state", Args: "{}", Result: "{}", ElapsedMs: 5}},
		}}
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
	if row.Role != "assistant" || row.PromptVersion == "" || row.PromptVersion != assistantPromptVersion(gotStable) {
		t.Fatalf("prompt version %q does not match the stable part the run was given", row.PromptVersion)
	}
	calls, _ := store.ListToolCalls(row.ID)
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
	if list, _ := store.ListFeedback(); len(list) != 0 {
		t.Errorf("expected cleared")
	}
}
