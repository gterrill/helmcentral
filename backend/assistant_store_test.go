package main

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func newTestAssistantStore(t *testing.T) *assistantStore {
	t.Helper()
	store, err := newAssistantStore(filepath.Join(t.TempDir(), "assistant.sqlite"))
	if err != nil {
		t.Fatalf("newAssistantStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// sequencedClock returns each of times in order across successive calls,
// repeating the last one once exhausted, so a test can control exactly how
// "now" advances between store operations without a real sleep.
func sequencedClock(times ...time.Time) func() time.Time {
	i := 0
	return func() time.Time {
		got := times[i]
		if i < len(times)-1 {
			i++
		}
		return got
	}
}

func TestAssistantStore_CreateAndListConversationsOrderedByUpdatedAt(t *testing.T) {
	store := newTestAssistantStore(t)
	t0 := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
	store.now = sequencedClock(t0, t0.Add(1*time.Second))

	first, err := store.CreateConversation("First")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	second, err := store.CreateConversation("Second")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	if first.ID == "" || second.ID == "" || first.ID == second.ID {
		t.Fatalf("expected distinct assigned IDs, got %q and %q", first.ID, second.ID)
	}

	list, err := store.ListConversations()
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 conversations, got %d", len(list))
	}
	// Second was created a second later, so it must lead the list
	// (ListConversations orders updated_at DESC).
	if list[0].ID != second.ID || list[1].ID != first.ID {
		t.Fatalf("expected most-recently-updated conversation first, got %+v", list)
	}
	if list[0].Title != "Second" || list[1].Title != "First" {
		t.Fatalf("unexpected titles: %+v", list)
	}
}

func TestAssistantStore_AppendMessageAssignsIncreasingSeqAndBumpsUpdatedAt(t *testing.T) {
	store := newTestAssistantStore(t)
	t0 := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
	store.now = sequencedClock(t0)

	conv, err := store.CreateConversation("Whitsundays")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	t1 := t0.Add(5 * time.Second)
	store.now = sequencedClock(t1)
	first, err := store.AppendMessage(assistantMessage{
		ConversationID: conv.ID,
		Role:           "user",
		Content:        "Tongue Bay or Blue Pearl Bay first?",
	})
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if first.ID == "" {
		t.Fatalf("expected AppendMessage to assign an ID")
	}
	if first.Seq != 1 {
		t.Fatalf("expected the first message to get seq 1, got %d", first.Seq)
	}
	if !first.CreatedAt.Equal(t1) {
		t.Fatalf("expected CreatedAt %v, got %v", t1, first.CreatedAt)
	}

	t2 := t1.Add(5 * time.Second)
	store.now = sequencedClock(t2)
	second, err := store.AppendMessage(assistantMessage{
		ConversationID:   conv.ID,
		Role:             "assistant",
		Content:          "Check the forecast for both before deciding.",
		Model:            "anthropic/claude-sonnet-4.5",
		PromptTokens:     120,
		CompletionTokens: 40,
		CostUSD:          0.0184,
		ToolRounds:       2,
	})
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if second.Seq != 2 {
		t.Fatalf("expected the second message to get seq 2, got %d", second.Seq)
	}
	if second.ID == first.ID {
		t.Fatalf("expected distinct message IDs")
	}

	updated, ok, err := store.GetConversation(conv.ID)
	if err != nil || !ok {
		t.Fatalf("GetConversation: ok=%v err=%v", ok, err)
	}
	if !updated.UpdatedAt.Equal(t2) {
		t.Fatalf("expected conversations.updated_at bumped to %v, got %v", t2, updated.UpdatedAt)
	}
}

func TestAssistantStore_ListMessagesOrderedBySeq(t *testing.T) {
	store := newTestAssistantStore(t)
	store.now = sequencedClock(time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC))

	conv, err := store.CreateConversation("Whitsundays")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	contents := []string{"first", "second", "third"}
	for _, c := range contents {
		if _, err := store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "user", Content: c}); err != nil {
			t.Fatalf("AppendMessage(%q): %v", c, err)
		}
	}

	messages, err := store.ListMessages(conv.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != len(contents) {
		t.Fatalf("expected %d messages, got %d", len(contents), len(messages))
	}
	for i, want := range contents {
		if messages[i].Content != want || messages[i].Seq != i+1 {
			t.Fatalf("message %d: expected content %q seq %d, got %+v", i, want, i+1, messages[i])
		}
	}
}

func TestAssistantStore_DeleteConversationRemovesItsMessages(t *testing.T) {
	store := newTestAssistantStore(t)
	store.now = sequencedClock(time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC))

	conv, err := store.CreateConversation("Whitsundays")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	if _, err := store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "user", Content: "hi"}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	if err := store.DeleteConversation(conv.ID); err != nil {
		t.Fatalf("DeleteConversation: %v", err)
	}

	if _, ok, err := store.GetConversation(conv.ID); err != nil || ok {
		t.Fatalf("expected the conversation to be gone, ok=%v err=%v", ok, err)
	}
	messages, err := store.ListMessages(conv.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("expected the conversation's messages to be deleted too, got %d", len(messages))
	}
}

func TestAssistantStore_DeleteConversationMissingIDReturnsSentinelError(t *testing.T) {
	store := newTestAssistantStore(t)
	err := store.DeleteConversation("does-not-exist")
	if !errors.Is(err, errAssistantConversationNotFound) {
		t.Fatalf("expected errAssistantConversationNotFound, got %v", err)
	}
}

func TestAssistantStore_GetConversationMissReturnsNotOk(t *testing.T) {
	store := newTestAssistantStore(t)
	_, ok, err := store.GetConversation("does-not-exist")
	if err != nil {
		t.Fatalf("expected no error for a missing id, got %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false for a missing id")
	}
}

func TestAssistantStore_AppendMessageToMissingConversationErrors(t *testing.T) {
	store := newTestAssistantStore(t)
	_, err := store.AppendMessage(assistantMessage{ConversationID: "does-not-exist", Role: "user", Content: "hi"})
	if !errors.Is(err, errAssistantConversationNotFound) {
		t.Fatalf("expected errAssistantConversationNotFound, got %v", err)
	}
}

func TestAssistantStore_SetTitleRenamesWithoutBumpingUpdatedAt(t *testing.T) {
	store := newTestAssistantStore(t)
	t0 := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
	store.now = sequencedClock(t0)

	conv, err := store.CreateConversation("New conversation")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	if err := store.SetTitle(conv.ID, "Tongue Bay vs Blue Pearl Bay"); err != nil {
		t.Fatalf("SetTitle: %v", err)
	}

	renamed, ok, err := store.GetConversation(conv.ID)
	if err != nil || !ok {
		t.Fatalf("GetConversation: ok=%v err=%v", ok, err)
	}
	if renamed.Title != "Tongue Bay vs Blue Pearl Bay" {
		t.Fatalf("expected the title to be renamed, got %q", renamed.Title)
	}
	if !renamed.UpdatedAt.Equal(t0) {
		t.Fatalf("expected SetTitle to leave updated_at untouched, got %v", renamed.UpdatedAt)
	}
}

// ── attachments (ADR 0106) ───────────────────────────────────────────────

func TestAssistantStore_AttachmentsRoundTripThroughAppendAndListInOrder(t *testing.T) {
	store := newTestAssistantStore(t)
	store.now = sequencedClock(time.Date(2026, 9, 18, 8, 0, 0, 0, time.UTC))

	conv, err := store.CreateConversation("Engine questions")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	attachments := []assistantAttachment{
		{DocumentID: "doc-2", Filename: "b-receipt.pdf"},
		{DocumentID: "doc-1", Filename: "a-manual.pdf"},
	}
	appended, err := store.AppendMessage(assistantMessage{
		ConversationID: conv.ID,
		Role:           "user",
		Content:        "What's the service interval?",
		Attachments:    attachments,
	})
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if len(appended.Attachments) != 2 {
		t.Fatalf("expected AppendMessage to echo back 2 attachments, got %+v", appended.Attachments)
	}

	messages, err := store.ListMessages(conv.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	got := messages[0].Attachments
	if len(got) != 2 {
		t.Fatalf("expected 2 attachments round-tripped, got %+v", got)
	}
	// Position order, not insertion/document-id order: doc-2 was attached
	// first (position 0), so it must come back first.
	if got[0] != attachments[0] || got[1] != attachments[1] {
		t.Fatalf("expected attachments in position order %+v, got %+v", attachments, got)
	}
}

func TestAssistantStore_MessageWithNoAttachmentsRoundTripsEmpty(t *testing.T) {
	store := newTestAssistantStore(t)
	store.now = sequencedClock(time.Date(2026, 9, 18, 8, 0, 0, 0, time.UTC))

	conv, err := store.CreateConversation("Plain question")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	if _, err := store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "user", Content: "hi"}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	messages, err := store.ListMessages(conv.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if len(messages[0].Attachments) != 0 {
		t.Fatalf("expected no attachments, got %+v", messages[0].Attachments)
	}
}

func TestAssistantStore_DeleteConversationRemovesAttachmentRows(t *testing.T) {
	store := newTestAssistantStore(t)
	store.now = sequencedClock(time.Date(2026, 9, 18, 8, 0, 0, 0, time.UTC))

	conv, err := store.CreateConversation("Engine questions")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	msg, err := store.AppendMessage(assistantMessage{
		ConversationID: conv.ID,
		Role:           "user",
		Content:        "See attached",
		Attachments:    []assistantAttachment{{DocumentID: "doc-1", Filename: "a.pdf"}},
	})
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	if err := store.DeleteConversation(conv.ID); err != nil {
		t.Fatalf("DeleteConversation: %v", err)
	}

	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM message_attachments WHERE message_id = ?`, msg.ID).Scan(&count); err != nil {
		t.Fatalf("count message_attachments: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected deleting the conversation to remove its attachment rows, found %d", count)
	}
}

func TestAssistantStore_ReopeningSamePathIsIdempotentAndPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "assistant.sqlite")

	store1, err := newAssistantStore(path)
	if err != nil {
		t.Fatalf("newAssistantStore (1st open): %v", err)
	}
	if _, err := store1.CreateConversation("Persisted"); err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	if err := store1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	store2, err := newAssistantStore(path)
	if err != nil {
		t.Fatalf("newAssistantStore (2nd open, same path): %v", err)
	}
	defer store2.Close()

	list, err := store2.ListConversations()
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(list) != 1 || list[0].Title != "Persisted" {
		t.Fatalf("expected the conversation created before reopening to persist, got %+v", list)
	}
}
