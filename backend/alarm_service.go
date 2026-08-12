package main

import (
	"context"
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/labstack/echo/v4"
)

// alarmEvaluationInterval is how often rules are re-evaluated. Dwell and
// hysteresis are expressed in seconds, so anything finer buys nothing.
const alarmEvaluationInterval = 1 * time.Second

var globalAlarmEngine = newAlarmEngine()

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
}

func recordAlarmEvent(event alarmEvent, now time.Time) {
	// Edge-triggered logging, the idiom gnss_validation.go uses: log the
	// transition, never the steady state, so a live alarm does not flood.
	log.Printf("alarm %s: %s [%s] %s", event.Kind, event.Rule.Label, event.Status.State, event.Status.Message)

	switch event.Kind {
	case alarmEventRaised:
		_, err := globalAlarmLogStore.RecordRaised(alarmLogEntry{
			RuleID:       event.Rule.ID,
			Source:       alarmSourceRule,
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

func alarmsHandler(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]any{
		"alarms": activeAlarms(),
		"worst":  worstAlarmState(),
	})
}

func acknowledgeAlarmHandler(c echo.Context) error {
	ruleID := c.Param("id")
	now := time.Now().UTC()

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
