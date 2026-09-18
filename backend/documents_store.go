package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// document is one row of the documents table: a file's metadata and
// indexing state. The bytes themselves live on disk, named by the lowercase
// hex of SHA256 with no extension (ADR 0106) - this struct never carries
// file content, only what describes and indexes it.
type document struct {
	ID           string     `json:"id"`
	SHA256       string     `json:"sha256"`
	FolderID     *string    `json:"folder_id,omitempty"`
	Filename     string     `json:"filename"`
	Title        string     `json:"title,omitempty"`
	Notes        string     `json:"notes,omitempty"`
	MIME         string     `json:"mime,omitempty"`
	SizeBytes    int64      `json:"size_bytes,omitempty"`
	PageCount    int        `json:"page_count,omitempty"`
	Summary      string     `json:"summary,omitempty"`
	Markdown     string     `json:"markdown,omitempty"`
	Status       string     `json:"status"`
	Stage        string     `json:"stage"`
	Enrich       bool       `json:"enrich"`
	ForceOCR     bool       `json:"force_ocr,omitempty"`
	IndexedWith  string     `json:"indexed_with,omitempty"`
	Error        string     `json:"error,omitempty"`
	IndexModel   string     `json:"index_model,omitempty"`
	IndexCostUSD float64    `json:"index_cost_usd,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	IndexedAt    *time.Time `json:"indexed_at,omitempty"`

	// ReindexSeq is a fencing token, internal only (never serialised):
	// MarkReindex bumps it every time the operator explicitly requeues this
	// document, and the indexer records the value it saw when it popped a
	// document off the queue (NextPending) so its own eventual
	// SetIndexedIfCurrent call can tell "nobody touched this while I was
	// working on it" apart from "a fresh reindex landed mid-pass" - review
	// finding at documents_handlers.go:791.
	ReindexSeq int `json:"-"`

	// OperatorTags and SuggestedTags are populated by Get/GetBySHA/List/
	// Search/ListFolder/NextPending from document_tags. As an INPUT to
	// Insert, OperatorTags seeds the document's initial operator tag set
	// (an upload's own "tags" field - documents_handlers.go) written in the
	// same transaction as the row itself; every other write comes from
	// UpdateMeta (which replaces the operator set) or SetSuggested (B4's
	// enrich step, which never overwrites an operator tag of the same
	// text).
	OperatorTags  []string `json:"operator_tags,omitempty"`
	SuggestedTags []string `json:"suggested_tags,omitempty"`
}

// documentFolder is one row of document_folders: a virtual folder that
// exists only in SQLite. The bytes on disk stay flat and hash-named
// regardless of how documents are filed (ADR 0106) - moving or renaming a
// folder never touches the filesystem, only these rows and the affected
// documents' meta chunks.
type documentFolder struct {
	ID        string    `json:"id"`
	ParentID  *string   `json:"parent_id,omitempty"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// documentChunk is one row of document_chunks: a span of a document's text,
// searchable through document_chunks_fts. Source is "meta" (seq 0, one per
// document, rebuilt whenever title/filename/folder/tags/summary/notes
// change), "local" (extracted text, B2) or "ocr" (Mate's transcription,
// B4). ID is the SQLite rowid, required as-is by content_rowid='id' on the
// FTS5 table below.
type documentChunk struct {
	ID         int64  `json:"id,omitempty"`
	DocumentID string `json:"document_id,omitempty"`
	Seq        int    `json:"seq"`
	Source     string `json:"source"`
	PageStart  int    `json:"page_start,omitempty"`
	PageEnd    int    `json:"page_end,omitempty"`
	Heading    string `json:"heading,omitempty"`
	Text       string `json:"text"`
}

// documentTag is one row of document_tags: a tag on a document, either
// typed by the operator (UpdateMeta) or proposed by Mate during enrichment
// (SetSuggested). The (document_id, tag) primary key is what lets
// SetSuggested's INSERT OR IGNORE leave an existing operator tag alone
// rather than silently reclassifying it as merely suggested.
type documentTag struct {
	DocumentID string `json:"document_id"`
	Tag        string `json:"tag"`
	Source     string `json:"source"`
}

// documentTagCount is one row of TagCounts' output: a tag (operator- or
// suggested-sourced, counted together) and how many distinct documents
// carry it - the B3 API's GET /api/documents/tags chip list
// (documents_handlers.go).
type documentTagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// documentSearchResult is one row of Search's output: the best-scoring
// chunk for a matched document, not a raw document_chunks row - Search
// collapses however many chunks of a document matched down to the single
// best one before this struct is built. FolderID rides along so a caller
// that wants a folder path (assistant_tools.go's search_documents tool) can
// resolve it straight from this row - one query per distinct folder, cached
// across hits - rather than running its own Get per hit just to learn which
// folder it was already sitting in (a review finding: that used to be an
// N+1 Get+FolderPath per result, silently swallowing whatever it errored
// on).
type documentSearchResult struct {
	DocumentID string  `json:"document_id"`
	Filename   string  `json:"filename"`
	Title      string  `json:"title,omitempty"`
	Status     string  `json:"status"`
	FolderID   *string `json:"folder_id,omitempty"`
	PageStart  int     `json:"page,omitempty"`
	Heading    string  `json:"heading,omitempty"`
	Snippet    string  `json:"snippet"`
	Score      float64 `json:"-"`
}

// documentSweepResult is sweepDocumentsDir's report: how many abandoned
// upload temp files it removed, and how many mismatches between disk and
// database it merely logged. Returned (rather than only logged) so the boot
// sweep's behavior is directly assertable in tests without scraping log
// output.
type documentSweepResult struct {
	RemovedTemp  int
	OrphanFiles  int
	MissingFiles int
}

// Sentinel errors, checked with errors.Is by callers (B3's handlers) that
// need to distinguish these specific, expected conditions from every other
// database failure - AGENTS.md's fallback policy: fail fast and surface
// explicitly rather than mask.
var (
	// errDocumentNotFound is returned by every method that operates on a
	// specific document id that does not exist.
	errDocumentNotFound = errors.New("document not found")

	// errDocumentDuplicate is returned by Insert when a document with the
	// same sha256 already exists. The document returned alongside it is the
	// existing row, not the input - callers use its ID rather than issuing a
	// second OCR charge for bytes already on file.
	errDocumentDuplicate = errors.New("document already exists")

	// errFolderNotFound is returned whenever a folder id referenced by a
	// call - as the target itself, or as a parent/destination - does not
	// exist in document_folders.
	errFolderNotFound = errors.New("folder not found")

	// errFolderCycle is returned by MoveFolder when the requested parent is
	// the folder itself or one of its own descendants.
	errFolderCycle = errors.New("folder move would create a cycle")

	// errFolderNotEmpty is returned by DeleteFolder when the folder still
	// has subfolders or documents. There is no recursive delete.
	errFolderNotEmpty = errors.New("folder is not empty")

	// errFolderNameTaken is returned by CreateFolder/RenameFolder/MoveFolder
	// when another folder with the same name (case-insensitively) already
	// exists under the same parent (root included).
	errFolderNameTaken = errors.New("folder name already exists")

	// errFolderNameInvalid is returned when a folder name, after trimming,
	// is empty, over 120 characters, or contains "/".
	errFolderNameInvalid = errors.New("invalid folder name")
)

// globalDocumentStore is the process-wide document store (ADR 0106), opened
// once in main() and shared by the upload/search/folder handlers (B3) and
// the background indexer (B4). nil until initialised in main(), mirroring
// globalAssistantStore (assistant_store.go).
var globalDocumentStore *documentStore

func documentsDBPath() string {
	return cacheFilePath("DOCUMENTS_DB_PATH", "data/documents.sqlite")
}

func documentsDirPath() string {
	return cacheFilePath("DOCUMENTS_DIR", "data/documents")
}

// documentStore is the SQLite-backed store behind the document library:
// metadata, virtual folders, tags, chunks and FTS5 search. Modelled on
// assistantStore (assistant_store.go): a single-connection pool (modernc/
// sqlite surfaces concurrent writes as "database is locked" rather than
// queueing them itself), a mutex serializing every method since the pool
// is one connection wide, and an injectable now so tests can control
// timestamps without a real sleep.
type documentStore struct {
	mu  sync.Mutex
	db  *sql.DB
	now func() time.Time
}

// newDocumentStore opens (creating if necessary) the SQLite database at
// dbPath and ensures every table, index, and FTS5 trigger exists. No WAL:
// unlike tile_cache.go/nearby_contacts.go, this store's whole point is that
// the backup unit is exactly one db file plus one folder of hash-named
// files (ADR 0106) - a WAL sidecar file would break that. foreign_keys(1)
// is required (not merely nice to have): documents.folder_id's ON DELETE
// RESTRICT, document_tags/document_chunks' ON DELETE CASCADE, and the FTS5
// triggers' correctness after a cascade all depend on it actually being
// enforced, so it is read back and verified rather than trusted - modernc's
// _pragma DSN parameters are otherwise silently unverified.
func newDocumentStore(dbPath string) (*documentStore, error) {
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create documents store directory: %w", err)
		}
	}

	dsn := dbPath + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open documents database: %w", err)
	}
	db.SetMaxOpenConns(1)

	var fk int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		db.Close()
		return nil, fmt.Errorf("check foreign_keys pragma: %w", err)
	}
	if fk != 1 {
		db.Close()
		return nil, fmt.Errorf("foreign_keys pragma did not take effect (got %d, want 1)", fk)
	}

	for _, stmt := range documentStoreSchema {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			return nil, fmt.Errorf("create documents schema: %w", err)
		}
	}

	return &documentStore{db: db, now: func() time.Time { return time.Now().UTC() }}, nil
}

// documentStoreSchema is applied in order by newDocumentStore. Every
// statement is idempotent (IF NOT EXISTS / CREATE TRIGGER IF NOT EXISTS) so
// a fresh path and a pre-existing one open the same way, matching every
// other store's CREATE TABLE IF NOT EXISTS convention in this codebase.
var documentStoreSchema = []string{
	`CREATE TABLE IF NOT EXISTS document_folders (
		id         TEXT PRIMARY KEY,
		parent_id  TEXT REFERENCES document_folders(id),
		name       TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	)`,
	// A UNIQUE constraint ignores NULLs, so two root-level folders (both
	// parent_id NULL) would never collide under a plain
	// UNIQUE(parent_id, name) constraint. Indexing COALESCE(parent_id,'')
	// instead makes root a normal, collidable "parent" like any other.
	`CREATE UNIQUE INDEX IF NOT EXISTS document_folders_parent_name
		ON document_folders (COALESCE(parent_id, ''), lower(name))`,

	`CREATE TABLE IF NOT EXISTS documents (
		id             TEXT PRIMARY KEY,
		sha256         TEXT NOT NULL UNIQUE,
		folder_id      TEXT REFERENCES document_folders(id) ON DELETE RESTRICT,
		filename       TEXT NOT NULL,
		title          TEXT NOT NULL DEFAULT '',
		notes          TEXT NOT NULL DEFAULT '',
		mime           TEXT NOT NULL DEFAULT '',
		size_bytes     INTEGER NOT NULL DEFAULT 0,
		page_count     INTEGER NOT NULL DEFAULT 0,
		summary        TEXT NOT NULL DEFAULT '',
		markdown       TEXT NOT NULL DEFAULT '',
		status         TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','indexed','failed')),
		stage          TEXT NOT NULL DEFAULT 'extract' CHECK (stage IN ('extract','enrich','done')),
		enrich         INTEGER NOT NULL DEFAULT 0 CHECK (enrich IN (0,1)),
		force_ocr      INTEGER NOT NULL DEFAULT 0 CHECK (force_ocr IN (0,1)),
		indexed_with   TEXT NOT NULL DEFAULT '' CHECK (indexed_with IN ('','local','mate')),
		error          TEXT NOT NULL DEFAULT '',
		index_model    TEXT NOT NULL DEFAULT '',
		index_cost_usd REAL NOT NULL DEFAULT 0,
		reindex_seq    INTEGER NOT NULL DEFAULT 0,
		created_at     INTEGER NOT NULL,
		updated_at     INTEGER NOT NULL,
		indexed_at     INTEGER
	)`,
	`CREATE INDEX IF NOT EXISTS documents_folder_id ON documents (folder_id)`,
	// NextPending's oldest-pending-first scan.
	`CREATE INDEX IF NOT EXISTS documents_status_created_at ON documents (status, created_at)`,

	`CREATE TABLE IF NOT EXISTS document_tags (
		document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
		tag         TEXT NOT NULL,
		source      TEXT NOT NULL CHECK (source IN ('operator','suggested')),
		PRIMARY KEY (document_id, tag)
	)`,
	`CREATE INDEX IF NOT EXISTS document_tags_tag ON document_tags (tag)`,

	`CREATE TABLE IF NOT EXISTS document_chunks (
		id          INTEGER PRIMARY KEY,
		document_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
		seq         INTEGER NOT NULL,
		source      TEXT NOT NULL CHECK (source IN ('meta','local','ocr')),
		page_start  INTEGER NOT NULL DEFAULT 0,
		page_end    INTEGER NOT NULL DEFAULT 0,
		heading     TEXT NOT NULL DEFAULT '',
		text        TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE INDEX IF NOT EXISTS document_chunks_document_id ON document_chunks (document_id)`,

	// External-content FTS5 table: the indexed text lives only in
	// document_chunks, never duplicated into the FTS b-tree itself, kept in
	// sync by the three triggers below. Metadata (the meta chunk) and body
	// text share this one table and one bm25 ranking - see the meta chunk
	// comment on rebuildMetaChunkTx.
	`CREATE VIRTUAL TABLE IF NOT EXISTS document_chunks_fts USING fts5(
		text, heading,
		content='document_chunks', content_rowid='id',
		tokenize='porter unicode61 remove_diacritics 2'
	)`,
	`CREATE TRIGGER IF NOT EXISTS document_chunks_ai AFTER INSERT ON document_chunks BEGIN
		INSERT INTO document_chunks_fts(rowid, text, heading) VALUES (new.id, new.text, new.heading);
	END`,
	`CREATE TRIGGER IF NOT EXISTS document_chunks_ad AFTER DELETE ON document_chunks BEGIN
		INSERT INTO document_chunks_fts(document_chunks_fts, rowid, text, heading) VALUES('delete', old.id, old.text, old.heading);
	END`,
	`CREATE TRIGGER IF NOT EXISTS document_chunks_au AFTER UPDATE ON document_chunks BEGIN
		INSERT INTO document_chunks_fts(document_chunks_fts, rowid, text, heading) VALUES('delete', old.id, old.text, old.heading);
		INSERT INTO document_chunks_fts(rowid, text, heading) VALUES (new.id, new.text, new.heading);
	END`,
}

func (s *documentStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// documentColumns is every documents column in the exact order scanDocument
// expects, shared by every query that reads a full document row so the two
// never drift apart.
const documentColumns = `id, sha256, folder_id, filename, title, notes, mime, size_bytes, page_count,
	summary, markdown, status, stage, enrich, force_ocr, indexed_with, error,
	index_model, index_cost_usd, reindex_seq, created_at, updated_at, indexed_at`

// rowScanner is satisfied by both *sql.Row and *sql.Rows, letting
// scanDocument read either a single QueryRow result or one row of a Query
// loop with the same code.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanDocument(row rowScanner) (document, error) {
	var d document
	var folderID sql.NullString
	var indexedAt sql.NullInt64
	var createdAt, updatedAt int64
	var enrich, forceOCR int

	if err := row.Scan(
		&d.ID, &d.SHA256, &folderID, &d.Filename, &d.Title, &d.Notes, &d.MIME, &d.SizeBytes, &d.PageCount,
		&d.Summary, &d.Markdown, &d.Status, &d.Stage, &enrich, &forceOCR, &d.IndexedWith, &d.Error,
		&d.IndexModel, &d.IndexCostUSD, &d.ReindexSeq, &createdAt, &updatedAt, &indexedAt,
	); err != nil {
		return document{}, err
	}

	if folderID.Valid {
		v := folderID.String
		d.FolderID = &v
	}
	d.Enrich = enrich != 0
	d.ForceOCR = forceOCR != 0
	d.CreatedAt = time.Unix(createdAt, 0).UTC()
	d.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	if indexedAt.Valid {
		t := time.Unix(indexedAt.Int64, 0).UTC()
		d.IndexedAt = &t
	}
	return d, nil
}

// sqlQueryer is satisfied by both *sql.DB and *sql.Tx, letting the read
// helpers below (folderPath, folderSubtreeIDs, folderNameTaken, rowExists,
// documentTagsOf, queryDocuments) run identically whether called at the top
// of a public method or from inside another method's transaction.
type sqlQueryer interface {
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
}

// rowExists reports whether query (expected to SELECT a single sentinel
// column such as "1") returns any row.
func rowExists(q sqlQueryer, query string, args ...any) (bool, error) {
	var x int
	err := q.QueryRow(query, args...).Scan(&x)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func checkRowsAffected(res sql.Result, notFoundErr error) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return notFoundErr
	}
	return nil
}

func nullableString(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// validateFolderName trims name and enforces the 1-120 character, no-"/"
// rule shared by CreateFolder and RenameFolder.
func validateFolderName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	n := utf8.RuneCountInString(trimmed)
	if n < 1 || n > 120 || strings.Contains(trimmed, "/") {
		return "", errFolderNameInvalid
	}
	return trimmed, nil
}

// folderNameTaken reports whether another folder (any id other than
// excludeID) already has name (case-insensitively) under parentID.
// excludeID is "" for a brand new folder (CreateFolder), where nothing
// should be excluded.
func folderNameTaken(q sqlQueryer, parentID *string, name, excludeID string) (bool, error) {
	var parentKey string
	if parentID != nil {
		parentKey = *parentID
	}
	return rowExists(q,
		`SELECT 1 FROM document_folders WHERE COALESCE(parent_id,'') = ? AND lower(name) = lower(?) AND id != ? LIMIT 1`,
		parentKey, name, excludeID)
}

// folderPath returns id's ancestor chain root-first, id itself last, via a
// recursive CTE that walks parent_id upward. errFolderNotFound if id does
// not exist.
func folderPath(q sqlQueryer, id string) ([]documentFolder, error) {
	rows, err := q.Query(`
		WITH RECURSIVE path(id, parent_id, name, created_at, updated_at, depth) AS (
			SELECT id, parent_id, name, created_at, updated_at, 0 FROM document_folders WHERE id = ?
			UNION ALL
			SELECT f.id, f.parent_id, f.name, f.created_at, f.updated_at, path.depth + 1
			FROM document_folders f JOIN path ON f.id = path.parent_id
		)
		SELECT id, parent_id, name, created_at, updated_at FROM path ORDER BY depth DESC`, id)
	if err != nil {
		return nil, fmt.Errorf("folder path: %w", err)
	}
	defer rows.Close()

	var out []documentFolder
	for rows.Next() {
		var f documentFolder
		var parentID sql.NullString
		var created, updated int64
		if err := rows.Scan(&f.ID, &parentID, &f.Name, &created, &updated); err != nil {
			return nil, fmt.Errorf("folder path: scan: %w", err)
		}
		if parentID.Valid {
			v := parentID.String
			f.ParentID = &v
		}
		f.CreatedAt = time.Unix(created, 0).UTC()
		f.UpdatedAt = time.Unix(updated, 0).UTC()
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("folder path: %w", err)
	}
	if len(out) == 0 {
		return nil, errFolderNotFound
	}
	return out, nil
}

// folderSubtreeIDs returns rootID plus every descendant folder id, via a
// recursive CTE walking parent_id downward. errFolderNotFound if rootID
// does not exist - a bogus folder id filters to "nothing" only by way of an
// explicit error, never silently.
func folderSubtreeIDs(q sqlQueryer, rootID string) ([]string, error) {
	ok, err := rowExists(q, `SELECT 1 FROM document_folders WHERE id = ?`, rootID)
	if err != nil {
		return nil, fmt.Errorf("folder subtree: check root: %w", err)
	}
	if !ok {
		return nil, errFolderNotFound
	}

	rows, err := q.Query(`
		WITH RECURSIVE sub(id) AS (
			SELECT id FROM document_folders WHERE id = ?
			UNION ALL
			SELECT f.id FROM document_folders f JOIN sub ON f.parent_id = sub.id
		)
		SELECT id FROM sub`, rootID)
	if err != nil {
		return nil, fmt.Errorf("folder subtree: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("folder subtree: scan: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// documentTagsOf returns docID's tags split by source.
func documentTagsOf(q sqlQueryer, docID string) (operator, suggested []string, err error) {
	rows, err := q.Query(`SELECT tag, source FROM document_tags WHERE document_id = ? ORDER BY tag`, docID)
	if err != nil {
		return nil, nil, fmt.Errorf("document tags: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var tag, source string
		if err := rows.Scan(&tag, &source); err != nil {
			return nil, nil, fmt.Errorf("document tags: scan: %w", err)
		}
		if source == "operator" {
			operator = append(operator, tag)
		} else {
			suggested = append(suggested, tag)
		}
	}
	return operator, suggested, rows.Err()
}

// TagCounts returns every distinct tag across all documents (operator- and
// suggested-sourced alike), alphabetically, with how many distinct
// documents carry it - the B3 API's GET /api/documents/tags chip list
// (documents_handlers.go).
func (s *documentStore) TagCounts() ([]documentTagCount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT tag, COUNT(DISTINCT document_id) FROM document_tags GROUP BY tag ORDER BY tag`)
	if err != nil {
		return nil, fmt.Errorf("tag counts: %w", err)
	}
	defer rows.Close()

	var out []documentTagCount
	for rows.Next() {
		var tc documentTagCount
		if err := rows.Scan(&tc.Tag, &tc.Count); err != nil {
			return nil, fmt.Errorf("tag counts: scan: %w", err)
		}
		out = append(out, tc)
	}
	return out, rows.Err()
}

// attachTags fills doc.OperatorTags/SuggestedTags from document_tags.
func (s *documentStore) attachTags(q sqlQueryer, doc *document) error {
	operator, suggested, err := documentTagsOf(q, doc.ID)
	if err != nil {
		return err
	}
	doc.OperatorTags = operator
	doc.SuggestedTags = suggested
	return nil
}

// rebuildMetaChunkTx rewrites docID's meta chunk (seq 0, source 'meta'):
// title, filename, folder path, tags (operator plus suggested), summary
// and notes, all in one chunk so metadata and body text share the same
// bm25 ranking (ADR 0106). Called - always inside the caller's own
// transaction - by Insert, UpdateMeta, SetSuggested and MoveDocuments for
// the document itself, and by rebuildMetaChunksUnderFolderTx for every
// document under a renamed or moved folder.
func (s *documentStore) rebuildMetaChunkTx(tx *sql.Tx, docID string) error {
	var title, filename, notes, summary string
	var folderID sql.NullString
	err := tx.QueryRow(`SELECT title, filename, notes, summary, folder_id FROM documents WHERE id = ?`, docID).
		Scan(&title, &filename, &notes, &summary, &folderID)
	if errors.Is(err, sql.ErrNoRows) {
		return errDocumentNotFound
	}
	if err != nil {
		return fmt.Errorf("rebuild meta chunk: read document: %w", err)
	}

	var folderPathText string
	if folderID.Valid {
		path, err := folderPath(tx, folderID.String)
		if err != nil {
			return fmt.Errorf("rebuild meta chunk: folder path: %w", err)
		}
		names := make([]string, len(path))
		for i, f := range path {
			names[i] = f.Name
		}
		folderPathText = strings.Join(names, "/")
	}

	operatorTags, suggestedTags, err := documentTagsOf(tx, docID)
	if err != nil {
		return fmt.Errorf("rebuild meta chunk: tags: %w", err)
	}
	allTags := append(append([]string{}, operatorTags...), suggestedTags...)

	var text strings.Builder
	text.WriteString(title)
	text.WriteByte('\n')
	text.WriteString(filename)
	if folderPathText != "" {
		text.WriteByte('\n')
		text.WriteString(folderPathText)
	}
	if len(allTags) > 0 {
		text.WriteByte('\n')
		text.WriteString(strings.Join(allTags, " "))
	}
	if summary != "" {
		text.WriteByte('\n')
		text.WriteString(summary)
	}
	if notes != "" {
		text.WriteByte('\n')
		text.WriteString(notes)
	}

	if _, err := tx.Exec(`DELETE FROM document_chunks WHERE document_id = ? AND seq = 0 AND source = 'meta'`, docID); err != nil {
		return fmt.Errorf("rebuild meta chunk: delete: %w", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO document_chunks (document_id, seq, source, page_start, page_end, heading, text) VALUES (?, 0, 'meta', 0, 0, ?, ?)`,
		docID, title, text.String(),
	); err != nil {
		return fmt.Errorf("rebuild meta chunk: insert: %w", err)
	}
	return nil
}

// rebuildMetaChunksUnderFolderTx rebuilds the meta chunk of every document
// in folderID's subtree (folderID included), so a rename or move of the
// folder itself is reflected in every affected document's searchable
// folder-path text.
func (s *documentStore) rebuildMetaChunksUnderFolderTx(tx *sql.Tx, folderID string) error {
	ids, err := folderSubtreeIDs(tx, folderID)
	if err != nil {
		return fmt.Errorf("rebuild meta chunks under folder: %w", err)
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	rows, err := tx.Query(`SELECT id FROM documents WHERE folder_id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return fmt.Errorf("rebuild meta chunks under folder: list documents: %w", err)
	}
	var docIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("rebuild meta chunks under folder: scan: %w", err)
		}
		docIDs = append(docIDs, id)
	}
	scanErr := rows.Err()
	rows.Close()
	if scanErr != nil {
		return fmt.Errorf("rebuild meta chunks under folder: %w", scanErr)
	}

	for _, id := range docIDs {
		if err := s.rebuildMetaChunkTx(tx, id); err != nil {
			return err
		}
	}
	return nil
}

// ── documents ────────────────────────────────────────────────────────────

// Insert adds a new document row: pending/extract, with the caller's
// SHA256/Filename/MIME/SizeBytes/FolderID/Enrich/Title/Notes and every
// other field at its zero value, plus doc.OperatorTags (if any) written as
// its initial operator tag set in the same transaction - not a separate
// UpdateMeta call after the fact, which a caller retrying a failed upload
// would silently skip on the resulting duplicate response. On a duplicate
// sha256, Insert instead returns the EXISTING document (not the input's
// fields) wrapped in errDocumentDuplicate -
// callers check errors.Is(err, errDocumentDuplicate) and use the returned
// document's id rather than paying for a second OCR pass over identical
// bytes.
func (s *documentStore) Insert(doc document) (document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, err := scanDocument(s.db.QueryRow(`SELECT `+documentColumns+` FROM documents WHERE sha256 = ?`, doc.SHA256))
	if err == nil {
		if tagErr := s.attachTags(s.db, &existing); tagErr != nil {
			return document{}, tagErr
		}
		return existing, fmt.Errorf("document with sha256 %s already exists as %s: %w", doc.SHA256, existing.ID, errDocumentDuplicate)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return document{}, fmt.Errorf("insert document: check duplicate: %w", err)
	}

	if doc.FolderID != nil {
		ok, err := rowExists(s.db, `SELECT 1 FROM document_folders WHERE id = ?`, *doc.FolderID)
		if err != nil {
			return document{}, fmt.Errorf("insert document: check folder: %w", err)
		}
		if !ok {
			return document{}, errFolderNotFound
		}
	}

	now := s.now()
	doc.ID = uuid.NewString()
	doc.Status = "pending"
	doc.Stage = "extract"
	doc.IndexedWith = ""
	doc.Error = ""
	doc.CreatedAt = now
	doc.UpdatedAt = now
	doc.IndexedAt = nil
	initialTags := doc.OperatorTags

	tx, err := s.db.Begin()
	if err != nil {
		return document{}, fmt.Errorf("insert document: begin: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`INSERT INTO documents (id, sha256, folder_id, filename, title, notes, mime, size_bytes, page_count, summary, markdown, status, stage, enrich, force_ocr, indexed_with, error, index_model, index_cost_usd, created_at, updated_at, indexed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		doc.ID, doc.SHA256, nullableString(doc.FolderID), doc.Filename, doc.Title, doc.Notes, doc.MIME, doc.SizeBytes, doc.PageCount,
		doc.Summary, doc.Markdown, doc.Status, doc.Stage, boolToInt(doc.Enrich), boolToInt(doc.ForceOCR), doc.IndexedWith, doc.Error,
		doc.IndexModel, doc.IndexCostUSD, now.Unix(), now.Unix(), nil,
	); err != nil {
		return document{}, fmt.Errorf("insert document: %w", err)
	}

	if err := insertOperatorTagsTx(tx, doc.ID, initialTags); err != nil {
		return document{}, fmt.Errorf("insert document: tags: %w", err)
	}

	if err := s.rebuildMetaChunkTx(tx, doc.ID); err != nil {
		return document{}, err
	}

	if err := s.attachTags(tx, &doc); err != nil {
		return document{}, err
	}

	if err := tx.Commit(); err != nil {
		return document{}, fmt.Errorf("insert document: commit: %w", err)
	}
	return doc, nil
}

// Get looks up a single document by id, tags included.
// errDocumentNotFound if id does not exist.
func (s *documentStore) Get(id string) (document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	doc, err := scanDocument(s.db.QueryRow(`SELECT `+documentColumns+` FROM documents WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return document{}, errDocumentNotFound
	}
	if err != nil {
		return document{}, fmt.Errorf("get document: %w", err)
	}
	if err := s.attachTags(s.db, &doc); err != nil {
		return document{}, err
	}
	return doc, nil
}

// GetBySHA looks up a single document by its sha256. ok is false, with a
// nil error, when no document has that hash - the normal "no such upload
// yet" case a duplicate check makes on every upload, not a database
// failure.
func (s *documentStore) GetBySHA(sha256 string) (document, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	doc, err := scanDocument(s.db.QueryRow(`SELECT `+documentColumns+` FROM documents WHERE sha256 = ?`, sha256))
	if errors.Is(err, sql.ErrNoRows) {
		return document{}, false, nil
	}
	if err != nil {
		return document{}, false, fmt.Errorf("get document by sha256: %w", err)
	}
	if err := s.attachTags(s.db, &doc); err != nil {
		return document{}, false, err
	}
	return doc, true, nil
}

// documentsRootFolderSentinel is the folderID value queryDocuments and
// Search treat as "the root level" rather than a real folder id - the B3
// HTTP API's "folder=root" query value (documents_handlers.go) resolves to
// a pointer to this. It has to be a sentinel distinct from nil (queryDocuments'
// own doc comment: nil already means "no filter, every document
// everywhere") and distinct from any real folder id, which CreateFolder
// guarantees by always assigning a uuid - "" can never collide with one.
const documentsRootFolderSentinel = ""

// queryDocuments builds and runs the shared filtered-listing query behind
// List: folderID nil means no folder filter at all (every document,
// anywhere); a non-nil folderID scopes to that folder, its full subtree
// too when recursive is true; folderID pointing at documentsRootFolderSentinel
// scopes to documents with no folder at all (or, recursively, to the whole
// tree - root's subtree is everything). This is a distinct contract from
// ListFolder's parentID, where nil means "the root folder" specifically
// rather than "no filter" - the two methods answer different questions (a
// flat/searchable listing vs. one level of folder browsing) and are kept as
// separate query paths rather than forced to share one nil convention.
func queryDocuments(q sqlQueryer, folderID *string, recursive bool, tag string, limit, offset int) ([]document, error) {
	query := `SELECT ` + documentColumns + ` FROM documents d WHERE 1=1`
	var args []any

	if folderID != nil {
		if *folderID == documentsRootFolderSentinel {
			// The root sentinel (see its doc comment): a non-recursive
			// listing is exactly the documents with no folder at all; a
			// recursive one is the whole tree, i.e. no filter, since root's
			// subtree is every folder there is.
			if !recursive {
				query += ` AND d.folder_id IS NULL`
			}
		} else if recursive {
			ids, err := folderSubtreeIDs(q, *folderID)
			if err != nil {
				return nil, err
			}
			placeholders := make([]string, len(ids))
			for i, fid := range ids {
				placeholders[i] = "?"
				args = append(args, fid)
			}
			query += ` AND d.folder_id IN (` + strings.Join(placeholders, ",") + `)`
		} else {
			ok, err := rowExists(q, `SELECT 1 FROM document_folders WHERE id = ?`, *folderID)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, errFolderNotFound
			}
			query += ` AND d.folder_id = ?`
			args = append(args, *folderID)
		}
	}
	if tag != "" {
		query += ` AND EXISTS (SELECT 1 FROM document_tags t WHERE t.document_id = d.id AND t.tag = ?)`
		args = append(args, tag)
	}
	// created_at is whole seconds, so several documents inserted in the same
	// second would otherwise sort in no fixed order between calls (SQLite
	// makes no promise about ties) - unstable across pages of the same
	// listing. d.id as a tiebreaker fixes the order the same way
	// NextPending's own ORDER BY already does.
	query += ` ORDER BY d.created_at DESC, d.id DESC`
	if limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, limit, offset)
	}

	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	defer rows.Close()

	var out []document
	for rows.Next() {
		doc, err := scanDocument(rows)
		if err != nil {
			return nil, fmt.Errorf("list documents: scan: %w", err)
		}
		out = append(out, doc)
	}
	return out, rows.Err()
}

// List returns documents matching the given filters, newest first. limit<=0
// means no LIMIT/OFFSET at all (every matching row). See queryDocuments for
// the folderID/recursive contract.
func (s *documentStore) List(folderID *string, recursive bool, tag string, limit, offset int) ([]document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	docs, err := queryDocuments(s.db, folderID, recursive, tag, limit, offset)
	if err != nil {
		return nil, err
	}
	for i := range docs {
		if err := s.attachTags(s.db, &docs[i]); err != nil {
			return nil, err
		}
	}
	return docs, nil
}

// Count returns the total number of documents - a single SELECT count(*),
// for a caller (collectAssistantPromptContext, assistant_prompt.go) that
// only ever wanted how many there are, not List(nil, false, "", 0, 0)'s
// full rows (every column including markdown, plus one tag query per row)
// just to take len() of the result.
func (s *documentStore) Count() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM documents`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count documents: %w", err)
	}
	return n, nil
}

// Search runs an already-sanitised FTS5 MATCH string (B2's ftsMatchQuery
// sanitises operator input into this form) against document_chunks_fts,
// weighting a heading hit (the meta chunk's title lives in heading) above a
// body hit via bm25(fts,1.0,2.0), and returning a two-marker-delimited
// snippet per SQLite's snippet(). Every matching chunk is read in score
// order (rows.Close, deferred below, makes stopping partway through cheap)
// and collapsed to the single best chunk per document; the first offset
// distinct documents are skipped, and reading stops once limit documents
// have been collected past that point. There is no separate cap on how
// many raw chunk hits are scanned first - an earlier LIMIT 200 there made a
// document unreachable whenever 200 other documents' chunks all outranked
// its own single hit. An empty (post-sanitising) query is "no hits", not an
// error - FTS5 rejects an empty MATCH string outright, so this is checked
// before ever reaching SQL.
func (s *documentStore) Search(query string, folderID *string, recursive bool, tag string, limit, offset int) ([]documentSearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(query) == "" || limit <= 0 {
		return nil, nil
	}

	sqlQuery := `
		SELECT d.id, d.filename, d.title, d.status, d.folder_id, c.page_start, c.heading,
		       snippet(document_chunks_fts, 0, char(2), char(3), '…', 16) AS snippet,
		       bm25(document_chunks_fts, 1.0, 2.0) AS score
		FROM document_chunks_fts
		JOIN document_chunks c ON c.id = document_chunks_fts.rowid
		JOIN documents d ON d.id = c.document_id
		WHERE document_chunks_fts MATCH ?`
	args := []any{query}

	if folderID != nil {
		if *folderID == documentsRootFolderSentinel {
			// See queryDocuments' identical branch and the sentinel's doc
			// comment: non-recursive root means "no folder at all";
			// recursive root means "no filter", since root's subtree is
			// every folder there is.
			if !recursive {
				sqlQuery += ` AND d.folder_id IS NULL`
			}
		} else if recursive {
			ids, err := folderSubtreeIDs(s.db, *folderID)
			if err != nil {
				return nil, err
			}
			placeholders := make([]string, len(ids))
			for i, fid := range ids {
				placeholders[i] = "?"
				args = append(args, fid)
			}
			sqlQuery += ` AND d.folder_id IN (` + strings.Join(placeholders, ",") + `)`
		} else {
			ok, err := rowExists(s.db, `SELECT 1 FROM document_folders WHERE id = ?`, *folderID)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, errFolderNotFound
			}
			sqlQuery += ` AND d.folder_id = ?`
			args = append(args, *folderID)
		}
	}
	if tag != "" {
		sqlQuery += ` AND EXISTS (SELECT 1 FROM document_tags t WHERE t.document_id = d.id AND t.tag = ?)`
		args = append(args, tag)
	}
	sqlQuery += ` ORDER BY score`

	rows, err := s.db.Query(sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("search documents: %w", err)
	}
	defer rows.Close()

	seen := map[string]bool{}
	skipped := 0
	var out []documentSearchResult
	for rows.Next() {
		var r documentSearchResult
		var folderID sql.NullString
		if err := rows.Scan(&r.DocumentID, &r.Filename, &r.Title, &r.Status, &folderID, &r.PageStart, &r.Heading, &r.Snippet, &r.Score); err != nil {
			return nil, fmt.Errorf("scan search result: %w", err)
		}
		if folderID.Valid {
			v := folderID.String
			r.FolderID = &v
		}
		if seen[r.DocumentID] {
			continue
		}
		seen[r.DocumentID] = true
		if skipped < offset {
			skipped++
			continue
		}
		out = append(out, r)
		if len(out) >= limit {
			break
		}
	}
	return out, rows.Err()
}

// insertOperatorTagsTx inserts tags (trimmed, empty entries skipped - the
// same rule an operator retyping the field with stray commas gets) as
// docID's operator tags inside tx, promoting a tag already present as
// merely suggested to operator source rather than duplicating it. Shared by
// Insert (an upload's initial tag set) and updateMetaTx (an operator edit)
// so the two never drift apart on the trim/skip-empty/promote rule.
func insertOperatorTagsTx(tx *sql.Tx, docID string, tags []string) error {
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, err := tx.Exec(
			`INSERT INTO document_tags (document_id, tag, source) VALUES (?, ?, 'operator')
			 ON CONFLICT(document_id, tag) DO UPDATE SET source = 'operator'`,
			docID, tag,
		); err != nil {
			return fmt.Errorf("insert operator tag %q: %w", tag, err)
		}
	}
	return nil
}

// updateMetaTx is UpdateMeta's body, factored out so PatchDocument
// (documents_handlers.go's combined PATCH) can apply title/notes/tags in
// the same transaction as a folder move rather than two separate commits.
// It does not rebuild the meta chunk itself - PatchDocument and UpdateMeta
// each do that once, after every edit they're applying has landed.
func (s *documentStore) updateMetaTx(tx *sql.Tx, now time.Time, id string, title, notes *string, tags []string) error {
	ok, err := rowExists(tx, `SELECT 1 FROM documents WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("update meta: check document: %w", err)
	}
	if !ok {
		return errDocumentNotFound
	}

	if title != nil {
		if _, err := tx.Exec(`UPDATE documents SET title = ?, updated_at = ? WHERE id = ?`, *title, now.Unix(), id); err != nil {
			return fmt.Errorf("update meta: title: %w", err)
		}
	}
	if notes != nil {
		if _, err := tx.Exec(`UPDATE documents SET notes = ?, updated_at = ? WHERE id = ?`, *notes, now.Unix(), id); err != nil {
			return fmt.Errorf("update meta: notes: %w", err)
		}
	}
	if tags != nil {
		if _, err := tx.Exec(`DELETE FROM document_tags WHERE document_id = ? AND source = 'operator'`, id); err != nil {
			return fmt.Errorf("update meta: clear tags: %w", err)
		}
		if err := insertOperatorTagsTx(tx, id, tags); err != nil {
			return fmt.Errorf("update meta: %w", err)
		}
	}
	return nil
}

// UpdateMeta applies operator edits: title and notes update only when
// non-nil (PATCH semantics - omitted fields are left alone), and tags, when
// non-nil, fully REPLACES the operator-sourced tag set (an empty non-nil
// slice clears it). A tag already present as a suggested tag is promoted to
// operator source rather than duplicated. The meta chunk is rebuilt
// afterward so search reflects the edit immediately.
func (s *documentStore) UpdateMeta(id string, title, notes *string, tags []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("update meta: begin: %w", err)
	}
	defer tx.Rollback()

	if err := s.updateMetaTx(tx, s.now(), id, title, notes, tags); err != nil {
		return err
	}
	if err := s.rebuildMetaChunkTx(tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

// SetSuggested records Mate's enrichment proposal (B4): summary is always
// replaced; title only fills in an empty title (an operator's own title is
// never overwritten); each tag is inserted with source 'suggested' via
// INSERT OR IGNORE, so a tag already present as an operator tag keeps that
// source rather than being duplicated or downgraded. The meta chunk is
// rebuilt afterward.
func (s *documentStore) SetSuggested(id, title, summary string, tags []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("set suggested: begin: %w", err)
	}
	defer tx.Rollback()

	var currentTitle string
	if err := tx.QueryRow(`SELECT title FROM documents WHERE id = ?`, id).Scan(&currentTitle); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errDocumentNotFound
		}
		return fmt.Errorf("set suggested: read document: %w", err)
	}

	now := s.now()
	newTitle := currentTitle
	if strings.TrimSpace(currentTitle) == "" {
		newTitle = title
	}
	if _, err := tx.Exec(`UPDATE documents SET title = ?, summary = ?, updated_at = ? WHERE id = ?`, newTitle, summary, now.Unix(), id); err != nil {
		return fmt.Errorf("set suggested: update: %w", err)
	}

	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, err := tx.Exec(
			`INSERT INTO document_tags (document_id, tag, source) VALUES (?, ?, 'suggested') ON CONFLICT(document_id, tag) DO NOTHING`,
			id, tag,
		); err != nil {
			return fmt.Errorf("set suggested: insert tag %q: %w", tag, err)
		}
	}

	if err := s.rebuildMetaChunkTx(tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

// ReplaceChunks atomically swaps every chunk whose source is in sources for
// the given chunks, all in one transaction: neither a partial delete nor a
// partial insert can be observed by a concurrent Search. Does not touch the
// meta chunk (source 'meta') unless the caller explicitly lists it in
// sources - callers replacing extracted or OCR'd body text pass
// []string{"local"} or []string{"local","ocr"}, never "meta".
func (s *documentStore) ReplaceChunks(id string, sources []string, chunks []documentChunk) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("replace chunks: begin: %w", err)
	}
	defer tx.Rollback()

	ok, err := rowExists(tx, `SELECT 1 FROM documents WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("replace chunks: check document: %w", err)
	}
	if !ok {
		return errDocumentNotFound
	}

	for _, source := range sources {
		if _, err := tx.Exec(`DELETE FROM document_chunks WHERE document_id = ? AND source = ?`, id, source); err != nil {
			return fmt.Errorf("replace chunks: delete %s: %w", source, err)
		}
	}

	for _, chunk := range chunks {
		if _, err := tx.Exec(
			`INSERT INTO document_chunks (document_id, seq, source, page_start, page_end, heading, text) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, chunk.Seq, chunk.Source, chunk.PageStart, chunk.PageEnd, chunk.Heading, chunk.Text,
		); err != nil {
			return fmt.Errorf("replace chunks: insert: %w", err)
		}
	}

	return tx.Commit()
}

// ChunksFrom returns id's chunks with seq >= fromSeq, ordered by seq - the
// B3 API's paginated GET .../text endpoint (documents_handlers.go). Callers
// pass fromSeq=1 to skip the meta chunk (seq 0, metadata rather than
// document text) and start at the actual body; fromSeq=0 includes it.
// errDocumentNotFound if id does not exist - an empty result on its own
// can't distinguish "no chunks yet" (a pending document) from "no such
// document" the way this codebase's fail-fast policy requires.
func (s *documentStore) ChunksFrom(id string, fromSeq int) ([]documentChunk, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ok, err := rowExists(s.db, `SELECT 1 FROM documents WHERE id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("chunks from: check document: %w", err)
	}
	if !ok {
		return nil, errDocumentNotFound
	}

	rows, err := s.db.Query(
		`SELECT id, document_id, seq, source, page_start, page_end, heading, text
		 FROM document_chunks WHERE document_id = ? AND seq >= ? ORDER BY seq`,
		id, fromSeq,
	)
	if err != nil {
		return nil, fmt.Errorf("chunks from: %w", err)
	}
	defer rows.Close()

	var out []documentChunk
	for rows.Next() {
		var ch documentChunk
		if err := rows.Scan(&ch.ID, &ch.DocumentID, &ch.Seq, &ch.Source, &ch.PageStart, &ch.PageEnd, &ch.Heading, &ch.Text); err != nil {
			return nil, fmt.Errorf("chunks from: scan: %w", err)
		}
		out = append(out, ch)
	}
	return out, rows.Err()
}

// SetExtracted stores the extract stage's flattened Markdown and page count
// on the document row (B4's documents_indexer.go) - separate from
// ReplaceChunks(local), which stores the same text chunked for search: the
// enrich stage's text-layer-PDF and text-type branches need the whole
// markdown in one string (the first 24k characters of it) to send
// OpenRouter, not the chunk-by-chunk form, and documentNeedsOCR recomputes
// the OCR heuristic from markdown/page_count after a restart rather than
// needing its own persisted column.
func (s *documentStore) SetExtracted(id, markdown string, pageCount int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(`UPDATE documents SET markdown = ?, page_count = ?, updated_at = ? WHERE id = ?`, markdown, pageCount, s.now().Unix(), id)
	if err != nil {
		return fmt.Errorf("set extracted: %w", err)
	}
	return checkRowsAffected(res, errDocumentNotFound)
}

// SetWarning stores a non-fatal message in documents.error without
// disturbing status, stage or indexed_with (B4's documents_indexer.go): a
// document whose extract stage found some but not all pages unreadable
// still reaches indexed, but the warning needs to survive SetIndexed's own
// unconditional error=” clear (SetIndexed marks a clean finish; this
// records that the finish was not entirely clean) - so the indexer calls
// SetIndexed first, then SetWarning, whenever there is one to carry
// forward.
func (s *documentStore) SetWarning(id, msg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(`UPDATE documents SET error = ?, updated_at = ? WHERE id = ?`, msg, s.now().Unix(), id)
	if err != nil {
		return fmt.Errorf("set warning: %w", err)
	}
	return checkRowsAffected(res, errDocumentNotFound)
}

// AddIndexCost records which model performed an enrich-stage OpenRouter
// call and adds its usage.cost to the document's running index_cost_usd
// total (B4's documents_enrich.go) - a running total, not an overwrite,
// since a document reindexed more than once should show everything spent
// on it, not just the most recent pass.
func (s *documentStore) AddIndexCost(id, model string, cost float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(
		`UPDATE documents SET index_model = ?, index_cost_usd = index_cost_usd + ?, updated_at = ? WHERE id = ?`,
		model, cost, s.now().Unix(), id,
	)
	if err != nil {
		return fmt.Errorf("add index cost: %w", err)
	}
	return checkRowsAffected(res, errDocumentNotFound)
}

func (s *documentStore) SetStage(id, stage string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(`UPDATE documents SET stage = ?, updated_at = ? WHERE id = ?`, stage, s.now().Unix(), id)
	if err != nil {
		return fmt.Errorf("set stage: %w", err)
	}
	return checkRowsAffected(res, errDocumentNotFound)
}

// SetIndexed marks a document successfully indexed: status/stage go to
// indexed/done, indexedWith records how ("local" or "mate"), any previous
// error is cleared, and indexed_at is stamped. Unconditional - callers that
// need to know whether a concurrent MarkReindex superseded this document
// while it was being worked on (the indexer's own finishIndexed) want
// SetIndexedIfCurrent instead.
func (s *documentStore) SetIndexed(id, indexedWith string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	res, err := s.db.Exec(
		`UPDATE documents SET status = 'indexed', stage = 'done', indexed_with = ?, error = '', indexed_at = ?, updated_at = ? WHERE id = ?`,
		indexedWith, now.Unix(), now.Unix(), id,
	)
	if err != nil {
		return fmt.Errorf("set indexed: %w", err)
	}
	return checkRowsAffected(res, errDocumentNotFound)
}

// SetIndexedIfCurrent is SetIndexed guarded by reindex_seq: the write only
// applies if id's reindex_seq still equals expectedSeq, the value the
// indexer read (document.ReindexSeq) when it popped this document off the
// queue via NextPending. MarkReindex bumps reindex_seq on every explicit
// reindex request, so a mismatch here means the operator asked for a fresh
// pass while this one was still running - documents_handlers.go:791's
// review finding, where the in-flight pass's own SetIndexed used to
// overwrite the pending/extract row MarkReindex had just written,
// silently discarding the request. ok=false with a nil error is that race,
// not a store failure: the row is already exactly where MarkReindex left
// it (pending/extract, force_ocr set), ready for the next pass, and
// finishIndexed's own doc comment covers what the caller does with that.
// ok=false with errDocumentNotFound means id itself no longer exists.
func (s *documentStore) SetIndexedIfCurrent(id string, expectedSeq int, indexedWith string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	res, err := s.db.Exec(
		`UPDATE documents SET status = 'indexed', stage = 'done', indexed_with = ?, error = '', indexed_at = ?, updated_at = ? WHERE id = ? AND reindex_seq = ?`,
		indexedWith, now.Unix(), now.Unix(), id, expectedSeq,
	)
	if err != nil {
		return false, fmt.Errorf("set indexed if current: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("set indexed if current: rows affected: %w", err)
	}
	if n > 0 {
		return true, nil
	}
	exists, err := rowExists(s.db, `SELECT 1 FROM documents WHERE id = ?`, id)
	if err != nil {
		return false, fmt.Errorf("set indexed if current: check existence: %w", err)
	}
	if !exists {
		return false, errDocumentNotFound
	}
	return false, nil
}

// SetFailed marks a document failed with errMsg, leaving stage as-is (so
// the stage it failed at, e.g. "enrich", stays visible) and indexed_at
// untouched (a document that never finished indexing has no indexed_at).
func (s *documentStore) SetFailed(id, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(`UPDATE documents SET status = 'failed', error = ?, updated_at = ? WHERE id = ?`, errMsg, s.now().Unix(), id)
	if err != nil {
		return fmt.Errorf("set failed: %w", err)
	}
	return checkRowsAffected(res, errDocumentNotFound)
}

// MarkReindex resets id to pending/extract for a fresh indexing pass: the
// B3 API's explicit POST .../reindex action (documents_handlers.go), which
// ADR 0106 treats as consent the same way an upload while Mate is on is -
// so enrich is passed in fresh (readiness at the moment of the reindex
// call) rather than reusing whatever the document's enrich column already
// held from its original upload. error is cleared (a previous failure
// shouldn't linger once a fresh attempt is queued) and force_ocr is always
// set: a reindex exists specifically to retry OCR even when the original
// local text looked adequate enough not to need it automatically.
// reindex_seq is bumped every call, unconditionally - it's the fencing
// token SetIndexedIfCurrent uses to detect exactly this call landing while
// the indexer is already mid-pass on id (review finding, same function's
// doc comment).
func (s *documentStore) MarkReindex(id string, enrich bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(
		`UPDATE documents SET status = 'pending', stage = 'extract', error = '', force_ocr = 1, enrich = ?, reindex_seq = reindex_seq + 1, updated_at = ? WHERE id = ?`,
		boolToInt(enrich), s.now().Unix(), id,
	)
	if err != nil {
		return fmt.Errorf("mark reindex: %w", err)
	}
	return checkRowsAffected(res, errDocumentNotFound)
}

// NextPending returns the oldest-created pending document, ok=false if
// there is none - the indexer's queue-pop, and how pending rows resume
// after a restart (B4).
func (s *documentStore) NextPending() (document, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	doc, err := scanDocument(s.db.QueryRow(`SELECT ` + documentColumns + ` FROM documents WHERE status = 'pending' ORDER BY created_at ASC, id ASC LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return document{}, false, nil
	}
	if err != nil {
		return document{}, false, fmt.Errorf("next pending: %w", err)
	}
	if err := s.attachTags(s.db, &doc); err != nil {
		return document{}, false, err
	}
	return doc, true, nil
}

// Delete removes a document row (and, via ON DELETE CASCADE, its tags and
// chunks - which in turn removes its FTS5 rows through the document_chunks
// triggers) in one transaction, and returns its sha256 so the caller can
// os.Remove the corresponding file after the transaction commits. Delete
// itself never touches the filesystem: a failed file removal must not be
// able to leave a half-deleted database row behind.
func (s *documentStore) Delete(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return "", fmt.Errorf("delete document: begin: %w", err)
	}
	defer tx.Rollback()

	var sha string
	if err := tx.QueryRow(`SELECT sha256 FROM documents WHERE id = ?`, id).Scan(&sha); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errDocumentNotFound
		}
		return "", fmt.Errorf("delete document: lookup: %w", err)
	}

	if _, err := tx.Exec(`DELETE FROM documents WHERE id = ?`, id); err != nil {
		return "", fmt.Errorf("delete document: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("delete document: commit: %w", err)
	}
	return sha, nil
}

// moveDocumentsTx is MoveDocuments' body, factored out so PatchDocument can
// apply a folder move in the same transaction as a title/notes/tags edit
// rather than two separate commits - a bad folder_id must reject the whole
// patch, not land the other fields first and fail only on the move.
func (s *documentStore) moveDocumentsTx(tx *sql.Tx, now time.Time, ids []string, folderID *string) error {
	if folderID != nil {
		ok, err := rowExists(tx, `SELECT 1 FROM document_folders WHERE id = ?`, *folderID)
		if err != nil {
			return fmt.Errorf("move documents: check folder: %w", err)
		}
		if !ok {
			return errFolderNotFound
		}
	}

	for _, id := range ids {
		res, err := tx.Exec(`UPDATE documents SET folder_id = ?, updated_at = ? WHERE id = ?`, nullableString(folderID), now.Unix(), id)
		if err != nil {
			return fmt.Errorf("move documents: update %s: %w", id, err)
		}
		if err := checkRowsAffected(res, errDocumentNotFound); err != nil {
			return err
		}
		if err := s.rebuildMetaChunkTx(tx, id); err != nil {
			return err
		}
	}
	return nil
}

// MoveDocuments assigns folderID (nil for root) to every listed document,
// all in one transaction, and rebuilds each moved document's meta chunk so
// its searchable folder path stays current. An unknown document id fails
// the whole call with errDocumentNotFound - a partial bulk move that
// silently skips ids it doesn't recognize would hide a caller's bug.
func (s *documentStore) MoveDocuments(ids []string, folderID *string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("move documents: begin: %w", err)
	}
	defer tx.Rollback()

	if err := s.moveDocumentsTx(tx, s.now(), ids, folderID); err != nil {
		return err
	}
	return tx.Commit()
}

// PatchDocument applies patchDocumentHandler's whole PATCH body in ONE
// transaction: title/notes/tags (updateMetaTx) and, when moveFolder is
// true, folder_id (moveDocumentsTx for the single id). A bad folder_id
// therefore rejects the whole request - it can never leave title/notes/
// tags committed on their own. Document-not-found is checked before any
// write regardless of which fields are present, so it always takes
// precedence over a bad folder_id. The meta chunk is rebuilt once: by
// moveDocumentsTx when a move happened (it always rebuilds the ids it
// touches), or explicitly here otherwise.
func (s *documentStore) PatchDocument(id string, title, notes *string, tags []string, folderID *string, moveFolder bool) (document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return document{}, fmt.Errorf("patch document: begin: %w", err)
	}
	defer tx.Rollback()

	now := s.now()
	metaChanged := title != nil || notes != nil || tags != nil

	if metaChanged {
		if err := s.updateMetaTx(tx, now, id, title, notes, tags); err != nil {
			return document{}, err
		}
	} else {
		ok, err := rowExists(tx, `SELECT 1 FROM documents WHERE id = ?`, id)
		if err != nil {
			return document{}, fmt.Errorf("patch document: check document: %w", err)
		}
		if !ok {
			return document{}, errDocumentNotFound
		}
	}

	if moveFolder {
		if err := s.moveDocumentsTx(tx, now, []string{id}, folderID); err != nil {
			return document{}, err
		}
	} else if metaChanged {
		if err := s.rebuildMetaChunkTx(tx, id); err != nil {
			return document{}, err
		}
	}

	doc, err := scanDocument(tx.QueryRow(`SELECT `+documentColumns+` FROM documents WHERE id = ?`, id))
	if err != nil {
		return document{}, fmt.Errorf("patch document: read back: %w", err)
	}
	if err := s.attachTags(tx, &doc); err != nil {
		return document{}, err
	}

	if err := tx.Commit(); err != nil {
		return document{}, fmt.Errorf("patch document: commit: %w", err)
	}
	return doc, nil
}

// ── folders ──────────────────────────────────────────────────────────────

// CreateFolder validates name, checks parentID exists (when given) and that
// no sibling already has the same name case-insensitively, then inserts the
// new folder.
func (s *documentStore) CreateFolder(name string, parentID *string) (documentFolder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	trimmed, err := validateFolderName(name)
	if err != nil {
		return documentFolder{}, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return documentFolder{}, fmt.Errorf("create folder: begin: %w", err)
	}
	defer tx.Rollback()

	if parentID != nil {
		ok, err := rowExists(tx, `SELECT 1 FROM document_folders WHERE id = ?`, *parentID)
		if err != nil {
			return documentFolder{}, fmt.Errorf("create folder: check parent: %w", err)
		}
		if !ok {
			return documentFolder{}, errFolderNotFound
		}
	}

	taken, err := folderNameTaken(tx, parentID, trimmed, "")
	if err != nil {
		return documentFolder{}, fmt.Errorf("create folder: check name: %w", err)
	}
	if taken {
		return documentFolder{}, errFolderNameTaken
	}

	now := s.now()
	folder := documentFolder{ID: uuid.NewString(), ParentID: parentID, Name: trimmed, CreatedAt: now, UpdatedAt: now}
	if _, err := tx.Exec(
		`INSERT INTO document_folders (id, parent_id, name, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		folder.ID, nullableString(parentID), folder.Name, now.Unix(), now.Unix(),
	); err != nil {
		return documentFolder{}, fmt.Errorf("create folder: insert: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return documentFolder{}, fmt.Errorf("create folder: commit: %w", err)
	}
	return folder, nil
}

// PatchFolder applies patchDocumentFolderHandler's whole PATCH body in ONE
// transaction, mirroring PatchDocument: a rename (name non-nil) and/or a
// move (moveParent true, parentID the new parent or nil for root) commit
// together. Before this method existed, the handler called RenameFolder and
// MoveFolder as two separate transactions, so a rejected move (a cycle, a
// name collision, or a missing parent) could return 409/404 with the rename
// already committed. name is validated and, when moveParent is set, a
// self-parent is rejected before the transaction opens (matching
// RenameFolder/MoveFolder's own prior behaviour); everything that needs the
// row's current state - existence, the name-collision check under the
// eventual parent, the cycle check - runs inside it, so any failure leaves
// neither field changed. The meta chunk of every document in id's subtree
// is rebuilt exactly once, since either a new name or a new parent changes
// the folder-path text they all carry.
func (s *documentStore) PatchFolder(id string, name *string, parentID *string, moveParent bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var trimmedName string
	if name != nil {
		var err error
		trimmedName, err = validateFolderName(*name)
		if err != nil {
			return err
		}
	}

	if moveParent && parentID != nil && *parentID == id {
		return errFolderCycle
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("patch folder: begin: %w", err)
	}
	defer tx.Rollback()

	var currentName string
	var currentParent sql.NullString
	if err := tx.QueryRow(`SELECT name, parent_id FROM document_folders WHERE id = ?`, id).Scan(&currentName, &currentParent); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errFolderNotFound
		}
		return fmt.Errorf("patch folder: read: %w", err)
	}

	if name == nil && !moveParent {
		return tx.Commit()
	}

	newName := currentName
	if name != nil {
		newName = trimmedName
	}

	targetParent := parentID
	if !moveParent {
		targetParent = nil
		if currentParent.Valid {
			v := currentParent.String
			targetParent = &v
		}
	}

	if moveParent && targetParent != nil {
		ok, err := rowExists(tx, `SELECT 1 FROM document_folders WHERE id = ?`, *targetParent)
		if err != nil {
			return fmt.Errorf("patch folder: check parent: %w", err)
		}
		if !ok {
			return errFolderNotFound
		}

		cyclic, err := rowExists(tx, `
			WITH RECURSIVE anc(fid) AS (
				SELECT parent_id FROM document_folders WHERE id = ?
				UNION ALL
				SELECT f.parent_id FROM document_folders f JOIN anc ON f.id = anc.fid WHERE f.parent_id IS NOT NULL
			)
			SELECT 1 FROM anc WHERE fid = ? LIMIT 1`, *targetParent, id)
		if err != nil {
			return fmt.Errorf("patch folder: check cycle: %w", err)
		}
		if cyclic {
			return errFolderCycle
		}
	}

	taken, err := folderNameTaken(tx, targetParent, newName, id)
	if err != nil {
		return fmt.Errorf("patch folder: check name: %w", err)
	}
	if taken {
		return errFolderNameTaken
	}

	now := s.now()
	if _, err := tx.Exec(`UPDATE document_folders SET name = ?, parent_id = ?, updated_at = ? WHERE id = ?`,
		newName, nullableString(targetParent), now.Unix(), id); err != nil {
		return fmt.Errorf("patch folder: update: %w", err)
	}

	if err := s.rebuildMetaChunksUnderFolderTx(tx, id); err != nil {
		return err
	}

	return tx.Commit()
}

// RenameFolder is a thin wrapper over PatchFolder for callers that only
// ever rename (documents_store_test.go exercises it directly).
func (s *documentStore) RenameFolder(id, name string) error {
	return s.PatchFolder(id, &name, nil, false)
}

// MoveFolder is a thin wrapper over PatchFolder for callers that only ever
// reparent (documents_store_test.go exercises it directly).
func (s *documentStore) MoveFolder(id string, parentID *string) error {
	return s.PatchFolder(id, nil, parentID, true)
}

// DeleteFolder removes an empty folder. errFolderNotEmpty if it still has
// subfolders or documents - there is no recursive delete, by design (ADR
// 0106): an operator emptying a folder is a deliberate, visible act, not a
// side effect of deleting its parent.
func (s *documentStore) DeleteFolder(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("delete folder: begin: %w", err)
	}
	defer tx.Rollback()

	ok, err := rowExists(tx, `SELECT 1 FROM document_folders WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete folder: check exists: %w", err)
	}
	if !ok {
		return errFolderNotFound
	}

	hasSub, err := rowExists(tx, `SELECT 1 FROM document_folders WHERE parent_id = ? LIMIT 1`, id)
	if err != nil {
		return fmt.Errorf("delete folder: check subfolders: %w", err)
	}
	hasDocs, err := rowExists(tx, `SELECT 1 FROM documents WHERE folder_id = ? LIMIT 1`, id)
	if err != nil {
		return fmt.Errorf("delete folder: check documents: %w", err)
	}
	if hasSub || hasDocs {
		return errFolderNotEmpty
	}

	if _, err := tx.Exec(`DELETE FROM document_folders WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete folder: %w", err)
	}
	return tx.Commit()
}

// ListFolder returns parentID's immediate subfolders and the documents
// filed directly in it (neither recursive), each ordered case-
// insensitively by name. parentID nil means root (folder_id/parent_id
// NULL) - unlike List's folderID, which nil instead treats as "no filter at
// all"; see queryDocuments' doc comment for why these differ.
// errFolderNotFound if parentID is non-nil and does not exist.
func (s *documentStore) ListFolder(parentID *string) ([]documentFolder, []document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var parentKey string
	if parentID != nil {
		ok, err := rowExists(s.db, `SELECT 1 FROM document_folders WHERE id = ?`, *parentID)
		if err != nil {
			return nil, nil, fmt.Errorf("list folder: check parent: %w", err)
		}
		if !ok {
			return nil, nil, errFolderNotFound
		}
		parentKey = *parentID
	}

	folderRows, err := s.db.Query(
		`SELECT id, parent_id, name, created_at, updated_at FROM document_folders WHERE COALESCE(parent_id,'') = ? ORDER BY lower(name)`,
		parentKey,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("list folder: subfolders: %w", err)
	}
	var folders []documentFolder
	for folderRows.Next() {
		var f documentFolder
		var pid sql.NullString
		var created, updated int64
		if err := folderRows.Scan(&f.ID, &pid, &f.Name, &created, &updated); err != nil {
			folderRows.Close()
			return nil, nil, fmt.Errorf("list folder: scan subfolder: %w", err)
		}
		if pid.Valid {
			v := pid.String
			f.ParentID = &v
		}
		f.CreatedAt = time.Unix(created, 0).UTC()
		f.UpdatedAt = time.Unix(updated, 0).UTC()
		folders = append(folders, f)
	}
	scanErr := folderRows.Err()
	folderRows.Close()
	if scanErr != nil {
		return nil, nil, fmt.Errorf("list folder: %w", scanErr)
	}

	docRows, err := s.db.Query(`SELECT `+documentColumns+` FROM documents WHERE COALESCE(folder_id,'') = ? ORDER BY lower(filename)`, parentKey)
	if err != nil {
		return nil, nil, fmt.Errorf("list folder: documents: %w", err)
	}
	var docs []document
	for docRows.Next() {
		doc, err := scanDocument(docRows)
		if err != nil {
			docRows.Close()
			return nil, nil, fmt.Errorf("list folder: scan document: %w", err)
		}
		docs = append(docs, doc)
	}
	scanErr = docRows.Err()
	docRows.Close()
	if scanErr != nil {
		return nil, nil, fmt.Errorf("list folder: %w", scanErr)
	}

	for i := range docs {
		if err := s.attachTags(s.db, &docs[i]); err != nil {
			return nil, nil, err
		}
	}

	return folders, docs, nil
}

// FolderPath returns id's ancestor chain root-first, id last.
// errFolderNotFound if id does not exist.
func (s *documentStore) FolderPath(id string) ([]documentFolder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return folderPath(s.db, id)
}

// ResolveFolderPath resolves a "/"-separated path such as "Receipts/2026"
// (the search_documents tool's own folder argument, assistant_tools.go) to
// a folder id, walking down from the root and matching each segment's name
// case-insensitively - the same case-insensitive rule folderNameTaken
// already enforces on create/rename, so a path an operator would actually
// type always resolves regardless of how the folder's name was cased when
// it was made. Leading/trailing/doubled slashes and blank segments (e.g. a
// trailing "/") are skipped rather than rejected. errFolderNotFound if any
// segment along the way does not exist, or if path resolves to no segments
// at all (empty, or all-blank).
func (s *documentStore) ResolveFolderPath(path string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var parentKey string
	id := ""
	for _, segment := range strings.Split(path, "/") {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		row := s.db.QueryRow(
			`SELECT id FROM document_folders WHERE COALESCE(parent_id,'') = ? AND lower(name) = lower(?)`,
			parentKey, segment)
		if err := row.Scan(&id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return "", errFolderNotFound
			}
			return "", fmt.Errorf("resolve folder path: %w", err)
		}
		parentKey = id
	}
	if id == "" {
		return "", errFolderNotFound
	}
	return id, nil
}

// TopLevelFolderNames returns the names of every folder directly under the
// root, alphabetically (the same order ListFolder's own subfolder query
// already returns them in) - shared by the document library's live
// system-prompt line (assistant_prompt.go) and the search_documents tool's
// "unknown folder" error (assistant_tools.go), so both name what's actually
// there from the one query rather than drifting apart.
//
// This used to be ListFolder(nil), which - besides the folder names this
// wants - also loads every root-filed document's whole row (markdown
// column included) plus a tag query per row, on every Mate question
// (collectAssistantPromptContext runs this on each call), the exact cost
// Count() above was added to avoid. A single query over document_folders,
// scoped the same way ListFolder's own subfolder query is, replaces it.
func (s *documentStore) TopLevelFolderNames() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT name FROM document_folders WHERE COALESCE(parent_id,'') = '' ORDER BY lower(name)`)
	if err != nil {
		return nil, fmt.Errorf("top-level folder names: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("top-level folder names: scan: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("top-level folder names: %w", err)
	}
	return names, nil
}

// ── boot sweep ───────────────────────────────────────────────────────────

// allSHAs returns every document's sha256, for sweepDocumentsDir to compare
// against what's actually on disk.
func (s *documentStore) allSHAs() (map[string]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT sha256 FROM documents`)
	if err != nil {
		return nil, fmt.Errorf("list sha256 hashes: %w", err)
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var sha string
		if err := rows.Scan(&sha); err != nil {
			return nil, fmt.Errorf("scan sha256: %w", err)
		}
		out[sha] = true
	}
	return out, rows.Err()
}

// sweepDocumentsDir runs once at boot (main.go): it deletes abandoned
// upload-*.tmp files (a crash mid-upload, before the sha256 rename), and
// separately reports - by logging, never deleting - any file on disk with
// no matching document row and any document row whose file is missing.
// Both mismatches are left alone rather than "fixed": AGENTS.md's fallback
// policy is to surface a data/source problem explicitly, and guessing
// which side (disk or database) is wrong would risk destroying real
// evidence of an operator's own out-of-band mistake. Counts are returned,
// not just logged, so this is directly assertable in tests.
func sweepDocumentsDir(dir string, store *documentStore) (documentSweepResult, error) {
	var result documentSweepResult

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return result, fmt.Errorf("sweep documents dir: %w", err)
	}

	shas, err := store.allSHAs()
	if err != nil {
		return result, fmt.Errorf("sweep documents dir: %w", err)
	}

	onDisk := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "upload-") && strings.HasSuffix(name, ".tmp") {
			if err := os.Remove(filepath.Join(dir, name)); err != nil {
				return result, fmt.Errorf("sweep documents dir: remove %s: %w", name, err)
			}
			result.RemovedTemp++
			continue
		}
		onDisk[name] = true
		if !shas[name] {
			log.Printf("documents: orphan file %s has no matching document row", name)
			result.OrphanFiles++
		}
	}

	for sha := range shas {
		if !onDisk[sha] {
			log.Printf("documents: document row references missing file %s", sha)
			result.MissingFiles++
		}
	}

	return result, nil
}
