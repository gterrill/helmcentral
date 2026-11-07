package main

import (
	"strings"
	"testing"
	"time"
)

// positionFeed lets a test move a fix between trusted, critical and missing
// without rebuilding the watcher.
type positionFeed struct {
	gnssCritical bool
	ok           bool
}

func newTestPositionWatcher(anchor *anchorWatchData, feed *positionFeed) *anchorPositionWatcher {
	return &anchorPositionWatcher{
		anchor: func() *anchorWatchData { return anchor },
		position: func() (float64, float64, bool, bool) {
			return -21.1113, 149.2276, feed.gnssCritical, feed.ok
		},
	}
}

func TestAnchorPositionLostWaitsTheFullDelay(t *testing.T) {
	feed := &positionFeed{ok: false}
	watcher := newTestPositionWatcher(anchorAt(-21.1113, 149.2276, 30), feed)
	start := alarmNow

	if _, ok := watcher.check(start); ok {
		t.Fatalf("the first missed fix must only start the timer")
	}
	if _, ok := watcher.check(start.Add(anchorPositionLostDelay - time.Second)); ok {
		t.Fatalf("59 s without a fix must not raise")
	}
}

func TestAnchorPositionLostRaisesAtTheDelay(t *testing.T) {
	feed := &positionFeed{ok: false}
	watcher := newTestPositionWatcher(anchorAt(-21.1113, 149.2276, 30), feed)
	start := alarmNow

	watcher.check(start)
	event, ok := watcher.check(start.Add(anchorPositionLostDelay))
	if !ok || event.Kind != alarmEventRaised {
		t.Fatalf("60 s without a fix must raise, got %+v", event)
	}
	if event.Rule.ID != anchorPositionLostRuleID || event.Status.RuleID != anchorPositionLostRuleID {
		t.Fatalf("rule id: got %q", event.Rule.ID)
	}
	if event.Status.State != alarmStateAlarm {
		t.Fatalf("state: got %q, want %q", event.Status.State, alarmStateAlarm)
	}
	if event.Status.Label != "Anchor Position Lost" {
		t.Fatalf("title: got %q", event.Status.Label)
	}
	// It must say the position is unknown, never that the anchor is dragging.
	body := strings.ToLower(event.Status.Message)
	if !strings.Contains(body, "position") || strings.Contains(body, "drag") || strings.Contains(body, "!") {
		t.Fatalf("body should say the position is unknown, got %q", event.Status.Message)
	}
	// Distinct from the drag alarm, so the two never share a card or a path.
	if event.Rule.ID == anchorDragRuleID || event.Rule.Path == anchorDragPath {
		t.Fatalf("must not share the drag alarm's identity")
	}
}

func TestAnchorPositionLostRaisesOnlyOnce(t *testing.T) {
	feed := &positionFeed{ok: false}
	watcher := newTestPositionWatcher(anchorAt(-21.1113, 149.2276, 30), feed)
	start := alarmNow

	raises := 0
	for i := 0; i < 20; i++ {
		if event, ok := watcher.check(start.Add(time.Duration(i) * 10 * time.Second)); ok && event.Kind == alarmEventRaised {
			raises++
		}
	}
	if raises != 1 {
		t.Fatalf("expected 1 raise across a sustained outage, got %d", raises)
	}
}

func TestAnchorPositionLostRaisesOnACriticalFix(t *testing.T) {
	feed := &positionFeed{ok: true, gnssCritical: true}
	watcher := newTestPositionWatcher(anchorAt(-21.1113, 149.2276, 30), feed)
	start := alarmNow

	watcher.check(start)
	event, ok := watcher.check(start.Add(anchorPositionLostDelay))
	if !ok || event.Kind != alarmEventRaised {
		t.Fatalf("an untrusted fix held for 60 s must raise, got %+v", event)
	}
}

func TestAnchorPositionLostClearsWhenATrustedFixReturns(t *testing.T) {
	feed := &positionFeed{ok: false}
	watcher := newTestPositionWatcher(anchorAt(-21.1113, 149.2276, 30), feed)
	start := alarmNow

	watcher.check(start)
	watcher.check(start.Add(anchorPositionLostDelay))

	feed.ok = true
	event, ok := watcher.check(start.Add(anchorPositionLostDelay + time.Second))
	if !ok || event.Kind != alarmEventCleared || event.Rule.ID != anchorPositionLostRuleID {
		t.Fatalf("a trusted fix must clear the alarm, got %+v", event)
	}
	if event.Status.Message != "Position restored" {
		t.Fatalf("a returned fix says so, got %q", event.Status.Message)
	}

	if _, ok := watcher.check(start.Add(anchorPositionLostDelay + 2*time.Second)); ok {
		t.Fatalf("a clear is reported once")
	}
}

func TestAnchorPositionLostQuietWithoutAWatch(t *testing.T) {
	feed := &positionFeed{ok: false}
	watcher := newTestPositionWatcher(nil, feed)
	start := alarmNow

	for i := 0; i < 10; i++ {
		if _, ok := watcher.check(start.Add(time.Duration(i) * 30 * time.Second)); ok {
			t.Fatalf("no anchor watch means no position-lost alarm")
		}
	}
}

func TestAnchorPositionLostTimerStartsWhenTheWatchIsSet(t *testing.T) {
	feed := &positionFeed{ok: false}
	var anchor *anchorWatchData
	watcher := newTestPositionWatcher(nil, feed)
	watcher.anchor = func() *anchorWatchData { return anchor }
	start := alarmNow

	// An hour of outage before any watch exists counts for nothing.
	watcher.check(start)
	anchor = anchorAt(-21.1113, 149.2276, 30)
	if _, ok := watcher.check(start.Add(time.Hour)); ok {
		t.Fatalf("the delay runs from when the watch is active, not from the outage")
	}
	if _, ok := watcher.check(start.Add(time.Hour + anchorPositionLostDelay)); !ok {
		t.Fatalf("expected a raise 60 s after the watch became active")
	}
}

func TestAnchorPositionLostBriefDropoutResetsTheTimer(t *testing.T) {
	feed := &positionFeed{ok: false}
	watcher := newTestPositionWatcher(anchorAt(-21.1113, 149.2276, 30), feed)
	start := alarmNow

	watcher.check(start)
	watcher.check(start.Add(40 * time.Second))

	feed.ok = true
	if _, ok := watcher.check(start.Add(45 * time.Second)); ok {
		t.Fatalf("a fix returning before the alarm raised has nothing to clear")
	}

	feed.ok = false
	watcher.check(start.Add(50 * time.Second))
	// 70 s after the first loss but only 20 s into the second one.
	if _, ok := watcher.check(start.Add(70 * time.Second)); ok {
		t.Fatalf("the timer must restart after a trusted fix")
	}
	if _, ok := watcher.check(start.Add(110 * time.Second)); !ok {
		t.Fatalf("expected a raise 60 s into the second outage")
	}
}

func TestAnchorPositionLostClearsWhenTheWatchIsCancelled(t *testing.T) {
	feed := &positionFeed{ok: false}
	anchor := anchorAt(-21.1113, 149.2276, 30)
	watcher := newTestPositionWatcher(anchor, feed)
	watcher.anchor = func() *anchorWatchData { return anchor }
	start := alarmNow

	watcher.check(start)
	watcher.check(start.Add(anchorPositionLostDelay))

	anchor = nil
	event, ok := watcher.check(start.Add(anchorPositionLostDelay + time.Second))
	if !ok || event.Kind != alarmEventCleared {
		t.Fatalf("weighing anchor must take the alarm with it, got %+v", event)
	}
	if event.Status.Message != "Anchor watch cancelled" {
		t.Fatalf("a cancel must not claim the position is back, got %q", event.Status.Message)
	}
}

// A drag alarm that is already up stays up through an outage: the drag watcher
// stands down on a lost fix, so it neither clears nor re-raises, and only a
// trusted fix decides whether the boat is back inside the circle.
func TestDragAlarmStaysRaisedThroughAnOutage(t *testing.T) {
	anchor := anchorAt(-21.1113, 149.2276, 30)
	feed := &positionFeed{ok: true}
	drag := &anchorDragWatcher{
		anchor: func() *anchorWatchData { return anchor },
		position: func() (float64, float64, bool, bool) {
			if !feed.ok {
				return 0, 0, false, false
			}
			return metersNorth(-21.1113, 80), 149.2276, false, true
		},
	}
	if _, ok := drag.check(alarmNow); !ok {
		t.Fatalf("expected the drag to raise first")
	}

	feed.ok = false
	for i := 1; i <= 5; i++ {
		if _, ok := drag.check(alarmNow.Add(time.Duration(i) * time.Minute)); ok {
			t.Fatalf("the drag alarm must neither clear nor re-raise during an outage")
		}
	}
	if !drag.raised {
		t.Fatalf("the drag alarm must still be raised")
	}
}
