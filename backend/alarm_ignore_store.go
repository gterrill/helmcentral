package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

/*
The two operator ignore lists, both in helmcentral.sqlite:

  - ignored_sensors: identifiers the sensor-health checks skip
    (alarm_ignored_sensors.go).
  - ignored_alarms: network alarm kinds hidden everywhere
    (alarm_ignored_alarms.go).

Each row keeps the moment the operator ignored it, so Settings can show how
long a thing has been hidden. Both lists are read on hot paths -- the anomaly
tick, and every alarm list -- so the rows are held in memory and the database
is only touched by a change.
*/

// ignoreList names one of the two tables.
type ignoreList struct {
	table  string
	column string
}

var (
	ignoredSensorsList = ignoreList{table: "ignored_sensors", column: "identifier"}
	ignoredAlarmsList  = ignoreList{table: "ignored_alarms", column: "kind"}
)

// ignoreRow is a cached row: when it was ignored and its insertion order.
type ignoreRow struct {
	at  time.Time
	seq int
}

// ignoreEntry is one ignored key and when the operator ignored it.
type ignoreEntry struct {
	Key       string
	IgnoredAt time.Time
}

type alarmIgnoreStore struct {
	mu      sync.RWMutex
	db      *sql.DB
	path    string
	cached  map[string]map[string]ignoreRow // table -> key -> row
	nextSeq int
}

// globalIgnoreStore is opened once in main(), beside the alarm log store.
var globalIgnoreStore *alarmIgnoreStore

func createAlarmIgnoreSchema(db *sql.DB) error {
	for _, list := range []ignoreList{ignoredSensorsList, ignoredAlarmsList} {
		if _, err := db.Exec(fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s (%s TEXT PRIMARY KEY, ignored_at INTEGER NOT NULL)`,
			list.table, list.column)); err != nil {
			return fmt.Errorf("create %s table: %w", list.table, err)
		}
	}
	return nil
}

func newAlarmIgnoreStore(dbPath string) (*alarmIgnoreStore, error) {
	db, err := openHelmcentralDB(dbPath)
	if err != nil {
		return nil, err
	}
	if err := createAlarmIgnoreSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	s := &alarmIgnoreStore{db: db, path: dbPath, cached: map[string]map[string]ignoreRow{}}
	for _, list := range []ignoreList{ignoredSensorsList, ignoredAlarmsList} {
		rows, err := db.Query(fmt.Sprintf(`SELECT %s, ignored_at FROM %s ORDER BY rowid`, list.column, list.table))
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("read %s: %w", list.table, err)
		}
		m := map[string]ignoreRow{}
		for rows.Next() {
			var key string
			var at int64
			if err := rows.Scan(&key, &at); err != nil {
				rows.Close()
				db.Close()
				return nil, fmt.Errorf("read %s: %w", list.table, err)
			}
			s.nextSeq++
			m[key] = ignoreRow{at: time.Unix(at, 0).UTC(), seq: s.nextSeq}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("read %s: %w", list.table, err)
		}
		s.cached[list.table] = m
	}
	return s, nil
}

func (s *alarmIgnoreStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// add ignores key at the given time. Adding a key that is already ignored
// keeps its original date and is not an error.
func (s *alarmIgnoreStore) add(list ignoreList, key string, at time.Time) error {
	if s == nil {
		return fmt.Errorf("ignore lists are not available")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.cached[list.table][key]; ok {
		return nil
	}
	if _, err := s.db.Exec(
		fmt.Sprintf(`INSERT OR IGNORE INTO %s (%s, ignored_at) VALUES (?, ?)`, list.table, list.column),
		key, at.UTC().Unix()); err != nil {
		return fmt.Errorf("ignore %s: %w", key, err)
	}
	s.nextSeq++
	s.cached[list.table][key] = ignoreRow{at: time.Unix(at.UTC().Unix(), 0).UTC(), seq: s.nextSeq}
	return nil
}

// remove stops ignoring key. Removing a key that was never ignored is not an
// error.
func (s *alarmIgnoreStore) remove(list ignoreList, key string) error {
	if s == nil {
		return fmt.Errorf("ignore lists are not available")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.db.Exec(
		fmt.Sprintf(`DELETE FROM %s WHERE %s = ?`, list.table, list.column), key); err != nil {
		return fmt.Errorf("restore %s: %w", key, err)
	}
	delete(s.cached[list.table], key)
	return nil
}

// entries returns the list in the order the entries were added.
func (s *alarmIgnoreStore) entries(list ignoreList) []ignoreEntry {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows := make([]ignoreRow, 0, len(s.cached[list.table]))
	keys := map[int]string{}
	for key, row := range s.cached[list.table] {
		rows = append(rows, row)
		keys[row.seq] = key
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].seq < rows[j].seq })
	out := make([]ignoreEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, ignoreEntry{Key: keys[row.seq], IgnoredAt: row.at})
	}
	return out
}

// set returns the keys as a set.
func (s *alarmIgnoreStore) set(list ignoreList) map[string]bool {
	if s == nil {
		return map[string]bool{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make(map[string]bool, len(s.cached[list.table]))
	for key := range s.cached[list.table] {
		out[key] = true
	}
	return out
}

func (s *alarmIgnoreStore) has(list ignoreList, key string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.cached[list.table][key]
	return ok
}

// legacyIgnoredSensorsField is the one field alarm-rules.json used to carry
// the sensor ignore list in. It exists only for moveLegacyIgnoredSensors and
// can be deleted, with that function, in a later release.
type legacyIgnoredSensorsField struct {
	IgnoredSensors []string `json:"ignored_sensors"`
}

// moveLegacyIgnoredSensors moves the sensor ignore list out of
// alarm-rules.json into the ignored_sensors table, dating each entry at now,
// then rewrites the file without the field. The insert commits before the
// rewrite and is idempotent, so a crash between them repeats the move on the
// next boot and loses nothing. It returns how many entries it found.
func moveLegacyIgnoredSensors(s *alarmIgnoreStore, rulesPath string, now time.Time) (int, error) {
	data, err := os.ReadFile(rulesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("reading alarm rules for the ignored-sensor move: %w", err)
	}
	var legacy legacyIgnoredSensorsField
	if len(data) > 0 {
		if err := json.Unmarshal(data, &legacy); err != nil {
			return 0, fmt.Errorf("parsing alarm rules for the ignored-sensor move: %w", err)
		}
	}
	var moved []string
	for _, id := range legacy.IgnoredSensors {
		if id = strings.TrimSpace(id); id != "" {
			moved = append(moved, id)
		}
	}
	if len(moved) == 0 {
		return 0, nil
	}

	for _, id := range moved {
		if err := s.add(ignoredSensorsList, id, now); err != nil {
			return 0, err
		}
	}

	var file map[string]json.RawMessage
	if err := json.Unmarshal(data, &file); err != nil {
		return 0, fmt.Errorf("parsing alarm rules for the ignored-sensor move: %w", err)
	}
	delete(file, "ignored_sensors")
	alarmRulesMu.Lock()
	defer alarmRulesMu.Unlock()
	if err := writeJSONFileAtomic(rulesPath, file); err != nil {
		return 0, fmt.Errorf("rewriting alarm rules after the ignored-sensor move: %w", err)
	}
	log.Printf("alarm ignore lists: moved %d ignored sensor(s) from alarm-rules.json to helmcentral.sqlite", len(moved))
	return len(moved), nil
}
