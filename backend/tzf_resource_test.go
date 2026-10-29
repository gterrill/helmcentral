package main

import (
	"runtime"
	"testing"
	"time"
)

// The Pi 5 check (ADR 0228): the zone finder builds its index on first use,
// so a slow or hungry build would show up as a stalled first screen. CI runs
// this on linux/arm64. Skipped under -short because it forces the build.
func TestZoneFinderInitStaysSmall(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the full zone index; run without -short")
	}
	const maxInit = 3 * time.Second
	const maxHeap = 64 << 20

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	start := time.Now()
	if _, err := zoneFinder(); err != nil {
		t.Fatalf("zoneFinder: %v", err)
	}
	took := time.Since(start)

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	heap := int64(after.HeapAlloc) - int64(before.HeapAlloc)

	t.Logf("zone finder init %s, heap +%d MB (%s/%s)", took, heap>>20, runtime.GOOS, runtime.GOARCH)
	if took > maxInit {
		t.Errorf("init took %s, limit %s", took, maxInit)
	}
	if heap > maxHeap {
		t.Errorf("heap after init +%d MB, limit %d MB", heap>>20, maxHeap>>20)
	}
}
