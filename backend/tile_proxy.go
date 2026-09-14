package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

var transparentPNG1x1 = buildTransparentPNG1x1()

// worldImageryMaxZoom is the deepest zoom level this proxy will ever ask
// Esri for. Esri's World Imagery layer resolves well past 18 in many
// coastal/populated areas via Maxar Vivid updates; areas without that
// depth now gracefully degrade to the nearest coarser cached/available
// zoom (see resolveWorldImageryTile in tile_cache.go) instead of being
// capped from ever asking.
const worldImageryMaxZoom = 20

// tileCacheControl is the normal Cache-Control for a genuine tile at the
// requested key - imagery doesn't change on human timescales, so the
// browser is told to hold onto it for a while.
const tileCacheControl = "public, max-age=1800"

// degradedTileCacheControl is used instead for a coarser or blank fallback
// (resolveWorldImageryTile's `degraded` return) - short on purpose, so the
// browser retries this exact tile again soon rather than pinning today's
// degraded answer for the same 30 minutes a real tile gets.
const degradedTileCacheControl = "public, max-age=60"

func buildTransparentPNG1x1() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.NRGBA{R: 0, G: 0, B: 0, A: 0})

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		return nil
	}

	return buffer.Bytes()
}

// proxyWorldImageryTileHandler is a handler factory (rather than a plain
// echo.HandlerFunc) so tests can inject a *tileCache pointed at a temp-dir
// sqlite file and a fake tileFetcher instead of a real cache/Esri network
// call. main.go registers it as
// proxyWorldImageryTileHandler(globalTileCache, http.DefaultClient).
func proxyWorldImageryTileHandler(cache *tileCache, fetcher tileFetcher) echo.HandlerFunc {
	return func(c echo.Context) error {
		z := strings.TrimSpace(c.Param("z"))
		x := strings.TrimSpace(c.Param("x"))
		y := strings.TrimSpace(c.Param("y"))
		if z == "" || y == "" || x == "" {
			return c.NoContent(http.StatusBadRequest)
		}

		zInt, zErr := strconv.Atoi(z)
		xInt, xErr := strconv.Atoi(x)
		yInt, yErr := strconv.Atoi(y)
		if zErr != nil || xErr != nil || yErr != nil {
			return c.NoContent(http.StatusBadRequest)
		}
		zInt, xInt, yInt = clampTileCoords(zInt, xInt, yInt, worldImageryMaxZoom)

		data, contentType, degraded, err := resolveWorldImageryTile(cache, fetcher, worldImagerySource, zInt, xInt, yInt)
		if err != nil {
			return err
		}

		if degraded {
			// A coarser or blank fallback is never persisted under this
			// tile's own key (see resolveWorldImageryTile), so the browser
			// must not sit on it for the normal 30 minutes either - a short
			// TTL means the next pan/zoom over this same tile retries
			// upstream once the link is back, instead of a permanently
			// blurred or blank spot only a full cache wipe would cure.
			c.Response().Header().Set("Cache-Control", degradedTileCacheControl)
		} else {
			c.Response().Header().Set("Cache-Control", tileCacheControl)
		}
		return c.Blob(http.StatusOK, contentType, data)
	}
}

func clampTileCoords(z, x, y, maxZoom int) (int, int, int) {
	if z <= maxZoom {
		return z, x, y
	}

	shift := z - maxZoom
	return maxZoom, x >> shift, y >> shift
}
