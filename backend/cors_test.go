package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestCorsOwnOrigin_HTTPRequestReportsHTTPScheme(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://192.168.1.50:8080/api/health", nil)
	req.Host = "192.168.1.50:8080"

	got := corsOwnOrigin(req)
	want := "http://192.168.1.50:8080"
	if got != want {
		t.Fatalf("corsOwnOrigin = %q, want %q", got, want)
	}
}

func TestCorsOwnOrigin_ForwardedProtoHTTPSIsHonoured(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://helmcentral.example/api/health", nil)
	req.Host = "helmcentral.example"
	req.Header.Set("X-Forwarded-Proto", "https")

	got := corsOwnOrigin(req)
	want := "https://helmcentral.example"
	if got != want {
		t.Fatalf("corsOwnOrigin = %q, want %q", got, want)
	}
}

func TestCorsExplicitAllowedOrigins_ParsesCommaSeparatedList(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://a.example, https://b.example ,https://c.example")

	got := corsExplicitAllowedOrigins()
	want := []string{"https://a.example", "https://b.example", "https://c.example"}
	if len(got) != len(want) {
		t.Fatalf("expected %d origins, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("origin[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCorsExplicitAllowedOrigins_EmptyEnvYieldsNoExtraOrigins(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "")
	if got := corsExplicitAllowedOrigins(); len(got) != 0 {
		t.Fatalf("expected no explicit origins, got %v", got)
	}
}

// TestCorsMiddleware_ReflectsOwnOriginNotWildcard is the regression this
// whole file exists for: AllowOrigins: []string{"*"} together with
// AllowCredentials is rejected by every browser and was half the README's
// security warning (docs/adr/0040). The replacement must echo back the
// server's own origin — not "*" — when credentials are involved.
func TestCorsMiddleware_ReflectsOwnOriginNotWildcard(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "")

	e := echo.New()
	e.Use(corsMiddleware())
	e.GET("/api/health", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Host = "192.168.1.50:8080"
	req.Header.Set("Origin", "http://192.168.1.50:8080")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	got := rec.Header().Get("Access-Control-Allow-Origin")
	if got != "http://192.168.1.50:8080" {
		t.Fatalf("expected Access-Control-Allow-Origin to reflect the server's own origin, got %q", got)
	}
	if got == "*" {
		t.Fatal("expected the wildcard to be gone")
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("expected Access-Control-Allow-Credentials: true, since the session cookie needs it")
	}
}

func TestCorsMiddleware_RejectsOriginNotInAllowlist(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "")

	e := echo.New()
	e.Use(corsMiddleware())
	e.GET("/api/health", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Host = "192.168.1.50:8080"
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no Access-Control-Allow-Origin for an origin outside the allowlist, got %q", got)
	}
}

func TestCorsMiddleware_HonoursExplicitAllowedOrigins(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://dashboard.example")

	e := echo.New()
	e.Use(corsMiddleware())
	e.GET("/api/health", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Host = "192.168.1.50:8080"
	req.Header.Set("Origin", "https://dashboard.example")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://dashboard.example" {
		t.Fatalf("expected the explicitly configured origin to be allowed, got %q", got)
	}
}
