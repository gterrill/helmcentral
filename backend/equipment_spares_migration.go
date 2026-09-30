package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
)

// equipmentTableBody is the equipment table's column list and constraints.
// documentStoreSchema builds the table from it on a fresh database, and
// rebuildEquipmentForSpares builds its replacement from the same text, so the
// two can never drift apart.
//
// part_number and required_quantity arrived with the spares stock fields, and
// quantity's CHECK went from >= 1 to >= 0 at the same time: a spare that has
// run out is still a spare. required_quantity is nullable (nobody has said how
// many the boat should carry) and "out of stock" is derived from on hand below
// it, never stored.
const equipmentTableBody = `(
		id              TEXT PRIMARY KEY,
		name            TEXT NOT NULL,
		category        TEXT NOT NULL CHECK (category IN ('mechanical','general')),
		system          TEXT NOT NULL DEFAULT 'other' CHECK (system IN ('propulsion','electrical','water','fuel','bilge','anchoring','safety','hvac','navigation','appliances','structure','other')),
		manufacturer    TEXT NOT NULL DEFAULT '',
		model           TEXT NOT NULL DEFAULT '',
		serial          TEXT NOT NULL DEFAULT '',
		part_number     TEXT NOT NULL DEFAULT '',
		quantity        INTEGER NOT NULL DEFAULT 1 CHECK (quantity >= 0),
		required_quantity INTEGER CHECK (required_quantity IS NULL OR required_quantity >= 0),
		status          TEXT NOT NULL DEFAULT 'deployed' CHECK (status IN ('deployed','stored')),
		zone_id         TEXT REFERENCES inventory_zones(id) ON DELETE RESTRICT,
		bin_id          TEXT REFERENCES inventory_bins(id) ON DELETE RESTRICT,
		location_detail TEXT NOT NULL DEFAULT '',
		install_date    TEXT NOT NULL DEFAULT '',
		hour_meter_path TEXT NOT NULL DEFAULT '',
		profile_id      TEXT NOT NULL DEFAULT '',
		aliases_json    TEXT NOT NULL DEFAULT '[]',
		verified_aboard INTEGER NOT NULL DEFAULT 0 CHECK (verified_aboard IN (0,1)),
		notes           TEXT NOT NULL DEFAULT '',
		created_at      INTEGER NOT NULL,
		updated_at      INTEGER NOT NULL
	)`

// equipmentIndexes are the indexes documentStoreSchema creates on equipment.
// A table rebuild drops them with the old table, so the rebuild recreates them
// from this same list.
var equipmentIndexes = []string{
	`CREATE INDEX IF NOT EXISTS equipment_system ON equipment (system)`,
	`CREATE INDEX IF NOT EXISTS equipment_category ON equipment (category)`,
	`CREATE INDEX IF NOT EXISTS equipment_zone_id ON equipment (zone_id)`,
	`CREATE INDEX IF NOT EXISTS equipment_name ON equipment (lower(name))`,
}

// equipmentColumnsBeforeSpares are the columns a database made before the
// spares fields has; the rebuild copies exactly these and lets the two new
// ones take their defaults.
const equipmentColumnsBeforeSpares = `id, name, category, system, manufacturer, model, serial, quantity, status,
	zone_id, bin_id, location_detail, install_date, hour_meter_path, profile_id,
	aliases_json, verified_aboard, notes, created_at, updated_at`

// rebuildEquipmentForSpares upgrades an equipment table made before the
// spares fields. SQLite cannot alter a CHECK constraint, so the table is
// rebuilt the way SQLite's own documentation prescribes: foreign keys off
// (a PRAGMA that only takes effect outside a transaction, hence the pinned
// connection), then in one transaction create the new table, copy the rows,
// drop the old one, rename the new one into place and recreate the indexes,
// and finally run foreign_key_check and refuse to commit if it reports
// anything. Every table that points at equipment (documents links, maintenance
// rules, log entries, parts used, meter resets) keeps pointing at it by name,
// so it needs no change.
//
// Whether a rebuild is needed is read from the table's own SQL in
// sqlite_master: a table that already has required_quantity is current and
// this returns at once, so it runs once per database and every later open is
// a single SELECT.
//
// A database that already has a dangling reference fails here, loudly,
// instead of having it silently carried over or dropped; AGENTS.md's
// fail-fast rule over a quiet repair.
func rebuildEquipmentForSpares(db *sql.DB) error {
	var ddl string
	err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'equipment'`).Scan(&ddl)
	if err != nil {
		return fmt.Errorf("rebuild equipment: read table definition: %w", err)
	}
	if strings.Contains(ddl, "required_quantity") {
		return nil
	}

	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("rebuild equipment: take connection: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("rebuild equipment: foreign keys off: %w", err)
	}
	rebuildErr := rebuildEquipmentTx(ctx, conn)

	// Whatever happened, this connection goes back to the pool with foreign
	// keys enforced again; if that cannot be confirmed the connection is
	// discarded rather than reused with them off.
	restoreErr := restoreForeignKeys(ctx, conn)
	if rebuildErr != nil {
		return rebuildErr
	}
	return restoreErr
}

func rebuildEquipmentTx(ctx context.Context, conn *sql.Conn) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("rebuild equipment: begin: %w", err)
	}
	defer tx.Rollback()

	steps := []string{
		`CREATE TABLE equipment_new ` + equipmentTableBody,
		`INSERT INTO equipment_new (` + equipmentColumnsBeforeSpares + `) SELECT ` + equipmentColumnsBeforeSpares + ` FROM equipment`,
		`DROP TABLE equipment`,
		`ALTER TABLE equipment_new RENAME TO equipment`,
	}
	steps = append(steps, equipmentIndexes...)
	for _, stmt := range steps {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("rebuild equipment: %w", err)
		}
	}

	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("rebuild equipment: foreign key check: %w", err)
	}
	var problems []string
	for rows.Next() {
		var table, parent string
		var rowid sql.NullInt64
		var fkid int
		if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			rows.Close()
			return fmt.Errorf("rebuild equipment: foreign key check: %w", err)
		}
		problems = append(problems, fmt.Sprintf("%s row %d refers to a missing %s", table, rowid.Int64, parent))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("rebuild equipment: foreign key check: %w", err)
	}
	if len(problems) > 0 {
		return fmt.Errorf("rebuild equipment: foreign key violation, not committing (%d found, first: %s)", len(problems), problems[0])
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("rebuild equipment: commit: %w", err)
	}
	return nil
}

// restoreForeignKeys turns enforcement back on for conn and reads it back. A
// connection that cannot be confirmed is marked bad so database/sql closes it
// instead of handing it to the next caller with foreign keys off.
func restoreForeignKeys(ctx context.Context, conn *sql.Conn) error {
	var fk int
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err == nil {
		err = conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk)
		if err == nil && fk == 1 {
			return nil
		}
	}
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	return errors.New("rebuild equipment: could not turn foreign keys back on; connection discarded")
}
