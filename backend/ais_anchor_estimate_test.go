package main

import (
	"encoding/json"
	"math"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// A target's swing circle is centred a little north-east of the test anchor, so
// the projection is exercised off the origin.
const (
	estCentreLat = aisTestAnchorLat + 0.0003
	estCentreLon = aisTestAnchorLon + 0.0002
)

// estPoint is the point at bearing (degrees from north) and range from a centre.
func estPoint(centreLat, centreLon, bearing, rangeM float64) aisTrackPointWire {
	const mPerDeg = math.Pi * 6371000.0 / 180
	b := bearing * math.Pi / 180
	return aisTrackPointWire{
		Lat: centreLat + rangeM*math.Cos(b)/mPerDeg,
		Lon: centreLon + rangeM*math.Sin(b)/(mPerDeg*math.Cos(centreLat*math.Pi/180)),
	}
}

// estArc is n points along an arc of arcDeg starting at startDeg, at radius
// radiusM with uniform noise of +-noiseM on the range.
func estArc(rng *rand.Rand, n int, startDeg, arcDeg, radiusM, noiseM float64) []aisTrackPointWire {
	pts := make([]aisTrackPointWire, n)
	for i := range pts {
		bearing := startDeg + arcDeg*float64(i)/float64(n-1)
		pts[i] = estPoint(estCentreLat, estCentreLon, bearing, radiusM+(rng.Float64()*2-1)*noiseM)
	}
	return pts
}

func TestFitAISAnchorRecoversCentreFromA120DegreeArc(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	pts := estArc(rng, 60, 200, 120, 40, 3)
	fit, ok := fitAISAnchor(pts, aisTestAnchorLat, aisTestAnchorLon)
	if !ok {
		t.Fatal("a 120 degree arc at 40 m must give an estimate")
	}
	if d := haversineMeters(fit.Lat, fit.Lon, estCentreLat, estCentreLon); d > 6 {
		t.Fatalf("centre off by %.1f m: %+v", d, fit)
	}
	if math.Abs(fit.SwingRadiusM-40) > 6 {
		t.Fatalf("radius %.1f m, want about 40", fit.SwingRadiusM)
	}
	if fit.SwingArcDeg < 100 || fit.SwingArcDeg > 125 {
		t.Fatalf("arc %.0f degrees, want about 120", fit.SwingArcDeg)
	}
	if fit.Points != 60 {
		t.Fatalf("points %d, want 60", fit.Points)
	}
}

func TestFitAISAnchorUsesTheOuterRingWhenTheBoatSitsInsideItsCircle(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	// Slack rode: a cloud of points inside the circle, plus the taut ring at 40 m.
	var pts []aisTrackPointWire
	for i := 0; i < 50; i++ {
		pts = append(pts, estPoint(estCentreLat, estCentreLon, rng.Float64()*360, 28*math.Sqrt(rng.Float64())))
	}
	pts = append(pts, estArc(rng, 50, 100, 220, 40, 2)...)
	rng.Shuffle(len(pts), func(i, j int) { pts[i], pts[j] = pts[j], pts[i] })
	fit, ok := fitAISAnchor(pts, aisTestAnchorLat, aisTestAnchorLon)
	if !ok {
		t.Fatal("a disc with an outer ring must give an estimate")
	}
	if math.Abs(fit.SwingRadiusM-40) > 5 {
		t.Fatalf("radius %.1f m, want the ring's 40", fit.SwingRadiusM)
	}
	if d := haversineMeters(fit.Lat, fit.Lon, estCentreLat, estCentreLon); d > 6 {
		t.Fatalf("centre off by %.1f m: %+v", d, fit)
	}
}

// estTransit is points on a straight east-going line, 10 m apart, with noise in latitude.
func estTransit(rng *rand.Rand, n int, noiseM float64) []aisTrackPointWire {
	pts := make([]aisTrackPointWire, n)
	for i := range pts {
		p := estPoint(aisTestAnchorLat, aisTestAnchorLon, 90, float64(i)*10-200)
		p.Lat += (rng.Float64()*2 - 1) * noiseM / 111000
		pts[i] = p
	}
	return pts
}

func TestFitAISAnchorRejectsTracksThatAreNotATrustworthyCircle(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	tests := []struct {
		name string
		pts  []aisTrackPointWire
	}{
		{"a 40 degree arc", estArc(rng, 60, 200, 40, 40, 1)}, {"a clean transit", estTransit(rng, 40, 0)},
		{"a transit with 1 m noise", estTransit(rng, 40, 1)},
		{"a transit with 3 m noise", estTransit(rng, 40, 3)},
		{"too few points", estArc(rng, aisAnchorFitMinPoints-1, 0, 270, 40, 1)},
		{"a 2 m circle", estArc(rng, 60, 0, 300, 2, 0.2)},
		{"a 100 m circle", estArc(rng, 60, 0, 300, 100, 0.2)},
		{"a 300 m circle", estArc(rng, 60, 0, 300, 300, 0.2)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if fit, ok := fitAISAnchor(tc.pts, aisTestAnchorLat, aisTestAnchorLon); ok {
				t.Fatalf("must not give an estimate: %+v", fit)
			}
		})
	}
	// The same arc with the least points allowed is accepted, so the gate is the count.
	if _, ok := fitAISAnchor(estArc(rng, aisAnchorFitMinPoints, 0, 270, 40, 1), aisTestAnchorLat, aisTestAnchorLon); !ok {
		t.Fatalf("%d clean points must give an estimate", aisAnchorFitMinPoints)
	}
}

// stampAISPoints gives the points times stepMs apart from firstMs and returns them.
func stampAISPoints(pts []aisTrackPointWire, firstMs, stepMs int64) []aisTrackPointWire {
	for i := range pts {
		pts[i].TMs = firstMs + int64(i)*stepMs
	}
	return pts
}

func TestSettledAISRunDropsTheApproach(t *testing.T) {
	rng := rand.New(rand.NewSource(14))
	var pts []aisTrackPointWire
	for k := 0; k < 8; k++ {
		pts = append(pts, estPoint(estCentreLat, estCentreLon, 90, 400-30*float64(k)))
	}
	pts = append(pts, estArc(rng, 25, 200, 120, 40, 2)...)
	got := settledAISRun(pts, aisTestAnchorLat, aisTestAnchorLon)
	if len(got) != 25 {
		t.Fatalf("want the 25 swing points, got %d", len(got))
	}
	if got[0] != pts[8] {
		t.Fatal("the run keeps the points in their original order, from the first swing point")
	}
}

func TestSettledAISRunIgnoresSegmentFlagsAndReportingGaps(t *testing.T) {
	rng := rand.New(rand.NewSource(15))
	// A boat that stopped reporting for an hour and never moved: the recorder
	// flags a new segment at the gap, but it is one swing.
	pts := stampAISPoints(estArc(rng, 40, 100, 200, 40, 1), 0, 60_000)
	for i := 20; i < len(pts); i++ {
		pts[i].TMs += 3_600_000
	}
	pts[20].NewSegment = true
	if got := settledAISRun(pts, aisTestAnchorLat, aisTestAnchorLon); len(got) != 40 {
		t.Fatalf("a gap with no move keeps the whole run, got %d of 40", len(got))
	}
	est := estimateAISAnchors(anchorWatchData{Lat: aisTestAnchorLat, Lon: aisTestAnchorLon, RadiusMeters: 20},
		map[string][]aisTrackPointWire{"111111111": pts}, 1)
	if len(est) != 1 || est[0].Points != 40 {
		t.Fatalf("want one estimate from all 40 points, got %+v", est)
	}
}

func TestSettledAISRunOfADepartedBoatIsShortAndGivesNoEstimate(t *testing.T) {
	rng := rand.New(rand.NewSource(16))
	pts := estArc(rng, 30, 100, 200, 40, 1)
	for k := 1; k <= 6; k++ { // weighing anchor and motoring away
		pts = append(pts, estPoint(estCentreLat, estCentreLon, 90, 40+100*float64(k)))
	}
	got := settledAISRun(pts, aisTestAnchorLat, aisTestAnchorLon)
	if len(got) >= aisAnchorFitMinPoints {
		t.Fatalf("a departed boat's run must collapse, got %d points", len(got))
	}
	if est := estimateAISAnchors(anchorWatchData{Lat: aisTestAnchorLat, Lon: aisTestAnchorLon, RadiusMeters: 20},
		map[string][]aisTrackPointWire{"111111111": pts}, 1); len(est) != 0 {
		t.Fatalf("a departed boat has no estimate, got %+v", est)
	}
}

func TestSettledAISRunOfNoPointsIsEmpty(t *testing.T) {
	if got := settledAISRun(nil, aisTestAnchorLat, aisTestAnchorLon); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestAISAnchorEstimatesIgnoreTheApproachBeforeTheBoatSettles(t *testing.T) {
	rng := rand.New(rand.NewSource(12))
	// Motoring in from 400 m at 4 kn: eight points 30 m apart every 15 s, stored
	// because each moved 25 m. Then 25 swing points a minute apart over 120 degrees.
	var pts []aisTrackPointWire
	for k := 0; k < 8; k++ {
		pts = append(pts, estPoint(estCentreLat, estCentreLon, 90, 400-30*float64(k)))
	}
	pts = append(pts, estArc(rng, 25, 200, 200, 40, 2)...)
	got := estimateAISAnchors(anchorWatchData{Lat: aisTestAnchorLat, Lon: aisTestAnchorLon, RadiusMeters: 20},
		map[string][]aisTrackPointWire{"111111111": pts}, 1)
	if len(got) != 1 {
		t.Fatalf("the swing after the approach must give an estimate, got %+v", got)
	}
	if d := haversineMeters(got[0].Lat, got[0].Lon, estCentreLat, estCentreLon); d > 6 {
		t.Fatalf("centre off by %.1f m: %+v", d, got[0])
	}
	if got[0].Points != 25 {
		t.Fatalf("the approach is not counted, want 25 points, got %d", got[0].Points)
	}
}

// aisFixture is a capture from a real anchorage (testdata/ais-anchorage-great-keppel.json):
// the watch, and each track under a label in place of its MMSI, times relative.
type aisFixture struct {
	Watch struct {
		Lat          float64 `json:"lat"`
		Lon          float64 `json:"lon"`
		RadiusMeters float64 `json:"radius_meters"`
	} `json:"watch"`
	// Own is our own swing trail, in the captures that have one.
	Own    []aisTrackPointWire `json:"own"`
	Tracks []struct {
		Label  string              `json:"label"`
		Points []aisTrackPointWire `json:"points"`
	} `json:"tracks"`
}

// The evening capture has our own swing trail too: the ratio comes from it, and
// every neighbour is judged with it.
func TestAISAnchorEstimatesOnTheEveningCapture(t *testing.T) {
	raw, err := os.ReadFile("testdata/ais-anchorage-evening.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx aisFixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	watch := anchorWatchData{Lat: fx.Watch.Lat, Lon: fx.Watch.Lon, RadiusMeters: fx.Watch.RadiusMeters}
	own, ready := fitAISAnchor(settledAISRun(fx.Own, watch.Lat, watch.Lon), watch.Lat, watch.Lon)
	if !ready {
		t.Fatal("our own trail must fit")
	}
	t.Logf("own: radius %.1f m, arc %.0f degrees", own.SwingRadiusM, own.SwingArcDeg)
	if own.SwingRadiusM < 50 || own.SwingRadiusM > 55 || own.SwingArcDeg < 300 {
		t.Errorf("own fit %.1f m over %.0f degrees, want about 52.6 over 314", own.SwingRadiusM, own.SwingArcDeg)
	}
	ratio, ok := ownSwingRatio(watch, fx.Own)
	t.Logf("ratio %.3f", ratio)
	if !ok || math.Abs(ratio-65/52.6) > 0.04 {
		t.Errorf("ratio %.3f (ready %v), want about %.2f", ratio, ok, 65/52.6)
	}
	tracks := map[string][]aisTrackPointWire{}
	for _, tr := range fx.Tracks {
		tracks[tr.Label] = tr.Points
	}
	got := estimateAISAnchors(watch, tracks, ratio)
	for _, e := range got {
		east, north := localENMeters(watch.Lat, watch.Lon, e.Lat, e.Lon)
		t.Logf("%s: radius %.1f m (drawn %.1f), arc %.0f degrees, centre %.0f E %.0f N, overlaps ours %v", e.MMSI, e.SwingRadiusM, e.DrawnRadiusM, e.SwingArcDeg, east, north, e.OverlapsOurs)
		if e.OverlapsOurs {
			t.Errorf("%s must not overlap ours", e.MMSI)
		}
	}
	byLabel := map[string]aisAnchorEstimate{}
	for _, e := range got {
		byLabel[e.MMSI] = e
	}
	// Wild Child fits 17.0 m over 122 degrees here. Its earlier swing, 80 to 100 m
	// east of our anchor, which pinned a 30 m circle on the Keppel capture, aged
	// out of this capture's 24 hours; keeping points for the life of the watch is
	// what fixes this live.
	wc, ok := byLabel["anchored-b-gaps"]
	if !ok || math.Abs(wc.SwingRadiusM-17.0) > 1.5 || math.Abs(wc.SwingArcDeg-122) > 8 {
		t.Errorf("Wild Child %+v, want about 17.0 m over 122 degrees", wc)
	}
	if math.Abs(wc.DrawnRadiusM-wc.SwingRadiusM*ratio) > 1e-9 {
		t.Errorf("drawn radius must be the fit times the ratio: %+v", wc)
	}
	// small-target has left: its track stops at the circle's edge and its last
	// settled stretch still fits on its own. The recorder's inside state hides it.
	st, ok := byLabel["small-target"]
	if !ok || math.Abs(st.SwingRadiusM-53.4) > 3 {
		t.Errorf("small-target %+v: the fit alone still gives a circle, which the inside filter hides", st)
	}
	if len(got) != 2 {
		t.Errorf("want Wild Child and small-target fitted and nothing else, got %d estimates", len(got))
	}
	markAISInside("anchored-b-gaps")
	t.Cleanup(resetAISAnchorTrackStateForTest)
	if shown := insideAISEstimates(got); len(shown) != 1 || shown[0].MMSI != "anchored-b-gaps" {
		t.Errorf("only the boat still inside the circle is offered, got %+v", shown)
	}
}

// A neighbour anchored at 1:16 PM that had barely swung in five hours, with its
// approach still at the head of the track. A fixed settled radius let the 1:10 PM
// approach point (about 96 m west and 42 m north of our anchor) into the run, and
// the fit drew a circle through it and the cluster.
func TestSettledAISRunOfAnUnswungBoatDropsItsApproach(t *testing.T) {
	raw, err := os.ReadFile("testdata/ais-anchorage-unswung.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx aisFixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	if len(fx.Tracks) != 1 || fx.Tracks[0].Label != "settled-unswung" {
		t.Fatalf("want the one settled-unswung track, got %d tracks", len(fx.Tracks))
	}
	watch := anchorWatchData{Lat: fx.Watch.Lat, Lon: fx.Watch.Lon, RadiusMeters: fx.Watch.RadiusMeters}
	pts := fx.Tracks[0].Points
	run := settledAISRun(pts, watch.Lat, watch.Lon)
	t.Logf("settled-unswung: run %d of %d points", len(run), len(pts))
	// The run may keep the approach point. The fit through it and the cluster
	// fails the arc gate, so the boat has no circle and none is drawn.
	if est := estimateAISAnchors(watch, map[string][]aisTrackPointWire{"settled-unswung": pts}, 1); len(est) != 0 {
		t.Fatalf("a boat that has not swung must not be drawn, got %+v", est)
	}
}

func TestAISAnchorEstimatesOnARealAnchorage(t *testing.T) {
	raw, err := os.ReadFile("testdata/ais-anchorage-great-keppel.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx aisFixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	watch := anchorWatchData{Lat: fx.Watch.Lat, Lon: fx.Watch.Lon, RadiusMeters: fx.Watch.RadiusMeters}
	tracks := map[string][]aisTrackPointWire{}
	for _, tr := range fx.Tracks {
		tracks[tr.Label] = tr.Points
		run := settledAISRun(tr.Points, watch.Lat, watch.Lon)
		t.Logf("%s: run %d of %d points", tr.Label, len(run), len(tr.Points))
	}
	byLabel := map[string]aisAnchorEstimate{}
	for _, e := range estimateAISAnchors(watch, tracks, 1) {
		byLabel[e.MMSI] = e
		t.Logf("%s: %+v", e.MMSI, e)
		if e.OverlapsOurs {
			t.Errorf("%s: the estimate must not overlap ours", e.MMSI)
		}
	}
	want := map[string]struct{ minR, maxR, east, north float64 }{
		"anchored-a":      {30, 40, 202, 208},
		"anchored-b-gaps": {25, 35, 65, 214},
	}
	for label, w := range want {
		e, ok := byLabel[label]
		if !ok {
			t.Errorf("%s: want an estimate", label)
			continue
		}
		east, north := localENMeters(watch.Lat, watch.Lon, e.Lat, e.Lon)
		t.Logf("%s: radius %.1f m, arc %.0f degrees, centre %.0f E %.0f N, %.0f m from our anchor", label, e.SwingRadiusM, e.SwingArcDeg, east, north, math.Hypot(east, north))
		if e.SwingRadiusM < w.minR || e.SwingRadiusM > w.maxR {
			t.Errorf("%s: radius %.1f m, want %.0f to %.0f", label, e.SwingRadiusM, w.minR, w.maxR)
		}
		if math.Hypot(east-w.east, north-w.north) > 10 {
			t.Errorf("%s: centre %.0f E %.0f N, want about %.0f E %.0f N", label, east, north, w.east, w.north)
		}
	}
	for _, label := range []string{"departed-a", "departed-b", "transit-a", "transit-b", "wide-wander", "anchored-c-small-swing"} {
		if _, ok := tracks[label]; !ok {
			t.Errorf("%s: missing from the fixture", label)
		}
		if e, ok := byLabel[label]; ok {
			t.Errorf("%s: want no estimate, got %+v", label, e)
		}
	}
	if len(byLabel) != len(want) {
		t.Errorf("want exactly %d estimates, got %d", len(want), len(byLabel))
	}
}

func TestAISAnchorEstimateOverlapComparesCentreDistanceWithTheSumOfRadii(t *testing.T) {
	rng := rand.New(rand.NewSource(10))
	pts := estArc(rng, 40, 100, 200, 40, 0.5)
	pts[0].NewSegment = true
	segs := map[string][]aisTrackPointWire{"111111111": pts}
	centreDistance := haversineMeters(aisTestAnchorLat, aisTestAnchorLon, estCentreLat, estCentreLon) // about 40 m
	// Our circle just reaches their swing circle: radius 40 + our radius > distance.
	near := estimateAISAnchors(anchorWatchData{Lat: aisTestAnchorLat, Lon: aisTestAnchorLon, RadiusMeters: centreDistance}, segs, 1)
	if len(near) != 1 || !near[0].OverlapsOurs {
		t.Fatalf("circles %.0f m apart with radii %.0f and about 40 overlap: %+v", centreDistance, centreDistance, near)
	}
	// Move our anchor 300 m away: well clear.
	far := estimateAISAnchors(anchorWatchData{Lat: aisTestAnchorLat - 0.003, Lon: aisTestAnchorLon, RadiusMeters: 20}, segs, 1)
	if len(far) != 1 || far[0].OverlapsOurs {
		t.Fatalf("circles 300 m apart do not overlap: %+v", far)
	}
}

// ownSwing is n points on an arc of our own swing, about our anchor, a minute apart.
func ownSwing(rng *rand.Rand, n int, radiusM float64) []aisTrackPointWire {
	pts := make([]aisTrackPointWire, n)
	for i := range pts {
		bearing := 60 + 200*float64(i)/float64(n-1)
		pts[i] = estPoint(aisTestAnchorLat, aisTestAnchorLon, bearing, radiusM+(rng.Float64()*2-1)*1.5)
	}
	return stampAISPoints(pts, 0, 60_000)
}

func TestOwnSwingRatioIsTheWatchRadiusOverOurFittedRadius(t *testing.T) {
	rng := rand.New(rand.NewSource(17))
	watch := anchorWatchData{Lat: aisTestAnchorLat, Lon: aisTestAnchorLon, RadiusMeters: 50}
	r, ok := ownSwingRatio(watch, ownSwing(rng, 40, 40))
	if !ok || math.Abs(r-1.25) > 0.1 {
		t.Fatalf("ratio %.2f (ready %v), want about 1.25 (50 m watch over a 40 m swing)", r, ok)
	}
}

func TestOwnSwingRatioIsNotReadyWhenOurTrailCannotBeFitted(t *testing.T) {
	rng := rand.New(rand.NewSource(18))
	watch := anchorWatchData{Lat: aisTestAnchorLat, Lon: aisTestAnchorLon, RadiusMeters: 45}
	short := ownSwing(rng, 40, 30)
	for i := range short {
		short[i] = estPoint(aisTestAnchorLat, aisTestAnchorLon, 60+30*float64(i)/39, 30)
	}
	for name, trail := range map[string][]aisTrackPointWire{
		"no trail":        nil,
		"too few points":  ownSwing(rng, 10, 30),
		"a 30 degree arc": short,
	} {
		if r, ok := ownSwingRatio(watch, trail); ok {
			t.Fatalf("%s: must not be ready, got ratio %.2f", name, r)
		}
	}
}

func TestOwnSwingRatioIsCappedAtTwo(t *testing.T) {
	rng := rand.New(rand.NewSource(25))
	// A small swing in light air against a big watch radius must not paint every
	// neighbour red: 60 m over 6 m is 10, and is held to 2.
	watch := anchorWatchData{Lat: aisTestAnchorLat, Lon: aisTestAnchorLon, RadiusMeters: 60}
	if r, ok := ownSwingRatio(watch, ownSwing(rng, 40, 6)); !ok || r != 2 {
		t.Fatalf("ratio %.2f (ready %v), want the cap of 2", r, ok)
	}
}

func TestOwnSwingRatioIsNeverUnderOne(t *testing.T) {
	rng := rand.New(rand.NewSource(19))
	watch := anchorWatchData{Lat: aisTestAnchorLat, Lon: aisTestAnchorLon, RadiusMeters: 20}
	if r, ok := ownSwingRatio(watch, ownSwing(rng, 40, 30)); !ok || r != 1 {
		t.Fatalf("a watch radius inside our fitted circle: ratio %.2f (ready %v), want 1 and ready", r, ok)
	}
}

func TestAISAnchorEstimateIsDrawnAtItsSwingTimesOurRatioAndOverlapUsesWhatIsDrawn(t *testing.T) {
	rng := rand.New(rand.NewSource(20))
	pts := estArc(rng, 40, 100, 200, 40, 0.5)
	segs := map[string][]aisTrackPointWire{"111111111": pts}
	// Put our anchor so the centres are further apart than our radius plus their
	// bare swing, but closer than our radius plus their drawn swing.
	const ratio = 1.5
	watch := anchorWatchData{Lat: aisTestAnchorLat - 0.0012, Lon: aisTestAnchorLon}
	d := haversineMeters(watch.Lat, watch.Lon, estCentreLat, estCentreLon)
	watch.RadiusMeters = d - 40 - 5
	bare := estimateAISAnchors(watch, segs, 1)
	drawn := estimateAISAnchors(watch, segs, ratio)
	if len(bare) != 1 || len(drawn) != 1 {
		t.Fatalf("want one estimate each, got %+v and %+v", bare, drawn)
	}
	if bare[0].OverlapsOurs || !drawn[0].OverlapsOurs {
		t.Fatalf("overlap must use the drawn radius: ratio 1 %v, ratio %.1f %v", bare[0].OverlapsOurs, ratio, drawn[0].OverlapsOurs)
	}
	if bare[0].DrawnRadiusM != bare[0].SwingRadiusM {
		t.Fatalf("at ratio 1 the drawn radius is the fit: %+v", bare[0])
	}
	if math.Abs(drawn[0].DrawnRadiusM-drawn[0].SwingRadiusM*ratio) > 1e-9 {
		t.Fatalf("drawn radius is the fit times the ratio: %+v", drawn[0])
	}
}

// ---- store and handler ------------------------------------------------------

func insertAISPoints(t *testing.T, store *passageStore, mmsi string, base time.Time, pts []aisTrackPointWire) {
	t.Helper()
	// Points are recorded after the watch started: move the start back to cover them.
	if started, err := store.anchorTrailStarted(); err == nil && !started.IsZero() && started.After(base) {
		setTrailStart(t, store, base.Add(-time.Minute))
	}
	for i, p := range pts {
		if _, err := store.db.Exec(`INSERT INTO ais_anchor_track_points (mmsi, t_ms, lat, lon, new_segment) VALUES (?,?,?,?,?)`,
			mmsi, base.Add(time.Duration(i)*time.Minute).UnixMilli(), p.Lat, p.Lon, boolInt(i == 0 || p.NewSegment)); err != nil {
			t.Fatal(err)
		}
	}
}

type aisAnchorsBody struct {
	Estimates   []aisAnchorEstimate `json:"estimates"`
	MarginRatio float64             `json:"margin_ratio"`
}

// appendOwnSwing stores our own swing trail, a minute apart from just after the drop.
func appendOwnSwing(t *testing.T, store *passageStore, pts []aisTrackPointWire) {
	t.Helper()
	base := time.Now().UTC().Add(time.Second)
	for i, p := range pts {
		if err := store.appendAnchorTrailPoint(base.Add(time.Duration(i)*time.Minute), p.Lat, p.Lon); err != nil {
			t.Fatal(err)
		}
	}
}

func getAISAnchors(t *testing.T) (int, aisAnchorsBody, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := getAISAnchorsHandler(echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/anchor-watch/ais-anchors", nil), rec)); err != nil {
		t.Fatal(err)
	}
	var body aisAnchorsBody
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body, rec.Body.String()
}

func TestAISAnchorsHandlerIsEmptyWithNoWatch(t *testing.T) {
	aisEnv(t)
	code, body, raw := getAISAnchors(t)
	if code != http.StatusOK || body.Estimates == nil || len(body.Estimates) != 0 {
		t.Fatalf("want 200 and an empty list, got %d %s", code, raw)
	}
}

func TestAISAnchorsHandlerServesAnEstimateFromTheStoredTrack(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	rng := rand.New(rand.NewSource(11))
	base := time.Now().UTC().Add(-2 * time.Hour)
	// A transit first, then the boat anchors: only the later segment counts.
	transit := make([]aisTrackPointWire, 25)
	for i := range transit {
		transit[i] = estPoint(aisTestAnchorLat, aisTestAnchorLon, 90, float64(i)*15)
	}
	markAISInside("111111111", "222222222")
	insertAISPoints(t, store, "111111111", base, transit)
	insertAISPoints(t, store, "111111111", base.Add(time.Hour), estArc(rng, 40, 100, 200, 40, 1))
	// A second target that has only passed through.
	insertAISPoints(t, store, "222222222", base, transit)
	appendOwnSwing(t, store, ownSwing(rng, 40, 30)) // estimates wait for our own swing

	code, body, raw := getAISAnchors(t)
	if code != http.StatusOK || len(body.Estimates) != 1 {
		t.Fatalf("want one estimate, got %d %s", code, raw)
	}
	e := body.Estimates[0]
	if e.MMSI != "111111111" || e.Points != 40 || math.Abs(e.SwingRadiusM-40) > 6 || e.SwingArcDeg < 150 {
		t.Fatalf("unexpected estimate %+v", e)
	}
	if d := haversineMeters(e.Lat, e.Lon, estCentreLat, estCentreLon); d > 5 {
		t.Fatalf("centre off by %.1f m", d)
	}
	// Their centre is about 40 m from our anchor, our radius is the default 20 m
	// and theirs about 40: 40 < 20 + 40.
	if !e.OverlapsOurs {
		t.Fatalf("expected overlap: %+v", e)
	}
}

func TestAISAnchorsHandlerSurfacesAWatchFileThatFailedToLoad(t *testing.T) {
	aisEnv(t)
	prevState, prevErr := anchorWatchState, anchorWatchLoadErr
	anchorWatchMu.Lock()
	anchorWatchState, anchorWatchLoadErr = nil, "anchor watch file unreadable: bad json"
	anchorWatchMu.Unlock()
	t.Cleanup(func() {
		anchorWatchMu.Lock()
		anchorWatchState, anchorWatchLoadErr = prevState, prevErr
		anchorWatchMu.Unlock()
	})
	code, _, raw := getAISAnchors(t)
	if code != http.StatusInternalServerError || !strings.Contains(raw, "bad json") {
		t.Fatalf("a corrupt watch file must not look like no watch, got %d %s", code, raw)
	}
}

func TestAISAnchorsHandlerDoesNotRefitWhenNothingChanged(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	rng := rand.New(rand.NewSource(13))
	base := time.Now().UTC().Add(-2 * time.Hour)
	markAISInside("111111111", "222222222")
	insertAISPoints(t, store, "111111111", base, estArc(rng, 40, 100, 200, 40, 1))
	appendOwnSwing(t, store, ownSwing(rng, 40, 30))

	before := aisAnchorEstimateComputes.Load()
	_, first, _ := getAISAnchors(t)
	_, second, _ := getAISAnchors(t)
	if got := aisAnchorEstimateComputes.Load() - before; got != 1 {
		t.Fatalf("two requests on unchanged data must compute once, computed %d times", got)
	}
	if len(first.Estimates) != 1 || len(second.Estimates) != 1 || first.Estimates[0] != second.Estimates[0] {
		t.Fatalf("the cached answer must equal the first: %+v vs %+v", first, second)
	}

	// A new stored point changes the answer's input.
	insertAISPoints(t, store, "222222222", base.Add(100*time.Minute), []aisTrackPointWire{{Lat: aisTestAnchorLat, Lon: aisTestAnchorLon}})
	getAISAnchors(t)
	if got := aisAnchorEstimateComputes.Load() - before; got != 2 {
		t.Fatalf("a new stored point must recompute, computed %d times", got)
	}

	// So does a new radius on the watch.
	current, _, _ := currentAnchorWatch()
	if code, resp := patchAnchorWatchForTest(t, map[string]any{"radius_meters": current.RadiusMeters + 10}); code != http.StatusOK {
		t.Fatalf("radius patch: %d %v", code, resp)
	}
	getAISAnchors(t)
	if got := aisAnchorEstimateComputes.Load() - before; got != 3 {
		t.Fatalf("a changed radius must recompute, computed %d times", got)
	}
}

// A point stored within the same key step does not refit: the recorder stores a
// point per target every 30 s, and a refit per point would run on every poll.
func TestAISAnchorsHandlerDoesNotRefitForAPointInTheSameStep(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	rng := rand.New(rand.NewSource(14))
	// Start of a key step, so the next point is certainly inside it.
	base := time.Now().UTC().Add(-2 * time.Hour).Truncate(aisAnchorOwnTrailKeyStep)
	markAISInside("111111111", "222222222")
	insertAISPoints(t, store, "111111111", base, estArc(rng, 40, 100, 200, 40, 1))
	appendOwnSwing(t, store, ownSwing(rng, 40, 30))
	before := aisAnchorEstimateComputes.Load()
	getAISAnchors(t)

	newest, err := store.aisAnchorNewestMs()
	if err != nil {
		t.Fatal(err)
	}
	inside := time.UnixMilli(newest).Truncate(aisAnchorOwnTrailKeyStep).Add(aisAnchorOwnTrailKeyStep - time.Second)
	if _, err := store.db.Exec(`INSERT INTO ais_anchor_track_points (mmsi, t_ms, lat, lon, new_segment) VALUES (?,?,?,?,1)`,
		"222222222", inside.UnixMilli(), aisTestAnchorLat, aisTestAnchorLon); err != nil {
		t.Fatal(err)
	}
	getAISAnchors(t)
	if got := aisAnchorEstimateComputes.Load() - before; got != 1 {
		t.Fatalf("a new point inside the same step must be served from the cache, computed %d times", got)
	}
}

func TestAISAnchorsHandlerServesOurRatioAndDrawnRadii(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	if code, resp := patchAnchorWatchForTest(t, map[string]any{"radius_meters": 45.0}); code != http.StatusOK {
		t.Fatalf("radius patch: %d %v", code, resp)
	}
	rng := rand.New(rand.NewSource(21))
	base := time.Now().UTC().Add(-2 * time.Hour)
	markAISInside("111111111")
	insertAISPoints(t, store, "111111111", base, estArc(rng, 40, 100, 200, 40, 1))

	// Our own trail is too short to fit yet: nothing is offered, not bare radii.
	code, body, raw := getAISAnchors(t)
	if code != http.StatusOK || len(body.Estimates) != 0 || body.Estimates == nil {
		t.Fatalf("no estimates until our own swing fits, got %d %s", code, raw)
	}

	appendOwnSwing(t, store, ownSwing(rng, 40, 30))
	_, body, raw = getAISAnchors(t)
	if len(body.Estimates) != 1 || math.Abs(body.MarginRatio-1.5) > 0.15 {
		t.Fatalf("ratio %.2f, want about 1.5 (45 m watch over a 30 m swing), got %s", body.MarginRatio, raw)
	}
	e := body.Estimates[0]
	if math.Abs(e.DrawnRadiusM-e.SwingRadiusM*body.MarginRatio) > 1e-6 {
		t.Fatalf("drawn radius must be the fit times the ratio: %s", raw)
	}
}

func TestAISAnchorsHandlerRecomputesWhenOurOwnTrailGrows(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	rng := rand.New(rand.NewSource(22))
	appendOwnSwing(t, store, ownSwing(rng, 5, 30)[:3])
	before := aisAnchorEstimateComputes.Load()
	getAISAnchors(t)
	getAISAnchors(t)
	if got := aisAnchorEstimateComputes.Load() - before; got != 1 {
		t.Fatalf("unchanged data computes once, computed %d times", got)
	}
	later := time.Now().UTC().Add(time.Hour)
	if err := store.appendAnchorTrailPoint(later, aisTestAnchorLat+0.0003, aisTestAnchorLon); err != nil {
		t.Fatal(err)
	}
	getAISAnchors(t)
	if got := aisAnchorEstimateComputes.Load() - before; got != 2 {
		t.Fatalf("a new own trail point must recompute, computed %d times", got)
	}
}

// markAISInside stands in for the recorder having seen these targets inside the
// 500 m circle on its last tick.
func markAISInside(mmsis ...string) {
	aisTracks.mu.Lock()
	defer aisTracks.mu.Unlock()
	for _, m := range mmsis {
		aisTracks.inside[m] = true
	}
}

func TestAISAnchorsHandlerOnlyOffersTargetsStillInsideTheRecordingCircle(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	rng := rand.New(rand.NewSource(24))
	base := time.Now().UTC().Add(-2 * time.Hour)
	insertAISPoints(t, store, "111111111", base, estArc(rng, 40, 100, 200, 40, 1))
	appendOwnSwing(t, store, ownSwing(rng, 40, 30))

	// A good track, but the boat has left the circle: no estimate.
	if _, body, raw := getAISAnchors(t); len(body.Estimates) != 0 {
		t.Fatalf("a target outside the circle gets no estimate, got %s", raw)
	}
	before := aisAnchorEstimateComputes.Load()
	// The recorder sees it inside: the same cached fit is served.
	markAISInside("111111111")
	if _, body, raw := getAISAnchors(t); len(body.Estimates) != 1 || body.Estimates[0].MMSI != "111111111" {
		t.Fatalf("a target inside the circle gets its estimate, got %s", raw)
	}
	if got := aisAnchorEstimateComputes.Load() - before; got != 0 {
		t.Fatalf("the inside filter applies after the cache: no refit, but computed %d times", got)
	}
	// And leaves again.
	aisTracks.mu.Lock()
	delete(aisTracks.inside, "111111111")
	aisTracks.mu.Unlock()
	if _, body, raw := getAISAnchors(t); len(body.Estimates) != 0 {
		t.Fatalf("a target that left the circle loses its estimate, got %s", raw)
	}
}

func TestAISAnchorTracksSinceWatchIgnoreRowsFromBeforeTheWatchStarted(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	started, err := store.anchorTrailStarted()
	if err != nil || started.IsZero() {
		t.Fatalf("a watch is running: %v %v", started, err)
	}
	for i, at := range []time.Time{started.Add(-time.Hour), started.Add(time.Minute)} {
		if _, err := store.db.Exec(`INSERT INTO ais_anchor_track_points (mmsi, t_ms, lat, lon, new_segment) VALUES ('111111111', ?, ?, ?, 0)`,
			at.UnixMilli(), aisInsideLat+float64(i)*1e-5, aisTestAnchorLon); err != nil {
			t.Fatal(err)
		}
	}
	all, err := store.aisAnchorTracksSinceWatch()
	if err != nil {
		t.Fatal(err)
	}
	if len(all["111111111"]) != 1 {
		t.Fatalf("only the row from after the watch started counts, got %+v", all)
	}
}

func TestAnchorTrailThinnedReadsTheWholeWatchPastTheStoreCap(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	started, _ := store.anchorTrailStarted()
	// 15,000 own points 5 s apart: more than anchorTrailMaxPoints, written
	// directly. The read covers all of it, one point in thirty seconds.
	for i := 0; i < 15000; i++ {
		if _, err := store.db.Exec(`INSERT INTO anchor_trail_points (t_ms, lat, lon) VALUES (?,?,?)`,
			started.Add(time.Duration(i+1)*5*time.Second).UnixMilli(), aisTestAnchorLat, aisTestAnchorLon); err != nil {
			t.Fatal(err)
		}
	}
	pts, err := store.anchorTrailThinned(ownTrailSpacing)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) < 2400 || len(pts) > 2600 {
		t.Fatalf("15,000 points 5 s apart thinned to 30 s want about 2,500, got %d", len(pts))
	}
	if !pts[0].Timestamp.Equal(started.Add(5 * time.Second)) {
		t.Fatalf("the read must start at the watch's first point, got %v", pts[0].Timestamp)
	}
	if last := pts[len(pts)-1].Timestamp; last.Before(started.Add(15000*5*time.Second - time.Minute)) {
		t.Fatalf("the read must reach the newest point, ends at %v", last)
	}
}

func TestAISAnchorsHandlerRecomputesForOurTrailAtMostOnceAMinute(t *testing.T) {
	store := aisEnv(t)
	dropWatch(t)
	t0 := time.Now().UTC().Truncate(time.Minute).Add(2*time.Hour + 5*time.Second)
	add := func(at time.Time, dLat float64) {
		if err := store.appendAnchorTrailPoint(at, aisTestAnchorLat+dLat, aisTestAnchorLon); err != nil {
			t.Fatal(err)
		}
	}
	add(t0, 0)
	before := aisAnchorEstimateComputes.Load()
	getAISAnchors(t)
	add(t0.Add(20*time.Second), 0.0001) // same minute
	getAISAnchors(t)
	if got := aisAnchorEstimateComputes.Load() - before; got != 1 {
		t.Fatalf("our trail growing inside a minute must not recompute, computed %d times", got)
	}
	add(t0.Add(70*time.Second), 0.0002) // next minute
	getAISAnchors(t)
	if got := aisAnchorEstimateComputes.Load() - before; got != 2 {
		t.Fatalf("a new minute of our trail must recompute, computed %d times", got)
	}
}

func TestAISAnchorsHandlerFailsLoudlyWithoutAStore(t *testing.T) {
	aisEnv(t)
	dropWatch(t)
	previous := globalPassageStore
	globalPassageStore = nil
	t.Cleanup(func() { globalPassageStore = previous })
	if code, _, _ := getAISAnchors(t); code != http.StatusInternalServerError {
		t.Fatalf("expected 500 with no store, got %d", code)
	}
}
