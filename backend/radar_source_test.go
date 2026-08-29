package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// withGlobalRadarTargetStore swaps the package-level radar target store for
// the duration of a test, mirroring withGlobalSnapshot (collision_ais_test.go).
func withGlobalRadarTargetStore(t *testing.T, store *radarTargetStore) {
	t.Helper()
	original := globalRadarTargetStore
	globalRadarTargetStore = store
	t.Cleanup(func() { globalRadarTargetStore = original })
}

// radarSnapshotWithRadars builds a signalKSnapshot whose self tree carries
// the "radars" branch the mayara SignalK plugin actually publishes as
// deltas — vessels.self.radars.<id>.controls.*, and nothing else (verified
// against the live plugin 2026-08-29). Built through applyDelta rather than
// a hand-nested map literal so the test exercises the same tree-reassembly
// path production deltas go through, mirroring selfSnapshotWithDepth
// (signalk_payload_test.go).
func radarSnapshotWithRadars(t *testing.T, radars map[string]struct{ modelName, userName string }) *signalKSnapshot {
	t.Helper()

	snapshot := newSignalKSnapshot()
	for id, names := range radars {
		snapshot.applyDelta(signalKDelta{
			Context: "vessels.self",
			Updates: []signalKUpdate{{Values: []signalKValue{
				// The plugin wraps each control value in its own object; see
				// TestRadarsFromSnapshotReadsTheCapturedControlDeltaShape and
				// testdata/mayara/radar-controls-delta.json. Seeding a bare
				// scalar here is what let the nesting bug reach production.
				{Path: "radars." + id + ".controls.modelName", Value: map[string]any{"value": names.modelName, "timestamp": "2026-08-28T00:22:23.086858100Z"}},
				{Path: "radars." + id + ".controls.userName", Value: map[string]any{"value": names.userName, "timestamp": "2026-08-28T00:22:22.878896300Z"}},
				{Path: "radars." + id + ".controls.power", Value: map[string]any{"value": float64(mayaraTransmitPowerValue), "timestamp": "2026-08-28T22:08:52.254697200Z"}},
			}}},
		}, time.Now().UTC())
	}
	snapshot.setSelfContext("vessels.self")
	return snapshot
}

func TestRadarsFromSnapshotReturnsBothDualRangeRadars(t *testing.T) {
	snapshot := radarSnapshotWithRadars(t, map[string]struct{ modelName, userName string }{
		"fur6424A": {modelName: "DRS4DNXT", userName: "DRS4D-NXT 6424"},
		"fur6424B": {modelName: "DRS4DNXT", userName: "DRS4D-NXT 6424 B"},
	})

	radars := radarsFromSnapshot(snapshot)
	if len(radars) != 2 {
		t.Fatalf("expected 2 radars, got %d: %+v", len(radars), radars)
	}

	byID := make(map[string]radarInfo, len(radars))
	for _, radar := range radars {
		byID[radar.ID] = radar
	}

	a, ok := byID["fur6424A"]
	if !ok {
		t.Fatalf("fur6424A missing from radarsFromSnapshot: %+v", radars)
	}
	if a.Name != "DRS4D-NXT 6424" {
		t.Errorf("fur6424A.Name = %q, want %q", a.Name, "DRS4D-NXT 6424")
	}

	b, ok := byID["fur6424B"]
	if !ok {
		t.Fatalf("fur6424B missing from radarsFromSnapshot: %+v", radars)
	}
	if b.Name != "DRS4D-NXT 6424 B" {
		t.Errorf("fur6424B.Name = %q, want %q", b.Name, "DRS4D-NXT 6424 B")
	}
}

// No "radars" key in the self tree is the availability signal: the mayara
// SignalK plugin is not installed, or not yet publishing anything.
func TestRadarsFromSnapshotNilWhenRadarsAbsent(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{Values: []signalKValue{{Path: "navigation.headingTrue", Value: 1.2}}}},
	}, time.Now().UTC())
	snapshot.setSelfContext("vessels.self")

	if got := radarsFromSnapshot(snapshot); got != nil {
		t.Fatalf("expected nil when radars is absent, got %+v", got)
	}
}

// A vessel context that has never received any delta at all — the state
// right after startup, before the SignalK stream connects — must also read
// as no radars, not panic on a nil tree.
func TestRadarsFromSnapshotNilWhenSelfTreeUnknown(t *testing.T) {
	snapshot := newSignalKSnapshot()
	if got := radarsFromSnapshot(snapshot); got != nil {
		t.Fatalf("expected nil when the self context has never been seen, got %+v", got)
	}
}

// radarTargetsFixture decodes the captured REST response verbatim, per
// AGENTS.md's fixture rule: this is mayara's real wire shape, not
// hand-authored JSON, and is the regression guard for the is_dangerous
// snake_case tag (backend/testdata/mayara/README.md).
func radarTargetsFixture(t *testing.T, path string) []mayaraArpaTarget {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var targets []mayaraArpaTarget
	if err := json.Unmarshal(raw, &targets); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return targets
}

// The default fixture radar is transmitting, which is the ordinary case these
// tests describe. A zero-value radarInfo would read as standby and the
// staleness gate would correctly drop everything, which is a different test.
func testRadarPoller(store *radarTargetStore) *radarPoller {
	return testRadarPollerWith(store,
		[]radarInfo{{ID: "fur6424A", Name: "DRS4D-NXT 6424", Transmitting: true}},
		nil,
	)
}

func testRadarPollerWith(store *radarTargetStore, radars []radarInfo, fetch func(string) ([]mayaraArpaTarget, error)) *radarPoller {
	return &radarPoller{
		store:        store,
		now:          func() time.Time { return time.Now().UTC() },
		radars:       func() []radarInfo { return radars },
		fetchTargets: fetch,
		ownShipFix:   func() (float64, float64, bool) { return 0, 0, false },
	}
}

// The captured fixture (21 live targets, 9 with is_dangerous:true) driven
// through the real poll-and-convert path. This is the regression guard for
// the danger block's snake_case tag surviving from the wire, through
// pollOnce, into the store.
func TestRadarPollOnceLoadsCapturedFixtureAndPreservesIsDangerous(t *testing.T) {
	fixture := radarTargetsFixture(t, "testdata/mayara/targets-fur6424A.json")

	store := newRadarTargetStore()
	poller := testRadarPoller(store)
	poller.fetchTargets = func(radarID string) ([]mayaraArpaTarget, error) {
		if radarID != "fur6424A" {
			t.Fatalf("unexpected radarID %q", radarID)
		}
		return fixture, nil
	}

	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	if err := poller.pollOnce(now); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}

	targets := store.list(now)
	if len(targets) != len(fixture) {
		t.Fatalf("expected %d targets from the fixture, got %d", len(fixture), len(targets))
	}

	dangerous := 0
	for _, target := range targets {
		if target.IsDangerous {
			dangerous++
		}
	}
	if dangerous != 9 {
		t.Fatalf("is_dangerous survived for %d targets, want 9 (backend/testdata/mayara/README.md; the snake_case regression guard)", dangerous)
	}
}

// A poll response is the tracker's full current state: a target present in
// one poll and absent from the next must be gone, with no ageing involved.
func TestRadarPollOnceTargetAbsentFromNextPollIsGone(t *testing.T) {
	store := newRadarTargetStore()
	poller := testRadarPoller(store)

	calls := 0
	poller.fetchTargets = func(radarID string) ([]mayaraArpaTarget, error) {
		calls++
		if calls == 1 {
			return []mayaraArpaTarget{
				newTestArpaTarget(1, "tracking", 500),
				newTestArpaTarget(2, "tracking", 800),
			}, nil
		}
		return []mayaraArpaTarget{newTestArpaTarget(1, "tracking", 510)}, nil
	}

	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	if err := poller.pollOnce(now); err != nil {
		t.Fatalf("first pollOnce: %v", err)
	}
	if got := len(store.list(now)); got != 2 {
		t.Fatalf("after the first poll: got %d targets, want 2", got)
	}

	next := now.Add(2 * time.Second)
	if err := poller.pollOnce(next); err != nil {
		t.Fatalf("second pollOnce: %v", err)
	}
	list := store.list(next)
	if len(list) != 1 || list[0].TargetID != 1 {
		t.Fatalf("target 2, absent from the second poll, must be gone, got %+v", list)
	}
}

// A blip must not blank the map: a failing poll marks the store
// disconnected but leaves whatever it already held in place.
func TestRadarPollOnceFailingPollDoesNotPurgeAndMarksUnreachable(t *testing.T) {
	store := newRadarTargetStore()
	poller := testRadarPoller(store)

	poller.fetchTargets = func(radarID string) ([]mayaraArpaTarget, error) {
		return []mayaraArpaTarget{newTestArpaTarget(1, "tracking", 500)}, nil
	}

	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	if err := poller.pollOnce(now); err != nil {
		t.Fatalf("first pollOnce: %v", err)
	}
	if connected, _ := store.status(); !connected {
		t.Fatalf("a successful poll should report connected")
	}

	poller.fetchTargets = func(radarID string) ([]mayaraArpaTarget, error) {
		return nil, fmt.Errorf("simulated mayara unreachable")
	}

	next := now.Add(2 * time.Second)
	if err := poller.pollOnce(next); err == nil {
		t.Fatalf("expected pollOnce to report the fetch failure")
	}

	if connected, _ := store.status(); connected {
		t.Fatalf("a failed poll should report disconnected")
	}
	if got := len(store.list(next)); got != 1 {
		t.Fatalf("a failed poll must not purge existing targets, got %d entries", got)
	}

	// The same failure must surface through the payload as
	// "mayara-unreachable", not "disabled" — radars are present, only the
	// poll itself is failing.
	snapshot := radarSnapshotWithRadars(t, map[string]struct{ modelName, userName string }{
		"fur6424A": {modelName: "DRS4DNXT", userName: "DRS4D-NXT 6424"},
	})
	withGlobalSnapshot(t, snapshot)
	withGlobalRadarTargetStore(t, store)

	payload := buildRadarTargetsPayload()
	if payload["source"] != "mayara-unreachable" {
		t.Fatalf("source = %v, want %q", payload["source"], "mayara-unreachable")
	}
	targets, ok := payload["targets"].([]radarTarget)
	if !ok || len(targets) != 1 {
		t.Fatalf("targets = %#v, want the one surviving target, not purged", payload["targets"])
	}
}

// Dual range must not double-poll: only the first radar radarsFromSnapshot
// reports is ever fetched, even with two present.
func TestRadarPollOnceOnlyPollsTheFirstRadar(t *testing.T) {
	store := newRadarTargetStore()
	poller := testRadarPoller(store)
	poller.radars = func() []radarInfo {
		return []radarInfo{{ID: "fur6424A"}, {ID: "fur6424B"}}
	}

	calls := 0
	poller.fetchTargets = func(radarID string) ([]mayaraArpaTarget, error) {
		calls++
		if radarID != "fur6424A" {
			t.Fatalf("expected only fur6424A to be polled, got %q", radarID)
		}
		return nil, nil
	}

	if err := poller.pollOnce(time.Now().UTC()); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}
	if calls != 1 {
		t.Fatalf("fetchTargets called %d times, want exactly 1 (dual range must not double-poll)", calls)
	}
}

func TestBuildRadarTargetsPayloadSourceDisabledWhenNoRadarsInSnapshot(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.setSelfContext("vessels.self")
	withGlobalSnapshot(t, snapshot)

	store := newRadarTargetStore()
	store.setConnected(true) // even "connected" must not leak through while disabled
	store.replace("fur6424A", []radarTarget{newTestRadarTarget("fur6424A", 1, "tracking", 500, time.Now().UTC())}, time.Now().UTC())
	withGlobalRadarTargetStore(t, store)

	payload := buildRadarTargetsPayload()
	if payload["source"] != "disabled" {
		t.Fatalf("source = %v, want %q", payload["source"], "disabled")
	}
	targets, ok := payload["targets"].([]radarTarget)
	if !ok || len(targets) != 0 {
		t.Fatalf("targets = %#v, want an empty slice when disabled", payload["targets"])
	}
	radars, ok := payload["radars"].([]radarInfo)
	if !ok || len(radars) != 0 {
		t.Fatalf("radars = %#v, want an empty slice when disabled", payload["radars"])
	}
}

func TestBuildRadarTargetsPayloadSourceMayaraWhenPollSucceeded(t *testing.T) {
	snapshot := radarSnapshotWithRadars(t, map[string]struct{ modelName, userName string }{
		"fur6424A": {modelName: "DRS4DNXT", userName: "DRS4D-NXT 6424"},
	})
	withGlobalSnapshot(t, snapshot)

	store := newRadarTargetStore()
	now := time.Now().UTC()
	store.replace("fur6424A", []radarTarget{newTestRadarTarget("fur6424A", 3, "tracking", 1200, now)}, now)
	store.setConnected(true)
	withGlobalRadarTargetStore(t, store)

	payload := buildRadarTargetsPayload()
	if payload["source"] != "mayara" {
		t.Fatalf("source = %v, want %q", payload["source"], "mayara")
	}
	targets, ok := payload["targets"].([]radarTarget)
	if !ok || len(targets) != 1 || targets[0].ID != "fur6424A:3" {
		t.Fatalf("targets = %#v, want [fur6424A:3]", payload["targets"])
	}
}

// TestBuildRadarTargetsPayloadCapsTargetsAtTwenty guards the
// presentation-only cap: 25 targets go into the store, at most 20 come out,
// and the ones kept are the nearest by range since store.list already sorts
// ascending on RangeM.
func TestBuildRadarTargetsPayloadCapsTargetsAtTwenty(t *testing.T) {
	snapshot := radarSnapshotWithRadars(t, map[string]struct{ modelName, userName string }{
		"fur6424A": {modelName: "DRS4DNXT", userName: "DRS4D-NXT 6424"},
	})
	withGlobalSnapshot(t, snapshot)

	store := newRadarTargetStore()
	now := time.Now().UTC()
	targets := make([]radarTarget, 0, 25)
	for i := uint64(1); i <= 25; i++ {
		targets = append(targets, newTestRadarTarget("fur6424A", i, "tracking", float64(i)*100, now))
	}
	store.replace("fur6424A", targets, now)
	store.setConnected(true)
	withGlobalRadarTargetStore(t, store)

	payload := buildRadarTargetsPayload()
	got, ok := payload["targets"].([]radarTarget)
	if !ok {
		t.Fatalf("targets has unexpected type: %#v", payload["targets"])
	}
	if len(got) != 20 {
		t.Fatalf("len(targets) = %d, want 20", len(got))
	}
	if got[0].RangeM != 100 {
		t.Fatalf("nearest target range = %v, want 100 (the cap must keep the nearest, not an arbitrary 20)", got[0].RangeM)
	}
	for _, target := range got {
		if target.RangeM > 2000 {
			t.Fatalf("target at range %v survived the cap; the farthest 5 of 25 (2100-2500) must be dropped", target.RangeM)
		}
	}
}

func TestRadarTargetsHandlerReturnsPayload(t *testing.T) {
	snapshot := newSignalKSnapshot()
	snapshot.setSelfContext("vessels.self")
	withGlobalSnapshot(t, snapshot)
	withGlobalRadarTargetStore(t, newRadarTargetStore())

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/radar/targets", nil)
	rec := httptest.NewRecorder()

	if err := radarTargetsHandler(e.NewContext(req, rec)); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var decoded map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	for _, key := range []string{"datetime", "source", "radars", "targets"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("response missing %q: %s", key, rec.Body.String())
		}
	}
	if decoded["source"] != "disabled" {
		t.Fatalf("source = %v, want %q", decoded["source"], "disabled")
	}
}

// The no-fix sentinel must not be mistaken for a position.
//
// vesselStateData is initialised with Latitude: -1, Longitude: -1 as its
// "nothing known yet" value (signalk.go, fetchSignalKVesselState). A plain
// range check passes that straight through, because -1,-1 is inside
// [-90,90] and [-180,180]: it is a real point in the Gulf of Guinea.
//
// For the AIS path the damage is bounded, since every genuine contact then
// falls outside the 5 km range gate and the list comes back empty. Radar
// projects target positions FROM own ship, so a sentinel treated as a fix
// puts every derived contact off West Africa and plots it with confidence.
// Reporting no position is correct; inventing one is the masking failure
// AGENTS.md forbids.
func TestOwnShipFixRejectsTheNoFixSentinel(t *testing.T) {
	for _, tc := range []struct {
		name    string
		state   vesselStateData
		wantOK  bool
		wantLat float64
		wantLon float64
	}{
		{
			name:   "sentinel is not a fix",
			state:  vesselStateData{Latitude: -1, Longitude: -1},
			wantOK: false,
		},
		{
			name:   "latitude sentinel alone is not a fix",
			state:  vesselStateData{Latitude: -1, Longitude: 148.95},
			wantOK: false,
		},
		{
			name:   "out of range is not a fix",
			state:  vesselStateData{Latitude: 91, Longitude: 148.95},
			wantOK: false,
		},
		{
			name:    "a real fix is a fix",
			state:   vesselStateData{Latitude: -20.3457, Longitude: 148.9490},
			wantOK:  true,
			wantLat: -20.3457,
			wantLon: 148.9490,
		},
		{
			name:    "null island is a fix, improbable but valid",
			state:   vesselStateData{Latitude: 0, Longitude: 0},
			wantOK:  true,
			wantLat: 0,
			wantLon: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lat, lon, ok := ownShipFixFromVesselState(tc.state)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (state %+v)", ok, tc.wantOK, tc.state)
			}
			if ok && (lat != tc.wantLat || lon != tc.wantLon) {
				t.Fatalf("fix = %v,%v want %v,%v", lat, lon, tc.wantLat, tc.wantLon)
			}
		})
	}
}

// A radar in standby keeps serving targets, and they are not observations.
//
// Measured 2026-08-28 21:50Z: the radar had been in standby (power 0) for
// about half an hour and mayara was still serving 50 targets, statuses
// "tracking" and "lost", five still flagged is_dangerous, positions drifting
// under Kalman extrapolation. Captured as testdata/mayara/
// targets-standby-stale.json with controls-standby.json beside it.
//
// Ageing against our own receive time cannot see this: the poll succeeds
// every two seconds and returns the same extrapolated set, so the target
// never looks stale from where we stand. That is ADR 0057 section 6 a third
// time, after the prioritizer writing once on state change and mayara's
// sixty-second bursts.
func TestPollDropsEverythingWhileTheRadarIsInStandby(t *testing.T) {
	arpa := capturedStandbyTargets(t)
	if len(arpa) == 0 {
		t.Fatal("fixture carries no targets; it is meant to hold the stale standby set")
	}

	store := newRadarTargetStore()
	poller := testRadarPollerWith(store,
		[]radarInfo{{ID: "fur6424A", Name: "DRS4D-NXT 6424", Transmitting: false}},
		func(string) ([]mayaraArpaTarget, error) { return arpa, nil },
	)

	now := time.Date(2026, 8, 28, 21, 50, 47, 0, time.UTC)
	if err := poller.pollOnce(now); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}

	if got := store.list(now); len(got) != 0 {
		t.Fatalf("radar in standby served %d targets; extrapolated contacts must not reach the map", len(got))
	}
}

// While transmitting, an individual contact that stopped being observed must
// still go. mayara's own clock is the reference on both sides of the
// comparison, so its skew against ours does not matter: ADR 0062 decision 5
// forbids ageing against lastSeen in absolute terms, and this does not.
func TestPollDropsTargetsTrailingTheRadarClock(t *testing.T) {
	radarNow := time.Date(2026, 8, 28, 21, 50, 0, 0, time.UTC)

	fresh := newTestArpaTarget(1, "tracking", 400)
	fresh.LastSeen = radarNow.Add(-2 * time.Second).Format(time.RFC3339Nano)
	stale := newTestArpaTarget(2, "tracking", 600)
	stale.LastSeen = radarNow.Add(-30 * time.Minute).Format(time.RFC3339Nano)

	store := newRadarTargetStore()
	poller := testRadarPollerWith(store,
		[]radarInfo{{ID: "fur6424A", Name: "R", Transmitting: true, clockNow: radarNow}},
		func(string) ([]mayaraArpaTarget, error) {
			return []mayaraArpaTarget{fresh, stale}, nil
		},
	)

	// Our own clock is deliberately offset from the radar's to prove the
	// comparison never touches it.
	if err := poller.pollOnce(radarNow.Add(9 * time.Hour)); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}

	got := store.list(radarNow.Add(9 * time.Hour))
	if len(got) != 1 {
		t.Fatalf("want only the fresh target, got %d", len(got))
	}
	if got[0].TargetID != 1 {
		t.Fatalf("kept target %d, want the freshly observed one", got[0].TargetID)
	}
}

func capturedStandbyTargets(t *testing.T) []mayaraArpaTarget {
	t.Helper()
	raw, err := os.ReadFile("testdata/mayara/targets-standby-stale.json")
	if err != nil {
		t.Fatalf("read standby fixture: %v", err)
	}
	var targets []mayaraArpaTarget
	if err := json.Unmarshal(raw, &targets); err != nil {
		t.Fatalf("decode standby fixture: %v", err)
	}
	return targets
}

// The projection cross-check's dedup map is keyed per target and, under
// polling, has no reconnect to reset it. mayara's target ids climb (100000003
// to 100000338 across one session's captures), so without a reset point the
// map grows for as long as the process runs.
//
// A poll that observes nothing is the natural boundary: the radar has gone to
// standby or stopped painting, so whatever it acquires next is a fresh
// population, and the rate limit should start over for it.
func TestPollWithNoObservedTargetsResetsProjectionMismatchDedup(t *testing.T) {
	radarProjectionMismatchTracker.mu.Lock()
	radarProjectionMismatchTracker.logged["fur6424A:1"] = true
	radarProjectionMismatchTracker.mu.Unlock()

	store := newRadarTargetStore()
	poller := testRadarPollerWith(store,
		[]radarInfo{{ID: "fur6424A", Name: "R", Transmitting: false}},
		func(string) ([]mayaraArpaTarget, error) {
			return []mayaraArpaTarget{newTestArpaTarget(1, "tracking", 400)}, nil
		},
	)

	if err := poller.pollOnce(time.Now().UTC()); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}

	radarProjectionMismatchTracker.mu.Lock()
	defer radarProjectionMismatchTracker.mu.Unlock()
	if len(radarProjectionMismatchTracker.logged) != 0 {
		t.Fatalf("dedup map still holds %d entries after an empty poll", len(radarProjectionMismatchTracker.logged))
	}
}

// The real delta shape, which the hand-built fixture above does not have.
//
// The plugin wraps each control in an object: the delta value for
// radars.<id>.controls.userName is {"value":"DRS4D-NXT 6424","timestamp":...},
// not the bare string. applyDelta then stores that object under its own
// "value" key, so the reachable path is controls.userName.value.value, one
// level deeper than mayara's REST shape, which is flat.
//
// Seeding a test with a plain string hides that entirely, and it did: the
// unit tests passed while the live payload came back with empty names and
// transmitting false on a transmitting radar. Captured 2026-08-29 into
// testdata/mayara/radar-controls-delta.json.
func TestRadarsFromSnapshotReadsTheCapturedControlDeltaShape(t *testing.T) {
	raw, err := os.ReadFile("testdata/mayara/radar-controls-delta.json")
	if err != nil {
		t.Fatalf("read control delta fixture: %v", err)
	}
	var delta signalKDelta
	if err := json.Unmarshal(raw, &delta); err != nil {
		t.Fatalf("decode control delta fixture: %v", err)
	}

	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(delta, time.Now().UTC())
	snapshot.setSelfContext("vessels.self")

	radars := radarsFromSnapshot(snapshot)
	if len(radars) == 0 {
		t.Fatal("no radars decoded from the captured control delta")
	}

	var a radarInfo
	for _, radar := range radars {
		if radar.ID == "fur6424A" {
			a = radar
		}
	}
	if a.ID == "" {
		t.Fatalf("fur6424A missing: %+v", radars)
	}
	if a.Name != "DRS4D-NXT 6424" {
		t.Errorf("Name = %q, want %q (userName is nested a level deeper than the REST shape)", a.Name, "DRS4D-NXT 6424")
	}
	if !a.Transmitting {
		t.Error("Transmitting is false although the captured power control reads 2 (Transmit)")
	}
	if a.clockNow.IsZero() {
		t.Error("clockNow is zero; the control timestamps carry mayara's own clock")
	}
}
