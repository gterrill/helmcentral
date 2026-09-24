package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// This file is the inventory-scoped half of documentStore
// (documents_store.go): zones, bins and equipment records (plan "Inventory:
// the equipment registry and locations", Phase 0), following the same
// per-topic file split as notes_store.go and manuals_store.go. Unlike a
// manual (a document_folders row wearing a flag) or a note (a documents row
// wearing a kind), a zone/bin/equipment record is genuinely its own table -
// there is no existing row shape to reuse - so this file carries full CRUD
// rather than a thin scoped extension.
//
// Sentinel errors below follow manuals_store.go's own idiom exactly: a
// plain error value, checked with errors.Is by documentErrorStatus
// (documents_handlers.go) and by this file's own tests, one sentinel per
// distinct condition a caller might need to branch on. The one exception is
// inventoryInUseError (the RESTRICT-in-use case): a bare sentinel can say
// THAT a zone or bin is in use, but the 409 body's "in_use" count
// (inventory_handlers.go's delete handlers) needs the actual number, so
// that case is a small struct which still answers errors.Is(err,
// errZoneInUse) / errors.Is(err, errBinInUse) via its own Is method - the
// classification and the detail travel together without the handler having
// to parse either the struct's Error() string or SQLite's.

var (
	// errZoneNotFound is returned by any method that reads or writes a
	// specific zone id that does not exist - as the target itself, or as a
	// zone_id referenced by CreateBin/CreateEquipment/UpdateEquipment.
	errZoneNotFound = errors.New("zone not found")

	// errZoneNameInvalid is returned when a zone name, after trimming, is
	// empty. There is no length/character restriction beyond that (unlike
	// validateFolderName's 120-char/no-"/" rule) - a zone name is a short
	// free-text label ("Engine room (stbd)"), not a path segment.
	errZoneNameInvalid = errors.New("zone name is required")

	// errZoneNameTaken is returned by CreateZone/UpdateZone when another
	// zone already has the same name case-insensitively - the unique index
	// on lower(name) (documents_store.go's schema) is what actually
	// enforces this; this sentinel is what a clean pre-check maps it to.
	errZoneNameTaken = errors.New("a zone with this name already exists")

	// errZoneInUse is returned by DeleteZone when a bin still lives in the
	// zone, or an equipment item still references it directly - the
	// classification half of inventoryInUseError below, which carries the
	// exact count.
	errZoneInUse = errors.New("zone is in use")

	// errBinNotFound is returned by any method that reads or writes a
	// specific bin id that does not exist - as the target itself, or as a
	// bin_id referenced by CreateEquipment/UpdateEquipment.
	errBinNotFound = errors.New("bin not found")

	// errBinCodeInvalid is returned when a bin code, after trimming, is
	// empty - a bin without a printable code is not a location an operator
	// can label anything with.
	errBinCodeInvalid = errors.New("bin code is required")

	// errBinCodeTaken is returned by CreateBin/UpdateBin when another bin
	// already has the same code case-insensitively - boat-wide, not scoped
	// to one zone (inventory_bins_code's own schema comment: a code is read
	// off a printed label without knowing which zone it names).
	errBinCodeTaken = errors.New("a bin with this code already exists")

	// errBinInUse is returned by DeleteBin when an equipment item still
	// references it - the classification half of inventoryInUseError.
	errBinInUse = errors.New("bin is in use")

	// errEquipmentNotFound is returned by any method that operates on a
	// specific equipment id that does not exist.
	errEquipmentNotFound = errors.New("equipment not found")

	// errEquipmentNameRequired is returned when an equipment name, after
	// trimming, is empty.
	errEquipmentNameRequired = errors.New("equipment name is required")

	// errEquipmentInvalidCategory/System/Status are returned when a caller
	// supplies a value outside the schema's own CHECK-constrained enum.
	// category has no default (a record has to say which kind it is), so a
	// blank value is rejected the same as a bogus one; system and status DO
	// have a default (applied by CreateEquipment/UpdateEquipment before this
	// check ever runs), so only a genuinely bogus non-blank value reaches
	// this sentinel for them. The schema's own CHECK constraints are the
	// last line of defense if a caller somehow reaches the INSERT/UPDATE
	// with something these checks missed - a real, loud failure rather than
	// silently written bad data, never expected to actually fire.
	errEquipmentInvalidCategory = errors.New("category must be mechanical or general")
	errEquipmentInvalidSystem   = errors.New("unknown equipment system")
	errEquipmentInvalidStatus   = errors.New("status must be deployed or stored")

	// errEquipmentLocationMismatch is returned by validateEquipmentLocation
	// when a caller supplies BOTH zone_id and bin_id and they disagree - the
	// bin belongs to a different zone than the one named. Plan's own
	// decision: "If both are supplied and disagree, that is a validation
	// error, not a silent fix" - CreateEquipment/UpdateEquipment never
	// quietly repoint the request to the bin's real zone.
	errEquipmentLocationMismatch = errors.New("bin does not belong to the given zone")
)

// inventoryInUseError is DeleteZone/DeleteBin's return value whenever other
// rows still reference the zone or bin being deleted. RESTRICT is what the
// schema actually enforces (inventory_bins.zone_id and equipment.zone_id/
// bin_id all reference their target ON DELETE RESTRICT), but SQLite's own
// constraint-violation error carries no usable count for the operator to
// act on - "FOREIGN KEY constraint failed" says nothing about how many
// items are in the way. Task instructions are explicit on this point:
// DeleteZone/DeleteBin pre-check with COUNT(*) inside the SAME transaction
// as the eventual DELETE, rather than attempting the delete first and
// trying to parse the driver's error string - a COUNT the caller already
// has full control over is a number that can be trusted; a parsed error
// string is coupled to whatever text this exact SQLite build happens to
// produce today.
//
// Is satisfies errors.Is against whichever sentinel (errZoneInUse or
// errBinInUse) this particular error wraps, so a caller that only cares
// about "is this a such-and-such-in-use error" can keep using errors.Is
// exactly like every other sentinel in this file; a caller that ALSO wants
// the count (inventory_handlers.go's delete handlers, for the 409 body's
// "in_use" field) uses errors.As instead.
type inventoryInUseError struct {
	sentinel error
	Count    int
	Detail   string
}

func (e *inventoryInUseError) Error() string {
	return fmt.Sprintf("%s: %s", e.sentinel.Error(), e.Detail)
}

func (e *inventoryInUseError) Is(target error) bool { return target == e.sentinel }

// ── types ────────────────────────────────────────────────────────────────

// inventoryZone is one row of inventory_zones, an area aboard (salon,
// engine room, lazarette) - ADR 0065 §4's design, carried over unchanged.
// Bins is populated by ListZones and by the single-zone helpers below
// (CreateZone/UpdateZone), always as the zone's full, currently-ordered bin
// list, never lazily - a zone is small enough aboard a single boat that
// there is no listing of zones that doesn't also want to show what's in
// each one.
type inventoryZone struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	SortIndex int            `json:"sort_index"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	Bins      []inventoryBin `json:"bins"`
}

// inventoryBin is one row of inventory_bins: a numbered, printable-coded
// container filed inside exactly one zone.
type inventoryBin struct {
	ID        string    `json:"id"`
	ZoneID    string    `json:"zone_id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	SortIndex int       `json:"sort_index"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// equipmentItem is one row of equipment, the registry's own record, plus
// three read-only joined fields (ZoneName, BinCode, LinkCount) that only
// ever come back from a read (GetEquipment/ListEquipment) and are ignored
// on a write (CreateEquipment/UpdateEquipment read the request's ZoneID/
// BinID, never ZoneName/BinCode, to resolve where something lives).
// Aliases is always a non-nil, normalised (trimmed, deduped) slice - see
// normalizeAliases - so it serialises as "[]" rather than "null" on a
// record with none.
type equipmentItem struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Category       string    `json:"category"`
	System         string    `json:"system"`
	Manufacturer   string    `json:"manufacturer"`
	Model          string    `json:"model"`
	Serial         string    `json:"serial"`
	Quantity       int       `json:"quantity"`
	Status         string    `json:"status"`
	ZoneID         *string   `json:"zone_id"`
	BinID          *string   `json:"bin_id"`
	LocationDetail string    `json:"location_detail"`
	InstallDate    string    `json:"install_date"`
	HourMeterPath  string    `json:"hour_meter_path"`
	ProfileID      string    `json:"profile_id"`
	Aliases        []string  `json:"aliases"`
	VerifiedAboard bool      `json:"verified_aboard"`
	Notes          string    `json:"notes"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`

	// Joined, read-only. Always the zero value on a value CreateEquipment/
	// UpdateEquipment haven't yet re-read after their own write.
	//
	// No omitempty anywhere in this struct, ADR 0115 §7: the browser binds
	// these fields through a TypeScript interface that declares every one of
	// them, and a key the server drops for an empty value arrives as
	// `undefined` in code that type-checked clean. An item with no zone and no
	// bin is the commonest shape there is - a record straight out of the New
	// item button - so it is exactly the one that must still carry the keys.
	ZoneName  string `json:"zone_name"`
	BinCode   string `json:"bin_code"`
	LinkCount int    `json:"link_count"`

	// PhotoIDs (ADR 0127, amended 2026-09-25) is a VIEW over the item's own
	// linked documents: whichever ones are image/jpeg or image/png, ordered
	// by sort_index then document_id, cover first - never nil (see
	// equipmentByID/ListEquipment, the same "always the keys" reasoning as
	// ZoneName/BinCode above). No tag is involved and none is written on
	// upload - a document is a photo because of what it IS (an image MIME
	// type), not because of a flag someone (an operator, Mate) happened to
	// set on it. It is NOT every equipment_documents link (EquipmentDocuments
	// returns that); it is the image-MIME subset, which is what the bin
	// page's photo strip and the editor's photo row both read. A photo also
	// still counts toward LinkCount and still shows in the Documents tab -
	// it is a linked document like any other, just one this view also
	// happens to surface as a picture.
	PhotoIDs []string `json:"photo_ids"`

	// ExclusivePhotoIDs (2026-09-25 amendment) is the subset of PhotoIDs that
	// reference NOTHING else - no other equipment_documents row anywhere
	// still links the same document. It is what the frontend's delete
	// dialogs offer to also delete: an item delete's "Also delete N
	// photo(s) only this item uses" checkbox, and the strip's per-photo
	// "Remove and delete" choice. Computed only by equipmentByID (GetEquipment)
	// - ListEquipment leaves it empty, the one place in this struct that is
	// NOT "always the keys": the Equipment index and the bin page's grid
	// never need it, and computing a per-row exclusivity subquery for every
	// item in a filtered listing would be work nothing reads.
	ExclusivePhotoIDs []string `json:"exclusive_photo_ids"`
}

// equipmentDocument is one row of EquipmentDocuments' output: an
// equipment_documents link joined against documents for the fields a
// document-picker list actually wants to show (title, filename, kind,
// note_type) - never the document's full body/markdown, which nothing about
// an equipment record's Documents tab needs. SortIndex (ADR 0127) is
// carried through unconditionally - EquipmentDocuments returns every link,
// photo or not, and the photo strip is what actually orders by it; this
// struct just stops hiding the column from a caller that wants it.
type equipmentDocument struct {
	DocumentID string `json:"document_id"`
	Title      string `json:"title"`
	Filename   string `json:"filename"`
	Kind       string `json:"kind"`
	NoteType   string `json:"note_type"`
	Source     string `json:"source"`
	SortIndex  int    `json:"sort_index"`
}

// equipmentFilter is ListEquipment's input: every field blank means no
// filter at all (every equipment record, matching queryDocuments' own
// nil-means-unfiltered convention). See ListEquipment's own doc comment for
// why Query matches name/manufacturer/model AND aliases entirely in Go
// rather than splitting the two into a SQL LIKE plus a Go-side pass. BinID
// (ADR 0127) is the bin page's own filter - `?bin=` on GET
// /api/inventory/equipment - and mirrors ZoneID exactly: an exact match
// against a foreign-key column, no substring matching involved.
type equipmentFilter struct {
	Category string
	System   string
	Status   string
	ZoneID   string
	BinID    string
	Query    string
}

// Valid* sets back every CHECK-constrained enum column equipment carries -
// used by CreateEquipment/UpdateEquipment (this file) and by
// validateEquipmentInput (inventory_handlers.go) so the two never drift
// apart on what counts as a legal value.
var (
	validEquipmentCategories = map[string]bool{"mechanical": true, "general": true}
	validEquipmentSystems    = map[string]bool{
		"propulsion": true, "electrical": true, "water": true, "fuel": true,
		"bilge": true, "anchoring": true, "safety": true, "hvac": true,
		"navigation": true, "appliances": true, "structure": true, "other": true,
	}
	validEquipmentStatuses = map[string]bool{"deployed": true, "stored": true}
)

// ── zones ────────────────────────────────────────────────────────────────

// CreateZone validates and trims name, checks no zone already carries it
// case-insensitively, then inserts. sort_index starts at 0 - this cycle has
// no zone-reorder endpoint (plain CRUD, per plan), so every zone is born at
// the same sort_index and ListZones' own ORDER BY falls back to
// lower(name).
func (s *documentStore) CreateZone(name string) (inventoryZone, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return inventoryZone{}, errZoneNameInvalid
	}

	tx, err := s.db.Begin()
	if err != nil {
		return inventoryZone{}, fmt.Errorf("create zone: begin: %w", err)
	}
	defer tx.Rollback()

	taken, err := rowExists(tx, `SELECT 1 FROM inventory_zones WHERE lower(name) = lower(?)`, trimmed)
	if err != nil {
		return inventoryZone{}, fmt.Errorf("create zone: check name: %w", err)
	}
	if taken {
		return inventoryZone{}, errZoneNameTaken
	}

	now := s.now()
	id := uuid.NewString()
	if _, err := tx.Exec(
		`INSERT INTO inventory_zones (id, name, sort_index, created_at, updated_at) VALUES (?, ?, 0, ?, ?)`,
		id, trimmed, now.Unix(), now.Unix(),
	); err != nil {
		return inventoryZone{}, fmt.Errorf("create zone: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return inventoryZone{}, fmt.Errorf("create zone: commit: %w", err)
	}
	return inventoryZone{ID: id, Name: trimmed, CreatedAt: now, UpdatedAt: now, Bins: []inventoryBin{}}, nil
}

// UpdateZone renames an existing zone - the only field this cycle's plain
// CRUD ever changes on a zone (sort_index has no editor this cycle; see
// CreateZone's own comment).
func (s *documentStore) UpdateZone(id, name string) (inventoryZone, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return inventoryZone{}, errZoneNameInvalid
	}

	tx, err := s.db.Begin()
	if err != nil {
		return inventoryZone{}, fmt.Errorf("update zone: begin: %w", err)
	}
	defer tx.Rollback()

	ok, err := rowExists(tx, `SELECT 1 FROM inventory_zones WHERE id = ?`, id)
	if err != nil {
		return inventoryZone{}, fmt.Errorf("update zone: check exists: %w", err)
	}
	if !ok {
		return inventoryZone{}, errZoneNotFound
	}

	taken, err := rowExists(tx, `SELECT 1 FROM inventory_zones WHERE lower(name) = lower(?) AND id != ?`, trimmed, id)
	if err != nil {
		return inventoryZone{}, fmt.Errorf("update zone: check name: %w", err)
	}
	if taken {
		return inventoryZone{}, errZoneNameTaken
	}

	now := s.now()
	if _, err := tx.Exec(`UPDATE inventory_zones SET name = ?, updated_at = ? WHERE id = ?`, trimmed, now.Unix(), id); err != nil {
		return inventoryZone{}, fmt.Errorf("update zone: %w", err)
	}

	zone, err := zoneByID(tx, id)
	if err != nil {
		return inventoryZone{}, err
	}

	if err := tx.Commit(); err != nil {
		return inventoryZone{}, fmt.Errorf("update zone: commit: %w", err)
	}
	return zone, nil
}

// DeleteZone refuses to remove a zone that is still in use - a bin filed
// under it, or an equipment item referencing it directly. That second case
// covers a bin-placed item too: bin-implies-zone (validateEquipmentLocation
// below) means equipment.zone_id is ALWAYS set whenever bin_id is, so
// counting equipment by zone_id alone already reaches every item sitting in
// one of this zone's bins as well as every item assigned to the zone with
// no bin - there is no separate "equipment in a bin under this zone" count
// to also take, it would double-count. See inventoryInUseError's own doc
// comment for why this pre-checks with COUNT(*) rather than attempting the
// DELETE and inspecting what SQLite's RESTRICT violation says.
func (s *documentStore) DeleteZone(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("delete zone: begin: %w", err)
	}
	defer tx.Rollback()

	ok, err := rowExists(tx, `SELECT 1 FROM inventory_zones WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete zone: check exists: %w", err)
	}
	if !ok {
		return errZoneNotFound
	}

	var binCount, equipmentCount int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM inventory_bins WHERE zone_id = ?`, id).Scan(&binCount); err != nil {
		return fmt.Errorf("delete zone: count bins: %w", err)
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM equipment WHERE zone_id = ?`, id).Scan(&equipmentCount); err != nil {
		return fmt.Errorf("delete zone: count equipment: %w", err)
	}
	if total := binCount + equipmentCount; total > 0 {
		return &inventoryInUseError{
			sentinel: errZoneInUse,
			Count:    total,
			Detail:   fmt.Sprintf("%d bin(s) and %d equipment item(s) still reference it", binCount, equipmentCount),
		}
	}

	if _, err := tx.Exec(`DELETE FROM inventory_zones WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete zone: %w", err)
	}
	return tx.Commit()
}

// ListZones returns every zone, each with its own bins nested, both
// ordered sort_index then lower-name/code. Drains the zone listing query
// fully (rows.Close, before the per-zone bin lookups below ever run) for
// the same single-connection-pool reason ListManuals' own doc comment
// gives (manuals_store.go): a second query issued while the first's rows
// are still open would block forever on a pool that is exactly one
// connection wide.
func (s *documentStore) ListZones() ([]inventoryZone, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT id, name, sort_index, created_at, updated_at FROM inventory_zones ORDER BY sort_index, lower(name)`)
	if err != nil {
		return nil, fmt.Errorf("list zones: %w", err)
	}
	type zoneRow struct {
		id, name             string
		sortIndex            int
		createdAt, updatedAt int64
	}
	var raw []zoneRow
	for rows.Next() {
		var r zoneRow
		if err := rows.Scan(&r.id, &r.name, &r.sortIndex, &r.createdAt, &r.updatedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("list zones: scan: %w", err)
		}
		raw = append(raw, r)
	}
	scanErr := rows.Err()
	rows.Close()
	if scanErr != nil {
		return nil, fmt.Errorf("list zones: %w", scanErr)
	}

	out := make([]inventoryZone, 0, len(raw))
	for _, r := range raw {
		bins, err := binsForZone(s.db, r.id)
		if err != nil {
			return nil, err
		}
		out = append(out, inventoryZone{
			ID: r.id, Name: r.name, SortIndex: r.sortIndex,
			CreatedAt: time.Unix(r.createdAt, 0).UTC(), UpdatedAt: time.Unix(r.updatedAt, 0).UTC(),
			Bins: bins,
		})
	}
	return out, nil
}

// zoneByID reads a single zone plus its nested bins - the unlocked half of
// UpdateZone's return value, callable from inside a tx that already holds
// s.mu (documentStore's mutex is not reentrant, so ListZones/UpdateZone's
// own exported, locking methods can't call each other directly).
func zoneByID(q sqlQueryer, id string) (inventoryZone, error) {
	var z inventoryZone
	var createdAt, updatedAt int64
	err := q.QueryRow(`SELECT id, name, sort_index, created_at, updated_at FROM inventory_zones WHERE id = ?`, id).
		Scan(&z.ID, &z.Name, &z.SortIndex, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return inventoryZone{}, errZoneNotFound
	}
	if err != nil {
		return inventoryZone{}, fmt.Errorf("zone by id: %w", err)
	}
	z.CreatedAt = time.Unix(createdAt, 0).UTC()
	z.UpdatedAt = time.Unix(updatedAt, 0).UTC()

	bins, err := binsForZone(q, id)
	if err != nil {
		return inventoryZone{}, err
	}
	z.Bins = bins
	return z, nil
}

// binsForZone returns zoneID's own bins, ordered sort_index then
// lower(code) - shared by ListZones, zoneByID and UpdateZone's return value
// so there is exactly one query that decides bin order.
func binsForZone(q sqlQueryer, zoneID string) ([]inventoryBin, error) {
	rows, err := q.Query(
		`SELECT id, zone_id, code, name, sort_index, created_at, updated_at FROM inventory_bins WHERE zone_id = ? ORDER BY sort_index, lower(code)`,
		zoneID,
	)
	if err != nil {
		return nil, fmt.Errorf("bins for zone: %w", err)
	}
	defer rows.Close()

	out := []inventoryBin{}
	for rows.Next() {
		var b inventoryBin
		var createdAt, updatedAt int64
		if err := rows.Scan(&b.ID, &b.ZoneID, &b.Code, &b.Name, &b.SortIndex, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("bins for zone: scan: %w", err)
		}
		b.CreatedAt = time.Unix(createdAt, 0).UTC()
		b.UpdatedAt = time.Unix(updatedAt, 0).UTC()
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("bins for zone: %w", err)
	}
	return out, nil
}

// ── bins ─────────────────────────────────────────────────────────────────

// CreateBin validates zoneID exists and code is non-blank and unique
// (case-insensitively, boat-wide - inventory_bins_code's own schema
// comment), then inserts.
func (s *documentStore) CreateBin(zoneID, code, name string) (inventoryBin, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	trimmedCode := strings.TrimSpace(code)
	if trimmedCode == "" {
		return inventoryBin{}, errBinCodeInvalid
	}
	trimmedName := strings.TrimSpace(name)

	tx, err := s.db.Begin()
	if err != nil {
		return inventoryBin{}, fmt.Errorf("create bin: begin: %w", err)
	}
	defer tx.Rollback()

	ok, err := rowExists(tx, `SELECT 1 FROM inventory_zones WHERE id = ?`, zoneID)
	if err != nil {
		return inventoryBin{}, fmt.Errorf("create bin: check zone: %w", err)
	}
	if !ok {
		return inventoryBin{}, errZoneNotFound
	}

	taken, err := rowExists(tx, `SELECT 1 FROM inventory_bins WHERE lower(code) = lower(?)`, trimmedCode)
	if err != nil {
		return inventoryBin{}, fmt.Errorf("create bin: check code: %w", err)
	}
	if taken {
		return inventoryBin{}, errBinCodeTaken
	}

	now := s.now()
	id := uuid.NewString()
	if _, err := tx.Exec(
		`INSERT INTO inventory_bins (id, zone_id, code, name, sort_index, created_at, updated_at) VALUES (?, ?, ?, ?, 0, ?, ?)`,
		id, zoneID, trimmedCode, trimmedName, now.Unix(), now.Unix(),
	); err != nil {
		return inventoryBin{}, fmt.Errorf("create bin: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return inventoryBin{}, fmt.Errorf("create bin: commit: %w", err)
	}
	return inventoryBin{ID: id, ZoneID: zoneID, Code: trimmedCode, Name: trimmedName, CreatedAt: now, UpdatedAt: now}, nil
}

// UpdateBin renames/recodes an existing bin. It never re-zones one - plain
// CRUD, per plan, has no "move this bin to a different zone" action this
// cycle, so zone_id is fixed at creation and every equipment record already
// pointing at this bin keeps a location that still makes sense.
func (s *documentStore) UpdateBin(id, code, name string) (inventoryBin, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	trimmedCode := strings.TrimSpace(code)
	if trimmedCode == "" {
		return inventoryBin{}, errBinCodeInvalid
	}
	trimmedName := strings.TrimSpace(name)

	tx, err := s.db.Begin()
	if err != nil {
		return inventoryBin{}, fmt.Errorf("update bin: begin: %w", err)
	}
	defer tx.Rollback()

	ok, err := rowExists(tx, `SELECT 1 FROM inventory_bins WHERE id = ?`, id)
	if err != nil {
		return inventoryBin{}, fmt.Errorf("update bin: check exists: %w", err)
	}
	if !ok {
		return inventoryBin{}, errBinNotFound
	}

	taken, err := rowExists(tx, `SELECT 1 FROM inventory_bins WHERE lower(code) = lower(?) AND id != ?`, trimmedCode, id)
	if err != nil {
		return inventoryBin{}, fmt.Errorf("update bin: check code: %w", err)
	}
	if taken {
		return inventoryBin{}, errBinCodeTaken
	}

	now := s.now()
	if _, err := tx.Exec(`UPDATE inventory_bins SET code = ?, name = ?, updated_at = ? WHERE id = ?`, trimmedCode, trimmedName, now.Unix(), id); err != nil {
		return inventoryBin{}, fmt.Errorf("update bin: %w", err)
	}

	bin, err := binByID(tx, id)
	if err != nil {
		return inventoryBin{}, err
	}

	if err := tx.Commit(); err != nil {
		return inventoryBin{}, fmt.Errorf("update bin: commit: %w", err)
	}
	return bin, nil
}

// DeleteBin refuses to remove a bin that still has an equipment item
// pointing at it - see inventoryInUseError's own doc comment for why this
// pre-checks with COUNT(*) rather than attempting the DELETE.
func (s *documentStore) DeleteBin(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("delete bin: begin: %w", err)
	}
	defer tx.Rollback()

	ok, err := rowExists(tx, `SELECT 1 FROM inventory_bins WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete bin: check exists: %w", err)
	}
	if !ok {
		return errBinNotFound
	}

	var equipmentCount int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM equipment WHERE bin_id = ?`, id).Scan(&equipmentCount); err != nil {
		return fmt.Errorf("delete bin: count equipment: %w", err)
	}
	if equipmentCount > 0 {
		return &inventoryInUseError{
			sentinel: errBinInUse,
			Count:    equipmentCount,
			Detail:   fmt.Sprintf("%d equipment item(s) still reference it", equipmentCount),
		}
	}

	if _, err := tx.Exec(`DELETE FROM inventory_bins WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete bin: %w", err)
	}
	return tx.Commit()
}

// binByID reads a single bin - the unlocked helper UpdateBin's own return
// value calls from inside its own transaction.
func binByID(q sqlQueryer, id string) (inventoryBin, error) {
	var b inventoryBin
	var createdAt, updatedAt int64
	err := q.QueryRow(`SELECT id, zone_id, code, name, sort_index, created_at, updated_at FROM inventory_bins WHERE id = ?`, id).
		Scan(&b.ID, &b.ZoneID, &b.Code, &b.Name, &b.SortIndex, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return inventoryBin{}, errBinNotFound
	}
	if err != nil {
		return inventoryBin{}, fmt.Errorf("bin by id: %w", err)
	}
	b.CreatedAt = time.Unix(createdAt, 0).UTC()
	b.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return b, nil
}

// ── equipment ────────────────────────────────────────────────────────────

// validateEquipmentLocation resolves and checks an equipment record's
// zone/bin pair before CreateEquipment/UpdateEquipment ever try to write
// it. A bin implies its own zone (plan's own decision): a caller supplying
// bin_id alone gets zone_id derived from the bin's own row; a caller
// supplying both gets a hard error if they disagree
// (errEquipmentLocationMismatch) rather than a silent correction to the
// bin's real zone, which would write something different from what was
// asked for without saying so. Returns the resolved zone_id (nil if
// neither was given, the caller's own zoneID if only that was given and it
// exists, or the bin's zone_id if binID was given).
func validateEquipmentLocation(q sqlQueryer, zoneID, binID *string) (*string, error) {
	if binID == nil {
		if zoneID == nil {
			return nil, nil
		}
		ok, err := rowExists(q, `SELECT 1 FROM inventory_zones WHERE id = ?`, *zoneID)
		if err != nil {
			return nil, fmt.Errorf("validate equipment location: check zone: %w", err)
		}
		if !ok {
			return nil, errZoneNotFound
		}
		return zoneID, nil
	}

	var binZoneID string
	err := q.QueryRow(`SELECT zone_id FROM inventory_bins WHERE id = ?`, *binID).Scan(&binZoneID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errBinNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("validate equipment location: read bin: %w", err)
	}

	if zoneID != nil && *zoneID != binZoneID {
		return nil, errEquipmentLocationMismatch
	}
	return &binZoneID, nil
}

// normalizeAliases trims every alias, drops blanks, and removes duplicates
// case-sensitively - "AIS" and "ais" are kept as distinct alt names, since
// an operator typing an acronym's exact casing is a deliberate choice, not
// noise to fold away - preserving first-seen order. Both CreateEquipment
// and UpdateEquipment run every alias list through this before it is ever
// marshalled to aliases_json, so the stored JSON is always the canonical
// form regardless of what a caller (a handler that already did its own
// pass, or a store test calling CreateEquipment directly) passed in - there
// is exactly one place a bare, un-normalised alias list could end up
// written to disk, and this is it.
func normalizeAliases(in []string) []string {
	// Keyed by the folded form, but the operator's own first spelling is what
	// gets stored: matching an alias against something the crew said is
	// case-insensitive, so "genset" and "Genset" are one alias spelled twice,
	// not two aliases. Folding only the KEY keeps the dedupe honest without
	// flattening the capitalisation the operator chose to look at.
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, a := range in {
		trimmed := strings.TrimSpace(a)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, trimmed)
	}
	return out
}

// equipmentColumns is every equipment column plus the two joined location
// fields and the link count, in the exact order scanEquipmentRow expects -
// the same documentColumns/scanDocument split documents_store.go itself
// uses, so a query built with this constant and scanEquipmentRow can never
// drift apart from each other.
const equipmentColumns = `e.id, e.name, e.category, e.system, e.manufacturer, e.model, e.serial, e.quantity, e.status,
	e.zone_id, e.bin_id, e.location_detail, e.install_date, e.hour_meter_path, e.profile_id, e.aliases_json,
	e.verified_aboard, e.notes, e.created_at, e.updated_at,
	z.name, b.code,
	(SELECT COUNT(*) FROM equipment_documents ed WHERE ed.equipment_id = e.id)`

// equipmentFromClause is shared by every read of the equipment table that
// wants the joined zone name and bin code alongside it - equipmentByID and
// ListEquipment both start from exactly this, so the two can never
// disagree about which zone/bin a row's joined fields come from.
const equipmentFromClause = `equipment e
	LEFT JOIN inventory_zones z ON z.id = e.zone_id
	LEFT JOIN inventory_bins b ON b.id = e.bin_id`

func scanEquipmentRow(row rowScanner) (equipmentItem, error) {
	var it equipmentItem
	var zoneID, binID, zoneName, binCode sql.NullString
	var aliasesJSON string
	var verified int
	var createdAt, updatedAt int64

	if err := row.Scan(
		&it.ID, &it.Name, &it.Category, &it.System, &it.Manufacturer, &it.Model, &it.Serial, &it.Quantity, &it.Status,
		&zoneID, &binID, &it.LocationDetail, &it.InstallDate, &it.HourMeterPath, &it.ProfileID, &aliasesJSON,
		&verified, &it.Notes, &createdAt, &updatedAt,
		&zoneName, &binCode, &it.LinkCount,
	); err != nil {
		return equipmentItem{}, err
	}

	if zoneID.Valid {
		v := zoneID.String
		it.ZoneID = &v
	}
	if binID.Valid {
		v := binID.String
		it.BinID = &v
	}
	it.ZoneName = zoneName.String
	it.BinCode = binCode.String
	it.VerifiedAboard = verified != 0
	it.CreatedAt = time.Unix(createdAt, 0).UTC()
	it.UpdatedAt = time.Unix(updatedAt, 0).UTC()

	var aliases []string
	if err := json.Unmarshal([]byte(aliasesJSON), &aliases); err != nil {
		return equipmentItem{}, fmt.Errorf("scan equipment: aliases: %w", err)
	}
	if aliases == nil {
		aliases = []string{}
	}
	it.Aliases = aliases

	return it, nil
}

// photoMIMEs is the exact set of MIME types photoIDsForEquipmentIDs treats
// as "this linked document is a photo" - image/jpeg or image/png, the same
// pair uploadEquipmentPhotoHandler accepts (inventory_handlers.go). No tag
// involved (2026-09-25 amendment): a document is a photo because of what it
// IS, not a flag written on upload.
const photoMIMEsClause = `d.mime IN ('image/jpeg', 'image/png')`

// photoIDsForEquipmentIDs returns each of ids' own photo document ids
// (image/jpeg or image/png among its linked documents), cover first -
// ordered by sort_index then document_id. One aggregate query over every id
// at once (equipmentColumns' own doc comment gives the identical reasoning
// for the zone/bin join above it: a handful of items aboard one boat, never
// worth N+1 queries to avoid), shared by equipmentByID (one id) and
// ListEquipment (the whole filtered result) so the two can never compute
// this differently. Callers get back only the ids that actually matched -
// an id with no photos simply has no entry in the map, and every call site
// treats a missing key the same as an empty slice.
func photoIDsForEquipmentIDs(q sqlQueryer, ids []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(ids) == 0 {
		return out, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	rows, err := q.Query(`
		SELECT ed.equipment_id, ed.document_id
		FROM equipment_documents ed
		JOIN documents d ON d.id = ed.document_id AND `+photoMIMEsClause+`
		WHERE ed.equipment_id IN (`+strings.Join(placeholders, ",")+`)
		ORDER BY ed.equipment_id, ed.sort_index, ed.document_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("photo ids for equipment: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var equipmentID, documentID string
		if err := rows.Scan(&equipmentID, &documentID); err != nil {
			return nil, fmt.Errorf("photo ids for equipment: scan: %w", err)
		}
		out[equipmentID] = append(out[equipmentID], documentID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photo ids for equipment: %w", err)
	}
	return out, nil
}

// exclusivePhotoIDsForEquipmentID returns the subset of equipmentID's own
// photo ids (photoIDsForEquipmentIDs' same image-MIME view) that reference
// NOTHING else - no other equipment_documents row, on any item, still links
// the same document. This is what a delete dialog can safely offer to also
// delete: a document some OTHER item still links must never be removed just
// because this one's link to it is going away (the operator's own decision,
// "never delete a document that another item still links").
func exclusivePhotoIDsForEquipmentID(q sqlQueryer, equipmentID string) ([]string, error) {
	rows, err := q.Query(`
		SELECT ed.document_id
		FROM equipment_documents ed
		JOIN documents d ON d.id = ed.document_id AND `+photoMIMEsClause+`
		WHERE ed.equipment_id = ?
		AND (SELECT COUNT(*) FROM equipment_documents ed2 WHERE ed2.document_id = ed.document_id) = 1
		ORDER BY ed.sort_index, ed.document_id`, equipmentID)
	if err != nil {
		return nil, fmt.Errorf("exclusive photo ids for equipment: %w", err)
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var documentID string
		if err := rows.Scan(&documentID); err != nil {
			return nil, fmt.Errorf("exclusive photo ids for equipment: scan: %w", err)
		}
		out = append(out, documentID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("exclusive photo ids for equipment: %w", err)
	}
	return out, nil
}

// equipmentByID reads a single equipment row, zone name, bin code and photo
// ids joined in. The unlocked helper GetEquipment's own locking method, and
// CreateEquipment/UpdateEquipment from inside their own transaction, all
// call this rather than duplicating the query.
func equipmentByID(q sqlQueryer, id string) (equipmentItem, error) {
	row := q.QueryRow(`SELECT `+equipmentColumns+` FROM `+equipmentFromClause+` WHERE e.id = ?`, id)
	it, err := scanEquipmentRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return equipmentItem{}, errEquipmentNotFound
	}
	if err != nil {
		return equipmentItem{}, fmt.Errorf("get equipment: %w", err)
	}

	photoMap, err := photoIDsForEquipmentIDs(q, []string{id})
	if err != nil {
		return equipmentItem{}, err
	}
	it.PhotoIDs = photoMap[id]
	if it.PhotoIDs == nil {
		it.PhotoIDs = []string{}
	}

	exclusive, err := exclusivePhotoIDsForEquipmentID(q, id)
	if err != nil {
		return equipmentItem{}, err
	}
	it.ExclusivePhotoIDs = exclusive
	return it, nil
}

// CreateEquipment inserts a new equipment record. category is required
// (no default - a record has to say which kind it is); system and status
// default to 'other'/'deployed' when left blank, then both are checked
// against their own valid* set the same as a non-blank value would be -
// same treatment, just with a default value substituted first. zone_id/
// bin_id go through validateEquipmentLocation before anything is written,
// so a bad or contradictory location never reaches the INSERT at all.
func (s *documentStore) CreateEquipment(item equipmentItem) (equipmentItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	name := strings.TrimSpace(item.Name)
	if name == "" {
		return equipmentItem{}, errEquipmentNameRequired
	}
	if !validEquipmentCategories[item.Category] {
		return equipmentItem{}, errEquipmentInvalidCategory
	}
	system := item.System
	if system == "" {
		system = "other"
	}
	if !validEquipmentSystems[system] {
		return equipmentItem{}, errEquipmentInvalidSystem
	}
	status := item.Status
	if status == "" {
		status = "deployed"
	}
	if !validEquipmentStatuses[status] {
		return equipmentItem{}, errEquipmentInvalidStatus
	}
	quantity := item.Quantity
	if quantity <= 0 {
		quantity = 1
	}

	tx, err := s.db.Begin()
	if err != nil {
		return equipmentItem{}, fmt.Errorf("create equipment: begin: %w", err)
	}
	defer tx.Rollback()

	resolvedZoneID, err := validateEquipmentLocation(tx, item.ZoneID, item.BinID)
	if err != nil {
		return equipmentItem{}, err
	}

	aliasesJSON, err := json.Marshal(normalizeAliases(item.Aliases))
	if err != nil {
		return equipmentItem{}, fmt.Errorf("create equipment: marshal aliases: %w", err)
	}

	now := s.now()
	id := uuid.NewString()
	if _, err := tx.Exec(`
		INSERT INTO equipment (
			id, name, category, system, manufacturer, model, serial, quantity, status,
			zone_id, bin_id, location_detail, install_date, hour_meter_path, profile_id,
			aliases_json, verified_aboard, notes, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, name, item.Category, system, strings.TrimSpace(item.Manufacturer), strings.TrimSpace(item.Model), strings.TrimSpace(item.Serial), quantity, status,
		nullableString(resolvedZoneID), nullableString(item.BinID), item.LocationDetail, item.InstallDate, item.HourMeterPath, item.ProfileID,
		string(aliasesJSON), boolToInt(item.VerifiedAboard), item.Notes, now.Unix(), now.Unix(),
	); err != nil {
		return equipmentItem{}, fmt.Errorf("create equipment: %w", err)
	}

	created, err := equipmentByID(tx, id)
	if err != nil {
		return equipmentItem{}, err
	}

	if err := tx.Commit(); err != nil {
		return equipmentItem{}, fmt.Errorf("create equipment: commit: %w", err)
	}
	return created, nil
}

// UpdateEquipment replaces id's WHOLE editable field set in one
// transaction - a PUT, not a PATCH (plan's own API shape), the same
// full-replace contract CreateEquipment's own fields follow. zone_id/bin_id
// go through validateEquipmentLocation exactly as they do on create -
// there is no special-cased "location unchanged" path, so a PUT that
// merely echoes the record's current location back still gets it
// re-validated, which is cheap and keeps this method's location handling
// identical to CreateEquipment's rather than a second, subtly different
// copy of it.
func (s *documentStore) UpdateEquipment(id string, item equipmentItem) (equipmentItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	name := strings.TrimSpace(item.Name)
	if name == "" {
		return equipmentItem{}, errEquipmentNameRequired
	}
	if !validEquipmentCategories[item.Category] {
		return equipmentItem{}, errEquipmentInvalidCategory
	}
	system := item.System
	if system == "" {
		system = "other"
	}
	if !validEquipmentSystems[system] {
		return equipmentItem{}, errEquipmentInvalidSystem
	}
	status := item.Status
	if status == "" {
		status = "deployed"
	}
	if !validEquipmentStatuses[status] {
		return equipmentItem{}, errEquipmentInvalidStatus
	}
	quantity := item.Quantity
	if quantity <= 0 {
		quantity = 1
	}

	tx, err := s.db.Begin()
	if err != nil {
		return equipmentItem{}, fmt.Errorf("update equipment: begin: %w", err)
	}
	defer tx.Rollback()

	ok, err := rowExists(tx, `SELECT 1 FROM equipment WHERE id = ?`, id)
	if err != nil {
		return equipmentItem{}, fmt.Errorf("update equipment: check exists: %w", err)
	}
	if !ok {
		return equipmentItem{}, errEquipmentNotFound
	}

	resolvedZoneID, err := validateEquipmentLocation(tx, item.ZoneID, item.BinID)
	if err != nil {
		return equipmentItem{}, err
	}

	aliasesJSON, err := json.Marshal(normalizeAliases(item.Aliases))
	if err != nil {
		return equipmentItem{}, fmt.Errorf("update equipment: marshal aliases: %w", err)
	}

	now := s.now()
	if _, err := tx.Exec(`
		UPDATE equipment SET
			name = ?, category = ?, system = ?, manufacturer = ?, model = ?, serial = ?, quantity = ?, status = ?,
			zone_id = ?, bin_id = ?, location_detail = ?, install_date = ?, hour_meter_path = ?, profile_id = ?,
			aliases_json = ?, verified_aboard = ?, notes = ?, updated_at = ?
		WHERE id = ?`,
		name, item.Category, system, strings.TrimSpace(item.Manufacturer), strings.TrimSpace(item.Model), strings.TrimSpace(item.Serial), quantity, status,
		nullableString(resolvedZoneID), nullableString(item.BinID), item.LocationDetail, item.InstallDate, item.HourMeterPath, item.ProfileID,
		string(aliasesJSON), boolToInt(item.VerifiedAboard), item.Notes, now.Unix(),
		id,
	); err != nil {
		return equipmentItem{}, fmt.Errorf("update equipment: %w", err)
	}

	updated, err := equipmentByID(tx, id)
	if err != nil {
		return equipmentItem{}, err
	}

	if err := tx.Commit(); err != nil {
		return equipmentItem{}, fmt.Errorf("update equipment: commit: %w", err)
	}
	return updated, nil
}

// DeleteEquipment removes an equipment row. Its equipment_documents links
// need no explicit cleanup here - ON DELETE CASCADE on
// equipment_documents.equipment_id (documents_store.go's schema) removes
// them as part of the same SQLite-internal operation, proved by
// TestDocumentStore_DeleteEquipmentCascadesLinks (inventory_store_test.go)
// rather than merely assumed from the schema text. That cascade removes
// only the LINK - every linked document (photo or not) survives an item
// delete by default (2026-09-25 amendment: unlink only, the operator's own
// decision that a delete must never destroy a document without being asked).
//
// deletePhotos, when true, additionally deletes the document row (and,
// per the caller's own file, its bytes on disk) for each of id's own
// EXCLUSIVE photos - the ones exclusivePhotoIDsForEquipmentID already
// reports nothing else links, re-read fresh inside this same transaction
// rather than trusting a caller-supplied list, so a link added between
// that earlier read and this call can never be deleted out from under
// whoever just added it. A shared photo (still linked to another item) is
// never touched either way. Returns the sha256 of every document this call
// actually deleted, so deleteEquipmentHandler (inventory_handlers.go) can
// remove each one's file from disk too - the same store/handler split
// RemoveEquipmentPhoto/deleteEquipmentPhotoHandler already draw.
//
// This inlines the same single `DELETE FROM documents WHERE id = ?`
// documentStore.Delete (documents_store.go) itself runs, rather than
// calling that exported method directly - Delete manages its own
// transaction, and SQLite/database/sql here has no savepoint support to
// nest one transaction inside another, so the only way to keep "unlink
// this item, then delete its now-orphaned photos" atomic is to run both
// steps' SQL inside the one transaction this method already holds.
func (s *documentStore) DeleteEquipment(id string, deletePhotos bool) (deletedPhotoSHAs []string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("delete equipment: begin: %w", err)
	}
	defer tx.Rollback()

	ok, err := rowExists(tx, `SELECT 1 FROM equipment WHERE id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("delete equipment: check equipment: %w", err)
	}
	if !ok {
		return nil, errEquipmentNotFound
	}

	var exclusiveIDs []string
	if deletePhotos {
		exclusiveIDs, err = exclusivePhotoIDsForEquipmentID(tx, id)
		if err != nil {
			return nil, err
		}
	}

	if _, err := tx.Exec(`DELETE FROM equipment WHERE id = ?`, id); err != nil {
		return nil, fmt.Errorf("delete equipment: %w", err)
	}

	for _, docID := range exclusiveIDs {
		// The equipment row (and its cascade) is already gone by this point
		// - a fresh recheck here would only ever confirm what
		// exclusivePhotoIDsForEquipmentID already established inside this
		// SAME transaction, so there is nothing new for it to catch that a
		// concurrent writer couldn't just as easily have raced regardless.
		var sha string
		if err := tx.QueryRow(`SELECT sha256 FROM documents WHERE id = ?`, docID).Scan(&sha); err != nil {
			return nil, fmt.Errorf("delete equipment: read photo sha: %w", err)
		}
		if _, err := tx.Exec(`DELETE FROM documents WHERE id = ?`, docID); err != nil {
			return nil, fmt.Errorf("delete equipment: delete photo document: %w", err)
		}
		deletedPhotoSHAs = append(deletedPhotoSHAs, sha)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("delete equipment: commit: %w", err)
	}
	return deletedPhotoSHAs, nil
}

// GetEquipment reads a single equipment record, zone name and bin code
// joined in. It does NOT include the record's linked documents - that is
// EquipmentDocuments' own job (plan's method list keeps the two separate);
// the HTTP handler for GET /api/inventory/equipment/:id
// (inventory_handlers.go) calls both and composes {item, documents} itself.
func (s *documentStore) GetEquipment(id string) (equipmentItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return equipmentByID(s.db, id)
}

// equipmentMatchesQuery reports whether q (already trimmed and lowercased
// by the caller) is a case-insensitive substring of it's name,
// manufacturer, model, or any of its aliases - the single match rule
// ListEquipment applies regardless of which field actually hits, so a
// search for "genset" behaves identically whether that word lives in the
// name column or only in the alias list.
func equipmentMatchesQuery(it equipmentItem, q string) bool {
	if strings.Contains(strings.ToLower(it.Name), q) ||
		strings.Contains(strings.ToLower(it.Manufacturer), q) ||
		strings.Contains(strings.ToLower(it.Model), q) {
		return true
	}
	for _, alias := range it.Aliases {
		if strings.Contains(strings.ToLower(alias), q) {
			return true
		}
	}
	return false
}

// ListEquipment returns every equipment record matching filter, grouped by
// system then ordered by name - the Equipment index's own "grouped by
// system in enum order" wants a stable per-system, per-name order to
// display, and system's CHECK-constrained value set already sorts
// alphabetically close enough to plan's declared enum order for this
// cycle's plain listing (Phase 2's own grouping, if it wants a different
// order, re-sorts client-side rather than this method taking an explicit
// order parameter no other caller needs).
//
// Category/System/Status/ZoneID are applied as SQL WHERE clauses - each is
// an exact match against a CHECK-constrained column, so there is nothing
// approximate about them. Query is different: aliases_json is a JSON blob
// (this file's own top comment), not a column SQL can search a single
// alias out of without json_each, and running the SAME substring rule
// against name/manufacturer/model that aliases needs anyway (rather than a
// SQL LIKE for the structured columns and a DIFFERENT Go rule for aliases)
// keeps "does q match this record" one predicate, not two that could
// silently disagree about case-folding or partial-word matching. Equipment
// aboard one boat is a few dozen rows at most, so fetching every row that
// passes the structural filters and running equipmentMatchesQuery in Go
// over all of them costs nothing worth optimising away.
func (s *documentStore) ListEquipment(filter equipmentFilter) ([]equipmentItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `SELECT ` + equipmentColumns + ` FROM ` + equipmentFromClause + ` WHERE 1=1`
	var args []any

	if filter.Category != "" {
		query += ` AND e.category = ?`
		args = append(args, filter.Category)
	}
	if filter.System != "" {
		query += ` AND e.system = ?`
		args = append(args, filter.System)
	}
	if filter.Status != "" {
		query += ` AND e.status = ?`
		args = append(args, filter.Status)
	}
	if filter.ZoneID != "" {
		query += ` AND e.zone_id = ?`
		args = append(args, filter.ZoneID)
	}
	if filter.BinID != "" {
		query += ` AND e.bin_id = ?`
		args = append(args, filter.BinID)
	}
	query += ` ORDER BY e.system, lower(e.name)`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list equipment: %w", err)
	}
	defer rows.Close()

	var out []equipmentItem
	for rows.Next() {
		it, err := scanEquipmentRow(rows)
		if err != nil {
			return nil, fmt.Errorf("list equipment: scan: %w", err)
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list equipment: %w", err)
	}

	// The Go-side `q` text filter runs BEFORE photoIDsForEquipmentIDs below -
	// review finding: this used to fetch photo ids for every structurally-
	// matching row first and only then filter by q, doing that lookup's own
	// work for rows the filter was about to discard. Filtering first means
	// the aggregate photo query below only ever runs over the rows this call
	// actually returns.
	q := strings.ToLower(strings.TrimSpace(filter.Query))
	if q != "" {
		filtered := make([]equipmentItem, 0, len(out))
		for _, it := range out {
			if equipmentMatchesQuery(it, q) {
				filtered = append(filtered, it)
			}
		}
		out = filtered
	}

	// One aggregate photoIDsForEquipmentIDs call over the surviving result,
	// not one per item (that function's own doc comment) - the bin page and
	// the equipment index both need photo_ids on every row they show.
	ids := make([]string, len(out))
	for i, it := range out {
		ids[i] = it.ID
	}
	photoMap, err := photoIDsForEquipmentIDs(s.db, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].PhotoIDs = photoMap[out[i].ID]
		if out[i].PhotoIDs == nil {
			out[i].PhotoIDs = []string{}
		}
		// ExclusivePhotoIDs' own doc comment: a listing never needs it, so
		// it is left an (always non-nil, per this struct's own "no
		// omitempty" convention) empty slice rather than run through
		// exclusivePhotoIDsForEquipmentID once per row.
		out[i].ExclusivePhotoIDs = []string{}
	}

	return out, nil
}

// ── equipment documents ──────────────────────────────────────────────────

// EquipmentDocuments returns id's linked documents, joined against
// documents for title/filename/kind/note_type, newest-linked-document-first.
// errEquipmentNotFound if id doesn't exist - an empty result must not be
// ambiguous between "this equipment has no linked documents" and "this
// equipment doesn't exist" (AGENTS.md's fail-fast policy). A link whose
// document row is gone cannot exist in the first place - ON DELETE CASCADE
// on equipment_documents.document_id removes the link the instant its
// document does (TestDocumentStore_DeletingDocumentRemovesEquipmentLinks) -
// so the JOIN below never needs an "or the document might not be there"
// tolerance; every row this query returns has a real document behind it by
// construction.
func (s *documentStore) EquipmentDocuments(id string) ([]equipmentDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ok, err := rowExists(s.db, `SELECT 1 FROM equipment WHERE id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("equipment documents: check equipment: %w", err)
	}
	if !ok {
		return nil, errEquipmentNotFound
	}

	rows, err := s.db.Query(`
		SELECT ed.document_id, d.title, d.filename, d.kind, d.note_type, ed.source, ed.sort_index
		FROM equipment_documents ed
		JOIN documents d ON d.id = ed.document_id
		WHERE ed.equipment_id = ?
		ORDER BY d.created_at DESC, d.id DESC`, id)
	if err != nil {
		return nil, fmt.Errorf("equipment documents: %w", err)
	}
	defer rows.Close()

	out := []equipmentDocument{}
	for rows.Next() {
		var ed equipmentDocument
		if err := rows.Scan(&ed.DocumentID, &ed.Title, &ed.Filename, &ed.Kind, &ed.NoteType, &ed.Source, &ed.SortIndex); err != nil {
			return nil, fmt.Errorf("equipment documents: scan: %w", err)
		}
		out = append(out, ed)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("equipment documents: %w", err)
	}
	return out, nil
}

// SetEquipmentDocuments replaces id's WHOLE linked-document set in one
// transaction - "replace wholesale" (plan's own phrase), PHOTOS INCLUDED
// (2026-09-25 amendment: the earlier photo-tagged carve-out is gone along
// with the tag itself - the Documents tab lists every linked document,
// photos too, and this is the one write that owns that whole set). Every
// existing equipment_documents row for id NOT named in docIDs is deleted;
// every id in docIDs not already linked is inserted fresh, all under
// source='operator' (this cycle only edits links from the equipment side -
// plan's own "Links edited from the equipment side this cycle" decision -
// so every link this method ever writes is an explicit operator action;
// source='suggested' has no writer yet, reserved for the enrichment cycle
// this schema is sized to receive without churn).
//
// A link this call KEEPS has its sort_index left exactly as it was - the
// operator's own decision: a document-tab save must not silently reshuffle
// the photo strip's order just because the Documents tab (which knows
// nothing about photo order) happened to resend the same id. Only a link
// this call actually INSERTS gets a fresh sort_index (0 - meaningless for a
// non-photo link, and a photo added this way was never ordered by an
// operator action that cares where it lands; AddEquipmentPhoto is what
// assigns max+1 for an upload that does care).
//
// A docID that doesn't name a real documents row fails the
// equipment_documents.document_id foreign key and rolls back the WHOLE
// replace - deliberately NOT pre-checked here. inventory_handlers.go's own
// PUT handler pre-checks every id itself (via globalDocumentStore.Get) so
// it can name the specific offending id in a clean 404 before ever reaching
// this method; duplicating that same lookup here, only to throw its result
// away and let the FK re-derive the same answer, would be two places
// deciding the identical question. This method's job is only to make the
// write atomic and correct - AGENTS.md's fail-fast policy: the real
// constraint failure surfaces as-is rather than this method inventing its
// own, possibly-differently-worded, version of "unknown document id".
//
// docIDs IS deduped here, first-occurrence order kept: it names "the whole
// set" the record should end up linked to, not a sequence of individual
// link operations, so the same id appearing twice means the same thing as
// it appearing once. Left undeduped, a repeated id would collide with
// equipment_documents' own (equipment_id, document_id) primary key on the
// second INSERT attempt (though the "already linked, skip" check below
// already stops that for any id linked before this call started).
func (s *documentStore) SetEquipmentDocuments(id string, docIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("set equipment documents: begin: %w", err)
	}
	defer tx.Rollback()

	ok, err := rowExists(tx, `SELECT 1 FROM equipment WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("set equipment documents: check equipment: %w", err)
	}
	if !ok {
		return errEquipmentNotFound
	}

	wanted := map[string]bool{}
	ordered := make([]string, 0, len(docIDs))
	for _, docID := range docIDs {
		if wanted[docID] {
			continue
		}
		wanted[docID] = true
		ordered = append(ordered, docID)
	}

	existing := map[string]bool{}
	rows, err := tx.Query(`SELECT document_id FROM equipment_documents WHERE equipment_id = ?`, id)
	if err != nil {
		return fmt.Errorf("set equipment documents: read existing links: %w", err)
	}
	for rows.Next() {
		var docID string
		if err := rows.Scan(&docID); err != nil {
			rows.Close()
			return fmt.Errorf("set equipment documents: scan existing link: %w", err)
		}
		existing[docID] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("set equipment documents: read existing links: %w", err)
	}
	rows.Close()

	for docID := range existing {
		if wanted[docID] {
			continue
		}
		if _, err := tx.Exec(
			`DELETE FROM equipment_documents WHERE equipment_id = ? AND document_id = ?`,
			id, docID,
		); err != nil {
			return fmt.Errorf("set equipment documents: unlink %s: %w", docID, err)
		}
	}

	now := s.now().Unix()
	for _, docID := range ordered {
		if existing[docID] {
			continue
		}
		if _, err := tx.Exec(
			`INSERT INTO equipment_documents (equipment_id, document_id, source, sort_index, created_at) VALUES (?, ?, 'operator', 0, ?)`,
			id, docID, now,
		); err != nil {
			return fmt.Errorf("set equipment documents: link %s: %w", docID, err)
		}
	}

	return tx.Commit()
}

// ── equipment photos ─────────────────────────────────────────────────────
// 2026-09-25 amendment: a photo is no longer a tagged document - it is
// whichever of an item's ordinary linked documents happens to be image/jpeg
// or image/png (photoIDsForEquipmentIDs' own doc comment). AddEquipmentPhoto/
// SetEquipmentPhotoOrder/RemoveEquipmentPhoto below still exist as their own
// methods (the photo strip still wants "link at the end", "reorder", "unlink
// this one" as distinct operations from SetEquipmentDocuments' whole-set
// replace), but none of them write or read a tag any more - they read and
// write equipment_documents links exactly like SetEquipmentDocuments does,
// just scoped to one document at a time or ordered by photoIDsForEquipmentIDs'
// own image-MIME view.

// errEquipmentPhotoSetMismatch is returned by SetEquipmentPhotoOrder when
// order doesn't name EXACTLY the item's current photo id set - ADR 0127:
// "must name exactly the item's current photo set, or it returns 400",
// deliberately not a partial-reorder/subset-allowed API the way
// SetEquipmentDocuments' whole-set replace is for ordinary links, so a
// stale client's PUT can never silently add or drop a photo.
var errEquipmentPhotoSetMismatch = errors.New("photo set does not match the item's current photos")

// errEquipmentPhotoNotFound is returned by RemoveEquipmentPhoto when
// documentID is not currently one of equipmentID's own photos (linked AND
// image/jpeg or image/png) - a stale UI, an id belonging to a different
// item's photo, or a linked document that isn't an image. Distinct from
// errEquipmentNotFound, which means the equipment record itself doesn't
// exist.
var errEquipmentPhotoNotFound = errors.New("photo not found on this item")

// sameIDSet reports whether a and b hold the same ids, in any order -
// SetEquipmentPhotoOrder's own "exactly the current set" check.
func sameIDSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, v := range a {
		counts[v]++
	}
	for _, v := range b {
		counts[v]--
	}
	for _, c := range counts {
		if c != 0 {
			return false
		}
	}
	return true
}

// AddEquipmentPhoto links documentID to equipmentID, at the end of the
// item's current photo order - max(sort_index)+1 among its OWN photo
// (image-MIME) links (ADR 0127: "the first one is the cover"), so a freshly
// uploaded or shared photo always lands after every photo already on the
// strip. A no-op if the link already exists (equipment_documents' PRIMARY
// KEY is (equipment_id, document_id) - re-adding an existing pair, e.g.
// identical photo bytes uploaded twice for the same item, leaves its
// position exactly where it already was rather than erroring). This is also
// the ONLY place a byte-identical upload's dedupe match gets linked
// (inventory_handlers.go's uploadEquipmentPhotoHandler) - 2026-09-25
// amendment: there is no longer a 409 refusal for a sha match that names a
// document the operator filed for some other reason; it is simply linked,
// the same as any other existing document would be.
func (s *documentStore) AddEquipmentPhoto(equipmentID, documentID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("add equipment photo: begin: %w", err)
	}
	defer tx.Rollback()

	ok, err := rowExists(tx, `SELECT 1 FROM equipment WHERE id = ?`, equipmentID)
	if err != nil {
		return fmt.Errorf("add equipment photo: check equipment: %w", err)
	}
	if !ok {
		return errEquipmentNotFound
	}

	already, err := rowExists(tx, `SELECT 1 FROM equipment_documents WHERE equipment_id = ? AND document_id = ?`, equipmentID, documentID)
	if err != nil {
		return fmt.Errorf("add equipment photo: check existing link: %w", err)
	}
	if already {
		return tx.Commit()
	}

	var maxSort sql.NullInt64
	if err := tx.QueryRow(`
		SELECT MAX(ed.sort_index) FROM equipment_documents ed
		JOIN documents d ON d.id = ed.document_id AND `+photoMIMEsClause+`
		WHERE ed.equipment_id = ?`, equipmentID).Scan(&maxSort); err != nil {
		return fmt.Errorf("add equipment photo: max sort: %w", err)
	}
	next := 0
	if maxSort.Valid {
		next = int(maxSort.Int64) + 1
	}

	now := s.now().Unix()
	if _, err := tx.Exec(
		`INSERT INTO equipment_documents (equipment_id, document_id, source, sort_index, created_at) VALUES (?, ?, 'operator', ?, ?)`,
		equipmentID, documentID, next, now,
	); err != nil {
		return fmt.Errorf("add equipment photo: link: %w", err)
	}

	return tx.Commit()
}

// SetEquipmentPhotoOrder rewrites sort_index on equipmentID's existing
// photo links to match order exactly - ADR 0127: "Make cover" is this same
// call with the chosen id moved to the front. order must name EXACTLY the
// item's current photo id set (errEquipmentPhotoSetMismatch otherwise - see
// its own doc comment for why a partial reorder isn't accepted the way
// SetEquipmentDocuments' whole-set replace is for ordinary links).
func (s *documentStore) SetEquipmentPhotoOrder(equipmentID string, order []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("set equipment photo order: begin: %w", err)
	}
	defer tx.Rollback()

	ok, err := rowExists(tx, `SELECT 1 FROM equipment WHERE id = ?`, equipmentID)
	if err != nil {
		return fmt.Errorf("set equipment photo order: check equipment: %w", err)
	}
	if !ok {
		return errEquipmentNotFound
	}

	photoMap, err := photoIDsForEquipmentIDs(tx, []string{equipmentID})
	if err != nil {
		return err
	}
	if !sameIDSet(photoMap[equipmentID], order) {
		return errEquipmentPhotoSetMismatch
	}

	for i, docID := range order {
		if _, err := tx.Exec(
			`UPDATE equipment_documents SET sort_index = ? WHERE equipment_id = ? AND document_id = ?`,
			i, equipmentID, docID,
		); err != nil {
			return fmt.Errorf("set equipment photo order: %w", err)
		}
	}

	return tx.Commit()
}

// RemoveEquipmentPhoto detaches documentID from equipmentID's photo strip -
// UNLINK ONLY (2026-09-25 amendment, superseding the earlier "a photo has no
// life outside its item" rule: the operator's own decision is that removing
// a photo, or deleting the item it belongs to, must never destroy a document
// without being explicitly asked - deleteEquipmentPhotoHandler is what
// offers that choice, via its own `delete` query flag, and does the actual
// document deletion itself, reusing the same DELETE the exported Delete
// method runs; this method never touches the documents table at all any
// more). errEquipmentPhotoNotFound if documentID isn't currently one of
// equipmentID's own photos (linked AND image/jpeg or image/png).
func (s *documentStore) RemoveEquipmentPhoto(equipmentID, documentID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("remove equipment photo: begin: %w", err)
	}
	defer tx.Rollback()

	ok, err := rowExists(tx, `SELECT 1 FROM equipment WHERE id = ?`, equipmentID)
	if err != nil {
		return fmt.Errorf("remove equipment photo: check equipment: %w", err)
	}
	if !ok {
		return errEquipmentNotFound
	}

	isPhoto, err := rowExists(tx, `
		SELECT 1 FROM equipment_documents ed
		JOIN documents d ON d.id = ed.document_id AND `+photoMIMEsClause+`
		WHERE ed.equipment_id = ? AND ed.document_id = ?`, equipmentID, documentID)
	if err != nil {
		return fmt.Errorf("remove equipment photo: check link: %w", err)
	}
	if !isPhoto {
		return errEquipmentPhotoNotFound
	}

	if _, err := tx.Exec(
		`DELETE FROM equipment_documents WHERE equipment_id = ? AND document_id = ?`,
		equipmentID, documentID,
	); err != nil {
		return fmt.Errorf("remove equipment photo: unlink: %w", err)
	}

	return tx.Commit()
}

// DocumentStillLinkedToEquipment reports whether ANY equipment_documents row
// anywhere still references documentID - deleteEquipmentPhotoHandler's own
// "re-check exclusivity server-side at delete time" (inventory_handlers.go):
// called AFTER RemoveEquipmentPhoto's own unlink, so a document another item
// linked in the meantime is never mistaken for orphaned just because it was
// exclusive to THIS item a moment ago.
func (s *documentStore) DocumentStillLinkedToEquipment(documentID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return rowExists(s.db, `SELECT 1 FROM equipment_documents WHERE document_id = ?`, documentID)
}
