package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// ── ftsMatchQuery ────────────────────────────────────────────────────────

func TestFTSMatchQuery(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   string
		wantOK bool
	}{
		{"empty string", "", "", false},
		{"whitespace only", "   \t\n  ", "", false},
		{"single word", "impeller", `"impeller"*`, true},
		{"two words", "engine impeller", `"engine" "impeller"*`, true},
		{"hyphenated part number", "PN-1234-A", `"PN-1234-A"*`, true},
		{"two hyphenated part numbers", "PN-1234-A PN-5678-B", `"PN-1234-A" "PN-5678-B"*`, true},
		{"embedded double quote is doubled", `say "hi"`, `"say" """hi"""*`, true},
		{"lone double quote", `"`, `""""*`, true},
		{"extra whitespace between words is collapsed by Fields", "  engine   impeller  ", `"engine" "impeller"*`, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ftsMatchQuery(tc.input)
			if ok != tc.wantOK {
				t.Fatalf("ftsMatchQuery(%q) ok = %v, want %v", tc.input, ok, tc.wantOK)
			}
			if got != tc.want {
				t.Fatalf("ftsMatchQuery(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestFTSMatchQuery_CapsAtSixteenTokens(t *testing.T) {
	words := make([]string, 20)
	for i := range words {
		words[i] = "w" + strconv.Itoa(i)
	}
	input := strings.Join(words, " ")

	got, ok := ftsMatchQuery(input)
	if !ok {
		t.Fatalf("expected ok=true for 20 words")
	}

	// Exactly 16 quoted tokens, in order, only the last one starred.
	want := make([]string, 16)
	for i := 0; i < 16; i++ {
		want[i] = `"w` + strconv.Itoa(i) + `"`
	}
	want[15] += "*"
	wantStr := strings.Join(want, " ")

	if got != wantStr {
		t.Fatalf("ftsMatchQuery(20 words) = %q, want %q", got, wantStr)
	}
	if strings.Contains(got, "w16") || strings.Contains(got, "w19") {
		t.Fatalf("expected tokens beyond the 16th to be dropped, got %q", got)
	}
}

// FuzzFTSMatchQuery feeds arbitrary strings through ftsMatchQuery and, for
// every non-empty result, runs the produced string as a MATCH query against
// a real FTS5 table. Because ftsMatchQuery always wraps every token in a
// (correctly escaped) quoted phrase, the result must always be syntactically
// valid FTS5, regardless of what punctuation or FTS5 keywords the raw input
// contained - a syntax error here would mean the escaping is broken.
func FuzzFTSMatchQuery(f *testing.F) {
	seeds := []string{
		"impeller",
		"PN-1234-A",
		`he said "hello"`,
		"",
		"   ",
		strings.Repeat("word ", 20),
		"a\tb\nc",
		"über café",
		"*",
		`"`,
		`""`,
		"AND OR NOT NEAR",
		"(engine OR impeller)",
		"col:value",
		"NEAR(engine impeller, 5)",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	dbPath := filepath.Join(f.TempDir(), "fuzz_fts.sqlite")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		f.Fatalf("open fuzz fts db: %v", err)
	}
	f.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE VIRTUAL TABLE t USING fts5(text, heading, tokenize='porter unicode61 remove_diacritics 2')`); err != nil {
		f.Fatalf("create fts5 table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO t(text, heading) VALUES ('impeller torque spec PN-1234-A', 'Impeller Guide'), ('the quick brown fox', '')`); err != nil {
		f.Fatalf("seed fts5 table: %v", err)
	}

	f.Fuzz(func(t *testing.T, input string) {
		query, ok := ftsMatchQuery(input)
		if !ok {
			return
		}
		rows, err := db.Query(`SELECT rowid FROM t WHERE t MATCH ?`, query)
		if err != nil {
			t.Fatalf("MATCH %q (from input %q) failed: %v", query, input, err)
		}
		for rows.Next() {
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("MATCH %q (from input %q) failed during iteration: %v", query, input, err)
		}
		rows.Close()
	})
}

// ── end-to-end: extract, chunk, search ──────────────────────────────────

func TestDocumentsEndToEnd_ExtractChunkAndSearchFindsPageTwo(t *testing.T) {
	store := newTestDocumentStore(t)

	doc, err := store.Insert(document{SHA256: "sha-e2e", Filename: "manual.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	ex, err := extractDocumentText(context.Background(), testdataPath("two_page.pdf"), "application/pdf")
	if err != nil {
		t.Fatalf("extractDocumentText: %v", err)
	}

	chunks := chunkDocument(ex, "application/pdf")
	if len(chunks) == 0 {
		t.Fatalf("expected chunkDocument to produce chunks")
	}
	if err := store.ReplaceChunks(doc.ID, []string{"local"}, chunks); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}

	query, ok := ftsMatchQuery("THERMOSTATTOKEN")
	if !ok {
		t.Fatalf("expected ftsMatchQuery to accept a single word")
	}

	results, err := store.Search(query, nil, false, "", 10, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected exactly 1 result, got %d: %+v", len(results), results)
	}
	if results[0].DocumentID != doc.ID {
		t.Fatalf("expected the hit to be the inserted document, got %q", results[0].DocumentID)
	}
	if results[0].PageStart != 2 {
		t.Fatalf("expected PageStart=2 (THERMOSTATTOKEN is on page 2), got %d", results[0].PageStart)
	}
	if !strings.Contains(results[0].Snippet, "\x02") {
		t.Fatalf("expected the snippet to contain the \\x02 highlight marker, got %q", results[0].Snippet)
	}
}

// ── ranking: a title hit outranks a body-only hit ────────────────────────

func TestDocumentsSearch_TitleHitRanksAboveBodyOnlyHit(t *testing.T) {
	store := newTestDocumentStore(t)

	titleDoc, err := store.Insert(document{SHA256: "sha-title-hit", Filename: "a.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert (title doc): %v", err)
	}
	title := "Impeller Replacement Guide"
	if err := store.UpdateMeta(titleDoc.ID, &title, nil, nil); err != nil {
		t.Fatalf("UpdateMeta: %v", err)
	}

	bodyDoc, err := store.Insert(document{SHA256: "sha-body-hit", Filename: "b.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert (body doc): %v", err)
	}
	if err := store.ReplaceChunks(bodyDoc.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "Some unrelated engine notes that happen to mention the impeller only once, in passing, near the end."},
	}); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}

	query, ok := ftsMatchQuery("impeller")
	if !ok {
		t.Fatalf("expected ftsMatchQuery to accept 'impeller'")
	}

	results, err := store.Search(query, nil, false, "", 10, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected both documents to match 'impeller', got %d: %+v", len(results), results)
	}
	if results[0].DocumentID != titleDoc.ID {
		t.Fatalf("expected the title hit (%s) to rank above the body-only hit (%s), got order %+v",
			titleDoc.ID, bodyDoc.ID, results)
	}
}
