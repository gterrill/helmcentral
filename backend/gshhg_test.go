package main

import (
	"encoding/json"
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
