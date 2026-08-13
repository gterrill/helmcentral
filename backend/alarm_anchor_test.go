package main

import (
	"strings"
	"testing"
)

// metersNorth offsets a latitude by roughly n metres, enough for a drag test.
func metersNorth(lat float64, meters float64) float64 {
	return lat + meters/111_320.0
}

func testAnchorWatcher(anchor *anchorWatchData, lat, lon float64, gnssCritical, ok bool) *anchorDragWatcher {
	return &anchorDragWatcher{
		anchor:   func() *anchorWatchData { return anchor },
		position: func() (float64, float64, bool, bool) { return lat, lon, gnssCritical, ok },
	}
}

func anchorAt(lat, lon, radius float64) *anchorWatchData {
	return &anchorWatchData{Lat: lat, Lon: lon, RadiusMeters: radius}
}

func TestAnchorWatcherQuietInsideTheCircle(t *testing.T) {
	watcher := testAnchorWatcher(anchorAt(-21.1113, 149.2276, 30), metersNorth(-21.1113, 10), 149.2276, false, true)

	if _, ok := watcher.check(alarmNow); ok {
		t.Fatalf("a vessel inside its circle must not alarm")
	}
}

func TestAnchorWatcherRaisesOutsideRadiusPlusBuffer(t *testing.T) {
	watcher := testAnchorWatcher(anchorAt(-21.1113, 149.2276, 30), metersNorth(-21.1113, 60), 149.2276, false, true)

	event, ok := watcher.check(alarmNow)
	if !ok || event.Kind != alarmEventRaised {
		t.Fatalf("expected a drag alarm, got %+v", event)
	}
	if event.Status.State != alarmStateAlarm {
		t.Fatalf("state: got %q, want %q", event.Status.State, alarmStateAlarm)
	}
	if !strings.Contains(event.Status.Message, "dragging") {
		t.Fatalf("message should say it is dragging, got %q", event.Status.Message)
	}
}

// The buffer must be respected, or every vessel swinging on its rode alarms.
func TestAnchorWatcherDoesNotRaiseJustPastRadiusWithinBuffer(t *testing.T) {
	// 32m out with a 30m radius is inside the 4.572m buffer.
	watcher := testAnchorWatcher(anchorAt(-21.1113, 149.2276, 30), metersNorth(-21.1113, 32), 149.2276, false, true)

	if _, ok := watcher.check(alarmNow); ok {
		t.Fatalf("within the drag buffer must not alarm")
	}
}

// Asymmetric thresholds: it clears only back inside the radius, so a vessel
// sitting on the boundary cannot flap the alarm.
func TestAnchorWatcherClearsOnlyBackInsideTheRadius(t *testing.T) {
	anchor := anchorAt(-21.1113, 149.2276, 30)
	watcher := &anchorDragWatcher{anchor: func() *anchorWatchData { return anchor }}

	position := func(meters float64) {
		watcher.position = func() (float64, float64, bool, bool) {
			return metersNorth(-21.1113, meters), 149.2276, false, true
		}
	}

	position(60)
	if _, ok := watcher.check(alarmNow); !ok {
		t.Fatalf("expected the drag to raise first")
	}

	// Back inside radius+buffer but still outside the radius: stays raised.
	position(32)
	if _, ok := watcher.check(alarmNow); ok {
		t.Fatalf("must not clear while still outside the radius")
	}

	position(20)
	event, ok := watcher.check(alarmNow)
	if !ok || event.Kind != alarmEventCleared {
		t.Fatalf("expected a clear once back inside the radius, got %+v", event)
	}
}

func TestAnchorWatcherRaisesOnlyOnceWhileDragging(t *testing.T) {
	watcher := testAnchorWatcher(anchorAt(-21.1113, 149.2276, 30), metersNorth(-21.1113, 80), 149.2276, false, true)

	raises := 0
	for i := 0; i < 5; i++ {
		if event, ok := watcher.check(alarmNow); ok && event.Kind == alarmEventRaised {
			raises++
		}
	}

	if raises != 1 {
		t.Fatalf("expected 1 raise across a sustained drag, got %d", raises)
	}
}

// A lost fix is not a drag. Position freezes at its last trusted value during a
// SignalK outage (ADR 0037), so alarming on it would cry wolf exactly when the
// operator can least check — and the stream watchdog already reports the outage.
func TestAnchorWatcherIgnoresACriticalGNSSFix(t *testing.T) {
	watcher := testAnchorWatcher(anchorAt(-21.1113, 149.2276, 30), metersNorth(-21.1113, 500), 149.2276, true, true)

	if _, ok := watcher.check(alarmNow); ok {
		t.Fatalf("a critical GNSS fix must not raise a drag alarm")
	}
}

func TestAnchorWatcherIgnoresAnUnusablePosition(t *testing.T) {
	watcher := testAnchorWatcher(anchorAt(-21.1113, 149.2276, 30), 0, 0, false, false)

	if _, ok := watcher.check(alarmNow); ok {
		t.Fatalf("no usable position means nothing to judge")
	}
}

// Weighing anchor must take the alarm with it.
func TestAnchorWatcherClearsWhenTheWatchIsCancelled(t *testing.T) {
	anchor := anchorAt(-21.1113, 149.2276, 30)
	watcher := &anchorDragWatcher{
		anchor: func() *anchorWatchData { return anchor },
		position: func() (float64, float64, bool, bool) {
			return metersNorth(-21.1113, 80), 149.2276, false, true
		},
	}

	if _, ok := watcher.check(alarmNow); !ok {
		t.Fatalf("expected the drag to raise first")
	}

	anchor = nil
	event, ok := watcher.check(alarmNow)
	if !ok || event.Kind != alarmEventCleared {
		t.Fatalf("cancelling the watch must clear the alarm, got %+v", event)
	}
}

func TestAnchorWatcherQuietWhenNoWatchIsSet(t *testing.T) {
	watcher := testAnchorWatcher(nil, -21.1113, 149.2276, false, true)

	if _, ok := watcher.check(alarmNow); ok {
		t.Fatalf("no anchor watch means no drag alarm")
	}
}
