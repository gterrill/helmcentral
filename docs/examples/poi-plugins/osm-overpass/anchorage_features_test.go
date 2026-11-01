package main

// anchorage_features_test.go tests the anchorage_features export against live
// captures under testdata/ (taken with the exact query the export sends, from
// the boat's local Overpass mirror, data current 2026-10-31), plus small
// hand-written element sets for the shapes the mirror holds no example of
// (a seamark restricted area).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	butterflyLat, butterflyLon = -20.0765, 148.9250 // West Butterfly Bay, Hook Island
	moretonLat, moretonLon     = -27.4940, 153.3366 // Moreton Bay, between The Corner and a coral seabed area
)

func anchoragePost(t *testing.T, fixture string, sent *[]string) overpassResponseFunc {
	t.Helper()
	res := loadOverpassResult(t, fixture)
	return func(q string) (overpassResponse, error) {
		if sent != nil {
			*sent = append(*sent, q)
		}
		return res, nil
	}
}

func TestRunAnchorageFeatures_ButterflyBayMoorings(t *testing.T) {
	var sent []string
	out, err := runAnchorageFeatures(anchoragePost(t, "overpass_anchorage_butterfly_bay.json", &sent), anchorageFeaturesInput{Lat: butterflyLat, Lon: butterflyLon})
	if err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || !strings.Contains(sent[0], "around:1852,-20.076500,148.925000") {
		t.Errorf("want one query within 1 nm of the position, got %v", sent)
	}
	if strings.Contains(sent[0], "meta") {
		t.Errorf("the mirror has no meta files, but the query asks for them: %s", sent[0])
	}
	if len(out.Moorings) != 23 {
		t.Fatalf("moorings = %d, want the 23 public moorings in the fixture", len(out.Moorings))
	}
	first := out.Moorings[0]
	if first.VesselSizeM == nil || *first.VesselSizeM != 20 || first.Class != "B" {
		t.Errorf("nearest mooring = %+v, want VesselSize 20 and class B from its description", first)
	}
	if first.Name == "" || !strings.HasPrefix(first.OSM, "node/") {
		t.Errorf("a mooring keeps its name and OSM id: %+v", first)
	}
	for i := 1; i < len(out.Moorings); i++ {
		if out.Moorings[i].DistanceM < out.Moorings[i-1].DistanceM {
			t.Fatal("moorings are not nearest first")
		}
	}
	if out.Bay == nil || out.Bay.Name != "Butterfly Bay" || out.Bay.Contains {
		t.Errorf("bay = %+v, want the nearest bay, Butterfly Bay, mapped as a point and so not containing", out.Bay)
	}
	if out.DataAsOf != "2026-10-31T21:22:14Z" {
		t.Errorf("DataAsOf = %q, want the answer's timestamp_osm_base", out.DataAsOf)
	}
	if len(out.RestrictedAreas) != 2 {
		t.Errorf("restricted = %+v, want the two marine protected areas and no land park", out.RestrictedAreas)
	}
	for _, r := range out.RestrictedAreas {
		if r.Source != "protected_area" || r.Restriction != "Marine Park" || len(r.Geometry) != 0 || r.DistanceM != nil || r.Category != "" {
			t.Errorf("protected area %+v: want source protected_area, title Marine Park, no geometry, no distance and no category (the raw protect_class is not a name)", r)
		}
	}
}

func TestRunAnchorageFeatures_MoretonBayAreas(t *testing.T) {
	out, err := runAnchorageFeatures(anchoragePost(t, "overpass_anchorage_moreton_bay.json", nil), anchorageFeaturesInput{Lat: moretonLat, Lon: moretonLon})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.AnchorageAreas) != 1 || out.AnchorageAreas[0].Name != "The Corner" {
		t.Errorf("anchorage areas = %+v, want The Corner", out.AnchorageAreas)
	}
	if len(out.SeabedAreas) != 1 || out.SeabedAreas[0].Surface != "coral" {
		t.Errorf("seabed areas = %+v, want one coral area", out.SeabedAreas)
	}
	if len(out.Moorings) != 0 {
		t.Errorf("moorings = %+v, want none mapped here", out.Moorings)
	}
	if len(out.AnchorageAreas[0].Geometry) != 1 || out.AnchorageAreas[0].Geometry[0].Kind != "point" {
		t.Errorf("an anchorage mapped as a node is one point: %+v", out.AnchorageAreas[0].Geometry)
	}
}

func TestRunAnchorageFeatures_GoldenWire(t *testing.T) {
	for name, tc := range map[string]struct {
		fixture string
		lat     float64
		lon     float64
	}{
		"anchorage_features_butterfly_bay_output.json": {"overpass_anchorage_butterfly_bay.json", butterflyLat, butterflyLon},
		"anchorage_features_moreton_bay_output.json":   {"overpass_anchorage_moreton_bay.json", moretonLat, moretonLon},
	} {
		out, err := runAnchorageFeatures(anchoragePost(t, tc.fixture, nil), anchorageFeaturesInput{Lat: tc.lat, Lon: tc.lon})
		if err != nil {
			t.Fatal(err)
		}
		got, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, '\n')
		path := filepath.Join("testdata", name)
		if *updateMarinaGolden {
			if err := os.WriteFile(path, got, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("export output drifted from %s; run go test -update if the change is intended", path)
		}
	}
}

func TestParseMooringDescription(t *testing.T) {
	size, class := parseMooringDescription("FID 65\nPt_No 66\nLocation Henning Island\nMSQ_Moorin 898\nLat_dDeg -20.27\nVesselSize 25\nMooring_cl C\nMooring_Li 48mm")
	if size == nil || *size != 25 || class != "C" {
		t.Errorf("got %v %q, want 25 and C", size, class)
	}
	for _, desc := range []string{"", "Location Henning Island\nMSQ_Moorin 898", "VesselSize big\nMooring_cl", "VesselSize 0", "VesselSize -4"} {
		size, class := parseMooringDescription(desc)
		if size != nil || class != "" {
			t.Errorf("%q parsed to %v %q, want nothing", desc, size, class)
		}
	}
	// Only one of the two is a partial answer, not an untagged mooring.
	size, class = parseMooringDescription("VesselSize 12")
	if size == nil || *size != 12 || class != "" {
		t.Errorf("size only: %v %q", size, class)
	}
}

func TestClassifyAnchorage_MooringWithNoUsableDescriptionIsUntagged(t *testing.T) {
	lat, lon := -20.35, 148.7
	node := func(id int64, tags map[string]string) overpassElement {
		la, lo := lat+float64(id)*1e-5, lon
		return overpassElement{Type: "node", ID: id, Lat: &la, Lon: &lo, Tags: tags}
	}
	out := classifyAnchorage([]overpassElement{
		node(1, map[string]string{"seamark:type": "mooring", "ref": "A1"}),
		node(2, map[string]string{"seamark:type": "buoy_special_purpose", "seamark:buoy_special_purpose:category": "mooring", "name": "FID 9", "description": "Location Somewhere"}),
		node(3, map[string]string{"seamark:type": "buoy_special_purpose", "seamark:buoy_special_purpose:category": "mooring", "description": "VesselSize 14\nMooring_cl A"}),
		node(4, map[string]string{"seamark:type": "buoy_special_purpose", "seamark:buoy_special_purpose:category": "cardinal"}),
	}, lat, lon)
	if len(out.Moorings) != 3 {
		t.Fatalf("moorings = %+v, want three (a non-mooring special purpose buoy is not one)", out.Moorings)
	}
	for _, m := range out.Moorings[:2] {
		if m.VesselSizeM != nil || m.Class != "" {
			t.Errorf("%+v: want no size or class", m)
		}
	}
	if m := out.Moorings[2]; m.VesselSizeM == nil || *m.VesselSizeM != 14 || m.Class != "A" {
		t.Errorf("%+v: want 14 m, class A", m)
	}
	if out.Moorings[0].Ref != "A1" {
		t.Errorf("ref kept: %+v", out.Moorings[0])
	}
}

func TestClassifyAnchorage_RestrictedAreaAndContainingBay(t *testing.T) {
	// Hand-written: the mirror holds no seamark restricted area to capture.
	lat, lon := -20.0, 148.0
	pt := func(la, lo float64) *overpassLatLon { return &overpassLatLon{Lat: la, Lon: lo} }
	restricted := overpassElement{Type: "way", ID: 10, Tags: map[string]string{
		"seamark:type": "restricted_area", "seamark:restricted_area:restriction": "anchoring_prohibited",
		"seamark:restricted_area:category": "nature_reserve", "name": "Reef protection zone",
	}, Geometry: []*overpassLatLon{pt(-20.001, 148.001), pt(-20.001, 148.003), pt(-20.003, 148.003), pt(-20.003, 148.001), pt(-20.001, 148.001)}}
	outer := overpassElement{Type: "way", ID: 11, Tags: map[string]string{"natural": "bay", "name": "Big Bay"},
		Geometry: []*overpassLatLon{pt(-19.99, 147.99), pt(-19.99, 148.01), pt(-20.01, 148.01), pt(-20.01, 147.99), pt(-19.99, 147.99)}}
	la, lo := -20.0, 148.0001
	nearPoint := overpassElement{Type: "node", ID: 12, Lat: &la, Lon: &lo, Tags: map[string]string{"natural": "bay", "name": "Pointy Cove"}}
	out := classifyAnchorage([]overpassElement{restricted, nearPoint, outer}, lat, lon)
	if len(out.RestrictedAreas) != 1 {
		t.Fatalf("restricted = %+v", out.RestrictedAreas)
	}
	r := out.RestrictedAreas[0]
	if r.Source != "seamark" || r.Restriction != "anchoring_prohibited" || r.Category != "nature_reserve" || r.Name != "Reef protection zone" || len(r.Geometry) != 1 || r.DistanceM == nil || *r.DistanceM > 400 || r.Geometry[0].Kind != "polygon" {
		t.Errorf("restricted area = %+v", r)
	}
	if out.Bay == nil || out.Bay.Name != "Big Bay" || !out.Bay.Contains {
		t.Errorf("bay = %+v, want the bay that holds the position, not the nearer point bay", out.Bay)
	}
}

func TestRunAnchorageFeatures_EmptyIsAnEmptyAnswerAndNoDateStaysEmpty(t *testing.T) {
	post := func(string) (overpassResponse, error) { return overpassResponse{}, nil }
	out, err := runAnchorageFeatures(post, anchorageFeaturesInput{Lat: -20, Lon: 148})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	if !strings.Contains(string(raw), `"restricted_areas":[]`) || !strings.Contains(string(raw), `"moorings":[]`) || !strings.Contains(string(raw), `"bay":null`) {
		t.Errorf("empty lists must be [] and no bay null: %s", raw)
	}
	if out.DataAsOf != "" || strings.Contains(string(raw), "data_as_of") {
		t.Errorf("no timestamp must leave the date empty, got %q", out.DataAsOf)
	}
}

func TestRunAnchorageFeatures_QueryErrorIsReturned(t *testing.T) {
	post := func(string) (overpassResponse, error) { return overpassResponse{}, os.ErrDeadlineExceeded }
	if _, err := runAnchorageFeatures(post, anchorageFeaturesInput{Lat: -20, Lon: 148}); err == nil {
		t.Fatal("a failing query must be an error, not an empty answer")
	}
}
