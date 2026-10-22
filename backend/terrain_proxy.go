package main

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

// terrainSource is the tiles.source key for AWS Terrarium elevation tiles
// after the sea has been flattened to 0 m. The key names the processing as
// well as the provider: raw Terrarium tiles would be a different thing.
const terrainSource = "aws-terrarium-sea0"

// terrainMaxZoom is the deepest level the Terrarium dataset publishes.
const terrainMaxZoom = 15

// terrainSeaFloor is Terrarium's encoding of 0 m: elevation = R*256 + G +
// B/256 - 32768, so any pixel whose 24-bit value R<<16|G<<8|B is below
// 128<<16 lies below sea level.
const terrainSeaFloor = 128 << 16

func fetchTerrainUpstream(fetcher tileFetcher, z, x, y int) ([]byte, string, error) {
	tileURL := fmt.Sprintf("https://s3.amazonaws.com/elevation-tiles-prod/terrarium/%d/%d/%d.png", z, x, y)
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

	flat, err := clampTerrainSeaToZero(body)
	if err != nil {
		return nil, "", err
	}
	return flat, "image/png", nil
}

// clampTerrainSeaToZero sets every pixel below sea level to exactly 0 m, so
// the chart and imagery draped over the terrain sit on a flat sea instead of
// sagging into the bathymetry.
func clampTerrainSeaToZero(data []byte) ([]byte, error) {
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode terrain tile: %w", err)
	}
	// Work on the pixel bytes directly: one conversion to NRGBA (a no-op for
	// the usual opaque PNG), then a pass over Pix, four bytes a pixel.
	out, ok := src.(*image.NRGBA)
	if !ok {
		b := src.Bounds()
		out = image.NewNRGBA(b)
		draw.Draw(out, b, src, b.Min, draw.Src)
	}
	for row := 0; row < out.Rect.Dy(); row++ {
		line := out.Pix[row*out.Stride : row*out.Stride+out.Rect.Dx()*4]
		for i := 0; i+3 < len(line); i += 4 {
			if int(line[i])<<16|int(line[i+1])<<8|int(line[i+2]) < terrainSeaFloor {
				line[i], line[i+1], line[i+2], line[i+3] = 128, 0, 0, 255
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, fmt.Errorf("encode terrain tile: %w", err)
	}
	return buf.Bytes(), nil
}

// resolveTerrainTile is cache-through with no degradation: a tile that cannot
// be fetched is an error, never a coarser or flat stand-in.
func resolveTerrainTile(cache *tileCache, fetcher tileFetcher, z, x, y int) ([]byte, string, error) {
	key := fmt.Sprintf("%s/%d/%d/%d", terrainSource, z, x, y)
	v, err, _ := cache.resolveGroup.Do(key, func() (any, error) {
		if data, contentType, ok, err := cache.get(terrainSource, z, x, y); err != nil {
			return nil, &terrainCacheError{err}
		} else if ok {
			return tileResolveResult{data: data, contentType: contentType}, nil
		}
		data, contentType, err := callThroughBreaker(fetchTerrainUpstream, fetcher, z, x, y)
		if err != nil {
			return nil, err
		}
		if err := cache.put(terrainSource, z, x, y, data, contentType); err != nil {
			return nil, &terrainCacheError{err}
		}
		return tileResolveResult{data: data, contentType: contentType}, nil
	})
	if err != nil {
		return nil, "", err
	}
	res := v.(tileResolveResult)
	return res.data, res.contentType, nil
}

// terrainCacheError marks a local cache failure, which is a server fault,
// apart from an upstream failure, which is a bad gateway.
type terrainCacheError struct{ err error }

func (e *terrainCacheError) Error() string { return e.err.Error() }
func (e *terrainCacheError) Unwrap() error { return e.err }

func proxyTerrainTileHandler(cache *tileCache, fetcher tileFetcher) echo.HandlerFunc {
	return func(c echo.Context) error {
		z, zErr := strconv.Atoi(strings.TrimSpace(c.Param("z")))
		x, xErr := strconv.Atoi(strings.TrimSpace(c.Param("x")))
		y, yErr := strconv.Atoi(strings.TrimSpace(c.Param("y")))
		if zErr != nil || xErr != nil || yErr != nil {
			return c.NoContent(http.StatusBadRequest)
		}
		z, x, y = clampTileCoords(z, x, y, terrainMaxZoom)

		data, contentType, err := resolveTerrainTile(cache, fetcher, z, x, y)
		if err != nil {
			if cacheErr, ok := err.(*terrainCacheError); ok {
				return cacheErr.err
			}
			log.Printf("terrain tile %d/%d/%d unavailable: %v", z, x, y, err)
			c.Response().Header().Set("Cache-Control", "no-store")
			return c.NoContent(http.StatusBadGateway)
		}
		c.Response().Header().Set("Cache-Control", tileCacheControl)
		return c.Blob(http.StatusOK, contentType, data)
	}
}
