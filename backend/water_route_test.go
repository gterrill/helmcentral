package main

import (
	"context"
	"errors"
	"math"
	"math/rand"
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
	r, err := routeOverWater(context.Background(), from, to, far, 5)
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
	r, err := routeOverWater(context.Background(), from, to, lines, 5)
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
	r, err := routeOverWater(context.Background(), from, to, []shorelineLine{island}, 5)
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
	r, err := routeOverWater(context.Background(), shorelinePoint{-20.0, 149.0}, shorelinePoint{-20.0, 149.3}, lines, 5)
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
	r, err := routeOverWater(context.Background(), shorelinePoint{-20.0, 149.0}, shorelinePoint{-20.0, 149.3}, []shorelineLine{reef}, 5)
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
	_, err := routeOverWater(context.Background(), shorelinePoint{-20.0, 149.0}, shorelinePoint{-20.0, 149.5}, lines, 5)
	if err == nil || !strings.Contains(err.Error(), "start") || !strings.Contains(err.Error(), "land") {
		t.Fatalf("want a start-on-land error, got %v", err)
	}
}

func TestRouteOverWater_DestinationOnLandIsAnError(t *testing.T) {
	lines := []shorelineLine{islandRing("way/big", -20.0, 149.3, 0.1)}
	_, err := routeOverWater(context.Background(), shorelinePoint{-20.0, 149.0}, shorelinePoint{-20.0, 149.3}, lines, 5)
	if err == nil || !strings.Contains(err.Error(), "destination") || !strings.Contains(err.Error(), "land") {
		t.Fatalf("want a destination-on-land error, got %v", err)
	}
}

func TestRouteOverWater_ShoreSideDestinationIsMovedSeawardAndSaid(t *testing.T) {
	// A town or a marina sits on the charted shore, a few metres inland.
	island := islandRing("way/island", -20.0, 149.15, 0.03)
	to := shorelinePoint{-20.0, 149.15 - 0.03 + 0.0005} // 50 m inside the west shore
	r, err := routeOverWater(context.Background(), shorelinePoint{-20.0, 148.9}, to, []shorelineLine{island}, 5)
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
	_, err := routeOverWater(context.Background(), shorelinePoint{-20.0, 149.05}, shorelinePoint{-20.0, 149.5}, []shorelineLine{island, lagoon}, 5)
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
	r, err := routeOverWater(context.Background(), shorelinePoint{-20.0, 149.0}, shorelinePoint{-20.0, 149.3}, lines, 5)
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
			r, err := routeOverWater(context.Background(), haslewood, c.to, lines, 6)
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

// The coast between Airlie Beach and Goldsmith Island holds more than 2700
// corners within 8 nm of the run. It must route, not refuse to.
var (
	coralSeaMarina     = shorelinePoint{-20.2666, 148.7099}
	goldsmithAnchorage = shorelinePoint{-20.668, 149.135}
)

func TestRouteOverWater_IntricateCoastStillRoutes(t *testing.T) {
	lines := loadShorelineFile(t, "testdata/water_route/whitsundays-goldsmith.json.gz")
	start := time.Now()
	r, err := routeOverWater(context.Background(), coralSeaMarina, goldsmithAnchorage, lines, 8)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%.2f nm routed, %.2f nm straight, %d points, %v", r.DistanceNm, r.StraightLineNm, len(r.Points), time.Since(start))
	if r.DistanceNm < 36 || r.DistanceNm > 39 {
		t.Errorf("routed %.2f nm, want 36-39", r.DistanceNm)
	}
	if r.DistanceNm < r.StraightLineNm*1.05 {
		t.Errorf("routed %.2f nm is barely longer than the straight %.2f: it went through land", r.DistanceNm, r.StraightLineNm)
	}
	if routePolylineCrossesShore(r, lines) {
		t.Errorf("route crosses the shoreline: %+v", r.Points)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("took %v", time.Since(start))
	}
}

// A settled node queues its candidate edges a batch at a time. That is a way
// of holding fewer of them, not of choosing among them, so the distance must
// not depend on the batch size. Route random pairs of water points through the
// Whitsundays with one candidate to a batch and with every candidate in one,
// which is the queue as it was before batching, and demand the same distance.
func TestRouteOverWater_BatchSizeNeverChangesAnAnswer(t *testing.T) {
	lines := loadShorelineFile(t, "testdata/water_route/whitsundays-goldsmith.json.gz")
	oldBatch, oldQueued := waterRouteBatch, waterRouteMaxQueued
	t.Cleanup(func() { waterRouteBatch, waterRouteMaxQueued = oldBatch, oldQueued })
	waterRouteMaxQueued = 1 << 30 // the all-in-one run must not meet the bound

	// Water points are told from land the way the router does: by the side of
	// the nearest shoreline segment, in a projection about the middle of the area.
	proj := newWRProjection(-20.5, 149.0)
	var segs []wrSeg
	for _, chain := range chainCoastline(lines) {
		for i := 1; i < len(chain); i++ {
			segs = append(segs, wrSeg{proj.fwd(chain[i-1].Lat, chain[i-1].Lon), proj.fwd(chain[i].Lat, chain[i].Lon)})
		}
	}
	rng := rand.New(rand.NewSource(222))
	water := func() shorelinePoint {
		for {
			p := shorelinePoint{Lat: -20.95 + rng.Float64()*0.9, Lon: 148.55 + rng.Float64()*0.9}
			if d, _, _, onLand, _ := nearestShore(proj.fwd(p.Lat, p.Lon), segs); !onLand && d > 0.1 {
				return p
			}
		}
	}

	want := 40
	if testing.Short() {
		want = 12
	}
	var routed, refused int
	for tried := 0; tried < want; {
		a, b := water(), water()
		if straight := haversineMeters(a.Lat, a.Lon, b.Lat, b.Lon) / metersPerNauticalMile; straight < 1 || straight > 25 {
			continue
		}
		tried++
		waterRouteBatch = 1
		one, errOne := routeOverWater(context.Background(), a, b, lines, 6)
		waterRouteBatch = 1 << 20
		all, errAll := routeOverWater(context.Background(), a, b, lines, 6)
		switch {
		case errOne != nil || errAll != nil:
			if errOne == nil || errAll == nil || errOne.Error() != errAll.Error() {
				t.Errorf("%v to %v: one at a time gave %v, all at once %v", a, b, errOne, errAll)
			}
			refused++
		case math.Abs(one.DistanceNm-all.DistanceNm) > 1e-6:
			t.Errorf("%v to %v: one at a time %.6f nm, all at once %.6f nm", a, b, one.DistanceNm, all.DistanceNm)
		default:
			routed++
		}
	}
	t.Logf("%d pairs: %d routed the same either way, %d refused alike (closed off, or on land)", want, routed, refused)
}

// The queue is bounded by what it holds. A search that would hold more is
// refused, and says so rather than returning the best path it had.
func TestRouteOverWater_AQueueBeyondTheBoundIsRefused(t *testing.T) {
	lines := loadShorelineFile(t, "testdata/water_route/whitsundays-goldsmith.json.gz")
	old := waterRouteMaxQueued
	t.Cleanup(func() { waterRouteMaxQueued = old })
	waterRouteMaxQueued = 200
	_, err := routeOverWater(context.Background(), coralSeaMarina, goldsmithAnchorage, lines, 8)
	if err == nil || !strings.Contains(err.Error(), "too intricate to route in one go") || !strings.Contains(err.Error(), "shorter legs") {
		t.Fatalf("want the coast named too intricate, got %v", err)
	}
	if errors.Is(err, errNoWaterRoute) {
		t.Errorf("a refused search is not a closed-off one: %v", err)
	}
	if !errors.Is(err, errWaterRouteTooIntricate) {
		t.Errorf("the refusal should wrap errWaterRouteTooIntricate: %v", err)
	}
}

// segmentsCross reports whether the open segments ab and cd properly cross.
func segmentsCross(ax, ay, bx, by, cx, cy, dx, dy float64) bool {
	return wrSegDist(wrSeg{wrPt{ax, ay}, wrPt{bx, by}}, wrSeg{wrPt{cx, cy}, wrPt{dx, dy}}) == 0
}

func TestRouteOverWater_ExactMarginCoversTheEllipseSideways(t *testing.T) {
	// Any shorter path lies inside the ellipse with the two ends as foci and
	// this path's length as its length. It reaches sqrt(L²-s²)/2 to the side
	// of the line, which is more than the (L-s)/2 it overshoots the ends.
	lines := []shorelineLine{islandRing("way/island", -20.0, 149.15, 0.03)}
	r, err := routeOverWater(context.Background(), shorelinePoint{-20.0, 149.0}, shorelinePoint{-20.0, 149.3}, lines, 5)
	if err != nil {
		t.Fatal(err)
	}
	l, s := r.DistanceNm, r.StraightLineNm
	want := math.Sqrt(l*l-s*s) / 2
	if math.Abs(r.ExactWithinNm-want) > 1e-9 {
		t.Errorf("ExactWithinNm %.4f, want sqrt(L²-s²)/2 = %.4f", r.ExactWithinNm, want)
	}
	if r.ExactWithinNm <= (l-s)/2 {
		t.Errorf("the sideways reach %.4f must exceed the overshoot %.4f", r.ExactWithinNm, (l-s)/2)
	}
}

func TestRouteOverWater_ACancelledContextStopsTheSearch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	lines := loadShorelineFile(t, "testdata/water_route/whitsundays.json.gz")
	_, err := routeOverWater(ctx, haslewood, coralSea, lines, 6)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}
