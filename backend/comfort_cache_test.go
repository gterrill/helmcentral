package main

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ── the comfort fit reads, keeps and re-judges (ADR 0301) ────────────────────

// fitSpells is fitComfortModels for spells alone: none set aside, none waiting.
func fitSpells(spells []comfortSpell) *comfortModelSet {
	return fitComfortModels(spells, comfortRejections{}, comfortWaiting{})
}

// cachedIDs are the passages the worker holds a judgement of, in order.
func cachedIDs(w *comfortModelWorker) []string {
	var ids []string
	for id := range w.cache {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func sortedIDs(ids ...string) []string {
	sort.Strings(ids)
	return ids
}

func sameIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func clearFigures(t *testing.T, store *passageStore, id string) {
	t.Helper()
	if _, err := store.db.Exec(`UPDATE passages SET stats_json = NULL, felt_deg = NULL WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
}

// twoSeaPassages is two passages, a degree apart, each with its hindcast stored.
func twoSeaPassages(t *testing.T) (*passageStore, passageRecord, passageRecord, []feltSpell) {
	t.Helper()
	store, a, spellsA := seaPassage(t)
	b, _ := addSeaPassage(t, store, 11)
	runSeaHindcast(context.Background(), store, resolveTo(&fakeHindcast{}), seaNow)
	return store, a, b, spellsA
}

// usableCount is the spells the last fit could learn from.
func usableCount(set *comfortModelSet) int { return set.Rejected.Total - set.Rejected.count() }

// 1
func TestASecondFitWithNothingChangedJudgesNoPassageAndLogsNothing(t *testing.T) {
	store, _, _, _ := twoSeaPassages(t)
	buf := logOf(t)
	store.comfort.fit(t.Context(), store, noCoastline)
	if got := store.comfort.judged.Load(); got != 2 {
		t.Fatalf("the first fit judged %d passages, want 2", got)
	}
	if buf.Len() == 0 {
		t.Fatal("the first fit should say what it did")
	}
	buf.Reset()
	store.comfort.fit(t.Context(), store, noCoastline)
	if got := store.comfort.judged.Load(); got != 2 {
		t.Errorf("a second fit with nothing changed judged %d more passages, want none", got-2)
	}
	if buf.Len() != 0 {
		t.Errorf("a second fit with nothing changed logged: %s", buf.String())
	}
}

// 2
func TestAPassageWithNoFiguresIsWaitingAndIsNotWorkedOutByTheFit(t *testing.T) {
	store, rec, _ := seaPassage(t)
	clearFigures(t, store, rec.ID)
	before := store.figuresComputed.Load()
	store.comfort.fit(t.Context(), store, noCoastline)
	set := store.comfort.Load()
	if set.Waiting.Passages != 1 {
		t.Errorf("waiting passages = %d, want 1", set.Waiting.Passages)
	}
	if got := store.figuresComputed.Load() - before; got != 0 {
		t.Errorf("the fit worked out %d passages' figures; it only reads", got)
	}
	want := "Comfort model: not learned yet; 1 passage is waiting for its figures to be worked out."
	if set.Summary != want {
		t.Errorf("summary %q, want %q", set.Summary, want)
	}
	if got := set.wire().WaitingPassages; got != 1 {
		t.Errorf("wire waiting_passages = %d, want 1", got)
	}
	addSeaPassage(t, store, 11)
	clearFigures(t, store, rec.ID)
	store.comfort.fit(t.Context(), store, noCoastline)
	if got := store.comfort.Load().Summary; !strings.Contains(got, "; 1 passage is waiting for its figures to be worked out") {
		t.Errorf("a fit that read a passage must still name the one waiting: %q", got)
	}
}

// 3
func TestStoringAPassagesFiguresAsksForAFitAndEndsTheWait(t *testing.T) {
	store, rec, _ := seaPassage(t)
	clearFigures(t, store, rec.ID)
	signalled(store.comfort)
	store.comfort.fit(t.Context(), store, noCoastline)
	if _, err := store.PassageFiguresFor(rec); err != nil {
		t.Fatal(err)
	}
	if !signalled(store.comfort) {
		t.Error("working out a passage's figures must ask for a fit")
	}
	store.comfort.fit(t.Context(), store, noCoastline)
	if got := store.comfort.Load().Waiting.Passages; got != 0 {
		t.Errorf("waiting passages = %d after the figures were stored, want 0", got)
	}
}

// 4
func TestANewHindcastRowJudgesOnlyItsOwnPassageAgain(t *testing.T) {
	store, a, _, spellsA := twoSeaPassages(t)
	_ = a
	store.comfort.fit(t.Context(), store, noCoastline)
	if got := store.comfort.judged.Load(); got != 2 {
		t.Fatalf("judged %d, want 2", got)
	}
	keys := distinctSeaKeys(spellsA)
	first, _, _ := hourRange(keys)
	var minLat, maxLat, lon int
	set := false
	for k := range keys {
		if !set {
			minLat, maxLat, lon, set = k.LatC, k.LatC, k.LonC, true
		}
		minLat, maxLat = min(minLat, k.LatC), max(maxLat, k.LatC)
	}
	stored := false
	for lat := minLat; lat <= maxLat && !stored; lat++ {
		k := seaKey{HourMs: first, LatC: lat, LonC: lon}
		if keys[k] {
			continue
		}
		if err := store.StoreSeaHindcastRows([]seaHindcastRow{{seaKey: k, Provider: "fake-waves", Model: "ncep_gfswave025", FetchedMs: 1, MissingReason: "x"}}); err != nil {
			t.Fatal(err)
		}
		stored = true
	}
	if !stored {
		t.Fatal("no free cell inside the passage's own box to store a row in")
	}
	store.comfort.fit(t.Context(), store, noCoastline)
	if got := store.comfort.judged.Load(); got != 3 {
		t.Errorf("a new row inside one passage's box judged %d passages, want only that one (3 in all)", got-2)
	}
}

// 5
func TestANewFetchRayTurnsUncheckedSpellsIntoUsableOnes(t *testing.T) {
	rig := newFetchRig(t, 15, 180, landEastOf(2))
	buf := logOf(t)
	rig.store.comfort.fit(t.Context(), rig.store, rig.coast)
	if got := usableCount(rig.store.comfort.Load()); got != 0 {
		t.Fatalf("with no rays stored %d spells are usable, want 0", got)
	}
	rig.sweep(noBudgetLimit)
	buf.Reset()
	rig.store.comfort.fit(t.Context(), rig.store, rig.coast)
	set := rig.store.comfort.Load()
	if got := usableCount(set); got != 2 {
		t.Errorf("after the sweep stored its rays %d spells are usable, want 2: %+v", got, set.Rejected)
	}
	if out := buf.String(); !strings.Contains(out, "1 wave model") || strings.Contains(out, "1 wave models") {
		t.Errorf("one wave model must read in the singular: %s", out)
	}
	judged := rig.store.comfort.judged.Load()
	rig.store.comfort.fit(t.Context(), rig.store, rig.coast)
	if got := rig.store.comfort.judged.Load(); got != judged {
		t.Errorf("a fit with nothing new judged %d passages again", got-judged)
	}
}

// 6
func TestAChangedCoastlineJudgesAgainWithNoNewRowsAndWakesTheFit(t *testing.T) {
	rig := newFetchRig(t, 15, 180, landEastOf(2))
	rig.sweep(noBudgetLimit)
	on := true
	rig.reader.Enabled = func() (bool, error) { return on, nil }
	rig.store.comfort.fit(t.Context(), rig.store, rig.coast)
	if got := usableCount(rig.store.comfort.Load()); got != 2 {
		t.Fatalf("with the coastline on %d spells are usable, want 2", got)
	}
	judged := rig.store.comfort.judged.Load()
	on = false
	rig.store.comfort.fit(t.Context(), rig.store, rig.coast)
	if got := usableCount(rig.store.comfort.Load()); got != 0 {
		t.Errorf("with the coastline off %d spells are usable, want 0", got)
	}
	if got := rig.store.comfort.judged.Load(); got != judged+1 {
		t.Errorf("turning the coastline off judged %d passages again, want 1", got-judged)
	}

	// The pass that finds the coastline changed asks for the fit, though it stored nothing.
	on = true
	runSeaPass(context.Background(), rig.store, resolveTo(&fakeHindcast{}), rig.coast, seaNow)
	signalled(rig.store.comfort)
	on = false
	runSeaPass(context.Background(), rig.store, resolveTo(&fakeHindcast{}), rig.coast, seaNow)
	if !signalled(rig.store.comfort) {
		t.Error("a pass that found the coastline turned off stored nothing but must ask for a fit")
	}
	runSeaPass(context.Background(), rig.store, resolveTo(&fakeHindcast{}), rig.coast, seaNow)
	if signalled(rig.store.comfort) {
		t.Error("a pass that found the coastline as it was asked for a fit")
	}
}

// 7
func TestAMergedPassageDropsItsOldIDAndASplitOneJudgesTheNewID(t *testing.T) {
	store, rec, _ := seaPassage(t)
	runSeaHindcast(context.Background(), store, resolveTo(&fakeHindcast{}), seaNow)
	store.comfort.fit(t.Context(), store, noCoastline)
	if got := cachedIDs(store.comfort); !sameIDs(got, []string{rec.ID}) {
		t.Fatalf("cached %v, want [%s]", got, rec.ID)
	}
	a, b, err := store.SplitPassage(rec.ID, rec.Start.Add(15*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	store.comfort.fit(t.Context(), store, noCoastline)
	if got := cachedIDs(store.comfort); !sameIDs(got, sortedIDs(a.ID, b.ID)) {
		t.Errorf("after a split cached %v, want %v", got, sortedIDs(a.ID, b.ID))
	}
	for _, r := range []passageRecord{a, b} {
		if _, err := store.PassageFiguresFor(r); err != nil {
			t.Fatal(err)
		}
	}
	judged := store.comfort.judged.Load()
	store.comfort.fit(t.Context(), store, noCoastline)
	if got := store.comfort.judged.Load() - judged; got != 2 {
		t.Errorf("the two halves judged %d passages, want 2", got)
	}
	m, err := store.MergePassages(a.ID, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	store.comfort.fit(t.Context(), store, noCoastline)
	if got := cachedIDs(store.comfort); !sameIDs(got, []string{m.ID}) {
		t.Errorf("after a merge cached %v, want [%s]", got, m.ID)
	}
}

// A passage merged or split away between listing the heads and reading its
// figures is dropped, as an absent head is; it does not fail the fit.
func TestAPassageGoneBetweenTheListingAndTheReadIsDroppedNotAnError(t *testing.T) {
	store, _, _ := seaPassage(t)
	w := &comfortModelWorker{}
	e, err := w.entryFor(store, noCoastline().newLoader(), "", comfortHead{id: "merged-away", start: 1, end: 2})
	if err != nil {
		t.Fatalf("a vanished passage failed the fit: %v", err)
	}
	if e != nil {
		t.Errorf("a vanished passage left an entry: %+v", e)
	}
}

// Hidden passages are not trained on, as ComfortBands leaves them out, and
// hiding or showing one asks for a fit.
func TestAHiddenPassageIsNotTrainedOnAndHidingItAsksForAFit(t *testing.T) {
	store, rec, _ := seaPassage(t)
	runSeaHindcast(context.Background(), store, resolveTo(&fakeHindcast{}), seaNow)
	signalled(store.comfort)
	yes, no := true, false
	if _, err := store.PatchPassage(rec.ID, passagePatch{Hidden: &yes}); err != nil {
		t.Fatal(err)
	}
	if !signalled(store.comfort) {
		t.Error("hiding a passage must ask for a fit")
	}
	heads, err := store.comfortHeads()
	if err != nil {
		t.Fatal(err)
	}
	if len(heads) != 0 {
		t.Errorf("a hidden passage is a head: %+v", heads)
	}
	if _, err := store.PatchPassage(rec.ID, passagePatch{Hidden: &no}); err != nil {
		t.Fatal(err)
	}
	if !signalled(store.comfort) {
		t.Error("showing a passage must ask for a fit")
	}
	if heads, _ = store.comfortHeads(); len(heads) != 1 {
		t.Errorf("a shown passage is not a head: %+v", heads)
	}
}

// 8
func TestTwoReadsOfOneStalePassageWorkItOutOnce(t *testing.T) {
	store, rec, _ := seaPassage(t)
	clearFigures(t, store, rec.ID)
	before := store.figuresComputed.Load()
	store.figuresHook = func() { time.Sleep(150 * time.Millisecond) }
	var wg sync.WaitGroup
	got := make([]passageFigures, 2)
	for i := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f, err := store.PassageFiguresFor(rec)
			if err != nil {
				t.Error(err)
			}
			got[i] = f
		}()
	}
	wg.Wait()
	if n := store.figuresComputed.Load() - before; n != 1 {
		t.Errorf("two concurrent reads worked the passage out %d times, want once", n)
	}
	if len(got[0].Stats.FeltSpells) != 2 || len(got[1].Stats.FeltSpells) != 2 {
		t.Errorf("both reads must carry the figures: %d and %d spells", len(got[0].Stats.FeltSpells), len(got[1].Stats.FeltSpells))
	}
}

// A reader that comes after a clear must not join a flight that read the points
// from before it: the flight is keyed by the generation the reader saw.
func TestAReaderAfterAClearDoesNotJoinAFlightThatReadTheOldPoints(t *testing.T) {
	store, rec, _ := seaPassage(t)
	clearFigures(t, store, rec.ID)
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	store.figuresHook = func() {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
	}
	doneA := make(chan struct{})
	go func() {
		defer close(doneA)
		_, _ = store.PassageFiguresFor(rec)
	}()
	<-started
	clearFigures(t, store, rec.ID)
	doneB := make(chan struct{})
	go func() {
		defer close(doneB)
		_, _ = store.PassageFiguresFor(rec)
	}()
	select {
	case <-doneB:
	case <-time.After(2 * time.Second):
		t.Error("the reader after the clear waited on the flight that read the old points")
	}
	close(release)
	<-doneA
	<-doneB
	if got := calls.Load(); got != 2 {
		t.Errorf("figures were worked out %d times, want 2 (one per generation)", got)
	}
}

// A refused store leaves a recorded failure standing: nothing was stored, so
// nothing has been worked out.
func TestARefusedStoreDoesNotClearARecordedFailure(t *testing.T) {
	store, rec, _ := seaPassage(t)
	clearFigures(t, store, rec.ID)
	store.noteFiguresResult(rec.ID, errors.New("boom"))
	store.figuresHook = func() { clearFigures(t, store, rec.ID) }
	if _, err := store.PassageFiguresFor(rec); err != nil {
		t.Fatal(err)
	}
	if got := store.figuresFailure(rec.ID); got != "boom" {
		t.Errorf("failure after a refused store = %q, want it kept", got)
	}
}

// The record of failed figures follows the live passages, as the cache does.
func TestAFailureRecordedForAMergedAwayPassageIsPruned(t *testing.T) {
	store, rec, _ := seaPassage(t)
	store.noteFiguresResult("merged-away", errors.New("boom"))
	store.noteFiguresResult(rec.ID, errors.New("still here"))
	store.comfort.fit(t.Context(), store, noCoastline)
	if got := store.figuresFailure("merged-away"); got != "" {
		t.Errorf("a failure for a passage that is gone is kept: %q", got)
	}
	if got := store.figuresFailure(rec.ID); got != "still here" {
		t.Errorf("a live passage's failure = %q, want it kept", got)
	}
}

// 9
func TestAnImportThatClearsTheFiguresDuringARecomputeRefusesTheStaleStore(t *testing.T) {
	store, rec, _ := seaPassage(t)
	clearFigures(t, store, rec.ID)
	signalled(store.comfort)
	store.figuresHook = func() { clearFigures(t, store, rec.ID) }
	if _, err := store.PassageFiguresFor(rec); err != nil {
		t.Fatal(err)
	}
	var raw *string
	if err := store.db.QueryRow(`SELECT stats_json FROM passages WHERE id = ?`, rec.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw != nil {
		t.Error("figures worked out from points read before the clear were stored over it")
	}
	if signalled(store.comfort) {
		t.Error("a refused store must not ask for a fit")
	}
	store.comfort.fit(t.Context(), store, noCoastline)
	if got := store.comfort.Load().Waiting.Passages; got != 1 {
		t.Errorf("waiting passages = %d, want 1", got)
	}
}

// 10
func TestAHourWithNeitherSystemIsFlatOnlyWhenTheCombinedSeaIsSmall(t *testing.T) {
	answer := func(height *float64) func([]currentsPoint, time.Time, time.Time) waveHindcastBundle {
		return func(points []currentsPoint, start, end time.Time) waveHindcastBundle {
			return answerEveryPoint(points, start, end, func(_ currentsPoint, h time.Time) waveHindcastHour {
				return waveHindcastHour{waveHourPoint: waveHourPoint{Time: h, WaveHeightM: height}}
			})
		}
	}
	for _, c := range []struct {
		name   string
		height *float64
		want   string
	}{
		{"calm", pfloat(0.05), ""},
		{"combined sea but no split", pfloat(0.5), seaReasonNoSplit},
		{"combined unknown", nil, seaReasonNoSplit},
	} {
		t.Run(c.name, func(t *testing.T) {
			store, _, spells := seaPassage(t)
			runSeaHindcast(context.Background(), store, resolveTo(&fakeHindcast{answer: answer(c.height)}), seaNow)
			sea := store.seaLookupFor(spells).forSpell(spells[0])
			if sea.Reason != c.want {
				t.Fatalf("the page says %q, want %q", sea.Reason, c.want)
			}
			cs, why := comfortSpellFrom("p", comfortFeltSpell(pfloat(0)), sea, "gfs")
			if c.want == "" && (why != "" || cs.Swell != nil || cs.WindWave != nil) {
				t.Errorf("a calm hour trains as flat: %+v %q", cs, why)
			}
			if c.want != "" && !strings.Contains(why, "split") {
				t.Errorf("the fit's reason %q should say there is no split", why)
			}
		})
	}

	t.Run("a system with no height is absent on both sides", func(t *testing.T) {
		sp := fetchSpell(15, 180)
		row := &seaHindcastRow{Swell: &seaTrainFigures{HeightM: 0, PeriodS: 8, FromDeg: 270}, WaveHeightM: pfloat(0.5)}
		if got := seaForSpell(sp, row, nil).Reason; got != seaReasonNoSplit {
			t.Errorf("a zero-height swell with a 0.5 m combined sea: %q", got)
		}
		row = &seaHindcastRow{Swell: &seaTrainFigures{HeightM: 0.3, PeriodS: 8, FromDeg: 270}, WaveHeightM: pfloat(0.5)}
		if got := seaForSpell(sp, row, nil).Reason; got != "" {
			t.Errorf("a swell is a split whatever the combined height: %q", got)
		}
	})
}

func TestRowsStoredWithoutTheCombinedHeightAreFetchedAgainOnce(t *testing.T) {
	for _, c := range []struct {
		name   string
		height *float64
	}{{"plugin gives it", pfloat(1)}, {"plugin never gives it", nil}} {
		t.Run(c.name, func(t *testing.T) {
			store, _, spells := seaPassage(t)
			fake := &fakeHindcast{answer: func(points []currentsPoint, start, end time.Time) waveHindcastBundle {
				return answerEveryPoint(points, start, end, func(_ currentsPoint, h time.Time) waveHindcastHour {
					hour := swellAndChop(h)
					hour.WaveHeightM = c.height
					return hour
				})
			}}
			runSeaHindcast(context.Background(), store, resolveTo(fake), seaNow)
			if _, err := store.db.Exec(`UPDATE sea_hindcast_hours SET wave_height_m = NULL, wave_height_seen = 0`); err != nil {
				t.Fatal(err)
			}
			calls := len(fake.calls)
			sum := runSeaHindcast(context.Background(), store, resolveTo(fake), seaNow)
			if len(fake.calls) == calls || sum.Filled != len(distinctSeaKeys(spells)) {
				t.Fatalf("the sweep must ask for the rows stored before the figure was kept: %d calls, %+v", len(fake.calls)-calls, sum)
			}
			for k, row := range storedSea(t, store, spells) {
				if (c.height == nil) != (row.WaveHeightM == nil) || (c.height != nil && *row.WaveHeightM != *c.height) {
					t.Errorf("row %+v kept combined height %v, want %v", k, row.WaveHeightM, c.height)
				}
			}
			calls = len(fake.calls)
			runSeaHindcast(context.Background(), store, resolveTo(fake), seaNow)
			if len(fake.calls) != calls {
				t.Error("a row asked for again must not be asked for a third time")
			}
		})
	}
}

// 11
func TestEachWayAHourCanHaveNoSeaIsCountedApart(t *testing.T) {
	reasons := []string{
		seaReasonNoPosition,
		seaReasonNotFetched,
		seaReasonCantFetch + "no wave plugin is set.",
		seaRefusedPrefix + " for this hour: too many requests",
		seaReasonNoFigure,
		seaReasonNoSplit,
	}
	seen := map[string]string{}
	for _, r := range reasons {
		_, why := comfortSpellFrom("p", comfortFeltSpell(pfloat(0)), spellSea{Reason: r}, "gfs")
		if why == "" {
			t.Fatalf("%q must set the spell aside", r)
		}
		if other, dup := seen[why]; dup {
			t.Errorf("%q and %q are set aside for one reason, %q", r, other, why)
		}
		seen[why] = r
	}
}

func TestAnUnreadableHindcastStopsTheFit(t *testing.T) {
	store, _, _ := seaPassage(t)
	if _, err := store.db.Exec(`DROP TABLE sea_hindcast_hours`); err != nil {
		t.Fatal(err)
	}
	store.comfort.fit(t.Context(), store, noCoastline)
	set := store.comfort.Load()
	if set == nil || !strings.Contains(set.Err, "hindcast") {
		t.Fatalf("a hindcast that cannot be read must fail the fit: %+v", set)
	}
}

func TestAMissingWavePluginDoesNotStopTheFitAndIsSaid(t *testing.T) {
	rig := newFetchRig(t, 15, 180, landEastOf(2))
	rig.sweep(noBudgetLimit)
	rig.store.setSeaProblem(seaReasonCantFetch + "no wave plugin is set.")
	rig.store.comfort.fit(t.Context(), rig.store, rig.coast)
	set := rig.store.comfort.Load()
	if set.Err != "" {
		t.Fatalf("a missing plugin must not fail the fit: %s", set.Err)
	}
	if got := usableCount(set); got != 2 {
		t.Errorf("the fit still learns from the stored hours: %d usable, want 2", got)
	}
	if !strings.Contains(set.Summary, "new sea hours are waiting for the wave plugin") {
		t.Errorf("summary %q", set.Summary)
	}
	rig.store.setSeaProblem("")
	rig.store.comfort.fit(t.Context(), rig.store, rig.coast)
	if strings.Contains(rig.store.comfort.Load().Summary, "wave plugin") {
		t.Errorf("the note must go once the plugin works: %q", rig.store.comfort.Load().Summary)
	}
}

// 12
func TestTheMismatchSentenceLowercasesAnArticleMidSentence(t *testing.T) {
	set := learnedSet(t)
	reason := func(label string) string {
		return set.predict(comfortHourInput{Model: waveModelRef{ID: "open-meteo", Label: label}}).Sway.Reason
	}
	if got := reason("The Open-Meteo best-match blend"); !strings.Contains(got, "this forecast comes from the Open-Meteo best-match blend.") {
		t.Errorf("reason %q", got)
	}
	if got := reason("GFS-Wave"); !strings.Contains(got, "this forecast comes from GFS-Wave.") {
		t.Errorf("reason %q", got)
	}
}

// 13
func TestARepeatedFailureOfTheFitIsLoggedOnce(t *testing.T) {
	rig := newFetchRig(t, 15, 180)
	if _, err := rig.store.db.Exec(`DROP TABLE sea_fetch_rays`); err != nil {
		t.Fatal(err)
	}
	buf := logOf(t)
	rig.store.comfort.fit(t.Context(), rig.store, noCoastline)
	rig.store.comfort.fit(t.Context(), rig.store, noCoastline)
	if n := strings.Count(buf.String(), "could not read the record"); n != 1 {
		t.Errorf("a failure that repeated was logged %d times, want once: %s", n, buf.String())
	}
}

// 14
func TestTheNotWorkedOutPhraseIsOneSharedPhrase(t *testing.T) {
	var none *comfortModelSet
	if got := none.statusLine(); !strings.Contains(got, comfortNotWorkedOut) {
		t.Errorf("status line %q", got)
	}
	if got := none.predict(comfortHourInput{}).Sway.Reason; !strings.Contains(got, comfortNotWorkedOut) {
		t.Errorf("prediction reason %q", got)
	}
}

// 15
func TestAPassageWhoseFiguresKeepFailingIsWordedWithItsError(t *testing.T) {
	store, bad, _ := seaPassage(t)
	failFiguresOf(t, store, bad.ID)
	if _, err := store.PassageFiguresFor(bad); err == nil {
		t.Fatal("the store was meant to fail")
	}
	store.comfort.fit(t.Context(), store, noCoastline)
	set := store.comfort.Load()
	if set.Waiting.Passages != 1 || len(set.Waiting.Failed) != 1 {
		t.Fatalf("waiting %+v", set.Waiting)
	}
	if !strings.Contains(set.Summary, "disk on fire") || strings.Contains(set.Summary, "waiting for its figures") {
		t.Errorf("a passage that keeps failing is not just waiting: %q", set.Summary)
	}
	if _, err := store.db.Exec(`DROP TRIGGER fail_figures`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PassageFiguresFor(bad); err != nil {
		t.Fatal(err)
	}
	store.comfort.fit(t.Context(), store, noCoastline)
	if got := store.comfort.Load(); got.Waiting.Passages != 0 || strings.Contains(got.Summary, "disk on fire") {
		t.Errorf("once the figures are stored the failure goes: %+v %q", got.Waiting, got.Summary)
	}
}
