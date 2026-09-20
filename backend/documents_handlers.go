package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// This file serves the document library's HTTP API (ADR 0106): upload,
// listing/search, folder browsing and CRUD, content/text serving, and
// reindex. Every handler here reads globalDocumentStore directly (the same
// package-level-var pattern assistant_handlers.go uses for
// globalAssistantStore), so tests swap it for a t.TempDir()-backed store
// (withTestDocumentStore, documents_handlers_test.go) rather than
// constructing handlers with a store argument.

// documentMaxUploadBytes caps one upload's total request body. A var, not a
// const: TestUploadDocumentHandler_OverCapReturns413 lowers it for the
// duration of one test so it doesn't have to actually push 100MB+ of body
// through httptest to exercise the 413 path.
var documentMaxUploadBytes int64 = 100 << 20 // 100 MB

// documentIndexerWake nudges the background indexer (B4) to look at the
// database queue immediately after a successful upload or reindex, rather
// than waiting for its own poll. nil until B4 assigns it in main() - every
// caller here goes through wakeDocumentIndexer instead of calling this
// directly, so an upload/reindex works identically (indexing merely resumes
// on the indexer's own schedule) before B4 exists.
var documentIndexerWake func()

func wakeDocumentIndexer() {
	if documentIndexerWake != nil {
		documentIndexerWake()
	}
}

// documentIndexerStartBackfill and documentIndexerBackfillStatus wire POST
// /api/documents/embeddings/backfill and GET /api/documents/embeddings
// (E1c) to the running indexer's backfill methods, the same nil-until-wired
// pattern as documentIndexerWake above: nil until main() assigns them once
// the real documentIndexer exists, so a test that never built one still
// gets sane, non-panicking handler behaviour (a backfill start is refused,
// a status read comes back as "never run") rather than a nil-func panic.
var documentIndexerStartBackfill func() error
var documentIndexerBackfillStatus func() documentBackfillStatus

// currentDocumentBackfillStatus is documentIndexerBackfillStatus's nil-safe
// caller.
func currentDocumentBackfillStatus() documentBackfillStatus {
	if documentIndexerBackfillStatus != nil {
		return documentIndexerBackfillStatus()
	}
	return documentBackfillStatus{}
}

// ── per-sha256 locking ───────────────────────────────────────────────────

// documentShaLock is one entry of documentShaLocks: a mutex plus a
// reference count of how many callers currently hold or are waiting on it,
// so lockDocumentSHA can drop the entry once nobody needs it anymore rather
// than growing the map forever.
type documentShaLock struct {
	mu  sync.Mutex
	ref int
}

var documentShaLocksMu sync.Mutex
var documentShaLocks = map[string]*documentShaLock{}

// lockDocumentSHA locks sha's own mutex (creating one on first use) and
// returns the function that unlocks it. uploadDocumentHandler holds this
// across its whole GetBySHA/rename/Insert sequence, and
// deleteDocumentHandler holds it across its database delete and file
// removal, so an upload and a delete racing over the same content can never
// interleave into a row with no file, or a file no row points at.
func lockDocumentSHA(sha string) func() {
	documentShaLocksMu.Lock()
	l, ok := documentShaLocks[sha]
	if !ok {
		l = &documentShaLock{}
		documentShaLocks[sha] = l
	}
	l.ref++
	documentShaLocksMu.Unlock()

	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		documentShaLocksMu.Lock()
		l.ref--
		if l.ref == 0 {
			delete(documentShaLocks, sha)
		}
		documentShaLocksMu.Unlock()
	}
}

// ── JSON shapes ──────────────────────────────────────────────────────────

// documentTagJSON is one entry of documentJSON's Tags: a tag plus which
// source assigned it, letting the frontend render an operator's own tags
// differently from Mate's suggestions without knowing about the
// document_tags table's split.
type documentTagJSON struct {
	Tag    string `json:"tag"`
	Source string `json:"source"`
}

// documentJSON is the wire shape returned by every document-reading and
// document-writing endpoint below - deliberately not the store's own
// `document` struct (documents_store.go): it never carries Markdown (a
// document's full extracted/OCR'd text has no place in a list or metadata
// response - GET .../text exists precisely so a client fetches that
// separately, on purpose, possibly paginated) and it folds OperatorTags/
// SuggestedTags into one ordered slice instead of exposing the two-source
// split as two JSON arrays.
//
// ADR 0115 §7: every field below is unconditional (no `,omitempty`), including the
// pointers: use-documents.ts's DocumentRecord declares all of them as
// always-present (folder_id and indexed_at as `| null`, everything else as
// a plain scalar), and document-details-page.tsx renders several of them
// directly - index_cost_usd.toFixed(4), notes in a controlled Textarea. A
// zero cost, an empty note and an unfiled, never-indexed document are real
// values the client needs to render, not absent ones, and `,omitempty`
// cannot tell "genuinely zero" apart from "unset" - it drops both, which
// left the frontend decoding an absent key as `undefined` rather than the
// 0/""/null its own types promise.
type documentJSON struct {
	ID           string            `json:"id"`
	SHA256       string            `json:"sha256"`
	FolderID     *string           `json:"folder_id"`
	Filename     string            `json:"filename"`
	Title        string            `json:"title"`
	Notes        string            `json:"notes"`
	MIME         string            `json:"mime"`
	SizeBytes    int64             `json:"size_bytes"`
	PageCount    int               `json:"page_count"`
	Summary      string            `json:"summary"`
	Status       string            `json:"status"`
	Stage        string            `json:"stage"`
	IndexedWith  string            `json:"indexed_with"`
	Error        string            `json:"error"`
	IndexModel   string            `json:"index_model"`
	IndexCostUSD float64           `json:"index_cost_usd"`
	Tags         []documentTagJSON `json:"tags"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	IndexedAt    *time.Time        `json:"indexed_at"`

	// Kind/NoteType/NoteTypeSource/Pinned/SortIndex back the notes feature
	// (notes_store.go). Unconditional, like every field above, for the
	// reason ADR 0115 §7 gives: use-notes.ts and use-documents.ts declare
	// all five as always-present, and `,omitempty` cannot tell "genuinely
	// zero" apart from "unset". On a kind='file' row these are all their
	// zero value, which is a real answer the client renders (an unpinned,
	// unordered, untyped file) rather than an absent one.
	Kind           string `json:"kind"`
	NoteType       string `json:"note_type"`
	NoteTypeSource string `json:"note_type_source"`
	Pinned         bool   `json:"pinned"`
	SortIndex      int    `json:"sort_index"`
}

func toDocumentJSON(d document) documentJSON {
	tags := make([]documentTagJSON, 0, len(d.OperatorTags)+len(d.SuggestedTags))
	for _, t := range d.OperatorTags {
		tags = append(tags, documentTagJSON{Tag: t, Source: "operator"})
	}
	for _, t := range d.SuggestedTags {
		tags = append(tags, documentTagJSON{Tag: t, Source: "suggested"})
	}
	return documentJSON{
		ID:           d.ID,
		SHA256:       d.SHA256,
		FolderID:     d.FolderID,
		Filename:     d.Filename,
		Title:        d.Title,
		Notes:        d.Notes,
		MIME:         d.MIME,
		SizeBytes:    d.SizeBytes,
		PageCount:    d.PageCount,
		Summary:      d.Summary,
		Status:       d.Status,
		Stage:        d.Stage,
		IndexedWith:  d.IndexedWith,
		Error:        d.Error,
		IndexModel:   d.IndexModel,
		IndexCostUSD: d.IndexCostUSD,
		Tags:         tags,
		CreatedAt:    d.CreatedAt,
		UpdatedAt:    d.UpdatedAt,
		IndexedAt:    d.IndexedAt,

		Kind:           d.Kind,
		NoteType:       d.NoteType,
		NoteTypeSource: d.NoteTypeSource,
		Pinned:         d.Pinned,
		SortIndex:      d.SortIndex,
	}
}

// ── error mapping ────────────────────────────────────────────────────────

// documentErrorStatus maps the document/folder store's sentinel errors to
// the HTTP status this API reports them as. Upload's own folder_id check is
// deliberately NOT routed through this (uploadDocumentHandler reports a bad
// folder_id as 400, a request-shape problem, rather than 404 - see its own
// comment).
func documentErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, errDocumentNotFound):
		return http.StatusNotFound, "document not found"
	case errors.Is(err, errFolderNotFound):
		return http.StatusNotFound, "folder not found"
	case errors.Is(err, errFolderCycle):
		return http.StatusConflict, errFolderCycle.Error()
	case errors.Is(err, errFolderNameTaken):
		return http.StatusConflict, errFolderNameTaken.Error()
	case errors.Is(err, errFolderNotEmpty):
		return http.StatusConflict, errFolderNotEmpty.Error()
	case errors.Is(err, errFolderNameInvalid):
		return http.StatusBadRequest, errFolderNameInvalid.Error()
	// errDocumentDuplicate predates the notes feature (Insert's own
	// sha256-collision check, documents_store.go) but fell through to 500
	// here until now - harmless while the only caller that could ever
	// trigger it (uploadDocumentHandler) always intercepted it itself
	// before this function ever saw it. ReplaceNoteBody's own collision
	// check (notes_store.go, plan §1's residual UUID-collision case) is
	// the first caller that reaches documentErrorStatus WITH this
	// sentinel still on the error, so it needs a real mapping now: 409,
	// naming the colliding document's id (already baked into err's own
	// message by the store method that returned it).
	case errors.Is(err, errDocumentDuplicate):
		return http.StatusConflict, err.Error()
	case errors.Is(err, errNoteBodyEmpty):
		return http.StatusBadRequest, errNoteBodyEmpty.Error()
	case errors.Is(err, errNoteBodyTooLarge):
		return http.StatusRequestEntityTooLarge, errNoteBodyTooLarge.Error()
	case errors.Is(err, errNotANote):
		return http.StatusConflict, errNotANote.Error()
	default:
		return http.StatusInternalServerError, err.Error()
	}
}

// writeDocumentError answers c with documentErrorStatus's mapping of err,
// logging the ones that mean something actually went wrong (as opposed to
// an entirely expected "not found"/"conflict" outcome the caller triggered
// themselves).
func writeDocumentError(c echo.Context, err error) error {
	status, msg := documentErrorStatus(err)
	if status == http.StatusInternalServerError {
		log.Printf("documents: %v", err)
	}
	return c.JSON(status, map[string]string{"error": msg})
}

// ── query parameter helpers ──────────────────────────────────────────────

const (
	documentListDefaultLimit = 50
	documentListMaxLimit     = 200
)

func parseDocumentLimit(raw string) int {
	if raw == "" {
		return documentListDefaultLimit
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return documentListDefaultLimit
	}
	if n > documentListMaxLimit {
		return documentListMaxLimit
	}
	return n
}

func parseDocumentOffset(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func parseDocumentBool(raw string) bool {
	v, _ := strconv.ParseBool(raw)
	return v
}

// resolveDocumentFolderQueryParam turns the "folder" query parameter into
// List/Search's folderID contract: "" (not given at all) means no filter,
// "root" means documentsRootFolderSentinel (documents_store.go), and
// anything else is taken as a literal folder id (List/Search themselves
// report errFolderNotFound if it doesn't exist).
func resolveDocumentFolderQueryParam(raw string) *string {
	raw = strings.TrimSpace(raw)
	switch raw {
	case "":
		return nil
	case "root":
		v := documentsRootFolderSentinel
		return &v
	default:
		return &raw
	}
}

// ── GET /api/documents ───────────────────────────────────────────────────

func listDocumentsHandler(c echo.Context) error {
	q := strings.TrimSpace(c.QueryParam("q"))
	tag := strings.TrimSpace(c.QueryParam("tag"))
	recursive := parseDocumentBool(c.QueryParam("recursive"))
	limit := parseDocumentLimit(c.QueryParam("limit"))
	offset := parseDocumentOffset(c.QueryParam("offset"))
	folderID := resolveDocumentFolderQueryParam(c.QueryParam("folder"))

	if q != "" {
		outcome, err := hybridDocumentSearch(c.Request().Context(), documentSearchParams{
			Store:     globalDocumentStore,
			Query:     q,
			FolderID:  folderID,
			Recursive: recursive,
			Tag:       tag,
			Limit:     limit,
			Offset:    offset,
			Readiness: func() (assistantReadiness, string, error) { return checkAssistantReadiness(assistantSettingsPath()) },
		})
		if err != nil {
			return writeDocumentError(c, err)
		}
		resp := map[string]any{"results": outcome.Results, "mode": outcome.Mode}
		if outcome.SemanticProblem != "" {
			resp["semantic_problem"] = outcome.SemanticProblem
		}
		return c.JSON(http.StatusOK, resp)
	}

	docs, err := globalDocumentStore.List(folderID, recursive, tag, limit, offset)
	if err != nil {
		return writeDocumentError(c, err)
	}
	out := make([]documentJSON, len(docs))
	for i, d := range docs {
		out[i] = toDocumentJSON(d)
	}
	return c.JSON(http.StatusOK, map[string]any{"documents": out})
}

// ── GET /api/documents/tags ──────────────────────────────────────────────

func documentTagsHandler(c echo.Context) error {
	counts, err := globalDocumentStore.TagCounts()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if counts == nil {
		counts = []documentTagCount{}
	}
	return c.JSON(http.StatusOK, counts)
}

// ── GET /api/documents/:id ───────────────────────────────────────────────

func getDocumentHandler(c echo.Context) error {
	doc, err := globalDocumentStore.Get(c.Param("id"))
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, toDocumentJSON(doc))
}

// ── GET /api/documents/:id/content ───────────────────────────────────────

// documentInlineMIMEAllowList is the only MIME types documentContentHandler
// will ever serve `inline`; everything else - including SVG and HTML,
// which a browser would otherwise happily execute in the page's own origin
// - is always `attachment` regardless of the ?download= query (ADR 0106's
// serving-security section).
var documentInlineMIMEAllowList = map[string]bool{
	"application/pdf":  true,
	"image/png":        true,
	"image/jpeg":       true,
	"image/gif":        true,
	"image/webp":       true,
	"text/plain":       true,
	"text/markdown":    true,
	"text/csv":         true,
	"application/json": true,
}

func documentContentHandler(c echo.Context) error {
	doc, err := globalDocumentStore.Get(c.Param("id"))
	if err != nil {
		return writeDocumentError(c, err)
	}

	path := filepath.Join(documentsDirPath(), doc.SHA256)
	f, err := os.Open(path)
	if err != nil {
		// A missing file here means the database and disk have drifted
		// apart (the sweep at boot logs exactly this case rather than
		// silently fixing it either way - documents_store.go's
		// sweepDocumentsDir) - a genuine server-side problem, not a 404 for
		// this document id, which the caller supplied correctly.
		log.Printf("documents: content: open %s: %v", path, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "document file missing on disk"})
	}
	defer f.Close()

	contentType := doc.MIME
	if strings.HasPrefix(contentType, "text/") || contentType == "application/json" {
		contentType += "; charset=utf-8"
	}
	header := c.Response().Header()
	header.Set(echo.HeaderContentType, contentType)
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", "sandbox")
	header.Set("ETag", `"`+doc.SHA256+`"`)
	header.Set("Cache-Control", "private, max-age=31536000, immutable")

	disposition := "attachment"
	if c.QueryParam("download") != "1" && documentInlineMIMEAllowList[doc.MIME] {
		disposition = "inline"
	}
	header.Set(echo.HeaderContentDisposition, mime.FormatMediaType(disposition, map[string]string{"filename": doc.Filename}))

	http.ServeContent(c.Response(), c.Request(), doc.Filename, doc.CreatedAt, f)
	return nil
}

// ── GET /api/documents/:id/text ──────────────────────────────────────────

// documentTextChunkCharCap softly bounds one GET .../text response: chunks
// are appended until adding the next one would cross this, so a single
// response can't grow unbounded regardless of how large the underlying
// document is. The first chunk is always included even if it alone exceeds
// the cap, so the endpoint always makes forward progress (next_chunk moves
// on) rather than getting stuck.
const documentTextChunkCharCap = 100_000

func documentTextHandler(c echo.Context) error {
	fromSeq := 1 // seq 0 is the meta chunk (title/tags/notes), not body text
	if raw := c.QueryParam("chunk"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			fromSeq = n
		}
	}

	chunks, err := globalDocumentStore.ChunksFrom(c.Param("id"), fromSeq)
	if err != nil {
		return writeDocumentError(c, err)
	}

	var b strings.Builder
	nextChunk := 0
	for _, ch := range chunks {
		if b.Len() > 0 && b.Len()+len(ch.Text) > documentTextChunkCharCap {
			nextChunk = ch.Seq
			break
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(ch.Text)
	}

	resp := map[string]any{"text": b.String()}
	if nextChunk > 0 {
		resp["next_chunk"] = nextChunk
	}
	return c.JSON(http.StatusOK, resp)
}

// ── POST /api/documents (upload) ─────────────────────────────────────────

// headCapture is a bounded io.Writer that keeps only the first 512 bytes
// ever written to it and discards the rest - exactly what
// detectDocumentMIME needs from an upload, gathered in the same
// io.MultiWriter pass as the on-disk temp file and the sha256 hash rather
// than read from disk a second time afterward.
type headCapture struct {
	buf []byte
}

func (h *headCapture) Write(p []byte) (int, error) {
	if len(h.buf) < 512 {
		room := 512 - len(h.buf)
		if room > len(p) {
			room = len(p)
		}
		h.buf = append(h.buf, p[:room]...)
	}
	return len(p), nil
}

// documentUploadReadError distinguishes the one read failure that means
// something specific (the body exceeded documentMaxUploadBytes) from every
// other multipart read error, which is just a malformed request.
func documentUploadReadError(c echo.Context, err error) error {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return c.JSON(http.StatusRequestEntityTooLarge, map[string]string{
			"error": fmt.Sprintf("upload exceeds the %d byte limit", documentMaxUploadBytes),
		})
	}
	return c.JSON(http.StatusBadRequest, map[string]string{"error": "failed to read upload: " + err.Error()})
}

// parseDocumentTagsField splits an upload's comma-separated "tags" field:
// trimmed, empty entries dropped, same as an operator retyping the field
// with stray commas.
func parseDocumentTagsField(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	tags := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			tags = append(tags, p)
		}
	}
	return tags
}

// uploadDocumentHandler streams a multipart upload straight to disk (ADR
// 0106): the request body is capped at documentMaxUploadBytes up front, and
// the "file" part is read with MultipartReader (not FormFile, which would
// otherwise spill a large file into a hidden temp file of its own before
// this handler ever saw it) into its own upload-*.tmp, hashed as it goes.
// Every failure path removes whatever temp/renamed file this attempt had
// created - AGENTS.md's fallback policy has no room for an orphaned upload
// file left behind by a request that never actually created a document.
func uploadDocumentHandler(c echo.Context) error {
	req := c.Request()
	req.Body = http.MaxBytesReader(c.Response(), req.Body, documentMaxUploadBytes)

	reader, err := req.MultipartReader()
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "expected multipart/form-data"})
	}

	dir := documentsDirPath()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to prepare document storage"})
	}

	var (
		tmpPath  string
		filename string
		title    string
		tagsRaw  string
		folderID *string
		size     int64
		haveFile bool
	)
	removeTemp := func() {
		if tmpPath != "" {
			os.Remove(tmpPath)
		}
	}

	hasher := sha256.New()
	head := &headCapture{}

	for {
		part, partErr := reader.NextPart()
		if partErr == io.EOF {
			break
		}
		if partErr != nil {
			removeTemp()
			return documentUploadReadError(c, partErr)
		}

		switch part.FormName() {
		case "file":
			if tmpPath != "" {
				part.Close()
				removeTemp()
				return c.JSON(http.StatusBadRequest, map[string]string{"error": "only one file per upload"})
			}
			filename = part.FileName()
			tmpFile, createErr := os.CreateTemp(dir, "upload-*.tmp")
			if createErr != nil {
				part.Close()
				removeTemp()
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create temp file"})
			}
			tmpPath = tmpFile.Name()
			mw := io.MultiWriter(tmpFile, hasher, head)
			n, copyErr := io.Copy(mw, part)
			closeErr := tmpFile.Close()
			part.Close()
			if copyErr != nil {
				removeTemp()
				return documentUploadReadError(c, copyErr)
			}
			if closeErr != nil {
				removeTemp()
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to save upload"})
			}
			size = n
			haveFile = n > 0
		case "title":
			b, readErr := io.ReadAll(part)
			part.Close()
			if readErr != nil {
				removeTemp()
				return documentUploadReadError(c, readErr)
			}
			title = strings.TrimSpace(string(b))
		case "tags":
			b, readErr := io.ReadAll(part)
			part.Close()
			if readErr != nil {
				removeTemp()
				return documentUploadReadError(c, readErr)
			}
			tagsRaw = string(b)
		case "folder_id":
			b, readErr := io.ReadAll(part)
			part.Close()
			if readErr != nil {
				removeTemp()
				return documentUploadReadError(c, readErr)
			}
			if v := strings.TrimSpace(string(b)); v != "" {
				folderID = &v
			}
		default:
			part.Close()
		}
	}

	if !haveFile {
		removeTemp()
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "file is required"})
	}

	sha := hex.EncodeToString(hasher.Sum(nil))

	// Readiness is checked before the bytes ever move to their final,
	// content-addressed path: a failure here removes only this attempt's
	// own temp file. Renaming first and checking readiness after would risk
	// deleting an EXISTING document's file on a readiness failure, whenever
	// this upload's bytes happen to match one already on disk (the final
	// path is content-addressed by sha256, so it can collide with an
	// existing document's own file).
	readiness, _, err := checkAssistantReadiness(assistantSettingsPath())
	if err != nil {
		removeTemp()
		log.Printf("documents: upload: check assistant readiness: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	// documentEnrichReadinessProblem, not readiness.Problem directly: this
	// consent decision also requires a document model, a document-only
	// requirement that must never affect chat's own readiness.
	enrichProblem := documentEnrichReadinessProblem(readiness)

	// Everything from here to Insert runs under sha's own lock, shared with
	// deleteDocumentHandler: a concurrent delete of the document this hash
	// already belongs to, and this upload discovering or creating that row,
	// can never interleave. Under the lock, GetBySHA - not Insert's own
	// duplicate check - is what actually decides "duplicate", because only
	// under the lock can "no row owns this hash" be trusted; that is what
	// makes removing finalPath safe below on a later failure.
	unlockSHA := lockDocumentSHA(sha)
	defer unlockSHA()

	if existing, ok, err := globalDocumentStore.GetBySHA(sha); err != nil {
		removeTemp()
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	} else if ok {
		// Identical bytes already on file: no second copy, no second OCR
		// charge (ADR 0106). The file on disk belongs to the existing row,
		// so only this attempt's own temp file is removed.
		removeTemp()
		return c.JSON(http.StatusOK, map[string]any{"document": toDocumentJSON(existing), "duplicate": true})
	}

	finalPath := filepath.Join(dir, sha)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		removeTemp()
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to store upload"})
	}
	tmpPath = "" // renamed into place; removeTemp must not touch it anymore

	inserted, err := globalDocumentStore.Insert(document{
		SHA256:       sha,
		FolderID:     folderID,
		Filename:     filename,
		Title:        title,
		MIME:         detectDocumentMIME(head.buf, filename),
		SizeBytes:    size,
		Enrich:       enrichProblem == "",
		OperatorTags: parseDocumentTagsField(tagsRaw),
	})
	if errors.Is(err, errDocumentDuplicate) {
		// Insert reports the duplicate itself here (its own sha256 check),
		// so the file is never removed in this branch: it belongs to the
		// existing row Insert just found, not to this attempt.
		return c.JSON(http.StatusOK, map[string]any{"document": toDocumentJSON(inserted), "duplicate": true})
	}
	if err != nil {
		// Safe to remove: still holding sha's lock, and GetBySHA just
		// confirmed no row owned this hash, so finalPath can only be this
		// attempt's own file.
		os.Remove(finalPath)
		if errors.Is(err, errFolderNotFound) {
			// Upload's folder_id is part of the request body, not a path
			// segment naming an existing resource - a bad value here is a
			// 400 (malformed request), not the 404 the same sentinel maps
			// to everywhere else in this file (documentErrorStatus).
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "folder not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	wakeDocumentIndexer()
	return c.JSON(http.StatusCreated, map[string]any{"document": toDocumentJSON(inserted), "duplicate": false})
}

// ── PATCH /api/documents/:id ──────────────────────────────────────────────

// patchDocumentHandler applies a presence-aware partial update: a field
// absent from the JSON body is left untouched, distinguished from an
// explicit null/empty by decoding into map[string]json.RawMessage first
// (mirrors parseCollisionProfiles' raw-field style, collision_profile.go)
// rather than a struct of plain pointers, which can't tell "omitted" apart
// from JSON null. folder_id: null moves the document to the root, same
// contract as MoveDocuments' own nil (documents_store.go). Every field is
// parsed and validated before any of them reaches the store, and
// PatchDocument applies them all in one transaction, so an invalid
// folder_id rejects the whole patch - title/notes/tags never commit only
// to have folder_id fail on its own afterward.
func patchDocumentHandler(c echo.Context) error {
	id := c.Param("id")

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(c.Request().Body).Decode(&raw); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if len(raw) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "no patch fields provided"})
	}

	var title, notes *string
	var tags []string
	var folderID *string
	var moveFolder bool

	if v, ok := raw["title"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid title"})
		}
		title = &s
	}
	if v, ok := raw["notes"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid notes"})
		}
		notes = &s
	}
	if v, ok := raw["tags"]; ok {
		if err := json.Unmarshal(v, &tags); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid tags"})
		}
	}
	if v, ok := raw["folder_id"]; ok {
		moveFolder = true
		if string(v) != "null" {
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid folder_id"})
			}
			folderID = &s
		}
	}

	doc, err := globalDocumentStore.PatchDocument(id, title, notes, tags, folderID, moveFolder)
	if err != nil {
		return writeDocumentError(c, err)
	}
	// PatchDocument always rebuilds the document's meta chunk
	// (rebuildMetaChunkTx, documents_store.go): the DELETE+INSERT cascades
	// away its old vector (ON DELETE CASCADE on document_chunk_embeddings),
	// so the meta chunk needs re-embedding - wake the indexer rather than
	// leaving that to wait for the next unrelated upload/reindex.
	wakeDocumentIndexer()
	return c.JSON(http.StatusOK, toDocumentJSON(doc))
}

// ── DELETE /api/documents/:id ─────────────────────────────────────────────

// deleteDocumentHandler removes the database row first (documentStore.
// Delete, transactional) and only then the file on disk, matching ADR
// 0106's ordering: a failure removing the file leaves an orphan the boot
// sweep will report, never a document row pointing at bytes that are
// already gone. Both steps run under the document's sha256 lock, shared
// with uploadDocumentHandler, so a concurrent identical upload can never
// observe - or cause - a row with no file or a file no row points at.
func deleteDocumentHandler(c echo.Context) error {
	doc, err := globalDocumentStore.Get(c.Param("id"))
	if err != nil {
		return writeDocumentError(c, err)
	}

	unlockSHA := lockDocumentSHA(doc.SHA256)
	defer unlockSHA()

	sha, err := globalDocumentStore.Delete(doc.ID)
	if err != nil {
		return writeDocumentError(c, err)
	}

	path := filepath.Join(documentsDirPath(), sha)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		log.Printf("documents: delete: failed to remove file %s: %v", path, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to remove document file"})
	}
	return c.NoContent(http.StatusNoContent)
}

// ── POST /api/documents/:id/reindex ───────────────────────────────────────

// reindexDocumentHandler is the operator's explicit "index this again"
// action, which ADR 0106 treats as consent the same way uploading while
// Mate is on does. Readiness is re-checked at the moment of the call - not
// reused from whatever the document's enrich column held from its original
// upload - since the operator may have turned Mate on (or off) since.
func reindexDocumentHandler(c echo.Context) error {
	id := c.Param("id")

	if _, err := globalDocumentStore.Get(id); err != nil {
		return writeDocumentError(c, err)
	}

	readiness, _, err := checkAssistantReadiness(assistantSettingsPath())
	if err != nil {
		log.Printf("documents: reindex: check assistant readiness: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	// documentEnrichReadinessProblem, not readiness.Problem directly: this
	// consent decision also requires a document model, a document-only
	// requirement that must never affect chat's own readiness.
	problem := documentEnrichReadinessProblem(readiness)
	enrich := problem == ""

	if err := globalDocumentStore.MarkReindex(id, enrich); err != nil {
		return writeDocumentError(c, err)
	}

	doc, err := globalDocumentStore.Get(id)
	if err != nil {
		return writeDocumentError(c, err)
	}

	wakeDocumentIndexer()

	return c.JSON(http.StatusOK, map[string]any{
		"document": toDocumentJSON(doc),
		"enrich":   enrich,
		"problem":  problem,
	})
}

// ── POST /api/documents/move ──────────────────────────────────────────────

func moveDocumentsHandler(c echo.Context) error {
	var body struct {
		IDs      []string `json:"ids"`
		FolderID *string  `json:"folder_id"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if len(body.IDs) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "ids is required"})
	}
	if err := globalDocumentStore.MoveDocuments(body.IDs, body.FolderID); err != nil {
		return writeDocumentError(c, err)
	}
	// MoveDocuments rebuilds every moved document's meta chunk (its folder
	// path text changed), cascading away their old vectors the same way
	// patchDocumentHandler's PatchDocument call does above - wake the
	// indexer so re-embedding starts now rather than at the next unrelated
	// upload/reindex.
	wakeDocumentIndexer()
	return c.NoContent(http.StatusNoContent)
}

// ── GET /api/documents/embeddings, POST .../backfill (E1c) ──────────────

// documentEmbeddingsStatusJSON is GET /api/documents/embeddings' response
// shape. Enabled is false exactly when Problem is non-empty - a blank
// assistant.embedding_model or a chat-level readiness Problem
// (documentEmbedReadinessProblem, documents_embed.go) - so a caller can
// branch on Enabled alone without also having to check Problem's presence.
type documentEmbeddingsStatusJSON struct {
	Enabled    bool                    `json:"enabled"`
	Model      string                  `json:"model"`
	Dimensions int                     `json:"dimensions"`
	Problem    string                  `json:"problem,omitempty"`
	Counts     documentEmbeddingCounts `json:"counts"`
	Backfill   documentBackfillStatus  `json:"backfill"`
}

// GET /api/documents/embeddings
func documentsEmbeddingsStatusHandler(c echo.Context) error {
	readiness, _, err := checkAssistantReadiness(assistantSettingsPath())
	if err != nil {
		log.Printf("documents: embeddings status: check assistant readiness: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	problem := documentEmbedReadinessProblem(readiness)

	counts, err := globalDocumentStore.EmbeddingCounts(readiness.EmbeddingModel, readiness.EmbeddingDimensions)
	if err != nil {
		return writeDocumentError(c, err)
	}

	return c.JSON(http.StatusOK, documentEmbeddingsStatusJSON{
		Enabled:    problem == "",
		Model:      readiness.EmbeddingModel,
		Dimensions: readiness.EmbeddingDimensions,
		Problem:    problem,
		Counts:     counts,
		Backfill:   currentDocumentBackfillStatus(),
	})
}

// documentEmbeddingsBackfillDryRunJSON is the ?dry_run=1 response shape:
// the same library-wide counts the status endpoint reports, plus a token
// estimate - nothing here starts any work.
type documentEmbeddingsBackfillDryRunJSON struct {
	Counts documentEmbeddingCounts `json:"counts"`
	// TokensEstimate is CharsPending/4 - the usual rough
	// characters-per-token ratio for English text. It is an estimate to
	// give the operator a sense of scale before they click the button, not
	// a quote: the price per token belongs to the model, not to this code,
	// so no dollar figure is computed here.
	TokensEstimate int `json:"tokens_estimate"`
}

// documentEmbeddingsBackfillJSON is the non-dry-run response shape, both on
// success (200) and on an already-running conflict (409).
type documentEmbeddingsBackfillJSON struct {
	Backfill documentBackfillStatus `json:"backfill"`
}

// POST /api/documents/embeddings/backfill[?dry_run=1]
func documentsEmbeddingsBackfillHandler(c echo.Context) error {
	readiness, _, err := checkAssistantReadiness(assistantSettingsPath())
	if err != nil {
		log.Printf("documents: embeddings backfill: check assistant readiness: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	if parseDocumentBool(c.QueryParam("dry_run")) {
		counts, err := globalDocumentStore.EmbeddingCounts(readiness.EmbeddingModel, readiness.EmbeddingDimensions)
		if err != nil {
			return writeDocumentError(c, err)
		}
		return c.JSON(http.StatusOK, documentEmbeddingsBackfillDryRunJSON{
			Counts:         counts,
			TokensEstimate: counts.CharsPending / 4,
		})
	}

	if problem := documentEmbedReadinessProblem(readiness); problem != "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": problem})
	}

	if documentIndexerStartBackfill == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "document indexer not available"})
	}
	if err := documentIndexerStartBackfill(); err != nil {
		return c.JSON(http.StatusConflict, map[string]any{
			"error":    err.Error(),
			"backfill": currentDocumentBackfillStatus(),
		})
	}
	return c.JSON(http.StatusOK, documentEmbeddingsBackfillJSON{Backfill: currentDocumentBackfillStatus()})
}

// ── document-folders ───────────────────────────────────────────────────────

// fetchDocumentFolder reads back one folder by id, reusing FolderPath
// (documents_store.go) rather than adding a dedicated single-folder getter:
// FolderPath's root-first chain always ends with id itself.
func fetchDocumentFolder(id string) (documentFolder, error) {
	path, err := globalDocumentStore.FolderPath(id)
	if err != nil {
		return documentFolder{}, err
	}
	return path[len(path)-1], nil
}

// GET /api/document-folders?parent=
func listDocumentFoldersHandler(c echo.Context) error {
	parentRaw := strings.TrimSpace(c.QueryParam("parent"))
	var parentID *string
	if parentRaw != "" {
		parentID = &parentRaw
	}

	folders, docs, err := globalDocumentStore.ListFolder(parentID)
	if err != nil {
		return writeDocumentError(c, err)
	}

	path := []documentFolder{}
	if parentID != nil {
		path, err = globalDocumentStore.FolderPath(*parentID)
		if err != nil {
			return writeDocumentError(c, err)
		}
	}

	if folders == nil {
		folders = []documentFolder{}
	}
	docsJSON := make([]documentJSON, len(docs))
	for i, d := range docs {
		docsJSON[i] = toDocumentJSON(d)
	}

	return c.JSON(http.StatusOK, map[string]any{
		"path":      path,
		"folders":   folders,
		"documents": docsJSON,
	})
}

// POST /api/document-folders
func createDocumentFolderHandler(c echo.Context) error {
	var body struct {
		Name     string  `json:"name"`
		ParentID *string `json:"parent_id"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}

	folder, err := globalDocumentStore.CreateFolder(body.Name, body.ParentID)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusCreated, folder)
}

// PATCH /api/document-folders/:id
//
// A rename and a move in the same body commit together through one call to
// PatchFolder (documents_store.go): before that method existed, this
// handler committed RenameFolder in its own transaction and then called
// MoveFolder, so a move rejected as a cycle, a name collision or a missing
// parent returned 409/404 with the rename already applied.
func patchDocumentFolderHandler(c echo.Context) error {
	id := c.Param("id")

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(c.Request().Body).Decode(&raw); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if len(raw) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "no patch fields provided"})
	}

	var name *string
	if v, ok := raw["name"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid name"})
		}
		name = &s
	}

	var parentID *string
	var moveParent bool
	if v, ok := raw["parent_id"]; ok {
		moveParent = true
		if string(v) != "null" {
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid parent_id"})
			}
			parentID = &s
		}
	}

	if name != nil || moveParent {
		if err := globalDocumentStore.PatchFolder(id, name, parentID, moveParent); err != nil {
			return writeDocumentError(c, err)
		}
		// A rename or a move rebuilds the meta chunk of every document in
		// this folder's subtree (rebuildMetaChunksUnderFolderTx, called
		// unconditionally once PatchFolder gets this far) - their folder
		// path text changed, cascading away their old vectors the same way
		// patchDocumentHandler's own call does. Unlike PatchFolder, plain
		// CreateFolder/DeleteFolder never rebuild any document's meta chunk
		// (a new folder starts empty; DeleteFolder refuses a non-empty
		// one), so neither of those needs this wake.
		wakeDocumentIndexer()
	}

	folder, err := fetchDocumentFolder(id)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, folder)
}

// DELETE /api/document-folders/:id
func deleteDocumentFolderHandler(c echo.Context) error {
	if err := globalDocumentStore.DeleteFolder(c.Param("id")); err != nil {
		return writeDocumentError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}
