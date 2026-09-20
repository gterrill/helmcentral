package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mustCreateTestNote posts body (and, when non-empty, title) to
// createNoteHandler directly - the same "call the handler, not the router"
// idiom every other *_handlers_test.go file in this package uses - and
// returns the created document.
func mustCreateTestNote(t *testing.T, body, title string) documentJSON {
	t.Helper()
	reqBody := `{"body":` + jsonString(body)
	if title != "" {
		reqBody += `,"title":` + jsonString(title)
	}
	reqBody += `}`

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/notes", reqBody, "")
	if err := createNoteHandler(c); err != nil {
		t.Fatalf("createNoteHandler returned error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Document documentJSON `json:"document"`
		Body     string       `json:"body"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}
	return resp.Document
}

// jsonString marshals s as a JSON string literal - a tiny helper so the
// handwritten request bodies above don't have to hand-escape quotes or
// newlines in test fixture text.
func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// ── POST /api/notes ──────────────────────────────────────────────────────

func TestCreateNoteHandler_CreatesWithBlobOnDiskAtReturnedSHA(t *testing.T) {
	withTestDocumentStore(t)

	doc := mustCreateTestNote(t, "1. Warm up for five minutes\n2. Bring on load gradually", "Genset start-up")

	if doc.Kind != "note" {
		t.Fatalf("expected kind=note, got %q", doc.Kind)
	}
	if doc.MIME != "text/markdown" {
		t.Fatalf("expected mime=text/markdown, got %q", doc.MIME)
	}
	if doc.Title != "Genset start-up" {
		t.Fatalf("expected the given title to be used, got %q", doc.Title)
	}
	if doc.NoteType != noteTypeProcedure {
		t.Fatalf("expected the classifier to pick %q, got %q", noteTypeProcedure, doc.NoteType)
	}
	if doc.NoteTypeSource != "auto" {
		t.Fatalf("expected note_type_source=auto when type wasn't given, got %q", doc.NoteTypeSource)
	}

	blobPath := filepath.Join(documentsDirPath(), doc.SHA256)
	raw, err := os.ReadFile(blobPath)
	if err != nil {
		t.Fatalf("expected a blob on disk at the returned sha256: %v", err)
	}
	meta, body, err := parseNoteFile(raw)
	if err != nil {
		t.Fatalf("parseNoteFile on the stored blob: %v", err)
	}
	if meta.ID != doc.ID {
		t.Fatalf("expected the frontmatter id to equal documents.id: frontmatter=%q document=%q", meta.ID, doc.ID)
	}
	if body != "1. Warm up for five minutes\n2. Bring on load gradually\n" {
		t.Fatalf("unexpected stored body: %q", body)
	}
}

func TestCreateNoteHandler_DerivesTitleAndTypeWhenOmitted(t *testing.T) {
	withTestDocumentStore(t)

	doc := mustCreateTestNote(t, "# Genset start-up\n1. Warm up\n2. Bring on load", "")
	if doc.Title != "Genset start-up" {
		t.Fatalf("expected the derived title from the ATX heading, got %q", doc.Title)
	}
}

func TestCreateNoteHandler_EmptyBodyReturns400(t *testing.T) {
	withTestDocumentStore(t)

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/notes", `{"body":"   "}`, "")
	if err := createNoteHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an empty (whitespace-only) body, got %d: %s", rec.Code, rec.Body.String())
	}
	if entries := documentsDirEntries(t); len(entries) != 0 {
		t.Fatalf("expected no blob left behind on a 400, got %v", entries)
	}
}

func TestCreateNoteHandler_OversizeBodyReturns413(t *testing.T) {
	withTestDocumentStore(t)

	big := make([]byte, noteMaxBodyBytes+1)
	for i := range big {
		big[i] = 'a'
	}
	c, rec := newDocumentEchoContext(http.MethodPost, "/api/notes", `{"body":`+jsonString(string(big))+`}`, "")
	if err := createNoteHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for an oversize body, got %d: %s", rec.Code, rec.Body.String())
	}
	if entries := documentsDirEntries(t); len(entries) != 0 {
		t.Fatalf("expected no blob left behind on a 413, got %v", entries)
	}
}

func TestCreateNoteHandler_InvalidTypeReturns400(t *testing.T) {
	withTestDocumentStore(t)

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/notes", `{"body":"hello","type":"nonsense"}`, "")
	if err := createNoteHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an invalid explicit type, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ── GET /api/notes ───────────────────────────────────────────────────────

func TestListNotesHandler_FiledZeroIsTheInbox(t *testing.T) {
	withTestDocumentStore(t)

	unfiled := mustCreateTestNote(t, "loose thought", "")
	filed := mustCreateTestNote(t, "will be filed", "")

	folder, err := globalDocumentStore.CreateFolder("Contacts", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if _, err := globalDocumentStore.PatchDocument(filed.ID, nil, nil, nil, &folder.ID, true, nil); err != nil {
		t.Fatalf("PatchDocument (file into folder): %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/notes?filed=0", "", "")
	if err := listNotesHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Notes []documentJSON `json:"notes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Notes) != 1 || resp.Notes[0].ID != unfiled.ID {
		t.Fatalf("expected only the unfiled note in the inbox, got %+v", resp.Notes)
	}
}

func TestListNotesHandler_ExcludesUploadedFiles(t *testing.T) {
	withTestDocumentStore(t)
	mustInsertDocument(t, globalDocumentStore, "some-file-sha", "manual.pdf", nil)
	note := mustCreateTestNote(t, "a note", "")

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/notes", "", "")
	if err := listNotesHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	var resp struct {
		Notes []documentJSON `json:"notes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Notes) != 1 || resp.Notes[0].ID != note.ID {
		t.Fatalf("expected an uploaded file to never appear in /api/notes, got %+v", resp.Notes)
	}
}

// ── GET /api/notes/:id ───────────────────────────────────────────────────

func TestGetNoteHandler_ReturnsDocumentAndBody(t *testing.T) {
	withTestDocumentStore(t)
	created := mustCreateTestNote(t, "the body text", "My note")

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/notes/"+created.ID, "", created.ID)
	if err := getNoteHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Document documentJSON `json:"document"`
		Body     string       `json:"body"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Document.ID != created.ID {
		t.Fatalf("expected the same document id back, got %q", resp.Document.ID)
	}
	if resp.Body != "the body text\n" {
		t.Fatalf("unexpected body: %q", resp.Body)
	}
}

// TestGetNoteHandler_IncludesChecklistAndActiveRun is plan §4's extension
// of GET /api/notes/:id from {document, body} to {document, body,
// checklist, active_run} - one call serving both the reader and the
// runner. checklist is the note's current template items (no tick state);
// active_run is null until a run is actually started, then reflects it.
func TestGetNoteHandler_IncludesChecklistAndActiveRun(t *testing.T) {
	withTestDocumentStore(t)
	created := mustCreateTestNote(t, "- [ ] Seacocks open\n- [ ] Check bilge", "Shutdown")

	type getNoteResponse struct {
		Document  documentJSON      `json:"document"`
		Body      string            `json:"body"`
		Checklist []checklistItem   `json:"checklist"`
		ActiveRun *checklistRunView `json:"active_run"`
	}

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/notes/"+created.ID, "", created.ID)
	if err := getNoteHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	var resp getNoteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Checklist) != 2 {
		t.Fatalf("expected 2 checklist items, got %+v", resp.Checklist)
	}
	if resp.ActiveRun != nil {
		t.Fatalf("expected active_run to be null before any run is started, got %+v", resp.ActiveRun)
	}

	c, rec = newDocumentEchoContext(http.MethodPost, "/api/notes/"+created.ID+"/checklist-runs", "", created.ID)
	if err := createChecklistRunHandler(c); err != nil {
		t.Fatalf("createChecklistRunHandler: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 starting a fresh run, got %d: %s", rec.Code, rec.Body.String())
	}

	c, rec = newDocumentEchoContext(http.MethodGet, "/api/notes/"+created.ID, "", created.ID)
	if err := getNoteHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.ActiveRun == nil {
		t.Fatalf("expected active_run to be populated once a run is started")
	}
	if resp.ActiveRun.DocumentID != created.ID {
		t.Fatalf("expected active_run.document_id to be the note's id, got %q", resp.ActiveRun.DocumentID)
	}
	if resp.ActiveRun.Total != 2 {
		t.Fatalf("expected active_run.total 2, got %d", resp.ActiveRun.Total)
	}
}

func TestGetNoteHandler_KindFileIDReturns409(t *testing.T) {
	withTestDocumentStore(t)
	fileDoc := mustInsertDocument(t, globalDocumentStore, "a-file-sha", "manual.pdf", nil)

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/notes/"+fileDoc.ID, "", fileDoc.ID)
	if err := getNoteHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for a kind=file id against /api/notes/:id, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetNoteHandler_UnknownIDReturns404(t *testing.T) {
	withTestDocumentStore(t)
	c, rec := newDocumentEchoContext(http.MethodGet, "/api/notes/does-not-exist", "", "does-not-exist")
	if err := getNoteHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ── PATCH /api/notes/:id ─────────────────────────────────────────────────

func TestPatchNoteHandler_BodyEditRemovesOldBlobAndServesNewBytes(t *testing.T) {
	withTestDocumentStore(t)
	created := mustCreateTestNote(t, "original body", "A note")
	oldPath := filepath.Join(documentsDirPath(), created.SHA256)
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("expected the original blob to exist before the edit: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/notes/"+created.ID, `{"body":"updated body text"}`, created.ID)
	if err := patchNoteHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Document documentJSON `json:"document"`
		Body     string       `json:"body"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Document.SHA256 == created.SHA256 {
		t.Fatalf("expected the sha256 to change after a body edit")
	}
	if resp.Body != "updated body text\n" {
		t.Fatalf("unexpected served body: %q", resp.Body)
	}

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("expected the old blob removed after a successful edit, stat err=%v", err)
	}
	newPath := filepath.Join(documentsDirPath(), resp.Document.SHA256)
	newRaw, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("expected the new blob to exist on disk: %v", err)
	}
	_, newBody, err := parseNoteFile(newRaw)
	if err != nil {
		t.Fatalf("parseNoteFile on the new blob: %v", err)
	}
	if newBody != "updated body text\n" {
		t.Fatalf("unexpected new blob body: %q", newBody)
	}
}

func TestPatchNoteHandler_SameBodyIsANoOpOnDisk(t *testing.T) {
	withTestDocumentStore(t)
	created := mustCreateTestNote(t, "same body", "A note")

	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/notes/"+created.ID, `{"body":"same body"}`, created.ID)
	if err := patchNoteHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if entries := documentsDirEntries(t); len(entries) != 1 {
		t.Fatalf("expected exactly one blob on disk after a same-body patch, got %v", entries)
	}
}

func TestPatchNoteHandler_EmptyBodyReturns400(t *testing.T) {
	withTestDocumentStore(t)
	created := mustCreateTestNote(t, "hello", "")

	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/notes/"+created.ID, `{"body":"   "}`, created.ID)
	if err := patchNoteHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPatchNoteHandler_OversizeBodyReturns413(t *testing.T) {
	withTestDocumentStore(t)
	created := mustCreateTestNote(t, "hello", "")

	big := make([]byte, noteMaxBodyBytes+1)
	for i := range big {
		big[i] = 'a'
	}
	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/notes/"+created.ID, `{"body":`+jsonString(string(big))+`}`, created.ID)
	if err := patchNoteHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPatchNoteHandler_KindFileIDReturns409(t *testing.T) {
	withTestDocumentStore(t)
	fileDoc := mustInsertDocument(t, globalDocumentStore, "a-file-sha-2", "manual.pdf", nil)

	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/notes/"+fileDoc.ID, `{"title":"x"}`, fileDoc.ID)
	if err := patchNoteHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for a kind=file id against PATCH /api/notes/:id, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPatchNoteHandler_FolderOnlyMoveDoesNotTouchTheBlob(t *testing.T) {
	withTestDocumentStore(t)
	created := mustCreateTestNote(t, "file me", "")
	folder, err := globalDocumentStore.CreateFolder("Contacts", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/notes/"+created.ID, `{"folder_id":"`+folder.ID+`"}`, created.ID)
	if err := patchNoteHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Document documentJSON `json:"document"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Document.SHA256 != created.SHA256 {
		t.Fatalf("expected sha256 unchanged by a folder-only move: before=%q after=%q", created.SHA256, resp.Document.SHA256)
	}
	if resp.Document.FolderID == nil || *resp.Document.FolderID != folder.ID {
		t.Fatalf("expected the note filed into %q, got %v", folder.ID, resp.Document.FolderID)
	}
	if entries := documentsDirEntries(t); len(entries) != 1 {
		t.Fatalf("expected still exactly one blob on disk after a folder-only move, got %v", entries)
	}
}

// TestPatchNoteHandler_SortIndexLandsAtomicallyWithFolderMove pins plan
// §4's "promotion gets no endpoint" - filing a note into a manual at a
// given position is this exact PATCH, folder_id and sort_index together,
// not a dedicated manuals endpoint. Neither field touches the note's
// rendered frontmatter, so the blob must not change either.
func TestPatchNoteHandler_SortIndexLandsAtomicallyWithFolderMove(t *testing.T) {
	withTestDocumentStore(t)
	created := mustCreateTestNote(t, "file me at a position", "")
	manual, err := globalDocumentStore.CreateManual("Operations Manual")
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/notes/"+created.ID,
		`{"folder_id":"`+manual.ID+`","sort_index":3}`, created.ID)
	if err := patchNoteHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Document documentJSON `json:"document"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Document.SHA256 != created.SHA256 {
		t.Fatalf("expected sha256 unchanged: before=%q after=%q", created.SHA256, resp.Document.SHA256)
	}
	if resp.Document.FolderID == nil || *resp.Document.FolderID != manual.ID {
		t.Fatalf("expected the note filed into %q, got %v", manual.ID, resp.Document.FolderID)
	}
	if resp.Document.SortIndex != 3 {
		t.Fatalf("expected sort_index 3, got %d", resp.Document.SortIndex)
	}
}

func TestPatchNoteHandler_TypeOverrideSetsOperatorSource(t *testing.T) {
	withTestDocumentStore(t)
	created := mustCreateTestNote(t, "call Dave at 555-0142", "")
	if created.NoteType != noteTypeContact {
		t.Fatalf("expected the classifier to pick contact, got %q", created.NoteType)
	}

	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/notes/"+created.ID, `{"type":"quirk"}`, created.ID)
	if err := patchNoteHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Document documentJSON `json:"document"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Document.NoteType != "quirk" || resp.Document.NoteTypeSource != "operator" {
		t.Fatalf("expected type=quirk source=operator, got type=%q source=%q", resp.Document.NoteType, resp.Document.NoteTypeSource)
	}

	// The override must also have been rendered into the new blob's own
	// frontmatter, since type is part of what renderNoteFile serialises.
	raw, err := os.ReadFile(filepath.Join(documentsDirPath(), resp.Document.SHA256))
	if err != nil {
		t.Fatalf("read new blob: %v", err)
	}
	meta, _, err := parseNoteFile(raw)
	if err != nil {
		t.Fatalf("parseNoteFile: %v", err)
	}
	if meta.Type != "quirk" {
		t.Fatalf("expected the new blob's frontmatter type to be %q, got %q", "quirk", meta.Type)
	}
}

func TestPatchNoteHandler_UnknownFieldsOnlyReturns400(t *testing.T) {
	withTestDocumentStore(t)
	created := mustCreateTestNote(t, "hello", "")

	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/notes/"+created.ID, `{}`, created.ID)
	if err := patchNoteHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an empty patch body, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestUpdateNoteBody_StoreFailureLeavesNoOrphanBlob injects a store failure
// by pre-seeding another document row whose sha256 exactly matches what
// this PATCH's rendered bytes will hash to - ReplaceNoteBody's own
// collision check (notes_store.go, plan §1's residual UUID-collision case)
// then rejects the edit, and the handler must remove the blob it had
// already written under the (still-held) new-sha lock before returning
// the error, rather than leaving it behind with no row pointing at it.
func TestUpdateNoteBody_StoreFailureLeavesNoOrphanBlob(t *testing.T) {
	withTestDocumentStore(t)
	created := mustCreateTestNote(t, "original body", "A note")

	got, err := globalDocumentStore.Get(created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	newBody := "this body will collide"
	rendered := renderNoteFile(noteFileMeta{ID: got.ID, Title: got.Title, Type: got.NoteType, Tags: got.OperatorTags, Created: got.CreatedAt}, newBody)
	collidingSHA := sha256Hex(rendered)

	// Plant an unrelated row (and its own, unrelated, on-disk file) that
	// already owns collidingSHA.
	if err := os.WriteFile(filepath.Join(documentsDirPath(), collidingSHA), []byte("unrelated content"), 0o644); err != nil {
		t.Fatalf("write colliding fixture file: %v", err)
	}
	colliding, err := globalDocumentStore.Insert(document{SHA256: collidingSHA, Filename: "other.bin", MIME: "application/octet-stream"})
	if err != nil {
		t.Fatalf("Insert colliding document: %v", err)
	}

	before := documentsDirEntries(t) // the note's own blob + the colliding fixture file
	if len(before) != 2 {
		t.Fatalf("expected exactly 2 files on disk before the patch, got %v", before)
	}

	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/notes/"+created.ID, `{"body":`+jsonString(newBody)+`}`, created.ID)
	if err := patchNoteHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 on a sha collision, got %d: %s", rec.Code, rec.Body.String())
	}

	after := documentsDirEntries(t)
	if len(after) != len(before) {
		t.Fatalf("expected the same %d files on disk after the rejected edit (no orphan left behind), got %d: %v", len(before), len(after), after)
	}

	// The note's own row and blob must be exactly as they were.
	stillCurrent, err := globalDocumentStore.Get(created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stillCurrent.SHA256 != created.SHA256 {
		t.Fatalf("expected the note's sha256 unchanged after a rejected collision, got %q", stillCurrent.SHA256)
	}
	if _, err := os.Stat(filepath.Join(documentsDirPath(), created.SHA256)); err != nil {
		t.Fatalf("expected the note's original blob to still exist: %v", err)
	}

	// The colliding row's own file must be completely untouched - not
	// merely present, but never overwritten by the rejected edit's bytes.
	// A naive "write the new blob, then discover the collision" ordering
	// would rename this edit's temp file straight over the existing
	// destination path (os.Rename replaces an existing file) before ever
	// reaching ReplaceNoteBody's own check.
	collidedContent, err := os.ReadFile(filepath.Join(documentsDirPath(), collidingSHA))
	if err != nil {
		t.Fatalf("expected the colliding row's file to still exist: %v", err)
	}
	if string(collidedContent) != "unrelated content" {
		t.Fatalf("expected the colliding row's file content untouched, got %q", collidedContent)
	}
	_ = colliding
}

// TestUpdateNoteBody_UnlinkFailureStillReturns200 forces the old blob's
// removal (edit protocol step 5) to fail by replacing it with a
// non-empty DIRECTORY at the same path - os.Remove refuses a non-empty
// directory ("directory not empty"), the same shape of failure an
// EACCES or a hostile symlink would produce, without needing a
// filesystem-permission trick that wouldn't run the same way under every
// CI user. The edit itself must still succeed: the database row is
// already correct once ReplaceNoteBody commits, and the failed removal is
// logged, not surfaced as a request failure (see patchNoteHandler's own
// comment on this exact step).
func TestUpdateNoteBody_UnlinkFailureStillReturns200(t *testing.T) {
	withTestDocumentStore(t)
	created := mustCreateTestNote(t, "original body", "A note")

	oldPath := filepath.Join(documentsDirPath(), created.SHA256)
	if err := os.Remove(oldPath); err != nil {
		t.Fatalf("remove the real blob to replace it with a directory: %v", err)
	}
	if err := os.Mkdir(oldPath, 0o755); err != nil {
		t.Fatalf("mkdir at the old blob's path: %v", err)
	}
	if err := os.WriteFile(filepath.Join(oldPath, "keepme"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write a file inside the directory (so os.Remove sees it as non-empty): %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/notes/"+created.ID, `{"body":"updated content"}`, created.ID)
	if err := patchNoteHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 even though the old blob's removal failed, got %d: %s", rec.Code, rec.Body.String())
	}

	info, err := os.Stat(oldPath)
	if err != nil || !info.IsDir() {
		t.Fatalf("expected the directory standing in for the old blob to remain (removal failed as intended), err=%v", err)
	}

	var resp struct {
		Document documentJSON `json:"document"`
		Body     string       `json:"body"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Body != "updated content\n" {
		t.Fatalf("unexpected served body: %q", resp.Body)
	}
	newPath := filepath.Join(documentsDirPath(), resp.Document.SHA256)
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("expected the new blob to exist on disk despite the old one's failed removal: %v", err)
	}

	// The row itself must show the edit as fully committed.
	got, err := globalDocumentStore.Get(created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SHA256 != resp.Document.SHA256 {
		t.Fatalf("expected the row's sha256 to match the response, row=%q response=%q", got.SHA256, resp.Document.SHA256)
	}
}

// TestSweepDocumentsDir_ReportsASupersededNoteBlobAsAnOrphan exercises
// exactly the scenario the edit protocol's step 5 leaves behind when a
// blob removal fails (or, as here, is skipped outright): a file on disk
// that no row's sha256 points at anymore, because ReplaceNoteBody moved
// the row on to a new hash without anything removing the old file. No
// sweep code needed changing for this (plan §1) - sweepDocumentsDir
// already reports exactly this mismatch, and this test is what proves it.
func TestSweepDocumentsDir_ReportsASupersededNoteBlobAsAnOrphan(t *testing.T) {
	withTestDocumentStore(t)
	created := mustCreateTestNote(t, "original body", "A note")

	// Move the row on to a new sha256 via the store directly, bypassing
	// the handler entirely - so, unlike a normal edit, neither a new blob
	// nor a removal of the old one ever happens. The old blob
	// (created.SHA256) is left on disk with nothing pointing at it; the
	// new one (never written) is missing.
	if _, err := globalDocumentStore.ReplaceNoteBody(created.ID, "sha-nobody-wrote", 10); err != nil {
		t.Fatalf("ReplaceNoteBody: %v", err)
	}

	result, err := sweepDocumentsDir(documentsDirPath(), globalDocumentStore)
	if err != nil {
		t.Fatalf("sweepDocumentsDir: %v", err)
	}
	if result.OrphanFiles != 1 {
		t.Fatalf("expected the superseded blob reported as 1 orphan file, got %d", result.OrphanFiles)
	}
	if result.MissingFiles != 1 {
		t.Fatalf("expected the row's new (never-written) sha256 reported as 1 missing file, got %d", result.MissingFiles)
	}

	// The sweep only reports - it must not have deleted the orphan.
	if _, err := os.Stat(filepath.Join(documentsDirPath(), created.SHA256)); err != nil {
		t.Fatalf("expected the orphaned blob left on disk (sweep reports, never deletes a content file): %v", err)
	}
}

// ── PATCH /api/notes/:id - pinned (plan §8: the operator-facing way to
// pin/unpin a note, without which the pinned-notes prompt feature is
// unreachable) ──────────────────────────────────────────────────────────

func TestPatchNoteHandler_PinnedSetsFlagWithoutTouchingTheBlob(t *testing.T) {
	withTestDocumentStore(t)
	created := mustCreateTestNote(t, "flick the switch off", "Shower drain")

	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/notes/"+created.ID, `{"pinned":true}`, created.ID)
	if err := patchNoteHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Document documentJSON `json:"document"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.Document.Pinned {
		t.Fatalf("expected pinned=true in the response")
	}
	if resp.Document.SHA256 != created.SHA256 {
		t.Fatalf("expected sha256 unchanged (pinned isn't part of the note's rendered frontmatter): before=%q after=%q", created.SHA256, resp.Document.SHA256)
	}
}

func TestPatchNoteHandler_PinnedFalseUnpins(t *testing.T) {
	withTestDocumentStore(t)
	created := mustCreateTestNote(t, "flick the switch off", "Shower drain")
	if err := globalDocumentStore.SetNotePinned(created.ID, true); err != nil {
		t.Fatalf("SetNotePinned: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/notes/"+created.ID, `{"pinned":false}`, created.ID)
	if err := patchNoteHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Document documentJSON `json:"document"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Document.Pinned {
		t.Fatalf("expected pinned=false in the response")
	}
}

// ── POST /api/notes/classify/backfill ───────────────────────────────────

func TestNotesClassifyBackfillHandler_DryRunSetsNothing(t *testing.T) {
	withTestDocumentStore(t)
	mustCreateTestNote(t, "call the yard on 555-0142", "")

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/notes/classify/backfill?dry_run=1", "", "")
	if err := notesClassifyBackfillHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// The note above was already classified at creation time (note_type_
	// source="auto"), so it's still a candidate - dry_run must report it
	// without having changed anything.
	if resp.Count != 1 {
		t.Fatalf("expected count=1, got %d", resp.Count)
	}
}

func TestNotesClassifyBackfillHandler_SkipsOperatorTouchesAutoAndBlank(t *testing.T) {
	withTestDocumentStore(t)

	// An operator override must survive the backfill untouched.
	operatorNote := mustCreateTestNote(t, "call the yard on 555-0142", "")
	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/notes/"+operatorNote.ID, `{"type":"quirk"}`, operatorNote.ID)
	if err := patchNoteHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("setup PATCH type=quirk failed: err=%v code=%d body=%s", err, rec.Code, rec.Body.String())
	}

	// A pre-classifier note: note_type_source='' (simulated directly - no
	// API path leaves a note in this state today, but it's exactly the
	// historical row shape the backfill exists for).
	blank, err := globalDocumentStore.InsertNote(document{SHA256: "blank-note-sha", Filename: "blank.md", MIME: "text/markdown"})
	if err != nil {
		t.Fatalf("InsertNote: %v", err)
	}
	blankPath := filepath.Join(documentsDirPath(), "blank-note-sha")
	if err := os.WriteFile(blankPath, renderNoteFile(noteFileMeta{ID: blank.ID, Title: "Blank", Created: blank.CreatedAt}, "call Dave at 555-0142"), 0o644); err != nil {
		t.Fatalf("write blank note blob: %v", err)
	}

	c2, rec2 := newDocumentEchoContext(http.MethodPost, "/api/notes/classify/backfill", "", "")
	if err := notesClassifyBackfillHandler(c2); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec2.Code, rec2.Body.String())
	}

	updatedOperator, err := globalDocumentStore.Get(operatorNote.ID)
	if err != nil {
		t.Fatalf("Get operator note: %v", err)
	}
	if updatedOperator.NoteType != "quirk" || updatedOperator.NoteTypeSource != "operator" {
		t.Fatalf("expected the operator override untouched, got type=%q source=%q", updatedOperator.NoteType, updatedOperator.NoteTypeSource)
	}

	updatedBlank, err := globalDocumentStore.Get(blank.ID)
	if err != nil {
		t.Fatalf("Get blank note: %v", err)
	}
	if updatedBlank.NoteType != noteTypeContact || updatedBlank.NoteTypeSource != "auto" {
		t.Fatalf("expected the blank-source note reclassified to contact/auto, got type=%q source=%q", updatedBlank.NoteType, updatedBlank.NoteTypeSource)
	}
}

// ── POST /api/notes/enrich/backfill ─────────────────────────────────────

func TestNotesEnrichBackfillHandler_DryRunReturnsCountAndEstimateWithoutSettingAnything(t *testing.T) {
	withTestDocumentStore(t)
	created := mustCreateTestNote(t, "ring Dave about the mooring, 0412 555 555", "")

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/notes/enrich/backfill?dry_run=1", "", "")
	if err := notesEnrichBackfillHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Count          int `json:"count"`
		TokensEstimate int `json:"tokens_estimate"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Count != 1 {
		t.Fatalf("expected count=1, got %d", resp.Count)
	}

	got, err := globalDocumentStore.Get(created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Enrich {
		t.Fatalf("expected the dry run to set nothing, but enrich is now true")
	}
}

func TestNotesEnrichBackfillHandler_NotReadyReturns400AndSetsNothing(t *testing.T) {
	withTestDocumentStore(t)
	withTestSecretsStore(t) // an empty-but-real store: no OPENROUTER_API_KEY set
	created := mustCreateTestNote(t, "ring Dave about the mooring, 0412 555 555", "")
	// No settings fixture either - assistant readiness fails
	// (checkAssistantReadiness's own "assistant is switched off" branch).

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/notes/enrich/backfill", "", "")
	if err := notesEnrichBackfillHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	got, err := globalDocumentStore.Get(created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Enrich {
		t.Fatalf("expected a rejected (not-ready) backfill to set nothing")
	}
}

func TestNotesEnrichBackfillHandler_RunSetsEnrichAndWakesIndexer(t *testing.T) {
	withTestDocumentStore(t)
	secrets := withTestSecretsStore(t)
	if err := secrets.Set("OPENROUTER_API_KEY", "sk-test"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	t.Setenv("SETTINGS_FILE", writeDocumentEmbeddingsSettingsFixture(t, "openai/text-embedding-3-small", 512))
	created := mustCreateTestNote(t, "ring Dave about the mooring, 0412 555 555", "")

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/notes/enrich/backfill", "", "")
	if err := notesEnrichBackfillHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Count != 1 {
		t.Fatalf("expected count=1, got %d", resp.Count)
	}

	got, err := globalDocumentStore.Get(created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.Enrich {
		t.Fatalf("expected the note's enrich flag now set")
	}
}

// A note's title and tags are rendered into its YAML frontmatter, so they
// are FILE BYTES, not just database columns - and only `body` was ever
// capped. A one-byte body with a megabyte title passes noteMaxBodyBytes and
// writes a megabyte blob into DOCUMENTS_DIR, with a fresh sha256 every time,
// so it is additive: repeat it and the disk fills one request at a time.
//
// That is precisely the bypass noteMaxBodyBytes' own comment says the cap
// exists to prevent, so the check has to be on what actually reaches disk.
func TestCreateNote_RejectsANoteWhoseRenderedBytesExceedTheCap(t *testing.T) {
	withTestDocumentStore(t)

	body := map[string]any{
		"body":  "x",
		"title": strings.Repeat("t", noteMaxBodyBytes+1),
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/notes", string(raw), "")
	if err := createNoteHandler(c); err != nil {
		t.Fatalf("createNoteHandler: %v", err)
	}

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413 - an oversized title is an oversized note", rec.Code)
	}
}
