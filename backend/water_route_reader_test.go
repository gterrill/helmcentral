package main

import (
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
	got, err := r.RouteOverWater(haslewood.Lat, haslewood.Lon, cidHarbour.Lat, cidHarbour.Lon)
	if err != nil {
		t.Fatal(err)
	}
	if got.DistanceNm < 12 || got.DistanceNm > 20 || got.StraightLineNm > 8 {
		t.Errorf("Haslewood to Cid Harbour: routed %.1f nm, straight %.1f nm", got.DistanceNm, got.StraightLineNm)
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
	got, err := r.RouteOverWater(haslewood.Lat, haslewood.Lon, cidHarbour.Lat, cidHarbour.Lon)
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
	_, err := r.RouteOverWater(haslewood.Lat, haslewood.Lon, coralSea.Lat, coralSea.Lon)
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
	_, err := r.RouteOverWater(haslewood.Lat, haslewood.Lon, coralSea.Lat, coralSea.Lon)
	if err == nil || !strings.Contains(err.Error(), "Overpass timed out") {
		t.Fatalf("want the failure reason, got %v", err)
	}
}

func TestRouteOverWaterReader_SwitchOffIsSaid(t *testing.T) {
	r, store := newRouteReader(t, false)
	holdWhitsundays(t, store, coverageSquare{-21, 149}, coverageSquare{-21, 148})
	_, err := r.RouteOverWater(haslewood.Lat, haslewood.Lon, cidHarbour.Lat, cidHarbour.Lon)
	if err == nil || !strings.Contains(err.Error(), "Offline coastline is off") {
		t.Fatalf("got %v", err)
	}
}

func TestRouteOverWaterReader_NoReaderIsSaid(t *testing.T) {
	var r *offlineCoastlineReader
	if _, err := r.RouteOverWater(haslewood.Lat, haslewood.Lon, cidHarbour.Lat, cidHarbour.Lon); err == nil {
		t.Fatal("expected an error")
	}
}
