package main

import (
	"sort"
	"strings"
	"time"
)

// telemetryReader is how the backend reads our own boat's live telemetry
// (ADR 0178). Paths are plain SignalK dotted paths, never ending in
// ".value": the reader unwraps SignalK's {value, timestamp, $source, meta}
// leaves itself. Nothing it returns is a sentinel. A path that is missing,
// or holds the wrong type, reports ok=false; an age with no evidence behind
// it reports AgeKnown=false. A real reading of -1 is a reading.
//
// Values are SI exactly as SignalK stores them. The reader resolves SignalK
// paths only, never helmcentral.* derived paths (those are computed from
// this reader), and it holds no staleness thresholds: consumers decide what
// is too old.
//
// Two adapters satisfy it: *telemetryView over the delta-stream snapshot,
// and fakeTelemetry in the tests.
type telemetryReader interface {
	// Number reads a numeric value. It accepts the leaf at path, a bare
	// value at path, or a field inside an object leaf on the way to path
	// ("design.length.overall" inside design.length's value).
	Number(path string) (telemetryNumber, bool)
	// Value reads whatever value path holds, of any type, resolved the
	// same way as Number.
	Value(path string) (any, bool)
	// String reads a string value, resolved the same way as Number.
	String(path string) (string, bool)
	// Bool reads a boolean value, resolved the same way as Number. A number
	// reads as true when it is not zero, the way SignalK switches publish.
	Bool(path string) (bool, bool)
	// Position reads navigation.position's raw latitude and longitude. It
	// does not judge the fix: the GNSS trust gate (gnss_validation.go)
	// decides whether a position can be shown.
	Position() (telemetryPosition, bool)
	// Sample reads the leaf at exactly path, whatever its value's type,
	// null included. A bare value or a field inside an object leaf is not a
	// leaf here: this is the form a gauge or an alarm rule binds.
	Sample(path string) telemetrySample
	// Timestamp is the newest source timestamp declared anywhere at or
	// under path. It never falls back to arrival time, for the consumers
	// that judge a whole subtree (the Wind tile, the Tanks tile) by the
	// source's own clock.
	Timestamp(path string) (time.Time, bool)
	// Children lists, sorted, the keys under path that are themselves
	// branches or leaves: battery, engine, tank and controller instances.
	Children(path string) []string
	// Paths lists every leaf for the path picker (collectSignalKPaths).
	Paths() []signalKPath
}

// telemetryNumber is one numeric reading and how old it is.
type telemetryNumber struct {
	Value    float64
	Age      time.Duration
	AgeKnown bool
	Source   string
}

// telemetryPosition is navigation.position's raw fix and how old it is.
type telemetryPosition struct {
	Latitude  float64
	Longitude float64
	Age       time.Duration
	AgeKnown  bool
	Source    string
}

// telemetrySample is one leaf as Sample reads it. Present means a leaf
// exists at the path; its Value may still be null or any JSON type.
type telemetrySample struct {
	Value    any
	Present  bool
	Age      time.Duration
	AgeKnown bool
	Source   string
	Units    string
	// Timestamp is the source timestamp the leaf declares on itself, as
	// written, "" when it declares none.
	Timestamp string
	// Received is when the leaf last arrived on the delta stream, zero if
	// it never has. Age already folds it in; it is exposed for the alarm
	// engine, whose stale-data check has always timed arrival alone
	// (alarmSample.LastSeen).
	Received time.Time
}

// telemetryView is a point-in-time view of our own boat over the snapshot.
//
// selfTelemetry takes one consistent copy of the self tree, so every value a
// fetcher reads belongs to the same instant. liveSelfTelemetry copies
// nothing up front: each read resolves its path under the snapshot's read
// lock and copies only what it returns, for the per-tick readers that touch
// a handful of leaves and must never pay for a whole-tree copy.
type telemetryView struct {
	tree     map[string]any
	live     bool
	snapshot *signalKSnapshot
	context  string
	now      time.Time
	hasSelf  bool
}

var _ telemetryReader = (*telemetryView)(nil)

// selfTelemetry is selfTelemetryAt the current time.
func (s *signalKSnapshot) selfTelemetry() *telemetryView {
	return s.selfTelemetryAt(time.Now().UTC())
}

// selfTelemetryAt copies the self tree once and ages every read against now.
// Before the server's hello frame there is no self tree: HasSelf reports
// false and every read reports absent.
func (s *signalKSnapshot) selfTelemetryAt(now time.Time) *telemetryView {
	view := &telemetryView{snapshot: s, now: now}
	view.context = s.selfContext()
	if view.context == "" {
		return view
	}
	view.tree = s.treeFor(view.context)
	view.hasSelf = view.tree != nil
	return view
}

// liveSelfTelemetry reads straight from the snapshot, one path at a time.
// Two reads may straddle a delta; use selfTelemetry when they must agree.
func (s *signalKSnapshot) liveSelfTelemetry(now time.Time) *telemetryView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.contexts[s.selfCtx]
	return &telemetryView{live: true, snapshot: s, context: s.selfCtx, now: now, hasSelf: s.selfCtx != "" && exists}
}

// HasSelf reports whether the self tree existed when the view was taken.
func (t *telemetryView) HasSelf() bool { return t.hasSelf }

// arrivalFunc reports when a path last arrived on the delta stream, zero if
// never.
type arrivalFunc func(path string) time.Time

// withRoot runs fn against the self tree, with arrival times at hand. In
// live mode fn runs under the snapshot's read lock, taken once for the tree
// walk and the arrival lookups together, so fn must not call back into the
// snapshot (a recursive RLock deadlocks behind a queued writer) and must
// copy anything it lets escape. A copied view takes the lock only when fn
// asks for an arrival time.
func (t *telemetryView) withRoot(fn func(root map[string]any, arrived arrivalFunc)) {
	if !t.live {
		fn(t.tree, t.arrivedUnlocked)
		return
	}
	t.snapshot.mu.RLock()
	defer t.snapshot.mu.RUnlock()
	fn(t.snapshot.contexts[t.context], func(path string) time.Time {
		if t.context == "" {
			return time.Time{}
		}
		return t.snapshot.pathSeen[t.context+"|"+path]
	})
}

func (t *telemetryView) escape(v any) any {
	if t.live {
		return deepCopyValue(v)
	}
	return v
}

// arrivedUnlocked is a copied view's arrival lookup, zero when the view has
// no snapshot behind it.
func (t *telemetryView) arrivedUnlocked(path string) time.Time {
	if t.snapshot == nil || t.context == "" {
		return time.Time{}
	}
	return t.snapshot.lastSeen(t.context, path)
}

// telemetryAge is the one age rule (ADR 0083), used by every read in this
// file and by pathAge: the node's own newest source timestamp when it
// declares one, since that survives a reconnect replay that resets arrival
// time; arrival time when it declares none; unknown when there is neither.
func telemetryAge(stamp time.Time, stamped bool, arrived, now time.Time) (time.Duration, bool) {
	if stamped {
		return clampAge(now.Sub(stamp)), true
	}
	return arrivalAge(arrived, now)
}

func arrivalAge(arrived, now time.Time) (time.Duration, bool) {
	if arrived.IsZero() {
		return 0, false
	}
	return clampAge(now.Sub(arrived)), true
}

// clampAge treats a source clock marginally ahead of ours as current.
func clampAge(d time.Duration) time.Duration {
	if d < 0 {
		return 0
	}
	return d
}

// telemetryAgeSeconds renders an age the way the payloads carry it: seconds
// to one decimal, or -1 for unknown where the browser contract still
// expects that sentinel.
func telemetryAgeSeconds(age time.Duration, known bool) float64 {
	if !known {
		return -1
	}
	return roundTo1(age.Seconds())
}

// newestTimestamp finds the most recent RFC3339 "timestamp" anywhere in the
// subtree.
func newestTimestamp(node any) (time.Time, bool) {
	newest := time.Time{}

	var walk func(any)
	walk = func(current any) {
		entry, ok := current.(map[string]any)
		if !ok {
			return
		}
		for key, child := range entry {
			if key == "timestamp" {
				raw, isString := child.(string)
				if !isString {
					continue
				}
				parsed, err := time.Parse(time.RFC3339, raw)
				if err == nil && parsed.After(newest) {
					newest = parsed.UTC()
				}
				continue
			}
			walk(child)
		}
	}
	walk(node)

	return newest, !newest.IsZero()
}

func telemetrySegments(path string) []string {
	segments := strings.Split(strings.TrimSpace(path), ".")
	if len(segments) == 0 || segments[0] == "" {
		return nil
	}
	return segments
}

// walkTelemetry follows segments from root, returning whatever sits there.
func walkTelemetry(root map[string]any, segments []string) (any, bool) {
	if root == nil || len(segments) == 0 {
		return nil, false
	}
	var current any = root
	for _, segment := range segments {
		asMap, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := asMap[segment]
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

// resolvedValue is a value found by resolveTelemetryValue and the leaf that
// owns it, whose timestamp and arrival time date it.
type resolvedValue struct {
	value     any
	owner     map[string]any // nil for a bare value
	ownerPath string
}

// resolveTelemetryValue finds the value a dotted path names, trying in turn
// the leaf at path (its "value"), a bare value at path, and a field inside
// an object leaf on the way (navigation.position's latitude).
func resolveTelemetryValue(root map[string]any, segments []string) (resolvedValue, bool) {
	if node, ok := walkTelemetry(root, segments); ok {
		if asMap, isMap := node.(map[string]any); isMap {
			if value, hasValue := asMap["value"]; hasValue {
				return resolvedValue{value: value, owner: asMap, ownerPath: strings.Join(segments, ".")}, true
			}
			return resolvedValue{}, false
		}
		return resolvedValue{value: node, ownerPath: strings.Join(segments, ".")}, true
	}

	var current any = root
	for i, segment := range segments {
		asMap, ok := current.(map[string]any)
		if !ok {
			return resolvedValue{}, false
		}
		if next, ok := asMap[segment]; ok {
			current = next
			continue
		}
		inner, ok := asMap["value"].(map[string]any)
		if !ok || i == 0 {
			return resolvedValue{}, false
		}
		value, ok := walkTelemetry(inner, segments[i:])
		if !ok {
			return resolvedValue{}, false
		}
		return resolvedValue{value: value, owner: asMap, ownerPath: strings.Join(segments[:i], ".")}, true
	}
	return resolvedValue{}, false
}

// resolved is one flexible read, with everything copied out of the tree and
// its age already worked out from what was gathered under the one lock.
type resolved struct {
	value    any
	source   string
	age      time.Duration
	ageKnown bool
}

// resolve reads path; withAge says whether the caller wants the age, so a
// value-only read never looks up an arrival time.
func (t *telemetryView) resolve(path string, withAge bool) (resolved, bool) {
	segments := telemetrySegments(path)
	if segments == nil {
		return resolved{}, false
	}
	var out resolved
	var ok bool
	var stamp, arrival time.Time
	var stamped bool
	t.withRoot(func(root map[string]any, arrived arrivalFunc) {
		var found resolvedValue
		found, ok = resolveTelemetryValue(root, segments)
		if !ok {
			return
		}
		out.value = t.escape(found.value)
		if found.owner != nil {
			out.source, _ = found.owner["$source"].(string)
			stamp, stamped = newestTimestamp(found.owner)
		}
		if withAge && !stamped {
			arrival = arrived(found.ownerPath)
		}
	})
	if ok && withAge {
		out.age, out.ageKnown = telemetryAge(stamp, stamped, arrival, t.now)
	}
	return out, ok
}

func numericTelemetryValue(raw any) (float64, bool) {
	switch v := raw.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	}
	return 0, false
}

func boolTelemetryValue(raw any) (bool, bool) {
	switch v := raw.(type) {
	case bool:
		return v, true
	case float64:
		return v != 0, true
	}
	return false, false
}

func (t *telemetryView) Number(path string) (telemetryNumber, bool) {
	r, ok := t.resolve(path, true)
	if !ok {
		return telemetryNumber{}, false
	}
	value, ok := numericTelemetryValue(r.value)
	if !ok {
		return telemetryNumber{}, false
	}
	return telemetryNumber{Value: value, Age: r.age, AgeKnown: r.ageKnown, Source: r.source}, true
}

func (t *telemetryView) Value(path string) (any, bool) {
	r, ok := t.resolve(path, false)
	return r.value, ok
}

func (t *telemetryView) String(path string) (string, bool) {
	r, ok := t.resolve(path, false)
	if !ok {
		return "", false
	}
	s, ok := r.value.(string)
	return s, ok
}

func (t *telemetryView) Bool(path string) (bool, bool) {
	r, ok := t.resolve(path, false)
	if !ok {
		return false, false
	}
	return boolTelemetryValue(r.value)
}

func (t *telemetryView) Position() (telemetryPosition, bool) {
	lat, latOK := t.resolve("navigation.position.latitude", true)
	lon, lonOK := t.resolve("navigation.position.longitude", false)
	if !latOK || !lonOK {
		return telemetryPosition{}, false
	}
	latitude, latOK := numericTelemetryValue(lat.value)
	longitude, lonOK := numericTelemetryValue(lon.value)
	if !latOK || !lonOK {
		return telemetryPosition{}, false
	}
	return telemetryPosition{Latitude: latitude, Longitude: longitude, Age: lat.age, AgeKnown: lat.ageKnown, Source: lat.source}, true
}

func (t *telemetryView) Sample(path string) telemetrySample {
	segments := telemetrySegments(path)
	if segments == nil {
		return telemetrySample{}
	}
	var sample telemetrySample
	var stamp time.Time
	var stamped bool
	t.withRoot(func(root map[string]any, arrived arrivalFunc) {
		node, ok := walkTelemetry(root, segments)
		if !ok {
			return
		}
		leaf, ok := node.(map[string]any)
		if !ok {
			return
		}
		value, ok := leaf["value"]
		if !ok {
			return
		}
		sample.Present = true
		sample.Value = t.escape(value)
		sample.Source, _ = leaf["$source"].(string)
		sample.Timestamp, _ = leaf["timestamp"].(string)
		sample.Units = unitsFor(leaf)
		stamp, stamped = newestTimestamp(leaf)
		sample.Received = arrived(path)
	})
	if !sample.Present {
		return telemetrySample{}
	}
	sample.Age, sample.AgeKnown = telemetryAge(stamp, stamped, sample.Received, t.now)
	return sample
}

func (t *telemetryView) Timestamp(path string) (time.Time, bool) {
	segments := telemetrySegments(path)
	if segments == nil {
		return time.Time{}, false
	}
	var stamp time.Time
	var ok bool
	t.withRoot(func(root map[string]any, _ arrivalFunc) {
		node, found := walkTelemetry(root, segments)
		if !found {
			return
		}
		stamp, ok = newestTimestamp(node)
	})
	return stamp, ok
}

func (t *telemetryView) Children(path string) []string {
	segments := telemetrySegments(path)
	if segments == nil {
		return nil
	}
	var out []string
	t.withRoot(func(root map[string]any, _ arrivalFunc) {
		node, ok := walkTelemetry(root, segments)
		if !ok {
			return
		}
		branch, ok := node.(map[string]any)
		if !ok {
			return
		}
		for key, child := range branch {
			if _, isMap := child.(map[string]any); isMap {
				out = append(out, key)
			}
		}
	})
	sort.Strings(out)
	return out
}

// Paths walks a copy: in live mode the tree is copied under the read lock
// and walked and sorted outside it, so the delta stream is not held up for
// the walk.
func (t *telemetryView) Paths() []signalKPath {
	root := t.tree
	if t.live {
		root = t.snapshot.treeFor(t.context)
	}
	if root == nil {
		return nil
	}
	return collectSignalKPaths(root)
}

// unitsAt is SignalK's own unit declaration (meta.units) on whatever node
// sits at path, leaf or not, "" when it declares none.
func (t *telemetryView) unitsAt(path string) string {
	var units string
	if segments := telemetrySegments(path); segments != nil {
		t.withRoot(func(root map[string]any, _ arrivalFunc) {
			if node, ok := walkTelemetry(root, segments); ok {
				if asMap, isMap := node.(map[string]any); isMap {
					units = unitsFor(asMap)
				}
			}
		})
	}
	return units
}

// ageAt applies the age rule to whatever node sits at path, leaf or branch,
// with arrived as the arrival-time evidence (pathAge's contract).
func (t *telemetryView) ageAt(path string, arrived time.Time, now time.Time) (time.Duration, bool) {
	var stamp time.Time
	var stamped bool
	if segments := telemetrySegments(path); segments != nil {
		t.withRoot(func(root map[string]any, _ arrivalFunc) {
			if node, ok := walkTelemetry(root, segments); ok {
				if _, isMap := node.(map[string]any); isMap {
					stamp, stamped = newestTimestamp(node)
				}
			}
		})
	}
	return telemetryAge(stamp, stamped, arrived, now)
}
