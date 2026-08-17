package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// sessionCookieName is the HttpOnly cookie carrying the session token. It is
// deliberately unrelated to any SignalK cookie — Helmcentral runs on its own
// origin, so an SK session cookie is never sent to it (docs/adr/0040).
const sessionCookieName = "hc_session"

const (
	authModeNone    = "none"
	authModeSignalK = "signalk"
)

// ── auth.mode resolution ─────────────────────────────────────────────────────

// resolveAuthMode resolves the operative auth.mode from settings.yaml.
//
// settings.yaml is the only source. There was previously a
// HELMCENTRAL_AUTH_MODE environment override as a lockout escape hatch, but it
// was both redundant and confusing: a locked-out operator can already edit
// settings.yaml (which is what setting the mode meant anyway), while a second
// source that silently outranked the Settings page made "I changed it and
// nothing happened" a plausible outcome during setup.
//
// The lockout it guarded against is now prevented rather than undone —
// validateSettingsChange refuses to save mode:signalk against a SignalK server
// whose security is switched off.
//
// Read per request rather than cached, so a change takes effect without a
// restart. Only the startup security probe is one-shot.
func resolveAuthMode(settingsPath string) string {
	mode := loadSettingString(settingsPath, []string{"auth", "mode"})
	if mode == "" {
		return authModeNone
	}
	return mode
}

func authModeHandler(c echo.Context) error {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	return c.JSON(http.StatusOK, map[string]string{"mode": resolveAuthMode(settingsPath)})
}

// ── role resolution ───────────────────────────────────────────────────────────

// skUserLevelToRole maps SignalK's /skServer/loginStatus userLevel field to
// Helmcentral's internal role tiers.
//
// "admin" and "readonly" are documented SignalK/signalk-server values.
// "readwrite" is NOT confirmed against a live server response — it is
// strongly implied (a three-tier ACL needs something between readonly and
// admin) but was never observed in this environment. This map is the single
// place that assumption lives, specifically so a future live-server check
// only has to touch this one line.
//
// Any userLevel not in this map is rejected by resolveRoleFromUserLevel
// rather than defaulted to a permissive tier — fail closed, per this
// project's auth policy.
var skUserLevelToRole = map[string]string{
	"admin":     roleAdmin,
	"readonly":  roleReadonly,
	"readwrite": roleReadwrite, // UNVERIFIED against a live SignalK server — see comment above.
}

// resolveRoleFromUserLevel converts a userLevel string into a Helmcentral
// role, failing closed on anything it doesn't recognise rather than guessing
// a permission tier.
func resolveRoleFromUserLevel(userLevel string) (string, error) {
	role, ok := skUserLevelToRole[userLevel]
	if !ok {
		return "", fmt.Errorf("signalk reported unrecognised userLevel %q — refusing to log in rather than guessing a permission tier", userLevel)
	}
	return role, nil
}

// ── SignalK delegated login (two-step: login, then loginStatus) ────────────
//
// This deliberately does NOT reuse acquireSignalKToken/skTokenCache
// (signalk.go) — those belong to Helmcentral's own service account (the
// identity that drives the delta-stream subscription, putSignalKValue
// writes, route activation, and alarm notifications.* writes). A user's
// login here must never populate, read, or invalidate that cache: mixing
// the two would let a user's JWT influence Helmcentral's own outbound
// writes, or vice versa. See docs/adr/0040.

// signalKAuthRejectedError distinguishes "SignalK answered but rejected the
// request" (bad credentials, or a loginStatus that isn't loggedIn) from a
// transport failure ("SignalK could not be reached at all"). The two need
// different HTTP statuses: a rejection is 401, unreachable is 502 — nothing
// to authenticate against.
type signalKAuthRejectedError struct {
	message string
}

func (e *signalKAuthRejectedError) Error() string { return e.message }

// signalKLoginWithCredentials POSTs the given credentials to
// {sk}/signalk/v1/auth/login and returns the JWT. Per SignalK/signalk-server
// issue #1336, this JWT does not carry the user's access level — only an
// APPROVED/PENDING/DENIED-style status — so it is not decoded locally; the
// caller must follow up with signalKLoginStatus to learn the role.
func signalKLoginWithCredentials(signalkURL, username, password string) (string, error) {
	url := strings.TrimRight(signalkURL, "/") + "/signalk/v1/auth/login"
	body, err := json.Marshal(map[string]string{"username": username, "password": password})
	if err != nil {
		return "", fmt.Errorf("signalk login: encode request: %w", err)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("could not reach signalk at %s: %w", signalkURL, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", &signalKAuthRejectedError{message: fmt.Sprintf("signalk rejected login: %s", strings.TrimSpace(string(respBody)))}
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil || result.Token == "" {
		return "", fmt.Errorf("signalk login: could not parse token response: %s", string(respBody))
	}
	return result.Token, nil
}

// signalKLoginStatus is the authoritative source of the logged-in user's
// access level: GET {sk}/skServer/loginStatus, called WITH the JWT from
// signalKLoginWithCredentials. It returns
// {"status":"loggedIn","username":"...","userLevel":"admin"|"readonly"|...}.
func signalKLoginStatus(signalkURL, token string) (string, error) {
	url := strings.TrimRight(signalkURL, "/") + "/skServer/loginStatus"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("signalk loginStatus: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not reach signalk loginStatus at %s: %w", signalkURL, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", &signalKAuthRejectedError{message: fmt.Sprintf("signalk loginStatus returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))}
	}

	var status struct {
		Status    string `json:"status"`
		UserLevel string `json:"userLevel"`
	}
	if err := json.Unmarshal(respBody, &status); err != nil {
		return "", fmt.Errorf("signalk loginStatus: could not parse response: %s", string(respBody))
	}
	if status.Status != "loggedIn" {
		return "", &signalKAuthRejectedError{message: fmt.Sprintf("signalk loginStatus reported status %q, expected loggedIn", status.Status)}
	}
	return status.UserLevel, nil
}

// writeSignalKAuthError answers a login-step failure with 401 when SignalK
// itself rejected the request, or 502 for a genuine transport failure —
// never swallowed into a generic success/failure.
func writeSignalKAuthError(c echo.Context, err error) error {
	var rejected *signalKAuthRejectedError
	if errors.As(err, &rejected) {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": rejected.message})
	}
	return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
}

// ── cookie helpers ────────────────────────────────────────────────────────────

// cookieShouldBeSecure reports whether the request arrived over TLS, either
// directly or via a reverse proxy that sets X-Forwarded-Proto (the README's
// recommended path for remote access). Setting Secure unconditionally would
// silently discard every cookie on the default plain-HTTP boat LAN.
func cookieShouldBeSecure(c echo.Context) bool {
	if c.Request().TLS != nil {
		return true
	}
	return strings.EqualFold(c.Request().Header.Get("X-Forwarded-Proto"), "https")
}

func setSessionCookie(c echo.Context, token string) {
	c.SetCookie(&http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   cookieShouldBeSecure(c),
		Expires:  time.Now().UTC().Add(sessionTTL),
	})
}

func clearSessionCookie(c echo.Context) {
	c.SetCookie(&http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   cookieShouldBeSecure(c),
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
}

// ── handlers ──────────────────────────────────────────────────────────────────

// loginHandler implements the delegated login flow (docs/adr/0040):
// forward credentials to SignalK, resolve the role from loginStatus (never
// from decoding the JWT — see signalKLoginStatus), and on success mint a
// Helmcentral session. SignalK's own SK JWT is discarded once loginStatus
// has answered — this codebase never stores or reuses it.
func loginHandler(sessions *sessionStore) echo.HandlerFunc {
	return func(c echo.Context) error {
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		}
		username := strings.TrimSpace(req.Username)
		if username == "" || req.Password == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "username and password are required"})
		}

		signalkURL, _ := resolveSignalKURL()

		token, err := signalKLoginWithCredentials(signalkURL, username, req.Password)
		if err != nil {
			return writeSignalKAuthError(c, err)
		}

		userLevel, err := signalKLoginStatus(signalkURL, token)
		if err != nil {
			return writeSignalKAuthError(c, err)
		}

		role, err := resolveRoleFromUserLevel(userLevel)
		if err != nil {
			return c.JSON(http.StatusForbidden, map[string]string{"error": err.Error()})
		}

		sessionToken, err := sessions.Create(username, role)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create session: " + err.Error()})
		}

		setSessionCookie(c, sessionToken)

		return c.JSON(http.StatusOK, map[string]any{
			"authenticated": true,
			"username":      username,
			"role":          role,
		})
	}
}

func logoutHandler(sessions *sessionStore) echo.HandlerFunc {
	return func(c echo.Context) error {
		cookie, err := c.Cookie(sessionCookieName)
		if err == nil && cookie.Value != "" {
			if delErr := sessions.Delete(cookie.Value); delErr != nil {
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": delErr.Error()})
			}
		}
		clearSessionCookie(c)
		return c.JSON(http.StatusOK, map[string]bool{"ok": true})
	}
}

// meHandler answers {authenticated:false} rather than 401 for "no session" —
// this is a public endpoint the SPA polls to decide what to render, not a
// gate.
func meHandler(sessions *sessionStore) echo.HandlerFunc {
	return func(c echo.Context) error {
		cookie, err := c.Cookie(sessionCookieName)
		if err != nil || cookie.Value == "" {
			return c.JSON(http.StatusOK, map[string]any{"authenticated": false})
		}

		rec, err := sessions.Validate(cookie.Value)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		if rec == nil {
			return c.JSON(http.StatusOK, map[string]any{"authenticated": false})
		}

		return c.JSON(http.StatusOK, map[string]any{
			"authenticated": true,
			"username":      rec.SKUsername,
			"role":          rec.Role,
		})
	}
}
