package main

import (
	"container/list"
	"context"
	"errors"
	"sync"
)

// The shore a leg's hours cast their rays over, kept between requests (ADR 0302).
// A geometry is the shoreline projected around one position; building one reads
// and projects the lines of every square within 15 nm, which is the cost of a plan
// assessment's comfort. The plan page asks again every few minutes for the same
// legs, so the geometries are kept, the least recently used dropped once their
// segments pass fetchGeoBudgetSegments. The squares' own lines are kept the same
// way, so a new position in a square already read does not read it again. Both are
// sized by what they hold, not by how many entries there are: a position far out to
// sea holds no segment and costs one. A geometry is never changed once built, so
// readers share it.

const (
	// fetchGeoBudgetSegments is about 16 MB of projected segments (40 bytes each).
	fetchGeoBudgetSegments = 400_000
	// squareLinesBudgetPoints is about 16 MB of shoreline points (16 bytes each).
	squareLinesBudgetPoints = 1_000_000
)

// legFetchGeometries is the memo every plan assessment reads.
var legFetchGeometries = newFetchGeoMemo(fetchGeoBudgetSegments)

// fetchGeoMemoKey names a position snapped to 0.001 degree and the state of the
// squares its shore was read from: a square downloaded again, replaced or removed
// gives a new stamp, so a stale geometry is never read for a changed shore.
type fetchGeoMemoKey struct {
	latE3, lonE3 int
	stamp        string
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

// geoCall is a build in flight, which readers of its key wait on.
type geoCall struct {
	done chan struct{}
	geo  *fetchGeometry
	err  error
}

type fetchGeoMemo struct {
	geos   *weightedLRU[fetchGeoMemoKey, *fetchGeometry]
	lines  *squareLinesCache
	mu     sync.Mutex // the builds in flight
	flying map[fetchGeoMemoKey]*geoCall
	// onLookup and onBuild are test hooks, nil in production: the memo was asked, and
	// a geometry was built. A test sets them before the memo is used.
	onLookup, onBuild func()
}

// squareLinesCache is the lines of held squares, shared by every request's loader.
type squareLinesCache struct {
	lru *weightedLRU[squareLinesKey, []shorelineLine]
	// onRead is a test hook, nil in production: a square was read from the store.
	onRead func()
}

func newFetchGeoMemo(budgetSegments int) *fetchGeoMemo {
	return &fetchGeoMemo{
		geos: newWeightedLRU[fetchGeoMemoKey, *fetchGeometry](budgetSegments, func(g *fetchGeometry) int { return len(g.segs) }),
		lines: &squareLinesCache{lru: newWeightedLRU[squareLinesKey, []shorelineLine](squareLinesBudgetPoints, func(ls []shorelineLine) int {
			n := 0
			for _, l := range ls {
				n += len(l.Points)
			}
			return n
		})},
		flying: map[fetchGeoMemoKey]*geoCall{},
	}
}

var errGeoBuildUnfinished = errors.New("the shore geometry build did not finish")

// get is the geometry for a key, built by build when the memo has none. Readers that
// miss on one key at once share one build. A failed build is never kept. A reader
// whose request is done stops waiting for another's build, and a build that failed
// because the request that began it ran out of time is not the failure of the readers
// that waited on it: they build it themselves.
func (m *fetchGeoMemo) get(ctx context.Context, key fetchGeoMemoKey, build func() (*fetchGeometry, error)) (*fetchGeometry, error) {
	if m.onLookup != nil {
		m.onLookup()
	}
	for {
		if g, ok := m.geos.get(key); ok {
			return g, nil
		}
		m.mu.Lock()
		// A build that finished while this reader reached for the lock has kept its result.
		if g, ok := m.geos.get(key); ok {
			m.mu.Unlock()
			return g, nil
		}
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

// build runs the build this reader began and answers the readers waiting on it.
func (m *fetchGeoMemo) build(key fetchGeoMemoKey, c *geoCall, build func() (*fetchGeometry, error)) (*fetchGeometry, error) {
	defer func() {
		m.mu.Lock()
		delete(m.flying, key)
		m.mu.Unlock()
		close(c.done)
	}()
	g, err := build()
	if err != nil {
		c.err = err
		return nil, err
	}
	if m.onBuild != nil {
		m.onBuild()
	}
	m.geos.put(key, g)
	c.geo, c.err = g, nil
	return g, nil
}
