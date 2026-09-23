package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
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
// to have the store apply a DIFFERENT default); profile_id, when given AND
// different from existingProfileID, must be a currently loaded
// engineProfiles() id (a profile is a file on disk, not a foreign key the
// schema can enforce, so this is the only place that ever checks it).
// existingProfileID is the stored record's own profile_id on an update ("" on
// a create, where nothing is stored yet): a profile can be deleted out from
// under a record that already links to it, and re-checking profile_id
// against the live profile list on every PUT would then 400 every future
// edit of that record for a field the request never touched. Leaving
// profile_id exactly as it already was is therefore always accepted without
// a fresh lookup; only a NEW or CHANGED value is re-validated. install_date
// blank or YYYY-MM-DD; hour_meter_path trimmed with no embedded whitespace.
// It does NOT re-validate the zone_id/
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
func validateEquipmentInput(req equipmentRequest, existingProfileID string) (equipmentItem, *inventoryValidationError) {
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
	if profileID != "" && profileID != existingProfileID {
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
// &status=&zone=&bin=&q=: every equipment record matching the given
// filters, each with link_count/zone_name/bin_code/photo_ids already joined
// in (ListEquipment's own doc comment, inventory_store.go). bin (ADR 0127)
// is the bin page's own filter - `useEquipment({ bin: bin.id })` on the
// frontend.
func listEquipmentHandler(c echo.Context) error {
	filter := equipmentFilter{
		Category: c.QueryParam("category"),
		System:   c.QueryParam("system"),
		Status:   c.QueryParam("status"),
		ZoneID:   c.QueryParam("zone"),
		BinID:    c.QueryParam("bin"),
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

	item, verr := validateEquipmentInput(req, "")
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
// validateEquipmentInput pass as create - except profile_id, where it also
// loads the stored record first so a value the request left unchanged is
// never re-checked against engineProfiles() (validateEquipmentInput's own
// doc comment explains why) - then UpdateEquipment's whole-record replace.
func updateEquipmentHandler(c echo.Context) error {
	limitNoteRequestBody(c)
	var req equipmentRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}

	existing, err := globalDocumentStore.GetEquipment(c.Param("id"))
	if err != nil {
		return writeDocumentError(c, err)
	}

	item, verr := validateEquipmentInput(req, existing.ProfileID)
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

// ── equipment photos ─────────────────────────────────────────────────────
// ADR 0127: a photo is an ordinary uploaded document, tagged 'photo' and
// linked through equipment_documents (inventory_store.go's own "equipment
// photos" section). respondWithUpdatedEquipment is shared by all three
// handlers below - every one of them answers with the item as it stands
// right after the write, so the frontend never has to reload separately to
// see its own change reflected.

// respondWithUpdatedEquipment re-reads id and answers c with {item} at
// status - the common tail of every photo write below (and, unlike a write
// that merely echoes back the request body, this is a real re-read, so
// photo_ids always reflects exactly what the write just did).
func respondWithUpdatedEquipment(c echo.Context, id string, status int) error {
	item, err := globalDocumentStore.GetEquipment(id)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(status, map[string]any{"item": item})
}

// uploadEquipmentPhotoHandler is POST /api/inventory/equipment/:id/photos
// (multipart, one "file" part): stores the file through the same path
// uploadDocumentHandler uses (documents_handlers.go) - same
// documentMaxUploadBytes cap, same content-sniffed MIME detection, same
// sha256 dedupe - tags it 'photo', and links it at the end of the item's
// photo order. Existence is checked FIRST, before the multipart body is
// ever read, so an upload to an unknown id never writes a file at all
// (ADR 0127's own test list: "An upload to an unknown id stores no
// document").
func uploadEquipmentPhotoHandler(c echo.Context) error {
	id := c.Param("id")
	if _, err := globalDocumentStore.GetEquipment(id); err != nil {
		return writeDocumentError(c, err)
	}

	req := c.Request()
	req.Body = http.MaxBytesReader(c.Response(), req.Body, documentMaxUploadBytes)

	reader, err := req.MultipartReader()
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "expected multipart/form-data"})
	}

	dir := documentsDirPath()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to prepare document storage"})
	}

	var (
		tmpPath  string
		filename string
		size     int64
		haveFile bool
	)
	removeTemp := func() {
		if tmpPath != "" {
			os.Remove(tmpPath)
		}
	}

	hasher := sha256.New()
	head := &headCapture{}

	for {
		part, partErr := reader.NextPart()
		if partErr == io.EOF {
			break
		}
		if partErr != nil {
			removeTemp()
			return documentUploadReadError(c, partErr)
		}
		if part.FormName() != "file" {
			part.Close()
			continue
		}
		if tmpPath != "" {
			part.Close()
			removeTemp()
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "only one file per upload"})
		}
		filename = part.FileName()
		tmpFile, createErr := os.CreateTemp(dir, "upload-*.tmp")
		if createErr != nil {
			part.Close()
			removeTemp()
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create temp file"})
		}
		tmpPath = tmpFile.Name()
		mw := io.MultiWriter(tmpFile, hasher, head)
		n, copyErr := io.Copy(mw, part)
		closeErr := tmpFile.Close()
		part.Close()
		if copyErr != nil {
			removeTemp()
			return documentUploadReadError(c, copyErr)
		}
		if closeErr != nil {
			removeTemp()
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to save upload"})
		}
		size = n
		haveFile = n > 0
	}

	if !haveFile {
		removeTemp()
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "file is required"})
	}

	// ADR 0127: "Accept JPEG or PNG only. Reject HEIC with the enrich
	// stage's existing message." - documentHEICRejectionMessage
	// (documents_enrich.go) is that same shared wording, so an operator
	// sees ONE explanation for "why can't Helmcentral use this" wherever
	// they meet it.
	mimeType := detectDocumentMIME(head.buf, filename)
	switch mimeType {
	case "image/jpeg", "image/png":
		// accepted
	case "image/heic":
		removeTemp()
		return c.JSON(http.StatusBadRequest, map[string]string{"error": documentHEICRejectionMessage})
	default:
		removeTemp()
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "only JPEG or PNG photos are accepted"})
	}

	// Readiness is checked before the bytes ever move to their final,
	// content-addressed path - uploadDocumentHandler's own reasoning
	// (documents_handlers.go): a failure here removes only this attempt's
	// own temp file, never an existing document's.
	enrich, _, err := documentEnrichFlag("inventory: upload photo")
	if err != nil {
		removeTemp()
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	sha := hex.EncodeToString(hasher.Sum(nil))

	unlockSHA := lockDocumentSHA(sha)
	defer unlockSHA()

	if existing, ok, err := globalDocumentStore.GetBySHA(sha); err != nil {
		removeTemp()
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	} else if ok {
		// Identical bytes already in the library (Insert's own sha256
		// dedupe, uploadDocumentHandler's own comment) - the file on disk
		// belongs to that existing row, so only this attempt's own temp
		// file is removed. EnsurePhotoTag covers the case where that row
		// wasn't already tagged 'photo' (e.g. it was linked as an ordinary
		// document elsewhere first).
		removeTemp()
		if err := globalDocumentStore.EnsurePhotoTag(existing.ID); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		if err := globalDocumentStore.AddEquipmentPhoto(id, existing.ID); err != nil {
			return writeDocumentError(c, err)
		}
		return respondWithUpdatedEquipment(c, id, http.StatusOK)
	}

	finalPath := filepath.Join(dir, sha)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		removeTemp()
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to store upload"})
	}
	tmpPath = "" // renamed into place; removeTemp must not touch it anymore

	inserted, err := globalDocumentStore.Insert(document{
		SHA256:       sha,
		Filename:     filename,
		MIME:         mimeType,
		SizeBytes:    size,
		Enrich:       enrich,
		OperatorTags: []string{"photo"},
	})
	if errors.Is(err, errDocumentDuplicate) {
		// Insert's own sha256 race: another request created the row between
		// our GetBySHA check and this Insert call. Same shape as the
		// "already existed" branch above - the file belongs to that row,
		// not to this attempt - and the SAME EnsurePhotoTag call is needed
		// for the same reason: the row that won the race might not have
		// been tagged 'photo' (e.g. an ordinary document upload racing this
		// one for identical bytes), and without it this upload would link
		// successfully but never actually show up in photo_ids.
		if err := globalDocumentStore.EnsurePhotoTag(inserted.ID); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		if err := globalDocumentStore.AddEquipmentPhoto(id, inserted.ID); err != nil {
			return writeDocumentError(c, err)
		}
		return respondWithUpdatedEquipment(c, id, http.StatusOK)
	}
	if err != nil {
		// Safe to remove: still holding sha's lock, and GetBySHA just
		// confirmed no row owned this hash, so finalPath can only be this
		// attempt's own file.
		os.Remove(finalPath)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	if err := globalDocumentStore.AddEquipmentPhoto(id, inserted.ID); err != nil {
		// The document now exists but isn't linked - surfaced as-is
		// (AGENTS.md fail-fast policy) rather than silently leaving an
		// orphaned, unlinked photo document with no cleanup.
		return writeDocumentError(c, err)
	}

	wakeDocumentIndexer()
	return respondWithUpdatedEquipment(c, id, http.StatusCreated)
}

type setEquipmentPhotoOrderRequest struct {
	DocumentIDs []string `json:"document_ids"`
}

// setEquipmentPhotoOrderHandler is PUT /api/inventory/equipment/:id/photos:
// {document_ids: [...]}, rewriting sort_index to match the given order
// exactly - ADR 0127: "Make cover" is this same call with the chosen id
// moved to the front. document_ids must name EXACTLY the item's current
// photo set (errEquipmentPhotoSetMismatch -> 400, mapped through
// writeDocumentError/documentErrorStatus) - see SetEquipmentPhotoOrder's
// own doc comment (inventory_store.go) for why a partial reorder isn't
// accepted the way SetEquipmentDocuments' whole-set replace is for
// ordinary links.
func setEquipmentPhotoOrderHandler(c echo.Context) error {
	limitNoteRequestBody(c)
	id := c.Param("id")

	var req setEquipmentPhotoOrderRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}

	if err := globalDocumentStore.SetEquipmentPhotoOrder(id, req.DocumentIDs); err != nil {
		return writeDocumentError(c, err)
	}
	return respondWithUpdatedEquipment(c, id, http.StatusOK)
}

// deleteEquipmentPhotoHandler is DELETE
// /api/inventory/equipment/:id/photos/:documentId: detaches the photo and,
// ONLY when no other item still links the same document (uploads are
// deduplicated by sha256, so byte-identical photos on two items share one
// document row - RemoveEquipmentPhoto's own doc comment), deletes the
// document and its file too (ADR 0127: "a photo has no life outside its
// item"). The sha-lock-then-remove-file sequence otherwise matches
// deleteDocumentHandler (documents_handlers.go), so an upload racing the
// exact same content can never interleave into a row with no file or a file
// no row points at.
func deleteEquipmentPhotoHandler(c echo.Context) error {
	id := c.Param("id")
	documentID := c.Param("documentId")

	doc, err := globalDocumentStore.Get(documentID)
	if err != nil {
		return writeDocumentError(c, err)
	}

	unlockSHA := lockDocumentSHA(doc.SHA256)
	defer unlockSHA()

	sha, documentDeleted, err := globalDocumentStore.RemoveEquipmentPhoto(id, documentID)
	if err != nil {
		return writeDocumentError(c, err)
	}

	if documentDeleted {
		path := filepath.Join(documentsDirPath(), sha)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Printf("inventory: delete photo: failed to remove file %s: %v", path, err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to remove document file"})
		}
	}
	return respondWithUpdatedEquipment(c, id, http.StatusOK)
}
