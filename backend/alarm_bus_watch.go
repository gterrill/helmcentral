package main

import (
	"fmt"
	"strings"
	"time"
)

// busNotificationDwell debounces a notification before Helmcentral raises it.
// ADR 0038 §3 argues hard that undebounced alarms are why people switch marine
// alarm systems off; a flapping Victron cell-voltage alarm must not page on
// every edge.
const busNotificationDwell = 5 * time.Second

// busNotificationState is what busNotificationWatcher remembers between ticks
// for one bus-sourced notification, keyed by its namespaced RuleID
// ("notifications:<path>").
type busNotificationState struct {
	// pendingSince is when the notification was first seen live. Only
	// meaningful before raised is true; recovery before the dwell elapses
	// drops the entire entry rather than clearing this field, so the next
	// sighting starts the dwell over (alarm_engine.go's own rule).
	pendingSince time.Time
	raised       bool

	// label, path and state are the notification's last known shape, kept so
	// a clear can be reported after the notification has already vanished
	// from the tree and there is nothing left to read them from.
	label string
	path  string
	state string
}

// busNotificationWatcher watches the SignalK notifications subtree other
// producers raise into and turns edges into alarmEvents for recordAlarmEvent,
// mirroring streamWatchdog's shape (alarm_watchdog.go). Without this, a
// notification raised by anything else on the bus -- Victron GX, N2K devices,
// other SignalK plugins -- is visible only in the live alarm list
// (activeAlarms), read fresh on every request: no off-boat push is ever
// dispatched, no alarm-log row is ever written, and once the source clears
// it, it is gone from the live list too with no trace it ever happened.
type busNotificationWatcher struct {
	snapshot *signalKSnapshot
	dwell    time.Duration
	tracked  map[string]*busNotificationState
}

func newBusNotificationWatcher(snapshot *signalKSnapshot) *busNotificationWatcher {
	return &busNotificationWatcher{
		snapshot: snapshot,
		dwell:    busNotificationDwell,
		tracked:  map[string]*busNotificationState{},
	}
}

// check diffs the live notifications tree against what was tracked on the
// previous tick and returns the transitions. Edge-triggered throughout, like
// the rule engine and the watchdog: a persistently live notification produces
// exactly one raise, not one per tick.
func (w *busNotificationWatcher) check(now time.Time) []alarmEvent {
	if w.dwell <= 0 {
		w.dwell = busNotificationDwell
	}
	if w.tracked == nil {
		w.tracked = map[string]*busNotificationState{}
	}

	owned := ownedNotificationPaths()

	live := map[string]alarmStatus{}
	for _, status := range signalKNotifications(w.snapshot) {
		// Path ownership is the guard against re-ingesting Helmcentral's own
		// alarms. Commit 7d8ffb3 made Helmcentral publish its own rule and
		// watchdog alarms onto notifications.* (signalk_publish.go); without
		// this, a rule fires, gets published, is read back here on the next
		// tick, and gets re-raised as a "bus" alarm -- a duplicate log row and
		// a duplicate notification for every alarm Helmcentral itself raises.
		//
		// This is deliberately not a $source check: the exact string
		// signalk-server stamps on a re-emitted delta is not verifiable from
		// this repo, and a guard that cannot be tested is worse than one that
		// can be. Path ownership is deterministic and entirely local.
		if owned[status.Path] {
			continue
		}
		live[status.RuleID] = status
	}

	var events []alarmEvent

	for ruleID, status := range live {
		state, tracking := w.tracked[ruleID]
		if !tracking {
			state = &busNotificationState{pendingSince: now}
			w.tracked[ruleID] = state
		}
		state.label = status.Label
		state.path = status.Path

		if !state.raised {
			if now.Sub(state.pendingSince) < w.dwell {
				continue
			}
			state.raised = true
			state.state = status.State
			events = append(events, busNotificationEvent(alarmEventRaised, status))
			continue
		}

		// Only worsening escalates. recordAlarmEvent's switch only logs
		// raised/cleared, so this dispatches without writing a duplicate log
		// row for what is still the same open occurrence.
		if alarmStateRank[status.State] > alarmStateRank[state.state] {
			state.state = status.State
			events = append(events, busNotificationEvent(alarmEventEscalated, status))
		} else {
			state.state = status.State
		}
	}

	for ruleID, state := range w.tracked {
		if _, stillLive := live[ruleID]; stillLive {
			continue
		}
		if state.raised {
			events = append(events, busNotificationEvent(alarmEventCleared, alarmStatus{
				RuleID:  ruleID,
				Label:   state.label,
				Path:    state.path,
				Phase:   alarmPhaseNormal,
				State:   alarmStateNormal,
				Message: fmt.Sprintf("%s cleared", state.label),
			}))
		}
		// A pending entry that recovered before the dwell elapsed is simply
		// forgotten: it never raised, so there is nothing to clear, and the
		// next sighting starts a fresh dwell rather than resuming this one.
		delete(w.tracked, ruleID)
	}

	return events
}

// busNotificationEvent synthesizes a pseudo-rule for one notification status,
// following streamWatchdog's shape (alarm_watchdog.go). Notify is
// deliberately left empty: selectTransports treats an empty list as "every
// enabled transport" (alarm_notify.go), and a bus notification was never
// configured with one of its own.
func busNotificationEvent(kind string, status alarmStatus) alarmEvent {
	return alarmEvent{
		Kind: kind,
		Rule: alarmRule{
			ID:      status.RuleID,
			Enabled: true,
			Label:   status.Label,
			Path:    status.Path,
			State:   status.State,
		},
		Status: status,
		Source: alarmSourceSignalK,
	}
}

// ownedNotificationPaths is the set of notification paths Helmcentral itself
// publishes onto the bus -- every configured rule (as signalKNotifyTransport.Send
// derives them from msg.Path) plus the stream watchdog's own path
// (watchdogPath, alarm_watchdog.go). Reading one of these back out of the
// notifications tree is Helmcentral hearing its own echo, not a report from
// another producer.
//
// Disabled rules are included deliberately. Disabling a rule does not retract
// what it already published, so filtering to enabled rules would leave that
// notification on the bus for this watcher to re-ingest as though another
// producer had raised it. The cost is a narrow false negative -- a third-party
// notification at a path some local rule also owns is not reported -- which is
// the right way round: a missed duplicate beats a self-inflicted alarm loop.
func ownedNotificationPaths() map[string]bool {
	owned := map[string]bool{watchdogPath: true}
	for _, rule := range listAlarmRules() {
		owned[notificationsRoot+"."+strings.TrimPrefix(rule.Path, notificationsRoot+".")] = true
	}
	return owned
}
