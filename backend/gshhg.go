package main

import (
	_ "embed"
	"net/http"

	"github.com/labstack/echo/v4"
)

// Source: NOAA/NGDC Global Self-consistent, Hierarchical, High-resolution
// Geography (GSHHG) database v2.3.7, low-resolution ("l") L1 boundary level
// only (continents/islands; lake/pond sub-levels excluded). Public domain.
// Converted once, offline, from the official shapefile distribution
// (https://www.ngdc.noaa.gov/mgg/shorelines/) with coordinates rounded to
// 4 decimal places (~11m precision). See
// docs/adr/0009-gshhg-coastline-fallback.md.
//
//go:embed data/gshhg_coastline_l.json
var gshhgCoastlineJSON []byte

// gshhgCoastlineHandler serves the embedded GSHHG coastline GeoJSON used as
// a reference-only fallback layer when no navigational chart is available.
// The response is static and compiled into the binary, so it is safe to
// cache far longer than the live-remote-tile proxy in tile_proxy.go.
func gshhgCoastlineHandler(c echo.Context) error {
	c.Response().Header().Set("Cache-Control", "public, max-age=604800, immutable")
	return c.Blob(http.StatusOK, "application/json", gshhgCoastlineJSON)
}
