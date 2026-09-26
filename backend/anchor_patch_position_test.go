package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This file covers patchAnchorWatch's new lat/lon handling (the Adjust mode
// plan, part 1): PATCH gains an atomic position+radius write alongside the
// fields it already patched, publishing to SignalK like setAnchorWatch's own
// reposition path (ADR 0133) but — unlike that path — persisting nothing and
// reporting 502 when the publish fails, since a PATCH is a deliberate,
// explicit adjustment rather than the "SignalK is down, keep the local
// safety watch anyway" case POST/drop exists for.

// Test: lat and lon must arrive together — a lone one is rejected before any
// state is touched.
func TestPatchAnchorWatch_PositionRequiresBothLatAndLon(t *testing.T) {
	anchorPublishEnv(t)
	if code, resp := postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0}); code != http.StatusOK {
		t.Fatalf("drop: %d %v", code, resp)
	}

	if code, resp := patchAnchorWatchForTest(t, map[string]any{"lat": -20.01}); code != http.StatusBadRequest {
		t.Fatalf("expected 400 for lat without lon, got %d: %+v", code, resp)
	}
	if code, resp := patchAnchorWatchForTest(t, map[string]any{"lon": 149.01}); code != http.StatusBadRequest {
		t.Fatalf("expected 400 for lon without lat, got %d: %+v", code, resp)
	}
}

// Test: lat/lon are range-validated the same way setAnchorWatch validates them.
func TestPatchAnchorWatch_PositionValidatesRange(t *testing.T) {
	anchorPublishEnv(t)
	if code, resp := postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0}); code != http.StatusOK {
		t.Fatalf("drop: %d %v", code, resp)
	}

	if code, resp := patchAnchorWatchForTest(t, map[string]any{"lat": 95.0, "lon": 149.0}); code != http.StatusBadRequest {
		t.Fatalf("expected 400 for out-of-range lat, got %d: %+v", code, resp)
	}
	if code, resp := patchAnchorWatchForTest(t, map[string]any{"lat": -20.0, "lon": 185.0}); code != http.StatusBadRequest {
		t.Fatalf("expected 400 for out-of-range lon, got %d: %+v", code, resp)
	}
}

// Test: a PATCH carrying lat/lon and radius_meters together lands both in one
// write, and publishes the new position to SignalK.
func TestPatchAnchorWatch_PositionAndRadiusIsOneAtomicWrite(t *testing.T) {
	stub := anchorPublishEnv(t)
	if code, resp := postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0}); code != http.StatusOK {
		t.Fatalf("drop: %d %v", code, resp)
	}

	code, resp := patchAnchorWatchForTest(t, map[string]any{
		"lat":           -20.001,
		"lon":           149.001,
		"radius_meters": 35.0,
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", code, resp)
	}
	if got, _ := resp["lat"].(float64); got != -20.001 {
		t.Fatalf("lat not updated: %v", resp["lat"])
	}
	if got, _ := resp["lon"].(float64); got != 149.001 {
		t.Fatalf("lon not updated: %v", resp["lon"])
	}
	if got, _ := resp["radius_meters"].(float64); got != 35.0 {
		t.Fatalf("radius not updated: %v", resp["radius_meters"])
	}

	frames := stub.captured()
	if len(frames) != 2 { // the drop, then the adjust
		t.Fatalf("expected two published deltas (drop, adjust), got %d", len(frames))
	}
	var delta signalKDelta
	if err := json.Unmarshal(frames[1], &delta); err != nil {
		t.Fatal(err)
	}
	pos, ok := delta.Updates[0].Values[0].Value.(map[string]any)
	if !ok || pos["latitude"] != -20.001 || pos["longitude"] != 149.001 {
		t.Fatalf("published wrong position: %v", delta.Updates[0].Values[0].Value)
	}
}

// Test: a radius-only PATCH (the existing, unchanged path) must not publish
// anything to SignalK — only a position change does.
func TestPatchAnchorWatch_RadiusOnlyDoesNotPublishPosition(t *testing.T) {
	stub := anchorPublishEnv(t)
	if code, resp := postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0}); code != http.StatusOK {
		t.Fatalf("drop: %d %v", code, resp)
	}
	if code, resp := patchAnchorWatchForTest(t, map[string]any{"radius_meters": 40.0}); code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", code, resp)
	}
	if len(stub.captured()) != 1 { // only the drop
		t.Fatalf("radius-only PATCH must not publish position, got %d frames", len(stub.captured()))
	}
}

// Test: when the publish fails, patchAnchorWatch reports 502 and persists
// nothing — neither the in-memory state nor the file on disk move to the new
// position. This is the opposite of setAnchorWatch's own reposition path,
// which keeps the local watch as a safety net on a failed publish; PATCH is a
// deliberate Adjust-mode write, not that emergency case.
func TestPatchAnchorWatch_PositionPublishFailureReturns502AndDoesNotPersist(t *testing.T) {
	shortenSignalKConfirmWindow(t)
	stub := anchorPublishEnv(t)
	if code, resp := postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0}); code != http.StatusOK {
		t.Fatalf("drop: %d %v", code, resp)
	}
	stub.mu.Lock()
	stub.ingest = false
	stub.mu.Unlock()

	code, resp := patchAnchorWatchForTest(t, map[string]any{"lat": -20.5, "lon": 149.5})
	if code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %+v", code, resp)
	}

	anchorWatchMu.RLock()
	lat, lon := anchorWatchState.Lat, anchorWatchState.Lon
	anchorWatchMu.RUnlock()
	if lat != -20.0 || lon != 149.0 {
		t.Fatalf("failed publish must not move the in-memory position, got %v,%v", lat, lon)
	}

	raw, err := os.ReadFile(anchorWatchFilePath())
	if err != nil {
		t.Fatalf("read anchor_watch.json: %v", err)
	}
	var onDisk anchorWatchData
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("parse anchor_watch.json: %v", err)
	}
	if onDisk.Lat != -20.0 || onDisk.Lon != 149.0 {
		t.Fatalf("failed publish must not persist the new position, got %v,%v", onDisk.Lat, onDisk.Lon)
	}
}

// Test: a radius_meters PATCH alongside a failed lat/lon change must also not
// land — the whole PATCH is one atomic write, so the radius must not move
// either when the position half fails.
func TestPatchAnchorWatch_PublishFailureDoesNotPersistRadiusEither(t *testing.T) {
	shortenSignalKConfirmWindow(t)
	stub := anchorPublishEnv(t)
	if code, resp := postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0, "radius_meters": 20.0}); code != http.StatusOK {
		t.Fatalf("drop: %d %v", code, resp)
	}
	stub.mu.Lock()
	stub.ingest = false
	stub.mu.Unlock()

	code, _ := patchAnchorWatchForTest(t, map[string]any{"lat": -20.5, "lon": 149.5, "radius_meters": 99.0})
	if code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", code)
	}

	anchorWatchMu.RLock()
	radius := anchorWatchState.RadiusMeters
	anchorWatchMu.RUnlock()
	if radius != 20.0 {
		t.Fatalf("failed publish must not persist the radius either, got %v", radius)
	}
}

// Test: set_at identifies the anchoring session and must survive a PATCH
// reposition exactly like it survives a POST reposition (Test 19 in
// anchor_test.go).
func TestPatchAnchorWatch_PositionKeepsSetAt(t *testing.T) {
	anchorPublishEnv(t)
	_, dropResp := postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0})
	setAt, _ := dropResp["set_at"].(string)
	if setAt == "" {
		t.Fatalf("expected a set_at from the drop, got %+v", dropResp)
	}

	code, resp := patchAnchorWatchForTest(t, map[string]any{"lat": -20.5, "lon": 149.5})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", code, resp)
	}
	if got, _ := resp["set_at"].(string); got != setAt {
		t.Fatalf("expected set_at %q to survive the PATCH reposition, got %q", setAt, got)
	}
}

// Test: rode/sea-state/seabed and the planning depth pair — everything a
// field-only PATCH already carries forward from `current` — must keep
// carrying forward when the same PATCH also moves the anchor.
func TestPatchAnchorWatch_PositionCarriesOtherFieldsForward(t *testing.T) {
	anchorPublishEnv(t)
	if code, resp := postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0}); code != http.StatusOK {
		t.Fatalf("drop: %d %v", code, resp)
	}
	if code, resp := patchAnchorWatchForTest(t, map[string]any{
		"rode_deployed_m": 42.5,
		"sea_state":       "choppy",
		"seabed_type":     "mud",
	}); code != http.StatusOK {
		t.Fatalf("expected 200 from the rode/conditions PATCH, got %d: %+v", code, resp)
	}

	code, resp := patchAnchorWatchForTest(t, map[string]any{"lat": -20.5, "lon": 149.5})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", code, resp)
	}
	if got, _ := resp["rode_deployed_m"].(float64); got != 42.5 {
		t.Fatalf("expected rode_deployed_m 42.5 to survive the position PATCH, got %v", resp["rode_deployed_m"])
	}
	if got, _ := resp["sea_state"].(string); got != "choppy" {
		t.Fatalf("expected sea_state to survive the position PATCH, got %q", got)
	}
	if got, _ := resp["seabed_type"].(string); got != "mud" {
		t.Fatalf("expected seabed_type to survive the position PATCH, got %q", got)
	}
}

// Test: a position change resets the post-anchor self trail, the same way a
// POST reposition does — the trail is a record of the boat's movement
// relative to THIS anchor position, and stops meaning anything once the
// anchor moves.
func TestPatchAnchorWatch_PositionResetsSelfTrail(t *testing.T) {
	anchorPublishEnv(t)
	if code, resp := postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0}); code != http.StatusOK {
		t.Fatalf("drop: %d %v", code, resp)
	}
	recordSelfTrailPoint(-20.0001, 149.0001)
	recordSelfTrailPoint(-20.0002, 149.0002)
	if pts := getSelfTrailSince(time.Time{}); len(pts) != 2 {
		t.Fatalf("expected 2 trail points before the adjust, got %d", len(pts))
	}

	if code, resp := patchAnchorWatchForTest(t, map[string]any{"lat": -20.5, "lon": 149.5}); code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", code, resp)
	}

	if pts := getSelfTrailSince(time.Time{}); len(pts) != 0 {
		t.Fatalf("expected the trail reset after a position adjust, got %d points", len(pts))
	}
}

// Test: the opposite of the above — a radius-only PATCH must not reset the
// trail, since the anchor position (what the trail is measured against)
// hasn't moved.
func TestPatchAnchorWatch_RadiusOnlyDoesNotResetSelfTrail(t *testing.T) {
	anchorPublishEnv(t)
	if code, resp := postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0}); code != http.StatusOK {
		t.Fatalf("drop: %d %v", code, resp)
	}
	recordSelfTrailPoint(-20.0001, 149.0001)

	if code, resp := patchAnchorWatchForTest(t, map[string]any{"radius_meters": 40.0}); code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", code, resp)
	}

	if pts := getSelfTrailSince(time.Time{}); len(pts) != 1 {
		t.Fatalf("expected the trail untouched by a radius-only PATCH, got %d points", len(pts))
	}
}

// Test: PATCHing lat/lon with no active watch reports 404, matching the
// existing field-only PATCH behaviour, and publishes nothing.
func TestPatchAnchorWatch_PositionWithNoActiveWatchReturns404(t *testing.T) {
	stub := anchorPublishEnv(t) // starts with anchorWatchState nil
	code, resp := patchAnchorWatchForTest(t, map[string]any{"lat": -20.0, "lon": 149.0})
	if code != http.StatusNotFound {
		t.Fatalf("expected 404 with no active watch, got %d: %+v", code, resp)
	}
	if len(stub.captured()) != 0 {
		t.Fatalf("must not publish with no active watch, got %d frames", len(stub.captured()))
	}
}

// Test (code-review finding): when a position-changing PATCH's SignalK
// publish SUCCEEDS but the subsequent local save fails, the operator must be
// told SignalK already moved and the local record didn't — not the same
// generic "failed to persist" a field-only PATCH's save failure gets, which
// says nothing about the two systems now disagreeing.
func TestPatchAnchorWatch_PositionSaveFailureIsExplicitAndLoud(t *testing.T) {
	stub := anchorPublishEnv(t)
	if code, resp := postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0}); code != http.StatusOK {
		t.Fatalf("drop: %d %v", code, resp)
	}

	// Break the local save path only now, after the drop already landed
	// successfully — the PATCH below must still publish to SignalK (the stub
	// is untouched) before persisting fails.
	notADir := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(notADir, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANCHOR_WATCH_FILE", filepath.Join(notADir, "anchor.json"))

	code, resp := patchAnchorWatchForTest(t, map[string]any{"lat": -20.5, "lon": 149.5})
	if code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %+v", code, resp)
	}
	msg, _ := resp["error"].(string)
	if !strings.Contains(msg, "SignalK") || !strings.Contains(strings.ToLower(msg), "local") {
		t.Fatalf("expected an explicit SignalK-updated-but-local-save-failed message, got %q", msg)
	}

	// Confirms the scenario the message describes actually happened: the
	// PATCH really did publish the new position before the save failed.
	if frames := stub.captured(); len(frames) != 2 { // the drop, then the PATCH's own publish
		t.Fatalf("expected the PATCH to have published before the save failed, got %d frames", len(frames))
	}
}

// Test (code-review finding, revised): the doc comment above patchAnchorWatch
// used to claim that nothing could run in the gap it opens around the
// SignalK publish while repositioning, because a second PATCH/POST/DELETE is
// ruled out by anchorLifecycleMu. That's true for another lifecycle write,
// but the background place-name resolver (place_name.go's
// resolveAndPinAnchorWatchPlaceName) isn't gated by anchorLifecycleMu at
// all — it takes anchorWatchMu directly — and so it CAN pin a name onto
// anchorWatchState during exactly that gap. A first version of this fix
// carried whatever pinned name it found there forward into `updated`, on the
// theory that it was "freshly resolved" and so worth keeping.
//
// That theory doesn't hold for a POSITION-changing PATCH: the racer below
// simulates a resolve that was already in flight for the OLD point (kicked
// off before this PATCH ever started) landing during the publish gap - it
// is not a resolve for the NEW point the anchor is being moved to, and
// carrying it forward would show the wrong place name for wherever the
// operator just put the anchor. The corrected behaviour clears PlaceName on
// any position change regardless of what raced in during the gap, and starts
// its own fresh resolve for the new point instead (see
// TestPatchAnchorWatch_PositionClearsPlaceNameAndResolvesFreshForTheNewPoint).
func TestPatchAnchorWatch_PositionMoveDiscardsAPlaceNamePinnedForTheOldPointDuringPublish(t *testing.T) {
	stub := anchorPublishEnv(t)
	if code, resp := postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0}); code != http.StatusOK {
		t.Fatalf("drop: %d %v", code, resp)
	}

	anchorWatchMu.RLock()
	original := anchorWatchState
	anchorWatchMu.RUnlock()

	// Delay only the confirmation GET (not the delta write itself — ingest
	// stays true, so the new position is genuinely visible in the stub's
	// model straight away): this opens a deterministic window during which
	// patchAnchorWatch is unlocked and blocked inside the publish call,
	// without relying on a fragile sleep-timed guess against real network
	// I/O.
	stub.mu.Lock()
	stub.modelDelay = 300 * time.Millisecond
	stub.mu.Unlock()
	t.Cleanup(func() {
		stub.mu.Lock()
		stub.modelDelay = 0
		stub.mu.Unlock()
	})

	racerDone := make(chan struct{})
	go func() {
		defer close(racerDone)
		time.Sleep(30 * time.Millisecond)
		// Simulates resolveAndPinAnchorWatchPlaceName's own write
		// (place_name.go): takes anchorWatchMu directly, never
		// anchorLifecycleMu, exactly like the real background resolver.
		anchorWatchMu.Lock()
		if anchorWatchState == original {
			pinned := *anchorWatchState
			pinned.PlaceName = "Racername"
			anchorWatchState = &pinned
			anchorWatchMu.Unlock()
			_ = saveAnchorWatch(&pinned)
		} else {
			anchorWatchMu.Unlock()
		}
	}()

	code, resp := patchAnchorWatchForTest(t, map[string]any{"lat": -20.5, "lon": 149.5})
	<-racerDone
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", code, resp)
	}
	if got, _ := resp["place_name"].(string); got != "" {
		t.Fatalf("expected the OLD point's name (even one pinned during the publish gap) to be discarded on a move, got %q", got)
	}

	anchorWatchMu.RLock()
	live := anchorWatchState.PlaceName
	anchorWatchMu.RUnlock()
	if live != "" {
		t.Fatalf("expected the in-memory place name to be cleared, got %q", live)
	}

	raw, err := os.ReadFile(anchorWatchFilePath())
	if err != nil {
		t.Fatalf("read anchor_watch.json: %v", err)
	}
	var onDisk anchorWatchData
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("parse anchor_watch.json: %v", err)
	}
	if onDisk.PlaceName != "" {
		t.Fatalf("expected the persisted place name to be cleared, got %q", onDisk.PlaceName)
	}
}

// Test (code-review finding): a position-changing PATCH is a hand-placed
// position (Adjust mode's crosshair/pan), not the live-GPS bow-corrected
// drop - it must not carry BowOffsetM/BowOffsetApplied/BowOffsetReason/
// HeadingAtSetDeg forward from the old point the way it used to, unlike
// setAnchorWatch's own reposition path (POST with an active watch), which
// already clears these when no offset is applied.
func TestPatchAnchorWatch_PositionClearsBowOffsetAndHeadingFields(t *testing.T) {
	anchorTestEnv(t, 8)
	seedHeadingTrue(t, 0)
	resetAnchorWatchState(t)

	// Drop with a bow offset actually applied, so there is something for the
	// later hand-placed reposition to leave stale if it isn't cleared.
	dropCode, dropResp := postAnchorWatch(t, map[string]any{
		"lat": -21.1113, "lon": 149.2276, "apply_bow_offset": true,
	})
	if dropCode != http.StatusOK {
		t.Fatalf("drop: %d %v", dropCode, dropResp)
	}
	if applied, _ := dropResp["bow_offset_applied"].(bool); !applied {
		t.Fatalf("expected the drop itself to apply a bow offset (test setup), got %+v", dropResp)
	}

	code, resp := patchAnchorWatchForTest(t, map[string]any{"lat": -21.12, "lon": 149.23})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", code, resp)
	}
	if applied, _ := resp["bow_offset_applied"].(bool); applied {
		t.Fatalf("expected bow_offset_applied false after a hand-placed reposition, got %+v", resp)
	}
	if got, _ := resp["bow_offset_m"].(float64); got != 0 {
		t.Fatalf("expected bow_offset_m 0 after a hand-placed reposition, got %v", resp["bow_offset_m"])
	}
	if got, _ := resp["heading_at_set_deg"].(float64); got != -1 {
		t.Fatalf("expected heading_at_set_deg -1 after a hand-placed reposition, got %v", resp["heading_at_set_deg"])
	}
	if reason, _ := resp["bow_offset_reason"].(string); reason != "placed by hand in Adjust" {
		t.Fatalf("expected bow_offset_reason %q, got %q", "placed by hand in Adjust", reason)
	}
}

// Test (code-review finding): the opposite of the above - a radius-only
// PATCH (no position change) must keep carrying the bow-offset/heading
// fields forward exactly like it always has. Only a position change resets
// them.
func TestPatchAnchorWatch_RadiusOnlyKeepsBowOffsetAndHeadingFields(t *testing.T) {
	anchorTestEnv(t, 8)
	seedHeadingTrue(t, 0)
	resetAnchorWatchState(t)

	dropCode, dropResp := postAnchorWatch(t, map[string]any{
		"lat": -21.1113, "lon": 149.2276, "apply_bow_offset": true,
	})
	if dropCode != http.StatusOK {
		t.Fatalf("drop: %d %v", dropCode, dropResp)
	}
	wantBowOffsetM, _ := dropResp["bow_offset_m"].(float64)
	wantHeading, _ := dropResp["heading_at_set_deg"].(float64)

	code, resp := patchAnchorWatchForTest(t, map[string]any{"radius_meters": 40.0})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", code, resp)
	}
	if applied, _ := resp["bow_offset_applied"].(bool); !applied {
		t.Fatalf("expected bow_offset_applied to survive a radius-only PATCH, got %+v", resp)
	}
	if got, _ := resp["bow_offset_m"].(float64); got != wantBowOffsetM {
		t.Fatalf("expected bow_offset_m %v to survive a radius-only PATCH, got %v", wantBowOffsetM, got)
	}
	if got, _ := resp["heading_at_set_deg"].(float64); got != wantHeading {
		t.Fatalf("expected heading_at_set_deg %v to survive a radius-only PATCH, got %v", wantHeading, got)
	}
}

// Test (code-review finding): a position-changing PATCH must not carry the
// OLD point's place name forward - the name is for wherever the anchor WAS,
// not wherever the operator just put it. The immediate response reports an
// empty place_name (a fresh resolve for the new point runs in the
// background, exactly like setAnchorWatch's own drop/reposition path -
// TestSetAnchorWatch_ResolvesAndPinsPlaceNameAsync), and that resolve does
// land on the new point once it completes.
func TestPatchAnchorWatch_PositionClearsPlaceNameAndResolvesFreshForTheNewPoint(t *testing.T) {
	anchorPublishEnv(t)
	resetPlaceNameCache(t)
	resetPlaceNameTickState(t)

	provider := &fakePlaceNameProvider{id: "fake-place-names", results: map[int]placeNameResult{
		400: {Name: "Old Place"},
	}}
	withFakePlaceNameProviderResolver(t, provider)

	if code, resp := postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0}); code != http.StatusOK {
		t.Fatalf("drop: %d %v", code, resp)
	}
	waitForCondition(t, 2*time.Second, func() bool {
		anchorWatchMu.RLock()
		defer anchorWatchMu.RUnlock()
		return anchorWatchState != nil && anchorWatchState.PlaceName == "Old Place"
	})

	// Reconfigure the provider so a place name landing after the PATCH can
	// only be the fresh resolve this fix is supposed to start, not a stale
	// copy of the drop's own name.
	provider.mu.Lock()
	provider.results = map[int]placeNameResult{400: {Name: "New Place"}}
	provider.mu.Unlock()

	code, resp := patchAnchorWatchForTest(t, map[string]any{"lat": -20.5, "lon": 149.5})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", code, resp)
	}
	if got, _ := resp["place_name"].(string); got != "" {
		t.Fatalf("expected an empty place_name in the immediate response (the old point's name must not carry forward), got %q", got)
	}

	waitForCondition(t, 2*time.Second, func() bool {
		anchorWatchMu.RLock()
		defer anchorWatchMu.RUnlock()
		return anchorWatchState != nil && anchorWatchState.PlaceName == "New Place"
	})
}

// Test (code-review finding, round 2): Undo re-sends the pre-Adjust position
// as another position-changing PATCH, which the "placed by hand in Adjust"
// defaults above would otherwise stamp onto the restored point too - wrongly
// marking a genuine bow-corrected drop as unapplied and losing its resolved
// place name, even though Undo is putting the anchor back exactly where it
// was, not making a new hand-placed move. `restore` lets the caller (only
// ever Undo) hand back the exact per-point facts the PATCH it's undoing had
// replaced, applied instead of the hand-placed defaults.
func TestPatchAnchorWatch_RestoreAppliesThePreAdjustPerPointFacts(t *testing.T) {
	anchorPublishEnv(t)
	if code, resp := postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0}); code != http.StatusOK {
		t.Fatalf("drop: %d %v", code, resp)
	}

	provider := &fakePlaceNameProvider{id: "fake-place-names", results: map[int]placeNameResult{
		400: {Name: "Should Not Be Called"},
	}}
	withFakePlaceNameProviderResolver(t, provider)

	code, resp := patchAnchorWatchForTest(t, map[string]any{
		"lat": -20.5, "lon": 149.5,
		"restore": map[string]any{
			"bow_offset_m":       8.0,
			"bow_offset_applied": true,
			"bow_offset_reason":  "",
			"heading_at_set_deg": 45.0,
			"place_name":         "Goldsmith Island",
		},
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", code, resp)
	}
	if applied, _ := resp["bow_offset_applied"].(bool); !applied {
		t.Fatalf("expected bow_offset_applied restored to true, got %+v", resp)
	}
	if got, _ := resp["bow_offset_m"].(float64); got != 8.0 {
		t.Fatalf("expected bow_offset_m restored to 8, got %v", resp["bow_offset_m"])
	}
	if got, _ := resp["heading_at_set_deg"].(float64); got != 45.0 {
		t.Fatalf("expected heading_at_set_deg restored to 45, got %v", resp["heading_at_set_deg"])
	}
	if got, _ := resp["place_name"].(string); got != "Goldsmith Island" {
		t.Fatalf("expected place_name restored to %q, got %q", "Goldsmith Island", got)
	}

	// A restored, non-empty place name is already known - starting a fresh
	// resolve over it (which would eventually overwrite it with whatever the
	// provider returns) is exactly the bug this feature exists to avoid.
	time.Sleep(100 * time.Millisecond)
	if calls := provider.callCount(); calls != 0 {
		t.Fatalf("expected no place-name resolve to start when restore.place_name is non-empty, got %d provider calls", calls)
	}
}

// Test: restore with an EMPTY place_name (the pre-Adjust point had none
// resolved yet) still starts a fresh resolve for the point Undo just
// restored - the same as an ordinary hand-placed move, since there is
// genuinely nothing to restore.
func TestPatchAnchorWatch_RestoreWithEmptyPlaceNameStillResolvesFresh(t *testing.T) {
	anchorPublishEnv(t)
	resetPlaceNameCache(t)
	resetPlaceNameTickState(t)
	if code, resp := postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0}); code != http.StatusOK {
		t.Fatalf("drop: %d %v", code, resp)
	}

	provider := &fakePlaceNameProvider{id: "fake-place-names", results: map[int]placeNameResult{
		400: {Name: "Resolved After Undo"},
	}}
	withFakePlaceNameProviderResolver(t, provider)

	code, resp := patchAnchorWatchForTest(t, map[string]any{
		"lat": -20.5, "lon": 149.5,
		"restore": map[string]any{
			"bow_offset_m":       0.0,
			"bow_offset_applied": false,
			"bow_offset_reason":  "heading unavailable",
			"heading_at_set_deg": -1.0,
			"place_name":         "",
		},
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %+v", code, resp)
	}
	if reason, _ := resp["bow_offset_reason"].(string); reason != "heading unavailable" {
		t.Fatalf("expected bow_offset_reason restored to %q, got %q", "heading unavailable", reason)
	}

	waitForCondition(t, 2*time.Second, func() bool {
		anchorWatchMu.RLock()
		defer anchorWatchMu.RUnlock()
		return anchorWatchState != nil && anchorWatchState.PlaceName == "Resolved After Undo"
	})
}

// Test: restore is rejected outright without a position change - it exists
// only to carry the facts a position-changing PATCH would otherwise
// overwrite, and a radius-only PATCH never touches any of them.
func TestPatchAnchorWatch_RestoreRejectedWithoutPositionChange(t *testing.T) {
	anchorPublishEnv(t)
	if code, resp := postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0}); code != http.StatusOK {
		t.Fatalf("drop: %d %v", code, resp)
	}

	code, resp := patchAnchorWatchForTest(t, map[string]any{
		"radius_meters": 40.0,
		"restore": map[string]any{
			"bow_offset_m":       0.0,
			"bow_offset_applied": false,
			"bow_offset_reason":  "",
			"heading_at_set_deg": -1.0,
			"place_name":         "",
		},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400 for restore without a position change, got %d: %+v", code, resp)
	}
}

// Test: restore's own fields are validated - an out-of-range heading is
// rejected rather than silently stored and shown as a real corrected
// heading.
func TestPatchAnchorWatch_RestoreRejectsInvalidHeading(t *testing.T) {
	anchorPublishEnv(t)
	if code, resp := postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0}); code != http.StatusOK {
		t.Fatalf("drop: %d %v", code, resp)
	}

	code, resp := patchAnchorWatchForTest(t, map[string]any{
		"lat": -20.5, "lon": 149.5,
		"restore": map[string]any{
			"bow_offset_m":       0.0,
			"bow_offset_applied": false,
			"bow_offset_reason":  "",
			"heading_at_set_deg": 400.0,
			"place_name":         "",
		},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an out-of-range heading_at_set_deg, got %d: %+v", code, resp)
	}
}

// Test: a negative bow_offset_m (not a real distance) is rejected too.
func TestPatchAnchorWatch_RestoreRejectsNegativeBowOffset(t *testing.T) {
	anchorPublishEnv(t)
	if code, resp := postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0}); code != http.StatusOK {
		t.Fatalf("drop: %d %v", code, resp)
	}

	code, resp := patchAnchorWatchForTest(t, map[string]any{
		"lat": -20.5, "lon": 149.5,
		"restore": map[string]any{
			"bow_offset_m":       -1.0,
			"bow_offset_applied": false,
			"bow_offset_reason":  "",
			"heading_at_set_deg": -1.0,
			"place_name":         "",
		},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a negative bow_offset_m, got %d: %+v", code, resp)
	}
}

// Test: restore's string fields are length-capped - an implausibly long
// value (never something a real bow_offset_reason/place_name is) is
// rejected rather than stored verbatim.
func TestPatchAnchorWatch_RestoreRejectsOverlongStrings(t *testing.T) {
	anchorPublishEnv(t)
	if code, resp := postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0}); code != http.StatusOK {
		t.Fatalf("drop: %d %v", code, resp)
	}

	tooLong := strings.Repeat("x", 1000)
	code, resp := patchAnchorWatchForTest(t, map[string]any{
		"lat": -20.5, "lon": 149.5,
		"restore": map[string]any{
			"bow_offset_m":       0.0,
			"bow_offset_applied": false,
			"bow_offset_reason":  tooLong,
			"heading_at_set_deg": -1.0,
			"place_name":         "",
		},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an overlong bow_offset_reason, got %d: %+v", code, resp)
	}
}
