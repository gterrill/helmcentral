package main

import (
	"strings"
	"testing"
	"time"
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

func TestShorelineAround_AHeldInlandOwnSquareIsUsed(t *testing.T) {
	store := newTestCoastlineStore(t)
	if err := store.SaveSquare(coverageSquare{Lat: -24, Lon: 133}, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	r := &offlineCoastlineReader{Enabled: func() (bool, error) { return true, nil }, Store: store}
	if _, err := r.ShorelineAround(-23.5, 133.5); err != nil {
		t.Errorf("a square held from before is used: %v", err)
	}
}

func TestShorelineAround_AnUnheldInlandNeighbourHoldsNoCoastline(t *testing.T) {
	store := newTestCoastlineStore(t)
	r := &offlineCoastlineReader{Enabled: func() (bool, error) { return true, nil }, Store: store}
	// Find a position in a coastal square whose 15 nm reaches an inland one.
	for lat := -23.0; lat < -20.0; lat += 0.05 {
		for lon := 145.0; lon < 149.0; lon += 0.05 {
			own, err := squareContaining(lat, lon)
			if err != nil || squareInland(own) {
				continue
			}
			needed, _ := squaresWithin(lat, lon, shelterMaxFetchNm)
			hasInland := false
			for _, sq := range needed {
				if squareInland(sq) {
					hasInland = true
				}
			}
			if !hasInland {
				continue
			}
			for _, sq := range needed {
				if !squareInland(sq) {
					if err := store.SaveSquare(sq, nil, time.Now()); err != nil {
						t.Fatal(err)
					}
				}
			}
			set, err := r.ShorelineAround(lat, lon)
			if err != nil {
				t.Fatalf("%.2f, %.2f: an unheld inland neighbour must not stop the estimate: %v", lat, lon, err)
			}
			if set.Oldest.IsZero() {
				t.Error("oldest must come from the squares used")
			}
			return
		}
	}
	t.Fatal("no coastal position next to an inland square found")
}
