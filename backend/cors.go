package main

import (
	"net/http"
	"os"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// corsOwnOrigin reports the origin (scheme://host[:port]) the browser used
// to reach this server on the current request — same-origin operation
// (the SPA embedded in this binary, or the Vite dev proxy) needs exactly
// this to be allowed, and nothing wider.
func corsOwnOrigin(req *http.Request) string {
	scheme := "http"
	if req.TLS != nil || strings.EqualFold(req.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + req.Host
}

// corsExplicitAllowedOrigins parses the optional, operator-set
// CORS_ALLOWED_ORIGINS (comma-separated) — for a deployment that
// legitimately needs a second, genuinely different origin to call the API
// with credentials (e.g. a separately hosted frontend). Empty/unset yields
// no extra origins, not a wildcard.
func corsExplicitAllowedOrigins() []string {
	raw := strings.TrimSpace(os.Getenv("CORS_ALLOWED_ORIGINS"))
	if raw == "" {
		return nil
	}
	var origins []string
	for _, o := range strings.Split(raw, ",") {
		o = strings.TrimSpace(o)
		if o != "" {
			origins = append(origins, o)
		}
	}
	return origins
}

// corsMiddleware replaces the previous AllowOrigins: []string{"*"}
// (docs/adr/0040, README's security warning): "*" combined with
// AllowCredentials is rejected by every browser, so the old config was
// simultaneously advertising an open API and not actually working for any
// credentialed cross-origin caller. The allowlist here is exactly the
// server's own origin (so the embedded SPA and the Vite dev proxy keep
// working unchanged) plus any explicit CORS_ALLOWED_ORIGINS.
//
// The allowlist is computed per-request (not once at startup) because the
// server's own origin depends on how the browser reached it — the LAN IP,
// a hostname, or localhost during development — which isn't knowable in
// advance.
func corsMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			allowed := append([]string{corsOwnOrigin(c.Request())}, corsExplicitAllowedOrigins()...)
			handler := middleware.CORSWithConfig(middleware.CORSConfig{
				AllowOrigins:     allowed,
				AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete},
				AllowCredentials: true,
			})(next)
			return handler(c)
		}
	}
}
