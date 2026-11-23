package main

import (
	"sync"
	"sync/atomic"
)

// The memo counts nothing itself. A test that wants to know how many geometries were
// built, how often the memo was asked or how many squares were read from the store
// makes the memo with newCountedMemo, which hangs counters on its test hooks.

type memoCounts struct{ built, lookups, reads atomic.Int64 }

var memoCountsOf sync.Map // *fetchGeoMemo -> *memoCounts

func newCountedMemo(budgetSegments int) *fetchGeoMemo {
	m := newFetchGeoMemo(budgetSegments)
	c := &memoCounts{}
	m.onBuild = func() { c.built.Add(1) }
	m.onLookup = func() { c.lookups.Add(1) }
	m.lines.onRead = func() { c.reads.Add(1) }
	memoCountsOf.Store(m, c)
	return m
}

func (m *fetchGeoMemo) counts() *memoCounts {
	c, ok := memoCountsOf.Load(m)
	if !ok {
		panic("the memo was not made by newCountedMemo")
	}
	return c.(*memoCounts)
}

// builtCount is how many geometries the memo has built, for a test to tell a read
// from a build.
func (m *fetchGeoMemo) builtCount() int { return int(m.counts().built.Load()) }

// lookupCount is how many times the memo was asked.
func (m *fetchGeoMemo) lookupCount() int { return int(m.counts().lookups.Load()) }

// readCount is how many squares were read from the store into the memo's lines.
func (m *fetchGeoMemo) readCount() int { return int(m.counts().reads.Load()) }
