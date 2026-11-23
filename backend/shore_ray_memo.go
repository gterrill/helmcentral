package main

import (
	"container/list"
	"context"
	"errors"
	"sync"
)

// What the shore said of a position, kept between requests (ADR 0302). A ray result
// is one number and two flags: how far the shore is along a bearing, whether a reef
// stops it, whether the sea is open. Casting one needs the shoreline projected
// around the position, which reads and projects the lines of every square within
// 15 nm and is the cost of a plan assessment's comfort. The plan page asks again
// every few minutes for the same legs, so the results are kept, not the geometry:
// a result is a few bytes, a geometry is tens of thousands of segments. The least
// recently used result is dropped once the memo holds shoreRayEntries. The squares'
// own lines are kept the same way, sized by their points, so a new position in a
// square already read does not read it again.
//
// The geometry lives only inside one request, built the first time a ray at its
// position is not in the memo and let go when the leg ends. A forecast run that
// brings a bearing nobody asked before therefore costs a build for that position,
// about as much as a cold run.

const (
	// shoreRayEntries is how many ray results the memo keeps, each a few hundred
	// bytes with its key and its place in the list, so under 10 MB.
	shoreRayEntries = 32_768
	// squareLinesBudgetPoints is about 16 MB of shoreline points (16 bytes each).
	squareLinesBudgetPoints = 1_000_000
)

// legShoreRays is the memo every plan assessment reads.
var legShoreRays = newShoreRayMemo(shoreRayEntries)

// shoreSpotKey names a position snapped to 0.001 degree and the state of the squares
// its shore was read from: a square downloaded again, replaced or removed gives a new
// stamp, so a result is never read for a changed shore.
type shoreSpotKey struct {
	latE3, lonE3 int32
	stamp        string
}

// shoreRayKey names one ray result: the position, the kind of ray and the bearing in
// tenths of a degree (0 for a calm, which has no bearing).
type shoreRayKey struct {
	shoreSpotKey
	kind uint8
	dd   int16
}

// shoreRayKind is the kind of ray as the key keeps it. Any kind but the two named is
// a wind ray, as fetchGeometry.ray reads it.
func shoreRayKind(kind string) uint8 {
	switch kind {
	case fetchKindCalm:
		return 1
	case fetchKindSwell:
		return 2
	}
	return 0
}

// squareLinesKey names a held square as it was downloaded.
type squareLinesKey struct {
	square                    coverageSquare
	downloadedAt, dataAsOfSec int64
}

// weightedLRU keeps values until their weights pass a budget, then drops the least
// recently used. The newest is always kept, however heavy.
type weightedLRU[K comparable, V any] struct {
	mu     sync.Mutex
	budget int
	used   int
	weigh  func(V) int
	order  *list.List // most recently used first
	at     map[K]*list.Element
}

type lruEntry[K comparable, V any] struct {
	key    K
	val    V
	weight int
}

func newWeightedLRU[K comparable, V any](budget int, weigh func(V) int) *weightedLRU[K, V] {
	return &weightedLRU[K, V]{budget: budget, weigh: weigh, order: list.New(), at: map[K]*list.Element{}}
}

func (c *weightedLRU[K, V]) get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.at[key]
	if !ok {
		var zero V
		return zero, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*lruEntry[K, V]).val, true
}

// put keeps a value as the newest. A key already held (two loaders that both missed
// it) is replaced, at its new weight, and counted once.
func (c *weightedLRU[K, V]) put(key K, val V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := max(c.weigh(val), 1)
	if el, ok := c.at[key]; ok {
		e := el.Value.(*lruEntry[K, V])
		c.used += w - e.weight
		e.val, e.weight = val, w
		c.order.MoveToFront(el)
	} else {
		c.at[key] = c.order.PushFront(&lruEntry[K, V]{key: key, val: val, weight: w})
		c.used += w
	}
	for c.used > c.budget && c.order.Len() > 1 {
		oldest := c.order.Back()
		e := oldest.Value.(*lruEntry[K, V])
		delete(c.at, e.key)
		c.used -= e.weight
		c.order.Remove(oldest)
	}
}

// geoCall is a build in flight, which requests that need the same geometry wait on.
type geoCall struct {
	done chan struct{}
	geo  *fetchGeometry
	err  error
}

type shoreRayMemo struct {
	rays  *weightedLRU[shoreRayKey, rayResult]
	lines *squareLinesCache
	mu    sync.Mutex // the builds in flight
	// flying is the geometries being built now. A geometry is not kept once built, so
	// this is only for requests that need one at the same moment to share the build.
	flying map[shoreSpotKey]*geoCall
	// onBuild is a test hook, nil in production: a geometry was built. A test sets it
	// before the memo is used.
	onBuild func()
	// onTry is a test hook, nil in production: a geometry build was begun, whether or
	// not it finished.
	onTry func()
}

// squareLinesCache is the lines of held squares, shared by every request's loader.
type squareLinesCache struct {
	lru *weightedLRU[squareLinesKey, []shorelineLine]
	// onRead is a test hook, nil in production: a square was read from the store.
	onRead func()
}

func newShoreRayMemo(entries int) *shoreRayMemo {
	return &shoreRayMemo{
		rays: newWeightedLRU[shoreRayKey, rayResult](entries, func(rayResult) int { return 1 }),
		lines: &squareLinesCache{lru: newWeightedLRU[squareLinesKey, []shorelineLine](squareLinesBudgetPoints, func(ls []shorelineLine) int {
			n := 0
			for _, l := range ls {
				n += len(l.Points)
			}
			return n
		})},
		flying: map[shoreSpotKey]*geoCall{},
	}
}

var errGeoBuildUnfinished = errors.New("the shore geometry build did not finish")

// geometry builds the geometry of a position with build. Requests that need one at
// once share one build, and a failed build is never kept. A request whose ctx is done
// stops waiting for another's build, and a build that failed because the request that
// began it ran out of time is not the failure of the requests that waited on it: they
// build it themselves.
func (m *shoreRayMemo) geometry(ctx context.Context, key shoreSpotKey, build func() (*fetchGeometry, error)) (*fetchGeometry, error) {
	for {
		m.mu.Lock()
		c, flying := m.flying[key]
		if !flying {
			c = &geoCall{done: make(chan struct{}), err: errGeoBuildUnfinished}
			m.flying[key] = c
		}
		m.mu.Unlock()
		if !flying {
			return m.build(key, c, build)
		}
		select {
		case <-c.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if c.err == nil || !(errors.Is(c.err, context.Canceled) || errors.Is(c.err, context.DeadlineExceeded)) || ctx.Err() != nil {
			return c.geo, c.err
		}
	}
}

// build runs the build this request began and answers the requests waiting on it.
func (m *shoreRayMemo) build(key shoreSpotKey, c *geoCall, build func() (*fetchGeometry, error)) (*fetchGeometry, error) {
	defer func() {
		m.mu.Lock()
		delete(m.flying, key)
		m.mu.Unlock()
		close(c.done)
	}()
	if m.onTry != nil {
		m.onTry()
	}
	g, err := build()
	if err != nil {
		c.err = err
		return nil, err
	}
	if m.onBuild != nil {
		m.onBuild()
	}
	c.geo, c.err = g, nil
	return g, nil
}
