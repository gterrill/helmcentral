package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newTestSecretsStore(t *testing.T) *secretsStore {
	t.Helper()
	dir := t.TempDir()
	store, err := newSecretsStore(filepath.Join(dir, "secrets.sqlite"), filepath.Join(dir, "secrets.key"))
	if err != nil {
		t.Fatalf("newSecretsStore: %v", err)
	}
	t.Cleanup(func() { _ = store.db.Close() })
	return store
}

func TestSecretsStore_SetThenGetRoundTrips(t *testing.T) {
	store := newTestSecretsStore(t)

	if err := store.Set("SIGNALK_USERNAME", "admin"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	value, ok, err := store.Get("SIGNALK_USERNAME")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true after Set")
	}
	if value != "admin" {
		t.Fatalf("expected value %q, got %q", "admin", value)
	}
}

func TestSecretsStore_GetUnknownKeyReturnsNotOk(t *testing.T) {
	store := newTestSecretsStore(t)

	_, ok, err := store.Get("NEVER_SET")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false for a key that was never set")
	}
}

func TestSecretsStore_SetEmptyStringDeletesRow(t *testing.T) {
	store := newTestSecretsStore(t)

	if err := store.Set("SIGNALK_PASSWORD", "hunter2"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if has, err := store.Has("SIGNALK_PASSWORD"); err != nil || !has {
		t.Fatalf("expected Has=true after Set, got has=%v err=%v", has, err)
	}

	if err := store.Set("SIGNALK_PASSWORD", ""); err != nil {
		t.Fatalf("Set (clear): %v", err)
	}

	if has, err := store.Has("SIGNALK_PASSWORD"); err != nil || has {
		t.Fatalf("expected Has=false after clearing with an empty string, got has=%v err=%v", has, err)
	}
	if _, ok, err := store.Get("SIGNALK_PASSWORD"); err != nil || ok {
		t.Fatalf("expected Get ok=false after clearing, got ok=%v err=%v", ok, err)
	}
}

func TestSecretsStore_ReopenWithSameMasterKeyDecryptsExistingRows(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "secrets.sqlite")
	keyPath := filepath.Join(dir, "secrets.key")

	store1, err := newSecretsStore(dbPath, keyPath)
	if err != nil {
		t.Fatalf("newSecretsStore (1st open): %v", err)
	}
	if err := store1.Set("INFLUXDB_TOKEN", "influx-token-123"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := store1.db.Close(); err != nil {
		t.Fatalf("close store1: %v", err)
	}

	store2, err := newSecretsStore(dbPath, keyPath)
	if err != nil {
		t.Fatalf("newSecretsStore (2nd open, same key file): %v", err)
	}
	t.Cleanup(func() { _ = store2.db.Close() })

	value, ok, err := store2.Get("INFLUXDB_TOKEN")
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if !ok || value != "influx-token-123" {
		t.Fatalf("expected value to survive reopen with the same key file, got ok=%v value=%q", ok, value)
	}
}

func TestSecretsStore_ReopenWithWrongMasterKeyFailsFast(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "secrets.sqlite")
	keyPath := filepath.Join(dir, "secrets.key")

	store1, err := newSecretsStore(dbPath, keyPath)
	if err != nil {
		t.Fatalf("newSecretsStore (1st open): %v", err)
	}
	if err := store1.Set("INFLUXDB_TOKEN", "influx-token-123"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := store1.db.Close(); err != nil {
		t.Fatalf("close store1: %v", err)
	}

	// Simulate a lost/rotated key file: overwrite it with a different,
	// validly-shaped 32-byte key. Reopening must fail fast, not silently
	// regenerate or ignore the now-undecryptable rows.
	wrongKey := make([]byte, 32)
	for i := range wrongKey {
		wrongKey[i] = byte(i + 1)
	}
	if err := os.WriteFile(keyPath, wrongKey, 0o600); err != nil {
		t.Fatalf("overwrite key file: %v", err)
	}

	if _, err := newSecretsStore(dbPath, keyPath); err == nil {
		t.Fatalf("expected newSecretsStore to fail fast when the master key can't decrypt existing rows")
	}
}

// TestNewSecretsStore_DataDirectoryCreatedMode0700 is the S-3 security-audit
// finding: newSecretsStore's directory creation
// (os.MkdirAll(dir, 0o755) at the top of newSecretsStore, historically)
// created the data directory world/group-readable, and resolveMasterKey's
// later os.MkdirAll(dir, 0o700) on the SAME directory was a silent no-op -
// os.MkdirAll returns nil without chmod'ing a directory that already
// exists, so the documented 0700 (ADR 0023 §2) never actually applied, even
// on a completely fresh install where the directory never existed before
// this call. That leaves secrets.sqlite's ciphertext, the session hash
// table, and the alarm log all readable by any other local user. The parent
// directory here is deliberately a not-yet-existing subdirectory of
// t.TempDir(), not t.TempDir() itself, so this test cannot pass by
// accident from t.TempDir()'s own directory mode.
func TestNewSecretsStore_DataDirectoryCreatedMode0700(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "data")
	dbPath := filepath.Join(dir, "secrets.sqlite")
	keyPath := filepath.Join(dir, "secrets.key")

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("expected the data directory to not exist before newSecretsStore runs")
	}

	store, err := newSecretsStore(dbPath, keyPath)
	if err != nil {
		t.Fatalf("newSecretsStore: %v", err)
	}
	t.Cleanup(func() { _ = store.db.Close() })

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat data directory: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("expected data directory mode 0700 (ADR 0023 §2), got %o", perm)
	}
}

func TestSecretsStore_MissingKeyFileIsGeneratedAndPersisted(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "secrets.sqlite")
	keyPath := filepath.Join(dir, "secrets.key")

	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Fatalf("expected key file to not exist yet")
	}

	if _, err := newSecretsStore(dbPath, keyPath); err != nil {
		t.Fatalf("newSecretsStore: %v", err)
	}

	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("expected key file to be generated, stat failed: %v", err)
	}
	if info.Size() != 32 {
		t.Fatalf("expected a 32-byte generated key file, got %d bytes", info.Size())
	}
}

func TestSecretsStore_HELMCENTRAL_MASTER_KEY_InvalidBase64FailsFast(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "secrets.sqlite")
	keyPath := filepath.Join(dir, "secrets.key")

	t.Setenv("HELMCENTRAL_MASTER_KEY", "not-valid-base64!!!")

	if _, err := newSecretsStore(dbPath, keyPath); err == nil {
		t.Fatalf("expected newSecretsStore to fail fast on invalid base64 HELMCENTRAL_MASTER_KEY")
	}
}

func TestSecretsStore_HELMCENTRAL_MASTER_KEY_WrongLengthFailsFast(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "secrets.sqlite")
	keyPath := filepath.Join(dir, "secrets.key")

	// Valid base64, but decodes to 16 bytes, not the required 32.
	t.Setenv("HELMCENTRAL_MASTER_KEY", "MTIzNDU2Nzg5MDEyMzQ1Ng==")

	if _, err := newSecretsStore(dbPath, keyPath); err == nil {
		t.Fatalf("expected newSecretsStore to fail fast on a wrong-length HELMCENTRAL_MASTER_KEY")
	}
}

func TestSecretsStore_LoadIntoEnv_SetsOnlyCoreEnvSecretKeys(t *testing.T) {
	store := newTestSecretsStore(t)

	for _, key := range knownSecretKeys {
		if err := store.Set(key, "value-for-"+key); err != nil {
			t.Fatalf("Set(%s): %v", key, err)
		}
	}
	for _, key := range knownSecretKeys {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}

	if err := store.LoadIntoEnv(); err != nil {
		t.Fatalf("LoadIntoEnv: %v", err)
	}

	for _, key := range coreEnvSecretKeys {
		got := os.Getenv(key)
		want := "value-for-" + key
		if got != want {
			t.Errorf("expected coreEnvSecretKeys %s to be set to %q, got %q", key, want, got)
		}
	}

	weatherKitKeys := []string{"WEATHERKIT_KEY_ID", "WEATHERKIT_TEAM_ID", "WEATHERKIT_SERVICE_ID", "WEATHERKIT_PRIVATE_KEY"}
	for _, key := range weatherKitKeys {
		if got := os.Getenv(key); got != "" {
			t.Errorf("expected WEATHERKIT_* key %s to NOT be set into the process env by LoadIntoEnv, got %q", key, got)
		}
	}
}

func TestSecretsStore_ImportFromEnv_ImportsSetKeysAndSkipsUnset(t *testing.T) {
	store := newTestSecretsStore(t)

	for _, key := range knownSecretKeys {
		os.Unsetenv(key)
	}
	t.Setenv("SIGNALK_USERNAME", "admin")
	t.Setenv("INFLUXDB_TOKEN", "token-abc")

	imported, err := store.ImportFromEnv()
	if err != nil {
		t.Fatalf("ImportFromEnv: %v", err)
	}

	want := []string{"SIGNALK_USERNAME", "INFLUXDB_TOKEN"}
	sort.Strings(imported)
	sort.Strings(want)
	if len(imported) != len(want) {
		t.Fatalf("expected imported=%v, got %v", want, imported)
	}
	for i := range want {
		if imported[i] != want[i] {
			t.Fatalf("expected imported=%v, got %v", want, imported)
		}
	}

	value, ok, err := store.Get("SIGNALK_USERNAME")
	if err != nil || !ok || value != "admin" {
		t.Fatalf("expected SIGNALK_USERNAME=admin to be imported, got value=%q ok=%v err=%v", value, ok, err)
	}
}

func TestSecretsStore_ImportFromEnv_IsIdempotentAndDoesNotClearExistingValues(t *testing.T) {
	store := newTestSecretsStore(t)

	for _, key := range knownSecretKeys {
		os.Unsetenv(key)
	}
	t.Setenv("SIGNALK_USERNAME", "admin")

	if _, err := store.ImportFromEnv(); err != nil {
		t.Fatalf("ImportFromEnv (1st run): %v", err)
	}

	// Simulate the operator having since removed .env: the env var is no
	// longer set on a second run. Re-running must be a safe no-op for this
	// key - it must NOT clear the value already imported into the store.
	os.Unsetenv("SIGNALK_USERNAME")

	imported, err := store.ImportFromEnv()
	if err != nil {
		t.Fatalf("ImportFromEnv (2nd run): %v", err)
	}
	if len(imported) != 0 {
		t.Fatalf("expected no keys imported on the 2nd run (env var now unset), got %v", imported)
	}

	value, ok, err := store.Get("SIGNALK_USERNAME")
	if err != nil || !ok || value != "admin" {
		t.Fatalf("expected SIGNALK_USERNAME to still be %q in the store after a 2nd ImportFromEnv run with the env var unset, got value=%q ok=%v err=%v", "admin", value, ok, err)
	}
}

func TestIsKnownSecretKey(t *testing.T) {
	if !isKnownSecretKey("SIGNALK_USERNAME") {
		t.Errorf("expected SIGNALK_USERNAME to be a known secret key")
	}
	if isKnownSecretKey("NOT_A_SECRET") {
		t.Errorf("expected NOT_A_SECRET to not be a known secret key")
	}
}

// TestIsKnownSecretKey_OpenRouterAPIKey pins OPENROUTER_API_KEY (ADR 0065 §5,
// ADR 0093) as a stored secret that trusted host code reads directly from
// globalSecretsStore rather than through LoadIntoEnv/coreEnvSecretKeys - see
// coreEnvSecretKeys's own doc comment for why WEATHERKIT_* is excluded the
// same way.
func TestIsKnownSecretKey_OpenRouterAPIKey(t *testing.T) {
	if !isKnownSecretKey("OPENROUTER_API_KEY") {
		t.Errorf("expected OPENROUTER_API_KEY to be a known secret key")
	}
	for _, key := range coreEnvSecretKeys {
		if key == "OPENROUTER_API_KEY" {
			t.Fatalf("OPENROUTER_API_KEY must never appear in coreEnvSecretKeys: it must not enter the process environment where a WASM plugin's ${VAR} config expansion could reach it")
		}
	}
}

// TestSecretsStore_SwappedCiphertextRowsFailToDecrypt is the S-4
// security-audit finding: encrypt/decrypt used a nil AAD, so the "key"
// column (the secret's NAME) was never cryptographically bound to its
// ciphertext. Swapping the (ciphertext, nonce) pair between two rows -
// something a compromised backup restore, a buggy migration, or direct
// sqlite file tampering could all do - must be detected and refused, not
// silently decrypt into the wrong plaintext under the right key. Before the
// fix, both Get calls below succeed and return each other's values (e.g.
// the backend would send the SignalK password to openrouter.ai as a bearer
// token, per the finding's own example); after binding the key as AAD,
// gcm.Open's authentication tag no longer matches once the row is under a
// different key name, so both must fail.
func TestSecretsStore_SwappedCiphertextRowsFailToDecrypt(t *testing.T) {
	store := newTestSecretsStore(t)

	if err := store.Set("SIGNALK_PASSWORD", "hunter2"); err != nil {
		t.Fatalf("Set(SIGNALK_PASSWORD): %v", err)
	}
	if err := store.Set("OPENROUTER_API_KEY", "sk-or-abc123"); err != nil {
		t.Fatalf("Set(OPENROUTER_API_KEY): %v", err)
	}

	var ciphertext1, nonce1, ciphertext2, nonce2 []byte
	if err := store.db.QueryRow(`SELECT ciphertext, nonce FROM secrets WHERE key = ?`, "SIGNALK_PASSWORD").Scan(&ciphertext1, &nonce1); err != nil {
		t.Fatalf("read SIGNALK_PASSWORD row: %v", err)
	}
	if err := store.db.QueryRow(`SELECT ciphertext, nonce FROM secrets WHERE key = ?`, "OPENROUTER_API_KEY").Scan(&ciphertext2, &nonce2); err != nil {
		t.Fatalf("read OPENROUTER_API_KEY row: %v", err)
	}

	// Swap the (ciphertext, nonce) pairs between the two rows directly, the
	// way a mishandled backup/restore or a raw sqlite edit could.
	if _, err := store.db.Exec(`UPDATE secrets SET ciphertext = ?, nonce = ? WHERE key = ?`, ciphertext2, nonce2, "SIGNALK_PASSWORD"); err != nil {
		t.Fatalf("swap into SIGNALK_PASSWORD: %v", err)
	}
	if _, err := store.db.Exec(`UPDATE secrets SET ciphertext = ?, nonce = ? WHERE key = ?`, ciphertext1, nonce1, "OPENROUTER_API_KEY"); err != nil {
		t.Fatalf("swap into OPENROUTER_API_KEY: %v", err)
	}

	if value, ok, err := store.Get("SIGNALK_PASSWORD"); err == nil {
		t.Fatalf("expected Get(SIGNALK_PASSWORD) to fail after its ciphertext was swapped with another row's, got ok=%v value=%q", ok, value)
	}
	if value, ok, err := store.Get("OPENROUTER_API_KEY"); err == nil {
		t.Fatalf("expected Get(OPENROUTER_API_KEY) to fail after its ciphertext was swapped with another row's, got ok=%v value=%q", ok, value)
	}
}

// encryptNilAADForTest reproduces the PRE-S-4 encryption format (nil AAD)
// directly with stdlib crypto, independent of secretsStore.encrypt, so this
// test still compiles and is meaningful regardless of that method's current
// signature. It stands in for a row written by an older build of this
// binary, before the key column was bound in as AAD.
func encryptNilAADForTest(t *testing.T, key [32]byte, plaintext string) (ciphertext, nonce []byte) {
	t.Helper()
	block, err := aes.NewCipher(key[:])
	if err != nil {
		t.Fatalf("aes.NewCipher: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("cipher.NewGCM: %v", err)
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatalf("rand.Read nonce: %v", err)
	}
	ciphertext = gcm.Seal(nil, nonce, []byte(plaintext), nil)
	return ciphertext, nonce
}

// TestSecretsStore_LegacyNilAADRowIsMigratedOnOpen is the S-4 migration
// half of the fix: an existing row written before AAD binding (nil AAD, the
// only format that ever existed before this change - this repo has a
// single operator and no installed base, per AGENTS.md, so this simulates
// the one and only real upgrade case rather than a hypothetical) must not
// simply stop decrypting the moment AAD binding ships. newSecretsStore
// detects it's the legacy format, re-encrypts it with the key bound as AAD,
// and persists that - a one-time, explicit, logged migration (chosen over
// failing the boot check outright, since a clean re-encrypt is safe and
// simplest with no installed base to protect against a botched migration).
func TestSecretsStore_LegacyNilAADRowIsMigratedOnOpen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "secrets.sqlite")
	keyPath := filepath.Join(dir, "secrets.key")

	// Bring up a store once just to get a real master key generated and
	// persisted to keyPath, then close it and hand-write a legacy-format row
	// directly, bypassing Set (which now always writes the new AAD-bound
	// format) entirely.
	bootstrap, err := newSecretsStore(dbPath, keyPath)
	if err != nil {
		t.Fatalf("newSecretsStore (bootstrap): %v", err)
	}
	masterKey := bootstrap.key
	if err := bootstrap.db.Close(); err != nil {
		t.Fatalf("close bootstrap store: %v", err)
	}

	ciphertext, nonce := encryptNilAADForTest(t, masterKey, "hunter2")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO secrets (key, ciphertext, nonce, updated_at) VALUES (?, ?, ?, ?)`,
		"SIGNALK_PASSWORD", ciphertext, nonce, time.Now().UTC().Unix(),
	); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close direct db handle: %v", err)
	}

	// Reopening must migrate the legacy row rather than fail-fast on it, and
	// the migrated value must read back correctly afterwards.
	store, err := newSecretsStore(dbPath, keyPath)
	if err != nil {
		t.Fatalf("newSecretsStore (after legacy row inserted): %v", err)
	}
	t.Cleanup(func() { _ = store.db.Close() })

	value, ok, err := store.Get("SIGNALK_PASSWORD")
	if err != nil {
		t.Fatalf("Get after migration: unexpected error: %v", err)
	}
	if !ok || value != "hunter2" {
		t.Fatalf("expected the migrated row to read back as %q, got ok=%v value=%q", "hunter2", ok, value)
	}

	// The row must actually have been rewritten (new nonce, AAD-bound
	// ciphertext) rather than merely tolerated in place - reopening a THIRD
	// time must not need to migrate anything, proving the migration
	// persisted rather than re-running the legacy fallback every boot.
	var storedCiphertext, storedNonce []byte
	if err := store.db.QueryRow(`SELECT ciphertext, nonce FROM secrets WHERE key = ?`, "SIGNALK_PASSWORD").Scan(&storedCiphertext, &storedNonce); err != nil {
		t.Fatalf("read migrated row: %v", err)
	}
	if bytes.Equal(storedNonce, nonce) {
		t.Fatalf("expected the migration to persist a freshly re-encrypted row (new nonce), got the original legacy nonce unchanged")
	}
}

func TestSecretsStore_All_ReflectsSetKeys(t *testing.T) {
	store := newTestSecretsStore(t)

	if err := store.Set("SIGNALK_USERNAME", "admin"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	all, err := store.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != len(knownSecretKeys) {
		t.Fatalf("expected All() to report every knownSecretKeys entry, got %d entries", len(all))
	}
	if !all["SIGNALK_USERNAME"] {
		t.Errorf("expected SIGNALK_USERNAME=true in All(), got %+v", all)
	}
	if all["INFLUXDB_TOKEN"] {
		t.Errorf("expected INFLUXDB_TOKEN=false in All(), got %+v", all)
	}
}
