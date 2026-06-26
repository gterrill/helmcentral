package main

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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

func satChartsDirPath() string {
	return cacheFilePath("SAT_CHARTS_DIR", "data/sat-charts")
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
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return satChartCatalogEntry{}, fmt.Errorf("open sqlite: %w", err)
	}
	defer db.Close()

	if err := verifyMBTilesSchema(db); err != nil {
		return satChartCatalogEntry{}, err
	}

	meta := map[string]string{}
	rows, err := db.Query("SELECT name, value FROM metadata")
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
	fileHeader, err := c.FormFile("file")
	if err != nil {
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
	return c.JSON(http.StatusOK, map[string]string{"status": "deleted"})
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

	path := satChartFilePath(id)
	if _, err := os.Stat(path); err != nil {
		return c.NoContent(http.StatusNotFound)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return c.NoContent(http.StatusNotFound)
	}
	defer db.Close()

	tmsRow := xyzRowToTMSRow(z, y)

	var tileData []byte
	err = db.QueryRow(
		"SELECT tile_data FROM tiles WHERE zoom_level = ? AND tile_column = ? AND tile_row = ?",
		z, x, tmsRow,
	).Scan(&tileData)
	if err != nil {
		return c.NoContent(http.StatusNotFound)
	}

	var format string
	_ = db.QueryRow("SELECT value FROM metadata WHERE name = 'format'").Scan(&format)

	c.Response().Header().Set("Cache-Control", "public, max-age=604800, immutable")
	return c.Blob(http.StatusOK, tileContentType(format), tileData)
}
