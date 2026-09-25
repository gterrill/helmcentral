package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	if err := globalDocumentStore.PatchEquipmentDocuments(item.ID, []string{doc.ID}, nil); err != nil {
		t.Fatalf("PatchEquipmentDocuments: %v", err)
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

// TestUpdateEquipmentHandlerSeedsFullBankRuleWhenProfileIDLinksHouseBank is
// code review finding 3: vesselHouseBankReady (alarm_seed_anomaly.go)
// resolves the house bank's linked profile through the Inventory item's own
// profile_id (equipmentProfile, anomaly_detector.go) -- the OTHER half of
// its dependency besides the profile's own full_soc/charge_warn slots
// (engine_profiles.go's updateProfileHandler already re-seeds for that
// half, seedAnomalyRulesAfterProfileSave). updateEquipmentHandler never
// re-seeded, so linking an already-complete battery profile to the house
// bank's Inventory record took no effect until the next vessel-settings
// save or a server restart. Fixed at the source: updateEquipmentHandler
// now re-seeds whenever the record it just saved is the vessel's own house
// bank -- scoped that narrowly, the same way the profile-save half is
// scoped to profileKindBattery only, so an unrelated equipment edit (a
// spare impeller's quantity) does no needless work.
func TestUpdateEquipmentHandlerSeedsFullBankRuleWhenProfileIDLinksHouseBank(t *testing.T) {
	withTestDocumentStore(t)
	withTempAlarmRules(t)
	setupBatteryProfileFixture(t)

	item, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "House bank", Category: "mechanical"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}

	settingsPath := filepath.Join(t.TempDir(), "settings.yaml")
	t.Setenv("SETTINGS_FILE", settingsPath)
	if err := saveVesselSettings(settingsPath, vesselSettings{
		HouseBank: &vesselHouseBankSetting{Path: "electrical.batteries.0", EquipmentID: item.ID, CapacityAh: 400, Cells: 8},
	}); err != nil {
		t.Fatalf("saveVesselSettings: %v", err)
	}

	if _, ok := findAlarmRule(t, anomalyFullBankWarnRuleID); ok {
		t.Fatalf("did not expect the full-bank rule seeded before the house bank's Inventory item had a linked profile")
	}

	body := `{"name":"House bank","category":"mechanical","profile_id":"test-battery-wiring"}`
	c, rec := newDocumentEchoContext(http.MethodPut, "/api/inventory/equipment/"+item.ID, body, item.ID)
	if err := updateEquipmentHandler(c); err != nil {
		t.Fatalf("updateEquipmentHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if _, ok := findAlarmRule(t, anomalyFullBankWarnRuleID); !ok {
		t.Fatalf("expected linking the house bank's Inventory item to a complete battery profile to seed the full-bank rule immediately, not wait for the next vessel-settings save or restart")
	}
}

// TestUpdateEquipmentHandlerDoesNotReseedForUnrelatedEquipment guards the
// scoping half of the fix above: an equipment record that is NOT the
// vessel's own house bank must not trigger a reseed at all -- there is
// nothing for it to complete, and doing this unconditionally on every save
// would be needless work (loadVesselSettings plus a full seedAnomalyRules
// pass) for no operator-visible effect.
func TestUpdateEquipmentHandlerDoesNotReseedForUnrelatedEquipment(t *testing.T) {
	withTestDocumentStore(t)
	withTempAlarmRules(t)
	setupBatteryProfileFixture(t)

	settingsPath := filepath.Join(t.TempDir(), "settings.yaml")
	t.Setenv("SETTINGS_FILE", settingsPath)
	if err := saveVesselSettings(settingsPath, vesselSettings{}); err != nil {
		t.Fatalf("saveVesselSettings: %v", err)
	}

	item, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Spare impeller", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}

	body := `{"name":"Spare impeller","category":"general","profile_id":"test-battery-wiring"}`
	c, rec := newDocumentEchoContext(http.MethodPut, "/api/inventory/equipment/"+item.ID, body, item.ID)
	if err := updateEquipmentHandler(c); err != nil {
		t.Fatalf("updateEquipmentHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if _, ok := findAlarmRule(t, anomalyFullBankWarnRuleID); ok {
		t.Fatalf("did not expect a reseed for equipment that is not the vessel's own house bank, even though its profile_id happens to be a complete battery profile")
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

// ── PATCH /api/inventory/equipment/:id/documents ─────────────────────────

// TestPatchEquipmentDocumentsHandler_AddsAndRemoves is the handler-level
// core case: a single PATCH body naming both add and remove ids applies
// both.
func TestPatchEquipmentDocumentsHandler_AddsAndRemoves(t *testing.T) {
	withTestDocumentStore(t)

	item, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Generator", Category: "mechanical"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	docA, err := globalDocumentStore.Insert(document{SHA256: "sha-patch-eq-a", Filename: "a.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	docB, err := globalDocumentStore.Insert(document{SHA256: "sha-patch-eq-b", Filename: "b.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := globalDocumentStore.PatchEquipmentDocuments(item.ID, []string{docA.ID}, nil); err != nil {
		t.Fatalf("PatchEquipmentDocuments (seed): %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/inventory/equipment/"+item.ID+"/documents",
		`{"add":["`+docB.ID+`"],"remove":["`+docA.ID+`"]}`, item.ID)
	if err := patchEquipmentDocumentsHandler(c); err != nil {
		t.Fatalf("patchEquipmentDocumentsHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Documents []equipmentDocument `json:"documents"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Documents) != 1 || resp.Documents[0].DocumentID != docB.ID {
		t.Fatalf("expected only docB linked (docA removed, docB added), got %+v", resp.Documents)
	}
}

// TestPatchEquipmentDocumentsHandler_UnknownDocumentIDReturns404WithName is
// the PATCH-form port: an unknown id in add is pre-checked and named in a
// clean 404, same as the old PUT handler did.
func TestPatchEquipmentDocumentsHandler_UnknownDocumentIDReturns404WithName(t *testing.T) {
	withTestDocumentStore(t)

	item, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Generator", Category: "mechanical"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/inventory/equipment/"+item.ID+"/documents", `{"add":["does-not-exist"]}`, item.ID)
	if err := patchEquipmentDocumentsHandler(c); err != nil {
		t.Fatalf("patchEquipmentDocumentsHandler returned error: %v", err)
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

// TestPatchEquipmentDocumentsHandler_AcceptsAPhoto pins the same "a photo is
// an ordinary linked document" rule at the PATCH handler layer: an
// image/jpeg or image/png id in add is linked exactly like any other, and
// shows up in both EquipmentDocuments (the Documents tab) and, because it's
// an image, GetEquipment's own PhotoIDs view (the strip).
func TestPatchEquipmentDocumentsHandler_AcceptsAPhoto(t *testing.T) {
	withTestDocumentStore(t)

	item, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Generator", Category: "mechanical"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	photo, err := globalDocumentStore.Insert(document{SHA256: "sha-patch-eq-accept-photo", Filename: "engine.jpg", MIME: "image/jpeg"})
	if err != nil {
		t.Fatalf("Insert(photo): %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/inventory/equipment/"+item.ID+"/documents", `{"add":["`+photo.ID+`"]}`, item.ID)
	if err := patchEquipmentDocumentsHandler(c); err != nil {
		t.Fatalf("patchEquipmentDocumentsHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	docs, err := globalDocumentStore.EquipmentDocuments(item.ID)
	if err != nil {
		t.Fatalf("EquipmentDocuments: %v", err)
	}
	if len(docs) != 1 || docs[0].DocumentID != photo.ID {
		t.Fatalf("expected the photo linked as an ordinary document, got %+v", docs)
	}

	got, err := globalDocumentStore.GetEquipment(item.ID)
	if err != nil {
		t.Fatalf("GetEquipment: %v", err)
	}
	if len(got.PhotoIDs) != 1 || got.PhotoIDs[0] != photo.ID {
		t.Fatalf("expected the same document to also show up in the photo strip, got %#v", got.PhotoIDs)
	}
}

// TestPatchEquipmentDocumentsHandler_LeavesUnnamedPhotoAlone is the
// handler-level port of the diff-based PATCH's core promise: a photo linked
// via the photo upload route, never named in add or remove, survives a
// PATCH that touches an unrelated ordinary document.
func TestPatchEquipmentDocumentsHandler_LeavesUnnamedPhotoAlone(t *testing.T) {
	withTestDocumentStore(t)

	item, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Adhesives bin", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	photo, err := globalDocumentStore.Insert(document{SHA256: "sha-patch-h-untouched-photo", Filename: "a.jpg", MIME: "image/jpeg"})
	if err != nil {
		t.Fatalf("Insert(photo): %v", err)
	}
	if err := globalDocumentStore.AddEquipmentPhoto(item.ID, photo.ID); err != nil {
		t.Fatalf("AddEquipmentPhoto: %v", err)
	}
	manual, err := globalDocumentStore.Insert(document{SHA256: "sha-patch-h-untouched-manual", Filename: "manual.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatalf("Insert(manual): %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/inventory/equipment/"+item.ID+"/documents", `{"add":["`+manual.ID+`"]}`, item.ID)
	if err := patchEquipmentDocumentsHandler(c); err != nil {
		t.Fatalf("patchEquipmentDocumentsHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	item2, err := globalDocumentStore.GetEquipment(item.ID)
	if err != nil {
		t.Fatalf("GetEquipment: %v", err)
	}
	if len(item2.PhotoIDs) != 1 || item2.PhotoIDs[0] != photo.ID {
		t.Fatalf("expected the photo, never named in add or remove, still on the strip, got %+v", item2.PhotoIDs)
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

// TestUploadEquipmentPhotoHandler_DuplicateOntoNonPhotoLibraryDocumentReturns409
// pins the amended ADR 0127 design (review finding): uploading bytes that
// already exist in the library as an ORDINARY document (never tagged
// 'photo') used to silently tag that document 'photo' and link it -
// repurposing a document the operator filed under Documents for something
// they never asked to attach as a photo. The upload is refused instead, and
// nothing about the existing document changes.
// TestUploadEquipmentPhotoHandler_DuplicateLinksExistingDocument pins the
// 2026-09-25 amendment: there is no longer a 409 for a byte-identical
// upload onto a document that wasn't already "a photo" - that distinction
// no longer exists. It is simply linked, whatever it was filed under, and
// no new document row or file is created (sha256 dedupe, Insert's own
// contract).
func TestUploadEquipmentPhotoHandler_DuplicateLinksExistingDocument(t *testing.T) {
	withTestDocumentStore(t)

	item, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Adhesives bin", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}

	sum := sha256.Sum256(validJPEGBytes)
	sha := hex.EncodeToString(sum[:])
	existing, err := globalDocumentStore.Insert(document{SHA256: sha, Filename: "receipt.jpg", Title: "Fuel receipt", MIME: "image/jpeg"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	c, rec := newInventoryPhotoUploadContext(t, item.ID, []documentUploadField{{name: "file", filename: "a.jpg", content: validJPEGBytes}})
	if err := uploadEquipmentPhotoHandler(c); err != nil {
		t.Fatalf("uploadEquipmentPhotoHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (link, not a new upload), got %d: %s", rec.Code, rec.Body.String())
	}

	itemAfter, err := globalDocumentStore.GetEquipment(item.ID)
	if err != nil {
		t.Fatalf("GetEquipment: %v", err)
	}
	if len(itemAfter.PhotoIDs) != 1 || itemAfter.PhotoIDs[0] != existing.ID {
		t.Fatalf("expected the existing document linked as the item's photo, got %+v", itemAfter.PhotoIDs)
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

// TestDeleteEquipmentPhotoHandler_DefaultUnlinksOnlyFileSurvives pins the
// 2026-09-25 amendment: without ?delete=true, removing a photo from the
// strip is unlink-only - the document and its file on disk both survive.
func TestDeleteEquipmentPhotoHandler_DefaultUnlinksOnlyFileSurvives(t *testing.T) {
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
	if len(entries) != 1 {
		t.Fatalf("expected the photo's file to survive an ordinary (unlink-only) remove, found %v", entries)
	}
	itemAfter, err := globalDocumentStore.GetEquipment(item.ID)
	if err != nil {
		t.Fatalf("GetEquipment: %v", err)
	}
	if len(itemAfter.PhotoIDs) != 0 {
		t.Fatalf("expected the photo unlinked from the item, got %+v", itemAfter.PhotoIDs)
	}
	if _, err := globalDocumentStore.Get(photoID); err != nil {
		t.Fatalf("expected the document itself to survive, got %v", err)
	}
}

// TestDeleteEquipmentPhotoHandler_DeleteFlagRemovesFileAndDocument pins the
// explicit-choice half: ?delete=true additionally removes the document row
// and its file, but only once nothing else links it.
func TestDeleteEquipmentPhotoHandler_DeleteFlagRemovesFileAndDocument(t *testing.T) {
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

	c, rec := newInventoryEquipmentIDContext(http.MethodDelete, "/api/inventory/equipment/"+item.ID+"/photos/"+photoID+"?delete=true", "", item.ID, photoID)
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
	if _, err := globalDocumentStore.Get(photoID); !errors.Is(err, errDocumentNotFound) {
		t.Fatalf("expected the photo document itself deleted, got %v", err)
	}
}

// TestDeleteEquipmentPhotoHandler_DeleteFlagSharedPhotoSurvivesRemovalFromOneItem
// pins the most serious ADR 0127 review finding, still true under the
// explicit-choice flow: uploads are deduplicated by sha256, so the SAME
// bytes uploaded as a photo on two different items share one documents row
// and one file on disk. Removing item A's photo, even with ?delete=true,
// must leave item B's photo (and the underlying file) intact - it is not
// EXCLUSIVE to A.
func TestDeleteEquipmentPhotoHandler_DeleteFlagSharedPhotoSurvivesRemovalFromOneItem(t *testing.T) {
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

	c, rec := newInventoryEquipmentIDContext(http.MethodDelete, "/api/inventory/equipment/"+itemA.ID+"/photos/"+photoID+"?delete=true", "", itemA.ID, photoID)
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
	photo, err := globalDocumentStore.Insert(document{SHA256: "sha-cascade-photo", Filename: "a.jpg", MIME: "image/jpeg"})
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

// TestDeleteEquipmentHandler_DefaultKeepsPhotoDocumentAndFile pins the
// operator's 2026-09-25 decision: an ordinary item delete (no
// ?delete_photos=true) never destroys a linked photo document - only the
// link cascades away.
func TestDeleteEquipmentHandler_DefaultKeepsPhotoDocumentAndFile(t *testing.T) {
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

	c, rec := newDocumentEchoContext(http.MethodDelete, "/api/inventory/equipment/"+item.ID, "", item.ID)
	if err := deleteEquipmentHandler(c); err != nil {
		t.Fatalf("deleteEquipmentHandler returned error: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	if entries := documentsDirEntries(t); len(entries) != 1 {
		t.Fatalf("expected the photo's file to survive an ordinary delete, found %v", entries)
	}
	if _, err := globalDocumentStore.Get(photoID); err != nil {
		t.Fatalf("expected the photo document itself to survive, got %v", err)
	}
}

// TestDeleteEquipmentHandler_FlagRemovesUnsharedPhotoDocumentAndFile is item
// 1 of the pre-release review, now behind the explicit ?delete_photos=true
// choice: with the flag, the photo's document row and its file on disk are
// both removed along with the item, when nothing else links it.
func TestDeleteEquipmentHandler_FlagRemovesUnsharedPhotoDocumentAndFile(t *testing.T) {
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
	if len(documentsDirEntries(t)) != 1 {
		t.Fatalf("expected the uploaded photo's file on disk before delete")
	}

	c, rec := newDocumentEchoContext(http.MethodDelete, "/api/inventory/equipment/"+item.ID+"?delete_photos=true", "", item.ID)
	if err := deleteEquipmentHandler(c); err != nil {
		t.Fatalf("deleteEquipmentHandler returned error: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	if entries := documentsDirEntries(t); len(entries) != 0 {
		t.Fatalf("expected the photo's file removed from disk, found %v", entries)
	}
	if _, err := globalDocumentStore.Get(photoID); !errors.Is(err, errDocumentNotFound) {
		t.Fatalf("expected the photo document itself deleted, got %v", err)
	}
}

// TestDeleteEquipmentHandler_FlagSharedPhotoFileSurvivesEquipmentDelete pins
// the sha256-dedupe sharing rule at the handler layer, still under the
// explicit ?delete_photos=true choice: item A and item B share one uploaded
// photo (same bytes), and deleting item A must leave item B's copy -
// document row AND file - completely alone, because it is not EXCLUSIVE to A.
func TestDeleteEquipmentHandler_FlagSharedPhotoFileSurvivesEquipmentDelete(t *testing.T) {
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

	c, rec := newDocumentEchoContext(http.MethodDelete, "/api/inventory/equipment/"+itemA.ID+"?delete_photos=true", "", itemA.ID)
	if err := deleteEquipmentHandler(c); err != nil {
		t.Fatalf("deleteEquipmentHandler returned error: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
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

// TestDeleteEquipmentHandler_KeepsOrdinaryLinkedDocumentAndFile pins the
// other half of item 1: an ordinary (non-photo) linked document must NOT be
// deleted along with the item even with the flag set - only its
// equipment_documents link cascades away, same as before this fix.
func TestDeleteEquipmentHandler_KeepsOrdinaryLinkedDocumentAndFile(t *testing.T) {
	withTestDocumentStore(t)
	store := globalDocumentStore

	item, err := store.CreateEquipment(equipmentItem{Name: "Generator", Category: "mechanical"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	manual := insertTestDocumentWithFile(t, store, "sha-delete-eq-handler-manual", "manual.pdf", "application/pdf", []byte("manual bytes"))
	if err := store.PatchEquipmentDocuments(item.ID, []string{manual.ID}, nil); err != nil {
		t.Fatalf("PatchEquipmentDocuments: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodDelete, "/api/inventory/equipment/"+item.ID+"?delete_photos=true", "", item.ID)
	if err := deleteEquipmentHandler(c); err != nil {
		t.Fatalf("deleteEquipmentHandler returned error: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	if entries := documentsDirEntries(t); len(entries) != 1 {
		t.Fatalf("expected the ordinary linked document's file to survive, found %v", entries)
	}
	if _, err := store.Get(manual.ID); err != nil {
		t.Fatalf("expected the ordinary linked document to survive, got %v", err)
	}
}

// ── PATCH /api/inventory/equipment/:id/documents removes only what's named ─

// TestPatchEquipmentDocumentsHandler_RemovesOnlyTheNamedPhoto pins the
// PATCH-form replacement for the old whole-set "unlink what's left out"
// test: remove must name a photo explicitly to unlink it - the document
// itself survives.
func TestPatchEquipmentDocumentsHandler_RemovesOnlyTheNamedPhoto(t *testing.T) {
	withTestDocumentStore(t)

	item, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Adhesives bin", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	photo, err := globalDocumentStore.Insert(document{SHA256: "sha-patch-h-unlink-photo", Filename: "a.jpg", MIME: "image/jpeg"})
	if err != nil {
		t.Fatalf("Insert(photo): %v", err)
	}
	if err := globalDocumentStore.AddEquipmentPhoto(item.ID, photo.ID); err != nil {
		t.Fatalf("AddEquipmentPhoto: %v", err)
	}

	body := `{"remove":["` + photo.ID + `"]}`
	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/inventory/equipment/"+item.ID+"/documents", body, item.ID)
	if err := patchEquipmentDocumentsHandler(c); err != nil {
		t.Fatalf("patchEquipmentDocumentsHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	item2, err := globalDocumentStore.GetEquipment(item.ID)
	if err != nil {
		t.Fatalf("GetEquipment: %v", err)
	}
	if len(item2.PhotoIDs) != 0 {
		t.Fatalf("expected the photo unlinked, got %+v", item2.PhotoIDs)
	}
	if _, err := globalDocumentStore.Get(photo.ID); err != nil {
		t.Fatalf("expected the photo document itself to survive, got %v", err)
	}
}

// TestPatchEquipmentDocumentsHandler_NewLinkGoesLastNotBeforeCover pins the
// PATCH-form replacement for the old whole-set sort_index test: an id in
// add lands after the item's existing photos (the cover included), at the
// handler layer.
func TestPatchEquipmentDocumentsHandler_NewLinkGoesLastNotBeforeCover(t *testing.T) {
	withTestDocumentStore(t)

	item, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Adhesives bin", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	cover, err := globalDocumentStore.Insert(document{SHA256: "sha-patch-h-cover", Filename: "cover.jpg", MIME: "image/jpeg"})
	if err != nil {
		t.Fatalf("Insert(cover): %v", err)
	}
	if err := globalDocumentStore.AddEquipmentPhoto(item.ID, cover.ID); err != nil {
		t.Fatalf("AddEquipmentPhoto(cover): %v", err)
	}
	newPhoto, err := globalDocumentStore.Insert(document{SHA256: "sha-patch-h-new", Filename: "new.jpg", MIME: "image/jpeg"})
	if err != nil {
		t.Fatalf("Insert(new): %v", err)
	}

	body := `{"add":["` + newPhoto.ID + `"]}`
	c, rec := newDocumentEchoContext(http.MethodPatch, "/api/inventory/equipment/"+item.ID+"/documents", body, item.ID)
	if err := patchEquipmentDocumentsHandler(c); err != nil {
		t.Fatalf("patchEquipmentDocumentsHandler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	item2, err := globalDocumentStore.GetEquipment(item.ID)
	if err != nil {
		t.Fatalf("GetEquipment: %v", err)
	}
	if len(item2.PhotoIDs) != 2 || item2.PhotoIDs[0] != cover.ID || item2.PhotoIDs[1] != newPhoto.ID {
		t.Fatalf("expected the cover unchanged and the new photo last, got %+v", item2.PhotoIDs)
	}
}

// ── exclusive_photo_ids ───────────────────────────────────────────────────

// TestGetEquipmentHandler_ExclusivePhotoIDsNamesOnlyExclusivePhotos pins the
// GET response's own delete-dialog support field.
func TestGetEquipmentHandler_ExclusivePhotoIDsNamesOnlyExclusivePhotos(t *testing.T) {
	withTestDocumentStore(t)

	item, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Adhesives bin", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	other, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Other item", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment(other): %v", err)
	}
	exclusive, err := globalDocumentStore.Insert(document{SHA256: "sha-h-exclusive", Filename: "a.jpg", MIME: "image/jpeg"})
	if err != nil {
		t.Fatalf("Insert(exclusive): %v", err)
	}
	shared, err := globalDocumentStore.Insert(document{SHA256: "sha-h-shared", Filename: "b.jpg", MIME: "image/jpeg"})
	if err != nil {
		t.Fatalf("Insert(shared): %v", err)
	}
	if err := globalDocumentStore.AddEquipmentPhoto(item.ID, exclusive.ID); err != nil {
		t.Fatalf("AddEquipmentPhoto(exclusive): %v", err)
	}
	if err := globalDocumentStore.AddEquipmentPhoto(item.ID, shared.ID); err != nil {
		t.Fatalf("AddEquipmentPhoto(shared, item): %v", err)
	}
	if err := globalDocumentStore.AddEquipmentPhoto(other.ID, shared.ID); err != nil {
		t.Fatalf("AddEquipmentPhoto(shared, other): %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/inventory/equipment/"+item.ID, "", item.ID)
	if err := getEquipmentHandler(c); err != nil {
		t.Fatalf("getEquipmentHandler returned error: %v", err)
	}
	var resp struct {
		Item equipmentItem `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Item.ExclusivePhotoIDs) != 1 || resp.Item.ExclusivePhotoIDs[0] != exclusive.ID {
		t.Fatalf("expected only the exclusive photo named, got %+v", resp.Item.ExclusivePhotoIDs)
	}
}

// TestDeleteEquipmentHandler_KeepsRemovingPhotoFilesAfterOneFails is from the
// final pre-release review: the handler used to return on the first photo
// file it could not remove, leaving every later photo's file behind - and
// by then the item and its photo rows were already gone, so nothing would
// ever try again. Every file is attempted; a failure is still reported,
// saying plainly that the item itself was deleted. Runs under
// ?delete_photos=true - the only case any photo file is ever touched by
// this handler any more.
func TestDeleteEquipmentHandler_KeepsRemovingPhotoFilesAfterOneFails(t *testing.T) {
	withTestDocumentStore(t)

	item, err := globalDocumentStore.CreateEquipment(equipmentItem{Name: "Adhesives bin", Category: "general"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	dir := documentsDirPath()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	for _, sha := range []string{"sha-a-unremovable", "sha-b-removable"} {
		photo, err := globalDocumentStore.Insert(document{SHA256: sha, Filename: sha + ".jpg", MIME: "image/jpeg"})
		if err != nil {
			t.Fatalf("Insert: %v", err)
		}
		if err := globalDocumentStore.AddEquipmentPhoto(item.ID, photo.ID); err != nil {
			t.Fatalf("AddEquipmentPhoto: %v", err)
		}
	}
	// A non-empty directory where the first photo's file should be makes
	// os.Remove fail for it; the second is an ordinary file.
	if err := os.MkdirAll(filepath.Join(dir, "sha-a-unremovable", "keep"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sha-b-removable"), []byte("jpeg"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	c, rec := newDocumentEchoContext(http.MethodDelete, "/api/inventory/equipment/"+item.ID+"?delete_photos=true", "", item.ID)
	if err := deleteEquipmentHandler(c); err != nil {
		t.Fatalf("deleteEquipmentHandler returned error: %v", err)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for the file that could not be removed, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "deleted") {
		t.Fatalf("expected the error to say the item itself was deleted, got %s", rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "sha-b-removable")); !os.IsNotExist(err) {
		t.Fatalf("expected the second photo's file removed despite the first failing, stat err = %v", err)
	}
}
