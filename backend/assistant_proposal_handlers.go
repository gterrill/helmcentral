package main

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
)

// POST /api/assistant/proposals/:id/apply?today=YYYY-MM-DD (ADR 0146)
//
// The operator's Apply tap on a maintenance proposal card. today is required
// for the same reason every maintenance write requires it: an hours-only
// baseline is converted at the operator's own date, not the server's.
// Answers {proposal} with the stored result; a proposal already applied
// answers the same and writes nothing; a stale or dismissed one is a 409 and
// nothing is written.
func applyAssistantProposalHandler(c echo.Context) error {
	today, verr := requireTodayParam(c)
	if verr != nil {
		return writeInventoryValidationError(c, verr)
	}
	p, err := globalAssistantStore.ApplyProposal(c.Param("id"), today)
	if err != nil {
		return writeAssistantProposalError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"proposal": p})
}

// POST /api/assistant/proposals/:id/dismiss
func dismissAssistantProposalHandler(c echo.Context) error {
	p, err := globalAssistantStore.DismissProposal(c.Param("id"))
	if err != nil {
		return writeAssistantProposalError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"proposal": p})
}

// writeAssistantProposalError answers with the status an apply or dismiss
// failure deserves: the proposal's own 404/409, a maintenance command's
// validation or refusal, or the maintenance sentinels' usual mapping.
func writeAssistantProposalError(c echo.Context, err error) error {
	if status, ok := assistantProposalErrorStatus(err); ok {
		return c.JSON(status, map[string]string{"error": err.Error()})
	}
	var verr *inventoryValidationError
	if errors.As(err, &verr) {
		return c.JSON(http.StatusBadRequest, map[string]string{"field": verr.Field, "message": err.Error(), "error": err.Error()})
	}
	var cerr *maintenanceCommandError
	if errors.As(err, &cerr) {
		return c.JSON(cerr.Status, map[string]string{"error": err.Error()})
	}
	return writeDocumentError(c, err)
}
