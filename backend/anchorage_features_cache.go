package main

import (
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// anchorageFeaturesTTL is how long an Overpass answer is reused. OpenStreetMap
// changes slowly and the answer carries its own data_as_of, so six hours loses
// nothing a skipper could act on.
const anchorageFeaturesTTL = 6 * time.Hour

// anchorageFeaturesMaxEntries bounds the cache; past it the oldest entry goes.
const anchorageFeaturesMaxEntries = 64

type anchorageFeaturesEntry struct {
	value    anchorageFeatures
	cachedAt time.Time
}

// anchorageFeaturesCache holds successful anchorage_features answers in memory
// only, so a restart or a plugin swap starts empty. It never serves an old
// answer after a failed fetch: an error is returned as the error.
type anchorageFeaturesCache struct {
	mu    sync.Mutex
	data  map[string]anchorageFeaturesEntry
	group singleflight.Group
	now   func() time.Time
}

func newAnchorageFeaturesCache() *anchorageFeaturesCache {
	return &anchorageFeaturesCache{data: map[string]anchorageFeaturesEntry{}, now: time.Now}
}

// anchorageFeaturesCacheKey formats the position as the plugin query does
// (six decimals). The guest works out distances and bay containment from the
// exact point, so a hit must only ever be the same query.
func anchorageFeaturesCacheKey(pluginKey string, lat, lon float64) string {
	return fmt.Sprintf("%s|%.6f,%.6f", pluginKey, lat, lon)
}

func (c *anchorageFeaturesCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.data)
}

// get returns the cached answer for key, marked Cached, or runs fetch (merged
// across concurrent callers) and caches a success.
func (c *anchorageFeaturesCache) get(key string, fetch func() (anchorageFeatures, error)) (anchorageFeatures, error) {
	if v, ok := c.lookup(key); ok {
		return v, nil
	}
	v, err, _ := c.group.Do(key, func() (any, error) {
		// A caller that missed just before another fetch finished lands here
		// after the answer is stored; it must not fetch again.
		if v, ok := c.lookup(key); ok {
			return v, nil
		}
		f, err := fetch()
		if err != nil {
			return nil, err
		}
		c.mu.Lock()
		c.data[key] = anchorageFeaturesEntry{value: f, cachedAt: c.now()}
		c.evictLocked()
		c.mu.Unlock()
		return f, nil
	})
	if err != nil {
		return anchorageFeatures{}, err
	}
	return v.(anchorageFeatures), nil
}

// lookup returns a live entry marked Cached, and deletes an expired one.
func (c *anchorageFeaturesCache) lookup(key string) (anchorageFeatures, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.data[key]
	if !ok {
		return anchorageFeatures{}, false
	}
	if c.now().Sub(e.cachedAt) >= anchorageFeaturesTTL {
		delete(c.data, key)
		return anchorageFeatures{}, false
	}
	v := e.value
	v.Cached, v.CachedAt = true, e.cachedAt
	return v, true
}

func (c *anchorageFeaturesCache) evictLocked() {
	if len(c.data) > anchorageFeaturesMaxEntries {
		now := c.now()
		for k, e := range c.data {
			if now.Sub(e.cachedAt) >= anchorageFeaturesTTL {
				delete(c.data, k)
			}
		}
	}
	for len(c.data) > anchorageFeaturesMaxEntries {
		oldest, first := "", true
		var at time.Time
		for k, e := range c.data {
			if first || e.cachedAt.Before(at) {
				oldest, at, first = k, e.cachedAt, false
			}
		}
		delete(c.data, oldest)
	}
}
