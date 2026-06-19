package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

var transparentPNG1x1 = buildTransparentPNG1x1()

const worldImageryMaxZoom = 18

func buildTransparentPNG1x1() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.NRGBA{R: 0, G: 0, B: 0, A: 0})

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		return nil
	}

	return buffer.Bytes()
}

func himawariServicePath(variant string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(variant)) {
	case "transparent":
		return "GOES31D", true
	case "colorized":
		return "GOES31C", true
	case "infrared":
		return "GOES31", true
	default:
		return "", false
	}
}

func proxyHimawariTileHandler(c echo.Context) error {
	servicePath, ok := himawariServicePath(c.Param("variant"))
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid himawari variant"})
	}

	z := strings.TrimSpace(c.Param("z"))
	x := strings.TrimSpace(c.Param("x"))
	y := strings.TrimSpace(c.Param("y"))
	if z == "" || y == "" || x == "" {
		return c.NoContent(http.StatusBadRequest)
	}

	tileURL := fmt.Sprintf(
		"https://earthlive.maptiles.arcgis.com/arcgis/rest/services/GOES/%s/MapServer/tile/%s/%s/%s",
		servicePath,
		z,
		y,
		x,
	)

	client := &http.Client{Timeout: 6 * time.Second}
	req, err := http.NewRequest(http.MethodGet, tileURL, nil)
	if err != nil {
		return c.Blob(http.StatusOK, "image/png", transparentPNG1x1)
	}
	req.Header.Set("User-Agent", "helmcentral/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return c.Blob(http.StatusOK, "image/png", transparentPNG1x1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.Blob(http.StatusOK, "image/png", transparentPNG1x1)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return c.Blob(http.StatusOK, "image/png", transparentPNG1x1)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/png"
	}

	c.Response().Header().Set("Cache-Control", "public, max-age=300")
	return c.Blob(http.StatusOK, contentType, body)
}

func proxyWorldImageryTileHandler(c echo.Context) error {
	z := strings.TrimSpace(c.Param("z"))
	x := strings.TrimSpace(c.Param("x"))
	y := strings.TrimSpace(c.Param("y"))
	if z == "" || y == "" || x == "" {
		return c.NoContent(http.StatusBadRequest)
	}

	zInt, zErr := strconv.Atoi(z)
	xInt, xErr := strconv.Atoi(x)
	yInt, yErr := strconv.Atoi(y)
	if zErr == nil && xErr == nil && yErr == nil {
		zInt, xInt, yInt = clampTileCoords(zInt, xInt, yInt, worldImageryMaxZoom)
		z = strconv.Itoa(zInt)
		x = strconv.Itoa(xInt)
		y = strconv.Itoa(yInt)
	}

	tileURL := fmt.Sprintf(
		"https://services.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/%s/%s/%s",
		z,
		y,
		x,
	)

	client := &http.Client{Timeout: 6 * time.Second}
	req, err := http.NewRequest(http.MethodGet, tileURL, nil)
	if err != nil {
		return c.Blob(http.StatusOK, "image/png", transparentPNG1x1)
	}
	req.Header.Set("User-Agent", "helmcentral/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return c.Blob(http.StatusOK, "image/png", transparentPNG1x1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.Blob(http.StatusOK, "image/png", transparentPNG1x1)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return c.Blob(http.StatusOK, "image/png", transparentPNG1x1)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}

	c.Response().Header().Set("Cache-Control", "public, max-age=1800")
	return c.Blob(http.StatusOK, contentType, body)
}

func clampTileCoords(z, x, y, maxZoom int) (int, int, int) {
	if z <= maxZoom {
		return z, x, y
	}

	shift := z - maxZoom
	return maxZoom, x >> shift, y >> shift
}
