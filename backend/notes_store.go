package main

import (
	"database/sql"
	"errors"
	"fmt"
	"unicode/utf8"
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
	// errNoteChangedUnderfoot is the compare-and-swap miss in
	// ReplaceNoteFrontmatter: the note's bytes moved between the caller
	// snapshotting it and the caller trying to write. 409, because the
	// request was fine and the world changed, not the request.
	errNoteChangedUnderfoot = errors.New("note changed while it was being reclassified")

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

// ReplaceNoteFrontmatter swaps a note's blob for one whose FRONTMATTER
// changed but whose body did not - today, only the classify backfill's
// `type:` rewrite (notes_handlers.go's reclassifyNote).
//
// Identical to ReplaceNoteBody in everything except the one thing that
// matters here: it does NOT reset status/stage or bump reindex_seq, so the
// indexer is never re-run. That is safe precisely because extractTextFile
// strips the leading frontmatter fence before extracting
// (documents_extract.go), so the text the indexer would read is
// byte-identical either way - a re-extract would rebuild the same chunks
// and buy the same embeddings again, at full OpenRouter cost, for a note
// whose searchable content did not change by one character.
//
// It matters because the classify backfill is the free one. The plan gives
// it no "this bills you" confirmation, unlike the enrich backfill beside
// it, on the stated grounds that it costs nothing and needs no Mate.
// Requeueing here would make that promise false, and silently.
//
// The meta chunk IS rebuilt, because note_type is part of it and that is
// what makes the new type searchable - cheap, local, and no embedding call.
func (s *documentStore) ReplaceNoteFrontmatter(id, expectedSHA, newSHA, noteType string, size int64) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var currentSHA, currentSource, kind string
	err := s.db.QueryRow(`SELECT sha256, note_type_source, kind FROM documents WHERE id = ?`, id).Scan(&currentSHA, &currentSource, &kind)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errDocumentNotFound
	}
	if err != nil {
		return "", fmt.Errorf("replace note frontmatter: read document: %w", err)
	}
	if kind != "note" {
		return "", errNotANote
	}
	// Compare-and-swap. The classify backfill loops over a snapshot taken
	// once, so by the time it reaches a given note the operator may have
	// edited it - and rewriting from the stale render would both revert the
	// row AND hand the caller the operator's fresh blob to delete as
	// "superseded". Refuse instead; the edit path reclassifies on its own.
	if currentSHA != expectedSHA {
		return "", errNoteChangedUnderfoot
	}
	if currentSHA == newSHA {
		return currentSHA, nil
	}

	existing, err := scanDocument(s.db.QueryRow(`SELECT `+documentColumns+` FROM documents WHERE sha256 = ?`, newSHA))
	if err == nil {
		return "", fmt.Errorf("note frontmatter rewrite collides with existing document %s: %w", existing.ID, errDocumentDuplicate)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("replace note frontmatter: check collision: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return "", fmt.Errorf("replace note frontmatter: begin: %w", err)
	}
	defer tx.Rollback()

	// note_type moves in the SAME transaction as the sha. Two separately
	// locked calls would leave a window where the blob on disk already
	// names the new type while the row still names the old one - exactly
	// the disk/DB agreement this whole function exists to keep. An
	// operator override stays sticky, the same precedence
	// SetNoteTypeIfNotOperator enforces.
	res, err := tx.Exec(
		`UPDATE documents SET sha256 = ?, size_bytes = ?, updated_at = ?,
		        note_type = CASE WHEN note_type_source = 'operator' THEN note_type ELSE ? END,
		        note_type_source = CASE WHEN note_type_source = 'operator' THEN note_type_source ELSE 'auto' END
		 WHERE id = ?`,
		newSHA, size, s.now().Unix(), noteType, id,
	)
	if err != nil {
		return "", fmt.Errorf("replace note frontmatter: %w", err)
	}
	if err := checkRowsAffected(res, errDocumentNotFound); err != nil {
		return "", err
	}

	if err := s.rebuildMetaChunkTx(tx, id); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("replace note frontmatter: commit: %w", err)
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

// ── Pinned notes (plan §8: Mate's pinned-note prompt) ────────────────────

// SetNotePinned writes id's pinned flag directly. Unlike note_type there is
// no precedence to defer to here: pinning is always an explicit operator
// action (the plan's own "add the operator-facing way to pin and unpin a
// note" - without it the pinned-notes prompt feature has no way to ever
// hold anything), so there is nothing else in this codebase that could ever
// race to set it out from under the operator's own tap the way an
// automatic reclassification can race note_type.
func (s *documentStore) SetNotePinned(id string, pinned bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var kind string
	err := s.db.QueryRow(`SELECT kind FROM documents WHERE id = ?`, id).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return errDocumentNotFound
	}
	if err != nil {
		return fmt.Errorf("set note pinned: read document: %w", err)
	}
	if kind != "note" {
		return errNotANote
	}

	res, err := s.db.Exec(`UPDATE documents SET pinned = ?, updated_at = ? WHERE id = ?`, boolToInt(pinned), s.now().Unix(), id)
	if err != nil {
		return fmt.Errorf("set note pinned: %w", err)
	}
	return checkRowsAffected(res, errDocumentNotFound)
}

// PinnedNotes returns every kind='note' document with pinned=1,
// alphabetically by title - the whole set, every time. The prompt's own cap
// (assistantMaxPinnedNotes/assistantMaxPinnedNoteChars, assistant_prompt.go)
// is applied at RENDER time, not here, the same split ListManuals/ListNotes
// already draw between "what exists" and what a particular caller shows -
// so a caller that ever wants a different cap (or none at all) doesn't need
// a second store method.
func (s *documentStore) PinnedNotes() ([]document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT ` + documentColumns + ` FROM documents WHERE kind = 'note' AND pinned = 1 ORDER BY lower(title)`)
	if err != nil {
		return nil, fmt.Errorf("pinned notes: %w", err)
	}
	defer rows.Close()

	var out []document
	for rows.Next() {
		doc, err := scanDocument(rows)
		if err != nil {
			return nil, fmt.Errorf("pinned notes: scan: %w", err)
		}
		out = append(out, doc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pinned notes: %w", err)
	}
	return out, nil
}

// ── Backfills (plan §9's "no-Mate path": the operator who enables Mate, or
// upgrades the classifier, later) ─────────────────────────────────────────

// NoteClassifyBackfillCandidates returns every kind='note' document whose
// note_type_source is the empty string (never classified - a note captured before the
// classifier shipped) or 'auto' (classified, but never confirmed by an
// operator override or upgraded by Mate) - never 'operator', which is
// sticky against exactly this kind of automatic pass, the same precedence
// SetNoteTypeIfNotOperator already enforces for a single note. Ordered
// oldest-first (created_at, then id) purely so a backfill run's own log
// output and any future progress reporting reads in a stable, predictable
// order - nothing about classification itself depends on order.
func (s *documentStore) NoteClassifyBackfillCandidates() ([]document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT ` + documentColumns + ` FROM documents WHERE kind = 'note' AND note_type_source IN ('', 'auto') ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("note classify backfill candidates: %w", err)
	}
	defer rows.Close()

	var out []document
	for rows.Next() {
		doc, err := scanDocument(rows)
		if err != nil {
			return nil, fmt.Errorf("note classify backfill candidates: scan: %w", err)
		}
		out = append(out, doc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("note classify backfill candidates: %w", err)
	}
	return out, nil
}

// notesEnrichCounts is NoteEnrichBackfillCounts' return value: how many
// notes the enrich backfill would touch, and the chars/4 token-estimate
// input notesEnrichBackfillHandler's dry run reports - the same shape
// EmbeddingCounts' CharsPending gives documentsEmbeddingsBackfillHandler's
// own dry run (documents_store.go), just without that call's extra
// chunk-level fields, which have no equivalent here: this counts whole
// notes, not chunks.
type notesEnrichCounts struct {
	NotesPending int
	CharsPending int
}

// NoteEnrichBackfillCounts reports how many kind='note' documents currently
// have enrich=0 (never sent for Mate enrichment) and the character count of
// their already-extracted markdown (documents.markdown - populated at
// create/edit time regardless of the enrich flag, since local extraction
// always runs; see extractTextFile's doc comment, documents_extract.go).
// Read-only: this starts no work, matching the ?dry_run=1 contract
// documentsEmbeddingsBackfillHandler's own dry run keeps.
func (s *documentStore) NoteEnrichBackfillCounts() (notesEnrichCounts, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT markdown FROM documents WHERE kind = 'note' AND enrich = 0`)
	if err != nil {
		return notesEnrichCounts{}, fmt.Errorf("note enrich backfill counts: %w", err)
	}
	defer rows.Close()

	var counts notesEnrichCounts
	for rows.Next() {
		var markdown string
		if err := rows.Scan(&markdown); err != nil {
			return notesEnrichCounts{}, fmt.Errorf("note enrich backfill counts: scan: %w", err)
		}
		counts.NotesPending++
		counts.CharsPending += utf8.RuneCountInString(markdown)
	}
	if err := rows.Err(); err != nil {
		return notesEnrichCounts{}, fmt.Errorf("note enrich backfill counts: %w", err)
	}
	return counts, nil
}

// EnrichBackfillNotes sets enrich=1 on every kind='note' document that
// doesn't already carry it, in ONE UPDATE, and requeues each for
// extraction/enrichment the same way MarkReindex (documents_store.go) does
// for a single document's "Reindex…" action: status back to 'pending',
// stage back to 'extract', force_ocr along for the ride even though a note
// never needs OCR (there is no narrower "just flip enrich" write, and
// force_ocr=1 on a note is harmless - extractTextFile never looks at it),
// reindex_seq bumped so a pass already mid-flight on one of these rows can
// tell it was superseded. This does NOT call MarkReindex directly: that
// method takes s.mu itself, and this whole-library update (matched by
// kind/enrich rather than a single id) needs to run as one statement under
// one lock acquisition, not one call - and one round trip - per note.
//
// Returns how many rows were touched, for the backfill endpoint's response.
// Actually starting the indexer on them is the HTTP handler's job
// (wakeDocumentIndexer), the same split documentsEmbeddingsBackfillHandler
// draws between "flip the rows" and "wake the pass that processes them".
func (s *documentStore) EnrichBackfillNotes() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(
		`UPDATE documents SET status = 'pending', stage = 'extract', error = '', force_ocr = 1, enrich = 1, reindex_seq = reindex_seq + 1, updated_at = ?
		 WHERE kind = 'note' AND enrich = 0`,
		s.now().Unix(),
	)
	if err != nil {
		return 0, fmt.Errorf("enrich backfill notes: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("enrich backfill notes: rows affected: %w", err)
	}
	return int(n), nil
}
