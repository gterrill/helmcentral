package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// ── anchorage shelter estimate (ADR 0203) ────────────────────────────────
//
// Shelter is estimated from charted shoreline alone: for every compass
// bearing a ray is cast from the anchor over the OpenStreetMap coastline and
// reef lines held in the offline coastline (coastline_store.go), and the distance to the first
// obstacle is the fetch - how far wind blowing from that bearing has run
// over water before it reaches the boat. Everything here is pure geometry
// over already-downloaded lines (ADR 0091: the provider returns raw features,
// the host computes). Wind direction is always the direction the wind comes
// FROM, so a ray cast towards 090 grades the east wind.

const (
	shelterBearingStepDeg = 10
	shelterBearingCount   = 360 / shelterBearingStepDeg

	// shelterMaxFetchNm is how far a ray looks. A ray that finds nothing is
	// open water: wind and sea have more than this to build in.
	shelterMaxFetchNm = 15.0

	// shelterWindowDeg is the half-width of the window that stops a bearing
	// running along a shore reading as sheltered: it counts only if every
	// ray within this many degrees is also short.
	shelterWindowDeg = 30

	// shelterShelteredMaxNm is the longest fetch, anywhere in the window,
	// that still counts as sheltered.
	shelterShelteredMaxNm = 1.5

	// shelterPartialMaxNm is the longest own-bearing fetch that is graded
	// partial when the window is not short enough to be sheltered: the
	// fringe of a shelter, or an island two or three miles off.
	shelterPartialMaxNm = 5.0

	// shelterEarthRadiusM matches the equirectangular projection used for the
	// validation cases in the ADR.
	shelterEarthRadiusM = 6371000.0

	shelterSheltered = "sheltered"
	shelterPartial   = "partial"
	shelterExposed   = "exposed"
	// shelterReef is a bearing whose only obstruction is the edge of the reef
	// the anchor sits inside. It is partial at best and depends on the tide.
	shelterReef = "reef"

	shelterCaveat = "Estimate from charted shoreline only. It does not account for wind bending round " +
		"headlands, gusts funnelling down valleys, or swell wrapping round a point or over a reef, and " +
		"charted coastline can be incomplete or out of date. Compare the sheltered arcs with the forecast " +
		"wind direction for the stay and treat the result as a first look, not a guarantee."
)

type shorelinePoint struct {
	Lat float64
	Lon float64
}

// shorelineLine is one polyline the provider returned: a piece of
// natural=coastline, or a natural=reef way or outer relation member. Closed
// means the provider saw the whole ring (first point == last); only closed
// reef lines are ever treated as polygons. Coastline ways are fragments.
type shorelineLine struct {
	ID     string
	Kind   string // "coastline" or "reef"
	Name   string
	Closed bool
	Points []shorelinePoint
}

type shelterBearing struct {
	BearingDeg  int     `json:"bearing_deg"`
	FetchNm     float64 `json:"fetch_nm"`
	Grade       string  `json:"grade"`
	Open        bool    `json:"open,omitempty"`
	Blocker     string  `json:"blocker"`
	ReefWidthNm float64 `json:"reef_width_nm,omitempty"`
}

type shelterArc struct {
	Grade    string `json:"grade"`
	FromDeg  int    `json:"from_deg"`
	ToDeg    int    `json:"to_deg"`
	FromName string `json:"from"`
	ToName   string `json:"to"`
	Text     string `json:"text"`
}

type shelterEstimate struct {
	Summary    string           `json:"summary"`
	InsideReef bool             `json:"inside_reef,omitempty"`
	ReefID     string           `json:"reef_id,omitempty"`
	ReefName   string           `json:"reef_name,omitempty"`
	Arcs       []shelterArc     `json:"arcs"`
	Bearings   []shelterBearing `json:"bearings"`
	Caveat     string           `json:"caveat"`
}

type shelterSegment struct {
	x1, y1, x2, y2 float64
	line           int
}

func estimateShelter(lat, lon float64, lines []shorelineLine) shelterEstimate {
	kx := math.Cos(lat*math.Pi/180) * math.Pi / 180 * shelterEarthRadiusM
	ky := math.Pi / 180 * shelterEarthRadiusM
	project := func(p shorelinePoint) (float64, float64) {
		return (p.Lon - lon) * kx, (p.Lat - lat) * ky
	}

	var segs []shelterSegment
	rings := map[int][][2]float64{}
	for i, line := range lines {
		pts := make([][2]float64, len(line.Points))
		for j, p := range line.Points {
			x, y := project(p)
			pts[j] = [2]float64{x, y}
		}
		for j := 1; j < len(pts); j++ {
			segs = append(segs, shelterSegment{pts[j-1][0], pts[j-1][1], pts[j][0], pts[j][1], i})
		}
		if line.Kind == "reef" && line.Closed && len(pts) >= 4 {
			rings[i] = pts
		}
	}

	host := hostReefRing(rings)

	maxM := shelterMaxFetchNm * metersPerNauticalMile
	fetch := make([]float64, shelterBearingCount)
	hit := make([]int, shelterBearingCount) // line index, -1 = open
	for k := range fetch {
		fetch[k], hit[k] = castShelterRay(segs, float64(k*shelterBearingStepDeg), maxM)
	}

	est := shelterEstimate{Caveat: shelterCaveat}
	if host >= 0 {
		est.InsideReef = true
		est.ReefID = lines[host].ID
		est.ReefName = lines[host].Name
	}

	windowSteps := shelterWindowDeg / shelterBearingStepDeg
	for k := 0; k < shelterBearingCount; k++ {
		nm := fetch[k] / metersPerNauticalMile
		b := shelterBearing{BearingDeg: k * shelterBearingStepDeg, FetchNm: math.Round(nm*100) / 100}
		switch {
		case hit[k] < 0:
			b.Open = true
			b.FetchNm = shelterMaxFetchNm
			b.Blocker = fmt.Sprintf("open water beyond %g nm", shelterMaxFetchNm)
			b.Grade = shelterExposed
		case hit[k] == host && host >= 0:
			b.Grade = shelterReef
			b.ReefWidthNm = b.FetchNm
			b.Blocker = "edge of the reef you are inside"
			if n := lines[host].Name; n != "" {
				b.Blocker = "edge of " + n
			}
		default:
			b.Blocker = blockerLabel(lines[hit[k]])
		}
		if b.Grade == "" {
			windowMax := 0.0
			for d := -windowSteps; d <= windowSteps; d++ {
				idx := ((k+d)%shelterBearingCount + shelterBearingCount) % shelterBearingCount
				windowMax = math.Max(windowMax, fetch[idx]/metersPerNauticalMile)
			}
			switch {
			case windowMax <= shelterShelteredMaxNm:
				b.Grade = shelterSheltered
			case nm <= shelterPartialMaxNm:
				b.Grade = shelterPartial
			default:
				b.Grade = shelterExposed
			}
		}
		est.Bearings = append(est.Bearings, b)
	}

	est.Arcs = shelterArcs(est.Bearings)
	est.Summary = shelterSummary(est)
	return est
}

func blockerLabel(l shorelineLine) string {
	if l.Name != "" {
		return l.Name
	}
	if l.Kind == "reef" {
		return "reef"
	}
	return "coastline"
}

// hostReefRing returns the index of the smallest closed reef ring that
// contains the anchor (the projection origin), or -1. Containment is a
// ray-crossing parity test and is only meaningful on closed rings, which is
// why open coastline fragments never reach it.
func hostReefRing(rings map[int][][2]float64) int {
	best, bestArea := -1, math.Inf(1)
	for idx, pts := range rings {
		crossings := 0
		for j := 1; j < len(pts); j++ {
			x1, y1, x2, y2 := pts[j-1][0], pts[j-1][1], pts[j][0], pts[j][1]
			if (y1 > 0) == (y2 > 0) {
				continue
			}
			if x1+(0-y1)*(x2-x1)/(y2-y1) > 0 {
				crossings++
			}
		}
		if crossings%2 == 0 {
			continue
		}
		area := 0.0
		for j := 1; j < len(pts); j++ {
			area += pts[j-1][0]*pts[j][1] - pts[j][0]*pts[j-1][1]
		}
		area = math.Abs(area) / 2
		if area < bestArea || (area == bestArea && idx < best) {
			best, bestArea = idx, area
		}
	}
	return best
}

// castShelterRay returns the distance in metres to the first segment hit
// along bearingDeg (clockwise from north) and that segment's line index, or
// (maxM, -1) when nothing lies within maxM. Hits within a metre of the
// anchor are ignored.
func castShelterRay(segs []shelterSegment, bearingDeg, maxM float64) (float64, int) {
	ux, uy := math.Sin(bearingDeg*math.Pi/180), math.Cos(bearingDeg*math.Pi/180)
	best, who := maxM, -1
	for _, s := range segs {
		ex, ey := s.x2-s.x1, s.y2-s.y1
		den := ux*ey - uy*ex
		if math.Abs(den) < 1e-12 {
			continue
		}
		t := (s.x1*ey - s.y1*ex) / den
		u := (s.x1*uy - s.y1*ux) / den
		if u >= 0 && u <= 1 && t > 1 && t < best {
			best, who = t, s.line
		}
	}
	return best, who
}

// shelterArcs folds the per-bearing grades into contiguous compass arcs,
// treating the circle as cyclic so an arc can run through north.
func shelterArcs(bearings []shelterBearing) []shelterArc {
	n := len(bearings)
	start := -1
	for i := 0; i < n; i++ {
		if bearings[i].Grade != bearings[(i+n-1)%n].Grade {
			start = i
			break
		}
	}
	var arcs []shelterArc
	if start < 0 {
		arcs = append(arcs, newShelterArc(bearings[0].Grade, bearings[0].BearingDeg, bearings[n-1].BearingDeg, true))
		return arcs
	}
	runFrom := start
	for off := 1; off <= n; off++ {
		i := (start + off) % n
		prev := (start + off - 1) % n
		if off == n || bearings[i].Grade != bearings[prev].Grade {
			arcs = append(arcs, newShelterArc(bearings[runFrom%n].Grade, bearings[runFrom%n].BearingDeg, bearings[prev].BearingDeg, false))
			runFrom = start + off
		}
	}
	sort.Slice(arcs, func(a, b int) bool { return arcs[a].FromDeg < arcs[b].FromDeg })
	return arcs
}

func newShelterArc(grade string, from, to int, allRound bool) shelterArc {
	a := shelterArc{Grade: grade, FromDeg: from, ToDeg: to,
		FromName: degreesToDirection(float64(from)), ToName: degreesToDirection(float64(to))}
	switch {
	case allRound:
		a.Text = "all round"
	case from == to:
		a.Text = fmt.Sprintf("%s (%03d°)", a.FromName, from)
	default:
		a.Text = fmt.Sprintf("%s through %s (%03d°–%03d°)", a.FromName, a.ToName, from, to)
	}
	return a
}

func shelterSummary(est shelterEstimate) string {
	var parts []string
	if est.InsideReef {
		who := "a reef"
		if est.ReefName != "" {
			who = est.ReefName
		}
		lo, hi := est.Bearings[0], est.Bearings[0]
		for _, b := range est.Bearings {
			if b.ReefWidthNm < lo.ReefWidthNm {
				lo = b
			}
			if b.ReefWidthNm > hi.ReefWidthNm {
				hi = b
			}
		}
		parts = append(parts, fmt.Sprintf("The anchor is inside %s. Reef width upwind runs from %.1f nm (wind from %s, %03d°) to %.1f nm (wind from %s, %03d°). "+
			"Reef shelter depends on the tide: it breaks the sea best near low water and gives less as the reef covers",
			who, lo.ReefWidthNm, degreesToDirection(float64(lo.BearingDeg)), lo.BearingDeg,
			hi.ReefWidthNm, degreesToDirection(float64(hi.BearingDeg)), hi.BearingDeg))
	}
	lead := map[string]string{
		shelterSheltered: "Sheltered from",
		shelterReef:      "Behind the reef from",
		shelterPartial:   "Partly sheltered from",
		shelterExposed:   "Exposed to",
	}
	for _, grade := range []string{shelterSheltered, shelterReef, shelterPartial, shelterExposed} {
		var texts []string
		for _, a := range est.Arcs {
			if a.Grade == grade {
				texts = append(texts, a.Text)
			}
		}
		if len(texts) == 0 {
			continue
		}
		words := strings.Fields(lead[grade])
		joiner := " and " + words[len(words)-1] + " "
		if len(texts) == 1 && texts[0] == "all round" {
			parts = append(parts, strings.Join(words[:len(words)-1], " ")+" all round")
			continue
		}
		parts = append(parts, lead[grade]+" "+strings.Join(texts, joiner))
	}
	return strings.Join(parts, ". ") + ". Directions are where the wind comes from."
}
