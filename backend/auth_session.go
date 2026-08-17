package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// ── SignalK delegated auth: role constants ──────────────────────────────────
//
// These three are Helmcentral's internal permission tiers, resolved from
// SignalK's own userLevel (see auth_handlers.go's resolveRoleFromUserLevel).
// They are deliberately just strings, not SignalK's own vocabulary renamed -
// Helmcentral does not invent a fourth tier or reorder SignalK's.
const (
	roleReadonly  = "readonly"
	roleReadwrite = "readwrite"
	roleAdmin     = "admin"
)

const (
	// sessionTTL is the sliding session lifetime (docs/adr/0040): a tablet
	// left on the nav station overnight should not be logged out by morning.
	sessionTTL = 7 * 24 * time.Hour

	// sessionRenewThreshold: a session is only slid forward to a fresh
	// sessionTTL if it hasn't been renewed in this long, so a busy session
	// doesn't hit a write on every single request.
	sessionRenewThreshold = 1 * time.Hour

	// sessionTokenBytes is the size of the crypto/rand token minted per
	// login, before base64url encoding for the cookie.
	sessionTokenBytes = 32
)

func sessionsDBPath() string { return cacheFilePath("SESSIONS_DB_PATH", "data/sessions.sqlite") }

// sessionRecord is what a valid token resolves to.
type sessionRecord struct {
	SKUsername string
	Role       string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastSeenAt time.Time
}

// sessionStore is a SQLite-backed store of user sessions, mirroring
// secrets_store.go's open/fail-fast precedent (docs/adr/0023): the process
// should exit rather than run with a broken session store, since a session
// store that silently doesn't persist would make every login look like it
// worked and then vanish.
//
// Only a SHA-256 hash of each session token is ever stored - never the
// plaintext - so a database read (or leak) cannot mint a usable session; see
// docs/adr/0040.
type sessionStore struct {
	db *sql.DB
}

func newSessionStore(dbPath string) (*sessionStore, error) {
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create sessions store directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sessions database: %w", err)
	}
	// Single connection, same reasoning as secrets_store.go and the other
	// SQLite stores in this codebase: modernc.org/sqlite surfaces concurrent
	// writers as "database is locked" rather than queueing them, so capping
	// the pool at one connection makes database/sql do the queueing instead.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS sessions (
		token_hash   TEXT PRIMARY KEY,
		sk_username  TEXT NOT NULL,
		role         TEXT NOT NULL,
		created_at   INTEGER NOT NULL,
		expires_at   INTEGER NOT NULL,
		last_seen_at INTEGER NOT NULL
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create sessions table: %w", err)
	}

	return &sessionStore{db: db}, nil
}

func (s *sessionStore) Close() error { return s.db.Close() }

// generateSessionToken mints 32 crypto/rand bytes, base64url-encoded (no
// padding, so it's a clean cookie value) for the token, and its SHA-256 hash
// (hex) for storage. The two are never derivable from each other in the
// direction that matters: knowing the hash does not yield a token that
// re-hashes to it (short of breaking SHA-256), which is what
// TestSessionStore_StoredHashCannotRoundTripToAUsableToken exercises.
func generateSessionToken() (token, hash string, err error) {
	raw := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("session: generate token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	hash = hashSessionToken(token)
	return token, hash, nil
}

func hashSessionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Create mints a new session for skUsername/role and persists only its hash.
// Returns the plaintext token, which the caller sets as the cookie value -
// this is the only moment the plaintext exists outside the browser.
func (s *sessionStore) Create(skUsername, role string) (token string, err error) {
	token, hash, err := generateSessionToken()
	if err != nil {
		return "", err
	}

	now := time.Now().UTC()
	expiresAt := now.Add(sessionTTL)
	if _, err := s.db.Exec(
		`INSERT INTO sessions (token_hash, sk_username, role, created_at, expires_at, last_seen_at) VALUES (?, ?, ?, ?, ?, ?)`,
		hash, skUsername, role, now.Unix(), expiresAt.Unix(), now.Unix(),
	); err != nil {
		return "", fmt.Errorf("session: insert: %w", err)
	}

	return token, nil
}

// Validate looks up token and returns its session record, or nil (not an
// error) if the token is empty, unknown, or expired - "no session" is an
// ordinary outcome every caller must handle, not a failure. A record found
// more than sessionRenewThreshold past its last_seen_at is slid forward to a
// fresh sessionTTL from now and the new expiry is persisted before
// returning, so a session in active use never silently approaches its
// original expiry.
func (s *sessionStore) Validate(token string) (*sessionRecord, error) {
	if token == "" {
		return nil, nil
	}

	hash := hashSessionToken(token)
	now := time.Now().UTC()

	var rec sessionRecord
	var createdAt, expiresAt, lastSeenAt int64
	row := s.db.QueryRow(`SELECT sk_username, role, created_at, expires_at, last_seen_at FROM sessions WHERE token_hash = ?`, hash)
	if err := row.Scan(&rec.SKUsername, &rec.Role, &createdAt, &expiresAt, &lastSeenAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("session: lookup: %w", err)
	}
	rec.CreatedAt = time.Unix(createdAt, 0).UTC()
	rec.ExpiresAt = time.Unix(expiresAt, 0).UTC()
	rec.LastSeenAt = time.Unix(lastSeenAt, 0).UTC()

	if now.After(rec.ExpiresAt) {
		// Delete the expired row eagerly rather than waiting for the next
		// sweep - the row is provably dead and there is no reason to keep
		// answering queries about it.
		_, _ = s.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, hash)
		return nil, nil
	}

	if now.Sub(rec.LastSeenAt) > sessionRenewThreshold {
		newExpiresAt := now.Add(sessionTTL)
		if _, err := s.db.Exec(
			`UPDATE sessions SET expires_at = ?, last_seen_at = ? WHERE token_hash = ?`,
			newExpiresAt.Unix(), now.Unix(), hash,
		); err != nil {
			return nil, fmt.Errorf("session: renew: %w", err)
		}
		rec.ExpiresAt = newExpiresAt
		rec.LastSeenAt = now
	}

	return &rec, nil
}

// Delete invalidates a session (logout). Deleting an unknown token is a
// no-op, not an error - logging out twice, or logging out with an
// already-expired cookie, must not surface as a failure to the caller.
func (s *sessionStore) Delete(token string) error {
	hash := hashSessionToken(token)
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, hash); err != nil {
		return fmt.Errorf("session: delete: %w", err)
	}
	return nil
}

// Sweep deletes every expired row and returns how many were removed. Called
// once at startup and hourly thereafter (see startSessionSweeper in
// auth_middleware.go), on top of Validate's lazy per-row cleanup, so a
// session nobody ever tries to use again doesn't sit in the table forever.
func (s *sessionStore) Sweep() (int64, error) {
	now := time.Now().UTC().Unix()
	res, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, now)
	if err != nil {
		return 0, fmt.Errorf("session: sweep: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("session: sweep: rows affected: %w", err)
	}
	return n, nil
}
