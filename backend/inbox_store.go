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
		filing_ms INTEGER
	)`); err != nil {
		return fmt.Errorf("create inbox schema: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_inbox_items_capture ON inbox_items(capture_ms)`); err != nil {
		return fmt.Errorf("create inbox schema: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_inbox_items_source ON inbox_items(source_sha256)`); err != nil {
		return fmt.Errorf("create inbox schema: %w", err)
	}
	// Originals already filed out of the Inbox, so sharing the roll again skips them.
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS inbox_filed (
		source_sha256 TEXT PRIMARY KEY
	)`); err != nil {
		return fmt.Errorf("create inbox schema: %w", err)
	}
	return nil
}

// Where an original is already held.
const (
	inboxWhereNone  = ""
	inboxWhereInbox = "inbox"
	inboxWhereFiled = "filed"
)

// SourceWhere says where an original with this hash is already held: waiting
// in the Inbox, filed out of it before, or nowhere ("").
func (s *inboxStore) SourceWhere(sha string) (string, error) {
	for _, t := range []struct{ where, query string }{
		{inboxWhereInbox, `SELECT 1 FROM inbox_items WHERE source_sha256 = ? LIMIT 1`},
		{inboxWhereFiled, `SELECT 1 FROM inbox_filed WHERE source_sha256 = ? LIMIT 1`},
	} {
		found, err := rowExists(s.db, t.query, sha)
		if err != nil {
			return "", fmt.Errorf("check %s source hash: %w", t.where, err)
		}
		if found {
			return t.where, nil
		}
	}
	return inboxWhereNone, nil
}

// KnownSources answers which of these original-file hashes are already in the
// Inbox or filed out of it.
func (s *inboxStore) KnownSources(shas []string) ([]string, error) {
	if len(shas) == 0 {
		return []string{}, nil
	}
	marks, one := inPlaceholders(shas)
	args := append(append(make([]any, 0, 2*len(shas)), one...), one...)
	rows, err := s.db.Query(
		`SELECT source_sha256 FROM inbox_items WHERE source_sha256 IN (`+marks+`)
		 UNION SELECT source_sha256 FROM inbox_filed WHERE source_sha256 IN (`+marks+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("check known source hashes: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var sha string
		if err := rows.Scan(&sha); err != nil {
			return nil, fmt.Errorf("check known source hashes: %w", err)
		}
		out = append(out, sha)
	}
	return out, rows.Err()
}

// MarkFiled remembers that an original left the Inbox for a passage or Documents.
func (s *inboxStore) MarkFiled(sha string) error {
	if sha == "" {
		return nil
	}
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO inbox_filed (source_sha256) VALUES (?)`, sha); err != nil {
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

const inboxColumns = `id, sha256, filename, mime, size_bytes, capture_ms, bearing_deg, phone_lat, phone_lon, fingerprint, sharpness, created_ms, source_sha256, has_thumb`

func scanInboxItem(row interface{ Scan(...any) error }) (inboxItem, error) {
	var it inboxItem
	var capture sql.NullInt64
	var bearing, lat, lon sql.NullFloat64
	var thumb int
	if err := row.Scan(&it.ID, &it.SHA256, &it.Filename, &it.MIME, &it.SizeBytes, &capture, &bearing, &lat, &lon,
		&it.Fingerprint, &it.Sharpness, &it.CreatedMs, &it.SourceSHA256, &thumb); err != nil {
		return it, err
	}
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
	_, err := s.db.Exec(`INSERT INTO inbox_items (`+inboxColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
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
