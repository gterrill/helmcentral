package main

import (
	"errors"
	"testing"
)

// This file exercises inventory_store.go: the equipment registry's zones,
// bins and equipment records (plan "Inventory: the equipment registry and
// locations", Phase 0). Written before inventory_store.go itself
// (AGENTS.md's test-first policy) - every function and sentinel these tests
// reference is designed here first, then implemented to make them pass.

// ── zones ────────────────────────────────────────────────────────────────

func TestDocumentStore_CreateZoneRoundTrips(t *testing.T) {
	store := newTestDocumentStore(t)

	zone, err := store.CreateZone("Engine room (stbd)")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	if zone.Name != "Engine room (stbd)" {
		t.Fatalf("expected the given name, got %q", zone.Name)
	}
	if zone.ID == "" {
		t.Fatalf("expected an assigned id")
	}
	if len(zone.Bins) != 0 {
		t.Fatalf("expected a brand new zone to have no bins, got %+v", zone.Bins)
	}

	zones, err := store.ListZones()
	if err != nil {
		t.Fatalf("ListZones: %v", err)
	}
	if len(zones) != 1 || zones[0].ID != zone.ID {
		t.Fatalf("expected the new zone to be listed, got %+v", zones)
	}
}

func TestDocumentStore_CreateZoneTrimsNameAndRejectsBlank(t *testing.T) {
	store := newTestDocumentStore(t)

	zone, err := store.CreateZone("  Salon  ")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	if zone.Name != "Salon" {
		t.Fatalf("expected the trimmed name, got %q", zone.Name)
	}

	if _, err := store.CreateZone("   "); !errors.Is(err, errZoneNameInvalid) {
		t.Fatalf("expected errZoneNameInvalid for a blank name, got %v", err)
	}
}

// TestDocumentStore_CreateZoneRejectsCaseInsensitiveDuplicateName pins the
// unique index on lower(name) (documents_store.go's schema).
func TestDocumentStore_CreateZoneRejectsCaseInsensitiveDuplicateName(t *testing.T) {
	store := newTestDocumentStore(t)

	if _, err := store.CreateZone("Lazarette"); err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	if _, err := store.CreateZone("lazarette"); !errors.Is(err, errZoneNameTaken) {
		t.Fatalf("expected errZoneNameTaken case-insensitively, got %v", err)
	}
}

func TestDocumentStore_UpdateZoneRenames(t *testing.T) {
	store := newTestDocumentStore(t)

	zone, err := store.CreateZone("Salon")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}

	updated, err := store.UpdateZone(zone.ID, "Saloon")
	if err != nil {
		t.Fatalf("UpdateZone: %v", err)
	}
	if updated.Name != "Saloon" {
		t.Fatalf("expected the renamed value, got %q", updated.Name)
	}

	zones, err := store.ListZones()
	if err != nil {
		t.Fatalf("ListZones: %v", err)
	}
	if len(zones) != 1 || zones[0].Name != "Saloon" {
		t.Fatalf("expected the rename to persist, got %+v", zones)
	}
}

func TestDocumentStore_UpdateZoneUnknownIDReturnsNotFound(t *testing.T) {
	store := newTestDocumentStore(t)
	if _, err := store.UpdateZone("does-not-exist", "X"); !errors.Is(err, errZoneNotFound) {
		t.Fatalf("expected errZoneNotFound, got %v", err)
	}
}

func TestDocumentStore_DeleteZoneRemovesAnUnusedZone(t *testing.T) {
	store := newTestDocumentStore(t)

	zone, err := store.CreateZone("Salon")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	if err := store.DeleteZone(zone.ID); err != nil {
		t.Fatalf("DeleteZone: %v", err)
	}

	zones, err := store.ListZones()
	if err != nil {
		t.Fatalf("ListZones: %v", err)
	}
	if len(zones) != 0 {
		t.Fatalf("expected no zones left, got %+v", zones)
	}
}

func TestDocumentStore_DeleteZoneUnknownIDReturnsNotFound(t *testing.T) {
	store := newTestDocumentStore(t)
	if err := store.DeleteZone("does-not-exist"); !errors.Is(err, errZoneNotFound) {
		t.Fatalf("expected errZoneNotFound, got %v", err)
	}
}

// TestDocumentStore_DeleteZoneWithABinIsRefusedWithCount is a required
// Verification case (task item 3): RESTRICT maps to errZoneInUse carrying
// the exact count, computed by a pre-check COUNT(*) rather than parsed out
// of SQLite's own constraint-violation error text.
func TestDocumentStore_DeleteZoneWithABinIsRefusedWithCount(t *testing.T) {
	store := newTestDocumentStore(t)

	zone, err := store.CreateZone("Lazarette")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	if _, err := store.CreateBin(zone.ID, "LAZ-01", "Spares"); err != nil {
		t.Fatalf("CreateBin: %v", err)
	}

	err = store.DeleteZone(zone.ID)
	if !errors.Is(err, errZoneInUse) {
		t.Fatalf("expected errZoneInUse, got %v", err)
	}
	var inUse *inventoryInUseError
	if !errors.As(err, &inUse) {
		t.Fatalf("expected *inventoryInUseError, got %T (%v)", err, err)
	}
	if inUse.Count != 1 {
		t.Fatalf("expected in_use count 1 (the one bin), got %d", inUse.Count)
	}

	zones, err := store.ListZones()
	if err != nil {
		t.Fatalf("ListZones: %v", err)
	}
	if len(zones) != 1 {
		t.Fatalf("expected the zone to survive the refused delete, got %+v", zones)
	}
}

// TestDocumentStore_DeleteZoneWithDirectlyAssignedEquipmentIsRefused covers
// the OTHER thing that can hold a zone in place: an equipment item pointing
// at it with no bin at all.
func TestDocumentStore_DeleteZoneWithDirectlyAssignedEquipmentIsRefused(t *testing.T) {
	store := newTestDocumentStore(t)

	zone, err := store.CreateZone("Engine room")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	if _, err := store.CreateEquipment(equipmentItem{Name: "Generator", Category: "mechanical", ZoneID: &zone.ID}); err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}

	err = store.DeleteZone(zone.ID)
	if !errors.Is(err, errZoneInUse) {
		t.Fatalf("expected errZoneInUse, got %v", err)
	}
}

// ── bins ─────────────────────────────────────────────────────────────────

func TestDocumentStore_CreateBinRoundTrips(t *testing.T) {
	store := newTestDocumentStore(t)

	zone, err := store.CreateZone("Salon")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	bin, err := store.CreateBin(zone.ID, "SAL-04", "Life jackets")
	if err != nil {
		t.Fatalf("CreateBin: %v", err)
	}
	if bin.Code != "SAL-04" || bin.Name != "Life jackets" || bin.ZoneID != zone.ID {
		t.Fatalf("expected the given fields to round-trip, got %+v", bin)
	}

	zones, err := store.ListZones()
	if err != nil {
		t.Fatalf("ListZones: %v", err)
	}
	if len(zones) != 1 || len(zones[0].Bins) != 1 || zones[0].Bins[0].ID != bin.ID {
		t.Fatalf("expected the bin nested under its zone, got %+v", zones)
	}
}

func TestDocumentStore_CreateBinUnknownZoneReturnsNotFound(t *testing.T) {
	store := newTestDocumentStore(t)
	if _, err := store.CreateBin("does-not-exist", "X-01", ""); !errors.Is(err, errZoneNotFound) {
		t.Fatalf("expected errZoneNotFound, got %v", err)
	}
}

func TestDocumentStore_CreateBinRejectsBlankCode(t *testing.T) {
	store := newTestDocumentStore(t)
	zone, err := store.CreateZone("Salon")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	if _, err := store.CreateBin(zone.ID, "   ", "X"); !errors.Is(err, errBinCodeInvalid) {
		t.Fatalf("expected errBinCodeInvalid, got %v", err)
	}
}

// TestDocumentStore_CreateBinRejectsCaseInsensitiveDuplicateCode pins the
// unique index on lower(code) - a bin's printable code is unique boat-wide,
// not per zone (documents_store.go's schema comment on inventory_bins_code).
func TestDocumentStore_CreateBinRejectsCaseInsensitiveDuplicateCode(t *testing.T) {
	store := newTestDocumentStore(t)

	zoneA, err := store.CreateZone("Salon")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	zoneB, err := store.CreateZone("Lazarette")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	if _, err := store.CreateBin(zoneA.ID, "SAL-04", ""); err != nil {
		t.Fatalf("CreateBin: %v", err)
	}
	if _, err := store.CreateBin(zoneB.ID, "sal-04", ""); !errors.Is(err, errBinCodeTaken) {
		t.Fatalf("expected errBinCodeTaken case-insensitively across zones, got %v", err)
	}
}

func TestDocumentStore_UpdateBinRenamesAndRecodes(t *testing.T) {
	store := newTestDocumentStore(t)

	zone, err := store.CreateZone("Salon")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	bin, err := store.CreateBin(zone.ID, "SAL-04", "Life jackets")
	if err != nil {
		t.Fatalf("CreateBin: %v", err)
	}

	updated, err := store.UpdateBin(bin.ID, "SAL-05", "PFDs")
	if err != nil {
		t.Fatalf("UpdateBin: %v", err)
	}
	if updated.Code != "SAL-05" || updated.Name != "PFDs" || updated.ZoneID != zone.ID {
		t.Fatalf("expected the updated fields with the zone unchanged, got %+v", updated)
	}
}

func TestDocumentStore_UpdateBinUnknownIDReturnsNotFound(t *testing.T) {
	store := newTestDocumentStore(t)
	if _, err := store.UpdateBin("does-not-exist", "X-01", ""); !errors.Is(err, errBinNotFound) {
		t.Fatalf("expected errBinNotFound, got %v", err)
	}
}

func TestDocumentStore_DeleteBinRemovesAnUnusedBin(t *testing.T) {
	store := newTestDocumentStore(t)

	zone, err := store.CreateZone("Salon")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	bin, err := store.CreateBin(zone.ID, "SAL-04", "")
	if err != nil {
		t.Fatalf("CreateBin: %v", err)
	}
	if err := store.DeleteBin(bin.ID); err != nil {
		t.Fatalf("DeleteBin: %v", err)
	}

	zones, err := store.ListZones()
	if err != nil {
		t.Fatalf("ListZones: %v", err)
	}
	if len(zones[0].Bins) != 0 {
		t.Fatalf("expected the bin gone, got %+v", zones[0].Bins)
	}
}

// TestDocumentStore_DeleteBinWithEquipmentIsRefusedWithCount is the bin half
// of task item 3's required RESTRICT/count coverage.
func TestDocumentStore_DeleteBinWithEquipmentIsRefusedWithCount(t *testing.T) {
	store := newTestDocumentStore(t)

	zone, err := store.CreateZone("Lazarette")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	bin, err := store.CreateBin(zone.ID, "LAZ-01", "")
	if err != nil {
		t.Fatalf("CreateBin: %v", err)
	}
	if _, err := store.CreateEquipment(equipmentItem{Name: "Spare impeller", Category: "general", BinID: &bin.ID}); err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}

	err = store.DeleteBin(bin.ID)
	if !errors.Is(err, errBinInUse) {
		t.Fatalf("expected errBinInUse, got %v", err)
	}
	var inUse *inventoryInUseError
	if !errors.As(err, &inUse) {
		t.Fatalf("expected *inventoryInUseError, got %T (%v)", err, err)
	}
	if inUse.Count != 1 {
		t.Fatalf("expected in_use count 1, got %d", inUse.Count)
	}
}

// ── equipment ────────────────────────────────────────────────────────────

func TestDocumentStore_CreateEquipmentRoundTripsAliasesZoneAndBin(t *testing.T) {
	store := newTestDocumentStore(t)

	zone, err := store.CreateZone("Engine room (stbd)")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	bin, err := store.CreateBin(zone.ID, "ER-01", "Filters")
	if err != nil {
		t.Fatalf("CreateBin: %v", err)
	}

	item, err := store.CreateEquipment(equipmentItem{
		Name:         "Generator",
		Category:     "mechanical",
		System:       "electrical",
		Manufacturer: "Onan",
		Model:        "13.5kW",
		ZoneID:       &zone.ID,
		BinID:        &bin.ID,
		// A padded duplicate ("genset" again after trimming) must collapse,
		// and so must "Genset", which differs only in case: alias matching is
		// case-insensitive, so that is one alias spelled twice. The dedupe key
		// is folded but the stored spelling is whichever the operator typed
		// first, so the surviving pair is the original "genset" plus "donk".
		Aliases: []string{" genset ", "genset", "Genset", "donk"},
	})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	if len(item.Aliases) != 2 || item.Aliases[0] != "genset" || item.Aliases[1] != "donk" {
		t.Fatalf("expected aliases trimmed, deduped and order-preserved, got %+v", item.Aliases)
	}
	if item.ZoneID == nil || *item.ZoneID != zone.ID {
		t.Fatalf("expected the given zone_id, got %+v", item.ZoneID)
	}
	if item.BinID == nil || *item.BinID != bin.ID {
		t.Fatalf("expected the given bin_id, got %+v", item.BinID)
	}
	if item.ZoneName != "Engine room (stbd)" || item.BinCode != "ER-01" {
		t.Fatalf("expected the joined zone name and bin code, got zone=%q bin=%q", item.ZoneName, item.BinCode)
	}
	if item.Status != "deployed" {
		t.Fatalf("expected status to default to deployed, got %q", item.Status)
	}
	if item.Quantity != 1 {
		t.Fatalf("expected quantity to default to 1, got %d", item.Quantity)
	}

	got, err := store.GetEquipment(item.ID)
	if err != nil {
		t.Fatalf("GetEquipment: %v", err)
	}
	if got.Name != "Generator" || got.Manufacturer != "Onan" || len(got.Aliases) != 2 {
		t.Fatalf("expected GetEquipment to round-trip the created row, got %+v", got)
	}
}

func TestDocumentStore_CreateEquipmentDerivesZoneFromBinWhenZoneOmitted(t *testing.T) {
	store := newTestDocumentStore(t)

	zone, err := store.CreateZone("Lazarette")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	bin, err := store.CreateBin(zone.ID, "LAZ-01", "")
	if err != nil {
		t.Fatalf("CreateBin: %v", err)
	}

	item, err := store.CreateEquipment(equipmentItem{Name: "Spare impeller", Category: "general", BinID: &bin.ID})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	if item.ZoneID == nil || *item.ZoneID != zone.ID {
		t.Fatalf("expected zone_id derived from the bin's own zone, got %+v", item.ZoneID)
	}
}

// TestDocumentStore_CreateEquipmentRejectsBinFromAnotherZone is a required
// Verification case: a bin implies its zone, and a caller supplying BOTH
// that disagree gets a validation error, not a silent fix (plan's own
// decision text).
func TestDocumentStore_CreateEquipmentRejectsBinFromAnotherZone(t *testing.T) {
	store := newTestDocumentStore(t)

	zoneA, err := store.CreateZone("Salon")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	zoneB, err := store.CreateZone("Lazarette")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	bin, err := store.CreateBin(zoneB.ID, "LAZ-01", "")
	if err != nil {
		t.Fatalf("CreateBin: %v", err)
	}

	_, err = store.CreateEquipment(equipmentItem{Name: "X", Category: "general", ZoneID: &zoneA.ID, BinID: &bin.ID})
	if !errors.Is(err, errEquipmentLocationMismatch) {
		t.Fatalf("expected errEquipmentLocationMismatch, got %v", err)
	}
}

func TestDocumentStore_CreateEquipmentUnknownZoneReturnsNotFound(t *testing.T) {
	store := newTestDocumentStore(t)
	badZone := "does-not-exist"
	if _, err := store.CreateEquipment(equipmentItem{Name: "X", Category: "general", ZoneID: &badZone}); !errors.Is(err, errZoneNotFound) {
		t.Fatalf("expected errZoneNotFound, got %v", err)
	}
}

func TestDocumentStore_CreateEquipmentUnknownBinReturnsNotFound(t *testing.T) {
	store := newTestDocumentStore(t)
	badBin := "does-not-exist"
	if _, err := store.CreateEquipment(equipmentItem{Name: "X", Category: "general", BinID: &badBin}); !errors.Is(err, errBinNotFound) {
		t.Fatalf("expected errBinNotFound, got %v", err)
	}
}

func TestDocumentStore_CreateEquipmentRequiresName(t *testing.T) {
	store := newTestDocumentStore(t)
	if _, err := store.CreateEquipment(equipmentItem{Category: "general"}); !errors.Is(err, errEquipmentNameRequired) {
		t.Fatalf("expected errEquipmentNameRequired, got %v", err)
	}
}

func TestDocumentStore_CreateEquipmentRejectsInvalidCategory(t *testing.T) {
	store := newTestDocumentStore(t)
	if _, err := store.CreateEquipment(equipmentItem{Name: "X", Category: "bogus"}); !errors.Is(err, errEquipmentInvalidCategory) {
		t.Fatalf("expected errEquipmentInvalidCategory, got %v", err)
	}
}

func TestDocumentStore_UpdateEquipmentReplacesFields(t *testing.T) {
	store := newTestDocumentStore(t)

	item, err := store.CreateEquipment(equipmentItem{Name: "Generator", Category: "mechanical", System: "electrical"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}

	updated, err := store.UpdateEquipment(item.ID, equipmentItem{
		Name: "Generator (Onan)", Category: "mechanical", System: "electrical",
		Manufacturer: "Onan", Status: "stored", Quantity: 2, Aliases: []string{"genset"},
	})
	if err != nil {
		t.Fatalf("UpdateEquipment: %v", err)
	}
	if updated.Name != "Generator (Onan)" || updated.Manufacturer != "Onan" || updated.Status != "stored" || updated.Quantity != 2 {
		t.Fatalf("expected the replaced fields, got %+v", updated)
	}
	if len(updated.Aliases) != 1 || updated.Aliases[0] != "genset" {
		t.Fatalf("expected the new alias list, got %+v", updated.Aliases)
	}
}

func TestDocumentStore_UpdateEquipmentUnknownIDReturnsNotFound(t *testing.T) {
	store := newTestDocumentStore(t)
	if _, err := store.UpdateEquipment("does-not-exist", equipmentItem{Name: "X", Category: "general"}); !errors.Is(err, errEquipmentNotFound) {
		t.Fatalf("expected errEquipmentNotFound, got %v", err)
	}
}

func TestDocumentStore_DeleteEquipmentRemovesRow(t *testing.T) {
	store := newTestDocumentStore(t)

	item, err := store.CreateEquipment(equipmentItem{Name: "Spare impeller", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	if err := store.DeleteEquipment(item.ID); err != nil {
		t.Fatalf("DeleteEquipment: %v", err)
	}
	if _, err := store.GetEquipment(item.ID); !errors.Is(err, errEquipmentNotFound) {
		t.Fatalf("expected errEquipmentNotFound after delete, got %v", err)
	}
}

func TestDocumentStore_DeleteEquipmentUnknownIDReturnsNotFound(t *testing.T) {
	store := newTestDocumentStore(t)
	if err := store.DeleteEquipment("does-not-exist"); !errors.Is(err, errEquipmentNotFound) {
		t.Fatalf("expected errEquipmentNotFound, got %v", err)
	}
}

// TestDocumentStore_ListEquipmentFiltersByCategorySystemStatusZoneAndQuery
// is a required Verification case covering every ListEquipment filter,
// including a query hit that only matches through an alias (task/plan:
// "aliases in Go").
func TestDocumentStore_ListEquipmentFiltersByCategorySystemStatusZoneAndQuery(t *testing.T) {
	store := newTestDocumentStore(t)

	zone, err := store.CreateZone("Engine room")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	generator, err := store.CreateEquipment(equipmentItem{
		Name: "Generator", Category: "mechanical", System: "electrical", Status: "deployed",
		ZoneID: &zone.ID, Aliases: []string{"genset"},
	})
	if err != nil {
		t.Fatalf("CreateEquipment (generator): %v", err)
	}
	impeller, err := store.CreateEquipment(equipmentItem{
		Name: "Spare impeller", Category: "general", System: "propulsion", Status: "stored",
	})
	if err != nil {
		t.Fatalf("CreateEquipment (impeller): %v", err)
	}

	byCategory, err := store.ListEquipment(equipmentFilter{Category: "general"})
	if err != nil {
		t.Fatalf("ListEquipment (category): %v", err)
	}
	if len(byCategory) != 1 || byCategory[0].ID != impeller.ID {
		t.Fatalf("expected only the impeller by category, got %+v", byCategory)
	}

	bySystem, err := store.ListEquipment(equipmentFilter{System: "electrical"})
	if err != nil {
		t.Fatalf("ListEquipment (system): %v", err)
	}
	if len(bySystem) != 1 || bySystem[0].ID != generator.ID {
		t.Fatalf("expected only the generator by system, got %+v", bySystem)
	}

	byStatus, err := store.ListEquipment(equipmentFilter{Status: "stored"})
	if err != nil {
		t.Fatalf("ListEquipment (status): %v", err)
	}
	if len(byStatus) != 1 || byStatus[0].ID != impeller.ID {
		t.Fatalf("expected only the impeller by status, got %+v", byStatus)
	}

	byZone, err := store.ListEquipment(equipmentFilter{ZoneID: zone.ID})
	if err != nil {
		t.Fatalf("ListEquipment (zone): %v", err)
	}
	if len(byZone) != 1 || byZone[0].ID != generator.ID {
		t.Fatalf("expected only the generator by zone, got %+v", byZone)
	}

	byQueryName, err := store.ListEquipment(equipmentFilter{Query: "impeller"})
	if err != nil {
		t.Fatalf("ListEquipment (query name): %v", err)
	}
	if len(byQueryName) != 1 || byQueryName[0].ID != impeller.ID {
		t.Fatalf("expected the impeller matched by name, got %+v", byQueryName)
	}

	byQueryAlias, err := store.ListEquipment(equipmentFilter{Query: "genset"})
	if err != nil {
		t.Fatalf("ListEquipment (query alias): %v", err)
	}
	if len(byQueryAlias) != 1 || byQueryAlias[0].ID != generator.ID {
		t.Fatalf("expected the generator matched only through its alias, got %+v", byQueryAlias)
	}
}

// ── equipment documents ──────────────────────────────────────────────────

func TestDocumentStore_SetEquipmentDocumentsReplacesWholesale(t *testing.T) {
	store := newTestDocumentStore(t)

	item, err := store.CreateEquipment(equipmentItem{Name: "Generator", Category: "mechanical"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	docA := mustInsertDocument(t, store, "sha-inv-a", "a.pdf", nil)
	docB := mustInsertDocument(t, store, "sha-inv-b", "b.pdf", nil)

	if err := store.SetEquipmentDocuments(item.ID, []string{docA.ID}); err != nil {
		t.Fatalf("SetEquipmentDocuments (first): %v", err)
	}
	docs, err := store.EquipmentDocuments(item.ID)
	if err != nil {
		t.Fatalf("EquipmentDocuments: %v", err)
	}
	if len(docs) != 1 || docs[0].DocumentID != docA.ID || docs[0].Source != "operator" || docs[0].Filename != "a.pdf" {
		t.Fatalf("expected exactly docA joined with its filename, got %+v", docs)
	}

	if err := store.SetEquipmentDocuments(item.ID, []string{docB.ID}); err != nil {
		t.Fatalf("SetEquipmentDocuments (second): %v", err)
	}
	docs, err = store.EquipmentDocuments(item.ID)
	if err != nil {
		t.Fatalf("EquipmentDocuments: %v", err)
	}
	if len(docs) != 1 || docs[0].DocumentID != docB.ID {
		t.Fatalf("expected the set REPLACED (docB only), not appended, got %+v", docs)
	}
}

func TestDocumentStore_SetEquipmentDocumentsUnknownEquipmentIDReturnsNotFound(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := mustInsertDocument(t, store, "sha-inv-missing-eq", "a.pdf", nil)
	if err := store.SetEquipmentDocuments("does-not-exist", []string{doc.ID}); !errors.Is(err, errEquipmentNotFound) {
		t.Fatalf("expected errEquipmentNotFound, got %v", err)
	}
}

// TestDocumentStore_SetEquipmentDocumentsUnknownDocumentIDFailsForeignKey is
// a required Verification case: a document id that does not exist fails the
// equipment_documents.document_id foreign key (task item 3's "pre-check the
// RESTRICT case with COUNT" is about DELETE; this INSERT-time case is left
// to the real constraint on purpose - see SetEquipmentDocuments' own doc
// comment for why).
func TestDocumentStore_SetEquipmentDocumentsUnknownDocumentIDFailsForeignKey(t *testing.T) {
	store := newTestDocumentStore(t)

	item, err := store.CreateEquipment(equipmentItem{Name: "Generator", Category: "mechanical"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}

	if err := store.SetEquipmentDocuments(item.ID, []string{"does-not-exist"}); err == nil {
		t.Fatalf("expected the unknown document id to fail the foreign key")
	}

	docs, err := store.EquipmentDocuments(item.ID)
	if err != nil {
		t.Fatalf("EquipmentDocuments: %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("expected the whole replace to roll back on the FK failure, got %+v", docs)
	}
}

// TestDocumentStore_SetEquipmentDocumentsDedupesDuplicateIDs pins the fix
// for a caller that hands back the same document_id twice (["d1","d1"]):
// without deduping first, the second INSERT collides with
// equipment_documents' own (equipment_id, document_id) primary key and the
// whole replace fails with a raw SQLite constraint error instead of just
// linking the document once.
func TestDocumentStore_SetEquipmentDocumentsDedupesDuplicateIDs(t *testing.T) {
	store := newTestDocumentStore(t)

	item, err := store.CreateEquipment(equipmentItem{Name: "Generator", Category: "mechanical"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	doc := mustInsertDocument(t, store, "sha-inv-dup", "a.pdf", nil)

	if err := store.SetEquipmentDocuments(item.ID, []string{doc.ID, doc.ID}); err != nil {
		t.Fatalf("SetEquipmentDocuments with a duplicate id: %v", err)
	}

	docs, err := store.EquipmentDocuments(item.ID)
	if err != nil {
		t.Fatalf("EquipmentDocuments: %v", err)
	}
	if len(docs) != 1 || docs[0].DocumentID != doc.ID {
		t.Fatalf("expected the duplicate id collapsed to a single link, got %+v", docs)
	}
}

// TestDocumentStore_DeleteEquipmentCascadesLinks proves the foreign_keys
// pragma (already enabled by newDocumentStore's DSN, verified by
// TestNewDocumentStore_ForeignKeysPragmaEnabled in documents_store_test.go)
// actually makes equipment_documents' ON DELETE CASCADE fire - task item 1's
// "write a test that proves a cascade actually fires", not merely assumed
// from the schema text.
func TestDocumentStore_DeleteEquipmentCascadesLinks(t *testing.T) {
	store := newTestDocumentStore(t)

	item, err := store.CreateEquipment(equipmentItem{Name: "Generator", Category: "mechanical"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	doc := mustInsertDocument(t, store, "sha-cascade-eq", "a.pdf", nil)
	if err := store.SetEquipmentDocuments(item.ID, []string{doc.ID}); err != nil {
		t.Fatalf("SetEquipmentDocuments: %v", err)
	}

	if err := store.DeleteEquipment(item.ID); err != nil {
		t.Fatalf("DeleteEquipment: %v", err)
	}

	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM equipment_documents WHERE equipment_id = ?`, item.ID).Scan(&count); err != nil {
		t.Fatalf("count equipment_documents: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected ON DELETE CASCADE to remove the link row, found %d still there", count)
	}
}

// TestDocumentStore_DeletingDocumentRemovesEquipmentLinks is task item 2's
// required test: equipment_documents.document_id ON DELETE CASCADE gives
// "delete a document, its links vanish" behaviour for free - proved here,
// not just assumed from the schema.
func TestDocumentStore_DeletingDocumentRemovesEquipmentLinks(t *testing.T) {
	store := newTestDocumentStore(t)

	item, err := store.CreateEquipment(equipmentItem{Name: "Generator", Category: "mechanical"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	doc := mustInsertDocument(t, store, "sha-cascade-doc", "a.pdf", nil)
	if err := store.SetEquipmentDocuments(item.ID, []string{doc.ID}); err != nil {
		t.Fatalf("SetEquipmentDocuments: %v", err)
	}

	if _, err := store.Delete(doc.ID); err != nil {
		t.Fatalf("Delete(document): %v", err)
	}

	docs, err := store.EquipmentDocuments(item.ID)
	if err != nil {
		t.Fatalf("EquipmentDocuments: %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("expected the link to vanish once its document was deleted, got %+v", docs)
	}
}

func TestDocumentStore_EquipmentDocumentsUnknownEquipmentIDReturnsNotFound(t *testing.T) {
	store := newTestDocumentStore(t)
	if _, err := store.EquipmentDocuments("does-not-exist"); !errors.Is(err, errEquipmentNotFound) {
		t.Fatalf("expected errEquipmentNotFound, got %v", err)
	}
}

// An alias exists for one purpose: matching what the crew actually says out
// loud ("have we got a spare for the genset?"). That matching is
// case-insensitive, so "genset" and "Genset" are not two aliases, they are
// one alias spelled two ways, and keeping both would show the operator two
// identical-looking chips and give a future resolver two rows to score
// against for the same word. Dedupe therefore folds case, keeping the first
// spelling the operator typed - the same rule inventory_zones and
// inventory_bins already enforce in SQL with their lower(name)/lower(code)
// unique indexes.
func TestDocumentStore_CreateEquipmentDedupesAliasesCaseInsensitively(t *testing.T) {
	store := newTestDocumentStore(t)

	item, err := store.CreateEquipment(equipmentItem{
		Name:     "Generator",
		Category: "mechanical",
		Aliases:  []string{"genset", " Genset ", "GENSET", "donk"},
	})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	if len(item.Aliases) != 2 || item.Aliases[0] != "genset" || item.Aliases[1] != "donk" {
		t.Fatalf("expected case-insensitive dedupe keeping the first spelling, got %#v", item.Aliases)
	}
}

// ── equipment photos (ADR 0127) ─────────────────────────────────────────
// Written before the store methods themselves (AGENTS.md's test-first
// policy), the same convention every other section of this file follows.

// mustInsertPhotoDocument inserts a document already tagged 'photo' -
// mustInsertDocument's own shape, extended with the one tag every photo
// fixture below needs.
func mustInsertPhotoDocument(t *testing.T, store *documentStore, sha, filename string) document {
	t.Helper()
	doc, err := store.Insert(document{SHA256: sha, Filename: filename, MIME: "image/jpeg", OperatorTags: []string{"photo"}})
	if err != nil {
		t.Fatalf("Insert(%q): %v", filename, err)
	}
	return doc
}

func TestDocumentStore_AddEquipmentPhotoAssignsIncrementingSortIndexCoverFirst(t *testing.T) {
	store := newTestDocumentStore(t)
	item, err := store.CreateEquipment(equipmentItem{Name: "Adhesives bin", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	photoA := mustInsertPhotoDocument(t, store, "sha-photo-a", "a.jpg")
	photoB := mustInsertPhotoDocument(t, store, "sha-photo-b", "b.jpg")

	if err := store.AddEquipmentPhoto(item.ID, photoA.ID); err != nil {
		t.Fatalf("AddEquipmentPhoto(a): %v", err)
	}
	if err := store.AddEquipmentPhoto(item.ID, photoB.ID); err != nil {
		t.Fatalf("AddEquipmentPhoto(b): %v", err)
	}

	got, err := store.GetEquipment(item.ID)
	if err != nil {
		t.Fatalf("GetEquipment: %v", err)
	}
	if len(got.PhotoIDs) != 2 || got.PhotoIDs[0] != photoA.ID || got.PhotoIDs[1] != photoB.ID {
		t.Fatalf("expected photo_ids in upload order [a,b], got %#v", got.PhotoIDs)
	}
}

func TestDocumentStore_AddEquipmentPhotoUnknownEquipmentReturnsNotFound(t *testing.T) {
	store := newTestDocumentStore(t)
	photo := mustInsertPhotoDocument(t, store, "sha-photo-orphan", "a.jpg")
	if err := store.AddEquipmentPhoto("does-not-exist", photo.ID); !errors.Is(err, errEquipmentNotFound) {
		t.Fatalf("expected errEquipmentNotFound, got %v", err)
	}
}

func TestDocumentStore_AddEquipmentPhotoTwiceIsANoOpNotAnError(t *testing.T) {
	store := newTestDocumentStore(t)
	item, err := store.CreateEquipment(equipmentItem{Name: "Adhesives bin", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	photo := mustInsertPhotoDocument(t, store, "sha-photo-twice", "a.jpg")

	if err := store.AddEquipmentPhoto(item.ID, photo.ID); err != nil {
		t.Fatalf("AddEquipmentPhoto (first): %v", err)
	}
	if err := store.AddEquipmentPhoto(item.ID, photo.ID); err != nil {
		t.Fatalf("AddEquipmentPhoto (second, same pair): %v", err)
	}

	got, err := store.GetEquipment(item.ID)
	if err != nil {
		t.Fatalf("GetEquipment: %v", err)
	}
	if len(got.PhotoIDs) != 1 {
		t.Fatalf("expected re-adding the same link to stay a single entry, got %#v", got.PhotoIDs)
	}
}

// TestDocumentStore_SetEquipmentPhotoOrderMakesCoverByReordering pins ADR
// 0124's "Make cover is this call with the chosen id moved to the front".
func TestDocumentStore_SetEquipmentPhotoOrderMakesCoverByReordering(t *testing.T) {
	store := newTestDocumentStore(t)
	item, err := store.CreateEquipment(equipmentItem{Name: "Adhesives bin", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	photoA := mustInsertPhotoDocument(t, store, "sha-order-a", "a.jpg")
	photoB := mustInsertPhotoDocument(t, store, "sha-order-b", "b.jpg")
	if err := store.AddEquipmentPhoto(item.ID, photoA.ID); err != nil {
		t.Fatalf("AddEquipmentPhoto(a): %v", err)
	}
	if err := store.AddEquipmentPhoto(item.ID, photoB.ID); err != nil {
		t.Fatalf("AddEquipmentPhoto(b): %v", err)
	}

	if err := store.SetEquipmentPhotoOrder(item.ID, []string{photoB.ID, photoA.ID}); err != nil {
		t.Fatalf("SetEquipmentPhotoOrder: %v", err)
	}

	got, err := store.GetEquipment(item.ID)
	if err != nil {
		t.Fatalf("GetEquipment: %v", err)
	}
	if len(got.PhotoIDs) != 2 || got.PhotoIDs[0] != photoB.ID || got.PhotoIDs[1] != photoA.ID {
		t.Fatalf("expected the new cover (b) first, got %#v", got.PhotoIDs)
	}
}

func TestDocumentStore_SetEquipmentPhotoOrderRejectsMismatchedSet(t *testing.T) {
	store := newTestDocumentStore(t)
	item, err := store.CreateEquipment(equipmentItem{Name: "Adhesives bin", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	photoA := mustInsertPhotoDocument(t, store, "sha-mismatch-a", "a.jpg")
	photoB := mustInsertPhotoDocument(t, store, "sha-mismatch-b", "b.jpg")
	if err := store.AddEquipmentPhoto(item.ID, photoA.ID); err != nil {
		t.Fatalf("AddEquipmentPhoto(a): %v", err)
	}

	// Missing id (empty order against one existing photo).
	if err := store.SetEquipmentPhotoOrder(item.ID, []string{}); !errors.Is(err, errEquipmentPhotoSetMismatch) {
		t.Fatalf("expected errEquipmentPhotoSetMismatch for a missing id, got %v", err)
	}
	// Extra id (b was never added to this item).
	if err := store.SetEquipmentPhotoOrder(item.ID, []string{photoA.ID, photoB.ID}); !errors.Is(err, errEquipmentPhotoSetMismatch) {
		t.Fatalf("expected errEquipmentPhotoSetMismatch for an extra id, got %v", err)
	}
}

// TestDocumentStore_SetEquipmentPhotoOrderRejectsUnknownEquipmentID pins an
// ADR 0127 review finding: an unknown equipment id must be rejected
// explicitly (errEquipmentNotFound), not fall through to the photo-set
// comparison below it (which would see an empty current set and, for an
// empty `order`, wrongly report success instead of "no such item").
func TestDocumentStore_SetEquipmentPhotoOrderRejectsUnknownEquipmentID(t *testing.T) {
	store := newTestDocumentStore(t)
	if err := store.SetEquipmentPhotoOrder("does-not-exist", []string{}); !errors.Is(err, errEquipmentNotFound) {
		t.Fatalf("expected errEquipmentNotFound, got %v", err)
	}
}

func TestDocumentStore_RemoveEquipmentPhotoDeletesLinkAndDocument(t *testing.T) {
	store := newTestDocumentStore(t)
	item, err := store.CreateEquipment(equipmentItem{Name: "Adhesives bin", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	photo := mustInsertPhotoDocument(t, store, "sha-remove", "a.jpg")
	if err := store.AddEquipmentPhoto(item.ID, photo.ID); err != nil {
		t.Fatalf("AddEquipmentPhoto: %v", err)
	}

	sha, deleted, err := store.RemoveEquipmentPhoto(item.ID, photo.ID)
	if err != nil {
		t.Fatalf("RemoveEquipmentPhoto: %v", err)
	}
	if sha != "sha-remove" {
		t.Fatalf("expected the photo's own sha256 back, got %q", sha)
	}
	if !deleted {
		t.Fatalf("expected the document to be reported deleted when no other item references it")
	}

	if _, err := store.Get(photo.ID); !errors.Is(err, errDocumentNotFound) {
		t.Fatalf("expected the document itself to be deleted, got %v", err)
	}
	got, err := store.GetEquipment(item.ID)
	if err != nil {
		t.Fatalf("GetEquipment: %v", err)
	}
	if len(got.PhotoIDs) != 0 {
		t.Fatalf("expected no photos left, got %#v", got.PhotoIDs)
	}
}

func TestDocumentStore_RemoveEquipmentPhotoNotOwnedByItemReturnsNotFound(t *testing.T) {
	store := newTestDocumentStore(t)
	item, err := store.CreateEquipment(equipmentItem{Name: "Adhesives bin", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	// A plain (non-photo) linked document is not a photo this item can remove
	// through the photo route, even though it IS linked.
	doc := mustInsertDocument(t, store, "sha-not-photo", "manual.pdf", nil)
	if err := store.SetEquipmentDocuments(item.ID, []string{doc.ID}); err != nil {
		t.Fatalf("SetEquipmentDocuments: %v", err)
	}
	if _, _, err := store.RemoveEquipmentPhoto(item.ID, doc.ID); !errors.Is(err, errEquipmentPhotoNotFound) {
		t.Fatalf("expected errEquipmentPhotoNotFound, got %v", err)
	}
}

// TestDocumentStore_RemoveEquipmentPhotoSharedWithAnotherItemKeepsIt pins the
// most serious of the ADR 0127 review findings: uploads are deduplicated by
// sha256 (documentStore.Insert), so byte-identical photos added to two
// different items share ONE documents row, linked twice. Removing the photo
// from item A must drop only A's own equipment_documents link - the document
// row (and, at the handler layer, its file) must survive because item B's
// link still references it.
func TestDocumentStore_RemoveEquipmentPhotoSharedWithAnotherItemKeepsIt(t *testing.T) {
	store := newTestDocumentStore(t)
	itemA, err := store.CreateEquipment(equipmentItem{Name: "Bin A item", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment(A): %v", err)
	}
	itemB, err := store.CreateEquipment(equipmentItem{Name: "Bin B item", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment(B): %v", err)
	}
	photo := mustInsertPhotoDocument(t, store, "sha-shared", "a.jpg")
	if err := store.AddEquipmentPhoto(itemA.ID, photo.ID); err != nil {
		t.Fatalf("AddEquipmentPhoto(A): %v", err)
	}
	if err := store.AddEquipmentPhoto(itemB.ID, photo.ID); err != nil {
		t.Fatalf("AddEquipmentPhoto(B): %v", err)
	}

	sha, deleted, err := store.RemoveEquipmentPhoto(itemA.ID, photo.ID)
	if err != nil {
		t.Fatalf("RemoveEquipmentPhoto: %v", err)
	}
	if sha != "sha-shared" {
		t.Fatalf("expected the photo's own sha256 back, got %q", sha)
	}
	if deleted {
		t.Fatalf("expected the document to be KEPT while item B still links it")
	}

	if _, err := store.Get(photo.ID); err != nil {
		t.Fatalf("expected the shared document to survive, got %v", err)
	}
	gotA, err := store.GetEquipment(itemA.ID)
	if err != nil {
		t.Fatalf("GetEquipment(A): %v", err)
	}
	if len(gotA.PhotoIDs) != 0 {
		t.Fatalf("expected item A's own link removed, got %#v", gotA.PhotoIDs)
	}
	gotB, err := store.GetEquipment(itemB.ID)
	if err != nil {
		t.Fatalf("GetEquipment(B): %v", err)
	}
	if len(gotB.PhotoIDs) != 1 || gotB.PhotoIDs[0] != photo.ID {
		t.Fatalf("expected item B's own link untouched, got %#v", gotB.PhotoIDs)
	}
}

// TestDocumentStore_SetEquipmentDocumentsPreservesPhotoLinksAndOrder pins
// ADR 0127's central compatibility rule: the pre-existing whole-set-replace
// PUT .../documents must not be able to wipe or reshuffle the photo strip a
// completely separate part of the UI manages.
func TestDocumentStore_SetEquipmentDocumentsPreservesPhotoLinksAndOrder(t *testing.T) {
	store := newTestDocumentStore(t)
	item, err := store.CreateEquipment(equipmentItem{Name: "Adhesives bin", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	photoA := mustInsertPhotoDocument(t, store, "sha-preserve-a", "a.jpg")
	photoB := mustInsertPhotoDocument(t, store, "sha-preserve-b", "b.jpg")
	if err := store.AddEquipmentPhoto(item.ID, photoA.ID); err != nil {
		t.Fatalf("AddEquipmentPhoto(a): %v", err)
	}
	if err := store.AddEquipmentPhoto(item.ID, photoB.ID); err != nil {
		t.Fatalf("AddEquipmentPhoto(b): %v", err)
	}

	manual := mustInsertDocument(t, store, "sha-preserve-manual", "manual.pdf", nil)
	if err := store.SetEquipmentDocuments(item.ID, []string{manual.ID}); err != nil {
		t.Fatalf("SetEquipmentDocuments: %v", err)
	}

	got, err := store.GetEquipment(item.ID)
	if err != nil {
		t.Fatalf("GetEquipment: %v", err)
	}
	if len(got.PhotoIDs) != 2 || got.PhotoIDs[0] != photoA.ID || got.PhotoIDs[1] != photoB.ID {
		t.Fatalf("expected both photo links and their order untouched, got %#v", got.PhotoIDs)
	}

	docs, err := store.EquipmentDocuments(item.ID)
	if err != nil {
		t.Fatalf("EquipmentDocuments: %v", err)
	}
	foundManual := false
	for _, d := range docs {
		if d.DocumentID == manual.ID {
			foundManual = true
		}
	}
	if !foundManual {
		t.Fatalf("expected the manual to still be linked as an ordinary document, got %+v", docs)
	}
	if len(docs) != 3 {
		t.Fatalf("expected 2 photo links + 1 manual link = 3, got %d: %+v", len(docs), docs)
	}
}

// TestDocumentStore_RemovingPhotoTagLeavesLinkButDropsFromPhotoIDs pins ADR
// 0123 §3 applied to photos: "an item's photos are its linked documents
// tagged photo" - untag it, and it leaves the strip but the link (and the
// document) survive as an ordinary linked document. Not a special case,
// just the same tags-say-what-it-is rule already in force.
func TestDocumentStore_RemovingPhotoTagLeavesLinkButDropsFromPhotoIDs(t *testing.T) {
	store := newTestDocumentStore(t)
	item, err := store.CreateEquipment(equipmentItem{Name: "Adhesives bin", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	photo := mustInsertPhotoDocument(t, store, "sha-untag", "a.jpg")
	if err := store.AddEquipmentPhoto(item.ID, photo.ID); err != nil {
		t.Fatalf("AddEquipmentPhoto: %v", err)
	}

	// Remove the 'photo' tag the way the Documents feature's own tag editor
	// would (UpdateMeta replaces the WHOLE operator tag set) - leaving it
	// tagged with something else entirely, never untagged outright.
	if err := store.UpdateMeta(photo.ID, nil, nil, []string{"consumable"}); err != nil {
		t.Fatalf("UpdateMeta: %v", err)
	}

	got, err := store.GetEquipment(item.ID)
	if err != nil {
		t.Fatalf("GetEquipment: %v", err)
	}
	if len(got.PhotoIDs) != 0 {
		t.Fatalf("expected the untagged document to leave the photo strip, got %#v", got.PhotoIDs)
	}

	docs, err := store.EquipmentDocuments(item.ID)
	if err != nil {
		t.Fatalf("EquipmentDocuments: %v", err)
	}
	if len(docs) != 1 || docs[0].DocumentID != photo.ID {
		t.Fatalf("expected the link itself to survive, got %+v", docs)
	}
}

func TestDocumentStore_ListEquipmentFiltersByBinID(t *testing.T) {
	store := newTestDocumentStore(t)
	zone, err := store.CreateZone("Lazarette")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	binA, err := store.CreateBin(zone.ID, "LAZ-01", "")
	if err != nil {
		t.Fatalf("CreateBin(a): %v", err)
	}
	binB, err := store.CreateBin(zone.ID, "LAZ-02", "")
	if err != nil {
		t.Fatalf("CreateBin(b): %v", err)
	}
	if _, err := store.CreateEquipment(equipmentItem{Name: "Tape", Category: "general", BinID: &binA.ID}); err != nil {
		t.Fatalf("CreateEquipment(a): %v", err)
	}
	if _, err := store.CreateEquipment(equipmentItem{Name: "Caulk", Category: "general", BinID: &binB.ID}); err != nil {
		t.Fatalf("CreateEquipment(b): %v", err)
	}

	items, err := store.ListEquipment(equipmentFilter{BinID: binA.ID})
	if err != nil {
		t.Fatalf("ListEquipment: %v", err)
	}
	if len(items) != 1 || items[0].Name != "Tape" {
		t.Fatalf("expected only the item filed in bin A, got %+v", items)
	}
}

func TestDocumentStore_ListEquipmentCarriesPhotoIDsForEveryItem(t *testing.T) {
	store := newTestDocumentStore(t)
	item, err := store.CreateEquipment(equipmentItem{Name: "Zip ties", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	photo := mustInsertPhotoDocument(t, store, "sha-list-photo", "a.jpg")
	if err := store.AddEquipmentPhoto(item.ID, photo.ID); err != nil {
		t.Fatalf("AddEquipmentPhoto: %v", err)
	}

	items, err := store.ListEquipment(equipmentFilter{})
	if err != nil {
		t.Fatalf("ListEquipment: %v", err)
	}
	if len(items) != 1 || len(items[0].PhotoIDs) != 1 || items[0].PhotoIDs[0] != photo.ID {
		t.Fatalf("expected photo_ids carried through ListEquipment, got %+v", items)
	}
}
