package main

// anchorage_geometry_test.go: containment and distance for anchorage_features
// elements, including multipolygon relations that arrive as open member ways.

import "testing"

func llPts(pairs ...float64) []*overpassLatLon {
	var out []*overpassLatLon
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, &overpassLatLon{Lat: pairs[i], Lon: pairs[i+1]})
	}
	return out
}

func TestDistanceTo_RelationRingsAssembledFromMemberWays(t *testing.T) {
	// Square 148.00..148.02 x -20.00..-20.02 split in three ways, one reversed.
	rel := overpassElement{Type: "relation", ID: 1, Members: []overpassMember{
		{Type: "way", Role: "outer", Geometry: llPts(-20.00, 148.00, -20.00, 148.02)},
		{Type: "way", Role: "outer", Geometry: llPts(-20.02, 148.00, -20.02, 148.02, -20.00, 148.02)},
		{Type: "way", Role: "outer", Geometry: llPts(-20.02, 148.00, -20.00, 148.00)},
	}}
	_, contains, ok := distanceTo(rel, elementShapes(rel), -20.01, 148.01)
	if !ok || !contains {
		t.Fatalf("point inside the assembled ring: contains=%v ok=%v", contains, ok)
	}
	d, contains, _ := distanceTo(rel, elementShapes(rel), -20.01, 148.03)
	if contains || d < 1000 || d > 1100 {
		t.Errorf("outside point: dist=%d contains=%v", d, contains)
	}
}

func TestDistanceTo_RelationInnerRingIsNotContained(t *testing.T) {
	rel := overpassElement{Type: "relation", ID: 2, Members: []overpassMember{
		{Type: "way", Role: "outer", Geometry: llPts(-20.00, 148.00, -20.00, 148.02, -20.02, 148.02, -20.02, 148.00, -20.00, 148.00)},
		{Type: "way", Role: "inner", Geometry: llPts(-20.008, 148.008, -20.008, 148.012, -20.012, 148.012, -20.012, 148.008, -20.008, 148.008)},
	}}
	if _, c, _ := distanceTo(rel, elementShapes(rel), -20.01, 148.01); c {
		t.Error("point inside the hole must not be contained")
	}
	if _, c, _ := distanceTo(rel, elementShapes(rel), -20.003, 148.003); !c {
		t.Error("point in the ring outside the hole must be contained")
	}
}

func TestDistanceTo_MeasuresToSegmentNotVertex(t *testing.T) {
	// A two-vertex edge about 21 km long passing about 500 m north of the point.
	el := overpassElement{Type: "way", ID: 3, Geometry: llPts(-19.9955, 147.9, -19.9955, 148.1)}
	d, contains, ok := distanceTo(el, elementShapes(el), -20.0, 148.0)
	if !ok || contains || d < 450 || d > 550 {
		t.Errorf("dist=%d contains=%v ok=%v, want about 500 m", d, contains, ok)
	}
}
