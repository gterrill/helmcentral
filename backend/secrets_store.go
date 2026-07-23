package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	_ "modernc.org/sqlite"
)

// knownSecretKeys is the full set of secret names Helmcentral understands,
// across both trusted host Go code and WASM plugins. This is the single
// source of truth used by the HTTP settings handlers (validation + shape of
// GET/POST /api/settings/secrets) and by ImportFromEnv's one-time migration
// path.
var knownSecretKeys = []string{
	"SIGNALK_USERNAME", "SIGNALK_PASSWORD", "INFLUXDB_TOKEN", "STORMGLASS_API_KEY",
	"GEONAMES_USERNAME", "WEATHERKIT_KEY_ID", "WEATHERKIT_TEAM_ID", "WEATHERKIT_SERVICE_ID", "WEATHERKIT_PRIVATE_KEY",
}

// coreEnvSecretKeys is the subset of knownSecretKeys trusted, non-sandboxed
// host Go code reads directly via getEnv/os.Getenv. LoadIntoEnv sets ONLY
// these into the process environment. WEATHERKIT_* are deliberately
// excluded - they are plugin-only and must never become globally visible
// via os.Setenv; see the wasm_plugin.go allowlist gate instead.
var coreEnvSecretKeys = []string{
	"SIGNALK_USERNAME", "SIGNALK_PASSWORD", "INFLUXDB_TOKEN", "STORMGLASS_API_KEY", "GEONAMES_USERNAME",
}

func isKnownSecretKey(key string) bool {
	for _, k := range knownSecretKeys {
		if k == key {
			return true
		}
	}
	return false
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
func newSecretsStore(dbPath, keyPath string) (*secretsStore, error) {
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
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
		if err := os.MkdirAll(dir, 0o700); err != nil {
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
func (s *secretsStore) verifyExistingRowsDecrypt(keyPath string) error {
	rows, err := s.db.Query(`SELECT key, ciphertext, nonce FROM secrets`)
	if err != nil {
		return fmt.Errorf("secrets store: list existing secrets: %w", err)
	}
	defer rows.Close()

	failed := 0
	for rows.Next() {
		var key string
		var ciphertext, nonce []byte
		if err := rows.Scan(&key, &ciphertext, &nonce); err != nil {
			return fmt.Errorf("secrets store: scan existing secret row: %w", err)
		}
		if _, err := s.decrypt(ciphertext, nonce); err != nil {
			failed++
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("secrets store: iterate existing secrets: %w", err)
	}

	if failed > 0 {
		return fmt.Errorf("secrets store: cannot decrypt %d existing secret(s) with the current master key — check HELMCENTRAL_MASTER_KEY or %s", failed, keyPath)
	}
	return nil
}

func (s *secretsStore) encrypt(plaintext string) (ciphertext, nonce []byte, err error) {
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
	ciphertext = gcm.Seal(nil, nonce, []byte(plaintext), nil)
	return ciphertext, nonce, nil
}

func (s *secretsStore) decrypt(ciphertext, nonce []byte) (string, error) {
	block, err := aes.NewCipher(s.key[:])
	if err != nil {
		return "", fmt.Errorf("secrets store: create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("secrets store: create GCM: %w", err)
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
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

	plaintext, err := s.decrypt(ciphertext, nonce)
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

	ciphertext, nonce, err := s.encrypt(value)
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
// secrets via getEnv/os.Getenv (SignalK, InfluxDB, Storm Glass, GeoNames)
// keeps working unchanged. WEATHERKIT_* (and any other knownSecretKeys not
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
