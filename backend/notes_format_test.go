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
