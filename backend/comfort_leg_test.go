package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The comfort chip on a plan leg (ADR 0302). The leg runs east along 2 nm of open
// water near the fetch tests' position, 8 AM on the 14th at Brisbane, one forecast
// stamp for each hour. Waves from the north are on the beam, waves from the east
// dead ahead. The test model reads 2 degrees of roll for each metre of 6 s swell
// on the beam, and the bands put Lively from 1.3 degrees.

const lcModelID = "test_wave"

func lcRoute() []passageRoutePoint {
	a, b := fetchPt(-1, 0), fetchPt(1, 0)
	return []passageRoutePoint{{Lat: a.Lat, Lon: a.Lon, Name: "A"}, {Lat: b.Lat, Lon: b.Lon, Name: "B"}}
}

func lcHeading() float64 {
	r := lcRoute()
	return bearingDeg(r[0].Lat, r[0].Lon, r[1].Lat, r[1].Lon)
}

// lcHour is an hour with a 6 s swell of the height given from the direction given.
func lcHour(swellH, swellFrom float64) passageHour {
	h := passageHour{
		WindMS: kn(10), GustMS: kn(10), WindDirDeg: 90,
		HasWave: true, WaveM: swellH, WaveDirDeg: swellFrom, HasWaveDir: true, HasWavePeriod: true, WavePeriodS: 6,
		WaveModel: waveModelRef{ID: lcModelID, Label: "The test wave model"},
	}
	h.Swell = &seaTrainFigures{HeightM: swellH, PeriodS: 6, FromDeg: swellFrom}
	return h
}

func lcHours(hs ...passageHour) []*passageHour {
	out := make([]*passageHour, len(hs))
	for i := range hs {
		out[i] = &hs[i]
	}
	return out
}

func lcBands() comfortBands {
	return learnComfortBands([]ratedFelt{{ratingComfortable, 0.5}, {ratingComfortable, 0.6}, {ratingLively, 2.0}, {ratingLively, 2.5}})
}

func lcFit(t comfortTarget, bs float64, maxXs float64) *comfortTargetFit {
	return &comfortTargetFit{Target: t, Coef: varianceCoef{Bs: bs, Bw: bs}, MaxXs: maxXs, MaxXw: maxXs}
}

// lcSet learns sway, snap and pounding. Roll is 2 degrees for each metre of 6 s
// swell on the beam; maxXs is the steepest feature in the record.
func lcSet(maxXs float64) *comfortModelSet {
	perM := math.Pi / (gravityMPerS2 * 36 / (2 * math.Pi))
	bs := (2.0 / perM) * (2.0 / perM)
	group := func(name string, fits ...*comfortTargetFit) comfortGroup {
		return comfortGroup{comfortGroupStatus: comfortGroupStatus{Name: name, Learned: true}, Fits: fits}
	}
	return &comfortModelSet{Fits: map[string]*comfortFit{lcModelID: {
		Sway:     group("sway", lcFit(targetRoll, bs, maxXs), lcFit(targetPitch, 0, maxXs)),
		Snap:     group("snap", lcFit(targetSnap, 1e4, maxXs*10)),
		Pounding: group("pounding", lcFit(targetPounding, 1e4, maxXs)),
	}}}
}

// lcUnlearned is a set whose sway (and so every group) is held back for a reason.
func lcUnlearned(reason string) *comfortModelSet {
	g := func(name string) comfortGroup {
		return comfortGroup{comfortGroupStatus: comfortGroupStatus{Name: name, Reason: reason}}
	}
	return &comfortModelSet{Fits: map[string]*comfortFit{lcModelID: {Sway: g("sway"), Snap: g("snap"), Pounding: g("pounding")}}}
}

type lcIn struct {
	hours    []*passageHour // index i is the stamp 8 AM + i hours; nil is absent from the forecast
	set      *comfortModelSet
	bands    *comfortBands
	helm     *float64
	noHelm   bool
	reader   *offlineCoastlineReader
	memo     *shoreRayMemo
	from     time.Time
	underway bool
	// arriveAfter moves the arrival from the departure plus one hour for each forecast stamp.
	arriveAfter time.Duration
	route       []passageRoutePoint // lcRoute() when empty
	setup       *comfortSetup       // one request's shared state, when a test runs legs in one request
	// onPredict is the setup's test hook; ctx is the request's, Background when nil.
	onPredict func()
	ctx       context.Context
}

func lcRun(t *testing.T, in lcIn) *planLegComfort {
	t.Helper()
	c, _, _ := lcRunAll(t, in)
	return c
}

func lcRunMemo(t *testing.T, in lcIn) (*planLegComfort, *shoreRayMemo) {
	t.Helper()
	c, _, memo := lcRunAll(t, in)
	return c, memo
}

// lcJudged is the hours as judged, with what the wire leaves out.
func lcJudged(t *testing.T, in lcIn) []judgedHour {
	t.Helper()
	_, hours, _ := lcRunAll(t, in)
	return hours
}

// lcWorst is the roughest band among a leg's hours, "" when none has one.
func lcWorst(c *planLegComfort) string {
	worst := ""
	for _, h := range c.Hours {
		if bandRank[h.Band] > bandRank[worst] {
			worst = h.Band
		}
	}
	return worst
}

func lcRunAll(t *testing.T, in lcIn) (*planLegComfort, []judgedHour, *shoreRayMemo) {
	t.Helper()
	hours := float64(len(in.hours))
	route := in.route
	if len(route) == 0 {
		route = lcRoute()
	}
	plan := planPassageLeg(0, passageLegArgs{Name: "A → B", Route: route, PassageHours: &hours})
	m := map[int64]passageHour{}
	var last int64
	for i, h := range in.hours {
		if h == nil {
			continue
		}
		stamp := paAt(14, 8+i, 0).Unix()
		m[stamp] = *h
		last = max(last, stamp)
	}
	as := newPassageAssessor(pwLimitsAll(), forecastSource{}, pwNoTides, pwSun)
	for _, pt := range plan.samples {
		as.cache[passagePointKey(pt)] = passagePointHours{hours: m, last: last}
	}
	set, bands, helm := in.set, lcBands(), 2.0
	if set == nil {
		set = lcSet(1)
	}
	if in.bands != nil {
		bands = *in.bands
	}
	if in.helm != nil {
		helm = *in.helm
	}
	reader := in.reader
	if reader == nil {
		reader = fetchReader(t)
	}
	memo := in.memo
	if memo == nil {
		memo = newCountedMemo(shoreRayEntries)
	}
	from := in.from
	if from.IsZero() {
		from = paAt(14, 8, 0)
	}
	helmPtr := &helm
	if in.noHelm {
		helmPtr = nil
	}
	setup := in.setup
	if setup == nil {
		setup = newComfortSetup(set, bands, helmPtr, nil, func() *coastlineLoader { return reader.newLoader() }, memo)
		setup.onPredict = in.onPredict
	}
	tl, err := spanTimeline(paAt(14, 8, 0), paAt(14, 8, 0).Add(hoursDuration(hours)).Add(in.arriveAfter))
	if err != nil {
		t.Fatal(err)
	}
	input := legComfortInput{
		comfortSetup: setup, as: as, plan: plan, tl: tl, from: from,
		underway: in.underway, loc: paLoc,
	}
	ctx := in.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	judged, blocked := input.judgedHours(ctx)
	input.endLeg()
	if blocked != "" {
		return notJudged(blocked), nil, memo
	}
	return input.comfortOf(judged), judged, memo
}

// lcMust stops a test whose leg came back without the hours it reads.
func lcMust(t *testing.T, c *planLegComfort, hours int) {
	t.Helper()
	if c == nil || len(c.Hours) < hours {
		t.Fatalf("comfort = %+v, want at least %d hours", c, hours)
	}
}

func lcFigures(h float64, period int, from float64) string {
	heading := lcHeading()
	return fmt.Sprintf("swell %.1f m %d s %s", h, period, seaWords(&heading, from))
}

// 1
func TestLegComfort_TwoLivelyHoursThenComfortable(t *testing.T) {
	c := lcRun(t, lcIn{hours: lcHours(lcHour(1, 0), lcHour(1, 0), lcHour(0.2, 0), lcHour(0.2, 0), lcHour(0.2, 0))})
	want := "Lively for the first 2 h: roll, " + lcFigures(1, 6, 0) + ". Then Comfortable."
	if c.Text != want {
		t.Fatalf("text = %q\nwant   %q", c.Text, want)
	}
	if lcWorst(c) != "lively" || len(c.Hours) != 5 {
		t.Fatalf("worst %q, %d hours", lcWorst(c), len(c.Hours))
	}
	stretches := stretchesOf(lcJudged(t, lcIn{hours: lcHours(lcHour(1, 0), lcHour(1, 0), lcHour(0.2, 0), lcHour(0.2, 0), lcHour(0.2, 0))}))
	if len(stretches) != 2 || stretches[0].band != "lively" || stretches[0].lead != "roll" || stretches[0].roughest().figures != lcFigures(1, 6, 0) {
		t.Errorf("stretches = %+v", stretches)
	}
	if c.Hours[0].Band != "lively" || c.Hours[4].Band != "comfortable" || c.Hours[0].Label != "8 AM" {
		t.Errorf("hours = %+v %+v", c.Hours[0], c.Hours[4])
	}
}

// 2
func TestLegComfort_AllComfortableSaysSoAndNothingElse(t *testing.T) {
	c := lcRun(t, lcIn{hours: lcHours(lcHour(0.2, 0), lcHour(0.2, 0), lcHour(0.2, 0))})
	if c.Text != "Comfortable." || lcWorst(c) != "comfortable" {
		t.Fatalf("comfort = %+v", c)
	}
}

// 3
func TestLegComfort_AnEnclosedHourMidLegIsAGapNotAWord(t *testing.T) {
	reader := fetchReader(t, fetchWay("n", fetchPt(-18, 3), fetchPt(18, 3))) // land three miles north
	c := lcRun(t, lcIn{reader: reader, hours: lcHours(lcHour(0.5, 90), lcHour(0.5, 90), lcHour(1, 0), lcHour(0.5, 90), lcHour(0.5, 90))})
	want := "Comfortable for the first 2 h. Then not judged for 1 h: swell not judged in enclosed water. Then Comfortable."
	if c.Text != want {
		t.Fatalf("text = %q\nwant   %q", c.Text, want)
	}
	if h := c.Hours[2]; h.Band != "" || h.Line != "10 AM · Not judged: swell not judged in enclosed water" {
		t.Errorf("enclosed hour = %+v", h)
	}
}

// 4
func TestLegComfort_BandsNotLearnedGiveNoWordButTheFigures(t *testing.T) {
	b := learnComfortBands([]ratedFelt{{ratingComfortable, 0.5}, {ratingLively, 2}})
	c := lcRun(t, lcIn{bands: &b, hours: lcHours(lcHour(0.2, 0), lcHour(1, 0), lcHour(0.2, 0))})
	if !strings.HasPrefix(c.Text, "Comfort not learned yet") || !strings.Contains(c.Text, lcFigures(1, 6, 0)) {
		t.Fatalf("text = %q", c.Text)
	}
	if strings.Contains(c.Text, "Comfortable") || lcWorst(c) != "" {
		t.Errorf("no band word before the bands are learned: %q worst %q", c.Text, lcWorst(c))
	}
	for _, h := range c.Hours {
		if h.Band != "" || !strings.Contains(h.Line, "roll ") {
			t.Errorf("hour = %+v: figures, no band", h)
		}
	}
}

// 5
func TestLegComfort_BeyondTheRecordIsSaidInBothWays(t *testing.T) {
	// (a) no Rough edge, and the sway is above every rated passage's.
	c := lcRun(t, lcIn{hours: lcHours(lcHour(2, 0), lcHour(2, 0))})
	want := "Lively throughout, rougher than any recorded passage: roll, " + lcFigures(2, 6, 0) + "."
	if c.Text != want {
		t.Fatalf("text = %q\nwant   %q", c.Text, want)
	}
	// (b) mild sway in seas steeper than anything the model learned from.
	c = lcRun(t, lcIn{set: lcSet(0.01), hours: lcHours(lcHour(0.2, 0), lcHour(0.2, 0))})
	if c.Text != "Comfortable in seas steeper than any recorded passage." || lcWorst(c) != "comfortable" || !c.Hours[0].Beyond {
		t.Fatalf("beyond-by-steepness = %+v", c)
	}
}

// 6
func TestLegComfort_ASwayThatIsGatedSaysWhyAndGivesNoWord(t *testing.T) {
	c := lcRun(t, lcIn{set: lcUnlearned("Needs 8 passages with motion and sea, has 2"), hours: lcHours(lcHour(1, 0), lcHour(1, 0))})
	want := "Comfort not judged yet: needs 8 passages with motion and sea, has 2."
	if c.Text != want || lcWorst(c) != "" {
		t.Fatalf("text = %q worst %q", c.Text, lcWorst(c))
	}
	c = lcRun(t, lcIn{set: &comfortModelSet{}, hours: lcHours(lcHour(1, 0))})
	if !strings.HasPrefix(c.Text, "Comfort not judged yet: ") {
		t.Fatalf("an empty model: %q", c.Text)
	}
}

// 7
func TestLegComfort_CoastlineOffLeavesEveryHourUnchecked(t *testing.T) {
	reader := &offlineCoastlineReader{Enabled: func() (bool, error) { return false, nil }, Store: newTestCoastlineStore(t)}
	withWind := lcHour(0.5, 0)
	withWind.WindWave = &seaTrainFigures{HeightM: 0.5, PeriodS: 4, FromDeg: 0}
	withWind.WindDirDeg = 0
	c := lcRun(t, lcIn{reader: reader, hours: lcHours(withWind, lcHour(0.5, 0))})
	lcMust(t, c, 2)
	off := "the Offline coastline is off; turn it on in Settings → Offline coastline"
	if got, want := c.Hours[0].Line, "8 AM · Not judged: wind wave not checked against fetch: "+off; got != want {
		t.Errorf("wind hour line = %q", got)
	}
	if got, want := c.Hours[1].Line, "9 AM · Not judged: swell not checked for enclosed water: "+off; got != want {
		t.Errorf("swell-only hour line = %q", got)
	}
	if lcWorst(c) != "" || strings.Contains(c.Text, "Comfortable") || strings.Contains(c.Text, "Lively") {
		t.Errorf("an unchecked sea gets no word: %q worst %q", c.Text, lcWorst(c))
	}
	for _, h := range c.Hours {
		if h.Band != "" {
			t.Errorf("hour band = %q", h.Band)
		}
	}
}

// 8
func TestLegComfort_ABayCapsTheWindWaveAndTheModelSeesTheCap(t *testing.T) {
	reader := fetchReader(t, waterRing(2.5))
	h := lcHour(0, 0)
	h.Swell = nil
	h.WindMS, h.WindDirDeg = kn(20), 0
	h.WindWave = &seaTrainFigures{HeightM: 1.4, PeriodS: 5, FromDeg: 0}
	h.WaveM = 1.4
	set := lcSet(1)
	c, judged, _ := lcRunAll(t, lcIn{set: set, reader: reader, hours: lcHours(h)})
	lcMust(t, c, 1)
	ww := judged[0].windWave
	if ww == nil || ww.Fetch == nil || ww.Fetch.Check != seaCheckCapped || ww.HeightM >= 1.4 || ww.PeriodS > 5 {
		t.Fatalf("wind wave = %+v", ww)
	}
	if !strings.Contains(ww.Note, "fetch-limited from") || !strings.Contains(c.Hours[0].Line, "wind wave "+fmt.Sprintf("%.1f m", ww.HeightM)) || !strings.Contains(c.Hours[0].Line, "(fetch-limited from") {
		t.Errorf("note = %q, line = %q", ww.Note, c.Hours[0].Line)
	}
	want := set.predict(comfortHourInput{Model: waveModelRef{ID: lcModelID}, HeadingDeg: lcHeading(), WindWave: &ww.seaTrainFigures})
	if got := judged[0].rollDeg; got == nil || math.Abs(*got-*want.RollDeg) > 1e-9 {
		t.Errorf("roll = %v, want %v (the model reads the capped figures)", got, *want.RollDeg)
	}
	// The model's own figure for the uncapped 1.4 m at 5 s is a different roll: the
	// hour was not predicted from the hindcast.
	uncapped := set.predict(comfortHourInput{Model: waveModelRef{ID: lcModelID}, HeadingDeg: lcHeading(), WindWave: h.WindWave})
	if math.Abs(*judged[0].rollDeg-*uncapped.RollDeg) < 1e-6 {
		t.Errorf("roll %v is the uncapped figure", *judged[0].rollDeg)
	}
}

func TestLegComfort_AStretchQuotesTheSystemThatAddsMostToTheLead(t *testing.T) {
	h := lcHour(0.2, 0)
	h.WindWave = &seaTrainFigures{HeightM: 1.2, PeriodS: 6, FromDeg: 0}
	h.WindDirDeg = 0
	c := lcRun(t, lcIn{hours: lcHours(h)})
	lcMust(t, c, 1)
	if !strings.Contains(c.Text, strings.Replace(lcFigures(1.2, 6, 0), "swell", "wind wave", 1)) {
		t.Fatalf("text = %q, want the wind wave's figures", c.Text)
	}
}

func TestComfortGroup_LeadNamesTheLargerPartAndTheBiggerSystem(t *testing.T) {
	set := lcSet(1)
	g := set.Fits[lcModelID].Sway
	swell := &seaTrainFigures{HeightM: 1, PeriodS: 6, FromDeg: 0}
	wind := &seaTrainFigures{HeightM: 0.2, PeriodS: 6, FromDeg: 0}
	_, parts, _ := g.at(90, swell, wind)
	if got := g.lead(parts, 90, swell, wind); got != (comfortLead{Word: "roll", System: "swell"}) {
		t.Errorf("lead = %+v", got)
	}
	_, parts, _ = g.at(90, wind, swell)
	if got := g.lead(parts, 90, wind, swell); got != (comfortLead{Word: "roll", System: "wind wave"}) {
		t.Errorf("lead = %+v", got)
	}
}

// 9
func TestFetchGeoMemo_KeepsBuildsAcrossRequestsAndRebuildsWhenTheShoreChanges(t *testing.T) {
	reader := fetchReader(t)
	memo := newCountedMemo(shoreRayEntries)
	hours := lcHours(lcHour(0.5, 0), lcHour(0.5, 0))
	lcRunMemo(t, lcIn{reader: reader, memo: memo, hours: hours})
	first := memo.builtCount()
	if first == 0 {
		t.Fatal("the first assessment built no geometry")
	}
	lcRunMemo(t, lcIn{reader: reader, memo: memo, hours: hours})
	if got := memo.builtCount(); got != first {
		t.Fatalf("a second assessment built %d more", got-first)
	}
	// A square downloaded again is a new stamp, so a new key.
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
	if got := memo.builtCount(); got <= first {
		t.Fatalf("a changed square rebuilt nothing: %d builds", got)
	}
}

func TestShoreRayMemo_ConcurrentRequestsForOneGeometryBuildItOnce(t *testing.T) {
	memo := newCountedMemo(8)
	var wg sync.WaitGroup
	got := make([]*fetchGeometry, 16)
	for i := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g, err := memo.geometry(context.Background(), shoreSpotKey{latE3: 1, stamp: "s"}, func() (*fetchGeometry, error) {
				time.Sleep(20 * time.Millisecond)
				return &fetchGeometry{}, nil
			})
			if err != nil {
				t.Error(err)
			}
			got[i] = g
		}()
	}
	wg.Wait()
	if memo.builtCount() != 1 {
		t.Fatalf("builds = %d", memo.builtCount())
	}
	for _, g := range got {
		if g == nil || g != got[0] {
			t.Fatal("requests got different geometries")
		}
	}
}

func TestShoreRayMemo_AFailedBuildIsNotKept(t *testing.T) {
	memo := newCountedMemo(8)
	k := shoreSpotKey{latE3: 1, stamp: "s"}
	if _, err := memo.geometry(context.Background(), k, func() (*fetchGeometry, error) { return nil, errors.New("no lines") }); err == nil {
		t.Fatal("the failure was swallowed")
	}
	g, err := memo.geometry(context.Background(), k, func() (*fetchGeometry, error) { return &fetchGeometry{}, nil })
	if err != nil || g == nil {
		t.Fatalf("a failure was kept: %v %v", g, err)
	}
}

// 10
func TestLegComfort_AnHourThatCannotBeReadIsAReasonNeverComfortable(t *testing.T) {
	cases := []struct {
		name  string
		hours []*passageHour
		set   *comfortModelSet
		want  string
		lead  string
	}{
		{"no split", func() []*passageHour {
			h := lcHour(0, 0)
			h.Swell, h.WaveM = nil, 1.0
			return lcHours(h)
		}(), nil, "gave no swell and wind-wave split", "Not judged: "},
	}
	// A model that cannot judge the leg is the whole text, with no hours to list.
	old := lcHour(0.2, 0)
	old.WaveModel = waveModelRef{}
	if got := lcRun(t, lcIn{hours: lcHours(old)}); !strings.HasPrefix(got.Text, "Comfort not judged yet: ") || !strings.Contains(got.Text, "the wave plugin doesn't name its wave model") || len(got.Hours) != 0 {
		t.Errorf("old plugin: %+v", got)
	}
	mismatch := lcHour(0.2, 0)
	mismatch.WaveModel = waveModelRef{ID: "other", Label: "Another model"}
	if got := lcRun(t, lcIn{hours: lcHours(mismatch)}); !strings.HasPrefix(got.Text, "Comfort not judged yet: ") || !strings.Contains(got.Text, "was learned on test_wave") || len(got.Hours) != 0 {
		t.Errorf("model mismatch: %+v", got)
	}
	for _, c := range cases {
		got := lcRun(t, lcIn{hours: c.hours, set: c.set})
		lcMust(t, got, 1)
		if !strings.HasPrefix(got.Text, c.lead) || !strings.Contains(got.Text, c.want) || strings.Contains(got.Text, "Comfortable") || lcWorst(got) != "" {
			t.Errorf("%s: text = %q worst %q", c.name, got.Text, lcWorst(got))
		}
		if got.Hours[0].Band != "" || !strings.Contains(got.Hours[0].Line, " · Not judged: ") {
			t.Errorf("%s: hour = %+v", c.name, got.Hours[0])
		}
	}
	// An hour the forecast has none of, inside a leg that is otherwise judged.
	got := lcRun(t, lcIn{hours: []*passageHour{ptr(lcHour(0.2, 0)), nil, ptr(lcHour(0.2, 0))}})
	lcMust(t, got, 3)
	if h := got.Hours[1]; h.Band != "" || !strings.Contains(h.Line, " · Not judged: ") {
		t.Fatalf("missing hour = %+v", h)
	}
	if !strings.Contains(got.Text, "not judged for 1 h") {
		t.Errorf("text = %q", got.Text)
	}
}

// 12
func TestLegComfort_ALegUnderWayIsReadFromNowAndSaysSo(t *testing.T) {
	c := lcRun(t, lcIn{underway: true, from: paAt(14, 10, 30),
		hours: lcHours(lcHour(0.2, 0), lcHour(0.2, 0), lcHour(1, 0), lcHour(1, 0), lcHour(0.2, 0))})
	want := "Lively for the next 1 h 30 min: roll, " + lcFigures(1, 6, 0) + ". Then Comfortable."
	lcMust(t, c, 1)
	if c.Text != want || len(c.Hours) != 3 || c.Hours[0].Label != "10 AM" {
		t.Fatalf("text = %q, %d hours, first %q", c.Text, len(c.Hours), c.Hours[0].Label)
	}
	// Past its ETA the leg has no span left, and says so.
	r := planComfortRun{env: planEnv{now: paAt(14, 13, 30)}, ready: func() (*comfortSetup, string) { return &comfortSetup{}, "" }}
	c = r.leg(context.Background(), &planLegWork{underway: true, windReady: true, eta: paAt(14, 13, 0), pace: linearPace(5), fromLoc: paLoc})
	if c == nil || c.Text != "The leg is under way and its ETA has passed." || len(c.Hours) != 0 {
		t.Fatalf("ETA passed = %+v", c)
	}
}

func TestWalkLegHours_ReckonsPositionOverTheRealSpanNotTheSpanLeft(t *testing.T) {
	hours := 5.0
	plan := planPassageLeg(0, passageLegArgs{Name: "A → B", Route: lcRoute(), PassageHours: &hours})
	m := map[int64]passageHour{}
	for i := range 5 {
		m[paAt(14, 8+i, 0).Unix()] = lcHour(0.2, 0)
	}
	as := newPassageAssessor(pwLimitsAll(), forecastSource{}, pwNoTides, pwSun)
	for _, pt := range plan.samples {
		as.cache[passagePointKey(pt)] = passagePointHours{hours: m, last: paAt(14, 12, 0).Unix()}
	}
	var got []legHour
	tl, err := spanTimeline(paAt(14, 8, 0), paAt(14, 13, 0))
	if err != nil {
		t.Fatal(err)
	}
	err = as.walkLegHours(plan, tl, paAt(14, 10, 30), paAt(14, 13, 0), func(h legHour) error {
		got = append(got, h)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || !got[0].t.Equal(paAt(14, 10, 0)) || !got[2].t.Equal(paAt(14, 12, 0)) {
		t.Fatalf("stamps = %v", got)
	}
	lat, lon, _ := plan.at(2.0 / 5.0)
	if got[0].lat != lat || got[0].lon != lon || !got[0].have {
		t.Errorf("first position %v,%v want %v,%v (a fifth of the way at 10 AM of a leg that left at 8)", got[0].lat, got[0].lon, lat, lon)
	}
	// The cell's walk includes the stamp of the arrival: it asks for one hour past it.
	got = nil
	as.walkLegHours(plan, tl, paAt(14, 8, 0), paAt(14, 14, 0), func(h legHour) error {
		got = append(got, h)
		return nil
	})
	if len(got) != 6 {
		t.Errorf("the cell's walk yielded %d stamps, want 6", len(got))
	}
}

// 14
func TestLegComfort_TheSecondLineGivesSnapAndPoundingOrTheirReason(t *testing.T) {
	hours := lcHours(lcHour(1, 0), lcHour(1, 90), lcHour(0.5, 45))
	c, judged, _ := lcRunAll(t, lcIn{hours: hours})
	var snap, pound float64
	for _, h := range judged {
		if h.snapG == nil || h.poundingG == nil {
			t.Fatalf("hour = %+v", h)
		}
		snap, pound = math.Max(snap, *h.snapG), math.Max(pound, *h.poundingG)
	}
	want := fmt.Sprintf("Snap up to %.2f g at the helm · pounding up to %.2f g", snap, pound)
	if snap == 0 || pound == 0 || c.Motion != want {
		t.Fatalf("motion = %q, want %q", c.Motion, want)
	}
	// A blank helm height says why, and pounding still shows.
	c = lcRun(t, lcIn{noHelm: true, hours: hours})
	if !strings.Contains(c.Motion, "helm height") || !strings.Contains(c.Motion, "pounding up to") || strings.Contains(c.Motion, "Snap up to") {
		t.Errorf("no helm height: %q", c.Motion)
	}
	// Snap and pounding held back for one reason say it once.
	set := lcSet(1)
	f := set.Fits[lcModelID]
	f.Snap = comfortGroup{comfortGroupStatus: comfortGroupStatus{Name: "snap", Reason: "needs 5 passages with 10 Hz motion, has 1"}}
	f.Pounding = comfortGroup{comfortGroupStatus: comfortGroupStatus{Name: "pounding", Reason: "needs 5 passages with 10 Hz motion, has 1"}}
	c = lcRun(t, lcIn{set: set, hours: hours})
	if want := "Snap and pounding not shown: needs 5 passages with 10 Hz motion, has 1"; c.Motion != want {
		t.Errorf("shared reason: %q, want %q", c.Motion, want)
	}
	if lcWorst(c) == "" {
		t.Error("the sway still has its word")
	}
}

// 16
func TestLegComfort_APositionChartedAshoreIsAnHonestGap(t *testing.T) {
	reader := fetchReader(t, landRing(4))
	c := lcRun(t, lcIn{reader: reader, hours: lcHours(lcHour(0.5, 0), lcHour(0.5, 0))})
	lcMust(t, c, 1)
	if c.Text != "Not judged: position charted ashore." || c.Hours[0].Band != "" || c.Hours[0].Line != "8 AM · Not judged: position charted ashore" {
		t.Fatalf("text = %q hour %+v", c.Text, c.Hours[0])
	}
}

func TestLegComfort_MoreThanThreeStretchesReadAsAtTimes(t *testing.T) {
	c, judged, _ := lcRunAll(t, lcIn{hours: lcHours(lcHour(0.2, 0), lcHour(1, 0), lcHour(0.2, 0), lcHour(1, 0), lcHour(0.2, 0))})
	if n := len(stretchesOf(judged)); n != 5 {
		t.Fatalf("stretches = %d", n)
	}
	want := "Lively at times; roughest from 9 AM for 1 h: roll, " + lcFigures(1, 6, 0) + "."
	if c.Text != want {
		t.Fatalf("text = %q\nwant   %q", c.Text, want)
	}
}

// ── through the plan assessment ──────────────────────────────────────────

// lcForecastOf is a forecast of every hour of the 14th to the 24th, the hour at h as
// at(h) gives it.
func lcForecastOf(at func(h time.Time) passageHour) forecastSource {
	hours := map[int64]passageHour{}
	for h := paAt(14, 0, 0); !h.After(paAt(24, 23, 0)); h = h.Add(time.Hour) {
		hours[h.Unix()] = at(h)
	}
	return forecastSource{Provider: "wx", WaveProvider: "waves", Fetch: func(context.Context, float64, float64) (passageForecast, error) {
		return passageForecast{Hours: hours}, nil
	}}
}

// lcForecast is a week of mild weather with a small swell, each hour naming its model.
func lcForecast() forecastSource {
	return lcForecastOf(func(time.Time) passageHour {
		x := lcHour(0.2, 100)
		x.WindDirDeg = 190
		return x
	})
}

func lcDeps(t *testing.T, f *paFake, modelCalls *atomic.Int32) planAssessDeps {
	t.Helper()
	d := paDeps(f)
	reader := &offlineCoastlineReader{Enabled: func() (bool, error) { return false, nil }, Store: newTestCoastlineStore(t)}
	set, bands := lcSet(1), lcBands()
	d.comfort = &planComfortDeps{
		model: func() (*comfortModelSet, error) {
			modelCalls.Add(1)
			return set, nil
		},
		bands:      func() (comfortBands, error) { return bands, nil },
		helmHeight: func() (*float64, error) { return ptr(2.0), nil },
		loader:     func() *coastlineLoader { return reader.newLoader() },
		memo:       newCountedMemo(shoreRayEntries),
	}
	return d
}

// 11
func TestAssessPlan_ComfortIsOnlyLookedForWhenAskedAndNeverInAVerdictsOnlyRun(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	p.Stops[0].DepartTime = "08:00"
	var calls atomic.Int32
	f := &paFake{fc: lcForecast()}
	d := lcDeps(t, f, &calls)

	plain := assessPlan(context.Background(), p, legs, d, planAssessOptions{})
	if plain.Legs[0].Comfort != nil || calls.Load() != 0 {
		t.Fatalf("comfort without being asked for: %+v (%d model reads)", plain.Legs[0].Comfort, calls.Load())
	}
	raw, err := json.Marshal(plain.Legs[0])
	if err != nil || !strings.Contains(string(raw), `"comfort":null`) {
		t.Fatalf("a leg not looked at carries comfort:null: %s %v", raw, err)
	}

	verdicts := assessPlan(context.Background(), p, legs, d, planAssessOptions{VerdictsOnly: true, Comfort: true})
	if verdicts.Legs[0].Comfort != nil || calls.Load() != 0 {
		t.Fatalf("verdicts-only read comfort: %+v", verdicts.Legs[0].Comfort)
	}

	asked := assessPlan(context.Background(), p, legs, d, planAssessOptions{Comfort: true})
	if asked.Legs[0].Comfort == nil || asked.Legs[0].Comfort.Text == "" || asked.Legs[0].Verdict != plain.Legs[0].Verdict {
		t.Fatalf("asked for: %+v", asked.Legs[0])
	}
	if !equalJSON(t, plain.Legs[0].WindChanges, asked.Legs[0].WindChanges) {
		t.Error("asking for comfort changed the wind changes")
	}

	// No comfort deps at all is comfort off, for a caller that never asks.
	d.comfort = nil
	if got := assessPlan(context.Background(), p, legs, d, planAssessOptions{Comfort: true}); got.Legs[0].Comfort != nil {
		t.Errorf("comfort off still gave comfort: %+v", got.Legs[0].Comfort)
	}
}

// F1: asking for comfort with readers that are not wired is an error on the leg, not a
// leg that quietly has none.
func TestAssessPlan_AskingForComfortWithUnwiredReadersIsSaidNotSilent(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	p.Stops[0].DepartTime = "08:00"
	var calls atomic.Int32
	d := lcDeps(t, &paFake{fc: lcForecast()}, &calls)
	d.comfort = &planComfortDeps{}
	got := assessPlan(context.Background(), p, legs, d, planAssessOptions{Comfort: true})
	c := got.Legs[0].Comfort
	if c == nil || !strings.HasPrefix(c.Text, "Comfort not judged yet: ") || !strings.Contains(c.Text, "not wired") {
		t.Fatalf("unwired readers = %+v", c)
	}

	// A reader that fails, such as the passage store not running, is said the same way.
	d = lcDeps(t, &paFake{fc: lcForecast()}, &calls)
	d.comfort.model = func() (*comfortModelSet, error) { return nil, errPassageStoreNotRunning }
	got = assessPlan(context.Background(), p, legs, d, planAssessOptions{Comfort: true})
	if c := got.Legs[0].Comfort; c == nil || c.Text != "Comfort not judged yet: the comfort model couldn't be read: the passage store is not running." {
		t.Fatalf("failing model reader = %+v", c)
	}
}

func equalJSON(t *testing.T, a, b any) bool {
	t.Helper()
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// 13
func TestAssessPlan_ComfortIsWorkedOutOnceForEachLegAtTheChosenDeparture(t *testing.T) {
	p, legs := paPlan(t, "2026-11-14", 0, 1, 0)
	var calls atomic.Int32
	f := &paFake{fc: lcForecast(), departures: func(q planDepartureQuery) ([]tidalDeparture, error) {
		var out []tidalDeparture
		for i := range 5 {
			out = append(out, tidalDeparture{Depart: q.Earliest.Add(time.Duration(i+1) * time.Hour), Current: planCurrentReading{Have: true, MeanKn: 0.4}})
		}
		return out, nil
	}}
	d := lcDeps(t, f, &calls)
	got := assessPlan(context.Background(), p, legs, d, planAssessOptions{Comfort: true})
	judged := 0
	for _, l := range got.Legs {
		if l.Comfort == nil {
			continue
		}
		judged++
		lcMust(t, l.Comfort, 1)
		depart := paParse(t, l.Depart)
		if first := paParse(t, l.Comfort.Hours[0].At); !first.Equal(depart.Truncate(time.Hour)) {
			t.Errorf("comfort starts %v, the chosen departure is %v", first, depart)
		}
	}
	if judged == 0 || calls.Load() != 1 {
		t.Fatalf("%d legs with comfort, %d model reads: the model is read once for the request, not for each of five candidates", judged, calls.Load())
	}
}

// BenchmarkLegComfortThreeLegs judges three legs over the Whitsundays' real coast,
// each hour with a swell and a wind wave, as the plan page's assessment does. cold
// builds every shore geometry, warm reads them from the memo. A request makes its
// own loader either way, so the squares' records are read each time.
func BenchmarkLegComfortThreeLegs(b *testing.B) {
	legs := [][2]shorelinePoint{{coralSeaMarina, haslewood}, {haslewood, cidHarbour}, {cidHarbour, goldsmithAnchorage}}
	reader := whitsundaysReader(b, legs)
	set, bands, helm := lcSet(1), lcBands(), 2.0
	hour := lcHour(0.5, 120)
	hour.WindMS, hour.WindDirDeg = kn(15), 110
	hour.WindWave = &seaTrainFigures{HeightM: 0.6, PeriodS: 4, FromDeg: 110}
	hours := map[int64]passageHour{}
	for i := range 24 {
		hours[paAt(14, i, 0).Unix()] = hour
	}
	type prepared struct {
		plan  passageLegPlan
		hours float64
	}
	var plans []prepared
	as := newPassageAssessor(pwLimitsAll(), forecastSource{}, pwNoTides, pwSun)
	for _, leg := range legs {
		nm := haversineMeters(leg[0].Lat, leg[0].Lon, leg[1].Lat, leg[1].Lon) / metersPerNauticalMile
		h := math.Max(2, math.Ceil(nm/6))
		plan := planPassageLeg(0, passageLegArgs{Name: "leg", Route: []passageRoutePoint{{Lat: leg[0].Lat, Lon: leg[0].Lon}, {Lat: leg[1].Lat, Lon: leg[1].Lon}}, PassageHours: &h})
		for _, pt := range plan.samples {
			as.cache[passagePointKey(pt)] = passagePointHours{hours: hours, last: paAt(14, 23, 0).Unix()}
		}
		plans = append(plans, prepared{plan, h})
	}
	request := func(memo *shoreRayMemo) {
		setup := newComfortSetup(set, bands, &helm, nil, func() *coastlineLoader { return reader.newLoader() }, memo)
		for _, p := range plans {
			tl, err := spanTimeline(paAt(14, 8, 0), paAt(14, 8, 0).Add(hoursDuration(p.hours)))
			if err != nil {
				b.Fatal(err)
			}
			c := legComfort(context.Background(), legComfortInput{
				comfortSetup: setup, as: as, plan: p.plan, tl: tl,
				from: paAt(14, 8, 0), loc: paLoc,
			})
			if c.Text == "" || len(c.Hours) == 0 {
				b.Fatalf("no comfort: %+v", c)
			}
		}
	}
	b.Run("cold", func(b *testing.B) {
		for range b.N {
			request(newShoreRayMemo(shoreRayEntries))
		}
	})
	b.Run("warm", func(b *testing.B) {
		memo := newCountedMemo(shoreRayEntries)
		request(memo)
		if memo.builtCount() == 0 {
			b.Fatal("no shore geometry was built, so the benchmark measures nothing")
		}
		b.ResetTimer()
		for range b.N {
			request(memo)
		}
	})
}

// whitsundaysReader holds the Whitsundays' real coast, and open water in every other
// square within reach of the legs' ends.
func whitsundaysReader(b *testing.B, legs [][2]shorelinePoint) *offlineCoastlineReader {
	b.Helper()
	lines := loadShorelineFile(b, "testdata/water_route/whitsundays.json.gz")
	own, err := squareContaining(haslewood.Lat, haslewood.Lon)
	if err != nil {
		b.Fatal(err)
	}
	var ends [][2]float64
	for _, leg := range legs {
		for _, p := range leg {
			ends = append(ends, [2]float64{p.Lat, p.Lon})
		}
	}
	store := seedCoastlineAlong(b, coastlineTestAt, ends, func(sq coverageSquare) []shorelineLine {
		if sq == own {
			return lines
		}
		return nil
	})
	return &offlineCoastlineReader{Enabled: func() (bool, error) { return true, nil }, Store: store}
}

// BenchmarkRankComfort ranks one leg's real candidate set: a departure every 15
// minutes from 6 AM to 5.45 PM, 48 of them, each judged hour by hour over the
// Whitsundays' coast. Cold builds every geometry its rays need; warm reads every ray
// from the memo and builds none (the builds/op figure).
func BenchmarkRankComfort(b *testing.B) {
	benchRank(b, [][2]shorelinePoint{{coralSeaMarina, haslewood}}) // marina to marina, as BenchmarkLegComfortThreeLegs
}

// BenchmarkRankComfortThreeLegs is the same for the three legs of
// BenchmarkLegComfortThreeLegs, ranked one after another in one request.
func BenchmarkRankComfortThreeLegs(b *testing.B) {
	benchRank(b, [][2]shorelinePoint{{coralSeaMarina, haslewood}, {haslewood, cidHarbour}, {cidHarbour, goldsmithAnchorage}})
}

func benchRank(b *testing.B, legs [][2]shorelinePoint) {
	reader := whitsundaysReader(b, legs)
	set, bands, helm := lcSet(1), lcBands(), 2.0
	hour := lcHour(0.5, 120)
	hour.WindMS, hour.WindDirDeg = kn(15), 110
	hour.WindWave = &seaTrainFigures{HeightM: 0.6, PeriodS: 4, FromDeg: 110}
	hours := map[int64]passageHour{}
	for t := paAt(14, 0, 0); t.Before(paAt(15, 12, 0)); t = t.Add(time.Hour) {
		hours[t.Unix()] = hour
	}
	as := newPassageAssessor(pwLimitsAll(), forecastSource{}, pwNoTides, pwSun)
	type rankedLeg struct {
		w      *planLegWork
		choice *departureChoice
	}
	var work []rankedLeg
	for _, leg := range legs {
		nm := haversineMeters(leg[0].Lat, leg[0].Lon, leg[1].Lat, leg[1].Lon) / metersPerNauticalMile
		legHours := math.Max(2, math.Ceil(nm/6))
		plan := planPassageLeg(0, passageLegArgs{Name: "leg", Route: []passageRoutePoint{{Lat: leg[0].Lat, Lon: leg[0].Lon}, {Lat: leg[1].Lat, Lon: leg[1].Lon}}, PassageHours: &legHours})
		for _, pt := range plan.samples {
			as.cache[passagePointKey(pt)] = passagePointHours{hours: hours, last: paAt(15, 11, 0).Unix()}
		}
		choice := &departureChoice{}
		for d := paAt(14, 6, 0); !d.After(paAt(14, 17, 45)); d = d.Add(15 * time.Minute) {
			tl, err := plan.timeline(d)
			if err != nil {
				b.Fatal(err)
			}
			choice.cands = append(choice.cands, tidalDeparture{Depart: d, tl: tl})
			choice.cells = append(choice.cells, passageWindowCell{State: "go"})
		}
		work = append(work, rankedLeg{&planLegWork{plan: plan, fromLoc: paLoc}, choice})
	}
	newRanking := func(memo *shoreRayMemo) *departureRanking {
		setup := newComfortSetup(set, bands, &helm, nil, func() *coastlineLoader { return reader.newLoader() }, memo)
		return &departureRanking{run: planComfortRun{as: as}, setup: setup}
	}
	// The candidates ranked are counted once, outside the timing.
	candidates, ranked := 0, 0
	rk := newRanking(newShoreRayMemo(shoreRayEntries))
	for _, l := range work {
		for i := range l.choice.cands {
			candidates++
			if _, hours, why := l.choice.judge(context.Background(), rk, l.w, i); why == "" {
				if _, why = rankKeyOf(hours); why == "" {
					ranked++
				}
			}
		}
		rk.setup.endLeg()
	}
	// A leg whose every candidate has swell not judged mid-leg (enclosed water) is
	// not ranked, and says so; every candidate is still judged.
	legsRanked := 0
	request := func(memo *shoreRayMemo) {
		rk := newRanking(memo)
		legsRanked = 0
		for _, l := range work {
			_, basis, chip := l.choice.rank(context.Background(), rk, l.w)
			switch {
			case strings.HasPrefix(basis, "Chosen for comfort: ") && chip != nil:
				legsRanked++
			case !strings.HasPrefix(basis, "Not chosen for comfort: "):
				b.Fatalf("no basis: %q", basis)
			}
		}
	}
	b.Run("cold", func(b *testing.B) {
		var last *shoreRayMemo
		for range b.N {
			last = newCountedMemo(shoreRayEntries)
			request(last)
		}
		b.ReportMetric(float64(candidates), "candidates")
		b.ReportMetric(float64(ranked), "ranked")
		b.ReportMetric(float64(legsRanked), "legs-ranked")
		b.ReportMetric(float64(last.builtCount()), "builds/op")
		b.ReportMetric(float64(last.held()), "memo-entries")
	})
	b.Run("warm", func(b *testing.B) {
		memo := newCountedMemo(shoreRayEntries)
		request(memo)
		primed := memo.builtCount()
		if primed == 0 {
			b.Fatal("no shore geometry was built, so the benchmark measures nothing")
		}
		b.ResetTimer()
		for range b.N {
			request(memo)
		}
		b.ReportMetric(float64(memo.builtCount()-primed)/float64(b.N), "builds/op")
		b.ReportMetric(float64(memo.held()), "memo-entries")
	})
}

func TestPlanAssessDepsFromTool_WiresEveryComfortReader(t *testing.T) {
	d := planAssessDepsFromTool(assistantToolDeps{}, "settings.yaml")
	if d.comfort == nil || d.comfort.unwired() != "" {
		t.Fatalf("comfort readers not wired: %+v", d.comfort)
	}
	if d.comfort.memo == nil {
		t.Fatal("the shore geometry memo is not wired")
	}
}
