package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/labstack/echo/v4"
	_ "modernc.org/sqlite"
)

func setupSatChartsTest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SAT_CHARTS_DIR", dir)
	return dir
}

func buildTestPNGTile() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// buildTestMBTiles creates a minimal valid MBTiles SQLite file at path, with
// the given metadata and exactly one tile at (z, xyzX, xyzY) expressed in
// XYZ coordinates (converted to TMS internally, matching a real export).
func buildTestMBTiles(t *testing.T, path string, z, xyzX, xyzY int, name, bounds string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	schema := []string{
		`CREATE TABLE metadata (name TEXT, value TEXT)`,
		`CREATE TABLE tiles (zoom_level INTEGER, tile_column INTEGER, tile_row INTEGER, tile_data BLOB)`,
	}
	for _, stmt := range schema {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec schema %q: %v", stmt, err)
		}
	}

	meta := map[string]string{
		"name":    name,
		"bounds":  bounds,
		"minzoom": fmt.Sprintf("%d", z),
		"maxzoom": fmt.Sprintf("%d", z),
		"format":  "png",
	}
	for k, v := range meta {
		if _, err := db.Exec(`INSERT INTO metadata (name, value) VALUES (?, ?)`, k, v); err != nil {
			t.Fatalf("insert metadata %q: %v", k, err)
		}
	}

	tmsRow := xyzRowToTMSRow(z, xyzY)
	if _, err := db.Exec(
		`INSERT INTO tiles (zoom_level, tile_column, tile_row, tile_data) VALUES (?, ?, ?, ?)`,
		z, xyzX, tmsRow, buildTestPNGTile(),
	); err != nil {
		t.Fatalf("insert tile: %v", err)
	}
}

func newMultipartUploadRequest(t *testing.T, target, filename string, content []byte) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write form file content: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, target, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

func TestXYZRowToTMSRow_KnownValues(t *testing.T) {
	cases := []struct{ z, xyzRow, wantTMSRow int }{
		{z: 0, xyzRow: 0, wantTMSRow: 0},
		{z: 1, xyzRow: 0, wantTMSRow: 1},
		{z: 1, xyzRow: 1, wantTMSRow: 0},
		{z: 5, xyzRow: 0, wantTMSRow: 31},
		{z: 5, xyzRow: 31, wantTMSRow: 0},
		{z: 18, xyzRow: 150119, wantTMSRow: (1<<18 - 1) - 150119},
	}
	for _, tc := range cases {
		got := xyzRowToTMSRow(tc.z, tc.xyzRow)
		if got != tc.wantTMSRow {
			t.Errorf("xyzRowToTMSRow(%d, %d) = %d, want %d", tc.z, tc.xyzRow, got, tc.wantTMSRow)
		}
	}
}

func TestXYZRowToTMSRow_IsSelfInverse(t *testing.T) {
	for z := 0; z <= 10; z++ {
		for xyzRow := 0; xyzRow < (1 << z); xyzRow++ {
			tmsRow := xyzRowToTMSRow(z, xyzRow)
			roundTrip := xyzRowToTMSRow(z, tmsRow)
			if roundTrip != xyzRow {
				t.Fatalf("z=%d xyzRow=%d: round trip via TMS gave %d, want %d", z, xyzRow, roundTrip, xyzRow)
			}
		}
	}
}

func TestUploadSatChartHandler_AcceptsValidMBTiles(t *testing.T) {
	dir := setupSatChartsTest(t)

	srcPath := filepath.Join(t.TempDir(), "source.mbtiles")
	buildTestMBTiles(t, srcPath, 5, 10, 12, "Test Reef Chart", "150.0,-25.0,151.0,-24.0")
	content, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	c, rec := newMultipartUploadRequest(t, "/api/sat-charts", "source.mbtiles", content)
	if err := uploadSatChartHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var entry satChartCatalogEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if entry.Name != "Test Reef Chart" {
		t.Errorf("expected name %q, got %q", "Test Reef Chart", entry.Name)
	}
	if entry.Bounds != [4]float64{150.0, -25.0, 151.0, -24.0} {
		t.Errorf("unexpected bounds: %v", entry.Bounds)
	}
	if entry.MinZoom != 5 || entry.MaxZoom != 5 {
		t.Errorf("unexpected zoom range: min=%d max=%d", entry.MinZoom, entry.MaxZoom)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read storage dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one stored chart file, found %d", len(entries))
	}
}

func TestUploadSatChartHandler_RejectsNonMBTilesFile(t *testing.T) {
	dir := setupSatChartsTest(t)

	c, rec := newMultipartUploadRequest(t, "/api/sat-charts", "not-a-chart.txt", []byte("this is plainly not sqlite"))
	if err := uploadSatChartHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".mbtiles" {
			t.Fatalf("rejected upload should not leave a file in storage, found %q", e.Name())
		}
	}
}

func TestUploadSatChartHandler_RejectsSQLiteMissingRequiredTables(t *testing.T) {
	setupSatChartsTest(t)

	srcPath := filepath.Join(t.TempDir(), "incomplete.mbtiles")
	db, err := sql.Open("sqlite", srcPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE metadata (name TEXT, value TEXT)`); err != nil {
		t.Fatalf("create metadata table: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	content, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	c, rec := newMultipartUploadRequest(t, "/api/sat-charts", "incomplete.mbtiles", content)
	if err := uploadSatChartHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for MBTiles missing the tiles table, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSatChartTileHandler_ServesCorrectTileViaXYZRequest(t *testing.T) {
	dir := setupSatChartsTest(t)
	id := "test-chart-id"
	buildTestMBTiles(t, filepath.Join(dir, id+".mbtiles"), 5, 10, 12, "Test", "150,-25,151,-24")

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/sat-charts/"+id+"/5/10/12", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id", "z", "x", "y")
	c.SetParamValues(id, "5", "10", "12")

	if err := satChartTileHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("expected image/png, got %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=604800, immutable" {
		t.Fatalf("unexpected Cache-Control: %q", cc)
	}
	wantTile := buildTestPNGTile()
	if !bytes.Equal(rec.Body.Bytes(), wantTile) {
		t.Fatalf("tile bytes did not match expected fixture data")
	}
}

func TestSatChartTileHandler_Returns404ForMissingTile(t *testing.T) {
	dir := setupSatChartsTest(t)
	id := "test-chart-id"
	buildTestMBTiles(t, filepath.Join(dir, id+".mbtiles"), 5, 10, 12, "Test", "150,-25,151,-24")

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/sat-charts/"+id+"/5/11/12", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id", "z", "x", "y")
	c.SetParamValues(id, "5", "11", "12")

	if err := satChartTileHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a tile that does not exist, got %d", rec.Code)
	}
}

func TestSatChartTileHandler_Returns404ForUnknownChartID(t *testing.T) {
	setupSatChartsTest(t)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/sat-charts/does-not-exist/5/11/12", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id", "z", "x", "y")
	c.SetParamValues("does-not-exist", "5", "11", "12")

	if err := satChartTileHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown chart id, got %d", rec.Code)
	}
}

func TestListSatChartsHandler_SkipsCorruptFileWithoutFailing(t *testing.T) {
	dir := setupSatChartsTest(t)
	buildTestMBTiles(t, filepath.Join(dir, "good.mbtiles"), 5, 10, 12, "Good Chart", "150,-25,151,-24")
	if err := os.WriteFile(filepath.Join(dir, "corrupt.mbtiles"), []byte("not sqlite at all"), 0o644); err != nil {
		t.Fatalf("write corrupt fixture: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/sat-charts", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := listSatChartsHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 despite one corrupt file, got %d", rec.Code)
	}

	var resp struct {
		Charts []satChartCatalogEntry `json:"charts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Charts) != 1 {
		t.Fatalf("expected exactly 1 chart (corrupt one skipped), got %d", len(resp.Charts))
	}
	if resp.Charts[0].Name != "Good Chart" {
		t.Errorf("expected the good chart to be listed, got %q", resp.Charts[0].Name)
	}
}

func TestListSatChartsHandler_EmptyWhenDirectoryMissing(t *testing.T) {
	t.Setenv("SAT_CHARTS_DIR", filepath.Join(t.TempDir(), "does-not-exist-yet"))

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/sat-charts", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := listSatChartsHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for a missing directory, got %d", rec.Code)
	}

	var resp struct {
		Charts []satChartCatalogEntry `json:"charts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Charts) != 0 {
		t.Fatalf("expected no charts, got %d", len(resp.Charts))
	}
}

func TestDeleteSatChartHandler_RemovesFileAndListReflectsIt(t *testing.T) {
	dir := setupSatChartsTest(t)
	id := "to-delete"
	buildTestMBTiles(t, filepath.Join(dir, id+".mbtiles"), 5, 10, 12, "Doomed Chart", "150,-25,151,-24")

	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/sat-charts/"+id, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(id)

	if err := deleteSatChartHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if _, err := os.Stat(filepath.Join(dir, id+".mbtiles")); !os.IsNotExist(err) {
		t.Fatalf("expected file to be removed, stat err = %v", err)
	}
}

func TestDeleteSatChartHandler_RejectsPathTraversalID(t *testing.T) {
	setupSatChartsTest(t)

	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/sat-charts/..%2Fetc%2Fpasswd", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("../etc/passwd")

	if err := deleteSatChartHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a path-traversal id, got %d", rec.Code)
	}
}
