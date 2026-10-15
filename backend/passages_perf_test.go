package main

import (
	"os"
	"testing"
	"time"
)

// Review finding 10: the list must not re-read every track on every request.
func TestPassageStatsAreStoredAndTheListReadsThem(t *testing.T) {
	s := newTestPassageStore(t)
	b := simpleTrip(2 * time.Hour)
	appendAll(t, s, b.pts)
	runPassageDetection(s)

	first, err := s.ListPassageSummaries()
	if err != nil || len(first) != 1 || first[0].Stats.DistanceNM < 11 {
		t.Fatalf("%v %+v", err, first)
	}
	// the track is gone from the log; the stored figure answers the list
	if _, err := s.db.Exec(`DELETE FROM track_points`); err != nil {
		t.Fatal(err)
	}
	again, _ := s.ListPassageSummaries()
	if again[0].Stats.DistanceNM != first[0].Stats.DistanceNM {
		t.Errorf("list recomputed from points: %v then %v", first[0].Stats.DistanceNM, again[0].Stats.DistanceNM)
	}
}

func TestPassageStatsRecomputedWhenTheirPointsOrRangeChange(t *testing.T) {
	s := newTestPassageStore(t)
	b := simpleTrip(2 * time.Hour)
	appendAll(t, s, b.pts)
	runPassageDetection(s)
	list, _ := s.ListPassageSummaries()
	full := list[0].Stats.DistanceNM

	// trim changes the range
	rec := list[0].passageRecord
	if _, err := s.TrimPassage(rec.ID, rec.Start, rec.Start.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	list, _ = s.ListPassageSummaries()
	if list[0].Stats.DistanceNM >= full-2 {
		t.Errorf("trim left stale stats: %.1f vs %.1f", list[0].Stats.DistanceNM, full)
	}

	// split gives both halves their own
	a, c, _ := s.SplitPassage(rec.ID, rec.Start.Add(30*time.Minute))
	list, _ = s.ListPassageSummaries()
	if len(list) != 2 {
		t.Fatalf("%d", len(list))
	}
	got := map[string]float64{}
	for _, p := range list {
		got[p.ID] = p.Stats.DistanceNM
	}
	if got[a.ID] < 2 || got[c.ID] < 2 {
		t.Errorf("halves %v", got)
	}

	// merge
	m, _ := s.MergePassages(a.ID, c.ID)
	list, _ = s.ListPassageSummaries()
	if len(list) != 1 || list[0].ID != m.ID || list[0].Stats.DistanceNM < got[a.ID]+got[c.ID]-0.2 {
		t.Errorf("merge: %+v", list)
	}

}

func TestImportIntoAStoredPassageInvalidatesItsStats(t *testing.T) {
	s := newTestPassageStore(t)
	fixes := importTrip(simpleTrip(2 * time.Hour).pts)[0].Fixes
	s.ImportGPX([]gpxFile{{Name: "a", Fixes: fixes}})
	list, _ := s.ListPassageSummaries()
	if len(list) != 1 {
		t.Fatal(len(list))
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM passages WHERE stats_json IS NOT NULL`).Scan(&n)
	if n != 1 {
		t.Fatalf("stored stats %d", n)
	}
	mid := fixes[100].At.Add(7 * time.Second)
	sum, _ := s.ImportGPX([]gpxFile{{Name: "b", Fixes: []gpxFix{{At: mid, Lat: fixes[100].Lat, Lon: fixes[100].Lon}}}})
	if sum.PointsAdded != 1 {
		t.Fatalf("%+v", sum)
	}
	s.db.QueryRow(`SELECT COUNT(*) FROM passages WHERE stats_json IS NOT NULL`).Scan(&n)
	if n != 0 {
		t.Errorf("import into a passage's range must clear its stats, %d still stored", n)
	}
}

func TestMarinaJobSkipsCruisesThatHaveNotChanged(t *testing.T) {
	s, c := cruiseWithRise(t)
	stubMarina(t, func(float64, float64) (bool, error) { return true, nil })
	now := time.Now()
	s.LookUpNewWaterRisesAt(now)
	before := s.cruiseCoreCalls.Load()
	s.LookUpNewWaterRisesAt(now.Add(time.Minute))
	if got := s.cruiseCoreCalls.Load(); got != before {
		t.Errorf("unchanged cruise was rebuilt: %d -> %d", before, got)
	}
	// filing another passage in makes it a candidate again
	p := seedPassage(t, s, passageEpoch.Add(10*time.Hour), passageEpoch.Add(11*time.Hour))
	s.PatchPassage(p.ID, passagePatch{CruiseID: &c.ID, CruiseIDSet: true})
	s.LookUpNewWaterRisesAt(now.Add(2 * time.Minute))
	if s.cruiseCoreCalls.Load() == before {
		t.Error("a changed cruise must be looked at")
	}
}

func TestMarinaJobRevisitsAUnchangedCruiseWhenAFailureIsDueForRetry(t *testing.T) {
	s, c := cruiseWithRise(t)
	stubMarina(t, func(float64, float64) (bool, error) { return false, os.ErrDeadlineExceeded })
	now := time.Now()
	s.LookUpNewWaterRisesAt(now)
	calls := stubMarina(t, func(float64, float64) (bool, error) { return true, nil })
	s.LookUpNewWaterRisesAt(now.Add(7 * time.Hour))
	if *calls != 1 {
		t.Fatalf("retry calls %d", *calls)
	}
	_, _, st, _ := s.CruiseStats(c.ID)
	if st.WaterRises[0].SuggestionStatus != "found" {
		t.Errorf("%+v", st.WaterRises[0])
	}
}
