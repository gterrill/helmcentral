package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// knownSecretKeys is the full set of secret names Helmcentral understands,
// across both trusted host Go code and WASM plugins. This is the single
// source of truth used by the HTTP settings handlers (validation + shape of
// GET/POST /api/settings/secrets) and by ImportFromEnv's one-time migration
// path.
var knownSecretKeys = []string{
	"SIGNALK_USERNAME", "SIGNALK_PASSWORD", "INFLUXDB_TOKEN",
	"SMTP_PASSWORD", "NTFY_TOKEN", "WEATHERKIT_KEY_ID", "WEATHERKIT_TEAM_ID", "WEATHERKIT_SERVICE_ID", "WEATHERKIT_PRIVATE_KEY",
	"VAPID_PUBLIC_KEY", "VAPID_PRIVATE_KEY", "GOOGLE_PLACES_API_KEY",
	// OPENROUTER_API_KEY backs the onboard assistant's outbound LLM calls
	// (ADR 0065 §5, ADR 0093). It belongs in knownSecretKeys only, never in
	// coreEnvSecretKeys: the assistant handlers read it directly from
	// globalSecretsStore, the same way ADR 0065 §5 required for the
	// inventory vision call, so it never enters the process environment
	// where a WASM plugin's ${VAR} config expansion could reach it.
	"OPENROUTER_API_KEY",
}

// coreEnvSecretKeys is the subset of knownSecretKeys trusted, non-sandboxed
// host Go code reads directly via getEnv/os.Getenv. LoadIntoEnv sets ONLY
// these into the process environment. WEATHERKIT_* are deliberately
// excluded - they are plugin-only and must never become globally visible
// via os.Setenv; see the wasm_plugin.go allowlist gate instead.
var coreEnvSecretKeys = []string{
	"SIGNALK_USERNAME", "SIGNALK_PASSWORD", "INFLUXDB_TOKEN",
}

func isKnownSecretKey(key string) bool {
	for _, k := range knownSecretKeys {
		if k == key {
			return true
		}
	}
	return false
}

// isCoreEnvSecretKey reports whether key is one of coreEnvSecretKeys, the
// subset LoadIntoEnv copies into the process environment at boot. Mirrors
// isKnownSecretKey above; clearBoundSecret is the only caller, and needs
// this to know whether deleting the store row is the whole job or whether a
// live os.Unsetenv is also required (see its own doc comment).
func isCoreEnvSecretKey(key string) bool {
	for _, k := range coreEnvSecretKeys {
		if k == key {
			return true
		}
	}
	return false
}

// destinationChanged reports whether a destination-identifying value (a
// hostname, address, or bare URL with no case- or slash-significant path -
// never used for anything else) has meaningfully changed between two saves.
// Comparison is on a normalized form so cosmetic differences that name the
// same place don't count: surrounding whitespace (a resubmitted form field),
// a trailing slash on a URL (validateAlarmTransports already strips this
// for ntfy.Server; influxdb.url gets no such normalization on its way to
// disk, so it needs it here), and a hostname typed in different case (DNS
// names are case-insensitive, so "Boat.Local" and "boat.local" are the same
// destination). This stays deliberately conservative toward NOT clearing:
// callers use it to decide whether to wipe a working credential
// (clearBoundSecret below), and a spurious clear costs an operator a working
// alarm path for no reason - see the E-1 fix in alarm_transports.go and
// signalk.go's updateSettingsHandler for what calls this and why.
func destinationChanged(previous, next string) bool {
	normalize := func(s string) string {
		return strings.ToLower(strings.TrimRight(strings.TrimSpace(s), "/"))
	}
	return normalize(previous) != normalize(next)
}

// clearBoundSecret deletes key from the encrypted store because the
// destination it is sent to (an ntfy/SMTP host, an InfluxDB URL, a SignalK
// address) just changed - this is the E-1 fix from the 2026-09-19 security
// audit (ADR 0111 amendment). Every alarm transport sends its secret to
// whatever destination the matching setting currently names, and that
// setting is free text: point ntfy.server at a server you control, save,
// and NTFY_TOKEN arrives in its Authorization header on the next send. The
// fix is Option B: repointing a destination clears the secret bound to it,
// so the new destination gets nothing until the credential is re-entered.
//
// reason is a short human-readable description of what changed (e.g. "ntfy
// server https://ntfy.sh -> https://attacker.example"), logged alongside the
// key. This is deliberately loud, not a quiet background action: an
// operator whose alarms have gone silent needs to see why in the log,
// exactly as they'd expect after repointing a destination.
//
// globalSecretsStore is checked for nil the way wasm_plugin.go's
// configForWasmPlugin checks it before a read, but the failure mode here is
// the opposite of that read path's. A plugin that can't read a secret just
// runs without it - fail-closed costs nothing there. A destination change
// that can't CLEAR a bound secret would otherwise save the new destination
// next to the old credential, sight unseen - exactly the exfiltration this
// function exists to prevent. So this returns an error instead of
// swallowing it, and every caller must abort the whole settings save on it
// rather than persist the new destination with the old secret still bound
// to it. In production this is unreachable in practice: main() opens
// globalSecretsStore and fails fast (log.Fatalf) if that fails, before any
// route is registered, so by the time a request reaches here the store is
// always open. It is reachable in a test that exercises a destination
// change without first setting up a store - see withTestSecretsStore
// (secrets_settings_handlers_test.go).
func clearBoundSecret(key, reason string) error {
	if globalSecretsStore == nil {
		return fmt.Errorf("secrets store unavailable, cannot clear %s (%s)", key, reason)
	}
	if err := globalSecretsStore.Set(key, ""); err != nil {
		return fmt.Errorf("clearing %s (%s): %w", key, reason, err)
	}
	// SIGNALK_USERNAME, SIGNALK_PASSWORD and INFLUXDB_TOKEN are copied into
	// the process environment once, at boot (LoadIntoEnv), because trusted
	// host code reads them via getEnv/os.Getenv on every call
	// (loadSignalKCredentials, loadInfluxSettings) rather than asking the
	// store fresh each time. Deleting the store row alone would leave that
	// cached copy live in THIS process until its next restart - the exact
	// leak this function exists to close, merely delayed rather than
	// prevented. Unsetting it here closes it immediately instead of on the
	// next reboot.
	if isCoreEnvSecretKey(key) {
		os.Unsetenv(key)
	}
	log.Printf("secrets: cleared %s — %s", key, reason)
	return nil
}

// globalSecretsStore is the process-wide encrypted secrets store, opened
// once in main() before any provider registration runs, and shared by the
// /api/settings/secrets handlers (reads/writes) and the wasm_plugin.go
// per-plugin secrets allowlist gate (reads only).
var globalSecretsStore *secretsStore

func secretsDBPath() string  { return cacheFilePath("SECRETS_DB_PATH", "data/secrets.sqlite") }
func secretsKeyPath() string { return cacheFilePath("SECRETS_KEY_PATH", "data/secrets.key") }

// secretsStore is a SQLite-backed, AES-256-GCM-encrypted-at-rest store of
// arbitrary string secrets, mirroring nearby_contacts.go's SQLite-store
// pattern (single-connection pool, CREATE TABLE IF NOT EXISTS). It is
// deliberately generic - it does not validate keys against knownSecretKeys
// itself; that validation is the HTTP handler's job (see
// secrets_settings_handlers.go), keeping this type reusable for any string
// key.
type secretsStore struct {
	db  *sql.DB
	key [32]byte
}

// newSecretsStore opens (creating if necessary) the SQLite database at
// dbPath and resolves the AES-256-GCM master key, in priority order:
//  1. HELMCENTRAL_MASTER_KEY env var, base64-decoded - must be exactly 32
//     bytes; invalid base64 or wrong length is an immediate, fail-fast
//     error (no fallback to the key file).
//  2. keyPath file, read as 32 raw bytes - wrong length is an error.
//  3. Neither present: generate 32 random bytes and persist them to keyPath
//     (0600, parent dir 0700) for future opens.
//
// After resolving the key, every existing row is decrypted as an integrity
// check. If any row fails to decrypt (wrong/rotated key), newSecretsStore
// returns an error rather than proceeding - no silent regeneration, no
// plaintext fallback, per this repo's fail-fast/no-masking-fallback policy.
// The caller (main.go) is expected to log.Fatalf on this error.
// ensureDirMode0700 creates dir if needed (like os.MkdirAll) and then
// explicitly chmods it to 0700, closing the S-3 security-audit finding:
// os.MkdirAll returns nil WITHOUT touching the mode of a directory that
// already exists, so a bare `os.MkdirAll(dir, 0o700)` only ever produces
// 0700 the very first time a directory is created (and even then, only if
// nothing upstream of it in the process - e.g. an earlier, wider-mode
// MkdirAll call on the very same path - got there first). This data
// directory holds secrets.sqlite (ciphertext + nonce - the plaintext is
// safe, but the "key" column names which secret each row is, and the S-4
// fix below binds that name into the AAD rather than treating it as
// non-sensitive), sessions.sqlite's token-hash table, and the alarm log -
// none of which modernc.org/sqlite creates more restrictively than 0644, so
// the directory mode is the only thing standing between those files and any
// other local account on the box.
func ensureDirMode0700(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	return nil
}

func newSecretsStore(dbPath, keyPath string) (*secretsStore, error) {
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := ensureDirMode0700(dir); err != nil {
			return nil, fmt.Errorf("create secrets store directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open secrets database: %w", err)
	}
	// modernc.org/sqlite surfaces concurrent writes as "database is locked"
	// rather than queueing them itself; capping the pool at one connection
	// makes database/sql queue callers instead, same reasoning as
	// nearby_contacts.go and tile_cache.go's single-writer SQLite usage.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS secrets (
		key         TEXT PRIMARY KEY,
		ciphertext  BLOB NOT NULL,
		nonce       BLOB NOT NULL,
		updated_at  INTEGER NOT NULL
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create secrets table: %w", err)
	}

	key, err := resolveMasterKey(keyPath)
	if err != nil {
		db.Close()
		return nil, err
	}

	store := &secretsStore{db: db, key: key}

	if err := store.verifyExistingRowsDecrypt(keyPath); err != nil {
		db.Close()
		return nil, err
	}

	return store, nil
}

// resolveMasterKey implements the three-step master key resolution order
// documented on newSecretsStore.
func resolveMasterKey(keyPath string) ([32]byte, error) {
	var key [32]byte

	if raw, ok := os.LookupEnv("HELMCENTRAL_MASTER_KEY"); ok {
		decoded, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return key, fmt.Errorf("secrets store: HELMCENTRAL_MASTER_KEY is not valid base64: %w", err)
		}
		if len(decoded) != 32 {
			return key, fmt.Errorf("secrets store: HELMCENTRAL_MASTER_KEY must decode to exactly 32 bytes, got %d", len(decoded))
		}
		copy(key[:], decoded)
		return key, nil
	}

	if raw, err := os.ReadFile(keyPath); err == nil {
		if len(raw) != 32 {
			return key, fmt.Errorf("secrets store: master key file %s must be exactly 32 bytes, got %d", keyPath, len(raw))
		}
		copy(key[:], raw)
		return key, nil
	} else if !os.IsNotExist(err) {
		return key, fmt.Errorf("secrets store: read master key file %s: %w", keyPath, err)
	}

	// Neither env var nor key file: generate and persist a new random key.
	if _, err := rand.Read(key[:]); err != nil {
		return key, fmt.Errorf("secrets store: generate master key: %w", err)
	}
	dir := filepath.Dir(keyPath)
	if dir != "" && dir != "." {
		if err := ensureDirMode0700(dir); err != nil {
			return key, fmt.Errorf("secrets store: create master key directory: %w", err)
		}
	}
	if err := os.WriteFile(keyPath, key[:], 0o600); err != nil {
		return key, fmt.Errorf("secrets store: write generated master key to %s: %w", keyPath, err)
	}
	return key, nil
}

// verifyExistingRowsDecrypt is the fail-fast integrity check run once at
// open time: every row already in the table must decrypt with the resolved
// master key. A single undecryptable row means the key is wrong (rotated,
// lost, or mismatched between HELMCENTRAL_MASTER_KEY and keyPath) and the
// store must refuse to proceed rather than silently treating those secrets
// as absent.
//
// It also performs a one-time AAD migration (S-4, security audit 2026-09):
// every row is now encrypted with its "key" column (the secret's NAME)
// bound in as AES-GCM's additional authenticated data, so a (ciphertext,
// nonce) pair can no longer be swapped onto a different row's key and still
// decrypt - see encrypt/decrypt's own doc comments. A row written before
// this change used a nil AAD, so it will fail decrypt(key, ...) here even
// though the master key is perfectly correct; for each row that fails the
// AAD-bound attempt, a second attempt with a nil AAD (decryptWithAAD(nil,
// ...)) checks whether it's simply pre-migration rather than genuinely
// undecryptable. A row that decrypts under nil AAD is re-encrypted
// AAD-bound and the new (ciphertext, nonce) persisted immediately, logged
// individually - this is the ONE place in this file nil AAD is ever tried,
// and it only ever runs once per row (the next open finds it already
// AAD-bound and takes the fast path above). This is deliberately NOT a
// per-call fallback inside decrypt() itself - Get/Set never try nil AAD -
// so nothing about the store's steady-state behavior silently accepts the
// weaker pre-migration format going forward.
//
// This project has a single operator and no installed base (AGENTS.md), so
// a clean re-encrypt-on-open was chosen over failing the boot check and
// requiring a separate operator-triggered migration step: there is no real
// fleet of existing installs whose data a botched automatic migration could
// put at risk, and re-encrypting is safe to run unconditionally (a row
// already in the new format never reaches the nil-AAD branch at all).
//
// A row that fails BOTH the AAD-bound and nil-AAD attempts is a genuine
// integrity failure (wrong/rotated key) and still refuses to open the
// store, exactly as before this change.
func (s *secretsStore) verifyExistingRowsDecrypt(keyPath string) error {
	rows, err := s.db.Query(`SELECT key, ciphertext, nonce FROM secrets`)
	if err != nil {
		return fmt.Errorf("secrets store: list existing secrets: %w", err)
	}
	defer rows.Close()

	type legacyRow struct {
		key       string
		plaintext string
	}
	var toMigrate []legacyRow

	failed := 0
	for rows.Next() {
		var key string
		var ciphertext, nonce []byte
		if err := rows.Scan(&key, &ciphertext, &nonce); err != nil {
			return fmt.Errorf("secrets store: scan existing secret row: %w", err)
		}
		if _, err := s.decrypt(key, ciphertext, nonce); err == nil {
			continue // already AAD-bound (current format) - nothing to do
		}
		plaintext, legacyErr := s.decryptWithAAD(nil, ciphertext, nonce)
		if legacyErr != nil {
			// Fails under both the current AAD-bound key and the legacy
			// nil-AAD format: a genuine integrity failure, not a format
			// difference.
			failed++
			continue
		}
		toMigrate = append(toMigrate, legacyRow{key: key, plaintext: plaintext})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("secrets store: iterate existing secrets: %w", err)
	}

	if failed > 0 {
		return fmt.Errorf("secrets store: cannot decrypt %d existing secret(s) with the current master key — check HELMCENTRAL_MASTER_KEY or %s", failed, keyPath)
	}

	for _, row := range toMigrate {
		ciphertext, nonce, err := s.encrypt(row.key, row.plaintext)
		if err != nil {
			return fmt.Errorf("secrets store: AAD migration: re-encrypt %s: %w", row.key, err)
		}
		if _, err := s.db.Exec(`UPDATE secrets SET ciphertext = ?, nonce = ? WHERE key = ?`, ciphertext, nonce, row.key); err != nil {
			return fmt.Errorf("secrets store: AAD migration: persist %s: %w", row.key, err)
		}
		log.Printf("secrets store: migrated %q from nil-AAD to key-bound-AAD encryption (S-4, one-time)", row.key)
	}
	if len(toMigrate) > 0 {
		log.Printf("secrets store: AAD migration complete, %d secret(s) re-encrypted", len(toMigrate))
	}

	return nil
}

// encrypt seals plaintext under the store's master key, binding key (the
// secret's own name, e.g. "SIGNALK_PASSWORD") in as AES-GCM's additional
// authenticated data. See decrypt's doc comment for why.
func (s *secretsStore) encrypt(key, plaintext string) (ciphertext, nonce []byte, err error) {
	block, err := aes.NewCipher(s.key[:])
	if err != nil {
		return nil, nil, fmt.Errorf("secrets store: create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("secrets store: create GCM: %w", err)
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("secrets store: generate nonce: %w", err)
	}
	ciphertext = gcm.Seal(nil, nonce, []byte(plaintext), []byte(key))
	return ciphertext, nonce, nil
}

// decrypt opens ciphertext/nonce under the store's master key, requiring
// key (the row's own "key" column) as AES-GCM's additional authenticated
// data (S-4, security audit 2026-09).
//
// Before this, Seal/Open both passed a nil AAD, so the "key" column was
// authenticated by nothing: gcm.Open only checks that ciphertext+nonce are
// internally consistent with the master key, never that they were sealed
// FOR this particular row. A (ciphertext, nonce) pair copied from one row
// into another - a botched restore, a migration bug, direct sqlite
// tampering - would decrypt successfully under the wrong key name, handing
// back a real, validly-decrypted secret that just isn't the one the caller
// asked for (e.g. reading SIGNALK_PASSWORD back with OPENROUTER_API_KEY's
// value). Binding key as AAD makes gcm.Open's authentication tag itself
// depend on which row it's being opened for, so a swapped pair now fails
// closed instead of silently decrypting into the wrong secret.
func (s *secretsStore) decrypt(key string, ciphertext, nonce []byte) (string, error) {
	return s.decryptWithAAD([]byte(key), ciphertext, nonce)
}

// decryptWithAAD is decrypt's shared implementation, taking the AAD
// explicitly rather than always deriving it from a key name. It exists
// ONLY so verifyExistingRowsDecrypt's one-time migration can attempt the
// legacy nil-AAD format (aad == nil) without duplicating the AES/GCM setup
// - ordinary callers (Get, Set, decrypt) always go through decrypt(key,
// ...) and never call this directly, so nothing in the store's normal
// read/write path ever tries more than one AAD per call.
func (s *secretsStore) decryptWithAAD(aad, ciphertext, nonce []byte) (string, error) {
	block, err := aes.NewCipher(s.key[:])
	if err != nil {
		return "", fmt.Errorf("secrets store: create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("secrets store: create GCM: %w", err)
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return "", fmt.Errorf("secrets store: decrypt: %w", err)
	}
	return string(plaintext), nil
}

// Get returns the decrypted value for key, or ok=false if no row exists.
func (s *secretsStore) Get(key string) (value string, ok bool, err error) {
	var ciphertext, nonce []byte
	row := s.db.QueryRow(`SELECT ciphertext, nonce FROM secrets WHERE key = ?`, key)
	if err := row.Scan(&ciphertext, &nonce); err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, fmt.Errorf("secrets store: read %s: %w", key, err)
	}

	plaintext, err := s.decrypt(key, ciphertext, nonce)
	if err != nil {
		return "", false, fmt.Errorf("secrets store: decrypt %s: %w", key, err)
	}
	return plaintext, true, nil
}

// Set encrypts and upserts value under key. An empty value deletes the row
// instead (clearing a secret), so a cleared key returns ok=false from a
// subsequent Get/Has rather than an empty-string value.
func (s *secretsStore) Set(key, value string) error {
	if value == "" {
		if _, err := s.db.Exec(`DELETE FROM secrets WHERE key = ?`, key); err != nil {
			return fmt.Errorf("secrets store: delete %s: %w", key, err)
		}
		return nil
	}

	ciphertext, nonce, err := s.encrypt(key, value)
	if err != nil {
		return err
	}

	if _, err := s.db.Exec(
		`INSERT INTO secrets (key, ciphertext, nonce, updated_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET ciphertext = excluded.ciphertext, nonce = excluded.nonce, updated_at = excluded.updated_at`,
		key, ciphertext, nonce, time.Now().UTC().Unix(),
	); err != nil {
		return fmt.Errorf("secrets store: upsert %s: %w", key, err)
	}
	return nil
}

// Has reports whether key has a stored value, without decrypting it.
func (s *secretsStore) Has(key string) (bool, error) {
	var exists int
	row := s.db.QueryRow(`SELECT 1 FROM secrets WHERE key = ?`, key)
	if err := row.Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("secrets store: check %s: %w", key, err)
	}
	return true, nil
}

// All returns, for every knownSecretKeys entry, whether it is currently set
// (true) or not (false). Never returns plaintext or ciphertext values -
// this backs the GET /api/settings/secrets response shape directly.
func (s *secretsStore) All() (map[string]bool, error) {
	result := make(map[string]bool, len(knownSecretKeys))
	for _, key := range knownSecretKeys {
		has, err := s.Has(key)
		if err != nil {
			return nil, err
		}
		result[key] = has
	}
	return result, nil
}

// LoadIntoEnv sets each coreEnvSecretKeys entry found in the store into the
// process environment via os.Setenv, so trusted host Go code that reads
// secrets via getEnv/os.Getenv (SignalK, InfluxDB) keeps working
// unchanged. WEATHERKIT_* (and any other knownSecretKeys not
// in coreEnvSecretKeys) are deliberately never set here - they are
// plugin-only and reach guests exclusively through the
// allowed_secrets.json-gated path in wasm_plugin.go's configForWasmPlugin.
func (s *secretsStore) LoadIntoEnv() error {
	for _, key := range coreEnvSecretKeys {
		value, ok, err := s.Get(key)
		if err != nil {
			return err
		}
		if ok {
			if err := os.Setenv(key, value); err != nil {
				return fmt.Errorf("secrets store: setenv %s: %w", key, err)
			}
		}
	}
	return nil
}

// ImportFromEnv is the explicit, one-time migration path from the old
// backend/.env-based configuration: for each knownSecretKeys name currently
// set (non-empty) in the process environment, it is written into the store.
// Keys that are empty/unset in the process env are skipped, not cleared -
// so re-running this after the operator has removed .env (and therefore
// those env vars are no longer set) is a safe no-op, never destructive.
// This is deliberately an explicit, operator-triggered action (via POST
// /api/settings/secrets/import-env), not something run silently on every
// startup, per this repo's fallback policy against masking/silent behavior
// changes.
func (s *secretsStore) ImportFromEnv() ([]string, error) {
	var imported []string
	for _, key := range knownSecretKeys {
		value := os.Getenv(key)
		if value == "" {
			continue
		}
		if err := s.Set(key, value); err != nil {
			return nil, err
		}
		imported = append(imported, key)
	}
	sort.Strings(imported)
	return imported, nil
}
