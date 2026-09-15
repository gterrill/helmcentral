package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// globalPluginOverridesStore is the process-wide plugin allowlist override
// store, opened once in main() and shared by the wasm_plugin.go
// allowedHostsForWasmPlugin/allowedSecretsForWasmPlugin lookups (reads) and
// the /api/plugins/:type/:id/overrides handlers (reads/writes).
var globalPluginOverridesStore *pluginOverridesStore

func pluginOverridesDBPath() string {
	return cacheFilePath("PLUGIN_OVERRIDES_DB_PATH", "data/plugin_overrides.sqlite")
}

// pluginOverridesStore is a SQLite-backed store of per-plugin allowed-hosts/
// allowed-secrets overrides, mirroring nearby_contacts.go's SQLite-store
// pattern (single-connection pool, CREATE TABLE IF NOT EXISTS). Unlike
// secrets_store.go, this is a NEW sibling store rather than an addition to
// the encrypted secrets store: overrides are plain string arrays (hostnames,
// secret key NAMES - never secret values), so there is nothing here that
// needs encryption at rest, and this store has an entirely different
// lifecycle (one row per plugin file, not one row per secret key). See
// docs/adr/0024-plugin-descriptions-and-allowlist-overrides.md.
type pluginOverridesStore struct{ db *sql.DB }

// newPluginOverridesStore opens (creating if necessary) the SQLite database
// at dbPath and ensures the plugin_overrides table exists. Mirrors
// newNearbyContactStore's idiom exactly; unlike newSecretsStore, there is no
// master-key/encryption integrity check to run at open time - a normal
// sqlite open failure is the only fail-fast condition here.
func newPluginOverridesStore(dbPath string) (*pluginOverridesStore, error) {
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create plugin overrides directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open plugin overrides database: %w", err)
	}
	// modernc.org/sqlite surfaces concurrent writes as "database is locked"
	// rather than queueing them itself; capping the pool at one connection
	// makes database/sql queue callers instead, same reasoning as
	// nearby_contacts.go and secrets_store.go's single-writer SQLite usage.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS plugin_overrides (
		wasm_path       TEXT PRIMARY KEY,
		allowed_hosts   TEXT NOT NULL,
		allowed_secrets TEXT NOT NULL,
		updated_at      INTEGER NOT NULL
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create plugin_overrides table: %w", err)
	}

	// plugin_config_values holds one row per (plugin, declared config field)
	// - the values a plugin author's <name>.config_fields.json sidecar
	// exposes as operator-editable in the Settings provider modal (the
	// "plugins declare their own settings" rewrite of ADR 0100), applied on
	// the very next plugin call with no restart. Unlike plugin_overrides
	// above (one row per plugin, both allowlists together), this is one row
	// per field so a partial save (or the absence of any saved value at all,
	// the normal default) is a natural, ungrouped state - most plugins have
	// zero rows here forever.
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS plugin_config_values (
		wasm_path  TEXT NOT NULL,
		key        TEXT NOT NULL,
		value      TEXT NOT NULL,
		updated_at INTEGER NOT NULL,
		PRIMARY KEY (wasm_path, key)
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create plugin_config_values table: %w", err)
	}

	return &pluginOverridesStore{db: db}, nil
}

// Get returns the stored allowed-hosts/allowed-secrets override for
// wasmPath, or ok=false if no override has been saved for it - the normal
// default state, meaning the caller should fall back to the plugin's
// on-disk <name>.allowed_hosts.json/<name>.allowed_secrets.json companion
// files. wasmPath is the full plugin file path (e.g.
// "plugins/tides/bom.wasm"), NOT (type, id) - see this store's package doc
// and docs/adr/0024 for why: the wasm path is already known before a
// plugin's self-reported id can be determined (allowedHostsForWasmPlugin
// runs before module instantiation), and ids can collide across domains
// (both the tide and forecast-warnings BOM plugins report id "bom").
func (s *pluginOverridesStore) Get(wasmPath string) (allowedHosts, allowedSecrets []string, ok bool, err error) {
	var hostsJSON, secretsJSON string
	row := s.db.QueryRow(`SELECT allowed_hosts, allowed_secrets FROM plugin_overrides WHERE wasm_path = ?`, wasmPath)
	if err := row.Scan(&hostsJSON, &secretsJSON); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil, false, nil
		}
		return nil, nil, false, fmt.Errorf("plugin overrides store: read %s: %w", wasmPath, err)
	}

	var hosts []string
	if err := json.Unmarshal([]byte(hostsJSON), &hosts); err != nil {
		return nil, nil, false, fmt.Errorf("plugin overrides store: parse allowed_hosts for %s: %w", wasmPath, err)
	}
	var secrets []string
	if err := json.Unmarshal([]byte(secretsJSON), &secrets); err != nil {
		return nil, nil, false, fmt.Errorf("plugin overrides store: parse allowed_secrets for %s: %w", wasmPath, err)
	}

	return hosts, secrets, true, nil
}

// Set upserts the allowed-hosts/allowed-secrets override for wasmPath. Both
// arrays are always saved together in one row - there is no partial-update
// path - so a saved override is always internally consistent. nil slices
// are stored as empty JSON arrays, not JSON null, so a subsequent Get never
// needs to distinguish "override sets zero hosts" from "no override" beyond
// the ok flag.
func (s *pluginOverridesStore) Set(wasmPath string, allowedHosts, allowedSecrets []string) error {
	if allowedHosts == nil {
		allowedHosts = []string{}
	}
	if allowedSecrets == nil {
		allowedSecrets = []string{}
	}

	hostsJSON, err := json.Marshal(allowedHosts)
	if err != nil {
		return fmt.Errorf("plugin overrides store: marshal allowed_hosts for %s: %w", wasmPath, err)
	}
	secretsJSON, err := json.Marshal(allowedSecrets)
	if err != nil {
		return fmt.Errorf("plugin overrides store: marshal allowed_secrets for %s: %w", wasmPath, err)
	}

	if _, err := s.db.Exec(
		`INSERT INTO plugin_overrides (wasm_path, allowed_hosts, allowed_secrets, updated_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(wasm_path) DO UPDATE SET allowed_hosts = excluded.allowed_hosts, allowed_secrets = excluded.allowed_secrets, updated_at = excluded.updated_at`,
		wasmPath, string(hostsJSON), string(secretsJSON), time.Now().UTC().Unix(),
	); err != nil {
		return fmt.Errorf("plugin overrides store: upsert %s: %w", wasmPath, err)
	}
	return nil
}

// Delete clears any stored override for wasmPath, so a subsequent lookup
// reverts to the plugin's on-disk companion-file defaults. Deleting a path
// with no existing override is a no-op, not an error.
func (s *pluginOverridesStore) Delete(wasmPath string) error {
	if _, err := s.db.Exec(`DELETE FROM plugin_overrides WHERE wasm_path = ?`, wasmPath); err != nil {
		return fmt.Errorf("plugin overrides store: delete %s: %w", wasmPath, err)
	}
	return nil
}

// GetConfigValues returns every stored plugin-declared config value for
// wasmPath, keyed by config field key. No stored rows is the normal default
// state (an empty, non-nil map, no error) - most plugins declare no
// config_fields at all, or an operator hasn't touched the ones they do
// declare, and wasmPluginBase.call's overlay treats a key's absence here as
// "use whatever config.json (or the plugin's own built-in default)
// already provides."
func (s *pluginOverridesStore) GetConfigValues(wasmPath string) (map[string]string, error) {
	rows, err := s.db.Query(`SELECT key, value FROM plugin_config_values WHERE wasm_path = ?`, wasmPath)
	if err != nil {
		return nil, fmt.Errorf("plugin overrides store: read config values for %s: %w", wasmPath, err)
	}
	defer rows.Close()

	values := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("plugin overrides store: scan config value for %s: %w", wasmPath, err)
		}
		values[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("plugin overrides store: read config values for %s: %w", wasmPath, err)
	}
	return values, nil
}

// SetConfigValues upserts each key/value pair in values for wasmPath. A
// blank (or all-whitespace) value DELETES that key's row instead of storing
// an empty string, so the field reverts to whatever config.json provides
// (or the plugin's own built-in default when config.json has no entry for
// it either) - the same "blank means unset, not literally empty" contract
// every other settings-shaped value in this app follows. All writes for
// this call happen in one transaction, so a failure partway through never
// leaves some keys saved and others not.
func (s *pluginOverridesStore) SetConfigValues(wasmPath string, values map[string]string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("plugin overrides store: begin config values tx for %s: %w", wasmPath, err)
	}
	defer tx.Rollback()

	now := time.Now().UTC().Unix()
	for key, value := range values {
		if strings.TrimSpace(value) == "" {
			if _, err := tx.Exec(`DELETE FROM plugin_config_values WHERE wasm_path = ? AND key = ?`, wasmPath, key); err != nil {
				return fmt.Errorf("plugin overrides store: delete config value %s/%s: %w", wasmPath, key, err)
			}
			continue
		}
		if _, err := tx.Exec(
			`INSERT INTO plugin_config_values (wasm_path, key, value, updated_at) VALUES (?, ?, ?, ?)
			 ON CONFLICT(wasm_path, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
			wasmPath, key, value, now,
		); err != nil {
			return fmt.Errorf("plugin overrides store: upsert config value %s/%s: %w", wasmPath, key, err)
		}
	}
	return tx.Commit()
}

// DeleteConfigValues removes every stored config value for wasmPath.
// Deleting a path with no stored rows is a no-op, not an error - mirrors
// Delete's own no-op-on-missing-row contract above.
func (s *pluginOverridesStore) DeleteConfigValues(wasmPath string) error {
	if _, err := s.db.Exec(`DELETE FROM plugin_config_values WHERE wasm_path = ?`, wasmPath); err != nil {
		return fmt.Errorf("plugin overrides store: delete config values for %s: %w", wasmPath, err)
	}
	return nil
}
