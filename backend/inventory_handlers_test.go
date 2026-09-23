package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// This file exercises inventory_handlers.go (plan "Inventory: the equipment
// registry and locations", Phase 1), the same "call the handler directly,
// not through a router" idiom every other *_handlers_test.go file in this
// package uses (manuals_handlers_test.go, notes_handlers_test.go).

// ── GET /api/inventory/zones ─────────────────────────────────────────────

func TestListZonesHandler_EmptyReturnsEmptyArrayNot404(t *testing.T) {
	withTestDocumentStore(t)

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/inventory/zones", "", "")
	if err := listZonesHandler(c); err != nil {
		t.Fatalf("listZonesHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Zones []inventoryZone `json:"zones"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Zones == nil {
		t.Fatalf("expected an empty array, got a null zones field")
	}
}

// ── POST /api/inventory/zones ────────────────────────────────────────────

func TestCreateZoneHandler_CreatesAndReturns201(t *testing.T) {
	withTestDocumentStore(t)

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/zones", `{"name":"Lazarette"}`, "")
	if err := createZoneHandler(c); err != nil {
		t.Fatalf("createZoneHandler returned error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var wrapped struct {
		Zone inventoryZone `json:"zone"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wrapped); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	zone := wrapped.Zone
	if zone.Name != "Lazarette" {
		t.Fatalf("expected the given name, got %q", zone.Name)
	}
}

func TestCreateZoneHandler_DuplicateNameReturns409(t *testing.T) {
	withTestDocumentStore(t)

	if _, err := globalDocumentStore.CreateZone("Salon"); err != nil {
		t.Fatalf("CreateZone: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/zones", `{"name":"salon"}`, "")
	if err := createZoneHandler(c); err != nil {
		t.Fatalf("createZoneHandler returned error: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ── PUT /api/inventory/zones/:id ─────────────────────────────────────────

func TestUpdateZoneHandler_Renames(t *testing.T) {
	withTestDocumentStore(t)

	zone, err := globalDocumentStore.CreateZone("Salon")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPut, "/api/inventory/zones/"+zone.ID, `{"name":"Saloon"}`, zone.ID)
	if err := updateZoneHandler(c); err != nil {
		t.Fatalf("updateZoneHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var wrapped struct {
		Zone inventoryZone `json:"zone"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wrapped); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	updated := wrapped.Zone
	if updated.Name != "Saloon" {
		t.Fatalf("expected the renamed value, got %q", updated.Name)
	}
}

// ── DELETE /api/inventory/zones/:id ──────────────────────────────────────

func TestDeleteZoneHandler_RemovesAndReturns204(t *testing.T) {
	withTestDocumentStore(t)

	zone, err := globalDocumentStore.CreateZone("Salon")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodDelete, "/api/inventory/zones/"+zone.ID, "", zone.ID)
	if err := deleteZoneHandler(c); err != nil {
		t.Fatalf("deleteZoneHandler returned error: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestDeleteZoneHandler_InUseReturns409WithCount is a required Verification
// case: plan's own API shape is "409 with {error, in_use}".
func TestDeleteZoneHandler_InUseReturns409WithCount(t *testing.T) {
	withTestDocumentStore(t)

	zone, err := globalDocumentStore.CreateZone("Lazarette")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	if _, err := globalDocumentStore.CreateBin(zone.ID, "LAZ-01", ""); err != nil {
		t.Fatalf("CreateBin: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodDelete, "/api/inventory/zones/"+zone.ID, "", zone.ID)
	if err := deleteZoneHandler(c); err != nil {
		t.Fatalf("deleteZoneHandler returned error: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Error string `json:"error"`
		InUse int    `json:"in_use"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.InUse != 1 {
		t.Fatalf("expected in_use=1, got %+v", resp)
	}
	if resp.Error == "" {
		t.Fatalf("expected a non-empty error message naming the conflict")
	}
}

// ── POST /api/inventory/bins ─────────────────────────────────────────────

func TestCreateBinHandler_CreatesAndReturns201(t *testing.T) {
	withTestDocumentStore(t)

	zone, err := globalDocumentStore.CreateZone("Salon")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}

	body := `{"zone_id":"` + zone.ID + `","code":"SAL-04","name":"Life jackets"}`
	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/bins", body, "")
	if err := createBinHandler(c); err != nil {
		t.Fatalf("createBinHandler returned error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var wrapped struct {
		Bin inventoryBin `json:"bin"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wrapped); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	bin := wrapped.Bin
	if bin.Code != "SAL-04" || bin.ZoneID != zone.ID {
		t.Fatalf("expected the given fields, got %+v", bin)
	}
}

// ── PUT /api/inventory/bins/:id ──────────────────────────────────────────

func TestUpdateBinHandler_RenamesAndRecodes(t *testing.T) {
	withTestDocumentStore(t)

	zone, err := globalDocumentStore.CreateZone("Salon")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	bin, err := globalDocumentStore.CreateBin(zone.ID, "SAL-04", "Life jackets")
	if err != nil {
		t.Fatalf("CreateBin: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPut, "/api/inventory/bins/"+bin.ID, `{"code":"SAL-05","name":"PFDs"}`, bin.ID)
	if err := updateBinHandler(c); err != nil {
		t.Fatalf("updateBinHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var wrapped struct {
		Bin inventoryBin `json:"bin"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wrapped); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	updated := wrapped.Bin
	if updated.Code != "SAL-05" || updated.Name != "PFDs" {
		t.Fatalf("expected the updated fields, got %+v", updated)
	}
}

// ── DELETE /api/inventory/bins/:id ───────────────────────────────────────

func TestDeleteBinHandler_InUseReturns409WithCount(t *testing.T) {
	withTestDocumentStore(t)

	zone, err := globalDocumentStore.CreateZone("Lazarette")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	bin, err := globalDocumentStore.CreateBin(zone.ID, "LAZ-01", "")
	if err != nil {
		t.Fatalf("CreateBin: %v", err)
	}
	if _, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Spare impeller", Category: "general", BinID: &bin.ID}); err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodDelete, "/api/inventory/bins/"+bin.ID, "", bin.ID)
	if err := deleteBinHandler(c); err != nil {
		t.Fatalf("deleteBinHandler returned error: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		InUse int `json:"in_use"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.InUse != 1 {
		t.Fatalf("expected in_use=1, got %+v", resp)
	}
}

// ── POST /api/inventory/equipment ────────────────────────────────────────

func TestCreateEquipmentHandler_CreatesAndReturns201(t *testing.T) {
	withTestDocumentStore(t)

	body := `{"name":"Generator","category":"mechanical","system":"electrical","manufacturer":"Onan"}`
	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/equipment", body, "")
	if err := createEquipmentHandler(c); err != nil {
		t.Fatalf("createEquipmentHandler returned error: %v", err)
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
	item := wrapped.Item
	if item.Name != "Generator" || item.Manufacturer != "Onan" {
		t.Fatalf("expected the given fields, got %+v", item)
	}
}

// TestCreateEquipmentHandler_MissingNameReturns400WithFieldShape pins the
// {field, message} validation error shape (task instructions: "see
// validateSettingsChange in backend/signalk.go for the shape").
func TestCreateEquipmentHandler_MissingNameReturns400WithFieldShape(t *testing.T) {
	withTestDocumentStore(t)

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/equipment", `{"category":"mechanical"}`, "")
	if err := createEquipmentHandler(c); err != nil {
		t.Fatalf("createEquipmentHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Field   string `json:"field"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Field != "name" || resp.Message == "" {
		t.Fatalf("expected field=name with a message, got %+v", resp)
	}
}

func TestCreateEquipmentHandler_InvalidCategoryReturns400WithFieldShape(t *testing.T) {
	withTestDocumentStore(t)

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/equipment", `{"name":"X","category":"bogus"}`, "")
	if err := createEquipmentHandler(c); err != nil {
		t.Fatalf("createEquipmentHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Field string `json:"field"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Field != "category" {
		t.Fatalf("expected field=category, got %+v", resp)
	}
}

func TestCreateEquipmentHandler_InvalidSystemReturns400WithFieldShape(t *testing.T) {
	withTestDocumentStore(t)

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/equipment", `{"name":"X","category":"general","system":"bogus"}`, "")
	if err := createEquipmentHandler(c); err != nil {
		t.Fatalf("createEquipmentHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Field string `json:"field"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Field != "system" {
		t.Fatalf("expected field=system, got %+v", resp)
	}
}

func TestCreateEquipmentHandler_InvalidStatusReturns400WithFieldShape(t *testing.T) {
	withTestDocumentStore(t)

	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/equipment", `{"name":"X","category":"general","status":"bogus"}`, "")
	if err := createEquipmentHandler(c); err != nil {
		t.Fatalf("createEquipmentHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Field string `json:"field"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Field != "status" {
		t.Fatalf("expected field=status, got %+v", resp)
	}
}

// TestCreateEquipmentHandler_AliasesTrimmedAndDeduped pins plan's own
// "aliases trimmed and deduped" validateEquipmentInput responsibility at
// the HTTP layer (inventory_store_test.go already pins the same rule at
// the store layer via normalizeAliases directly).
func TestCreateEquipmentHandler_AliasesTrimmedAndDeduped(t *testing.T) {
	withTestDocumentStore(t)

	body := `{"name":"Generator","category":"mechanical","aliases":[" genset ","genset","Genset"]}`
	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/equipment", body, "")
	if err := createEquipmentHandler(c); err != nil {
		t.Fatalf("createEquipmentHandler returned error: %v", err)
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
	item := wrapped.Item
	if len(item.Aliases) != 1 || item.Aliases[0] != "genset" {
		t.Fatalf("expected aliases trimmed and case-insensitively deduped, got %+v", item.Aliases)
	}
}

// TestCreateEquipmentHandler_UnknownProfileIDReturns400 pins plan's
// "profile_id must be a loaded engineProfiles() id when non-empty".
func TestCreateEquipmentHandler_UnknownProfileIDReturns400(t *testing.T) {
	withTestDocumentStore(t)

	body := `{"name":"Generator","category":"mechanical","profile_id":"does-not-exist"}`
	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/equipment", body, "")
	if err := createEquipmentHandler(c); err != nil {
		t.Fatalf("createEquipmentHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Field string `json:"field"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Field != "profile_id" {
		t.Fatalf("expected field=profile_id, got %+v", resp)
	}
}

// TestCreateEquipmentHandler_BadInstallDateReturns400 pins plan's
// "install_date blank or YYYY-MM-DD".
func TestCreateEquipmentHandler_BadInstallDateReturns400(t *testing.T) {
	withTestDocumentStore(t)

	body := `{"name":"Generator","category":"mechanical","install_date":"09/23/2026"}`
	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/equipment", body, "")
	if err := createEquipmentHandler(c); err != nil {
		t.Fatalf("createEquipmentHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Field string `json:"field"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Field != "install_date" {
		t.Fatalf("expected field=install_date, got %+v", resp)
	}
}

// TestCreateEquipmentHandler_HourMeterPathWithSpaceReturns400 pins plan's
// "hour_meter_path trimmed, no spaces".
func TestCreateEquipmentHandler_HourMeterPathWithSpaceReturns400(t *testing.T) {
	withTestDocumentStore(t)

	body := `{"name":"Generator","category":"mechanical","hour_meter_path":"electrical generator runtime"}`
	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/equipment", body, "")
	if err := createEquipmentHandler(c); err != nil {
		t.Fatalf("createEquipmentHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Field string `json:"field"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Field != "hour_meter_path" {
		t.Fatalf("expected field=hour_meter_path, got %+v", resp)
	}
}

func TestCreateEquipmentHandler_LocationMismatchReturns400(t *testing.T) {
	withTestDocumentStore(t)

	zoneA, err := globalDocumentStore.CreateZone("Salon")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	zoneB, err := globalDocumentStore.CreateZone("Lazarette")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	bin, err := globalDocumentStore.CreateBin(zoneB.ID, "LAZ-01", "")
	if err != nil {
		t.Fatalf("CreateBin: %v", err)
	}

	body := `{"name":"X","category":"general","zone_id":"` + zoneA.ID + `","bin_id":"` + bin.ID + `"}`
	c, rec := newDocumentEchoContext(http.MethodPost, "/api/inventory/equipment", body, "")
	if err := createEquipmentHandler(c); err != nil {
		t.Fatalf("createEquipmentHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ── GET /api/inventory/equipment ─────────────────────────────────────────

func TestListEquipmentHandler_FiltersByQueryParams(t *testing.T) {
	withTestDocumentStore(t)

	if _, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Generator", Category: "mechanical", System: "electrical"}); err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	if _, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Spare impeller", Category: "general", System: "propulsion"}); err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/inventory/equipment?category=general", "", "")
	if err := listEquipmentHandler(c); err != nil {
		t.Fatalf("listEquipmentHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []equipmentItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].Name != "Spare impeller" {
		t.Fatalf("expected only the impeller, got %+v", resp.Items)
	}
}

// ── GET /api/inventory/equipment/:id ─────────────────────────────────────

// TestGetEquipmentHandler_ReturnsItemAndJoinedDocuments is a required
// Verification case: "GET joins titles."
func TestGetEquipmentHandler_ReturnsItemAndJoinedDocuments(t *testing.T) {
	withTestDocumentStore(t)

	item, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Generator", Category: "mechanical"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	doc, err := globalDocumentStore.Insert(document{SHA256: "sha-get-eq", Filename: "generator.pdf", Title: "Generator quirk note", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := globalDocumentStore.SetEquipmentDocuments(item.ID, []string{doc.ID}); err != nil {
		t.Fatalf("SetEquipmentDocuments: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/inventory/equipment/"+item.ID, "", item.ID)
	if err := getEquipmentHandler(c); err != nil {
		t.Fatalf("getEquipmentHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Item      equipmentItem       `json:"item"`
		Documents []equipmentDocument `json:"documents"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Item.ID != item.ID {
		t.Fatalf("expected the item, got %+v", resp.Item)
	}
	if len(resp.Documents) != 1 || resp.Documents[0].Title != "Generator quirk note" {
		t.Fatalf("expected the joined document with its title, got %+v", resp.Documents)
	}
}

func TestGetEquipmentHandler_UnknownIDReturns404(t *testing.T) {
	withTestDocumentStore(t)

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/inventory/equipment/does-not-exist", "", "does-not-exist")
	if err := getEquipmentHandler(c); err != nil {
		t.Fatalf("getEquipmentHandler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ── PUT /api/inventory/equipment/:id ─────────────────────────────────────

func TestUpdateEquipmentHandler_ReplacesFields(t *testing.T) {
	withTestDocumentStore(t)

	item, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Generator", Category: "mechanical"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}

	body := `{"name":"Generator (Onan)","category":"mechanical","manufacturer":"Onan","status":"stored"}`
	c, rec := newDocumentEchoContext(http.MethodPut, "/api/inventory/equipment/"+item.ID, body, item.ID)
	if err := updateEquipmentHandler(c); err != nil {
		t.Fatalf("updateEquipmentHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var wrapped struct {
		Item equipmentItem `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wrapped); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	updated := wrapped.Item
	if updated.Name != "Generator (Onan)" || updated.Manufacturer != "Onan" || updated.Status != "stored" {
		t.Fatalf("expected the replaced fields, got %+v", updated)
	}
}

// ── DELETE /api/inventory/equipment/:id ──────────────────────────────────

func TestDeleteEquipmentHandler_RemovesAndReturns204(t *testing.T) {
	withTestDocumentStore(t)

	item, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Spare impeller", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodDelete, "/api/inventory/equipment/"+item.ID, "", item.ID)
	if err := deleteEquipmentHandler(c); err != nil {
		t.Fatalf("deleteEquipmentHandler returned error: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ── PUT /api/inventory/equipment/:id/documents ───────────────────────────

func TestSetEquipmentDocumentsHandler_ReplacesTheSet(t *testing.T) {
	withTestDocumentStore(t)

	item, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Generator", Category: "mechanical"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	docA, err := globalDocumentStore.Insert(document{SHA256: "sha-set-eq-a", Filename: "a.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	docB, err := globalDocumentStore.Insert(document{SHA256: "sha-set-eq-b", Filename: "b.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPut, "/api/inventory/equipment/"+item.ID+"/documents", `{"document_ids":["`+docA.ID+`"]}`, item.ID)
	if err := setEquipmentDocumentsHandler(c); err != nil {
		t.Fatalf("setEquipmentDocumentsHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	c, rec = newDocumentEchoContext(http.MethodPut, "/api/inventory/equipment/"+item.ID+"/documents", `{"document_ids":["`+docB.ID+`"]}`, item.ID)
	if err := setEquipmentDocumentsHandler(c); err != nil {
		t.Fatalf("setEquipmentDocumentsHandler returned error: %v", err)
	}
	var resp struct {
		Documents []equipmentDocument `json:"documents"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Documents) != 1 || resp.Documents[0].DocumentID != docB.ID {
		t.Fatalf("expected the set REPLACED (docB only), got %+v", resp.Documents)
	}
}

// TestSetEquipmentDocumentsHandler_UnknownDocumentIDReturns404WithName is a
// required Verification case: plan's own API shape is "404 naming an
// unknown document id".
func TestSetEquipmentDocumentsHandler_UnknownDocumentIDReturns404WithName(t *testing.T) {
	withTestDocumentStore(t)

	item, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Generator", Category: "mechanical"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPut, "/api/inventory/equipment/"+item.ID+"/documents", `{"document_ids":["does-not-exist"]}`, item.ID)
	if err := setEquipmentDocumentsHandler(c); err != nil {
		t.Fatalf("setEquipmentDocumentsHandler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
	if !jsonBodyContains(t, rec.Body.Bytes(), "does-not-exist") {
		t.Fatalf("expected the 404 body to name the unknown id, got %s", rec.Body.String())
	}

	docs, err := globalDocumentStore.EquipmentDocuments(item.ID)
	if err != nil {
		t.Fatalf("EquipmentDocuments: %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("expected nothing linked after the rejected call, got %+v", docs)
	}
}

// jsonBodyContains reports whether raw's top-level "error" string field
// contains want - a small helper so the 404-names-the-id assertion above
// doesn't need to know the exact wording of the message, only that the
// offending id is somewhere in it.
func jsonBodyContains(t *testing.T, raw []byte, want string) bool {
	t.Helper()
	var resp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return strings.Contains(resp.Error, want)
}

// ADR 0115 §7, learned the hard way on documentJSON: every field of a record
// the browser binds to is serialised unconditionally, never with omitempty.
// A TypeScript interface declaring `bin_code: string` is a lie the moment the
// server omits the key for an item with no bin, and the lie surfaces as
// `undefined` in a component that type-checked clean. An item with nothing
// filled in is the exact case the frontend renders most often - a record just
// created from the New item button - so it is the case that must carry every
// key.
func TestListEquipmentHandler_EveryFieldIsUnconditional(t *testing.T) {
	withTestDocumentStore(t)

	if _, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Bare", Category: "general"}); err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/inventory/equipment", "", "")
	if err := listEquipmentHandler(c); err != nil {
		t.Fatalf("listEquipmentHandler returned error: %v", err)
	}
	var payload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("expected one item, got %d", len(payload.Items))
	}
	for _, key := range []string{
		"id", "name", "category", "system", "manufacturer", "model", "serial",
		"quantity", "status", "zone_id", "bin_id", "location_detail",
		"install_date", "hour_meter_path", "profile_id", "aliases",
		"verified_aboard", "notes", "created_at", "updated_at",
		"zone_name", "bin_code", "link_count",
	} {
		if _, ok := payload.Items[0][key]; !ok {
			t.Errorf("field %q missing from an item with nothing filled in", key)
		}
	}
}
