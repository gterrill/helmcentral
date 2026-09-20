package main

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// This file serves the checklist-runs feature's HTTP API (plan "Notes and
// the Boat's Manual" §3/§4, ADR 0118): POST/GET .../checklist-runs(/active)
// under a note id, and PATCH/POST/DELETE under a run id, backed wholesale
// by checklist_runs_store.go. documents_handlers.go and notes_handlers.go
// are the models to read alongside this file - the same writeDocumentError
// mapping, the same "call the store, translate its sentinel" shape.

// ── POST /api/notes/:id/checklist-runs ───────────────────────────────────

// createChecklistRunHandler is POST /api/notes/:id/checklist-runs (plan
// §3/§4): starts a fresh run (201, resumed:false) or, when the note
// already has an active one, hands that same run straight back (200,
// resumed:true) rather than erroring or starting a second one - modelled
// on uploadDocumentHandler's own duplicate:true shape (documents_handlers.go)
// so the frontend has exactly one pattern for "you already have this,
// here it is" across both features.
func createChecklistRunHandler(c echo.Context) error {
	run, resumed, err := globalDocumentStore.StartOrResumeChecklistRun(c.Param("id"))
	if err != nil {
		return writeDocumentError(c, err)
	}
	status := http.StatusCreated
	if resumed {
		status = http.StatusOK
	}
	return c.JSON(status, map[string]any{"run": run, "resumed": resumed})
}

// ── GET /api/notes/:id/checklist-runs/active ─────────────────────────────

// getActiveChecklistRunHandler is GET /api/notes/:id/checklist-runs/active
// (plan §4): the note's currently open run, or {"run": null} when there
// isn't one - not a 404, the same "you haven't started one yet" reasoning
// listManualsHandler's own empty-array response gives.
func getActiveChecklistRunHandler(c echo.Context) error {
	run, found, err := globalDocumentStore.ActiveChecklistRun(c.Param("id"))
	if err != nil {
		return writeDocumentError(c, err)
	}
	if !found {
		return c.JSON(http.StatusOK, map[string]any{"run": nil})
	}
	return c.JSON(http.StatusOK, map[string]any{"run": run})
}

// ── PATCH /api/checklist-runs/:runId/items ───────────────────────────────

type checklistTickRequest struct {
	ItemKey    string `json:"item_key"`
	Occurrence int    `json:"occurrence"`
	Checked    bool   `json:"checked"`
}

// tickChecklistItemHandler is PATCH /api/checklist-runs/:runId/items (plan
// §3/§4): ticks or unticks ONE item, keyed by (item_key, occurrence), and
// returns the WHOLE run - never a per-item delta - so the client re-renders
// from the response rather than merging state locally. Plan §3's own
// reasoning: this call may be the last thing that happens before the
// operator walks away from the screen.
func tickChecklistItemHandler(c echo.Context) error {
	var req checklistTickRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if strings.TrimSpace(req.ItemKey) == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "item_key is required"})
	}
	if req.Occurrence < 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "occurrence must not be negative"})
	}

	run, err := globalDocumentStore.TickChecklistItem(c.Param("runId"), req.ItemKey, req.Occurrence, req.Checked)
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"run": run})
}

// ── POST /api/checklist-runs/:runId/complete ─────────────────────────────

// completeChecklistRunHandler is POST /api/checklist-runs/:runId/complete
// (plan §3/§4): stamps completed_at and returns the final run.
func completeChecklistRunHandler(c echo.Context) error {
	run, err := globalDocumentStore.CompleteChecklistRun(c.Param("runId"))
	if err != nil {
		return writeDocumentError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"run": run})
}

// ── DELETE /api/checklist-runs/:runId ─────────────────────────────────────

// abandonChecklistRunHandler is DELETE /api/checklist-runs/:runId (plan
// §3/§4): stamps abandoned_at and NEVER deletes the row
// (AbandonChecklistRun's own doc comment, checklist_runs_store.go) - the
// same "DELETE clears a flag, moves and deletes nothing" shape
// clearManualHandler already uses for a manual's role flag
// (manuals_handlers.go).
func abandonChecklistRunHandler(c echo.Context) error {
	if err := globalDocumentStore.AbandonChecklistRun(c.Param("runId")); err != nil {
		return writeDocumentError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}
