package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// replayFixture applies every leaf of a captured SignalK self tree to a
// snapshot. freshness maps a dotted path prefix to how long before now it was
// last seen; everything else is one second old.
func replayFixture(t *testing.T, file string, now time.Time, age map[string]time.Duration) *signalKSnapshot {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var tree map[string]any
	if err := json.Unmarshal(raw, &tree); err != nil {
		t.Fatal(err)
	}
	snap := newSignalKSnapshot()
	snap.setSelfContext("vessels.self")
	var walk func(prefix string, node map[string]any)
	walk = func(prefix string, node map[string]any) {
		if v, ok := node["value"]; ok {
			seen := now.Add(-time.Second)
			for pre, d := range age {
				if strings.HasPrefix(prefix, pre) {
					seen = now.Add(-d)
				}
			}
			snap.applyDelta(signalKDelta{Context: "vessels.self", Updates: []signalKUpdate{{Values: []signalKValue{{Path: prefix, Value: v}}}}}, seen)
			return
		}
		for k, child := range node {
			if m, ok := child.(map[string]any); ok {
				next := k
				if prefix != "" {
					next = prefix + "." + k
				}
				walk(next, m)
			}
		}
	}
	walk("", tree)
	return snap
}

const liveSubset = "testdata/passages/self-subset-2026-10-14.json"

func liveState() vesselStateData {
	return vesselStateData{Latitude: -19.25215335783864, Longitude: 146.82380063270864, PositionLastUpdateAge: 1, GeneratorState: "stopped"}
}

func TestBuildTrackSampleFromLiveCapture(t *testing.T) {
	now := time.Now()
	snap := replayFixture(t, liveSubset, now, map[string]time.Duration{
		"propulsion.": 48 * time.Hour,
		// the GX only publishes the generator on change
		"electrical.generator": 14 * time.Second,
	})
	s := buildTrackSample(snap, liveState(), "electrical.batteries.0", nil, now)

	if !s.FixOK {
		t.Error("fresh untrusted-free fix must be OK")
	}
	if s.SOGKts == nil || !approx(*s.SOGKts, 0.0149188926683417*metersPerSecondToKnots, 1e-9) {
		t.Errorf("sog %v", s.SOGKts)
	}
	if s.COGDeg == nil || !approx(*s.COGDeg, 3.1743*180/3.141592653589793, 1e-6) {
		t.Errorf("cog %v", s.COGDeg)
	}
	if s.TWSKts == nil || !approx(*s.TWSKts, 2.41*metersPerSecondToKnots, 1e-9) {
		t.Errorf("tws %v", s.TWSKts)
	}
	if s.SoCPct == nil || *s.SoCPct != 100 {
		t.Errorf("soc %v", s.SoCPct)
	}
	// .0 is 1.0 x 0.34 m3, .3 is 0.79752 x 0.66 m3
	if s.WaterL == nil || !approx(*s.WaterL, 340+0.79752*660, 1e-6) {
		t.Errorf("water %v, want the sum of both tanks", s.WaterL)
	}
	if s.GenState == nil || *s.GenState != "stopped" {
		t.Errorf("a generator state published 14 s ago is still the state: %v", s.GenState)
	}
	if s.FuelLph != nil {
		t.Errorf("rates last seen two days ago with no engine-off knowledge must be absent, got %v", *s.FuelLph)
	}
}

func TestBuildTrackSampleStaleMeasurementsAreAbsent(t *testing.T) {
	now := time.Now()
	snap := replayFixture(t, liveSubset, now, map[string]time.Duration{
		"navigation.speedOverGround":      11 * time.Second,
		"environment.wind":                time.Hour,
		"electrical.batteries":            30 * time.Second,
		"tanks.freshWater.3.currentLevel": time.Minute,
		"propulsion.":                     time.Second,
	})
	s := buildTrackSample(snap, liveState(), "electrical.batteries.0", nil, now)
	if s.SOGKts != nil || s.TWSKts != nil || s.SoCPct != nil {
		t.Errorf("readings over 10 s old must be absent: sog=%v tws=%v soc=%v", s.SOGKts, s.TWSKts, s.SoCPct)
	}
	if s.WaterL != nil {
		t.Errorf("one old tank level must leave the sum absent, got %v", *s.WaterL)
	}
	// both engines reporting a fresh rate: 0 and 4.1667e-7 m3/s = 1.5 L/h
	if s.FuelLph == nil || !approx(*s.FuelLph, 1.5, 1e-6) {
		t.Errorf("fuel %v, want 1.5 L/h", s.FuelLph)
	}
}

func TestBuildTrackSampleNoHouseBankMeansNoStateOfCharge(t *testing.T) {
	now := time.Now()
	snap := replayFixture(t, liveSubset, now, nil)
	if s := buildTrackSample(snap, liveState(), "", nil, now); s.SoCPct != nil {
		t.Errorf("soc %v: with no house bank chosen there is no battery to read", *s.SoCPct)
	}
}

func TestBuildTrackSampleFixNotOKWhenCriticalOrOld(t *testing.T) {
	now := time.Now()
	snap := replayFixture(t, liveSubset, now, nil)
	st := liveState()
	st.GNSSCriticalAlert = true
	if buildTrackSample(snap, st, "", nil, now).FixOK {
		t.Error("gnss_critical_alert must reject the fix")
	}
	st = liveState()
	st.PositionLastUpdateAge = 12
	if buildTrackSample(snap, st, "", nil, now).FixOK {
		t.Error("a fix older than 10 s must be rejected")
	}
	st.PositionLastUpdateAge = -1
	if buildTrackSample(snap, st, "", nil, now).FixOK {
		t.Error("a fix of unknown age cannot be shown fresh")
	}
}

func TestTrackFuelRateOffEngineIsKnownZero(t *testing.T) {
	now := time.Now()
	read := func(path string) alarmSample {
		if strings.Contains(path, "port") {
			return alarmSample{EngineOff: true}
		}
		return alarmSample{Present: true, Value: 2.0 / 3600000, LastSeen: now.Add(-time.Second)}
	}
	got := trackFuelRateLph(read, []string{"propulsion.port.fuel.rate", "propulsion.starboard.fuel.rate"}, now)
	if got == nil || !approx(*got, 2, 1e-9) {
		t.Errorf("fuel %v, want 2 L/h", got)
	}
	allOff := func(string) alarmSample { return alarmSample{EngineOff: true} }
	if got := trackFuelRateLph(allOff, []string{"propulsion.port.fuel.rate"}, now); got == nil || *got != 0 {
		t.Errorf("all engines off is a known zero, got %v", got)
	}
	if got := trackFuelRateLph(read, nil, now); got != nil {
		t.Errorf("no rate paths at all is absent, got %v", *got)
	}
}

// The passage log finds burn paths by collectFuelPaths' rule, nested ids
// included, while walking only the propulsion branch.
func TestFuelRatePathsMatchesCollectFuelPaths(t *testing.T) {
	now := time.Now()
	snap := newSignalKSnapshot()
	snap.setSelfContext("vessels.self")
	for _, p := range []string{"propulsion.port.fuel.rate", "propulsion.gen.set.fuel.rate", "propulsion.port.revolutions", "tanks.fuel.0.currentLevel", "electrical.batteries.0.fuel.rate"} {
		snap.applyDelta(signalKDelta{Context: "vessels.self", Updates: []signalKUpdate{{Values: []signalKValue{{Path: p, Value: 1.0}}}}}, now)
	}
	view := snap.selfTelemetryAt(now)
	want, _ := collectFuelPaths(view.Paths())
	got := fuelRatePaths(view)
	if strings.Join(got, ",") != strings.Join(want, ",") || len(got) != 2 {
		t.Fatalf("fuelRatePaths %v, collectFuelPaths %v", got, want)
	}
}
