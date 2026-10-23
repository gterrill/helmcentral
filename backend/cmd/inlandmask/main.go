// Command inlandmask writes backend/coastline_data/coastline_inland_squares.bin, the
// bitmap the offline coastline uses to skip coverage squares that are all
// land (ADR 0203, "Inland squares are skipped").
//
// Source: GSHHG low-resolution level 1 land polygons (public domain), as
// GeoJSON. The file is not kept in the repository; take it from history:
//
//	git show b330ff78^:backend/data/gshhg_coastline_l.json > /tmp/gshhg.json
//	cd backend && go run ./cmd/inlandmask /tmp/gshhg.json coastline_data/coastline_inland_squares.bin
//
// Upstream, should the repository history be lost: NOAA GSHHG 2.3.7,
// GSHHS_l_L1 (low resolution, level 1 land), converted to GeoJSON polygons
// (https://www.ngdc.noaa.gov/mgg/shorelines/). Rebuild the JSON from that
// shapefile as one Polygon feature per ring, [lon, lat] coordinates.
//
// Output: 360 x 180 bits, 8100 bytes. Bit (lat+90)*360 + (lon+180), least
// significant bit first within a byte, is set when the 1 degree square whose
// south-west corner is lat, lon is inland.
//
// A square is inland when its centre is on land AND no point of it lies
// within inlandMarginNm of any land polygon edge. The margin is the longest
// shelter ray (15 nm) plus 5 nm for the coarseness of the land data. Every
// error is meant to fall toward "not inland", which only costs a download:
// Antarctica and small islands missing from the data, and the Caspian Sea
// (which GSHHG fills as land but OpenStreetMap draws as coastline), are all
// forced to "not inland".
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
)

const (
	inlandMarginNm = 20.0
	densifyNm      = 1.0
	edgeSamples    = 20   // coarse scan per edge before refining
	distTolNm      = 0.05 // slack added to the margin so rounding never favours inland
	earthRadiusNm  = 3440.065
)

type pt struct{ lat, lon float64 }

type collection struct {
	Features []struct {
		Geometry struct {
			Coordinates [][][2]float64 `json:"coordinates"`
		} `json:"geometry"`
	} `json:"features"`
}

func haversineNm(a, b pt) float64 {
	p1, p2 := a.lat*math.Pi/180, b.lat*math.Pi/180
	dp := p2 - p1
	dl := (b.lon - a.lon) * math.Pi / 180
	h := math.Sin(dp/2)*math.Sin(dp/2) + math.Cos(p1)*math.Cos(p2)*math.Sin(dl/2)*math.Sin(dl/2)
	return 2 * earthRadiusNm * math.Asin(math.Min(1, math.Sqrt(h)))
}

// distToSquareNm is the great-circle distance from p to the nearest point of
// the square, 0 inside. Each edge is scanned coarsely, then the best sample
// is refined by ternary search, so the result is exact to well under 0.01 nm
// and never overestimates by more than that. The caller adds distTolNm.
func distToSquareNm(p pt, la, lo int) float64 {
	s, w, n, e := float64(la), float64(lo), float64(la+1), float64(lo+1)
	if p.lat >= s && p.lat <= n && p.lon >= w && p.lon <= e {
		return 0
	}
	best := math.MaxFloat64
	edge := func(at func(f float64) pt) {
		d := func(f float64) float64 { return haversineNm(p, at(f)) }
		bf, bd := 0.0, math.MaxFloat64
		for i := 0; i <= edgeSamples; i++ {
			f := float64(i) / edgeSamples
			if v := d(f); v < bd {
				bf, bd = f, v
			}
		}
		lo, hi := math.Max(0, bf-1.0/edgeSamples), math.Min(1, bf+1.0/edgeSamples)
		for k := 0; k < 40; k++ {
			m1, m2 := lo+(hi-lo)/3, hi-(hi-lo)/3
			if d(m1) < d(m2) {
				hi = m2
			} else {
				lo = m1
			}
		}
		if v := d((lo + hi) / 2); v < bd {
			bd = v
		}
		if bd < best {
			best = bd
		}
	}
	edge(func(f float64) pt { return pt{s, w + f} })
	edge(func(f float64) pt { return pt{n, w + f} })
	edge(func(f float64) pt { return pt{s + f, w} })
	edge(func(f float64) pt { return pt{s + f, e} })
	return best
}

func sqIndex(la, lo int) int { return (la+90)*360 + (lo + 180) }

func inRing(ring [][2]float64, lat, lon float64) bool {
	in := false
	for i, j := 0, len(ring)-1; i < len(ring); j, i = i, i+1 {
		xi, yi, xj, yj := ring[i][0], ring[i][1], ring[j][0], ring[j][1]
		if (yi > lat) != (yj > lat) && lon < (xj-xi)*(lat-yi)/(yj-yi)+xi {
			in = !in
		}
	}
	return in
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: inlandmask <gshhg_coastline_l.json> <out.bin>")
		os.Exit(2)
	}
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var fc collection
	if err := json.Unmarshal(raw, &fc); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Densified edge points, bucketed by square.
	buckets := make(map[int][]pt)
	add := func(p pt) {
		la, lo := int(math.Floor(p.lat)), int(math.Floor(p.lon))
		if lo >= 180 {
			lo = -180
		}
		if la > 89 {
			la = 89
		}
		if la < -90 {
			la = -90
		}
		buckets[sqIndex(la, lo)] = append(buckets[sqIndex(la, lo)], p)
	}
	type bbox struct{ s, n, w, e float64 }
	var rings [][][2]float64
	var boxes []bbox
	for _, f := range fc.Features {
		for _, ring := range f.Geometry.Coordinates {
			b := bbox{90, -90, 180, -180}
			for i, c := range ring {
				b.s, b.n = math.Min(b.s, c[1]), math.Max(b.n, c[1])
				b.w, b.e = math.Min(b.w, c[0]), math.Max(b.e, c[0])
				a := pt{c[1], c[0]}
				add(a)
				if i == 0 {
					continue
				}
				p := ring[i-1]
				prev := pt{p[1], p[0]}
				steps := int(haversineNm(prev, a) / densifyNm)
				for k := 1; k <= steps; k++ {
					f := float64(k) / float64(steps+1)
					add(pt{prev.lat + (a.lat-prev.lat)*f, prev.lon + (a.lon-prev.lon)*f})
				}
			}
			rings = append(rings, ring)
			boxes = append(boxes, b)
		}
	}

	onLand := func(lat, lon float64) bool {
		in := false
		for i, r := range rings {
			b := boxes[i]
			if lat < b.s || lat > b.n || lon < b.w || lon > b.e {
				continue
			}
			if inRing(r, lat, lon) {
				in = !in
			}
		}
		return in
	}

	// Squares forced to "not inland": the Caspian Sea.
	caspian := func(la, lo int) bool { return la >= 36 && la <= 47 && lo >= 46 && lo <= 54 }

	mask := make([]byte, 360*180/8)
	count := 0
	for la := -90; la < 90; la++ {
		for lo := -180; lo < 180; lo++ {
			if caspian(la, lo) || !onLand(float64(la)+0.5, float64(lo)+0.5) {
				continue
			}
			// Neighbourhood of buckets that can hold a point within the margin.
			dLat := inlandMarginNm/60 + 1
			worst := math.Max(math.Abs(float64(la)), math.Abs(float64(la+1))) + dLat
			cosw := math.Cos(math.Min(worst, 89.5) * math.Pi / 180)
			dLon := int(math.Ceil(dLat / cosw))
			all := dLon >= 180
			near := false
		search:
			for bl := la - 1; bl <= la+1; bl++ {
				if bl < -90 || bl > 89 {
					continue
				}
				lonFrom, lonTo := lo-dLon, lo+dLon
				if all {
					lonFrom, lonTo = -180, 179
				}
				for bo := lonFrom; bo <= lonTo; bo++ {
					w := ((bo+180)%360+360)%360 - 180
					for _, p := range buckets[sqIndex(bl, w)] {
						if distToSquareNm(p, la, lo) <= inlandMarginNm+distTolNm {
							near = true
							break search
						}
					}
				}
			}
			if near {
				continue
			}
			i := sqIndex(la, lo)
			mask[i/8] |= 1 << (i % 8)
			count++
		}
	}
	if err := os.WriteFile(os.Args[2], mask, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("%d inland squares of %d written to %s\n", count, 360*180, os.Args[2])
}
