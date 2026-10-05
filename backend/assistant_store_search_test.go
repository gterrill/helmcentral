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

// VACUUM may renumber the implicit rowids of messages (its primary key is
// TEXT). The index is keyed on the message id, so a renumbering must not
// misalign it. Swapping rowids is the simulation.
func TestSearchConversations_SurvivesRowidRenumbering(t *testing.T) {
	s := newTestAssistantStore(t)
	a := seedSearchConv(t, s, "a", [2]string{"user", "windlass remote"})
	b := seedSearchConv(t, s, "b", [2]string{"user", "bilge alarm"})
	if _, err := s.db.Exec(`UPDATE messages SET rowid = 1000 - rowid`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`VACUUM`); err != nil {
		t.Fatal(err)
	}
	hits, _ := s.SearchConversations("windlass", assistantConversationSearchOptions{})
	if len(hits) != 1 || hits[0].ID != a.ID || len(hits[0].Excerpts) != 1 || !strings.Contains(hits[0].Excerpts[0].Text, "windlass") {
		t.Fatalf("hits = %+v", hits)
	}
	if err := s.DeleteConversation(a.ID); err != nil {
		t.Fatal(err)
	}
	if hits, _ := s.SearchConversations("windlass", assistantConversationSearchOptions{}); len(hits) != 0 {
		t.Fatalf("deleted conversation still found: %+v", hits)
	}
	if hits, _ := s.SearchConversations("bilge", assistantConversationSearchOptions{}); len(hits) != 1 || hits[0].ID != b.ID {
		t.Fatalf("other conversation lost: %+v", hits)
	}
}

// A database that ran the earlier shape of the index (keyed by rowid, no
// message_id column) is rebuilt on open.
func TestAssistantSearchSchema_ReplacesRowidKeyedIndex(t *testing.T) {
	s := newTestAssistantStore(t)
	c := seedSearchConv(t, s, "a", [2]string{"user", "windlass remote"})
	for _, q := range []string{
		`DROP TRIGGER messages_fts_insert`, `DROP TRIGGER messages_fts_delete`, `DROP TRIGGER messages_fts_update`,
		`DROP TABLE messages_fts`,
		`CREATE VIRTUAL TABLE messages_fts USING fts5(content, conversation_id UNINDEXED)`,
		`INSERT INTO messages_fts (rowid, content, conversation_id) SELECT rowid, content, conversation_id FROM messages`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := createAssistantSearchSchema(s.db); err != nil {
		t.Fatal(err)
	}
	hits, err := s.SearchConversations("windlass", assistantConversationSearchOptions{})
	if err != nil || len(hits) != 1 || hits[0].ID != c.ID {
		t.Fatalf("hits=%+v err=%v", hits, err)
	}
}

// A common word matches many messages in old conversations; a recent
// conversation with a single match must still be returned.
func TestSearchConversations_RecentConversationSurvivesManyOlderMatches(t *testing.T) {
	s := newTestAssistantStore(t)
	t0 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { t0 = t0.Add(time.Minute); return t0 }
	for i := 0; i < 12; i++ {
		msgs := make([][2]string, 0, 40)
		for j := 0; j < 40; j++ {
			msgs = append(msgs, [2]string{"user", "engine engine engine engine hours and engine checks"})
		}
		seedSearchConv(t, s, "old", msgs...)
	}
	recent := seedSearchConv(t, s, "recent", [2]string{"user", "one passing mention of the engine"})
	hits, err := s.SearchConversations("engine", assistantConversationSearchOptions{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 3 || hits[0].ID != recent.ID || len(hits[0].Excerpts) != 1 {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestSearchConversations_TitleMatchIsUnicodeAndPunctuationAware(t *testing.T) {
	s := newTestAssistantStore(t)
	c := seedSearchConv(t, s, "Écoutille leak", [2]string{"user", "hello"})
	other := seedSearchConv(t, s, "Racor bowl", [2]string{"user", "hello"})
	for _, q := range []string{"écoutille", "ÉCOUTILLE", `"racor",`, `racor, bowl`} {
		hits, err := s.SearchConversations(q, assistantConversationSearchOptions{})
		if err != nil || len(hits) != 1 {
			t.Fatalf("query %q: hits=%+v err=%v", q, hits, err)
		}
		want := c.ID
		if strings.Contains(strings.ToLower(q), "racor") {
			want = other.ID
		}
		if hits[0].ID != want {
			t.Fatalf("query %q: got %s want %s", q, hits[0].ID, want)
		}
	}
}

// Setup is all-or-nothing: a failure part-way leaves no half-built index
// (which would otherwise exist, be empty and never be backfilled).
func TestAssistantSearchSchema_FailureLeavesNoHalfBuiltIndex(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "x.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, q := range []string{
		// No role column: the backfill, the last step, fails.
		`CREATE TABLE messages (id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL, content TEXT NOT NULL)`,
		`INSERT INTO messages VALUES ('m1', 'c1', 'windlass')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := createAssistantSearchSchema(db); err == nil {
		t.Fatal("expected setup to fail")
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'messages_fts'`).Scan(&n)
	if n != 0 {
		t.Fatal("half-built messages_fts left behind")
	}
	for _, q := range []string{
		`DROP TABLE messages`,
		`CREATE TABLE messages (id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL, role TEXT NOT NULL, content TEXT NOT NULL)`,
		`INSERT INTO messages VALUES ('m1', 'c1', 'user', 'windlass')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := createAssistantSearchSchema(db); err != nil {
		t.Fatal(err)
	}
	db.QueryRow(`SELECT COUNT(*) FROM messages_fts WHERE messages_fts MATCH 'windlass'`).Scan(&n)
	if n != 1 {
		t.Fatalf("retry did not backfill, got %d", n)
	}
}

func TestStripMarkdownForExcerpt(t *testing.T) {
	cases := map[string]string{
		"1. **Confirm the correct circuit.** Check whether":      "Confirm the correct circuit. Check whether",
		"## Heading\n- item one\n* item `two`":                   "Heading item one item two",
		"See [the manual](/documents?document=abc) and __this__": "See the manual and this",
		"a  *b*   _c_\n\n  d":                                    "a b c d",
	}
	for in, want := range cases {
		if got := stripMarkdownForExcerpt(in); got != want {
			t.Errorf("stripMarkdownForExcerpt(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSearchConversations_ExcerptIsPlainTextWithMatchEarly(t *testing.T) {
	s := newTestAssistantStore(t)
	long := strings.Repeat("filler words go here ", 30)
	seedSearchConv(t, s, "x", [2]string{"assistant", "1. **Intro** " + long + "then the **Racor** bowl [drain](/documents?document=1) is fine. " + long})
	hits, err := s.SearchConversations("racor", assistantConversationSearchOptions{ExcerptTokens: 20})
	if err != nil || len(hits) != 1 || len(hits[0].Excerpts) != 1 {
		t.Fatalf("hits=%+v err=%v", hits, err)
	}
	text := hits[0].Excerpts[0].Text
	if strings.ContainsAny(text, "*[]()#") {
		t.Fatalf("markdown left in %q", text)
	}
	if i := strings.Index(text, "Racor"); i < 0 || i > 60 {
		t.Fatalf("match not early in %q (at %d)", text, i)
	}
}
