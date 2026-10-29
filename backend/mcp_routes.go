package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// The Settings side of outside assistants (ADR 0231): create, list, switch and
// revoke tokens, and read the activity log. All admin tier. The /mcp endpoint
// itself is not here; it has its own credential (mcp_server.go).

func outsideAssistantRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodGet, "/api/outside-assistants", tierAdmin, listOutsideAssistantsHandler},
		{http.MethodGet, "/api/outside-assistants/activity", tierAdmin, listOutsideAssistantActivityHandler},
		{http.MethodPost, "/api/outside-assistants/tokens", tierAdmin, createOutsideAssistantTokenHandler},
		{http.MethodPut, "/api/outside-assistants/tokens/:id/tools", tierAdmin, setOutsideAssistantToolsHandler},
		{http.MethodDelete, "/api/outside-assistants/tokens/:id", tierAdmin, revokeOutsideAssistantTokenHandler},
	}
}

type outsideAssistantTokenView struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	// Tools is the token's tool list, narrowed to what can be offered at all.
	Tools []string `json:"tools"`
}

func outsideAssistantStoreOrError(c echo.Context) (*outsideAssistantStore, error) {
	if globalOutsideAssistantStore == nil {
		return nil, c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "the outside assistant store is not open"})
	}
	return globalOutsideAssistantStore, nil
}

func tokenView(s *outsideAssistantStore, rec mcpTokenRecord) (outsideAssistantTokenView, error) {
	stored, err := s.ToolsFor(rec.ID)
	if err != nil {
		return outsideAssistantTokenView{}, err
	}
	tools := []string{}
	for _, d := range mcpToolsForList(stored) {
		tools = append(tools, d.Name)
	}
	return outsideAssistantTokenView{ID: rec.ID, Name: rec.Name, CreatedAt: rec.CreatedAt, LastUsedAt: rec.LastUsedAt, Tools: tools}, nil
}

// GET /api/outside-assistants
func listOutsideAssistantsHandler(c echo.Context) error {
	s, done := outsideAssistantStoreOrError(c)
	if s == nil {
		return done
	}
	recs, err := s.ListTokens()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	tokens := make([]outsideAssistantTokenView, 0, len(recs))
	for _, r := range recs {
		v, err := tokenView(s, r)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		tokens = append(tokens, v)
	}
	type toolView struct {
		Name      string `json:"name"`
		PlainName string `json:"plain_name"`
		DefaultOn bool   `json:"default_on"`
	}
	tools := []toolView{}
	for _, d := range mcpToolDefs() {
		tools = append(tools, toolView{Name: d.Name, PlainName: d.Plain, DefaultOn: d.DefaultOn})
	}
	return c.JSON(http.StatusOK, map[string]any{"tokens": tokens, "tools": tools})
}

// GET /api/outside-assistants/activity
func listOutsideAssistantActivityHandler(c echo.Context) error {
	s, done := outsideAssistantStoreOrError(c)
	if s == nil {
		return done
	}
	limit := 50
	if raw := c.QueryParam("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 500 {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "limit must be between 1 and 500"})
		}
		limit = n
	}
	act, err := s.ListActivity(limit)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]any{"activity": act})
}

// POST /api/outside-assistants/tokens
// The response is the only time the secret is ever given.
func createOutsideAssistantTokenHandler(c echo.Context) error {
	s, done := outsideAssistantStoreOrError(c)
	if s == nil {
		return done
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
	}
	if strings.TrimSpace(req.Name) == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "give the token a name, such as the app or computer it is for"})
	}
	rec, secret, err := s.CreateToken(req.Name, mcpDefaultTools())
	if errors.Is(err, errMCPTokenInvalid) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": strings.TrimPrefix(err.Error(), errMCPTokenInvalid.Error()+": ")})
	}
	if err != nil {
		log.Printf("outside assistants: create token: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "the token could not be created"})
	}
	view, err := tokenView(s, rec)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, map[string]any{"token": view, "secret": secret})
}

// PUT /api/outside-assistants/tokens/:id/tools
func setOutsideAssistantToolsHandler(c echo.Context) error {
	s, done := outsideAssistantStoreOrError(c)
	if s == nil {
		return done
	}
	var req struct {
		Tools []string `json:"tools"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
	}
	for _, name := range req.Tools {
		if !mcpOffered(name) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("%s cannot be offered to an outside assistant", name)})
		}
	}
	id := c.Param("id")
	if err := s.SetTools(id, req.Tools); err != nil {
		if errors.Is(err, errMCPTokenNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	rec, err := s.GetToken(id)
	if err != nil {
		if errors.Is(err, errMCPTokenNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	view, err := tokenView(s, rec)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]any{"token": view})
}

// DELETE /api/outside-assistants/tokens/:id
func revokeOutsideAssistantTokenHandler(c echo.Context) error {
	s, done := outsideAssistantStoreOrError(c)
	if s == nil {
		return done
	}
	if err := s.RevokeToken(c.Param("id")); err != nil {
		if errors.Is(err, errMCPTokenNotFound) {
			return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}
