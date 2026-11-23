package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Review pass 2 on the comfort chip on a plan leg (ADR 0302): one fixed ladder per
// leg, found before any hour is walked.

func ladderSetWith(ids ...string) *comfortModelSet {
	set := lcSet(1)
	fit := set.Fits[lcModelID]
	set.Fits = map[string]*comfortFit{}
	for _, id := range ids {
		set.Fits[id] = fit
	}
	return set
}

func TestComfortLadder_ReadsOneReasonPerRungBeforeAnyHour(t *testing.T) {
	named := waveModelRef{ID: lcModelID, Label: "The test wave model"}
	gfs := waveModelRef{ID: "gfs_wave", Label: "GFS-Wave"}
	blend := waveModelRef{ID: "om_blend", Label: "The Open-Meteo best-match blend"}
	ecmwf := waveModelRef{ID: "ecmwf_wam", Label: "ECMWF WAM"}
	cases := []struct {
		name       string
		set        *comfortModelSet
		bandsOK    bool
		models     []waveModelRef
		contains   []string
		excludes   []string
		unlearned  bool
		wantNoStop bool
	}{
		{name: "model not worked out", set: nil, bandsOK: true, models: []waveModelRef{named},
			contains: []string{"not worked out yet"}},
		{name: "model failed", set: &comfortModelSet{Err: "the record is unreadable"}, bandsOK: true, models: []waveModelRef{named},
			contains: []string{"couldn't be worked out", "the record is unreadable"}},
		{name: "sway not learned everywhere", set: lcUnlearned("Needs 8 passages with motion and sea, has 2"), bandsOK: true, models: []waveModelRef{named},
			contains: []string{"Needs 8 passages with motion and sea, has 2"}},
		{name: "sway held back with no words still blocks", set: lcUnlearned(""), bandsOK: true, models: []waveModelRef{named},
			contains: []string{"no reason"}},
		{name: "old plugin on some hours", set: lcSet(1), bandsOK: true, models: []waveModelRef{named, {}},
			contains: []string{"update the wave plugin"}},
		{name: "forecast model with no fit", set: ladderSetWith("gfs_wave"), bandsOK: true, models: []waveModelRef{blend},
			contains: []string{"learned on gfs_wave", "the Open-Meteo best-match blend"}},
		{name: "seam of two wave models gives two reasons", set: ladderSetWith("gfs_wave"), bandsOK: true, models: []waveModelRef{blend, ecmwf, blend},
			contains: []string{"the Open-Meteo best-match blend", "ECMWF WAM"}},
		{name: "a fitted model beside an unfitted one names only the unfitted", set: ladderSetWith("gfs_wave"), bandsOK: true, models: []waveModelRef{gfs, blend},
			contains: []string{"the Open-Meteo best-match blend"}, excludes: []string{"GFS-Wave."}},
		{name: "bands not learned is no block", set: lcSet(1), bandsOK: false, models: []waveModelRef{named},
			unlearned: true, wantNoStop: true},
		{name: "model rung comes before bands", set: nil, bandsOK: false, models: []waveModelRef{named},
			contains: []string{"not worked out yet"}},
		{name: "a clean leg passes", set: lcSet(1), bandsOK: true, models: []waveModelRef{named}, wantNoStop: true},
	}
	for _, c := range cases {
		got := comfortLadder(c.set, c.bandsOK, c.models)
		if c.wantNoStop {
			if got.Blocked != "" || got.BandsNotLearned != c.unlearned {
				t.Errorf("%s: got %+v, want no block, unlearned=%v", c.name, got, c.unlearned)
			}
			continue
		}
		if got.Blocked == "" {
			t.Errorf("%s: not blocked, want a reason", c.name)
		}
		for _, s := range c.contains {
			if !strings.Contains(got.Blocked, s) {
				t.Errorf("%s: %q lacks %q", c.name, got.Blocked, s)
			}
		}
		for _, s := range c.excludes {
			if strings.Contains(got.Blocked, s) {
				t.Errorf("%s: %q holds %q", c.name, got.Blocked, s)
			}
		}
	}
}

// A leg with the bands unlearned and only gaps that are not the model's still says the
// comfort is not learned yet.
func TestLegComfort_BandsNotLearnedWithOnlyHourGapsStillSaysSo(t *testing.T) {
	b := learnComfortBands([]ratedFelt{{ratingComfortable, 0.5}, {ratingLively, 2}})
	reader := fetchReader(t, fetchWay("n", fetchPt(-18, 3), fetchPt(18, 3))) // land three miles north: enclosed swell
	c := lcRun(t, lcIn{reader: reader, bands: &b, hours: lcHours(lcHour(1, 0), lcHour(1, 0))})
	if !strings.HasPrefix(c.Text, "Comfort not learned yet.") || !strings.Contains(c.Text, "Not judged for 2 h: swell not judged in enclosed water.") {
		t.Fatalf("text = %q", c.Text)
	}
}

// A leg whose model is blocked has no hours: the ladder is the leg's whole text.
func TestLegComfort_ASeamOfTwoWaveModelsNamesBothReasons(t *testing.T) {
	a, b := lcHour(0.2, 0), lcHour(0.2, 0)
	a.WaveModel = waveModelRef{ID: "x", Label: "Model X"}
	b.WaveModel = waveModelRef{ID: "y", Label: "Model Y"}
	c := lcRun(t, lcIn{hours: lcHours(a, b)})
	if !strings.HasPrefix(c.Text, "Comfort not judged yet: ") || !strings.Contains(c.Text, "Model X") || !strings.Contains(c.Text, "Model Y") || len(c.Hours) != 0 {
		t.Fatalf("text = %q, %d hours", c.Text, len(c.Hours))
	}
}

// The model's standing leads the leg's text whatever the plugin did on some hours.
func TestLegComfort_TheModelRungComesBeforeTheOldPluginRung(t *testing.T) {
	old := lcHour(1, 0)
	old.WaveModel = waveModelRef{}
	c := lcRun(t, lcIn{set: lcUnlearned("Needs 8 passages with motion and sea, has 2"), hours: []*passageHour{&old, ptr(lcHour(1, 0))}})
	if want := "Comfort not judged yet: needs 8 passages with motion and sea, has 2."; c.Text != want {
		t.Fatalf("text = %q, want %q", c.Text, want)
	}
}

// A few seconds of an hour at either end of a leg is no stretch of its own.
func TestLegComfort_ASliverOfAnHourIsFoldedIntoItsNeighbour(t *testing.T) {
	// Departs 8:00, arrives 10:00:30: the stamp at 10 AM covers 30 seconds.
	hours := lcHours(lcHour(0.2, 0), lcHour(0.2, 0), lcHour(1, 0))
	c, judged, _ := lcRunAll(t, lcIn{hours: hours, arriveAfter: -(time.Hour - 30*time.Second)})
	if len(judged) != 3 || strings.Contains(c.Text, "0 min") {
		t.Fatalf("text = %q", c.Text)
	}
	if want := "Comfortable."; c.Text != want {
		t.Fatalf("text = %q, want %q: a 30 s sliver is not judged on its own", c.Text, want)
	}
	// The same at the start of a leg under way.
	c, _, _ = lcRunAll(t, lcIn{underway: true, from: paAt(14, 9, 0).Add(-30 * time.Second),
		hours: lcHours(lcHour(1, 0), lcHour(0.2, 0), lcHour(0.2, 0))})
	if strings.Contains(c.Text, "0 min") || strings.Contains(c.Text, "Lively") {
		t.Fatalf("under way: %q", c.Text)
	}
	if got := durationText(20 * time.Second); got == "0 min" {
		t.Errorf("durationText never prints 0 min, got %q", got)
	}
}

// Positions snap to 0.001 degree (about 110 m).
func TestComfortSetup_SnapsPositionsToAThousandthOfADegree(t *testing.T) {
	k := comfortSnapKey(-27.4549, 153.0171)
	if k != [2]int{-27455, 153017} {
		t.Fatalf("key = %v, want {-27455 153017}", k)
	}
}

// A channel hugging the track: land lies 0.1 nm south of it and the leg runs 0.25 nm
// north of that. Snapped to 0.01 degree the track sits on the land; at 0.001 degree
// it stays in the water and is judged.
func TestLegComfort_ASnapNeverMovesAChannelPointOntoLand(t *testing.T) {
	// Land on the left of a way heading west is to the south.
	land := fetchWay("s", fetchPt(18, 0.1), fetchPt(-18, 0.1))
	reader := fetchReader(t, land)
	a, b := fetchPt(-1, 0.252), fetchPt(1, 0.252)
	route := []passageRoutePoint{{Lat: a.Lat, Lon: a.Lon, Name: "A"}, {Lat: b.Lat, Lon: b.Lon, Name: "B"}}
	c := lcRun(t, lcIn{reader: reader, route: route, hours: lcHours(lcHour(0.2, 0), lcHour(0.2, 0))})
	if strings.Contains(c.Text, "ashore") || c.Text != "Comfortable." {
		t.Fatalf("text = %q: a point in the water was judged from a position snapped onto land", c.Text)
	}
}

// Comfort's readers are read when the first leg that gets comfort asks, never for a
// plan whose legs all skip it.
func TestPlanComfortRun_ReadsTheModelBandsAndHelmHeightOnlyWhenALegNeedsThem(t *testing.T) {
	reads := 0
	d := &planComfortDeps{
		model: func() (*comfortModelSet, error) { reads++; return lcSet(1), nil },
		bands: func() (comfortBands, error) { reads++; return lcBands(), nil },
		helmHeight: func() (*float64, error) {
			reads++
			return ptr(2.0), nil
		},
		loader: func() *coastlineLoader { return nil },
		memo:   newCountedMemo(fetchGeoBudgetSegments),
	}
	run := newPlanComfortRun(nil, planEnv{now: paAt(14, 9, 0)}, d) // both legs' ETAs have passed: no walk
	if got := run.leg(context.Background(), &planLegWork{res: &planLegAssessment{}}); got != nil || reads != 0 {
		t.Fatalf("a leg with no comfort read %d readers and gave %+v", reads, got)
	}
	for range 2 {
		run.leg(context.Background(), &planLegWork{res: &planLegAssessment{}, underway: true, windReady: true, eta: paAt(14, 7, 0), hours: 2, fromLoc: paLoc})
	}
	if reads != 3 {
		t.Fatalf("readers read %d times, want once each for two legs", reads)
	}
}
