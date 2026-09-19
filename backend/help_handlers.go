package main

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
)

// This file serves the in-app help embedded at build time
// (backend/assistant_help.go) over HTTP, for the frontend's Help sheet
// (ADR 0095) - the same globalHelp pages Mate's read_help tool already
// answers from (assistant_tools.go's executeReadHelp), reached here by
// direct page id rather than through the assistant's tool-calling loop.

// getHelpPageHandler builds GET /api/help/*. help is injected the
// same way assistantToolDeps.help is - production passes
// func() []helpPage { return globalHelp } (main.go's buildAPIRoutes),
// tests pass a fixture - so this handler never touches the global directly.
func getHelpPageHandler(help func() []helpPage) echo.HandlerFunc {
	return func(c echo.Context) error {
		pages := help()
		if len(pages) == 0 {
			// The route exists; the build just never staged help. Same
			// sentence executeReadHelp uses (assistant_tools.go), so the
			// operator sees one consistent message whether they hit this
			// from Mate or the Help sheet. 503, not 404: this is a build
			// problem, not an unknown page.
			return c.JSON(http.StatusServiceUnavailable, map[string]string{
				"error": "help is not embedded in this build (run make help-stage)",
			})
		}

		// The wildcard route param, not a named one: a help page id
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
			"error": fmt.Sprintf("unknown help page %q", id),
		})
	}
}
