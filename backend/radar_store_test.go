package main

import (
	"testing"
	"time"
)

// newTestArpaTarget builds a mayaraArpaTarget directly in Go, per AGENTS.md's
// fixture rule: these tests exercise our own types, not mayara's wire
// format, so there is nothing to capture and nothing to hand-author.
func newTestArpaTarget(id uint64, status string, rangeM float64) mayaraArpaTarget {
	target := mayaraArpaTarget{ID: id, Status: status}
	target.Position.Bearing = 0.3
	target.Position.Distance = int(rangeM)
	return target
}

// newTestRadarTarget builds a store-ready radarTarget through the real
// conversion path (radarTargetFromArpa), so these store-level tests exercise
// the same struct radarPoller.pollOnce actually produces rather than a
// hand-built stand-in that could quietly drift from it.
func newTestRadarTarget(radarID string, id uint64, status string, rangeM float64, now time.Time) radarTarget {
	return radarTargetFromArpa(radarID, newTestArpaTarget(id, status, rangeM), 0, 0, false, now)
}

// A poll response is the tracker's full current state: a target missing from
// the new set is gone immediately, no ageing involved. This is the property
// that replaced ADR 0062's three-signal eviction (null delta, status:"lost",
// wall-clock sweep) with a single full-state swap.
func TestRadarStoreReplaceDropsTargetsAbsentFromNewSet(t *testing.T) {
	store := newRadarTargetStore()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)

	store.replace("fur6424A", []radarTarget{
		newTestRadarTarget("fur6424A", 1, "tracking", 500, now),
		newTestRadarTarget("fur6424A", 2, "tracking", 800, now),
	}, now)
	if got := len(store.list(now)); got != 2 {
		t.Fatalf("after the first replace: got %d targets, want 2", got)
	}

	next := now.Add(2 * time.Second)
	store.replace("fur6424A", []radarTarget{
		newTestRadarTarget("fur6424A", 1, "tracking", 510, next),
	}, next)

	list := store.list(next)
	if len(list) != 1 || list[0].TargetID != 1 {
		t.Fatalf("target 2 absent from the new poll must be gone, got %+v", list)
	}
}

// mayara's REST payload still carries status:"lost" as the transitional
// state just before a target disappears from the list entirely. replace must
// not store it as if it were still tracked.
func TestRadarStoreReplaceDropsLostStatusEntries(t *testing.T) {
	store := newRadarTargetStore()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)

	store.replace("fur6424A", []radarTarget{
		newTestRadarTarget("fur6424A", 1, "tracking", 500, now),
		newTestRadarTarget("fur6424A", 2, "lost", 800, now),
	}, now)

	list := store.list(now)
	if len(list) != 1 || list[0].TargetID != 1 {
		t.Fatalf("a status:lost entry must not be stored, got %+v", list)
	}
}

func TestRadarStoreListFiltersByAge(t *testing.T) {
	store := newRadarTargetStore()
	start := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)

	store.replace("fur6424A", []radarTarget{newTestRadarTarget("fur6424A", 1, "tracking", 800, start)}, start)

	if got := len(store.list(start.Add(radarTargetMaxAge - time.Second))); got != 1 {
		t.Fatalf("target just under max age should still list, got %d entries", got)
	}

	if got := len(store.list(start.Add(radarTargetMaxAge + time.Second))); got != 0 {
		t.Fatalf("target past max age should not list, got %d entries", got)
	}
}

func TestRadarStoreListRecomputesAgeSecondsAgainstNow(t *testing.T) {
	store := newRadarTargetStore()
	start := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)

	store.replace("fur6424A", []radarTarget{newTestRadarTarget("fur6424A", 1, "tracking", 800, start)}, start)

	list := store.list(start.Add(12 * time.Second))
	if len(list) != 1 {
		t.Fatalf("expected 1 target, got %d", len(list))
	}
	if list[0].AgeSeconds != 12 {
		t.Fatalf("age_seconds = %d, want 12", list[0].AgeSeconds)
	}
}

func TestRadarStoreListSortsByRangeAscending(t *testing.T) {
	store := newRadarTargetStore()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)

	store.replace("fur6424A", []radarTarget{
		newTestRadarTarget("fur6424A", 1, "tracking", 5000, now),
		newTestRadarTarget("fur6424A", 2, "tracking", 200, now),
		newTestRadarTarget("fur6424A", 3, "tracking", 1500, now),
	}, now)

	list := store.list(now)
	if len(list) != 3 {
		t.Fatalf("expected 3 targets, got %d", len(list))
	}
	if list[0].TargetID != 2 || list[1].TargetID != 3 || list[2].TargetID != 1 {
		t.Fatalf("list not sorted nearest-first: %+v", list)
	}
}

func TestRadarStoreSweepEvictsStaleAndReportsCount(t *testing.T) {
	store := newRadarTargetStore()
	start := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)

	store.replace("fur6424A", []radarTarget{newTestRadarTarget("fur6424A", 1, "tracking", 800, start)}, start)

	freshAt := start.Add(radarTargetMaxAge)
	store.replace("fur6424A", []radarTarget{
		newTestRadarTarget("fur6424A", 1, "tracking", 800, start),
		newTestRadarTarget("fur6424A", 2, "tracking", 900, freshAt),
	}, freshAt)

	sweepAt := start.Add(radarTargetMaxAge + time.Second)
	evicted := store.sweep(sweepAt)

	if evicted != 1 {
		t.Fatalf("sweep should evict exactly the stale target, evicted=%d", evicted)
	}

	remaining := store.list(sweepAt)
	if len(remaining) != 1 || remaining[0].TargetID != 2 {
		t.Fatalf("sweep should keep the fresh target, got %+v", remaining)
	}
}

func TestRadarStoreKeepsFreshTargetAcrossSweep(t *testing.T) {
	store := newRadarTargetStore()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)

	store.replace("fur6424A", []radarTarget{newTestRadarTarget("fur6424A", 1, "tracking", 800, now)}, now)

	evicted := store.sweep(now.Add(time.Second))
	if evicted != 0 {
		t.Fatalf("a fresh target must survive a sweep, evicted=%d", evicted)
	}
	if got := len(store.list(now.Add(time.Second))); got != 1 {
		t.Fatalf("fresh target should still list after sweep, got %d entries", got)
	}
}

// Dual range is real on the live rig: one physical radar presents two radar
// ids (fur6424A, fur6424B), and mayara numbers targets independently within
// each, so target id 3 exists twice on a boat with exactly one radar. The
// store key must disambiguate, and each radar's replace must only touch its
// own entries — polling fur6424A must never evict fur6424B's targets.
func TestRadarStoreDualRangeSameTargetIDProducesTwoEntries(t *testing.T) {
	store := newRadarTargetStore()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)

	store.replace("fur6424A", []radarTarget{newTestRadarTarget("fur6424A", 3, "tracking", 1000, now)}, now)
	store.replace("fur6424B", []radarTarget{newTestRadarTarget("fur6424B", 3, "tracking", 2000, now)}, now)

	list := store.list(now)
	if len(list) != 2 {
		t.Fatalf("two radars publishing target id 3 must produce two entries, got %d", len(list))
	}

	ids := map[string]bool{}
	for _, target := range list {
		ids[target.ID] = true
	}
	if !ids["fur6424A:3"] || !ids["fur6424B:3"] {
		t.Fatalf("expected distinct ids fur6424A:3 and fur6424B:3, got %v", ids)
	}
}

func TestRadarStoreDisconnectDoesNotPurge(t *testing.T) {
	store := newRadarTargetStore()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)

	store.replace("fur6424A", []radarTarget{newTestRadarTarget("fur6424A", 1, "tracking", 800, now)}, now)

	store.setConnected(true)
	store.setConnected(false)

	// A failed poll must not blank the map: targets age out through list's
	// own filter, never through setConnected.
	if got := len(store.list(now)); got != 1 {
		t.Fatalf("a failed poll must not purge targets, got %d entries", got)
	}

	connected, _ := store.status()
	if connected {
		t.Fatalf("status should report disconnected")
	}
}

// An empty target list is a legitimate poll result — a live radar with
// nothing currently tracked — and must still record that the poll landed,
// the same way a poll that returns targets does.
func TestRadarStoreReplaceRecordsLastMessageEvenWhenEmpty(t *testing.T) {
	store := newRadarTargetStore()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)

	store.replace("fur6424A", nil, now)

	_, lastMessage := store.status()
	if !lastMessage.Equal(now) {
		t.Fatalf("lastMessage = %v, want %v", lastMessage, now)
	}
}
