package main

import (
	"context"
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
	}

	return evicted
}

// vesselContextSweepInterval is how often startVesselContextSweeper checks
// for stale vessel contexts to evict. A one-minute cadence keeps the scan
// (one pass over pathSeen, see evictStaleVesselContexts) cheap enough not to
// measure while staying well under vesselContextStaleAfter's own hour.
const vesselContextSweepInterval = 1 * time.Minute

// startVesselContextSweeper runs evictStaleVesselContexts on
// vesselContextSweepInterval until ctx is cancelled (backend-perf-audit.md
// Tier 1 #3, "the snapshot never forgets"). A dedicated ticker rather than
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
