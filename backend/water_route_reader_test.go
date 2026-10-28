package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func newRouteReader(t *testing.T, enabled bool) (*offlineCoastlineReader, *coastlineStore) {
	t.Helper()
	store := newTestCoastlineStore(t)
	return &offlineCoastlineReader{
		Enabled: func() (bool, error) { return enabled, nil },
		Store:   store,
		Boat:    func() (float64, float64, bool) { return haslewood.Lat, haslewood.Lon, true },
	}, store
}

func holdWhitsundays(t *testing.T, store *coastlineStore, squares ...coverageSquare) {
	t.Helper()
	lines := loadShorelineFile(t, "testdata/water_route/whitsundays.json.gz")
	for i, sq := range squares {
		var l []shorelineLine
		if i == 0 {
			l = lines // the way lines are held in one square or both; the router must cope
		}
		if err := store.SaveSquare(sq, l, coastlineTestAt.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRouteOverWaterReader_WhitsundaysFromStoredSquares(t *testing.T) {
	r, store := newRouteReader(t, true)
	holdWhitsundays(t, store, coverageSquare{-21, 149}, coverageSquare{-21, 148})
	got, err := r.RouteOverWater(context.Background(), haslewood.Lat, haslewood.Lon, cidHarbour.Lat, cidHarbour.Lon)
	if err != nil {
		t.Fatal(err)
	}
	if got.DistanceNm < 12 || got.DistanceNm > 20 || got.StraightLineNm > 8 {
		t.Errorf("Haslewood to Cid Harbour: routed %.1f nm, straight %.1f nm", got.DistanceNm, got.StraightLineNm)
	}
}

func TestRouteOverWaterReader_IntricateCoastStillRoutes(t *testing.T) {
	r, store := newRouteReader(t, true)
	lines := loadShorelineFile(t, "testdata/water_route/whitsundays-goldsmith.json.gz")
	for _, sq := range []coverageSquare{{-21, 148}, {-21, 149}, {-20, 148}, {-20, 149}} {
		if err := store.SaveSquare(sq, lines, coastlineTestAt); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now()
	got, err := r.RouteOverWater(context.Background(), coralSeaMarina.Lat, coralSeaMarina.Lon, goldsmithAnchorage.Lat, goldsmithAnchorage.Lon)
	if err != nil {
		t.Fatal(err)
	}
	if got.DistanceNm < 36 || got.DistanceNm > 39 || got.DistanceNm < got.StraightLineNm*1.05 {
		t.Errorf("Coral Sea Marina to Goldsmith: routed %.2f nm, straight %.2f nm", got.DistanceNm, got.StraightLineNm)
	}
	if routePolylineCrossesShore(got, lines) {
		t.Errorf("route crosses the shoreline: %+v", got.Points)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("took %v", time.Since(start))
	}
}

func TestRouteOverWaterReader_ASearchPastTheTimeLimitIsTooIntricate(t *testing.T) {
	r, store := newRouteReader(t, true)
	lines := loadShorelineFile(t, "testdata/water_route/whitsundays-goldsmith.json.gz")
	for _, sq := range []coverageSquare{{-21, 148}, {-21, 149}, {-20, 148}, {-20, 149}} {
		if err := store.SaveSquare(sq, lines, coastlineTestAt); err != nil {
			t.Fatal(err)
		}
	}
	old := waterRouteMaxTime
	t.Cleanup(func() { waterRouteMaxTime = old })
	waterRouteMaxTime = time.Nanosecond
	_, err := r.RouteOverWater(context.Background(), coralSeaMarina.Lat, coralSeaMarina.Lon, goldsmithAnchorage.Lat, goldsmithAnchorage.Lon)
	if !errors.Is(err, errWaterRouteTooIntricate) {
		t.Fatalf("want the too intricate refusal, got %v", err)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		t.Errorf("a search that ran too long is a refusal, not the caller's deadline: %v", err)
	}
	if !strings.Contains(err.Error(), "too intricate to route in one go") || !strings.Contains(err.Error(), "shorter legs") {
		t.Errorf("the advice is missing: %v", err)
	}
}

func TestRouteOverWaterReader_AParentDeadlineKeepsItsOwnError(t *testing.T) {
	r, store := newRouteReader(t, true)
	lines := loadShorelineFile(t, "testdata/water_route/whitsundays-goldsmith.json.gz")
	for _, sq := range []coverageSquare{{-21, 148}, {-21, 149}, {-20, 148}, {-20, 149}} {
		if err := store.SaveSquare(sq, lines, coastlineTestAt); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	_, err := r.RouteOverWater(ctx, coralSeaMarina.Lat, coralSeaMarina.Lon, goldsmithAnchorage.Lat, goldsmithAnchorage.Lon)
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errWaterRouteTooIntricate) {
		t.Fatalf("want the caller's own deadline, got %v", err)
	}
}

func TestRouteOverWaterReader_AWayHeldInTwoSquaresIsOneCoast(t *testing.T) {
	r, store := newRouteReader(t, true)
	lines := loadShorelineFile(t, "testdata/water_route/whitsundays.json.gz")
	for _, sq := range []coverageSquare{{-21, 148}, {-21, 149}} {
		if err := store.SaveSquare(sq, lines, coastlineTestAt); err != nil {
			t.Fatal(err)
		}
	}
	got, err := r.RouteOverWater(context.Background(), haslewood.Lat, haslewood.Lon, cidHarbour.Lat, cidHarbour.Lon)
	if err != nil {
		t.Fatal(err)
	}
	if got.DistanceNm < 12 || got.DistanceNm > 20 {
		t.Errorf("routed %.1f nm", got.DistanceNm)
	}
}

func TestRouteOverWaterReader_MissingSquareIsNamed(t *testing.T) {
	r, store := newRouteReader(t, true)
	holdWhitsundays(t, store, coverageSquare{-21, 149}) // -21,148 is not held
	_, err := r.RouteOverWater(context.Background(), haslewood.Lat, haslewood.Lon, coralSea.Lat, coralSea.Lon)
	if err == nil || !strings.Contains(err.Error(), "21°S 148°E") || !strings.Contains(err.Error(), "not downloaded") {
		t.Fatalf("want the missing square named, got %v", err)
	}
}

func TestRouteOverWaterReader_FailedSquareSaysWhy(t *testing.T) {
	r, store := newRouteReader(t, true)
	holdWhitsundays(t, store, coverageSquare{-21, 149})
	if err := store.RecordFailure(coverageSquare{-21, 148}, "Overpass timed out", coastlineTestAt, coastlineTestAt.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_, err := r.RouteOverWater(context.Background(), haslewood.Lat, haslewood.Lon, coralSea.Lat, coralSea.Lon)
	if err == nil || !strings.Contains(err.Error(), "Overpass timed out") {
		t.Fatalf("want the failure reason, got %v", err)
	}
}

func TestRouteOverWaterReader_SwitchOffIsSaid(t *testing.T) {
	r, store := newRouteReader(t, false)
	holdWhitsundays(t, store, coverageSquare{-21, 149}, coverageSquare{-21, 148})
	_, err := r.RouteOverWater(context.Background(), haslewood.Lat, haslewood.Lon, cidHarbour.Lat, cidHarbour.Lon)
	if err == nil || !strings.Contains(err.Error(), "Offline coastline is off") {
		t.Fatalf("got %v", err)
	}
}

func TestRouteOverWaterReader_NoReaderIsSaid(t *testing.T) {
	var r *offlineCoastlineReader
	if _, err := r.RouteOverWater(context.Background(), haslewood.Lat, haslewood.Lon, cidHarbour.Lat, cidHarbour.Lon); err == nil {
		t.Fatal("expected an error")
	}
}

func TestRouteOverWaterReader_ACancelledContextStopsBetweenMargins(t *testing.T) {
	r, store := newRouteReader(t, true)
	holdWhitsundays(t, store, coverageSquare{-21, 149}, coverageSquare{-21, 148})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.RouteOverWater(ctx, haslewood.Lat, haslewood.Lon, cidHarbour.Lat, cidHarbour.Lon); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}
