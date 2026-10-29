package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// Tailscale Funnel adds Tailscale-Funnel-Request to everything it forwards and
// deletes any value a visitor sends, so the header cannot be forged or
// stripped from outside. The guard (funnel_guard.go) lets only /mcp through.

type funnelGuardEnv struct {
	e     *echo.Echo
	logs  []string
	clock time.Time
}

func newFunnelGuardEnv() *funnelGuardEnv {
	env := &funnelGuardEnv{clock: time.Date(2026, 10, 30, 2, 0, 0, 0, time.UTC)}
	g := &funnelGuard{
		now:  func() time.Time { return env.clock },
		logf: func(format string, args ...any) { env.logs = append(env.logs, fmt.Sprintf(format, args...)) },
	}
	env.e = echo.New()
	env.e.Pre(g.middleware())
	reached := func(c echo.Context) error { return c.String(http.StatusOK, "reached "+c.Request().URL.Path) }
	env.e.Any("/mcp", reached)
	env.e.Any("/mcp/", reached)
	env.e.GET("/api/stream", reached)
	env.e.GET("/", reached)
	env.e.GET("/mcpx", reached)
	return env
}

func (env *funnelGuardEnv) do(method, target string, viaFunnel bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	if viaFunnel {
		req.Header.Set("Tailscale-Funnel-Request", "?1")
		req.Header.Set("X-Forwarded-For", "203.0.113.9")
	}
	rec := httptest.NewRecorder()
	env.e.ServeHTTP(rec, req)
	return rec
}

func TestFunnelGuard_RequestsWithoutTheHeaderAreUntouched(t *testing.T) {
	env := newFunnelGuardEnv()
	for _, p := range []string{"/", "/api/stream", "/mcp", "/mcpx"} {
		if rec := env.do(http.MethodGet, p, false); rec.Code != http.StatusOK {
			t.Fatalf("%s without the header: %d", p, rec.Code)
		}
	}
	if len(env.logs) != 0 {
		t.Fatalf("nothing should be logged: %v", env.logs)
	}
}

func TestFunnelGuard_OnlyMCPPassesThroughFunnel(t *testing.T) {
	env := newFunnelGuardEnv()
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		if rec := env.do(method, "/mcp", true); rec.Code != http.StatusOK {
			t.Fatalf("%s /mcp through Funnel: %d", method, rec.Code)
		}
	}
	// A query string does not change the path.
	if rec := env.do(http.MethodPost, "/mcp?x=1", true); rec.Code != http.StatusOK {
		t.Fatalf("/mcp?x=1 through Funnel: %d", rec.Code)
	}
	for _, p := range []string{"/", "/api/stream", "/api/outside-assistants", "/mcp/", "/mcpx", "/mcp/../api/stream", "/MCP", "/index.html"} {
		rec := env.do(http.MethodGet, p, true)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s through Funnel: want 404, got %d", p, rec.Code)
		}
		if body := rec.Body.String(); body != "404 page not found\n" {
			t.Fatalf("%s: the refusal should look like Tailscale's own 404, got %q", p, body)
		}
	}
}

func TestFunnelGuard_AnyValueOfTheHeaderCountsAsFunnel(t *testing.T) {
	env := newFunnelGuardEnv()
	req := httptest.NewRequest(http.MethodGet, "/api/stream", nil)
	req.Header.Set("Tailscale-Funnel-Request", "")
	rec := httptest.NewRecorder()
	env.e.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("an empty header value must still be treated as Funnel: %d", rec.Code)
	}
}

func TestFunnelGuard_RefusalsAreLoggedAtMostOncePerMinute(t *testing.T) {
	env := newFunnelGuardEnv()
	env.do(http.MethodGet, "/api/stream", true)
	env.do(http.MethodGet, "/", true)
	if len(env.logs) != 1 {
		t.Fatalf("want one log line, got %v", env.logs)
	}
	for _, want := range []string{"GET", "/api/stream", "203.0.113.9"} {
		if !strings.Contains(env.logs[0], want) {
			t.Fatalf("log line %q should name %q", env.logs[0], want)
		}
	}
	env.clock = env.clock.Add(59 * time.Second)
	env.do(http.MethodGet, "/", true)
	if len(env.logs) != 1 {
		t.Fatalf("still inside the minute: %v", env.logs)
	}
	env.clock = env.clock.Add(2 * time.Second)
	env.do(http.MethodPost, "/api/x", true)
	if len(env.logs) != 2 {
		t.Fatalf("a minute on, a second line is due: %v", env.logs)
	}
	if !strings.Contains(env.logs[1], "2 more") {
		t.Fatalf("the line should say how many refusals were left out since the last one: %q", env.logs[1])
	}
}

func TestFunnelGuard_MCPThroughFunnelStillNeedsAToken(t *testing.T) {
	store := newTestMCPStore(t)
	deps := contextTestDeps(t)
	svc := newMCPService(store, func() assistantToolDeps { return deps })
	e := echo.New()
	e.Pre(newFunnelGuard().middleware())
	e.Any("/mcp", svc.Handle)

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(mcpListBody))
	req.Header.Set("Tailscale-Funnel-Request", "?1")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("/mcp through Funnel with no token: want 401, got %d", rec.Code)
	}
}
