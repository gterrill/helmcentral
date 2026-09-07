package main

import (
	"testing"
	"time"
)

func TestGNSSDepartureDoesNotDependOnAnchoredLabel(t *testing.T) {
	for _, running := range []bool{false, true} {
		resetGNSSPositionValidationState()
		now := time.Now().UTC()
		good := gnssPositionValidation{Status: "trusted", Trusted: true}
		first := gnssObservedSample{Latitude: -20, Longitude: 149, Navigation: "anchored", DepthMeters: 10, HasObservedAt: true, ObservedAt: now, EnginesRunning: running}
		applyGNSSHeuristics(good, first, now)
		next := first
		next.Latitude += 0.001
		next.ObservedAt = now.Add(10 * time.Second)
		got := applyGNSSHeuristics(good, next, next.ObservedAt)
		if running && got.Status != "trusted" {
			t.Fatalf("running engine departure frozen by anchored label: %+v", got)
		}
		if !running && !got.Critical {
			t.Fatalf("stationary jump protection lost: %+v", got)
		}
	}
	resetGNSSPositionValidationState()
}

func TestGNSSRunningEnginesDoNotBypassBadFix(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)
	now := time.Now().UTC()
	bad := gnssPositionValidation{Status: "critical", Critical: true, Reason: "no fix"}
	got := applyGNSSHeuristics(bad, gnssObservedSample{Latitude: -20, Longitude: 149, Navigation: "anchored", EnginesRunning: true, HasObservedAt: true, ObservedAt: now}, now)
	if !got.Critical || got.Trusted {
		t.Fatalf("bad fix was bypassed: %+v", got)
	}
}
