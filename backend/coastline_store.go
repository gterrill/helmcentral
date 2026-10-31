package main

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"
)

// ── offline coastline store (ADR 0203) ───────────────────────────────────
//
// One row per coverage square in helmcentral.sqlite (ADR 0141). The geometry
// is the square's shoreline lines as gzipped JSON in a single blob: a square
// is only ever read or replaced whole. A square that was fetched and has no
// coastline is a valid ready row with an empty line list; a square that has
// never downloaded is a failed row with no blob.
//
// Writes go through RunTx, the same convention as the record store (ADR
// 0184): the store mutex is held across the transaction and fn never sees a
// second connection. The coastline tables are not record tables, so the
// store keeps its own handle and mutex like the alarm log and the AIS log.

const (
	coastlineStatusReady  = "ready"
	coastlineStatusFailed = "failed"
)

type coastlineSquareRecord struct {
	Square       coverageSquare
	Status       string
	DownloadedAt time.Time // zero while status is failed
	// DataAsOf is the date of the Overpass server's map data when the square
	// was downloaded: the point edits are counted from. Zero only for a
	// square held from before it was recorded, which is downloaded again.
	DataAsOf      time.Time
	EditsSince    int
	CheckedAt     time.Time
	LastError     string
	Attempts      int
	NextAttemptAt time.Time
}

// awaitingRefresh is true for a held square stored before data dates were
// kept. It is usable, but the edit check can't count from a date it lacks, so
// it is downloaded again and replaced only when that succeeds.
func (r coastlineSquareRecord) awaitingRefresh() bool {
	return r.Status == coastlineStatusReady && r.DataAsOf.IsZero()
}

type coastlineStore struct {
	mu sync.Mutex
	db *sql.DB
}

func createCoastlineSchema(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS coastline_squares (
			lat             INTEGER NOT NULL,
			lon             INTEGER NOT NULL,
			status          TEXT NOT NULL,
			downloaded_at   INTEGER NOT NULL DEFAULT 0,
			edits_since     INTEGER NOT NULL DEFAULT 0,
			checked_at      INTEGER NOT NULL DEFAULT 0,
			last_error      TEXT NOT NULL DEFAULT '',
			attempts        INTEGER NOT NULL DEFAULT 0,
			next_attempt_at INTEGER NOT NULL DEFAULT 0,
			geometry        BLOB,
			data_as_of      INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (lat, lon)
		)`); err != nil {
		return fmt.Errorf("create coastline squares table: %w", err)
	}
	return addColumnIfMissing(db, "coastline_squares", "data_as_of", "INTEGER NOT NULL DEFAULT 0")
}

func newCoastlineStore(dbPath string) (*coastlineStore, error) {
	db, err := openHelmcentralDB(dbPath)
	if err != nil {
		return nil, err
	}
	if err := createCoastlineSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	// Squares stored before data dates were kept stay as they are: they still
	// serve routing and shelter. The manager downloads them again in the
	// background and replaces each only when that works.
	return &coastlineStore{db: db}, nil
}

func (s *coastlineStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// RunTx runs fn in one transaction on the store's connection and commits
// only when fn returns nil. See store_tx.go for the convention.
func (s *coastlineStore) RunTx(op string, fn func(tx *sql.Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("%s: begin: %w", op, err)
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%s: commit: %w", op, err)
	}
	return nil
}

type storedShorelineLine struct {
	ID     string       `json:"i"`
	Kind   string       `json:"k"`
	Name   string       `json:"n,omitempty"`
	Closed bool         `json:"c,omitempty"`
	Points [][2]float64 `json:"p"`
}

func encodeShorelineLines(lines []shorelineLine) ([]byte, error) {
	stored := make([]storedShorelineLine, len(lines))
	for i, l := range lines {
		pts := make([][2]float64, len(l.Points))
		for j, p := range l.Points {
			pts[j] = [2]float64{p.Lat, p.Lon}
		}
		stored[i] = storedShorelineLine{ID: l.ID, Kind: l.Kind, Name: l.Name, Closed: l.Closed, Points: pts}
	}
	raw, err := json.Marshal(stored)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodeShorelineLines(blob []byte) ([]shorelineLine, error) {
	zr, err := gzip.NewReader(bytes.NewReader(blob))
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	var stored []storedShorelineLine
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, err
	}
	lines := make([]shorelineLine, len(stored))
	for i, l := range stored {
		pts := make([]shorelinePoint, len(l.Points))
		for j, p := range l.Points {
			pts[j] = shorelinePoint{Lat: p[0], Lon: p[1]}
		}
		lines[i] = shorelineLine{ID: l.ID, Kind: l.Kind, Name: l.Name, Closed: l.Closed, Points: pts}
	}
	return lines, nil
}

func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UTC().Unix()
}

func timeOrZero(sec int64) time.Time {
	if sec == 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}

// SaveSquareAsOf stores a freshly downloaded square: ready, no edits since, no
// error, replacing whatever the square held. dataAsOf is the date of the map
// data the server gave it.
func (s *coastlineStore) SaveSquareAsOf(sq coverageSquare, lines []shorelineLine, at, dataAsOf time.Time) error {
	if dataAsOf.IsZero() {
		return fmt.Errorf("save square %s: the server gave no data date", sq.Label())
	}
	blob, err := encodeShorelineLines(lines)
	if err != nil {
		return fmt.Errorf("encode square %s: %w", sq.Label(), err)
	}
	return s.RunTx("save coastline square", func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			INSERT INTO coastline_squares (lat, lon, status, downloaded_at, edits_since, checked_at, last_error, attempts, next_attempt_at, geometry, data_as_of)
			VALUES (?, ?, ?, ?, 0, ?, '', 0, 0, ?, ?)
			ON CONFLICT (lat, lon) DO UPDATE SET
				status = excluded.status, downloaded_at = excluded.downloaded_at, edits_since = 0,
				checked_at = excluded.checked_at, last_error = '', attempts = 0, next_attempt_at = 0,
				geometry = excluded.geometry, data_as_of = excluded.data_as_of`,
			sq.Lat, sq.Lon, coastlineStatusReady, unixOrZero(at), unixOrZero(at), blob, unixOrZero(dataAsOf))
		return err
	})
}

// RecordFailure notes a failed download or check. A square already held
// keeps its geometry and stays ready; a square never downloaded is stored as
// failed so the page can show it.
func (s *coastlineStore) RecordFailure(sq coverageSquare, message string, at, nextAttempt time.Time) error {
	return s.RunTx("record coastline failure", func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			INSERT INTO coastline_squares (lat, lon, status, last_error, attempts, next_attempt_at)
			VALUES (?, ?, ?, ?, 1, ?)
			ON CONFLICT (lat, lon) DO UPDATE SET
				last_error = excluded.last_error, attempts = attempts + 1, next_attempt_at = excluded.next_attempt_at`,
			sq.Lat, sq.Lon, coastlineStatusFailed, message, unixOrZero(nextAttempt))
		return err
	})
}

// DeleteFailed removes a square's row only if it failed and holds no
// geometry; a held square is never touched.
func (s *coastlineStore) DeleteFailed(sq coverageSquare) error {
	return s.RunTx("delete failed coastline square", func(tx *sql.Tx) error {
		_, err := tx.Exec(`DELETE FROM coastline_squares WHERE lat = ? AND lon = ? AND status = ?`, sq.Lat, sq.Lon, coastlineStatusFailed)
		return err
	})
}

// DeferCheck puts a held square's next edit check off until at, without
// counting it as checked. The check reached a working server, so an earlier
// failure no longer stands.
func (s *coastlineStore) DeferCheck(sq coverageSquare, until time.Time) error {
	return s.RunTx("defer coastline check", func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE coastline_squares SET next_attempt_at = ?, last_error = '', attempts = 0 WHERE lat = ? AND lon = ? AND status = ?`,
			unixOrZero(until), sq.Lat, sq.Lon, coastlineStatusReady)
		return err
	})
}

// RecordEdits stores the result of a weekly check on a held square.
func (s *coastlineStore) RecordEdits(sq coverageSquare, edits int, checkedAt time.Time) error {
	return s.RunTx("record coastline edits", func(tx *sql.Tx) error {
		res, err := tx.Exec(`
			UPDATE coastline_squares SET edits_since = ?, checked_at = ?, last_error = '', attempts = 0, next_attempt_at = 0
			WHERE lat = ? AND lon = ? AND status = ?`,
			edits, unixOrZero(checkedAt), sq.Lat, sq.Lon, coastlineStatusReady)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("record edits: square %s is not held", sq.Label())
		}
		return nil
	})
}

func scanCoastlineRecord(row interface{ Scan(...any) error }) (coastlineSquareRecord, error) {
	var r coastlineSquareRecord
	var downloaded, checked, next, asOf int64
	if err := row.Scan(&r.Square.Lat, &r.Square.Lon, &r.Status, &downloaded, &r.EditsSince, &checked, &r.LastError, &r.Attempts, &next, &asOf); err != nil {
		return r, err
	}
	r.DownloadedAt, r.CheckedAt, r.NextAttemptAt, r.DataAsOf = timeOrZero(downloaded), timeOrZero(checked), timeOrZero(next), timeOrZero(asOf)
	return r, nil
}

const coastlineRecordColumns = `lat, lon, status, downloaded_at, edits_since, checked_at, last_error, attempts, next_attempt_at, data_as_of`

// Records returns every square the store has a row for, without geometry.
func (s *coastlineStore) Records() ([]coastlineSquareRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT ` + coastlineRecordColumns + ` FROM coastline_squares ORDER BY lat, lon`)
	if err != nil {
		return nil, fmt.Errorf("read coastline squares: %w", err)
	}
	defer rows.Close()
	var out []coastlineSquareRecord
	for rows.Next() {
		r, err := scanCoastlineRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("read coastline squares: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Lines assembles the shoreline of squares, every one of which must be held.
// Reefs are fetched whole into every square they touch, so they are deduped
// by id; coastline is clipped to its square and is simply joined.
func (s *coastlineStore) Lines(squares []coverageSquare) ([]shorelineLine, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []shorelineLine
	seenReef := map[string]bool{}
	sorted := append([]coverageSquare(nil), squares...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Lat != sorted[j].Lat {
			return sorted[i].Lat < sorted[j].Lat
		}
		return sorted[i].Lon < sorted[j].Lon
	})
	for _, sq := range sorted {
		var blob []byte
		var status string
		err := s.db.QueryRow(`SELECT status, geometry FROM coastline_squares WHERE lat = ? AND lon = ?`, sq.Lat, sq.Lon).Scan(&status, &blob)
		if err == sql.ErrNoRows || (err == nil && (status != coastlineStatusReady || blob == nil)) {
			return nil, fmt.Errorf("coastline square %s is not held", sq.Label())
		}
		if err != nil {
			return nil, fmt.Errorf("read coastline square %s: %w", sq.Label(), err)
		}
		lines, err := decodeShorelineLines(blob)
		if err != nil {
			return nil, fmt.Errorf("decode coastline square %s: %w", sq.Label(), err)
		}
		for _, l := range lines {
			if l.Kind == "reef" {
				if seenReef[l.ID] {
					continue
				}
				seenReef[l.ID] = true
			}
			out = append(out, l)
		}
	}
	return out, nil
}

// Clear deletes every square.
func (s *coastlineStore) Clear() error {
	return s.RunTx("clear coastline coverage", func(tx *sql.Tx) error {
		_, err := tx.Exec(`DELETE FROM coastline_squares`)
		return err
	})
}
