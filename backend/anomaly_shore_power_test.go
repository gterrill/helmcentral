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
	return acSourceState{BatteryAt: at, Publishers: map[string]bool{gxSystem: true}}
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
	state := acSourceState{Publishers: map[string]bool{gxSystem: true}}
	got := silentSources(shoreOffSources(now), now, time.Second, nil, state)
	if len(got) != 2 {
		t.Fatalf("a charger silent on shore power is a fault; got %v", got)
	}
}

func TestSilentSourcesReportsDeviceQuietAgainOnceShoreIsBack(t *testing.T) {
	now := shoreOffNow()
	// Back on shore: the state no longer reads battery.
	state := acSourceState{Publishers: map[string]bool{gxSystem: true}}
	got := silentSources(shoreOffSources(now), now, time.Second, nil, state)
	if len(got) != 2 || got[0] != skyllaN2K || got[1] != skyllaGX {
		t.Fatalf("got %v, want both sources reported", got)
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
	st := s.acSourceFor("vessels.self")
	if !st.BatteryAt.Equal(base.Add(45*time.Second)) || !st.Publishers[gxSystem] {
		t.Fatalf("got %+v, want transition at +45s published by %s", st, gxSystem)
	}
}

func TestACSourceReadsTheNumberAndClearsWhenBackOnShore(t *testing.T) {
	s := newSignalKSnapshot()
	s.setSelfContext("vessels.self")
	base := time.Date(2026, 10, 21, 22, 0, 0, 0, time.UTC)
	s.applyDelta(acDelta(gxSystem, "electrical.venus.acSourceNumber", 3.0, base), base)
	s.applyDelta(acDelta(gxSystem, "electrical.venus.acSourceNumber", 240.0, base.Add(time.Minute)), base.Add(time.Minute))
	if st := s.acSourceFor("vessels.self"); !st.BatteryAt.Equal(base.Add(time.Minute)) {
		t.Fatalf("240 is battery; got %+v", st)
	}
	s.applyDelta(acDelta(gxSystem, "electrical.venus.acSourceNumber", 3.0, base.Add(time.Hour)), base.Add(time.Hour))
	if st := s.acSourceFor("vessels.self"); !st.BatteryAt.IsZero() || !st.Publishers[gxSystem] {
		t.Fatalf("shore is back; got %+v", st)
	}
}

func TestACSourceFirstSightOfBatteryIsNotATransition(t *testing.T) {
	s := newSignalKSnapshot()
	s.setSelfContext("vessels.self")
	base := time.Date(2026, 10, 21, 22, 0, 0, 0, time.UTC)
	s.applyDelta(acDelta(gxSystem, "electrical.venus.acSource", "battery", base), base)
	if st := s.acSourceFor("vessels.self"); !st.BatteryAt.IsZero() || !st.Publishers[gxSystem] {
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
	if st := s.acSourceFor("vessels.self"); len(st.Publishers) != 1 {
		t.Fatalf("got %+v, want only %s", st, gxSystem)
	}
}
