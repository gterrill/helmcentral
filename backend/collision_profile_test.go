package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The plugin's shipped defaults, verbatim: the anchor profile really is all
// zeros, which is what decision 3 exists to catch.
const collisionProfilesJSON = `{
	"current": "offshore",
	"anchor":   {"warning":{"cpa":0,"tcpa":60,"speed":0},"danger":{"cpa":0,"tcpa":60,"speed":0},"guard":{"range":0,"speed":0}},
	"harbor":   {"warning":{"cpa":0.5,"tcpa":10,"speed":0.5},"danger":{"cpa":0.1,"tcpa":5,"speed":3},"guard":{"range":0,"speed":0}},
	"coastal":  {"warning":{"cpa":2,"tcpa":30,"speed":0},"danger":{"cpa":1,"tcpa":10,"speed":0.5},"guard":{"range":0,"speed":0}},
	"offshore": {"warning":{"cpa":4,"tcpa":30,"speed":0},"danger":{"cpa":2,"tcpa":15,"speed":0},"guard":{"range":0,"speed":0}}
}`

func snapshotInState(state string) *signalKSnapshot {
	snapshot := newSignalKSnapshot()
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{Values: []signalKValue{{Path: "navigation.state", Value: state}}}},
	}, alarmNow)
	snapshot.setSelfContext("vessels.self")
	return snapshot
}

// syncerFor wires a syncer to a parsed profile document and records every save.
func syncerFor(t *testing.T, state, doc string) (*collisionProfileSyncer, *[]string) {
	t.Helper()
	current, profiles, err := parseCollisionProfiles([]byte(doc))
	if err != nil {
		t.Fatalf("parseCollisionProfiles: %v", err)
	}

	saved := []string{}
	syncer := &collisionProfileSyncer{
		snapshot: snapshotInState(state),
		load: func() (string, map[string]collisionProfile, error) {
			return current, profiles, nil
		},
		save: func(profile string) error {
			saved = append(saved, profile)
			current = profile
			return nil
		},
	}
	return syncer, &saved
}

func TestParseCollisionProfilesReadsTheShippedDocument(t *testing.T) {
	current, profiles, err := parseCollisionProfiles([]byte(collisionProfilesJSON))
	if err != nil {
		t.Fatalf("parseCollisionProfiles: %v", err)
	}
	if current != "offshore" {
		t.Fatalf("current: got %q, want offshore", current)
	}
	if len(profiles) != 4 {
		t.Fatalf("expected 4 profiles, got %d (%v)", len(profiles), profiles)
	}
	if profiles["offshore"].Danger.Cpa != 2 || profiles["offshore"].Danger.Speed != 0 {
		t.Fatalf("offshore danger: got %+v", profiles["offshore"].Danger)
	}
}

// A document with no "current" is not a profile document. Guessing one would
// mean writing a profile the operator never chose.
func TestParseCollisionProfilesRejectsADocumentWithNoCurrent(t *testing.T) {
	if _, _, err := parseCollisionProfiles([]byte(`{"anchor":{}}`)); err == nil {
		t.Fatal("a document with no current must be rejected")
	}
}

// The whole point: anchoring selects the anchored profile.
func TestCollisionProfileSyncerFollowsTheVesselIntoHarbor(t *testing.T) {
	syncer, saved := syncerFor(t, "moored", collisionProfilesJSON)

	if events := syncer.check(alarmNow); len(events) != 0 {
		t.Fatalf("a successful switch raises nothing, got %+v", events)
	}
	if len(*saved) != 1 || (*saved)[0] != "harbor" {
		t.Fatalf("saved: got %v, want [harbor]", *saved)
	}
}

// Decision 2: acting on transitions, not on disagreement, is what lets a manual
// override survive. Under way the mapping says coastal, so a syncer that
// reconciled every tick would stomp a deliberate switch to offshore.
func TestCollisionProfileSyncerActsOncePerTransition(t *testing.T) {
	syncer, saved := syncerFor(t, "sailing", collisionProfilesJSON)

	syncer.check(alarmNow)
	if len(*saved) != 1 || (*saved)[0] != "coastal" {
		t.Fatalf("first tick: got %v, want [coastal]", *saved)
	}

	// The operator switches to offshore by hand. State has not changed.
	syncer.check(alarmNow.Add(time.Minute))
	syncer.check(alarmNow.Add(2 * time.Minute))
	if len(*saved) != 1 {
		t.Fatalf("a manual override must survive until the next transition, got %v", *saved)
	}
}

// Decision 3: the shipped anchor profile can never fire. Adopting it would take
// the collision alarm offline exactly when the boat is left unattended.
func TestCollisionProfileSyncerRefusesASilentProfile(t *testing.T) {
	syncer, saved := syncerFor(t, "anchored", collisionProfilesJSON)

	events := syncer.check(alarmNow)
	if len(*saved) != 0 {
		t.Fatalf("a profile that can never fire must not be selected, saved %v", *saved)
	}
	if len(events) != 1 || events[0].Kind != alarmEventRaised {
		t.Fatalf("the refusal must be surfaced, got %+v", events)
	}
	if events[0].Status.State != alarmStateWarn {
		t.Fatalf("refusal state: got %q", events[0].Status.State)
	}
}

// An anchor profile with real thresholds is selected normally.
func TestCollisionProfileSyncerSelectsAConfiguredAnchorProfile(t *testing.T) {
	configured := `{
		"current": "offshore",
		"anchor":   {"warning":{"cpa":0.25,"tcpa":20,"speed":1},"danger":{"cpa":0.1,"tcpa":10,"speed":1},"guard":{"range":0.25,"speed":1}},
		"offshore": {"warning":{"cpa":4,"tcpa":30,"speed":0},"danger":{"cpa":2,"tcpa":15,"speed":0},"guard":{"range":0,"speed":0}}
	}`
	syncer, saved := syncerFor(t, "anchored", configured)

	if events := syncer.check(alarmNow); len(events) != 0 {
		t.Fatalf("a configured anchor profile raises nothing, got %+v", events)
	}
	if len(*saved) != 1 || (*saved)[0] != "anchor" {
		t.Fatalf("saved: got %v, want [anchor]", *saved)
	}
}

// A guard ring alone is a working anchored profile, and is the shape this ADR
// actually recommends. CPA of zero must not condemn it.
func TestCollisionProfileSyncerAcceptsAGuardOnlyProfile(t *testing.T) {
	guardOnly := `{
		"current": "offshore",
		"anchor":   {"warning":{"cpa":0,"tcpa":0,"speed":0},"danger":{"cpa":0,"tcpa":0,"speed":0},"guard":{"range":0.25,"speed":1}},
		"offshore": {"warning":{"cpa":4,"tcpa":30,"speed":0},"danger":{"cpa":2,"tcpa":15,"speed":0},"guard":{"range":0,"speed":0}}
	}`
	syncer, saved := syncerFor(t, "anchored", guardOnly)

	if events := syncer.check(alarmNow); len(events) != 0 {
		t.Fatalf("a guard ring is a working profile, got %+v", events)
	}
	if len(*saved) != 1 || (*saved)[0] != "anchor" {
		t.Fatalf("saved: got %v, want [anchor]", *saved)
	}
}

// getActiveCollisionProfile indexes the document by name; an undefined profile
// makes calcAlarms throw inside a catch that swallows it. Total silent failure.
func TestCollisionProfileSyncerRefusesAnUndefinedProfile(t *testing.T) {
	missing := `{"current":"offshore","offshore":{"warning":{"cpa":4,"tcpa":30,"speed":0},"danger":{"cpa":2,"tcpa":15,"speed":0},"guard":{"range":0,"speed":0}}}`
	syncer, saved := syncerFor(t, "anchored", missing)

	events := syncer.check(alarmNow)
	if len(*saved) != 0 {
		t.Fatalf("an undefined profile must not be selected, saved %v", *saved)
	}
	if len(events) != 1 || events[0].Kind != alarmEventRaised {
		t.Fatalf("the refusal must be surfaced, got %+v", events)
	}
}

// Already on the right profile: nothing to write.
func TestCollisionProfileSyncerIsQuietWhenAlreadyCorrect(t *testing.T) {
	syncer, saved := syncerFor(t, "sailing", `{"current":"coastal","coastal":{"warning":{"cpa":2,"tcpa":30,"speed":0},"danger":{"cpa":1,"tcpa":10,"speed":0.5},"guard":{"range":0,"speed":0}}}`)

	if events := syncer.check(alarmNow); len(events) != 0 {
		t.Fatalf("got %+v", events)
	}
	if len(*saved) != 0 {
		t.Fatalf("nothing to change, saved %v", *saved)
	}
}

// A state we have no mapping for is left alone rather than guessed at.
func TestCollisionProfileSyncerIgnoresAnUnmappedState(t *testing.T) {
	syncer, saved := syncerFor(t, "aground", collisionProfilesJSON)

	if events := syncer.check(alarmNow); len(events) != 0 {
		t.Fatalf("got %+v", events)
	}
	if len(*saved) != 0 {
		t.Fatalf("an unmapped state must not select a profile, saved %v", *saved)
	}
}

// Decision 4: a raise with no clear leaves an open alarm-log row forever.
func TestCollisionProfileSyncerClearsAnEarlierRefusal(t *testing.T) {
	syncer, saved := syncerFor(t, "anchored", collisionProfilesJSON)

	events := syncer.check(alarmNow)
	if len(events) != 1 || events[0].Kind != alarmEventRaised {
		t.Fatalf("expected the refusal to raise, got %+v", events)
	}

	// Weighing anchor and getting under way selects a profile that works.
	syncer.snapshot = snapshotInState("motoring")
	events = syncer.check(alarmNow.Add(time.Hour))
	if len(*saved) != 1 || (*saved)[0] != "coastal" {
		t.Fatalf("saved: got %v, want [coastal]", *saved)
	}
	if len(events) != 1 || events[0].Kind != alarmEventCleared {
		t.Fatalf("the refusal must clear once a profile is selected, got %+v", events)
	}
}

// A plugin that has moved its endpoint must fail loudly, not drift silently.
func TestCollisionProfileSyncerSurfacesALoadFailure(t *testing.T) {
	syncer := &collisionProfileSyncer{
		snapshot: snapshotInState("anchored"),
		load: func() (string, map[string]collisionProfile, error) {
			return "", nil, errors.New("signalk returned status 404")
		},
		save: func(string) error {
			t.Fatal("nothing should be written when the profiles cannot be read")
			return nil
		},
	}

	events := syncer.check(alarmNow)
	if len(events) != 1 || events[0].Kind != alarmEventRaised {
		t.Fatalf("a load failure must surface, got %+v", events)
	}
}

func failingLoadSyncer(t *testing.T, loadErr error) *collisionProfileSyncer {
	t.Helper()
	return &collisionProfileSyncer{
		snapshot: snapshotInState("anchored"),
		load: func() (string, map[string]collisionProfile, error) {
			return "", nil, loadErr
		},
		save: func(string) error { return nil },
	}
}

// A refusal that is still standing does not raise again on the next
// navigation.state change: the log held 327 open rows in a week.
func TestCollisionProfileSyncerDoesNotRaiseWhileARefusalStands(t *testing.T) {
	syncer := failingLoadSyncer(t, errors.New("boom"))

	if events := syncer.check(alarmNow); len(events) != 1 || events[0].Kind != alarmEventRaised {
		t.Fatalf("first refusal must raise, got %+v", events)
	}
	for i, state := range []string{"motoring", "anchored", "sailing"} {
		syncer.snapshot = snapshotInState(state)
		for _, e := range syncer.check(alarmNow.Add(time.Duration(i+1) * time.Hour)) {
			if e.Kind == alarmEventRaised {
				t.Fatalf("a standing refusal must not raise again, got %+v", e)
			}
		}
	}
}

// After the refusal clears through a successful load, a new failure raises again.
func TestCollisionProfileSyncerRaisesAgainAfterTheRefusalClears(t *testing.T) {
	loadErr := errors.New("boom")
	syncer := &collisionProfileSyncer{
		snapshot: snapshotInState("anchored"),
		load: func() (string, map[string]collisionProfile, error) {
			if loadErr != nil {
				return "", nil, loadErr
			}
			return "coastal", map[string]collisionProfile{}, nil
		},
		save: func(string) error { return nil },
	}
	if events := syncer.check(alarmNow); len(events) != 1 || events[0].Kind != alarmEventRaised {
		t.Fatalf("got %+v", events)
	}

	loadErr = nil
	syncer.snapshot = snapshotInState("motoring")
	if events := syncer.check(alarmNow.Add(time.Hour)); len(events) != 1 || events[0].Kind != alarmEventCleared {
		t.Fatalf("a good load on the new state must clear, got %+v", events)
	}

	loadErr = errors.New("boom again")
	syncer.snapshot = snapshotInState("anchored")
	if events := syncer.check(alarmNow.Add(2 * time.Hour)); len(events) != 1 || events[0].Kind != alarmEventRaised {
		t.Fatalf("a new failure after the clear must raise, got %+v", events)
	}
}

// A standing refusal whose cause or state has changed updates the card in place
// rather than leaving it stale; the same message does nothing.
func TestCollisionProfileSyncerUpdatesAStandingRefusalWhenTheMessageChanges(t *testing.T) {
	loadErr := error(&signalkStatusError{Status: 401})
	syncer := &collisionProfileSyncer{
		snapshot: snapshotInState("anchored"),
		load: func() (string, map[string]collisionProfile, error) {
			return "", nil, loadErr
		},
		save: func(string) error { return nil },
	}
	syncer.check(alarmNow)

	syncer.snapshot = snapshotInState("motoring")
	events := syncer.check(alarmNow.Add(time.Hour))
	if len(events) != 1 || events[0].Kind != alarmEventChanged {
		t.Fatalf("a new state must update the standing card, got %+v", events)
	}
	if !strings.Contains(events[0].Status.Message, "motoring") {
		t.Errorf("updated body: %q", events[0].Status.Message)
	}

	loadErr = errors.New("dial tcp: refused")
	syncer.snapshot = snapshotInState("sailing")
	events = syncer.check(alarmNow.Add(2 * time.Hour))
	if len(events) != 1 || events[0].Kind != alarmEventChanged || !strings.Contains(events[0].Status.Message, "could not read") {
		t.Fatalf("a new cause must update the standing card, got %+v", events)
	}

	// Back to a state that maps to the same profile with the same failure and
	// the same words: nothing to say.
	syncer.snapshot = snapshotInState("motoring")
	events = syncer.check(alarmNow.Add(3 * time.Hour))
	if len(events) != 1 { // "motoring" differs from "sailing" in the body
		t.Fatalf("got %+v", events)
	}
	syncer.lastState = ""
	if events = syncer.check(alarmNow.Add(4 * time.Hour)); len(events) != 0 {
		t.Fatalf("same message must stay quiet, got %+v", events)
	}
}

func TestCollisionProfileSaveRefusedLoginIsClassifiedByStatus(t *testing.T) {
	syncer, _ := syncerFor(t, "motoring", `{"current":"anchor","coastal":{"warning":{"cpa":2,"tcpa":30,"speed":0},"danger":{"cpa":1,"tcpa":10,"speed":0.5},"guard":{"range":0,"speed":0}}}`)
	syncer.save = func(string) error { return &signalkStatusError{Status: 403, Body: "<html>nope</html>"} }
	events := syncer.check(alarmNow)
	want := `SignalK refused Helmcentral's login when switching to the "coastal" collision profile, so the boat is motoring on the old profile.`
	if len(events) != 1 || events[0].Status.Message != want {
		t.Fatalf("got %+v, want %q", events, want)
	}
}

func TestCollisionProfileRefusalBodyIsPlainWording(t *testing.T) {
	authErr := &signalkStatusError{Status: 401, Body: "You do not have permission to view this resource, <a href='/admin/#/login'>Please Login</a>"}
	events := failingLoadSyncer(t, authErr).check(alarmNow)
	if len(events) != 1 {
		t.Fatalf("got %+v", events)
	}
	body := events[0].Status.Message
	want := "SignalK refused Helmcentral's login when reading the collision profiles, so the boat is anchored on the old profile."
	if body != want {
		t.Errorf("auth body:\n got %q\nwant %q", body, want)
	}
	for _, bad := range []string{"<", "401", "signalk returned", "Please Login"} {
		if strings.Contains(body, bad) {
			t.Errorf("body leaks %q: %q", bad, body)
		}
	}

	events = failingLoadSyncer(t, errors.New("dial tcp: refused")).check(alarmNow)
	want = "Helmcentral could not read the collision profiles from SignalK, so the boat is anchored on the old profile."
	if got := events[0].Status.Message; got != want {
		t.Errorf("read body:\n got %q\nwant %q", got, want)
	}
	if events[0].Rule.Label != "Collision Profile Not Changed" {
		t.Errorf("title: %q", events[0].Rule.Label)
	}
}

func TestCollisionProfileOtherRefusalsAreOperatorWording(t *testing.T) {
	missing := `{"current":"offshore","offshore":{"warning":{"cpa":4,"tcpa":30,"speed":0},"danger":{"cpa":2,"tcpa":15,"speed":0},"guard":{"range":0,"speed":0}}}`
	syncer, _ := syncerFor(t, "anchored", missing)
	got := syncer.check(alarmNow)[0].Status.Message
	if want := `The "anchor" collision profile is not defined in SignalK, so the boat is anchored on the old profile.`; got != want {
		t.Errorf("got %q want %q", got, want)
	}

	syncer, _ = syncerFor(t, "anchored", collisionProfilesJSON)
	got = syncer.check(alarmNow)[0].Status.Message
	if want := `The "anchor" collision profile has every CPA and guard range at zero, so it would silence AIS collision alarms. The boat is anchored on the old profile.`; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// A restart forgets the standing refusal. Seeded from the alarm log's open row,
// the same refusal raises nothing, a changed one updates the row, and a good
// load clears it.
func TestCollisionProfileSyncerAdoptsTheOpenRowAfterARestart(t *testing.T) {
	const words = "Helmcentral could not read the collision profiles from SignalK, so the boat is anchored on the old profile."
	loadErr := errors.New("boom")
	newSyncer := func() *collisionProfileSyncer {
		syncer := &collisionProfileSyncer{
			snapshot: snapshotInState("anchored"),
			load: func() (string, map[string]collisionProfile, error) {
				if loadErr != nil {
					return "", nil, loadErr
				}
				return "coastal", map[string]collisionProfile{}, nil
			},
			save: func(string) error { return nil },
		}
		syncer.adoptOpenRefusal(alarmLogEntry{RuleID: collisionProfileRuleID, Message: words, RaisedAt: alarmNow.Add(-time.Hour)})
		return syncer
	}

	syncer := newSyncer()
	if events := syncer.check(alarmNow); len(events) != 0 {
		t.Fatalf("the same refusal after a restart raises nothing, got %+v", events)
	}

	syncer = newSyncer()
	loadErr = &signalkStatusError{Status: 401}
	events := syncer.check(alarmNow)
	if len(events) != 1 || events[0].Kind != alarmEventChanged {
		t.Fatalf("a changed message updates the open row, got %+v", events)
	}

	syncer = newSyncer()
	loadErr = nil
	syncer.snapshot = snapshotInState("motoring")
	if events := syncer.check(alarmNow); len(events) != 1 || events[0].Kind != alarmEventCleared {
		t.Fatalf("a good load clears the adopted row, got %+v", events)
	}
}

func withTestProfileSyncer(t *testing.T, syncer *collisionProfileSyncer) {
	t.Helper()
	original := globalCollisionProfileSyncer
	globalCollisionProfileSyncer = syncer
	t.Cleanup(func() { globalCollisionProfileSyncer = original })
	engine := globalAlarmEngine
	globalAlarmEngine = newAlarmEngine()
	t.Cleanup(func() { globalAlarmEngine = engine })
	withGlobalSnapshot(t, snapshotInState("anchored"))
}

func failingProfileSyncer() *collisionProfileSyncer {
	return &collisionProfileSyncer{
		snapshot: snapshotInState("anchored"),
		load:     func() (string, map[string]collisionProfile, error) { return "", nil, errors.New("boom") },
		save:     func(string) error { return nil },
	}
}

func collisionProfileActive() []alarmStatus {
	var out []alarmStatus
	for _, a := range activeAlarms() {
		if a.RuleID == collisionProfileRuleID {
			out = append(out, a)
		}
	}
	return out
}

func TestCollisionProfileRefusalShowsAsActiveUntilItClears(t *testing.T) {
	syncer := failingProfileSyncer()
	withTestProfileSyncer(t, syncer)
	if got := collisionProfileActive(); len(got) != 0 {
		t.Fatalf("nothing standing yet: %+v", got)
	}
	syncer.check(alarmNow)
	got := collisionProfileActive()
	if len(got) != 1 || got[0].Label != collisionProfileLabel || got[0].Phase != alarmPhaseActive || !got[0].RaisedAt.Equal(alarmNow) || got[0].Message == "" {
		t.Fatalf("a standing refusal is an active alarm: %+v", got)
	}
	if !liveAlarmPaths()[collisionProfilePath] {
		t.Error("the reconciler must not clear the bus copy as a ghost")
	}
	syncer.resolve()
	if got := collisionProfileActive(); len(got) != 0 {
		t.Fatalf("resolved: %+v", got)
	}
	if liveAlarmPaths()[collisionProfilePath] {
		t.Error("a resolved refusal holds no path")
	}
}

func TestCollisionProfileAdoptedRefusalShowsAsActiveWithItsRaisedTime(t *testing.T) {
	syncer := failingProfileSyncer()
	raised := alarmNow.Add(-3 * time.Hour)
	syncer.adoptOpenRefusal(alarmLogEntry{RuleID: collisionProfileRuleID, Message: "old words", RaisedAt: raised})
	withTestProfileSyncer(t, syncer)
	got := collisionProfileActive()
	if len(got) != 1 || !got[0].RaisedAt.Equal(raised) || got[0].Message != "old words" {
		t.Fatalf("adopted: %+v", got)
	}
}

// A restart must not count as a transition: the state restored from the store
// is already acted on, so the operator's manual profile survives a deploy.
func TestCollisionProfileSyncerIgnoresARestartOnTheSameState(t *testing.T) {
	syncer, saved := syncerFor(t, "moored", collisionProfilesJSON)
	attachStored(t, syncer, "moored")

	if events := syncer.check(alarmNow); len(events) != 0 {
		t.Fatalf("a restart on the same state raises nothing, got %+v", events)
	}
	if len(*saved) != 0 {
		t.Fatalf("a restart on the same state must not reselect the profile, saved %v", *saved)
	}
}

// If the boat changed state while Helmcentral was down, that is a transition.
func TestCollisionProfileSyncerActsOnAStateChangedWhileDown(t *testing.T) {
	syncer, saved := syncerFor(t, "sailing", collisionProfilesJSON)
	attachStored(t, syncer, "moored")

	syncer.check(alarmNow)
	if len(*saved) != 1 || (*saved)[0] != "coastal" {
		t.Fatalf("saved: got %v, want [coastal]", *saved)
	}
}

// The state is persisted when acted on, and only then.
func TestCollisionProfileSyncerPersistsTheActedOnState(t *testing.T) {
	syncer, _ := syncerFor(t, "moored", collisionProfilesJSON)
	persisted := []string{}
	syncer.persistState = func(state string) error {
		persisted = append(persisted, state)
		return nil
	}

	syncer.check(alarmNow)
	syncer.check(alarmNow.Add(time.Minute))
	if len(persisted) != 1 || persisted[0] != "moored" {
		t.Fatalf("persisted: got %v, want [moored]", persisted)
	}
}

// A failed write is logged but must not stop the profile switch.
func TestCollisionProfileSyncerSwitchesWhenPersistingFails(t *testing.T) {
	syncer, saved := syncerFor(t, "moored", collisionProfilesJSON)
	syncer.persistState = func(string) error { return errors.New("disk full") }

	syncer.check(alarmNow)
	if len(*saved) != 1 || (*saved)[0] != "harbor" {
		t.Fatalf("saved: got %v, want [harbor]", *saved)
	}
}

func TestCollisionProfileStateSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "helmcentral.sqlite")
	store, err := newAlarmLogStore(path)
	if err != nil {
		t.Fatalf("newAlarmLogStore: %v", err)
	}
	if state, err := store.LoadCollisionProfileState(); err != nil || state != "" {
		t.Fatalf("empty store: got %q, %v", state, err)
	}
	if err := store.SaveCollisionProfileState("anchored"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCollisionProfileState("moored"); err != nil {
		t.Fatal(err)
	}
	store.Close()

	store, err = newAlarmLogStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if state, err := store.LoadCollisionProfileState(); err != nil || state != "moored" {
		t.Fatalf("reopened: got %q, %v, want moored", state, err)
	}
}

// attachStored wires a syncer to a store that already holds state.
func attachStored(t *testing.T, syncer *collisionProfileSyncer, state string) {
	t.Helper()
	err := syncer.attachStateStore(
		func() (string, error) { return state, nil },
		func(string) error { return nil })
	if err != nil {
		t.Fatalf("attachStateStore: %v", err)
	}
}

// A refused transition is not settled, so it is not remembered.
func TestCollisionProfileSyncerDoesNotPersistARefusedTransition(t *testing.T) {
	syncer := failingProfileSyncer()
	persisted := []string{}
	syncer.persistState = func(state string) error {
		persisted = append(persisted, state)
		return nil
	}

	if events := syncer.check(alarmNow); len(events) != 1 || events[0].Kind != alarmEventRaised {
		t.Fatalf("expected a raised refusal, got %+v", events)
	}
	if len(persisted) != 0 {
		t.Fatalf("a refused transition must not be persisted, got %v", persisted)
	}
}

// After a refusal, a restart on the same state retries, selects the profile
// and clears the adopted refusal.
func TestCollisionProfileSyncerRetriesARefusalAfterRestart(t *testing.T) {
	stored := ""
	first := failingProfileSyncer()
	first.snapshot = snapshotInState("moored")
	first.persistState = func(state string) error { stored = state; return nil }
	events := first.check(alarmNow)
	if len(events) != 1 || events[0].Kind != alarmEventRaised {
		t.Fatalf("expected a raised refusal, got %+v", events)
	}

	// SignalK is fixed and Helmcentral restarts with the refusal still open.
	second, saved := syncerFor(t, "moored", collisionProfilesJSON)
	attachStored(t, second, stored)
	second.adoptOpenRefusal(alarmLogEntry{RuleID: collisionProfileRuleID, Message: events[0].Status.Message, RaisedAt: alarmNow})

	cleared := second.check(alarmNow.Add(time.Hour))
	if len(*saved) != 1 {
		t.Fatalf("the restart must retry and select the profile, saved %v", *saved)
	}
	if len(cleared) != 1 || cleared[0].Kind != alarmEventCleared {
		t.Fatalf("the adopted refusal must clear, got %+v", cleared)
	}
}

// standingRefusalSyncer anchors the boat on the shipped, silent anchor profile
// so a refusal stands, then lets a test change what the plugin reports and
// counts how often the profiles are read.
func standingRefusalSyncer(t *testing.T) (syncer *collisionProfileSyncer, setCurrent func(string), setLoadErr func(error), loads *int) {
	t.Helper()
	current, profiles, err := parseCollisionProfiles([]byte(collisionProfilesJSON))
	if err != nil {
		t.Fatalf("parseCollisionProfiles: %v", err)
	}
	var loadErr error
	count := 0
	syncer = &collisionProfileSyncer{
		snapshot: snapshotInState("anchored"),
		load: func() (string, map[string]collisionProfile, error) {
			count++
			if loadErr != nil {
				return "", nil, loadErr
			}
			return current, profiles, nil
		},
		save: func(string) error {
			t.Fatal("polling a standing refusal must never write to the plugin")
			return nil
		},
	}
	if events := syncer.check(alarmNow); len(events) != 1 || events[0].Kind != alarmEventRaised {
		t.Fatalf("expected a raised refusal, got %+v", events)
	}
	count = 0
	return syncer, func(name string) { current = name }, func(err error) { loadErr = err }, &count
}

// The operator picks a working profile by hand while still anchored.
// navigation.state never changes, so only the poll can see it.
func TestCollisionProfileSyncerClearsAStandingRefusalWhenTheOperatorPicksAWorkingProfile(t *testing.T) {
	syncer, setCurrent, _, loads := standingRefusalSyncer(t)
	stored := []string{}
	syncer.persistState = func(state string) error { stored = append(stored, state); return nil }

	setCurrent("harbor")
	events := syncer.check(alarmNow.Add(collisionProfilePollInterval))
	if *loads != 1 {
		t.Fatalf("expected one poll, got %d loads", *loads)
	}
	if len(events) != 1 || events[0].Kind != alarmEventCleared {
		t.Fatalf("the refusal must clear, got %+v", events)
	}
	if got := syncer.active(); len(got) != 0 {
		t.Fatalf("no alarm should stand, got %+v", got)
	}
	// A restart must not replay the transition and refuse again.
	if len(stored) != 1 || stored[0] != "anchored" {
		t.Fatalf("the settled state must be persisted, got %v", stored)
	}
}

func TestCollisionProfileSyncerDoesNotPollBeforeTheIntervalElapses(t *testing.T) {
	syncer, setCurrent, _, loads := standingRefusalSyncer(t)
	setCurrent("harbor")

	if events := syncer.check(alarmNow.Add(collisionProfilePollInterval - time.Second)); len(events) != 0 {
		t.Fatalf("expected no event, got %+v", events)
	}
	if *loads != 0 {
		t.Fatalf("expected no load before the interval, got %d", *loads)
	}
}

func TestCollisionProfileSyncerKeepsTheRefusalWhileTheCurrentProfileCannotFire(t *testing.T) {
	for _, current := range []string{"anchor", "nonesuch"} {
		syncer, setCurrent, _, loads := standingRefusalSyncer(t)
		setCurrent(current)

		if events := syncer.check(alarmNow.Add(collisionProfilePollInterval)); len(events) != 0 {
			t.Fatalf("current %q: expected no event, got %+v", current, events)
		}
		if *loads != 1 || len(syncer.active()) != 1 {
			t.Fatalf("current %q: refusal must stand after one poll, loads %d", current, *loads)
		}
	}
}

// A failed poll is not a pass: the refusal stays, and the next poll waits a
// full interval rather than hammering SignalK.
func TestCollisionProfileSyncerKeepsTheRefusalWhenThePollFails(t *testing.T) {
	syncer, setCurrent, setLoadErr, loads := standingRefusalSyncer(t)
	setCurrent("harbor")
	setLoadErr(errors.New("signalk returned status 502"))

	if events := syncer.check(alarmNow.Add(collisionProfilePollInterval)); len(events) != 0 {
		t.Fatalf("expected no event, got %+v", events)
	}
	if len(syncer.active()) != 1 {
		t.Fatal("a failed poll must leave the refusal standing")
	}
	syncer.check(alarmNow.Add(collisionProfilePollInterval + time.Second))
	if *loads != 1 {
		t.Fatalf("a failed poll must not be retried every tick, got %d loads", *loads)
	}
}

func TestCollisionProfileSyncerDoesNotPollWhenNothingIsRefused(t *testing.T) {
	syncer, _, _, loads := standingRefusalSyncer(t)
	syncer.resolve()

	if events := syncer.check(alarmNow.Add(time.Hour)); len(events) != 0 {
		t.Fatalf("expected no event, got %+v", events)
	}
	if *loads != 0 {
		t.Fatalf("nothing is wrong, so nothing should be read, got %d loads", *loads)
	}
}
