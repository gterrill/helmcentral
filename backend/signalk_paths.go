package main

import (
	"net/http"
	"sort"
	"strings"

	"github.com/labstack/echo/v4"
)

// signalKPath describes one bindable value, for the widget path picker.
type signalKPath struct {
	Path  string   `json:"path"`
	Units string   `json:"units,omitempty"`
	Value *float64 `json:"value,omitempty"`
}

// Depth cap on the walk. SignalK trees are shallow; anything deeper is a
// malformed or cyclic payload, and a runaway walk on a boat computer is worse
// than a truncated picker.
const signalKPathMaxDepth = 12

// collectSignalKPaths walks a snapshot tree and returns every node carrying a
// value leaf, as a dotted path.
//
// Nothing enumerated paths before this: every accessor was a fixed key chain,
// so a widget could only show a value a developer had already wired. This is
// what the delta stream (ADR 0037) made possible.
func collectSignalKPaths(tree map[string]any) []signalKPath {
	var out []signalKPath
	walkSignalKPaths(tree, nil, 0, &out)

	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func walkSignalKPaths(node map[string]any, prefix []string, depth int, out *[]signalKPath) {
	if depth > signalKPathMaxDepth {
		return
	}

	if raw, ok := node["value"]; ok {
		// A leaf. Object values (navigation.position) are containers rather
		// than something a gauge can render, so descend into them instead.
		if asMap, isMap := raw.(map[string]any); isMap {
			for key, child := range asMap {
				if _, childIsMap := child.(map[string]any); childIsMap {
					continue
				}
				entry := signalKPath{Path: strings.Join(append(append([]string{}, prefix...), key), ".")}
				if number, isNumber := child.(float64); isNumber {
					entry.Value = &number
				}
				*out = append(*out, entry)
			}
			return
		}

		entry := signalKPath{Path: strings.Join(prefix, "."), Units: unitsFor(node)}
		if number, isNumber := raw.(float64); isNumber {
			entry.Value = &number
		}
		if entry.Path != "" {
			*out = append(*out, entry)
		}
		return
	}

	for key, child := range node {
		// meta carries units and display hints, not values of its own.
		if key == "meta" || key == "$source" || key == "timestamp" {
			continue
		}
		asMap, ok := child.(map[string]any)
		if !ok {
			continue
		}
		walkSignalKPaths(asMap, append(append([]string{}, prefix...), key), depth+1, out)
	}
}

// unitsFor reads SignalK's own unit declaration, which is what lets the picker
// offer sensible conversions rather than making the operator know that oil
// pressure arrives in pascals.
func unitsFor(node map[string]any) string {
	meta, ok := node["meta"].(map[string]any)
	if !ok {
		return ""
	}
	units, _ := meta["units"].(string)
	return units
}

func signalKPathsHandler(c echo.Context) error {
	tree := globalSignalKSnapshot.selfTree()
	if tree == nil {
		return c.JSON(http.StatusOK, map[string]any{"paths": []signalKPath{}})
	}
	return c.JSON(http.StatusOK, map[string]any{"paths": collectSignalKPaths(tree)})
}

// buildGaugeValuesPayload pushes the current value of every path a gauge is
// bound to. The backend owns the page config, so it can work out what to send
// without the browser subscribing to anything.
func buildGaugeValuesPayload() map[string]any {
	paths := gaugeBoundPaths()
	values := make(map[string]any, len(paths))

	if len(paths) > 0 {
		read := snapshotAlarmReader(globalSignalKSnapshot)
		for _, path := range paths {
			sample := read(path)
			if !sample.Present {
				// Absent stays absent: a gauge must render the structural dash
				// rather than a zero it would be read as a real measurement.
				values[path] = nil
				continue
			}
			values[path] = sample.Value
		}
	}

	return map[string]any{"values": values}
}
