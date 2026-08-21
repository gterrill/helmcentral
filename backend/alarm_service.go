package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/labstack/echo/v4"
)

// alarmEvaluationInterval is how often rules are re-evaluated. Dwell and
// hysteresis are expressed in seconds, so anything finer buys nothing.
const alarmEvaluationInterval = 1 * time.Second

var globalAlarmEngine = newAlarmEngine()
var globalBusNotificationWatcher = newBusNotificationWatcher(globalSignalKSnapshot)

// startAlarmEvaluator runs the rule engine against the delta-stream snapshot
// until ctx is cancelled, persisting every transition to the alarm log.
func startAlarmEvaluator(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			evaluateAlarmsOnce(now.UTC())
		}
	}
}

func evaluateAlarmsOnce(now time.Time) {
	// Deliberately no early return on an empty rule set: evaluate is also what
	// drops statuses for rules that have been deleted or disabled, so skipping
	// it would leave the last deleted rule's alarm stuck active forever.
	rules := listAlarmRules()

	events := globalAlarmEngine.evaluate(rules, snapshotAlarmReader(globalSignalKSnapshot), now)
	for _, event := range events {
		recordAlarmEvent(event, now)
	}

	// Re-read each tick, the same reason startStreamWatchdog re-reads its
	// silence threshold: production never reassigns globalSignalKSnapshot, so
	// this is a no-op there, but tests substitute it per case and the watcher
	// must never be left watching a stale snapshot object.
	globalBusNotificationWatcher.snapshot = globalSignalKSnapshot
	for _, event := range globalBusNotificationWatcher.check(now) {
		recordAlarmEvent(event, now)
	}
}

func recordAlarmEvent(event alarmEvent, now time.Time) {
	// Edge-triggered logging, the idiom gnss_validation.go uses: log the
	// transition, never the steady state, so a live alarm does not flood.
	log.Printf("alarm %s: %s [%s] %s", event.Kind, event.Rule.Label, event.Status.State, event.Status.Message)

	source := event.Source
	if source == "" {
		source = alarmSourceRule
	}

	switch event.Kind {
	case alarmEventRaised:
		_, err := globalAlarmLogStore.RecordRaised(alarmLogEntry{
			RuleID:       event.Rule.ID,
			Source:       source,
			Label:        event.Rule.Label,
			Path:         event.Rule.Path,
			State:        event.Status.State,
			Message:      event.Status.Message,
			ValueAtRaise: event.Status.Value,
			RaisedAt:     now,
		})
		if err != nil {
			log.Printf("alarm log: %v", err)
		}
	case alarmEventCleared:
		if err := globalAlarmLogStore.MarkCleared(event.Rule.ID, now); err != nil {
			log.Printf("alarm log: %v", err)
		}
	}

	if globalAlarmDispatcher != nil {
		globalAlarmDispatcher.dispatch(event, vesselNameForNotifications())
	}
}

// vesselNameForNotifications labels off-boat notifications, which is the whole
// difference between "ALARM: Bilge" and knowing which boat sent it.
func vesselNameForNotifications() string {
	return fetchSignalKSelfName()
}

// activeAlarms merges rule-driven alarms with notifications raised by anything
// else on the SignalK bus, worst severity first.
func activeAlarms() []alarmStatus {
	// Always a list, never null: the UI iterates this without a nil guard.
	combined := make([]alarmStatus, 0)
	combined = append(combined, globalAlarmEngine.active()...)
	combined = append(combined, signalKNotifications(globalSignalKSnapshot)...)

	sort.Slice(combined, func(i, j int) bool {
		if alarmStateRank[combined[i].State] != alarmStateRank[combined[j].State] {
			return alarmStateRank[combined[i].State] > alarmStateRank[combined[j].State]
		}
		return combined[i].Path < combined[j].Path
	})
	return combined
}

func worstAlarmState() string {
	worst := alarmStateNormal
	for _, status := range activeAlarms() {
		if alarmStateRank[status.State] > alarmStateRank[worst] {
			worst = status.State
		}
	}
	return worst
}

// ── HTTP ──────────────────────────────────────────────────────────────────────

func listAlarmRulesHandler(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]any{"rules": listAlarmRules()})
}

func createAlarmRuleHandler(c echo.Context) error {
	var rule alarmRule
	if err := c.Bind(&rule); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
	}

	created, err := createAlarmRule(rule)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, created)
}

func updateAlarmRuleHandler(c echo.Context) error {
	var rule alarmRule
	if err := c.Bind(&rule); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
	}

	updated, err := updateAlarmRule(c.Param("id"), rule)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, updated)
}

func deleteAlarmRuleHandler(c echo.Context) error {
	if err := deleteAlarmRule(c.Param("id")); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

// buildAlarmsPayload is shared by the REST handler and the SSE stream so the
// two shapes cannot drift apart.
func buildAlarmsPayload() map[string]any {
	return map[string]any{
		"alarms": activeAlarms(),
		"worst":  worstAlarmState(),
	}
}

func alarmsHandler(c echo.Context) error {
	return c.JSON(http.StatusOK, buildAlarmsPayload())
}

func acknowledgeAlarmHandler(c echo.Context) error {
	return alarmActionHandler(c, notificationActionAcknowledge)
}

// silenceAlarmHandler stops an alarm sounding without acknowledging it. SignalK
// draws that distinction and Helmcentral's own engine does not, so this action
// exists only for notifications on the bus.
func silenceAlarmHandler(c echo.Context) error {
	return alarmActionHandler(c, notificationActionSilence)
}

func alarmActionHandler(c echo.Context, action string) error {
	// Echo prefers URL.RawPath, so a param arrives still percent-encoded. Rule
	// ids are UUIDs and never notice, but a bus-sourced id carries the
	// "notifications:" namespace colon the browser encodes as %3A — left
	// escaped it matches no prefix and no rule, and answers 409 for every
	// alarm on the bus.
	ruleID, err := url.PathUnescape(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "malformed alarm id"})
	}
	now := time.Now().UTC()

	// A bus-sourced notification is never in the engine's status map — it is
	// rebuilt from the SignalK tree on every poll — so the action goes to the
	// SignalK Notifications API, where it survives and where every other
	// consumer sees it.
	if path, busSourced := notificationRuleIDPath(ruleID); busSourced {
		status, err := actOnSignalKNotification(globalSignalKSnapshot, path, action, now, postSignalKNotificationAction)
		if err != nil {
			// A refusal is a 409 the operator can act on; a failed call is an
			// upstream fault and must not be dressed up as one.
			code := http.StatusBadGateway
			if errors.Is(err, errNotificationNotLive) || errors.Is(err, errNotificationEmergency) || errors.Is(err, errNotificationNotAllowed) {
				code = http.StatusConflict
			}
			return c.JSON(code, map[string]string{"error": err.Error()})
		}

		if action == notificationActionAcknowledge {
			if err := globalAlarmLogStore.MarkAcknowledged(ruleID, now); err != nil {
				log.Printf("alarm log: %v", err)
			}
		}
		return c.JSON(http.StatusOK, status)
	}

	if action == notificationActionSilence {
		// The engine has one action. Silencing a rule alarm without
		// acknowledging it is not a thing it can express, and pretending
		// otherwise would report a silence that never happened.
		return c.JSON(http.StatusConflict, map[string]string{
			"error": "a rule-driven alarm can only be acknowledged, not silenced",
		})
	}

	if !globalAlarmEngine.acknowledge(ruleID, now) {
		// Either there is nothing live to acknowledge, or it is an emergency,
		// which SignalK says cannot be silenced.
		return c.JSON(http.StatusConflict, map[string]string{
			"error": "alarm is not acknowledgeable",
		})
	}

	if err := globalAlarmLogStore.MarkAcknowledged(ruleID, now); err != nil {
		log.Printf("alarm log: %v", err)
	}
	return c.JSON(http.StatusOK, globalAlarmEngine.statusFor(ruleID))
}

func alarmLogHandler(c echo.Context) error {
	entries, err := globalAlarmLogStore.Recent(100)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if entries == nil {
		entries = []alarmLogEntry{}
	}
	return c.JSON(http.StatusOK, map[string]any{"entries": entries})
}

func getAlarmTransportsHandler(c echo.Context) error {
	return c.JSON(http.StatusOK, getAlarmTransports())
}

func setAlarmTransportsHandler(c echo.Context) error {
	var config alarmTransportConfig
	if err := c.Bind(&config); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
	}

	saved, err := setAlarmTransports(config)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, saved)
}

// testAlarmTransportsHandler sends a probe through every enabled transport.
// Discovering at 3am that the ntfy topic was mistyped is the failure this
// exists to prevent.
func testAlarmTransportsHandler(c echo.Context) error {
	transports := buildTransports(getAlarmTransports())
	if len(transports) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "no transports are enabled"})
	}

	msg := notificationMessage{
		Kind:    alarmEventRaised,
		Label:   "Helmcentral test notification",
		Path:    "notifications.helmcentral.test",
		State:   alarmStateAlert,
		Message: "If you can read this, alarm notifications are working.",
		Vessel:  vesselNameForNotifications(),
		At:      time.Now().UTC(),
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 20*time.Second)
	defer cancel()

	results := map[string]string{}
	for _, transport := range transports {
		if err := transport.Send(ctx, msg); err != nil {
			results[transport.ID()] = err.Error()
			continue
		}
		results[transport.ID()] = "ok"
	}
	return c.JSON(http.StatusOK, map[string]any{"results": results})
}
