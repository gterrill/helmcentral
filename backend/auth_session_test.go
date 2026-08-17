package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestSessionStore(t *testing.T) *sessionStore {
	t.Helper()
	dir := t.TempDir()
	store, err := newSessionStore(filepath.Join(dir, "sessions.sqlite"))
	if err != nil {
		t.Fatalf("newSessionStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestSessionStore_CreateThenValidateRoundTrips(t *testing.T) {
	store := newTestSessionStore(t)

	token, err := store.Create("skipper", roleReadwrite)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if token == "" {
		t.Fatal("expected a non-empty token")
	}

	rec, err := store.Validate(token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if rec == nil {
		t.Fatal("expected a session record, got nil")
	}
	if rec.SKUsername != "skipper" {
		t.Fatalf("expected sk_username %q, got %q", "skipper", rec.SKUsername)
	}
	if rec.Role != roleReadwrite {
		t.Fatalf("expected role %q, got %q", roleReadwrite, rec.Role)
	}
}

func TestSessionStore_ValidateUnknownTokenReturnsNilNotError(t *testing.T) {
	store := newTestSessionStore(t)

	rec, err := store.Validate("this-token-was-never-minted")
	if err != nil {
		t.Fatalf("Validate: unexpected error: %v", err)
	}
	if rec != nil {
		t.Fatalf("expected nil record for an unknown token, got %+v", rec)
	}
}

func TestSessionStore_ValidateEmptyTokenReturnsNilNotError(t *testing.T) {
	store := newTestSessionStore(t)

	rec, err := store.Validate("")
	if err != nil {
		t.Fatalf("Validate: unexpected error: %v", err)
	}
	if rec != nil {
		t.Fatalf("expected nil record for an empty token, got %+v", rec)
	}
}

// TestSessionStore_StoredHashCannotRoundTripToAUsableToken proves the core
// security property of the design: the store never keeps the plaintext
// token, only its SHA-256 hash, so a database read cannot mint a session.
// Presenting the stored hash value itself as if it were the token must NOT
// validate, because Validate hashes whatever it's given before comparing.
func TestSessionStore_StoredHashCannotRoundTripToAUsableToken(t *testing.T) {
	store := newTestSessionStore(t)

	token, err := store.Create("skipper", roleAdmin)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	var storedHash string
	row := store.db.QueryRow(`SELECT token_hash FROM sessions WHERE sk_username = ?`, "skipper")
	if err := row.Scan(&storedHash); err != nil {
		t.Fatalf("reading stored hash directly: %v", err)
	}
	if storedHash == token {
		t.Fatal("expected the stored value to be a hash distinct from the plaintext token")
	}

	// Presenting the stored hash as a token must fail: Validate hashes its
	// input, so hash(storedHash) != storedHash (== the real row's key).
	rec, err := store.Validate(storedHash)
	if err != nil {
		t.Fatalf("Validate(storedHash): unexpected error: %v", err)
	}
	if rec != nil {
		t.Fatal("expected the stored hash to NOT be usable as a session token")
	}
}

func TestSessionStore_DeleteInvalidatesSession(t *testing.T) {
	store := newTestSessionStore(t)

	token, err := store.Create("skipper", roleReadonly)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.Delete(token); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	rec, err := store.Validate(token)
	if err != nil {
		t.Fatalf("Validate after delete: unexpected error: %v", err)
	}
	if rec != nil {
		t.Fatal("expected session to be invalid after Delete")
	}
}

func TestSessionStore_ValidateExpiredSessionReturnsNil(t *testing.T) {
	store := newTestSessionStore(t)

	token, hash, err := generateSessionToken()
	if err != nil {
		t.Fatalf("generateSessionToken: %v", err)
	}

	past := time.Now().UTC().Add(-2 * time.Hour)
	if _, err := store.db.Exec(
		`INSERT INTO sessions (token_hash, sk_username, role, created_at, expires_at, last_seen_at) VALUES (?, ?, ?, ?, ?, ?)`,
		hash, "skipper", roleReadonly, past.Add(-sessionTTL).Unix(), past.Unix(), past.Add(-sessionTTL).Unix(),
	); err != nil {
		t.Fatalf("inserting an already-expired row directly: %v", err)
	}

	rec, err := store.Validate(token)
	if err != nil {
		t.Fatalf("Validate: unexpected error: %v", err)
	}
	if rec != nil {
		t.Fatal("expected an expired session to be invalid")
	}
}

// TestSessionStore_ValidateSlidesExpiryForwardWhenOlderThanRenewThreshold
// covers the plan's "7-day TTL, renewed on use if more than an hour old"
// rule: a session last touched more than sessionRenewThreshold ago has its
// expiry pushed a fresh sessionTTL out from now on the next successful
// validation, so a tablet left on the nav station overnight doesn't log
// itself out.
func TestSessionStore_ValidateSlidesExpiryForwardWhenOlderThanRenewThreshold(t *testing.T) {
	store := newTestSessionStore(t)

	token, hash, err := generateSessionToken()
	if err != nil {
		t.Fatalf("generateSessionToken: %v", err)
	}

	// last_seen_at 2 hours ago (older than the 1-hour renew threshold), but
	// expires_at still comfortably in the future so it's a valid session.
	now := time.Now().UTC()
	lastSeen := now.Add(-2 * time.Hour)
	originalExpiry := now.Add(sessionTTL - 2*time.Hour)
	if _, err := store.db.Exec(
		`INSERT INTO sessions (token_hash, sk_username, role, created_at, expires_at, last_seen_at) VALUES (?, ?, ?, ?, ?, ?)`,
		hash, "skipper", roleReadwrite, lastSeen.Unix(), originalExpiry.Unix(), lastSeen.Unix(),
	); err != nil {
		t.Fatalf("inserting a stale-but-valid row directly: %v", err)
	}

	rec, err := store.Validate(token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if rec == nil {
		t.Fatal("expected the session to still be valid")
	}
	if !rec.ExpiresAt.After(originalExpiry) {
		t.Fatalf("expected expiry to slide forward past the original %v, got %v", originalExpiry, rec.ExpiresAt)
	}

	var storedExpiresAt int64
	row := store.db.QueryRow(`SELECT expires_at FROM sessions WHERE token_hash = ?`, hash)
	if err := row.Scan(&storedExpiresAt); err != nil {
		t.Fatalf("reading persisted expiry: %v", err)
	}
	if storedExpiresAt <= originalExpiry.Unix() {
		t.Fatalf("expected the renewed expiry to be persisted to the database, got %v want > %v", storedExpiresAt, originalExpiry.Unix())
	}
}

func TestSessionStore_ValidateDoesNotRenewWithinThreshold(t *testing.T) {
	store := newTestSessionStore(t)

	token, hash, err := generateSessionToken()
	if err != nil {
		t.Fatalf("generateSessionToken: %v", err)
	}

	now := time.Now().UTC()
	lastSeen := now.Add(-10 * time.Minute) // well within the 1-hour threshold
	originalExpiry := now.Add(sessionTTL - 10*time.Minute)
	if _, err := store.db.Exec(
		`INSERT INTO sessions (token_hash, sk_username, role, created_at, expires_at, last_seen_at) VALUES (?, ?, ?, ?, ?, ?)`,
		hash, "skipper", roleReadonly, lastSeen.Unix(), originalExpiry.Unix(), lastSeen.Unix(),
	); err != nil {
		t.Fatalf("inserting a fresh row directly: %v", err)
	}

	if _, err := store.Validate(token); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	var storedExpiresAt int64
	row := store.db.QueryRow(`SELECT expires_at FROM sessions WHERE token_hash = ?`, hash)
	if err := row.Scan(&storedExpiresAt); err != nil {
		t.Fatalf("reading persisted expiry: %v", err)
	}
	if storedExpiresAt != originalExpiry.Unix() {
		t.Fatalf("expected expiry to be left untouched inside the renew threshold, got %v want %v", storedExpiresAt, originalExpiry.Unix())
	}
}

func TestSessionStore_SweepRemovesOnlyExpiredRows(t *testing.T) {
	store := newTestSessionStore(t)

	liveToken, err := store.Create("live-user", roleReadonly)
	if err != nil {
		t.Fatalf("Create (live): %v", err)
	}

	_, expiredHash, err := generateSessionToken()
	if err != nil {
		t.Fatalf("generateSessionToken: %v", err)
	}
	past := time.Now().UTC().Add(-48 * time.Hour)
	if _, err := store.db.Exec(
		`INSERT INTO sessions (token_hash, sk_username, role, created_at, expires_at, last_seen_at) VALUES (?, ?, ?, ?, ?, ?)`,
		expiredHash, "expired-user", roleReadonly, past.Unix(), past.Add(time.Hour).Unix(), past.Unix(),
	); err != nil {
		t.Fatalf("inserting an expired row directly: %v", err)
	}

	swept, err := store.Sweep()
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if swept != 1 {
		t.Fatalf("expected Sweep to remove exactly 1 expired row, removed %d", swept)
	}

	rec, err := store.Validate(liveToken)
	if err != nil {
		t.Fatalf("Validate (live) after sweep: %v", err)
	}
	if rec == nil {
		t.Fatal("expected the live session to survive the sweep")
	}

	var remainingExpired int
	row := store.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE token_hash = ?`, expiredHash)
	if err := row.Scan(&remainingExpired); err != nil {
		t.Fatalf("counting remaining rows: %v", err)
	}
	if remainingExpired != 0 {
		t.Fatalf("expected the expired row to be gone after Sweep, found %d", remainingExpired)
	}
}

func TestNewSessionStore_FailsFastOnUnwritableParentDirectory(t *testing.T) {
	// Mirrors secrets_store.go's fail-fast precedent: an unopenable database
	// must return an error rather than a store that silently doesn't persist.
	// A file (not a directory) in the path where a directory is expected
	// forces os.MkdirAll to fail.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	_, err := newSessionStore(filepath.Join(blocker, "nested", "sessions.sqlite"))
	if err == nil {
		t.Fatal("expected newSessionStore to fail when its directory can't be created")
	}
}
