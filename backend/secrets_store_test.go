package main

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
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
	if err := store1.Set("STORMGLASS_API_KEY", "sg-key-123"); err != nil {
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

	value, ok, err := store2.Get("STORMGLASS_API_KEY")
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if !ok || value != "sg-key-123" {
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
	if err := store1.Set("STORMGLASS_API_KEY", "sg-key-123"); err != nil {
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
	t.Setenv("STORMGLASS_API_KEY", "sg-key")

	imported, err := store.ImportFromEnv()
	if err != nil {
		t.Fatalf("ImportFromEnv: %v", err)
	}

	want := []string{"SIGNALK_USERNAME", "STORMGLASS_API_KEY"}
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
	if all["STORMGLASS_API_KEY"] {
		t.Errorf("expected STORMGLASS_API_KEY=false in All(), got %+v", all)
	}
}
