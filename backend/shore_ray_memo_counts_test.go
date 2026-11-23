package main

import (
	"sync"
	"sync/atomic"
)

// The memo counts nothing itself. A test that wants to know how many geometries were
// built or how many squares were read from the store makes the memo with
// newCountedMemo, which hangs counters on its test hooks.

type memoCounts struct{ built, reads, tries atomic.Int64 }

var memoCountsOf sync.Map // *shoreRayMemo -> *memoCounts

func newCountedMemo(entries int) *shoreRayMemo {
	m := newShoreRayMemo(entries)
	c := &memoCounts{}
	m.onBuild = func() { c.built.Add(1) }
	m.onTry = func() { c.tries.Add(1) }
	m.lines.onRead = func() { c.reads.Add(1) }
	memoCountsOf.Store(m, c)
	return m
}

func (m *shoreRayMemo) counts() *memoCounts {
	c, ok := memoCountsOf.Load(m)
	if !ok {
		panic("the memo was not made by newCountedMemo")
	}
	return c.(*memoCounts)
}

// builtCount is how many geometries have been built, for a test to tell a read from a
// build.
func (m *shoreRayMemo) builtCount() int { return int(m.counts().built.Load()) }

// triedCount is how many geometry builds were begun, failed ones too.
func (m *shoreRayMemo) triedCount() int { return int(m.counts().tries.Load()) }

// readCount is how many squares were read from the store into the memo's lines.
func (m *shoreRayMemo) readCount() int { return int(m.counts().reads.Load()) }

// held is how many ray results the memo holds.
func (m *shoreRayMemo) held() int { return m.rays.order.Len() }
