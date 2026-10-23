package main

// shoreline_test.go covers the OPTIONAL shoreline_square and
// shoreline_changes exports: the raw coastline and reef lines of one coverage
// square, and the count of edits since a date. The provider returns raw
// geometry; the host computes every distance.

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// A one degree square off Airlie Beach, the shape the host asks for.
var testSquare = shorelineBoxInput{South: -21, West: 148, North: -20, East: 149}

func TestBuildShorelineQuery_OneRoundTripWithGlobalBBox(t *testing.T) {
	q, err := buildShorelineQuery(testSquare)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(q, "[out:json][timeout:100][bbox:-21.000000,148.000000,-20.000000,149.000000]") {
		t.Errorf("want the square as the global bbox in the header (tag-first scans time out on the mirrors), got %s", q)
	}
	for _, clause := range []string{`way["natural"="coastline"]`, `way["natural"="reef"]`, `relation["natural"="reef"]`} {
		if !strings.Contains(q, clause) {
			t.Errorf("query misses %s: %s", clause, q)
		}
	}
	if !strings.Contains(q, ".c out geom(-21.000000,148.000000,-20.000000,149.000000)") || !strings.HasSuffix(q, ".r out geom;") {
		t.Errorf("want coastline clipped to the square and reefs whole, got %s", q)
	}
	if strings.Contains(q, "around:") {
		t.Errorf("no around filter: the bbox is the spatial index: %s", q)
	}
}

func TestShorelineBox_RefusesWhereTheHostMustNotAsk(t *testing.T) {
	bad := map[string]shorelineBoxInput{
		"antimeridian": {South: -20, West: 179.5, North: -19, East: 180.5},
		"pole":         {South: 89.5, West: 0, North: 90.5, East: 1},
		"no area":      {South: -20, West: 148, North: -20, East: 149},
		"inverted":     {South: -19, West: 148, North: -20, East: 149},
		"huge":         {South: -30, West: 140, North: -20, East: 150},
	}
	for name, box := range bad {
		if _, err := buildShorelineQuery(box); err == nil {
			t.Errorf("%s: want an error, not a slow unbounded scan", name)
		}
		if _, err := buildShorelineChangesQuery(box, time.Now()); err == nil {
			t.Errorf("%s (changes): want an error", name)
		}
	}
}

func TestBuildShorelineChangesQuery_NewerFilterAndCount(t *testing.T) {
	since := time.Date(2026, 10, 3, 9, 30, 0, 0, time.FixedZone("AEST", 10*3600))
	q, err := buildShorelineChangesQuery(testSquare, since)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(q, "[out:json][timeout:60][bbox:-21.000000,148.000000,-20.000000,149.000000]") {
		t.Errorf("header: %s", q)
	}
	for _, clause := range []string{
		`way["natural"="coastline"](newer:"2026-10-02T23:30:00Z")`,
		`way["natural"="reef"](newer:"2026-10-02T23:30:00Z")`,
		`relation["natural"="reef"](newer:"2026-10-02T23:30:00Z")`,
	} {
		if !strings.Contains(q, clause) {
			t.Errorf("query misses %s: %s", clause, q)
		}
	}
	if !strings.HasSuffix(q, "out count;") {
		t.Errorf("want a count, not the elements: %s", q)
	}
	if _, err := buildShorelineChangesQuery(testSquare, time.Time{}); err == nil {
		t.Error("a missing since must be an error")
	}
}

func TestRunShorelineChanges(t *testing.T) {
	count := func(total string) overpassQueryFunc {
		return func(string) ([]overpassElement, error) {
			return []overpassElement{{Type: "count", Tags: map[string]string{"nodes": "0", "ways": "2", "relations": "1", "total": total}}}, nil
		}
	}
	out, err := runShorelineChanges(count("3"), testSquare, time.Now())
	if err != nil || out.Changed != 3 {
		t.Fatalf("changed %v err %v", out, err)
	}
	if out, err := runShorelineChanges(count("0"), testSquare, time.Now()); err != nil || out.Changed != 0 {
		t.Errorf("zero edits is a real answer: %v %v", out, err)
	}
	if _, err := runShorelineChanges(count("many"), testSquare, time.Now()); err == nil {
		t.Error("an unreadable count is an error")
	}
	none := func(string) ([]overpassElement, error) { return nil, nil }
	if _, err := runShorelineChanges(none, testSquare, time.Now()); err == nil {
		t.Error("no count element must be an error, never zero edits")
	}
	failing := func(string) ([]overpassElement, error) { return nil, fmt.Errorf("overpass rate limited") }
	if _, err := runShorelineChanges(failing, testSquare, time.Now()); err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("want the upstream error, got %v", err)
	}
}

func shoreEl(typ string, id int64, tags map[string]string, pts ...[2]float64) overpassElement {
	el := overpassElement{Type: typ, ID: id, Tags: tags}
	for _, p := range pts {
		el.Geometry = append(el.Geometry, &overpassLatLon{Lat: p[0], Lon: p[1]})
	}
	return el
}

func TestRunShorelineSquare_ClassifiesAndKeepsReefRingsWhole(t *testing.T) {
	ring := [][2]float64{{-20.55, 148.93}, {-20.55, 148.95}, {-20.53, 148.95}, {-20.53, 148.93}, {-20.55, 148.93}}
	post := func(q string) ([]overpassElement, error) {
		return []overpassElement{
			shoreEl("way", 1, map[string]string{"natural": "reef", "name": "Fairey Reef"}, ring...),
			shoreEl("way", 2, map[string]string{"natural": "coastline"}, [2]float64{-20.6, 148.9}, [2]float64{-20.5, 148.9}),
			shoreEl("way", 3, map[string]string{"natural": "cliff"}, [2]float64{-20.6, 148.9}, [2]float64{-20.5, 148.9}),
			{Type: "relation", ID: 9, Tags: map[string]string{"natural": "reef", "name": "Big Reef"}, Members: []overpassMember{
				{Type: "way", Role: "outer", Geometry: []*overpassLatLon{{Lat: -20.51, Lon: 148.91}, {Lat: -20.51, Lon: 148.92}, {Lat: -20.50, Lon: 148.92}, {Lat: -20.51, Lon: 148.91}}},
				{Type: "way", Role: "inner", Geometry: []*overpassLatLon{{Lat: -20.505, Lon: 148.912}, {Lat: -20.505, Lon: 148.915}}},
				{Type: "node", Role: "label"},
			}},
		}, nil
	}
	out, err := runShorelineSquare(post, testSquare)
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

func TestRunShorelineSquare_ReefCrossingTheSquareEdgeStaysWhole(t *testing.T) {
	// A reef straddling the east edge (149.0). Overpass returns it whole
	// because reefs are output without a bbox; the plugin must not clip it.
	ring := [][2]float64{{-20.5, 148.95}, {-20.5, 149.05}, {-20.45, 149.05}, {-20.45, 148.95}, {-20.5, 148.95}}
	post := func(string) ([]overpassElement, error) {
		return []overpassElement{shoreEl("way", 4, map[string]string{"natural": "reef"}, ring...)}, nil
	}
	out, err := runShorelineSquare(post, testSquare)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Lines) != 1 || !out.Lines[0].Closed || len(out.Lines[0].Points) != 5 {
		t.Errorf("reef across the edge must stay a closed ring: %+v", out.Lines)
	}
}

func TestRunShorelineSquare_ClipsCoastlineToTheSquareAndThins(t *testing.T) {
	// A coast running far outside the square in both directions: only the part
	// in the square (plus the segment that crosses its edge) goes over the wire.
	var pts [][2]float64
	for i := 0; i <= 600; i++ {
		pts = append(pts, [2]float64{-21.5 + float64(i)*0.005, 148.5}) // 3 degrees long
	}
	post := func(string) ([]overpassElement, error) {
		return []overpassElement{shoreEl("way", 5, map[string]string{"natural": "coastline"}, pts...)}, nil
	}
	out, err := runShorelineSquare(post, testSquare)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Lines) != 1 {
		t.Fatalf("lines: %v", out.Lines)
	}
	got := out.Lines[0].Points
	if len(got) < 195 || len(got) > 210 {
		t.Errorf("want only the ~201 points in the square of 601, got %d", len(got))
	}
	for _, p := range got {
		if p[0] < -21.01 || p[0] > -19.99 {
			t.Errorf("point %v is well outside the square", p)
		}
	}
	if out.Lines[0].Closed {
		t.Error("a clipped coastline is not a ring")
	}

	// Thinning: points a few metres apart collapse, ends are kept.
	dense := [][2]float64{}
	for i := 0; i <= 100; i++ {
		dense = append(dense, [2]float64{-20.5 + float64(i)*0.000009, 148.5}) // ~1 m apart
	}
	post = func(string) ([]overpassElement, error) {
		return []overpassElement{shoreEl("way", 6, map[string]string{"natural": "coastline"}, dense...)}, nil
	}
	out, err = runShorelineSquare(post, testSquare)
	if err != nil {
		t.Fatal(err)
	}
	pp := out.Lines[0].Points
	if len(pp) > 12 || pp[0] != [2]float64{-20.5, 148.5} || pp[len(pp)-1][0] < -20.4992 {
		t.Errorf("thinned %d points: first %v last %v", len(pp), pp[0], pp[len(pp)-1])
	}
}

func TestRunShorelineSquare_NullNodesSplitCoastlineButBreakReefs(t *testing.T) {
	// out geom(bbox) returns null for nodes outside the box.
	coast := shoreEl("way", 7, map[string]string{"natural": "coastline"},
		[2]float64{-20.51, 148.9}, [2]float64{-20.50, 148.9}, [2]float64{-20.49, 148.9})
	coast.Geometry = append([]*overpassLatLon{nil}, coast.Geometry...)
	coast.Geometry = append(coast.Geometry, nil, &overpassLatLon{Lat: -20.45, Lon: 148.9}, &overpassLatLon{Lat: -20.44, Lon: 148.9})
	post := func(string) ([]overpassElement, error) { return []overpassElement{coast}, nil }
	out, err := runShorelineSquare(post, testSquare)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Lines) != 2 || len(out.Lines[0].Points) != 3 || len(out.Lines[1].Points) != 2 {
		t.Errorf("a null must split the coast, never become a point at 0,0: %+v", out.Lines)
	}

	reef := shoreEl("way", 8, map[string]string{"natural": "reef"}, [2]float64{-20, 148.9}, [2]float64{-20, 148.91})
	reef.Geometry = append(reef.Geometry, nil)
	post = func(string) ([]overpassElement, error) { return []overpassElement{reef}, nil }
	if _, err := runShorelineSquare(post, testSquare); err == nil {
		t.Error("a reef with a missing node must be an error")
	}
}

func TestRunShorelineSquare_NoCoastIsAnEmptyAnswerButErrorsAreErrors(t *testing.T) {
	empty := func(string) ([]overpassElement, error) { return nil, nil }
	out, err := runShorelineSquare(empty, testSquare)
	if err != nil || out.Lines == nil || len(out.Lines) != 0 {
		t.Errorf("open sea is an empty list, not an error: %+v %v", out, err)
	}
	post := func(string) ([]overpassElement, error) {
		return nil, fmt.Errorf("overpass query failed: runtime error: timed out")
	}
	if _, err := runShorelineSquare(post, testSquare); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("want the upstream error, got %v", err)
	}
}

func TestParseOverpassBody_ShorelineRemarkIsAnError(t *testing.T) {
	body := []byte(`{"elements":[],"remark":"runtime error: Query timed out in \"query\" at line 1 after 12 seconds."}`)
	if _, err := parseOverpassBody("application/json", body); err == nil {
		t.Error("a runtime-error remark must be an error")
	}
}

func TestParseOverpassBody_CountElementDecodes(t *testing.T) {
	body := []byte(`{"elements":[{"type":"count","id":0,"tags":{"nodes":"0","ways":"2","relations":"1","total":"3"}}]}`)
	els, err := parseOverpassBody("application/json", body)
	if err != nil || len(els) != 1 || els[0].Type != "count" || els[0].Tags["total"] != "3" {
		t.Errorf("count element: %+v %v", els, err)
	}
}

func TestRunShorelineSquare_RealStonehavenCaptureStaysModest(t *testing.T) {
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
	out, err := runShorelineSquare(post, testSquare)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	t.Logf("stonehaven in square -21,148: %d lines, %d bytes", len(out.Lines), len(b))
	if len(out.Lines) < 50 {
		t.Errorf("only %d lines", len(out.Lines))
	}
	if len(b) > 600_000 {
		t.Errorf("payload %d bytes is not modest for a slow boat link", len(b))
	}
}

func TestRunShorelineSquare_ReefRelationOuterRingOfSeveralWaysIsAssembled(t *testing.T) {
	// A reef relation whose outer ring is two ways meeting end to end, the
	// second written in the opposite direction. Emitted way by way neither is
	// closed and an anchor inside the reef is missed.
	a := []*overpassLatLon{{Lat: -20.50, Lon: 148.90}, {Lat: -20.50, Lon: 148.92}, {Lat: -20.52, Lon: 148.92}}
	b := []*overpassLatLon{{Lat: -20.50, Lon: 148.90}, {Lat: -20.52, Lon: 148.90}, {Lat: -20.52, Lon: 148.92}}
	post := func(string) ([]overpassElement, error) {
		return []overpassElement{{Type: "relation", ID: 5, Tags: map[string]string{"natural": "reef", "name": "Split Reef"}, Members: []overpassMember{
			{Type: "way", Role: "outer", Geometry: a},
			{Type: "way", Role: "outer", Geometry: b},
		}}}, nil
	}
	out, err := runShorelineSquare(post, testSquare)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Lines) != 1 {
		t.Fatalf("want one assembled ring, got %+v", out.Lines)
	}
	l := out.Lines[0]
	if !l.Closed || l.Kind != "reef" || l.Name != "Split Reef" || l.Points[0] != l.Points[len(l.Points)-1] {
		t.Errorf("ring not closed: %+v", l)
	}
	if len(l.Points) != 5 {
		t.Errorf("want 5 points (4 corners + closing), got %d: %v", len(l.Points), l.Points)
	}
}

func TestRunShorelineSquare_ReefRelationOuterWaysThatDoNotJoinStayOpen(t *testing.T) {
	a := []*overpassLatLon{{Lat: -20.50, Lon: 148.90}, {Lat: -20.50, Lon: 148.92}}
	b := []*overpassLatLon{{Lat: -20.60, Lon: 148.90}, {Lat: -20.60, Lon: 148.92}}
	post := func(string) ([]overpassElement, error) {
		return []overpassElement{{Type: "relation", ID: 6, Tags: map[string]string{"natural": "reef"}, Members: []overpassMember{
			{Type: "way", Role: "outer", Geometry: a}, {Type: "way", Role: "outer", Geometry: b},
		}}}, nil
	}
	out, err := runShorelineSquare(post, testSquare)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Lines) != 2 || out.Lines[0].Closed || out.Lines[1].Closed {
		t.Errorf("unjoined ways must stay open and separate: %+v", out.Lines)
	}
}

func TestOverpassStatusError_CarriesRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	err := overpassStatusError(429, "30", "text/html", []byte("<html>"), now)
	if want := "overpass rate limited (HTTP 429 with a non-JSON body, retry after 30s)"; err.Error() != want {
		t.Errorf("got %q, want %q", err, want)
	}
	err = overpassStatusError(504, now.Add(90*time.Second).Format(time.RFC1123), "application/json", []byte("{}"), now)
	if want := "overpass returned status 504, retry after 91s"; err.Error() != want {
		t.Errorf("got %q, want %q", err, want)
	}
	if err := overpassStatusError(503, "", "application/json", []byte("{}"), now); err.Error() != "overpass returned status 503" {
		t.Errorf("got %q", err)
	}
}
