package main

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/labstack/echo/v4"
)

// This file serves the inventory feature's HTTP API (plan "Inventory: the
// equipment registry and locations", Phase 1): zones, bins and equipment
// under /api/inventory/ - a new API family, deliberately separate from
// /api/equipment-profiles (engine_profiles.go), which stays exactly where
// it is (plan's own decision: "Profiles move, they do not change" - only
// the frontend section that surfaces them moves, in Phase 2). manuals_
// handlers.go and documents_handlers.go are this file's own models: reuse
// writeDocumentError/documentErrorStatus for sentinel-error mapping, call
// the handler directly from tests rather than through a router, and bind
// request bodies with c.Bind the same way every other write handler here
// does.

// ── validation ───────────────────────────────────────────────────────────

// inventoryValidationError names the single request field a POST/PUT
// /api/inventory/equipment body failed on, in the {field, message} shape
// task instructions point at validateSettingsChange (signalk.go) for: one
// struct, one failure reported at a time - a form-per-field UI wants to
// know WHICH input to flag, not a full list of everything currently wrong
// with it. installDatePattern backs the one field whose "valid" shape is a
// literal pattern rather than a fixed enum.
type inventoryValidationError struct {
	Field   string
	Message string
}

func (e *inventoryValidationError) Error() string { return e.Message }

var installDatePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// equipmentRequest is the JSON body POST/PUT /api/inventory/equipment
// accept - a full replace on PUT, matching CreateEquipment/UpdateEquipment's
// own whole-record contract (inventory_store.go).
type equipmentRequest struct {
	Name           string   `json:"name"`
	Category       string   `json:"category"`
	System         string   `json:"system"`
	Manufacturer   string   `json:"manufacturer"`
	Model          string   `json:"model"`
	Serial         string   `json:"serial"`
	Quantity       int      `json:"quantity"`
	Status         string   `json:"status"`
	ZoneID         *string  `json:"zone_id"`
	BinID          *string  `json:"bin_id"`
	LocationDetail string   `json:"location_detail"`
	InstallDate    string   `json:"install_date"`
	HourMeterPath  string   `json:"hour_meter_path"`
	ProfileID      string   `json:"profile_id"`
	Aliases        []string `json:"aliases"`
	VerifiedAboard bool     `json:"verified_aboard"`
	Notes          string   `json:"notes"`
}

// trimStringPtr trims *p and collapses it to nil when the result is blank -
// {"zone_id":""} and {"zone_id":null} both mean "no zone" to this API, the
// same way an empty-string form field means "cleared" rather than "set to
// the empty string" everywhere else equipment location is handled.
func trimStringPtr(p *string) *string {
	if p == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*p)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// validateEquipmentInput checks every field validateSettingsChange-style -
// name required; category/system/status validated against their own valid*
// set (system/status default first, same as CreateEquipment/UpdateEquipment
// themselves do, so a request that omits them never trips this check only
// to have the store apply a DIFFERENT default); profile_id, when given,
// must be a currently loaded engineProfiles() id (a profile is a file on
// disk, not a foreign key the schema can enforce, so this is the only place
// that ever checks it); install_date blank or YYYY-MM-DD; hour_meter_path
// trimmed with no embedded whitespace. It does NOT re-validate the zone_id/
// bin_id pairing - that needs a database lookup (does this bin's own zone
// match?) that only the store can cheaply make inside the same transaction
// as the write (validateEquipmentLocation, inventory_store.go); this
// function passes both straight through and lets CreateEquipment/
// UpdateEquipment's own errZoneNotFound/errBinNotFound/
// errEquipmentLocationMismatch surface through the normal
// writeDocumentError path instead of this function re-deriving the same
// check a second time against data it would have to fetch just to ask.
//
// Aliases are normalised (trimmed, deduped) via normalizeAliases
// (inventory_store.go) rather than re-implemented here - CreateEquipment/
// UpdateEquipment apply the identical function again on write, which is a
// harmless no-op on an already-normalised list, so there is exactly one
// place that decides what "normalised" means.
func validateEquipmentInput(req equipmentRequest) (equipmentItem, *inventoryValidationError) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return equipmentItem{}, &inventoryValidationError{Field: "name", Message: "name is required"}
	}

	if !validEquipmentCategories[req.Category] {
		return equipmentItem{}, &inventoryValidationError{Field: "category", Message: "category must be mechanical or general"}
	}

	system := req.System
	if system == "" {
		system = "other"
	}
	if !validEquipmentSystems[system] {
		return equipmentItem{}, &inventoryValidationError{Field: "system", Message: fmt.Sprintf("unknown system %q", req.System)}
	}

	status := req.Status
	if status == "" {
		status = "deployed"
	}
	if !validEquipmentStatuses[status] {
		return equipmentItem{}, &inventoryValidationError{Field: "status", Message: "status must be deployed or stored"}
	}

	profileID := strings.TrimSpace(req.ProfileID)
	if profileID != "" {
		profiles, _ := engineProfiles()
		found := false
		for _, p := range profiles {
			if p.ID == profileID {
				found = true
				break
			}
		}
		if !found {
			return equipmentItem{}, &inventoryValidationError{Field: "profile_id", Message: fmt.Sprintf("unknown profile %q", profileID)}
		}
	}

	installDate := strings.TrimSpace(req.InstallDate)
	if installDate != "" && !installDatePattern.MatchString(installDate) {
		return equipmentItem{}, &inventoryValidationError{Field: "install_date", Message: "install_date must be blank or YYYY-MM-DD"}
	}

	hourMeterPath := strings.TrimSpace(req.HourMeterPath)
	if strings.ContainsAny(hourMeterPath, " \t\n") {
		return equipmentItem{}, &inventoryValidationError{Field: "hour_meter_path", Message: "hour_meter_path cannot contain spaces"}
	}

	quantity := req.Quantity
	if quantity <= 0 {
		quantity = 1
	}

	return equipmentItem{
		Name:           name,
		Category:       req.Category,
		System:         system,
		Manufacturer:   strings.TrimSpace(req.Manufacturer),
		Model:          strings.TrimSpace(req.Model),
		Serial:         strings.TrimSpace(req.Serial),
		Quantity:       quantity,
		Status:         status,
		ZoneID:         trimStringPtr(req.ZoneID),
		BinID:          trimStringPtr(req.BinID),
		LocationDetail: strings.TrimSpace(req.LocationDetail),
		InstallDate:    installDate,
		HourMeterPath:  hourMeterPath,
		ProfileID:      profileID,
		Aliases:        normalizeAliases(req.Aliases),
		VerifiedAboard: req.VerifiedAboard,
		Notes:          req.Notes,
	}, nil
}

// writeInventoryValidationError answers c with verr in the {field, message}
// shape validateEquipmentInput's own doc comment explains.
func writeInventoryValidationError(c echo.Context, verr *inventoryValidationError) error {
	return c.JSON(http.StatusBadRequest, map[string]string{"field": verr.Field, "message": verr.Message})
}

// ── zones ────────────────────────────────────────────────────────────────

// listZonesHandler is GET /api/inventory/zones: every zone, each with its
// own bins nested (ListZones' own doc comment, inventory_store.go). Returns
// an empty array, never 404, before the operator has created one -
// listManualsHandler's own "you have not started one is a normal state"
// reasoning applies identically here.
func listZonesHandler(c echo.Context) error {
	zones, err := globalDocumentStore.ListZones()
	if err != nil {
		return writeDocumentError(c, err)
	}
	if zones == nil {
		zones = []inventoryZone{}
	}
	return c.JSON(http.StatusOK, map[string]any{"zones": zones})
}

type zoneRequest struct {
	Name string `json:"name"`
}

// createZoneHandler is POST /api/inventory/zones: {name}. 409 on a
// duplicate name (errZoneNameTaken) and 400 on a blank one
// (errZoneNameInvalid), both mapped through documentErrorStatus.
func createZoneHandler(c echo.Context) error {
	limitNoteRequestBody(c)
	var req zoneRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	zone, err := globalDocumentStore.CreateZone(req.Name)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusCreated, map[string]any{"zone": zone})
}

// updateZoneHandler is PUT /api/inventory/zones/:id: {name}, a rename.
func updateZoneHandler(c echo.Context) error {
	limitNoteRequestBody(c)
	var req zoneRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	zone, err := globalDocumentStore.UpdateZone(c.Param("id"), req.Name)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"zone": zone})
}

// deleteZoneHandler is DELETE /api/inventory/zones/:id: 204 on success,
// 409 with {error, in_use} (plan's own shape) when a bin or an equipment
// item still references the zone - errors.As pulls the exact count out of
// inventoryInUseError (inventory_store.go's own doc comment explains why
// that count exists rather than being derived from SQLite's own constraint
// error), and every OTHER error (404 not found, or a real failure) still
// falls through to the ordinary writeDocumentError mapping.
func deleteZoneHandler(c echo.Context) error {
	err := globalDocumentStore.DeleteZone(c.Param("id"))
	if err == nil {
		return c.NoContent(http.StatusNoContent)
	}
	var inUse *inventoryInUseError
	if errors.As(err, &inUse) {
		return c.JSON(http.StatusConflict, map[string]any{"error": inUse.Error(), "in_use": inUse.Count})
	}
	return writeDocumentError(c, err)
}

// ── bins ─────────────────────────────────────────────────────────────────

type binRequest struct {
	ZoneID string `json:"zone_id"`
	Code   string `json:"code"`
	Name   string `json:"name"`
}

// createBinHandler is POST /api/inventory/bins: {zone_id, code, name}.
func createBinHandler(c echo.Context) error {
	limitNoteRequestBody(c)
	var req binRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	bin, err := globalDocumentStore.CreateBin(req.ZoneID, req.Code, req.Name)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusCreated, map[string]any{"bin": bin})
}

// updateBinHandler is PUT /api/inventory/bins/:id: {code, name} - never
// zone_id (UpdateBin's own doc comment: no re-zone action this cycle).
func updateBinHandler(c echo.Context) error {
	limitNoteRequestBody(c)
	var req binRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	bin, err := globalDocumentStore.UpdateBin(c.Param("id"), req.Code, req.Name)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"bin": bin})
}

// deleteBinHandler is DELETE /api/inventory/bins/:id: 204 on success, 409
// with {error, in_use} when an equipment item still references the bin -
// the same errors.As pattern deleteZoneHandler uses.
func deleteBinHandler(c echo.Context) error {
	err := globalDocumentStore.DeleteBin(c.Param("id"))
	if err == nil {
		return c.NoContent(http.StatusNoContent)
	}
	var inUse *inventoryInUseError
	if errors.As(err, &inUse) {
		return c.JSON(http.StatusConflict, map[string]any{"error": inUse.Error(), "in_use": inUse.Count})
	}
	return writeDocumentError(c, err)
}

// ── equipment ────────────────────────────────────────────────────────────

// listEquipmentHandler is GET /api/inventory/equipment?category=&system=
// &status=&zone=&q=: every equipment record matching the given filters,
// each with link_count/zone_name/bin_code already joined in
// (ListEquipment's own doc comment, inventory_store.go).
func listEquipmentHandler(c echo.Context) error {
	filter := equipmentFilter{
		Category: c.QueryParam("category"),
		System:   c.QueryParam("system"),
		Status:   c.QueryParam("status"),
		ZoneID:   c.QueryParam("zone"),
		Query:    c.QueryParam("q"),
	}
	items, err := globalDocumentStore.ListEquipment(filter)
	if err != nil {
		return writeDocumentError(c, err)
	}
	if items == nil {
		items = []equipmentItem{}
	}
	return c.JSON(http.StatusOK, map[string]any{"items": items})
}

// createEquipmentHandler is POST /api/inventory/equipment: validated by
// validateEquipmentInput first (a clean {field, message} 400 for anything
// it catches), then CreateEquipment (a 404/400 for a bad or contradictory
// zone/bin, mapped through writeDocumentError).
func createEquipmentHandler(c echo.Context) error {
	limitNoteRequestBody(c)
	var req equipmentRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}

	item, verr := validateEquipmentInput(req)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}

	created, err := globalDocumentStore.CreateEquipment(item)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusCreated, map[string]any{"item": created})
}

// getEquipmentHandler is GET /api/inventory/equipment/:id: {item,
// documents} - two separate store calls composed here (GetEquipment,
// EquipmentDocuments), matching plan's method list keeping the two apart
// (inventory_store.go's own doc comments on both).
func getEquipmentHandler(c echo.Context) error {
	id := c.Param("id")

	item, err := globalDocumentStore.GetEquipment(id)
	if err != nil {
		return writeDocumentError(c, err)
	}
	documents, err := globalDocumentStore.EquipmentDocuments(id)
	if err != nil {
		return writeDocumentError(c, err)
	}
	if documents == nil {
		documents = []equipmentDocument{}
	}
	return c.JSON(http.StatusOK, map[string]any{"item": item, "documents": documents})
}

// updateEquipmentHandler is PUT /api/inventory/equipment/:id: the same
// validateEquipmentInput pass as create, then UpdateEquipment's whole-record
// replace.
func updateEquipmentHandler(c echo.Context) error {
	limitNoteRequestBody(c)
	var req equipmentRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}

	item, verr := validateEquipmentInput(req)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}

	updated, err := globalDocumentStore.UpdateEquipment(c.Param("id"), item)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"item": updated})
}

// deleteEquipmentHandler is DELETE /api/inventory/equipment/:id: 204 on
// success. Its equipment_documents links need no handler-side cleanup -
// ON DELETE CASCADE already removes them (DeleteEquipment's own doc
// comment, inventory_store.go).
func deleteEquipmentHandler(c echo.Context) error {
	if err := globalDocumentStore.DeleteEquipment(c.Param("id")); err != nil {
		return writeDocumentError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// ── equipment documents ──────────────────────────────────────────────────

type setEquipmentDocumentsRequest struct {
	DocumentIDs []string `json:"document_ids"`
}

// setEquipmentDocumentsHandler is PUT /api/inventory/equipment/:id/documents:
// {document_ids: []}, replacing the equipment's WHOLE linked-document set
// (plan's "replace wholesale"). Every id is checked against
// globalDocumentStore.Get BEFORE SetEquipmentDocuments is ever called, so a
// bad id can be named in a clean 404 ("document %s not found") rather than
// surfacing as SetEquipmentDocuments' own foreign-key failure, which names
// no id at all (SetEquipmentDocuments' own doc comment explains why that
// split - pre-check here, real constraint there - is deliberate rather
// than duplicated logic).
func setEquipmentDocumentsHandler(c echo.Context) error {
	limitNoteRequestBody(c)
	id := c.Param("id")

	var req setEquipmentDocumentsRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}

	for _, docID := range req.DocumentIDs {
		if _, err := globalDocumentStore.Get(docID); err != nil {
			if errors.Is(err, errDocumentNotFound) {
				return c.JSON(http.StatusNotFound, map[string]string{"error": fmt.Sprintf("document %s not found", docID)})
			}
			return writeDocumentError(c, err)
		}
	}

	if err := globalDocumentStore.SetEquipmentDocuments(id, req.DocumentIDs); err != nil {
		return writeDocumentError(c, err)
	}

	documents, err := globalDocumentStore.EquipmentDocuments(id)
	if err != nil {
		return writeDocumentError(c, err)
	}
	if documents == nil {
		documents = []equipmentDocument{}
	}
	return c.JSON(http.StatusOK, map[string]any{"documents": documents})
}
