package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// The two failure modes an alarm system cannot detect from the inside, and the
// reason Maretron owners pay for a cloud subscription:
//
//  1. The data source dies. Under a delta stream every value simply stops
//     changing, so a frozen dashboard looks like a calm boat.
//  2. The boat itself goes off — power, internet, hardware. Nothing can be sent,
//     and silence is indistinguishable from "nothing is wrong".
//
// The watchdog covers the first. The heartbeat covers the second by inverting
// the signal: a periodic "still alive" means its *absence* is the alarm.

const (
	watchdogRuleID = "helmcentral:signalk-stream"
	watchdogPath   = "notifications.helmcentral.signalk"

	defaultStreamSilenceSeconds = 120
	watchdogCheckInterval       = 15 * time.Second
	heartbeatCheckInterval      = 1 * time.Minute

	// watchdogReconcileGrace is how long the bus is given to catch up with a
	// transition before a disagreement counts as real. A published clear travels
	// to the server and back through the delta stream, so the bus lags by a
	// round trip; without this the watchdog would republish a clear that is
	// merely in flight.
	watchdogReconcileGrace = 90 * time.Second
)

type streamWatchdog struct {
	snapshot     *signalKSnapshot
	silenceAfter time.Duration

	raised           bool
	lastTransitionAt time.Time
}

func newStreamWatchdog(snapshot *signalKSnapshot, silenceAfter time.Duration) *streamWatchdog {
	if silenceAfter <= 0 {
		silenceAfter = defaultStreamSilenceSeconds * time.Second
	}
	return &streamWatchdog{snapshot: snapshot, silenceAfter: silenceAfter}
}

// watchdogSilenceAfter resolves the configured threshold, treating zero as
// "use the default". It exists so the running watchdog and the value the API
// reports can never disagree: a guard that skipped zero left the watchdog on a
// previous threshold while /api/alarm-transports reported the default, which is
// the trusted-and-silently-wrong failure this package tries hardest to avoid.
func watchdogSilenceAfter(config watchdogConfig) time.Duration {
	if config.StreamSilenceSeconds <= 0 {
		return defaultStreamSilenceSeconds * time.Second
	}
	return time.Duration(config.StreamSilenceSeconds) * time.Second
}

func watchdogRule() alarmRule {
	return alarmRule{
		ID:      watchdogRuleID,
		Enabled: true,
		Label:   "SignalK stream",
		Path:    watchdogPath,
		State:   alarmStateAlarm,
		Methods: []string{"visual", "sound"},
	}
}

// silence reports whether the stream currently counts as dead and how long it
// has been quiet. judged is false before the first message, when there is
// nothing to judge — a server that has not finished starting is not an outage.
func (w *streamWatchdog) silence(now time.Time) (silent bool, quiet time.Duration, judged bool) {
	connected, lastMessage := w.snapshot.status()

	silent = !connected
	if !lastMessage.IsZero() {
		quiet = now.Sub(lastMessage)
		if quiet > w.silenceAfter {
			silent = true
		}
	}
	return silent, quiet, !lastMessage.IsZero()
}

// check reports a transition, if any. Like the rule engine it is edge-triggered:
// a persistently dead stream produces one alarm, not one per tick.
func (w *streamWatchdog) check(now time.Time) (alarmEvent, bool) {
	silent, quiet, judged := w.silence(now)

	if !judged && !w.raised {
		return alarmEvent{}, false
	}

	if silent == w.raised {
		return alarmEvent{}, false
	}
	w.raised = silent
	w.lastTransitionAt = now

	if silent {
		message := "SignalK stream is not connected"
		if quiet > 0 {
			message = fmt.Sprintf("No SignalK data for %s", quiet.Round(time.Second))
		}
		rule := watchdogRule()
		return alarmEvent{
			Kind: alarmEventRaised,
			Rule: rule,
			Status: alarmStatus{
				RuleID:   watchdogRuleID,
				Label:    rule.Label,
				Path:     rule.Path,
				Phase:    alarmPhaseActive,
				State:    alarmStateAlarm,
				Message:  message,
				RaisedAt: now,
			},
		}, true
	}

	return w.clearEvent("SignalK stream recovered"), true
}

func (w *streamWatchdog) clearEvent(message string) alarmEvent {
	rule := watchdogRule()
	return alarmEvent{
		Kind: alarmEventCleared,
		Rule: rule,
		Status: alarmStatus{
			RuleID:  watchdogRuleID,
			Label:   rule.Label,
			Path:    rule.Path,
			Phase:   alarmPhaseNormal,
			State:   alarmStateNormal,
			Message: message,
		},
	}
}

// publishedLive reports whether the bus still carries Helmcentral's own
// watchdog notification in a raised state, as seen coming back off the stream.
func (w *streamWatchdog) publishedLive() bool {
	value, ok := notificationValueAt(w.snapshot, strings.TrimPrefix(watchdogPath, notificationsRoot+"."))
	return ok && notificationValueIsLive(value)
}

// reconcile repairs a published state that no longer matches reality.
//
// check takes its edges from w.raised, which lives only in memory, but what it
// publishes is a notification object on the SignalK bus that outlives this
// process. When a clear is lost — a write dropped while the server was
// unreachable, a stale queued raise landing on top of it, a restart while an
// alarm was open — the two disagree, and because the edge has already been
// taken nothing will ever emit that clear again. The boat is left showing an
// alarm no condition supports and no code path can retract, which is precisely
// the trusted-and-silently-wrong failure this package exists to prevent.
//
// Only the clearing direction is reconciled. Republishing a clear is idempotent,
// so the worst case is a redundant one; republishing a raise on a disagreement
// would instead let a lagging bus manufacture alarms out of nothing.
func (w *streamWatchdog) reconcile(now time.Time) (alarmEvent, bool) {
	if w.raised {
		return alarmEvent{}, false
	}

	silent, _, judged := w.silence(now)
	if silent || !judged {
		return alarmEvent{}, false
	}

	if !w.lastTransitionAt.IsZero() && now.Sub(w.lastTransitionAt) < watchdogReconcileGrace {
		return alarmEvent{}, false
	}

	if !w.publishedLive() {
		return alarmEvent{}, false
	}

	w.lastTransitionAt = now
	return w.clearEvent("SignalK stream is healthy; clearing a stale alarm left on the bus"), true
}

func startStreamWatchdog(ctx context.Context, interval time.Duration) {
	watchdog := newStreamWatchdog(globalSignalKSnapshot, watchdogSilenceAfter(getAlarmTransports().Watchdog))

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			// Re-read each tick so a settings change takes effect without a
			// restart, including a reset back to the default.
			watchdog.silenceAfter = watchdogSilenceAfter(getAlarmTransports().Watchdog)

			at := now.UTC()
			event, ok := watchdog.check(at)
			if !ok {
				event, ok = watchdog.reconcile(at)
			}
			if ok {
				recordAlarmEvent(event, at)
			}
		}
	}
}

// ── heartbeat ─────────────────────────────────────────────────────────────────

type heartbeatSender struct {
	dispatcher *alarmDispatcher
	interval   time.Duration
	lastSent   time.Time
}

// due reports whether a heartbeat should go out now.
func (h *heartbeatSender) due(now time.Time) bool {
	if h.interval <= 0 {
		return false
	}
	return h.lastSent.IsZero() || now.Sub(h.lastSent) >= h.interval
}

// send delivers the heartbeat directly rather than through the retry queue.
//
// A missed heartbeat is self-correcting — the next one covers it — whereas
// queueing would deliver a burst of stale "still alive" messages on reconnect,
// each claiming a health that was true hours ago. That is worse than useless
// for a signal whose entire meaning is timeliness.
func (h *heartbeatSender) send(ctx context.Context, now time.Time, activeCount int, worst string) {
	transports := h.dispatcher.transports()
	if len(transports) == 0 {
		return
	}

	summary := "All clear"
	if activeCount > 0 {
		summary = fmt.Sprintf("%d active alarm(s), worst: %s", activeCount, worst)
	}

	msg := notificationMessage{
		Kind:    alarmEventRaised,
		RuleID:  "helmcentral:heartbeat",
		Label:   "Helmcentral heartbeat",
		Path:    "notifications.helmcentral.heartbeat",
		State:   alarmStateNormal,
		Message: fmt.Sprintf("Helmcentral is running. %s.", summary),
		Vessel:  vesselNameForNotifications(),
		At:      now,
	}

	h.lastSent = now

	for _, transport := range transports {
		// The SignalK transport is on-boat, so a heartbeat there proves
		// nothing about whether the boat can be reached.
		if transport.ID() == transportSignalK {
			continue
		}
		if err := transport.Send(ctx, msg); err != nil {
			log.Printf("heartbeat: %s failed (not queued, the next one supersedes it): %v", transport.ID(), err)
		}
	}
}

func startHeartbeat(ctx context.Context, interval time.Duration) {
	sender := &heartbeatSender{dispatcher: globalAlarmDispatcher}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			minutes := getAlarmTransports().Watchdog.HeartbeatMinutes
			sender.interval = time.Duration(minutes) * time.Minute

			at := now.UTC()
			if !sender.due(at) {
				continue
			}

			active := activeAlarms()
			sender.send(ctx, at, len(active), worstAlarmStateOf(active))
		}
	}
}
