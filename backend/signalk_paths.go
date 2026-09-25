package main

import (
	"net/http"
	"sort"
	"strings"
	"time"

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
	paths := []signalKPath{}
	if tree := globalSignalKSnapshot.selfTree(); tree != nil {
		paths = collectSignalKPaths(tree)
	}

	// Hoisted out of the loop below: this used to run inside the loop over
	// derivedPathIDs, recomputing every derived path (two self-tree copies,
	// the fuel-path tree walks, the barometer ring scan) once per id --
	// 172-234ms measured against the boat for what should be one pass
	// (backend-perf-audit.md Tier 1 #2).
	derivedValues := derivedPathValues()

	// Derived paths are listed alongside the published ones, or an operator
	// has no way to find something to bind that the vessel never announces.
	for _, derived := range derivedPathIDs {
		entry := signalKPath{Path: derived, Units: derivedPathUnits[derived]}
		if value := derivedValues[derived]; value != nil {
			entry.Value = value
		}
		paths = append(paths, entry)
	}

	return c.JSON(http.StatusOK, map[string]any{"paths": paths})
}

// signalKPathSample is one leaf of the self tree as check_signalk_paths
// (Mate diagnostics, ADR 0131) needs it: collectSignalKPaths' own walk,
// extended to carry the fields it drops ($source, timestamp) plus an age
// computed the same way pathAge computes one for a single known path.
//
// Value is the raw decoded JSON value (float64, string, bool, or a nested
// map for an object leaf like navigation.position) rather than
// collectSignalKPaths' float64-only *Value - a diagnostic tool has a use for
// a non-numeric leaf's value where the widget picker never did.
type signalKPathSample struct {
	Path  string
	Value any
	Units string
	// Source is the leaf's own declared "$source", "" when the node carries
	// none (an object-value child like navigation.position.latitude, which
	// inherits its parent's source rather than declaring its own).
	Source string
	// Timestamp is the leaf's own declared RFC3339 "timestamp", "" when the
	// node carries none at all - see signalk_paths.go's own note (referenced
	// from pathAge below) on why a dead path's last declared timestamp is
	// worth keeping rather than discarding.
	Timestamp string
	// AgeSeconds is -1 when unknown (neither the node nor pathSeen has
	// anything for this path - freshestTimestampAge's own "no evidence"
	// contract), never a value that reads as freshly measured.
	AgeSeconds float64
}

// collectSignalKPathSamples walks tree exactly like collectSignalKPaths, but
// keeps $source/timestamp per leaf and computes each leaf's age via
// signalKPathSampleAge, instead of discarding both the way the widget path
// picker always has. tree may be nil (no self tree yet); the result is then
// simply empty.
func collectSignalKPathSamples(snapshot *signalKSnapshot, context string, tree map[string]any, now time.Time) []signalKPathSample {
	out := make([]signalKPathSample, 0)
	walkSignalKPathSamples(snapshot, context, tree, nil, 0, now, &out)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func walkSignalKPathSamples(snapshot *signalKSnapshot, context string, node map[string]any, prefix []string, depth int, now time.Time, out *[]signalKPathSample) {
	if depth > signalKPathMaxDepth {
		return
	}

	if raw, ok := node["value"]; ok {
		path := strings.Join(prefix, ".")
		if path == "" {
			return
		}
		source, _ := node["$source"].(string)
		timestamp, _ := node["timestamp"].(string)
		*out = append(*out, signalKPathSample{
			Path:       path,
			Value:      raw,
			Units:      unitsFor(node),
			Source:     source,
			Timestamp:  timestamp,
			AgeSeconds: signalKPathSampleAge(snapshot, context, path, node, now),
		})
		return
	}

	for key, child := range node {
		if key == "meta" || key == "$source" || key == "timestamp" {
			continue
		}
		asMap, ok := child.(map[string]any)
		if !ok {
			continue
		}
		walkSignalKPathSamples(snapshot, context, asMap, append(append([]string{}, prefix...), key), depth+1, now, out)
	}
}

// signalKPathSampleAge mirrors pathAge's own preference below - a node's own
// declared timestamp survives a SignalK reconnect replay and wins whenever
// it exists; pathSeen (the snapshot's own arrival-time record) is consulted
// only when the node carries none - but works from a node already in hand
// from collectSignalKPathSamples' walk, rather than re-resolving it via
// snapshot.nodeAt(path) the way pathAge itself does: the walk has already
// found this node once, and re-walking the whole tree per leaf to find it
// again would cost this diagnostic tool one full path resolution per match.
func signalKPathSampleAge(snapshot *signalKSnapshot, context, path string, node map[string]any, now time.Time) float64 {
	if nodeAge := freshestTimestampAge(node, now); nodeAge >= 0 {
		return nodeAge
	}
	seen := snapshot.lastSeen(context, path)
	if seen.IsZero() {
		return -1
	}
	age := now.Sub(seen).Seconds()
	if age < 0 {
		return 0
	}
	return roundTo1(age)
}

// pathAge is the single computation of how stale a snapshot path is, shared
// between buildGaugeValuesPayload and every derived path that reads through
// the same alarmReader (ADR 0083), so a widget bound directly to a path and
// a derived figure computed from that same path can never disagree about
// its age. Takes the snapshot explicitly, the same way alarmReader itself
// does, rather than reaching for the global: a derived-path computation
// tested against a local snapshot must resolve the node from that same
// snapshot, not whatever globalSignalKSnapshot happens to hold.
//
// The node's own SignalK timestamp is preferred over the alarm engine's
// pathSeen record. signalk_stream.go's own comment already notes that the
// server replays its whole retained model on every subscribe, and that
// happens on an ordinary reconnect as much as a backend restart: applyDelta
// resets pathSeen to the arrival time for every path a replay touches, dead
// ones included, but the replayed delta still carries the source's original
// declared timestamp into the node. Only the node timestamp survives that
// replay telling the truth, so it wins whenever the path has one. pathSeen
// is used only when the node carries no timestamp at all -- the same
// "arrival time is all the evidence there is" case ADR 0068 already accepted
// for the Solar and Battery & Power tiles it fed directly from REST.
func pathAge(snapshot *signalKSnapshot, sample alarmSample, path string, now time.Time) float64 {
	if nodeAge := freshestTimestampAge(snapshot.nodeAt(path), now); nodeAge >= 0 {
		return nodeAge
	}
	return alarmSampleAge(sample, now)
}

// buildGaugeValuesPayload pushes the current value of every path a gauge is
// bound to. The backend owns the page config, so it can work out what to send
// without the browser subscribing to anything.
//
// It also carries "ages" (ADR 0083): seconds since each path's last update,
// computed against the vessel clock once per build so nothing here depends
// on the browser's clock. Every widget bound through gauge-values -- a
// standalone gauge, a group, a cluster, a lamp, the ribbon -- reads the same
// map, rather than each source growing its own bespoke age the way Solar and
// Battery & Power did before this (ADR 0068).
func buildGaugeValuesPayload() map[string]any {
	paths := gaugeBoundPaths()
	values := make(map[string]any, len(paths))
	ages := make(map[string]float64, len(paths))

	now := time.Now().UTC()

	// One self-tree copy shared by computeDerivedPathsFromTree and the
	// snapshot reader below, instead of each fetching its own -- this used to
	// cost two whole-tree copies on every build (backend-perf-audit.md
	// Tier 1 #2), once per second via the hub even before this fix.
	context := globalSignalKSnapshot.selfContext()
	tree := globalSignalKSnapshot.selfTree()
	derivedValues, derivedAges := computeDerivedPathsFromTree(globalSignalKSnapshot, context, tree, now)

	if len(paths) > 0 {
		read := alarmReaderFromTree(globalSignalKSnapshot, context, tree)
		for _, path := range paths {
			// A derived path is computed rather than looked up, but rides the
			// same event so every widget binds it the same way.
			if isDerivedPath(path) {
				if value := derivedValues[path]; value != nil {
					values[path] = *value
				} else {
					values[path] = nil
				}
				ages[path] = derivedAges[path]
				continue
			}

			sample := read(path)
			if !sample.Present {
				// Absent stays absent: a gauge must render the structural dash
				// rather than a zero it would be read as a real measurement.
				values[path] = nil
			} else {
				values[path] = sample.Value
			}

			// -1 when neither the node nor pathSeen has anything, or the
			// path is absent.
			ages[path] = pathAge(globalSignalKSnapshot, sample, path, now)
		}
	}

	return map[string]any{"values": values, "ages": ages}
}
