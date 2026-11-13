package main

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// waterRouteMargins are the corridor margins tried in turn, in nautical
// miles. A narrow corridor is fast and nearly always enough; a wider one is
// tried only when the narrow one found no path. Each needs its own coverage
// squares, so a route near the edge of the held coastline reports the missing
// square instead of giving a path that might be too long.
var waterRouteMargins = []float64{3, 8, 20, 50}

// waterRouteMaxTime bounds one whole call to RouteOverWater, every margin and
// the retry that proves the answer. The queue bound caps memory but not time,
// and Mate's run has a budget of its own, so a search that outruns this is
// refused as too intricate. A var so a test can shorten it.
var waterRouteMaxTime = 30 * time.Second

// squaresInBox returns the coverage squares a box touches.
func squaresInBox(south, west, north, east float64) ([]coverageSquare, error) {
	if west < -180 || east > 180 {
		return nil, fmt.Errorf("%w: the route area crosses the 180° meridian", errCoastlineUnsupportedArea)
	}
	if math.Abs(south) > coastlineMaxLat || math.Abs(north) > coastlineMaxLat {
		return nil, fmt.Errorf("%w: the route area reaches a pole", errCoastlineUnsupportedArea)
	}
	var out []coverageSquare
	for la := int(math.Floor(south)); la <= int(math.Floor(north)); la++ {
		for lo := int(math.Floor(west)); lo <= int(math.Floor(east)); lo++ {
			out = append(out, coverageSquare{Lat: la, Lon: lo})
		}
	}
	return out, nil
}

// coastlineLoader reads the coverage squares one route needs. It reads the
// records once and each square's lines once, however many margins are tried.
type coastlineLoader struct {
	r      *offlineCoastlineReader
	byKey  map[coverageSquare]coastlineSquareRecord
	lines  map[coverageSquare][]shorelineLine
	loaded bool
}

func (r *offlineCoastlineReader) newLoader() *coastlineLoader {
	return &coastlineLoader{r: r, lines: map[coverageSquare][]shorelineLine{}}
}

// inBox is the shoreline of every square a box touches. Every square must be
// held (an inland square that was never downloaded holds no coastline by
// definition). A square that is not is named in the error: a route over a
// coastline with a hole in it could cross land.
func (l *coastlineLoader) inBox(south, west, north, east float64) ([]shorelineLine, error) {
	r := l.r
	if r == nil || r.Store == nil {
		return nil, fmt.Errorf("Offline coastline is not available on this install")
	}
	enabled, err := r.Enabled()
	if err != nil {
		return nil, fmt.Errorf("could not read the Offline coastline setting: %w", err)
	}
	if !enabled {
		return nil, fmt.Errorf("Offline coastline is off — turn it on in Settings → Offline coastline")
	}
	needed, err := squaresInBox(south, west, north, east)
	if err != nil {
		return nil, err
	}
	if !l.loaded {
		recs, err := r.Store.Records()
		if err != nil {
			return nil, err
		}
		l.byKey = make(map[coverageSquare]coastlineSquareRecord, len(recs))
		for _, rec := range recs {
			l.byKey[rec.Square] = rec
		}
		l.loaded = true
	}
	var missing []string
	for _, sq := range needed {
		rec, held := l.byKey[sq]
		switch {
		case held && rec.Status == coastlineStatusReady:
		case squareInland(sq):
			// no sea within 20 nm, so no coastline to hold
		case held && rec.Status == coastlineStatusFailed:
			missing = append(missing, fmt.Sprintf("%s (download failed: %s)", sq.Label(), rec.LastError))
		default:
			missing = append(missing, fmt.Sprintf("%s (not downloaded)", sq.Label()))
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("the route needs Offline coastline squares that are not held: %s. Helmcentral holds the squares within %.0f nm of the boat and any it downloaded earlier. %s",
			strings.Join(missing, ", "), coastlineCoverageRadiusNm, coastlineDownloadHint)
	}
	var out []shorelineLine
	for _, sq := range needed {
		if rec, held := l.byKey[sq]; !held || rec.Status != coastlineStatusReady {
			continue
		}
		got, ok := l.lines[sq]
		if !ok {
			got, err = r.Store.Lines([]coverageSquare{sq})
			if err != nil {
				return nil, err
			}
			l.lines[sq] = got
		}
		out = append(out, got...)
	}
	return out, nil
}

// RouteOverWater is the shortest path over water between two positions,
// from the coastline held on board. It never returns the straight line in
// place of a route: when the coastline it needs is missing, an end point is
// on land, or no water connects the two, it says so.
//
// The corridor is widened only as far as needed. If proving the path found is
// the shortest needs a wider corridor than is held, the call fails and names
// the missing square. It does not hand back the unproven path: a route that
// may be too long, offered as the distance, is the quiet substitution this
// tool exists to remove, and an explicit caveat would put the burden of
// noticing it on Mate and the operator.
func (r *offlineCoastlineReader) RouteOverWater(ctx context.Context, fromLat, fromLon, toLat, toLon float64) (waterRoute, error) {
	limit := waterRouteMaxTime
	timed, cancel := context.WithTimeoutCause(ctx, limit, errWaterRouteTooIntricate)
	defer cancel()
	route, err := r.routeWithin(timed, fromLat, fromLon, toLat, toLon)
	// Only this limit counts. A cancelled or expired parent keeps its own error.
	if err != nil && ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) && errors.Is(context.Cause(timed), errWaterRouteTooIntricate) {
		return waterRoute{}, fmt.Errorf("the route from %.4f, %.4f to %.4f, %.4f is %w: the search ran past %v; plan it in shorter legs",
			fromLat, fromLon, toLat, toLon, errWaterRouteTooIntricate, limit)
	}
	return route, err
}

func (r *offlineCoastlineReader) routeWithin(ctx context.Context, fromLat, fromLon, toLat, toLon float64) (waterRoute, error) {
	for _, e := range []struct {
		name     string
		lat, lon float64
	}{{"start", fromLat, fromLon}, {"destination", toLat, toLon}} {
		sq, err := squareContaining(e.lat, e.lon)
		if err != nil {
			return waterRoute{}, err
		}
		if squareInland(sq) {
			return waterRoute{}, fmt.Errorf("the %s (%.4f, %.4f) is inland, well clear of the sea", e.name, e.lat, e.lon)
		}
	}
	from, to := shorelinePoint{Lat: fromLat, Lon: fromLon}, shorelinePoint{Lat: toLat, Lon: toLon}
	loader := r.newLoader()

	try := func(margin float64) (waterRoute, error) {
		if err := ctx.Err(); err != nil {
			return waterRoute{}, err
		}
		dLat := margin / 60
		dLon := dLat / math.Cos(math.Max(math.Abs(fromLat), math.Abs(toLat))*math.Pi/180)
		lines, err := loader.inBox(
			math.Min(fromLat, toLat)-dLat, math.Min(fromLon, toLon)-dLon,
			math.Max(fromLat, toLat)+dLat, math.Max(fromLon, toLon)+dLon)
		if err != nil {
			return waterRoute{}, err
		}
		return routeOverWater(ctx, from, to, lines, margin)
	}

	var lastErr error
	for _, m := range waterRouteMargins {
		route, err := try(m)
		if errors.Is(err, errNoWaterRoute) {
			lastErr = err
			continue
		}
		if err != nil {
			return waterRoute{}, err
		}
		if route.ExactWithinNm > m {
			// A shorter path could lie outside this corridor: search the
			// corridor the path found allows, which settles it.
			return try(route.ExactWithinNm + 0.1)
		}
		return route, nil
	}
	return waterRoute{}, lastErr
}

// ── a passage through several points ──────────────────────────────────────

// segmentRouter routes one point-to-point segment over water.
type segmentRouter func(ctx context.Context, from, to [2]float64) (waterRoute, error)

// routedPassageLeg is one straight run of a routed passage.
type routedPassageLeg struct {
	BearingDeg int
	Nm         float64
}

// routedPassageMove is an input point charted behind the shoreline and how far
// it was moved to the water. Point indexes the points the passage was routed
// through; a via shared by two segments can appear twice.
type routedPassageMove struct {
	Point int
	Nm    float64
}

// routedPassage is a run over water through a list of points, routed segment
// by segment. route_distance, estimate_passage and the plan legs all read it.
type routedPassage struct {
	// Points runs from the first point to the last, a bend at each headland.
	Points         []shorelinePoint
	DistanceNm     float64
	StraightLineNm float64
	// CourseDeg is the bearing of the longest straight run, the one course for
	// wind angle and tide. Legs has every run.
	CourseDeg int
	Legs      []routedPassageLeg
	// Notes says, per segment, which ends were charted behind the shoreline.
	Notes []string
	Moved []routedPassageMove
}

// routedPassageError is the segment that could not be routed (0 is the first)
// and why. Its text is the router's, unchanged.
type routedPassageError struct {
	Segment int
	Err     error
}

func (e *routedPassageError) Error() string { return e.Err.Error() }
func (e *routedPassageError) Unwrap() error { return e.Err }

// routePassageThrough routes each segment between consecutive points with route
// and joins them. A segment that fails fails the whole passage with a
// *routedPassageError; a straight line is never put in its place.
func routePassageThrough(ctx context.Context, route segmentRouter, points [][2]float64) (routedPassage, error) {
	var out routedPassage
	for k := 0; k+1 < len(points); k++ {
		r, err := route(ctx, points[k], points[k+1])
		if err != nil {
			return routedPassage{}, &routedPassageError{Segment: k, Err: err}
		}
		out.DistanceNm += r.DistanceNm
		out.StraightLineNm += r.StraightLineNm
		start := 0
		if k > 0 {
			start = 1 // the joint is the previous segment's last point
		}
		out.Points = append(out.Points, r.Points[start:]...)
		if r.StartMovedNm > 0 {
			out.Moved = append(out.Moved, routedPassageMove{Point: k, Nm: r.StartMovedNm})
		}
		if r.EndMovedNm > 0 {
			out.Moved = append(out.Moved, routedPassageMove{Point: k + 1, Nm: r.EndMovedNm})
		}
		if n := routeMovedNote(r); n != "" {
			out.Notes = append(out.Notes, n)
		}
	}
	longest := -1.0
	for i := 1; i < len(out.Points); i++ {
		a, b := out.Points[i-1], out.Points[i]
		nm := haversineMeters(a.Lat, a.Lon, b.Lat, b.Lon) / metersPerNauticalMile
		brg := int(math.Round(bearingDeg(a.Lat, a.Lon, b.Lat, b.Lon))) % 360
		out.Legs = append(out.Legs, routedPassageLeg{BearingDeg: brg, Nm: nm})
		if nm > longest {
			longest, out.CourseDeg = nm, brg
		}
	}
	return out, nil
}

// ── memoised segments ─────────────────────────────────────────────────────

// waterRouteFailureTTL is how long a failed segment is remembered, so a page
// refetched every few seconds does not repeat a search that just failed.
var waterRouteFailureTTL = 30 * time.Second

const waterRouteCacheMax = 256

// waterRouteKey names a segment on one state of the coastline: the stamp
// changes when a square is downloaded, replaced or removed.
type waterRouteKey struct {
	pts   [4]float64
	stamp string
}

func (k waterRouteKey) String() string { return fmt.Sprintf("%s|%v", k.stamp, k.pts) }

type waterRouteEntry struct {
	route   waterRoute
	err     error
	at      time.Time
	expires time.Time // zero for a success: its stamp keeps it honest
}

// waterRouteCache remembers routed segments, and runs identical concurrent
// searches once.
type waterRouteCache struct {
	mu      sync.Mutex
	now     func() time.Time
	entries map[waterRouteKey]waterRouteEntry
	flight  singleflight.Group
}

func newWaterRouteCache() *waterRouteCache { return &waterRouteCache{now: time.Now} }

func (c *waterRouteCache) lookup(key waterRouteKey) (waterRouteEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if ok && !e.expires.IsZero() && !c.now().Before(e.expires) {
		delete(c.entries, key)
		return waterRouteEntry{}, false
	}
	return e, ok
}

func (c *waterRouteCache) store(key waterRouteKey, route waterRoute, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[waterRouteKey]waterRouteEntry{}
	}
	if _, held := c.entries[key]; !held && len(c.entries) >= waterRouteCacheMax {
		c.evictOneLocked()
	}
	now := c.now()
	e := waterRouteEntry{route: route, err: err, at: now}
	if err != nil {
		e.expires = now.Add(waterRouteFailureTTL)
	}
	c.entries[key] = e
}

// evictOneLocked drops the oldest entry, so one new segment costs one old one
// and not the whole cache.
func (c *waterRouteCache) evictOneLocked() {
	var oldest waterRouteKey
	var oldestAt time.Time
	first := true
	for k, e := range c.entries {
		if first || e.at.Before(oldestAt) {
			oldest, oldestAt, first = k, e.at, false
		}
	}
	delete(c.entries, oldest)
}

type waterRouteResult struct {
	route waterRoute
	err   error
}

// route serves key from the cache or finds it. A result is remembered only
// while ctx is alive: a failure caused by this request running out of time says
// nothing about the segment.
func (c *waterRouteCache) route(ctx context.Context, key waterRouteKey, find func() (waterRoute, error)) (waterRoute, error) {
	if e, ok := c.lookup(key); ok {
		return e.route, e.err
	}
	run := func() (waterRouteResult, bool) {
		v, _, shared := c.flight.Do(key.String(), func() (any, error) {
			r, err := find()
			if ctx.Err() == nil {
				c.store(key, r, err)
			}
			return waterRouteResult{route: r, err: err}, nil
		})
		return v.(waterRouteResult), shared
	}
	res, shared := run()
	// A search shared with another request ends with that request's context.
	if shared && res.err != nil && ctx.Err() == nil &&
		(errors.Is(res.err, context.Canceled) || errors.Is(res.err, context.DeadlineExceeded)) {
		res.route, res.err = find()
		if ctx.Err() == nil {
			c.store(key, res.route, res.err)
		}
	}
	return res.route, res.err
}

// coverageStamp identifies the squares held and ready right now.
func (r *offlineCoastlineReader) coverageStamp() (string, error) {
	recs, err := r.Store.Records()
	if err != nil {
		return "", fmt.Errorf("could not read the Offline coastline squares: %w", err)
	}
	h := fnv.New64a()
	for _, rec := range recs {
		if rec.Status == coastlineStatusReady {
			fmt.Fprintf(h, "%v,%v,%d,%d;", rec.Square.Lat, rec.Square.Lon, rec.DownloadedAt.Unix(), rec.DataAsOf.Unix())
		}
	}
	return strconv.FormatUint(h.Sum64(), 16), nil
}

// memoisedRouter checks the install, the switch and the squares once, then
// returns a router whose results are remembered in memo for the coastline as it
// is now: turning the coastline off ends them, a changed set of squares starts
// afresh, and a failure is kept only briefly.
func (r *offlineCoastlineReader) memoisedRouter(memo *waterRouteCache) (segmentRouter, error) {
	if r == nil || r.Store == nil {
		return nil, errors.New("Offline coastline is not available on this install, so the run cannot be routed over water")
	}
	enabled, err := r.Enabled()
	if err != nil {
		return nil, fmt.Errorf("could not read the Offline coastline setting: %w", err)
	}
	if !enabled {
		return nil, errors.New("Offline coastline is off — turn it on in Settings → Offline coastline")
	}
	stamp, err := r.coverageStamp()
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, from, to [2]float64) (waterRoute, error) {
		key := waterRouteKey{pts: [4]float64{from[0], from[1], to[0], to[1]}, stamp: stamp}
		return memo.route(ctx, key, func() (waterRoute, error) {
			return r.RouteOverWater(ctx, from[0], from[1], to[0], to[1])
		})
	}, nil
}
