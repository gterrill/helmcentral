package main

import (
	"database/sql"
	"errors"
	"fmt"
)

// This file is the note-scoped half of documentStore (documents_store.go):
// a note is a documents row with kind='note', so folders, tags, chunks,
// FTS5 search, the boot sweep and every generic document method already
// work for it. What's here is only what a note needs and a plain uploaded
// file does not - inserting one with its id pre-baked into the frontmatter
// that's already on disk, replacing its body (the two-writer edit protocol
// plan §1 calls the riskiest part of this feature), the note_type
// precedence rule, and a kind='note'-scoped listing.

// Sentinel errors specific to notes, checked with errors.Is by
// notes_handlers.go the same way documents_store.go's own sentinels are
// checked by documents_handlers.go.
var (
	// errNotANote is returned whenever a call that only makes sense for a
	// note (ReplaceNoteBody, and notes_handlers.go's own id lookups) is
	// given the id of a kind='file' document instead.
	errNotANote = errors.New("document is not a note")

	// errNoteBodyEmpty and errNoteBodyTooLarge are notes_handlers.go's own
	// request-validation sentinels, defined here (with the rest of this
	// file's error vocabulary) rather than inline in the handler, so
	// documentErrorStatus (documents_handlers.go) can map them the same
	// way it maps every other store-level sentinel.
	errNoteBodyEmpty    = errors.New("note body is empty")
	errNoteBodyTooLarge = errors.New("note body is too large")
)

// InsertNote is Insert (documents_store.go) with Kind pinned to "note" -
// the one difference between a captured note and an uploaded file row, and
// pinning it here (rather than leaving every caller to remember to set it)
// means a note can never accidentally land as kind='file', which would
// make it invisible to every /api/notes listing (ListNotes below) while
// still sitting in the document library under GET /api/documents. Every
// other field - most importantly doc.ID and doc.CreatedAt, which
// notes_handlers.go pre-computes and bakes into the note's frontmatter
// BEFORE this is ever called - passes straight through to Insert
// unchanged; see Insert's own doc comment for why it honours a caller-
// supplied ID/CreatedAt instead of always generating fresh ones.
func (s *documentStore) InsertNote(doc document) (document, error) {
	doc.Kind = "note"
	return s.Insert(doc)
}

// ReplaceNoteBody is the write side of the edit protocol's step 4 (plan
// §1): id's sha256/size_bytes/updated_at/status/stage/error/reindex_seq are
// updated in one transaction, and the meta chunk is rebuilt so search
// reflects the edit immediately. It returns the sha256 the row carried
// BEFORE this call, so notes_handlers.go knows which blob on disk is now
// superseded and safe to remove.
//
// force_ocr is deliberately left untouched - the one difference from
// MarkReindex (documents_store.go), and why this can't just call that
// method with a sha update bolted on: MarkReindex always sets force_ocr=1
// because ITS caller (the operator's explicit "reindex this" action) exists
// specifically to retry OCR. An ordinary note edit has no OCR involved at
// all (notes are text/markdown, never scanned), so force_ocr should simply
// keep whatever value it already had - almost always 0, since nothing in
// this codebase ever sets it on a note.
//
// Two special cases, both checked before any write:
//
//   - newSHA already equal to the current sha256 is a true no-op - nothing
//     is written, and the old sha is returned unchanged. notes_handlers.go
//     already short-circuits this case itself (step 1 of the edit
//     protocol: render, compare, only proceed to the lock/write dance on a
//     genuine difference), but this method stays correct if called
//     directly, which is exactly what a store-level test of this exact
//     case does.
//   - newSHA colliding with some OTHER row's sha256 is reported as
//     errDocumentDuplicate, naming the colliding document's id - the
//     residual case plan §1 calls out explicitly: sha256 is UNIQUE across
//     the WHOLE documents table, kind='file' rows included, and a note
//     edited to bytes that happen to match an existing upload is a real,
//     if vanishingly unlikely, collision that has to surface rather than
//     be assumed away. This mirrors Insert's own check-before-write
//     duplicate handling exactly.
func (s *documentStore) ReplaceNoteBody(id, newSHA string, size int64) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var currentSHA, kind string
	err := s.db.QueryRow(`SELECT sha256, kind FROM documents WHERE id = ?`, id).Scan(&currentSHA, &kind)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errDocumentNotFound
	}
	if err != nil {
		return "", fmt.Errorf("replace note body: read document: %w", err)
	}
	if kind != "note" {
		return "", errNotANote
	}
	if currentSHA == newSHA {
		return currentSHA, nil
	}

	existing, err := scanDocument(s.db.QueryRow(`SELECT `+documentColumns+` FROM documents WHERE sha256 = ?`, newSHA))
	if err == nil {
		return "", fmt.Errorf("note edit collides with existing document %s: %w", existing.ID, errDocumentDuplicate)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("replace note body: check collision: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return "", fmt.Errorf("replace note body: begin: %w", err)
	}
	defer tx.Rollback()

	now := s.now()
	res, err := tx.Exec(
		`UPDATE documents SET sha256 = ?, size_bytes = ?, updated_at = ?, status = 'pending', stage = 'extract', error = '', reindex_seq = reindex_seq + 1 WHERE id = ?`,
		newSHA, size, now.Unix(), id,
	)
	if err != nil {
		return "", fmt.Errorf("replace note body: %w", err)
	}
	if err := checkRowsAffected(res, errDocumentNotFound); err != nil {
		return "", err
	}

	if err := s.rebuildMetaChunkTx(tx, id); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("replace note body: commit: %w", err)
	}
	return currentSHA, nil
}

// SetNoteTypeIfNotOperator writes id's note_type/note_type_source UNLESS
// doing so would let a lower-precedence source overwrite an operator's own
// choice - the one method plan §2 assigns the whole operator > mate > auto
// precedence rule to (the same relationship document_tags' operator/
// suggested split already has). Two cases:
//
//   - source == "operator": always writes. This call itself IS the
//     operator action (a one-tap override in notes_handlers.go), so it
//     always lands, even over an earlier operator choice - a fresh,
//     deliberate override is not a lower-precedence source trying to
//     second-guess the standing one.
//   - source is "auto" or "mate": writes only if the CURRENT
//     note_type_source is not already "operator". This is the guard the
//     method's name refers to - an operator's classification is sticky
//     against anything automatic, so a body edit's routine reclassification
//     (classifyNoteType re-run in notes_handlers.go) can never quietly
//     undo an explicit override.
func (s *documentStore) SetNoteTypeIfNotOperator(id, noteType, source string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var currentSource, kind string
	err := s.db.QueryRow(`SELECT note_type_source, kind FROM documents WHERE id = ?`, id).Scan(&currentSource, &kind)
	if errors.Is(err, sql.ErrNoRows) {
		return errDocumentNotFound
	}
	if err != nil {
		return fmt.Errorf("set note type: read document: %w", err)
	}
	if kind != "note" {
		return errNotANote
	}

	if currentSource == "operator" && source != "operator" {
		return nil
	}

	res, err := s.db.Exec(
		`UPDATE documents SET note_type = ?, note_type_source = ?, updated_at = ? WHERE id = ?`,
		noteType, source, s.now().Unix(), id,
	)
	if err != nil {
		return fmt.Errorf("set note type: %w", err)
	}
	return checkRowsAffected(res, errDocumentNotFound)
}

// ListNotes returns kind='note' documents, newest first (the same
// created_at-then-id tiebreak queryDocuments itself uses), optionally
// narrowed to the inbox (unfiledOnly: folder_id IS NULL - the Notes panel's
// default view, plan §4's GET /api/notes?filed=0) and/or a note_type/tag
// filter. limit<=0 means every matching row, no LIMIT/OFFSET - matching
// List's own contract (documents_store.go), though
// notes_handlers.go's caller always runs limit through parseDocumentLimit
// first, which never returns a non-positive value.
//
// Deliberately its OWN query rather than a kind/noteType parameter threaded
// through queryDocuments: that function backs every /api/documents listing
// and search path and already carries non-trivial folder/tag branching
// several existing callers depend on. Notes are the only caller that will
// ever need a kind or note_type filter, so adding two more parameters (and
// two more branches) to a function several other call sites share, for one
// new caller's sake, would cost more than the handful of lines this
// duplicates.
func (s *documentStore) ListNotes(unfiledOnly bool, noteType, tag string, limit, offset int) ([]document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `SELECT ` + documentColumns + ` FROM documents d WHERE d.kind = 'note'`
	var args []any

	if unfiledOnly {
		query += ` AND d.folder_id IS NULL`
	}
	if noteType != "" {
		query += ` AND d.note_type = ?`
		args = append(args, noteType)
	}
	if tag != "" {
		query += ` AND EXISTS (SELECT 1 FROM document_tags t WHERE t.document_id = d.id AND t.tag = ?)`
		args = append(args, tag)
	}
	query += ` ORDER BY d.created_at DESC, d.id DESC`
	if limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, limit, offset)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}
	defer rows.Close()

	var out []document
	for rows.Next() {
		doc, err := scanDocument(rows)
		if err != nil {
			return nil, fmt.Errorf("list notes: scan: %w", err)
		}
		out = append(out, doc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}

	for i := range out {
		if err := s.attachTags(s.db, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}
