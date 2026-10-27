package main

import (
	"math"
	"strings"
	"testing"
	"time"
)

// islandRing is a square island of the given half-size in degrees, wound the
// way OpenStreetMap winds an island: counter-clockwise, land on the left.
func islandRing(id string, lat, lon, half float64) shorelineLine {
	pts := []shorelinePoint{
		{lat - half, lon - half}, {lat - half, lon + half}, {lat + half, lon + half},
		{lat + half, lon - half}, {lat - half, lon - half},
	}
	return shorelineLine{ID: id, Kind: "coastline", Closed: true, Points: pts}
}

func routePolylineCrossesShore(route waterRoute, lines []shorelineLine) bool {
	for i := 1; i < len(route.Points); i++ {
		a, b := route.Points[i-1], route.Points[i]
		for _, l := range lines {
			for j := 1; j < len(l.Points); j++ {
				c, d := l.Points[j-1], l.Points[j]
				if segmentsCross(a.Lon, a.Lat, b.Lon, b.Lat, c.Lon, c.Lat, d.Lon, d.Lat) {
					return true
				}
			}
		}
	}
	return false
}

func TestRouteOverWater_ClearWaterIsTheStraightLine(t *testing.T) {
	from, to := shorelinePoint{-20.0, 149.0}, shorelinePoint{-20.0, 149.3}
	far := []shorelineLine{islandRing("way/far", -20.4, 149.15, 0.02)}
	r, err := routeOverWater(from, to, far, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Points) != 2 {
		t.Fatalf("clear water must give start and end only, got %d points", len(r.Points))
	}
	if math.Abs(r.DistanceNm-r.StraightLineNm) > 0.01 {
		t.Errorf("distance %.3f nm, straight line %.3f nm", r.DistanceNm, r.StraightLineNm)
	}
	if r.InitialBearingDeg < 89 || r.InitialBearingDeg > 91 {
		t.Errorf("bearing %.1f, want about 090", r.InitialBearingDeg)
	}
}

func TestRouteOverWater_IslandInTheWayIsGoneAround(t *testing.T) {
	from, to := shorelinePoint{-20.0, 149.0}, shorelinePoint{-20.0, 149.3}
	lines := []shorelineLine{islandRing("way/island", -20.0, 149.15, 0.03)}
	r, err := routeOverWater(from, to, lines, 5)
	if err != nil {
		t.Fatal(err)
	}
	if r.DistanceNm <= r.StraightLineNm*1.01 {
		t.Errorf("routed %.2f nm is not longer than the straight %.2f nm", r.DistanceNm, r.StraightLineNm)
	}
	if len(r.Points) < 3 {
		t.Errorf("expected at least one bend, got %d points", len(r.Points))
	}
	if routePolylineCrossesShore(r, lines) {
		t.Errorf("route crosses the island: %+v", r.Points)
	}
	// Going round half of a 0.06 degree square costs well under 40% extra.
	if r.DistanceNm > r.StraightLineNm*1.4 {
		t.Errorf("routed %.2f nm is far longer than needed (straight %.2f)", r.DistanceNm, r.StraightLineNm)
	}
}

func TestRouteOverWater_KeepsClearanceOffTheShore(t *testing.T) {
	from, to := shorelinePoint{-20.0, 149.0}, shorelinePoint{-20.0, 149.3}
	island := islandRing("way/island", -20.0, 149.15, 0.03)
	r, err := routeOverWater(from, to, []shorelineLine{island}, 5)
	if err != nil {
		t.Fatal(err)
	}
	// The bends are the island's corners pushed seaward, not on them.
	for _, p := range r.Points[1 : len(r.Points)-1] {
		if d := pointToLinesNm(p, []shorelineLine{island}); d < waterRouteClearanceNm*0.9 {
			t.Errorf("bend %+v is %.3f nm off the shore, want at least %.3f", p, d, waterRouteClearanceNm)
		}
	}
}

func TestRouteOverWater_OneBlockingFragmentJoinedFromTwoWays(t *testing.T) {
	// OpenStreetMap hands the coast over as fragments that share end points.
	ring := islandRing("x", -20.0, 149.15, 0.03).Points
	a := shorelineLine{ID: "way/a", Kind: "coastline", Points: ring[:3]}
	b := shorelineLine{ID: "way/b", Kind: "coastline", Points: ring[2:]}
	lines := []shorelineLine{a, b}
	r, err := routeOverWater(shorelinePoint{-20.0, 149.0}, shorelinePoint{-20.0, 149.3}, lines, 5)
	if err != nil {
		t.Fatal(err)
	}
	if routePolylineCrossesShore(r, lines) || r.DistanceNm <= r.StraightLineNm*1.01 {
		t.Errorf("fragmented island not avoided: %.2f vs %.2f, %+v", r.DistanceNm, r.StraightLineNm, r.Points)
	}
}

func TestRouteOverWater_ReefsDoNotBlock(t *testing.T) {
	reef := islandRing("way/reef", -20.0, 149.15, 0.03)
	reef.Kind = "reef"
	r, err := routeOverWater(shorelinePoint{-20.0, 149.0}, shorelinePoint{-20.0, 149.3}, []shorelineLine{reef}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Points) != 2 {
		t.Errorf("a reef is not land: %+v", r.Points)
	}
}

func TestRouteOverWater_StartOnLandIsAnError(t *testing.T) {
	// 1.5 nm inside a 0.1 degree island is beyond the snap distance.
	lines := []shorelineLine{islandRing("way/big", -20.0, 149.0, 0.1)}
	_, err := routeOverWater(shorelinePoint{-20.0, 149.0}, shorelinePoint{-20.0, 149.5}, lines, 5)
	if err == nil || !strings.Contains(err.Error(), "start") || !strings.Contains(err.Error(), "land") {
		t.Fatalf("want a start-on-land error, got %v", err)
	}
}

func TestRouteOverWater_DestinationOnLandIsAnError(t *testing.T) {
	lines := []shorelineLine{islandRing("way/big", -20.0, 149.3, 0.1)}
	_, err := routeOverWater(shorelinePoint{-20.0, 149.0}, shorelinePoint{-20.0, 149.3}, lines, 5)
	if err == nil || !strings.Contains(err.Error(), "destination") || !strings.Contains(err.Error(), "land") {
		t.Fatalf("want a destination-on-land error, got %v", err)
	}
}

func TestRouteOverWater_ShoreSideDestinationIsMovedSeawardAndSaid(t *testing.T) {
	// A town or a marina sits on the charted shore, a few metres inland.
	island := islandRing("way/island", -20.0, 149.15, 0.03)
	to := shorelinePoint{-20.0, 149.15 - 0.03 + 0.0005} // 50 m inside the west shore
	r, err := routeOverWater(shorelinePoint{-20.0, 148.9}, to, []shorelineLine{island}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if r.EndMovedNm <= 0 || r.EndMovedNm > waterRouteSnapMaxNm {
		t.Errorf("end moved %.3f nm, want a small positive figure", r.EndMovedNm)
	}
	if r.StartMovedNm != 0 {
		t.Errorf("start was in water but moved %.3f nm", r.StartMovedNm)
	}
	last := r.Points[len(r.Points)-1]
	if last.Lon >= island.Points[0].Lon {
		t.Errorf("route ends on land: %+v", last)
	}
}

func TestRouteOverWater_EnclosedWaterHasNoRoute(t *testing.T) {
	// A lagoon (a clockwise ring: water inside, land out) in the middle of an
	// island: the start sits in it and the open sea is on the far side.
	island := islandRing("way/island", -20.0, 149.05, 0.2)
	lagoon := shorelineLine{ID: "way/lagoon", Kind: "coastline", Closed: true, Points: []shorelinePoint{
		{-20.05, 149.0}, {-19.95, 149.0}, {-19.95, 149.1}, {-20.05, 149.1}, {-20.05, 149.0},
	}}
	_, err := routeOverWater(shorelinePoint{-20.0, 149.05}, shorelinePoint{-20.0, 149.5}, []shorelineLine{island, lagoon}, 5)
	if err == nil || !strings.Contains(err.Error(), "no water route") {
		t.Fatalf("want a no-route error, got %v", err)
	}
}

func TestRouteOverWater_ChannelBetweenTwoIslandsIsUsed(t *testing.T) {
	// Two islands 0.01 degrees (0.6 nm) apart with the straight line down the
	// gap: the route goes through, not round.
	a := islandRing("way/a", -20.03, 149.15, 0.02)
	b := islandRing("way/b", -19.97, 149.15, 0.02) // gap between lat -20.01 and -19.99
	lines := []shorelineLine{a, b}
	r, err := routeOverWater(shorelinePoint{-20.0, 149.0}, shorelinePoint{-20.0, 149.3}, lines, 5)
	if err != nil {
		t.Fatal(err)
	}
	if routePolylineCrossesShore(r, lines) {
		t.Fatalf("crosses: %+v", r.Points)
	}
	if r.DistanceNm > r.StraightLineNm*1.01 {
		t.Errorf("the gap should be used: %.2f vs straight %.2f", r.DistanceNm, r.StraightLineNm)
	}
}

func pointToLinesNm(p shorelinePoint, lines []shorelineLine) float64 {
	proj := newWRProjection(p.Lat, p.Lon)
	q := proj.fwd(p.Lat, p.Lon)
	best := math.Inf(1)
	for _, l := range lines {
		for j := 1; j < len(l.Points); j++ {
			d, _ := wrPointSegDist(q, wrSeg{proj.fwd(l.Points[j-1].Lat, l.Points[j-1].Lon), proj.fwd(l.Points[j].Lat, l.Points[j].Lon)})
			best = math.Min(best, d)
		}
	}
	return best
}

// ── real shoreline: the Whitsundays ───────────────────────────────────────
//
// testdata/water_route/whitsundays.json.gz is natural=coastline from the
// Overpass server for 20.55°S-19.95°S, 148.55°E-149.35°E, captured
// 2026-10-27. The boat was at Haslewood Island, east of Whitsunday Island.

var (
	haslewood  = shorelinePoint{-20.2748, 149.0737}
	cidHarbour = shorelinePoint{-20.25, 148.95}
	coralSea   = shorelinePoint{-20.265, 148.71}
)

func TestRouteOverWater_WhitsundaysRunsRoundTheIslandNotThroughIt(t *testing.T) {
	lines := loadShorelineFile(t, "testdata/water_route/whitsundays.json.gz")
	for _, c := range []struct {
		name         string
		to           shorelinePoint
		minNm, maxNm float64
	}{
		// Straight lines were 7.3 nm and 20.5 nm; both cross Whitsunday Island.
		{"Cid Harbour", cidHarbour, 10, 30},
		{"Coral Sea Marina", coralSea, 22, 40},
	} {
		t.Run(c.name, func(t *testing.T) {
			start := time.Now()
			r, err := routeOverWater(haslewood, c.to, lines, 6)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s: %.1f nm routed, %.1f nm straight, bearing %03.0f, %d points, start moved %.2f end moved %.2f nm, %v",
				c.name, r.DistanceNm, r.StraightLineNm, r.InitialBearingDeg, len(r.Points), r.StartMovedNm, r.EndMovedNm, time.Since(start))
			if r.DistanceNm < c.minNm || r.DistanceNm > c.maxNm {
				t.Errorf("routed %.1f nm outside %.0f-%.0f", r.DistanceNm, c.minNm, c.maxNm)
			}
			if r.DistanceNm < r.StraightLineNm*1.1 {
				t.Errorf("routed %.1f nm is barely longer than the straight %.1f: it went through the island", r.DistanceNm, r.StraightLineNm)
			}
			if routePolylineCrossesShore(r, lines) {
				t.Errorf("route crosses the shoreline: %+v", r.Points)
			}
			if time.Since(start) > 2*time.Second {
				t.Errorf("took %v", time.Since(start))
			}
		})
	}
}
