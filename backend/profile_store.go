package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// globalProfileStore is the process-wide equipment profile store, opened
// once in main() and shared by loadEngineProfiles and the profile handlers.
var globalProfileStore *profileStore

var (
	errProfileNotFound = errors.New("profile not found")
	errProfileExists   = errors.New("profile id already exists")
)

// profileStore keeps the operator's equipment profiles as rows in
// helmcentral.sqlite. Each row holds the canonical profile document; the
// based_on_* columns record which catalogue entry (if any) a profile was
// copied from. Writes are exposed as functions over a *sql.Tx so a caller
// can run them in the same transaction as other writes.
//
// The store does not own a connection. It borrows the document store's, so a
// profile write and a maintenance_rules check run in ONE transaction (a
// profile edit that would strand job state, a profile delete while an item
// still points at it). mu is that document store's mutex: every use of the
// shared single connection, from either side, goes through it.
type profileStore struct {
	db *sql.DB
	mu *sync.Mutex
}

// profileBasedOn names the catalogue entry a profile was copied from. All
// three fields are empty for a profile the operator wrote from scratch.
type profileBasedOn struct {
	Source string
	ID     string
	SHA256 string
}

func createProfileSchema(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS equipment_profiles (
		id               TEXT PRIMARY KEY,
		kind             TEXT NOT NULL,
		body             TEXT NOT NULL,
		based_on_source  TEXT NOT NULL DEFAULT '',
		based_on_id      TEXT NOT NULL DEFAULT '',
		based_on_sha256  TEXT NOT NULL DEFAULT '',
		created_at       INTEGER NOT NULL,
		updated_at       INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("create equipment_profiles table: %w", err)
	}
	return nil
}

// newProfileStore opens the profile table on ds's database handle. ds must
// outlive the returned store, and nothing inside an inTx callback may call a
// documentStore method (they take the same mutex and connection).
func newProfileStore(ds *documentStore) (*profileStore, error) {
	if err := createProfileSchema(ds.db); err != nil {
		return nil, err
	}
	return &profileStore{db: ds.db, mu: &ds.mu}, nil
}

// inTx runs fn in one transaction, committing when it returns nil. The
// transaction is the document store's own connection, so fn may read and
// write maintenance_rules and equipment as well as equipment_profiles.
func (s *profileStore) inTx(fn func(tx *sql.Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("profile store: begin: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("profile store: commit: %w", err)
	}
	return nil
}

func profileExistsTx(tx *sql.Tx, id string) (bool, error) {
	var one int
	err := tx.QueryRow(`SELECT 1 FROM equipment_profiles WHERE id = ?`, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("profile store: check %s: %w", id, err)
	}
	return true, nil
}

// insertProfileTx adds a profile row, returning errProfileExists if the id is
// taken. The caller is expected to have validated the profile already.
func insertProfileTx(tx *sql.Tx, p engineProfile, basedOn profileBasedOn) error {
	exists, err := profileExistsTx(tx, p.ID)
	if err != nil {
		return err
	}
	if exists {
		return errProfileExists
	}
	body, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("profile store: encode %s: %w", p.ID, err)
	}
	now := time.Now().UTC().Unix()
	if _, err := tx.Exec(`INSERT INTO equipment_profiles
		(id, kind, body, based_on_source, based_on_id, based_on_sha256, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Kind, string(body), basedOn.Source, basedOn.ID, basedOn.SHA256, now, now); err != nil {
		return fmt.Errorf("profile store: insert %s: %w", p.ID, err)
	}
	return nil
}

// updateProfileTx replaces a profile's document, leaving its based_on_*
// columns alone. It returns errProfileNotFound if no such row exists.
func updateProfileTx(tx *sql.Tx, p engineProfile) error {
	body, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("profile store: encode %s: %w", p.ID, err)
	}
	res, err := tx.Exec(`UPDATE equipment_profiles SET kind = ?, body = ?, updated_at = ? WHERE id = ?`,
		p.Kind, string(body), time.Now().UTC().Unix(), p.ID)
	if err != nil {
		return fmt.Errorf("profile store: update %s: %w", p.ID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("profile store: update %s: %w", p.ID, err)
	}
	if n == 0 {
		return errProfileNotFound
	}
	return nil
}

// deleteProfileTx removes a profile row, returning errProfileNotFound if
// there was none.
func deleteProfileTx(tx *sql.Tx, id string) error {
	res, err := tx.Exec(`DELETE FROM equipment_profiles WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("profile store: delete %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("profile store: delete %s: %w", id, err)
	}
	if n == 0 {
		return errProfileNotFound
	}
	return nil
}

// getProfileTx reads one stored profile. The stored document is decoded
// as-is; loadEngineProfiles is what re-validates rows.
func getProfileTx(tx *sql.Tx, id string) (engineProfile, profileBasedOn, error) {
	var body string
	var basedOn profileBasedOn
	err := tx.QueryRow(`SELECT body, based_on_source, based_on_id, based_on_sha256 FROM equipment_profiles WHERE id = ?`, id).
		Scan(&body, &basedOn.Source, &basedOn.ID, &basedOn.SHA256)
	if errors.Is(err, sql.ErrNoRows) {
		return engineProfile{}, profileBasedOn{}, errProfileNotFound
	}
	if err != nil {
		return engineProfile{}, profileBasedOn{}, fmt.Errorf("profile store: read %s: %w", id, err)
	}
	var p engineProfile
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		return engineProfile{}, profileBasedOn{}, fmt.Errorf("profile store: decode %s: %w", id, err)
	}
	return p, basedOn, nil
}

// profileRow is one raw stored row, before validation.
type profileRow struct {
	ID   string
	Body string
}

// allRows returns every stored row, oldest id first.
func (s *profileStore) allRows() ([]profileRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id, body FROM equipment_profiles ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("profile store: list: %w", err)
	}
	defer rows.Close()
	var out []profileRow
	for rows.Next() {
		var r profileRow
		if err := rows.Scan(&r.ID, &r.Body); err != nil {
			return nil, fmt.Errorf("profile store: list: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *profileStore) count() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM equipment_profiles`).Scan(&n); err != nil {
		return 0, fmt.Errorf("profile store: count: %w", err)
	}
	return n, nil
}

func (s *profileStore) ids() (map[string]bool, error) {
	rows, err := s.allRows()
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(rows))
	for _, r := range rows {
		out[r.ID] = true
	}
	return out, nil
}
