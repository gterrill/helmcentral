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
	Context string          `json:"context"`
	Updates []signalKUpdate `json:"updates"`

	// Self is only populated on the server's opening hello frame, which names
	// this vessel's context and carries no updates.
	Self string `json:"self"`
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
