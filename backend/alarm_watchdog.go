package main

import (
	"context"
	"fmt"
	"log"
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
)

type streamWatchdog struct {
	snapshot     *signalKSnapshot
	silenceAfter time.Duration

	raised bool
}

func newStreamWatchdog(snapshot *signalKSnapshot, silenceAfter time.Duration) *streamWatchdog {
	if silenceAfter <= 0 {
		silenceAfter = defaultStreamSilenceSeconds * time.Second
	}
	return &streamWatchdog{snapshot: snapshot, silenceAfter: silenceAfter}
}

// check reports a transition, if any. Like the rule engine it is edge-triggered:
// a persistently dead stream produces one alarm, not one per tick.
func (w *streamWatchdog) check(now time.Time) (alarmEvent, bool) {
	connected, lastMessage := w.snapshot.status()

	silent := !connected
	var silence time.Duration
	if !lastMessage.IsZero() {
		silence = now.Sub(lastMessage)
		if silence > w.silenceAfter {
			silent = true
		}
	}

	// Before the first message there is nothing to judge — a server that has
	// not finished starting is not an outage.
	if lastMessage.IsZero() && !w.raised {
		return alarmEvent{}, false
	}

	if silent == w.raised {
		return alarmEvent{}, false
	}
	w.raised = silent

	rule := alarmRule{
		ID:      watchdogRuleID,
		Enabled: true,
		Label:   "SignalK stream",
		Path:    watchdogPath,
		State:   alarmStateAlarm,
		Methods: []string{"visual", "sound"},
	}

	if silent {
		message := "SignalK stream is not connected"
		if silence > 0 {
			message = fmt.Sprintf("No SignalK data for %s", silence.Round(time.Second))
		}
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

	return alarmEvent{
		Kind: alarmEventCleared,
		Rule: rule,
		Status: alarmStatus{
			RuleID:  watchdogRuleID,
			Label:   rule.Label,
			Path:    rule.Path,
			Phase:   alarmPhaseNormal,
			State:   alarmStateNormal,
			Message: "SignalK stream recovered",
		},
	}, true
}

func startStreamWatchdog(ctx context.Context, interval time.Duration) {
	watchdog := newStreamWatchdog(globalSignalKSnapshot,
		time.Duration(getAlarmTransports().Watchdog.StreamSilenceSeconds)*time.Second)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			// Re-read each tick so a settings change takes effect without a
			// restart.
			if seconds := getAlarmTransports().Watchdog.StreamSilenceSeconds; seconds > 0 {
				watchdog.silenceAfter = time.Duration(seconds) * time.Second
			}

			if event, ok := watchdog.check(now.UTC()); ok {
				recordAlarmEvent(event, now.UTC())
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
			sender.send(ctx, at, len(active), worstAlarmState())
		}
	}
}
