package main

import (
	"context"
	"errors"
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

// maxPDFExtractedTextBytes caps the cumulative extracted text accumulated
// across every page of one PDF - mirrors maxTextExtractBytes's cap on a
// text-family file's input size, but this one bounds output, not input.
// github.com/ledongthuc/pdf decompresses a PDF content stream (very often
// flate-compressed) and tokenises it lazily as it goes, and Page.GetPlainText
// accumulates every Tj/TJ operand into its own unbounded buffer with no
// limit of its own - a content stream only a few KB compressed can expand to
// hundreds of MB or more (measured in testing: a 3 KB compressed stream of
// repeated "(AAAA...)Tj" expanded to 660 KB of text; scaled up, that's a
// multi-GB allocation, and Go treats an out-of-memory condition as fatal -
// no defer or recover catches it). See extractPageText for where this is
// actually enforced: checked as text accumulates, not after the page has
// already finished decompressing.
const maxPDFExtractedTextBytes = 20 * 1024 * 1024 // 20 MB, matches maxTextExtractBytes

// errPDFTextBudgetExceeded is the exact value extractPageText panics with
// the moment its shared budget (maxPDFExtractedTextBytes) is exceeded.
// extractPDFPage's recover turns it into a returned error like any other
// page-level panic, but extractPDF's loop checks for this specific value
// (errors.Is) so it can fail the whole document immediately rather than
// treating a blown text budget as an ordinary single-page failure that the
// rest of the document should keep going past.
var errPDFTextBudgetExceeded = errors.New("pdf text extraction exceeded the maximum accumulated text budget")

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
// cgo-backed renderer was never an option. Failures are handled at four
// levels: opening the reader itself is wrapped in a recover (belt-and-braces
// alongside the library's own internal recover in NewReaderEncrypted); the
// page count is read via readPDFPageCount, which has its own recover
// (NumPage() is the first call that actually walks the lazily-resolved page
// tree, and can panic just like Open can - see that function's doc comment)
// and explicitly rejects a negative /Count rather than letting it reach the
// page slice's capacity below; each page's text extraction runs behind
// extractPDFPage's own recover so one malformed page can't take down the
// whole document; and a document-wide extracted-text budget
// (maxPDFExtractedTextBytes, enforced in extractPageText) stops a
// flate-bomb content stream from being decompressed into gigabytes before
// anything notices. The document as a whole only fails if every page
// failed, or if the text budget was blown partway through; an ordinary
// partial page failure just means that page contributes no text and the
// rest of the document proceeds.
func extractPDF(ctx context.Context, path string) (extractedDocument, error) {
	f, reader, err := openPDFReader(path)
	if err != nil {
		return extractedDocument{}, err
	}
	defer f.Close()

	numPages, err := readPDFPageCount(reader, path)
	if err != nil {
		return extractedDocument{}, err
	}
	if numPages > maxPDFPages {
		return extractedDocument{}, fmt.Errorf("pdf %s declares %d pages, over the %d page cap", path, numPages, maxPDFPages)
	}

	fonts := make(map[string]*pdf.Font)
	pages := make([]extractedPage, 0, numPages)
	var firstErr error
	var failedPages []int
	// extractedTextBudget is a running total of extracted-text bytes shared
	// across every iteration of the loop below (extractPDFPage takes a
	// pointer to it) - a document-wide cap, not a per-page one, because
	// /Contents can be one indirect object several pages share, and a
	// per-page-only cap would let the same bomb detonate again on every page
	// that references it.
	var extractedTextBudget int64

	for i := 1; i <= numPages; i++ {
		if err := ctx.Err(); err != nil {
			return extractedDocument{}, fmt.Errorf("extract pdf %s: %w", path, err)
		}

		text, pageErr := extractPDFPage(reader, i, fonts, &extractedTextBudget)
		if pageErr != nil {
			if errors.Is(pageErr, errPDFTextBudgetExceeded) {
				// Not an ordinary single-page failure: the document-wide
				// budget is blown, so there's no point (and no safe way,
				// memory-wise) to keep going into the remaining pages.
				return extractedDocument{}, fmt.Errorf("pdf %s: %w (stopped at page %d of %d declared pages)", path, pageErr, i, numPages)
			}
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

// readPDFPageCount reads reader.NumPage() behind its own recover and rejects
// a negative result outright. pdf.Open only validates the file's outer
// framing (header, xref table, trailer) - github.com/ledongthuc/pdf resolves
// indirect objects LAZILY, so NumPage() (which walks Root -> Pages -> Count)
// is the first call that actually reads the page tree, and therefore the
// first call that can panic on a malformed one. Confirmed by hand: a PDF
// whose xref entry for the Root object points at the wrong byte offset opens
// cleanly (pdf.Open never looks at what's at that offset) and then panics
// inside NumPage() with "unexpected keyword ... parsing object" the moment
// it tries to resolve Root - openPDFReader's own recover, wrapped only
// around pdf.Open, never sees it. Separately, NumPage() does no validation
// of its own beyond int(Count.Int64()) - a forged page tree's /Count can be
// negative, which sails straight past extractPDF's "numPages > maxPDFPages"
// check (false for a negative numPages) and would otherwise reach
// make([]extractedPage, 0, numPages) as an instant "makeslice: cap out of
// range" panic. Rejecting it here, explicitly and by name, keeps that panic
// from ever being reachable instead of adding yet another recover to catch
// it after the fact - a negative page count is exactly as malformed a PDF as
// one that fails to open at all, so it gets the same explicit-error
// treatment as everything else in this file (no clamping to zero: that
// would silently index a document as if it had no pages, when what actually
// happened is a corrupt or hostile page tree).
func readPDFPageCount(reader *pdf.Reader, path string) (numPages int, err error) {
	defer func() {
		if r := recover(); r != nil {
			numPages = 0
			err = fmt.Errorf("panic reading page count of pdf %s: %v", path, r)
		}
	}()
	numPages = reader.NumPage()
	if numPages < 0 {
		return 0, fmt.Errorf("pdf %s declares a negative page count (%d)", path, numPages)
	}
	return numPages, nil
}

// extractPDFPage reads one page's plain text, recovering any panic raised
// while interpreting that page's content stream into a page-scoped error -
// one malformed page's operators can't take the rest of the document down
// with it. budget is extractPDF's shared, document-wide running total of
// extracted-text bytes (see extractPageText's doc comment for why it has to
// be document-wide rather than reset per page). A panic that is exactly
// errPDFTextBudgetExceeded is returned as-is, not rewrapped into the generic
// "panic: %v" error below, so extractPDF's loop can recognise it with
// errors.Is and treat it differently from an ordinary page failure.
func extractPDFPage(reader *pdf.Reader, num int, fonts map[string]*pdf.Font, budget *int64) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			text = ""
			if r == errPDFTextBudgetExceeded {
				err = errPDFTextBudgetExceeded
				return
			}
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	page := reader.Page(num)
	return extractPageText(page, fonts, budget)
}

// extractPageText re-implements github.com/ledongthuc/pdf's own
// Page.GetPlainText (page.go in that module: the same BT/T*/Tf/Tj/TJ/'/"
// operator handling, operator for operator) using the library's own
// exported Interpret hook instead of calling GetPlainText directly. The one
// thing GetPlainText cannot do is stop partway through a page: it
// accumulates every Tj/TJ operand into its own unbounded buffer, and a PDF
// content stream is very often flate-compressed and decompressed LAZILY,
// token by token, as Interpret reads it - so a content stream of only a few
// KB compressed can expand to hundreds of MB or more of accumulated text
// well before GetPlainText would ever return (see maxPDFExtractedTextBytes's
// doc comment for the measured numbers). budget is a running total of
// extracted-text bytes shared across every page of the whole document -
// extractPDF passes the same pointer into each call in its loop, because
// /Contents can be one indirect object several pages share, and a per-page-
// only cap would let the same bomb detonate again on every page that
// references it. Once *budget exceeds maxPDFExtractedTextBytes this panics
// with errPDFTextBudgetExceeded from inside the same closures GetPlainText
// itself writes through (appendText below), so Interpret's token loop
// unwinds immediately and no further bytes of the stream are decompressed at
// all - checked as text accumulates, not only noticed after the fact once
// the damage is already allocated. extractPDFPage's own recover (wrapping
// this call) is what turns that panic into a returned error.
func extractPageText(page pdf.Page, fonts map[string]*pdf.Font, budget *int64) (result string, err error) {
	// Mirrors GetPlainText's own "empty content" short-circuit exactly - a
	// page with no /Contents at all is not malformed, just blank.
	if page.V.IsNull() || page.V.Key("Contents").Kind() == pdf.Null {
		return "", nil
	}
	strm := page.V.Key("Contents")
	var enc pdf.TextEncoding = pdfNopEncoding{}

	// GetPlainText falls back to p.fontCache() when fonts is nil, but that
	// method is unexported - unreachable from this package. Unlike that
	// fallback, this isn't a behaviour gap in practice: extractPDF always
	// constructs a non-nil (if possibly empty) map before calling in here,
	// and a lookup on a nil map in Go reads as "not found" rather than
	// panicking, so an empty or nil fonts map both just mean every operator
	// falls back to pdfNopEncoding below, same as upstream's own nopEncoder.

	var textBuilder strings.Builder
	appendText := func(s string) {
		*budget += int64(len(s))
		if *budget > maxPDFExtractedTextBytes {
			panic(errPDFTextBudgetExceeded)
		}
		textBuilder.WriteString(s)
	}
	showText := func(s string) { appendText(s) }
	showEncodedText := func(s string) { appendText(decodePDFText(enc, s)) }

	pdf.Interpret(strm, func(stk *pdf.Stack, op string) {
		args := popPDFArgs(stk)

		switch op {
		default:
			// Easier debug - kept to match upstream GetPlainText's own
			// structure, not because this package ever wants the noise.
			return
		case "BT": // add a space between text objects
			showText("\n")
		case "T*": // move to start of next line
			showEncodedText("\n")
		case "Tf": // set text font and size
			if len(args) != 2 {
				panic("bad TL")
			}
			if font, ok := fonts[args[0].Name()]; ok {
				enc = font.Encoder()
			} else {
				enc = pdfNopEncoding{}
			}
		case "\"": // set spacing, move to next line, and show text
			if len(args) != 3 {
				panic("bad \" operator")
			}
			fallthrough
		case "'": // move to next line and show text
			if len(args) != 1 {
				panic("bad ' operator")
			}
			fallthrough
		case "Tj": // show text
			if len(args) != 1 {
				panic("bad Tj operator")
			}
			showEncodedText(args[0].RawString())
		case "TJ": // show text, allowing individual glyph positioning
			v := args[0]
			for i := 0; i < v.Len(); i++ {
				x := v.Index(i)
				if x.Kind() == pdf.String {
					showEncodedText(x.RawString())
				}
			}
		}
	})
	return textBuilder.String(), nil
}

// popPDFArgs pops every value currently on stk and returns them with the
// bottom of the stack at index 0 - the operand order PDF content-stream
// operators expect. Reimplemented here (github.com/ledongthuc/pdf has an
// identical unexported popArgs in page.go) only because extractPageText,
// needing to reimplement GetPlainText itself (see its own doc comment), has
// no access to the library's own private helper.
func popPDFArgs(stk *pdf.Stack) []pdf.Value {
	n := stk.Len()
	args := make([]pdf.Value, n)
	for i := n - 1; i >= 0; i-- {
		args[i] = stk.Pop()
	}
	return args
}

// decodePDFText mirrors github.com/ledongthuc/pdf's own (unexported)
// decodeText: decode raw through enc, then round-trip the result rune by
// rune through a strings.Builder - how that library sanitises whatever
// TextEncoding.Decode returns into valid UTF-8 (WriteRune substitutes
// U+FFFD for anything that isn't a valid rune) before the text reaches the
// rest of this file.
func decodePDFText(enc pdf.TextEncoding, raw string) string {
	var b strings.Builder
	for _, ch := range enc.Decode(raw) {
		b.WriteRune(ch)
	}
	return b.String()
}

// pdfNopEncoding mirrors github.com/ledongthuc/pdf's own (unexported)
// nopEncoder: the default TextEncoding used before any Tf operator has named
// an embedded font, and used again whenever an operator names a font
// extractPageText's own fonts map has no entry for - it passes raw bytes
// through unchanged.
type pdfNopEncoding struct{}

func (pdfNopEncoding) Decode(raw string) string { return raw }
