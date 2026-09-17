package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ledongthuc/pdf"
)

// maxTextExtractBytes caps how large a text-family file (text/plain,
// text/markdown, text/csv, application/json) extractDocumentText will read
// into memory. A file over the cap is a hard error (AGENTS.md's fallback
// policy: fail fast rather than silently truncate a document and index only
// part of it).
const maxTextExtractBytes = 20 * 1024 * 1024 // 20 MB

// maxPDFPages caps how many pages extractDocumentText will read from a PDF.
// A PDF that declares more pages than this fails outright rather than
// spending minutes walking a pathological file.
const maxPDFPages = 2000

// pdfNeedsOCRAvgCharsPerPage is the average trimmed-characters-per-page
// threshold below which a PDF's text layer is considered too thin to trust
// (e.g. a scanned page with only a handful of OCR'd header characters) and
// extractDocumentText flags NeedsOCR instead.
const pdfNeedsOCRAvgCharsPerPage = 100

// textExtensionMIME refines an http.DetectContentType result of
// "text/plain*" using nothing but a fixed, lowercase-extension lookup - no
// mime.TypeByExtension, which depends on /etc/mime.types and has nothing to
// read on Alpine. An extension not in this map means the file is kept as
// application/octet-stream (indexed with no text) rather than guessed at.
var textExtensionMIME = map[string]string{
	".md":       "text/markdown",
	".markdown": "text/markdown",
	".csv":      "text/csv",
	".json":     "application/json",
	".txt":      "text/plain",
	".log":      "text/plain",
	".nmea":     "text/plain",
	".gpx":      "text/plain",
}

// textMIMETypes is the set of MIME strings detectDocumentMIME can produce
// that extractDocumentText treats as plain text: read whole, validated as
// UTF-8, no page structure.
var textMIMETypes = map[string]bool{
	"text/plain":       true,
	"text/markdown":    true,
	"text/csv":         true,
	"application/json": true,
}

// detectDocumentMIME classifies an uploaded file from its first bytes (head)
// and its filename. http.DetectContentType alone decides the binary formats
// it already recognises correctly (pdf/jpeg/png/gif/webp); everything it
// calls "text/plain" is refined by textExtensionMIME because the sniffer
// itself can't tell a CSV from an NMEA log from a plain log file. HEIC/HEIF
// are special-cased on extension alone: Go's sniffer has no signature for
// them at all, so they'd otherwise always fall through to octet-stream.
// Anything else - including a text/plain sniff whose extension isn't in the
// fixed map - is application/octet-stream: indexed with no local text,
// never guessed at.
func detectDocumentMIME(head []byte, filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))

	if ext == ".heic" || ext == ".heif" {
		return "image/heic"
	}

	sniffed := http.DetectContentType(head)
	switch {
	case strings.HasPrefix(sniffed, "application/pdf"):
		return "application/pdf"
	case strings.HasPrefix(sniffed, "image/jpeg"):
		return "image/jpeg"
	case strings.HasPrefix(sniffed, "image/png"):
		return "image/png"
	case strings.HasPrefix(sniffed, "image/gif"):
		return "image/gif"
	case strings.HasPrefix(sniffed, "image/webp"):
		return "image/webp"
	}

	if strings.HasPrefix(sniffed, "text/plain") {
		if mt, ok := textExtensionMIME[ext]; ok {
			return mt
		}
	}

	return "application/octet-stream"
}

// extractedPage is one page's worth of extracted text, 1-indexed to match
// how PDF viewers and the rest of the document store (page_start/page_end)
// number pages.
type extractedPage struct {
	Number int
	Text   string
}

// extractedDocument is extractDocumentText's result: per-page text when the
// source has real pages (PDFs), a flattened Markdown/text view used both for
// non-paged chunking and for B4's enrichment prompt, and whether the local
// text is thin enough that Mate's OCR should be offered. FailedPages and
// FirstPageErr surface a partial PDF failure (some but not all pages)
// rather than letting it disappear silently: extractPDF used to just skip a
// failed page's text and move on, which meant a page's worth of missing
// text never showed up anywhere the operator could see it (found in B2
// review, addressed in B4 - documents_indexer.go writes FailedPages/
// FirstPageErr to documents.error so it stays visible even once the
// document reaches indexed).
type extractedDocument struct {
	Pages     []extractedPage
	PageCount int
	Markdown  string
	NeedsOCR  bool

	// FailedPages holds the 1-indexed page numbers extractPDFPage could not
	// read, in ascending order. Empty for every non-PDF source and for a
	// PDF whose pages all extracted cleanly.
	FailedPages []int
	// FirstPageErr is the first failed page's own error text (extractPDF's
	// firstErr, already page-numbered and path-qualified). Empty exactly
	// when FailedPages is empty.
	FirstPageErr string
}

// extractDocumentText extracts whatever local, non-paid text it can from the
// file at path, dispatching purely on mime (as produced by
// detectDocumentMIME). It never calls out to Mate or any other paid
// service - that's B4's enrich stage, gated on operator consent.
func extractDocumentText(ctx context.Context, path, mimeType string) (extractedDocument, error) {
	switch {
	case textMIMETypes[mimeType]:
		return extractTextFile(path)
	case mimeType == "application/pdf":
		return extractPDF(ctx, path)
	case strings.HasPrefix(mimeType, "image/"):
		return extractedDocument{NeedsOCR: true}, nil
	default:
		// application/octet-stream, and anything else detectDocumentMIME
		// didn't recognise: no local text, and nothing Mate's OCR could
		// usefully do with it either.
		return extractedDocument{}, nil
	}
}

// extractTextFile reads path whole (capped at maxTextExtractBytes - an
// oversized file is an error, never a silent truncation) and sanitises it to
// valid UTF-8.
func extractTextFile(path string) (extractedDocument, error) {
	info, err := os.Stat(path)
	if err != nil {
		return extractedDocument{}, fmt.Errorf("stat text file: %w", err)
	}
	if info.Size() > maxTextExtractBytes {
		return extractedDocument{}, fmt.Errorf("text file %s is %d bytes, over the %d byte cap", path, info.Size(), maxTextExtractBytes)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return extractedDocument{}, fmt.Errorf("read text file: %w", err)
	}

	text := strings.ToValidUTF8(string(raw), "")
	return extractedDocument{Markdown: text}, nil
}

// extractPDF extracts text from a PDF one page at a time via
// github.com/ledongthuc/pdf, the one pure-Go PDF text library chosen for
// this project (ADR 0106): CGO stays off for the armv7 build, so a
// cgo-backed renderer was never an option. Failures are handled at three
// levels: opening the reader itself is wrapped in a recover (belt-and-braces
// alongside the library's own internal recover in NewReaderEncrypted), the
// page count is capped before any page is touched, and each page's
// GetPlainText runs behind its own recover so one malformed page can't take
// down the whole document. The document as a whole only fails if every page
// failed; a partial page failure just means that page contributes no text.
func extractPDF(ctx context.Context, path string) (extractedDocument, error) {
	f, reader, err := openPDFReader(path)
	if err != nil {
		return extractedDocument{}, err
	}
	defer f.Close()

	numPages := reader.NumPage()
	if numPages > maxPDFPages {
		return extractedDocument{}, fmt.Errorf("pdf %s declares %d pages, over the %d page cap", path, numPages, maxPDFPages)
	}

	fonts := make(map[string]*pdf.Font)
	pages := make([]extractedPage, 0, numPages)
	var firstErr error
	var failedPages []int

	for i := 1; i <= numPages; i++ {
		if err := ctx.Err(); err != nil {
			return extractedDocument{}, fmt.Errorf("extract pdf %s: %w", path, err)
		}

		text, pageErr := extractPDFPage(reader, i, fonts)
		if pageErr != nil {
			failedPages = append(failedPages, i)
			if firstErr == nil {
				firstErr = fmt.Errorf("pdf %s page %d: %w", path, i, pageErr)
			}
			continue
		}
		pages = append(pages, extractedPage{Number: i, Text: text})
	}

	if numPages > 0 && len(failedPages) == numPages {
		return extractedDocument{}, firstErr
	}

	mdParts := make([]string, len(pages))
	var totalChars int
	for i, p := range pages {
		mdParts[i] = p.Text
		totalChars += len(strings.TrimSpace(p.Text))
	}

	var needsOCR bool
	if numPages > 0 {
		avg := float64(totalChars) / float64(numPages)
		needsOCR = avg < pdfNeedsOCRAvgCharsPerPage
	}

	var firstPageErr string
	if firstErr != nil {
		firstPageErr = firstErr.Error()
	}

	return extractedDocument{
		Pages:        pages,
		PageCount:    numPages,
		Markdown:     strings.Join(mdParts, "\n\n"),
		NeedsOCR:     needsOCR,
		FailedPages:  failedPages,
		FirstPageErr: firstPageErr,
	}, nil
}

// openPDFReader wraps pdf.Open in its own recover so a panic surfaced while
// merely opening the file (as opposed to reading a specific page) still
// comes back as a plain error - never a crashed indexer goroutine.
func openPDFReader(path string) (f *os.File, reader *pdf.Reader, err error) {
	defer func() {
		if r := recover(); r != nil {
			if f != nil {
				f.Close()
			}
			f, reader = nil, nil
			err = fmt.Errorf("panic opening pdf %s: %v", path, r)
		}
	}()
	f, reader, err = pdf.Open(path)
	return f, reader, err
}

// extractPDFPage reads one page's plain text, recovering any panic raised
// while interpreting that page's content stream into a page-scoped error -
// one malformed page's operators can't take the rest of the document down
// with it.
func extractPDFPage(reader *pdf.Reader, num int, fonts map[string]*pdf.Font) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			text = ""
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	page := reader.Page(num)
	return page.GetPlainText(fonts)
}
