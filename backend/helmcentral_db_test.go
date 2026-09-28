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

// ── all three stores sharing one file ───────────────────────────────────

func TestThreeStores_ShareOneFileConcurrently(t *testing.T) {
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

	// Read every write back, through the store that wrote it - proving all
	// three actually landed in the one shared file rather than three
	// separate ones that happened to share a name.
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

	// And confirm it is genuinely one file on disk, not three.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected shared file to exist: %v", err)
	}
}

// ── checkForLegacyDatabaseFiles (startup guard) ─────────────────────────

// legacyGuardTestPaths returns three legacy paths inside a fresh temp dir,
// at their DEFAULT filenames beside where helmcentral.sqlite would go -
// none of them created yet. Most tests use these; the one that exercises a
// legacy path outside this directory builds its own paths instead.
func legacyGuardTestPaths(t *testing.T) (legacyDocs, legacyAssistant, legacyNearby string) {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "documents.sqlite"),
		filepath.Join(dir, "assistant.sqlite"),
		filepath.Join(dir, "nearby-contacts.sqlite")
}

func TestCheckForLegacyDatabaseFiles_NoneFoundReturnsNil(t *testing.T) {
	legacyDocs, legacyAssistant, legacyNearby := legacyGuardTestPaths(t)
	if err := checkForLegacyDatabaseFiles(legacyDocs, legacyAssistant, legacyNearby); err != nil {
		t.Fatalf("expected nil when no legacy files exist, got %v", err)
	}
}

func TestCheckForLegacyDatabaseFiles_DetectsDocumentsSqlite(t *testing.T) {
	legacyDocs, legacyAssistant, legacyNearby := legacyGuardTestPaths(t)
	mustWriteFile(t, legacyDocs, "x")

	err := checkForLegacyDatabaseFiles(legacyDocs, legacyAssistant, legacyNearby)
	if err == nil {
		t.Fatal("expected an error when documents.sqlite is present")
	}
}

func TestCheckForLegacyDatabaseFiles_DetectsAssistantSqlite(t *testing.T) {
	legacyDocs, legacyAssistant, legacyNearby := legacyGuardTestPaths(t)
	mustWriteFile(t, legacyAssistant, "x")

	err := checkForLegacyDatabaseFiles(legacyDocs, legacyAssistant, legacyNearby)
	if err == nil {
		t.Fatal("expected an error when assistant.sqlite is present")
	}
}

func TestCheckForLegacyDatabaseFiles_DetectsNearbyContactsSqlite(t *testing.T) {
	legacyDocs, legacyAssistant, legacyNearby := legacyGuardTestPaths(t)
	mustWriteFile(t, legacyNearby, "x")

	err := checkForLegacyDatabaseFiles(legacyDocs, legacyAssistant, legacyNearby)
	if err == nil {
		t.Fatal("expected an error when nearby-contacts.sqlite is present")
	}
}

// TestCheckForLegacyDatabaseFiles_DetectsPathOutsideHelmcentralDir pins the
// review finding that the guard used to derive default filenames from
// helmcentralPath's own directory, while migrate-db resolves the three
// legacy paths independently through DOCUMENTS_DB_PATH/ASSISTANT_DB_PATH/
// NEARBY_CONTACTS_DB_PATH (legacyDocumentsDBPath and friends,
// backend/helmcentral_db.go) - which can point anywhere. An install that
// had set one of those env vars to a custom location would pass the old
// guard (nothing at the default path beside helmcentral.sqlite) and start
// up against an empty combined file while its real legacy data sat
// untouched at the custom path. Passing the exact paths in, the same ones
// migrate-db itself resolves and uses, closes that gap: this legacy
// documents.sqlite lives in a directory that has nothing to do with where
// helmcentral.sqlite would go, and the guard still has to catch it.
func TestCheckForLegacyDatabaseFiles_DetectsPathOutsideHelmcentralDir(t *testing.T) {
	legacyDir := t.TempDir()
	legacyDocs := filepath.Join(legacyDir, "custom-documents.sqlite")
	mustWriteFile(t, legacyDocs, "x")

	// The other two legacy paths, and helmcentral.sqlite's own directory,
	// are a completely different temp dir - only legacyDocs, off on its
	// own custom path, exists at all.
	helmcentralDir := t.TempDir()
	legacyAssistant := filepath.Join(helmcentralDir, "assistant.sqlite")
	legacyNearby := filepath.Join(helmcentralDir, "nearby-contacts.sqlite")

	err := checkForLegacyDatabaseFiles(legacyDocs, legacyAssistant, legacyNearby)
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
// path plus the three legacy paths inside it, none of them created yet.
func migrateTestPaths(t *testing.T) (newPath, legacyDocs, legacyAssistant, legacyNearby string) {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "helmcentral.sqlite"),
		filepath.Join(dir, "documents.sqlite"),
		filepath.Join(dir, "assistant.sqlite"),
		filepath.Join(dir, "nearby-contacts.sqlite")
}

func TestMigrateToHelmcentralDB_RefusesWhenHelmcentralAlreadyExists(t *testing.T) {
	newPath, legacyDocs, legacyAssistant, legacyNearby := migrateTestPaths(t)
	mustWriteFile(t, newPath, "already here")
	mustWriteFile(t, legacyDocs, "irrelevant")

	_, err := migrateToHelmcentralDB(newPath, legacyDocs, legacyAssistant, legacyNearby)
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

func TestMigrateToHelmcentralDB_RefusesWhenDocumentsMissing(t *testing.T) {
	newPath, legacyDocs, legacyAssistant, legacyNearby := migrateTestPaths(t)
	// legacyDocs deliberately not created.

	_, err := migrateToHelmcentralDB(newPath, legacyDocs, legacyAssistant, legacyNearby)
	if err == nil {
		t.Fatal("expected an error when documents.sqlite does not exist")
	}
	if _, statErr := os.Stat(newPath); statErr == nil {
		t.Fatal("expected no helmcentral.sqlite to be created on refusal")
	}
}

func TestMigrateToHelmcentralDB_RefusesWhenHotJournalPresent(t *testing.T) {
	newPath, legacyDocs, legacyAssistant, legacyNearby := migrateTestPaths(t)
	mustCreateLegacyDocumentsDB(t, legacyDocs)
	mustWriteFile(t, legacyDocs+"-journal", "hot journal")

	_, err := migrateToHelmcentralDB(newPath, legacyDocs, legacyAssistant, legacyNearby)
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

// TestMigrateToHelmcentralDB_FailureAfterRenameRestoresDocumentsFile pins the
// review finding that a failure partway through migration - here, the
// nearby-contacts copy hitting a legacy file with a missing column - must
// not leave the install permanently stuck: documents.sqlite had already
// been renamed to helmcentral.sqlite before the failure, so every rerun
// would otherwise refuse at the very first check (helmcentral.sqlite
// already exists) with no documents.sqlite left to migrate from.
func TestMigrateToHelmcentralDB_FailureAfterRenameRestoresDocumentsFile(t *testing.T) {
	newPath, legacyDocs, legacyAssistant, legacyNearby := migrateTestPaths(t)
	mustCreateLegacyDocumentsDB(t, legacyDocs)
	mustCreateLegacyAssistantDB(t, legacyAssistant)
	mustCreateLegacyNearbyDBMissingColumn(t, legacyNearby)

	_, err := migrateToHelmcentralDB(newPath, legacyDocs, legacyAssistant, legacyNearby)
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

	result, err := migrateToHelmcentralDB(newPath, legacyDocs, legacyAssistant, legacyNearby)
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
	newPath, legacyDocs, legacyAssistant, legacyNearby := migrateTestPaths(t)
	mustCreateLegacyDocumentsDB(t, legacyDocs)
	mustCreateLegacyAssistantDB(t, legacyAssistant)
	// legacyNearby deliberately left absent - keeps this test scoped to the
	// one failure under test.

	// Force the assistant.sqlite -> assistant.sqlite.migrated rename to
	// fail: pre-create a directory at the destination, so os.Rename errors.
	if err := os.Mkdir(legacyAssistant+".migrated", 0o755); err != nil {
		t.Fatalf("mkdir blocker: %v", err)
	}

	result, err := migrateToHelmcentralDB(newPath, legacyDocs, legacyAssistant, legacyNearby)
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
	newPath, legacyDocs, legacyAssistant, legacyNearby := migrateTestPaths(t)
	mustCreateLegacyDocumentsDB(t, legacyDocs)

	result, err := migrateToHelmcentralDB(newPath, legacyDocs, legacyAssistant, legacyNearby)
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

func TestMigrateToHelmcentralDB_HappyPathAllThreeFiles(t *testing.T) {
	newPath, legacyDocs, legacyAssistant, legacyNearby := migrateTestPaths(t)
	mustCreateLegacyDocumentsDB(t, legacyDocs)
	mustCreateLegacyAssistantDB(t, legacyAssistant)
	mustCreateLegacyNearbyDB(t, legacyNearby)

	result, err := migrateToHelmcentralDB(newPath, legacyDocs, legacyAssistant, legacyNearby)
	if err != nil {
		t.Fatalf("migrateToHelmcentralDB: %v", err)
	}

	if !result.AssistantMigrated {
		t.Error("expected AssistantMigrated to be true")
	}
	if !result.NearbyMigrated {
		t.Error("expected NearbyMigrated to be true")
	}

	// documents.sqlite is gone (renamed to helmcentral.sqlite); assistant and
	// nearby-contacts are gone from their own names, renamed to .migrated,
	// never deleted outright. (renameLegacyToMigrated's own handling of a
	// surviving -wal/-shm sidecar is covered directly, in isolation, by
	// TestRenameLegacyToMigrated_RenamesWALAndSHMSidecarsWhenPresent below -
	// not here, since DETACHing a WAL-mode attached database, the step
	// immediately before this rename, always checkpoints and clears its own
	// -wal/-shm as a side effect when (as here) nothing else still has it
	// open, so there is never really one left over to plant for this test
	// to find.)
	for _, p := range []string{legacyDocs, legacyAssistant, legacyNearby} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("expected %s to no longer exist, stat err = %v", p, err)
		}
	}
	for _, p := range []string{legacyAssistant + ".migrated", legacyNearby + ".migrated"} {
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
