package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

// ── tiers ─────────────────────────────────────────────────────────────────────

// apiTier is one of the four permission tiers every /api route is
// classified into (docs/adr/0040, plan §3):
//
//	public — no session required at all (health check, the auth endpoints
//	         themselves, the embedded SPA).
//	read   — any valid session (readonly and above).
//	write  — readwrite and above: anything that commands equipment or
//	         changes stored state that isn't itself a security setting.
//	admin  — admin only: settings, secrets, plugin config, alarm transports.
//
// tierUnset (the zero value) is deliberately NOT a usable tier: an
// apiRoute struct literal that forgets to set Tier must fail loudly rather
// than silently registering as the most permissive tier available.
type apiTier int

const (
	tierUnset apiTier = iota
	tierPublic
	tierRead
	tierWrite
	tierAdmin
)

// roleRank orders Helmcentral's three roles so a session's role can be
// compared against a route's minimum requirement with a single >=.
var roleRank = map[string]int{
	roleReadonly:  1,
	roleReadwrite: 2,
	roleAdmin:     3,
}

// roleSatisfies reports whether role meets or exceeds required on the
// readonly < readwrite < admin ladder.
func roleSatisfies(role, required string) bool {
	return roleRank[role] >= roleRank[required]
}

// tierMinRole is the minimum role each non-public tier requires. Public and
// unset are handled separately by requireRole/registerAPIRoutes.
var tierMinRole = map[apiTier]string{
	tierRead:  roleReadonly,
	tierWrite: roleReadwrite,
	tierAdmin: roleAdmin,
}

// requireRole builds the Echo middleware gating one non-public tier.
//
// It resolves auth.mode fresh on every request (readSettings is already
// re-read per-request elsewhere in this codebase, e.g. loadSignalKSettings)
// rather than caching it at startup, so flipping the setting takes effect
// without a restart. When mode is "none" — this release's default — every
// request passes through unauthenticated, exactly Helmcentral's pre-auth
// behaviour; the fail-closed checks below only engage once an operator has
// opted into mode: signalk.
func requireRole(sessions *sessionStore, minRole string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
			if resolveAuthMode(settingsPath) == authModeNone {
				return next(c)
			}

			cookie, err := c.Cookie(sessionCookieName)
			if err != nil || cookie.Value == "" {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			}

			rec, err := sessions.Validate(cookie.Value)
			if err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
			}
			if rec == nil {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "session expired or invalid"})
			}

			if !roleSatisfies(rec.Role, minRole) {
				return c.JSON(http.StatusForbidden, map[string]string{
					"error": fmt.Sprintf("this action requires %s access; your session has %s", minRole, rec.Role),
				})
			}

			c.Set("hc_session_username", rec.SKUsername)
			c.Set("hc_session_role", rec.Role)
			return next(c)
		}
	}
}

// ── route table ───────────────────────────────────────────────────────────────

// apiRoute is one entry in the production route table (built by
// buildAPIRoutes in main.go). Routes are data, not scattered e.GET/e.POST
// calls, specifically so a new endpoint's tier is visible in one table
// instead of being a per-call detail someone can forget (plan §3).
type apiRoute struct {
	Method  string
	Path    string
	Tier    apiTier
	Handler echo.HandlerFunc
}

// registerAPIRoutes is the ONLY place any /api route may be wired to the
// Echo instance. It builds the four tier groups (each with its own
// middleware, or none for public), registers every entry in routes against
// the matching group, and returns a "METHOD path" -> tier map recording
// exactly what was registered.
//
// That returned map is what makes the tier system self-enforcing rather
// than merely documented: TestAPIRouteCoverage_EveryRegisteredAPIRouteHasATier
// (auth_middleware_test.go) walks the live Echo router after calling this
// function and fails if any /api route exists that this map doesn't know
// about — which can only happen if a future change registers a route
// directly on the echo.Echo instead of adding it to buildAPIRoutes.
func registerAPIRoutes(e *echo.Echo, sessions *sessionStore, routes []apiRoute) map[string]apiTier {
	publicGroup := e.Group("")
	readGroup := e.Group("", requireRole(sessions, tierMinRole[tierRead]))
	writeGroup := e.Group("", requireRole(sessions, tierMinRole[tierWrite]))
	adminGroup := e.Group("", requireRole(sessions, tierMinRole[tierAdmin]))

	registered := make(map[string]apiTier, len(routes))
	for _, route := range routes {
		var group *echo.Group
		switch route.Tier {
		case tierPublic:
			group = publicGroup
		case tierRead:
			group = readGroup
		case tierWrite:
			group = writeGroup
		case tierAdmin:
			group = adminGroup
		default:
			// A route with no explicit tier must never silently become
			// reachable — fail the build rather than guess.
			panic(fmt.Sprintf("api route %s %s registered with no explicit auth tier", route.Method, route.Path))
		}
		group.Add(route.Method, route.Path, route.Handler)
		registered[route.Method+" "+route.Path] = route.Tier
	}
	return registered
}

// ── background sweep ─────────────────────────────────────────────────────────

// sessionSweepInterval is how often expired sessions are swept out of the
// store on top of Validate's lazy per-row cleanup (plan §1: "a sweep on
// startup and hourly").
const sessionSweepInterval = 1 * time.Hour

// startSessionSweeper runs sessions.Sweep() once immediately (covering
// rows that expired while the process was down) and then on
// sessionSweepInterval until ctx is cancelled. A sweep failure is logged,
// not fatal — a transient sweep error does not make existing sessions
// invalid, only leaves a few expired rows around until the next tick.
func startSessionSweeper(ctx context.Context, sessions *sessionStore, interval time.Duration) {
	sweep := func() {
		n, err := sessions.Sweep()
		if err != nil {
			log.Printf("session sweep: %v", err)
			return
		}
		if n > 0 {
			log.Printf("session sweep: removed %d expired session(s)", n)
		}
	}

	sweep()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweep()
		}
	}
}
