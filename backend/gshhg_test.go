package main

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestGshhgCoastlineHandler_ReturnsEmbeddedGeoJSON(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/gshhg-coastline", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := gshhgCoastlineHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=604800, immutable" {
		t.Fatalf("unexpected Cache-Control: %q", cc)
	}

	if !json.Valid(rec.Body.Bytes()) {
		t.Fatalf("response body is not valid JSON")
	}

	var parsed struct {
		Type     string `json:"type"`
		Features []struct {
			Type string `json:"type"`
		} `json:"features"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("failed to unmarshal response as GeoJSON: %v", err)
	}
	if parsed.Type != "FeatureCollection" {
		t.Fatalf("expected FeatureCollection, got %q", parsed.Type)
	}
	if len(parsed.Features) == 0 {
		t.Fatalf("expected at least one feature, got none")
	}
}

// TestGshhgCoastlineHandler_GzipsForClientsThatAcceptIt is item 3's core
// regression test: the 1.5MB embedded coastline used to run through the
// gzip middleware on every single request (measured +90-110ms on the
// boat). It's now compressed once at startup (mustGzip's package-var
// initializer) and served pre-compressed to a client that says it accepts
// gzip, with Vary: Accept-Encoding so a cache in front of this never mixes
// the two representations up.
func TestGshhgCoastlineHandler_GzipsForClientsThatAcceptIt(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/gshhg-coastline", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := gshhgCoastlineHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("expected Content-Encoding: gzip, got %q", got)
	}
	if got := rec.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Fatalf("expected Vary: Accept-Encoding, got %q", got)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=604800, immutable" {
		t.Fatalf("unexpected Cache-Control: %q", cc)
	}
	if rec.Body.Len() >= len(gshhgCoastlineJSON) {
		t.Fatalf("compressed body (%d bytes) is not smaller than the source (%d bytes)", rec.Body.Len(), len(gshhgCoastlineJSON))
	}

	gz, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer gz.Close()
	decoded, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("reading gzip body: %v", err)
	}
	if string(decoded) != string(gshhgCoastlineJSON) {
		t.Fatalf("decompressed body does not match the source coastline JSON")
	}
}

// TestGshhgCoastlineHandler_RawForClientsThatDoNotAcceptGzip is the other
// client kind item 3 asks for: no Accept-Encoding at all must still get a
// plain, directly-parseable body, with Vary: Accept-Encoding present so a
// shared cache still knows to vary on it, but no Content-Encoding header.
func TestGshhgCoastlineHandler_RawForClientsThatDoNotAcceptGzip(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/gshhg-coastline", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := gshhgCoastlineHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("expected no Content-Encoding for a client that did not ask for gzip, got %q", got)
	}
	if got := rec.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Fatalf("expected Vary: Accept-Encoding, got %q", got)
	}
	if !json.Valid(rec.Body.Bytes()) {
		t.Fatalf("response body is not valid JSON")
	}
	if string(rec.Body.Bytes()) != string(gshhgCoastlineJSON) {
		t.Fatalf("raw response body does not match the source coastline JSON")
	}
}
