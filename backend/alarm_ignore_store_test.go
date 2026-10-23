package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// withTempIgnoreStore installs an empty ignore store in a temp database and
// removes it when the test ends.
func withTempIgnoreStore(t *testing.T) *alarmIgnoreStore {
	t.Helper()
	store, err := newAlarmIgnoreStore(filepath.Join(t.TempDir(), "helmcentral.sqlite"))
	if err != nil {
		t.Fatalf("newAlarmIgnoreStore: %v", err)
	}
	prev := globalIgnoreStore
	globalIgnoreStore = store
	t.Cleanup(func() {
		globalIgnoreStore = prev
		store.Close()
	})
	return store
}

func TestIgnoreStoreKeepsTheDateAndSurvivesReopen(t *testing.T) {
	store := withTempIgnoreStore(t)
	first := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)

	if err := store.add(ignoredAlarmsList, "perpendicularPassed", first); err != nil {
		t.Fatal(err)
	}
	// Ignoring again is not an error and does not move the date.
	if err := store.add(ignoredAlarmsList, "perpendicularPassed", first.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	reopened, err := newAlarmIgnoreStore(store.path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got := reopened.entries(ignoredAlarmsList)
	if len(got) != 1 || got[0].Key != "perpendicularPassed" || !got[0].IgnoredAt.Equal(first) {
		t.Fatalf("got %+v", got)
	}
	if len(reopened.entries(ignoredSensorsList)) != 0 {
		t.Fatal("the two lists must be separate")
	}
}

func TestMoveLegacyIgnoredSensorsMovesAndRewritesTheFile(t *testing.T) {
	store := withTempIgnoreStore(t)
	rulesPath := filepath.Join(t.TempDir(), "alarm-rules.json")
	legacy := `{"rules":[{"id":"r1","label":"Bilge"}],"seeded_sets":["x"],"ignored_sensors":["a.b","venus:512"]}`
	if err := os.WriteFile(rulesPath, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)

	n, err := moveLegacyIgnoredSensors(store, rulesPath, now)
	if err != nil || n != 2 {
		t.Fatalf("moved %d, err %v", n, err)
	}
	entries := store.entries(ignoredSensorsList)
	if len(entries) != 2 || !entries[0].IgnoredAt.Equal(now) {
		t.Fatalf("entries: %+v", entries)
	}

	data, _ := os.ReadFile(rulesPath)
	var file map[string]json.RawMessage
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	if _, ok := file["ignored_sensors"]; ok {
		t.Fatalf("field still in the file: %s", data)
	}
	if _, ok := file["rules"]; !ok {
		t.Fatalf("rules lost: %s", data)
	}
	if _, ok := file["seeded_sets"]; !ok {
		t.Fatalf("seeded_sets lost: %s", data)
	}

	// A second boot finds nothing to move.
	if n, err := moveLegacyIgnoredSensors(store, rulesPath, now.Add(time.Hour)); err != nil || n != 0 {
		t.Fatalf("second run moved %d, err %v", n, err)
	}
}

func TestMoveLegacyIgnoredSensorsRepeatsAfterACrashWithoutLosingOrRedating(t *testing.T) {
	store := withTempIgnoreStore(t)
	rulesPath := filepath.Join(t.TempDir(), "alarm-rules.json")
	if err := os.WriteFile(rulesPath, []byte(`{"rules":[],"ignored_sensors":["a.b"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	early := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	// The crash: the rows were committed, the file was never rewritten.
	if err := store.add(ignoredSensorsList, "a.b", early); err != nil {
		t.Fatal(err)
	}

	if n, err := moveLegacyIgnoredSensors(store, rulesPath, early.Add(time.Hour)); err != nil || n != 1 {
		t.Fatalf("moved %d, err %v", n, err)
	}
	e := store.entries(ignoredSensorsList)
	if len(e) != 1 || !e[0].IgnoredAt.Equal(early) {
		t.Fatalf("entries: %+v", e)
	}
	data, _ := os.ReadFile(rulesPath)
	if string(data) == "" || json.Valid(data) == false {
		t.Fatalf("bad file %s", data)
	}
}

func TestMoveLegacyIgnoredSensorsMissingFileOrFieldIsNothingToDo(t *testing.T) {
	store := withTempIgnoreStore(t)
	dir := t.TempDir()
	if n, err := moveLegacyIgnoredSensors(store, filepath.Join(dir, "none.json"), time.Now()); err != nil || n != 0 {
		t.Fatalf("missing file: %d, %v", n, err)
	}
	p := filepath.Join(dir, "r.json")
	os.WriteFile(p, []byte(`{"rules":[]}`), 0o644)
	if n, err := moveLegacyIgnoredSensors(store, p, time.Now()); err != nil || n != 0 {
		t.Fatalf("no field: %d, %v", n, err)
	}
}

func TestMoveLegacyIgnoredSensorsFailsLoudlyOnUnreadableFile(t *testing.T) {
	store := withTempIgnoreStore(t)
	p := filepath.Join(t.TempDir(), "r.json")
	os.WriteFile(p, []byte(`{not json`), 0o644)
	if _, err := moveLegacyIgnoredSensors(store, p, time.Now()); err == nil {
		t.Fatal("expected an error")
	}
}

// Two senders with the same name are numbered by list order, so the order has
// to be the order they were added, including after the move from the rules file.
func TestIgnoredSensorsKeepInsertionOrderThroughMigrationAndReopen(t *testing.T) {
	store := withTempIgnoreStore(t)
	rulesPath := filepath.Join(t.TempDir(), "alarm-rules.json")
	if err := os.WriteFile(rulesPath, []byte(`{"rules":[],"ignored_sensors":["b.path","a.path","c.path"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := moveLegacyIgnoredSensors(store, rulesPath, time.Now()); err != nil {
		t.Fatal(err)
	}
	check := func(s *alarmIgnoreStore) {
		t.Helper()
		var keys []string
		for _, e := range s.entries(ignoredSensorsList) {
			keys = append(keys, e.Key)
		}
		if strings.Join(keys, ",") != "b.path,a.path,c.path" {
			t.Fatalf("order: %v", keys)
		}
	}
	check(store)
	reopened, err := newAlarmIgnoreStore(store.path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	check(reopened)
}
