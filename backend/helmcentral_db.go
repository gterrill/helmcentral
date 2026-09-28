package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// helmcentralDBPath is the one business database (ADR 0141): documents,
// notes, the equipment registry, maintenance, checklists, Mate's
// conversation history, the AIS sighting log, the alarm log and
// notification queue, registered web push devices, and plugin
// overrides/config values all live in this single file. It replaces
// DOCUMENTS_DB_PATH, ASSISTANT_DB_PATH, NEARBY_CONTACTS_DB_PATH,
// ALARM_LOG_DB, WEBPUSH_DB_PATH and PLUGIN_OVERRIDES_DB_PATH outright -
// there is no compatibility alias for any of the six, per this codebase's
// no-speculative-fallback rule - so a pre-0141 install has to run the
// one-off `migrate-db` subcommand below before it starts
// (checkForLegacyDatabaseFiles).
func helmcentralDBPath() string {
	return cacheFilePath("HELMCENTRAL_DB_PATH", "data/helmcentral.sqlite")
}

// legacyDocumentsDBPath through legacyPluginOverridesDBPath resolve where
// the six files helmcentralDBPath replaces used to live.
// DOCUMENTS_DB_PATH/ASSISTANT_DB_PATH/NEARBY_CONTACTS_DB_PATH/ALARM_LOG_DB/
// WEBPUSH_DB_PATH/PLUGIN_OVERRIDES_DB_PATH are gone as runtime configuration
// - HELMCENTRAL_DB_PATH is the only environment variable that names a
// business-database path now - but migrate-db still has to find a real
// install's existing files, including one that set a non-default path via
// one of the old variables. These six exist for migrate-db's own use only
// (runMigrateDBCommand); nothing else in this codebase calls them.
func legacyDocumentsDBPath() string {
	return cacheFilePath("DOCUMENTS_DB_PATH", "data/documents.sqlite")
}

func legacyAssistantDBPath() string {
	return cacheFilePath("ASSISTANT_DB_PATH", "data/assistant.sqlite")
}

func legacyNearbyContactsDBPath() string {
	return cacheFilePath("NEARBY_CONTACTS_DB_PATH", "data/nearby-contacts.sqlite")
}

func legacyAlarmLogDBPath() string {
	return cacheFilePath("ALARM_LOG_DB", "data/alarm-log.sqlite")
}

func legacyWebPushDBPath() string {
	return cacheFilePath("WEBPUSH_DB_PATH", "data/webpush-subscriptions.sqlite")
}

func legacyPluginOverridesDBPath() string {
	return cacheFilePath("PLUGIN_OVERRIDES_DB_PATH", "data/plugin_overrides.sqlite")
}

// openHelmcentralDB opens (creating if necessary) a connection to the one
// business database, configured identically no matter which of the six
// stores (documentStore, assistantStore, nearbyContactStore, alarmLogStore,
// webPushSubscriptionStore, pluginOverridesStore) is opening it: foreign
// keys enforced, a 30-second busy timeout so one store's write never
// surfaces to another as a bare "database is locked", and WAL with
// synchronous(NORMAL) so a long document write never blocks a concurrent
// read from Mate's conversation history or the AIS sighting log - readers
// under WAL don't wait on a writer the way the default rollback journal
// does (ADR 0141; nearby_contacts.go's newNearbyContactStore carried this
// exact WAL/synchronous(NORMAL) reasoning alone before the six stores
// shared a file).
//
// Every caller keeps its own *sql.DB from its own call to this function,
// each capped at one connection (SetMaxOpenConns(1)): modernc/sqlite
// surfaces a second concurrent writer as "database is locked" rather than
// queuing it, so capping each store's own pool at one connection makes
// database/sql do that queuing for that store's own writes, and it is WAL
// that then lets a *different* store's one-connection pool read the same
// file without waiting on it at all.
//
// Both pragmas that matter for correctness are read back and verified
// rather than trusted - modernc's own _pragma DSN parameters are otherwise
// silently unverified, the same reasoning newDocumentStore has always
// applied to foreign_keys alone.
//
// _txlock=immediate (a modernc/sqlite DSN parameter, not a _pragma - see
// sqlite.go's own doc comment on Driver.Open) makes every db.Begin() issue
// "BEGIN IMMEDIATE" instead of the default "BEGIN" (deferred): a deferred
// transaction takes no lock and no read snapshot until its first statement
// runs, and a multi-statement transaction that reads before it writes (e.g.
// AppendMessage, assistant_store.go: SELECT the conversation and its max
// seq, then INSERT the message) takes that snapshot at the SELECT. If any
// other connection commits a change before this transaction's later write
// runs, the write's attempt to upgrade a now-stale snapshot into a writer
// fails immediately with SQLITE_BUSY_SNAPSHOT - not something busy_timeout
// retries help with, since the problem isn't a lock to wait out, it's a
// snapshot that can never become valid by waiting. BEGIN IMMEDIATE closes
// that window by taking the write lock at BEGIN, before any read in the
// transaction runs: either it acquires the lock right away, or - if another
// connection already holds it - it blocks and retries exactly the way
// busy_timeout is meant to cover, and only then does the transaction's own
// reads and writes proceed against a snapshot nothing else can invalidate
// out from under it.
func openHelmcentralDB(path string) (*sql.DB, error) {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create helmcentral database directory: %w", err)
		}
	}

	dsn := path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open helmcentral database: %w", err)
	}
	db.SetMaxOpenConns(1)

	var fk int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		db.Close()
		return nil, fmt.Errorf("check foreign_keys pragma: %w", err)
	}
	if fk != 1 {
		db.Close()
		return nil, fmt.Errorf("foreign_keys pragma did not take effect (got %d, want 1)", fk)
	}

	var journalMode string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		db.Close()
		return nil, fmt.Errorf("check journal_mode pragma: %w", err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		db.Close()
		return nil, fmt.Errorf("journal_mode pragma did not take effect (got %q, want %q)", journalMode, "wal")
	}

	return db, nil
}

// checkForLegacyDatabaseFiles is the fail-fast startup guard (ADR 0141): if
// any of the six files helmcentral.sqlite replaces still exist at their own
// resolved paths, main() refuses to start rather than either silently
// ignoring years of document/conversation/sighting/alarm/device/plugin-config
// history (if it opened helmcentralPath and carried on) or starting against
// an empty combined file while the real data sits untouched at its old path.
// Migration is an operator-run, one-off step (migrate-db, runMigrateDBCommand
// below) precisely because it renames and copies data - never automatic.
//
// The six paths are passed in - exactly what legacyDocumentsDBPath through
// legacyPluginOverridesDBPath (above) resolve to, which is what
// runMigrateDBCommand itself uses - rather than derived here from
// helmcentralPath's own directory. DOCUMENTS_DB_PATH/ASSISTANT_DB_PATH/
// NEARBY_CONTACTS_DB_PATH/ALARM_LOG_DB/WEBPUSH_DB_PATH/
// PLUGIN_OVERRIDES_DB_PATH can each point anywhere; an install that had set
// one of them to a non-default location would pass a guard that only
// checked default filenames beside helmcentralPath, and boot straight into
// an empty combined database while its real legacy file sat untouched
// wherever that variable actually pointed it.
func checkForLegacyDatabaseFiles(legacyDocsPath, legacyAssistantPath, legacyNearbyPath, legacyAlarmLogPath, legacyWebPushPath, legacyPluginOverridesPath string) error {
	var found []string
	for _, p := range []string{legacyDocsPath, legacyAssistantPath, legacyNearbyPath, legacyAlarmLogPath, legacyWebPushPath, legacyPluginOverridesPath} {
		if _, err := os.Stat(p); err == nil {
			found = append(found, p)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("check for legacy database files: %w", err)
		}
	}
	if len(found) == 0 {
		return nil
	}
	return fmt.Errorf(
		"found pre-upgrade database file(s) that documents/assistant/nearby-contacts/alarm-log/webpush/plugin-overrides used to live in: %s - "+
			"run the one-time migration first, then restart. Docker Compose: this process crash-loops on this "+
			"error, so `docker exec` will not reliably reach it - run "+
			"`docker compose run --rm helmcentral /app/helmcentral migrate-db` against the same volume, then "+
			"`docker compose up -d helmcentral`. Native install: stop the service, run "+
			"`sudo -u helmcentral HELMCENTRAL_STATE_DIR=/var/lib/helmcentral /usr/local/bin/helmcentral migrate-db`, "+
			"then start it again",
		strings.Join(found, ", "))
}

// migrationTable is one table copied whole, by an explicit column list,
// from a legacy file into the combined database - see copyLegacyTablesTx.
type migrationTable struct {
	name    string
	columns []string
}

// assistantMigrationTables mirrors newAssistantStore's own schema
// (assistant_store.go) exactly: conversations, then messages (which
// reference a conversation only by id, no declared foreign key), then
// message_attachments.
var assistantMigrationTables = []migrationTable{
	{name: "conversations", columns: []string{"id", "title", "created_at", "updated_at"}},
	{name: "messages", columns: []string{
		"id", "conversation_id", "seq", "role", "content", "model",
		"prompt_tokens", "completion_tokens", "cost_usd", "tool_rounds", "created_at",
	}},
	{name: "message_attachments", columns: []string{"message_id", "document_id", "filename", "position"}},
}

// nearbyMigrationTables mirrors newNearbyContactStore's own schema
// (nearby_contacts.go): one table, id included explicitly so a sighting's
// original rowid survives the move.
var nearbyMigrationTables = []migrationTable{
	{name: "nearby_vessel_contacts", columns: []string{
		"id", "vessel_key", "name", "seen_at", "lat", "lon", "geoname", "nav_context",
	}},
}

// alarmLogMigrationTables mirrors createAlarmLogSchema's own schema
// (alarm_log_store.go): alarm_log, then notification_queue including
// rule_id - a column ensureQueueTable ALTER TABLEs onto notification_queue
// lazily on every open (alarm_log_store.go), which a legacy file predating
// that ALTER may not have yet. migrateAttachedLegacyData applies the same
// ALTER directly to the attached legacy file before this copy runs, so the
// explicit column list here is always valid against it.
var alarmLogMigrationTables = []migrationTable{
	{name: "alarm_log", columns: []string{
		"id", "rule_id", "source", "label", "path", "state", "message",
		"value_at_raise", "raised_at", "acked_at", "cleared_at",
	}},
	{name: "notification_queue", columns: []string{
		"id", "transport", "rule_id", "payload", "attempts", "next_attempt_at", "created_at", "last_error",
	}},
}

// webPushMigrationTables mirrors createWebPushSchema's own schema
// (webpush_subscriptions_store.go): one table, id included explicitly so a
// device registration's original id survives the move.
var webPushMigrationTables = []migrationTable{
	{name: "push_subscriptions", columns: []string{
		"id", "endpoint", "p256dh", "auth", "label", "user_agent",
		"vapid_public_key", "created_at", "last_success_at", "last_error",
	}},
}

// pluginOverridesMigrationTables mirrors createPluginOverridesSchema's own
// schema (plugin_overrides_store.go): plugin_overrides, then
// plugin_config_values - a table added to this store after plugin_overrides
// itself shipped, which a legacy file that predates it lacks entirely.
// migrateAttachedLegacyData creates it on the attached legacy file (empty,
// if missing) before this copy runs, so the explicit column list here is
// always valid against it.
var pluginOverridesMigrationTables = []migrationTable{
	{name: "plugin_overrides", columns: []string{
		"wasm_path", "allowed_hosts", "allowed_secrets", "updated_at",
	}},
	{name: "plugin_config_values", columns: []string{
		"wasm_path", "key", "value", "updated_at",
	}},
}

// migrateDBResult is migrateToHelmcentralDB's report: what actually moved,
// for both the printed summary and this package's own tests to assert
// against directly rather than scraping stdout.
type migrateDBResult struct {
	// DocumentsRenamed is true when a legacy documents.sqlite existed and was
	// renamed forward into helmcentralPath (migrateWithDocumentsFile), false
	// when no documents.sqlite ever existed and helmcentralPath was created
	// fresh with just the documents schema instead (migrateWithoutDocumentsFile).
	DocumentsRenamed         bool
	AssistantMigrated        bool
	NearbyMigrated           bool
	AlarmLogMigrated         bool
	WebPushMigrated          bool
	PluginOverridesMigrated  bool
	AssistantRowCounts       map[string]int
	NearbyRowCounts          map[string]int
	AlarmLogRowCounts        map[string]int
	WebPushRowCounts         map[string]int
	PluginOverridesRowCounts map[string]int
}

// migrateToHelmcentralDB is the operator-run, one-off migration behind the
// `migrate-db` subcommand (runMigrateDBCommand). It takes every path
// explicitly rather than resolving them itself, so it is directly testable
// against temp-dir fixtures with no environment-variable juggling.
//
// Idempotence is enforced by refusing outright rather than by skipping
// silently (ADR 0141): a second run against an already-migrated install
// refuses at the first check because helmcentralPath already exists, never
// tries to re-copy anything, and never deletes what it doesn't touch.
//
// documents.sqlite is not required to exist. An install old enough to
// predate the document store (ADR 0106) can have any of the other five
// legacy files - Mate's conversations, the AIS sighting log, the alarm log,
// registered web push devices, plugin overrides - with no documents.sqlite
// ever having been created. Refusing outright whenever documents.sqlite was
// missing, regardless of what else was present, left that install unable
// to either start (checkForLegacyDatabaseFiles) or migrate: this function
// instead only refuses when NONE of the six legacy files exist at all
// (migrateWithDocumentsFile is used when documents.sqlite is present,
// migrateWithoutDocumentsFile otherwise).
func migrateToHelmcentralDB(helmcentralPath, legacyDocsPath, legacyAssistantPath, legacyNearbyPath, legacyAlarmLogPath, legacyWebPushPath, legacyPluginOverridesPath string) (migrateDBResult, error) {
	var result migrateDBResult

	if _, err := os.Stat(helmcentralPath); err == nil {
		return result, fmt.Errorf("%s already exists - migration already ran, or this install never had a documents.sqlite to migrate from; refusing to overwrite it", helmcentralPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, fmt.Errorf("check %s: %w", helmcentralPath, err)
	}

	docsExist, err := fileExists(legacyDocsPath)
	if err != nil {
		return result, fmt.Errorf("check %s: %w", legacyDocsPath, err)
	}

	var migrated migrateDBResult
	if docsExist {
		migrated, err = migrateWithDocumentsFile(helmcentralPath, legacyDocsPath, legacyAssistantPath, legacyNearbyPath, legacyAlarmLogPath, legacyWebPushPath, legacyPluginOverridesPath)
	} else {
		otherLegacyPaths := []string{legacyAssistantPath, legacyNearbyPath, legacyAlarmLogPath, legacyWebPushPath, legacyPluginOverridesPath}
		var anyOtherExists bool
		for _, p := range otherLegacyPaths {
			exists, existsErr := fileExists(p)
			if existsErr != nil {
				return result, fmt.Errorf("check %s: %w", p, existsErr)
			}
			if exists {
				anyOtherExists = true
				break
			}
		}
		if !anyOtherExists {
			return result, fmt.Errorf("no legacy database file exists at %s, %s, %s, %s, %s or %s - a fresh install has nothing to migrate; only run this against an existing data directory",
				legacyDocsPath, legacyAssistantPath, legacyNearbyPath, legacyAlarmLogPath, legacyWebPushPath, legacyPluginOverridesPath)
		}
		migrated, err = migrateWithoutDocumentsFile(helmcentralPath, legacyAssistantPath, legacyNearbyPath, legacyAlarmLogPath, legacyWebPushPath, legacyPluginOverridesPath)
	}
	if err != nil {
		return migrated, err
	}
	migrated.DocumentsRenamed = docsExist

	// Only reached after migrateAttachedLegacyData's own transaction has
	// committed, so a legacy file is renamed to .migrated if and only if
	// its rows are durably in helmcentral.sqlite - never before, and never
	// on a run that failed and got rolled back (or, when documents.sqlite
	// never existed, removed) above. Never deleted outright, so an operator
	// who wants to double-check the numbers still has the original file.
	//
	// A failure here cannot be rolled back (the rows are committed) and
	// leaves both startup and a re-run refusing, so the error names the
	// one manual step that finishes the job.
	if migrated.AssistantMigrated {
		if err := renameLegacyToMigrated(legacyAssistantPath); err != nil {
			return migrated, manualMigratedRenameError(legacyAssistantPath, helmcentralPath, err)
		}
	}
	if migrated.NearbyMigrated {
		if err := renameLegacyToMigrated(legacyNearbyPath); err != nil {
			return migrated, manualMigratedRenameError(legacyNearbyPath, helmcentralPath, err)
		}
	}
	if migrated.AlarmLogMigrated {
		if err := renameLegacyToMigrated(legacyAlarmLogPath); err != nil {
			return migrated, manualMigratedRenameError(legacyAlarmLogPath, helmcentralPath, err)
		}
	}
	if migrated.WebPushMigrated {
		if err := renameLegacyToMigrated(legacyWebPushPath); err != nil {
			return migrated, manualMigratedRenameError(legacyWebPushPath, helmcentralPath, err)
		}
	}
	if migrated.PluginOverridesMigrated {
		if err := renameLegacyToMigrated(legacyPluginOverridesPath); err != nil {
			return migrated, manualMigratedRenameError(legacyPluginOverridesPath, helmcentralPath, err)
		}
	}

	return migrated, nil
}

// fileExists reports whether path exists, distinguishing "does not exist"
// (false, nil) from a real stat error the caller has to surface rather than
// silently treat as absent.
func fileExists(path string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return true, nil
	} else if errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else {
		return false, err
	}
}

// migrateWithDocumentsFile is migrateToHelmcentralDB's path when a legacy
// documents.sqlite exists: the hot-journal check, the forward rename, and
// the ATTACH-and-copy of the other five legacy files, with a failure at any
// point renaming helmcentralPath straight back to documents.sqlite so a
// failed run leaves the install exactly as it found it.
func migrateWithDocumentsFile(helmcentralPath, legacyDocsPath, legacyAssistantPath, legacyNearbyPath, legacyAlarmLogPath, legacyWebPushPath, legacyPluginOverridesPath string) (migrateDBResult, error) {
	var result migrateDBResult

	// A hot rollback journal means the last process to hold documents.sqlite
	// open did not shut down cleanly. Renaming it forward under the new name
	// would make the very next open (openHelmcentralDB, a few lines down) run
	// SQLite's own crash recovery against it as a side effect of this
	// migration, silently taking on whatever that journal was mid-way
	// through. The fail-fast choice is to stop and have the operator shut
	// Helmcentral down cleanly first - confirming the journal clears on its
	// own - rather than have a one-off rename decide to gamble on recovery.
	if _, err := os.Stat(legacyDocsPath + "-journal"); err == nil {
		return result, fmt.Errorf("%s-journal exists - documents.sqlite was not shut down cleanly; stop Helmcentral fully, confirm the -journal file clears, then retry", legacyDocsPath)
	}

	if err := renameDocumentsFileForward(legacyDocsPath, helmcentralPath); err != nil {
		return result, err
	}

	// From here on the rename above has already happened: helmcentralPath
	// exists and legacyDocsPath doesn't. Any failure from this point has to
	// undo that, or the install is stuck - a second run would refuse at the
	// very first check in migrateToHelmcentralDB (helmcentralPath already
	// exists) with no documents.sqlite left to migrate from, and there would
	// be no way to retry short of an operator manually renaming the file
	// back by hand.
	migrated, err := migrateAttachedLegacyData(helmcentralPath, legacyAssistantPath, legacyNearbyPath, legacyAlarmLogPath, legacyWebPushPath, legacyPluginOverridesPath)
	if err != nil {
		if restoreErr := renameDocumentsFileBack(helmcentralPath, legacyDocsPath); restoreErr != nil {
			return migrated, fmt.Errorf("%w (additionally, restoring %s failed: %v - the data directory needs manual repair)", err, legacyDocsPath, restoreErr)
		}
		return migrated, fmt.Errorf("%w - restored %s to its original name; fix the problem above and re-run migrate-db", err, legacyDocsPath)
	}

	return migrated, nil
}

// migrateWithoutDocumentsFile is migrateToHelmcentralDB's path for an install
// old enough to predate the document store (ADR 0106): no documents.sqlite
// ever existed, but at least one of the other five legacy files does. There
// is nothing to rename forward, so helmcentralPath is created fresh through
// openHelmcentralDB and given the documents schema directly
// (createDocumentsSchema, documents_store.go - the same schema
// newDocumentStore applies on an ordinary open), so the combined database is
// complete even though no legacy documents.sqlite ever fed it a row. The
// rest - creating the other five stores' tables, ATTACHing whichever of
// their legacy files exist, and copying every row in one transaction - is
// the same migrateAttachedLegacyData both paths share.
//
// On any failure the file this created is removed outright (removeHelmcentralFile),
// sidecars included, rather than renamed back the way migrateWithDocumentsFile
// undoes its forward rename - there is no legacy documents.sqlite to rename
// it back to. Removing it is what makes a rerun safe: the very first check
// in migrateToHelmcentralDB refuses whenever helmcentralPath already exists,
// so a half-created file left behind would strand the install exactly the
// way a half-migrated one would.
func migrateWithoutDocumentsFile(helmcentralPath, legacyAssistantPath, legacyNearbyPath, legacyAlarmLogPath, legacyWebPushPath, legacyPluginOverridesPath string) (migrateDBResult, error) {
	var result migrateDBResult

	db, err := openHelmcentralDB(helmcentralPath)
	if err != nil {
		return result, fmt.Errorf("create %s: %w", helmcentralPath, err)
	}
	if err := createDocumentsSchema(db); err != nil {
		db.Close()
		if removeErr := removeHelmcentralFile(helmcentralPath); removeErr != nil {
			return result, fmt.Errorf("create documents schema in %s: %w (additionally, removing it failed: %v - the data directory needs manual repair)", helmcentralPath, err, removeErr)
		}
		return result, fmt.Errorf("create documents schema in %s: %w - removed the incomplete file; fix the problem above and re-run migrate-db", helmcentralPath, err)
	}
	db.Close()

	migrated, err := migrateAttachedLegacyData(helmcentralPath, legacyAssistantPath, legacyNearbyPath, legacyAlarmLogPath, legacyWebPushPath, legacyPluginOverridesPath)
	if err != nil {
		if removeErr := removeHelmcentralFile(helmcentralPath); removeErr != nil {
			return migrated, fmt.Errorf("%w (additionally, removing %s failed: %v - the data directory needs manual repair)", err, helmcentralPath, removeErr)
		}
		return migrated, fmt.Errorf("%w - removed the incomplete %s; fix the problem above and re-run migrate-db", err, helmcentralPath)
	}

	return migrated, nil
}

// removeHelmcentralFile removes helmcentralPath and its -wal/-shm sidecars,
// if present. migrateWithoutDocumentsFile's own failure cleanup: there is no
// legacy documents.sqlite to rename back to the way renameDocumentsFileBack
// undoes migrateWithDocumentsFile's forward rename, so undoing here means
// removing the file this run created instead.
func removeHelmcentralFile(path string) error {
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", p, err)
		}
	}
	return nil
}

// renameDocumentsFileForward renames documents.sqlite (and its -wal/-shm
// siblings, if present) to helmcentral.sqlite - the first, and only
// irreversible-without-help, step of the migration. Paired with
// renameDocumentsFileBack, which undoes exactly this.
//
// A failure partway (the main file moved, a sidecar did not) undoes only the
// moves this call made, in reverse, so documents.sqlite and its sidecars are
// never left split across two names. It does not call
// renameDocumentsFileBack, which would also carry back anything that
// happened to already sit at a helmcentral.sqlite-* name.
func renameDocumentsFileForward(legacyDocsPath, helmcentralPath string) error {
	type move struct{ from, to string }
	var done []move
	fail := func(err error) error {
		for i := len(done) - 1; i >= 0; i-- {
			if undoErr := os.Rename(done[i].to, done[i].from); undoErr != nil {
				return fmt.Errorf("%w (additionally, moving %s back to %s failed: %v - the data directory needs manual repair)", err, done[i].to, done[i].from, undoErr)
			}
		}
		return err
	}

	if err := os.Rename(legacyDocsPath, helmcentralPath); err != nil {
		return fmt.Errorf("rename %s to %s: %w", legacyDocsPath, helmcentralPath, err)
	}
	done = append(done, move{legacyDocsPath, helmcentralPath})
	for _, suffix := range []string{"-wal", "-shm"} {
		src := legacyDocsPath + suffix
		if _, err := os.Stat(src); err == nil {
			if err := os.Rename(src, helmcentralPath+suffix); err != nil {
				return fail(fmt.Errorf("rename %s to %s: %w", src, helmcentralPath+suffix, err))
			}
			done = append(done, move{src, helmcentralPath + suffix})
		} else if !errors.Is(err, os.ErrNotExist) {
			return fail(fmt.Errorf("check %s: %w", src, err))
		}
	}
	return nil
}

// manualMigratedRenameError explains a .migrated rename that failed after
// the copy committed: the data is safe, and the operator finishes the
// migration by moving the old file aside by hand.
func manualMigratedRenameError(legacyPath, helmcentralPath string, err error) error {
	return fmt.Errorf("%w - its data was already safely copied into %s; finish by renaming %s to %s by hand (and any %s-wal/-shm beside it), then start Helmcentral. Do not re-run migrate-db",
		err, helmcentralPath, legacyPath, legacyPath+".migrated", legacyPath)
}

// renameDocumentsFileBack undoes renameDocumentsFileForward, on any failure
// migrateAttachedLegacyData returns: renames helmcentral.sqlite (and any
// -wal/-shm it now carries) back to documents.sqlite, so a failed migration
// leaves the install exactly as migrate-db found it, ready to fix the
// underlying problem and re-run the same command. openHelmcentralDB having
// already converted the file to WAL, and the (still-empty, since nothing
// committed) assistant/nearby-contact tables it created inside it along the
// way, are both harmless to hand back under the old name: the pre-0141
// binary that opens documents.sqlite next never asked for WAL and has no
// idea those tables exist.
func renameDocumentsFileBack(helmcentralPath, legacyDocsPath string) error {
	if err := os.Rename(helmcentralPath, legacyDocsPath); err != nil {
		return fmt.Errorf("rename %s back to %s: %w", helmcentralPath, legacyDocsPath, err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		src := helmcentralPath + suffix
		if _, err := os.Stat(src); err == nil {
			if err := os.Rename(src, legacyDocsPath+suffix); err != nil {
				return fmt.Errorf("rename %s back to %s: %w", src, legacyDocsPath+suffix, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("check %s: %w", src, err)
		}
	}
	return nil
}

// migrateAttachedLegacyData opens helmcentralPath (already renamed forward
// from documents.sqlite by the caller), creates every other store's tables
// in it, and copies every row from whichever of legacyAssistantPath/
// legacyNearbyPath/legacyAlarmLogPath/legacyWebPushPath/
// legacyPluginOverridesPath exist - all within ONE transaction, so a
// failure copying any one file rolls back every other one too rather than
// leaving the combined database committed with some migrated and others
// missing. The caller (migrateToHelmcentralDB) is responsible for undoing
// the forward rename on any error this returns, and for only renaming a
// legacy file to .migrated once this has returned success.
func migrateAttachedLegacyData(helmcentralPath, legacyAssistantPath, legacyNearbyPath, legacyAlarmLogPath, legacyWebPushPath, legacyPluginOverridesPath string) (migrateDBResult, error) {
	var result migrateDBResult

	db, err := openHelmcentralDB(helmcentralPath)
	if err != nil {
		return result, fmt.Errorf("open %s: %w", helmcentralPath, err)
	}
	defer db.Close()

	if err := createAssistantSchema(db); err != nil {
		return result, fmt.Errorf("create assistant tables: %w", err)
	}
	if err := createNearbyContactSchema(db); err != nil {
		return result, fmt.Errorf("create nearby-contact tables: %w", err)
	}
	if err := createAlarmLogSchema(db); err != nil {
		return result, fmt.Errorf("create alarm log tables: %w", err)
	}
	if err := createWebPushSchema(db); err != nil {
		return result, fmt.Errorf("create web push tables: %w", err)
	}
	if err := createPluginOverridesSchema(db); err != nil {
		return result, fmt.Errorf("create plugin overrides tables: %w", err)
	}

	// Attach whichever legacy files exist before opening the transaction
	// that copies from them, so a file that was never there (an install
	// that never used Mate has no assistant.sqlite, one that never logged
	// an AIS contact has no nearby-contacts.sqlite, and so on for the
	// other three) is simply skipped rather than treated as an error.
	assistantPresent, err := attachLegacyIfPresent(db, legacyAssistantPath, "legacy_assistant")
	if err != nil {
		return result, err
	}
	if assistantPresent {
		defer detachLegacy(db, "legacy_assistant")
	} else {
		log.Printf("migrate-db: %s not found - nothing to migrate for it", legacyAssistantPath)
	}

	nearbyPresent, err := attachLegacyIfPresent(db, legacyNearbyPath, "legacy_nearby")
	if err != nil {
		return result, err
	}
	if nearbyPresent {
		defer detachLegacy(db, "legacy_nearby")
	} else {
		log.Printf("migrate-db: %s not found - nothing to migrate for it", legacyNearbyPath)
	}

	alarmLogPresent, err := attachLegacyIfPresent(db, legacyAlarmLogPath, "legacy_alarm_log")
	if err != nil {
		return result, err
	}
	if alarmLogPresent {
		defer detachLegacy(db, "legacy_alarm_log")

		// A legacy alarm-log.sqlite from before notification_queue.rule_id
		// existed (ensureQueueTable's own lazy ALTER TABLE,
		// alarm_log_store.go) won't have the column: the live store patches
		// it on every open, but ATTACHing the raw file bypasses that
		// entirely. Apply the identical ALTER directly to the attached
		// legacy file before the explicit-column-list copy below, which
		// would otherwise fail with "no such column: rule_id" against an
		// old-enough file. Idempotent, same "duplicate column name" swallow
		// ensureQueueTable itself uses, for a file already on the current
		// schema.
		if _, err := db.Exec(`ALTER TABLE legacy_alarm_log.notification_queue ADD COLUMN rule_id TEXT NOT NULL DEFAULT ''`); err != nil &&
			!strings.Contains(err.Error(), "duplicate column name") {
			return result, fmt.Errorf("add rule_id column to %s's notification_queue: %w", legacyAlarmLogPath, err)
		}
	} else {
		log.Printf("migrate-db: %s not found - nothing to migrate for it", legacyAlarmLogPath)
	}

	webPushPresent, err := attachLegacyIfPresent(db, legacyWebPushPath, "legacy_webpush")
	if err != nil {
		return result, err
	}
	if webPushPresent {
		defer detachLegacy(db, "legacy_webpush")
	} else {
		log.Printf("migrate-db: %s not found - nothing to migrate for it", legacyWebPushPath)
	}

	pluginOverridesPresent, err := attachLegacyIfPresent(db, legacyPluginOverridesPath, "legacy_plugin_overrides")
	if err != nil {
		return result, err
	}
	if pluginOverridesPresent {
		defer detachLegacy(db, "legacy_plugin_overrides")

		// plugin_config_values was added to this store after plugin_overrides
		// itself shipped (plugin_overrides_store.go): a legacy file that
		// predates it has plugin_overrides but no plugin_config_values table
		// at all. Create it directly on the attached legacy file - identical
		// DDL to newPluginOverridesStore's own, CREATE TABLE IF NOT EXISTS,
		// so a file that already has it is untouched - before the copy
		// below, which would otherwise fail with "no such table" against an
		// old-enough file.
		if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS legacy_plugin_overrides.plugin_config_values (
			wasm_path  TEXT NOT NULL,
			key        TEXT NOT NULL,
			value      TEXT NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (wasm_path, key)
		)`); err != nil {
			return result, fmt.Errorf("create plugin_config_values table on %s: %w", legacyPluginOverridesPath, err)
		}
	} else {
		log.Printf("migrate-db: %s not found - nothing to migrate for it", legacyPluginOverridesPath)
	}

	if !assistantPresent && !nearbyPresent && !alarmLogPresent && !webPushPresent && !pluginOverridesPresent {
		return result, nil
	}

	tx, err := db.Begin()
	if err != nil {
		return result, fmt.Errorf("begin migration transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()

	var assistantCounts, nearbyCounts, alarmLogCounts, webPushCounts, pluginOverridesCounts map[string]int
	if assistantPresent {
		assistantCounts, err = copyLegacyTablesTx(tx, "legacy_assistant", assistantMigrationTables)
		if err != nil {
			return result, fmt.Errorf("migrate %s: %w", legacyAssistantPath, err)
		}
	}
	if nearbyPresent {
		nearbyCounts, err = copyLegacyTablesTx(tx, "legacy_nearby", nearbyMigrationTables)
		if err != nil {
			return result, fmt.Errorf("migrate %s: %w", legacyNearbyPath, err)
		}
	}
	if alarmLogPresent {
		alarmLogCounts, err = copyLegacyTablesTx(tx, "legacy_alarm_log", alarmLogMigrationTables)
		if err != nil {
			return result, fmt.Errorf("migrate %s: %w", legacyAlarmLogPath, err)
		}
	}
	if webPushPresent {
		webPushCounts, err = copyLegacyTablesTx(tx, "legacy_webpush", webPushMigrationTables)
		if err != nil {
			return result, fmt.Errorf("migrate %s: %w", legacyWebPushPath, err)
		}
	}
	if pluginOverridesPresent {
		pluginOverridesCounts, err = copyLegacyTablesTx(tx, "legacy_plugin_overrides", pluginOverridesMigrationTables)
		if err != nil {
			return result, fmt.Errorf("migrate %s: %w", legacyPluginOverridesPath, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit migration: %w", err)
	}
	committed = true

	// Set only now that the transaction has actually committed - not
	// alongside each copy above, while it was still one failure in another
	// table away from being rolled back.
	result.AssistantMigrated = assistantPresent
	result.AssistantRowCounts = assistantCounts
	result.NearbyMigrated = nearbyPresent
	result.NearbyRowCounts = nearbyCounts
	result.AlarmLogMigrated = alarmLogPresent
	result.AlarmLogRowCounts = alarmLogCounts
	result.WebPushMigrated = webPushPresent
	result.WebPushRowCounts = webPushCounts
	result.PluginOverridesMigrated = pluginOverridesPresent
	result.PluginOverridesRowCounts = pluginOverridesCounts

	return result, nil
}

// attachLegacyIfPresent ATTACHes legacyPath under alias if it exists, and
// reports whether it did; a legacy file that does not exist at all is not
// an error (see migrateAttachedLegacyData's own doc comment).
func attachLegacyIfPresent(db *sql.DB, legacyPath, alias string) (bool, error) {
	if _, err := os.Stat(legacyPath); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("check %s: %w", legacyPath, err)
	}

	if _, err := db.Exec(fmt.Sprintf(`ATTACH DATABASE ? AS %s`, alias), legacyPath); err != nil {
		return false, fmt.Errorf("attach %s: %w", legacyPath, err)
	}
	return true, nil
}

// detachLegacy detaches alias, logging rather than failing the migration on
// error: by the time this runs (deferred, in migrateAttachedLegacyData) the
// transaction touching alias has already committed or been rolled back, so
// there is nothing left for a detach failure to protect - it only matters
// as a courtesy to the caller, which renames this exact file to .migrated
// (or, on the restore path, leaves it untouched) right after.
func detachLegacy(db *sql.DB, alias string) {
	if _, err := db.Exec(fmt.Sprintf(`DETACH DATABASE %s`, alias)); err != nil {
		log.Printf("migrate-db: detach %s: %v", alias, err)
	}
}

// copyLegacyTablesTx copies every row of each named table, by an explicit
// column list, from alias into the same-named table in main. Part of the
// single transaction migrateAttachedLegacyData runs across both legacy
// files: a row-count mismatch on any table, from either file, returns an
// error that leaves tx uncommitted, so the caller's deferred rollback
// undoes every table this migration touched, not just this one file's own.
func copyLegacyTablesTx(tx *sql.Tx, alias string, tables []migrationTable) (map[string]int, error) {
	counts := make(map[string]int)
	for _, table := range tables {
		cols := strings.Join(table.columns, ", ")
		insertSQL := fmt.Sprintf(`INSERT INTO main.%s (%s) SELECT %s FROM %s.%s`, table.name, cols, cols, alias, table.name)
		if _, err := tx.Exec(insertSQL); err != nil {
			return nil, fmt.Errorf("copy %s.%s: %w", alias, table.name, err)
		}

		var legacyCount, mainCount int
		if err := tx.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.%s`, alias, table.name)).Scan(&legacyCount); err != nil {
			return nil, fmt.Errorf("count %s.%s: %w", alias, table.name, err)
		}
		if err := tx.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM main.%s`, table.name)).Scan(&mainCount); err != nil {
			return nil, fmt.Errorf("count main.%s: %w", table.name, err)
		}
		if mainCount != legacyCount {
			return nil, fmt.Errorf("row count mismatch copying %s.%s: legacy has %d, main has %d after copy", alias, table.name, legacyCount, mainCount)
		}
		counts[table.name] = mainCount
	}
	return counts, nil
}

// renameLegacyToMigrated renames a fully-migrated legacy file (assistant.sqlite,
// nearby-contacts.sqlite, alarm-log.sqlite, webpush-subscriptions.sqlite, or
// plugin_overrides.sqlite) to "<path>.migrated" - never deleted outright,
// so an operator who wants to double-check the numbers still has the
// original file - and, if either sidecar is present, carries
// "<path>-wal"/"<path>-shm" along to "<path>-wal.migrated"/"<path>-shm.migrated"
// too, so nothing stale under the old name is left beside the renamed file.
// Callers detach path from the migration's db connection before calling
// this (migrateAttachedLegacyData's deferred detachLegacy calls, which run
// before migrateToHelmcentralDB proceeds to this rename).
func renameLegacyToMigrated(path string) error {
	if err := os.Rename(path, path+".migrated"); err != nil {
		return fmt.Errorf("rename %s to .migrated: %w", path, err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		src := path + suffix
		if _, err := os.Stat(src); err == nil {
			if err := os.Rename(src, src+".migrated"); err != nil {
				return fmt.Errorf("rename %s to .migrated: %w", src, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("check %s: %w", src, err)
		}
	}
	return nil
}

// runMigrateDBCommand is the `migrate-db` subcommand's entry point (main.go
// checks os.Args[1] for it before doing anything else). It resolves every
// path itself - the real HELMCENTRAL_DB_PATH plus the six legacy
// DOCUMENTS_DB_PATH/ASSISTANT_DB_PATH/NEARBY_CONTACTS_DB_PATH/ALARM_LOG_DB/
// WEBPUSH_DB_PATH/PLUGIN_OVERRIDES_DB_PATH env vars, for this one-off purpose
// only - and returns the process exit code, printing a summary to stdout on
// success or the refusal reason to stderr on failure.
func runMigrateDBCommand() int {
	newPath := helmcentralDBPath()
	result, err := migrateToHelmcentralDB(newPath, legacyDocumentsDBPath(), legacyAssistantDBPath(), legacyNearbyContactsDBPath(),
		legacyAlarmLogDBPath(), legacyWebPushDBPath(), legacyPluginOverridesDBPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "migrate-db: %v\n", err)
		return 1
	}

	fmt.Printf("migrate-db: %s is ready, in WAL mode\n", newPath)
	if result.DocumentsRenamed {
		fmt.Println("migrate-db: documents.sqlite renamed into the combined database")
	} else {
		fmt.Println("migrate-db: no documents.sqlite found - created a fresh, empty document library in the combined database")
	}
	if result.AssistantMigrated {
		fmt.Printf("migrate-db: assistant.sqlite migrated - %s\n", formatRowCounts(assistantMigrationTables, result.AssistantRowCounts))
	} else {
		fmt.Println("migrate-db: no assistant.sqlite found - nothing to migrate for Mate's conversation history")
	}
	if result.NearbyMigrated {
		fmt.Printf("migrate-db: nearby-contacts.sqlite migrated - %s\n", formatRowCounts(nearbyMigrationTables, result.NearbyRowCounts))
	} else {
		fmt.Println("migrate-db: no nearby-contacts.sqlite found - nothing to migrate for the AIS sighting log")
	}
	if result.AlarmLogMigrated {
		fmt.Printf("migrate-db: alarm-log.sqlite migrated - %s\n", formatRowCounts(alarmLogMigrationTables, result.AlarmLogRowCounts))
	} else {
		fmt.Println("migrate-db: no alarm-log.sqlite found - nothing to migrate for the alarm log and notification queue")
	}
	if result.WebPushMigrated {
		fmt.Printf("migrate-db: webpush-subscriptions.sqlite migrated - %s\n", formatRowCounts(webPushMigrationTables, result.WebPushRowCounts))
	} else {
		fmt.Println("migrate-db: no webpush-subscriptions.sqlite found - nothing to migrate for registered push devices")
	}
	if result.PluginOverridesMigrated {
		fmt.Printf("migrate-db: plugin_overrides.sqlite migrated - %s\n", formatRowCounts(pluginOverridesMigrationTables, result.PluginOverridesRowCounts))
	} else {
		fmt.Println("migrate-db: no plugin_overrides.sqlite found - nothing to migrate for plugin overrides and config values")
	}
	return 0
}

// formatRowCounts renders counts in tables' own order (map iteration order
// is not stable, and the print output should read the same way every run).
func formatRowCounts(tables []migrationTable, counts map[string]int) string {
	parts := make([]string, 0, len(tables))
	for _, table := range tables {
		parts = append(parts, fmt.Sprintf("%s: %d", table.name, counts[table.name]))
	}
	return strings.Join(parts, ", ")
}
