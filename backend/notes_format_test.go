package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func testNoteMeta() noteFileMeta {
	return noteFileMeta{
		ID:      "0f3b1c2e-1234-4abc-8def-1234567890ab",
		Title:   "Genset start-up",
		Type:    "procedure",
		Tags:    []string{"genset", "engine-room"},
		Created: time.Date(2026, 9, 20, 4, 11, 7, 0, time.UTC),
	}
}

func TestRenderNoteFile_IsByteStable(t *testing.T) {
	meta := testNoteMeta()
	body := "1. Warm up for five minutes\n2. Bring on load gradually"

	first := renderNoteFile(meta, body)
	for i := 0; i < 5; i++ {
		again := renderNoteFile(meta, body)
		if string(again) != string(first) {
			t.Fatalf("renderNoteFile is not byte-stable: run %d differs\nfirst: %q\nagain: %q", i, first, again)
		}
	}

	want := "---\n" +
		"id: 0f3b1c2e-1234-4abc-8def-1234567890ab\n" +
		"title: Genset start-up\n" +
		"type: procedure\n" +
		"tags: [engine-room, genset]\n" +
		"created: \"2026-09-20T04:11:07Z\"\n" +
		"---\n" +
		"1. Warm up for five minutes\n2. Bring on load gradually\n"
	if string(first) != want {
		t.Fatalf("renderNoteFile output mismatch:\ngot:  %q\nwant: %q", first, want)
	}
}

func TestRenderNoteFile_TagOrderIsNormalised(t *testing.T) {
	meta := testNoteMeta()
	body := "hello"

	meta.Tags = []string{"zebra", "alpha", "mike"}
	sortedFirst := renderNoteFile(meta, body)

	meta.Tags = []string{"mike", "zebra", "alpha"}
	sortedSecond := renderNoteFile(meta, body)

	if string(sortedFirst) != string(sortedSecond) {
		t.Fatalf("expected tag order in the input to not affect rendered bytes:\na: %q\nb: %q", sortedFirst, sortedSecond)
	}
	if !strings.Contains(string(sortedFirst), "tags: [alpha, mike, zebra]") {
		t.Fatalf("expected tags to render alphabetically sorted, got %q", sortedFirst)
	}
}

func TestRenderNoteFile_LFOnlyAndSingleTrailingNewline(t *testing.T) {
	meta := testNoteMeta()
	rendered := renderNoteFile(meta, "line one\nline two   \n\n\n")

	if strings.Contains(string(rendered), "\r") {
		t.Fatalf("expected no carriage returns in rendered output, got %q", rendered)
	}
	if !strings.HasSuffix(string(rendered), "line two\n") {
		t.Fatalf("expected trailing whitespace/blank lines trimmed to exactly one newline, got %q", rendered)
	}
	if strings.HasSuffix(string(rendered), "line two\n\n") {
		t.Fatalf("expected exactly one trailing newline, got more: %q", rendered)
	}
}

func TestRenderParseRoundTrip(t *testing.T) {
	meta := testNoteMeta()
	body := "1. Warm up for five minutes\n2. Bring on load gradually"

	rendered := renderNoteFile(meta, body)
	gotMeta, gotBody, err := parseNoteFile(rendered)
	if err != nil {
		t.Fatalf("parseNoteFile: %v", err)
	}
	if gotMeta.ID != meta.ID || gotMeta.Title != meta.Title || gotMeta.Type != meta.Type {
		t.Fatalf("parseNoteFile meta mismatch: got %+v, want id/title/type from %+v", gotMeta, meta)
	}
	if !gotMeta.Created.Equal(meta.Created) {
		t.Fatalf("parseNoteFile Created = %v, want %v", gotMeta.Created, meta.Created)
	}
	wantTags := []string{"engine-room", "genset"} // sorted on render
	if len(gotMeta.Tags) != len(wantTags) {
		t.Fatalf("parseNoteFile Tags = %v, want %v", gotMeta.Tags, wantTags)
	}
	for i, tag := range wantTags {
		if gotMeta.Tags[i] != tag {
			t.Fatalf("parseNoteFile Tags = %v, want %v", gotMeta.Tags, wantTags)
		}
	}
	// parseNoteFile returns the body exactly as it sits on disk after the
	// closing fence, which always ends in the single "\n" renderNoteFile
	// appends - it does not re-trim on the way back out (see its own doc
	// comment), so the round trip gains that one trailing newline back.
	if gotBody != body+"\n" {
		t.Fatalf("parseNoteFile body = %q, want %q", gotBody, body+"\n")
	}

	// Re-rendering what was just parsed must reproduce the exact same
	// bytes - the round trip that makes "if the new sha equals the
	// current sha, do nothing" (plan §1) a real optimisation rather than
	// one that never fires.
	again := renderNoteFile(gotMeta, gotBody)
	if string(again) != string(rendered) {
		t.Fatalf("re-rendering parsed output did not reproduce the original bytes:\noriginal: %q\nagain:    %q", rendered, again)
	}
}

func TestRenderParseRoundTrip_EmptyTags(t *testing.T) {
	meta := testNoteMeta()
	meta.Tags = nil
	rendered := renderNoteFile(meta, "just some body text")

	gotMeta, gotBody, err := parseNoteFile(rendered)
	if err != nil {
		t.Fatalf("parseNoteFile: %v", err)
	}
	if len(gotMeta.Tags) != 0 {
		t.Fatalf("expected no tags, got %v", gotMeta.Tags)
	}
	if gotBody != "just some body text\n" {
		t.Fatalf("unexpected body: %q", gotBody)
	}
	if !strings.Contains(string(rendered), "tags: []") {
		t.Fatalf("expected an explicit empty tags list, got %q", rendered)
	}
}

func TestParseNoteFile_MissingLeadingDelimiterFailsLoudly(t *testing.T) {
	_, _, err := parseNoteFile([]byte("title: not a note\nbody text"))
	if !errors.Is(err, errNoteFileFrontmatterMissing) {
		t.Fatalf("expected errNoteFileFrontmatterMissing, got %v", err)
	}
}

func TestParseNoteFile_UnclosedFrontmatterFailsLoudly(t *testing.T) {
	_, _, err := parseNoteFile([]byte("---\nid: x\ntitle: y\nno closing fence here"))
	if !errors.Is(err, errNoteFileFrontmatterUnclosed) {
		t.Fatalf("expected errNoteFileFrontmatterUnclosed, got %v", err)
	}
}

func TestParseNoteFile_HorizontalRuleAloneIsNotFrontmatter(t *testing.T) {
	// A leading "---" with nothing that reads as a closing fence anywhere
	// in the file is an ordinary Markdown horizontal rule that happens to
	// open the document, not malformed frontmatter - parseNoteFile still
	// has to reject it (it is never a valid note file), but via the
	// "unclosed" error, not a panic or a misparse.
	_, _, err := parseNoteFile([]byte("---\nSome paragraph that follows a rule, with no second fence.\n"))
	if !errors.Is(err, errNoteFileFrontmatterUnclosed) {
		t.Fatalf("expected errNoteFileFrontmatterUnclosed, got %v", err)
	}
}

func TestParseNoteFile_InvalidCreatedTimestamp(t *testing.T) {
	raw := "---\nid: x\ntitle: y\ntype: note\ntags: []\ncreated: not-a-timestamp\n---\nbody\n"
	_, _, err := parseNoteFile([]byte(raw))
	if err == nil {
		t.Fatalf("expected an error for an unparseable created timestamp")
	}
}

func TestDeriveNoteTitle(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "ATX heading wins over first line",
			body: "# Genset start-up\nSome body text follows.",
			want: "Genset start-up",
		},
		{
			name: "deeper heading level still strips all hashes",
			body: "### Winterising steps\nMore text.",
			want: "Winterising steps",
		},
		{
			name: "no heading falls back to first non-blank line",
			body: "\n\nRing Dave about the mooring, 555-0142.",
			want: "Ring Dave about the mooring, 555-0142.",
		},
		{
			name: "first line truncated to 80 runes",
			body: strings.Repeat("a", 120),
			want: strings.Repeat("a", 80),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deriveNoteTitle(tc.body); got != tc.want {
				t.Fatalf("deriveNoteTitle(%q) = %q, want %q", tc.body, got, tc.want)
			}
		})
	}
}

// ── checklist items (plan §3) ────────────────────────────────────────────

// TestNormalizeChecklistItemText mirrors, fixture for fixture,
// frontend/src/test/checklist-item-text.test.ts's own cases against
// normalizeChecklistItemText (frontend/src/lib/checklist-item-text.ts) -
// this is the Go side's INDEPENDENT implementation of the identical stated
// rule ("strip the leading task marker, strip inline emphasis/code
// delimiters, collapse internal whitespace, trim"), not a port of the TS
// one, but both must agree on every one of these lines or a tick keyed on
// one side and read on the other would silently disagree.
func TestNormalizeChecklistItemText(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string
	}{
		{"strips a leading unchecked GFM marker", "- [ ] Seacocks open", "Seacocks open"},
		{"strips a leading checked GFM marker", "- [x] Seacocks open", "Seacocks open"},
		{"strips a leading checked GFM marker, uppercase X", "- [X] Seacocks open", "Seacocks open"},
		{"strips a leading marker using * instead of -", "* [ ] Seacocks open", "Seacocks open"},
		{"is unaffected by bolding a word", "- [ ] **Seacocks** open", "Seacocks open"},
		{"is unaffected by italicising a word", "- [ ] Seacocks *open*", "Seacocks open"},
		{"is unaffected by inline code marks", "- [ ] Check the `raw water` strainer", "Check the raw water strainer"},
		{"collapses internal whitespace", "- [ ] Seacocks   open", "Seacocks open"},
		{"trims leading and trailing whitespace", "- [ ]   Seacocks open  ", "Seacocks open"},
		{"leaves plain text with no checkbox marker alone", "Seacocks open", "Seacocks open"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeChecklistItemText(tc.line); got != tc.want {
				t.Fatalf("normalizeChecklistItemText(%q) = %q, want %q", tc.line, got, tc.want)
			}
		})
	}
}

// TestNormalizeChecklistItemText_EmphasisChangeIsInvisible is the specific
// property the whole function exists for, asserted directly rather than
// only via two cases above happening to produce the same "want": a
// checklist item's key must not move when the WYSIWYG editor round-trips
// emphasis differently than it was typed (plan §3's own worked example).
func TestNormalizeChecklistItemText_EmphasisChangeIsInvisible(t *testing.T) {
	before := normalizeChecklistItemText("- [ ] Seacocks open")
	after := normalizeChecklistItemText("- [ ] **Seacocks** open")
	if before != after {
		t.Fatalf("expected emphasis to be invisible to normalisation: before=%q after=%q", before, after)
	}
}

// TestParseChecklistItems is the pure, no-store half of plan §3's checklist
// runs: given a note's raw body, which lines are checklist items, what
// their normalised text and nesting depth are, and - the case a run's
// PRIMARY KEY (run_id, item_key, occurrence) exists for - that two
// identical lines get distinct occurrences rather than colliding.
func TestParseChecklistItems(t *testing.T) {
	t.Run("an unchecked dash item", func(t *testing.T) {
		items := parseChecklistItems("- [ ] Seacocks open")
		if len(items) != 1 {
			t.Fatalf("expected 1 item, got %d: %+v", len(items), items)
		}
		if items[0].Text != "Seacocks open" {
			t.Fatalf("unexpected text: %q", items[0].Text)
		}
		if items[0].Occurrence != 0 {
			t.Fatalf("expected occurrence 0, got %d", items[0].Occurrence)
		}
		if items[0].Depth != 0 {
			t.Fatalf("expected depth 0, got %d", items[0].Depth)
		}
	})

	t.Run("a checked dash item", func(t *testing.T) {
		items := parseChecklistItems("- [x] Seacocks open")
		if len(items) != 1 {
			t.Fatalf("expected 1 item, got %d: %+v", len(items), items)
		}
		if items[0].Text != "Seacocks open" {
			t.Fatalf("unexpected text: %q", items[0].Text)
		}
	})

	t.Run("a star-marker item", func(t *testing.T) {
		items := parseChecklistItems("* [ ] Seacocks open")
		if len(items) != 1 {
			t.Fatalf("expected 1 item, got %d: %+v", len(items), items)
		}
		if items[0].Text != "Seacocks open" {
			t.Fatalf("unexpected text: %q", items[0].Text)
		}
	})

	t.Run("nested depth", func(t *testing.T) {
		body := "- [ ] Top item\n  - [ ] Sub item\n    - [ ] Sub sub item\n- [ ] Second top item"
		items := parseChecklistItems(body)
		if len(items) != 4 {
			t.Fatalf("expected 4 items, got %d: %+v", len(items), items)
		}
		wantDepths := []int{0, 1, 2, 0}
		for i, want := range wantDepths {
			if items[i].Depth != want {
				t.Fatalf("item %d (%q): expected depth %d, got %d", i, items[i].Text, want, items[i].Depth)
			}
		}
	})

	t.Run("a line merely containing [x] mid-sentence is not an item", func(t *testing.T) {
		body := "- Remember to flip the switch [x] before starting\nNote: [x] means done"
		items := parseChecklistItems(body)
		if len(items) != 0 {
			t.Fatalf("expected no checklist items, got %d: %+v", len(items), items)
		}
	})

	t.Run("duplicate identical lines get distinct occurrences", func(t *testing.T) {
		body := "- [ ] Check bilge\n- [ ] Check bilge\n- [ ] Check bilge"
		items := parseChecklistItems(body)
		if len(items) != 3 {
			t.Fatalf("expected 3 items, got %d: %+v", len(items), items)
		}
		key := items[0].Key
		for i, item := range items {
			if item.Key != key {
				t.Fatalf("item %d: expected identical lines to share a key, got %q vs %q", i, item.Key, key)
			}
			if item.Occurrence != i {
				t.Fatalf("item %d: expected occurrence %d, got %d", i, i, item.Occurrence)
			}
		}
	})

	t.Run("an emphasis-only difference still keys identically", func(t *testing.T) {
		items := parseChecklistItems("- [ ] Seacocks open\n- [ ] **Seacocks** open")
		if len(items) != 2 {
			t.Fatalf("expected 2 items, got %d: %+v", len(items), items)
		}
		if items[0].Key != items[1].Key {
			t.Fatalf("expected the two items to share a key despite the emphasis difference: %q vs %q", items[0].Key, items[1].Key)
		}
		if items[1].Occurrence != 1 {
			t.Fatalf("expected the second identical-by-key item to get occurrence 1, got %d", items[1].Occurrence)
		}
	})

	t.Run("a plain bullet with no checkbox is not an item", func(t *testing.T) {
		items := parseChecklistItems("- Just a plain bullet\n1. A numbered line")
		if len(items) != 0 {
			t.Fatalf("expected no checklist items, got %d: %+v", len(items), items)
		}
	})

	t.Run("no checklist items in an empty or prose-only body", func(t *testing.T) {
		if items := parseChecklistItems(""); len(items) != 0 {
			t.Fatalf("expected no items for an empty body, got %+v", items)
		}
		if items := parseChecklistItems("Just some prose.\nNo lists here."); len(items) != 0 {
			t.Fatalf("expected no items for a prose-only body, got %+v", items)
		}
	})
}
