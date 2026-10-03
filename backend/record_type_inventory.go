package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Inventory record types (ADR 0158): equipment, locations (zones), bins and
// decks. Each type's create, update and delete are the same transaction-taking
// commands its REST handlers call (inventory_store.go, inventory_decks_store.go).
// A bin's pin and a location's outline on a deck are registered as fields where
// a changeset can sensibly set them; the layout editor stays the operator's
// tool for drawing, since Mate cannot see a plan.

const (
	recordTypeEquipment = "equipment"
	recordTypeLocation  = "location"
	recordTypeBin       = "bin"
	recordTypeDeck      = "deck"
)

func init() {
	defaultRecordRegistry.register(equipmentRecordType())
	defaultRecordRegistry.register(locationRecordType())
	defaultRecordRegistry.register(binRecordType())
	defaultRecordRegistry.register(deckRecordType())
}

// escapePathSegment is encodeURIComponent for a URL path segment, the form
// the frontend uses for a bin's code (lib/app-location.ts).
func escapePathSegment(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

func unixVersion(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func nilStringPtr(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func equipmentSystemNames() []string {
	out := make([]string, 0, len(validEquipmentSystems))
	for k := range validEquipmentSystems {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ── equipment ───────────────────────────────────────────────────────────

func equipmentRecordType() *recordType {
	return &recordType{
		Name:    recordTypeEquipment,
		Label:   "equipment item",
		Summary: "An item in the equipment registry: gear aboard or a spare, with where it lives. Setting bin_id alone files it in that bin's location; setting zone_id alone to a different location takes it out of its old bin.",
		Actions: []string{changeCreate, changeUpdate, changeDelete},
		Filters: map[string]string{"query": "name, maker, model or alias contains this", "system": "one system", "status": "deployed or stored", "zone_id": "in this location", "bin_id": "in this bin"},
		Fields: []recordField{
			{Name: "name", Kind: kindString, Writable: true, Description: "what the item is called"},
			{Name: "system", Kind: kindString, Writable: true, Enum: equipmentSystemNames(), Description: "the system it belongs to"},
			{Name: "manufacturer", Kind: kindString, Writable: true},
			{Name: "model", Kind: kindString, Writable: true},
			{Name: "serial", Kind: kindString, Writable: true},
			{Name: "part_number", Kind: kindString, Writable: true},
			{Name: "quantity", Kind: kindInteger, Writable: true, Description: "how many are on hand"},
			{Name: "required_quantity", Kind: kindInteger, Writable: true, Nullable: true, Description: "how many a spare should be carried; null when nobody has said"},
			{Name: "status", Kind: kindString, Writable: true, Enum: []string{"deployed", "stored"}},
			{Name: "zone_id", Kind: kindID, Writable: true, Nullable: true, Ref: recordTypeLocation, Description: "the location it is in"},
			{Name: "bin_id", Kind: kindID, Writable: true, Nullable: true, Ref: recordTypeBin, Description: "the bin it is in"},
			{Name: "location_detail", Kind: kindString, Writable: true, Description: "free text about where, e.g. \"port side, behind the panel\""},
			{Name: "install_date", Kind: kindDate, Writable: true, Description: "YYYY-MM-DD or blank"},
			{Name: "hour_meter_path", Kind: kindString, Writable: true, Description: "the live hour-meter path, no spaces"},
			{Name: "profile_id", Kind: kindString, Writable: true, Description: "the equipment profile it follows"},
			{Name: "aliases", Kind: kindStringList, Writable: true, Description: "other names it goes by"},
			{Name: "verified_aboard", Kind: kindBoolean, Writable: true, Description: "whether the operator has checked it is on the boat"},
			{Name: "notes", Kind: kindString, Writable: true},
			{Name: "category", Kind: kindString, Description: "mechanical when it has an hour meter, otherwise general"},
			{Name: "zone_name", Kind: kindString, Description: "the name of its location"},
			{Name: "bin_code", Kind: kindString, Description: "the code of its bin"},
		},
		Get:    getEquipmentRecord,
		List:   listEquipmentRecords,
		Create: createEquipmentRecord,
		Update: updateEquipmentRecord,
		Delete: func(env changeEnv, b recordSnapshot) error {
			_, err := deleteEquipmentTx(env.tx, b.ID, false)
			return err
		},
		Effects:  equipmentDeleteEffects,
		Describe: describeEquipment,
		Href:     func(r recordSnapshot) string { return "/inventory/equipment/" + r.ID },
		AfterCommit: func(res changeOpResult) {
			if res.Type == recordTypeEquipment && res.Action == changeUpdate {
				reseedAnomalyRulesIfHouseBank(res.ID)
			}
		},
	}
}

func equipmentSnapshot(it equipmentItem) recordSnapshot {
	aliases := it.Aliases
	if aliases == nil {
		aliases = []string{}
	}
	return recordSnapshot{
		ID: it.ID, Label: it.Name, Version: unixVersion(it.UpdatedAt),
		Fields: map[string]any{
			"name": it.Name, "system": it.System, "manufacturer": it.Manufacturer, "model": it.Model, "serial": it.Serial,
			"part_number": it.PartNumber, "quantity": it.Quantity, "required_quantity": nilInt(it.RequiredQuantity),
			"status": it.Status, "zone_id": nilStringPtr(it.ZoneID), "bin_id": nilStringPtr(it.BinID),
			"location_detail": it.LocationDetail, "install_date": it.InstallDate, "hour_meter_path": it.HourMeterPath,
			"profile_id": it.ProfileID, "aliases": aliases, "verified_aboard": it.VerifiedAboard, "notes": it.Notes,
			"category": it.Category, "zone_name": it.ZoneName, "bin_code": it.BinCode,
		},
	}
}

func getEquipmentRecord(q sqlQueryer, id string) (recordSnapshot, error) {
	it, err := equipmentByID(q, id)
	if errors.Is(err, errEquipmentNotFound) {
		return recordSnapshot{}, errRecordNotFound
	}
	if err != nil {
		return recordSnapshot{}, err
	}
	return equipmentSnapshot(it), nil
}

func listEquipmentRecords(s *documentStore, f map[string]string) ([]recordSnapshot, error) {
	items, err := s.ListEquipment(equipmentFilter{Query: f["query"], System: f["system"], Status: f["status"], ZoneID: f["zone_id"], BinID: f["bin_id"]})
	if err != nil {
		return nil, err
	}
	out := make([]recordSnapshot, len(items))
	for i, it := range items {
		out[i] = equipmentSnapshot(it)
	}
	return out, nil
}

// equipmentRequestFrom is the form-shaped request a set of fields means.
func equipmentRequestFrom(fields map[string]any) (equipmentRequest, error) {
	raw, err := json.Marshal(fields)
	if err != nil {
		return equipmentRequest{}, err
	}
	var req equipmentRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return equipmentRequest{}, fmt.Errorf("read equipment fields: %w", err)
	}
	return req, nil
}

func mapInventoryError(err error) error {
	switch {
	case errors.Is(err, errZoneNameTaken):
		return fieldErr("name", "a location with this name already exists")
	case errors.Is(err, errZoneNameInvalid):
		return fieldErr("name", "a location needs a name")
	case errors.Is(err, errZoneNotFound):
		return fieldErr("zone_id", "no such location")
	case errors.Is(err, errBinCodeTaken):
		return fieldErr("code", "a bin with this code already exists")
	case errors.Is(err, errBinCodeInvalid):
		return fieldErr("code", "a bin needs a code")
	case errors.Is(err, errBinNotFound):
		return fieldErr("bin_id", "no such bin")
	case errors.Is(err, errDeckNameTaken):
		return fieldErr("name", "a deck with this name already exists")
	case errors.Is(err, errDeckNameInvalid):
		return fieldErr("name", "a deck needs a name")
	case errors.Is(err, errDeckNotFound):
		return fieldErr("deck_id", "no such deck")
	case errors.Is(err, errDocumentNotFound):
		return fieldErr("plan_document_id", "no such document (find it with search_documents)")
	case errors.Is(err, errPolygonInvalid):
		return fieldErr("polygon", "%s", err)
	case errors.Is(err, errEquipmentNotFound):
		return fieldErr("id", "no such equipment item")
	case errors.Is(err, errEquipmentNameRequired):
		return fieldErr("name", "an item needs a name")
	case errors.Is(err, errEquipmentLocationMismatch):
		return fieldErr("bin_id", "that bin is not in the given location")
	case errors.Is(err, errEquipmentQuantityInvalid):
		return fieldErr("quantity", "quantity cannot be negative")
	}
	var inUse *inventoryInUseError
	if errors.As(err, &inUse) {
		return &fieldedError{Field: "id", Message: "still in use: " + inUse.Detail + "; move or remove those first", cause: err}
	}
	return err
}

func createEquipmentRecord(env changeEnv, fields fieldSet) (createdRecord, error) {
	req, err := equipmentRequestFrom(fields)
	if err != nil {
		return createdRecord{}, err
	}
	item, verr := validateEquipmentInput(req, "")
	if verr != nil {
		return createdRecord{}, verr
	}
	created, err := createEquipmentTx(env.tx, env.now, item)
	if err != nil {
		return createdRecord{}, mapInventoryError(err)
	}
	return createdRecord{ID: created.ID}, nil
}

func updateEquipmentRecord(env changeEnv, before recordSnapshot, given fieldSet) error {
	merged := mergeFields(before.Fields, given)
	// A bin implies its location, and a bin belongs to one location: moving
	// by bin alone lets the bin say where, and moving to another location
	// takes the item out of its old bin.
	_, hasZone := given["zone_id"]
	_, hasBin := given["bin_id"]
	if hasBin && !hasZone && given["bin_id"] != nil {
		merged["zone_id"] = nil
	}
	if hasZone && !hasBin && !sameFieldValue(before.Fields["zone_id"], given["zone_id"]) {
		merged["bin_id"] = nil
	}
	req, err := equipmentRequestFrom(merged)
	if err != nil {
		return err
	}
	item, verr := validateEquipmentInput(req, stringOf(before.Fields["profile_id"]))
	if verr != nil {
		return verr
	}
	if _, err := updateEquipmentTx(env.tx, env.now, before.ID, item); err != nil {
		return mapInventoryError(err)
	}
	return nil
}

func equipmentDeleteEffects(q sqlQueryer, before recordSnapshot) ([]string, error) {
	count := func(query string) (n int, err error) {
		err = q.QueryRow(query, before.ID).Scan(&n)
		return n, err
	}
	rules, err := count(`SELECT COUNT(*) FROM maintenance_rules WHERE equipment_id = ?`)
	if err != nil {
		return nil, fmt.Errorf("count rules: %w", err)
	}
	entries, err := count(`SELECT COUNT(*) FROM maintenance_log_entries WHERE equipment_id = ?`)
	if err != nil {
		return nil, fmt.Errorf("count log entries: %w", err)
	}
	links, err := count(`SELECT COUNT(*) FROM equipment_documents WHERE equipment_id = ?`)
	if err != nil {
		return nil, fmt.Errorf("count document links: %w", err)
	}
	var out []string
	if rules > 0 {
		out = append(out, fmt.Sprintf("removes its %s", plural(rules, "maintenance rule", "maintenance rules")))
	}
	if entries > 0 {
		out = append(out, fmt.Sprintf("removes its %s", plural(entries, "maintenance log entry", "maintenance log entries")))
	}
	if links > 0 {
		out = append(out, fmt.Sprintf("unlinks %s, which stay in Documents", plural(links, "document", "documents")))
	}
	return out, nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func describeEquipment(d describeInput) string {
	switch d.Op.Action {
	case changeCreate:
		line := "Add " + d.After.Label
		if place := equipmentPlace(d.After.Fields); place != "" {
			line += " in " + place
		}
		return line
	case changeDelete:
		line := "Delete " + d.Before.Label
		if len(d.Effects) > 0 {
			line += " (" + joinEffects(d.Effects) + ")"
		}
		return line
	}
	onlyPlace := true
	for name := range d.Op.Fields {
		if name != "zone_id" && name != "bin_id" && name != "location_detail" {
			onlyPlace = false
		}
	}
	if onlyPlace {
		if place := equipmentPlace(d.After.Fields); place != "" {
			return "Move " + d.Before.Label + " to " + place
		}
		return "Take " + d.Before.Label + " out of its location"
	}
	return genericDescription(d)
}

func joinEffects(effects []string) string {
	out := ""
	for i, e := range effects {
		if i > 0 {
			out += "; "
		}
		out += e
	}
	return out
}

func equipmentPlace(f map[string]any) string {
	bin, zone := stringOf(f["bin_code"]), stringOf(f["zone_name"])
	switch {
	case bin != "" && zone != "":
		return "bin " + bin + " (" + zone + ")"
	case bin != "":
		return "bin " + bin
	}
	return zone
}

// ── locations (zones) ───────────────────────────────────────────────────

func locationRecordType() *recordType {
	return &recordType{
		Name:    recordTypeLocation,
		Label:   "location",
		Summary: "A place aboard that holds bins and equipment (salon, engine room, lazarette). A location can sit on a deck plan with an outline; both deck_id and polygon are set together, or both are null to take it off the plan. Mate cannot see a plan, so leave outlines to the operator unless they gave the points.",
		Actions: []string{changeCreate, changeUpdate, changeDelete},
		Fields: []recordField{
			{Name: "name", Kind: kindString, Writable: true, Description: "unique, case-insensitive"},
			{Name: "deck_id", Kind: kindID, Writable: true, Nullable: true, Ref: recordTypeDeck, Description: "the deck plan it is outlined on"},
			{Name: "polygon", Kind: kindPolygon, Writable: true, Nullable: true, Description: "its outline on the plan: 3 to 64 [x, y] points, each between 0 and 1, x across and y down"},
			{Name: "bin_codes", Kind: kindStringList, Description: "the codes of the bins in it"},
		},
		Get:           getLocationRecord,
		List:          listLocationRecords,
		Create:        createLocationRecord,
		Update:        updateLocationRecord,
		Delete:        func(env changeEnv, b recordSnapshot) error { return mapInventoryErrorOnly(deleteZoneTx(env.tx, b.ID)) },
		Effects:       locationDeleteEffects,
		UpdateEffects: locationUpdateEffects,
		Describe:      describeLocation,
		Href:          func(r recordSnapshot) string { return "/inventory/locations/" + r.ID },
	}
}

func mapInventoryErrorOnly(err error) error {
	if err == nil {
		return nil
	}
	return mapInventoryError(err)
}

func zoneSnapshot(z inventoryZone) recordSnapshot {
	codes := make([]string, len(z.Bins))
	for i, b := range z.Bins {
		codes[i] = b.Code
	}
	var polygon any
	if z.Polygon != nil {
		polygon = z.Polygon
	}
	return recordSnapshot{ID: z.ID, Label: z.Name, Version: unixVersion(z.UpdatedAt),
		Fields: map[string]any{"name": z.Name, "deck_id": nilStringPtr(z.DeckID), "polygon": polygon, "bin_codes": codes}}
}

func getLocationRecord(q sqlQueryer, id string) (recordSnapshot, error) {
	z, err := zoneByID(q, id)
	if errors.Is(err, errZoneNotFound) {
		return recordSnapshot{}, errRecordNotFound
	}
	if err != nil {
		return recordSnapshot{}, err
	}
	return zoneSnapshot(z), nil
}

func listLocationRecords(s *documentStore, _ map[string]string) ([]recordSnapshot, error) {
	zones, err := s.ListZones()
	if err != nil {
		return nil, err
	}
	out := make([]recordSnapshot, len(zones))
	for i, z := range zones {
		out[i] = zoneSnapshot(z)
	}
	return out, nil
}

func polygonOf(v any) [][2]float64 {
	p, _ := v.([][2]float64)
	return p
}

// placeZoneTx puts a location on a deck with an outline, or takes it off
// (deckID empty). Its bins lose their pins either way, since a pin only
// means something against the outline it was set in.
func placeZoneTx(tx *sql.Tx, now time.Time, zoneID, deckID string, polygon [][2]float64) error {
	if _, err := tx.Exec(`UPDATE inventory_bins SET pin_x = NULL, pin_y = NULL WHERE zone_id = ?`, zoneID); err != nil {
		return fmt.Errorf("place location: clear pins: %w", err)
	}
	if deckID == "" {
		_, err := tx.Exec(`UPDATE inventory_zones SET deck_id = NULL, polygon = NULL, updated_at = ? WHERE id = ?`, now.Unix(), zoneID)
		return err
	}
	if _, err := deckByID(tx, deckID); err != nil {
		return err
	}
	if err := validatePolygon(polygon); err != nil {
		return err
	}
	poly, err := json.Marshal(polygon)
	if err != nil {
		return fmt.Errorf("place location: encode polygon: %w", err)
	}
	_, err = tx.Exec(`UPDATE inventory_zones SET deck_id = ?, polygon = ?, updated_at = ? WHERE id = ?`, deckID, string(poly), now.Unix(), zoneID)
	return err
}

func checkPlacement(deckID string, polygon [][2]float64) error {
	if deckID == "" && polygon != nil {
		return fieldErr("polygon", "a location must be on a deck to have an outline; set deck_id too")
	}
	if deckID != "" && polygon == nil {
		return fieldErr("polygon", "a location on a deck needs an outline (3 to 64 [x, y] points between 0 and 1); the operator can draw it on the deck's plan page instead")
	}
	return nil
}

func createLocationRecord(env changeEnv, fields fieldSet) (createdRecord, error) {
	deckID, polygon := stringOf(fields["deck_id"]), polygonOf(fields["polygon"])
	if err := checkPlacement(deckID, polygon); err != nil {
		return createdRecord{}, err
	}
	z, err := createZoneTx(env.tx, env.now, stringOf(fields["name"]))
	if err != nil {
		return createdRecord{}, mapInventoryError(err)
	}
	if deckID != "" {
		if err := placeZoneTx(env.tx, env.now, z.ID, deckID, polygon); err != nil {
			return createdRecord{}, mapInventoryError(err)
		}
	}
	return createdRecord{ID: z.ID}, nil
}

func updateLocationRecord(env changeEnv, before recordSnapshot, given fieldSet) error {
	if v, ok := given["name"]; ok && !sameFieldValue(before.Fields["name"], v) {
		if _, err := updateZoneTx(env.tx, env.now, before.ID, stringOf(v)); err != nil {
			return mapInventoryError(err)
		}
	}
	_, hasDeck := given["deck_id"]
	_, hasPoly := given["polygon"]
	if hasDeck || hasPoly {
		merged := mergeFields(before.Fields, given)
		deckID, polygon := stringOf(merged["deck_id"]), polygonOf(merged["polygon"])
		if hasDeck && given["deck_id"] == nil && !hasPoly {
			polygon = nil
		}
		if err := checkPlacement(deckID, polygon); err != nil {
			return err
		}
		if err := placeZoneTx(env.tx, env.now, before.ID, deckID, polygon); err != nil {
			return mapInventoryError(err)
		}
	}
	return nil
}

func locationDeleteEffects(q sqlQueryer, before recordSnapshot) ([]string, error) {
	if before.Fields["deck_id"] != nil {
		return []string{"takes its outline off the deck plan"}, nil
	}
	return nil, nil
}

// locationUpdateEffects warns when a placement change will clear bin pins:
// placeZoneTx unpins every bin in the location whenever deck_id or polygon
// is given.
func locationUpdateEffects(q sqlQueryer, before recordSnapshot, given fieldSet) ([]string, error) {
	_, hasDeck := given["deck_id"]
	_, hasPoly := given["polygon"]
	if !hasDeck && !hasPoly {
		return nil, nil
	}
	var n int
	if err := q.QueryRow(`SELECT COUNT(*) FROM inventory_bins WHERE zone_id = ? AND pin_x IS NOT NULL`, before.ID).Scan(&n); err != nil {
		return nil, fmt.Errorf("count pinned bins: %w", err)
	}
	if n == 0 {
		return nil, nil
	}
	return []string{"clears the pins of " + plural(n, "bin", "bins")}, nil
}

func describeLocation(d describeInput) string {
	switch d.Op.Action {
	case changeCreate:
		return "Add location " + d.After.Label
	case changeDelete:
		line := "Delete location " + d.Before.Label
		if len(d.Effects) > 0 {
			line += " (" + joinEffects(d.Effects) + ")"
		}
		return line
	}
	var parts []string
	if after := stringOf(d.After.Fields["name"]); after != stringOf(d.Before.Fields["name"]) {
		parts = append(parts, fmt.Sprintf("rename to %q", after))
	}
	_, hasDeck := d.Op.Fields["deck_id"]
	_, hasPoly := d.Op.Fields["polygon"]
	if hasDeck || hasPoly {
		if d.After.Fields["deck_id"] == nil {
			parts = append(parts, "take it off the deck plan")
		} else {
			parts = append(parts, "put it on a deck plan with an outline")
		}
	}
	line := "Change location " + d.Before.Label + ": " + joinEffects(parts)
	if len(d.Effects) > 0 {
		line += " (" + joinEffects(d.Effects) + ")"
	}
	return line
}

// ── bins ────────────────────────────────────────────────────────────────

func binRecordType() *recordType {
	return &recordType{
		Name:    recordTypeBin,
		Label:   "bin",
		Summary: "A numbered container filed in one location. A bin cannot move to another location once made.",
		Actions: []string{changeCreate, changeUpdate, changeDelete},
		Filters: map[string]string{"zone_id": "only bins in this location"},
		Fields: []recordField{
			{Name: "zone_id", Kind: kindID, Writable: true, Ref: recordTypeLocation, Description: "the location it is in; set on create only"},
			{Name: "code", Kind: kindString, Writable: true, Description: "its printed code, unique boat-wide, case-insensitive"},
			{Name: "name", Kind: kindString, Writable: true, Description: "what it holds, in a few words"},
			{Name: "zone_name", Kind: kindString, Description: "the name of its location"},
		},
		Get: getBinRecord,
		List: func(s *documentStore, f map[string]string) ([]recordSnapshot, error) {
			zones, err := s.ListZones()
			if err != nil {
				return nil, err
			}
			var out []recordSnapshot
			for _, z := range zones {
				if f["zone_id"] != "" && f["zone_id"] != z.ID {
					continue
				}
				for _, b := range z.Bins {
					out = append(out, binSnapshot(b, z.Name))
				}
			}
			return out, nil
		},
		Create: func(env changeEnv, fields fieldSet) (createdRecord, error) {
			b, err := createBinTx(env.tx, env.now, stringOf(fields["zone_id"]), stringOf(fields["code"]), stringOf(fields["name"]))
			if err != nil {
				return createdRecord{}, mapInventoryError(err)
			}
			return createdRecord{ID: b.ID}, nil
		},
		Update: func(env changeEnv, before recordSnapshot, given fieldSet) error {
			if v, ok := given["zone_id"]; ok && !sameFieldValue(before.Fields["zone_id"], v) {
				return fieldErr("zone_id", "a bin cannot move to another location; make a new bin there")
			}
			merged := mergeFields(before.Fields, given)
			_, err := updateBinTx(env.tx, env.now, before.ID, stringOf(merged["code"]), stringOf(merged["name"]))
			return mapInventoryErrorOnly(err)
		},
		Delete: func(env changeEnv, b recordSnapshot) error { return mapInventoryErrorOnly(deleteBinTx(env.tx, b.ID)) },
		Href: func(r recordSnapshot) string {
			return "/inventory/bins/" + escapePathSegment(stringOf(r.Fields["code"]))
		},
	}
}

func binSnapshot(b inventoryBin, zoneName string) recordSnapshot {
	label := b.Code
	if b.Name != "" {
		label += " (" + b.Name + ")"
	}
	return recordSnapshot{ID: b.ID, Label: label, Version: unixVersion(b.UpdatedAt),
		Fields: map[string]any{"zone_id": b.ZoneID, "code": b.Code, "name": b.Name, "zone_name": zoneName}}
}

func getBinRecord(q sqlQueryer, id string) (recordSnapshot, error) {
	b, err := binByID(q, id)
	if errors.Is(err, errBinNotFound) {
		return recordSnapshot{}, errRecordNotFound
	}
	if err != nil {
		return recordSnapshot{}, err
	}
	var zoneName string
	if err := q.QueryRow(`SELECT name FROM inventory_zones WHERE id = ?`, b.ZoneID).Scan(&zoneName); err != nil {
		return recordSnapshot{}, fmt.Errorf("bin location name: %w", err)
	}
	return binSnapshot(b, zoneName), nil
}

// ── decks ───────────────────────────────────────────────────────────────

func deckRecordType() *recordType {
	return &recordType{
		Name:    recordTypeDeck,
		Label:   "deck",
		Summary: "A deck plan: a named deck with a picture of its plan. The plan must be a JPEG or PNG image document; a page of a PDF cannot be a plan yet, so ask the operator to upload a picture of that page. Mate cannot see the picture: the card shows it to the operator, who checks it is the right one.",
		Actions: []string{changeCreate, changeUpdate, changeDelete},
		Fields: []recordField{
			{Name: "name", Kind: kindString, Writable: true, Description: "unique, case-insensitive"},
			{Name: "plan_document_id", Kind: kindID, Writable: true, Ref: "document", Description: "the id of the JPEG or PNG document to use as the plan (from search_documents)"},
		},
		Get: getDeckRecord,
		List: func(s *documentStore, _ map[string]string) ([]recordSnapshot, error) {
			decks, err := s.ListDecks()
			if err != nil {
				return nil, err
			}
			out := make([]recordSnapshot, len(decks))
			for i, d := range decks {
				out[i] = deckSnapshot(d)
			}
			return out, nil
		},
		Create: func(env changeEnv, fields fieldSet) (createdRecord, error) {
			d, err := createDeckTx(env.tx, env.now, stringOf(fields["name"]))
			if err != nil {
				return createdRecord{}, mapInventoryError(err)
			}
			if plan := stringOf(fields["plan_document_id"]); plan != "" {
				if _, err := setDeckPlanTx(env.tx, env.now, d.ID, plan); err != nil {
					return createdRecord{}, mapInventoryError(err)
				}
			}
			return createdRecord{ID: d.ID}, nil
		},
		Update: func(env changeEnv, before recordSnapshot, given fieldSet) error {
			if v, ok := given["name"]; ok && !sameFieldValue(before.Fields["name"], v) {
				if _, err := updateDeckTx(env.tx, env.now, before.ID, stringOf(v)); err != nil {
					return mapInventoryError(err)
				}
			}
			if v, ok := given["plan_document_id"]; ok && !sameFieldValue(before.Fields["plan_document_id"], v) {
				if _, err := setDeckPlanTx(env.tx, env.now, before.ID, stringOf(v)); err != nil {
					return mapInventoryError(err)
				}
			}
			return nil
		},
		Delete: func(env changeEnv, b recordSnapshot) error {
			_, err := deleteDeckTx(env.tx, env.now, b.ID)
			return mapInventoryErrorOnly(err)
		},
		Effects: func(q sqlQueryer, before recordSnapshot) ([]string, error) {
			var n int
			if err := q.QueryRow(`SELECT COUNT(*) FROM inventory_zones WHERE deck_id = ?`, before.ID).Scan(&n); err != nil {
				return nil, fmt.Errorf("count outlines: %w", err)
			}
			var out []string
			if n > 0 {
				out = append(out, "removes the outlines of "+plural(n, "location", "locations")+" and the pins of their bins")
			}
			if before.Fields["plan_document_id"] != nil {
				out = append(out, "the plan picture stays in Documents")
			}
			return out, nil
		},
		Describe: func(d describeInput) string {
			switch d.Op.Action {
			case changeCreate:
				line := "Add deck " + d.After.Label
				if d.After.Fields["plan_document_id"] != nil {
					line += " with a plan picture"
				}
				return line
			case changeDelete:
				line := "Delete deck " + d.Before.Label
				if len(d.Effects) > 0 {
					line += " (" + joinEffects(d.Effects) + ")"
				}
				return line
			}
			var parts []string
			if after := stringOf(d.After.Fields["name"]); after != stringOf(d.Before.Fields["name"]) {
				parts = append(parts, fmt.Sprintf("rename to %q", after))
			}
			if _, ok := d.Op.Fields["plan_document_id"]; ok {
				parts = append(parts, "use a different plan picture")
			}
			return "Change deck " + d.Before.Label + ": " + joinEffects(parts)
		},
		Href: func(r recordSnapshot) string { return "/inventory/decks/" + r.ID },
	}
}

func deckSnapshot(d inventoryDeck) recordSnapshot {
	return recordSnapshot{ID: d.ID, Label: d.Name, Version: unixVersion(d.UpdatedAt),
		Fields: map[string]any{"name": d.Name, "plan_document_id": nilStringPtr(d.PlanDocumentID)}}
}

func getDeckRecord(q sqlQueryer, id string) (recordSnapshot, error) {
	d, err := deckByID(q, id)
	if errors.Is(err, errDeckNotFound) {
		return recordSnapshot{}, errRecordNotFound
	}
	if err != nil {
		return recordSnapshot{}, err
	}
	return deckSnapshot(d), nil
}
