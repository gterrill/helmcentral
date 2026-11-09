package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func TestSoundedCells_RouteIsRegistered(t *testing.T) {
	for _, r := range anchorRoutes() {
		if r.Method == http.MethodGet && r.Path == "/api/anchor-watch/sounded-cells" && r.Tier == tierRead {
			return
		}
	}
	t.Error("GET /api/anchor-watch/sounded-cells is not in anchorRoutes")
}

func TestSoundedCells_Statuses(t *testing.T) {
	t.Cleanup(func() { soundedPublished.Store(nil) })
	setAt := time.Date(2026, 11, 9, 8, 0, 0, 0, time.UTC)
	watch := anchorWatchData{Lat: -17.5, Lon: 177.4, SetAt: setAt}

	soundedPublished.Store(nil)
	if got := soundedCellsFor(watch, false); got.Status != soundedNoWatch || len(got.Cells) != 0 {
		t.Errorf("no watch: %+v", got)
	}
	if got := soundedCellsFor(watch, true); got.Status != soundedNotReady || got.Cells == nil || len(got.Cells) != 0 {
		t.Errorf("nothing published: %+v", got)
	}

	// Another stay's publication is not this one's.
	soundedPublished.Store(&soundedEntry{setAtMs: setAt.Add(-time.Hour).UnixMilli(), lat: watch.Lat, lon: watch.Lon, cells: []tideCalCell{{E: 1, N: 1}}})
	if got := soundedCellsFor(watch, true); got.Status != soundedNotReady || len(got.Cells) != 0 {
		t.Errorf("another set_at: %+v", got)
	}

	soundedPublished.Store(&soundedEntry{setAtMs: setAt.UnixMilli(), lat: watch.Lat, lon: watch.Lon, cells: []tideCalCell{{E: 2, N: -3}, {E: 0, N: 0}}})
	got := soundedCellsFor(watch, true)
	if got.Status != soundedReady || got.CellM != tideCalCellM || got.OriginLat != watch.Lat || got.OriginLon != watch.Lon || len(got.Cells) != 2 {
		t.Fatalf("ready: %+v", got)
	}
	sort.Slice(got.Cells, func(i, j int) bool { return got.Cells[i][0] < got.Cells[j][0] })
	if got.Cells[0] != [2]int{0, 0} || got.Cells[1] != [2]int{2, -3} {
		t.Errorf("cells: %v", got.Cells)
	}
}

// Which ground has been crossed is known from the positions alone, so a tide
// chart that is down, and so no fit, still leaves the cells published.
func TestSoundedCells_PublishedWhileTheTideChartIsDown(t *testing.T) {
	anchorTrailEnv(t)
	resetTideCalState(t)
	t.Cleanup(func() { soundedPublished.Store(nil) })
	soundedPublished.Store(nil)
	globalTideCalRecorder.reset()
	prev := tideCalChartSource
	tideCalChartSource = func() (tideCalChart, error) { return tideCalChart{}, errors.New("tide service down") }
	t.Cleanup(func() { tideCalChartSource = prev })
	dropAnchor(t, -23.1847, 150.9322)
	watch, active, _ := currentAnchorWatch()
	ctx, cancel := context.WithCancel(context.Background())
	done := startTideCalibrationWorker(ctx)
	t.Cleanup(func() { cancel(); <-done })

	feedMinutes(t, time.Date(2026, 11, 8, 1, 0, 0, 0, time.UTC), 4, 4.0, false)
	deadline := time.Now().Add(3 * time.Second)
	var got soundedCellsResponse
	for time.Now().Before(deadline) {
		got = soundedCellsFor(watch, active)
		if got.Status == soundedReady && len(got.Cells) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got.Status != soundedReady || len(got.Cells) == 0 {
		t.Fatalf("with the chart down: %+v", got)
	}
	if tideCalPublishedFor(watch) {
		t.Error("a fit was published from a chart that failed")
	}
}

func TestSoundedCells_EveryRecordedMinuteCellOnce(t *testing.T) {
	watch := anchorWatchData{Lat: -17.5, Lon: 177.4, SetAt: time.Date(2026, 11, 9, 8, 0, 0, 0, time.UTC)}
	at := watch.SetAt
	readings := []tideCalReading{
		{At: at, Lat: watch.Lat, Lon: watch.Lon, DepthM: 3},
		{At: at.Add(time.Minute), Lat: watch.Lat + 0.000001, Lon: watch.Lon, DepthM: 3},
		{At: at.Add(2 * time.Minute), Lat: watch.Lat + 0.0001, Lon: watch.Lon, DepthM: 3}, // about 11 m north
	}
	cells := soundedCellsOf(readings, watch.Lat, watch.Lon)
	if len(cells) != 2 {
		t.Errorf("want 2 distinct cells, got %v", cells)
	}
}

func TestSoundedCells_Endpoint(t *testing.T) {
	t.Cleanup(func() { soundedPublished.Store(nil) })
	soundedPublished.Store(nil)
	e := echo.New()
	rec := httptest.NewRecorder()
	c := e.NewContext(httptest.NewRequest(http.MethodGet, "/api/anchor-watch/sounded-cells", nil), rec)
	if err := serveSoundedCells(c, anchorWatchData{}, false); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("code %d err %v", rec.Code, err)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("missing no-store")
	}
}

// The stay keeps its distinct cells as minutes arrive, so a publish does not
// rescan the readings; a moved anchor rebuilds them from the new origin.
func TestSoundedCells_StayKeepsItsCellsAsMinutesArrive(t *testing.T) {
	resetTideCalState(t)
	t.Cleanup(func() { soundedPublished.Store(nil) })
	store := newTestPassageStore(t)
	watch := anchorWatchData{Lat: -17.5, Lon: 177.4, SetAt: time.Date(2026, 11, 9, 8, 0, 0, 0, time.UTC)}
	t0 := watch.SetAt
	if err := store.appendTideCalReading(watch.SetAt, tideCalReading{At: t0, Lat: watch.Lat, Lon: watch.Lon, DepthM: 3}); err != nil {
		t.Fatal(err)
	}
	if err := publishSoundedCells(watch, store); err != nil {
		t.Fatal(err)
	}
	if got := soundedCellsFor(watch, true); len(got.Cells) != 1 {
		t.Fatalf("first publish: %+v", got)
	}
	north := tideCalReading{At: t0.Add(time.Minute), Lat: watch.Lat + 0.0001, Lon: watch.Lon, DepthM: 3}
	tideCalStayAppend(store, watch.SetAt, north)
	tideCalStayMu.Lock()
	held := len(tideCalStayCur.cells)
	tideCalStayMu.Unlock()
	if held != 2 {
		t.Errorf("the stay holds %d cells after an appended minute, want 2", held)
	}
	if err := publishSoundedCells(watch, store); err != nil {
		t.Fatal(err)
	}
	if got := soundedCellsFor(watch, true); len(got.Cells) != 2 {
		t.Errorf("second publish: %+v", got)
	}
	// A corrected anchor republishes the same readings from the new origin.
	moved := watch
	moved.Lat += 0.0001
	if err := publishSoundedCells(moved, store); err != nil {
		t.Fatal(err)
	}
	got := soundedCellsFor(moved, true)
	if got.Status != soundedReady || got.OriginLat != moved.Lat || len(got.Cells) != 2 {
		t.Errorf("moved anchor: %+v", got)
	}
}
