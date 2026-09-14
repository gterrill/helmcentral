package main

import (
	"embed"
	"io/fs"
	"log"
	"mime"
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
	f, err := distFS.Open("index.html")
	if err != nil {
		return
	}
	f.Close()

	registerStaticHandlerFS(e, distFS)
}

// registerStaticHandlerFS is the testable core of registerStaticHandler: it
// assumes distFS already holds a real frontend build.
func registerStaticHandlerFS(e *echo.Echo, distFS fs.FS) {
	// Go's builtin MIME table has no .webmanifest entry, and the release
	// container ships no /etc/mime.types, so http.FileServer would sniff the
	// manifest as text/plain — which Firefox rejects outright, silently
	// un-installing the PWA and with it iOS's only route to web push.
	if err := mime.AddExtensionType(".webmanifest", "application/manifest+json"); err != nil {
		log.Printf("static: could not register the .webmanifest MIME type: %v", err)
	}

	fileServer := http.FileServer(http.FS(distFS))

	serve := func(c echo.Context) error {
		reqPath := strings.TrimPrefix(c.Request().URL.Path, "/")
		isShell := true
		if reqPath != "" {
			f, openErr := distFS.Open(reqPath)
			if openErr != nil {
				// An unknown path is a client-side route (/anchor, /routes, ...).
				// Serve the app shell by pointing at the directory root, NOT at
				// "/index.html": http.FileServer answers an explicit index.html
				// request with a 301 back to "./", which turned every deep link
				// into a redirect instead of the dashboard.
				c.Request().URL.Path = "/"
			} else {
				f.Close()
				isShell = false
			}
		}

		// The shell (index.html, served at "/" and at every unmatched deep
		// link) references hashed /assets/* files by name. A kiosk left
		// running for days reloads its tab far more often than a phone
		// bookmark does, and a cached shell from before the last deploy
		// names assets that no longer exist on disk - a blank kiosk screen
		// with nothing in any log to say why.
		//
		// Hashed assets are the opposite case: their filename already
		// changes on every build (Vite's content hash), so the same name can
		// never resolve to different bytes across a deploy. http.FileServer
		// leaves them with no Cache-Control at all - and no ETag or
		// Last-Modified either, since embed.FS reports a zero modtime for
		// every file - so a kiosk reload was refetching the whole ~2.3MB
		// bundle every single time. Tell the browser it never needs to
		// revalidate.
		switch {
		case isShell:
			c.Response().Header().Set("Cache-Control", "no-cache")
		case strings.HasPrefix(reqPath, "assets/"):
			c.Response().Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fileServer.ServeHTTP(c.Response().Writer, c.Request())
		return nil
	}

	// Both routes are required. Echo's "/*" wildcard does not match the bare
	// root path, so registering only the wildcard left "/" - the dashboard's
	// own entry point - falling through to Echo's JSON 404.
	e.GET("/", serve)
	e.GET("/*", serve)
}
