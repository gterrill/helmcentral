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

// TestSatChartTileHandler_ReusesHandleAcrossRequests is item 2's core
// regression test: sat_charts.go used to os.Stat, sql.Open, run the tile
// query, run a second query for the format, then close, for every single
// tile request. This proves a second request no longer repeats any of that
// open/stat sequence by removing the underlying file directly (bypassing
// deleteSatChartHandler, which is the only thing that should ever
// invalidate the cache) between two requests: a naive per-request
// re-open would 404 on the second request since os.Stat would fail, but a
// cached, already-open handle keeps serving from its open file descriptor
// regardless of what happens to the path on disk afterwards.
func TestSatChartTileHandler_ReusesHandleAcrossRequests(t *testing.T) {
	dir := setupSatChartsTest(t)
	id := "reuse-test-chart"
	path := filepath.Join(dir, id+".mbtiles")
	buildTestMBTiles(t, path, 5, 10, 12, "Reuse Test", "150,-25,151,-24")

	requestTile := func() *httptest.ResponseRecorder {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/api/sat-charts/"+id+"/5/10/12", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id", "z", "x", "y")
		c.SetParamValues(id, "5", "10", "12")
		if err := satChartTileHandler(c); err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		return rec
	}

	first := requestTile()
	if first.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", first.Code)
	}

	// Remove the file out from under the cache, without going through
	// deleteSatChartHandler. A handler that still stats/opens per request
	// would 404 here; a cached handle keeps its already-open file
	// descriptor and keeps serving.
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove chart file: %v", err)
	}

	second := requestTile()
	if second.Code != http.StatusOK {
		t.Fatalf("second request (file removed on disk, handle should be cached): expected 200, got %d", second.Code)
	}
	if !bytes.Equal(second.Body.Bytes(), buildTestPNGTile()) {
		t.Fatalf("second request served different tile bytes than the first")
	}
	if ct := second.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("second request: expected cached image/png content type, got %q", ct)
	}
}

// TestSatChartTileHandler_RemovedChartDoesNotServeFromStaleHandleAfterDelete
// is item 2's other required test: deleting a chart through the real
// handler must invalidate its cached handle, not leave it servable forever
// from a stale open file descriptor.
func TestSatChartTileHandler_RemovedChartDoesNotServeFromStaleHandleAfterDelete(t *testing.T) {
	dir := setupSatChartsTest(t)
	id := "delete-invalidates-cache"
	buildTestMBTiles(t, filepath.Join(dir, id+".mbtiles"), 5, 10, 12, "Delete Test", "150,-25,151,-24")

	tileReq := func() (echo.Context, *httptest.ResponseRecorder) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/api/sat-charts/"+id+"/5/10/12", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id", "z", "x", "y")
		c.SetParamValues(id, "5", "10", "12")
		return c, rec
	}

	c, rec := tileReq()
	if err := satChartTileHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 before delete, got %d", rec.Code)
	}

	e := echo.New()
	delReq := httptest.NewRequest(http.MethodDelete, "/api/sat-charts/"+id, nil)
	delRec := httptest.NewRecorder()
	dc := e.NewContext(delReq, delRec)
	dc.SetParamNames("id")
	dc.SetParamValues(id)
	if err := deleteSatChartHandler(dc); err != nil {
		t.Fatalf("delete handler returned error: %v", err)
	}
	if delRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from delete, got %d: %s", delRec.Code, delRec.Body.String())
	}

	c2, rec2 := tileReq()
	if err := satChartTileHandler(c2); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete (no stale handle), got %d", rec2.Code)
	}
}

// TestSatChartTileHandler_CacheKeyIncludesDirectorySoSameIDInDifferentDirsDoesNotLeak
// guards against a handle cache keyed on the bare chart id alone:
// SAT_CHARTS_DIR differs per test (each gets its own t.TempDir(), and in
// production it can be repointed via HELMCENTRAL_STATE_DIR too), so two
// charts that happen to share an id string but live under different
// directories must never serve each other's tiles from a stale cached
// handle.
func TestSatChartTileHandler_CacheKeyIncludesDirectorySoSameIDInDifferentDirsDoesNotLeak(t *testing.T) {
	id := "shared-id"

	dirA := t.TempDir()
	t.Setenv("SAT_CHARTS_DIR", dirA)
	buildTestMBTiles(t, filepath.Join(dirA, id+".mbtiles"), 5, 10, 12, "Chart A", "150,-25,151,-24")

	requestTile := func() *httptest.ResponseRecorder {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/api/sat-charts/"+id+"/5/10/12", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id", "z", "x", "y")
		c.SetParamValues(id, "5", "10", "12")
		if err := satChartTileHandler(c); err != nil {
			t.Fatalf("handler returned error: %v", err)
		}
		return rec
	}

	first := requestTile()
	if first.Code != http.StatusOK {
		t.Fatalf("chart A: expected 200, got %d", first.Code)
	}

	dirB := t.TempDir()
	t.Setenv("SAT_CHARTS_DIR", dirB)
	// Chart B has no tile at z=5,x=10,y=12 at all - a different chart file
	// entirely, sharing only the id string with chart A.
	buildTestMBTiles(t, filepath.Join(dirB, id+".mbtiles"), 5, 20, 20, "Chart B", "150,-25,151,-24")

	second := requestTile()
	if second.Code != http.StatusNotFound {
		t.Fatalf("chart B (same id, different directory, no tile at this z/x/y): expected 404, got %d - served from chart A's cached handle instead of chart B's own file", second.Code)
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

// ── U-2: unbounded upload body ──────────────────────────────────────────

// TestUploadSatChartHandler_OverCapReturns413 is U-2's regression test:
// before the fix, uploadSatChartHandler had no http.MaxBytesReader at all,
// so a multipart body of any size would stream straight through c.FormFile
// into a temp file on SAT_CHARTS_DIR's volume - the same volume holding
// documents.sqlite, alarm-log.sqlite and the tile cache. satChartMaxUploadBytes
// is a var (not a const), same reasoning as documentMaxUploadBytes
// (documents_handlers.go): this test lowers it so it doesn't have to push
// a multi-GB body through httptest to exercise the 413 path.
func TestUploadSatChartHandler_OverCapReturns413(t *testing.T) {
	dir := setupSatChartsTest(t)

	origCap := satChartMaxUploadBytes
	satChartMaxUploadBytes = 1024
	t.Cleanup(func() { satChartMaxUploadBytes = origCap })

	content := bytes.Repeat([]byte("x"), 4096)
	c, rec := newMultipartUploadRequest(t, "/api/sat-charts", "huge.mbtiles", content)
	if err := uploadSatChartHandler(c); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", rec.Code, rec.Body.String())
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read storage dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no files left behind by a rejected oversized upload, found %d", len(entries))
	}
}

// ── U-4: unbounded metadata read ────────────────────────────────────────

// TestReadMBTilesMetadata_CapsRowCount is U-4's row-cap regression test.
// Before the fix, "SELECT name, value FROM metadata" had no LIMIT, so an
// uploaded file could pad the metadata table with junk rows ahead of the
// ones the code actually needs. This inserts 500 padding rows (under an
// allowed key, so key-filtering alone can't save it) before the required
// name/bounds/minzoom/maxzoom rows, then lowers satChartMetadataRowLimit
// far below 500: with the cap enforced, the query's LIMIT is reached
// before the required rows (inserted, and therefore read back in rowid
// order, last) are ever seen, so bounds is missing and parsing fails.
func TestReadMBTilesMetadata_CapsRowCount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "row-flood.mbtiles")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	schema := []string{
		`CREATE TABLE metadata (name TEXT, value TEXT)`,
		`CREATE TABLE tiles (zoom_level INTEGER, tile_column INTEGER, tile_row INTEGER, tile_data BLOB)`,
	}
	for _, stmt := range schema {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec schema %q: %v", stmt, err)
		}
	}
	for i := 0; i < 500; i++ {
		if _, err := db.Exec(`INSERT INTO metadata (name, value) VALUES ('format', 'png')`); err != nil {
			t.Fatalf("insert padding row %d: %v", i, err)
		}
	}
	required := [][2]string{
		{"name", "Flood Test"},
		{"bounds", "150.0,-25.0,151.0,-24.0"},
		{"minzoom", "5"},
		{"maxzoom", "5"},
	}
	for _, row := range required {
		if _, err := db.Exec(`INSERT INTO metadata (name, value) VALUES (?, ?)`, row[0], row[1]); err != nil {
			t.Fatalf("insert required row %q: %v", row[0], err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	origLimit := satChartMetadataRowLimit
	satChartMetadataRowLimit = 10
	t.Cleanup(func() { satChartMetadataRowLimit = origLimit })

	if _, err := readMBTilesMetadata(path); err == nil {
		t.Fatalf("expected an error: the required bounds/name/minzoom/maxzoom rows were inserted after 500 padding rows, so a row limit of 10 should never reach them")
	}
}

// TestReadMBTilesMetadata_CapsValueSize is U-4's value-size-cap regression
// test. Before the fix, a metadata row's value had no length limit, so a
// single row could hold an arbitrarily large payload. This lowers
// satChartMetadataValueLimit below the length of a perfectly ordinary
// bounds string: with the cap enforced, that row is excluded from the
// query's result set entirely, leaving bounds unset and parsing fails.
func TestReadMBTilesMetadata_CapsValueSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big-value.mbtiles")
	buildTestMBTiles(t, path, 5, 10, 12, "Test", "150.0,-25.0,151.0,-24.0")

	origLimit := satChartMetadataValueLimit
	satChartMetadataValueLimit = 8 // shorter than "150.0,-25.0,151.0,-24.0"
	t.Cleanup(func() { satChartMetadataValueLimit = origLimit })

	if _, err := readMBTilesMetadata(path); err == nil {
		t.Fatalf("expected an error: the bounds value is longer than the (test-lowered) value size cap and should have been excluded from the result, leaving bounds unparseable")
	}
}

// TestReadMBTilesMetadata_IgnoresUnrelatedMetadataKeys checks the other
// half of U-4's fix: only the handful of metadata keys the code actually
// reads (name, bounds, minzoom, maxzoom, format) are selected at all, so
// padding under any other key never competes for a slot in the
// row-limited result set in the first place - this holds even without
// lowering any cap.
func TestReadMBTilesMetadata_IgnoresUnrelatedMetadataKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "padded.mbtiles")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	schema := []string{
		`CREATE TABLE metadata (name TEXT, value TEXT)`,
		`CREATE TABLE tiles (zoom_level INTEGER, tile_column INTEGER, tile_row INTEGER, tile_data BLOB)`,
	}
	for _, stmt := range schema {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec schema %q: %v", stmt, err)
		}
	}
	for i := 0; i < satChartMetadataRowLimit*3; i++ {
		if _, err := db.Exec(
			`INSERT INTO metadata (name, value) VALUES (?, 'filler')`,
			fmt.Sprintf("padding-%d", i),
		); err != nil {
			t.Fatalf("insert padding row %d: %v", i, err)
		}
	}
	required := map[string]string{
		"name": "Padded Chart", "bounds": "150.0,-25.0,151.0,-24.0", "minzoom": "5", "maxzoom": "5", "format": "png",
	}
	for k, v := range required {
		if _, err := db.Exec(`INSERT INTO metadata (name, value) VALUES (?, ?)`, k, v); err != nil {
			t.Fatalf("insert required row %q: %v", k, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	entry, err := readMBTilesMetadata(path)
	if err != nil {
		t.Fatalf("readMBTilesMetadata: %v", err)
	}
	if entry.Name != "Padded Chart" {
		t.Errorf("expected name %q, got %q", "Padded Chart", entry.Name)
	}
	if entry.Bounds != [4]float64{150.0, -25.0, 151.0, -24.0} {
		t.Errorf("unexpected bounds: %v", entry.Bounds)
	}
}

// ── U-5: abandoned temp files never swept ───────────────────────────────

// TestSweepSatChartsDir_RemovesAbandonedTempUploads is U-5's regression
// test, mirroring TestSweepDocumentsDir_RemovesAbandonedTempUploads
// (documents_store_test.go): a crash, OOM kill, or restart mid-upload
// leaves an upload-*.mbtiles.tmp file behind that nothing previously swept.
func TestSweepSatChartsDir_RemovesAbandonedTempUploads(t *testing.T) {
	dir := t.TempDir()

	tmpPath := filepath.Join(dir, "upload-abc123.mbtiles.tmp")
	if err := os.WriteFile(tmpPath, []byte("partial"), 0o644); err != nil {
		t.Fatalf("write temp upload: %v", err)
	}
	// A real chart file must never be swept just because it also lives in
	// this directory.
	realPath := filepath.Join(dir, "real-chart.mbtiles")
	if err := os.WriteFile(realPath, []byte("bytes"), 0o644); err != nil {
		t.Fatalf("write real chart: %v", err)
	}

	result, err := sweepSatChartsDir(dir)
	if err != nil {
		t.Fatalf("sweepSatChartsDir: %v", err)
	}
	if result.RemovedTemp != 1 {
		t.Fatalf("expected 1 removed temp file, got %d", result.RemovedTemp)
	}
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("expected the temp upload to be removed, stat err=%v", err)
	}
	if _, err := os.Stat(realPath); err != nil {
		t.Fatalf("expected the real chart file to remain, got %v", err)
	}
}

func TestSweepSatChartsDir_MissingDirectoryIsNotAnError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist-yet")

	result, err := sweepSatChartsDir(dir)
	if err != nil {
		t.Fatalf("sweepSatChartsDir: %v", err)
	}
	if result.RemovedTemp != 0 {
		t.Fatalf("expected 0 removed temp files for a missing directory, got %d", result.RemovedTemp)
	}
}

// ── U-7: attacker-supplied SQLite opened read-write ─────────────────────

// buildWALModeMBTilesFixture writes a minimal valid MBTiles file at path,
// then switches it into WAL journal mode. journal_mode is persisted in a
// SQLite file's own header, not the connection that set it, so this stays
// in effect for whoever opens the file next - a real WAL-mode export
// toolchain would leave a file in exactly this state, and so would an
// attacker deliberately crafting one for U-7.
func buildWALModeMBTilesFixture(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
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
		"name": "WAL Chart", "bounds": "150,-25,151,-24", "minzoom": "5", "maxzoom": "5", "format": "png",
	}
	for k, v := range meta {
		if _, err := db.Exec(`INSERT INTO metadata (name, value) VALUES (?, ?)`, k, v); err != nil {
			t.Fatalf("insert metadata %q: %v", k, err)
		}
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		t.Fatalf("set WAL journal mode: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close fixture db: %v", err)
	}
}

// TestPlainSQLiteOpenOnWALModeFile_TransientlyCreatesSidecars characterizes
// the mechanism U-7 is about, independent of this package's own code: a
// bare sql.Open (what readMBTilesMetadata used to use) reading a WAL-mode
// file makes SQLite create -wal/-shm sidecars while a statement is open
// against it. A clean return auto-checkpoints and removes them again - the
// harm is a crash, OOM kill, or restart while that connection is open,
// which leaves them behind permanently (invisible to the catalog's
// ".mbtiles" filter and to sweepSatChartsDir). That means the sidecars
// have to be checked for *before* the connection closes, not after; this
// test exists so the regression test below (on the fixed DSN) has
// something real to regress against in this environment.
func TestPlainSQLiteOpenOnWALModeFile_TransientlyCreatesSidecars(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wal-mode-plain.mbtiles")
	buildWALModeMBTilesFixture(t, path)

	db, err := sql.Open("sqlite", path) // the old, unguarded form
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT name, value FROM metadata`)
	if err != nil {
		t.Fatalf("query metadata: %v", err)
	}
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("rows.Close: %v", err)
	}

	foundWAL := statExists(t, path+"-wal")
	foundSHM := statExists(t, path+"-shm")
	if !foundWAL || !foundSHM {
		t.Fatalf("expected a bare sql.Open to create -wal/-shm sidecars while reading a WAL-mode file (foundWAL=%v foundSHM=%v) - if this stops reproducing, the mode=ro&immutable=1 test below is no longer proving anything", foundWAL, foundSHM)
	}
}

// TestSatChartReadOnlyDSN_NeverCreatesWALSidecars is U-7's actual
// regression test: satChartReadOnlyDSN is the DSN both readMBTilesMetadata
// and satChartHandleCache.get open uploaded/attacker-supplied MBTiles
// bytes with. Unlike the bare form above, mode=ro&immutable=1 must never
// create -wal/-shm sidecars for a WAL-mode file, checked here while the
// connection is still open so a close-time auto-checkpoint can't hide a
// transient creation the way it would for the plain form.
func TestSatChartReadOnlyDSN_NeverCreatesWALSidecars(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wal-mode-readonly.mbtiles")
	buildWALModeMBTilesFixture(t, path)

	db, err := sql.Open("sqlite", satChartReadOnlyDSN(path))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT name, value FROM metadata`)
	if err != nil {
		t.Fatalf("query metadata: %v", err)
	}
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("rows.Close: %v", err)
	}

	if statExists(t, path+"-wal") || statExists(t, path+"-shm") {
		t.Fatalf("satChartReadOnlyDSN created a -wal/-shm sidecar while reading a WAL-mode file - expected mode=ro&immutable=1 to avoid needing WAL machinery at all (U-7)")
	}
}

func statExists(t *testing.T, path string) bool {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		return true
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", path, err)
	}
	return false
}

// ── U-6: tiles serve without nosniff/CSP ────────────────────────────────

// TestSatChartTileHandler_SetsNosniffAndCSPHeaders is U-6's regression
// test, mirroring documentContentHandler's headers (documents_handlers.go).
func TestSatChartTileHandler_SetsNosniffAndCSPHeaders(t *testing.T) {
	dir := setupSatChartsTest(t)
	id := "header-test-chart"
	buildTestMBTiles(t, filepath.Join(dir, id+".mbtiles"), 5, 10, 12, "Header Test", "150,-25,151,-24")

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
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("expected X-Content-Type-Options: nosniff, got %q", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != "sandbox" {
		t.Fatalf("expected Content-Security-Policy: sandbox, got %q", got)
	}
}
