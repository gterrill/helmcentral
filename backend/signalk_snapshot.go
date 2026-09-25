package main

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// signalKSnapshot holds the current state of SignalK data as a nested tree,
// built by reassembling delta messages. It is thread-safe and provides a
// snapshot that remains compatible with the existing lookupString/lookupNumber/
// lookupBool functions.
type signalKSnapshot struct {
	mu          sync.RWMutex
	contexts    map[string]map[string]any  // context string → nested tree
	pathSeen    map[string]time.Time       // "<context>|<dotted path>" → last update time
	sourceSeen  map[string]sourceSeenEntry // "<context>|<$source>" → publishing history
	selfCtx     string                     // which context is this vessel, per the stream's hello frame
	connected   bool
	lastMessage time.Time
}

// sourceSeenEntry tracks one $source's publishing history within a context:
// enough for the sensor-health "silent source" check (anomaly_sensor_health.go)
// to tell a source that has gone quiet mid-stream from one that never
// established a publishing cadence in the first place. First/Last are wall
// times the update carrying that $source was received; Count is how many
// update blocks have carried it.
type sourceSeenEntry struct {
	First time.Time
	Last  time.Time
	Count int
}

// vesselContextPrefix separates vessel contexts from the other trees a
// subscribe=all stream carries (atons, aircraft, meteo).
const vesselContextPrefix = "vessels."

// signalKDeltaMaxPathSegments caps how many dotted segments a single delta
// value's path may have before applyDelta rejects it outright, rather than
// ever building the nested tree for it.
//
// signalk_paths.go's own picker walk caps recursion depth at
// signalKPathMaxDepth with the reasoning this borrows directly: "SignalK
// trees are shallow; anything deeper is a malformed or cyclic payload, and a
// runaway walk on a boat computer is worse than a truncated picker." That
// walk only ever runs over a tree applyDelta already built, though, so it
// cannot protect against the tree itself being too deep to walk in the first
// place: a ~3.4MB delta with a 1.7-million-segment path fits comfortably
// under signalKStreamReadLimit's 4MB, and applyDelta's own segment loop is
// iterative, so it survives building a million-level-deep nested map. Every
// *recursive* walker over that tree afterwards -- deepCopyMap/deepCopyValue
// below, plus freshestTimestampAge and flattenNotificationLeaves elsewhere
// in this package -- then blows Go's ~1GB goroutine stack, which is a FATAL
// runtime error recover() cannot catch (K-1, backend security audit).
//
// The fix belongs here, at ingestion, so the deep tree is never built at
// all: rejecting one malformed value is far cheaper and more robust than
// trying to depth-limit every walker that might ever touch the tree. Reusing
// signalKPathMaxDepth's own value (rather than picking a new number) keeps
// the two bounds trivially consistent, since a tree's nesting depth and its
// paths' segment count are the same thing here.
const signalKDeltaMaxPathSegments = signalKPathMaxDepth

// signalKContextMaxDistinctPaths caps how many distinct dotted paths a
// single context's tree may hold, self included.
//
// evictStaleVesselContexts (below) only ever drops a WHOLE non-self vessel
// context -- deliberately never self, because this vessel's own data must
// not disappear just because it has gone quiet (e.g. sitting at anchor with
// nothing changing). That is the wrong tool against a device publishing an
// ever-growing set of DISTINCT paths under self -- no context key at all
// files a sender under self, per signalKDelta.Context's own doc comment --
// e.g. environment.sensor.<counter> at 10Hz: none of those paths individually
// look stale the instant after they arrive, and the whole context is very
// much alive, so neither existing eviction catches it. buildGaugeValuesPayload
// deep-copies the whole self tree once per second regardless, so an unbounded
// path count becomes unbounded per-second CPU and garbage, eventually
// starving the 1Hz tick and OOM-killing the box -- with nothing in the log to
// explain why (K-2, backend security audit).
//
// A pure age-based eviction (drop a path once its own pathSeen entry is
// older than some window) was considered and rejected: a real SignalK
// server can publish a static value -- design.length, mmsi, a tank's
// capacity -- exactly once at subscribe time and never again unless it
// changes, so an age cutoff would eventually evict genuinely-still-true
// vessel data on a perfectly ordinary, healthy boat with no attack
// happening at all. A count cap only ever activates once a context is
// already holding far more distinct paths than any real N2K/SignalK
// installation this codebase has measured (a 3,000-leaf tree,
// backend-perf-audit.md's own sample) -- so ordinary boat data never
// approaches it, while a flood is bounded well short of the sizes that made
// the growth catastrophic. Eviction order still leans on pathSeen's
// timestamps (oldest first, evictExcessPaths below), so if the cap is ever
// actually reached on a real boat, what goes first is whatever has gone
// longest without an update -- the least likely thing anyone is looking at
// right now.
const signalKContextMaxDistinctPaths = 10000

// signalKContextMaxDistinctSources caps how many distinct $source values a
// single context's sourceSeen history may hold, self included -- the same
// "count cap survives a flood, ordinary boats never approach it" reasoning
// signalKContextMaxDistinctPaths documents above, scaled down to match how
// few distinct $source strings even a large N2K/SignalK installation
// actually uses (one source per gateway/bus/plugin, not one per path).
// sourceSeen has no path-count-style eviction trigger of its own -- it is
// keyed by $source, not by path, so evictExcessPaths never touches it --
// and it is exempt from evictStaleVesselContexts' own age cutoff for self,
// the one context that eviction can never drop wholesale. Without this cap,
// a hostile or malformed delta stream that varies $source per message would
// grow sourceSeen without bound under self in exactly the way K-2 (backend
// security audit) already found for distinct paths.
const signalKContextMaxDistinctSources = 500

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
		contexts:   make(map[string]map[string]any),
		pathSeen:   make(map[string]time.Time),
		sourceSeen: make(map[string]sourceSeenEntry),
	}
}

// radarTargetDeltaPath reports whether path is one of mayara's own ARPA
// target nodes, "radars.<radar id>.targets.<target id>" (see
// testdata/mayara/target-delta.json). radar_source.go's REST poller is the
// only consumer of radar targets anywhere in this codebase; nothing reads
// one off the snapshot tree, so applyDelta drops these before they reach it
// rather than storing and later evicting them.
func radarTargetDeltaPath(path string) bool {
	segments := strings.SplitN(path, ".", 4)
	return len(segments) >= 3 && segments[0] == "radars" && segments[2] == "targets"
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
		// Tracked once per update block, independent of which (if any)
		// individual values below get kept: the silent-source check cares
		// about this $source's publishing cadence, not which paths a given
		// block happened to carry (a block that is entirely radar targets,
		// dropped below, is still a real heartbeat from that source).
		if update.SourceRef != "" && len(update.Values) > 0 {
			key := d.Context + "|" + update.SourceRef
			entry := s.sourceSeen[key]
			if entry.Count == 0 {
				entry.First = now
			}
			entry.Last = now
			entry.Count++
			s.sourceSeen[key] = entry
		}

		for _, val := range update.Values {
			// mayara's ARPA target nodes are never read off this snapshot --
			// radar_source.go's REST poller owns radar targets entirely -- and
			// every target id it has ever assigned climbs forever and is
			// replayed as a null on every resubscribe (~6,000 measured against
			// the boat, signalk_stream.go's read-limit comment). Storing them
			// bought nothing and cost every whole-tree copy in this file a
			// little more each week, so they are dropped before they ever
			// reach the tree or pathSeen.
			if radarTargetDeltaPath(val.Path) {
				continue
			}

			// An empty path carries top-level scalars (e.g. name) which the REST
			// tree presents unwrapped, so they must not gain a "value" key.
			if val.Path == "" {
				s.pathSeen[d.Context+"|"+val.Path] = now
				if valMap, ok := val.Value.(map[string]any); ok {
					for k, v := range valMap {
						// A real dotted-path delta may already have built a whole
						// branch at this key (e.g. "navigation"). The empty-path
						// merge exists only to carry unwrapped top-level SCALARS
						// like name, never to replace a subtree wholesale (K-4,
						// backend security audit) -- the alternative is a single
						// hostile or malformed {"path":"","value":{"navigation":0}}
						// delta quietly destroying everything navigation ever held.
						if _, isBranch := tree[k].(map[string]any); isBranch {
							log.Printf("signalk snapshot: dropping empty-path merge of %q under %s: would overwrite an existing branch with a scalar", k, d.Context)
							continue
						}
						tree[k] = v
					}
				}
				continue
			}

			segments := strings.Split(val.Path, ".")
			if len(segments) > signalKDeltaMaxPathSegments {
				// Reject the whole value before the tree is ever built, rather
				// than truncate the path or depth-limit the walk below -- see
				// signalKDeltaMaxPathSegments's doc comment for why the deep
				// tree must never exist in the first place (K-1, backend
				// security audit). %.80s keeps one hostile multi-megabyte path
				// from also blowing out the log.
				log.Printf("signalk snapshot: dropping delta value for %s: path has %d segments, more than the %d cap (malformed or hostile payload): %.80s...",
					d.Context, len(segments), signalKDeltaMaxPathSegments, val.Path)
				continue
			}

			s.pathSeen[d.Context+"|"+val.Path] = now

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

// snapshotWholeTreeCopies counts operations that deep-copy an entire
// context's tree -- treeFor, and vesselsTree's per-vessel copy -- rather
// than one of the leaf/branch accessors added for backend-perf-audit.md
// Tier 1 #2 (nodeAt, vesselNotificationBranches). Tests use it to assert
// that evaluating N alarm rules, or building the gauge-values payload, no
// longer costs O(N) whole-tree copies -- the "dozens a second" the audit
// measured at 1.2ms/1.4MB/8k allocs per copy of a 3,000-leaf tree. Atomic
// because the delta stream writer and every reader touch it concurrently;
// one atomic add is immaterial next to the copy it counts.
var snapshotWholeTreeCopies int64

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
	atomic.AddInt64(&snapshotWholeTreeCopies, 1)
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

// nodeAt resolves a dotted SignalK path to its node within the self tree --
// the map carrying "value" and, when the source declared one, "meta" or
// "timestamp" -- so a caller can read metadata about a path (its unit, its
// freshest timestamp) without pulling in the value snapshotAlarmReader hands
// back. Returns nil for an unknown path or before the self tree exists, the
// same "absent, not a guess" answer selfTree itself gives.
//
// Walks the live tree under one RLock and copies only the node found,
// instead of selfTree's whole-tree deep copy just to throw away everything
// but that one node -- backend-perf-audit.md Tier 1 #2, paid once per
// enabled alarm rule and once per gauge-bound path on every tick before
// this.
func (s *signalKSnapshot) nodeAt(path string) map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.selfCtx == "" {
		return nil
	}
	return s.nodeAtLocked(s.selfCtx, path)
}

// nodeAtContext is nodeAt's counterpart for an arbitrary context, not just
// self -- the leaf reader signalKCollisionNotifications uses to read one AIS
// target's COG, SOG, nav status, ship type and position for the COLREGS
// encounter line (collision_colregs.go, ADR 0098), each read costing one
// small node copy rather than treeFor's whole-context deep copy of
// everything else AIS publishes about that target (backend-perf-audit.md
// Tier 1 #2, the same lesson vesselNotificationBranches already applied to
// this file).
func (s *signalKSnapshot) nodeAtContext(context, path string) map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.nodeAtLocked(context, path)
}

// nodeAtLocked is nodeAt's walk, callable only while s.mu is already held.
// It reaches into s.contexts directly rather than through a locking
// accessor: nothing here may call a method that itself takes s.mu, since a
// recursive RLock deadlocks whenever a writer is queued between the two
// acquisitions (the same hazard selfTree's own comment documents).
func (s *signalKSnapshot) nodeAtLocked(context, path string) map[string]any {
	tree, ok := s.contexts[context]
	if !ok {
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
	// Handlers read this concurrently with the stream writing; returning the
	// live map would race. Unlike treeFor this does not count toward
	// snapshotWholeTreeCopies: copying one node, not the whole tree, is
	// exactly the point of this accessor.
	return deepCopyMap(node)
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
		atomic.AddInt64(&snapshotWholeTreeCopies, 1)
		vessels[strings.TrimPrefix(context, vesselContextPrefix)] = deepCopyMap(tree)
	}
	return vessels
}

// vesselNotificationBranches returns, for every vessel context (self
// included), a deep copy of just its notifications subtree, keyed by bare
// vessel id the same way vesselsTree is.
//
// signalKCollisionNotifications is the only caller, and a target's
// notifications.navigation.closestApproach leaf is the only field of
// another vessel's tree this host ever reads (ADR 0057) -- copying the
// whole tree per target, everything AIS publishes about it included, bought
// nothing and cost more every week the snapshot ran (Tier 1 #3: nothing
// evicted a stale AIS context before this).
func (s *signalKSnapshot) vesselNotificationBranches() map[string]map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make(map[string]map[string]any, len(s.contexts))
	for context, tree := range s.contexts {
		if !strings.HasPrefix(context, vesselContextPrefix) {
			continue
		}
		root, ok := tree[notificationsRoot].(map[string]any)
		if !ok {
			continue
		}
		out[strings.TrimPrefix(context, vesselContextPrefix)] = map[string]any{
			notificationsRoot: deepCopyMap(root),
		}
	}
	return out
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

// sourcesFor returns a copy of every $source seen under context, keyed by
// the bare source id (not "<context>|<$source>"). A copy, like nodeAt's, so
// a caller (anomaly_sensor_health.go's silentSources) can iterate it without
// holding the snapshot lock across its own arithmetic.
func (s *signalKSnapshot) sourcesFor(context string) map[string]sourceSeenEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	prefix := context + "|"
	result := make(map[string]sourceSeenEntry)
	for key, entry := range s.sourceSeen {
		source, ok := strings.CutPrefix(key, prefix)
		if !ok {
			continue
		}
		result[source] = entry
	}
	return result
}

// lastSeen reports when a path last carried an update, or the zero time if it
// never has. Alarm rules use it to tell a live sensor from a frozen one.
func (s *signalKSnapshot) lastSeen(context, path string) time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pathSeen[context+"|"+path]
}

// vesselContextStaleAfter is how long a non-self vessel context may go
// without a fresh delta before evictStaleVesselContexts drops it. Before
// this, nothing ever evicted a context (collision_ais.go used to document
// exactly that): every AIS target this box had ever heard from stayed in
// the tree forever, so every whole-tree copy in this file got a little
// slower every week it ran (backend-perf-audit.md Tier 1 #3). An hour is
// comfortably longer than an ordinary AIS gap -- a target passing behind an
// island, a VHF duty-cycle silence -- while still bounding growth to
// "vessels heard within about the last hour," the same order of magnitude
// as collisionTargetMaxAge and radarPresenceMaxAge's own "gone quiet" gates
// elsewhere in this package.
const vesselContextStaleAfter = 1 * time.Hour

// evictStaleVesselContexts drops every non-self vessel context whose newest
// pathSeen entry is older than vesselContextStaleAfter, taking every
// pathSeen entry for that context with it. A context with no pathSeen entry
// at all counts as stale too -- the same "no evidence of freshness" rule
// stale() already applies to one path, applied here to a whole context. self
// is never a candidate, whatever its own pathSeen ages look like: it is this
// vessel, not a contact that can go out of range. Non-vessel contexts
// (atons, aircraft, meteo) are left alone too -- they are not the "every AIS
// target ever heard" growth this exists to bound, and nothing in this
// package's hot paths copies them the way selfTree/vesselsTree do vessels.
//
// pathSeen stays the flat "<context>|<path>" map it always was, keyed by
// path rather than by context first, so every direct pathSeen[...] access
// this package's tests already make keeps working unchanged. Finding each
// context's newest entry costs one pass over the whole map; cheap enough at
// the sweeper's one-minute cadence (startVesselContextSweeper) that
// restructuring pathSeen was not worth the churn to those tests.
//
// Returns the evicted contexts, sorted, so a caller can log exactly what
// left.
func (s *signalKSnapshot) evictStaleVesselContexts(now time.Time) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	newest := map[string]time.Time{}
	for key, seen := range s.pathSeen {
		context, _, ok := strings.Cut(key, "|")
		if !ok {
			continue
		}
		if seen.After(newest[context]) {
			newest[context] = seen
		}
	}

	var evicted []string
	for context := range s.contexts {
		if context == "" || context == s.selfCtx || !strings.HasPrefix(context, vesselContextPrefix) {
			continue
		}
		if now.Sub(newest[context]) > vesselContextStaleAfter {
			evicted = append(evicted, context)
		}
	}
	sort.Strings(evicted)

	for _, context := range evicted {
		delete(s.contexts, context)
		prefix := context + "|"
		for key := range s.pathSeen {
			if strings.HasPrefix(key, prefix) {
				delete(s.pathSeen, key)
			}
		}
		// sourceSeen is keyed "<context>|<$source>", the same shape as
		// pathSeen's own "<context>|<path>" -- an evicted context must lose
		// these too, or a contact heard once leaks one entry per $source it
		// ever used for as long as the process runs.
		for key := range s.sourceSeen {
			if strings.HasPrefix(key, prefix) {
				delete(s.sourceSeen, key)
			}
		}
	}

	return evicted
}

// deletePathLocked removes the leaf a dotted path resolves to from context's
// tree -- applyDelta's own segment-walk, run in reverse: every segment
// except the last is a branch map to descend into, and the last is the key
// to remove from its parent (the map itself carries "value"/"timestamp"/
// "$source", so deleting that key removes the whole leaf in one step). Must
// be called with s.mu already held for writing.
//
// A path with no matching leaf (already evicted, or the tree's shape has
// since changed under it) is a silent no-op: eviction racing an unrelated
// delta for the same path is an expected, harmless overlap, not an error.
func (s *signalKSnapshot) deletePathLocked(context, path string) {
	tree, ok := s.contexts[context]
	if !ok {
		return
	}

	segments := strings.Split(path, ".")
	current := tree
	for i, segment := range segments {
		if i == len(segments)-1 {
			delete(current, segment)
			return
		}
		child, ok := current[segment].(map[string]any)
		if !ok {
			return
		}
		current = child
	}
}

// evictExcessPaths bounds every context (self included) at
// signalKContextMaxDistinctPaths distinct paths, dropping the
// least-recently-updated ones first until each is back at the cap -- see
// signalKContextMaxDistinctPaths's own doc comment for why a count cap,
// rather than an age cutoff, is the trigger here. Returns how many paths
// were evicted per context, so a caller can log exactly what left.
func (s *signalKSnapshot) evictExcessPaths() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()

	type seenPath struct {
		path string
		seen time.Time
	}
	byContext := map[string][]seenPath{}
	for key, seen := range s.pathSeen {
		context, path, ok := strings.Cut(key, "|")
		if !ok {
			continue
		}
		byContext[context] = append(byContext[context], seenPath{path: path, seen: seen})
	}

	evicted := map[string]int{}
	for context, paths := range byContext {
		if len(paths) <= signalKContextMaxDistinctPaths {
			continue
		}

		sort.Slice(paths, func(i, j int) bool { return paths[i].seen.Before(paths[j].seen) })

		excess := len(paths) - signalKContextMaxDistinctPaths
		for i := 0; i < excess; i++ {
			delete(s.pathSeen, context+"|"+paths[i].path)
			s.deletePathLocked(context, paths[i].path)
		}
		evicted[context] = excess
	}

	return evicted
}

// evictExcessSources bounds every context (self included) at
// signalKContextMaxDistinctSources distinct $source entries in sourceSeen,
// dropping the least-recently-seen ones first until each is back at the
// cap. Mirrors evictExcessPaths exactly, one map over: same shape of key,
// same age-ordered eviction, same "return what was evicted per context so
// the caller can log it" contract.
func (s *signalKSnapshot) evictExcessSources() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()

	type seenSource struct {
		source string
		seen   time.Time
	}
	byContext := map[string][]seenSource{}
	for key, entry := range s.sourceSeen {
		context, source, ok := strings.Cut(key, "|")
		if !ok {
			continue
		}
		byContext[context] = append(byContext[context], seenSource{source: source, seen: entry.Last})
	}

	evicted := map[string]int{}
	for context, sources := range byContext {
		if len(sources) <= signalKContextMaxDistinctSources {
			continue
		}

		sort.Slice(sources, func(i, j int) bool { return sources[i].seen.Before(sources[j].seen) })

		excess := len(sources) - signalKContextMaxDistinctSources
		for i := 0; i < excess; i++ {
			delete(s.sourceSeen, context+"|"+sources[i].source)
		}
		evicted[context] = excess
	}

	return evicted
}

// vesselContextSweepInterval is how often startVesselContextSweeper checks
// for stale vessel contexts to evict. A one-minute cadence keeps the scan
// (one pass over pathSeen, see evictStaleVesselContexts) cheap enough not to
// measure while staying well under vesselContextStaleAfter's own hour.
const vesselContextSweepInterval = 1 * time.Minute

// startVesselContextSweeper runs evictStaleVesselContexts, evictExcessPaths
// and evictExcessSources on vesselContextSweepInterval until ctx is
// cancelled (backend-perf-audit.md Tier 1 #3, "the snapshot never forgets";
// K-2, backend security audit, extends the same sweep to per-path and
// per-source growth within a context that never itself goes stale). A
// dedicated ticker rather than
// piggybacking on the stream watchdog's 15s tick: the sweep's cadence has no
// reason to track the watchdog's, and keeping them separate means changing
// one interval can never accidentally change the other.
func startVesselContextSweeper(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			evicted := globalSignalKSnapshot.evictStaleVesselContexts(now.UTC())
			if len(evicted) > 0 {
				log.Printf("signalk snapshot: evicted %d stale vessel context(s): %s", len(evicted), strings.Join(evicted, ", "))
			}

			if excessByContext := globalSignalKSnapshot.evictExcessPaths(); len(excessByContext) > 0 {
				contexts := make([]string, 0, len(excessByContext))
				for context := range excessByContext {
					contexts = append(contexts, context)
				}
				sort.Strings(contexts)

				parts := make([]string, 0, len(contexts))
				for _, context := range contexts {
					parts = append(parts, fmt.Sprintf("%s (%d)", context, excessByContext[context]))
				}
				log.Printf("signalk snapshot: evicted excess distinct paths, oldest-updated-first, to stay under %d per context: %s",
					signalKContextMaxDistinctPaths, strings.Join(parts, ", "))
			}

			if excessByContext := globalSignalKSnapshot.evictExcessSources(); len(excessByContext) > 0 {
				contexts := make([]string, 0, len(excessByContext))
				for context := range excessByContext {
					contexts = append(contexts, context)
				}
				sort.Strings(contexts)

				parts := make([]string, 0, len(contexts))
				for _, context := range contexts {
					parts = append(parts, fmt.Sprintf("%s (%d)", context, excessByContext[context]))
				}
				log.Printf("signalk snapshot: evicted excess distinct sources, oldest-seen-first, to stay under %d per context: %s",
					signalKContextMaxDistinctSources, strings.Join(parts, ", "))
			}
		}
	}
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
