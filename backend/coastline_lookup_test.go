package main

import (
	"strings"
	"testing"
)

func TestShorelineAround_SurfacesAnUnusableBoatPositionInsteadOfOutsideCoverage(t *testing.T) {
	store := newTestCoastlineStore(t)
	r := &offlineCoastlineReader{
		Enabled: func() (bool, error) { return true, nil },
		Store:   store,
		// A boat too near the 180th meridian for coverage to be worked out.
		Boat: func() (float64, float64, bool) { return -17, 179.6, true },
	}
	_, err := r.ShorelineAround(-20.1, 148.9)
	if err == nil {
		t.Fatal("nothing is held, so this must fail")
	}
	if strings.Contains(err.Error(), "outside Offline coastline coverage") || !strings.Contains(err.Error(), "180") {
		t.Errorf("the real problem must be surfaced, got %q", err)
	}
}

func TestShorelineAround_AnInlandPositionSaysItIsAshore(t *testing.T) {
	store := newTestCoastlineStore(t)
	r := &offlineCoastlineReader{
		Enabled: func() (bool, error) { return true, nil },
		Store:   store,
		Boat:    func() (float64, float64, bool) { return -20.1, 148.9, true },
	}
	// Central Australia: 20 nm from the sea in every direction and then some.
	_, err := r.ShorelineAround(-23.5, 133.5)
	if err == nil {
		t.Fatal("an inland position has no anchorage shelter")
	}
	msg := err.Error()
	if !strings.Contains(msg, "inland") || strings.Contains(msg, "outside Offline coastline coverage") || strings.Contains(msg, "still downloading") {
		t.Errorf("want a plain inland message, got %q", msg)
	}
}
