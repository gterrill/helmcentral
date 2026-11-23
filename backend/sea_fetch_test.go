package main

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

// ── the fetch cap on the wind wave, and the enclosed-water swell flag (ADR 0300) ──

const (
	fetchTestLat = -20.1
	fetchTestLon = 148.9
)

// fetchPt is a point x nautical miles east and y north of the test position.
func fetchPt(x, y float64) shorelinePoint {
	return newWRProjection(fetchTestLat, fetchTestLon).inv(wrPt{x, y})
}

func fetchWay(id string, pts ...shorelinePoint) shorelineLine {
	return shorelineLine{ID: id, Kind: "coastline", Points: pts}
}

// Coastline ways run with land on the left, so a shore with land to the east runs
// south and one with land to the west runs north.
func landEastOf(nm float64) shorelineLine { return fetchWay("e", fetchPt(nm, 18), fetchPt(nm, -18)) }
func landWestOf(nm float64) shorelineLine { return fetchWay("w", fetchPt(-nm, -18), fetchPt(-nm, 18)) }

// waterRing is a clockwise ring of coastline, which has the water inside it.
func waterRing(half float64) shorelineLine {
	return fetchWay("ring", fetchPt(-half, half), fetchPt(half, half), fetchPt(half, -half), fetchPt(-half, -half), fetchPt(-half, half))
}

// landRing is an anticlockwise ring, which has the land inside it.
func landRing(half float64) shorelineLine {
	return fetchWay("island", fetchPt(-half, half), fetchPt(-half, -half), fetchPt(half, -half), fetchPt(half, half), fetchPt(-half, half))
}

// seedCoastlineAlong holds, saved at savedAt, every square within reach of the points
// ([lat, lon]) that is not inland, each with the lines linesIn gives for it.
func seedCoastlineAlong(t testing.TB, savedAt time.Time, points [][2]float64, linesIn func(coverageSquare) []shorelineLine) *coastlineStore {
	t.Helper()
	store := newTestCoastlineStore(t)
	held := map[coverageSquare]bool{}
	for _, p := range points {
		needed, err := squaresWithin(p[0], p[1], shelterMaxFetchNm)
		if err != nil {
			t.Fatal(err)
		}
		for _, sq := range needed {
			if squareInland(sq) || held[sq] {
				continue
			}
			held[sq] = true
			if err := store.SaveSquareAsOf(sq, linesIn(sq), savedAt, coastlineTestDataAsOf); err != nil {
				t.Fatal(err)
			}
		}
	}
	return store
}

// seedCoastlineAround holds every square within reach of a position that is not
// inland, the lines in the one that holds the position.
func seedCoastlineAround(t *testing.T, lat, lon float64, lines []shorelineLine) *coastlineStore {
	t.Helper()
	own, err := squareContaining(lat, lon)
	if err != nil {
		t.Fatal(err)
	}
	return seedCoastlineAlong(t, time.Now(), [][2]float64{{lat, lon}}, func(sq coverageSquare) []shorelineLine {
		if sq == own {
			return lines
		}
		return nil
	})
}

// fetchReader holds every square within reach of the test position, the lines in
// the one that holds it.
func fetchReader(t *testing.T, lines ...shorelineLine) *offlineCoastlineReader {
	t.Helper()
	store := seedCoastlineAround(t, fetchTestLat, fetchTestLon, lines)
	return &offlineCoastlineReader{Enabled: func() (bool, error) { return true, nil }, Store: store}
}

// fetchSpell is a spell at the test position, heading north, with the wind as
// the stats would have kept it.
func fetchSpell(tws, from float64) feltSpell {
	sp := feltSpell{feltSpellCore: feltSpellCore{Start: 1_700_000_000, End: 1_700_000_900}}
	lat, lon, head := fetchTestLat, fetchTestLon, 0.0
	sp.MidLat, sp.MidLon, sp.MeanHeadingDeg = &lat, &lon, &head
	sp.WindKept, sp.WindKts, sp.WindFromDeg = true, &tws, &from
	return sp
}

func windWaveOf(h, p float64) *seaTrainFigures {
	return &seaTrainFigures{HeightM: h, PeriodS: p, FromDeg: 90}
}

// liveRays is the rays of a spell cast live over the reader's shore: the sweep's
// own source, which these tests use in place of the stored rows to check the
// judging itself. The page's source is the stored rows (sea_fetch_sweep_test.go).
func liveRays(reader *offlineCoastlineReader) func(sp feltSpell) raySource {
	loader := reader.newLoader()
	return func(sp feltSpell) raySource {
		return func() (rayFunc, string) {
			set, err := loader.around(*sp.MidLat, *sp.MidLon)
			if err != nil {
				return nil, coastlineReason(err)
			}
			geo := newFetchGeometry(*sp.MidLat, *sp.MidLon, set.Lines)
			return func(kind string, bearingDeg float64) (rayResult, string) {
				dd := 0
				if kind != fetchKindCalm {
					dd = bearingDD(bearingDeg)
				}
				return geo.ray(kind, dd), ""
			}, ""
		}
	}
}

// judged is the sea for one spell with the stored row given and the fetch check
// on the reader, cast live.
func judged(sp feltSpell, swell, windWave *seaTrainFigures, reader *offlineCoastlineReader) spellSea {
	k, _ := sp.seaKey()
	row := seaHindcastRow{seaKey: k, Swell: swell, WindWave: windWave}
	l := &seaLookup{rows: map[seaKey]seaHindcastRow{k: row}}
	sea := l.forSpell(sp)
	judgeSea(sp, &sea, liveRays(reader)(sp))
	return sea
}

func TestFetchLimitedWaveMatchesTheManualFigure(t *testing.T) {
	// Deep water, 20 kn over 5 nm: about half a metre and a 2.2 s peak period.
	hm0, tp := fetchWaveLimit(20, 5*metersPerNauticalMile)
	if !approx(hm0, 0.50, 0.01) || !approx(tp, 2.2, 0.05) {
		t.Errorf("Hm0 %.3f m, Tp %.3f s; want about 0.50 m and 2.2 s", hm0, tp)
	}
}

func TestLandTwoMilesUpwindCapsTheWindWave(t *testing.T) {
	reader := fetchReader(t, landEastOf(2))
	// Heading 000 and a wind angle of +90 is wind from 090, which comes off the land.
	sea := judged(fetchSpell(20, 90), nil, windWaveOf(1.5, 6), reader)
	w := sea.WindWave
	if w == nil || w.Fetch == nil || w.Fetch.Check != seaCheckCapped {
		t.Fatalf("wind wave %+v, want capped", w)
	}
	f := w.Fetch
	if f.FetchNm == nil || !approx(*f.FetchNm, 2.03, 0.05) {
		t.Errorf("fetch %v nm, want about 2.03", f.FetchNm)
	}
	if f.CapHeightM == nil || w.HeightM != *f.CapHeightM || w.HeightM >= 0.5 {
		t.Errorf("height %v with cap %v, want the cap, under 0.5 m", w.HeightM, f.CapHeightM)
	}
	if f.HindcastHeightM == nil || *f.HindcastHeightM != 1.5 || f.HindcastPeriodS == nil || *f.HindcastPeriodS != 6 {
		t.Errorf("the model's figures are kept: %+v", f)
	}
	if f.CapPeriodS == nil || w.PeriodS != *f.CapPeriodS || w.PeriodS >= 6 {
		t.Errorf("period %v with cap %v, want the capped period under 6 s", w.PeriodS, f.CapPeriodS)
	}
	if f.WindKts == nil || *f.WindKts != 20 {
		t.Errorf("wind %v kn, want 20", f.WindKts)
	}
	if !strings.HasPrefix(w.Note, "fetch-limited from 1.5 m 6 s: 2.0 nm fetch in 20 kn") {
		t.Errorf("note %q", w.Note)
	}
}

func TestLandTwoMilesDownwindLeavesTheWindWaveOpen(t *testing.T) {
	reader := fetchReader(t, landWestOf(2))
	sea := judged(fetchSpell(20, 90), nil, windWaveOf(1.5, 6), reader)
	w := sea.WindWave
	if w == nil || w.Fetch.Check != seaCheckOpen || w.HeightM != 1.5 || w.PeriodS != 6 || w.Fetch.FetchNm != nil || w.Note != "open water upwind" {
		t.Errorf("land behind the boat leaves the upwind water open: %+v", w)
	}
}

func TestACalmSpellTakesTheLongestFetchAnywhere(t *testing.T) {
	// Land on one side only: some bearing is open, so nothing caps.
	open := judged(fetchSpell(0.5, 0), nil, windWaveOf(0.4, 4), fetchReader(t, landEastOf(2)))
	if open.WindWave.Fetch.Check != seaCheckOpen {
		t.Errorf("a calm with an open bearing: %+v", open.WindWave)
	}
	// Shut in all round, a calm has no sea to build.
	shut := judged(fetchSpell(0.5, 0), nil, windWaveOf(0.4, 4), fetchReader(t, waterRing(2)))
	w := shut.WindWave
	if w.Fetch.Check != seaCheckCapped || w.Fetch.CapHeightM == nil || *w.Fetch.CapHeightM > 0.02 {
		t.Errorf("a calm in a ring of land: %+v", w)
	}
}

func TestAnOffshoreBreezeOffABeachCapsToAFewCentimetres(t *testing.T) {
	sea := judged(fetchSpell(20, 90), nil, windWaveOf(0.8, 5), fetchReader(t, landEastOf(0.1)))
	w := sea.WindWave
	if w.Fetch.Check != seaCheckCapped || w.HeightM <= 0 || w.HeightM > 0.1 {
		t.Errorf("a tenth of a mile of fetch: %+v", w)
	}
}

func TestAHindcastBelowTheCapIsLeftAlone(t *testing.T) {
	sea := judged(fetchSpell(20, 90), nil, windWaveOf(0.3, 1.5), fetchReader(t, landEastOf(5)))
	w := sea.WindWave
	if w.Fetch.Check != seaCheckWithin || w.HeightM != 0.3 || w.PeriodS != 1.5 {
		t.Fatalf("under the cap: %+v", w)
	}
	if w.Fetch.CapHeightM == nil || *w.Fetch.CapHeightM < 0.3 {
		t.Errorf("the cap is shown: %v", w.Fetch.CapHeightM)
	}
	if !strings.HasPrefix(w.Note, "within the ") || !strings.HasSuffix(w.Note, " m fetch limit") {
		t.Errorf("note %q", w.Note)
	}
}

func TestAMarinaBasinCapsToAFewCentimetres(t *testing.T) {
	sea := judged(fetchSpell(15, 90), nil, windWaveOf(0.6, 4), fetchReader(t, waterRing(0.08)))
	w := sea.WindWave
	if w.Fetch.Check != seaCheckCapped || w.HeightM > 0.08 {
		t.Errorf("a basin 0.08 nm across: %+v", w)
	}
}

func TestAPositionInsideALandRingIsChartedAshore(t *testing.T) {
	sea := judged(fetchSpell(15, 90), nil, windWaveOf(0.6, 4), fetchReader(t, landRing(0.5)))
	w := sea.WindWave
	if w.Fetch.Check != seaCheckUnchecked || !strings.Contains(w.Fetch.Reason, "charted ashore") || !strings.HasPrefix(w.Note, "not checked against fetch: ") {
		t.Errorf("a position ashore: %+v", w)
	}
	if w.HeightM != 0.6 || w.PeriodS != 4 {
		t.Errorf("an unchecked spell shows the model's figures: %+v", w)
	}
}

func TestAReefUpwindCapsAndSaysItDependsOnTheTide(t *testing.T) {
	reef := shorelineLine{ID: "r", Kind: "reef", Points: []shorelinePoint{fetchPt(1.5, -18), fetchPt(1.5, 18)}}
	w := judged(fetchSpell(20, 90), nil, windWaveOf(1.2, 6), fetchReader(t, reef)).WindWave
	if w.Fetch.Check != seaCheckCapped || !strings.Contains(w.Fetch.Caveat, "tide") || w.Fetch.Reason != "" || !strings.HasSuffix(w.Note, "; "+reefTideCaveat) {
		t.Errorf("a reef blocks like land and the caveat says it depends on the tide: %+v", w)
	}
}

func TestMissingCoastlineSaysWhichSquareAndWhy(t *testing.T) {
	store := newTestCoastlineStore(t)
	own, _ := squareContaining(fetchTestLat, fetchTestLon)
	if err := store.SaveSquareAsOf(own, nil, time.Now(), coastlineTestDataAsOf); err != nil {
		t.Fatal(err)
	}
	needed, _ := squaresWithin(fetchTestLat, fetchTestLon, shelterMaxFetchNm)
	var absent coverageSquare
	for _, sq := range needed {
		if sq != own && !squareInland(sq) {
			absent = sq
			break
		}
	}
	reader := &offlineCoastlineReader{Enabled: func() (bool, error) { return true, nil }, Store: store}
	w := judged(fetchSpell(20, 90), nil, windWaveOf(1.5, 6), reader).WindWave
	if w.Fetch.Check != seaCheckUnchecked || !strings.Contains(w.Fetch.Reason, absent.Label()) || !strings.Contains(w.Fetch.Reason, "outside coverage") {
		t.Errorf("a neighbour that is not held: %+v (want %s)", w, absent.Label())
	}
	if w.HeightM != 1.5 {
		t.Errorf("the model's figure stays, labelled: %+v", w)
	}
}

func TestACoastlineThatIsOffOrNotInstalledSaysSo(t *testing.T) {
	store := newTestCoastlineStore(t)
	off := &offlineCoastlineReader{Enabled: func() (bool, error) { return false, nil }, Store: store}
	if w := judged(fetchSpell(20, 90), nil, windWaveOf(1.5, 6), off).WindWave; w.Fetch.Check != seaCheckUnchecked || !strings.Contains(w.Fetch.Reason, "off") {
		t.Errorf("switched off: %+v", w)
	}
	if w := judged(fetchSpell(20, 90), nil, windWaveOf(1.5, 6), nil).WindWave; w.Fetch.Check != seaCheckUnchecked || !strings.Contains(w.Fetch.Reason, "isn't on this install") {
		t.Errorf("no reader: %+v", w)
	}
	unreadable := &offlineCoastlineReader{Enabled: func() (bool, error) { return false, errors.New("settings file unreadable") }, Store: store}
	if w := judged(fetchSpell(20, 90), nil, windWaveOf(1.5, 6), unreadable).WindWave; w.Fetch.Check != seaCheckUnchecked || !strings.Contains(w.Fetch.Reason, "couldn't be read") {
		t.Errorf("unreadable setting: %+v", w)
	}
}

func TestWithoutUsableWindASpellSaysWhyItWasNotChecked(t *testing.T) {
	reader := fetchReader(t, landEastOf(2))
	cases := map[string]func(*feltSpell){
		"before wind was kept":      func(sp *feltSpell) { sp.WindKept, sp.WindKts, sp.WindFromDeg = false, nil, nil },
		"no true wind":              func(sp *feltSpell) { sp.WindKts, sp.WindFromDeg = nil, nil },
		"no wind direction":         func(sp *feltSpell) { sp.WindFromDeg = nil },
		"direction varied too much": func(sp *feltSpell) { sp.WindFromDeg, sp.WindDirMissing = nil, windDirVaried },
	}
	for want, mutate := range cases {
		sp := fetchSpell(20, 90)
		mutate(&sp)
		w := judged(sp, nil, windWaveOf(1.5, 6), reader).WindWave
		if w.Fetch.Check != seaCheckUnchecked || !strings.Contains(w.Fetch.Reason, want) || w.HeightM != 1.5 {
			t.Errorf("%s: %+v", want, w)
		}
	}
}

func TestNoWindWaveMeansNothingToCap(t *testing.T) {
	sea := judged(fetchSpell(20, 90), &seaTrainFigures{HeightM: 0.8, PeriodS: 9, FromDeg: 270}, nil, nil)
	if sea.WindWave != nil {
		t.Errorf("wind wave %+v, want none", sea.WindWave)
	}
}

func TestAMissingHindcastHourKeepsItsOwnReason(t *testing.T) {
	sp := fetchSpell(20, 90)
	sea := (&seaLookup{rows: map[seaKey]seaHindcastRow{}}).forSpell(sp)
	judgeSea(sp, &sea, liveRays(nil)(sp))
	if sea.Reason != seaReasonNotFetched || sea.WindWave != nil {
		t.Errorf("sea %+v, want the not-fetched reason alone", sea)
	}
}

// A bay closed to the south-east: a shore 1 nm east and another 1 nm south.
func closedToTheSouthEast() []shorelineLine {
	return []shorelineLine{
		fetchWay("east", fetchPt(1, 18), fetchPt(1, -1)),
		fetchWay("south", fetchPt(1, -1), fetchPt(-18, -1)),
	}
}

func TestSwellFromAClosedQuarterIsNotJudged(t *testing.T) {
	reader := fetchReader(t, closedToTheSouthEast()...)
	swell := &seaTrainFigures{HeightM: 0.8, PeriodS: 9, FromDeg: 135}
	s := judged(fetchSpell(10, 0), swell, nil, reader).Swell
	if s == nil || !s.Enclosed || s.Note != "swell not judged in enclosed water" || s.Fetch != nil {
		t.Fatalf("south-east swell in a bay closed to the south-east: %+v", s)
	}
	if s.HeightM != 0.8 {
		t.Errorf("the stored figure is untouched: %+v", s)
	}
	north := &seaTrainFigures{HeightM: 0.8, PeriodS: 9, FromDeg: 0}
	if n := judged(fetchSpell(10, 0), north, nil, reader).Swell; n == nil || n.Enclosed || n.Note != "" {
		t.Errorf("the same bay with a northerly swell is judged: %+v", n)
	}
}

func TestSeaTrainWireNamesForTheFetchCheck(t *testing.T) {
	reader := fetchReader(t, landEastOf(2))
	sea := judged(fetchSpell(20, 90), nil, windWaveOf(1.5, 6), reader)
	raw, _ := json.Marshal(sea.WindWave)
	for _, key := range []string{`"fetch":{"check":"capped"`, `"fetch_nm"`, `"wind_kts"`, `"hindcast_height_m"`, `"hindcast_period_s"`, `"cap_height_m"`, `"cap_period_s"`, `"note":"fetch-limited from 1.5 m`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("wire lacks %s: %s", key, raw)
		}
	}
	swell, _ := json.Marshal(judged(fetchSpell(10, 0), &seaTrainFigures{HeightM: 0.8, PeriodS: 9, FromDeg: 135}, nil, fetchReader(t, closedToTheSouthEast()...)).Swell)
	if !strings.Contains(string(swell), `"enclosed":true`) || strings.Contains(string(swell), `"fetch"`) {
		t.Errorf("a swell carries only the enclosed flag: %s", swell)
	}
}

func TestThePassagePeakUsesCheckedSpellsAndCountsTheRest(t *testing.T) {
	train := func(h float64, check string) *seaTrain {
		return &seaTrain{seaTrainFigures: seaTrainFigures{HeightM: h, PeriodS: 4}, Fetch: &fetchCheck{Check: check}}
	}
	spells := []feltSpellOut{
		spellOut(1000, 1900, spellSea{WindWave: train(0.3, seaCheckCapped)}),
		spellOut(2000, 2900, spellSea{WindWave: train(1.8, seaCheckUnchecked)}),
		spellOut(3000, 3900, spellSea{WindWave: train(0.5, seaCheckOpen)}),
	}
	got := seaSummaryOf(spells)
	if got.WindWave == nil || got.WindWave.HeightM != 0.5 {
		t.Errorf("peak %+v, want the 0.5 m of a checked spell", got.WindWave)
	}
	if !strings.Contains(got.Partial, "not checked against fetch for 1 of 3 spells") {
		t.Errorf("partial %q", got.Partial)
	}
	all := seaSummaryOf([]feltSpellOut{spells[1]})
	if all.WindWave != nil || !strings.Contains(all.Reason, "not checked against fetch") || strings.Contains(all.Reason, "no swell or wind wave") {
		t.Errorf("every wind wave unchecked is never called flat: %+v", all)
	}
}

// What a spell reads off its shore: every figure the page can show.
type fetchReading struct {
	Wind, Wind2, Calm     [3]float64
	EnclosedSE, EnclosedN bool
	OnLand                bool
}

func readGeometry(g *fetchGeometry) fetchReading {
	var r fetchReading
	m, open, reef := g.windFetch(90)
	r.Wind = [3]float64{m, b2f(open), b2f(reef)}
	m, open, reef = g.windFetch(200)
	r.Wind2 = [3]float64{m, b2f(open), b2f(reef)}
	m, open, reef = g.calmFetch()
	r.Calm = [3]float64{m, b2f(open), b2f(reef)}
	r.EnclosedSE, r.EnclosedN = g.enclosed(135), g.enclosed(0)
	r.OnLand = g.onLand
	return r
}

// newUnculledFetchGeometry is the geometry of every line given, with nothing left
// out: what newFetchGeometry must come to, however much it culls.
func newUnculledFetchGeometry(lat, lon float64, lines []shorelineLine) *fetchGeometry {
	segs, _ := projectShoreline(lat, lon, lines)
	g := &fetchGeometry{}
	var coast []wrSeg
	for _, s := range segs {
		kind := lines[s.line].Kind
		s.line = fetchSegCoast
		if kind == "reef" {
			s.line = fetchSegReef
		}
		g.segs = append(g.segs, s)
		if kind == "coastline" {
			coast = append(coast, wrSeg{wrPt{s.x1, s.y1}, wrPt{s.x2, s.y2}})
		}
	}
	_, _, _, onLand, found := nearestShore(wrPt{}, coast)
	g.onLand = found && onLand
	return g
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func TestCullingTheShoreChangesNothingASpellReads(t *testing.T) {
	reef := shorelineLine{ID: "r", Kind: "reef", Points: []shorelinePoint{fetchPt(1.5, -18), fetchPt(1.5, 18)}}
	far := fetchWay("far", fetchPt(-30, -40), fetchPt(-30, 40), fetchPt(30, 40))
	fixtures := map[string][]shorelineLine{
		"near shore with far shore beyond reach": {landEastOf(2), reef, far, waterRing(40)},
		"a bay closed to the south-east":         append(closedToTheSouthEast(), far),
		"a basin":                                {waterRing(0.08), far},
		"shore only beyond 15 nm":                {far, landEastOf(25)},
		// Nearest shore is 20 nm away with land on the left: ashore, though no ray reaches it.
		"ashore with the shore out of reach": {landWestOf(-20)},
		"open water":                         nil,
		// Both ends far outside the reach, the way between them across it.
		"a long shore crossing the reach":        {fetchWay("cross", fetchPt(40, 3), fetchPt(-40, 3)), landEastOf(2)},
		"a corner of the box outside the circle": {fetchWay("corner", fetchPt(14.8, 14.8), fetchPt(14.9, -14.9)), landEastOf(5)},
	}
	for name, lines := range fixtures {
		culled := readGeometry(newFetchGeometry(fetchTestLat, fetchTestLon, lines))
		whole := readGeometry(newUnculledFetchGeometry(fetchTestLat, fetchTestLon, lines))
		if culled != whole {
			t.Errorf("%s: culled %+v, unculled %+v", name, culled, whole)
		}
	}
	if !readGeometry(newFetchGeometry(fetchTestLat, fetchTestLon, []shorelineLine{landWestOf(-20)})).OnLand {
		t.Error("a position 20 nm inside the land is charted ashore, culled or not")
	}
}

// Only the shore near a position is projected: a spell never projects every line of
// the squares it needs.
func TestOnlyNearbyShoreIsProjectedForASpell(t *testing.T) {
	far := fetchWay("far", fetchPt(-30, -40), fetchPt(-30, 40), fetchPt(30, 40))
	cross := fetchWay("cross", fetchPt(40, 3), fetchPt(-40, 3), fetchPt(-40, 50))
	near := landEastOf(2)
	got := cullShoreline(fetchTestLat, fetchTestLon, []shorelineLine{far, cross, near})
	var ids []string
	points := 0
	for _, l := range got {
		ids = append(ids, l.ID)
		points += len(l.Points)
	}
	if strings.Contains(strings.Join(ids, ","), "far") || points != 4 {
		t.Errorf("kept %v with %d points, want the crossing way's one segment and the near way", ids, points)
	}
}

// A loader that failed to read the coastline's switch tries again on the next call,
// as it did before the loader was shared.
func TestALoaderRetriesAfterATransientError(t *testing.T) {
	calls := 0
	r := &offlineCoastlineReader{Store: newTestCoastlineStore(t), Enabled: func() (bool, error) {
		calls++
		if calls == 1 {
			return false, errors.New("settings busy")
		}
		return true, nil
	}}
	l := r.newLoader()
	if err := l.ready(); err == nil {
		t.Fatal("the first call should fail")
	}
	if err := l.ready(); err != nil {
		t.Errorf("the second call gives %v, want it to retry and succeed", err)
	}
	if err := l.ready(); err != nil || calls != 2 {
		t.Errorf("success is remembered: err %v after %d reads, want nil after 2", err, calls)
	}
}

// Land just across the antimeridian caps the wind wave as it would anywhere else.
func TestLandAcrossTheAntimeridianIsKept(t *testing.T) {
	const lat, lon = fetchTestLat, 179.99
	// 2 nm of longitude, wrapped past 180 to the west-longitude side.
	dLon := 2.0 / 60 / math.Cos(lat*math.Pi/180)
	eastLon := lon + dLon - 360
	// Land to the east, so the way runs south.
	way := fetchWay("e", shorelinePoint{Lat: lat + 0.3, Lon: eastLon}, shorelinePoint{Lat: lat - 0.3, Lon: eastLon})
	if got := cullShoreline(lat, lon, []shorelineLine{way}); len(got) != 1 {
		t.Fatalf("cull kept %d lines, want the way across the antimeridian", len(got))
	}
	fetch, open, _ := newFetchGeometry(lat, lon, []shorelineLine{way}).windFetch(90)
	if open || !approx(fetch/metersPerNauticalMile, 2.03, 0.05) {
		t.Errorf("fetch %.0f m open %v, want about 2.03 nm", fetch, open)
	}
}

func TestShorelineAroundErrorsAreTypedAndKeepShelterWording(t *testing.T) {
	store := newTestCoastlineStore(t)
	own, _ := squareContaining(fetchTestLat, fetchTestLon)
	if err := store.SaveSquareAsOf(own, nil, time.Now(), coastlineTestDataAsOf); err != nil {
		t.Fatal(err)
	}
	r := &offlineCoastlineReader{Enabled: func() (bool, error) { return true, nil }, Store: store}
	_, err := r.newLoader().around(fetchTestLat, fetchTestLon)
	var typed *coastlineLookupError
	if !errors.As(err, &typed) || typed.Problem != coastlineSquaresMissing || len(typed.Missing) == 0 {
		t.Fatalf("error %v, want typed with the missing squares", err)
	}
	_, err = r.ShorelineAround(fetchTestLat, fetchTestLon)
	if err == nil || !strings.Contains(err.Error(), "shelter at") || !strings.Contains(err.Error(), "needs neighbouring Offline coastline squares") {
		t.Errorf("shelter's wording changed: %v", err)
	}
	off := &offlineCoastlineReader{Enabled: func() (bool, error) { return false, nil }, Store: store}
	_, err = off.newLoader().around(fetchTestLat, fetchTestLon)
	if !errors.As(err, &typed) || typed.Problem != coastlineOff {
		t.Errorf("off: %v", err)
	}
	_, err = off.ShorelineAround(fetchTestLat, fetchTestLon)
	if err == nil || err.Error() != "Offline coastline is off — turn it on in Settings → Offline coastline" {
		t.Errorf("off, in shelter's words: %v", err)
	}
}

// An inland square within reach that is held but not ready (still downloading, or
// failed) holds no coastline by definition and must not fail the lookup.
func TestAHeldButNotReadyInlandSquareIsSkipped(t *testing.T) {
	var inland coverageSquare
	found := false
	for la := -30; la <= -20 && !found; la++ {
		for lo := 130; lo <= 140 && !found; lo++ {
			if sq := (coverageSquare{Lat: la, Lon: lo}); squareInland(sq) {
				inland, found = sq, true
			}
		}
	}
	if !found {
		t.Skip("no inland square in the mask")
	}
	for _, status := range []string{"downloading", coastlineStatusFailed} {
		l := fetchReader(t).newLoader()
		if err := l.ready(); err != nil {
			t.Fatal(err)
		}
		l.byKey[inland] = coastlineSquareRecord{Square: inland, Status: status}
		set, err := l.linesFor([]coverageSquare{inland})
		if err != nil || len(set.Lines) != 0 {
			t.Errorf("%s inland square: lines %v, err %v; want skipped", status, set.Lines, err)
		}
	}
}

// Height and period are each held to their own fetch limit.
func TestThePeriodIsCappedEvenWhenTheHeightIsWithinItsLimit(t *testing.T) {
	reader := fetchReader(t, landEastOf(2))
	probe := judged(fetchSpell(20, 90), nil, windWaveOf(1.5, 6), reader).WindWave.Fetch
	h := *probe.CapHeightM * 0.5 // under the height limit, with a period far over its limit
	w := judged(fetchSpell(20, 90), nil, windWaveOf(h, 9), reader).WindWave
	if w.Fetch.Check != seaCheckCapped || w.HeightM != h || w.PeriodS != *w.Fetch.CapPeriodS || w.PeriodS >= 9 {
		t.Fatalf("a long period under a low height: %+v", w)
	}
	if !strings.HasPrefix(w.Note, "period fetch-limited from 9 s: ") || !strings.Contains(w.Note, "nm fetch in 20 kn") {
		t.Errorf("note %q should say only the period was cut", w.Note)
	}
	within := judged(fetchSpell(20, 90), nil, windWaveOf(h, *probe.CapPeriodS*0.5), reader).WindWave
	if within.Fetch.Check != seaCheckWithin {
		t.Errorf("both under their limits: %+v", within.Fetch)
	}
}

// The capped note names what was cut and nothing else.
func TestTheCappedNoteNamesOnlyWhatWasCut(t *testing.T) {
	reader := fetchReader(t, landEastOf(2))
	probe := judged(fetchSpell(20, 90), nil, windWaveOf(1.5, 6), reader).WindWave.Fetch
	lowH, lowP := *probe.CapHeightM*0.5, *probe.CapPeriodS*0.5
	cases := []struct {
		name       string
		h, p       float64
		wantPrefix string
	}{
		{"height only", 1.5, lowP, "fetch-limited from 1.5 m: "},
		{"period only", lowH, 9, "period fetch-limited from 9 s: "},
		{"both", 1.5, 6, "fetch-limited from 1.5 m 6 s: "},
	}
	for _, c := range cases {
		w := judged(fetchSpell(20, 90), nil, windWaveOf(c.h, c.p), reader).WindWave
		if w.Fetch.Check != seaCheckCapped || !strings.HasPrefix(w.Note, c.wantPrefix) || !strings.HasSuffix(w.Note, "nm fetch in 20 kn") {
			t.Errorf("%s: %q, want it to start %q", c.name, w.Note, c.wantPrefix)
		}
	}
}

// A cut smaller than the page's own rounding (a tenth of a metre, a whole second)
// would read as a cut to the same figure, so it is not named; with none left the
// check reads as within.
func TestACutThatDoesNotShowAtDisplayRoundingIsNotNamed(t *testing.T) {
	reader := fetchReader(t, landEastOf(2))
	probe := judged(fetchSpell(20, 90), nil, windWaveOf(1.5, 6), reader).WindWave.Fetch
	if *probe.CapHeightM < 0.25 || *probe.CapHeightM > 0.34 || *probe.CapPeriodS < 1.0 || *probe.CapPeriodS > 1.45 {
		t.Fatalf("the fixture moved: cap %.3f m %.3f s", *probe.CapHeightM, *probe.CapPeriodS)
	}
	// A wave a hair over the cap in height reads as the cap's own figure; 1.4 s
	// reads as 1 s against a cap of the same 1 s.
	hairH := math.Max(math.Round(*probe.CapHeightM*10)/10+0.04, *probe.CapHeightM+0.001)
	if hairH <= *probe.CapHeightM || math.Round(hairH*10) != math.Round(*probe.CapHeightM*10) {
		t.Fatalf("the fixture moved: %.3f m against a cap of %.3f m reads differently", hairH, *probe.CapHeightM)
	}
	slightH := judged(fetchSpell(20, 90), nil, windWaveOf(hairH, 1.0), reader).WindWave
	if slightH.Fetch.Check != seaCheckWithin || strings.Contains(slightH.Note, "fetch-limited") {
		t.Errorf("a height cut that reads the same: %+v note %q", slightH.Fetch, slightH.Note)
	}
	slightP := judged(fetchSpell(20, 90), nil, windWaveOf(*probe.CapHeightM*0.5, 1.4), reader).WindWave
	if slightP.Fetch.Check != seaCheckWithin || strings.Contains(slightP.Note, "fetch-limited") {
		t.Errorf("a period cut that reads the same: %+v note %q", slightP.Fetch, slightP.Note)
	}
	// A height cut that shows, beside a period cut that does not, names the height alone.
	heightShows := judged(fetchSpell(20, 90), nil, windWaveOf(0.9, 1.4), reader).WindWave
	if heightShows.Fetch.Check != seaCheckCapped || !strings.HasPrefix(heightShows.Note, "fetch-limited from 0.9 m: ") {
		t.Errorf("height shows, period does not: %q", heightShows.Note)
	}
	periodShows := judged(fetchSpell(20, 90), nil, windWaveOf(hairH, 5), reader).WindWave
	if periodShows.Fetch.Check != seaCheckCapped || !strings.HasPrefix(periodShows.Note, "period fetch-limited from 5 s: ") {
		t.Errorf("period shows, height does not: %q", periodShows.Note)
	}
}

// A calm has no wind direction, so no wording about upwind.
func TestACalmSpellNeverSaysUpwind(t *testing.T) {
	open := judged(fetchSpell(0.5, 0), nil, windWaveOf(0.4, 4), fetchReader(t, landEastOf(2))).WindWave
	if open.Note != "open water nearby" {
		t.Errorf("a calm with an open bearing: %q", open.Note)
	}
	shut := judged(fetchSpell(0.5, 0), nil, windWaveOf(0.4, 4), fetchReader(t, waterRing(2))).WindWave
	if strings.Contains(shut.Note, "upwind") || !strings.Contains(shut.Note, "calm") || !strings.HasPrefix(shut.Note, "fetch-limited from ") {
		t.Errorf("a calm in a ring of land: %q", shut.Note)
	}
	if w := judged(fetchSpell(20, 90), nil, windWaveOf(0.9, 5), fetchReader(t, landWestOf(2))).WindWave; w.Note != "open water upwind" {
		t.Errorf("a wind still says upwind: %q", w.Note)
	}
}

// A swell that cannot be checked for enclosed water keeps its figure, says why, and
// stays out of the passage's peak.
func TestASwellWithNoShoreToReadIsLabelledNotChecked(t *testing.T) {
	swell := &seaTrainFigures{HeightM: 0.8, PeriodS: 9, FromDeg: 135}
	s := judged(fetchSpell(10, 0), swell, nil, nil).Swell
	if s == nil || s.Enclosed || s.HeightM != 0.8 || !strings.HasPrefix(s.Note, "not checked for enclosed water: ") || !s.Unchecked {
		t.Fatalf("swell with no coastline: %+v", s)
	}
	spells := []feltSpellOut{
		spellOut(1000, 1900, spellSea{Swell: &seaTrain{seaTrainFigures: seaTrainFigures{HeightM: 2, PeriodS: 9}, Unchecked: true}}),
		spellOut(2000, 2900, spellSea{Swell: &seaTrain{seaTrainFigures: seaTrainFigures{HeightM: 0.5, PeriodS: 9}}}),
	}
	got := seaSummaryOf(spells)
	if got.Swell == nil || got.Swell.HeightM != 0.5 || !strings.Contains(got.Partial, "1 of 2") {
		t.Errorf("peak %+v partial %q, want the checked 0.5 m and the other counted", got.Swell, got.Partial)
	}
}

// The notes sit inside the page's own brackets, so a reason has none of its own.
func TestNoSpellNoteHoldsNestedBrackets(t *testing.T) {
	store := newTestCoastlineStore(t)
	own, _ := squareContaining(fetchTestLat, fetchTestLon)
	if err := store.SaveSquareAsOf(own, nil, time.Now(), coastlineTestDataAsOf); err != nil {
		t.Fatal(err)
	}
	readers := map[string]*offlineCoastlineReader{
		"off":        {Enabled: func() (bool, error) { return false, nil }, Store: store},
		"missing":    {Enabled: func() (bool, error) { return true, nil }, Store: store},
		"not there":  nil,
		"unreadable": {Enabled: func() (bool, error) { return false, errors.New("settings file unreadable") }, Store: store},
	}
	noBrackets := func(name, note string) {
		t.Helper()
		if note == "" || strings.ContainsAny(note, "()[]") {
			t.Errorf("%s: note %q is empty or has brackets", name, note)
		}
	}
	for name, reader := range readers {
		noBrackets("coastline "+name, judged(fetchSpell(20, 90), nil, windWaveOf(1.5, 6), reader).WindWave.Note)
	}
	// Every reason the wind itself can give, and the position's.
	good := fetchReader(t, landEastOf(2))
	for name, mutate := range map[string]func(*feltSpell){
		"before wind was kept": func(sp *feltSpell) { sp.WindKept, sp.WindKts, sp.WindFromDeg = false, nil, nil },
		"no true wind":         func(sp *feltSpell) { sp.WindKts, sp.WindFromDeg = nil, nil },
		"no wind direction":    func(sp *feltSpell) { sp.WindFromDeg = nil },
		"direction varied":     func(sp *feltSpell) { sp.WindFromDeg, sp.WindDirMissing = nil, windDirVaried },
	} {
		sp := fetchSpell(20, 90)
		mutate(&sp)
		noBrackets(name, judged(sp, nil, windWaveOf(1.5, 6), good).WindWave.Note)
	}
	noBrackets("ashore", judged(fetchSpell(15, 90), nil, windWaveOf(0.6, 4), fetchReader(t, landRing(0.5))).WindWave.Note)
	noBrackets("reef", judged(fetchSpell(20, 90), nil, windWaveOf(1.2, 6), fetchReader(t, shorelineLine{ID: "r", Kind: "reef", Points: []shorelinePoint{fetchPt(1.5, -18), fetchPt(1.5, 18)}})).WindWave.Note)
	// Every way the coastline itself can be unreadable.
	for _, problem := range []coastlineProblem{coastlineNotOnInstall, coastlineSettingUnreadable, coastlineOff, coastlineInland, coastlineSquaresMissing} {
		err := &coastlineLookupError{Problem: problem, Err: errors.New("settings file unreadable"), Missing: []missingSquare{{Label: "S21 E149", State: "download failed: no route"}, {Label: "S21 E148", State: "still downloading"}}}
		noBrackets("coastlineReason "+string(problem), coastlineReason(err))
	}
	noBrackets("coastlineReason other", coastlineReason(errors.New("disk full")))
	// The swell's notes sit in the same brackets.
	swell := &seaTrainFigures{HeightM: 0.8, PeriodS: 9, FromDeg: 135}
	noBrackets("swell unchecked", judged(fetchSpell(10, 0), swell, nil, nil).Swell.Note)
	noBrackets("swell enclosed", judged(fetchSpell(10, 0), swell, nil, fetchReader(t, closedToTheSouthEast()...)).Swell.Note)
	if w := judged(fetchSpell(20, 90), nil, windWaveOf(1.5, 6), readers["off"]).WindWave; !strings.Contains(w.Note, "Settings → Offline coastline") {
		t.Errorf("off still says where to turn it on: %q", w.Note)
	}
	if w := judged(fetchSpell(20, 90), nil, windWaveOf(1.5, 6), readers["missing"]).WindWave; !strings.Contains(w.Note, "square ") || !strings.Contains(w.Note, "outside coverage") {
		t.Errorf("missing square: %q", w.Note)
	}
}

func TestSpellsAtOnePositionReadTheSquaresAndTheShoreOnce(t *testing.T) {
	reads := 0
	reader := fetchReader(t, landEastOf(2))
	enabled := reader.Enabled
	reader.Enabled = func() (bool, error) { reads++; return enabled() }
	rays := liveRays(reader)
	for i := 0; i < 3; i++ {
		sea := spellSea{WindWave: &seaTrain{seaTrainFigures: *windWaveOf(1.5, 6)}}
		sp := fetchSpell(20, 90)
		judgeSea(sp, &sea, rays(sp))
		if sea.WindWave.Fetch == nil || sea.WindWave.Fetch.Check != seaCheckCapped {
			t.Fatalf("spell %d: %+v", i, sea.WindWave)
		}
	}
	if reads != 1 {
		t.Errorf("the switch was read %d times, want once", reads)
	}
}
