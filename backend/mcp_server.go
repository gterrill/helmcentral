package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Helmcentral as an MCP server (ADR 0231): Streamable HTTP at /mcp on the
// backend's own port, read-only, for outside assistants such as Claude Code.
//
// It is not an /api route and not behind the user-session tiers. Its one
// credential is a per-client bearer token (mcp_store.go). With no tokens, or a
// revoked or unknown one, every request is refused with 401, and a user
// session never opens it. The server runs stateless: each request is
// authenticated, gets a server built from that token's tool list, and the
// SDK negotiates the protocol revision (2025-11-25 and 2026-07-28) with the
// client. It calls no model and works with Mate off.

type mcpCallKey struct{}

// mcpCall is what one authenticated request carries to the SDK's per-request
// server factory.
type mcpCall struct {
	token mcpTokenRecord
	tools []mcpToolDef
}

type mcpService struct {
	store   *outsideAssistantStore
	deps    func() assistantToolDeps
	now     func() time.Time
	handler *mcp.StreamableHTTPHandler
}

func newMCPService(store *outsideAssistantStore, deps func() assistantToolDeps) *mcpService {
	m := &mcpService{store: store, deps: deps, now: time.Now}
	m.handler = mcp.NewStreamableHTTPHandler(m.serverFor, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
	})
	return m
}

// Handle is the /mcp route.
func (m *mcpService) Handle(c echo.Context) error {
	req := c.Request()
	if origin := req.Header.Get("Origin"); origin != "" && !mcpOriginAllowed(req, origin) {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "this origin may not use the outside assistant endpoint"})
	}
	secret, ok := bearerToken(req)
	if !ok || m.store == nil {
		return mcpUnauthorized(c)
	}
	token, found, err := m.store.Authenticate(secret)
	if err != nil {
		log.Printf("outside assistants: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "the token store could not be read"})
	}
	if !found {
		return mcpUnauthorized(c)
	}
	stored, err := m.store.ToolsFor(token.ID)
	if err != nil {
		log.Printf("outside assistants: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "the token's tool list could not be read"})
	}
	call := &mcpCall{token: token, tools: mcpToolsForList(stored)}
	m.handler.ServeHTTP(c.Response(), req.WithContext(context.WithValue(req.Context(), mcpCallKey{}, call)))
	return nil
}

func mcpUnauthorized(c echo.Context) error {
	c.Response().Header().Set("WWW-Authenticate", `Bearer realm="helmcentral"`)
	return c.JSON(http.StatusUnauthorized, map[string]string{"error": "a valid outside assistant token is required"})
}

func bearerToken(r *http.Request) (string, bool) {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	scheme, value, ok := strings.Cut(h, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	value = strings.TrimSpace(value)
	return value, value != ""
}

// mcpOriginAllowed is the spec's Origin check. A browser sends Origin; a native
// client such as Claude Code does not. The app's own origin and any origin the
// operator listed in CORS_ALLOWED_ORIGINS are allowed, nothing else.
func mcpOriginAllowed(r *http.Request, origin string) bool {
	if origin == corsOwnOrigin(r) {
		return true
	}
	for _, o := range corsExplicitAllowedOrigins() {
		if origin == o {
			return true
		}
	}
	return false
}

// serverFor builds the MCP server one request is served by, from the token's
// tool list. It is the SDK's getServer: returning nil would be a 400.
func (m *mcpService) serverFor(r *http.Request) *mcp.Server {
	call, _ := r.Context().Value(mcpCallKey{}).(*mcpCall)
	if call == nil {
		return nil
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "helmcentral", Title: "Helmcentral", Version: mcpServerVersion()},
		&mcp.ServerOptions{Instructions: mcpInstructions})
	enabled := map[string]bool{}
	for _, def := range call.tools {
		enabled[def.Name] = true
	}
	on := func(name string) bool { return enabled[name] }
	srv.AddReceivingMiddleware(m.logActivity(call.token.Name, on))

	hasContext := false
	for _, def := range call.tools {
		def := def
		if def.Name == mcpBoatContextTool {
			hasContext = true
		}
		srv.AddTool(&mcp.Tool{
			Name:        def.Name,
			Title:       def.Plain,
			Description: def.Description,
			InputSchema: def.Schema,
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return m.runTool(mcpEnabledCtx(ctx, on), def, req.Params.Arguments), nil
		})
	}
	if hasContext {
		srv.AddResource(&mcp.Resource{
			URI:         mcpContextResourceURI,
			Name:        "boat-context",
			Title:       "Boat context",
			Description: "How this boat is set up: equipment, roles, units, particulars, alarm rules, tank levels and time zone. Generated on every read.",
			MIMEType:    "text/markdown",
		}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			md, err := buildBoatContext(m.toolDeps(mcpBoatContextTool), on)
			if err != nil {
				return nil, err
			}
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: mcpContextResourceURI, MIMEType: "text/markdown", Text: md}}}, nil
		})
	}
	return srv
}

func (m *mcpService) runTool(ctx context.Context, def mcpToolDef, args json.RawMessage) *mcp.CallToolResult {
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	out, err := def.Run(ctx, m.toolDeps(def.Name), args)
	if err != nil {
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: out}}}
}

// toolDeps are the production readers for one call. Only the maintenance
// tool also gets the boat's own date: it works out status against a local date,
// and an outside assistant has no browser to supply one. Nothing else pays for
// the zone lookup. With no position fix ever seen the zone is a guess, so no
// date is given and the tool says so.
func (m *mcpService) toolDeps(tool string) assistantToolDeps {
	d := m.deps()
	if tool != "list_maintenance" {
		return d
	}
	now := m.now()
	if d.now != nil {
		now = d.now()
	}
	if bt := d.boatTime(); !bt.NoFix && bt.Err == nil && bt.Loc != nil {
		y, mo, day := now.In(bt.Loc).Date()
		d.today = time.Date(y, mo, day, 0, 0, 0, 0, time.UTC)
	}
	return d
}

// logActivity records every request after it ends: the client, what it asked
// for, the outcome and how long it took.
func (m *mcpService) logActivity(client string, on func(string) bool) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if strings.HasPrefix(method, "notifications/") || method == "ping" {
				return next(ctx, method, req)
			}
			start := m.now()
			res, err := next(ctx, method, req)
			kind, name, offered := "request", method, true
			switch p := req.(type) {
			case *mcp.CallToolRequest:
				kind, name = "tool", p.Params.Name
				offered = mcpOffered(name) && on(name)
			case *mcp.ReadResourceRequest:
				kind, name = "resource", p.Params.URI
				offered = name == mcpContextResourceURI && on(mcpBoatContextTool)
			}
			m.store.QueueActivity(mcpActivity{At: start, Client: client, Kind: kind, Name: name, Outcome: mcpOutcome(offered, res, err), DurationMS: m.now().Sub(start).Milliseconds()})
			return res, err
		}
	}
}

// mcpOutcome is "ok" or the class of the failure. It is refused only when the
// tool or resource is not on offer to this token, which the caller decides from
// the policy and the token's list. Anything else that fails is an error (a bad
// request included), and a tool that ran and said it could not answer is a
// tool_error.
func mcpOutcome(offered bool, res mcp.Result, err error) string {
	if err != nil {
		if !offered {
			return "refused"
		}
		return "error"
	}
	if r, ok := res.(*mcp.CallToolResult); ok && r != nil && r.IsError {
		return "tool_error"
	}
	return "ok"
}

func mcpServerVersion() string {
	if v := strings.TrimSpace(buildVersion); v != "" {
		return v
	}
	return "dev"
}
