package main

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func seedSearchConv(t *testing.T, s *assistantStore, title string, msgs ...[2]string) assistantConversation {
	t.Helper()
	c, err := s.CreateConversation(title)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		if _, err := s.AppendMessage(assistantMessage{ConversationID: c.ID, Role: m[0], Content: m[1]}); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func TestSearchConversations_MatchesLaterMessageCaseInsensitive(t *testing.T) {
	s := newTestAssistantStore(t)
	c := seedSearchConv(t, s, "Fuel polisher question",
		[2]string{"user", "What does the Reverso fuel polisher do?"},
		[2]string{"assistant", "It cleans fuel. The discharge manifold on the STARBOARD side has no sampling valve."})
	seedSearchConv(t, s, "Unrelated", [2]string{"user", "anchor chain length"})

	hits, err := s.SearchConversations("starboard", assistantConversationSearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != c.ID {
		t.Fatalf("hits = %+v", hits)
	}
	if len(hits[0].Excerpts) != 1 || !strings.Contains(strings.ToLower(hits[0].Excerpts[0].Text), "starboard") || hits[0].Excerpts[0].Role != "assistant" {
		t.Fatalf("excerpts = %+v", hits[0].Excerpts)
	}
}

func TestSearchConversations_PrefixAndAllWords(t *testing.T) {
	s := newTestAssistantStore(t)
	c := seedSearchConv(t, s, "x", [2]string{"user", "racor bowl drain advice"})
	seedSearchConv(t, s, "y", [2]string{"user", "racor filter only"})
	hits, _ := s.SearchConversations("racor dra", assistantConversationSearchOptions{})
	if len(hits) != 1 || hits[0].ID != c.ID {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestSearchConversations_TitleOnlyMatchHasNoExcerpt(t *testing.T) {
	s := newTestAssistantStore(t)
	c := seedSearchConv(t, s, "Watermaker service", [2]string{"user", "hello there"})
	hits, err := s.SearchConversations("WATERMAKER", assistantConversationSearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != c.ID || len(hits[0].Excerpts) != 0 {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestSearchConversations_ExcludesConversationAndWatchRows(t *testing.T) {
	s := newTestAssistantStore(t)
	a := seedSearchConv(t, s, "a", [2]string{"user", "bilge pump"})
	b := seedSearchConv(t, s, "b", [2]string{"watch", "bilge report"})
	hits, _ := s.SearchConversations("bilge", assistantConversationSearchOptions{ExcludeConvID: a.ID})
	if len(hits) != 0 {
		t.Fatalf("expected none (excluded + watch role), got %+v", hits)
	}
	hits, _ = s.SearchConversations("bilge", assistantConversationSearchOptions{})
	if len(hits) != 1 || hits[0].ID != a.ID || hits[0].ID == b.ID {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestSearchConversations_DeletedConversationLeavesIndex(t *testing.T) {
	s := newTestAssistantStore(t)
	c := seedSearchConv(t, s, "a", [2]string{"user", "windlass"})
	if err := s.DeleteConversation(c.ID); err != nil {
		t.Fatal(err)
	}
	hits, _ := s.SearchConversations("windlass", assistantConversationSearchOptions{})
	if len(hits) != 0 {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestSearchConversations_EmptyQueryErrors(t *testing.T) {
	s := newTestAssistantStore(t)
	if _, err := s.SearchConversations("   ", assistantConversationSearchOptions{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestSearchConversations_SpecialCharactersDoNotBreakMatch(t *testing.T) {
	s := newTestAssistantStore(t)
	seedSearchConv(t, s, "a", [2]string{"user", "hello"})
	if _, err := s.SearchConversations(`"foo" AND (bar`, assistantConversationSearchOptions{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSearchConversations_OrdersMostRecentFirstAndLimits(t *testing.T) {
	s := newTestAssistantStore(t)
	t0 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { t0 = t0.Add(time.Minute); return t0 }
	old := seedSearchConv(t, s, "old", [2]string{"user", "generator"})
	newer := seedSearchConv(t, s, "newer", [2]string{"user", "generator"})
	hits, _ := s.SearchConversations("generator", assistantConversationSearchOptions{})
	if len(hits) != 2 || hits[0].ID != newer.ID || hits[1].ID != old.ID {
		t.Fatalf("hits = %+v", hits)
	}
	hits, _ = s.SearchConversations("generator", assistantConversationSearchOptions{Limit: 1})
	if len(hits) != 1 {
		t.Fatalf("limit not applied: %+v", hits)
	}
}

func TestAssistantSearchSchema_BackfillsExistingMessages(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "old.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, q := range []string{
		`CREATE TABLE conversations (id TEXT PRIMARY KEY, title TEXT NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL)`,
		`CREATE TABLE messages (id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL, seq INTEGER NOT NULL, role TEXT NOT NULL, content TEXT NOT NULL,
			model TEXT NOT NULL DEFAULT '', prompt_tokens INTEGER NOT NULL DEFAULT 0, completion_tokens INTEGER NOT NULL DEFAULT 0,
			cost_usd REAL NOT NULL DEFAULT 0, tool_rounds INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL)`,
		`INSERT INTO conversations VALUES ('c1', 'Old chat', 1, 1)`,
		`INSERT INTO messages (id, conversation_id, seq, role, content, created_at) VALUES ('m1', 'c1', 1, 'user', 'the reverso polisher', 1)`,
		`INSERT INTO messages (id, conversation_id, seq, role, content, created_at) VALUES ('m2', 'c1', 2, 'watch', 'reverso watch', 1)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := createAssistantSearchSchema(db); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages_fts WHERE messages_fts MATCH 'reverso'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("backfilled matches = %d, want 1 (watch row excluded)", n)
	}
	// Running again must not duplicate.
	if err := createAssistantSearchSchema(db); err != nil {
		t.Fatal(err)
	}
	db.QueryRow(`SELECT COUNT(*) FROM messages_fts`).Scan(&n)
	if n != 1 {
		t.Fatalf("rows after second run = %d", n)
	}
}
