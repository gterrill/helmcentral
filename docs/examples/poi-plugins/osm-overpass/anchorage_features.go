package main

// anchorage_features.go: the anchorage_features export. Given a position, it
// reports what OpenStreetMap records within one nautical mile that bears on
// anchoring there: restricted areas, public moorings, charted anchorage areas,
// seabed areas and the bay. One Overpass request, no marina to find first.
//
// Nothing here fills a gap. A mooring whose description does not give a vessel
// size says so; an empty list means nothing is mapped, which the host words as
// "none mapped", never "none". This file has no dependency on the extism PDK,
// like the rest of osm-overpass.go's logic, so `go test` covers it.

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

const (
	// anchorageRadiusM is one nautical mile.
	anchorageRadiusM = 1852
	// The caps are per class, asked of Overpass separately so a field of public
	// moorings cannot crowd out a restricted area. Reaching one sets Truncated.
	anchorageRestrictedCap = 50
	anchorageAreaCap       = 50
	anchorageMooringCap    = 200
	anchorageBayCap        = 20
	anchorageProtectedCap  = 20
	// anchorageShapesPerFeature bounds the shapes kept for one area.
	anchorageShapesPerFeature = 20
)

// buildAnchorageFeaturesQuery asks for each class in its own capped set.
// Restricted areas, anchorage and seabed areas and bays come with geometry
// (the card draws them; a bay's is only used to see whether it contains the
// position). Moorings and protected areas come as a centre: a mooring is a
// point, and a marine park's boundary can be hundreds of kilometres long.
// It asks for tags and geometry only: the boat's local Overpass mirror has no
// meta files, and "out meta" there is answered with a runtime error.
func buildAnchorageFeaturesQuery(lat, lon float64) string {
	around := fmt.Sprintf("around:%d,%.6f,%.6f", anchorageRadiusM, lat, lon)
	var b strings.Builder
	b.WriteString("[out:json][timeout:20];")
	fmt.Fprintf(&b, `nwr(%s)["seamark:type"="restricted_area"];out tags geom %d;`, around, anchorageRestrictedCap)
	fmt.Fprintf(&b, `(nwr(%s)["seamark:type"~"^(anchorage|seabed_area)$"];nwr(%s)["anchorage"="yes"];);out tags geom %d;`, around, around, anchorageAreaCap)
	fmt.Fprintf(&b, `(nwr(%s)["seamark:type"="mooring"];nwr(%s)["seamark:type"="buoy_special_purpose"]["seamark:buoy_special_purpose:category"="mooring"];);out tags center %d;`, around, around, anchorageMooringCap)
	fmt.Fprintf(&b, `nwr(%s)["natural"="bay"];out tags geom %d;`, around, anchorageBayCap)
	fmt.Fprintf(&b, `nwr(%s)["boundary"~"^(protected_area|marine_protected_area)$"]["maritime"="yes"];out tags center %d;`, around, anchorageProtectedCap)
	return b.String()
}

// ── output ───────────────────────────────────────────────────────────────

type anchorageFeaturesInput struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// anchorageRestricted is a seamark restricted area, or a protected area. Source
// says which: "seamark" or "protected_area". A protected area carries no
// geometry and no distance (its centre says nothing about how close the
// boundary is), and is not claimed to forbid anchoring: its Restriction is the
// protection title as mapped ("Marine Park"), and the rules differ by zone.
type anchorageRestricted struct {
	OSM         string        `json:"osm"`
	Name        string        `json:"name"`
	Source      string        `json:"source"`
	Restriction string        `json:"restriction"`
	Category    string        `json:"category,omitempty"`
	DistanceM   *int          `json:"distance_m"`
	Geometry    []marinaShape `json:"geometry"`
}

type anchorageMooring struct {
	OSM  string `json:"osm"`
	Name string `json:"name"`
	Ref  string `json:"ref"`
	// VesselSizeM and Class are read from the Queensland public mooring
	// description. The host works out for itself that neither was read.
	VesselSizeM *float64 `json:"vessel_size_m"`
	Class       string   `json:"class"`
	Lat         float64  `json:"lat"`
	Lon         float64  `json:"lon"`
	DistanceM   int      `json:"distance_m"`
}

type anchorageArea struct {
	OSM       string        `json:"osm"`
	Name      string        `json:"name"`
	Category  string        `json:"category"`
	DistanceM int           `json:"distance_m"`
	Geometry  []marinaShape `json:"geometry"`
}

type anchorageSeabed struct {
	OSM       string        `json:"osm"`
	Name      string        `json:"name"`
	Surface   string        `json:"surface"`
	DistanceM int           `json:"distance_m"`
	Geometry  []marinaShape `json:"geometry"`
}

// anchorageBay is the natural=bay that contains the position, or failing that
// the nearest one. Contains is true only when a mapped outline holds the
// position; a bay mapped as a single point never contains anything.
type anchorageBay struct {
	OSM       string `json:"osm"`
	Name      string `json:"name"`
	Contains  bool   `json:"contains"`
	DistanceM int    `json:"distance_m"`
}

type anchorageFeaturesOutput struct {
	RestrictedAreas []anchorageRestricted `json:"restricted_areas"`
	Moorings        []anchorageMooring    `json:"moorings"`
	AnchorageAreas  []anchorageArea       `json:"anchorage_areas"`
	SeabedAreas     []anchorageSeabed     `json:"seabed_areas"`
	Bay             *anchorageBay         `json:"bay"`
	Truncated       bool                  `json:"truncated,omitempty"`
	// DataAsOf is Overpass's timestamp_osm_base (RFC 3339), empty when the
	// answer carries none. It is never filled from the clock.
	DataAsOf string `json:"data_as_of,omitempty"`
}

// ── mooring descriptions ─────────────────────────────────────────────────

// parseMooringDescription reads the two lines of a Queensland public mooring's
// free-text description that matter to a skipper, "VesselSize 25" and
// "Mooring_cl C". Every other line (FID, Pt_No, coordinates, line spec) is
// ignored. Neither line, or a VesselSize that is not a positive number, leaves
// that value unset.
func parseMooringDescription(desc string) (sizeM *float64, class string) {
	for _, line := range strings.Split(desc, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "VesselSize":
			if n, err := strconv.ParseFloat(value, 64); err == nil && n > 0 && !math.IsInf(n, 0) {
				sizeM = &n
			}
		case "Mooring_cl":
			class = value
		}
	}
	return sizeM, class
}

// ── classification ───────────────────────────────────────────────────────

func isMooringElement(tags map[string]string) bool {
	return tags["seamark:type"] == "mooring" ||
		(tags["seamark:type"] == "buoy_special_purpose" && tags["seamark:buoy_special_purpose:category"] == "mooring")
}

// isProtectedArea is a marine protected area: boundary=protected_area (or
// marine_protected_area) tagged maritime=yes. The land national parks that
// line a shore are not listed, since they say nothing about anchoring.
func isProtectedArea(tags map[string]string) bool {
	return (tags["boundary"] == "protected_area" || tags["boundary"] == "marine_protected_area") && tags["maritime"] == "yes"
}

func isAnchorageArea(tags map[string]string) bool {
	return tags["seamark:type"] == "anchorage" || tags["anchorage"] == "yes"
}

// distanceTo is the distance to the nearest point of an element's shapes (or
// its position or centre when it has none), or 0 when a closed outline holds
// the position. contains says whether one does. ok is false for an element
// with nothing to measure from.
func distanceTo(el overpassElement, shapes []marinaShape, lat, lon float64) (dist int, contains, ok bool) {
	best := math.MaxFloat64
	found := false
	var outers, inners [][][2]float64
	if len(el.Members) > 0 && el.Lat == nil && len(el.Geometry) == 0 {
		outers, inners = relationRings(el.Members)
	} else {
		for _, sh := range shapes {
			if sh.Kind == "polygon" {
				outers = append(outers, sh.Points)
			}
		}
	}
	held := false
	for _, r := range outers {
		if pointInRing(r, lat, lon) {
			held = true
		}
	}
	for _, r := range inners {
		if pointInRing(r, lat, lon) {
			held = false
		}
	}
	if held {
		return 0, true, true
	}
	for _, sh := range shapes {
		if len(sh.Points) == 1 {
			best, found = math.Min(best, haversineMeters(lat, lon, sh.Points[0][0], sh.Points[0][1])), true
		}
		for i := 1; i < len(sh.Points); i++ {
			best, found = math.Min(best, segmentMeters(lat, lon, sh.Points[i-1], sh.Points[i])), true
		}
	}
	if !found {
		elLat, elLon, has := elementLatLon(el)
		if !has {
			return 0, false, false
		}
		best = haversineMeters(lat, lon, elLat, elLon)
	}
	return int(math.Round(best)), false, true
}

// segmentMeters is the distance from (lat, lon) to the segment a-b, on a local
// equirectangular plane centred on the position: exact enough within the one
// nautical mile this export looks at.
func segmentMeters(lat, lon float64, a, b [2]float64) float64 {
	const mPerDeg = 111194.9
	cos := math.Cos(lat * math.Pi / 180)
	ax, ay := (a[1]-lon)*mPerDeg*cos, (a[0]-lat)*mPerDeg
	bx, by := (b[1]-lon)*mPerDeg*cos, (b[0]-lat)*mPerDeg
	dx, dy := bx-ax, by-ay
	t := 0.0
	if l2 := dx*dx + dy*dy; l2 > 0 {
		t = math.Max(0, math.Min(1, -(ax*dx+ay*dy)/l2))
	}
	return math.Hypot(ax+t*dx, ay+t*dy)
}

// relationRings joins a relation's member ways end to end (either direction)
// into closed rings, split by role. Ways that do not close into a ring are
// left out of containment (they are still drawn and measured). A member with
// no role counts as outer.
func relationRings(members []overpassMember) (outers, inners [][][2]float64) {
	for _, inner := range []bool{true, false} {
		var ways [][][2]float64
		for _, m := range members {
			if (m.Role == "inner") != inner {
				continue
			}
			if pts := geomPoints(m.Geometry); len(pts) >= 2 {
				ways = append(ways, pts)
			}
		}
		if inner {
			inners = joinRings(ways)
		} else {
			outers = joinRings(ways)
		}
	}
	return outers, inners
}

func joinRings(ways [][][2]float64) [][][2]float64 {
	var rings [][][2]float64
	used := make([]bool, len(ways))
	for i := range ways {
		if used[i] {
			continue
		}
		used[i] = true
		ring := append([][2]float64(nil), ways[i]...)
		for ring[0] != ring[len(ring)-1] {
			joined := false
			for j, w := range ways {
				if used[j] {
					continue
				}
				end := ring[len(ring)-1]
				if w[0] == end {
					ring = append(ring, w[1:]...)
				} else if w[len(w)-1] == end {
					for k := len(w) - 2; k >= 0; k-- {
						ring = append(ring, w[k])
					}
				} else {
					continue
				}
				used[j], joined = true, true
				break
			}
			if !joined {
				break
			}
		}
		if len(ring) >= 4 && ring[0] == ring[len(ring)-1] {
			rings = append(rings, ring)
		}
	}
	return rings
}

// pointInRing is the even-odd test on [lat, lon] pairs. Over an area as small
// as a bay a plane is exact enough.
func pointInRing(ring [][2]float64, lat, lon float64) bool {
	in := false
	for i, j := 0, len(ring)-1; i < len(ring); j, i = i, i+1 {
		a, b := ring[i], ring[j]
		if (a[0] > lat) != (b[0] > lat) && lon < (b[1]-a[1])*(lat-a[0])/(b[0]-a[0])+a[1] {
			in = !in
		}
	}
	return in
}

func osmRef(el overpassElement) string { return fmt.Sprintf("%s/%d", el.Type, el.ID) }

func capShapes(shapes []marinaShape) []marinaShape {
	if len(shapes) > anchorageShapesPerFeature {
		shapes = shapes[:anchorageShapesPerFeature]
	}
	if shapes == nil {
		shapes = []marinaShape{}
	}
	return shapes
}

func areaName(tags map[string]string) string {
	return firstTag(tags, "name", "seamark:name")
}

// classifyAnchorage sorts the elements into the classes, nearest first. The
// same element can be returned by two sets, so each is counted once.
func classifyAnchorage(elements []overpassElement, lat, lon float64) anchorageFeaturesOutput {
	out := anchorageFeaturesOutput{
		RestrictedAreas: []anchorageRestricted{},
		Moorings:        []anchorageMooring{},
		AnchorageAreas:  []anchorageArea{},
		SeabedAreas:     []anchorageSeabed{},
	}
	seen := map[string]bool{}
	var bays []anchorageBay
	var nRestricted, nProtected, nMooring, nArea, nBay int
	for _, el := range elements {
		ref := osmRef(el)
		if seen[ref] {
			continue
		}
		seen[ref] = true
		tags := el.Tags
		shapes := elementShapes(el)
		dist, contains, ok := distanceTo(el, shapes, lat, lon)
		if !ok {
			continue
		}
		switch {
		case tags["seamark:type"] == "restricted_area":
			nRestricted++
			out.RestrictedAreas = append(out.RestrictedAreas, anchorageRestricted{
				OSM: ref, Name: areaName(tags), Source: "seamark",
				Restriction: strings.TrimSpace(tags["seamark:restricted_area:restriction"]),
				Category:    strings.TrimSpace(tags["seamark:restricted_area:category"]),
				DistanceM:   &dist, Geometry: capShapes(shapes),
			})
		case isProtectedArea(tags):
			nProtected++
			out.RestrictedAreas = append(out.RestrictedAreas, anchorageRestricted{
				OSM: ref, Name: areaName(tags), Source: "protected_area",
				Restriction: firstTag(tags, "protection_title"),
				// protect_class is an IUCN code list such as "1;2;4;6"; it names
				// nothing an operator can read, so it is not passed on.
				DistanceM:   nil, Geometry: []marinaShape{},
			})
		case isMooringElement(tags):
			nMooring++
			elLat, elLon, _ := elementLatLon(el)
			size, class := parseMooringDescription(tags["description"])
			out.Moorings = append(out.Moorings, anchorageMooring{
				OSM: ref, Name: strings.TrimSpace(tags["name"]), Ref: strings.TrimSpace(tags["ref"]),
				VesselSizeM: size, Class: class,
				Lat: roundCoord(elLat), Lon: roundCoord(elLon), DistanceM: dist,
			})
		case tags["seamark:type"] == "seabed_area":
			nArea++
			out.SeabedAreas = append(out.SeabedAreas, anchorageSeabed{
				OSM: ref, Name: areaName(tags), Surface: strings.TrimSpace(tags["seamark:seabed_area:surface"]),
				DistanceM: dist, Geometry: capShapes(shapes),
			})
		case isAnchorageArea(tags):
			nArea++
			out.AnchorageAreas = append(out.AnchorageAreas, anchorageArea{
				OSM: ref, Name: areaName(tags), Category: strings.TrimSpace(tags["seamark:anchorage:category"]),
				DistanceM: dist, Geometry: capShapes(shapes),
			})
		case tags["natural"] == "bay":
			nBay++
			if name := areaName(tags); name != "" {
				bays = append(bays, anchorageBay{OSM: ref, Name: name, Contains: contains, DistanceM: dist})
			}
		}
	}
	sort.SliceStable(out.RestrictedAreas, func(i, j int) bool {
		// A protected area has no measured distance and follows the seamark areas.
		a, b := out.RestrictedAreas[i].DistanceM, out.RestrictedAreas[j].DistanceM
		if (a == nil) != (b == nil) {
			return a != nil
		}
		return a != nil && *a < *b
	})
	sort.SliceStable(out.Moorings, func(i, j int) bool { return out.Moorings[i].DistanceM < out.Moorings[j].DistanceM })
	sort.SliceStable(out.AnchorageAreas, func(i, j int) bool { return out.AnchorageAreas[i].DistanceM < out.AnchorageAreas[j].DistanceM })
	sort.SliceStable(out.SeabedAreas, func(i, j int) bool { return out.SeabedAreas[i].DistanceM < out.SeabedAreas[j].DistanceM })

	// A bay that holds the position beats a nearer one that does not.
	sort.SliceStable(bays, func(i, j int) bool {
		if bays[i].Contains != bays[j].Contains {
			return bays[i].Contains
		}
		return bays[i].DistanceM < bays[j].DistanceM
	})
	if len(bays) > 0 {
		out.Bay = &bays[0]
	}
	out.Truncated = nRestricted >= anchorageRestrictedCap || nProtected >= anchorageProtectedCap ||
		nMooring >= anchorageMooringCap || nArea >= anchorageAreaCap || nBay >= anchorageBayCap
	return out
}

// runAnchorageFeatures is the export's entry point. A failing query is returned
// as it came; a position with nothing mapped is a valid, empty answer.
func runAnchorageFeatures(post overpassResponseFunc, input anchorageFeaturesInput) (anchorageFeaturesOutput, error) {
	res, err := post(buildAnchorageFeaturesQuery(input.Lat, input.Lon))
	if err != nil {
		return anchorageFeaturesOutput{}, err
	}
	out := classifyAnchorage(res.Elements, input.Lat, input.Lon)
	out.DataAsOf = strings.TrimSpace(res.Osm3s.TimestampOsmBase)
	return out, nil
}
