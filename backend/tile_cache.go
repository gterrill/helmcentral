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

// cartoBasemapSource is the tiles.source key for Carto vector basemap tiles
// (.mvt), stored in the same table as worldImagerySource above - the
// source column exists precisely so a second tile provider needs no schema
// change. One value, not two: both Carto styles this app loads (Positron
// light, Dark Matter dark) declare exactly one vector source, "carto",
// and it is the same tile data either way - only the style document (which
// this proxy also caches, in basemap_assets below) decides how those tiles
// get colored. There is no light/dark split at the tile level.
const cartoBasemapSource = "carto-basemap"

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

	// basemap_assets holds the four Carto basemap asset classes that are
	// NOT (z,x,y)-addressable, so they don't fit the tiles table above:
	// style.json (x2, one per theme), tiles.json, glyph PBFs, and sprite
	// files. "path" is this proxy's own request path (e.g.
	// "/api/basemap/style/positron"), which already uniquely identifies an
	// asset - no need for a second addressing scheme on top of it. Same
	// no-TTL reasoning as tiles: none of these change on human timescales,
	// so a fetched-once asset is cached forever until an explicit clear.
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS basemap_assets (
		path TEXT PRIMARY KEY,
		content_type TEXT NOT NULL,
		data BLOB NOT NULL,
		fetched_at INTEGER NOT NULL
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create basemap_assets table: %w", err)
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

// getBasemapAsset / putBasemapAsset mirror tileCache's get/put above, but
// keyed on the basemap_assets table's free-text "path" primary key instead
// of (source, z, x, y). Kept as separate methods rather than generalizing
// the tile methods over both tables: the two call sites (resolveCartoVectorTile
// vs. resolveBasemapDocument in basemap_proxy.go) already have genuinely
// different resolution semantics (cache-through-with-404-on-miss vs.
// cache-through-with-502-on-miss), so sharing a key-shaped storage method is
// as far as that unification usefully goes.
func (tc *tileCache) getBasemapAsset(path string) (data []byte, contentType string, ok bool, err error) {
	row := tc.db.QueryRow(`SELECT content_type, data FROM basemap_assets WHERE path = ?`, path)
	if err := row.Scan(&contentType, &data); err != nil {
		if err == sql.ErrNoRows {
			return nil, "", false, nil
		}
		return nil, "", false, fmt.Errorf("read cached basemap asset: %w", err)
	}
	return data, contentType, true, nil
}

func (tc *tileCache) putBasemapAsset(path string, data []byte, contentType string) error {
	_, err := tc.db.Exec(
		`INSERT INTO basemap_assets (path, content_type, data, fetched_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(path) DO UPDATE SET
			content_type = excluded.content_type,
			data = excluded.data,
			fetched_at = excluded.fetched_at`,
		path, contentType, data, time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("store cached basemap asset: %w", err)
	}
	return nil
}

// clear wipes both the tiles table (Esri imagery + Carto vector basemap
// tiles) and basemap_assets (style/tilejson/glyph/sprite documents).
// Deliberate, not an oversight, that DELETE /api/world-imagery/cache below
// clears both: a stale basemap_assets entry has exactly the same shape as a
// stale tile - no TTL, cached forever until told otherwise - so a second,
// narrower escape hatch just for basemap_assets would be one more thing to
// remember to also clear, for no real operational benefit. A full offline
// basemap re-seed after this is one "cache this area" prefetch away either
// way (Section 5 below), same as it already is for imagery.
func (tc *tileCache) clear() error {
	if _, err := tc.db.Exec(`DELETE FROM tiles`); err != nil {
		return fmt.Errorf("clear tile cache: %w", err)
	}
	if _, err := tc.db.Exec(`DELETE FROM basemap_assets`); err != nil {
		return fmt.Errorf("clear basemap assets cache: %w", err)
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

// cartoVectorTileHosts are the 4 subdomains Carto shards vector tile
// requests across, confirmed directly against the "tiles" array in the
// upstream tiles.json response (tiles-a/b/c/d.basemaps.cartocdn.com).
// Picking a host deterministically from the tile coordinate spreads
// prefetch's concurrent worker-pool load the way a real panning client
// would, rather than hammering a single subdomain from every worker.
var cartoVectorTileHosts = [...]string{"tiles-a", "tiles-b", "tiles-c", "tiles-d"}

// fetchCartoVectorTileUpstream performs a single upstream Carto vector
// tile (.mvt) fetch. Same blanket "non-200/transport/body-read error all
// become one error" shape as fetchWorldImageryUpstream above - the caller
// (resolveCartoVectorTile) decides what that means, which here is "give up
// entirely," not "try a coarser zoom."
func fetchCartoVectorTileUpstream(fetcher tileFetcher, z, x, y int) ([]byte, string, error) {
	host := cartoVectorTileHosts[(x+y)%len(cartoVectorTileHosts)]
	tileURL := fmt.Sprintf(
		"https://%s.basemaps.cartocdn.com/vectortiles/carto.streets/v1/%d/%d/%d.mvt",
		host, z, x, y,
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
		contentType = "application/x-protobuf"
	}
	return body, contentType, nil
}

// resolveCartoVectorTile is the cache-through resolver for Carto vector
// basemap tiles. Deliberately NOT resolveWorldImageryTile, and deliberately
// without its graceful-degradation behavior, for a reason specific to
// vector data: an MVT tile's feature geometry is encoded in tile-local
// coordinates (an extent, typically 0-4096, spanning just that one tile's
// bounding box). A raster image degrades gracefully because a coarser
// tile's pixels still look like a blurrier version of the right place when
// stretched to fill a finer tile's footprint. A coarser MVT tile's bytes
// served under a finer tile's key do not degrade - they are wrong: every
// coastline, island, and label decodes at a scale and offset that assumes
// the smaller bounding box of the tile it was actually cut for, so it
// renders confidently, silently, and incorrectly, shifted and rescaled
// into whatever fraction of the requested tile its origin corner happens
// to land in. That is worse than nothing on a navigation chart. So the
// semantics here are narrower on purpose:
//  1. cache hit at the requested (z,x,y) -> serve immediately.
//  2. cache miss -> fetch upstream. Success -> cache and serve.
//  3. upstream failure -> return an error. No parent-tile walk, no
//     synthesized empty tile. The caller (basemapVectorTileHandler) turns
//     this into a 404, which MapLibre already treats as "no data for this
//     tile" and simply doesn't draw it - correct behavior for genuinely
//     absent data, as opposed to wrong data presented as real.
//
// Cache read/write errors (infrastructure, not upstream availability) are
// surfaced to the caller exactly as resolveWorldImageryTile does.
func resolveCartoVectorTile(cache *tileCache, fetcher tileFetcher, z, x, y int) ([]byte, string, error) {
	if data, contentType, ok, err := cache.get(cartoBasemapSource, z, x, y); err != nil {
		return nil, "", err
	} else if ok {
		return data, contentType, nil
	}

	data, contentType, err := fetchCartoVectorTileUpstream(fetcher, z, x, y)
	if err != nil {
		return nil, "", err
	}

	if err := cache.put(cartoBasemapSource, z, x, y, data, contentType); err != nil {
		return nil, "", err
	}
	return data, contentType, nil
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

// tileKind discriminates which resolver a prefetch job's worker pool must
// route a given tileCoord to. Esri imagery and Carto vector basemap tiles
// are fetched from different upstreams into the same (z,x,y)-keyed tiles
// table under different source values, and - critically - a basemap tile
// must never be routed to resolveWorldImageryTile: that function's parent-
// tile degradation would reproduce, inside the prefetch worker pool, the
// exact MVT-coordinate-mismatch hazard resolveCartoVectorTile's doc
// comment above exists to avoid.
type tileKind int

const (
	tileKindImagery tileKind = iota
	tileKindBasemap
)

type tileCoord struct {
	kind    tileKind
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
// [minZoom,maxZoom], tagged with the given kind. Only called once
// countTilesForBBox has already confirmed the total is within the
// prefetch cap.
func tilesForBBox(west, south, east, north float64, minZoom, maxZoom int, kind tileKind) []tileCoord {
	var tiles []tileCoord
	for z := minZoom; z <= maxZoom; z++ {
		xMin, xMax, yMin, yMax := zoomTileRange(west, south, east, north, z)
		for x := xMin; x <= xMax; x++ {
			for y := yMin; y <= yMax; y++ {
				tiles = append(tiles, tileCoord{kind: kind, z: z, x: x, y: y})
			}
		}
	}
	return tiles
}

// basemapPrefetchMaxZoom caps how deep a "cache this area" prefetch will
// enqueue Carto vector basemap tiles, regardless of the requested imagery
// maxZoom: Carto's vector source stops at zoom 14 (cartoBasemapMaxZoom in
// basemap_proxy.go), and MapLibre overzooms client-side past a source's
// declared maxzoom rather than requesting deeper tiles that don't exist -
// so prefetching past 14 would just mean fetches that 404 forever and
// count against the operator's cap for nothing. A separate named constant
// here (rather than importing cartoBasemapMaxZoom directly into this
// arithmetic) keeps the "why 14" reasoning next to the one that actually
// matters for a request budget, while both constants stay equal by
// definition - see the equality assertion this repo's tests carry for it.
const basemapPrefetchMaxZoom = 14

// countPrefetchTiles and prefetchTileCoords compute the COMBINED "cache
// this area" job: Esri imagery across the full requested [minZoom,maxZoom]
// (unchanged from before this feature), plus Carto vector basemap tiles
// across [minZoom, min(maxZoom, basemapPrefetchMaxZoom)]. Both delegate to
// the existing single-source helpers above per tileKind and just add the
// two counts/lists together - tilesForBBox/countTilesForBBox already
// return zero tiles for an empty range (minZoom > maxZoom), so a request
// entirely deeper than z14 naturally contributes zero basemap tiles with
// no extra branching here.
func countPrefetchTiles(west, south, east, north float64, minZoom, maxZoom int) int {
	basemapMaxZoom := maxZoom
	if basemapMaxZoom > basemapPrefetchMaxZoom {
		basemapMaxZoom = basemapPrefetchMaxZoom
	}
	return countTilesForBBox(west, south, east, north, minZoom, maxZoom) +
		countTilesForBBox(west, south, east, north, minZoom, basemapMaxZoom)
}

func prefetchTileCoords(west, south, east, north float64, minZoom, maxZoom int) []tileCoord {
	basemapMaxZoom := maxZoom
	if basemapMaxZoom > basemapPrefetchMaxZoom {
		basemapMaxZoom = basemapPrefetchMaxZoom
	}
	tiles := tilesForBBox(west, south, east, north, minZoom, maxZoom, tileKindImagery)
	tiles = append(tiles, tilesForBBox(west, south, east, north, minZoom, basemapMaxZoom, tileKindBasemap)...)
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

// runPrefetchJob fetches every tile in tiles using a bounded worker pool,
// routing each one to the resolver matching its kind: imagery tiles through
// resolveWorldImageryTile's cache-through + degradation logic, basemap
// tiles through resolveCartoVectorTile's narrower cache-through-or-fail
// logic. Getting this dispatch wrong - e.g. sending a basemap tile through
// resolveWorldImageryTile - is exactly the bug this whole split exists to
// prevent (see resolveCartoVectorTile's doc comment), so the switch below
// has an explicit default case rather than silently falling through to one
// resolver or the other: an unrecognised kind can only mean a bug in
// prefetchTileCoords (the only place tileCoord values are constructed), and
// it is logged and counted as a failed tile - loud, but not a crash of the
// whole background-worker pool (or the process, since an unrecovered
// goroutine panic here would take down every other in-flight request too)
// over what should be an unreachable case.
// Completed tiles are immediately servable through the regular tile
// endpoints mid-job, since they land in the same cache the live proxies
// read.
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
				var err error
				switch t.kind {
				case tileKindImagery:
					_, _, err = resolveWorldImageryTile(cache, fetcher, worldImagerySource, t.z, t.x, t.y)
				case tileKindBasemap:
					_, _, err = resolveCartoVectorTile(cache, fetcher, t.z, t.x, t.y)
				default:
					err = fmt.Errorf("unrecognised tileKind %d", t.kind)
				}
				if err != nil {
					log.Printf("prefetch job %s: tile kind=%d z=%d x=%d y=%d failed: %v", job.ID, t.kind, t.z, t.x, t.y, err)
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
// handler factory: computes the COMBINED tile count (Esri imagery across
// the full requested zoom range, plus Carto vector basemap tiles capped at
// basemapPrefetchMaxZoom - see countPrefetchTiles/prefetchTileCoords) for
// the requested bbox/zoom range, rejects it with that combined computed
// count if over maxPrefetchTiles, or otherwise kicks off a background
// prefetch job covering both and returns its id. The frontend's existing
// progress pill reads {done, total} with no notion of imagery vs. basemap,
// so folding both into one combined job/count here is what keeps it
// accurate with no frontend change.
func prefetchWorldImageryHandler(cache *tileCache, fetcher tileFetcher) echo.HandlerFunc {
	return func(c echo.Context) error {
		var body prefetchRequestBody
		if err := c.Bind(&body); err != nil {
			return c.NoContent(http.StatusBadRequest)
		}
		if body.MaxZoom < body.MinZoom || body.MinZoom < 0 {
			return c.NoContent(http.StatusBadRequest)
		}

		total := countPrefetchTiles(body.West, body.South, body.East, body.North, body.MinZoom, body.MaxZoom)
		if total > maxPrefetchTiles {
			return c.JSON(http.StatusBadRequest, map[string]any{
				"error":      fmt.Sprintf("area too large: %d tiles (max %d)", total, maxPrefetchTiles),
				"totalTiles": total,
			})
		}

		tiles := prefetchTileCoords(body.West, body.South, body.East, body.North, body.MinZoom, body.MaxZoom)
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
// escape hatch: clears every cached tile (across all sources, so both Esri
// imagery and Carto vector basemap tiles) AND every cached basemap_assets
// document (style.json x2, tiles.json, glyphs, sprites) - see tileCache.clear's
// comment for why the two tables share one clear operation. Kept on its
// existing /api/world-imagery path rather than adding a second basemap-
// specific endpoint: from an operator's point of view this is one "wipe
// the offline map cache and start clean" action, not two.
func deleteWorldImageryCacheHandler(cache *tileCache) echo.HandlerFunc {
	return func(c echo.Context) error {
		if err := cache.clear(); err != nil {
			return err
		}
		return c.NoContent(http.StatusOK)
	}
}
