package main

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"fmt"
	"net/http"
	"strings"

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

// gshhgCoastlineGzipped is gshhgCoastlineJSON, gzip-compressed once at
// startup. Measured on the boat, running the 1.5MB coastline through the
// gzip middleware cost +90-110ms on every single request (backend perf
// audit Tier 3) despite the response never changing - it's static and
// compiled into the binary, so there is nothing to recompress after the
// first time. This route is on compression.go's skip list so the general
// gzip middleware never wraps it a second time; gshhgCoastlineHandler below
// does its own client-negotiated choice between this and the raw bytes.
var gshhgCoastlineGzipped = mustGzip(gshhgCoastlineJSON)

// mustGzip gzip-compresses data or panics. Used only for gshhgCoastlineGzipped's
// package-var initializer, at startup, against a fixed compiled-in byte
// slice: a failure here would mean gzip.Writer itself is broken, which is a
// build-time-detectable condition worth crashing loudly on rather than
// silently falling back to serving the coastline uncompressed forever.
func mustGzip(data []byte) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(data); err != nil {
		panic(fmt.Sprintf("gzip: %v", err))
	}
	if err := gz.Close(); err != nil {
		panic(fmt.Sprintf("gzip: %v", err))
	}
	return buf.Bytes()
}

// gshhgCoastlineHandler serves the embedded GSHHG coastline GeoJSON used as
// a reference-only fallback layer when no navigational chart is available.
// The response is static and compiled into the binary, so it is safe to
// cache far longer than the live-remote-tile proxy in tile_proxy.go.
//
// Serves the pre-gzipped bytes with Content-Encoding: gzip to a client that
// says it accepts gzip (the same substring check Echo's own gzip middleware
// uses, so this route drops out of that middleware's work without changing
// what any client actually receives), and the raw bytes otherwise. Vary:
// Accept-Encoding either way, so any cache sitting in front of this knows
// the response depends on that header and never serves one client's
// representation to the other.
func gshhgCoastlineHandler(c echo.Context) error {
	header := c.Response().Header()
	header.Set("Cache-Control", "public, max-age=604800, immutable")
	header.Set("Vary", "Accept-Encoding")

	if strings.Contains(c.Request().Header.Get(echo.HeaderAcceptEncoding), "gzip") {
		header.Set("Content-Encoding", "gzip")
		return c.Blob(http.StatusOK, "application/json", gshhgCoastlineGzipped)
	}
	return c.Blob(http.StatusOK, "application/json", gshhgCoastlineJSON)
}
