package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func mcpRouteRequest(t *testing.T, method, path, body string, h echo.HandlerFunc, params map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	for k, v := range params {
		c.SetParamNames(k)
		c.SetParamValues(v)
	}
	if err := h(c); err != nil {
		t.Fatal(err)
	}
	return rec
}

func withMCPStore(t *testing.T) *outsideAssistantStore {
	t.Helper()
	s := newTestMCPStore(t)
	prev := globalOutsideAssistantStore
	globalOutsideAssistantStore = s
	t.Cleanup(func() { globalOutsideAssistantStore = prev })
	return s
}

func TestOutsideAssistantRoutes_AreAdminTier(t *testing.T) {
	rows := outsideAssistantRoutes()
	if len(rows) == 0 {
		t.Fatal("no routes")
	}
	for _, r := range rows {
		if r.Tier != tierAdmin {
			t.Errorf("%s %s is tier %v, want admin", r.Method, r.Path, r.Tier)
		}
	}
}

func TestOutsideAssistantRoutes_CreateShowsSecretOnceAndSeedsTheCuratedList(t *testing.T) {
	s := withMCPStore(t)
	rec := mcpRouteRequest(t, http.MethodPost, "/api/outside-assistants/tokens", `{"name":"Claude Code"}`, createOutsideAssistantTokenHandler, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var created struct {
		Token  outsideAssistantTokenView `json:"token"`
		Secret string                    `json:"secret"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if !strings.HasPrefix(created.Secret, mcpTokenPrefix) {
		t.Fatalf("secret: %q", created.Secret)
	}
	if strings.Join(created.Token.Tools, ",") != strings.Join(sortedCopy(mcpDefaultTools()), ",") {
		t.Fatalf("seeded tools: %v", created.Token.Tools)
	}
	list := mcpRouteRequest(t, http.MethodGet, "/api/outside-assistants", "", listOutsideAssistantsHandler, nil)
	if strings.Contains(list.Body.String(), created.Secret) {
		t.Fatal("the secret is listed after creation")
	}
	if got, _ := s.ListTokens(); len(got) != 1 {
		t.Fatalf("tokens: %+v", got)
	}
}

func TestOutsideAssistantRoutes_CreateNeedsAName(t *testing.T) {
	withMCPStore(t)
	rec := mcpRouteRequest(t, http.MethodPost, "/api/outside-assistants/tokens", `{"name":"  "}`, createOutsideAssistantTokenHandler, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("blank name: %d", rec.Code)
	}
}

func TestOutsideAssistantRoutes_SwitchToolsRefusesWhatIsNeverOffered(t *testing.T) {
	s := withMCPStore(t)
	tok, _, _ := s.CreateToken("a", nil)
	ok := mcpRouteRequest(t, http.MethodPut, "/api/outside-assistants/tokens/x/tools", `{"tools":["get_vessel_status","check_signalk_paths"]}`, setOutsideAssistantToolsHandler, map[string]string{"id": tok.ID})
	if ok.Code != http.StatusOK {
		t.Fatalf("switch: %d %s", ok.Code, ok.Body)
	}
	for _, bad := range []string{"search_web", "propose_changes", "read_conversation", "nonsense"} {
		rec := mcpRouteRequest(t, http.MethodPut, "/x", `{"tools":["`+bad+`"]}`, setOutsideAssistantToolsHandler, map[string]string{"id": tok.ID})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, rec.Code)
		}
	}
	got, _ := s.ToolsFor(tok.ID)
	if strings.Join(got, ",") != "check_signalk_paths,get_vessel_status" {
		t.Fatalf("stored tools: %v", got)
	}
}

func TestOutsideAssistantRoutes_RevokeAndUnknown(t *testing.T) {
	s := withMCPStore(t)
	tok, secret, _ := s.CreateToken("a", nil)
	if rec := mcpRouteRequest(t, http.MethodDelete, "/x", "", revokeOutsideAssistantTokenHandler, map[string]string{"id": "nope"}); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown: %d", rec.Code)
	}
	if rec := mcpRouteRequest(t, http.MethodDelete, "/x", "", revokeOutsideAssistantTokenHandler, map[string]string{"id": tok.ID}); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d", rec.Code)
	}
	if _, found, _ := s.Authenticate(secret); found {
		t.Fatal("revoked token still works")
	}
}

func TestOutsideAssistantRoutes_ListCarriesToolsTokensAndActivity(t *testing.T) {
	s := withMCPStore(t)
	tok, _, _ := s.CreateToken("a", []string{"get_vessel_status"})
	_ = s.RecordActivity(mcpActivity{At: s.now(), Client: "a", Kind: "tool", Name: "get_vessel_status", Outcome: "ok", DurationMS: 5})
	rec := mcpRouteRequest(t, http.MethodGet, "/api/outside-assistants", "", listOutsideAssistantsHandler, nil)
	var body struct {
		Tokens []outsideAssistantTokenView `json:"tokens"`
		Tools  []struct {
			Name      string `json:"name"`
			PlainName string `json:"plain_name"`
			DefaultOn bool   `json:"default_on"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Tokens) != 1 || body.Tokens[0].ID != tok.ID || len(body.Tools) != len(mcpToolDefs()) {
		t.Fatalf("body: %s", rec.Body)
	}
	for _, tool := range body.Tools {
		if tool.PlainName == "" {
			t.Errorf("%s has no plain name", tool.Name)
		}
		if !mcpOffered(tool.Name) {
			t.Errorf("%s is listed but never offered", tool.Name)
		}
	}
	act := mcpRouteRequest(t, http.MethodGet, "/api/outside-assistants/activity", "", listOutsideAssistantActivityHandler, nil)
	if !strings.Contains(act.Body.String(), "get_vessel_status") {
		t.Fatalf("activity: %s", act.Body)
	}
}

func TestOutsideAssistantRoutes_StoreFailureIs500NotBlameOnTheName(t *testing.T) {
	s := withMCPStore(t)
	s.rand = failingReader{}
	rec := mcpRouteRequest(t, http.MethodPost, "/x", `{"name":"Laptop"}`, createOutsideAssistantTokenHandler, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "entropy") {
		t.Errorf("internal error leaked: %s", rec.Body)
	}
	long := strings.Repeat("x", mcpTokenNameMax+1)
	if rec := mcpRouteRequest(t, http.MethodPost, "/x", `{"name":"`+long+`"}`, createOutsideAssistantTokenHandler, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("long name: %d", rec.Code)
	}
}

func TestMCPToolDefs_BuiltOnce(t *testing.T) {
	a, b := mcpToolDefs(), mcpToolDefs()
	if &a[0] != &b[0] {
		t.Fatal("tool definitions are rebuilt on every call")
	}
}
