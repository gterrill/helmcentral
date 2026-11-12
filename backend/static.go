package main

import (
	"embed"
	"errors"
	"io"
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
		if c.Request().Method == http.MethodHead {
			if allow := headOwnedElsewhere(e, c.Request()); allow != "" {
				c.Response().Header().Set(echo.HeaderAllow, allow)
				return echo.NewHTTPError(http.StatusMethodNotAllowed)
			}
		}
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
	//
	// HEAD shares GET's routes: a kiosk launcher that probes readiness with
	// `wget --spider` got Echo's 404 for the dashboard otherwise.
	// http.FileServer already answers HEAD with GET's status and headers
	// and no body.
	methods := []string{http.MethodGet, http.MethodHead}

	// The manifest's share target posts files here. The installed app's
	// service worker answers the POST itself and the server never sees it. If
	// the post does arrive, no worker was running, so the files are lost; say
	// so on the upload page rather than answer with a bare error.
	//
	// The body is read and discarded first. Redirecting with a large upload
	// unread makes Go close the connection mid-upload, and the phone then
	// shows a network error instead of following the redirect. The route
	// has no auth, so the read is capped at 4 MB, enough for a small share.
	// Past the cap the box stops reading, logs it, and sends the redirect
	// with Connection: close; a large share may then show the phone a
	// network error, which is the price of not reading for strangers.
	e.POST(shareTargetRoute, func(c echo.Context) error {
		if _, err := io.Copy(io.Discard, http.MaxBytesReader(c.Response(), c.Request().Body, shareDrainMaxBytes)); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				log.Printf("static: share body over the %d byte cap, closing the connection", shareDrainMaxBytes)
				c.Response().Header().Set("Connection", "close")
			} else {
				log.Printf("static: reading the unhandled share body: %v", err)
			}
		}
		return c.Redirect(http.StatusSeeOther, shareWithoutWorkerLocation)
	})

	e.Match(methods, staticRootRoute, serve)
	e.Match(methods, staticCatchAllRoute, serve)
}

const (
	shareTargetRoute           = "/share-target"
	shareWithoutWorkerLocation = "/inbox/upload?share_error=no-worker"
	shareDrainMaxBytes         = 4 << 20

	staticRootRoute     = "/"
	staticCatchAllRoute = "/*"
)

// headOwnedElsewhere reports the methods a request's path allows when GET on
// it reaches a route other than the shell's - an API route, the OAuth
// .well-known documents. Every such route is GET-only, so Echo hands a HEAD
// on it to the catch-all, which would answer a JSON endpoint with a 200 HTML
// page. The answer is the 405 Echo itself gives for a method a route lacks.
// It returns "" when GET would reach the shell too.
func headOwnedElsewhere(e *echo.Echo, req *http.Request) string {
	probe := e.AcquireContext()
	defer e.ReleaseContext(probe)
	probe.Reset(req, nil)
	e.Router().Find(http.MethodGet, echo.GetPath(req), probe)

	route := probe.Path()
	if route == staticRootRoute || route == staticCatchAllRoute {
		return ""
	}
	allow := []string{http.MethodOptions}
	for _, r := range e.Routes() {
		if r.Path == route {
			allow = append(allow, r.Method)
		}
	}
	return strings.Join(allow, ", ")
}
