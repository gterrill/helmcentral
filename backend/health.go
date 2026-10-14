package main

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

func healthCheck(c echo.Context) error {
	version, revision := resolveBuildMetadata()

	return c.JSON(http.StatusOK, map[string]string{
		"status":   "ok",
		"version":  version,
		"revision": revision,
	})
}

// healthRoutes returns the /api routes for the liveness endpoint.
// Each row carries its own auth tier (docs/adr/0177).
func healthRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodGet, "/api/health", tierPublic, healthCheck},
	}
}
