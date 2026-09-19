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

// TestSessionStore_ValidateExpiresPastAbsoluteMaxLifetimeEvenIfRecentlyUsed
// is the S-5 security-audit finding: Validate slides expires_at forward on
// every use more than sessionRenewThreshold old, and never once consults
// created_at, so a token used regularly (say, weekly) never actually
// expires - it just keeps sliding forever. This inserts a row whose
// created_at is far beyond sessionAbsoluteMaxLifetime in the past, but
// whose expires_at/last_seen_at look exactly like a session that has been
// kept alive by recent, regular use (the sliding-window check alone would
// call this valid). Validate must still refuse it.
func TestSessionStore_ValidateExpiresPastAbsoluteMaxLifetimeEvenIfRecentlyUsed(t *testing.T) {
	store := newTestSessionStore(t)

	token, hash, err := generateSessionToken()
	if err != nil {
		t.Fatalf("generateSessionToken: %v", err)
	}

	now := time.Now().UTC()
	createdAt := now.Add(-sessionAbsoluteMaxLifetime - 24*time.Hour) // well past the absolute cap
	lastSeen := now.Add(-10 * time.Minute)                           // "used" recently
	expiresAt := now.Add(sessionTTL)                                 // sliding window says "comfortably valid"
	if _, err := store.db.Exec(
		`INSERT INTO sessions (token_hash, sk_username, role, created_at, expires_at, last_seen_at) VALUES (?, ?, ?, ?, ?, ?)`,
		hash, "skipper", roleAdmin, createdAt.Unix(), expiresAt.Unix(), lastSeen.Unix(),
	); err != nil {
		t.Fatalf("inserting a row past the absolute cap directly: %v", err)
	}

	rec, err := store.Validate(token)
	if err != nil {
		t.Fatalf("Validate: unexpected error: %v", err)
	}
	if rec != nil {
		t.Fatalf("expected a session older than sessionAbsoluteMaxLifetime to be invalid regardless of sliding expiry, got %+v", rec)
	}
}

// TestSessionStore_ValidateSlideNeverExceedsAbsoluteMaxLifetime proves the
// sliding renewal itself is capped: a session inside the absolute lifetime
// but close to its edge must not be slid a full fresh sessionTTL past
// created_at - only up to the absolute deadline.
func TestSessionStore_ValidateSlideNeverExceedsAbsoluteMaxLifetime(t *testing.T) {
	store := newTestSessionStore(t)

	token, hash, err := generateSessionToken()
	if err != nil {
		t.Fatalf("generateSessionToken: %v", err)
	}

	now := time.Now().UTC()
	// Created just under 1 hour short of the absolute cap, last used long
	// enough ago to trigger a slide, with a sliding expiry that (before this
	// fix) would jump a full sessionTTL past now - well beyond the absolute
	// deadline computed from created_at.
	createdAt := now.Add(-sessionAbsoluteMaxLifetime + 30*time.Minute)
	lastSeen := now.Add(-2 * time.Hour)
	expiresAt := now.Add(1 * time.Hour)
	if _, err := store.db.Exec(
		`INSERT INTO sessions (token_hash, sk_username, role, created_at, expires_at, last_seen_at) VALUES (?, ?, ?, ?, ?, ?)`,
		hash, "skipper", roleReadwrite, createdAt.Unix(), expiresAt.Unix(), lastSeen.Unix(),
	); err != nil {
		t.Fatalf("inserting a near-cap row directly: %v", err)
	}

	rec, err := store.Validate(token)
	if err != nil {
		t.Fatalf("Validate: unexpected error: %v", err)
	}
	if rec == nil {
		t.Fatal("expected the session to still be valid (inside the absolute cap)")
	}

	absoluteDeadline := createdAt.Add(sessionAbsoluteMaxLifetime)
	if rec.ExpiresAt.After(absoluteDeadline.Add(time.Second)) {
		t.Fatalf("expected the slid expiry to be capped at the absolute deadline %v, got %v", absoluteDeadline, rec.ExpiresAt)
	}
	naiveSlide := now.Add(sessionTTL)
	if rec.ExpiresAt.After(naiveSlide.Add(-time.Hour)) {
		t.Fatalf("expected the slide to be visibly capped short of a naive now+sessionTTL (%v), got %v", naiveSlide, rec.ExpiresAt)
	}
}

// TestSessionStore_DeleteAllForUserInvalidatesOnlyThatUsersSessions is the
// S-5 "no sign-out-everywhere, no role-change revocation" finding:
// Delete(token) only ever takes the single token presented at logout, so
// there was no way to revoke every session belonging to one SignalK
// username at once - needed both for an operator-initiated "sign out all
// my devices" and for invalidating existing sessions after a SignalK role
// change. A second, unrelated user's session must be left untouched.
func TestSessionStore_DeleteAllForUserInvalidatesOnlyThatUsersSessions(t *testing.T) {
	store := newTestSessionStore(t)

	tokenA1, err := store.Create("skipper", roleAdmin)
	if err != nil {
		t.Fatalf("Create (skipper, device 1): %v", err)
	}
	tokenA2, err := store.Create("skipper", roleAdmin)
	if err != nil {
		t.Fatalf("Create (skipper, device 2): %v", err)
	}
	tokenB, err := store.Create("mate", roleReadonly)
	if err != nil {
		t.Fatalf("Create (mate): %v", err)
	}

	n, err := store.DeleteAllForUser("skipper")
	if err != nil {
		t.Fatalf("DeleteAllForUser: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected DeleteAllForUser to report 2 rows removed, got %d", n)
	}

	for _, tok := range []string{tokenA1, tokenA2} {
		rec, err := store.Validate(tok)
		if err != nil {
			t.Fatalf("Validate (skipper token after DeleteAllForUser): %v", err)
		}
		if rec != nil {
			t.Fatalf("expected skipper's session to be invalidated, got %+v", rec)
		}
	}

	rec, err := store.Validate(tokenB)
	if err != nil {
		t.Fatalf("Validate (mate token after skipper's DeleteAllForUser): %v", err)
	}
	if rec == nil {
		t.Fatal("expected mate's unrelated session to survive skipper's DeleteAllForUser")
	}
}

// TestSessionStore_DeleteAllInvalidatesEverySession proves the box-wide
// revocation primitive (S-5) removes every session for every user, not just
// one.
func TestSessionStore_DeleteAllInvalidatesEverySession(t *testing.T) {
	store := newTestSessionStore(t)

	tokenA, err := store.Create("skipper", roleAdmin)
	if err != nil {
		t.Fatalf("Create (skipper): %v", err)
	}
	tokenB, err := store.Create("mate", roleReadonly)
	if err != nil {
		t.Fatalf("Create (mate): %v", err)
	}

	n, err := store.DeleteAll()
	if err != nil {
		t.Fatalf("DeleteAll: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected DeleteAll to report 2 rows removed, got %d", n)
	}

	for _, tok := range []string{tokenA, tokenB} {
		rec, err := store.Validate(tok)
		if err != nil {
			t.Fatalf("Validate after DeleteAll: %v", err)
		}
		if rec != nil {
			t.Fatalf("expected every session to be invalidated by DeleteAll, got %+v", rec)
		}
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
