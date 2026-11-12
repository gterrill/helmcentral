package main

import (
	"path/filepath"
	"testing"
)

// An Inbox table made before Mate sorting gains its new columns when the store
// opens, and the items already waiting keep reading.
func TestInboxStoreUpgradesAnOlderTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.sqlite")
	db, err := openHelmcentralDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE inbox_items (
		id TEXT PRIMARY KEY,
		sha256 TEXT NOT NULL UNIQUE,
		filename TEXT NOT NULL,
		mime TEXT NOT NULL,
		size_bytes INTEGER NOT NULL,
		capture_ms INTEGER,
		bearing_deg REAL,
		phone_lat REAL,
		phone_lon REAL,
		fingerprint TEXT NOT NULL DEFAULT '',
		sharpness REAL NOT NULL DEFAULT 0,
		created_ms INTEGER NOT NULL,
		source_sha256 TEXT NOT NULL DEFAULT '',
		has_thumb INTEGER NOT NULL DEFAULT 0,
		filing_ms INTEGER
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO inbox_items (id, sha256, filename, mime, size_bytes, bearing_deg, created_ms) VALUES ('old1', 'aa', 'a.jpg', 'image/jpeg', 10, 80, 1)`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	store, err := newInboxStore(path)
	if err != nil {
		t.Fatalf("opening the older table: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	it, err := store.Get("old1")
	if err != nil {
		t.Fatal(err)
	}
	if it.SuggestedKind != "" || it.ClassifyError != "" || it.BearingRef != "" {
		t.Errorf("%+v", it)
	}

	// A second open finds the columns there already.
	again, err := newInboxStore(path)
	if err != nil {
		t.Fatalf("opening twice: %v", err)
	}
	_ = again.Close()

	if err := store.SetSuggestedKind("old1", "document"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetClassifyError("old1", "boom"); err != nil {
		t.Fatal(err)
	}
	it, _ = store.Get("old1")
	if it.SuggestedKind != "document" || it.ClassifyError != "boom" {
		t.Errorf("%+v", it)
	}
	// Neither a sorted nor a failed item is waiting to be sorted.
	if pending, err := store.Unclassified(); err != nil || len(pending) != 0 {
		t.Errorf("%v %v", pending, err)
	}
}
