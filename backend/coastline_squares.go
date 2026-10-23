package main

import (
	_ "embed"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// ── coverage squares (ADR 0203) ───────────────────────────────────────────
//
// The offline coastline is held as fixed 1 degree by 1 degree squares on the
// integer-degree grid. A square is named by its south-west corner, so -21,148
// is 21°S to 20°S and 148°E to 149°E. Fixed squares mean a boat that moves
// never re-downloads what it already holds, and each square can be dated and
// updated on its own.

const (
	// coastlineCoverageRadiusNm is how far from the boat coverage reaches:
	// every square any part of which lies within this distance.
	coastlineCoverageRadiusNm = 200.0

	// squareEdgeSamples is how many points per edge squareDistanceNm samples
	// when finding the nearest point of a square. Edges are one degree long,
	// so 20 steps is 3 nm apart at most.
	squareEdgeSamples = 20

	// coastlineMaxLat keeps squares clear of the pole: beyond it the
	// longitude width of a circle runs away.
	coastlineMaxLat = 89.0
)

// errCoastlineUnsupportedArea is returned for a circle that crosses the
// antimeridian or reaches a pole. Squares do not wrap, and silently covering
// only one side would leave an anchorage with no shoreline across the line
// that reads as open water.
var errCoastlineUnsupportedArea = errors.New("offline coastline does not cover areas across the 180° meridian or near a pole")

type coverageSquare struct {
	Lat int // south edge, whole degrees, -90..89
	Lon int // west edge, whole degrees, -180..179
}

func (s coverageSquare) ID() string { return fmt.Sprintf("%d,%d", s.Lat, s.Lon) }

// Bounds returns south, west, north, east in degrees.
func (s coverageSquare) Bounds() (south, west, north, east float64) {
	return float64(s.Lat), float64(s.Lon), float64(s.Lat + 1), float64(s.Lon + 1)
}

// Label is how the operator and Mate name a square: its south-west corner.
func (s coverageSquare) Label() string {
	ns, ew := "N", "E"
	lat, lon := s.Lat, s.Lon
	if lat < 0 {
		ns, lat = "S", -lat
	}
	if lon < 0 {
		ew, lon = "W", -lon
	}
	return fmt.Sprintf("%d°%s %d°%s", lat, ns, lon, ew)
}

func (s coverageSquare) valid() bool {
	return s.Lat >= -90 && s.Lat <= 89 && s.Lon >= -180 && s.Lon <= 179
}

func parseCoverageSquareID(id string) (coverageSquare, error) {
	parts := strings.Split(id, ",")
	if len(parts) != 2 {
		return coverageSquare{}, fmt.Errorf("coverage square %q is not lat,lon", id)
	}
	lat, err1 := strconv.Atoi(parts[0])
	lon, err2 := strconv.Atoi(parts[1])
	sq := coverageSquare{Lat: lat, Lon: lon}
	if err1 != nil || err2 != nil || !sq.valid() {
		return coverageSquare{}, fmt.Errorf("coverage square %q is not a valid lat,lon", id)
	}
	return sq, nil
}

// squareContaining is the square a position lies in. A point on a shared
// edge belongs to the square that edge is the south or west side of.
func squareContaining(lat, lon float64) (coverageSquare, error) {
	sq := coverageSquare{Lat: int(math.Floor(lat)), Lon: int(math.Floor(lon))}
	if !sq.valid() {
		return coverageSquare{}, fmt.Errorf("%w: %g, %g", errCoastlineUnsupportedArea, lat, lon)
	}
	return sq, nil
}

// squareDistanceNm is the distance from a position to the nearest point of a
// square, 0 inside it. The nearest point of a great-circle distance is not
// simply the clamped coordinate, so the perimeter is sampled.
func squareDistanceNm(lat, lon float64, sq coverageSquare) float64 {
	south, west, north, east := sq.Bounds()
	if lat >= south && lat <= north && lon >= west && lon <= east {
		return 0
	}
	best := math.MaxFloat64
	try := func(pLat, pLon float64) {
		if d := haversineMeters(lat, lon, pLat, pLon) / metersPerNauticalMile; d < best {
			best = d
		}
	}
	for i := 0; i <= squareEdgeSamples; i++ {
		f := float64(i) / squareEdgeSamples
		try(south, west+f)
		try(north, west+f)
		try(south+f, west)
		try(south+f, east)
	}
	return best
}

// squaresWithin returns every square any part of which lies within radiusNm
// of the position, nearest first. A circle across the antimeridian or over a
// pole is an error, not a partial answer.
func squaresWithin(lat, lon, radiusNm float64) ([]coverageSquare, error) {
	if _, err := squareContaining(lat, lon); err != nil {
		return nil, err
	}
	dLat := radiusNm / 60
	if math.Abs(lat)+dLat > coastlineMaxLat {
		return nil, fmt.Errorf("%w: %.1f nm around %.2f, %.2f reaches a pole", errCoastlineUnsupportedArea, radiusNm, lat, lon)
	}
	dLon := dLat / math.Cos((math.Abs(lat)+dLat)*math.Pi/180)
	if lon-dLon < -180 || lon+dLon > 180 {
		return nil, fmt.Errorf("%w: %.1f nm around %.2f, %.2f crosses the 180° meridian", errCoastlineUnsupportedArea, radiusNm, lat, lon)
	}

	type ranked struct {
		sq coverageSquare
		d  float64
	}
	var found []ranked
	for la := int(math.Floor(lat - dLat)); la <= int(math.Floor(lat+dLat)); la++ {
		for lo := int(math.Floor(lon - dLon)); lo <= int(math.Floor(lon+dLon)); lo++ {
			sq := coverageSquare{Lat: la, Lon: lo}
			if !sq.valid() {
				continue
			}
			if d := squareDistanceNm(lat, lon, sq); d <= radiusNm {
				found = append(found, ranked{sq, d})
			}
		}
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].d != found[j].d {
			return found[i].d < found[j].d
		}
		if found[i].sq.Lat != found[j].sq.Lat {
			return found[i].sq.Lat < found[j].sq.Lat
		}
		return found[i].sq.Lon < found[j].sq.Lon
	})
	out := make([]coverageSquare, len(found))
	for i, f := range found {
		out[i] = f.sq
	}
	return out, nil
}

// ── inland squares ────────────────────────────────────────────────────────

// inlandMaskBytes is a 360 x 180 bitmap, bit (lat+90)*360 + (lon+180) set when
// the square is inland: its centre is on land and no part of it lies within
// 20 nm of the sea (the longest shelter ray, 15 nm, plus 5 nm for the
// coarseness of the land data). Such a square can never be needed for a
// shelter estimate, so it is not downloaded. Generated by cmd/inlandmask from
// GSHHG; every error in it falls toward "not inland", which only costs a
// download. ADR 0203.
//
//go:embed data/coastline_inland_squares.bin
var inlandMaskBytes []byte

const inlandMaskSize = 360 * 180 / 8

func init() {
	if len(inlandMaskBytes) != inlandMaskSize {
		panic(fmt.Sprintf("coastline_inland_squares.bin is %d bytes, want %d: regenerate it with cmd/inlandmask", len(inlandMaskBytes), inlandMaskSize))
	}
}

// squareInland reports whether a square is all land, 20 nm or more from the
// sea. An invalid square is never inland.
func squareInland(sq coverageSquare) bool {
	if !sq.valid() {
		return false
	}
	i := (sq.Lat+90)*360 + (sq.Lon + 180)
	return inlandMaskBytes[i/8]&(1<<(i%8)) != 0
}

// coverageWanted is the squares the boat wants downloaded around a position:
// every square within coastlineCoverageRadiusNm, nearest first, without the
// inland ones. skipped is how many inland squares were left out. The worker,
// the status page and the shelter lookup all ask here, so they cannot
// disagree about what coverage is.
func coverageWanted(lat, lon float64) (wanted []coverageSquare, skipped int, err error) {
	inRange, err := squaresWithin(lat, lon, coastlineCoverageRadiusNm)
	if err != nil {
		return nil, 0, err
	}
	wanted = make([]coverageSquare, 0, len(inRange))
	for _, sq := range inRange {
		if squareInland(sq) {
			skipped++
			continue
		}
		wanted = append(wanted, sq)
	}
	return wanted, skipped, nil
}
