package main

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestFeaturesCache(now *time.Time) *anchorageFeaturesCache {
	c := newAnchorageFeaturesCache()
	c.now = func() time.Time { return *now }
	return c
}

func TestAnchorageFeaturesCache_SecondCallWithinTTLSkipsThePlugin(t *testing.T) {
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	c := newTestFeaturesCache(&now)
	calls := 0
	fetch := func() (anchorageFeatures, error) {
		calls++
		return anchorageFeatures{DataAsOf: "2026-11-01T00:00:00Z"}, nil
	}
	key := anchorageFeaturesCacheKey("p", -20.07651, 148.92501)

	first, err := c.get(key, fetch)
	if err != nil || first.Cached {
		t.Fatalf("first = %+v, %v; want a fresh answer", first, err)
	}
	now = now.Add(time.Hour)
	second, err := c.get(anchorageFeaturesCacheKey("p", -20.07651, 148.92501), fetch)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !second.Cached || !second.CachedAt.Equal(now.Add(-time.Hour)) {
		t.Errorf("calls = %d, second = cached %v at %v", calls, second.Cached, second.CachedAt)
	}
	if second.DataAsOf != "2026-11-01T00:00:00Z" {
		t.Errorf("data_as_of changed: %q", second.DataAsOf)
	}
}

func TestAnchorageFeaturesCache_DifferentPositionOrPluginMisses(t *testing.T) {
	now := time.Now()
	c := newTestFeaturesCache(&now)
	calls := 0
	fetch := func() (anchorageFeatures, error) { calls++; return anchorageFeatures{}, nil }
	for _, k := range []string{
		anchorageFeaturesCacheKey("p", -20.0765, 148.925),
		anchorageFeaturesCacheKey("p", -20.0766, 148.925),
		anchorageFeaturesCacheKey("q", -20.0765, 148.925),
		anchorageFeaturesCacheKey("p", -20.07651, 148.925), // 5th decimal differs
	} {
		if _, err := c.get(k, fetch); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 4 {
		t.Errorf("calls = %d, want 4", calls)
	}
}

func TestAnchorageFeaturesCache_ExpiryRefetches(t *testing.T) {
	now := time.Now()
	c := newTestFeaturesCache(&now)
	calls := 0
	fetch := func() (anchorageFeatures, error) { calls++; return anchorageFeatures{}, nil }
	key := anchorageFeaturesCacheKey("p", 1, 2)
	c.get(key, fetch)
	now = now.Add(anchorageFeaturesTTL - time.Second)
	c.get(key, fetch)
	if calls != 1 {
		t.Fatalf("calls = %d before expiry", calls)
	}
	now = now.Add(2 * time.Second)
	got, _ := c.get(key, fetch)
	if calls != 2 || got.Cached {
		t.Errorf("calls = %d cached = %v after expiry", calls, got.Cached)
	}
}

func TestAnchorageFeaturesCache_ErrorsAreNeverCachedNorMaskedByOldData(t *testing.T) {
	now := time.Now()
	c := newTestFeaturesCache(&now)
	key := anchorageFeaturesCacheKey("p", 1, 2)
	boom := errors.New("overpass is busy")

	if _, err := c.get(key, func() (anchorageFeatures, error) { return anchorageFeatures{}, boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	calls := 0
	if _, err := c.get(key, func() (anchorageFeatures, error) { calls++; return anchorageFeatures{}, nil }); err != nil || calls != 1 {
		t.Fatalf("after an error: err = %v calls = %d", err, calls)
	}
	// Expired success followed by an error: the error is returned, not the old answer.
	now = now.Add(anchorageFeaturesTTL + time.Second)
	if got, err := c.get(key, func() (anchorageFeatures, error) { return anchorageFeatures{}, boom }); !errors.Is(err, boom) || got.Cached {
		t.Errorf("got %+v, %v; want the error", got, err)
	}
}

func TestAnchorageFeaturesCache_ConcurrentIdenticalCallsMakeOnePluginCall(t *testing.T) {
	c := newAnchorageFeaturesCache()
	var calls, started atomic.Int32
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	fetch := func() (anchorageFeatures, error) {
		calls.Add(1)
		entered <- struct{}{}
		<-release
		return anchorageFeatures{}, nil
	}
	key := anchorageFeaturesCacheKey("p", 1, 2)
	const n = 8
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			started.Add(1)
			c.get(key, fetch)
		}()
	}
	<-entered // the first fetch is in flight and held
	deadline := time.Now().Add(5 * time.Second)
	for started.Load() < n && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if started.Load() < n {
		t.Fatal("goroutines did not all start")
	}
	close(release)
	wg.Wait()
	if n := calls.Load(); n != 1 {
		t.Errorf("plugin calls = %d, want 1", n)
	}
}

func TestAnchorageFeaturesCache_ExpiredEntriesAreDroppedOnReadAndBeforeEviction(t *testing.T) {
	now := time.Now()
	c := newTestFeaturesCache(&now)
	ok := func() (anchorageFeatures, error) { return anchorageFeatures{}, nil }
	c.get("old", ok)
	now = now.Add(anchorageFeaturesTTL + time.Second)
	boom := errors.New("down")
	if _, err := c.get("old", func() (anchorageFeatures, error) { return anchorageFeatures{}, boom }); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if c.len() != 0 {
		t.Errorf("expired entry kept after read: %d", c.len())
	}
	for i := 0; i < anchorageFeaturesMaxEntries; i++ {
		c.get(fmt.Sprintf("e%d", i), ok)
	}
	now = now.Add(anchorageFeaturesTTL + time.Second)
	c.get("fresh1", ok)
	now = now.Add(time.Second)
	c.get("fresh2", ok)
	if n := c.len(); n != 2 {
		t.Errorf("entries = %d, want 2: expired ones go before the bound is enforced", n)
	}
}

func TestAnchorageFeaturesCache_IsBounded(t *testing.T) {
	now := time.Now()
	c := newTestFeaturesCache(&now)
	for i := 0; i < anchorageFeaturesMaxEntries+10; i++ {
		now = now.Add(time.Second)
		c.get(anchorageFeaturesCacheKey("p", float64(i)/1000, 0), func() (anchorageFeatures, error) { return anchorageFeatures{}, nil })
	}
	if n := c.len(); n != anchorageFeaturesMaxEntries {
		t.Errorf("entries = %d, want %d", n, anchorageFeaturesMaxEntries)
	}
}
