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
