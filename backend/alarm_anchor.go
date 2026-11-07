package main

import (
	"context"
	"fmt"
	"time"
)

// Anchor drag detection lives on the server so the alarm survives the browser.
//
// It used to be derived in the React hook, which meant closing the tab silenced
// it — the single worst property the old alarm had, since dragging happens at
// 3am with the screen off. Server-side it also flows through the normal alarm
// path: logged, acknowledgeable, and delivered off the boat by whatever
// transports are configured (ADR 0038).

const (
	anchorDragRuleID = "helmcentral:anchor-drag"
	anchorDragPath   = "notifications.navigation.anchor"

	// Matches the frontend's former DRAG_BUFFER_METERS (15ft), so the distance
	// at which the alarm fires is unchanged by the move.
	anchorDragBufferMeters = 4.572

	anchorDragCheckInterval = 5 * time.Second

	anchorPositionLostRuleID = "helmcentral:anchor-position-lost"
	anchorPositionLostPath   = "notifications.navigation.anchorPosition"

	// How long the fix must stay missing or untrusted, without a break, before
	// the position-lost alarm raises. A GPS resettling after a brief dropout
	// should not wake anyone.
	anchorPositionLostDelay = 60 * time.Second
)

type anchorDragWatcher struct {
	// anchor and position are injectable so tests need neither stored state
	// nor a SignalK stream.
	anchor   func() *anchorWatchData
	position func() (lat float64, lon float64, gnssCritical bool, ok bool)

	raised bool
}

func newAnchorDragWatcher() *anchorDragWatcher {
	return &anchorDragWatcher{
		anchor: func() *anchorWatchData {
			anchorWatchMu.RLock()
			defer anchorWatchMu.RUnlock()
			return anchorWatchState
		},
		position: func() (float64, float64, bool, bool) {
			state, err := fetchSignalKVesselState()
			if err != nil {
				return 0, 0, true, false
			}
			if !hasUsableVesselPosition(state.Latitude, state.Longitude) {
				return 0, 0, state.GNSSCriticalAlert, false
			}
			return state.Latitude, state.Longitude, state.GNSSCriticalAlert, true
		},
	}
}

// check reports a transition, if any. Edge-triggered like the rest of the alarm
// system, so a vessel that stays outside its circle raises once.
func (w *anchorDragWatcher) check(now time.Time) (alarmEvent, bool) {
	anchor := w.anchor()
	if anchor == nil {
		// The watch was cancelled or the anchor raised; any live drag alarm
		// goes with it.
		return w.transition(false, 0, now)
	}

	lat, lon, gnssCritical, ok := w.position()

	// A lost fix is not a drag. The position freezes at its last trusted value
	// during an outage (ADR 0037), so treating that as movement would cry wolf
	// exactly when the operator can least verify it. The outage itself is
	// reported by anchorPositionWatcher below, not here, and an already-raised
	// drag alarm stays raised until a trusted fix says the boat is back inside
	// the circle. (The stream watchdog only notices when the whole SignalK
	// stream goes quiet, so it does not cover a dead or untrusted GPS.)
	if !ok || gnssCritical {
		return alarmEvent{}, false
	}

	distance := haversineMeters(anchor.Lat, anchor.Lon, lat, lon)

	// Asymmetric thresholds: it fires past radius + buffer but only clears back
	// inside the radius, so a vessel sitting on the boundary cannot flap.
	if w.raised {
		return w.transition(distance > anchor.RadiusMeters, distance, now)
	}
	return w.transition(distance > anchor.RadiusMeters+anchorDragBufferMeters, distance, now)
}

func (w *anchorDragWatcher) transition(dragging bool, distance float64, now time.Time) (alarmEvent, bool) {
	if dragging == w.raised {
		return alarmEvent{}, false
	}
	w.raised = dragging

	rule := alarmRule{
		ID:      anchorDragRuleID,
		Enabled: true,
		Label:   "Anchor Dragging",
		Path:    anchorDragPath,
		State:   alarmStateAlarm,
		Methods: []string{"visual", "sound"},
	}

	if dragging {
		return alarmEvent{
			Kind: alarmEventRaised,
			Rule: rule,
			Status: alarmStatus{
				RuleID:   anchorDragRuleID,
				Label:    rule.Label,
				Path:     rule.Path,
				Phase:    alarmPhaseActive,
				State:    alarmStateAlarm,
				Value:    distance,
				Message:  fmt.Sprintf("Anchor %.0f m from the drop point.", distance),
				RaisedAt: now,
			},
		}, true
	}

	return alarmEvent{
		Kind: alarmEventCleared,
		Rule: rule,
		Status: alarmStatus{
			RuleID:  anchorDragRuleID,
			Label:   rule.Label,
			Path:    rule.Path,
			Phase:   alarmPhaseNormal,
			State:   alarmStateNormal,
			Value:   distance,
			Message: "Anchor holding",
		},
	}, true
}

// anchorPositionWatcher raises a separate alarm when an active anchor watch has
// had no trusted fix for anchorPositionLostDelay. The drag check stands down on
// a lost fix (a lost fix is not a drag), which at 3am would leave a dragging
// boat silent; this alarm says so plainly without claiming a drag.
//
// A drag alarm already raised is left alone during the outage: it keeps
// showing, neither cleared nor re-evaluated, until a trusted fix lets the drag
// check judge the real position. Cancelling the watch clears both.
type anchorPositionWatcher struct {
	anchor   func() *anchorWatchData
	position func() (lat float64, lon float64, gnssCritical bool, ok bool)

	// lostSince is when the current unbroken run of missing or untrusted fixes
	// began; zero while the fix is good or no watch is active.
	lostSince time.Time
	raised    bool
}

func newAnchorPositionWatcher() *anchorPositionWatcher {
	drag := newAnchorDragWatcher()
	return &anchorPositionWatcher{anchor: drag.anchor, position: drag.position}
}

func (w *anchorPositionWatcher) check(now time.Time) (alarmEvent, bool) {
	if w.anchor() == nil {
		return w.settle(now, "Anchor watch cancelled")
	}

	_, _, gnssCritical, ok := w.position()
	if ok && !gnssCritical {
		return w.settle(now, "Position restored")
	}

	if w.lostSince.IsZero() {
		w.lostSince = now
	}
	if w.raised || now.Sub(w.lostSince) < anchorPositionLostDelay {
		return alarmEvent{}, false
	}
	w.raised = true

	rule := w.rule()
	return alarmEvent{
		Kind: alarmEventRaised,
		Rule: rule,
		Status: alarmStatus{
			RuleID:   rule.ID,
			Label:    rule.Label,
			Path:     rule.Path,
			Phase:    alarmPhaseActive,
			State:    alarmStateAlarm,
			Message:  "Position unknown. The anchor watch can't tell whether the boat is holding.",
			RaisedAt: now,
		},
	}, true
}

// settle ends an outage: the timer restarts, and a raised alarm clears with
// message, which says why: a returned fix, or a watch cancelled with no fix.
func (w *anchorPositionWatcher) settle(now time.Time, message string) (alarmEvent, bool) {
	w.lostSince = time.Time{}
	if !w.raised {
		return alarmEvent{}, false
	}
	w.raised = false

	rule := w.rule()
	return alarmEvent{
		Kind: alarmEventCleared,
		Rule: rule,
		Status: alarmStatus{
			RuleID:  rule.ID,
			Label:   rule.Label,
			Path:    rule.Path,
			Phase:   alarmPhaseNormal,
			State:   alarmStateNormal,
			Message: message,
		},
	}, true
}

func (w *anchorPositionWatcher) rule() alarmRule {
	return alarmRule{
		ID:      anchorPositionLostRuleID,
		Enabled: true,
		Label:   "Anchor Position Lost",
		Path:    anchorPositionLostPath,
		State:   alarmStateAlarm,
		Methods: []string{"visual", "sound"},
	}
}

func startAnchorDragWatcher(ctx context.Context, interval time.Duration) {
	watcher := newAnchorDragWatcher()
	positionWatcher := newAnchorPositionWatcher()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if event, ok := watcher.check(now.UTC()); ok {
				recordAlarmEvent(event, now.UTC())
			}
			if event, ok := positionWatcher.check(now.UTC()); ok {
				recordAlarmEvent(event, now.UTC())
			}
		}
	}
}
