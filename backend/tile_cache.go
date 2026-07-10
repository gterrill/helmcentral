package main

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	_ "modernc.org/sqlite"
)

// worldImagerySource is the tiles.source key for Esri World Imagery tiles
// cached through this proxy. A free-text key (rather than a fixed schema
// column per provider) so the same cache table can serve other imagery
// sources later without a schema change.
const worldImagerySource = "esri-world-imagery"

// maxDegradeLevels bounds how many zoom levels coarser than the originally
// requested tile the cache-through proxy will try before giving up and
// falling back to a blank tile, per the graceful-degradation design.
const maxDegradeLevels = 4

// maxPrefetchTiles caps the total number of tiles a single "cache this
// area" prefetch request may enqueue - generous for one lagoon entrance at
// fine zoom, but protective of the boat's uplink against an accidental
// whole-region request.
const maxPrefetchTiles = 8000

// prefetchWorkerCount is the bounded worker-pool size for a prefetch job's
// concurrent tile fetches.
const prefetchWorkerCount = 6

// globalTileCache is the process-wide tile cache instance, opened once in
// main() and passed into the handler factories at route registration time.
var globalTileCache *tileCache

// newWorldImageryHTTPClient constructs an *http.Client configured with a
// 6-second timeout for upstream Esri World Imagery tile fetches. This timeout
// ensures that slow or hanging upstream requests fail fast into the graceful
// degradation path, rather than indefinitely blocking live tile requests or
// stalling the prefetch worker pool.
func newWorldImageryHTTPClient() *http.Client {
	return &http.Client{Timeout: 6 * time.Second}
}

// tileFetcher is the minimal interface the world-imagery proxy needs from
// an HTTP client, extracted so tests can inject a fake upstream instead of
// hitting the real Esri endpoint. *http.Client already satisfies this
// interface, so http.DefaultClient can be passed directly with no adapter.
type tileFetcher interface {
	Do(req *http.Request) (*http.Response, error)
}

// tileCache is a SQLite-backed cache-through store for proxied map tiles.
// There is no TTL/expiry - satellite basemap imagery doesn't change on
// human timescales (the same reasoning sat_charts.go already applies to
// uploaded MBTiles packages), so a tile is cached forever once fetched.
// The DELETE /api/world-imagery/cache endpoint is the explicit escape
// hatch if a cached result ever needs to be cleared (e.g. once Esri adds
// coverage where a blank/degraded result was previously cached).
type tileCache struct {
	db *sql.DB
}

func tileCachePath() string {
	return cacheFilePath("TILE_CACHE_PATH", "data/tile-cache.sqlite")
}

func newTileCache(dbPath string) (*tileCache, error) {
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create tile cache directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open tile cache database: %w", err)
	}
	// The prefetch worker pool (Section 2) hits this same *sql.DB
	// concurrently from several goroutines. SQLite only allows one writer
	// at a time and modernc.org/sqlite's default busy behavior surfaces
	// that as a "database is locked" error rather than waiting - observed
	// directly via TestPrefetchWorldImageryJob_RunsToCompletionWithCorrectCounts
	// failing intermittently with SQLITE_BUSY before this was added.
	// Capping the pool at one connection makes database/sql itself queue
	// concurrent callers instead, which is simplest-correct at this app's
	// scale (a personal dashboard / small fleet, same reasoning sat_charts.go
	// documents for its own SQLite usage).
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS tiles (
		source TEXT NOT NULL,
		z INTEGER NOT NULL,
		x INTEGER NOT NULL,
		y INTEGER NOT NULL,
		content_type TEXT NOT NULL,
		data BLOB NOT NULL,
		fetched_at INTEGER NOT NULL,
		PRIMARY KEY (source, z, x, y)
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create tiles table: %w", err)
	}

	return &tileCache{db: db}, nil
}

func (tc *tileCache) close() error {
	return tc.db.Close()
}

func (tc *tileCache) get(source string, z, x, y int) (data []byte, contentType string, ok bool, err error) {
	row := tc.db.QueryRow(
		`SELECT content_type, data FROM tiles WHERE source = ? AND z = ? AND x = ? AND y = ?`,
		source, z, x, y,
	)
	if err := row.Scan(&contentType, &data); err != nil {
		if err == sql.ErrNoRows {
			return nil, "", false, nil
		}
		return nil, "", false, fmt.Errorf("read cached tile: %w", err)
	}
	return data, contentType, true, nil
}

func (tc *tileCache) put(source string, z, x, y int, data []byte, contentType string) error {
	_, err := tc.db.Exec(
		`INSERT INTO tiles (source, z, x, y, content_type, data, fetched_at) VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(source, z, x, y) DO UPDATE SET
			content_type = excluded.content_type,
			data = excluded.data,
			fetched_at = excluded.fetched_at`,
		source, z, x, y, contentType, data, time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("store cached tile: %w", err)
	}
	return nil
}

func (tc *tileCache) clear() error {
	if _, err := tc.db.Exec(`DELETE FROM tiles`); err != nil {
		return fmt.Errorf("clear tile cache: %w", err)
	}
	return nil
}

// fetchWorldImageryUpstream performs a single upstream Esri World Imagery
// tile fetch via the injected fetcher. A non-200 response, transport
// error, or body-read error are all treated the same way (matching
// tile_proxy.go's pre-existing blanket "anything not a clean 200 falls
// through" behavior) and reported as an error to the caller, which decides
// what to do next (degrade to a coarser zoom, or give up).
func fetchWorldImageryUpstream(fetcher tileFetcher, z, x, y int) ([]byte, string, error) {
	tileURL := fmt.Sprintf(
		"https://services.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/%d/%d/%d",
		z, y, x,
	)

	req, err := http.NewRequest(http.MethodGet, tileURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("build upstream request: %w", err)
	}
	req.Header.Set("User-Agent", "helmcentral/1.0")

	resp, err := fetcher.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("upstream request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read upstream body: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}
	return body, contentType, nil
}

// resolveWorldImageryTile implements the cache-through + graceful-
// degradation semantics shared by both the live tile proxy and the
// prefetch worker pool:
//  1. Cache hit at the requested (source,z,x,y) -> serve immediately, no
//     upstream call.
//  2. Cache miss -> fetch upstream. Success -> cache and serve.
//  3. Upstream failure -> retry at (z-1,x>>1,y>>1), then (z-2,...), up to
//     maxDegradeLevels coarser levels, each checking the cache first (a
//     coarser-level cache hit does not invoke the fetcher). The first
//     level that yields bytes (from cache or a successful fetch) is
//     served, and is ALSO cached under the originally-requested key so a
//     repeat request for the same missing deep tile doesn't re-walk the
//     fallback chain.
//  4. If every level is exhausted (or z runs below 0), fall back to the
//     transparent blank tile, and cache that blank result under the
//     original key too - deliberately no TTL, matching the rest of this
//     cache; DELETE /api/world-imagery/cache is the explicit escape hatch
//     to clear a stale blank result later.
//
// Cache read/write errors (infrastructure failures, not upstream-imagery
// availability) are surfaced to the caller rather than masked, per the
// fail-fast policy - only the upstream-availability path degrades.
func resolveWorldImageryTile(cache *tileCache, fetcher tileFetcher, source string, z, x, y int) ([]byte, string, error) {
	if data, contentType, ok, err := cache.get(source, z, x, y); err != nil {
		return nil, "", err
	} else if ok {
		return data, contentType, nil
	}

	if data, contentType, err := fetchWorldImageryUpstream(fetcher, z, x, y); err == nil {
		if err := cache.put(source, z, x, y, data, contentType); err != nil {
			return nil, "", err
		}
		return data, contentType, nil
	}

	cz, cx, cy := z, x, y
	for level := 1; level <= maxDegradeLevels; level++ {
		cz, cx, cy = cz-1, cx>>1, cy>>1
		if cz < 0 {
			break
		}

		if data, contentType, ok, err := cache.get(source, cz, cx, cy); err != nil {
			return nil, "", err
		} else if ok {
			if err := cache.put(source, z, x, y, data, contentType); err != nil {
				return nil, "", err
			}
			return data, contentType, nil
		}

		if data, contentType, err := fetchWorldImageryUpstream(fetcher, cz, cx, cy); err == nil {
			if err := cache.put(source, cz, cx, cy, data, contentType); err != nil {
				return nil, "", err
			}
			if err := cache.put(source, z, x, y, data, contentType); err != nil {
				return nil, "", err
			}
			return data, contentType, nil
		}
	}

	if err := cache.put(source, z, x, y, transparentPNG1x1, "image/png"); err != nil {
		return nil, "", err
	}
	return transparentPNG1x1, "image/png", nil
}

// lonToTileX and latToTileY are the standard slippy-map bbox<->tile
// conversions (see e.g. the OSM wiki's "Slippy map tilenames"). No
// antimeridian-crossing handling - out of scope for this feature.
func lonToTileX(lon float64, z int) int {
	n := math.Pow(2, float64(z))
	return int(math.Floor((lon + 180.0) / 360.0 * n))
}

func latToTileY(lat float64, z int) int {
	n := math.Pow(2, float64(z))
	latRad := lat * math.Pi / 180.0
	return int(math.Floor((1.0 - math.Log(math.Tan(latRad)+1.0/math.Cos(latRad))/math.Pi) / 2.0 * n))
}

type tileCoord struct {
	z, x, y int
}

// zoomTileRange returns the inclusive x/y tile-index range covering the
// given bbox at a single zoom level.
func zoomTileRange(west, south, east, north float64, z int) (xMin, xMax, yMin, yMax int) {
	xMin = lonToTileX(west, z)
	xMax = lonToTileX(east, z)
	// North maps to a smaller Y (Y increases southward in XYZ tile space).
	yMin = latToTileY(north, z)
	yMax = latToTileY(south, z)
	if xMin > xMax {
		xMin, xMax = xMax, xMin
	}
	if yMin > yMax {
		yMin, yMax = yMax, yMin
	}
	return xMin, xMax, yMin, yMax
}

// countTilesForBBox computes the total tile count across [minZoom,maxZoom]
// using only arithmetic (no per-tile iteration), so it stays cheap even
// for a wildly over-cap request - the whole point is to reject those
// before ever building a tile list.
func countTilesForBBox(west, south, east, north float64, minZoom, maxZoom int) int {
	total := 0
	for z := minZoom; z <= maxZoom; z++ {
		xMin, xMax, yMin, yMax := zoomTileRange(west, south, east, north, z)
		total += (xMax - xMin + 1) * (yMax - yMin + 1)
	}
	return total
}

// tilesForBBox builds the actual tile coordinate list across
// [minZoom,maxZoom]. Only called once countTilesForBBox has already
// confirmed the total is within the prefetch cap.
func tilesForBBox(west, south, east, north float64, minZoom, maxZoom int) []tileCoord {
	var tiles []tileCoord
	for z := minZoom; z <= maxZoom; z++ {
		xMin, xMax, yMin, yMax := zoomTileRange(west, south, east, north, z)
		for x := xMin; x <= xMax; x++ {
			for y := yMin; y <= yMax; y++ {
				tiles = append(tiles, tileCoord{z: z, x: x, y: y})
			}
		}
	}
	return tiles
}

// prefetchJob tracks one "cache this area" background prefetch job's
// progress. In-memory only (not persisted to disk) - a job's progress
// doesn't need to survive a server restart, unlike routes/dashboard-pages
// state.
type prefetchJob struct {
	ID    string
	Total int
	Done  int32

	mu       sync.RWMutex
	complete bool
}

func (j *prefetchJob) addDone(n int32) {
	done := atomic.AddInt32(&j.Done, n)
	if int(done) >= j.Total {
		j.mu.Lock()
		j.complete = true
		j.mu.Unlock()
	}
}

func (j *prefetchJob) snapshot() (total, done int, complete bool) {
	j.mu.RLock()
	complete = j.complete
	j.mu.RUnlock()
	return j.Total, int(atomic.LoadInt32(&j.Done)), complete
}

var (
	prefetchJobsMu sync.RWMutex
	prefetchJobs   = map[string]*prefetchJob{}
)

func registerPrefetchJob(job *prefetchJob) {
	prefetchJobsMu.Lock()
	prefetchJobs[job.ID] = job
	prefetchJobsMu.Unlock()
}

func lookupPrefetchJob(id string) (*prefetchJob, bool) {
	prefetchJobsMu.RLock()
	job, ok := prefetchJobs[id]
	prefetchJobsMu.RUnlock()
	return job, ok
}

// runPrefetchJob fetches every tile in tiles through the same cache-through
// + degradation logic as the live proxy (resolveWorldImageryTile), using a
// bounded worker pool. Completed tiles are immediately servable through the
// regular tile endpoint mid-job, since they land in the same cache.
func runPrefetchJob(job *prefetchJob, cache *tileCache, fetcher tileFetcher, tiles []tileCoord) {
	if len(tiles) == 0 {
		job.mu.Lock()
		job.complete = true
		job.mu.Unlock()
		return
	}

	tileCh := make(chan tileCoord)
	var wg sync.WaitGroup
	for i := 0; i < prefetchWorkerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range tileCh {
				if _, _, err := resolveWorldImageryTile(cache, fetcher, worldImagerySource, t.z, t.x, t.y); err != nil {
					log.Printf("prefetch job %s: tile z=%d x=%d y=%d failed: %v", job.ID, t.z, t.x, t.y, err)
				}
				job.addDone(1)
			}
		}()
	}

	for _, t := range tiles {
		tileCh <- t
	}
	close(tileCh)
	wg.Wait()
}

type prefetchRequestBody struct {
	West    float64 `json:"west"`
	South   float64 `json:"south"`
	East    float64 `json:"east"`
	North   float64 `json:"north"`
	MinZoom int     `json:"minZoom"`
	MaxZoom int     `json:"maxZoom"`
}

// prefetchWorldImageryHandler is the POST /api/world-imagery/prefetch
// handler factory: computes the tile count for the requested bbox/zoom
// range, rejects it with the computed count if over maxPrefetchTiles, or
// otherwise kicks off a background prefetch job and returns its id.
func prefetchWorldImageryHandler(cache *tileCache, fetcher tileFetcher) echo.HandlerFunc {
	return func(c echo.Context) error {
		var body prefetchRequestBody
		if err := c.Bind(&body); err != nil {
			return c.NoContent(http.StatusBadRequest)
		}
		if body.MaxZoom < body.MinZoom || body.MinZoom < 0 {
			return c.NoContent(http.StatusBadRequest)
		}

		total := countTilesForBBox(body.West, body.South, body.East, body.North, body.MinZoom, body.MaxZoom)
		if total > maxPrefetchTiles {
			return c.JSON(http.StatusBadRequest, map[string]any{
				"error":      fmt.Sprintf("area too large: %d tiles (max %d)", total, maxPrefetchTiles),
				"totalTiles": total,
			})
		}

		tiles := tilesForBBox(body.West, body.South, body.East, body.North, body.MinZoom, body.MaxZoom)
		job := &prefetchJob{ID: uuid.NewString(), Total: len(tiles)}
		registerPrefetchJob(job)

		go runPrefetchJob(job, cache, fetcher, tiles)

		return c.JSON(http.StatusAccepted, map[string]any{
			"jobId":      job.ID,
			"totalTiles": job.Total,
		})
	}
}

// prefetchStatusHandler is the GET /api/world-imagery/prefetch/:jobId
// handler factory.
func prefetchStatusHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		jobID := c.Param("jobId")
		job, ok := lookupPrefetchJob(jobID)
		if !ok {
			return c.NoContent(http.StatusNotFound)
		}

		total, done, complete := job.snapshot()
		return c.JSON(http.StatusOK, map[string]any{
			"total":    total,
			"done":     done,
			"complete": complete,
		})
	}
}

// deleteWorldImageryCacheHandler is the DELETE /api/world-imagery/cache
// escape hatch: clears every cached tile (across all sources), e.g. to
// force a re-fetch after Esri adds coverage where a blank/degraded result
// was previously cached.
func deleteWorldImageryCacheHandler(cache *tileCache) echo.HandlerFunc {
	return func(c echo.Context) error {
		if err := cache.clear(); err != nil {
			return err
		}
		return c.NoContent(http.StatusOK)
	}
}
