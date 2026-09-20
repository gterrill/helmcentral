package main

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// This file is the manuals-scoped half of documentStore (documents_store.go):
// plan §2's document_folders.role column flags a TOP-LEVEL folder as one of
// the boat's authored manuals rather than an ordinary collection, and
// everything here reads or writes that flag and the ordering
// (document_folders.sort_index / documents.sort_index) it earns a folder
// once it carries one. A manual is not a separate table - it is a
// document_folders row like any other, browsed through the exact same
// ListFolder a plain collection uses (documents_store.go) - so this file
// only adds what a manual needs and a plain folder does not: listing every
// flagged folder library-wide, flagging/creating one, clearing the flag
// back off, an ordered whole-subtree read (a manual's own reading view
// wants its entire table of contents in one call, not one level at a
// time), and the whole-sibling-list reorder the Manuals panel's Arrange
// mode drives.
//
// Two invariants plan §2 assigns to Go because SQLite cannot express
// either one itself (ALTER TABLE ADD COLUMN cannot add a table-level CHECK
// at all - see the schema comment on role, documents_store.go):
//
//   - role<>'' is only valid when parent_id IS NULL - enforced by
//     FlagManual/CreateManual, both of which only ever touch a top-level
//     folder (errManualNotTopLevel otherwise).
//   - a manual's name must be unique "among manuals" - already the
//     STRONGER guarantee document_folders_parent_name (documents_store.go's
//     schema) gives every top-level folder regardless of role, since every
//     manual's parent_id is NULL and that index is unique on
//     (COALESCE(parent_id,''), lower(name)). No separate manuals-only
//     uniqueness check is needed here; folderNameTaken/errFolderNameTaken
//     already cover it.

var (
	// errNotAManual is returned by any method that reads or writes a
	// SPECIFIC folder as a manual (ClearManual, ManualTree,
	// ReorderManualChildren) when that folder exists but is not currently
	// flagged role='manual'. Demoting, reading the tree of, or reordering
	// within a folder the operator does not think of as a manual is a real
	// error, not a silent no-op or an empty result.
	errNotAManual = errors.New("folder is not a manual")

	// errManualNotTopLevel is returned by FlagManual (and, defensively,
	// would be by CreateManual if it were ever asked to nest - it never is,
	// by construction) when the folder in question has a parent. This is
	// plan §2's first Go-enforced invariant.
	errManualNotTopLevel = errors.New("only a top-level folder can be a manual")
)

// manualSummary is one row of ListManuals' output, and the shape
// CreateManual/FlagManual return too (plan §4's POST /api/manuals): a
// document_folders row currently flagged role='manual', its name, how many
// documents sit anywhere in its subtree (DocumentCount - recursively,
// since a manual's sections are as much subfolders as directly-filed notes
// and files), and when the folder itself was last touched.
type manualSummary struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	DocumentCount int       `json:"document_count"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// manualTreeNode is one node of ManualTree's output: either a folder (with
// its own further Children) or a document (always a leaf - a note or a
// filed PDF doesn't nest). Type distinguishes the two so a client can
// choose an icon without inspecting which of the document-only fields
// (Kind/NoteType/MIME) are populated - all three are the zero value on a
// folder node. Name is a folder's own name, or a document's title when it
// has one, else its filename - the same "what do I call this" precedence
// the ordering query itself sorts by.
type manualTreeNode struct {
	Type      string    `json:"type"`
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	SortIndex int       `json:"sort_index"`
	UpdatedAt time.Time `json:"updated_at"`

	// Document-only. Always zero-valued on a Type=="folder" node.
	Kind     string `json:"kind,omitempty"`
	NoteType string `json:"note_type,omitempty"`
	MIME     string `json:"mime,omitempty"`

	// Folder-only. Always nil on a Type=="document" node (a document is a
	// leaf), and nil rather than empty on a childless folder too - omitempty
	// keeps a leaf folder's JSON as bare as a document's.
	Children []manualTreeNode `json:"children,omitempty"`
}

// manualReorderItem is one entry of ReorderManualChildren's items: a
// single sibling's new position. Kind is "folder" or "document" - a
// manual's children are a mix of both (plan §2), so the id alone doesn't
// say which table to write.
type manualReorderItem struct {
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	SortIndex int    `json:"sort_index"`
}

// manualSubtreeDocumentCount returns how many documents.rows sit anywhere
// in folderID's own subtree (folderID's own direct children plus every
// descendant folder's), the exact recursive count ListManuals reports per
// manual. Built the same way rebuildMetaChunksUnderFolderTx
// (documents_store.go) turns folderSubtreeIDs' id list into an IN clause,
// rather than a hand-rolled recursive CTE of its own, so this file doesn't
// carry a second way of walking a folder subtree.
func manualSubtreeDocumentCount(q sqlQueryer, folderID string) (int, error) {
	ids, err := folderSubtreeIDs(q, folderID)
	if err != nil {
		return 0, fmt.Errorf("manual document count: %w", err)
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	var n int
	err = q.QueryRow(`SELECT COUNT(*) FROM documents WHERE folder_id IN (`+strings.Join(placeholders, ",")+`)`, args...).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("manual document count: %w", err)
	}
	return n, nil
}

// CreateManual creates a brand new TOP-LEVEL folder with role='manual' set
// in the same INSERT, rather than CreateFolder (documents_store.go)
// followed by a separate flag step - a crash between the two would
// otherwise leave an ordinary, unflagged folder behind with nothing
// operator-visible to say the create had only half-landed. name goes
// through the same validateFolderName/folderNameTaken checks CreateFolder
// itself runs; see this file's own top comment on why no SEPARATE
// manuals-only uniqueness check is needed.
func (s *documentStore) CreateManual(name string) (manualSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	trimmed, err := validateFolderName(name)
	if err != nil {
		return manualSummary{}, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return manualSummary{}, fmt.Errorf("create manual: begin: %w", err)
	}
	defer tx.Rollback()

	taken, err := folderNameTaken(tx, nil, trimmed, "")
	if err != nil {
		return manualSummary{}, fmt.Errorf("create manual: check name: %w", err)
	}
	if taken {
		return manualSummary{}, errFolderNameTaken
	}

	now := s.now()
	id := uuid.NewString()
	if _, err := tx.Exec(
		`INSERT INTO document_folders (id, parent_id, name, created_at, updated_at, role) VALUES (?, NULL, ?, ?, ?, 'manual')`,
		id, trimmed, now.Unix(), now.Unix(),
	); err != nil {
		return manualSummary{}, fmt.Errorf("create manual: insert: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return manualSummary{}, fmt.Errorf("create manual: commit: %w", err)
	}
	return manualSummary{ID: id, Name: trimmed, DocumentCount: 0, UpdatedAt: now}, nil
}

// FlagManual marks an EXISTING top-level folder role='manual' - the second
// way plan §4's POST /api/manuals can be called ({folder_id} rather than
// {name}). errManualNotTopLevel if the folder has a parent (plan §2's
// first Go-enforced invariant). Flagging an already-flagged folder is
// idempotent, not an error - the operator clicking the same "make this a
// manual" action twice is not a mistake worth surfacing.
func (s *documentStore) FlagManual(folderID string) (manualSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return manualSummary{}, fmt.Errorf("flag manual: begin: %w", err)
	}
	defer tx.Rollback()

	var name string
	var parentID sql.NullString
	err = tx.QueryRow(`SELECT name, parent_id FROM document_folders WHERE id = ?`, folderID).Scan(&name, &parentID)
	if errors.Is(err, sql.ErrNoRows) {
		return manualSummary{}, errFolderNotFound
	}
	if err != nil {
		return manualSummary{}, fmt.Errorf("flag manual: read folder: %w", err)
	}
	if parentID.Valid {
		return manualSummary{}, errManualNotTopLevel
	}

	now := s.now()
	if _, err := tx.Exec(`UPDATE document_folders SET role = 'manual', updated_at = ? WHERE id = ?`, now.Unix(), folderID); err != nil {
		return manualSummary{}, fmt.Errorf("flag manual: update: %w", err)
	}

	count, err := manualSubtreeDocumentCount(tx, folderID)
	if err != nil {
		return manualSummary{}, err
	}

	if err := tx.Commit(); err != nil {
		return manualSummary{}, fmt.Errorf("flag manual: commit: %w", err)
	}
	return manualSummary{ID: folderID, Name: name, DocumentCount: count, UpdatedAt: now}, nil
}

// ClearManual demotes id back to an ordinary collection: role resets to ”
// and updated_at is stamped, and NOTHING ELSE - no note or document
// beneath it moves, no subfolder is touched, and no row outside this one
// document_folders id is written at all. This is the whole method plan §2
// promises when it says clearing the flag "demotes a manual to an
// ordinary collection without moving a single note": it keeps that promise
// by not doing anything except this one column, not by doing a careful
// no-op move. errNotAManual if id exists but isn't currently role='manual'
// - a demote on a folder the operator doesn't think of as a manual is a
// real error, not a silent no-op.
func (s *documentStore) ClearManual(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var role string
	err := s.db.QueryRow(`SELECT role FROM document_folders WHERE id = ?`, id).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return errFolderNotFound
	}
	if err != nil {
		return fmt.Errorf("clear manual: read folder: %w", err)
	}
	if role != "manual" {
		return errNotAManual
	}

	res, err := s.db.Exec(`UPDATE document_folders SET role = '', updated_at = ? WHERE id = ?`, s.now().Unix(), id)
	if err != nil {
		return fmt.Errorf("clear manual: %w", err)
	}
	return checkRowsAffected(res, errFolderNotFound)
}

// ListManuals returns every document_folders row currently flagged
// role='manual' - there can be any number at once (plan §2: "any number of
// folders may carry it", the index behind document_folders.role is plain,
// not unique) - alphabetically by name, each with its recursive document
// count. The listing query itself is closed and fully drained before
// manualSubtreeDocumentCount runs its own query per manual: this store's
// pool is one connection wide (documentStore's own doc comment), so a
// second query issued while the first's rows are still open would block
// forever waiting for a connection that will never free up.
func (s *documentStore) ListManuals() ([]manualSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT id, name, updated_at FROM document_folders WHERE role = 'manual' ORDER BY lower(name)`)
	if err != nil {
		return nil, fmt.Errorf("list manuals: %w", err)
	}
	type manualRow struct {
		id, name  string
		updatedAt int64
	}
	var raw []manualRow
	for rows.Next() {
		var r manualRow
		if err := rows.Scan(&r.id, &r.name, &r.updatedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("list manuals: scan: %w", err)
		}
		raw = append(raw, r)
	}
	scanErr := rows.Err()
	rows.Close()
	if scanErr != nil {
		return nil, fmt.Errorf("list manuals: %w", scanErr)
	}

	out := make([]manualSummary, 0, len(raw))
	for _, r := range raw {
		count, err := manualSubtreeDocumentCount(s.db, r.id)
		if err != nil {
			return nil, err
		}
		out = append(out, manualSummary{ID: r.id, Name: r.name, DocumentCount: count, UpdatedAt: time.Unix(r.updatedAt, 0).UTC()})
	}
	return out, nil
}

// ManualTree returns manualID's entire subtree - folders and documents
// interleaved into one ordered list at every level (plan §2/§4), recursed
// all the way down - as the single tree a manual's own reading view wants,
// root node first. errFolderNotFound if manualID doesn't exist,
// errNotAManual if it exists but isn't currently role='manual'.
func (s *documentStore) ManualTree(manualID string) (manualTreeNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var name, role string
	var sortIndex int
	var updatedAt int64
	err := s.db.QueryRow(`SELECT name, role, sort_index, updated_at FROM document_folders WHERE id = ?`, manualID).
		Scan(&name, &role, &sortIndex, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return manualTreeNode{}, errFolderNotFound
	}
	if err != nil {
		return manualTreeNode{}, fmt.Errorf("manual tree: read manual: %w", err)
	}
	if role != "manual" {
		return manualTreeNode{}, errNotAManual
	}

	root := manualTreeNode{
		Type:      "folder",
		ID:        manualID,
		Name:      name,
		SortIndex: sortIndex,
		UpdatedAt: time.Unix(updatedAt, 0).UTC(),
	}
	children, err := s.manualTreeChildren(s.db, manualID)
	if err != nil {
		return manualTreeNode{}, err
	}
	root.Children = children
	return root, nil
}

// manualTreeChildren returns parentID's immediate children - subfolders and
// directly-filed documents merged into ONE list, ordered sort_index then
// lower(name) (plan §2: "Ordering becomes ORDER BY sort_index, lower(name)"
// - ties fall back to name, which the single ORDER BY across the UNION ALL
// gives for free) - and recurses into every subfolder so the whole subtree
// comes back from one top-level call. This is deliberately its OWN query
// rather than a second caller threaded through ListFolder
// (documents_store.go's own "one level of folder browsing, two separate
// slices" contract, which the generic Documents panel wants): a manual's
// reading view wants its whole table of contents, folders and documents
// interleaved in actual reading order, in one shape - the same reasoning
// ListNotes (notes_store.go) gives for not overloading queryDocuments.
func (s *documentStore) manualTreeChildren(q sqlQueryer, parentID string) ([]manualTreeNode, error) {
	// The UNION ALL is wrapped in a subquery rather than carrying its own
	// ORDER BY: SQLite only allows a compound SELECT's own ORDER BY to
	// reference a column by name or position, not an arbitrary expression
	// like lower(display_name) - "2nd ORDER BY term does not match any
	// column in the result set" is what that restriction says when hit
	// directly. Treating the UNION ALL as an ordinary derived table sidesteps
	// it: the outer SELECT is a plain single-"table" query, where ordering by
	// an expression over its own output columns is unremarkable.
	rows, err := q.Query(`
		SELECT * FROM (
			SELECT 'folder' AS item_type, id, name AS display_name, sort_index, updated_at,
			       '' AS doc_kind, '' AS doc_note_type, '' AS doc_mime
			FROM document_folders WHERE parent_id = ?
			UNION ALL
			SELECT 'document' AS item_type, id,
			       CASE WHEN trim(title) <> '' THEN title ELSE filename END AS display_name,
			       sort_index, updated_at, kind AS doc_kind, note_type AS doc_note_type, mime AS doc_mime
			FROM documents WHERE folder_id = ?
		)
		ORDER BY sort_index, lower(display_name)`,
		parentID, parentID,
	)
	if err != nil {
		return nil, fmt.Errorf("manual tree children: %w", err)
	}

	type childRow struct {
		itemType, id, name, kind, noteType, mime string
		sortIndex                                int
		updatedAt                                int64
	}
	var raw []childRow
	for rows.Next() {
		var r childRow
		if err := rows.Scan(&r.itemType, &r.id, &r.name, &r.sortIndex, &r.updatedAt, &r.kind, &r.noteType, &r.mime); err != nil {
			rows.Close()
			return nil, fmt.Errorf("manual tree children: scan: %w", err)
		}
		raw = append(raw, r)
	}
	scanErr := rows.Err()
	rows.Close()
	if scanErr != nil {
		return nil, fmt.Errorf("manual tree children: %w", scanErr)
	}

	// rows is fully closed before any recursive call below issues a further
	// query on the same single-connection pool - the identical ordering
	// ListManuals' own doc comment explains.
	out := make([]manualTreeNode, 0, len(raw))
	for _, r := range raw {
		node := manualTreeNode{
			Type:      r.itemType,
			ID:        r.id,
			Name:      r.name,
			SortIndex: r.sortIndex,
			UpdatedAt: time.Unix(r.updatedAt, 0).UTC(),
		}
		if r.itemType == "document" {
			node.Kind = r.kind
			node.NoteType = r.noteType
			node.MIME = r.mime
		} else {
			children, err := s.manualTreeChildren(q, r.id)
			if err != nil {
				return nil, err
			}
			node.Children = children
		}
		out = append(out, node)
	}
	return out, nil
}

// ManualSectionNames returns manualID's immediate child FOLDER names only -
// not the documents filed directly under it - ordered sort_index then
// lower(name), the same ordering manualTreeChildren uses for a manual's
// full subtree, one level deep. This is Mate's manual index
// (manualIndexLine, assistant_prompt.go): a section is something with its
// own sub-contents, which is what makes it worth naming to the model as a
// clause of its own ("Operations Manual (Before Leaving, Getting
// Underway)") - a document filed loose at the manual's top level isn't a
// section and would only pad that line with titles a search_documents call
// already finds. Deliberately its own query rather than reusing
// manualTreeChildren (which interleaves folders and documents, and
// recurses): this needs exactly one level, folders only.
func (s *documentStore) ManualSectionNames(manualID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT name FROM document_folders WHERE parent_id = ? ORDER BY sort_index, lower(name)`, manualID)
	if err != nil {
		return nil, fmt.Errorf("manual section names: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("manual section names: scan: %w", err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("manual section names: %w", err)
	}
	return out, nil
}

// ReorderManualChildren applies a fresh sort_index to every item in items,
// all under parentID, in ONE transaction - following MoveDocuments'
// precedent (documents_store.go): an id items names that isn't actually a
// CHILD of parentID (wrong id, wrong kind, or simply doesn't exist) fails
// the WHOLE call, never a partial reorder that silently drops the ids it
// doesn't recognise and leaves two sections both claiming position 3 (plan
// §4's own reasoning for why this is whole-sibling-list, not per-item).
//
// manualID must itself be role='manual' (errNotAManual otherwise,
// errFolderNotFound if it doesn't exist at all) and parentID must be
// manualID itself or one of its descendants (errFolderNotFound otherwise)
// - a reorder call is always scoped to ONE manual's own tree, never able to
// touch a sibling list that belongs to a different manual or an ordinary
// collection just because the caller supplied the "wrong" parent_id
// alongside a manual's own :id.
func (s *documentStore) ReorderManualChildren(manualID, parentID string, items []manualReorderItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("reorder manual children: begin: %w", err)
	}
	defer tx.Rollback()

	var role string
	err = tx.QueryRow(`SELECT role FROM document_folders WHERE id = ?`, manualID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return errFolderNotFound
	}
	if err != nil {
		return fmt.Errorf("reorder manual children: read manual: %w", err)
	}
	if role != "manual" {
		return errNotAManual
	}

	subtree, err := folderSubtreeIDs(tx, manualID)
	if err != nil {
		return fmt.Errorf("reorder manual children: subtree: %w", err)
	}
	inSubtree := false
	for _, id := range subtree {
		if id == parentID {
			inSubtree = true
			break
		}
	}
	if !inSubtree {
		return errFolderNotFound
	}

	now := s.now().Unix()
	for _, item := range items {
		switch item.Kind {
		case "folder":
			res, err := tx.Exec(
				`UPDATE document_folders SET sort_index = ?, updated_at = ? WHERE id = ? AND parent_id = ?`,
				item.SortIndex, now, item.ID, parentID,
			)
			if err != nil {
				return fmt.Errorf("reorder manual children: update folder %s: %w", item.ID, err)
			}
			if err := checkRowsAffected(res, errFolderNotFound); err != nil {
				return err
			}
		case "document":
			res, err := tx.Exec(
				`UPDATE documents SET sort_index = ?, updated_at = ? WHERE id = ? AND folder_id = ?`,
				item.SortIndex, now, item.ID, parentID,
			)
			if err != nil {
				return fmt.Errorf("reorder manual children: update document %s: %w", item.ID, err)
			}
			if err := checkRowsAffected(res, errDocumentNotFound); err != nil {
				return err
			}
		default:
			// The handler validates kind before ever calling this method
			// (manuals_handlers.go) - reaching here means something upstream
			// didn't, which is a real bug, not a request-shape problem this
			// method should quietly paper over (AGENTS.md's fallback
			// policy).
			return fmt.Errorf("reorder manual children: unknown item kind %q for id %s", item.Kind, item.ID)
		}
	}

	return tx.Commit()
}
