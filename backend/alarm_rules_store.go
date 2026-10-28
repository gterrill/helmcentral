package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

/*
Operator alarm rules in helmcentral.sqlite (ADR 0224).

Two tables, both in the record store beside equipment and maintenance:

  - alarm_rules: one row per stored rule. Structured fields (methods, notify)
    are JSON text; the threshold fields are plain columns. seeded_by holds the
    marker of the built-in set that created the rule, NULL for a rule the
    operator made. updated_at is the rule's version for changesets.
  - alarm_rule_seeded_sets: the markers of built-in sets already offered, so a
    set the operator deleted on purpose never comes back.

The engine reads alarmRulesState, an in-memory copy, once a second. Every
write goes through documentStore.writeAlarmRules, which runs the SQL in one
transaction and reloads the copy from the database before it lets go of the
store lock, so the copy is only ever a committed state. The *Tx functions
below take the transaction, so a changeset can call them inside RunTx; it then
calls refreshAlarmRules once the commit lands.

Derived rules (zone:, tank:) are never stored.
*/

var errAlarmRuleNotFound = errors.New("no such alarm rule")

// alarmSQLExecer is the part of *sql.Tx and *sql.DB the rule functions use.
type alarmSQLExecer interface {
	sqlQueryer
	Exec(query string, args ...any) (sql.Result, error)
}

func createAlarmRulesSchema(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS alarm_rules (
			id                     TEXT PRIMARY KEY,
			enabled                INTEGER NOT NULL,
			path                   TEXT NOT NULL,
			label                  TEXT NOT NULL,
			path_role              TEXT NOT NULL DEFAULT '',
			op                     TEXT NOT NULL,
			value                  REAL NOT NULL,
			hysteresis             REAL NOT NULL DEFAULT 0,
			dwell_seconds          INTEGER NOT NULL DEFAULT 0,
			stale_after_seconds    INTEGER NOT NULL DEFAULT 0,
			alarm_level_value      REAL,
			capacity_path          TEXT NOT NULL DEFAULT '',
			state                  TEXT NOT NULL,
			methods_json           TEXT NOT NULL DEFAULT '[]',
			notify_json            TEXT NOT NULL DEFAULT '[]',
			escalate_after_seconds INTEGER NOT NULL DEFAULT 0,
			seeded_by              TEXT,
			created_at             INTEGER NOT NULL,
			updated_at             INTEGER NOT NULL
		)`); err != nil {
		return fmt.Errorf("create alarm_rules table: %w", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS alarm_rule_seeded_sets (
			marker    TEXT PRIMARY KEY,
			seeded_at INTEGER NOT NULL
		)`); err != nil {
		return fmt.Errorf("create alarm_rule_seeded_sets table: %w", err)
	}
	return nil
}

const alarmRuleColumns = `id, enabled, path, label, path_role, op, value, hysteresis,
	dwell_seconds, stale_after_seconds, alarm_level_value, capacity_path, state,
	methods_json, notify_json, escalate_after_seconds, seeded_by, created_at, updated_at`

func alarmRuleStoreTime(t time.Time) int64 { return t.UTC().UnixNano() }

func alarmRuleFromStoreTime(n int64) time.Time { return time.Unix(0, n).UTC() }

func alarmJSONList(list []string) (string, error) {
	if list == nil {
		list = []string{}
	}
	b, err := json.Marshal(list)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func scanAlarmRule(row interface{ Scan(...any) error }) (alarmRule, error) {
	var (
		r                          alarmRule
		enabled                    int
		level                      sql.NullFloat64
		seeded                     sql.NullString
		methods, notify            string
		createdNanos, updatedNanos int64
	)
	if err := row.Scan(&r.ID, &enabled, &r.Path, &r.Label, &r.PathRole, &r.Op, &r.Value, &r.Hysteresis,
		&r.DwellSeconds, &r.StaleAfterSeconds, &level, &r.CapacityPath, &r.State,
		&methods, &notify, &r.EscalateAfterSeconds, &seeded, &createdNanos, &updatedNanos); err != nil {
		return alarmRule{}, err
	}
	r.Enabled = enabled != 0
	if level.Valid {
		v := level.Float64
		r.AlarmLevelValue = &v
	}
	r.SeededBy = seeded.String
	if err := json.Unmarshal([]byte(methods), &r.Methods); err != nil {
		return alarmRule{}, fmt.Errorf("alarm rule %s: methods: %w", r.ID, err)
	}
	if err := json.Unmarshal([]byte(notify), &r.Notify); err != nil {
		return alarmRule{}, fmt.Errorf("alarm rule %s: notify: %w", r.ID, err)
	}
	r.CreatedAt = alarmRuleFromStoreTime(createdNanos)
	r.UpdatedAt = alarmRuleFromStoreTime(updatedNanos)
	return r, nil
}

// alarmRuleArgs is the column values for r, in alarmRuleColumns order.
func alarmRuleArgs(r alarmRule) ([]any, error) {
	methods, err := alarmJSONList(r.Methods)
	if err != nil {
		return nil, fmt.Errorf("alarm rule %s: methods: %w", r.ID, err)
	}
	notify, err := alarmJSONList(r.Notify)
	if err != nil {
		return nil, fmt.Errorf("alarm rule %s: notify: %w", r.ID, err)
	}
	var level any
	if r.AlarmLevelValue != nil {
		level = *r.AlarmLevelValue
	}
	var seeded any
	if strings.TrimSpace(r.SeededBy) != "" {
		seeded = r.SeededBy
	}
	return []any{r.ID, boolToInt(r.Enabled), r.Path, r.Label, r.PathRole, r.Op, r.Value, r.Hysteresis,
		r.DwellSeconds, r.StaleAfterSeconds, level, r.CapacityPath, r.State,
		methods, notify, r.EscalateAfterSeconds, seeded,
		alarmRuleStoreTime(r.CreatedAt), alarmRuleStoreTime(r.UpdatedAt)}, nil
}

// insertAlarmRuleTx adds r as it stands: the caller has validated it and set
// its ID and timestamps. An id already in use is an error, never a replace.
func insertAlarmRuleTx(x alarmSQLExecer, r alarmRule) error {
	args, err := alarmRuleArgs(r)
	if err != nil {
		return err
	}
	if _, err := x.Exec(`INSERT INTO alarm_rules (`+alarmRuleColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, args...); err != nil {
		return fmt.Errorf("insert alarm rule %q: %w", r.Label, err)
	}
	return nil
}

// updateAlarmRuleTx overwrites the row under r.ID. The caller keeps the
// identity fields (id, created_at, seeded_by) from the stored rule.
func updateAlarmRuleTx(x alarmSQLExecer, r alarmRule) error {
	args, err := alarmRuleArgs(r)
	if err != nil {
		return err
	}
	// Everything after the id, then the id again for the WHERE.
	res, err := x.Exec(`UPDATE alarm_rules SET enabled = ?, path = ?, label = ?, path_role = ?, op = ?,
		value = ?, hysteresis = ?, dwell_seconds = ?, stale_after_seconds = ?, alarm_level_value = ?,
		capacity_path = ?, state = ?, methods_json = ?, notify_json = ?, escalate_after_seconds = ?,
		seeded_by = ?, created_at = ?, updated_at = ? WHERE id = ?`, append(args[1:], r.ID)...)
	if err != nil {
		return fmt.Errorf("update alarm rule %q: %w", r.Label, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errAlarmRuleNotFound
	}
	return nil
}

func deleteAlarmRuleTx(x alarmSQLExecer, id string) error {
	res, err := x.Exec(`DELETE FROM alarm_rules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete alarm rule: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errAlarmRuleNotFound
	}
	return nil
}

func alarmRuleByIDQ(q sqlQueryer, id string) (alarmRule, error) {
	r, err := scanAlarmRule(q.QueryRow(`SELECT `+alarmRuleColumns+` FROM alarm_rules WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return alarmRule{}, errAlarmRuleNotFound
	}
	return r, err
}

// listAlarmRulesQ returns every stored rule in the API's order.
func listAlarmRulesQ(q sqlQueryer) ([]alarmRule, error) {
	rows, err := q.Query(`SELECT ` + alarmRuleColumns + ` FROM alarm_rules`)
	if err != nil {
		return nil, fmt.Errorf("read alarm rules: %w", err)
	}
	defer rows.Close()
	var out []alarmRule
	for rows.Next() {
		r, err := scanAlarmRule(rows)
		if err != nil {
			return nil, fmt.Errorf("read alarm rules: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read alarm rules: %w", err)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func alarmSeededSetsQ(q sqlQueryer) ([]string, error) {
	rows, err := q.Query(`SELECT marker FROM alarm_rule_seeded_sets ORDER BY rowid`)
	if err != nil {
		return nil, fmt.Errorf("read alarm seeded sets: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, fmt.Errorf("read alarm seeded sets: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// recordAlarmSeededSetTx notes that a built-in set has been offered. Recording
// a marker twice is not an error.
func recordAlarmSeededSetTx(x alarmSQLExecer, marker string, now time.Time) error {
	if _, err := x.Exec(`INSERT OR IGNORE INTO alarm_rule_seeded_sets (marker, seeded_at) VALUES (?, ?)`,
		marker, alarmRuleStoreTime(now)); err != nil {
		return fmt.Errorf("record alarm seed marker %q: %w", marker, err)
	}
	return nil
}

// reloadAlarmRulesCache replaces the in-memory copy with what q holds.
func reloadAlarmRulesCache(q sqlQueryer) error {
	rules, err := listAlarmRulesQ(q)
	if err != nil {
		return err
	}
	sets, err := alarmSeededSetsQ(q)
	if err != nil {
		return err
	}
	next := make(map[string]*alarmRule, len(rules))
	for i := range rules {
		next[rules[i].ID] = &rules[i]
	}
	alarmRulesMu.Lock()
	alarmRulesState = next
	alarmRulesSeededSets = sets
	alarmRulesMu.Unlock()
	return nil
}

// writeAlarmRules runs fn in one transaction and, once it has committed,
// reloads the in-memory copy before the store lock is released, so two
// writers cannot leave the copy older than the database.
func (s *documentStore) writeAlarmRules(op string, fn func(tx *sql.Tx, now time.Time) error) error {
	s.lockForTx()
	defer s.mu.Unlock()
	if err := s.runTxLocked(op, fn); err != nil {
		return err
	}
	if err := reloadAlarmRulesCache(s.db); err != nil {
		return fmt.Errorf("%s: reload alarm rules after commit: %w", op, err)
	}
	return nil
}

// refreshAlarmRules reloads the in-memory copy. A changeset that wrote rules
// through RunTx calls it after the commit.
func refreshAlarmRules(s *documentStore) error {
	return s.Read(func(q sqlQueryer) error { return reloadAlarmRulesCache(q) })
}

// alarmRuleStore is the store the rules live in. A missing store is a boot
// ordering bug, so it is reported rather than worked around.
func alarmRuleStore() (*documentStore, error) {
	if globalDocumentStore == nil {
		return nil, errors.New("alarm rules: the record store is not open")
	}
	return globalDocumentStore, nil
}

// seedAlarmRuleSet offers a built-in set once: it adds every rule, stamped
// with marker in seeded_by, and records the marker, all in one transaction, so
// a failure leaves nothing half seeded. It does nothing when marker is already
// recorded. With fixedIDs the rules keep the ids they carry (the anomaly set);
// without, each gets a fresh one.
//
// A fixed-id rule that already exists with the same path and state is left as
// it is (the operator may have tuned it since, or a build before this one
// created it without a marker); one that exists as something else is an error.
func seedAlarmRuleSet(marker string, rules []alarmRule, fixedIDs bool) error {
	alarmRulesMu.RLock()
	for _, m := range alarmRulesSeededSets {
		if m == marker {
			alarmRulesMu.RUnlock()
			return nil
		}
	}
	alarmRulesMu.RUnlock()

	store, err := alarmRuleStore()
	if err != nil {
		return err
	}
	prepared := make([]alarmRule, len(rules))
	for i, rule := range rules {
		if fixedIDs && strings.TrimSpace(rule.ID) == "" {
			return fmt.Errorf("seeding %s rule %q: no fixed id", marker, rule.Label)
		}
		if err := validateAlarmRule(&rule); err != nil {
			return fmt.Errorf("seeding %s rule %q: %w", marker, rule.Label, err)
		}
		rule.SeededBy = marker
		prepared[i] = rule
	}
	return store.writeAlarmRules("seed alarm rules "+marker, func(tx *sql.Tx, now time.Time) error {
		sets, err := alarmSeededSetsQ(tx)
		if err != nil {
			return err
		}
		for _, m := range sets {
			if m == marker {
				return nil
			}
		}
		for _, rule := range prepared {
			if !fixedIDs {
				rule.ID = uuid.NewString()
			} else if existing, err := alarmRuleByIDQ(tx, rule.ID); err == nil {
				if existing.Path == rule.Path && existing.State == rule.State {
					continue
				}
				return fmt.Errorf("seeding %s rule %q: id %q is already in use by %q", marker, rule.Label, rule.ID, existing.Label)
			} else if !errors.Is(err, errAlarmRuleNotFound) {
				return err
			}
			rule.CreatedAt = now
			rule.UpdatedAt = now
			if err := insertAlarmRuleTx(tx, rule); err != nil {
				return fmt.Errorf("seeding %s: %w", marker, err)
			}
		}
		return recordAlarmSeededSetTx(tx, marker, now)
	})
}
