package main

// marina_details_test.go tests the marina_details export: the two queries it
// sends, how the answer is classified, and the Coral Sea Marina result against
// live captures under testdata/ (README fixtures section).

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
)

const coralSeaLat, coralSeaLon = -20.2663105, 148.7101677

func coralSeaPost(t *testing.T) (overpassResponseFunc, *[]string) {
	t.Helper()
	resolve := loadOverpassResult(t, "overpass_marina_resolve_coral_sea.json")
	details := loadOverpassResult(t, "overpass_marina_details_coral_sea.json")
	var sent []string
	post := func(query string) (overpassResponse, error) {
		sent = append(sent, query)
		if strings.Contains(query, "around.m") {
			return details, nil
		}
		return resolve, nil
	}
	return post, &sent
}

func loadOverpassResult(t *testing.T, filename string) overpassResponse {
	t.Helper()
	res, err := parseOverpassResponse("application/json", loadFixture(t, filename))
	if err != nil {
		t.Fatalf("parse fixture %s: %v", filename, err)
	}
	return res
}

func TestRunMarinaDetails_PassesTheDataDateThrough(t *testing.T) {
	post, _ := coralSeaPost(t)
	out, err := runMarinaDetails(post, marinaDetailsInput{Lat: coralSeaLat, Lon: coralSeaLon})
	if err != nil {
		t.Fatal(err)
	}
	if out.DataAsOf != "2026-10-29T21:20:09Z" {
		t.Errorf("DataAsOf = %q, want the details answer's timestamp_osm_base", out.DataAsOf)
	}
	raw, _ := json.Marshal(out)
	if !strings.Contains(string(raw), `"data_as_of":"2026-10-29T21:20:09Z"`) {
		t.Errorf("output does not carry data_as_of: %s", raw)
	}
}

func TestRunMarinaDetails_NoTimestampLeavesTheDateEmpty(t *testing.T) {
	post, _ := coralSeaPost(t)
	bare := func(q string) (overpassResponse, error) {
		res, err := post(q)
		res.Osm3s.TimestampOsmBase = ""
		return res, err
	}
	out, err := runMarinaDetails(bare, marinaDetailsInput{Lat: coralSeaLat, Lon: coralSeaLon})
	if err != nil {
		t.Fatal(err)
	}
	if out.DataAsOf != "" {
		t.Errorf("DataAsOf = %q, want empty (never the clock)", out.DataAsOf)
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "data_as_of") {
		t.Errorf("empty date should be omitted: %s", raw)
	}
}

func TestParseOverpassResult_KeepsTheOsmBaseTimestamp(t *testing.T) {
	body := []byte(`{"version":0.6,"osm3s":{"timestamp_osm_base":"2026-10-29T21:20:09Z"},"elements":[]}`)
	res, err := parseOverpassResponse("application/json", body)
	if err != nil || res.Osm3s.TimestampOsmBase != "2026-10-29T21:20:09Z" {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestBuildMarinaResolveQuery_NearestMarinaOrHarbourWithin500m(t *testing.T) {
	q := buildMarinaResolveQuery(-20.266310, 148.710168)
	for _, want := range []string{
		`nwr["leisure"="marina"](around:500,-20.266310,148.710168)`,
		`nwr["seamark:type"="harbour"](around:500,-20.266310,148.710168)`,
		"out tags center",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("resolve query missing %q:\n%s", want, q)
		}
	}
}

func TestMarinaQueries_NeverAskForMeta(t *testing.T) {
	// The boat's local mirror has no meta files: "out meta" there answers
	// HTTP 200 with a runtime error remark.
	resolve := buildMarinaResolveQuery(coralSeaLat, coralSeaLon)
	details, err := buildMarinaDetailsQuery("way", 245930150)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{resolve, details} {
		if strings.Contains(q, "meta") {
			t.Errorf("query asks for meta:\n%s", q)
		}
	}
}

func TestBuildMarinaDetailsQuery_AroundTheMarinaElement(t *testing.T) {
	q, err := buildMarinaDetailsQuery("way", 245930150)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"way(245930150)->.m",
		"(around.m:300)",
		"(around.m:1000)",
		`["waterway"="fuel"]`,
		`["leisure"="slipway"]`,
		`["name"]`,
		"out tags geom",
		"(.a; - .f;)->.a;",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("details query missing %q:\n%s", want, q)
		}
	}
}

func TestBuildMarinaDetailsQuery_RejectsAnUnknownElementType(t *testing.T) {
	if _, err := buildMarinaDetailsQuery("area", 1); err == nil {
		t.Fatal("expected an error for an element type that is not node, way or relation")
	}
}

func TestRunMarinaDetails_CoralSeaMarinaLive(t *testing.T) {
	post, sent := coralSeaPost(t)
	out, err := runMarinaDetails(post, marinaDetailsInput{Lat: coralSeaLat, Lon: coralSeaLon})
	if err != nil {
		t.Fatalf("runMarinaDetails: %v", err)
	}
	if len(*sent) != 2 {
		t.Fatalf("expected a resolve query then a details query, sent %d", len(*sent))
	}

	m := out.Marina
	if m.Name != "Coral Sea Marina Resort" || m.OSM != "way/245930150" {
		t.Errorf("marina = %q %q", m.Name, m.OSM)
	}
	if m.Berths == nil || *m.Berths != 520 {
		t.Errorf("berths = %v, want 520", m.Berths)
	}
	if m.Fee != "yes" || m.PowerSupply != "yes" {
		t.Errorf("fee %q power %q, want yes yes", m.Fee, m.PowerSupply)
	}
	if !strings.HasPrefix(m.Website, "https://www.coralseamarina.com") {
		t.Errorf("website = %q", m.Website)
	}
	if m.MaxDraughtM != nil || m.MaxLengthM != nil || m.DepthM != nil {
		t.Errorf("Coral Sea charts no draught, length or depth, got %v %v %v", m.MaxDraughtM, m.MaxLengthM, m.DepthM)
	}
	if m.Phone != "" || m.VHF != "" {
		t.Errorf("Coral Sea charts no phone or VHF, got %q %q", m.Phone, m.VHF)
	}

	wantArms := []string{"A Arm", "B&C Arm", "D&E Arm", "F&G Arm", "H Arm", "L Arm", "M&N Arm", "O&P Arm", "Q&Z Arm", "R&S Arm", "T&U Arm", "V&W Arm", "X&Y Arm"}
	if !reflect.DeepEqual(m.PontoonArms, wantArms) {
		t.Errorf("pontoon arms = %v, want %v", m.PontoonArms, wantArms)
	}

	fuel := facility(out, "fuel")
	if fuel == nil || fuel.Count != 2 {
		t.Fatalf("fuel = %+v, want two fuel points", fuel)
	}
	if len(fuel.Facts) != 0 {
		t.Errorf("fuel type is not tagged at Coral Sea, but facts = %v", fuel.Facts)
	}
	if h := facility(out, "haul_out"); h == nil || h.Facts["crane:type"] != "travel_lift" {
		t.Errorf("haul_out = %+v, want the travel lift", h)
	}
	if facility(out, "boatyard") == nil {
		t.Error("boatyard missing")
	}
	if r := facility(out, "ramp"); r == nil || r.Name != "Coral Sea Marina Boatramp" || r.Facts["fee"] != "no" {
		t.Errorf("ramp = %+v", r)
	}
	if tl := facility(out, "toilets"); tl == nil || tl.Count != 2 {
		t.Errorf("toilets = %+v", tl)
	}

	var port, starboard int
	for _, mk := range out.EntranceMarks {
		switch mk.Side {
		case "port":
			port += mk.Count
		case "starboard":
			starboard += mk.Count
		}
	}
	if port != 2 || starboard != 2 {
		t.Errorf("lateral marks port %d starboard %d, want 2 and 2: %+v", port, starboard, out.EntranceMarks)
	}

	var woolworths *marinaAshoreOut
	for i, a := range out.Ashore {
		if a.Kind == "supermarket" {
			woolworths = &out.Ashore[i]
		}
		if a.Name == "" {
			t.Errorf("ashore entry without a name: %+v", a)
		}
	}
	if woolworths == nil || woolworths.Name != "Woolworths" || !strings.HasPrefix(woolworths.OpeningHours, "Mo-Fr 08:00-21:00") {
		t.Errorf("supermarket = %+v", woolworths)
	}
	for _, a := range out.Ashore {
		if a.DistanceM <= 0 || a.DistanceM > 1500 {
			t.Errorf("ashore distance out of range: %+v", a)
		}
	}
}

func TestRunMarinaDetails_CoralSeaGeometryLive(t *testing.T) {
	post, _ := coralSeaPost(t)
	out, err := runMarinaDetails(post, marinaDetailsInput{Lat: coralSeaLat, Lon: coralSeaLon})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Marina.Outline) != 1 || out.Marina.Outline[0].Kind != "polygon" || len(out.Marina.Outline[0].Points) < 4 {
		t.Errorf("marina outline = %+v", out.Marina.Outline)
	}
	if len(out.Marina.Pontoons) < 20 {
		t.Errorf("pontoons = %d, want every pier drawn, named or not", len(out.Marina.Pontoons))
	}
	var named int
	for _, p := range out.Marina.Pontoons {
		if p.Name != "" {
			named++
		}
		if p.Shape.Kind == "point" {
			t.Errorf("a pier cannot be a point: %+v", p)
		}
	}
	if named != 13 {
		t.Errorf("named pontoons = %d, want 13", named)
	}
	if fuel := facility(out, "fuel"); fuel == nil || len(fuel.Geometry) != 2 || fuel.Geometry[0].Kind != "point" {
		t.Errorf("fuel geometry = %+v", fuel)
	}
	if bw := facility(out, "breakwater"); bw == nil || len(bw.Geometry) != 2 {
		t.Errorf("breakwater geometry = %+v", bw)
	}
	for _, mk := range out.EntranceMarks {
		if len(mk.Geometry) != mk.Count {
			t.Errorf("mark %+v has %d shapes for %d marks", mk, len(mk.Geometry), mk.Count)
		}
	}
	for _, a := range out.Ashore {
		if a.Lat == 0 || a.Lon == 0 {
			t.Errorf("ashore without a position: %+v", a)
		}
	}
}

func TestShapeFromPoints_ThinsLongLinesKeepingTheEnds(t *testing.T) {
	var pts [][2]float64
	for i := 0; i < 1000; i++ {
		pts = append(pts, [2]float64{float64(i), 0})
	}
	sh, _ := shapeFromPoints(pts)
	if len(sh.Points) != marinaShapeMaxPoints || sh.Points[0] != pts[0] || sh.Points[len(sh.Points)-1] != pts[999] {
		t.Errorf("thinned to %d, ends %v %v", len(sh.Points), sh.Points[0], sh.Points[len(sh.Points)-1])
	}
}

// The host rejects an unclosed polygon, so the plugin must only call a ring a
// polygon when it closes, and thinning a long ring must keep it closed.
func TestShapeFromPoints_PolygonsAreAlwaysClosed(t *testing.T) {
	open, _ := shapeFromPoints([][2]float64{{0, 0}, {0, 1}, {1, 1}, {1, 0}})
	if open.Kind != "line" {
		t.Errorf("an open four-point way is %q, want line", open.Kind)
	}
	var ring [][2]float64
	for i := 0; i < 1000; i++ {
		a := float64(i) / 1000 * 2 * math.Pi
		ring = append(ring, [2]float64{math.Cos(a), math.Sin(a)})
	}
	ring = append(ring, ring[0])
	sh, _ := shapeFromPoints(ring)
	closed := sh.Points[0] == sh.Points[len(sh.Points)-1]
	if sh.Kind != "polygon" || !closed || len(sh.Points) > marinaShapeMaxPoints {
		t.Errorf("thinned ring: kind %q, %d points, closed %v", sh.Kind, len(sh.Points), closed)
	}
}

func facility(out marinaDetailsOutput, kind string) *marinaFacilityOut {
	for i := range out.Facilities {
		if out.Facilities[i].Kind == kind {
			return &out.Facilities[i]
		}
	}
	return nil
}

func TestRunMarinaDetails_NoMarinaIsAnErrorNotAnEmptyResult(t *testing.T) {
	post := func(string) (overpassResponse, error) { return overpassResponse{}, nil }
	_, err := runMarinaDetails(post, marinaDetailsInput{Lat: -20.5, Lon: 149})
	if err == nil || !strings.Contains(err.Error(), "no marina charted within 500 m of that position") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunMarinaDetails_QueryErrorIsReturnedNotMasked(t *testing.T) {
	boom := fmt.Errorf("overpass query failed: runtime error: Tried to use meta file but no meta files available")
	post := func(string) (overpassResponse, error) { return overpassResponse{}, boom }
	if _, err := runMarinaDetails(post, marinaDetailsInput{Lat: coralSeaLat, Lon: coralSeaLon}); err != boom {
		t.Fatalf("err = %v, want the query's own error", err)
	}
}

func TestMeterlessRemarkFromTheLocalMirrorIsAnError(t *testing.T) {
	body := []byte(`{"version":0.6,"elements":[],"remark":"runtime error: Tried to use meta file but no meta files available"}`)
	if _, err := parseOverpassResponse("application/json", body); err == nil {
		t.Fatal("a runtime error remark must not read as an empty answer")
	}
}

func TestRunMarinaDetails_SecondQueryErrorIsReturned(t *testing.T) {
	resolve := loadOverpassResult(t, "overpass_marina_resolve_coral_sea.json")
	boom := fmt.Errorf("overpass rate limited")
	post := func(q string) (overpassResponse, error) {
		if strings.Contains(q, "around.m") {
			return overpassResponse{}, boom
		}
		return resolve, nil
	}
	if _, err := runMarinaDetails(post, marinaDetailsInput{Lat: coralSeaLat, Lon: coralSeaLon}); err != boom {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

func TestPickMarina_PrefersAMarinaOverAHarbourNode(t *testing.T) {
	lat, lon := 1.0, 1.0
	harbour := overpassElement{Type: "node", ID: 1, Lat: &lat, Lon: &lon, Tags: map[string]string{"seamark:type": "harbour"}}
	farLat := 1.001
	marina := overpassElement{Type: "way", ID: 2, Center: &overpassLatLon{Lat: farLat, Lon: lon}, Tags: map[string]string{"leisure": "marina", "name": "M"}}
	got, ok := pickMarina([]overpassElement{harbour, marina}, lat, lon)
	if !ok || got.ID != 2 {
		t.Fatalf("picked %+v", got)
	}
}

func marinaWith(tags map[string]string) overpassElement {
	lat, lon := 10.0, 20.0
	return overpassElement{Type: "way", ID: 9, Center: &overpassLatLon{Lat: lat, Lon: lon}, Tags: tags}
}

func TestClassifyMarina_ChartedLimitsAndContact(t *testing.T) {
	m := marinaWith(map[string]string{
		"leisure": "marina", "name": "Test", "maxdraught": "2.5", "maxlength": "18 m", "depth": "3",
		"contact:phone": "+61 7 1234 5678", "communication:vhf": "73", "opening_hours": "24/7", "capacity": "40",
	})
	out := classifyMarina(m, nil, 10, 20)
	got := out.Marina
	if got.MaxDraughtM == nil || *got.MaxDraughtM != 2.5 || got.MaxLengthM == nil || *got.MaxLengthM != 18 || got.DepthM == nil || *got.DepthM != 3 {
		t.Errorf("limits = %v %v %v", got.MaxDraughtM, got.MaxLengthM, got.DepthM)
	}
	if got.Phone != "+61 7 1234 5678" || got.VHF != "73" || got.OpeningHours != "24/7" {
		t.Errorf("contact = %+v", got)
	}
}

func TestClassifyMarina_FeetAndUnreadableValues(t *testing.T) {
	m := marinaWith(map[string]string{"leisure": "marina", "maxdraught": "8 ft", "maxlength": "plenty", "capacity": "lots"})
	got := classifyMarina(m, nil, 10, 20).Marina
	if got.MaxDraughtM == nil || *got.MaxDraughtM < 2.43 || *got.MaxDraughtM > 2.44 {
		t.Errorf("8 ft = %v m, want 2.438", got.MaxDraughtM)
	}
	if got.MaxLengthM != nil || got.Berths != nil {
		t.Errorf("unreadable values must stay unset, got %v %v", got.MaxLengthM, got.Berths)
	}
	want := []string{"capacity=lots", "maxlength=plenty"}
	if !reflect.DeepEqual(got.UnreadTags, want) {
		t.Errorf("unread tags = %v, want %v", got.UnreadTags, want)
	}
}

func TestClassifyMarina_MarineFacilityTags(t *testing.T) {
	near := func(tags map[string]string) overpassElement {
		lat, lon := 10.0, 20.0
		return overpassElement{Type: "node", ID: 100, Lat: &lat, Lon: &lon, Tags: tags}
	}
	cases := []struct {
		tags map[string]string
		kind string
	}{
		{map[string]string{"seamark:type": "small_craft_facility", "seamark:small_craft_facility:category": "fuel_station"}, "fuel"},
		{map[string]string{"seamark:type": "small_craft_facility", "seamark:small_craft_facility:category": "water_tap"}, "water"},
		{map[string]string{"seamark:type": "small_craft_facility", "seamark:small_craft_facility:category": "pump-out"}, "pump_out"},
		{map[string]string{"seamark:type": "small_craft_facility", "seamark:small_craft_facility:category": "chandler"}, "chandler"},
		{map[string]string{"seamark:type": "small_craft_facility", "seamark:small_craft_facility:category": "showers"}, "showers"},
		{map[string]string{"seamark:type": "small_craft_facility", "seamark:small_craft_facility:category": "laundrette"}, "laundry"},
		{map[string]string{"seamark:type": "small_craft_facility", "seamark:small_craft_facility:category": "visitor_berth"}, "visitor_berth"},
		{map[string]string{"seamark:type": "small_craft_facility", "seamark:small_craft_facility:category": "slipway"}, "ramp"},
		{map[string]string{"seamark:type": "small_craft_facility", "seamark:small_craft_facility:category": "boat_hoist"}, "haul_out"},
		{map[string]string{"amenity": "sanitary_dump_station"}, "pump_out"},
		{map[string]string{"amenity": "drinking_water"}, "water"},
		{map[string]string{"amenity": "water_point"}, "water"},
		{map[string]string{"shop": "ship_chandler", "name": "Marine Supplies"}, "chandler"},
		{map[string]string{"shop": "boat", "name": "Boats R Us"}, "boat_shop"},
		{map[string]string{"man_made": "crane"}, "crane"},
		{map[string]string{"man_made": "breakwater"}, "breakwater"},
	}
	for _, c := range cases {
		out := classifyMarina(marinaWith(map[string]string{"leisure": "marina"}), []overpassElement{near(c.tags)}, 10, 20)
		if facility(out, c.kind) == nil {
			t.Errorf("%v: no %q facility in %+v", c.tags, c.kind, out.Facilities)
		}
	}
}

func TestClassifyMarina_FuelTypeComesOnlyFromTags(t *testing.T) {
	lat, lon := 10.0, 20.0
	pump := overpassElement{Type: "node", ID: 5, Lat: &lat, Lon: &lon, Tags: map[string]string{"waterway": "fuel", "fuel:diesel": "yes", "fuel:octane_95": "no"}}
	out := classifyMarina(marinaWith(map[string]string{"leisure": "marina"}), []overpassElement{pump}, 10, 20)
	f := facility(out, "fuel")
	if f == nil || f.Facts["fuel:diesel"] != "yes" || f.Facts["fuel:octane_95"] != "no" {
		t.Fatalf("fuel = %+v", f)
	}
}

func TestClassifyMarina_FuelTaggedOnTheMarinaItself(t *testing.T) {
	out := classifyMarina(marinaWith(map[string]string{"leisure": "marina", "fuel:diesel": "yes", "sanitary_dump_station": "yes", "toilets": "no"}), nil, 10, 20)
	if f := facility(out, "fuel"); f == nil || f.Facts["fuel:diesel"] != "yes" {
		t.Errorf("fuel = %+v", f)
	}
	if facility(out, "pump_out") == nil {
		t.Error("pump_out missing")
	}
	if facility(out, "toilets") != nil || !reflect.DeepEqual(out.Marina.TaggedAbsent, []string{"toilets"}) {
		t.Errorf("toilets=no must be reported as tagged absent, not listed: %+v %v", out.Facilities, out.Marina.TaggedAbsent)
	}
}

func TestClassifyMarina_PrivatePiersAreNotArms(t *testing.T) {
	lat, lon := 10.0, 20.0
	pier := func(id int64, tags map[string]string) overpassElement {
		return overpassElement{Type: "way", ID: id, Center: &overpassLatLon{Lat: lat, Lon: lon}, Tags: tags}
	}
	out := classifyMarina(marinaWith(map[string]string{"leisure": "marina"}), []overpassElement{
		pier(1, map[string]string{"man_made": "pier", "name": "A Arm"}),
		pier(2, map[string]string{"man_made": "pier", "name": "Owner's Jetty", "access": "private"}),
		pier(3, map[string]string{"man_made": "pier"}),
	}, 10, 20)
	if !reflect.DeepEqual(out.Marina.PontoonArms, []string{"A Arm"}) {
		t.Errorf("arms = %v", out.Marina.PontoonArms)
	}
}

func TestClassifyMarina_AshoreKeepsThreeNearestPerKindAndNeverUnnamed(t *testing.T) {
	var els []overpassElement
	for i := 0; i < 5; i++ {
		lat, lon := 10.0+float64(i+1)*0.0005, 20.0
		els = append(els, overpassElement{Type: "node", ID: int64(i + 1), Lat: &lat, Lon: &lon, Tags: map[string]string{"amenity": "atm", "name": fmt.Sprintf("ATM %d", i+1)}})
	}
	lat, lon := 10.0001, 20.0
	els = append(els, overpassElement{Type: "node", ID: 50, Lat: &lat, Lon: &lon, Tags: map[string]string{"amenity": "pharmacy"}})
	out := classifyMarina(marinaWith(map[string]string{"leisure": "marina"}), els, 10, 20)
	if len(out.Ashore) != 3 || out.Ashore[0].Name != "ATM 1" || out.Ashore[2].Name != "ATM 3" {
		t.Errorf("ashore = %+v", out.Ashore)
	}
}

func TestClassifyMarina_AnElementIsNeverCountedAsItsOwnMarina(t *testing.T) {
	m := marinaWith(map[string]string{"leisure": "marina", "name": "Test", "amenity": "toilets"})
	out := classifyMarina(m, []overpassElement{m}, 10, 20)
	if facility(out, "toilets") != nil && facility(out, "toilets").Count != 1 {
		t.Errorf("the marina element appeared twice: %+v", out.Facilities)
	}
}

func TestMarinaDetailsOutput_MarshalsEmptyListsNotNull(t *testing.T) {
	out := classifyMarina(marinaWith(map[string]string{"leisure": "marina"}), nil, 10, 20)
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"facilities":[]`, `"entrance_marks":[]`, `"ashore":[]`, `"pontoon_arms":[]`, `"pontoons":[]`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("%s missing from %s", key, raw)
		}
	}
}

func TestClassifyMarina_FuelNoOnTheMarinaItselfIsTaggedAbsent(t *testing.T) {
	out := classifyMarina(marinaWith(map[string]string{"leisure": "marina", "fuel:diesel": "no", "fuel:octane_95": "no"}), nil, 10, 20)
	if facility(out, "fuel") != nil {
		t.Errorf("fuel listed although every fuel tag is no: %+v", out.Facilities)
	}
	if !reflect.DeepEqual(out.Marina.TaggedAbsent, []string{"fuel:diesel", "fuel:octane_95"}) {
		t.Errorf("tagged absent = %v", out.Marina.TaggedAbsent)
	}
	out = classifyMarina(marinaWith(map[string]string{"leisure": "marina", "fuel:diesel": "yes", "fuel:octane_95": "no"}), nil, 10, 20)
	if facility(out, "fuel") == nil || len(out.Marina.TaggedAbsent) != 0 {
		t.Errorf("a yes among the fuel tags must list fuel: %+v %v", out.Facilities, out.Marina.TaggedAbsent)
	}
}

func TestClassifyMarina_SmallCraftFacilityTaggedOnTheMarinaItself(t *testing.T) {
	lat, lon := 10.0, 20.0
	out := classifyMarina(overpassElement{Type: "node", ID: 9, Lat: &lat, Lon: &lon, Tags: map[string]string{
		"leisure": "marina", "seamark:type": "small_craft_facility",
		"seamark:small_craft_facility:category": "fuel_station;boat_hoist;electricity",
	}}, nil, 10, 20)
	for _, kind := range []string{"fuel", "haul_out", "power"} {
		f := facility(out, kind)
		if f == nil {
			t.Fatalf("no %q facility in %+v", kind, out.Facilities)
		}
		if f.Count != 1 || f.Geometry == nil || len(f.Geometry) != 0 {
			t.Errorf("%s = %+v, want one feature and no geometry (the marina's outline is not a place)", kind, f)
		}
	}
}
