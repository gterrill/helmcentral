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
//
// unitOf resolves each leaf's unit (nil: the node's own meta.units only).
func collectSignalKPaths(tree map[string]any, unitOf func(path string, node map[string]any) string) []signalKPath {
	if unitOf == nil {
		unitOf = func(_ string, node map[string]any) string { return unitsFor(node) }
	}
	var out []signalKPath
	walkSignalKPaths(tree, nil, 0, &out, unitOf)

	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func walkSignalKPaths(node map[string]any, prefix []string, depth int, out *[]signalKPath, unitOf func(path string, node map[string]any) string) {
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

		entry := signalKPath{Path: strings.Join(prefix, ".")}
		entry.Units = unitOf(entry.Path, node)
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
		walkSignalKPaths(asMap, append(append([]string{}, prefix...), key), depth+1, out, unitOf)
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
	if published := globalSignalKSnapshot.liveSelfTelemetry(time.Now().UTC()).Paths(); published != nil {
		paths = published
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

	// The anomaly engine-differential paths are dynamic (one per configured
	// engine per quantity), so they cannot live in derivedPathIDs' static
	// list the way every other derived path does.
	vessel, err := loadVesselView(getEnv("SETTINGS_FILE", "../settings.yaml"))
	if err == nil {
		for _, path := range anomalyEngineResidualPathIDs(vessel.Engines) {
			entry := signalKPath{Path: path, Units: unitForAlarmPath(globalSignalKSnapshot, path)}
			if value := derivedValues[path]; value != nil {
				entry.Value = value
			}
			paths = append(paths, entry)
		}
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
	// anything for this path - telemetryAge's own "no evidence"
	// contract), never a value that reads as freshly measured.
	AgeSeconds float64
}

// pathSamples walks the view exactly like collectSignalKPaths, but keeps
// $source/timestamp per leaf and ages each leaf by the one age rule
// (telemetryAge), instead of discarding both the way the tile path picker
// always has. With no self tree yet the result is simply empty.
func (t *telemetryView) pathSamples() []signalKPathSample {
	type found struct {
		sample  signalKPathSample
		stamp   time.Time
		stamped bool
		arrival time.Time
	}
	var leaves []found
	t.withRoot(func(root map[string]any, arrived arrivalFunc) {
		var walk func(node map[string]any, prefix []string, depth int)
		walk = func(node map[string]any, prefix []string, depth int) {
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
				stamp, stamped := newestTimestamp(node)
				var arrival time.Time
				if !stamped {
					arrival = arrived(path)
				}
				leaves = append(leaves, found{
					sample: signalKPathSample{
						Path:      path,
						Value:     t.escape(raw),
						Units:     t.unitOf(path, node),
						Source:    source,
						Timestamp: timestamp,
					},
					stamp:   stamp,
					stamped: stamped,
					arrival: arrival,
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
				walk(asMap, append(append([]string{}, prefix...), key), depth+1)
			}
		}
		if root != nil {
			walk(root, nil, 0)
		}
	})

	out := make([]signalKPathSample, 0, len(leaves))
	for _, leaf := range leaves {
		leaf.sample.AgeSeconds = telemetryAgeSeconds(telemetryAge(leaf.stamp, leaf.stamped, leaf.arrival, t.now))
		out = append(out, leaf.sample)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// pathAge is the single computation of how stale a snapshot path is, shared
// between buildGaugeValuesPayload and every derived path that reads through
// the same alarmReader (ADR 0083), so a tile bound directly to a path and
// a derived figure computed from that same path can never disagree about
// its age. It applies the view's one age rule (telemetryAge) to whatever
// node sits at path, with the sample's own arrival time as the fallback
// evidence. Takes the view explicitly, the same view the alarmReader was
// built from, rather than reaching for the global: a derived-path
// computation tested against a local snapshot must resolve the node from
// that same snapshot, not whatever globalSignalKSnapshot happens to hold.
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
//
// -1 is the gauge-values payload's "unknown" on the wire.
func pathAge(t *telemetryView, sample alarmSample, path string, now time.Time) float64 {
	return telemetryAgeSeconds(t.ageAt(path, sample.LastSeen, now))
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

	// One self-tree copy shared by computeDerivedPathsWithStates and the
	// snapshot reader below, instead of each fetching its own -- this used to
	// cost two whole-tree copies on every build (backend-perf-audit.md
	// Tier 1 #2), once per second via the hub even before this fix.
	telemetry := globalSignalKSnapshot.selfTelemetryAt(now)
	engineStateOf := memoEngineStates(globalSignalKSnapshot, now)
	derivedValues, derivedAges := computeDerivedPathsWithStates(telemetry, engineStateOf, now)

	if len(paths) > 0 {
		read := alarmReaderFromView(telemetry)
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

			// A switched-off engine's paths carry no value and no age, the way
			// the Engine Cluster tile shows them, so a gauge, group or lamp
			// bound to one agrees with it instead of marking a frozen reading
			// STALE.
			if id := propulsionEngineID(path); id != "" && engineStateOf()[id].State == engineStateOff {
				values[path] = nil
				ages[path] = -1
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
			ages[path] = pathAge(telemetry, sample, path, now)
		}
	}

	payload := map[string]any{"values": values, "ages": ages}
	// Whether each engine is running, switched off or lost (engine_state.go),
	// so the Engine Cluster tile can say OFF instead of STALE.
	if engines := engineStatesJSON(engineStateOf()); len(engines) > 0 {
		payload["engines"] = engines
	}
	return payload
}
