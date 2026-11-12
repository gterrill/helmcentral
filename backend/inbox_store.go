package main

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// The Inbox (ADR 0279): photos and files shared from a phone that wait to be
// culled and sent somewhere. An item is not a Document until it is filed, so
// the library's indexer never reads a shot that is about to be binned. The
// bytes sit in their own directory, named by their sha256; the row beside them
// carries what the phone knew when the file was taken.

var (
	errInboxItemNotFound = errors.New("inbox item not found")
	// errInboxDuplicate is Insert's answer for a sha256 already in the Inbox.
	errInboxDuplicate = errors.New("inbox item already exists")
	// errInboxBusy is Claim's answer for an item another request is filing.
	errInboxBusy = errors.New("this item is already being filed")
)

// inboxClaimStale is how old a filing claim must be before another request may
// take the item over: long enough for a slow copy, short enough that a crash
// mid-filing does not strand the item.
const inboxClaimStale = 5 * time.Minute

// globalInboxStore is opened once in main() and read by the /api/inbox handlers.
var globalInboxStore *inboxStore

func inboxDirPath() string {
	return cacheFilePath("INBOX_DIR", "data/inbox")
}

// inboxFilePath is where an item's bytes live.
func inboxFilePath(sha string) string {
	return filepath.Join(inboxDirPath(), sha)
}

// inboxThumbPath is where an item's small preview lives, beside its bytes.
func inboxThumbPath(sha string) string {
	return filepath.Join(inboxDirPath(), sha+".thumb")
}

type inboxItem struct {
	ID        string
	SHA256    string
	Filename  string
	MIME      string
	SizeBytes int64
	// CaptureMs is when the photo was taken, Unix ms, nil when the file says
	// nothing.
	CaptureMs *int64
	// BearingDeg is the direction the phone faced, present only when a real
	// one arrived with the file.
	BearingDeg *float64
	// PhoneLat and PhoneLon are the phone's own position, present only when
	// both are real. They only ever raise a warning.
	PhoneLat, PhoneLon *float64
	// Fingerprint is a perceptual hash (hex) and Sharpness a focus score, both
	// worked out on the phone.
	Fingerprint string
	Sharpness   float64
	CreatedMs   int64
	// SourceSHA256 is the hash of the original file as the phone held it, or
	// empty when the phone could not work it out.
	SourceSHA256 string
	// HasThumb says a small preview is stored beside the file.
	HasThumb bool
	// SuggestedKind is what Mate made of the photo: scenery, document or
	// equipment. Empty until Mate has looked, and for good when it is not set up.
	SuggestedKind string
	// ClassifyError is the plain line recorded when Mate could not tell; the
	// cause is in the log. Every failure is tried again by itself (NextTryMs).
	ClassifyError string
	// ClassifyAttempts counts the failures since Mate last answered.
	ClassifyAttempts int
	// NextTryMs is when a failed sort is tried again, Unix ms; 0 for an item
	// that has not failed.
	NextTryMs int64
	// SuggestedTitle is the title Mate made for the photo, kept so it is not
	// paid for twice. Empty until Mate has made one.
	SuggestedTitle string
}

type inboxStore struct {
	db *sql.DB
}

func newInboxStore(dbPath string) (*inboxStore, error) {
	db, err := openHelmcentralDB(dbPath)
	if err != nil {
		return nil, err
	}
	if err := createInboxSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	return &inboxStore{db: db}, nil
}

func (s *inboxStore) Close() error { return s.db.Close() }

func createInboxSchema(db *sql.DB) error {
	// source_sha256 is the hash of the file as it left the phone, before the
	// re-encode, so the same original is recognised however it was shrunk.
	// filing_ms is set while a request is filing the item (see Claim).
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS inbox_items (
		id TEXT PRIMARY KEY,
		sha256 TEXT NOT NULL UNIQUE,
		filename TEXT NOT NULL,
		mime TEXT NOT NULL,
		size_bytes INTEGER NOT NULL,
		capture_ms INTEGER,
		bearing_deg REAL,
		phone_lat REAL,
		phone_lon REAL,
		fingerprint TEXT NOT NULL DEFAULT '',
		sharpness REAL NOT NULL DEFAULT 0,
		created_ms INTEGER NOT NULL,
		source_sha256 TEXT NOT NULL DEFAULT '',
		has_thumb INTEGER NOT NULL DEFAULT 0,
		filing_ms INTEGER,
		suggested_kind TEXT,
		classify_error TEXT NOT NULL DEFAULT '',
		classify_attempts INTEGER NOT NULL DEFAULT 0,
		next_try_ms INTEGER NOT NULL DEFAULT 0,
		suggested_title TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		return fmt.Errorf("create inbox schema: %w", err)
	}
	if err := addInboxMateColumns(db); err != nil {
		return err
	}
	// The classifier reads only the items Mate has not sorted; this keeps that
	// read short however many sorted items the Inbox holds.
	if _, err := db.Exec(`DROP INDEX IF EXISTS idx_inbox_items_pending`); err != nil {
		return fmt.Errorf("create inbox schema: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_inbox_items_unsorted ON inbox_items(created_ms, id) WHERE suggested_kind IS NULL`); err != nil {
		return fmt.Errorf("create inbox schema: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_inbox_items_capture ON inbox_items(capture_ms)`); err != nil {
		return fmt.Errorf("create inbox schema: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_inbox_items_source ON inbox_items(source_sha256)`); err != nil {
		return fmt.Errorf("create inbox schema: %w", err)
	}
	// Originals already filed out of the Inbox, so sharing the roll again skips
	// them. copy_sha256 is the hash of the stored copy the original became, so
	// the original is forgotten once that copy is deleted; '' means no copy was
	// recorded, and the original is remembered for good.
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS inbox_filed (
		source_sha256 TEXT PRIMARY KEY,
		copy_sha256 TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		return fmt.Errorf("create inbox schema: %w", err)
	}
	return addInboxFiledCopyColumn(db)
}

// addInboxFiledCopyColumn upgrades an inbox_filed table made before it
// recorded copies. Its existing rows record no copy, which keeps them remembered.
func addInboxFiledCopyColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(inbox_filed)`)
	if err != nil {
		return fmt.Errorf("inspect inbox_filed: %w", err)
	}
	have := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, colType string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			rows.Close()
			return fmt.Errorf("inspect inbox_filed: %w", err)
		}
		if name == "copy_sha256" {
			have = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("inspect inbox_filed: %w", err)
	}
	rows.Close()
	if have {
		return nil
	}
	if _, err := db.Exec(`ALTER TABLE inbox_filed ADD COLUMN copy_sha256 TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("add inbox_filed.copy_sha256: %w", err)
	}
	return nil
}

// addInboxMateColumns upgrades an inbox_items table made before Mate sorting
// (ADR 0283). It is a no-op on a table that already has them.
func addInboxMateColumns(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(inbox_items)`)
	if err != nil {
		return fmt.Errorf("inspect inbox_items: %w", err)
	}
	have := map[string]bool{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, colType string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			rows.Close()
			return fmt.Errorf("inspect inbox_items: %w", err)
		}
		have[name] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("inspect inbox_items: %w", err)
	}
	rows.Close()
	for _, col := range []struct{ name, ddl string }{
		{"suggested_kind", `suggested_kind TEXT`},
		{"classify_error", `classify_error TEXT NOT NULL DEFAULT ''`},
		{"classify_attempts", `classify_attempts INTEGER NOT NULL DEFAULT 0`},
		{"next_try_ms", `next_try_ms INTEGER NOT NULL DEFAULT 0`},
		{"suggested_title", `suggested_title TEXT NOT NULL DEFAULT ''`},
	} {
		if have[col.name] {
			continue
		}
		if _, err := db.Exec(`ALTER TABLE inbox_items ADD COLUMN ` + col.ddl); err != nil {
			return fmt.Errorf("add inbox_items.%s: %w", col.name, err)
		}
	}
	return nil
}

// Where an original is already held.
const (
	inboxWhereNone  = ""
	inboxWhereInbox = "inbox"
	inboxWhereFiled = "filed"
)

// copyHeld says whether a stored copy with this hash still exists in Documents.
type copyHeld func(copySHA string) (bool, error)

// filedHeld says whether an original is still remembered as filed: its row
// exists and either records no copy or the copy it records is still held.
func (s *inboxStore) filedHeld(sha string, held copyHeld) (bool, error) {
	var copySHA string
	err := s.db.QueryRow(`SELECT copy_sha256 FROM inbox_filed WHERE source_sha256 = ?`, sha).Scan(&copySHA)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check filed source hash: %w", err)
	}
	if copySHA == "" {
		return true, nil
	}
	return held(copySHA)
}

// SourceWhere says where an original with this hash is already held: waiting
// in the Inbox, filed out of it with its copy still held, or nowhere ("").
func (s *inboxStore) SourceWhere(sha string, held copyHeld) (string, error) {
	found, err := rowExists(s.db, `SELECT 1 FROM inbox_items WHERE source_sha256 = ? LIMIT 1`, sha)
	if err != nil {
		return "", fmt.Errorf("check inbox source hash: %w", err)
	}
	if found {
		return inboxWhereInbox, nil
	}
	filed, err := s.filedHeld(sha, held)
	if err != nil {
		return "", err
	}
	if filed {
		return inboxWhereFiled, nil
	}
	return inboxWhereNone, nil
}

// KnownSources answers which of these original-file hashes are already in the
// Inbox or filed out of it with the copy still held.
func (s *inboxStore) KnownSources(shas []string, held copyHeld) ([]string, error) {
	if len(shas) == 0 {
		return []string{}, nil
	}
	marks, one := inPlaceholders(shas)
	rows, err := s.db.Query(`SELECT source_sha256 FROM inbox_items WHERE source_sha256 IN (`+marks+`)`, one...)
	if err != nil {
		return nil, fmt.Errorf("check known source hashes: %w", err)
	}
	seen := map[string]bool{}
	out := []string{}
	for rows.Next() {
		var sha string
		if err := rows.Scan(&sha); err != nil {
			rows.Close()
			return nil, fmt.Errorf("check known source hashes: %w", err)
		}
		seen[sha] = true
		out = append(out, sha)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	// Filed originals come in one query; only those with a recorded copy ask
	// the library, so a big roll of new photos costs the library nothing.
	rows, err = s.db.Query(`SELECT source_sha256, copy_sha256 FROM inbox_filed WHERE source_sha256 IN (`+marks+`)`, one...)
	if err != nil {
		return nil, fmt.Errorf("check known source hashes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var sha, copySHA string
		if err := rows.Scan(&sha, &copySHA); err != nil {
			return nil, fmt.Errorf("check known source hashes: %w", err)
		}
		if seen[sha] {
			continue
		}
		if copySHA != "" {
			ok, err := held(copySHA)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
		}
		seen[sha] = true
		out = append(out, sha)
	}
	return out, rows.Err()
}

// MarkFiled remembers that an original left the Inbox for a passage, Documents,
// equipment or a job, as the stored copy with hash copySHA. Filing it again
// replaces the recorded copy.
func (s *inboxStore) MarkFiled(sha, copySHA string) error {
	if sha == "" {
		return nil
	}
	if _, err := s.db.Exec(`INSERT INTO inbox_filed (source_sha256, copy_sha256) VALUES (?, ?)
		ON CONFLICT(source_sha256) DO UPDATE SET copy_sha256 = excluded.copy_sha256`, sha, copySHA); err != nil {
		return fmt.Errorf("record filed source hash: %w", err)
	}
	return nil
}

// Claim takes the right to file an item. Only one caller holds it at a time:
// the conditional UPDATE changes a row for exactly one of two racing requests.
// A claim older than inboxClaimStale is taken over. It answers
// errInboxItemNotFound for an id that is not there and errInboxBusy for one
// another request holds. It returns the token (the claim's timestamp) that
// Release needs, so a request whose stale claim was taken over cannot clear the
// new holder's.
func (s *inboxStore) Claim(id string) (int64, error) { return s.claimAt(id, time.Now()) }

func (s *inboxStore) claimAt(id string, now time.Time) (int64, error) {
	res, err := s.db.Exec(
		`UPDATE inbox_items SET filing_ms = ? WHERE id = ? AND (filing_ms IS NULL OR filing_ms < ?)`,
		now.UnixMilli(), id, now.Add(-inboxClaimStale).UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("claim inbox item: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return now.UnixMilli(), nil
	}
	found, err := rowExists(s.db, `SELECT 1 FROM inbox_items WHERE id = ?`, id)
	if err != nil {
		return 0, fmt.Errorf("claim inbox item: %w", err)
	}
	if !found {
		return 0, errInboxItemNotFound
	}
	return 0, errInboxBusy
}

// Release gives up the claim Claim returned token for. A claim another request
// has since taken over, or an item already filed and gone, is left alone and is
// not an error.
func (s *inboxStore) Release(id string, token int64) error {
	if _, err := s.db.Exec(`UPDATE inbox_items SET filing_ms = NULL WHERE id = ? AND filing_ms = ?`, id, token); err != nil {
		return fmt.Errorf("release inbox item: %w", err)
	}
	return nil
}

const inboxColumns = `id, sha256, filename, mime, size_bytes, capture_ms, bearing_deg, phone_lat, phone_lon, fingerprint, sharpness, created_ms, source_sha256, has_thumb, suggested_kind, classify_error, classify_attempts, next_try_ms, suggested_title`

func scanInboxItem(row interface{ Scan(...any) error }) (inboxItem, error) {
	var it inboxItem
	var capture sql.NullInt64
	var bearing, lat, lon sql.NullFloat64
	var kind sql.NullString
	var thumb int
	if err := row.Scan(&it.ID, &it.SHA256, &it.Filename, &it.MIME, &it.SizeBytes, &capture, &bearing, &lat, &lon,
		&it.Fingerprint, &it.Sharpness, &it.CreatedMs, &it.SourceSHA256, &thumb, &kind, &it.ClassifyError, &it.ClassifyAttempts, &it.NextTryMs, &it.SuggestedTitle); err != nil {
		return it, err
	}
	it.SuggestedKind = kind.String
	if capture.Valid {
		v := capture.Int64
		it.CaptureMs = &v
	}
	it.BearingDeg, it.PhoneLat, it.PhoneLon = nullToPtr(bearing), nullToPtr(lat), nullToPtr(lon)
	it.HasThumb = thumb != 0
	return it, nil
}

// Insert adds an item, giving it an id and a creation time. A sha256 already
// held comes back as errInboxDuplicate with the existing row.
func (s *inboxStore) Insert(it inboxItem) (inboxItem, error) {
	if existing, ok, err := s.GetBySHA(it.SHA256); err != nil {
		return inboxItem{}, err
	} else if ok {
		return existing, errInboxDuplicate
	}
	it.ID, it.CreatedMs = uuid.NewString(), time.Now().UnixMilli()
	_, err := s.db.Exec(`INSERT INTO inbox_items (`+inboxColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,NULL,'',0,0,'')`,
		it.ID, it.SHA256, it.Filename, it.MIME, it.SizeBytes, nullInt64(it.CaptureMs),
		nullFloat(it.BearingDeg), nullFloat(it.PhoneLat), nullFloat(it.PhoneLon), it.Fingerprint, it.Sharpness, it.CreatedMs,
		it.SourceSHA256, boolToInt(it.HasThumb))
	if err != nil {
		return inboxItem{}, fmt.Errorf("insert inbox item: %w", err)
	}
	return it, nil
}

func (s *inboxStore) Get(id string) (inboxItem, error) {
	it, err := scanInboxItem(s.db.QueryRow(`SELECT `+inboxColumns+` FROM inbox_items WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return it, errInboxItemNotFound
	}
	if err != nil {
		return it, fmt.Errorf("get inbox item: %w", err)
	}
	return it, nil
}

// GetMany returns the items that exist among ids, keyed by id. An id that is
// not there is left out.
func (s *inboxStore) GetMany(ids []string) (map[string]inboxItem, error) {
	out := make(map[string]inboxItem, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	marks, args := inPlaceholders(ids)
	rows, err := s.db.Query(`SELECT `+inboxColumns+` FROM inbox_items WHERE id IN (`+marks+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("get inbox items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		it, err := scanInboxItem(rows)
		if err != nil {
			return nil, fmt.Errorf("scan inbox item: %w", err)
		}
		out[it.ID] = it
	}
	return out, rows.Err()
}

// GetBySHA says whether an item with these bytes is already waiting.
func (s *inboxStore) GetBySHA(sha string) (inboxItem, bool, error) {
	it, err := scanInboxItem(s.db.QueryRow(`SELECT `+inboxColumns+` FROM inbox_items WHERE sha256 = ?`, sha))
	if errors.Is(err, sql.ErrNoRows) {
		return it, false, nil
	}
	if err != nil {
		return it, false, fmt.Errorf("get inbox item by sha256: %w", err)
	}
	return it, true, nil
}

// List returns every item in capture order, items with no capture time last
// (oldest arrival first among them).
func (s *inboxStore) List() ([]inboxItem, error) {
	rows, err := s.db.Query(`SELECT ` + inboxColumns + ` FROM inbox_items ORDER BY capture_ms IS NULL, capture_ms, created_ms, id`)
	if err != nil {
		return nil, fmt.Errorf("list inbox: %w", err)
	}
	defer rows.Close()
	out := []inboxItem{}
	for rows.Next() {
		it, err := scanInboxItem(rows)
		if err != nil {
			return nil, fmt.Errorf("scan inbox item: %w", err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// inboxCursor is a place in arrival order: the next page starts after it, so a
// pass always moves on whatever happened to the items it has been through.
type inboxCursor struct {
	createdMs int64
	id        string
}

// Unclassified returns the first page of items Mate should look at now.
func (s *inboxStore) Unclassified() ([]inboxItem, error) {
	return s.UnclassifiedAfter(time.Now(), inboxCursor{})
}

// UnclassifiedAfter returns one page of inboxClassifyPage items that come after
// the cursor in arrival order and that Mate should look at at now: no kind yet,
// and either no recorded failure or one whose retry has come due.
func (s *inboxStore) UnclassifiedAfter(now time.Time, after inboxCursor) ([]inboxItem, error) {
	rows, err := s.db.Query(`SELECT `+inboxColumns+` FROM inbox_items
		WHERE suggested_kind IS NULL
		  AND (classify_error = '' OR next_try_ms <= ?)
		  AND (created_ms > ? OR (created_ms = ? AND id > ?))
		ORDER BY created_ms, id LIMIT ?`,
		now.UnixMilli(), after.createdMs, after.createdMs, after.id, inboxClassifyPage)
	if err != nil {
		return nil, fmt.Errorf("list unclassified inbox items: %w", err)
	}
	defer rows.Close()
	var out []inboxItem
	for rows.Next() {
		it, err := scanInboxItem(rows)
		if err != nil {
			return nil, fmt.Errorf("scan inbox item: %w", err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// SetSuggestedKind records what Mate made of an item and clears any earlier
// failure. An item gone since (filed or deleted) is left alone, not an error.
func (s *inboxStore) SetSuggestedKind(id, kind string) error {
	if _, err := s.db.Exec(`UPDATE inbox_items SET suggested_kind = ?, classify_error = '', classify_attempts = 0, next_try_ms = 0 WHERE id = ?`, kind, id); err != nil {
		return fmt.Errorf("record suggested kind: %w", err)
	}
	return nil
}

// SetClassifyRetry records a failed sort: the notice to show, how many times the
// item has failed, and when to try it again.
func (s *inboxStore) SetClassifyRetry(id, msg string, attempts int, nextTryMs int64) error {
	if _, err := s.db.Exec(`UPDATE inbox_items SET classify_error = ?, classify_attempts = ?, next_try_ms = ? WHERE id = ?`, msg, attempts, nextTryMs, id); err != nil {
		return fmt.Errorf("record classify retry: %w", err)
	}
	return nil
}

// NextRetryMs is the earliest time a failed sort comes due, or 0 when no item is
// waiting to be retried.
func (s *inboxStore) NextRetryMs() (int64, error) {
	var next sql.NullInt64
	if err := s.db.QueryRow(`SELECT MIN(next_try_ms) FROM inbox_items WHERE suggested_kind IS NULL AND next_try_ms > 0`).Scan(&next); err != nil {
		return 0, fmt.Errorf("find the next Mate retry: %w", err)
	}
	return next.Int64, nil
}

// RequeueFailed clears every recorded sorting failure and its back-off, so Mate
// looks at those items again now, and says how many there were.
func (s *inboxStore) RequeueFailed() (int, error) {
	res, err := s.db.Exec(`UPDATE inbox_items SET classify_error = '', classify_attempts = 0, next_try_ms = 0 WHERE suggested_kind IS NULL AND classify_error != ''`)
	if err != nil {
		return 0, fmt.Errorf("requeue failed inbox items: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// SetSuggestedTitle keeps the title Mate made for an item.
func (s *inboxStore) SetSuggestedTitle(id, title string) error {
	if _, err := s.db.Exec(`UPDATE inbox_items SET suggested_title = ? WHERE id = ?`, title, id); err != nil {
		return fmt.Errorf("record suggested title: %w", err)
	}
	return nil
}

func (s *inboxStore) Count() (int, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM inbox_items`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count inbox: %w", err)
	}
	return n, nil
}

// Delete removes the row only; the caller removes the files.
func (s *inboxStore) Delete(id string) error {
	n, err := s.DeleteMany([]string{id})
	if err != nil {
		return err
	}
	if n == 0 {
		return errInboxItemNotFound
	}
	return nil
}

// DeleteMany removes the rows only, in one statement, and says how many were
// there; the caller removes the files. It ignores filing claims: it is for a
// caller that holds the claim itself.
func (s *inboxStore) DeleteMany(ids []string) (int, error) {
	deleted, err := s.deleteWhere(ids, "", nil)
	return len(deleted), err
}

// DeleteUnclaimed removes the rows among ids that no request is filing (no
// claim, or one gone stale) and returns the ids it removed; the caller removes
// their files and reports the rest as being filed.
func (s *inboxStore) DeleteUnclaimed(ids []string) ([]string, error) {
	return s.deleteWhere(ids, ` AND (filing_ms IS NULL OR filing_ms < ?)`,
		[]any{time.Now().Add(-inboxClaimStale).UnixMilli()})
}

func (s *inboxStore) deleteWhere(ids []string, cond string, condArgs []any) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	marks, args := inPlaceholders(ids)
	rows, err := s.db.Query(
		`DELETE FROM inbox_items WHERE id IN (`+marks+`)`+cond+` RETURNING id`, append(args, condArgs...)...)
	if err != nil {
		return nil, fmt.Errorf("delete inbox items: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("delete inbox items: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
