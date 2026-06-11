package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// ── helpers ────────────────────────────────────────────────────────────────────

func makeTracksServer(t *testing.T, tracksBody map[string]any, vesselsBody map[string]any) *httptest.Server {
	t.Helper()
	tracksJSON, _ := json.Marshal(tracksBody)
	vesselsJSON, _ := json.Marshal(vesselsBody)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/tracks") || strings.Contains(r.URL.Path, "v1/api/tracks"):
			w.Write(tracksJSON)
		case strings.Contains(r.URL.Path, "/vessels/self"):
			w.Write([]byte(`{"name":"TESTSELF"}`))
		case strings.HasSuffix(r.URL.Path, "/vessels"):
			w.Write(vesselsJSON)
		default:
			http.NotFound(w, r)
		}
	}))
	return srv
}

func settingsFileForServer(t *testing.T, serverURL string) string {
	t.Helper()
	// buildSignalKURL accepts a full http:// URL as address; use that directly.
	content := []byte("signalk:\n  address: " + serverURL + "\n  port: 3000\n")
	dir := t.TempDir()
	path := dir + "/settings.yaml"
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("could not write temp settings: %v", err)
	}
	return path
}

// ── tests ──────────────────────────────────────────────────────────────────────

func TestFetchSignalKAISTrails_ReturnsNamedTrails(t *testing.T) {
	tracksBody := map[string]any{
		"vessels.urn:mrn:imo:mmsi:123456789": map[string]any{
			"type": "MultiLineString",
			"coordinates": [][][]float64{
				{{152.91, -25.29}, {152.92, -25.30}},
			},
		},
	}
	vesselsBody := map[string]any{
		"urn:mrn:imo:mmsi:123456789": map[string]any{
			"name": "PEGASUS",
		},
	}

	srv := makeTracksServer(t, tracksBody, vesselsBody)
	defer srv.Close()

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SIGNALK_TRACKS_PATH", "/v1/api/tracks")
	t.Setenv("SIGNALK_VESSELS_PATH", "/vessels")
	t.Setenv("SIGNALK_VESSEL_PATH", "/vessels/self")

	trails := fetchSignalKAISTrails(settingsPath)

	if len(trails) != 1 {
		t.Fatalf("expected 1 trail, got %d", len(trails))
	}
	pts, ok := trails["PEGASUS"]
	if !ok {
		t.Fatal("expected trail keyed by vessel name PEGASUS")
	}
	if len(pts) != 2 {
		t.Fatalf("expected 2 points, got %d", len(pts))
	}
	// SignalK/tracks uses [lon, lat] order; adapter must flip to lat/lon.
	if pts[0].Lat != -25.29 || pts[0].Lon != 152.91 {
		t.Errorf("first point lat/lon mismatch: got %+v", pts[0])
	}
}

func TestFetchSignalKAISTrails_ExcludesSelf(t *testing.T) {
	tracksBody := map[string]any{
		"vessels.self": map[string]any{
			"type": "MultiLineString",
			"coordinates": [][][]float64{
				{{152.91, -25.29}},
			},
		},
	}
	srv := makeTracksServer(t, tracksBody, map[string]any{})
	defer srv.Close()

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SIGNALK_TRACKS_PATH", "/v1/api/tracks")
	t.Setenv("SIGNALK_VESSELS_PATH", "/vessels")
	t.Setenv("SIGNALK_VESSEL_PATH", "/vessels/self")

	trails := fetchSignalKAISTrails(settingsPath)
	if len(trails) != 0 {
		t.Fatalf("expected self vessel excluded, got %d trails", len(trails))
	}
}

func TestFetchSignalKAISTrails_EmptyOnBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not-json"))
	}))
	defer srv.Close()

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SIGNALK_TRACKS_PATH", "/v1/api/tracks")
	t.Setenv("SIGNALK_VESSELS_PATH", "/vessels")
	t.Setenv("SIGNALK_VESSEL_PATH", "/vessels/self")

	trails := fetchSignalKAISTrails(settingsPath)
	if len(trails) != 0 {
		t.Fatalf("expected empty result on bad JSON, got %d trails", len(trails))
	}
}

func TestFetchSignalKAISTrails_EmptyOnNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SIGNALK_TRACKS_PATH", "/v1/api/tracks")
	t.Setenv("SIGNALK_VESSELS_PATH", "/vessels")
	t.Setenv("SIGNALK_VESSEL_PATH", "/vessels/self")

	trails := fetchSignalKAISTrails(settingsPath)
	if len(trails) != 0 {
		t.Fatalf("expected empty result on non-OK status, got %d trails", len(trails))
	}
}

func TestFetchSignalKAISTrails_SkipsInvalidCoordinates(t *testing.T) {
	tracksBody := map[string]any{
		"vessels.urn:mrn:imo:mmsi:111": map[string]any{
			"type": "MultiLineString",
			"coordinates": [][][]float64{
				{{999.0, 999.0}}, // out of range
			},
		},
	}
	srv := makeTracksServer(t, tracksBody, map[string]any{})
	defer srv.Close()

	settingsPath := settingsFileForServer(t, srv.URL)
	t.Setenv("SIGNALK_TRACKS_PATH", "/v1/api/tracks")
	t.Setenv("SIGNALK_VESSELS_PATH", "/vessels")
	t.Setenv("SIGNALK_VESSEL_PATH", "/vessels/self")

	trails := fetchSignalKAISTrails(settingsPath)
	if len(trails) != 0 {
		t.Fatalf("expected invalid coordinates skipped, got %d trails", len(trails))
	}
}
