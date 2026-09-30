package main

import (
	"fmt"
	"log"
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

	// staleNMEALogged tracks, per notification label, whether the "set aside
	// as stale" diagnostic (logStaleNMEATransitions, ADR 0144) has already
	// been logged for the current stale spell -- so the log carries one line
	// for the whole spell, not one every tick for as long as the plotter's
	// route stays stopped, matching notificationSyncer's own failing-streak
	// idiom (notification_sync.go).
	staleNMEALogged map[string]bool
}

func newBusNotificationWatcher(snapshot *signalKSnapshot) *busNotificationWatcher {
	return &busNotificationWatcher{
		snapshot:        snapshot,
		dwell:           busNotificationDwell,
		tracked:         map[string]*busNotificationState{},
		staleNMEALogged: map[string]bool{},
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

	// Collision alarms are raised on the AIS targets' own contexts, which the
	// self walk never reaches (ADR 0057). Without them here a CPA alarm is
	// visible only in the live list: no push, no log row, no trace afterwards.
	//
	// helmcentralOwnershipPredicate is the same guard signalKNotifications
	// applies for display (alarm_ownership.go): it recognises a path under
	// Helmcentral's own derived-value namespace as an echo even when no rule
	// on this instance is currently configured for it, which is exactly the
	// shape of a notification a different Helmcentral instance left on the
	// same boat's bus. ownedNotificationPaths below still runs on top of it —
	// it also owns a disabled rule's path, which this predicate deliberately
	// does not (alarm_ownership.go), and this watcher must not re-raise that
	// one either: disabling a rule does not retract what it already
	// published.
	busStatuses := signalKNotifications(w.snapshot, helmcentralOwnershipPredicate(), now)
	busStatuses = append(busStatuses, signalKCollisionNotifications(w.snapshot, now)...)

	// signalKNotifications silently drops a notification it has set aside as
	// stale (ADR 0144), with no trace of its own -- from this watcher's own
	// live/raise/clear bookkeeping below, one going stale is indistinguishable
	// from one that genuinely cleared, which is the point (a stale one leaving
	// behaves exactly like a clear). This is only the diagnostic: it logs the
	// transition so an operator watching the log can tell "the route really
	// cleared" apart from "Helmcentral is quietly sitting on an old
	// notification."
	w.logStaleNMEATransitions(now)

	live := map[string]alarmStatus{}
	for _, status := range busStatuses {
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
			// An alarm already acknowledged on the bus has been seen and dealt
			// with -- by an MFD, another client, or by this crew before a
			// Helmcentral restart. Raising it pages someone for something they
			// have already handled, and because these statuses are rebuilt from
			// the tree rather than held locally, a restart would do it again on
			// every boot for as long as the condition lasts.
			//
			// Deliberately a skip of the raise only, not a filter on the live
			// set above. Acknowledging is not clearing: dropping these from
			// live would make an already-raised one look like it had vanished,
			// emitting a clear that closes its log row and pushes "alarm over"
			// while the condition is still live.
			//
			// Silenced is not acknowledged and is not skipped -- a silenced
			// alarm has stopped sounding but is still demanding attention
			// (alarm_notifications.go).
			if status.Phase == alarmPhaseAcknowledged {
				// An acknowledgement withdrawn later leaves a live alarm nobody
				// has answered, so it serves a full fresh dwell from that point
				// rather than raising the instant the flag flips back.
				state.pendingSince = now
				continue
			}

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

// logStaleNMEATransitions logs once when a live self notification is set
// aside because its NMEA 0183 sentence has gone stale (nmeaNotificationIsStale,
// ADR 0144), and once when it is no longer excluded -- not on every tick for
// as long as the condition holds either way.
func (w *busNotificationWatcher) logStaleNMEATransitions(now time.Time) {
	if w.staleNMEALogged == nil {
		w.staleNMEALogged = map[string]bool{}
	}

	root := w.snapshot.nodeAt(notificationsRoot)
	stale := staleNMEANotificationLabels(w.snapshot, root, now)

	for label, info := range stale {
		if w.staleNMEALogged[label] {
			continue
		}
		w.staleNMEALogged[label] = true
		lastSeen := "not since Helmcentral started listening"
		if info.EverSeen {
			lastSeen = info.LastSeen.Format(time.RFC3339)
		}
		log.Printf("notifications.%s: no fresh %s sentence (last seen: %s), set aside rather than surfaced as an alarm",
			label, info.Sentence, lastSeen)
	}

	var live []alarmStatus
	if root != nil {
		collectSignalKNotifications(root, nil, &live)
	}
	stillLive := map[string]bool{}
	for _, status := range live {
		stillLive[status.Label] = true
	}

	for label := range w.staleNMEALogged {
		if _, stillStale := stale[label]; stillStale {
			continue
		}
		delete(w.staleNMEALogged, label)
		// A notification that cleared while set aside has gone, not come
		// back; its clear needs no line of its own here.
		if stillLive[label] {
			log.Printf("notifications.%s: no longer set aside as stale", label)
		}
	}
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
	owned := map[string]bool{watchdogPath: true, collisionProfilePath: true}
	for _, rule := range listAlarmRules() {
		owned[notificationsRoot+"."+strings.TrimPrefix(rule.Path, notificationsRoot+".")] = true
	}
	return owned
}
