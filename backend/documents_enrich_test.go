package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
)

// enrichTestRequestBody decodes the single request a fakeOpenRouterDoer
// recorded into the wire shape B4's enrich stage builds: a model, one user
// message whose content is an array of blocks, and optional plugins. Kept
// loose (map[string]any for the content parts) rather than reusing
// openRouterContentBlock, so the assertions below are checking the actual
// bytes on the wire, not merely that Go's own marshalling round-trips.
type enrichTestRequestBody struct {
	Model    string `json:"model"`
	Stream   bool   `json:"stream"`
	Messages []struct {
		Role    string           `json:"role"`
		Content []map[string]any `json:"content"`
	} `json:"messages"`
	Plugins []map[string]any `json:"plugins"`
	Usage   struct {
		Include bool `json:"include"`
	} `json:"usage"`
}

func decodeEnrichRequest(t *testing.T, raw []byte) enrichTestRequestBody {
	t.Helper()
	var body enrichTestRequestBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode request body: %v (%s)", err, raw)
	}
	return body
}

// ── scanned PDF: file part + mistral-ocr plugin, OCR chunks replace local ──

func TestDocumentEnrich_ScannedPDFSendsFilePartAndOCRPluginAndReplacesChunks(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	doc := seedTestDocumentFile(t, store, dir, testdataPath("scanned_two_page.pdf"), "scan.pdf", "application/pdf", true)

	fixture, err := os.ReadFile("testdata/openrouter_document_pdf_2p.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, string(fixture))}}
	idx := newTestDocumentIndexer(t, store, dir)
	idx.doer = doer

	if _, err := idx.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	if len(doer.requests) != 1 {
		t.Fatalf("expected exactly one OpenRouter call, got %d", len(doer.requests))
	}
	req := decodeEnrichRequest(t, doer.bodies[0])
	if req.Model != "google/gemini-2.5-flash" {
		t.Fatalf("expected the configured document_model, got %q", req.Model)
	}
	if !req.Usage.Include {
		t.Fatalf("expected usage.include=true so cost comes back")
	}
	if len(req.Plugins) != 1 || req.Plugins[0]["id"] != "file-parser" {
		t.Fatalf("expected the file-parser plugin, got %+v", req.Plugins)
	}
	pdfOpt, _ := req.Plugins[0]["pdf"].(map[string]any)
	if pdfOpt == nil || pdfOpt["engine"] != "mistral-ocr" {
		t.Fatalf("expected plugin.pdf.engine=mistral-ocr, got %+v", req.Plugins[0])
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != "user" {
		t.Fatalf("expected one user message, got %+v", req.Messages)
	}
	var sawFile bool
	for _, part := range req.Messages[0].Content {
		if part["type"] == "file" {
			sawFile = true
			file, _ := part["file"].(map[string]any)
			if file == nil || file["filename"] != "scan.pdf" {
				t.Fatalf("unexpected file block: %+v", part)
			}
			fd, _ := file["file_data"].(string)
			if !strings.HasPrefix(fd, "data:application/pdf;base64,") {
				t.Fatalf("expected a base64 data URL, got %q", fd)
			}
			if _, hasText := part["text"]; hasText {
				t.Fatalf("expected no text key on the file block, got %+v", part)
			}
		}
	}
	if !sawFile {
		t.Fatalf("expected a file content block, got %+v", req.Messages[0].Content)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "indexed" || got.IndexedWith != "mate" {
		t.Fatalf("expected indexed/mate, got %+v", got)
	}
	if got.Title == "" || got.Summary == "" {
		t.Fatalf("expected a suggested title and summary, got %+v", got)
	}
	if len(got.SuggestedTags) == 0 || len(got.SuggestedTags) > 8 {
		t.Fatalf("expected 1-8 suggested tags, got %d: %+v", len(got.SuggestedTags), got.SuggestedTags)
	}
	if got.IndexModel != "google/gemini-2.5-flash" {
		t.Fatalf("unexpected index_model: %q", got.IndexModel)
	}
	const wantCost = 0.0046432
	if diff := got.IndexCostUSD - wantCost; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("expected index_cost_usd %v, got %v", wantCost, got.IndexCostUSD)
	}

	// OCR chunks replace local ones - page 1 has "impeller", page 2 has
	// "pump seal" (backend/testdata/openrouter_document_pdf_2p.json). This
	// is checked directly against the per-page OCR chunk text rather than
	// through ranked FTS Search: Mate's own suggested tags for this fixture
	// include "Johnson impeller kit" (a real captured reply, not something
	// this test controls), so the meta chunk also matches "impeller" and,
	// being much shorter than the OCR chunk, out-ranks it under bm25's
	// length normalisation - a realistic ranking interaction, not a bug,
	// but the wrong thing for this test to assert on. "pump seal" has no
	// such collision and is confirmed via Search below as well.
	chunks, err := store.ChunksFrom(doc.ID, 1)
	if err != nil {
		t.Fatalf("ChunksFrom: %v", err)
	}
	var page1Text, page2Text string
	for _, c := range chunks {
		if c.Source == "local" {
			t.Fatalf("expected no local-sourced chunks left after OCR replaced them, got %+v", c)
		}
		switch c.PageStart {
		case 1:
			page1Text += c.Text
		case 2:
			page2Text += c.Text
		}
	}
	if !strings.Contains(page1Text, "impeller") {
		t.Fatalf("expected page 1's OCR text to mention impeller, got %q", page1Text)
	}
	if !strings.Contains(page2Text, "pump seal") {
		t.Fatalf("expected page 2's OCR text to mention pump seal, got %q", page2Text)
	}

	q2, _ := ftsMatchQuery("seal")
	r2, err := store.Search(q2, nil, false, "", 10, 0)
	if err != nil {
		t.Fatalf("Search seal: %v", err)
	}
	if len(r2) != 1 || r2[0].PageStart != 2 {
		t.Fatalf("expected pump seal to hit page 2, got %+v", r2)
	}
}

// TestDocumentEnrich_ScannedPDFWritesOCRTextIntoMarkdown pins the review
// finding at assistant_run.go:778: documents.markdown was only ever written
// by the extract stage (SetExtracted) - OCR output went into ocr chunks and
// never back into markdown, so anything reading doc.Markdown directly (the
// assistant's attachment excerpt) never saw it. scanned_two_page.pdf's own
// text layer is thin enough to need OCR in the first place (empty, in this
// fixture's case - see documents_extract_test.go), so markdown holding OCR
// text after enrichment is the only way it ends up with anything useful in
// it at all. page_count must stay exactly as extraction found it (2): OCR
// reads the same already-paginated PDF, it doesn't discover new pages.
func TestDocumentEnrich_ScannedPDFWritesOCRTextIntoMarkdown(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	doc := seedTestDocumentFile(t, store, dir, testdataPath("scanned_two_page.pdf"), "scan.pdf", "application/pdf", true)

	fixture, err := os.ReadFile("testdata/openrouter_document_pdf_2p.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, string(fixture))}}
	idx := newTestDocumentIndexer(t, store, dir)
	idx.doer = doer

	if _, err := idx.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(got.Markdown, "impeller") {
		t.Fatalf("expected page 1's OCR text (impeller) in markdown, got %q", got.Markdown)
	}
	if !strings.Contains(got.Markdown, "pump seal") {
		t.Fatalf("expected page 2's OCR text (pump seal) in markdown, got %q", got.Markdown)
	}
	if got.PageCount != 2 {
		t.Fatalf("expected page_count to stay at extraction's own 2, got %d", got.PageCount)
	}
}

// TestDocumentEnrich_ImageWritesOCRTextIntoMarkdown is the image half of
// the same finding: the image branch's OCR text comes from the JSON
// reply's own "text" field rather than message.Annotations, but it must
// land in documents.markdown exactly the same way.
func TestDocumentEnrich_ImageWritesOCRTextIntoMarkdown(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	doc := seedTestDocumentFile(t, store, dir, testdataPath("receipt.png"), "receipt.png", "image/png", true)

	fixture, err := os.ReadFile("testdata/openrouter_document_image.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, string(fixture))}}
	idx := newTestDocumentIndexer(t, store, dir)
	idx.doer = doer

	if _, err := idx.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(got.Markdown, "impeller kit") {
		t.Fatalf("expected the image's transcribed text in markdown, got %q", got.Markdown)
	}
}

// TestDocumentEnrich_ScannedPDFAttachmentExcerptShowsOCRTextNotTextLayer is
// the end-to-end version of the same finding: assistantAttachmentBlock
// (assistant_run.go) renders its "Excerpt:" line straight from doc.Markdown.
// Before this fix, scanned_two_page.pdf's own text layer is thin enough to
// trim to nothing, so the preamble carried no excerpt at all despite Mate
// having read two pages of real text - the same "no useful excerpt"
// symptom the review finding describes for an attached photo, just via a
// different route (an empty text layer rather than no local text stage at
// all). After the fix, markdown holds what OCR actually read.
func TestDocumentEnrich_ScannedPDFAttachmentExcerptShowsOCRTextNotTextLayer(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	doc := seedTestDocumentFile(t, store, dir, testdataPath("scanned_two_page.pdf"), "scan.pdf", "application/pdf", true)

	fixture, err := os.ReadFile("testdata/openrouter_document_pdf_2p.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, string(fixture))}}
	idx := newTestDocumentIndexer(t, store, dir)
	idx.doer = doer

	if _, err := idx.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	block := assistantAttachmentBlock(got, true)
	if !strings.Contains(block, "Excerpt:") {
		t.Fatalf("expected an Excerpt: line now that markdown holds OCR text, got %q", block)
	}
	if !strings.Contains(block, "impeller") {
		t.Fatalf("expected the excerpt to carry the OCR'd text, got %q", block)
	}
}

// TestDocumentEnrich_UnparseableReplyAfterOCRStillRecordsCostAndKeepsOCRChunks
// pins the review finding at documents_enrich.go:291: AddIndexCost used to
// run after the JSON parse, the OCR switch and SetSuggested, so any failure
// after the OpenRouter call returns - including an unparseable reply -
// recorded no cost at all and threw away the OCR text already sitting in
// message.Annotations, entirely independent of whether message.Content
// happens to parse as the expected {title,summary,tags} JSON. A billed,
// successful OCR call must not end as a failed document with index_cost_usd
// 0 and no searchable text just because the *other* half of the reply was
// malformed.
func TestDocumentEnrich_UnparseableReplyAfterOCRStillRecordsCostAndKeepsOCRChunks(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	doc := seedTestDocumentFile(t, store, dir, testdataPath("scanned_two_page.pdf"), "scan.pdf", "application/pdf", true)

	raw, err := os.ReadFile("testdata/openrouter_document_pdf_2p.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixture map[string]any
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	choices, _ := fixture["choices"].([]any)
	msg, _ := choices[0].(map[string]any)["message"].(map[string]any)
	// Corrupt only the JSON reply - the OCR annotations and usage.cost stay
	// exactly as the real capture has them.
	msg["content"] = "Sorry, I can't help with that request."
	body, err := json.Marshal(fixture)
	if err != nil {
		t.Fatalf("marshal corrupted fixture: %v", err)
	}

	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, string(body))}}
	idx := newTestDocumentIndexer(t, store, dir)
	idx.doer = doer

	if _, err := idx.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "failed" {
		t.Fatalf("expected status failed, got %q", got.Status)
	}
	if !strings.Contains(got.Error, "invalid JSON reply") {
		t.Fatalf("expected the parse error recorded, got %q", got.Error)
	}

	const wantCost = 0.0046432 // testdata/openrouter_document_pdf_2p.json's usage.cost
	if diff := got.IndexCostUSD - wantCost; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("expected index_cost_usd %v (the fixture's usage.cost) to survive the parse failure, got %v", wantCost, got.IndexCostUSD)
	}
	if got.IndexModel != "google/gemini-2.5-flash" {
		t.Fatalf("expected index_model to survive the parse failure, got %q", got.IndexModel)
	}

	q, _ := ftsMatchQuery("seal")
	results, err := store.Search(q, nil, false, "", 10, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected the OCR text to remain searchable despite the parse failure, got %d hits: %+v", len(results), results)
	}
}

func TestDocumentEnrich_ScannedPDFNoAnnotationsFails(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	doc := seedTestDocumentFile(t, store, dir, testdataPath("scanned_two_page.pdf"), "scan.pdf", "application/pdf", true)

	body := `{"id":"gen-1","model":"m","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"{\"title\":\"x\",\"summary\":\"y\",\"tags\":[]}"}}],"usage":{"cost":0.001}}`
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, body)}}
	idx := newTestDocumentIndexer(t, store, dir)
	idx.doer = doer

	if _, err := idx.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "failed" || got.Error != "OCR returned no text" {
		t.Fatalf(`expected failed "OCR returned no text", got %+v`, got)
	}
}

// ── text-layer PDF: text part, no plugin, no OCR chunks ────────────────

func TestDocumentEnrich_TextLayerPDFSendsTextPartNoPluginNoOCRChunks(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	doc := seedTestDocumentFile(t, store, dir, testdataPath("two_page.pdf"), "manual.pdf", "application/pdf", true)

	// Reused even though this particular fixture also carries annotations -
	// the text-layer branch never looks at message.Annotations at all, only
	// at the JSON-fenced {title,summary,tags} in message.content.
	fixture, err := os.ReadFile("testdata/openrouter_document_pdf.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, string(fixture))}}
	idx := newTestDocumentIndexer(t, store, dir)
	idx.doer = doer

	if _, err := idx.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	if len(doer.requests) != 1 {
		t.Fatalf("expected exactly one OpenRouter call, got %d", len(doer.requests))
	}
	req := decodeEnrichRequest(t, doer.bodies[0])
	if len(req.Plugins) != 0 {
		t.Fatalf("expected no plugins for a text-layer PDF, got %+v", req.Plugins)
	}
	if len(req.Messages) != 1 || len(req.Messages[0].Content) != 1 {
		t.Fatalf("expected a single text content block, got %+v", req.Messages)
	}
	part := req.Messages[0].Content[0]
	if part["type"] != "text" {
		t.Fatalf("expected a text block, got %+v", part)
	}
	text, _ := part["text"].(string)
	if !strings.Contains(text, "IMPELLERTOKEN") {
		t.Fatalf("expected the local markdown text in the prompt, got %q", text)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "indexed" || got.IndexedWith != "mate" {
		t.Fatalf("expected indexed/mate, got %+v", got)
	}

	chunks, err := store.ChunksFrom(doc.ID, 1)
	if err != nil {
		t.Fatalf("ChunksFrom: %v", err)
	}
	for _, c := range chunks {
		if c.Source == "ocr" {
			t.Fatalf("expected no ocr-sourced chunks for a text-layer PDF, got %+v", c)
		}
	}
	// The local chunks from extraction must still be there and searchable.
	q, _ := ftsMatchQuery("THERMOSTATTOKEN")
	results, err := store.Search(q, nil, false, "", 10, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected the local chunk to remain searchable, got %d", len(results))
	}
}

// ── image: image_url part, OCR chunks from the reply's text field ─────────

func TestDocumentEnrich_ImageSendsImageURLPartAndOCRChunksFromReplyText(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	doc := seedTestDocumentFile(t, store, dir, testdataPath("receipt.png"), "receipt.png", "image/png", true)

	fixture, err := os.ReadFile("testdata/openrouter_document_image.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(200, string(fixture))}}
	idx := newTestDocumentIndexer(t, store, dir)
	idx.doer = doer

	if _, err := idx.processOne(context.Background()); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	req := decodeEnrichRequest(t, doer.bodies[0])
	if len(req.Plugins) != 0 {
		t.Fatalf("expected no plugins for an image, got %+v", req.Plugins)
	}
	var sawImage bool
	for _, part := range req.Messages[0].Content {
		if part["type"] == "image_url" {
			sawImage = true
			imgURL, _ := part["image_url"].(map[string]any)
			url, _ := imgURL["url"].(string)
			if !strings.HasPrefix(url, "data:image/png;base64,") {
				t.Fatalf("expected a base64 png data URL, got %q", url)
			}
			if _, hasText := part["text"]; hasText {
				t.Fatalf("expected no text key on the image_url block, got %+v", part)
			}
		}
	}
	if !sawImage {
		t.Fatalf("expected an image_url content block, got %+v", req.Messages[0].Content)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "indexed" || got.IndexedWith != "mate" {
		t.Fatalf("expected indexed/mate, got %+v", got)
	}
	if len(got.SuggestedTags) == 0 || len(got.SuggestedTags) > 8 {
		t.Fatalf("expected 1-8 suggested tags (fixture has 10, capped), got %d", len(got.SuggestedTags))
	}

	// Checked directly against the OCR chunk (same reasoning as the scanned
	// PDF test above: this fixture's own suggested tags also mention
	// "impeller kit", so ranked Search can return the shorter meta chunk
	// ahead of the OCR chunk under bm25's length normalisation).
	chunks, err := store.ChunksFrom(doc.ID, 1)
	if err != nil {
		t.Fatalf("ChunksFrom: %v", err)
	}
	var ocrText string
	var sawOCRPage1 bool
	for _, c := range chunks {
		if c.Source == "ocr" {
			ocrText += c.Text
			if c.PageStart == 1 {
				sawOCRPage1 = true
			}
		}
	}
	if !sawOCRPage1 {
		t.Fatalf("expected an ocr-sourced chunk on page 1, got %+v", chunks)
	}
	if !strings.Contains(ocrText, "impeller") {
		t.Fatalf("expected the image's OCR'd text to be recorded, got %q", ocrText)
	}
}

// ── shutdown mid-call leaves the row pending, not failed ──────────────────

// TestDocumentEnrich_ShutdownMidCallLeavesDocumentPendingNotFailed pins the
// review finding: the OpenRouter call's context is derived from the
// server's own stream context (ctx, here), so a shutdown while the call is
// in flight makes openRouterChatCompletionOnce return a "context canceled"
// error indistinguishable, by text alone, from a genuine upstream failure.
// Before this fix that error went straight to failDoc, so the document
// never got retried on the next boot. runExtractStage already tells a
// shutdown apart from its own timeout by checking the OUTER ctx, not the
// derived call context - this mirrors that: ctxErrDoer stands in for a real
// *http.Client observing an already-cancelled request context.
func TestDocumentEnrich_ShutdownMidCallLeavesDocumentPendingNotFailed(t *testing.T) {
	store := withTestDocumentStore(t)
	dir := documentsDirPath()
	raw := []byte("some notes, content is irrelevant - the call never reaches a response")
	sha := writeTestDocumentBytes(t, dir, raw)
	doc, err := store.Insert(document{SHA256: sha, Filename: "notes.txt", MIME: "text/plain", SizeBytes: int64(len(raw)), Enrich: true})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := store.SetStage(doc.ID, "enrich"); err != nil {
		t.Fatalf("SetStage: %v", err)
	}

	idx := newTestDocumentIndexer(t, store, dir)
	idx.doer = ctxErrDoer{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // simulate the server shutting down before/during the call

	if err := idx.runEnrichStage(ctx, doc); err != nil {
		t.Fatalf("runEnrichStage: %v", err)
	}

	got, err := store.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "pending" {
		t.Fatalf("expected the document to remain pending after a shutdown mid-call, got status %q (error %q)", got.Status, got.Error)
	}
	if got.Stage != "enrich" {
		t.Fatalf("expected the document to remain at stage enrich for the next boot to retry, got %q", got.Stage)
	}
}

// ── document-only readiness: blank document model, without touching chat ──

// TestDocumentEnrichReadinessProblem_BlankDocumentModelReportsProblem is the
// document-side half of the follow-up correction: the blank-document-model
// check moved off checkAssistantReadiness's own Problem (which chat status/
// run gate on) and onto this helper instead, applied only by the document
// upload/reindex handlers (documents_handlers.go) and runEnrichStage below.
// Everything else about readiness (Enabled/Configured/Model) is otherwise
// fine here, isolating the document_model check itself.
func TestDocumentEnrichReadinessProblem_BlankDocumentModelReportsProblem(t *testing.T) {
	readiness := assistantReadiness{Enabled: true, Configured: true, Model: "openai/gpt-4o", DocumentModel: ""}

	problem := documentEnrichReadinessProblem(readiness)
	if !strings.Contains(strings.ToLower(problem), "document model") {
		t.Fatalf("expected the problem to name the document model, got %q", problem)
	}
}

// TestDocumentEnrichReadinessProblem_ChatProblemTakesPrecedence proves the
// helper never masks a genuine chat-level problem (assistant off, no key,
// no chat model) behind the document-model message, and reports ready when
// both the chat and document models are configured.
func TestDocumentEnrichReadinessProblem_ChatProblemTakesPrecedence(t *testing.T) {
	readiness := assistantReadiness{Problem: "The assistant is switched off. Enable it in Settings → Assistant."}
	if problem := documentEnrichReadinessProblem(readiness); problem != readiness.Problem {
		t.Fatalf("expected the chat problem to pass through unchanged, got %q", problem)
	}

	ready := assistantReadiness{Enabled: true, Configured: true, Model: "openai/gpt-4o", DocumentModel: "openai/gpt-4o-mini"}
	if problem := documentEnrichReadinessProblem(ready); problem != "" {
		t.Fatalf("expected no problem once both models are configured, got %q", problem)
	}
}
