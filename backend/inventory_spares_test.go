package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// Spares need three things the equipment table did not have: a part number,
// a required quantity (so "out of stock" is derived, not stored), and a
// quantity that may be zero. SQLite cannot alter a CHECK constraint, so a
// database made before this change has to have its equipment table rebuilt;
// these tests cover the fresh schema, the rebuild, and the API.

func TestEquipmentSpares_RoundTripsPartNumberAndRequiredQuantity(t *testing.T) {
	store := newTestDocumentStore(t)

	req := 4
	item, err := store.CreateEquipment(equipmentItem{
		Name: "Racor Seal Kit", PartNumber: "  21669 ", Quantity: 0, RequiredQuantity: &req, Status: "stored",
	})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	if item.PartNumber != "21669" {
		t.Fatalf("part number should be trimmed, got %q", item.PartNumber)
	}
	if item.Quantity != 0 {
		t.Fatalf("a spare may be at zero on hand, got %d", item.Quantity)
	}
	if item.RequiredQuantity == nil || *item.RequiredQuantity != 4 {
		t.Fatalf("required quantity = %v", item.RequiredQuantity)
	}

	got, err := store.GetEquipment(item.ID)
	if err != nil {
		t.Fatalf("GetEquipment: %v", err)
	}
	if got.PartNumber != "21669" || got.Quantity != 0 || got.RequiredQuantity == nil || *got.RequiredQuantity != 4 {
		t.Fatalf("round trip = %+v", got)
	}

	// Clearing the required quantity is a whole-record replace with nil.
	got.RequiredQuantity = nil
	updated, err := store.UpdateEquipment(got.ID, got)
	if err != nil {
		t.Fatalf("UpdateEquipment: %v", err)
	}
	if updated.RequiredQuantity != nil {
		t.Fatalf("required quantity should clear to nil, got %v", *updated.RequiredQuantity)
	}
	if updated.Quantity != 0 {
		t.Fatalf("update must not turn a zero quantity into one, got %d", updated.Quantity)
	}
}

func TestEquipmentSpares_RejectsNegativeQuantities(t *testing.T) {
	store := newTestDocumentStore(t)

	if _, err := store.CreateEquipment(equipmentItem{Name: "Bad", Quantity: -1}); !errors.Is(err, errEquipmentQuantityInvalid) {
		t.Fatalf("negative quantity: got %v", err)
	}
	neg := -2
	if _, err := store.CreateEquipment(equipmentItem{Name: "Bad", Quantity: 1, RequiredQuantity: &neg}); !errors.Is(err, errEquipmentQuantityInvalid) {
		t.Fatalf("negative required quantity: got %v", err)
	}

	ok, err := store.CreateEquipment(equipmentItem{Name: "Fine", Quantity: 1})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	if _, err := store.UpdateEquipment(ok.ID, equipmentItem{Name: "Fine", Quantity: -1}); !errors.Is(err, errEquipmentQuantityInvalid) {
		t.Fatalf("update negative quantity: got %v", err)
	}
}

// The schema itself, not just the Go checks, refuses a negative.
func TestEquipmentSpares_SchemaChecksAreTheLastLineOfDefence(t *testing.T) {
	store := newTestDocumentStore(t)
	insert := func(qty, req string) error {
		_, err := store.db.Exec(`INSERT INTO equipment (id, name, category, quantity, required_quantity, created_at, updated_at)
			VALUES (lower(hex(randomblob(8))), 'x', 'general', ` + qty + `, ` + req + `, 0, 0)`)
		return err
	}
	if err := insert("0", "NULL"); err != nil {
		t.Fatalf("quantity 0 must be accepted: %v", err)
	}
	if err := insert("1", "0"); err != nil {
		t.Fatalf("required 0 must be accepted: %v", err)
	}
	if err := insert("-1", "NULL"); err == nil {
		t.Fatalf("quantity -1 must be rejected by the schema")
	}
	if err := insert("1", "-1"); err == nil {
		t.Fatalf("required -1 must be rejected by the schema")
	}
}

// oldEquipmentDDL is the equipment table as it shipped before spares: no
// part_number, no required_quantity, quantity CHECK (quantity >= 1).
const oldEquipmentDDL = `CREATE TABLE IF NOT EXISTS equipment (
		id              TEXT PRIMARY KEY,
		name            TEXT NOT NULL,
		category        TEXT NOT NULL CHECK (category IN ('mechanical','general')),
		system          TEXT NOT NULL DEFAULT 'other' CHECK (system IN ('propulsion','electrical','water','fuel','bilge','anchoring','safety','hvac','navigation','appliances','structure','other')),
		manufacturer    TEXT NOT NULL DEFAULT '',
		model           TEXT NOT NULL DEFAULT '',
		serial          TEXT NOT NULL DEFAULT '',
		quantity        INTEGER NOT NULL DEFAULT 1 CHECK (quantity >= 1),
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

// openPreSparesDatabase builds a database file as a boat running the
// previous release would have it: every table the store creates, with the
// old equipment definition, plus rows in equipment and in every table that
// points at it.
func openPreSparesDatabase(t *testing.T) (path string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "helmcentral.sqlite")
	db, err := openHelmcentralDB(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	replaced := false
	for _, stmt := range documentStoreSchema {
		if strings.HasPrefix(stmt, "CREATE TABLE IF NOT EXISTS equipment (") {
			stmt = oldEquipmentDDL
			replaced = true
		}
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("schema: %v\n%s", err, stmt)
		}
	}
	if !replaced {
		t.Fatalf("did not find the equipment table in the schema")
	}
	if err := applyDocumentStoreMigrations(db); err != nil {
		t.Fatalf("migrations: %v", err)
	}

	for _, q := range []string{
		`INSERT INTO inventory_zones (id, name, sort_index, created_at, updated_at) VALUES ('z1', 'Salon', 0, 1, 1)`,
		`INSERT INTO inventory_bins (id, zone_id, code, name, sort_index, created_at, updated_at) VALUES ('b1', 'z1', 'SAL-01', 'Salon bin', 0, 1, 1)`,
		`INSERT INTO equipment (id, name, category, quantity, zone_id, bin_id, aliases_json, notes, created_at, updated_at)
			VALUES ('e1', 'Generator', 'general', 2, 'z1', 'b1', '["genset"]', 'keep dry', 100, 200)`,
		`INSERT INTO equipment (id, name, category, quantity, created_at, updated_at) VALUES ('e2', 'Anchor', 'general', 1, 100, 200)`,
		`INSERT INTO documents (id, sha256, filename, created_at, updated_at) VALUES ('d1', 'aa', 'manual.pdf', 1, 1)`,
		`INSERT INTO equipment_documents (equipment_id, document_id, source, created_at) VALUES ('e1', 'd1', 'operator', 1)`,
		`INSERT INTO maintenance_rules (id, equipment_id, description, created_at, updated_at) VALUES ('r1', 'e1', 'oil', 1, 1)`,
		`INSERT INTO maintenance_log_entries (id, equipment_id, performed_at, kind, created_at, updated_at) VALUES ('l1', 'e1', '2026-01-01', 'maintenance', 1, 1)`,
		`INSERT INTO maintenance_log_parts (id, log_entry_id, equipment_id, part_name, quantity) VALUES ('p1', 'l1', 'e2', 'Anchor', 1)`,
		`INSERT INTO hour_meter_resets (id, equipment_id, old_reading, new_reading, changed_at, created_at) VALUES ('h1', 'e1', 1, 2, '2026-01-01', 1)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed: %v\n%s", err, q)
		}
	}
	return path
}

func TestEquipmentSpares_RebuildKeepsRowsAndEveryReference(t *testing.T) {
	path := openPreSparesDatabase(t)

	store, err := newDocumentStore(path)
	if err != nil {
		t.Fatalf("newDocumentStore on an old database: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	gen, err := store.GetEquipment("e1")
	if err != nil {
		t.Fatalf("GetEquipment e1: %v", err)
	}
	if gen.Name != "Generator" || gen.Quantity != 2 || gen.Notes != "keep dry" ||
		len(gen.Aliases) != 1 || gen.Aliases[0] != "genset" ||
		gen.ZoneID == nil || *gen.ZoneID != "z1" || gen.BinID == nil || *gen.BinID != "b1" ||
		gen.CreatedAt.Unix() != 100 || gen.UpdatedAt.Unix() != 200 {
		t.Fatalf("row not preserved: %+v", gen)
	}
	if gen.PartNumber != "" || gen.RequiredQuantity != nil {
		t.Fatalf("new columns should start empty: %+v", gen)
	}
	if gen.LinkCount != 1 {
		t.Fatalf("equipment_documents link lost: link_count = %d", gen.LinkCount)
	}

	for table, q := range map[string]string{
		"equipment":             `SELECT COUNT(*) FROM equipment`,
		"equipment_documents":   `SELECT COUNT(*) FROM equipment_documents WHERE equipment_id = 'e1'`,
		"maintenance_rules":     `SELECT COUNT(*) FROM maintenance_rules WHERE equipment_id = 'e1'`,
		"maintenance_log":       `SELECT COUNT(*) FROM maintenance_log_entries WHERE equipment_id = 'e1'`,
		"maintenance_log_parts": `SELECT COUNT(*) FROM maintenance_log_parts WHERE equipment_id = 'e2'`,
		"hour_meter_resets":     `SELECT COUNT(*) FROM hour_meter_resets WHERE equipment_id = 'e1'`,
	} {
		var n int
		if err := store.db.QueryRow(q).Scan(&n); err != nil {
			t.Fatalf("%s: %v", table, err)
		}
		want := 1
		if table == "equipment" {
			want = 2
		}
		if n != want {
			t.Errorf("%s: %d rows after the rebuild, want %d", table, n, want)
		}
	}

	// The references still point at the new table: deleting the equipment
	// cascades exactly as before.
	if _, err := store.DeleteEquipment("e1", false); err != nil {
		t.Fatalf("DeleteEquipment: %v", err)
	}
	var n int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM maintenance_rules`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rules should cascade on delete: n=%d err=%v", n, err)
	}

	// The rebuilt table takes the new values.
	if _, err := store.CreateEquipment(equipmentItem{Name: "Zero", Quantity: 0}); err != nil {
		t.Fatalf("quantity 0 after the rebuild: %v", err)
	}

	// Indexes came back with it.
	for _, idx := range []string{"equipment_system", "equipment_category", "equipment_zone_id", "equipment_name"} {
		var name string
		if err := store.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, idx).Scan(&name); err != nil {
			t.Errorf("index %s missing after the rebuild: %v", idx, err)
		}
	}

	// Foreign keys are back on for the store's own connection.
	var fk int
	if err := store.db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("foreign_keys = %d (err %v) after the rebuild, want 1", fk, err)
	}
}

func TestEquipmentSpares_RebuildRunsOnce(t *testing.T) {
	path := openPreSparesDatabase(t)

	first, err := newDocumentStore(path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	item, err := first.CreateEquipment(equipmentItem{Name: "Added after", Quantity: 0, PartNumber: "P1"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	first.Close()

	// A second open must not rebuild again: a rebuild copies only the old
	// columns, which would wipe the part number just written.
	second, err := newDocumentStore(path)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	t.Cleanup(func() { second.Close() })
	got, err := second.GetEquipment(item.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.PartNumber != "P1" {
		t.Fatalf("part number lost on reopen: %+v", got)
	}
}

func TestEquipmentSpares_RebuildRefusesADatabaseWithBrokenReferences(t *testing.T) {
	path := openPreSparesDatabase(t)

	// Plant a dangling reference the way a past bug or a manual edit could
	// have: foreign keys off, a log part pointing at nothing.
	db, err := openHelmcentralDB(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	if _, err := conn.ExecContext(context.Background(), `PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatalf("fk off: %v", err)
	}
	if _, err := conn.ExecContext(context.Background(), `INSERT INTO equipment_documents (equipment_id, document_id, source, created_at) VALUES ('ghost', 'd1', 'operator', 1)`); err != nil {
		t.Fatalf("plant: %v", err)
	}
	conn.Close()
	db.Close()

	if _, err := newDocumentStore(path); err == nil {
		t.Fatalf("expected the rebuild to fail on a foreign key violation, not commit over it")
	} else if !strings.Contains(err.Error(), "foreign key") {
		t.Fatalf("error should name the foreign key problem, got %v", err)
	}
}

// ── handlers ─────────────────────────────────────────────────────────────

func TestEquipmentHandlers_SpareFields(t *testing.T) {
	withTestDocumentStore(t)

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/equipment",
		`{"name":"Impeller Kit","part_number":"3974456","quantity":0,"required_quantity":3,"status":"stored"}`, "")
	if err := createEquipmentHandler(c); err != nil {
		t.Fatalf("create: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var wrapped struct {
		Item equipmentItem `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wrapped); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	it := wrapped.Item
	if it.PartNumber != "3974456" || it.Quantity != 0 || it.RequiredQuantity == nil || *it.RequiredQuantity != 3 {
		t.Fatalf("created = %+v", it)
	}
	// The wire keeps the keys even when empty (ADR 0115 section 7).
	if !strings.Contains(rec.Body.String(), `"part_number":"3974456"`) || !strings.Contains(rec.Body.String(), `"required_quantity":3`) {
		t.Fatalf("body = %s", rec.Body.String())
	}

	// Quantity omitted: still defaults to one, as before.
	c, rec = newDocumentEchoContext(http.MethodPost, "/api/inventory/equipment", `{"name":"Plain"}`, "")
	if err := createEquipmentHandler(c); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("create plain: %v %d", err, rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wrapped); err != nil || wrapped.Item.Quantity != 1 {
		t.Fatalf("omitted quantity should default to 1, got %+v (%v)", wrapped.Item, err)
	}
	if wrapped.Item.RequiredQuantity != nil || !strings.Contains(rec.Body.String(), `"required_quantity":null`) {
		t.Fatalf("required_quantity should be an explicit null: %s", rec.Body.String())
	}

	// Explicit negatives are 400s naming the field.
	for field, body := range map[string]string{
		"quantity":          `{"name":"X","quantity":-1}`,
		"required_quantity": `{"name":"X","required_quantity":-1}`,
	} {
		c, rec = newDocumentEchoContext(http.MethodPost, "/api/inventory/equipment", body, "")
		if err := createEquipmentHandler(c); err != nil {
			t.Fatalf("create: %v", err)
		}
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"field":"`+field+`"`) {
			t.Fatalf("%s: expected 400 naming the field, got %d %s", field, rec.Code, rec.Body.String())
		}
	}
}
