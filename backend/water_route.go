package main

import (
	"context"
	"errors"
	"fmt"
	"math"
)

// ── routing over water (ADR 0216) ─────────────────────────────────────────
//
// A passage distance is the length of a path that stays on the water, never
// the straight line between two points. The path is found over the charted
// coastline held in the offline coastline (coastline_store.go):
//
//  1. Coastline fragments that share an end point are joined, then thinned
//     with Douglas-Peucker to waterRouteSimplifyNm.
//  2. OpenStreetMap winds the coastline with land on the left and water on
//     the right. That is what tells a point on land from one on water (the
//     side of the nearest shoreline segment) and which corners a path can
//     bend round: only a left turn (towards the land) is a headland that
//     sticks out into the water. Every such corner becomes a graph node, pushed seaward by
//     waterRouteClearanceNm along the bisector of its two segments.
//  3. Two nodes are joined when the straight line between them crosses no
//     shoreline segment and stays clear of every one by half the clearance.
//     The graph is never built whole: A* tests an edge only when it could
//     improve a node, and a spatial grid keeps each test to the shoreline
//     segments near the line.
//
// Reefs are not shoreline here. A charted reef is a hazard to read off the
// chart, not land, and treating one as a wall would shut anchorages inside
// fringing reef. Nothing here masks a missing input: the caller proves the
// shoreline for the corridor is held, and every failure below is an error
// that says what is wrong.

const (
	// waterRouteClearanceNm is how far a bend is kept off the shore. 0.05 nm
	// (about 90 m) is wide enough to keep a path off a charted edge that may
	// be a few tens of metres out, and narrow enough that the passages
	// between islands a cruiser really uses stay open.
	waterRouteClearanceNm = 0.05

	// waterRouteSimplifyNm is the Douglas-Peucker tolerance for the
	// shoreline, well inside the clearance.
	waterRouteSimplifyNm = 0.01

	// waterRouteSnapMaxNm is how far inland a start or destination may sit
	// and still be moved to the water. A town, a jetty or a marina is charted
	// as a point a few metres behind the shoreline; the route starts or ends
	// at the water in front of it and says how far it moved. Further inland
	// than this is an error, not a guess.
	waterRouteSnapMaxNm = 0.5

	// waterRouteMaxStraightNm bounds a request so a typo cannot ask the
	// search to load half an ocean.
	waterRouteMaxStraightNm = 300.0

	// waterRouteMiterLimit caps how far a sharp corner's nudge is pushed, in
	// clearances.
	waterRouteMiterLimit = 3.0

	// waterRouteGridCellNm is the spatial index cell.
	waterRouteGridCellNm = 0.25
)

// waterRouteMaxNodes bounds the search: the queue holds every candidate edge,
// so memory grows with the square of this.
const waterRouteMaxNodes = 2000

var errNoWaterRoute = errors.New("no water route found")

// waterRoute is a path over water from the start to the destination.
type waterRoute struct {
	// Points runs from the start to the destination, with a bend at each
	// headland the path rounds.
	Points            []shorelinePoint
	DistanceNm        float64
	StraightLineNm    float64
	InitialBearingDeg float64 // true, of the first leg
	// StartMovedNm and EndMovedNm are how far an end point on the charted
	// shore was moved to the water in front of it. Zero when it was on water.
	StartMovedNm float64
	EndMovedNm   float64
	// ExactWithinNm is the margin the search needs for this path to be the
	// shortest there is: any shorter path lies inside the ellipse whose
	// foci are the two ends and whose length is this path's, which reaches
	// (DistanceNm - StraightLineNm) / 2 beyond the box the ends span.
	ExactWithinNm float64
}

type wrPt struct{ x, y float64 }

type wrSeg struct{ a, b wrPt }

// wrProjection is a flat projection in nautical miles around a centre.
type wrProjection struct{ lat0, lon0, kx float64 }

func newWRProjection(lat, lon float64) wrProjection {
	return wrProjection{lat0: lat, lon0: lon, kx: 60 * math.Cos(lat*math.Pi/180)}
}

func (p wrProjection) fwd(lat, lon float64) wrPt {
	return wrPt{(lon - p.lon0) * p.kx, (lat - p.lat0) * 60}
}

func (p wrProjection) inv(q wrPt) shorelinePoint {
	return shorelinePoint{Lat: p.lat0 + q.y/60, Lon: p.lon0 + q.x/p.kx}
}

func wrSub(a, b wrPt) wrPt           { return wrPt{a.x - b.x, a.y - b.y} }
func wrCross(a, b wrPt) float64      { return a.x*b.y - a.y*b.x }
func wrDot(a, b wrPt) float64        { return a.x*b.x + a.y*b.y }
func wrLen(a wrPt) float64           { return math.Hypot(a.x, a.y) }
func wrDist(a, b wrPt) float64       { return math.Hypot(a.x-b.x, a.y-b.y) }
func wrScale(a wrPt, k float64) wrPt { return wrPt{a.x * k, a.y * k} }
func wrAdd(a, b wrPt) wrPt           { return wrPt{a.x + b.x, a.y + b.y} }

func wrPointSegDist(p wrPt, s wrSeg) (dist, t float64) {
	ab := wrSub(s.b, s.a)
	l2 := wrDot(ab, ab)
	if l2 == 0 {
		return wrDist(p, s.a), 0
	}
	t = wrDot(wrSub(p, s.a), ab) / l2
	tc := math.Max(0, math.Min(1, t))
	return wrDist(p, wrAdd(s.a, wrScale(ab, tc))), t
}

// wrSegDist is the distance between two segments, 0 when they cross.
func wrSegDist(s, u wrSeg) float64 {
	d1 := wrCross(wrSub(s.b, s.a), wrSub(u.a, s.a))
	d2 := wrCross(wrSub(s.b, s.a), wrSub(u.b, s.a))
	d3 := wrCross(wrSub(u.b, u.a), wrSub(s.a, u.a))
	d4 := wrCross(wrSub(u.b, u.a), wrSub(s.b, u.a))
	if ((d1 > 0 && d2 < 0) || (d1 < 0 && d2 > 0)) && ((d3 > 0 && d4 < 0) || (d3 < 0 && d4 > 0)) {
		return 0
	}
	m, _ := wrPointSegDist(u.a, s)
	for _, d := range []float64{
		func() float64 { v, _ := wrPointSegDist(u.b, s); return v }(),
		func() float64 { v, _ := wrPointSegDist(s.a, u); return v }(),
		func() float64 { v, _ := wrPointSegDist(s.b, u); return v }(),
	} {
		m = math.Min(m, d)
	}
	return m
}

// ── shoreline preparation ─────────────────────────────────────────────────

type wrKey struct{ lat, lon float64 }

// chainCoastline joins coastline fragments that share an end point into
// longer polylines, keeping their winding. Reef lines are dropped.
func chainCoastline(lines []shorelineLine) [][]shorelinePoint {
	// A way that crosses a square's edge is held whole in both squares, so
	// the same id can arrive twice.
	var frags [][]shorelinePoint
	seen := map[string]bool{}
	for _, l := range lines {
		if l.Kind != "coastline" || len(l.Points) < 2 || (l.ID != "" && seen[l.ID]) {
			continue
		}
		seen[l.ID] = true
		frags = append(frags, l.Points)
	}
	starts := map[wrKey][]int{}
	ends := map[wrKey][]int{}
	for i, f := range frags {
		s, e := f[0], f[len(f)-1]
		if s == e {
			continue // a closed ring cannot be extended
		}
		starts[wrKey{s.Lat, s.Lon}] = append(starts[wrKey{s.Lat, s.Lon}], i)
		ends[wrKey{e.Lat, e.Lon}] = append(ends[wrKey{e.Lat, e.Lon}], i)
	}
	used := make([]bool, len(frags))
	take := func(m map[wrKey][]int, k wrKey) int {
		for _, i := range m[k] {
			if !used[i] {
				return i
			}
		}
		return -1
	}
	var out [][]shorelinePoint
	for i, f := range frags {
		if used[i] {
			continue
		}
		used[i] = true
		chain := append([]shorelinePoint(nil), f...)
		if f[0] != f[len(f)-1] {
			for {
				e := chain[len(chain)-1]
				j := take(starts, wrKey{e.Lat, e.Lon})
				if j < 0 {
					break
				}
				used[j] = true
				chain = append(chain, frags[j][1:]...)
				if chain[0] == chain[len(chain)-1] {
					break
				}
			}
			for chain[0] != chain[len(chain)-1] {
				s := chain[0]
				j := take(ends, wrKey{s.Lat, s.Lon})
				if j < 0 {
					break
				}
				used[j] = true
				chain = append(append([]shorelinePoint(nil), frags[j][:len(frags[j])-1]...), chain...)
			}
		}
		out = append(out, chain)
	}
	return out
}

// wrSimplify thins a projected polyline, keeping its end points.
func wrSimplify(pts []wrPt, tol float64) []wrPt {
	if len(pts) < 3 {
		return pts
	}
	keep := make([]bool, len(pts))
	keep[0], keep[len(pts)-1] = true, true
	type span struct{ i, j int }
	stack := []span{{0, len(pts) - 1}}
	for len(stack) > 0 {
		s := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		far, farD := -1, tol
		for k := s.i + 1; k < s.j; k++ {
			if d, _ := wrPointSegDist(pts[k], wrSeg{pts[s.i], pts[s.j]}); d > farD {
				far, farD = k, d
			}
		}
		if far >= 0 {
			keep[far] = true
			stack = append(stack, span{s.i, far}, span{far, s.j})
		}
	}
	out := make([]wrPt, 0, len(pts))
	for i, p := range pts {
		if keep[i] {
			out = append(out, p)
		}
	}
	return out
}

// ── spatial index ─────────────────────────────────────────────────────────

type wrIndex struct {
	segs     []wrSeg
	minX     float64
	minY     float64
	w, h     int
	cells    [][]int32
	cellSeen []uint32
	seen     []uint32
	stamp    uint32
}

func (ix *wrIndex) cellOf(p wrPt) (int, int) {
	return int(math.Floor((p.x - ix.minX) / waterRouteGridCellNm)), int(math.Floor((p.y - ix.minY) / waterRouteGridCellNm))
}

// walk calls fn for the cell of each sample along a segment, spaced at
// step, so a cell the segment crosses is missed only by a corner. fn is not
// called twice in a row for one cell.
func (ix *wrIndex) walk(s wrSeg, step float64, fn func(i, j int)) {
	l := wrDist(s.a, s.b)
	n := int(math.Ceil(l/step)) + 1
	li, lj := math.MinInt, math.MinInt
	for k := 0; k <= n; k++ {
		t := float64(k) / float64(n)
		i, j := ix.cellOf(wrAdd(s.a, wrScale(wrSub(s.b, s.a), t)))
		if i != li || j != lj {
			fn(i, j)
			li, lj = i, j
		}
	}
}

// newWRIndex indexes segments on a grid over the box. Every segment passed
// must lie inside it.
func newWRIndex(segs []wrSeg, minX, minY, maxX, maxY float64) *wrIndex {
	ix := &wrIndex{segs: segs, minX: minX, minY: minY}
	ix.w = int((maxX-minX)/waterRouteGridCellNm) + 3
	ix.h = int((maxY-minY)/waterRouteGridCellNm) + 3
	ix.cells = make([][]int32, ix.w*ix.h)
	ix.cellSeen = make([]uint32, ix.w*ix.h)
	ix.seen = make([]uint32, len(segs))
	for id, s := range segs {
		ix.walk(s, waterRouteGridCellNm/8, func(i, j int) {
			if k, ok := ix.slot(i, j); ok {
				ix.cells[k] = append(ix.cells[k], int32(id))
			}
		})
	}
	return ix
}

func (ix *wrIndex) slot(i, j int) (int, bool) {
	if i < 0 || j < 0 || i >= ix.w || j >= ix.h {
		return 0, false
	}
	return j*ix.w + i, true
}

// clear reports whether the segment keeps more than thr from every shoreline
// segment, ignoring the two segments named (a node's own neighbours). thr 0
// asks only that it crosses none. thr is far below the cell size.
func (ix *wrIndex) clear(s wrSeg, thr float64, skipA, skipB int) bool {
	ix.stamp++
	loX, hiX := math.Min(s.a.x, s.b.x)-thr, math.Max(s.a.x, s.b.x)+thr
	loY, hiY := math.Min(s.a.y, s.b.y)-thr, math.Max(s.a.y, s.b.y)+thr
	ok := true
	ix.walk(s, waterRouteGridCellNm/2, func(ci, cj int) {
		for di := -1; di <= 1 && ok; di++ {
			for dj := -1; dj <= 1 && ok; dj++ {
				k, in := ix.slot(ci+di, cj+dj)
				if !in || ix.cellSeen[k] == ix.stamp {
					continue
				}
				ix.cellSeen[k] = ix.stamp
				for _, id := range ix.cells[k] {
					if ix.seen[id] == ix.stamp || int(id) == skipA || int(id) == skipB {
						continue
					}
					ix.seen[id] = ix.stamp
					o := ix.segs[id]
					if math.Max(o.a.x, o.b.x) < loX || math.Min(o.a.x, o.b.x) > hiX || math.Max(o.a.y, o.b.y) < loY || math.Min(o.a.y, o.b.y) > hiY {
						continue
					}
					if wrSegDist(s, o) <= thr {
						ok = false
						return
					}
				}
			}
		}
	})
	return ok
}

// ── land and water ────────────────────────────────────────────────────────

// nearestShore returns the distance to the nearest shoreline segment, the
// closest point on it and the unit normal pointing to the water side. found
// is false when there is no shoreline at all. On a tie (a point level with
// a vertex) the water side wins unless every tied segment says land.
func nearestShore(p wrPt, segs []wrSeg) (dist float64, q wrPt, water wrPt, onLand bool, found bool) {
	best := math.Inf(1)
	var bi int
	for i, s := range segs {
		if d, _ := wrPointSegDist(p, s); d < best {
			best, bi = d, i
		}
	}
	if math.IsInf(best, 1) {
		return 0, wrPt{}, wrPt{}, false, false
	}
	onLand = true
	for _, s := range segs {
		d, _ := wrPointSegDist(p, s)
		if d > best+1e-9 {
			continue
		}
		if wrCross(wrSub(s.b, s.a), wrSub(p, s.a)) < 0 {
			onLand = false
		}
	}
	s := segs[bi]
	ab := wrSub(s.b, s.a)
	_, t := wrPointSegDist(p, s)
	t = math.Max(0, math.Min(1, t))
	q = wrAdd(s.a, wrScale(ab, t))
	l := wrLen(ab)
	if l == 0 {
		return best, q, wrPt{}, onLand, true
	}
	water = wrPt{ab.y / l, -ab.x / l} // right of the direction of travel
	return best, q, water, onLand, true
}

// ── the search ────────────────────────────────────────────────────────────

type wrItem struct {
	node, from int
	g, f       float64
}

// wrHeap is a binary min-heap on f. It is typed so a push does not box.
type wrHeap []wrItem

func (h *wrHeap) push(it wrItem) {
	*h = append(*h, it)
	a := *h
	i := len(a) - 1
	for i > 0 {
		p := (i - 1) / 2
		if a[p].f <= a[i].f {
			break
		}
		a[p], a[i] = a[i], a[p]
		i = p
	}
}

func (h *wrHeap) pop() wrItem {
	a := *h
	top := a[0]
	last := len(a) - 1
	a[0] = a[last]
	a = a[:last]
	i := 0
	for {
		l, r, m := 2*i+1, 2*i+2, i
		if l < len(a) && a[l].f < a[m].f {
			m = l
		}
		if r < len(a) && a[r].f < a[m].f {
			m = r
		}
		if m == i {
			break
		}
		a[i], a[m] = a[m], a[i]
		i = m
	}
	*h = a
	return top
}

// routeOverWater finds the shortest path over water from one point to
// another. lines must hold every coastline within marginNm of the box the two
// points span; the caller proves that. marginNm bounds where bends are
// looked for.
func routeOverWater(ctx context.Context, from, to shorelinePoint, lines []shorelineLine, marginNm float64) (waterRoute, error) {
	straight := haversineMeters(from.Lat, from.Lon, to.Lat, to.Lon) / metersPerNauticalMile
	if straight > waterRouteMaxStraightNm {
		return waterRoute{}, fmt.Errorf("a %.0f nm run is more than the %.0f nm that can be routed over water in one go; plan it in legs", straight, waterRouteMaxStraightNm)
	}
	proj := newWRProjection((from.Lat+to.Lat)/2, (from.Lon+to.Lon)/2)

	// Shoreline: joined, projected, thinned. polys keeps the vertex order
	// that gives the winding; allSegs feeds the land test.
	var polys [][]wrPt
	var allSegs []wrSeg
	for _, chain := range chainCoastline(lines) {
		pts := make([]wrPt, len(chain))
		for i, p := range chain {
			pts[i] = proj.fwd(p.Lat, p.Lon)
		}
		closed := chain[0] == chain[len(chain)-1]
		pts = wrSimplify(pts, waterRouteSimplifyNm)
		if closed && len(pts) < 4 {
			continue
		}
		polys = append(polys, pts)
		for i := 1; i < len(pts); i++ {
			allSegs = append(allSegs, wrSeg{pts[i-1], pts[i]})
		}
	}

	ends := [2]struct {
		name string
		in   shorelinePoint
		pt   wrPt
		near float64
		movd float64
	}{{name: "start", in: from}, {name: "destination", in: to}}
	for i := range ends {
		e := &ends[i]
		e.pt = proj.fwd(e.in.Lat, e.in.Lon)
		e.near = math.Inf(1)
		d, q, water, onLand, found := nearestShore(e.pt, allSegs)
		if found {
			e.near = d
		}
		if found && onLand {
			if d > waterRouteSnapMaxNm {
				return waterRoute{}, fmt.Errorf("the %s (%.4f, %.4f) is on land, %.1f nm inside the charted shoreline", e.name, e.in.Lat, e.in.Lon, d)
			}
			moved := wrAdd(q, wrScale(water, waterRouteClearanceNm))
			if _, _, _, still, _ := nearestShore(moved, allSegs); still {
				return waterRoute{}, fmt.Errorf("the %s (%.4f, %.4f) is on land and the water in front of it could not be found", e.name, e.in.Lat, e.in.Lon)
			}
			e.movd = wrDist(e.pt, moved)
			e.pt = moved
			e.near = waterRouteClearanceNm
		}
	}
	start, end := ends[0].pt, ends[1].pt

	// Only shoreline near the two points matters to the path.
	minX, maxX := math.Min(start.x, end.x)-marginNm, math.Max(start.x, end.x)+marginNm
	minY, maxY := math.Min(start.y, end.y)-marginNm, math.Max(start.y, end.y)+marginNm
	inBox := func(p wrPt) bool { return p.x >= minX && p.x <= maxX && p.y >= minY && p.y <= maxY }
	var segs []wrSeg
	segAt := map[[2]int]int{} // (poly, vertex) -> index of the segment starting there
	segIdx := func(pi, i int) int {
		if k, ok := segAt[[2]int{pi, i}]; ok {
			return k
		}
		return -1 // outside the box: never indexed, so nothing to skip
	}
	for pi, pts := range polys {
		for i := 1; i < len(pts); i++ {
			a, b := pts[i-1], pts[i]
			if (a.x < minX && b.x < minX) || (a.x > maxX && b.x > maxX) || (a.y < minY && b.y < minY) || (a.y > maxY && b.y > maxY) {
				continue
			}
			segAt[[2]int{pi, i - 1}] = len(segs)
			segs = append(segs, wrSeg{a, b})
		}
	}
	ix := newWRIndex(segs, minX-1, minY-1, maxX+1, maxY+1)

	finish := func(path []wrPt) waterRoute {
		r := waterRoute{StraightLineNm: straight, StartMovedNm: ends[0].movd, EndMovedNm: ends[1].movd}
		for _, p := range path {
			r.Points = append(r.Points, proj.inv(p))
		}
		// The ends are the points asked for, unless one was moved off land.
		if ends[0].movd == 0 {
			r.Points[0] = from
		}
		if ends[1].movd == 0 {
			r.Points[len(r.Points)-1] = to
		}
		for i := 1; i < len(r.Points); i++ {
			r.DistanceNm += haversineMeters(r.Points[i-1].Lat, r.Points[i-1].Lon, r.Points[i].Lat, r.Points[i].Lon) / metersPerNauticalMile
		}
		r.ExactWithinNm = math.Sqrt(math.Max(0, r.DistanceNm*r.DistanceNm-r.StraightLineNm*r.StraightLineNm)) / 2
		r.InitialBearingDeg = bearingDeg(r.Points[0].Lat, r.Points[0].Lon, r.Points[1].Lat, r.Points[1].Lon)
		return r
	}

	edgeThr := func(na, nb float64) float64 {
		return math.Min(waterRouteClearanceNm*0.5, 0.9*math.Min(na, nb))
	}
	if ix.clear(wrSeg{start, end}, edgeThr(ends[0].near, ends[1].near), -1, -1) {
		return finish([]wrPt{start, end}), nil
	}

	// Nodes: start, destination, then every headland corner in the box.
	pos := []wrPt{start, end}
	near := []float64{ends[0].near, ends[1].near}
	c := waterRouteClearanceNm
	for pi, pts := range polys {
		closed := len(pts) >= 4 && pts[0] == pts[len(pts)-1]
		for i := 0; i < len(pts); i++ {
			v := pts[i]
			if !inBox(v) {
				continue
			}
			var din, dout wrPt
			haveIn, haveOut := false, false
			inSeg, outSeg := -1, -1
			switch {
			case i > 0:
				din, haveIn = wrSub(v, pts[i-1]), true
				inSeg = segIdx(pi, i-1)
			case closed:
				din, haveIn = wrSub(v, pts[len(pts)-2]), true
				inSeg = segIdx(pi, len(pts)-2)
			}
			switch {
			case i < len(pts)-1:
				dout, haveOut = wrSub(pts[i+1], v), true
				outSeg = segIdx(pi, i)
			case closed:
				dout, haveOut = wrSub(pts[1], v), true
				outSeg = segIdx(pi, 0)
			}
			if closed && i == len(pts)-1 {
				continue // the same vertex as i == 0
			}
			var nudge wrPt
			switch {
			case haveIn && haveOut:
				if wrCross(din, dout) <= 0 {
					continue // a bay, not a headland
				}
				n1 := wrPt{din.y, -din.x}
				n2 := wrPt{dout.y, -dout.x}
				n1, n2 = wrScale(n1, 1/wrLen(n1)), wrScale(n2, 1/wrLen(n2))
				bis := wrAdd(n1, n2)
				bl := wrLen(bis)
				if bl < 1e-6 {
					nudge = wrScale(n1, c)
					break
				}
				m := math.Min(c/(bl/2), c*waterRouteMiterLimit)
				nudge = wrScale(bis, m/bl)
			case haveOut || haveIn:
				// The loose end of a fragment: round it.
				d := dout
				if !haveOut {
					d = din
				}
				l := wrLen(d)
				if l == 0 {
					continue
				}
				n := wrPt{d.y / l, -d.x / l}
				axis := wrScale(d, 1/l)
				if !haveOut {
					nudge = wrScale(wrAdd(n, axis), c/math.Sqrt2)
				} else {
					nudge = wrScale(wrAdd(n, wrScale(axis, -1)), c/math.Sqrt2)
				}
			default:
				continue
			}
			p := wrAdd(v, nudge)
			if !ix.clear(wrSeg{v, p}, 0, inSeg, outSeg) {
				continue // squeezed against other shore
			}
			if wrDist(start, p)+wrDist(p, end) > wrDist(start, end)+2*marginNm {
				continue // too far out of the way to be on a path within the margin
			}
			pos = append(pos, p)
			near = append(near, c)
		}
	}

	if len(pos) > waterRouteMaxNodes {
		return waterRoute{}, fmt.Errorf("the shoreline between %.4f, %.4f and %.4f, %.4f is too intricate to route in one go (%d corners); plan it in shorter legs", from.Lat, from.Lon, to.Lat, to.Lon, len(pos))
	}

	// A* with lazy edges: a candidate edge goes on the queue untested and is
	// tested only when it reaches the front. Most are never reached, and the
	// visibility test is where all the time goes.
	n := len(pos)
	g := make([]float64, n)
	parent := make([]int, n)
	settled := make([]bool, n)
	h := func(i int) float64 { return wrDist(pos[i], end) }
	pq := &wrHeap{}
	pops := 0
	push := func(from int) {
		for v := 0; v < n; v++ {
			if !settled[v] {
				nd := g[from] + wrDist(pos[from], pos[v])
				pq.push(wrItem{node: v, from: from, g: nd, f: nd + h(v)})
			}
		}
	}
	settled[0] = true
	push(0)
	for len(*pq) > 0 {
		if pops++; pops%256 == 0 {
			if err := ctx.Err(); err != nil {
				return waterRoute{}, err
			}
		}
		it := pq.pop()
		v := it.node
		if settled[v] {
			continue
		}
		if !ix.clear(wrSeg{pos[it.from], pos[v]}, edgeThr(near[it.from], near[v]), -1, -1) {
			continue
		}
		settled[v], g[v], parent[v] = true, it.g, it.from
		if v == 1 {
			var rev []wrPt
			for k := 1; ; k = parent[k] {
				rev = append(rev, pos[k])
				if k == 0 {
					break
				}
			}
			path := make([]wrPt, len(rev))
			for i, p := range rev {
				path[len(rev)-1-i] = p
			}
			return finish(path), nil
		}
		push(v)
	}
	return waterRoute{}, fmt.Errorf("%w from %.4f, %.4f to %.4f, %.4f within %.0f nm of the straight line: the water between them is closed off by charted shoreline", errNoWaterRoute, from.Lat, from.Lon, to.Lat, to.Lon, marginNm)
}
