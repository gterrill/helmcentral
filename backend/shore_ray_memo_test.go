package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

// The memo of what the shore said of a position, kept between requests (ADR 0302): one
// result for each position, kind of ray and bearing, and the geometry only inside a
// request.

// srSetup is one request's shared state over the reader and memo given.
func srSetup(reader *offlineCoastlineReader, memo *shoreRayMemo) *comfortSetup {
	return newComfortSetup(lcSet(1), lcBands(), nil, nil, func() *coastlineLoader { return reader.newLoader() }, memo)
}

// srRay is what the shore says of a wind from deg at a position, or why not.
func srRay(ctx context.Context, s *comfortSetup, pos [2]float64, kind string, deg float64) (rayResult, string) {
	get, why := s.raysAt(ctx, pos[0], pos[1])()
	if get == nil {
		return rayResult{}, why
	}
	return get(kind, deg)
}

func srMust(t *testing.T, s *comfortSetup, pos [2]float64, kind string, deg float64) rayResult {
	t.Helper()
	res, why := srRay(context.Background(), s, pos, kind, deg)
	if why != "" {
		t.Fatalf("no ray at %v: %s", pos, why)
	}
	return res
}

var srHere = [2]float64{fetchTestLat, fetchTestLon}

func srWantBuilt(t *testing.T, memo *shoreRayMemo, want int, when string) {
	t.Helper()
	if got := memo.builtCount(); got != want {
		t.Errorf("%s: %d geometries built, want %d", when, got, want)
	}
}

// A square downloaded again is a new stamp, and only the positions that read it miss.
func TestShoreRayMemo_AStampChangeMissesOnlyThePositionsNearIt(t *testing.T) {
	far := [2]float64{fetchTestLat, fetchTestLon + 3} // no square in common with srHere
	store := seedCoastlineAlong(t, time.Now(), [][2]float64{srHere, far}, func(coverageSquare) []shorelineLine { return nil })
	reader := &offlineCoastlineReader{Enabled: func() (bool, error) { return true, nil }, Store: store}
	memo := newCountedMemo(shoreRayEntries)
	ask := func(pos [2]float64) { srMust(t, srSetup(reader, memo), pos, fetchKindWind, 90) }
	ask(srHere)
	ask(far)
	srWantBuilt(t, memo, 2, "the first request")
	ask(srHere)
	ask(far)
	srWantBuilt(t, memo, 2, "the same shore again")

	needed, err := squaresWithin(srHere[0], srHere[1], shelterMaxFetchNm)
	if err != nil {
		t.Fatal(err)
	}
	for _, sq := range needed {
		if squareInland(sq) {
			continue
		}
		if err := store.SaveSquareAsOf(sq, nil, coastlineTestAt.Add(3*time.Hour), coastlineTestDataAsOf); err != nil {
			t.Fatal(err)
		}
	}
	ask(far)
	srWantBuilt(t, memo, 2, "a position that never read the square")
	ask(srHere)
	srWantBuilt(t, memo, 3, "a position that did")
}

// A request that casts several rays at one position builds its geometry once, and
// lets it go with the leg.
func TestShoreRayMemo_APositionIsBuiltOnceForALeg(t *testing.T) {
	memo := newCountedMemo(shoreRayEntries)
	s := srSetup(fetchReader(t), memo)
	srMust(t, s, srHere, fetchKindWind, 90)
	srMust(t, s, srHere, fetchKindWind, 120)
	srMust(t, s, srHere, fetchKindSwell, 100)
	srMust(t, s, srHere, fetchKindCalm, 0)
	srWantBuilt(t, memo, 1, "four rays in one leg")

	s.endLeg()
	srMust(t, s, srHere, fetchKindWind, 90)
	srWantBuilt(t, memo, 1, "a ray the request already cast, in the next leg")
	srMust(t, s, srHere, fetchKindWind, 200)
	srWantBuilt(t, memo, 2, "a new bearing in the next leg")
}

// The memo holds at most its bound, and what it dropped is built again.
func TestShoreRayMemo_TheBoundEvictsAndTheEvictedIsRebuilt(t *testing.T) {
	reader := fetchReader(t)
	memo := newCountedMemo(2)
	first := srSetup(reader, memo)
	for _, deg := range []float64{10, 20, 30} {
		srMust(t, first, srHere, fetchKindWind, deg)
	}
	if got := memo.held(); got != 2 {
		t.Fatalf("the memo holds %d results, want its bound of 2", got)
	}
	srWantBuilt(t, memo, 1, "three rays in one leg")

	second := srSetup(reader, memo)
	srMust(t, second, srHere, fetchKindWind, 30)
	srWantBuilt(t, memo, 1, "a result still held")
	srMust(t, second, srHere, fetchKindWind, 10)
	srWantBuilt(t, memo, 2, "a result that was evicted")
}

// Why the shore could not be read belongs to the request that asked. The next one,
// with the coastline on, gets its rays.
func TestShoreRayMemo_AWhyIsNeverKeptAcrossRequests(t *testing.T) {
	on := false
	reader := fetchReader(t)
	reader.Enabled = func() (bool, error) { return on, nil }
	memo := newCountedMemo(shoreRayEntries)
	_, why := srRay(context.Background(), srSetup(reader, memo), srHere, fetchKindWind, 90)
	if !strings.Contains(why, "Offline coastline is off") || memo.held() != 0 {
		t.Fatalf("with the coastline off: why %q, %d results held", why, memo.held())
	}
	on = true
	res, why := srRay(context.Background(), srSetup(reader, memo), srHere, fetchKindWind, 90)
	if why != "" || !res.Open {
		t.Fatalf("with the coastline on: %+v %q", res, why)
	}
	if memo.held() != 1 {
		t.Errorf("%d results held, want the one ray", memo.held())
	}
}

// Once a position's geometry has failed to build in a request, the rays after it at
// that position say the same without building again.
func TestShoreRayMemo_AFailedBuildIsNotRetriedForEveryRayAtThePosition(t *testing.T) {
	reader := fetchReader(t)
	memo := newCountedMemo(shoreRayEntries)
	s := srSetup(reader, memo)
	get, why := s.raysAt(context.Background(), srHere[0], srHere[1])()
	if get == nil {
		t.Fatalf("no rays at the position: %s", why)
	}
	// The squares are known to the loader; reading their lines now fails.
	if err := reader.Store.Close(); err != nil {
		t.Fatal(err)
	}
	_, first := get(fetchKindWind, 90)
	if first == "" || first == comfortReasonOutOfTime {
		t.Fatalf("the first ray's why = %q, want the build's failure", first)
	}
	for _, deg := range []float64{100, 110} {
		if _, got := get(fetchKindWind, deg); got != first {
			t.Errorf("ray at %v: why = %q, want %q", deg, got, first)
		}
	}
	if got := memo.triedCount(); got != 1 {
		t.Errorf("%d builds begun for one position, want 1", got)
	}
}

// A request holds the lines of the leg it is judging, not of every leg it has judged.
func TestShoreRayMemo_ALegEndLetsGoOfTheLoadersSets(t *testing.T) {
	s := srSetup(fetchReader(t), newCountedMemo(shoreRayEntries))
	srMust(t, s, srHere, fetchKindWind, 90)
	if len(s.loader().sets) == 0 {
		t.Fatal("the loader holds no set, so the test proves nothing")
	}
	s.endLeg()
	if got := len(s.loader().sets); got != 0 {
		t.Errorf("the loader holds %d sets after the leg ended", got)
	}
	srMust(t, s, srHere, fetchKindWind, 200)
}

// A position charted ashore is a result like any other, so it is kept.
func TestShoreRayMemo_OnLandIsKept(t *testing.T) {
	reader := fetchReader(t, landRing(4))
	memo := newCountedMemo(shoreRayEntries)
	if res := srMust(t, srSetup(reader, memo), srHere, fetchKindWind, 90); !res.OnLand {
		t.Fatalf("not ashore: %+v", res)
	}
	if res := srMust(t, srSetup(reader, memo), srHere, fetchKindWind, 90); !res.OnLand {
		t.Fatalf("not ashore the second time: %+v", res)
	}
	srWantBuilt(t, memo, 1, "two requests at a position ashore")
	if memo.held() != 1 {
		t.Errorf("%d results held, want 1", memo.held())
	}
}

// A request whose time is up gets out-of-time before any ray is read, warm or not, and
// nothing is kept for it.
func TestShoreRayMemo_APastDeadlineIsOutOfTimeEvenWhenWarmAndKeepsNothing(t *testing.T) {
	done, cancel := context.WithCancel(context.Background())
	cancel()

	cold := newCountedMemo(shoreRayEntries)
	if _, why := srRay(done, srSetup(fetchReader(t), cold), srHere, fetchKindWind, 90); why != comfortReasonOutOfTime {
		t.Errorf("cold: why = %q", why)
	}
	if cold.held() != 0 {
		t.Errorf("%d results kept for a request with no time", cold.held())
	}
	srWantBuilt(t, cold, 0, "a request with no time")

	warm := newCountedMemo(shoreRayEntries)
	reader := fetchReader(t)
	srMust(t, srSetup(reader, warm), srHere, fetchKindWind, 90)
	if _, why := srRay(done, srSetup(reader, warm), srHere, fetchKindWind, 90); why != comfortReasonOutOfTime {
		t.Errorf("warm: why = %q", why)
	}
}
