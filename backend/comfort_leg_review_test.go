package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Review fixes to the comfort chip on a plan leg (ADR 0302): the wire a leg sends,
// the wording an hour reads in, and how often a leg reads the model and the shore.

// F6: a position charted ashore is told by a flag the shore check sets, never by the
// words of a note.
func TestShoreGap_AshoreIsAFlagNotTheWordsOfANote(t *testing.T) {
	swell := &seaTrain{seaTrainFigures: seaTrainFigures{HeightM: 1, PeriodS: 6}, Unchecked: true, Note: swellUncheckedNote + reasonAshore}
	if got := shoreGap(spellSea{Swell: swell}); got == comfortReasonAshore {
		t.Fatalf("a note that only reads like ashore was taken for it: %q", got)
	}
	flagged := &seaTrain{seaTrainFigures: seaTrainFigures{HeightM: 1, PeriodS: 6}, Unchecked: true, ashore: true, Note: "any words at all"}
	if got := shoreGap(spellSea{Swell: flagged}); got != comfortReasonAshore {
		t.Errorf("a swell flagged ashore = %q", got)
	}
	wave := &seaTrain{seaTrainFigures: seaTrainFigures{HeightM: 1, PeriodS: 4}, Fetch: &fetchCheck{Check: seaCheckUnchecked, Reason: "any words at all", Ashore: true}}
	if got := shoreGap(spellSea{WindWave: wave}); got != comfortReasonAshore {
		t.Errorf("a wind wave flagged ashore = %q", got)
	}
}

// F7: a forecast hour is "this hour", never "this spell".
func TestCheckWindWave_NamesTheHourForAForecastHourAndTheSpellForASpell(t *testing.T) {
	kts, from := 12.0, 90.0
	cases := []struct {
		name string
		in   windInput
		want string
	}{
		{"hour, not kept", windInput{span: "hour"}, "this hour was stored before wind was kept"},
		{"hour, no wind", windInput{kept: true, span: "hour"}, "no true wind was recorded for this hour"},
		{"hour, varied", windInput{kept: true, kts: &kts, dirMissing: windDirVaried, span: "hour"}, "the wind direction varied too much during the hour"},
		{"spell, not kept", windInput{span: "spell"}, "this spell was stored before wind was kept"},
		{"spell, varied", windInput{kept: true, kts: &kts, dirMissing: windDirVaried, span: "spell"}, "the wind direction varied too much during the spell"},
	}
	for _, c := range cases {
		got := checkWindWave(c.in, &seaTrain{}, nil)
		if got.Reason != c.want {
			t.Errorf("%s: reason = %q, want %q", c.name, got.Reason, c.want)
		}
	}
	_ = from
}

// W1, W2: the server sends each hour's line in the logbook's sea words, and nothing
// the page does not read.
func TestLegComfort_TheWireIsTextMotionAndEachHoursLine(t *testing.T) {
	set := lcSet(1)
	c := lcRun(t, lcIn{set: set, hours: lcHours(lcHour(1, 0), lcHour(0.2, 0))})
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	if got := keysOf(top); got != "hours,motion,text" {
		t.Errorf("leg keys = %s", got)
	}
	var hours []map[string]json.RawMessage
	if err := json.Unmarshal(top["hours"], &hours); err != nil || len(hours) != 2 {
		t.Fatalf("hours = %s %v", top["hours"], err)
	}
	for _, h := range hours {
		if got := keysOf(h); got != "at,band,beyond,label,line" {
			t.Errorf("hour keys = %s", got)
		}
	}

	heading := lcHeading()
	p := set.predict(comfortHourInput{Model: waveModelRef{ID: lcModelID}, HeadingDeg: heading, Swell: &seaTrainFigures{HeightM: 1, PeriodS: 6, FromDeg: 0}})
	want := fmt.Sprintf("8 AM · Lively · roll %.1f°, pitch %.1f° · swell 1.0 m 6 s %s · snap %.2f g at the helm · pounding %.2f g",
		*p.RollDeg, *p.PitchDeg, seaWords(&heading, 0), *p.SnapSidewaysG(2.0), *p.Pounding.Value)
	if got := c.Hours[0].Line; got != want {
		t.Errorf("line = %q\nwant   %q", got, want)
	}
}

func TestLegComfort_AnHourNotJudgedReadsItsReasonInItsLine(t *testing.T) {
	reader := fetchReader(t, fetchWay("n", fetchPt(-18, 3), fetchPt(18, 3)))
	c := lcRun(t, lcIn{reader: reader, hours: lcHours(lcHour(0.5, 90), lcHour(1, 0))})
	lcMust(t, c, 2)
	if got, want := c.Hours[1].Line, "9 AM · Not judged: swell not judged in enclosed water"; got != want {
		t.Errorf("line = %q, want %q", got, want)
	}
}

func keysOf(m map[string]json.RawMessage) string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return strings.Join(ks, ",")
}

// E3: the model is read once for each hour, not once before the shore and again after.
func TestLegComfort_PredictsEachJudgedHourOnce(t *testing.T) {
	var predictions atomic.Int64
	c := lcRun(t, lcIn{onPredict: func() { predictions.Add(1) }, hours: lcHours(lcHour(1, 0), lcHour(1, 0), lcHour(0.2, 0), lcHour(0.2, 0), lcHour(0.2, 0))})
	lcMust(t, c, 5)
	if got := predictions.Load(); got != 5 {
		t.Fatalf("predictions = %d for 5 judged hours", got)
	}
}

// E1, E4: a leg that comes back over its own positions, in a memo that holds one ray
// result, still builds each position once.
func TestLegComfort_AnOutAndBackLegBuildsEachPositionOnceEvenInATinyMemo(t *testing.T) {
	a, b := fetchPt(-1, 0), fetchPt(1, 0)
	route := []passageRoutePoint{{Lat: a.Lat, Lon: a.Lon}, {Lat: b.Lat, Lon: b.Lon}, {Lat: a.Lat, Lon: a.Lon}}
	hours := make([]passageHour, 8)
	for i := range hours {
		hours[i] = lcHour(0.5, 0)
	}
	n := float64(len(hours))
	plan := planPassageLeg(0, passageLegArgs{Name: "there and back", Route: route, PassageHours: &n})
	distinct := map[[2]int]bool{}
	for i := range hours {
		lat, lon, _ := plan.at(float64(i) / n)
		distinct[[2]int{int(lat*100 + 0.5*sign(lat)), int(lon*100 + 0.5*sign(lon))}] = true
	}
	if len(distinct) < 4 {
		t.Fatalf("the route has only %d distinct positions: the test would prove nothing", len(distinct))
	}
	var hs []*passageHour
	for i := range hours {
		hs = append(hs, &hours[i])
	}
	memo := newCountedMemo(1)
	lcRunMemo(t, lcIn{memo: memo, route: route, hours: hs})
	if got := memo.builtCount(); got != len(distinct) {
		t.Errorf("built %d geometries for %d distinct positions", got, len(distinct))
	}
}

func sign(x float64) float64 {
	if x < 0 {
		return -1
	}
	return 1
}

// E2: a square's lines are read once, however many new positions lie in it.
func TestFetchGeoMemo_ReadsASquaresLinesOnceWhateverTheNextPositions(t *testing.T) {
	reader := fetchReader(t)
	memo := newCountedMemo(shoreRayEntries)
	lcRunMemo(t, lcIn{reader: reader, memo: memo, hours: lcHours(lcHour(0.5, 0), lcHour(0.5, 0))})
	first := memo.readCount()
	if first == 0 || memo.builtCount() == 0 {
		t.Fatal("the first request read no lines")
	}
	before := memo.builtCount()
	lcRunMemo(t, lcIn{reader: reader, memo: memo, hours: lcHours(lcHour(0.5, 0), lcHour(0.5, 0), lcHour(0.5, 0))})
	if memo.builtCount() == before {
		t.Fatal("the second request found only positions already built, so it proves nothing")
	}
	if got := memo.readCount(); got != first {
		t.Fatalf("the second request read %d more squares' lines", got-first)
	}
}

// E1: a request that judges the same positions again, in a memo that cannot hold
// them, builds nothing twice, since the request remembers the rays it has cast.
func TestLegComfort_OneRequestNeverBuildsAPositionTwiceEvenWhenTheSharedMemoCycles(t *testing.T) {
	memo := newCountedMemo(1)
	reader := fetchReader(t)
	setup := newComfortSetup(lcSet(1), lcBands(), nil, nil, func() *coastlineLoader { return reader.newLoader() }, memo)
	hours := lcHours(lcHour(0.5, 0), lcHour(0.5, 0), lcHour(0.5, 0), lcHour(0.5, 0))
	lcRunMemo(t, lcIn{setup: setup, memo: memo, hours: hours})
	first := memo.builtCount()
	if first < 3 {
		t.Fatalf("only %d builds in the first leg: the test would prove nothing", first)
	}
	lcRunMemo(t, lcIn{setup: setup, memo: memo, hours: hours})
	if got := memo.builtCount(); got != first {
		t.Fatalf("the second leg of the request built %d more", got-first)
	}
}

// E2: a square read as it was downloaded is not read again for a new request, and a
// square downloaded again is.
func TestSquareLinesCache_FollowsTheStateOfTheSquare(t *testing.T) {
	reader := fetchReader(t)
	memo := newCountedMemo(shoreRayEntries)
	hours := lcHours(lcHour(0.5, 0), lcHour(0.5, 0))
	lcRunMemo(t, lcIn{reader: reader, memo: memo, hours: hours})
	first := memo.readCount()
	needed, err := squaresWithin(fetchTestLat, fetchTestLon, shelterMaxFetchNm)
	if err != nil {
		t.Fatal(err)
	}
	for _, sq := range needed {
		if squareInland(sq) {
			continue
		}
		if err := reader.Store.SaveSquareAsOf(sq, nil, coastlineTestAt.Add(3*time.Hour), coastlineTestDataAsOf); err != nil {
			t.Fatal(err)
		}
	}
	lcRunMemo(t, lcIn{reader: reader, memo: memo, hours: hours})
	if got := memo.readCount(); got <= first {
		t.Fatalf("a square downloaded again was not read again: %d reads", got)
	}
}

// Review pass 3, finding 1: a leg under way with no readable ETA reads its real
// reason, never "ETA has passed".
func TestPlanComfortRun_ALegUnderWayWithNoETAReadsWhyNotThatItsETAPassed(t *testing.T) {
	r := planComfortRun{env: planEnv{now: paAt(14, 13, 30)}, ready: func() (*comfortSetup, string) { return &comfortSetup{}, "" }}
	w := &planLegWork{underway: true, windReady: false, fromLoc: paLoc, hours: 5,
		etaWhy:       "The leg is under way and has no departure time set, so its ETA isn't known.",
		chosenDepart: paAt(14, 8, 0), chosenArrive: paAt(14, 13, 0)}
	w.res = &planLegAssessment{Verdict: "ok"}
	c := r.leg(context.Background(), w)
	if c == nil || strings.Contains(c.Text, "ETA has passed") || !strings.Contains(c.Text, "no departure time set") || !strings.HasPrefix(c.Text, "Comfort not judged yet: ") {
		t.Fatalf("text = %+v", c)
	}
}

// rvHour is a judged or gap hour for the stretch tests: from minute a to minute b of
// the 8 AM hour.
func rvHour(a, b int, band, reason string) judgedHour {
	h := judgedHour{start: paAt(14, 8, a), end: paAt(14, 8, 0).Add(time.Duration(b) * time.Minute), band: band, reason: reason}
	if band == bandLively {
		h.lead = comfortLead{Word: "roll"}
		h.figures = "swell 1 m 6 s"
	}
	return h
}

func rvInput() legComfortInput {
	return legComfortInput{comfortSetup: &comfortSetup{bands: comfortBands{Learned: true}}, loc: paLoc}
}

// Review pass 3, finding 2: a gap sliver is never folded into a judged stretch.
func TestStretchesOf_AGapSliverNextToJudgedHoursStaysItsOwnGap(t *testing.T) {
	hours := []judgedHour{rvHour(0, 8, "", "position charted ashore"), rvHour(8, 68, bandComfortable, "")}
	st := stretchesOf(hours)
	if len(st) != 2 || !st[0].gap() || st[0].length() != 8*time.Minute || st[1].length() != 60*time.Minute {
		t.Fatalf("stretches = %+v", st)
	}
	if got := rvInput().text(hours, st); !strings.HasPrefix(got, "Not judged for the first 8 min: position charted ashore.") {
		t.Fatalf("text = %q", got)
	}
	// A judged sliver beside a judged stretch still folds in.
	if st := stretchesOf([]judgedHour{rvHour(0, 68, bandComfortable, ""), rvHour(68, 72, bandComfortable, "")}); len(st) != 1 || st[0].length() != 72*time.Minute {
		t.Fatalf("judged sliver = %+v", st)
	}
}

// Review pass 3, finding 3: the summary names every gap reason.
func TestSummary_NamesEveryDistinctGapReason(t *testing.T) {
	hours := []judgedHour{rvHour(0, 60, bandComfortable, ""), rvHour(60, 120, "", "position charted ashore"),
		rvHour(120, 180, bandComfortable, ""), rvHour(180, 240, "", "no wave split")}
	got := rvInput().text(hours, stretchesOf(hours))
	if !strings.Contains(got, "position charted ashore") || !strings.Contains(got, "no wave split") || !strings.Contains(got, "2 h") {
		t.Fatalf("text = %q", got)
	}
	gaps := []judgedHour{rvHour(0, 60, "", "position charted ashore"), rvHour(60, 120, "", "no wave split"),
		rvHour(120, 180, "", "position charted ashore"), rvHour(180, 240, "", "no wave split")}
	got = rvInput().text(gaps, stretchesOf(gaps))
	if !strings.Contains(got, "position charted ashore") || !strings.Contains(got, "no wave split") {
		t.Fatalf("all gaps text = %q", got)
	}
}

// Review pass 3, finding 4: where a stretch starts plus how long it runs is where it ends.
func TestStretch_StartPlusLengthIsItsEndWithALeadingSliver(t *testing.T) {
	hours := []judgedHour{rvHour(0, 5, bandComfortable, ""), rvHour(5, 65, bandLively, ""),
		rvHour(65, 125, bandComfortable, ""), rvHour(125, 185, "", "no wave split"), rvHour(185, 245, bandComfortable, "")}
	st := stretchesOf(hours)
	if len(st) != 4 {
		t.Fatalf("stretches = %d", len(st))
	}
	if got := st[0].start().Add(st[0].length()); !got.Equal(st[0].end()) {
		t.Fatalf("start %v + length %v = %v, end %v", st[0].start(), st[0].length(), got, st[0].end())
	}
}
