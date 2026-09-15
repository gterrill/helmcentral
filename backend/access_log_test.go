package main

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// TestAccessLogSkipWriter_DropsSuccessfulGetAndHeadLines is item 6's unit
// test: fed the exact JSON-line shape Echo's default logger format
// produces, accessLogSkipWriter must swallow a successful (2xx or 304)
// GET/HEAD line and forward everything else - errors, and any non-GET/HEAD
// method - unconditionally.
func TestAccessLogSkipWriter_DropsSuccessfulGetAndHeadLines(t *testing.T) {
	line := func(method string, status int) string {
		return `{"time":"2026-09-15T09:00:00Z","method":"` + method + `","status":` + strconv.Itoa(status) + `}` + "\n"
	}

	cases := []struct {
		name string
		line string
		want bool // true: forwarded to underlying
	}{
		{"GET 200 dropped", line(http.MethodGet, 200), false},
		{"GET 304 dropped", line(http.MethodGet, http.StatusNotModified), false},
		{"HEAD 200 dropped", line(http.MethodHead, 200), false},
		{"GET 404 forwarded", line(http.MethodGet, 404), true},
		{"GET 500 forwarded", line(http.MethodGet, 500), true},
		{"POST 200 forwarded", line(http.MethodPost, 200), true},
		{"DELETE 200 forwarded", line(http.MethodDelete, 200), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf strings.Builder
			w := accessLogSkipWriter{underlying: &buf}
			n, err := w.Write([]byte(tc.line))
			if err != nil {
				t.Fatalf("Write: %v", err)
			}
			if n != len(tc.line) {
				t.Fatalf("Write returned n=%d, want %d (the real logger middleware only checks for a short-write error)", n, len(tc.line))
			}
			got := buf.Len() > 0
			if got != tc.want {
				t.Fatalf("forwarded=%v, want %v (line: %s)", got, tc.want, tc.line)
			}
		})
	}
}

// TestAccessLogSkipWriter_UnparsableLineIsForwarded guards the fail-fast
// side: a line that doesn't decode as the expected JSON shape (a future
// Format change, say) must still reach the underlying writer rather than be
// silently swallowed - losing log lines by accident is worse than a few
// noisy ones getting through.
func TestAccessLogSkipWriter_UnparsableLineIsForwarded(t *testing.T) {
	var buf strings.Builder
	w := accessLogSkipWriter{underlying: &buf}
	if _, err := w.Write([]byte("not json at all\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected an unparsable line to be forwarded rather than dropped")
	}
}

// TestAccessLogMiddleware_EndToEndOnlyKeepsNonSkippedLines exercises the
// exact wiring main.go uses: middleware.LoggerWithConfig with Output set to
// accessLogSkipWriter, driven through the real Echo middleware chain. This
// is the regression guard for the fact that LoggerConfig's own Skipper runs
// before next(c) and so cannot see the response status (see main.go's
// middleware.LoggerWithConfig call and access_log.go's doc comment) -
// proving the post-handler decision actually reaches the real formatted
// output, not just a hand-built line.
func TestAccessLogMiddleware_EndToEndOnlyKeepsNonSkippedLines(t *testing.T) {
	var buf strings.Builder
	e := echo.New()
	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Output: accessLogSkipWriter{underlying: &buf},
	}))
	e.GET("/ok", func(c echo.Context) error { return c.NoContent(http.StatusOK) })
	e.GET("/missing", func(c echo.Context) error { return c.NoContent(http.StatusNotFound) })
	e.POST("/write", func(c echo.Context) error { return c.NoContent(http.StatusOK) })

	get := func(path string) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
	}
	post := func(path string) {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
	}

	get("/ok")
	get("/missing")
	post("/write")

	out := buf.String()
	if strings.Contains(out, `"uri":"/ok"`) {
		t.Fatalf("successful GET must not be logged, got:\n%s", out)
	}
	if !strings.Contains(out, `"uri":"/missing"`) {
		t.Fatalf("a 404 GET must still be logged, got:\n%s", out)
	}
	if !strings.Contains(out, `"uri":"/write"`) {
		t.Fatalf("a successful POST must still be logged, got:\n%s", out)
	}
}
