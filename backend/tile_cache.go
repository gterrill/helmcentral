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
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"golang.org/x/sync/singleflight"
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

// tileCircuitBreakerThreshold/tileCircuitBreakerCooldown govern
// globalTileCircuitBreaker below: a handful of consecutive transport
// failures (not a clean HTTP status like 404, which is a real answer, not
// an outage) trips the breaker, which then skips every upstream attempt -
// serving cache-only - for the cooldown. With the link genuinely down, a
// live tile request or a prefetch worker would otherwise pay the fetch
// client's own timeout (6s, newWorldImageryHTTPClient) on every single
// tile, compounding across resolveWorldImageryTile's coarser-zoom walk.
const (
	tileCircuitBreakerThreshold = 3
	tileCircuitBreakerCooldown  = 60 * time.Second
)

// tileTransportError marks an upstream tile fetch failure as transport-level
// (DNS, TCP, TLS, a timed-out or dropped connection - anything below the
// HTTP layer) as opposed to a clean HTTP response carrying a non-200 status
// such as 404, which legitimately means "no tile here" and must never trip
// the breaker: Esri and Carto both 404 constantly for out-of-coverage or
// past-maxzoom tiles while perfectly reachable.
type tileTransportError struct{ err error }

func (e *tileTransportError) Error() string { return e.err.Error() }
func (e *tileTransportError) Unwrap() error { return e.err }

// tileCircuitBreaker is shared by both upstream tile fetchers (Esri World
// Imagery and Carto vector basemap, via callThroughBreaker below): the
// boat's uplink being down is one fact about the world, not a fact about
// either provider individually, so one shared breaker discovers it once
// instead of each provider spending its own tileCircuitBreakerThreshold
// failures rediscovering the same outage.
type tileCircuitBreaker struct {
	mu              sync.Mutex
	consecutiveFail int
	openUntil       time.Time
}

var globalTileCircuitBreaker = &tileCircuitBreaker{}

// errTileCircuitBreakerOpen is returned by callThroughBreaker in place of
// ever touching the network while the breaker is open.
var errTileCircuitBreakerOpen = errors.New("tile cache: circuit breaker open, skipping upstream")

// allow reports whether an upstream attempt may proceed right now. Once
// openUntil has passed, this returns true again (the "half-open" probe) -
// a real attempt is allowed through, and its outcome (recordSuccess or
// another recordTransportFailure) decides whether the breaker stays closed
// or reopens for another cooldown.
func (b *tileCircuitBreaker) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return time.Now().After(b.openUntil)
}

// recordSuccess closes the breaker and clears its failure count, logging
// only the actual open-to-closed transition.
func (b *tileCircuitBreaker) recordSuccess() {
	b.mu.Lock()
	wasOpen := !b.openUntil.IsZero() && time.Now().Before(b.openUntil)
	b.consecutiveFail = 0
	b.openUntil = time.Time{}
	b.mu.Unlock()
	if wasOpen {
		log.Printf("tile cache: circuit breaker closed, upstream reachable again")
	}
}

// recordTransportFailure counts one transport-level failure and opens the
// breaker once tileCircuitBreakerThreshold consecutive failures are seen,
// logging only that transition (not every failure that leads up to it, and
// never a repeat of an already-open breaker).
func (b *tileCircuitBreaker) recordTransportFailure() {
	b.mu.Lock()
	b.consecutiveFail++
	trip := b.consecutiveFail >= tileCircuitBreakerThreshold && time.Now().After(b.openUntil)
	if trip {
		b.openUntil = time.Now().Add(tileCircuitBreakerCooldown)
	}
	b.mu.Unlock()
	if trip {
		log.Printf("tile cache: circuit breaker open after %d consecutive transport failures, skipping upstream for %s", tileCircuitBreakerThreshold, tileCircuitBreakerCooldown)
	}
}

// tileUpstreamFetchFunc is the shape shared by fetchWorldImageryUpstream and
// fetchCartoVectorTileUpstream, letting both go through one breaker-gated
// wrapper.
type tileUpstreamFetchFunc func(fetcher tileFetcher, z, x, y int) ([]byte, string, error)

// callThroughBreaker gates fetch through globalTileCircuitBreaker: while
// open, upstream is never touched at all - no request is built, no timeout
// is waited out, nothing - which is the entire point on a link that might
// be completely absent. A successful call closes the breaker; a
// transport-level failure counts toward tripping it; a clean HTTP-status
// error (e.g. 404) does neither, since it isn't evidence the link is down.
func callThroughBreaker(fetch tileUpstreamFetchFunc, fetcher tileFetcher, z, x, y int) ([]byte, string, error) {
	if !globalTileCircuitBreaker.allow() {
		return nil, "", errTileCircuitBreakerOpen
	}

	data, contentType, err := fetch(fetcher, z, x, y)
	if err == nil {
		globalTileCircuitBreaker.recordSuccess()
		return data, contentType, nil
	}

	var transportErr *tileTransportError
	if errors.As(err, &transportErr) {
		globalTileCircuitBreaker.recordTransportFailure()
	}
	return nil, "", err
}

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
// human timescales, so a tile is cached forever once fetched.
// The DELETE /api/world-imagery/cache endpoint is the explicit escape
// hatch if a cached result ever needs to be cleared (e.g. once Esri adds
// coverage where a blank/degraded result was previously cached).
//
// resolveGroup merges concurrent resolves for the same (source,z,x,y) -
// see resolveWorldImageryTile/resolveCartoVectorTile - so a browser
// re-requesting a tile mid-fetch, or a live request racing a prefetch
// worker over the same tile, costs one upstream attempt, not several.
type tileCache struct {
	db           *sql.DB
	resolveGroup singleflight.Group
}

func tileCachePath() string {
	return cacheFilePath("TILE_CACHE_PATH", "data/tile-cache.sqlite")
}

// tileCacheMaxOpenConns bounds the connection pool once WAL mode is on.
// Rollback-journal mode (the modernc.org/sqlite default with no pragmas)
// only ever allows one writer and blocks every reader behind it, which is
// why this used to be capped at a single connection instead - a prefetch
// job's writes would otherwise lock out the live map's reads for as long
// as the job ran. WAL specifically permits concurrent readers alongside
// one writer, so a small pool (not just 1, not unbounded) lets the live
// tile proxy keep reading while a prefetch job's worker pool writes.
const tileCacheMaxOpenConns = 4

func newTileCache(dbPath string) (*tileCache, error) {
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create tile cache directory: %w", err)
		}
	}

	// WAL + synchronous(NORMAL) + busy_timeout(5000), via modernc's
	// "?_pragma=" DSN form (applied per new connection - see
	// applyQueryParams in modernc.org/sqlite). Without these, this ran in
	// the driver's default rollback-journal mode on one connection (see
	// tileCacheMaxOpenConns's doc comment for why that's a problem);
	// busy_timeout makes a writer/writer conflict wait up to 5s instead of
	// failing immediately with SQLITE_BUSY, which is what
	// TestPrefetchWorldImageryJob_RunsToCompletionWithCorrectCounts used to
	// hit intermittently before the single-connection cap papered over it.
	dsn := dbPath + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open tile cache database: %w", err)
	}
	db.SetMaxOpenConns(tileCacheMaxOpenConns)

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
		return nil, "", &tileTransportError{err: fmt.Errorf("upstream request failed: %w", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", &tileTransportError{err: fmt.Errorf("read upstream body: %w", err)}
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}
	return body, contentType, nil
}

// tileResolveResult carries resolveWorldImageryTile's outcome through its
// singleflight.Group merge (singleflight.Do can only hand back one `any`
// value per key, not resolveWorldImageryTile's three named returns).
type tileResolveResult struct {
	data        []byte
	contentType string
	degraded    bool
}

// resolveWorldImageryTile implements the cache-through + graceful-
// degradation semantics shared by both the live tile proxy and the
// prefetch worker pool:
//  1. Cache hit at the requested (source,z,x,y) -> serve immediately, no
//     upstream call.
//  2. Cache miss -> fetch upstream (through the shared circuit breaker).
//     Success -> cache and serve.
//  3. Upstream failure -> retry at (z-1,x>>1,y>>1), then (z-2,...), up to
//     maxDegradeLevels coarser levels, each checking the cache first (a
//     coarser-level cache hit does not invoke the fetcher). The first
//     level that yields bytes (from cache or a successful fetch) is
//     served as a DEGRADED result.
//  4. If every level is exhausted (or z runs below 0), fall back to the
//     transparent blank tile - also DEGRADED.
//
// Unlike the pre-fix version, a degraded or blank result is never cached
// under the originally-requested (z,x,y) key - only ever under a coarser
// tile's own genuine key, when that coarser tile was actually,
// successfully fetched (or already held) at that key. Caching a degraded
// result at the fine key is exactly what let one offline pan blank or blur
// a tile forever, curable only by wiping the whole cache; see
// tile_proxy.go's short Cache-Control on a degraded response for the
// matching browser-side half of this fix. The `degraded` return tells the
// caller which Cache-Control to use.
//
// Concurrent callers for the same (source,z,x,y) are merged into one
// resolve via cache.resolveGroup (singleflight), so a browser re-requesting
// a tile mid-fetch, or a live request racing a prefetch worker on the same
// tile, costs one upstream attempt, not two.
//
// Cache read/write errors (infrastructure failures, not upstream-imagery
// availability) are surfaced to the caller rather than masked, per the
// fail-fast policy - only the upstream-availability path degrades.
func resolveWorldImageryTile(cache *tileCache, fetcher tileFetcher, source string, z, x, y int) ([]byte, string, bool, error) {
	key := fmt.Sprintf("%s/%d/%d/%d", source, z, x, y)
	v, err, _ := cache.resolveGroup.Do(key, func() (any, error) {
		data, contentType, degraded, err := resolveWorldImageryTileUncached(cache, fetcher, source, z, x, y)
		if err != nil {
			return nil, err
		}
		return tileResolveResult{data, contentType, degraded}, nil
	})
	if err != nil {
		return nil, "", false, err
	}
	res := v.(tileResolveResult)
	return res.data, res.contentType, res.degraded, nil
}

func resolveWorldImageryTileUncached(cache *tileCache, fetcher tileFetcher, source string, z, x, y int) ([]byte, string, bool, error) {
	if data, contentType, ok, err := cache.get(source, z, x, y); err != nil {
		return nil, "", false, err
	} else if ok {
		return data, contentType, false, nil
	}

	if data, contentType, err := callThroughBreaker(fetchWorldImageryUpstream, fetcher, z, x, y); err == nil {
		if err := cache.put(source, z, x, y, data, contentType); err != nil {
			return nil, "", false, err
		}
		return data, contentType, false, nil
	}

	cz, cx, cy := z, x, y
	for level := 1; level <= maxDegradeLevels; level++ {
		cz, cx, cy = cz-1, cx>>1, cy>>1
		if cz < 0 {
			break
		}

		if data, contentType, ok, err := cache.get(source, cz, cx, cy); err != nil {
			return nil, "", false, err
		} else if ok {
			return data, contentType, true, nil
		}

		if data, contentType, err := callThroughBreaker(fetchWorldImageryUpstream, fetcher, cz, cx, cy); err == nil {
			if err := cache.put(source, cz, cx, cy, data, contentType); err != nil {
				return nil, "", false, err
			}
			return data, contentType, true, nil
		}
	}

	return transparentPNG1x1, "image/png", true, nil
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
		return nil, "", &tileTransportError{err: fmt.Errorf("upstream request failed: %w", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", &tileTransportError{err: fmt.Errorf("read upstream body: %w", err)}
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
// surfaced to the caller exactly as resolveWorldImageryTile does. Upstream
// fetches go through the same shared circuit breaker as imagery
// (callThroughBreaker), and concurrent callers for the same tile are
// merged through cache.resolveGroup, same reasoning as
// resolveWorldImageryTile.
func resolveCartoVectorTile(cache *tileCache, fetcher tileFetcher, z, x, y int) ([]byte, string, error) {
	key := fmt.Sprintf("%s/%d/%d/%d", cartoBasemapSource, z, x, y)
	v, err, _ := cache.resolveGroup.Do(key, func() (any, error) {
		data, contentType, err := resolveCartoVectorTileUncached(cache, fetcher, z, x, y)
		if err != nil {
			return nil, err
		}
		return tileResolveResult{data: data, contentType: contentType}, nil
	})
	if err != nil {
		return nil, "", err
	}
	res := v.(tileResolveResult)
	return res.data, res.contentType, nil
}

func resolveCartoVectorTileUncached(cache *tileCache, fetcher tileFetcher, z, x, y int) ([]byte, string, error) {
	if data, contentType, ok, err := cache.get(cartoBasemapSource, z, x, y); err != nil {
		return nil, "", err
	} else if ok {
		return data, contentType, nil
	}

	data, contentType, err := callThroughBreaker(fetchCartoVectorTileUpstream, fetcher, z, x, y)
	if err != nil {
		return nil, "", err
	}

	if err := cache.put(cartoBasemapSource, z, x, y, data, contentType); err != nil {
		return nil, "", err
	}
	return data, contentType, nil
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
