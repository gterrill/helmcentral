package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// withTestDocumentStore points globalDocumentStore at a fresh
// t.TempDir()-backed store, and DOCUMENTS_DIR at a second, empty
// t.TempDir(), for the duration of the test - the same package-level-var
// swap idiom as withTestAssistantStore (assistant_handlers_test.go), plus
// an env-var override for its own on-disk directory.
func withTestDocumentStore(t *testing.T) *documentStore {
	t.Helper()
	store := newTestDocumentStore(t)
	t.Setenv("DOCUMENTS_DIR", t.TempDir())
	prev := globalDocumentStore
	globalDocumentStore = store
	t.Cleanup(func() { globalDocumentStore = prev })

	// ADR 0120: createNoteHandler now calls checkAssistantReadiness on
	// every note create (documentEnrichFlag, documents_enrich.go), which
	// dereferences globalSecretsStore - nil by default outside a test that
	// installs its own (withTestSecretsStore). That used to matter only to
	// the handful of tests that cared about Mate readiness directly; now
	// every caller of mustCreateTestNote across the suite (checklist runs,
	// Mate's pinned-notes prompt, this file's own note tests) goes through
	// the same path. Rather than sprinkling withTestSecretsStore across
	// every one of those unrelated test files, seed a real-but-empty store
	// here whenever the caller hasn't already installed its own - Get on an
	// empty store safely reports "not configured" (readiness.Configured =
	// false), the exact same "Mate isn't ready" outcome the old hardcoded
	// enrich=false gave every one of these tests, so nothing about their
	// behaviour changes; only the nil-pointer panic that would otherwise
	// follow does. A test that wants Mate actually ready still calls
	// withTestSecretsStore(t) itself, same as always - its own prev/cleanup
	// composes fine on top of this one (LIFO cleanup order).
	if globalSecretsStore == nil {
		prevSecrets := globalSecretsStore
		globalSecretsStore = newTestSecretsStore(t)
		t.Cleanup(func() { globalSecretsStore = prevSecrets })
	}

	return store
}

// newDocumentEchoContext builds an echo.Context/ResponseRecorder pair the
// same way assistant_handlers_test.go's newAssistantEchoContext does: a
// direct httptest request (target may carry a query string), a JSON
// Content-Type when body is non-empty, and an "id" path param set when id
// is non-empty. Handlers here are called directly rather than through a
// registered router, matching every other *_handlers_test.go file in this
// package.
func newDocumentEchoContext(method, target, body, id string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if id != "" {
		c.SetParamNames("id")
		c.SetParamValues(id)
	}
	return c, rec
}

// documentUploadField is one part of a multipart upload built by
// newDocumentUploadContext: a plain form field when filename is "", a file
// part otherwise.
type documentUploadField struct {
	name     string
	filename string
	content  []byte
}

// newDocumentUploadContext builds a POST /api/documents multipart request,
// a CreateFormFile-based builder extended with plain fields (title, tags,
// folder_id) uploadDocumentHandler reads via MultipartReader rather than
// FormFile.
func newDocumentUploadContext(t *testing.T, fields []documentUploadField) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for _, f := range fields {
		var w io.Writer
		var err error
		if f.filename != "" {
			w, err = writer.CreateFormFile(f.name, f.filename)
		} else {
			w, err = writer.CreateFormField(f.name)
		}
		if err != nil {
			t.Fatalf("create part %q: %v", f.name, err)
		}
		if _, err := w.Write(f.content); err != nil {
			t.Fatalf("write part %q: %v", f.name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/documents", body)
	req.Header.Set(echo.HeaderContentType, writer.FormDataContentType())
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

// documentsDirEntries lists documentsDirPath()'s current contents (test
// helper for "no temp files left behind" assertions).
func documentsDirEntries(t *testing.T) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(documentsDirPath())
	if err != nil {
		t.Fatalf("ReadDir(documentsDirPath()): %v", err)
	}
	return entries
}

// insertTestDocumentWithFile inserts a document row AND writes its bytes to
// disk at dir/<sha> - most handler tests below need both, since
// documentContentHandler/deleteDocumentHandler read the database row to
// find the file, not the other way around.
func insertTestDocumentWithFile(t *testing.T, store *documentStore, sha, filename, mimeType string, content []byte) document {
	t.Helper()
	if err := os.WriteFile(filepath.Join(documentsDirPath(), sha), content, 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
	doc, err := store.Insert(document{SHA256: sha, Filename: filename, MIME: mimeType, SizeBytes: int64(len(content))})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	return doc
}

// ── POST /api/documents (upload) ────────────────────────────────────────

func TestUploadDocumentHandler_CreatesThenDuplicateReturns200WithOneFileOnDisk(t *testing.T) {
	withTestDocumentStore(t)
	withTestSecretsStore(t)
	content, err := os.ReadFile(testdataPath("two_page.pdf"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	c, rec := newDocumentUploadContext(t, []documentUploadField{
		{name: "file", filename: "manual.pdf", content: content},
	})
	if err := uploadDocumentHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var first struct {
		Document  documentJSON `json:"document"`
		Duplicate bool         `json:"duplicate"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if first.Duplicate {
		t.Fatalf("expected duplicate=false on the first upload")
	}
	if first.Document.MIME != "application/pdf" {
		t.Fatalf("expected application/pdf, got %q", first.Document.MIME)
	}
	if first.Document.Status != "pending" || first.Document.Stage != "extract" {
		t.Fatalf("expected pending/extract, got %q/%q", first.Document.Status, first.Document.Stage)
	}

	if entries := documentsDirEntries(t); len(entries) != 1 {
		t.Fatalf("expected exactly 1 file on disk after the first upload, got %d: %v", len(entries), entries)
	}

	c2, rec2 := newDocumentUploadContext(t, []documentUploadField{
		{name: "file", filename: "manual-again.pdf", content: content},
	})
	if err := uploadDocumentHandler(c2); err != nil {
		t.Fatalf("handler returned error (duplicate): %v", err)
	}
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 for a duplicate upload, got %d: %s", rec2.Code, rec2.Body.String())
	}

	var second struct {
		Document  documentJSON `json:"document"`
		Duplicate bool         `json:"duplicate"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &second); err != nil {
		t.Fatalf("unmarshal (duplicate): %v", err)
	}
	if !second.Duplicate {
		t.Fatalf("expected duplicate=true on the second upload")
	}
	if second.Document.ID != first.Document.ID {
		t.Fatalf("expected the duplicate to report the same document id, got %q vs %q", second.Document.ID, first.Document.ID)
	}

	if entries := documentsDirEntries(t); len(entries) != 1 {
		t.Fatalf("expected still exactly 1 file on disk after the duplicate upload, got %d: %v", len(entries), entries)
	}
}

func TestUploadDocumentHandler_OverCapReturns413AndLeavesNoTempFile(t *testing.T) {
	withTestDocumentStore(t)
	withTestSecretsStore(t)

	prevCap := documentMaxUploadBytes
	documentMaxUploadBytes = 200 // enough for multipart framing, not for the body below
	t.Cleanup(func() { documentMaxUploadBytes = prevCap })

	content := bytes.Repeat([]byte("x"), 5000)
	c, rec := newDocumentUploadContext(t, []documentUploadField{
		{name: "file", filename: "big.bin", content: content},
	})
	if err := uploadDocumentHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "200") {
		t.Fatalf("expected the 413 body to name the byte limit, got %s", rec.Body.String())
	}

	if entries := documentsDirEntries(t); len(entries) != 0 {
		t.Fatalf("expected no temp files left after a 413, got %v", entries)
	}
}

func TestUploadDocumentHandler_MissingFileReturns400AndLeavesNoTempFile(t *testing.T) {
	withTestDocumentStore(t)
	withTestSecretsStore(t)

	c, rec := newDocumentUploadContext(t, []documentUploadField{
		{name: "title", content: []byte("Some Title")},
	})
	if err := uploadDocumentHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if entries := documentsDirEntries(t); len(entries) != 0 {
		t.Fatalf("expected no temp files left after a 400, got %v", entries)
	}
}

func TestUploadDocumentHandler_EmptyFileReturns400AndLeavesNoTempFile(t *testing.T) {
	withTestDocumentStore(t)
	withTestSecretsStore(t)

	c, rec := newDocumentUploadContext(t, []documentUploadField{
		{name: "file", filename: "empty.txt", content: []byte{}},
	})
	if err := uploadDocumentHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an empty file, got %d: %s", rec.Code, rec.Body.String())
	}
	if entries := documentsDirEntries(t); len(entries) != 0 {
		t.Fatalf("expected no temp files left after an empty-file 400, got %v", entries)
	}
}

func TestUploadDocumentHandler_ParsesTrimmedTitleTagsAndFolderID(t *testing.T) {
	withTestDocumentStore(t)
	withTestSecretsStore(t)

	folder, err := globalDocumentStore.CreateFolder("Manuals", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	c, rec := newDocumentUploadContext(t, []documentUploadField{
		{name: "file", filename: "notes.txt", content: []byte("hello world")},
		{name: "title", content: []byte("  Engine Notes  ")},
		{name: "tags", content: []byte("engine, diesel ,, ")},
		{name: "folder_id", content: []byte(folder.ID)},
	})
	if err := uploadDocumentHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Document documentJSON `json:"document"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Document.Title != "Engine Notes" {
		t.Fatalf("expected the title trimmed to %q, got %q", "Engine Notes", resp.Document.Title)
	}
	if resp.Document.FolderID == nil || *resp.Document.FolderID != folder.ID {
		t.Fatalf("expected folder_id=%q, got %v", folder.ID, resp.Document.FolderID)
	}
	got := map[string]bool{}
	for _, tag := range resp.Document.Tags {
		got[tag.Tag] = true
		if tag.Source != "operator" {
			t.Fatalf("expected an upload's tags to be operator-sourced, got %+v", tag)
		}
	}
	if !got["engine"] || !got["diesel"] {
		t.Fatalf("expected tags engine and diesel (empty entries dropped), got %+v", resp.Document.Tags)
	}
	if len(resp.Document.Tags) != 2 {
		t.Fatalf("expected exactly 2 tags (blank entries dropped), got %+v", resp.Document.Tags)
	}
}

func TestUploadDocumentHandler_UnknownFolderIDReturns400AndRemovesFile(t *testing.T) {
	withTestDocumentStore(t)
	withTestSecretsStore(t)

	c, rec := newDocumentUploadContext(t, []documentUploadField{
		{name: "file", filename: "notes.txt", content: []byte("hello")},
		{name: "folder_id", content: []byte("does-not-exist")},
	})
	if err := uploadDocumentHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unknown folder_id, got %d: %s", rec.Code, rec.Body.String())
	}
	if entries := documentsDirEntries(t); len(entries) != 0 {
		t.Fatalf("expected the renamed file to be removed after Insert failed, got %v", entries)
	}
}

// ── GET /api/documents/:id ───────────────────────────────────────────────

// TestGetDocumentHandler_ZeroValueFieldsAreNotOmitted guards against
// documentJSON's `,omitempty` tags dropping a real zero value from the wire
// the same way an unset field would be - a document read locally with Mate
// off (index_cost_usd genuinely 0), no notes typed yet, filed nowhere and
// never indexed is the ordinary case, not an edge one, and
// document-details-page.tsx renders index_cost_usd.toFixed(4) and notes
// unconditionally (DocumentRecord types both as always-present). An omitted
// key there is not the same thing as a zero value to that page.
func TestGetDocumentHandler_ZeroValueFieldsAreNotOmitted(t *testing.T) {
	withTestDocumentStore(t)
	doc, err := globalDocumentStore.Insert(document{SHA256: "sha-get-zero-values", Filename: "blank.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/documents/"+doc.ID, "", doc.ID)
	if err := getDocumentHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	for _, want := range []string{`"index_cost_usd":0`, `"notes":""`, `"folder_id":null`, `"indexed_at":null`} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected response to carry %s rather than omit the field, got %s", want, body)
		}
	}
}

// ── GET /api/documents/:id/content ──────────────────────────────────────

func TestDocumentContentHandler_InlinePDFWithSecurityHeaders(t *testing.T) {
	withTestDocumentStore(t)
	content, err := os.ReadFile(testdataPath("two_page.pdf"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	doc := insertTestDocumentWithFile(t, globalDocumentStore, "sha-content-pdf", "manual.pdf", "application/pdf", content)

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/documents/"+doc.ID+"/content", "", doc.ID)
	if err := documentContentHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/pdf" {
		t.Fatalf("expected Content-Type application/pdf, got %q", got)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "inline;") {
		t.Fatalf("expected an inline disposition for a PDF, got %q", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("expected nosniff, got %q", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != "sandbox" {
		t.Fatalf("expected Content-Security-Policy: sandbox, got %q", got)
	}
	if got := rec.Header().Get("ETag"); got != `"sha-content-pdf"` {
		t.Fatalf("expected the ETag to be the sha256, got %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("expected an immutable Cache-Control, got %q", got)
	}
	if !bytes.Equal(rec.Body.Bytes(), content) {
		t.Fatalf("expected the served body to match the stored file")
	}
}

func TestDocumentContentHandler_DownloadParamForcesAttachment(t *testing.T) {
	withTestDocumentStore(t)
	content := []byte("%PDF-1.4 fake pdf bytes for a disposition test")
	doc := insertTestDocumentWithFile(t, globalDocumentStore, "sha-content-dl", "manual.pdf", "application/pdf", content)

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/documents/"+doc.ID+"/content?download=1", "", doc.ID)
	if err := documentContentHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment;") {
		t.Fatalf("expected an attachment disposition with ?download=1, got %q", got)
	}
}

func TestDocumentContentHandler_OctetStreamMIMEIsAlwaysAttachment(t *testing.T) {
	withTestDocumentStore(t)
	// An SVG or HTML upload always detects as application/octet-stream
	// (detectDocumentMIME has no case for either), which is exactly how the
	// "SVG/HTML always attachment" rule is enforced: neither MIME is ever
	// in documentInlineMIMEAllowList.
	content := []byte("<svg xmlns='http://www.w3.org/2000/svg'><script>alert(1)</script></svg>")
	doc := insertTestDocumentWithFile(t, globalDocumentStore, "sha-content-svg", "diagram.svg", "application/octet-stream", content)

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/documents/"+doc.ID+"/content", "", doc.ID)
	if err := documentContentHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment;") {
		t.Fatalf("expected an SVG served as octet-stream to always be an attachment, got %q", got)
	}
}

func TestDocumentContentHandler_TextMIMEGetsUTF8Charset(t *testing.T) {
	withTestDocumentStore(t)
	content := []byte("engine log line one\nengine log line two\n")
	doc := insertTestDocumentWithFile(t, globalDocumentStore, "sha-content-charset", "engine.log", "text/plain", content)

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/documents/"+doc.ID+"/content", "", doc.ID)
	if err := documentContentHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("expected a charset appended to a text/* mime, got %q", got)
	}
}

func TestDocumentContentHandler_RangeRequestReturns206(t *testing.T) {
	withTestDocumentStore(t)
	content := bytes.Repeat([]byte("A"), 2000)
	doc := insertTestDocumentWithFile(t, globalDocumentStore, "sha-content-range", "log.txt", "text/plain", content)

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/documents/"+doc.ID+"/content", "", doc.ID)
	c.Request().Header.Set("Range", "bytes=0-99")
	if err := documentContentHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("expected 206, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 100 {
		t.Fatalf("expected exactly 100 bytes for bytes=0-99, got %d", rec.Body.Len())
	}
}

func TestDocumentContentHandler_IfNoneMatchReturns304(t *testing.T) {
	withTestDocumentStore(t)
	content := []byte("hello world")
	doc := insertTestDocumentWithFile(t, globalDocumentStore, "sha-content-etag", "notes.txt", "text/plain", content)

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/documents/"+doc.ID+"/content", "", doc.ID)
	c.Request().Header.Set("If-None-Match", `"sha-content-etag"`)
	if err := documentContentHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDocumentContentHandler_MissingFileOnDiskReturns500(t *testing.T) {
	withTestDocumentStore(t)
	doc, err := globalDocumentStore.Insert(document{SHA256: "sha-missing-on-disk", Filename: "gone.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	// Deliberately never write the file at documentsDirPath()/doc.SHA256.

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/documents/"+doc.ID+"/content", "", doc.ID)
	if err := documentContentHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for a missing file, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDocumentContentHandler_UnknownIDReturns404(t *testing.T) {
	withTestDocumentStore(t)
	c, rec := newDocumentEchoContext(http.MethodGet, "/api/documents/does-not-exist/content", "", "does-not-exist")
	if err := documentContentHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ── GET /api/documents/:id/text ─────────────────────────────────────────

func TestDocumentTextHandler_JoinsChunksFromDefaultSeq(t *testing.T) {
	withTestDocumentStore(t)
	doc, err := globalDocumentStore.Insert(document{SHA256: "sha-text", Filename: "a.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := globalDocumentStore.ReplaceChunks(doc.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "first chunk"},
		{Seq: 2, Source: "local", Text: "second chunk"},
	}); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/documents/"+doc.ID+"/text", "", doc.ID)
	if err := documentTextHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	var resp struct {
		Text      string `json:"text"`
		NextChunk int    `json:"next_chunk"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(resp.Text, "first chunk") || !strings.Contains(resp.Text, "second chunk") {
		t.Fatalf("expected both chunks joined, got %q", resp.Text)
	}
	if resp.NextChunk != 0 {
		t.Fatalf("expected no next_chunk when everything fit, got %d", resp.NextChunk)
	}
}

func TestDocumentTextHandler_CapsAndReportsNextChunk(t *testing.T) {
	withTestDocumentStore(t)
	doc, err := globalDocumentStore.Insert(document{SHA256: "sha-text-cap", Filename: "a.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	big := strings.Repeat("a", documentTextChunkCharCap)
	if err := globalDocumentStore.ReplaceChunks(doc.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: big},
		{Seq: 2, Source: "local", Text: "overflow"},
	}); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/documents/"+doc.ID+"/text", "", doc.ID)
	if err := documentTextHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	var resp struct {
		Text      string `json:"text"`
		NextChunk int    `json:"next_chunk"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.NextChunk != 2 {
		t.Fatalf("expected next_chunk=2, got %d", resp.NextChunk)
	}
	if strings.Contains(resp.Text, "overflow") {
		t.Fatalf("expected the second chunk to be excluded once the cap was reached")
	}
}

func TestDocumentTextHandler_ChunkParamSkipsAhead(t *testing.T) {
	withTestDocumentStore(t)
	doc, err := globalDocumentStore.Insert(document{SHA256: "sha-text-skip", Filename: "a.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := globalDocumentStore.ReplaceChunks(doc.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "first"},
		{Seq: 2, Source: "local", Text: "second"},
	}); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/documents/"+doc.ID+"/text?chunk=2", "", doc.ID)
	if err := documentTextHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	var resp struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if strings.Contains(resp.Text, "first") || !strings.Contains(resp.Text, "second") {
		t.Fatalf("expected chunk=2 to skip the first chunk, got %q", resp.Text)
	}
}

// ── DELETE /api/documents/:id ────────────────────────────────────────────

func TestDeleteDocumentHandler_RemovesRowAndFile(t *testing.T) {
	withTestDocumentStore(t)
	doc := insertTestDocumentWithFile(t, globalDocumentStore, "sha-delete-me", "notes.txt", "text/plain", []byte("bye"))

	c, rec := newDocumentEchoContext(http.MethodDelete, "/api/documents/"+doc.ID, "", doc.ID)
	if err := deleteDocumentHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	if _, err := globalDocumentStore.Get(doc.ID); !errors.Is(err, errDocumentNotFound) {
		t.Fatalf("expected the row to be gone, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(documentsDirPath(), doc.SHA256)); !os.IsNotExist(err) {
		t.Fatalf("expected the file to be removed, stat err=%v", err)
	}
}

func TestDeleteDocumentHandler_UnknownIDReturns404(t *testing.T) {
	withTestDocumentStore(t)
	c, rec := newDocumentEchoContext(http.MethodDelete, "/api/documents/does-not-exist", "", "does-not-exist")
	if err := deleteDocumentHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ── PATCH /api/documents/:id ─────────────────────────────────────────────

// TestPatchDocumentHandler_WakesIndexer pins E1c's wake wiring:
// PatchDocument rebuilds the document's meta chunk (rebuildMetaChunkTx),
// cascading away its old vector, so the handler must nudge the indexer to
// re-embed it rather than leaving that to wait for an unrelated upload or
// reindex.
func TestPatchDocumentHandler_WakesIndexer(t *testing.T) {
	withTestDocumentStore(t)
	doc, err := globalDocumentStore.Insert(document{SHA256: "sha-patch-wake", Filename: "a.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	woken := false
	prevWake := documentIndexerWake
	documentIndexerWake = func() { woken = true }
	t.Cleanup(func() { documentIndexerWake = prevWake })

	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/documents/"+doc.ID, `{"title":"New Title"}`, doc.ID)
	if err := patchDocumentHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !woken {
		t.Fatalf("expected the document PATCH to wake the indexer so its meta chunk gets re-embedded")
	}
}

func TestPatchDocumentHandler_TitleChangeUpdatesSearchResults(t *testing.T) {
	withTestDocumentStore(t)
	doc, err := globalDocumentStore.Insert(document{SHA256: "sha-patch-title", Filename: "a.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	query, ok := ftsMatchQuery("impeller")
	if !ok {
		t.Fatalf("expected ftsMatchQuery to accept 'impeller'")
	}
	before, err := globalDocumentStore.Search(query, nil, false, "", 10, 0)
	if err != nil {
		t.Fatalf("Search (before): %v", err)
	}
	if len(before) != 0 {
		t.Fatalf("expected no hits before the title mentions 'impeller', got %+v", before)
	}

	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/documents/"+doc.ID, `{"title":"Impeller Replacement Guide"}`, doc.ID)
	if err := patchDocumentHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	after, err := globalDocumentStore.Search(query, nil, false, "", 10, 0)
	if err != nil {
		t.Fatalf("Search (after): %v", err)
	}
	if len(after) != 1 || after[0].DocumentID != doc.ID {
		t.Fatalf("expected the title patch to make the document findable by 'impeller', got %+v", after)
	}
}

func TestPatchDocumentHandler_FolderIDNullMovesToRoot(t *testing.T) {
	withTestDocumentStore(t)
	folder, err := globalDocumentStore.CreateFolder("Manuals", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	doc, err := globalDocumentStore.Insert(document{SHA256: "sha-patch-root", Filename: "a.pdf", MIME: "application/pdf", FolderID: &folder.ID})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/documents/"+doc.ID, `{"folder_id":null}`, doc.ID)
	if err := patchDocumentHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	got, err := globalDocumentStore.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.FolderID != nil {
		t.Fatalf("expected folder_id:null to move the document to the root, got %q", *got.FolderID)
	}
}

func TestPatchDocumentHandler_OmittedFieldsAreUntouched(t *testing.T) {
	withTestDocumentStore(t)
	folder, err := globalDocumentStore.CreateFolder("Manuals", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	doc, err := globalDocumentStore.Insert(document{
		SHA256: "sha-patch-omit", Filename: "a.pdf", MIME: "application/pdf",
		FolderID: &folder.ID, Title: "Original Title",
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/documents/"+doc.ID, `{"notes":"new notes"}`, doc.ID)
	if err := patchDocumentHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	got, err := globalDocumentStore.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != "Original Title" {
		t.Fatalf("expected the omitted title to be untouched, got %q", got.Title)
	}
	if got.FolderID == nil || *got.FolderID != folder.ID {
		t.Fatalf("expected the omitted folder_id to be untouched, got %v", got.FolderID)
	}
	if got.Notes != "new notes" {
		t.Fatalf("expected notes to be updated, got %q", got.Notes)
	}
}

func TestPatchDocumentHandler_UnknownIDReturns404(t *testing.T) {
	withTestDocumentStore(t)
	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/documents/does-not-exist", `{"title":"x"}`, "does-not-exist")
	if err := patchDocumentHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ── POST /api/documents/:id/reindex ──────────────────────────────────────

func TestReindexDocumentHandler_AssistantOffReturnsEnrichFalseAndProblem(t *testing.T) {
	withTestDocumentStore(t)
	withTestSecretsStore(t)
	t.Setenv("SETTINGS_FILE", writeAssistantSettingsFixture(t, false, "openai/gpt-4o"))

	doc, err := globalDocumentStore.Insert(document{SHA256: "sha-reindex-h", Filename: "a.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := globalDocumentStore.SetIndexed(doc.ID, "local"); err != nil {
		t.Fatalf("SetIndexed: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/documents/"+doc.ID+"/reindex", "", doc.ID)
	if err := reindexDocumentHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Document documentJSON `json:"document"`
		Enrich   bool         `json:"enrich"`
		Problem  string       `json:"problem"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Enrich {
		t.Fatalf("expected enrich=false with the assistant disabled")
	}
	if resp.Problem == "" {
		t.Fatalf("expected a non-empty problem naming why Mate can't run")
	}
	if resp.Document.Status != "pending" || resp.Document.Stage != "extract" {
		t.Fatalf("expected status/stage reset to pending/extract, got %q/%q", resp.Document.Status, resp.Document.Stage)
	}
	if resp.Document.Error != "" {
		t.Fatalf("expected error to be cleared, got %q", resp.Document.Error)
	}
}

func TestReindexDocumentHandler_UnknownIDReturns404(t *testing.T) {
	withTestDocumentStore(t)
	withTestSecretsStore(t)
	c, rec := newDocumentEchoContext(http.MethodPost, "/api/documents/does-not-exist/reindex", "", "does-not-exist")
	if err := reindexDocumentHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ── POST /api/documents/move ─────────────────────────────────────────────

func TestMoveDocumentsHandler_MovesToFolder(t *testing.T) {
	withTestDocumentStore(t)
	folder, err := globalDocumentStore.CreateFolder("Manuals", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	doc1, err := globalDocumentStore.Insert(document{SHA256: "sha-move-1", Filename: "a.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	doc2, err := globalDocumentStore.Insert(document{SHA256: "sha-move-2", Filename: "b.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	body := fmt.Sprintf(`{"ids":[%q,%q],"folder_id":%q}`, doc1.ID, doc2.ID, folder.ID)
	c, rec := newDocumentEchoContext(http.MethodPost, "/api/documents/move", body, "")
	if err := moveDocumentsHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	got1, err := globalDocumentStore.Get(doc1.ID)
	if err != nil {
		t.Fatalf("Get(doc1): %v", err)
	}
	got2, err := globalDocumentStore.Get(doc2.ID)
	if err != nil {
		t.Fatalf("Get(doc2): %v", err)
	}
	if got1.FolderID == nil || *got1.FolderID != folder.ID {
		t.Fatalf("expected doc1 moved into %q, got %v", folder.ID, got1.FolderID)
	}
	if got2.FolderID == nil || *got2.FolderID != folder.ID {
		t.Fatalf("expected doc2 moved into %q, got %v", folder.ID, got2.FolderID)
	}
}

func TestMoveDocumentsHandler_EmptyIDsReturns400(t *testing.T) {
	withTestDocumentStore(t)
	c, rec := newDocumentEchoContext(http.MethodPost, "/api/documents/move", `{"ids":[]}`, "")
	if err := moveDocumentsHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ── GET /api/documents/tags ──────────────────────────────────────────────

func TestDocumentTagsHandler_ReturnsCounts(t *testing.T) {
	withTestDocumentStore(t)
	a, err := globalDocumentStore.Insert(document{SHA256: "sha-tags-a", Filename: "a.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert(a): %v", err)
	}
	b, err := globalDocumentStore.Insert(document{SHA256: "sha-tags-b", Filename: "b.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert(b): %v", err)
	}
	if err := globalDocumentStore.UpdateMeta(a.ID, nil, nil, []string{"engine"}); err != nil {
		t.Fatalf("UpdateMeta(a): %v", err)
	}
	if err := globalDocumentStore.UpdateMeta(b.ID, nil, nil, []string{"engine", "receipts"}); err != nil {
		t.Fatalf("UpdateMeta(b): %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/documents/tags", "", "")
	if err := documentTagsHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var counts []documentTagCount
	if err := json.Unmarshal(rec.Body.Bytes(), &counts); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := map[string]int{}
	for _, c := range counts {
		got[c.Tag] = c.Count
	}
	if got["engine"] != 2 {
		t.Fatalf("expected tag 'engine' with count 2, got %+v", counts)
	}
	if got["receipts"] != 1 {
		t.Fatalf("expected tag 'receipts' with count 1, got %+v", counts)
	}
}

// ── GET /api/documents (list/search) ─────────────────────────────────────

func TestListDocumentsHandler_SearchReturnsSnippetAndPage(t *testing.T) {
	withTestDocumentStore(t)
	withTestSecretsStore(t)
	// hybridDocumentSearch (E1d) now checks assistant readiness on every
	// search, so this needs a settings file even though the test itself
	// only cares about the FTS side - an assistant-disabled fixture keeps
	// this deterministic regardless of the real settings.yaml one directory
	// up (assistantSettingsPath's own default), the same reasoning
	// TestReindexDocumentHandler_AssistantOffReturnsEnrichFalseAndProblem
	// already applies.
	t.Setenv("SETTINGS_FILE", writeAssistantSettingsFixture(t, false, "openai/gpt-4o"))
	doc, err := globalDocumentStore.Insert(document{SHA256: "sha-list-search", Filename: "manual.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := globalDocumentStore.ReplaceChunks(doc.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", PageStart: 3, Heading: "Cooling", Text: "check the impeller before each season"},
	}); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/documents?q=impeller", "", "")
	if err := listDocumentsHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Results         []documentSearchResult `json:"results"`
		Mode            string                 `json:"mode"`
		SemanticProblem string                 `json:"semantic_problem"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %+v", resp.Results)
	}
	if resp.Results[0].PageStart != 3 {
		t.Fatalf("expected page 3, got %+v", resp.Results[0])
	}
	if !strings.Contains(resp.Results[0].Snippet, "\x02") {
		t.Fatalf("expected a highlighted snippet, got %q", resp.Results[0].Snippet)
	}
	if resp.Mode != "fts" {
		t.Fatalf("expected mode fts with the assistant disabled, got %q", resp.Mode)
	}
	if resp.SemanticProblem != "" {
		t.Fatalf("expected no semantic_problem when semantic search was never configured, got %q", resp.SemanticProblem)
	}
}

// writeAssistantSettingsFixtureWithEmbedding writes a settings.yaml with an
// embedding_model/embedding_dimensions pair alongside enabled/model -
// writeAssistantSettingsFixture (assistant_handlers_test.go) predates E1d
// and knows nothing of either field, so the handler's own "semantic
// configured" tests below need their own fixture writer, same direct-to-disk
// reasoning as writeAssistantSettingsFixture's own doc comment.
func writeAssistantSettingsFixtureWithEmbedding(t *testing.T, model, embeddingModel string, embeddingDimensions int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	body := fmt.Sprintf(
		"assistant:\n    enabled: true\n    model: %q\n    embedding_model: %q\n    embedding_dimensions: %d\n    notes: \"\"\n",
		model, embeddingModel, embeddingDimensions,
	)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write settings fixture: %v", err)
	}
	return path
}

// withTestOpenRouterHTTPClient swaps the package-level openRouterHTTPClient
// (the production doer hybridDocumentSearch's cachedQueryEmbedding falls
// back to when documentSearchParams.Doer is nil) for fake, for the duration
// of the test - the same swap-and-restore idiom
// assistant_handlers_test.go's own tests already use for
// assistantOpenRouterDoer. listDocumentsHandler builds its own
// documentSearchParams with no Doer field set, so exercising "mode":"hybrid"
// through the real HTTP handler (rather than hybridDocumentSearch directly)
// needs this rather than a params field.
func withTestOpenRouterHTTPClient(t *testing.T, fake openRouterDoer) {
	t.Helper()
	prev := openRouterHTTPClient
	openRouterHTTPClient = fake
	t.Cleanup(func() { openRouterHTTPClient = prev })
}

func TestListDocumentsHandler_SearchWithSemanticConfiguredReturnsModeHybrid(t *testing.T) {
	resetDocumentQueryEmbedCache(t)
	withTestDocumentStore(t)
	store := withTestSecretsStore(t)
	if err := store.Set("OPENROUTER_API_KEY", "sk-test"); err != nil {
		t.Fatalf("Set OPENROUTER_API_KEY: %v", err)
	}
	t.Setenv("SETTINGS_FILE", writeAssistantSettingsFixtureWithEmbedding(t, "openai/gpt-4o", "openai/text-embedding-3-small", 4))

	doer := &fakeOpenRouterDoer{responses: []*http.Response{fakeEmbeddingsResponse(t, 4, 1, 0)}}
	withTestOpenRouterHTTPClient(t, doer)

	doc, err := globalDocumentStore.Insert(document{SHA256: "sha-hybrid-mode", Filename: "manual.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := globalDocumentStore.ReplaceChunks(doc.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "check the impeller before each season"},
	}); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/documents?q=impeller", "", "")
	if err := listDocumentsHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Mode            string `json:"mode"`
		SemanticProblem string `json:"semantic_problem"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Mode != "hybrid" {
		t.Fatalf("expected mode hybrid with semantic search configured and working, got %q", resp.Mode)
	}
	if resp.SemanticProblem != "" {
		t.Fatalf("expected no semantic_problem when the embedding call succeeded, got %q", resp.SemanticProblem)
	}
	if len(doer.requests) != 1 {
		t.Fatalf("expected exactly one embeddings request, got %d", len(doer.requests))
	}
}

func TestListDocumentsHandler_SearchWithFailedEmbeddingReturnsModeFTSAndSemanticProblem(t *testing.T) {
	resetDocumentQueryEmbedCache(t)
	withTestDocumentStore(t)
	store := withTestSecretsStore(t)
	if err := store.Set("OPENROUTER_API_KEY", "sk-test"); err != nil {
		t.Fatalf("Set OPENROUTER_API_KEY: %v", err)
	}
	t.Setenv("SETTINGS_FILE", writeAssistantSettingsFixtureWithEmbedding(t, "openai/gpt-4o", "openai/text-embedding-3-small", 4))

	doer := &fakeOpenRouterDoer{responses: []*http.Response{openRouterFakeResponse(500, `{"error":{"message":"upstream exploded"}}`)}}
	withTestOpenRouterHTTPClient(t, doer)

	doc, err := globalDocumentStore.Insert(document{SHA256: "sha-semantic-problem", Filename: "manual.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := globalDocumentStore.ReplaceChunks(doc.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "check the impeller before each season"},
	}); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/documents?q=impeller", "", "")
	if err := listDocumentsHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Results         []documentSearchResult `json:"results"`
		Mode            string                 `json:"mode"`
		SemanticProblem string                 `json:"semantic_problem"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Mode != "fts" {
		t.Fatalf("expected mode fts when the embedding call fails, got %q", resp.Mode)
	}
	if resp.SemanticProblem == "" {
		t.Fatalf("expected a non-empty semantic_problem naming the upstream failure")
	}
	if len(resp.Results) != 1 {
		t.Fatalf("expected the FTS result to still come through, got %+v", resp.Results)
	}
}

func TestListDocumentsHandler_ListResponseNeverIncludesMarkdown(t *testing.T) {
	withTestDocumentStore(t)
	if _, err := globalDocumentStore.Insert(document{
		SHA256: "sha-list-plain", Filename: "manual.pdf", MIME: "application/pdf",
	}); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	// Markdown is populated the same way B4's extract stage would, directly
	// through the database, since Insert itself never accepts it - the
	// point of this test is that the JSON response never surfaces the
	// column regardless of how it got set.
	if _, err := globalDocumentStore.db.Exec(`UPDATE documents SET markdown = ? WHERE sha256 = ?`, "should never leak into the API", "sha-list-plain"); err != nil {
		t.Fatalf("seed markdown: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/documents", "", "")
	if err := listDocumentsHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "should never leak") {
		t.Fatalf("expected markdown to never appear in the list response, got %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"markdown"`) {
		t.Fatalf("expected no markdown field at all in the list response, got %s", rec.Body.String())
	}
}

func TestListDocumentsHandler_FolderRootFiltersOutNestedDocuments(t *testing.T) {
	withTestDocumentStore(t)
	folder, err := globalDocumentStore.CreateFolder("Manuals", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if _, err := globalDocumentStore.Insert(document{SHA256: "sha-list-root", Filename: "root.pdf", MIME: "application/pdf"}); err != nil {
		t.Fatalf("Insert(root): %v", err)
	}
	if _, err := globalDocumentStore.Insert(document{SHA256: "sha-list-nested", Filename: "nested.pdf", MIME: "application/pdf", FolderID: &folder.ID}); err != nil {
		t.Fatalf("Insert(nested): %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/documents?folder=root", "", "")
	if err := listDocumentsHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	var resp struct {
		Documents []documentJSON `json:"documents"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Documents) != 1 || resp.Documents[0].Filename != "root.pdf" {
		t.Fatalf("expected only the root-level document, got %+v", resp.Documents)
	}
}

// ── document-folders ──────────────────────────────────────────────────────

func TestDocumentFolderHandlers_CreateNestAndBrowse(t *testing.T) {
	withTestDocumentStore(t)

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/document-folders", `{"name":"Manuals"}`, "")
	if err := createDocumentFolderHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var manuals documentFolder
	if err := json.Unmarshal(rec.Body.Bytes(), &manuals); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	c2, rec2 := newDocumentEchoContext(http.MethodPost, "/api/document-folders", fmt.Sprintf(`{"name":"Engine","parent_id":%q}`, manuals.ID), "")
	if err := createDocumentFolderHandler(c2); err != nil {
		t.Fatalf("handler returned error (nested): %v", err)
	}
	if rec2.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec2.Code, rec2.Body.String())
	}
	var engine documentFolder
	if err := json.Unmarshal(rec2.Body.Bytes(), &engine); err != nil {
		t.Fatalf("unmarshal (nested): %v", err)
	}
	if engine.ParentID == nil || *engine.ParentID != manuals.ID {
		t.Fatalf("expected Engine nested under Manuals, got %+v", engine)
	}

	c3, rec3 := newDocumentEchoContext(http.MethodGet, "/api/document-folders?parent="+manuals.ID, "", "")
	if err := listDocumentFoldersHandler(c3); err != nil {
		t.Fatalf("handler returned error (browse): %v", err)
	}
	if rec3.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec3.Code, rec3.Body.String())
	}
	var listResp struct {
		Path      []documentFolder `json:"path"`
		Folders   []documentFolder `json:"folders"`
		Documents []documentJSON   `json:"documents"`
	}
	if err := json.Unmarshal(rec3.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal (browse): %v", err)
	}
	if len(listResp.Folders) != 1 || listResp.Folders[0].ID != engine.ID {
		t.Fatalf("expected Engine as the only subfolder of Manuals, got %+v", listResp.Folders)
	}
	if len(listResp.Path) != 1 || listResp.Path[0].ID != manuals.ID {
		t.Fatalf("expected path=[Manuals], got %+v", listResp.Path)
	}
	if len(listResp.Documents) != 0 {
		t.Fatalf("expected no documents directly in Manuals, got %+v", listResp.Documents)
	}
}

func TestDocumentFolderHandlers_CreateDuplicateNameReturns409(t *testing.T) {
	withTestDocumentStore(t)
	if _, err := globalDocumentStore.CreateFolder("Manuals", nil); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/document-folders", `{"name":"manuals"}`, "")
	if err := createDocumentFolderHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for a case-insensitive duplicate name, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDocumentFolderHandlers_MoveIntoOwnDescendantReturns409(t *testing.T) {
	withTestDocumentStore(t)
	manuals, err := globalDocumentStore.CreateFolder("Manuals", nil)
	if err != nil {
		t.Fatalf("CreateFolder(Manuals): %v", err)
	}
	engine, err := globalDocumentStore.CreateFolder("Engine", &manuals.ID)
	if err != nil {
		t.Fatalf("CreateFolder(Engine): %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/document-folders/"+manuals.ID, fmt.Sprintf(`{"parent_id":%q}`, engine.ID), manuals.ID)
	if err := patchDocumentFolderHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for a cyclic move, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDocumentFolderHandlers_DeleteNotEmptyReturns409(t *testing.T) {
	withTestDocumentStore(t)
	manuals, err := globalDocumentStore.CreateFolder("Manuals", nil)
	if err != nil {
		t.Fatalf("CreateFolder(Manuals): %v", err)
	}
	if _, err := globalDocumentStore.CreateFolder("Engine", &manuals.ID); err != nil {
		t.Fatalf("CreateFolder(Engine): %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodDelete, "/api/document-folders/"+manuals.ID, "", manuals.ID)
	if err := deleteDocumentFolderHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for a non-empty folder, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDocumentFolderHandlers_UnknownIDReturns404(t *testing.T) {
	withTestDocumentStore(t)
	c, rec := newDocumentEchoContext(http.MethodDelete, "/api/document-folders/does-not-exist", "", "does-not-exist")
	if err := deleteDocumentFolderHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDocumentFolderHandlers_RenamePresenceAware(t *testing.T) {
	withTestDocumentStore(t)
	manuals, err := globalDocumentStore.CreateFolder("Manuals", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/document-folders/"+manuals.ID, `{"name":"Manuals & Guides"}`, manuals.ID)
	if err := patchDocumentFolderHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got documentFolder
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Name != "Manuals & Guides" {
		t.Fatalf("expected the renamed folder, got %+v", got)
	}
}

// TestDocumentFolderHandlers_RenamePlusInvalidMoveAppliesNeither pins the
// PATCH's one-transaction contract (matching patchDocumentHandler): a name
// and an invalid parent_id in the same request must not half-apply. Before
// the fix, patchDocumentFolderHandler committed RenameFolder in its own
// transaction before calling MoveFolder, so a rejected move (cycle here)
// still left the rename committed and the response was 409 with the name
// already changed.
func TestDocumentFolderHandlers_RenamePlusInvalidMoveAppliesNeither(t *testing.T) {
	withTestDocumentStore(t)
	manuals, err := globalDocumentStore.CreateFolder("Manuals", nil)
	if err != nil {
		t.Fatalf("CreateFolder(Manuals): %v", err)
	}
	engine, err := globalDocumentStore.CreateFolder("Engine", &manuals.ID)
	if err != nil {
		t.Fatalf("CreateFolder(Engine): %v", err)
	}

	// Renaming Manuals to Powerplant while moving it under its own child
	// Engine - the name is perfectly valid, the move is a cycle.
	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/document-folders/"+manuals.ID,
		fmt.Sprintf(`{"name":"Powerplant","parent_id":%q}`, engine.ID), manuals.ID)
	if err := patchDocumentFolderHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for a cyclic move, got %d: %s", rec.Code, rec.Body.String())
	}

	folder, err := fetchDocumentFolder(manuals.ID)
	if err != nil {
		t.Fatalf("fetchDocumentFolder: %v", err)
	}
	if folder.Name != "Manuals" {
		t.Fatalf("expected the rename to be rolled back alongside the rejected move, got name %q", folder.Name)
	}
	if folder.ParentID != nil {
		t.Fatalf("expected the folder to remain at the root, got parent %v", folder.ParentID)
	}
}

// TestDocumentFolderHandlers_RenamePlusValidMoveAppliesBoth is the positive
// case alongside the test above: a rename and a valid move in the same
// request both land.
func TestDocumentFolderHandlers_RenamePlusValidMoveAppliesBoth(t *testing.T) {
	withTestDocumentStore(t)
	manuals, err := globalDocumentStore.CreateFolder("Manuals", nil)
	if err != nil {
		t.Fatalf("CreateFolder(Manuals): %v", err)
	}
	archive, err := globalDocumentStore.CreateFolder("Archive", nil)
	if err != nil {
		t.Fatalf("CreateFolder(Archive): %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/document-folders/"+manuals.ID,
		fmt.Sprintf(`{"name":"Old Manuals","parent_id":%q}`, archive.ID), manuals.ID)
	if err := patchDocumentFolderHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var got documentFolder
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Name != "Old Manuals" {
		t.Fatalf("expected the rename to apply, got name %q", got.Name)
	}
	if got.ParentID == nil || *got.ParentID != archive.ID {
		t.Fatalf("expected the move to apply, got parent %v", got.ParentID)
	}
}

// ── route tiers ──────────────────────────────────────────────────────────

// TestBuildAPIRoutes_DocumentRoutesHaveExpectedTiers mirrors
// TestBuildAPIRoutes_AssistantRoutesHaveExpectedTiers
// (assistant_handlers_test.go): every document/document-folder route is
// present in the production route table at exactly the tier the plan
// specifies. TestAPIRouteCoverage_* (auth_middleware_test.go) already
// guards that every registered route has SOME explicit tier; this pins
// WHICH tier each of these specific routes got.
func TestBuildAPIRoutes_DocumentRoutesHaveExpectedTiers(t *testing.T) {
	sessions := newTestSessionStore(t)
	want := map[string]apiTier{
		"GET /api/documents":               tierRead,
		"GET /api/documents/tags":          tierRead,
		"GET /api/documents/:id":           tierRead,
		"GET /api/documents/:id/content":   tierRead,
		"GET /api/documents/:id/text":      tierRead,
		"GET /api/document-folders":        tierRead,
		"POST /api/documents":              tierWrite,
		"POST /api/documents/move":         tierWrite,
		"PATCH /api/documents/:id":         tierWrite,
		"DELETE /api/documents/:id":        tierWrite,
		"POST /api/documents/:id/reindex":  tierWrite,
		"POST /api/document-folders":       tierWrite,
		"PATCH /api/document-folders/:id":  tierWrite,
		"DELETE /api/document-folders/:id": tierWrite,
	}

	got := map[string]apiTier{}
	for _, route := range buildAPIRoutes(sessions, newWorldImageryHTTPClient()) {
		key := route.Method + " " + route.Path
		if _, ok := want[key]; ok {
			got[key] = route.Tier
		}
	}
	for key, tier := range want {
		if got[key] != tier {
			t.Errorf("route %s: got tier %v, want %v", key, got[key], tier)
		}
	}
	if len(got) != len(want) {
		t.Errorf("expected exactly %d document routes registered, found %d: %+v", len(want), len(got), got)
	}
}

func TestUploadDocumentHandler_SecondFilePartReturns400AndLeavesNoTempFile(t *testing.T) {
	withTestDocumentStore(t)
	withTestSecretsStore(t)

	c, rec := newDocumentUploadContext(t, []documentUploadField{
		{name: "file", filename: "a.txt", content: []byte("first")},
		{name: "file", filename: "b.txt", content: []byte("second")},
	})
	if err := uploadDocumentHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if entries := documentsDirEntries(t); len(entries) != 0 {
		t.Fatalf("expected no files left after a 400, got %v", entries)
	}
}

// Duplicates are detected by GetBySHA before the bytes are ever renamed
// into place, under the upload's per-sha256 lock - not by Insert after the
// fact. Insert's own duplicate check still exists for a race the lock
// itself already rules out within one process. Either way, the loser must
// answer as a duplicate and must not delete the file the winner's row
// points at.
func TestUploadDocumentHandler_DuplicateAtInsertKeepsExistingFile(t *testing.T) {
	store := withTestDocumentStore(t)
	withTestSecretsStore(t)

	content := []byte("same bytes")
	sum := sha256.Sum256(content)
	sha := hex.EncodeToString(sum[:])
	existing := insertTestDocumentWithFile(t, store, sha, "first.txt", "text/plain", content)

	c, rec := newDocumentUploadContext(t, []documentUploadField{
		{name: "file", filename: "second.txt", content: content},
	})
	if err := uploadDocumentHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 duplicate, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), existing.ID) {
		t.Fatalf("expected the existing document %s in the response, got %s", existing.ID, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(documentsDirPath(), sha)); err != nil {
		t.Fatalf("existing document's file must survive: %v", err)
	}
}

// TestUploadDocumentHandler_AssistantReadinessFailurePreservesExistingFile
// guards the order of operations: the temp file must not be renamed to its
// final, content-addressed path until AFTER checkAssistantReadiness
// succeeds, because renaming first and then removing that path on a
// readiness failure would delete whatever file already lived there - the
// EXISTING document's file, whenever this upload's bytes happen to match
// one already on disk. Readiness is checked first, so a failure here must
// remove only this attempt's own temp file and never touch the existing
// document.
func TestUploadDocumentHandler_AssistantReadinessFailurePreservesExistingFile(t *testing.T) {
	store := withTestDocumentStore(t)
	withTestSecretsStore(t)

	// Malformed settings.yaml makes checkAssistantReadiness return a
	// genuine error (readSettings' YAML parse failure) rather than merely
	// reporting a "problem" - the failure mode this test targets.
	settingsPath := filepath.Join(t.TempDir(), "settings.yaml")
	if err := os.WriteFile(settingsPath, []byte("assistant: [this is not valid yaml"), 0o644); err != nil {
		t.Fatalf("write malformed settings: %v", err)
	}
	t.Setenv("SETTINGS_FILE", settingsPath)

	content := []byte("bytes that will collide with an existing document")
	sum := sha256.Sum256(content)
	sha := hex.EncodeToString(sum[:])
	existing := insertTestDocumentWithFile(t, store, sha, "first.txt", "text/plain", content)

	c, rec := newDocumentUploadContext(t, []documentUploadField{
		{name: "file", filename: "second.txt", content: content},
	})
	if err := uploadDocumentHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when the readiness check itself fails, got %d: %s", rec.Code, rec.Body.String())
	}

	if _, err := os.Stat(filepath.Join(documentsDirPath(), sha)); err != nil {
		t.Fatalf("expected the existing document's file to survive a readiness-check failure, got %v", err)
	}
	if _, err := globalDocumentStore.Get(existing.ID); err != nil {
		t.Fatalf("expected the existing document row to survive, got %v", err)
	}
	if entries := documentsDirEntries(t); len(entries) != 1 {
		t.Fatalf("expected exactly 1 file left on disk (no leaked temp file, no deleted existing file), got %d: %v", len(entries), entries)
	}
}

// TestDeleteDocumentHandler_BlocksOnSHALockUntilReleased is a deterministic
// race test: uploadDocumentHandler and deleteDocumentHandler share one
// per-sha256 lock precisely so a concurrent delete and an identical
// re-upload can never interleave into a row with no file or a file no row
// points at. Rather than racing a real concurrent upload (sleep-and-hope),
// this takes the lock itself and proves the delete handler blocks on it
// until released, touching neither the row nor the file in the meantime.
func TestDeleteDocumentHandler_BlocksOnSHALockUntilReleased(t *testing.T) {
	withTestDocumentStore(t)
	doc := insertTestDocumentWithFile(t, globalDocumentStore, "sha-delete-lock", "notes.txt", "text/plain", []byte("bye"))

	unlock := lockDocumentSHA(doc.SHA256)

	done := make(chan error, 1)
	go func() {
		c, rec := newDocumentEchoContext(http.MethodDelete, "/api/documents/"+doc.ID, "", doc.ID)
		err := deleteDocumentHandler(c)
		if err == nil && rec.Code != http.StatusNoContent {
			err = fmt.Errorf("expected 204, got %d: %s", rec.Code, rec.Body.String())
		}
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("expected the delete handler to block while the sha lock is held, but it finished (err=%v)", err)
	case <-time.After(100 * time.Millisecond):
	}

	// Still held: neither the row nor the file may be gone yet.
	if _, err := globalDocumentStore.Get(doc.ID); err != nil {
		t.Fatalf("expected the document row to survive while the lock is held, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(documentsDirPath(), doc.SHA256)); err != nil {
		t.Fatalf("expected the file to survive while the lock is held, got %v", err)
	}

	unlock()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("delete handler failed once unblocked: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("expected the delete handler to finish once the lock was released")
	}

	if _, err := globalDocumentStore.Get(doc.ID); !errors.Is(err, errDocumentNotFound) {
		t.Fatalf("expected the row to be gone after delete, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(documentsDirPath(), doc.SHA256)); !os.IsNotExist(err) {
		t.Fatalf("expected the file to be gone after delete, stat err=%v", err)
	}
}

// TestPatchDocumentHandler_BadFolderIDLeavesTitleUnchanged is the
// handler-level mirror of
// TestDocumentStore_PatchDocumentBadFolderIDLeavesTitleUnchanged
// (documents_store_test.go): title and folder_id commit through one
// PatchDocument transaction, so a bad folder_id must leave the title
// unchanged even though the request as a whole reports 404.
func TestPatchDocumentHandler_BadFolderIDLeavesTitleUnchanged(t *testing.T) {
	withTestDocumentStore(t)
	doc, err := globalDocumentStore.Insert(document{SHA256: "sha-patch-atomic-h", Filename: "a.pdf", MIME: "application/pdf", Title: "Original Title"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/documents/"+doc.ID, `{"title":"New Title","folder_id":"does-not-exist"}`, doc.ID)
	if err := patchDocumentHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a bad folder_id, got %d: %s", rec.Code, rec.Body.String())
	}

	got, err := globalDocumentStore.Get(doc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != "Original Title" {
		t.Fatalf("expected the title to remain unchanged when folder_id is invalid, got %q", got.Title)
	}
}

// ── GET /api/documents/embeddings, POST .../backfill (E1c) ──────────────

// writeDocumentEmbeddingsSettingsFixture writes a settings.yaml with the
// assistant fully ready (enabled, a chat model, a document model) and
// embedding_model/embedding_dimensions set exactly as given - unlike
// writeAssistantSettingsFixture (assistant_handlers_test.go), which never
// mentions embedding_model at all, so an absent key would default to
// defaultEmbeddingModel/defaultEmbeddingDimensions (signalk.go's
// presence-vs-value distinction) rather than the specific value each test
// below needs to pin.
func writeDocumentEmbeddingsSettingsFixture(t *testing.T, embeddingModel string, dims int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	body := fmt.Sprintf(
		"assistant:\n    enabled: true\n    model: %q\n    document_model: %q\n    embedding_model: %q\n    embedding_dimensions: %d\n    notes: \"\"\n",
		"openai/gpt-4o", "openai/gpt-4o", embeddingModel, dims,
	)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write settings fixture: %v", err)
	}
	return path
}

func TestDocumentsEmbeddingsStatusHandler_SemanticSearchOff(t *testing.T) {
	withTestDocumentStore(t)
	secrets := withTestSecretsStore(t)
	if err := secrets.Set("OPENROUTER_API_KEY", "sk-test"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	t.Setenv("SETTINGS_FILE", writeDocumentEmbeddingsSettingsFixture(t, "", 512))

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/documents/embeddings", "", "")
	if err := documentsEmbeddingsStatusHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp documentEmbeddingsStatusJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Enabled {
		t.Fatalf("expected enabled=false with a blank embedding model")
	}
	if resp.Problem == "" {
		t.Fatalf("expected a problem naming why semantic search is off")
	}
}

func TestDocumentsEmbeddingsStatusHandler_SemanticSearchOn(t *testing.T) {
	withTestDocumentStore(t)
	secrets := withTestSecretsStore(t)
	if err := secrets.Set("OPENROUTER_API_KEY", "sk-test"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	t.Setenv("SETTINGS_FILE", writeDocumentEmbeddingsSettingsFixture(t, "openai/text-embedding-3-small", 512))

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/documents/embeddings", "", "")
	if err := documentsEmbeddingsStatusHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp documentEmbeddingsStatusJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.Enabled {
		t.Fatalf("expected enabled=true, got problem %q", resp.Problem)
	}
	if resp.Model != "openai/text-embedding-3-small" || resp.Dimensions != 512 {
		t.Fatalf("unexpected model/dimensions: %+v", resp)
	}
	if resp.Problem != "" {
		t.Fatalf("expected no problem, got %q", resp.Problem)
	}
}
