package main

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
)

// This file serves the operator manual embedded at build time
// (backend/assistant_manual.go) over HTTP, for the frontend's Manual sheet
// (ADR 0095) - the same globalManual pages Mate's read_manual tool already
// answers from (assistant_tools.go's executeReadManual), reached here by
// direct page id rather than through the assistant's tool-calling loop.

// getManualPageHandler builds GET /api/manual/*. manual is injected the
// same way assistantToolDeps.manual is - production passes
// func() []manualPage { return globalManual } (main.go's buildAPIRoutes),
// tests pass a fixture - so this handler never touches the global directly.
func getManualPageHandler(manual func() []manualPage) echo.HandlerFunc {
	return func(c echo.Context) error {
		pages := manual()
		if len(pages) == 0 {
			// The route exists; the build just never staged a manual. Same
			// sentence executeReadManual uses (assistant_tools.go), so the
			// operator sees one consistent message whether they hit this
			// from Mate or the Manual sheet. 503, not 404: this is a build
			// problem, not an unknown page.
			return c.JSON(http.StatusServiceUnavailable, map[string]string{
				"error": "the manual is not embedded in this build (run make manual-stage)",
			})
		}

		// The wildcard route param, not a named one: a manual page id
		// contains a slash ("features/dashboard"), which a normal echo
		// :param cannot hold.
		id := c.Param("*")
		for i := range pages {
			if pages[i].ID == id {
				return c.JSON(http.StatusOK, map[string]string{
					"id":    pages[i].ID,
					"title": pages[i].Title,
					"body":  pages[i].Body,
				})
			}
		}

		// Exact string match over the loaded slice above, so an id like
		// "../etc/passwd" is simply absent from it, not resolved against
		// the filesystem - there is no path traversal to guard against
		// because there is no path lookup at all.
		return c.JSON(http.StatusNotFound, map[string]string{
			"error": fmt.Sprintf("unknown manual page %q", id),
		})
	}
}
