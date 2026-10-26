package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
)

// passageDecisionRoutes returns the /api routes for recorded passage
// decisions (ADR 0211).
func passageDecisionRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodPut, "/api/passage-decisions/:id", tierWrite, putPassageDecisionHandler},
	}
}

// passageDecisionBodyMax caps the request body; a real one is under 100 bytes.
const passageDecisionBodyMax = 4 << 10

// State is a pointer so a body with no state key is refused rather than read
// as an operator blank.
type passageDecisionBody struct {
	State   *string `json:"state"`
	RowDate *string `json:"row_date"`
}

// putPassageDecisionHandler records what the operator did with a passage plan.
func putPassageDecisionHandler(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "passage decision not found"})
	}
	var body passageDecisionBody
	dec := json.NewDecoder(http.MaxBytesReader(c.Response(), c.Request().Body, passageDecisionBodyMax))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil || body.State == nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "body must be {state, row_date?}"})
	}
	if _, err := dec.Token(); err != io.EOF {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "body must be a single JSON object"})
	}
	view, err := globalAssistantStore.SetOperatorPassageDecision(id, *body.State, body.RowDate)
	switch {
	case errors.Is(err, errPassageDecisionNotFound):
		return c.JSON(http.StatusNotFound, map[string]string{"error": "passage decision not found"})
	case errors.Is(err, errPassageDecisionInvalid):
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	case err != nil:
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]any{"decision": view})
}
