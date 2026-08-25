package main

import (
	"errors"
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
