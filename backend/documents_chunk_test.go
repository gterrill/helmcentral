package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// paragraphOfLen returns a single paragraph (no blank lines, no whitespace
// at all) of exactly n runes, built from a repeating letter sequence. It
// deliberately contains no spaces so trimming never changes its length -
// callers that assert on exact rune counts across a split rely on that.
func paragraphOfLen(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	var b strings.Builder
	for b.Len() < n {
		b.WriteString(letters)
	}
	s := []rune(b.String())
	return string(s[:n])
}

func TestChunkDocument_SeqStartsAtOneAndSourceIsLocal(t *testing.T) {
	ex := extractedDocument{Markdown: "a short plain document"}
	chunks := chunkDocument(ex, "text/plain")
	if len(chunks) != 1 {
		t.Fatalf("expected exactly 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Seq != 1 {
		t.Fatalf("expected the first chunk's Seq to be 1 (0 is reserved for the meta chunk), got %d", chunks[0].Seq)
	}
	if chunks[0].Source != "local" {
		t.Fatalf("expected source 'local', got %q", chunks[0].Source)
	}
}

func TestChunkDocument_PlainTextNeverExceedsHardMax(t *testing.T) {
	// Five paragraphs, each smaller than the hard max on its own, but
	// together they must be split across multiple chunks rather than one
	// giant blob.
	paragraphs := make([]string, 5)
	for i := range paragraphs {
		paragraphs[i] = paragraphOfLen(600)
	}
	ex := extractedDocument{Markdown: strings.Join(paragraphs, "\n\n")}

	chunks := chunkDocument(ex, "text/plain")
	if len(chunks) < 2 {
		t.Fatalf("expected more than one chunk for 3000 runes of text, got %d", len(chunks))
	}
	for i, c := range chunks {
		n := utf8.RuneCountInString(c.Text)
		if n > chunkMaxChars {
			t.Fatalf("chunk %d has %d runes, over the hard max %d", i, n, chunkMaxChars)
		}
		if n == 0 {
			t.Fatalf("chunk %d is empty", i)
		}
	}
}

func TestChunkDocument_SingleOversizedParagraphIsHardSplit(t *testing.T) {
	// One paragraph, no blank lines, way over the hard max: must fall back
	// to line/rune splitting rather than exceeding chunkMaxChars.
	huge := paragraphOfLen(chunkMaxChars * 3)
	ex := extractedDocument{Markdown: huge}

	chunks := chunkDocument(ex, "text/plain")
	if len(chunks) < 3 {
		t.Fatalf("expected the oversized paragraph to be split into several chunks, got %d", len(chunks))
	}
	var total int
	for i, c := range chunks {
		n := utf8.RuneCountInString(c.Text)
		if n > chunkMaxChars {
			t.Fatalf("chunk %d has %d runes, over the hard max %d", i, n, chunkMaxChars)
		}
		total += n
	}
	if total != chunkMaxChars*3 {
		t.Fatalf("expected no characters to be lost while splitting, got %d runes total, want %d", total, chunkMaxChars*3)
	}
}

func TestChunkDocument_EmptyTextProducesNoChunks(t *testing.T) {
	ex := extractedDocument{Markdown: "   \n\n  "}
	chunks := chunkDocument(ex, "text/plain")
	if len(chunks) != 0 {
		t.Fatalf("expected no chunks for blank text, got %d", len(chunks))
	}
}

// ── PDF page boundaries ──────────────────────────────────────────────────

func TestChunkDocument_PDFChunksNeverSpanPagesAndCarryPageNumbers(t *testing.T) {
	ex := extractedDocument{
		PageCount: 3,
		Pages: []extractedPage{
			{Number: 1, Text: "Impeller torque spec is 45 Nm."},
			{Number: 2, Text: "Thermostat opens at 82 degrees Celsius."},
			{Number: 3, Text: paragraphOfLen(chunkMaxChars + 500)}, // forces a page to split into 2+ chunks
		},
	}

	chunks := chunkDocument(ex, "application/pdf")
	if len(chunks) < 4 {
		t.Fatalf("expected at least 4 chunks (1+1+2), got %d", len(chunks))
	}

	byPage := map[int][]documentChunk{}
	for _, c := range chunks {
		if c.PageStart != c.PageEnd {
			t.Fatalf("expected a PDF chunk to never span pages, got PageStart=%d PageEnd=%d", c.PageStart, c.PageEnd)
		}
		byPage[c.PageStart] = append(byPage[c.PageStart], c)
	}

	if len(byPage[1]) != 1 || !strings.Contains(byPage[1][0].Text, "Impeller") {
		t.Fatalf("expected exactly 1 chunk for page 1 containing its text, got %+v", byPage[1])
	}
	if len(byPage[2]) != 1 || !strings.Contains(byPage[2][0].Text, "Thermostat") {
		t.Fatalf("expected exactly 1 chunk for page 2 containing its text, got %+v", byPage[2])
	}
	if len(byPage[3]) < 2 {
		t.Fatalf("expected page 3's oversized text to split into multiple chunks, got %d", len(byPage[3]))
	}
	for _, c := range byPage[3] {
		if utf8.RuneCountInString(c.Text) > chunkMaxChars {
			t.Fatalf("page 3 chunk exceeds hard max: %d runes", utf8.RuneCountInString(c.Text))
		}
	}
}

func TestChunkDocument_PDFSkipsBlankPages(t *testing.T) {
	ex := extractedDocument{
		PageCount: 2,
		Pages: []extractedPage{
			{Number: 1, Text: "   \n  "},
			{Number: 2, Text: "Real content on page two."},
		},
	}
	chunks := chunkDocument(ex, "application/pdf")
	if len(chunks) != 1 {
		t.Fatalf("expected only page 2 to produce a chunk, got %d", len(chunks))
	}
	if chunks[0].PageStart != 2 {
		t.Fatalf("expected the surviving chunk to be page 2, got PageStart=%d", chunks[0].PageStart)
	}
}

func TestChunkDocument_PDFSeqIsContinuousAcrossPages(t *testing.T) {
	ex := extractedDocument{
		PageCount: 2,
		Pages: []extractedPage{
			{Number: 1, Text: "first page text"},
			{Number: 2, Text: "second page text"},
		},
	}
	chunks := chunkDocument(ex, "application/pdf")
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].Seq != 1 || chunks[1].Seq != 2 {
		t.Fatalf("expected seq 1 then 2, got %d then %d", chunks[0].Seq, chunks[1].Seq)
	}
}

// ── markdown headings ────────────────────────────────────────────────────

func TestChunkDocument_MarkdownSplitsOnHeadingsAndSetsHeading(t *testing.T) {
	md := "" +
		"Intro text before any heading.\n\n" +
		"# Engine\n\n" +
		"Engine section body.\n\n" +
		"## Impeller\n\n" +
		"Replace every 500 hours.\n\n" +
		"### Torque\n\n" +
		"45 Nm on the housing bolts.\n\n" +
		"# Electrical\n\n" +
		"Electrical section body."

	chunks := chunkDocument(extractedDocument{Markdown: md}, "text/markdown")

	headings := map[string]string{}
	for _, c := range chunks {
		headings[c.Heading] += c.Text + " "
	}

	if !strings.Contains(headings[""], "Intro text before any heading") {
		t.Fatalf("expected the pre-heading text to survive with an empty heading, got %+v", headings)
	}
	if !strings.Contains(headings["Engine"], "Engine section body") {
		t.Fatalf("expected an Engine-headed chunk, got %+v", headings)
	}
	if !strings.Contains(headings["Impeller"], "Replace every 500 hours") {
		t.Fatalf("expected an Impeller-headed chunk, got %+v", headings)
	}
	if !strings.Contains(headings["Torque"], "45 Nm on the housing bolts") {
		t.Fatalf("expected a Torque-headed chunk (### is still within range), got %+v", headings)
	}
	if !strings.Contains(headings["Electrical"], "Electrical section body") {
		t.Fatalf("expected an Electrical-headed chunk, got %+v", headings)
	}
}

func TestChunkDocument_MarkdownIgnoresHeadingsDeeperThanThree(t *testing.T) {
	md := "# Top\n\nTop body.\n\n#### TooDeep\n\nThis stays part of Top, not its own section."
	chunks := chunkDocument(extractedDocument{Markdown: md}, "text/markdown")

	for _, c := range chunks {
		if c.Heading == "TooDeep" {
			t.Fatalf("expected a level-4 heading to not start a new section, got a chunk headed %q", c.Heading)
		}
	}
	var joined string
	for _, c := range chunks {
		joined += c.Text
	}
	if !strings.Contains(joined, "TooDeep") || !strings.Contains(joined, "This stays part of Top") {
		t.Fatalf("expected the #### line and its body to remain in the Top section's text, got %q", joined)
	}
}

func TestChunkDocument_MarkdownNoHeadingsIsOneUnheadedSection(t *testing.T) {
	chunks := chunkDocument(extractedDocument{Markdown: "Just a paragraph, no headings at all."}, "text/markdown")
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Heading != "" {
		t.Fatalf("expected an empty heading when the document has none, got %q", chunks[0].Heading)
	}
}
