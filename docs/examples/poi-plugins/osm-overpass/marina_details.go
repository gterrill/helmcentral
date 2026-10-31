package main

// marina_details.go: the marina_details export. Given a position, it finds the
// marina or harbour charted there and reports what OpenStreetMap records about
// it and its surroundings.
//
// A marina's own element usually carries little more than a name, a berth count
// and whether power is laid on. The useful detail (fuel, a travel lift, the
// ramp, the pontoon arms, the entrance marks) is mapped as separate features
// beside it, so this makes two queries: one to find the marina, and one for
// everything within reach of that element. This file has no dependency on the
// extism PDK, like the rest of osm-overpass.go's logic, so `go test` covers it.

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

const (
	// marinaResolveRadiusM is how far from the position a marina may be
	// charted and still be the one meant.
	marinaResolveRadiusM = 500
	// marinaFacilityRadiusM is how far from the marina element its
	// facilities, pontoons and entrance marks are gathered.
	marinaFacilityRadiusM = 300
	// marinaAshoreRadiusM is how far shops and services ashore are gathered.
	marinaAshoreRadiusM = 1000
	// marinaDetailsElementCap bounds the marine features in the details
	// answer, and marinaAshoreElementCap the services ashore, which are asked
	// for apart so they do not crowd the marine features out. Reaching either
	// sets Truncated so the host can say the list may be incomplete.
	marinaDetailsElementCap = 400
	marinaAshoreElementCap  = 100
	// marinaAshorePerKind is how many of each kind of service ashore are kept,
	// nearest first.
	marinaAshorePerKind = 3
)

// ── queries ──────────────────────────────────────────────────────────────

// buildMarinaResolveQuery finds every marina and harbour element within
// marinaResolveRadiusM of the position. It asks for tags and centre only: the
// boat's local Overpass mirror has no meta files, and "out meta" there is
// answered with a runtime error.
func buildMarinaResolveQuery(lat, lon float64) string {
	around := fmt.Sprintf("around:%d,%.6f,%.6f", marinaResolveRadiusM, lat, lon)
	return fmt.Sprintf(`[out:json][timeout:8];(nwr["leisure"="marina"](%s);nwr["seamark:type"="harbour"](%s););out tags center;`, around, around)
}

// buildMarinaDetailsQuery gathers the marina element itself, the marine
// features within marinaFacilityRadiusM of it (with geometry, for the map) and
// the named shops and services within marinaAshoreRadiusM (centre only, since
// only a position and a distance are wanted). Hotels, restaurants and other
// tourism are left out on purpose.
func buildMarinaDetailsQuery(elementType string, id int64) (string, error) {
	switch elementType {
	case "node", "way", "relation":
	default:
		return "", fmt.Errorf("marina_details: cannot query around a %q element", elementType)
	}
	near := fmt.Sprintf("(around.m:%d)", marinaFacilityRadiusM)
	far := fmt.Sprintf("(around.m:%d)", marinaAshoreRadiusM)

	var b strings.Builder
	fmt.Fprintf(&b, "[out:json][timeout:12];%s(%d)->.m;(", elementType, id)
	for _, clause := range []string{
		`["waterway"="fuel"]`,
		`["amenity"~"^(toilets|shower|drinking_water|water_point|sanitary_dump_station)$"]`,
		`["man_made"~"^(crane|pier|breakwater)$"]`,
		`["leisure"="slipway"]`,
		`["seamark:type"~"^(small_craft_facility|beacon_lateral|buoy_lateral)$"]`,
		`["shop"~"^(boat|ship_chandler)$"]`,
	} {
		fmt.Fprintf(&b, "nwr%s%s;", near, clause)
	}
	b.WriteString(")->.f;(")
	for _, key := range []string{"shop", "amenity"} {
		fmt.Fprintf(&b, `nwr%s["%s"~"^(%s)$"]["name"];`, far, key, strings.Join(ashoreKindsOf(key), "|"))
	}
	// An element that is both a marine feature and a service ashore is output once.
	fmt.Fprintf(&b, ")->.a;(.a; - .f;)->.a;(.m;.f;);out tags geom %d;.a out tags center %d;", marinaDetailsElementCap, marinaAshoreElementCap)
	return b.String(), nil
}

// ── geometry ─────────────────────────────────────────────────────────────

const (
	// marinaShapeMaxPoints bounds one shape. A longer line is thinned by
	// keeping evenly spaced points and always its last.
	marinaShapeMaxPoints = 300
	// marinaShapesPerFeature bounds the shapes kept for one facility or mark.
	marinaShapesPerFeature = 50
)

// marinaShape is a point, a line or a closed polygon, as [lat, lon] pairs. The
// host keeps it for drawing a map; the model never sees it.
type marinaShape struct {
	Kind   string       `json:"kind"`
	Points [][2]float64 `json:"points"`
}

func roundCoord(v float64) float64 { return math.Round(v*1e6) / 1e6 }

func shapeFromPoints(pts [][2]float64) (marinaShape, bool) {
	if len(pts) == 0 {
		return marinaShape{}, false
	}
	if len(pts) == 1 {
		return marinaShape{Kind: "point", Points: pts}, true
	}
	kind := "line"
	if len(pts) >= 4 && pts[0] == pts[len(pts)-1] {
		kind = "polygon"
	}
	if len(pts) > marinaShapeMaxPoints {
		step := float64(len(pts)-1) / float64(marinaShapeMaxPoints-1)
		thin := make([][2]float64, 0, marinaShapeMaxPoints)
		for i := 0; i < marinaShapeMaxPoints-1; i++ {
			thin = append(thin, pts[int(math.Round(float64(i)*step))])
		}
		pts = append(thin, pts[len(pts)-1])
	}
	return marinaShape{Kind: kind, Points: pts}, true
}

func geomPoints(g []*overpassLatLon) [][2]float64 {
	pts := make([][2]float64, 0, len(g))
	for _, p := range g {
		if p != nil {
			pts = append(pts, [2]float64{roundCoord(p.Lat), roundCoord(p.Lon)})
		}
	}
	return pts
}

// elementShapes returns the shapes an element draws as: a node is a point, a
// way is a line or polygon, and a relation is one shape per member that has a
// geometry.
func elementShapes(el overpassElement) []marinaShape {
	var shapes []marinaShape
	add := func(pts [][2]float64) {
		if sh, ok := shapeFromPoints(pts); ok {
			shapes = append(shapes, sh)
		}
	}
	switch {
	case el.Lat != nil && el.Lon != nil:
		add([][2]float64{{roundCoord(*el.Lat), roundCoord(*el.Lon)}})
	case len(el.Geometry) > 0:
		add(geomPoints(el.Geometry))
	default:
		for _, m := range el.Members {
			add(geomPoints(m.Geometry))
		}
	}
	return shapes
}

// ── output ───────────────────────────────────────────────────────────────

type marinaDetailsInput struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// marinaOut is what the marina's own element and the pontoons around it say.
// Every field the data does not carry is left empty or nil: the host reads an
// empty field as "not recorded", never as "not there".
type marinaOut struct {
	OSM          string   `json:"osm"`
	Name         string   `json:"name"`
	Lat          float64  `json:"lat"`
	Lon          float64  `json:"lon"`
	Berths       *int     `json:"berths"`
	Fee          string   `json:"fee,omitempty"`
	PowerSupply  string   `json:"power_supply,omitempty"`
	Phone        string   `json:"phone,omitempty"`
	Email        string   `json:"email,omitempty"`
	Website      string   `json:"website,omitempty"`
	VHF          string   `json:"vhf,omitempty"`
	OpeningHours string   `json:"opening_hours,omitempty"`
	MaxDraughtM  *float64 `json:"max_draught_m"`
	MaxLengthM   *float64 `json:"max_length_m"`
	DepthM       *float64 `json:"depth_m"`
	// UnreadTags are tags on the marina that were present but could not be
	// understood as a number, as "key=value".
	UnreadTags []string `json:"unread_tags,omitempty"`
	// TaggedAbsent are facilities the marina's own tags say it does not have
	// (for example toilets=no).
	TaggedAbsent []string `json:"tagged_absent,omitempty"`
	PontoonArms  []string `json:"pontoon_arms"`
	// Outline is the marina's own shape; Pontoons are every pier beside it,
	// named or not, private or not, for drawing.
	Outline  []marinaShape   `json:"outline"`
	Pontoons []marinaPontoon `json:"pontoons"`
}

type marinaPontoon struct {
	Name    string      `json:"name,omitempty"`
	Private bool        `json:"private,omitempty"`
	Shape   marinaShape `json:"shape"`
}

type marinaFacilityOut struct {
	Kind  string            `json:"kind"`
	Name  string            `json:"name,omitempty"`
	Count int               `json:"count"`
	Facts map[string]string `json:"facts,omitempty"`
	// Geometry holds one shape per mapped feature counted in Count.
	Geometry []marinaShape `json:"geometry"`
}

type marinaMarkOut struct {
	Kind     string        `json:"kind"`
	Side     string        `json:"side"`
	Colour   string        `json:"colour,omitempty"`
	Name     string        `json:"name,omitempty"`
	Count    int           `json:"count"`
	Geometry []marinaShape `json:"geometry"`
}

type marinaAshoreOut struct {
	Kind         string  `json:"kind"`
	Name         string  `json:"name"`
	OpeningHours string  `json:"opening_hours,omitempty"`
	DistanceM    int     `json:"distance_m"`
	Lat          float64 `json:"lat"`
	Lon          float64 `json:"lon"`
}

type marinaDetailsOutput struct {
	Marina        marinaOut           `json:"marina"`
	Facilities    []marinaFacilityOut `json:"facilities"`
	EntranceMarks []marinaMarkOut     `json:"entrance_marks"`
	Ashore        []marinaAshoreOut   `json:"ashore"`
	Truncated     bool                `json:"truncated,omitempty"`
	// DataAsOf is when the OpenStreetMap data was last updated on the server
	// that answered (Overpass's timestamp_osm_base, RFC 3339), empty when the
	// answer carries none.
	DataAsOf string `json:"data_as_of,omitempty"`
}

// ── resolving the marina ─────────────────────────────────────────────────

func isMarinaElement(tags map[string]string) bool {
	return tags["leisure"] == "marina" || tags["seamark:type"] == "harbour"
}

// pickMarina chooses the marina the position means: a leisure=marina element
// before a bare harbour mark, and the nearest of those.
func pickMarina(elements []overpassElement, lat, lon float64) (overpassElement, bool) {
	var best overpassElement
	bestRank, bestDist, found := 2, math.MaxFloat64, false
	for _, el := range elements {
		if !isMarinaElement(el.Tags) {
			continue
		}
		elLat, elLon, ok := elementLatLon(el)
		if !ok {
			continue
		}
		rank := 1
		if el.Tags["leisure"] == "marina" {
			rank = 0
		}
		dist := haversineMeters(lat, lon, elLat, elLon)
		if !found || rank < bestRank || (rank == bestRank && dist < bestDist) {
			best, bestRank, bestDist, found = el, rank, dist, true
		}
	}
	return best, found
}

// runMarinaDetails is the export's entry point. A failing query is returned as
// it came, and a position with no marina charted is an error, never an empty
// result.
func runMarinaDetails(post overpassResponseFunc, input marinaDetailsInput) (marinaDetailsOutput, error) {
	resolved, err := post(buildMarinaResolveQuery(input.Lat, input.Lon))
	if err != nil {
		return marinaDetailsOutput{}, err
	}
	marina, ok := pickMarina(resolved.Elements, input.Lat, input.Lon)
	if !ok {
		return marinaDetailsOutput{}, fmt.Errorf("no marina charted within %d m of that position", marinaResolveRadiusM)
	}
	query, err := buildMarinaDetailsQuery(marina.Type, marina.ID)
	if err != nil {
		return marinaDetailsOutput{}, err
	}
	details, err := post(query)
	if err != nil {
		return marinaDetailsOutput{}, err
	}
	elements := details.Elements
	lat, lon, _ := elementLatLon(marina)
	out := classifyMarina(marina, elements, lat, lon)
	// The date is the details answer's own, never the clock: the map data the
	// card draws came from that answer. An answer without one leaves it empty.
	out.DataAsOf = strings.TrimSpace(details.Osm3s.TimestampOsmBase)
	var ashore int
	for _, el := range elements {
		if ashoreKind(el.Tags) != "" {
			ashore++
		}
	}
	out.Truncated = len(elements)-ashore >= marinaDetailsElementCap || ashore >= marinaAshoreElementCap
	return out, nil
}

// ── classifying what was found ───────────────────────────────────────────

// factKeys are the tags kept as a facility's facts, besides any fuel:* tag.
var factKeys = map[string]bool{
	"fee": true, "opening_hours": true, "operator": true, "access": true,
	"phone": true, "contact:phone": true, "capacity": true,
	"crane:type": true, "crane:capacity": true, "floating": true,
}

// facilityFacts returns the tags kept as a facility's facts. With fuelOnly it
// keeps just the fuel:* tags, for the fuel a marina's own element declares.
func facilityFacts(tags map[string]string, fuelOnly bool) map[string]string {
	var facts map[string]string
	for k, v := range tags {
		if !strings.HasPrefix(k, "fuel:") && (fuelOnly || !factKeys[k]) {
			continue
		}
		if strings.TrimSpace(v) == "" {
			continue
		}
		if facts == nil {
			facts = map[string]string{}
		}
		facts[k] = v
	}
	return facts
}

// smallCraftKinds maps the S-57 small craft facility categories to the kinds
// Mate reports. A category not listed here is reported as "other".
var smallCraftKinds = map[string]string{
	"boatyard": "boatyard", "fuel_station": "fuel", "water_tap": "water",
	"electricity": "power", "pump-out": "pump_out", "chandler": "chandler",
	"showers": "showers", "laundrette": "laundry", "visitor_berth": "visitor_berth",
	"slipway": "ramp", "boat_hoist": "haul_out", "crane": "crane", "toilets": "toilets",
}

// facilityKinds returns the facility kinds one nearby element stands for, with
// any fact the kind itself implies. An element can stand for several.
func facilityKinds(tags map[string]string) []string {
	var kinds []string
	if tags["waterway"] == "fuel" {
		kinds = append(kinds, "fuel")
	}
	switch tags["man_made"] {
	case "crane":
		if tags["crane:type"] == "travel_lift" {
			kinds = append(kinds, "haul_out")
		} else {
			kinds = append(kinds, "crane")
		}
	case "breakwater":
		kinds = append(kinds, "breakwater")
	}
	if tags["leisure"] == "slipway" {
		kinds = append(kinds, "ramp")
	}
	switch tags["amenity"] {
	case "toilets":
		kinds = append(kinds, "toilets")
	case "shower":
		kinds = append(kinds, "showers")
	case "drinking_water", "water_point":
		kinds = append(kinds, "water")
	case "sanitary_dump_station":
		kinds = append(kinds, "pump_out")
	}
	switch tags["shop"] {
	case "ship_chandler":
		kinds = append(kinds, "chandler")
	case "boat":
		kinds = append(kinds, "boat_shop")
	}
	return append(kinds, smallCraftFacilityKinds(tags)...)
}

// smallCraftFacilityKinds returns the kinds a seamark small craft facility's
// categories stand for, one per category.
func smallCraftFacilityKinds(tags map[string]string) []string {
	var kinds []string
	if tags["seamark:type"] == "small_craft_facility" {
		for _, c := range strings.Split(tags["seamark:small_craft_facility:category"], ";") {
			c = strings.TrimSpace(c)
			if c == "" {
				continue
			}
			if kind, ok := smallCraftKinds[c]; ok {
				kinds = append(kinds, kind)
			} else {
				kinds = append(kinds, "other")
			}
		}
	}
	return kinds
}

var facilityOrder = map[string]int{
	"fuel": 0, "power": 1, "water": 2, "pump_out": 3, "haul_out": 4, "boatyard": 5, "crane": 6, "ramp": 7,
	"visitor_berth": 8, "toilets": 9, "showers": 10, "laundry": 11, "chandler": 12, "boat_shop": 13,
	"breakwater": 14, "other": 15,
}

// marinaOwnFacilityTags are tags a marina carries about itself. A "yes" is a
// facility on the marina's record; a "no" is reported as tagged absent.
var marinaOwnFacilityTags = []struct{ tag, kind string }{
	{"sanitary_dump_station", "pump_out"}, {"drinking_water", "water"},
	{"shower", "showers"}, {"toilets", "toilets"},
}

func classifyMarina(marina overpassElement, elements []overpassElement, lat, lon float64) marinaDetailsOutput {
	tags := marina.Tags
	out := marinaDetailsOutput{
		Facilities:    []marinaFacilityOut{},
		EntranceMarks: []marinaMarkOut{},
		Ashore:        []marinaAshoreOut{},
	}
	m := &out.Marina
	m.OSM = fmt.Sprintf("%s/%d", marina.Type, marina.ID)
	m.Name = bestName(tags)
	m.Lat, m.Lon = lat, lon
	m.Fee = strings.TrimSpace(tags["fee"])
	m.PowerSupply = strings.TrimSpace(tags["power_supply"])
	m.Phone = firstTag(tags, "phone", "contact:phone")
	m.Email = firstTag(tags, "email", "contact:email")
	m.Website = firstTag(tags, "website", "contact:website")
	m.VHF = firstTag(tags, "communication:vhf", "vhf", "seamark:radio_station:channel")
	m.OpeningHours = strings.TrimSpace(tags["opening_hours"])
	m.PontoonArms = []string{}
	// The resolve query returns the marina's centre only; its outline comes
	// with the details answer, which includes the marina element itself.
	outlineFrom := marina
	for _, el := range elements {
		if el.Type == marina.Type && el.ID == marina.ID {
			outlineFrom = el
		}
	}
	m.Outline = append([]marinaShape{}, elementShapes(outlineFrom)...)
	m.Pontoons = []marinaPontoon{}

	var unread []string
	if v := strings.TrimSpace(tags["capacity"]); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			m.Berths = &n
		} else {
			unread = append(unread, "capacity="+v)
		}
	}
	for _, f := range []struct {
		tag string
		dst **float64
	}{{"maxdraught", &m.MaxDraughtM}, {"maxlength", &m.MaxLengthM}, {"depth", &m.DepthM}} {
		v := strings.TrimSpace(tags[f.tag])
		if v == "" {
			continue
		}
		if metres, ok := parseMetres(v); ok {
			*f.dst = &metres
		} else {
			unread = append(unread, f.tag+"="+v)
		}
	}
	sort.Strings(unread)
	m.UnreadTags = unread

	// The marina's own tags. A facility it declares about itself has no place
	// of its own on a map (its only shape would be the outline), so it carries
	// no geometry.
	facilities := map[string]*marinaFacilityOut{}
	add := func(kind, name string, facts map[string]string, shapes []marinaShape) {
		key := kind + "|" + name + "|" + factsKey(facts)
		f, ok := facilities[key]
		if ok {
			f.Count++
		} else {
			f = &marinaFacilityOut{Kind: kind, Name: name, Count: 1, Facts: facts, Geometry: []marinaShape{}}
			facilities[key] = f
		}
		f.Geometry = appendShapes(f.Geometry, shapes)
	}
	hasFuel := false
	if fuel := facilityFacts(tags, true); fuel != nil {
		var absent []string
		for k, v := range fuel {
			if strings.TrimSpace(v) != "no" {
				hasFuel = true
			} else {
				absent = append(absent, k)
			}
		}
		if hasFuel {
			add("fuel", "", fuel, nil)
		} else {
			sort.Strings(absent)
			m.TaggedAbsent = append(m.TaggedAbsent, absent...)
		}
	}
	for _, kind := range smallCraftFacilityKinds(tags) {
		if kind == "fuel" && hasFuel {
			continue // the marina's fuel tags already list it
		}
		var facts map[string]string
		if kind == "other" {
			facts = map[string]string{"category": tags["seamark:small_craft_facility:category"]}
		}
		add(kind, "", facts, nil)
	}
	for _, o := range marinaOwnFacilityTags {
		switch strings.TrimSpace(tags[o.tag]) {
		case "yes":
			add(o.kind, "", nil, nil)
		case "no":
			m.TaggedAbsent = append(m.TaggedAbsent, o.tag)
		}
	}

	type markKey struct{ kind, side, colour, name string }
	marks := map[markKey]*marinaMarkOut{}
	arms := map[string]bool{}
	type ashoreCandidate struct {
		out  marinaAshoreOut
		dist float64
	}
	var ashore []ashoreCandidate

	for _, el := range elements {
		if el.Type == marina.Type && el.ID == marina.ID {
			continue
		}
		t := el.Tags
		name := bestName(t)

		if t["man_made"] == "pier" {
			private := t["access"] == "private" || t["access"] == "no"
			for _, sh := range elementShapes(el) {
				m.Pontoons = append(m.Pontoons, marinaPontoon{Name: name, Private: private, Shape: sh})
			}
			if name != "" && !private {
				arms[name] = true
			}
			continue
		}
		if kind := t["seamark:type"]; kind == "beacon_lateral" || kind == "buoy_lateral" {
			side := strings.TrimSpace(t["seamark:"+kind+":category"])
			if side == "" {
				side = "not tagged"
			}
			mk := markKey{strings.TrimSuffix(kind, "_lateral"), side, strings.TrimSpace(t["seamark:"+kind+":colour"]), name}
			mark, ok := marks[mk]
			if ok {
				mark.Count++
			} else {
				mark = &marinaMarkOut{Kind: mk.kind, Side: mk.side, Colour: mk.colour, Name: mk.name, Count: 1, Geometry: []marinaShape{}}
				marks[mk] = mark
			}
			mark.Geometry = appendShapes(mark.Geometry, elementShapes(el))
			continue
		}

		if kinds := facilityKinds(t); len(kinds) > 0 {
			shapes := elementShapes(el)
			for _, kind := range kinds {
				facts := facilityFacts(t, false)
				if kind == "other" {
					if facts == nil {
						facts = map[string]string{}
					}
					facts["category"] = t["seamark:small_craft_facility:category"]
				}
				add(kind, name, facts, shapes)
			}
		}

		if kind := ashoreKind(t); kind != "" && name != "" {
			if elLat, elLon, ok := elementLatLon(el); ok {
				d := haversineMeters(lat, lon, elLat, elLon)
				ashore = append(ashore, ashoreCandidate{
					out:  marinaAshoreOut{Kind: kind, Name: name, OpeningHours: strings.TrimSpace(t["opening_hours"]), DistanceM: int(math.Round(d/10) * 10), Lat: roundCoord(elLat), Lon: roundCoord(elLon)},
					dist: d,
				})
			}
		}
	}

	for _, f := range facilities {
		out.Facilities = append(out.Facilities, *f)
	}
	sort.Slice(out.Facilities, func(i, j int) bool {
		a, b := out.Facilities[i], out.Facilities[j]
		if facilityOrder[a.Kind] != facilityOrder[b.Kind] {
			return facilityOrder[a.Kind] < facilityOrder[b.Kind]
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return factsKey(a.Facts) < factsKey(b.Facts)
	})
	for _, mk := range marks {
		out.EntranceMarks = append(out.EntranceMarks, *mk)
	}
	sort.Slice(out.EntranceMarks, func(i, j int) bool {
		a, b := out.EntranceMarks[i], out.EntranceMarks[j]
		return fmt.Sprint(a.Kind, a.Side, a.Colour, a.Name) < fmt.Sprint(b.Kind, b.Side, b.Colour, b.Name)
	})
	for name := range arms {
		m.PontoonArms = append(m.PontoonArms, name)
	}
	sort.Strings(m.PontoonArms)

	sort.Slice(ashore, func(i, j int) bool {
		a, b := ashore[i], ashore[j]
		if ashoreRank(a.out.Kind) != ashoreRank(b.out.Kind) {
			return ashoreRank(a.out.Kind) < ashoreRank(b.out.Kind)
		}
		return a.dist < b.dist
	})
	perKind := map[string]int{}
	for _, c := range ashore {
		if perKind[c.out.Kind] >= marinaAshorePerKind {
			continue
		}
		perKind[c.out.Kind]++
		out.Ashore = append(out.Ashore, c.out)
	}
	return out
}

func appendShapes(dst, src []marinaShape) []marinaShape {
	for _, sh := range src {
		if len(dst) >= marinaShapesPerFeature {
			break
		}
		dst = append(dst, sh)
	}
	return dst
}

// ashoreKinds lists the shops and services reported ashore, in the order they
// are reported. The query's tag filters and the kind lookup both come from it.
var ashoreKinds = []struct{ key, kind string }{
	{"shop", "supermarket"}, {"shop", "convenience"}, {"shop", "laundry"},
	{"amenity", "pharmacy"}, {"amenity", "doctors"}, {"amenity", "hospital"}, {"amenity", "atm"}, {"amenity", "bank"},
}

// ashoreKindsOf returns the ashore kinds tagged under one key, for the query.
func ashoreKindsOf(key string) []string {
	var kinds []string
	for _, k := range ashoreKinds {
		if k.key == key {
			kinds = append(kinds, k.kind)
		}
	}
	return kinds
}

func ashoreKind(tags map[string]string) string {
	for _, k := range ashoreKinds {
		if tags[k.key] == k.kind {
			return k.kind
		}
	}
	return ""
}

func ashoreRank(kind string) int {
	for i, k := range ashoreKinds {
		if k.kind == kind {
			return i
		}
	}
	return len(ashoreKinds)
}

func factsKey(facts map[string]string) string {
	keys := make([]string, 0, len(facts))
	for k := range facts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + "=" + facts[k] + ";")
	}
	return b.String()
}

func firstTag(tags map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(tags[k]); v != "" {
			return v
		}
	}
	return ""
}

// parseMetres reads an OSM dimension: a number in metres, with or without a
// trailing "m", or in feet ("8 ft", "8'"). Anything else is not understood and
// the caller reports it as unread rather than guessing.
func parseMetres(raw string) (float64, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	factor := 1.0
	switch {
	case strings.HasSuffix(s, "ft"):
		s, factor = strings.TrimSpace(strings.TrimSuffix(s, "ft")), 0.3048
	case strings.HasSuffix(s, "'"):
		s, factor = strings.TrimSpace(strings.TrimSuffix(s, "'")), 0.3048
	case strings.HasSuffix(s, "m"):
		s = strings.TrimSpace(strings.TrimSuffix(s, "m"))
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return 0, false
	}
	return v * factor, true
}
