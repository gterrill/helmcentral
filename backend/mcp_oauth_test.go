package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// OAuth sign-in for the MCP endpoint (mcp_oauth.go): the access token a client
// receives is the outside-assistant token the operator pasted on the sign-in
// page, so these tests lean on the real token store and the real /mcp handler.

const (
	oauthTestBase     = "https://helm.example"
	oauthTestRedirect = "https://chat.example/oauth/callback"
)

type oauthEnv struct {
	store *outsideAssistantStore
	oauth *mcpOAuth
	e     *echo.Echo
	clock time.Time
}

func newOAuthEnv(t *testing.T) *oauthEnv {
	t.Helper()
	env := &oauthEnv{store: newTestMCPStore(t), clock: time.Date(2026, 10, 30, 2, 0, 0, 0, time.UTC)}
	env.oauth = newMCPOAuth(env.store)
	env.oauth.now = func() time.Time { return env.clock }
	env.store.now = func() time.Time { return env.clock }
	svc := newMCPService(env.store, func() assistantToolDeps { return contextTestDeps(t) })
	env.e = echo.New()
	env.e.Any(mcpPath, svc.Handle)
	env.oauth.register(env.e)
	return env
}

func (env *oauthEnv) do(method, target, contentType, body string, header ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Host = "helm.example"
	req.Header.Set("X-Forwarded-Proto", "https")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	env.e.ServeHTTP(rec, req)
	return rec
}

func (env *oauthEnv) postForm(target string, v url.Values) *httptest.ResponseRecorder {
	return env.do(http.MethodPost, target, "application/x-www-form-urlencoded", v.Encode())
}

func registerBody(name string, uris ...string) string {
	b, _ := json.Marshal(map[string]any{"client_name": name, "redirect_uris": uris})
	return string(b)
}

func (env *oauthEnv) register(body string) *httptest.ResponseRecorder {
	return env.do(http.MethodPost, "/mcp/oauth/register", "application/json", body)
}

func (env *oauthEnv) registerOK(t *testing.T, name string) string {
	t.Helper()
	rec := env.register(registerBody(name, oauthTestRedirect))
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rec.Code, rec.Body.String())
	}
	return jsonBody(t, rec)["client_id"].(string)
}

func pkcePair() (verifier, challenge string) {
	verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

func authorizeValues(clientID, challenge string) url.Values {
	return url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {oauthTestRedirect},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {"xyz 1"},
		"resource":              {oauthTestBase + "/mcp"},
	}
}

func location(t *testing.T, rec *httptest.ResponseRecorder) *url.URL {
	t.Helper()
	if rec.Code != http.StatusFound {
		t.Fatalf("want a 302, got %d: %s", rec.Code, rec.Body.String())
	}
	u, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// signIn runs the authorize POST with a pasted token and returns the code.
func (env *oauthEnv) signIn(t *testing.T, clientID, challenge, secret string) string {
	t.Helper()
	v := authorizeValues(clientID, challenge)
	v.Set("action", "allow")
	v.Set("token", secret)
	loc := location(t, env.postForm("/mcp/oauth/authorize", v))
	code := loc.Query().Get("code")
	if code == "" {
		t.Fatalf("no code in %s", loc)
	}
	return code
}

func tokenValues(clientID, code, verifier string) url.Values {
	return url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID},
		"redirect_uri":  {oauthTestRedirect},
		"code_verifier": {verifier},
	}
}

func jsonBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %q", rec.Body.String())
	}
	return out
}

func TestMCPOAuth_MetadataDocuments(t *testing.T) {
	env := newOAuthEnv(t)
	for _, p := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp"} {
		rec := env.do(http.MethodGet, p, "", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d", p, rec.Code)
		}
		doc := jsonBody(t, rec)
		if doc["resource"] != oauthTestBase+"/mcp" || doc["resource_name"] != "Helmcentral" {
			t.Fatalf("%s: %v", p, doc)
		}
		if as, _ := doc["authorization_servers"].([]any); len(as) != 1 || as[0] != oauthTestBase {
			t.Fatalf("%s authorization_servers: %v", p, doc["authorization_servers"])
		}
		if bm, _ := doc["bearer_methods_supported"].([]any); len(bm) != 1 || bm[0] != "header" {
			t.Fatalf("%s bearer methods: %v", p, doc["bearer_methods_supported"])
		}
	}
	doc := jsonBody(t, env.do(http.MethodGet, "/.well-known/oauth-authorization-server", "", ""))
	for k, want := range map[string]any{
		"issuer":                 oauthTestBase,
		"authorization_endpoint": oauthTestBase + "/mcp/oauth/authorize",
		"token_endpoint":         oauthTestBase + "/mcp/oauth/token",
		"registration_endpoint":  oauthTestBase + "/mcp/oauth/register",
		"authorization_response_iss_parameter_supported": true,
	} {
		if doc[k] != want {
			t.Fatalf("%s: got %v, want %v", k, doc[k], want)
		}
	}
	for k, want := range map[string]string{
		"response_types_supported":              "code",
		"grant_types_supported":                 "authorization_code",
		"code_challenge_methods_supported":      "S256",
		"token_endpoint_auth_methods_supported": "none",
	} {
		if l, _ := doc[k].([]any); len(l) != 1 || l[0] != want {
			t.Fatalf("%s: %v", k, doc[k])
		}
	}
	if _, ok := doc["client_id_metadata_document_supported"]; ok {
		t.Fatal("client ID metadata documents must not be advertised: they need an outbound fetch")
	}
}

func TestMCPOAuth_BaseURLFollowsHostAndForwardedProto(t *testing.T) {
	env := newOAuthEnv(t)
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	req.Host = "192.168.1.5:8080"
	rec := httptest.NewRecorder()
	env.e.ServeHTTP(rec, req)
	if got := jsonBody(t, rec)["resource"]; got != "http://192.168.1.5:8080/mcp" {
		t.Fatalf("plain http: %v", got)
	}
}

func TestMCPOAuth_MCP401PointsAtTheResourceMetadata(t *testing.T) {
	env := newOAuthEnv(t)
	rec := env.do(http.MethodPost, "/mcp", "application/json", mcpListBody)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
	want := `Bearer realm="helmcentral", resource_metadata="` + oauthTestBase + `/.well-known/oauth-protected-resource/mcp"`
	if got := rec.Header().Get("WWW-Authenticate"); got != want {
		t.Fatalf("WWW-Authenticate: %q, want %q", got, want)
	}
}

func TestMCPOAuth_RegisterSuccessShape(t *testing.T) {
	env := newOAuthEnv(t)
	rec := env.register(registerBody("  ChatGPT  ", oauthTestRedirect))
	if rec.Code != http.StatusCreated {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control: %q", rec.Header().Get("Cache-Control"))
	}
	doc := jsonBody(t, rec)
	id, _ := doc["client_id"].(string)
	if !strings.HasPrefix(id, "hcoc_") || len(id) < len("hcoc_")+22 {
		t.Fatalf("client_id: %q", id)
	}
	if doc["client_name"] != "ChatGPT" || doc["token_endpoint_auth_method"] != "none" {
		t.Fatalf("%v", doc)
	}
	if _, ok := doc["client_id_issued_at"].(float64); !ok {
		t.Fatalf("client_id_issued_at: %v", doc)
	}
	if uris, _ := doc["redirect_uris"].([]any); len(uris) != 1 || uris[0] != oauthTestRedirect {
		t.Fatalf("redirect_uris: %v", doc["redirect_uris"])
	}
	if g, _ := doc["grant_types"].([]any); len(g) != 1 || g[0] != "authorization_code" {
		t.Fatalf("grant_types: %v", doc["grant_types"])
	}
	if g, _ := doc["response_types"].([]any); len(g) != 1 || g[0] != "code" {
		t.Fatalf("response_types: %v", doc["response_types"])
	}
	// No name gets the plain default.
	rec = env.register(`{"redirect_uris":["https://a.example/cb"]}`)
	if rec.Code != http.StatusCreated || jsonBody(t, rec)["client_name"] != "An outside assistant" {
		t.Fatalf("default name: %d %s", rec.Code, rec.Body.String())
	}
	// Clients that ask for refresh_token as well are accepted and told they
	// got authorization_code only.
	rec = env.register(`{"redirect_uris":["https://a.example/cb"],"grant_types":["authorization_code","refresh_token"],"response_types":["code"],"token_endpoint_auth_method":"none"}`)
	if g, _ := jsonBody(t, rec)["grant_types"].([]any); rec.Code != http.StatusCreated || len(g) != 1 || g[0] != "authorization_code" {
		t.Fatalf("grant types with refresh_token: %d %s", rec.Code, rec.Body.String())
	}
	// Loopback http is allowed for native clients.
	for _, u := range []string{"http://localhost:7000/cb", "http://127.0.0.1:7000/cb", "http://[::1]:7000/cb"} {
		if rec := env.register(registerBody("x", u)); rec.Code != http.StatusCreated {
			t.Fatalf("%s: %d %s", u, rec.Code, rec.Body.String())
		}
	}
}

func TestMCPOAuth_RegisterRefusals(t *testing.T) {
	env := newOAuthEnv(t)
	many := []string{}
	for i := 0; i < 6; i++ {
		many = append(many, fmt.Sprintf("https://a.example/cb%d", i))
	}
	badRedirect := map[string]string{
		"plain http":     registerBody("x", "http://chat.example/cb"),
		"fragment":       registerBody("x", "https://chat.example/cb#frag"),
		"empty fragment": registerBody("x", "https://chat.example/cb#"),
		"relative":       registerBody("x", "/cb"),
		"custom scheme":  registerBody("x", "myapp://cb"),
		"userinfo":       registerBody("x", "https://u:p@chat.example/cb"),
		"none":           `{"client_name":"x"}`,
		"empty list":     registerBody("x"),
		"six":            registerBody("x", many...),
		"http lookalike": registerBody("x", "http://localhost.evil.example/cb"),
	}
	for name, body := range badRedirect {
		rec := env.register(body)
		if rec.Code != http.StatusBadRequest || jsonBody(t, rec)["error"] != "invalid_redirect_uri" {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	metadata := map[string]string{
		"confidential":  `{"redirect_uris":["https://a.example/cb"],"token_endpoint_auth_method":"client_secret_basic"}`,
		"implicit":      `{"redirect_uris":["https://a.example/cb"],"grant_types":["implicit"]}`,
		"password":      `{"redirect_uris":["https://a.example/cb"],"grant_types":["password"]}`,
		"token resp":    `{"redirect_uris":["https://a.example/cb"],"response_types":["token"]}`,
		"control chars": `{"redirect_uris":["https://a.example/cb"],"client_name":"a\u0007b"}`,
		"bidi override": `{"redirect_uris":["https://a.example/cb"],"client_name":"Chat\u202eTPG"}`,
		"zero width":    `{"redirect_uris":["https://a.example/cb"],"client_name":"Chat\u200bGPT"}`,
		"long name":     `{"redirect_uris":["https://a.example/cb"],"client_name":"` + strings.Repeat("a", 81) + `"}`,
		"not json":      `nope`,
		"not a string":  `{"redirect_uris":[1]}`,
	}
	for name, body := range metadata {
		rec := env.register(body)
		if rec.Code != http.StatusBadRequest || jsonBody(t, rec)["error"] != "invalid_client_metadata" {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	rec := env.register(`{"redirect_uris":["https://a.example/cb"],"pad":"` + strings.Repeat("a", 17<<10) + `"}`)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("an oversized body: %d", rec.Code)
	}
}

func TestMCPOAuth_RegisterAtTheCapEvictsTheOldestUnauthorizedClient(t *testing.T) {
	env := newOAuthEnv(t)
	tok, _, _ := env.store.CreateToken("ChatGPT", mcpDefaultTools())
	var ids []string
	for i := 0; i < mcpOAuthClientCap; i++ {
		ids = append(ids, env.registerOK(t, fmt.Sprintf("app %d", i)))
	}
	if err := env.store.MarkOAuthClientAuthorized(ids[0], tok.ID); err != nil {
		t.Fatal(err)
	}
	env.clock = env.clock.Add(mcpOAuthClientGrace + time.Minute)
	fresh := env.registerOK(t, "newcomer")
	if _, found, _ := env.store.GetOAuthClient(ids[0]); !found {
		t.Fatal("an authorized client was evicted")
	}
	if _, found, _ := env.store.GetOAuthClient(ids[1]); found {
		t.Fatal("the oldest never-authorized client should have gone")
	}
	if _, found, _ := env.store.GetOAuthClient(fresh); !found {
		t.Fatal("the new client was not stored")
	}
}

func TestMCPOAuth_RegisterRefusedWhenEveryClientIsAuthorized(t *testing.T) {
	env := newOAuthEnv(t)
	tok, _, _ := env.store.CreateToken("ChatGPT", mcpDefaultTools())
	for i := 0; i < mcpOAuthClientCap; i++ {
		id := env.registerOK(t, fmt.Sprintf("app %d", i))
		if err := env.store.MarkOAuthClientAuthorized(id, tok.ID); err != nil {
			t.Fatal(err)
		}
	}
	rec := env.register(registerBody("one too many", oauthTestRedirect))
	if rec.Code != http.StatusServiceUnavailable || jsonBody(t, rec)["error"] != "temporarily_unavailable" {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

// A client that has not signed in yet is kept for its first 15 minutes, so a
// flood of registrations cannot push out the one the operator is in the middle
// of signing in with.
func TestMCPOAuth_ANeverAuthorizedClientIsSafeForItsFirstFifteenMinutes(t *testing.T) {
	env := newOAuthEnv(t)
	var ids []string
	for i := 0; i < mcpOAuthClientCap; i++ {
		ids = append(ids, env.registerOK(t, fmt.Sprintf("app %d", i)))
	}
	for _, wait := range []time.Duration{0, 14*time.Minute + 59*time.Second} {
		env.clock = env.clock.Add(wait)
		rec := env.register(registerBody("flood", oauthTestRedirect))
		if rec.Code != http.StatusServiceUnavailable || jsonBody(t, rec)["error"] != "temporarily_unavailable" {
			t.Fatalf("after %s: %d %s", wait, rec.Code, rec.Body.String())
		}
	}
	env.clock = env.clock.Add(2 * time.Second)
	fresh := env.registerOK(t, "newcomer")
	if _, found, _ := env.store.GetOAuthClient(ids[0]); found {
		t.Fatal("the oldest client should have gone once it passed 15 minutes")
	}
	if _, found, _ := env.store.GetOAuthClient(ids[1]); !found {
		t.Fatal("only one client should have been evicted")
	}
	if _, found, _ := env.store.GetOAuthClient(fresh); !found {
		t.Fatal("the new client was not stored")
	}
}

// A client is kept at the cap only while the token it signed in with is live.
// Once that token is revoked the client can sign in to nothing, so it makes
// room like one that never signed in, and re-adding apps over the years never
// fills the table for good.
func TestMCPOAuth_AClientWhoseTokenWasRevokedCanBeEvicted(t *testing.T) {
	env := newOAuthEnv(t)
	live, _, _ := env.store.CreateToken("ChatGPT", mcpDefaultTools())
	gone, _, _ := env.store.CreateToken("Old ChatGPT", mcpDefaultTools())
	var ids []string
	for i := 0; i < mcpOAuthClientCap; i++ {
		id := env.registerOK(t, fmt.Sprintf("app %d", i))
		tok := live.ID
		if i == 1 {
			tok = gone.ID
		}
		if err := env.store.MarkOAuthClientAuthorized(id, tok); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := env.store.RevokeToken(gone.ID); err != nil {
		t.Fatal(err)
	}
	env.registerOK(t, "newcomer")
	if _, found, _ := env.store.GetOAuthClient(ids[1]); found {
		t.Fatal("the client signed in with a revoked token should have made room")
	}
	if _, found, _ := env.store.GetOAuthClient(ids[0]); !found {
		t.Fatal("a client with a live token was evicted")
	}
}

func TestMCPOAuth_AuthorizeNeverRedirectsForABadClientOrRedirect(t *testing.T) {
	env := newOAuthEnv(t)
	id := env.registerOK(t, "ChatGPT")
	_, challenge := pkcePair()

	unknown := authorizeValues("hcoc_unknown", challenge)
	other := authorizeValues(id, challenge)
	other.Set("redirect_uri", "https://evil.example/cb")
	prefix := authorizeValues(id, challenge)
	prefix.Set("redirect_uri", oauthTestRedirect+"/x")
	for name, v := range map[string]url.Values{"unknown client": unknown, "other redirect": other, "prefix redirect": prefix} {
		for _, rec := range []*httptest.ResponseRecorder{
			env.do(http.MethodGet, "/mcp/oauth/authorize?"+v.Encode(), "", ""),
			env.postForm("/mcp/oauth/authorize", v),
		} {
			if rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" {
				t.Fatalf("%s: %d, Location %q", name, rec.Code, rec.Header().Get("Location"))
			}
			if !strings.Contains(rec.Body.String(), "This sign-in link isn't valid") {
				t.Fatalf("%s: %s", name, rec.Body.String())
			}
		}
	}
}

// Registration is open, so anyone can register a client with a redirect of
// their own. Every fault found before the operator answers therefore gets the
// error page: a redirect would make the boat's public address an open redirector.
func TestMCPOAuth_AuthorizeFaultsBeforeTheOperatorAnswersNeverRedirect(t *testing.T) {
	env := newOAuthEnv(t)
	id := env.registerOK(t, "ChatGPT")
	_, challenge := pkcePair()
	cases := map[string]func(url.Values){
		"response_type":  func(v url.Values) { v.Set("response_type", "token") },
		"bogus type":     func(v url.Values) { v.Set("response_type", "bogus") },
		"no challenge":   func(v url.Values) { v.Del("code_challenge") },
		"short":          func(v url.Values) { v.Set("code_challenge", "abc") },
		"bad charset":    func(v url.Values) { v.Set("code_challenge", strings.Repeat("+", 43)) },
		"no method":      func(v url.Values) { v.Del("code_challenge_method") },
		"plain method":   func(v url.Values) { v.Set("code_challenge_method", "plain") },
		"wrong resource": func(v url.Values) { v.Set("resource", "https://other.example/mcp") },
	}
	for name, mutate := range cases {
		v := authorizeValues(id, challenge)
		mutate(v)
		post := url.Values{"action": {"allow"}, "token": {"hcmcp_nope"}}
		for k, vals := range v {
			post[k] = vals
		}
		for _, rec := range []*httptest.ResponseRecorder{
			env.do(http.MethodGet, "/mcp/oauth/authorize?"+v.Encode(), "", ""),
			env.postForm("/mcp/oauth/authorize", post),
		} {
			if rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" {
				t.Fatalf("%s: %d, Location %q", name, rec.Code, rec.Header().Get("Location"))
			}
			if !strings.Contains(rec.Body.String(), "This sign-in link isn't valid") {
				t.Fatalf("%s: %s", name, rec.Body.String())
			}
		}
	}
	// A POST with no answer, or one the page never offers, is a fault too.
	for name, action := range map[string][]string{"no action": nil, "unknown action": {"maybe"}} {
		v := authorizeValues(id, challenge)
		if action != nil {
			v["action"] = action
		}
		rec := env.postForm("/mcp/oauth/authorize", v)
		if rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" {
			t.Fatalf("%s: %d, Location %q", name, rec.Code, rec.Header().Get("Location"))
		}
	}
}

func TestMCPOAuth_AuthorizePageNamesTheClientSafelyAndSetsHeaders(t *testing.T) {
	env := newOAuthEnv(t)
	rec := env.register(registerBody("<script>x</script>", oauthTestRedirect))
	id := jsonBody(t, rec)["client_id"].(string)
	_, challenge := pkcePair()
	rec = env.do(http.MethodGet, "/mcp/oauth/authorize?"+authorizeValues(id, challenge).Encode(), "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "<script>x") {
		t.Fatal("the client name was not escaped")
	}
	for _, want := range []string{"&lt;script&gt;x&lt;/script&gt;", "Sign in to Helmcentral", "chat.example", `type="password"`, `name="token"`, "Allow", "Cancel", `value="allow"`, `value="deny"`, `name="code_challenge"`, `name="state"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("page lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "may have been revoked") {
		t.Fatal("the bad-token message showed before any attempt")
	}
	h := rec.Header()
	if got := h.Get("Content-Security-Policy"); got != "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'" {
		t.Fatalf("CSP: %q", got)
	}
	if h.Get("X-Frame-Options") != "DENY" || h.Get("Referrer-Policy") != "no-referrer" || h.Get("Cache-Control") != "no-store" {
		t.Fatalf("headers: %v", h)
	}
	if ct := h.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type: %q", ct)
	}
}

func TestMCPOAuth_AuthorizePostRefusesABadTokenWithoutRedirecting(t *testing.T) {
	env := newOAuthEnv(t)
	id := env.registerOK(t, "ChatGPT")
	_, challenge := pkcePair()
	for _, tok := range []string{"", "hcmcp_nope"} {
		v := authorizeValues(id, challenge)
		v.Set("action", "allow")
		v.Set("token", tok)
		rec := env.postForm("/mcp/oauth/authorize", v)
		if rec.Code != http.StatusUnauthorized || rec.Header().Get("Location") != "" {
			t.Fatalf("%q: %d", tok, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "That token isn't valid. It may have been revoked.") {
			t.Fatalf("%q: %s", tok, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "hcmcp_nope") {
			t.Fatal("the pasted token was echoed back into the page")
		}
		if rec.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("the re-rendered page lost its headers")
		}
	}
	if c, _, _ := env.store.GetOAuthClient(id); c.AuthorizedAt != nil {
		t.Fatal("a failed sign-in marked the client authorized")
	}
}

func TestMCPOAuth_AuthorizePostDenyRedirectsAccessDenied(t *testing.T) {
	env := newOAuthEnv(t)
	id := env.registerOK(t, "ChatGPT")
	_, challenge := pkcePair()
	v := authorizeValues(id, challenge)
	v.Set("action", "deny")
	q := location(t, env.postForm("/mcp/oauth/authorize", v)).Query()
	if q.Get("error") != "access_denied" || q.Get("state") != "xyz 1" || q.Get("iss") != oauthTestBase || q.Get("code") != "" {
		t.Fatalf("%v", q)
	}
}

func TestMCPOAuth_AuthorizePostAllowIssuesACode(t *testing.T) {
	env := newOAuthEnv(t)
	_, secret, _ := env.store.CreateToken("ChatGPT token", mcpDefaultTools())
	id := env.registerOK(t, "ChatGPT")
	_, challenge := pkcePair()
	v := authorizeValues(id, challenge)
	v.Set("action", "allow")
	v.Set("token", "  "+secret+"\n")
	loc := location(t, env.postForm("/mcp/oauth/authorize", v))
	q := loc.Query()
	if loc.Host != "chat.example" || loc.Path != "/oauth/callback" || len(q.Get("code")) < 43 || q.Get("state") != "xyz 1" || q.Get("iss") != oauthTestBase {
		t.Fatalf("%s", loc)
	}
	if c, _, _ := env.store.GetOAuthClient(id); c.AuthorizedAt == nil {
		t.Fatal("the client was not marked authorized")
	}
	env.store.waitForQueued()
	acts, err := env.store.ListActivity(5)
	if err != nil || len(acts) != 1 || acts[0].Client != "ChatGPT token" || acts[0].Name != "sign-in from ChatGPT (chat.example)" || acts[0].Outcome != "ok" || acts[0].Kind != "request" {
		t.Fatalf("activity: %+v %v", acts, err)
	}
}

var oauthHiddenInput = regexp.MustCompile(`<input type="hidden" name="([^"]*)" value="([^"]*)">`)

// A client that sends no resource parameter signs in and exchanges its code:
// the form carries back only the fields the request had.
func TestMCPOAuth_FullFlowWithoutAResourceParameter(t *testing.T) {
	env := newOAuthEnv(t)
	_, secret, _ := env.store.CreateToken("ChatGPT", mcpDefaultTools())
	id := env.registerOK(t, "ChatGPT")
	verifier, challenge := pkcePair()
	v := authorizeValues(id, challenge)
	v.Del("resource")
	page := env.do(http.MethodGet, "/mcp/oauth/authorize?"+v.Encode(), "", "")
	if page.Code != http.StatusOK {
		t.Fatalf("page: %d %s", page.Code, page.Body.String())
	}
	if strings.Contains(page.Body.String(), `name="resource"`) || strings.Contains(page.Body.String(), `name="scope"`) {
		t.Fatalf("the form carries fields the request did not have:\n%s", page.Body.String())
	}
	form := url.Values{"action": {"allow"}, "token": {secret}}
	for _, m := range oauthHiddenInput.FindAllStringSubmatch(page.Body.String(), -1) {
		form.Set(m[1], html.UnescapeString(m[2]))
	}
	code := location(t, env.postForm("/mcp/oauth/authorize", form)).Query().Get("code")
	if code == "" {
		t.Fatal("Allow did not issue a code")
	}
	if rec := env.postForm("/mcp/oauth/token", tokenValues(id, code, verifier)); rec.Code != http.StatusOK {
		t.Fatalf("token: %d %s", rec.Code, rec.Body.String())
	}
}

// An empty resource is the same as none.
func TestMCPOAuth_AnEmptyResourceCountsAsAbsent(t *testing.T) {
	env := newOAuthEnv(t)
	_, secret, _ := env.store.CreateToken("ChatGPT", mcpDefaultTools())
	id := env.registerOK(t, "ChatGPT")
	verifier, challenge := pkcePair()
	v := authorizeValues(id, challenge)
	v.Set("resource", "")
	v.Set("action", "allow")
	v.Set("token", secret)
	code := location(t, env.postForm("/mcp/oauth/authorize", v)).Query().Get("code")
	tv := tokenValues(id, code, verifier)
	tv.Set("resource", "")
	if rec := env.postForm("/mcp/oauth/token", tv); rec.Code != http.StatusOK {
		t.Fatalf("token: %d %s", rec.Code, rec.Body.String())
	}
}

func TestMCPOAuth_AuthorizeKeepsTheRegisteredRedirectsQuery(t *testing.T) {
	env := newOAuthEnv(t)
	rec := env.register(registerBody("x", "https://chat.example/cb?tenant=7"))
	id := jsonBody(t, rec)["client_id"].(string)
	_, secret, _ := env.store.CreateToken("t", mcpDefaultTools())
	_, challenge := pkcePair()
	v := authorizeValues(id, challenge)
	v.Set("redirect_uri", "https://chat.example/cb?tenant=7")
	v.Set("action", "allow")
	v.Set("token", secret)
	q := location(t, env.postForm("/mcp/oauth/authorize", v)).Query()
	if q.Get("tenant") != "7" || q.Get("code") == "" {
		t.Fatalf("%v", q)
	}
}

func TestMCPOAuth_AuthorizePostRefusesARevokedToken(t *testing.T) {
	env := newOAuthEnv(t)
	rec, secret, _ := env.store.CreateToken("old", mcpDefaultTools())
	if err := env.store.RevokeToken(rec.ID); err != nil {
		t.Fatal(err)
	}
	id := env.registerOK(t, "ChatGPT")
	_, challenge := pkcePair()
	v := authorizeValues(id, challenge)
	v.Set("action", "allow")
	v.Set("token", secret)
	if rec := env.postForm("/mcp/oauth/authorize", v); rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token: %d", rec.Code)
	}
}

func TestMCPOAuth_FullFlowEndsWithAWorkingBearerToken(t *testing.T) {
	env := newOAuthEnv(t)
	_, secret, _ := env.store.CreateToken("ChatGPT", mcpDefaultTools())
	id := env.registerOK(t, "ChatGPT")
	verifier, challenge := pkcePair()
	code := env.signIn(t, id, challenge, secret)

	rec := env.postForm("/mcp/oauth/token", tokenValues(id, code, verifier))
	if rec.Code != http.StatusOK {
		t.Fatalf("token: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("headers: %v", rec.Header())
	}
	doc := jsonBody(t, rec)
	if doc["access_token"] != secret || doc["token_type"] != "Bearer" {
		t.Fatalf("%v", doc)
	}
	if _, ok := doc["refresh_token"]; ok {
		t.Fatal("there is no refresh token")
	}
	if _, ok := doc["expires_in"]; ok {
		t.Fatal("the token does not expire")
	}

	call := env.do(http.MethodPost, "/mcp", "application/json", mcpListBody,
		"Authorization", "Bearer "+doc["access_token"].(string), "Accept", "application/json, text/event-stream")
	if call.Code != http.StatusOK || !strings.Contains(call.Body.String(), `"tools"`) {
		t.Fatalf("/mcp with the access token: %d %s", call.Code, call.Body.String())
	}
}

func TestMCPOAuth_TokenRefusals(t *testing.T) {
	verifier, challenge := pkcePair()
	type setup struct {
		env      *oauthEnv
		id, code string
	}
	fresh := func(t *testing.T) setup {
		env := newOAuthEnv(t)
		_, secret, _ := env.store.CreateToken("ChatGPT", mcpDefaultTools())
		id := env.registerOK(t, "ChatGPT")
		return setup{env, id, env.signIn(t, id, challenge, secret)}
	}
	cases := []struct {
		name   string
		mutate func(t *testing.T, s setup, v url.Values)
		want   string
	}{
		{"wrong verifier", func(t *testing.T, s setup, v url.Values) { v.Set("code_verifier", strings.Repeat("a", 43)) }, "invalid_grant"},
		{"missing verifier", func(t *testing.T, s setup, v url.Values) { v.Del("code_verifier") }, "invalid_grant"},
		{"short verifier", func(t *testing.T, s setup, v url.Values) { v.Set("code_verifier", "short") }, "invalid_grant"},
		{"verifier charset", func(t *testing.T, s setup, v url.Values) { v.Set("code_verifier", strings.Repeat("+", 43)) }, "invalid_grant"},
		{"wrong redirect", func(t *testing.T, s setup, v url.Values) { v.Set("redirect_uri", "https://chat.example/other") }, "invalid_grant"},
		{"wrong client", func(t *testing.T, s setup, v url.Values) { v.Set("client_id", s.env.registerOK(t, "other")) }, "invalid_grant"},
		{"unknown code", func(t *testing.T, s setup, v url.Values) { v.Set("code", "nope") }, "invalid_grant"},
		{"wrong resource", func(t *testing.T, s setup, v url.Values) { v.Set("resource", "https://other.example/mcp") }, "invalid_target"},
		{"unsupported grant", func(t *testing.T, s setup, v url.Values) { v.Set("grant_type", "refresh_token") }, "unsupported_grant_type"},
		{"no grant", func(t *testing.T, s setup, v url.Values) { v.Del("grant_type") }, "unsupported_grant_type"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := fresh(t)
			v := tokenValues(s.id, s.code, verifier)
			tc.mutate(t, s, v)
			rec := s.env.postForm("/mcp/oauth/token", v)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%d %s", rec.Code, rec.Body.String())
			}
			doc := jsonBody(t, rec)
			if doc["error"] != tc.want || doc["error_description"] == "" {
				t.Fatalf("%v", doc)
			}
			if rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("Cache-Control: %q", rec.Header().Get("Cache-Control"))
			}
			if strings.Contains(rec.Body.String(), "hcmcp_") {
				t.Fatal("a refusal leaked the token")
			}
		})
	}

	t.Run("matching resource is fine", func(t *testing.T) {
		s := fresh(t)
		v := tokenValues(s.id, s.code, verifier)
		v.Set("resource", oauthTestBase+"/mcp")
		if rec := s.env.postForm("/mcp/oauth/token", v); rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("code is single use", func(t *testing.T) {
		s := fresh(t)
		if rec := s.env.postForm("/mcp/oauth/token", tokenValues(s.id, s.code, verifier)); rec.Code != http.StatusOK {
			t.Fatalf("first use: %d", rec.Code)
		}
		rec := s.env.postForm("/mcp/oauth/token", tokenValues(s.id, s.code, verifier))
		if rec.Code != http.StatusBadRequest || jsonBody(t, rec)["error"] != "invalid_grant" {
			t.Fatalf("reuse: %d %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("a failed attempt still burns the code", func(t *testing.T) {
		s := fresh(t)
		bad := tokenValues(s.id, s.code, strings.Repeat("a", 43))
		if rec := s.env.postForm("/mcp/oauth/token", bad); rec.Code != http.StatusBadRequest {
			t.Fatalf("%d", rec.Code)
		}
		if rec := s.env.postForm("/mcp/oauth/token", tokenValues(s.id, s.code, verifier)); rec.Code != http.StatusBadRequest {
			t.Fatalf("the right verifier after a wrong one: %d", rec.Code)
		}
	})
	t.Run("expired code", func(t *testing.T) {
		s := fresh(t)
		s.env.clock = s.env.clock.Add(5*time.Minute + time.Second)
		rec := s.env.postForm("/mcp/oauth/token", tokenValues(s.id, s.code, verifier))
		if rec.Code != http.StatusBadRequest || jsonBody(t, rec)["error"] != "invalid_grant" {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("just inside the lifetime", func(t *testing.T) {
		s := fresh(t)
		s.env.clock = s.env.clock.Add(4*time.Minute + 59*time.Second)
		if rec := s.env.postForm("/mcp/oauth/token", tokenValues(s.id, s.code, verifier)); rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("token revoked between sign-in and exchange", func(t *testing.T) {
		env := newOAuthEnv(t)
		rec, secret, _ := env.store.CreateToken("ChatGPT", mcpDefaultTools())
		id := env.registerOK(t, "ChatGPT")
		code := env.signIn(t, id, challenge, secret)
		if err := env.store.RevokeToken(rec.ID); err != nil {
			t.Fatal(err)
		}
		out := env.postForm("/mcp/oauth/token", tokenValues(id, code, verifier))
		if out.Code != http.StatusBadRequest || jsonBody(t, out)["error"] != "invalid_grant" {
			t.Fatalf("%d %s", out.Code, out.Body.String())
		}
	})
	t.Run("oversized body", func(t *testing.T) {
		env := newOAuthEnv(t)
		rec := env.postForm("/mcp/oauth/token", url.Values{"pad": {strings.Repeat("a", 17<<10)}})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%d", rec.Code)
		}
	})
}

func TestMCPOAuth_PendingCodesAreCapped(t *testing.T) {
	env := newOAuthEnv(t)
	_, secret, _ := env.store.CreateToken("ChatGPT", mcpDefaultTools())
	id := env.registerOK(t, "ChatGPT")
	verifier, challenge := pkcePair()
	first := env.signIn(t, id, challenge, secret)
	for i := 0; i < mcpOAuthCodeCap; i++ {
		env.clock = env.clock.Add(time.Millisecond)
		env.signIn(t, id, challenge, secret)
	}
	if n := env.oauth.pending(); n != mcpOAuthCodeCap {
		t.Fatalf("pending codes: %d", n)
	}
	if rec := env.postForm("/mcp/oauth/token", tokenValues(id, first, verifier)); rec.Code != http.StatusBadRequest {
		t.Fatalf("the oldest code should have been dropped: %d", rec.Code)
	}
}
