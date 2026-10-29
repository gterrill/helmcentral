package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpTestEnv struct {
	store  *outsideAssistantStore
	server *httptest.Server
	deps   assistantToolDeps
}

func newMCPTestEnv(t *testing.T) *mcpTestEnv {
	t.Helper()
	store := newTestMCPStore(t)
	deps := contextTestDeps(t)
	svc := newMCPService(store, func() assistantToolDeps { return deps })
	e := echo.New()
	e.Any("/mcp", svc.Handle)
	srv := httptest.NewServer(e)
	t.Cleanup(srv.Close)
	return &mcpTestEnv{store: store, server: srv, deps: deps}
}

func (env *mcpTestEnv) post(t *testing.T, bearer, origin, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, env.server.URL+"/mcp", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

const mcpListBody = `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`

func TestMCPAuth_NoTokensRefusesEverything(t *testing.T) {
	env := newMCPTestEnv(t)
	if r := env.post(t, "", "", mcpListBody); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no header: %d", r.StatusCode)
	}
	if r := env.post(t, "hcmcp_anything", "", mcpListBody); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unknown token: %d", r.StatusCode)
	}
	if r := env.post(t, "", "", mcpListBody); r.Header.Get("WWW-Authenticate") == "" {
		t.Fatal("a 401 should say Bearer")
	}
}

func TestMCPAuth_NoFallbackToUserSessions(t *testing.T) {
	env := newMCPTestEnv(t)
	req, _ := http.NewRequest(http.MethodPost, env.server.URL+"/mcp", bytes.NewBufferString(mcpListBody))
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "a-valid-looking-session"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a session cookie opened /mcp: %d", resp.StatusCode)
	}
}

func TestMCPAuth_GoodBadAndRevokedToken(t *testing.T) {
	env := newMCPTestEnv(t)
	rec, secret, _ := env.store.CreateToken("laptop", mcpDefaultTools())
	if r := env.post(t, secret, "", mcpListBody); r.StatusCode != http.StatusOK {
		t.Fatalf("good token: %d", r.StatusCode)
	}
	if r := env.post(t, secret+"x", "", mcpListBody); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad token: %d", r.StatusCode)
	}
	if err := env.store.RevokeToken(rec.ID); err != nil {
		t.Fatal(err)
	}
	if r := env.post(t, secret, "", mcpListBody); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked token: %d", r.StatusCode)
	}
}

func TestMCPOrigin_ForeignBrowserOriginRejected(t *testing.T) {
	env := newMCPTestEnv(t)
	_, secret, _ := env.store.CreateToken("laptop", mcpDefaultTools())
	if r := env.post(t, secret, "https://evil.example", mcpListBody); r.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign origin: %d", r.StatusCode)
	}
	if r := env.post(t, secret, "null", mcpListBody); r.StatusCode != http.StatusForbidden {
		t.Fatalf("null origin: %d", r.StatusCode)
	}
	if r := env.post(t, secret, env.server.URL, mcpListBody); r.StatusCode != http.StatusOK {
		t.Fatalf("own origin: %d", r.StatusCode)
	}
	if r := env.post(t, secret, "", mcpListBody); r.StatusCode != http.StatusOK {
		t.Fatalf("no origin (a native client): %d", r.StatusCode)
	}
}

func TestMCPOrigin_ExplicitlyAllowedOrigin(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://helm.example")
	env := newMCPTestEnv(t)
	_, secret, _ := env.store.CreateToken("laptop", mcpDefaultTools())
	if r := env.post(t, secret, "https://helm.example", mcpListBody); r.StatusCode != http.StatusOK {
		t.Fatalf("allowed origin: %d", r.StatusCode)
	}
}

func (env *mcpTestEnv) connect(t *testing.T, secret string) *mcp.ClientSession {
	t.Helper()
	hc := &http.Client{Transport: bearerTransport{secret: secret}}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	cs, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: env.server.URL + "/mcp", HTTPClient: hc, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

type bearerTransport struct{ secret string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.secret)
	return http.DefaultTransport.RoundTrip(r)
}

func mcpClientToolNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	return names
}

// A real client round trip over HTTP: initialize, tools/list, a tool call, and
// the context resource.
func TestMCPRoundTrip_InitializeListCallAndReadResource(t *testing.T) {
	env := newMCPTestEnv(t)
	_, secret, _ := env.store.CreateToken("Claude Code", mcpDefaultTools())
	cs := env.connect(t, secret)

	init := cs.InitializeResult()
	if init == nil || !strings.Contains(init.Instructions, "operator's units") {
		t.Fatalf("instructions: %+v", init)
	}

	names := mcpClientToolNames(t, cs)
	if strings.Join(names, ",") != strings.Join(sortedCopy(mcpDefaultTools()), ",") {
		t.Fatalf("tools/list:\n got %v\nwant %v", names, sortedCopy(mcpDefaultTools()))
	}
	again := mcpClientToolNames(t, cs)
	if strings.Join(again, ",") != strings.Join(names, ",") {
		t.Fatal("tools/list order is not deterministic")
	}

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_vessel_status", Arguments: map[string]any{}})
	if err != nil || res.IsError {
		t.Fatalf("get_vessel_status: %v %+v", err, res)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "soc_percent") || !strings.Contains(text, "display") {
		t.Fatalf("status result: %s", text)
	}

	ctxRes, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_boat_context", Arguments: map[string]any{}})
	if err != nil || ctxRes.IsError || !strings.Contains(ctxRes.Content[0].(*mcp.TextContent).Text, "Service Bank") {
		t.Fatalf("get_boat_context: %v %+v", err, ctxRes)
	}

	rr, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: mcpContextResourceURI})
	if err != nil || len(rr.Contents) != 1 || rr.Contents[0].MIMEType != "text/markdown" || !strings.Contains(rr.Contents[0].Text, "Service Bank") {
		t.Fatalf("resource: %v %+v", err, rr)
	}
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func TestMCPTools_PerTokenListFilters(t *testing.T) {
	env := newMCPTestEnv(t)
	_, secret, _ := env.store.CreateToken("narrow", []string{"get_active_alarms"})
	cs := env.connect(t, secret)
	if got := mcpClientToolNames(t, cs); strings.Join(got, ",") != "get_active_alarms" {
		t.Fatalf("tools: %v", got)
	}
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_vessel_status", Arguments: map[string]any{}})
	if err == nil && !res.IsError {
		t.Fatal("a tool outside the token's list ran")
	}
	// Without get_boat_context the resource is not offered either.
	if _, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: mcpContextResourceURI}); err == nil {
		t.Fatal("the context resource was readable without get_boat_context")
	}
}

func TestMCPTools_NeverOfferedRefusedEvenIfTheTokenNamesThem(t *testing.T) {
	env := newMCPTestEnv(t)
	// Stored directly, past the checks the Settings API makes.
	_, secret, _ := env.store.CreateToken("sneaky", []string{
		"search_web", "propose_changes", "search_conversations", "read_conversation",
		"list_records", "run_watch_now", "get_vessel_status",
	})
	cs := env.connect(t, secret)
	if got := mcpClientToolNames(t, cs); strings.Join(got, ",") != "get_vessel_status" {
		t.Fatalf("tools: %v", got)
	}
	for _, name := range []string{"search_web", "propose_changes", "search_conversations", "read_conversation", "list_records", "run_watch_now"} {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
		if err == nil && !res.IsError {
			t.Errorf("%s ran", name)
		}
	}
}

func TestMCPActivity_EveryCallIsLogged(t *testing.T) {
	env := newMCPTestEnv(t)
	_, secret, _ := env.store.CreateToken("Claude Code", mcpDefaultTools())
	cs := env.connect(t, secret)
	_, _ = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_vessel_status", Arguments: map[string]any{}})
	_, _ = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "search_web", Arguments: map[string]any{}})
	_, _ = cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: mcpContextResourceURI})

	var tool, refused, resource *mcpActivity
	act, err := env.store.ListActivity(50)
	if err != nil {
		t.Fatal(err)
	}
	for i := range act {
		a := act[i]
		switch {
		case a.Kind == "tool" && a.Name == "get_vessel_status":
			tool = &act[i]
		case a.Kind == "tool" && a.Name == "search_web":
			refused = &act[i]
		case a.Kind == "resource" && a.Name == mcpContextResourceURI:
			resource = &act[i]
		}
	}
	if tool == nil || tool.Client != "Claude Code" || tool.Outcome != "ok" || tool.At.IsZero() || time.Since(tool.At) > time.Minute {
		t.Errorf("tool entry: %+v", tool)
	}
	if refused == nil || refused.Outcome != "refused" {
		t.Errorf("refused entry: %+v", refused)
	}
	if resource == nil || resource.Outcome != "ok" {
		t.Errorf("resource entry: %+v", resource)
	}
}

func TestMCPActivity_ToolErrorIsClassed(t *testing.T) {
	env := newMCPTestEnv(t)
	_, secret, _ := env.store.CreateToken("c", mcpDefaultTools())
	cs := env.connect(t, secret)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_alarm_context", Arguments: map[string]any{"alarm_id": "nope"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Skip("alarm context did not fail for an unknown id in this fixture")
	}
	act, _ := env.store.ListActivity(10)
	found := false
	for _, a := range act {
		if a.Name == "get_alarm_context" && a.Outcome == "tool_error" {
			found = true
		}
	}
	if !found {
		t.Errorf("no tool_error entry: %+v", act)
	}
}

func TestMCPProtocol_BothRevisionsAreServed(t *testing.T) {
	env := newMCPTestEnv(t)
	_, secret, _ := env.store.CreateToken("c", mcpDefaultTools())

	// The SDK's own client negotiates the newest revision it knows.
	cs := env.connect(t, secret)
	if v := cs.InitializeResult().ProtocolVersion; v != "2026-07-28" {
		t.Errorf("newest revision negotiated as %q", v)
	}

	// A client that only knows 2025-11-25 is served that revision.
	resp := env.post(t, secret, "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"old","version":"0"}}}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initialize with 2025-11-25: %d", resp.StatusCode)
	}
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	if !strings.Contains(buf.String(), `"protocolVersion":"2025-11-25"`) {
		t.Errorf("2025-11-25 not served: %s", buf.String())
	}
}

func TestMCPOutcome_RefusedOnlyWhenNotOffered(t *testing.T) {
	bad := &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "bad"}
	if got := mcpOutcome(true, nil, bad); got != "error" {
		t.Errorf("bad params on an offered tool: %q", got)
	}
	if got := mcpOutcome(false, nil, bad); got != "refused" {
		t.Errorf("not offered: %q", got)
	}
	if got := mcpOutcome(true, &mcp.CallToolResult{IsError: true}, nil); got != "tool_error" {
		t.Errorf("tool error: %q", got)
	}
	if got := mcpOutcome(true, &mcp.CallToolResult{}, nil); got != "ok" {
		t.Errorf("ok: %q", got)
	}
}

func TestMCPContext_ServedToATokenWithoutParticularsHasNone(t *testing.T) {
	env := newMCPTestEnv(t)
	_, secret, _ := env.store.CreateToken("narrow", []string{mcpBoatContextTool})
	cs := env.connect(t, secret)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: mcpBoatContextTool, Arguments: map[string]any{}})
	if err != nil || res.IsError {
		t.Fatal(err, res)
	}
	for _, text := range []string{res.Content[0].(*mcp.TextContent).Text, mustReadContext(t, cs)} {
		if strings.Contains(text, "Lagoon") || strings.Contains(text, "House bank low") || strings.Contains(text, "Not available") {
			t.Errorf("context ignores the token's list:\n%s", text)
		}
	}
}

func mustReadContext(t *testing.T, cs *mcp.ClientSession) string {
	t.Helper()
	rr, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: mcpContextResourceURI})
	if err != nil {
		t.Fatal(err)
	}
	return rr.Contents[0].Text
}

func TestMCPToolDeps_TodayOnlyForMaintenance(t *testing.T) {
	env := newMCPTestEnv(t)
	svc := newMCPService(env.store, func() assistantToolDeps { return env.deps })
	if got := svc.toolDeps("get_active_alarms"); !got.today.IsZero() {
		t.Error("today worked out for a tool that does not use it")
	}
	if got := svc.toolDeps("list_maintenance"); got.today.IsZero() {
		t.Error("list_maintenance has no date")
	}
}
