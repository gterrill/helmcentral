package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// Review pass 1 on the comfort chip on a plan leg (ADR 0302).

const lcOutOfTime = "the comfort check ran out of time"

// 1: a model that is held back is the reason, whatever else is wrong with the hour. An
// hour from an old plugin has no wave model label, and a hole in the forecast has no
// figures, but the operator's fix is the model's.
func TestLegComfort_AModelHeldBackIsTheReasonBeforeWhatIsWrongWithTheHour(t *testing.T) {
	cases := []struct {
		name string
		set  *comfortModelSet
		want string
	}{
		{"sway gated", lcUnlearned("Needs 8 passages with motion and sea, has 2"), "Comfort not judged yet: needs 8 passages with motion and sea, has 2."},
		{"fit failed", &comfortModelSet{Err: "the record is unreadable"}, notJudgedText("The comfort model couldn't be worked out: the record is unreadable.")},
		{"nothing learned", &comfortModelSet{}, notJudgedText(comfortReasonNoLearn)},
	}
	for _, c := range cases {
		old := lcHour(1, 0)
		old.WaveModel = waveModelRef{} // an old plugin names no wave model
		got := lcRun(t, lcIn{set: c.set, hours: []*passageHour{&old, nil, ptr(lcHour(1, 0))}})
		if got.Text != c.want {
			t.Errorf("%s: text = %q\nwant %q", c.name, got.Text, c.want)
		}
		for i, h := range got.Hours {
			if strings.Contains(h.Line, "plugin") || strings.Contains(h.Line, "no figure") {
				t.Errorf("%s: hour %d reads the hour's trouble, not the model's: %q", c.name, i, h.Line)
			}
		}
	}
	var nilSet *comfortModelSet
	if got := nilSet.heldBack(); got == "" {
		t.Error("a model that was never worked out is not held back")
	}
}

// 2: the leg's comfort stops when the request's time is up, and says so for every hour
// it did not reach.
func TestLegComfort_StopsAtTheRequestDeadlineAndSaysSoForTheHoursItDidNotReach(t *testing.T) {
	hours := func() []*passageHour {
		return lcHours(lcHour(0.2, 0), lcHour(0.2, 0), lcHour(0.2, 0), lcHour(0.2, 0), lcHour(0.2, 0))
	}
	// Already out of time: no hour is judged, and every one is accounted for.
	done, cancel := context.WithCancel(context.Background())
	cancel()
	predicted := 0
	c := lcRun(t, lcIn{ctx: done, onPredict: func() { predicted++ }, hours: hours()})
	if predicted != 0 || len(c.Hours) != 5 {
		t.Fatalf("predicted %d, %d hours", predicted, len(c.Hours))
	}
	for i, h := range c.Hours {
		if !strings.HasSuffix(h.Line, "Not judged: "+lcOutOfTime) {
			t.Errorf("hour %d = %q", i, h.Line)
		}
	}
	if want := "Not judged: " + lcOutOfTime + "."; c.Text != want {
		t.Errorf("text = %q, want %q", c.Text, want)
	}

	// Out of time after two hours.
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	n := 0
	c = lcRun(t, lcIn{ctx: ctx, onPredict: func() {
		if n++; n == 2 {
			stop()
		}
	}, hours: hours()})
	want := "Comfortable for the first 2 h. Then not judged for 3 h: " + lcOutOfTime + "."
	if len(c.Hours) != 5 || c.Text != want {
		t.Fatalf("text = %q (%d hours), want %q", c.Text, len(c.Hours), want)
	}
	if c.Hours[1].Band != "comfortable" || c.Hours[2].Band != "" {
		t.Errorf("bands = %q %q", c.Hours[1].Band, c.Hours[2].Band)
	}
}

// 2: a reader waiting for another request's build stops when its own request is done.
func TestFetchGeoMemo_AWaiterStopsWaitingWhenItsRequestIsDone(t *testing.T) {
	memo := newCountedMemo(8)
	k := fetchGeoMemoKey{latE3: 1, stamp: "s"}
	release, started := make(chan struct{}), make(chan struct{})
	defer close(release)
	go memo.get(context.Background(), k, func() (*fetchGeometry, error) {
		close(started)
		<-release
		return &fetchGeometry{}, nil
	})
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := memo.get(ctx, k, func() (*fetchGeometry, error) { return nil, errors.New("must not build") })
		result <- err
	}()
	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want the deadline", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the waiter was still waiting long after its request's deadline")
	}
}

// 2: a build another request abandoned for its own deadline is not this request's
// failure; the waiter builds the geometry itself.
func TestFetchGeoMemo_AWaiterDoesNotInheritAnotherRequestsDeadline(t *testing.T) {
	memo := newCountedMemo(8)
	k := fetchGeoMemoKey{latE3: 1, stamp: "s"}
	builder, abandon := context.WithCancel(context.Background())
	started := make(chan struct{})
	go memo.get(builder, k, func() (*fetchGeometry, error) {
		close(started)
		<-builder.Done()
		return nil, builder.Err()
	})
	<-started
	type result struct {
		g   *fetchGeometry
		err error
	}
	got := make(chan result, 1)
	go func() {
		g, err := memo.get(context.Background(), k, func() (*fetchGeometry, error) { return &fetchGeometry{}, nil })
		got <- result{g, err}
	}()
	time.Sleep(20 * time.Millisecond)
	abandon()
	select {
	case r := <-got:
		if r.err != nil || r.g == nil {
			t.Fatalf("the waiter inherited %v", r.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the waiter never finished")
	}
}

// 3, 9: a key put twice keeps one entry, at the newer weight.
func TestWeightedLRU_APutOfAKeyAlreadyHeldReplacesItAndKeepsTheBudgetRight(t *testing.T) {
	lru := newWeightedLRU[string, int](10, func(v int) int { return v })
	lru.put("a", 3)
	lru.put("a", 5)
	if lru.used != 5 || lru.order.Len() != 1 {
		t.Fatalf("used %d in %d entries after putting a twice, want 5 in 1", lru.used, lru.order.Len())
	}
	if v, _ := lru.get("a"); v != 5 {
		t.Errorf("a = %d, want the newer value", v)
	}
	lru.put("b", 4)
	if lru.used != 9 || lru.order.Len() != 2 {
		t.Fatalf("used %d in %d entries, want 9 in 2", lru.used, lru.order.Len())
	}
	lru.put("a", 8) // a is the newest again; b is the one pushed out
	if _, ok := lru.get("b"); ok || lru.used != 8 || lru.order.Len() != 1 {
		t.Errorf("used %d in %d entries, b kept %v; want 8 in 1, b gone", lru.used, lru.order.Len(), ok)
	}
}

// 3, 9: loaders of two requests that both miss a square put it twice; the budget
// counts it once.
func TestSquareLinesCache_LoadersMissingTheSameSquareTogetherCountItOnce(t *testing.T) {
	reader := fetchReader(t, fetchWay("n", fetchPt(-18, 3), fetchPt(18, 3)))
	cache := newFetchGeoMemo(fetchGeoBudgetSegments).lines
	needed, err := squaresWithin(fetchTestLat, fetchTestLon, shelterMaxFetchNm)
	if err != nil {
		t.Fatal(err)
	}
	var sq coverageSquare
	var want int
	for _, s := range needed {
		if squareInland(s) {
			continue
		}
		lines, err := reader.Store.Lines([]coverageSquare{s})
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range lines {
			want += len(l.Points)
		}
		if want > 0 {
			sq = s
			break
		}
	}
	if want == 0 {
		t.Fatal("no held square has a line: the test would prove nothing")
	}
	const loaders = 8
	var ready, start sync.WaitGroup
	ready.Add(loaders)
	start.Add(1)
	var done sync.WaitGroup
	for range loaders {
		l := reader.newLoader()
		l.shared = cache
		if err := l.ready(); err != nil {
			t.Fatal(err)
		}
		done.Add(1)
		go func() {
			defer done.Done()
			ready.Done()
			start.Wait()
			if _, err := l.squareLines(sq); err != nil {
				t.Error(err)
			}
		}()
	}
	ready.Wait()
	start.Done()
	done.Wait()
	if cache.lru.used != want || cache.lru.order.Len() != 1 {
		t.Fatalf("the cache counts %d points in %d entries, want %d in 1", cache.lru.used, cache.lru.order.Len(), want)
	}
}

// 4: a gap always carries a reason, even when the model's own reason came back empty,
// so a stretch is a gap exactly when its hours are, and a leg of more than three
// stretches can word it.
func TestLegComfort_AGapAlwaysCarriesAReasonAndAnyLegOfManyStretchesCanBeWorded(t *testing.T) {
	// Hours the forecast has none of, between judged ones.
	in := lcIn{hours: []*passageHour{ptr(lcHour(0.2, 0)), nil, ptr(lcHour(1, 0)), nil, ptr(lcHour(0.2, 0))}}
	c := lcRun(t, in)
	if !strings.HasPrefix(c.Text, "Lively at times") {
		t.Errorf("text = %q", c.Text)
	}
	for _, bad := range []string{": .", ": ,", "yet: "} {
		if strings.HasSuffix(c.Text, bad) || strings.Contains(c.Text, bad+" ") || strings.HasSuffix(c.Text, bad+".") {
			t.Errorf("a gap with no reason reached the wire in %q", c.Text)
		}
	}
	for i, h := range c.Hours {
		if strings.HasSuffix(strings.TrimSpace(h.Line), "Not judged:") {
			t.Errorf("hour %d has no reason: %q", i, h.Line)
		}
	}
	for _, h := range lcJudged(t, in) {
		if h.gap() != (h.swayDeg == nil) || h.gap() != (h.reason != "") {
			t.Errorf("hour %s: gap %v, sway nil %v, reason %q", h.label, h.gap(), h.swayDeg == nil, h.reason)
		}
	}
	for _, s := range stretchesOf(lcJudged(t, in)) {
		if s.gap() != s.hours[0].gap() {
			t.Errorf("stretch gap %v but its first hour's is %v", s.gap(), s.hours[0].gap())
		}
	}
	// The same through a model held back with no words for the whole leg.
	all := lcRun(t, lcIn{set: lcUnlearned(""), hours: lcHours(lcHour(1, 0), lcHour(1, 0))})
	if !strings.HasPrefix(all.Text, "Comfort not judged yet: ") || strings.HasSuffix(all.Text, "yet: .") || len(all.Text) < len("Comfort not judged yet: x.") {
		t.Errorf("text = %q", all.Text)
	}
}

// 5: a leg of many stretches that is Comfortable where judged still says some of it
// was in seas steeper than anything recorded.
func TestLegComfort_ASummaryKeepsTheSteeperThanRecordedClause(t *testing.T) {
	reader := fetchReader(t, fetchWay("n", fetchPt(-18, 3), fetchPt(18, 3))) // land three miles north
	calm, enclosed := lcHour(0.5, 90), lcHour(1, 0)
	c := lcRun(t, lcIn{reader: reader, set: lcSet(0.01), hours: lcHours(calm, enclosed, calm, enclosed, calm)})
	if !strings.HasPrefix(c.Text, "Comfortable where judged") || !strings.Contains(c.Text, comfortBeyondSteep) {
		t.Fatalf("text = %q", c.Text)
	}
	if !strings.Contains(c.Text, "Not judged for 2 h in all: swell not judged in enclosed water.") {
		t.Errorf("the gaps went missing: %q", c.Text)
	}
}

// 6: before the bands are learned the chip says so once, gives the roughest sea, and
// words each gap as its own sentence with its length.
func TestLegComfort_UnlearnedBandsReadAsSentencesWithDurations(t *testing.T) {
	b := learnComfortBands([]ratedFelt{{ratingComfortable, 0.5}, {ratingLively, 2}})
	reader := fetchReader(t, fetchWay("n", fetchPt(-18, 3), fetchPt(18, 3)))
	c := lcRun(t, lcIn{reader: reader, bands: &b, hours: lcHours(lcHour(0.5, 90), lcHour(1, 0))})
	if !strings.HasPrefix(c.Text, "Comfort not learned yet. Roughest sea: ") ||
		!strings.HasSuffix(c.Text, " Not judged for 1 h: swell not judged in enclosed water.") || strings.Contains(c.Text, "Then") {
		t.Errorf("trailing gap: %q", c.Text)
	}

	c = lcRun(t, lcIn{bands: &b, hours: lcHours(lcHour(0.2, 0), lcHour(1, 0), lcHour(0.2, 0))})
	if want := "Comfort not learned yet. Roughest sea: roll, " + lcFigures(1, 6, 0) + "."; c.Text != want {
		t.Errorf("text = %q, want %q", c.Text, want)
	}

	// Steeper than anything recorded is said here too.
	c = lcRun(t, lcIn{bands: &b, set: lcSet(0.01), hours: lcHours(lcHour(0.2, 0), lcHour(0.2, 0))})
	if !strings.HasSuffix(c.Text, comfortBeyondSteep+".") || !strings.HasPrefix(c.Text, "Comfort not learned yet.") {
		t.Errorf("steeper: %q", c.Text)
	}

	// A flat sea has no roll to speak of.
	flat := lcHour(0, 0)
	flat.Swell, flat.WindWave = nil, nil
	flat.WaveM = 0.05
	c = lcRun(t, lcIn{bands: &b, hours: lcHours(flat, flat)})
	if want := "Comfort not learned yet. Roughest sea: flat."; c.Text != want {
		t.Errorf("flat: %q, want %q", c.Text, want)
	}
}

// 7: the chip words a sea the way each hour's line does.
func TestLegComfort_TheChipAndTheHourLinesWordTheSeaAlike(t *testing.T) {
	c := lcRun(t, lcIn{hours: lcHours(lcHour(1, 0), lcHour(0.2, 0))})
	figures := lcFigures(1, 6, 0)
	if !strings.Contains(c.Text, figures) || !strings.Contains(c.Hours[0].Line, figures) {
		t.Errorf("text %q and line %q should both say %q", c.Text, c.Hours[0].Line, figures)
	}
	if strings.Contains(c.Text, " m at ") {
		t.Errorf("the old wording is back: %q", c.Text)
	}
}
