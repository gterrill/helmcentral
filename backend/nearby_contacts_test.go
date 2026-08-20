package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// newTestNearbyContactStore returns a store with dwell 0, so a candidate new
// encounter confirms on its very first tick - preserving the pre-dwell
// one-call-one-row semantics the majority of this file's tests rely on.
func newTestNearbyContactStore(t *testing.T) *nearbyContactStore {
	t.Helper()
	return newTestNearbyContactStoreWithDwell(t, 0)
}

// newTestNearbyContactStoreWithDwell is like newTestNearbyContactStore but
// lets a test exercise the confirmation dwell explicitly, for the
// dwell/confirmation state-machine tests below.
func newTestNearbyContactStoreWithDwell(t *testing.T, dwell time.Duration) *nearbyContactStore {
	t.Helper()
	dir := t.TempDir()
	store, err := newNearbyContactStore(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatalf("newNearbyContactStore: %v", err)
	}
	store.dwell = dwell
	t.Cleanup(func() { _ = store.close() })
	return store
}

// countRows returns the raw row count for vesselKey, bypassing summary()'s
// "exclude the current ongoing encounter" contract - used by tests that
// want to assert how many rows recordContactIfNew actually inserted, as
// opposed to summary()'s prior-encounters count.
func countRows(t *testing.T, store *nearbyContactStore, vesselKey string) int {
	t.Helper()
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM nearby_vessel_contacts WHERE vessel_key = ?`, vesselKey).Scan(&count); err != nil {
		t.Fatalf("count rows for %s: %v", vesselKey, err)
	}
	return count
}

func TestRecordContactIfNew_SameVesselWithinSessionGapInsertsOnlyOneRow(t *testing.T) {
	store := newTestNearbyContactStore(t)
	base := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)

	if err := store.recordContactIfNew("316042555", "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", base, base); err != nil {
		t.Fatalf("recordContactIfNew (1st tick): %v", err)
	}
	// Second tick 5 seconds later, well within the 1-hour session gap.
	if err := store.recordContactIfNew("316042555", "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", base.Add(5*time.Second), base.Add(5*time.Second)); err != nil {
		t.Fatalf("recordContactIfNew (2nd tick): %v", err)
	}
	// Third tick 20 minutes later, still within the gap.
	if err := store.recordContactIfNew("316042555", "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", base.Add(20*time.Minute), base.Add(20*time.Minute)); err != nil {
		t.Fatalf("recordContactIfNew (3rd tick): %v", err)
	}

	if got := countRows(t, store, "316042555"); got != 1 {
		t.Fatalf("expected 1 row for a single encounter, got %d", got)
	}
}

func TestRecordContactIfNew_AfterGapElapsedInsertsSecondRow(t *testing.T) {
	store := newTestNearbyContactStore(t)
	base := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)

	if err := store.recordContactIfNew("316042555", "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", base, base); err != nil {
		t.Fatalf("recordContactIfNew (encounter 1): %v", err)
	}
	// Gap of 61 minutes (> the 1-hour contactSessionGap) is a new encounter.
	second := base.Add(61 * time.Minute)
	if err := store.recordContactIfNew("316042555", "TAKU X", -21.60, 149.80, "Airlie Beach", "motoring", second, second); err != nil {
		t.Fatalf("recordContactIfNew (encounter 2): %v", err)
	}

	if got := countRows(t, store, "316042555"); got != 2 {
		t.Fatalf("expected 2 rows for two distinct encounters, got %d", got)
	}
	// summary() reports the prior encounter (the first one), not the
	// current ongoing one (the second), per its "exclude current" contract.
	seenCount, lastSeenAt, err := store.summary("316042555")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if seenCount != 1 {
		t.Fatalf("expected seenCount 1 (the first encounter, prior to the current ongoing one), got %d", seenCount)
	}
	if !lastSeenAt.Equal(base) {
		t.Fatalf("expected last seen at %s (the first, prior encounter), got %s", base, lastSeenAt)
	}
}

// TestRecordContactIfNew_FortyFiveMinuteGapStaysSameEncounter demonstrates
// the behavior enabled by widening contactSessionGap from 30 minutes to 1
// hour: a 45-minute quiet period used to exceed the old 30-minute gap and
// start a new encounter, but must now collapse into the same encounter
// (single row) since it's under the new 1-hour threshold.
func TestRecordContactIfNew_FortyFiveMinuteGapStaysSameEncounter(t *testing.T) {
	store := newTestNearbyContactStore(t)
	base := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)

	if err := store.recordContactIfNew("316042555", "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", base, base); err != nil {
		t.Fatalf("recordContactIfNew (1st tick): %v", err)
	}
	if err := store.recordContactIfNew("316042555", "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", base.Add(45*time.Minute), base.Add(45*time.Minute)); err != nil {
		t.Fatalf("recordContactIfNew (2nd tick, 45 min later): %v", err)
	}

	if got := countRows(t, store, "316042555"); got != 1 {
		t.Fatalf("expected 1 row: a 45-minute gap should stay within the same encounter under the 1-hour contactSessionGap, got %d", got)
	}
}

// TestSummary_ExcludesCurrentOngoingEncounterFromPriorCount is the
// regression test for Bug 2: summary() must report encounters *prior to*
// the current, still-ongoing one, not the raw total row count. With 3
// distinct encounters recorded, the most recent one is the "current"
// encounter and must be excluded, leaving 2 prior encounters with
// lastSeenAt equal to the second-most-recent row's timestamp.
//
// Spacing is 2 hours (> the 1-hour contactSessionGap) rather than exactly
// 1 hour: recordContactIfNew's comparison is strictly
// "now.Sub(last) > contactSessionGap", so a gap exactly equal to the
// threshold would not count as a new encounter and this test would collapse
// to a single row instead of exercising 3 distinct ones.
// TestRecordContactIfNew_SurvivesProcessRestart is the regression test for
// Bug 1: the in-memory lastSeen map is process-lifetime only, so a real
// backend restart wipes it. Without falling back to the database on a cold
// cache, the first poll tick after a restart would treat every
// currently-visible vessel as brand new and insert a duplicate row, even
// though the vessel has been continuously in range. This test simulates a
// restart by closing the store and reopening a *new* store instance
// pointed at the same DB file (so lastSeen starts genuinely empty, just
// like after a real process restart) and recording the same vessel again
// shortly after, well within contactSessionGap.
func TestRecordContactIfNew_SurvivesProcessRestart(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite")
	base := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)

	store1, err := newNearbyContactStore(dbPath)
	if err != nil {
		t.Fatalf("newNearbyContactStore (1st process): %v", err)
	}
	// Zero dwell: this test is about lastSeen surviving a restart, not about
	// the confirmation dwell, so confirm on the first tick as before dwell
	// existed.
	store1.dwell = 0
	if err := store1.recordContactIfNew("316042555", "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", base, base); err != nil {
		t.Fatalf("recordContactIfNew (before restart): %v", err)
	}
	if err := store1.close(); err != nil {
		t.Fatalf("close store1: %v", err)
	}

	// Simulate a restart: a brand new store instance means a genuinely
	// empty in-memory lastSeen map, exactly like a real process restart.
	store2, err := newNearbyContactStore(dbPath)
	if err != nil {
		t.Fatalf("newNearbyContactStore (2nd process): %v", err)
	}
	store2.dwell = 0
	t.Cleanup(func() { _ = store2.close() })

	// 5 seconds later, well within contactSessionGap - a real continuation
	// of the same encounter, not a new one.
	if err := store2.recordContactIfNew("316042555", "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", base.Add(5*time.Second), base.Add(5*time.Second)); err != nil {
		t.Fatalf("recordContactIfNew (after restart): %v", err)
	}

	var rowCount int
	if err := store2.db.QueryRow(`SELECT COUNT(*) FROM nearby_vessel_contacts WHERE vessel_key = ?`, "316042555").Scan(&rowCount); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("expected 1 row to survive a restart within the session gap, got %d (restart falsely started a new encounter)", rowCount)
	}
}

// TestRecordContactIfNew_GapExceedsSessionGapButPositionUnchangedStaysSameEncounter
// is the core position-override case: a 90-minute gap exceeds
// contactSessionGap (1 hour) but the vessel's position hasn't moved, so this
// must still collapse into a single row - e.g. an AIS dropout while the
// vessel sits at anchor or a dock.
func TestRecordContactIfNew_GapExceedsSessionGapButPositionUnchangedStaysSameEncounter(t *testing.T) {
	store := newTestNearbyContactStore(t)
	base := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)

	if err := store.recordContactIfNew("316042555", "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", base, base); err != nil {
		t.Fatalf("recordContactIfNew (1st tick): %v", err)
	}
	// 90 minutes later, same position: past contactSessionGap but within
	// contactSessionMaxGapForPositionOverride and unmoved.
	if err := store.recordContactIfNew("316042555", "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", base.Add(90*time.Minute), base.Add(90*time.Minute)); err != nil {
		t.Fatalf("recordContactIfNew (2nd tick, 90 min later, same position): %v", err)
	}

	if got := countRows(t, store, "316042555"); got != 1 {
		t.Fatalf("expected 1 row: a 90-minute gap with unchanged position should stay within the same encounter via the position override, got %d", got)
	}
}

// TestRecordContactIfNew_GapExceedsSessionGapAndPositionMovedInsertsSecondRow
// confirms the position override does NOT apply when the vessel has
// actually relocated: same 90-minute gap as the "unchanged position" case
// above, but this time the position has moved well past
// contactSessionMoveThresholdMeters (~1112m via a 0.010 degree latitude
// shift - comfortably clear of the 750m threshold, unlike the old ~556m
// shift this test used before contactSessionMoveThresholdMeters was widened
// from 100m to 750m), so this must still count as a new encounter.
func TestRecordContactIfNew_GapExceedsSessionGapAndPositionMovedInsertsSecondRow(t *testing.T) {
	store := newTestNearbyContactStore(t)
	base := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)

	if err := store.recordContactIfNew("316042555", "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", base, base); err != nil {
		t.Fatalf("recordContactIfNew (1st tick): %v", err)
	}
	if err := store.recordContactIfNew("316042555", "TAKU X", -21.58, 149.79, "Airlie Beach", "motoring", base.Add(90*time.Minute), base.Add(90*time.Minute)); err != nil {
		t.Fatalf("recordContactIfNew (2nd tick, 90 min later, moved ~1112m): %v", err)
	}

	if got := countRows(t, store, "316042555"); got != 2 {
		t.Fatalf("expected 2 rows: a 90-minute gap combined with a >750m relocation should not trigger the position override, got %d", got)
	}
}

// TestRecordContactIfNew_GapExceeds24HourCapInsertsNewEncounterDespiteUnchangedPosition
// confirms the outer cap: even with the position completely unchanged, a
// 25-hour gap exceeds contactSessionMaxGapForPositionOverride and must
// always be recorded as a new encounter. A vessel silent for over a day
// that reappears in the same spot more plausibly left and came back than
// stayed continuously.
func TestRecordContactIfNew_GapExceeds24HourCapInsertsNewEncounterDespiteUnchangedPosition(t *testing.T) {
	store := newTestNearbyContactStore(t)
	base := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)

	if err := store.recordContactIfNew("316042555", "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", base, base); err != nil {
		t.Fatalf("recordContactIfNew (1st tick): %v", err)
	}
	if err := store.recordContactIfNew("316042555", "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", base.Add(25*time.Hour), base.Add(25*time.Hour)); err != nil {
		t.Fatalf("recordContactIfNew (2nd tick, 25h later, same position): %v", err)
	}

	if got := countRows(t, store, "316042555"); got != 2 {
		t.Fatalf("expected 2 rows: a 25-hour gap must always be a new encounter, regardless of unchanged position, got %d", got)
	}
}

// TestRecordContactIfNew_GapJustUnder24HourCapStaysSameEncounterWhenPositionUnchanged
// is the cap boundary sanity check: a 23-hour gap is still within
// contactSessionMaxGapForPositionOverride, so an unchanged position must
// still collapse into a single row.
func TestRecordContactIfNew_GapJustUnder24HourCapStaysSameEncounterWhenPositionUnchanged(t *testing.T) {
	store := newTestNearbyContactStore(t)
	base := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)

	if err := store.recordContactIfNew("316042555", "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", base, base); err != nil {
		t.Fatalf("recordContactIfNew (1st tick): %v", err)
	}
	if err := store.recordContactIfNew("316042555", "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", base.Add(23*time.Hour), base.Add(23*time.Hour)); err != nil {
		t.Fatalf("recordContactIfNew (2nd tick, 23h later, same position): %v", err)
	}

	if got := countRows(t, store, "316042555"); got != 1 {
		t.Fatalf("expected 1 row: a 23-hour gap is still within the 24-hour position-override cap, got %d", got)
	}
}

// TestRecordContactIfNew_PositionOverrideBoundaryAt750Meters checks both
// sides of the contactSessionMoveThresholdMeters boundary with a fixed
// 90-minute gap (past contactSessionGap, within the 24h cap) and a fixed
// longitude, varying only latitude - latitude degrees are a constant
// ~111,195m (per haversineMeters's earth radius) regardless of longitude,
// unlike longitude degrees which shrink away from the equator, so varying
// latitude keeps the math simple. A ~0.0067 degree shift is ~745m (within
// the 750m threshold, no new row); a ~0.0072 degree shift is ~800m (outside
// it, new row). These replace the old 100m-boundary test now that
// contactSessionMoveThresholdMeters is 750m, per the measured bimodal
// distance distribution (largest non-relocation excursion 555m, smallest
// genuine relocation 5117m) documented on the constant itself.
func TestRecordContactIfNew_PositionOverrideBoundaryAt750Meters(t *testing.T) {
	base := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)
	const baseLat = -21.59
	const baseLon = 149.79

	t.Run("within 750m stays same encounter", func(t *testing.T) {
		store := newTestNearbyContactStore(t)
		if err := store.recordContactIfNew("316042555", "TAKU X", baseLat, baseLon, "Airlie Beach", "anchored", base, base); err != nil {
			t.Fatalf("recordContactIfNew (1st tick): %v", err)
		}
		// ~745m offset: within the 750m threshold.
		if err := store.recordContactIfNew("316042555", "TAKU X", baseLat+0.0067, baseLon, "Airlie Beach", "anchored", base.Add(90*time.Minute), base.Add(90*time.Minute)); err != nil {
			t.Fatalf("recordContactIfNew (2nd tick, ~745m away): %v", err)
		}
		if got := countRows(t, store, "316042555"); got != 1 {
			t.Fatalf("expected 1 row: a ~745m shift is within contactSessionMoveThresholdMeters, got %d", got)
		}
	})

	t.Run("beyond 750m starts new encounter", func(t *testing.T) {
		store := newTestNearbyContactStore(t)
		if err := store.recordContactIfNew("316042555", "TAKU X", baseLat, baseLon, "Airlie Beach", "anchored", base, base); err != nil {
			t.Fatalf("recordContactIfNew (1st tick): %v", err)
		}
		// ~800m offset: outside the 750m threshold.
		if err := store.recordContactIfNew("316042555", "TAKU X", baseLat+0.0072, baseLon, "Airlie Beach", "anchored", base.Add(90*time.Minute), base.Add(90*time.Minute)); err != nil {
			t.Fatalf("recordContactIfNew (2nd tick, ~800m away): %v", err)
		}
		if got := countRows(t, store, "316042555"); got != 2 {
			t.Fatalf("expected 2 rows: a ~800m shift exceeds contactSessionMoveThresholdMeters, got %d", got)
		}
	})
}

// TestRecordContactIfNew_SurvivesProcessRestartWithPositionOverride is a
// sibling of TestRecordContactIfNew_SurvivesProcessRestart: it confirms the
// position override also applies via the cold-cache database fallback
// (lastRecordedContact), not just the hot in-memory path. The simulated
// restart happens after a 90-minute gap (past contactSessionGap, within the
// 24h cap) with the position unchanged, so the post-restart tick must still
// be treated as the same encounter.
func TestRecordContactIfNew_SurvivesProcessRestartWithPositionOverride(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite")
	base := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)

	store1, err := newNearbyContactStore(dbPath)
	if err != nil {
		t.Fatalf("newNearbyContactStore (1st process): %v", err)
	}
	// Zero dwell: this test is about the position override surviving a
	// restart via the DB fallback, not about the confirmation dwell, so
	// confirm on the first tick as before dwell existed.
	store1.dwell = 0
	if err := store1.recordContactIfNew("316042555", "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", base, base); err != nil {
		t.Fatalf("recordContactIfNew (before restart): %v", err)
	}
	if err := store1.close(); err != nil {
		t.Fatalf("close store1: %v", err)
	}

	// Simulate a restart: a brand new store instance means a genuinely
	// empty in-memory lastSeen map, exactly like a real process restart.
	store2, err := newNearbyContactStore(dbPath)
	if err != nil {
		t.Fatalf("newNearbyContactStore (2nd process): %v", err)
	}
	store2.dwell = 0
	t.Cleanup(func() { _ = store2.close() })

	// 90 minutes later, same position: past contactSessionGap but within
	// the position-override cap and unmoved. The in-memory map is cold
	// (post-restart), so this exercises the DB fallback's lastRecordedContact
	// lookup rather than the in-memory lastSeen map.
	if err := store2.recordContactIfNew("316042555", "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", base.Add(90*time.Minute), base.Add(90*time.Minute)); err != nil {
		t.Fatalf("recordContactIfNew (after restart): %v", err)
	}

	if got := countRows(t, store2, "316042555"); got != 1 {
		t.Fatalf("expected 1 row: the position override must apply across the cold-cache DB fallback too, got %d (restart falsely started a new encounter)", got)
	}
}

func TestSummary_ExcludesCurrentOngoingEncounterFromPriorCount(t *testing.T) {
	store := newTestNearbyContactStore(t)
	base := time.Date(2026, time.July, 12, 8, 0, 0, 0, time.UTC)

	// Each recording is spaced more than contactSessionGap apart AND more
	// than contactSessionMoveThresholdMeters apart, so each one lands as a
	// distinct encounter (distinct row): a time gap alone is no longer
	// sufficient to force a new encounter (see the position override in
	// recordContactIfNew), so this test also moves the position each time
	// by ~1.5km, well past the 100m threshold, to keep testing genuinely
	// distinct encounters rather than accidentally exercising the
	// position-override continuation path.
	times := []time.Time{
		base,
		base.Add(2 * time.Hour),
		base.Add(4 * time.Hour),
	}
	positions := [][2]float64{
		{-21.59, 149.79},
		{-21.60, 149.80},
		{-21.61, 149.81},
	}
	for i, ts := range times {
		lat, lon := positions[i][0], positions[i][1]
		if err := store.recordContactIfNew("316042555", "TAKU X", lat, lon, "Airlie Beach", "anchored", ts, ts); err != nil {
			t.Fatalf("recordContactIfNew: %v", err)
		}
	}

	seenCount, lastSeenAt, err := store.summary("316042555")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if seenCount != 2 {
		t.Fatalf("expected seenCount 2 (3 total rows minus the current ongoing encounter), got %d", seenCount)
	}
	want := times[1] // second-most-recent row, i.e. the most recent *prior* encounter
	if !lastSeenAt.Equal(want) {
		t.Fatalf("expected lastSeenAt %s, got %s", want, lastSeenAt)
	}
}

// TestSummary_SingleRowReturnsZeroPriorSightings is the regression test for
// Bug 2's core symptom: a vessel's very first-ever sighting has exactly one
// recorded row (its own current, ongoing encounter, inserted by the poller
// moments ago) and no priors, so summary() must report 0/zero-time rather
// than counting that row as a sighting of itself.
func TestSummary_SingleRowReturnsZeroPriorSightings(t *testing.T) {
	store := newTestNearbyContactStore(t)
	base := time.Date(2026, time.July, 12, 8, 0, 0, 0, time.UTC)

	if err := store.recordContactIfNew("316042555", "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", base, base); err != nil {
		t.Fatalf("recordContactIfNew: %v", err)
	}

	seenCount, lastSeenAt, err := store.summary("316042555")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if seenCount != 0 {
		t.Fatalf("expected seenCount 0 for a vessel's first-ever (and only) sighting, got %d", seenCount)
	}
	if !lastSeenAt.IsZero() {
		t.Fatalf("expected zero-value lastSeenAt for a vessel's first-ever sighting, got %s", lastSeenAt)
	}
}

func TestSummary_UnknownVesselReturnsZeroValue(t *testing.T) {
	store := newTestNearbyContactStore(t)

	seenCount, lastSeenAt, err := store.summary("no-such-vessel")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if seenCount != 0 {
		t.Fatalf("expected seenCount 0 for unknown vessel, got %d", seenCount)
	}
	if !lastSeenAt.IsZero() {
		t.Fatalf("expected zero lastSeenAt for unknown vessel, got %s", lastSeenAt)
	}
}

func TestListSightings_ReturnsNewestFirst(t *testing.T) {
	store := newTestNearbyContactStore(t)
	base := time.Date(2026, time.July, 12, 8, 0, 0, 0, time.UTC)

	first := base
	second := base.Add(1 * time.Hour)
	if err := store.recordContactIfNew("316042555", "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", first, first); err != nil {
		t.Fatalf("recordContactIfNew (1st): %v", err)
	}
	if err := store.recordContactIfNew("316042555", "TAKU X", -21.61, 149.81, "Nara Inlet", "motoring", second.Add(31*time.Minute+time.Hour), second.Add(31*time.Minute+time.Hour)); err != nil {
		t.Fatalf("recordContactIfNew (2nd): %v", err)
	}

	sightings, err := store.listSightings("316042555")
	if err != nil {
		t.Fatalf("listSightings: %v", err)
	}
	if len(sightings) != 2 {
		t.Fatalf("expected 2 sightings, got %d", len(sightings))
	}
	if !sightings[0].SeenAt.After(sightings[1].SeenAt) {
		t.Fatalf("expected newest-first ordering, got %s then %s", sightings[0].SeenAt, sightings[1].SeenAt)
	}
	if sightings[0].Geoname != "Nara Inlet" || sightings[0].NavContext != "motoring" {
		t.Fatalf("expected newest sighting to be the Nara Inlet/motoring one, got %+v", sightings[0])
	}
	if sightings[1].Geoname != "Airlie Beach" || sightings[1].NavContext != "anchored" {
		t.Fatalf("expected oldest sighting to be the Airlie Beach/anchored one, got %+v", sightings[1])
	}
}

// TestGetNearbyVesselSightingsHandler_DecodesURLEncodedKeyParam exercises the
// real Echo router (not manual c.SetParamValues, which bypasses URL
// decoding entirely) with a request path built the same way the frontend
// builds it: encodeURIComponent("name:TAKU X") -> "name%3ATAKU%20X". Echo's
// router matches on the request's escaped path and does NOT url-decode
// route params for you - c.Param("key") returns the raw
// "name%3ATAKU%20X" unless the handler decodes it itself, which would never
// match any vessel_key stored in the database (those are stored decoded,
// e.g. by the poller in tracks.go). This is a regression test for that.
func TestGetNearbyVesselSightingsHandler_DecodesURLEncodedKeyParam(t *testing.T) {
	store := newTestNearbyContactStore(t)
	seenAt := time.Date(2026, time.July, 10, 9, 14, 0, 0, time.UTC)
	if err := store.recordContactIfNew("name:TAKU X", "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", seenAt, seenAt); err != nil {
		t.Fatalf("recordContactIfNew: %v", err)
	}

	e := echo.New()
	e.GET("/api/nearby-vessels/:key/sightings", getNearbyVesselSightingsHandler(store))

	req := httptest.NewRequest(http.MethodGet, "/api/nearby-vessels/name%3ATAKU%20X/sightings", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Sightings []nearbyVesselSightingWire `json:"sightings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Sightings) != 1 {
		t.Fatalf("expected 1 sighting for the URL-encoded key to resolve to the stored vessel_key, got %d: %s", len(resp.Sightings), rec.Body.String())
	}
	if resp.Sightings[0].Geoname != "Airlie Beach" {
		t.Fatalf("expected the Airlie Beach sighting, got %+v", resp.Sightings[0])
	}
}

func TestVesselContactKey_ReturnsMMSI(t *testing.T) {
	key, ok := vesselContactKey("316042555")
	if !ok {
		t.Fatalf("expected ok=true for a non-empty MMSI")
	}
	if key != "316042555" {
		t.Fatalf("expected MMSI key, got %q", key)
	}
}

func TestVesselContactKey_NotOkWhenMMSIEmpty(t *testing.T) {
	if _, ok := vesselContactKey(""); ok {
		t.Fatalf("expected ok=false for empty MMSI")
	}
	if _, ok := vesselContactKey("   "); ok {
		t.Fatalf("expected ok=false for whitespace-only MMSI")
	}
}

// ── Confirmation dwell state machine (nearby_contacts.go's pendingContact) ──

// TestRecordContactIfNew_RingGrazeBlipDoesNotBecomeASightingButGenuineArrivalDoes
// is the regression test for the bug this whole change fixes: Osprey IV
// showed two sighting-history rows an hour apart for what was a single
// visit. One tick placed it just inside the 5000m detection ring (an
// optimistic range fix on a boat actually anchored just outside it), then it
// went silent for over an hour, then it genuinely motored over and anchored
// - using the real timestamps from that incident (2026-08-20 04:28:23 and
// 05:28:39 UTC). With the confirmation dwell in place, the lone ring-graze
// tick never accumulates the required dwell before the 61-minute silence
// resets it, so it produces no row at all; only the genuine arrival, once
// confirmed, produces the single expected row - backdated to its first
// tick, not whatever tick happened to cross the dwell threshold.
func TestRecordContactIfNew_RingGrazeBlipDoesNotBecomeASightingButGenuineArrivalDoes(t *testing.T) {
	store := newTestNearbyContactStoreWithDwell(t, contactConfirmDwell)
	const vesselKey = "231234567"

	// The lone ring-graze blip: one optimistic fix, never followed up.
	blipAt := time.Date(2026, time.August, 20, 4, 28, 23, 0, time.UTC)
	if err := store.recordContactIfNew(vesselKey, "OSPREY IV", -20.83767, 149.23831, "Keswick Island", "anchored", blipAt, blipAt); err != nil {
		t.Fatalf("recordContactIfNew (ring-graze blip): %v", err)
	}

	// Silence for 61 minutes - well past contactConfirmMaxTickGap - so the
	// blip's pending candidate is discarded rather than resumed.
	arrivalStart := time.Date(2026, time.August, 20, 5, 28, 39, 0, time.UTC)
	arrivalLat, arrivalLon := -20.79810, 149.26460 // genuinely motored over and anchored here, 5179m away
	posSeenAtArrival := arrivalStart
	posSeenRefreshed := arrivalStart.Add(2 * time.Minute)

	// The genuine arrival: polled every 5s, exactly like the real 5s server
	// poller, for the full contactConfirmDwell (5 minutes), with the AIS
	// position refreshing partway through. navContext changes from
	// "motoring" (still underway on arrival) to "anchored" (settled) partway
	// through, so the confirmed row can be checked against the *first*
	// tick's value, not the last.
	for elapsed := 0 * time.Second; elapsed <= contactConfirmDwell; elapsed += 5 * time.Second {
		tick := arrivalStart.Add(elapsed)
		posSeen := posSeenAtArrival
		if elapsed >= 2*time.Minute {
			posSeen = posSeenRefreshed
		}
		navContext := "motoring"
		if elapsed >= 1*time.Minute {
			navContext = "anchored"
		}
		if err := store.recordContactIfNew(vesselKey, "OSPREY IV", arrivalLat, arrivalLon, "Keswick Island", navContext, posSeen, tick); err != nil {
			t.Fatalf("recordContactIfNew (arrival tick at +%s): %v", elapsed, err)
		}
	}

	if got := countRows(t, store, vesselKey); got != 1 {
		t.Fatalf("expected exactly 1 row (the ring-graze blip must not become a sighting), got %d", got)
	}

	sightings, err := store.listSightings(vesselKey)
	if err != nil {
		t.Fatalf("listSightings: %v", err)
	}
	if !sightings[0].SeenAt.Equal(arrivalStart) {
		t.Fatalf("expected the row backdated to the arrival's first tick %s, got %s", arrivalStart, sightings[0].SeenAt)
	}
	if sightings[0].NavContext != "motoring" {
		t.Fatalf("expected the row to carry the arrival's first-tick nav_context %q, not a later tick's, got %q", "motoring", sightings[0].NavContext)
	}
}

// TestRecordContactIfNew_DwellConfirmationBackdatesToFirstTick is the core
// dwell-confirmation case: ticks every 5 seconds (mirroring the real 5s
// server poller) for the full contactConfirmDwell, with the AIS position
// refreshing partway through, produces exactly one row - backdated to the
// very first tick, not the tick that happened to cross the dwell threshold.
func TestRecordContactIfNew_DwellConfirmationBackdatesToFirstTick(t *testing.T) {
	store := newTestNearbyContactStoreWithDwell(t, contactConfirmDwell)
	const vesselKey = "316042555"
	start := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)
	posSeenA := start
	posSeenB := start.Add(2 * time.Minute)

	for elapsed := 0 * time.Second; elapsed <= contactConfirmDwell; elapsed += 5 * time.Second {
		tick := start.Add(elapsed)
		posSeen := posSeenA
		if elapsed >= 2*time.Minute {
			posSeen = posSeenB
		}
		if err := store.recordContactIfNew(vesselKey, "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", posSeen, tick); err != nil {
			t.Fatalf("recordContactIfNew (tick at +%s): %v", elapsed, err)
		}
	}

	if got := countRows(t, store, vesselKey); got != 1 {
		t.Fatalf("expected exactly 1 row once the dwell elapses with a refreshed position, got %d", got)
	}
	sightings, err := store.listSightings(vesselKey)
	if err != nil {
		t.Fatalf("listSightings: %v", err)
	}
	if !sightings[0].SeenAt.Equal(start) {
		t.Fatalf("expected the row backdated to the first tick %s, got %s", start, sightings[0].SeenAt)
	}
}

// TestRecordContactIfNew_DwellNotYetElapsedInsertsNoRow confirms a candidate
// that has been ticking continuously, with its position refreshed, for less
// than contactConfirmDwell produces no row at all yet.
func TestRecordContactIfNew_DwellNotYetElapsedInsertsNoRow(t *testing.T) {
	store := newTestNearbyContactStoreWithDwell(t, contactConfirmDwell)
	const vesselKey = "316042555"
	start := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)
	posSeenA := start
	posSeenB := start.Add(2 * time.Minute)

	const dwellShyOf = 4 * time.Minute
	for elapsed := 0 * time.Second; elapsed <= dwellShyOf; elapsed += 5 * time.Second {
		tick := start.Add(elapsed)
		posSeen := posSeenA
		if elapsed >= 2*time.Minute {
			posSeen = posSeenB
		}
		if err := store.recordContactIfNew(vesselKey, "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", posSeen, tick); err != nil {
			t.Fatalf("recordContactIfNew (tick at +%s): %v", elapsed, err)
		}
	}

	if got := countRows(t, store, vesselKey); got != 0 {
		t.Fatalf("expected 0 rows: 4 minutes of ticking is still short of the 5-minute contactConfirmDwell, got %d", got)
	}
}

// TestRecordContactIfNew_DwellInterruptedByGapRestartsAndInsertsNoRow
// confirms a gap wider than contactConfirmMaxTickGap restarts the dwell
// timer from scratch rather than resuming it: 4 minutes of continuous
// ticking, then a 60-second gap (4x contactConfirmMaxTickGap), then 4 more
// minutes of ticking. Neither stretch alone reaches the 5-minute dwell, and
// the second stretch must not inherit elapsed time from the first, so no
// row is ever inserted.
func TestRecordContactIfNew_DwellInterruptedByGapRestartsAndInsertsNoRow(t *testing.T) {
	store := newTestNearbyContactStoreWithDwell(t, contactConfirmDwell)
	const vesselKey = "316042555"
	start := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)

	tickPosSeen := func(stretchStart time.Time, elapsed time.Duration) time.Time {
		if elapsed >= 1*time.Minute {
			return stretchStart.Add(1 * time.Minute)
		}
		return stretchStart
	}

	const stretch = 4 * time.Minute
	for elapsed := 0 * time.Second; elapsed <= stretch; elapsed += 5 * time.Second {
		tick := start.Add(elapsed)
		if err := store.recordContactIfNew(vesselKey, "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", tickPosSeen(start, elapsed), tick); err != nil {
			t.Fatalf("recordContactIfNew (1st stretch, tick at +%s): %v", elapsed, err)
		}
	}

	// A 60s gap - 4x contactConfirmMaxTickGap (15s) - restarts the dwell.
	secondStart := start.Add(stretch).Add(60 * time.Second)
	for elapsed := 0 * time.Second; elapsed <= stretch; elapsed += 5 * time.Second {
		tick := secondStart.Add(elapsed)
		if err := store.recordContactIfNew(vesselKey, "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", tickPosSeen(secondStart, elapsed), tick); err != nil {
			t.Fatalf("recordContactIfNew (2nd stretch, tick at +%s): %v", elapsed, err)
		}
	}

	if got := countRows(t, store, vesselKey); got != 0 {
		t.Fatalf("expected 0 rows: neither 4-minute stretch alone reaches the 5-minute dwell, and the gap must discard the first stretch rather than letting it carry over, got %d", got)
	}
}

// TestRecordContactIfNew_StalePositionNeverConfirms confirms that ticking
// continuously for far longer than contactConfirmDwell is not sufficient on
// its own: if the AIS position is never refreshed (positionSeen never
// changes from its value at dwell start), the candidate never confirms,
// however long it keeps ticking. This is what stops a frozen optimistic fix
// (ADR 0042's staleness gap) from ever being mistaken for a real, continuing
// presence.
func TestRecordContactIfNew_StalePositionNeverConfirms(t *testing.T) {
	store := newTestNearbyContactStoreWithDwell(t, contactConfirmDwell)
	const vesselKey = "316042555"
	start := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)
	posSeen := start // never changes across any tick below

	const tickFor = 10 * time.Minute // double the 5-minute dwell
	for elapsed := 0 * time.Second; elapsed <= tickFor; elapsed += 5 * time.Second {
		tick := start.Add(elapsed)
		if err := store.recordContactIfNew(vesselKey, "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", posSeen, tick); err != nil {
			t.Fatalf("recordContactIfNew (tick at +%s): %v", elapsed, err)
		}
	}

	if got := countRows(t, store, vesselKey); got != 0 {
		t.Fatalf("expected 0 rows: a position that never refreshes must never confirm, however long the dwell has elapsed, got %d", got)
	}
}
