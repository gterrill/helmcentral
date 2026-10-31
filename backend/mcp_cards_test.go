package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// An outside assistant has no card to show. The production deps carry a card
// sink for Mate's chat, and the MCP service must drop it, or marina_details
// tells the outside model that a card sits under its reply.
func TestMCPCards_MarinaDetailsCarriesNoCardInstruction(t *testing.T) {
	store := newTestMCPStore(t)
	deps := marinaCardDeps(&fakeMarinaProvider{details: coralSeaWire(t)}, vesselStateData{LengthOverallM: 15, DraftM: 1.4})
	svc := newMCPService(store, func() assistantToolDeps { return deps })
	e := echo.New()
	e.Any("/mcp", svc.Handle)
	srv := httptest.NewServer(e)
	t.Cleanup(srv.Close)
	env := &mcpTestEnv{store: store, server: srv, deps: deps}

	_, secret, _ := store.CreateToken("t", []string{"marina_details"})
	res, err := env.connect(t, secret).CallTool(context.Background(), &mcp.CallToolParams{Name: "marina_details", Arguments: map[string]any{"lat": -20.2663, "lon": 148.7102}})
	if err != nil || res.IsError {
		t.Fatalf("marina_details: %v %+v", err, res)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &fields); err != nil {
		t.Fatalf("result: %v (%s)", err, text)
	}
	if _, ok := fields["card"]; ok || strings.Contains(text, "marina card") {
		t.Errorf("an outside assistant was told about a card: %s", text)
	}
}
