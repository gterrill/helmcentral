package main

import (
	"testing"
)

func pathsFrom(t *testing.T, body string) []signalKPath {
	t.Helper()
	seedSelfTree(t, body)
	return collectSignalKPaths(globalSignalKSnapshot.selfTree())
}

func pathNames(paths []signalKPath) []string {
	names := make([]string, 0, len(paths))
	for _, path := range paths {
		names = append(names, path.Path)
	}
	return names
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// Nothing enumerated paths before this: every accessor was a fixed key chain,
// so a widget could only ever show a value a developer had already wired.
func TestCollectSignalKPathsFindsNestedLeaves(t *testing.T) {
	paths := pathsFrom(t, `{
		"environment": {"depth": {"belowTransducer": {"value": 3.2}}},
		"propulsion": {"port": {"oilPressure": {"value": 241325.0}}}
	}`)

	names := pathNames(paths)
	for _, want := range []string{"environment.depth.belowTransducer", "propulsion.port.oilPressure"} {
		if !contains(names, want) {
			t.Fatalf("expected %q in %v", want, names)
		}
	}
}

func TestCollectSignalKPathsIsSortedForAStablePicker(t *testing.T) {
	paths := pathsFrom(t, `{
		"tanks": {"fuel": {"0": {"currentLevel": {"value": 0.8}}}},
		"environment": {"depth": {"belowTransducer": {"value": 3.2}}}
	}`)

	names := pathNames(paths)
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("paths must be sorted, got %v", names)
		}
	}
}

// SignalK declares units in meta; surfacing them is what lets the picker offer
// sensible conversions instead of making the operator know oil pressure is in
// pascals.
func TestCollectSignalKPathsSurfacesUnitsFromMeta(t *testing.T) {
	paths := pathsFrom(t, `{
		"propulsion": {"port": {"oilPressure": {"value": 241325.0, "meta": {"units": "Pa"}}}}
	}`)

	if len(paths) != 1 {
		t.Fatalf("expected 1 path, got %v", pathNames(paths))
	}
	if paths[0].Units != "Pa" {
		t.Fatalf("units: got %q, want %q", paths[0].Units, "Pa")
	}
}

func TestCollectSignalKPathsCarriesCurrentValue(t *testing.T) {
	paths := pathsFrom(t, `{"environment": {"depth": {"belowTransducer": {"value": 3.25}}}}`)

	if len(paths) != 1 || paths[0].Value == nil {
		t.Fatalf("expected a value to preview, got %+v", paths)
	}
	if *paths[0].Value != 3.25 {
		t.Fatalf("value: got %v, want 3.25", *paths[0].Value)
	}
}

// A position is a container, not something a gauge can render, so its members
// are offered individually.
func TestCollectSignalKPathsDescendsIntoObjectValues(t *testing.T) {
	paths := pathsFrom(t, `{
		"navigation": {"position": {"value": {"latitude": -21.11, "longitude": 149.22}}}
	}`)

	names := pathNames(paths)
	for _, want := range []string{"navigation.position.latitude", "navigation.position.longitude"} {
		if !contains(names, want) {
			t.Fatalf("expected %q in %v", want, names)
		}
	}
	if contains(names, "navigation.position") {
		t.Fatalf("the container itself is not bindable, got %v", names)
	}
}

func TestCollectSignalKPathsSkipsMetadataSiblings(t *testing.T) {
	paths := pathsFrom(t, `{
		"environment": {"depth": {"belowTransducer": {
			"value": 3.2, "timestamp": "2026-08-13T00:00:00Z", "$source": "n2k.1"
		}}}
	}`)

	names := pathNames(paths)
	if len(names) != 1 || names[0] != "environment.depth.belowTransducer" {
		t.Fatalf("timestamp and $source are not bindable paths, got %v", names)
	}
}

func TestCollectSignalKPathsHandlesStringValues(t *testing.T) {
	paths := pathsFrom(t, `{"navigation": {"state": {"value": "anchored"}}}`)

	if len(paths) != 1 || paths[0].Path != "navigation.state" {
		t.Fatalf("expected navigation.state, got %v", pathNames(paths))
	}
	if paths[0].Value != nil {
		t.Fatalf("a string has no numeric preview, got %v", *paths[0].Value)
	}
}

func TestCollectSignalKPathsEmptyTree(t *testing.T) {
	if paths := collectSignalKPaths(map[string]any{}); len(paths) != 0 {
		t.Fatalf("expected no paths, got %v", pathNames(paths))
	}
}
