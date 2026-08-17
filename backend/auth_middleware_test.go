package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func noopOKHandler(c echo.Context) error { return c.JSON(http.StatusOK, map[string]bool{"ok": true}) }

// setupAuthMiddlewareTest points SETTINGS_FILE at a fresh temp settings.yaml
// with auth.mode set as requested, and clears any HELMCENTRAL_AUTH_MODE
// override left over from another test.
func setupAuthMiddlewareTest(t *testing.T, mode string) {
	t.Helper()
	settingsPath := writeAuthModeSettingsFixture(t, mode)
	t.Setenv("SETTINGS_FILE", settingsPath)
	t.Setenv("HELMCENTRAL_AUTH_MODE", "")
}

func mustSessionToken(t *testing.T, sessions *sessionStore, role string) string {
	t.Helper()
	if role == "" {
		return ""
	}
	token, err := sessions.Create("tester", role)
	if err != nil {
		t.Fatalf("sessions.Create: %v", err)
	}
	return token
}

// ── role ranking ──────────────────────────────────────────────────────────────

func TestRoleSatisfies_OrdersReadonlyBelowReadwriteBelowAdmin(t *testing.T) {
	cases := []struct {
		role, required string
		want           bool
	}{
		{roleReadonly, roleReadonly, true},
		{roleReadonly, roleReadwrite, false},
		{roleReadonly, roleAdmin, false},
		{roleReadwrite, roleReadonly, true},
		{roleReadwrite, roleReadwrite, true},
		{roleReadwrite, roleAdmin, false},
		{roleAdmin, roleReadonly, true},
		{roleAdmin, roleReadwrite, true},
		{roleAdmin, roleAdmin, true},
	}
	for _, tc := range cases {
		got := roleSatisfies(tc.role, tc.required)
		if got != tc.want {
			t.Errorf("roleSatisfies(%q, %q) = %v, want %v", tc.role, tc.required, got, tc.want)
		}
	}
}

// ── requireRole / tier behaviour, mode=signalk ───────────────────────────────

func TestRequireRole_TableOverTiersAndRoles(t *testing.T) {
	setupAuthMiddlewareTest(t, "signalk")
	sessions := newTestSessionStore(t)

	e := echo.New()
	routes := []apiRoute{
		{Method: http.MethodGet, Path: "/api/test/public", Tier: tierPublic, Handler: noopOKHandler},
		{Method: http.MethodGet, Path: "/api/test/read", Tier: tierRead, Handler: noopOKHandler},
		{Method: http.MethodPost, Path: "/api/test/write", Tier: tierWrite, Handler: noopOKHandler},
		{Method: http.MethodGet, Path: "/api/test/admin", Tier: tierAdmin, Handler: noopOKHandler},
	}
	registerAPIRoutes(e, sessions, routes)

	cases := []struct {
		path, method, role string
		want               int
	}{
		{"/api/test/public", http.MethodGet, "", http.StatusOK},
		{"/api/test/public", http.MethodGet, roleReadonly, http.StatusOK},

		{"/api/test/read", http.MethodGet, "", http.StatusUnauthorized},
		{"/api/test/read", http.MethodGet, roleReadonly, http.StatusOK},
		{"/api/test/read", http.MethodGet, roleReadwrite, http.StatusOK},
		{"/api/test/read", http.MethodGet, roleAdmin, http.StatusOK},

		{"/api/test/write", http.MethodPost, "", http.StatusUnauthorized},
		{"/api/test/write", http.MethodPost, roleReadonly, http.StatusForbidden},
		{"/api/test/write", http.MethodPost, roleReadwrite, http.StatusOK},
		{"/api/test/write", http.MethodPost, roleAdmin, http.StatusOK},

		{"/api/test/admin", http.MethodGet, "", http.StatusUnauthorized},
		{"/api/test/admin", http.MethodGet, roleReadonly, http.StatusForbidden},
		{"/api/test/admin", http.MethodGet, roleReadwrite, http.StatusForbidden},
		{"/api/test/admin", http.MethodGet, roleAdmin, http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s_%s_role=%s", tc.method, tc.path, tc.role), func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if token := mustSessionToken(t, sessions, tc.role); token != "" {
				req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("expected status %d, got %d: %s", tc.want, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestRequireRole_InvalidOrExpiredSessionIsUnauthorized(t *testing.T) {
	setupAuthMiddlewareTest(t, "signalk")
	sessions := newTestSessionStore(t)

	e := echo.New()
	registerAPIRoutes(e, sessions, []apiRoute{
		{Method: http.MethodGet, Path: "/api/test/read", Tier: tierRead, Handler: noopOKHandler},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/test/read", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "not-a-real-token"})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for an unrecognised session cookie, got %d", rec.Code)
	}
}

// ── mode=none passthrough ─────────────────────────────────────────────────────

// TestRequireRole_ModeNoneAllowsEveryRequestRegardlessOfTier proves the
// default (unconfigured) release behaviour: with no auth.mode set,
// Helmcentral runs exactly as it did before this change — every tier is
// open, including admin — because auth.mode defaults to "none" so upgrading
// an existing install can never lock its operator out.
func TestRequireRole_ModeNoneAllowsEveryRequestRegardlessOfTier(t *testing.T) {
	setupAuthMiddlewareTest(t, "") // unset -> defaults to "none"
	sessions := newTestSessionStore(t)

	e := echo.New()
	registerAPIRoutes(e, sessions, []apiRoute{
		{Method: http.MethodGet, Path: "/api/test/admin", Tier: tierAdmin, Handler: noopOKHandler},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/test/admin", nil) // no cookie at all
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected mode=none to allow an unauthenticated admin-tier request, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ── route table coverage ─────────────────────────────────────────────────────

// TestAPIRouteCoverage_EveryRegisteredAPIRouteHasATier is the point of the
// whole exercise: it builds and registers the REAL production route table
// (buildAPIRoutes, the same one main() uses) and then walks every route
// Echo actually has registered. Any /api route present in Echo's router but
// absent from the tier registry means it was added outside
// buildAPIRoutes/registerAPIRoutes — bypassing the one place a tier gets
// assigned — and must fail the build, not ship silently open.
func TestAPIRouteCoverage_EveryRegisteredAPIRouteHasATier(t *testing.T) {
	sessions := newTestSessionStore(t)

	e := echo.New()
	routes := buildAPIRoutes(sessions, newWorldImageryHTTPClient())
	registry := registerAPIRoutes(e, sessions, routes)
	registerStaticHandler(e)

	if len(registry) == 0 {
		t.Fatal("expected the production route table to be non-empty")
	}

	uncovered := 0
	for _, route := range e.Routes() {
		if !strings.HasPrefix(route.Path, "/api/") {
			continue // the embedded SPA's GET /* and any non-API route
		}
		if _, ok := registry[route.Method+" "+route.Path]; !ok {
			t.Errorf("route %s %s is registered on the server but not covered by any auth tier", route.Method, route.Path)
			uncovered++
		}
	}
	if uncovered > 0 {
		t.Fatalf("%d /api route(s) registered without an auth tier — add them to buildAPIRoutes in main.go", uncovered)
	}
}

// TestAPIRouteCoverage_NoDuplicateMethodPathEntries guards against a copy-paste
// error in the route table silently shadowing an earlier tier assignment.
func TestAPIRouteCoverage_NoDuplicateMethodPathEntries(t *testing.T) {
	sessions := newTestSessionStore(t)
	routes := buildAPIRoutes(sessions, newWorldImageryHTTPClient())

	seen := make(map[string]bool, len(routes))
	for _, route := range routes {
		key := route.Method + " " + route.Path
		if seen[key] {
			t.Errorf("duplicate route table entry for %s", key)
		}
		seen[key] = true
	}
}

// TestAPIRouteCoverage_EveryProductionRouteHasAKnownTier guards against a
// route entry with a zero-value/unrecognised Tier slipping through (e.g. a
// struct literal that forgot to set Tier), which registerAPIRoutes would
// otherwise register as tierPublic (the zero value) — the most permissive
// tier, exactly the wrong direction to fail silently toward.
func TestAPIRouteCoverage_EveryProductionRouteHasAnExplicitTier(t *testing.T) {
	sessions := newTestSessionStore(t)
	routes := buildAPIRoutes(sessions, newWorldImageryHTTPClient())

	for _, route := range routes {
		switch route.Tier {
		case tierPublic, tierRead, tierWrite, tierAdmin:
			// ok
		default:
			t.Errorf("route %s %s has an unrecognised tier value %v", route.Method, route.Path, route.Tier)
		}
	}
}
