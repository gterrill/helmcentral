package main

import (
	"testing"
	"time"
)

// The 2026-10-21 shore-power disconnect: the Venus GX system service switched
// electrical.venus.acSource to "battery" (acSourceNumber 240) and the Skylla-i
// shore charger, which lives on shore AC, went quiet through both of its
// views: the N2K gateway's and the GX's own charger service.
const (
	skyllaN2K = "YachtDevices.35"
	skyllaGX  = "venus.com.victronenergy.charger.0"
	gxSystem  = "venus.com.victronenergy.system.0"
)

func shoreOffNow() time.Time { return time.Date(2026, 10, 21, 22, 20, 0, 0, time.UTC) }

func shoreOffSwitchover() time.Time { return time.Date(2026, 10, 21, 22, 10, 45, 0, time.UTC) }

func shoreOffSources(now time.Time) []sourceHealth {
	first := now.Add(-3 * time.Hour)
	return []sourceHealth{
		{Source: skyllaN2K, First: first, Last: time.Date(2026, 10, 21, 22, 10, 39, 0, time.UTC), Count: 5000},
		{Source: skyllaGX, First: first, Last: time.Date(2026, 10, 21, 22, 10, 50, 0, time.UTC), Count: 5000},
		{Source: gxSystem, First: first, Last: now.Add(-2 * time.Second), Count: 90000},
	}
}

func onBatteryAt(at time.Time) acSourceState {
	return acSourceState{BatteryAt: at, Sources: []string{gxSystem}}
}

func TestSilentSourcesExcusesShoreChargerAfterShorePowerOff(t *testing.T) {
	now := shoreOffNow()
	got := silentSources(shoreOffSources(now), now, time.Second, nil, onBatteryAt(shoreOffSwitchover()))
	if len(got) != 0 {
		t.Fatalf("both views of the shore charger go quiet with shore power; got %v", got)
	}
}

func TestSilentSourcesReportsQuietDeviceOnShorePower(t *testing.T) {
	now := shoreOffNow()
	// The GX publishes acSource, last seen as grid: no battery transition.
	state := acSourceState{Sources: []string{gxSystem}}
	got := silentSources(shoreOffSources(now), now, time.Second, nil, state)
	if len(got) != 2 {
		t.Fatalf("a charger silent on shore power is a fault; got %v", got)
	}
}

func TestSilentSourcesReportsDeviceQuietAgainOnceShoreIsBack(t *testing.T) {
	now := shoreOffNow()
	snap := newSignalKSnapshot()
	snap.setSelfContext("vessels.self")
	at := func(d time.Duration) time.Time { return shoreOffSwitchover().Add(d) }
	snap.applyDelta(acDelta(gxSystem, "electrical.venus.acSource", "shore", at(-time.Minute)), at(-time.Minute))
	snap.applyDelta(acDelta(gxSystem, "electrical.venus.acSource", "battery", at(0)), at(0))
	if got := silentSources(shoreOffSources(now), now, time.Second, nil, snap.acSourceFor()); len(got) != 0 {
		t.Fatalf("on battery both are excused; got %v", got)
	}
	snap.applyDelta(acDelta(gxSystem, "electrical.venus.acSource", "shore", at(5*time.Minute)), at(5*time.Minute))
	got := silentSources(shoreOffSources(now), now, time.Second, nil, snap.acSourceFor())
	if len(got) != 2 || got[0] != skyllaN2K || got[1] != skyllaGX {
		t.Fatalf("back on shore and still quiet; got %v, want both reported", got)
	}
}

func TestSilentSourcesDoesNotExcuseAnythingOnTheACSourceConnection(t *testing.T) {
	now := shoreOffNow()
	// The GX crashed just after the unplugging: every venus.* service is quiet.
	sources := shoreOffSources(now)
	sources[2].Last = shoreOffSwitchover().Add(5 * time.Second)
	got := silentSources(sources, now, time.Second, nil, onBatteryAt(shoreOffSwitchover()))
	reported := map[string]bool{}
	for _, g := range got {
		reported[g] = true
	}
	if !reported[skyllaGX] || !reported[gxSystem] || reported[skyllaN2K] {
		t.Fatalf("got %v, want every venus.* service reported and the N2K view excused", got)
	}
}

func TestSilentSourcesReportsDeviceQuietTwoMinutesAfterSwitchover(t *testing.T) {
	now := shoreOffNow()
	sources := shoreOffSources(now)
	sources[0].Last = shoreOffSwitchover().Add(2 * time.Minute) // went quiet long after
	got := silentSources(sources, now, time.Second, nil, onBatteryAt(shoreOffSwitchover()))
	if len(got) != 1 || got[0] != skyllaN2K {
		t.Fatalf("got %v, want only %s", got, skyllaN2K)
	}
}

func TestSilentSourcesWithoutAnACSourcePublisherIsUnchanged(t *testing.T) {
	now := shoreOffNow()
	got := silentSources(shoreOffSources(now), now, time.Second, nil, acSourceState{})
	if len(got) != 2 {
		t.Fatalf("no acSource publisher, nothing is excused; got %v", got)
	}
}

func TestSilentSourcesDoesNotExcuseTheACSourcePublisherItself(t *testing.T) {
	now := shoreOffNow()
	sources := shoreOffSources(now)
	sources[2].Last = shoreOffSwitchover().Add(5 * time.Second) // the GX itself went quiet
	got := silentSources(sources, now, time.Second, nil, onBatteryAt(shoreOffSwitchover()))
	found := false
	for _, s := range got {
		found = found || s == gxSystem
	}
	if !found {
		t.Fatalf("the acSource publisher going quiet must be reported; got %v", got)
	}
}

// --- tracking in the snapshot ------------------------------------------------

func acDelta(source, path string, value any, at time.Time) signalKDelta {
	return signalKDelta{Context: "vessels.self", Updates: []signalKUpdate{
		{SourceRef: source, Timestamp: at.Format(time.RFC3339), Values: []signalKValue{{Path: path, Value: value}}},
	}}
}

func TestACSourceRecordsTheTransitionNotTheRepeats(t *testing.T) {
	s := newSignalKSnapshot()
	s.setSelfContext("vessels.self")
	base := time.Date(2026, 10, 21, 22, 0, 0, 0, time.UTC)
	s.applyDelta(acDelta(gxSystem, "electrical.venus.acSource", "shore", base), base)
	s.applyDelta(acDelta(gxSystem, "electrical.venus.acSource", "battery", base.Add(45*time.Second)), base.Add(45*time.Second))
	for i := 1; i <= 3; i++ { // the GX republishes the same value every few seconds
		at := base.Add(45*time.Second + time.Duration(i)*5*time.Second)
		s.applyDelta(acDelta(gxSystem, "electrical.venus.acSource", "battery", at), at)
	}
	st := s.acSourceFor()
	if !st.BatteryAt.Equal(base.Add(45*time.Second)) || !st.publishes(gxSystem) {
		t.Fatalf("got %+v, want transition at +45s published by %s", st, gxSystem)
	}
}

func TestACSourceReadsTheNumberAndClearsWhenBackOnShore(t *testing.T) {
	s := newSignalKSnapshot()
	s.setSelfContext("vessels.self")
	base := time.Date(2026, 10, 21, 22, 0, 0, 0, time.UTC)
	s.applyDelta(acDelta(gxSystem, "electrical.venus.acSourceNumber", 3.0, base), base)
	s.applyDelta(acDelta(gxSystem, "electrical.venus.acSourceNumber", 240.0, base.Add(time.Minute)), base.Add(time.Minute))
	if st := s.acSourceFor(); !st.BatteryAt.Equal(base.Add(time.Minute)) {
		t.Fatalf("240 is battery; got %+v", st)
	}
	s.applyDelta(acDelta(gxSystem, "electrical.venus.acSourceNumber", 3.0, base.Add(time.Hour)), base.Add(time.Hour))
	if st := s.acSourceFor(); !st.BatteryAt.IsZero() || !st.publishes(gxSystem) {
		t.Fatalf("shore is back; got %+v", st)
	}
}

func TestACSourceFirstSightOfBatteryIsNotATransition(t *testing.T) {
	s := newSignalKSnapshot()
	s.setSelfContext("vessels.self")
	base := time.Date(2026, 10, 21, 22, 0, 0, 0, time.UTC)
	s.applyDelta(acDelta(gxSystem, "electrical.venus.acSource", "battery", base), base)
	if st := s.acSourceFor(); !st.BatteryAt.IsZero() || !st.publishes(gxSystem) {
		t.Fatalf("a restart on battery does not know when the switch happened; got %+v", st)
	}
}

func TestACSourceIgnoresOtherVesselsAndOtherPaths(t *testing.T) {
	s := newSignalKSnapshot()
	s.setSelfContext("vessels.self")
	base := time.Date(2026, 10, 21, 22, 0, 0, 0, time.UTC)
	s.applyDelta(acDelta(gxSystem, "electrical.venus.acSource", "shore", base), base)
	s.applyDelta(acDelta(gxSystem, "electrical.venus.acSource", "battery", base.Add(time.Minute)), base.Add(time.Minute))
	other := acDelta("x.y", "electrical.venus.acSource", "battery", base)
	other.Context = "vessels.urn:mrn:imo:mmsi:1"
	s.applyDelta(other, base)
	s.applyDelta(acDelta("n2k.1", "navigation.acSource", "battery", base), base)
	if st := s.acSourceFor(); len(st.Sources) != 1 {
		t.Fatalf("got %+v, want only %s", st, gxSystem)
	}
}

func TestACSourceIgnoresNullAndUnknownReadings(t *testing.T) {
	s := newSignalKSnapshot()
	s.setSelfContext("vessels.self")
	base := time.Date(2026, 10, 21, 22, 0, 0, 0, time.UTC)
	sw := base.Add(time.Minute)
	s.applyDelta(acDelta(gxSystem, "electrical.venus.acSource", "shore", base), base)
	s.applyDelta(acDelta(gxSystem, "electrical.venus.acSource", "battery", sw), sw)
	for i, v := range []any{nil, "NA", 0.0, "mystery"} {
		at := sw.Add(time.Duration(i+1) * 10 * time.Second)
		s.applyDelta(acDelta(gxSystem, "electrical.venus.acSource", v, at), at)
	}
	at := sw.Add(time.Minute)
	s.applyDelta(acDelta(gxSystem, "electrical.venus.acSource", "battery", at), at)
	if st := s.acSourceFor(); !st.BatteryAt.Equal(sw) {
		t.Fatalf("got %+v, want the original switch at %v", st, sw)
	}
}

func TestACSourceIgnoresAnOlderReadingArrivingLate(t *testing.T) {
	s := newSignalKSnapshot()
	s.setSelfContext("vessels.self")
	base := time.Date(2026, 10, 21, 22, 10, 0, 0, time.UTC)
	s.applyDelta(acDelta(gxSystem, "electrical.venus.acSource", "shore", base), base)
	s.applyDelta(acDelta(gxSystem, "electrical.venus.acSource", "battery", base.Add(45*time.Second)), base.Add(45*time.Second))
	s.applyDelta(acDelta(gxSystem, "electrical.venus.acSource", "shore", base.Add(40*time.Second)), base.Add(46*time.Second))
	if st := s.acSourceFor(); !st.BatteryAt.Equal(base.Add(45 * time.Second)) {
		t.Fatalf("got %+v, want still battery at +45s", st)
	}
}

func TestACSourceReconnectIsAFirstSighting(t *testing.T) {
	s := newSignalKSnapshot()
	s.setSelfContext("vessels.self")
	base := time.Date(2026, 10, 21, 22, 0, 0, 0, time.UTC)
	s.applyDelta(acDelta(gxSystem, "electrical.venus.acSource", "shore", base), base)
	s.setConnected(false)
	s.setConnected(true)
	// The reconnect dump carries the new value.
	s.applyDelta(acDelta(gxSystem, "electrical.venus.acSource", "battery", base.Add(time.Hour)), base.Add(time.Hour))
	if st := s.acSourceFor(); !st.BatteryAt.IsZero() {
		t.Fatalf("a change first seen after a reconnect is not a known transition; got %+v", st)
	}
}

func TestACSourceClearedWhenSelfContextChanges(t *testing.T) {
	s := newSignalKSnapshot()
	s.setSelfContext("vessels.self")
	base := time.Date(2026, 10, 21, 22, 0, 0, 0, time.UTC)
	s.applyDelta(acDelta(gxSystem, "electrical.venus.acSource", "shore", base), base)
	s.setSelfContext("vessels.urn:mrn:imo:mmsi:2")
	if st := s.acSourceFor(); len(st.Sources) != 0 {
		t.Fatalf("got %+v, want nothing tracked for the new vessel", st)
	}
}
