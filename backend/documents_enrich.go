package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// documentsDefaultOCRPageCap bounds how large a scanned PDF's OCR bill can
// grow before an explicit force_ocr (a reindex - ADR 0106 treats it as
// fresh consent) is required. 200 pages is roughly $0.40 at
// documentsOCRCostPerPageUSD, past which an accidentally-uploaded whole
// manual could otherwise run up real, unattended money.
const documentsDefaultOCRPageCap = 200

// documentsOCRCostPerPageUSD is only ever used to word the page-cap
// rejection message ("about $X for N pages") - it plays no part in the
// real cost, which always comes from the reply's own usage.cost (the whole
// point of B4 recording cost is never guessing at it).
const documentsOCRCostPerPageUSD = 0.002

// documentsEnrichMarkdownCapRunes caps how much of a text-layer PDF's or a
// text file's local markdown is sent to the enrich prompt - a whole manual
// would otherwise inflate every enrich call's token cost for no benefit to
// a title/summary/tags request.
const documentsEnrichMarkdownCapRunes = 24000

// documentsImageMaxBytes is the image-enrichment size cap named in the plan
// (ADR 0106): larger than this and the enrich stage fails rather than
// sending a multi-ten-megabyte data URL.
const documentsImageMaxBytes = 20 << 20 // 20 MB

// documentSuggestedTagsCap bounds how many of Mate's suggested tags
// SetSuggested records. Real captures (backend/testdata/openrouter_document_*.json)
// show a model asked for "up to 5 tags" returning 10 anyway; 8 is a
// deliberately generous cap given that, not a literal reading of the
// prompt's own number.
const documentSuggestedTagsCap = 8

// documentEnrichJSONInstruction is appended to every enrich prompt: the
// reply is parsed strictly (documentEnrichReply below), so the model must
// not wrap its JSON in any prose.
const documentEnrichJSONInstruction = `Reply with a single JSON object only - no prose before or after it, and no markdown fence other than the JSON itself.`

var documentEnrichPDFPrompt = `Read this document and reply with a JSON object: {"title": string, "summary": string, "tags": [string]}. Keep the title short (under 80 characters), the summary two or three sentences, and suggest up to 5 tags. ` + documentEnrichJSONInstruction

var documentEnrichImagePrompt = `Read this image and reply with a JSON object: {"text": string, "title": string, "summary": string, "tags": [string]}. "text" is everything readable in the image, transcribed as plain text (empty string if there is none). Keep the title short (under 80 characters), the summary two or three sentences, and suggest up to 5 tags. ` + documentEnrichJSONInstruction

// documentEnrichReply is the strict shape every enrich prompt asks for.
// Text is only ever populated by the image branch (the PDF branches get
// their text from message.Annotations instead - see ocrPagesFromAnnotations).
type documentEnrichReply struct {
	Text    string   `json:"text"`
	Title   string   `json:"title"`
	Summary string   `json:"summary"`
	Tags    []string `json:"tags"`
}

// documentEnrichKind distinguishes how an enrich-stage reply's text (if
// any) becomes OCR chunks, once the JSON {title,summary,tags} part has
// already been parsed identically for all three kinds.
type documentEnrichKind int

const (
	// enrichKindTextOnly is a text-layer PDF or a text-family document: no
	// plugin, no OCR fee, and the reply carries no text to chunk (the local
	// extraction's chunks, from the extract stage, are left exactly as
	// they are).
	enrichKindTextOnly documentEnrichKind = iota
	// enrichKindOCRFile is a scanned PDF sent through the file-parser
	// plugin: the OCR'd text comes back in message.Annotations, not the
	// JSON reply's own fields.
	enrichKindOCRFile
	// enrichKindImageOCR is an image sent as image_url: the OCR'd text
	// comes back in the JSON reply's own "text" field.
	enrichKindImageOCR
)

// documentEnrichJSONFenceRe matches a JSON reply wrapped in exactly one
// ```json ... ``` fence (some models add one despite being asked not to).
// Only this one specific wrapping is stripped - json.Unmarshal is then
// strict about everything else, per ADR 0106's "parsed strictly" call.
var documentEnrichJSONFenceRe = regexp.MustCompile("(?s)^```json\\s*\\n?(.*?)\\n?```$")

func stripDocumentEnrichJSONFence(s string) string {
	s = strings.TrimSpace(s)
	if m := documentEnrichJSONFenceRe.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1])
	}
	return s
}

// truncateRunes returns the first n runes of s, or s itself when it is
// already that short or shorter - used both to cap the markdown sent to
// the enrich prompt and to cap a failed reply's text in the error message.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// documentNeedsOCR recomputes the same "thin text layer" heuristic
// extractPDF itself uses (pdfNeedsOCRAvgCharsPerPage, documents_extract.go),
// from the document's own persisted markdown/page_count rather than a
// separate stored flag: a document resumed at stage=enrich after a restart
// has no in-memory extractedDocument left, but SetExtracted (the extract
// stage's own store write) already wrote everything this needs. Any
// non-PDF caller, or a PDF with no pages at all, is treated as needing OCR -
// there is no text layer to send instead.
func documentNeedsOCR(doc document) bool {
	if doc.PageCount <= 0 {
		return true
	}
	avg := float64(len(strings.TrimSpace(doc.Markdown))) / float64(doc.PageCount)
	return avg < pdfNeedsOCRAvgCharsPerPage
}

// withChunkSource returns a copy of chunks with Source overwritten - used
// to relabel chunkDocument's always-"local" output as "ocr" for B4's two
// OCR paths (chunkDocument itself stays untouched, since its committed B2
// tests pin every chunk it emits as source "local").
func withChunkSource(chunks []documentChunk, source string) []documentChunk {
	out := make([]documentChunk, len(chunks))
	for i, c := range chunks {
		c.Source = source
		out[i] = c
	}
	return out
}

// ocrMarkdownFromPages flattens pages the same way extractPDF joins its own
// pages into extractedDocument.Markdown (documents_extract.go): one blank
// line between pages. Used to give an OCR result the same markdown shape
// local extraction would have produced, whether pages came from the
// file-parser plugin's annotations (a scanned PDF) or a single synthetic
// page built from the JSON reply's own "text" field (an image).
func ocrMarkdownFromPages(pages []extractedPage) string {
	parts := make([]string, len(pages))
	for i, p := range pages {
		parts[i] = p.Text
	}
	return strings.Join(parts, "\n\n")
}

// writeOCRResult replaces doc's OCR chunks and writes their flattened text
// into documents.markdown, so markdown always holds the best text the
// system has (B4 review finding, backend/assistant_run.go's
// assistantAttachmentBlock reads doc.Markdown directly for its excerpt, and
// otherwise never saw anything OCR produced - documents.markdown is
// otherwise only ever written by the extract stage's SetExtracted). Page
// count is left exactly as extraction already found it (doc.PageCount):
// OCR reads the same already-paginated PDF, or, for an image, extraction's
// own page_count of 0 - either way it is not this call's to change.
func (idx *documentIndexer) writeOCRResult(doc document, pages []extractedPage) error {
	ocrChunks := withChunkSource(chunkDocument(extractedDocument{Pages: pages}, "application/pdf"), "ocr")
	if err := idx.store.ReplaceChunks(doc.ID, []string{"local", "ocr"}, ocrChunks); err != nil {
		return err
	}
	return idx.store.SetExtracted(doc.ID, ocrMarkdownFromPages(pages), doc.PageCount)
}

// ocrPagesFromAnnotations turns the file-parser plugin's annotation shape
// (backend/testdata/openrouter_document_pdf_2p.json) into per-page text:
// one entry per file (this codebase only ever sends one), whose Content is
// a `<file name="...">` sentinel, one part per page, then `</file>` - so
// page N is the Nth part strictly between the two sentinels. Returns nil
// for no annotations, or an annotation with fewer than 2 content parts
// (nothing between the sentinels to be a page).
func ocrPagesFromAnnotations(annotations []openRouterAnnotation) []extractedPage {
	if len(annotations) == 0 {
		return nil
	}
	parts := annotations[0].File.Content
	if len(parts) <= 2 {
		return nil
	}
	body := parts[1 : len(parts)-1]
	pages := make([]extractedPage, 0, len(body))
	for i, part := range body {
		pages = append(pages, extractedPage{Number: i + 1, Text: part.Text})
	}
	return pages
}

// documentEnrichReadinessProblem extends checkAssistantReadiness's own
// Problem with the one requirement that is document-only: assistant.
// document_model must not be blank. It is applied here and by the
// upload/reindex handlers (documents_handlers.go) - never by chat's own
// readiness (assistant/status, assistant/run), which must stay ready
// regardless of the document model, since the two are independent settings
// (ADR 0106). readiness.Problem, when already set, always wins: a chat-level
// problem (assistant off, no key, no chat model) is reported as-is, never
// masked by the document-model message.
func documentEnrichReadinessProblem(readiness assistantReadiness) string {
	if readiness.Problem != "" {
		return readiness.Problem
	}
	if strings.TrimSpace(readiness.DocumentModel) == "" {
		return "No document model is configured. Set one in Settings → Assistant."
	}
	return ""
}

// runEnrichStage sends doc's content to documentModel() and applies the
// reply, or fails the document. It returns a non-nil error only for a
// genuine infra failure (idx.readiness() itself erroring on a broken
// settings file or a secrets-store read error) - processOne propagates that
// so Run's documentsIndexerErrorBackoff applies instead of spinning
// immediately back onto the same still-pending document. Every other
// failure it can hit (an unready/misconfigured assistant, an unsupported
// MIME, an upstream error, an unparseable reply) is a normal, expected
// outcome recorded via SetFailed and reported as a nil error - a bug in the
// document or its configuration, not in the indexer.
func (idx *documentIndexer) runEnrichStage(ctx context.Context, doc document) error {
	readiness, apiKey, err := idx.readiness()
	if err != nil {
		return fmt.Errorf("documents indexer: enrich readiness check for %s: %w", doc.ID, err)
	}
	if problem := documentEnrichReadinessProblem(readiness); problem != "" {
		idx.failDoc(doc.ID, problem)
		return nil
	}

	if doc.MIME == "application/octet-stream" {
		// Nothing local or paid can be done with an unrecognised type - the
		// extract stage already left it indexed-worthy with no text.
		idx.finishIndexed(doc, "local", doc.Error)
		return nil
	}
	if doc.MIME == "image/heic" {
		idx.failDoc(doc.ID, "image/heic is not supported for reading; convert to JPEG")
		return nil
	}

	blocks, plugins, timeout, kind, err := idx.buildEnrichRequest(doc)
	if err != nil {
		idx.failDoc(doc.ID, err.Error())
		return nil
	}

	model, err := idx.documentModel()
	if err != nil {
		idx.failDoc(doc.ID, fmt.Sprintf("document model setting: %v", err))
		return nil
	}
	req := openRouterChatRequest{
		Model:    model,
		Messages: []openRouterMessage{openRouterUserMessage(blocks...)},
		Plugins:  plugins,
		Usage:    &openRouterUsageOption{Include: true},
	}

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resp, err := openRouterChatCompletionOnce(callCtx, idx.doer, apiKey, req)
	if err != nil {
		if ctx.Err() != nil {
			// The outer context is what ended, not idx.ocrTimeout/
			// idx.enrichTimeout - a shutdown mid-call, not a genuine
			// upstream failure. Mirrors runExtractStage's shutdown handling
			// (documents_indexer.go): leave the row pending/enrich
			// (untouched) for the next boot to retry, rather than mislabel
			// a shutdown as a failed document a restart can never fix.
			return nil
		}
		idx.failDoc(doc.ID, err.Error())
		return nil
	}
	if len(resp.Choices) == 0 {
		idx.failDoc(doc.ID, "openrouter returned no choices")
		return nil
	}
	message := resp.Choices[0].Message

	// Record the cost and the model as soon as the response comes back -
	// before anything that can still fail (the JSON parse, the OCR switch,
	// SetSuggested). This is a billed call: a ~$0.30 OCR run must not end as
	// a failed document with index_cost_usd still 0 just because something
	// after it went wrong (review finding - AddIndexCost used to run last).
	if err := idx.store.AddIndexCost(doc.ID, model, resp.Usage.Cost); err != nil {
		log.Printf("documents: indexer: add index cost %s: %v", doc.ID, err)
	}

	// enrichKindOCRFile's transcribed text lives in message.Annotations,
	// entirely independent of whether message.Content parses as the
	// expected JSON - so it (and the markdown copy - B4 review finding at
	// assistant_run.go:778) is written before ever touching the JSON reply,
	// and survives an unparseable reply below. enrichKindImageOCR has no
	// such independent source (its "text" field lives inside the same JSON
	// object as title/summary/tags), so that one is handled after a
	// successful parse instead, further down.
	if kind == enrichKindOCRFile {
		pages := ocrPagesFromAnnotations(message.Annotations)
		if len(pages) == 0 {
			idx.failDoc(doc.ID, "OCR returned no text")
			return nil
		}
		if err := idx.writeOCRResult(doc, pages); err != nil {
			idx.failDoc(doc.ID, err.Error())
			return nil
		}
	}

	var reply documentEnrichReply
	raw := string(message.Content)
	if err := json.Unmarshal([]byte(stripDocumentEnrichJSONFence(raw)), &reply); err != nil {
		idx.failDoc(doc.ID, "invalid JSON reply: "+truncateRunes(raw, 200))
		return nil
	}

	if kind == enrichKindImageOCR {
		page := extractedPage{Number: 1, Text: reply.Text}
		if err := idx.writeOCRResult(doc, []extractedPage{page}); err != nil {
			idx.failDoc(doc.ID, err.Error())
			return nil
		}
	}

	tags := reply.Tags
	if len(tags) > documentSuggestedTagsCap {
		tags = tags[:documentSuggestedTagsCap]
	}
	if err := idx.store.SetSuggested(doc.ID, reply.Title, reply.Summary, tags); err != nil {
		idx.failDoc(doc.ID, err.Error())
		return nil
	}

	idx.finishIndexed(doc, "mate", doc.Error)
	return nil
}

// buildEnrichRequest dispatches on doc.MIME (every value detectDocumentMIME
// can produce, minus octet-stream/heic which runEnrichStage handles before
// calling this) to the blocks/plugins/timeout/kind an enrich call needs.
func (idx *documentIndexer) buildEnrichRequest(doc document) ([]openRouterContentBlock, []openRouterPlugin, time.Duration, documentEnrichKind, error) {
	switch {
	// ForceOCR (MarkReindex - documents_store.go) exists precisely so a
	// reindex can retry OCR even when documentNeedsOCR already found the
	// local text layer adequate: it must select this branch on its own, not
	// merely lift buildScannedPDFRequest's page cap once already inside it.
	case doc.MIME == "application/pdf" && (doc.ForceOCR || documentNeedsOCR(doc)):
		return idx.buildScannedPDFRequest(doc)
	case doc.MIME == "application/pdf", textMIMETypes[doc.MIME]:
		return idx.buildTextRequest(doc), nil, idx.enrichTimeout, enrichKindTextOnly, nil
	case strings.HasPrefix(doc.MIME, "image/"):
		return idx.buildImageRequest(doc)
	default:
		return nil, nil, 0, 0, fmt.Errorf("documents: enrich: no enrichment path for MIME %q", doc.MIME)
	}
}

// buildScannedPDFRequest is the OCR branch: a page-count cap (unless
// force_ocr), then the whole file as a base64 data URL behind the
// file-parser/mistral-ocr plugin.
func (idx *documentIndexer) buildScannedPDFRequest(doc document) ([]openRouterContentBlock, []openRouterPlugin, time.Duration, documentEnrichKind, error) {
	pageCap := idx.ocrPageCap
	if pageCap <= 0 {
		pageCap = documentsDefaultOCRPageCap
	}
	if doc.PageCount > pageCap && !doc.ForceOCR {
		cost := float64(doc.PageCount) * documentsOCRCostPerPageUSD
		return nil, nil, 0, 0, fmt.Errorf(
			"scanned PDF has %d pages, over the %d page OCR cap (about $%.2f at $%.3f/page); reindex with force_ocr to proceed anyway",
			doc.PageCount, pageCap, cost, documentsOCRCostPerPageUSD,
		)
	}

	raw, err := os.ReadFile(filepath.Join(idx.dir, doc.SHA256))
	if err != nil {
		return nil, nil, 0, 0, fmt.Errorf("read document file for OCR: %w", err)
	}
	dataURL := "data:application/pdf;base64," + base64.StdEncoding.EncodeToString(raw)

	blocks := []openRouterContentBlock{
		{Type: "text", Text: documentEnrichPDFPrompt},
		{Type: "file", File: &openRouterFileBlock{Filename: doc.Filename, FileData: dataURL}},
	}
	plugins := []openRouterPlugin{{ID: "file-parser", PDF: &openRouterPluginPDF{Engine: "mistral-ocr"}}}
	return blocks, plugins, idx.ocrTimeout, enrichKindOCRFile, nil
}

// buildTextRequest is the no-OCR-fee branch: a text-layer PDF or a
// text-family document, sending the first documentsEnrichMarkdownCapRunes
// characters of the already-extracted markdown.
func (idx *documentIndexer) buildTextRequest(doc document) []openRouterContentBlock {
	text := truncateRunes(doc.Markdown, documentsEnrichMarkdownCapRunes)
	return []openRouterContentBlock{
		{Type: "text", Text: documentEnrichPDFPrompt + "\n\n" + text},
	}
}

// buildImageRequest sends the whole image as a base64 data URL, capped at
// documentsImageMaxBytes.
func (idx *documentIndexer) buildImageRequest(doc document) ([]openRouterContentBlock, []openRouterPlugin, time.Duration, documentEnrichKind, error) {
	if doc.SizeBytes > documentsImageMaxBytes {
		return nil, nil, 0, 0, fmt.Errorf("image is %d bytes, over the %d byte OCR cap", doc.SizeBytes, documentsImageMaxBytes)
	}

	raw, err := os.ReadFile(filepath.Join(idx.dir, doc.SHA256))
	if err != nil {
		return nil, nil, 0, 0, fmt.Errorf("read document file for OCR: %w", err)
	}
	dataURL := "data:" + doc.MIME + ";base64," + base64.StdEncoding.EncodeToString(raw)

	blocks := []openRouterContentBlock{
		{Type: "text", Text: documentEnrichImagePrompt},
		{Type: "image_url", ImageURL: &openRouterImageURLBlock{URL: dataURL}},
	}
	return blocks, nil, idx.enrichTimeout, enrichKindImageOCR, nil
}
