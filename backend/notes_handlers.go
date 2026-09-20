package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// This file serves the notes feature's HTTP API (plan "Notes and the
// Boat's Manual", ADR 0114): POST/GET/PATCH /api/notes and GET
// /api/notes/:id. A note is a documents row with kind='note'
// (notes_store.go), so this reuses toDocumentJSON, writeDocumentError,
// lockDocumentSHA and the documentsDirPath() blob layout wholesale -
// documents_handlers.go is the model to read alongside this file, and this
// one only adds what's actually different about a note: the body lives in
// the request/response JSON directly (not a multipart file part), and an
// edit is a render-compare-swap dance (the "edit protocol", plan §1)
// rather than a plain metadata PATCH.

// noteMaxBodyBytes caps a note's body field on the JSON create/PATCH path
// (plan §4). maxTextExtractBytes (documents_extract.go) is 20 MB for an
// uploaded FILE - a deliberate act with its own multipart size limit
// (documentMaxUploadBytes) - but a note is typed or dictated at a helm
// screen. Without its own, much smaller cap, POST/PATCH /api/notes would
// be an unbounded upload channel hiding behind ordinary JSON, sidestepping
// documentMaxUploadBytes entirely.
const noteMaxBodyBytes = 1 << 20 // 1 MB

// noteMaxRequestBytes bounds the whole JSON request, envelope included, so
// an oversized one is refused while it is still being read rather than after
// it is in memory. Generous relative to the body cap: JSON escaping can
// inflate the body, and the exact limit is enforced on the rendered bytes.
const noteMaxRequestBytes = 4 << 20 // 4 MB

// noteRenderedTooLarge reports whether a rendered note exceeds the cap.
//
// The cap has to be on the RENDERED bytes, not on req.Body alone. Title and
// tags are written into the YAML frontmatter, so they are file content: a
// one-byte body with a megabyte title passed the body-only check and wrote a
// megabyte blob, with a fresh sha256 each time, which made it additive - the
// exact "unbounded upload channel hiding behind ordinary JSON" the constant's
// own comment says it exists to prevent.
func noteRenderedTooLarge(rendered []byte) bool {
	return len(rendered) > noteMaxBodyBytes
}

// limitNoteRequestBody bounds what a JSON note request can pull into memory
// BEFORE it is decoded.
//
// noteMaxBodyBytes alone does not do this: c.Bind and json.Decode read the
// whole request first, so the bytes are already resident by the time any cap
// runs, and peak RSS equals request size. A multi-gigabyte POST would
// OOM-kill the backend, which under ADR 0111 is the availability failure
// that matters more on this box than anything confidentiality-shaped.
//
// Same treatment, same reason, as uploadDocumentHandler's own
// http.MaxBytesReader (documents_handlers.go). The allowance is the body cap
// plus room for the rest of the JSON envelope and its escaping; the precise
// limit on what reaches disk is noteRenderedTooLarge, after decoding.
func limitNoteRequestBody(c echo.Context) {
	req := c.Request()
	req.Body = http.MaxBytesReader(c.Response(), req.Body, noteMaxRequestBytes)
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// readNoteBody reads doc's blob off disk and parses out just its body -
// the "current body" every handler below needs whenever a request doesn't
// supply a fresh one itself (PATCH's metadata-only path re-rendering with
// the existing body; every handler's own final response). A read or parse
// failure here means disk and database have drifted (the same condition
// documentContentHandler treats as a 500, documents_handlers.go), not a
// per-request problem the caller did anything to cause.
func readNoteBody(doc document) (string, error) {
	path := filepath.Join(documentsDirPath(), doc.SHA256)
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("note file missing on disk: %w", err)
	}
	_, body, err := parseNoteFile(raw)
	if err != nil {
		return "", fmt.Errorf("stored note file is malformed: %w", err)
	}
	return body, nil
}

// lockTwoDocumentSHAs locks BOTH a and b's per-sha256 locks
// (lockDocumentSHA, documents_handlers.go), always in lexicographic order
// regardless of which order the caller passes them in, and only once when
// a == b. This is a hazard new to the note edit protocol (plan §1, step
// 2): every existing sha-locking caller (uploadDocumentHandler,
// deleteDocumentHandler) only ever holds one hash at a time. Locking two
// hashes per call means two concurrent note edits can deadlock unless both
// acquire their pair in the SAME global order - if edit A's old sha equals
// edit B's new sha, and edit B's old sha equals edit A's new sha (each
// edit "crossing" into the other's territory), then "lock old, then lock
// new" would have A hold its old/B's-new while waiting for B's old/A's-new,
// and B doing the exact mirror image: a classic deadlock. Locking in a
// fixed order (lexicographically smaller hash first, always) makes that
// impossible - at most one of the two calls can ever be first in line for
// the smaller hash.
func lockTwoDocumentSHAs(a, b string) (unlockA, unlockB func()) {
	if a == b {
		unlock := lockDocumentSHA(a)
		return unlock, func() {}
	}
	first, second := a, b
	if second < first {
		first, second = second, first
	}
	unlockFirst := lockDocumentSHA(first)
	unlockSecond := lockDocumentSHA(second)
	if first == a {
		return unlockFirst, unlockSecond
	}
	return unlockSecond, unlockFirst
}

// getNoteOrError fetches id and confirms it is a note, the shared prelude
// of every handler below that takes an :id.
func getNoteOrError(id string) (document, error) {
	doc, err := globalDocumentStore.Get(id)
	if err != nil {
		return document{}, err
	}
	if doc.Kind != "note" {
		return document{}, errNotANote
	}
	return doc, nil
}

// ── GET /api/notes ───────────────────────────────────────────────────────

// listNotesHandler is GET /api/notes: kind='note' documents, newest first,
// with the Notes inbox's own filters (plan §4) - ?filed=0 narrows to
// unfiled notes (the inbox view itself), ?type= and ?tag= narrow further,
// and limit/offset reuse parseDocumentLimit/parseDocumentOffset exactly as
// GET /api/documents does. The response key is "notes" (plural), which is
// the one place in this codebase's JSON that word is allowed to mean the
// feature rather than the pre-existing operator-annotation column -
// documents.notes never appears in this file at all.
func listNotesHandler(c echo.Context) error {
	unfiledOnly := c.QueryParam("filed") == "0"
	noteType := strings.TrimSpace(c.QueryParam("type"))
	tag := strings.TrimSpace(c.QueryParam("tag"))
	limit := parseDocumentLimit(c.QueryParam("limit"))
	offset := parseDocumentOffset(c.QueryParam("offset"))

	notes, err := globalDocumentStore.ListNotes(unfiledOnly, noteType, tag, limit, offset)
	if err != nil {
		return writeDocumentError(c, err)
	}
	out := make([]documentJSON, len(notes))
	for i, d := range notes {
		out[i] = toDocumentJSON(d)
	}
	return c.JSON(http.StatusOK, map[string]any{"notes": out})
}

// ── GET /api/notes/:id ───────────────────────────────────────────────────

// getNoteHandler is GET /api/notes/:id: a note's metadata plus its body,
// read fresh off disk - one call serving both an editor and a reader (plan
// §4). checklist is the CURRENT body's own checklist items
// (parseChecklistItems, notes_format.go) - the template, with no tick
// state, present even with no active run so a reader knows whether "Start
// checklist" belongs on screen at all. active_run is that note's open
// checklist run (checklist_runs_store.go), or null when there isn't one -
// "you haven't started one" is a normal state, not an error.
func getNoteHandler(c echo.Context) error {
	doc, err := getNoteOrError(c.Param("id"))
	if err != nil {
		return writeDocumentError(c, err)
	}

	body, err := readNoteBody(doc)
	if err != nil {
		// Logged with the full detail (path, parse error); the response
		// stays generic - the same split documentContentHandler's own
		// "document file missing on disk" 500 makes (documents_handlers.go)
		// - so a disk/database drift never puts a filesystem path in an API
		// response.
		log.Printf("notes: get %s: %v", doc.ID, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "note file missing or unreadable on disk"})
	}

	checklist := parseChecklistItems(body)
	if checklist == nil {
		checklist = []checklistItem{}
	}
	activeRun, found, err := globalDocumentStore.ActiveChecklistRun(doc.ID)
	if err != nil {
		return writeDocumentError(c, err)
	}
	var activeRunJSON any
	if found {
		activeRunJSON = activeRun
	}
	return c.JSON(http.StatusOK, map[string]any{
		"document":   toDocumentJSON(doc),
		"body":       body,
		"checklist":  checklist,
		"active_run": activeRunJSON,
	})
}

// ── POST /api/notes ──────────────────────────────────────────────────────

type createNoteRequest struct {
	Body     string   `json:"body"`
	Title    string   `json:"title"`
	FolderID *string  `json:"folder_id"`
	Tags     []string `json:"tags"`
	Type     string   `json:"type"`
}

// createNoteHandler is POST /api/notes (plan §4): only body is required -
// capture demands nothing else (R1). Title, left blank, is derived from
// the body (deriveNoteTitle, notes_format.go: the first ATX heading, else
// the first line); type, left blank, comes from the local classifier
// (classifyNoteType, notes_classify.go) rather than staying empty - an
// operator who never touches the type picker still gets an icon, not a
// blank slot (R2).
//
// The note's id and created timestamp are generated here, before the blob
// is ever rendered, because both are baked into the frontmatter itself
// (plan §1: "Frontmatter id IS documents.id") - they have to exist before
// renderNoteFile runs, not be handed back afterward the way Insert's usual
// caller receives them. See Insert's own doc comment (documents_store.go)
// on why it honours a caller-supplied ID/CreatedAt instead of always
// generating fresh ones.
func createNoteHandler(c echo.Context) error {
	limitNoteRequestBody(c)
	var req createNoteRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}

	if strings.TrimSpace(req.Body) == "" {
		return writeDocumentError(c, errNoteBodyEmpty)
	}
	if len(req.Body) > noteMaxBodyBytes {
		return writeDocumentError(c, errNoteBodyTooLarge)
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = deriveNoteTitle(req.Body)
	}

	noteType := strings.TrimSpace(req.Type)
	noteTypeSource := "auto"
	if noteType != "" {
		if !validNoteType(noteType) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid note type"})
		}
		noteTypeSource = "operator"
	} else {
		noteType = classifyNoteType(req.Body)
	}

	id := uuid.NewString()
	created := time.Now().UTC().Truncate(time.Second)
	rendered := renderNoteFile(noteFileMeta{ID: id, Title: title, Type: noteType, Tags: req.Tags, Created: created}, req.Body)
	if noteRenderedTooLarge(rendered) {
		return writeDocumentError(c, errNoteBodyTooLarge)
	}
	sha := sha256Hex(rendered)

	dir := documentsDirPath()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to prepare document storage"})
	}

	tmp, err := os.CreateTemp(dir, "upload-*.tmp")
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create temp file"})
	}
	tmpPath := tmp.Name()
	removeTemp := func() {
		if tmpPath != "" {
			os.Remove(tmpPath)
		}
	}
	if _, err := tmp.Write(rendered); err != nil {
		tmp.Close()
		removeTemp()
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to write note"})
	}
	if err := tmp.Close(); err != nil {
		removeTemp()
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to write note"})
	}

	// Held across GetBySHA/rename/InsertNote - the same shape
	// uploadDocumentHandler's own single-sha lock takes (documents_handlers.go),
	// so a concurrent second create (or a delete) racing over the exact
	// same rendered bytes can never interleave into a row with no file or
	// a file no row points at. A fresh note's frontmatter carries a uuid
	// nothing else on the boat has ever written, so a genuine collision
	// here would mean some OTHER row's bytes happen to match this note's
	// rendered output exactly - vanishingly unlikely, but handled exactly
	// the way Insert already handles it, not assumed away.
	unlockSHA := lockDocumentSHA(sha)
	defer unlockSHA()

	if existing, ok, err := globalDocumentStore.GetBySHA(sha); err != nil {
		removeTemp()
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	} else if ok {
		removeTemp()
		return writeDocumentError(c, fmt.Errorf("note collides with existing document %s: %w", existing.ID, errDocumentDuplicate))
	}

	finalPath := filepath.Join(dir, sha)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		removeTemp()
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to store note"})
	}
	tmpPath = "" // renamed into place; removeTemp must not touch it anymore

	inserted, err := globalDocumentStore.InsertNote(document{
		ID:             id,
		SHA256:         sha,
		FolderID:       req.FolderID,
		Filename:       title + ".md",
		Title:          title,
		MIME:           "text/markdown",
		SizeBytes:      int64(len(rendered)),
		CreatedAt:      created,
		NoteType:       noteType,
		NoteTypeSource: noteTypeSource,
		OperatorTags:   req.Tags,
		// Notes default to enrich=false regardless of whether Mate is
		// configured and ready (plan §8) - departing from
		// uploadDocumentHandler's Enrich: enrichProblem == "". Uploading a
		// file is a deliberate act carrying its own consent (what ADR 0106
		// relied on); capturing a note in one tap at the helm is not, and
		// "ring Dave about the mooring, 0412…" is exactly the kind of text
		// an operator would not expect to leave the boat unprompted.
		// Enrichment is offered per-note, and library-wide via backfill,
		// in a later phase.
		Enrich: false,
	})
	if err != nil {
		// Safe to remove: still holding sha's lock, and GetBySHA just
		// confirmed no row owned this hash, so finalPath can only be this
		// attempt's own file.
		os.Remove(finalPath)
		if errors.Is(err, errFolderNotFound) {
			// folder_id is part of the request body, not a path segment
			// naming an existing resource - a bad value here is 400
			// (malformed request), matching uploadDocumentHandler's
			// identical folder_id handling rather than the 404
			// documentErrorStatus maps errFolderNotFound to everywhere else.
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "folder not found"})
		}
		return writeDocumentError(c, err)
	}

	wakeDocumentIndexer()
	return c.JSON(http.StatusCreated, map[string]any{"document": toDocumentJSON(inserted), "body": req.Body})
}

// ── PATCH /api/notes/:id ─────────────────────────────────────────────────

// applyNoteMetadata lands the database-only side of a note PATCH:
// title/tags/folder/sort_index via PatchDocument (documents_store.go - the
// exact same call patchDocumentHandler uses; the operator-annotation
// `notes` column is always passed nil, since nothing about the notes
// feature ever touches it), and note_type/note_type_source via
// SetNoteTypeIfNotOperator when setType is true. Skips the PatchDocument
// call entirely when none of title/tags/folder/sort_index are present, so
// a PATCH that only touches "type" (or nothing database-side beyond what
// ReplaceNoteBody's own caller already committed) doesn't open a pointless
// transaction.
//
// sort_index is what makes "file this note into a manual at position 3"
// (plan §4's "promotion gets no endpoint" - it's this PATCH) land
// atomically with the folder move that files it there in the first place:
// PatchDocument applies folder_id and sort_index in the SAME transaction,
// so a request carrying both either takes both or neither.
//
// pinned (plan §8) is neither part of a note's rendered frontmatter nor of
// PatchDocument's own field set - it goes through the dedicated
// SetNotePinned instead, the same "not every metadata field is a
// PatchDocument column" split note_type/note_type_source already draw via
// SetNoteTypeIfNotOperator just below.
func applyNoteMetadata(id string, title *string, tags []string, folderID *string, moveFolder bool, sortIndex *int, setType bool, noteType, noteTypeSource string, pinned *bool) error {
	if title != nil || tags != nil || moveFolder || sortIndex != nil {
		if _, err := globalDocumentStore.PatchDocument(id, title, nil, tags, folderID, moveFolder, sortIndex); err != nil {
			return err
		}
	}
	if setType {
		if err := globalDocumentStore.SetNoteTypeIfNotOperator(id, noteType, noteTypeSource); err != nil {
			return err
		}
	}
	if pinned != nil {
		if err := globalDocumentStore.SetNotePinned(id, *pinned); err != nil {
			return err
		}
	}
	return nil
}

// patchNoteHandler is PATCH /api/notes/:id: a presence-aware
// map[string]json.RawMessage body, the same idiom patchDocumentHandler
// uses (documents_handlers.go) - a field absent from the JSON is left
// untouched. Six fields are recognised: title, tags, folder_id (a move,
// null clears it to root - identical semantics to patchDocumentHandler's
// own), sort_index (this note's position among its new siblings - plan §4's
// manual promotion is this same PATCH with folder_id and sort_index
// together, not a dedicated endpoint), type (an explicit note_type
// override, always source='operator') and body (a full replacement of the
// note's text).
//
// This is the edit protocol's home (plan §1), the riskiest part of this
// phase. Step 1: title/type/tags/body all feed the note's frontmatter, so
// ANY of them being patched means the bytes on disk might change - the
// handler renders using whichever fields this call supplies, falling back
// to the note's current title/type/tags/body for anything it doesn't, and
// compares the result's sha256 against what's already stored.
//
//   - If they're equal (the only way this happens is a folder_id-only
//     patch, which isn't part of the frontmatter at all, or a patch whose
//     values happen to exactly match what was already there), there's
//     nothing to write to disk and nothing to reindex - only the metadata
//     actually present in the patch lands in the database, through
//     applyNoteMetadata.
//   - If they differ, this is a genuine content edit: steps 2-6 run
//     exactly as plan §1 numbers them, each commented at its own step
//     below.
func patchNoteHandler(c echo.Context) error {
	limitNoteRequestBody(c)
	id := c.Param("id")

	doc, err := getNoteOrError(id)
	if err != nil {
		return writeDocumentError(c, err)
	}

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(c.Request().Body).Decode(&raw); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if len(raw) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "no patch fields provided"})
	}

	var title, bodyPatch *string
	var tags []string
	var folderID *string
	var moveFolder bool
	var noteTypeOverride *string
	var sortIndex *int
	var pinned *bool

	if v, ok := raw["title"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid title"})
		}
		title = &s
	}
	if v, ok := raw["tags"]; ok {
		if err := json.Unmarshal(v, &tags); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid tags"})
		}
		if tags == nil {
			tags = []string{}
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
	if v, ok := raw["type"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid type"})
		}
		s = strings.TrimSpace(s)
		if !validNoteType(s) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid note type"})
		}
		noteTypeOverride = &s
	}
	if v, ok := raw["body"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}
		if strings.TrimSpace(s) == "" {
			return writeDocumentError(c, errNoteBodyEmpty)
		}
		if len(s) > noteMaxBodyBytes {
			return writeDocumentError(c, errNoteBodyTooLarge)
		}
		bodyPatch = &s
	}
	// sort_index is plan §4's promotion path: "file this note into a manual
	// at position 3" is this PATCH with folder_id AND sort_index together,
	// not a dedicated endpoint (manuals_handlers.go's own doc comment says
	// why). It never feeds the note's rendered frontmatter - only
	// title/type/tags/body do - so it plays no part in needsRender below.
	if v, ok := raw["sort_index"]; ok {
		var n int
		if err := json.Unmarshal(v, &n); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid sort_index"})
		}
		sortIndex = &n
	}
	// pinned (plan §8): the operator-facing way to pin/unpin a note for
	// Mate's pinned-notes prompt (assistant_prompt.go). Like sort_index, it
	// plays no part in the note's rendered frontmatter, so it never feeds
	// needsRender below.
	if v, ok := raw["pinned"]; ok {
		var p bool
		if err := json.Unmarshal(v, &p); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid pinned"})
		}
		pinned = &p
	}

	needsRender := title != nil || tags != nil || noteTypeOverride != nil || bodyPatch != nil

	// resolvedType/resolvedSource is what SetNoteTypeIfNotOperator
	// eventually writes (via applyNoteMetadata below), and - when setType
	// - what gets rendered into frontmatter too, so the file on disk and
	// the database row never disagree about this note's type after the
	// patch commits. An explicit operator override always takes it, with
	// source "operator"; otherwise a body edit re-runs the local
	// classifier (plan §8: "runs on every body edit where
	// note_type_source != 'operator'") - SetNoteTypeIfNotOperator enforces
	// that same guard again on its own, so getting this condition wrong
	// here would cost a wasted render, not a correctness bug, but
	// computing it up front is what lets a single render reflect it.
	var setType bool
	var resolvedType, resolvedSource string
	switch {
	case noteTypeOverride != nil:
		resolvedType, resolvedSource, setType = *noteTypeOverride, "operator", true
	case bodyPatch != nil && doc.NoteTypeSource != "operator":
		resolvedType, resolvedSource, setType = classifyNoteType(*bodyPatch), "auto", true
	}

	effectiveTitle := doc.Title
	if title != nil {
		effectiveTitle = *title
	}
	effectiveType := doc.NoteType
	if setType {
		effectiveType = resolvedType
	}
	effectiveTags := doc.OperatorTags
	if tags != nil {
		effectiveTags = tags
	}

	newSHA := doc.SHA256
	var rendered []byte
	if needsRender {
		effectiveBody := ""
		if bodyPatch != nil {
			effectiveBody = *bodyPatch
		} else {
			effectiveBody, err = readNoteBody(doc)
			if err != nil {
				log.Printf("notes: patch %s: %v", doc.ID, err)
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": "note file missing or unreadable on disk"})
			}
		}
		rendered = renderNoteFile(noteFileMeta{ID: doc.ID, Title: effectiveTitle, Type: effectiveType, Tags: effectiveTags, Created: doc.CreatedAt}, effectiveBody)
		if noteRenderedTooLarge(rendered) {
			return writeDocumentError(c, errNoteBodyTooLarge)
		}
		newSHA = sha256Hex(rendered)
	}

	if newSHA == doc.SHA256 {
		// Nothing about the serialised bytes changed - see this
		// function's own doc comment on why. Only the database side of
		// whatever was actually patched needs to land.
		if err := applyNoteMetadata(doc.ID, title, tags, folderID, moveFolder, sortIndex, setType, resolvedType, resolvedSource, pinned); err != nil {
			return writeDocumentError(c, err)
		}
	} else {
		// A genuine content edit: plan §1's edit protocol, steps 2-6.
		//
		// Step 2: lock BOTH hashes, in lexicographic order - the
		// two-writer hazard that's new to notes (lockTwoDocumentSHAs' own
		// doc comment explains why a fixed order is required, not just
		// convenient).
		unlockA, unlockB := lockTwoDocumentSHAs(doc.SHA256, newSHA)
		defer unlockA()
		defer unlockB()

		// A collision check BEFORE any filesystem write, under the lock
		// just acquired - the same ordering uploadDocumentHandler's own
		// GetBySHA-before-rename uses (documents_handlers.go), and for the
		// identical reason: os.Rename below would otherwise silently
		// REPLACE whatever already sits at dir/newSHA if another row
		// already owns that hash (POSIX rename semantics overwrite an
		// existing destination), clobbering that row's real content before
		// ReplaceNoteBody's own collision check ever got a chance to say
		// no. Checking first means a collision is rejected without this
		// note's edit ever touching a byte that belongs to the other row.
		if existing, ok, err := globalDocumentStore.GetBySHA(newSHA); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		} else if ok {
			return writeDocumentError(c, fmt.Errorf("note edit collides with existing document %s: %w", existing.ID, errDocumentDuplicate))
		}

		dir := documentsDirPath()
		// Step 3: write to a temp file, then rename into place at the new
		// sha's path. The upload-*.tmp prefix is reused deliberately -
		// not a fresh naming scheme - so an interrupted note edit is
		// cleaned up by the exact same boot sweep
		// (sweepDocumentsDir) that already deletes an abandoned upload's
		// temp file, with no new sweep logic required.
		tmp, err := os.CreateTemp(dir, "upload-*.tmp")
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create temp file"})
		}
		tmpPath := tmp.Name()
		removeTemp := func() {
			if tmpPath != "" {
				os.Remove(tmpPath)
			}
		}
		if _, err := tmp.Write(rendered); err != nil {
			tmp.Close()
			removeTemp()
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to write note"})
		}
		if err := tmp.Close(); err != nil {
			removeTemp()
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to write note"})
		}

		finalPath := filepath.Join(dir, newSHA)
		if err := os.Rename(tmpPath, finalPath); err != nil {
			removeTemp()
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to store note"})
		}
		tmpPath = ""

		// Step 4. ReplaceNoteBody repeats its own collision check
		// internally (notes_store.go) - now redundant in the ordinary
		// case, since the GetBySHA above and this call both run under the
		// same held lock with nothing able to intervene between them, but
		// it is what keeps ReplaceNoteBody itself correct when called
		// directly (as a store-level test does) rather than only through
		// this handler's own pre-check.
		oldSHA, err := globalDocumentStore.ReplaceNoteBody(doc.ID, newSHA, int64(len(rendered)))
		if err != nil {
			// Step 5's error branch: the new blob never became THE blob
			// for this document, so it's removed. Safe under the lock -
			// nothing else can have claimed newSHA's path between the
			// rename above and here.
			os.Remove(finalPath)
			return writeDocumentError(c, err)
		}

		if err := applyNoteMetadata(doc.ID, title, tags, folderID, moveFolder, sortIndex, setType, resolvedType, resolvedSource, pinned); err != nil {
			return writeDocumentError(c, err)
		}

		// Step 5's success branch: the old blob is now superseded. A
		// failure removing it is LOGGED, NOT RETURNED - the database row
		// is already correct (ReplaceNoteBody committed above), and the
		// leftover file is exactly what sweepDocumentsDir already reports
		// as an orphan at the next boot
		// (TestSweepDocumentsDir_ReportsASupersededNoteBlobAsAnOrphan).
		// Returning an error here instead would tell the operator their
		// edit failed when, from the database's point of view - the
		// source of truth while the note exists at all - it fully
		// succeeded; that is a strictly worse outcome than one extra file
		// sitting on disk until the next boot notices it.
		oldPath := filepath.Join(dir, oldSHA)
		if err := os.Remove(oldPath); err != nil && !os.IsNotExist(err) {
			log.Printf("notes: patch %s: failed to remove superseded blob %s: %v", doc.ID, oldPath, err)
		}

		// Step 6.
		wakeDocumentIndexer()
	}

	updated, err := globalDocumentStore.Get(doc.ID)
	if err != nil {
		return writeDocumentError(c, err)
	}
	body, err := readNoteBody(updated)
	if err != nil {
		log.Printf("notes: patch %s: %v", doc.ID, err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "note file missing or unreadable on disk"})
	}
	return c.JSON(http.StatusOK, map[string]any{"document": toDocumentJSON(updated), "body": body})
}

// ── POST /api/notes/classify/backfill ────────────────────────────────────
//
// The no-Mate half of plan §9's two backfills: runs classifyNoteType (a
// pure Go function, notes_classify.go) over every note the operator never
// classified themselves. No network call, no OpenRouter spend, no Mate -
// the more important of the two backfills, because a note captured before
// the classifier shipped has note_type == "".

// reclassifyNote re-runs classifyNoteType over doc's current on-disk body
// and, if the result differs from what's already stored (or doc has never
// been classified at all), applies it exactly the way an ordinary
// type-only PATCH does (patchNoteHandler's own edit protocol): re-render
// the frontmatter under the new type, take both sha256 locks, write the
// new blob, ReplaceNoteFrontmatter, then SetNoteTypeIfNotOperator, then
// remove the superseded blob. This keeps the on-disk frontmatter
// and the database in agreement about note_type - the SAME invariant every
// other type-changing path in this codebase already keeps (ADR 0116:
// "Frontmatter is for portability" - a reclassified note that only changed
// in the database would report the wrong type to a crew member reading the
// raw file, or to a restored backup with no app in front of it).
//
// source is always "auto", never "operator": nothing about a backfill pass
// run over the whole library is the operator making a deliberate choice
// about this ONE note (that's what the type picker is for), and
// SetNoteTypeIfNotOperator's own precedence rule already refuses to let
// "auto" overwrite "operator" regardless - this is a second, cheaper guard
// (doc.NoteTypeSource == "operator" is checked before doing any work at
// all), not the only one.
//
// Returns changed=false, with nothing written anywhere, when the
// classifier's answer already matches doc.NoteType AND doc.NoteTypeSource
// is already "auto" - the ordinary case for a backfill run a second time
// over a library it already processed once.
func reclassifyNote(doc document) (changed bool, err error) {
	if doc.NoteTypeSource == "operator" {
		return false, nil
	}

	body, err := readNoteBody(doc)
	if err != nil {
		return false, err
	}
	newType := classifyNoteType(body)
	if newType == doc.NoteType && doc.NoteTypeSource == "auto" {
		return false, nil
	}

	rendered := renderNoteFile(noteFileMeta{ID: doc.ID, Title: doc.Title, Type: newType, Tags: doc.OperatorTags, Created: doc.CreatedAt}, body)
	newSHA := sha256Hex(rendered)

	if newSHA == doc.SHA256 {
		// Only reachable if the rendered bytes happen not to depend on the
		// type field changing (they always do - type is part of the
		// frontmatter - so this is a defensive no-op path, not one this
		// function's own tests expect to exercise) - the database side
		// alone still needs to land.
		if err := globalDocumentStore.SetNoteTypeIfNotOperator(doc.ID, newType, "auto"); err != nil {
			return false, err
		}
		return true, nil
	}

	// Steps 2-6 of the edit protocol (plan §1 / patchNoteHandler's own doc
	// comment), driven here instead of through the HTTP layer.
	unlockA, unlockB := lockTwoDocumentSHAs(doc.SHA256, newSHA)
	defer unlockA()
	defer unlockB()

	if existing, ok, err := globalDocumentStore.GetBySHA(newSHA); err != nil {
		return false, err
	} else if ok {
		return false, fmt.Errorf("note reclassify collides with existing document %s: %w", existing.ID, errDocumentDuplicate)
	}

	dir := documentsDirPath()
	tmp, err := os.CreateTemp(dir, "upload-*.tmp")
	if err != nil {
		return false, fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	removeTemp := func() {
		if tmpPath != "" {
			os.Remove(tmpPath)
		}
	}
	if _, err := tmp.Write(rendered); err != nil {
		tmp.Close()
		removeTemp()
		return false, fmt.Errorf("failed to write note: %w", err)
	}
	if err := tmp.Close(); err != nil {
		removeTemp()
		return false, fmt.Errorf("failed to write note: %w", err)
	}

	finalPath := filepath.Join(dir, newSHA)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		removeTemp()
		return false, fmt.Errorf("failed to store note: %w", err)
	}
	tmpPath = ""

	// ReplaceNoteFrontmatter, NOT ReplaceNoteBody: only the `type:` line
	// changed, and extractTextFile strips the frontmatter fence before
	// extracting, so the indexer would rebuild byte-identical chunks and
	// buy byte-identical embeddings again for every note this touches. The
	// classify backfill is the FREE one - it ships with no "this bills
	// you" confirmation, unlike the enrich backfill beside it, so silently
	// requeueing every reclassified note would make that promise false.
	// doc.SHA256 is the compare-and-swap expectation: this loop runs over a
	// snapshot, and the operator may have edited this very note since it
	// was taken. ReplaceNoteFrontmatter refuses rather than reverting them,
	// and sets note_type in the same transaction so the blob on disk and
	// the row can never disagree about it.
	oldSHA, err := globalDocumentStore.ReplaceNoteFrontmatter(doc.ID, doc.SHA256, newSHA, newType, int64(len(rendered)))
	if err != nil {
		os.Remove(finalPath)
		return false, err
	}

	oldPath := filepath.Join(dir, oldSHA)
	if err := os.Remove(oldPath); err != nil && !os.IsNotExist(err) {
		log.Printf("notes: classify backfill %s: failed to remove superseded blob %s: %v", doc.ID, oldPath, err)
	}

	// No wakeDocumentIndexer() here, deliberately: nothing was queued.
	return true, nil
}

type notesClassifyBackfillDryRunJSON struct {
	Count int `json:"count"`
}

type notesClassifyBackfillJSON struct {
	Count int `json:"count"`
	// Skipped is notes the operator edited while the pass was running, so
	// their snapshot went stale and rewriting from it would have reverted
	// the edit. Unconditional (no omitempty): a client rendering "3
	// reclassified, 0 skipped" needs the zero, and this project has been
	// bitten by omitempty hiding a genuine zero before (ADR 0115 §7).
	Skipped int `json:"skipped"`
}

// notesClassifyBackfillHandler is POST /api/notes/classify/backfill[?dry_run=1]:
// dry_run reports how many notes are currently candidates (note_type_source
// IN (empty, 'auto')) without touching anything; the real call runs
// reclassifyNote over every one of them and reports how many actually
// changed. Fail-fast, not best-effort: a failure reclassifying one note
// stops the pass and reports the error rather than silently skipping it and
// reporting a success count that doesn't match what the operator asked for
// - notes already reclassified before the failure keep their new
// classification (there is no whole-pass rollback), the same "no masking
// fallbacks" reasoning as every other write path in this codebase.
func notesClassifyBackfillHandler(c echo.Context) error {
	candidates, err := globalDocumentStore.NoteClassifyBackfillCandidates()
	if err != nil {
		return writeDocumentError(c, err)
	}

	if parseDocumentBool(c.QueryParam("dry_run")) {
		return c.JSON(http.StatusOK, notesClassifyBackfillDryRunJSON{Count: len(candidates)})
	}

	changed := 0
	skipped := 0
	for _, doc := range candidates {
		wasChanged, err := reclassifyNote(doc)
		// A note the operator edited while this pass was running is not a
		// failure - the edit path reclassifies it on its own, and stopping
		// the whole run over one would make the backfill unusable on any
		// library big enough to want it. It is still reported rather than
		// swallowed: the response says how many were left behind.
		if errors.Is(err, errNoteChangedUnderfoot) {
			log.Printf("notes: classify backfill: %s changed while running, left for its own edit to reclassify", doc.ID)
			skipped++
			continue
		}
		if err != nil {
			log.Printf("notes: classify backfill: reclassify %s: %v", doc.ID, err)
			return writeDocumentError(c, err)
		}
		if wasChanged {
			changed++
		}
	}
	return c.JSON(http.StatusOK, notesClassifyBackfillJSON{Count: changed, Skipped: skipped})
}

// ── POST /api/notes/enrich/backfill ──────────────────────────────────────
//
// The Mate half of plan §9's two backfills: sets enrich=1 on every note
// that doesn't already carry it and wakes the indexer, so Mate summarises
// and (if semantic search is configured) embeds them the same way it does
// any note captured with enrichment offered and accepted. Reuses the
// ?dry_run=1 shape and "this bills you" consent wording of
// POST /api/documents/embeddings/backfill (documents_handlers.go):
// dry_run costs nothing and starts nothing; making the real call - not a
// separate confirmation step - IS the operator's consent for this text to
// reach OpenRouter, the same as that embeddings endpoint's own comment.

type notesEnrichBackfillDryRunJSON struct {
	Count int `json:"count"`
	// TokensEstimate is CharsPending/4, the same rough characters-per-token
	// estimate documentEmbeddingsBackfillDryRunJSON already gives its own
	// caller (documents_handlers.go) - a scale-setting figure for the
	// confirmation dialog, not a price quote.
	TokensEstimate int `json:"tokens_estimate"`
}

type notesEnrichBackfillJSON struct {
	Count int `json:"count"`
}

// notesEnrichBackfillHandler is POST /api/notes/enrich/backfill[?dry_run=1].
// dry_run needs no assistant readiness check at all - it only counts and
// estimates, the same as the embeddings backfill's own dry run. The real
// call does: reindexDocumentHandler's own per-document "Reindex…" action
// checks documentEnrichReadinessProblem before ever setting enrich=1 on a
// single document, and a library-wide backfill gets no lighter a gate.
func notesEnrichBackfillHandler(c echo.Context) error {
	if parseDocumentBool(c.QueryParam("dry_run")) {
		counts, err := globalDocumentStore.NoteEnrichBackfillCounts()
		if err != nil {
			return writeDocumentError(c, err)
		}
		return c.JSON(http.StatusOK, notesEnrichBackfillDryRunJSON{
			Count:          counts.NotesPending,
			TokensEstimate: counts.CharsPending / 4,
		})
	}

	readiness, _, err := checkAssistantReadiness(assistantSettingsPath())
	if err != nil {
		log.Printf("notes: enrich backfill: check assistant readiness: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if problem := documentEnrichReadinessProblem(readiness); problem != "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": problem})
	}

	n, err := globalDocumentStore.EnrichBackfillNotes()
	if err != nil {
		return writeDocumentError(c, err)
	}
	wakeDocumentIndexer()
	return c.JSON(http.StatusOK, notesEnrichBackfillJSON{Count: n})
}
