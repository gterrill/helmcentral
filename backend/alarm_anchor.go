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
	// exactly when the operator can least verify it — and the stream watchdog
	// already reports the outage itself.
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
		Label:   "Anchor dragging",
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
				Message:  fmt.Sprintf("Anchor dragging: %.0fm from where it was set", distance),
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

func startAnchorDragWatcher(ctx context.Context, interval time.Duration) {
	watcher := newAnchorDragWatcher()

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
		}
	}
}
