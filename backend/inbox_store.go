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
)

// globalInboxStore is opened once in main() and read by the /api/inbox handlers.
var globalInboxStore *inboxStore

func inboxDirPath() string {
	return cacheFilePath("INBOX_DIR", "data/inbox")
}

// inboxFilePath is where an item's bytes live.
func inboxFilePath(sha string) string {
	return filepath.Join(inboxDirPath(), sha)
}

type inboxItem struct {
	ID        string
	SHA256    string
	Filename  string
	MIME      string
	SizeBytes int64
	// CaptureMs is when the photo was taken, Unix ms, nil when the file says
	// nothing. CaptureOffsetMin is the photo's own offset from UTC in minutes.
	CaptureMs        *int64
	CaptureOffsetMin *int
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
	// SuggestedTitle is reserved for the Mate title suggestions to come.
	SuggestedTitle *string
	// SourceSHA256 is the hash of the original file as the phone held it, or
	// empty when the phone could not work it out.
	SourceSHA256 string
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
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS inbox_items (
		id TEXT PRIMARY KEY,
		sha256 TEXT NOT NULL UNIQUE,
		filename TEXT NOT NULL,
		mime TEXT NOT NULL,
		size_bytes INTEGER NOT NULL,
		capture_ms INTEGER,
		capture_offset_min INTEGER,
		bearing_deg REAL,
		phone_lat REAL,
		phone_lon REAL,
		fingerprint TEXT NOT NULL DEFAULT '',
		sharpness REAL NOT NULL DEFAULT 0,
		created_ms INTEGER NOT NULL,
		suggested_title TEXT
	)`); err != nil {
		return fmt.Errorf("create inbox schema: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_inbox_items_capture ON inbox_items(capture_ms)`); err != nil {
		return fmt.Errorf("create inbox schema: %w", err)
	}
	// The hash of the file as it left the phone, before the re-encode, so the
	// same original is recognised however it was shrunk.
	if err := ensureColumn(db, "inbox_items", "source_sha256", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_inbox_items_source ON inbox_items(source_sha256)`); err != nil {
		return fmt.Errorf("create inbox schema: %w", err)
	}
	// Originals already filed out of the Inbox, so sharing the roll again skips them.
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS inbox_filed (
		source_sha256 TEXT PRIMARY KEY,
		filed_ms INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("create inbox schema: %w", err)
	}
	return nil
}

// HasSource says an item with this original-file hash is waiting.
func (s *inboxStore) HasSource(sha string) (bool, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM inbox_items WHERE source_sha256 = ?`, sha).Scan(&n); err != nil {
		return false, fmt.Errorf("check inbox source hash: %w", err)
	}
	return n > 0, nil
}

// SourceFiled says an original with this hash was filed out of the Inbox before.
func (s *inboxStore) SourceFiled(sha string) (bool, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM inbox_filed WHERE source_sha256 = ?`, sha).Scan(&n); err != nil {
		return false, fmt.Errorf("check filed source hash: %w", err)
	}
	return n > 0, nil
}

// MarkFiled remembers that an original left the Inbox for a passage or Documents.
func (s *inboxStore) MarkFiled(sha string) error {
	if sha == "" {
		return nil
	}
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO inbox_filed (source_sha256, filed_ms) VALUES (?, ?)`, sha, time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("record filed source hash: %w", err)
	}
	return nil
}

const inboxColumns = `id, sha256, filename, mime, size_bytes, capture_ms, capture_offset_min, bearing_deg, phone_lat, phone_lon, fingerprint, sharpness, created_ms, suggested_title, source_sha256`

func scanInboxItem(row interface{ Scan(...any) error }) (inboxItem, error) {
	var it inboxItem
	var capture, offset sql.NullInt64
	var bearing, lat, lon sql.NullFloat64
	var title sql.NullString
	if err := row.Scan(&it.ID, &it.SHA256, &it.Filename, &it.MIME, &it.SizeBytes, &capture, &offset, &bearing, &lat, &lon,
		&it.Fingerprint, &it.Sharpness, &it.CreatedMs, &title, &it.SourceSHA256); err != nil {
		return it, err
	}
	if capture.Valid {
		v := capture.Int64
		it.CaptureMs = &v
	}
	if offset.Valid {
		v := int(offset.Int64)
		it.CaptureOffsetMin = &v
	}
	it.BearingDeg, it.PhoneLat, it.PhoneLon = nullToPtr(bearing), nullToPtr(lat), nullToPtr(lon)
	if title.Valid {
		v := title.String
		it.SuggestedTitle = &v
	}
	return it, nil
}

func nullInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
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
	_, err := s.db.Exec(`INSERT INTO inbox_items (`+inboxColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		it.ID, it.SHA256, it.Filename, it.MIME, it.SizeBytes, nullInt64(it.CaptureMs), nullInt(it.CaptureOffsetMin),
		nullFloat(it.BearingDeg), nullFloat(it.PhoneLat), nullFloat(it.PhoneLon), it.Fingerprint, it.Sharpness, it.CreatedMs, nullableString(it.SuggestedTitle), it.SourceSHA256)
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

// Delete removes the row only; the caller removes the file.
func (s *inboxStore) Delete(id string) error {
	res, err := s.db.Exec(`DELETE FROM inbox_items WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete inbox item: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errInboxItemNotFound
	}
	return nil
}
