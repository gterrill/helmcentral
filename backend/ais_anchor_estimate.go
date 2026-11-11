package main

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/labstack/echo/v4"
)

// Where nearby boats have laid their anchors (ADR 0275). A boat at anchor stays
// on a circle round its anchor, so a circle fitted to the positions Helmcentral
// already stores for it (ais_anchor_track.go) estimates the anchor and the
// boat's swing radius. Positions only: no heading, no antenna dimensions. An
// estimate is offered only when the track looks like a circle; a target that
// does not simply has none.

const (
	// aisAnchorFitMinPoints: fewer stored points cannot tell a circle from a
	// wander. A boat at anchor reports about every 3 minutes (Class B slower), so
	// 20 stored points can take about an hour to arrive.
	aisAnchorFitMinPoints = 20
	// aisAnchorFitMinArcDeg is the bearing span the ring points must cover round
	// the fitted centre. A boat that has only swung a little can sit on many
	// circles; a quarter turn pins the centre to a few metres. A boat with a small
	// swing gets a small circle, which is accepted so that circles appear sooner.
	aisAnchorFitMinArcDeg = 90.0
	// aisAnchorFitMinRadiusM and aisAnchorFitMaxRadiusM bound a believable swing.
	// Under 5 m is GNSS jitter on a boat that is not swinging at all. Over 80 m is
	// more than a boat lays out: about 5:1 scope in 12 m of water, plus the boat's
	// length. A wandering boat or a straight transit fits a bigger circle than that.
	aisAnchorFitMinRadiusM = 5.0
	aisAnchorFitMaxRadiusM = 80.0
	// aisAnchorSettledRadiusM caps how far a point may lie from the middle of the
	// run it belongs to. A boat on its anchor never strays more than its swing
	// diameter from the middle of its swing, so 1.5 times the largest believable
	// radius is room to spare. A boat that has left has its newest points away
	// from the rest, so its run collapses short of aisAnchorFitMinPoints and it
	// gets no estimate, which is right.
	aisAnchorSettledRadiusM = 1.5 * aisAnchorFitMaxRadiusM
	// aisAnchorFitMaxResidualM and aisAnchorFitMaxResidualFrac bound the RMS
	// distance of the ring points from the fitted circle: the larger of the two,
	// so a small circle is not failed for GNSS noise alone.
	aisAnchorFitMaxResidualM    = 5.0
	aisAnchorFitMaxResidualFrac = 0.25
	// aisAnchorFitRingFrac keeps the points at least this fraction of the 90th
	// percentile distance from the centre. In light air a boat sits inside its
	// circle on slack rode; only the taut stretch lies on it, so only the outer
	// ring is fitted.
	aisAnchorFitRingFrac = 0.8
	// aisAnchorFitRingPercentile is the distance percentile the ring is cut from.
	aisAnchorFitRingPercentile = 0.9
	// aisAnchorFitMinRingPoints: a ring of fewer points is not a fit.
	aisAnchorFitMinRingPoints = 10
	// aisAnchorFitPasses is how many times the ring is cut and refitted. Each pass
	// moves the centre less; three settle it to well under a metre.
	aisAnchorFitPasses = 3
	// aisAnchorMaxMarginRatio caps our ratio of room. A small own fit in light air
	// (our boat barely swinging on a big watch radius) would otherwise draw every
	// neighbour several times its swing and paint them all red.
	aisAnchorMaxMarginRatio = 2.0
	// ownTrailSpacing thins our own swing trail before it is fitted: one point per
	// 30 seconds, which is the AIS recorder's own floor, so the two swings are fitted
	// from alike data and the settled run stays cheap.
	ownTrailSpacing = 30 * time.Second
	// aisAnchorOwnTrailKeyStep is how finely our own trail's newest point time
	// enters the cache key. Our trail grows every few seconds, and a ratio that
	// moves with each point is not worth a refit each time: at most about once a
	// minute.
	aisAnchorOwnTrailKeyStep = time.Minute
)

// aisAnchorEstimate is one target's estimated anchor and swing circle, as the
// endpoint serves it. The fit fills everything but MMSI and OverlapsOurs.
type aisAnchorEstimate struct {
	MMSI         string  `json:"mmsi"`
	Lat          float64 `json:"lat"`
	Lon          float64 `json:"lon"`
	SwingRadiusM float64 `json:"swing_radius_m"`
	// DrawnRadiusM is SwingRadiusM times our margin ratio: the radius the map draws.
	DrawnRadiusM float64 `json:"drawn_radius_m"`
	SwingArcDeg  float64 `json:"swing_arc_deg"`
	Points       int     `json:"points"`
	OverlapsOurs bool    `json:"overlaps_ours"`
}

// kasaCircle is the algebraic least-squares circle through the points (Kåsa):
// it minimises the sum of (x²+y²+Dx+Ey+F)², which is linear in D, E and F. It
// is not exact on a short arc, but it never needs a starting guess. ok is false
// when the points are collinear.
func kasaCircle(xs, ys []float64) (cx, cy, r float64, ok bool) {
	n := float64(len(xs))
	if len(xs) < 3 {
		return 0, 0, 0, false
	}
	var mx, my float64
	for i := range xs {
		mx += xs[i]
		my += ys[i]
	}
	mx /= n
	my /= n
	var suu, suv, svv, suuu, svvv, suvv, svuu float64
	for i := range xs {
		u, v := xs[i]-mx, ys[i]-my
		suu += u * u
		suv += u * v
		svv += v * v
		suuu += u * u * u
		svvv += v * v * v
		suvv += u * v * v
		svuu += v * u * u
	}
	det := suu*svv - suv*suv
	if det <= 1e-9*(suu+svv)*(suu+svv) {
		return 0, 0, 0, false
	}
	b1 := 0.5 * (suuu + suvv)
	b2 := 0.5 * (svvv + svuu)
	a := (b1*svv - b2*suv) / det
	b := (suu*b2 - suv*b1) / det
	return mx + a, my + b, math.Sqrt(a*a + b*b + (suu+svv)/n), true
}

// aisRingPoints is the points at least aisAnchorFitRingFrac times the
// aisAnchorFitRingPercentile distance from (cx, cy), with their distances.
func aisRingPoints(xs, ys []float64, cx, cy float64) (rx, ry, dist []float64) {
	d := make([]float64, len(xs))
	for i := range xs {
		d[i] = math.Hypot(xs[i]-cx, ys[i]-cy)
	}
	sorted := append([]float64(nil), d...)
	sort.Float64s(sorted)
	cut := aisAnchorFitRingFrac * sorted[int(aisAnchorFitRingPercentile*float64(len(sorted)-1))]
	for i := range d {
		if d[i] >= cut {
			rx = append(rx, xs[i])
			ry = append(ry, ys[i])
			dist = append(dist, d[i])
		}
	}
	return rx, ry, dist
}

// fitAISAnchor fits a swing circle to one target's points, projected to local
// metres about (refLat, refLon): a Kåsa circle through all of them, then
// aisAnchorFitPasses refits to the outer ring alone. ok is false when the points
// do not make a trustworthy circle: too few, too short an arc, an implausible
// radius, or a ring that is not round.
func fitAISAnchor(points []aisTrackPointWire, refLat, refLon float64) (aisAnchorEstimate, bool) {
	if len(points) < aisAnchorFitMinPoints {
		return aisAnchorEstimate{}, false
	}
	xs := make([]float64, len(points))
	ys := make([]float64, len(points))
	for i, p := range points {
		xs[i], ys[i] = localENMeters(refLat, refLon, p.Lat, p.Lon)
	}
	cx, cy, _, ok := kasaCircle(xs, ys)
	if !ok {
		return aisAnchorEstimate{}, false
	}
	for i := 0; i < aisAnchorFitPasses; i++ {
		rx, ry, _ := aisRingPoints(xs, ys, cx, cy)
		if len(rx) < aisAnchorFitMinRingPoints {
			return aisAnchorEstimate{}, false
		}
		if cx, cy, _, ok = kasaCircle(rx, ry); !ok {
			return aisAnchorEstimate{}, false
		}
	}
	// Judge the circle by the ring cut from its own final centre.
	rx, ry, dist := aisRingPoints(xs, ys, cx, cy)
	if len(rx) < aisAnchorFitMinRingPoints {
		return aisAnchorEstimate{}, false
	}
	var radius float64
	for _, d := range dist {
		radius += d
	}
	radius /= float64(len(dist))
	if radius < aisAnchorFitMinRadiusM || radius > aisAnchorFitMaxRadiusM {
		return aisAnchorEstimate{}, false
	}
	var sq float64
	for _, d := range dist {
		sq += (d - radius) * (d - radius)
	}
	if math.Sqrt(sq/float64(len(dist))) > math.Max(aisAnchorFitMaxResidualM, aisAnchorFitMaxResidualFrac*radius) {
		return aisAnchorEstimate{}, false
	}
	bearings := make([]float64, len(rx))
	for i := range rx {
		bearings[i] = math.Atan2(ry[i]-cy, rx[i]-cx) * 180 / math.Pi
	}
	sort.Float64s(bearings)
	gap := bearings[0] + 360 - bearings[len(bearings)-1]
	for i := 1; i < len(bearings); i++ {
		gap = math.Max(gap, bearings[i]-bearings[i-1])
	}
	arc := 360 - gap
	if arc < aisAnchorFitMinArcDeg {
		return aisAnchorEstimate{}, false
	}
	return aisAnchorEstimate{
		Lat:          refLat + cy/metersPerDegreeLat,
		Lon:          refLon + cx/(metersPerDegreeLat*math.Cos(refLat*math.Pi/180)),
		SwingRadiusM: radius,
		SwingArcDeg:  arc,
		Points:       len(points),
	}, true
}

// insertSorted puts v into the ascending slice s, keeping it ascending.
func insertSorted(s []float64, v float64) []float64 {
	i := sort.SearchFloat64s(s, v)
	s = append(s, 0)
	copy(s[i+1:], s[i:])
	s[i] = v
	return s
}

// medianSorted is the median of an ascending slice.
func medianSorted(s []float64) float64 {
	n := len(s)
	return (s[(n-1)/2] + s[n/2]) / 2
}

// settledAISRun is the newest stretch of a target's track that sits together:
// walking back from the newest point, a point stays in the run while it lies
// within aisAnchorSettledRadiusM of the median position (x and y medians
// separately, in metres about the ref point) of the points kept so far including
// it. The first point that does not ends the run. It drops the approach, and
// ignores new_segment flags and reporting gaps: a boat that went quiet without
// moving is still one swing. Points are in storage order and come back in it.
func settledAISRun(points []aisTrackPointWire, refLat, refLon float64) []aisTrackPointWire {
	var xs, ys []float64
	for i := len(points) - 1; i >= 0; i-- {
		x, y := localENMeters(refLat, refLon, points[i].Lat, points[i].Lon)
		xs, ys = insertSorted(xs, x), insertSorted(ys, y)
		if math.Hypot(x-medianSorted(xs), y-medianSorted(ys)) > aisAnchorSettledRadiusM {
			return points[i+1:]
		}
	}
	return points
}

// ownSwingRatio is the proportion of room we have left round our own swing: the
// watch radius over the radius of the circle our own trail fits, never under 1
// (it never shrinks their circle). The same settled run and fit as for any other
// boat, on our swing trail since the watch was set. Fit 40 m and watch radius
// 50 m give 1.25. ok is false while our trail does not pass the gates yet: there
// is no ratio, and no AIS circle is offered.
func ownSwingRatio(watch anchorWatchData, trail []aisTrackPointWire) (ratio float64, ok bool) {
	run := settledAISRun(trail, watch.Lat, watch.Lon)
	if len(run) < aisAnchorFitMinPoints {
		return 0, false
	}
	own, ok := fitAISAnchor(run, watch.Lat, watch.Lon)
	if !ok {
		return 0, false
	}
	return math.Min(aisAnchorMaxMarginRatio, math.Max(1, watch.RadiusMeters/own.SwingRadiusM)), true
}

// estimateAISAnchors fits each target's settled run and keeps the ones
// that pass, in MMSI order. Each is drawn at its fitted swing times ratio, our
// own ratio of room, so a boat on less rode gets less room. Ours is the watch's
// own circle, as the map draws it: the two overlap when their centres are closer
// than our radius plus their drawn radius.
func estimateAISAnchors(watch anchorWatchData, tracks map[string][]aisTrackPointWire, ratio float64) []aisAnchorEstimate {
	out := []aisAnchorEstimate{}
	for mmsi, points := range tracks {
		run := settledAISRun(points, watch.Lat, watch.Lon)
		if len(run) < aisAnchorFitMinPoints {
			continue
		}
		est, ok := fitAISAnchor(run, watch.Lat, watch.Lon)
		if !ok {
			continue
		}
		est.MMSI = mmsi
		est.DrawnRadiusM = est.SwingRadiusM * ratio
		est.OverlapsOurs = haversineMeters(watch.Lat, watch.Lon, est.Lat, est.Lon) < watch.RadiusMeters+est.DrawnRadiusM
		out = append(out, est)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MMSI < out[j].MMSI })
	return out
}

// aisAnchorTracksSinceWatch is every stored point recorded since the current
// watch started, grouped by target in storage order. The estimator reads this
// rather than the 24 hours the trails serve: a boat's whole time at anchor
// counts towards its swing.
func (s *passageStore) aisAnchorTracksSinceWatch() (map[string][]aisTrackPointWire, error) {
	out := map[string][]aisTrackPointWire{}
	started, err := s.anchorTrailStarted()
	if err != nil {
		return nil, err
	}
	if started.IsZero() {
		return out, nil
	}
	rows, err := s.db.Query(`SELECT mmsi, t_ms, lat, lon, new_segment FROM ais_anchor_track_points
		WHERE t_ms >= ? ORDER BY seq`, started.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("query ais anchor tracks since the watch: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var mmsi string
		var seg int
		p := aisTrackPointWire{}
		if err := rows.Scan(&mmsi, &p.TMs, &p.Lat, &p.Lon, &seg); err != nil {
			return nil, fmt.Errorf("scan ais anchor tracks since the watch: %w", err)
		}
		p.NewSegment = seg == 1
		out[mmsi] = append(out[mmsi], p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query ais anchor tracks since the watch: %w", err)
	}
	return out, nil
}

// anchorTrailThinned is our own swing trail for the whole watch, oldest first,
// one point per spacing at most. The trail endpoint serves the newest
// anchorTrailMaxPoints; this read has no such cap, so both swings in the ratio
// cover the same span, and thinning keeps the fit's work small: the store holds
// a point per metre moved or per minute, which is thousands over a long watch.
func (s *passageStore) anchorTrailThinned(spacing time.Duration) ([]*trailPoint, error) {
	started, err := s.anchorTrailStarted()
	if err != nil || started.IsZero() {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT t_ms, lat, lon FROM anchor_trail_points WHERE t_ms >= ? ORDER BY t_ms`, started.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("query anchor trail: %w", err)
	}
	defer rows.Close()
	var out []*trailPoint
	var lastMs int64
	for rows.Next() {
		var ms int64
		p := &trailPoint{}
		if err := rows.Scan(&ms, &p.Lat, &p.Lon); err != nil {
			return nil, fmt.Errorf("scan anchor trail: %w", err)
		}
		if len(out) > 0 && ms-lastMs < spacing.Milliseconds() {
			continue
		}
		p.Timestamp = time.UnixMilli(ms).UTC()
		out = append(out, p)
		lastMs = ms
	}
	return out, rows.Err()
}

// aisAnchorNewestMs is the newest stored point's time in Unix ms, 0 for an empty
// table. The cache key takes it at a coarse step (aisAnchorOwnTrailKeyStep): a
// point is stored per target every ~30 s, and a refit per point would run the
// whole O(n^2) fit on every poll.
func (s *passageStore) aisAnchorNewestMs() (int64, error) {
	var ms int64
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(t_ms), 0) FROM ais_anchor_track_points`).Scan(&ms); err != nil {
		return 0, fmt.Errorf("read newest ais anchor track point: %w", err)
	}
	return ms, nil
}

// anchorTrailStamp is our own swing trail's start and newest point time in Unix
// ms (0 for none): what changes when the trail grows or restarts.
func (s *passageStore) anchorTrailStamp() (startedMs, newestMs int64, err error) {
	started, err := s.anchorTrailStarted()
	if err != nil || started.IsZero() {
		return 0, 0, err
	}
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(t_ms), 0) FROM anchor_trail_points WHERE t_ms >= ?`, started.UnixMilli()).Scan(&newestMs); err != nil {
		return 0, 0, fmt.Errorf("read newest anchor trail point: %w", err)
	}
	return started.UnixMilli(), newestMs, nil
}

// aisAnchorEstimateKey is what the handler's answer depends on: the stored AIS
// points, the watch, and our own swing trail (it sets the ratio). Nothing ages
// out while the watch runs, so no clock is in it. The newest times are taken at
// a coarse step, so a fit is redone about once a minute, not per stored point.
// A table emptied at a drop or raise reads 0, and trailStartedMs changes then.
type aisAnchorEstimateKey struct {
	newestAISMs            int64
	lat, lon, radiusMeters float64
	trailStartedMs         int64
	trailNewestMs          int64
}

// aisAnchorEstimateCache holds the last answer. The mutex is held across the
// recompute, so concurrent requests on a changed key fit once, not once each.
var aisAnchorEstimateCache struct {
	mu        sync.Mutex
	valid     bool
	key       aisAnchorEstimateKey
	estimates []aisAnchorEstimate
	ratio     float64
}

func resetAISAnchorEstimateCache() {
	aisAnchorEstimateCache.mu.Lock()
	aisAnchorEstimateCache.valid = false
	aisAnchorEstimateCache.estimates = nil
	aisAnchorEstimateCache.mu.Unlock()
}

// aisAnchorEstimateComputes counts the times the handler read and fitted the
// tracks, so a test can see a cached answer was served.
var aisAnchorEstimateComputes atomic.Int64

// GET /api/anchor-watch/ais-anchors
// Returns the estimated anchor and swing circle of each nearby target whose
// recorded track fits one, and whether that circle overlaps the watch's own.
// Each carries swing_radius_m (the fit) and drawn_radius_m (the fit times our
// margin_ratio, once at the top level). Empty with no watch, and empty until our
// own swing trail fits a circle: without our ratio the drawn circles would be
// wrong, so none is offered. The fit reads every point recorded since the watch
// started and is cached until the newest point moves to a new minute, the watch changes or our own
// trail grows. Only targets the recorder last saw inside the 500 m circle are
// served (insideAISEstimates). Errors are never cached.
func getAISAnchorsHandler(c echo.Context) error {
	watch, active, loadErr := currentAnchorWatch()
	if !active {
		// A watch file that failed to load is not "no watch": say so.
		if loadErr != "" {
			log.Printf("ais anchor estimates: anchor watch unreadable: %s", loadErr)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "AIS anchor estimates unavailable: " + loadErr})
		}
		return c.JSON(http.StatusOK, map[string]any{"estimates": []aisAnchorEstimate{}, "margin_ratio": 1})
	}
	store := globalPassageStore
	if store == nil {
		log.Printf("ais anchor estimates: %v", errAnchorTrailNoStore)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "AIS anchor estimates unavailable: track store is not open"})
	}

	cache := &aisAnchorEstimateCache
	cache.mu.Lock()
	defer cache.mu.Unlock()
	newestAISMs, err := store.aisAnchorNewestMs()
	if err != nil {
		log.Printf("ais anchor estimates: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "AIS anchor estimates unavailable: could not read the tracks"})
	}
	trailStartedMs, trailNewestMs, err := store.anchorTrailStamp()
	if err != nil {
		log.Printf("ais anchor estimates: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "AIS anchor estimates unavailable: could not read our own trail"})
	}
	key := aisAnchorEstimateKey{
		newestAISMs: time.UnixMilli(newestAISMs).Truncate(aisAnchorOwnTrailKeyStep).UnixMilli(), lat: watch.Lat, lon: watch.Lon, radiusMeters: watch.RadiusMeters,
		trailStartedMs: trailStartedMs,
		trailNewestMs:  time.UnixMilli(trailNewestMs).Truncate(aisAnchorOwnTrailKeyStep).UnixMilli(),
	}
	if cache.valid && cache.key == key {
		return c.JSON(http.StatusOK, map[string]any{"estimates": insideAISEstimates(cache.estimates), "margin_ratio": cache.ratio})
	}

	aisAnchorEstimateComputes.Add(1)
	// Everything recorded since the watch started, grouped by target in storage order.
	tracks, err := store.aisAnchorTracksSinceWatch()
	if err != nil {
		log.Printf("ais anchor estimates: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "AIS anchor estimates unavailable: could not read the tracks"})
	}
	own, err := store.anchorTrailThinned(ownTrailSpacing)
	if err != nil {
		log.Printf("ais anchor estimates: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "AIS anchor estimates unavailable: could not read our own trail"})
	}
	ownPoints := make([]aisTrackPointWire, len(own))
	for i, p := range own {
		ownPoints[i] = aisTrackPointWire{Lat: p.Lat, Lon: p.Lon, TMs: p.Timestamp.UnixMilli()}
	}
	ratio, ready := ownSwingRatio(watch, ownPoints)
	cache.estimates = []aisAnchorEstimate{}
	cache.ratio = 1
	if ready {
		cache.ratio = ratio
		cache.estimates = estimateAISAnchors(watch, tracks, ratio)
	}
	cache.key, cache.valid = key, true
	return c.JSON(http.StatusOK, map[string]any{"estimates": insideAISEstimates(cache.estimates), "margin_ratio": cache.ratio})
}

// insideAISEstimates keeps the estimates of the targets the recorder last saw
// inside the 500 m recording circle (aisTrackState.inside). A boat that has gone
// has a track that stops at the circle's edge, and its last settled stretch can
// still fit a circle, so it is not offered. This runs after the cache, so the
// cache key needs no inside set. After a restart inside is empty until the next
// poll tick, and no estimate shows until then.
func insideAISEstimates(all []aisAnchorEstimate) []aisAnchorEstimate {
	aisTracks.mu.Lock()
	defer aisTracks.mu.Unlock()
	out := []aisAnchorEstimate{}
	for _, e := range all {
		if aisTracks.inside[e.MMSI] {
			out = append(out, e)
		}
	}
	return out
}
