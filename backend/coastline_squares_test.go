package main

import (
	"errors"
	"testing"
)

func TestCoverageSquare_BoundsLabelAndID(t *testing.T) {
	sq := coverageSquare{Lat: -21, Lon: 148}
	s, w, n, e := sq.Bounds()
	if s != -21 || n != -20 || w != 148 || e != 149 {
		t.Errorf("bounds %v %v %v %v", s, w, n, e)
	}
	if got := sq.Label(); got != "21°S 148°E" {
		t.Errorf("label %q", got)
	}
	if got := (coverageSquare{Lat: 51, Lon: -2}).Label(); got != "51°N 2°W" {
		t.Errorf("label %q", got)
	}
	back, err := parseCoverageSquareID(sq.ID())
	if err != nil || back != sq {
		t.Errorf("round trip %v %v", back, err)
	}
	if _, err := parseCoverageSquareID("nonsense"); err == nil {
		t.Error("want an error")
	}
	if _, err := parseCoverageSquareID("91,10"); err == nil {
		t.Error("latitude out of range must be refused")
	}
}

func TestSquareContaining(t *testing.T) {
	cases := []struct {
		lat, lon float64
		want     coverageSquare
	}{
		{-20.1, 148.9, coverageSquare{-21, 148}},
		{-21, 148, coverageSquare{-21, 148}}, // a corner belongs to the square it is the south-west of
		{0.5, -0.5, coverageSquare{0, -1}},
		{51.2, -2.0, coverageSquare{51, -2}},
	}
	for _, c := range cases {
		got, err := squareContaining(c.lat, c.lon)
		if err != nil || got != c.want {
			t.Errorf("(%v,%v) = %v, %v; want %v", c.lat, c.lon, got, err, c.want)
		}
	}
	if _, err := squareContaining(90, 0); err == nil {
		t.Error("the pole has no square")
	}
	if _, err := squareContaining(0, 180); err == nil {
		t.Error("longitude 180 has no square")
	}
}

func TestSquaresWithin_200nmAroundStonehaven(t *testing.T) {
	got, err := squaresWithin(-20.1, 148.9, 200)
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != (coverageSquare{-21, 148}) {
		t.Errorf("nearest first: want the boat's own square, got %v", got[0])
	}
	if len(got) < 40 || len(got) > 80 {
		t.Errorf("%d squares for a 200 nm circle is implausible", len(got))
	}
	set := map[coverageSquare]bool{}
	for _, sq := range got {
		if set[sq] {
			t.Errorf("duplicate %v", sq)
		}
		set[sq] = true
	}
	// 2.9 degrees of latitude south is 174 nm: in. 3.9 degrees is 234 nm: out.
	if !set[coverageSquare{-24, 148}] {
		t.Error("square 174 nm south must be in range")
	}
	if set[coverageSquare{-25, 148}] {
		t.Error("square 234 nm south must be out of range")
	}
	// Order is by distance: every square is no nearer than the one before.
	prev := -1.0
	for _, sq := range got {
		d := squareDistanceNm(-20.1, 148.9, sq)
		if d < prev {
			t.Fatalf("not nearest-first at %v (%.1f after %.1f)", sq, d, prev)
		}
		if d > 200 {
			t.Errorf("%v is %.1f nm away", sq, d)
		}
		prev = d
	}
}

func TestSquaresWithin_AnySquareTouchingTheCircleCounts(t *testing.T) {
	// Boat on the south edge of its square, 15 nm: the square to the south
	// is touched, the one two south is not.
	got, err := squaresWithin(-20.999, 148.5, 15)
	if err != nil {
		t.Fatal(err)
	}
	set := map[coverageSquare]bool{}
	for _, sq := range got {
		set[sq] = true
	}
	if !set[coverageSquare{-21, 148}] || !set[coverageSquare{-22, 148}] || set[coverageSquare{-23, 148}] || set[coverageSquare{-20, 148}] {
		t.Errorf("got %v", got)
	}
	// Mid-square, 15 nm reaches no neighbour.
	got, _ = squaresWithin(-20.5, 148.5, 15)
	if len(got) != 1 {
		t.Errorf("mid-square: %v", got)
	}
	// Corner: four squares.
	got, _ = squaresWithin(-20.99, 148.99, 15)
	if len(got) != 4 {
		t.Errorf("corner: %v", got)
	}
}

func TestSquaresWithin_AntimeridianAndPolesAreErrors(t *testing.T) {
	for _, c := range [][2]float64{{-17, 179.5}, {-17, -179.5}, {10, 178}} {
		_, err := squaresWithin(c[0], c[1], 200)
		if !errors.Is(err, errCoastlineUnsupportedArea) {
			t.Errorf("(%v) want errCoastlineUnsupportedArea, got %v", c, err)
		}
	}
	if _, err := squaresWithin(88, 0, 200); !errors.Is(err, errCoastlineUnsupportedArea) {
		t.Errorf("pole: %v", err)
	}
	// 15 nm radius near the antimeridian is fine away from it, error on it.
	if _, err := squaresWithin(-17, 179.9, 15); !errors.Is(err, errCoastlineUnsupportedArea) {
		t.Errorf("shelter across the antimeridian: %v", err)
	}
	if _, err := squaresWithin(-17, 170, 15); err != nil {
		t.Errorf("far from it: %v", err)
	}
}

func TestSquareInland_KnownSquares(t *testing.T) {
	cases := []struct {
		name   string
		sq     coverageSquare
		inland bool
	}{
		{"central Australia", coverageSquare{Lat: -24, Lon: 133}, true},
		{"Alps", coverageSquare{Lat: 47, Lon: 10}, true},
		{"Whitsundays, Hook Island", coverageSquare{Lat: -21, Lon: 148}, false},
		{"Whitsundays, Hayman side", coverageSquare{Lat: -21, Lon: 149}, false},
		{"mid Pacific", coverageSquare{Lat: 0, Lon: -140}, false},
		{"Coral Sea", coverageSquare{Lat: -15, Lon: 155}, false},
		{"Caspian Sea", coverageSquare{Lat: 42, Lon: 50}, false},
		{"Solent", coverageSquare{Lat: 50, Lon: -2}, false},
		{"Antarctica (not in the land data)", coverageSquare{Lat: -80, Lon: 0}, false},
	}
	for _, c := range cases {
		if got := squareInland(c.sq); got != c.inland {
			t.Errorf("%s %v: inland = %v, want %v", c.name, c.sq, got, c.inland)
		}
	}
}

func TestSquareInland_OutOfRangeIsNotInland(t *testing.T) {
	if squareInland(coverageSquare{Lat: 95, Lon: 0}) || squareInland(coverageSquare{Lat: 0, Lon: 200}) {
		t.Error("an invalid square must never be treated as inland")
	}
}

func TestSquareInland_SeaIsNeverNearInlandSquares(t *testing.T) {
	// Every shelter ray is at most 15 nm, so a square that touches the
	// 20 nm band of any square at the Whitsundays must not be inland.
	wanted, err := squaresWithin(-20.1, 148.9, 40)
	if err != nil {
		t.Fatal(err)
	}
	for _, sq := range wanted {
		if squareInland(sq) {
			t.Errorf("%v is within 40 nm of Hook Island yet inland", sq)
		}
	}
}

func TestCoverageWanted_DropsInlandSquaresAndCountsThem(t *testing.T) {
	// Mackay hinterland: the 200 nm circle reaches square after square of
	// the Queensland interior.
	lat, lon := -20.1, 148.9
	all, err := squaresWithin(lat, lon, coastlineCoverageRadiusNm)
	if err != nil {
		t.Fatal(err)
	}
	wanted, skipped, err := coverageWanted(lat, lon)
	if err != nil {
		t.Fatal(err)
	}
	if skipped == 0 {
		t.Fatal("the circle reaches the Queensland interior, some squares must be skipped")
	}
	if len(wanted)+skipped != len(all) {
		t.Errorf("wanted %d + skipped %d != %d in range", len(wanted), skipped, len(all))
	}
	for _, sq := range wanted {
		if squareInland(sq) {
			t.Errorf("%v is inland but wanted", sq)
		}
	}
	// Order is kept: still nearest first.
	last := -1.0
	for _, sq := range wanted {
		d := squareDistanceNm(lat, lon, sq)
		if d < last {
			t.Fatalf("not nearest first at %v", sq)
		}
		last = d
	}
}

func TestCoverageWanted_RefusesUnsupportedAreas(t *testing.T) {
	if _, _, err := coverageWanted(-17, 179.6); !errors.Is(err, errCoastlineUnsupportedArea) {
		t.Errorf("err = %v", err)
	}
}
