package main

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
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

// TestUpdateEquipmentHandler_UnchangedProfileIDSurvivesProfileDeletion pins
// the fix for validateEquipmentInput re-checking profile_id against
// engineProfiles() on every PUT: a record linked to a profile that is later
// deleted must still be editable as long as the edit does not touch
// profile_id - only a NEW or CHANGED profile_id needs to name a currently
// loaded profile.
func TestUpdateEquipmentHandler_UnchangedProfileIDSurvivesProfileDeletion(t *testing.T) {
	withTestDocumentStore(t)

	// engineProfiles() carries none in this test process - the same state
	// as after the profile file backing "now-deleted-profile" was removed.
	// CreateEquipment goes straight to the store, bypassing
	// validateEquipmentInput's own profile_id check, the same way a record
	// already linked to a profile before it was ever deleted would have
	// gotten there.
	item, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Generator", Category: "mechanical", ProfileID: "now-deleted-profile"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}

	body := `{"name":"Generator","category":"mechanical","profile_id":"now-deleted-profile"}`
	c, rec := newDocumentEchoContext(http.MethodPut, "/api/inventory/equipment/"+item.ID, body, item.ID)
	if err := updateEquipmentHandler(c); err != nil {
		t.Fatalf("updateEquipmentHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for an update that leaves its already-linked profile_id unchanged, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestUpdateEquipmentHandler_ChangedProfileIDStillValidated is the other
// half of the fix above: an update that introduces a DIFFERENT profile_id
// must still be validated against engineProfiles(), not waved through just
// because the record already had some profile_id set.
func TestUpdateEquipmentHandler_ChangedProfileIDStillValidated(t *testing.T) {
	withTestDocumentStore(t)

	item, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Generator", Category: "mechanical", ProfileID: "already-linked-profile"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}

	body := `{"name":"Generator","category":"mechanical","profile_id":"a-different-unknown-profile"}`
	c, rec := newDocumentEchoContext(http.MethodPut, "/api/inventory/equipment/"+item.ID, body, item.ID)
	if err := updateEquipmentHandler(c); err != nil {
		t.Fatalf("updateEquipmentHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a changed profile_id that names no loaded profile, got %d: %s", rec.Code, rec.Body.String())
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
		"zone_name", "bin_code", "link_count", "photo_ids",
	} {
		if _, ok := payload.Items[0][key]; !ok {
			t.Errorf("field %q missing from an item with nothing filled in", key)
		}
	}
}

// ── GET /api/inventory/equipment?bin= ────────────────────────────────────

func TestListEquipmentHandler_FiltersByBin(t *testing.T) {
	withTestDocumentStore(t)

	zone, err := globalDocumentStore.CreateZone("Lazarette")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	binA, err := globalDocumentStore.CreateBin(zone.ID, "LAZ-01", "")
	if err != nil {
		t.Fatalf("CreateBin(a): %v", err)
	}
	binB, err := globalDocumentStore.CreateBin(zone.ID, "LAZ-02", "")
	if err != nil {
		t.Fatalf("CreateBin(b): %v", err)
	}
	if _, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Tape", Category: "general", BinID: &binA.ID}); err != nil {
		t.Fatalf("CreateEquipment(a): %v", err)
	}
	if _, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Caulk", Category: "general", BinID: &binB.ID}); err != nil {
		t.Fatalf("CreateEquipment(b): %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/inventory/equipment?bin="+binA.ID, "", "")
	if err := listEquipmentHandler(c); err != nil {
		t.Fatalf("listEquipmentHandler returned error: %v", err)
	}
	var resp struct {
		Items []equipmentItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].Name != "Tape" {
		t.Fatalf("expected only the item filed in bin A, got %+v", resp.Items)
	}
}

// ── equipment photos (ADR 0127) ─────────────────────────────────────────
// Written before the handlers themselves (AGENTS.md's test-first policy),
// the same convention every other section of this file follows.

// validJPEGBytes/validPNGBytes are just enough of each format's own magic
// number for http.DetectContentType (detectDocumentMIME's own sniffer) to
// recognise them - detectDocumentMIME never looks past the first 512 bytes
// either way (headCapture's own doc comment, documents_handlers.go), so
// neither fixture needs to be a real, decodable image.
var validJPEGBytes = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, []byte("fake jpeg bytes for sniffing")...)
var validPNGBytes = append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("fake png bytes for sniffing")...)

// newInventoryPhotoUploadContext builds a POST /api/inventory/equipment/:id/
// photos multipart request - newDocumentUploadContext's own builder
// (documents_handlers_test.go), extended with the :id path param the photo
// routes key on and a caller-chosen target so the same helper covers both
// the photo upload endpoint's tests below.
func newInventoryPhotoUploadContext(t *testing.T, id string, fields []documentUploadField) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for _, f := range fields {
		var w io.Writer
		var err error
		if f.filename != "" {
			w, err = writer.CreateFormFile(f.name, f.filename)
		} else {
			w, err = writer.CreateFormField(f.name)
		}
		if err != nil {
			t.Fatalf("create part %q: %v", f.name, err)
		}
		if _, err := w.Write(f.content); err != nil {
			t.Fatalf("write part %q: %v", f.name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/inventory/equipment/"+id+"/photos", body)
	req.Header.Set(echo.HeaderContentType, writer.FormDataContentType())
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(id)
	return c, rec
}

// newInventoryEquipmentIDContext is newDocumentEchoContext's own shape with
// a SECOND path param - the photo delete route's own :id/:documentId -
// which newDocumentEchoContext (a single "id" param) can't express.
func newInventoryEquipmentIDContext(method, target, body, id, documentID string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id", "documentId")
	c.SetParamValues(id, documentID)
	return c, rec
}

func TestUploadEquipmentPhotoHandler_TwoUploadsComeBackInUploadOrder(t *testing.T) {
	withTestDocumentStore(t)

	item, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Adhesives bin", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}

	c1, rec1 := newInventoryPhotoUploadContext(t, item.ID, []documentUploadField{{name: "file", filename: "a.jpg", content: validJPEGBytes}})
	if err := uploadEquipmentPhotoHandler(c1); err != nil {
		t.Fatalf("uploadEquipmentPhotoHandler (first): %v", err)
	}
	if rec1.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec1.Code, rec1.Body.String())
	}

	c2, rec2 := newInventoryPhotoUploadContext(t, item.ID, []documentUploadField{{name: "file", filename: "b.png", content: validPNGBytes}})
	if err := uploadEquipmentPhotoHandler(c2); err != nil {
		t.Fatalf("uploadEquipmentPhotoHandler (second): %v", err)
	}
	if rec2.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec2.Code, rec2.Body.String())
	}

	var resp struct {
		Item equipmentItem `json:"item"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Item.PhotoIDs) != 2 {
		t.Fatalf("expected two photos, got %+v", resp.Item.PhotoIDs)
	}
}

func TestUploadEquipmentPhotoHandler_UnknownItemStoresNoDocument(t *testing.T) {
	store := withTestDocumentStore(t)

	before, err := store.ListEquipment(equipmentFilter{})
	if err != nil {
		t.Fatalf("ListEquipment: %v", err)
	}

	c, rec := newInventoryPhotoUploadContext(t, "does-not-exist", []documentUploadField{{name: "file", filename: "a.jpg", content: validJPEGBytes}})
	if err := uploadEquipmentPhotoHandler(c); err != nil {
		t.Fatalf("uploadEquipmentPhotoHandler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}

	entries := documentsDirEntries(t)
	if len(entries) != 0 {
		t.Fatalf("expected no file written for an upload to an unknown item, found %v", entries)
	}
	after, err := store.ListEquipment(equipmentFilter{})
	if err != nil {
		t.Fatalf("ListEquipment: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("expected no document row created, before=%d after=%d", len(before), len(after))
	}
}

func TestUploadEquipmentPhotoHandler_NonImageReturns400(t *testing.T) {
	withTestDocumentStore(t)

	item, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Adhesives bin", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}

	c, rec := newInventoryPhotoUploadContext(t, item.ID, []documentUploadField{{name: "file", filename: "notes.txt", content: []byte("hello world")}})
	if err := uploadEquipmentPhotoHandler(c); err != nil {
		t.Fatalf("uploadEquipmentPhotoHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	entries := documentsDirEntries(t)
	if len(entries) != 0 {
		t.Fatalf("expected no file left behind for a rejected upload, found %v", entries)
	}
}

func TestUploadEquipmentPhotoHandler_HEICReturnsEnrichStageMessage(t *testing.T) {
	withTestDocumentStore(t)

	item, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Adhesives bin", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}

	c, rec := newInventoryPhotoUploadContext(t, item.ID, []documentUploadField{{name: "file", filename: "a.heic", content: []byte("not a real heic but the extension is what matters")}})
	if err := uploadEquipmentPhotoHandler(c); err != nil {
		t.Fatalf("uploadEquipmentPhotoHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "convert to JPEG") {
		t.Fatalf("expected the enrich stage's own HEIC message, got %s", rec.Body.String())
	}
}

// ── PUT /api/inventory/equipment/:id/photos ──────────────────────────────

func TestSetEquipmentPhotoOrderHandler_ReorderMovesSecondToFrontChangesCover(t *testing.T) {
	withTestDocumentStore(t)

	item, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Adhesives bin", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	photoA, err := globalDocumentStore.Insert(document{SHA256: "sha-reorder-a", Filename: "a.jpg", MIME: "image/jpeg", OperatorTags: []string{"photo"}})
	if err != nil {
		t.Fatalf("Insert(a): %v", err)
	}
	photoB, err := globalDocumentStore.Insert(document{SHA256: "sha-reorder-b", Filename: "b.jpg", MIME: "image/jpeg", OperatorTags: []string{"photo"}})
	if err != nil {
		t.Fatalf("Insert(b): %v", err)
	}
	if err := globalDocumentStore.AddEquipmentPhoto(item.ID, photoA.ID); err != nil {
		t.Fatalf("AddEquipmentPhoto(a): %v", err)
	}
	if err := globalDocumentStore.AddEquipmentPhoto(item.ID, photoB.ID); err != nil {
		t.Fatalf("AddEquipmentPhoto(b): %v", err)
	}

	body := `{"document_ids":["` + photoB.ID + `","` + photoA.ID + `"]}`
	c, rec := newDocumentEchoContext(http.MethodPut, "/api/inventory/equipment/"+item.ID+"/photos", body, item.ID)
	if err := setEquipmentPhotoOrderHandler(c); err != nil {
		t.Fatalf("setEquipmentPhotoOrderHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Item equipmentItem `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Item.PhotoIDs) != 2 || resp.Item.PhotoIDs[0] != photoB.ID {
		t.Fatalf("expected b to become the cover, got %+v", resp.Item.PhotoIDs)
	}
}

func TestSetEquipmentPhotoOrderHandler_MismatchedSetReturns400(t *testing.T) {
	withTestDocumentStore(t)

	item, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Adhesives bin", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	photoA, err := globalDocumentStore.Insert(document{SHA256: "sha-mismatch-h-a", Filename: "a.jpg", MIME: "image/jpeg", OperatorTags: []string{"photo"}})
	if err != nil {
		t.Fatalf("Insert(a): %v", err)
	}
	if err := globalDocumentStore.AddEquipmentPhoto(item.ID, photoA.ID); err != nil {
		t.Fatalf("AddEquipmentPhoto(a): %v", err)
	}

	// Missing id: an empty order against one existing photo.
	c, rec := newDocumentEchoContext(http.MethodPut, "/api/inventory/equipment/"+item.ID+"/photos", `{"document_ids":[]}`, item.ID)
	if err := setEquipmentPhotoOrderHandler(c); err != nil {
		t.Fatalf("setEquipmentPhotoOrderHandler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a missing id, got %d: %s", rec.Code, rec.Body.String())
	}

	// Extra id: a photo id the item does not own.
	body := `{"document_ids":["` + photoA.ID + `","does-not-exist"]}`
	c2, rec2 := newDocumentEchoContext(http.MethodPut, "/api/inventory/equipment/"+item.ID+"/photos", body, item.ID)
	if err := setEquipmentPhotoOrderHandler(c2); err != nil {
		t.Fatalf("setEquipmentPhotoOrderHandler returned error: %v", err)
	}
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an extra id, got %d: %s", rec2.Code, rec2.Body.String())
	}
}

// ── DELETE /api/inventory/equipment/:id/photos/:documentId ──────────────

func TestDeleteEquipmentPhotoHandler_RemovesFileAndLink(t *testing.T) {
	withTestDocumentStore(t)

	item, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Adhesives bin", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	c1, rec1 := newInventoryPhotoUploadContext(t, item.ID, []documentUploadField{{name: "file", filename: "a.jpg", content: validJPEGBytes}})
	if err := uploadEquipmentPhotoHandler(c1); err != nil {
		t.Fatalf("uploadEquipmentPhotoHandler: %v", err)
	}
	var uploaded struct {
		Item equipmentItem `json:"item"`
	}
	if err := json.Unmarshal(rec1.Body.Bytes(), &uploaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	photoID := uploaded.Item.PhotoIDs[0]

	c, rec := newInventoryEquipmentIDContext(http.MethodDelete, "/api/inventory/equipment/"+item.ID+"/photos/"+photoID, "", item.ID, photoID)
	if err := deleteEquipmentPhotoHandler(c); err != nil {
		t.Fatalf("deleteEquipmentPhotoHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	entries := documentsDirEntries(t)
	if len(entries) != 0 {
		t.Fatalf("expected the photo's file removed from disk, found %v", entries)
	}
}

// TestDeleteEquipmentPhotoHandler_SharedPhotoSurvivesRemovalFromOneItem pins
// the most serious ADR 0127 review finding: uploads are deduplicated by
// sha256, so the SAME bytes uploaded as a photo on two different items share
// one documents row and one file on disk. Removing the photo from item A
// must leave item B's photo (and the underlying file) intact.
func TestDeleteEquipmentPhotoHandler_SharedPhotoSurvivesRemovalFromOneItem(t *testing.T) {
	withTestDocumentStore(t)

	itemA, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Bin A item", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment(A): %v", err)
	}
	itemB, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Bin B item", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment(B): %v", err)
	}

	cA, recA := newInventoryPhotoUploadContext(t, itemA.ID, []documentUploadField{{name: "file", filename: "a.jpg", content: validJPEGBytes}})
	if err := uploadEquipmentPhotoHandler(cA); err != nil {
		t.Fatalf("uploadEquipmentPhotoHandler(A): %v", err)
	}
	var uploadedA struct {
		Item equipmentItem `json:"item"`
	}
	if err := json.Unmarshal(recA.Body.Bytes(), &uploadedA); err != nil {
		t.Fatalf("unmarshal(A): %v", err)
	}
	photoID := uploadedA.Item.PhotoIDs[0]

	// Same bytes onto item B - Insert's own sha256 dedupe links the SAME
	// document row rather than storing a second file.
	cB, recB := newInventoryPhotoUploadContext(t, itemB.ID, []documentUploadField{{name: "file", filename: "a.jpg", content: validJPEGBytes}})
	if err := uploadEquipmentPhotoHandler(cB); err != nil {
		t.Fatalf("uploadEquipmentPhotoHandler(B): %v", err)
	}
	var uploadedB struct {
		Item equipmentItem `json:"item"`
	}
	if err := json.Unmarshal(recB.Body.Bytes(), &uploadedB); err != nil {
		t.Fatalf("unmarshal(B): %v", err)
	}
	if len(uploadedB.Item.PhotoIDs) != 1 || uploadedB.Item.PhotoIDs[0] != photoID {
		t.Fatalf("expected B to link the SAME deduplicated document, got %+v", uploadedB.Item.PhotoIDs)
	}

	c, rec := newInventoryEquipmentIDContext(http.MethodDelete, "/api/inventory/equipment/"+itemA.ID+"/photos/"+photoID, "", itemA.ID, photoID)
	if err := deleteEquipmentPhotoHandler(c); err != nil {
		t.Fatalf("deleteEquipmentPhotoHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	entries := documentsDirEntries(t)
	if len(entries) != 1 {
		t.Fatalf("expected the shared file to survive on disk, found %v", entries)
	}

	itemBAfter, err := globalDocumentStore.GetEquipment(itemB.ID)
	if err != nil {
		t.Fatalf("GetEquipment(B): %v", err)
	}
	if len(itemBAfter.PhotoIDs) != 1 || itemBAfter.PhotoIDs[0] != photoID {
		t.Fatalf("expected item B's photo untouched, got %+v", itemBAfter.PhotoIDs)
	}

	c2, rec2 := newDocumentEchoContext(http.MethodGet, "/api/documents/"+photoID+"/content", "", photoID)
	if err := documentContentHandler(c2); err != nil {
		t.Fatalf("documentContentHandler: %v", err)
	}
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected the shared photo's content still servable, got %d: %s", rec2.Code, rec2.Body.String())
	}
}

// TestDeleteEquipmentHandler_CascadesPhotoLinks is a required Verification
// case: "Deleting the item cascades its photo links."
func TestDeleteEquipmentHandler_CascadesPhotoLinks(t *testing.T) {
	store := withTestDocumentStore(t)

	item, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Adhesives bin", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	photo, err := globalDocumentStore.Insert(document{SHA256: "sha-cascade-photo", Filename: "a.jpg", MIME: "image/jpeg", OperatorTags: []string{"photo"}})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := globalDocumentStore.AddEquipmentPhoto(item.ID, photo.ID); err != nil {
		t.Fatalf("AddEquipmentPhoto: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodDelete, "/api/inventory/equipment/"+item.ID, "", item.ID)
	if err := deleteEquipmentHandler(c); err != nil {
		t.Fatalf("deleteEquipmentHandler returned error: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM equipment_documents WHERE equipment_id = ?`, item.ID).Scan(&count); err != nil {
		t.Fatalf("count equipment_documents: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected the photo link to cascade away with the item, found %d still there", count)
	}
}

// ── PUT /api/inventory/equipment/:id/documents leaves photos alone ───────

func TestSetEquipmentDocumentsHandler_LeavesPhotoLinksAndOrderUntouched(t *testing.T) {
	withTestDocumentStore(t)

	item, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Adhesives bin", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	photoA, err := globalDocumentStore.Insert(document{SHA256: "sha-h-preserve-a", Filename: "a.jpg", MIME: "image/jpeg", OperatorTags: []string{"photo"}})
	if err != nil {
		t.Fatalf("Insert(a): %v", err)
	}
	photoB, err := globalDocumentStore.Insert(document{SHA256: "sha-h-preserve-b", Filename: "b.jpg", MIME: "image/jpeg", OperatorTags: []string{"photo"}})
	if err != nil {
		t.Fatalf("Insert(b): %v", err)
	}
	if err := globalDocumentStore.AddEquipmentPhoto(item.ID, photoA.ID); err != nil {
		t.Fatalf("AddEquipmentPhoto(a): %v", err)
	}
	if err := globalDocumentStore.AddEquipmentPhoto(item.ID, photoB.ID); err != nil {
		t.Fatalf("AddEquipmentPhoto(b): %v", err)
	}
	manual, err := globalDocumentStore.Insert(document{SHA256: "sha-h-preserve-manual", Filename: "manual.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert(manual): %v", err)
	}

	body := `{"document_ids":["` + manual.ID + `"]}`
	c, rec := newDocumentEchoContext(http.MethodPut, "/api/inventory/equipment/"+item.ID+"/documents", body, item.ID)
	if err := setEquipmentDocumentsHandler(c); err != nil {
		t.Fatalf("setEquipmentDocumentsHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	item2, err := globalDocumentStore.GetEquipment(item.ID)
	if err != nil {
		t.Fatalf("GetEquipment: %v", err)
	}
	if len(item2.PhotoIDs) != 2 || item2.PhotoIDs[0] != photoA.ID || item2.PhotoIDs[1] != photoB.ID {
		t.Fatalf("expected both photo links and their order untouched, got %+v", item2.PhotoIDs)
	}
}

// ── removing the 'photo' tag ─────────────────────────────────────────────

func TestGetEquipmentHandler_RemovingPhotoTagDropsFromPhotoIDsButKeepsLink(t *testing.T) {
	withTestDocumentStore(t)

	item, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Adhesives bin", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	photo, err := globalDocumentStore.Insert(document{SHA256: "sha-h-untag", Filename: "a.jpg", MIME: "image/jpeg", OperatorTags: []string{"photo"}})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := globalDocumentStore.AddEquipmentPhoto(item.ID, photo.ID); err != nil {
		t.Fatalf("AddEquipmentPhoto: %v", err)
	}

	if err := globalDocumentStore.UpdateMeta(photo.ID, nil, nil, []string{"consumable"}); err != nil {
		t.Fatalf("UpdateMeta: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/inventory/equipment/"+item.ID, "", item.ID)
	if err := getEquipmentHandler(c); err != nil {
		t.Fatalf("getEquipmentHandler returned error: %v", err)
	}
	var resp struct {
		Item      equipmentItem       `json:"item"`
		Documents []equipmentDocument `json:"documents"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Item.PhotoIDs) != 0 {
		t.Fatalf("expected the untagged photo to leave photo_ids, got %+v", resp.Item.PhotoIDs)
	}
	if len(resp.Documents) != 1 || resp.Documents[0].DocumentID != photo.ID {
		t.Fatalf("expected the link itself to survive as an ordinary linked document, got %+v", resp.Documents)
	}
}
