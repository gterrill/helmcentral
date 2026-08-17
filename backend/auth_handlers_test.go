package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func setupAuthHandlersTest(t *testing.T) {
	t.Helper()
	invalidateSignalKToken()
	t.Cleanup(invalidateSignalKToken)
}

func newAuthRequest(t *testing.T, method, path string, body any) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	return newAuthRequestWithCookie(t, method, path, body, "")
}

func newAuthRequestWithCookie(t *testing.T, method, path string, body any, cookieValue string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	e := echo.New()

	var req *http.Request
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal request body: %v", err)
		}
		req = httptest.NewRequest(method, path, strings.NewReader(string(payload)))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if cookieValue != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookieValue})
	}

	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

func setCookieFromResponse(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == sessionCookieName {
			return ck
		}
	}
	t.Fatalf("expected a %s cookie in the response, got none", sessionCookieName)
	return nil
}

// ── resolveRoleFromUserLevel ─────────────────────────────────────────────────

func TestResolveRoleFromUserLevel_KnownValuesMapToTheExpectedRole(t *testing.T) {
	cases := map[string]string{
		"admin":     roleAdmin,
		"readonly":  roleReadonly,
		"readwrite": roleReadwrite,
	}
	for userLevel, want := range cases {
		got, err := resolveRoleFromUserLevel(userLevel)
		if err != nil {
			t.Fatalf("resolveRoleFromUserLevel(%q): unexpected error: %v", userLevel, err)
		}
		if got != want {
			t.Fatalf("resolveRoleFromUserLevel(%q) = %q, want %q", userLevel, got, want)
		}
	}
}

// TestResolveRoleFromUserLevel_UnknownValueFailsClosed is the load-bearing
// test for the plan's fail-closed requirement: an unrecognised userLevel
// string must be rejected outright, never defaulted to a permissive (or any)
// tier.
func TestResolveRoleFromUserLevel_UnknownValueFailsClosed(t *testing.T) {
	_, err := resolveRoleFromUserLevel("superuser")
	if err == nil {
		t.Fatal("expected an unrecognised userLevel to be rejected, got nil error")
	}
	if !strings.Contains(err.Error(), "superuser") {
		t.Fatalf("expected the error to name the unexpected value, got: %v", err)
	}
}

func TestResolveRoleFromUserLevel_EmptyValueFailsClosed(t *testing.T) {
	_, err := resolveRoleFromUserLevel("")
	if err == nil {
		t.Fatal("expected an empty userLevel to be rejected, got nil error")
	}
}

// ── POST /api/auth/login ─────────────────────────────────────────────────────

func TestLoginHandler_SuccessCallsLoginThenLoginStatusAndSetsCookie(t *testing.T) {
	setupAuthHandlersTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	rs.on(http.MethodPost, "/signalk/v1/auth/login", http.StatusOK, `{"token":"user-jwt-abc","timeToLive":86400}`)
	rs.on(http.MethodGet, "/skServer/loginStatus", http.StatusOK, `{"status":"loggedIn","username":"skipper","userLevel":"readwrite"}`)

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	sessions := newTestSessionStore(t)

	c, rec := newAuthRequest(t, http.MethodPost, "/api/auth/login", map[string]string{"username": "skipper", "password": "hunter2"})

	if err := loginHandler(sessions)(c); err != nil {
		t.Fatalf("loginHandler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("could not parse response body: %v", err)
	}
	if payload["authenticated"] != true {
		t.Fatalf("expected authenticated=true, got %+v", payload)
	}
	if payload["username"] != "skipper" {
		t.Fatalf("expected username=skipper, got %+v", payload)
	}
	if payload["role"] != roleReadwrite {
		t.Fatalf("expected role=%s, got %+v", roleReadwrite, payload)
	}

	cookie := setCookieFromResponse(t, rec)
	if !cookie.HttpOnly {
		t.Fatal("expected the session cookie to be HttpOnly")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("expected SameSite=Lax, got %v", cookie.SameSite)
	}
	if cookie.Path != "/" {
		t.Fatalf("expected Path=/, got %q", cookie.Path)
	}
	if cookie.Secure {
		t.Fatal("expected Secure=false for a plain-HTTP request (a boat LAN is not TLS)")
	}

	sessionRec, err := sessions.Validate(cookie.Value)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if sessionRec == nil {
		t.Fatal("expected the cookie value to be a usable session token")
	}
	if sessionRec.Role != roleReadwrite {
		t.Fatalf("expected the stored session role to be %s, got %s", roleReadwrite, sessionRec.Role)
	}

	calls := rs.calls()
	if len(calls) != 2 {
		t.Fatalf("expected exactly 2 upstream calls (login then loginStatus), got %d: %+v", len(calls), calls)
	}
	if calls[0].Path != "/signalk/v1/auth/login" {
		t.Fatalf("expected the first call to be the login endpoint, got %q", calls[0].Path)
	}
	if calls[1].Path != "/skServer/loginStatus" {
		t.Fatalf("expected the second call to be loginStatus, got %q", calls[1].Path)
	}

	// Critical regression guard: a user login must never populate the
	// service account's token cache. The two identities are unrelated -
	// mixing them here would mean a user's JWT could end up authorizing
	// Helmcentral's own outbound writes, or vice versa.
	if skTokenCache != nil {
		t.Fatal("expected user login to leave the service-account token cache (skTokenCache) untouched")
	}
}

func TestLoginHandler_BadCredentialsReturns401WithSKMessageSurfaced(t *testing.T) {
	setupAuthHandlersTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	rs.on(http.MethodPost, "/signalk/v1/auth/login", http.StatusUnauthorized, `{"message":"invalid username or password"}`)

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	sessions := newTestSessionStore(t)
	c, rec := newAuthRequest(t, http.MethodPost, "/api/auth/login", map[string]string{"username": "skipper", "password": "wrong"})

	if err := loginHandler(sessions)(c); err != nil {
		t.Fatalf("loginHandler: %v", err)
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid username or password") {
		t.Fatalf("expected SignalK's own rejection message to be surfaced verbatim, got: %s", rec.Body.String())
	}
}

// TestLoginHandler_SignalKUnreachableSurfacesNotSwallowed is the fallback
// policy test: an unreachable SignalK must not be silently treated as "not
// logged in" or, worse, permitted through - it must surface as an explicit
// error distinct from a credentials rejection.
func TestLoginHandler_SignalKUnreachableSurfacesNotSwallowed(t *testing.T) {
	setupAuthHandlersTest(t)

	unreachable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	unreachableURL := unreachable.URL
	unreachable.Close() // closed before use: guarantees connection refused

	settingsPath := settingsFileForServer(t, unreachableURL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	sessions := newTestSessionStore(t)
	c, rec := newAuthRequest(t, http.MethodPost, "/api/auth/login", map[string]string{"username": "skipper", "password": "hunter2"})

	if err := loginHandler(sessions)(c); err != nil {
		t.Fatalf("loginHandler: %v", err)
	}
	if rec.Code == http.StatusOK {
		t.Fatalf("expected an unreachable SignalK to fail the login, got 200: %s", rec.Body.String())
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 for an unreachable upstream, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "error") {
		t.Fatalf("expected an explicit error field, got: %s", body)
	}
}

func TestLoginHandler_UnknownUserLevelRejectsLoginRatherThanDefaulting(t *testing.T) {
	setupAuthHandlersTest(t)
	srv, rs := newRecordingServer(t)
	defer srv.Close()

	rs.on(http.MethodPost, "/signalk/v1/auth/login", http.StatusOK, `{"token":"user-jwt-abc"}`)
	rs.on(http.MethodGet, "/skServer/loginStatus", http.StatusOK, `{"status":"loggedIn","username":"skipper","userLevel":"superadmin"}`)

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SETTINGS_FILE", settingsPath)

	sessions := newTestSessionStore(t)
	c, rec := newAuthRequest(t, http.MethodPost, "/api/auth/login", map[string]string{"username": "skipper", "password": "hunter2"})

	if err := loginHandler(sessions)(c); err != nil {
		t.Fatalf("loginHandler: %v", err)
	}
	if rec.Code == http.StatusOK {
		t.Fatalf("expected an unrecognised userLevel to reject the login, got 200: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "superadmin") {
		t.Fatalf("expected the error to name the unexpected userLevel, got: %s", rec.Body.String())
	}

	// No session must have been created for a rejected login.
	if len(rs.calls()) != 2 {
		t.Fatalf("expected exactly 2 upstream calls, got %d", len(rs.calls()))
	}
}

func TestLoginHandler_MissingCredentialsReturns400(t *testing.T) {
	setupAuthHandlersTest(t)
	sessions := newTestSessionStore(t)
	c, rec := newAuthRequest(t, http.MethodPost, "/api/auth/login", map[string]string{"username": "", "password": ""})

	if err := loginHandler(sessions)(c); err != nil {
		t.Fatalf("loginHandler: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing credentials, got %d", rec.Code)
	}
}

// ── POST /api/auth/logout ────────────────────────────────────────────────────

func TestLogoutHandler_InvalidatesSessionAndClearsCookie(t *testing.T) {
	setupAuthHandlersTest(t)
	sessions := newTestSessionStore(t)

	token, err := sessions.Create("skipper", roleReadwrite)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	c, rec := newAuthRequestWithCookie(t, http.MethodPost, "/api/auth/logout", nil, token)
	if err := logoutHandler(sessions)(c); err != nil {
		t.Fatalf("logoutHandler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rec2, err := sessions.Validate(token)
	if err != nil {
		t.Fatalf("Validate after logout: %v", err)
	}
	if rec2 != nil {
		t.Fatal("expected the session to be invalidated after logout")
	}

	cookie := setCookieFromResponse(t, rec)
	if cookie.MaxAge > 0 {
		t.Fatalf("expected the logout cookie to expire immediately (MaxAge<=0), got %d", cookie.MaxAge)
	}
}

func TestLogoutHandler_NoCookiePresentStillSucceeds(t *testing.T) {
	setupAuthHandlersTest(t)
	sessions := newTestSessionStore(t)

	c, rec := newAuthRequest(t, http.MethodPost, "/api/auth/logout", nil)
	if err := logoutHandler(sessions)(c); err != nil {
		t.Fatalf("logoutHandler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 even with no session cookie present, got %d", rec.Code)
	}
}

// ── GET /api/auth/me ──────────────────────────────────────────────────────────

func TestMeHandler_NoCookieReturnsUnauthenticatedNot401(t *testing.T) {
	setupAuthHandlersTest(t)
	sessions := newTestSessionStore(t)

	c, rec := newAuthRequest(t, http.MethodGet, "/api/auth/me", nil)
	if err := meHandler(sessions)(c); err != nil {
		t.Fatalf("meHandler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (not 401) so the SPA can decide what to render, got %d", rec.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("could not parse response: %v", err)
	}
	if payload["authenticated"] != false {
		t.Fatalf("expected authenticated=false, got %+v", payload)
	}
}

func TestMeHandler_ValidCookieReturnsIdentity(t *testing.T) {
	setupAuthHandlersTest(t)
	sessions := newTestSessionStore(t)

	token, err := sessions.Create("skipper", roleAdmin)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	c, rec := newAuthRequestWithCookie(t, http.MethodGet, "/api/auth/me", nil, token)
	if err := meHandler(sessions)(c); err != nil {
		t.Fatalf("meHandler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("could not parse response: %v", err)
	}
	if payload["authenticated"] != true {
		t.Fatalf("expected authenticated=true, got %+v", payload)
	}
	if payload["username"] != "skipper" {
		t.Fatalf("expected username=skipper, got %+v", payload)
	}
	if payload["role"] != roleAdmin {
		t.Fatalf("expected role=%s, got %+v", roleAdmin, payload)
	}
}

func TestMeHandler_ExpiredCookieReturnsUnauthenticated(t *testing.T) {
	setupAuthHandlersTest(t)
	sessions := newTestSessionStore(t)

	c, rec := newAuthRequestWithCookie(t, http.MethodGet, "/api/auth/me", nil, "not-a-real-token")
	if err := meHandler(sessions)(c); err != nil {
		t.Fatalf("meHandler: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("could not parse response: %v", err)
	}
	if payload["authenticated"] != false {
		t.Fatalf("expected authenticated=false for an unrecognised cookie, got %+v", payload)
	}
}

// ── GET /api/auth/mode ────────────────────────────────────────────────────────

func TestAuthModeHandler_DefaultsToNoneWhenUnset(t *testing.T) {
	settingsPath := writeAuthModeSettingsFixture(t, "")
	t.Setenv("SETTINGS_FILE", settingsPath)
	t.Setenv("HELMCENTRAL_AUTH_MODE", "")

	c, rec := newAuthRequest(t, http.MethodGet, "/api/auth/mode", nil)
	if err := authModeHandler(c); err != nil {
		t.Fatalf("authModeHandler: %v", err)
	}

	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("could not parse response: %v", err)
	}
	if payload["mode"] != authModeNone {
		t.Fatalf("expected default mode %q, got %q", authModeNone, payload["mode"])
	}
}

func TestAuthModeHandler_ReadsSignalKModeFromSettings(t *testing.T) {
	settingsPath := writeAuthModeSettingsFixture(t, "signalk")
	t.Setenv("SETTINGS_FILE", settingsPath)
	t.Setenv("HELMCENTRAL_AUTH_MODE", "")

	c, rec := newAuthRequest(t, http.MethodGet, "/api/auth/mode", nil)
	if err := authModeHandler(c); err != nil {
		t.Fatalf("authModeHandler: %v", err)
	}

	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("could not parse response: %v", err)
	}
	if payload["mode"] != authModeSignalK {
		t.Fatalf("expected mode %q, got %q", authModeSignalK, payload["mode"])
	}
}

// TestAuthModeHandler_EnvOverrideWinsOverSettings covers the documented
// recovery hatch for a locked-out operator: HELMCENTRAL_AUTH_MODE=none must
// win even when settings.yaml says signalk, since the Settings page that
// would otherwise fix this sits behind admin.
func TestAuthModeHandler_EnvOverrideWinsOverSettings(t *testing.T) {
	settingsPath := writeAuthModeSettingsFixture(t, "signalk")
	t.Setenv("SETTINGS_FILE", settingsPath)
	t.Setenv("HELMCENTRAL_AUTH_MODE", "none")

	c, rec := newAuthRequest(t, http.MethodGet, "/api/auth/mode", nil)
	if err := authModeHandler(c); err != nil {
		t.Fatalf("authModeHandler: %v", err)
	}

	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("could not parse response: %v", err)
	}
	if payload["mode"] != authModeNone {
		t.Fatalf("expected env override to force mode=none, got %q", payload["mode"])
	}
}

func writeAuthModeSettingsFixture(t *testing.T, mode string) string {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/settings.yaml"
	content := "signalk:\n  address: localhost\n  port: 3000\n"
	if mode != "" {
		content += "auth:\n  mode: " + mode + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("could not write temp settings: %v", err)
	}
	return path
}
