package main

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// signalKSnapshot holds the current state of SignalK data as a nested tree,
// built by reassembling delta messages. It is thread-safe and provides a
// snapshot that remains compatible with the existing lookupString/lookupNumber/
// lookupBool functions.
type signalKSnapshot struct {
	mu          sync.RWMutex
	contexts    map[string]map[string]any // context string → nested tree
	pathSeen    map[string]time.Time      // "<context>|<dotted path>" → last update time
	selfCtx     string                    // which context is this vessel, per the stream's hello frame
	connected   bool
	lastMessage time.Time
}

// vesselContextPrefix separates vessel contexts from the other trees a
// subscribe=all stream carries (atons, aircraft, meteo).
const vesselContextPrefix = "vessels."

// signalKDelta is a SignalK delta message received over the WebSocket stream.
type signalKDelta struct {
	// omitempty on both string fields is load-bearing for publishing
	// (signalk_publish.go), not cosmetic: signalk-server silently DROPS a delta
	// carrying "context": "", while one that omits the key is filed under the
	// sending connection's own vessel. Without this the socket accepts the
	// frame and the server discards it, which is the hardest kind of failure to
	// see. Unmarshalling is unaffected — absent and empty both decode to "".
	Context string          `json:"context,omitempty"`
	Updates []signalKUpdate `json:"updates"`

	// Self is only populated on the server's opening hello frame, which names
	// this vessel's context and carries no updates. It is server-to-client
	// only, so it must never appear on a published delta.
	Self string `json:"self,omitempty"`
}

// signalKUpdate is an update within a delta message, containing timestamp,
// source, and one or more values.
type signalKUpdate struct {
	Source    map[string]any `json:"source,omitempty"`
	SourceRef string         `json:"$source,omitempty"`
	Timestamp string         `json:"timestamp,omitempty"`
	Values    []signalKValue `json:"values"`
}

// signalKValue is a single path-value pair within an update.
type signalKValue struct {
	Path  string `json:"path"`
	Value any    `json:"value"`
}

// newSignalKSnapshot creates a new empty snapshot.
func newSignalKSnapshot() *signalKSnapshot {
	return &signalKSnapshot{
		contexts: make(map[string]map[string]any),
		pathSeen: make(map[string]time.Time),
	}
}

// applyDelta merges a delta message into the snapshot's tree. Deltas carry flat
// dotted paths; they are reassembled into the nested shape the SignalK REST tree
// has so lookupString/lookupNumber/lookupBool keep working unchanged.
//
// now is the receive time, recorded against every path in the delta so stale()
// can report a sensor that stopped reporting. It is a parameter rather than an
// internal time.Now() so callers can drive it deterministically, matching stale().
func (s *signalKSnapshot) applyDelta(d signalKDelta, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.contexts[d.Context]; !ok {
		s.contexts[d.Context] = make(map[string]any)
	}
	tree := s.contexts[d.Context]

	for _, update := range d.Updates {
		for _, val := range update.Values {
			s.pathSeen[d.Context+"|"+val.Path] = now

			// An empty path carries top-level scalars (e.g. name) which the REST
			// tree presents unwrapped, so they must not gain a "value" key.
			if val.Path == "" {
				if valMap, ok := val.Value.(map[string]any); ok {
					for k, v := range valMap {
						tree[k] = v
					}
				}
				continue
			}

			segments := strings.Split(val.Path, ".")
			current := tree
			for i, segment := range segments {
				child, ok := current[segment].(map[string]any)
				if !ok {
					// Either absent, or a scalar left by an empty-path merge that
					// a later delta has revealed to be a branch. Replacing is the
					// only way to keep the tree walkable.
					child = make(map[string]any)
					current[segment] = child
				}

				if i < len(segments)-1 {
					current = child
					continue
				}

				child["value"] = val.Value
				if update.Timestamp != "" {
					child["timestamp"] = update.Timestamp
				}
				if update.SourceRef != "" {
					child["$source"] = update.SourceRef
				}
			}
		}
	}

	s.lastMessage = now
}

// treeFor returns a deep copy of the tree for the given context, or nil if
// the context is unknown. The returned map can be safely mutated without
// affecting the snapshot.
func (s *signalKSnapshot) treeFor(context string) map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tree, ok := s.contexts[context]
	if !ok {
		return nil
	}

	// Handlers read this concurrently with the stream writing; returning the
	// live map would race.
	return deepCopyMap(tree)
}

// deepCopyMap creates a deep copy of a map[string]any, recursively copying
// nested maps and slices.
func deepCopyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}

	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = deepCopyValue(v)
	}
	return result
}

// deepCopyValue recursively copies a value, handling maps, slices, and scalars.
func deepCopyValue(v any) any {
	switch typedV := v.(type) {
	case map[string]any:
		return deepCopyMap(typedV)
	case []any:
		result := make([]any, len(typedV))
		for i, item := range typedV {
			result[i] = deepCopyValue(item)
		}
		return result
	default:
		// Scalars (string, float64, bool, nil, etc.) copy by value
		return v
	}
}

// setSelfContext records which context is this vessel. Deltas carry the real
// context ("vessels.urn:mrn:..."), never the literal "vessels.self", so without
// this the self tree is unreachable. Some servers report self unprefixed.
func (s *signalKSnapshot) setSelfContext(context string) {
	trimmed := strings.TrimSpace(context)
	if trimmed == "" {
		return
	}
	if !strings.HasPrefix(trimmed, vesselContextPrefix) {
		trimmed = vesselContextPrefix + trimmed
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.selfCtx = trimmed
}

func (s *signalKSnapshot) selfContext() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.selfCtx
}

// selfTree stands in for GET /signalk/v1/api/vessels/self. It deliberately
// takes the two locks in sequence rather than nesting them: recursive RLock
// deadlocks whenever a writer is queued between the two acquisitions.
func (s *signalKSnapshot) selfTree() map[string]any {
	self := s.selfContext()
	if self == "" {
		return nil
	}
	return s.treeFor(self)
}

// nodeAt resolves a dotted SignalK path to its leaf node within the self
// tree -- the map carrying "value" and, when the source declared one, "meta"
// -- so a caller can read metadata about a path (its unit, say) rather than
// only the value snapshotAlarmReader hands back. Returns nil for an unknown
// path or before the self tree exists, the same "absent, not a guess" answer
// selfTree itself gives.
func (s *signalKSnapshot) nodeAt(path string) map[string]any {
	tree := s.selfTree()
	if tree == nil {
		return nil
	}

	segments := strings.Split(strings.TrimSpace(path), ".")
	if len(segments) == 0 || segments[0] == "" {
		return nil
	}

	var current any = tree
	for _, segment := range segments {
		asMap, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		next, ok := asMap[segment]
		if !ok {
			return nil
		}
		current = next
	}

	node, ok := current.(map[string]any)
	if !ok {
		return nil
	}
	return node
}

// vesselsTree stands in for GET /signalk/v1/api/vessels, which is keyed by bare
// vessel id rather than by delta context.
func (s *signalKSnapshot) vesselsTree() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	vessels := make(map[string]any, len(s.contexts))
	for context, tree := range s.contexts {
		if !strings.HasPrefix(context, vesselContextPrefix) {
			continue
		}
		vessels[strings.TrimPrefix(context, vesselContextPrefix)] = deepCopyMap(tree)
	}
	return vessels
}

// stale returns true if the path was never seen for the context, or if
// now.Sub(seen) > maxAge. The caller must pass now explicitly so tests
// can be deterministic.
func (s *signalKSnapshot) stale(context, path string, maxAge time.Duration, now time.Time) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pathKey := context + "|" + path
	seen, ok := s.pathSeen[pathKey]
	if !ok {
		return true
	}

	return now.Sub(seen) > maxAge
}

// setConnected sets the connected status.
func (s *signalKSnapshot) setConnected(connected bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.connected = connected
}

// status returns the current connected status and the time of the last
// message received.
func (s *signalKSnapshot) status() (connected bool, lastMessage time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.connected, s.lastMessage
}

// knownContexts returns a sorted slice of all contexts that have received
// at least one delta. If no contexts have been applied, returns an empty slice.
func (s *signalKSnapshot) knownContexts() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.contexts) == 0 {
		return []string{}
	}

	result := make([]string, 0, len(s.contexts))
	for ctx := range s.contexts {
		result = append(result, ctx)
	}

	sort.Strings(result)
	return result
}

// lastSeen reports when a path last carried an update, or the zero time if it
// never has. Alarm rules use it to tell a live sensor from a frozen one.
func (s *signalKSnapshot) lastSeen(context, path string) time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pathSeen[context+"|"+path]
}

// reconcileNotifications replaces the notifications subtree held for context
// with the server's REST copy, keeping any leaf a delta updated at or after
// readStartedAt (ADR 0086).
//
// Nothing else ever re-reads the server: the snapshot is fed only by the
// delta stream (signalk_stream.go), so a notification the server has
// forgotten -- a restart that emptied its NotificationManager, a clear the
// instant+minPeriod stream dropped inside one debounce window, or its
// 60-120s clean() sweep -- stays live here until that exact path happens to
// change again. This is the periodic correction against the REST tree that
// closes that gap. server == nil means the server holds no notifications for
// that context at all (a 404).
//
// Returns the dotted notification paths (relative to "notifications") whose
// leaf was removed or whose liveness (notificationValueIsLive) changed,
// sorted, so the caller can log exactly what it corrected.
func (s *signalKSnapshot) reconcileNotifications(context string, server map[string]any, readStartedAt time.Time) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	tree, ok := s.contexts[context]
	if !ok {
		// The syncer only ever asks about contexts it already holds; this must
		// never manufacture one.
		return nil
	}

	ours := map[string]map[string]any{}
	if root, ok := tree[notificationsRoot].(map[string]any); ok {
		flattenNotificationLeaves(root, nil, ours)
	}
	serverLeaves := map[string]map[string]any{}
	flattenNotificationLeaves(server, nil, serverLeaves)

	paths := make(map[string]bool, len(ours)+len(serverLeaves))
	for path := range ours {
		paths[path] = true
	}
	for path := range serverLeaves {
		paths[path] = true
	}

	kept := map[string]map[string]any{}
	var changed []string

	for path := range paths {
		key := context + "|" + notificationsRoot + "." + path
		seen := s.pathSeen[key]
		ourLeaf, weHold := ours[path]
		serverLeaf, serverHas := serverLeaves[path]

		// A delta that arrived after the read began is newer than anything the
		// read can say, so our copy wins regardless of what the server answered.
		if !seen.IsZero() && !seen.Before(readStartedAt) {
			if weHold {
				kept[path] = ourLeaf
			}
			continue
		}

		if serverHas {
			newLeaf := deepCopyMap(serverLeaf)
			kept[path] = newLeaf
			s.pathSeen[key] = readStartedAt

			oldLive := weHold && leafNotificationIsLive(ourLeaf)
			if oldLive != leafNotificationIsLive(newLeaf) {
				changed = append(changed, path)
			}
			continue
		}

		// The server no longer has this leaf at all: it is gone, whether or not
		// it was live. paths only ever contains this branch when weHold is true
		// (otherwise neither side carries the leaf and it was never unioned in).
		delete(s.pathSeen, key)
		changed = append(changed, path)
	}

	if len(kept) == 0 {
		delete(tree, notificationsRoot)
	} else {
		root := map[string]any{}
		for path, leaf := range kept {
			setNotificationLeaf(root, strings.Split(path, "."), leaf)
		}
		tree[notificationsRoot] = root
	}

	sort.Strings(changed)
	return changed
}

// flattenNotificationLeaves walks a notifications subtree, collecting every
// leaf -- a map node carrying a "value" key, any value including nil -- keyed
// by its dotted path relative to the walk's root. A branch node's own child
// that is a map without a "value" key (typically a schema "meta" block
// describing the branch) is descended into like any other child and
// contributes no leaf, which is exactly what dropping it means.
func flattenNotificationLeaves(node map[string]any, prefix []string, out map[string]map[string]any) {
	if node == nil {
		return
	}
	if _, ok := node["value"]; ok {
		out[strings.Join(prefix, ".")] = node
		return
	}
	for key, child := range node {
		asMap, ok := child.(map[string]any)
		if !ok {
			continue
		}
		flattenNotificationLeaves(asMap, append(append([]string{}, prefix...), key), out)
	}
}

// setNotificationLeaf writes leaf at a dotted path within root, creating
// branch maps as it goes -- the same walk applyDelta uses to reassemble a
// flat delta path into the nested REST shape.
func setNotificationLeaf(root map[string]any, segments []string, leaf map[string]any) {
	current := root
	for i, segment := range segments {
		if i == len(segments)-1 {
			current[segment] = leaf
			return
		}
		child, ok := current[segment].(map[string]any)
		if !ok {
			child = make(map[string]any)
			current[segment] = child
		}
		current = child
	}
}

// leafNotificationIsLive reports whether a flattened notification leaf (the
// map carrying "value" plus timestamp/$source/meta) is currently live,
// tolerating a leaf whose "value" is nil or not a map -- notificationValueIsLive
// requires a map to read "state" off.
func leafNotificationIsLive(leaf map[string]any) bool {
	value, ok := leaf["value"].(map[string]any)
	if !ok {
		return false
	}
	return notificationValueIsLive(value)
}
