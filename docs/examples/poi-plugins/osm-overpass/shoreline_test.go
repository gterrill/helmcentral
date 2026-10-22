package main

// shoreline_test.go covers the OPTIONAL shoreline_near export: the raw
// coastline and reef lines Mate's anchorage_shelter tool casts rays over.
// The provider returns raw geometry; the host computes every distance.

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestBuildShorelineQuery_OneRoundTripWithGlobalBBox(t *testing.T) {
	q, err := buildShorelineQuery(-20.097633, 148.902533, 30558)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(q, "[out:json][timeout:12][bbox:") {
		t.Errorf("want the global bbox in the header (tag-first scans time out on the mirrors), got %s", q)
	}
	for _, clause := range []string{`way["natural"="coastline"]`, `way["natural"="reef"]`, `relation["natural"="reef"]`} {
		if !strings.Contains(q, clause) {
			t.Errorf("query misses %s: %s", clause, q)
		}
	}
	if !strings.Contains(q, ".c out geom(") || !strings.HasSuffix(q, ".r out geom;") {
		t.Errorf("want coastline clipped to the box and reefs whole, got %s", q)
	}
	if strings.Contains(q, "around:") {
		t.Errorf("no around filter: the bbox is the spatial index: %s", q)
	}
}

func TestBuildShorelineQuery_RefusesWhereNoBBoxExists(t *testing.T) {
	if _, err := buildShorelineQuery(-20, 179.9, 30000); err == nil {
		t.Error("a box across the antimeridian must be an error, not a slow unbounded scan")
	}
	if _, err := buildShorelineQuery(89.9, 0, 30000); err == nil {
		t.Error("a box over the pole must be an error")
	}
}

func shoreEl(typ string, id int64, tags map[string]string, pts ...[2]float64) overpassElement {
	el := overpassElement{Type: typ, ID: id, Tags: tags}
	for _, p := range pts {
		el.Geometry = append(el.Geometry, &overpassLatLon{Lat: p[0], Lon: p[1]})
	}
	return el
}

func TestRunShorelineNear_ClassifiesAndKeepsReefRingsWhole(t *testing.T) {
	ring := [][2]float64{{-19.55, 148.93}, {-19.55, 148.95}, {-19.53, 148.95}, {-19.53, 148.93}, {-19.55, 148.93}}
	post := func(q string) ([]overpassElement, error) {
		return []overpassElement{
			shoreEl("way", 1, map[string]string{"natural": "reef", "name": "Fairey Reef"}, ring...),
			shoreEl("way", 2, map[string]string{"natural": "coastline"}, [2]float64{-19.6, 148.9}, [2]float64{-19.5, 148.9}),
			shoreEl("way", 3, map[string]string{"natural": "cliff"}, [2]float64{-19.6, 148.9}, [2]float64{-19.5, 148.9}),
			{Type: "relation", ID: 9, Tags: map[string]string{"natural": "reef", "name": "Big Reef"}, Members: []overpassMember{
				{Type: "way", Role: "outer", Geometry: []*overpassLatLon{{Lat: -19.51, Lon: 148.91}, {Lat: -19.51, Lon: 148.92}, {Lat: -19.50, Lon: 148.92}, {Lat: -19.51, Lon: 148.91}}},
				{Type: "way", Role: "inner", Geometry: []*overpassLatLon{{Lat: -19.505, Lon: 148.912}, {Lat: -19.505, Lon: 148.915}}},
				{Type: "node", Role: "label"},
			}},
		}, nil
	}
	out, err := runShorelineNear(post, -19.5434, 148.939367, 30558)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]shorelineLineOut{}
	for _, l := range out.Lines {
		byID[l.ID] = l
	}
	reef := byID["way/1"]
	if reef.Kind != "reef" || reef.Name != "Fairey Reef" || !reef.Closed || len(reef.Points) != 5 {
		t.Errorf("reef way %+v", reef)
	}
	if c := byID["way/2"]; c.Kind != "coastline" || c.Closed {
		t.Errorf("coastline %+v", c)
	}
	if _, ok := byID["way/3"]; ok {
		t.Error("only coastline and reef are shoreline")
	}
	rel := byID["relation/9/0"]
	if rel.Kind != "reef" || rel.Name != "Big Reef" || !rel.Closed {
		t.Errorf("relation outer member %+v (all: %v)", rel, out.Lines)
	}
	if len(out.Lines) != 3 {
		t.Errorf("want reef way, coastline and one outer member only, got %d", len(out.Lines))
	}
}

func TestRunShorelineNear_ClipsCoastlineToTheSearchBoxAndThins(t *testing.T) {
	// A coast running far outside the box in both directions: only the part
	// in the box (plus the segment that crosses its edge) goes over the wire.
	var pts [][2]float64
	for i := 0; i <= 400; i++ {
		pts = append(pts, [2]float64{-20.0 + float64(i)*0.005, 148.9}) // 2 degrees long
	}
	post := func(string) ([]overpassElement, error) {
		return []overpassElement{shoreEl("way", 5, map[string]string{"natural": "coastline"}, pts...)}, nil
	}
	out, err := runShorelineNear(post, -19.0, 148.9, 18520) // 10 nm
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Lines) != 1 {
		t.Fatalf("lines: %v", out.Lines)
	}
	got := out.Lines[0].Points
	if len(got) < 60 || len(got) > 72 {
		t.Errorf("want only the ~67 points in the box of 401, got %d", len(got))
	}
	for _, p := range got {
		if p[0] < -19.19 || p[0] > -18.81 {
			t.Errorf("point %v is well outside the box", p)
		}
	}
	if out.Lines[0].Closed {
		t.Error("a clipped coastline is not a ring")
	}

	// Thinning: points a few metres apart collapse, ends are kept.
	dense := [][2]float64{}
	for i := 0; i <= 100; i++ {
		dense = append(dense, [2]float64{-19.0 + float64(i)*0.000009, 148.9}) // ~1 m apart
	}
	post = func(string) ([]overpassElement, error) {
		return []overpassElement{shoreEl("way", 6, map[string]string{"natural": "coastline"}, dense...)}, nil
	}
	out, err = runShorelineNear(post, -19.0, 148.9, 18520)
	if err != nil {
		t.Fatal(err)
	}
	pp := out.Lines[0].Points
	if len(pp) > 12 || pp[0] != [2]float64{-19.0, 148.9} || pp[len(pp)-1][0] < -18.9992 {
		t.Errorf("thinned %d points: first %v last %v", len(pp), pp[0], pp[len(pp)-1])
	}
}

func TestRunShorelineNear_NullNodesSplitCoastlineButBreakReefs(t *testing.T) {
	// out geom(bbox) returns null for nodes outside the box.
	coast := shoreEl("way", 7, map[string]string{"natural": "coastline"},
		[2]float64{-19.01, 148.9}, [2]float64{-19.00, 148.9}, [2]float64{-18.99, 148.9})
	coast.Geometry = append([]*overpassLatLon{nil}, coast.Geometry...)
	coast.Geometry = append(coast.Geometry, nil, &overpassLatLon{Lat: -18.95, Lon: 148.9}, &overpassLatLon{Lat: -18.94, Lon: 148.9})
	post := func(string) ([]overpassElement, error) { return []overpassElement{coast}, nil }
	out, err := runShorelineNear(post, -19.0, 148.9, 18520)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Lines) != 2 || len(out.Lines[0].Points) != 3 || len(out.Lines[1].Points) != 2 {
		t.Errorf("a null must split the coast, never become a point at 0,0: %+v", out.Lines)
	}

	reef := shoreEl("way", 8, map[string]string{"natural": "reef"}, [2]float64{-19, 148.9}, [2]float64{-19, 148.91})
	reef.Geometry = append(reef.Geometry, nil)
	post = func(string) ([]overpassElement, error) { return []overpassElement{reef}, nil }
	if _, err := runShorelineNear(post, -19.0, 148.9, 18520); err == nil {
		t.Error("a reef with a missing node must be an error")
	}
}

func TestRunShorelineNear_UpstreamErrorsAreErrors(t *testing.T) {
	post := func(string) ([]overpassElement, error) {
		return nil, fmt.Errorf("overpass query failed: runtime error: timed out")
	}
	if _, err := runShorelineNear(post, -20, 149, 30000); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("want the upstream error, got %v", err)
	}
}

func TestParseOverpassBody_ShorelineRemarkIsAnError(t *testing.T) {
	body := []byte(`{"elements":[],"remark":"runtime error: Query timed out in \"query\" at line 1 after 12 seconds."}`)
	if _, err := parseOverpassBody("application/json", body); err == nil {
		t.Error("a runtime-error remark must be an error")
	}
}

func TestRunShorelineNear_RealStonehavenCaptureStaysModest(t *testing.T) {
	f, err := os.Open("../../../../backend/testdata/shelter/stonehaven.json.gz")
	if err != nil {
		t.Skipf("fixture not available: %v", err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var resp overpassResponse
	if err := json.NewDecoder(zr).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	post := func(string) ([]overpassElement, error) { return resp.Elements, nil }
	out, err := runShorelineNear(post, -20.097633, 148.902533, 30558)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	t.Logf("stonehaven: %d lines, %d bytes", len(out.Lines), len(b))
	if len(out.Lines) < 50 {
		t.Errorf("only %d lines", len(out.Lines))
	}
	if len(b) > 600_000 {
		t.Errorf("payload %d bytes is not modest for a slow boat link", len(b))
	}
}
