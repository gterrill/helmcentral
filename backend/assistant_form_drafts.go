package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// Form drafts (ADR 0165). When Mate fills in a PDF form the result is not a
// document. It is a draft that rides under Mate's reply as a card, and the
// operator reads it, then saves it to Documents or dismisses it. Until the
// operator saves, nothing about it is in the document library, so a form
// filled wrongly leaves no trace there.

const (
	formDraftPending   = "draft"
	formDraftSaved     = "saved"
	formDraftDismissed = "dismissed"

	// A draft that never reached a message (the run failed or was cancelled
	// after fill_form made it) is swept after this long.
	formDraftOrphanAge = 24 * time.Hour
)

var (
	errFormDraftNotFound  = errors.New("form draft not found")
	errFormDraftDismissed = errors.New("this filled-in form was dismissed and can no longer be saved")
	errFormDraftSaved     = errors.New("this filled-in form was already saved and can no longer be dismissed")
	errFormDraftTitle     = errors.New("give the document a title")
)

// assistantFormDraft is one card under an assistant message.
type assistantFormDraft struct {
	ID               string `json:"id"`
	MessageID        string `json:"message_id,omitempty"`
	SourceDocumentID string `json:"source_document_id"`
	Title            string `json:"title"`
	Folder           string `json:"folder"`
	Filename         string `json:"filename"`
	PageCount        int    `json:"page_count"`
	Status           string `json:"status"`
	// DocumentID is the document the operator saved it as.
	DocumentID string    `json:"document_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

func createAssistantFormDraftsSchema(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS assistant_form_drafts (
			id                 TEXT PRIMARY KEY,
			message_id         TEXT REFERENCES messages(id) ON DELETE CASCADE,
			position           INTEGER NOT NULL DEFAULT 0,
			source_document_id TEXT NOT NULL,
			title              TEXT NOT NULL,
			folder             TEXT NOT NULL DEFAULT '',
			filename           TEXT NOT NULL,
			page_count         INTEGER NOT NULL DEFAULT 0,
			content            BLOB NOT NULL,
			status             TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'saved', 'dismissed')),
			document_id        TEXT NOT NULL DEFAULT '',
			created_at         INTEGER NOT NULL
		)`); err != nil {
		return fmt.Errorf("create assistant_form_drafts table: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS assistant_form_drafts_message ON assistant_form_drafts (message_id)`); err != nil {
		return fmt.Errorf("index assistant_form_drafts table: %w", err)
	}
	return nil
}

const formDraftColumns = `id, COALESCE(message_id, ''), source_document_id, title, folder, filename, page_count, status, document_id, created_at`

func scanFormDraft(row rowScanner) (assistantFormDraft, error) {
	var d assistantFormDraft
	var created int64
	if err := row.Scan(&d.ID, &d.MessageID, &d.SourceDocumentID, &d.Title, &d.Folder, &d.Filename, &d.PageCount, &d.Status, &d.DocumentID, &created); err != nil {
		return assistantFormDraft{}, err
	}
	d.CreatedAt = time.Unix(created, 0).UTC()
	return d, nil
}

// CreateFormDraft stores a filled-in form. It belongs to no message yet: the
// run attaches it when it saves the reply that carries it.
func (s *assistantStore) CreateFormDraft(d assistantFormDraft, content []byte) (assistantFormDraft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.Title) == "" || len(content) == 0 {
		return assistantFormDraft{}, errors.New("create form draft: id, title and content are required")
	}
	now := s.now()
	// Sweep what earlier runs left behind.
	if _, err := s.db.Exec(`DELETE FROM assistant_form_drafts WHERE message_id IS NULL AND created_at < ?`, now.Add(-formDraftOrphanAge).Unix()); err != nil {
		return assistantFormDraft{}, fmt.Errorf("sweep form drafts: %w", err)
	}
	if _, err := s.db.Exec(
		`INSERT INTO assistant_form_drafts (id, message_id, source_document_id, title, folder, filename, page_count, content, status, created_at)
		 VALUES (?, NULL, ?, ?, ?, ?, ?, ?, 'draft', ?)`,
		d.ID, d.SourceDocumentID, d.Title, d.Folder, d.Filename, d.PageCount, content, now.Unix()); err != nil {
		return assistantFormDraft{}, fmt.Errorf("create form draft: %w", err)
	}
	d.Status, d.CreatedAt, d.MessageID, d.DocumentID = formDraftPending, now, "", ""
	return d, nil
}

// attachFormDraftsTx ties drafts to the message that carries them, in the
// transaction that saves the message.
func attachFormDraftsTx(tx *sql.Tx, messageID string, drafts []assistantFormDraft) error {
	for i := range drafts {
		res, err := tx.Exec(
			`UPDATE assistant_form_drafts SET message_id = ?, position = ? WHERE id = ? AND message_id IS NULL`,
			messageID, i, drafts[i].ID)
		if err != nil {
			return fmt.Errorf("attach form draft %s: %w", drafts[i].ID, err)
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return fmt.Errorf("attach form draft %s: it is not an unattached draft", drafts[i].ID)
		}
		drafts[i].MessageID = messageID
	}
	return nil
}

func formDraftsForMessages(q sqlQueryer, messageIDs []string) (map[string][]assistantFormDraft, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(messageIDs))
	args := make([]any, len(messageIDs))
	for i, id := range messageIDs {
		placeholders[i], args[i] = "?", id
	}
	rows, err := q.Query(`SELECT `+formDraftColumns+` FROM assistant_form_drafts WHERE message_id IN (`+strings.Join(placeholders, ",")+`) ORDER BY message_id, position`, args...)
	if err != nil {
		return nil, fmt.Errorf("list message form drafts: %w", err)
	}
	defer rows.Close()
	out := map[string][]assistantFormDraft{}
	for rows.Next() {
		d, err := scanFormDraft(rows)
		if err != nil {
			return nil, fmt.Errorf("scan form draft: %w", err)
		}
		out[d.MessageID] = append(out[d.MessageID], d)
	}
	return out, rows.Err()
}

func (s *assistantStore) GetFormDraft(id string) (assistantFormDraft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := scanFormDraft(s.db.QueryRow(`SELECT `+formDraftColumns+` FROM assistant_form_drafts WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return assistantFormDraft{}, errFormDraftNotFound
	}
	if err != nil {
		return assistantFormDraft{}, fmt.Errorf("get form draft: %w", err)
	}
	return d, nil
}

// FormDraftContent is the PDF as it was filled in. A dismissed draft's bytes
// are gone.
func (s *assistantStore) FormDraftContent(id string) (assistantFormDraft, []byte, error) {
	d, err := s.GetFormDraft(id)
	if err != nil {
		return assistantFormDraft{}, nil, err
	}
	if d.Status == formDraftDismissed {
		return d, nil, errFormDraftDismissed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var content []byte
	if err := s.db.QueryRow(`SELECT content FROM assistant_form_drafts WHERE id = ?`, id).Scan(&content); err != nil {
		return assistantFormDraft{}, nil, fmt.Errorf("read form draft content: %w", err)
	}
	return d, content, nil
}

// DismissFormDraft discards a draft that has not been saved. Dismissing a
// dismissed one does nothing; a saved one is a document now and cannot be
// dismissed from here.
func (s *assistantStore) DismissFormDraft(id string) (assistantFormDraft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := scanFormDraft(s.db.QueryRow(`SELECT `+formDraftColumns+` FROM assistant_form_drafts WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return assistantFormDraft{}, errFormDraftNotFound
	}
	if err != nil {
		return assistantFormDraft{}, fmt.Errorf("get form draft: %w", err)
	}
	switch d.Status {
	case formDraftDismissed:
		return d, nil
	case formDraftSaved:
		return assistantFormDraft{}, errFormDraftSaved
	}
	res, err := s.db.Exec(`UPDATE assistant_form_drafts SET status = 'dismissed', content = x'' WHERE id = ? AND status = 'draft'`, id)
	if err != nil {
		return assistantFormDraft{}, fmt.Errorf("dismiss form draft: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		// Saved between the read and the write: it is a document now.
		return assistantFormDraft{}, errFormDraftSaved
	}
	d.Status = formDraftDismissed
	return d, nil
}

// markFormDraftSaved records the document, and the title and folder the
// operator saved it under, so a reloaded card says what was kept.
func (s *assistantStore) markFormDraftSaved(id, documentID, title, folder string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`UPDATE assistant_form_drafts SET status = 'saved', document_id = ?, title = ?, folder = ? WHERE id = ? AND status = 'draft'`, documentID, title, folder, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errFormDraftDismissed
	}
	return nil
}

// dismissFormDraftExclusive is Dismiss under the same lock Save holds, so the
// two cannot interleave: one wins whole and the other is refused.
func dismissFormDraftExclusive(asst *assistantStore, id string) (assistantFormDraft, error) {
	saveFormDraftMu.Lock()
	defer saveFormDraftMu.Unlock()
	return asst.DismissFormDraft(id)
}

// formDraftHistoryBlock tells Mate on a later turn what became of a draft.
func formDraftHistoryBlock(d assistantFormDraft) string {
	var state string
	switch d.Status {
	case formDraftSaved:
		state = "the operator saved it to Documents as document " + d.DocumentID
	case formDraftDismissed:
		state = "the operator dismissed it; it was not kept"
	default:
		state = "still waiting for the operator to save or dismiss it"
	}
	return fmt.Sprintf("\n\n[Filled-in form %q: %s]", d.Title, state)
}

// ── saving a draft as a document ────────────────────────────────────────

// saveFormDraftMu keeps two taps on Save from making two documents.
var saveFormDraftMu sync.Mutex

type formDraftSaveResult struct {
	Draft     assistantFormDraft
	Document  document
	Duplicate bool // the same bytes were already in the library
}

// ensureFolderPath finds or creates each folder of an "A/B/C" path and returns
// the last one's id, nil for the library root.
func ensureFolderPath(store *documentStore, path string) (*string, error) {
	var parent *string
	prefix := ""
	for _, seg := range strings.Split(path, "/") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		if prefix != "" {
			prefix += "/"
		}
		prefix += seg
		id, err := store.ResolveFolderPath(prefix)
		if errors.Is(err, errFolderNotFound) {
			f, cerr := store.CreateFolder(seg, parent)
			if cerr != nil {
				return nil, cerr
			}
			id = f.ID
		} else if err != nil {
			return nil, err
		}
		parent = &id
	}
	return parent, nil
}

// SaveFormDraft makes the document. It goes through the same file store, the
// same record and the same indexing as an upload, so the saved form is found
// by search like any other document. Saving again returns the same document.
func saveFormDraft(asst *assistantStore, docs *documentStore, id, title, folder string) (formDraftSaveResult, error) {
	saveFormDraftMu.Lock()
	defer saveFormDraftMu.Unlock()

	d, content, err := asst.FormDraftContent(id)
	if err != nil {
		return formDraftSaveResult{}, err
	}
	if d.Status == formDraftSaved && d.DocumentID != "" {
		doc, gerr := docs.Get(d.DocumentID)
		if gerr == nil {
			return formDraftSaveResult{Draft: d, Document: doc}, nil
		}
		if !errors.Is(gerr, errDocumentNotFound) {
			return formDraftSaveResult{}, gerr
		}
		// The saved document was deleted since. Saving again makes a new one.
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return formDraftSaveResult{}, errFormDraftTitle
	}

	folderID, err := ensureFolderPath(docs, folder)
	if err != nil {
		return formDraftSaveResult{}, err
	}
	enrich, _, err := documentEnrichFlag("form draft: save")
	if err != nil {
		return formDraftSaveResult{}, err
	}

	dir := documentsDirPath()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return formDraftSaveResult{}, fmt.Errorf("prepare document storage: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "upload-*.tmp")
	if err != nil {
		return formDraftSaveResult{}, fmt.Errorf("prepare the file: %w", err)
	}
	sum := sha256.New()
	if _, err := io.MultiWriter(tmp, sum).Write(content); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return formDraftSaveResult{}, fmt.Errorf("write the file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return formDraftSaveResult{}, fmt.Errorf("write the file: %w", err)
	}
	head := content
	if len(head) > 512 {
		head = head[:512]
	}
	up := uploadedFile{tmpPath: tmp.Name(), filename: d.Filename, size: int64(len(content)), sha: hex.EncodeToString(sum.Sum(nil)), head: head}

	var saved document
	var duplicate bool
	err = storeUploadedFile(dir, up,
		document{FolderID: folderID, Filename: d.Filename, Title: title, MIME: "application/pdf", SizeBytes: up.size, Enrich: enrich},
		func(existing document) error { saved, duplicate = existing, true; return nil },
		func(inserted document) error { saved = inserted; wakeDocumentIndexer(); return nil },
		func(err error) error { return err },
	)
	if err != nil {
		return formDraftSaveResult{}, err
	}
	if err := asst.markFormDraftSaved(id, saved.ID, title, folder); err != nil {
		return formDraftSaveResult{}, fmt.Errorf("record the saved document: %w", err)
	}
	d.Status, d.DocumentID, d.Title, d.Folder = formDraftSaved, saved.ID, title, folder
	return formDraftSaveResult{Draft: d, Document: saved, Duplicate: duplicate}, nil
}

// ── routes ──────────────────────────────────────────────────────────────

func writeFormDraftError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, errFormDraftNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, errFormDraftDismissed), errors.Is(err, errFormDraftSaved):
		return c.JSON(http.StatusConflict, map[string]string{"error": err.Error()})
	case errors.Is(err, errFormDraftTitle), errors.Is(err, errFolderNameInvalid), errors.Is(err, errFolderNameTaken):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	log.Printf("assistant: form draft: %v", err)
	return writeDocumentError(c, err)
}

// GET /api/assistant/form-drafts/:id/content
func formDraftContentHandler(c echo.Context) error {
	d, content, err := globalAssistantStore.FormDraftContent(c.Param("id"))
	if err != nil {
		return writeFormDraftError(c, err)
	}
	header := c.Response().Header()
	disposition := "inline"
	if c.QueryParam("download") == "1" {
		disposition = "attachment"
	}
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", "sandbox")
	header.Set("Cache-Control", "private, no-store")
	header.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": d.Filename}))
	return c.Blob(http.StatusOK, "application/pdf", content)
}

type formDraftSaveRequest struct {
	Title  string `json:"title"`
	Folder string `json:"folder"`
}

// POST /api/assistant/form-drafts/:id/save {title, folder}
func saveFormDraftHandler(c echo.Context) error {
	var req formDraftSaveRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	res, err := saveFormDraft(globalAssistantStore, globalDocumentStore, c.Param("id"), req.Title, req.Folder)
	if err != nil {
		return writeFormDraftError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"draft": res.Draft, "document": toDocumentJSON(res.Document), "duplicate": res.Duplicate})
}

// POST /api/assistant/form-drafts/:id/dismiss
func dismissFormDraftHandler(c echo.Context) error {
	d, err := dismissFormDraftExclusive(globalAssistantStore, c.Param("id"))
	if err != nil {
		return writeFormDraftError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"draft": d})
}
