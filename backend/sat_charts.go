package main

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	_ "modernc.org/sqlite"
)

type satChartCatalogEntry struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Bounds    [4]float64 `json:"bounds"` // [west, south, east, north]
	MinZoom   int        `json:"minzoom"`
	MaxZoom   int        `json:"maxzoom"`
	Format    string     `json:"format"`
	SizeBytes int64      `json:"size_bytes"`
}

// satChartMaxUploadBytes caps one upload's total request body (U-2): the
// only body cap anywhere else in the backend is documentMaxUploadBytes
// (documents_handlers.go), scoped to /api/documents, so without this one
// sat-charts had none at all. MBTiles satellite chart exports are
// legitimately large - a single region's imagery pyramid taken out to a
// useful zoom level routinely runs into the multiple-GB range - so this
// can't be anywhere near as tight as the document cap. 4 GiB is chosen as
// generous headroom for a real chart while still being a hard backstop
// against an unbounded body filling the disk that also holds
// documents.sqlite, alarm-log.sqlite, and the tile cache. A var, not a
// const: TestUploadSatChartHandler_OverCapReturns413 lowers it for the
// duration of one test so it doesn't have to actually push 4GB of body
// through httptest to exercise the 413 path.
var satChartMaxUploadBytes int64 = 4 << 30 // 4 GiB

// satChartMetadataRowLimit and satChartMetadataValueLimit bound how much
// of an uploaded file's own metadata table readMBTilesMetadata will read
// (U-4): that table is attacker-controlled bytes, read fresh on every
// listSatChartsHandler call (there is no cache for it, unlike tile reads -
// satChartHandleCache covers only satChartTileHandler), so an uploaded
// file padded with a huge metadata table would otherwise be re-parsed into
// memory on every dashboard load. Real MBTiles metadata for the five keys
// this code reads (name, bounds, minzoom, maxzoom, format) is a handful of
// rows well under a few hundred bytes each; both bounds are generous
// relative to that. vars, not consts: tests lower them rather than writing
// megabytes of fixture data to exercise the caps.
var (
	satChartMetadataRowLimit   = 100
	satChartMetadataValueLimit = 4096 // bytes
)

func satChartsDirPath() string {
	return cacheFilePath("SAT_CHARTS_DIR", "data/sat-charts")
}

// satChartReadOnlyDSN is the DSN every read of an uploaded MBTiles file's
// own SQLite bytes must use - readMBTilesMetadata below and
// satChartHandleCache.get further down this file (U-7). The file is
// attacker-supplied (freshly uploaded, or in readMBTilesMetadata's case
// sometimes not yet proven to even be a real MBTiles export) and must
// never be opened read-write: without mode=ro, SQLite may write to it -
// e.g. to roll back a stale journal, or to satisfy WAL's locking protocol
// if the file's own header says journal_mode=WAL - creating -wal/-shm
// sidecars next to it that neither the catalog listing's ".mbtiles"
// filter nor sweepSatChartsDir know to look for, and meaning the bytes
// later renamed into the catalog aren't necessarily the bytes uploaded.
//
// The "file:" prefix is required: without it, modernc.org/sqlite's DSN
// parser strips everything from the first "?" onward before handing the
// string to sqlite3_open_v2, so mode=ro and immutable=1 would otherwise be
// silently dropped (see modernc.org/sqlite's newConn). mode=ro opens
// read-only regardless of the Go-level open flags; immutable=1
// additionally tells SQLite the file will never change out from under
// this connection, letting it skip locking calls, change detection, and
// WAL-index setup it would otherwise do on every query. That's safe here
// because nothing else touches these bytes while they're being read this
// way: satChartHandleCache's own doc comment covers why for the catalog
// path, and readMBTilesMetadata's callers never mutate tmpPath or an
// already-renamed chart file while a read is in flight either.
func satChartReadOnlyDSN(path string) string {
	return "file:" + path + "?mode=ro&immutable=1"
}

// satChartSweepResult is sweepSatChartsDir's report: how many abandoned
// upload temp files it removed. Returned (not just logged) so the boot
// sweep's behavior is directly assertable in tests without scraping log
// output, mirroring documentSweepResult (documents_store.go).
type satChartSweepResult struct {
	RemovedTemp int
}

// sweepSatChartsDir runs once at boot (main.go, next to sweepDocumentsDir):
// it removes abandoned upload-*.mbtiles.tmp files left behind by a crash,
// OOM kill, or restart mid-upload (U-5) - uploadSatChartHandler's own
// deferred os.Remove never got to run for them, the catalog listing's
// ".mbtiles" suffix filter can't see them to report them either, and
// DELETE can't name a chart id that was never assigned. Unlike
// sweepDocumentsDir there is no separate catalog to cross-check disk
// against - each chart's own MBTiles metadata table *is* the catalog (ADR
// 0011) - so there's no orphan/missing comparison to make here, only the
// temp files to remove.
func sweepSatChartsDir(dir string) (satChartSweepResult, error) {
	var result satChartSweepResult

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return result, fmt.Errorf("sweep sat charts dir: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "upload-") && strings.HasSuffix(name, ".mbtiles.tmp") {
			if err := os.Remove(filepath.Join(dir, name)); err != nil {
				return result, fmt.Errorf("sweep sat charts dir: remove %s: %w", name, err)
			}
			result.RemovedTemp++
		}
	}

	return result, nil
}

func satChartFilePath(id string) string {
	return filepath.Join(satChartsDirPath(), id+".mbtiles")
}

func isValidSatChartID(id string) bool {
	return id != "" && !strings.ContainsAny(id, "/\\") && !strings.Contains(id, "..")
}

// xyzRowToTMSRow converts a Google/Slippy-map (XYZ) tile row, where Y=0 is
// the top of the world, to the TMS row convention MBTiles uses, where Y=0
// is the bottom. Per the MBTiles spec: tms_row = (2^z - 1) - xyz_row. The
// same formula converts in the other direction too (it's self-inverse).
func xyzRowToTMSRow(z, xyzRow int) int {
	return (1 << z) - 1 - xyzRow
}

func verifyMBTilesSchema(db *sql.DB) error {
	for _, table := range []string{"tiles", "metadata"} {
		var name string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err == sql.ErrNoRows {
			return fmt.Errorf("missing required MBTiles table %q", table)
		}
		if err != nil {
			return fmt.Errorf("checking for table %q: %w", table, err)
		}
	}
	return nil
}

func parseMBTilesBounds(raw string) ([4]float64, error) {
	parts := strings.Split(raw, ",")
	if len(parts) != 4 {
		return [4]float64{}, fmt.Errorf("invalid bounds %q: expected 4 comma-separated values", raw)
	}
	var out [4]float64
	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return [4]float64{}, fmt.Errorf("invalid bounds value %q: %w", p, err)
		}
		out[i] = v
	}
	return out, nil
}

func readMBTilesMetadata(dbPath string) (satChartCatalogEntry, error) {
	db, err := sql.Open("sqlite", satChartReadOnlyDSN(dbPath))
	if err != nil {
		return satChartCatalogEntry{}, fmt.Errorf("open sqlite: %w", err)
	}
	defer db.Close()
	// This function opens and closes its own connection per call rather
	// than sharing satChartHandleCache's - unlike that cache, which bounds
	// a pool shared across many concurrent tile requests for one chart,
	// only one connection is ever needed here.
	db.SetMaxOpenConns(1)

	if err := verifyMBTilesSchema(db); err != nil {
		return satChartCatalogEntry{}, err
	}

	// U-4: only the keys this function actually reads below, a row LIMIT
	// past what any real MBTiles file needs for them, and a per-value size
	// cap via length(value) in the WHERE clause - filtered by SQLite before
	// a value is ever handed back to Go, so an oversized value's bytes are
	// never materialized here at all rather than being read and discarded.
	meta := map[string]string{}
	rows, err := db.Query(
		`SELECT name, value FROM metadata
		 WHERE name IN ('name', 'bounds', 'minzoom', 'maxzoom', 'format')
		   AND length(value) <= ?
		 LIMIT ?`,
		satChartMetadataValueLimit, satChartMetadataRowLimit,
	)
	if err != nil {
		return satChartCatalogEntry{}, fmt.Errorf("query metadata: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return satChartCatalogEntry{}, fmt.Errorf("scan metadata row: %w", err)
		}
		meta[k] = v
	}
	if err := rows.Err(); err != nil {
		return satChartCatalogEntry{}, err
	}

	bounds, err := parseMBTilesBounds(meta["bounds"])
	if err != nil {
		return satChartCatalogEntry{}, err
	}

	minZoom, _ := strconv.Atoi(meta["minzoom"])
	maxZoom, _ := strconv.Atoi(meta["maxzoom"])
	format := strings.TrimSpace(meta["format"])
	if format == "" {
		format = "png"
	}

	return satChartCatalogEntry{
		Name:    strings.TrimSpace(meta["name"]),
		Bounds:  bounds,
		MinZoom: minZoom,
		MaxZoom: maxZoom,
		Format:  format,
	}, nil
}

func tileContentType(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	default:
		return "image/png"
	}
}

// POST /api/sat-charts
func uploadSatChartHandler(c echo.Context) error {
	// U-2: cap the request body before anything parses it - the same
	// http.MaxBytesReader approach uploadDocumentHandler uses
	// (documents_handlers.go), which was previously the only body cap
	// anywhere in the backend. Without this, c.FormFile below spills an
	// unbounded body into a temp file via ParseMultipartForm before this
	// handler gets any say in the matter.
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, satChartMaxUploadBytes)

	fileHeader, err := c.FormFile("file")
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return c.JSON(http.StatusRequestEntityTooLarge, map[string]string{
				"error": fmt.Sprintf("upload exceeds the %d byte limit", satChartMaxUploadBytes),
			})
		}
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "missing file field"})
	}

	src, err := fileHeader.Open()
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "could not open uploaded file"})
	}
	defer src.Close()

	dir := satChartsDirPath()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to prepare storage"})
	}

	// Stream to a temp file in the same directory first, so a bad/partial
	// upload never lands in the catalog at all and the final rename is
	// atomic (same filesystem).
	tmpFile, err := os.CreateTemp(dir, "upload-*.mbtiles.tmp")
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create temp file"})
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath) // no-op once successfully renamed

	if _, err := io.Copy(tmpFile, src); err != nil {
		tmpFile.Close()
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to save upload"})
	}
	if err := tmpFile.Close(); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to save upload"})
	}

	entry, err := readMBTilesMetadata(tmpPath)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "not a valid MBTiles file: " + err.Error()})
	}

	id := uuid.NewString()
	if entry.Name == "" {
		entry.Name = strings.TrimSuffix(fileHeader.Filename, filepath.Ext(fileHeader.Filename))
	}
	entry.ID = id

	finalPath := satChartFilePath(id)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to store chart"})
	}

	if info, statErr := os.Stat(finalPath); statErr == nil {
		entry.SizeBytes = info.Size()
	}

	return c.JSON(http.StatusCreated, entry)
}

// GET /api/sat-charts
func listSatChartsHandler(c echo.Context) error {
	dir := satChartsDirPath()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return c.JSON(http.StatusOK, map[string]any{"charts": []satChartCatalogEntry{}})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to list charts"})
	}

	charts := make([]satChartCatalogEntry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".mbtiles") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".mbtiles")
		path := filepath.Join(dir, e.Name())

		entry, err := readMBTilesMetadata(path)
		if err != nil {
			// One corrupt chart shouldn't take down visibility of every
			// other valid one (see docs/adr/0011), but it's logged loudly
			// so the corruption is never silently hidden.
			log.Printf("sat-charts: skipping unreadable chart %q: %v", e.Name(), err)
			continue
		}
		entry.ID = id
		if entry.Name == "" {
			entry.Name = id
		}
		if info, statErr := e.Info(); statErr == nil {
			entry.SizeBytes = info.Size()
		}
		charts = append(charts, entry)
	}

	return c.JSON(http.StatusOK, map[string]any{"charts": charts})
}

// DELETE /api/sat-charts/:id
func deleteSatChartHandler(c echo.Context) error {
	id := c.Param("id")
	if !isValidSatChartID(id) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid chart id"})
	}

	path := satChartFilePath(id)
	if _, err := os.Stat(path); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "chart not found"})
	}
	if err := os.Remove(path); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to delete chart"})
	}
	globalSatChartHandles.invalidate(id)
	return c.JSON(http.StatusOK, map[string]string{"status": "deleted"})
}

// satChartHandle is one chart's cached read-only MBTiles connection plus its
// format string, read once when the handle is opened rather than on every
// tile request.
type satChartHandle struct {
	db     *sql.DB
	format string
}

// satChartHandleCache caches one open satChartHandle per chart file path, so
// satChartTileHandler doesn't os.Stat, sql.Open, run the tile query, run a
// second query just for the format string, then close, for every single
// tile request (backend perf audit Tier 3). Keyed by satChartFilePath(id) -
// the resolved absolute path - rather than the bare id: SAT_CHARTS_DIR can
// change (a test's own t.TempDir() per test function; in production,
// HELMCENTRAL_STATE_DIR being repointed), and two charts that happen to
// share an id string but live under different directories must not serve
// each other's tiles from a stale cached handle. Charts are installed by
// uploadSatChartHandler renaming a freshly-written temp file into place
// under a freshly generated uuid id (satChartTileHandler above) - never
// rewritten in place at an existing path - so mode=ro plus immutable=1 is
// safe: nothing this process does, or any other process should be doing,
// changes a chart's bytes once it exists at that path. deleteSatChartHandler
// invalidates (closes and drops) the entry for a path that's removed, so a
// deleted chart never keeps serving from a stale handle.
type satChartHandleCache struct {
	mu      sync.Mutex
	handles map[string]*satChartHandle
}

var globalSatChartHandles = &satChartHandleCache{handles: make(map[string]*satChartHandle)}

// get returns the cached handle for id, opening and caching one on a miss.
// A missing underlying file is reported the same way it always was: a plain
// os.Stat failure, before ever trying to open it as SQLite.
func (c *satChartHandleCache) get(id string) (*satChartHandle, error) {
	path := satChartFilePath(id)

	c.mu.Lock()
	if h, ok := c.handles[path]; ok {
		c.mu.Unlock()
		return h, nil
	}
	c.mu.Unlock()

	if _, err := os.Stat(path); err != nil {
		return nil, err
	}

	// satChartReadOnlyDSN's doc comment (above, near satChartsDirPath)
	// covers why mode=ro&immutable=1 is required; safe here specifically
	// per this type's own doc comment above.
	db, err := sql.Open("sqlite", satChartReadOnlyDSN(path))
	if err != nil {
		return nil, err
	}
	// Concurrent tile requests for the same chart share this one handle;
	// bounding the pool keeps that from opening an unbounded number of OS
	// file descriptors against one chart file under a burst of requests,
	// same reasoning as tileCacheMaxOpenConns (tile_cache.go).
	db.SetMaxOpenConns(4)

	var format string
	_ = db.QueryRow("SELECT value FROM metadata WHERE name = 'format'").Scan(&format)

	h := &satChartHandle{db: db, format: format}

	c.mu.Lock()
	// Another request may have opened and cached this same path while this
	// one was doing the work above; keep whichever handle won the race and
	// close the other rather than leaking it.
	if existing, ok := c.handles[path]; ok {
		c.mu.Unlock()
		db.Close()
		return existing, nil
	}
	c.handles[path] = h
	c.mu.Unlock()
	return h, nil
}

// invalidate closes and drops the cached handle for id's chart file, if
// any. Called by deleteSatChartHandler so a removed chart never keeps
// serving tiles from a handle opened before the delete.
func (c *satChartHandleCache) invalidate(id string) {
	path := satChartFilePath(id)
	c.mu.Lock()
	h, ok := c.handles[path]
	delete(c.handles, path)
	c.mu.Unlock()
	if ok {
		h.db.Close()
	}
}

// GET /api/sat-charts/:id/:z/:x/:y
func satChartTileHandler(c echo.Context) error {
	id := c.Param("id")
	if !isValidSatChartID(id) {
		return c.NoContent(http.StatusBadRequest)
	}

	z, zErr := strconv.Atoi(c.Param("z"))
	x, xErr := strconv.Atoi(c.Param("x"))
	y, yErr := strconv.Atoi(c.Param("y"))
	if zErr != nil || xErr != nil || yErr != nil {
		return c.NoContent(http.StatusBadRequest)
	}

	handle, err := globalSatChartHandles.get(id)
	if err != nil {
		return c.NoContent(http.StatusNotFound)
	}

	tmsRow := xyzRowToTMSRow(z, y)

	var tileData []byte
	err = handle.db.QueryRow(
		"SELECT tile_data FROM tiles WHERE zoom_level = ? AND tile_column = ? AND tile_row = ?",
		z, x, tmsRow,
	).Scan(&tileData)
	if err != nil {
		return c.NoContent(http.StatusNotFound)
	}

	// U-6: tileData is bytes straight out of an uploaded SQLite file, and
	// tileContentType derives from that same file's own metadata.format -
	// both attacker-controlled. tileContentType can only ever emit
	// image/png|jpeg|webp, so this isn't an XSS vector, but the headers
	// documentContentHandler sets deliberately for the same reason
	// (documents_handlers.go) were simply absent here.
	header := c.Response().Header()
	header.Set("Cache-Control", "public, max-age=604800, immutable")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", "sandbox")
	return c.Blob(http.StatusOK, tileContentType(handle.format), tileData)
}
