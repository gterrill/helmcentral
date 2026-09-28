package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── openHelmcentralDB ────────────────────────────────────────────────────

func TestOpenHelmcentralDB_EnablesForeignKeysAndWAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "helmcentral.sqlite")

	db, err := openHelmcentralDB(path)
	if err != nil {
		t.Fatalf("openHelmcentralDB: %v", err)
	}
	defer db.Close()

	var fk int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Fatalf("expected foreign_keys pragma to be 1, got %d", fk)
	}

	var journalMode string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("expected journal_mode pragma to be %q, got %q", "wal", journalMode)
	}
}

func TestOpenHelmcentralDB_CreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "helmcentral.sqlite")

	db, err := openHelmcentralDB(path)
	if err != nil {
		t.Fatalf("openHelmcentralDB: %v", err)
	}
	defer db.Close()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected database file to exist at %q: %v", path, err)
	}
}

// TestOpenHelmcentralDB_ReadThenWriteTransactionSurvivesInterveningWrite pins
// a review finding: three stores each hold their own connection to
// helmcentral.sqlite, and a deferred transaction that reads first and writes
// later (assistant_store.go's AppendMessage, ~line 332: SELECT the
// conversation and its max seq, THEN INSERT the message) gets
// SQLITE_BUSY_SNAPSHOT the instant another connection commits in between -
// immediately, bypassing busy_timeout entirely, because a deferred
// transaction's read snapshot going stale is not a "someone else holds the
// lock, wait" situation busy_timeout's retry loop can do anything about.
// nearbyContactStore's every-5-second AIS poll write is exactly the kind of
// intervening commit that triggers this against a live Mate conversation.
//
// This test opens A the real way (openHelmcentralDB, the fix under test) and
// B as a second, independent connection to the same file with its own short
// busy_timeout (chosen only to keep this test fast if the fix is missing -
// see below - not because the value itself matters to what's being tested).
// It is fully sequential, on one goroutine: A begins and reads, B does one
// complete independent write (mirroring the AIS poller's autocommit
// insert), then A attempts its own write. No sleep and no second goroutine
// is needed - every wait here is SQLite's own bounded busy-retry loop, not
// a hand-rolled delay, and the ordering is deterministic because the whole
// script runs on a single goroutine with no scheduling race between steps:
//
//   - Without the fix, A's begin is deferred and holds no lock yet, so B's
//     write lands immediately (nothing to contend with) and commits before
//     A ever attempts to write. A's later write then finds its read
//     snapshot stale and fails immediately with SQLITE_BUSY(_SNAPSHOT) -
//     confirmed empirically before writing this test.
//   - With the fix (_txlock=immediate), A's begin already holds the write
//     lock before this test's SELECT even runs, so B's write instead blocks
//     on that lock, retries per its own busy_timeout, and fails fast (its
//     busy_timeout is deliberately short) - which this test does not assert
//     on, since proving B doesn't wedge forever under contention is not the
//     point here, only that A's own read-then-write transaction is never
//     the one that fails. A's write then succeeds immediately, since it
//     held the lock uncontested the whole time.
func TestOpenHelmcentralDB_ReadThenWriteTransactionSurvivesInterveningWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "helmcentral.sqlite")

	dbA, err := openHelmcentralDB(path)
	if err != nil {
		t.Fatalf("openHelmcentralDB: %v", err)
	}
	defer dbA.Close()

	if _, err := dbA.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := dbA.Exec(`INSERT INTO t (id, v) VALUES (1, 'seed')`); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	// B: a second, independent connection to the same file - what a second
	// store (e.g. nearbyContactStore) would hold. Its own short
	// busy_timeout is there only so this test resolves quickly once the fix
	// makes B's write contend with A's held lock; it is not itself under
	// test.
	dbB, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(200)")
	if err != nil {
		t.Fatalf("open second connection: %v", err)
	}
	defer dbB.Close()
	dbB.SetMaxOpenConns(1)

	// A: begin, then read - the exact shape AppendMessage follows (read the
	// conversation and its max seq before inserting the new message row).
	txA, err := dbA.Begin()
	if err != nil {
		t.Fatalf("begin A: %v", err)
	}
	defer txA.Rollback()

	var v string
	if err := txA.QueryRow(`SELECT v FROM t WHERE id = 1`).Scan(&v); err != nil {
		t.Fatalf("A's read: %v", err)
	}

	// B: one independent, complete write - the AIS poller's autocommit
	// insert landing while A's transaction is still open, mid-flight.
	if _, err := dbB.Exec(`INSERT INTO t (id, v) VALUES (2, 'from B')`); err != nil {
		t.Logf("B's write returned %v (expected once the fix makes B contend with A's held lock; not asserted on)", err)
	}

	// A: now write, within the same transaction it read in. This must
	// succeed either way - it is what the fix is actually about - even
	// though B's independent write landed somewhere in between A's read
	// and this line.
	if _, err := txA.Exec(`INSERT INTO t (id, v) VALUES (3, 'from A')`); err != nil {
		t.Fatalf("expected A's write to survive B's intervening write, got: %v", err)
	}
	if err := txA.Commit(); err != nil {
		t.Fatalf("expected A's commit to succeed, got: %v", err)
	}
}

// ── all six stores sharing one file ─────────────────────────────────────

func TestSixStores_ShareOneFileConcurrently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "helmcentral.sqlite")

	docStore, err := newDocumentStore(path)
	if err != nil {
		t.Fatalf("newDocumentStore: %v", err)
	}
	defer docStore.Close()

	asStore, err := newAssistantStore(path)
	if err != nil {
		t.Fatalf("newAssistantStore: %v", err)
	}
	defer asStore.Close()

	ncStore, err := newNearbyContactStore(path)
	if err != nil {
		t.Fatalf("newNearbyContactStore: %v", err)
	}
	// dwell 0 so the very first tick confirms the encounter immediately,
	// matching newTestNearbyContactStore's own convention
	// (nearby_contacts_test.go).
	ncStore.dwell = 0
	defer ncStore.close()

	alStore, err := newAlarmLogStore(path)
	if err != nil {
		t.Fatalf("newAlarmLogStore: %v", err)
	}
	defer alStore.Close()

	wpStore, err := newWebPushSubscriptionStore(path)
	if err != nil {
		t.Fatalf("newWebPushSubscriptionStore: %v", err)
	}
	defer wpStore.Close()

	poStore, err := newPluginOverridesStore(path)
	if err != nil {
		t.Fatalf("newPluginOverridesStore: %v", err)
	}
	defer poStore.db.Close()

	// Write through each store.
	doc, err := docStore.Insert(document{SHA256: "shared-file-sha", Filename: "manual.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert document: %v", err)
	}

	conv, err := asStore.CreateConversation("shared file test")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}

	now := time.Date(2026, time.September, 28, 10, 0, 0, 0, time.UTC)
	if err := ncStore.recordContactIfNew("mmsi:987654321", "Test Vessel", -33.8, 151.2, "", "underway", now, now); err != nil {
		t.Fatalf("recordContactIfNew: %v", err)
	}

	if _, err := alStore.RecordRaised(alarmLogEntry{RuleID: "rule-shared", Label: "Shared file test", State: alarmStateAlarm, RaisedAt: now}); err != nil {
		t.Fatalf("RecordRaised: %v", err)
	}

	if _, err := wpStore.Upsert(webPushSubscription{Endpoint: "https://push.example/shared-file", P256dh: "k", Auth: "a", VAPIDPublicKey: "key-shared"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	if err := poStore.Set("plugins/tides/shared-file.wasm", []string{"example.com"}, nil); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Read every write back, through the store that wrote it - proving all
	// six actually landed in the one shared file rather than six separate
	// ones that happened to share a name.
	gotDoc, ok, err := docStore.GetBySHA("shared-file-sha")
	if err != nil || !ok {
		t.Fatalf("GetBySHA: ok=%v err=%v", ok, err)
	}
	if gotDoc.ID != doc.ID {
		t.Fatalf("expected document id %q, got %q", doc.ID, gotDoc.ID)
	}

	convs, err := asStore.ListConversations()
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(convs) != 1 || convs[0].ID != conv.ID {
		t.Fatalf("expected exactly the one conversation just created, got %+v", convs)
	}

	if count := countRows(t, ncStore, "mmsi:987654321"); count != 1 {
		t.Fatalf("expected 1 nearby contact row, got %d", count)
	}

	entries, err := alStore.Recent(10)
	if err != nil || len(entries) != 1 || entries[0].RuleID != "rule-shared" {
		t.Fatalf("expected the shared-file alarm log entry, got entries=%+v err=%v", entries, err)
	}

	subs, err := wpStore.All()
	if err != nil || len(subs) != 1 || subs[0].Endpoint != "https://push.example/shared-file" {
		t.Fatalf("expected the shared-file push subscription, got subs=%+v err=%v", subs, err)
	}

	hosts, _, ok, err := poStore.Get("plugins/tides/shared-file.wasm")
	if err != nil || !ok || len(hosts) != 1 || hosts[0] != "example.com" {
		t.Fatalf("expected the shared-file plugin override, got hosts=%v ok=%v err=%v", hosts, ok, err)
	}

	// And confirm it is genuinely one file on disk, not six.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected shared file to exist: %v", err)
	}
}

// ── checkForLegacyDatabaseFiles (startup guard) ─────────────────────────

// legacyGuardTestPaths returns six legacy paths inside a fresh temp dir, at
// their DEFAULT filenames beside where helmcentral.sqlite would go - none
// of them created yet. Most tests use these; the one that exercises a
// legacy path outside this directory builds its own paths instead.
func legacyGuardTestPaths(t *testing.T) (legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides string) {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "documents.sqlite"),
		filepath.Join(dir, "assistant.sqlite"),
		filepath.Join(dir, "nearby-contacts.sqlite"),
		filepath.Join(dir, "alarm-log.sqlite"),
		filepath.Join(dir, "webpush-subscriptions.sqlite"),
		filepath.Join(dir, "plugin_overrides.sqlite")
}

func TestCheckForLegacyDatabaseFiles_NoneFoundReturnsNil(t *testing.T) {
	legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides := legacyGuardTestPaths(t)
	if err := checkForLegacyDatabaseFiles(legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides); err != nil {
		t.Fatalf("expected nil when no legacy files exist, got %v", err)
	}
}

func TestCheckForLegacyDatabaseFiles_DetectsDocumentsSqlite(t *testing.T) {
	legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides := legacyGuardTestPaths(t)
	mustWriteFile(t, legacyDocs, "x")

	err := checkForLegacyDatabaseFiles(legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides)
	if err == nil {
		t.Fatal("expected an error when documents.sqlite is present")
	}
}

func TestCheckForLegacyDatabaseFiles_DetectsAssistantSqlite(t *testing.T) {
	legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides := legacyGuardTestPaths(t)
	mustWriteFile(t, legacyAssistant, "x")

	err := checkForLegacyDatabaseFiles(legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides)
	if err == nil {
		t.Fatal("expected an error when assistant.sqlite is present")
	}
}

func TestCheckForLegacyDatabaseFiles_DetectsNearbyContactsSqlite(t *testing.T) {
	legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides := legacyGuardTestPaths(t)
	mustWriteFile(t, legacyNearby, "x")

	err := checkForLegacyDatabaseFiles(legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides)
	if err == nil {
		t.Fatal("expected an error when nearby-contacts.sqlite is present")
	}
}

func TestCheckForLegacyDatabaseFiles_DetectsAlarmLogSqlite(t *testing.T) {
	legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides := legacyGuardTestPaths(t)
	mustWriteFile(t, legacyAlarmLog, "x")

	err := checkForLegacyDatabaseFiles(legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides)
	if err == nil {
		t.Fatal("expected an error when alarm-log.sqlite is present")
	}
}

func TestCheckForLegacyDatabaseFiles_DetectsWebPushSqlite(t *testing.T) {
	legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides := legacyGuardTestPaths(t)
	mustWriteFile(t, legacyWebPush, "x")

	err := checkForLegacyDatabaseFiles(legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides)
	if err == nil {
		t.Fatal("expected an error when webpush-subscriptions.sqlite is present")
	}
}

func TestCheckForLegacyDatabaseFiles_DetectsPluginOverridesSqlite(t *testing.T) {
	legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides := legacyGuardTestPaths(t)
	mustWriteFile(t, legacyPluginOverrides, "x")

	err := checkForLegacyDatabaseFiles(legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides)
	if err == nil {
		t.Fatal("expected an error when plugin_overrides.sqlite is present")
	}
}

// TestCheckForLegacyDatabaseFiles_DetectsPathOutsideHelmcentralDir pins the
// review finding that the guard used to derive default filenames from
// helmcentralPath's own directory, while migrate-db resolves each legacy
// path independently through DOCUMENTS_DB_PATH/ASSISTANT_DB_PATH/
// NEARBY_CONTACTS_DB_PATH/ALARM_LOG_DB/WEBPUSH_DB_PATH/
// PLUGIN_OVERRIDES_DB_PATH (legacyDocumentsDBPath and friends,
// backend/helmcentral_db.go) - which can each point anywhere. An install
// that had set one of those env vars to a custom location would pass the
// old guard (nothing at the default path beside helmcentral.sqlite) and
// start up against an empty combined file while its real legacy data sat
// untouched at the custom path. Passing the exact paths in, the same ones
// migrate-db itself resolves and uses, closes that gap: this legacy
// documents.sqlite lives in a directory that has nothing to do with where
// helmcentral.sqlite would go, and the guard still has to catch it.
func TestCheckForLegacyDatabaseFiles_DetectsPathOutsideHelmcentralDir(t *testing.T) {
	legacyDir := t.TempDir()
	legacyDocs := filepath.Join(legacyDir, "custom-documents.sqlite")
	mustWriteFile(t, legacyDocs, "x")

	// The other five legacy paths, and helmcentral.sqlite's own directory,
	// are a completely different temp dir - only legacyDocs, off on its
	// own custom path, exists at all.
	helmcentralDir := t.TempDir()
	legacyAssistant := filepath.Join(helmcentralDir, "assistant.sqlite")
	legacyNearby := filepath.Join(helmcentralDir, "nearby-contacts.sqlite")
	legacyAlarmLog := filepath.Join(helmcentralDir, "alarm-log.sqlite")
	legacyWebPush := filepath.Join(helmcentralDir, "webpush-subscriptions.sqlite")
	legacyPluginOverrides := filepath.Join(helmcentralDir, "plugin_overrides.sqlite")

	err := checkForLegacyDatabaseFiles(legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides)
	if err == nil {
		t.Fatal("expected an error when a legacy file exists at a custom path outside the helmcentral directory")
	}
}

func mustWriteFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// ── renameDocumentsFileForward ───────────────────────────────────────────

// TestRenameDocumentsFileForward_UndoesPartialRenameOnSidecarFailure pins
// the review finding that a failure moving -wal/-shm AFTER the main file
// has already been renamed used to strand documents.sqlite's own pieces
// across two different names: helmcentral.sqlite (the main file, and
// whichever sidecar succeeded) plus documents.sqlite-shm (say) still under
// its old name. For a WAL-mode file, an orphaned -wal/-shm left behind like
// that can hold committed data never checkpointed into the main file - data
// that then looks lost the moment the two pieces are no longer under
// matching names. This forces the -shm rename specifically to fail (by
// pre-creating a directory at its destination, so os.Rename errors) and
// checks that renameDocumentsFileForward undoes the main file's rename and
// the -wal rename that DID succeed, leaving documents.sqlite (and its own
// -wal/-shm) exactly as they were before this call.
func TestRenameDocumentsFileForward_UndoesPartialRenameOnSidecarFailure(t *testing.T) {
	dir := t.TempDir()
	legacyDocs := filepath.Join(dir, "documents.sqlite")
	helmcentralPath := filepath.Join(dir, "helmcentral.sqlite")

	mustWriteFile(t, legacyDocs, "main db content")
	mustWriteFile(t, legacyDocs+"-wal", "wal content")
	mustWriteFile(t, legacyDocs+"-shm", "shm content")

	// Block the -shm rename specifically: renaming a file onto an existing
	// directory fails, so this deterministically forces the failure on the
	// second sidecar, after the -wal rename (the first) has already
	// succeeded.
	if err := os.Mkdir(helmcentralPath+"-shm", 0o755); err != nil {
		t.Fatalf("mkdir blocker: %v", err)
	}

	err := renameDocumentsFileForward(legacyDocs, helmcentralPath)
	if err == nil {
		t.Fatal("expected an error when the -shm rename fails")
	}

	if _, statErr := os.Stat(legacyDocs); statErr != nil {
		t.Errorf("expected documents.sqlite to be restored to its original name: %v", statErr)
	}
	if _, statErr := os.Stat(legacyDocs + "-wal"); statErr != nil {
		t.Errorf("expected documents.sqlite-wal to be restored to its original name: %v", statErr)
	}
	if _, statErr := os.Stat(helmcentralPath); !os.IsNotExist(statErr) {
		t.Errorf("expected helmcentral.sqlite to be gone after the undo, stat err = %v", statErr)
	}
	if _, statErr := os.Stat(helmcentralPath + "-wal"); !os.IsNotExist(statErr) {
		t.Errorf("expected helmcentral.sqlite-wal to be gone after the undo, stat err = %v", statErr)
	}
	// The blocking directory itself is untouched - not this function's job
	// to clean up, only to not strand real data around it.
	if info, statErr := os.Stat(helmcentralPath + "-shm"); statErr != nil || !info.IsDir() {
		t.Errorf("expected the blocking directory to remain in place, stat err = %v", statErr)
	}
}

// ── migrateToHelmcentralDB ───────────────────────────────────────────────

// migrateTestPaths returns a fresh temp dir's would-be helmcentral.sqlite
// path plus the six legacy paths inside it, none of them created yet.
func migrateTestPaths(t *testing.T) (newPath, legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides string) {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "helmcentral.sqlite"),
		filepath.Join(dir, "documents.sqlite"),
		filepath.Join(dir, "assistant.sqlite"),
		filepath.Join(dir, "nearby-contacts.sqlite"),
		filepath.Join(dir, "alarm-log.sqlite"),
		filepath.Join(dir, "webpush-subscriptions.sqlite"),
		filepath.Join(dir, "plugin_overrides.sqlite")
}

func TestMigrateToHelmcentralDB_RefusesWhenHelmcentralAlreadyExists(t *testing.T) {
	newPath, legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides := migrateTestPaths(t)
	mustWriteFile(t, newPath, "already here")
	mustWriteFile(t, legacyDocs, "irrelevant")

	_, err := migrateToHelmcentralDB(newPath, legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides)
	if err == nil {
		t.Fatal("expected an error when helmcentral.sqlite already exists")
	}
	// Operator-visible text must stay in plain language - no ADR numbers or
	// other internal-decision-record references leaking into a message the
	// operator has to read and act on.
	if strings.Contains(strings.ToUpper(err.Error()), "ADR") {
		t.Errorf("expected no ADR reference in the operator-visible error, got %q", err.Error())
	}
}

// TestMigrateToHelmcentralDB_RefusesWhenNoLegacyFilesExist pins the review
// finding that refusing outright whenever documents.sqlite alone was
// missing - regardless of what else was present - left an install old
// enough to predate the document store (ADR 0106) unable to migrate even
// though it genuinely had other legacy data. The refusal now only fires
// when NONE of the six legacy files exist at all: a plain fresh install
// with nothing to migrate.
func TestMigrateToHelmcentralDB_RefusesWhenNoLegacyFilesExist(t *testing.T) {
	newPath, legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides := migrateTestPaths(t)
	// None of the six legacy paths created.

	_, err := migrateToHelmcentralDB(newPath, legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides)
	if err == nil {
		t.Fatal("expected an error when no legacy database file exists")
	}
	if _, statErr := os.Stat(newPath); statErr == nil {
		t.Fatal("expected no helmcentral.sqlite to be created on refusal")
	}
}

// TestMigrateToHelmcentralDB_DocumentsMissingButOtherLegacyFilesPresentSucceeds
// covers the install this review finding was about: no documents.sqlite
// ever existed (predating ADR 0106), but assistant.sqlite and
// alarm-log.sqlite do. migrateWithoutDocumentsFile creates helmcentral.sqlite
// fresh, with the documents schema but no document rows, then ATTACHes and
// copies the two legacy files that do exist - the same single transaction
// migrateWithDocumentsFile's ATTACH-and-copy step uses. All six stores,
// including documentStore itself (with an empty but usable schema), must be
// able to open the result afterward.
func TestMigrateToHelmcentralDB_DocumentsMissingButOtherLegacyFilesPresentSucceeds(t *testing.T) {
	newPath, legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides := migrateTestPaths(t)
	// legacyDocs, legacyNearby, legacyWebPush and legacyPluginOverrides
	// deliberately absent - only assistant and alarm-log exist, standing in
	// for an install old enough to predate the document store but that had
	// Mate and alarms configured.
	mustCreateLegacyAssistantDB(t, legacyAssistant)
	mustCreateLegacyAlarmLogDB(t, legacyAlarmLog)

	result, err := migrateToHelmcentralDB(newPath, legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides)
	if err != nil {
		t.Fatalf("migrateToHelmcentralDB: %v", err)
	}

	if result.DocumentsRenamed {
		t.Error("expected DocumentsRenamed to be false when documents.sqlite never existed")
	}
	if !result.AssistantMigrated {
		t.Error("expected AssistantMigrated to be true")
	}
	if !result.AlarmLogMigrated {
		t.Error("expected AlarmLogMigrated to be true")
	}
	if result.NearbyMigrated || result.WebPushMigrated || result.PluginOverridesMigrated {
		t.Errorf("expected the three absent legacy files to report false, got %+v", result)
	}

	if _, statErr := os.Stat(newPath); statErr != nil {
		t.Fatalf("expected helmcentral.sqlite to exist: %v", statErr)
	}
	assertJournalModeWAL(t, newPath)

	for _, p := range []string{legacyAssistant, legacyAlarmLog} {
		if _, statErr := os.Stat(p); !os.IsNotExist(statErr) {
			t.Errorf("expected %s to no longer exist, stat err = %v", p, statErr)
		}
		if _, statErr := os.Stat(p + ".migrated"); statErr != nil {
			t.Errorf("expected %s.migrated to exist: %v", p, statErr)
		}
	}

	// documents.sqlite never existed, so there is no legacyDocs (or
	// legacyDocs.migrated) to check either way - only that documentStore
	// itself can open the combined file with a usable, empty schema.
	docStore, err := newDocumentStore(newPath)
	if err != nil {
		t.Fatalf("newDocumentStore on migrated file: %v", err)
	}
	docStore.Close()

	asStore, err := newAssistantStore(newPath)
	if err != nil {
		t.Fatalf("newAssistantStore on migrated file: %v", err)
	}
	defer asStore.Close()
	convs, err := asStore.ListConversations()
	if err != nil || len(convs) != 1 || convs[0].Title != "legacy conversation" {
		t.Fatalf("expected the migrated conversation to survive, got convs=%+v err=%v", convs, err)
	}

	ncStore, err := newNearbyContactStore(newPath)
	if err != nil {
		t.Fatalf("newNearbyContactStore on migrated file: %v", err)
	}
	ncStore.close()

	alStore, err := newAlarmLogStore(newPath)
	if err != nil {
		t.Fatalf("newAlarmLogStore on migrated file: %v", err)
	}
	defer alStore.Close()
	entries, err := alStore.Recent(10)
	if err != nil || len(entries) != 1 || entries[0].RuleID != "rule-1" {
		t.Fatalf("expected the migrated alarm log entry to survive, got entries=%+v err=%v", entries, err)
	}

	wpStore, err := newWebPushSubscriptionStore(newPath)
	if err != nil {
		t.Fatalf("newWebPushSubscriptionStore on migrated file: %v", err)
	}
	wpStore.Close()

	poStore, err := newPluginOverridesStore(newPath)
	if err != nil {
		t.Fatalf("newPluginOverridesStore on migrated file: %v", err)
	}
	poStore.db.Close()
}

// TestMigrateToHelmcentralDB_FailureWithoutDocumentsFileRemovesHelmcentralFile
// pins the failure-cleanup half of the same fix: when documents.sqlite never
// existed, there is nothing to rename helmcentral.sqlite back to on failure,
// so migrateWithoutDocumentsFile removes the file it created instead. A
// broken webpush-subscriptions.sqlite (missing vapid_public_key, a required
// column with no default) fails the copy after assistant has already copied
// successfully inside the same transaction; the assistant file must come out
// of the rollback untouched, and a rerun after fixing the fixture must
// succeed.
func TestMigrateToHelmcentralDB_FailureWithoutDocumentsFileRemovesHelmcentralFile(t *testing.T) {
	newPath, legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides := migrateTestPaths(t)
	mustCreateLegacyAssistantDB(t, legacyAssistant)
	mustCreateLegacyWebPushDBMissingColumn(t, legacyWebPush)

	_, err := migrateToHelmcentralDB(newPath, legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides)
	if err == nil {
		t.Fatal("expected an error when the web push copy fails")
	}

	if _, statErr := os.Stat(newPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected helmcentral.sqlite to have been removed, stat err = %v", statErr)
	}
	if _, statErr := os.Stat(newPath + "-wal"); !os.IsNotExist(statErr) {
		t.Errorf("expected helmcentral.sqlite-wal to have been removed too, stat err = %v", statErr)
	}

	if _, statErr := os.Stat(legacyAssistant); statErr != nil {
		t.Errorf("expected assistant.sqlite to remain at its original name (its own copy was inside the same rolled-back transaction): %v", statErr)
	}
	if _, statErr := os.Stat(legacyAssistant + ".migrated"); !os.IsNotExist(statErr) {
		t.Errorf("expected assistant.sqlite NOT to be renamed to .migrated, since nothing committed, stat err = %v", statErr)
	}

	// Fix the broken fixture and confirm the install is genuinely retryable.
	if err := os.Remove(legacyWebPush); err != nil {
		t.Fatalf("remove broken legacy webpush db: %v", err)
	}
	mustCreateLegacyWebPushDB(t, legacyWebPush)

	result, err := migrateToHelmcentralDB(newPath, legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides)
	if err != nil {
		t.Fatalf("second migrateToHelmcentralDB after fixing the fixture: %v", err)
	}
	if !result.AssistantMigrated {
		t.Error("expected AssistantMigrated to be true on the successful retry")
	}
	if !result.WebPushMigrated {
		t.Error("expected WebPushMigrated to be true on the successful retry")
	}
}

func TestMigrateToHelmcentralDB_RefusesWhenHotJournalPresent(t *testing.T) {
	newPath, legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides := migrateTestPaths(t)
	mustCreateLegacyDocumentsDB(t, legacyDocs)
	mustWriteFile(t, legacyDocs+"-journal", "hot journal")

	_, err := migrateToHelmcentralDB(newPath, legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides)
	if err == nil {
		t.Fatal("expected an error when a hot rollback journal is present")
	}
	// The source file must be left exactly where it was - refusing must not
	// rename a database an operator still needs to clean up by hand.
	if _, statErr := os.Stat(legacyDocs); statErr != nil {
		t.Fatalf("expected documents.sqlite to remain in place: %v", statErr)
	}
}

// mustCreateLegacyDocumentsDB creates a real pre-migration documents.sqlite
// via newDocumentStore itself (its own documentStoreSchema), with one row,
// standing in for a real operator's database. documents.sqlite is renamed
// forward rather than ATTACHed and copied (migrateToHelmcentralDB), so the
// migrated file has to carry the real schema already - a fixture with only
// an id/filename stand-in table (as a fake table created ad hoc here once
// did) leaves out columns like folder_id that documentStoreSchema's own
// indexes and triggers reference, which then breaks the moment the migrated
// file is reopened through newDocumentStore.
func mustCreateLegacyDocumentsDB(t *testing.T, path string) {
	t.Helper()
	store, err := newDocumentStore(path)
	if err != nil {
		t.Fatalf("newDocumentStore (legacy fixture): %v", err)
	}
	defer store.Close()
	if _, err := store.Insert(document{ID: "doc-1", SHA256: "legacy-fixture-sha", Filename: "manual.pdf", MIME: "application/pdf"}); err != nil {
		t.Fatalf("Insert (legacy fixture): %v", err)
	}
}

// mustCreateLegacyAssistantDB creates a real pre-migration assistant.sqlite
// via newAssistantStore itself (its own schema code), with one conversation
// and one message, so the migration's row-count verification has real data
// to check.
func mustCreateLegacyAssistantDB(t *testing.T, path string) {
	t.Helper()
	store, err := newAssistantStore(path)
	if err != nil {
		t.Fatalf("newAssistantStore (legacy fixture): %v", err)
	}
	defer store.Close()
	conv, err := store.CreateConversation("legacy conversation")
	if err != nil {
		t.Fatalf("CreateConversation (legacy fixture): %v", err)
	}
	if _, err := store.AppendMessage(assistantMessage{ConversationID: conv.ID, Role: "user", Content: "hello"}); err != nil {
		t.Fatalf("AppendMessage (legacy fixture): %v", err)
	}
}

// mustCreateLegacyNearbyDB creates a real pre-migration nearby-contacts.sqlite
// via newNearbyContactStore itself, with one confirmed sighting.
func mustCreateLegacyNearbyDB(t *testing.T, path string) {
	t.Helper()
	store, err := newNearbyContactStore(path)
	if err != nil {
		t.Fatalf("newNearbyContactStore (legacy fixture): %v", err)
	}
	defer store.close()
	store.dwell = 0
	now := time.Date(2026, time.September, 20, 8, 0, 0, 0, time.UTC)
	if err := store.recordContactIfNew("mmsi:111111111", "Legacy Vessel", -33.8, 151.2, "", "anchored", now, now); err != nil {
		t.Fatalf("recordContactIfNew (legacy fixture): %v", err)
	}
}

// mustCreateLegacyNearbyDBMissingColumn creates a pre-migration
// nearby-contacts.sqlite whose nearby_vessel_contacts table is missing the
// geoname column, standing in for a corrupt or hand-edited legacy file: the
// migration's copy step ("INSERT INTO main.nearby_vessel_contacts (...,
// geoname, ...) SELECT ..., geoname, ... FROM legacy_nearby...") then fails
// deterministically at the SQL level ("no such column: geoname") rather
// than needing any filesystem-permission trickery to force a failure.
func mustCreateLegacyNearbyDBMissingColumn(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open broken legacy nearby db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE nearby_vessel_contacts (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		vessel_key  TEXT NOT NULL,
		name        TEXT NOT NULL,
		seen_at     INTEGER NOT NULL,
		lat         REAL NOT NULL,
		lon         REAL NOT NULL,
		nav_context TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("create broken nearby_vessel_contacts table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO nearby_vessel_contacts (vessel_key, name, seen_at, lat, lon, nav_context) VALUES ('mmsi:1', 'Broken Vessel', 0, -33.8, 151.2, 'anchored')`); err != nil {
		t.Fatalf("insert broken nearby row: %v", err)
	}
}

// mustCreateLegacyAlarmLogDB creates a real pre-migration alarm-log.sqlite
// via newAlarmLogStore itself, with one alarm_log occurrence and one queued
// notification, so the migration's row-count verification has real data in
// both tables to check.
func mustCreateLegacyAlarmLogDB(t *testing.T, path string) {
	t.Helper()
	store, err := newAlarmLogStore(path)
	if err != nil {
		t.Fatalf("newAlarmLogStore (legacy fixture): %v", err)
	}
	defer store.Close()
	now := time.Date(2026, time.September, 20, 8, 0, 0, 0, time.UTC)
	if _, err := store.RecordRaised(alarmLogEntry{RuleID: "rule-1", Label: "Legacy alarm", State: alarmStateAlarm, RaisedAt: now}); err != nil {
		t.Fatalf("RecordRaised (legacy fixture): %v", err)
	}
	if err := store.Enqueue("ntfy", "rule-1", []byte("legacy payload"), now); err != nil {
		t.Fatalf("Enqueue (legacy fixture): %v", err)
	}
}

// mustCreateLegacyAlarmLogDBMissingRuleIDColumn creates a pre-migration
// alarm-log.sqlite whose notification_queue table predates
// ensureQueueTable/createAlarmLogSchema's own ALTER TABLE that added
// rule_id: a real install that was never reopened after that column
// shipped. migrateAttachedLegacyData (helmcentral_db.go) patches this
// column onto the attached legacy file before copying, so migration must
// succeed against it - unlike mustCreateLegacyNearbyDBMissingColumn above,
// this is not a fixture used to force a failure.
func mustCreateLegacyAlarmLogDBMissingRuleIDColumn(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open pre-rule_id legacy alarm log db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE alarm_log (
		id             TEXT PRIMARY KEY,
		rule_id        TEXT NOT NULL,
		source         TEXT NOT NULL,
		label          TEXT NOT NULL,
		path           TEXT NOT NULL,
		state          TEXT NOT NULL,
		message        TEXT NOT NULL,
		value_at_raise REAL NOT NULL,
		raised_at      INTEGER NOT NULL,
		acked_at       INTEGER,
		cleared_at     INTEGER
	)`); err != nil {
		t.Fatalf("create pre-rule_id alarm_log table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO alarm_log (id, rule_id, source, label, path, state, message, value_at_raise, raised_at)
		VALUES ('log-1', 'rule-1', 'rule', 'Legacy alarm', 'electrical.batteries.house.voltage', 'alarm', 'House bank low', 11.0, 0)`); err != nil {
		t.Fatalf("insert pre-rule_id alarm_log row: %v", err)
	}
	// notification_queue as it existed before rule_id was ALTER TABLE'd on -
	// no rule_id column at all.
	if _, err := db.Exec(`CREATE TABLE notification_queue (
		id              TEXT PRIMARY KEY,
		transport       TEXT NOT NULL,
		payload         BLOB NOT NULL,
		attempts        INTEGER NOT NULL DEFAULT 0,
		next_attempt_at INTEGER NOT NULL,
		created_at      INTEGER NOT NULL,
		last_error      TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		t.Fatalf("create pre-rule_id notification_queue table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO notification_queue (id, transport, payload, attempts, next_attempt_at, created_at)
		VALUES ('queue-1', 'ntfy', 'legacy payload', 0, 0, 0)`); err != nil {
		t.Fatalf("insert pre-rule_id notification_queue row: %v", err)
	}
}

// mustCreateLegacyWebPushDB creates a real pre-migration
// webpush-subscriptions.sqlite via newWebPushSubscriptionStore itself, with
// one registered device.
func mustCreateLegacyWebPushDB(t *testing.T, path string) {
	t.Helper()
	store, err := newWebPushSubscriptionStore(path)
	if err != nil {
		t.Fatalf("newWebPushSubscriptionStore (legacy fixture): %v", err)
	}
	defer store.Close()
	if _, err := store.Upsert(webPushSubscription{
		Endpoint:       "https://push.example/legacy",
		P256dh:         "legacy-p256dh",
		Auth:           "legacy-auth",
		Label:          "Legacy phone",
		VAPIDPublicKey: "legacy-key",
	}); err != nil {
		t.Fatalf("Upsert (legacy fixture): %v", err)
	}
}

// mustCreateLegacyWebPushDBMissingColumn creates a pre-migration
// webpush-subscriptions.sqlite whose push_subscriptions table is missing
// the vapid_public_key column, standing in for a corrupt or hand-edited
// legacy file - used to force a failure during the copy of a NEW (post
// ADR 0141) table, the same way mustCreateLegacyNearbyDBMissingColumn does
// for nearby-contacts above. vapid_public_key has no default, so the
// explicit-column-list copy fails deterministically at the SQL level ("no
// such column: vapid_public_key").
func mustCreateLegacyWebPushDBMissingColumn(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open broken legacy webpush db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE push_subscriptions (
		id              TEXT PRIMARY KEY,
		endpoint        TEXT NOT NULL UNIQUE,
		p256dh          TEXT NOT NULL,
		auth            TEXT NOT NULL,
		label           TEXT NOT NULL DEFAULT '',
		user_agent      TEXT NOT NULL DEFAULT '',
		created_at      INTEGER NOT NULL,
		last_success_at INTEGER,
		last_error      TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		t.Fatalf("create broken push_subscriptions table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO push_subscriptions (id, endpoint, p256dh, auth, created_at) VALUES ('sub-1', 'https://push.example/broken', 'p', 'a', 0)`); err != nil {
		t.Fatalf("insert broken push_subscriptions row: %v", err)
	}
}

// mustCreateLegacyPluginOverridesDB creates a real pre-migration
// plugin_overrides.sqlite via newPluginOverridesStore itself, with one
// override row and one config value row.
func mustCreateLegacyPluginOverridesDB(t *testing.T, path string) {
	t.Helper()
	store, err := newPluginOverridesStore(path)
	if err != nil {
		t.Fatalf("newPluginOverridesStore (legacy fixture): %v", err)
	}
	defer store.db.Close()
	if err := store.Set("plugins/tides/bom.wasm", []string{"www.bom.gov.au"}, nil); err != nil {
		t.Fatalf("Set (legacy fixture): %v", err)
	}
	if err := store.SetConfigValues("plugins/poi/osm-overpass.wasm", map[string]string{"overpass_url": "https://overpass.example/api/interpreter"}); err != nil {
		t.Fatalf("SetConfigValues (legacy fixture): %v", err)
	}
}

// mustCreateLegacyPluginOverridesDBMissingConfigValuesTable creates a
// pre-migration plugin_overrides.sqlite that predates plugin_config_values
// (createPluginOverridesSchema's second table, added after plugin_overrides
// itself shipped): a real install that was never reopened after that table
// shipped, so plugin_config_values doesn't exist in it at all.
// migrateAttachedLegacyData (helmcentral_db.go) creates this table directly
// on the attached legacy file before copying, so migration must succeed
// against it - unlike mustCreateLegacyNearbyDBMissingColumn above, this is
// not a fixture used to force a failure.
func mustCreateLegacyPluginOverridesDBMissingConfigValuesTable(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open pre-plugin_config_values legacy db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE plugin_overrides (
		wasm_path       TEXT PRIMARY KEY,
		allowed_hosts   TEXT NOT NULL,
		allowed_secrets TEXT NOT NULL,
		updated_at      INTEGER NOT NULL
	)`); err != nil {
		t.Fatalf("create pre-plugin_config_values plugin_overrides table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO plugin_overrides (wasm_path, allowed_hosts, allowed_secrets, updated_at)
		VALUES ('plugins/tides/bom.wasm', '["www.bom.gov.au"]', '[]', 0)`); err != nil {
		t.Fatalf("insert pre-plugin_config_values plugin_overrides row: %v", err)
	}
}

// TestMigrateToHelmcentralDB_FailureAfterRenameRestoresDocumentsFile pins the
// review finding that a failure partway through migration - here, the
// nearby-contacts copy hitting a legacy file with a missing column - must
// not leave the install permanently stuck: documents.sqlite had already
// been renamed to helmcentral.sqlite before the failure, so every rerun
// would otherwise refuse at the very first check (helmcentral.sqlite
// already exists) with no documents.sqlite left to migrate from.
func TestMigrateToHelmcentralDB_FailureAfterRenameRestoresDocumentsFile(t *testing.T) {
	newPath, legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides := migrateTestPaths(t)
	mustCreateLegacyDocumentsDB(t, legacyDocs)
	mustCreateLegacyAssistantDB(t, legacyAssistant)
	mustCreateLegacyNearbyDBMissingColumn(t, legacyNearby)

	_, err := migrateToHelmcentralDB(newPath, legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides)
	if err == nil {
		t.Fatal("expected an error when the nearby-contacts copy fails")
	}

	if _, statErr := os.Stat(legacyDocs); statErr != nil {
		t.Fatalf("expected documents.sqlite to be restored to its original name: %v", statErr)
	}
	if _, statErr := os.Stat(newPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected helmcentral.sqlite to be gone after the restore, stat err = %v", statErr)
	}
	if _, statErr := os.Stat(legacyAssistant); statErr != nil {
		t.Fatalf("expected assistant.sqlite to remain at its original name (its own copy was inside the same rolled-back transaction): %v", statErr)
	}
	if _, statErr := os.Stat(legacyAssistant + ".migrated"); !os.IsNotExist(statErr) {
		t.Fatalf("expected assistant.sqlite NOT to be renamed to .migrated, since nothing committed, stat err = %v", statErr)
	}

	// documents.sqlite must be usable again, not left half-converted, even
	// though the restored file has picked up WAL mode and the (still
	// empty, since nothing committed) assistant/nearby-contact tables along
	// the way - both harmless leftovers from the failed attempt.
	store, openErr := newDocumentStore(legacyDocs)
	if openErr != nil {
		t.Fatalf("newDocumentStore on restored file: %v", openErr)
	}
	var filename string
	scanErr := store.db.QueryRow(`SELECT filename FROM documents WHERE id = 'doc-1'`).Scan(&filename)
	store.Close()
	if scanErr != nil {
		t.Fatalf("read restored document row: %v", scanErr)
	}
	if filename != "manual.pdf" {
		t.Fatalf("expected restored filename %q, got %q", "manual.pdf", filename)
	}

	// Fix the broken fixture and confirm the install is genuinely retryable.
	if err := os.Remove(legacyNearby); err != nil {
		t.Fatalf("remove broken legacy nearby db: %v", err)
	}
	mustCreateLegacyNearbyDB(t, legacyNearby)

	result, err := migrateToHelmcentralDB(newPath, legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides)
	if err != nil {
		t.Fatalf("second migrateToHelmcentralDB after fixing the fixture: %v", err)
	}
	if !result.AssistantMigrated {
		t.Error("expected AssistantMigrated to be true on the successful retry")
	}
	if !result.NearbyMigrated {
		t.Error("expected NearbyMigrated to be true on the successful retry")
	}
}

// TestMigrateToHelmcentralDB_FailureInNewTableRestoresDocumentsFile pins the
// same review finding as TestMigrateToHelmcentralDB_FailureAfterRenameRestoresDocumentsFile
// above, but with the failure inside one of the three tables added by this
// change (alarm log, web push, plugin overrides) instead of nearby-contacts:
// a broken push_subscriptions table (missing vapid_public_key, a required
// column with no default) fails the copy after assistant, nearby-contacts
// and alarm-log have already copied successfully inside the same
// transaction. Every one of the five ATTACH-and-copy legacy files - not
// just the one that failed - must come out of the rollback untouched: none
// renamed to .migrated, since nothing committed.
func TestMigrateToHelmcentralDB_FailureInNewTableRestoresDocumentsFile(t *testing.T) {
	newPath, legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides := migrateTestPaths(t)
	mustCreateLegacyDocumentsDB(t, legacyDocs)
	mustCreateLegacyAssistantDB(t, legacyAssistant)
	mustCreateLegacyNearbyDB(t, legacyNearby)
	mustCreateLegacyAlarmLogDB(t, legacyAlarmLog)
	mustCreateLegacyWebPushDBMissingColumn(t, legacyWebPush)
	mustCreateLegacyPluginOverridesDB(t, legacyPluginOverrides)

	_, err := migrateToHelmcentralDB(newPath, legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides)
	if err == nil {
		t.Fatal("expected an error when the web push copy fails")
	}

	if _, statErr := os.Stat(legacyDocs); statErr != nil {
		t.Fatalf("expected documents.sqlite to be restored to its original name: %v", statErr)
	}
	if _, statErr := os.Stat(newPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected helmcentral.sqlite to be gone after the restore, stat err = %v", statErr)
	}

	for _, p := range []string{legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides} {
		if _, statErr := os.Stat(p); statErr != nil {
			t.Errorf("expected %s to remain at its original name (its own copy was inside the same rolled-back transaction): %v", p, statErr)
		}
		if _, statErr := os.Stat(p + ".migrated"); !os.IsNotExist(statErr) {
			t.Errorf("expected %s NOT to be renamed to .migrated, since nothing committed, stat err = %v", p, statErr)
		}
	}
}

// TestMigrateToHelmcentralDB_RenameToMigratedFailureExplainsManualFix pins
// the review finding that a failure renaming a legacy file to .migrated -
// AFTER migrateAttachedLegacyData's transaction has already committed its
// rows into helmcentral.sqlite - leaves the install stuck: startup refuses
// (assistant.sqlite is still sitting at its old name) and a second
// migrate-db run refuses too (helmcentral.sqlite already exists), with no
// automatic way forward. The error has to spell out the exact manual step,
// since the operator has no other way to know the data already made it in
// safely and this is a cleanup-only failure, not a lost migration.
func TestMigrateToHelmcentralDB_RenameToMigratedFailureExplainsManualFix(t *testing.T) {
	newPath, legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides := migrateTestPaths(t)
	mustCreateLegacyDocumentsDB(t, legacyDocs)
	mustCreateLegacyAssistantDB(t, legacyAssistant)
	// legacyNearby deliberately left absent - keeps this test scoped to the
	// one failure under test.

	// Force the assistant.sqlite -> assistant.sqlite.migrated rename to
	// fail: pre-create a directory at the destination, so os.Rename errors.
	if err := os.Mkdir(legacyAssistant+".migrated", 0o755); err != nil {
		t.Fatalf("mkdir blocker: %v", err)
	}

	result, err := migrateToHelmcentralDB(newPath, legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides)
	if err == nil {
		t.Fatal("expected an error when renaming assistant.sqlite to .migrated fails")
	}

	msg := err.Error()
	if !strings.Contains(msg, "already safely copied") {
		t.Errorf("expected the error to say the data already made it into helmcentral.sqlite, got %q", msg)
	}
	if !strings.Contains(msg, legacyAssistant+".migrated") {
		t.Errorf("expected the error to name the exact manual rename target, got %q", msg)
	}

	if _, statErr := os.Stat(legacyAssistant); statErr != nil {
		t.Errorf("expected assistant.sqlite to remain at its original name after the failed rename: %v", statErr)
	}

	// The data itself must genuinely be in helmcentral.sqlite - this
	// failure is purely a bookkeeping rename, not a lost migration.
	if !result.AssistantMigrated {
		t.Error("expected AssistantMigrated to be true - the copy itself succeeded")
	}
	asStore, storeErr := newAssistantStore(newPath)
	if storeErr != nil {
		t.Fatalf("newAssistantStore on the combined file: %v", storeErr)
	}
	defer asStore.Close()
	convs, listErr := asStore.ListConversations()
	if listErr != nil {
		t.Fatalf("ListConversations: %v", listErr)
	}
	if len(convs) != 1 || convs[0].Title != "legacy conversation" {
		t.Fatalf("expected the migrated conversation to be present despite the rename failure, got %+v", convs)
	}
}

func TestMigrateToHelmcentralDB_OnlyDocumentsPresent(t *testing.T) {
	newPath, legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides := migrateTestPaths(t)
	mustCreateLegacyDocumentsDB(t, legacyDocs)

	result, err := migrateToHelmcentralDB(newPath, legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides)
	if err != nil {
		t.Fatalf("migrateToHelmcentralDB: %v", err)
	}

	if result.AssistantMigrated {
		t.Error("expected AssistantMigrated to be false when assistant.sqlite never existed")
	}
	if result.NearbyMigrated {
		t.Error("expected NearbyMigrated to be false when nearby-contacts.sqlite never existed")
	}
	if _, err := os.Stat(legacyDocs); !os.IsNotExist(err) {
		t.Errorf("expected documents.sqlite to be gone (renamed), stat err = %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("expected helmcentral.sqlite to exist: %v", err)
	}

	assertJournalModeWAL(t, newPath)

	// The renamed file's own document row must have made the trip.
	store, err := newDocumentStore(newPath)
	if err != nil {
		t.Fatalf("newDocumentStore on migrated file: %v", err)
	}
	defer store.Close()
	var filename string
	if err := store.db.QueryRow(`SELECT filename FROM documents WHERE id = 'doc-1'`).Scan(&filename); err != nil {
		t.Fatalf("read migrated document row: %v", err)
	}
	if filename != "manual.pdf" {
		t.Fatalf("expected migrated filename %q, got %q", "manual.pdf", filename)
	}
}

// TestMigrateToHelmcentralDB_HappyPathAllSixFiles exercises the full
// migration with every one of the six legacy files present and holding real
// rows: documents.sqlite (renamed forward) plus the five ATTACH-and-copy
// files (assistant, nearby-contacts, alarm-log, webpush-subscriptions,
// plugin_overrides). The alarm-log and plugin_overrides fixtures are
// deliberately the pre-lazy-migration shapes
// (mustCreateLegacyAlarmLogDBMissingRuleIDColumn,
// mustCreateLegacyPluginOverridesDBMissingConfigValuesTable) rather than the
// current ones, so this happy path also proves migrateAttachedLegacyData's
// own column/table patch-up runs successfully as part of an otherwise
// ordinary migration, not only in a test that targets it directly.
func TestMigrateToHelmcentralDB_HappyPathAllSixFiles(t *testing.T) {
	newPath, legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides := migrateTestPaths(t)
	mustCreateLegacyDocumentsDB(t, legacyDocs)
	mustCreateLegacyAssistantDB(t, legacyAssistant)
	mustCreateLegacyNearbyDB(t, legacyNearby)
	mustCreateLegacyAlarmLogDBMissingRuleIDColumn(t, legacyAlarmLog)
	mustCreateLegacyWebPushDB(t, legacyWebPush)
	mustCreateLegacyPluginOverridesDBMissingConfigValuesTable(t, legacyPluginOverrides)

	result, err := migrateToHelmcentralDB(newPath, legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides)
	if err != nil {
		t.Fatalf("migrateToHelmcentralDB: %v", err)
	}

	if !result.AssistantMigrated {
		t.Error("expected AssistantMigrated to be true")
	}
	if !result.NearbyMigrated {
		t.Error("expected NearbyMigrated to be true")
	}
	if !result.AlarmLogMigrated {
		t.Error("expected AlarmLogMigrated to be true")
	}
	if !result.WebPushMigrated {
		t.Error("expected WebPushMigrated to be true")
	}
	if !result.PluginOverridesMigrated {
		t.Error("expected PluginOverridesMigrated to be true")
	}

	// documents.sqlite is gone (renamed to helmcentral.sqlite); the other
	// five are gone from their own names, renamed to .migrated, never
	// deleted outright. (renameLegacyToMigrated's own handling of a
	// surviving -wal/-shm sidecar is covered directly, in isolation, by
	// TestRenameLegacyToMigrated_RenamesWALAndSHMSidecarsWhenPresent below -
	// not here, since DETACHing a WAL-mode attached database, the step
	// immediately before this rename, always checkpoints and clears its own
	// -wal/-shm as a side effect when (as here) nothing else still has it
	// open, so there is never really one left over to plant for this test
	// to find.)
	for _, p := range []string{legacyDocs, legacyAssistant, legacyNearby, legacyAlarmLog, legacyWebPush, legacyPluginOverrides} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("expected %s to no longer exist, stat err = %v", p, err)
		}
	}
	for _, p := range []string{legacyAssistant + ".migrated", legacyNearby + ".migrated", legacyAlarmLog + ".migrated", legacyWebPush + ".migrated", legacyPluginOverrides + ".migrated"} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to exist: %v", p, err)
		}
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("expected helmcentral.sqlite to exist: %v", err)
	}

	assertJournalModeWAL(t, newPath)

	// Every row from every legacy file must have made the trip into the one
	// combined file - the actual point of the migration.
	asStore, err := newAssistantStore(newPath)
	if err != nil {
		t.Fatalf("newAssistantStore on migrated file: %v", err)
	}
	defer asStore.Close()
	convs, err := asStore.ListConversations()
	if err != nil {
		t.Fatalf("ListConversations on migrated file: %v", err)
	}
	if len(convs) != 1 || convs[0].Title != "legacy conversation" {
		t.Fatalf("expected the migrated conversation to survive, got %+v", convs)
	}
	msgs, err := asStore.ListMessages(convs[0].ID)
	if err != nil {
		t.Fatalf("ListMessages on migrated file: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "hello" {
		t.Fatalf("expected the migrated message to survive, got %+v", msgs)
	}

	ncStore, err := newNearbyContactStore(newPath)
	if err != nil {
		t.Fatalf("newNearbyContactStore on migrated file: %v", err)
	}
	defer ncStore.close()
	if count := countRows(t, ncStore, "mmsi:111111111"); count != 1 {
		t.Fatalf("expected the migrated sighting to survive, got %d rows", count)
	}

	alStore, err := newAlarmLogStore(newPath)
	if err != nil {
		t.Fatalf("newAlarmLogStore on migrated file: %v", err)
	}
	defer alStore.Close()
	entries, err := alStore.Recent(10)
	if err != nil || len(entries) != 1 || entries[0].RuleID != "rule-1" {
		t.Fatalf("expected the migrated alarm log entry to survive, got entries=%+v err=%v", entries, err)
	}
	depth, err := alStore.QueueDepth()
	if err != nil || depth != 1 {
		t.Fatalf("expected the migrated notification queue row to survive, got depth=%d err=%v", depth, err)
	}
	// The pre-rule_id legacy fixture's queued row must have picked up the
	// default empty rule_id, from the ALTER TABLE migrateAttachedLegacyData
	// applies to the attached legacy file before copying - not an error, and
	// not left NULL.
	var ruleID string
	if err := alStore.db.QueryRow(`SELECT rule_id FROM notification_queue WHERE id = 'queue-1'`).Scan(&ruleID); err != nil {
		t.Fatalf("read migrated notification_queue.rule_id: %v", err)
	}
	if ruleID != "" {
		t.Errorf("expected the pre-rule_id row's rule_id to default to empty, got %q", ruleID)
	}

	wpStore, err := newWebPushSubscriptionStore(newPath)
	if err != nil {
		t.Fatalf("newWebPushSubscriptionStore on migrated file: %v", err)
	}
	defer wpStore.Close()
	subs, err := wpStore.All()
	if err != nil || len(subs) != 1 || subs[0].Endpoint != "https://push.example/legacy" {
		t.Fatalf("expected the migrated push subscription to survive, got subs=%+v err=%v", subs, err)
	}

	poStore, err := newPluginOverridesStore(newPath)
	if err != nil {
		t.Fatalf("newPluginOverridesStore on migrated file: %v", err)
	}
	defer poStore.db.Close()
	hosts, _, ok, err := poStore.Get("plugins/tides/bom.wasm")
	if err != nil || !ok || len(hosts) != 1 || hosts[0] != "www.bom.gov.au" {
		t.Fatalf("expected the migrated plugin override to survive, got hosts=%v ok=%v err=%v", hosts, ok, err)
	}

	// Row counts reported by the migration itself must match what actually
	// landed.
	if result.AssistantRowCounts["conversations"] != 1 {
		t.Errorf("expected 1 migrated conversation reported, got %d", result.AssistantRowCounts["conversations"])
	}
	if result.AssistantRowCounts["messages"] != 1 {
		t.Errorf("expected 1 migrated message reported, got %d", result.AssistantRowCounts["messages"])
	}
	if result.NearbyRowCounts["nearby_vessel_contacts"] != 1 {
		t.Errorf("expected 1 migrated sighting reported, got %d", result.NearbyRowCounts["nearby_vessel_contacts"])
	}
	if result.AlarmLogRowCounts["alarm_log"] != 1 {
		t.Errorf("expected 1 migrated alarm log entry reported, got %d", result.AlarmLogRowCounts["alarm_log"])
	}
	if result.AlarmLogRowCounts["notification_queue"] != 1 {
		t.Errorf("expected 1 migrated notification queue row reported, got %d", result.AlarmLogRowCounts["notification_queue"])
	}
	if result.WebPushRowCounts["push_subscriptions"] != 1 {
		t.Errorf("expected 1 migrated push subscription reported, got %d", result.WebPushRowCounts["push_subscriptions"])
	}
	if result.PluginOverridesRowCounts["plugin_overrides"] != 1 {
		t.Errorf("expected 1 migrated plugin override reported, got %d", result.PluginOverridesRowCounts["plugin_overrides"])
	}
	if result.PluginOverridesRowCounts["plugin_config_values"] != 0 {
		t.Errorf("expected 0 migrated plugin config values (the legacy file predated that table), got %d", result.PluginOverridesRowCounts["plugin_config_values"])
	}
}

// ── renameLegacyToMigrated ───────────────────────────────────────────────

func TestRenameLegacyToMigrated_RenamesWALAndSHMSidecarsWhenPresent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "assistant.sqlite")
	mustWriteFile(t, path, "main db content")
	mustWriteFile(t, path+"-wal", "wal sidecar content")
	mustWriteFile(t, path+"-shm", "shm sidecar content")

	if err := renameLegacyToMigrated(path); err != nil {
		t.Fatalf("renameLegacyToMigrated: %v", err)
	}

	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("expected %s to be gone, stat err = %v", p, err)
		}
	}
	for _, p := range []string{path + ".migrated", path + "-wal.migrated", path + "-shm.migrated"} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to exist: %v", p, err)
		}
	}
}

func TestRenameLegacyToMigrated_NoSidecarsIsFine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nearby-contacts.sqlite")
	mustWriteFile(t, path, "main db content")

	if err := renameLegacyToMigrated(path); err != nil {
		t.Fatalf("renameLegacyToMigrated: %v", err)
	}
	if _, err := os.Stat(path + ".migrated"); err != nil {
		t.Fatalf("expected %s.migrated to exist: %v", path, err)
	}
	if _, err := os.Stat(path + "-wal.migrated"); !os.IsNotExist(err) {
		t.Errorf("expected no -wal.migrated when there was never a -wal sidecar, stat err = %v", err)
	}
	if _, err := os.Stat(path + "-shm.migrated"); !os.IsNotExist(err) {
		t.Errorf("expected no -shm.migrated when there was never a -shm sidecar, stat err = %v", err)
	}
}

func assertJournalModeWAL(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open %s to check journal_mode: %v", path, err)
	}
	defer db.Close()
	var journalMode string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("expected %s to be in WAL mode, got %q", path, journalMode)
	}
}
