package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// ── MIME detection ──────────────────────────────────────────────────────

func TestDetectDocumentMIME(t *testing.T) {
	pdfHeader := []byte("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n1 0 obj\n")
	jpegHeader := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'}
	pngHeader := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	gifHeader := []byte("GIF89a")
	webpHeader := append([]byte("RIFF\x00\x00\x00\x00WEBPVP"), 0)
	plainText := []byte("just some ordinary ASCII text, nothing special here\n")
	binaryJunk := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}

	tests := []struct {
		name     string
		head     []byte
		filename string
		want     string
	}{
		{"pdf by sniff", pdfHeader, "manual.pdf", "application/pdf"},
		{"pdf regardless of extension", pdfHeader, "manual.bin", "application/pdf"},
		{"jpeg", jpegHeader, "receipt.jpg", "image/jpeg"},
		{"png", pngHeader, "screenshot.png", "image/png"},
		{"gif", gifHeader, "diagram.gif", "image/gif"},
		{"webp", webpHeader, "photo.webp", "image/webp"},
		{"heic overrides sniffing", binaryJunk, "IMG_0001.heic", "image/heic"},
		{"heif overrides sniffing", binaryJunk, "IMG_0002.HEIF", "image/heic"},
		{"text/plain sniff refined to markdown", plainText, "notes.md", "text/markdown"},
		{"text/plain sniff refined to markdown (long ext)", plainText, "notes.markdown", "text/markdown"},
		{"text/plain sniff refined to csv", plainText, "expenses.csv", "text/csv"},
		{"text/plain sniff refined to json", plainText, "settings.json", "application/json"},
		{"text/plain sniff kept for txt", plainText, "readme.txt", "text/plain"},
		{"text/plain sniff kept for log", plainText, "engine.log", "text/plain"},
		{"text/plain sniff kept for nmea", plainText, "track.nmea", "text/plain"},
		{"text/plain sniff kept for gpx", plainText, "track.gpx", "text/plain"},
		{"text/plain sniff with unknown extension falls to octet-stream", plainText, "notes.xyz", "application/octet-stream"},
		{"text/plain sniff with no extension falls to octet-stream", plainText, "README", "application/octet-stream"},
		{"unknown binary is octet-stream", binaryJunk, "firmware.bin", "application/octet-stream"},
		{"extension is case-insensitive", plainText, "NOTES.MD", "text/markdown"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := detectDocumentMIME(tc.head, tc.filename)
			if got != tc.want {
				t.Fatalf("detectDocumentMIME(%q, %q) = %q, want %q", tc.head, tc.filename, got, tc.want)
			}
		})
	}
}

// ── text extraction ─────────────────────────────────────────────────────

func writeTestFile(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestExtractDocumentText_TextFile(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "notes.txt", []byte("line one\nline two\n"))

	ex, err := extractDocumentText(context.Background(), path, "text/plain")
	if err != nil {
		t.Fatalf("extractDocumentText: %v", err)
	}
	if ex.Markdown != "line one\nline two\n" {
		t.Fatalf("expected the raw text back, got %q", ex.Markdown)
	}
	if ex.NeedsOCR {
		t.Fatalf("expected a text file to never need OCR")
	}
}

func TestExtractDocumentText_TextFileInvalidUTF8IsSanitised(t *testing.T) {
	dir := t.TempDir()
	// 0xFF is never valid UTF-8 on its own.
	raw := append([]byte("valid before "), 0xFF, 0xFE)
	raw = append(raw, []byte(" valid after")...)
	path := writeTestFile(t, dir, "mixed.txt", raw)

	ex, err := extractDocumentText(context.Background(), path, "text/plain")
	if err != nil {
		t.Fatalf("extractDocumentText: %v", err)
	}
	if strings.Contains(ex.Markdown, "\xFF") || strings.Contains(ex.Markdown, "\xFE") {
		t.Fatalf("expected invalid UTF-8 bytes to be stripped, got %q", ex.Markdown)
	}
	if !strings.Contains(ex.Markdown, "valid before") || !strings.Contains(ex.Markdown, "valid after") {
		t.Fatalf("expected the valid surrounding text to survive, got %q", ex.Markdown)
	}
}

func TestExtractDocumentText_TextFileOverCapIsAnErrorNotATruncation(t *testing.T) {
	dir := t.TempDir()
	big := make([]byte, maxTextExtractBytes+1)
	for i := range big {
		big[i] = 'a'
	}
	path := writeTestFile(t, dir, "huge.txt", big)

	_, err := extractDocumentText(context.Background(), path, "text/plain")
	if err == nil {
		t.Fatalf("expected an error for a text file over the cap, got none")
	}
}

func TestExtractDocumentText_TextFileAtCapSucceeds(t *testing.T) {
	dir := t.TempDir()
	exact := make([]byte, maxTextExtractBytes)
	for i := range exact {
		exact[i] = 'a'
	}
	path := writeTestFile(t, dir, "exact.txt", exact)

	if _, err := extractDocumentText(context.Background(), path, "text/plain"); err != nil {
		t.Fatalf("expected a file exactly at the cap to succeed, got %v", err)
	}
}

func TestExtractDocumentText_Image(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "photo.jpg", []byte{0xFF, 0xD8, 0xFF, 0xE0})

	ex, err := extractDocumentText(context.Background(), path, "image/jpeg")
	if err != nil {
		t.Fatalf("extractDocumentText: %v", err)
	}
	if ex.Markdown != "" || len(ex.Pages) != 0 {
		t.Fatalf("expected no local text for an image, got %+v", ex)
	}
	if !ex.NeedsOCR {
		t.Fatalf("expected an image to need OCR")
	}
}

func TestExtractDocumentText_OctetStream(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "firmware.bin", []byte{0x00, 0x01, 0x02})

	ex, err := extractDocumentText(context.Background(), path, "application/octet-stream")
	if err != nil {
		t.Fatalf("extractDocumentText: %v", err)
	}
	if ex.Markdown != "" || len(ex.Pages) != 0 {
		t.Fatalf("expected no text extracted for octet-stream, got %+v", ex)
	}
	if ex.NeedsOCR {
		t.Fatalf("expected octet-stream to never be flagged for OCR (there's nothing to OCR)")
	}
}

// ── PDF extraction ───────────────────────────────────────────────────────

func testdataPath(name string) string {
	return filepath.Join("testdata", "documents", name)
}

func TestExtractDocumentText_PDFYieldsWordsPerPage(t *testing.T) {
	ex, err := extractDocumentText(context.Background(), testdataPath("two_page.pdf"), "application/pdf")
	if err != nil {
		t.Fatalf("extractDocumentText: %v", err)
	}
	if ex.PageCount != 2 {
		t.Fatalf("expected 2 pages, got %d", ex.PageCount)
	}
	if len(ex.Pages) != 2 {
		t.Fatalf("expected 2 extracted pages, got %d", len(ex.Pages))
	}
	if ex.Pages[0].Number != 1 || !strings.Contains(ex.Pages[0].Text, "IMPELLERTOKEN") {
		t.Fatalf("expected page 1 to contain IMPELLERTOKEN, got %+v", ex.Pages[0])
	}
	if ex.Pages[1].Number != 2 || !strings.Contains(ex.Pages[1].Text, "THERMOSTATTOKEN") {
		t.Fatalf("expected page 2 to contain THERMOSTATTOKEN, got %+v", ex.Pages[1])
	}
	if !strings.Contains(ex.Markdown, "IMPELLERTOKEN") || !strings.Contains(ex.Markdown, "THERMOSTATTOKEN") {
		t.Fatalf("expected Markdown to join both pages, got %q", ex.Markdown)
	}
	if !strings.Contains(ex.Markdown, "\n\n") {
		t.Fatalf("expected pages to be joined with a blank-line separator, got %q", ex.Markdown)
	}
	if ex.NeedsOCR {
		t.Fatalf("expected a text-rich PDF to not need OCR")
	}
}

func TestExtractDocumentText_TruncatedPDFErrorsWithoutPanicking(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("extractDocumentText panicked on a truncated PDF: %v", r)
		}
	}()
	_, err := extractDocumentText(context.Background(), testdataPath("two_page_truncated.pdf"), "application/pdf")
	if err == nil {
		t.Fatalf("expected an error for a truncated PDF")
	}
}

func TestExtractDocumentText_GarbageBytesNamedPDFErrorsWithoutPanicking(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("extractDocumentText panicked on garbage bytes: %v", r)
		}
	}()
	_, err := extractDocumentText(context.Background(), testdataPath("garbage.pdf"), "application/pdf")
	if err == nil {
		t.Fatalf("expected an error for garbage bytes named .pdf")
	}
}

func TestExtractDocumentText_EmptyTextPDFNeedsOCR(t *testing.T) {
	ex, err := extractDocumentText(context.Background(), testdataPath("empty_text.pdf"), "application/pdf")
	if err != nil {
		t.Fatalf("extractDocumentText: %v", err)
	}
	if ex.PageCount != 1 {
		t.Fatalf("expected 1 page, got %d", ex.PageCount)
	}
	if !ex.NeedsOCR {
		t.Fatalf("expected an image-only/empty-text PDF to need OCR")
	}
}

func TestExtractDocumentText_PDFPageCapExceeded(t *testing.T) {
	// extractPDF's cap check reads NumPage() - a single /Count integer - and
	// returns before ever walking Kids, so a single dummy leaf page behind a
	// dishonestly large /Count is enough to exercise the cap without
	// materialising thousands of real pages.
	body := buildMinimalPDFWithPagesDict(t,
		"<< /Type /Pages /Kids [4 0 R] /Count "+strconv.Itoa(maxPDFPages+1)+" >>",
	)
	dir := t.TempDir()
	path := writeTestFile(t, dir, "toobig.pdf", body)

	_, err := extractDocumentText(context.Background(), path, "application/pdf")
	if err == nil {
		t.Fatalf("expected an error once the declared page count exceeds the cap")
	}
}

// TestExtractDocumentText_PDFSecondPageBrokenContentStreamIsVisibleNotSilent
// hand-crafts a 2-page PDF (via buildTwoPagePDFPage2BrokenContentStream
// below) whose second page's content stream contains a malformed operator
// (a "Tf" with only one operand - ledongthuc/pdf's Interpret panics with
// "bad TL" on that, recovered by GetPlainText into a returned error, same
// as a genuinely corrupt content stream would produce) - a broken content
// stream extractPDFPage can neither parse nor blame on a truncated file.
// extractPDF must not silently drop page 2's failure (the B2-review gap
// ADR 0106 calls out): the document still succeeds as a whole (page 1 is
// fine), but FailedPages/FirstPageErr make page 2's failure visible to the
// indexer (documents_indexer.go) rather than a document that simply looks
// like it has less text than it should.
func TestExtractDocumentText_PDFSecondPageBrokenContentStreamIsVisibleNotSilent(t *testing.T) {
	body := buildTwoPagePDFPage2BrokenContentStream(t)
	dir := t.TempDir()
	path := writeTestFile(t, dir, "broken-page-2.pdf", body)

	ex, err := extractDocumentText(context.Background(), path, "application/pdf")
	if err != nil {
		t.Fatalf("extractDocumentText: unexpected top-level error (page 1 should still succeed): %v", err)
	}
	if ex.PageCount != 2 {
		t.Fatalf("expected 2 declared pages, got %d", ex.PageCount)
	}
	if len(ex.Pages) != 1 || ex.Pages[0].Number != 1 {
		t.Fatalf("expected only page 1 to have extracted, got %+v", ex.Pages)
	}
	if len(ex.FailedPages) != 1 || ex.FailedPages[0] != 2 {
		t.Fatalf("expected FailedPages [2], got %v", ex.FailedPages)
	}
	if ex.FirstPageErr == "" {
		t.Fatalf("expected a non-empty FirstPageErr describing page 2's failure")
	}
	if !strings.Contains(ex.FirstPageErr, "2") {
		t.Fatalf("expected FirstPageErr to reference page 2, got %q", ex.FirstPageErr)
	}
}

// buildTwoPagePDFPage2BrokenContentStream assembles a well-formed 2-page
// PDF (correct header, xref table and trailer) using the same object layout
// as buildMinimalPDFWithPagesDict, except page 2 (object 6)'s content
// stream (object 7) carries a malformed "Tf" operator (one operand instead
// of the required two: font name with no size). ledongthuc/pdf's Interpret
// panics on that ("bad TL"), which Page.GetPlainText recovers into a
// returned error - the same observable shape (extractPDFPage's own pageErr)
// a genuinely corrupted or truncated stream produces, without needing a
// truncated file (already covered by two_page_truncated.pdf) or a dangling
// object reference (which ledongthuc/pdf resolves to Null and silently
// treats as an empty page - not a failure at all).
func buildTwoPagePDFPage2BrokenContentStream(t *testing.T) []byte {
	t.Helper()
	var buf strings.Builder
	offsets := make(map[int]int)

	writeObj := func(num int, body string) {
		offsets[num] = buf.Len()
		buf.WriteString(strconv.Itoa(num))
		buf.WriteString(" 0 obj\n")
		buf.WriteString(body)
		buf.WriteString("\nendobj\n")
	}

	const page2Stream = "BT /F1 Tf (PAGE TWO BROKEN) Tj ET"

	buf.WriteString("%PDF-1.4\n")
	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObj(2, "<< /Type /Pages /Kids [4 0 R 6 0 R] /Count 2 >>")
	writeObj(3, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	writeObj(4, "<< /Type /Page /Parent 2 0 R /Resources << /Font << /F1 3 0 R >> >> /MediaBox [0 0 612 792] /Contents 5 0 R >>")
	writeObj(5, "<< /Length 44 >>\nstream\nBT /F1 12 Tf 72 700 Td (PAGE ONE OK) Tj ET\nendstream")
	writeObj(6, "<< /Type /Page /Parent 2 0 R /Resources << /Font << /F1 3 0 R >> >> /MediaBox [0 0 612 792] /Contents 7 0 R >>")
	writeObj(7, "<< /Length "+strconv.Itoa(len(page2Stream))+" >>\nstream\n"+page2Stream+"\nendstream")

	xrefStart := buf.Len()
	buf.WriteString("xref\n0 8\n")
	buf.WriteString("0000000000 65535 f \n")
	for i := 1; i <= 7; i++ {
		buf.WriteString(pad10(offsets[i]))
		buf.WriteString(" 00000 n \n")
	}
	buf.WriteString("trailer\n<< /Size 8 /Root 1 0 R >>\nstartxref\n")
	buf.WriteString(strconv.Itoa(xrefStart))
	buf.WriteString("\n%%EOF")

	return []byte(buf.String())
}

func TestExtractDocumentText_ContextCancelledBetweenPages(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := extractDocumentText(ctx, testdataPath("two_page.pdf"), "application/pdf")
	if err == nil {
		t.Fatalf("expected an error once the context is already cancelled")
	}
}

// buildMinimalPDFWithPagesDict assembles a tiny, well-formed PDF (correct
// header, xref table and trailer) whose object 2 is the caller-supplied
// Pages dictionary body, with a single leaf Page (object 4) with an empty
// content stream to satisfy the Kids references.
func buildMinimalPDFWithPagesDict(t *testing.T, pagesDict string) []byte {
	t.Helper()
	var buf strings.Builder
	offsets := make(map[int]int)

	writeObj := func(num int, body string) {
		offsets[num] = buf.Len()
		buf.WriteString(strconv.Itoa(num))
		buf.WriteString(" 0 obj\n")
		buf.WriteString(body)
		buf.WriteString("\nendobj\n")
	}

	buf.WriteString("%PDF-1.4\n")
	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObj(2, pagesDict)
	writeObj(3, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	writeObj(4, "<< /Type /Page /Parent 2 0 R /Resources << /Font << /F1 3 0 R >> >> /MediaBox [0 0 612 792] /Contents 5 0 R >>")
	writeObj(5, "<< /Length 0 >>\nstream\n\nendstream")

	xrefStart := buf.Len()
	buf.WriteString("xref\n0 6\n")
	buf.WriteString("0000000000 65535 f \n")
	for i := 1; i <= 5; i++ {
		buf.WriteString(pad10(offsets[i]))
		buf.WriteString(" 00000 n \n")
	}
	buf.WriteString("trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n")
	buf.WriteString(strconv.Itoa(xrefStart))
	buf.WriteString("\n%%EOF")

	return []byte(buf.String())
}

func pad10(n int) string {
	s := strconv.Itoa(n)
	for len(s) < 10 {
		s = "0" + s
	}
	return s
}
