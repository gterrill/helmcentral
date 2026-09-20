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
// read fresh off disk - one call serving both an editor and a read-only
// view (plan §4). It does not yet return checklist/active_run: those
// belong to the checklist-runs feature (plan §3), which has its own
// tables and is a later phase this one deliberately does not build.
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
	return c.JSON(http.StatusOK, map[string]any{"document": toDocumentJSON(doc), "body": body})
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
// title/tags/folder via PatchDocument (documents_store.go - the exact same
// call patchDocumentHandler uses; the operator-annotation `notes` column
// is always passed nil, since nothing about the notes feature ever touches
// it), and note_type/note_type_source via SetNoteTypeIfNotOperator when
// setType is true. Skips the PatchDocument call entirely when none of
// title/tags/folder are present, so a PATCH that only touches "type" (or
// nothing database-side beyond what ReplaceNoteBody's own caller already
// committed) doesn't open a pointless transaction.
func applyNoteMetadata(id string, title *string, tags []string, folderID *string, moveFolder bool, setType bool, noteType, noteTypeSource string) error {
	if title != nil || tags != nil || moveFolder {
		if _, err := globalDocumentStore.PatchDocument(id, title, nil, tags, folderID, moveFolder); err != nil {
			return err
		}
	}
	if setType {
		if err := globalDocumentStore.SetNoteTypeIfNotOperator(id, noteType, noteTypeSource); err != nil {
			return err
		}
	}
	return nil
}

// patchNoteHandler is PATCH /api/notes/:id: a presence-aware
// map[string]json.RawMessage body, the same idiom patchDocumentHandler
// uses (documents_handlers.go) - a field absent from the JSON is left
// untouched. Five fields are recognised: title, tags, folder_id (a move,
// null clears it to root - identical semantics to patchDocumentHandler's
// own), type (an explicit note_type override, always source='operator')
// and body (a full replacement of the note's text).
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
		newSHA = sha256Hex(rendered)
	}

	if newSHA == doc.SHA256 {
		// Nothing about the serialised bytes changed - see this
		// function's own doc comment on why. Only the database side of
		// whatever was actually patched needs to land.
		if err := applyNoteMetadata(doc.ID, title, tags, folderID, moveFolder, setType, resolvedType, resolvedSource); err != nil {
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

		if err := applyNoteMetadata(doc.ID, title, tags, folderID, moveFolder, setType, resolvedType, resolvedSource); err != nil {
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
