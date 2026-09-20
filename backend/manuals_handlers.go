package main

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// This file serves the manuals feature's HTTP API (plan "Notes and the
// Boat's Manual" §4): GET/POST /api/manuals, DELETE /api/manuals/:id, GET
// /api/manuals/:id/tree and POST /api/manuals/:id/reorder. A manual is a
// document_folders row with role='manual' (manuals_store.go), so this
// reuses writeDocumentError and the same store wholesale -
// documents_handlers.go and notes_handlers.go are the models to read
// alongside this file.
//
// Promotion - "file this note into a manual at position 3" - deliberately
// gets NO endpoint here. It is the existing PATCH /api/notes/:id
// {folder_id, sort_index} (notes_handlers.go's patchNoteHandler), which
// already applies a folder move and metadata in one transaction through
// PatchDocument (documents_store.go). A dedicated route here would be a
// second path doing the same write, for nothing.

// ── GET /api/manuals ─────────────────────────────────────────────────────

// listManualsHandler is GET /api/manuals: every folder currently flagged
// role='manual', alphabetically, each with its section (document) count and
// updated_at. Returns an empty array, never 404, when the operator hasn't
// started one yet - "you have not started one" is a normal state (plan
// §4), the same reasoning documentTagsHandler's own empty-list handling
// already follows.
func listManualsHandler(c echo.Context) error {
	manuals, err := globalDocumentStore.ListManuals()
	if err != nil {
		return writeDocumentError(c, err)
	}
	if manuals == nil {
		manuals = []manualSummary{}
	}
	return c.JSON(http.StatusOK, map[string]any{"manuals": manuals})
}

// ── POST /api/manuals ────────────────────────────────────────────────────

type createManualRequest struct {
	Name     string  `json:"name"`
	FolderID *string `json:"folder_id"`
}

// createManualHandler is POST /api/manuals (plan §4): {name} creates a
// brand new top-level manual, {folder_id} flags an existing top-level
// folder instead. folder_id takes precedence when both are somehow
// present - a caller that means to flag an existing folder has no reason
// to also invent a name, and CreateManual would only ignore it anyway.
// 409 on a non-top-level folder (errManualNotTopLevel) or a duplicate name
// (errFolderNameTaken) - both mapped by documentErrorStatus.
func createManualHandler(c echo.Context) error {
	// Same bound as the note write paths: refuse an oversized request
	// while it is still being read, not after it is in memory.
	limitNoteRequestBody(c)
	var req createManualRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}

	var (
		manual manualSummary
		err    error
	)
	switch {
	case req.FolderID != nil && strings.TrimSpace(*req.FolderID) != "":
		manual, err = globalDocumentStore.FlagManual(strings.TrimSpace(*req.FolderID))
	case strings.TrimSpace(req.Name) != "":
		manual, err = globalDocumentStore.CreateManual(strings.TrimSpace(req.Name))
	default:
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "name or folder_id is required"})
	}
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusCreated, manual)
}

// ── DELETE /api/manuals/:id ──────────────────────────────────────────────

// clearManualHandler is DELETE /api/manuals/:id: clears the role flag only
// - demotes id to a plain collection, moving and deleting nothing beneath
// it (ClearManual's own doc comment, manuals_store.go). 409 if id exists
// but isn't currently a manual (errNotAManual), 404 if it doesn't exist at
// all.
func clearManualHandler(c echo.Context) error {
	if err := globalDocumentStore.ClearManual(c.Param("id")); err != nil {
		return writeDocumentError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// ── GET /api/manuals/:id/tree ────────────────────────────────────────────

// manualTreeHandler is GET /api/manuals/:id/tree: that manual's whole
// subtree, folders and documents interleaved and ordered sort_index then
// name at every level (ManualTree's own doc comment, manuals_store.go).
func manualTreeHandler(c echo.Context) error {
	tree, err := globalDocumentStore.ManualTree(c.Param("id"))
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, tree)
}

// ── POST /api/manuals/:id/reorder ────────────────────────────────────────

type reorderManualRequest struct {
	ParentID string              `json:"parent_id"`
	Items    []manualReorderItem `json:"items"`
}

// reorderManualHandler is POST /api/manuals/:id/reorder: the WHOLE sibling
// list under parent_id, with each item's new sort_index, applied in one
// transaction (ReorderManualChildren's own doc comment, manuals_store.go).
// kind and id are validated here, before the store is ever called, so a
// malformed item never reaches ReorderManualChildren's own defensive
// default case; an id ReorderManualChildren doesn't recognise as an actual
// child of parent_id still rejects the whole call, mapped through
// writeDocumentError the same way every other bulk operation in this
// codebase is. Returns the manual's freshly ordered tree, the same shape
// GET .../tree returns, so the caller never has to make a second request
// just to see what it committed.
func reorderManualHandler(c echo.Context) error {
	// Same bound as the note write paths: refuse an oversized request
	// while it is still being read, not after it is in memory.
	limitNoteRequestBody(c)
	id := c.Param("id")

	var req reorderManualRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if strings.TrimSpace(req.ParentID) == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "parent_id is required"})
	}
	for _, item := range req.Items {
		if item.Kind != "folder" && item.Kind != "document" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "each item's kind must be \"folder\" or \"document\""})
		}
		if strings.TrimSpace(item.ID) == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "each item's id is required"})
		}
	}

	if err := globalDocumentStore.ReorderManualChildren(id, req.ParentID, req.Items); err != nil {
		return writeDocumentError(c, err)
	}

	tree, err := globalDocumentStore.ManualTree(id)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, tree)
}
