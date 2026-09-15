package main

import (
	"log"
	"net/http"
	_ "net/http/pprof" // registers Index/Cmdline/Profile/Symbol/Trace and the runtime profile handlers onto http.DefaultServeMux; only reachable through the route registerPprofRoutes wires up below, and only when HELMCENTRAL_PPROF=1.
	"os"

	"github.com/labstack/echo/v4"
)

// helmcentralPprofEnvVar gates net/http/pprof's handlers behind an explicit
// opt-in. There was no pprof or expvar anywhere in the backend (backend
// perf audit Tier 3), so the next audit pass had to infer CPU/goroutine
// splits from measurement rather than read them directly. Off by default:
// auth mode is often "none" on the boat (checkAuthModeAtStartup, main.go),
// and pprof - especially a profile capture that blocks for the requested
// duration, or cmdline - is not something to expose to anyone who can reach
// the LAN or Tailscale address unasked.
const helmcentralPprofEnvVar = "HELMCENTRAL_PPROF"

// registerPprofRoutes wires net/http/pprof's handlers under /debug/pprof/
// when HELMCENTRAL_PPROF=1, and does nothing at all otherwise - the route
// is simply never registered, so a request to it 404s the same as any other
// unknown path. The net/http/pprof import above always registers its
// handlers onto http.DefaultServeMux at package init, unconditionally, but
// that's harmless on its own: this process never serves DefaultServeMux
// over HTTP itself (e.Start uses Echo's own router), so nothing reaches
// those handlers unless this route also exists.
func registerPprofRoutes(e *echo.Echo) {
	if os.Getenv(helmcentralPprofEnvVar) != "1" {
		return
	}
	log.Printf("WARNING: %s=1 - /debug/pprof/* is registered and reachable to anyone who can reach this server.", helmcentralPprofEnvVar)
	e.Any("/debug/pprof/*", echo.WrapHandler(http.DefaultServeMux))
}
