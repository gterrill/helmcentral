package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/labstack/echo/v4"
)

// OAuth sign-in for the MCP endpoint (ADR 0233), for clients such as ChatGPT's
// custom connectors that can sign in with OAuth but cannot send a fixed header.
//
// The access token a client receives is the outside-assistant token (mcp_store.go)
// that the operator pastes on the sign-in page. There is no second token type,
// no refresh token and no expiry: revoking that token in Settings cuts off the
// OAuth client too, and /mcp authenticates exactly as it did before. The sign-in
// page is public (it is reached through Tailscale Funnel) and most boats run
// auth.mode none, so a valid pasted token is the only way to approve; no button
// works without one. Authorization codes live in memory only, for five minutes,
// and hold the pasted secret until they are exchanged or expire.
//
// These are not /api routes. Only the metadata, register, authorize and token
// paths below are reachable through Funnel (funnel_guard.go).

const (
	oauthProtectedResourcePath    = "/.well-known/oauth-protected-resource"
	oauthProtectedResourceMCPPath = "/.well-known/oauth-protected-resource/mcp"
	oauthServerMetadataPath       = "/.well-known/oauth-authorization-server"
	oauthAuthorizePath            = "/mcp/oauth/authorize"
	oauthTokenPath                = "/mcp/oauth/token"
	oauthRegisterPath             = "/mcp/oauth/register"

	mcpOAuthBodyLimit    = 16 << 10
	mcpOAuthCodeTTL      = 5 * time.Minute
	mcpOAuthCodeCap      = 100
	mcpOAuthMaxRedirects = 5
	mcpOAuthNameMax      = 80
	mcpOAuthDefaultName  = "An outside assistant"
)

// oauthBaseURL is the address the caller reached Helmcentral at. Tailscale
// Serve and Funnel keep the original Host and set X-Forwarded-Proto.
func oauthBaseURL(r *http.Request) string { return corsOwnOrigin(r) }

type mcpOAuthCode struct {
	clientID    string
	redirectURI string
	challenge   string
	resource    string
	secret      []byte // the pasted outside-assistant token
	tokenID     string
	created     time.Time
	expires     time.Time
}

func (c *mcpOAuthCode) wipe() {
	for i := range c.secret {
		c.secret[i] = 0
	}
	c.secret = nil
}

type mcpOAuth struct {
	store *outsideAssistantStore
	now   func() time.Time

	mu    sync.Mutex
	codes map[string]*mcpOAuthCode // keyed by the SHA-256 of the code
}

func newMCPOAuth(store *outsideAssistantStore) *mcpOAuth {
	return &mcpOAuth{store: store, now: time.Now, codes: map[string]*mcpOAuthCode{}}
}

func (o *mcpOAuth) register(e *echo.Echo) {
	e.GET(oauthProtectedResourcePath, o.handleProtectedResource)
	e.GET(oauthProtectedResourceMCPPath, o.handleProtectedResource)
	e.GET(oauthServerMetadataPath, o.handleServerMetadata)
	e.POST(oauthRegisterPath, o.handleRegister)
	e.GET(oauthAuthorizePath, o.handleAuthorize)
	e.POST(oauthAuthorizePath, o.handleAuthorize)
	e.POST(oauthTokenPath, o.handleToken)
}

func (o *mcpOAuth) handleProtectedResource(c echo.Context) error {
	base := oauthBaseURL(c.Request())
	return c.JSON(http.StatusOK, map[string]any{
		"resource":                 base + mcpPath,
		"authorization_servers":    []string{base},
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "Helmcentral",
	})
}

// handleServerMetadata deliberately leaves out client_id_metadata_document_supported:
// accepting a URL as a client id means fetching it, and this endpoint is public.
func (o *mcpOAuth) handleServerMetadata(c echo.Context) error {
	base := oauthBaseURL(c.Request())
	return c.JSON(http.StatusOK, map[string]any{
		"issuer":                                         base,
		"authorization_endpoint":                         base + oauthAuthorizePath,
		"token_endpoint":                                 base + oauthTokenPath,
		"registration_endpoint":                          base + oauthRegisterPath,
		"response_types_supported":                       []string{"code"},
		"grant_types_supported":                          []string{"authorization_code"},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          []string{"none"},
		"authorization_response_iss_parameter_supported": true,
	})
}

func oauthJSONError(c echo.Context, status int, code, desc string) error {
	return c.JSON(status, map[string]string{"error": code, "error_description": desc})
}

// handleRegister is RFC 7591 dynamic client registration, for public clients
// only.
func (o *mcpOAuth) handleRegister(c echo.Context) error {
	c.Response().Header().Set("Cache-Control", "no-store")
	body, err := io.ReadAll(io.LimitReader(c.Request().Body, mcpOAuthBodyLimit+1))
	if err != nil {
		return oauthJSONError(c, http.StatusBadRequest, "invalid_client_metadata", "the request body could not be read")
	}
	if len(body) > mcpOAuthBodyLimit {
		return oauthJSONError(c, http.StatusRequestEntityTooLarge, "invalid_client_metadata", "the request body is too large")
	}
	var req struct {
		RedirectURIs            []string `json:"redirect_uris"`
		ClientName              string   `json:"client_name"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
		GrantTypes              []string `json:"grant_types"`
		ResponseTypes           []string `json:"response_types"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return oauthJSONError(c, http.StatusBadRequest, "invalid_client_metadata", "the request body is not the JSON object RFC 7591 describes")
	}
	if len(req.RedirectURIs) == 0 || len(req.RedirectURIs) > mcpOAuthMaxRedirects {
		return oauthJSONError(c, http.StatusBadRequest, "invalid_redirect_uri", "give between 1 and 5 redirect_uris")
	}
	for _, raw := range req.RedirectURIs {
		if reason := badOAuthRedirect(raw); reason != "" {
			return oauthJSONError(c, http.StatusBadRequest, "invalid_redirect_uri", reason)
		}
	}
	if m := req.TokenEndpointAuthMethod; m != "" && m != "none" {
		return oauthJSONError(c, http.StatusBadRequest, "invalid_client_metadata", "only public clients (token_endpoint_auth_method none) are supported")
	}
	// A client that asks for refresh_token is told it was not given one (the
	// response lists authorization_code only); it signs in again instead.
	for _, g := range req.GrantTypes {
		if g != "authorization_code" && g != "refresh_token" {
			return oauthJSONError(c, http.StatusBadRequest, "invalid_client_metadata", "only the authorization_code grant is supported")
		}
	}
	for _, r := range req.ResponseTypes {
		if r != "code" {
			return oauthJSONError(c, http.StatusBadRequest, "invalid_client_metadata", "only the code response type is supported")
		}
	}
	name := strings.TrimSpace(req.ClientName)
	if utf8.RuneCountInString(name) > mcpOAuthNameMax {
		return oauthJSONError(c, http.StatusBadRequest, "invalid_client_metadata", "client_name can be at most 80 characters")
	}
	// Format characters (bidi overrides, zero-width spaces) would let a name
	// pass for another app's on the sign-in page and in the activity list.
	if strings.IndexFunc(name, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) >= 0 {
		return oauthJSONError(c, http.StatusBadRequest, "invalid_client_metadata", "client_name cannot hold control or invisible formatting characters")
	}
	if name == "" {
		name = mcpOAuthDefaultName
	}
	client, err := o.store.RegisterOAuthClient(name, req.RedirectURIs)
	if errors.Is(err, errMCPOAuthClientsFull) {
		log.Printf("outside assistants: refused an OAuth client registration: all %d client slots belong to clients that are signed in or still within their first 15 minutes", mcpOAuthClientCap)
		return oauthJSONError(c, http.StatusServiceUnavailable, "temporarily_unavailable", "this server cannot take another client right now")
	}
	if err != nil {
		log.Printf("outside assistants: %v", err)
		return oauthJSONError(c, http.StatusInternalServerError, "server_error", "the client could not be stored")
	}
	return c.JSON(http.StatusCreated, map[string]any{
		"client_id":                  client.ID,
		"client_id_issued_at":        client.CreatedAt.Unix(),
		"client_name":                client.Name,
		"redirect_uris":              client.RedirectURIs,
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code"},
		"response_types":             []string{"code"},
	})
}

// badOAuthRedirect says why a redirect URI cannot be registered, or "" when it
// can: absolute, no fragment, https, or http to a loopback host.
func badOAuthRedirect(raw string) string {
	if raw == "" || len(raw) > 2048 || strings.Contains(raw, "#") {
		return "a redirect_uri must be absolute and have no fragment"
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Host == "" || u.User != nil {
		return "a redirect_uri must be an absolute https URL"
	}
	switch u.Scheme {
	case "https":
		return ""
	case "http":
		switch u.Hostname() {
		case "localhost", "127.0.0.1", "::1":
			return ""
		}
	}
	return "a redirect_uri must use https (http only for localhost, 127.0.0.1 or [::1])"
}

const (
	oauthChallengeChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	// RFC 7636 section 4.1: the verifier is unreserved characters.
	oauthVerifierChars = oauthChallengeChars + ".~"
)

// oauthPKCEShape reports whether s is 43 to 128 characters drawn from chars.
func oauthPKCEShape(s, chars string) bool {
	if len(s) < 43 || len(s) > 128 {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune(chars, r) {
			return false
		}
	}
	return true
}

func oauthPageHeaders(c echo.Context) {
	h := c.Response().Header()
	// No form-action: browsers apply it to the redirect after the form posts.
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cache-Control", "no-store")
}

// handleAuthorize serves the sign-in page (GET) and takes its answer (POST).
// Both validate the request the same way, and every fault found before the
// operator answers gets the error page, never a redirect. Registration is open,
// so any redirect on file may belong to a stranger, and redirecting on a fault
// would make the boat's public address an open redirector (RFC 9700 4.11.2).
// Only the operator's own choice goes back to the client's registered redirect
// URI: Cancel with access_denied, Allow with a code.
func (o *mcpOAuth) handleAuthorize(c echo.Context) error {
	oauthPageHeaders(c)
	r := c.Request()
	post := r.Method == http.MethodPost
	v := r.URL.Query()
	if post {
		r.Body = http.MaxBytesReader(c.Response(), r.Body, mcpOAuthBodyLimit)
		if err := r.ParseForm(); err != nil {
			return oauthErrorPage(c)
		}
		v = r.PostForm
	}
	client, found, err := o.store.GetOAuthClient(v.Get("client_id"))
	if err != nil {
		log.Printf("outside assistants: %v", err)
		return c.String(http.StatusInternalServerError, "The sign-in could not be checked.")
	}
	redirect := v.Get("redirect_uri")
	if !found || !slices.Contains(client.RedirectURIs, redirect) {
		return oauthErrorPage(c)
	}
	base := oauthBaseURL(r)

	if v.Get("response_type") != "code" ||
		!oauthPKCEShape(v.Get("code_challenge"), oauthChallengeChars) ||
		v.Get("code_challenge_method") != "S256" {
		return oauthErrorPage(c)
	}
	resource := base + mcpPath
	// An empty resource is the same as none.
	for _, res := range v["resource"] {
		if res != "" && res != resource {
			return oauthErrorPage(c)
		}
	}

	page := oauthPage{ClientName: client.Name, Host: hostOf(redirect), Fields: oauthHiddenFields(v)}
	if !post {
		return renderOAuthPage(c, http.StatusOK, page)
	}
	switch v.Get("action") {
	case "deny":
		return oauthRedirect(c, redirect, v.Get("state"), base, "access_denied", "the operator declined")
	case "allow":
	default:
		return oauthErrorPage(c)
	}

	secret := strings.TrimSpace(v.Get("token"))
	token, ok, err := o.store.Authenticate(secret)
	if err != nil {
		log.Printf("outside assistants: %v", err)
		return c.String(http.StatusInternalServerError, "The token could not be checked.")
	}
	if !ok {
		log.Printf("outside assistants: OAuth sign-in refused for client %s: the pasted token is not valid", client.ID)
		page.Bad = true
		return renderOAuthPage(c, http.StatusUnauthorized, page)
	}
	if err := o.store.MarkOAuthClientAuthorized(client.ID, token.ID); err != nil {
		log.Printf("outside assistants: %v", err)
		return c.String(http.StatusInternalServerError, "The sign-in could not be saved.")
	}
	code, err := o.issueCode(&mcpOAuthCode{
		clientID: client.ID, redirectURI: redirect, challenge: v.Get("code_challenge"),
		resource: resource, secret: []byte(secret), tokenID: token.ID,
	})
	if err != nil {
		log.Printf("outside assistants: could not make an authorization code: %v", err)
		return c.String(http.StatusInternalServerError, "The sign-in could not be completed.")
	}
	o.store.QueueActivity(mcpActivity{At: o.now(), Client: token.Name, Kind: "request", Name: "sign-in from " + client.Name + " (" + page.Host + ")", Outcome: "ok"})
	return oauthRedirectWith(c, redirect, url.Values{"code": {code}, "state": {v.Get("state")}, "iss": {base}})
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

// oauthRedirect sends the operator's refusal back to the client (RFC 6749
// 4.1.2.1, with the RFC 9207 issuer).
func oauthRedirect(c echo.Context, redirect, state, issuer, code, desc string) error {
	return oauthRedirectWith(c, redirect, url.Values{"error": {code}, "error_description": {desc}, "state": {state}, "iss": {issuer}})
}

// oauthRedirectWith adds params to the registered redirect URI, keeping any
// query it already has, and drops the ones with no value.
func oauthRedirectWith(c echo.Context, redirect string, params url.Values) error {
	u, err := url.Parse(redirect)
	if err != nil {
		return oauthErrorPage(c)
	}
	q := u.Query()
	for k, vals := range params {
		if len(vals) > 0 && vals[0] != "" {
			q.Set(k, vals[0])
		}
	}
	u.RawQuery = q.Encode()
	return c.Redirect(http.StatusFound, u.String())
}

// issueCode stores an authorization code and returns it. Pending codes are
// capped: expired ones go first, then the oldest.
func (o *mcpOAuth) issueCode(rec *mcpOAuthCode) (string, error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", err
	}
	code := base64.RawURLEncoding.EncodeToString(raw)
	now := o.now()
	rec.created, rec.expires = now, now.Add(mcpOAuthCodeTTL)
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sweepLocked(now)
	for len(o.codes) >= mcpOAuthCodeCap {
		var oldestKey string
		for k, c := range o.codes {
			if oldestKey == "" || c.created.Before(o.codes[oldestKey].created) {
				oldestKey = k
			}
		}
		o.codes[oldestKey].wipe()
		delete(o.codes, oldestKey)
	}
	o.codes[hashMCPToken(code)] = rec
	// The clock may be a test's, so this only bounds how long an unredeemed
	// secret stays in memory when no other sign-in comes along.
	time.AfterFunc(mcpOAuthCodeTTL+time.Second, func() {
		o.mu.Lock()
		defer o.mu.Unlock()
		o.sweepLocked(o.now())
	})
	return code, nil
}

func (o *mcpOAuth) sweepLocked(now time.Time) {
	for k, c := range o.codes {
		if !now.Before(c.expires) {
			c.wipe()
			delete(o.codes, k)
		}
	}
}

// takeCode removes a code and returns it, or nil when it is unknown or has
// expired. It is single use even if the caller then refuses the exchange.
func (o *mcpOAuth) takeCode(code string) *mcpOAuthCode {
	o.mu.Lock()
	defer o.mu.Unlock()
	now := o.now()
	o.sweepLocked(now)
	key := hashMCPToken(code)
	rec := o.codes[key]
	delete(o.codes, key)
	return rec
}

// pending is how many codes are held; the tests use it to check the cap.
func (o *mcpOAuth) pending() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.codes)
}

// handleToken exchanges an authorization code for the access token, which is
// the outside-assistant token the operator pasted.
func (o *mcpOAuth) handleToken(c echo.Context) error {
	h := c.Response().Header()
	h.Set("Cache-Control", "no-store")
	h.Set("Pragma", "no-cache")
	r := c.Request()
	r.Body = http.MaxBytesReader(c.Response(), r.Body, mcpOAuthBodyLimit)
	if err := r.ParseForm(); err != nil {
		return oauthJSONError(c, http.StatusBadRequest, "invalid_request", "the request body could not be read")
	}
	v := r.PostForm
	if v.Get("grant_type") != "authorization_code" {
		return oauthJSONError(c, http.StatusBadRequest, "unsupported_grant_type", "only authorization_code is supported")
	}
	code := o.takeCode(v.Get("code"))
	if code == nil {
		return oauthJSONError(c, http.StatusBadRequest, "invalid_grant", "the code is not valid or has expired")
	}
	defer code.wipe()
	grantFail := func(desc string) error { return oauthJSONError(c, http.StatusBadRequest, "invalid_grant", desc) }
	if v.Get("client_id") != code.clientID {
		return grantFail("the code was issued to a different client")
	}
	if v.Get("redirect_uri") != code.redirectURI {
		return grantFail("redirect_uri does not match the sign-in")
	}
	verifier := v.Get("code_verifier")
	if !oauthPKCEShape(verifier, oauthVerifierChars) {
		return grantFail("a code_verifier of 43 to 128 characters is required")
	}
	sum := sha256.Sum256([]byte(verifier))
	if subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(code.challenge)) != 1 {
		return grantFail("code_verifier does not match the code_challenge")
	}
	for _, res := range v["resource"] {
		if res != "" && res != code.resource {
			return oauthJSONError(c, http.StatusBadRequest, "invalid_target", "resource must be "+code.resource)
		}
	}
	token, ok, err := o.store.Authenticate(string(code.secret))
	if err != nil {
		log.Printf("outside assistants: %v", err)
		return oauthJSONError(c, http.StatusInternalServerError, "server_error", "the token could not be checked")
	}
	if !ok || token.ID != code.tokenID {
		return grantFail("the token behind this sign-in has been revoked")
	}
	return c.JSON(http.StatusOK, map[string]string{"access_token": string(code.secret), "token_type": "Bearer"})
}

type oauthField struct{ Name, Value string }

type oauthPage struct {
	ClientName string
	Host       string
	Fields     []oauthField
	Bad        bool
}

// oauthHiddenFields carries the authorization request through the form, in a
// fixed order, leaving out the fields the request did not have.
func oauthHiddenFields(v url.Values) []oauthField {
	var out []oauthField
	for _, k := range []string{"response_type", "client_id", "redirect_uri", "code_challenge", "code_challenge_method", "state", "resource", "scope"} {
		if val := v.Get(k); val != "" {
			out = append(out, oauthField{k, val})
		}
	}
	return out
}

func renderOAuthPage(c echo.Context, status int, p oauthPage) error {
	c.Response().Header().Set("Content-Type", "text/html; charset=utf-8")
	c.Response().WriteHeader(status)
	return oauthSignInTemplate.Execute(c.Response(), p)
}

func oauthErrorPage(c echo.Context) error {
	c.Response().Header().Set("Content-Type", "text/html; charset=utf-8")
	c.Response().WriteHeader(http.StatusBadRequest)
	return oauthErrorTemplate.Execute(c.Response(), nil)
}

const oauthPageHead = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark">
<title>{{.Title}}</title>
<style>
:root{--bg:#f5f6f8;--card:#fff;--fg:#14181f;--muted:#4b5563;--line:#d3d8e0;--accent:#1d4ed8;--accent-fg:#fff;--bad:#b42318}
@media (prefers-color-scheme:dark){:root{--bg:#0e1116;--card:#171b22;--fg:#e8ebf0;--muted:#a3acb9;--line:#2c333d;--accent:#6b9bff;--accent-fg:#0e1116;--bad:#ff8a80}}
*{box-sizing:border-box}
body{margin:0;padding:16px;background:var(--bg);color:var(--fg);font:16px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif}
main{max-width:30rem;margin:0 auto;padding:20px;background:var(--card);border:1px solid var(--line);border-radius:12px}
h1{margin:0 0 12px;font-size:1.35rem;line-height:1.25;overflow-wrap:anywhere}
p{margin:0 0 12px;overflow-wrap:anywhere}
.muted{color:var(--muted)}
.bad{color:var(--bad);font-weight:600}
label{display:block;margin:16px 0 4px;font-weight:600}
input[type=password]{width:100%;padding:10px 12px;font:inherit;color:var(--fg);background:var(--bg);border:1px solid var(--line);border-radius:8px}
.row{display:flex;gap:12px;margin-top:16px}
button{flex:1;padding:12px;font:inherit;font-weight:600;border-radius:8px;border:1px solid var(--line);background:transparent;color:var(--fg);cursor:pointer}
button.allow{background:var(--accent);border-color:var(--accent);color:var(--accent-fg)}
button:focus-visible,input:focus-visible{outline:3px solid var(--accent);outline-offset:2px}
</style>
</head>
<body>
<main>
`

var oauthSignInTemplate = template.Must(template.New("signin").Parse(strings.Replace(oauthPageHead, "{{.Title}}", "Sign in to Helmcentral", 1) + `<h1>Let {{.ClientName}} read the boat</h1>
<p>{{.ClientName}}, at {{.Host}}, asks to read the boat through Helmcentral. To allow it, paste an outside assistant token below.</p>
<p class="muted">Make one in Helmcentral under Settings, Outside assistants, and name it after this app. Then you can revoke it on its own. Only go on if you started this from that app.</p>
{{if .Bad}}<p class="bad" role="alert">That token isn't valid. It may have been revoked. Make a new one in Settings, Outside assistants.</p>
{{end}}<form method="post" action="` + oauthAuthorizePath + `">
{{range .Fields}}<input type="hidden" name="{{.Name}}" value="{{.Value}}">
{{end}}<label for="token">Token</label>
<input id="token" type="password" name="token" autocomplete="off" autocapitalize="off" spellcheck="false" required>
<div class="row">
<button class="allow" type="submit" name="action" value="allow">Allow</button>
<button type="submit" name="action" value="deny" formnovalidate>Cancel</button>
</div>
</form>
</main>
</body>
</html>
`))

var oauthErrorTemplate = template.Must(template.New("error").Parse(strings.Replace(oauthPageHead, "{{.Title}}", "Sign-in link not valid", 1) + `<h1>This sign-in link isn't valid</h1>
<p>Start again from the app you were connecting. If it keeps happening, remove the connector there and add it again.</p>
</main>
</body>
</html>
`))
