package main

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

//go:embed all:dist
var embeddedFrontend embed.FS

// registerStaticHandler serves the embedded frontend SPA from the backend.
// It is a no-op when the dist directory is empty (local dev without a frontend build).
func registerStaticHandler(e *echo.Echo) {
	distFS, err := fs.Sub(embeddedFrontend, "dist")
	if err != nil {
		return
	}

	// Only activate when index.html is present (i.e. a real frontend build was embedded).
	if _, err := distFS.Open("index.html"); err != nil {
		return
	}

	fileServer := http.FileServer(http.FS(distFS))

	e.GET("/*", func(c echo.Context) error {
		reqPath := strings.TrimPrefix(c.Request().URL.Path, "/")
		if reqPath == "" {
			reqPath = "index.html"
		}

		// If the asset doesn't exist, fall back to index.html for SPA routing.
		if _, openErr := distFS.Open(reqPath); openErr != nil {
			c.Request().URL.Path = "/index.html"
		}

		fileServer.ServeHTTP(c.Response().Writer, c.Request())
		return nil
	})
}
