package main

import (
	"net/http"
	"sync/atomic"

	"github.com/labstack/echo/v4"
)

// The sounded cells are the 2.5 m cells of the swing circle the boat has been
// over during this watch, one per recorded minute. The Anchor Watch map clears
// the shading over them. The worker publishes them with the tide fit; this
// route only reads that publication: no store, no fit, no lock the recorder
// holds (docs/adr/0274).

const (
	soundedNoWatch  = "no_watch"
	soundedNotReady = "not_ready"
	soundedReady    = "ready"
)

// soundedCellsResponse is the wire shape. A cell [e, n] covers east
// [e*cell_m, (e+1)*cell_m) and north [n*cell_m, (n+1)*cell_m) of the origin,
// measured as localENMeters measures them.
type soundedCellsResponse struct {
	Status    string   `json:"status"`
	CellM     float64  `json:"cell_m"`
	OriginLat float64  `json:"origin_lat"`
	OriginLon float64  `json:"origin_lon"`
	SetAt     string   `json:"set_at,omitempty"`
	Cells     [][2]int `json:"cells"`
}

// soundedEntry is the published cells of one stay at one anchor position.
type soundedEntry struct {
	setAtMs  int64
	lat, lon float64
	cells    []tideCalCell
}

// soundedPublished is the latest cell set. The worker is its only writer.
var soundedPublished atomic.Pointer[soundedEntry]

// publishSoundedCells loads the stay if need be and publishes its cells. It
// does not depend on the tide chart or the fit.
func publishSoundedCells(watch anchorWatchData, store *passageStore) error {
	st, err := tideCalStayLoaded(watch, store)
	if err != nil {
		return err
	}
	tideCalStayMu.Lock()
	st.ensureCells(watch.Lat, watch.Lon)
	cells := make([]tideCalCell, 0, len(st.cells))
	for c := range st.cells {
		cells = append(cells, c)
	}
	tideCalStayMu.Unlock()
	soundedPublished.Store(&soundedEntry{setAtMs: watch.SetAt.UnixMilli(), lat: watch.Lat, lon: watch.Lon, cells: cells})
	return nil
}

// soundedCellsOf is the distinct cells of a stay's readings.
func soundedCellsOf(readings []tideCalReading, anchorLat, anchorLon float64) []tideCalCell {
	seen := map[tideCalCell]struct{}{}
	out := []tideCalCell{}
	for _, r := range readings {
		c := tideCalCellAt(anchorLat, anchorLon, r.Lat, r.Lon)
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return out
}

// soundedCellsFor reads the published state for this watch.
func soundedCellsFor(watch anchorWatchData, active bool) soundedCellsResponse {
	out := soundedCellsResponse{Status: soundedNoWatch, CellM: tideCalCellM, Cells: [][2]int{}}
	if !active {
		return out
	}
	out.OriginLat, out.OriginLon = watch.Lat, watch.Lon
	out.SetAt = watch.SetAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	out.Status = soundedNotReady
	e := soundedPublished.Load()
	if e == nil || e.setAtMs != watch.SetAt.UnixMilli() || e.lat != watch.Lat || e.lon != watch.Lon {
		return out
	}
	out.Status = soundedReady
	for _, c := range e.cells {
		out.Cells = append(out.Cells, [2]int{c.E, c.N})
	}
	return out
}

func serveSoundedCells(c echo.Context, watch anchorWatchData, active bool) error {
	c.Response().Header().Set("Cache-Control", "no-store")
	return c.JSON(http.StatusOK, soundedCellsFor(watch, active))
}

// getAnchorSoundedCells is GET /api/anchor-watch/sounded-cells.
func getAnchorSoundedCells(c echo.Context) error {
	watch, active, _ := currentAnchorWatch()
	return serveSoundedCells(c, watch, active)
}
