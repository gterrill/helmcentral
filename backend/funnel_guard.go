package main

import (
	"log"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

// Tailscale Funnel is the documented way to give Claude's phone and web
// connectors a public address for /mcp (ADR 0232-a). The operator mounts only
// /mcp and the sign-in paths on it (ADR 0233), but a mistyped command can mount
// the whole port. This guard is the second line: a request that arrived through
// Funnel is answered 404 unless its path is exactly one of funnelPaths.
//
// Funnel marks every request it forwards with Tailscale-Funnel-Request and
// deletes any value a visitor sends, so an outside client can neither forge
// the header nor remove it. The guard keys on the header's presence, not its
// value. Requests without it (the LAN, Tailscale itself, Claude Code on a
// laptop) are not touched.

// funnelPaths is every path a Funnel visitor may reach: the MCP endpoint and the
// OAuth sign-in (mcp_oauth.go). Matched whole, never as a prefix.
var funnelPaths = map[string]bool{
	mcpPath:                       true,
	oauthAuthorizePath:            true,
	oauthTokenPath:                true,
	oauthRegisterPath:             true,
	oauthProtectedResourcePath:    true,
	oauthProtectedResourceMCPPath: true,
	oauthServerMetadataPath:       true,
}

const (
	funnelRequestHeader = "Tailscale-Funnel-Request"
	funnelLogInterval   = time.Minute
)

type funnelGuard struct {
	refusals rateLimitedLog
}

func newFunnelGuard() *funnelGuard {
	return &funnelGuard{refusals: rateLimitedLog{now: time.Now, logf: log.Printf, interval: funnelLogInterval}}
}

// middleware is registered with e.Pre so it runs before routing: a path no
// route owns, and the single-page app's fallback, are covered too.
func (g *funnelGuard) middleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			req := c.Request()
			// The constant is already in canonical form, so the map is read
			// directly. The path compared is the one Echo routes on: the raw,
			// still-encoded path when there is one, so /mc%70 is not /mcp.
			if _, viaFunnel := req.Header[funnelRequestHeader]; !viaFunnel || funnelPaths[echo.GetPath(req)] {
				return next(c)
			}
			g.logRefusal(req)
			// The same plain body Tailscale's own http.NotFound gives.
			return c.String(http.StatusNotFound, "404 page not found\n")
		}
	}
}

// logRefusal says a Funnel request was refused, at most once a minute, so a
// misconfigured Funnel is visible without a scanner flooding the log. A
// correct /mcp mount still forwards /mcp/<anything>, so a refusal on its own
// is not proof of a mistake; the line says what to check.
func (g *funnelGuard) logRefusal(req *http.Request) {
	g.refusals.log(" (%d more refused since the last line)",
		"funnel guard: refused %s %q from %q: only /mcp, its sign-in paths and the OAuth metadata are served through Tailscale Funnel; if refused paths fall outside those, `tailscale funnel status` shows more than that is shared",
		req.Method, req.URL.Path, req.Header.Get("X-Forwarded-For"))
}
